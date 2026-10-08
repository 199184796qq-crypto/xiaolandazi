package httpapi

import (
	"testing"

	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/model"
)

func TestBuildPlanP0AllowancesFromConfirmedFactReview(t *testing.T) {
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(md)
	if err != nil {
		t.Fatal(err)
	}
	reviews := []model.LiveRiskReview{{
		Severity: "P1", Status: "confirmed", Evidence: "用户已知晓并确认",
		Sources: []model.LiveRiskReviewSource{{SourceKey: "facts/1/0", Text: "品牌为70年老字号"}},
	}}
	allowances := buildPlanP0Allowances(reviews, policy, "ordinary_food")
	if len(allowances) != 1 || allowances[0].RuleID != "l1.p0.absolute_claim" || allowances[0].MatchKey != policy.MatchKey("老字号") {
		t.Fatalf("confirmed fact did not compile into the expected plan allowance: %+v", allowances)
	}
	reviews[0].Status = "pending"
	if got := buildPlanP0Allowances(reviews, policy, "ordinary_food"); len(got) != 0 {
		t.Fatalf("unconfirmed fact released runtime speech: %+v", got)
	}
}

func TestBuildPlanP0AllowancesDoesNotReleaseHardRuleFromUserConfirmation(t *testing.T) {
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(md)
	if err != nil {
		t.Fatal(err)
	}
	reviews := []model.LiveRiskReview{{
		Severity: "P0", RuleID: "l1.p0.food_selected_permanent_ban", Status: "confirmed", SemanticDecision: "not_required",
		Sources: []model.LiveRiskReviewSource{{SourceKey: "facts/1/0", Text: "本商品使用食品级材料"}},
	}}
	if got := buildPlanP0Allowances(reviews, policy, "ordinary_food"); len(got) != 0 {
		t.Fatalf("hard rule was released without backend approval: %+v", got)
	}
	reviews[0].AppealStatus = "approved"
	if got := buildPlanP0Allowances(reviews, policy, "ordinary_food"); len(got) == 0 {
		t.Fatal("backend-approved hard rule did not enter the plan-local allowance")
	}
}
