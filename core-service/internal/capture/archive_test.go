package capture

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"livecompanion/core/internal/model"
)

type fakeArchiveStore struct {
	mu                     sync.Mutex
	uploads, deletes       []string
	ttl                    time.Duration
	failUpload, failDelete bool
	started                chan struct{}
	resume                 chan struct{}
	active, maximum        int
}

func (s *fakeArchiveStore) Upload(ctx context.Context, key string, r io.Reader, size int64, _ time.Time) error {
	s.mu.Lock()
	s.active++
	if s.active > s.maximum {
		s.maximum = s.active
	}
	s.uploads = append(s.uploads, key)
	fail := s.failUpload
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active--; s.mu.Unlock() }()
	if s.started != nil {
		s.started <- struct{}{}
		select {
		case <-s.resume:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return errors.New("partial upload")
	}
	if fail {
		return errors.New("do not expose upstream credential error")
	}
	return nil
}
func (s *fakeArchiveStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, key)
	if s.failDelete {
		return errors.New("delete unavailable")
	}
	return nil
}
func (s *fakeArchiveStore) DownloadURL(_ context.Context, key, name string, ttl time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = ttl
	return "https://recording.example.test/" + key + "?test-signature=local", nil
}

func archiveFixture(t *testing.T) (*Manager, *fakeArchiveStore, *time.Time) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	m := &Manager{root: t.TempDir(), now: func() time.Time { return now }, rooms: make(map[int64]*roomState)}
	s := &fakeArchiveStore{}
	if err := m.ConfigureArchives(s, []model.Room{{ID: 33, TenantID: 14}}); err != nil {
		t.Fatal(err)
	}
	return m, s, &now
}
func addReadyArchive(t *testing.T, m *Manager, started time.Time) *recording {
	t.Helper()
	id := started.Format("20060102-150405") + "-room-33"
	dir := filepath.Join(m.root, "room-33", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	wav := testWAVBytes([]byte{1, 2, 3, 4})
	if err := os.WriteFile(filepath.Join(dir, "recording.wav"), wav, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "segment_000001.flac"), []byte("segment"), 0o600)
	finished := started.Add(time.Minute)
	rec := &recording{tenantID: 14, dir: dir, status: RecordingStatus{ID: id, Status: RecordingReady, StartedAt: started, FinishedAt: finished, FinalFileName: "recording.wav", FinalBytes: int64(len(wav)), SegmentCount: 1, ArchiveStatus: ArchivePending, ExpiresAt: finished.Add(RecordingRetention)}}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stateLocked(33).recording = rec
	if err := m.persistRecordingLocked(33, rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func TestArchiveUploadRetryRestartAndExpiry(t *testing.T) {
	m, store, now := archiveFixture(t)
	rec := addReadyArchive(t, m, now.Add(-2*time.Minute))
	expiry := rec.status.ExpiresAt
	store.failUpload = true
	if !m.archiveOne(context.Background(), true) {
		t.Fatal("upload not queued")
	}
	if got := m.Snapshot(33).Recording; got.ArchiveStatus != ArchiveFailed || bytes.Contains([]byte(got.ArchiveError), []byte("credential")) {
		t.Fatalf("unsafe/incorrect error: %#v", got)
	}
	if _, err := os.Stat(filepath.Join(rec.dir, "recording.wav")); err != nil {
		t.Fatal("failed upload removed recording")
	}
	if m.archiveOne(context.Background(), true) {
		t.Fatal("failure hot loop instead of backoff")
	}
	store.failUpload = false
	*now = now.Add(time.Minute)
	if !m.archiveOne(context.Background(), true) {
		t.Fatal("retry missing")
	}
	if m.Snapshot(33).Recording.ArchiveStatus != ArchiveReady {
		t.Fatal("upload not ready")
	}
	if len(store.uploads) != 2 {
		t.Fatalf("uploads=%v", store.uploads)
	}
	if m.archiveOne(context.Background(), true) {
		t.Fatal("already uploaded twice")
	}
	for i := 0; i < 2; i++ {
		if _, err := m.RecordingDownload(context.Background(), 14, 33, "中文录音.wav"); err != nil {
			t.Fatal(err)
		}
	}
	if store.ttl != 15*time.Minute {
		t.Fatalf("ttl=%v", store.ttl)
	}
	if _, err := m.RecordingDownload(context.Background(), 14, 33, "x.wav", "stale-recording-id"); !errors.Is(err, ErrRecordingChanged) {
		t.Fatal("stale card downloaded a different recording")
	}
	if _, err := m.RecordingDownload(context.Background(), 15, 33, "x.wav"); !errors.Is(err, ErrRecordingNotReady) {
		t.Fatal("cross-tenant access allowed")
	}
	restarted := &Manager{root: m.root, now: m.now, rooms: make(map[int64]*roomState)}
	if err := restarted.ConfigureArchives(store, []model.Room{{ID: 33, TenantID: 14}}); err != nil {
		t.Fatal(err)
	}
	if got := restarted.Snapshot(33).Recording; got == nil || got.ArchiveStatus != ArchiveReady || !got.ExpiresAt.Equal(expiry) {
		t.Fatalf("restart renewed/lost archive: %#v", got)
	}
	*now = expiry.Add(-3500 * time.Millisecond)
	if _, err := restarted.RecordingDownload(context.Background(), 14, 33, "x.wav"); err != nil {
		t.Fatal(err)
	}
	if store.ttl != 3*time.Second {
		t.Fatalf("link exceeds retention: %v", store.ttl)
	}
	*now = expiry
	if _, err := restarted.RecordingDownload(context.Background(), 14, 33, "x.wav"); !errors.Is(err, ErrRecordingExpired) {
		t.Fatal("expired download allowed")
	}
	if _, _, err := restarted.RecordingFile(33); !errors.Is(err, ErrRecordingExpired) {
		t.Fatal("expired local file bypass")
	}
	store.failDelete = true
	if !restarted.archiveOne(context.Background(), false) {
		t.Fatal("cleanup not scheduled")
	}
	if _, err := os.Stat(filepath.Join(rec.dir, "recording.wav")); err != nil {
		t.Fatal("failed OSS deletion discarded retry evidence")
	}
	store.failDelete = false
	*now = now.Add(time.Minute)
	if !restarted.archiveOne(context.Background(), false) {
		t.Fatal("cleanup retry missing")
	}
	if _, err := os.Stat(filepath.Join(rec.dir, "recording.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local recording not cleaned")
	}
	if _, err := os.Stat(filepath.Join(rec.dir, archiveManifestName)); err != nil {
		t.Fatal("expiry tombstone lost")
	}
	if restarted.archiveOne(context.Background(), false) {
		t.Fatal("cleaned archive scheduled again")
	}
}

func TestArchiveOlderJobDoesNotOverwriteNewRecording(t *testing.T) {
	m, store, now := archiveFixture(t)
	first := addReadyArchive(t, m, now.Add(-5*time.Minute))
	second := addReadyArchive(t, m, now.Add(-2*time.Minute))
	if !m.archiveOne(context.Background(), true) {
		t.Fatal("no job")
	}
	if got := m.Snapshot(33).Recording; got.ID != second.status.ID || got.ArchiveStatus != ArchivePending {
		t.Fatalf("older completion replaced newest: %#v", got)
	}
	if !m.archiveOne(context.Background(), true) {
		t.Fatal("second job missing")
	}
	if len(store.uploads) != 2 || store.uploads[0] != archiveObjectKey(14, 33, first.status.ID) {
		t.Fatalf("wrong upload order: %v", store.uploads)
	}
	*now = first.status.ExpiresAt
	if !m.archiveOne(context.Background(), false) {
		t.Fatal("old cleanup missing")
	}
	if _, err := os.Stat(filepath.Join(second.dir, "recording.wav")); err != nil {
		t.Fatal("old cleanup removed current recording")
	}
}

func TestArchiveLegacyRecoveryDoesNotRenewOrDeleteOldFiles(t *testing.T) {
	m, store, now := archiveFixture(t)
	fresh := addReadyArchive(t, m, now.Add(-2*time.Minute))
	old := addReadyArchive(t, m, now.Add(-48*time.Hour))
	for _, rec := range []*recording{fresh, old} {
		_ = os.Remove(filepath.Join(rec.dir, archiveManifestName))
		finished := rec.status.FinishedAt
		if err := os.Chtimes(filepath.Join(rec.dir, "recording.wav"), finished, finished); err != nil {
			t.Fatal(err)
		}
	}
	restored := &Manager{root: m.root, now: m.now, rooms: make(map[int64]*roomState)}
	if err := restored.ConfigureArchives(store, []model.Room{{ID: 33, TenantID: 14}}); err != nil {
		t.Fatal(err)
	}
	if len(restored.archiveJobs) != 1 || !restored.Snapshot(33).Recording.ExpiresAt.Equal(fresh.status.ExpiresAt) {
		t.Fatal("legacy recovery changed retention")
	}
	if _, err := os.Stat(filepath.Join(old.dir, "recording.wav")); err != nil {
		t.Fatal("unowned old legacy file deleted")
	}
	otherTenant := &Manager{root: m.root, now: m.now, rooms: make(map[int64]*roomState)}
	if err := otherTenant.ConfigureArchives(store, []model.Room{{ID: 33, TenantID: 99}}); err != nil {
		t.Fatal(err)
	}
	if otherTenant.Snapshot(33).Recording != nil {
		t.Fatal("restored previous tenant's recording")
	}
}

func TestArchiveWorkersBoundedAndCancelWithoutLosingPending(t *testing.T) {
	m, store, now := archiveFixture(t)
	for i := 1; i <= 5; i++ {
		addReadyArchive(t, m, now.Add(-time.Duration(i)*2*time.Minute))
	}
	store.started = make(chan struct{}, 5)
	store.resume = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { m.RunArchives(ctx); close(done) }()
	for i := 0; i < 2; i++ {
		select {
		case <-store.started:
		case <-time.After(3 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	select {
	case <-store.started:
		t.Fatal("unbounded concurrent uploads")
	case <-time.After(30 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker ignored cancellation")
	}
	store.mu.Lock()
	maximum := store.maximum
	store.mu.Unlock()
	if maximum != 2 {
		t.Fatalf("concurrency=%d", maximum)
	}
	for _, job := range m.archiveJobs {
		if _, err := os.Stat(filepath.Join(job.dir, "recording.wav")); err != nil {
			t.Fatal("cancel removed local evidence")
		}
	}
}

func TestArchiveRealFFmpegFinalizationQueuesUpload(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}
	m, _, now := archiveFixture(t)
	rec := addReadyArchive(t, m, now.Add(-2*time.Minute))
	_ = os.Remove(filepath.Join(rec.dir, "recording.wav"))
	_ = os.Remove(filepath.Join(rec.dir, "segment_000001.flac"))
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=0.05", "-ac", "2", "-ar", "44100", "-c:a", "flac", filepath.Join(rec.dir, "segment_000001.flac"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate local test audio: %v %s", err, output)
	}
	m.ffmpegPath = ffmpeg
	rec.cmd = exec.Command(ffmpeg, "-version")
	if err := rec.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	rec.done = make(chan struct{})
	rec.processDone = make(chan struct{})
	rec.status.Status = RecordingRecording
	m.waitRecording(33, rec)
	if got := m.Snapshot(33).Recording; got.Status != RecordingReady || got.ArchiveStatus != ArchivePending || got.FinalBytes <= 44 {
		t.Fatalf("finalization did not publish pending upload: %#v", got)
	}
	if !m.archiveOne(context.Background(), true) || m.Snapshot(33).Recording.ArchiveStatus != ArchiveReady {
		t.Fatal("finalized WAV not automatically eligible for upload")
	}
}

func TestArchiveDeletedRoomStillCleansAfterRestart(t *testing.T) {
	m, store, now := archiveFixture(t)
	rec := addReadyArchive(t, m, now.Add(-2*time.Minute))
	if !m.archiveOne(context.Background(), true) {
		t.Fatal("upload missing")
	}
	*now = rec.status.ExpiresAt
	restored := &Manager{root: m.root, now: m.now, rooms: make(map[int64]*roomState)}
	if err := restored.ConfigureArchives(store, nil); err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot(33).Recording != nil {
		t.Fatal("deleted room recording exposed")
	}
	if !restored.archiveOne(context.Background(), false) {
		t.Fatal("deleted room object leaked")
	}
	if _, err := os.Stat(filepath.Join(rec.dir, "recording.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("deleted room local files leaked")
	}
}

func TestArchiveInterruptedUploadRecoveryAndForgedKeyIsolation(t *testing.T) {
	m, store, now := archiveFixture(t)
	rec := addReadyArchive(t, m, now.Add(-2*time.Minute))
	job := m.archiveJobs[rec.status.ID]
	job.Recording.ArchiveStatus = ArchiveUploading
	if err := writeArchiveManifest(job.dir, job.archiveManifest); err != nil {
		t.Fatal(err)
	}
	restored := &Manager{root: m.root, now: m.now, rooms: make(map[int64]*roomState)}
	if err := restored.ConfigureArchives(store, []model.Room{{ID: 33, TenantID: 14}}); err != nil {
		t.Fatal(err)
	}
	if restored.Snapshot(33).Recording.ArchiveStatus != ArchivePending || !restored.archiveOne(context.Background(), true) {
		t.Fatal("crashed upload not resumed")
	}
	job.ObjectKey = "formal-plan-audio/keep.wav"
	if err := writeArchiveManifest(job.dir, job.archiveManifest); err != nil {
		t.Fatal(err)
	}
	other := &Manager{root: m.root, now: m.now, rooms: make(map[int64]*roomState)}
	if err := other.ConfigureArchives(store, []model.Room{{ID: 33, TenantID: 14}}); err != nil {
		t.Fatal(err)
	}
	if len(other.archiveJobs) != 0 {
		t.Fatal("foreign object accepted for deletion")
	}
}
