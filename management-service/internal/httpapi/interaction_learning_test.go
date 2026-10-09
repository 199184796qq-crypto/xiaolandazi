package httpapi

import "testing"

func TestSimulationCannotSilentlyDiscardUnknownOrIncompleteFields(t *testing.T) {
	good := `{"items":[{"question":"不抽可以吗","context":"食品包装","kind":"missing_fact","meaning":"能否选择非真空","reason":"可选规则缺失","suggested_rule":"","needed_facts":["是否可不抽真空"]}]}`
	items, err := decodeInteractionSimulation(good)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	for _, raw := range []string{`{"items":null}`, `{"items":[{"question":"不抽","context":"包装","kind":"unsupported","meaning":"真空","reason":"简称"}]}`, good + ` {}`, `{"items":[],"other":1}`} {
		if _, err := decodeInteractionSimulation(raw); err == nil {
			t.Fatalf("bad simulation accepted: %s", raw)
		}
	}
}

func TestSimulationSuggestionsAreForUserJudgment(t *testing.T) {
	for _, raw := range []string{`{"items":[]}`, `{"items":[{"question":"不抽","context":"包装","kind":"semantic","meaning":"真空","reason":"简称","suggested_rule":"","needed_facts":[]}]}`, `{"items":[{"question":"几天到","context":"物流","kind":"missing_fact","meaning":"到货时效","reason":"模型建议","needed_facts":[]}]}`} {
		if _, err := decodeInteractionSimulation(raw); err != nil {
			t.Fatalf("model suggestion was vetoed before user judgment: %s: %v", raw, err)
		}
	}
}
