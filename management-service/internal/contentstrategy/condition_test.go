package contentstrategy

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestActualMixedModelConditionOutputIsExplicitlyCompatible(t *testing.T) {
	raw, err := os.ReadFile("testdata/model-angle-mixed-condition-keys.json")
	if err != nil {
		t.Fatal(err)
	}
	var table Table
	if err := Decode(string(raw), &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Rows) != 18 {
		t.Fatal("model rows lost")
	}
	for _, row := range table.Rows {
		if err := ValidateRowPolicy(row, true); err != nil {
			t.Fatal(row.Angle, err)
		}
	}
	canonical, _ := json.Marshal(table)
	if strings.Contains(string(canonical), `"type"`) || strings.Contains(string(canonical), `"value"`) || !strings.Contains(string(canonical), `"类型"`) {
		t.Fatal("noncanonical downstream schema")
	}
}
func TestConditionUnknownNullAndConflictingFieldsStayRejected(t *testing.T) {
	for _, raw := range []string{`{"类型":"always","extra":1}`, `{"type":"question","类型":"mainline"}`, `{"type":"purpose_match","value":"P01","值":"P02"}`, `{"type":null}`, `{}`} {
		var c Condition
		if err := Decode(raw, &c); err == nil {
			t.Fatal("malformed protocol accepted", raw)
		}
	}
	for _, raw := range []string{`{"type":"invented"}`, `{"type":"purpose_match"}`, `{"type":"always","value":"P01"}`} {
		var c Condition
		if err := Decode(raw, &c); err != nil {
			t.Fatal(err)
		}
		if err := ValidateRowPolicy(Row{Conditions: []Condition{c}}, false); err == nil {
			t.Fatal("invalid condition accepted", raw)
		}
	}
}

func TestCompatibleNamesEquivalentDuplicatesAndOptionalNullAreAccepted(t *testing.T) {
	for _, raw := range []string{`{"类型":"目的匹配","Type":"purpose_match","值":"P01","value":"P01"}`, `{"type":"Purpose-Match","目的编号":"P01"}`, `{"type":"question","value":null}`} {
		var c Condition
		if err := Decode(raw, &c); err != nil {
			t.Fatal(raw, err)
		}
		if err := ValidateRowPolicy(Row{Conditions: []Condition{c}}, false); err != nil {
			t.Fatal(err)
		}
	}
	var table Table
	if err := Decode(`{"rows":[{"purpose_id":"P01","angle":"价格","task":"报价","fact_ids":"F001","when":"询价","priority":"90","task_type":"必答","conditions":[{"Type":"QUESTION"},{"type":"purpose_match","purpose_id":"P01"}]}]}`, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Rows) != 1 || table.Rows[0].Role != "required" || table.Rows[0].Priority != 90 {
		t.Fatal(table)
	}
	if err := ValidateRowPolicy(table.Rows[0], true); err != nil {
		t.Fatal(err)
	}
}
