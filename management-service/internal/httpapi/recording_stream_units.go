package httpapi

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
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

	for round := 0; ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		order := recordingStreamOrder(len(tabs), previousLast, rng)
		for _, tabIndex := range order {
			refresh := recordingSentenceRefresh(tabs[tabIndex], round, rng)

			// 本轮这个标签要播的就绪单元；游标显式记录未消费范围。
			pending := make([]recordingReadyUnit, 0, len(tabs[tabIndex]))
			for index := range tabs[tabIndex] {
				if err := ctx.Err(); err != nil {
					return err
				}
				state := &tabs[tabIndex][index]
				if refresh[index] || len(state.Versions) == 0 {
					units, err := s.compileRecordingSentence(ctx, tenant, version, polish, state.Source, tabIndex, &seq)
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
					}
				}
				if len(state.Versions) == 0 {
					continue
				}
				pending = append(pending, state.Versions[rng.Intn(len(state.Versions))]...)
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
	parts := splitRecordingSentences(spoken)
	if len(parts) == 0 {
		parts = []string{strings.TrimSpace(spoken)}
	}
	units := make([]recordingReadyUnit, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
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
