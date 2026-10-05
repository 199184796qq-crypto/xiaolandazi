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
	"livecompanion/management/internal/semantic"
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
	AgentPromptValue(context.Context, string, string) string
}

type completer interface {
	Complete(context.Context, agentgateway.Request) (agentgateway.Response, error)
}

type leader interface {
	IsLeader() bool
}

type Worker struct {
	store            sessionStore
	core             *coreclient.Client
	agent            completer
	leader           leader
	interval         time.Duration
	executionRealm   string
	now              func() time.Time
	semanticEmbedder semantic.Embedder
	semanticService  *semantic.Service

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
		executionRealm:  "prod",
		now:             func() time.Time { return time.Now().UTC() },
		inFlight:        map[int64]bool{},
		lastProcessedID: map[int64]int64{},
	}
	if len(leaders) > 0 {
		w.leader = leaders[0]
	}
	return w
}

func (w *Worker) SetExecutionRealm(realm string) {
	if w == nil {
		return
	}
	w.executionRealm = model.NormalizeExecutionRealm(realm)
}

func (w *Worker) SetSemanticEmbedder(embedder semantic.Embedder) {
	if w == nil {
		return
	}
	w.semanticEmbedder = embedder
}

func (w *Worker) SetSemanticService(service *semantic.Service) {
	if w == nil {
		return
	}
	w.semanticService = service
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
		if !session.BelongsToExecutionRealm(w.executionRealm) {
			continue
		}
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

	var narrowed map[string]struct{}
	var semanticErr error
	if w.semanticService != nil && w.semanticService.Enabled() {
		narrowed, semanticErr = semanticCandidateTopicsWithService(
			ctx, w.semanticService, session.TenantID, session.RoomID, brain.Intelligence.TopTopics, eligible,
		)
	} else {
		narrowed, semanticErr = semanticCandidateTopics(ctx, w.semanticEmbedder, brain.Intelligence.TopTopics, eligible)
	}
	if semanticErr != nil {
		log.Printf("question cluster embedding prefilter degraded tenant=%d room=%d: %v", session.TenantID, session.RoomID, semanticErr)
	} else if len(narrowed) >= minEligibleBucketCount {
		eligible = narrowed
	}

	inputJSON, err := buildModelInput(brain.Intelligence.TopTopics, eligible, w.now())
	if err != nil {
		return err
	}
	systemPrompt := w.store.AgentPromptValue(ctx, "question.cluster.system", "按可用同一段主播回答覆盖的语义整理问题类别；不要回答问题，严格输出 JSON。")
	response, err := w.agent.Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: systemPrompt},
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

// This is a recall-oriented prefilter. The downstream LLM still validates actual merges.
const (
	semanticClusterCandidateThreshold = 0.40
	semanticClusterDocumentTTL        = 24 * time.Hour
)

func topicSemanticText(topic topicBucket) string {
	parts := []string{strings.TrimPrefix(strings.TrimSpace(topic.Topic), "Q:")}
	for _, sample := range topic.SampleQuestions {
		if sample = strings.TrimSpace(sample); sample != "" {
			parts = append(parts, sample)
		}
	}
	for _, question := range topic.Questions {
		if value := strings.TrimSpace(question.Content); value != "" {
			parts = append(parts, value)
		}
		if len(parts) >= 6 {
			break
		}
	}
	return strings.Join(parts, "\n")
}

func semanticCandidateTopicsWithService(
	ctx context.Context,
	service *semantic.Service,
	tenantID, roomID int64,
	topics []topicBucket,
	eligible map[string]struct{},
) (map[string]struct{}, error) {
	if service == nil || !service.Enabled() || tenantID <= 0 || roomID <= 0 || len(eligible) < minEligibleBucketCount {
		return nil, nil
	}
	selectedTopics := make([]topicBucket, 0, len(eligible))
	documents := make([]semantic.Document, 0, len(eligible))
	expiresAt := time.Now().UTC().Add(semanticClusterDocumentTTL)
	for _, topic := range topics {
		name := strings.TrimSpace(topic.Topic)
		if _, ok := eligible[name]; !ok {
			continue
		}
		text := topicSemanticText(topic)
		hash := semantic.HashText(name)
		if len(hash) > 16 {
			hash = hash[:16]
		}
		selectedTopics = append(selectedTopics, topic)
		documents = append(documents, semantic.Document{
			TenantID:      tenantID,
			RoomID:        roomID,
			ContentType:   semantic.ContentTypeQuestionCluster,
			SourceID:      "topic:" + hash,
			SourceVersion: 0,
			Status:        "active",
			Text:          text,
			ExpiresAt:     &expiresAt,
		})
	}
	if len(documents) < minEligibleBucketCount {
		return nil, nil
	}
	vectors, err := service.Resolve(ctx, documents)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(documents) {
		return nil, fmt.Errorf("question cluster semantic vectors=%d want=%d", len(vectors), len(documents))
	}
	candidates := make(map[string]struct{})
	for left := 0; left < len(vectors); left++ {
		for right := left + 1; right < len(vectors); right++ {
			if semantic.CosineSimilarity(vectors[left], vectors[right]) < semanticClusterCandidateThreshold {
				continue
			}
			candidates[strings.TrimSpace(selectedTopics[left].Topic)] = struct{}{}
			candidates[strings.TrimSpace(selectedTopics[right].Topic)] = struct{}{}
		}
	}
	service.RecordRetrieval(semantic.ContentTypeQuestionCluster, len(candidates) >= minEligibleBucketCount)
	return candidates, nil
}

func semanticCandidateTopics(ctx context.Context, embedder semantic.Embedder, topics []topicBucket, eligible map[string]struct{}) (map[string]struct{}, error) {
	if embedder == nil || !embedder.Enabled() || len(eligible) < minEligibleBucketCount {
		return nil, nil
	}
	selectedTopics := make([]topicBucket, 0, len(eligible))
	texts := make([]string, 0, len(eligible))
	for _, topic := range topics {
		name := strings.TrimSpace(topic.Topic)
		if _, ok := eligible[name]; !ok {
			continue
		}
		selectedTopics = append(selectedTopics, topic)
		texts = append(texts, topicSemanticText(topic))
	}
	if len(texts) < minEligibleBucketCount {
		return nil, nil
	}
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	if len(vectors) != len(texts) {
		return nil, fmt.Errorf("question cluster semantic vectors=%d want=%d", len(vectors), len(texts))
	}
	candidates := make(map[string]struct{})
	for left := 0; left < len(vectors); left++ {
		for right := left + 1; right < len(vectors); right++ {
			if semantic.CosineSimilarity(vectors[left], vectors[right]) < semanticClusterCandidateThreshold {
				continue
			}
			candidates[strings.TrimSpace(selectedTopics[left].Topic)] = struct{}{}
			candidates[strings.TrimSpace(selectedTopics[right].Topic)] = struct{}{}
		}
	}
	return candidates, nil
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
