package httpapi

import (
	"context"
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
	// 有「四层母版」时为 5 条（fourLayer+P0+mother+facts+user），无则 4 条（P0+mother+facts+user）。
	if len(messages) != 4 && len(messages) != 5 {
		t.Fatal("unexpected message count", len(messages))
	}
	// 最后一条必须是 user，且 P0 不能混进 user 内容。
	if messages[len(messages)-1].Role != "user" || strings.Contains(messages[len(messages)-1].Content, "完整有效P0") {
		t.Fatal("P0 became user content instead of independent system rules")
	}
	// P0 必须作为独立 system 消息存在。
	p0Independent := false
	for _, m := range messages {
		if m.Role == "system" && strings.Contains(m.Content, "完整有效P0") {
			p0Independent = true
			break
		}
	}
	if !p0Independent {
		t.Fatal("P0 lost independent system role")
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
	before := s.FinalContentSourceSHA()
	s.LatestExpressionConstraints = []map[string]any{{"表达限制": "不用夜宵举例", "用户采用的实时表达约束": true}}
	if before == s.FinalContentSourceSHA() {
		t.Fatal("adopted constraint did not invalidate final approval")
	}
	messages, e = finalMainlineContentMessages(&s, c, "mother")
	raw, _ = json.Marshal(messages)
	if e != nil || !strings.Contains(string(raw), "不用夜宵举例") {
		t.Fatal("live adopted requirement omitted", e)
	}
}

func TestCompletedRequirementRejectionIsNotAFormatOrTransportFailure(t *testing.T) {
	s := cycleTestState(t)
	p0, source, e := loadP0Mother()
	if e != nil {
		t.Fatal(e)
	}
	if e = s.InstallP0(p0, source); e != nil {
		t.Fatal(e)
	}
	s.Queue[0].FactScope = &anchorcycle.TaskFactScope{Corrections: []map[string]any{{"表达限制": "不把商品作为早餐举例"}}}
	mother, _, e := loadReplaceablePromptAsset("ANCHOR_SPEECH_CONSTRAINT_TEXT_REVIEW_PATH", "configs/prompts/anchor-style/speech-constraint-text-review.md", "requirement review")
	if e != nil {
		t.Fatal(e)
	}
	c := anchorcycle.Candidate{ID: "program-case", Text: "早上赶时间，开袋直接当早餐。", OutputText: "早上赶时间，开袋直接当早餐。"}
	c.ExpressionAudit = &anchorcycle.LectureAudit{Passed: false, Reason: "包含用户禁止的早餐场景", TextSHA: anchorcycle.ProductDigest(c.OutputText), ScopeSHA: anchorcycle.ProductDigest(s.EffectiveExpressionConstraints()), PromptSHA: derivedHash(mother)}
	server := &Server{}
	server.auditFinalMainlineContent(context.Background(), &s, &c)
	if c.FinalContentAudit == nil || c.FinalContentAudit.Decision != "有问题" || c.FinalContentAudit.Error != "" || c.FinalContentAudit.UpstreamReview != "speech_constraint_text_review" || c.FinalContentAudit.ExchangeID != 0 || s.FinalContentCandidateApproved(c) {
		t.Fatal("completed content rejection became a format failure or a fabricated final model call", c.FinalContentAudit)
	}
}
