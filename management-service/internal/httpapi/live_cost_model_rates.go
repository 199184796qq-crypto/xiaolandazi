package httpapi

import "strings"

// liveCostReferenceModelRates returns micro-CNY per million tokens. The Qwen
// tier uses the full input token count, including tokens served from cache:
// cached input changes its unit price, not the context-length pricing tier.
// Reference: https://docs.bailian.console.aliyun.com/zh/model-studio/qwen3-7-flash
// Unknown models retain the user's reference base rates for estimation only;
// these fallback numbers are not a verified supplier price or invoice amount.
func liveCostReferenceModelRates(model string, inputTokens int64) (input, cached, output int64) {
	model = strings.ToLower(strings.TrimSpace(model))
	if model != "qwen3.7-flash" && !strings.HasPrefix(model, "qwen3.7-flash-") {
		return 200_000, 40_000, 800_000
	}
	switch {
	case inputTokens <= 32_000:
		return 200_000, 40_000, 800_000
	case inputTokens <= 256_000:
		return 600_000, 120_000, 2_400_000
	default:
		return 1_200_000, 240_000, 4_800_000
	}
}
