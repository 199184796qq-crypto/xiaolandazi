package paidpipeline

import (
	"encoding/json"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/strategycenter"
)

func reportProcessor(t *testing.T, now *time.Time, capacity int) (*Processor, *agentdecision.Queue, *agentwork.Registry) {
	t.Helper()
	r := agentwork.New()
	r.SetPlan(29, 4, "report")
	r.SetMode(29, agentwork.ModeAnchor)
	r.StartAgent(29, 0)
	q := agentdecision.NewWithClock(func() time.Time { return *now }, time.Minute, time.Minute, capacity)
	policies := strategycenter.New()
	cfg, err := strategycenter.LoadInteractionMother()
	if err != nil {
		t.Fatal(err)
	}
	rule := cfg.Events["warmup"]
	rule.Enabled = false
	cfg.Events["warmup"] = rule
	prefs := strategycenter.DefaultRoomInteractionPreferences(29)
	prefs.TenantID = 14
	prefs.Schedule = &cfg
	policies.PutRoomInteractionPreferences(29, prefs)
	p := New(r, q, policies)
	p.now = func() time.Time { return *now }
	p.SetBrain(roombrain.NewManager())
	p.timeReports[29] = &timeReportSchedule{Epoch: p.cutEpoch(29), Due: now.Add(-time.Second)}
	return p, q, r
}

func TestTimeReportEntitlementSettlesOnlyOnComplete(t *testing.T) {
	now := time.Now().UTC()
	p, q, _ := reportProcessor(t, &now, 24)
	p.flushTimeReports(now)
	state := p.timeReports[29]
	due := state.Due
	id := state.Decision
	if id == "" || state.Topic == "" {
		t.Fatal("missing entitlement")
	}
	item, ok := q.ClaimNext(29)
	if !ok || item.ID != id || item.PriorityClass != "P0" {
		t.Fatal(item)
	}
	q.BindPlayback(29, id, "playing-task")
	now = now.Add(5 * time.Second)
	p.flushTimeReports(now)
	if !state.Due.Equal(due) || state.Decision != id {
		t.Fatal("claim/start consumed report entitlement")
	}
	q.Complete(29, id)
	p.flushTimeReports(now)
	if state.Decision != "" || state.Topic != "" || state.Due.Before(now.Add(90*time.Minute)) || state.Due.After(now.Add(240*time.Minute)) {
		t.Fatal("not settled from completion clock", state)
	}
	due = state.Due
	p.flushTimeReports(now.Add(time.Second))
	if !due.Equal(state.Due) {
		t.Fatal("receipt consumed twice")
	}
}

func TestTimeReportQueueFullAndFailureKeepSameEntitlement(t *testing.T) {
	now := time.Now().UTC()
	p, q, _ := reportProcessor(t, &now, 1)
	block := q.Enqueue(29, agentdecision.Candidate{Source: agentdecision.SourceManual, Topic: "manual", Question: "occupied", Priority: 150}).Item
	p.flushTimeReports(now)
	state := p.timeReports[29]
	topic, due := state.Topic, state.Due
	if state.Decision != "" || topic == "" || due.After(now) {
		t.Fatal("admission failure consumed entitlement", state)
	}
	q.Remove(29, block.ID)
	now = now.Add(10 * time.Second)
	p.flushTimeReports(now)
	id := state.Decision
	if id == "" {
		t.Fatal("full queue did not recover")
	}
	q.Remove(29, id)
	p.flushTimeReports(now)
	now = now.Add(9 * time.Second)
	p.flushTimeReports(now)
	if state.Decision != "" {
		t.Fatal("failure ignored 10s backoff")
	}
	now = now.Add(time.Second)
	p.flushTimeReports(now)
	if state.Decision == "" || state.Decision == id || state.Topic != topic || !state.Due.Equal(due) {
		t.Fatal("retry lost or duplicated quota", state)
	}
}

func TestTimeReportExpiryRetriesAndNewRunResets(t *testing.T) {
	now := time.Now().UTC()
	p, q, r := reportProcessor(t, &now, 24)
	p.flushTimeReports(now)
	old := *p.timeReports[29]
	now = now.Add(11 * time.Minute)
	p.flushTimeReports(now)
	now = now.Add(10 * time.Second)
	p.flushTimeReports(now)
	if state := p.timeReports[29]; state.Decision == "" || state.Decision == old.Decision || state.Topic != old.Topic {
		t.Fatal("expired report did not keep entitlement", state)
	}
	r.Set(29, agentwork.StateStopped)
	r.StartAgent(29, 0)
	p.flushTimeReports(now)
	if state := p.timeReports[29]; state.Epoch == old.Epoch || state.Topic != "" || state.Decision != "" || !state.Due.After(now) {
		t.Fatal("new run inherited old quota", state)
	}
	if len(q.Snapshot(29).Queue) != 0 {
		t.Fatal("old attempt survived new run")
	}
	r.Set(29, agentwork.StateStopped)
	p.flushTimeReports(now)
	if len(p.timeReports) != 0 {
		t.Fatal("stopped run retained report debt")
	}
}

func TestTimeReportActualAudioValidityAtPreparationAndStart(t *testing.T) {
	now := time.Now().UTC()
	p, q, _ := reportProcessor(t, &now, 24)
	p.flushTimeReports(now)
	item, _ := q.ClaimNext(29)
	rawMissing, _ := json.Marshal(map[string]any{"decision_id": item.ID, "duration_ms": 8000, "start_at": now})
	if err := p.reserveAudioV2(messagecaptain.Message{RoomID: 29, Payload: rawMissing}); err == nil {
		t.Fatal("report without original time validity accepted")
	}
	if !q.SetTimeReportValidity(29, item.ID, now.Add(30*time.Second)) {
		t.Fatal("deadline not bound")
	}
	reserve := func(start time.Time, duration int) error {
		raw, _ := json.Marshal(map[string]any{"decision_id": item.ID, "duration_ms": duration, "start_at": start})
		return p.reserveAudioV2(messagecaptain.Message{RoomID: 29, Payload: raw})
	}
	if err := reserve(now.Add(5*time.Second), 8000); err != nil {
		t.Fatal(err)
	}
	// Same audio after waiting must be checked again with the actual start.
	now = now.Add(25 * time.Second)
	if err := reserve(now, 8000); err == nil {
		t.Fatal("stale actual start crossed semantic deadline")
	}
	if err := reserve(now, 0); err == nil {
		t.Fatal("unknown duration bypassed report validity")
	}
	p.flushTimeReports(now)
	if p.timeReports[29].Decision == "" {
		t.Fatal("reservation rejection settled entitlement")
	}
}
