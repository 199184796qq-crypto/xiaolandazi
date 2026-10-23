package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/errordetail"
	"livecompanion/management/internal/model"
	"net/http"
	"sync"
	"time"
)

func (s *Server) runLiveAnchorProducer(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
	if recordingVoiceMode(version) == recordingStreamMode {
		return s.runRecordingStreamProducer(ctx, tenant, room, runtimeID, version)
	}
	var state *anchorcycle.State
	err := retryLiveOperation(ctx, "初始化本场主线", 0, nil, func() error {
		var err error
		state, err = s.createLiveAnchorRun(ctx, tenant, room, runtimeID, version)
		return err
	})
	if err != nil {
		return errordetail.WithStage("方案与开场初始化", err)
	}
	var mu sync.Mutex
	delivery := *state.Delivery
	var actualReceipts []anchorcycle.AnglePlaybackReceipt
	var archivedPlaybackSequence uint64
	supply := newCycleSupply(s, ctx, state)
	defer supply.stop()
	supply.startup = func(ctx context.Context, stage string) {
		s.reportLiveStartup(ctx, tenant, room, runtimeID, stage)
	}
	supply.guard = func(ctx context.Context) (bool, error) {
		paused, err := s.checkLiveAnchorOwner(ctx, tenant, room, runtimeID, version.ID)
		if err != nil || paused {
			return paused, err
		}
		mu.Lock()
		program := delivery.ProgramID
		mu.Unlock()
		if program == "" {
			return false, nil
		}
		snapshot, err := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
		if err != nil {
			return false, err
		}
		if !snapshot.Running || snapshot.ProgramID != program || snapshot.VersionID != version.ID {
			return false, anchorcycle.ErrStale
		}
		s.observeLibraryPlayback(ctx, tenant, room, runtimeID, snapshot.MainlinePlaybackReceipts, snapshot.PlaybackReceipts)
		if len(snapshot.MainlinePlaybackReceipts) > 0 {
			last := snapshot.MainlinePlaybackReceipts[len(snapshot.MainlinePlaybackReceipts)-1].Sequence
			if last > archivedPlaybackSequence {
				s.archiveMainlineReceipts(ctx, tenant, room, runtimeID, snapshot)
				archivedPlaybackSequence = last
			}
		}
		mu.Lock()
		actualReceipts = append([]anchorcycle.AnglePlaybackReceipt(nil), snapshot.MainlinePlaybackReceipts...)
		mu.Unlock()
		return snapshot.Suspended, nil
	}
	supply.externalStock = func(ctx context.Context) (int64, error) {
		mu.Lock()
		program := delivery.ProgramID
		mu.Unlock()
		if program == "" {
			return 0, nil
		}
		snapshot, err := s.core.GetRoomProgramSnapshot(ctx, tenant, room)
		if err != nil {
			return 0, err
		}
		if snapshot.ProgramID != program {
			return 0, anchorcycle.ErrStale
		}
		s.observeLibraryPlayback(ctx, tenant, room, runtimeID, snapshot.MainlinePlaybackReceipts, snapshot.PlaybackReceipts)
		mu.Lock()
		actualReceipts = append([]anchorcycle.AnglePlaybackReceipt(nil), snapshot.MainlinePlaybackReceipts...)
		mu.Unlock()
		return snapshot.BufferedMainlineMS, nil
	}
	supply.beforeText = func(ctx context.Context, latest *anchorcycle.State) error {
		mu.Lock()
		copy := delivery
		receipts := append([]anchorcycle.AnglePlaybackReceipt(nil), actualReceipts...)
		copy.DeliveredCandidates = append([]string(nil), delivery.DeliveredCandidates...)
		mu.Unlock()
		changed := latest.Delivery.ProgramID != copy.ProgramID || len(latest.Delivery.DeliveredCandidates) != len(copy.DeliveredCandidates)
		if !changed && len(copy.DeliveredCandidates) > 0 {
			changed = latest.Delivery.DeliveredCandidates[len(copy.DeliveredCandidates)-1] != copy.DeliveredCandidates[len(copy.DeliveredCandidates)-1]
		}
		latest.Delivery = &copy
		for _, segment := range latest.Accepted {
			latest.RegisterAngleTrack(segment)
		}
		priorSeq := latest.AngleReceiptSequence
		uses := latest.ApplyAnglePlayback(copy.ProgramID, receipts)
		changed = changed || priorSeq != latest.AngleReceiptSequence
		latest.AnglePendingUses = append(latest.AnglePendingUses, uses...)
		pending := []anchorcycle.AnglePlayedUse{}
		for _, use := range latest.AnglePendingUses {
			if err := s.store.RecordAngleDirectionPlay(ctx, tenant, version.PlanID, copy.ProgramID, use); err != nil {
				pending = append(pending, use)
			}
		}
		changed = changed || len(pending) != len(latest.AnglePendingUses)
		if len(pending) > 0 && len(uses) > 0 {
			latest.Warn("部分角度使用历史暂未保存，保留待重试，本场继续供料")
		}
		latest.AnglePendingUses = pending
		removed := latest.CompactDeliveredHistory()
		if changed || len(removed) > 0 {
			if err := s.anchorCycles.Save(ctx, latest); err != nil {
				return err
			}
		}
		if len(removed) > 0 {
			if pruner, ok := s.anchorCycles.(interface {
				PruneDeliveredVoices(context.Context, int64, int64, int64, string, []string) error
			}); ok {
				if err := pruner.PruneDeliveredVoices(ctx, tenant, version.PlanID, room, latest.RunID, removed); err != nil {
					return err
				}
			}
		}
		return nil
	}
	supply.onReady = func(ctx context.Context, latest *anchorcycle.State, segment anchorcycle.Segment) error {
		// Re-sign at handoff, including after a long AI pause; object lifetime is
		// independent of the temporary URL lifetime.
		if segment.Selected.LibraryDecision != "" && segment.Voice != nil {
			if a, e := s.libraryTrackedAudio(ctx, tenant, room, runtimeID, segment.Selected.ID); e != nil {
				return beforeMainlineSubmit(e)
			} else if a != nil {
				v := *segment.Voice
				v.Bundle = a.Bundle
				segment.Voice = &v
			}
		}
		track, err := liveCycleMainlineTrack(latest, segment)
		if err != nil {
			return beforeMainlineSubmit(err)
		}
		mu.Lock()
		program := delivery.ProgramID
		mu.Unlock()
		var snapshot coreclient.ProgramRefreshSnapshot
		if program == "" {
			s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
			snapshot, err = s.startLiveReadyProgram(ctx, tenant, room, version, fmt.Sprintf("live-runtime-%d", runtimeID), fmt.Sprintf("实时主线 V%d", version.VersionNo), track)
		} else {
			var generation uint64
			generation, err = s.appendRecordingStreamTrack(ctx, tenant, room, version, program, track)
			snapshot = coreclient.ProgramRefreshSnapshot{ProgramID: program, VersionID: version.ID, Generation: generation, Sequential: true}
		}
		if err != nil {
			return errordetail.WithStage("Core音频交付", err)
		}
		archiveCtx, archiveCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_ = s.store.SaveSessionSpeechAsset(archiveCtx, model.SessionSpeechAsset{TenantID: tenant, RoomID: room, SessionID: runtimeID,
			SourceRef: model.SessionMainlineRef(snapshot.ProgramID, track.ID), Kind: "mainline", Text: track.Text,
			AudioURL: track.AudioURL, DurationMS: track.DurationMS, ExpiresAt: model.SessionAudioExpiry(track.AudioURL)})
		archiveCancel()
		mu.Lock()
		delivery.ProgramID, delivery.Generation = snapshot.ProgramID, snapshot.Generation
		delivery.DeliveredCandidates = append(delivery.DeliveredCandidates, segment.Selected.ID)
		if len(delivery.DeliveredCandidates) > 256 {
			delivery.DeliveredCandidates = delivery.DeliveredCandidates[len(delivery.DeliveredCandidates)-256:]
		}
		mu.Unlock()
		if program == "" {
			s.reportLiveStartup(ctx, tenant, room, runtimeID, "waiting_playback")
		}
		return nil
	}
	supply.start()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-supply.done:
		supply.mu.Lock()
		message := supply.failure
		cause := supply.failureCause
		supply.mu.Unlock()
		if cause != nil {
			return cause
		}
		if message != "" {
			return errors.New(message)
		}
		return context.Canceled
	}
}

// The delivery decision is unchanged: an unconfirmed response never permits a
// resend. Keep the Core status/reason instead of reducing all failures to one
// generic "audio stopped" string.
func decodeLiveHandoffResponse(resp *http.Response, versionID int64) (coreclient.ProgramRefreshSnapshot, error) {
	var snapshot coreclient.ProgramRefreshSnapshot
	var raw json.RawMessage
	err := json.NewDecoder(resp.Body).Decode(&raw)
	if err == nil {
		err = json.Unmarshal(raw, &snapshot)
	}
	if err == nil && resp.StatusCode == http.StatusOK && snapshot.Sequential && snapshot.VersionID == versionID {
		return snapshot, nil
	}
	var body struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	reason := body.Error
	if reason == "" && err != nil {
		reason = err.Error()
	}
	if reason == "" {
		reason = fmt.Sprintf("响应版本=%d，预期版本=%d，顺序播放=%t", snapshot.VersionID, versionID, snapshot.Sequential)
	}
	return snapshot, &errordetail.Fault{Detail: errordetail.Detail{
		Code: "core_handoff_unconfirmed", Stage: "Core音频交付", HTTPStatus: resp.StatusCode,
		RequestID: errordetail.Safe(resp.Header.Get("X-Request-ID")),
		Message:   "完整声音追加未确认：" + errordetail.Safe(reason),
		Action:    "已暂停，不盲目重复提交；按场次和Core回执核查本次交付。",
	}, Cause: err}
}
