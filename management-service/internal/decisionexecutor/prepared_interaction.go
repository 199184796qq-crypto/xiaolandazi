package decisionexecutor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

const preparedInteractionTTL = 2 * time.Minute
const maxPreparedInteractions = 128

var errPreparedInteractionWaiting = errors.New("prepared interaction waiting for a safe cut")

type preparedInteractionKey struct {
	Tenant, Room, Session int64
	Decision              string
}

// Disposable execution state, not an answer cache: only the same unplayed
// decision in the same session can reuse this audio after an explicit rejection.
type preparedInteraction struct {
	Item           decisionItem
	Text, AudioURL string
	Scope, Memory  string
	ExpiresAt      time.Time
}

func preparationKey(session model.LiveRuntimeSession, item decisionItem) preparedInteractionKey {
	return preparedInteractionKey{session.TenantID, session.RoomID, session.ID, item.ID}
}

// Exclude playback cursors and heat, which move while TTS is being prepared.
// Include the event, selected plan, published facts/voice and live corrections.
func (w *Worker) interactionPreparationScope(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return "", err
	}
	facts, err := w.store.ListLiveAgentPlanFacts(ctx, session.TenantID, plan.ID)
	if err != nil {
		return "", err
	}
	published, err := w.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil && !errors.Is(err, appdb.ErrLiveAgentPlanVersionNotFound) {
		return "", err
	}
	versions, err := w.store.ListLiveAgentConfigVersions(ctx, session.TenantID)
	if err != nil {
		return "", err
	}
	voice, ok, err := w.readyVoice(ctx, session.TenantID, session.RoomID)
	if err != nil || !ok {
		return "", fmt.Errorf("读取待播声音身份失败: %v", err)
	}
	effective, err := policy.LoadGeneration()
	if err != nil {
		return "", err
	}
	input := []any{session.ExternalID, session.ExecutionRealm, plan, facts, published, versions, voice, effective,
		item.PurposeID, item.PurposeKnowledge, item.Topic, item.SampleQuestions, item.Nicknames, item.ReplyHint, item.ManualAction, item.ManualOrigin, item.ExecutionMode, item.FixedText, item.MissionKind, item.Schedule,
		w.store.AgentPromptValue(ctx, "policy.runtime.execution", ""), w.store.AgentPromptValue(ctx, "live.final_review.system", "")}
	if source, ok := w.store.(expressionConstraintStore); ok {
		constraints, e := source.CurrentSpeechExpressionConstraints(ctx, session.TenantID, plan.ID)
		if e != nil {
			return "", e
		}
		input = append(input, constraints)
	}
	if source, ok := w.store.(interface {
		ListLiveAgentPlanBenefits(context.Context, int64, int64) ([]model.LiveAgentPlanBenefit, error)
	}); ok {
		benefits, err := source.ListLiveAgentPlanBenefits(ctx, session.TenantID, plan.ID)
		if err != nil {
			return "", err
		}
		active := make([]bool, len(benefits))
		for i, benefit := range benefits {
			active[i] = (benefit.StartsAt == nil || !w.now().Before(*benefit.StartsAt)) && (benefit.EndsAt == nil || w.now().Before(*benefit.EndsAt))
		}
		input = append(input, benefits, active)
	}
	if source, ok := w.store.(interface {
		ListLiveAgentPlanProductLinks(context.Context, int64, int64) ([]model.LiveAgentPlanProductLink, error)
	}); ok {
		links, err := source.ListLiveAgentPlanProductLinks(ctx, session.TenantID, plan.ID)
		if err != nil {
			return "", err
		}
		input = append(input, links)
	}
	return faqDigest(input), nil
}

func (w *Worker) takePreparedInteraction(session model.LiveRuntimeSession, item decisionItem, scope, memory string) (preparedInteraction, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := preparationKey(session, item)
	prepared, ok := w.prepared[key]
	delete(w.prepared, key) // uncertain submission errors cannot leave reusable audio
	if !ok || !w.now().Before(prepared.ExpiresAt) || prepared.Scope != scope || prepared.Memory != memory {
		return preparedInteraction{}, false
	}
	return prepared, true
}

func (w *Worker) savePreparedInteraction(session model.LiveRuntimeSession, item decisionItem, text, audioURL, scope, memory string, expiresAt time.Time) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(audioURL) == "" || !w.now().Before(expiresAt) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.prepared == nil {
		w.prepared = make(map[preparedInteractionKey]preparedInteraction)
	}
	for key, value := range w.prepared {
		if !w.now().Before(value.ExpiresAt) {
			delete(w.prepared, key)
		}
	}
	if len(w.prepared) >= maxPreparedInteractions {
		var oldest preparedInteractionKey
		var expiry time.Time
		for key, value := range w.prepared {
			if expiry.IsZero() || value.ExpiresAt.Before(expiry) {
				oldest, expiry = key, value.ExpiresAt
			}
		}
		delete(w.prepared, oldest)
	}
	w.prepared[preparationKey(session, item)] = preparedInteraction{Item: item, Text: text, AudioURL: audioURL, Scope: scope, Memory: memory, ExpiresAt: expiresAt}
}

func (w *Worker) prunePreparedInteractions(sessions []model.LiveRuntimeSession) {
	active := make(map[[3]int64]bool)
	for _, session := range sessions {
		if session.Status == "running" && session.BelongsToExecutionRealm(w.executionRealm) {
			active[[3]int64{session.TenantID, session.RoomID, session.ID}] = true
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, value := range w.prepared {
		if !active[[3]int64{key.Tenant, key.Room, key.Session}] || !w.now().Before(value.ExpiresAt) {
			delete(w.prepared, key)
		}
	}
}
