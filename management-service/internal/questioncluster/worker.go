package questioncluster

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

const (
	defaultInterval        = 5 * time.Minute
	recentWindow           = 5 * time.Minute
	maxSmallBucketCount    = 3
	minEligibleBucketCount = 2
	minMergeConfidence     = 0.78
)

type sessionStore interface {
	ListRunningLiveRuntimeSessions(context.Context) ([]model.LiveRuntimeSession, error)
}

type completer interface {
	Complete(context.Context, agentgateway.Request) (agentgateway.Response, error)
}

type leader interface {
	IsLeader() bool
}

type Worker struct {
	store    sessionStore
	core     *coreclient.Client
	agent    completer
	leader   leader
	interval time.Duration
	now      func() time.Time

	mu              sync.Mutex
	inFlight        map[int64]bool
	lastProcessedID map[int64]int64
}

type brainView struct {
	Intelligence struct {
		TopTopics []topicBucket
	}
}

type topicBucket struct {
	Topic           string
	Count           int
	SampleQuestions []string
	Questions       []topicQuestion
}

type topicQuestion struct {
	EventID    int64
	Content    string
	OccurredAt time.Time
}

type modelGroup struct {
	RepresentativeTopic string   `json:"representative_topic"`
	SourceTopics        []string `json:"source_topics"`
	Confidence          float64  `json:"confidence"`
}

type modelOutput struct {
	Groups []modelGroup `json:"groups"`
}

type mergeGroup struct {
	RepresentativeTopic string
	SourceTopics        []string
}

func New(
	store sessionStore,
	core *coreclient.Client,
	agent completer,
	leaders ...leader,
) *Worker {
	w := &Worker{
		store:           store,
		core:            core,
		agent:           agent,
		interval:        defaultInterval,
		now:             func() time.Time { return time.Now().UTC() },
		inFlight:        map[int64]bool{},
		lastProcessedID: map[int64]int64{},
	}
	if len(leaders) > 0 {
		w.leader = leaders[0]
	}
	return w
}

func (w *Worker) Run(ctx context.Context) {
	if w == nil || w.store == nil || w.core == nil || w.agent == nil {
		return
	}
	w.runCycle(ctx)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runCycle(ctx)
		}
	}
}

// RunOnce executes one semantic clustering cycle immediately. It is used by
// operations/tests and follows the same running-agent and room-level guards as
// the scheduled five-minute loop.
func (w *Worker) RunOnce(ctx context.Context) {
	if w == nil || w.store == nil || w.core == nil || w.agent == nil {
		return
	}
	w.runCycle(ctx)
}

// RunRoomOnce forces one room through the same semantic merge pipeline. When
// includeAllSmall is true it is intended for manual inspection and considers
// all current-session Q: buckets with count <= 3 instead of only the latest
// five-minute increment. Scheduled production runs always pass false.
func (w *Worker) RunRoomOnce(ctx context.Context, tenantID, roomID int64, includeAllSmall bool) error {
	if w == nil || w.core == nil || w.agent == nil {
		return nil
	}
	return w.refineRoom(ctx, model.LiveRuntimeSession{
		TenantID: tenantID,
		RoomID:   roomID,
		Status:   "running",
	}, includeAllSmall)
}

func (w *Worker) runCycle(ctx context.Context) {
	if w.leader != nil && !w.leader.IsLeader() {
		return
	}
	sessions, err := w.store.ListRunningLiveRuntimeSessions(ctx)
	if err != nil {
		log.Printf("question cluster semantic list sessions: %v", err)
		return
	}

	const workers = 4
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, session := range sessions {
		if !strings.EqualFold(strings.TrimSpace(session.Status), "running") {
			continue
		}
		session := session
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := w.refineRoom(ctx, session, false); err != nil {
				log.Printf(
					"question cluster semantic refine tenant=%d room=%d: %v",
					session.TenantID,
					session.RoomID,
					err,
				)
			}
		}()
	}
	wg.Wait()
}

func (w *Worker) refineRoom(ctx context.Context, session model.LiveRuntimeSession, includeAllSmall bool) error {
	if session.RoomID <= 0 || session.TenantID <= 0 || session.Status != "running" {
		return nil
	}
	if !w.beginRoom(session.RoomID) {
		return nil
	}
	defer w.endRoom(session.RoomID)

	brain, err := w.fetchBrain(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return err
	}
	eligible, maxEventID := selectEligibleTopics(brain.Intelligence.TopTopics, w.now())
	scopeText := "最近 5 分钟产生的单条/小问题桶"
	if includeAllSmall {
		eligible, maxEventID = selectAllSmallTopics(brain.Intelligence.TopTopics)
		scopeText = "当前直播会话里的全部单条/小问题桶"
	}
	if len(eligible) < minEligibleBucketCount || maxEventID <= 0 {
		return nil
	}
	if w.alreadyProcessed(session.RoomID, maxEventID) {
		return nil
	}

	inputJSON, err := buildModelInput(brain.Intelligence.TopTopics, eligible, w.now())
	if err != nil {
		return err
	}
	response, err := w.agent.Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{
				Role:    "system",
				Content: "你是直播间问题语义聚类器，只负责把现有问题桶整理成适合主播统一回答的类别。不要回答问题，不要改写观众原话，不要创造新问题桶。聚类标准不是逐字同义：只要这些问题在直播中可以由主播用同一段回答一起覆盖，就应当合并，例如同属原料/榨油工艺、价格优惠、品质口感、保存方法、购买方式等。相反，如果需要明显不同的回答逻辑，就必须分开，例如发货时效不能和运费价格混为一类，售后破损不能和正常物流进度混为一类。目标是明显减少孤立的 +1 小桶，但不要为了减少数量而硬合并。输入中的 topic 是现有桶 ID；只有 eligible_for_merge=true 的桶可以放入 source_topics，representative_topic 必须从输入已有 topic 中选择。优先把多个小桶并入一个最能代表该回答类别的现有桶；已有 FAMILY: 桶如果语义匹配可以优先作为 representative_topic。不确定就不要合并。严格只输出 JSON：{\"groups\":[{\"representative_topic\":\"现有topic\",\"source_topics\":[\"要并入的现有topic\"],\"confidence\":0.0}]}。",
			},
			{
				Role:    "user",
				Content: "请整理" + scopeText + "。以下是当前桶数据：\n" + string(inputJSON),
			},
		},
		MaxTokens:      700,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        20 * time.Second,
	})
	if err != nil {
		return err
	}

	var output modelOutput
	if err := json.Unmarshal([]byte(stripJSONFence(response.Text)), &output); err != nil {
		return fmt.Errorf("decode semantic clustering output: %w", err)
	}
	groups := validateGroups(output.Groups, brain.Intelligence.TopTopics, eligible)
	if len(groups) == 0 {
		w.markProcessed(session.RoomID, maxEventID)
		return nil
	}
	if err := w.applyMerges(ctx, session.TenantID, session.RoomID, groups); err != nil {
		return err
	}
	w.markProcessed(session.RoomID, maxEventID)
	return nil
}

func (w *Worker) fetchBrain(ctx context.Context, tenantID, roomID int64) (brainView, error) {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := w.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/brain", roomID),
		query,
		nil,
	)
	if err != nil {
		return brainView{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return brainView{}, fmt.Errorf("core brain status %d", resp.StatusCode)
	}
	var result brainView
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return brainView{}, err
	}
	return result, nil
}

func (w *Worker) applyMerges(
	ctx context.Context,
	tenantID, roomID int64,
	groups []mergeGroup,
) error {
	payloadGroups := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		payloadGroups = append(payloadGroups, map[string]any{
			"representative_topic": group.RepresentativeTopic,
			"source_topics":        group.SourceTopics,
		})
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := w.core.DoRoom(
		ctx,
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/brain/topic-merges", roomID),
		query,
		map[string]any{"groups": payloadGroups},
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("core topic merge status %d", resp.StatusCode)
	}
	return nil
}

func selectEligibleTopics(topics []topicBucket, now time.Time) (map[string]struct{}, int64) {
	cutoff := now.Add(-recentWindow)
	eligible := make(map[string]struct{})
	var maxEventID int64
	for _, topic := range topics {
		name := strings.TrimSpace(topic.Topic)
		if !strings.HasPrefix(name, "Q:") || topic.Count <= 0 || topic.Count > maxSmallBucketCount {
			continue
		}
		recent := false
		for _, question := range topic.Questions {
			if question.OccurredAt.IsZero() || question.OccurredAt.Before(cutoff) {
				continue
			}
			recent = true
			if question.EventID > maxEventID {
				maxEventID = question.EventID
			}
		}
		if recent {
			eligible[name] = struct{}{}
		}
	}
	return eligible, maxEventID
}

func selectAllSmallTopics(topics []topicBucket) (map[string]struct{}, int64) {
	eligible := make(map[string]struct{})
	var maxEventID int64
	for _, topic := range topics {
		name := strings.TrimSpace(topic.Topic)
		if !strings.HasPrefix(name, "Q:") || topic.Count <= 0 || topic.Count > maxSmallBucketCount {
			continue
		}
		eligible[name] = struct{}{}
		for _, question := range topic.Questions {
			if question.EventID > maxEventID {
				maxEventID = question.EventID
			}
		}
	}
	return eligible, maxEventID
}

func buildModelInput(
	topics []topicBucket,
	eligible map[string]struct{},
	now time.Time,
) ([]byte, error) {
	type item struct {
		Topic            string   `json:"topic"`
		Count            int      `json:"count"`
		EligibleForMerge bool     `json:"eligible_for_merge"`
		Samples          []string `json:"samples,omitempty"`
		RecentQuestions  []string `json:"recent_questions,omitempty"`
	}
	cutoff := now.Add(-recentWindow)
	items := make([]item, 0, len(topics))
	for _, topic := range topics {
		entry := item{
			Topic:   topic.Topic,
			Count:   topic.Count,
			Samples: append([]string(nil), topic.SampleQuestions...),
		}
		_, entry.EligibleForMerge = eligible[topic.Topic]
		for _, question := range topic.Questions {
			if question.OccurredAt.Before(cutoff) {
				continue
			}
			content := strings.TrimSpace(question.Content)
			if content == "" {
				continue
			}
			entry.RecentQuestions = append(entry.RecentQuestions, content)
			if len(entry.RecentQuestions) >= 5 {
				break
			}
		}
		items = append(items, entry)
	}
	return json.Marshal(map[string]any{
		"window_minutes": 5,
		"topics":         items,
	})
}

func validateGroups(
	groups []modelGroup,
	topics []topicBucket,
	eligible map[string]struct{},
) []mergeGroup {
	known := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		known[strings.TrimSpace(topic.Topic)] = struct{}{}
	}
	occupied := map[string]struct{}{}
	out := make([]mergeGroup, 0, len(groups))
	for _, group := range groups {
		if group.Confidence < minMergeConfidence {
			continue
		}
		representative := strings.TrimSpace(group.RepresentativeTopic)
		if _, ok := known[representative]; !ok {
			continue
		}
		if _, used := occupied[representative]; used {
			continue
		}
		sources := make([]string, 0, len(group.SourceTopics))
		local := map[string]struct{}{}
		valid := true
		for _, source := range group.SourceTopics {
			source = strings.TrimSpace(source)
			if source == "" || source == representative {
				continue
			}
			if _, ok := known[source]; !ok {
				valid = false
				break
			}
			if _, ok := eligible[source]; !ok {
				valid = false
				break
			}
			if _, used := occupied[source]; used {
				valid = false
				break
			}
			if _, seen := local[source]; seen {
				continue
			}
			local[source] = struct{}{}
			sources = append(sources, source)
		}
		if !valid || len(sources) == 0 {
			continue
		}
		occupied[representative] = struct{}{}
		for _, source := range sources {
			occupied[source] = struct{}{}
		}
		out = append(out, mergeGroup{
			RepresentativeTopic: representative,
			SourceTopics:        sources,
		})
	}
	return out
}

func stripJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		value = strings.TrimPrefix(value, "```json")
		value = strings.TrimPrefix(value, "```JSON")
		value = strings.TrimPrefix(value, "```")
		value = strings.TrimSuffix(value, "```")
	}
	return strings.TrimSpace(value)
}

func (w *Worker) beginRoom(roomID int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inFlight[roomID] {
		return false
	}
	w.inFlight[roomID] = true
	return true
}

func (w *Worker) endRoom(roomID int64) {
	w.mu.Lock()
	delete(w.inFlight, roomID)
	w.mu.Unlock()
}

func (w *Worker) alreadyProcessed(roomID, maxEventID int64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastProcessedID[roomID] >= maxEventID
}

func (w *Worker) markProcessed(roomID, maxEventID int64) {
	w.mu.Lock()
	if maxEventID > w.lastProcessedID[roomID] {
		w.lastProcessedID[roomID] = maxEventID
	}
	w.mu.Unlock()
}
