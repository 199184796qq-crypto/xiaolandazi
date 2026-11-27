package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestScriptHumanVoiceRollIsPerSelectedOccurrence(t *testing.T) {
	piece := scriptHumanPieceKey("group-1", "entry-2")
	if piece == "" || piece == scriptHumanPieceKey("group-1-entry", "2") || scriptHumanPieceKey("..", "entry") != "" {
		t.Fatal("invalid piece identity")
	}
	var picked [3]int
	for i := 0; i < 30000; i++ {
		occurrence := fmt.Sprintf("auto/%d/2", i)
		roll := scriptHumanRoll(14, 29, 99, 13, piece, occurrence)
		first := roll.Intn(100)
		if first != scriptHumanRoll(14, 29, 99, 13, piece, occurrence).Intn(100) {
			t.Fatal("retry rerolled the same selection")
		}
		if first < 30 {
			picked[roll.Perm(len(picked))[0]]++
		}
	}
	for _, n := range picked {
		if n < 2750 || n > 3250 {
			t.Fatalf("30%% split across 3 recordings: %v", picked)
		}
	}
	v := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"recording_script": map[string]any{"version": 2, "tabs": []any{
		map[string]any{"tab_id": "group-1", "entries": []any{
			map[string]any{"entry_id": "entry-2", "text": strings.Repeat("直播间话术。", 110)},
			map[string]any{"entry_id": "other", "text": strings.Repeat("别的片段。", 110)}}},
	}}}}
	units, err := scriptFirstUnits(v, "")
	if err != nil || len(units) != 2 {
		t.Fatalf("grouped pieces: %d %v", len(units), err)
	}
	for _, unit := range units {
		if strings.HasPrefix(unit.Text, "直播间话术") != (unit.HumanKey == piece) {
			t.Fatalf("opening reorder mismatched recording association: %s", unit.Pool)
		}
	}
}

func TestScriptHumanVoiceRoutesRequireAuthentication(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerScriptHumanVoiceRoutes(mux)
	path := "/api/v1/live-agent-plans/9/script/groups/g/pieces/e/recordings"
	for _, test := range []struct{ method, path string }{
		{"GET", path}, {"POST", path}, {"PUT", path + "/42"}, {"PATCH", path + "/42"}, {"DELETE", path + "/42"}, {"GET", path + "/42/content"},
		{"GET", path + "/42/playback"}, {"POST", path + "/uploads/start"}, {"POST", path + "/uploads/complete"},
	} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: %d", test.method, test.path, response.Code)
		}
	}
}

func TestScriptHumanVoiceLeavesAIPoolAvailable(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{PlanID: 71}
	unit := volcengineScriptUnit{Text: "测试正文", Pool: "piece"}
	const tenant, room int64 = 14, 29
	const instance = "long-9-1"
	audio := volcengineCachedAudio{AssetID: 10, DurationMS: 1000, Text: unit.Text}
	if err := reserveScriptPolicyAudio(root, v, unit, tenant, room, instance, audio); err != nil {
		t.Fatal(err)
	}
	audio.HumanVoice = true
	if err := reserveScriptPolicyAudio(root, v, unit, tenant, room, instance, audio); err != nil {
		t.Fatal(err)
	}
	heard := scriptDeliveredTrack{HumanVoice: true, TrackID: instance, InstanceID: instance, PolicyText: unit.Text,
		PolicyDurationMS: 1000, Block: scriptFirstCachedBlock{DurationMS: 1000}}
	if err := recordScriptPolicyPCM(root, tenant, room, v.PlanID, heard, 1000, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	policy, err := readScriptRoomPolicy(scriptRoomPolicyPath(root, tenant, room, v.PlanID), tenant, room, v.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	cycle := policy.Cycles[scriptCycleKey(v, unit)]
	if len(cycle.Pending) != 0 || len(cycle.Used) != 0 || len(policy.Spans) != 1 {
		t.Fatalf("unexpected AI selection state: %+v", cycle)
	}
}
