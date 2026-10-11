package policy

import (
	"encoding/json"
	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"os"
	"strings"
)

// LoadGeneration reads only the public P0 boundary. Runtime business rules,
// facts and style are supplied by the selected saved agent plan. This loader has
// no database/room dependency: legacy L1/L2/L3 catalogs cannot enter generation.
func LoadGeneration() (model.LiveEffectivePolicy, error) {
	var md []byte
	var err error
	if path := strings.TrimSpace(os.Getenv("ANCHOR_STYLE_P0_POLICY_PATH")); path != "" {
		md, err = os.ReadFile(path)
	} else {
		md, err = interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	}
	if err != nil {
		return model.LiveEffectivePolicy{}, err
	}
	p, err := contentguard.Compile(string(md))
	if err != nil {
		return model.LiveEffectivePolicy{}, err
	}
	rules := make([]map[string]any, 0, len(p.Data.Rules))
	for _, r := range p.Data.Rules {
		rules = append(rules, map[string]any{
			"类别": r.Category, "适用范围": r.Scope, "适用条件": r.Condition,
			"处理方式": r.ReviewMode, "相关表述": r.Terms, "匹配表达": r.Patterns,
			"排除表述": r.ExcludedTerms, "所需依据": r.RequiredBasis, "规则说明": r.Note,
		})
	}
	raw, err := json.Marshal(map[string]any{"完整有效P0": rules})
	if err != nil {
		return model.LiveEffectivePolicy{}, err
	}
	return model.LiveEffectivePolicy{
		IndustryCode: "general",
		Rules:        []model.LiveEffectivePolicyRule{{Title: "有效P0", Text: string(raw), ExecutionMode: model.LivePolicyModeIntent, SourceLayer: "P0"}},
		Conflicts:    []model.LivePolicyConflict{}, PromptText: string(raw),
	}, nil
}
