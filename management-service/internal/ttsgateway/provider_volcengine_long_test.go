package ttsgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/ttsscheduler"
)

func TestVolcengineFidelityDoesNotAdoptOrResubmitLegacyTask(t *testing.T) {
	r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_a", Rate: 1, Text: "原文。", LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json")}}
	legacyInput, _ := json.Marshal([]any{VolcengineICL2, r.VoiceID, r.Text, r.Rate, "pcm-24000-v1"})
	sum := sha256.Sum256(legacyInput)
	wav := tencentPCMToWAV([]byte{1, 2, 3, 4})
	audioSum := sha256.Sum256(wav)
	state := longTextState{Version: 1, InputSHA: hex.EncodeToString(sum[:]), CredentialSHA: digestSynthesisText("fixture"), VoiceID: r.VoiceID, TaskID: "legacy-paid-task", Complete: true, AudioSHA: hex.EncodeToString(audioSum[:])}
	if err := os.WriteFile(r.LongText.StatePath+".wav", wav, 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveLongTextState(r.LongText.StatePath, state); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadLongTextResult(r); ok {
		t.Fatal("legacy task accepted as restored-reference audio")
	}
	var sent atomic.Int32
	p := NewVolcengineProvider(VolcengineConfig{APIKey: "fixture", Client: &http.Client{Transport: longTestTransport(func(*http.Request) (*http.Response, error) {
		sent.Add(1)
		return nil, errors.New("must retain the legacy task")
	})}})
	if _, err := p.SynthesizeURL(t.Context(), r); err == nil || sent.Load() != 0 {
		t.Fatal("legacy checkpoint overwritten or charged again", err)
	}
	retained, err := readLongTextState(r.LongText.StatePath)
	if err != nil || retained.InputSHA != state.InputSHA || retained.TaskID != state.TaskID {
		t.Fatal("legacy checkpoint changed", err)
	}
}

type longTestTransport func(*http.Request) (*http.Response, error)

func (f longTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func longTestResponse(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)}
}

func TestVolcengineLongSubmitOnceResumeAndNativeUsage(t *testing.T) {
	var submits, queries, downloads, fees atomic.Int32
	var id string
	failDownload := true
	client := &http.Client{Transport: longTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			downloads.Add(1)
			if r.Header.Get("X-Api-Key") != "" || r.Header.Get("Authorization") != "" {
				t.Error("credential forwarded to audio URL")
			}
			if failDownload {
				return nil, errors.New("synthetic download outage")
			}
			return longTestResponse(strings.Repeat("\x01\x02", 24000)), nil
		}
		if r.Header.Get("X-Api-Key") != "pool-key" || r.Header.Get("X-Api-Resource-Id") != VolcengineICL2 {
			t.Error("wrong selected account")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if strings.HasSuffix(r.URL.Path, "/submit") {
			submits.Add(1)
			id, _ = body["unique_id"].(string)
			p := body["req_params"].(map[string]any)
			a := p["audio_params"].(map[string]any)
			if p["speaker"] != "S_mapped" || p["text"] != "测试。" || p["model"] != "seed-tts-2.0-standard" || a["enable_timestamp"] != true || a["format"] != "pcm" {
				t.Error("bad long payload")
			}
			additions, ok := p["additions"].(string)
			var flags map[string]bool
			if !ok || json.Unmarshal([]byte(additions), &flags) != nil || !flags["tone_fidelity"] {
				t.Error("reference-tone mode missing from actual long submit")
			}
			return longTestResponse(`{"code":20000000,"data":{"task_id":"` + id + `"}}`), nil
		}
		queries.Add(1)
		if body["task_id"] != id {
			t.Error("changed task on retry")
		}
		return longTestResponse(`{"code":20000000,"data":{"task_status":2,"audio_url":"https://audio.bytespeech.com/file?signature=private","synthesize_text_length":3,"sentences":[{"text":"测试。","startTime":0,"endTime":1,"words":[{"word":"测试。","startTime":0,"endTime":1}]}]}}`), nil
	})}
	g := New(ProviderVolcengine, VolcengineICL2)
	g.Register(NewVolcengineProvider(VolcengineConfig{Client: client, LongEndpoint: "https://fixture.invalid/tts", LongPollInterval: time.Millisecond}))
	var admissions atomic.Int32
	g.scheduler = schedulerFixture(func(_ context.Context, w ttsscheduler.Work) (ttsscheduler.Permit, error) {
		if admissions.Add(1) > 1 && w.RequiredAccountID != "fire-owner" {
			t.Error("recovery not pinned to cloud owner")
		}
		return ttsscheduler.Permit{Credential: ttsscheduler.Credential{AccountID: "fire-owner", APIKey: "pool-key", VoiceID: "S_mapped"}, Finish: func(string) {}}, nil
	})
	ctx := WithSynthesisObserver(t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) { fees.Add(1); return "original-ticket", nil }, after: func(context.Context, SynthesisReceipt) error { return nil }})
	r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_source", Text: "测试。", Rate: 1, LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json"), SourceVoiceID: "S_source"}}
	if _, err := g.SynthesizeURL(ctx, r); err == nil {
		t.Fatal("download outage hidden")
	}
	failDownload = false
	out, err := g.SynthesizeURL(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if submits.Load() != 1 || fees.Load() != 1 || queries.Load() != 2 || downloads.Load() != 2 || !out.AccountingPending || out.VoiceID != "S_mapped" || out.Usage.Characters == nil || *out.Usage.Characters != 3 || len(out.Subtitles) != 1 {
		t.Fatal("paid task repeated or receipt lost", submits.Load(), fees.Load(), out)
	}
	if cached, ok := LoadLongTextResult(r); !ok || cached.AudioFile != out.AudioFile {
		t.Fatal("source/mapped cache identity broken")
	}
	raw, err := os.ReadFile(out.AudioFile)
	if err != nil || len(raw) != 48044 || string(raw[:4]) != "RIFF" {
		t.Fatal("bad complete WAV", err)
	}
	state, err := os.ReadFile(r.LongText.StatePath)
	if err != nil || strings.Contains(string(state), "pool-key") || strings.Contains(string(state), "signature=") {
		t.Fatal("sensitive response persisted")
	}
	r.Text = "另一段。"
	if _, ok := LoadLongTextResult(r); ok {
		t.Fatal("wrong text hit paid cache")
	}
}

func TestVolcengineLongDrainsAcceptedTaskAfterStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var submits, queries atomic.Int32
	client := &http.Client{Transport: longTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return longTestResponse(strings.Repeat("\x00\x01", 100)), nil
		}
		if strings.HasSuffix(r.URL.Path, "/submit") {
			submits.Add(1)
			cancel()
			return longTestResponse(`{"code":20000000,"data":{}}`), nil
		}
		queries.Add(1)
		if err := r.Context().Err(); err != nil {
			t.Error("accepted cloud task prematurely abandoned", err)
		}
		return longTestResponse(`{"code":20000000,"data":{"task_status":2,"audio_url":"https://audio.bytespeech.com/file","synthesize_text_length":1,"sentences":[{"text":"好","startTime":0,"endTime":0.004}]}}`), nil
	})}
	p := NewVolcengineProvider(VolcengineConfig{APIKey: "fixture-key", Client: client})
	r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_a", Rate: 1, Text: "好", LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json")}}
	out, err := p.SynthesizeURL(ctx, r)
	if err != nil || out.AudioFile == "" || submits.Load() != 1 || queries.Load() != 1 {
		t.Fatal("accepted paid work lost", err, out)
	}
	if _, ok := LoadLongTextResult(r); !ok {
		t.Fatal("stopped session lost future reusable paid result")
	}
	var posts atomic.Int32
	p = NewVolcengineProvider(VolcengineConfig{APIKey: "fixture-key", Client: &http.Client{Transport: longTestTransport(func(*http.Request) (*http.Response, error) { posts.Add(1); return nil, errors.New("must not send") })}})
	r.LongText = &LongTextJob{StatePath: filepath.Join(t.TempDir(), "not-started.json")}
	if _, err = p.SynthesizeURL(ctx, r); !DefinitelyNotSent(err) || posts.Load() != 0 {
		t.Fatal("stopped before acceptance still sent", err)
	}
}

func TestVolcengineLongRejectsForeignAudioAndWrongOwner(t *testing.T) {
	p := NewVolcengineProvider(VolcengineConfig{APIKey: "fixture-key"}).(*volcengineProvider)
	for _, url := range []string{"http://audio.bytespeech.com/a", "https://audio.bytespeech.com.evil.test/a", "https://127.0.0.1/a", "https://user@audio.bytespeech.com/a"} {
		if err := p.downloadLongPCM(t.Context(), http.DefaultClient, url, filepath.Join(t.TempDir(), "a.wav")); err == nil {
			t.Fatal("unsafe destination accepted", url)
		}
	}
	r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_a", Rate: 1, Text: "原文", LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json")}}
	if err := saveLongTextState(r.LongText.StatePath, longTextState{Version: 1, InputSHA: longTextInputSHA(r), CredentialSHA: "other-owner", VoiceID: "S_a", TaskID: "fixed-task"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SynthesizeURL(t.Context(), r); !DefinitelyNotSent(err) {
		t.Fatal("task ownership ignored", err)
	}
}

func TestVolcengineLongLostSubmitResponseOnlyQueries(t *testing.T) {
	var submits, queries atomic.Int32
	client := &http.Client{Transport: longTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return longTestResponse(strings.Repeat("\x00\x01", 100)), nil
		}
		if strings.HasSuffix(r.URL.Path, "/submit") {
			submits.Add(1)
			return nil, errors.New("response lost after cloud accepted")
		}
		queries.Add(1)
		return longTestResponse(`{"code":20000000,"data":{"task_status":2,"audio_url":"https://audio.bytespeech.com/file","sentences":[{"text":"好","startTime":0,"endTime":0.004}]}}`), nil
	})}
	p := NewVolcengineProvider(VolcengineConfig{APIKey: "fixture-key", Client: client, LongPollInterval: time.Millisecond})
	r := SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_a", Rate: 1, Text: "好", LongText: &LongTextJob{StatePath: filepath.Join(t.TempDir(), "job.json")}}
	if out, err := p.SynthesizeURL(t.Context(), r); err != nil || out.AudioFile == "" || submits.Load() != 1 || queries.Load() != 1 {
		t.Fatal("uncertain submit repeated or abandoned", err, submits.Load(), queries.Load())
	}
}
