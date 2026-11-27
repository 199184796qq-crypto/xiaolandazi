package agentgateway

import "testing"

func TestAutoScriptStagesUseOperatorReviewModel(t *testing.T) {
	for _, stage := range []string{"script_auto_planner", "script_auto_writer", "script_auto_fact_review"} {
		if !UsesReviewModel(stage) || !UsesOperatorPinnedModel(stage) {
			t.Fatalf("automatic script stage %q bypasses the operator review model", stage)
		}
	}
}
