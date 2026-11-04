package httpapi

import (
	"context"
	"fmt"
	"log"
	"math/rand"
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
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/voicebatch"
)

const (
	rewriteMinRunes = 400
	rewriteMaxRunes = 1600
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
	if frozen, ok := scriptRewriteSnapshotFor(v); ok && frozen.Prompt == "" {
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
	openings := scriptUsedOpenings(pool, voiceID)
	kind := pickOpeningKind(rand.New(rand.NewSource(time.Now().UnixNano())), nil)
	document, err := s.loadScriptRewriteSnapshot(ctx, tenant, v)
	if err != nil || strings.TrimSpace(document.Prompt) == "" {
		return
	}
	turn, err := beginScriptRewriteTurn(ctx, recordingCacheMediaRoot(), newScriptRewriteContextScope(tenant, v.PlanID, unit.Text, document.Facts))
	if err != nil {
		log.Printf("script rewrite context unavailable plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	defer turn.Close()
	prompt, err := renderBackgroundScriptRewritePrompt(document.Prompt, unit.Text, openings, kind, turn.Previous)
	if err != nil || strings.TrimSpace(prompt) == "" {
		return
	}
	work, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	result, err := s.completePlayedScriptModel(work, agentgateway.Request{
		Stage:          "script_rewrite",
		Messages:       scriptRewriteMessages(prompt),
		ResponseFormat: agentgateway.ResponseText,
		MaxTokens:      2400,
		Timeout:        180 * time.Second,
	})
	if err != nil {
		log.Printf("script rewrite failed plan=%d piece=%s: %v", v.PlanID, shortKey(unit.Key), err)
		return
	}
	parsed, err := parseScriptRewriteResponse(result.Text)
	if err != nil {
		log.Printf("script rewrite output rejected plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	script := strings.TrimSpace(parsed.Script)
	// P0 命中时只改命中的那几句：先让模型改写，改不好就删掉这句，不整段丢掉。
	script = s.repairRewriteViolations(work, v, script)
	// 删句之后可能不足 400 字：先试着用事实补写；补不上也照用，不丢弃。
	if utf8.RuneCountInString(script) < rewriteMinRunes {
		if extended, ok := s.extendRewriteScript(work, v, script); ok {
			script = s.repairRewriteViolations(work, v, extended)
		} else {
			log.Printf("script rewrite short but kept plan=%d piece=%s runes=%d", v.PlanID, shortKey(unit.Key), utf8.RuneCountInString(script))
		}
	}
	if !rewriteScriptAcceptable(v, unit.Text, script) {
		log.Printf("script rewrite rejected plan=%d piece=%s runes=%d", v.PlanID, shortKey(unit.Key), utf8.RuneCountInString(script))
		return
	}
	if err := saveScriptSharedText(recordingCacheMediaRoot(), v, unit, script); err != nil {
		log.Printf("script shared text save failed plan=%d piece=%s: %v", v.PlanID, unit.poolID(), err)
		return
	}
	parsed.Script = script
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

// rewriteScriptAcceptable 只挡空内容。
//
// 用户口径：删句之后即使不足 400 字，也要入库并合成 TTS，不要整段丢掉；
// 补写只是尽量做，补不上也照用。重复度也不再卡。
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
			if patched, ok := s.askRewriteSentences(ctx, v, targets); ok {
				script = replaceRewriteSentences(script, sentences, targets, patched)
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

// extendRewriteScript 把过短的改写稿补到 400 字以上。
//
// 只允许用方案的事实依据补，不许编；补完仍不够就放弃这次改写。
func (s *Server) extendRewriteScript(ctx context.Context, v model.LiveAgentPlanVersion, script string) (string, bool) {
	document, err := s.loadScriptRewriteSnapshot(ctx, v.TenantID, v)
	if err != nil {
		return "", false
	}
	facts := strings.TrimSpace(document.Facts)
	if facts == "" {
		return "", false
	}
	prompt := "下面这段直播话术太短了，请补到 400 字以上。" +
		"只能用【事实依据】里已有的内容补细节，不许编价格、时效、规格、售后，不许加事实里没有的数字；" +
		"保持原来的说法和顺序，只把没讲透的地方讲开；" +
		"直接输出补好后的完整话术，一段连续文字，不要解释、不要小标题。\n\n" +
		"【现有话术】\n" + strings.TrimSpace(script) + "\n\n【事实依据】\n" + facts
	result, err := s.completePlayedScriptModel(ctx, agentgateway.Request{
		Stage:     "script_rewrite_extend",
		Messages:  []agentgateway.Message{{Role: "user", Content: prompt}},
		MaxTokens: 2000,
		Timeout:   150 * time.Second,
	})
	if err != nil {
		return "", false
	}
	out := strings.TrimSpace(result.Text)
	if utf8.RuneCountInString(out) <= utf8.RuneCountInString(script) {
		return "", false
	}
	return out, true
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

// scriptUsedOpenings 把这个片段已经用过的开场列成清单，交给模型避开。
//
// 只给开头 12 字，token 很省；比"禁止某类词"稳，模型按实际用过的写法换角度。
func scriptUsedOpenings(pool scriptplay.Pool, voiceID string) string {
	var lines []string
	seen := map[string]bool{}
	for _, v := range pool.VoiceVersions(voiceID) {
		text := strings.TrimSpace(v.Text)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > 12 {
			runes = runes[:12]
		}
		head := string(runes)
		if seen[head] {
			continue
		}
		seen[head] = true
		lines = append(lines, "- "+head)
	}
	if len(lines) == 0 {
		return "（还没有用过的开场，这一版自己起一个）"
	}
	return strings.Join(lines, "\n")
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
