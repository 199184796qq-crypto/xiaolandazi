package audioout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type CreateTestTaskInput struct {
	RoomID       int64
	SessionID    string
	SpeechTaskID string
	Label        string
	DurationMS   int
	CallbackURL  string
}

type CreateExternalTaskInput struct {
	RoomID      int64
	SessionID   string
	Label       string
	AudioURL    string
	CallbackURL string
}

type InsertInteractionInput struct {
	RoomID         int64
	SessionID      string
	Label          string
	AudioURL       string
	Text           string
	CallbackURL    string
	ResumeOffsetMS *int
	SwitchAtMS     *int
}

type ProgramTrack struct {
	ID         string                   `json:"id,omitempty"`
	Label      string                   `json:"label,omitempty"`
	Text       string                   `json:"text,omitempty"`
	AudioURL   string                   `json:"audio_url"`
	DurationMS int                      `json:"duration_ms,omitempty"`
	Timeline   []ProgramTimelineSegment `json:"timeline,omitempty"`
	SafePoints []ProgramSafePoint       `json:"safe_points,omitempty"`
}

type ProgramTimelineSegment struct {
	SegmentID string `json:"segment_id"`
	Index     int    `json:"index"`
	StartMS   int    `json:"start_ms"`
	EndMS     int    `json:"end_ms"`
	Text      string `json:"text"`
	SafeCut   bool   `json:"safe_cut"`
}

type ProgramSafePoint struct {
	ID          string   `json:"id"`
	CutMS       int      `json:"cut_ms"`
	Score       int      `json:"score"`
	Grade       string   `json:"grade"`
	Kind        string   `json:"kind"`
	SentenceID  string   `json:"sentence_id"`
	LeftPreview string   `json:"left_preview"`
	NextPreview string   `json:"next_preview"`
	Topics      []string `json:"topics,omitempty"`
}

type StartProgramInput struct {
	RoomID    int64          `json:"room_id"`
	SessionID string         `json:"session_id,omitempty"`
	Label     string         `json:"label,omitempty"`
	VersionID int64          `json:"version_id,omitempty"`
	VersionNo int64          `json:"version_no,omitempty"`
	Tracks    []ProgramTrack `json:"tracks"`
}

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

type RoomProgramSnapshot struct {
	ProgramID          string                   `json:"program_id,omitempty"`
	RoomID             int64                    `json:"room_id"`
	VersionID          int64                    `json:"version_id,omitempty"`
	VersionNo          int64                    `json:"version_no,omitempty"`
	TrackID            string                   `json:"track_id,omitempty"`
	TrackIndex         int                      `json:"track_index,omitempty"`
	TrackCount         int                      `json:"track_count,omitempty"`
	TrackText          string                   `json:"track_text,omitempty"`
	Timeline           []ProgramTimelineSegment `json:"timeline,omitempty"`
	SafePoints         []ProgramSafePoint       `json:"safe_points,omitempty"`
	CurrentMS          int                      `json:"current_ms,omitempty"`
	CurrentSegment     *ProgramTimelineSegment  `json:"current_segment,omitempty"`
	NextSafeCutMS      int                      `json:"next_safe_cut_ms,omitempty"`
	Running            bool                     `json:"running"`
	Suspended          bool                     `json:"suspended,omitempty"`
	ResumeOffsetMS     int                      `json:"resume_offset_ms,omitempty"`
	PlannedNextTrackID string                   `json:"planned_next_track_id,omitempty"`
	PlannedNextScore   int                      `json:"planned_next_score,omitempty"`
	PlannedNextReason  string                   `json:"planned_next_reason,omitempty"`
	Sequence           uint64                   `json:"sequence,omitempty"`
	Slot               string                   `json:"slot,omitempty"`
	Task               *SpeechTask              `json:"task,omitempty"`
	StartedAt          time.Time                `json:"started_at,omitempty"`
	ServerTime         time.Time                `json:"server_time"`
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

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		http:    &http.Client{Timeout: 4 * time.Second},
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.baseURL != ""
}

func (c *Client) CreateTestTask(ctx context.Context, input CreateTestTaskInput) (SpeechTask, error) {
	if !c.Enabled() {
		return SpeechTask{}, fmt.Errorf("audio service is not configured")
	}
	if input.RoomID <= 0 || strings.TrimSpace(input.SessionID) == "" {
		return SpeechTask{}, fmt.Errorf("room_id and session_id are required")
	}
	body, err := json.Marshal(map[string]any{
		"speech_task_id": input.SpeechTaskID,
		"label":          input.Label,
		"duration_ms":    input.DurationMS,
		"callback_url":   input.CallbackURL,
	})
	if err != nil {
		return SpeechTask{}, err
	}
	endpoint := c.baseURL + "/internal/v1/rooms/" + strconv.FormatInt(input.RoomID, 10) +
		"/sessions/" + url.PathEscape(strings.TrimSpace(input.SessionID)) + "/tasks/test-tone"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SpeechTask{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Audio-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return SpeechTask{}, fmt.Errorf("audio service request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechTask{}, fmt.Errorf("audio service http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var task SpeechTask
	if err := json.Unmarshal(raw, &task); err != nil {
		return SpeechTask{}, fmt.Errorf("decode audio task: %w", err)
	}
	return task, nil
}

func (c *Client) CreateExternalTask(ctx context.Context, input CreateExternalTaskInput) (SpeechTask, error) {
	if !c.Enabled() {
		return SpeechTask{}, fmt.Errorf("audio service is not configured")
	}
	if input.RoomID <= 0 || strings.TrimSpace(input.SessionID) == "" {
		return SpeechTask{}, fmt.Errorf("room_id and session_id are required")
	}
	if strings.TrimSpace(input.AudioURL) == "" {
		return SpeechTask{}, fmt.Errorf("audio_url is required")
	}
	body, err := json.Marshal(map[string]any{
		"label":        strings.TrimSpace(input.Label),
		"audio_url":    strings.TrimSpace(input.AudioURL),
		"callback_url": strings.TrimSpace(input.CallbackURL),
	})
	if err != nil {
		return SpeechTask{}, err
	}
	endpoint := c.baseURL + "/internal/v1/rooms/" + strconv.FormatInt(input.RoomID, 10) +
		"/sessions/" + url.PathEscape(strings.TrimSpace(input.SessionID)) + "/tasks/external-wav"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return SpeechTask{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Audio-Token", c.token)
	}
	client := &http.Client{Timeout: 50 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SpeechTask{}, fmt.Errorf("audio service external task request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SpeechTask{}, fmt.Errorf("audio service http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var task SpeechTask
	if err := json.Unmarshal(raw, &task); err != nil {
		return SpeechTask{}, fmt.Errorf("decode audio task: %w", err)
	}
	return task, nil
}

func (c *Client) StartTestProgram(ctx context.Context, roomID int64, sessionID, label, callbackURL string) (RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service is not configured")
	}
	if roomID <= 0 {
		return RoomProgramSnapshot{}, fmt.Errorf("room_id is required")
	}
	body, err := json.Marshal(map[string]any{
		"session_id":   strings.TrimSpace(sessionID),
		"label":        strings.TrimSpace(label),
		"callback_url": strings.TrimSpace(callbackURL),
	})
	if err != nil {
		return RoomProgramSnapshot{}, err
	}
	endpoint := c.baseURL + "/internal/v1/rooms/" + strconv.FormatInt(roomID, 10) + "/program/test-loop/start"
	return c.doProgramRequest(ctx, http.MethodPost, endpoint, body)
}

func (c *Client) InsertTestProgramInteraction(ctx context.Context, input InsertInteractionInput) (RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service is not configured")
	}
	if input.RoomID <= 0 {
		return RoomProgramSnapshot{}, fmt.Errorf("room_id is required")
	}
	if strings.TrimSpace(input.AudioURL) == "" {
		return RoomProgramSnapshot{}, fmt.Errorf("audio_url is required")
	}
	body, err := json.Marshal(map[string]any{
		"session_id":       strings.TrimSpace(input.SessionID),
		"label":            strings.TrimSpace(input.Label),
		"audio_url":        strings.TrimSpace(input.AudioURL),
		"text":             strings.TrimSpace(input.Text),
		"callback_url":     strings.TrimSpace(input.CallbackURL),
		"resume_offset_ms": input.ResumeOffsetMS,
		"switch_at_ms":     input.SwitchAtMS,
	})
	if err != nil {
		return RoomProgramSnapshot{}, err
	}
	endpoint := c.baseURL + "/internal/v1/rooms/" + strconv.FormatInt(input.RoomID, 10) + "/program/test-loop/interaction"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return RoomProgramSnapshot{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Audio-Token", c.token)
	}
	client := &http.Client{Timeout: 50 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service interaction request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var snapshot RoomProgramSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return RoomProgramSnapshot{}, fmt.Errorf("decode room program interaction: %w", err)
	}
	return snapshot, nil
}

func (c *Client) ProgramSnapshot(ctx context.Context, roomID int64) (RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service is not configured")
	}
	if roomID <= 0 {
		return RoomProgramSnapshot{}, fmt.Errorf("room_id is required")
	}
	endpoint := c.baseURL + "/v1/rooms/" + strconv.FormatInt(roomID, 10) + "/sync"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return RoomProgramSnapshot{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service sync request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service sync http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var snapshot RoomProgramSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return RoomProgramSnapshot{}, fmt.Errorf("decode room program sync: %w", err)
	}
	return snapshot, nil
}

func (c *Client) StopTestProgram(ctx context.Context, roomID int64) (RoomProgramSnapshot, error) {
	if !c.Enabled() {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service is not configured")
	}
	if roomID <= 0 {
		return RoomProgramSnapshot{}, fmt.Errorf("room_id is required")
	}
	endpoint := c.baseURL + "/internal/v1/rooms/" + strconv.FormatInt(roomID, 10) + "/program/test-loop/stop"
	return c.doProgramRequest(ctx, http.MethodPost, endpoint, []byte("{}"))
}

func (c *Client) doProgramRequest(ctx context.Context, method, endpoint string, body []byte) (RoomProgramSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return RoomProgramSnapshot{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("X-Audio-Token", c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RoomProgramSnapshot{}, fmt.Errorf("audio service http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var snapshot RoomProgramSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return RoomProgramSnapshot{}, fmt.Errorf("decode room program: %w", err)
	}
	return snapshot, nil
}
