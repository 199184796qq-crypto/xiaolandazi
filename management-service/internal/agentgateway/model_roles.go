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
	p, err := g.speechModels.ResolveSpeechModel(ctx, id)
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
	case "mainline_text", "speech_generation", "interaction_agent_answer", "speech_review", "speech_style_wrapper", "score_feedback", "style_feedback_review", "mainline_p0_context", "interaction_bridge", "interaction_return_bridge", "return_bridge":
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
