package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCycleProgressStreamOptInAndMilestones(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/live-agent-plans/2/realtime-preview", nil)
	r.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	result, req := cycleProgressStream(w, r)
	for _, percent := range []int{25, 20, 75, 75, 95} {
		reportCycleProgress(context.WithoutCancel(req.Context()), percent)
	}
	writeJSON(result, 200, map[string]any{"run": map[string]string{"run_id": "test"}})
	body := w.Body.String()
	if strings.Count(body, "event: progress") != 4 || !strings.Contains(body, `"percent":95`) || strings.Contains(body, `"percent":100`) || !strings.Contains(body, "event: complete") {
		t.Fatal(body)
	}
	for _, forbidden := range []string{"TTS", "OSS", "评分", "API", "模型"} {
		if strings.Contains(body, forbidden) {
			t.Fatal("internal stage exposed", body)
		}
	}
}

func TestCycleProgressJSONAndFormalUnchanged(t *testing.T) {
	for _, path := range []string{"/api/v1/live-agent-plans/2/realtime-preview", "/api/v1/live-agent-plans/2/anchor-style-workbench/cycle"} {
		r := httptest.NewRequest("POST", path, nil)
		if strings.Contains(path, "workbench") {
			r.Header.Set("Accept", "text/event-stream")
		}
		w := httptest.NewRecorder()
		result, req := cycleProgressStream(w, r)
		reportCycleProgress(req.Context(), 90)
		writeJSON(result, 200, map[string]bool{"ready": true})
		if strings.Contains(w.Body.String(), "event:") || w.Code != 200 {
			t.Fatal(w.Body.String())
		}
	}
}

func TestCycleProgressTerminalFailureIsNotSuccess(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/live-agent-plans/2/realtime-preview", nil)
	r.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	result, req := cycleProgressStream(w, r)
	reportCycleProgress(req.Context(), 40)
	writeError(result, 503, "声音准备失败")
	if !strings.Contains(w.Body.String(), "event: error") || strings.Contains(w.Body.String(), "event: complete") {
		t.Fatal(w.Body.String())
	}
}
