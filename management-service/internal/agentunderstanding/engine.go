package agentunderstanding

import (
	"context"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

const (
	ProtocolVersion = "1"
	KindChat        = "chat"
	KindCommand     = "command"
	KindClarify     = "clarify"
)

// ContextFrame is the normalized interaction context passed to every
// interpreter. Business pages and mobile clients do not decide semantics;
// they only provide context and consume UnifiedIntent.
type ContextFrame struct {
	Domain        string        `json:"domain"`
	TenantID      int64         `json:"tenant_id,omitempty"`
	RoomID        int64         `json:"room_id,omitempty"`
	PlanID        int64         `json:"plan_id,omitempty"`
	CurrentMode   string        `json:"current_mode,omitempty"`
	CurrentPath   string        `json:"current_path,omitempty"`
	PendingAction string        `json:"pending_action,omitempty"`
	History       []HistoryItem `json:"history,omitempty"`
	HasImages     bool          `json:"has_images,omitempty"`
	Now           time.Time     `json:"now"`
}

type HistoryItem struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type InterpretRequest struct {
	Message   string       `json:"message"`
	Frame     ContextFrame `json:"frame"`
	ImageURLs []string     `json:"image_urls,omitempty"`
}

// UnifiedIntent is deliberately business-neutral. Domain adapters can map
// target/changes to strongly typed command payloads after permission checks.
type UnifiedIntent struct {
	ProtocolVersion string         `json:"protocol_version"`
	Kind            string         `json:"kind"`
	Intent          string         `json:"intent"`
	Target          map[string]any `json:"target,omitempty"`
	Changes         map[string]any `json:"changes,omitempty"`
	Missing         []string       `json:"missing,omitempty"`
	Confidence      float64        `json:"confidence"`
	Reply           string         `json:"reply,omitempty"`
	Engine          string         `json:"engine"`
	Provider        string         `json:"provider,omitempty"`
	Model           string         `json:"model,omitempty"`
	LatencyMS       int64          `json:"latency_ms,omitempty"`
}

type IntentInterpreter interface {
	Interpret(context.Context, InterpretRequest) (UnifiedIntent, error)
}

// NormalizePolicy keeps policy rows safe and backward compatible.
func NormalizePolicy(input model.AgentUnderstandingPolicy) model.AgentUnderstandingPolicy {
	input.ScopeType = strings.ToLower(strings.TrimSpace(input.ScopeType))
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	input.Model = strings.TrimSpace(input.Model)
	input.BudgetFallback = strings.ToLower(strings.TrimSpace(input.BudgetFallback))
	if input.Mode != model.AgentUnderstandingModeProgram && input.Mode != model.AgentUnderstandingModeModel && input.Mode != model.AgentUnderstandingModeAuto {
		input.Mode = model.AgentUnderstandingModeModel
	}
	if input.Provider == "" {
		input.Provider = agentgateway.ProviderQwen
	}
	if input.Model == "" {
		input.Model = agentgateway.DefaultQwenModel
	}
	if input.MaxContextMessages <= 0 {
		input.MaxContextMessages = 10
	}
	if input.MaxContextMessages > 30 {
		input.MaxContextMessages = 30
	}
	if input.MaxTokens <= 0 {
		input.MaxTokens = 900
	}
	if input.MaxTokens > 4000 {
		input.MaxTokens = 4000
	}
	if input.TimeoutMS <= 0 {
		input.TimeoutMS = 12000
	}
	if input.TimeoutMS < 1000 {
		input.TimeoutMS = 1000
	}
	if input.TimeoutMS > 60000 {
		input.TimeoutMS = 60000
	}
	if input.MinConfidence <= 0 || input.MinConfidence > 1 {
		input.MinConfidence = 0.72
	}
	if input.BudgetFallback == "" {
		input.BudgetFallback = model.AgentUnderstandingBudgetFallbackProgram
	}
	return input
}

func DefaultPolicy() model.AgentUnderstandingPolicy {
	return NormalizePolicy(model.AgentUnderstandingPolicy{
		ScopeType:           model.AgentUnderstandingScopeSystem,
		ScopeID:             0,
		Mode:                model.AgentUnderstandingModeModel,
		Provider:            agentgateway.ProviderQwen,
		Model:               agentgateway.DefaultQwenModel,
		MaxContextMessages:  10,
		MaxTokens:           900,
		TimeoutMS:           12000,
		MonthlyBudgetTokens: 0,
		BudgetFallback:      model.AgentUnderstandingBudgetFallbackProgram,
		MinConfidence:       0.72,
		Enabled:             true,
	})
}

func TrimHistory(items []HistoryItem, limit int) []HistoryItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[len(items)-limit:]
}

// UseModel returns whether a domain should call an LLM after a program pass.
// PROGRAM never calls a model. MODEL always calls the model. AUTO calls the
// model only when the program resolver could not reach the configured
// confidence threshold.
func UseModel(policy model.AgentUnderstandingPolicy, programResolved bool, programConfidence float64) bool {
	policy = NormalizePolicy(policy)
	switch policy.Mode {
	case model.AgentUnderstandingModeProgram:
		return false
	case model.AgentUnderstandingModeAuto:
		return !programResolved || programConfidence < policy.MinConfidence
	default:
		return true
	}
}

func NeedsClarification(policy model.AgentUnderstandingPolicy, confidence float64) bool {
	policy = NormalizePolicy(policy)
	return confidence > 0 && confidence < policy.MinConfidence
}

func Timeout(policy model.AgentUnderstandingPolicy) time.Duration {
	policy = NormalizePolicy(policy)
	return time.Duration(policy.TimeoutMS) * time.Millisecond
}
