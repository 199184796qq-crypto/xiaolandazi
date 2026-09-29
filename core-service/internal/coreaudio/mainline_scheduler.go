package coreaudio

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	mainlinePlanLeadMS     = 5000
	mainlinePrefetchMaxB   = 32 << 20
	mainlineRecentHistoryN = 4
)

type MainlineAgentSignals struct {
	Heat                string
	Progress            string
	Atmosphere          string
	QuestionPressure    float64
	Orders30s           int
	OrderSignals30s     int
	NegativeFeedback30s int
	TopTopics           []string
}

type MainlineAgentSignalProvider func(roomID int64) MainlineAgentSignals

type scoredMainlineTrack struct {
	Index  int
	Score  int
	Reason []string
}

func (c *Client) SetMainlineAgentSignalProvider(provider MainlineAgentSignalProvider) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.mainlineSignals = provider
	c.mu.Unlock()
}

func (c *Client) planNextMainline(program *programState, taskID string) {
	if c == nil || program == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	c.mu.RLock()
	if c.programs[program.RoomID] != program || !program.Running || program.Suspended || program.CurrentTaskID != taskID {
		c.mu.RUnlock()
		return
	}
	if program.PlannedForTaskID == taskID && program.PlannedNextTrack >= 0 {
		c.mu.RUnlock()
		return
	}
	currentTrack := program.TrackIndex
	c.mu.RUnlock()

	nextIndex, score, reason := c.selectNextMainlineTrack(program, currentTrack)
	if nextIndex < 0 {
		return
	}

	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running || program.Suspended || program.CurrentTaskID != taskID {
		c.mu.Unlock()
		return
	}
	program.PlannedForTaskID = taskID
	program.PlannedNextTrack = nextIndex
	program.PlannedNextScore = score
	program.PlannedNextReason = reason
	nextID := program.Tracks[nextIndex].ID
	c.mu.Unlock()

	log.Printf(
		"[MAINLINE_AGENT] room=%d current=%s next=%s score=%d reason=%s",
		program.RoomID,
		program.Tracks[currentTrack].ID,
		nextID,
		score,
		reason,
	)
	c.prefetchMainlineTrack(program, taskID, nextIndex)
}

func (c *Client) selectNextMainlineTrack(program *programState, currentIndex int) (int, int, string) {
	if program == nil || len(program.Tracks) == 0 {
		return -1, 0, ""
	}
	if len(program.Tracks) == 1 {
		return 0, 0, "single-track"
	}

	c.mu.RLock()
	provider := c.mainlineSignals
	history := append([]string(nil), program.RecentTrackIDs...)
	playCounts := make(map[string]int, len(program.TrackPlayCount))
	for key, value := range program.TrackPlayCount {
		playCounts[key] = value
	}
	lastPlayed := make(map[string]time.Time, len(program.TrackLastPlayed))
	for key, value := range program.TrackLastPlayed {
		lastPlayed[key] = value
	}
	sequence := program.Sequence
	c.mu.RUnlock()

	signals := MainlineAgentSignals{}
	if provider != nil {
		signals = provider(program.RoomID)
	}
	now := c.now().UTC()
	scored := make([]scoredMainlineTrack, 0, len(program.Tracks))
	for index, track := range program.Tracks {
		if index == currentIndex {
			continue
		}
		score := 100
		reasons := []string{"base=100"}
		if count := playCounts[track.ID]; count > 0 {
			penalty := count * 12
			if penalty > 48 {
				penalty = 48
			}
			score -= penalty
			reasons = append(reasons, fmt.Sprintf("play-count=-%d", penalty))
		}
		for offset := 1; offset <= len(history); offset++ {
			if history[len(history)-offset] != track.ID {
				continue
			}
			penalty := 70 / offset
			score -= penalty
			reasons = append(reasons, fmt.Sprintf("recent-%d=-%d", offset, penalty))
			break
		}
		if last, ok := lastPlayed[track.ID]; ok {
			ageSeconds := int(now.Sub(last).Seconds())
			if ageSeconds > 0 {
				bonus := ageSeconds / 20
				if bonus > 24 {
					bonus = 24
				}
				score += bonus
				reasons = append(reasons, fmt.Sprintf("cooldown=+%d", bonus))
			}
		} else {
			score += 24
			reasons = append(reasons, "unplayed=+24")
		}

		text := strings.ToLower(track.Label + " " + track.Text)
		if strings.EqualFold(signals.Progress, "CONVERSION") || signals.Orders30s > 0 || signals.OrderSignals30s > 0 {
			if containsAnyMainline(text, "下单", "链接", "拍", "库存", "优惠", "到手", "购买", "价格", "赠", "送") {
				score += 32
				reasons = append(reasons, "conversion=+32")
			}
		}
		if strings.EqualFold(signals.Heat, "HOT") || strings.EqualFold(signals.Heat, "OVERHEATED") {
			if track.DurationMS > 0 && track.DurationMS <= 45000 {
				score += 10
				reasons = append(reasons, "hot-short=+10")
			}
		}
		if signals.QuestionPressure >= 0.45 {
			if containsAnyMainline(text, "为什么", "怎么", "哪里", "发货", "包邮", "退", "售后", "规格", "成分", "安全", "价格") {
				score += 14
				reasons = append(reasons, "question-pressure=+14")
			}
		}
		if signals.NegativeFeedback30s > 0 {
			if containsAnyMainline(text, "售后", "退", "退款", "放心", "安全", "保障", "正装", "不满意") {
				score += 18
				reasons = append(reasons, "trust-repair=+18")
			}
		}
		if topicBonus := mainlineTopicAffinity(text, signals.TopTopics); topicBonus > 0 {
			score += topicBonus
			reasons = append(reasons, fmt.Sprintf("topic=+%d", topicBonus))
		}
		jitter := deterministicMainlineJitter(program.RoomID, sequence, track.ID)
		score += jitter
		reasons = append(reasons, fmt.Sprintf("variety=%+d", jitter))
		scored = append(scored, scoredMainlineTrack{Index: index, Score: score, Reason: reasons})
	}
	if len(scored) == 0 {
		return currentIndex, 0, "fallback-current"
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].Score == scored[j].Score {
			return program.Tracks[scored[i].Index].ID < program.Tracks[scored[j].Index].ID
		}
		return scored[i].Score > scored[j].Score
	})
	best := scored[0]
	return best.Index, best.Score, strings.Join(best.Reason, ",")
}

func deterministicMainlineJitter(roomID int64, sequence uint64, trackID string) int {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%d:%d:%s", roomID, sequence, trackID)
	return int(h.Sum32()%13) - 6
}

func mainlineTopicAffinity(text string, topics []string) int {
	bonus := 0
	for _, topic := range topics {
		topic = strings.ToLower(strings.TrimSpace(topic))
		switch {
		case strings.Contains(topic, "价格") || strings.Contains(topic, "费用"):
			if containsAnyMainline(text, "价格", "多少钱", "到手", "优惠", "链接") {
				bonus += 12
			}
		case strings.Contains(topic, "发货") || strings.Contains(topic, "物流"):
			if containsAnyMainline(text, "发货", "快递", "包邮", "中通", "极兔", "邮政") {
				bonus += 12
			}
		case strings.Contains(topic, "售后") || strings.Contains(topic, "退款"):
			if containsAnyMainline(text, "售后", "退货", "退款", "不满意", "保障") {
				bonus += 12
			}
		case strings.Contains(topic, "成分") || strings.Contains(topic, "安全") || strings.Contains(topic, "品质"):
			if containsAnyMainline(text, "成分", "安全", "品质", "非转基因", "低芥酸", "原料") {
				bonus += 12
			}
		}
		if bonus >= 30 {
			return 30
		}
	}
	return bonus
}

func containsAnyMainline(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, strings.ToLower(value)) {
			return true
		}
	}
	return false
}

func (c *Client) prefetchMainlineTrack(program *programState, sourceTaskID string, trackIndex int) {
	if c == nil || program == nil || trackIndex < 0 || trackIndex >= len(program.Tracks) {
		return
	}
	track := program.Tracks[trackIndex]
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, track.AudioURL, nil)
		if err != nil {
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return
		}
		audio, err := io.ReadAll(io.LimitReader(resp.Body, mainlinePrefetchMaxB+1))
		if err != nil || len(audio) == 0 || len(audio) > mainlinePrefetchMaxB {
			return
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.programs[program.RoomID] != program || !program.Running || program.CurrentTaskID != sourceTaskID || program.PlannedNextTrack != trackIndex {
			return
		}
		program.PrefetchedTrack = trackIndex
		program.PrefetchedAudio = audio
	}()
}
