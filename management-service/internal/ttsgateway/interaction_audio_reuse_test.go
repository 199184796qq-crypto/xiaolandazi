package ttsgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/ttsscheduler"
)

type reuseFixture struct {
	t                   *testing.T
	srv                 *httptest.Server
	root                string
	paid, before, after atomic.Int64
	status              atomic.Int64
	html                atomic.Bool
	policy              InteractionAudioPolicy
	policyErr           error
	guardErr            error
	wait                <-chan struct{}
}

func newReuseFixture(t *testing.T) *reuseFixture {
	f := &reuseFixture{t: t, root: t.TempDir(), policy: InteractionAudioPolicy{Enabled: true, Retention: 5 * time.Hour}}
	f.status.Store(200)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if r.Header.Get("Range") != "bytes=0-63" {
				t.Error("probe must be bounded")
			}
			w.WriteHeader(int(f.status.Load()))
			if f.html.Load() {
				fmt.Fprint(w, "<html>expired or error page</html>")
			} else {
				fmt.Fprint(w, "RIFF0000WAVEfmt audio data")
			}
			return
		}
		f.paid.Add(1)
		if f.wait != nil {
			<-f.wait
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"request_id": "original-paid", "usage": map[string]any{"input_tokens": 13, "output_tokens": 32}, "output": map[string]any{"audio": map[string]any{"url": f.srv.URL + "/audio?Expires=" + strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10)}}})
	}))
	t.Cleanup(f.srv.Close)
	return f
}
func (f *reuseFixture) gateway() *Gateway {
	g := New(ProviderQwen, "qwen-audio-3.1-tts-flash")
	g.Register(NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: f.srv.URL}), "qwen")
	return g.WithInteractionAudioCache(NewInteractionAudioCache(f.root, "test-realm"))
}
func (f *reuseFixture) ctx(tenant, plan int64) context.Context {
	c := WithSynthesisObserver(f.t.Context(), receiptObserver{before: func(context.Context, SynthesisAttempt) (string, error) {
		f.before.Add(1)
		return "original-ticket", nil
	}, after: func(context.Context, SynthesisReceipt) error { f.after.Add(1); return nil }})
	return WithInteractionAudioReuse(c, tenant, plan, func(context.Context) (InteractionAudioPolicy, error) { return f.policy, f.policyErr }, func(context.Context) error { return f.guardErr })
}
func reuseRequest() SynthesizeRequest {
	return SynthesizeRequest{VoiceID: "goose", Text: "谢谢老乡们的关注，欢迎常来。", Rate: 1}
}
func TestInteractionAudioHistorical46CallsUseFourReadableSources(t *testing.T) {
	f := newReuseFixture(t)
	g := f.gateway()
	for group, count := range []int{30, 12, 2, 2} {
		for i := 0; i < count; i++ {
			r := reuseRequest()
			r.Text = fmt.Sprintf("历史第%d组有效话术。", group)
			a, e := g.SynthesizeURL(f.ctx(14, 4), r)
			if e != nil {
				t.Fatal(e)
			}
			if (i > 0) != a.AudioReused {
				t.Fatal("wrong reuse status", i, a.AudioReused)
			}
			if i > 0 && (a.ProviderRequestID != "" || a.Usage.InputTokens != nil || a.Usage.OutputTokens != nil || a.UsageSource != UsageAudioReuse) {
				t.Fatal("old usage counted twice")
			}
		}
	}
	if f.paid.Load() != 4 || f.before.Load() != 4 || f.after.Load() != 4 {
		t.Fatal("duplicate payment", f.paid.Load(), f.before.Load(), f.after.Load())
	}
}
func TestInteractionAudioCompleteIdentityIsolation(t *testing.T) {
	cases := []struct {
		name         string
		tenant, plan int64
		mutate       func(*SynthesizeRequest)
	}{
		{"tenant", 15, 4, func(*SynthesizeRequest) {}}, {"plan", 14, 5, func(*SynthesizeRequest) {}},
		{"text", 14, 4, func(r *SynthesizeRequest) { r.Text += "新" }}, {"voice", 14, 4, func(r *SynthesizeRequest) { r.VoiceID = "other" }},
		{"rate", 14, 4, func(r *SynthesizeRequest) { r.Rate = 1.1 }}, {"instruction", 14, 4, func(r *SynthesizeRequest) { r.Instruction = "自然" }},
		{"model", 14, 4, func(r *SynthesizeRequest) { r.Model = "qwen-audio-3.0-tts-plus" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newReuseFixture(t)
			g := f.gateway()
			_, e := g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
			if e != nil {
				t.Fatal(e)
			}
			r := reuseRequest()
			c.mutate(&r)
			_, e = g.SynthesizeURL(f.ctx(c.tenant, c.plan), r)
			if e != nil || f.paid.Load() != 2 {
				t.Fatal("identity crossed", e, f.paid.Load())
			}
		})
	}
}
func TestInteractionAudioPolicyAndReadability(t *testing.T) {
	for _, kind := range []string{"disabled", "policy-error", "retention-shortened", "expired", "404", "403", "503", "html"} {
		t.Run(kind, func(t *testing.T) {
			f := newReuseFixture(t)
			g := f.gateway()
			_, e := g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "disabled":
				f.policy.Enabled = false
			case "policy-error":
				f.policyErr = errors.New("unavailable")
			case "retention-shortened":
				f.policy.Retention = time.Hour
				g.interactionAudioCache.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
			case "expired":
				g.interactionAudioCache.now = func() time.Time { return time.Now().Add(6 * time.Hour) }
			case "404":
				f.status.Store(404)
			case "403":
				f.status.Store(403)
			case "503":
				f.status.Store(503)
			case "html":
				f.html.Store(true)
			}
			a, e := g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
			if kind == "policy-error" {
				if e == nil || f.paid.Load() != 1 {
					t.Fatal("policy failure became permission")
				}
				return
			}
			if e != nil || a.AudioReused || f.paid.Load() != 2 {
				t.Fatal("unavailable/disabled source reused", e, f.paid.Load())
			}
		})
	}
}
func TestInteractionAudioRestartAndConcurrentGatewaysBuyOnce(t *testing.T) {
	f := newReuseFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := f.gateway().SynthesizeURL(f.ctx(14, 4), reuseRequest())
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if f.paid.Load() != 1 || f.before.Load() != 1 {
		t.Fatal("concurrent duplicate purchase", f.paid.Load())
	}
	_, e := f.gateway().SynthesizeURL(f.ctx(14, 4), reuseRequest())
	if e != nil || f.paid.Load() != 1 {
		t.Fatal("restart missed durable metadata", e)
	}
}
func TestInteractionAudioMappedAccountVoiceAndCredentialIsolation(t *testing.T) {
	f := newReuseFixture(t)
	g := f.gateway()
	account, key, voice := "b", "key-b", "mapped-b"
	g.scheduler = schedulerFixture(func(context.Context, ttsscheduler.Work) (ttsscheduler.Permit, error) {
		return ttsscheduler.Permit{Credential: ttsscheduler.Credential{AccountID: account, APIKey: key, VoiceID: voice}, Finish: func(string) {}}, nil
	})
	call := func(want int64) {
		t.Helper()
		_, e := g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
		if e != nil || f.paid.Load() != want {
			t.Fatal("account/voice routing", e, f.paid.Load(), want)
		}
	}
	call(1)
	call(1)
	voice = "new-map"
	call(2)
	key = "rotated"
	call(3)
	account = "other-account"
	call(4)
	entries, _ := filepath.Glob(filepath.Join(g.interactionAudioCache.root, "*.json"))
	for _, p := range entries {
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), "key-b") || strings.Contains(string(raw), "rotated") {
			t.Fatal("credential persisted")
		}
	}
}
func TestInteractionAudioWaitRechecksControlsAndCancellation(t *testing.T) {
	f := newReuseFixture(t)
	g := f.gateway()
	_, e := g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
	if e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(g.interactionAudioCache.root, "*.json"))
	key := strings.TrimSuffix(filepath.Base(files[0]), ".json")
	release, e := sharedfilelock.Lock(t.Context(), filepath.Join(g.interactionAudioCache.root, "locks", key+".lock"))
	if e != nil {
		t.Fatal(e)
	}
	c, cancel := context.WithTimeout(f.ctx(14, 4), 20*time.Millisecond)
	defer cancel()
	_, e = g.SynthesizeURL(c, reuseRequest())
	release()
	if e == nil || f.paid.Load() != 1 {
		t.Fatal("cancelled waiter paid")
	}
	f.guardErr = errors.New("interaction disabled")
	_, e = g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
	if e == nil || f.paid.Load() != 1 {
		t.Fatal("disabled interaction used cache")
	}
}
func TestInteractionAudioExpiryDoesNotRenew(t *testing.T) {
	now := time.Now()
	expires := now.Add(time.Hour).Truncate(time.Second)
	got := interactionAudioExpiry("https://audio.invalid/file?Expires="+strconv.FormatInt(expires.Unix(), 10), now.Add(5*time.Hour), now)
	if !got.Equal(expires) {
		t.Fatal(got, expires)
	}
	got = interactionAudioExpiry("https://audio.invalid/file?signature=unknown", now.Add(5*time.Hour), now)
	if !got.Equal(now.Add(2 * time.Minute)) {
		t.Fatal("unknown signature lifetime extended")
	}
	f := newReuseFixture(t)
	g := f.gateway()
	_, _ = g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
	files, _ := filepath.Glob(filepath.Join(g.interactionAudioCache.root, "*.json"))
	before, _ := os.ReadFile(files[0])
	_, _ = g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
	after, _ := os.ReadFile(files[0])
	if string(before) != string(after) {
		t.Fatal("hit renewed original entry")
	}
}
func TestInteractionAudioTwoProcesses(t *testing.T) {
	if os.Getenv("INTERACTION_REUSE_CHILD") == "1" {
		g := New(ProviderQwen, "qwen-audio-3.1-tts-flash")
		g.Register(NewQwenProvider(QwenConfig{APIKey: "fixture", TTSBaseURL: os.Getenv("INTERACTION_REUSE_SERVER")}))
		g.WithInteractionAudioCache(NewInteractionAudioCache(os.Getenv("INTERACTION_REUSE_ROOT"), "test-realm"))
		c := WithInteractionAudioReuse(t.Context(), 14, 4, func(context.Context) (InteractionAudioPolicy, error) {
			return InteractionAudioPolicy{true, 5 * time.Hour}, nil
		}, nil)
		if _, e := g.SynthesizeURL(c, reuseRequest()); e != nil {
			t.Fatal(e)
		}
		return
	}
	f := newReuseFixture(t)
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		c := exec.Command(os.Args[0], "-test.run=^TestInteractionAudioTwoProcesses$")
		c.Env = append(os.Environ(), "INTERACTION_REUSE_CHILD=1", "INTERACTION_REUSE_SERVER="+f.srv.URL, "INTERACTION_REUSE_ROOT="+f.root)
		commands[i] = c
		if e := c.Start(); e != nil {
			t.Fatal(e)
		}
	}
	for _, c := range commands {
		if e := c.Wait(); e != nil {
			t.Fatal(e)
		}
	}
	if f.paid.Load() != 1 {
		t.Fatal("cross-process duplicate purchase", f.paid.Load())
	}
}

func TestInteractionAudioCorruptMetadataAndExpiryCleanup(t *testing.T) {
	f := newReuseFixture(t)
	g := f.gateway()
	_, e := g.SynthesizeURL(f.ctx(14, 4), reuseRequest())
	if e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(g.interactionAudioCache.root, "*.json"))
	path := files[0]
	if e = os.WriteFile(path, []byte("{broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = g.SynthesizeURL(f.ctx(14, 4), reuseRequest()); e != nil || f.paid.Load() != 2 {
		t.Fatal("corrupt recovery", e, f.paid.Load())
	}
	preserved, _ := filepath.Glob(path + ".unreadable-*")
	if len(preserved) != 1 {
		t.Fatal("lost corrupt evidence")
	}
	g.interactionAudioCache.prune(t.Context())
	if _, e = os.Stat(path); e != nil {
		t.Fatal("removed valid metadata")
	}
	g.interactionAudioCache.now = func() time.Time { return time.Now().Add(6 * time.Hour) }
	g.interactionAudioCache.prune(t.Context())
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("expired metadata retained", e)
	}
	if _, e = os.Stat(preserved[0]); e != nil {
		t.Fatal("cleanup removed evidence")
	}
}

func TestInteractionLatePaidResultRemainsReusableAfterOriginalDeadline(t *testing.T) {
	f := newReuseFixture(t)
	g := f.gateway()
	ctx, cancel := context.WithTimeout(f.ctx(14, 4), 20*time.Millisecond)
	defer cancel()
	f.wait = ctx.Done()
	r := reuseRequest()
	r.DrainOnCancel = true
	first, e := g.SynthesizeURL(ctx, r)
	if e != nil || first.AudioURL == "" || ctx.Err() == nil || f.after.Load() != 1 {
		t.Fatal("lost late paid response", e)
	}
	second, e := g.SynthesizeURL(f.ctx(14, 4), r)
	if e != nil || !second.AudioReused || f.paid.Load() != 1 || f.before.Load() != 1 || f.after.Load() != 1 {
		t.Fatal("late paid source bought again", e, f.paid.Load())
	}
}
