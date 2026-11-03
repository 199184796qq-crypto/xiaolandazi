package httpapi

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/ttsgateway"
)

func addReadyScriptFixture(t *testing.T, root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, voice string, asset int64) scriptplay.Version {
	t.Helper()
	v.VoiceIdentity.VoiceID = voice
	key := scriptFirstParagraphKey(v, unit.Text)
	dir := filepath.Join(root, "fixtures", key)
	cache := volcengineParagraphCache{Key: key, Audio: map[int]volcengineCachedAudio{100: {AssetID: asset, VoiceID: voice, DurationMS: 90000}}}
	if err := writeVolcengineJSON(filepath.Join(dir, "cache.json"), cache); err != nil {
		t.Fatal(err)
	}
	version := scriptplay.Version{Fingerprint: scriptplay.Fingerprint(unit.Text), VoiceID: voice, Model: v.VoiceIdentity.Model, Kind: scriptplay.KindOriginal, AudioRef: strconv.FormatInt(asset, 10), Text: unit.Text, DurationMS: 90000, SourcePath: dir}
	if err := addScriptVoiceVersion(root, v.PlanID, unit.poolID(), version); err != nil {
		t.Fatal(err)
	}
	return version
}

func TestScriptBudgetAdmissionIsAtomicAndReusesSamePurchase(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS", "100")
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := reserveScriptVoiceSynthesis(7, strconv.Itoa(i), 60); err == nil {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 1 || scriptVoiceBudgetLeft(7) != 40 {
		t.Fatal("concurrent requests overspent", successes.Load(), scriptVoiceBudgetLeft(7))
	}
	release, err := reserveScriptVoiceSynthesis(8, "paid-source", 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserveScriptVoiceSynthesis(8, "paid-source", 60); err != nil || scriptVoiceBudgetLeft(8) != 40 {
		t.Fatal("resume double-charged", err)
	}
	release(true)
	if scriptVoiceBudgetLeft(8) != 100 {
		t.Fatal("definite non-submission did not release")
	}
	release, err = reserveScriptVoiceSynthesis(8, "uncertain", 60)
	if err != nil {
		t.Fatal(err)
	}
	release(false)
	if _, err := reserveScriptVoiceSynthesis(8, "next", 50); !errors.Is(err, errScriptVoiceBudget) {
		t.Fatal("unknown supplier outcome must retain cost", err)
	}
}

func TestScriptBudgetRollingWindowAndCorruption(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS", "100")
	now := time.Now()
	state := scriptVoiceBudgetState{WindowStart: now.Add(-2 * time.Hour).Format(time.RFC3339), Chars: 100, Purchases: map[string]scriptVoicePurchase{"old": {At: now.Add(-61 * time.Minute), Chars: 60}, "recent": {At: now.Add(-2 * time.Minute), Chars: 40}}}
	if err := writeVolcengineJSON(scriptVoiceBudgetPath(9), state); err != nil {
		t.Fatal(err)
	}
	if left := scriptVoiceBudgetLeft(9); left != 60 {
		t.Fatal("rolling window reset recent purchases", left)
	}
	if err := os.WriteFile(scriptVoiceBudgetPath(9), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveScriptVoiceSynthesis(9, "new", 1); err == nil {
		t.Fatal("corrupt budget failed open")
	}
}

func TestScriptBudgetDeclineDoesNotPoisonPaidIntent(t *testing.T) {
	unit := volcengineScriptUnit{Key: "test", Dir: t.TempDir()}
	_, err := prepareScriptFirstPaid(t.Context(), unit, func(context.Context, string) (ttsgateway.SynthesizeResponse, error) {
		return ttsgateway.SynthesizeResponse{}, scriptBudgetAdmissionError{errScriptVoiceBudget}
	})
	if !errors.Is(err, errScriptVoiceBudget) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(unit.Dir, "paragraph-job.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local refusal left an uncertain paid job", err)
	}
}

func TestScriptPoolValidatesActualCacheAndSkipsBrokenCandidate(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{PlanID: 10, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "A", Rate: 1}}
	unit := volcengineScriptUnit{Pool: "e-tag-entry", Key: scriptFirstParagraphKey(v, "原文"), Text: "原文"}
	version := addReadyScriptFixture(t, root, v, unit, "A", 123)
	if _, ok := scriptVersionAudio(root, unit, v, version); !ok {
		t.Fatal("valid paid version rejected")
	}
	for _, bad := range []scriptplay.Version{
		{VoiceID: "A", Model: "other", Text: version.Text, SourcePath: version.SourcePath, AudioRef: "123"},
		{VoiceID: "A", Model: version.Model, Text: version.Text, SourcePath: version.SourcePath, AudioRef: "999"},
		{VoiceID: "B", Model: version.Model, Text: version.Text, SourcePath: version.SourcePath, AudioRef: "123"},
		{VoiceID: "A", Model: version.Model, Text: "被改过的字幕", SourcePath: version.SourcePath, AudioRef: "123"},
	} {
		if _, ok := scriptVersionAudio(root, unit, v, bad); ok {
			t.Fatal("mismatched metadata used", bad)
		}
	}
	broken := version
	broken.AudioRef = "999"
	broken.SourcePath = filepath.Join(root, "absent")
	broken.ID = "broken"
	if err := addScriptVoiceVersion(root, v.PlanID, unit.poolID(), broken); err != nil {
		t.Fatal(err)
	}
	// Edited wording keeps the paid variant, exactly as the new stable-ID design.
	unit.Text = "修改后的原话"
	audio, text, ok := scriptCompatiblePoolAudio(root, v, unit, "session1", rand.New(rand.NewSource(1)))
	if !ok || audio.AssetID != 123 || text != "原文" {
		t.Fatal("broken candidate blocked a valid paid variant", audio, text, ok)
	}
}

func TestScriptActualPlaybackCountsOnceAcrossCheckpointRetry(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root := recordingCacheMediaRoot()
	v := model.LiveAgentPlanVersion{PlanID: 11, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "A", Rate: 1}}
	unit := volcengineScriptUnit{Pool: "e-entry", Text: "原文"}
	addReadyScriptFixture(t, root, v, unit, "A", 123)
	cursor := volcengineScriptCursor{Delivered: []scriptDeliveredTrack{{Unit: 0, TrackID: "long-99-1", InstanceID: "long-99-1", AssetID: 123, VoiceID: "A", Model: v.VoiceIdentity.Model, Block: scriptFirstCachedBlock{AssetID: 123, Text: "原文", DurationMS: 90000}}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // never call a model in this test
	s := &Server{}
	save := func() error { return nil }
	queued := coreclient.ProgramRefreshSnapshot{TrackID: "long-99-1", CurrentMS: 200}
	if err := s.confirmScriptPlayback(ctx, 14, 29, 99, v, []volcengineScriptUnit{unit}, &cursor, queued, save); err != nil {
		t.Fatal(err)
	}
	pool, _ := loadScriptVoicePool(root, v.PlanID, unit.poolID())
	if pool.Versions[0].PlayCount != 0 {
		t.Fatal("queue position mistaken for accepted PCM")
	}
	actual := queued
	actual.MainlinePlaybackReceipts = []anchorcycle.AnglePlaybackReceipt{{TrackID: "long-99-1", StartedAt: time.Now().Format(time.RFC3339)}}
	if err := s.confirmScriptPlayback(ctx, 14, 29, 99, v, []volcengineScriptUnit{unit}, &cursor, actual, save); err != nil {
		t.Fatal(err)
	}
	cursor.Delivered[0].Confirmed = false // process crashed before its cursor save
	if err := s.confirmScriptPlayback(ctx, 14, 29, 99, v, []volcengineScriptUnit{unit}, &cursor, actual, save); err != nil {
		t.Fatal(err)
	}
	pool, _ = loadScriptVoicePool(root, v.PlanID, unit.poolID())
	if pool.Versions[0].PlayCount != 1 {
		t.Fatal("receipt retry counted twice", pool)
	}
	ledger, err := os.ReadFile(filepath.Join(root, "playback-ledger", "14-29.jsonl"))
	if err != nil || strings.Count(string(ledger), "\n") != 1 {
		t.Fatal("ledger repeated", err, string(ledger))
	}
}

func TestScriptRewriteExhaustedBudgetReturnsWithoutUnlockPanic(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS", "1")
	v := model.LiveAgentPlanVersion{PlanID: 12, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "A", Rate: 1}}
	unit := volcengineScriptUnit{Pool: "p", Text: "测试正文"}
	addReadyScriptFixture(t, recordingCacheMediaRoot(), v, unit, "A", 123)
	(&Server{}).maybeGenerateRewriteVersion(t.Context(), 14, 29, v, unit)
	// No store/gateway configured: reaching a model call would also fail this test.
}

func TestScriptDuplicateTextKeepsDistinctEntryGroups(t *testing.T) {
	v := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"recording_script": recordingScript{Version: 2, Tabs: []recordingScriptTab{
		{TabID: "a", Entries: []recordingScriptEntry{{EntryID: "s1", Text: longText("相同文案")}}},
		{TabID: "b", Entries: []recordingScriptEntry{{EntryID: "s1", Text: longText("相同文案")}}},
	}}}}
	units, err := scriptFirstUnits(v, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	groups := scriptPlayGroups(v, units)
	if len(groups) != 2 || len(groups[0]) != 1 || len(groups[1]) != 1 || groups[0][0] == groups[1][0] {
		t.Fatal("identical text merged separate tags", groups)
	}
}

func TestScriptSingleModeVisitsAllPiecesBeforeRepeating(t *testing.T) {
	v, units := playFixture(model.LiveAgentPlanVersion{})
	script := v.GenerationContext["recording_script"].(recordingScript)
	script.PlayMode = "single"
	v.GenerationContext["recording_script"] = script
	counts := make([]int, len(units))
	previous := -1
	var old []int
	rng := rand.New(rand.NewSource(30))
	for round := 0; round < 12; round++ {
		order := scriptPlayRoundOrder(v, units, round, previous, old, rng)
		order = scriptSingleSelectionOrder(v, units, order, counts, previous)
		for pos, index := range order {
			if !scriptPlaySkipInSingleMode(v, units, order, pos) {
				counts[index]++
				previous = index
			}
		}
		old = order
		for _, group := range scriptPlayGroups(v, units) {
			low, high := counts[group[0]], counts[group[0]]
			for _, index := range group {
				low = min(low, counts[index])
				high = max(high, counts[index])
			}
			if high-low > 1 {
				t.Fatal("single-select repeated before visiting all", counts)
			}
		}
	}
}

func TestScriptVersionTotalGuardAndHanEstimate(t *testing.T) {
	if cap := scriptVersionCapFromUnits(unitsWithChars(40, 900)); cap*40 > 80 {
		t.Fatal("total guard overridden by minimum", cap)
	}
	a := scriptVersionCapFromUnits([]volcengineScriptUnit{{Text: strings.Repeat("汉", 4000)}})
	b := scriptVersionCapFromUnits([]volcengineScriptUnit{{Text: strings.Repeat("汉", 4000) + strings.Repeat("，0123", 4000)}})
	if a != b {
		t.Fatal("punctuation changed the Han duration estimate", a, b)
	}
}

func TestScriptRecoveryBindsSessionVersionAndPendingStart(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	path := scriptSupplyCheckpointPath(14, 29, 99)
	cursor := volcengineScriptCursor{VersionID: 322, Signature: "source", ProgramID: "program-old", Order: []int{0, 1}, Position: 1, Sequence: 8, ActiveVoice: "B", VoiceBlockLeft: 3, UnitSelections: []int{1, 0}, Pending: &volcenginePendingTrack{Unit: 1, ID: "long-99-8", Audio: volcengineCachedAudio{AssetID: 77, Text: "实际改写正文"}}}
	if err := writeVolcengineJSON(path, cursor); err != nil {
		t.Fatal(err)
	}
	snapshot := coreclient.ProgramRefreshSnapshot{Running: true, VersionID: 322, ProgramID: "program-old"}
	snapshot.Task.SessionID = "volcengine-long-99"
	if id, ok := recoverScriptSupplyCheckpoint(14, 29, 99, 322, snapshot); !ok || id != "program-old" {
		t.Fatal("valid old-version checkpoint not resumed")
	}
	if _, ok := recoverScriptSupplyCheckpoint(14, 29, 99, 323, snapshot); ok {
		t.Fatal("latest published version replaced live version")
	}
	snapshot.Task.SessionID = "volcengine-long-100"
	if _, ok := recoverScriptSupplyCheckpoint(14, 29, 99, 322, snapshot); ok {
		t.Fatal("another session reused checkpoint")
	}
	snapshot.Task.SessionID = "volcengine-long-99"
	cursor.ProgramID = ""
	snapshot.TrackID = "long-99-8-raw"
	if err := writeVolcengineJSON(path, cursor); err != nil {
		t.Fatal(err)
	}
	if _, ok := recoverScriptSupplyCheckpoint(14, 29, 99, 322, snapshot); !ok {
		t.Fatal("lost first start acknowledgement could not reconcile")
	}
	var restored volcengineScriptCursor
	if err := readVolcengineJSON(path, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Position != 1 || restored.Pending.ID != "long-99-8" || restored.Pending.Audio.Text != "实际改写正文" || restored.ActiveVoice != "B" || restored.VoiceBlockLeft != 3 || restored.UnitSelections[0] != 1 {
		t.Fatal("recovery changed progress or caption", restored)
	}
	if _, ok := recoverScriptSupplyCheckpoint(14, 29, 100, 322, snapshot); ok {
		t.Fatal("missing checkpoint replayed first round")
	}
}

func TestScriptRewriteSnapshotKeepsPromptAndRejectsChangedFactsReuse(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{PlanID: 13, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "A", Rate: 1}}
	v = withScriptRewriteSnapshot(v, scriptRewriteSnapshot{Prompt: "事实：87元。样本：{{样本话术}}" + scriptOpeningBlockMarker + scriptTimeRuleMarker, Facts: "87元"})
	unit := volcengineScriptUnit{Pool: "e-piece", Text: "原文87元"}
	version := addReadyScriptFixture(t, root, v, unit, "A", 456)
	version.Kind = scriptplay.KindRewrite
	version.FactsKey = scriptRewriteFactsKey(v)
	if _, ok := scriptVersionAudio(root, unit, v, version); !ok {
		t.Fatal("matching facts rejected")
	}
	changed := withScriptRewriteSnapshot(v, scriptRewriteSnapshot{Prompt: "新事实", Facts: "99元"})
	if _, ok := scriptVersionAudio(root, unit, changed, version); ok {
		t.Fatal("paid old-price rewrite crossed facts boundary")
	}
	unit.Text = "措辞变化87元"
	if _, ok := scriptVersionAudio(root, unit, v, version); !ok {
		t.Fatal("wording change discarded compatible paid version")
	}
	prompt, err := (&Server{}).scriptRewritePrompt(t.Context(), 14, v, "这份样本", "开场", "方式")
	if err != nil || !strings.Contains(prompt, "事实：87元。样本：这份样本") || strings.Contains(prompt, "99元") {
		t.Fatal("frozen prompt read mutable store", err, prompt)
	}
}

func TestScriptOriginalCacheHitDoesNotRetireRewrite(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{PlanID: 14, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen-audio-3.1-tts-flash", VoiceID: "A", Rate: 1}}
	unit := volcengineScriptUnit{Pool: "e-piece", Text: "原文"}
	version := addReadyScriptFixture(t, root, v, unit, "A", 789)
	if added, err := commitScriptRewriteVersion(root, v, unit, version, 1, true); err != nil || added {
		t.Fatal("cache replay treated as new version", added, err)
	}
	pool, _ := loadScriptVoicePool(root, v.PlanID, unit.poolID())
	if len(pool.Versions) != 1 || pool.Versions[0].Kind != scriptplay.KindOriginal {
		t.Fatal("original retired on duplicate response")
	}
}
