package audiohub

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultReceiverTTL = 45 * time.Second
	maxAudioProbeBytes = 32 << 20
)

type Task struct {
	ID         string    `json:"speech_task_id"`
	RoomID     int64     `json:"room_id"`
	SessionID  string    `json:"session_id"`
	Kind       string    `json:"kind"`
	Label      string    `json:"label"`
	AudioURL   string    `json:"audio_url"`
	MimeType   string    `json:"mime_type"`
	DurationMS int       `json:"duration_ms"`
	StartMS    int       `json:"start_ms,omitempty"`
	Sequence   uint64    `json:"sequence,omitempty"`
	ProgramID  string    `json:"program_id,omitempty"`
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

type ControlEvent struct {
	RoomID       int64     `json:"room_id"`
	Action       string    `json:"action"`
	SpeechTaskID string    `json:"speech_task_id,omitempty"`
	ProgramID    string    `json:"program_id,omitempty"`
	PositionMS   int       `json:"position_ms,omitempty"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type Receiver struct {
	ReceiverID   string    `json:"receiver_id"`
	RoomID       int64     `json:"room_id"`
	TerminalType string    `json:"terminal_type"`
	Name         string    `json:"name,omitempty"`
	Capabilities []string  `json:"capabilities,omitempty"`
	RegisteredAt time.Time `json:"registered_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	Online       bool      `json:"online"`
}

type TaskSnapshot struct {
	Task           Task                     `json:"task"`
	ReceiverEvents map[string]PlaybackEvent `json:"receiver_events"`
	Terminal       bool                     `json:"terminal"`
}

type Metrics struct {
	Tasks       int `json:"tasks"`
	ActiveRooms int `json:"active_rooms"`
	Subscribers int `json:"subscribers"`
	Receivers   int `json:"receivers"`
}

type taskState struct {
	task           Task
	receiverEvents map[string]PlaybackEvent
	terminal       bool
}

type Hub struct {
	mu          sync.RWMutex
	tasks       map[string]*taskState
	roomLatest  map[int64]string
	subscribers map[int64]map[chan Task]struct{}
	controls    map[int64]map[chan ControlEvent]struct{}
	receivers   map[string]*Receiver
	receiverTTL time.Duration
	sequence    atomic.Uint64
	httpClient  *http.Client
	now         func() time.Time
}

func New() *Hub {
	return &Hub{
		tasks:       make(map[string]*taskState),
		roomLatest:  make(map[int64]string),
		subscribers: make(map[int64]map[chan Task]struct{}),
		controls:    make(map[int64]map[chan ControlEvent]struct{}),
		receivers:   make(map[string]*Receiver),
		receiverTTL: defaultReceiverTTL,
		httpClient:  &http.Client{Timeout: 20 * time.Second},
		now:         time.Now,
	}
}

func (h *Hub) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	h.mu.Lock()
	h.httpClient = client
	h.mu.Unlock()
}

func normalizeCapabilities(values []string) []string {
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

func cloneReceiver(value *Receiver) Receiver {
	if value == nil {
		return Receiver{}
	}
	copy := *value
	copy.Capabilities = append([]string(nil), value.Capabilities...)
	return copy
}

func (h *Hub) pruneReceiversLocked(now time.Time) {
	ttl := h.receiverTTL
	if ttl <= 0 {
		ttl = defaultReceiverTTL
	}
	for id, receiver := range h.receivers {
		if receiver == nil || receiver.LastSeenAt.IsZero() || now.Sub(receiver.LastSeenAt) > ttl {
			delete(h.receivers, id)
		}
	}
}

func (h *Hub) RegisterReceiver(input Receiver) (Receiver, error) {
	input.ReceiverID = strings.TrimSpace(input.ReceiverID)
	input.TerminalType = strings.TrimSpace(input.TerminalType)
	input.Name = strings.TrimSpace(input.Name)
	if input.ReceiverID == "" || input.RoomID <= 0 {
		return Receiver{}, errors.New("receiver_id and room_id are required")
	}
	if len(input.ReceiverID) > 160 || len(input.TerminalType) > 80 || len(input.Name) > 160 {
		return Receiver{}, errors.New("receiver registration is too long")
	}
	now := h.now().UTC()
	input.Capabilities = normalizeCapabilities(input.Capabilities)
	input.RegisteredAt = now
	input.LastSeenAt = now
	input.Online = true

	h.mu.Lock()
	h.pruneReceiversLocked(now)
	if existing := h.receivers[input.ReceiverID]; existing != nil && existing.RoomID == input.RoomID {
		input.RegisteredAt = existing.RegisteredAt
	}
	copy := input
	h.receivers[input.ReceiverID] = &copy
	h.mu.Unlock()
	return cloneReceiver(&copy), nil
}

func (h *Hub) HeartbeatReceiver(receiverID string, roomID int64) (Receiver, error) {
	receiverID = strings.TrimSpace(receiverID)
	if receiverID == "" || roomID <= 0 {
		return Receiver{}, errors.New("receiver_id and room_id are required")
	}
	now := h.now().UTC()
	h.mu.Lock()
	h.pruneReceiversLocked(now)
	receiver := h.receivers[receiverID]
	if receiver == nil || receiver.RoomID != roomID {
		h.mu.Unlock()
		return Receiver{}, errors.New("receiver is not registered for room")
	}
	receiver.LastSeenAt = now
	receiver.Online = true
	copy := cloneReceiver(receiver)
	h.mu.Unlock()
	return copy, nil
}

func (h *Hub) UnregisterReceiver(receiverID string, roomID int64) bool {
	receiverID = strings.TrimSpace(receiverID)
	if receiverID == "" {
		return false
	}
	h.mu.Lock()
	receiver := h.receivers[receiverID]
	if receiver == nil || (roomID > 0 && receiver.RoomID != roomID) {
		h.mu.Unlock()
		return false
	}
	delete(h.receivers, receiverID)
	h.mu.Unlock()
	return true
}

func (h *Hub) Receiver(receiverID string) (Receiver, bool) {
	now := h.now().UTC()
	h.mu.Lock()
	h.pruneReceiversLocked(now)
	receiver := h.receivers[strings.TrimSpace(receiverID)]
	copy := cloneReceiver(receiver)
	h.mu.Unlock()
	return copy, receiver != nil
}

func (h *Hub) RoomReceivers(roomID int64) []Receiver {
	now := h.now().UTC()
	h.mu.Lock()
	h.pruneReceiversLocked(now)
	items := make([]Receiver, 0)
	for _, receiver := range h.receivers {
		if receiver != nil && receiver.RoomID == roomID {
			items = append(items, cloneReceiver(receiver))
		}
	}
	h.mu.Unlock()
	return items
}

func (h *Hub) CreateExternalTask(ctx context.Context, roomID int64, sessionID, label, audioURL string) (Task, error) {
	if roomID <= 0 {
		return Task{}, errors.New("room_id is required")
	}
	sessionID = strings.TrimSpace(sessionID)
	audioURL = strings.TrimSpace(audioURL)
	if sessionID == "" || audioURL == "" {
		return Task{}, errors.New("session_id and audio_url are required")
	}
	durationMS, mimeType, err := h.probeDuration(ctx, audioURL)
	if err != nil {
		return Task{}, err
	}
	now := h.now().UTC()
	sequence := h.sequence.Add(1)
	task := Task{
		ID:         fmt.Sprintf("core-audio-%d-%d-%06d", roomID, now.UnixMilli(), sequence),
		RoomID:     roomID,
		SessionID:  sessionID,
		Kind:       "interaction_tts",
		Label:      strings.TrimSpace(label),
		AudioURL:   audioURL,
		MimeType:   mimeType,
		DurationMS: durationMS,
		Sequence:   sequence,
		StartedAt:  now,
		CreatedAt:  now,
	}
	return h.Publish(task)
}

func (h *Hub) Publish(task Task) (Task, error) {
	if task.RoomID <= 0 || strings.TrimSpace(task.ID) == "" || strings.TrimSpace(task.AudioURL) == "" {
		return Task{}, errors.New("task room_id, speech_task_id and audio_url are required")
	}
	if task.DurationMS <= 0 {
		return Task{}, errors.New("duration_ms must be positive")
	}
	now := h.now().UTC()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	if strings.TrimSpace(task.MimeType) == "" {
		task.MimeType = "audio/wav"
	}
	if task.Sequence == 0 {
		task.Sequence = h.sequence.Add(1)
	}
	state := &taskState{
		task:           task,
		receiverEvents: make(map[string]PlaybackEvent),
	}

	h.mu.Lock()
	if previousID := h.roomLatest[task.RoomID]; previousID != "" {
		if previous := h.tasks[previousID]; previous != nil && !previous.terminal {
			h.mu.Unlock()
			return Task{}, errors.New("room already has an active audio task")
		}
	}
	h.tasks[task.ID] = state
	h.roomLatest[task.RoomID] = task.ID
	subs := make([]chan Task, 0, len(h.subscribers[task.RoomID]))
	for ch := range h.subscribers[task.RoomID] {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- task:
		default:
		}
	}

	delay := time.Until(task.StartedAt.Add(time.Duration(task.DurationMS) * time.Millisecond))
	if delay < 0 {
		delay = 0
	}
	time.AfterFunc(delay, func() {
		h.Expire(task.ID)
	})
	return task, nil
}

func (h *Hub) activeTaskLocked(roomID int64, now time.Time) *Task {
	id := h.roomLatest[roomID]
	if id == "" {
		return nil
	}
	state := h.tasks[id]
	if state == nil || state.terminal {
		delete(h.roomLatest, roomID)
		return nil
	}
	copy := state.task
	startMS := int(now.Sub(copy.StartedAt).Milliseconds())
	if startMS < 0 {
		startMS = 0
	}
	if startMS >= copy.DurationMS {
		state.terminal = true
		delete(h.roomLatest, roomID)
		return nil
	}
	copy.StartMS = startMS
	return &copy
}

func (h *Hub) Subscribe(roomID int64) (<-chan Task, *Task, func()) {
	ch := make(chan Task, 32)
	now := h.now().UTC()
	h.mu.Lock()
	if h.subscribers[roomID] == nil {
		h.subscribers[roomID] = make(map[chan Task]struct{})
	}
	h.subscribers[roomID][ch] = struct{}{}
	latest := h.activeTaskLocked(roomID, now)
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		roomSubs := h.subscribers[roomID]
		if _, exists := roomSubs[ch]; exists {
			delete(roomSubs, ch)
			close(ch)
		}
		if len(roomSubs) == 0 {
			delete(h.subscribers, roomID)
		}
		h.mu.Unlock()
	}
	return ch, latest, cancel
}

func (h *Hub) SubscribeControls(roomID int64) (<-chan ControlEvent, func()) {
	ch := make(chan ControlEvent, 32)
	h.mu.Lock()
	if h.controls[roomID] == nil {
		h.controls[roomID] = make(map[chan ControlEvent]struct{})
	}
	h.controls[roomID][ch] = struct{}{}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		roomSubs := h.controls[roomID]
		if _, exists := roomSubs[ch]; exists {
			delete(roomSubs, ch)
			close(ch)
		}
		if len(roomSubs) == 0 {
			delete(h.controls, roomID)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

func (h *Hub) BroadcastControl(event ControlEvent) {
	if event.RoomID <= 0 {
		return
	}
	event.Action = strings.ToLower(strings.TrimSpace(event.Action))
	if event.Action == "" {
		return
	}
	if event.PositionMS < 0 {
		event.PositionMS = 0
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = h.now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	h.mu.RLock()
	subs := make([]chan ControlEvent, 0, len(h.controls[event.RoomID]))
	for ch := range h.controls[event.RoomID] {
		subs = append(subs, ch)
	}
	h.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *Hub) Expire(taskID string) bool {
	h.mu.Lock()
	state := h.tasks[strings.TrimSpace(taskID)]
	if state == nil || state.terminal {
		h.mu.Unlock()
		return false
	}
	state.terminal = true
	if h.roomLatest[state.task.RoomID] == state.task.ID {
		delete(h.roomLatest, state.task.RoomID)
	}
	h.mu.Unlock()
	return true
}

func (h *Hub) ReportEvent(taskID string, event PlaybackEvent) error {
	h.mu.Lock()
	state := h.tasks[strings.TrimSpace(taskID)]
	if state == nil {
		h.mu.Unlock()
		return errors.New("audio task not found")
	}
	event.SpeechTaskID = state.task.ID
	event.RoomID = state.task.RoomID
	event.SessionID = state.task.SessionID
	event.ReceiverID = strings.TrimSpace(event.ReceiverID)
	event.Status = strings.ToUpper(strings.TrimSpace(event.Status))
	if event.OccurredAt.IsZero() {
		event.OccurredAt = h.now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	if event.ProgressMS < 0 {
		event.ProgressMS = 0
	}
	if event.ProgressMS > state.task.DurationMS {
		event.ProgressMS = state.task.DurationMS
	}
	state.receiverEvents[event.ReceiverID] = event
	h.mu.Unlock()
	return nil
}

func (h *Hub) Snapshot(taskID string) (TaskSnapshot, bool) {
	h.mu.RLock()
	state := h.tasks[strings.TrimSpace(taskID)]
	if state == nil {
		h.mu.RUnlock()
		return TaskSnapshot{}, false
	}
	snapshot := TaskSnapshot{
		Task:           state.task,
		ReceiverEvents: make(map[string]PlaybackEvent, len(state.receiverEvents)),
		Terminal:       state.terminal,
	}
	for id, event := range state.receiverEvents {
		snapshot.ReceiverEvents[id] = event
	}
	h.mu.RUnlock()
	return snapshot, true
}

func (h *Hub) ActiveTask(roomID int64) *Task {
	now := h.now().UTC()
	h.mu.Lock()
	latest := h.activeTaskLocked(roomID, now)
	h.mu.Unlock()
	return latest
}

func (h *Hub) Metrics() Metrics {
	now := h.now().UTC()
	h.mu.Lock()
	h.pruneReceiversLocked(now)
	metrics := Metrics{
		Tasks:       len(h.tasks),
		ActiveRooms: len(h.roomLatest),
		Receivers:   len(h.receivers),
	}
	for _, roomSubs := range h.subscribers {
		metrics.Subscribers += len(roomSubs)
	}
	for _, roomSubs := range h.controls {
		metrics.Subscribers += len(roomSubs)
	}
	h.mu.Unlock()
	return metrics
}

func (h *Hub) ProbeExternalWAV(ctx context.Context, audioURL string) (int, string, error) {
	return h.probeDuration(ctx, audioURL)
}

func WAVDurationMS(audio []byte) (int, error) {
	return wavDurationMS(audio)
}

func (h *Hub) probeDuration(ctx context.Context, audioURL string) (int, string, error) {
	h.mu.RLock()
	client := h.httpClient
	h.mu.RUnlock()
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
	if err != nil {
		return 0, "", fmt.Errorf("prepare audio probe: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("download audio metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, "", fmt.Errorf("audio source http %d", resp.StatusCode)
	}
	audio, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioProbeBytes+1))
	if err != nil {
		return 0, "", fmt.Errorf("read audio source: %w", err)
	}
	if len(audio) > maxAudioProbeBytes {
		return 0, "", errors.New("audio source is too large")
	}
	durationMS, err := wavDurationMS(audio)
	if err != nil {
		return 0, "", err
	}
	mimeType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = "audio/wav"
	}
	return durationMS, mimeType, nil
}

func wavDurationMS(audio []byte) (int, error) {
	if len(audio) < 12 || string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		return 0, errors.New("audio source is not a RIFF/WAVE file")
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
