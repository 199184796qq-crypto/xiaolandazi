package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/contentguard"
	"livecompanion/management/internal/interactionpolicy"
)

func TestGenerationPolicyRetainsEveryP0RuleWithoutLegacyLayers(t *testing.T) {
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	p, err := contentguard.Compile(string(md))
	if err != nil {
		t.Fatal(err)
	}
	got, err := LoadGeneration()
	if err != nil || got.L1 != nil || got.L2 != nil || got.L3 != nil || len(got.Conflicts) != 0 {
		t.Fatal(got, err)
	}
	var packet map[string][]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got.PromptText), &packet); err != nil {
		t.Fatal(err)
	}
	rules := packet["完整有效P0"]
	if len(rules) != len(p.Data.Rules) {
		t.Fatal("P0 rule lost", len(rules), len(p.Data.Rules))
	}
	for i, r := range rules {
		for _, key := range []string{"适用范围", "适用条件", "处理方式", "相关表述", "匹配表达", "排除表述", "所需依据", "规则说明"} {
			if _, ok := r[key]; !ok {
				t.Fatal("P0 condition lost", key)
			}
		}
		for _, key := range []string{"id", "source_version_id", "plan_id"} {
			if _, ok := r[key]; ok {
				t.Fatal("program relation leaked", key)
			}
		}
		for key, want := range map[string]any{"适用范围": p.Data.Rules[i].Scope, "适用条件": p.Data.Rules[i].Condition, "排除表述": p.Data.Rules[i].ExcludedTerms} {
			raw, _ := json.Marshal(want)
			if string(r[key]) != string(raw) {
				t.Fatal("P0 changed", key)
			}
		}
	}
}

func TestGenerationPolicyDoesNotFallBackWhenP0MissingOrInvalid(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_P0_POLICY_PATH", "")
	dir := t.TempDir()
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", dir)
	if _, err := LoadGeneration(); err == nil {
		t.Fatal("missing P0 accepted")
	}
	path := filepath.Join(dir, "compliance", "L1", "p0-hard-block.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid P0"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGeneration(); err == nil {
		t.Fatal("invalid P0 accepted")
	}
}

func TestGenerationPolicyUsesExplicitCurrentP0Path(t *testing.T) {
	md, err := interactionpolicy.Read("compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "current-p0.md")
	if err := os.WriteFile(path, md, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", dir)
	t.Setenv("ANCHOR_STYLE_P0_POLICY_PATH", path)
	if _, err := LoadGeneration(); err != nil {
		t.Fatal("explicit P0 ignored", err)
	}
	t.Setenv("ANCHOR_STYLE_P0_POLICY_PATH", filepath.Join(dir, "missing.md"))
	if _, err := LoadGeneration(); err == nil {
		t.Fatal("invalid override fell back")
	}
}
