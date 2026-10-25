package httpapi

import "testing"

func TestLiveCostReferenceModelRatesContextBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		tokens                int64
		input, cached, output int64
	}{
		{"empty", 0, 200_000, 40_000, 800_000},
		{"first tier ceiling", 32_000, 200_000, 40_000, 800_000},
		{"second tier first token", 32_001, 600_000, 120_000, 2_400_000},
		{"second tier ceiling", 256_000, 600_000, 120_000, 2_400_000},
		{"third tier first token", 256_001, 1_200_000, 240_000, 4_800_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, cached, output := liveCostReferenceModelRates("qwen3.7-flash", tc.tokens)
			if input != tc.input || cached != tc.cached || output != tc.output {
				t.Fatalf("rates = (%d, %d, %d), want (%d, %d, %d)", input, cached, output, tc.input, tc.cached, tc.output)
			}
		})
	}
}

func TestLiveCostReferenceModelRatesRecognizesOnlyModelFamily(t *testing.T) {
	for _, model := range []string{"qwen3.7-flash", "qwen3.7-flash-2026-10-25", " QWEN3.7-FLASH-2026-10-25 "} {
		input, cached, output := liveCostReferenceModelRates(model, 32_001)
		if input != 600_000 || cached != 120_000 || output != 2_400_000 {
			t.Fatalf("model %q did not use the Qwen context tier: (%d, %d, %d)", model, input, cached, output)
		}
	}
	for _, model := range []string{"", "qwen3.7-flashx", "other-qwen3.7-flash", "qwen3.7-plus", "qwen3-flash"} {
		input, cached, output := liveCostReferenceModelRates(model, 256_001)
		if input != 200_000 || cached != 40_000 || output != 800_000 {
			t.Fatalf("unknown model %q did not retain the reference fallback: (%d, %d, %d)", model, input, cached, output)
		}
	}
}

func TestLiveCostReferenceModelRatesCachedTokensRemainInContext(t *testing.T) {
	// Of these 40,000 input tokens, 35,000 may be cached. The tier still uses
	// 40,000; subtracting cache to use 5,000 would incorrectly select tier one.
	const fullInputTokens = 40_000
	const cachedInputTokens = 35_000
	input, cached, output := liveCostReferenceModelRates("qwen3.7-flash", fullInputTokens)
	wrongInput, _, _ := liveCostReferenceModelRates("qwen3.7-flash", fullInputTokens-cachedInputTokens)
	if input != 600_000 || cached != 120_000 || output != 2_400_000 || input == wrongInput {
		t.Fatalf("full input count did not determine the context tier: (%d, %d, %d)", input, cached, output)
	}
}
