package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/purposelib"
	"net/http"
	"strings"
	"sync"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/domaincard"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/interactionpurpose"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
)

// Resolve the runtime only when a model is needed. Cache-only resolution stays
// independent of runtime/accounting availability. Judge and resolution share
// one immutable identity, even if the room starts another session meanwhile.
func interactionPurposeCostBinder(lookup func(context.Context, int64, int64) (model.LiveRuntimeSession, error), tenant, room, plan int64) func(context.Context) (context.Context, error) {
	var once sync.Once
	var session model.LiveRuntimeSession
	var loadErr error
	return func(ctx context.Context) (context.Context, error) {
		once.Do(func() {
			session, loadErr = lookup(ctx, tenant, room)
			if loadErr == nil && (session.ID <= 0 || session.TenantID != tenant || session.RoomID != room ||
				session.EndedAt != nil || (session.Status != "running" && session.Status != "paused")) {
				loadErr = fmt.Errorf("active purpose runtime unavailable")
			}
		})
		if loadErr != nil {
			return nil, fmt.Errorf("purpose billing scope unavailable: %w", loadErr)
		}
		ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenant, RoomID: room, PlanID: plan, RunID: session.ExternalID})
		return withLiveCostScope(ctx, liveCostCallScope{TenantID: tenant, RoomID: room, PlanID: plan, SessionID: session.ID,
			RunID: session.ExternalID, Priority: livecost.PriorityNecessary, Reason: "直播必要互动解意图",
			Attribution: &livecost.Attribution{Source: "interaction"}}), nil
	}
}

func (s *Server) SetPurposeInternalToken(token string) {
	s.purposeInternalToken = strings.TrimSpace(token)
}
func (s *Server) resolveInteractionPurpose(w http.ResponseWriter, r *http.Request) {
	if s.purposeInternalToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Core-Token")), []byte(s.purposeInternalToken)) != 1 {
		writeError(w, 401, "invalid internal token")
		return
	}
	var input struct {
		Tenant int64  `json:"tenant_id"`
		Room   int64  `json:"room_id"`
		Plan   int64  `json:"plan_id"`
		Text   string `json:"text"`
	}
	if readJSON(w, r, &input) != nil || input.Tenant <= 0 || input.Room <= 0 || input.Plan <= 0 || len([]rune(input.Text)) > 500 {
		writeError(w, 400, "invalid purpose scope or text")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 24*time.Second)
	defer cancel()
	plan, err := s.store.GetLiveAgentPlanForRoom(ctx, input.Tenant, input.Room)
	if err != nil || plan.ID != input.Plan {
		writeError(w, 409, "room plan changed")
		return
	}
	published, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, input.Tenant, input.Room)
	if err != nil || published.PlanID != input.Plan {
		writeError(w, 409, "published plan unavailable")
		return
	}
	ref, _ := published.GenerationContext["derived_safety"].(map[string]any)
	hash, _ := ref["source_sha256"].(string)
	asset, err := s.store.GetLiveDerivedSafety(ctx, input.Tenant, input.Plan, hash)
	if hash == "" || err != nil || strings.TrimSpace(asset.SafeFacts) == "" {
		writeError(w, 409, "published facts unavailable")
		return
	}
	prompt, err := interactionpolicy.Read("interaction-purpose-resolution.md")
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	judgePrompt, judgeErr := interactionpolicy.Read("interaction-purpose-judge.md")
	if judgeErr != nil {
		judgePrompt = nil // 判断母版缺失时降级：不启用判断模型层，仍走完整解意图
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		writeError(w, 500, "analysis profile unavailable")
		return
	}
	bindCost := interactionPurposeCostBinder(s.store.GetLiveRuntimeByRoom, input.Tenant, input.Room, input.Plan)
	resolver := interactionpurpose.Resolver{Repo: s.store, Semantic: s.purposeSemantic, Complete: func(ctx context.Context, payload string) (string, error) {
		ctx, err := bindCost(ctx)
		if err != nil {
			return "", err
		}
		result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "interaction_purpose_resolution", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: string(prompt)}, {Role: "user", Content: payload}}, 1800, false)
		return result.Text, err
	}}
	if len(judgePrompt) > 0 {
		resolver.Judge = func(ctx context.Context, payload string) (string, error) {
			ctx, err := bindCost(ctx)
			if err != nil {
				return "", err
			}
			result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "interaction_purpose_judge", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: string(judgePrompt)}, {Role: "user", Content: payload}}, 128, false)
			return result.Text, err
		}
	}
	query := interactionpurpose.Input{Tenant: input.Tenant, Room: input.Room, Plan: input.Plan, Text: input.Text, Facts: asset.SafeFacts, Prompt: string(prompt)}
	// The saved card is a recall index, not a replacement for the approved
	// facts. Give the intent model only the card's dimensions and the facts that
	// match this question.
	if card := domaincard.FromGeneration(published.GenerationContext); card != nil {
		parsed := contentstrategy.Facts(asset.SafeFacts)
		all := make([]domaincard.Fact, 0, len(parsed))
		for _, fact := range parsed {
			all = append(all, domaincard.Fact{ID: fact.ID, Text: fact.Text})
		}
		query.DomainCard = card
		query.CardFacts = domaincard.Match(input.Text, card, all)
	}
	if snapshot := s.loadContentStrategy(ctx, input.Tenant, input.Plan, asset.SafeFacts); snapshot != nil {
		query.Purposes = snapshot.Purposes
	}
	// 自动生成的目的清单优先：它带关键词，覆盖旧快照目的。
	if stored, err := s.store.ListPlanPurposes(ctx, input.Tenant, input.Plan); err == nil && len(stored) > 0 {
		query.Purposes = purposelib.ToContentPurposes(stored)
	}
	result, err := resolver.Resolve(ctx, query)
	if err != nil {
		writeError(w, 502, fmt.Sprintf("purpose resolution failed: %v", err))
		return
	}
	// 未覆盖问题自动进问题库：只记分类和问题，答案留空等运营补。
	if strings.TrimSpace(result.PurposeID) == "" {
		_, _ = s.store.AppendQuestionGap(ctx, input.Tenant, input.Plan, inferGapType(result), refineGapQuestion(result))
	}
	current, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, input.Tenant, input.Room)
	if err != nil || current.ID != published.ID {
		writeError(w, 409, "published facts changed during analysis")
		return
	}
	knowledge, _ := json.Marshal(result)
	writeJSON(w, 200, map[string]any{"question": result.Question, "relevant": result.Relevant, "topic": result.Topic, "purpose_id": result.PurposeID, "source": result.Source, "knowledge": json.RawMessage(knowledge)})
}

// refineGapQuestion turns an intent result into a short, reusable question.
func refineGapQuestion(result interactionpurpose.Result) string {
	text := ""
	if len(result.Angles) > 0 {
		text = strings.TrimSpace(result.Angles[0].Angle)
	}
	if text == "" {
		text = strings.TrimSpace(result.Purpose)
	}
	for _, prefix := range []string{"询问", "了解", "确认", "想知道", "问一下", "在问"} {
		text = strings.TrimPrefix(text, prefix)
	}
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "的")
	if len([]rune(text)) > 40 {
		text = string([]rune(text)[:40])
	}
	if text == "" {
		text = strings.TrimSpace(result.Purpose)
	}
	return text
}

// inferGapType maps an uncovered question to a question-library type.
func inferGapType(result interactionpurpose.Result) string {
	text := result.Purpose
	for _, angle := range result.Angles {
		text += angle.Angle
	}
	switch {
	case strings.Contains(text, "地区"), strings.Contains(text, "包邮"), strings.Contains(text, "运费"), strings.Contains(text, "真空"):
		return "pool_in_out"
	case strings.Contains(text, "能否"), strings.Contains(text, "可以"), strings.Contains(text, "是否"), strings.Contains(text, "能不能"):
		return "yes_no"
	case strings.Contains(text, "多久"), strings.Contains(text, "几天"), strings.Contains(text, "多重"):
		return "number"
	case strings.Contains(text, "怎么"), strings.Contains(text, "如何"):
		return "process"
	}
	return "fact"
}
