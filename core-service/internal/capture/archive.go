package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/model"
)

const (
	RecordingRetention  = 24 * time.Hour
	ArchivePending      = "pending"
	ArchiveUploading    = "uploading"
	ArchiveReady        = "ready"
	ArchiveFailed       = "failed"
	ArchiveExpired      = "expired"
	archiveManifestName = "archive.json"
)

var (
	ErrRecordingExpired   = errors.New("recording has expired")
	ErrArchiveNotReady    = errors.New("recording has not been uploaded")
	ErrArchiveUnavailable = errors.New("recording OSS archive is unavailable")
	ErrRecordingChanged   = errors.New("recording has changed")
)

// ArchiveStore streams uploads and verifies their size before returning success.
// It never makes recordings public; DownloadURL signs a private object on demand.
type ArchiveStore interface {
	Upload(context.Context, string, io.Reader, int64, time.Time) error
	Delete(context.Context, string) error
	DownloadURL(context.Context, string, string, time.Duration) (string, error)
}

type archiveManifest struct {
	TenantID    int64           `json:"tenant_id"`
	RoomID      int64           `json:"room_id"`
	Recording   RecordingStatus `json:"recording"`
	ObjectKey   string          `json:"object_key"`
	NextAttempt time.Time       `json:"next_attempt,omitempty"`
	Attempts    int             `json:"attempts"`
	Cleaned     bool            `json:"cleaned"`
}

type archiveJob struct {
	archiveManifest
	dir  string
	busy bool
}

type RecordingDownload struct {
	URL                string    `json:"url"`
	ExpiresAt          time.Time `json:"expires_at"`
	RecordingExpiresAt time.Time `json:"recording_expires_at"`
}

func archiveObjectKey(tenantID, roomID int64, id string) string {
	return fmt.Sprintf("live-recordings/tenant-%d/room-%d/%s/recording.wav", tenantID, roomID, id)
}

func writeArchiveManifest(dir string, manifest archiveManifest) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".archive-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), filepath.Join(dir, archiveManifestName))
}

func (m *Manager) persistRecordingLocked(roomID int64, rec *recording) error {
	if m.archiveJobs == nil {
		m.archiveJobs = make(map[string]*archiveJob)
	}
	job := m.archiveJobs[rec.status.ID]
	if job == nil {
		job = &archiveJob{archiveManifest: archiveManifest{TenantID: rec.tenantID, RoomID: roomID, ObjectKey: archiveObjectKey(rec.tenantID, roomID, rec.status.ID)}, dir: rec.dir}
		m.archiveJobs[rec.status.ID] = job
	}
	job.Recording = rec.status
	if m.archiveStore == nil && job.Recording.Status == RecordingReady {
		job.Recording.ArchiveStatus, job.Recording.ArchiveError = ArchiveFailed, "录音已保留，请联系管理员检查云端存储配置"
		rec.status = job.Recording
	}
	return writeArchiveManifest(job.dir, job.archiveManifest)
}

func (m *Manager) wakeArchives() {
	if m.archiveWake != nil {
		select {
		case m.archiveWake <- struct{}{}:
		default:
		}
	}
}

// ConfigureArchives restores durable jobs. Legacy WAVs are adopted only for
// known rooms and only within their original retention window; old files stay
// untouched. Call once, before serving requests or starting archive workers.
func (m *Manager) ConfigureArchives(store ArchiveStore, rooms []model.Room) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archiveStore = store
	m.archiveWake = make(chan struct{}, 1)
	if m.archiveJobs == nil {
		m.archiveJobs = make(map[string]*archiveJob)
	}
	roomTenants := make(map[int64]int64)
	for _, room := range rooms {
		roomTenants[room.ID] = room.TenantID
	}
	roomEntries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	for _, roomEntry := range roomEntries {
		if !roomEntry.IsDir() || !strings.HasPrefix(roomEntry.Name(), "room-") {
			continue
		}
		roomID, err := strconv.ParseInt(strings.TrimPrefix(roomEntry.Name(), "room-"), 10, 64)
		if err != nil || roomID <= 0 || roomEntry.Name() != fmt.Sprintf("room-%d", roomID) {
			continue
		}
		room := model.Room{ID: roomID, TenantID: roomTenants[roomID]}
		roomDir := filepath.Join(m.root, roomEntry.Name())
		if info, err := os.Lstat(roomDir); err != nil || !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(roomDir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() || !validRecordingID(entry.Name(), room.ID) {
				continue
			}
			dir := filepath.Join(roomDir, entry.Name())
			raw, readErr := os.ReadFile(filepath.Join(dir, archiveManifestName))
			var manifest archiveManifest
			if readErr == nil {
				if json.Unmarshal(raw, &manifest) != nil || manifest.TenantID <= 0 || manifest.RoomID != room.ID || manifest.Recording.ID != entry.Name() || manifest.ObjectKey != archiveObjectKey(manifest.TenantID, room.ID, entry.Name()) {
					continue
				}
			} else if errors.Is(readErr, os.ErrNotExist) {
				if room.TenantID <= 0 {
					continue
				}
				info, err := os.Lstat(filepath.Join(dir, "recording.wav"))
				if err != nil || !info.Mode().IsRegular() || !m.now().Before(info.ModTime().Add(RecordingRetention)) {
					continue
				}
				started, _ := time.Parse("20060102-150405", entry.Name()[:15])
				manifest = archiveManifest{TenantID: room.TenantID, RoomID: room.ID, ObjectKey: archiveObjectKey(room.TenantID, room.ID, entry.Name()), Recording: RecordingStatus{ID: entry.Name(), Status: RecordingReady, StartedAt: started, FinishedAt: info.ModTime().UTC(), DurationSeconds: maxInt64(0, int64(info.ModTime().Sub(started).Seconds())), SegmentCount: segmentCount(dir), FinalFileName: "recording.wav", FinalBytes: info.Size(), ExpiresAt: info.ModTime().UTC().Add(RecordingRetention), ArchiveStatus: ArchivePending}}
			} else {
				return readErr
			}
			status := &manifest.Recording
			if status.Status == RecordingRecording || status.Status == RecordingFinalizing {
				// A crashed FFmpeg is not a successfully completed recording.
				status.Status, status.Error = RecordingFailed, "录制进程已中断，请重新录制"
			}
			if status.Status == RecordingReady {
				if status.FinishedAt.IsZero() {
					continue
				}
				// Derive from completion, never from startup or a later download.
				status.ExpiresAt = status.FinishedAt.Add(RecordingRetention)
				if status.ArchiveStatus == ArchiveUploading {
					status.ArchiveStatus = ArchivePending
				}
				if !m.now().Before(status.ExpiresAt) {
					status.ArchiveStatus = ArchiveExpired
				}
				if store == nil && status.ArchiveStatus != ArchiveExpired {
					status.ArchiveStatus, status.ArchiveError = ArchiveFailed, "录音已保留，请联系管理员检查云端存储配置"
				}
			}
			job := &archiveJob{archiveManifest: manifest, dir: dir}
			if err := writeArchiveManifest(dir, manifest); err != nil {
				return err
			}
			m.archiveJobs[status.ID] = job
			// Deleted/transferred rooms still need expiry cleanup; never expose
			// the previous tenant's recording as the new tenant's latest capture.
			if room.TenantID != manifest.TenantID {
				continue
			}
			state := m.stateLocked(room.ID)
			if state.recording == nil || state.recording.status.StartedAt.Before(status.StartedAt) {
				state.recording = &recording{tenantID: room.TenantID, status: *status, dir: dir}
			}
		}
	}
	return nil
}

func validRecordingID(id string, roomID int64) bool {
	if len(id) < 22 || !strings.HasSuffix(id, "-room-"+strconv.FormatInt(roomID, 10)) {
		return false
	}
	_, err := time.Parse("20060102-150405", id[:15])
	return err == nil && id == id[:15]+"-room-"+strconv.FormatInt(roomID, 10)
}

func (m *Manager) syncArchiveStatusLocked(job *archiveJob) {
	if state := m.rooms[job.RoomID]; state != nil && state.recording != nil && state.recording.status.ID == job.Recording.ID {
		state.recording.status = job.Recording
	}
}

// Two bounded streaming uploads and a separate expiry worker keep a slow upload
// from blocking cleanup or creating an unbounded number of file readers.
func (m *Manager) RunArchives(ctx context.Context) {
	var workers sync.WaitGroup
	for _, upload := range []bool{true, true, false} {
		workers.Add(1)
		go func(upload bool) {
			defer workers.Done()
			ticker := time.NewTicker(5 * time.Second)
			defer ticker.Stop()
			for ctx.Err() == nil {
				if m.archiveOne(ctx, upload) {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				case <-m.archiveWake:
				}
			}
		}(upload)
	}
	workers.Wait()
}

func (m *Manager) archiveOne(ctx context.Context, upload bool) bool {
	m.mu.Lock()
	now := m.now().UTC()
	var jobs []*archiveJob
	for _, job := range m.archiveJobs {
		if job.busy || job.Cleaned || job.Recording.Status != RecordingReady || now.Before(job.NextAttempt) {
			continue
		}
		expired := !now.Before(job.Recording.ExpiresAt)
		if upload {
			if expired || m.archiveStore == nil || job.Recording.ArchiveStatus == ArchiveReady {
				continue
			}
		} else if !expired {
			continue
		}
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Recording.StartedAt.Before(jobs[j].Recording.StartedAt) })
	if len(jobs) == 0 {
		m.mu.Unlock()
		return false
	}
	job := jobs[0]
	job.busy = true
	if upload {
		job.Recording.ArchiveStatus = ArchiveUploading
	} else {
		job.Recording.ArchiveStatus = ArchiveExpired
	}
	m.syncArchiveStatusLocked(job)
	manifestErr := writeArchiveManifest(job.dir, job.archiveManifest)
	store, manifest, dir := m.archiveStore, job.archiveManifest, job.dir
	m.mu.Unlock()
	var err error
	if manifestErr != nil {
		err = manifestErr
	} else if upload {
		deadline := time.Until(manifest.Recording.ExpiresAt)
		if deadline > 30*time.Minute {
			deadline = 30 * time.Minute
		}
		uploadCtx, cancel := context.WithTimeout(ctx, deadline)
		err = uploadRecording(uploadCtx, store, dir, manifest)
		cancel()
	} else {
		cleanupCtx, cancel := context.WithTimeout(ctx, time.Minute)
		if store != nil {
			err = store.Delete(cleanupCtx, manifest.ObjectKey)
		} else {
			err = ErrArchiveUnavailable
		}
		cancel()
		if err == nil {
			err = cleanRecordingFiles(dir)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job.busy = false
	now = m.now().UTC()
	job.Attempts++
	if err != nil {
		job.NextAttempt = now.Add(time.Minute)
		if upload {
			job.Recording.ArchiveStatus, job.Recording.ArchiveError = ArchiveFailed, "上传失败，录音已保留，将自动重试"
		}
	} else {
		job.NextAttempt = time.Time{}
		job.Recording.ArchiveError = ""
		if upload {
			job.Recording.ArchiveStatus, job.Recording.UploadedAt = ArchiveReady, now
		} else {
			job.Cleaned = true
		}
	}
	if !now.Before(job.Recording.ExpiresAt) {
		job.Recording.ArchiveStatus = ArchiveExpired
	}
	if err := writeArchiveManifest(dir, job.archiveManifest); err != nil {
		// A ready object without a durable receipt must be retried after restart.
		job.NextAttempt = now.Add(time.Minute)
		if upload {
			job.Recording.ArchiveStatus, job.Recording.ArchiveError = ArchiveFailed, "上传结果保存失败，等待重试"
		}
	}
	m.syncArchiveStatusLocked(job)
	return true
}

func uploadRecording(ctx context.Context, store ArchiveStore, dir string, manifest archiveManifest) error {
	file := filepath.Join(dir, "recording.wav")
	info, err := os.Lstat(file)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != manifest.Recording.FinalBytes {
		return errors.New("recording size changed")
	}
	if err := validateWAVAudio(file); err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	return store.Upload(ctx, manifest.ObjectKey, f, info.Size(), manifest.Recording.ExpiresAt)
}

func cleanRecordingFiles(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name != "recording.wav" && name != "concat.txt" && name != "ffmpeg.log" && name != "merge.log" && !(strings.HasPrefix(name, "segment_") && strings.HasSuffix(name, ".flac")) {
			continue
		}
		if entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (m *Manager) RecordingDownload(ctx context.Context, tenantID, roomID int64, name string, expectedIDs ...string) (RecordingDownload, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.rooms[roomID]
	if state == nil || state.recording == nil || state.recording.tenantID != tenantID || state.recording.status.Status != RecordingReady {
		return RecordingDownload{}, ErrRecordingNotReady
	}
	status := state.recording.status
	if len(expectedIDs) > 0 && expectedIDs[0] != "" && expectedIDs[0] != status.ID {
		return RecordingDownload{}, ErrRecordingChanged
	}
	now := m.now().UTC()
	if status.ExpiresAt.IsZero() || !now.Before(status.ExpiresAt) {
		return RecordingDownload{}, ErrRecordingExpired
	}
	if m.archiveStore == nil {
		return RecordingDownload{}, ErrArchiveUnavailable
	}
	if status.ArchiveStatus != ArchiveReady {
		return RecordingDownload{}, ErrArchiveNotReady
	}
	job := m.archiveJobs[status.ID]
	if job == nil {
		return RecordingDownload{}, ErrArchiveNotReady
	}
	ttl := status.ExpiresAt.Sub(now)
	if ttl > 15*time.Minute {
		ttl = 15 * time.Minute
	}
	// OSS expiry is in whole seconds, and must not extend beyond retention.
	ttl = ttl.Truncate(time.Second)
	if ttl < time.Second {
		return RecordingDownload{}, ErrRecordingExpired
	}
	url, err := m.archiveStore.DownloadURL(ctx, job.ObjectKey, name, ttl)
	if err != nil {
		return RecordingDownload{}, ErrArchiveUnavailable
	}
	return RecordingDownload{URL: url, ExpiresAt: now.Add(ttl), RecordingExpiresAt: status.ExpiresAt}, nil
}
