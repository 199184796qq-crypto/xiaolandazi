package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"livecompanion/management/internal/model"
	"net/http"
	"time"
)

var errWarmupPurchaseNotAllowed = errors.New("暖场任务已失效或暂无播放窗口，未购买TTS")

// The earlier preflight precedes reading rules, instruction work and the shared
// TTS scheduler. Recheck at the admitted provider attempt, not before the queue.
// This guard neither releases/renews a task nor reserves any played duration.
func (w *Worker) checkWarmupTTSPurchase(ctx context.Context, session model.LiveRuntimeSession, item decisionItem, text string, rate float64) error {
	checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	request := map[string]any{"duration_ms": 3000, "until": item.ExpiresAt, "stage": "tts", "text": text, "rate": rate, "preparation_ms": w.estimateTTPreparation(text).Milliseconds()}
	resp, err := w.core.DoRoom(checkCtx, session.TenantID, session.RoomID, http.MethodPost, fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/preflight", session.RoomID, item.ID), tenantQuery(session.TenantID), request)
	if err != nil {
		return fmt.Errorf("%w: %v", errWarmupPurchaseNotAllowed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("%w: http %d", errWarmupPurchaseNotAllowed, resp.StatusCode)
	}
	var result struct {
		Allowed *bool  `json:"allowed"`
		Reason  string `json:"reason"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&result) != nil || result.Allowed == nil || !*result.Allowed {
		return fmt.Errorf("%w: %s", errWarmupPurchaseNotAllowed, result.Reason)
	}
	return nil
}
