package ttsgateway

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/ttsscheduler"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVolcengineScheduledCompleteAudioAndUsage(t *testing.T) {
	var audio []byte
	finished := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "selected-account-key" || r.Header.Get("X-Api-Resource-Id") != VolcengineICL2 || r.Header.Get("Authorization") != "" || r.Header.Get("X-Api-Request-Id") == "" || r.Header.Get("X-Control-Require-Usage-Tokens-Return") != "text_words" {
			t.Error("wrong provider credentials or headers")
		}
		var body struct {
			Params struct {
				Text, Speaker, Model, Additions string
				Audio                           struct {
					Format string
					Rate   int `json:"speech_rate"`
					Sample int `json:"sample_rate"`
				} `json:"audio_params"`
			} `json:"req_params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.Params.Speaker != "S_mapped" || body.Params.Text != "测试。" || body.Params.Audio.Rate != 1 || body.Params.Audio.Sample != 24000 || body.Params.Audio.Format != "pcm" {
			t.Error("wrong payload", body)
		}
		var additions map[string]bool
		if err := json.Unmarshal([]byte(body.Params.Additions), &additions); err != nil || !additions["tone_fidelity"] || body.Params.Model != "seed-tts-2.0-standard" {
			t.Error("reference-tone mode missing from actual short request", body.Params.Additions)
		}
		w.Header().Set("X-Tt-Logid", "safe-log-id")
		fmt.Fprintf(w, `{"code":0,"data":%q}`+"\n"+`{"code":0,"data":%q}`+"\n"+`{"code":20000000,"usage":{"text_words":3}}`, base64.StdEncoding.EncodeToString([]byte{1, 2}), base64.StdEncoding.EncodeToString([]byte{3, 4}))
	}))
	defer srv.Close()
	g := New(ProviderQwen, DefaultQwenTTSModel)
	g.Register(NewVolcengineProvider(VolcengineConfig{Endpoint: srv.URL, Client: srv.Client(), Publish: func(_ context.Context, raw []byte) (string, error) {
		audio = raw
		return "http://audio.invalid/fixture.wav", nil
	}}))
	g.scheduler = schedulerFixture(func(_ context.Context, w ttsscheduler.Work) (ttsscheduler.Permit, error) {
		if w.Provider != ProviderVolcengine || w.VoiceID != "S_source" || w.RoomID != 29 {
			t.Error("incorrect logical routing", w)
		}
		return ttsscheduler.Permit{Credential: ttsscheduler.Credential{AccountID: "volc-account", APIKey: "selected-account-key", VoiceID: "S_mapped"}, Finish: func(s string) { finished = s }}, nil
	})
	out, err := g.SynthesizeURL(WithQueueScope(t.Context(), ttsscheduler.Scope{RoomID: 29}), SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_source", Text: "测试。", Rate: 1.01})
	if err != nil {
		t.Fatal(err)
	}
	if finished != "completed" || out.Provider != ProviderVolcengine || out.VoiceID != "S_mapped" || out.ProviderRequestID != "safe-log-id" || out.Usage.Characters == nil || *out.Usage.Characters != 3 || out.Usage.TotalTokens != nil || out.UsageSource != UsageSupplierResponse {
		t.Fatal("bad synthesis receipt", out, finished)
	}
	if len(audio) != 48 || string(audio[:4]) != "RIFF" || binary.LittleEndian.Uint32(audio[24:]) != 24000 || binary.LittleEndian.Uint32(audio[28:]) != 48000 || string(audio[44:]) != string([]byte{1, 2, 3, 4}) {
		t.Fatal("PCM concatenation/WAV framing invalid")
	}
}

func TestVolcengineRejectsPartialAndInvalidStreams(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"truncated", `{"code":0,"data":"AQI="}`, 200},
		{"no audio", `{"code":20000000}`, 200},
		{"bad base64", `{"code":0,"data":"!"}`, 200},
		{"odd pcm", `{"code":0,"data":"AQ=="}{"code":20000000}`, 200},
		{"supplier error", `{"code":45000000,"message":"secret-key-must-not-escape"}`, 200},
		{"missing code", `{"data":"AQI="}`, 200},
		{"http error", `secret-key-must-not-escape`, 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			published := false
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			p := NewVolcengineProvider(VolcengineConfig{APIKey: "fixture", Endpoint: srv.URL, Publish: func(context.Context, []byte) (string, error) { published = true; return "unexpected", nil }})
			_, err := p.SynthesizeURL(t.Context(), SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_ok", Text: "测试", Rate: 1})
			if err == nil || published || calls != 1 || strings.Contains(err.Error(), "secret-key-must-not-escape") || DefinitelyNotSent(err) {
				t.Fatal("partial audio accepted, supplier failure retried, or error leaked", err)
			}
		})
	}
}

func TestVolcengineRoutingAndQueueCancellation(t *testing.T) {
	for _, provider := range []string{ProviderQwen, ProviderTencent, ProviderCosyVoice} {
		g := New(ProviderQwen, DefaultQwenTTSModel)
		g.Register(NewVolcengineProvider(VolcengineConfig{}))
		g.Register(&routingProvider{name: provider})
		if _, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Provider: provider, Model: VolcengineICL2, VoiceID: "S_ok", Text: "测试"}); err == nil {
			t.Fatal("cross-vendor routing accepted")
		}
	}
	g := New(ProviderVolcengine, VolcengineICL2)
	g.Register(NewVolcengineProvider(VolcengineConfig{}))
	g.scheduler = schedulerFixture(func(ctx context.Context, _ ttsscheduler.Work) (ttsscheduler.Permit, error) {
		<-ctx.Done()
		return ttsscheduler.Permit{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := g.SynthesizeURL(ctx, SynthesizeRequest{VoiceID: "S_ok", Text: "测试"})
	if !errors.Is(err, context.DeadlineExceeded) || !DefinitelyNotSent(err) {
		t.Fatal("queued cancellation not honored", err)
	}
}

type routingProvider struct{ name string }

func (p *routingProvider) Name() string { return p.name }
func (p *routingProvider) SynthesizeURL(context.Context, SynthesizeRequest) (SynthesizeResponse, error) {
	return SynthesizeResponse{}, errors.New("unexpected provider call")
}
func (p *routingProvider) CloneVoice(context.Context, CloneRequest) (CloneResponse, error) {
	return CloneResponse{}, errors.New("unexpected clone")
}

func TestVolcengineDoesNotFollowKeyRedirect(t *testing.T) {
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	p := NewVolcengineProvider(VolcengineConfig{APIKey: "private-key", Endpoint: srv.URL})
	_, err := p.SynthesizeURL(t.Context(), SynthesizeRequest{Model: VolcengineICL2, VoiceID: "S_ok", Text: "测试"})
	if err == nil || called {
		t.Fatal("speech credentials could be forwarded")
	}
}

func TestVolcengineCloneNeverFallsBackToAliyun(t *testing.T) {
	g := New(ProviderQwen, DefaultQwenTTSModel)
	if _, err := g.CloneVoice(t.Context(), CloneRequest{TargetModel: VolcengineICL2, AudioURL: "https://sample.invalid/audio.wav"}); err == nil || strings.Contains(err.Error(), "unknown tts provider") {
		t.Fatal("clone routed before rejecting unsupported operation", err)
	}
}
