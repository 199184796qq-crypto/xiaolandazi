package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
)

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
