package httpapi

import (
	"context"
	"errors"
	"log"
	"time"

	"livecompanion/management/internal/coreclient"
)

// The ready queue owns its cursor and complete audio units until Core confirms
// delivery. Both newly generated and cached units enter the same startup gate.
// Callbacks keep IO fault injection identical to the production control flow.
type recordingReadyQueue struct {
	pending      []recordingReadyUnit
	programID    string
	generation   uint64
	blockSeq     int
	delivered    int
	startedAt    time.Time
	tenant, room int64
	combine      func(context.Context, []recordingReadyUnit, int) (coreclient.ProgramRefreshTrack, int, error)
	start        func(context.Context, coreclient.ProgramRefreshTrack) (coreclient.ProgramRefreshSnapshot, error)
	append       func(context.Context, string, coreclient.ProgramRefreshTrack) (uint64, error)
	buffer       func(context.Context, string) error
	beforeAppend func(context.Context, coreclient.ProgramRefreshTrack) error
	afterAppend  func(context.Context, coreclient.ProgramRefreshTrack) error
}

func (q *recordingReadyQueue) flush(ctx context.Context, force bool) error {
	if q.programID == "" && !force && !recordingStartReady(q.pending, q.startedAt) {
		return nil
	}
	for len(q.pending) > 0 {
		if err := waitLiveLeasePermit(ctx); err != nil {
			return err
		}
		if q.programID != "" {
			if err := retryLiveOperation(ctx, "检查就绪播放缓冲", 0, nil, func() error { return q.buffer(ctx, q.programID) }); err != nil {
				return err
			}
		}
		take := min(recordingStreamBlockTarget(q.blockSeq), len(q.pending))
		var track coreclient.ProgramRefreshTrack
		consumed := 0
		err := retryLiveOperation(ctx, "整理已就绪话术", 3, nil, func() error {
			var err error
			track, consumed, err = q.combine(ctx, q.pending[:take], q.blockSeq)
			return err
		})
		if err != nil {
			if liveContinuityTerminal(ctx, err) {
				return err
			}
			log.Printf("recording ready unit skipped tenant=%d room=%d block=%d error=%v", q.tenant, q.room, q.blockSeq, err)
			consumed = 1
			track = coreclient.ProgramRefreshTrack{}
		}
		if consumed < 1 || consumed > take {
			consumed = 1
			track = coreclient.ProgramRefreshTrack{}
		}
		if track.AudioURL != "" {
			if q.beforeAppend != nil {
				if err := q.beforeAppend(ctx, track); err != nil {
					return err
				}
			}
			var deliveryErr error
			if q.programID == "" {
				var snapshot coreclient.ProgramRefreshSnapshot
				deliveryErr = retryLiveOperation(ctx, "交付就绪首段", 0, nil, func() error {
					var err error
					snapshot, err = q.start(ctx, track)
					return err
				})
				if deliveryErr == nil {
					q.programID, q.generation = snapshot.ProgramID, snapshot.Generation
				}
			} else {
				var generation uint64
				deliveryErr = retryLiveOperation(ctx, "交付就绪后段", 0, nil, func() error {
					var err error
					generation, err = q.append(ctx, q.programID, track)
					return err
				})
				if deliveryErr == nil {
					q.generation = generation
				}
			}
			if deliveryErr != nil {
				var rejected *mainlineNotSubmitted
				if !errors.As(deliveryErr, &rejected) {
					return deliveryErr
				}
				log.Printf("recording ready block rejected tenant=%d room=%d block=%d error=%v", q.tenant, q.room, q.blockSeq, deliveryErr)
			} else {
				q.delivered++
				log.Printf("recording stream block appended tenant=%d room=%d units=%d safe_points=%d generation=%d", q.tenant, q.room, consumed, len(track.SafePoints), q.generation)
			}
			if q.afterAppend != nil {
				if err := q.afterAppend(ctx, track); err != nil {
					return err
				}
			}
		}
		// Keep the exact ready track on an unknown outcome. Only a confirmed
		// acknowledgement or definite pre-acceptance rejection advances it.
		q.pending = q.pending[consumed:]
		q.blockSeq++
	}
	return nil
}
