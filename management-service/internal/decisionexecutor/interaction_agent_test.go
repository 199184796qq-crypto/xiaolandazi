package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

func interactionAnswerFixture(angle, reply string) string {
	raw, _ := json.Marshal(map[string]any{"reply": reply, "answer_outline": InteractionAnswerOutline{Purpose: "确认保存方式", Angle: angle, Aspects: []string{"是否需要冷藏", "包装对保存的影响"}, Avoid: []string{"复热吃法", "未经确认的保质天数"}}})
	return string(raw)
}

type interactionAgentTestStore struct {
	*fakeStore
	reads []string
}

func (s *interactionAgentTestStore) ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error) {
	return []model.AgentMemoryItem{
		{MemoryType: model.AgentMemoryTypeSemantic, Status: "active", CurrentVersion: &model.AgentMemoryVersion{ContentText: "已确认：需要冷藏。"}},
		{MemoryType: model.AgentMemoryTypeStyle, Status: "active", CurrentVersion: &model.AgentMemoryVersion{ContentText: "风格哨兵：开头必须说老乡们。"}},
	}, nil
}

func (s *interactionAgentTestStore) CurrentInteractionFacts(_ context.Context, tenant, room, plan int64) (string, error) {
	s.reads = append(s.reads, "current_facts")
	return "- [保存方式] 需要冷藏；禁说=常温无限期保存", nil
}

func TestInteractionAgentSharedContractAndSimulationPresentationBoundary(t *testing.T) {
	s := &interactionAgentTestStore{fakeStore: readyVoiceStore()}
	a := &fakeAgent{responses: []string{interactionAnswerFixture("保存环境", "需要冷藏，收到后放冰箱。"), interactionAnswerFixture("包装与保存条件", "要放冰箱，包装不同也要按已说明的条件保存。")}}
	core, tts := &fakeCore{}, &fakeTTS{}
	w := New(s, core, a, tts)
	ctx := context.Background()
	session := model.LiveRuntimeSession{TenantID: 7, RoomID: 11}
	live := decisionItem{MissionKind: "reply_question", SampleQuestions: []string{"放冰箱不"}}
	text, err := w.generateDecisionTextMutable(ctx, session, &live)
	if err != nil || text != "需要冷藏，收到后放冰箱。" || live.AnswerOutline == nil {
		t.Fatal(text, live.AnswerOutline, err)
	}
	if live.ContentAngle != "保存环境" || live.SelectedContentPlan.AnswerOutline != live.AnswerOutline {
		t.Fatal("business plan not passed to live execution", live)
	}
	recent := []InteractionRecentAnswer{{Question: "放冰箱不", Purpose: live.AnswerOutline.Purpose, Angle: live.ContentAngle, Reply: text}}
	result, err := w.SimulateInteraction(ctx, 7, 11, "放冰箱不", "不展开吃法", recent)
	if err != nil || result.AnswerOutline == nil || result.AnswerOutline.Angle != "包装与保存条件" {
		t.Fatal(result, err)
	}
	if len(a.requests) != 2 || len(a.planningRequests) != 2 {
		t.Fatal("each entry must use one business plan and one answer task", len(a.requests), len(a.planningRequests))
	}
	for _, request := range a.requests {
		if !strings.Contains(request.Messages[0].Content, "互动智能体：按目的与角度回答") || !strings.Contains(request.Messages[0].Content, "不是整段逐字照读") {
			t.Fatal("different business protocol", request)
		}
	}
	livePrompt, testPrompt := a.requests[0].Messages[1].Content, a.planningRequests[1].Messages[1].Content
	if !strings.Contains(livePrompt, "风格哨兵") || strings.Contains(testPrompt, "风格哨兵") {
		t.Fatal("simulation inherited style", livePrompt, testPrompt)
	}
	if !strings.Contains(testPrompt, "包装对保存") && !strings.Contains(testPrompt, "保存环境") {
		t.Fatal("recent business angle lost", testPrompt)
	}
	if !strings.Contains(testPrompt, "不展开吃法") || !strings.Contains(testPrompt, "常温无限期保存") {
		t.Fatal("correction or fact boundary lost", testPrompt)
	}
	if strings.Contains(testPrompt, "回归策略") || strings.Contains(testPrompt, "打断策略") || strings.Contains(result.Reply, "answer_outline") || tts.calls != 0 || core.dispatches != 0 {
		t.Fatal("simulation leaked delivery or caused playback", result)
	}
	if len(s.reads) < 1 {
		t.Fatal("test did not read selected plan facts")
	}
}

func TestInteractionAnswerMetadataFailureRetainsOnlyValidSpeech(t *testing.T) {
	valid := interactionAnswerFixture("保存环境", "需要冷藏。")
	for _, raw := range []string{
		`{"reply":"需要冷藏。"}`,
		strings.Replace(valid, `"purpose":`, `"fact_ids":[],"purpose":`, 1),
		strings.Replace(valid, `"aspects":["是否需要冷藏","包装对保存的影响"]`, `"aspects":null`, 1),
	} {
		reply, outline, warning, err := decodeInteractionAnswer(raw)
		if err != nil || reply != "需要冷藏。" || outline != nil || warning == "" {
			t.Fatal("invalid advisory vetoed speech or became a fake plan", reply, outline, warning, err)
		}
	}
	for _, raw := range []string{`{"reply":null}`, valid + `{}`, `{"reply":"未闭合`} {
		if _, _, _, err := decodeInteractionAnswer(raw); err == nil {
			t.Fatal("invalid reply transport accepted", raw)
		}
	}
	if reply, outline, warning, err := decodeInteractionAnswer("旧协议正文。"); err != nil || reply != "旧协议正文。" || outline != nil || warning == "" {
		t.Fatal(reply, outline, warning, err)
	}
}

func TestInteractionAgentKeepsAdvisoryReviewAndSpeechSeparate(t *testing.T) {
	s := &learningStore{fakeStore: *readyVoiceStore()}
	var fields map[string]any
	_ = json.Unmarshal([]byte(interactionAnswerFixture("保存环境", "需要冷藏。")), &fields)
	fields["needs_review"] = true
	fields["review"] = map[string]any{"kind": "missing_fact", "context": "保存条件", "reason": "未明确开袋条件", "needed_facts": []string{"开袋后的保存条件"}}
	raw, _ := json.Marshal(fields)
	a := &fakeAgent{responses: []string{string(raw)}}
	w := New(s, &fakeCore{}, a, &fakeTTS{})
	item := decisionItem{ID: "question", MissionKind: "reply_question", SampleQuestions: []string{"放冰箱不"}}
	text, err := w.generateDecisionTextMutable(context.Background(), model.LiveRuntimeSession{ID: 1, ExternalID: "live", TenantID: 7, RoomID: 11}, &item)
	if err != nil || text != "需要冷藏。" || item.AnswerOutline == nil || len(s.observations) != 1 {
		t.Fatal(text, err, item, s.observations)
	}
	if s.observations[0].Review.RawModelOutput != string(raw) || s.observations[0].Review.Reason != "未明确开袋条件" {
		t.Fatal("original output or real advisory lost", s.observations)
	}
}

func TestInteractionRecentAnswersAreBounded(t *testing.T) {
	if ValidateInteractionRecentAnswers(make([]InteractionRecentAnswer, 9)) == nil || ValidateInteractionRecentAnswers([]InteractionRecentAnswer{{Question: ""}}) == nil || ValidateInteractionRecentAnswers([]InteractionRecentAnswer{{Question: "问", Reply: strings.Repeat("字", 1001)}}) == nil {
		t.Fatal("unbounded or empty history accepted")
	}
	if err := ValidateInteractionRecentAnswers([]InteractionRecentAnswer{{Question: "放冰箱不", Purpose: "确认保存", Angle: "包装条件", Reply: "需要冷藏。"}}); err != nil {
		t.Fatal(err)
	}
}

func TestInteractionBusinessPlanRejectsModelOwnedRelations(t *testing.T) {
	valid := `{"answer_outline":{"purpose":"确认保存方式","angle":"存放环境","aspects":["是否冷藏"],"avoid":["复热吃法"]},"basis":["需要冷藏。"],"requirements":[]}`
	if _, err := decodeInteractionBusinessPlan(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{strings.Replace(valid, `"basis":`, `"fact_ids":["F001"],"basis":`, 1), strings.Replace(valid, `"requirements":[]`, `"requirements":null`, 1), strings.Replace(valid, `"aspects":["是否冷藏"]`, `"aspects":[]`, 1)} {
		if _, err := decodeInteractionBusinessPlan(raw); err == nil {
			t.Fatal("invalid planner contract accepted", raw)
		}
	}
}

func TestInteractionBusinessPlanKeepsValidPrimaryWhenOptionalCandidateIsMalformed(t *testing.T) {
	// Replay the actual provider's invalid optional placeholder; it must not
	// discard the valid business plan or pretend the optional task completed.
	raw := `{"answer_outline":{"purpose":"确认保存方式","angle":"存放环境","aspects":["是否冷藏"],"avoid":["复热吃法"]},"basis":["收到后冷藏。"],"requirements":[],"alternatives":[{"answer_outline":{"useful":false,"data":null}}]}`
	plan, err := decodeInteractionBusinessPlan(raw)
	if err != nil || plan == nil || plan.Outline.Angle != "存放环境" || len(plan.Basis) != 1 || len(plan.Alternatives) != 0 || plan.FormatWarning == "" {
		t.Fatal("optional format error discarded valid plan or hid incomplete output", plan, err)
	}
}

func TestInteractionPlanChoosesOtherSupportedAngle(t *testing.T) {
	plan := &interactionBusinessPlan{Outline: InteractionAnswerOutline{Purpose: "确认保存", Angle: "存放环境"}, Basis: []string{"收到后冷藏"}, Alternatives: []interactionPlanCandidate{{Outline: InteractionAnswerOutline{Purpose: "确认保存", Angle: "包装允许的保存条件"}, Basis: []string{"不同包装的保存条件"}}}}
	selectInteractionPlanAngle(plan, []string{"存放环境"})
	if plan.Outline.Angle != "包装允许的保存条件" || len(plan.Basis) != 1 || plan.Basis[0] != "不同包装的保存条件" || plan.Alternatives != nil {
		t.Fatal("only words changed instead of selecting another content task", plan)
	}
	selectInteractionPlanAngle(plan, []string{"包装允许的保存条件"})
	if plan.Outline.Angle != "包装允许的保存条件" {
		t.Fatal("single valid angle should retain correct answer")
	}
}

type failedInteractionPlanner struct{ fakeAgent }

func (a *failedInteractionPlanner) Complete(ctx context.Context, request agentgateway.Request) (agentgateway.Response, error) {
	if request.Stage == "interaction_agent_plan" {
		return agentgateway.Response{Text: `{"unknown":"invalid plan"}`}, nil
	}
	return a.fakeAgent.Complete(ctx, request)
}

func TestFailedBusinessPlanDoesNotBecomeCompletedExplanation(t *testing.T) {
	a := &failedInteractionPlanner{fakeAgent: fakeAgent{responses: []string{interactionAnswerFixture("保存环境", "需要冷藏。")}}}
	w := New(readyVoiceStore(), nil, a, nil)
	result, err := w.SimulateInteraction(context.Background(), 7, 11, "放冰箱不", "", nil)
	if err != nil || result.Reply != "需要冷藏。" || result.AnswerOutline != nil || result.AnswerOutlineError == "" {
		t.Fatal("failed planning invented completed metadata or discarded speech", result, err)
	}
}

func TestIncompleteBodyMetadataDoesNotHideItsFormatWarning(t *testing.T) {
	raw := strings.Replace(interactionAnswerFixture("保存环境", "需要冷藏。"), `"reply":`, `"unexpected":"invalid","reply":`, 1)
	a := &fakeAgent{responses: []string{raw}}
	w := New(readyVoiceStore(), nil, a, nil)
	result, err := w.SimulateInteraction(context.Background(), 7, 11, "放冰箱不", "", nil)
	if err != nil || result.Reply != "需要冷藏。" || result.AnswerOutline == nil || result.AnswerOutlineError == "" {
		t.Fatal("body formatting anomaly hid its warning or discarded valid plan", result, err)
	}
}

func TestFAQRetainsMatchingBusinessPlanWithCachedSpeech(t *testing.T) {
	w, _, core, a, _, now := newFAQTestWorker()
	a.responses = []string{interactionAnswerFixture("保存环境", "需要冷藏。"), interactionAnswerFixture("保存环境", "需要冷藏。")}
	executeFAQ(t, w, core, 11, "放冰箱不")
	*now = now.Add(1)
	executeFAQ(t, w, core, 11, "放冰箱不")
	if a.calls != 1 {
		t.Fatal("configured valid cache ignored")
	}
	for _, variants := range w.faqCache.buckets {
		if len(variants) != 1 || variants[0].AnswerOutline == nil || variants[0].AnswerOutline.Angle != "保存环境" {
			t.Fatal("cached answer separated from its real plan", variants)
		}
	}
}
