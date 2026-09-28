package db

import (
	"testing"

	"livecompanion/management/internal/agentrouting"
)

func TestAgentRoutingDefaultValueIsPermanentBaseline(t *testing.T) {
	if !preserveAgentPromptDefaultValue(agentrouting.ConfigKey) {
		t.Fatal("agent routing default_value must be preserved as the permanent system baseline")
	}
	if preserveAgentPromptDefaultValue("live.answer.system") {
		t.Fatal("ordinary prompt defaults should keep existing seed-update behavior")
	}
}
