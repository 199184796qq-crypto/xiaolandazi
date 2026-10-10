package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"net/http"
	"net/url"
	"sync"
)

func (s *Server) runLiveAnchorProducer(ctx context.Context, tenant, room, runtimeID int64, version model.LiveAgentPlanVersion) error {
	state, err := s.createLiveAnchorRun(ctx, tenant, room, runtimeID, version)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	delivery := *state.Delivery
	var actualReceipts []anchorcycle.AnglePlaybackReceipt
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
		track, err := liveCycleMainlineTrack(latest, segment)
		if err != nil {
			return err
		}
		mu.Lock()
		program := delivery.ProgramID
		mu.Unlock()
		query := url.Values{"tenant_id": {fmt.Sprint(tenant)}}
		path := fmt.Sprintf("/internal/v1/rooms/%d/audio/program/append", room)
		var input any = map[string]any{"expected_program_id": program, "expected_version_id": version.ID, "tracks": []coreclient.ProgramRefreshTrack{track}}
		if program == "" {
			s.reportLiveStartup(ctx, tenant, room, runtimeID, "sending_audio")
			path = fmt.Sprintf("/internal/v1/rooms/%d/audio/program/start", room)
			input = map[string]any{"session_id": fmt.Sprintf("live-runtime-%d", runtimeID), "label": fmt.Sprintf("实时主线 V%d", version.VersionNo), "version_id": version.ID, "version_no": version.VersionNo, "sequential": true, "tracks": []coreclient.ProgramRefreshTrack{track}}
		}
		resp, err := s.core.DoRoom(ctx, tenant, room, http.MethodPost, path, query, input)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var snapshot coreclient.ProgramRefreshSnapshot
		if err = json.NewDecoder(resp.Body).Decode(&snapshot); err != nil || resp.StatusCode != 200 || !snapshot.Sequential || snapshot.VersionID != version.ID {
			return errors.New("完整声音追加未确认，已暂停，不盲目重复提交")
		}
		if program != "" && snapshot.ProgramID != program {
			return anchorcycle.ErrStale
		}
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
		supply.mu.Unlock()
		if message != "" {
			return errors.New(message)
		}
		return context.Canceled
	}
}
