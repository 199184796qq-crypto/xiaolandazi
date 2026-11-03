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
