package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/model"
)

func TestCurrentMainlineSourceUsesPublishedUnitOrder(t *testing.T) {
	v := model.LiveAgentPlanVersion{GenerationContext: map[string]any{
		"content_mode":         model.LiveContentUserAudio,
		"recording_voice_mode": recordingStreamMode,
		"recording_script": map[string]any{
			"version":        2,
			"play_mode":      "full",
			"opening_tab_id": "second", "opening_entry_id": "s1",
			"tabs": []any{
				map[string]any{"tab_id": "first", "title": "第一组", "entries": []any{
					map[string]any{"entry_id": "s1", "title": "到店核销", "text": "今天来到门店继续和大家聊一聊" + strings.Repeat("甲", 500)},
				}},
				map[string]any{"tab_id": "second", "title": "第二组", "entries": []any{
					map[string]any{"entry_id": "s1", "title": "今日福利", "text": "欢迎来到这里" + strings.Repeat("乙", 400)},
				}},
			},
		},
	}}
	v.VoiceIdentity.Model = "qwen-audio-3.1-tts-flash"
	cursor := volcengineScriptCursor{
		ProgramID: "current",
		Delivered: []scriptDeliveredTrack{{Unit: 1, TrackID: "long-17-4"}},
		Pending:   &volcenginePendingTrack{Unit: 0, ID: "long-17-5"},
		Sequence:  5,
	}
	units, err := scriptFirstUnits(v, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ track, group, piece string }{
		{"long-17-4", "第一组", "到店核销"},
		{"long-17-5-raw", "第二组", "今日福利"},
		{"long-17-5-u0-b0", "第二组", "今日福利"},
	} {
		track, found := mainlineTrackDelivery(cursor, 17, tc.track)
		if !found || track.Unit < 0 || track.Unit >= len(units) {
			t.Fatalf("missing delivery for %s", tc.track)
		}
		got := mainlineSourceForUnit(v, units[track.Unit], track)
		if got.GroupName != tc.group || got.PieceName != tc.piece {
			t.Errorf("%s resolved to %#v with unit %d", tc.track, got, track.Unit)
		}
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), "source_preview") || strings.Contains(string(raw), "欢迎来到") {
			t.Fatalf("body preview exposed instead of pool name: %s", raw)
		}
	}
	if got, ok := mainlineTrackDelivery(cursor, 17, "long-17-99"); ok {
		t.Fatalf("unexpected match for missing track: %+v", got)
	}

	for _, tc := range []struct {
		name      string
		audio     volcengineCachedAudio
		recording string
		specified bool
	}{
		{name: "pool"},
		{name: "old generated audio from disabled prompt", audio: volcengineCachedAudio{SpecHash: "old-prompt-hash"}},
		{name: "enabled prompt uses audio from the pool", specified: true},
		{name: "linked human voice", audio: volcengineCachedAudio{HumanVoice: true,
			HumanSource: &mainlineHumanSource{RecordingName: "核销原声"}}, recording: "核销原声", specified: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cursor.Pending.Audio = tc.audio
			track, ok := mainlineTrackDelivery(cursor, 17, "long-17-5")
			if !ok {
				t.Fatal("missing pending delivery")
			}
			unit := units[track.Unit]
			unit.SpecPromptEnabled = tc.specified
			got := mainlineSourceForUnit(v, unit, track)
			if got.GroupName != "第二组" || got.PieceName != "今日福利" ||
				got.RecordingName != tc.recording || got.Specified != tc.specified || got.StandaloneHuman {
				t.Errorf("unexpected source: %+v", got)
			}
		})
	}
}

func TestCurrentMainlineSourceFollowsTheActualHumanDelivery(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	duration := uint64(1500)
	recording := model.MediaAsset{ID: 28, OriginalName: "三个链接.WAV", DurationMS: &duration,
		Metadata: map[string]any{"script_transcript": "1\n00:00:00,000 --> 00:00:01,500\n这里是原始录音。"}}
	audio := humanSourceCachedAudio(recording)
	if audio.HumanSource == nil || audio.HumanSource.RecordingName != "三个链接" || len(audio.Timeline) != 1 {
		t.Fatalf("timed recording lost its source name: %+v", audio)
	}
	combination, err := fissionCachedAudio(81, humanfission.Render{SRT: "1\n00:00:00,000 --> 00:00:01,400\n这是新组合。", Duration: 1400})
	if err != nil {
		t.Fatal(err)
	}
	const tenant, room, session = 7, 12, 17
	cursor := volcengineScriptCursor{VersionID: 23, ProgramID: "program-17", Sequence: 5,
		Delivered: []scriptDeliveredTrack{{Unit: -1, HumanVoice: true, TrackID: "long-17-4", HumanSource: &mainlineHumanSource{
			RecordingName: audio.HumanSource.RecordingName, CombinationName: "砂糖橘与门店核销",
		}}},
		Pending: &volcenginePendingTrack{Unit: -1, ID: "long-17-5", Audio: audio, HumanChoice: "human-source:28"},
	}
	path := scriptSupplyCheckpointPath(tenant, room, session)
	if err := writeVolcengineJSON(path, cursor); err != nil {
		t.Fatal(err)
	}
	if combination.HumanVoice != true {
		t.Fatal("rendered combination did not retain real recording provenance")
	}
	server := &Server{}
	program := mainlineProgramSource{ProgramID: cursor.ProgramID, VersionID: cursor.VersionID, SessionID: "volcengine-long-17"}
	for _, tc := range []struct{ track, piece string }{
		{"long-17-4", "砂糖橘与门店核销"},
		{"long-17-5", ""},
	} {
		program.TrackID = tc.track
		got := server.currentMainlineScriptSource(t.Context(), tenant, room, program)
		if got == nil || got.GroupName != "三个链接" || got.PieceName != tc.piece || got.RecordingName != "" || !got.StandaloneHuman {
			t.Errorf("%s resolved to %+v", tc.track, got)
		}
	}
	program.ProgramID = "other program"
	if got := server.currentMainlineScriptSource(t.Context(), tenant, room, program); got != nil {
		t.Errorf("unexpected source from another program: %+v", got)
	}
}
