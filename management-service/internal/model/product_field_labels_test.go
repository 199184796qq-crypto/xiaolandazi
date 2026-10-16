package model

import (
	"strings"
	"testing"
)

func TestProductFieldLabelsPreserveStableFieldsAndOtherLabels(t *testing.T) {
	old := map[string]string{"quantity": "套餐内容"}
	next, err := MergeProductFieldLabels(old, map[string]string{"audience": " 用餐人数 "})
	if err != nil || next["quantity"] != "套餐内容" || next["audience"] != "用餐人数" || len(old) != 1 {
		t.Fatal(next, err)
	}
	p := LiveAgentPlanProductLink{Quantity: "22道荤素菜", FieldLabels: next}
	if p.FieldLabel("quantity", "数量") != "套餐内容" || p.Quantity != "22道荤素菜" || p.FieldLabel("spec", "规格") != "规格" {
		t.Fatal("label changed the underlying value")
	}
	next, err = MergeProductFieldLabels(next, map[string]string{"quantity": "数量"})
	if err != nil || next["quantity"] != "" || next["audience"] != "用餐人数" {
		t.Fatal("default label restore cleared unrelated fields")
	}
	for _, patch := range []map[string]string{{"product_name": "改名"}, {"spec": " "}, {"spec": "含\n换行"}, {"spec": strings.Repeat("名", 33)}} {
		if _, err = MergeProductFieldLabels(nil, patch); err == nil {
			t.Fatal("invalid field name accepted")
		}
	}
	p.FieldLabels = map[string]string{"spec": "相同名称", "quantity": "相同名称"}
	if p.SourceFieldLabel("spec", "规格") == p.SourceFieldLabel("quantity", "数量") {
		t.Fatal("renamed fields collide in a source map")
	}
}
