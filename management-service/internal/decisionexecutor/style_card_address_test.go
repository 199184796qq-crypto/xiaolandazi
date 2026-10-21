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
	want := []string{"朋友们", "哥姐们", "老乡们"}
	if len(got) != len(want) {
		t.Fatalf("terms = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("terms = %#v, want %#v", got, want)
		}
	}
}
