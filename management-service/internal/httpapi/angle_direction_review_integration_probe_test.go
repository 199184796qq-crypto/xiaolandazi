package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"testing"
	"time"
)

func TestDirectionReviewPlanningWritingProviderIntegration(t *testing.T) {
	if os.Getenv("ANGLE_DIRECTION_INTEGRATION_PROBE") != "1" {
		t.Skip("explicit paid text-only integrated test")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
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
	p0, p0Path, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	rows := []map[string]any{}
	defer func() {
		b, e := json.MarshalIndent(map[string]any{"rows": rows, "writer": writer.Model, "reviewer": reviewer.Model, "passed": !t.Failed(), "text_only": true, "playback_count": "simulated", "business_pool_changed": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("ANGLE_DIRECTION_INTEGRATION_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	for i, fixture := range []struct{ category, identity, facts string }{
		{"熟食", "即食卤炸鸭，开袋直接吃，不作为生肉炒菜或炖汤。", "可选抽真空或不抽真空；未开封常温抽真空6天，不抽真空3天。"},
		{"家电", "3L电饭煲用于煮饭，已确认支持煎炸功能；不支持压力烹饪。", "煎炸时必须开盖，内胆不能放在明火上。"},
	} {
		task := anchorcycle.Task{Key: fmt.Sprint("synthetic-integration-", i), SellingPoint: "使用选择", Angle: "按确认条件帮助选择用途", AllowedLinksJSON: `["1"]`, FactScope: &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "program-topic", Products: []string{"1"}, Data: fixture.facts}}, ProductBoundary: []anchorcycle.HookFact{{ID: "program-identity", Products: []string{"1"}, Data: fixture.identity}}}}
		state, e := anchorcycle.New([]anchorcycle.Task{task}, anchorcycle.Config{Rolling: true, MaxChars: 220, BodySegments: 3, TextProfileID: writer.ID, FeedbackID: writer.ID})
		if e != nil {
			t.Fatal(e)
		}
		state.TenantID, state.PlanID, state.Surface, state.Phase = 14, 0, "formal_live", "body"
		state.TextModel, state.ReviewModel, state.BaselineJSON = writer, reviewer, textTestBaseline()
		state.Delivery = &anchorcycle.MainlineDelivery{ProgramID: "offline-simulated-program"}
		if e = state.InstallP0(p0, p0Path); e != nil {
			t.Fatal(e)
		}
		visits := []map[string]any{}
		for visit := 0; visit < 4; visit++ {
			started := time.Now()
			response, messages, e := server.completeCycleText(ctx, &state)
			hook := state.CurrentAngleHook()
			plan := state.CurrentAnglePlan()
			var planSnapshot any
			if plan != nil {
				b, _ := json.Marshal(plan)
				_ = json.Unmarshal(b, &planSnapshot)
			}
			visits = append(visits, map[string]any{"visit": visit + 1, "response": response, "messages": messages, "hook": hook, "plan": planSnapshot, "full_ms": time.Since(started).Milliseconds(), "generation_completed": e == nil})
			if e != nil || response.Text == "" || plan == nil || len(plan.Cards) == 0 || hook == nil {
				t.Errorf("%s integrated generation incomplete: %v", fixture.category, e)
				break
			}
			if len(anchorcycle.ReviewedDirectionCards(plan.Cards, plan.DirectionReviews, plan.ReviewContextSHA)) != len(plan.Cards) {
				t.Errorf("%s selected a direction without matching current approval", fixture.category)
			}
			segment := anchorcycle.Segment{Task: state.Task(), Selected: anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, AnglePlan: plan, AngleHook: hook}}
			state.Accepted = append(state.Accepted, segment)
			use := state.Used[state.Task().Key]
			use.Count++
			state.Used[state.Task().Key] = use
			state.RegisterAngleTrack(segment)
			state.ApplyAnglePlayback(state.Delivery.ProgramID, []anchorcycle.AnglePlaybackReceipt{{Sequence: uint64(visit + 1), TrackID: segment.Selected.ID, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}})
		}
		plan := state.CurrentAnglePlan()
		if plan == nil || plan.GenerationCount != 2 {
			t.Errorf("%s did not supplement after three simulated uses", fixture.category)
		}
		rows = append(rows, map[string]any{"category": fixture.category, "visits": visits, "final_plan": plan, "simulated_usage": state.AngleProgress, "warnings": state.Warnings})
		t.Logf("completed %s visits=%d", fixture.category, len(visits))
	}
}
