package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
	"log"
	"net/http"
	"time"
)

func sessionArchiveObjectKey(a model.SessionSpeechAsset) string {
	return fmt.Sprintf("live-downloads/tenant-%d/room-%d/session-%d/audio-%d.wav", a.TenantID, a.RoomID, a.SessionID, a.ID)
}

// These private copies are download archives, never generation/reuse inputs.
func (s *Server) preserveSessionDownloadAudio(ctx context.Context, a model.SessionSpeechAsset) error {
	store, err := s.cycleCacheOSS()
	if err != nil {
		return err
	}
	return preserveSessionDownloadAsset(ctx, a, store, s.store, sessionAudioClient())
}

type sessionDownloadArchiveWriter interface {
	ReserveSessionDownloadAudio(context.Context, model.SessionSpeechAsset, string, string) error
	CompleteSessionDownloadAudio(context.Context, model.SessionSpeechAsset, string, string, time.Time) error
}

func preserveSessionDownloadAsset(ctx context.Context, a model.SessionSpeechAsset, store storage.Store, writer sessionDownloadArchiveWriter, client *http.Client) error {
	if a.PlayedAt == nil || !a.PlayedAt.Add(model.SessionDownloadRetention).After(time.Now()) {
		return nil
	}
	if a.ID <= 0 || a.TenantID <= 0 || a.RoomID <= 0 || a.SessionID <= 0 {
		return fmt.Errorf("invalid archive scope")
	}
	key := sessionArchiveObjectKey(a)
	if a.ArchiveObjectKey != "" && (a.ArchiveObjectKey != key || a.ArchiveBucket != store.Bucket()) {
		return fmt.Errorf("archive storage scope mismatch")
	}
	if err := writer.ReserveSessionDownloadAudio(ctx, a, key, store.Bucket()); err != nil {
		return err
	}
	address, allowed := sessionAudioDownloadURL(a.AudioURL)
	if !allowed {
		return fmt.Errorf("invalid source audio URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("source audio http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (128<<20)+1))
	if err != nil {
		return err
	}
	if len(data) > 128<<20 {
		return fmt.Errorf("source audio too large")
	}
	if private, ok := store.(interface {
		PutPrivate(context.Context, string, io.Reader, string) error
	}); ok {
		err = private.PutPrivate(ctx, key, bytes.NewReader(data), "audio/wav")
	} else {
		return fmt.Errorf("private archive storage unavailable")
	}
	if err != nil {
		return err
	}
	until := a.PlayedAt.Add(model.SessionDownloadRetention)
	ttl := time.Until(until)
	if ttl <= 0 {
		return store.Delete(ctx, key)
	}
	address = ""
	if internal, ok := store.(interface {
		InternalSignedURL(context.Context, string, time.Duration) (string, error)
	}); ok {
		address, err = internal.InternalSignedURL(ctx, key, ttl)
	} else {
		address, err = store.SignedURL(ctx, key, ttl)
	}
	if err != nil {
		return err
	}
	return writer.CompleteSessionDownloadAudio(ctx, a, key, address, until)
}

func (s *Server) purgeSessionDownloads(ctx context.Context) error {
	if err := s.store.PurgeExpiredGeneratedSpeechHistory(ctx); err != nil {
		return err
	}
	items, err := s.store.SessionDownloadMaintenanceAssets(ctx, true, 1000)
	if err != nil {
		return err
	}
	for _, a := range items {
		if a.ArchiveObjectKey != "" {
			store, e := s.cycleCacheOSS()
			if e != nil {
				return e
			}
			if a.ArchiveBucket != store.Bucket() || a.ArchiveObjectKey != sessionArchiveObjectKey(a) {
				return fmt.Errorf("expired archive storage scope mismatch")
			}
			if e = store.Delete(ctx, a.ArchiveObjectKey); e != nil {
				return e
			}
		}
		if err = s.store.DeleteExpiredSessionDownloadAsset(ctx, a); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) RunSessionDownloadMaintenance(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if s.leader == nil || s.leader.IsLeader() {
			work, cancel := context.WithTimeout(ctx, 55*time.Second)
			if err := s.purgeSessionDownloads(work); err != nil {
				log.Printf("session download retention: %v", err)
			}
			items, err := s.store.SessionDownloadMaintenanceAssets(work, false, 5)
			if err == nil {
				for _, a := range items {
					if err = s.preserveSessionDownloadAudio(work, a); err != nil {
						log.Printf("session download archive room=%d asset=%d: %v", a.RoomID, a.ID, err)
						retry, stop := context.WithTimeout(context.WithoutCancel(work), 2*time.Second)
						_ = s.store.DelaySessionDownloadAudio(retry, a)
						stop()
					}
				}
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
