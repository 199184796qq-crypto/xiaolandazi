package httpapi

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"sort"
	"strings"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

// 话术优先的多标签播放链路。
//
// 规则（与产品口径一致）：
//   - 每个文本框是一个独立单元，自己记账：播过几次、每段最近 4 个版本、每段上次刷新的轮次。
//   - 每一轮把全部文本框随机重排一次，每个文本框恰好播一遍；跨轮不允许同一个框紧挨着。
//   - 前 4 次播放：每段都重新润色合成。
//   - 第 5 次起：每段 30%（向上取整、至少 1 段）重新润色，其余复用该段最近 4 个版本里的一个；
//     被刷新的段记下轮次，下一轮优先挑最久没刷过的，保证轮换。
//   - 框与框之间按普通句子衔接，不加额外停顿。
type recordingTabRun struct {
	segments  []recordingStreamSegment
	versions  [][]coreclient.ProgramRefreshTrack
	refreshed []int
}

// recordingStreamTexts 取本次要播的正文：优先用快照里的多标签，旧快照按单标签退化。
func recordingStreamTexts(version model.LiveAgentPlanVersion) []string {
	if script, ok := recordingScriptFromContext(version.GenerationContext); ok {
		texts := make([]string, 0, len(script.Tabs))
		for _, tab := range script.Tabs {
			if text := strings.TrimSpace(tab.Text); text != "" {
				texts = append(texts, text)
			}
		}
		if len(texts) > 0 {
			return texts
		}
	}
	for _, variant := range version.Variants {
		if variant.IsFormal && strings.TrimSpace(variant.Text) != "" {
			return []string{strings.TrimSpace(variant.Text)}
		}
	}
	return nil
}

// recordingStreamOrder 生成一轮的播放顺序；跨轮不重复上一轮的最后一个框。
func recordingStreamOrder(count, previousLast int, rng *rand.Rand) []int {
	order := rng.Perm(count)
	if count < 2 || previousLast < 0 || order[0] != previousLast {
		return order
	}
	swap := 1 + rng.Intn(count-1)
	order[0], order[swap] = order[swap], order[0]
	return order
}

// recordingStreamRefreshSet 决定这一轮哪些段要重新润色。
func recordingStreamRefreshSet(tab *recordingTabRun, round int, rng *rand.Rand) []bool {
	set := make([]bool, len(tab.segments))
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
		return tab.refreshed[order[i]] < tab.refreshed[order[j]]
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
	tabs := make([]recordingTabRun, 0, len(texts))
	total := 0
	for _, text := range texts {
		segments := splitRecordingStreamText(text)
		if len(segments) == 0 {
			continue
		}
		run := recordingTabRun{
			segments:  segments,
			versions:  make([][]coreclient.ProgramRefreshTrack, len(segments)),
			refreshed: make([]int, len(segments)),
		}
		for index := range run.refreshed {
			run.refreshed[index] = -1
		}
		tabs = append(tabs, run)
		total += len(segments)
	}
	if len(tabs) == 0 {
		return errors.New("话术优先分段为空")
	}
	log.Printf("recording stream start tenant=%d room=%d version=%d tabs=%d segments=%d", tenant, room, version.ID, len(tabs), total)

	polish := s.recordingPolishState(ctx, tenant, version.PlanID, room)
	programID, generation := "", uint64(0)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	seq := 0
	blockSeq := 0
	previousLast := -1

	for round := 0; ; round++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		order := recordingStreamOrder(len(tabs), previousLast, rng)
		for _, tabIndex := range order {
			tab := &tabs[tabIndex]
			refresh := recordingStreamRefreshSet(tab, round, rng)
			current := make([]recordingStreamCacheEntry, 0, len(tab.segments))
			blockStart := 0
			for index, segment := range tab.segments {
				if err := ctx.Err(); err != nil {
					return err
				}
				if programID != "" {
					if err := waitRecordingStreamBuffer(ctx, s, tenant, room, programID, version.ID); err != nil {
						return err
					}
				}
				var entry recordingStreamCacheEntry
				if refresh[index] || len(tab.versions[index]) == 0 {
					text := polishRecordingText(ctx, s, polish, segment.Text)
					track, err := s.synthesizeRecordingStreamSegment(ctx, tenant, version, text, seq, tabIndex)
					if err != nil {
						return err
					}
					seq++
					entry = recordingStreamCacheEntry{Text: text, Track: track}
					tab.versions[index] = append(tab.versions[index], track)
					if len(tab.versions[index]) > 4 {
						tab.versions[index] = tab.versions[index][len(tab.versions[index])-4:]
					}
					tab.refreshed[index] = round
					log.Printf("recording stream segment polished tenant=%d room=%d tab=%d round=%d index=%d chars=%d", tenant, room, tabIndex, round, index, len([]rune(text)))
				} else {
					pool := tab.versions[index]
					entry = recordingStreamCacheEntry{Text: segment.Text, Track: pool[rng.Intn(len(pool))]}
					log.Printf("recording stream segment reused tenant=%d room=%d tab=%d round=%d index=%d versions=%d", tenant, room, tabIndex, round, index, len(pool))
				}
				current = append(current, entry)

				blockTarget := recordingStreamBlockTarget(blockStart)
				if len(current)-blockStart >= blockTarget || index == len(tab.segments)-1 {
					block, err := s.combineRecordingStreamBlock(ctx, tenant, version, current[blockStart:], seq, blockSeq)
					blockSeq++
					if err != nil {
						return err
					}
					if programID == "" {
						snapshot, err := s.startRecordingStreamProgram(ctx, tenant, room, runtimeID, version, block)
						if err != nil {
							return err
						}
						programID, generation = snapshot.ProgramID, snapshot.Generation
						log.Printf("recording stream program started tenant=%d room=%d program=%s", tenant, room, programID)
						s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
					} else {
						nextGeneration, err := s.appendRecordingStreamTrack(ctx, tenant, room, version, programID, block)
						if err != nil {
							return err
						}
						generation = nextGeneration
					}
					log.Printf("recording stream block appended tenant=%d room=%d tab=%d round=%d chunks=%d generation=%d", tenant, room, tabIndex, round, len(current)-blockStart, generation)
					blockStart = len(current)
				}
			}
		}
		previousLast = order[len(order)-1]
	}
}
