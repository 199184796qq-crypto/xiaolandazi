package httpapi

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

func TestManualTextReportsAuditAndActualRepair(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	var phases []scriptManualProgress
	ctx := context.WithValue(t.Context(), scriptManualProgressKey{}, func(phase scriptManualProgress) {
		phases = append(phases, phase)
	})
	gateway := agentgateway.New("preview-test", "preview-test-model")
	gateway.Register(scriptPreviewTestProvider(func(req agentgateway.Request) agentgateway.Response {
		switch req.Stage {
		case "script_rewrite":
			if !slices.Equal(phases, []scriptManualProgress{scriptManualWriting}) {
				t.Fatalf("rewrite did not report writing: %v", phases)
			}
			return agentgateway.Response{Text: "话术骨架：开场\n一句话上下文：介绍核销\n话术：我们是全国第一。买好以后就近到店核销。"}
		case "script_rewrite_repair":
			if !slices.Equal(phases, []scriptManualProgress{scriptManualWriting, scriptManualReviewing, scriptManualRevising}) {
				t.Fatalf("repair did not report the rejected review: %v", phases)
			}
			return agentgateway.Response{Text: "可以就近选一家门店。"}
		default:
			t.Fatalf("unexpected model step %s", req.Stage)
			return agentgateway.Response{}
		}
	}))
	text, err := (&Server{agentGateway: gateway}).writeScriptRewrite(ctx, model.LiveAgentPlanVersion{}, volcengineScriptUnit{}, "介绍核销")
	if err != nil || strings.Contains(text.Script, "全国第一") || !strings.Contains(text.Script, "可以就近选一家门店") ||
		!slices.Equal(phases, []scriptManualProgress{scriptManualWriting, scriptManualReviewing, scriptManualRevising, scriptManualReviewing}) {
		t.Fatalf("unexpected foreground repair: %q, %v, %v", text.Script, phases, err)
	}
}

func TestManualGenerationQueuesByPieceWithoutKeepingPlaybackPending(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	ctx := t.Context()
	path := scriptManualPath(14, 29, 42)
	first := scriptManualJump{RequestID: "manual-request-0001", SessionID: 42, VersionID: 10, ProgramID: "program", PieceID: "kiwi"}
	queue := func(command scriptManualJump, wait bool) {
		t.Helper()
		if _, err := submitScriptManual(ctx, path, command); err != nil {
			t.Fatal(err)
		}
		if err := enqueueScriptManualGeneration(ctx, path, command, "same-audio-identity", 1, wait); err != nil {
			t.Fatal(err)
		}
	}
	applied := func(command scriptManualJump) {
		t.Helper()
		if err := updateScriptManual(ctx, path, command, "applied", ""); err != nil {
			t.Fatal(err)
		}
	}
	queue(first, false)
	applied(first)
	if err := updateScriptManualProgress(ctx, path, first, scriptManualWriting); err != nil {
		t.Fatal(err)
	}
	playing, err := readScriptManual(path)
	if err != nil || playing.Status != "applied" || playing.Progress != "" {
		t.Fatalf("background progress was exposed in cached playback: %+v, %v", playing, err)
	}
	next := first
	next.RequestID, next.PieceID = "manual-request-0002", "orange"
	queue(next, false)
	applied(next)
	again := first
	again.RequestID = "manual-request-0003"
	queue(again, true)
	state, err := readScriptManualState(path)
	if err != nil || len(state.Generation) != 2 || state.Generation[0].Command.RequestID != first.RequestID ||
		state.Generation[0].Waiter != again.RequestID || state.Generation[1].Command.RequestID != next.RequestID ||
		state.Current.Status != "preparing" || state.Current.Progress != scriptManualWriting ||
		state.Receipts[first.RequestID].Status != "applied" {
		t.Fatalf("room queue lost order, deduplication or playback state: %+v, %v", state, err)
	}
	if err := updateScriptManualProgress(ctx, path, first, scriptManualSynthesizing); err != nil {
		t.Fatal(err)
	}
	if err := finishScriptManualGeneration(ctx, path, first, volcengineCachedAudio{AssetID: 71}, nil); err != nil {
		t.Fatal(err)
	}
	state, err = readScriptManualState(path)
	if err != nil || len(state.Generation) != 1 || state.Generation[0].Command.RequestID != next.RequestID ||
		state.Prepared == nil || state.Prepared.AssetID != 71 || state.Current.RequestID != again.RequestID ||
		state.Current.Progress != scriptManualAudioReady {
		t.Fatalf("coalesced waiter did not get the audio: %+v, %v", state, err)
	}
	applied(again)
	if err := finishScriptManualGeneration(ctx, path, next, volcengineCachedAudio{}, errors.New("supplier failed")); err != nil {
		t.Fatal(err)
	}
	state, err = readScriptManualState(path)
	if err != nil || len(state.Generation) != 0 || state.Current.Status != "applied" ||
		state.Current.Progress != scriptManualAudioReady || state.Receipts[next.RequestID].Status != "applied" {
		t.Fatalf("background error modified completed playback: %+v, %v", state, err)
	}
	if pending, err := scriptManualGenerationPending(14, 29, 43); err != nil || pending {
		t.Fatalf("generation escaped the live session: %v, %v", pending, err)
	}
}

func TestManualGenerationReviewFailureIsVisibleOnlyWhenWaitingForAudio(t *testing.T) {
	for _, wait := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), "command.json")
		ctx := t.Context()
		job := scriptManualJump{RequestID: "manual-request-0001", SessionID: 42, VersionID: 10, ProgramID: "program", PieceID: "kiwi", Progress: scriptManualAudioReady}
		receipt, err := submitScriptManual(ctx, path, job)
		if err != nil || receipt.Progress != "" {
			t.Fatalf("caller-supplied progress was accepted: %+v, %v", receipt, err)
		}
		if err = enqueueScriptManualGeneration(ctx, path, job, "identity", 1, wait); err != nil {
			t.Fatal(err)
		}
		if !wait {
			if err = updateScriptManual(ctx, path, job, "applied", ""); err != nil {
				t.Fatal(err)
			}
		}
		for _, phase := range []scriptManualProgress{scriptManualQueued, scriptManualWriting, scriptManualReviewing, scriptManualReviewRejected} {
			if err = updateScriptManualProgress(ctx, path, job, phase); err != nil {
				t.Fatal(err)
			}
			current, err := readScriptManual(path)
			want := phase
			if !wait {
				want = ""
			}
			if err != nil || current.Progress != want {
				t.Fatalf("unexpected progress: %+v, %v, want %q", current, err, want)
			}
		}
		if err = finishScriptManualGeneration(ctx, path, job, volcengineCachedAudio{}, errSpecifiedScriptReviewRejected); err != nil {
			t.Fatal(err)
		}
		current, err := readScriptManual(path)
		if err != nil ||
			wait && (current.Progress != scriptManualReviewRejected || current.Status != "failed" || current.Error == "") ||
			!wait && (current.Progress != "" || current.Status != "applied" || current.Error != "") {
			t.Fatalf("wrong result of rejected generation (wait=%v): %+v, %v", wait, current, err)
		}
	}
}

func TestManualGenerationWaiterCanDetachWhenPreparedAudioAppears(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	ctx := t.Context()
	path := scriptManualPath(14, 29, 42)
	job := scriptManualJump{RequestID: "manual-request-0001", SessionID: 42, VersionID: 10, ProgramID: "program", PieceID: "kiwi"}
	if _, err := submitScriptManual(ctx, path, job); err != nil {
		t.Fatal(err)
	}
	for _, wait := range []bool{true, true, false} {
		if err := enqueueScriptManualGeneration(ctx, path, job, "identity", 1, wait); err != nil {
			t.Fatal(err)
		}
	}
	state, err := readScriptManualState(path)
	if err != nil || len(state.Generation) != 1 || state.Generation[0].Waiter != "" || state.Current.Progress != "" {
		t.Fatal(state, err)
	}
	if err := finishScriptManualGeneration(ctx, path, job, volcengineCachedAudio{}, errors.New("supplier failed")); err != nil {
		t.Fatal(err)
	}
	state, err = readScriptManualState(path)
	if err != nil || state.Current.Status == "failed" {
		t.Fatal("a background failure cancelled a ready cut", state, err)
	}
}

func TestManualGenerationWaitsForRoomSlotAndDropsAnOldVoiceBatch(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root := recordingCacheMediaRoot()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	path := scriptManualPath(14, 29, 42)
	version := manualTestVersion()
	p := coreclient.ProgramRefreshSnapshot{ProgramID: "program", VersionID: version.ID, Running: true, Sequential: true, ManualSelectionSupported: true}
	p.Task.SessionID = "volcengine-long-42"
	units, err := scriptFirstUnits(version, root)
	if err != nil {
		t.Fatal(err)
	}
	job := scriptManualJump{RequestID: "manual-request-0001", SessionID: 42, VersionID: version.ID, ProgramID: p.ProgramID, PieceID: units[0].poolID()}
	if _, err := submitScriptManual(ctx, path, job); err != nil {
		t.Fatal(err)
	}
	if err := enqueueScriptManualGeneration(ctx, path, job, "identity", 1, true); err != nil {
		t.Fatal(err)
	}
	release, ok, err := tryRoomScriptGeneration(root, 14, 29)
	if err != nil || !ok {
		t.Fatal("could not simulate an ongoing room generation", err)
	}
	s := &Server{}
	if err := s.tickScriptManualGeneration(ctx, 14, 29, 42, version, units, p, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ensureRoomScriptLookahead(ctx, 14, 29, 42, version, units[0], true); !errors.Is(err, errRoomScriptGenerationBusy) {
		t.Fatalf("another generation started in the occupied room: %v", err)
	}
	if releaseOther, ok, err := tryRoomScriptGeneration(root, 14, 30); err != nil || !ok {
		t.Fatalf("another room was blocked: %v, %v", ok, err)
	} else {
		releaseOther()
	}
	if pending, err := scriptManualGenerationPending(14, 29, 42); err != nil || !pending {
		t.Fatal("pending work was lost while the slot was busy", pending, err)
	}
	release()
	if err := s.tickScriptManualGeneration(ctx, 14, 29, 42, version, units, p, 2); err != nil {
		t.Fatal(err)
	}
	state, err := readScriptManualState(path)
	if err != nil || len(state.Generation) != 0 || state.Current.Status != "failed" {
		t.Fatal("old voice task was not retired", state, err)
	}
}

func TestManualCacheSelectionBypassesGenerationAndPrefersTargetedAudio(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{TenantID: 14, PlanID: 4, VoiceIdentity: playbackIdentity("selected")}
	unit := volcengineScriptUnit{Pool: "kiwi", Text: "原版口播。", SpecPromptEnabled: true, SpecPrompt: "改写为更口语的版本"}
	unit.Key = scriptFirstParagraphKey(v, unit.Text)
	unit.Dir = filepath.Join(root, "fixtures", unit.Key)
	addReadyScriptFixture(t, root, v, unit, v.VoiceIdentity.VoiceID, 11)
	ordinary := unit
	ordinary.Text = "普通改写口播。"
	addReadyScriptFixture(t, root, v, ordinary, v.VoiceIdentity.VoiceID, 12)
	releaseRoom, ok, err := tryRoomScriptGeneration(root, 14, 29)
	if err != nil || !ok {
		t.Fatal(err)
	}
	t.Cleanup(releaseRoom)
	releasePlan, err := tryManualScriptGeneration(root, 14, v, unit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releasePlan)
	anyAudio := func(int64) bool { return true }
	pick := func(id int64, available func(int64) bool) {
		t.Helper()
		audio, ok := selectManualCachedAudio(root, "room-29", v, unit, true, available)
		if ok != (id != 0) || audio.AssetID != id {
			t.Fatalf("cached asset = %d, ready = %v, want %d", audio.AssetID, ok, id)
		}
	}
	pick(11, anyAudio) // The target already has original audio and only ordinary rewrites.
	targeted := ordinary
	targeted.Text = "定向改写口播。"
	raw := addReadyScriptFixture(t, root, v, targeted, v.VoiceIdentity.VoiceID, 13)
	pool, err := loadScriptVoicePool(root, v.PlanID, unit.poolID())
	if err != nil {
		t.Fatal(err)
	}
	for i := range pool.Versions {
		if pool.Versions[i].AudioRef == raw.AudioRef {
			pool.Versions[i].Kind = scriptplay.KindRewrite
			pool.Versions[i].SpecHash = scriptPieceVersionHash(unit)
		}
	}
	if err := saveScriptVoicePool(root, v.PlanID, unit.poolID(), pool); err != nil {
		t.Fatal(err)
	}
	pick(13, anyAudio)
	pick(11, func(id int64) bool { return id == 11 })
	pick(0, func(int64) bool { return false })
	unit.SpecPrompt = "更新后的定向要求"
	pick(11, anyAudio)
}
