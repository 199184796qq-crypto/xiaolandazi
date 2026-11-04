package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/ttsgateway"
)

func TestGenerationBoundaryFullVoiceIdentity(t *testing.T) {
	for _, field := range []string{"binding", "profile", "provider", "pool", "source", "style", "emotion"} {
		t.Run(field, func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
			if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1"); err != nil {
				t.Fatal(err)
			}
			changed := v
			switch field {
			case "binding":
				changed.VoiceIdentity.BindingID++
			case "profile":
				changed.VoiceIdentity.ProfileID++
			case "provider":
				changed.VoiceIdentity.Provider = "different-provider"
			case "pool":
				changed.VoiceIdentity.TTSPool = "different-pool"
			case "source":
				changed.VoiceIdentity.Source = "different-source"
			case "style":
				changed.VoiceIdentity.Style = map[string]any{"strength": 2}
			case "emotion":
				changed.VoiceIdentity.Emotion = "different-emotion"
			}
			if play, err := pendingScriptGenerationPlay(root, 14, 29, 99, changed); err != nil || play != nil {
				t.Fatal("changed identity inherited purchase", play, err)
			}
		})
	}
}

func TestGenerationBoundaryRejectsMalformedAndForeignScope(t *testing.T) {
	for _, bad := range []string{"long-99-+1", "long-99-01", "long-99-0", "long-99--1", "long-98-1", "long-99-1-raw", "long-99-9223372036854775808", "foreign-99-1", "foreign-tenant", "zero-plan"} {
		t.Run(bad, func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			instance := bad
			if bad == "foreign-tenant" {
				v.TenantID = 15
				instance = "long-99-1"
			}
			if bad == "zero-plan" {
				v.PlanID = 0
				instance = "long-99-1"
			}
			if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, instance); err != nil {
				t.Fatal(err)
			}
			if play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); play != nil {
				t.Fatal("invalid evidence granted purchase", play)
			}
		})
	}
}

func TestGenerationBoundaryCancelledClaimDropsOldOpportunity(t *testing.T) {
	root, v := t.TempDir(), policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
	play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, ok, _ := claimScriptGenerationPlay(ctx, root, 14, 29, 99, v, u, play); ok {
		t.Fatal("cancelled purchase")
	}
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("resume can buy an old pre-stop opportunity", pending)
	}
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
	if pending, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); pending != nil {
		t.Fatal("replayed cancelled receipt resurrected purchase")
	}
}

func TestGenerationBoundaryVoiceRoundTripAndOldImplementationPermit(t *testing.T) {
	for _, transition := range []string{"voice-round-trip", "old-implementation"} {
		t.Run(transition, func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
			_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
			changed := v
			if transition == "voice-round-trip" {
				changed.VoiceIdentity.VoiceID = "B"
			} else {
				path := scriptGenerationPlayPath(root, 14, 29, v.PlanID)
				st, _ := readScriptGenerationPlays(path)
				st.Pending.Identity = shortScriptIdentity(scriptRewriteFactsKey(v) + scriptFirstParagraphKey(v, "policy"))
				if err := writeVolcengineJSON(path, st); err != nil {
					t.Fatal(err)
				}
			}
			if p, err := pendingScriptGenerationPlay(root, 14, 29, 99, changed); p != nil || err != nil {
				t.Fatal(p, err)
			}
			if p, err := pendingScriptGenerationPlay(root, 14, 29, 99, v); p != nil || err != nil {
				t.Fatal("old permit revived", p, err)
			}
			_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
			if p, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); p != nil {
				t.Fatal("same receipt revived old permit")
			}
			_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-2")
			if p, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); p == nil {
				t.Fatal("new actual play blocked")
			}
		})
	}
}

func TestGenerationBoundaryCancelledPollDropsPending(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root, v := recordingCacheMediaRoot(), policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
	ctx := agentgateway.WithRoomModelScope(t.Context(), agentgateway.RoomModelScope{TenantID: 14, RoomID: 29, RuntimeID: 99})
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	(&Server{}).tickScriptPoolPolicy(ctx, 14, 29, v, []volcengineScriptUnit{u})
	if p, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); p != nil {
		t.Fatal("cancelled poll left pre-stop work")
	}
}

func TestGenerationBoundaryOptionalRecordingDoesNotWaitForLocks(t *testing.T) {
	for _, lock := range []string{"opportunity", "pool"} {
		t.Run(lock, func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
			path := scriptGenerationPlayPath(root, 14, 29, v.PlanID) + ".lock"
			if lock == "pool" {
				path = filepath.Join(root, "script-voices", ".store.lock")
			}
			release, err := sharedfilelock.Lock(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			done := make(chan error, 1)
			go func() { done <- recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1") }()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(250 * time.Millisecond):
				release()
				<-done
				t.Fatal("optional generation blocked mainline on", lock)
			}
		})
	}
}

func TestGenerationBoundaryWrongProgramCannotGrantPurchase(t *testing.T) {
	for _, mismatch := range []string{"version", "program"} {
		t.Run(mismatch, func(t *testing.T) {
			t.Setenv("MEDIA_ROOT", t.TempDir())
			root, v := recordingCacheMediaRoot(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
			cursor := volcengineScriptCursor{VersionID: v.ID, ProgramID: "expected", Delivered: []scriptDeliveredTrack{{Unit: 0, TrackID: "long-99-1-raw", InstanceID: "long-99-1", AssetID: 1, VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model}}}
			p := coreclient.ProgramRefreshSnapshot{VersionID: v.ID, ProgramID: "expected"}
			if mismatch == "version" {
				p.VersionID++
			} else {
				p.ProgramID = "other-program"
			}
			p.PlaybackReceipts = []anchorcycle.PlaybackReceipt{{TrackID: cursor.Delivered[0].TrackID, PlayedMS: 20}}
			if err := (&Server{}).confirmScriptPlayback(t.Context(), 14, 29, 99, v, []volcengineScriptUnit{u}, &cursor, p, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			if play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); play != nil {
				t.Fatal("foreign snapshot granted a purchase", play)
			}
		})
	}
}

func TestGenerationBoundaryCorruptEvidenceFailsClosedAndKeepsNewerPlay(t *testing.T) {
	for _, failure := range []string{"broken-json", "empty-object", "negative-sequence", "pending-sequence", "corrupt-pool"} {
		t.Run(failure, func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
			_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
			play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v)
			path, raw := scriptGenerationPlayPath(root, 14, 29, v.PlanID), "{"
			switch failure {
			case "empty-object":
				raw = "{}"
			case "negative-sequence":
				raw = `{"session":99,"version":195,"last_sequence":-1}`
			case "pending-sequence":
				st, _ := readScriptGenerationPlays(path)
				st.LastSequence++
				if err := writeVolcengineJSON(path, st); err != nil {
					t.Fatal(err)
				}
			case "corrupt-pool":
				path = scriptVoicePoolPath(root, v.PlanID, u.poolID())
			}
			if failure != "pending-sequence" {
				if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play); ok || err == nil {
				if release != nil {
					release()
				}
				t.Fatal("corrupt evidence allowed work", ok, err)
			}
		})
	}
	root, v := t.TempDir(), policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
	old, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-2")
	discardScriptGenerationPlay(root, 14, 29, 99, v, old)
	if _, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, old); ok || err != nil {
		t.Fatal(ok, err)
	}
	if latest, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); latest == nil || latest.Instance != "long-99-2" {
		t.Fatal("stale worker damaged newer play", latest)
	}
}

func TestGenerationBoundaryLifecycleBeforeEveryModelAndPaidTTS(t *testing.T) {
	provider := &strictScriptModelProvider{}
	s := &Server{agentGateway: agentgateway.New(provider.Name(), agentgateway.DefaultQwenModel)}
	s.agentGateway.Register(provider)
	var stopped atomic.Bool
	guardErr := errors.New("room stopped/switched")
	ctx := context.WithValue(t.Context(), scriptGenerationGuardKey{}, func(context.Context) error {
		if stopped.Load() {
			return guardErr
		}
		return nil
	})
	request := agentgateway.Request{Stage: "script_rewrite", MaxTokens: 800, Messages: []agentgateway.Message{{Role: "user", Content: "原请求保持"}}}
	if _, err := s.completePlayedScriptModel(ctx, request); err != nil {
		t.Fatal(err)
	}
	stopped.Store(true) // Simulated owner change after the first model result.
	for _, stage := range []string{"script_rewrite", "script_rewrite_repair", "script_rewrite_extend", "script_rewrite_opening"} {
		request.Stage = stage
		if _, err := s.completePlayedScriptModel(ctx, request); !errors.Is(err, guardErr) {
			t.Fatal(stage, err)
		}
	}
	if provider.calls.Load() != 1 {
		t.Fatal("late owner change sent more models", provider.calls.Load())
	}
	unit := volcengineScriptUnit{Key: "paid", Dir: t.TempDir()}
	ttsCalls := 0
	generate := func(_ context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
		ttsCalls++
		if err := os.WriteFile(path, []byte("paid-source-simulation"), 0600); err != nil {
			return ttsgateway.SynthesizeResponse{}, err
		}
		stopped.Store(true) // Result arrives after owner changes; retain paid bytes.
		return ttsgateway.SynthesizeResponse{AudioFile: path, VoiceID: "A"}, nil
	}
	if _, err := prepareScriptFirstPaid(ctx, unit, generate); !errors.Is(err, guardErr) || ttsCalls != 0 {
		t.Fatal("stopped room sent TTS", err, ttsCalls)
	}
	stopped.Store(false)
	if _, err := prepareScriptFirstPaid(ctx, unit, generate); err != nil {
		t.Fatal(err)
	}
	if err := checkScriptGenerationGuard(ctx); !errors.Is(err, guardErr) {
		t.Fatal("late result could activate", err)
	}
	if _, err := prepareScriptFirstPaid(ctx, unit, generate); err != nil || ttsCalls != 1 {
		t.Fatal("paid result was not reused", err, ttsCalls)
	}
	if info, err := os.Stat(filepath.Join(unit.Dir, "paragraph.wav")); err != nil || info.Size() == 0 {
		t.Fatal("late paid bytes lost", err)
	}
}

func TestGenerationBoundaryCrashProcessHelper(t *testing.T) {
	root := os.Getenv("SCRIPT_GENERATION_CRASH_ROOT")
	if root == "" {
		return
	}
	v, u := policyVersion(), volcengineScriptUnit{Pool: "p", Text: "原文"}
	play, err := pendingScriptGenerationPlay(root, 14, 29, 99, v)
	if err != nil || play == nil {
		t.Fatal(play, err)
	}
	_, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	// Deliberately bypass defers: model/TTS outcome may be unknown on a crash.
	os.Exit(0)
}

func TestGenerationBoundaryCrashReleasesOSLeaseButNeverRebuys(t *testing.T) {
	root, v := t.TempDir(), policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestGenerationBoundaryCrashProcessHelper$")
	cmd.Env = append(os.Environ(), "SCRIPT_GENERATION_CRASH_ROOT="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(err, string(out))
	}
	release, ok, err := sharedfilelock.TryLock(scriptGenerationPlayPath(root, 14, 29, v.PlanID) + ".active.lock")
	if err != nil || !ok {
		t.Fatal("crash stranded OS lease", ok, err)
	}
	release()
	for n := 0; n < 100; n++ {
		_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-1")
	}
	if play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); play != nil {
		t.Fatal("restart repurchased unknown paid work", play)
	}
	_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, "long-99-2")
	if play, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); play == nil {
		t.Fatal("future real playback could not recover")
	}
}

func TestGenerationBoundaryMultiRoomConcurrentSharedFill(t *testing.T) {
	root, v := t.TempDir(), policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	for room := int64(29); room < 45; room++ {
		if err := recordScriptGenerationPlay(root, 14, room, 99, v, u, "long-99-1"); err != nil {
			t.Fatal(err)
		}
	}
	var purchases atomic.Int32
	var wg sync.WaitGroup
	for room := int64(29); room < 45; room++ {
		wg.Go(func() {
			sharedRelease, ok, err := tryScriptGeneration(root, 14, v.PlanID, u.poolID())
			if err != nil {
				t.Error(err)
				return
			}
			if !ok {
				return
			}
			defer sharedRelease()
			play, err := pendingScriptGenerationPlay(root, 14, room, 99, v)
			if err != nil {
				t.Error(err)
				return
			}
			release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, room, 99, v, u, play)
			if err != nil {
				t.Error(err)
				return
			}
			if !ok {
				return
			}
			defer release()
			purchases.Add(1)
			policyFixture(t, root, v, u, 2, "共享新声音", 75000, scriptplay.KindRewrite)
		})
	}
	wg.Wait()
	if purchases.Load() != 1 {
		t.Fatal("same shared deficit purchased repeatedly", purchases.Load())
	}
	for room := int64(29); room < 45; room++ {
		play, _ := pendingScriptGenerationPlay(root, 14, room, 99, v)
		if release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, room, 99, v, u, play); ok || err != nil {
			if release != nil {
				release()
			}
			t.Fatal("late room bought shared result", room, ok, err)
		}
	}
}

func TestGenerationSimulationSession640OpeningOldClockCannotFillAhead(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root, v := recordingCacheMediaRoot(), policyVersion()
	units := []volcengineScriptUnit{{Pool: "e-t2-s1", Text: "片段470"}, {Pool: "e-t2-s3", Text: "片段651"}, {Pool: "e-t2-s2", Text: "片段963"}, {Pool: "e-t1-s1", Text: "片段796"}, {Pool: "e-t1-s2", Text: "片段936"}}
	for i, u := range units {
		policyFixture(t, root, v, u, int64(i+1), u.Text, 120000, scriptplay.KindOriginal)
	}
	policyFixture(t, root, v, units[0], 6, "已有改写", 109440, scriptplay.KindRewrite)
	old := scriptPoolPolicyState{FirstObserved: time.Now().Add(-6 * time.Hour), InitialAudioMS: 87120}
	if err := writeVolcengineJSON(scriptPoolPolicyPath(root, v), old); err != nil {
		t.Fatal(err)
	}
	ctx := agentgateway.WithRoomModelScope(t.Context(), agentgateway.RoomModelScope{TenantID: 14, RoomID: 29, RuntimeID: 640})
	for poll := 0; poll < 500; poll++ {
		(&Server{}).tickScriptPoolPolicy(ctx, 14, 29, v, units)
	}
	for _, u := range units {
		if p, _ := pendingScriptGenerationPlay(root, 14, 29, 640, v); p != nil {
			t.Fatal("six-hour old clock minted purchase", u, p)
		}
	}
	purchases := 0
	for i, u := range units {
		instance := fmt.Sprintf("long-640-%d", i+1)
		for progress := 0; progress < 100; progress++ {
			if err := recordScriptGenerationPlay(root, 14, 29, 640, v, u, instance); err != nil {
				t.Fatal(err)
			}
			play, err := pendingScriptGenerationPlay(root, 14, 29, 640, v)
			if err != nil {
				t.Fatal(err)
			}
			if play == nil {
				continue
			}
			index, _, _, err := scriptPolicyGenerationForPiece(root, v, units, play.Piece, time.Now(), false)
			if err != nil || index != i {
				t.Fatal(index, err)
			}
			release, ok, err := claimScriptGenerationPlay(ctx, root, 14, 29, 640, v, u, play)
			if err != nil || !ok {
				t.Fatal(ok, err)
			}
			purchases++
			policyFixture(t, root, v, u, int64(100+i), "使用后新增"+u.Text, 120000, scriptplay.KindRewrite)
			release()
		}
		if purchases != i+1 {
			t.Fatal("one playback filled several versions", purchases)
		}
		for j, piece := range units {
			want := 1
			if j == 0 {
				want++
			}
			if j <= i {
				want++
			}
			if got := len(scriptCompatiblePool(root, v, piece).Versions); got != want {
				t.Fatal("unheard piece filled ahead", j, got, want)
			}
		}
	}
	t.Logf("640 opening simulation: 500 idle polls + 500 progress receipts; 5 actual instances => %d admitted versions, 0 idle purchases", purchases)
}

func TestGenerationSimulationRandomReorderingFailuresAndRestarts(t *testing.T) {
	for _, seed := range []int64{4, 19, 91} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "原文"}
			policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
			rng := rand.New(rand.NewSource(seed))
			var held func()
			purchases, last := 0, 0
			seen := map[string]bool{}
			for event := 0; event < 600; event++ {
				switch rng.Intn(5) {
				case 0, 1:
					last++
					_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, fmt.Sprintf("long-99-%d", last))
				case 2:
					if last > 0 {
						_ = recordScriptGenerationPlay(root, 14, 29, 99, v, u, fmt.Sprintf("long-99-%d", 1+rng.Intn(last)))
					}
				case 3:
					if held != nil {
						held()
						held = nil
					} // Provider failed or process restarted: no automatic retry.
				case 4:
					play, err := pendingScriptGenerationPlay(root, 14, 29, 99, v)
					if err != nil {
						t.Fatal(err)
					}
					if play == nil {
						continue
					}
					release, ok, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play)
					if err != nil {
						t.Fatal(err)
					}
					if !ok {
						continue
					}
					if seen[play.Instance] {
						t.Fatal("replayed instance purchased twice", play.Instance)
					}
					seen[play.Instance] = true
					purchases++
					held = release
				}
				if held != nil {
					if p, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); p != nil {
						t.Fatal("in-flight generation accumulated backlog")
					}
				}
				if purchases > last {
					t.Fatal("more purchases than actual playback", purchases, last)
				}
			}
			if held != nil {
				held()
			}
			t.Logf("seed=%d events=600 actualInstances=%d purchases=%d duplicatePurchases=0", seed, last, purchases)
		})
	}
}

func TestGenerationSimulationFourHourCachePlayback(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		latency   time.Duration
		failEvery int
		warm      bool
	}{{"cold-fast", 20 * time.Second, 0, false}, {"cold-slow", 6 * time.Minute, 0, false}, {"cold-failures", 20 * time.Second, 3, false}, {"warm-reuse", 20 * time.Second, 0, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			root, v := t.TempDir(), policyVersion()
			u := volcengineScriptUnit{Pool: "p", Text: "四百字原文"}
			units := []volcengineScriptUnit{u}
			duration := int64(75000)
			if scenario.warm {
				duration = 90 * 60000
			}
			policyFixture(t, root, v, u, 1, u.Text, duration, scriptplay.KindOriginal)
			base := time.Now().UTC()
			if err := beginScriptRoomPolicy(root, 14, 29, v.PlanID, 99); err != nil {
				t.Fatal(err)
			}
			// Replay the production stale-clock shape, not a fresh policy file.
			if err := writeVolcengineJSON(scriptPoolPolicyPath(root, v), scriptPoolPolicyState{FirstObserved: base.Add(-6 * time.Hour), InitialAudioMS: 87120}); err != nil {
				t.Fatal(err)
			}
			rng := rand.New(rand.NewSource(640))
			var held func()
			var finish time.Time
			pendingText := ""
			attempts, added, plays, first90 := 0, 0, 0, 0
			seen := map[string]bool{}
			complete := func() {
				if held == nil {
					return
				}
				if pendingText != "" {
					policyFixture(t, root, v, u, int64(1000+attempts), pendingText, 75000, scriptplay.KindRewrite)
					added++
				}
				held()
				held = nil
				pendingText = ""
			}
			for at := base; at.Before(base.Add(4 * time.Hour)); {
				if held != nil && !at.Before(finish) {
					complete()
				}
				plays++
				instance := fmt.Sprintf("long-99-%d", plays)
				audio, ok, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, instance, 0, rng, nil, at)
				if err != nil || !ok {
					t.Fatal("playback stopped while generation was busy", plays, err)
				}
				// Formal room playback policy receives simulated positive PCM.
				policyConsume(t, root, v, audio, 29, 99, plays, at)
				if _, err := markScriptVersionPlayedOnce(root, v.PlanID, u.poolID(), fmt.Sprint(audio.AssetID), instance); err != nil {
					t.Fatal(err)
				}
				if err := markScriptVersionCompletedOnce(root, v.PlanID, u.poolID(), fmt.Sprint(audio.AssetID), instance); err != nil {
					t.Fatal(err)
				}
				if err := recordScriptGenerationPlay(root, 14, 29, 99, v, u, instance); err != nil {
					t.Fatal(err)
				}
				play, err := pendingScriptGenerationPlay(root, 14, 29, 99, v)
				if err != nil {
					t.Fatal(err)
				}
				if play != nil {
					index, _, _, err := scriptPolicyGenerationForPiece(root, v, units, play.Piece, at, false, scriptPolicyRoomWarmup(root, 14, 29, v.PlanID, at))
					if err != nil {
						t.Fatal(err)
					}
					if index < 0 {
						discardScriptGenerationPlay(root, 14, 29, 99, v, play)
					} else {
						release, claimed, err := claimScriptGenerationPlay(t.Context(), root, 14, 29, 99, v, u, play)
						if err != nil || !claimed {
							t.Fatal(claimed, err)
						}
						if seen[instance] {
							t.Fatal("one PCM instance repurchased", instance)
						}
						seen[instance] = true
						attempts++
						held = release
						finish = at.Add(scenario.latency)
						if at.Sub(base) < 90*time.Minute {
							first90++
						}
						if scenario.failEvery == 0 || attempts%scenario.failEvery != 0 {
							pendingText = fmt.Sprintf("模拟新版%d", attempts)
						}
					}
				}
				if attempts > plays || added > attempts {
					t.Fatal("generation ran ahead of actual use", plays, attempts, added)
				}
				at = at.Add(time.Duration(audio.DurationMS) * time.Millisecond)
			}
			// Stopping drops unfinished optional work rather than draining a backlog.
			if held != nil {
				held()
			}
			if p, _ := pendingScriptGenerationPlay(root, 14, 29, 99, v); p != nil {
				t.Fatal("stop retained a catch-up purchase", p)
			}
			if scenario.warm && first90 != 0 {
				t.Fatal("warm pool bought during reuse window", first90)
			}
			t.Logf("four-hour scenario=%s PCM_instances=%d admitted_attempts=%d new_versions=%d first90_attempts=%d playback_failures=0", scenario.name, plays, attempts, added, first90)
		})
	}
}
