package decisionexecutor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionplan"
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

// Disposable execution state, not a cross-session answer cache. Normal replies
// require the same decision; mandatory reports follow the same outstanding quota.
type preparedInteraction struct {
	Item           decisionItem
	Text, AudioURL string
	Scope, Memory  string
	ExpiresAt      time.Time
}

func preparationKey(session model.LiveRuntimeSession, item decisionItem) preparedInteractionKey {
	decision := item.ID
	if retryableTimeReport(item) {
		decision = "time-report:" + strings.ToUpper(item.Topic)
	}
	return preparedInteractionKey{session.TenantID, session.RoomID, session.ID, decision}
}

// Exclude playback cursors and heat, which move while TTS is being prepared.
// Include the event, selected plan, published facts/voice and live corrections.
func (w *Worker) interactionContentScope(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	if e, ok := interactionplan.Decode(item.PurposeKnowledge); ok {
		if e.Session != session.ID || e.Tenant != session.TenantID || e.Room != session.RoomID {
			return "", fmt.Errorf("互动缓存场次不匹配")
		}
		if welcome, _ := welcomeV2(item); welcome {
			// Admission changes only this execution deadline, never the content
			// identity. Preserve already synthesized audio during cut retries.
			e.ExpiresAt = time.Time{}
		}
		return interactionDigest([]any{e, item.Nicknames, w.readingRulesFor(ctx, session)}), nil
	}
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
	return interactionDigest(input), nil
}

func (w *Worker) takePreparedInteraction(session model.LiveRuntimeSession, item decisionItem, scope, memory string) (preparedInteraction, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	key := preparationKey(session, item)
	prepared, ok := w.prepared[key]
	if !retryableTimeReport(item) {
		delete(w.prepared, key)
	} // reports remain owned by Core until actual completion
	if !ok || !w.now().Before(prepared.ExpiresAt) || prepared.Scope != scope || prepared.Memory != memory {
		delete(w.prepared, key)
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
	controls := map[[3]int64]model.RoomInteractionControls{}
	// Only disposable cached work needs a database check. Bound the entire cleanup
	// pass so a database outage does not block scheduling all other live rooms.
	cached := map[[3]int64]bool{}
	w.mu.Lock()
	for key := range w.prepared {
		cached[[3]int64{key.Tenant, key.Room, key.Session}] = true
	}
	for key := range w.timeReportDrafts {
		cached[[3]int64{key.Tenant, key.Room, key.Session}] = true
	}
	w.mu.Unlock()
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	for _, session := range sessions {
		key := [3]int64{session.TenantID, session.RoomID, session.ID}
		if !cached[key] {
			continue
		}
		if cleanupCtx.Err() != nil {
			break
		}
		if current, err := w.readInteractionControls(cleanupCtx, session); err == nil {
			controls[key] = current
		}
	}
	for _, session := range sessions {
		if session.Status == "running" && session.BelongsToExecutionRealm(w.executionRealm) {
			active[[3]int64{session.TenantID, session.RoomID, session.ID}] = true
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, value := range w.timeReportDrafts {
		if !active[[3]int64{key.Tenant, key.Room, key.Session}] || !w.now().Before(value.ValidUntil) {
			delete(w.timeReportDrafts, key)
		}
	}
	for key, value := range w.prepared {
		if c, ok := controls[[3]int64{key.Tenant, key.Room, key.Session}]; ok && (!c.Enabled(interactionControlKind(value.Item)) || c.DisabledAt[interactionControlKind(value.Item)] > value.Item.ControlsRevision) {
			delete(w.prepared, key)
			continue
		}
		if !active[[3]int64{key.Tenant, key.Room, key.Session}] || !w.now().Before(value.ExpiresAt) {
			delete(w.prepared, key)
		}
	}
}
