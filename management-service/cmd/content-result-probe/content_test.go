package main

import (
	"encoding/json"
	"testing"
)

func TestModelNumberingCannotChooseInternalIdentifiers(t *testing.T) {
	items := directionItems("1. 从工艺展开\n1. 从原料展开\n8. 从门店展开", "issued-request")
	if len(items) != 3 {
		t.Fatal(items)
	}
	for i, item := range items {
		if item.Sequence != i+1 || item.ID != "issued-request/direction-"+string(rune('1'+i)) {
			t.Fatal(items)
		}
	}
}

func TestUnstructuredResponseAndDecimalPriceKeepContent(t *testing.T) {
	for _, raw := range []string{"85.5元一只，按这一只的预算展开。", `{"index":999,"fact_ids":[":"],"task_id":"wrong"}`} {
		items := directionItems(raw, "owned")
		if len(items) != 1 || items[0].Text != raw || items[0].ID != "owned/direction-1" {
			t.Fatal(items)
		}
	}
	items := directionItems("1. 85.5元一只，从预算展开\n2、从保存方式展开", "owned")
	if len(items) != 2 || items[0].Text != "85.5元一只，从预算展开" {
		t.Fatal(items)
	}
}

func TestProgramContextSurvivesInputMutationAndJSONRoundTrip(t *testing.T) {
	input := binding{CaseID: "case-owned", SourceExchange: 3647, TaskKey: "angle-owned", Products: []string{"1"}, Facts: []string{"knowledge:16", "knowledge:20"}}
	issued := freezeBinding(input)
	input.Products[0] = "wrong"
	input.Facts[0] = "k:16"
	rawResponse := `{"case_id":"fake","task_key":"fake","product_ids":[":"],"fact_ids":["k:16"]}`
	record := struct {
		Binding binding     `json:"program_binding"`
		Items   []direction `json:"items"`
	}{issued, directionItems(rawResponse, "issued")}
	bytes, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Binding binding     `json:"program_binding"`
		Items   []direction `json:"items"`
	}
	if err = json.Unmarshal(bytes, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Binding.TaskKey != "angle-owned" || saved.Binding.Products[0] != "1" || saved.Binding.Facts[0] != "knowledge:16" || saved.Items[0].Text != rawResponse {
		t.Fatal(saved)
	}
}
