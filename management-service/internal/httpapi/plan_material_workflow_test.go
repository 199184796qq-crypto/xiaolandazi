package httpapi

import (
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"testing"
)

func TestMaterialWorkflowRequiresManualChoices(t *testing.T) {
	a := model.PlanMaterialClaimInput{ClaimKey: "price", Topic: "价格", Content: "87.6元", Value: "87.6元", MaterialClaimDetails: model.MaterialClaimDetails{ClientID: "a", Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}}}}
	b := a
	b.ClientID = "b"
	b.Content = "88.6元"
	b.Value = b.Content
	for _, action := range []string{"", "append"} {
		if _, _, _, err := materialImportChanges([]materialImportChoice{{b, action}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil); err == nil {
			t.Fatal("existing match accepted without manual choice", action)
		}
	}
	items, removed, kept, err := materialImportChanges([]materialImportChoice{{b, "keep"}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil)
	if err != nil || len(items) != 0 || len(removed) != 0 || kept != 1 {
		t.Fatal("keep altered existing", err)
	}
	items, removed, kept, err = materialImportChanges([]materialImportChoice{{b, "replace"}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil)
	if err != nil || len(items) != 1 || len(removed) != 1 || removed[0] != 10 || kept != 0 {
		t.Fatal("replace not explicit", err)
	}
	c := b
	c.ClientID = "c"
	c.Content = "99.6元"
	c.Value = c.Content
	c.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 102}}
	if _, _, _, err = materialImportChanges([]materialImportChoice{{c, "append"}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil); err != nil {
		t.Fatal("independent new card not appended", err)
	}
	a.ObjectTargets = append(a.ObjectTargets, c.ObjectTargets...)
	partial, removedPartial, _, partialErr := materialImportChanges([]materialImportChoice{{b, "replace"}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil)
	if partialErr != nil || len(partial) != 2 || len(removedPartial) != 1 || len(partial[1].ObjectTargets) != 1 || partial[1].ObjectTargets[0].LinkID != 102 || partial[1].Content != a.Content {
		t.Fatal("partial replacement lost unrelated owner", partial, partialErr)
	}
	if _, _, _, err = materialImportChanges([]materialImportChoice{{b, "replace"}, {c, "keep"}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil); err == nil {
		t.Fatal("inconsistent shared old choice accepted")
	}
	items, removed, _, err = materialImportChanges([]materialImportChoice{{b, "replace"}, {c, "replace"}}, []model.PlanMaterialClaimInput{a}, []int64{10}, nil)
	if err != nil || len(items) != 2 || len(removed) != 1 {
		t.Fatal("shared old row replaced twice", err)
	}
}

func TestMaterialWorkflowRoutesRequireActor(t *testing.T) {
	s := &Server{}
	for _, path := range []string{"clear", "commit-import"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/material-claims/"+path, nil)
		if path == "clear" {
			s.planMaterialClear(w, r)
		} else {
			s.planMaterialCommitImport(w, r)
		}
		if w.Code != 401 {
			t.Fatalf("%s unauthenticated=%d", path, w.Code)
		}
	}
}
