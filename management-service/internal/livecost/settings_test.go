package livecost

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func settingsJSON(t *testing.T, settings Settings) string {
	t.Helper()
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestLiveCostSettingsDefaultRoundTripAndExplicitZero(t *testing.T) {
	want := DefaultSettings()
	got, err := DecodeSettings(settingsJSON(t, want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	got.TTSPriceMicrosPer10K = 400_000
	got.DefaultModelRates[0].CachedInputMicrosPerMillion = 0
	got.MainlineHourlyMicros, got.OptionalHourlyMicros = 0, 0
	decoded, err := DecodeSettings(settingsJSON(t, got))
	if err != nil || decoded.TTSPriceMicrosPer10K != 400_000 || decoded.DefaultModelRates[0].CachedInputMicrosPerMillion != 0 {
		t.Fatal(decoded, err)
	}
	policy := decoded.Policy()
	if !policy.Configured || policy.OptionalLimitMicros != 0 || policy.MainlineTargetMicros != 0 || policy.TotalTargetMicros != want.TotalHourlyMicros {
		t.Fatal(policy)
	}
}

func TestLiveCostSettingsRejectsIncompleteCorruptOrUnknownJSON(t *testing.T) {
	valid := settingsJSON(t, DefaultSettings())
	for name, raw := range map[string]string{
		"empty": "", "partial": `{}`, "null": `null`, "array": `[]`, "malformed": `{`,
		"extra object": valid + ` {}`, "unknown": strings.Replace(valid, `"revision":0`, `"revision":0,"mystery":1`, 1),
		"duplicate":      strings.Replace(valid, `"revision":0`, `"revision":0,"revision":1`, 1),
		"case duplicate": strings.Replace(valid, `"revision":0`, `"revision":0,"Revision":1`, 1),
		"fraction":       strings.Replace(valid, `"revision":0`, `"revision":0.5`, 1),
		"missing cache":  strings.Replace(valid, `"cached_input_micros_per_million":40000,`, "", 1),
		"null cache":     strings.Replace(valid, `"cached_input_micros_per_million":40000`, `"cached_input_micros_per_million":null`, 1),
		"oversize":       strings.Repeat(" ", MaxSettingsJSONBytes+1),
		"too deep":       strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSettings(raw); err == nil {
				t.Fatal("invalid settings accepted")
			}
		})
	}
}

func TestLiveCostSettingsValidationLimits(t *testing.T) {
	changes := map[string]func(*Settings){
		"version":           func(s *Settings) { s.Version = 2 },
		"negative revision": func(s *Settings) { s.Revision = -1 },
		"zero tts":          func(s *Settings) { s.TTSPriceMicrosPer10K = 0 },
		"huge tts":          func(s *Settings) { s.TTSPriceMicrosPer10K = MaxConfiguredMicros + 1 },
		"negative mainline": func(s *Settings) { s.MainlineHourlyMicros = -1 },
		"budget order":      func(s *Settings) { s.OptionalHourlyMicros = s.MainlineHourlyMicros - 1 },
		"zero total":        func(s *Settings) { s.MainlineHourlyMicros, s.OptionalHourlyMicros, s.TotalHourlyMicros = 0, 0, 0 },
		"huge total":        func(s *Settings) { s.TotalHourlyMicros = MaxConfiguredMicros + 1 },
		"missing fallback":  func(s *Settings) { s.DefaultModelRates = nil },
		"zero input":        func(s *Settings) { s.DefaultModelRates[0].InputMicrosPerMillion = 0 },
		"zero output":       func(s *Settings) { s.DefaultModelRates[0].OutputMicrosPerMillion = 0 },
		"negative cache":    func(s *Settings) { s.DefaultModelRates[0].CachedInputMicrosPerMillion = -1 },
		"closed final tier": func(s *Settings) { s.DefaultModelRates[0].MaxInputTokens = 100 },
		"out of order":      func(s *Settings) { s.ModelPrices[0].Tiers[1].MaxInputTokens = 100 },
		"middle unbounded":  func(s *Settings) { s.ModelPrices[0].Tiers[0].MaxInputTokens = 0 },
		"duplicate rule":    func(s *Settings) { s.ModelPrices = append(s.ModelPrices, s.ModelPrices[0]) },
		"unknown wildcard":  func(s *Settings) { s.ModelPrices[0].Provider = "provider*" },
		"blank provider":    func(s *Settings) { s.ModelPrices[0].Provider = "" },
		"spaced model":      func(s *Settings) { s.ModelPrices[0].Model = " qwen3.7-flash" },
		"too many rules":    func(s *Settings) { s.ModelPrices = make([]ModelPrice, 33) },
		"too many tiers":    func(s *Settings) { s.DefaultModelRates = make([]TokenTier, 17) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			settings := DefaultSettings()
			change(&settings)
			if err := settings.Validate(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
}

func TestLiveCostSettingsSnapshotUsesProviderThenLongestModelAndIsIndependent(t *testing.T) {
	settings := DefaultSettings()
	low := []TokenTier{{InputMicrosPerMillion: 12, CachedInputMicrosPerMillion: 0, OutputMicrosPerMillion: 18}}
	settings.ModelPrices = append(settings.ModelPrices,
		ModelPrice{Provider: "*", Model: "qwen3.7-flash-special", Tiers: low},
		ModelPrice{Provider: "anchor:one", Model: "qwen3.7-flash", Tiers: []TokenTier{{InputMicrosPerMillion: 21, OutputMicrosPerMillion: 31}}},
	)
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		provider, model string
		input, want     int64
	}{
		{"other", "qwen3.7-flash", 32_000, 200_000},
		{"other", "qwen3.7-flash-20261025", 32_001, 600_000},
		{" OTHER ", " QWEN3.7-FLASH-20261025 ", 32_001, 600_000},
		{"other", "qwen3.7-flash", 256_001, 1_200_000},
		{"other", "qwen3.7-flash-special-1", 1, 12},
		{"anchor:one", "qwen3.7-flash-special-1", 1, 21},
		{"ANCHOR:ONE", "QWEN3.7-FLASH-SPECIAL-1", 1, 21},
		{"other", "unknown", 1, 200_000},
	} {
		snapshot := settings.Snapshot(tc.provider, tc.model)
		input, _, _ := TokenRates(snapshot.ModelTiers, tc.input)
		if input != tc.want {
			t.Fatal(tc, input)
		}
	}
	snapshot := settings.Snapshot("other", "qwen3.7-flash-special")
	settings.ModelPrices[1].Tiers[0].InputMicrosPerMillion = 99
	if snapshot.ModelTiers[0].InputMicrosPerMillion != 12 {
		t.Fatal("snapshot aliased mutable settings")
	}
	if snapshot.Config.ModelPrices[1].Tiers[0].InputMicrosPerMillion != 12 {
		t.Fatal("complete snapshot config aliased mutable settings")
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	repriced := snapshot.Config.Snapshot("other", "qwen3.7-flash-actual")
	if input, _, _ := TokenRates(repriced.ModelTiers, 32_001); input != 600_000 {
		t.Fatal("actual response model lost its frozen ladder", repriced)
	}
	other := settings.Snapshot("other", "unknown")
	other.ModelTiers[0].InputMicrosPerMillion = 1
	if settings.DefaultModelRates[0].InputMicrosPerMillion != 200_000 {
		t.Fatal("fallback snapshot aliased settings")
	}
}

func TestLiveCostPricingSnapshotRejectsInvalidOptionalSnapshot(t *testing.T) {
	valid := DefaultSettings().Snapshot("provider", "qwen3.7-flash")
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PricingSnapshot){
		func(s *PricingSnapshot) { s.TTSPriceMicrosPer10K = 0 },
		func(s *PricingSnapshot) { s.ModelTiers = nil },
		func(s *PricingSnapshot) { s.Revision++ },
		func(s *PricingSnapshot) {
			s.Contract = &SupplierPrice{Provider: "*", Model: "model", Kind: KindTTS, Unit: "characters", CharacterMicros: 80}
		},
		func(s *PricingSnapshot) {
			s.Contract = &SupplierPrice{Provider: "provider", Model: "model", Kind: KindTTS, Unit: "bytes", CharacterMicros: 80}
		},
	} {
		copy := DefaultSettings().Snapshot("provider", "qwen3.7-flash")
		change(&copy)
		if err := copy.Validate(); err == nil {
			t.Fatal("bad pricing snapshot accepted")
		}
	}
}

func TestEstimateTTSPer10KUsesConfiguredLowerPriceAndSafeCeiling(t *testing.T) {
	for _, tc := range []struct{ count, price, want int64 }{
		{10_000, 800_000, 800_000}, {10_000, 400_000, 400_000}, {1, 1, 1}, {3, 10_001, 4}, {0, 800_000, 0},
		{math.MaxInt64, 1, math.MaxInt64/10_000 + 1},
	} {
		got, err := EstimateTTSPer10K(tc.count, tc.price)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	for _, tc := range [][2]int64{{1, 0}, {-1, 1}, {1, -1}, {1, MaxConfiguredMicros + 1}, {math.MaxInt64, MaxConfiguredMicros}} {
		if _, err := EstimateTTSPer10K(tc[0], tc[1]); err == nil {
			t.Fatal("invalid or overflow quote accepted", tc)
		}
	}
}
