package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestParseFactRiskSemanticReviewRequiresExactBatch(t *testing.T) {
	pending := map[string]bool{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": true}
	raw := `{"items":[{"claim_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","decision":"evidence_required","reason":"经营年限需要实际记录","suggested_text":"有经营记录的话按实际年份说明"}]}`
	items, err := parseFactRiskSemanticReview(raw, pending)
	if err != nil || len(items) != 1 || items[0].Decision != "evidence_required" {
		t.Fatal(items, err)
	}
	for _, invalid := range []string{
		`{"items":[]}`,
		`{"items":[{"claim_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","decision":"pass","reason":"不是宣传","suggested_text":""}]}`,
		`{"items":[{"claim_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","decision":"allow","reason":"可以","suggested_text":""}]}`,
		`{"items":[{"claim_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","decision":"pass","reason":"","suggested_text":""}]}`,
	} {
		if _, err = parseFactRiskSemanticReview(invalid, pending); err == nil {
			t.Fatal("invalid semantic response accepted", invalid)
		}
	}
}

func TestGeneratedWheelAbsoluteClaimCannotPassWithoutFactPackageSupport(t *testing.T) {
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	reviews := map[string]model.LiveRiskReview{
		hash: {
			ClaimSHA256: hash,
			RuleID:      "l1.p0.absolute_claim",
			Sources:     []model.LiveRiskReviewSource{{SourceKey: "wheel/7/angle/1", Text: "7天无理由退货为唯一售后保障"}},
		},
	}
	decisions := []model.LiveRiskSemanticDecision{{ClaimSHA256: hash, Decision: "pass", Reason: "模型误判", Suggestion: ""}}
	got := enforceGeneratedWheelSemanticBoundaries(decisions, reviews, "- [售后政策] 支持7天无理由退货")
	if got[0].Decision != "block" || got[0].Suggestion != "" || got[0].Reason == "模型误判" {
		t.Fatalf("unsupported generated exclusivity was not blocked: %+v", got[0])
	}
	got = enforceGeneratedWheelSemanticBoundaries([]model.LiveRiskSemanticDecision{{ClaimSHA256: hash, Decision: "evidence_required", Reason: "待确认"}}, reviews, "- [售后政策] 7天无理由退货为唯一售后保障")
	if got[0].Decision != "evidence_required" {
		t.Fatalf("verbatim user fact should remain a semantic/evidence decision: %+v", got[0])
	}
	reviews[hash] = model.LiveRiskReview{
		ClaimSHA256: hash,
		RuleID:      "l1.p0.absolute_claim",
		Sources:     []model.LiveRiskReviewSource{{SourceKey: "wheel/8/angle/1", Text: "开袋后最好当天吃完"}},
	}
	got = enforceGeneratedWheelSemanticBoundaries([]model.LiveRiskSemanticDecision{{ClaimSHA256: hash, Decision: "pass", Reason: "这是食用建议"}}, reviews, "- [开袋后保存期限] 最好当天吃完")
	if got[0].Decision != "pass" {
		t.Fatalf("advisory use of 最好 was incorrectly treated as a promotional absolute claim: %+v", got[0])
	}
}

func TestGeneratedOrdinalReferencesRespectSemanticModel(t *testing.T) {
	for _, text := range []string{"第一次购买、想了解食用和保存方法的消费者", "第一步开袋，第二步加热", "第一天建议吃完", "第一批发货", "我们第一时间回复"} {
		reviews := map[string]model.LiveRiskReview{"hash": {RuleID: "l1.p0.absolute_claim", Sources: []model.LiveRiskReviewSource{{SourceKey: "wheel/5/audience", Text: text}}}}
		for _, decision := range []string{"pass", "evidence_required", "block"} {
			got := enforceGeneratedWheelSemanticBoundaries([]model.LiveRiskSemanticDecision{{ClaimSHA256: "hash", Decision: decision, Reason: "模型判断"}}, reviews, "")
			if got[0].Decision != decision || got[0].Reason != "模型判断" {
				t.Fatalf("ordinal %q overrode model: %+v", text, got)
			}
		}
	}
	for _, text := range []string{"销量第一", "行业第一品牌", "第一次购买全网第一品牌", "第一天销量第一", "第一批独家优惠", "唯一售后保障"} {
		if !requiresVerbatimGeneratedFact(text) {
			t.Fatalf("comparative claim bypassed: %s", text)
		}
	}
}
