package db

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestValidateUnderstandingPolicyInput(t *testing.T) {
	valid, err := validateUnderstandingPolicyInput(model.AgentUnderstandingPolicyInput{
		ScopeType:           model.AgentUnderstandingScopeMembership,
		ScopeID:             3,
		Mode:                model.AgentUnderstandingModeAuto,
		Provider:            "qwen",
		Model:               "qwen3.8-flash",
		MaxContextMessages:  12,
		MaxTokens:           700,
		TimeoutMS:           9000,
		MonthlyBudgetTokens: 100000,
		BudgetFallback:      model.AgentUnderstandingBudgetFallbackProgram,
		MinConfidence:       0.8,
		Enabled:             true,
	})
	if err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if valid.ScopeID != 3 || valid.Mode != model.AgentUnderstandingModeAuto {
		t.Fatalf("unexpected normalized policy: %#v", valid)
	}

	if _, err := validateUnderstandingPolicyInput(model.AgentUnderstandingPolicyInput{
		ScopeType: model.AgentUnderstandingScopeTenant,
		ScopeID:   0,
		Mode:      model.AgentUnderstandingModeProgram,
	}); err == nil {
		t.Fatal("tenant policy without tenant id should fail")
	}

	if _, err := validateUnderstandingPolicyInput(model.AgentUnderstandingPolicyInput{
		ScopeType: model.AgentUnderstandingScopeSystem,
		Mode:      "magic",
	}); err == nil {
		t.Fatal("unknown mode should fail")
	}
}
