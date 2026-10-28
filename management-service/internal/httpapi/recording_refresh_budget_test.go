package httpapi

import (
	"math"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func TestRecordingDynamicRefreshBudgetPacesSharedHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary livecost.Summary
		runes   int
		quote   int64
		elapsed time.Duration
		want    recordingRefreshBudget
	}{
		{"ample_budget_can_exceed_old_25_percent", livecost.Summary{}, 2200, 400_000, 0, recordingRefreshBudget{41, 1_200_000, 166_666}},
		{"cheap_full_pass_has_no_fixed_50_percent_ceiling", livecost.Summary{}, 2200, 100_000, 0, recordingRefreshBudget{100, 1_200_000, 166_666}},
		{"eleven_minute_pass", livecost.Summary{}, 2420, 1_000_000, 0, recordingRefreshBudget{18, 1_200_000, 183_333}},
		{"necessary_work_does_not_consume_optional_target", livecost.Summary{TotalMicros: 600_000}, 2200, 1_000_000, 0, recordingRefreshBudget{16, 700_000, 166_666}},
		{"no_forced_old_percentage", livecost.Summary{TotalMicros: 1_190_000, OptionalMicros: 1_190_000, MainlineOptionalMicros: 1_190_000}, 2200, 1_000_000, 0, recordingRefreshBudget{1, 10_000, 10_000}},
		{"unknown_already_in_cohorts", livecost.Summary{TotalMicros: 600_000, OptionalMicros: 600_000, MainlineOptionalMicros: 600_000, ReservedMicros: 500_000, UnknownMicros: 200_000, AgedUnresolvedMicros: 9_000_000}, 2200, 1_000_000, 0, recordingRefreshBudget{16, 600_000, 166_666}},
		{"half_hour_remaining", livecost.Summary{}, 2200, 1_000_000, 30 * time.Minute, recordingRefreshBudget{33, 1_200_000, 333_333}},
		{"catchup_uses_only_mainline_optional_spend", livecost.Summary{TotalMicros: 900_000, OptionalMicros: 900_000, MainlineOptionalMicros: 100_000}, 2200, 1_000_000, 30 * time.Minute, recordingRefreshBudget{30, 300_000, 300_000}},
		{"target_is_not_a_hard_stop", livecost.Summary{TotalMicros: 1_000_000, OptionalMicros: 1_000_000, MainlineOptionalMicros: 1_000_000}, 2200, 1_000_000, 0, recordingRefreshBudget{16, 200_000, 166_666}},
		{"whole_sentence_pass_bounds_period", livecost.Summary{}, 2200, 4_000_000, 55 * time.Minute, recordingRefreshBudget{25, 1_200_000, 1_000_000}},
		{"new_rolling_hour_after_first", livecost.Summary{}, 2200, 1_000_000, time.Hour, recordingRefreshBudget{16, 1_200_000, 166_666}},
		{"later_rolling_hour", livecost.Summary{}, 2200, 1_000_000, 12 * time.Hour, recordingRefreshBudget{16, 1_200_000, 166_666}},
		{"negative_elapsed_is_start", livecost.Summary{}, 2200, 1_000_000, -time.Hour, recordingRefreshBudget{16, 1_200_000, 166_666}},
		{"long_pass_cannot_exceed_headroom", livecost.Summary{}, 132_000, 4_000_000, 0, recordingRefreshBudget{30, 1_200_000, 1_200_000}},
		{"at_optional_soft_cap", livecost.Summary{TotalMicros: 1_200_000, OptionalMicros: 1_200_000}, 2200, 1, 0, recordingRefreshBudget{}},
		{"necessary_1_2_yuan_is_not_the_optional_soft_line", livecost.Summary{TotalMicros: 1_200_000}, 2200, 1_000_000, 0, recordingRefreshBudget{10, 100_000, 100_000}},
		{"at_room_target", livecost.Summary{TotalMicros: 1_300_000}, 2200, 1, 0, recordingRefreshBudget{}},
		{"necessary_overuse_pauses_only_optional", livecost.Summary{TotalMicros: 1_400_000}, 2200, 1, 0, recordingRefreshBudget{}},
		{"invalid_total_fails_closed", livecost.Summary{TotalMicros: -1}, 2200, 1, 0, recordingRefreshBudget{}},
		{"invalid_optional_subset_fails_closed", livecost.Summary{TotalMicros: 600_000, OptionalMicros: 700_000}, 2200, 1, 0, recordingRefreshBudget{}},
		{"invalid_mainline_subset_fails_closed", livecost.Summary{TotalMicros: 600_000, MainlineOptionalMicros: 1}, 2200, 1, 0, recordingRefreshBudget{}},
		{"empty_source", livecost.Summary{}, 0, 1, 0, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
		{"missing_quote", livecost.Summary{}, 2200, 0, 0, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
		{"negative_quote", livecost.Summary{}, 2200, -1, 0, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recordingDynamicRefreshBudget(tc.summary, tc.runes, tc.quote, tc.elapsed); got != tc.want {
				t.Fatalf("got=%+v want=%+v", got, tc.want)
			}
		})
	}
}

func TestRecordingDynamicRefreshBudgetExtremeValuesRemainBounded(t *testing.T) {
	for _, quote := range []int64{1, math.MaxInt64} {
		got := recordingDynamicRefreshBudget(livecost.Summary{}, math.MaxInt, quote, time.Duration(math.MaxInt64))
		if got.HeadroomMicros != 1_200_000 || got.RoundLimitMicros != 1_200_000 || got.Percent < 0 || got.Percent > 100 {
			t.Fatalf("overflow or invalid allowance: %+v", got)
		}
	}
	got := recordingDynamicRefreshBudget(livecost.Summary{TotalMicros: 1_299_999}, 1, math.MaxInt64, 0)
	if got.HeadroomMicros != 1 || got.RoundLimitMicros != 1 || got.Percent != 0 {
		t.Fatalf("allowance exceeded the total-room remainder: %+v", got)
	}
}

func TestRecordingDynamicRefreshBudgetForDurationPacesMeasuredRounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		summary  livecost.Summary
		quote    int64
		elapsed  time.Duration
		duration time.Duration
		want     recordingRefreshBudget
	}{
		{"actual_ten_minutes", livecost.Summary{}, 1_000_000, 0, 10 * time.Minute, recordingRefreshBudget{16, 1_200_000, 166_666}},
		{"actual_thirty_minutes", livecost.Summary{}, 1_000_000, 0, 30 * time.Minute, recordingRefreshBudget{50, 1_200_000, 500_000}},
		{"ten_minutes_catches_up_over_remaining_half_hour", livecost.Summary{}, 1_000_000, 30 * time.Minute, 10 * time.Minute, recordingRefreshBudget{33, 1_200_000, 333_333}},
		{"thirty_minutes_bounds_remaining_quarter_hour", livecost.Summary{}, 1_000_000, 45 * time.Minute, 30 * time.Minute, recordingRefreshBudget{100, 1_200_000, 1_000_000}},
		{"mainline_target_not_other_optional_cost_drives_catchup", livecost.Summary{TotalMicros: 900_000, OptionalMicros: 900_000, MainlineOptionalMicros: 100_000}, 1_000_000, 30 * time.Minute, 10 * time.Minute, recordingRefreshBudget{30, 300_000, 300_000}},
		{"target_is_not_hard_stop", livecost.Summary{TotalMicros: 1_000_000, OptionalMicros: 1_000_000, MainlineOptionalMicros: 1_000_000}, 1_000_000, 0, 10 * time.Minute, recordingRefreshBudget{16, 200_000, 166_666}},
		{"over_hour_pass_caps_at_shared_headroom", livecost.Summary{}, 4_000_000, 0, 90 * time.Minute, recordingRefreshBudget{30, 1_200_000, 1_200_000}},
		{"first_rolling_hour_boundary", livecost.Summary{}, 1_000_000, time.Hour, 30 * time.Minute, recordingRefreshBudget{50, 1_200_000, 500_000}},
		{"many_rolling_hours", livecost.Summary{}, 1_000_000, 24 * time.Hour, 30 * time.Minute, recordingRefreshBudget{50, 1_200_000, 500_000}},
		{"negative_elapsed_is_start", livecost.Summary{}, 1_000_000, -time.Hour, 30 * time.Minute, recordingRefreshBudget{50, 1_200_000, 500_000}},
		{"zero_duration_has_no_allowance", livecost.Summary{}, 1_000_000, 0, 0, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
		{"negative_duration_has_no_allowance", livecost.Summary{}, 1_000_000, 0, -time.Minute, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
		{"missing_quote", livecost.Summary{}, 0, 0, 10 * time.Minute, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
		{"negative_quote", livecost.Summary{}, -1, 0, 10 * time.Minute, recordingRefreshBudget{HeadroomMicros: 1_200_000}},
		{"last_optional_micro_is_respected", livecost.Summary{TotalMicros: 1_199_999, OptionalMicros: 1_199_999}, 1, 0, 10 * time.Minute, recordingRefreshBudget{100, 1, 1}},
		{"optional_soft_limit", livecost.Summary{TotalMicros: 1_200_000, OptionalMicros: 1_200_000}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"necessary_work_at_soft_line_still_allows_room_remainder", livecost.Summary{TotalMicros: 1_200_000}, 1_000_000, 0, 10 * time.Minute, recordingRefreshBudget{10, 100_000, 100_000}},
		{"last_room_micro_is_respected", livecost.Summary{TotalMicros: 1_299_999}, 1, 0, 10 * time.Minute, recordingRefreshBudget{100, 1, 1}},
		{"total_room_target", livecost.Summary{TotalMicros: 1_300_000}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"necessary_overuse_pauses_only_optional", livecost.Summary{TotalMicros: 1_400_000}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"invalid_negative_total", livecost.Summary{TotalMicros: -1}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"invalid_negative_optional", livecost.Summary{OptionalMicros: -1}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"invalid_negative_mainline", livecost.Summary{MainlineOptionalMicros: -1}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"invalid_optional_subset", livecost.Summary{TotalMicros: 10, OptionalMicros: 11}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
		{"invalid_mainline_subset", livecost.Summary{TotalMicros: 10, OptionalMicros: 5, MainlineOptionalMicros: 6}, 1, 0, 10 * time.Minute, recordingRefreshBudget{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := recordingDynamicRefreshBudgetForDuration(tc.summary, tc.quote, tc.elapsed, tc.duration); got != tc.want {
				t.Fatalf("got=%+v want=%+v", got, tc.want)
			}
		})
	}
}

func TestRecordingDynamicRefreshBudgetForDurationExtremeValuesRemainBounded(t *testing.T) {
	for _, elapsed := range []time.Duration{time.Duration(math.MinInt64), 0, time.Hour - time.Nanosecond, time.Duration(math.MaxInt64)} {
		for _, quote := range []int64{1, math.MaxInt64} {
			got := recordingDynamicRefreshBudgetForDuration(livecost.Summary{}, quote, elapsed, time.Duration(math.MaxInt64))
			if got.HeadroomMicros != 1_200_000 || got.RoundLimitMicros != 1_200_000 || got.Percent < 0 || got.Percent > 100 {
				t.Fatalf("elapsed=%v quote=%d overflow or invalid allowance: %+v", elapsed, quote, got)
			}
		}
	}
	if got := recordingDynamicRefreshBudgetForDuration(livecost.Summary{}, 1, 0, time.Duration(math.MinInt64)); got != (recordingRefreshBudget{HeadroomMicros: 1_200_000}) {
		t.Fatalf("minimum duration granted an allowance: %+v", got)
	}
	if got := recordingDynamicRefreshBudgetForDuration(livecost.Summary{}, math.MaxInt64, 0, time.Nanosecond); got != (recordingRefreshBudget{HeadroomMicros: 1_200_000}) {
		t.Fatalf("sub-micro allocation rounded up or overflowed: %+v", got)
	}
}

func TestRecordingDynamicRefreshBudgetDurationPreservesLegacyRateWrapper(t *testing.T) {
	for _, elapsed := range []time.Duration{-time.Hour, 0, 30 * time.Minute, 55 * time.Minute, time.Hour, 12 * time.Hour} {
		for _, minutes := range []int{1, 10, 30, 90} {
			summary := livecost.Summary{TotalMicros: 300_000, OptionalMicros: 200_000, MainlineOptionalMicros: 100_000}
			legacy := recordingDynamicRefreshBudget(summary, minutes*220, 2_000_000, elapsed)
			measured := recordingDynamicRefreshBudgetForDuration(summary, 2_000_000, elapsed, time.Duration(minutes)*time.Minute)
			if legacy != measured {
				t.Fatalf("elapsed=%v minutes=%d legacy=%+v measured=%+v", elapsed, minutes, legacy, measured)
			}
		}
	}
}

func recordingRefreshQuoteFixture() (*Server, model.LiveAgentPlanVersion, *anchorcycle.State) {
	s := &Server{}
	version := model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{Model: ttsgateway.CosyVoiceFlash}}
	state := &anchorcycle.State{TextModel: &agentgateway.SpeechModel{ID: "profile", Model: "qwen3.7-flash", Enabled: true}}
	state.Config.TextProfileID = "profile"
	return s, version, state
}

func TestRecordingRefreshQuoteUsesWeightedTextAndThreeModelAttempts(t *testing.T) {
	s, version, state := recordingRefreshQuoteFixture()
	// 你好。 has 3 Unicode code points / 5 weighted characters. Ceil(25%
	// expansion) gives 4 / 7. TTS is 7*80=560; each model attempt quotes
	// 771 input and 32 output tokens, ceil(179.8)=180 micro-CNY, three times.
	got, err := s.recordingRefreshQuote("你好。", version, state)
	if err != nil || got != 1100 {
		t.Fatalf("quote=%d err=%v want=1100", got, err)
	}
	// Frozen snapshots intentionally omit credentials; estimating never needs
	// to fetch them or contact a model/TTS endpoint.
	if state.TextModel.APIKey != "" || version.VoiceIdentity.Rate != 0 {
		t.Fatal("quote mutated the caller's immutable snapshots")
	}
}

func TestRecordingRefreshQuoteMatchesExactContractsAndUsesConfiguredPrice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []liveCostPriceRule
		want  int64
	}{
		{"tts_matching_higher", []liveCostPriceRule{{Provider: ttsgateway.ProviderCosyVoice, Model: ttsgateway.CosyVoiceFlash, Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 100}}, 1240},
		{"tts_other_provider_ignored", []liveCostPriceRule{{Provider: "other", Model: ttsgateway.CosyVoiceFlash, Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 100}}, 1100},
		// Seven expanded weighted characters at one micro each, plus three
		// ordinary model attempts at ceil(179.8)=180 micro-CNY each.
		{"tts_lower_contract_reduces_allowance", []liveCostPriceRule{{Provider: ttsgateway.ProviderCosyVoice, Model: ttsgateway.CosyVoiceFlash, Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 1}}, 7 + 3*180},
		{"model_matching_higher", []liveCostPriceRule{{Provider: "anchor:profile", Model: "qwen3.7-flash", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 1_000_000, OutputMicrosPerMillion: 2_000_000}}, 3065},
		{"model_other_profile_ignored", []liveCostPriceRule{{Provider: "anchor:other", Model: "qwen3.7-flash", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 1_000_000, OutputMicrosPerMillion: 2_000_000}}, 1100},
		{"model_other_model_ignored", []liveCostPriceRule{{Provider: "anchor:profile", Model: "other", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 1_000_000, OutputMicrosPerMillion: 2_000_000}}, 1100},
		// The same seven TTS characters cost 560; each of the three model
		// attempts costs ceil((771 input + 32 output)/1e6)=1 micro-CNY.
		{"model_lower_contract_reduces_allowance", []liveCostPriceRule{{Provider: "anchor:profile", Model: "qwen3.7-flash", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 1, OutputMicrosPerMillion: 1}}, 560 + 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, version, state := recordingRefreshQuoteFixture()
			s.liveCostPrices = tc.rules
			if got, err := s.recordingRefreshQuote("你好。", version, state); err != nil || got != tc.want {
				t.Fatalf("quote=%d err=%v want=%d", got, err, tc.want)
			}
		})
	}
}

func TestRecordingRefreshQuoteIncludesConfiguredTokenTTSAllowance(t *testing.T) {
	s, version, state := recordingRefreshQuoteFixture()
	version.VoiceIdentity.Model = "qwen-audio-3.1-tts-flash"
	s.liveCostPrices = []liveCostPriceRule{{Provider: ttsgateway.ProviderQwen, Model: version.VoiceIdentity.Model, Kind: livecost.KindTTS, Unit: "tokens", InputMicrosPerMillion: 10_000_000, OutputMicrosPerMillion: 20_000_000}}
	// Token-billed speech is quoted on the supplier's real unit: input tokens ≈
	// sent characters, output tokens ≈ 1.35 × billable characters. For "你好。"
	// that is 4 input and 10 audio tokens: 40+200=240, plus the model's 540.
	if got, err := s.recordingRefreshQuote("你好。", version, state); err != nil || got != 780 {
		t.Fatalf("quote=%d err=%v want=780", got, err)
	}
}

func TestRecordingRefreshQuoteUsesCurrentQwenInputTier(t *testing.T) {
	for _, tc := range []struct {
		runes int
		want  int64
	}{{31_232, 6_267_520}, {31_233, 6_310_003}} {
		s, version, state := recordingRefreshQuoteFixture()
		if got, err := s.recordingRefreshQuote(strings.Repeat("字", tc.runes), version, state); err != nil || got != tc.want {
			t.Fatalf("source=%d quote=%d err=%v want=%d", tc.runes, got, err, tc.want)
		}
	}
}

func TestRecordingRefreshQuoteRejectsMissingIdentityAndOverflows(t *testing.T) {
	for _, change := range []func(**Server, *model.LiveAgentPlanVersion, **anchorcycle.State){
		func(s **Server, _ *model.LiveAgentPlanVersion, _ **anchorcycle.State) { *s = nil },
		func(_ **Server, _ *model.LiveAgentPlanVersion, p **anchorcycle.State) { *p = nil },
		func(_ **Server, _ *model.LiveAgentPlanVersion, p **anchorcycle.State) { (*p).TextModel = nil },
		func(_ **Server, _ *model.LiveAgentPlanVersion, p **anchorcycle.State) { (*p).TextModel.Enabled = false },
		func(_ **Server, _ *model.LiveAgentPlanVersion, p **anchorcycle.State) { (*p).TextModel.Model = "" },
		func(_ **Server, _ *model.LiveAgentPlanVersion, p **anchorcycle.State) { (*p).TextModel.ID = "" },
		func(_ **Server, _ *model.LiveAgentPlanVersion, p **anchorcycle.State) {
			(*p).Config.TextProfileID = "other"
		},
		func(_ **Server, v *model.LiveAgentPlanVersion, _ **anchorcycle.State) { v.VoiceIdentity.Model = "" },
		func(_ **Server, v *model.LiveAgentPlanVersion, _ **anchorcycle.State) {
			v.VoiceIdentity.Rate = math.NaN()
		},
		func(_ **Server, v *model.LiveAgentPlanVersion, _ **anchorcycle.State) { v.VoiceIdentity.Rate = -1 },
		func(s **Server, _ *model.LiveAgentPlanVersion, _ **anchorcycle.State) {
			(*s).liveCostPrices = []liveCostPriceRule{{Provider: ttsgateway.ProviderCosyVoice, Model: ttsgateway.CosyVoiceFlash, Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: math.MaxInt64}}
		},
		func(s **Server, _ *model.LiveAgentPlanVersion, _ **anchorcycle.State) {
			(*s).liveCostPrices = []liveCostPriceRule{{Provider: "anchor:profile", Model: "qwen3.7-flash", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: math.MaxInt64, OutputMicrosPerMillion: math.MaxInt64}}
		},
	} {
		s, version, state := recordingRefreshQuoteFixture()
		change(&s, &version, &state)
		if _, err := s.recordingRefreshQuote("你好。", version, state); err == nil {
			t.Fatal("invalid or overflowing optional estimate was admitted")
		}
	}
	s, version, state := recordingRefreshQuoteFixture()
	if _, err := s.recordingRefreshQuote("  ", version, state); err == nil {
		t.Fatal("empty source admitted")
	}
	for _, count := range []int64{-1, math.MaxInt64} {
		if _, err := recordingRefreshExpandedCount(count); err == nil {
			t.Fatalf("invalid expanded count admitted: %d", count)
		}
	}
}

func TestCheaperSpeechQuoteBuysMoreUpdates(t *testing.T) {
	policy := livecost.DefaultPolicy()
	summary := livecost.Summary{}
	expensive := recordingDynamicRefreshBudgetForDurationWithPolicy(summary, 5_000_000, 0, 10*time.Minute, policy)
	cheap := recordingDynamicRefreshBudgetForDurationWithPolicy(summary, 2_500_000, 0, 10*time.Minute, policy)
	if cheap.Percent <= expensive.Percent {
		t.Fatalf("cheaper quote should allow more updates: expensive=%d cheap=%d", expensive.Percent, cheap.Percent)
	}
}

func TestRecordingRefreshQuoteUsesTokenDefaultsWithoutContract(t *testing.T) {
	s, version, state := recordingRefreshQuoteFixture()
	version.VoiceIdentity.Model = "qwen-audio-3.1-tts-flash"
	s.liveCostPrices = nil
	// Supplier token price (1.5 in / 12 out per million) is the billing basis even
	// before an operator adds a contract row: 4 input + 10 audio tokens + 540 model.
	if got, err := s.recordingRefreshQuote("你好。", version, state); err != nil || got != 666 {
		t.Fatalf("quote=%d err=%v want=666", got, err)
	}
}
