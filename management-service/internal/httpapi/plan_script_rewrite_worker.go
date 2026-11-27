package httpapi

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
	"livecompanion/management/internal/scriptchain"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/voicebatch"
)

// 后台补改写版本：同一时间只跑一条，避免模型并发和 TTS 抢占。
var rewriteWorker = struct {
	sync.Mutex
	inFlight map[string]bool
	slot     chan struct{}
}{inFlight: map[string]bool{}, slot: make(chan struct{}, 1)}

// maybeGenerateRewriteVersion consumes one real playback opportunity before
// servicing the shared plan supply decision. Inventory polls alone cannot buy.
//
// 规则（对齐开发任务书 4.1–4.3）：
//  1. 该音色下已有原始版本（原始优先）
//  2. 版本数没有到上限
//  3. 同一条片段同一时间只跑一个改写
//
// 校验不过就放弃本次，不写入版本池；失败只记日志，不影响直播。
func (s *Server) maybeGenerateRewriteVersion(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) {
	var epochErr error
	ctx, v, epochErr = s.voiceAssetGeneration(ctx, tenant, v)
	if epochErr != nil {
		return
	}
	unit = scriptUnitForVoice(v, unit)
	if strings.TrimSpace(unit.Text) == "" {
		return
	}
	if frozen, ok := scriptRewriteSnapshotFor(v); ok && frozen.RedLines == nil && frozen.Prompt == "" {
		return
	}
	session := agentgateway.ContextRoomModelScope(ctx).RuntimeID
	play, playErr := pendingScriptGenerationPlay(recordingCacheMediaRoot(), tenant, room, session, v)
	if ctx.Err() != nil || session <= 0 || playErr != nil || play == nil || play.Piece != unit.poolID() {
		if ctx.Err() != nil {
			discardScriptGenerationPlay(recordingCacheMediaRoot(), tenant, room, session, v, play)
		}
		return
	}
	defer func() {
		if ctx.Err() != nil {
			discardScriptGenerationPlay(recordingCacheMediaRoot(), tenant, room, session, v, play)
		}
	}()
	voiceID := v.VoiceIdentity.VoiceID
	// All voices share text: serialize by piece, not by person, so two rooms
	// cannot purchase the same new rewrite concurrently for different voices.
	poolKey := fmt.Sprintf("%d|%d|%s", tenant, v.PlanID, unit.poolID())
	rewriteWorker.Lock()
	if rewriteWorker.inFlight[poolKey] {
		rewriteWorker.Unlock()
		return
	}
	pool := scriptCompatiblePool(recordingCacheMediaRoot(), v, unit)
	if !pool.HasOriginal(voiceID) {
		rewriteWorker.Unlock()
		return
	}
	// Capacity and finite refresh batches are now owned by the plan policy.
	cap := 0
	full := false
	retireRef := ""

	rewriteWorker.inFlight[poolKey] = true
	rewriteWorker.Unlock()
	defer func() {
		rewriteWorker.Lock()
		delete(rewriteWorker.inFlight, poolKey)
		rewriteWorker.Unlock()
	}()

	select {
	case rewriteWorker.slot <- struct{}{}:
		defer func() { <-rewriteWorker.slot }()
	case <-ctx.Done():
		return
	}
	roomRelease, idle, roomErr := tryRoomScriptGeneration(recordingCacheMediaRoot(), tenant, room)
	if roomErr != nil || !idle {
		return
	}
	defer roomRelease()
	if pending, err := scriptManualGenerationPending(tenant, room, session); err != nil || pending {
		return
	}
	sharedRelease, acquired, lockErr := tryScriptGeneration(recordingCacheMediaRoot(), tenant, v.PlanID, unit.poolID())
	if lockErr != nil || !acquired {
		return
	}
	defer sharedRelease()
	planRelease, planAcquired, planErr := tryScriptGeneration(recordingCacheMediaRoot(), tenant, v.PlanID, "policy-"+scriptFirstParagraphKey(v, "policy"))
	if planErr != nil || !planAcquired {
		return
	}
	defer planRelease()
	units, unitsErr := scriptFirstUnits(v, filepath.Dir(unit.Dir))
	if unitsErr != nil {
		return
	}
	index, wanted, retired, policyErr := scriptPolicyGenerationForPiece(recordingCacheMediaRoot(), v, units, play.Piece, time.Now().UTC(), true, scriptPolicyRoomWarmup(recordingCacheMediaRoot(), tenant, room, v.PlanID, time.Now().UTC()))
	if policyErr != nil || index < 0 || units[index].poolID() != unit.poolID() {
		discardScriptGenerationPlay(recordingCacheMediaRoot(), tenant, room, session, v, play)
		return
	}
	cap, retireRef, full = wanted, retired, retired != ""
	// Re-read after the cross-process gate: another room may have completed a
	// shared audio slot while this worker was waiting for the provider slot.
	pool = scriptCompatiblePool(recordingCacheMediaRoot(), v, unit)
	if !pool.HasOriginal(voiceID) {
		return
	}

	// Compatibility hook; hourly character limits have been retired.
	if !allowScriptVoiceSynthesis(v.PlanID, hanCount(unit.Text)) {
		return
	}
	if ctx.Err() != nil {
		return
	}
	guard := func(c context.Context) error { return s.checkScriptGenerationOwner(c, tenant, room, session, v) }
	if err := guard(ctx); err != nil {
		discardScriptGenerationPlay(recordingCacheMediaRoot(), tenant, room, session, v, play)
		return
	}
	playRelease, claimed, claimErr := claimScriptGenerationPlay(ctx, recordingCacheMediaRoot(), tenant, room, session, v, unit, play)
	if claimErr != nil || !claimed {
		return
	}
	defer playRelease()
	ctx = context.WithValue(ctx, scriptGenerationGuardKey{}, guard)
	log.Printf("script generation play consumed tenant=%d room=%d session=%d piece=%s instance=%s", tenant, room, session, unit.poolID(), play.Instance)
	ctx = withLiveCostScope(ctx, liveCostCallScope{TenantID: tenant, RoomID: room, PlanID: v.PlanID, Priority: "optional", Reason: "话术后台改写", Key: fmt.Sprintf("script-rewrite:%d:%s:%d", v.PlanID, unit.poolID(), time.Now().UnixNano())})
	ctx = withScriptModelCostSteps(ctx)
	pool = scriptCompatiblePool(recordingCacheMediaRoot(), v, unit)
	if !pool.HasOriginal(voiceID) || (!full && len(pool.Versions) >= cap) {
		return
	}
	if shared, found, err := scriptMissingSharedText(recordingCacheMediaRoot(), v, unit); err != nil {
		log.Printf("script shared text unavailable plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	} else if found {
		if err := checkScriptGenerationGuard(ctx); err != nil {
			return
		}
		if err := s.synthesizeSharedScriptText(ctx, tenant, room, v, unit, shared.Text, cap, full, retireRef); err != nil {
			log.Printf("script shared text tts deferred plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		}
		return
	}
	document, frozen := scriptRewriteSnapshotFor(v)
	if !frozen {
		var err error
		document, err = s.loadScriptRewriteSnapshot(ctx, tenant, v)
		if err != nil {
			return
		}
	}
	if document.RedLines == nil && strings.TrimSpace(document.Prompt) == "" {
		return
	}
	facts, err := s.rewriteFactsJSON(ctx, tenant, v.PlanID)
	if err != nil {
		log.Printf("script rewrite facts unavailable plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	if len(facts) == 0 || string(facts) == "[]" {
		log.Printf("script rewrite skipped without confirmed facts plan=%d piece=%s", v.PlanID, unit.poolID())
		return
	}
	redlines, ok := backgroundRewriteRedLines(document)
	if !ok {
		log.Printf("script rewrite redlines unavailable plan=%d piece=%s", v.PlanID, unit.poolID())
		return
	}
	turn, err := beginScriptRewriteTurn(ctx, recordingCacheMediaRoot(), newScriptRewriteContextScope(tenant, v.PlanID, unit.Text, string(facts)))
	if err != nil {
		log.Printf("script rewrite context unavailable plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	defer turn.Close()
	work := ctx
	var cancel context.CancelFunc
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()
	parsed, err := s.generateScriptText(ctx, scriptGenerationDefault, scriptTextGenerationInput{
		material: scriptTextMaterial{version: v, unit: unit, facts: facts, redLines: redlines},
		session:  session,
		writingContext: func(ctx context.Context) context.Context {
			work, cancel = context.WithTimeout(ctx, 180*time.Second)
			return work
		},
	})
	if err != nil {
		log.Printf("script rewrite failed plan=%d piece=%s: %v", v.PlanID, shortKey(unit.Key), err)
		return
	}
	script := parsed.Script
	if err := saveScriptSharedText(recordingCacheMediaRoot(), v, unit, script); err != nil {
		log.Printf("script shared text save failed plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	if err := turn.Save(parsed); err != nil {
		log.Printf("script rewrite context save failed plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	turn.Close() // Context survives a later TTS failure; audio never holds this lock.
	// 改写正文合成成新音频：键里带文本，天然和原文缓存分开。
	dir := unit.Dir
	if cut := strings.LastIndex(dir, string(filepath.Separator)); cut > 0 {
		dir = dir[:cut]
	}
	rewriteUnit := volcengineScriptUnit{Text: script, Key: scriptFirstParagraphKey(v, script), Dir: filepath.Join(dir, scriptFirstParagraphKey(v, script))}
	if err := checkScriptGenerationGuard(work); err != nil {
		return
	}
	audio, err := s.ensureScriptFirstParagraph(work, tenant, room, 0, v, rewriteUnit, true)
	if err != nil || audio.AssetID <= 0 {
		log.Printf("script rewrite tts failed plan=%d piece=%s: %v", v.PlanID, shortKey(unit.Key), err)
		return
	}
	if err := checkScriptGenerationGuard(work); err != nil {
		// Paid source/cache remains retained; late work cannot activate or retire.
		return
	}
	version := scriptplay.Version{
		FactsKey:    scriptRewriteFactsKey(v),
		PieceID:     unit.poolID(),
		Fingerprint: scriptplay.Fingerprint(script),
		Text:        script,
		VoiceID:     v.VoiceIdentity.VoiceID,
		Model:       v.VoiceIdentity.Model,
		Kind:        scriptplay.KindRewrite,
		AudioRef:    strconv.FormatInt(audio.AssetID, 10),
		DurationMS:  audio.DurationMS,
		SourcePath:  rewriteUnit.Dir,
	}
	if added, err := s.commitVoiceRewrite(ctx, tenant, recordingCacheMediaRoot(), v, unit, version, cap, full, retireRef); err != nil {
		log.Printf("script rewrite register failed plan=%d piece=%s: %v", v.PlanID, shortKey(unit.Key), err)
		return
	} else if !added {
		return
	}
	if err := markScriptSharedTextRendered(recordingCacheMediaRoot(), v, unit, script); err != nil {
		log.Printf("script shared text receipt failed: %v", err)
	}
	if hits := scriptHasTimeDrift(script); len(hits) > 0 {
		log.Printf("script rewrite time drift plan=%d piece=%s words=%v", v.PlanID, shortKey(unit.Key), hits)
	}
	log.Printf("script rewrite ready plan=%d piece=%s runes=%d budget_left=%d", v.PlanID, shortKey(unit.Key), utf8.RuneCountInString(script), scriptVoiceBudgetLeft(v.PlanID))
}

// writeScriptRewrite is shared by live supply and the editor preview. The
// caller decides whether and when the returned text is admitted to the pool.
func (s *Server) writeScriptRewrite(ctx context.Context, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, prompt string) (scriptRewriteOutput, error) {
	request := agentgateway.Request{
		Stage:          "script_rewrite",
		Messages:       scriptRewriteMessages(prompt),
		ResponseFormat: agentgateway.ResponseText,
		MaxTokens:      4000,
		Timeout:        180 * time.Second,
	}
	if _, live := ctx.Value(scriptGenerationGuardKey{}).(func(context.Context) error); live {
		s.applyLiveScriptReasoning(ctx, &request)
	}
	reportScriptManualProgress(ctx, scriptManualWriting)
	result, err := s.completePlayedScriptModel(ctx, request)
	if err != nil {
		return scriptRewriteOutput{}, err
	}
	parsed, err := parseScriptRewriteResponse(joinRewriteBodyLines(result.Text))
	if err != nil {
		return scriptRewriteOutput{}, fmt.Errorf("改写输出格式错误: %w", err)
	}
	script := stripBracketed(scriptchain.NormalizeScript(parsed.Script))
	// P0 命中时只修命中的句子，修不好就删句；较短的有效正文仍然保留。
	reportScriptManualProgress(ctx, scriptManualReviewing)
	script = s.repairRewriteViolations(ctx, v, script)
	parsed.Script = stripBracketed(scriptchain.NormalizeScript(script))
	if !rewriteScriptAcceptable(v, unit.Text, parsed.Script) {
		return scriptRewriteOutput{}, fmt.Errorf("改写正文为空")
	}
	return parsed, nil
}

// renderChainRewritePrompt 拼接：结构（分析模型）+ 程序允许的通用要求 + 话术要求（客户原文）+ 红线 + 事实。
func renderChainRewritePrompt(persona, requirement, speechRequirements, redlines, facts string) string {
	var b strings.Builder
	if p := strings.TrimSpace(persona); p != "" {
		b.WriteString("【主播人设】\n")
		b.WriteString(p)
		b.WriteString("\n\n")
	}
	b.WriteString("以下内容是给你的结构说明，只用于安排讲述顺序。正文里不许出现其中的词：收口、段落、节点、链条、逻辑链、路径、门槛、环节。\n")
	b.WriteString(strings.TrimSpace(requirement))
	b.WriteString("\n只使用事实清单中的事实，保留适用条件和例外。资料没有写的，不作承诺。正文不朗读字段名和数组符号。\n")
	b.WriteString("开场和与观众聊天时避免绑定播放时段。事实清单中明确的发货、配送等时效照实表达。\n")
	b.WriteString("如果按这个顺序讲完还不到目标字数，换一个角度把同一件事再讲一次；不要写“再讲一遍”“换个说法”这类说明自己在重复的话。\n")
	if req := strings.TrimSpace(speechRequirements); req != "" {
		b.WriteString("\n【话术要求】\n")
		b.WriteString(req)
	}
	b.WriteString("\n\n【红线禁止】\n")
	b.WriteString(strings.TrimSpace(redlines))
	b.WriteString("\n\n【事实清单】\n")
	b.WriteString(facts)
	b.WriteString("\n\n")
	b.WriteString(planprompt.RewriteOutputContract)
	return b.String()
}

func (s *Server) checkScriptGenerationOwner(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.store == nil {
		return fmt.Errorf("script generation owner unavailable")
	}
	paused, err := s.checkLiveAnchorOwner(ctx, tenant, room, session, v.ID)
	if err != nil {
		return err
	}
	state, err := s.store.RuntimeVoiceBatch(ctx, tenant, room, session, v.ID)
	if err != nil {
		return err
	}
	active := v
	active.VoiceIdentity = state.Active
	if paused || state.Pending != nil || !voicebatch.Same(state.Active, v.VoiceIdentity) || scriptGenerationIdentity(active) != scriptGenerationIdentity(v) {
		return fmt.Errorf("script generation owner changed or paused")
	}
	return nil
}

// scriptRewritePrompt 取方案专属的改写提示词并填上样本。
func (s *Server) scriptRewritePrompt(ctx context.Context, tenant int64, v model.LiveAgentPlanVersion, sample, openings, openingKind string) (string, error) {
	document, err := s.loadScriptRewriteSnapshot(ctx, tenant, v)
	if err != nil {
		return "", err
	}
	return renderBackgroundScriptRewritePrompt(document.Prompt, sample, openings, openingKind, scriptRewriteOutput{})
}

func renderBackgroundScriptRewritePrompt(template, sample, openings, openingKind string, previous scriptRewriteOutput) (string, error) {
	prompt := strings.TrimSpace(template)
	if prompt == "" {
		return "", nil
	}
	prompt, err := renderScriptRewriteRequest(prompt, sample, openings, openingKind, previous)
	if err != nil {
		return "", err
	}
	// Move only our exact suffix; never re-parse already inserted sample or
	// opening text as a template, since its placeholders are literal data.
	prompt = strings.TrimSpace(strings.TrimSuffix(prompt, planprompt.RewriteOutputContract))
	// 存下来的可能是旧版提示词（占位符已被替换成空），补一段保证开场指令一定送到。
	if !strings.Contains(prompt, scriptOpeningBlockMarker) {
		prompt += scriptOpeningBlock(openings, openingKind)
	}
	// 时间语义：这条音频会在任何时段反复播，不能被绑定到某个时间。
	if !strings.Contains(prompt, scriptTimeRuleMarker) {
		prompt += scriptTimeRuleBlock()
	}
	return prompt + "\n\n" + planprompt.RewriteOutputContract, nil
}

// rewriteScriptAcceptable accepts any nonempty body after review.
func rewriteScriptAcceptable(v model.LiveAgentPlanVersion, sample, script string) bool {
	return strings.TrimSpace(script) != ""
}

// longestCommonRunes 保留给测试使用：判断两段文本的最长公共子串长度。
func longestCommonRunes(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	best := 0
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best = cur[j]
				}
			}
		}
		prev = cur
	}
	return best
}

// annotateScript 用当前 P0/P1/P2 规则检查改写正文，返回命中描述。
func annotateScript(text, p0, p1, p2 string) ([]string, error) {
	result, err := contentguard.Annotate(text, p0, p1, p2)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		if hit.Severity == "P0" {
			out = append(out, hit.RuleID+":"+hit.Text)
		}
	}
	return out, nil
}

// repairRewriteViolations 只处理 P0 命中的句子：先请模型改写这几句，改不动就删掉。
//
// 不整段丢弃的原因：整段是付过费的，扔了可惜；删一句只损失一句。
func (s *Server) repairRewriteViolations(ctx context.Context, v model.LiveAgentPlanVersion, script string) string {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		log.Printf("script rewrite P0 rules unavailable: %v", err)
		return ""
	}
	for attempt := 0; attempt < 2; attempt++ {
		hits, err := annotateScript(script, p0, p1, p2)
		if err != nil || len(hits) == 0 {
			return script
		}
		sentences := splitRewriteSentences(script)
		targets := pickViolatingSentences(sentences, hits)
		if len(targets) == 0 {
			return script
		}
		if attempt == 0 {
			reportScriptManualProgress(ctx, scriptManualRevising)
			if patched, ok := s.askRewriteSentences(ctx, v, targets); ok {
				script = replaceRewriteSentences(script, sentences, targets, patched)
				reportScriptManualProgress(ctx, scriptManualReviewing)
				continue
			}
		}
		// 改写失败或仍然命中：直接删掉这几句。
		script = removeRewriteSentences(script, sentences, targets)
	}
	return script
}

// splitRewriteSentences 按中文句末标点切句，保留标点。
func splitRewriteSentences(text string) []string {
	var out []string
	var buf strings.Builder
	for _, r := range text {
		buf.WriteRune(r)
		switch r {
		case '。', '！', '？', '；', '!', '?', ';':
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	if strings.TrimSpace(buf.String()) != "" {
		out = append(out, buf.String())
	}
	return out
}

// pickViolatingSentences 找出包含命中词的句子。
func pickViolatingSentences(sentences, hits []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, hit := range hits {
		word := hit
		if idx := strings.LastIndex(hit, ":"); idx >= 0 {
			word = hit[idx+1:]
		}
		if strings.TrimSpace(word) == "" {
			continue
		}
		for _, sentence := range sentences {
			if strings.Contains(sentence, word) && !seen[sentence] {
				seen[sentence] = true
				out = append(out, sentence)
			}
		}
	}
	return out
}

// askRewriteSentences 只把命中的句子交给模型改写，保持事实和数字不变。
func (s *Server) askRewriteSentences(ctx context.Context, v model.LiveAgentPlanVersion, targets []string) ([]string, bool) {
	prompt := "把下面这几句里的违规说法改掉，其他内容不要动：事实、价格、数字、规格、时效一律保持原样；" +
		"不要加新信息；一行一句，只输出改好的句子，不要编号、不要解释。\n\n" + strings.Join(targets, "\n")
	result, err := s.completePlayedScriptModel(ctx, agentgateway.Request{
		Stage:     "script_rewrite_repair",
		Messages:  []agentgateway.Message{{Role: "user", Content: prompt}},
		MaxTokens: 800,
		Timeout:   90 * time.Second,
	})
	if err != nil {
		return nil, false
	}
	lines := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(result.Text, "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) != len(targets) {
		return nil, false
	}
	return lines, true
}

// replaceRewriteSentences 用改好的句子替换原句。
func replaceRewriteSentences(script string, sentences, targets, patched []string) string {
	replaced := append([]string(nil), sentences...)
	for i, sentence := range sentences {
		for j, target := range targets {
			if sentence == target && j < len(patched) {
				replaced[i] = patched[j]
			}
		}
	}
	return strings.Join(replaced, "")
}

// removeRewriteSentences 删掉违规句，保留其余内容。
func removeRewriteSentences(script string, sentences, targets []string) string {
	var kept []string
	for _, sentence := range sentences {
		drop := false
		for _, target := range targets {
			if sentence == target {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, sentence)
		}
	}
	return strings.Join(kept, "")
}

// rewriteRefreshChance 是槽位满时每次播放换掉一条旧版本的概率。
const (
	rewriteRefreshChance = 0.4
	// rewriteRetireAfterPlays 每条版本至少被播这么多次，才允许被换掉。
	rewriteRetireAfterPlays = 4
)

// dropOldestScriptVersion 淘汰该音色下最久没播的一条版本，保持版本数不超上限。
//
// 只从池子里摘掉记录，音频文件留给后续清理任务处理，避免正在播的音频被删。
func dropOldestScriptVersion(planID int64, pieceKey, voiceID string, minPlays int, models ...string) {
	root := recordingCacheMediaRoot()
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return
	}
	defer release()
	pool, err := loadScriptVoicePoolLocked(root, planID, pieceKey)
	if err != nil {
		return
	}
	versions := pool.VoiceVersions(voiceID)
	if len(versions) <= 1 {
		return
	}
	// 只在"已经播够次数"的版本里挑最久没播的，保证新版本至少能被听到几次。
	oldest := -1
	for i := range versions {
		if len(models) > 0 && versions[i].Model != models[0] {
			continue
		}
		if versions[i].Kind != scriptplay.KindRewrite || versions[i].PlayCount < minPlays {
			continue
		}
		if oldest < 0 || scriptPlaybackBefore(versions[i].LastPlayedAt, versions[oldest].LastPlayedAt) {
			oldest = i
		}
	}
	if oldest < 0 {
		return
	}
	target := versions[oldest]
	kept := make([]scriptplay.Version, 0, len(pool.Versions)-1)
	for _, version := range pool.Versions {
		if version.AudioRef == target.AudioRef && version.VoiceID == target.VoiceID {
			continue
		}
		kept = append(kept, version)
	}
	pool.Versions = kept
	if err := saveScriptVoicePoolLocked(root, planID, pieceKey, pool); err == nil {
		log.Printf("script version rotated out plan=%d piece=%s ref=%s", planID, shortKey(pieceKey), target.AudioRef)
	}
}

// refreshRoll 决定槽位满时这一次要不要换掉一条旧版本。
//
// 单独抽出来是为了能统计验证概率，避免"看起来像 40%"。
func refreshRoll(sample float64) bool {
	return sample <= rewriteRefreshChance
}

// openingSimilarity 比较两段开头（前 12 字）的相似度，用于发现"每版都一个开场"。
func openingSimilarity(a, b string) float64 {
	ar, br := []rune(strings.TrimSpace(a)), []rune(strings.TrimSpace(b))
	if len(ar) > 12 {
		ar = ar[:12]
	}
	if len(br) > 12 {
		br = br[:12]
	}
	if len(ar) == 0 || len(br) == 0 {
		return 0
	}
	common := 0
	seen := map[rune]int{}
	for _, r := range br {
		seen[r]++
	}
	for _, r := range ar {
		if seen[r] > 0 {
			common++
			seen[r]--
		}
	}
	return 2 * float64(common) / float64(len(ar)+len(br))
}

// rewriteOpeningTooSimilar 判断这条改写稿的开场是否和池子里已有版本撞车。
//
// 只看开头一小段：正文相似度低就行，观众记住的是开头。
func rewriteOpeningTooSimilar(pool scriptplay.Pool, voiceID, script string) bool {
	head := script
	if runes := []rune(head); len(runes) > 12 {
		head = string(runes[:12])
	}
	for _, v := range pool.VoiceVersions(voiceID) {
		if openingSimilarity(head, v.Text) >= 0.6 {
			return true
		}
	}
	return false
}

// askRewriteOpening 只重写第一句开场，其余保持不变。
func (s *Server) askRewriteOpening(ctx context.Context, script string) (string, bool) {
	prompt := "只改下面这段话术的第一句：换一个完全不同的切入角度，" +
		"不要打招呼、不要自我介绍、不要叫观众称呼，直接落在具体内容上；" +
		"其余部分原样保留，事实和数字一律不动；直接输出完整话术，不要解释。\n\n" + script
	result, err := s.completePlayedScriptModel(ctx, agentgateway.Request{
		Stage:     "script_rewrite_opening",
		Messages:  []agentgateway.Message{{Role: "user", Content: prompt}},
		MaxTokens: 2000,
		Timeout:   120 * time.Second,
	})
	if err != nil {
		return "", false
	}
	out := strings.TrimSpace(result.Text)
	if utf8.RuneCountInString(out) < utf8.RuneCountInString(script)/2 {
		return "", false
	}
	return out, true
}

// scriptOpeningBlockMarker 用来判断提示词里是否已经带了开场指令。
const scriptOpeningBlockMarker = "【这一版的开场要求】"

// scriptOpeningBlock 在提示词尾部补一段开场要求，兼容旧版存下来的提示词。
func scriptOpeningBlock(openings, openingKind string) string {
	var b strings.Builder
	b.WriteString("\n\n" + scriptOpeningBlockMarker + "\n")
	if openingKind != "" {
		b.WriteString("这一版必须用这种开场方式：" + openingKind + "\n")
	}
	if strings.TrimSpace(openings) != "" {
		b.WriteString("这些开头这个片段已经用过，不要再用同样的写法：\n" + openings + "\n")
	}
	b.WriteString("开场可以打招呼、可以自我介绍，但不能和上面任何一条用同一个套路。\n")
	return b.String()
}

// scriptTimeRuleMarker 标记提示词里是否已带时间语义约束。
const scriptTimeRuleMarker = "【时间语义要求】"

// scriptTimeRuleBlock 强制这条音频的措辞在任何时段都成立。
func scriptTimeRuleBlock() string {
	return "\n\n" + scriptTimeRuleMarker + "\n" +
		"这条音频会被反复播放，凌晨播、下午播都可能是同一条。\n" +
		"所以不许出现具体钟点和具体时段（早上、上午、中午、下午、傍晚、晚上、凌晨、半夜），" +
		"也不许写“刚下班”“刚吃完晚饭”“睡前”这类只在某个时段成立的说法。\n" +
		"要提时间就只用“这会儿”“现在”“今儿”这类中性说法。\n"
}

// forbiddenTimeWords 用来检查生成结果有没有偷偷绑定时段。
var forbiddenTimeWords = []string{"下午", "上午", "早上", "中午", "傍晚", "晚上", "凌晨", "半夜", "刚下班", "刚吃完", "睡前"}

// scriptHasTimeDrift 返回命中的时段词，用于日志告警。
func scriptHasTimeDrift(script string) []string {
	var hits []string
	for _, word := range forbiddenTimeWords {
		if strings.Contains(script, word) {
			hits = append(hits, word)
		}
	}
	return hits
}
