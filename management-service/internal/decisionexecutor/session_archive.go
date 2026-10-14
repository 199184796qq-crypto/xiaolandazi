package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
	"log"
	"time"
)

func (w *Worker) archiveSessionAudio(ctx context.Context, session model.LiveRuntimeSession, ref, kind, text, address string, duration int) {
	writer, ok := w.store.(model.SessionSpeechArchiveWriter)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	err := writer.SaveSessionSpeechAsset(ctx, model.SessionSpeechAsset{TenantID: session.TenantID, RoomID: session.RoomID, SessionID: session.ID,
		SourceRef: ref, Kind: kind, Text: text, AudioURL: address, DurationMS: duration, ExpiresAt: model.SessionAudioExpiry(address)})
	if err != nil {
		log.Printf("session audio history room=%d: %v", session.RoomID, err)
	}
}

func (w *Worker) archiveSessionPlayback(ctx context.Context, session model.LiveRuntimeSession, snapshot coreSpeechRuntimeSnapshot) {
	writer, ok := w.store.(model.SessionSpeechArchiveWriter)
	if !ok {
		return
	}
	p := snapshot.Interrupt
	if p.StartedAt == nil || p.DecisionID == "" {
		return
	}
	if p.Status != "playing" && p.Status != "returning" && p.Status != "completed" {
		return
	}
	_ = writer.MarkSessionSpeechPlayed(ctx, session.TenantID, session.RoomID, session.ID, "reply:"+p.DecisionID, *p.StartedAt)
	if p.BridgeUsed {
		_ = writer.MarkSessionSpeechPlayed(ctx, session.TenantID, session.RoomID, session.ID, "bridge:"+p.DecisionID, *p.StartedAt)
	}
}
