package httpapi

import (
	"context"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/ttsgateway"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func multiRoomVersion() model.LiveAgentPlanVersion {
	return withScriptRewriteSnapshot(model.LiveAgentPlanVersion{ID: 195, TenantID: 14, PlanID: 4, RoomID: 29, VoiceIdentity: playbackIdentity("A")}, scriptRewriteSnapshot{Prompt: "same persona, redlines and rewrite rules", Facts: "same verified facts"})
}
func TestMultiRoomSharesTextAcrossRoomsAndVoicesButSeparatesCompletePolicy(t *testing.T) {
	root := t.TempDir()
	a := multiRoomVersion()
	u := volcengineScriptUnit{Pool: "entry-one", Text: "original"}
	if err := saveScriptSharedText(root, a, u, "accepted rewrite"); err != nil {
		t.Fatal(err)
	}
	b := a
	b.RoomID = 30
	b.VoiceIdentity = playbackIdentity("B")
	if text, found, err := scriptMissingSharedText(root, b, u); err != nil || !found || text.Text != "accepted rewrite" {
		t.Fatal(text, found, err)
	}
	for _, which := range []string{"facts", "rules", "tenant", "plan"} {
		changed := b
		switch which {
		case "facts":
			changed = withScriptRewriteSnapshot(changed, scriptRewriteSnapshot{Prompt: "same persona, redlines and rewrite rules", Facts: "different price"})
		case "rules":
			changed = withScriptRewriteSnapshot(changed, scriptRewriteSnapshot{Prompt: "changed redlines", Facts: "same verified facts"})
		case "tenant":
			changed.TenantID = 24
		case "plan":
			changed.PlanID = 5
		}
		if _, found, err := scriptMissingSharedText(root, changed, u); err != nil || found {
			t.Fatal(which, "crossed policy boundary", found, err)
		}
		if err := saveScriptSharedText(root, changed, u, "other scope"); err != nil {
			t.Fatal(err)
		}
	}
	if text, found, err := scriptMissingSharedText(root, a, u); err != nil || !found || text.Text != "accepted rewrite" {
		t.Fatal("other facts erased original shared text", text, found, err)
	}
}

func TestMultiRoomUsesOwnHistoryAndSamePaidAudio(t *testing.T) {
	root := t.TempDir()
	a := multiRoomVersion()
	b := a
	b.RoomID = 30
	u := volcengineScriptUnit{Pool: "entry-one", Text: "original"}
	original := addReadyScriptFixture(t, root, a, u, "A", 101)
	rewritten := u
	rewritten.Text = "accepted rewrite"
	second := addReadyScriptFixture(t, root, a, rewritten, "A", 102)
	second.Kind = scriptplay.KindRewrite
	second.FactsKey = scriptRewriteFactsKey(a)
	if err := saveScriptVoicePool(root, a.PlanID, u.poolID(), scriptplay.Pool{Versions: []scriptplay.Version{original, second}}); err != nil {
		t.Fatal(err)
	}
	if err := markScriptRoomPlayback(root, 14, 29, 4, u.poolID(), "101", "roomA-session1-track1", true); err != nil {
		t.Fatal(err)
	}
	if err := markScriptRoomPlayback(root, 14, 30, 4, u.poolID(), "102", "roomB-session2-track1", true); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		v    model.LiveAgentPlanVersion
		want int64
	}{{a, 102}, {b, 101}} {
		history, err := loadScriptRoomPlayback(root, 14, sample.v.RoomID, 4)
		if err != nil {
			t.Fatal(err)
		}
		audio, _, ok := scriptCompatiblePoolAudio(root, sample.v, u, fmt.Sprint(sample.v.RoomID), rand.New(rand.NewSource(2)), history)
		if !ok || audio.AssetID != sample.want {
			t.Fatal("other room advanced local history", sample.v.RoomID, audio, ok)
		}
	}
	// A restart/repeated receipt does not increment B, and room C stays empty.
	for i := 0; i < 3; i++ {
		if err := markScriptRoomPlayback(root, 14, 30, 4, u.poolID(), "102", "roomB-session2-track1", true); err != nil {
			t.Fatal(err)
		}
	}
	h, err := loadScriptRoomPlayback(root, 14, 30, 4)
	if err != nil {
		t.Fatal(err)
	}
	if state := h[scriptRoomAssetKey(u.poolID(), "102")]; state.PlayCount != 1 || state.CompleteCount != 1 {
		t.Fatal(state)
	}
	if h, err := loadScriptRoomPlayback(root, 14, 31, 4); err != nil || len(h) != 0 {
		t.Fatal(h, err)
	}
	if _, ok := scriptVersionAudio(root, u, b, second); !ok {
		t.Fatal("same voice/plan paid audio unavailable to B")
	}
	b.VoiceIdentity.Rate = 1.05
	if _, ok := scriptVersionAudio(root, u, b, second); ok {
		t.Fatal("changed audio parameters reused old PCM")
	}
}

func TestMultiRoomNewRulesCanReapproveExactPaidAudioWithoutRetiringOrResettingCounters(t *testing.T) {
	root := t.TempDir()
	v := multiRoomVersion()
	u := volcengineScriptUnit{Pool: "entry", Text: "accepted rewrite"}
	old := addReadyScriptFixture(t, root, v, u, "A", 101)
	old.Kind = scriptplay.KindRewrite
	old.FactsKey = "old-rule-fingerprint"
	old.PlayCount = 7
	old.CompleteCount = 6
	if err := saveScriptVoicePool(root, v.PlanID, u.poolID(), scriptplay.Pool{Versions: []scriptplay.Version{old}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := scriptVersionAudio(root, u, v, old); ok {
		t.Fatal("old rules accepted without validation")
	}
	accepted := old
	accepted.FactsKey = scriptRewriteFactsKey(v)
	if added, err := commitScriptRewriteVersion(root, v, u, accepted, 1, true, "101"); err != nil || added {
		t.Fatal("same paid asset treated as a replacement", added, err)
	}
	pool, err := loadScriptVoicePool(root, v.PlanID, u.poolID())
	if err != nil || len(pool.Versions) != 1 {
		t.Fatal(pool, err)
	}
	if pool.Versions[0].PlayCount != 7 || pool.Versions[0].CompleteCount != 6 {
		t.Fatal("reapproval reset actual PCM counts", pool)
	}
	if _, ok := scriptVersionAudio(root, u, v, pool.Versions[0]); !ok {
		t.Fatal("newly accepted exact paid audio not reusable")
	}
}

func TestMultiRoomParagraphProcessHelper(t *testing.T) {
	root := os.Getenv("SHARED_PARAGRAPH_HELPER_ROOT")
	if root == "" {
		return
	}
	u := volcengineScriptUnit{Key: "one-paid-source", Dir: filepath.Join(root, "paragraph")}
	release, err := lockVolcengineParagraph(t.Context(), u.Dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = prepareScriptFirstPaid(t.Context(), u, func(_ context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
		f, err := os.OpenFile(filepath.Join(root, "purchases"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return ttsgateway.SynthesizeResponse{}, err
		}
		_, err = f.WriteString("one fake purchase\n")
		_ = f.Close()
		if err != nil {
			return ttsgateway.SynthesizeResponse{}, err
		}
		if err := os.WriteFile(path, scriptFirstPCMToWAV(make([]int16, 24000)), 0600); err != nil {
			return ttsgateway.SynthesizeResponse{}, err
		}
		return ttsgateway.SynthesizeResponse{AudioFile: path, VoiceID: "A"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestMultiRoomConcurrentProcessesPurchaseSameParagraphOnlyOnce(t *testing.T) {
	root := t.TempDir()
	var commands []*exec.Cmd
	for i := 0; i < 4; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMultiRoomParagraphProcessHelper$")
		cmd.Env = append(os.Environ(), "SHARED_PARAGRAPH_HELPER_ROOT="+root)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "purchases"))
	if err != nil || string(raw) != "one fake purchase\n" {
		t.Fatal("duplicate paid synthesis", string(raw), err)
	}
}

func TestMultiRoomPoolProcessHelper(t *testing.T) {
	root := os.Getenv("SHARED_POOL_HELPER_ROOT")
	if root == "" {
		return
	}
	for i := 0; i < 8; i++ {
		instance := fmt.Sprintf("process-%d-instance-%d", os.Getpid(), i)
		if fresh, err := markScriptVersionPlayedOnce(root, 4, "piece", "101", instance); err != nil || !fresh {
			t.Fatal(fresh, err)
		}
		if err := markScriptVersionCompletedOnce(root, 4, "piece", "101", instance); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMultiRoomPoolCountersDoNotLoseSeparateProcessReceipts(t *testing.T) {
	root := t.TempDir()
	if err := addScriptVoiceVersion(root, 4, "piece", scriptplay.Version{VoiceID: "A", Model: "same", Kind: scriptplay.KindOriginal, AudioRef: "101", Text: "original", Fingerprint: scriptplay.Fingerprint("original")}); err != nil {
		t.Fatal(err)
	}
	var commands []*exec.Cmd
	for i := 0; i < 4; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMultiRoomPoolProcessHelper$")
		cmd.Env = append(os.Environ(), "SHARED_POOL_HELPER_ROOT="+root)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	for _, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := loadScriptVoicePool(root, 4, "piece")
	if err != nil || len(pool.Versions) != 1 || pool.Versions[0].PlayCount != 32 || pool.Versions[0].CompleteCount != 32 {
		t.Fatal("cross-process receipts lost", pool, err)
	}
}

func TestMultiRoomRewriteGateAndRoomCancellation(t *testing.T) {
	root := t.TempDir()
	release, acquired, err := tryScriptGeneration(root, 14, 4, "piece")
	if err != nil || !acquired {
		t.Fatal(err)
	}
	if other, acquired, err := tryScriptGeneration(root, 14, 4, "piece"); err != nil || acquired {
		if other != nil {
			other()
		}
		t.Fatal("duplicate shared rewrite", acquired, err)
	}
	other, acquired, err := tryScriptGeneration(root, 14, 5, "piece")
	if err != nil || !acquired {
		t.Fatal("other plan blocked", err)
	}
	other()
	release()
	path := filepath.Join(root, "paragraph")
	first, err := lockVolcengineParagraph(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	a, cancelA := context.WithTimeout(t.Context(), 25*time.Millisecond)
	defer cancelA()
	if _, err := lockVolcengineParagraph(a, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	first()
	if b, err := lockVolcengineParagraph(t.Context(), path); err != nil {
		t.Fatal("A cancellation cancelled B", err)
	} else {
		b()
	}
}

func TestMultiRoomPCMCountersMergeConcurrentReceiptsWithoutAdvancingOthers(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- markScriptRoomPlayback(root, 14, 29, 4, "piece", "101", strconv.Itoa(i), true)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	h, err := loadScriptRoomPlayback(root, 14, 29, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := h[scriptRoomAssetKey("piece", "101")]; got.PlayCount != 32 || got.CompleteCount != 32 {
		t.Fatal(got)
	}
	h, err = loadScriptRoomPlayback(root, 14, 30, 4)
	if err != nil || len(h) != 0 {
		t.Fatal(h, err)
	}
}

func TestSharedScriptArchiveOwnershipDoesNotBelongToFirstRoom(t *testing.T) {
	v := multiRoomVersion()
	// A grouped saved plan is shared; the origin room is audit information.
	v.GenerationContext["recording_script"] = recordingScript{Version: 2, Tabs: []recordingScriptTab{{TabID: "t1", Title: "group", Entries: []recordingScriptEntry{{EntryID: "s1", Text: strings.Repeat("这是完整的原始话术。", 50)}}}}}
	ctx := withScriptSharedAsset(t.Context(), v, 29)
	m := map[string]any{"room_id": int64(29)}
	applyScriptSharedAssetInfo(ctx, m)
	if m["room_id"] != int64(0) || m["generated_by_room_id"] != int64(29) || m["cache_scope"] != "plan_shared" {
		t.Fatal(m)
	}
	if !isSharedScriptMedia(model.MediaAsset{AssetType: "generated_voice", Metadata: m}) {
		t.Fatal("shared asset deletion unprotected")
	}
	for _, key := range []string{"paragraph_key", "volcengine_tempo_percent"} {
		if !isSharedScriptMedia(model.MediaAsset{AssetType: "generated_voice", Metadata: map[string]any{"purpose": "live_agent_formal_voice_timeline", key: "legacy"}}) {
			t.Fatal("legacy paid cache deletion unprotected", key)
		}
	}
	if isSharedScriptMedia(model.MediaAsset{AssetType: "source_audio", Metadata: map[string]any{}}) {
		t.Fatal("ordinary upload misclassified")
	}
}

func TestMultiRoomCanonicalAssetDirectorySurvivesRoomCacheClear(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	v := multiRoomVersion()
	v.GenerationContext["recording_script"] = recordingScript{Version: 2, Tabs: []recordingScriptTab{{TabID: "t1", Title: "group", Entries: []recordingScriptEntry{{EntryID: "s1", Text: strings.Repeat("这是完整的原始话术。", 50)}}}}}
	a, err := scriptFirstCacheDir(14, 29, v)
	if err != nil {
		t.Fatal(err)
	}
	b, err := scriptFirstCacheDir(14, 30, v)
	if err != nil || a != b {
		t.Fatal("room created private shared audio copy", a, b, err)
	}
	if _, err := scriptFirstCacheDir(24, 30, v); err == nil {
		t.Fatal("cross tenant plan accepted")
	}
	marker := filepath.Join(a, "paid-audio")
	if err := os.MkdirAll(a, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("paid"), 0600); err != nil {
		t.Fatal(err)
	}
	scope, ok := recordingCacheScopeFor(14, 29, 4)
	if !ok {
		t.Fatal("room scope invalid")
	}
	if err := scope.RemoveCache("", ""); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "paid" {
		t.Fatal("room clear removed shared paid audio", err)
	}
	if scriptSupplyCheckpointPath(14, 29, 600) == scriptSupplyCheckpointPath(14, 30, 601) {
		t.Fatal("room checkpoints collide")
	}
}
