package paidpipeline

import (
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/strategycenter"
	"testing"
)

func TestRoomConfigurationFrozenUntilNextStart(t *testing.T) {
	r := agentwork.New()
	q := agentdecision.New()
	policies := strategycenter.New()
	cfg, err := strategycenter.LoadInteractionMother()
	if err != nil {
		t.Fatal(err)
	}
	prefs := strategycenter.DefaultRoomInteractionPreferences(7)
	prefs.TenantID = 2
	prefs.Schedule = &cfg
	policies.PutRoomInteractionPreferences(7, prefs)
	p := New(r, q, policies)
	r.Set(7, agentwork.StateWorking)
	first, err := p.schedule(7, 2)
	if err != nil {
		t.Fatal(err)
	}
	rule := cfg.Events["product_question"]
	rule.Weight = 10
	cfg.Events["product_question"] = rule
	cfg.Version = 2
	prefs.Schedule = &cfg
	policies.PutRoomInteractionPreferences(7, prefs)
	same, _ := p.schedule(7, 2)
	if same.Events["product_question"].Weight != first.Events["product_question"].Weight {
		t.Fatal("running session changed silently")
	}
	r.Set(7, agentwork.StateStopped)
	r.Set(7, agentwork.StateWorking)
	next, _ := p.schedule(7, 2)
	if next.Version != 2 || next.Events["product_question"].Weight != 10 {
		t.Fatal("next start did not use saved configuration")
	}
}
