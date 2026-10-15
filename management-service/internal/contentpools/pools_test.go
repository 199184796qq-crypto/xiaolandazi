package contentpools

import (
	"testing"
)

func TestNetChangesKeepLatestValueAndRemoveRevertedEdits(t *testing.T) {
	base := Sources{Rows: []SourceRow{{ID: "attributes:8", Area: "products", Label: "口味", Products: []int64{1}, Fields: map[string]string{"内容": "五香"}}}}
	next := Sources{Rows: []SourceRow{{ID: "attributes:8", Area: "products", Label: "口味", Products: []int64{1}, Fields: map[string]string{"内容": "麻辣"}}}}
	got := Changes(base, next)
	if len(got) != 1 || got[0].Before != "五香" || got[0].After != "麻辣" {
		t.Fatalf("net change: %+v", got)
	}
	next.Rows[0].Fields["内容"] = "香辣"
	got = Changes(base, next)
	if len(got) != 1 || got[0].After != "香辣" {
		t.Fatal(got)
	}
	next.Rows[0].Fields["内容"] = "五香"
	if len(Changes(base, next)) != 0 {
		t.Fatal("reverted edit still pending")
	}
	next.Revision = 100
	next.ObservedAt = 9000
	if base.SHA() != next.SHA() {
		t.Fatal("revision or observation time changed content digest")
	}
}
func TestCommonApplicabilityDoesNotExpandToNewProductsAndUnchangedPoolReuses(t *testing.T) {
	products := []Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}, {ID: 3, Link: "3"}}
	facts := []Fact{{ID: "a", Text: "A专属", Sources: []string{"products:1"}, Products: []int64{1}, Private: true}, {ID: "b", Text: "B专属", Sources: []string{"products:2"}, Products: []int64{2}, Private: true}, {ID: "same", Text: "共有属性", Sources: []string{"attributes:1", "attributes:2"}, Products: []int64{1, 2}}}
	first := GroupPools(facts, products, "m1")
	fingerprints := map[string]string{}
	for _, p := range first {
		fingerprints[p.Key] = p.Fingerprint
		if p.Owner == 0 && (Contains(p.Products, 3) || len(p.Products) != 2) {
			t.Fatal("common range expanded", p)
		}
	}
	facts[0].Text = "A变更"
	second := GroupPools(facts, products, "m1")
	for _, p := range second {
		same := p.Fingerprint == fingerprints[p.Key]
		if (p.Owner == 1) == same {
			t.Fatalf("affected pool reuse wrong: %+v", p)
		}
	}
}
func TestValidateSetRejectsWrongScopeSourceAndTaskIDs(t *testing.T) {
	sources := Sources{Products: []Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}}, Rows: []SourceRow{{ID: "facts:1"}}}
	set := Set{Sources: sources, SourceSHA: sources.SHA(), SafeFacts: "事实", Pools: []Pool{{Key: "common", Products: []int64{1, 2}, Facts: []Fact{{ID: "f", Sources: []string{"facts:1"}, Products: []int64{1, 2}}}, Tasks: []Task{{ID: "t", Point: "卖点", Angle: "角度", Sources: []string{"f"}}}}}}
	if err := ValidateSet(set); err != nil {
		t.Fatal(err)
	}
	set.Pools[0].Facts[0].Products = []int64{1}
	if ValidateSet(set) == nil {
		t.Fatal("A-only fact accepted for B")
	}
	set.Pools[0].Facts[0].Products = []int64{1, 2}
	set.Pools[0].Facts[0].Sources = []string{"unknown"}
	if ValidateSet(set) == nil {
		t.Fatal("unknown DB origin accepted")
	}
	set.Pools[0].Facts[0].Sources = []string{"facts:1"}
	set.Pools[0].Tasks = append(set.Pools[0].Tasks, set.Pools[0].Tasks[0])
	if ValidateSet(set) == nil {
		t.Fatal("duplicate stable task ID accepted")
	}
}

func TestUncoveredFacts(t *testing.T) {
	set := Set{Pools: []Pool{{
		Key: "common",
		Facts: []Fact{
			{ID: "f1"},
			{ID: "f2"},
			{ID: "f3"},
		},
		Tasks: []Task{{ID: "t1", Point: "卖点", Angle: "角度", Sources: []string{"f1"}}},
	}}}
	got := set.UncoveredFacts()
	if len(got) != 2 || got[0] != "f2" || got[1] != "f3" {
		t.Fatalf("UncoveredFacts 应返回未被引用的 f2/f3，got %v", got)
	}
}
