package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"testing"
	"time"
)

// A user-visible example using real planning, direction review and writing.
// No voice synthesis, business adoption or playback receipts are submitted.
func TestSamePointThreeProviderExample(t *testing.T) {
	if os.Getenv("SAME_POINT_THREE_PROBE") != "1" {
		t.Skip("explicit paid three-pass example")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
	defer stop()
	models, err := store.SpeechModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := store.ResolveSpeechModel(ctx, models.DefaultID)
	if err != nil || writer == nil {
		t.Fatal("writer unavailable")
	}
	reviewID, err := store.ResolveTextRole(ctx, "angle_direction_review")
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := store.ResolveSpeechModel(ctx, reviewID)
	if err != nil || reviewer == nil {
		t.Fatal("reviewer unavailable")
	}
	p0, source, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	facts := "可选抽真空或不抽真空。未开封、常温条件下，抽真空6天，不抽真空3天。开封后建议当天吃完。"
	boundary := "这是已加工好的即食卤炸鸭，开袋直接吃，不作为生肉炒菜或炖汤。"
	task := anchorcycle.Task{Key: "offline-packing-example", SellingPoint: "包装选择及常温保存", Angle: "帮助按食用计划选对包装，分别从近期食用、晚几天食用、两种包装对比展开", PainPoint: "不知道两种包装怎样按自己的食用计划选择", AllowedLinksJSON: `["1"]`, FactScope: &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "program-fact", Products: []string{"1"}, Data: facts}}, ProductBoundary: []anchorcycle.HookFact{{ID: "program-boundary", Products: []string{"1"}, Data: boundary}}, Corrections: []map[string]any{{"表达限制": "凡讲常温保存天数，保留未开封前提；不能把6天或3天套到开封后。"}}}}
	state, err := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{Rolling: true, MaxChars: 220, BodySegments: 3, TextProfileID: writer.ID, FeedbackID: writer.ID})
	if err != nil {
		t.Fatal(err)
	}
	state.TenantID, state.PlanID, state.Surface, state.Phase = 14, 0, factPreviewSurface, "body"
	state.TextModel, state.ReviewModel, state.BaselineJSON, state.Facts = writer, reviewer, textTestBaseline(), boundary+"\n"+facts
	if err = state.InstallP0(p0, source); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	rows := []map[string]any{}
	defer func() {
		b, e := json.MarshalIndent(map[string]any{"point": task.SellingPoint, "goal": "帮助观众按食用计划选对包装", "facts": facts, "product": boundary, "p0": p0, "writer": writer.Model, "reviewer": reviewer.Model, "plan": state.CurrentAnglePlan(), "rows": rows, "text_only": true, "business_pool_changed": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("SAME_POINT_THREE_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	accepted := 0
	for visit := 0; visit < 6 && accepted < 3; visit++ {
		response, messages, e := server.completeCycleText(ctx, &state)
		if e != nil || response.Text == "" {
			t.Fatal("writing incomplete", e)
		}
		candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, OutputText: response.Text, AnglePlan: state.CurrentAnglePlan(), AngleHook: state.CurrentAngleHook()}
		cleanCycleOpeningInstruction(&state, &candidate, "")
		cleanCycleConsecutiveSpeech(&state, &candidate)
		if candidate.OutputText == "" {
			rows = append(rows, map[string]any{"visit": visit + 1, "generation": response, "generation_messages": messages, "selected_direction": candidate.AngleHook, "final_text": "", "accepted": false, "skip_reason": "consecutive_exact_duplicate", "duplicate_removals": candidate.ExactDuplicateRemovals})
			use := state.Used[state.Task().Key]
			use.Count++
			state.Used[state.Task().Key] = use
			t.Logf("attempt=%d duplicate skipped", visit+1)
			continue
		}
		if e = state.CheckP0Candidate(&candidate); e != nil {
			t.Fatal(e)
		}
		server.auditFinalMainlineContent(ctx, &state, &candidate)
		passed := state.FinalContentCandidateApproved(candidate)
		rows = append(rows, map[string]any{"visit": visit + 1, "generation": response, "generation_messages": messages, "selected_direction": candidate.AngleHook, "final_text": candidate.OutputText, "p0_report": candidate.P0Report, "p0_reference": priorP0ReviewView(&state, candidate), "final_audit": candidate.FinalContentAudit, "expression_audit": candidate.ExpressionAudit, "accepted": passed})
		if passed {
			accepted++
			state.Accepted = append(state.Accepted, anchorcycle.Segment{Task: state.Task(), Selected: candidate})
		}
		use := state.Used[state.Task().Key]
		use.Count++
		state.Used[state.Task().Key] = use
		t.Logf("visit=%d accepted=%v review=%s", visit+1, passed, candidate.FinalContentAudit.Raw)
	}
	if accepted != 3 {
		t.Errorf("only %d usable paragraphs within six bounded attempts", accepted)
	}
}
