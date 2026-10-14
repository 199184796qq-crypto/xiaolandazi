package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
	"net/http"
	"os"
	"testing"
	"time"
)

type downloadArchiveFixture struct {
	data      []byte
	key       string
	reserved  bool
	completed bool
	expiry    time.Time
	ttl       time.Duration
	private   bool
}

// Explicit opt-in. Only creates synthetic private audio objects, then deletes
// both; it never generates speech or writes a business/session database row.
func TestSessionDownloadPrivateOSSIntegration(t *testing.T) {
	raw := os.Getenv("SESSION_DOWNLOAD_OSS_TEST_CONFIG")
	if raw == "" {
		t.Skip("requires explicit private OSS fixture test configuration")
	}
	var cfg storage.Config
	if json.Unmarshal([]byte(raw), &cfg) != nil {
		t.Fatal("invalid OSS test configuration")
	}
	store, err := storage.NewOSS(cfg)
	if err != nil {
		t.Fatal("OSS fixture setup failed")
	}
	at := time.Now().UTC()
	a := model.SessionSpeechAsset{ID: at.UnixNano() / 1000, TenantID: 14, RoomID: 26, SessionID: 9000000000001, PlayedAt: &at}
	key := sessionArchiveObjectKey(a)
	source := key + ".fixture-source"
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if store.Delete(cleanup, source) != nil || store.Delete(cleanup, key) != nil {
			t.Error("private fixture cleanup failed")
		}
	}()
	payload := []byte("RIFF synthetic download verification bytes")
	if store.PutPrivate(ctx, source, bytes.NewReader(payload), "audio/wav") != nil {
		t.Fatal("private fixture source upload failed")
	}
	a.AudioURL, err = store.InternalSignedURL(ctx, source, 2*time.Minute)
	if err != nil {
		t.Fatal("private source signing failed")
	}
	writer := &downloadArchiveFixture{}
	if preserveSessionDownloadAsset(ctx, a, store, writer, sessionAudioClient()) != nil {
		t.Fatal("actual private archive copy failed")
	}
	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal("actual archive read failed")
	}
	got, e := io.ReadAll(reader)
	reader.Close()
	if e != nil || !bytes.Equal(got, payload) || !writer.completed || !writer.expiry.Equal(at.Add(24*time.Hour)) {
		t.Fatal("actual copied bytes/retention differ")
	}
	if store.Delete(ctx, key) != nil {
		t.Fatal("actual archive deletion failed")
	}
	if reader, e = store.Open(ctx, key); e == nil {
		reader.Close()
		t.Fatal("deleted fixture still readable")
	}
	t.Log("private OSS copy, readback, 24-hour expiry and deletion verified; no business records or TTS calls")
}

func (s *downloadArchiveFixture) Driver() string { return "oss" }
func (s *downloadArchiveFixture) Bucket() string { return "fixture-bucket" }
func (s *downloadArchiveFixture) Put(context.Context, string, io.Reader, string) error {
	return fmt.Errorf("public upload forbidden")
}
func (s *downloadArchiveFixture) PutPrivate(ctx context.Context, key string, r io.Reader, _ string) error {
	if !s.reserved {
		return fmt.Errorf("unreserved object")
	}
	s.private = true
	s.key = key
	var e error
	s.data, e = io.ReadAll(r)
	return e
}
func (s *downloadArchiveFixture) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.data)), nil
}
func (s *downloadArchiveFixture) Delete(context.Context, string) error { s.data = nil; return nil }
func (s *downloadArchiveFixture) SignedURL(_ context.Context, key string, ttl time.Duration) (string, error) {
	s.ttl = ttl
	return "https://fixture-bucket.aliyuncs.com/" + key, nil
}
func (s *downloadArchiveFixture) ReserveSessionDownloadAudio(context.Context, model.SessionSpeechAsset, string, string) error {
	s.reserved = true
	return nil
}
func (s *downloadArchiveFixture) CompleteSessionDownloadAudio(_ context.Context, _ model.SessionSpeechAsset, key, address string, expiry time.Time) error {
	s.completed = true
	s.expiry = expiry
	return nil
}

type archiveTransport func(*http.Request) (*http.Response, error)

func (f archiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSessionDownloadAudioCopyPreserves24HoursAndOwnership(t *testing.T) {
	at := time.Now().Add(-2 * time.Hour)
	a := model.SessionSpeechAsset{ID: 9, TenantID: 1, RoomID: 2, SessionID: 3, AudioURL: "https://source-bucket.aliyuncs.com/a.wav", PlayedAt: &at}
	s := &downloadArchiveFixture{}
	client := &http.Client{Transport: archiveTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader([]byte("original wave bytes"))), Header: make(http.Header)}, nil
	})}
	if err := preserveSessionDownloadAsset(context.Background(), a, s, s, client); err != nil {
		t.Fatal(err)
	}
	if !s.private || !s.completed || string(s.data) != "original wave bytes" || s.key != "live-downloads/tenant-1/room-2/session-3/audio-9.wav" || !s.expiry.Equal(at.Add(24*time.Hour)) || s.ttl < 21*time.Hour || s.ttl > 22*time.Hour {
		t.Fatal("archive ownership/bytes/retention incorrect", s)
	}
	for _, expired := range []bool{true, false} {
		copy := a
		f := &downloadArchiveFixture{}
		if expired {
			old := time.Now().Add(-25 * time.Hour)
			copy.PlayedAt = &old
		} else {
			copy.PlayedAt = nil
		}
		if e := preserveSessionDownloadAsset(context.Background(), copy, f, f, client); e != nil || f.reserved || f.completed {
			t.Fatal("unplayed or expired audio copied", e)
		}
	}
	bad := a
	bad.ArchiveObjectKey = "live-downloads/tenant-8/room-2/session-3/audio-9.wav"
	bad.ArchiveBucket = s.Bucket()
	if e := preserveSessionDownloadAsset(context.Background(), bad, s, s, client); e == nil {
		t.Fatal("cross-tenant archive key accepted")
	}
}
