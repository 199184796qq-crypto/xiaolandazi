package decisionexecutor

import (
	"context"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func (w *Worker) interactionAudioReuseContext(ctx context.Context, scope workerPaidScope) context.Context {
	reader, ok := w.store.(model.TenantGenerationCachePolicyReader)
	if !ok {
		return ctx
	}
	return ttsgateway.WithInteractionAudioReuse(ctx, scope.session.TenantID, scope.planID, func(c context.Context) (ttsgateway.InteractionAudioPolicy, error) {
		enabled, err := reader.GenerationCacheReuseEnabledForTenant(c, scope.session.TenantID, model.InteractionGenerationCacheReuseSetting)
		if err != nil || !enabled {
			return ttsgateway.InteractionAudioPolicy{Enabled: enabled}, err
		}
		hours, err := reader.AnchorVoiceCacheRetentionHoursForTenant(c, scope.session.TenantID)
		return ttsgateway.InteractionAudioPolicy{Enabled: true, Retention: time.Duration(hours) * time.Hour}, err
	}, checkInteractionControl)
}
