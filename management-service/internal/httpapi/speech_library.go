package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"

	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	lib "livecompanion/management/internal/speechlibrary"
)

func (s *Server) SetSpeechLibrary(repo lib.Repository) { s.speechLibrary = repo }
func (s *Server) libraryPolicy(ctx context.Context) (lib.Policy, error) {
	if s.store == nil {
		return lib.DefaultPolicy(), nil
	}
	return s.store.SpeechLibraryPolicy(ctx)
}
func libraryTextIndependent(text string) bool {
	return !regexp.MustCompile(`刚才|上面说|前面说|上一段|接下来再|稍后再|等会儿再|待会儿再|昨天说|刚刚回答|刚刚问|刚来的.{0,8}问`).MatchString(text)
}
func libraryTemporalSource(state *anchorcycle.State) string {
	source := state.Facts
	for _, task := range state.Queue {
		if task.FactScope != nil {
			raw, _ := json.Marshal(task.FactScope)
			source += string(raw)
		}
	}
	return source
}
func libraryDeadlineSensitive(state *anchorcycle.State) bool {
	// Explicit expiring offers stay on the fresh path; they cannot outlive a
	// deadline while the fact snapshot itself remains unchanged.
	source := libraryTemporalSource(state)
	if scope := state.Task().FactScope; scope != nil {
		raw, _ := json.Marshal(scope)
		source = string(raw)
	}
	return regexp.MustCompile(`截止|截至|限时|活动时间|[0-9]{4}[年/-][0-9]{1,2}|[0-9]{1,2}[:：][0-9]{2}|[0-9]{1,2}点|(?:上午|下午|晚上|早上|凌晨|中午)[一二三四五六七八九十]{1,3}点`).MatchString(source)
}
func libraryTimeBoundary(state *anchorcycle.State) string {
	source := libraryTemporalSource(state)
	if regexp.MustCompile(`今天|今日|明天|明日|今晚|当日|本周|本月|截止|截至|限时|活动时间|[0-9]{4}[年/-][0-9]{1,2}`).MatchString(source) {
		return time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02")
	}
	return ""
}

// Credential/account and run IDs are transport identities, not content. Model,
// protocol, generation settings, prompt and endpoint changes invalidate text.
func libraryModel(p *agentgateway.SpeechModel) any {
	if p == nil {
		return nil
	}
	q := *p
	q.ID = ""
	q.Name = ""
	q.APIKey = ""
	q.HasAPIKey = false
	return q
}

// Supplementing another direction doesn't edit an existing direction. Each
// reused candidate separately proves its own saved card still exists unchanged.
func libraryAngle(p *anchorcycle.AnglePlan) any {
	if p == nil {
		return nil
	}
	return struct {
		Goal, Action, Concern string
		Calculations          []anchorcycle.HookCalculation
	}{p.Goal, p.Action, p.AudienceConcern, p.Calculations}
}
func libraryHookCurrent(state *anchorcycle.State, c anchorcycle.Candidate) bool {
	plan := state.CurrentAnglePlan()
	if c.AngleHook == nil {
		return plan == nil || len(plan.Cards) == 0
	}
	if plan == nil {
		return false
	}
	for _, h := range plan.Cards {
		if lib.Digest(h) == lib.Digest(*c.AngleHook) {
			return true
		}
	}
	return false
}
func libraryVoiceSHA(state *anchorcycle.State) string {
	if state.Config.TTS == nil {
		return lib.Digest(nil)
	}
	v := state.Config.TTS
	return lib.Digest(struct {
		Source, ID, Model string
		Profile           int64
		Rate              float64
		Format            string
	}{v.Source, v.VoiceID, v.Model, v.ProfileID, v.Rate, "pcm-exact-sentence-bundle-v1"})
}
func libraryTextSHA(state *anchorcycle.State, task anchorcycle.Task, plan *anchorcycle.AnglePlan) (string, error) {
	if state.P0PolicyMD == "" || state.TextModel == nil || !state.Config.SkipReview && state.ReviewModel == nil {
		return "", errors.New("话术库需要完整P0及当前生成模式的模型快照")
	}
	mothers := map[string]string{}
	names := []string{"angle-hook-plan.md", "fact-purpose-angle-generation.md", "first-live-opening.md"}
	if !state.Config.SkipReview {
		names = append(names, "mainline-final-content-review.md", "speech-constraint-text-review.md")
	}
	for _, name := range names {
		b, e := interactionpolicy.Read(name)
		if e != nil {
			return "", e
		}
		mothers[name] = lib.Digest(string(b))
	}
	if state.Config.SkipReview {
		mothers["admission"] = lib.AdmissionVersion + ":" + lib.GenerationAccepted
	} else if p, _, e := loadAuditFourLayerPrompt(); e == nil {
		mothers["audit-four-layer"] = lib.Digest(p)
		mothers["admission"] = lib.AdmissionVersion + ":" + lib.ReviewPassed
	} else {
		return "", e
	}
	_, contract, _, e := agentgateway.TextPromptContract(state.TextModel.Model)
	if e != nil {
		return "", e
	}
	for _, m := range []struct{ env, name string }{{"MAINLINE_FINAL_CONTENT_REVIEW_PROMPT_PATH", "mainline-final-content-review.md"}, {"ANCHOR_SPEECH_CONSTRAINT_TEXT_REVIEW_PATH", "speech-constraint-text-review.md"}, {"ANCHOR_STYLE_ANGLE_HOOK_PLAN_PROMPT_PATH", "angle-hook-plan.md"}} {
		if state.Config.SkipReview && m.name != "angle-hook-plan.md" {
			continue
		}
		md, _, err := loadReplaceablePromptAsset(m.env, "configs/prompts/anchor-style/"+m.name, "话术库规则指纹")
		if err != nil {
			return "", err
		}
		mothers[m.name] = lib.Digest(md)
	}
	mothers["text-contract"] = contract
	rules, err := loadGenerationP0()
	if err != nil {
		return "", err
	}
	mothers["generation-p0"] = lib.Digest(rules)
	mothers["time-boundary"] = libraryTimeBoundary(state)
	// Earlier entries may have been generated with explicit scope prefixes
	// misread as categories or full facts reintroduced through content strategy.
	// Isolate that material contract and the injected industry mapping without
	// deleting history or changing the operator's cache-reuse preference.
	mothers["fact-side-material"] = lib.Digest(struct {
		Version string
		Sides   []string
		Mapping map[string][]string
	}{"phase-side-and-initial-opening-v2", state.FissionSides, state.FactSideMap})
	config := state.Config
	config.TTS = nil
	config.CredentialID = ""
	config.TextProfileID = ""
	config.FeedbackID = ""
	var focus *anchorcycle.ProductFocus
	if state.ProductFocus != nil {
		q := *state.ProductFocus
		q.ResetRun()
		focus = &q
	}
	corrections := append([]map[string]any{}, state.LatestExpressionConstraints...)
	if task.FactScope != nil {
		for _, c := range task.FactScope.Corrections {
			if c["用户采用的实时表达约束"] != true {
				corrections = append(corrections, c)
			}
		}
	}
	queueSHA := state.LibrarySourceSHA
	if queueSHA == "" {
		queueSHA = lib.Digest(state.Queue)
	}
	facts := state.Facts
	var strategy any = state.ContentStrategy
	// Current saved tasks carry their complete facts and product boundary.
	// Their slot changes independently; global fact/queue digests would evict
	// unrelated products too. Legacy tasks without provenance stay conservative.
	if libraryScopedTasks(state) {
		facts, queueSHA, focus, strategy = "", "task-scoped-v1", nil, nil
	}
	// The run's TextPromptSHA is the last request's transport receipt. Repairs
	// clear it, normal requests fill it again. Use the resolved current contract
	// so real mother/model changes, rather than request timing, invalidate audio.
	return lib.Digest(struct {
		Tenant, Room, Plan, Style, Card, Wheel                      int64
		Task                                                        anchorcycle.Task
		Queue                                                       string
		StyleMD, Facts, Baseline, P0, OpeningLibrary, TextPromptSHA string
		Allowances                                                  []anchorcycle.P0Allowance
		Config                                                      anchorcycle.Config
		Corrections                                                 any
		Strategy, Focus, Angle, TextModel, ReviewModel              any
		Mothers                                                     map[string]string
	}{state.TenantID, state.RoomID, state.PlanID, state.StyleID, state.ScorecardID, state.WheelID, task, queueSHA, state.StyleMD, facts, state.BaselineJSON, state.P0PolicyMD, state.OpeningHookLibraryMD, contract, state.P0Allowances, config, corrections, strategy, focus, libraryAngle(plan), libraryModel(state.TextModel), libraryModel(state.ReviewModel), mothers}), nil
}
func (s *Server) libraryScope(state *anchorcycle.State, segment anchorcycle.Segment) (lib.Scope, error) {
	if state.Delivery == nil {
		return lib.Scope{}, errors.New("真实运行场次不存在")
	}
	plan := segment.Selected.AnglePlan
	if plan == nil && segment.Selected.ID == "" {
		plan = state.CurrentAnglePlan()
	}
	text, e := libraryTextSHA(state, segment.Task, plan)
	phase := segment.Phase
	if phase == "" {
		phase = state.Phase
	}
	opening := ""
	if phase == "opening" {
		opening = segment.OpeningVersion
		if opening == "" {
			opening = state.OpeningVersion()
		}
	}
	side := segment.Selected.FissionSide
	if segment.Selected.ID == "" {
		side = state.CurrentFissionSide()
	}
	// Phase and side belong to a frozen segment, never the producer's next task.
	// Keep them out of RoomContext, or every phase change invalidates the room.
	text = lib.Digest(struct{ Contract, Phase, Opening, Side string }{text, phase, opening, side})
	base, be := libraryTextSHA(state, anchorcycle.Task{}, nil)
	if e == nil {
		e = be
	}
	slot := segment.Task.SlotKey
	if slot == "" {
		slot = segment.Task.Key
	}
	if phase != "body" {
		slot = phase + ":" + opening + ":" + slot
	}
	// Independent side inventories must coexist. A side change is selection,
	// not a fact revision of the preceding side's slot.
	slot += ":side:" + lib.Digest(side)[:16]
	return lib.Scope{Realm: s.executionRealm, Tenant: state.TenantID, Plan: state.PlanID, Room: state.RoomID, Runtime: state.Delivery.RuntimeSessionID, Slot: slot, TextContext: text, VoiceContext: libraryVoiceSHA(state), RoomContext: base}, e
}
func libraryEligible(state *anchorcycle.State) bool {
	phase := state.Phase == "body" || state.Phase == "closing" || state.Phase == "opening" && !state.FirstLiveOpening()
	return state.Surface == "formal_live" && phase && state.SpeechReuseEnabled() && state.Task().ProductMode != "lecture" && state.Task().ProductMode != "auxiliary" && state.PreviewLimitMS == 0 && state.Config.TTS != nil && len(state.Attempts) == 0 && state.P0PendingRepair == nil && state.PriceHook == nil && state.SegmentDirective == "" && !libraryDeadlineSensitive(state)
}
func (s *Server) selectLibraryText(ctx context.Context, state *anchorcycle.State, b anchorstylescore.Baseline) (*lib.Entry, string, string, string) {
	if s.speechLibrary == nil || !libraryEligible(state) || !s.mainlineCacheReuseEnabled(ctx, state.TenantID) {
		return nil, "", "", ""
	}
	sc, e := s.libraryScope(state, anchorcycle.Segment{Phase: state.Phase, Task: state.Task()})
	if e != nil {
		state.Warn("话术库指纹未完成，正常生成：" + e.Error())
		return nil, "", "", ""
	}
	policy, e := s.libraryPolicy(ctx)
	if e != nil {
		state.Warn("话术库配置不可用，正常生成")
		return nil, "", "", ""
	}
	// Decide the mix once from adopted outputs. Do not multiply the 70% quota by
	// the old per-slot 30/60/70 probability a second time.
	rate := 0
	if state.ShouldReuse() {
		rate = 100
	}
	policy.OneRate, policy.TwoRate, policy.FullRate = rate, rate, rate
	decision := lib.Digest(struct {
		Run, Slot, Text, Voice string
		Position               int
	}{state.RunID, sc.Key(), sc.TextContext, sc.VoiceContext, state.TotalAccepted()})
	track := anchorcycle.ID()
	choice, e := s.speechLibrary.Select(ctx, sc, policy, decision, track)
	if e != nil {
		state.Warn("话术库读取失败，正常生成：" + e.Error())
		return nil, "", "", ""
	}
	log.Printf("speech library room=%d runtime=%d slot=%s decision=%s reason=%s", state.RoomID, sc.Runtime, sc.Slot, decision, choice.Reason)
	if choice.TrackID != "" {
		track = choice.TrackID
	}
	if rate == 0 && choice.Entry != nil {
		_ = s.speechLibrary.Cancel(ctx, decision)
		return nil, "", "", ""
	}
	if choice.Entry != nil {
		c := choice.Entry.Candidate
		// Only current approval receipts can authorize reuse, without model rescans.
		mode := lib.GenerationAccepted
		var e error
		if !state.Config.SkipReview {
			mode = lib.ReviewPassed
			var expected string
			expected, e = s.libraryReviewContext(state, c)
			if e == nil {
				state.FinalContentReviewRequired = true
				state.FinalContentReviewContextSHA = expected
			}
		}
		copy := c
		cleanCycleConsecutiveSpeech(state, &copy)
		if e != nil || choice.Entry.ValidateAdmission(sc, mode) != nil || !libraryHookCurrent(state, c) || !libraryTextIndependent(c.OutputText) || !state.FinalContentCandidateApproved(c) || state.CheckApprovedText(c, cycleVoiceText(c)) != nil || copy.OutputText != c.OutputText || anchorstylescore.Measure(c.OutputText, b).Chars > state.Config.MaxChars || c.Score.OverallScore < state.Config.Threshold || anchorcycle.InspectPrice(c.OutputText, state.Task()).Hook != "" {
			_ = s.speechLibrary.Cancel(ctx, decision)
			log.Printf("speech library room=%d decision=%s reason=receipt_or_recent_invalid", state.RoomID, decision)
			// The cancelled lease cannot publish the fresh fallback. Let the
			// approved fresh path reserve a new, active storage decision.
			return nil, "", "", ""
		}
	}
	return choice.Entry, decision, track, sc.TextContext
}
func (s *Server) libraryReviewContext(state *anchorcycle.State, c anchorcycle.Candidate) (string, error) {
	if state.ReviewModel == nil {
		return "", errors.New("审核模型缺失")
	}
	mother, _, e := loadReplaceablePromptAsset("MAINLINE_FINAL_CONTENT_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/mainline-final-content-review.md", "最终文字审核母版")
	if e != nil {
		return "", e
	}
	requirementModel, requirementMother := "", ""
	if len(state.EffectiveExpressionConstraints()) > 0 {
		if c.ExpressionAudit == nil {
			return "", errors.New("表达要求回执缺失")
		}
		m, _, e := loadReplaceablePromptAsset("ANCHOR_SPEECH_CONSTRAINT_TEXT_REVIEW_PATH", "configs/prompts/anchor-style/speech-constraint-text-review.md", "表达要求审核母版")
		if e != nil || c.ExpressionAudit.PromptSHA != derivedHash(m) {
			return "", errors.New("表达要求审核母版变化")
		}
		requirementModel, requirementMother = c.ExpressionAudit.Model, c.ExpressionAudit.PromptSHA
	}
	return finalMainlineReviewContext(state.FinalContentSourceSHA(), *state.ReviewModel, mother, requirementModel, requirementMother), nil
}

// A finally approved repair is as reusable as a first draft. This reserves
// storage only; it never selects old text during a repair or promise task.
func (s *Server) prepareApprovedLibraryText(ctx context.Context, state *anchorcycle.State, c *anchorcycle.Candidate) {
	phase := state.Phase == "body" || state.Phase == "closing" || state.Phase == "opening" && !state.FirstLiveOpening()
	if c.LibraryDecision != "" || s.speechLibrary == nil || state.Surface != "formal_live" || !phase || state.Config.TTS == nil || state.PriceHook != nil || state.SegmentDirective != "" || state.Task().ProductMode == "lecture" || state.Task().ProductMode == "auxiliary" || libraryDeadlineSensitive(state) || !libraryTextIndependent(c.OutputText) || !state.FinalContentCandidateApproved(*c) || !s.mainlineCacheReuseEnabled(ctx, state.TenantID) {
		return
	}
	if anchorcycle.InspectPrice(c.OutputText, state.Task()).Hook != "" {
		return
	}
	sc, e := s.libraryScope(state, anchorcycle.Segment{Phase: state.Phase, Task: state.Task(), Selected: *c})
	if e != nil {
		return
	}
	p, e := s.libraryPolicy(ctx)
	if e != nil {
		return
	}
	p.OneRate, p.TwoRate, p.FullRate = 0, 0, 0
	decision := lib.Digest(struct {
		Run, Scope, Text, Voice, Mode string
		Position                      int
	}{state.RunID, sc.Key(), sc.TextContext, sc.VoiceContext, "approved-fresh", state.TotalAccepted()})
	if _, e = s.speechLibrary.Select(ctx, sc, p, decision, c.ID); e != nil {
		state.Warn("已采纳正文未登记长期库，继续正常声音生成")
		return
	}
	c.LibraryDecision, c.LibraryTextSHA = decision, sc.TextContext
}
func finalMainlineReviewContext(source string, p agentgateway.SpeechModel, mother, requirementModel, requirementMother string) string {
	return anchorcycle.ProductDigest(struct{ Source, Model, Protocol, Endpoint, Mother, RequirementModel, RequirementMother string }{source, p.Model, p.Protocol, p.BaseURL, mother, requirementModel, requirementMother})
}

func (s *Server) libraryVoice(ctx context.Context, state *anchorcycle.State, segment anchorcycle.Segment) (*anchorcycle.VoiceAsset, error) {
	c := segment.Selected
	if s.speechLibrary == nil || c.LibraryEntryID == "" || !s.mainlineCacheReuseEnabled(ctx, state.TenantID) {
		return nil, nil
	}
	sc, e := s.libraryScope(state, segment)
	if e != nil || sc.TextContext != c.LibraryTextSHA {
		return nil, lib.ErrStale
	}
	a, e := s.speechLibrary.ClaimAudio(ctx, sc, c.LibraryDecision, c.LibraryEntryID)
	if e != nil || a == nil {
		return nil, e
	}
	oss, e := s.cycleCacheOSS()
	if e != nil {
		return nil, e
	}
	if a.Bucket != oss.Bucket() || a.Prefix != lib.ObjectPrefix(sc, a.ID) || a.Bundle.Text != cycleVoiceText(c) {
		return nil, errors.New("话术库音频范围不匹配")
	}
	// A signed URL alone doesn't prove the object exists. A read confirms it; if
	// missing, keep the approved text and regenerate only its TTS bundle.
	check, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if head, ok := oss.(interface {
		Exists(context.Context, string) (bool, error)
	}); ok {
		exists, err := head.Exists(check, a.Prefix+"audio.wav")
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, errors.New("话术库声音文件不存在，仅重新合成TTS")
		}
	} else {
		stream, err := oss.Open(check, a.Prefix+"audio.wav")
		if err != nil {
			return nil, err
		}
		stream.Close()
	}
	a.Bundle.AudioURL, e = oss.SignedURL(ctx, a.Prefix+"audio.wav", 24*time.Hour)
	if e != nil {
		return nil, e
	}
	if e = validateLiveAgentVariantTimeline(a.Bundle); e != nil {
		return nil, e
	}
	if e = validateLiveAgentVariantSchedulingAssets(a.Bundle); e != nil {
		return nil, e
	}
	return &anchorcycle.VoiceAsset{Status: "ready", CacheID: a.ID, CacheHit: true, Bundle: a.Bundle}, nil
}
func (s *Server) archiveLibraryVoice(ctx context.Context, state *anchorcycle.State, seg anchorcycle.Segment, raw []byte, timeline []model.LiveAgentPlanTimelineSegment, duration int64) (*anchorcycle.VoiceAsset, *lib.Audio, error) {
	c := seg.Selected
	sc, e := s.libraryScope(state, seg)
	if e != nil || sc.TextContext != c.LibraryTextSHA {
		// 诊断：打印两边指纹不一致时的关键输入，定位文字阶段与语音阶段 scope 差异。
		log.Printf("library scope stale room=%d candidate=%s want=%s got=%s err=%v task_key=%s seg_task_key=%s slotkey=%s seg_slotkey=%s angleplan_nil=%v seg_angleplan_nil=%v",
			state.RoomID, c.ID, c.LibraryTextSHA, sc.TextContext, e,
			state.Task().Key, seg.Task.Key, state.Task().SlotKey, seg.Task.SlotKey,
			state.CurrentAnglePlan() == nil, seg.Selected.AnglePlan == nil)
		return nil, nil, lib.ErrStale
	}
	if e = state.CheckApprovedText(c, cycleVoiceText(c)); e != nil {
		return nil, nil, e
	}
	oss, e := s.cycleCacheOSS()
	if e != nil {
		return nil, nil, e
	}
	entry := c.LibraryEntryID
	if entry == "" {
		entry = anchorcycle.ID()
	}
	a := lib.Audio{ID: anchorcycle.ID(), EntryID: entry, Bucket: oss.Bucket(), VoiceContext: sc.VoiceContext}
	a.Prefix = lib.ObjectPrefix(sc, a.ID)
	safe := buildFullShowSafePoints(timeline)
	srt := liveAgentTimelineSRT(timeline)
	keys := lib.ObjectKeys(a.Prefix)
	manifest := map[string]any{"version": "speech-library-bundle-v1", "library_audio_id": a.ID, "storage_driver": "oss", "storage_bucket": a.Bucket, "audio_duration_ms": duration, "audio_object_key": keys[0], "timeline_precision": "segment_pcm_exact", "text_sha": sc.TextContext, "voice_sha": sc.VoiceContext, "voice_id": state.Config.TTS.VoiceID, "tts_model": state.Config.TTS.Model, "voice_rate": state.Config.TTS.Rate}
	a.Bundle = model.LiveAgentPlanVersionVariant{VariantKey: "library-" + a.ID, Text: cycleVoiceText(c), AudioDurationMS: duration, Timeline: timeline, SRT: srt, SafePoints: safe, AssetManifest: manifest, VoiceIdentityKey: state.Config.TTS.VoiceID}
	if e = validateLiveAgentVariantTimeline(a.Bundle); e != nil {
		return nil, nil, e
	}
	if e = validateLiveAgentVariantSchedulingAssets(a.Bundle); e != nil {
		return nil, nil, e
	}
	if e = s.speechLibrary.ReserveAudio(ctx, sc, c.LibraryDecision, c.ID, a); e != nil {
		return nil, nil, e
	}
	j := func(v any) []byte { b, _ := json.Marshal(v); return b }
	payloads := [][]byte{raw, []byte(srt), j(map[string]any{"audio_duration_ms": duration, "sentences": timeline, "safe_points": safe}), j(map[string]any{"safe_points": safe}), j(manifest)}
	for i, key := range keys {
		mime := "application/json"
		if i == 0 {
			mime = "audio/wav"
		}
		if i == 1 {
			mime = "application/x-subrip; charset=utf-8"
		}
		if private, ok := oss.(interface {
			PutPrivate(context.Context, string, io.Reader, string) error
		}); ok {
			e = private.PutPrivate(ctx, key, bytes.NewReader(payloads[i]), mime)
		} else {
			e = oss.Put(ctx, key, bytes.NewReader(payloads[i]), mime)
		}
		if e != nil {
			return nil, nil, e
		}
	}
	a.Bundle.AudioURL, e = oss.SignedURL(ctx, keys[0], 24*time.Hour)
	if e != nil {
		return nil, nil, e
	}
	return &anchorcycle.VoiceAsset{Status: "ready", CacheID: a.ID, Bundle: a.Bundle}, &a, nil
}
func (s *Server) publishLibraryVoice(ctx context.Context, state *anchorcycle.State, seg anchorcycle.Segment, a *lib.Audio) {
	if a == nil {
		return
	}
	sc, e := s.libraryScope(state, seg)
	if e != nil {
		state.Warn("话术库入库失败，当前声音继续播放：" + e.Error())
		return
	}
	p, e := s.libraryPolicy(ctx)
	if e != nil {
		state.Warn("话术库入库失败，当前声音继续播放：配置不可用")
		return
	}
	entry, e := s.libraryAdmittedEntry(state, seg, sc, a.EntryID)
	if e != nil {
		state.Warn("话术库入库失败，当前声音继续播放：" + e.Error())
		log.Printf("speech library publish room=%d candidate=%s error=%v", state.RoomID, seg.Selected.ID, e)
		return
	}
	c := seg.Selected
	c.Prompt = ""
	c.RawText = ""
	c.EventIDs = nil
	c.LibraryEntryID = ""
	c.LibraryDecision = ""
	c.LibraryTextSHA = ""
	c.CacheID = ""
	if c.P0Report != nil {
		q := *c.P0Report
		c.P0Report = &q
	}
	entry.Candidate = c
	e = s.speechLibrary.Publish(ctx, sc, p, seg.Selected.LibraryDecision, seg.Selected.ID, entry, *a)
	if e != nil {
		state.Warn("话术库入库失败，当前声音继续播放：" + e.Error())
		log.Printf("speech library publish room=%d candidate=%s error=%v", state.RoomID, seg.Selected.ID, e)
	} else {
		log.Printf("speech library publish room=%d runtime=%d candidate=%s entry=%s audio=%s mode=%s result=ready", state.RoomID, sc.Runtime, seg.Selected.ID, entry.ID, a.ID, entry.Admission.Mode)
	}
}

func (s *Server) observeLibraryPlayback(ctx context.Context, tenant, room, runtime int64, p []anchorcycle.AnglePlaybackReceipt, done []anchorcycle.PlaybackReceipt) {
	if s.speechLibrary == nil {
		return
	}
	for _, r := range p {
		at, e := time.Parse(time.RFC3339Nano, r.StartedAt)
		if e == nil {
			if e = s.speechLibrary.MarkPlayed(ctx, tenant, room, runtime, r.TrackID, at); e != nil {
				log.Printf("speech library playback accounting room=%d: %v", room, e)
			}
		}
	}
	if len(p) > 0 {
		last := p[len(p)-1]
		at, e := time.Parse(time.RFC3339Nano, last.StartedAt)
		if e == nil {
			if repo, ok := s.speechLibrary.(interface {
				ReleaseLibraryBefore(context.Context, int64, int64, int64, string, time.Time) error
			}); ok {
				_ = repo.ReleaseLibraryBefore(ctx, tenant, room, runtime, last.TrackID, at)
			}
		}
	}
	if repo, ok := s.speechLibrary.(interface {
		ReleaseLibraryTrack(context.Context, int64, int64, int64, string) error
	}); ok {
		for _, r := range done {
			if r.Complete {
				_ = repo.ReleaseLibraryTrack(ctx, tenant, room, runtime, r.TrackID)
			}
		}
	}
}

// This read supplies existing playback/download references, not cache selection.
func (s *Server) libraryTrackedAudio(ctx context.Context, tenant, room, runtime int64, track string) (*lib.Audio, error) {
	repo, ok := s.speechLibrary.(interface {
		FindLibraryAudio(context.Context, int64, int64, int64, string) (*lib.Audio, error)
	})
	if !ok {
		return nil, nil
	}
	a, e := repo.FindLibraryAudio(ctx, tenant, room, runtime, track)
	if e != nil || a == nil {
		return a, e
	}
	sc := lib.Scope{Realm: s.executionRealm, Tenant: tenant, Room: room}
	// Plan ID comes from the program-owned prefix, and is checked by exact shape.
	shape := regexp.MustCompile(fmt.Sprintf(`^mainline-speech-library/v1/%s/t%d/r%d/p[1-9][0-9]*/%s/$`, lib.Digest(sc.Realm)[:16], tenant, room, a.ID))
	oss, e := s.cycleCacheOSS()
	if e != nil {
		return nil, e
	}
	if a.Bucket != oss.Bucket() || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(a.ID) || !shape.MatchString(a.Prefix) {
		return nil, errors.New("已播声音引用范围不匹配")
	}
	a.Bundle.AudioURL, e = oss.SignedURL(ctx, a.Prefix+"audio.wav", 24*time.Hour)
	return a, e
}
func (s *Server) RunSpeechLibraryCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if s.speechLibrary != nil {
			job, cancel := context.WithTimeout(ctx, 30*time.Second)
			e := s.cleanupSpeechLibrary(job)
			cancel()
			if e != nil && ctx.Err() == nil {
				log.Printf("speech library cleanup: %v", e)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Server) cleanupSpeechLibrary(ctx context.Context) error {
	oss, e := s.cycleCacheOSS()
	if e != nil {
		return e
	}
	items, e := s.speechLibrary.Due(ctx, 20)
	if e != nil {
		return e
	}
	realm := lib.Digest(s.executionRealm)[:16]
	for _, a := range items {
		if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(a.ID) || a.Bucket != oss.Bucket() || !regexp.MustCompile(`^mainline-speech-library/v1/`+realm+`/t[1-9][0-9]*/r[1-9][0-9]*/p[1-9][0-9]*/`+a.ID+`/$`).MatchString(a.Prefix) {
			return fmt.Errorf("拒绝清理范围不明的话术库声音")
		}
		for _, key := range lib.ObjectKeys(a.Prefix) {
			if e = oss.Delete(ctx, key); e != nil {
				return e
			}
		}
		if e = s.speechLibrary.ForgetAudio(ctx, a.ID); e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) speechLibraryPolicyGet(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	p, e := s.store.SpeechLibraryPolicy(r.Context())
	if e != nil {
		writeError(w, 503, "读取话术库配置失败")
		return
	}
	stats, e := s.speechLibrary.Statistics(r.Context())
	if e != nil {
		writeError(w, 503, "读取话术库统计失败")
		return
	}
	writeJSON(w, 200, map[string]any{"policy": p, "stats": stats, "revision": lib.Digest(p)})
}
func (s *Server) speechLibraryPolicyPut(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input struct {
		Policy   lib.Policy `json:"policy"`
		Expected string     `json:"expected_revision"`
	}
	if e := readJSON(w, r, &input); e != nil {
		writeError(w, 400, "话术库设置格式错误")
		return
	}
	p := input.Policy
	if e := p.Validate(); e != nil {
		writeError(w, 400, e.Error())
		return
	}
	if e := s.store.SaveSpeechLibraryPolicy(r.Context(), p, actor.UserID, input.Expected); e != nil {
		if errors.Is(e, lib.ErrStale) {
			writeError(w, 409, "话术库设置已变化，请刷新后重新修改")
		} else {
			writeError(w, 503, "保存话术库设置失败")
		}
		return
	}
	s.speechLibraryPolicyGet(w, r)
}
