package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/model"
)

func TestNormalizePlanTextUsesLongestConfirmedVariantFirst(t *testing.T) {
	terms := []model.LiveAgentPlanTerm{
		{
			ID:            1,
			CanonicalText: "绸都杨鸭子",
			Status:        "active",
			Variants: []model.LiveAgentPlanTermVariant{
				{ID: 11, VariantText: "丑都养鸭子", ConfirmationCount: 2},
				{ID: 12, VariantText: "养鸭子", ConfirmationCount: 1},
			},
		},
	}
	got := normalizePlanText("欢迎来到丑都养鸭子直播间，今天继续讲鸡。", terms)
	if got.NormalizedText != "欢迎来到绸都杨鸭子直播间，今天继续讲鸡。" {
		t.Fatalf("normalized=%q", got.NormalizedText)
	}
	if len(got.Applied) != 1 {
		t.Fatalf("applied=%#v", got.Applied)
	}
	if len(got.HotTerms) != 1 || got.HotTerms[0] != "绸都杨鸭子" {
		t.Fatalf("hot terms=%#v", got.HotTerms)
	}
}

func TestManagementProductionDoesNotRegisterDevRuntimeRoutes(t *testing.T) {
	server := &Server{env: "production"}
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/internal/v1/dev/runtime/models"},
		{http.MethodPost, "/internal/v1/dev/runtime/answer-tts"},
		{http.MethodPost, "/internal/v1/dev/runtime/synthesize-text"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s status=%d want=404 body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestManagementDevelopmentRegistersDevRuntimeRoutes(t *testing.T) {
	server := &Server{env: "development"}
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/dev/runtime/answer-tts", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatalf("development route was not registered")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=400 body=%s", rec.Code, rec.Body.String())
	}
}
