package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"log"
	"net/http"
	"net/url"
	"time"
)

func (s *Server) watchSessionArchive(session model.LiveRuntimeSession) {
	if s.anchorProducerContext == nil || !session.BelongsToExecutionRealm(s.executionRealm) {
		return
	}
	if _, loaded := s.sessionArchiveWatches.LoadOrStore(session.ID, true); loaded {
		return
	}
	go func() {
		defer s.sessionArchiveWatches.Delete(session.ID)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(s.anchorProducerContext, 3*time.Second)
			s.syncSessionArchive(ctx, session)
			current, err := s.store.GetLiveRuntimeByRoom(ctx, session.TenantID, session.RoomID)
			cancel()
			if err != nil || current.ID != session.ID || (current.Status != "running" && current.Status != "paused") {
				return
			}
			select {
			case <-s.anchorProducerContext.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Server) syncSessionArchive(ctx context.Context, session model.LiveRuntimeSession) {
	p, err := s.core.GetRoomProgramSnapshot(ctx, session.TenantID, session.RoomID)
	if err == nil && p.Task.SessionID == fmt.Sprintf("live-runtime-%d", session.ID) {
		s.observeLibraryPlayback(ctx, session.TenantID, session.RoomID, session.ID, p.MainlinePlaybackReceipts, p.PlaybackReceipts)
		for _, track := range p.FutureWindow {
			if track.ID == "" || track.Text == "" {
				continue
			}
			address := ""
			if !p.Sequential && track.ID == p.TrackID {
				address = p.Task.AudioURL
			}
			if err := s.store.SaveSessionSpeechAsset(ctx, model.SessionSpeechAsset{TenantID: session.TenantID, RoomID: session.RoomID, SessionID: session.ID,
				SourceRef: model.SessionMainlineRef(p.ProgramID, track.ID), Kind: "mainline", Text: track.Text, AudioURL: address,
				DurationMS: track.DurationMS, ExpiresAt: model.SessionAudioExpiry(address)}); err != nil {
				log.Printf("session archive room=%d: %v", session.RoomID, err)
			}
		}
		// Backfill only tracks with an actual Core receipt, never all generated
		// candidates. This also supports a live session opened before deployment.
		for _, receipt := range p.MainlinePlaybackReceipts {
			at, e := time.Parse(time.RFC3339Nano, receipt.StartedAt)
			if e != nil {
				continue
			}
			ref := model.SessionMainlineRef(p.ProgramID, receipt.TrackID)
			if s.store.HasSessionSpeechAudio(ctx, session.TenantID, session.RoomID, session.ID, ref) {
				continue
			}
			if a, e := s.libraryTrackedAudio(ctx, session.TenantID, session.RoomID, session.ID, receipt.TrackID); e == nil && a != nil {
				b := a.Bundle
				_ = s.store.SaveSessionSpeechAsset(ctx, model.SessionSpeechAsset{TenantID: session.TenantID, RoomID: session.RoomID, SessionID: session.ID, SourceRef: ref, Kind: "mainline", Text: b.Text, AudioURL: b.AudioURL, DurationMS: int(b.AudioDurationMS), PlayedAt: &at, ExpiresAt: model.SessionAudioExpiry(b.AudioURL)})
				continue
			}
			if reader, ok := s.anchorVoiceCache.(interface {
				FindCandidate(context.Context, int64, string) (model.LiveAgentPlanVersionVariant, bool)
			}); ok {
				if bundle, found := reader.FindCandidate(ctx, session.TenantID, receipt.TrackID); found {
					_ = s.store.SaveSessionSpeechAsset(ctx, model.SessionSpeechAsset{TenantID: session.TenantID, RoomID: session.RoomID, SessionID: session.ID,
						SourceRef: ref, Kind: "mainline", Text: bundle.Text, AudioURL: bundle.AudioURL, DurationMS: int(bundle.AudioDurationMS), PlayedAt: &at, ExpiresAt: model.SessionAudioExpiry(bundle.AudioURL)})
				}
			}
		}
		s.archiveMainlineReceipts(ctx, session.TenantID, session.RoomID, session.ID, p)
		if !p.Sequential {
			s.syncRecordingArchive(ctx, session, p)
		}
	}
	s.syncSessionInterruptArchive(ctx, session)
}

func (s *Server) syncRecordingArchive(ctx context.Context, session model.LiveRuntimeSession, p coreclient.ProgramRefreshSnapshot) {
	if p.TrackID == "" || p.TrackText == "" || p.Task.StartedAt == nil {
		return
	}
	// The task can be queued before playback. Confirm consumed PCM frames
	// and the current track's segment instead of using wall-clock progress.
	resp, err := s.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/audio-engine", session.RoomID), url.Values{"tenant_id": {fmt.Sprint(session.TenantID)}}, nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var engine struct {
		ActiveSource     string `json:"active_source"`
		CursorMS         int    `json:"mainline_cursor_ms"`
		CurrentSegmentID string `json:"current_segment_id"`
	}
	if json.NewDecoder(resp.Body).Decode(&engine) != nil || engine.ActiveSource != "mainline" || engine.CursorMS <= 0 {
		return
	}
	matched := false
	for _, segment := range p.Timeline {
		if segment.SegmentID == engine.CurrentSegmentID {
			matched = true
			break
		}
	}
	if !matched {
		return
	}
	ref := model.SessionMainlineRef(p.ProgramID, p.TrackID)
	if err = s.store.SaveSessionSpeechAsset(ctx, model.SessionSpeechAsset{TenantID: session.TenantID, RoomID: session.RoomID, SessionID: session.ID, SourceRef: ref, Kind: "mainline", Text: p.TrackText, AudioURL: p.Task.AudioURL, DurationMS: p.Task.DurationMS, ExpiresAt: model.SessionAudioExpiry(p.Task.AudioURL)}); err == nil {
		_ = s.store.RecordSessionRecordingPlayback(ctx, session.TenantID, session.RoomID, session.ID, ref, *p.Task.StartedAt)
	}
}

func (s *Server) syncSessionInterruptArchive(ctx context.Context, session model.LiveRuntimeSession) {
	resp, err := s.core.DoRoom(ctx, session.TenantID, session.RoomID, http.MethodGet, fmt.Sprintf("/internal/v1/rooms/%d/speech-runtime", session.RoomID), url.Values{"tenant_id": {fmt.Sprint(session.TenantID)}}, nil)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	// Explicit wire tags are required for the snake_case Core payload.
	var wire struct {
		Interrupt struct {
			Status     string     `json:"status"`
			DecisionID string     `json:"decision_id"`
			ReplyText  string     `json:"reply_text"`
			AudioURL   string     `json:"audio_url"`
			StartedAt  *time.Time `json:"started_at"`
			UpdatedAt  *time.Time `json:"updated_at"`
			DurationMS int        `json:"duration_ms"`
			BridgeUsed bool       `json:"bridge_used"`
		} `json:"interrupt"`
	}
	if json.NewDecoder(resp.Body).Decode(&wire) != nil {
		return
	}
	v := wire.Interrupt
	if v.StartedAt == nil || v.DecisionID == "" || (v.Status != "playing" && v.Status != "returning" && v.Status != "completed") {
		return
	}
	// A current room snapshot may still contain the previous session's answer.
	if !s.store.SessionHasGeneratedDecision(ctx, session.TenantID, session.RoomID, session.ID, v.DecisionID) {
		return
	}
	_ = s.store.SaveSessionSpeechAsset(ctx, model.SessionSpeechAsset{TenantID: session.TenantID, RoomID: session.RoomID, SessionID: session.ID,
		SourceRef: "reply:" + v.DecisionID, Kind: "interaction", Text: v.ReplyText, AudioURL: v.AudioURL, DurationMS: v.DurationMS, PlayedAt: v.StartedAt, ExpiresAt: model.SessionAudioExpiry(v.AudioURL)})
	if v.BridgeUsed && v.UpdatedAt != nil {
		_ = s.store.MarkSessionSpeechPlayed(ctx, session.TenantID, session.RoomID, session.ID, "bridge:"+v.DecisionID, *v.UpdatedAt)
	}
}

// Observe output history after process startup without restarting any live work.
func (s *Server) RunSessionArchiveWatch(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		sessions, err := s.store.ListRunningLiveRuntimeSessions(ctx)
		if err == nil {
			for _, session := range sessions {
				s.watchSessionArchive(session)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
