package paidpipeline

import (
	"context"
	"fmt"
	"strings"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/roomintel"
	"livecompanion/core/internal/timeline"
)

const (
	debtBacklogThreshold      = 0.75
	debtBacklogMinGap         = 2 * time.Minute
	debtQuestionMaxAge        = 10 * time.Minute
	debtInteractionMaxAge     = 5 * time.Minute
	debtBudgetRetryDelay      = 8 * time.Second
	debtDefaultRetryDelay     = 15 * time.Second
	debtSemanticResolveBudget = 120 * time.Millisecond
)

type debtQuestionBacklog struct {
	Topic       string
	EventID     int64
	UserID      string
	Nickname    string
	Content     string
	OccurredAt  time.Time
	RepeatCount int
	UniqueUsers int
}

func debtBacklogKey(roomID int64, kind string) string {
	return fmt.Sprintf("%d:%s", roomID, strings.ToUpper(strings.TrimSpace(kind)))
}

func lastCompletedInteractionAt(view roombrain.View) time.Time {
	last := view.Timeline.StartedAt
	if raw := view.Timeline.Summary["last_by_kind"]; raw != nil {
		switch values := raw.(type) {
		case map[timeline.PinKind]time.Time:
			if at := values[timeline.PinAnswer]; at.After(last) {
				last = at
			}
		case map[string]time.Time:
			if at := values[string(timeline.PinAnswer)]; at.After(last) {
				last = at
			}
		}
	}
	for _, debt := range view.Timeline.Debts {
		if debt.LastSpent.After(last) {
			last = debt.LastSpent
		}
	}
	return last
}

func debtStateReady(view roombrain.View, kind timeline.DebtKind, now time.Time) (roombrain.DebtView, bool) {
	debt, ok := view.Timeline.Debts[string(kind)]
	if !ok || debt.Value < debtBacklogThreshold {
		return roombrain.DebtView{}, false
	}
	if !debt.CooldownUntil.IsZero() && now.Before(debt.CooldownUntil) {
		return debt, false
	}
	return debt, true
}

func pickDebtQuestion(view roombrain.View, now time.Time, skipEventID int64) (debtQuestionBacklog, bool) {
	best := debtQuestionBacklog{}
	for _, bucket := range view.Intelligence.TopTopics {
		questions := bucket.TTSQuestions
		if len(questions) == 0 {
			questions = bucket.Questions
		}
		for _, question := range questions {
			content := strings.TrimSpace(question.Content)
			if content == "" || (skipEventID > 0 && question.EventID == skipEventID) {
				continue
			}
			at := question.OccurredAt.UTC()
			if at.IsZero() {
				at = bucket.LastSeenAt.UTC()
			}
			if !bucket.LastAnsweredAt.IsZero() && !at.After(bucket.LastAnsweredAt) {
				continue
			}
			if !at.IsZero() && now.Sub(at) > debtQuestionMaxAge {
				continue
			}
			if best.Content != "" && !at.After(best.OccurredAt) {
				continue
			}
			best = debtQuestionBacklog{
				Topic:       strings.TrimSpace(bucket.Topic),
				EventID:     question.EventID,
				UserID:      strings.TrimSpace(question.UserID),
				Nickname:    strings.TrimSpace(question.Nickname),
				Content:     content,
				OccurredAt:  at,
				RepeatCount: bucket.Count,
				UniqueUsers: bucket.UniqueUsers,
			}
		}
	}
	return best, best.Content != ""
}

func debtInteractionMissionKey(event model.RoomEvent) string {
	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "chat", "comment":
		if strings.TrimSpace(event.Content) != "" {
			return "reply_chat"
		}
	case "member":
		if strings.TrimSpace(event.Nickname) != "" {
			return "welcome_named"
		}
		return "welcome_batch"
	case "follow":
		return "reply_follow"
	case "like":
		return "reply_like"
	}
	return ""
}

func pickDebtInteractionEvent(items []model.RoomEvent, now, after time.Time, skipEventID int64) (model.RoomEvent, string, bool) {
	var best model.RoomEvent
	bestKey := ""
	for _, event := range items {
		key := debtInteractionMissionKey(event)
		if key == "" || (skipEventID > 0 && event.ID == skipEventID) {
			continue
		}
		at := event.OccurredAt.UTC()
		if at.IsZero() {
			continue
		}
		if !after.IsZero() && !at.After(after) {
			continue
		}
		if now.Sub(at) > debtInteractionMaxAge {
			continue
		}
		if bestKey == "" || at.After(best.OccurredAt) {
			best = event
			bestKey = key
		}
	}
	return best, bestKey, bestKey != ""
}

func (p *Processor) debtRetryAllowed(roomID int64, kind string, now time.Time) bool {
	if p == nil {
		return false
	}
	key := debtBacklogKey(roomID, kind)
	p.debtMu.Lock()
	retryAt := p.debtRetryAfter[key]
	p.debtMu.Unlock()
	return retryAt.IsZero() || !now.Before(retryAt)
}

func (p *Processor) debtLastEvent(roomID int64, kind string) int64 {
	if p == nil {
		return 0
	}
	key := debtBacklogKey(roomID, kind)
	p.debtMu.Lock()
	eventID := p.debtLastEventID[key]
	p.debtMu.Unlock()
	return eventID
}

func (p *Processor) setDebtRetry(roomID int64, kind string, retryAt time.Time) {
	if p == nil {
		return
	}
	key := debtBacklogKey(roomID, kind)
	p.debtMu.Lock()
	if retryAt.IsZero() {
		delete(p.debtRetryAfter, key)
	} else {
		p.debtRetryAfter[key] = retryAt.UTC()
	}
	p.debtMu.Unlock()
}

func (p *Processor) markDebtEvent(roomID int64, kind string, eventID int64) {
	if p == nil || eventID <= 0 {
		return
	}
	key := debtBacklogKey(roomID, kind)
	p.debtMu.Lock()
	p.debtLastEventID[key] = eventID
	delete(p.debtRetryAfter, key)
	p.debtMu.Unlock()
}

func (p *Processor) roomHasPendingInteractionWindow(roomID int64) bool {
	if p == nil {
		return false
	}
	p.interactionMu.Lock()
	defer p.interactionMu.Unlock()
	for _, window := range p.interactionWindows[roomID] {
		if window != nil && window.PendingCount > 0 {
			return true
		}
	}
	return false
}

func (p *Processor) enqueueQuestionDebtBacklog(roomID int64, view roombrain.View, debt roombrain.DebtView, now time.Time) bool {
	preference := 1.0
	if p.policies != nil {
		preference = p.policies.InteractionPreferenceFactor(roomID, "question")
	}
	if preference <= 0 {
		return false
	}
	question, ok := pickDebtQuestion(view, now, p.debtLastEvent(roomID, string(timeline.DebtQuestion)))
	if !ok {
		return false
	}
	topic := p.resolveSemanticTopic(roomID, question.Content, question.Topic, debtSemanticResolveBudget)
	if strings.TrimSpace(topic) == "" {
		topic = strings.TrimSpace(question.Topic)
	}
	signal := basepipeline.Signal{
		RoomID:  roomID,
		EventID: question.EventID,
		UserID:  question.UserID,
		Content: question.Content,
		Topic:   question.Topic,
	}
	businessValue := questionBusinessValue(signal)
	waitingSeconds := 0
	if !question.OccurredAt.IsZero() && now.After(question.OccurredAt) {
		waitingSeconds = int(now.Sub(question.OccurredAt).Seconds())
	}
	priority := 55 + int(debt.Value*30) + int(businessValue*10)
	if priority > 100 {
		priority = 100
	}
	heat := normalizedInteractionHeat(view.Intelligence.Heat)
	eventValue := 72*preference + debt.Value*42 + businessValue*20
	if eventValue > 180 {
		eventValue = 180
	}
	valueLevel := interactionValueLevel(eventValue)
	highValue := strings.EqualFold(valueLevel, "HIGH")
	budget, allowed, budgetReason := p.checkInteractionBudget(roomID, heat, highValue, now)
	if !allowed {
		p.setDebtRetry(roomID, string(timeline.DebtQuestion), now.Add(debtBudgetRetryDelay))
		return false
	}

	mergedIDs := []int64{}
	if question.EventID > 0 {
		mergedIDs = append(mergedIDs, question.EventID)
	}
	candidate := agentdecision.Candidate{
		Source:            agentdecision.SourceAgent,
		Topic:             topic,
		Title:             "补答观众问题",
		Question:          question.Content,
		Summary:           "问题债务达到阈值且当前互动队列为空，补入真实未回答问题",
		ReplyHint:         "优先回答这条真实未回答问题；结合当前主线和事实依据生成一段完整、自然、可直接播出的回答。",
		MissionKind:       "reply_question",
		MissionEventCount: 1,
		Priority:          priority,
		EventID:           question.EventID,
		UserID:            question.UserID,
		Nicknames:         []string{question.Nickname},
		InteractionDecision: agentdecision.InteractionDecision{
			Handle:           true,
			PrimaryEvent:     "question_debt_backlog",
			MergedEventIDs:   mergedIDs,
			EventValue:       eventValue,
			ValueLevel:       valueLevel,
			Reason:           fmt.Sprintf("问题债务=%.2f，队列为空且超过2分钟未完成互动；%s", debt.Value, budgetReason),
			DeadlineAt:       now.Add(10 * time.Second),
			BudgetLevel:      budget.Level,
			BudgetAllowed:    true,
			Heat:             heat,
			PreferenceFactor: preference,
			QuestionDebt: &agentdecision.QuestionDebt{
				Topic:           topic,
				FirstSeenAt:     question.OccurredAt,
				RepeatCount:     maxInteractionInt(1, question.RepeatCount),
				UniqueUsers:     maxInteractionInt(1, question.UniqueUsers),
				WaitingSeconds:  waitingSeconds,
				BusinessValue:   businessValue,
				CurrentPriority: float64(priority),
			},
		},
	}
	result := p.decisions.Enqueue(roomID, candidate)
	if result.Suppressed {
		retryAt := now.Add(debtDefaultRetryDelay)
		if result.RecentlyAnswered != nil && result.RecentlyAnswered.CooldownUntil.After(retryAt) {
			retryAt = result.RecentlyAnswered.CooldownUntil
		}
		p.setDebtRetry(roomID, string(timeline.DebtQuestion), retryAt)
		return false
	}
	if result.Item == nil {
		p.setDebtRetry(roomID, string(timeline.DebtQuestion), now.Add(debtDefaultRetryDelay))
		return false
	}
	if !result.Merged {
		p.recordInteractionBudgetUse(roomID, heat, highValue, now)
	}
	p.markDebtEvent(roomID, string(timeline.DebtQuestion), question.EventID)
	p.logInteractionMission(roomID, candidate)
	return true
}

func (p *Processor) enqueueInteractionDebtBacklog(roomID int64, view roombrain.View, debt roombrain.DebtView, lastCompletedAt, now time.Time) bool {
	if p.sessions == nil || p.roomHasPendingInteractionWindow(roomID) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	items, err := p.sessions.ListRecent(ctx, nil, roomID, 200)
	cancel()
	if err != nil {
		p.setDebtRetry(roomID, string(timeline.DebtInteraction), now.Add(debtDefaultRetryDelay))
		return false
	}
	event, key, ok := pickDebtInteractionEvent(items, now, lastCompletedAt, p.debtLastEvent(roomID, string(timeline.DebtInteraction)))
	if !ok {
		return false
	}
	rule, ok := defaultInteractionRules[key]
	if !ok {
		return false
	}
	_, effectiveWeight, enabled := p.interactionRuleWeight(roomID, event.TenantID, key)
	if !enabled {
		return false
	}
	at := event.OccurredAt.UTC()
	window := interactionWindow{
		TenantID:      event.TenantID,
		RoomID:        roomID,
		Key:           key,
		PendingCount:  1,
		TotalEvents:   1,
		FirstPending:  at,
		LastEventAt:   at,
		LatestEventID: event.ID,
		LatestUserID:  strings.TrimSpace(event.UserID),
		LatestName:    strings.TrimSpace(event.Nickname),
		LatestContent: strings.TrimSpace(event.Content),
		EventIDs:      []int64{event.ID},
		UserIDs:       []string{event.UserID},
		Names:         []string{event.Nickname},
	}
	candidate := interactionMissionCandidate(rule, window, now)
	if key == "reply_chat" {
		candidate.Topic = p.resolveSemanticTopic(roomID, candidate.Question, "", debtSemanticResolveBudget)
	}
	decision := p.interactionWindowDecision(rule, window, effectiveWeight, now)
	decision.Reason = fmt.Sprintf("互动债务=%.2f，队列为空且超过2分钟未完成互动；%s", debt.Value, decision.Reason)
	candidate.InteractionDecision = decision
	candidate.Priority = interactionEffectivePriority(rule.Priority, effectiveWeight)
	if candidate.Priority < 36 {
		candidate.Priority = 36
	}
	highValue := strings.EqualFold(decision.ValueLevel, "HIGH")
	budget, allowed, budgetReason := p.checkInteractionBudget(roomID, decision.Heat, highValue, now)
	if !allowed {
		p.setDebtRetry(roomID, string(timeline.DebtInteraction), now.Add(debtBudgetRetryDelay))
		return false
	}
	candidate.InteractionDecision.BudgetLevel = budget.Level
	candidate.InteractionDecision.BudgetAllowed = true
	candidate.InteractionDecision.Reason = strings.TrimSpace(candidate.InteractionDecision.Reason + "；" + budgetReason)
	result := p.decisions.Enqueue(roomID, candidate)
	if result.Suppressed {
		retryAt := now.Add(debtDefaultRetryDelay)
		if result.RecentlyAnswered != nil && result.RecentlyAnswered.CooldownUntil.After(retryAt) {
			retryAt = result.RecentlyAnswered.CooldownUntil
		}
		p.setDebtRetry(roomID, string(timeline.DebtInteraction), retryAt)
		return false
	}
	if result.Item == nil {
		p.setDebtRetry(roomID, string(timeline.DebtInteraction), now.Add(debtDefaultRetryDelay))
		return false
	}
	if !result.Merged {
		p.recordInteractionBudgetUse(roomID, decision.Heat, highValue, now)
	}
	p.markDebtEvent(roomID, string(timeline.DebtInteraction), event.ID)
	p.logInteractionMission(roomID, candidate)
	return true
}

func (p *Processor) FlushDebtBacklog(now time.Time) {
	if p == nil || p.runtime == nil || p.decisions == nil || p.brain == nil {
		return
	}
	if now.IsZero() {
		now = p.clock()
	} else {
		now = now.UTC()
	}
	for _, runtime := range p.runtime.Snapshots() {
		if runtime.RoomID <= 0 || runtime.State != agentwork.StateWorking {
			continue
		}
		queue := p.decisions.Snapshot(runtime.RoomID)
		if len(queue.Queue) > 0 {
			continue
		}
		view, err := p.brain.Snapshot(runtime.RoomID)
		if err != nil {
			continue
		}
		lastCompleted := lastCompletedInteractionAt(view)
		if runtime.WorkingSince != nil && runtime.WorkingSince.After(lastCompleted) {
			lastCompleted = runtime.WorkingSince.UTC()
		}
		if lastCompleted.IsZero() {
			lastCompleted = now
		}
		if now.Sub(lastCompleted) < debtBacklogMinGap {
			continue
		}

		if debt, ready := debtStateReady(view, timeline.DebtQuestion, now); ready &&
			p.debtRetryAllowed(runtime.RoomID, string(timeline.DebtQuestion), now) {
			if p.enqueueQuestionDebtBacklog(runtime.RoomID, view, debt, now) {
				continue
			}
		}
		if debt, ready := debtStateReady(view, timeline.DebtInteraction, now); ready &&
			p.debtRetryAllowed(runtime.RoomID, string(timeline.DebtInteraction), now) {
			_ = p.enqueueInteractionDebtBacklog(runtime.RoomID, view, debt, lastCompleted, now)
		}
	}
}

// Compile-time guard that the question backlog continues to use the canonical
// Room Brain question type rather than inventing a second storage model.
var _ roomintel.TopicQuestion
