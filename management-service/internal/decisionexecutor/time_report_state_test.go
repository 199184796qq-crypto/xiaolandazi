package decisionexecutor

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func reportClaim(id string) string {
	return `{"claimed":true,"item":{"id":"` + id + `","topic":"INTERACTION:TIME_REPORT:run:1","mission_kind":"time_report","scene":"time_report","sample_questions":["自然提一句时间"]}}`
}
func reportCalls(agent *fakeAgent) int {
	n := 0
	for _, r := range agent.requests {
		if r.Stage == "interaction_time_report" {
			n++
		}
	}
	return n
}

func TestTimeReportTTSFailureReusesTextUntilPeriodExpires(t *testing.T) {
	w, _, core, agent, tts, session := preparedTestWorker()
	core.claimRaw = reportClaim("report-1")
	core.dispatchStatus = 0
	now := time.Date(2026, 10, 31, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	agent.responses = []string{"下午了，大家歇一歇。", "晚上了，谢谢大家陪伴。"}
	tts.err = errors.New("temporary synthesis failure")
	if err := w.processRoom(context.Background(), session); err == nil {
		t.Fatal("TTS failure lost")
	}
	tts.err = nil
	now = now.Add(3 * time.Minute)
	core.claimRaw = reportClaim("report-2")
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if reportCalls(agent) != 1 || tts.calls != 2 {
		t.Fatalf("still-valid text regenerated: model=%d tts=%d", reportCalls(agent), tts.calls)
	}
	// A later retry of the same quota reuses audio, but is bound to the NEW claim.
	now = now.Add(3 * time.Minute)
	core.claimRaw = reportClaim("report-3")
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if reportCalls(agent) != 1 || tts.calls != 2 || core.dispatch["decision_id"] != "report-3" {
		t.Fatalf("audio reuse lost execution identity: %v", core.dispatch)
	}
	if until, ok := core.dispatch["time_report_valid_until"].(time.Time); !ok || until.In(beijingZone).Hour() != 18 {
		t.Fatal("original time validity missing", core.dispatch)
	}
	now = time.Date(2026, 10, 31, 18, 1, 0, 0, beijingZone)
	core.claimRaw = reportClaim("report-4")
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if reportCalls(agent) != 2 || tts.calls != 3 || !strings.Contains(tts.last.Text, "晚上") {
		t.Fatal("expired afternoon not regenerated", reportCalls(agent), tts.calls, tts.last.Text)
	}
	for _, req := range agent.requests {
		if req.Stage == "interaction_time_report" && !strings.Contains(req.Messages[0].Content, "完整有效P0") {
			t.Fatal("report prompt omitted full P0")
		}
	}
}

type signedReportTTS struct{ calls int }

func (a *signedReportTTS) SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	a.calls++
	return ttsgateway.SynthesizeResponse{AudioURL: "https://audio.example/report.wav?unknown-signature=value"}, nil
}

func TestTimeReportAudioLifetimeIsNotRenewedByCutRetry(t *testing.T) {
	w, _, core, agent, _, session := preparedTestWorker()
	core.claimRaw = reportClaim("same-report")
	now := time.Date(2026, 10, 31, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	agent.responses = []string{"下午了，谢谢陪伴。"}
	tts := &signedReportTTS{}
	w.tts = tts
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	until := now.Add(preparedInteractionTTL)
	now = now.Add(time.Minute)
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	for _, prepared := range w.prepared {
		if !prepared.ExpiresAt.Equal(until) {
			t.Fatal("cut retry extended original audio expiry", prepared.ExpiresAt, until)
		}
	}
	if tts.calls != 1 || reportCalls(agent) != 1 {
		t.Fatal("valid cut retry regenerated audio/text")
	}
	now = until
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 2 || reportCalls(agent) != 1 {
		t.Fatal("expired audio should renew TTS only", tts.calls, reportCalls(agent))
	}
}

type advancingReportTTS struct {
	now   *time.Time
	calls int
}

func (a *advancingReportTTS) SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	a.calls++
	*a.now = a.now.Add(10 * time.Second)
	return ttsgateway.SynthesizeResponse{AudioURL: "http://audio.local/report.wav"}, nil
}

func TestTimeReportCannotDispatchAfterTTSCrossesTimeBoundary(t *testing.T) {
	w, _, core, agent, _, session := preparedTestWorker()
	core.claimRaw = reportClaim("report-boundary")
	core.dispatchStatus = 0
	now := time.Date(2026, 10, 31, 17, 59, 55, 0, beijingZone)
	w.now = func() time.Time { return now }
	agent.responses = []string{"现在17:59了，感谢陪伴。"}
	w.tts = &advancingReportTTS{now: &now}
	if err := w.processRoom(context.Background(), session); !errors.Is(err, errTimeReportStale) {
		t.Fatal("post-TTS time expiry not rejected", err)
	}
	if core.dispatches != 0 {
		t.Fatal("outdated audio reached Core")
	}
}

func TestTimeReportScopeChangeInvalidatesAndAudioExpiryDoesNotRegenerateText(t *testing.T) {
	w, store, core, agent, tts, session := preparedTestWorker()
	core.claimRaw = reportClaim("report-1")
	core.dispatchStatus = 0
	now := time.Date(2026, 10, 31, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	agent.responses = []string{"下午了，谢谢陪伴。", "下午好，大家歇一歇。"}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	for k, v := range w.prepared {
		v.ExpiresAt = now.Add(time.Minute)
		w.prepared[k] = v
	}
	now = now.Add(2 * time.Minute)
	core.claimRaw = reportClaim("report-2")
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if reportCalls(agent) != 1 || tts.calls != 2 {
		t.Fatal("expired audio unnecessarily regenerated valid text")
	}
	store.publishedVersion.VoiceIdentity.VoiceID = "new-voice"
	core.claimRaw = reportClaim("report-3")
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if reportCalls(agent) != 2 || tts.calls != 3 {
		t.Fatal("changed voice reused prior prepared content")
	}
	w.prunePreparedInteractions(nil)
	if len(w.timeReportDrafts) != 0 || len(w.prepared) != 0 {
		t.Fatal("ended session kept report execution cache")
	}
}

func TestTimeReportAudioExpiryKeepsOriginalDeadline(t *testing.T) {
	now := time.Now()
	valid := now.Add(time.Hour)
	url := "https://audio.example/report.wav?Expires=invalid" // malformed expiry gets a bounded audio cache only
	if got := timeReportAudioExpiry(url, valid, now); !got.Equal(now.Add(preparedInteractionTTL)) {
		t.Fatal(got)
	}
	if got := timeReportAudioExpiry("https://audio.example/report.wav", valid, now); !got.Equal(valid) {
		t.Fatal(got)
	}
}

func TestTimeReportPaidIdentityRetriesAndRegeneration(t *testing.T) {
	w, _, core, agent, tts, session := preparedTestWorker()
	core.claimRaw = reportClaim("same-claim")
	core.dispatchStatus = 0
	now := time.Date(2026, 10, 31, 14, 20, 0, 0, beijingZone)
	w.now = func() time.Time { return now }
	agent.responses = []string{"下午了，感谢陪伴。", "晚上了，感谢陪伴。"}
	var audioKeys, modelKeys []string
	w.SetPaidRequestAdapters(func(ctx context.Context, _ model.LiveRuntimeSession, _ string, _ int64, key string) context.Context {
		audioKeys = append(audioKeys, key)
		return ctx
	}, func(ctx context.Context, _ model.LiveRuntimeSession, req agentgateway.Request, _ int64, key string, call func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
		if req.Stage == "interaction_time_report" {
			modelKeys = append(modelKeys, key)
		}
		return call(ctx, req)
	})
	tts.err = errors.New("unknown TTS receipt")
	if err := w.processRoom(context.Background(), session); err == nil {
		t.Fatal("missing TTS failure")
	}
	now = now.Add(time.Minute)
	tts.err = nil
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if len(modelKeys) != 1 || len(audioKeys) != 2 || audioKeys[0] != audioKeys[1] {
		t.Fatal("valid-content retry changed paid operation identity", modelKeys, audioKeys)
	}
	// A known expired audio asset renews ONLY TTS, with a fresh operation key.
	for k, v := range w.prepared {
		v.ExpiresAt = now
		w.prepared[k] = v
	}
	for k, v := range w.timeReportDrafts {
		v.AudioUntil = now
		w.timeReportDrafts[k] = v
	}
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if len(modelKeys) != 1 || len(audioKeys) != 3 || audioKeys[2] == audioKeys[1] {
		t.Fatal("expired asset reused a completed/expired TTS receipt", modelKeys, audioKeys)
	}
	now = time.Date(2026, 10, 31, 18, 1, 0, 0, beijingZone)
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if len(modelKeys) != 2 || modelKeys[0] == modelKeys[1] || !strings.Contains(tts.last.Text, "晚上") {
		t.Fatal("expired text reused old paid receipt", modelKeys, tts.last.Text)
	}
}
