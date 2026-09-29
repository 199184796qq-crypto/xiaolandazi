package coreaudio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

type programTrack struct {
	ID         string
	Label      string
	Kind       string
	Text       string
	AudioURL   string
	MimeType   string
	DurationMS int
	Timeline   []audioout.ProgramTimelineSegment
	SafePoints []audioout.ProgramSafePoint
}

type programState struct {
	ID                string
	RoomID            int64
	SessionID         string
	Label             string
	VersionID         int64
	VersionNo         int64
	Tracks            []programTrack
	TrackIndex        int
	StartedAt         time.Time
	SegmentStartedAt  time.Time
	CurrentTaskID     string
	CurrentSlot       string
	Sequence          uint64
	Running           bool
	Suspended         bool
	ManualPaused      bool
	ResumeOffsetMS    int
	PlannedNextTrack  int
	PlannedForTaskID  string
	PlannedNextScore  int
	PlannedNextReason string
	RecentTrackIDs    []string
	TrackPlayCount    map[string]int
	TrackLastPlayed   map[string]time.Time
	PrefetchedTrack   int
	PrefetchedAudio   []byte
}

type Client struct {
	mu sync.RWMutex

	hub             *audiohub.Hub
	roomAudio       *roomaudio.Engine
	publicURL       string
	programs        map[int64]*programState
	sequence        atomic.Uint64
	now             func() time.Time
	mainlineSignals MainlineAgentSignalProvider
	mirrorMu        sync.Mutex
	mirrors         map[int64]context.CancelFunc

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
		mirrors:   make(map[int64]context.CancelFunc),
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

func (c *Client) SetRoomAudioEngine(engine *roomaudio.Engine) {
	if c == nil {
		return
	}
	c.mirrorMu.Lock()
	c.roomAudio = engine
	c.mirrorMu.Unlock()
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

	return c.startPreparedProgram(roomID, sessionID, label, 0, 0, []programTrack{{
		ID:         "test-mainline",
		Label:      label,
		Kind:       "test_wav_program",
		AudioURL:   c.testAudioURL(),
		MimeType:   "audio/wav",
		DurationMS: c.testDurationMS,
	}})
}

func (c *Client) StartProgram(ctx context.Context, input audioout.StartProgramInput) (audioout.RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return audioout.RoomProgramSnapshot{}, errors.New("Core声音广播未启用")
	}
	if input.RoomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	if len(input.Tracks) == 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("program tracks are required")
	}
	if len(input.Tracks) > 20 {
		return audioout.RoomProgramSnapshot{}, errors.New("program tracks exceed limit")
	}
	prepared := make([]programTrack, 0, len(input.Tracks))
	for index, track := range input.Tracks {
		url := strings.TrimSpace(track.AudioURL)
		if url == "" {
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("track %d audio_url is required", index+1)
		}
		durationMS, mimeType, err := c.hub.ProbeExternalWAV(ctx, url)
		if err != nil {
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("probe track %d: %w", index+1, err)
		}
		id := strings.TrimSpace(track.ID)
		if id == "" {
			id = fmt.Sprintf("track-%d", index+1)
		}
		label := strings.TrimSpace(track.Label)
		if label == "" {
			label = id
		}
		timeline, err := normalizeProgramTimeline(track.Timeline, durationMS)
		if err != nil {
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("track %d timeline: %w", index+1, err)
		}
		safePoints, err := normalizeProgramSafePoints(track.SafePoints, timeline, durationMS)
		if err != nil {
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("track %d safe points: %w", index+1, err)
		}
		prepared = append(prepared, programTrack{
			ID:         id,
			Label:      label,
			Kind:       "mainline_program",
			Text:       strings.TrimSpace(track.Text),
			AudioURL:   url,
			MimeType:   mimeType,
			DurationMS: durationMS,
			Timeline:   timeline,
			SafePoints: safePoints,
		})
	}
	return c.startPreparedProgram(
		input.RoomID,
		strings.TrimSpace(input.SessionID),
		strings.TrimSpace(input.Label),
		input.VersionID,
		input.VersionNo,
		prepared,
	)
}

func normalizeProgramTimeline(input []audioout.ProgramTimelineSegment, durationMS int) ([]audioout.ProgramTimelineSegment, error) {
	if len(input) == 0 {
		return nil, nil
	}
	if len(input) > 1024 || durationMS <= 0 {
		return nil, errors.New("invalid timeline size or duration")
	}
	result := append([]audioout.ProgramTimelineSegment(nil), input...)
	previousEnd := 0
	for index := range result {
		segment := &result[index]
		segment.SegmentID = strings.TrimSpace(segment.SegmentID)
		segment.Text = strings.TrimSpace(segment.Text)
		if segment.SegmentID == "" || segment.Text == "" {
			return nil, errors.New("timeline segment id and text are required")
		}
		// Older published versions marked every generated chunk as a safe cut.
		// Runtime only accepts a real sentence/semantic ending so an interaction
		// never cuts after a comma or an arbitrary max-length chunk.
		segment.SafeCut = segment.SafeCut && strongSemanticEnding(segment.Text)
		if segment.Index != index+1 {
			return nil, errors.New("timeline index is not sequential")
		}
		if segment.StartMS < previousEnd || segment.StartMS < 0 || segment.EndMS <= segment.StartMS {
			return nil, errors.New("timeline overlaps or has an invalid time range")
		}
		if segment.EndMS > durationMS+5 {
			return nil, errors.New("timeline exceeds probed audio duration")
		}
		previousEnd = segment.EndMS
	}
	return result, nil
}

func strongSemanticEnding(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	runes := []rune(text)
	switch runes[len(runes)-1] {
	case '。', '！', '？', '!', '?', '；', ';':
		return true
	default:
		return false
	}
}

func normalizeProgramSafePoints(
	input []audioout.ProgramSafePoint,
	timeline []audioout.ProgramTimelineSegment,
	durationMS int,
) ([]audioout.ProgramSafePoint, error) {
	if durationMS <= 0 {
		return nil, errors.New("invalid audio duration")
	}
	if len(input) == 0 {
		fallback := make([]audioout.ProgramSafePoint, 0, len(timeline))
		for _, segment := range timeline {
			if !segment.SafeCut {
				continue
			}
			nextPreview := ""
			if segment.Index < len(timeline) {
				nextPreview = strings.TrimSpace(timeline[segment.Index].Text)
			}
			fallback = append(fallback, audioout.ProgramSafePoint{
				ID:          fmt.Sprintf("SP%03d", len(fallback)+1),
				CutMS:       segment.EndMS,
				Score:       90,
				Grade:       "B",
				Kind:        "SENTENCE",
				SentenceID:  segment.SegmentID,
				LeftPreview: strings.TrimSpace(segment.Text),
				NextPreview: nextPreview,
			})
		}
		return fallback, nil
	}
	if len(input) > 1024 {
		return nil, errors.New("safe point count exceeds limit")
	}
	result := append([]audioout.ProgramSafePoint(nil), input...)
	previousCut := 0
	segmentByID := make(map[string]audioout.ProgramTimelineSegment, len(timeline))
	for _, segment := range timeline {
		segmentByID[segment.SegmentID] = segment
	}
	for index := range result {
		point := &result[index]
		point.ID = strings.TrimSpace(point.ID)
		point.Grade = strings.ToUpper(strings.TrimSpace(point.Grade))
		point.Kind = strings.ToUpper(strings.TrimSpace(point.Kind))
		point.SentenceID = strings.TrimSpace(point.SentenceID)
		point.LeftPreview = strings.TrimSpace(point.LeftPreview)
		point.NextPreview = strings.TrimSpace(point.NextPreview)
		if point.ID == "" || point.SentenceID == "" || point.CutMS <= previousCut || point.CutMS > durationMS {
			return nil, errors.New("safe points are invalid or not strictly increasing")
		}
		if point.Score < 0 || point.Score > 100 {
			return nil, errors.New("safe point score is out of range")
		}
		if point.Grade != "A" && point.Grade != "B" && point.Grade != "C" {
			return nil, errors.New("safe point grade must be A, B, or C")
		}
		segment, ok := segmentByID[point.SentenceID]
		if !ok || segment.EndMS != point.CutMS || !strongSemanticEnding(segment.Text) {
			return nil, errors.New("safe point does not align to a semantic sentence boundary")
		}
		previousCut = point.CutMS
	}
	return result, nil
}

func (c *Client) startPreparedProgram(
	roomID int64,
	sessionID, label string,
	versionID, versionNo int64,
	tracks []programTrack,
) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 || len(tracks) == 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id and tracks are required")
	}
	if sessionID == "" {
		sessionID = fmt.Sprintf("core-room-%d", roomID)
	}
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
		VersionID:        versionID,
		VersionNo:        versionNo,
		Tracks:           append([]programTrack(nil), tracks...),
		TrackIndex:       0,
		StartedAt:        now,
		SegmentStartedAt: now,
		CurrentSlot:      "B",
		Running:          true,
		PlannedNextTrack: -1,
		PrefetchedTrack:  -1,
		TrackPlayCount:   make(map[string]int),
		TrackLastPlayed:  make(map[string]time.Time),
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
	if len(program.Tracks) == 0 {
		return audioout.SpeechTask{}, errors.New("room program has no tracks")
	}
	trackIndex := program.TrackIndex
	if trackIndex < 0 || trackIndex >= len(program.Tracks) {
		trackIndex = 0
	}
	track := program.Tracks[trackIndex]
	trackKind := strings.TrimSpace(track.Kind)
	if trackKind == "" {
		trackKind = "mainline_program"
	}
	if startOffsetMS < 0 {
		startOffsetMS = 0
	}
	if startOffsetMS >= track.DurationMS {
		startOffsetMS = 0
		startedAt = c.now().UTC()
	}
	slot := oppositeSlot(program.CurrentSlot)
	id := fmt.Sprintf("core-mainline-%d-%s-%06d", program.RoomID, program.ID, sequence)
	task, err := c.hub.Publish(audiohub.Task{
		ID:         id,
		RoomID:     program.RoomID,
		SessionID:  program.SessionID,
		Kind:       trackKind,
		Label:      track.Label,
		AudioURL:   track.AudioURL,
		MimeType:   track.MimeType,
		DurationMS: track.DurationMS,
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
	program.ManualPaused = false
	program.ResumeOffsetMS = 0
	if startOffsetMS == 0 {
		program.TrackPlayCount[track.ID]++
		program.TrackLastPlayed[track.ID] = c.now().UTC()
		program.RecentTrackIDs = append(program.RecentTrackIDs, track.ID)
		if len(program.RecentTrackIDs) > mainlineRecentHistoryN {
			program.RecentTrackIDs = append([]string(nil), program.RecentTrackIDs[len(program.RecentTrackIDs)-mainlineRecentHistoryN:]...)
		}
	} else {
		// Resuming after an interaction is still the same mainline selection.
		// Do not penalize it as a repeat, and drop the pre-interaction prefetch
		// so the Agent can re-plan against the latest room signals.
		program.PrefetchedTrack = -1
		program.PrefetchedAudio = nil
	}
	program.PlannedNextTrack = -1
	program.PlannedForTaskID = ""
	program.PlannedNextScore = 0
	program.PlannedNextReason = ""
	c.mu.Unlock()

	c.startRoomAudioMainlineMirror(program, trackIndex, track, startOffsetMS, task.ID)

	if c.roomAudio == nil {
		remainingMS := track.DurationMS - startOffsetMS
		if remainingMS < 1 {
			remainingMS = 1
		}
		time.AfterFunc(time.Duration(remainingMS)*time.Millisecond, func() {
			c.advanceMainline(program, task.ID)
		})
	}
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
	currentTrack := program.TrackIndex
	plannedTrack := program.PlannedNextTrack
	c.mu.RUnlock()

	nextTrack := plannedTrack
	if nextTrack < 0 || nextTrack >= len(program.Tracks) || (len(program.Tracks) > 1 && nextTrack == currentTrack) {
		nextTrack, _, _ = c.selectNextMainlineTrack(program, currentTrack)
	}
	if nextTrack < 0 || nextTrack >= len(program.Tracks) {
		nextTrack = currentTrack
	}

	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running || program.Suspended || program.CurrentTaskID != taskID {
		c.mu.Unlock()
		return
	}
	program.TrackIndex = nextTrack
	c.mu.Unlock()
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
	if c.roomAudio != nil {
		c.roomAudio.SetInterruptTimeline(input.RoomID, roomAudioInteractionTimeline(input.Text, durationMS))
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
	if !ok || (currentSnapshot.Task.Kind != "test_wav_program" && currentSnapshot.Task.Kind != "mainline_program") {
		return audioout.RoomProgramSnapshot{}, errors.New("current room output is not resumable mainline")
	}
	now := c.now().UTC()
	currentPositionMS := int(now.Sub(currentSnapshot.Task.StartedAt).Milliseconds())
	if currentPositionMS < 0 {
		currentPositionMS = 0
	}
	if c.roomAudio != nil {
		engineSnapshot := c.roomAudio.Snapshot(input.RoomID)
		if engineSnapshot.MainlineCursorMS > 0 {
			currentPositionMS = engineSnapshot.MainlineCursorMS
		}
	} else if receiverPositionMS, ok := activeReceiverPlaybackPosition(currentSnapshot); ok {
		currentPositionMS = receiverPositionMS
	}
	waitMS := 0
	if input.SwitchAtMS != nil {
		target := *input.SwitchAtMS
		if target < 0 || target >= currentSnapshot.Task.DurationMS {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms is outside current mainline")
		}
		if target <= currentPositionMS {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms already passed")
		}
		waitMS = target - currentPositionMS
		if waitMS > 35000 {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms is too far ahead")
		}
		if c.roomAudio != nil {
			cutSegmentID, resumeSegmentID := roomAudioCutSegments(program.Tracks[program.TrackIndex].Timeline, target)
			if _, prepareErr := c.roomAudio.PrepareInterrupt(input.RoomID); prepareErr == nil && cutSegmentID != "" {
				_, _ = c.roomAudio.ArmInterrupt(input.RoomID, cutSegmentID, resumeSegmentID)
			}
		}
	}
	if waitMS > 0 && input.SwitchAtMS != nil {
		target := *input.SwitchAtMS
		c.hub.BroadcastControl(audiohub.ControlEvent{
			RoomID:       input.RoomID,
			Action:       "prepare_switch",
			SpeechTaskID: currentTaskID,
			ProgramID:    program.ID,
			PositionMS:   target,
			OccurredAt:   c.now().UTC(),
		})
		var waitErr error
		if c.roomAudio != nil {
			waitErr = c.waitForRoomAudioSwitchPoint(ctx, input.RoomID, target, time.Duration(waitMS)*time.Millisecond)
		} else {
			waitErr = c.waitForReceiverSwitchPoint(ctx, currentTaskID, target, time.Duration(waitMS)*time.Millisecond)
		}
		if waitErr != nil {
			return audioout.RoomProgramSnapshot{}, waitErr
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
	if resumeMS > currentSnapshot.Task.DurationMS {
		resumeMS = currentSnapshot.Task.DurationMS
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
	program.ManualPaused = false
	program.ResumeOffsetMS = resumeMS
	c.mu.Unlock()

	c.cancelRoomAudioMirror(input.RoomID)
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
			resumeMS = prepareProgramResumeLocked(program, resumeMS)
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

	if c.roomAudio != nil {
		if _, err := c.roomAudio.StartInterrupt(input.RoomID); err == nil {
			c.startRoomAudioInterruptMirror(input.RoomID, strings.TrimSpace(input.AudioURL), interaction.ID)
		} else {
			log.Printf("room audio interrupt phase room=%d task=%s failed: %v", input.RoomID, interaction.ID, err)
		}
	}

	log.Printf(
		"core audio interaction start room=%d task=%s resume_ms=%d duration_ms=%d",
		input.RoomID, interaction.ID, resumeMS, durationMS,
	)
	// Receiver COMPLETED is authoritative. The delayed timer is only a
	// fallback for receivers that disappear without sending a terminal event.
	time.AfterFunc(time.Duration(durationMS+4000)*time.Millisecond, func() {
		c.resumeAfterInteraction(program, interaction.ID)
	})
	return c.ProgramSnapshot(context.Background(), input.RoomID)
}

func (c *Client) waitForReceiverSwitchPoint(ctx context.Context, taskID string, targetMS int, wallClockWait time.Duration) error {
	if targetMS <= 0 || wallClockWait <= 0 {
		return nil
	}
	snapshot, ok := c.hub.Snapshot(taskID)
	if !ok || !hasActivePlaybackReceiver(snapshot) {
		timer := time.NewTimer(wallClockWait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}

	// Once a receiver is actually playing, its media cursor is authoritative.
	// The task wall clock can run ahead during browser/device buffering, which
	// previously cut the highlighted final sentence before it was fully heard.
	deadline := time.NewTimer(wallClockWait + 3*time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if snapshot, ok := c.hub.Snapshot(taskID); ok && receiversReachedPlaybackPosition(snapshot, targetMS) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			// Backward-compatible fallback for a receiver that reports PLAYING but
			// does not yet acknowledge prepare_switch/progress.
			return nil
		case <-ticker.C:
		}
	}
}

func (c *Client) waitForRoomAudioSwitchPoint(ctx context.Context, roomID int64, targetMS int, wallClockWait time.Duration) error {
	if c.roomAudio == nil || roomID <= 0 || targetMS <= 0 {
		return nil
	}
	deadline := time.NewTimer(wallClockWait + 3*time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if c.roomAudio.Snapshot(roomID).MainlineCursorMS >= targetMS {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return nil
		case <-ticker.C:
		}
	}
}

func hasActivePlaybackReceiver(snapshot audiohub.TaskSnapshot) bool {
	for _, event := range snapshot.ReceiverEvents {
		switch strings.ToUpper(strings.TrimSpace(event.Status)) {
		case "PLAYING", "PROGRESS":
			return true
		}
	}
	return false
}

func activeReceiverPlaybackPosition(snapshot audiohub.TaskSnapshot) (int, bool) {
	positionMS := 0
	found := false
	for _, event := range snapshot.ReceiverEvents {
		switch strings.ToUpper(strings.TrimSpace(event.Status)) {
		case "PLAYING", "PROGRESS":
			if !found || event.ProgressMS < positionMS {
				positionMS = event.ProgressMS
			}
			found = true
		}
	}
	return positionMS, found
}

func receiversReachedPlaybackPosition(snapshot audiohub.TaskSnapshot, targetMS int) bool {
	receivers := 0
	for _, event := range snapshot.ReceiverEvents {
		switch strings.ToUpper(strings.TrimSpace(event.Status)) {
		case "PLAYING", "PROGRESS":
			receivers++
			if event.ProgressMS < targetMS {
				return false
			}
		case "COMPLETED":
			receivers++
		}
	}
	return receivers > 0
}

func (c *Client) CompleteProgramInteraction(_ context.Context, roomID int64, interactionTaskID string) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 || strings.TrimSpace(interactionTaskID) == "" {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id and interaction_task_id are required")
	}
	c.mu.RLock()
	program := c.programs[roomID]
	c.mu.RUnlock()
	if program == nil || !program.Running {
		return audioout.RoomProgramSnapshot{}, errors.New("room program is not running")
	}
	c.resumeAfterInteraction(program, strings.TrimSpace(interactionTaskID))
	return c.ProgramSnapshot(context.Background(), roomID)
}

func (c *Client) resumeAfterInteraction(program *programState, interactionTaskID string) {
	if program == nil {
		return
	}
	c.mu.Lock()
	current := c.programs[program.RoomID]
	if current != program || !program.Running || !program.Suspended || program.ManualPaused || program.CurrentTaskID != interactionTaskID {
		c.mu.Unlock()
		return
	}
	resumeMS := program.ResumeOffsetMS
	nextSequence := program.Sequence + 1
	program.Suspended = false
	resumeMS = prepareProgramResumeLocked(program, resumeMS)
	resumeSegmentID := ""
	if program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		resumeSegmentID = roomAudioSegmentAt(program.Tracks[program.TrackIndex].Timeline, resumeMS)
	}
	c.mu.Unlock()

	c.hub.Expire(interactionTaskID)
	if c.roomAudio != nil {
		if _, err := c.roomAudio.PrepareResume(program.RoomID, resumeSegmentID); err == nil {
			_, _ = c.roomAudio.StartResume(program.RoomID, resumeSegmentID)
		}
	}
	now := c.now().UTC()
	if _, err := c.publishMainline(program, nextSequence, resumeMS, now.Add(-time.Duration(resumeMS)*time.Millisecond)); err != nil {
		log.Printf("core audio mainline resume room=%d failed: %v", program.RoomID, err)
	} else {
		log.Printf(
			"core audio mainline resume room=%d interaction=%s resume_ms=%d sequence=%d",
			program.RoomID, interactionTaskID, resumeMS, nextSequence,
		)
	}
}

func (c *Client) PauseProgram(_ context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	now := c.now().UTC()
	c.mu.Lock()
	program := c.programs[roomID]
	if program == nil || !program.Running {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room program is not running")
	}
	if program.ManualPaused {
		snapshot := c.programSnapshotLocked(program, now)
		c.mu.Unlock()
		return snapshot, nil
	}
	currentTaskID := program.CurrentTaskID
	resumeMS := program.ResumeOffsetMS
	if !program.Suspended {
		if state, ok := c.hub.Snapshot(currentTaskID); ok {
			task := state.Task
			if task.Kind == "mainline_program" || task.Kind == "test_wav_program" {
				resumeMS = int(now.Sub(task.StartedAt).Milliseconds())
				if resumeMS < 0 {
					resumeMS = 0
				}
				if task.DurationMS > 0 && resumeMS > task.DurationMS {
					resumeMS = task.DurationMS
				}
			}
		}
	}
	program.ResumeOffsetMS = resumeMS
	program.Suspended = true
	program.ManualPaused = true
	snapshot := c.programSnapshotLocked(program, now)
	c.mu.Unlock()
	if currentTaskID != "" {
		c.hub.Expire(currentTaskID)
	}
	c.cancelRoomAudioMirror(roomID)
	if c.roomAudio != nil {
		_, _ = c.roomAudio.Pause(roomID)
	}
	c.hub.BroadcastControl(audiohub.ControlEvent{
		RoomID:       roomID,
		Action:       "pause",
		SpeechTaskID: currentTaskID,
		ProgramID:    program.ID,
		PositionMS:   resumeMS,
		OccurredAt:   now,
	})
	return snapshot, nil
}

func (c *Client) ResumeProgram(_ context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	if roomID <= 0 {
		return audioout.RoomProgramSnapshot{}, errors.New("room_id is required")
	}
	c.mu.Lock()
	program := c.programs[roomID]
	if program == nil || !program.Running {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room program is not running")
	}
	if !program.ManualPaused {
		if program.Suspended {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, errors.New("room program is suspended by an active interaction")
		}
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		return snapshot, nil
	}
	resumeMS := prepareProgramResumeLocked(program, program.ResumeOffsetMS)
	nextSequence := program.Sequence + 1
	c.mu.Unlock()

	now := c.now().UTC()
	if _, err := c.publishMainline(
		program,
		nextSequence,
		resumeMS,
		now.Add(-time.Duration(resumeMS)*time.Millisecond),
	); err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	return c.ProgramSnapshot(context.Background(), roomID)
}

func (c *Client) StopProgram(ctx context.Context, roomID int64) (audioout.RoomProgramSnapshot, error) {
	return c.StopTestProgram(ctx, roomID)
}

func prepareProgramResumeLocked(program *programState, resumeMS int) int {
	if program == nil {
		return 0
	}
	if resumeMS < 0 {
		resumeMS = 0
	}
	if len(program.Tracks) == 0 || program.TrackIndex < 0 || program.TrackIndex >= len(program.Tracks) {
		return resumeMS
	}
	durationMS := program.Tracks[program.TrackIndex].DurationMS
	if durationMS > 0 && resumeMS >= durationMS {
		program.TrackIndex++
		if program.TrackIndex >= len(program.Tracks) {
			program.TrackIndex = 0
		}
		return 0
	}
	return resumeMS
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
	trackID := ""
	trackText := ""
	var timeline []audioout.ProgramTimelineSegment
	var safePoints []audioout.ProgramSafePoint
	if program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		track := program.Tracks[program.TrackIndex]
		trackID = track.ID
		trackText = track.Text
		timeline = append([]audioout.ProgramTimelineSegment(nil), track.Timeline...)
		safePoints = append([]audioout.ProgramSafePoint(nil), track.SafePoints...)
	}
	snapshot := audioout.RoomProgramSnapshot{
		ProgramID:         program.ID,
		RoomID:            program.RoomID,
		VersionID:         program.VersionID,
		VersionNo:         program.VersionNo,
		TrackID:           trackID,
		TrackIndex:        program.TrackIndex,
		TrackCount:        len(program.Tracks),
		TrackText:         trackText,
		Timeline:          timeline,
		SafePoints:        safePoints,
		Running:           program.Running,
		Suspended:         program.Suspended,
		ResumeOffsetMS:    program.ResumeOffsetMS,
		PlannedNextScore:  program.PlannedNextScore,
		PlannedNextReason: program.PlannedNextReason,
		Sequence:          program.Sequence,
		Slot:              program.CurrentSlot,
		StartedAt:         program.StartedAt,
		ServerTime:        now,
	}
	if program.PlannedNextTrack >= 0 && program.PlannedNextTrack < len(program.Tracks) {
		snapshot.PlannedNextTrackID = program.Tracks[program.PlannedNextTrack].ID
	}
	currentMS := 0
	if program.Suspended {
		currentMS = program.ResumeOffsetMS
	}
	if state, ok := c.hub.Snapshot(program.CurrentTaskID); ok {
		task := hubTaskToAudioout(state.Task)
		if program.Running && !program.Suspended && task.Kind != "interaction_tts" && !task.StartedAt.IsZero() && task.DurationMS > 0 {
			progress := int(now.Sub(task.StartedAt).Milliseconds())
			if progress < 0 {
				progress = 0
			}
			if progress >= task.DurationMS {
				progress = task.DurationMS - 1
			}
			if receiverPositionMS, ok := activeReceiverPlaybackPosition(state); ok {
				progress = receiverPositionMS
				if progress >= task.DurationMS {
					progress = task.DurationMS - 1
				}
			}
			task.StartMS = progress
			currentMS = progress
		}
		snapshot.Task = &task
	}
	snapshot.CurrentMS = currentMS
	for index := range timeline {
		segment := timeline[index]
		if currentMS >= segment.StartMS && currentMS < segment.EndMS {
			copy := segment
			snapshot.CurrentSegment = &copy
			break
		}
	}
	for _, point := range safePoints {
		if point.CutMS > currentMS {
			snapshot.NextSafeCutMS = point.CutMS
			break
		}
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
	program.ManualPaused = false
	currentTaskID := program.CurrentTaskID
	snapshot := c.programSnapshotLocked(program, c.now().UTC())
	c.mu.Unlock()
	c.cancelRoomAudioMirror(roomID)
	if c.roomAudio != nil {
		c.roomAudio.Reset(roomID)
	}
	c.hub.Expire(currentTaskID)
	c.hub.BroadcastControl(audiohub.ControlEvent{
		RoomID:       roomID,
		Action:       "stop",
		SpeechTaskID: currentTaskID,
		ProgramID:    program.ID,
		OccurredAt:   c.now().UTC(),
	})
	return snapshot, nil
}

func (c *Client) startRoomAudioMainlineMirror(program *programState, trackIndex int, track programTrack, startOffsetMS int, taskID string) {
	if c == nil || c.roomAudio == nil || program == nil || program.RoomID <= 0 || strings.TrimSpace(track.AudioURL) == "" {
		return
	}
	c.cancelRoomAudioMirror(program.RoomID)
	ctx, cancel := context.WithCancel(context.Background())
	c.mirrorMu.Lock()
	c.mirrors[program.RoomID] = cancel
	c.mirrorMu.Unlock()

	segmentID := roomAudioSegmentAt(track.Timeline, startOffsetMS)
	c.roomAudio.SetMainlineTimeline(program.RoomID, roomAudioSpeechTimeline(track.Timeline))
	resumePhase := c.roomAudio.Snapshot(program.RoomID).Phase == roomaudio.PhaseResume
	if !resumePhase {
		if _, err := c.roomAudio.StartMainline(program.RoomID, segmentID); err != nil {
			log.Printf("room audio mirror start room=%d task=%s failed: %v", program.RoomID, taskID, err)
			cancel()
			return
		}
	}

	go func() {
		var source io.ReadCloser
		c.mu.Lock()
		if program.PrefetchedTrack == trackIndex && len(program.PrefetchedAudio) > 0 {
			source = io.NopCloser(bytes.NewReader(program.PrefetchedAudio))
			program.PrefetchedTrack = -1
			program.PrefetchedAudio = nil
		}
		c.mu.Unlock()
		if source == nil {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, track.AudioURL, nil)
			if err != nil {
				log.Printf("room audio mirror request room=%d task=%s failed: %v", program.RoomID, taskID, err)
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("room audio mirror download room=%d task=%s failed: %v", program.RoomID, taskID, err)
				}
				return
			}
			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				resp.Body.Close()
				log.Printf("room audio mirror source room=%d task=%s http=%d", program.RoomID, taskID, resp.StatusCode)
				return
			}
			source = resp.Body
		}
		defer source.Close()

		first := true
		resumeCompleted := false
		ticker := time.NewTicker(time.Duration(roomaudio.FrameDurationMS) * time.Millisecond)
		defer ticker.Stop()
		planned := false
		err := roomaudio.StreamWAVPCM(ctx, source, startOffsetMS, func(pcm []byte, cursorMS int) error {
			if !first {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
			first = false
			_, publishErr := c.roomAudio.PublishPCMAt(
				program.RoomID,
				roomaudio.SourceMainline,
				pcm,
				roomAudioSegmentAt(track.Timeline, cursorMS),
				cursorMS,
			)
			if publishErr == nil && resumePhase && !resumeCompleted {
				resumeCompleted = true
				_, _ = c.roomAudio.CompleteResume(program.RoomID)
			}
			if publishErr == nil && !planned && track.DurationMS-cursorMS <= mainlinePlanLeadMS {
				planned = true
				c.planNextMainline(program, taskID)
			}
			return publishErr
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("room audio mirror stream room=%d task=%s failed: %v", program.RoomID, taskID, err)
			return
		}
		if ctx.Err() == nil {
			c.advanceMainline(program, taskID)
		}
	}()
}

func (c *Client) startRoomAudioInterruptMirror(roomID int64, audioURL, taskID string) {
	if c == nil || c.roomAudio == nil || roomID <= 0 || strings.TrimSpace(audioURL) == "" {
		return
	}
	c.cancelRoomAudioMirror(roomID)
	ctx, cancel := context.WithCancel(context.Background())
	c.mirrorMu.Lock()
	c.mirrors[roomID] = cancel
	c.mirrorMu.Unlock()

	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, audioURL, nil)
		if err != nil {
			log.Printf("room audio interrupt mirror request room=%d task=%s failed: %v", roomID, taskID, err)
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("room audio interrupt mirror download room=%d task=%s failed: %v", roomID, taskID, err)
			}
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			log.Printf("room audio interrupt mirror source room=%d task=%s http=%d", roomID, taskID, resp.StatusCode)
			return
		}

		first := true
		ticker := time.NewTicker(time.Duration(roomaudio.FrameDurationMS) * time.Millisecond)
		defer ticker.Stop()
		err = roomaudio.StreamWAVPCM(ctx, resp.Body, 0, func(pcm []byte, cursorMS int) error {
			if !first {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
			first = false
			_, publishErr := c.roomAudio.PublishPCMAt(roomID, roomaudio.SourceInterrupt, pcm, "", cursorMS)
			return publishErr
		})
		if err != nil && ctx.Err() == nil {
			log.Printf("room audio interrupt mirror stream room=%d task=%s failed: %v", roomID, taskID, err)
			return
		}
		if ctx.Err() == nil {
			c.mu.RLock()
			program := c.programs[roomID]
			c.mu.RUnlock()
			if program != nil {
				c.resumeAfterInteraction(program, taskID)
			}
		}
	}()
}

func (c *Client) cancelRoomAudioMirror(roomID int64) {
	if c == nil || roomID <= 0 {
		return
	}
	c.mirrorMu.Lock()
	cancel := c.mirrors[roomID]
	delete(c.mirrors, roomID)
	c.mirrorMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func roomAudioSpeechTimeline(timeline []audioout.ProgramTimelineSegment) []roomaudio.SpeechSegment {
	result := make([]roomaudio.SpeechSegment, 0, len(timeline))
	for _, segment := range timeline {
		if strings.TrimSpace(segment.SegmentID) == "" || strings.TrimSpace(segment.Text) == "" || segment.EndMS <= segment.StartMS {
			continue
		}
		result = append(result, roomaudio.SpeechSegment{
			SegmentID: segment.SegmentID,
			StartMS:   segment.StartMS,
			EndMS:     segment.EndMS,
			Text:      segment.Text,
		})
	}
	return result
}

func roomAudioInteractionTimeline(text string, durationMS int) []roomaudio.SpeechSegment {
	text = strings.TrimSpace(text)
	if text == "" || durationMS <= 0 {
		return nil
	}
	parts := splitRoomAudioInteractionText(text)
	if len(parts) == 0 {
		parts = []string{text}
	}
	totalRunes := 0
	for _, part := range parts {
		length := len([]rune(part))
		if length < 1 {
			length = 1
		}
		totalRunes += length
	}
	result := make([]roomaudio.SpeechSegment, 0, len(parts))
	start := 0
	used := 0
	for index, part := range parts {
		length := len([]rune(part))
		if length < 1 {
			length = 1
		}
		used += length
		end := durationMS
		if index < len(parts)-1 {
			end = int(float64(durationMS) * float64(used) / float64(totalRunes))
		}
		if end <= start {
			end = start + 1
		}
		result = append(result, roomaudio.SpeechSegment{
			SegmentID: fmt.Sprintf("interrupt-%03d", index+1),
			StartMS:   start,
			EndMS:     end,
			Text:      strings.TrimSpace(part),
		})
		start = end
	}
	return result
}

func splitRoomAudioInteractionText(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	const target = 24
	const hardMax = 34
	result := make([]string, 0, 4)
	buffer := make([]rune, 0, hardMax)
	flush := func() {
		value := strings.TrimSpace(string(buffer))
		if value != "" {
			result = append(result, value)
		}
		buffer = buffer[:0]
	}
	for _, r := range runes {
		buffer = append(buffer, r)
		punctuation := strings.ContainsRune("，。！？!?；;、", r)
		if (punctuation && len(buffer) >= 12) || len(buffer) >= hardMax || (len(buffer) >= target && strings.ContainsRune("，。！？!?；;", r)) {
			flush()
		}
	}
	flush()
	return result
}

func roomAudioSegmentAt(timeline []audioout.ProgramTimelineSegment, cursorMS int) string {
	for _, segment := range timeline {
		if cursorMS >= segment.StartMS && cursorMS < segment.EndMS {
			return segment.SegmentID
		}
	}
	if len(timeline) > 0 && cursorMS >= timeline[len(timeline)-1].EndMS {
		return timeline[len(timeline)-1].SegmentID
	}
	return ""
}

func roomAudioCutSegments(timeline []audioout.ProgramTimelineSegment, cutMS int) (string, string) {
	for index, segment := range timeline {
		if segment.EndMS != cutMS {
			continue
		}
		resumeSegmentID := ""
		if index+1 < len(timeline) {
			resumeSegmentID = timeline[index+1].SegmentID
		}
		return segment.SegmentID, resumeSegmentID
	}
	return "", ""
}
