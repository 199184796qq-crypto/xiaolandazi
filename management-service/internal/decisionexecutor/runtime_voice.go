package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
	"strings"
)

func runtimeVoiceProfile(identity model.LiveAgentVoiceIdentity) model.VoiceProfile {
	rate := identity.Rate
	if rate < 0.5 || rate > 2 {
		rate = 1
	}
	return model.VoiceProfile{Name: strings.TrimSpace(identity.Name), Provider: strings.TrimSpace(identity.Provider), VoiceID: strings.TrimSpace(identity.VoiceID), CloneStatus: "ready", Config: map[string]any{
		"source": strings.TrimSpace(identity.Source), "target_model": strings.TrimSpace(identity.Model), "rate": rate,
		"identity_version": strings.TrimSpace(identity.Version), "binding_id": identity.BindingID,
		"emotion_enabled": identity.EmotionEnabled != nil && *identity.EmotionEnabled,
	}}
}

type runtimeVoiceSource interface {
	RuntimeVoiceIdentity(context.Context, int64, int64, int64) (model.LiveAgentVoiceIdentity, error)
}

func (w *Worker) currentRuntimeVoice(ctx context.Context, tenant, room int64, published model.LiveAgentPlanVersion) (model.LiveAgentVoiceIdentity, error) {
	if source, ok := w.store.(runtimeVoiceSource); ok {
		identity, err := source.RuntimeVoiceIdentity(ctx, tenant, room, published.ID)
		if err != nil {
			return identity, err
		}
		if identity.VoiceID != "" {
			return identity, nil
		}
	}
	return published.VoiceIdentity, nil
}
