package voicemodels

import (
	"livecompanion/management/internal/ttsgateway"
	"testing"
)

func TestCustomerVoiceModelCannotOverrideSystemSelection(t *testing.T) {
	c := Default()
	if c.CustomModel != ttsgateway.CosyVoiceFlash || c.OfficialLibrary != ttsgateway.TencentPremiumModel {
		t.Fatal("incorrect defaults", c)
	}
	for _, request := range []string{"", c.CustomModel} {
		if got, err := c.ResolveCustom(request); err != nil || got != c.CustomModel {
			t.Fatal(got, err)
		}
	}
	for _, request := range []string{ttsgateway.CosyVoicePlus, ttsgateway.DefaultQwenTTSModel, "unknown"} {
		if _, err := c.ResolveCustom(request); err == nil {
			t.Fatal("customer selected another model", request)
		}
	}
	c.CustomModel = ttsgateway.CosyVoicePlus
	if _, err := c.ResolveCustom(ttsgateway.CosyVoiceFlash); err == nil {
		t.Fatal("stale page accepted after admin change")
	}
	if got, err := c.ResolveCustom(""); err != nil || got != c.CustomModel {
		t.Fatal("server selection not applied", got, err)
	}
	c.OfficialLibrary = "unknown"
	if c.Validate() == nil {
		t.Fatal("unknown official library accepted")
	}
	c = Default()
	c.CustomModel = ttsgateway.TencentPremiumModel
	if c.Validate() == nil {
		t.Fatal("official-only provider accepted for cloning")
	}
}
