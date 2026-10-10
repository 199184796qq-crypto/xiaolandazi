package model

import "testing"

func TestRecommendedStyleHashTracksOnlyPortableAssets(t *testing.T) {
	style := LiveAnchorStyle{Name: "主播", Description: "自然", AvatarType: "female", AvatarURL: "data:image/jpeg;base64,abc", Profile: LiveAgentPlanAnchorStyleProfile{Summary: "原风格"}, PluginSettings: []LiveAnchorStylePluginSetting{{PluginID: "expression", Enabled: true}}}
	hash := AnchorRecommendationSHA(style)
	style.PluginSettings[0].Parameters = map[string]any{}
	if AnchorRecommendationSHA(style) != hash {
		t.Fatal("empty plugin parameters must survive database normalization")
	}
	style.ID = 99
	style.TenantID = 42
	style.SampleCount = 10
	style.BoundPlanCount = 3
	if AnchorRecommendationSHA(style) != hash {
		t.Fatal("private identifiers or runtime counters entered portable hash")
	}
	style.Profile.Summary = "用户后续改动"
	if AnchorRecommendationSHA(style) == hash {
		t.Fatal("profile edit not detected")
	}
	style.Profile.Summary = "原风格"
	style.PluginSettings[0].Enabled = false
	if AnchorRecommendationSHA(style) == hash {
		t.Fatal("plugin edit not detected")
	}
}
