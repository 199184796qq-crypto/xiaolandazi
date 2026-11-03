package planprompt

import (
	"strings"
	"testing"
)

func TestCustomerOutputFormatIsReplacedByContract(t *testing.T) {
	planning := "【输出要求】\n1. 正文不要出现英文\n\n【输出格式】\n输出 JSON 给我\n\n【其它要求】\n2. 结尾温柔一点"
	got := CompileRewritePlanning(Sections{PersonaMD: "主播"}, planning)

	if strings.Contains(got, "输出 JSON 给我") {
		t.Fatal("客户写的输出格式应被丢弃")
	}
	if strings.Count(got, "【输出格式（系统保留") != 1 {
		t.Fatalf("系统契约应只出现一次: %d", strings.Count(got, "【输出格式（系统保留"))
	}
	if !strings.Contains(got, "正文不要出现英文") || !strings.Contains(got, "结尾温柔一点") {
		t.Fatalf("输出要求与其它要求应保留: %s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "不得输出这三个字段以外的任何内容。") {
		t.Fatalf("系统契约必须在末尾: %s", got)
	}
}

func TestOutputFormatWithoutFollowingHeadingDropsToEnd(t *testing.T) {
	planning := "【输出要求】\n1. 简洁\n\n【输出格式】\n随便输出什么都行\n换行也算"
	got := CompileRewritePlanning(Sections{}, planning)
	if strings.Contains(got, "随便输出什么都行") || strings.Contains(got, "换行也算") {
		t.Fatal("输出格式后面的内容应一并丢弃")
	}
	if !strings.Contains(got, "1. 简洁") {
		t.Fatal("前面的输出要求应保留")
	}
}

func TestPlanningWithoutOutputFormatKeepsEverything(t *testing.T) {
	planning := "【输出要求】\n1. 口语化"
	got := CompileRewritePlanning(Sections{}, planning)
	if !strings.Contains(got, "1. 口语化") {
		t.Fatal("没有输出格式时不应丢内容")
	}
}