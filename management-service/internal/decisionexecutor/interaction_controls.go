package decisionexecutor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"livecompanion/management/internal/model"
)

var errInteractionDisabled = errors.New("互动已关闭，未播任务已作废")

type roomInteractionControlStore interface {
	GetRoomInteractionControls(context.Context, int64, int64) (model.RoomInteractionControls, error)
}
type interactionControlKey struct{}
type interactionControlGuard struct {
	worker     *Worker
	session    model.LiveRuntimeSession
	kind       string
	generation uint64
	done       chan struct{}
	once       sync.Once
}

func interactionControlKind(item decisionItem) string {
	kind := item.InteractionDecision.PrimaryEvent
	if kind == "" {
		kind = item.MissionKind
	}
	return model.InteractionControlKind(kind)
}
func manualControlBypass(item decisionItem) bool {
	// These are explicit human speech actions, independent of automatic schedulers.
	for _, source := range item.Sources {
		if source == "manual" {
			return true
		}
	}
	return false
}
func (w *Worker) readInteractionControls(ctx context.Context, session model.LiveRuntimeSession) (model.RoomInteractionControls, error) {
	if source, ok := w.store.(roomInteractionControlStore); ok {
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return source.GetRoomInteractionControls(readCtx, session.TenantID, session.RoomID)
	}
	return model.RoomInteractionControls{}, nil
}
func (g *interactionControlGuard) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	controls, err := g.worker.readInteractionControls(ctx, g.session)
	if err != nil {
		return fmt.Errorf("读取互动开关失败：%w", err)
	}
	if !controls.Enabled(g.kind) || controls.DisabledAt[g.kind] != g.generation {
		return errInteractionDisabled
	}
	return nil
}
func (g *interactionControlGuard) stop() { g.once.Do(func() { close(g.done) }) }

func (w *Worker) watchInteractionControls(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (context.Context, func(), error) {
	if manualControlBypass(item) {
		return ctx, func() {}, nil
	}
	controls, err := w.readInteractionControls(ctx, session)
	if err != nil {
		return ctx, func() {}, err
	}
	kind := interactionControlKind(item)
	if !controls.Enabled(kind) || controls.DisabledAt[kind] > item.ControlsRevision {
		w.dropPreparedControl(session, kind)
		return ctx, func() {}, errInteractionDisabled
	}
	guarded, cancel := context.WithCancel(ctx)
	g := &interactionControlGuard{worker: w, session: session, kind: kind, generation: controls.DisabledAt[kind], done: make(chan struct{})}
	guarded = context.WithValue(guarded, interactionControlKey{}, g)
	if _, ok := w.store.(roomInteractionControlStore); ok {
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-g.done:
					return
				case <-guarded.Done():
					return
				case <-ticker.C:
					if g.check(guarded) != nil {
						w.dropPreparedControl(session, kind)
						cancel()
						return
					}
				}
			}
		}()
	}
	return guarded, func() { g.stop(); cancel() }, nil
}
func checkInteractionControl(ctx context.Context) error {
	if guard, ok := ctx.Value(interactionControlKey{}).(*interactionControlGuard); ok {
		return guard.check(ctx)
	}
	return ctx.Err()
}
func handoffInteractionControl(ctx context.Context) {
	// Once HTTP dispatch starts, Core's task/epoch/first-PCM fences own cancellation.
	// Do not cancel an HTTP response after the matching audio has actually begun.
	if guard, ok := ctx.Value(interactionControlKey{}).(*interactionControlGuard); ok {
		guard.stop()
	}
}
func (w *Worker) dropPreparedControl(session model.LiveRuntimeSession, kind string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, value := range w.prepared {
		if key.Tenant == session.TenantID && key.Room == session.RoomID && key.Session == session.ID && interactionControlKind(value.Item) == kind {
			delete(w.prepared, key)
		}
	}
	if kind == "time_report" {
		for key := range w.timeReportDrafts {
			if key.Tenant == session.TenantID && key.Room == session.RoomID && key.Session == session.ID {
				delete(w.timeReportDrafts, key)
			}
		}
	}
}
func (w *Worker) interactionPreparationScope(ctx context.Context, session model.LiveRuntimeSession, item decisionItem) (string, error) {
	scope, err := w.interactionContentScope(ctx, session, item)
	if err != nil || manualControlBypass(item) {
		return scope, err
	}
	controls, err := w.readInteractionControls(ctx, session)
	if err != nil {
		return "", err
	}
	kind := interactionControlKind(item)
	if !controls.Enabled(kind) {
		return "", errInteractionDisabled
	}
	if generation := controls.DisabledAt[kind]; generation > 0 {
		return interactionDigest([]any{scope, kind, generation}), nil
	}
	return scope, nil
}
