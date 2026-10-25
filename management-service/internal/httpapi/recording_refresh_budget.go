package httpapi

import (
	"context"
	"errors"
	"math"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

// This is a scheduling allowance, never a supplier charge or an alternative to
// the transactional, shared-room reservation immediately before each request.
type recordingRefreshBudget struct {
	Percent          int
	HeadroomMicros   int64
	RoundLimitMicros int64
}

func recordingDynamicRefreshBudget(summary livecost.Summary, totalRunes int, fullQuoteMicros int64, elapsed time.Duration) recordingRefreshBudget {
	return recordingDynamicRefreshBudgetWithPolicy(summary, totalRunes, fullQuoteMicros, elapsed, livecost.DefaultPolicy())
}

func recordingDynamicRefreshBudgetWithPolicy(summary livecost.Summary, totalRunes int, fullQuoteMicros int64, elapsed time.Duration, policy livecost.Policy) recordingRefreshBudget {
	// Keep the legacy 220-runes/minute estimate exact, including source sizes
	// too large to represent as a time.Duration.
	round := new(big.Int).Mul(big.NewInt(int64(totalRunes)), big.NewInt(int64(time.Minute)))
	return recordingDynamicRefreshBudgetForPeriod(summary, fullQuoteMicros, elapsed, round, 220, policy)
}

// recordingDynamicRefreshBudgetForDuration uses the measured duration of an
// entire source round. Duration only paces optional spending; it is never a
// supplier charge or a replacement for the per-request shared reservation.
func recordingDynamicRefreshBudgetForDuration(summary livecost.Summary, fullQuoteMicros int64, elapsed, roundDuration time.Duration) recordingRefreshBudget {
	return recordingDynamicRefreshBudgetForDurationWithPolicy(summary, fullQuoteMicros, elapsed, roundDuration, livecost.DefaultPolicy())
}

func recordingDynamicRefreshBudgetForDurationWithPolicy(summary livecost.Summary, fullQuoteMicros int64, elapsed, roundDuration time.Duration, policy livecost.Policy) recordingRefreshBudget {
	return recordingDynamicRefreshBudgetForPeriod(summary, fullQuoteMicros, elapsed, big.NewInt(int64(roundDuration)), 1, policy)
}

// round/scale represents nanoseconds as a rational, keeping both public
// scheduling inputs on the same allocation and overflow-safe arithmetic path.
func recordingDynamicRefreshBudgetForPeriod(summary livecost.Summary, fullQuoteMicros int64, elapsed time.Duration, round *big.Int, scale int64, policy livecost.Policy) recordingRefreshBudget {
	var result recordingRefreshBudget
	policy, err := livecost.NormalizePolicy(policy)
	if err != nil || policy.MainlineTargetMicros == 0 || summary.TotalMicros < 0 || summary.OptionalMicros < 0 || summary.MainlineOptionalMicros < 0 ||
		summary.OptionalMicros > summary.TotalMicros || summary.MainlineOptionalMicros > summary.OptionalMicros ||
		summary.OptionalMicros >= policy.OptionalLimitMicros || summary.TotalMicros >= policy.TotalTargetMicros {
		return result
	}
	// Each cohort already includes its unresolved reservations. These are
	// overlapping reporting subsets, never additional expenses to add together.
	result.HeadroomMicros = policy.OptionalLimitMicros - summary.OptionalMicros
	if roomHeadroom := policy.TotalTargetMicros - summary.TotalMicros; roomHeadroom < result.HeadroomMicros {
		result.HeadroomMicros = roomHeadroom
	}
	if round == nil || round.Sign() <= 0 || scale <= 0 || fullQuoteMicros <= 0 {
		return result
	}
	if elapsed < 0 {
		elapsed = 0
	}
	remaining := policy.Window
	if elapsed < policy.Window {
		remaining -= elapsed
	}
	// Never multiply a time.Duration or monetary value in fixed-width integers.
	period := new(big.Int).Mul(big.NewInt(int64(remaining)), big.NewInt(scale))
	if round.Cmp(period) > 0 {
		period.Set(round)
	}
	// Mainline, all-optional and whole-room targets are separate configured
	// cohorts; each is already inclusive of its unsettled reservations.
	// Spread any catch-up balance across the remaining allocation period; using
	// max(base, entire target balance) would spend a whole hour in one short pass.
	targetRemaining := policy.MainlineTargetMicros - summary.MainlineOptionalMicros
	if targetRemaining < 0 {
		targetRemaining = 0
	}
	allowance := new(big.Int).Mul(big.NewInt(targetRemaining), round)
	allowance.Quo(allowance, period)
	base := new(big.Int).Mul(big.NewInt(policy.MainlineTargetMicros), round)
	base.Quo(base, new(big.Int).Mul(big.NewInt(int64(policy.Window)), big.NewInt(scale)))
	if base.Cmp(allowance) > 0 {
		allowance.Set(base)
	}
	if allowance.Cmp(big.NewInt(result.HeadroomMicros)) > 0 {
		allowance.SetInt64(result.HeadroomMicros)
	}
	result.RoundLimitMicros = allowance.Int64() // Bounded by HeadroomMicros.
	percent := new(big.Int).Mul(allowance, big.NewInt(100))
	percent.Quo(percent, big.NewInt(fullQuoteMicros))
	if percent.Cmp(big.NewInt(100)) >= 0 {
		result.Percent = 100
	} else {
		result.Percent = int(percent.Int64())
	}
	return result
}

// recordingRefreshQuote prices one source's optional model+TTS work without
// sending it. The current model snapshot determines the provider/model price;
// credentials, prompts, source text and signed URLs never enter diagnostics.
func (s *Server) recordingRefreshQuote(source string, version model.LiveAgentPlanVersion, polish *anchorcycle.State) (int64, error) {
	settings, valid := s.liveCostSettingsSnapshot(context.Background())
	if !valid {
		return 0, errors.New("更新费用设置暂不可用")
	}
	return s.recordingRefreshQuoteWithSettings(source, version, polish, settings)
}

// One immutable settings value prices the complete pass without a database
// query per source. Actual request admission still reads the newest budget.
func (s *Server) recordingRefreshQuoteWithSettings(source string, version model.LiveAgentPlanVersion, polish *anchorcycle.State, settings livecost.Settings) (int64, error) {
	if s == nil || polish == nil || polish.TextModel == nil {
		return 0, errors.New("更新费用预估缺少文字模型")
	}
	profile := polish.TextModel
	profileID := strings.TrimSpace(profile.ID)
	modelName := strings.TrimSpace(profile.Model)
	if profileID == "" || modelName == "" || !profile.Enabled || strings.TrimSpace(polish.Config.TextProfileID) != profileID {
		return 0, errors.New("更新费用预估的文字模型配置不可用")
	}
	if err := settings.Validate(); err != nil {
		return 0, errors.New("更新费用设置无效")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return 0, errors.New("更新费用预估缺少正文")
	}
	voice := version.VoiceIdentity
	voiceModel := strings.TrimSpace(voice.Model)
	if voiceModel == "" || math.IsNaN(voice.Rate) || math.IsInf(voice.Rate, 0) || voice.Rate < 0 {
		return 0, errors.New("更新费用预估的声音配置不可用")
	}
	runes := int64(utf8.RuneCountInString(source))
	sent, err := recordingRefreshExpandedCount(runes)
	if err != nil {
		return 0, err
	}
	billable, err := recordingRefreshExpandedCount(ttsgateway.BillableTextCharacters(source))
	if err != nil {
		return 0, err
	}
	rate := voice.Rate
	if rate == 0 {
		rate = 1
	}
	pricing := s.liveCostPricingForSettings(settings, ttsgateway.ProviderForModel(voiceModel), voiceModel)
	ttsQuote, _, err := s.estimateLiveTTSWithPricing(ttsgateway.SynthesisAttempt{
		Provider: ttsgateway.ProviderForModel(voiceModel), Model: voiceModel,
		VoiceID: voice.VoiceID, Rate: rate, SentTextCharacters: sent,
		BillableCharacters: billable, CharacterRule: ttsgateway.BillableCharacterRule,
	}, liveCostCallScope{Priority: livecost.PriorityOptional, Reason: "话术优先更新联合预估"}, pricing)
	if err != nil {
		return 0, err
	}
	if runes > math.MaxInt64-768 {
		return 0, errors.New("更新文字用量预估越界")
	}
	input := runes + 768
	output := int64(800)
	if runes < 400 {
		output = runes * 2
		if output < 32 {
			output = 32
		}
	}
	modelQuote, err := s.quoteLiveModel(s.liveCostPricingForSettings(settings, "anchor:"+profileID, modelName), input, 0, output)
	if err != nil {
		return 0, err
	}
	// Up to three definite returned P0 attempts are permitted by the existing
	// polisher. This allowance does not authorize retries after an unknown result.
	if modelQuote > math.MaxInt64/3 || ttsQuote > math.MaxInt64-modelQuote*3 {
		return 0, errors.New("更新联合费用预估越界")
	}
	return modelQuote*3 + ttsQuote, nil
}

func recordingRefreshExpandedCount(count int64) (int64, error) {
	if count < 0 {
		return 0, errors.New("更新文字用量预估无效")
	}
	extra := count / 4
	if count%4 != 0 {
		extra++
	}
	if count > math.MaxInt64-extra {
		return 0, errors.New("更新文字用量预估越界")
	}
	return count + extra, nil
}
