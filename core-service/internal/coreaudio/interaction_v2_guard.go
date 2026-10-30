package coreaudio

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/core/internal/messagecaptain"
	"time"
)

var errInteractionV2Wait = errors.New("互动播出额度或原始期限暂不允许，保留已合成声音继续排队")

func (c *Client) reserveInteractionV2(ctx context.Context, room int64, decision string, durationMS int, start time.Time) error {
	if decision == "" {
		return nil
	}
	c.mu.RLock()
	captain := c.messages
	c.mu.RUnlock()
	if captain == nil {
		return nil
	}
	raw, _ := json.Marshal(map[string]any{"decision_id": decision, "duration_ms": durationMS, "start_at": start})
	if failures := captain.Publish(ctx, messagecaptain.Message{Kind: "interaction.v2.reserve-audio", RoomID: room, Payload: raw}); len(failures) > 0 {
		return errInteractionV2Wait
	}
	return nil
}
