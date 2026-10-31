package agentdecision

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWelcomePreparedAdmissionFixedThirtySecondLease(t *testing.T) {
	now := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 10)
	raw, _ := json.Marshal(map[string]any{"protocol": "interaction_v2", "task": "w", "epoch": "boot", "welcome_quota": true, "expires_at": now.Add(30 * time.Second)})
	v := q.Enqueue(29, Candidate{Topic: "welcome-w", PurposeKnowledge: raw, TTLSeconds: 30}).Item
	if v == nil {
		t.Fatal("not enqueued")
	}
	deadline := now.Add(40 * time.Second)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["expires_at"] = deadline
	m["welcome_ready_at"] = now.Add(10 * time.Second)
	ready, _ := json.Marshal(m)
	if q.AdmitWelcome(29, v.ID, deadline, ready) {
		t.Fatal("unclaimed producer admitted audio")
	}
	q.ClaimNext(29)
	now = now.Add(10 * time.Second)
	if !q.AdmitWelcome(29, v.ID, deadline, ready) {
		t.Fatal("ready welcome not admitted")
	}
	for i := 0; i < 3; i++ {
		now = now.Add(time.Second)
		if !q.AdmitWelcome(29, v.ID, deadline, ready) {
			t.Fatal("idempotent ready failed")
		}
		q.RescheduleCut(29, v.ID)
		now = now.Add(time.Second)
		q.ClaimNext(29)
		got, _ := q.Get(29, v.ID)
		if !got.ExpiresAt.Equal(deadline) || !got.PreparedUntil.Equal(deadline) {
			t.Fatal("cut retry extended welcome")
		}
	}
	m["welcome_ready_at"] = now
	m["expires_at"] = now.Add(30 * time.Second)
	changed, _ := json.Marshal(m)
	if q.AdmitWelcome(29, v.ID, now.Add(30*time.Second), changed) {
		t.Fatal("producer renewed old rendition")
	}
	q.RescheduleCut(29, v.ID)
	now = deadline
	if _, ok := q.ClaimNext(29); ok {
		t.Fatal("prepared protection overrode 30 second welcome TTL")
	}
}
