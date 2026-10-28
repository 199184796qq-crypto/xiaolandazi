package agentgateway

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// A model-independent identity for disposable planning caches. Business code
// names the stage; the gateway alone resolves the operator's chosen model.
func (g *Gateway) ModelRoleIdentity(ctx context.Context, stage string) (string, error) {
	if g.speechModels == nil {
		return g.defaultProvider + ":" + g.defaultModel, nil
	}
	id := ""
	if roles, ok := g.speechModels.(TextRoleResolver); ok {
		var err error
		id, err = roles.ResolveTextRole(ctx, stage)
		if err != nil {
			return "", err
		}
	}
	p, err := g.resolveStageProfile(ctx, stage, id)
	if err != nil {
		return "", err
	}
	if p == nil {
		return g.defaultProvider + ":" + g.defaultModel, nil
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(p.ID+":"+p.Model+":"+p.BaseURL+":"+p.SystemPrompt))), nil
}

// Roles describe work, not model names. Existing per-task prompts stay separate.
func UsesSpeechModel(stage string) bool {
	switch stage {
	case "mainline_text", "speech_generation", "speech_review", "speech_style_wrapper", "score_feedback", "style_feedback_review", "mainline_p0_context", "interaction_bridge", "interaction_return_bridge", "return_bridge":
		return true
	default:
		return false
	}
}

type modelResolverContextKey struct{}

// Review uses the operator-owned review role. Planning and normalization remain
// analysis; registering existing review stages does not add model calls.
func UsesReviewModel(stage string) bool {
	switch stage {
	case "mainline_p0_context", "speech_review", "interaction_p0_context", "fact_fields_normalize", "fact_fields_normalize_review", "fact_expression_examples_review", "fact_template_influence_review", "speech_constraint_review", "speech_constraint_text_review", "angle_direction_review", "content_pool_task_review", "content_pool_fact_scope_review", "mainline_final_content_review", "fact_gap_check":
		return true
	default:
		return false
	}
}

// Interaction answers are spoken to viewers. The operator may pin them to a
// named role instead of the room's leased 口播 model, so these stages resolve
// outside the model pool and use the operator's own profile and key.
func UsesInteractionAnswerModel(stage string) bool {
	switch stage {
	case "interaction_single_direct_answer", "interaction_single_greet", "interaction_single_answer", "interaction_no_fact_boundary", "interaction_boundary_line", "interaction_covered_facts", "interaction_welcome_intro", "interaction_welcome_humanize", "interaction_welcome_highlight", "interaction_welcome_greeting", "interaction_welcome_hook", "interaction_p0_sentence_rewrite", "interaction_order_thanks", "interaction_order_fulfillment", "interaction_addressing_plan":
		return true
	default:
		return false
	}
}

// Intent mining for interaction questions is pinned the same way: the operator
// answers viewers with the review role, without the room pool taking over.
func UsesInteractionPurposeModel(stage string) bool {
	switch stage {
	case "interaction_purpose_resolution", "interaction_purpose_judge":
		return true
	default:
		return false
	}
}

// UsesOperatorPinnedModel reports stages that must resolve outside the room
// model pool and use the operator's own profile.
func UsesOperatorPinnedModel(stage string) bool {
	return UsesInteractionAnswerModel(stage) || UsesInteractionPurposeModel(stage)
}

// ResolveStageModel reports the provider identity and model a stage will use,
// without sending a request. Billing uses it so a paid record names the model
// that actually answered instead of the environment's default label.
func (g *Gateway) ResolveStageModel(ctx context.Context, stage string) (string, string, error) {
	if g == nil || g.speechModels == nil {
		return "", "", nil
	}
	id := ""
	if roles, ok := g.speechModels.(TextRoleResolver); ok {
		var err error
		id, err = roles.ResolveTextRole(ctx, stage)
		if err != nil {
			return "", "", err
		}
	}
	p, err := g.resolveStageProfile(ctx, stage, id)
	if err != nil || p == nil {
		return "", "", err
	}
	return "anchor:" + p.ID, p.Model, nil
}

// OperatorModelResolver bypasses the room model pool for pinned stages.
type OperatorModelResolver interface {
	ResolveOperatorSpeechModel(context.Context, string) (*SpeechModel, error)
}

func (g *Gateway) resolveStageProfile(ctx context.Context, stage, id string) (*SpeechModel, error) {
	if UsesOperatorPinnedModel(stage) {
		if pinned, ok := g.speechModels.(OperatorModelResolver); ok {
			return pinned.ResolveOperatorSpeechModel(ctx, id)
		}
	}
	return g.speechModels.ResolveSpeechModel(ctx, id)
}

func WithModelResolver(ctx context.Context, resolver SpeechModelResolver) context.Context {
	return context.WithValue(ctx, modelResolverContextKey{}, resolver)
}

func NewFromContext(ctx context.Context) *Gateway {
	g := NewFromEnv()
	if resolver, ok := ctx.Value(modelResolverContextKey{}).(SpeechModelResolver); ok {
		g.WithSpeechModels(resolver)
	}
	return g
}
