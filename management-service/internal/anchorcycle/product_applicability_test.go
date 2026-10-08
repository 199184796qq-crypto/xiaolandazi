package anchorcycle

import (
	"strings"
	"testing"
)

func TestOrdinaryMultiProductAngleKeepsSingleProductFacts(t *testing.T) {
	s := focusFixture(t)
	q := s.Queue[0]
	s.ProductFocus.Tasks[q.Key] = ScopedTask{Links: []string{"1", "2"}, Comparison: false}
	if !s.productTaskEligible(q) {
		t.Fatal("ordinary multi-product applicability excluded from mainline")
	}
	task := s.focusTask(q)
	if task.ProductMode != "single" || len(task.ProductLinks()) != 1 || task.ProductLinks()[0] != "1" {
		t.Fatal("applicability enlarged this segment's scope", task)
	}
	facts := s.ProductFacts(task)
	if !strings.Contains(facts, "900g") || strings.Contains(facts, "450g") || strings.Contains(facts, "48.8") {
		t.Fatal("other product facts leaked", facts)
	}
	if s.ProductTextIssue("2号链接半只450g只要48.8元", task) == "" {
		t.Fatal("single-product voice gate relaxed")
	}
	s.ProductFocus.Tasks[q.Key] = ScopedTask{Links: []string{"1", "2"}, Comparison: true}
	if s.productTaskEligible(q) {
		t.Fatal("true comparison entered mainline exclusive segment")
	}
}

func TestAuxiliaryUsesApplicabilityWithoutAddingThirdProduct(t *testing.T) {
	s := focusFixture(t)
	s.ProductFocus.Products = append(s.ProductFocus.Products, FocusProduct{LinkKey: "3", Roles: []string{"profit"}})
	q := s.Queue[0]
	s.ProductFocus.Tasks[q.Key] = ScopedTask{Links: []string{"2", "3"}, Comparison: false}
	s.ProductFocus.CurrentLink = "2"
	s.ProductFocus.AuxiliaryActive = true
	s.ProductFocus.MainChars = 1000
	if !s.productTaskEligible(q) {
		t.Fatal("ordinary angle for two auxiliaries must be reusable for the selected auxiliary")
	}
	task := s.focusTask(q)
	if task.FocusLink != "2" || containsLink(task.ProductLinks(), "3") {
		t.Fatal("third product added to current auxiliary", task)
	}
	s.ProductFocus.Tasks[q.Key] = ScopedTask{Links: []string{"2", "3"}, Comparison: true}
	if s.productTaskEligible(q) {
		t.Fatal("comparison against a third product exceeded this auxiliary's scope")
	}
}
