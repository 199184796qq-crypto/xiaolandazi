package agentgateway

import "context"

// Roles describe work, not model names. Existing per-task prompts stay separate.
func UsesSpeechModel(stage string) bool {
	switch stage {
	case "mainline_text", "speech_generation", "speech_review", "speech_style_wrapper", "rolling_feedback", "score_feedback", "accepted_macro_feedback", "rolling_style_feedback", "style_feedback_review", "mainline_p0_context", "interaction_bridge", "interaction_return_bridge", "return_bridge":
		return true
	default:
		return false
	}
}

type modelResolverContextKey struct{}

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
