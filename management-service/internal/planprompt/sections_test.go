package planprompt

import (
	"strings"
	"testing"
)

func TestComposeSeparatesUseCasesAndNormalizesHints(t *testing.T) {
	s := (Sections{"自然讲话\n〔提示〕不要发送", "900克87.6元", "整轮循环讲品", "只答当前问题"}).Normalize()
	if strings.Contains(s.Interaction(), "整轮循环讲品") || strings.Contains(s.Speech(), "只答当前问题") {
		t.Fatal("requirements leaked across uses")
	}
	for _, got := range []string{s.Interaction(), s.Speech()} {
		if strings.Count(got, "自然讲话") != 1 || strings.Count(got, "900克87.6元") != 1 || strings.Contains(got, "〔提示〕") {
			t.Fatal(got)
		}
	}
	if err := (Sections{PersonaMD: strings.Repeat("字", 20000)}).Validate(); err == nil {
		t.Fatal("compiled headings must count toward the limit")
	}
	if got := (Sections{}).Interaction(); got != "" {
		t.Fatal(got)
	}
}

func TestCompileDoesNotChangePageOriginals(t *testing.T) {
	s := Sections{
		PersonaMD:                 "  原始人设\r\n〔提示〕只保留在原文\r\n  ",
		FactsMD:                   "\n【互动回答要求】\n这个标题仍属于事实页\n",
		SpeechRequirementsMD:      "\n\n",
		InteractionRequirementsMD: " 原始互动要求 ",
	}
	original := s
	interaction, speech := s.Interaction(), s.Speech()
	if err := s.Validate(); err != nil || s != original {
		t.Fatal("compilation/validation changed original fields", s, err)
	}
	for _, compiled := range []string{interaction, speech} {
		if strings.Contains(compiled, "〔提示〕") || strings.Contains(compiled, "\r") || !strings.Contains(compiled, "这个标题仍属于事实页") {
			t.Fatal("compiled cleanup changed", compiled)
		}
	}
}

func TestLegacyProjectionKeepsFactsUnknownTextAndDistinctRequirements(t *testing.T) {
	raw := "前言\n【身份设定】\n我是主播\n【商品卡】\n1号900克87.6\n【事实依据】\n发京东\n【用词消歧】\n砍指切块\n【直播目标】\n循环介绍\n【话术规则】\n自然衔接\n【互动约束】\n先回答问题\n【未知附录】\n保留附录\n【输出格式要求】\n回答：正文"
	s, unstructured := SplitLegacy(raw)
	if unstructured {
		t.Fatal("recognised legacy sections marked unknown")
	}
	for _, value := range []string{"1号900克87.6", "发京东", "砍指切块"} {
		if !strings.Contains(s.FactsMD, value) {
			t.Fatal(value, s)
		}
	}
	if !strings.Contains(s.Interaction(), "保留附录") || !strings.Contains(s.Interaction(), "前言") || strings.Contains(s.Interaction(), "循环介绍") || strings.Contains(s.Speech(), "先回答问题") {
		t.Fatal(s)
	}
	free, unknown := SplitLegacy("没有分节的原文，全部保留。")
	if !unknown || free.InteractionRequirementsMD != "没有分节的原文，全部保留。" {
		t.Fatal(free, unknown)
	}
	for _, title := range []string{"主播人设", "事实依据", "直播话术要求", "互动回答要求"} {
		if strings.Count(s.Interaction(), "【"+title+"】") > 1 {
			t.Fatal("duplicate outer heading", title)
		}
	}
}
