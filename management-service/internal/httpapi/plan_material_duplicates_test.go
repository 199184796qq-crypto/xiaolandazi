package httpapi

import (
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaterialDuplicateRouteRequiresActor(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/material-claims/resolve-duplicates", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.planMaterialDuplicates(w, r)
	if w.Code != 401 {
		t.Fatalf("unauthenticated duplicate write: %d", w.Code)
	}
}

func TestMaterialDuplicateObjectBoundary(t *testing.T) {
	links := []materialLinkObject{{ID: 101, LinkKey: "1号链接"}, {ID: 102, LinkKey: "2号链接"}}
	input := model.PlanMaterialClaimInput{ClaimKey: "name", Topic: "商品名", Content: "杨鸭子", Value: "杨鸭子", SourceQuote: "商品名：杨鸭子", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}}}}
	stored := model.PlanMaterialClaim{ID: 1, ClaimKey: "name", Topic: "商品名", Content: "杨鸭子", Value: input.Value, SourceQuote: input.SourceQuote, MaterialClaimDetails: input.MaterialClaimDetails}
	expected := []db.MaterialClaimExpectation{{ID: 1}}
	if _, _, err := materialDuplicateObjectPlan([]model.PlanMaterialClaim{stored}, expected, input, "", links); err == nil {
		t.Fatal("unscoped legacy client accepted")
	}
	for _, owner := range []model.MaterialObjectTarget{{Kind: "plan"}, {Kind: "product", LinkID: 102}, {Kind: "store", StoreName: "王家店"}} {
		stored.ObjectTargets = []model.MaterialObjectTarget{owner}
		if _, _, err := materialDuplicateObjectPlan([]model.PlanMaterialClaim{stored}, expected, input, "link:101", links); err == nil {
			t.Fatal("cross object deletion accepted", owner)
		}
	}
	stored.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}, {Kind: "product", LinkID: 102}}
	rest, split, err := materialDuplicateObjectPlan([]model.PlanMaterialClaim{stored}, expected, input, "link:101", links)
	if err != nil || !split || len(rest) != 1 || rest[0].ObjectTargets[0].LinkID != 102 || rest[0].Content != stored.Content {
		t.Fatal("unrelated owner not preserved", rest, err)
	}
	stored.ObjectTargets = input.ObjectTargets
	rest, split, err = materialDuplicateObjectPlan([]model.PlanMaterialClaim{stored}, expected, input, "link:101", links)
	if err != nil || split || len(rest) != 0 {
		t.Fatal("ordinary same object failed", err)
	}
	stored.SourceQuote = "另一份原文"
	if _, _, err := materialDuplicateObjectPlan([]model.PlanMaterialClaim{stored}, expected, input, "link:101", links); err == nil {
		t.Fatal("different original accepted as duplicate")
	}
	stored.SourceQuote = input.SourceQuote
	stored.Value = "另一个值"
	if _, _, err := materialDuplicateObjectPlan([]model.PlanMaterialClaim{stored}, expected, input, "link:101", links); err == nil {
		t.Fatal("different value accepted as duplicate")
	}
}

func TestMaterialImportExactDuplicatesSeparateFromExistingAttribute(t *testing.T) {
	a := model.PlanMaterialClaimInput{ClaimKey: "price", Value: "87", SourceQuote: "价格87", MaterialClaimDetails: model.MaterialClaimDetails{ClientID: "a", Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}}}}
	b := a
	b.ClientID = "b"
	choices := func() []materialImportChoice {
		return []materialImportChoice{{Item: a, Action: "append"}, {Item: b, Action: "append"}}
	}
	if _, _, _, err := materialImportChanges(choices(), nil, nil, nil); err == nil {
		t.Fatal("exact duplicate drafts accepted")
	}
	for _, change := range []string{"value", "source"} {
		b = a
		b.ClientID = "b"
		if change == "value" {
			b.Value = "88"
		} else {
			b.SourceQuote = "另一原文"
		}
		got, _, _, err := materialImportChanges(choices(), nil, nil, nil)
		if err != nil || len(got) != 2 {
			t.Fatal("non-duplicate drafts rejected", change, err)
		}
	}
	b = a
	b.ClientID = "b"
	b.Value = "88"
	if _, _, _, err := materialImportChanges([]materialImportChoice{{Item: b, Action: "append"}}, []model.PlanMaterialClaimInput{a}, []int64{1}, nil); err == nil {
		t.Fatal("changed existing attribute bypassed manual keep/replace")
	}
}
