package decisionexecutor

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestAnchorStyleAddressTermsUsesCardOverrideWithoutSampleBaseline(t *testing.T) {
	profile := model.LiveAgentPlanAnchorStyleProfile{Delivery: &model.LiveAnchorDeliverySpec{
		Version: "anchor-delivery/v1",
		Habits: []model.LiveAnchorLiteralHabit{
			{Kind: "audience_address", Text: "老乡们"},
		},
		AddressingOverride: &model.LiveAnchorAddressingOverride{
			AudienceMode:  "replace",
			AudienceTerms: []string{"朋友们", "哥姐们"},
		},
	}}
	got := anchorStyleAddressTerms(profile)
	want := map[string]bool{"朋友们": true, "哥姐们": true, "老乡们": true}
	if len(got) != len(want) {
		t.Fatalf("terms = %#v, want all of %#v", got, want)
	}
	for _, term := range got {
		if !want[term] {
			t.Fatalf("unexpected term %q in %#v", term, got)
		}
	}
}
