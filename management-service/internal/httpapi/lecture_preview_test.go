package httpapi

import (
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"os"
	"testing"
)

func TestLectureAuditQuoteRetryIsBoundedAndNeverInventsApproval(t *testing.T) {
	calls := 0
	out, err := boundedLectureAudit("当前正文", func(previous string) (string, error) {
		calls++
		if calls == 1 {
			return `{"passed":false,"reason":"待核对","quote":"另一段正文"}`, nil
		}
		if previous == "" {
			t.Fatal("retry lost malformed result")
		}
		return `{"passed":true,"reason":"当前正文有依据"}`, nil
	})
	if err != nil || !out.Passed || calls != 2 {
		t.Fatal(out, err, calls)
	}
	calls = 0
	_, err = boundedLectureAudit("当前正文", func(string) (string, error) { calls++; return `{"passed":true,"quote":"另一段正文"}`, nil })
	if !errors.Is(err, errLectureAuditQuoteMismatch) || calls != 2 {
		t.Fatal("unbounded retry or invented approval", err, calls)
	}
	calls = 0
	out, err = boundedLectureAudit("当前正文", func(string) (string, error) { calls++; return `{"passed":false,"reason":"新增未确认事实"}`, nil })
	if err != nil || out.Passed || calls != 1 {
		t.Fatal("valid content rejection retried", out, err, calls)
	}
}

func TestLectureReviewSuggestionsDoNotBlockApprovedText(t *testing.T) {
	text := "今天发，明天到。"
	audit, err := parseLectureTaskAudit(`{"passed":true,"suggestions":[{"quote":"明天到","reason":"建议用户核对到货安排，也可在事实中设置负向要求"}]}`, text)
	if err != nil || !audit.Passed || len(audit.Suggestions) != 1 {
		t.Fatal(audit, err)
	}
	state := anchorcycle.State{Config: anchorcycle.Config{MaxChars: 220}, Lectures: &anchorcycle.LectureRuntime{Active: &anchorcycle.Task{ProductMode: "lecture", LectureFacts: "发货安排"}}}
	if err := installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	c := lectureTextCandidate(text)
	if err := state.CheckP0Candidate(&c); err != nil {
		t.Fatal(err)
	}
	audit.TextSHA, audit.ScopeSHA = anchorcycle.ProductDigest(cycleVoiceText(c)), anchorcycle.ProductDigest(state.Task().LectureFacts)
	c.LectureAudit = &audit
	out := lecturePreviewContent(&state, c, "")
	if out["approved"] != true || out["review_state"] != "approved" || len(out["issues"].([]lecturePreviewIssue)) != 0 || len(out["review_suggestions"].([]anchorcycle.LectureReviewSuggestion)) != 1 {
		t.Fatal(out)
	}
	if _, err := parseLectureTaskAudit(`{"passed":true,"suggestions":[{"quote":"别的正文","reason":"核对"}]}`, text); !errors.Is(err, errLectureAuditQuoteMismatch) {
		t.Fatal(err)
	}
}

func TestLectureTaskAuditHistoricalExplanationIsNotFailure(t *testing.T) {
	raw, err := os.ReadFile("testdata/lecture-preview-audit-20261011.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		ID     int64           `json:"id"`
		Text   string          `json:"text"`
		Review json.RawMessage `json:"review"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatal("historical previews missing")
	}
	for _, row := range rows {
		audit, err := parseLectureTaskAudit(string(row.Review), row.Text)
		if err != nil || !audit.Passed {
			t.Fatal(row.ID, audit, err)
		}
	}
	for _, raw := range []string{`{"passed":true}`, "```json\n{\"passed\":true,\"reason\":\"有明确事实依据\"}\n```"} {
		if audit, err := parseLectureTaskAudit(raw, "当前正文"); err != nil || !audit.Passed {
			t.Fatal(audit, err)
		}
	}
	rejected, err := parseLectureTaskAudit(`{"passed":false,"reason":"新增未确认事实"}`, "当前正文")
	if err != nil || rejected.Passed || rejected.Reason != "新增未确认事实" {
		t.Fatal(rejected, err)
	}
	for _, raw := range []string{`{}`, `{"passed":"true"}`, `{"passed":false}`, `{"passed":true,"quote":"另一段的正文"}`} {
		if _, err := parseLectureTaskAudit(raw, "当前正文"); err == nil {
			t.Fatal("invented approval", raw)
		}
	}
}

func TestLecturePreviewPreservesBodyAndSeparatesReviewFailure(t *testing.T) {
	state := anchorcycle.State{Config: anchorcycle.Config{MaxChars: 220}, Lectures: &anchorcycle.LectureRuntime{Active: &anchorcycle.Task{ProductMode: "lecture", LectureFacts: "产地信息已经确认", SellingPoint: "产地"}}}
	if err := installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	c := anchorcycle.Candidate{Text: "这款商品的产地信息在资料中。"}
	if err := state.CheckP0Candidate(&c); err != nil {
		t.Fatal(err)
	}
	c.LectureAudit = &anchorcycle.LectureAudit{Passed: true, Reason: "有明确事实依据", TextSHA: anchorcycle.ProductDigest(c.Text), ScopeSHA: anchorcycle.ProductDigest(state.Task().LectureFacts)}
	result := lecturePreviewContent(&state, c, "")
	if result["approved"] != true || result["raw_text"] != c.Text || result["review_note"] != "有明确事实依据" {
		t.Fatal(result)
	}
	c.LectureAudit.Passed, c.LectureAudit.Reason = false, "新增未确认事实"
	for _, kind := range []string{"fact_boundary", "review_error"} {
		if kind == "review_error" {
			c.LectureAudit.Error = "审核返回格式不完整"
		}
		result = lecturePreviewContent(&state, c, "")
		issues := result["issues"].([]lecturePreviewIssue)
		if result["approved"] != false || result["text"] != c.Text || len(issues) != 1 || issues[0].Kind != kind {
			t.Fatal(result)
		}
	}
	c.LectureAudit.Passed, c.LectureAudit.Error = true, ""
	c.P0ApprovedTextSHA = ""
	result = lecturePreviewContent(&state, c, "")
	if result["approved"] != false || result["text"] != c.Text {
		t.Fatal("preview bypassed P0 receipt", result)
	}
	result = lecturePreviewContent(&state, c, "读取审核规则失败")
	if result["approved"] != false || result["raw_text"] != c.Text {
		t.Fatal("scan failure lost generated body", result)
	}
}

func TestLecturePreviewNormalizesBeforeReviewAndKeepsReceiptIdentity(t *testing.T) {
	state := anchorcycle.State{Lectures: &anchorcycle.LectureRuntime{Active: &anchorcycle.Task{ProductMode: "lecture", LectureFacts: "资料描述原料来源"}}}
	if err := installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"这段讲解介绍原料来源", " 这段讲解介绍原料来源。\n", "这段讲解介绍原料来源！"} {
		c := lectureTextCandidate(raw)
		if err := state.CheckP0Candidate(&c); err != nil {
			t.Fatal(err)
		}
		identity := c.P0CheckedInputSHA
		c.LectureAudit = &anchorcycle.LectureAudit{Passed: true, TextSHA: anchorcycle.ProductDigest(cycleVoiceText(c)), ScopeSHA: anchorcycle.ProductDigest(state.Task().LectureFacts)}
		preview := lecturePreviewContent(&state, c, "")
		if preview["approved"] != true || c.P0CheckedInputSHA != identity || state.CheckApprovedText(c, cycleVoiceText(c)) != nil {
			t.Fatal("unchanged final body lost its approval", raw, preview)
		}
		c.OutputText += "另加一句。"
		if lecturePreviewContent(&state, c, "")["approved"] != false {
			t.Fatal("changed body reused old receipt")
		}
	}
}
