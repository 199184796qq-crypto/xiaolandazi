package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
	lib "livecompanion/management/internal/speechlibrary"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

type libraryMemory struct {
	entry                       *lib.Entry
	audio                       *lib.Audio
	claims, reserves, publishes int
	missing                     bool
}

func (m *libraryMemory) Select(_ context.Context, _ lib.Scope, _ lib.Policy, _ string, track string) (lib.Choice, error) {
	return lib.Choice{Entry: m.entry, TrackID: track, Reason: "fixture"}, nil
}
func (m *libraryMemory) Cancel(context.Context, string) error { return nil }
func (m *libraryMemory) ReserveAudio(_ context.Context, _ lib.Scope, _ string, _ string, a lib.Audio) error {
	m.reserves++
	return nil
}
func (m *libraryMemory) Publish(_ context.Context, _ lib.Scope, _ lib.Policy, _ string, _ string, e lib.Entry, a lib.Audio) error {
	m.entry = &e
	m.audio = &a
	m.publishes++
	return nil
}
func (m *libraryMemory) ClaimAudio(_ context.Context, sc lib.Scope, _, entry string) (*lib.Audio, error) {
	m.claims++
	if m.missing {
		return nil, errors.New("missing audio")
	}
	if m.audio != nil && m.audio.EntryID == entry && m.audio.VoiceContext == sc.VoiceContext {
		return m.audio, nil
	}
	return nil, nil
}
func (m *libraryMemory) MarkPlayed(context.Context, int64, int64, int64, string, time.Time) error {
	return nil
}
func (m *libraryMemory) ReleaseRuntime(context.Context, int64, int64, int64) error { return nil }
func (m *libraryMemory) Due(context.Context, int) ([]lib.Audio, error)             { return nil, nil }
func (m *libraryMemory) ForgetAudio(context.Context, string) error                 { return nil }
func (m *libraryMemory) Statistics(context.Context) (lib.Stats, error)             { return lib.Stats{}, nil }

func libraryFixture(t *testing.T) (*Server, *anchorcycle.State, anchorcycle.Segment, *libraryMemory) {
	t.Helper()
	s, state, segment, _ := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("ETag", "fixture"); w.WriteHeader(200) })
	state.Surface = "formal_live"
	state.RoomID = 15
	state.Phase = "body"
	state.Attempts = nil
	state.BaselineJSON = textTestBaseline()
	state.TextModel = &agentgateway.SpeechModel{ID: "account-a", Model: "qwen3.7-flash", Protocol: "openai_chat", BaseURL: "https://example.test/v1", Enabled: true}
	q := *state.TextModel
	state.ReviewModel = &q
	state.Delivery = &anchorcycle.MainlineDelivery{RuntimeSessionID: 1}
	s.executionRealm = "fixture"
	segment.Task = state.Task()
	segment.Selected.Text = "哥哥姐姐们，这款鸭子开袋就能吃。包装撕开，摆盘就可以上桌。"
	segment.Selected.OutputText = segment.Selected.Text
	if e := state.CheckP0Candidate(&segment.Selected); e != nil {
		t.Fatal(e)
	}
	sha, e := s.libraryReviewContext(state, segment.Selected)
	if e != nil {
		t.Fatal(e)
	}
	state.FinalContentReviewRequired = true
	state.FinalContentReviewContextSHA = sha
	segment.Selected.FinalContentAudit = &anchorcycle.FinalContentAudit{Decision: "通过", TextSHA: anchorcycle.ProductDigest(cycleVoiceText(segment.Selected)), ScopeSHA: state.FinalContentSourceSHA(), ContextSHA: sha}
	sc, e := s.libraryScope(state, segment)
	if e != nil {
		t.Fatal(e)
	}
	segment.Selected.LibraryDecision = lib.Digest("decision")
	segment.Selected.LibraryTextSHA = sc.TextContext
	m := &libraryMemory{}
	s.speechLibrary = m
	s.generationCachePolicy = &fixtureCacheReusePolicy{enabled: true}
	return s, state, segment, m
}
func cloneLibraryState(t *testing.T, s *anchorcycle.State) *anchorcycle.State {
	t.Helper()
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	var out anchorcycle.State
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	return &out
}
func TestSpeechLibraryContentFingerprintChangesAndVoiceOnly(t *testing.T) {
	s, state, seg, _ := libraryFixture(t)
	base, e := s.libraryScope(state, seg)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name string
		edit func(*anchorcycle.State, *anchorcycle.Segment)
	}{
		{"room", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.RoomID++ }},
		{"tenant", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.TenantID++ }},
		{"plan", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.PlanID++ }},
		{"fact", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.Facts += "价格已更新" }},
		{"style", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.StyleMD += "使用自然称呼" }},
		{"baseline", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.BaselineJSON += " " }},
		{"point", func(_ *anchorcycle.State, s *anchorcycle.Segment) { s.Task.SellingPoint += "新卖点" }},
		{"angle", func(_ *anchorcycle.State, s *anchorcycle.Segment) { s.Task.Angle += "新角度" }},
		{"product", func(_ *anchorcycle.State, s *anchorcycle.Segment) { s.Task.FocusLink = "2" }},
		{"benefit-and-condition", func(_ *anchorcycle.State, s *anchorcycle.Segment) {
			s.Task.FactScope = &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{Data: "仅组合装赠送，单只不赠送"}}}
		}},
		{"constraints", func(s *anchorcycle.State, _ *anchorcycle.Segment) {
			s.LatestExpressionConstraints = []map[string]any{{"要求": "不要拿早餐举例"}}
		}},
		{"model", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.TextModel.Model = "qwen3.8-flash" }},
		{"rule", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.P0PolicyMD += "\n " }},
		{"generation-settings", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.Config.MaxChars++ }},
		{"queue-angle", func(s *anchorcycle.State, _ *anchorcycle.Segment) { s.Queue[0].Angle += "已修订" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := cloneLibraryState(t, state)
			segment := seg
			tc.edit(copy, &segment)
			sc, e := s.libraryScope(copy, segment)
			if e != nil {
				t.Fatal(e)
			}
			if sc.TextContext == base.TextContext {
				t.Fatal("changed content did not invalidate prose")
			}
		})
	}
	for _, tc := range []struct {
		name string
		edit func(*anchorcycle.State)
	}{
		{"voice", func(s *anchorcycle.State) { s.Config.TTS.VoiceID = "another" }},
		{"tts-model", func(s *anchorcycle.State) { s.Config.TTS.Model = "another" }},
		{"rate", func(s *anchorcycle.State) { s.Config.TTS.Rate = 1.2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := cloneLibraryState(t, state)
			tc.edit(copy)
			sc, e := s.libraryScope(copy, seg)
			if e != nil || sc.TextContext != base.TextContext || sc.VoiceContext == base.VoiceContext {
				t.Fatal("voice-only change invalidated text or retained audio", e)
			}
		})
	}
	// Session/run counters, buffer sizes and account keys don't invalidate content.
	copy := cloneLibraryState(t, state)
	copy.RunID = "next-run"
	copy.Delivery.RuntimeSessionID++
	copy.Config.TTS.BufferSeconds++
	copy.TextModel.ID = "account-b"
	copy.TextModel.APIKey = "fixture-another"
	copy.TextModel.HasAPIKey = true
	copy.ReviewModel.ID = "account-b"
	sc, e := s.libraryScope(copy, seg)
	if e != nil || sc.TextContext != base.TextContext || sc.VoiceContext != base.VoiceContext {
		t.Fatal("transport or playback state polluted the content identity", e)
	}
}
func TestSpeechLibraryFullAudioBundleReuseAndVoiceRegeneration(t *testing.T) {
	s, state, seg, m := libraryFixture(t)
	timeline := []model.LiveAgentPlanTimelineSegment{{SegmentID: "test-adopted-1", Index: 1, StartMS: 0, EndMS: 1800, Text: "哥哥姐姐们，这款鸭子开袋就能吃。", SafeCut: true}, {SegmentID: "test-adopted-2", Index: 2, StartMS: 1800, EndMS: 3400, Text: "包装撕开，摆盘就可以上桌。", SafeCut: true}}
	voice, a, e := s.archiveLibraryVoice(context.Background(), state, seg, []byte("fixture-wave"), timeline, 3400)
	if e != nil {
		t.Fatal(e)
	}
	if voice.Bundle.SRT == "" || len(voice.Bundle.SafePoints) != 2 || m.reserves != 1 {
		t.Fatal("incomplete subtitle/timeline/safe-point bundle", voice)
	}
	s.publishLibraryVoice(context.Background(), state, seg, a)
	if m.publishes != 1 {
		t.Fatal("complete approved bundle not published")
	}
	seg.Selected.LibraryEntryID = a.EntryID
	hit, e := s.libraryVoice(context.Background(), state, seg)
	if e != nil || hit == nil || !hit.CacheHit || !reflect.DeepEqual(hit.Bundle.SafePoints, voice.Bundle.SafePoints) || hit.Bundle.Text != seg.Selected.OutputText {
		t.Fatal("full unchanged audio not reused", hit, e)
	}
	seg.Voice = hit
	state.Accepted = []anchorcycle.Segment{seg}
	track, e := liveCycleMainlineTrack(state, seg)
	if e != nil || len(track.SafePoints) != 2 || track.SafePoints[0].CutMS != 1800 || track.SafePoints[1].CutMS != 3400 {
		t.Fatal("Core handoff lost sentence endings", track, e)
	}
	oldText := seg.Selected.OutputText
	state.Config.TTS.VoiceID = "new-voice"
	hit, e = s.libraryVoice(context.Background(), state, seg)
	if e != nil || hit != nil || seg.Selected.OutputText != oldText {
		t.Fatal("voice change reused old audio or rewrote prose", hit, e)
	}
	regenerated, _, e := s.archiveLibraryVoice(context.Background(), state, seg, []byte("new-voice-wave"), timeline, 3400)
	if e != nil || regenerated.Bundle.Text != oldText {
		t.Fatal("voice-only TTS regeneration failed", e)
	}
	m.missing = true
	state.Config.TTS.VoiceID = "fixture-voice"
	if _, e = s.libraryVoice(context.Background(), state, seg); e == nil {
		t.Fatal("missing audio was blindly enqueued")
	}
}
func TestSpeechLibraryOpeningPreviewAndContextualTextStayFresh(t *testing.T) {
	_, s, _, _ := libraryFixture(t)
	if !libraryEligible(s) {
		t.Fatal("body unexpectedly ineligible")
	}
	s.Phase = "opening"
	if libraryEligible(s) {
		t.Fatal("opening reused")
	}
	s.Phase = "body"
	s.Surface = "generation_preview"
	if libraryEligible(s) {
		t.Fatal("preview polluted formal library")
	}
	for _, text := range []string{"刚才那位朋友问过了，接着说。", "下一段接下来再讲保存。"} {
		if libraryTextIndependent(text) {
			t.Fatal("context-dependent text entered cross-session library")
		}
	}
	if !libraryTextIndependent("开袋即食，和家人一起分享。") {
		t.Fatal("normal daily scene was excluded")
	}
}
func TestSpeechLibraryAPIRequiresPermission(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, h := range []http.HandlerFunc{s.speechLibraryPolicyGet, s.speechLibraryPolicyPut} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest("PUT", "/api/v1/system/speech-library-policy", nil))
		if w.Code != 401 {
			t.Fatal("unauthenticated cache settings access", w.Code)
		}
	}
}
func TestSpeechLibraryChangedDirectionCannotReuseOldCard(t *testing.T) {
	_, state, _, _ := libraryFixture(t)
	state.AnglePlanMotherSHA = "fixture"
	hook := anchorcycle.AngleHook{ID: "program-card", Title: "方便食用", Entry: "从下班回家切入", Action: "解释开袋即食"}
	plan := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{hook}}
	state.AnglePlans = map[string]*anchorcycle.AnglePlan{state.AnglePlanKey(): plan}
	c := anchorcycle.Candidate{AngleHook: &hook}
	if !libraryHookCurrent(state, c) {
		t.Fatal("unchanged approved direction not reusable")
	}
	plan.Cards = append(plan.Cards, anchorcycle.AngleHook{ID: "new-card", Title: "新增其他方向"})
	if !libraryHookCurrent(state, c) {
		t.Fatal("supplementing another direction invalidated unchanged card")
	}
	plan.Cards[0].Entry = "已经改为早餐切入"
	if libraryHookCurrent(state, c) {
		t.Fatal("edited direction reused old text")
	}
}
func TestSpeechLibraryHitAdvancesThroughRealAdoptionWithoutModel(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", "0")
	s, state, seg, m := libraryFixture(t)
	m.entry = &lib.Entry{ID: "0123456789abcdef0123456789abcdef", Candidate: seg.Selected}
	state.LibrarySourceSHA = anchorcycle.ProductDigest(state.Queue)
	repo := &supplyRepository{state: state}
	s.anchorCycles = repo
	// No DB/model transport exists in this fixture. Any model invocation would
	// fail; reuse must adopt the unchanged, scoped completed approval receipt.
	out, e := s.advanceAnchorCycle(context.Background(), state.TenantID, state.PlanID, state.RoomID, state.RunID, state.Revision)
	if e != nil || len(out.Accepted) != 1 || out.Accepted[0].Selected.ScoreScope != "speech_library_reused" || out.Accepted[0].Selected.OutputText != seg.Selected.OutputText {
		t.Fatal("cache hit failed real adoption", e, out)
	}
	if out.CheckApprovedText(out.Accepted[0].Selected, seg.Selected.OutputText) != nil {
		t.Fatal("cache adoption invalidated approval")
	}
}

func TestSpeechLibraryRandomExamplesCannotMutateSavedFacts(t *testing.T) {
	facts := []anchorcycle.HookFact{{Data: "开袋即食", ExpressionGuidance: []map[string]string{
		{"要求": "禁说", "内容": "不能拿早餐举例"},
		{"要求": "多个正向举例", "内容": "A"},
		{"要求": "多个正向举例", "内容": "B"},
		{"要求": "多个正向举例", "内容": "C"},
	}}}
	before := lib.Digest(facts)
	orders := map[string]bool{}
	for i := 0; i < 30; i++ {
		content, guidance := generationMaterialViews(facts)
		if _, ok := content[0]["表达指导（不作为商品事实）"]; ok {
			t.Fatal("writing examples leaked into product facts")
		}
		rules := guidance[0]["负向约束及正向举例（不作为新增事实，不直接照念指导）"].([]map[string]string)
		if rules[0]["内容"] != "不能拿早餐举例" || lib.Digest(facts) != before {
			t.Fatal("randomized view modified saved facts or negative requirements")
		}
		orders[lib.Digest(rules)] = true
	}
	if len(orders) < 2 {
		t.Fatal("model views no longer shuffle examples")
	}
}

func TestSpeechLibraryDeadlineOffersStayFresh(t *testing.T) {
	_, state, _, _ := libraryFixture(t)
	state.Facts = "现做现发，味道有一点香。"
	if !libraryEligible(state) {
		t.Fatal("ordinary descriptions mistaken for a deadline")
	}
	for _, fact := range []string{"活动截至今晚八点", "限时福利", "2026年10月15日活动", "下午三点结束"} {
		state.Facts = fact
		if libraryEligible(state) {
			t.Fatal("expiring offer reused", fact)
		}
	}
}
