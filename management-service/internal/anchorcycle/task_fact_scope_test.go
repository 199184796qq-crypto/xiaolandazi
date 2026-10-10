package anchorcycle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskFactScopeKeepsConditionsAndProductBoundaryAcrossRedis(t *testing.T) {
	s := fixture(t, 2)
	s.Queue[0].AllowedLinksJSON = `["1"]`
	s.Queue[0].FactScope = &TaskFactScope{Facts: []HookFact{
		{ID: "preservation", Data: "未开封常温：抽真空6天，不抽真空3天", Products: []string{"1", "2"}},
		{ID: "other-product", Data: "另一商品的保存资料", Products: []string{"2"}},
	}}
	s.Facts = "全量资料含物流"
	s.ProductFocus = nil
	raw, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	facts := restored.HookFacts()
	if len(facts) != 1 || facts[0].ID != "preservation" || !strings.Contains(facts[0].Data, "不抽真空3天") {
		t.Fatal(facts)
	}
	// Explicitly empty bindings must not silently become all room facts.
	restored.Queue[0].FactScope = &TaskFactScope{Facts: []HookFact{}}
	if len(restored.HookFacts()) != 0 {
		t.Fatal("empty task leaked unrelated facts")
	}
}

func TestDerivedClosingAndPriceTasksKeepTheirSourceTaskScope(t *testing.T) {
	s := fixture(t, 2)
	scope := &TaskFactScope{Facts: []HookFact{{ID: "price", Data: "半只450g，48.8元", Products: []string{"current"}}}}
	s.Queue[0].FactScope = scope
	s.PriceHook = &PriceHook{Task: s.Queue[0]}
	if s.Task().FactScope != scope {
		t.Fatal("price delivery expanded to full material")
	}
	s.PriceHook = nil
	s.Accepted = []Segment{{Task: s.Queue[0]}}
	s.Phase = "closing"
	if s.Task().FactScope != scope {
		t.Fatal("closing expanded to full material")
	}
}
