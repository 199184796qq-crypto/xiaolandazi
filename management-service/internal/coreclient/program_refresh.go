package coreclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var ErrProgramRefreshConflict = errors.New("Core program refresh baseline conflict")
var ErrProgramRefreshNotFound = errors.New("Core program refresh room not found")

type ProgramRefreshTimelineSegment struct {
	SegmentID string `json:"segment_id"`
	Index     int    `json:"index"`
	StartMS   int    `json:"start_ms"`
	EndMS     int    `json:"end_ms"`
	Text      string `json:"text"`
	SafeCut   bool   `json:"safe_cut"`
}

type ProgramRefreshSafePoint struct {
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

type ProgramRefreshTrack struct {
	ID         string                          `json:"id,omitempty"`
	Label      string                          `json:"label,omitempty"`
	Text       string                          `json:"text,omitempty"`
	AudioURL   string                          `json:"audio_url"`
	DurationMS int                             `json:"duration_ms,omitempty"`
	Timeline   []ProgramRefreshTimelineSegment `json:"timeline,omitempty"`
	SafePoints []ProgramRefreshSafePoint       `json:"safe_points,omitempty"`
}

type ProgramRefreshInput struct {
	JobID              string                `json:"job_id"`
	ExpectedProgramID  string                `json:"expected_program_id"`
	ExpectedVersionID  int64                 `json:"expected_version_id"`
	ExpectedGeneration uint64                `json:"expected_generation"`
	Generation         uint64                `json:"generation"`
	NewVersionID       int64                 `json:"new_version_id"`
	NewVersionNo       int64                 `json:"new_version_no"`
	ValidUntil         time.Time             `json:"valid_until"`
	Tracks             []ProgramRefreshTrack `json:"tracks"`
}

type ProgramRefreshSnapshot struct {
	ProgramID         string `json:"program_id"`
	RoomID            int64  `json:"room_id"`
	VersionID         int64  `json:"version_id"`
	VersionNo         int64  `json:"version_no"`
	Generation        uint64 `json:"generation"`
	PendingGeneration uint64 `json:"pending_generation"`
	PendingJobID      string `json:"pending_job_id"`
	LastAppliedJobID  string `json:"last_applied_job_id"`
	Running           bool   `json:"running"`
	Suspended         bool   `json:"suspended"`
	Sequence          uint64 `json:"sequence"`
}

func (c *Client) RefreshRoomProgram(ctx context.Context, tenantID, roomID int64, input ProgramRefreshInput) (ProgramRefreshSnapshot, error) {
	return c.roomProgramRefreshRequest(ctx, tenantID, roomID, http.MethodPost, "/refresh", input)
}

func (c *Client) GetRoomProgramSnapshot(ctx context.Context, tenantID, roomID int64) (ProgramRefreshSnapshot, error) {
	return c.roomProgramRefreshRequest(ctx, tenantID, roomID, http.MethodGet, "", nil)
}

func (c *Client) CancelRoomProgramRefresh(ctx context.Context, tenantID, roomID int64, jobID, expectedProgramID string) (ProgramRefreshSnapshot, error) {
	return c.roomProgramRefreshRequest(ctx, tenantID, roomID, http.MethodPost, "/refresh/cancel", map[string]string{"job_id": jobID, "expected_program_id": expectedProgramID})
}

func (c *Client) RenewRoomProgramRefresh(ctx context.Context, tenantID, roomID int64, jobID, expectedProgramID string, validUntil time.Time) (ProgramRefreshSnapshot, error) {
	return c.roomProgramRefreshRequest(ctx, tenantID, roomID, http.MethodPost, "/refresh/renew", map[string]any{"job_id": jobID, "expected_program_id": expectedProgramID, "valid_until": validUntil})
}

func (c *Client) roomProgramRefreshRequest(ctx context.Context, tenantID, roomID int64, method, suffix string, input any) (ProgramRefreshSnapshot, error) {
	var result ProgramRefreshSnapshot
	// Program state is process-local. Never fail over a baseline read to a
	// different node that could report another session or no active program.
	resp, err := c.doAt(ctx, c.baseURLForRoom(tenantID, roomID), method, fmt.Sprintf("/internal/v1/rooms/%d/audio/program%s", roomID, suffix), url.Values{"tenant_id": {strconv.FormatInt(tenantID, 10)}}, input)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return result, err
	}
	if resp.StatusCode == http.StatusConflict {
		return result, fmt.Errorf("%w: %s", ErrProgramRefreshConflict, raw)
	}
	if resp.StatusCode == http.StatusNotFound {
		return result, fmt.Errorf("%w: %s", ErrProgramRefreshNotFound, raw)
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("Core program refresh http %d: %s", resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, fmt.Errorf("decode Core program snapshot: %w", err)
	}
	return result, nil
}
