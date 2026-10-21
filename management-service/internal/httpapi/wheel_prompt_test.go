package httpapi

import (
	"strings"
	"testing"
)

var wheelP0Fixture = "```json\n" + `{
  "schema_version": 1,
  "layer": "L1",
  "severity": "P0",
  "policy_version": "test-v1",
  "status": "active",
  "execution": {"default_scope": "all", "max_rewrites": 3, "max_consecutive_skips": 3,
    "rewrite_scope": "matched_sentence_or_paragraph", "recheck_scope": "whole_candidate",
    "on_exhausted": "skip_current_selling_point_angle", "count_rejected_as_adopted": false,
    "allow_user_override": false},
  "normalization": {"unicode_form": "NFKC", "case_fold": false, "remove_zero_width": true,
    "ignore_spaces_for_match": true, "keep_original_offsets": true, "aliases": {}},
  "rules": [
    {"id": "r1", "severity": "P0", "category": "绝对词与极限词", "scope": "all", "condition": "always",
     "terms": ["最好", "最佳", "极致", "第一"], "patterns": [], "note": "程序扫描用的元信息，不该出现在生成提示词里"},
    {"id": "r2", "severity": "P0", "category": "医疗与保健功效", "scope": "all", "condition": "always",
     "terms": ["治疗", "根治"], "patterns": [], "note": "元信息"}
  ]
}` + "\n```"

// 轮盘提示词按四层组织：必说/可说/不提/改写；「不提」只给按类别的裸词表，
// 不带规则说明、匹配正则这类程序元信息。
func TestWheelPromptHasFourLayers(t *testing.T) {
	out, err := renderWheelFourLayerPrompt("母版正文", wheelP0Fixture, "行业红线：不承诺无隐形消费", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# 必说（本次任务）", "母版正文", "# 可说（事实包）", "fact_package",
		"# 不提（以下词句一律不许出现", "绝对词与极限词：最好、最佳、极致、第一",
		"行业红线：不承诺无隐形消费", "# 改写（禁说 → 该怎么说）"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in wheel prompt", want)
		}
	}
	if strings.Contains(out, "程序扫描用的元信息") {
		t.Fatal("rule notes are program metadata; they must not be sent to the generation model")
	}
	// 「不提」必须排在「改写」之前，语义顺序不能反。
	if strings.Index(out, "# 不提") > strings.Index(out, "# 改写") {
		t.Fatal("P0 red lines must come before rewrite guidance")
	}
	if _, err = renderWheelFourLayerPrompt("  ", "", "", ""); err == nil {
		t.Fatal("empty mother must be rejected")
	}
}

// P0 母版编译失败时不能把红线丢掉：宁可原文照用。
func TestWheelNotSayFallsBackToRawOnCompileError(t *testing.T) {
	raw := "编译不了的母版原文"
	if got := renderWheelNotSay(raw); got != raw {
		t.Fatalf("must fall back to raw policy, got %q", got)
	}
}
