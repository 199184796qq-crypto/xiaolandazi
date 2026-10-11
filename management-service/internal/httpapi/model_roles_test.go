package httpapi

import (
	"context"
	"livecompanion/management/internal/agentgateway"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkbenchReviewUsesConfiguredReviewRole(t *testing.T) {
	cfg := agentgateway.SpeechModels{SystemDefaultID: "analysis", DefaultID: "speech", ReviewDefaultID: "review"}
	for _, stage := range []string{"lecture_task_review", "fact_fields_normalize_review", "fact_expression_examples_review", "lecture_detail_normalize_review", "mainline_p0_context"} {
		if got := workbenchProfileForStage(context.Background(), cfg, "old-analysis", stage); got != "review" {
			t.Fatal(stage, got)
		}
	}
	if got := workbenchProfileForStage(context.Background(), cfg, "old-speech", "mainline_text"); got != "speech" {
		t.Fatal(got)
	}
	if got := workbenchProfileForStage(withPinnedSpeechProfile(context.Background(), "pinned"), cfg, "pinned", "mainline_text"); got != "pinned" {
		t.Fatal(got)
	}
	if got := workbenchProfileForStage(context.Background(), cfg, "old", "fact_fields_normalize"); got != "analysis" {
		t.Fatal(got)
	}
	cfg.ReviewDefaultID = ""
	if got := workbenchProfileForStage(context.Background(), cfg, "old", "lecture_task_review"); got != "analysis" {
		t.Fatal("legacy fallback changed", got)
	}
}

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
