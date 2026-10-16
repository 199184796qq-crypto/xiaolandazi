package httpapi

import (
	"livecompanion/management/internal/model"
	"testing"
)

func TestProductRenamedLabelIsUsedInVersionedGenerationFacts(t *testing.T) {
	p := model.LiveAgentPlanProductLink{ID: 5, LinkKey: "1号链接", VersionNo: 10, Quantity: "22道荤素菜", Audience: "10人", FieldLabels: map[string]string{"quantity": "套餐包含", "audience": "用餐人数"}}
	facts := compileAuthorizedGenerationFacts(nil, nil, []model.LiveAgentPlanProductLink{p})
	count := 0
	for _, f := range facts {
		if f.Predicate == "quantity" {
			if f.Label != "套餐包含" || f.Value != p.Quantity || f.FactID != "product_link:5:quantity:v10" {
				t.Fatal(f)
			}
			count++
		}
		if f.Predicate == "audience" {
			if f.Label != "用餐人数" || f.Value != "10人" {
				t.Fatal(f)
			}
			count++
		}
	}
	if count != 2 {
		t.Fatal("renamed facts lost")
	}
}
