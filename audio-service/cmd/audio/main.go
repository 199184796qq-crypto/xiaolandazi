package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultAddr         = "127.0.0.1:8082"
	defaultPublicURL    = "http://127.0.0.1:8082"
	defaultToken        = "local-audio-dev-token"
	defaultCoreToken    = "local-core-dev-token"
	maxBodyBytes        = 64 << 10
	defaultReceiverTTL  = 45 * time.Second
	defaultTestWAVPath  = `E:\直播伴播\测试素材\母带时间轴测试\mainline_same_tts.wav`
	schedulerReceiverID = "audio-service-scheduler"
)

type SpeechTask struct {
	ID         string    `json:"speech_task_id"`
	RoomID     int64     `json:"room_id"`
	SessionID  string    `json:"session_id"`
	Kind       string    `json:"kind"`
	Label      string    `json:"label"`
	AudioURL   string    `json:"audio_url"`
	MimeType   string    `json:"mime_type"`
	DurationMS int       `json:"duration_ms"`
	StartMS    int       `json:"start_ms,omitempty"`
	ProgramID  string    `json:"program_id,omitempty"`
	Sequence   uint64    `json:"sequence,omitempty"`
	Slot       string    `json:"slot,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type PlaybackEvent struct {
	SpeechTaskID string    `json:"speech_task_id"`
	RoomID       int64     `json:"room_id"`
	SessionID    string    `json:"session_id"`
	ReceiverID   string    `json:"receiver_id"`
	Status       string    `json:"status"`
	ProgressMS   int       `json:"progress_ms,omitempty"`
	Error        string    `json:"error,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type taskState struct {
	task              SpeechTask
	audio             []byte
	callbackURL       string
	referenceReceiver string
	receiverEvents    map[string]PlaybackEvent
	programManaged    bool
	terminal          bool
}

type roomProgram struct {
	ID               string
	RoomID           int64
	SessionID        string
	Label            string
	CallbackURL      string
	StartedAt        time.Time
	SegmentStartedAt time.Time
	CurrentTaskID    string
	Sequence         uint64
	Running          bool
	Suspended        bool
	ResumeOffsetMS   int
	cancel           context.CancelFunc
}

type RoomProgramSnapshot struct {
	ProgramID      string      `json:"program_id,omitempty"`
	RoomID         int64       `json:"room_id"`
	Running        bool        `json:"running"`
	Suspended      bool        `json:"suspended,omitempty"`
	ResumeOffsetMS int         `json:"resume_offset_ms,omitempty"`
	Sequence       uint64      `json:"sequence,omitempty"`
	Slot           string      `json:"slot,omitempty"`
	Task           *SpeechTask `json:"task,omitempty"`
	StartedAt      time.Time   `json:"started_at,omitempty"`
	ServerTime     time.Time   `json:"server_time"`
}

type TaskSnapshot struct {
	Task              SpeechTask               `json:"task"`
	ReferenceReceiver string                   `json:"reference_receiver,omitempty"`
	ReceiverEvents    map[string]PlaybackEvent `json:"receiver_events"`
	Terminal          bool                     `json:"terminal"`
}

type ReceiverRegistration struct {
	ReceiverID   string    `json:"receiver_id"`
	RoomID       int64     `json:"room_id"`
	TerminalType string    `json:"terminal_type"`
	Name         string    `json:"name,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	RegisteredAt time.Time `json:"registered_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Online       bool      `json:"online"`
}

type Broker struct {
	mu             sync.RWMutex
	tasks          map[string]*taskState
	roomLatest     map[int64]string
	programs       map[int64]*roomProgram
	subscribers    map[int64]map[chan SpeechTask]struct{}
	receivers      map[string]*ReceiverRegistration
	receiverTTL    time.Duration
	sequence       atomic.Uint64
	publicURL      string
	coreToken      string
	httpClient     *http.Client
	testAudio      []byte
	testDurationMS int
	testAudioPath  string
	testAudioVer   string
}

func NewBroker(publicURL, coreToken string) *Broker {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if publicURL == "" {
		publicURL = defaultPublicURL
	}
	return &Broker{
		tasks:       make(map[string]*taskState),
		roomLatest:  make(map[int64]string),
		programs:    make(map[int64]*roomProgram),
		subscribers: make(map[int64]map[chan SpeechTask]struct{}),
		receivers:   make(map[string]*ReceiverRegistration),
		receiverTTL: defaultReceiverTTL,
		publicURL:   publicURL,
		coreToken:   strings.TrimSpace(coreToken),
		httpClient:  &http.Client{Timeout: 2 * time.Second},
	}
}

func normalizeReceiverCapabilities(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 80 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) >= 16 {
			break
		}
	}
	return result
}

func (b *Broker) pruneStaleReceiversLocked(now time.Time) {
	ttl := b.receiverTTL
	if ttl <= 0 {
		ttl = defaultReceiverTTL
	}
	for id, receiver := range b.receivers {
		if receiver == nil || receiver.LastSeenAt.IsZero() || now.Sub(receiver.LastSeenAt) > ttl {
			delete(b.receivers, id)
		}
	}
}

func (b *Broker) RegisterReceiver(input ReceiverRegistration) (ReceiverRegistration, error) {
	input.ReceiverID = strings.TrimSpace(input.ReceiverID)
	input.TerminalType = strings.TrimSpace(input.TerminalType)
	input.Name = strings.TrimSpace(input.Name)
	if input.ReceiverID == "" || len(input.ReceiverID) > 160 {
		return ReceiverRegistration{}, errors.New("receiver_id is required")
	}
	if input.RoomID <= 0 {
		return ReceiverRegistration{}, errors.New("room_id must be positive")
	}
	if input.TerminalType == "" {
		input.TerminalType = "unknown"
	}
	if len(input.TerminalType) > 80 {
		return ReceiverRegistration{}, errors.New("terminal_type is too long")
	}
	if len(input.Name) > 160 {
		return ReceiverRegistration{}, errors.New("name is too long")
	}
	input.Capabilities = normalizeReceiverCapabilities(input.Capabilities)
	now := time.Now().UTC()
	input.RegisteredAt = now
	input.LastSeenAt = now
	input.Online = true

	b.mu.Lock()
	b.pruneStaleReceiversLocked(now)
	if existing := b.receivers[input.ReceiverID]; existing != nil && existing.RoomID == input.RoomID {
		input.RegisteredAt = existing.RegisteredAt
	}
	copy := input
	b.receivers[input.ReceiverID] = &copy
	b.mu.Unlock()
	return input, nil
}

func (b *Broker) HeartbeatReceiver(receiverID string, roomID int64) (ReceiverRegistration, error) {
	receiverID = strings.TrimSpace(receiverID)
	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStaleReceiversLocked(now)
	receiver := b.receivers[receiverID]
	if receiver == nil || receiver.RoomID != roomID {
		b.mu.Unlock()
		return ReceiverRegistration{}, errors.New("receiver is not registered for this room")
	}
	receiver.LastSeenAt = now
	receiver.Online = true
	copy := *receiver
	copy.Capabilities = append([]string(nil), receiver.Capabilities...)
	b.mu.Unlock()
	return copy, nil
}

func (b *Broker) UnregisterReceiver(receiverID string, roomID int64) error {
	receiverID = strings.TrimSpace(receiverID)
	b.mu.Lock()
	receiver := b.receivers[receiverID]
	if receiver == nil {
		b.mu.Unlock()
		return nil
	}
	if roomID > 0 && receiver.RoomID != roomID {
		b.mu.Unlock()
		return errors.New("receiver is registered to another room")
	}
	delete(b.receivers, receiverID)
	b.mu.Unlock()
	return nil
}

func (b *Broker) ReceiverForRoom(receiverID string, roomID int64) (ReceiverRegistration, bool) {
	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStaleReceiversLocked(now)
	receiver := b.receivers[strings.TrimSpace(receiverID)]
	if receiver == nil || receiver.RoomID != roomID {
		b.mu.Unlock()
		return ReceiverRegistration{}, false
	}
	copy := *receiver
	copy.Capabilities = append([]string(nil), receiver.Capabilities...)
	copy.Online = true
	b.mu.Unlock()
	return copy, true
}

func (b *Broker) ListReceivers(roomID int64) []ReceiverRegistration {
	now := time.Now().UTC()
	b.mu.Lock()
	b.pruneStaleReceiversLocked(now)
	result := make([]ReceiverRegistration, 0, len(b.receivers))
	for _, receiver := range b.receivers {
		if receiver == nil || (roomID > 0 && receiver.RoomID != roomID) {
			continue
		}
		copy := *receiver
		copy.Capabilities = append([]string(nil), receiver.Capabilities...)
		copy.Online = true
		result = append(result, copy)
	}
	b.mu.Unlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].RoomID == result[j].RoomID {
			return result[i].ReceiverID < result[j].ReceiverID
		}
		return result[i].RoomID < result[j].RoomID
	})
	return result
}

func (b *Broker) SubscribeRegistered(roomID int64, receiverID string) (<-chan SpeechTask, *SpeechTask, func(), error) {
	if _, ok := b.ReceiverForRoom(receiverID, roomID); !ok {
		return nil, nil, func() {}, errors.New("receiver is not registered for this room")
	}
	ch, latest, cancel := b.Subscribe(roomID)
	return ch, latest, cancel, nil
}

func NewBrokerWithTestAudio(publicURL, coreToken string, audio []byte, durationMS int, sourcePath string) (*Broker, error) {
	if len(audio) == 0 {
		return nil, errors.New("test audio is empty")
	}
	if durationMS <= 0 {
		return nil, errors.New("test audio duration must be positive")
	}
	broker := NewBroker(publicURL, coreToken)
	broker.testAudio = audio
	broker.testDurationMS = durationMS
	broker.testAudioPath = strings.TrimSpace(sourcePath)
	sum := sha256.Sum256(audio)
	broker.testAudioVer = fmt.Sprintf("%x", sum[:8])
	return broker, nil
}

func (b *Broker) CreateTestTask(roomID int64, sessionID, requestedID, label string, durationMS int, callbackURL string) (SpeechTask, error) {
	if roomID <= 0 {
		return SpeechTask{}, errors.New("room_id must be positive")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SpeechTask{}, errors.New("session_id is required")
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "房间播音通道 V1 测试音"
	}
	id := strings.TrimSpace(requestedID)
	if id == "" {
		id = fmt.Sprintf("sp-%d-%d-%d", roomID, time.Now().UnixMilli(), b.sequence.Add(1))
	}
	if len(id) > 160 {
		return SpeechTask{}, errors.New("speech_task_id is too long")
	}
	kind := "test_tone"
	var wav []byte
	if len(b.testAudio) > 0 {
		wav = b.testAudio
		durationMS = b.testDurationMS
		kind = "test_wav"
	} else {
		if durationMS <= 0 {
			durationMS = 2600
		}
		if durationMS < 700 {
			durationMS = 700
		}
		if durationMS > 10000 {
			durationMS = 10000
		}
		wav = testChimeWAV(durationMS)
	}
	now := time.Now().UTC()
	task := SpeechTask{
		ID:         id,
		RoomID:     roomID,
		SessionID:  sessionID,
		Kind:       kind,
		Label:      label,
		AudioURL:   b.publicURL + "/v1/tasks/" + url.PathEscape(id) + "/audio.wav",
		MimeType:   "audio/wav",
		DurationMS: durationMS,
		StartedAt:  now,
		CreatedAt:  now,
	}

	b.mu.Lock()
	if _, exists := b.tasks[id]; exists {
		b.mu.Unlock()
		return SpeechTask{}, errors.New("speech_task_id already exists")
	}
	b.tasks[id] = &taskState{
		task:           task,
		audio:          wav,
		callbackURL:    strings.TrimSpace(callbackURL),
		receiverEvents: make(map[string]PlaybackEvent),
	}
	b.roomLatest[roomID] = id
	subs := make([]chan SpeechTask, 0, len(b.subscribers[roomID]))
	for ch := range b.subscribers[roomID] {
		subs = append(subs, ch)
	}
	b.pruneLocked()
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- task:
		default:
			// A slow receiver must never block the room program or other receivers.
		}
	}
	b.scheduleTaskCompletion(task.ID)
	return task, nil
}

func (b *Broker) CreateExternalWAVTask(ctx context.Context, roomID int64, sessionID, label, audioURL, callbackURL string) (SpeechTask, error) {
	if roomID <= 0 {
		return SpeechTask{}, errors.New("room_id must be positive")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return SpeechTask{}, errors.New("session_id is required")
	}
	audio, durationMS, err := b.downloadExternalWAV(ctx, audioURL)
	if err != nil {
		return SpeechTask{}, err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "实时互动 TTS"
	}
	now := time.Now().UTC()
	id := fmt.Sprintf("interaction-%d-%d-%06d", roomID, now.UnixMilli(), b.sequence.Add(1))
	task := SpeechTask{
		ID:         id,
		RoomID:     roomID,
		SessionID:  sessionID,
		Kind:       "interaction_tts",
		Label:      label,
		AudioURL:   b.publicURL + "/v1/tasks/" + url.PathEscape(id) + "/audio.wav",
		MimeType:   "audio/wav",
		DurationMS: durationMS,
		StartedAt:  now,
		CreatedAt:  now,
	}
	b.mu.Lock()
	if oldID := b.roomLatest[roomID]; oldID != "" {
		if old := b.tasks[oldID]; old != nil && !old.terminal && old.task.Kind == "interaction_tts" {
			b.mu.Unlock()
			return SpeechTask{}, errors.New("room already has an active TTS interaction")
		}
	}
	b.tasks[id] = &taskState{
		task:           task,
		audio:          audio,
		callbackURL:    strings.TrimSpace(callbackURL),
		receiverEvents: make(map[string]PlaybackEvent),
	}
	b.roomLatest[roomID] = id
	subs := make([]chan SpeechTask, 0, len(b.subscribers[roomID]))
	for ch := range b.subscribers[roomID] {
		subs = append(subs, ch)
	}
	b.pruneLocked()
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- task:
		default:
		}
	}
	b.scheduleTaskCompletion(task.ID)
	return task, nil
}

func (b *Broker) createProgramTask(programID string, roomID int64, sessionID, label, callbackURL string, sequence uint64, startedAt time.Time) (SpeechTask, error) {
	if len(b.testAudio) == 0 || b.testDurationMS <= 0 {
		return SpeechTask{}, errors.New("test WAV is not configured")
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	} else {
		startedAt = startedAt.UTC()
	}
	slot := "A"
	if sequence%2 == 0 {
		slot = "B"
	}
	id := fmt.Sprintf("program-%d-%s-%06d", roomID, programID, sequence)
	startMS := int(time.Since(startedAt).Milliseconds())
	if startMS < 0 {
		startMS = 0
	}
	if startMS >= b.testDurationMS {
		startMS = max(0, b.testDurationMS-1)
	}
	task := SpeechTask{
		ID:         id,
		RoomID:     roomID,
		SessionID:  sessionID,
		Kind:       "test_wav_program",
		Label:      label,
		AudioURL:   b.publicURL + "/v1/test-audio.wav?v=" + b.testAudioVer,
		MimeType:   "audio/wav",
		DurationMS: b.testDurationMS,
		StartMS:    startMS,
		ProgramID:  programID,
		Sequence:   sequence,
		Slot:       slot,
		StartedAt:  startedAt,
		CreatedAt:  time.Now().UTC(),
	}

	b.mu.Lock()
	if oldID := b.roomLatest[roomID]; oldID != "" {
		if old := b.tasks[oldID]; old != nil && old.programManaged {
			old.terminal = true
		}
	}
	b.tasks[id] = &taskState{
		task:           task,
		audio:          b.testAudio,
		callbackURL:    strings.TrimSpace(callbackURL),
		receiverEvents: make(map[string]PlaybackEvent),
		programManaged: true,
	}
	b.roomLatest[roomID] = id
	subs := make([]chan SpeechTask, 0, len(b.subscribers[roomID]))
	for ch := range b.subscribers[roomID] {
		subs = append(subs, ch)
	}
	b.pruneLocked()
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- task:
		default:
		}
	}
	return task, nil
}

func (b *Broker) StartTestProgram(roomID int64, sessionID, label, callbackURL string) (RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return RoomProgramSnapshot{}, errors.New("room_id must be positive")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("dev-program-%d", roomID)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "房间连续声音测试"
	}

	b.mu.Lock()
	if existing := b.programs[roomID]; existing != nil && existing.Running {
		snapshot := b.programSnapshotLocked(existing, time.Now().UTC())
		b.mu.Unlock()
		return snapshot, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	programID := fmt.Sprintf("%d-%d", time.Now().UnixMilli(), b.sequence.Add(1))
	startedAt := time.Now().UTC()
	program := &roomProgram{
		ID:               programID,
		RoomID:           roomID,
		SessionID:        sessionID,
		Label:            label,
		CallbackURL:      strings.TrimSpace(callbackURL),
		StartedAt:        startedAt,
		SegmentStartedAt: startedAt,
		Sequence:         1,
		Running:          true,
		cancel:           cancel,
	}
	b.programs[roomID] = program
	b.mu.Unlock()

	task, err := b.createProgramTask(programID, roomID, sessionID, label, callbackURL, 1, startedAt)
	if err != nil {
		cancel()
		b.mu.Lock()
		delete(b.programs, roomID)
		b.mu.Unlock()
		return RoomProgramSnapshot{}, err
	}
	b.mu.Lock()
	if current := b.programs[roomID]; current == program && current.Running {
		current.CurrentTaskID = task.ID
	}
	snapshot := b.programSnapshotLocked(program, time.Now().UTC())
	b.mu.Unlock()

	go b.runTestProgram(ctx, program)
	return snapshot, nil
}

func (b *Broker) runTestProgram(ctx context.Context, program *roomProgram) {
	duration := time.Duration(b.testDurationMS) * time.Millisecond
	if duration <= 0 {
		return
	}
	for {
		b.mu.RLock()
		if !program.Running || program.Suspended {
			b.mu.RUnlock()
			return
		}
		sequence := program.Sequence
		segmentStartedAt := program.SegmentStartedAt
		b.mu.RUnlock()

		nextAt := segmentStartedAt.Add(duration)
		wait := time.Until(nextAt)
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		now := time.Now().UTC()
		elapsed := now.Sub(segmentStartedAt)
		advance := uint64(1)
		if elapsed > duration {
			advance = uint64(elapsed / duration)
			if advance == 0 {
				advance = 1
			}
		}
		targetSequence := sequence + advance
		nextStartedAt := segmentStartedAt.Add(time.Duration(advance) * duration)
		task, err := b.createProgramTask(program.ID, program.RoomID, program.SessionID, program.Label, program.CallbackURL, targetSequence, nextStartedAt)
		if err != nil {
			log.Printf("room program create segment room=%d sequence=%d failed: %v", program.RoomID, targetSequence, err)
			continue
		}
		b.mu.Lock()
		if current := b.programs[program.RoomID]; current == program && current.Running && !current.Suspended {
			current.Sequence = targetSequence
			current.SegmentStartedAt = nextStartedAt
			current.CurrentTaskID = task.ID
		} else if state := b.tasks[task.ID]; state != nil {
			state.terminal = true
		}
		b.mu.Unlock()
	}
}

func oppositeSlot(slot string) string {
	if slot == "A" {
		return "B"
	}
	return "A"
}

func (b *Broker) downloadExternalWAV(ctx context.Context, audioURL string) ([]byte, int, error) {
	audioURL = strings.TrimSpace(audioURL)
	parsed, err := url.Parse(audioURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, 0, errors.New("audio_url must be http or https")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, audioURL, nil)
	if err != nil {
		return nil, 0, err
	}
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("download external WAV: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("download external WAV http %d", resp.StatusCode)
	}
	const maxExternalWAV = 32 << 20
	audio, err := io.ReadAll(io.LimitReader(resp.Body, maxExternalWAV+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read external WAV: %w", err)
	}
	if len(audio) == 0 || len(audio) > maxExternalWAV {
		return nil, 0, errors.New("external WAV is empty or too large")
	}
	durationMS, err := wavDurationMS(audio)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect external WAV: %w", err)
	}
	return audio, durationMS, nil
}

func (b *Broker) InsertExternalWAV(
	ctx context.Context,
	roomID int64,
	sessionID, label, audioURL, callbackURL string,
	resumeOffsetMS *int,
	switchAtMS *int,
) (RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return RoomProgramSnapshot{}, errors.New("room_id must be positive")
	}
	audio, durationMS, err := b.downloadExternalWAV(ctx, audioURL)
	if err != nil {
		return RoomProgramSnapshot{}, err
	}
	now := time.Now().UTC()

	b.mu.Lock()
	program := b.programs[roomID]
	if program == nil || !program.Running {
		b.mu.Unlock()
		return RoomProgramSnapshot{}, errors.New("room program is not running")
	}
	if program.Suspended {
		b.mu.Unlock()
		return RoomProgramSnapshot{}, errors.New("room program already has an active interaction")
	}
	current := b.tasks[program.CurrentTaskID]
	if current == nil || current.task.Kind != "test_wav_program" {
		b.mu.Unlock()
		return RoomProgramSnapshot{}, errors.New("current room output is not resumable mainline")
	}

	currentTaskID := current.task.ID
	currentPositionMS := int(now.Sub(current.task.StartedAt).Milliseconds())
	if currentPositionMS < 0 {
		currentPositionMS = 0
	}
	waitMS := 0
	if switchAtMS != nil {
		target := *switchAtMS
		if target < 0 || target >= b.testDurationMS {
			b.mu.Unlock()
			return RoomProgramSnapshot{}, errors.New("switch_at_ms is outside current mainline")
		}
		if target <= currentPositionMS {
			b.mu.Unlock()
			return RoomProgramSnapshot{}, errors.New("switch_at_ms already passed")
		}
		waitMS = target - currentPositionMS
		if waitMS > 35000 {
			b.mu.Unlock()
			return RoomProgramSnapshot{}, errors.New("switch_at_ms is too far ahead")
		}
	}
	b.mu.Unlock()

	if waitMS > 0 {
		timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return RoomProgramSnapshot{}, ctx.Err()
		case <-timer.C:
		}
	}

	now = time.Now().UTC()
	b.mu.Lock()
	program = b.programs[roomID]
	if program == nil || !program.Running || program.Suspended || program.CurrentTaskID != currentTaskID {
		b.mu.Unlock()
		return RoomProgramSnapshot{}, errors.New("room mainline changed before safe switch point")
	}
	current = b.tasks[program.CurrentTaskID]
	if current == nil || current.task.Kind != "test_wav_program" {
		b.mu.Unlock()
		return RoomProgramSnapshot{}, errors.New("current room output is not resumable mainline")
	}

	resumeMS := int(now.Sub(current.task.StartedAt).Milliseconds())
	if switchAtMS != nil {
		resumeMS = *switchAtMS
	}
	if resumeOffsetMS != nil {
		resumeMS = *resumeOffsetMS
	}
	if resumeMS < 0 {
		resumeMS = 0
	}
	if resumeMS >= b.testDurationMS {
		resumeMS = max(0, b.testDurationMS-1)
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = program.SessionID
	}
	if strings.TrimSpace(label) == "" {
		label = "实时互动 TTS"
	}
	if strings.TrimSpace(callbackURL) == "" {
		callbackURL = program.CallbackURL
	}
	if program.cancel != nil {
		program.cancel()
	}
	program.Suspended = true
	program.ResumeOffsetMS = resumeMS
	sequence := program.Sequence + 1
	slot := oppositeSlot(current.task.Slot)
	taskID := fmt.Sprintf("interaction-%d-%s-%06d", roomID, program.ID, sequence)
	task := SpeechTask{
		ID:         taskID,
		RoomID:     roomID,
		SessionID:  sessionID,
		Kind:       "interaction_tts",
		Label:      label,
		AudioURL:   b.publicURL + "/v1/tasks/" + url.PathEscape(taskID) + "/audio.wav",
		MimeType:   "audio/wav",
		DurationMS: durationMS,
		ProgramID:  program.ID,
		Sequence:   sequence,
		Slot:       slot,
		StartedAt:  now,
		CreatedAt:  now,
	}
	current.terminal = true
	b.tasks[taskID] = &taskState{
		task:           task,
		audio:          audio,
		callbackURL:    strings.TrimSpace(callbackURL),
		receiverEvents: make(map[string]PlaybackEvent),
		programManaged: true,
	}
	b.roomLatest[roomID] = taskID
	program.Sequence = sequence
	program.SegmentStartedAt = now
	program.CurrentTaskID = taskID
	subs := make([]chan SpeechTask, 0, len(b.subscribers[roomID]))
	for ch := range b.subscribers[roomID] {
		subs = append(subs, ch)
	}
	b.pruneLocked()
	snapshot := b.programSnapshotLocked(program, now)
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- task:
		default:
		}
	}
	go b.resumeAfterInteraction(program, taskID, time.Duration(durationMS)*time.Millisecond, resumeMS)
	return snapshot, nil
}

func (b *Broker) resumeAfterInteraction(program *roomProgram, interactionTaskID string, duration time.Duration, resumeOffsetMS int) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	<-timer.C

	now := time.Now().UTC()
	b.completeTaskByClock(interactionTaskID, now)
	b.mu.RLock()
	current := b.programs[program.RoomID]
	canResume := current == program && current.Running && current.Suspended && current.CurrentTaskID == interactionTaskID
	sequence := current.Sequence + 1
	b.mu.RUnlock()
	if !canResume {
		return
	}

	resumeStartedAt := now.Add(-time.Duration(resumeOffsetMS) * time.Millisecond)
	task, err := b.createProgramTask(
		program.ID,
		program.RoomID,
		program.SessionID,
		program.Label,
		program.CallbackURL,
		sequence,
		resumeStartedAt,
	)
	if err != nil {
		log.Printf("resume room program room=%d after interaction failed: %v", program.RoomID, err)
		return
	}

	b.mu.Lock()
	current = b.programs[program.RoomID]
	if current != program || !current.Running || current.CurrentTaskID != interactionTaskID {
		if state := b.tasks[task.ID]; state != nil {
			state.terminal = true
		}
		b.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	current.Suspended = false
	current.ResumeOffsetMS = 0
	current.Sequence = sequence
	current.SegmentStartedAt = resumeStartedAt
	current.CurrentTaskID = task.ID
	current.cancel = cancel
	b.mu.Unlock()

	go b.runTestProgram(ctx, program)
}

func (b *Broker) StopTestProgram(roomID int64) (RoomProgramSnapshot, bool) {
	b.mu.Lock()
	program := b.programs[roomID]
	if program == nil {
		b.mu.Unlock()
		return RoomProgramSnapshot{RoomID: roomID, ServerTime: time.Now().UTC()}, false
	}
	program.Running = false
	if program.cancel != nil {
		program.cancel()
	}
	if state := b.tasks[program.CurrentTaskID]; state != nil {
		state.terminal = true
	}
	snapshot := b.programSnapshotLocked(program, time.Now().UTC())
	b.mu.Unlock()
	return snapshot, true
}

func (b *Broker) ProgramSnapshot(roomID int64) (RoomProgramSnapshot, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	program := b.programs[roomID]
	if program == nil {
		return RoomProgramSnapshot{RoomID: roomID, ServerTime: time.Now().UTC()}, false
	}
	return b.programSnapshotLocked(program, time.Now().UTC()), true
}

func (b *Broker) programSnapshotLocked(program *roomProgram, now time.Time) RoomProgramSnapshot {
	snapshot := RoomProgramSnapshot{
		ProgramID:      program.ID,
		RoomID:         program.RoomID,
		Running:        program.Running,
		Suspended:      program.Suspended,
		ResumeOffsetMS: program.ResumeOffsetMS,
		Sequence:       program.Sequence,
		StartedAt:      program.StartedAt,
		ServerTime:     now,
	}
	if state := b.tasks[program.CurrentTaskID]; state != nil {
		task := state.task
		if program.Running && !task.StartedAt.IsZero() {
			progress := int(now.Sub(task.StartedAt).Milliseconds())
			if progress < 0 {
				progress = 0
			}
			if progress >= task.DurationMS {
				progress = max(0, task.DurationMS-1)
			}
			task.StartMS = progress
		}
		snapshot.Task = &task
		snapshot.Slot = task.Slot
	}
	return snapshot
}

func (b *Broker) scheduleTaskCompletion(taskID string) {
	b.mu.RLock()
	state := b.tasks[taskID]
	if state == nil {
		b.mu.RUnlock()
		return
	}
	task := state.task
	b.mu.RUnlock()
	if task.DurationMS <= 0 {
		return
	}
	startedAt := task.StartedAt
	if startedAt.IsZero() {
		startedAt = task.CreatedAt
	}
	deadline := startedAt.Add(time.Duration(task.DurationMS) * time.Millisecond)
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		b.completeTaskByClock(taskID, time.Now().UTC())
	})
}

func (b *Broker) completeTaskByClock(taskID string, occurredAt time.Time) bool {
	b.mu.Lock()
	state := b.tasks[taskID]
	if state == nil || state.terminal {
		b.mu.Unlock()
		return false
	}
	state.terminal = true
	if b.roomLatest[state.task.RoomID] == taskID {
		delete(b.roomLatest, state.task.RoomID)
	}
	event := PlaybackEvent{
		SpeechTaskID: state.task.ID,
		RoomID:       state.task.RoomID,
		SessionID:    state.task.SessionID,
		ReceiverID:   schedulerReceiverID,
		Status:       "COMPLETED",
		ProgressMS:   state.task.DurationMS,
		OccurredAt:   occurredAt.UTC(),
	}
	state.receiverEvents[schedulerReceiverID] = event
	callbackURL := state.callbackURL
	b.mu.Unlock()
	if callbackURL != "" {
		b.postCoreEvent(callbackURL, event)
	}
	return true
}

func (b *Broker) pruneLocked() {
	if len(b.tasks) <= 512 {
		return
	}
	type candidate struct {
		id string
		at time.Time
	}
	items := make([]candidate, 0, len(b.tasks))
	latest := make(map[string]struct{}, len(b.roomLatest))
	for _, id := range b.roomLatest {
		latest[id] = struct{}{}
	}
	for id, state := range b.tasks {
		if _, keep := latest[id]; keep {
			continue
		}
		items = append(items, candidate{id: id, at: state.task.CreatedAt})
	}
	for len(b.tasks) > 384 && len(items) > 0 {
		oldest := 0
		for i := 1; i < len(items); i++ {
			if items[i].at.Before(items[oldest].at) {
				oldest = i
			}
		}
		delete(b.tasks, items[oldest].id)
		items = append(items[:oldest], items[oldest+1:]...)
	}
}

func (b *Broker) Subscribe(roomID int64) (<-chan SpeechTask, *SpeechTask, func()) {
	ch := make(chan SpeechTask, 8)
	b.mu.Lock()
	if b.subscribers[roomID] == nil {
		b.subscribers[roomID] = make(map[chan SpeechTask]struct{})
	}
	b.subscribers[roomID][ch] = struct{}{}
	var latest *SpeechTask
	if id := b.roomLatest[roomID]; id != "" {
		if state := b.tasks[id]; state != nil && !state.terminal {
			copy := state.task
			if !copy.StartedAt.IsZero() {
				copy.StartMS = int(time.Since(copy.StartedAt).Milliseconds())
				if copy.StartMS < 0 {
					copy.StartMS = 0
				}
				if copy.DurationMS > 0 && copy.StartMS >= copy.DurationMS {
					state.terminal = true
					if b.roomLatest[roomID] == id {
						delete(b.roomLatest, roomID)
					}
					copy.StartMS = copy.DurationMS
				}
			} else if state.referenceReceiver != "" {
				if event, ok := state.receiverEvents[state.referenceReceiver]; ok {
					copy.StartMS = event.ProgressMS
					if event.Status == "PLAYING" || event.Status == "PROGRESS" {
						copy.StartMS += int(time.Since(event.OccurredAt).Milliseconds())
					}
					if copy.StartMS < 0 {
						copy.StartMS = 0
					}
					if copy.StartMS >= copy.DurationMS {
						copy.StartMS = copy.DurationMS
					}
				}
			}
			if copy.StartMS < copy.DurationMS {
				latest = &copy
			}
		}
	}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if roomSubs := b.subscribers[roomID]; roomSubs != nil {
			if _, ok := roomSubs[ch]; ok {
				delete(roomSubs, ch)
				close(ch)
			}
			if len(roomSubs) == 0 {
				delete(b.subscribers, roomID)
			}
		}
		b.mu.Unlock()
	}
	return ch, latest, cancel
}

func (b *Broker) Audio(taskID string) ([]byte, SpeechTask, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	state := b.tasks[taskID]
	if state == nil {
		return nil, SpeechTask{}, false
	}
	return state.audio, state.task, true
}

func (b *Broker) Snapshot(taskID string) (TaskSnapshot, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	state := b.tasks[taskID]
	if state == nil {
		return TaskSnapshot{}, false
	}
	events := make(map[string]PlaybackEvent, len(state.receiverEvents))
	for id, event := range state.receiverEvents {
		events[id] = event
	}
	return TaskSnapshot{
		Task:              state.task,
		ReferenceReceiver: state.referenceReceiver,
		ReceiverEvents:    events,
		Terminal:          state.terminal,
	}, true
}

func validPlaybackStatus(status string) bool {
	switch status {
	case "READY", "PLAYING", "PROGRESS", "COMPLETED", "FAILED":
		return true
	default:
		return false
	}
}

func (b *Broker) ReportEvent(taskID string, input PlaybackEvent) (PlaybackEvent, bool, error) {
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	input.ReceiverID = strings.TrimSpace(input.ReceiverID)
	if input.ReceiverID == "" {
		return PlaybackEvent{}, false, errors.New("receiver_id is required")
	}
	if !validPlaybackStatus(input.Status) {
		return PlaybackEvent{}, false, errors.New("invalid playback status")
	}
	if input.ProgressMS < 0 {
		input.ProgressMS = 0
	}

	b.mu.Lock()
	state := b.tasks[taskID]
	if state == nil {
		b.mu.Unlock()
		return PlaybackEvent{}, false, errors.New("speech task not found")
	}
	input.SpeechTaskID = state.task.ID
	input.RoomID = state.task.RoomID
	input.SessionID = state.task.SessionID
	if input.OccurredAt.IsZero() {
		input.OccurredAt = time.Now().UTC()
	} else {
		input.OccurredAt = input.OccurredAt.UTC()
	}
	if state.referenceReceiver == "" {
		state.referenceReceiver = input.ReceiverID
	}
	state.receiverEvents[input.ReceiverID] = input
	isReference := state.referenceReceiver == input.ReceiverID
	if isReference && input.Status == "FAILED" {
		// Receiver feedback is health/observability only. A failed device may stop
		// being the reference for diagnostics, but it never controls task lifetime.
		state.referenceReceiver = ""
	}
	b.mu.Unlock()

	// Playback devices only report what happened locally. Task state and Core
	// progression are driven by the server clock, never by receiver callbacks.
	return input, isReference, nil
}

func (b *Broker) postCoreEvent(callbackURL string, event PlaybackEvent) {
	body, err := json.Marshal(event)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if b.coreToken != "" {
		req.Header.Set("X-Core-Token", b.coreToken)
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		log.Printf("audio callback task=%s status=%s failed: %v", event.SpeechTaskID, event.Status, err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("audio callback task=%s status=%s http=%d", event.SpeechTaskID, event.Status, resp.StatusCode)
	}
}

type server struct {
	broker        *Broker
	internalToken string
}

func newServer(broker *Broker, internalToken string) *server {
	return &server{broker: broker, internalToken: strings.TrimSpace(internalToken)}
}

func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /v1/receivers/register", s.registerReceiver)
	mux.HandleFunc("POST /v1/receivers/{receiverID}/heartbeat", s.heartbeatReceiver)
	mux.HandleFunc("POST /v1/receivers/{receiverID}/unregister", s.unregisterReceiver)
	mux.HandleFunc("GET /v1/rooms/{roomID}/receivers", s.roomReceivers)
	mux.Handle("POST /internal/v1/rooms/{roomID}/sessions/{sessionID}/tasks/test-tone", s.internal(http.HandlerFunc(s.createTestTask)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/sessions/{sessionID}/tasks/external-wav", s.internal(http.HandlerFunc(s.createExternalWAVTask)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/program/test-loop/start", s.internal(http.HandlerFunc(s.startTestProgram)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/program/test-loop/stop", s.internal(http.HandlerFunc(s.stopTestProgram)))
	mux.Handle("POST /internal/v1/rooms/{roomID}/program/test-loop/interaction", s.internal(http.HandlerFunc(s.insertTestProgramInteraction)))
	mux.HandleFunc("GET /v1/rooms/{roomID}/stream", s.roomStream)
	mux.HandleFunc("GET /v1/rooms/{roomID}/sync", s.roomSync)
	mux.HandleFunc("GET /v1/test-audio.wav", s.testAudio)
	mux.HandleFunc("GET /v1/tasks/{taskID}/audio.wav", s.taskAudio)
	mux.HandleFunc("GET /v1/tasks/{taskID}", s.taskState)
	mux.HandleFunc("POST /v1/tasks/{taskID}/events", s.taskEvent)
	return s.cors(mux)
}

func (s *server) internal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.internalToken == "" || r.Header.Get("X-Audio-Token") != s.internalToken {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) health(w http.ResponseWriter, _ *http.Request) {
	s.broker.mu.RLock()
	taskCount := len(s.broker.tasks)
	roomCount := len(s.broker.subscribers)
	receiverCount := 0
	for _, subs := range s.broker.subscribers {
		receiverCount += len(subs)
	}
	s.broker.mu.RUnlock()
	registeredReceivers := len(s.broker.ListReceivers(0))
	writeJSON(w, http.StatusOK, map[string]any{
		"service":              "audio-service",
		"status":               "ok",
		"time":                 time.Now().UTC(),
		"tasks":                taskCount,
		"subscribed_rooms":     roomCount,
		"connected_receivers":  receiverCount,
		"registered_receivers": registeredReceivers,
	})
}

func parsePositivePathInt(r *http.Request, name string) (int64, error) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(name)), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func (s *server) registerReceiver(w http.ResponseWriter, r *http.Request) {
	var input ReceiverRegistration
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	registration, err := s.broker.RegisterReceiver(input)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, registration)
}

func (s *server) heartbeatReceiver(w http.ResponseWriter, r *http.Request) {
	receiverID := strings.TrimSpace(r.PathValue("receiverID"))
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	registration, err := s.broker.HeartbeatReceiver(receiverID, input.RoomID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, registration)
}

func (s *server) unregisterReceiver(w http.ResponseWriter, r *http.Request) {
	receiverID := strings.TrimSpace(r.PathValue("receiverID"))
	var input struct {
		RoomID int64 `json:"room_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if err := s.broker.UnregisterReceiver(receiverID, input.RoomID); err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"unregistered": true})
}

func (s *server) roomReceivers(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": s.broker.ListReceivers(roomID)})
}

func (s *server) createTestTask(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	sessionID := strings.TrimSpace(r.PathValue("sessionID"))
	if sessionID == "" || len(sessionID) > 160 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session_id"})
		return
	}
	var input struct {
		SpeechTaskID string `json:"speech_task_id"`
		Label        string `json:"label"`
		DurationMS   int    `json:"duration_ms"`
		CallbackURL  string `json:"callback_url"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	task, err := s.broker.CreateTestTask(roomID, sessionID, input.SpeechTaskID, input.Label, input.DurationMS, input.CallbackURL)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *server) createExternalWAVTask(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	sessionID := strings.TrimSpace(r.PathValue("sessionID"))
	if sessionID == "" || len(sessionID) > 160 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid session_id"})
		return
	}
	var input struct {
		Label       string `json:"label"`
		AudioURL    string `json:"audio_url"`
		CallbackURL string `json:"callback_url"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(input.AudioURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "audio_url is required"})
		return
	}
	task, err := s.broker.CreateExternalWAVTask(r.Context(), roomID, sessionID, input.Label, input.AudioURL, input.CallbackURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, task)
}

func (s *server) startTestProgram(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var input struct {
		SessionID   string `json:"session_id"`
		Label       string `json:"label"`
		CallbackURL string `json:"callback_url"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	snapshot, err := s.broker.StartTestProgram(roomID, input.SessionID, input.Label, input.CallbackURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) stopTestProgram(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	snapshot, _ := s.broker.StopTestProgram(roomID)
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) insertTestProgramInteraction(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	var input struct {
		SessionID      string `json:"session_id"`
		Label          string `json:"label"`
		AudioURL       string `json:"audio_url"`
		CallbackURL    string `json:"callback_url"`
		ResumeOffsetMS *int   `json:"resume_offset_ms,omitempty"`
		SwitchAtMS     *int   `json:"switch_at_ms,omitempty"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if strings.TrimSpace(input.AudioURL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "audio_url is required"})
		return
	}
	snapshot, err := s.broker.InsertExternalWAV(
		r.Context(),
		roomID,
		input.SessionID,
		input.Label,
		input.AudioURL,
		input.CallbackURL,
		input.ResumeOffsetMS,
		input.SwitchAtMS,
	)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) roomSync(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	snapshot, _ := s.broker.ProgramSnapshot(roomID)
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) testAudio(w http.ResponseWriter, r *http.Request) {
	s.broker.mu.RLock()
	audio := s.broker.testAudio
	s.broker.mu.RUnlock()
	if len(audio) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "test audio is unavailable"})
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "test-audio.wav", time.Time{}, bytes.NewReader(audio))
}

func (s *server) roomStream(w http.ResponseWriter, r *http.Request) {
	roomID, err := parsePositivePathInt(r, "roomID")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	receiverID := strings.TrimSpace(r.URL.Query().Get("receiver_id"))
	if receiverID == "" || len(receiverID) > 160 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "receiver_id is required"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "streaming unsupported"})
		return
	}

	registration, registered := s.broker.ReceiverForRoom(receiverID, roomID)
	if !registered {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "receiver is not registered for this room"})
		return
	}
	ch, latest, cancel, err := s.broker.SubscribeRegistered(roomID, receiverID)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	writeSSE(w, "connected", registration)
	if latest != nil {
		writeSSE(w, "task", latest)
	}
	flusher.Flush()

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case task, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(w, "task", task)
			flusher.Flush()
		case <-keepAlive.C:
			if _, ok := s.broker.ReceiverForRoom(receiverID, roomID); !ok {
				writeSSE(w, "unregistered", map[string]any{"receiver_id": receiverID, "room_id": roomID})
				flusher.Flush()
				return
			}
			_, _ = io.WriteString(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w io.Writer, event string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
}

func (s *server) taskAudio(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	audio, task, ok := s.broker.Audio(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "speech task not found"})
		return
	}
	w.Header().Set("Content-Type", task.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(len(audio)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(audio)
}

func (s *server) taskState(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	snapshot, ok := s.broker.Snapshot(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "speech task not found"})
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *server) taskEvent(w http.ResponseWriter, r *http.Request) {
	taskID := strings.TrimSpace(r.PathValue("taskID"))
	var input PlaybackEvent
	if err := readJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	snapshot, ok := s.broker.Snapshot(taskID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "speech task not found"})
		return
	}
	if _, registered := s.broker.ReceiverForRoom(input.ReceiverID, snapshot.Task.RoomID); !registered {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "receiver is not registered for this room"})
		return
	}
	event, reference, err := s.broker.ReportEvent(taskID, input)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event": event, "room_reference": reference})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func testChimeWAV(durationMS int) []byte {
	const sampleRate = 24000
	const channels = 1
	const bitsPerSample = 16
	samples := sampleRate * durationMS / 1000
	dataSize := samples * channels * bitsPerSample / 8
	buf := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buf.WriteString("RIFF")
	_ = binary.Write(buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(buf, binary.LittleEndian, uint32(sampleRate*channels*bitsPerSample/8))
	_ = binary.Write(buf, binary.LittleEndian, uint16(channels*bitsPerSample/8))
	_ = binary.Write(buf, binary.LittleEndian, uint16(bitsPerSample))
	buf.WriteString("data")
	_ = binary.Write(buf, binary.LittleEndian, uint32(dataSize))

	for i := 0; i < samples; i++ {
		t := float64(i) / sampleRate
		progress := float64(i) / float64(samples)
		freq := 659.25
		switch {
		case progress > 0.68:
			freq = 783.99
		case progress > 0.36:
			freq = 880.00
		}
		envelope := 1.0
		if i < sampleRate/80 {
			envelope = float64(i) / float64(sampleRate/80)
		}
		if samples-i < sampleRate/60 {
			envelope *= float64(samples-i) / float64(sampleRate/60)
		}
		pulse := math.Mod(t, 0.5)
		if pulse > 0.38 {
			envelope *= 0.12
		}
		value := int16(math.Sin(2*math.Pi*freq*t) * 0.22 * 32767 * envelope)
		_ = binary.Write(buf, binary.LittleEndian, value)
	}
	return buf.Bytes()
}

func wavDurationMS(audio []byte) (int, error) {
	if len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return 0, errors.New("not a RIFF/WAVE file")
	}
	var byteRate uint32
	var dataSize uint32
	for offset := 12; offset+8 <= len(audio); {
		chunkID := string(audio[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(audio[offset+4 : offset+8])
		dataStart := offset + 8
		dataEnd := dataStart + int(chunkSize)
		streamingData := chunkID == "data" && dataEnd > len(audio)
		if dataEnd > len(audio) && !streamingData {
			return 0, errors.New("invalid WAV chunk size")
		}
		switch chunkID {
		case "fmt ":
			if chunkSize < 16 || dataStart+16 > len(audio) {
				return 0, errors.New("invalid WAV fmt chunk")
			}
			byteRate = binary.LittleEndian.Uint32(audio[dataStart+8 : dataStart+12])
		case "data":
			if streamingData {
				// Qwen Audio streaming WAVs keep a large placeholder data
				// chunk length and terminate the actual PCM payload at EOF.
				// Use the bytes that actually arrived for duration calculation.
				dataSize = uint32(len(audio) - dataStart)
				dataEnd = len(audio)
			} else {
				dataSize = chunkSize
			}
		}
		if byteRate > 0 && dataSize > 0 {
			break
		}
		offset = dataEnd
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if byteRate == 0 || dataSize == 0 {
		return 0, errors.New("WAV is missing fmt or data chunk")
	}
	durationMS := int((uint64(dataSize)*1000 + uint64(byteRate)/2) / uint64(byteRate))
	if durationMS <= 0 {
		return 0, errors.New("invalid WAV duration")
	}
	return durationMS, nil
}

func loadTestWAV(path string) ([]byte, int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, 0, errors.New("test WAV path is empty")
	}
	audio, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read test WAV %q: %w", path, err)
	}
	durationMS, err := wavDurationMS(audio)
	if err != nil {
		return nil, 0, fmt.Errorf("inspect test WAV %q: %w", path, err)
	}
	return audio, durationMS, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func main() {
	addr := envOrDefault("AUDIO_ADDR", defaultAddr)
	publicURL := envOrDefault("AUDIO_PUBLIC_URL", defaultPublicURL)
	audioToken := envOrDefault("AUDIO_INTERNAL_TOKEN", defaultToken)
	coreToken := envOrDefault("CORE_INTERNAL_TOKEN", defaultCoreToken)
	testWAVPath := envOrDefault("AUDIO_TEST_WAV_PATH", defaultTestWAVPath)

	testAudio, testDurationMS, err := loadTestWAV(testWAVPath)
	if err != nil {
		log.Fatalf("load configured test audio: %v", err)
	}
	broker, err := NewBrokerWithTestAudio(publicURL, coreToken, testAudio, testDurationMS, testWAVPath)
	if err != nil {
		log.Fatalf("configure test audio: %v", err)
	}
	api := newServer(broker, audioToken)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           api.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("audio-service listening on %s public_url=%s test_audio=%q test_duration_ms=%d", addr, publicURL, testWAVPath, testDurationMS)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
