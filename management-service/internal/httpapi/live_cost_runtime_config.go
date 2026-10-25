package httpapi

import (
	"context"
	"errors"
	"log"
	"time"

	"livecompanion/management/internal/livecost"
)

type liveCostSettingsContextKey struct{}
type liveCostSettingsContext struct {
	Settings livecost.Settings
	Valid    bool
}

func (s *Server) withLiveCostSettingsSnapshot(ctx context.Context, cfg livecost.Settings, valid bool) context.Context {
	return context.WithValue(ctx, liveCostSettingsContextKey{}, liveCostSettingsContext{cfg.Clone(), valid})
}

// Read for every new paid request or planning pass, so another process's save
// is visible without a restart. No price revision participates in media/key
// identity. A failure retains the last verified positive price for necessary
// supply, while callers must defer optional work when valid is false.
func (s *Server) liveCostSettingsSnapshot(ctx context.Context) (livecost.Settings, bool) {
	if value, ok := ctx.Value(liveCostSettingsContextKey{}).(liveCostSettingsContext); ok {
		return value.Settings.Clone(), value.Valid
	}
	if s == nil {
		return livecost.DefaultSettings(), true
	}
	loader := s.liveCostConfigLoader
	if loader == nil && s.store != nil {
		loader = s.store.LiveCostSettings
	}
	if loader == nil {
		return livecost.DefaultSettings(), true // isolated tests, never a production controller bypass
	}
	read, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cfg, err := loader(read)
	if err == nil {
		err = cfg.Validate()
	}
	s.liveCostMu.Lock()
	defer s.liveCostMu.Unlock()
	if err == nil {
		if s.liveCostConfig == nil || cfg.Revision >= s.liveCostConfig.Revision {
			copy := cfg.Clone()
			s.liveCostConfig = &copy
		}
		s.liveCostConfigWarned = false
		return s.liveCostConfig.Clone(), true
	}
	if !s.liveCostConfigWarned {
		log.Printf("[live-cost] 费用配置暂不可读取，可选更新暂缓；必要供料沿用最后有效价格并继续留痕")
		s.liveCostConfigWarned = true
	}
	if s.liveCostConfig != nil {
		return s.liveCostConfig.Clone(), false
	}
	return livecost.DefaultSettings(), false
}

func (s *Server) liveCostPricingSnapshot(ctx context.Context, provider, model string) (livecost.PricingSnapshot, bool) {
	cfg, valid := s.liveCostSettingsSnapshot(ctx)
	return s.liveCostPricingForSettings(cfg, provider, model), valid
}

// Before the first administrator save, preserve exact legacy supplier
// contracts. Once saved, the explicit backend configuration is authoritative,
// including lower prices. The legacy contract is frozen, never read at finish.
func (s *Server) liveCostPricingForSettings(cfg livecost.Settings, provider, model string) livecost.PricingSnapshot {
	price := cfg.Snapshot(provider, model)
	if cfg.Revision == 0 && s != nil {
		for _, kind := range []string{livecost.KindTTS, livecost.KindModel} {
			for _, unit := range []string{"characters", "tokens"} {
				if rule, ok := s.liveCostPrice(provider, model, kind, unit); ok {
					copy := rule
					price.Contract = &copy
					return price
				}
			}
		}
	}
	return price
}

func (s *Server) quoteLiveModel(price livecost.PricingSnapshot, input, cached, output int64) (int64, error) {
	inRate, cachedRate, outRate := livecost.TokenRates(price.ModelTiers, input)
	if contract := price.Contract; contract != nil && contract.Kind == livecost.KindModel && contract.Unit == "tokens" {
		inRate, cachedRate, outRate = contract.InputMicrosPerMillion, liveCostCachedRate(*contract), contract.OutputMicrosPerMillion
	}
	if inRate <= 0 || outRate <= 0 || cachedRate < 0 {
		return 0, errors.New("模型费用缺少有效价格，不能按零费用放行")
	}
	return livecost.EstimateTokens(input, cached, output, inRate, cachedRate, outRate)
}

func liveCostInvalidOptionalConfig(ctx context.Context, priority string, valid bool) error {
	if valid || priority != livecost.PriorityOptional {
		return nil
	}
	if events := liveCostScope(ctx).Events; events != nil {
		events.AccountingUnavailable.Store(true)
		events.OptionalDeclined.Store(true)
	}
	return errLiveCostOptionalDeclined
}
