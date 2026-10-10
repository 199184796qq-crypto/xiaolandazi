package anchorgeneration

import "testing"

func TestUnifiedModelRolesIgnoreStalePerFeatureSelections(t *testing.T) {
	s := Default()
	s.StyleProfileID = "old-style"
	s.SellingPointProfileID = "old-miner"
	s.Cycle.TextProfileID = "old-text"
	s.Cycle.Threshold = 73
	s.ApplyModelRoles("system", "speech")
	if s.StyleProfileID != "system" || s.ScorecardProfileID != "system" || s.SafetyProfileID != "system" || s.SellingPointProfileID != "system" || s.InteractionProfileID != "speech" || s.Cycle.TextProfileID != "speech" || s.Cycle.FeedbackID != "speech" || s.Cycle.Threshold != 73 {
		t.Fatal("unified role mapping or business parameters lost", s)
	}
}
