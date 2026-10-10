package httpapi

import (
	"context"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"testing"
)

func TestMissingP0NeverReachesPlannerOrWriter(t *testing.T) {
	// No store or provider is installed: passing the policy gate would panic.
	server := &Server{}
	state := &anchorcycle.State{}
	if _, _, err := server.completeCycleText(context.Background(), state); err == nil {
		t.Fatal("writer accepted missing P0")
	}
	server.ensureCycleAnglePlan(context.Background(), state, agentgateway.SpeechModel{})
	if len(state.Warnings) == 0 || len(state.AnglePlans) != 0 {
		t.Fatal("planner accepted missing P0")
	}
	if err := installP0Mother(state); err != nil {
		t.Fatal(err)
	}
	if err := requireCycleP0(state); err != nil {
		t.Fatal("effective policy rejected", err)
	}
	state.P0PolicyMD = "invalid policy document"
	if err := requireCycleP0(state); err == nil {
		t.Fatal("invalid P0 accepted")
	}
}
