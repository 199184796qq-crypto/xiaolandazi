package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func modelVersionForRewrite() model.LiveAgentPlanVersion {
	v := model.LiveAgentPlanVersion{}
	v.VoiceIdentity.VoiceID = "voice-a"
	v.VoiceIdentity.Model = "qwen"
	return v
}

func TestRewriteScriptAcceptableEnforcesLength(t *testing.T) {
	sample := strings.Repeat("这是原来的话术内容，讲价格讲工艺讲链接。", 20)
	// 不足 400 字也照用（用户口径：删句后短一点也要入库并合成）
	if !rewriteScriptAcceptable(modelVersionForRewrite(), sample, strings.Repeat("短", 399)) {
		t.Fatal("不足 400 字不应拒绝")
	}
	// 只有空内容才拒绝
	if rewriteScriptAcceptable(modelVersionForRewrite(), sample, "   ") {
		t.Fatal("空内容应拒绝")
	}
	// 重复度不再拦截：整段照抄在长度上合格就算通过
	if !rewriteScriptAcceptable(modelVersionForRewrite(), sample, sample) {
		t.Fatal("重复度不应再作为拒绝理由")
	}
}

func TestLongestCommonRunes(t *testing.T) {
	if got := longestCommonRunes([]rune("一二三四五六"), []rune("零三四五六七")); got != 4 {
		t.Fatalf("最长公共子串应为 4，实际 %d", got)
	}
	if got := longestCommonRunes(nil, []rune("abc")); got != 0 {
		t.Fatalf("空串应为 0，实际 %d", got)
	}
}

func TestParseRewriteOutputSplitsKV(t *testing.T) {
	skeleton, script := parseRewriteOutput("话术骨架：\n1. 甲 | 乙 | 丙 | 50%\n\n话术内容：\n正文在这里。")
	if !strings.Contains(skeleton, "甲") {
		t.Fatalf("骨架解析失败: %q", skeleton)
	}
	if script != "正文在这里。" {
		t.Fatalf("正文解析失败: %q", script)
	}
}

func TestParseScriptRewriteThreeKVAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      scriptRewriteOutput
	}{
		{"three fields", "话术骨架：入口 | 讲重点 | 50%\n一句话上下文：围绕送礼，先讲包装。\n改写内容：这是正文，价格是：八十七元六角。", scriptRewriteOutput{"入口 | 讲重点 | 50%", "围绕送礼，先讲包装。", "这是正文，价格是：八十七元六角。"}},
		{"CRLF BOM blanks", "\ufeff\r\n话术骨架:\r\n1. 包装 | 礼品 | 100%\r\n\r\n一句话上下文: 重点：包装。\r\n\r\n改写内容:\r\n这一条只讲包装。\r\n", scriptRewriteOutput{"1. 包装 | 礼品 | 100%", "重点：包装。", "这一条只讲包装。"}},
		{"legacy two", "话术骨架：\n甲\n乙\n话术内容：\n保留旧正文。", scriptRewriteOutput{"甲\n乙", "", "保留旧正文。"}},
		{"legacy body", "原来只返回可念的正文。正文中提到话术内容也不能被截断。", scriptRewriteOutput{Script: "原来只返回可念的正文。正文中提到话术内容也不能被截断。"}},
		{"body colon", "话术骨架：结构\n一句话上下文：物流\n改写内容：快递：按地址确认。", scriptRewriteOutput{"结构", "物流", "快递：按地址确认。"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseScriptRewriteOutput(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("got %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
}

func TestParseScriptRewriteMalformedMetadataNeverBecomesSpeech(t *testing.T) {
	for _, raw := range []string{
		"", "话术骨架：仅有骨架", "话术骨架：结构\n一句话上下文：只有简介",
		"话术骨架：结构\n一句话上下文：\n改写内容：正文。",
		"话术骨架：结构\n改写内容：漏上下文。",
		"一句话上下文：简介\n话术内容：正文。",
		"话术骨架：结构\n一句话上下文：简介\n改写内容：",
		"话术骨架：结构\n一句话上下文：简介\n改写内容：一\n改写内容：二",
		"话术骨架：结构\n一句话上下文：简介\n改写内容：一\n话术内容：二",
		"解释文字\n话术骨架：结构\n一句话上下文：简介\n改写内容：正文。",
		"话术骨架\n仅有断开的标签", "{\"改写内容\":\"不能将 JSON 当口播\"}", "```\n话术内容：正文。\n```",
	} {
		if got, err := parseScriptRewriteOutput(raw); err == nil || got.Script != "" {
			t.Errorf("malformed output accepted: %q => %#v, %v", raw, got, err)
		}
		if _, body := parseRewriteOutput(raw); body != "" {
			t.Errorf("legacy wrapper leaked metadata: %q", body)
		}
	}
}
