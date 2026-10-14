package model

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// Download archives are immutable output history, never resumable live state.
type SessionSpeechAsset struct {
	ID               int64      `json:"id"`
	TenantID         int64      `json:"-"`
	RoomID           int64      `json:"-"`
	SessionID        int64      `json:"session_id"`
	SourceRef        string     `json:"-"`
	Kind             string     `json:"kind"`
	Text             string     `json:"text"`
	AudioURL         string     `json:"-"`
	DurationMS       int        `json:"duration_ms"`
	PlayedAt         *time.Time `json:"played_at"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Available        bool       `json:"available"`
	CreatedAt        time.Time  `json:"-"`
	ArchiveObjectKey string     `json:"-"`
	ArchiveBucket    string     `json:"-"`
	ArchiveReady     bool       `json:"-"`
}

const SessionDownloadRetention = 24 * time.Hour

type SessionSpeechArchiveWriter interface {
	SaveSessionSpeechAsset(context.Context, SessionSpeechAsset) error
	MarkSessionSpeechPlayed(context.Context, int64, int64, int64, string, time.Time) error
}

func SessionMainlineRef(program, track string) string {
	return fmt.Sprintf("mainline:%x", sha256.Sum256([]byte(program+"\x00"+track)))
}

func SessionAudioExpiry(address string) *time.Time {
	u, err := url.Parse(address)
	if err != nil {
		return nil
	}
	v, err := strconv.ParseInt(u.Query().Get("Expires"), 10, 64)
	if err != nil || v <= 0 {
		return nil
	}
	at := time.Unix(v, 0).UTC()
	return &at
}
