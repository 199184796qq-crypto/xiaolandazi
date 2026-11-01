package paidpipeline

import (
	"livecompanion/core/internal/strategycenter"
	"testing"
	"time"
)

func TestTimeReportControlsWithdrawAndHotExtraCooldown(t *testing.T) {
	now := time.Now().UTC()
	p, q, _ := reportProcessor(t, &now, 24)
	state := p.timeReports[29]
	state.Due = now.Add(time.Hour)
	due := state.Due
	pref := p.policies.RoomInteractionPreferences(29)
	on := true
	pref.InteractionActive = &on
	pref.ControlsRevision = 1
	pref.Cooldowns = map[string]int{"time_report": 120}
	p.policies.PutRoomInteractionPreferences(29, pref)
	p.flushTimeReports(now)
	if !state.Due.Equal(due.Add(120 * time.Second)) {
		t.Fatal("hot report extra not applied", state.Due, due)
	}
	state.Due = now.Add(-time.Second)
	p.flushTimeReports(now)
	old := state.Decision
	if old == "" {
		t.Fatal("missing report")
	}
	// Re-enabled latest state records a skipped intermediate off update.
	pref.ControlsRevision = 3
	pref.DisabledAt = map[string]uint64{"time_report": 2}
	p.policies.PutRoomInteractionPreferences(29, pref)
	p.flushTimeReports(now)
	if _, ok := q.Get(29, old); ok {
		t.Fatal("retired report survived fast off/on")
	}
	if p.timeReports[29].ControlsRevision != 3 || p.timeReports[29].Decision != "" {
		t.Fatal("old report quota revived")
	}
	pref.Switches = map[string]bool{"time_report": false}
	pref.ControlsRevision = 4
	pref.DisabledAt["time_report"] = 4
	p.policies.PutRoomInteractionPreferences(29, pref)
	p.ApplyInteractionControls(29)
	if p.timeReports[29] != nil {
		t.Fatal("disabled report still owed")
	}
	if !pref.ControlEnabled("welcome") || strategycenter.ControlKind("time_report") != "time_report" {
		t.Fatal("unrelated interaction changed")
	}
}

func TestExplicitRoomSwitchReplacesRetiredLegacyOffSetting(t *testing.T) {
	now := time.Now().UTC()
	p, _, _ := reportProcessor(t, &now, 24)
	pref := p.policies.RoomInteractionPreferences(29)
	rule := pref.Schedule.Events["welcome"]
	rule.Enabled = false
	rule.Weight = 0
	pref.Schedule.Events["welcome"] = rule
	pref.OverallInteraction = 0
	p.policies.PutRoomInteractionPreferences(29, pref)
	scope := p.v2Scope(14, 29)
	if p.v2Policy(scope).Enabled["welcome"] {
		t.Fatal("unconfigured old room behavior changed")
	}
	on := true
	pref.InteractionActive = &on
	pref.Switches = map[string]bool{"welcome": true}
	pref.ControlsRevision = 1
	p.policies.PutRoomInteractionPreferences(29, pref)
	if !p.v2Policy(scope).Enabled["welcome"] {
		t.Fatal("visible on switch blocked by retired hidden setting")
	}
	pref.Switches["welcome"] = false
	pref.ControlsRevision = 2
	p.policies.PutRoomInteractionPreferences(29, pref)
	policy := p.v2Policy(scope)
	if policy.Enabled["welcome"] || !policy.Disabled["welcome"] {
		t.Fatal("new off switch not applied")
	}
}
