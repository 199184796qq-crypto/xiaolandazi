package httpapi

import (
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

func TestScriptMainlinePreferenceIsRoomScopedAndPersistent(t *testing.T) {
	root := t.TempDir()
	path := scriptMainlineModePath(root, 7, 12)
	otherRoom := scriptMainlineModePath(root, 7, 13)
	assertMode := func(path, want string) {
		t.Helper()
		if got, err := readScriptMainlineMode(path); err != nil || got != want {
			t.Fatalf("mode %q, want %q, error %v", got, want, err)
		}
	}
	assertMode(path, scriptMainlineAI)
	if err := writeScriptMainlineMode(t.Context(), path, scriptMainlineHuman); err != nil {
		t.Fatal(err)
	}
	assertMode(path, scriptMainlineHuman)
	assertMode(otherRoom, scriptMainlineAI)
	if err := writeScriptMainlineMode(t.Context(), path, scriptMainlineAI); err != nil {
		t.Fatal(err)
	}
	assertMode(path, scriptMainlineAI)
}

func TestScriptHumanPoolCombinesOriginalsAndRecipesWithoutAdjacentRepeat(t *testing.T) {
	rng := rand.New(rand.NewSource(19))
	fission := "fission:1:" + strings.Repeat("a", 64) + ":" + strings.Repeat("b", 64)
	groups := []scriptManualGroup{
		{Pieces: []scriptManualPiece{{ID: "human-source:1"}, {ID: fission}}},
		{Pieces: []scriptManualPiece{{ID: "human-source:2"}}},
		{Pieces: []scriptManualPiece{{ID: "human-source:3"}}},
	}
	previous := scriptHumanChoiceKey(strings.Replace(fission, strings.Repeat("a", 64), strings.Repeat("c", 64), 1))
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		order := scriptHumanChoiceOrder(groups, previous, rng)
		if len(order) != 4 || scriptHumanChoiceKey(order[0].ID) == previous || scriptHumanChoiceKey(order[3].ID) != previous {
			t.Fatalf("incorrect order after %q: %#v", previous, order)
		}
		seen[order[0].ID] = true
		previous = scriptHumanChoiceKey(order[0].ID)
	}
	if len(seen) != 4 {
		t.Fatalf("not all pool entries can play: %v", seen)
	}
	only := []scriptManualGroup{{Pieces: []scriptManualPiece{{ID: "human-source:1"}}}}
	order := scriptHumanChoiceOrder(only, "human-source:1", rng)
	if len(order) != 1 || order[0].ID != "human-source:1" || len(scriptHumanChoiceOrder(nil, "", rng)) != 0 {
		t.Fatal("one playable entry must repeat and an empty pool must wait", order)
	}
}

func TestScriptHumanPendingCannotAdvanceAIPlaylist(t *testing.T) {
	cursor := volcengineScriptCursor{VersionID: 9, Signature: "v", Order: []int{0}, GroupedOrder: true}
	cursor.Pending = &volcenginePendingTrack{Unit: -1, ID: "long-1-3", HumanChoice: "human-source:1",
		Audio: volcengineCachedAudio{AssetID: 100, HumanVoice: true}}
	if !validVolcengineCursor(cursor, 9, "v", 1) {
		t.Fatal("valid human append must survive checkpoint recovery")
	}
	cursor.Pending.Advance = true
	if validVolcengineCursor(cursor, 9, "v", 1) {
		t.Fatal("human append cannot advance the AI playlist")
	}
}

func TestScriptMainlineRadioCannotChangeActiveSessionMode(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	server := &Server{}
	path := scriptMainlineModePath(recordingCacheMediaRoot(), 7, 12)
	if err := writeScriptMainlineMode(t.Context(), path, scriptMainlineHuman); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		session int64
		body    string
		status  int
		mode    string
	}{
		{120, `{"session_id":120,"version_id":9,"program_id":"current","mainline_mode":"ai"}`, http.StatusConflict, scriptMainlineHuman},
		{0, `{"session_id":0,"version_id":9,"program_id":"current","mainline_mode":"ai"}`, http.StatusOK, scriptMainlineAI},
	} {
		req := httptest.NewRequest(http.MethodPatch, "/script-playback", strings.NewReader(tc.body))
		reply := httptest.NewRecorder()
		server.updateRoomMainlineMode(reply, req, t.Context(), 7, 12, scriptManualOptions{
			SessionID: tc.session, VersionID: 9, ProgramID: "current", MainlineMode: scriptMainlineHuman, ModeEditable: true,
		})
		got, err := readScriptMainlineMode(path)
		if reply.Code != tc.status || err != nil || got != tc.mode {
			t.Fatalf("session %d: response %d, mode %q, err %v", tc.session, reply.Code, got, err)
		}
	}
}

func TestScriptManualModeUsesTheSelectedPiecePool(t *testing.T) {
	for piece, want := range map[string]string{
		"human-source:19":     scriptMainlineHuman,
		"fission:19:recipe":   scriptMainlineHuman,
		"configured-ai-piece": scriptMainlineAI,
	} {
		if got := scriptManualMode(piece); got != want {
			t.Errorf("piece %q set mode %q, want %q", piece, got, want)
		}
	}
}

func TestScriptPlayingChoiceFollowsCoreTrackRatherThanNewestDelivery(t *testing.T) {
	tracks := []scriptDeliveredTrack{
		{TrackID: "old", PieceID: "completed", Unit: -1, Completed: true},
		{TrackID: "current", PieceID: "fission:combination", Unit: -1, HumanVoice: true},
		{TrackID: "future", PieceID: "human-source:22", Unit: -1, HumanVoice: true},
		{TrackID: "linked", Unit: 0, HumanVoice: true, AssetID: 431},
		{TrackID: "ai", Unit: 1},
	}
	units := []volcengineScriptUnit{{Pool: "fruit"}, {Key: "legacy-unit"}}
	for _, tc := range []struct {
		track, piece string
		recording    int64
	}{
		{"current", "fission:combination", 0},
		{"future", "human-source:22", 0},
		{"linked", "fruit", 431},
		{"ai", "legacy-unit", 0},
		{"old", "", 0},
		{"silence", "", 0},
	} {
		if piece, recording := scriptPlayingChoice(tracks, units, tc.track); piece != tc.piece || recording != tc.recording {
			t.Errorf("track %q returned (%q, %d), want (%q, %d)", tc.track, piece, recording, tc.piece, tc.recording)
		}
	}
}

func TestScriptMainlineChangesOnlyAfterCoreAppliesSelectedPiece(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	const tenant, room, session, version = 7, 12, 120, 9
	modePath := scriptMainlineModePath(recordingCacheMediaRoot(), tenant, room)
	manualPath := scriptManualPath(tenant, room, session)
	server := &Server{}
	for _, tc := range []struct{ piece, before, after string }{
		{"human-source:19", scriptMainlineAI, scriptMainlineHuman},
		{"ai-piece", scriptMainlineHuman, scriptMainlineAI},
	} {
		if err := writeScriptMainlineMode(t.Context(), modePath, tc.before); err != nil {
			t.Fatal(err)
		}
		command := scriptManualJump{RequestID: "jump-" + tc.piece, PieceID: tc.piece,
			SessionID: session, VersionID: version, ProgramID: "active"}
		submitted, err := submitScriptManual(t.Context(), manualPath, command)
		if err != nil {
			t.Fatal(err)
		}
		command = *submitted
		if err := updateScriptManual(t.Context(), manualPath, command, "waiting", ""); err != nil {
			t.Fatal(err)
		}
		cursor := &volcengineScriptCursor{ManualJump: &scriptManualStage{
			Command: command, Applied: true, LeasesConfirmed: true,
			Input: coreclient.ProgramRefreshInput{JobID: command.RequestID, ValidUntil: time.Now().Add(time.Minute)},
		}}
		p := coreclient.ProgramRefreshSnapshot{Running: true, Sequential: true, ManualSelectionSupported: true,
			ProgramID: "active", VersionID: version, PendingJobID: command.RequestID}
		p.Task.SessionID = "volcengine-long-120"
		tick := func() {
			t.Helper()
			handled, err := server.processScriptManual(t.Context(), tenant, room, session, model.LiveAgentPlanVersion{ID: version},
				nil, cursor, p, func() error { return nil })
			if !handled || err != nil {
				t.Fatalf("safe switch: handled=%v, %v", handled, err)
			}
		}
		tick()
		if mode, err := readScriptMainlineMode(modePath); err != nil || mode != tc.before {
			t.Fatalf("mode changed while waiting for Core: %q %v", mode, err)
		}
		p.PendingJobID, p.LastAppliedJobID = "", command.RequestID
		tick()
		if mode, err := readScriptMainlineMode(modePath); err != nil || mode != tc.after || cursor.ManualJump != nil {
			t.Fatalf("mode not saved after Core acknowledgement: %q %v", mode, err)
		}
	}
}
