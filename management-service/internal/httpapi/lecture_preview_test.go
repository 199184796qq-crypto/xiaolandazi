package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"os"
	"testing"
)

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
