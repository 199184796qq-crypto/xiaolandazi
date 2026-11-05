package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTemplateCatalogEndpointsRequireAuthentication(t *testing.T) {
	s := &Server{}
	for _, test := range []struct {
		method, path string
		handler      http.HandlerFunc
	}{
		{"GET", "/api/v1/system/plan-prompt-template-catalog", s.planPromptCatalogGet},
		{"PUT", "/api/v1/system/plan-prompt-template-catalog", s.planPromptCatalogPut},
		{"GET", "/api/v1/live-agent-plans/4/prompt/templates?page=persona_md", s.liveAgentPlanPromptTemplates},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`))
			test.handler(w, r)
			if w.Code != 401 {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}
