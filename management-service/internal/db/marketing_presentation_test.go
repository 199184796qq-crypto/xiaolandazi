package db

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestNormalizeMarketingCampaignPresentation(t *testing.T) {
	defaults, err := normalizeMarketingCampaignPresentation(nil)
	if err != nil || defaults.TemplateKey != "blue_tech" || !defaults.ShowSaving || !defaults.ShowConditions {
		t.Fatal(defaults, err)
	}
	custom, err := normalizeMarketingCampaignPresentation(&model.MarketingCampaignPresentation{
		TemplateKey:     "festival_red",
		DisplaySize:     "featured",
		BackgroundStyle: "spotlight",
		PrimaryColor:    "#aa1122",
		AccentColor:     "#ffcc00",
		Eyebrow:         "周年庆",
		BadgeText:       "限时",
		CTAButtonText:   "马上领取",
		ShowCountdown:   true,
		ShowSaving:      true,
		ShowConditions:  true,
	})
	if err != nil || custom.PrimaryColor != "#AA1122" || custom.SchemaVersion != 1 {
		t.Fatal(custom, err)
	}
	invalid := custom
	invalidAssetID := int64(-1)
	invalid.CoverAssetID = &invalidAssetID
	if _, err := normalizeMarketingCampaignPresentation(&invalid); err == nil {
		t.Fatal("invalid campaign asset ID accepted")
	}
}
