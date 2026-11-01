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
	"unicode/utf8"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const volcengineSupplyThresholdMS int64 = 40000
const volcengineReplayThresholdMS int64 = 15000

type volcengineScriptUnit struct{ Text, Key, Dir string }
type volcenginePendingTrack struct {
	Unit      int                   `json:"unit"`
	Audio     volcengineCachedAudio `json:"audio"`
	ID        string                `json:"id"`
	Advance   bool                  `json:"advance"`
	NextBlock int                   `json:"next_block,omitempty"`
}
type volcengineScriptCursor struct {
	VersionID            int64                           `json:"version_id"`
	Signature            string                          `json:"signature"`
	ProgramID            string                          `json:"program_id"`
	Round                int                             `json:"round"`
	Order                []int                           `json:"order"`
	Position             int                             `json:"position"`
	Sequence             int                             `json:"sequence"`
	Previous             int                             `json:"previous"`
	Pending              *volcenginePendingTrack         `json:"pending,omitempty"`
	LastQueued           int                             `json:"last_queued,omitempty"`
	ConsecutiveFallbacks int                             `json:"consecutive_fallbacks,omitempty"`
	SilenceBlocks        int                             `json:"silence_blocks,omitempty"`
	SilenceAssetID       int64                           `json:"silence_asset_id,omitempty"`
	PendingSilence       *coreclient.ProgramRefreshTrack `json:"pending_silence,omitempty"`
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
	return c.Pending == nil || (c.Pending.Unit >= 0 && c.Pending.Unit < n && c.Pending.Audio.AssetID > 0 && c.Pending.ID != "" && c.Pending.NextBlock >= 0 && c.Pending.NextBlock <= max(1, len(c.Pending.Audio.Blocks)))
}
func volcengineSupplyDue(snapshot coreclient.ProgramRefreshSnapshot, leadMS int64) bool {
	return snapshot.WaitingForContent || snapshot.BufferedMainlineMS < leadMS
}

// 处理开销：单段归档上传、下载校验。取消播放分块后每段只归档一次。
const volcengineProcessOverheadMS int64 = 2000

// 安全余量：吸收网络抖动与供应商排队。
const volcengineSupplyMarginMS int64 = 15000

// scriptFirstSupplyLead 按下一段字数估算预取余量。
// 复用段不需要合成，直接用 40 秒下限；只有首轮新生才按字数算。
func scriptFirstSupplyLead(units []volcengineScriptUnit, index int, model string, fresh bool) int64 {
	if !fresh || index < 0 || index >= len(units) {
		return volcengineSupplyThresholdMS
	}
	perRune := int64(26) // 阿里实测 24.5—29 ms/字，取 26
	if ttsgateway.ProviderForModel(model) == ttsgateway.ProviderVolcengine {
		perRune = 20 // 火山实测 18.4—21.3 ms/字
	}
	lead := int64(utf8.RuneCountInString(units[index].Text))*perRune + volcengineProcessOverheadMS + volcengineSupplyMarginMS
	if lead < volcengineSupplyThresholdMS {
		return volcengineSupplyThresholdMS
	}
	return lead
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
	if !ttsgateway.SupportsScriptParagraphs(modelName) {
		return errors.New("声音模型不支持话术整段合成")
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
	namespace := "volcengine-longtext-v1"
	if modelName != ttsgateway.VolcengineICL2 {
		namespace = "script-first-v1"
	}
	dir := scope.CacheDir(namespace, "paragraphs")
	units, err := scriptFirstUnits(v, dir)
	if err != nil {
		return err
	}
	// 方案级首段预热：把保存时预先合成的首段复制进本房间缓存，
	// 开播立即有声音；房间已有付费缓存时不覆盖。
	if len(units) > 0 {
		s.hydrateFirstParagraphFromPlan(ctx, tenant, room, v, units[0])
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
		blocks := pending.Audio.Blocks
		if len(blocks) == 0 {
			blocks = []scriptFirstCachedBlock{{AssetID: pending.Audio.AssetID, DurationMS: pending.Audio.DurationMS, Text: units[pending.Unit].Text, Timeline: pending.Audio.Timeline}}
		}
		if err := publishScriptFirstBlocks(c, pending, blocks, func(c context.Context, blockIndex int, block scriptFirstCachedBlock) error {
			if e := checkOwner(c); e != nil {
				return e
			}
			url, e := s.recordingStreamAssetURL(c, tenant, block.AssetID)
			if e != nil {
				return e
			}
			id := pending.ID
			if len(pending.Audio.Blocks) > 0 {
				id = scriptFirstTrackID(session, cursor.Sequence, pending.Unit, blockIndex)
			}
			if pending.Audio.UntimedTranscript {
				id += "-raw"
			}
			track := coreclient.ProgramRefreshTrack{ID: id, Label: "话术原文整段", Text: block.Text, AudioURL: url, DurationMS: int(block.DurationMS), AllowConsecutiveRepeat: true,
				UntimedTranscript: pending.Audio.UntimedTranscript,
				Timeline:          recordingStreamTimeline(block.Timeline), SafePoints: recordingStreamSafePoints(volcengineSemanticSafePoints(block.Timeline))}
			if cursor.ProgramID == "" {
				p, e := s.startLiveReadyProgram(c, tenant, room, v, fmt.Sprintf("volcengine-long-%d", session), fmt.Sprintf("话术整段 V%d", v.VersionNo), track)
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
			return nil
		}, save); err != nil {
			return err
		}
		if pending.Advance {
			cursor.Position++
			cursor.Previous = pending.Unit
		}
		cursor.LastQueued = pending.Unit
		if pending.Advance {
			cursor.ConsecutiveFallbacks = 0
		} else {
			cursor.ConsecutiveFallbacks++
			if len(known) == 1 || cursor.ConsecutiveFallbacks == 6 {
				log.Printf("script supply fallback tenant=%d room=%d session=%d cached=%d consecutive=%d", tenant, room, session, len(known), cursor.ConsecutiveFallbacks)
			}
		}
		cursor.Pending = nil
		return save()
	}
	queue := func(c context.Context, index int, audio volcengineCachedAudio, advance bool) error {
		cursor.Sequence++
		cursor.SilenceBlocks = 0
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
		pad, fallback := scriptFirstSilenceDue(p, cursor.SilenceBlocks)
		if cursor.PendingSilence != nil || (cursor.ProgramID != "" && pad) {
			if e = s.queueSilence(c, tenant, room, session, v, &cursor, save); e == nil {
				return nil
			}
			// An uncertain delivery must retain the identical ID/body for retry;
			// never enqueue a real paragraph ahead of a pending silence receipt.
			if cursor.PendingSilence != nil {
				return e
			}
			log.Printf("script supply silence failed tenant=%d room=%d session=%d error=%v", tenant, room, session, e)
			fallback = true // Unavailable storage must not strand playable cache.
		}
		if !fallback {
			return nil
		}
		if len(known) == 0 {
			return nil
		}
		// No model/TTS/tempo rendering in the waiting path, only a fresh URL.
		for len(known) > 0 {
			i, ok := scriptFirstFallback(known, scriptFirstPlayingUnit(p.TrackID, session), cursor.LastQueued, rng)
			if !ok {
				return nil
			}
			audio := known[i]
			if _, e = s.recordingStreamAssetURL(c, tenant, audio.AssetID); e != nil {
				delete(known, i)
				continue
			}
			return queue(c, i, audio, false)
		}
		return nil
	}
	retryAttempt := 0
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		if cursor.PendingSilence != nil {
			if err = checkOwner(ctx); err != nil {
				return err
			}
			if err = s.queueSilence(ctx, tenant, room, session, v, &cursor, save); err != nil {
				return err
			}
			continue
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
		nextIndex := cursor.Order[cursor.Position]
		needsSynthesis := cursor.Round == 0 && known[nextIndex].AssetID <= 0
		if cursor.ProgramID != "" && !volcengineSupplyDue(p, scriptFirstSupplyLead(units, nextIndex, v.VoiceIdentity.Model, needsSynthesis)) {
			if err = waitLiveProducer(ctx); err != nil {
				return err
			}
			continue
		}
		index := cursor.Order[cursor.Position]
		// 循环变速已关闭：跨轮固定 1.0 倍率，直接复用首轮原速音频。
		// 关闭原因是每轮每段一次 ffmpeg atempo 渲染对机器压力明显（2026-11-01）。
		// This check is the sole paid lookahead gate: no next submission until
		// buffered mainline reaches its length-based lead. One paragraph is in flight.
		work, done := context.WithCancel(ctx)
		allowSynthesis := cursor.Round == 0
		audio, e := awaitVolcengineParagraph(work, 500*time.Millisecond, func(c context.Context) (volcengineCachedAudio, error) {
			return s.ensureScriptFirstParagraph(c, tenant, room, session, v, units[index], allowSynthesis)
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
			delay := scriptFirstRetryDelay(retryAttempt, rng)
			retryAttempt++
			for until := time.Now().Add(delay); time.Now().Before(until); {
				if err = keepPlaying(ctx); err != nil {
					return err
				}
				if err = waitLiveProducer(ctx); err != nil {
					return err
				}
			}
			continue
		}
		retryAttempt = 0
		known[index] = audio
		if err = queue(ctx, index, audio, true); err != nil {
			return err
		}
	}
}

var errVolcengineLivePaused = errors.New("高仿真供料场次已暂停")
