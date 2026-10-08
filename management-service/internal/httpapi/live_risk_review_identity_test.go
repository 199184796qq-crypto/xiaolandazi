package httpapi

import (
	"strings"
	"testing"
)

func TestRiskAtomicClaimsMergeDifferentWordingAndPreserveAllSources(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	facts := "#事实\n- [product/指标] 低芥酸、零胆固醇、非转基因油菜籽\n- [商品卡] 链接=1号链接；商品=26年非转基因样品5L油；规格=5L；指标=低芥酸、零胆固醇、非转基因\n"
	wheel := `{"卖点列表":[{"卖点名":"非转基因原料","角度":["非转基因油菜籽说明","原料非转基因属性"]}]}`
	d, err := buildDerivedInput(facts, wheel, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range d.Reviews {
		counts[r.RuleID]++
		if r.RuleID == "food.p1.non_gmo" {
			if len(r.Sources) != 6 || r.SemanticKey == "" {
				t.Fatalf("sources/key lost: %+v", r)
			}
			r.Status = "confirmed"
			r.Evidence = "同商品同批次报告"
			d.Statuses[r.ClaimSHA256] = r
		}
	}
	for _, id := range []string{"food.p1.non_gmo", "food.p1.erucic_low", "food.p1.cholesterol_zero"} {
		if counts[id] != 1 {
			t.Fatalf("not grouped: %v", counts)
		}
	}
	for _, f := range d.Fields {
		if strings.Contains(f.Text, "非转基因") && !strings.Contains(f.Text, "低芥酸") && d.needsRewrite(f) {
			t.Fatal("one confirmation not shared across sources")
		}
	}
}

func TestRiskSameRuleDifferentProductsAndQuantifiedClaimsRemainSeparate(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	facts := "- [商品卡] 链接=1号链接；商品=甲油非转基因；规格=5L\n- [商品卡] 链接=2号链接；商品=乙油非转基因；规格=5L\n- [product/定量指标] 高油酸含量70%\n- [product/定量指标] 高油酸含量90%"
	d, err := buildDerivedInput(facts, "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range d.Reviews {
		counts[r.RuleID]++
		if r.RuleID == "food.p1.non_gmo" && strings.Contains(r.Subject, "1号") {
			r.Status = "confirmed"
			r.Evidence = "甲油报告"
			d.Statuses[r.ClaimSHA256] = r
		}
	}
	if counts["food.p1.non_gmo"] != 2 || counts["food.p1.oleic_high"] != 2 {
		t.Fatalf("cross-product/value merge: %v", counts)
	}
	for _, f := range d.Fields {
		if strings.Contains(f.Text, "乙油") && !d.needsRewrite(f) {
			t.Fatal("product A confirmation authorized product B")
		}
	}
}

func TestRiskAttributeWordingKeepsIdentityButBatchAndProductChangesReset(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	facts := "- [商品卡] 链接=1号链接；商品=甲油；规格=5L\n- [product/属性] 非转基因"
	original, err := buildDerivedInput(facts, "", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	hash := original.Reviews[0].ClaimSHA256
	for _, tc := range []struct {
		text string
		same bool
	}{{strings.ReplaceAll(facts, "属性] 非转基因", "属性] 使用非转基因油菜籽"), true}, {facts + "\n- [批次/批号] B002", false}, {strings.ReplaceAll(facts, "甲油", "乙油"), false}} {
		d, err := buildDerivedInput(tc.text, "", p0, p1, p2)
		if err != nil {
			t.Fatal(err)
		}
		if (d.Reviews[0].ClaimSHA256 == hash) != tc.same {
			t.Fatalf("unexpected identity: %s", tc.text)
		}
	}
}
