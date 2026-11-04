package decisionexecutor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const reportV2Body = "编号：U01\n意图：自然提一句时间｜置信度：95｜建议：简短问候\n类型：闲聊\n优先程度：普通\n优先原因：时间问候\n歧义：否\n候选：\n澄清句：\n话术要求：自然问候\n话术：下午了，感谢大家陪着。\n结束：U01"

func TestV2ReportUsesProgramClockAndNeedsNoModelExpiry(t *testing.T) {
	w, _, core, agent, tts, session, _ := v2Fixture(t, 9064101)
	now := time.Date(2026, 11, 4, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	core.claimRaw = reportClaim("report-v2-clock")
	agent.responses = []string{reportV2Body}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 1 || core.dispatches != 1 {
		t.Fatal("report did not reach audio", agent.calls, tts.calls, core.dispatches)
	}
	req := agent.requests[0]
	user := req.Messages[len(req.Messages)-1].Content
	if !strings.Contains(user, `"当前北京时间":"2026-11-04 14:20"`) || !strings.Contains(user, `"当前时段":"下午"`) {
		t.Fatal("report clock missing", user)
	}
	until, ok := core.dispatch["time_report_valid_until"].(time.Time)
	if !ok || until.In(beijingZone).Hour() != 18 {
		t.Fatal("program validity missing", until)
	}
}

func TestV2ReportTTSRetryKeepsOriginalTextAndGenerationClock(t *testing.T) {
	w, _, core, agent, tts, session, _ := v2Fixture(t, 9064102)
	now := time.Date(2026, 11, 4, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	core.claimRaw = reportClaim("report-first")
	agent.responses = []string{reportV2Body}
	tts.err = errors.New("temporary TTS failure")
	if err := w.processRoom(context.Background(), session); err == nil {
		t.Fatal("TTS fixture did not fail")
	}
	now = now.Add(3 * time.Minute)
	tts.err = nil
	core.claimRaw = reportClaim("report-second")
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 2 {
		t.Fatal("valid text was repurchased", agent.calls, tts.calls)
	}
	if core.dispatch["decision_id"] != "report-second" {
		t.Fatal("cached text retained old execution ID", core.dispatch)
	}
	if core.dispatch["interrupt_strategy"] != "hard_cut" || core.dispatch["deferred_return"] != false {
		t.Fatal("text retry reverted to legacy content/style metadata", core.dispatch)
	}
}

func TestNonReportV2RequestDoesNotReceiveProgramClock(t *testing.T) {
	w, _, core, agent, tts, session, _ := v2Fixture(t, 9064103)
	core.claimRaw = `{"claimed":true,"item":{"id":"ordinary","mission_kind":"reply_question","sample_questions":["哪天发货"]}}`
	agent.responses = []string{strings.ReplaceAll(reportV2Body, "下午了，感谢大家陪着。", "第二天上午发货。")}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 1 {
		t.Fatal("ordinary answer rejected", tts.calls)
	}
	for _, r := range agent.requests {
		for _, m := range r.Messages {
			if strings.Contains(m.Content, `"当前北京时间"`) || strings.Contains(m.Content, `"当前时段"`) {
				t.Fatal("clock leaked to ordinary request", m.Content)
			}
		}
	}
}

func TestProgramReportValidityUsesOriginalClockAndContent(t *testing.T) {
	at := time.Date(2026, 11, 4, 10, 35, 20, 0, beijingZone)
	for _, tc := range []struct {
		text         string
		valid        bool
		hour, minute int
	}{
		{"现在十点三十五分。", true, 10, 36}, {"现在十点。", true, 11, 0}, {"上午好。", true, 12, 0}, {"下午好。", false, 0, 0},
	} {
		t.Run(tc.text, func(t *testing.T) {
			until, valid := timeReportValidity(tc.text, at)
			if valid != tc.valid {
				t.Fatal(valid, until)
			}
			if valid && (until.In(beijingZone).Hour() != tc.hour || until.In(beijingZone).Minute() != tc.minute) {
				t.Fatal(until)
			}
		})
	}
}

func TestV2ReportMissingAcousticCutReusesPaidAudio(t *testing.T) {
	w, _, core, agent, tts, session, _ := v2Fixture(t, 9064104)
	now := time.Date(2026, 11, 4, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	core.claimRaw = reportClaim("report-wait-first")
	core.dispatchStatus = 409
	core.dispatchRaw = `{"error":"当前可用窗口内没有真实PCM声学切点，保留已合成声音继续排队"}`
	agent.responses = []string{reportV2Body}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Second)
	core.claimRaw = reportClaim("report-wait-second")
	core.dispatchStatus, core.dispatchRaw = 0, ""
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 1 || core.dispatch["decision_id"] != "report-wait-second" {
		t.Fatal("acoustic wait repurchased valid report", agent.calls, tts.calls, core.dispatch)
	}
}
