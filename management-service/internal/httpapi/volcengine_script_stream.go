package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

const volcengineSupplyThresholdMS int64 = 40000
const volcengineReplayThresholdMS int64 = 15000

type volcengineScriptUnit struct {
	Text string
	// Key 是音频缓存身份：文本一变就要重合成，所以它是文本哈希。
	Key string
	// Pool 是版本池身份：按话术条目 ID 生成，改文案不会丢已生成的版本。
	Pool string
	Dir  string
}

// poolID 返回版本池用的键：有条目 ID 就用条目 ID，否则退回文本哈希。
func (u volcengineScriptUnit) poolID() string {
	if u.Pool != "" {
		return u.Pool
	}
	return u.Key
}

type volcenginePendingTrack struct {
	Unit      int                   `json:"unit"`
	Audio     volcengineCachedAudio `json:"audio"`
	ID        string                `json:"id"`
	Advance   bool                  `json:"advance"`
	NextBlock int                   `json:"next_block,omitempty"`
}
type volcengineScriptCursor struct {
	GroupedOrder         bool                            `json:"grouped_order,omitempty"`
	RewriteSnapshot      *scriptRewriteSnapshot          `json:"rewrite_snapshot,omitempty"`
	Delivered            []scriptDeliveredTrack          `json:"delivered,omitempty"`
	ActiveVoice          string                          `json:"active_voice,omitempty"`
	ActiveModel          string                          `json:"active_model,omitempty"`
	VoiceBlockLeft       int                             `json:"voice_block_left,omitempty"`
	UnitSelections       []int                           `json:"unit_selections,omitempty"`
	VersionID            int64                           `json:"version_id"`
	Signature            string                          `json:"signature"`
	ProgramID            string                          `json:"program_id"`
	Round                int                             `json:"round"`
	Order                []int                           `json:"order"`
	Position             int                             `json:"position"`
	Sequence             int                             `json:"sequence"`
	Previous             int                             `json:"previous"`
	PreviousOrder        []int                           `json:"previous_order,omitempty"`
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
		if x < 0 || x >= n || seen[x] || (c.Round == 0 && !c.GroupedOrder && x != i) {
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
	if err := s.validateScriptPlaybackVoices(ctx, tenant, room, v); err != nil {
		return err
	}
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
	// 音频跟着方案走，不跟着房间走：同一方案的多个房间共用一份段落缓存，
	// 第二个房间开同一方案时直接复用，不再重新合成。
	if planDir, ok := planCacheDir(tenant, v.PlanID, v.VoiceIdentity.Model); ok && hasGroupedRecordingScript(v) {
		dir = filepath.Join(planDir, "paragraphs")
	}
	units, err := scriptFirstUnits(v, dir)
	if err != nil {
		return err
	}
	// 开播时顺手清掉不属于当前话术的旧版本池文件。
	pruneScriptVoicePools(v.PlanID, units)
	// 按整轮时长算这场的版本上限，片段越少版本越多。
	rememberScriptVersionCap(v, units)
	// 方案级首段预热：把保存时预先合成的首段复制进本房间缓存，
	// 开播立即有声音；房间已有付费缓存时不覆盖。
	if len(units) > 0 {
		if err := s.hydrateFirstParagraphFromPlan(ctx, tenant, room, v, units[0]); err != nil {
			return err
		}
	}
	signature := ""
	for _, unit := range units {
		signature += unit.Key
	}
	signature = volcengineParagraphKey(id, signature, v.VoiceIdentity.Rate)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	cursor := volcengineScriptCursor{VersionID: v.ID, Signature: signature, GroupedOrder: true, Order: scriptInitialPlaybackOrder(v, units), Previous: -1}
	checkpoint := scriptSupplyCheckpointPath(tenant, room, session)
	var saved volcengineScriptCursor
	if e := readVolcengineJSON(checkpoint, &saved); e == nil {
		if !validVolcengineCursor(saved, v.ID, signature, len(units)) {
			return errors.New("长段供料检查点与当前场次不一致")
		}
		cursor = saved
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	if cursor.RewriteSnapshot == nil {
		frozen := scriptRewriteSnapshot{}
		if cursor.Sequence == 0 {
			if snapshot, err := s.loadScriptRewriteSnapshot(ctx, tenant, v); err == nil {
				frozen = snapshot
			} else {
				log.Printf("script rewrite snapshot unavailable plan=%d: %v", v.PlanID, err)
			}
		}
		// Legacy checkpoints have no proof of their old facts. Restore playback,
		// but do not buy rewrites against a newly edited document after restart.
		cursor.RewriteSnapshot = &frozen
	}
	v = withScriptRewriteSnapshot(v, *cursor.RewriteSnapshot)
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
		if e = s.confirmScriptPlayback(ctx, tenant, room, session, v, units, &cursor, p, save); e != nil {
			return p, e
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
			blockText := units[pending.Unit].Text
			if strings.TrimSpace(pending.Audio.Text) != "" {
				blockText = pending.Audio.Text
			}
			blocks = []scriptFirstCachedBlock{{AssetID: pending.Audio.AssetID, DurationMS: pending.Audio.DurationMS, Text: blockText, Timeline: pending.Audio.Timeline}}
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
			track := coreclient.ProgramRefreshTrack{ID: id, Label: "话术主线整段", Text: block.Text, AudioURL: url, DurationMS: int(block.DurationMS), AllowConsecutiveRepeat: true,
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
			found := false
			for _, sent := range cursor.Delivered {
				if sent.TrackID == id {
					found = true
					break
				}
			}
			if !found {
				cursor.Delivered = append(cursor.Delivered, scriptDeliveredTrack{Unit: pending.Unit, TrackID: id, InstanceID: pending.ID, AssetID: pending.Audio.AssetID, VoiceID: pending.Audio.VoiceID, Model: cursor.ActiveModel, Block: block})
			}
			s.archiveTTSCacheReplay(c, tenant, room, session, cursor.ProgramID, track)
			return nil
		}, save); err != nil {
			return err
		}
		if pending.Advance {
			cursor.Position++
			cursor.Previous = pending.Unit
			if len(cursor.UnitSelections) != len(units) {
				cursor.UnitSelections = make([]int, len(units))
			}
			cursor.UnitSelections[pending.Unit]++
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
	// 音色轮换：按方案选中的音色列表走块，一块至少 min_voice_block 轮。
	// 目标音色没备齐就继续用当前音色，也就是"准备中"状态。
	voices := scriptVoicesForPlayback(v)
	voiceIdx := 0
	// Begin with the already-authorized formal voice, not an arbitrary list item.
	for i, voice := range voices {
		if voice.VoiceID == v.VoiceIdentity.VoiceID && (voice.Model == "" || voice.Model == v.VoiceIdentity.Model) {
			voiceIdx = i
		}
	}
	if cursor.ActiveVoice != "" {
		for i, voice := range voices {
			if voice.VoiceID == cursor.ActiveVoice && (voice.Model == "" || voice.Model == cursor.ActiveModel) {
				voiceIdx = i
			}
		}
	}
	activeV := scriptVersionForVoice(v, voices[voiceIdx])
	for i, audio := range known {
		if audio.VoiceID != activeV.VoiceIdentity.VoiceID {
			delete(known, i)
		}
	}
	restoringVoice := cursor.ActiveVoice != ""
	cursor.ActiveVoice = activeV.VoiceIdentity.VoiceID
	cursor.ActiveModel = activeV.VoiceIdentity.Model
	if !restoringVoice && cursor.VoiceBlockLeft <= 0 {
		cursor.VoiceBlockLeft = scriptVoiceBlockLength(activeV, units)
	}
	lastVoicePreparation := ""
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

			// At block end, keep retrying readiness each round rather than resetting
			// another four rounds while the target voice is still being prepared.
			cursor.VoiceBlockLeft = max(0, cursor.VoiceBlockLeft-1)
			if cursor.VoiceBlockLeft == 0 {
				if next, ready := scriptNextPlaybackVoice(voices, voiceIdx, v, units); ready {
					target := scriptVersionForVoice(v, voices[next])
					identity := target.VoiceIdentity
					if e := s.validatePlanVoiceIdentity(ctx, tenant, &identity); e != nil {
						log.Printf("script voice switch unavailable tenant=%d room=%d session=%d: %v", tenant, room, session, e)
					} else {
						voiceIdx, activeV = next, target
						cursor.ActiveVoice, cursor.ActiveModel = activeV.VoiceIdentity.VoiceID, activeV.VoiceIdentity.Model
						cursor.VoiceBlockLeft = scriptVoiceBlockLength(activeV, units)
						known = map[int]volcengineCachedAudio{}
					}
				} else if len(voices) == 1 {
					cursor.VoiceBlockLeft = scriptVoiceBlockLength(activeV, units)
				}
			}
			// 按话术标签分组排下一轮：固定标签锁位，其余随机。
			previousOrder := cursor.Order
			cursor.Order = scriptPlayRoundOrder(v, units, cursor.Round, cursor.Previous, previousOrder, rng)
			// 跨轮首尾不能撞同一片段，否则连着听两遍一样的内容。
			cursor.Order = scriptSingleSelectionOrder(v, units, cursor.Order, cursor.UnitSelections, cursor.Previous)
			cursor.PreviousOrder = previousOrder
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
		// 单选模式：该标签本轮已经播过一条，后面同组片段直接跳过。
		if scriptPlaySkipInSingleMode(v, units, cursor.Order, cursor.Position) {
			cursor.Position++
			if err = save(); err != nil {
				return err
			}
			continue
		}
		nextIndex := cursor.Order[cursor.Position]
		needsSynthesis := known[nextIndex].AssetID <= 0
		if cursor.ProgramID != "" && !volcengineSupplyDue(p, scriptFirstSupplyLead(units, nextIndex, v.VoiceIdentity.Model, needsSynthesis)) {
			if err = waitLiveProducer(ctx); err != nil {
				return err
			}
			continue
		}
		index := cursor.Order[cursor.Position]
		// Secondary preparation uses the same producer, account pool, budget and
		// durable paid checkpoints. Admit only below 40s, near the next voice block,
		// and with this voice's next piece already paid, so current supply takes priority.
		preparation := fmt.Sprintf("%d:%d", cursor.Round, cursor.Position)
		if len(voices) > 1 && scriptVoicePreparationDue(cursor, p, lastVoicePreparation, len(scriptCompatiblePool(recordingCacheMediaRoot(), activeV, units[index]).Versions) > 0) {
			if target, unit, needed := scriptNextVoicePreparation(voices, voiceIdx, v, units); needed {
				lastVoicePreparation = preparation
				identity := target.VoiceIdentity
				prepareErr := s.validatePlanVoiceIdentity(ctx, tenant, &identity)
				if prepareErr == nil {
					prepareErr = s.store.CheckTTSRoomPool(ctx, tenant, room, identity.Provider)
				}
				if prepareErr == nil {
					var prepared volcengineCachedAudio
					prepared, prepareErr = awaitVolcengineParagraph(ctx, 500*time.Millisecond, func(c context.Context) (volcengineCachedAudio, error) {
						if e := checkOwner(c); e != nil {
							return volcengineCachedAudio{}, e
						}
						return s.ensureScriptFirstParagraph(c, tenant, room, session, target, unit, true)
					}, keepPlaying)
					if prepareErr == nil {
						recordScriptVoiceVersion(recordingCacheMediaRoot(), v.PlanID, unit, target, prepared)
					}
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if errors.Is(prepareErr, anchorcycle.ErrStale) {
					return prepareErr
				}
				if prepareErr != nil {
					log.Printf("script next voice preparation deferred tenant=%d room=%d session=%d: %v", tenant, room, session, prepareErr)
				}
			}
		}
		// 循环变速已关闭：跨轮固定 1.0 倍率，直接复用首轮原速音频。
		// 关闭原因是每轮每段一次 ffmpeg atempo 渲染对机器压力明显（2026-11-01）。
		// This check is the sole paid lookahead gate: no next submission until
		// buffered mainline reaches its length-based lead. One paragraph is in flight.
		work, done := context.WithCancel(ctx)
		// Only canonical, authorized selected identities may purchase missing audio.
		allowSynthesis := voices[voiceIdx].Identity != nil || (activeV.VoiceIdentity.VoiceID == v.VoiceIdentity.VoiceID && activeV.VoiceIdentity.Model == v.VoiceIdentity.Model)
		voiceUnit := scriptUnitForVoice(activeV, units[index])
		audio, playedText, fromPool := scriptCompatiblePoolAudio(recordingCacheMediaRoot(), activeV, voiceUnit, fmt.Sprintf("%d-%d-%d", tenant, room, session), rng)
		if fromPool && strings.TrimSpace(playedText) != "" {
			audio.Text = playedText
		}
		if !fromPool {
			generationVersion := activeV
			audio, e = awaitVolcengineParagraph(work, 500*time.Millisecond, func(c context.Context) (volcengineCachedAudio, error) {
				return s.ensureScriptFirstParagraph(c, tenant, room, session, generationVersion, voiceUnit, allowSynthesis)
			}, keepPlaying)
		}
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
		// 把这次合成结果登记进片段版本池：音色、模型、音量、指纹与实际音频。
		// 现在生成器只返回原文，所以登记的一定是原始版本；改写接入后自动记改写。
		if !fromPool {
			recordScriptVoiceVersion(recordingCacheMediaRoot(), v.PlanID, voiceUnit, activeV, audio)
		}

		if err = queue(ctx, index, audio, true); err != nil {
			return err
		}
	}
}

var errVolcengineLivePaused = errors.New("高仿真供料场次已暂停")

// playbackLedger 记录每一次真实推送给 Core 的声音，一行一条。
//
// 目的：事后能查"这场直播念了哪几条、每条几遍"，也能按音频 ID 对回版本池。
// 只追加写，失败不影响播放。
func recordPlaybackLedger(tenant, room int64, v model.LiveAgentPlanVersion, unitIndex int, block scriptFirstCachedBlock, trackID string) {
	entry := map[string]any{
		"at":          time.Now().Format(time.RFC3339),
		"tenant_id":   tenant,
		"room_id":     room,
		"plan_id":     v.PlanID,
		"version_id":  v.ID,
		"unit_index":  unitIndex,
		"asset_id":    block.AssetID,
		"duration_ms": block.DurationMS,
		"track_id":    trackID,
		"voice_id":    v.VoiceIdentity.VoiceID,
		"text_head":   playbackTextHead(block.Text, 40),
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	dir := filepath.Join(recordingCacheMediaRoot(), "playback-ledger")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("%d-%d.jsonl", tenant, room))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

func playbackTextHead(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}
