package httpapi

import (
	"testing"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/model"
)

func TestHumanFissionNamesKeepManualLabels(t *testing.T) {
	state := humanfission.State{
		Parts:   []humanfission.Part{{Name: "手工改名"}, {}},
		Recipes: []humanfission.Recipe{{Name: "我的点播"}, {}},
	}
	applyHumanFissionNames(&state, []fissionNameRow{{1, "模型1"}, {2, "模型2"}, {2, "重复"}, {3, "不存在"}},
		[]fissionNameRow{{1, "模型1"}, {2, "新版组合"}})
	if state.NeedsNames() || state.Parts[0].Name != "手工改名" || state.Parts[1].Name != "模型2" ||
		state.Recipes[0].Name != "我的点播" || state.Recipes[1].Name != "新版组合" {
		t.Fatalf("invalid label replacement: %#v", state)
	}
}

func TestHumanSourcePlaybackPools(t *testing.T) {
	sources := []db.ScriptHumanVoiceSummary{{ID: 21, Name: "独立原声.MP3"}, {ID: 17, Name: "水果讲解.wav"}}
	recipe := humanfission.Recipe{ID: humanfission.Digest("recipe"), Name: "用券与门店"}
	job := db.HumanFissionJob{Source: 17, State: humanfission.State{Recipes: []humanfission.Recipe{recipe}}}
	groups, legacy := humanScriptGroups(sources, []db.HumanFissionJob{job})
	if len(groups) != 2 || groups[0].Title != "独立原声" || len(groups[0].Pieces) != 1 || len(groups[1].Pieces) != 2 {
		t.Fatalf("incorrect source groups: %#v", groups)
	}
	original, combination := groups[1].Pieces[0], groups[1].Pieces[1]
	if original.ID != "human-source:17" || original.Title != "水果讲解" ||
		combination.ID != fissionChoiceID(job, recipe) || combination.Title != recipe.Name ||
		!scriptStandaloneChoice(original.ID) || !scriptStandaloneChoice(combination.ID) ||
		scriptStandaloneChoice("ai-piece") || len(legacy.Pieces) != 1 || legacy.Pieces[0].ID != combination.ID {
		t.Fatalf("incorrect choices: %#v %#v", groups, legacy)
	}
}

func TestHumanSourcePlaybackAudio(t *testing.T) {
	duration := uint64(1200)
	asset := model.MediaAsset{ID: 17, DurationMS: &duration, Metadata: map[string]any{}}
	for _, tc := range []struct {
		transcript string
		timed      bool
	}{
		{"", false},
		{"校对后的纯文本。", false},
		{"1\n00:00:00,100 --> 00:00:01,000\n鲜果讲解。", true},
		{"1\n00:00:00,100 --> 00:00:03,000\n字幕超出音频。", false},
	} {
		asset.Metadata["script_transcript"] = tc.transcript
		audio := humanSourceCachedAudio(asset)
		if audio.AssetID != 17 || !audio.HumanVoice || audio.DurationMS != 1200 ||
			audio.UntimedTranscript == tc.timed || (len(audio.Timeline) != 0) != tc.timed {
			t.Fatalf("incorrect audio for %q: %#v", tc.transcript, audio)
		}
	}
}

func TestHumanFissionPlaybackIdentityAndContinuation(t *testing.T) {
	job := db.HumanFissionJob{Source: 17, State: humanfission.State{Input: "input", Generation: "first"}}
	recipe := humanfission.Recipe{ID: humanfission.Digest("recipe"), Name: "水果讲解"}
	id := fissionChoiceID(job, recipe)
	source, version, parsed, err := parseFissionChoiceID(id)
	if err != nil || source != 17 || version != job.State.RecipeVersion() || parsed != recipe.ID {
		t.Fatal(source, version, parsed, err)
	}
	recipe.Name = "新名称"
	if fissionChoiceID(job, recipe) != id {
		t.Fatal("renaming changed the choice identity")
	}
	job.State.Generation = "second"
	if fissionChoiceID(job, recipe) == id {
		t.Fatal("clearing and regenerating must invalidate older choices")
	}
	cursor := &volcengineScriptCursor{Order: []int{2, 0, 1}, Previous: 1, Position: 3,
		UnitSelections: []int{1, 2, 3}, Pending: &volcenginePendingTrack{Unit: 1},
		Delivered: []scriptDeliveredTrack{{TrackID: "playing", Unit: 0}, {TrackID: "rendered", Unit: -1}}}
	resumeScriptAfterFission(cursor, scriptFissionContinuation(cursor, "playing"))
	if cursor.Position != 2 || cursor.Previous != 0 || cursor.Pending != nil || cursor.UnitSelections[0] != 1 {
		t.Fatalf("named one-off playback changed the normal pool selection: %#v", cursor)
	}
	audio, err := fissionCachedAudio(17, humanfission.Render{Duration: 1200, SRT: "1\n00:00:00,100 --> 00:00:01,000\n鲜果讲解。"})
	if err != nil || audio.AssetID != 17 || !audio.HumanVoice || len(audio.Timeline) != 1 || audio.Text != "鲜果讲解。" {
		t.Fatal(audio, err)
	}
}
