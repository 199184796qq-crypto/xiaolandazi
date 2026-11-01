package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const volcengineSupplyThresholdMS int64 = 40000
const volcengineReplayThresholdMS int64 = 15000

type volcengineScriptUnit struct{ Text, Key, Dir string }
type volcenginePendingTrack struct {
	Unit    int                   `json:"unit"`
	Audio   volcengineCachedAudio `json:"audio"`
	ID      string                `json:"id"`
	Advance bool                  `json:"advance"`
}
type volcengineScriptCursor struct {
	VersionID int64                   `json:"version_id"`
	Signature string                  `json:"signature"`
	ProgramID string                  `json:"program_id"`
	Round     int                     `json:"round"`
	Order     []int                   `json:"order"`
	Position  int                     `json:"position"`
	Sequence  int                     `json:"sequence"`
	Previous  int                     `json:"previous"`
	Pending   *volcenginePendingTrack `json:"pending,omitempty"`
}

func usesVolcengineParagraphs(v model.LiveAgentPlanVersion) bool {
	return recordingVoiceMode(v) == recordingStreamMode && v.VoiceIdentity.Model == ttsgateway.VolcengineICL2
}
func volcengineScriptUnits(v model.LiveAgentPlanVersion, dir string) []volcengineScriptUnit {
	var units []volcengineScriptUnit
	for _, text := range recordingStreamTexts(v) {
		for _, part := range splitVolcengineParagraphs(text) {
			key := volcengineParagraphKey(v.VoiceIdentity.VoiceID, part, v.VoiceIdentity.Rate)
			units = append(units, volcengineScriptUnit{Text: part, Key: key, Dir: filepath.Join(dir, key)})
		}
	}
	return units
}
func volcengineParagraphOrder(n, round, previous int, rng *rand.Rand) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if round > 0 {
		rng.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
		if n > 1 && order[0] == previous {
			j := 1 + rng.Intn(n-1)
			order[0], order[j] = order[j], order[0]
		}
	}
	return order
}
func validVolcengineCursor(c volcengineScriptCursor, version int64, signature string, n int) bool {
	if c.VersionID != version || c.Signature != signature || c.Round < 0 || c.Position < 0 || c.Position > n || len(c.Order) != n {
		return false
	}
	seen := make([]bool, n)
	for i, x := range c.Order {
		if x < 0 || x >= n || seen[x] || (c.Round == 0 && x != i) {
			return false
		}
		seen[x] = true
	}
	return c.Pending == nil || (c.Pending.Unit >= 0 && c.Pending.Unit < n && c.Pending.Audio.AssetID > 0 && c.Pending.ID != "")
}
func volcengineSupplyDue(snapshot coreclient.ProgramRefreshSnapshot) bool {
	return snapshot.WaitingForContent || snapshot.BufferedMainlineMS < volcengineSupplyThresholdMS
}

// One worker owns the pending cloud request; the caller remains free to replay
// matching paid cache while accounts are queued or the async task is running.
// Cancellation stops admission/publication, but the provider drains accepted
// work to its durable staging file so it will never be purchased twice.
func awaitVolcengineParagraph(ctx context.Context, interval time.Duration, makeAudio func(context.Context) (volcengineCachedAudio, error), keepPlaying func(context.Context) error) (volcengineCachedAudio, error) {
	if err := ctx.Err(); err != nil {
		return volcengineCachedAudio{}, err
	}
	type result struct {
		audio volcengineCachedAudio
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				ch <- result{err: errors.New("长段准备异常，已保留任务检查点")}
			}
		}()
		audio, err := makeAudio(ctx)
		ch <- result{audio, err}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return volcengineCachedAudio{}, ctx.Err()
		case r := <-ch:
			if err := ctx.Err(); err != nil {
				return volcengineCachedAudio{}, err
			}
			return r.audio, r.err
		case <-ticker.C:
			if err := keepPlaying(ctx); err != nil {
				return volcengineCachedAudio{}, err
			}
		}
	}
}

func (s *Server) runVolcengineScriptProducer(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := waitLiveLeasePermit(ctx); err != nil {
		return err
	}
	id, modelName, err := s.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{Source: v.VoiceIdentity.Source, VoiceID: v.VoiceIdentity.VoiceID, ProfileID: v.VoiceIdentity.ProfileID, BindingID: v.VoiceIdentity.BindingID, Model: v.VoiceIdentity.Model})
	if err != nil {
		return err
	}
	if modelName != ttsgateway.VolcengineICL2 {
		return errors.New("高仿真长段模型与方案不一致")
	}
	v.VoiceIdentity.VoiceID = id
	v.VoiceIdentity.Model = modelName
	if v.VoiceIdentity.Rate == 0 {
		v.VoiceIdentity.Rate = 1
	}
	scope, ok := recordingCacheScopeFor(tenant, room, v.PlanID)
	if !ok {
		return errors.New("高仿真长段缓存目录不可用")
	}
	dir := scope.CacheDir("volcengine-longtext-v1", "paragraphs")
	units := volcengineScriptUnits(v, dir)
	if len(units) == 0 {
		return errors.New("话术优先没有可用长段原文")
	}
	signature := ""
	for _, unit := range units {
		signature += unit.Key
	}
	signature = volcengineParagraphKey(id, signature, v.VoiceIdentity.Rate)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	cursor := volcengineScriptCursor{VersionID: v.ID, Signature: signature, Order: volcengineParagraphOrder(len(units), 0, -1, rng), Previous: -1}
	checkpoint := filepath.Join(recordingCacheMediaRoot(), "volcengine-supply", fmt.Sprintf("%d-%d-%d.json", tenant, room, session))
	var saved volcengineScriptCursor
	if e := readVolcengineJSON(checkpoint, &saved); e == nil {
		if !validVolcengineCursor(saved, v.ID, signature, len(units)) {
			return errors.New("长段供料检查点与当前场次不一致")
		}
		cursor = saved
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	save := func() error { return writeVolcengineJSON(checkpoint, cursor) }
	if err = save(); err != nil {
		return err
	}
	// Only entries matching this exact paragraph/voice/rate namespace qualify.
	known := map[int]volcengineCachedAudio{}
	for i, unit := range units {
		if cache, e := loadVolcengineParagraphCache(filepath.Join(unit.Dir, "cache.json"), unit.Key); e == nil {
			if audio, exists := cache.Audio[100]; exists {
				known[i] = audio
			}
		}
	}
	checkOwner := func(c context.Context) error {
		if e := waitLiveLeasePermit(c); e != nil {
			return e
		}
		paused, e := s.checkLiveAnchorOwner(c, tenant, room, session, v.ID)
		if e != nil {
			return e
		}
		if paused {
			return errVolcengineLivePaused
		}
		return c.Err()
	}
	snapshot := func(c context.Context) (coreclient.ProgramRefreshSnapshot, error) {
		if e := checkOwner(c); e != nil {
			return coreclient.ProgramRefreshSnapshot{}, e
		}
		p, e := s.core.GetRoomProgramSnapshot(c, tenant, room)
		if e != nil {
			return p, e
		}
		if cursor.ProgramID != "" && (!p.Running || p.ProgramID != cursor.ProgramID || p.VersionID != v.ID) {
			return p, anchorcycle.ErrStale
		}
		if cursor.ProgramID == "" && p.Running && p.Task.SessionID != fmt.Sprintf("volcengine-long-%d", session) {
			return p, anchorcycle.ErrStale
		}
		return p, nil
	}
	publish := func(c context.Context) error {
		pending := cursor.Pending
		if pending == nil {
			return nil
		}
		if e := checkOwner(c); e != nil {
			return e
		}
		url, e := s.recordingStreamAssetURL(c, tenant, pending.Audio.AssetID)
		if e != nil {
			return e
		}
		track := coreclient.ProgramRefreshTrack{ID: pending.ID, Label: "高仿真原文长段", Text: units[pending.Unit].Text, AudioURL: url, DurationMS: int(pending.Audio.DurationMS), AllowConsecutiveRepeat: true,
			Timeline: recordingStreamTimeline(pending.Audio.Timeline), SafePoints: recordingStreamSafePoints(volcengineSemanticSafePoints(pending.Audio.Timeline))}
		if cursor.ProgramID == "" {
			p, e := s.startLiveReadyProgram(c, tenant, room, v, fmt.Sprintf("volcengine-long-%d", session), fmt.Sprintf("高仿真长段 V%d", v.VersionNo), track)
			if e != nil {
				return e
			}
			cursor.ProgramID = p.ProgramID
			s.reportLiveStartup(c, tenant, room, session, "sending_audio")
		} else {
			if _, e = s.appendRecordingStreamTrack(c, tenant, room, v, cursor.ProgramID, track); e != nil {
				return e
			}
		}
		s.archiveTTSCacheReplay(c, tenant, room, session, cursor.ProgramID, track)
		if pending.Advance {
			cursor.Position++
			cursor.Previous = pending.Unit
		}
		cursor.Pending = nil
		return save()
	}
	queue := func(c context.Context, index int, audio volcengineCachedAudio, advance bool) error {
		cursor.Sequence++
		cursor.Pending = &volcenginePendingTrack{Unit: index, Audio: audio, ID: fmt.Sprintf("long-%d-%d", session, cursor.Sequence), Advance: advance}
		if e := save(); e != nil {
			return e
		}
		return publish(c)
	}
	keepPlaying := func(c context.Context) error {
		p, e := snapshot(c)
		if errors.Is(e, errVolcengineLivePaused) {
			return nil
		}
		if e != nil {
			return e
		}
		if cursor.Pending != nil {
			return publish(c)
		}
		if cursor.ProgramID != "" && p.BufferedMainlineMS >= volcengineReplayThresholdMS && !p.WaitingForContent {
			return nil
		}
		if len(known) == 0 {
			return nil
		}
		// No model/TTS/tempo rendering in the waiting path, only a fresh URL.
		start := rng.Intn(len(units))
		for j := 0; j < len(units); j++ {
			i := (start + j) % len(units)
			audio, exists := known[i]
			if !exists {
				continue
			}
			if _, e = s.recordingStreamAssetURL(c, tenant, audio.AssetID); e != nil {
				delete(known, i)
				continue
			}
			return queue(c, i, audio, false)
		}
		return nil
	}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if cursor.Pending != nil {
			if err = publish(ctx); err != nil {
				if errors.Is(err, errVolcengineLivePaused) {
					if err = waitLiveProducer(ctx); err == nil {
						continue
					}
				}
				return err
			}
			continue
		}
		if cursor.Position == len(units) {
			cursor.Round++
			cursor.Position = 0
			cursor.Order = volcengineParagraphOrder(len(units), cursor.Round, cursor.Previous, rng)
			if err = save(); err != nil {
				return err
			}
		}
		p, e := snapshot(ctx)
		if errors.Is(e, errVolcengineLivePaused) {
			if err = waitLiveProducer(ctx); err != nil {
				return err
			}
			continue
		}
		if e != nil {
			return e
		}
		if cursor.ProgramID != "" && !volcengineSupplyDue(p) {
			if err = waitLiveProducer(ctx); err != nil {
				return err
			}
			continue
		}
		index := cursor.Order[cursor.Position]
		percent := 100
		if cursor.Round > 0 {
			percent = 90 + rng.Intn(21)
		}
		// This check is the sole paid lookahead gate: no next submission while
		// buffered mainline is >=40s. Exactly one paragraph is ever in flight.
		work, done := context.WithCancel(ctx)
		allowSynthesis := cursor.Round == 0
		audio, e := awaitVolcengineParagraph(work, 500*time.Millisecond, func(c context.Context) (volcengineCachedAudio, error) {
			return s.ensureVolcengineParagraph(c, tenant, room, session, v, units[index], percent, allowSynthesis)
		}, keepPlaying)
		done()
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(e, anchorcycle.ErrStale) {
				return e
			}
			log.Printf("volcengine paragraph waiting tenant=%d room=%d session=%d unit=%d error=%v", tenant, room, session, index, e)
			// Retry the same staging task, never skip to a partial random loop.
			for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
				if err = keepPlaying(ctx); err != nil {
					return err
				}
				if err = waitLiveProducer(ctx); err != nil {
					return err
				}
			}
			continue
		}
		if percent == 100 {
			known[index] = audio
		}
		if err = queue(ctx, index, audio, true); err != nil {
			return err
		}
	}
}

var errVolcengineLivePaused = errors.New("高仿真供料场次已暂停")
