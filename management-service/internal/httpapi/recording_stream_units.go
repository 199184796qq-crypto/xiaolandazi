package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"time"
	"unicode"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

// 话术优先的供料链路：源文本 → 完整句单元 → 逐句合成 → 就绪单元 → 合块追加。
//
// 关键合同：
//  1. 只在句末标点切句。句尾一定是句界，所以每句合成出的时间轴天然带合法切点，
//     不再靠字数硬切、也不再事后猜切点。
//  2. 润色改变句子时重新切句，一句一个 ready unit，正文与音频一一对应。
//  3. 缓存以「正文 + 音轨 + 时间轴」整体复用，不会出现旧声音配新字幕。
//  4. 用显式游标记账：单元失败只在单元层重试或跳过并留痕，不让整场停。
type recordingReadyUnit struct {
	Text  string
	Track coreclient.ProgramRefreshTrack
}

type recordingSentenceState struct {
	Source         string
	Versions       [][]recordingReadyUnit
	Refreshed      int
	VersionCount   int
	InventoryKnown bool
	UpdateEligible bool
}

// splitRecordingSentences 只按句末标点切句；整段没有句末标点时保留整段
// （长度由润色侧的上限约束，不在这里硬切，硬切会制造非法句界）。
func splitRecordingSentences(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	const marks = "。！？；!?;\n"
	var out []string
	start := 0
	for index := 0; index < len(runes); index++ {
		if !strings.ContainsRune(marks, runes[index]) {
			continue
		}
		part := strings.TrimSpace(string(runes[start : index+1]))
		if part != "" {
			out = append(out, part)
		}
		start = index + 1
	}
	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		out = append(out, tail)
	}
	return out
}

// Legacy percentage helpers remain for compatibility. The producer below uses
// prepareRecordingRefreshSchedule and joint monetary quotes instead.
// Optional updates stop once the rolling-hour soft budget is nearly spent.
// Original A and necessary first audio remain allowed; only refresh work is
// suppressed, and the caller can still fall back to a valid cached version.
const recordingBudgetSoftCapMicros int64 = 1_200_000

func recordingBudgetPercent(costMicros int64, desired int) int {
	if desired > 0 && costMicros >= recordingBudgetSoftCapMicros {
		return 0
	}
	return desired
}

func (s *Server) recordingBudgetPercent(ctx context.Context, tenant, room int64, desired int) int {
	if s == nil || s.store == nil || desired <= 0 {
		return desired
	}
	settings, valid := s.liveCostSettingsSnapshot(ctx)
	if !valid || settings.MainlineHourlyMicros == 0 {
		return 0
	}
	policy := settings.Policy()
	cost, err := s.liveCostController().Summary(livecost.WithPolicy(ctx, policy), livecost.Scope{TenantID: tenant, RoomID: room})
	if err != nil {
		log.Printf("recording stream budget unavailable tenant=%d room=%d error=%v; optional updates paused", tenant, room, err)
		return 0
	}
	// The transactional request ledger includes pending/unknown attempts; media
	// archival duration and a delayed cron snapshot cannot authorize paid work.
	percent := desired
	if cost.TotalMicros >= policy.TotalTargetMicros || cost.OptionalMicros >= policy.OptionalLimitMicros {
		percent = 0
	}
	if percent == 0 && desired > 0 {
		log.Printf("recording stream budget reuse-only tenant=%d room=%d cost_micros=%d target_micros=%d", tenant, room, cost.TotalMicros, policy.TotalTargetMicros)
	}
	return percent
}

func recordingUpdatePercent(totalRunes int) int {
	minutes := float64(totalRunes) / 220.0
	switch {
	case minutes < 20:
		return 25
	case minutes <= 60:
		return 20
	default:
		return 10
	}
}

func (s *Server) runRecordingStreamProducer(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
	if err := waitLiveLeasePermit(ctx); err != nil {
		return err
	}
	texts := recordingStreamTexts(version)
	if len(texts) == 0 {
		return errors.New("话术优先没有可播放的正文")
	}

	tabs := make([][]recordingSentenceState, 0, len(texts))
	total := 0
	for _, text := range texts {
		sentences := splitRecordingSentences(text)
		if len(sentences) == 0 {
			continue
		}
		states := make([]recordingSentenceState, 0, len(sentences))
		for _, sentence := range sentences {
			states = append(states, recordingSentenceState{Source: sentence, Refreshed: -1})
		}
		tabs = append(tabs, states)
		total += len(states)
	}
	if len(tabs) == 0 {
		return errors.New("话术优先分段为空")
	}
	log.Printf("recording stream start tenant=%d room=%d version=%d tabs=%d sentences=%d", tenant, room, version.ID, len(tabs), total)

	polish := s.recordingPolishState(ctx, tenant, version.PlanID, room)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	seq, previousLast := 0, -1
	queue := recordingReadyQueue{startedAt: time.Now(), tenant: tenant, room: room}
	queue.combine = func(ctx context.Context, ready []recordingReadyUnit, block int) (coreclient.ProgramRefreshTrack, int, error) {
		return s.combineRecordingStreamBlockSafe(ctx, tenant, version, toRecordingCacheEntries(ready), seq, block)
	}
	queue.start = func(ctx context.Context, track coreclient.ProgramRefreshTrack) (coreclient.ProgramRefreshSnapshot, error) {
		snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, track)
		if err == nil {
			s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
		}
		return snapshot, err
	}
	queue.append = func(ctx context.Context, program string, track coreclient.ProgramRefreshTrack) (uint64, error) {
		return s.appendRecordingStreamTrack(ctx, tenant, room, version, program, track)
	}
	queue.buffer = func(ctx context.Context, program string) error {
		return waitRecordingStreamBuffer(ctx, s, tenant, room, program, version.ID)
	}
	// A cancelled or failed partial tab is reported as partial, never as a
	// completed playback round. Costs are joined by the attribution below.
	var supplyStats *recordingSupplyStats
	defer func() {
		if supplyStats != nil {
			supplyStats.write(false)
		}
	}()

	// 命中就不润色、不合成，直接复用已有音轨。
	cacheScope, cacheOK := recordingCacheScopeFor(tenant, room, version.PlanID)
	// Resolve a profile/binding before cache lookup, so a changed binding cannot
	// continue speaking in a previously resolved voice under the same profile ID.
	resolvedID, resolvedModel, voiceErr := s.resolveFormalVoice(ctx, tenant, fullShowVariantVoiceInput{
		Source: version.VoiceIdentity.Source, VoiceID: version.VoiceIdentity.VoiceID,
		ProfileID: version.VoiceIdentity.ProfileID, BindingID: version.VoiceIdentity.BindingID,
		Model: version.VoiceIdentity.Model,
	})
	if voiceErr != nil {
		return voiceErr
	}
	version.VoiceIdentity.VoiceID, version.VoiceIdentity.Model = resolvedID, resolvedModel
	var cacheManifest *recordingcache.Manifest
	hit := map[string][]recordingcache.Entry{}
	planner := recordingUpdatePlanner{}
	blueprint, identityErr := recordingCacheSnapshot(version, texts, polish, runtimeID)
	if identityErr == nil {
		for tabIndex, states := range tabs {
			if tabIndex >= len(blueprint.Tabs) || len(blueprint.Tabs[tabIndex].Sources) != len(states) {
				identityErr = errors.New("话术缓存源句与当前供料分段不一致")
				break
			}
			for index, state := range states {
				if blueprint.Tabs[tabIndex].Sources[index] != state.Source {
					identityErr = errors.New("话术缓存源句与当前供料分段不一致")
					break
				}
			}
		}
	}
	if identityErr != nil {
		cacheOK = false
		blueprint = nil
		log.Printf("recording incremental identity unavailable tenant=%d room=%d; supply without persisted reuse: %v", tenant, room, identityErr)
	}
	if cacheOK {
		if err := waitLiveLeasePermit(ctx); err != nil {
			return err
		}
		loaded, loadedPlanner, stats, err := openRecordingIncrementalCache(cacheScope, blueprint)
		if err != nil {
			if errors.Is(err, errRecordingCacheSuperseded) {
				return err
			}
			cacheOK = false
			log.Printf("recording incremental cache load failed tenant=%d room=%d; retain normal supply: %v", tenant, room, err)
		}
		if loaded != nil {
			cacheManifest = loaded
			planner = loadedPlanner
			hit = recordingScopedCacheLookup(loaded)
			log.Printf("recording incremental cache loaded tenant=%d room=%d session=%d version=%d sources=%d inherited_versions=%d invalidated_versions=%d legacy_exact=%t", tenant, room, runtimeID, version.ID, len(hit), stats.InheritedVersions, stats.InvalidVersions, stats.LegacyExact)
		}
	}
	tabIDs := recordingStableTabIDs(version, texts)
	startedAt := queue.startedAt
	if s.store != nil {
		if session, err := s.store.GetLiveRuntimeSession(ctx, tenant, runtimeID); err == nil && session.RoomID == room && !session.StartedAt.IsZero() {
			startedAt = session.StartedAt
		}
	}
	startRound, startTabPos, startIndex := 0, 0, 0
	var order []int
	checkpoint, hasCheckpoint := loadRecordingSupplyCheckpoint(tenant, room, runtimeID)
	if hasCheckpoint && !recordingSupplyCheckpointMatches(checkpoint, version.ID, len(tabs)) {
		hasCheckpoint = false
	}
	if hasCheckpoint {
		startRound, startTabPos, startIndex = checkpoint.Round, checkpoint.TabPos, checkpoint.Index
		order = append([]int(nil), checkpoint.TabOrder...)
		seq, previousLast = checkpoint.Sequence, checkpoint.PreviousLast
		queue.blockSeq, queue.programID, queue.generation = checkpoint.BlockSeq, checkpoint.ProgramID, checkpoint.Generation
		log.Printf("recording stream resume checkpoint tenant=%d room=%d session=%d version=%d round=%d tab=%d index=%d block=%d", tenant, room, runtimeID, version.ID, startRound, startTabPos, startIndex, queue.blockSeq)
	}
	saveCheckpointWithPending := func(round, tabPos, index, blockSeq int, pending *recordingSupplyPendingAppend) error {
		err := saveRecordingSupplyCheckpoint(recordingSupplyCheckpoint{
			TenantID: tenant, RoomID: room, SessionID: runtimeID, VersionID: version.ID,
			ProgramID: queue.programID, Generation: queue.generation,
			Round: round, TabOrder: append([]int(nil), order...), TabPos: tabPos, Index: index,
			Sequence: seq, BlockSeq: blockSeq, PreviousLast: previousLast,
			Pending: pending,
		})
		if err != nil {
			log.Printf("recording stream checkpoint save failed tenant=%d room=%d session=%d: %v", tenant, room, runtimeID, err)
		}
		return err
	}
	saveCheckpoint := func(round, tabPos, index int) {
		_ = saveCheckpointWithPending(round, tabPos, index, queue.blockSeq, nil)
	}
	if hasCheckpoint && checkpoint.Pending != nil {
		if err := s.replayPendingRecordingSupply(ctx, tenant, room, runtimeID, version, &queue, checkpoint.Pending); err != nil {
			return err
		}
		checkpoint.Pending = nil
		saveCheckpoint(startRound, startTabPos, startIndex)
	}
	checkpointRound, checkpointTabPos, checkpointIndex := startRound, startTabPos, startIndex
	queue.beforeAppend = func(_ context.Context, track coreclient.ProgramRefreshTrack) error {
		return saveCheckpointWithPending(checkpointRound, checkpointTabPos, checkpointIndex+1, queue.blockSeq+1, &recordingSupplyPendingAppend{
			Track: track, ProgramID: queue.programID, BlockSeq: queue.blockSeq + 1,
		})
	}
	queue.afterAppend = func(_ context.Context, _ coreclient.ProgramRefreshTrack) error {
		return saveCheckpointWithPending(checkpointRound, checkpointTabPos, checkpointIndex+1, queue.blockSeq+1, nil)
	}
	cacheCandidates := func(tabIndex, index int) []recordingcache.Entry {
		if blueprint == nil {
			return nil
		}
		return recordingCacheCandidates(hit, blueprint.Tabs[tabIndex].UnitKeys[index], blueprint.Tabs[tabIndex].ContextKeys[index])
	}
	saveCache := func() error {
		if !cacheOK || cacheManifest == nil {
			return nil
		}
		cacheManifest.UpdateState, _ = json.Marshal(planner)
		saved, err := cacheScope.SaveActive(cacheManifest)
		if err != nil {
			log.Printf("recording stream cache save failed tenant=%d room=%d: %v", tenant, room, err)
			return nil // Successful paid media is still supplied; never buy it again.
		}
		if !saved {
			return errRecordingCacheSuperseded
		}
		return nil
	}

	for round := startRound; ; round++ {
		queue.delivered = 0
		checkpointRound = round
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := waitLiveLeasePermit(ctx); err != nil {
			return err
		}
		if len(order) == 0 || round != startRound {
			order = recordingStreamOrder(len(tabs), previousLast, recordingStreamPinned(version), rng)
		}
		refreshSchedule, updatePlans := s.prepareRecordingRefreshSchedule(ctx, tenant, room, version, polish, tabs, cacheCandidates,
			&planner, tabIDs, fmt.Sprintf("%d:%d", runtimeID, round), time.Since(startedAt), round == 0, rng)
		for tabPos, tabIndex := range order {
			checkpointTabPos = tabPos
			if round == startRound && tabPos < startTabPos {
				continue
			}
			roundPercent := refreshSchedule.Budget.Percent
			updatePlan := updatePlans[tabIndex]
			refresh := updatePlan.Selected
			supplyStats = newRecordingSupplyStats(tenant, room, runtimeID, version.PlanID, round+1, tabIDs[tabIndex], tabs[tabIndex], refresh, roundPercent, roundPercent)
			supplyStats.setSchedule(refreshSchedule)
			for index := range tabs[tabIndex] {
				checkpointIndex = index
				if round == startRound && tabPos == startTabPos && index < startIndex {
					continue
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := waitLiveLeasePermit(ctx); err != nil {
					return err
				}
				state := &tabs[tabIndex][index]
				if refresh[index] {
					if !s.admitRecordingRefresh(ctx, refreshSchedule, tenant, room, tabIndex, index) || !planner.BeginAttempt(updatePlan, index) {
						refresh[index] = false // Valid cache/original supply continues without optional work.
					}
				}
				priority, reason := livecost.PriorityNecessary, "当前事实和音色下缺少可播放声音，补充基础供料"
				if refresh[index] {
					priority, reason = livecost.PriorityOptional, "话术优先跨轮更新"
				}
				callScope := liveCostCallScope{TenantID: tenant, RoomID: room, PlanID: version.PlanID, SessionID: runtimeID,
					RunID: fmt.Sprint(runtimeID), Priority: priority, Reason: reason,
					Key:         fmt.Sprintf("recording:%d:%d:%s:%d:%d", runtimeID, version.ID, tabIDs[tabIndex], round, index),
					Attribution: &livecost.Attribution{Source: "recording_mainline", Round: round + 1, TabID: tabIDs[tabIndex], SourceIndex: index}, Events: &liveCostCallEvents{}}
				callCtx := withLiveCostScope(ctx, callScope)
				unitKey, contextKey := "", ""
				if blueprint != nil {
					unitKey = blueprint.Tabs[tabIndex].UnitKeys[index]
					contextKey = blueprint.Tabs[tabIndex].ContextKeys[index]
				}
				candidates := recordingCacheCandidates(hit, unitKey, contextKey)
				result, err := resolveRecordingReuse(callCtx, recordingReuseRequest{
					Versions: state.Versions, Cached: candidates, Refresh: refresh[index], RNG: rng,
					BuildCache: func(c context.Context, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
						units, ok := s.rebuildRecordingUnitsFromCache(c, tenant, entry)
						if !ok {
							return nil, false
						}
						for _, unit := range units {
							if _, err := approvedRecordingSpoken(polish, unit.Text); err != nil {
								return nil, false
							}
						}
						return units, true
					},
					Generate: func(c context.Context) ([]recordingReadyUnit, error) {
						var units []recordingReadyUnit
						err := liveContinuityAttempt(func() error {
							var err error
							if refresh[index] {
								units, err = s.compileRecordingSentence(c, tenant, version, polish, state.Source, tabIndex, &seq, recordingKnownSpokenVersions(state.Source, state.Versions, candidates, polish)...)
							} else {
								units, err = s.compileRecordingOriginal(c, tenant, version, polish, state.Source, tabIndex, &seq)
								if errors.Is(err, errRecordingOriginalRejected) {
									units, err = s.compileRecordingSentence(c, tenant, version, polish, state.Source, tabIndex, &seq)
								}
							}
							return err
						})
						return units, err
					},
				})
				// The text polisher may return the original on a declined model
				// request. Retain the real cost decision rather than reporting it
				// as a model-generated unchanged expression.
				if callScope.Events.OptionalDeclined.Load() {
					result.GenerationErr = errLiveCostOptionalDeclined
				}
				if refresh[index] {
					supplyStats.RefreshAttempts++
					refreshSchedule.complete(tabIndex, index, callScope.Events)
					if !result.Generated {
						supplyStats.ReplacementCandidates += refreshSchedule.replace(&planner, updatePlan, tabIndex, index)
					}
				}
				supplyStats.setSchedule(refreshSchedule)
				supplyStats.observe(state.Source, refresh[index], result, err)
				if err != nil {
					if liveContinuityTerminal(ctx, err) {
						return err
					}
					log.Printf("recording stream sentence unavailable tenant=%d room=%d tab=%d index=%d err=%v", tenant, room, tabIndex, index, err)
					continue
				}
				state.Versions = result.Versions
				if result.GenerationErr != nil {
					log.Printf("recording stream previous retained tenant=%d room=%d tab=%d index=%d err=%v", tenant, room, tabIndex, index, result.GenerationErr)
				}
				if result.Generated {
					state.Refreshed = round
					if refresh[index] {
						planner.Commit(updatePlan, index)
					}
					if entry, ok := recordingCacheEntryFromUnits(state.Source, result.Units); cacheOK && ok {
						entry.Polished = !sameSpeechText(state.Source, strings.Join(recordingUnitTexts(result.Units), ""))
						entry.UnitKey, entry.ContextKey = unitKey, contextKey
						cacheManifest.Entries = pruneRecordingCacheEntries(append(cacheManifest.Entries, entry))
						hit = recordingScopedCacheLookup(cacheManifest)
					}
				} else if result.CacheHit && result.CacheIndex >= 0 {
					pick := candidates[result.CacheIndex]
					for i := range cacheManifest.Entries {
						entry := &cacheManifest.Entries[i]
						if entry.UnitKey == pick.UnitKey && entry.Sentence == pick.Sentence && entry.AssetID == pick.AssetID && entry.UpdatedAt == pick.UpdatedAt {
							entry.Hits++
							break
						}
					}
				}
				if err := saveCache(); err != nil {
					return err
				}
				// The resolver selects a newly generated version directly this round.
				queue.pending = append(queue.pending, result.Units...)
				deliveredBefore := queue.delivered
				if err := queue.flush(ctx, false); err != nil {
					return err
				}
				nextIndex := index
				if queue.delivered > deliveredBefore {
					nextIndex = index + 1
				}
				saveCheckpoint(round, tabPos, nextIndex)
			}
			if err := saveCache(); err != nil {
				return err
			}
			supplyStats.write(true)
			supplyStats = nil
		}
		// A short script cannot supply sixty unique seconds. Start its complete
		// first pass instead of manufacturing repeated startup copies forever.
		if err := queue.flush(ctx, true); err != nil {
			return err
		}
		saveCheckpoint(round, len(order), 0)
		if queue.delivered == 0 {
			if err := supplyWait(ctx, 16*time.Second); err != nil {
				return err
			}
		}
		previousLast = order[len(order)-1]
	}
}

var errRecordingOriginalRejected = errors.New("原句未通过P0，需要改写")

// compileRecordingOriginal 让原文 A 在通过 P0 后直接合成，不先调用润色。
// 只有明确的 P0 内容拒绝才返回 errRecordingOriginalRejected；
// 审核状态不可用、存储或 TTS 错误不能被误判成“需要润色”。
func (s *Server) compileRecordingOriginal(ctx context.Context, tenant int64, version model.LiveAgentPlanVersion, polish *anchorcycle.State, source string, tabIndex int, seq *int) ([]recordingReadyUnit, error) {
	if polish == nil {
		return nil, errors.New("原句审核状态不可用")
	}
	candidate := anchorcycle.Candidate{ID: fmt.Sprintf("recording-original-%d", *seq), Text: source, OutputText: source}
	if err := polish.CheckP0Candidate(&candidate); err != nil {
		return nil, err
	}
	if candidate.P0Report == nil {
		return nil, errors.New("原句未取得P0审核回执")
	}
	if !candidate.P0Report.Passed {
		return nil, errRecordingOriginalRejected
	}
	approved := source
	if text := strings.TrimSpace(candidate.OutputText); text != "" {
		approved = text
	}
	if err := polish.CheckApprovedText(candidate, approved); err != nil {
		return nil, err
	}
	return s.compileRecordingSpoken(ctx, tenant, version, approved, tabIndex, seq)
}

func (s *Server) compileRecordingSentence(ctx context.Context, tenant int64, version model.LiveAgentPlanVersion, polish *anchorcycle.State, source string, tabIndex int, seq *int, existing ...string) ([]recordingReadyUnit, error) {
	// The active planner already chose a rewrite. The retired original-base
	// switch must not silently veto every optional update forever.
	spoken := s.polishRecordingRefreshText(ctx, polish, source, existing...)
	if strings.TrimSpace(spoken) == "" {
		spoken = source
	}
	// An optional rewrite that failed or echoed the original must reuse the
	// existing audio instead of buying another identical TTS rendition.
	if err := recordingRefreshNoveltyError(ctx, source, spoken, existing...); err != nil {
		return nil, err
	}
	if !sameNumbers(source, spoken) || suspiciousPolish(source, spoken) {
		return nil, errors.New("改写改变了原文信息，保留已有声音")
	}
	approved, err := approvedRecordingSpoken(polish, spoken)
	if err != nil {
		return nil, err
	}
	return s.compileRecordingSpoken(ctx, tenant, version, approved, tabIndex, seq)
}

// compileRecordingSpoken 把已经润色好的正文切句并逐句合成。
// 批量润色路径先拿到正文，再走这里，避免重复润色。
func (s *Server) compileRecordingSpoken(ctx context.Context, tenant int64, version model.LiveAgentPlanVersion, spoken string, tabIndex int, seq *int) ([]recordingReadyUnit, error) {
	sentences := splitRecordingSentences(spoken)
	if len(sentences) == 0 {
		sentences = []string{strings.TrimSpace(spoken)}
	}
	// 长句在逗号处再拆一层：整句 40 秒以上时，Core 的 35 秒窗口里没有
	// 句界，互动插不进去。拆出来的每一段仍是完整小句，各自成一条 track。
	parts := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		for _, chunk := range splitRecordingLongSentence(sentence, recordingSentenceMaxRunes) {
			// 拆一次仍超长（逗号太稀疏）就再拆一层，保证每条 track 够短。
			parts = append(parts, splitRecordingLongSentence(chunk, recordingSentenceMaxRunes*3/4)...)
		}
	}
	units := make([]recordingReadyUnit, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		// 送 TTS 前保证句尾是句末标点：没有句号的句子合成后整条轨道
		// 找不到安全切点，会被归档拒绝并整句跳过。
		if !strings.ContainsRune("。！？；!?;", []rune(text)[len([]rune(text))-1]) {
			text += "。"
		}
		track, err := s.synthesizeRecordingStreamSegment(ctx, tenant, version, text, *seq, tabIndex)
		if err != nil {
			return units, err
		}
		*seq++
		units = append(units, recordingReadyUnit{Text: text, Track: track})
	}
	return units, nil
}

func toRecordingCacheEntries(units []recordingReadyUnit) []recordingStreamCacheEntry {
	entries := make([]recordingStreamCacheEntry, 0, len(units))
	for _, unit := range units {
		entries = append(entries, recordingStreamCacheEntry{Text: unit.Text, Track: unit.Track})
	}
	return entries
}

// recordingSentenceMaxRunes 是单条 track 的目标上限（约 25 秒）。
const recordingSentenceMaxRunes = 110

// splitRecordingLongSentence 把过长的句子在逗号/顿号/冒号处拆成若干小句，
// 拆出来的每一段都以句末标点结尾，保证每条 track 都有可插入的句界。
func splitRecordingLongSentence(text string, maxRunes int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	runes := []rune(trimmed)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return []string{trimmed}
	}
	const softMarks = "，、：,:"
	var out []string
	start := 0
	for start < len(runes) {
		remaining := len(runes) - start
		if remaining <= maxRunes {
			out = append(out, terminateRecordingSentence(string(runes[start:])))
			break
		}
		cut := -1
		for i := start + maxRunes; i >= start+maxRunes/2; i-- {
			if strings.ContainsRune(softMarks, runes[i]) {
				cut = i
				break
			}
		}
		if cut < 0 {
			// 前半段没有标点就继续往前找，宁可短一点也不要硬切。
			for i := start + maxRunes - 1; i > start; i-- {
				if strings.ContainsRune(softMarks, runes[i]) {
					cut = i
					break
				}
			}
		}
		end, next := cut, cut+1
		if cut < 0 {
			// A hard boundary is between characters, unlike a punctuation cut.
			// Do not drop the boundary character or divide a price/quantity.
			end = start + maxRunes
			numeric := func(r rune) bool { return unicode.IsDigit(r) || strings.ContainsRune(".．%％", r) }
			if end < len(runes) && numeric(runes[end-1]) && numeric(runes[end]) {
				for end > start && numeric(runes[end-1]) {
					end--
				}
				if end == start {
					end = start + maxRunes
					for end < len(runes) && numeric(runes[end]) {
						end++
					}
				}
			}
			next = end
		}
		out = append(out, terminateRecordingSentence(string(runes[start:end])))
		start = next
		for start < len(runes) && strings.ContainsRune(" 　", runes[start]) {
			start++
		}
	}
	return out
}

// terminateRecordingSentence 去掉句尾的逗号类标点并补句号，保证结尾是句界。
func terminateRecordingSentence(text string) string {
	trimmed := strings.TrimSpace(text)
	trimmed = strings.TrimRight(trimmed, "，、：,:;； 　")
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if !strings.ContainsRune("。！？!?", runes[len(runes)-1]) {
		trimmed += "。"
	}
	return trimmed
}

// 起播缓冲目标：90 秒；合成过慢时退到 60 秒。
// 45 秒缓冲会被尾段 TTS 抖动吃穿，导致播放静音。
const (
	recordingStartBufferMS   = 90000
	recordingStartFallbackMS = 60000
	recordingStartSlowWait   = 120 * time.Second
)

// recordingStartReady 判断是否已经攒够开播所需的音频。
func recordingStartReady(pending []recordingReadyUnit, startAt time.Time) bool {
	total := 0
	for _, unit := range pending {
		total += int(unit.Track.DurationMS)
	}
	if total >= recordingStartBufferMS {
		return true
	}
	if time.Since(startAt) >= recordingStartSlowWait {
		return total >= recordingStartFallbackMS
	}
	return false
}
