package anchorcycle

import (
	"livecompanion/management/internal/anchorstylescore"
	"testing"
)

func TestFinalContentAdmissionRequiresCurrentReceiptAndKeepsFailureTrace(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Surface = "formal_live"
	s.FinalContentReviewRequired = true
	s.FinalContentReviewContextSHA = "review-context"
	c := candidate("包装可以按食用计划选择。", 100)
	if ok, e := s.AddCandidate(c, anchorstylescore.Baseline{}); e != nil || ok {
		t.Fatal("missing final approval admitted", e)
	}
	if len(s.Accepted) != 0 || len(s.GenerationSkips) != 1 || len(s.GenerationSkips[0].Attempts) != 1 || s.Status != "ready" {
		t.Fatal("final failure blocked or lost trace")
	}
	c = candidate("查看当前包装条件再选择。", 100)
	if e := s.CheckP0Candidate(&c); e != nil {
		t.Fatal(e)
	}
	c.FinalContentAudit = &FinalContentAudit{Decision: "通过", TextSHA: ProductDigest(spokenCandidate(c)), ScopeSHA: s.FinalContentSourceSHA(), ContextSHA: s.FinalContentReviewContextSHA}
	if !s.FinalContentCandidateApproved(c) {
		t.Fatal("current final approval lost")
	}
	changed := c
	changed.OutputText = "重新生成了别的话。"
	if s.FinalContentCandidateApproved(changed) {
		t.Fatal("text changed but approval survived")
	}
	s.Facts = "新的商品事实"
	if s.FinalContentCandidateApproved(c) {
		t.Fatal("facts changed but approval survived")
	}
	s.Facts = ""
	c.FinalContentAudit.ScopeSHA = s.FinalContentSourceSHA()
	s.FinalContentReviewContextSHA = "new-review-context"
	if s.FinalContentCandidateApproved(c) {
		t.Fatal("model or mother changed but approval survived")
	}
	c.FinalContentAudit.ContextSHA = s.FinalContentReviewContextSHA
	c.P0Report.Passed = false
	if s.FinalContentCandidateApproved(c) {
		t.Fatal("second audit overrode failed P0")
	}
}

func TestFrozenExpressionRequirementIsIndependentFromFinalFactApproval(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Queue[0].FactScope = &TaskFactScope{Corrections: []map[string]any{{"表达限制": "提及退款时保留第一节课结束24小时内的条件。"}, {"用户确认的事实修正": "每次课程60分钟"}, {"展开指导": "从购买前的顾虑展开"}}}
	if len(s.EffectiveExpressionConstraints()) != 1 {
		t.Fatal("facts or optional guidance became mandatory speech")
	}
	s.FinalContentReviewRequired = true
	s.FinalContentReviewContextSHA = "context"
	c := candidate("头节课程结束24小时内可以申请退款。", 100)
	if e := s.CheckP0Candidate(&c); e != nil {
		t.Fatal(e)
	}
	c.FinalContentAudit = &FinalContentAudit{Decision: "通过", TextSHA: ProductDigest(spokenCandidate(c)), ScopeSHA: s.FinalContentSourceSHA(), ContextSHA: s.FinalContentReviewContextSHA}
	if s.FinalContentCandidateApproved(c) {
		t.Fatal("final fact approval overrode missing expression receipt")
	}
	c.ExpressionAudit = &LectureAudit{Passed: true, TextSHA: ProductDigest(spokenCandidate(c)), ScopeSHA: ProductDigest(s.EffectiveExpressionConstraints())}
	if !s.FinalContentCandidateApproved(c) {
		t.Fatal("separate current receipts rejected")
	}
	s.Queue[0].FactScope.Corrections[0]["表达限制"] = "必须保留第一次上课后24小时内及线上申请条件。"
	if s.LectureCandidateApproved(c) {
		t.Fatal("changed frozen requirement reused old receipt")
	}
	c.ExpressionAudit.Passed = false
	if ok, e := s.AddCandidate(c, anchorstylescore.Baseline{}); e != nil || ok || len(s.GenerationSkips) != 1 || len(s.GenerationSkips[0].Attempts) != 1 || s.Status != "ready" {
		t.Fatal("requirement failure lost trace or stopped mainline", e)
	}
}
