package httpapi

import (
	"context"
	"fmt"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

const (
	scriptFirstSilenceBlockMS  = 2000
	scriptFirstSilenceMaxBlock = 10
)

func scriptFirstSilenceWAV() []byte {
	return scriptFirstPCMToWAV(make([]int16, 24000*scriptFirstSilenceBlockMS/1000))
}

// Keep the remaining audio; only pad/replay after the previous block drained.
func scriptFirstSilenceDue(p coreclient.ProgramRefreshSnapshot, blocks int) (pad, fallback bool) {
	if !p.WaitingForContent || p.BufferedMainlineMS > 0 {
		return false, false
	}
	return blocks < scriptFirstSilenceMaxBlock, blocks >= scriptFirstSilenceMaxBlock
}

func (s *Server) ensureScriptFirstSilence(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, cursor *volcengineScriptCursor) (int64, error) {
	if cursor.SilenceAssetID > 0 {
		if _, err := s.recordingStreamAssetURL(ctx, tenant, cursor.SilenceAssetID); err == nil {
			return cursor.SilenceAssetID, nil
		}
		cursor.SilenceAssetID = 0
	}
	key := fmt.Sprintf("silence-%dms-%d", scriptFirstSilenceBlockMS, v.PlanID)
	id, err := s.archiveUntimedScriptFirstAudio(ctx, tenant, room, v, key, "", "", scriptFirstSilenceWAV(), scriptFirstSilenceBlockMS)
	if err != nil {
		return 0, err
	}
	cursor.SilenceAssetID = id
	return id, nil
}

// Persist the reusable asset and entire append request before delivery. Core
// hashes the whole body, including its URL, for lost-ack retry idempotency.
func deliverScriptFirstSilence(ctx context.Context, session int64, cursor *volcengineScriptCursor, url string, save func() error, deliver func(context.Context, coreclient.ProgramRefreshTrack) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if cursor.PendingSilence == nil {
		cursor.Sequence++
		cursor.PendingSilence = &coreclient.ProgramRefreshTrack{
			ID:    fmt.Sprintf("silence-%d-%d", session, cursor.Sequence),
			Label: "静音垫片", AudioURL: url, DurationMS: scriptFirstSilenceBlockMS,
			AllowConsecutiveRepeat: true, UntimedTranscript: true,
		}
		if err := save(); err != nil {
			return err
		}
	}
	if err := deliver(ctx, *cursor.PendingSilence); err != nil {
		return err
	}
	cursor.SilenceBlocks++
	cursor.PendingSilence = nil
	return save()
}

func (s *Server) queueSilence(ctx context.Context, tenant, room, session int64, v model.LiveAgentPlanVersion, cursor *volcengineScriptCursor, save func() error) error {
	url := ""
	if cursor.PendingSilence == nil {
		assetID, err := s.ensureScriptFirstSilence(ctx, tenant, room, v, cursor)
		if err != nil {
			return err
		}
		url, err = s.recordingStreamAssetURL(ctx, tenant, assetID)
		if err != nil {
			return err
		}
	}
	return deliverScriptFirstSilence(ctx, session, cursor, url, save, func(c context.Context, track coreclient.ProgramRefreshTrack) error {
		_, err := s.appendRecordingStreamTrack(c, tenant, room, v, cursor.ProgramID, track)
		return err
	})
}
