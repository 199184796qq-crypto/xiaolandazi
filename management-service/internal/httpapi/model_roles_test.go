package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInternalAnalysisRejectsUnauthenticatedAndInvalidRequests(t *testing.T) {
	s := &Server{purposeInternalToken: "fixture-core-token"}
	for _, c := range []struct {
		token, body string
		status      int
	}{{"", `{}`, 401}, {"Bearer wrong", `{}`, 401}, {"Bearer fixture-core-token", `{"model":"old-model","messages":[],"stream":false,"max_tokens":100}`, 400}, {"Bearer fixture-core-token", `{"messages":[{"role":"tool","content":"x"}],"max_tokens":100}`, 400}} {
		r := httptest.NewRequest(http.MethodPost, "/internal/v1/model-analysis/chat/completions", strings.NewReader(c.body))
		r.Header.Set("Authorization", c.token)
		w := httptest.NewRecorder()
		s.internalModelAnalysis(w, r)
		if w.Code != c.status {
			t.Fatal("unexpected authentication or protocol result", w.Code, w.Body.String())
		}
	}
}
