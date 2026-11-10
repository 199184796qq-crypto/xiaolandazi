package httpapi

import (
	"encoding/json"
	"errors"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestMaterialSplitRequiresActor(t *testing.T) {
	w := httptest.NewRecorder()
	(&Server{}).planMaterialClaimSplit(w, httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/material-claims/10/split", nil))
	if w.Code != 401 {
		t.Fatalf("unauthenticated split=%d", w.Code)
	}
}

func TestMaterialSplitPreservesSourceOwnersAndRules(t *testing.T) {
	old := model.PlanMaterialClaim{ID: 10, TenantID: 14, PlanID: 4, ClaimKey: "heating", Topic: "加热方法", Content: "微波炉1分钟，空气炸锅3分钟", Value: "微波炉1分钟，空气炸锅3分钟", Polarity: "positive", FactType: "conditional", SourceQuote: "满99元，微波炉1分钟或空气炸锅3分钟，新疆除外", Conditions: json.RawMessage(`[{"field":"amount","op":"gte","values":["99"],"raw":"满99元","status":"resolved"}]`), MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}, {Kind: "product", LinkID: 102}}, Exceptions: json.RawMessage(`[{"field":"region","op":"in","values":["新疆"],"raw":"新疆除外","status":"resolved"}]`)}}
	links := []materialLinkObject{{ID: 101, LinkKey: "1号链接"}, {ID: 102, LinkKey: "2号链接"}}
	a := model.PlanMaterialClaimInput{ClaimKey: "heating", Topic: "加热方法", Content: "微波炉1分钟", Value: "微波炉1分钟", ReplacesID: 999, MaterialClaimDetails: model.MaterialClaimDetails{ClientID: "a", Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "plan"}}}}
	b := a
	b.ClientID = "b"
	b.Content = "空气炸锅3分钟"
	b.Value = b.Content
	existing := []model.PlanMaterialClaim{old}
	revision := db.MaterialClaimRevision(old)
	parts, err := materialSplitItems(existing, old.ID, revision, []model.PlanMaterialClaimInput{a, b}, links, 14, 4)
	if err != nil || len(parts) != 2 {
		t.Fatal(parts, err)
	}
	sameJSON := func(a, b []byte) bool {
		var x, y any
		_ = json.Unmarshal(a, &x)
		_ = json.Unmarshal(b, &y)
		return reflect.DeepEqual(x, y)
	}
	for _, p := range parts {
		if len(p.ObjectTargets) != 2 || p.ObjectTargets[0].LinkID != 101 || p.ObjectTargets[1].LinkID != 102 || !sameJSON(p.Conditions, old.Conditions) || !sameJSON(p.Exceptions, old.Exceptions) || p.SourceQuote != old.SourceQuote || p.ReplacesID != 0 || p.NeedsConfirmation || p.NeedsSplit {
			t.Fatal("source ownership/rules dropped", p)
		}
	}
	if !reflect.DeepEqual(existing, []model.PlanMaterialClaim{old}) {
		t.Fatal("preparation mutated stored source")
	}
	changed := old
	changed.Content = "已修改"
	if _, err = materialSplitItems([]model.PlanMaterialClaim{changed}, old.ID, revision, []model.PlanMaterialClaimInput{a, b}, links, 14, 4); !errors.Is(err, db.ErrMaterialClaimConflict) {
		t.Fatal("stale source accepted", err)
	}
	if _, err = materialSplitItems(nil, old.ID, revision, []model.PlanMaterialClaimInput{a, b}, links, 14, 4); !errors.Is(err, db.ErrMaterialClaimConflict) {
		t.Fatal("missing/foreign source accepted", err)
	}
	if _, err = materialSplitItems(existing, old.ID, revision, []model.PlanMaterialClaimInput{a, a}, links, 14, 4); err == nil {
		t.Fatal("duplicate client IDs accepted")
	}
	c := a
	c.ClientID = "c"
	if _, err = materialSplitItems(existing, old.ID, revision, []model.PlanMaterialClaimInput{a, c}, links, 14, 4); err == nil {
		t.Fatal("duplicate split rows accepted")
	}
	for _, items := range [][]model.PlanMaterialClaimInput{nil, make([]model.PlanMaterialClaimInput, 501)} {
		if _, err = materialSplitItems(existing, old.ID, revision, items, links, 14, 4); err == nil {
			t.Fatal("invalid split size accepted")
		}
	}
	a.Content = ""
	a.Value = ""
	if _, err = materialSplitItems(existing, old.ID, revision, []model.PlanMaterialClaimInput{a}, links, 14, 4); err == nil {
		t.Fatal("empty item accepted")
	}
}
