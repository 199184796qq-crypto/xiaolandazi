package anchorcycle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/anchorstylescore"
)

func TestRecordedPriceReviewsKeepAllReasonsAndSkipDirectly(t *testing.T) {
	for _, id := range []string{"23569", "23588"} {
		t.Run(id, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "final-review-"+id+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				ExchangeID int64  `json:"exchange_id"`
				Text       string `json:"text"`
				Reply      string `json:"reply"`
			}
			if err = json.Unmarshal(raw, &record); err != nil {
				t.Fatal(err)
			}
			r, err := ParseReviewConclusion(record.Reply)
			if err != nil || r.Decision != "有问题" || r.Reason == "" || r.Quote == "" {
				t.Fatal("recorded rejection lost its explanation", r, err)
			}
			if id == "23569" && !strings.Contains(r.Reason, "上哪儿找去") {
				t.Fatal(r.Reason)
			}
			if id == "23588" && (!strings.Contains(r.Reason, "绝了") || !strings.Contains(r.Reason, "手慢可就真没了哟") || !strings.Contains(r.Reason, "库存")) {
				t.Fatal(r.Reason)
			}
			s := fixture(t, 2)
			s.FinalContentReviewRequired = true
			c := candidate(record.Text, 100)
			c.FinalContentAudit = &FinalContentAudit{Decision: r.Decision, Reason: r.Reason, Quote: r.Quote, Raw: record.Reply, ExchangeID: record.ExchangeID}
			if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || ok {
				t.Fatal(ok, err)
			}
			// 局部重写已移除：有效违规直接跳过（记入 GenerationSkips），不再调度局部重写。
			if s.P0PendingRepair != nil || len(s.GenerationSkips) == 0 || s.FinalReviewRecovery != nil {
				t.Fatal("valid rejection was treated as a service failure or lost in repair", s)
			}
			if s.GenerationSkips[len(s.GenerationSkips)-1].Stage != "final_content_review" {
				t.Fatal("skip stage lost", s.GenerationSkips)
			}
		})
	}
}

func TestReviewConclusionAcceptsPresentationChangesWithoutGuessing(t *testing.T) {
	for _, raw := range []string{"有问题，原价应为245元。", "结论：有问题\n原句‘197元’与团购价187元不一致。", "**有问题**：使用期限错误。", "```json\n{\"decision\":\"不通过\",\"reason\":\"价格错误\",\"quote\":\"197元\"}\n```"} {
		r, err := ParseReviewConclusion(raw)
		if err != nil || r.Decision != "有问题" || r.Reason == "" {
			t.Fatal(raw, r, err)
		}
	}
	for _, raw := range []string{"可能通过", "通过？", "未确认能不能通过", `{"decision":"通过","violations":[{"原因":"价格错误"}]}`, `{"decision":"通过"`, "```json\n{\"decision\":\"通过\"}"} {
		if r, err := ParseReviewConclusion(raw); err == nil {
			t.Fatal("ambiguous or unfinished review approved", raw, r)
		}
	}
}

func TestFinalReviewFailurePreservesPhaseQuotaAndEligibility(t *testing.T) {
	for _, phase := range []string{"opening", "body", "closing"} {
		for _, review := range []*FinalContentAudit{nil, {Error: "方向审核没有完成约定判断"}, {Decision: "通过", TextSHA: "old-text", ContextSHA: "old-context"}, {Decision: "有问题"}, {Decision: "不确定", Reason: "尚未提供可用日期"}} {
			s := mixFixture(t, 2, "review-failure")
			s.FinalContentReviewRequired, s.Phase, s.BodyCount = true, phase, 2
			s.SelectProductTask()
			before := ProductDigest(struct {
				Cursor, Cycle, Body, Opening int
				Focus                        *ProductFocus
				Points                       map[string]*WeightedSchedule
				Content                      *WeightedSchedule
				Uses                         map[string]Use
				Count                        map[string]int
			}{s.Cursor, s.CycleNumber, s.BodyCount, s.OpeningCount, s.ProductFocus, s.PointSchedules, s.ContentSchedule, s.Used, s.PointUses})
			originalTask := s.Task()
			c := candidate("套餐包含已确认的菜品。", 100)
			c.FinalContentAudit = review
			if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || ok {
				t.Fatal(ok, err)
			}
			after := ProductDigest(struct {
				Cursor, Cycle, Body, Opening int
				Focus                        *ProductFocus
				Points                       map[string]*WeightedSchedule
				Content                      *WeightedSchedule
				Uses                         map[string]Use
				Count                        map[string]int
			}{s.Cursor, s.CycleNumber, s.BodyCount, s.OpeningCount, s.ProductFocus, s.PointSchedules, s.ContentSchedule, s.Used, s.PointUses})
			if before != after || s.Phase != phase || len(s.P0SkippedKeys) != 0 || len(s.DuplicateSkippedKeys) != 0 || len(s.Accepted) != 0 || s.P0PendingRepair != nil {
				t.Fatal("unfinished review changed phase, quotas, or eligibility", phase, s)
			}
			if len(s.GenerationSkips) != 1 || len(s.GenerationSkips[0].Attempts) != 1 || s.GenerationSkips[0].Task.Key != originalTask.Key || s.FinalReviewRecovery == nil || s.FinalReviewReady(time.Now()) {
				t.Fatal("failure trace/backoff missing", s)
			}
			if phase == "body" && s.Task().Key == originalTask.Key {
				t.Fatal("available sibling angle was not selected")
			}
			if phase != "body" && s.Task().Key != originalTask.Key {
				t.Fatal("opening/closing task advanced")
			}
		}
	}
}

func TestReviewUnavailableCircuitIsPersistentAndValidReviewResetsIt(t *testing.T) {
	s := p0Fixture(t, 2)
	s.FinalContentReviewRequired = true
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		s.deferFinalReview(Candidate{ID: ID(), Text: "待审核稿", FinalContentAudit: &FinalContentAudit{Error: "审核超时"}}, now.Add(time.Duration(i)*time.Second))
	}
	if s.FinalReviewRecovery == nil || !s.FinalReviewRecovery.Paused || s.FinalReviewReady(now.Add(24*time.Hour)) || len(s.P0SkippedKeys) != 0 || len(s.Accepted) != 0 {
		t.Fatal("unavailable reviewer kept requesting or disabled content", s)
	}
	raw, err := json.Marshal(s)
	var restored State
	if err != nil || json.Unmarshal(raw, &restored) != nil || restored.FinalReviewReady(now.Add(time.Hour)) {
		t.Fatal("outage guard lost across persistence", err)
	}
	// A verified recovered receipt clears the service failure counter. An old
	// approval for a changed draft still cannot be used by FinalContentCandidateApproved.
	c := candidate("查看当前已确认的套餐内容。", 100)
	if err := s.CheckP0Candidate(&c); err != nil {
		t.Fatal(err)
	}
	s.FinalContentReviewContextSHA = "current"
	c.FinalContentAudit = &FinalContentAudit{Decision: "通过", TextSHA: ProductDigest(c.Text), ScopeSHA: s.FinalContentSourceSHA(), ContextSHA: "current"}
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || !ok || s.FinalReviewRecovery != nil || s.Error != "" {
		t.Fatal("valid recovered review did not clear failure state", ok, err, s)
	}
}
