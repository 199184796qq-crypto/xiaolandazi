package httpapi

import (
	"strings"
	"testing"
)

func TestMergeAnchorStyleSections(t *testing.T) {
	results := map[string]string{
		"语气词":    "哟、啊、嘛",
		"称呼与自指":  "哥哥姐姐们、我们家",
		"句式与节奏":  "短句、反问",
		"方言与口头禅": "咋个、正儿八经",
	}
	md := mergeAnchorStyleSections(results)

	// 只有【口语风格】一个章节
	if !strings.Contains(md, "【口语风格】") {
		t.Fatalf("缺少章节【口语风格】:\n%s", md)
	}
	// 前四维归入口语风格
	for _, dim := range anchorStyleSpeechSections {
		if !strings.Contains(md, "**"+dim+"**") {
			t.Fatalf("口语风格缺少维度 %s:\n%s", dim, md)
		}
	}
	// 不应出现情绪/禁用词/全局规则章节（只保留语气风格）
	for _, s := range []string{"【情绪】", "【禁用词】", "【全局规则】"} {
		if strings.Contains(md, s) {
			t.Fatalf("不应有章节 %s:\n%s", s, md)
		}
	}
	// 不应出现 markdown 标题（扁平结构）
	if strings.Contains(md, "## ") {
		t.Fatalf("不应有 markdown 标题:\n%s", md)
	}
}

func TestAnchorStyleDimensionsCount(t *testing.T) {
	if len(anchorStyleDimensions) != 4 {
		t.Fatalf("维度数应为 4，实际 %d", len(anchorStyleDimensions))
	}
	if len(anchorStyleSpeechSections) != 4 {
		t.Fatalf("口语风格维度应为 4，实际 %d", len(anchorStyleSpeechSections))
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

	// 称呼与自指：开头【称呼与自指】标签重复
	got3 := cleanAnchorStyleDimensionContent("称呼与自指", "【称呼与自指】\n1. 主力称呼「哥哥姐姐们」，自指「我们家」。")
	if strings.Contains(got3, "【称呼与自指】") || strings.Contains(got3, "\n") {
		t.Fatalf("标签或换行未清理: %q", got3)
	}
}
