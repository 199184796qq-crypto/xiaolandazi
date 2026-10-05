package capture

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

const (
	ModeIdle           = "idle"
	ModeAudioRecording = "audio_recording"
	ModeFinalizing     = "finalizing"

	RecordingRecording  = "recording"
	RecordingFinalizing = "finalizing"
	RecordingReady      = "ready"
	RecordingFailed     = "failed"
)

var (
	ErrCaptureBusy       = errors.New("capture mode is busy")
	ErrRecordingMissing  = errors.New("audio recording is not active")
	ErrRecordingNotReady = errors.New("audio recording is not ready")
)

type StreamResolver interface {
	Stream(context.Context, model.Room) (collector.StreamSource, error)
}

type RecordingStatus struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at,omitempty"`
	DurationSeconds int64     `json:"duration_seconds"`
	SegmentCount    int       `json:"segment_count"`
	FinalFileName   string    `json:"final_file_name,omitempty"`
	FinalBytes      int64     `json:"final_bytes,omitempty"`
	SourceProtocol  string    `json:"source_protocol,omitempty"`
	Error           string    `json:"error,omitempty"`
}

type Snapshot struct {
	RoomID    int64            `json:"room_id"`
	Mode      string           `json:"mode"`
	Recording *RecordingStatus `json:"recording,omitempty"`
}

type recording struct {
	status      RecordingStatus
	dir         string
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	logFile     *os.File
	processDone chan struct{}
	done        chan struct{}
}

type roomState struct {
	recording *recording
}

type Manager struct {
	ffmpegPath string
	root       string
	resolver   StreamResolver
	now        func() time.Time

	mu    sync.Mutex
	rooms map[int64]*roomState
}

func NewManager(ffmpegPath, root string, resolver StreamResolver) (*Manager, error) {
	resolved, err := resolveFFmpeg(ffmpegPath)
	if err != nil {
		return nil, err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		root = "data/recordings"
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create capture root: %w", err)
	}
	return &Manager{
		ffmpegPath: resolved,
		root:       root,
		resolver:   resolver,
		now:        time.Now,
		rooms:      make(map[int64]*roomState),
	}, nil
}

func (m *Manager) stateLocked(roomID int64) *roomState {
	state := m.rooms[roomID]
	if state == nil {
		state = &roomState{}
		m.rooms[roomID] = state
	}
	return state
}

func (m *Manager) Snapshot(roomID int64) Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	state := m.stateLocked(roomID)
	return m.snapshotLocked(roomID, state, now)
}

func (m *Manager) snapshotLocked(roomID int64, state *roomState, now time.Time) Snapshot {
	result := Snapshot{RoomID: roomID, Mode: ModeIdle}
	if state.recording != nil {
		status := state.recording.status
		if status.Status == RecordingRecording {
			status.DurationSeconds = maxInt64(0, int64(now.Sub(status.StartedAt).Seconds()))
			result.Mode = ModeAudioRecording
		} else if status.Status == RecordingFinalizing {
			result.Mode = ModeFinalizing
		}
		status.SegmentCount = segmentCount(state.recording.dir)
		if status.FinalFileName != "" {
			if info, err := os.Stat(filepath.Join(state.recording.dir, status.FinalFileName)); err == nil {
				status.FinalBytes = info.Size()
			}
		}
		result.Recording = &status
	}
	return result
}

func (m *Manager) StartAudio(ctx context.Context, room model.Room) (Snapshot, error) {
	if m.resolver == nil {
		return Snapshot{}, errors.New("stream resolver is not configured")
	}
	m.mu.Lock()
	now := m.now().UTC()
	state := m.stateLocked(room.ID)
	if state.recording != nil && (state.recording.status.Status == RecordingRecording || state.recording.status.Status == RecordingFinalizing) {
		snapshot := m.snapshotLocked(room.ID, state, now)
		m.mu.Unlock()
		return snapshot, fmt.Errorf("%w: audio recording is already active", ErrCaptureBusy)
	}
	m.mu.Unlock()

	source, err := m.waitForStream(ctx, room)
	if err != nil {
		return m.Snapshot(room.ID), err
	}

	startedAt := m.now().UTC()
	id := fmt.Sprintf("%s-room-%d", startedAt.Format("20060102-150405"), room.ID)
	dir := filepath.Join(m.root, fmt.Sprintf("room-%d", room.ID), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return m.Snapshot(room.ID), fmt.Errorf("create recording dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "ffmpeg.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return m.Snapshot(room.ID), fmt.Errorf("open recording log: %w", err)
	}

	segmentPattern := filepath.Join(dir, "segment_%06d.flac")
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-rw_timeout", "5000000",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "2",
		"-probesize", "10000000",
		"-analyzeduration", "5000000",
		"-user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36",
		"-headers", "Referer: https://live.douyin.com/\r\nOrigin: https://live.douyin.com\r\n",
		"-i", source.URL,
		"-vn",
		"-map", "0:a:0",
		"-c:a", "flac",
		"-ar", "44100",
		"-ac", "2",
		"-f", "segment",
		"-segment_time", "60",
		"-reset_timestamps", "1",
		segmentPattern,
	}
	cmd := exec.Command(m.ffmpegPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = logFile.Close()
		return m.Snapshot(room.ID), fmt.Errorf("open ffmpeg stdin: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = logFile.Close()
		return m.Snapshot(room.ID), fmt.Errorf("start audio recording: %w", err)
	}

	rec := &recording{
		status: RecordingStatus{
			ID:             id,
			Status:         RecordingRecording,
			StartedAt:      startedAt,
			SourceProtocol: source.Protocol,
		},
		dir:         dir,
		cmd:         cmd,
		stdin:       stdin,
		logFile:     logFile,
		processDone: make(chan struct{}),
		done:        make(chan struct{}),
	}

	m.mu.Lock()
	if m.rooms[room.ID] != state || (state.recording != nil && (state.recording.status.Status == RecordingRecording || state.recording.status.Status == RecordingFinalizing)) {
		m.mu.Unlock()
		_, _ = io.WriteString(stdin, "q\n")
		_ = cmd.Wait()
		_ = stdin.Close()
		_ = logFile.Close()
		return m.Snapshot(room.ID), fmt.Errorf("%w: capture mode changed while starting", ErrCaptureBusy)
	}
	state.recording = rec
	snapshot := m.snapshotLocked(room.ID, state, m.now().UTC())
	m.mu.Unlock()

	go m.waitRecording(room.ID, rec)
	return snapshot, nil
}

func (m *Manager) StopAudio(_ context.Context, roomID int64) (Snapshot, error) {
	m.mu.Lock()
	state := m.stateLocked(roomID)
	rec := state.recording
	if rec == nil || rec.status.Status != RecordingRecording {
		snapshot := m.snapshotLocked(roomID, state, m.now().UTC())
		m.mu.Unlock()
		return snapshot, ErrRecordingMissing
	}

	// Stopping is intentionally asynchronous. The UI already understands the
	// finalizing state and polls for completion, so there is no reason to keep
	// the HTTP request open while ffmpeg flushes and the WAV file is assembled.
	rec.status.Status = RecordingFinalizing
	snapshot := m.snapshotLocked(roomID, state, m.now().UTC())
	stdin := rec.stdin
	process := rec.cmd
	m.mu.Unlock()

	if stdin != nil {
		if _, err := io.WriteString(stdin, "q\n"); err == nil {
			_ = stdin.Close()
			go forceStopRecording(rec, 3*time.Second)
			return snapshot, nil
		}
	}
	// If stdin has already gone away, make sure a wedged ffmpeg process cannot
	// keep this room locked forever. waitRecording will still finalize whatever
	// complete audio segments were written before the process exited.
	if process != nil && process.Process != nil {
		_ = process.Process.Kill()
	}
	return snapshot, nil
}

func (m *Manager) RecordingFile(roomID int64) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.stateLocked(roomID)
	if state.recording == nil || state.recording.status.Status != RecordingReady || state.recording.status.FinalFileName == "" {
		return "", "", ErrRecordingNotReady
	}
	path := filepath.Join(state.recording.dir, state.recording.status.FinalFileName)
	if _, err := os.Stat(path); err != nil {
		return "", "", err
	}
	return path, state.recording.status.FinalFileName, nil
}

func (m *Manager) Close() {
	m.mu.Lock()
	recordings := make([]*recording, 0)
	for _, state := range m.rooms {
		if state.recording != nil && state.recording.status.Status == RecordingRecording {
			recordings = append(recordings, state.recording)
		}
	}
	m.mu.Unlock()
	for _, rec := range recordings {
		if rec.stdin != nil {
			_, _ = io.WriteString(rec.stdin, "q\n")
		}
	}
	for _, rec := range recordings {
		select {
		case <-rec.done:
		case <-time.After(8 * time.Second):
			if rec.cmd != nil && rec.cmd.Process != nil {
				_ = rec.cmd.Process.Kill()
			}
		}
	}
}

func (m *Manager) waitForStream(ctx context.Context, room model.Room) (collector.StreamSource, error) {
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		source, err := m.resolver.Stream(ctx, room)
		if err == nil && strings.TrimSpace(source.URL) != "" {
			return source, nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return collector.StreamSource{}, ctx.Err()
		case <-deadline.C:
			if lastErr != nil {
				return collector.StreamSource{}, fmt.Errorf("live stream audio source unavailable: %w", lastErr)
			}
			return collector.StreamSource{}, errors.New("live stream audio source unavailable")
		case <-ticker.C:
		}
	}
}

func (m *Manager) waitRecording(roomID int64, rec *recording) {
	waitErr := rec.cmd.Wait()
	if rec.processDone != nil {
		close(rec.processDone)
	}
	if rec.stdin != nil {
		_ = rec.stdin.Close()
	}
	if rec.logFile != nil {
		_ = rec.logFile.Close()
	}

	m.mu.Lock()
	state := m.rooms[roomID]
	if state == nil || state.recording != rec {
		m.mu.Unlock()
		close(rec.done)
		return
	}
	rec.status.Status = RecordingFinalizing
	m.mu.Unlock()

	finalErr := m.finalize(rec)
	finishedAt := m.now().UTC()

	m.mu.Lock()
	if state.recording == rec {
		rec.status.FinishedAt = finishedAt
		rec.status.DurationSeconds = maxInt64(0, int64(finishedAt.Sub(rec.status.StartedAt).Seconds()))
		rec.status.SegmentCount = segmentCount(rec.dir)
		if finalErr != nil {
			rec.status.Status = RecordingFailed
			rec.status.Error = finalErr.Error()
			if waitErr != nil {
				rec.status.Error = waitErr.Error() + "; " + rec.status.Error
			}
		} else {
			rec.status.Status = RecordingReady
			if waitErr != nil {
				rec.status.Error = waitErr.Error()
			}
			rec.status.FinalFileName = "recording.wav"
			if info, err := os.Stat(filepath.Join(rec.dir, rec.status.FinalFileName)); err == nil {
				rec.status.FinalBytes = info.Size()
			}
		}
	}
	m.mu.Unlock()
	close(rec.done)
}

func (m *Manager) finalize(rec *recording) error {
	segments, err := filepath.Glob(filepath.Join(rec.dir, "segment_*.flac"))
	if err != nil {
		return err
	}
	sort.Strings(segments)
	if len(segments) == 0 {
		return errors.New("recording produced no audio segments")
	}
	listPath := filepath.Join(rec.dir, "concat.txt")
	listFile, err := os.Create(listPath)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(listFile)
	for _, segment := range segments {
		_, _ = fmt.Fprintf(writer, "file '%s'\n", filepath.Base(segment))
	}
	if err := writer.Flush(); err != nil {
		_ = listFile.Close()
		return err
	}
	if err := listFile.Close(); err != nil {
		return err
	}

	mergeLog, err := os.OpenFile(filepath.Join(rec.dir, "merge.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer mergeLog.Close()

	cmd := exec.Command(
		m.ffmpegPath,
		"-hide_banner",
		"-loglevel", "warning",
		"-f", "concat",
		"-safe", "0",
		"-i", "concat.txt",
		"-vn",
		"-c:a", "pcm_s16le",
		"-ar", "44100",
		"-ac", "2",
		"recording.wav",
	)
	cmd.Dir = rec.dir
	cmd.Stdout = mergeLog
	cmd.Stderr = mergeLog
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("merge audio recording: %w", err)
	}
	if err := validateWAVAudio(filepath.Join(rec.dir, "recording.wav")); err != nil {
		return err
	}
	return nil
}

func forceStopRecording(rec *recording, grace time.Duration) {
	if rec == nil || rec.cmd == nil || rec.cmd.Process == nil {
		return
	}
	wait := rec.processDone
	if wait == nil {
		wait = rec.done
	}
	if wait == nil {
		return
	}
	if grace <= 0 {
		grace = 3 * time.Second
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-wait:
		return
	case <-timer.C:
		_ = rec.cmd.Process.Kill()
	}
}

func validateWAVAudio(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open merged WAV: %w", err)
	}
	defer f.Close()

	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return fmt.Errorf("merged WAV header: %w", err)
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return errors.New("merged recording is not a valid WAV file")
	}

	chunkHeader := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, chunkHeader); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return errors.New("recording WAV contains no audio data")
			}
			return fmt.Errorf("read merged WAV chunk: %w", err)
		}
		size := binary.LittleEndian.Uint32(chunkHeader[4:8])
		if string(chunkHeader[:4]) == "data" {
			if size == 0 {
				return errors.New("recording WAV contains no audio data")
			}
			return nil
		}
		skip := int64(size)
		if size%2 == 1 {
			skip++
		}
		if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
			return fmt.Errorf("scan merged WAV chunks: %w", err)
		}
	}
}

func segmentCount(dir string) int {
	items, _ := filepath.Glob(filepath.Join(dir, "segment_*.flac"))
	return len(items)
}

func resolveFFmpeg(configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" && !strings.EqualFold(configured, "ffmpeg") {
		if resolved, err := exec.LookPath(configured); err == nil {
			return resolved, nil
		}
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("find ffmpeg %q: executable not found", configured)
	}

	binaryName := "ffmpeg"
	if runtime.GOOS == "windows" {
		binaryName = "ffmpeg.exe"
	}

	// Prefer the project-bundled ffmpeg. Douyin currently serves enhanced FLV
	// streams that the old ffmpeg commonly present on PATH cannot demux
	// correctly; using that binary can create a valid-looking but silent WAV.
	searchStarts := make([]string, 0, 2)
	if workingDir, err := os.Getwd(); err == nil {
		searchStarts = append(searchStarts, workingDir)
	}
	if executable, err := os.Executable(); err == nil {
		searchStarts = append(searchStarts, filepath.Dir(executable))
	}
	seen := make(map[string]struct{})
	for _, start := range searchStarts {
		dir := filepath.Clean(start)
		for depth := 0; depth < 8; depth++ {
			if _, ok := seen[dir]; !ok {
				seen[dir] = struct{}{}
				candidate := filepath.Join(dir, "data", "tools", "ffmpeg", "bin", binaryName)
				if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
					return candidate, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if resolved, err := exec.LookPath("ffmpeg"); err == nil {
		return resolved, nil
	}
	return "", errors.New("ffmpeg executable not found")
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
