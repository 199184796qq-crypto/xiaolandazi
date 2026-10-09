package anchorgeneration

import "testing"

func TestSharedFeedbackFollowsMainlineAndPreservesOtherRoles(t *testing.T) {
	cfg := Default()
	cfg.StyleProfileID, cfg.ScorecardProfileID, cfg.SellingPointProfileID = "style", "score", "miner"
	cfg.Cycle.TextProfileID, cfg.Cycle.FeedbackID = "flash", "old-omni"
	cfg.InteractionProfileID, cfg.SafetyProfileID = "interaction", "safety"
	cfg.NormalizeSharedFeedback()
	if cfg.Cycle.FeedbackID != "flash" || cfg.StyleProfileID != "style" || cfg.SafetyProfileID != "safety" || cfg.InteractionProfileID != "interaction" {
		t.Fatal(cfg)
	}
	cfg.Cycle.TextProfileID = "another-model"
	cfg.NormalizeSharedFeedback()
	if cfg.Cycle.FeedbackID != "another-model" {
		t.Fatal("feedback retained stale model")
	}
	cfg.Cycle.FeedbackID = ""
	if err := cfg.Validate(); err != nil {
		t.Fatal("derived role must validate without independent feedback selection", err)
	}
}

func TestAgentFeedbackUsesExistingTextConnection(t *testing.T) {
	cfg := Default()
	cfg.Cycle.CredentialID, cfg.Cycle.FeedbackID = "ali-connection", "old-feedback"
	cfg.InteractionProfileID = "interaction-connection"
	cfg.NormalizeSharedFeedback()
	if cfg.Cycle.FeedbackID != "interaction-connection" {
		t.Fatal(cfg)
	}
	cfg.InteractionProfileID = ""
	cfg.NormalizeSharedFeedback()
	if cfg.Cycle.FeedbackID != "ali-connection" {
		t.Fatal(cfg)
	}
}
