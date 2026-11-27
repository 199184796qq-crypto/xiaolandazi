package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
)

func TestPlanRiskAnnotationAllowsFirstSentenceButKeepsRanking(t *testing.T) {
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	text := "所以第一句我就想把这事儿说明白。我们是全国第一。"
	result, err := contentguard.Annotate(text, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	state := &anchorcycle.State{}
	if err := state.InstallP0(p0, "test"); err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(p0)
	if err != nil {
		t.Fatal(err)
	}
	result.Hits = state.FilterP0Annotations(policy, text, result.Hits)
	var ranking bool
	for _, hit := range result.Hits {
		if hit.Severity != "P0" {
			continue
		}
		if hit.Start < len([]rune("所以第一句")) {
			t.Fatalf("ordinary ordinal was marked: %+v", hit)
		}
		if strings.Contains(text, "全国第一") && strings.Contains("全国第一", hit.Text) {
			ranking = true
		}
	}
	if !ranking {
		t.Fatalf("ranking claim was released: %+v", result.Hits)
	}
}
