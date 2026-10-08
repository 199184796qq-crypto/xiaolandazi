package httpapi

import (
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"os"
	"strings"
	"testing"
)

func TestProductScopeOrdinaryAngleMayApplyToMultipleProducts(t *testing.T) {
	scope, facts, tasks := productScopeInputs()
	scope.Products = append(scope.Products, anchorcycle.FocusProduct{LinkKey: "3", Name: "另一商品"})
	tasks[0].SellingPoint, tasks[0].Angle = "包装", "真空包装与开袋即食"
	raw := strings.Replace(validScopeResponse, `"tasks":[{"id":1,"links":[],"shared":true,"comparison":false}]`, `"tasks":[{"id":1,"links":["1","2"],"shared":false,"comparison":false}]`, 1)
	if err := parseCycleProductScope(raw, scope, facts, tasks); err != nil {
		t.Fatal(err)
	}
	a := scope.Tasks[tasks[0].Key]
	if a.Shared || a.Comparison || len(a.Links) != 2 || hasScopeLink(a.Links, "3") {
		t.Fatal("scope was enlarged or changed into a comparison", a)
	}
	for _, bad := range []string{
		strings.Replace(raw, `"links":["1","2"]`, `"links":["1","99"]`, 1),
		strings.Replace(raw, `"links":["1","2"],"shared":false`, `"links":["1","2"],"shared":true`, 1),
		strings.Replace(validScopeResponse, `"links":["1"],"shared":false`, `"links":["2"],"shared":false`, 1),
	} {
		if parseCycleProductScope(bad, scope, facts, tasks) == nil {
			t.Fatal("invalid links, global sharing or product-card ownership accepted")
		}
	}
}

// Offline replay of actual failed model responses. This never calls a model,
// saves a plan, publishes a version or opens an audio output.
func TestProductScopeRecordedApplicabilityReplay(t *testing.T) {
	path := os.Getenv("ANCHOR_SCOPE_REPLAY_FIXTURE")
	if path == "" {
		t.Skip("explicit recorded fixture required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var records []struct {
		ExchangeID int64 `json:"exchange_id"`
		Inputs     struct {
			Products []anchorcycle.FocusProduct `json:"products"`
			Facts    []cycleScopeFactInput      `json:"facts"`
			Tasks    []cycleScopeTaskInput      `json:"tasks"`
		} `json:"inputs"`
		RawResponse string `json:"raw_response"`
	}
	if err = json.Unmarshal(raw, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) == 0 {
		t.Fatal("empty replay")
	}
	for _, r := range records {
		t.Run(fmt.Sprint(r.ExchangeID), func(t *testing.T) {
			scope := &anchorcycle.ProductFocus{Products: r.Inputs.Products}
			tasks := make([]anchorcycle.Task, len(r.Inputs.Tasks))
			for i, v := range r.Inputs.Tasks {
				tasks[i] = anchorcycle.Task{Key: fmt.Sprintf("task-%d", v.ID), SellingPoint: v.SellingPoint, Angle: v.Angle}
			}
			if err := parseCycleProductScope(r.RawResponse, scope, r.Inputs.Facts, tasks); err != nil {
				t.Fatal(err)
			}
			ordinaryMulti := 0
			for _, a := range scope.Tasks {
				if !a.Comparison && len(a.Links) > 1 {
					ordinaryMulti++
				}
			}
			if ordinaryMulti == 0 {
				t.Fatal("recorded false positive missing")
			}
			policy, _, err := loadCycleProductPolicy()
			if err != nil {
				t.Fatal(err)
			}
			scope.Policy = policy
			if err := scope.ValidateProducts(); err != nil {
				t.Fatal(err)
			}
			for _, product := range scope.Products {
				scope.CurrentLink = product.LinkKey
				scope.AuxiliaryActive = product.LinkKey != scope.MainLink()
				probe := anchorcycle.State{Queue: tasks, ProductFocus: scope, Phase: "opening", CycleNumber: 1}
				probe.SelectProductTask()
				if probe.Status == "spacing_blocked" || probe.Status == "compliance_blocked" {
					t.Fatalf("product %s has no usable individual angle", product.LinkKey)
				}
			}
			t.Logf("unchanged recorded result validates: facts=%d tasks=%d ordinary_multi=%d", len(scope.Facts), len(scope.Tasks), ordinaryMulti)
		})
	}
}
