package coreaudio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
)

type programState struct {
	ID               string
	RoomID           int64
	SessionID        string
	Label            string
	StartedAt        time.Time
	SegmentStartedAt time.Time
	CurrentTaskID    string
	CurrentSlot      string
	Sequence         uint64
	Running          bool
	Suspended        bool
	ResumeOffsetMS   int
}

type Client struct {
	mu sync.RWMutex

	hub       *audiohub.Hub
	publicURL string
	programs  map[int64]*programState
	sequence  atomic.Uint64
	now       func() time.Time

	testAudio      []byte
	testDurationMS int
	testVersion    string
	testPath       string
}

func New(hub *audiohub.Hub, publicURL, testWAVPath string) (*Client, error) {
	client := &Client{
		hub:       hub,
		publicURL: strings.TrimRight(strings.TrimSpace(publicURL), "/"),
		programs:  make(map[int64]*programState),
		now:       time.Now,
		testPath:  strings.TrimSpace(testWAVPath),
	}
	if client.publicURL == "" {
		client.publicURL = "http://127.0.0.1:8081"
	}
	if client.testPath == "" {
		return client, nil
	}
	audio, err := os.ReadFile(client.testPath)
	if err != nil {
		return client, fmt.Errorf("read core test WAV %q: %w", client.testPath, err)
	}
	durationMS, err := audiohub.WAVDurationMS(audio)
	if err != nil {
		return client, fmt.Errorf("inspect core test WAV %q: %w", client.testPath, err)
	}
	digest := sha256.Sum256(audio)
	client.testAudio = audio
	client.testDurationMS = durationMS
	client.testVersion = hex.EncodeToString(digest[:8])
	return client, nil
}

func (c *Client) Enabled() bool {
	return c != nil && c.hub != nil
}

func (c *Client) TestAudio() ([]byte, string, int, bool) {
	if c == nil || len(c.testAudio) == 0 || c.testDurationMS <= 0 {
		return nil, "", 0, false
	}
	return c.testAudio, c.testVersion, c.testDurationMS, true
}

func (c *Client) testAudioURL() string {
	if c.testVersion == "" {
		return c.publicURL + "/v1/test-audio.wav"
	}
	return c.publicURL + "/v1/test-audio.wav?v=" + c.testVersion
}

func hubTaskToAudioout(task audiohub.Task) audioout.SpeechTask {
	return audioout.SpeechTask{
		ID:         task.ID,
		RoomID:     task.RoomID,
		SessionID:  task.SessionID,
		Kind:       task.Kind,
		Label:      task.Label,
		AudioURL:   task.AudioURL,
		MimeType:   task.MimeType,
		DurationMS: task.DurationMS,
		StartMS:    task.StartMS,
		ProgramID:  task.ProgramID,
		Sequence:   task.Sequence,
		Slot:       task.Slot,
		StartedAt:  task.StartedAt,
		CreatedAt:  task.CreatedAt,
	}
}

func oppositeSlot(slot string) string {
	if slot == "A" {
		return "B"
	}
	return "A"
}

func (c *Client) CreateTestTask(_ context.Context, input audioout.CreateTestTaskInput) (audioout.SpeechTask, error) {
	if !c.Enabled() {
		return audioout.SpeechTask{}, errors.New("Core声音广播未启用")
	}
	if input.RoomID <= 0 || strings.TrimSpace(input.SessionID) == "" {
		return audioout.SpeechTask{}, errors.New("room_id and session_id are required")
	}
	if len(c.testAudio) == 0 || c.testDurationMS <= 0 {
		return audioout.SpeechTask{}, errors.New("Core测试WAV未配置")
	}
	now := c.now().UTC()
	durationMS := input.DurationMS
	if durationMS <= 0 || durationMS > c.testDurationMS {
		durationMS = c.testDurationMS
	}
	id := strings.TrimSpace(input.SpeechTaskID)
	if id == "" {
		id = fmt.Sprintf("core-test-%d-%d-%06d", input.RoomID, now.UnixMilli(), c.sequence.Add(1))
	}
	task, err := c.hub.Publish(audiohub.Task{
		ID:         id,
		RoomID:     input.RoomID,
		SessionID:  strings.TrimSpace(input.SessionID),
		Kind:       "test_wav",
		Label:      strings.TrimSpace(input.Label),
		AudioURL:   c.testAudioURL(),
		MimeType:   "audio/wav",
		DurationMS: durationMS,
		StartedAt:  now,
		CreatedAt:  now,
	})
	if err != nil {
		return audioout.SpeechTask{}, err
	}
	return hubTaskToAudioout(task), nil
}

func (c *Client) CreateExternalTask(ctx context.Context, input audioout.CreateExternalTaskInput) (audioout.SpeechTask, error) {
	if !c.Enabled() {
		return audioout.SpeechTask{}, errors.New("Core声音广播未启用")
	}
	task, err := c.hub.CreateExternalTask(
		ctx,
		input.RoomID,
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.Label),
		strings.TrimSpace(input.AudioURL),
	)
	if err != nil {
		return audioout.SpeechTask{}, err
	}
	return hubTaskToAudioout(task), nil
}

func (c *Client) StartTestProgram(_ context.Context, roomID int64, sessionID, label, _ string) (audioout.RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return audioout.RoomProgramSnapshot{}, errors.New("Core声音广播未启用")
	}
	if roomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	if len(c.testAudio) == 0 || c.testDurationMS <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("Core测试WAV未配置")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = fmt.Sprintf("core-room-%d", roomID)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Core 主线循环"
	}

	c.mu.Lock()
	if existing := c.programs[roomID]; existing != nil && existing.Running {
		snapshot := c.programSnapshotLocked(existing, c.now().UTC())
		c.mu.Unlock()
		return snapshot, nil
	}
	now := c.now().UTC()
	program := &programState{
		ID:               fmt.Sprintf("core-program-%d-%d-%06d", roomID, now.UnixMilli(), c.sequence.Add(1)),
		RoomID:           roomID,
		SessionID:        sessionID,
		Label:            label,
		StartedAt:        now,
		SegmentStartedAt: now,
		CurrentSlot:      "B",
		Running:          true,
	}
	c.programs[roomID] = program
	c.mu.Unlock()

	if _, err := c.publishMainline(program, 1, 0, now); err != nil {
		c.mu.Lock()
		if c.programs[roomID] == program {
			delete(c.programs, roomID)
		}
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, err
	}
	return c.ProgramSnapshot(context.Background(), roomID)
}

func (c *Client) publishMainline(program *programState, sequence uint64, startOffsetMS int, startedAt time.Time) (audioout.SpeechTask, error) {
	if program == nil || !program.Running {
		return audioout.SpeechTask{}, errors.New("room program is not running")
	}
	if startOffsetMS < 0 {
		startOffsetMS = 0
	}
	if startOffsetMS >= c.testDurationMS {
		startOffsetMS = 0
		startedAt = c.now().UTC()
	}
	slot := oppositeSlot(program.CurrentSlot)
	id := fmt.Sprintf("core-mainline-%d-%s-%06d", program.RoomID, program.ID, sequence)
	task, err := c.hub.Publish(audiohub.Task{
		ID:         id,
		RoomID:     program.RoomID,
		SessionID:  program.SessionID,
		Kind:       "test_wav_program",
		Label:      program.Label,
		AudioURL:   c.testAudioURL(),
		MimeType:   "audio/wav",
		DurationMS: c.testDurationMS,
		StartMS:    startOffsetMS,
		ProgramID:  program.ID,
		Sequence:   sequence,
		Slot:       slot,
		StartedAt:  startedAt.UTC(),
		CreatedAt:  c.now().UTC(),
	})
	if err != nil {
		return audioout.SpeechTask{}, err
	}

	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running {
		c.mu.Unlock()
		c.hub.Expire(task.ID)
		return audioout.SpeechTask{}, errors.New("room program stopped before mainline publish")
	}
	program.Sequence = sequence
	program.CurrentTaskID = task.ID
	program.CurrentSlot = task.Slot
	program.SegmentStartedAt = task.StartedAt
	program.Suspended = false
	program.ResumeOffsetMS = 0
	c.mu.Unlock()

	remainingMS := c.testDurationMS - startOffsetMS
	if remainingMS < 1 {
		remainingMS = 1
	}
	time.AfterFunc(time.Duration(remainingMS)*time.Millisecond, func() {
		c.advanceMainline(program, task.ID)
	})
	return hubTaskToAudioout(task), nil
}

func (c *Client) advanceMainline(program *programState, taskID string) {
	if program == nil {
		return
	}
	c.mu.RLock()
	current := c.programs[program.RoomID]
	if current != program || !program.Running || program.Suspended || program.CurrentTaskID != taskID {
		c.mu.RUnlock()
		return
	}
	nextSequence := program.Sequence + 1
	c.mu.RUnlock()

	c.hub.Expire(taskID)
	if _, err := c.publishMainline(program, nextSequence, 0, c.now().UTC()); err != nil {
		log.Printf("core audio mainline advance room=%d failed: %v", program.RoomID, err)
	}
}

func (c *Client) InsertTestProgramInteraction(ctx context.Context, input audioout.InsertInteractionInput) (audioout.RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return audioout.RoomProgramSnapshot{}, errors.New("Core声音广播未启用")
	}
	if input.RoomID <= 0 || strings.TrimSpace(input.AudioURL) == "" {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id and audio_url are required")
	}
	durationMS, mimeType, err := c.hub.ProbeExternalWAV(ctx, strings.TrimSpace(input.AudioURL))
	if err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}

	c.mu.RLock()
	program := c.programs[input.RoomID]
	if program == nil || !program.Running {
		c.mu.RUnlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room program is not running")
	}
	if program.Suspended {
		c.mu.RUnlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room program already has an active interaction")
	}
	currentTaskID := program.CurrentTaskID
	c.mu.RUnlock()

	currentSnapshot, ok := c.hub.Snapshot(currentTaskID)
	if !ok || currentSnapshot.Task.Kind != "test_wav_program" {
		return audioout.RoomProgramSnapshot{}, errors.New("current room output is not resumable mainline")
	}
	now := c.now().UTC()
	currentPositionMS := int(now.Sub(currentSnapshot.Task.StartedAt).Milliseconds())
	if currentPositionMS < 0 {
		currentPositionMS = 0
	}
	waitMS := 0
	if input.SwitchAtMS != nil {
		target := *input.SwitchAtMS
		if target < 0 || target >= c.testDurationMS {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms is outside current mainline")
		}
		if target <= currentPositionMS {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms already passed")
		}
		waitMS = target - currentPositionMS
		if waitMS > 35000 {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms is too far ahead")
		}
	}
	if waitMS > 0 {
		timer := time.NewTimer(time.Duration(waitMS) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return audioout.RoomProgramSnapshot{}, ctx.Err()
		case <-timer.C:
		}
	}

	now = c.now().UTC()
	c.mu.Lock()
	program = c.programs[input.RoomID]
	if program == nil || !program.Running || program.Suspended || program.CurrentTaskID != currentTaskID {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room mainline changed before safe switch point")
	}
	resumeMS := int(now.Sub(currentSnapshot.Task.StartedAt).Milliseconds())
	if input.SwitchAtMS != nil {
		resumeMS = *input.SwitchAtMS
	}
	if input.ResumeOffsetMS != nil {
		resumeMS = *input.ResumeOffsetMS
	}
	if resumeMS < 0 {
		resumeMS = 0
	}
	if resumeMS >= c.testDurationMS {
		resumeMS = c.testDurationMS - 1
	}
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" {
		sessionID = program.SessionID
	}
	label := strings.TrimSpace(input.Label)
	if label == "" {
		label = "实时互动 TTS"
	}
	sequence := program.Sequence + 1
	slot := oppositeSlot(program.CurrentSlot)
	interactionID := fmt.Sprintf("core-interaction-%d-%s-%06d", input.RoomID, program.ID, sequence)
	program.Suspended = true
	program.ResumeOffsetMS = resumeMS
	c.mu.Unlock()

	c.hub.Expire(currentTaskID)
	interaction, publishErr := c.hub.Publish(audiohub.Task{
		ID:         interactionID,
		RoomID:     input.RoomID,
		SessionID:  sessionID,
		Kind:       "interaction_tts",
		Label:      label,
		AudioURL:   strings.TrimSpace(input.AudioURL),
		MimeType:   mimeType,
		DurationMS: durationMS,
		ProgramID:  program.ID,
		Sequence:   sequence,
		Slot:       slot,
		StartedAt:  now,
		CreatedAt:  now,
	})
	if publishErr != nil {
		c.mu.Lock()
		if c.programs[input.RoomID] == program {
			program.Suspended = false
		}
		c.mu.Unlock()
		_, _ = c.publishMainline(program, sequence, resumeMS, now.Add(-time.Duration(resumeMS)*time.Millisecond))
		return audioout.RoomProgramSnapshot{}, publishErr
	}

	c.mu.Lock()
	if c.programs[input.RoomID] != program || !program.Running {
		c.mu.Unlock()
		c.hub.Expire(interaction.ID)
		return audioout.RoomProgramSnapshot{}, errors.New("room program stopped before interaction publish")
	}
	program.Sequence = sequence
	program.CurrentTaskID = interaction.ID
	program.CurrentSlot = interaction.Slot
	program.SegmentStartedAt = now
	c.mu.Unlock()

	time.AfterFunc(time.Duration(durationMS)*time.Millisecond, func() {
		c.resumeAfterInteraction(program, interaction.ID)
	})
	return c.ProgramSnapshot(context.Background(), input.RoomID)
}

func (c *Client) resumeAfterInteraction(program *programState, interactionTaskID string) {
	if program == nil {
		return
	}
	c.mu.Lock()
	current := c.programs[program.RoomID]
	if current != program || !program.Running || !program.Suspended || program.CurrentTaskID != interactionTaskID {
		c.mu.Unlock()
		return
	}
	resumeMS := program.ResumeOffsetMS
	nextSequence := program.Sequence + 1
	program.Suspended = false
	c.mu.Unlock()

	c.hub.Expire(interactionTaskID)
	now := c.now().UTC()
	if _, err := c.publishMainline(program, nextSequence, resumeMS, now.Add(-time.Duration(resumeMS)*time.Millisecond)); err != nil {
		log.Printf("core audio mainline resume room=%d failed: %v", program.RoomID, err)
	}
}

func (c *Client) ProgramSnapshot(_ context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	c.mu.RLock()
	program := c.programs[roomID]
	if program == nil {
		c.mu.RUnlock()
		return audioout.RoomProgramSnapshot{RoomID: roomID, ServerTime: c.now().UTC()}, nil
	}
	snapshot := c.programSnapshotLocked(program, c.now().UTC())
	c.mu.RUnlock()
	return snapshot, nil
}

func (c *Client) programSnapshotLocked(program *programState, now time.Time) audioout.RoomProgramSnapshot {
	snapshot := audioout.RoomProgramSnapshot{
		ProgramID:      program.ID,
		RoomID:         program.RoomID,
		Running:        program.Running,
		Suspended:      program.Suspended,
		ResumeOffsetMS: program.ResumeOffsetMS,
		Sequence:       program.Sequence,
		Slot:           program.CurrentSlot,
		StartedAt:      program.StartedAt,
		ServerTime:     now,
	}
	if state, ok := c.hub.Snapshot(program.CurrentTaskID); ok {
		task := hubTaskToAudioout(state.Task)
		if program.Running && !task.StartedAt.IsZero() && task.DurationMS > 0 {
			progress := int(now.Sub(task.StartedAt).Milliseconds())
			if progress < 0 {
				progress = 0
			}
			if progress >= task.DurationMS {
				progress = task.DurationMS - 1
			}
			task.StartMS = progress
		}
		snapshot.Task = &task
	}
	return snapshot
}

func (c *Client) StopTestProgram(_ context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	c.mu.Lock()
	program := c.programs[roomID]
	if program == nil {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{RoomID: roomID, ServerTime: c.now().UTC()}, nil
	}
	program.Running = false
	program.Suspended = false
	currentTaskID := program.CurrentTaskID
	snapshot := c.programSnapshotLocked(program, c.now().UTC())
	c.mu.Unlock()
	c.hub.Expire(currentTaskID)
	return snapshot, nil
}
