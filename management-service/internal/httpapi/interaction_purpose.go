package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/interactionpurpose"
)

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
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		writeError(w, 500, "analysis profile unavailable")
		return
	}
	resolver := interactionpurpose.Resolver{Repo: s.store, Semantic: s.purposeSemantic, Complete: func(ctx context.Context, payload string) (string, error) {
		ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: input.Tenant, RoomID: input.Room, PlanID: input.Plan})
		result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "interaction_purpose_resolution", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: string(prompt)}, {Role: "user", Content: payload}}, 1800, false)
		return result.Text, err
	}}
	query := interactionpurpose.Input{Tenant: input.Tenant, Room: input.Room, Plan: input.Plan, Text: input.Text, Facts: asset.SafeFacts, Prompt: string(prompt)}
	if snapshot := s.loadContentStrategy(ctx, input.Tenant, input.Plan, asset.SafeFacts); snapshot != nil {
		query.Purposes = snapshot.Purposes
	}
	result, err := resolver.Resolve(ctx, query)
	if err != nil {
		writeError(w, 502, fmt.Sprintf("purpose resolution failed: %v", err))
		return
	}
	current, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, input.Tenant, input.Room)
	if err != nil || current.ID != published.ID {
		writeError(w, 409, "published facts changed during analysis")
		return
	}
	knowledge, _ := json.Marshal(result)
	writeJSON(w, 200, map[string]any{"question": result.Question, "relevant": result.Relevant, "topic": result.Topic, "purpose_id": result.PurposeID, "source": result.Source, "knowledge": json.RawMessage(knowledge)})
}
