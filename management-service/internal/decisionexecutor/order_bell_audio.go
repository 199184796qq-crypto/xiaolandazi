package decisionexecutor

import (
	"context"
	"errors"
	"log"
	"strings"

	"livecompanion/management/internal/orderbell"
	"livecompanion/management/internal/ttsgateway"
)

func (w *Worker) markOrderBell(item *decisionItem, intentType string) {
	order := w.orderBell().Decide(intentType, orderOriginalText(item), item.ID)
	applyOrderBell(item, order)
}

// Only the associated viewer's current original message is evidence. Summary,
// title, generated body/meaning and historical messages cannot prove an order.
func orderOriginalText(item *decisionItem) string {
	for _, raw := range item.SampleQuestions {
		if text := strings.TrimSpace(raw); text != "" {
			return text
		}
	}
	return ""
}

func applyOrderBell(item *decisionItem, order orderbell.Decision) {
	item.OrderBell = order.IsOrder && order.Ring
	if order.IsOrder {
		log.Printf("order detected decision=%s flavor=%q ring=%t phrase=%q", item.ID, order.Flavor, order.Ring, order.Phrase)
	}
}

// Only canonical order behavior templates arrive here after their scope, count
// and body have been validated. Their intent label is generically "闲聊".
func (w *Worker) markConfirmedOrderBell(item *decisionItem) {
	applyOrderBell(item, w.orderBell().DecideConfirmed(orderOriginalText(item), item.ID))
}

// Run once after the final TTS result and correction checks, before dispatch or
// prepared-audio retention. Reclaiming a safe cut reuses the combined URL.
func (w *Worker) prependOrderBell(ctx context.Context, item *decisionItem, speechURL string) string {
	if !item.OrderBell {
		return speechURL
	}
	compose := w.orderAudioComposer
	if compose == nil {
		compose = ttsgateway.PrependOrderBell
	}
	audio, err := compose(ctx, speechURL)
	if err != nil {
		reason := "audio_unavailable"
		if errors.Is(err, ttsgateway.ErrOrderBellMissing) {
			reason = "missing_asset"
			log.Printf("order_bell_missing decision=%s asset=order-bell.wav", item.ID)
		}
		log.Printf("order bell skipped decision=%s reason=%s", item.ID, reason)
		return speechURL
	}
	log.Printf("order bell prepared decision=%s asset=order-bell.wav duration_ms=%d gap_ms=%d total_ms=%d", item.ID, audio.BellDurationMS, audio.GapDurationMS, audio.DurationMS)
	// This is preparation, not proof of physical playback. Core's existing PCM
	// receipts remain the authority for actual starting/completion.
	return audio.AudioURL
}
