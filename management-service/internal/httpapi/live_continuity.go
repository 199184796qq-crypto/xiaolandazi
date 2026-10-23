package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/errordetail"
)

// A technical failure does not transfer ownership or end a live session. Retry
// the smallest operation whose inputs we still own, retaining ready audio and
// idempotency IDs. Cancellation and a positively confirmed owner change win.
func liveContinuityTerminal(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, anchorcycle.ErrStale)
}

type liveLeasePermitKey struct{}

// Permit only the producer holding this exact token. Unknown liveness blocks
// new work while already-admitted Core audio continues to drain.
func waitLiveLeasePermit(ctx context.Context) error {
	until, ok := ctx.Value(liveLeasePermitKey{}).(*atomic.Int64)
	if !ok {
		return ctx.Err()
	}
	for until.Load() <= time.Now().UnixNano() {
		if err := supplyWait(ctx, 250*time.Millisecond); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func transientLiveOperation(err error) bool {
	var network net.Error
	return errors.Is(err, anchorcycle.ErrBusy) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.As(err, &network)
}

// Keep Core's diagnostic origin, while distinguishing a complete, explicit
// pre-acceptance rejection from an unknown result that must retain its unit ID.
func liveHandoffRejection(resp *http.Response) error {
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
	var reply struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		Stage     string `json:"stage"`
		Retryable *bool  `json:"retryable"`
	}
	decoded := readErr == nil && json.Unmarshal(raw, &reply) == nil
	if decoded && (reply.Code == "audio_start_cancelled" || reply.Code == "audio_program_state_changed") {
		// A complete lifecycle fence rejection is conclusive. Retrying the
		// next startup unit here could revive a manually stopped old session.
		return anchorcycle.ErrStale
	}
	code, stage := "core_handoff_unconfirmed", "播放服务"
	if reply.Code != "" {
		code = reply.Code
	}
	if reply.Stage != "" {
		stage = reply.Stage
	}
	reason := errordetail.Safe(reply.Error)
	if reason == "" {
		reason = fmt.Sprintf("播放服务响应状态%d", resp.StatusCode)
	}
	err := &errordetail.Fault{Detail: errordetail.Detail{Code: code, Stage: stage, HTTPStatus: resp.StatusCode,
		RequestID: errordetail.Safe(resp.Header.Get("X-Request-ID")), Message: reason,
		Action: "保留当前声音单元，核对后自动重试。"}, Cause: readErr}
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity || (decoded && reply.Retryable != nil && !*reply.Retryable) {
		err.Detail.Action = "隔离未被接收的声音单元，继续后续合格内容。"
		return beforeMainlineSubmit(err)
	}
	return err
}

func liveContinuityDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 5 {
		attempt = 5
	}
	return time.Second << (attempt - 1) // 1, 2, 4, 8, 16 seconds; never a hot loop.
}

func liveContinuityAttempt(fn func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("live operation panic recovered: %s\n%s", errordetail.Safe(fmt.Sprint(recovered)), debug.Stack())
			err = fmt.Errorf("直播任务遇到内部异常，正在自动恢复")
		}
	}()
	return fn()
}

func retryLiveOperation(ctx context.Context, stage string, limit int, wait func(context.Context, time.Duration) error, fn func() error) error {
	if wait == nil {
		wait = supplyWait
	}
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := waitLiveLeasePermit(ctx); err != nil {
			return err
		}
		err := liveContinuityAttempt(fn)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err == nil || liveContinuityTerminal(ctx, err) {
			return err
		}
		var rejected *mainlineNotSubmitted
		if errors.As(err, &rejected) {
			return err
		}
		if limit > 0 && attempt >= limit {
			return err
		}
		if attempt == 1 || attempt%10 == 0 {
			log.Printf("live operation retry stage=%s attempt=%d error=%s", stage, attempt, errordetail.Safe(err.Error()))
		}
		if err := wait(ctx, liveContinuityDelay(attempt)); err != nil {
			return err
		}
	}
}
