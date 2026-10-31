package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
)

func TestReportHTTPRequiresOriginalSemanticDeadline(t *testing.T) {
	now := time.Now().UTC()
	q := agentdecision.NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 4)
	q.Enqueue(1, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "report", MissionKind: "time_report", Question: "时间"})
	item, _ := q.ClaimNext(1)
	if bindTimeReportDeadline(q, 1, *item, time.Time{}, now) || bindTimeReportDeadline(q, 1, *item, now, now) {
		t.Fatal("missing/stale deadline passed")
	}
	if !bindTimeReportDeadline(q, 1, *item, now.Add(time.Minute), now) {
		t.Fatal("valid report rejected")
	}
	if !bindTimeReportDeadline(q, 1, agentdecision.Item{MissionKind: "order"}, time.Time{}, now) {
		t.Fatal("normal interaction changed")
	}
}

func TestTimeReportDeadlineThroughActualDispatchHTTP(t *testing.T) {
	s, _ := v2ServerItem(t)
	s.agentDecisions.ClearRoom(94)
	item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "INTERACTION:TIME_REPORT:run:1", MissionKind: "time_report", Question: "自然报时"}).Item
	s.agentDecisions.ClaimNext(94)
	for _, until := range []time.Time{{}, time.Now().Add(-time.Second), time.Now().Add(time.Minute)} {
		raw, _ := json.Marshal(map[string]any{"decision_id": item.ID, "audio_url": "http://unused.invalid/report.wav", "reply_text": "下午了。", "time_report_valid_until": until})
		r := httptest.NewRequest("POST", "/internal/v1/rooms/94/audio/interaction?tenant_id=7", bytes.NewReader(raw))
		r.SetPathValue("roomID", "94")
		out := httptest.NewRecorder()
		s.dispatchRoomAudioInteraction(out, r)
		if until.IsZero() || until.Before(time.Now()) {
			if out.Code != 409 {
				t.Fatal("HTTP admitted stale report before audio", out.Code, out.Body.String())
			}
		} else {
			// The fixture intentionally has no output service. The deadline must
			// still cross the real JSON/auth/claim path and bind before that guard.
			bound, _ := s.agentDecisions.Get(94, item.ID)
			if out.Code != 503 || !bound.TimeReportValidUntil.Equal(until) {
				t.Fatal("deadline wire contract failed", out.Code, out.Body.String(), bound)
			}
		}
	}
}

func TestTimeReportCannotBeSettledByGenericCompleteHTTP(t *testing.T) {
	s, _ := v2ServerItem(t)
	s.agentDecisions.ClearRoom(94)
	item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "report", MissionKind: "time_report", Question: "报时"}).Item
	s.agentDecisions.ClaimNext(94)
	r := httptest.NewRequest("POST", "/internal/v1/rooms/94/agent/decisions/complete", nil)
	r.SetPathValue("roomID", "94")
	r.SetPathValue("decisionID", item.ID)
	out := httptest.NewRecorder()
	s.completeRoomAgentDecision(out, r)
	if out.Code != 409 {
		t.Fatal("generic completed request consumed report quota", out.Code, out.Body.String())
	}
	if _, ok := s.agentDecisions.Get(94, item.ID); !ok {
		t.Fatal("report removed before actual audio completion")
	}
}
