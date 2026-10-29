package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

// Only complete, previously accepted tracks from the current version enter
// this short ring. The hook never starts/stops a program or generates speech.
type ttsWaitContinuity struct {
	mu       sync.Mutex
	tracks   []coreclient.ProgramRefreshTrack
	next     int
	pending  *coreclient.ProgramRefreshTrack
	snapshot func(context.Context) (coreclient.ProgramRefreshSnapshot, error)
	append   func(context.Context, coreclient.ProgramRefreshTrack) error
	accepted func(context.Context, coreclient.ProgramRefreshTrack)
}

func (c *ttsWaitContinuity) remember(track coreclient.ProgramRefreshTrack) {
	if track.AudioURL == "" || track.DurationMS <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tracks = append(c.tracks, track)
	if len(c.tracks) > 4 {
		c.tracks = append([]coreclient.ProgramRefreshTrack(nil), c.tracks[len(c.tracks)-4:]...)
	}
}
func (c *ttsWaitContinuity) wait(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil || c.snapshot == nil || c.append == nil || len(c.tracks) == 0 {
		return
	}
	snapshot, err := c.snapshot(ctx)
	if err != nil || !snapshot.Running || snapshot.Suspended || (c.pending == nil && snapshot.BufferedMainlineMS > 15000) {
		return
	}
	if c.pending == nil {
		for tried := 0; tried < len(c.tracks); tried++ {
			track := c.tracks[c.next%len(c.tracks)]
			c.next++
			// Never send an expired signed URL to Core; resume normal supply instead.
			if expiry := model.SessionAudioExpiry(track.AudioURL); expiry != nil && expiry.Before(time.Now().Add(time.Duration(track.DurationMS)*time.Millisecond+30*time.Second)) {
				continue
			}
			var raw [16]byte
			if _, err = rand.Read(raw[:]); err != nil {
				return
			}
			track.ID = "tts-cache-" + hex.EncodeToString(raw[:])
			track.AllowConsecutiveRepeat = true
			c.pending = &track
			break
		}
	}
	if c.pending == nil || ctx.Err() != nil {
		return
	}
	// An unknown append result is retried with exactly the same ID. Core's
	// idempotent append avoids buying or playing a duplicate due to a timeout.
	if err = c.append(ctx, *c.pending); err == nil {
		if c.accepted != nil {
			c.accepted(ctx, *c.pending)
		}
		c.pending = nil
	}
}

func (s *Server) archiveTTSCacheReplay(ctx context.Context, tenant, room, session int64, program string, track coreclient.ProgramRefreshTrack) {
	if s.store == nil {
		return
	}
	archiveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_ = s.store.SaveSessionSpeechAsset(archiveCtx, model.SessionSpeechAsset{TenantID: tenant, RoomID: room, SessionID: session,
		SourceRef: model.SessionMainlineRef(program, track.ID), Kind: "mainline", Text: track.Text, AudioURL: track.AudioURL, DurationMS: track.DurationMS, ExpiresAt: model.SessionAudioExpiry(track.AudioURL)})
}
