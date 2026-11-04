package httpapi

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

func policyFixture(t *testing.T, root string, v model.LiveAgentPlanVersion, u volcengineScriptUnit, asset int64, text string, duration int64, kind scriptplay.Kind) scriptplay.Version {
	t.Helper()
	key := scriptFirstParagraphKey(v, text)
	dir := filepath.Join(root, "fixtures", key)
	if err := writeVolcengineJSON(filepath.Join(dir, "cache.json"), volcengineParagraphCache{Key: key, Audio: map[int]volcengineCachedAudio{100: {AssetID: asset, VoiceID: v.VoiceIdentity.VoiceID, DurationMS: duration}}}); err != nil {
		t.Fatal(err)
	}
	x := scriptplay.Version{PieceID: u.poolID(), Fingerprint: scriptplay.Fingerprint(text), Text: text, Kind: kind, VoiceID: v.VoiceIdentity.VoiceID, Model: v.VoiceIdentity.Model, AudioRef: strconv.FormatInt(asset, 10), DurationMS: duration, SourcePath: dir, FactsKey: scriptRewriteFactsKey(v)}
	if err := addScriptVoiceVersion(root, v.PlanID, u.poolID(), x); err != nil {
		t.Fatal(err)
	}
	return x
}
func policyVersion() model.LiveAgentPlanVersion {
	return model.LiveAgentPlanVersion{TenantID: 14, PlanID: 4, ID: 195, VoiceIdentity: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "qwen-audio-3.1-tts-flash", Rate: 1}}
}
func policyConsume(t *testing.T, root string, v model.LiveAgentPlanVersion, a volcengineCachedAudio, room, session int64, seq int, at time.Time) {
	t.Helper()
	id := fmt.Sprintf("long-%d-%d", session, seq)
	track := scriptDeliveredTrack{TrackID: id, InstanceID: id, PolicyText: a.Text, PolicyDurationMS: a.DurationMS, Block: scriptFirstCachedBlock{DurationMS: a.DurationMS}}
	if err := recordScriptPolicyPCM(root, 14, room, 4, track, a.DurationMS, at.Format(time.RFC3339Nano), at.Add(time.Duration(a.DurationMS)*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyRepeatWindowClipsOffsetsAndCannotDiluteWithSilence(t *testing.T) {
	minute := int64(60000)
	s := []scriptPCMSpan{{Content: "A", Start: 0, End: 10 * minute, To: 1e9}, {Content: "B", Start: 10 * minute, End: 70 * minute, To: 1e9}, {Content: "A", Start: 70 * minute, End: 80 * minute, To: 1e9}}
	if got := scriptRepeatRate(s, 80*minute); math.Abs(got-.125) > 1e-9 {
		t.Fatal(got)
	}
	// At minute95 the first five minutes have expired: only half of A repeats.
	if got := scriptRepeatRate(s, 95*minute); math.Abs(got-5.0/75) > 1e-9 {
		t.Fatal(got)
	}
	s = []scriptPCMSpan{{Content: "same text", Start: 0, End: minute, To: 1e9}, {Content: "same text", Start: minute, End: 3 * minute, To: 1e9}}
	if got := scriptRepeatRate(s, 3*minute); math.Abs(got-2.0/3) > 1e-9 {
		t.Fatal("different voice duration escaped repetition", got)
	}
	if got := scriptRepeatRate(s, 20*minute); math.Abs(got-2.0/3) > 1e-9 {
		t.Fatal("silence diluted denominator", got)
	}
}

func TestPolicyReservationsRetriesNewFillAndRoomIsolation(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "piece", Text: "原文"}
	rng := rand.New(rand.NewSource(42))
	at := time.Now().UTC()
	policyFixture(t, root, v, u, 1, u.Text, 90000, scriptplay.KindOriginal)
	policyFixture(t, root, v, u, 2, "第二个表述", 90000, scriptplay.KindRewrite)
	a, ok, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, "long-99-1", 0, rng, nil, at)
	if err != nil || !ok {
		t.Fatal(err)
	}
	retry, _, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, "long-99-1", 0, rng, nil, at)
	if err != nil || retry.AssetID != a.AssetID {
		t.Fatal("retry changed paid asset", err)
	}
	path := scriptRoomPolicyPath(root, 14, 29, 4)
	st, _ := readScriptRoomPolicy(path, 14, 29, 4)
	for _, c := range st.Cycles {
		if len(c.Used) != 0 {
			t.Fatal("selection counted as playback")
		}
	}
	policyConsume(t, root, v, a, 29, 99, 1, at)
	b, _, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, "long-99-2", 0, rng, nil, at.Add(90*time.Second))
	if err != nil || b.AssetID == a.AssetID {
		t.Fatal("repeat before coverage", err)
	}
	policyConsume(t, root, v, b, 29, 99, 2, at.Add(90*time.Second))
	policyFixture(t, root, v, u, 3, "灌池期间新加入", 90000, scriptplay.KindRewrite)
	c, _, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, "long-99-3", 0, rng, nil, at.Add(180*time.Second))
	if err != nil || c.AssetID != 3 {
		t.Fatal("new fill waited for reset", c.AssetID, err)
	}
	other, err := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 30, 4), 14, 30, 4)
	if err != nil || len(other.Spans) > 0 || len(other.Cycles) > 0 {
		t.Fatal("room history leaked")
	}
	changed := v
	changed.VoiceIdentity.Rate = 1.05
	if _, ok, err := scriptPolicyPoolAudio(root, changed, u, 14, 29, 99, "long-99-4", 0, rng, nil, at); err != nil || ok {
		t.Fatal("wrong rate cache accepted", err)
	}
}

func TestPolicyLatePCMIsMonotonicAndIdempotent(t *testing.T) {
	root := t.TempDir()
	at := time.Now().UTC()
	track := scriptDeliveredTrack{TrackID: "long-1-1", InstanceID: "long-1-1", PolicyText: "同一正文", PolicyDurationMS: 60000, Block: scriptFirstCachedBlock{DurationMS: 60000}}
	for _, ms := range []int64{10000, 10000, 5000, 60000, 60000} {
		if err := recordScriptPolicyPCM(root, 14, 29, 4, track, ms, at.Format(time.RFC3339Nano), at.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
	if err != nil || st.Progress[track.TrackID].Played != 60000 || st.RepeatRate != 0 {
		t.Fatal(st, err)
	}
}

func TestPolicyMeasuredCapacityAndLargeOriginalReuse(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "piece", Text: strings.Repeat("字", 400)}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	p := scriptPlanPools(root, v, []volcengineScriptUnit{u})
	if p.Caps[0] != 64 {
		t.Fatal("400-character pool still capped at20", p.Caps)
	}
	policyFixture(t, root, v, u, 2, "短改写", 60000, scriptplay.KindRewrite)
	p = scriptPlanPools(root, v, []volcengineScriptUnit{u})
	if p.Caps[0] <= 64 {
		t.Fatal("short audio did not enlarge pool", p.Caps)
	}
	root = t.TempDir()
	var units []volcengineScriptUnit
	for i := 0; i < 20; i++ {
		u := volcengineScriptUnit{Pool: strconv.Itoa(i), Text: fmt.Sprintf("长原文%d", i)}
		units = append(units, u)
		policyFixture(t, root, v, u, int64(i+1), u.Text, 16*60000, scriptplay.KindOriginal)
	}
	at := time.Now().UTC()
	i, _, _, err := scriptPolicyGeneration(root, v, units, at, false)
	if err != nil || i >= 0 {
		t.Fatal("warm originals unnecessarily purchased on startup", i, err)
	}
	i, _, _, err = scriptPolicyGeneration(root, v, units, at.Add(91*time.Minute), false)
	if err != nil || i < 0 {
		t.Fatal("protected originals blocked refresh forever", i, err)
	}
}

func TestPolicyFullPoolBatchRequiresFourPCMAndCarriesFraction(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	units := []volcengineScriptUnit{u}
	for i := 0; i < 5; i++ {
		kind := scriptplay.KindRewrite
		text := fmt.Sprintf("变化%d", i)
		if i == 0 {
			kind = scriptplay.KindOriginal
			text = u.Text
		}
		policyFixture(t, root, v, u, int64(i+1), text, 16*60000, kind)
	}
	at := time.Now().UTC()
	i, _, _, err := scriptPolicyGeneration(root, v, units, at, false)
	if err != nil || i >= 0 {
		t.Fatal("unplayed pool refreshed", i, err)
	}
	pool, _ := loadScriptVoicePool(root, 4, u.poolID())
	for j := range pool.Versions {
		pool.Versions[j].CompleteCount = 4
		pool.Versions[j].PlayCount = 4
	}
	if err := saveScriptVoicePool(root, 4, u.poolID(), pool); err != nil {
		t.Fatal(err)
	}
	i, _, ref, err := scriptPolicyGeneration(root, v, units, at, false)
	if err != nil || i != 0 || ref == "1" {
		t.Fatal("batch missing or retired original", i, ref, err)
	}
	var st scriptPoolPolicyState
	readVolcengineJSON(scriptPoolPolicyPath(root, v), &st)
	if len(st.Retire) != 1 || st.Carry != 50 {
		t.Fatal("first 5-slot batch", st)
	}
	// A completed batch is recognized from the active inventory, not attempts.
	retired := st.Retire[0].Ref
	kept := pool.Versions[:0]
	for _, x := range pool.Versions {
		if x.AudioRef != retired {
			kept = append(kept, x)
		}
	}
	pool.Versions = kept
	saveScriptVoicePool(root, 4, u.poolID(), pool)
	x := policyFixture(t, root, v, u, 6, "替换后文本", 16*60000, scriptplay.KindRewrite)
	pool, _ = loadScriptVoicePool(root, 4, u.poolID())
	for j := range pool.Versions {
		if pool.Versions[j].AudioRef == x.AudioRef {
			pool.Versions[j].PlayCount = 4
			pool.Versions[j].CompleteCount = 4
		}
	}
	saveScriptVoicePool(root, 4, u.poolID(), pool)
	scriptPolicyGeneration(root, v, units, at, false)
	readVolcengineJSON(scriptPoolPolicyPath(root, v), &st)
	if len(st.Retire) != 2 || st.Carry != 0 {
		t.Fatal("30 percent remainder lost", st)
	}
}

func TestPolicyFormalSelectorMature400CharactersRollingWindow(t *testing.T) {
	for _, seed := range []int64{4, 19, 91} {
		t.Run(strconv.FormatInt(seed, 10), func(t *testing.T) {
			root := t.TempDir()
			v := policyVersion()
			u := volcengineScriptUnit{Pool: "one", Text: "原始正文"}
			at := time.Now().UTC()
			rng := rand.New(rand.NewSource(seed))
			for i := 0; i < 64; i++ {
				text := fmt.Sprintf("四百字的完整不同话术版本%d", i)
				kind := scriptplay.KindRewrite
				if i == 0 {
					text = u.Text
					kind = scriptplay.KindOriginal
				}
				policyFixture(t, root, v, u, int64(i+1), text, 75000, kind)
			}
			used := map[int64]bool{}
			worst := 0.0
			for n := 1; n <= 192; n++ {
				a, ok, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, fmt.Sprintf("long-99-%d", n), 0, rng, nil, at)
				if err != nil || !ok {
					t.Fatal(n, err)
				}
				if used[a.AssetID] {
					t.Fatal("version repeated before complete coverage", n, a.AssetID)
				}
				used[a.AssetID] = true
				if len(used) == 64 {
					used = map[int64]bool{}
				}
				policyConsume(t, root, v, a, 29, 99, n, at)
				at = at.Add(75 * time.Second)
				st, err := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
				if err != nil {
					t.Fatal(err)
				}
				worst = math.Max(worst, st.RepeatRate)
			}
			if worst > .2+1e-9 {
				t.Fatal("formal mature selector violated20%", worst)
			}
			t.Logf("actual selector seed=%d rolling90 worst=%.4f", seed, worst)
		})
	}
}

func TestPolicyFormalSelectorColdFill400Characters(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "one", Text: strings.Repeat("字", 400)}
	at := time.Now().UTC()
	begin := at
	rng := rand.New(rand.NewSource(7))
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	policyFixture(t, root, v, u, 2, "开播前准备好的第一版", 75000, scriptplay.KindRewrite)
	nextAsset := int64(3)
	worst := float64(0)
	for n := 1; n <= 80; n++ {
		// ~80 minutes of unique supply over75 minutes, using no real model or TTS.
		wanted := min(64, 2+int(at.Sub(begin).Seconds()/70.3125))
		for int(nextAsset) <= wanted {
			policyFixture(t, root, v, u, nextAsset, fmt.Sprintf("灌池新增版本%d", nextAsset), 75000, scriptplay.KindRewrite)
			nextAsset++
		}
		a, ok, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, fmt.Sprintf("long-99-%d", n), 0, rng, nil, at)
		if err != nil || !ok {
			t.Fatal(n, err)
		}
		policyConsume(t, root, v, a, 29, 99, n, at)
		at = at.Add(75 * time.Second)
		st, _ := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
		worst = math.Max(worst, st.RepeatRate)
	}
	if worst > .2+1e-9 {
		t.Fatal("cold fill repeat target", worst)
	}
	t.Logf("actual selector cold400 rolling90 worst=%.4f", worst)
}

func TestPolicyRetirementKeepsRoomLeaseUntilRoundEnd(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	rng := rand.New(rand.NewSource(3))
	at := time.Now().UTC()
	policyFixture(t, root, v, u, 1, u.Text, 90000, scriptplay.KindOriginal)
	policyFixture(t, root, v, u, 2, "旧改写", 90000, scriptplay.KindRewrite)
	a, _, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, "long-99-1", 0, rng, nil, at)
	if err != nil || a.AssetID != 1 {
		t.Fatal(err)
	}
	policyConsume(t, root, v, a, 29, 99, 1, at)
	// Another room completed enough plays and replaced the shared active entry.
	pool, _ := loadScriptVoicePool(root, 4, u.poolID())
	pool.Versions = pool.Versions[:1]
	if err = saveScriptVoicePool(root, 4, u.poolID(), pool); err != nil {
		t.Fatal(err)
	}
	policyFixture(t, root, v, u, 3, "新替换的改写", 90000, scriptplay.KindRewrite)
	b, _, err := scriptPolicyPoolAudio(root, v, u, 14, 29, 99, "long-99-2", 0, rng, nil, at.Add(90*time.Second))
	if err != nil || b.AssetID != 2 {
		t.Fatal("retirement skipped room's leased member", b.AssetID, err)
	}
	policyConsume(t, root, v, b, 29, 99, 2, at.Add(90*time.Second))
	scriptPolicyPoolAudio(root, v, u, 14, 29, 100, "long-100-1", 0, rng, nil, at.Add(180*time.Second))
	st, _ := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
	for _, c := range st.Cycles {
		for _, x := range c.Members {
			if x.AudioRef == "2" {
				t.Fatal("retired version survived new round")
			}
		}
	}
}

func TestPolicySupplyBackoffAndPlaybackOwnsPacing(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	at := time.Now().UTC()
	units := []volcengineScriptUnit{u}
	policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	i, _, _, err := scriptPolicyGeneration(root, v, units, at, true)
	if err != nil || i != 0 {
		t.Fatal(i, err)
	}
	i, _, _, _ = scriptPolicyGeneration(root, v, units, at.Add(time.Second), true)
	if i >= 0 {
		t.Fatal("fast poll retried paid work")
	}
	i, _, _, _ = scriptPolicyGeneration(root, v, units, at.Add(11*time.Second), true)
	if i != 0 {
		t.Fatal("first retry missing")
	}
	i, _, _, _ = scriptPolicyGeneration(root, v, units, at.Add(22*time.Second), true)
	if i >= 0 {
		t.Fatal("failure backoff did not grow")
	}
	for n := 2; n <= 6; n++ {
		policyFixture(t, root, v, u, int64(n), fmt.Sprintf("新版本%d", n), 75000, scriptplay.KindRewrite)
	}
	i, _, _, _ = scriptPolicyGeneration(root, v, units, at.Add(time.Minute), false)
	if i != 0 {
		t.Fatal("capacity decision unexpectedly depended on clock", i)
	}
	i, _, _, _ = scriptPolicyGeneration(root, v, units, at.Add(10*time.Minute), false)
	if i < 0 {
		t.Fatal("capacity decision missing", i)
	}
}

func TestPolicyForegroundOriginalAndHostCancellationShareReservation(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "前台新合成原文"}
	x := policyFixture(t, root, v, u, 1, u.Text, 75000, scriptplay.KindOriginal)
	a, _ := scriptVersionAudio(root, u, v, x)
	at := time.Now().UTC()
	if err := reserveScriptPolicyAudio(root, v, u, 14, 29, "long-99-1", a); err != nil {
		t.Fatal(err)
	}
	policyConsume(t, root, v, a, 29, 99, 1, at)
	st, _ := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
	for _, c := range st.Cycles {
		if !c.Used[scriptContentID(a.Text)] || len(c.Pending) > 0 {
			t.Fatal("foreground audio did not consume coverage", c)
		}
	}
	other := v
	other.VoiceIdentity.VoiceID = "B"
	x = policyFixture(t, root, other, u, 2, u.Text, 80000, scriptplay.KindOriginal)
	b, _ := scriptVersionAudio(root, u, other, x)
	reserveScriptPolicyAudio(root, v, u, 14, 29, "long-99-2", a)
	reserveScriptPolicyAudio(root, other, u, 14, 29, "long-99-2", b)
	st, _ = readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
	pending := 0
	for _, c := range st.Cycles {
		if c.Pending["long-99-2"] != "" {
			pending++
		}
	}
	if pending != 1 {
		t.Fatal("cancelled host stranded reservation", pending)
	}
}

func TestPolicySingleModeAllocatesByExposureAndForecastUsesQueue(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	script := recordingScript{Version: 2, PlayMode: "single", Tabs: []recordingScriptTab{
		{TabID: "a", Entries: []recordingScriptEntry{{EntryID: "1", Text: strings.Repeat("一", 400)}, {EntryID: "2", Text: strings.Repeat("二", 400)}}},
		{TabID: "b", Entries: []recordingScriptEntry{{EntryID: "3", Text: strings.Repeat("三", 400)}}},
	}}
	v.GenerationContext = map[string]any{"recording_script": script}
	var units []volcengineScriptUnit
	asset := int64(0)
	for ti, tab := range script.Tabs {
		for _, entry := range tab.Entries {
			asset++
			u := volcengineScriptUnit{Pool: scriptEntryPoolID(tab, ti, entry.EntryID), Key: scriptFirstParagraphKey(v, entry.Text), Text: entry.Text}
			units = append(units, u)
			policyFixture(t, root, v, u, asset, u.Text, 75000, scriptplay.KindOriginal)
		}
	}
	plan := scriptPlanPools(root, v, units)
	if plan.Caps[0] != 16 || plan.Caps[1] != 16 || plan.Caps[2] != 32 {
		t.Fatal("single-mode frequency ignored", plan.Caps)
	}
	at := time.Now().UTC()
	cursor := volcengineScriptCursor{Delivered: []scriptDeliveredTrack{
		{TrackID: "playing", PolicyText: "正在播的正文", PolicyDurationMS: 60000, PolicyRecordedMS: 30000, Block: scriptFirstCachedBlock{DurationMS: 60000}},
		{TrackID: "next", PolicyText: "等待播的正文", PolicyDurationMS: 60000, Block: scriptFirstCachedBlock{DurationMS: 60000}},
	}}
	snap := coreclient.ProgramRefreshSnapshot{TrackID: "playing", BufferedMainlineMS: 90000}
	snap.FutureWindow = append(snap.FutureWindow, struct {
		ID         string `json:"track_id"`
		Text       string `json:"text"`
		DurationMS int    `json:"duration_ms"`
	}{ID: "next", DurationMS: 60000})
	spans := scriptPolicyForecastQueue(cursor, snap, at)
	if len(spans) != 2 || math.Abs(spans[0].From-5e8) > 1e-5 || spans[1].End != at.UnixMilli()+90000 {
		t.Fatal("queue forecast omitted unplayed source tail", spans)
	}
	st := scriptRoomPolicy{Spans: spans}
	if rate := scriptForecastRate(st, scriptContentID("等待播的正文"), 60000, at.UnixMilli()+90000); rate < .39 {
		t.Fatal("buffered duplicate hidden", rate)
	}
}

func TestPolicyNewSessionWarmPoolDoesNotPurchaseRefresh(t *testing.T) {
	root := t.TempDir()
	v := policyVersion()
	u := volcengineScriptUnit{Pool: "p", Text: "原文"}
	units := []volcengineScriptUnit{u}
	at := time.Now().UTC()
	for i := 0; i < 5; i++ {
		text := fmt.Sprintf("旧缓存第%d版", i)
		kind := scriptplay.KindRewrite
		if i == 0 {
			text = u.Text
			kind = scriptplay.KindOriginal
		}
		policyFixture(t, root, v, u, int64(i+1), text, 16*60000, kind)
	}
	pool, _ := loadScriptVoicePool(root, 4, u.poolID())
	for i := range pool.Versions {
		pool.Versions[i].PlayCount = 4
		pool.Versions[i].CompleteCount = 4
	}
	saveScriptVoicePool(root, 4, u.poolID(), pool)
	if err := beginScriptRoomPolicy(root, 14, 29, 4, 99); err != nil {
		t.Fatal(err)
	}
	if !scriptPolicyRoomWarmup(root, 14, 29, 4, at) {
		t.Fatal("startup preparation counted as live time")
	}
	i, _, _, err := scriptPolicyGeneration(root, v, units, at, false, true)
	if err != nil || i >= 0 {
		t.Fatal("warm historical pool bought a new batch", i, err)
	}
	a, _ := scriptVersionAudio(root, u, v, pool.Versions[0])
	policyConsume(t, root, v, a, 29, 99, 1, at)
	if scriptPolicyRoomWarmup(root, 14, 29, 4, at.Add(91*time.Minute)) {
		t.Fatal("warmup never ended")
	}
	i, _, _, err = scriptPolicyGeneration(root, v, units, at.Add(91*time.Minute), false, false)
	if err != nil || i != 0 {
		t.Fatal("mature refresh did not resume", i, err)
	}
	beginScriptRoomPolicy(root, 14, 29, 4, 100)
	if !scriptPolicyRoomWarmup(root, 14, 29, 4, at.Add(92*time.Minute)) {
		t.Fatal("new session inherited old warmup clock")
	}
	st, _ := readScriptRoomPolicy(scriptRoomPolicyPath(root, 14, 29, 4), 14, 29, 4)
	if len(st.Spans) == 0 {
		t.Fatal("new session erased rolling90 evidence")
	}
}
