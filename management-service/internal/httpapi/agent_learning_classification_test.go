package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestNormalizeAgentLearningMemoryTypeUnknownDefaultsToSemantic(t *testing.T) {
	if got := normalizeAgentLearningMemoryType("unknown"); got != model.AgentMemoryTypeSemantic {
		t.Fatalf("unknown memory type normalized to %q, want semantic", got)
	}
}

func TestEnforceAgentLearningClassificationReclassifiesExampleInstruction(t *testing.T) {
	session := model.AgentLearningSession{
		Question: "1号车这个整鸡有哪些做法？",
		Target:   "1号车商品食用方法",
	}
	output := agentLearningModelOutput{
		MemoryType: model.AgentMemoryTypeFact,
		Target:     "1号车商品食用方法",
		MemoryKey:  "product:1:cooking_methods",
		ResultText: "1号车商品（整鸡）有多种做法，包括大盘鸡、炖汤、红烧、白切、烤鸡。",
		Structured: map[string]any{
			"fact_key": "cooking_methods",
			"value":    []string{"大盘鸡", "炖汤", "红烧", "白切", "烤鸡"},
			"source":   "model_inferred",
		},
	}

	got := enforceAgentLearningClassification(session, "有很多做法，你来举例", output)
	if got.MemoryType != model.AgentMemoryTypeSemantic {
		t.Fatalf("memory type=%q, want semantic", got.MemoryType)
	}
	if got.MatchedMemoryItemID != 0 {
		t.Fatalf("matched memory id=%d, want 0 after reclassification", got.MatchedMemoryItemID)
	}
	if strings.Contains(got.ResultText, "大盘鸡") || strings.Contains(got.ResultText, "白切") {
		t.Fatalf("model-invented examples leaked into stored result: %s", got.ResultText)
	}
	if !strings.Contains(got.ResultText, "有很多做法，你来举例") {
		t.Fatalf("stored result lost user instruction: %s", got.ResultText)
	}
	if !strings.Contains(got.ResultText, "通用常识") {
		t.Fatalf("stored strategy should allow runtime common-knowledge examples: %s", got.ResultText)
	}
	if got.Structured["semantic_mode"] != "response_strategy" {
		t.Fatalf("semantic_mode=%v, want response_strategy", got.Structured["semantic_mode"])
	}
	if _, exists := got.Structured["fact_key"]; exists {
		t.Fatal("fact_key should be removed after response-strategy reclassification")
	}
	if _, exists := got.Structured["value"]; exists {
		t.Fatal("fact value should be removed after response-strategy reclassification")
	}
}

func TestEnforceAgentLearningClassificationKeepsConfirmedFact(t *testing.T) {
	output := agentLearningModelOutput{
		MemoryType: model.AgentMemoryTypeFact,
		Target:     "价格状态",
		MemoryKey:  "price_status",
		ResultText: "价格等下就开。",
		Structured: map[string]any{"fact_key": "price_status", "value": "价格等下就开", "source": "human_confirmed"},
	}

	got := enforceAgentLearningClassification(model.AgentLearningSession{}, "价格等下就开", output)
	if got.MemoryType != model.AgentMemoryTypeFact {
		t.Fatalf("memory type=%q, want fact", got.MemoryType)
	}
	if got.ResultText != output.ResultText {
		t.Fatalf("confirmed fact result changed: %q", got.ResultText)
	}
}

func TestResponseStrategyCueRecognizesHowToAnswerLanguage(t *testing.T) {
	cases := []string{
		"有很多做法，你来举例",
		"以后有人问价格，先说价格等下就开",
		"遇到这类问题不要只说一种做法，多举几个",
		"先说库存，再说价格",
	}
	for _, tc := range cases {
		if !agentLearningLooksLikeResponseStrategy(tc) {
			t.Fatalf("feedback %q should be recognized as response strategy", tc)
		}
	}
	if agentLearningLooksLikeResponseStrategy("价格等下就开") {
		t.Fatal("plain confirmed business fact should not be recognized as response strategy")
	}
	if agentLearningLooksLikeResponseStrategy("先说一下，我们在绵阳山河") {
		t.Fatal("standalone conversational '先说' should not force response-strategy classification")
	}
}

func TestStrongAgentLearningMessageIntentSeparatesLearningTestAndChat(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{message: "不要太官方", want: "learning"},
		{message: "这个说法再自然一点", want: "learning"},
		{message: "以后有人问价格先说价格等下就开", want: "learning"},
		{message: "这个纠正非常好，保留好的，纠正错误", want: "learning"},
		{message: "其他不变，只改错误的地方", want: "learning"},
		{message: "你测试下", want: "test"},
		{message: "试试看", want: "test"},
		{message: "帮我回答这条弹幕：多少钱", want: "execution"},
		{message: "给这个用户回复，价格等下公布", want: "execution"},
		{message: "你不要太累了", want: ""},
		{message: "今天好累", want: ""},
		{message: "哈哈你还挺聪明", want: ""},
		{message: "我先去吃个饭", want: ""},
	}
	for _, tc := range cases {
		if got := strongAgentLearningMessageIntent(tc.message); got != tc.want {
			t.Fatalf("message %q intent=%q, want %q", tc.message, got, tc.want)
		}
	}
}

func TestAgentLearningRequestsFullRewriteOnlyOnExplicitCues(t *testing.T) {
	fullRewrite := []string{
		"全部重写一遍",
		"这个推翻重来",
		"原来的都不要，从头重写",
	}
	for _, feedback := range fullRewrite {
		if !agentLearningRequestsFullRewrite(feedback) {
			t.Fatalf("feedback %q should request full rewrite", feedback)
		}
	}

	incremental := []string{
		"这个纠正非常好，保留好的，纠正错误",
		"其他不变，只改这个地方",
		"这个县名改一下",
	}
	for _, feedback := range incremental {
		if agentLearningRequestsFullRewrite(feedback) {
			t.Fatalf("feedback %q should stay incremental", feedback)
		}
	}
}
