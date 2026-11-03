package ttsgateway

import (
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOldCloudRecoveryWaitIsBoundedWithoutResubmittingInsideProvider(t *testing.T) {
	r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_a", Rate: 1, Text: "原文", LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json"), RecoveryWait: 15 * time.Millisecond}}
	if err := saveLongTextState(r.LongText.StatePath, longTextState{Version: 1, InputSHA: longTextInputSHA(r), CredentialSHA: digestSynthesisText("key"), VoiceID: r.VoiceID, TaskID: "old"}); err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int32
	p := NewVolcengineProvider(VolcengineConfig{APIKey: "key", LongPollInterval: time.Millisecond, Client: &http.Client{Transport: longTestTransport(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, "/submit") {
			t.Error("provider itself resubmitted without caller retaining evidence")
		}
		queries.Add(1)
		return longTestResponse(`{"code":20000000,"data":{"task_status":1}}`), nil
	})}})
	start := time.Now()
	_, err := p.SynthesizeURL(t.Context(), r)
	if err == nil || queries.Load() == 0 || time.Since(start) > time.Second {
		t.Fatal("old task recovery not bounded", err, queries.Load(), time.Since(start))
	}
}
