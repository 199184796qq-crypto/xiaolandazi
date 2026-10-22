package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
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
	Source    string
	Versions  [][]recordingReadyUnit
	Refreshed int
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

// recordingSentenceRefresh 选出本轮要重新润色合成的句索引。
func recordingSentenceRefresh(states []recordingSentenceState, round int, rng *rand.Rand) []bool {
	set := make([]bool, len(states))
	if len(set) == 0 {
		return set
	}
	if round < 4 {
		for index := range set {
			set[index] = true
		}
		return set
	}
	want := (len(set)*30 + 99) / 100
	if want < 1 {
		want = 1
	}
	if want > len(set) {
		want = len(set)
	}
	order := rng.Perm(len(set))
	sort.SliceStable(order, func(i, j int) bool {
		return states[order[i]].Refreshed < states[order[j]].Refreshed
	})
	for i := 0; i < want; i++ {
		set[order[i]] = true
	}
	return set
}

func (s *Server) runRecordingStreamProducer(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
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
	programID, generation := "", uint64(0)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	seq, blockSeq, previousLast := 0, 0, -1

	// 开播前把本方案这一版话术的缓存清单读一次，之后整场走内存。
	// 命中就不润色、不合成，直接复用已有音轨。
	cacheScope, cacheOK := recordingCacheScopeFor(tenant, room, version.PlanID)
	voiceKey := recordingcache.VoiceKey(version.VoiceIdentity.VoiceID, version.VoiceIdentity.Model, fmt.Sprint(version.VoiceIdentity.Rate))
	scriptHash := recordingcache.ScriptHash(texts)
	var cacheManifest *recordingcache.Manifest
	hit := map[string][]recordingcache.Entry{}
	if cacheOK {
		loaded, err := cacheScope.Load(voiceKey, scriptHash)
		if err != nil {
			log.Printf("recording stream cache load failed tenant=%d room=%d: %v", tenant, room, err)
		} else if loaded != nil {
			cacheManifest = loaded
			hit = recordingCacheLookup(loaded)
			log.Printf("recording stream cache loaded tenant=%d room=%d entries=%d", tenant, room, len(hit))
		}
	}

	for round := 0; ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		order := recordingStreamOrder(len(tabs), previousLast, rng)
		for _, tabIndex := range order {
			refresh := recordingSentenceRefresh(tabs[tabIndex], round, rng)

			// 每 5 句合并成一次润色调用：固定前缀只付一次，成本降到原来的 1/5。
			// 返回按行拆回，行数或数字不一致的句子回退原文。
			batched := map[int]string{}
			for start := 0; start < len(tabs[tabIndex]); start += recordingPolishBatchSize {
				end := start + recordingPolishBatchSize
				if end > len(tabs[tabIndex]) {
					end = len(tabs[tabIndex])
				}
				need := false
				for i := start; i < end; i++ {
					if refresh[i] || len(tabs[tabIndex][i].Versions) == 0 {
						need = true
						break
					}
				}
				if !need {
					continue
				}
				sources := make([]string, 0, end-start)
				for i := start; i < end; i++ {
					sources = append(sources, tabs[tabIndex][i].Source)
				}
				polished := s.polishRecordingSentenceBatch(ctx, polish, sources)
				for i := start; i < end; i++ {
					if text := strings.TrimSpace(polished[i-start]); text != "" {
						batched[i] = text
					}
				}
			}

			// 本轮这个标签要播的就绪单元；游标显式记录未消费范围。
			pending := make([]recordingReadyUnit, 0, len(tabs[tabIndex]))
			for index := range tabs[tabIndex] {
				if err := ctx.Err(); err != nil {
					return err
				}
				state := &tabs[tabIndex][index]
				if refresh[index] || len(state.Versions) == 0 {
					if versions := hit[state.Source]; shouldReuseCachedVersion(len(versions), rng) {
						pick := versions[rng.Intn(len(versions))]
						if units, ok := s.rebuildRecordingUnitsFromCache(ctx, tenant, pick); ok && len(units) > 0 {
							if cacheManifest != nil {
								for i := range cacheManifest.Entries {
									if cacheManifest.Entries[i].Sentence == pick.Sentence && cacheManifest.Entries[i].AssetID == pick.AssetID {
										cacheManifest.Entries[i].Hits++
										break
									}
								}
							}
							log.Printf("recording stream cache hit tenant=%d room=%d tab=%d index=%d versions=%d hits=%d", tenant, room, tabIndex, index, len(versions), pick.Hits+1)
							pending = append(pending, units...)
							continue
						}
					}
					var units []recordingReadyUnit
					var err error
					if spoken, ok := batched[index]; ok {
						units, err = s.compileRecordingSpoken(ctx, tenant, version, spoken, tabIndex, &seq)
					} else {
						units, err = s.compileRecordingSentence(ctx, tenant, version, polish, state.Source, tabIndex, &seq)
					}
					if err != nil {
						// 单元级失败：有旧版本就沿用，没有就跳过这一句并留痕。
						if len(state.Versions) == 0 {
							log.Printf("recording stream sentence skipped tenant=%d room=%d tab=%d index=%d err=%v", tenant, room, tabIndex, index, err)
							continue
						}
						log.Printf("recording stream sentence kept previous tenant=%d room=%d tab=%d index=%d err=%v", tenant, room, tabIndex, index, err)
					} else if len(units) > 0 {
						state.Versions = append(state.Versions, units)
						if len(state.Versions) > 4 {
							state.Versions = state.Versions[len(state.Versions)-4:]
						}
						state.Refreshed = round
						// 只有真正被改写过的句子才进缓存；回退原文的条目进了缓存
						// 会把“没润色”固化成永久结果。
						if cacheOK && len(units) > 0 && strings.TrimSpace(units[0].Text) != strings.TrimSpace(state.Source) {
							if entry, ok := recordingCacheEntryFromUnits(state.Source, units); ok {
								entry.Polished = true
								if cacheManifest == nil {
									cacheManifest = &recordingcache.Manifest{Version: 2, VoiceKey: voiceKey, ScriptHash: scriptHash}
								}
								cacheManifest.Entries = append(cacheManifest.Entries, entry)
								cacheManifest.Entries = pruneRecordingCacheEntries(cacheManifest.Entries)
								hit = recordingCacheLookup(cacheManifest)
								if err := cacheScope.Save(cacheManifest); err != nil {
									log.Printf("recording stream cache save failed tenant=%d room=%d: %v", tenant, room, err)
								}
							}
						}
					}
				}
				if len(state.Versions) == 0 {
					continue
				}
				pending = append(pending, state.Versions[rng.Intn(len(state.Versions))]...)

				// 首块只等前 3 个就绪单元，不把整标签全部合成完才开播。
				// 否则 50 句的标签会让观众先听几十秒静音，第一句迟迟不出声。
				if programID == "" && len(pending) >= 3 {
					track, consumed, err := s.combineRecordingStreamBlockSafe(ctx, tenant, version, toRecordingCacheEntries(pending[:3]), seq, blockSeq)
					if err != nil {
						return err
					}
					blockSeq++
					pending = pending[consumed:]
					if track.AudioURL == "" {
						log.Printf("recording stream first block skipped tenant=%d room=%d tab=%d units=%d", tenant, room, tabIndex, consumed)
					} else {
						snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, track)
						if err != nil {
							return err
						}
						programID, generation = snapshot.ProgramID, snapshot.Generation
						log.Printf("recording stream first block started tenant=%d room=%d tab=%d units=%d program=%s", tenant, room, tabIndex, consumed, programID)
						s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
					}
				}
			}

			// 合块追加：每块最多 3 个就绪单元；块内切点由句界保证。
			cursor := 0
			for cursor < len(pending) {
				if err := ctx.Err(); err != nil {
					return err
				}
				if programID != "" {
					if err := waitRecordingStreamBuffer(ctx, s, tenant, room, programID, version.ID); err != nil {
						return err
					}
				}
				take := 3
				if remaining := len(pending) - cursor; remaining < take {
					take = remaining
				}
				track, consumed, err := s.combineRecordingStreamBlockSafe(ctx, tenant, version, toRecordingCacheEntries(pending[cursor:cursor+take]), seq, blockSeq)
				if err != nil {
					return err
				}
				blockSeq++
				cursor += consumed
				if track.AudioURL == "" {
					log.Printf("recording stream block skipped tenant=%d room=%d tab=%d round=%d units=%d", tenant, room, tabIndex, round, consumed)
					continue
				}
				if programID == "" {
					snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, track)
					if err != nil {
						return err
					}
					programID, generation = snapshot.ProgramID, snapshot.Generation
					log.Printf("recording stream program started tenant=%d room=%d program=%s", tenant, room, programID)
					s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
				} else {
					nextGeneration, err := s.appendRecordingStreamTrack(ctx, tenant, room, version, programID, track)
					if err != nil {
						return err
					}
					generation = nextGeneration
				}
				log.Printf("recording stream block appended tenant=%d room=%d tab=%d round=%d units=%d safe_points=%d generation=%d", tenant, room, tabIndex, round, consumed, len(track.SafePoints), generation)
			}
		}
		previousLast = order[len(order)-1]
	}
}

// compileRecordingSentence 把一句话润色后重新切句、逐句合成，返回就绪单元。
func (s *Server) compileRecordingSentence(ctx context.Context, tenant int64, version model.LiveAgentPlanVersion, polish *anchorcycle.State, source string, tabIndex int, seq *int) ([]recordingReadyUnit, error) {
	spoken := polishRecordingText(ctx, s, polish, source)
	if strings.TrimSpace(spoken) == "" {
		spoken = source
	}
	return s.compileRecordingSpoken(ctx, tenant, version, spoken, tabIndex, seq)
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
		if cut < 0 {
			cut = start + maxRunes - 1
		}
		out = append(out, terminateRecordingSentence(string(runes[start:cut])))
		start = cut + 1
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
