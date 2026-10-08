package db

import (
	"encoding/json"
	"testing"
)

func TestDefaultAliResourcesMergePreservesRolesAndOtherOptions(t *testing.T) {
	raw := `{"style_profile_id":"chosen-style","safety_profile_id":"chosen-safety","future_option":{"keep":true},"cycle":{"credential_profile_id":"chosen-ali-key","feedback_profile_id":"chosen-feedback","threshold":42,"environment_id":"env_selected"}}`
	merged, err := mergeAnchorDefaultResources(raw, "ws_default", "agent_default", "cn-beijing")
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	_ = json.Unmarshal([]byte(merged), &result)
	cycle := result["cycle"].(map[string]any)
	if result["style_profile_id"] != "chosen-style" || result["safety_profile_id"] != "chosen-safety" || cycle["credential_profile_id"] != "chosen-ali-key" || cycle["feedback_profile_id"] != "chosen-feedback" || cycle["threshold"] != float64(42) || cycle["environment_id"] != "env_selected" || cycle["workspace_id"] != "ws_default" || cycle["agent_id"] != "agent_default" {
		t.Fatal(result)
	}
	if result["future_option"].(map[string]any)["keep"] != true {
		t.Fatal("dropped unknown option")
	}
}
func TestDefaultAliResourcesInitializeEmptyJSONAndRejectInvalidCycle(t *testing.T) {
	if _, err := mergeAnchorDefaultResources(`{}`, "ws_default", "agent_default", "cn-beijing"); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeAnchorDefaultResources(`{"cycle":"broken"}`, "ws_default", "agent_default", "cn-beijing"); err == nil {
		t.Fatal("overwrote invalid stored options")
	}
}
