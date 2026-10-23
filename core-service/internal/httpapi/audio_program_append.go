package httpapi

import (
	"context"
	"livecompanion/core/internal/audioout"
	"net/http"
	"time"
)

// 追加回执里带整条节目快照，播得越久越大。这里只回最近一小时的回执和
// 有限长度的队列，调用方需要的 generation/缓冲量等标量不受影响。
const (
	appendReceiptWindow    = time.Hour
	appendCoverageLimit    = 200
	appendPlaybackLimit    = 500
	appendFutureTrackLimit = 100
)

func trimAppendSnapshot(snapshot audioout.RoomProgramSnapshot, now time.Time) audioout.RoomProgramSnapshot {
	if n := len(snapshot.PlaybackReceipts); n > appendCoverageLimit {
		snapshot.PlaybackReceipts = snapshot.PlaybackReceipts[n-appendCoverageLimit:]
	}
	if n := len(snapshot.MainlinePlaybackReceipts); n > 0 {
		cutoff := now.Add(-appendReceiptWindow)
		kept := snapshot.MainlinePlaybackReceipts[:0]
		for _, receipt := range snapshot.MainlinePlaybackReceipts {
			if !receipt.StartedAt.Before(cutoff) {
				kept = append(kept, receipt)
			}
		}
		if len(kept) > appendPlaybackLimit {
			kept = kept[len(kept)-appendPlaybackLimit:]
		}
		snapshot.MainlinePlaybackReceipts = kept
	}
	if n := len(snapshot.FutureWindow); n > appendFutureTrackLimit {
		snapshot.FutureWindow = snapshot.FutureWindow[n-appendFutureTrackLimit:]
	}
	return snapshot
}

type audioProgramAppendClient interface {
	AppendProgram(context.Context, audioout.AppendProgramInput) (audioout.RoomProgramSnapshot, error)
}

func (s *Server) appendRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	defer recoverRoomAudioDelivery(w)
	room, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenant, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenant, room); err != nil {
		writeError(w, 404, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, 503, "播音分发层尚未配置")
		return
	}
	client, ok := state.client.(audioProgramAppendClient)
	if !ok {
		writeError(w, 503, "播音分发层不支持逐段追加")
		return
	}
	var input audioout.AppendProgramInput
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "invalid ready unit")
		return
	}
	input.RoomID = room
	result, err := client.AppendProgram(r.Context(), input)
	if err != nil {
		writeAudioAppendFailure(w, err)
		return
	}
	writeJSON(w, 200, trimAppendSnapshot(result, time.Now()))
}
