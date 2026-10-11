package httpapi

import (
	"reflect"
	"sort"
	"testing"
)

func TestFactExamplePoolOnlyReturnsContent(t *testing.T) {
	if _, err := parseFactExamplePool(`{"examples":["一种说法","另一种说法","场景说法"]}`); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"examples":[]}`, `{"examples":["A","B",""]}`, `{"examples":["A","B","C"],"fact_id":7}`, `{"examples":["A","B","C"],"reviewed":true}`} {
		if _, err := parseFactExamplePool(raw); err == nil {
			t.Fatal("model supplied program metadata or invalid pool", raw)
		}
	}
}
func TestFactExamplesAcceptThreeToFiveAndShuffleWithoutChangingFacts(t *testing.T) {
	for _, raw := range []string{`{"examples":["A","B","C","D"]}`, `{"examples":["A","B","C","D","E"]}`} {
		if _, err := parseFactExamplePool(raw); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := parseFactExamplePool(`{"examples":["A","B","C","D","E","F"]}`); err == nil {
		t.Fatal("unbounded example count")
	}
	original := []string{"A", "B", "C", "D", "E"}
	ordered := shuffledFactExamples(original)
	sort.Strings(ordered)
	if !reflect.DeepEqual(original, ordered) {
		t.Fatal("shuffle changed example content")
	}
	ordered[0] = "modified"
	if original[0] != "A" {
		t.Fatal("shuffle overwrote persisted order")
	}
}
