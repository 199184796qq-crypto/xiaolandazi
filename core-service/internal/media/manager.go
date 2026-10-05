package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

var (
	ErrStreamUnavailable = errors.New("live stream unavailable")
	ErrMediaCapacity     = errors.New("live preview capacity reached")
)

const (
	playlistName = "index.m3u8"
	idleTimeout  = 30 * time.Second
)

type StreamResolver interface {
	Stream(context.Context, model.Room) (collector.StreamSource, error)
}

type session struct {
	roomID     int64
	sourceURL  string
	dir        string
	cmd        *exec.Cmd
	done       chan struct{}
	lastAccess time.Time
	err        error
}

type Stats struct {
	ActiveSessions int
	MaxSessions    int
}

type Manager struct {
	ffmpegPath  string
	root        string
	resolver    StreamResolver
	maxSessions int

	mu           sync.Mutex
	sessions     map[int64]*session
	deletedRooms map[int64]struct{}
	stop         chan struct{}
	wg           sync.WaitGroup
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{
		ActiveSessions: len(m.sessions),
		MaxSessions:    m.maxSessions,
	}
}

func NewManager(
	ffmpegPath string,
	root string,
	resolver StreamResolver,
	maxSessions int,
) (*Manager, error) {
	resolved, err := resolveFFmpeg(strings.TrimSpace(ffmpegPath))
	if err != nil {
		return nil, err
	}

	root = strings.TrimSpace(root)
	if root == "" {
		root = filepath.Join(os.TempDir(), "live-companion", "hls")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create media cache root: %w", err)
	}

	if maxSessions <= 0 {
		maxSessions = 8
	}
	manager := &Manager{
		ffmpegPath:  resolved,
		root:        root,
		resolver:    resolver,
		maxSessions: maxSessions,
		sessions:    make(map[int64]*session),
		stop:        make(chan struct{}),
	}

	manager.wg.Add(1)
	go manager.reapLoop()

	return manager, nil
}

func (m *Manager) File(
	ctx context.Context,
	room model.Room,
	name string,
) (string, error) {
	if !validMediaFile(name) {
		return "", errors.New("invalid media file")
	}

	source, err := m.waitForSource(ctx, room)
	if err != nil {
		return "", err
	}

	current, err := m.ensureSession(room, source)
	if err != nil {
		return "", err
	}

	playlist := filepath.Join(current.dir, playlistName)
	if err := waitForFile(ctx, current, playlist, 9*time.Second); err != nil {
		return "", err
	}

	m.touch(current)

	target := filepath.Join(current.dir, name)
	wait := 2500 * time.Millisecond
	if name == playlistName {
		wait = 2 * time.Second
	}
	if err := waitForFile(ctx, current, target, wait); err != nil {
		return "", err
	}

	return target, nil
}

func (m *Manager) Close() {
	close(m.stop)

	m.mu.Lock()
	sessions := make([]*session, 0, len(m.sessions))
	for _, current := range m.sessions {
		sessions = append(sessions, current)
	}
	m.sessions = make(map[int64]*session)
	m.mu.Unlock()

	for _, current := range sessions {
		stopSession(current)
	}

	m.wg.Wait()
}

func (m *Manager) waitForSource(
	ctx context.Context,
	room model.Room,
) (collector.StreamSource, error) {
	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()

	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()

	for {
		source, err := m.resolver.Stream(ctx, room)
		if err == nil && strings.TrimSpace(source.URL) != "" {
			return source, nil
		}

		select {
		case <-ctx.Done():
			return collector.StreamSource{}, ctx.Err()
		case <-deadline.C:
			return collector.StreamSource{}, fmt.Errorf("%w: source not discovered", ErrStreamUnavailable)
		case <-timer.C:
		}
	}
}

func (m *Manager) ensureSession(
	room model.Room,
	source collector.StreamSource,
) (*session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, deleted := m.deletedRooms[room.ID]; deleted {
		return nil, errors.New("room has been permanently deleted")
	}

	if current := m.sessions[room.ID]; current != nil {
		select {
		case <-current.done:
			delete(m.sessions, room.ID)
		default:
			if current.sourceURL == source.URL {
				current.lastAccess = time.Now()
				return current, nil
			}
			delete(m.sessions, room.ID)
			stopSession(current)
		}
	}

	if len(m.sessions) >= m.maxSessions {
		return nil, fmt.Errorf("%w: max_sessions=%d", ErrMediaCapacity, m.maxSessions)
	}

	dir := filepath.Join(m.root, fmt.Sprintf("room-%d", room.ID))
	if err := os.RemoveAll(dir); err != nil {
		return nil, fmt.Errorf("clear media cache: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create room media cache: %w", err)
	}

	logFile, err := os.OpenFile(
		filepath.Join(dir, "ffmpeg.log"),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o644,
	)
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg log: %w", err)
	}

	segmentPattern := filepath.Join(dir, "seg_%06d.ts")
	playlist := filepath.Join(dir, playlistName)

	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-nostdin",
		"-rw_timeout", "5000000",
		"-reconnect", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "2",
		"-user_agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36",
		"-headers", "Referer: https://live.douyin.com/\r\nOrigin: https://live.douyin.com\r\n",
		"-i", source.URL,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-vf", "scale=-2:720:force_original_aspect_ratio=decrease,pad=ceil(iw/2)*2:ceil(ih/2)*2,fps=20",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-tune", "zerolatency",
		"-pix_fmt", "yuv420p",
		"-g", "20",
		"-keyint_min", "20",
		"-sc_threshold", "0",
		"-maxrate", "1400k",
		"-bufsize", "2800k",
		"-c:a", "aac",
		"-b:a", "96k",
		"-ac", "2",
		"-ar", "44100",
		"-f", "hls",
		"-hls_time", "1",
		"-hls_list_size", "4",
		"-hls_flags", "delete_segments+omit_endlist+independent_segments",
		"-hls_segment_filename", segmentPattern,
		playlist,
	}

	cmd := exec.Command(m.ffmpegPath, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	current := &session{
		roomID:     room.ID,
		sourceURL:  source.URL,
		dir:        dir,
		cmd:        cmd,
		done:       make(chan struct{}),
		lastAccess: time.Now(),
	}
	m.sessions[room.ID] = current

	go func() {
		err := cmd.Wait()
		_ = logFile.Close()

		m.mu.Lock()
		current.err = err
		if m.sessions[room.ID] == current {
			delete(m.sessions, room.ID)
		}
		close(current.done)
		m.mu.Unlock()
	}()

	return current, nil
}

func (m *Manager) touch(current *session) {
	m.mu.Lock()
	if active := m.sessions[current.roomID]; active == current {
		current.lastAccess = time.Now()
	}
	m.mu.Unlock()
}

func (m *Manager) reapLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			var stale []*session

			m.mu.Lock()
			for roomID, current := range m.sessions {
				if now.Sub(current.lastAccess) > idleTimeout {
					delete(m.sessions, roomID)
					stale = append(stale, current)
				}
			}
			m.mu.Unlock()

			for _, current := range stale {
				stopSession(current)
			}
		}
	}
}

func stopSession(current *session) {
	if current == nil || current.cmd == nil || current.cmd.Process == nil {
		return
	}

	select {
	case <-current.done:
		return
	default:
		_ = current.cmd.Process.Kill()
	}
}

func waitForFile(
	ctx context.Context,
	current *session,
	path string,
	timeout time.Duration,
) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-current.done:
			if current.err != nil {
				return fmt.Errorf("ffmpeg exited: %w", current.err)
			}
			return errors.New("ffmpeg exited")
		case <-deadline.C:
			return fmt.Errorf("%w: media file %s not ready", ErrStreamUnavailable, filepath.Base(path))
		case <-ticker.C:
		}
	}
}

func resolveFFmpeg(configured string) (string, error) {
	if configured != "" && !strings.EqualFold(configured, "ffmpeg") {
		if resolved, err := exec.LookPath(configured); err == nil {
			return resolved, nil
		}
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("find ffmpeg %q: executable not found", configured)
	}

	if executable, err := os.Executable(); err == nil {
		projectRoot := filepath.Clean(
			filepath.Join(filepath.Dir(executable), "..", ".."),
		)
		binaryName := "ffmpeg"
		if runtime.GOOS == "windows" {
			binaryName = "ffmpeg.exe"
		}

		localPath := filepath.Join(
			projectRoot,
			"data",
			"tools",
			"ffmpeg",
			"bin",
			binaryName,
		)
		if info, statErr := os.Stat(localPath); statErr == nil && !info.IsDir() {
			return localPath, nil
		}
	}

	if resolved, err := exec.LookPath("ffmpeg"); err == nil {
		return resolved, nil
	}

	return "", errors.New("ffmpeg executable not found")
}
func validMediaFile(name string) bool {
	if name == playlistName {
		return true
	}
	if !strings.HasPrefix(name, "seg_") || !strings.HasSuffix(name, ".ts") {
		return false
	}
	if filepath.Base(name) != name {
		return false
	}
	return true
}
