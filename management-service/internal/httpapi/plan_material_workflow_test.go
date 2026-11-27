package httpapi

import (
	"livecompanion/management/internal/model"
	"net/http/httptest"
	"testing"
)

func TestMaterialWorkflowAcceptsOnlyAppend(t *testing.T) {
	item := model.PlanMaterialClaimInput{
		Content:              "汁水足、一口爆汁",
		MaterialClaimDetails: model.MaterialClaimDetails{ClientID: "taste"},
	}
	for _, action := range []string{"", "keep", "replace"} {
		if _, err := materialImportItems([]materialImportChoice{{Item: item, Action: action}}); err == nil {
			t.Fatalf("accepted retired import action %q", action)
		}
	}
	items, err := materialImportItems([]materialImportChoice{{Item: item, Action: "append"}})
	if err != nil || len(items) != 1 || items[0].Content != item.Content {
		t.Fatalf("append rejected or changed draft: %v, %v", items, err)
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
