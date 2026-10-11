package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"strings"
	"testing"
)

func TestFinalReviewIncludesValidatedP0AndScopedContentWithoutIndices(t *testing.T) {
	s := cycleTestState(t)
	p0, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.InstallP0(p0, source); e != nil {
		t.Fatal(e)
	}
	s.Queue[0].FactScope = &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "private-fact", Products: []string{"1"}, Data: "未开封常温抽真空6天，不抽真空3天。"}}, ProductBoundary: []anchorcycle.HookFact{{ID: "private-boundary", Data: "即食熟食。"}}, Corrections: []map[string]any{{"表达限制": "不用早餐举例"}}}
	s.LatestExpressionConstraints = []map[string]any{{"表达限制": "也不能暗示早餐食用"}}
	c := anchorcycle.Candidate{ID: "private-candidate", Text: "未开封常温抽真空能放6天。", OutputText: "未开封常温抽真空能放6天。"}
	if e = s.CheckP0Candidate(&c); e != nil {
		t.Fatal(e)
	}
	messages, e := finalMainlineContentMessages(&s, c, "mother")
	if e != nil {
		t.Fatal(e)
	}
	if len(messages) != 3 || messages[0].Role != "system" || messages[1].Role != "system" || messages[2].Role != "user" || !strings.Contains(messages[0].Content, "完整有效P0") || strings.Contains(messages[2].Content, "完整有效P0") {
		t.Fatal("P0 became user content instead of independent system rules")
	}
	raw, _ := json.Marshal(messages)
	for _, word := range []string{"前次P0审核结果", "完整有效P0", "商品必要事实", "不用早餐举例", "也不能暗示早餐食用"} {
		if !strings.Contains(string(raw), word) {
			t.Fatal("lost required content", word)
		}
	}
	for _, word := range []string{"private-fact", "private-boundary", "private-candidate", c.P0CheckedInputSHA} {
		if strings.Contains(string(raw), word) {
			t.Fatal("index responsibility leaked", word)
		}
	}
}
