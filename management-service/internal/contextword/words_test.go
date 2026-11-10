package contextword

import (
	"fmt"
	"strings"
	"testing"
)

func TestMatchConditionalCompetingSensesAndBoundaries(t *testing.T) {
	entries := []Entry{{Triggers: []string{"切"}, Meaning: "切块", When: "讨论商品处理", Enabled: true}, {Triggers: []string{"切"}, Meaning: "切换商品", When: "讨论上架顺序", Enabled: true}, {Triggers: []string{"SF"}, Meaning: "顺丰", Enabled: true}, {Triggers: []string{"邮"}, Meaning: "配送", Enabled: false}}
	got := Match(entries, "能切吗 sf能寄吗")
	if got == nil || len(got.Hits) != 3 || got.Hits[0].Meaning != "顺丰" || got.Hits[1].When != "讨论商品处理" || got.Hits[2].When != "讨论上架顺序" {
		t.Fatalf("%+v", got)
	}
	if Match(entries, "邮到ESFO") != nil {
		t.Fatal("disabled entry or partial ASCII alias matched")
	}
	if got := Match(entries, "切一下"); len(got.Hits) != 2 {
		t.Fatal(got)
	}
}
func TestMatchBoundsKeepWholeRulesAndReportOmissions(t *testing.T) {
	var entries []Entry
	for i := 0; i < 30; i++ {
		entries = append(entries, Entry{Triggers: []string{"切"}, Meaning: fmt.Sprint(i) + strings.Repeat("意", 399), When: strings.Repeat("条", 300), Example: strings.Repeat("例", 200), Enabled: true})
	}
	got := Match(entries, "切一下")
	if got == nil || len(got.Hits) != 4 || got.Omitted != 26 || len([]rune(got.Hits[0].When)) != 300 {
		t.Fatalf("%+v", got)
	}
}
func TestNormalizeWordsRequiresMeaningAllowsConditionalSenses(t *testing.T) {
	if _, e := Normalize([]Entry{{Triggers: []string{"切"}}}); e == nil {
		t.Fatal("empty meaning accepted")
	}
	a := Entry{Triggers: []string{"切", " 切 "}, Meaning: "切块", Enabled: true}
	got, e := Normalize([]Entry{a, {Triggers: []string{"切"}, Meaning: "切换", When: "上架", Enabled: true}})
	if e != nil || len(got[0].Triggers) != 1 {
		t.Fatal(got, e)
	}
	if _, e = Normalize([]Entry{a, a}); e == nil {
		t.Fatal("duplicate accepted")
	}
}
