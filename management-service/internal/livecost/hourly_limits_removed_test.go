package livecost

import (
	"encoding/json"
	"testing"
)

func TestPriceSettingsIgnoreAllLegacyHourlyTargets(t *testing.T) {
	for _, amounts := range [][3]int64{{0, 0, 0}, {800_000, 350_000, 1_300_000}, {3_000_000, 0, 1}, {-1, -2, -3}} {
		cfg := DefaultSettings()
		cfg.MainlineHourlyMicros, cfg.OptionalHourlyMicros, cfg.TotalHourlyMicros = amounts[0], amounts[1], amounts[2]
		decoded, err := DecodeSettings(settingsJSON(t, cfg))
		if err != nil || !decoded.Policy().AccountingOnly {
			t.Fatal(amounts, err)
		}
		if decoded.TTSPriceMicrosPer10K != cfg.TTSPriceMicrosPer10K {
			t.Fatal("price changed")
		}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(settingsJSON(t, DefaultSettings())), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"mainline_hourly_micros", "optional_hourly_micros", "total_hourly_micros"} {
		delete(raw, key)
	}
	encoded, _ := json.Marshal(raw)
	if cfg, err := DecodeSettings(string(encoded)); err != nil || !cfg.Policy().AccountingOnly {
		t.Fatal("price-only config rejected", cfg, err)
	}
}
