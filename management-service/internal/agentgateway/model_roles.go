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
	if stage == "qa_pairs_organize" {
		return true
	}
	switch stage {
	case "mainline_p0_context", "speech_review", "interaction_p0_context", "fact_fields_normalize", "fact_fields_normalize_review", "fact_expression_examples_review", "fact_template_influence_review", "speech_constraint_review", "speech_constraint_text_review", "angle_direction_review", "content_pool_task_review", "content_pool_fact_scope_review", "mainline_final_content_review", "fact_gap_check", "material_import_analyze", "selling_point_extraction", "product_selling_point_extraction", "selling_point_incremental_update", "selling_point_incremental_review", "wheel_p0_rewrite", "fact_content_purposes", "fact_content_angles", "script_auto_planner", "script_auto_writer", "script_auto_fact_review", "script_auto_p0_sentence_rewrite":
		return true
	default:
		return false
	}
}

// These spoken interaction stages use the operator review role when they
// aren't routed through the active room's model lease.
func UsesInteractionAnswerModel(stage string) bool {
	switch stage {
	case "interaction_single_direct_answer", "interaction_single_greet", "interaction_single_answer", "interaction_no_fact_boundary", "interaction_boundary_line", "interaction_covered_facts", "interaction_welcome_intro", "interaction_welcome_humanize", "interaction_welcome_highlight", "interaction_welcome_greeting", "interaction_welcome_hook", "interaction_welcome_writer", "interaction_p0_sentence_rewrite", "interaction_order_thanks", "interaction_order_fulfillment", "interaction_addressing_plan":
		return true
	default:
		return false
	}
}

// Intent and answer tests use the operator's review role.
func UsesInteractionPurposeModel(stage string) bool {
	switch stage {
	case "interaction_purpose_resolution", "interaction_purpose_judge", "interaction_engagement_announcement", "danmaku_test_intent", "danmaku_test_intent_qa", "danmaku_test_answer", "interaction_answerflow_intent", "interaction_answerflow_intent_qa", "interaction_answerflow_answer", "interaction_answerflow_batch_intent", "interaction_answerflow_recall_query":
		return true
	default:
		return false
	}
}

// UsesOperatorPinnedModel reports the configured role outside a live lease.
func UsesOperatorPinnedModel(stage string) bool {
	return stage == "qa_pairs_organize" || stage == "script_auto_planner" || stage == "script_auto_writer" || stage == "script_auto_fact_review" || stage == "script_auto_p0_sentence_rewrite" || UsesInteractionAnswerModel(stage) || UsesInteractionPurposeModel(stage)
}

// Use the same routing decision for generation, cost and model-exchange logs.
func UsesOperatorPinnedModelForContext(ctx context.Context, stage string) bool {
	if ContextRoomModelScope(ctx).RequireLease {
		switch stage {
		case "interaction_purpose_resolution", "interaction_purpose_judge", "interaction_engagement_announcement",
			"interaction_answerflow_intent", "interaction_answerflow_intent_qa", "interaction_answerflow_batch_intent", "interaction_answerflow_answer", "interaction_answerflow_recall_query",
			"interaction_single_direct_answer", "interaction_single_greet", "interaction_single_answer",
			"interaction_no_fact_boundary", "interaction_boundary_line", "interaction_covered_facts",
			"interaction_addressing_plan", "interaction_p0_sentence_rewrite":
			return false
		}
	}
	return UsesOperatorPinnedModel(stage)
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
	if UsesOperatorPinnedModelForContext(ctx, stage) {
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
