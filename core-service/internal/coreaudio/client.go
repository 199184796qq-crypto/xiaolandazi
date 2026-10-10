package coreaudio

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/roomaudio"
)

type programTrack struct {
	AllowConsecutiveRepeat bool
	ID                     string
	Label                  string
	Kind                   string
	Text                   string
	AudioURL               string
	MimeType               string
	DurationMS             int
	Timeline               []audioout.ProgramTimelineSegment
	SafePoints             []audioout.ProgramSafePoint
}

type programState struct {
	PlaybackCoverage    map[string]playbackCoverage
	PlaybackReceipts    []audioout.MainlineCoverageReceipt
	PlaybackSequence    uint64
	MainlinePlayedMS    int64
	ActualTrackReceipts []audioout.MainlinePlaybackReceipt
	ActualTrackSeen     map[string]bool
	ActualTrackSequence uint64
	InteractionReturn   *interactionReturn
	Sequential          bool
	WaitingForContent   bool
	ID                  string
	RoomID              int64
	SessionID           string
	Label               string
	VersionID           int64
	VersionNo           int64
	Tracks              []programTrack
	TrackIndex          int
	StartedAt           time.Time
	ActualStartedAt     time.Time
	CutWaitingTaskID    string
	CutWaitingEnded     bool
	SegmentStartedAt    time.Time
	CurrentTaskID       string
	CurrentSlot         string
	Sequence            uint64
	Running             bool
	Suspended           bool
	ManualPaused        bool
	ResumeOffsetMS      int
	PlannedNextTrack    int
	PlannedForTaskID    string
	PlannedNextScore    int
	PlannedNextReason   string
	RecentTrackIDs      []string
	TrackPlayCount      map[string]int
	TrackLastPlayed     map[string]time.Time
	PrefetchedTrack     int
	PrefetchedAudio     []byte
	Generation          uint64
	PendingRefresh      *programRefresh
	ApplyingRefresh     *programRefresh
	RefreshReceipts     map[string]programRefreshReceipt
	LastAppliedJobID    string
	Transitioning       bool
	AppendedUnits       map[string][32]byte
}

type Client struct {
	mu sync.RWMutex

	hub               *audiohub.Hub
	roomAudio         *roomaudio.Engine
	publicURL         string
	programs          map[int64]*programState
	sequence          atomic.Uint64
	now               func() time.Time
	mainlineSignals   MainlineAgentSignalProvider
	mirrorMu          sync.Mutex
	mirrors           map[int64]context.CancelFunc
	messages          *messagecaptain.Captain
	messageUnregister func()

	testAudio      []byte
	testDurationMS int
	testVersion    string
	testPath       string
}

func (c *Client) recordActualStart(program *programState, taskID string) {
	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running || program.CurrentTaskID != taskID || !program.ActualStartedAt.IsZero() {
		c.mu.Unlock()
		return
	}
	at := c.now().UTC()
	program.ActualStartedAt = at
	captain := c.messages
	c.mu.Unlock()
	if captain != nil {
		captain.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.MainlineStarted, RoomID: program.RoomID, RunID: program.ID, TaskID: taskID, OccurredAt: at})
	}
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

// Continuous PCM receivers do not play individual hub tasks. Report the
// paced stream's terminal boundary to the same lifecycle as receiver events.
func (c *Client) SetMessageCaptain(captain *messagecaptain.Captain) {
	if c.messageUnregister != nil {
		c.messageUnregister()
	}
	c.mu.Lock()
	c.messages = captain
	c.mu.Unlock()
	if captain != nil {
		c.messageUnregister = captain.Register("mainline-playback", func(ctx context.Context, message messagecaptain.Message) error {
			if message.Kind == messagecaptain.ReturnBridgeReady {
				return c.handleReturnMessage(ctx, message)
			}
			if message.Kind != messagecaptain.AudioTerminal {
				return nil
			}
			c.mu.RLock()
			program := c.programs[message.RoomID]
			valid := program != nil && program.ID == message.RunID && program.CurrentTaskID == message.TaskID && program.Suspended
			c.mu.RUnlock()
			if valid {
				c.resumeAfterInteraction(program, message.TaskID)
			}
			return nil
		})
	}
}

func (c *Client) observeInterruptTerminal(roomID int64, taskID, status string, streamErr error) {
	c.mu.RLock()
	program, captain := c.programs[roomID], c.messages
	valid := program != nil && program.Running && program.CurrentTaskID == taskID && program.Suspended
	c.mu.RUnlock()
	if !valid || captain == nil {
		return
	}
	if snapshot, ok := c.hub.Snapshot(taskID); ok {
		event := audioout.PlaybackEvent{RoomID: roomID, SpeechTaskID: taskID, SessionID: snapshot.Task.SessionID, ReceiverID: "core-pcm-output", Status: status, OccurredAt: c.now().UTC()}
		if streamErr != nil {
			event.Error = streamErr.Error()
		}
		raw, _ := json.Marshal(event)
		for _, err := range captain.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.AudioTerminal, RoomID: roomID, RunID: program.ID, TaskID: taskID, Payload: raw}) {
			log.Printf("runtime message delivery room=%d task=%s failed: %v", roomID, taskID, err)
		}
	}
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
	prepared, err := c.prepareProgramTracks(ctx, input.Tracks)
	if err != nil {
		return audioout.RoomProgramSnapshot{}, err
	}
	return c.startPreparedProgram(input.RoomID, strings.TrimSpace(input.SessionID), strings.TrimSpace(input.Label), input.VersionID, input.VersionNo, prepared, input.Sequential)
}

func (c *Client) prepareProgramTracks(ctx context.Context, tracks []audioout.ProgramTrack) ([]programTrack, error) {
	if len(tracks) == 0 || len(tracks) > 20 {
		return nil, errors.New("program requires 1 to 20 tracks")
	}
	prepared := make([]programTrack, 0, len(tracks))
	ids := make(map[string]bool)
	for index, track := range tracks {
		url := strings.TrimSpace(track.AudioURL)
		if url == "" {
			return nil, fmt.Errorf("track %d audio_url is required", index+1)
		}
		durationMS := track.DurationMS
		mimeType := ""
		var err error
		if durationMS > 0 {
			mimeType, err = c.hub.ProbeExternalWAVHeader(ctx, url)
		} else {
			durationMS, mimeType, err = c.hub.ProbeExternalWAV(ctx, url)
		}
		if err != nil {
			return nil, fmt.Errorf("probe track %d: %w", index+1, err)
		}
		id := strings.TrimSpace(track.ID)
		if id == "" {
			id = fmt.Sprintf("track-%d", index+1)
		}
		if ids[id] {
			return nil, fmt.Errorf("duplicate track id %q", id)
		}
		ids[id] = true
		label := strings.TrimSpace(track.Label)
		if label == "" {
			label = id
		}
		timeline, err := normalizeProgramTimeline(track.Timeline, durationMS)
		if err != nil {
			return nil, fmt.Errorf("track %d timeline: %w", index+1, err)
		}
		safePoints, err := normalizeProgramSafePoints(track.SafePoints, timeline, durationMS)
		if err != nil {
			return nil, fmt.Errorf("track %d safe points: %w", index+1, err)
		}
		prepared = append(prepared, programTrack{
			AllowConsecutiveRepeat: track.AllowConsecutiveRepeat,
			ID:                     id,
			Label:                  label,
			Kind:                   "mainline_program",
			Text:                   strings.TrimSpace(track.Text),
			AudioURL:               url,
			MimeType:               mimeType,
			DurationMS:             durationMS,
			Timeline:               timeline,
			SafePoints:             safePoints,
		})
	}
	return prepared, nil
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
	sequential ...bool,
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
	if len(sequential) > 0 && sequential[0] {
		kept := make([]programTrack, 0, len(tracks))
		for _, track := range tracks {
			if len(kept) > 0 && !track.AllowConsecutiveRepeat && audioout.SameSpeech(kept[len(kept)-1].Text, track.Text) {
				log.Printf("mainline omitted room=%d track=%s reason=consecutive_exact_duplicate", roomID, track.ID)
				continue
			}
			kept = append(kept, track)
		}
		tracks = kept
	}
	c.mu.Lock()
	if existing := c.programs[roomID]; existing != nil && existing.Running {
		snapshot := c.programSnapshotLocked(existing, c.now().UTC())
		c.mu.Unlock()
		return snapshot, nil
	}
	now := c.now().UTC()
	program := &programState{
		Sequential:       len(sequential) > 0 && sequential[0],
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
		RefreshReceipts:  make(map[string]programRefreshReceipt),
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
	if program == nil {
		return audioout.SpeechTask{}, errors.New("room program is not running")
	}
	c.mu.RLock()
	if c.programs[program.RoomID] != program || !program.Running {
		c.mu.RUnlock()
		return audioout.SpeechTask{}, errors.New("room program is not running")
	}
	if len(program.Tracks) == 0 {
		c.mu.RUnlock()
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
	c.mu.RUnlock()
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
		c.mu.Lock()
		rollbackProgramRefreshLocked(program)
		c.mu.Unlock()
		return audioout.SpeechTask{}, err
	}

	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running {
		c.mu.Unlock()
		c.hub.Expire(task.ID)
		return audioout.SpeechTask{}, errors.New("room program stopped before mainline publish")
	}
	program.Sequence = sequence
	finishProgramRefreshLocked(program)
	program.Transitioning = false
	program.CurrentTaskID = task.ID
	program.CurrentSlot = task.Slot
	program.SegmentStartedAt = task.StartedAt
	program.Suspended = false
	program.WaitingForContent = false
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
	c.mu.Lock()
	if program.CutWaitingTaskID == taskID {
		program.CutWaitingEnded = true
		c.mu.Unlock()
		return
	}
	current := c.programs[program.RoomID]
	if current != program || !program.Running || program.Suspended || program.Transitioning || program.CurrentTaskID != taskID {
		c.mu.Unlock()
		return
	}
	program.Transitioning = true
	if program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		finishPlaybackReceipt(program, program.Tracks[program.TrackIndex].ID)
	}
	expireProgramRefreshLocked(program, c.now().UTC())
	nextSequence := program.Sequence + 1
	currentTrack := program.TrackIndex
	plannedTrack := program.PlannedNextTrack
	if program.PendingRefresh != nil {
		if program.PendingRefresh.ProtectedNextIndex >= 0 {
			plannedTrack = program.PendingRefresh.ProtectedNextIndex
			program.PendingRefresh.ProtectedNextIndex = -1
			if program.Sequential && plannedTrack+1 < len(program.Tracks) {
				program.PendingRefresh.ProtectedNextIndex = plannedTrack + 1
			}
		} else {
			applyProgramRefreshLocked(program)
			currentTrack = program.TrackIndex
			plannedTrack = -1
		}
	}
	trackCount := len(program.Tracks)
	c.mu.Unlock()

	nextTrack := plannedTrack
	if nextTrack < 0 || nextTrack >= trackCount || (trackCount > 1 && nextTrack == currentTrack) {
		nextTrack, _, _ = c.selectNextMainlineTrack(program, currentTrack)
	}
	if nextTrack < 0 || nextTrack >= trackCount {
		if program.Sequential {
			c.mu.Lock()
			if c.programs[program.RoomID] == program && program.CurrentTaskID == taskID {
				program.WaitingForContent = true
				program.Transitioning = false
				program.PlannedNextTrack = -1
			}
			c.mu.Unlock()
			c.hub.Expire(taskID)
			return // An exhausted queue waits; it never replays old content.
		}
		nextTrack = currentTrack
	}

	c.mu.Lock()
	if c.programs[program.RoomID] != program || !program.Running || program.Suspended || program.CurrentTaskID != taskID {
		program.Transitioning = false
		c.mu.Unlock()
		return
	}
	program.TrackIndex = nextTrack
	program.WaitingForContent = false
	c.mu.Unlock()
	c.hub.Expire(taskID)
	if _, err := c.publishMainline(program, nextSequence, 0, c.now().UTC()); err != nil {
		c.mu.Lock()
		program.Transitioning = false
		c.mu.Unlock()
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
	if program.Suspended || program.Transitioning {
		c.mu.RUnlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room program already has an active interaction")
	}
	currentTaskID := program.CurrentTaskID
	c.mu.RUnlock()
	if input.ExpectedTaskID != "" && input.ExpectedTaskID != currentTaskID {
		return audioout.RoomProgramSnapshot{}, audioout.ErrInteractionSwitchStale
	}

	currentSnapshot, ok := c.hub.Snapshot(currentTaskID)
	if !ok || (currentSnapshot.Task.Kind != "test_wav_program" && currentSnapshot.Task.Kind != "mainline_program") {
		return audioout.RoomProgramSnapshot{}, errors.New("current room output is not resumable mainline")
	}
	now := c.now().UTC()
	wallClockPositionMS := int(now.Sub(currentSnapshot.Task.StartedAt).Milliseconds())
	receiverPositionMS, hasReceiverPosition := activeReceiverPlaybackPosition(currentSnapshot)
	roomAudioPositionMS := 0
	hasRoomAudioPosition := false
	if c.roomAudio != nil {
		engineSnapshot := c.roomAudio.Snapshot(input.RoomID)
		if engineSnapshot.ActiveSource == roomaudio.SourceMainline && engineSnapshot.Phase != roomaudio.PhaseIdle {
			roomAudioPositionMS = engineSnapshot.MainlineCursorMS
			hasRoomAudioPosition = true
		}
	}
	currentPositionMS := slowestMainlinePlaybackPosition(
		wallClockPositionMS,
		receiverPositionMS,
		hasReceiverPosition,
		roomAudioPositionMS,
		hasRoomAudioPosition,
		currentSnapshot.Task.DurationMS,
	)
	waitMS := 0
	if input.SwitchAtMS != nil {
		target := *input.SwitchAtMS
		if target < 0 || target > currentSnapshot.Task.DurationMS {
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: switch_at_ms is outside current mainline", audioout.ErrInteractionSwitchStale)
		}
		if target <= currentPositionMS {
			return audioout.RoomProgramSnapshot{}, fmt.Errorf("%w: switch_at_ms already passed", audioout.ErrInteractionSwitchStale)
		}
		waitMS = target - currentPositionMS
		if waitMS > 35000 {
			return audioout.RoomProgramSnapshot{}, errors.New("switch_at_ms is too far ahead")
		}
		// Reserve a track-end cut before its mirror can advance to the next
		// short file. On cancellation let the completed mainline advance.
		if target == currentSnapshot.Task.DurationMS {
			c.mu.Lock()
			if c.programs[input.RoomID] != program || program.CurrentTaskID != currentTaskID {
				c.mu.Unlock()
				return audioout.RoomProgramSnapshot{}, audioout.ErrInteractionSwitchStale
			}
			program.CutWaitingTaskID = currentTaskID
			program.CutWaitingEnded = false
			c.mu.Unlock()
			defer func() {
				c.mu.Lock()
				program.CutWaitingTaskID = ""
				advance := program.CutWaitingEnded && program.Running && !program.Suspended && program.CurrentTaskID == currentTaskID
				program.CutWaitingEnded = false
				c.mu.Unlock()
				if advance {
					go c.advanceMainline(program, currentTaskID)
				}
			}()
		}
		if c.roomAudio != nil {
			cutSegmentID, resumeSegmentID := roomAudioCutSegments(program.Tracks[program.TrackIndex].Timeline, target)
			if _, prepareErr := c.roomAudio.PrepareInterrupt(input.RoomID); prepareErr == nil && cutSegmentID != "" {
				_, _ = c.roomAudio.ArmInterrupt(input.RoomID, cutSegmentID, resumeSegmentID)
			}
		}
	}
	// Audio metadata is ready and the executable cut is fixed. Expose its
	// identity before waiting so bridge production can use this lead time.
	var reservedReturn *interactionReturn
	if input.DeferredReturn && program.Sequential && c.roomAudio != nil {
		c.mu.Lock()
		if c.programs[input.RoomID] != program || program.CurrentTaskID != currentTaskID || program.InteractionReturn != nil {
			c.mu.Unlock()
			return audioout.RoomProgramSnapshot{}, audioout.ErrInteractionSwitchStale
		}
		cut := currentPositionMS
		if input.SwitchAtMS != nil {
			cut = *input.SwitchAtMS
		}
		if input.ResumeOffsetMS != nil {
			cut = *input.ResumeOffsetMS
		}
		reservedID := fmt.Sprintf("core-interaction-%d-%s-%06d", input.RoomID, program.ID, program.Sequence+1)
		reservedReturn = buildInteractionReturn(program, reservedID, input.Text, cut, durationMS, input.AllowConsecutiveRepeat)
		reservedReturn.pendingMainlineTask = currentTaskID
		reservedReturn.snapshot.DecisionID = input.DecisionID
		reservedReturn.snapshot.Deadline = c.now().Add(time.Duration(waitMS+durationMS-800) * time.Millisecond)
		program.InteractionReturn = reservedReturn
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if program.InteractionReturn == reservedReturn && !program.Suspended {
				program.InteractionReturn = nil
			}
		}()
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
	if program == nil || !program.Running || program.Suspended || program.Transitioning || program.CurrentTaskID != currentTaskID {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, audioout.ErrInteractionSwitchStale
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
	if reservedReturn != nil && program.InteractionReturn == reservedReturn && reservedReturn.snapshot.TaskID == interactionID {
		reservedReturn.pendingMainlineTask = ""
	} else {
		program.InteractionReturn = nil
	}
	c.mu.Unlock()

	c.cancelRoomAudioMirror(input.RoomID)
	c.hub.Expire(currentTaskID)
	interaction, publishErr := c.hub.Publish(audiohub.Task{
		ID:         interactionID,
		RoomID:     input.RoomID,
		SessionID:  sessionID,
		Kind:       "interaction_audio",
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
	if state := program.InteractionReturn; state != nil && state.snapshot.BridgeStaged {
		c.hub.ExtendTaskDuration(interaction.ID, durationMS+len(state.bridgePCM)/roomaudio.PCMBytesPerFrame*roomaudio.FrameDurationMS)
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

// slowestMainlinePlaybackPosition keeps every active output on the same
// scheduling clock. A browser/device receiver and the Core PCM mirror can be
// at different positions while buffering or normalizing audio; selecting a
// safe cut from the faster cursor can put that cut more than 35 seconds ahead
// of the slower output and make the interaction retry forever.
func slowestMainlinePlaybackPosition(
	wallClockMS int,
	receiverMS int,
	hasReceiver bool,
	roomAudioMS int,
	hasRoomAudio bool,
	durationMS int,
) int {
	positionMS := wallClockMS
	if positionMS < 0 {
		positionMS = 0
	}
	hasPlaybackClock := false
	if hasReceiver {
		positionMS = receiverMS
		hasPlaybackClock = true
	}
	if hasRoomAudio && (!hasPlaybackClock || roomAudioMS < positionMS) {
		positionMS = roomAudioMS
		hasPlaybackClock = true
	}
	if positionMS < 0 {
		positionMS = 0
	}
	if durationMS > 0 && positionMS >= durationMS {
		positionMS = durationMS - 1
	}
	return positionMS
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
	if state := program.InteractionReturn; state != nil {
		if !state.finished {
			c.mu.Unlock()
			return
		}
		for i := program.TrackIndex; i < state.resumeTrack && i < len(program.Tracks); i++ {
			if i >= 0 {
				finishPlaybackReceipt(program, program.Tracks[i].ID)
			}
		}
		program.TrackIndex = state.resumeTrack
		resumeMS = state.resumeMS
		program.InteractionReturn = nil
	}
	nextSequence := program.Sequence + 1
	program.Suspended = false
	resumeMS = prepareProgramResumeLocked(program, resumeMS)
	if program.Sequential && program.WaitingForContent && program.ApplyingRefresh == nil {
		c.mu.Unlock()
		c.hub.Expire(interactionTaskID)
		return
	}
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
	if program.Transitioning {
		c.mu.Unlock()
		return audioout.RoomProgramSnapshot{}, errors.New("room program is advancing at a safe boundary; retry pause")
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
	if program.Sequential && program.WaitingForContent && program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		program.ResumeOffsetMS = program.Tracks[program.TrackIndex].DurationMS
	}
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
	if program.WaitingForContent && program.PendingRefresh == nil {
		program.Suspended, program.ManualPaused = false, false
		if program.TrackIndex+1 < len(program.Tracks) {
			go c.advanceMainline(program, program.CurrentTaskID)
		}
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		return snapshot, nil
	}
	resumeMS := prepareProgramResumeLocked(program, program.ResumeOffsetMS)
	if program.Sequential && program.WaitingForContent && program.ApplyingRefresh == nil {
		program.Suspended, program.ManualPaused = false, false
		snapshot := c.programSnapshotLocked(program, c.now().UTC())
		c.mu.Unlock()
		return snapshot, nil
	}
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
		finishPlaybackReceipt(program, program.Tracks[program.TrackIndex].ID)
		expireProgramRefreshLocked(program, time.Now().UTC())
		if refresh := program.PendingRefresh; refresh != nil {
			if refresh.ProtectedNextIndex >= 0 {
				program.TrackIndex = refresh.ProtectedNextIndex
				refresh.ProtectedNextIndex = -1
				if program.Sequential && program.TrackIndex+1 < len(program.Tracks) {
					refresh.ProtectedNextIndex = program.TrackIndex + 1
				}
				return 0
			}
			applyProgramRefreshLocked(program)
		}
		program.TrackIndex++
		if program.TrackIndex >= len(program.Tracks) {
			if program.Sequential {
				program.TrackIndex = len(program.Tracks) - 1
				program.WaitingForContent = true
				return durationMS
			}
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
		MainlinePlayedMS:         program.MainlinePlayedMS,
		PlaybackReceipts:         append([]audioout.MainlineCoverageReceipt(nil), program.PlaybackReceipts...),
		MainlinePlaybackReceipts: append([]audioout.MainlinePlaybackReceipt(nil), program.ActualTrackReceipts...),
		Sequential:               program.Sequential,
		WaitingForContent:        program.WaitingForContent,
		ProgramID:                program.ID,
		Generation:               program.Generation,
		LastAppliedJobID:         program.LastAppliedJobID,
		RoomID:                   program.RoomID,
		VersionID:                program.VersionID,
		VersionNo:                program.VersionNo,
		TrackID:                  trackID,
		TrackIndex:               program.TrackIndex,
		TrackCount:               len(program.Tracks),
		TrackText:                trackText,
		Timeline:                 timeline,
		SafePoints:               safePoints,
		Running:                  program.Running,
		Suspended:                program.Suspended,
		ResumeOffsetMS:           program.ResumeOffsetMS,
		PlannedNextScore:         program.PlannedNextScore,
		PlannedNextReason:        program.PlannedNextReason,
		Sequence:                 program.Sequence,
		Slot:                     program.CurrentSlot,
		StartedAt:                program.StartedAt,
		ActualStartedAt:          program.ActualStartedAt,
		ServerTime:               now,
	}
	if program.PendingRefresh != nil {
		snapshot.PendingGeneration = program.PendingRefresh.Generation
		snapshot.PendingJobID = program.PendingRefresh.JobID
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
		isInteractionAudio := task.Kind == "interaction_audio" || task.Kind == "interaction_tts"
		if program.Running && !program.Suspended && !isInteractionAudio && !task.StartedAt.IsZero() && task.DurationMS > 0 {
			wallClockPositionMS := int(now.Sub(task.StartedAt).Milliseconds())
			receiverPositionMS, hasReceiverPosition := activeReceiverPlaybackPosition(state)
			roomAudioPositionMS := 0
			hasRoomAudioPosition := false
			if c.roomAudio != nil {
				engineSnapshot := c.roomAudio.Snapshot(program.RoomID)
				if engineSnapshot.ActiveSource == roomaudio.SourceMainline && engineSnapshot.Phase != roomaudio.PhaseIdle {
					roomAudioPositionMS = engineSnapshot.MainlineCursorMS
					hasRoomAudioPosition = true
				}
			}
			progress := slowestMainlinePlaybackPosition(
				wallClockPositionMS,
				receiverPositionMS,
				hasReceiverPosition,
				roomAudioPositionMS,
				hasRoomAudioPosition,
				task.DurationMS,
			)
			task.StartMS = progress
			currentMS = progress
		}
		snapshot.Task = &task
	}
	snapshot.CurrentMS = currentMS
	// Publish bounded, ready future units for cross-track preparation. Execution
	// offsets and media authority are unchanged; no guessed audio is included.
	if program.Sequential && program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		offset := 0
		for index := program.TrackIndex; index < len(program.Tracks) && len(snapshot.FutureWindow) < 32; index++ {
			track := program.Tracks[index]
			snapshot.FutureWindow = append(snapshot.FutureWindow, audioout.ProgramWindowTrack{TrackID: track.ID, TrackIndex: index, OffsetMS: offset, DurationMS: track.DurationMS, Text: track.Text, Timeline: append([]audioout.ProgramTimelineSegment(nil), track.Timeline...), SafePoints: append([]audioout.ProgramSafePoint(nil), track.SafePoints...)})
			offset += track.DurationMS
			if offset-currentMS >= 90000 {
				break
			}
		}
	}
	if program.WaitingForContent && program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		snapshot.CurrentMS = program.Tracks[program.TrackIndex].DurationMS
	}
	if program.Sequential && program.TrackIndex >= 0 && program.TrackIndex < len(program.Tracks) {
		for _, queued := range program.Tracks[program.TrackIndex:] {
			snapshot.BufferedMainlineMS += int64(queued.DurationMS)
		}
		snapshot.BufferedMainlineMS = max(0, snapshot.BufferedMainlineMS-int64(snapshot.CurrentMS))
		if program.TrackIndex+1 < len(program.Tracks) {
			snapshot.NextTrackText = program.Tracks[program.TrackIndex+1].Text
		}
	}
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
	rollbackProgramRefreshLocked(program)
	if refresh := program.PendingRefresh; refresh != nil {
		receipt := program.RefreshReceipts[refresh.JobID]
		receipt.Cancelled = true
		program.RefreshReceipts[refresh.JobID] = receipt
		program.PendingRefresh = nil
	}
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
	captionTimeline := roomAudioSpeechTimeline(track.Timeline)
	fallbackCaptionID := ""
	if len(captionTimeline) == 0 && strings.TrimSpace(track.Text) != "" && track.DurationMS > 0 {
		// Legacy tracks without sentence timing still use the same output clock.
		// This is only a caption descriptor, never a newly inferred safe cut.
		fallbackCaptionID = taskID + "-caption"
		segmentID = fallbackCaptionID
		captionTimeline = []roomaudio.SpeechSegment{{SegmentID: fallbackCaptionID, StartMS: 0, EndMS: track.DurationMS, Text: track.Text}}
	}
	c.roomAudio.SetMainlineTimeline(program.RoomID, captionTimeline)
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
		publishFrame := func(pcm []byte, cursorMS int) error {
			if !first {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
			first = false
			captionID := roomAudioSegmentAt(track.Timeline, cursorMS)
			if captionID == "" {
				captionID = fallbackCaptionID
			}
			_, publishErr := c.roomAudio.PublishPCMAt(
				program.RoomID,
				roomaudio.SourceMainline,
				pcm,
				captionID,
				cursorMS,
			)
			if publishErr == nil {
				c.recordActualStart(program, taskID)
				c.recordActualTrackStart(program, taskID, track.ID)
			}
			if publishErr == nil && resumePhase && !resumeCompleted {
				resumeCompleted = true
				_, _ = c.roomAudio.CompleteResume(program.RoomID)
			}
			if publishErr == nil && !planned && track.DurationMS-cursorMS <= mainlinePlanLeadMS {
				planned = true
				c.planNextMainline(program, taskID)
			}
			return publishErr
		}
		err := roomaudio.StreamWAVPCM(ctx, source, startOffsetMS, publishFrame)
		if errors.Is(err, roomaudio.ErrPCMNormalizationRequired) && ctx.Err() == nil {
			_ = source.Close()
			first = true
			log.Printf("room audio mirror normalizing legacy source room=%d task=%s", program.RoomID, taskID)
			err = streamAudioURLAsCorePCM(ctx, track.AudioURL, startOffsetMS, publishFrame)
		}
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
		terminalSent := false
		defer func() {
			if !terminalSent && ctx.Err() == nil {
				c.finishReturn(roomID, taskID)
				c.observeInterruptTerminal(roomID, taskID, "FAILED", errors.New("interaction PCM output unavailable"))
			}
		}()
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
		nextCursorMS := 0
		ticker := time.NewTicker(time.Duration(roomaudio.FrameDurationMS) * time.Millisecond)
		defer ticker.Stop()
		publishFrame := func(pcm []byte, cursorMS int) error {
			if !first {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
			first = false
			nextCursorMS = cursorMS + roomaudio.FrameDurationMS
			_, publishErr := c.roomAudio.PublishPCMAt(roomID, roomaudio.SourceInterrupt, pcm, "", cursorMS)
			return publishErr
		}
		silence := make([]byte, roomaudio.PCMBytesPerFrame)
		for elapsed := 0; elapsed < roomaudio.VoiceGuardHeadMS; elapsed += roomaudio.FrameDurationMS {
			if err := publishFrame(silence, nextCursorMS); err != nil {
				if ctx.Err() == nil {
					log.Printf("room audio interrupt guard head room=%d task=%s failed: %v", roomID, taskID, err)
				}
				return
			}
		}
		headOffsetMS := nextCursorMS
		publishAudioFrame := func(pcm []byte, cursorMS int) error {
			err := publishFrame(pcm, headOffsetMS+cursorMS)
			if err == nil {
				c.markInteractionAudioStarted(roomID, taskID)
			}
			return err
		}
		err = roomaudio.StreamWAVPCM(ctx, resp.Body, 0, publishAudioFrame)
		if errors.Is(err, roomaudio.ErrPCMNormalizationRequired) && ctx.Err() == nil {
			_ = resp.Body.Close()
			log.Printf("room audio interrupt mirror normalizing legacy source room=%d task=%s", roomID, taskID)
			err = streamAudioURLAsCorePCM(ctx, audioURL, 0, publishAudioFrame)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("room audio interrupt mirror stream room=%d task=%s failed: %v", roomID, taskID, err)
			c.finishReturn(roomID, taskID)
			c.observeInterruptTerminal(roomID, taskID, "FAILED", err)
			terminalSent = true
			return
		}
		if ctx.Err() == nil {
			pcm, bridge := c.takeReturnBridge(roomID, taskID)
			if len(pcm) > 0 {
				c.mu.RLock()
				captain := c.messages
				program := c.programs[roomID]
				runID := ""
				if program != nil {
					runID = program.ID
				}
				c.mu.RUnlock()
				c.roomAudio.AppendInterruptSpeechSegment(roomID, roomaudio.SpeechSegment{SegmentID: taskID + "-bridge", StartMS: nextCursorMS, EndMS: nextCursorMS + len(pcm)/roomaudio.PCMBytesPerFrame*roomaudio.FrameDurationMS, Text: bridge})
				for at := 0; at < len(pcm); at += roomaudio.PCMBytesPerFrame {
					if err = publishFrame(pcm[at:at+roomaudio.PCMBytesPerFrame], nextCursorMS); err != nil {
						break
					}
					if at == 0 && captain != nil {
						raw, _ := json.Marshal(map[string]any{"text": bridge, "task_id": taskID})
						captain.Publish(ctx, messagecaptain.Message{Kind: messagecaptain.ReturnBridgeStarted, RoomID: roomID, RunID: runID, TaskID: taskID, Payload: raw})
					}
				}
				if err != nil {
					c.finishReturn(roomID, taskID)
					if ctx.Err() == nil {
						c.observeInterruptTerminal(roomID, taskID, "FAILED", err)
					}
					return
				}
			}
		}
		if ctx.Err() == nil {
			for elapsed := 0; elapsed < roomaudio.VoiceGuardTailMS; elapsed += roomaudio.FrameDurationMS {
				if err := publishFrame(silence, nextCursorMS); err != nil {
					if ctx.Err() == nil {
						log.Printf("room audio interrupt guard tail room=%d task=%s failed: %v", roomID, taskID, err)
					}
					return
				}
			}
		}
		if ctx.Err() == nil {
			c.finishReturn(roomID, taskID)
			terminalSent = true
			c.observeInterruptTerminal(roomID, taskID, "COMPLETED", nil)
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
			StartMS:   start + roomaudio.VoiceGuardHeadMS,
			EndMS:     end + roomaudio.VoiceGuardHeadMS,
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
