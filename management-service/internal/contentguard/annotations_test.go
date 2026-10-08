package contentguard

import (
	"os"
	"strings"
	"testing"
)

func annotationMothers(t *testing.T) (string, string, string) {
	t.Helper()
	read := func(path string) string {
		raw, err := os.ReadFile("../../../configs/prompts/anchor-style/compliance/" + path)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	return read("L1/p0-hard-block.md"), read("L2/food/p1-evidence-cards.md"), read("L2/food/p2-wording-rewrite.md")
}
func TestRiskAnnotationUsesThreeMothersAndUnicodeOffsets(t *testing.T) {
	p0, p1, p2 := annotationMothers(t)
	text := "😀零胆固醇，低\u200b芥酸，非转基因。买到就是赚到，省了健康钱，零风险。"
	result, err := Annotate(text, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	tiers := map[string]int{}
	for _, h := range result.Hits {
		tiers[h.Severity]++
		if string([]rune(text)[h.Start:h.End]) != h.Text {
			t.Fatal("original offset lost", h)
		}
	}
	if tiers["P1"] != 3 || tiers["P2"] != 2 || tiers["P0"] == 0 || len(result.RuleHashes) != 3 || result.Mode != "lexical_review_only" {
		t.Fatal(result)
	}
	if text != "😀零胆固醇，低\u200b芥酸，非转基因。买到就是赚到，省了健康钱，零风险。" {
		t.Fatal("text rewritten")
	}
}
func TestAnnotationExcludedTermsDoNotConfuseLowFattyAcidWithLowFat(t *testing.T) {
	p0, p1, p2 := annotationMothers(t)
	text := "低脂肪酸，低 脂 肪 酸，低脂。"
	result, err := Annotate(text, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	lowFat := 0
	for _, h := range result.Hits {
		if h.RuleID == "food.p1.fat_low" {
			lowFat++
			if h.Start != len([]rune("低脂肪酸，低 脂 肪 酸，")) {
				t.Fatal("excluded term marked", h)
			}
		}
	}
	if lowFat != 1 {
		t.Fatal(result)
	}
	dense, err := Annotate(strings.Repeat("低脂肪酸，", 1000), p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range dense.Hits {
		if h.RuleID == "food.p1.fat_low" {
			t.Fatal("dense exclusions ignored")
		}
	}
}
func TestAnnotationConditionalMarkersDoNotClaimViolation(t *testing.T) {
	p0, p1, p2 := annotationMothers(t)
	result, err := Annotate("都拍到了不？老手艺39年。", p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	conditional := false
	for _, h := range result.Hits {
		if h.RuleID == "food.p2.purchase_question" {
			conditional = true
			if !strings.Contains(h.Note, "不等于已判违规") {
				t.Fatal(h)
			}
		}
		if h.Severity == "P1" && !strings.Contains(h.Note, "不表示已有或缺少凭证") {
			t.Fatal(h)
		}
	}
	if !conditional {
		t.Fatal(result)
	}
	for _, md := range []string{
		strings.Replace(p2, `"severity": "P2"`, `"severity": "P1"`, 1),
		strings.Replace(p2, `"condition":"always_if_no_higher_priority_block"`, `"condition":"unknown_condition"`, 1),
		strings.Replace(p1, `"schema_version": 1`, `"new_unknown_field": true, "schema_version": 1`, 1),
	} {
		if _, err := compileAnnotation(md, "P2"); err == nil {
			t.Fatal("invalid mother accepted")
		}
	}
	if _, err := Annotate(strings.Repeat("字", 100001), p0, p1, p2); err == nil {
		t.Fatal("oversized text allowed")
	}
}
