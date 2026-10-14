package httpapi

import (
	"strings"
	"testing"
)

func TestMergeAnchorStyleSections(t *testing.T) {
	results := map[string]string{
		"语气词":   "哟、啊、嘛",
		"称呼与自指": "哥哥姐姐们、我们家",
		"句式与节奏": "短句、反问",
		"数字表达":  "拆价对比",
		"方言与口头禅": "咋个、正儿八经",
		"情绪":    "热情接地气",
		"禁用词":   "亲亲、宝子",
		"全局规则":  "开头轮换",
	}
	md := mergeAnchorStyleSections(results)

	// 四章节齐全
	for _, s := range []string{"【口语风格】", "【情绪】", "【禁用词】", "【全局规则】"} {
		if !strings.Contains(md, s) {
			t.Fatalf("缺少章节 %s:\n%s", s, md)
		}
	}
	// 前五维归入口语风格，后三维各一章节
	for _, dim := range anchorStyleSpeechSections {
		if !strings.Contains(md, "**"+dim+"**") {
			t.Fatalf("口语风格缺少维度 %s:\n%s", dim, md)
		}
	}
	// 不应出现 markdown 标题（扁平结构）
	if strings.Contains(md, "## ") {
		t.Fatalf("不应有 markdown 标题:\n%s", md)
	}
}

func TestAnchorStyleDimensionsCount(t *testing.T) {
	if len(anchorStyleDimensions) != 8 {
		t.Fatalf("维度数应为 8，实际 %d", len(anchorStyleDimensions))
	}
	if len(anchorStyleSpeechSections) != 5 {
		t.Fatalf("口语风格维度应为 5，实际 %d", len(anchorStyleSpeechSections))
	}
}

func TestCleanAnchorStyleDimensionContent(t *testing.T) {
	// 语气词：带标签+编号+子列表+换行（模型实际输出的脏格式）
	dirty := "【语气词特征】\n1. 句末助词（高频）：\n   - \"哟\"：极高频出现，用于陈述句。\n   - \"啊\"：用于感叹。\n\n2. 独立语气词（中频）：\n   - \"对呀\"：用于自我确认。"
	got := cleanAnchorStyleDimensionContent("语气词", dirty)
	if strings.Contains(got, "【") || strings.Contains(got, "\n") {
		t.Fatalf("清理后仍有标签或换行: %q", got)
	}
	if !strings.Contains(got, "哟") || !strings.Contains(got, "对呀") {
		t.Fatalf("清理后丢失内容: %q", got)
	}

	// 称呼：开头维度名重复
	got2 := cleanAnchorStyleDimensionContent("称呼与自指", "称呼与自指：高频使用“哥哥姐姐们”。")
	if strings.HasPrefix(got2, "称呼与自指") {
		t.Fatalf("维度名重复未去除: %q", got2)
	}

	// 全局规则：开头【全局规则】标签重复
	got3 := cleanAnchorStyleDimensionContent("全局规则", "【全局规则】\n1. 链接叫法规范：统一采用编号格式。")
	if strings.Contains(got3, "【全局规则】") || strings.Contains(got3, "\n") {
		t.Fatalf("标签或换行未清理: %q", got3)
	}
}
