package httpapi

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func manualTestVersion() model.LiveAgentPlanVersion {
	return model.LiveAgentPlanVersion{ID: 10, PlanID: 4, GenerationContext: map[string]any{"recording_script": map[string]any{
		"version": 2, "play_mode": "full", "opening_tab_id": "second", "opening_entry_id": "b",
		"tabs": []any{
			map[string]any{"tab_id": "first", "title": "第一组", "entries": []any{map[string]any{"entry_id": "a", "title": "介绍", "text": strings.Repeat("甲", 600)}, map[string]any{"entry_id": "a2", "title": "核销", "text": strings.Repeat("乙", 700)}}},
			map[string]any{"tab_id": "second", "title": "第二组", "entries": []any{map[string]any{"entry_id": "b", "title": "福利", "text": strings.Repeat("丙", 400)}}},
		},
	}}}
}

func TestManualChoicesUseSavedGroupEntryOrderAndStablePoolIDs(t *testing.T) {
	v := manualTestVersion()
	v.VoiceIdentity = model.LiveAgentVoiceIdentity{Provider: ttsgateway.ProviderTencent, Model: ttsgateway.TencentPremiumModel, VoiceID: "101030", Rate: 1}
	v.GenerationContext["content_mode"] = model.LiveContentUserAudio
	v.GenerationContext["recording_voice_mode"] = recordingStreamMode
	if !usesScriptFirstParagraphs(v) {
		t.Fatal("Tencent official voices must use complete script pieces for manual playback")
	}
	units, err := scriptFirstUnits(v, "")
	if err != nil {
		t.Fatal(err)
	}
	groups := scriptManualGroups(v, units)
	if len(groups) != 2 || groups[0].Title != "第一组" || groups[1].Title != "第二组" || groups[0].Pieces[0].Title != "介绍" || groups[0].Pieces[1].Title != "核销" {
		t.Fatalf("saved order lost: %+v", groups)
	}
	if groups[1].Pieces[0].Unit != 0 {
		t.Fatal("fixture opening rotation not active")
	}
	for _, group := range groups {
		for _, piece := range group.Pieces {
			if scriptManualUnit(units, piece.ID) != piece.Unit {
				t.Fatal("entry resolved by text/index instead of stable piece")
			}
			if piece.HumanKey == "" || piece.HumanKey != units[piece.Unit].HumanKey {
				t.Fatal("recordings must follow the same stable piece association")
			}
		}
	}
}

func TestManualCommandConcurrentRequestsAreSingleFlightAndReplaySafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands", "room-session.json")
	job := scriptManualJump{RequestID: "manual-command-0001", SessionID: 42, VersionID: 10, ProgramID: "same-billed-program", PieceID: "piece-a", RecordingID: 11, RecordingName: "first.wav"}
	var wg sync.WaitGroup
	errorsFound := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := submitScriptManual(context.Background(), path, job)
			if err != nil {
				errorsFound <- err
			} else if result.Status != "requested" {
				errorsFound <- fmt.Errorf("unexpected state %s", result.Status)
			}
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
	other := job
	other.RequestID = "manual-command-0002"
	other.PieceID = "piece-b"
	if _, err := submitScriptManual(t.Context(), path, other); !errors.Is(err, errScriptManualBusy) {
		t.Fatalf("concurrent different selection: %v", err)
	}
	conflict := job
	conflict.PieceID = "piece-b"
	if _, err := submitScriptManual(t.Context(), path, conflict); !errors.Is(err, errScriptManualStale) {
		t.Fatalf("id reused with new payload: %v", err)
	}
	conflict = job
	conflict.RecordingID = 12
	if _, err := submitScriptManual(t.Context(), path, conflict); !errors.Is(err, errScriptManualStale) {
		t.Fatalf("one request ID reused with a different recording: %v", err)
	}
	for _, state := range []string{"preparing", "waiting", "applied"} {
		if err := updateScriptManual(t.Context(), path, job, state, ""); err != nil {
			t.Fatal(err)
		}
	}
	replayed, err := submitScriptManual(t.Context(), path, job)
	if err != nil || replayed.Status != "applied" || replayed.RecordingID != 11 || replayed.RecordingName != "first.wav" {
		t.Fatal("lost acknowledgment restarted command", replayed, err)
	}
	if _, err := submitScriptManual(t.Context(), path, other); err != nil {
		t.Fatal(err)
	}
	if replayed, err := submitScriptManual(t.Context(), path, job); err != nil || replayed.Status != "applied" {
		t.Fatal("old HTTP retry recreated paid request after a newer command", err)
	}
	if err := updateScriptManual(t.Context(), path, job, "failed", "late old error"); !errors.Is(err, errScriptManualStale) {
		t.Fatal("late worker overwrote newer selection", err)
	}
	current, _ := readScriptManual(path)
	if current.RequestID != other.RequestID || current.Status != "requested" {
		t.Fatal("new selection damaged")
	}
}

func TestManualCommandCannotCrossSessionVersionProgramOrStop(t *testing.T) {
	v := manualTestVersion()
	job := &scriptManualJump{RequestID: "manual-command-0001", SessionID: 42, VersionID: v.ID, ProgramID: "current", Status: "preparing"}
	p := coreclient.ProgramRefreshSnapshot{ProgramID: "current", VersionID: v.ID, Running: true, Sequential: true, ManualSelectionSupported: true}
	p.Task.SessionID = "volcengine-long-42"
	if !scriptManualMatches(job, 42, v, p) {
		t.Fatal("valid owner rejected")
	}
	interacting := p
	interacting.Suspended = true
	if !scriptManualMatches(job, 42, v, interacting) {
		t.Fatal("temporary interaction suspension rejected a same-session selection")
	}
	for _, mutate := range []func(*coreclient.ProgramRefreshSnapshot){
		func(p *coreclient.ProgramRefreshSnapshot) { p.ProgramID = "new" },
		func(p *coreclient.ProgramRefreshSnapshot) { p.Running = false },
		func(p *coreclient.ProgramRefreshSnapshot) { p.Task.SessionID = "volcengine-long-43" },
		func(p *coreclient.ProgramRefreshSnapshot) { p.Sequential = false },
		func(p *coreclient.ProgramRefreshSnapshot) { p.VersionID++ },
		func(p *coreclient.ProgramRefreshSnapshot) { p.ManualSelectionSupported = false },
	} {
		copy := p
		mutate(&copy)
		if scriptManualMatches(job, 42, v, copy) {
			t.Fatal("stale owner accepted")
		}
	}
	if scriptManualMatches(job, 43, v, p) {
		t.Fatal("wrong session accepted")
	}
	v.ID++
	if scriptManualMatches(job, 42, v, p) {
		t.Fatal("wrong version accepted")
	}
}

func TestManualContinuationKeepsPlanOrderAndSingleMode(t *testing.T) {
	v := manualTestVersion()
	units, _ := scriptFirstUnits(v, "")
	groups := scriptPlayGroups(v, units)
	order := append(append([]int{}, groups[0]...), groups[1]...)
	cursor := volcengineScriptCursor{Order: order, Position: len(order), Round: 3, PreviousOrder: []int{2, 0, 1}}
	if err := scriptManualContinue(&cursor, groups[0][0]); err != nil {
		t.Fatal(err)
	}
	if cursor.Position != 1 || cursor.Round != 3 || !reflect.DeepEqual(cursor.Order, order) || !reflect.DeepEqual(cursor.PreviousOrder, []int{2, 0, 1}) {
		t.Fatalf("plan order replaced: %+v", cursor)
	}
	if scriptPlaySkipInSingleMode(v, units, order, cursor.Position) {
		t.Fatal("whole mode skipped following piece")
	}
	v.GenerationContext["recording_script"].(map[string]any)["play_mode"] = "single"
	if !scriptPlaySkipInSingleMode(v, units, order, cursor.Position) {
		t.Fatal("single mode replayed selected group")
	}
	if scriptPlaySkipInSingleMode(v, units, order, 2) {
		t.Fatal("single mode lost following group")
	}
}

func TestManualReplacementReleasesOnlyUnheardReservations(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	path := scriptRoomPolicyPath(recordingCacheMediaRoot(), 14, 29, 4)
	state, err := readScriptRoomPolicy(path, 14, 29, 4)
	if err != nil {
		t.Fatal(err)
	}
	state.Cycles["piece"] = &scriptPolicyCycle{Pending: map[string]string{"replaced": "asset-a", "keep": "asset-b"}, Used: map[string]bool{"heard": true}}
	if err := writeVolcengineJSON(path, state); err != nil {
		t.Fatal(err)
	}
	if err := releaseScriptManualReservations(t.Context(), 14, 29, 4, []string{"replaced"}); err != nil {
		t.Fatal(err)
	}
	after, err := readScriptRoomPolicy(path, 14, 29, 4)
	if err != nil {
		t.Fatal(err)
	}
	cycle := after.Cycles["piece"]
	if _, ok := cycle.Pending["replaced"]; ok || cycle.Pending["keep"] != "asset-b" || !cycle.Used["heard"] {
		t.Fatal("manual jump changed actual history/unrelated reservation")
	}
}
