package douyin

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

var (
	ErrBrowserUnavailable = errors.New("collector worker unavailable")
	ErrWorkerClosed       = errors.New("collector worker closed")
)

type workerCommand struct {
	Op        string `json:"op"`
	RoomID    int64  `json:"room_id,omitempty"`
	URL       string `json:"url,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

type workerMessage struct {
	Type         string `json:"type"`
	RoomID       int64  `json:"room_id,omitempty"`
	RequestID    string `json:"request_id,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	Protocol     string `json:"protocol,omitempty"`
	ResourceType string `json:"resource_type,omitempty"`
	URL          string `json:"url,omitempty"`
	Payload      string `json:"payload,omitempty"`
	Error        string `json:"error,omitempty"`
	FinalURL     string `json:"final_url,omitempty"`
	Title        string `json:"title,omitempty"`
	State        string `json:"state,omitempty"`
	Reason       string `json:"reason,omitempty"`
	PID          int    `json:"pid,omitempty"`
}

type previewResult struct {
	data        []byte
	contentType string
	err         error
}
type workerProcess struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	ready     chan struct{}
	readyOnce sync.Once
	done      chan struct{}

	errMu sync.Mutex
	err   error
}

func (p *workerProcess) markReady() {
	p.readyOnce.Do(func() {
		close(p.ready)
	})
}

func (p *workerProcess) setErr(err error) {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	p.err = err
}

func (p *workerProcess) getErr() error {
	p.errMu.Lock()
	defer p.errMu.Unlock()
	return p.err
}

type roomStateSignal struct {
	state  string
	reason string
}

type roomSession struct {
	frames    chan []byte
	errors    chan error
	transport chan struct{}
	states    chan roomStateSignal

	transportOnce sync.Once
}

func (s *roomSession) markTransport() {
	s.transportOnce.Do(func() {
		close(s.transport)
	})
}

func (s *roomSession) sendState(state string, reason string) {
	select {
	case s.states <- roomStateSignal{state: state, reason: reason}:
	default:
	}
}

type BrowserSession struct {
	manager *BrowserManager
	roomID  int64
	session *roomSession
	onClose func()
	once    sync.Once
}

func (s *BrowserSession) Frames() <-chan []byte {
	return s.session.frames
}

func (s *BrowserSession) Errors() <-chan error {
	return s.session.errors
}

func (s *BrowserSession) Transport() <-chan struct{} {
	return s.session.transport
}

func (s *BrowserSession) States() <-chan roomStateSignal {
	return s.session.states
}

func (s *BrowserSession) Close() {
	s.once.Do(func() {
		s.manager.StopRoom(s.roomID)
		if s.onClose != nil {
			s.onClose()
		}
	})
}

type BrowserManager struct {
	mu      sync.Mutex
	writeMu sync.Mutex

	previewMu      sync.Mutex
	previewSeq     uint64
	previewWaiters map[string]chan previewResult

	streamMu sync.RWMutex
	streams  map[int64]collector.StreamSource

	configuredPath string
	headless       bool

	worker   *workerProcess
	sessions map[int64]*roomSession
}

func NewBrowserManager(configuredPath string, headless bool) *BrowserManager {
	return &BrowserManager{
		configuredPath: strings.TrimSpace(configuredPath),
		headless:       headless,
		sessions:       make(map[int64]*roomSession),
		previewWaiters: make(map[string]chan previewResult),
		streams:        make(map[int64]collector.StreamSource),
	}
}

func (m *BrowserManager) clearStream(roomID int64) {
	m.streamMu.Lock()
	delete(m.streams, roomID)
	m.streamMu.Unlock()
}

func (m *BrowserManager) clearStreams() {
	m.streamMu.Lock()
	m.streams = make(map[int64]collector.StreamSource)
	m.streamMu.Unlock()
}

func (m *BrowserManager) StartRoom(
	ctx context.Context,
	room model.Room,
) (*BrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	m.clearStream(room.ID)

	if err := m.ensureWorker(ctx); err != nil {
		return nil, err
	}

	roomURL := strings.TrimSpace(room.SourceURL)
	if roomURL == "" {
		roomURL = "https://live.douyin.com/" +
			strings.TrimSpace(room.ExternalRoomID) +
			"?from=web_code_link"
	}
	if strings.TrimSpace(room.ExternalRoomID) == "" {
		return nil, errors.New("douyin room id is empty")
	}

	session := &roomSession{
		frames:    make(chan []byte, 512),
		errors:    make(chan error, 8),
		transport: make(chan struct{}),
		states:    make(chan roomStateSignal, 8),
	}

	m.mu.Lock()
	m.sessions[room.ID] = session
	m.mu.Unlock()

	if err := m.writeCommand(workerCommand{
		Op:     "start",
		RoomID: room.ID,
		URL:    roomURL,
	}); err != nil {
		m.mu.Lock()
		delete(m.sessions, room.ID)
		m.mu.Unlock()
		return nil, err
	}

	log.Printf(
		"collector room=%d worker=start source=%s",
		room.ID,
		roomURL,
	)

	return &BrowserSession{
		manager: m,
		roomID:  room.ID,
		session: session,
	}, nil
}

func (m *BrowserManager) RequestPreview(
	ctx context.Context,
	roomID int64,
) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	m.mu.Lock()
	_, active := m.sessions[roomID]
	worker := m.worker
	m.mu.Unlock()

	if !active {
		return nil, "", errors.New("room collector session is not active")
	}
	if worker == nil {
		return nil, "", ErrWorkerClosed
	}

	m.previewMu.Lock()
	m.previewSeq++
	requestID := fmt.Sprintf("%d-%d", roomID, m.previewSeq)
	waiter := make(chan previewResult, 1)
	m.previewWaiters[requestID] = waiter
	m.previewMu.Unlock()

	defer func() {
		m.previewMu.Lock()
		delete(m.previewWaiters, requestID)
		m.previewMu.Unlock()
	}()

	if err := m.writeCommand(workerCommand{
		Op:        "preview",
		RoomID:    roomID,
		RequestID: requestID,
	}); err != nil {
		return nil, "", err
	}

	select {
	case result := <-waiter:
		if result.err != nil {
			return nil, "", result.err
		}
		contentType := result.contentType
		if contentType == "" {
			contentType = "image/jpeg"
		}
		return result.data, contentType, nil
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-worker.done:
		if err := worker.getErr(); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrWorkerClosed, err)
		}
		return nil, "", ErrWorkerClosed
	}
}

func (m *BrowserManager) resolvePreview(
	requestID string,
	result previewResult,
) {
	if requestID == "" {
		return
	}

	m.previewMu.Lock()
	waiter := m.previewWaiters[requestID]
	m.previewMu.Unlock()

	if waiter == nil {
		return
	}

	select {
	case waiter <- result:
	default:
	}
}

func (m *BrowserManager) failPreviewWaiters(err error) {
	m.previewMu.Lock()
	waiters := make([]chan previewResult, 0, len(m.previewWaiters))
	for requestID, waiter := range m.previewWaiters {
		waiters = append(waiters, waiter)
		delete(m.previewWaiters, requestID)
	}
	m.previewMu.Unlock()

	for _, waiter := range waiters {
		select {
		case waiter <- previewResult{err: err}:
		default:
		}
	}
}
func (m *BrowserManager) Stream(roomID int64) (collector.StreamSource, error) {
	m.streamMu.RLock()
	source, ok := m.streams[roomID]
	m.streamMu.RUnlock()

	if !ok || strings.TrimSpace(source.URL) == "" {
		return collector.StreamSource{}, errors.New("live stream source is not available yet")
	}
	return source, nil
}

func (m *BrowserManager) setStreamCandidate(
	roomID int64,
	source collector.StreamSource,
) {
	if roomID <= 0 || strings.TrimSpace(source.URL) == "" {
		return
	}

	source.CapturedAt = time.Now().UTC()

	m.streamMu.Lock()
	current, exists := m.streams[roomID]
	if !exists || streamSourceScore(source) >= streamSourceScore(current) {
		m.streams[roomID] = source
	}
	m.streamMu.Unlock()
}

func streamSourceScore(source collector.StreamSource) int {
	score := 0
	protocol := strings.ToLower(strings.TrimSpace(source.Protocol))
	value := strings.ToLower(source.URL)

	switch protocol {
	case "hls":
		score += 100
	case "flv":
		score += 60
	default:
		score += 20
	}

	if strings.Contains(value, "biz_vcodec=h264") ||
		strings.Contains(value, "codec=h264") {
		score += 30
	}
	if strings.Contains(value, "biz_vcodec=h265") ||
		strings.Contains(value, "codec=h265") {
		score += 10
	}

	switch {
	case strings.Contains(value, "biz_quality=ld"):
		score += 12
	case strings.Contains(value, "biz_quality=sd"):
		score += 10
	case strings.Contains(value, "biz_quality=hd"):
		score += 8
	}

	return score
}
func (m *BrowserManager) StopRoom(roomID int64) {
	m.clearStream(roomID)
	m.mu.Lock()
	_, exists := m.sessions[roomID]
	delete(m.sessions, roomID)
	worker := m.worker
	m.mu.Unlock()

	if !exists || worker == nil {
		return
	}

	if err := m.writeCommand(workerCommand{
		Op:     "stop",
		RoomID: roomID,
	}); err != nil {
		log.Printf("collector room=%d worker=stop error=%v", roomID, err)
	}
}

func (m *BrowserManager) Close() {
	m.clearStreams()
	m.mu.Lock()
	worker := m.worker
	m.sessions = make(map[int64]*roomSession)
	m.mu.Unlock()

	if worker == nil {
		return
	}

	_ = m.writeCommand(workerCommand{Op: "shutdown"})

	select {
	case <-worker.done:
	case <-time.After(5 * time.Second):
		if worker.cmd.Process != nil {
			_ = worker.cmd.Process.Kill()
		}
	}
}

func (m *BrowserManager) ensureWorker(ctx context.Context) error {
	m.mu.Lock()
	worker := m.worker
	if worker == nil {
		var err error
		worker, err = m.startWorkerLocked()
		if err != nil {
			m.mu.Unlock()
			return err
		}
	}
	m.mu.Unlock()

	select {
	case <-worker.ready:
		return nil
	case <-worker.done:
		if err := worker.getErr(); err != nil {
			return fmt.Errorf("%w: %v", ErrWorkerClosed, err)
		}
		return ErrWorkerClosed
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(10 * time.Second):
		return errors.New("collector worker startup timeout")
	}
}

func (m *BrowserManager) startWorkerLocked() (*workerProcess, error) {
	nodePath, err := findNode()
	if err != nil {
		return nil, err
	}

	workerPath, err := findWorkerScript()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(nodePath, workerPath)
	cmd.Env = append(
		os.Environ(),
		"COLLECTOR_BROWSER_HEADLESS="+strconv.FormatBool(m.headless),
	)
	if m.configuredPath != "" {
		cmd.Env = append(
			cmd.Env,
			"COLLECTOR_BROWSER_PATH="+m.configuredPath,
		)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("collector worker stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("collector worker stdout: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("collector worker stderr: %w", err)
	}

	process := &workerProcess{
		cmd:   cmd,
		stdin: stdin,
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("start collector worker: %w", err)
	}

	m.worker = process

	go m.readWorkerStdout(process, stdout)
	go m.readWorkerStderr(stderr)
	go m.waitWorker(process)

	log.Printf(
		"collector worker started pid=%d node=%s script=%s",
		cmd.Process.Pid,
		nodePath,
		workerPath,
	)

	return process, nil
}

func (m *BrowserManager) readWorkerStdout(
	worker *workerProcess,
	reader io.Reader,
) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()

		var message workerMessage
		if err := json.Unmarshal(line, &message); err != nil {
			log.Printf("collector worker invalid json: %v", err)
			continue
		}

		switch message.Type {
		case "ready":
			worker.markReady()
			log.Printf("collector worker ready pid=%d", message.PID)

		case "room_started":
			log.Printf(
				"collector room=%d worker=room_started title=%q final_url=%s",
				message.RoomID,
				message.Title,
				message.FinalURL,
			)

		case "stream_candidate":
			m.setStreamCandidate(
				message.RoomID,
				collector.StreamSource{
					Protocol:     message.Protocol,
					URL:          message.URL,
					ContentType:  message.ContentType,
					ResourceType: message.ResourceType,
				},
			)
		case "transport_live":
			if session := m.getSession(message.RoomID); session != nil {
				session.markTransport()
			}

		case "room_state":
			if session := m.getSession(message.RoomID); session != nil {
				session.sendState(message.State, message.Reason)
			}
			log.Printf(
				"collector room=%d worker_state=%s reason=%s",
				message.RoomID,
				message.State,
				message.Reason,
			)

		case "frame":
			session := m.getSession(message.RoomID)
			if session == nil || message.Payload == "" {
				continue
			}

			raw, err := base64.StdEncoding.DecodeString(message.Payload)
			if err != nil || len(raw) == 0 {
				continue
			}

			select {
			case session.frames <- raw:
			default:
				log.Printf(
					"collector room=%d worker frame dropped: queue full",
					message.RoomID,
				)
			}

		case "preview_frame":
			raw, err := base64.StdEncoding.DecodeString(message.Payload)
			if err != nil {
				m.resolvePreview(
					message.RequestID,
					previewResult{err: fmt.Errorf("decode preview frame: %w", err)},
				)
				continue
			}
			m.resolvePreview(
				message.RequestID,
				previewResult{
					data:        raw,
					contentType: message.ContentType,
				},
			)

		case "preview_error":
			m.resolvePreview(
				message.RequestID,
				previewResult{err: errors.New(message.Error)},
			)

		case "room_error":
			m.sendRoomError(
				message.RoomID,
				errors.New(message.Error),
			)

		case "worker_error":
			log.Printf("collector worker error: %s", message.Error)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("collector worker stdout error: %v", err)
	}
}

func (m *BrowserManager) readWorkerStderr(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		log.Printf("%s", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.Printf("collector worker stderr error: %v", err)
	}
}

func (m *BrowserManager) waitWorker(worker *workerProcess) {
	err := worker.cmd.Wait()
	worker.setErr(err)
	close(worker.done)

	m.mu.Lock()
	if m.worker == worker {
		m.worker = nil
	}

	sessions := make([]*roomSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.sessions = make(map[int64]*roomSession)
	m.mu.Unlock()
	m.clearStreams()

	workerErr := ErrWorkerClosed
	if err != nil {
		workerErr = fmt.Errorf("%w: %v", ErrWorkerClosed, err)
	}

	for _, session := range sessions {
		select {
		case session.errors <- workerErr:
		default:
		}
	}

	m.failPreviewWaiters(workerErr)
	log.Printf("collector worker exited: %v", err)
}

func (m *BrowserManager) getSession(roomID int64) *roomSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[roomID]
}

func (m *BrowserManager) sendRoomError(roomID int64, err error) {
	session := m.getSession(roomID)
	if session == nil {
		return
	}

	select {
	case session.errors <- err:
	default:
	}
}

func (m *BrowserManager) writeCommand(command workerCommand) error {
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}

	m.mu.Lock()
	worker := m.worker
	m.mu.Unlock()

	if worker == nil || worker.stdin == nil {
		return ErrWorkerClosed
	}

	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	if _, err := worker.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("write collector worker command: %w", err)
	}

	return nil
}

func findNode() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("COLLECTOR_NODE_PATH")); configured != "" {
		if isFile(configured) {
			return configured, nil
		}
		return "", fmt.Errorf(
			"%w: COLLECTOR_NODE_PATH=%s",
			ErrBrowserUnavailable,
			configured,
		)
	}

	path, err := exec.LookPath("node")
	if err == nil {
		return path, nil
	}

	candidates := []string{}
	if runtime.GOOS == "windows" {
		candidates = append(
			candidates,
			`C:\Program Files\nodejs\node.exe`,
			filepath.Join(
				os.Getenv("LOCALAPPDATA"),
				"Programs",
				"nodejs",
				"node.exe",
			),
		)
	}

	for _, candidate := range candidates {
		if isFile(candidate) {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("%w: node executable not found", ErrBrowserUnavailable)
}

func findWorkerScript() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("COLLECTOR_WORKER_PATH")); configured != "" {
		if isFile(configured) {
			return filepath.Clean(configured), nil
		}
		return "", fmt.Errorf(
			"%w: COLLECTOR_WORKER_PATH=%s",
			ErrBrowserUnavailable,
			configured,
		)
	}

	candidates := []string{
		filepath.Join("collector-worker", "worker.mjs"),
	}

	if executable, err := os.Executable(); err == nil {
		executableDir := filepath.Dir(executable)
		candidates = append(
			candidates,
			filepath.Join(
				executableDir,
				"..",
				"..",
				"collector-worker",
				"worker.mjs",
			),
			filepath.Join(
				executableDir,
				"..",
				"collector-worker",
				"worker.mjs",
			),
		)
	}

	if workingDir, err := os.Getwd(); err == nil {
		candidates = append(
			candidates,
			filepath.Join(
				workingDir,
				"..",
				"collector-worker",
				"worker.mjs",
			),
		)
	}

	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if isFile(absolute) {
			return absolute, nil
		}
	}

	return "", fmt.Errorf(
		"%w: collector-worker/worker.mjs not found",
		ErrBrowserUnavailable,
	)
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
