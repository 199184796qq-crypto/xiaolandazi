package httpapi

import (
	"context"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestPlanSpeechTestReadsExplicitPlanWithoutRoomSnapshot(t *testing.T) {
	store := &planSpeechTestStore{tenantID: 14, roomID: 26, plan: model.LiveAgentPlan{ID: 4, TenantID: 14, Name: "当前编辑方案"}}
	ctx := context.Background()
	plan, err := store.GetLiveAgentPlanForRoom(ctx, 14, 26)
	if err != nil || plan.ID != 4 {
		t.Fatalf("selected plan lost: %#v %v", plan, err)
	}
	version, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, 26)
	if err != nil || version.PlanID != 4 || len(version.GenerationContext) != 0 {
		t.Fatalf("room snapshot leaked: %#v %v", version, err)
	}
	for _, scope := range [][2]int64{{15, 26}, {14, 27}} {
		if _, err := store.GetLiveAgentPlanForRoom(ctx, scope[0], scope[1]); err == nil {
			t.Fatal("wrong plan scope accepted")
		}
		if _, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, scope[0], scope[1]); err == nil {
			t.Fatal("wrong snapshot scope accepted")
		}
	}
	store.plan.TenantID = 15
	if _, err := store.GetLiveAgentPlanForRoom(ctx, 14, 26); err == nil {
		t.Fatal("foreign tenant plan accepted")
	}
}

func TestPlanAnswerAdoptionRequiresExplicitHumanConfirmedContent(t *testing.T) {
	valid := planAnswerAdoptionInput{PlanID: 4, Question: "发什么快递？", Answer: "使用京东物流。"}
	if err := validatePlanAnswerAdoption(valid); err != nil {
		t.Fatal(err)
	}
	for _, input := range []planAnswerAdoptionInput{
		{PlanID: 0, Question: valid.Question, Answer: valid.Answer},
		{PlanID: 4, Question: " ", Answer: valid.Answer},
		{PlanID: 4, Question: valid.Question, Answer: " "},
		{PlanID: 4, Question: strings.Repeat("问", 1001), Answer: valid.Answer},
		{PlanID: 4, Question: valid.Question, Answer: strings.Repeat("答", 4001)},
	} {
		if err := validatePlanAnswerAdoption(input); err == nil {
			t.Fatalf("invalid confirmation accepted: %#v", input)
		}
	}
}
