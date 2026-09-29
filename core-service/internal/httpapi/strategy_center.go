package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/strategycenter"
)

func (s *Server) strategyPolicy(w http.ResponseWriter, r *http.Request) {
	tenantID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("tenantID")), 10, 64)
	if err != nil || tenantID < 0 {
		writeError(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.strategyPolicies.Resolve(tenantID))
		return
	}
	var policy strategycenter.Policy
	if err := readJSON(w, r, &policy); err != nil {
		writeError(w, http.StatusBadRequest, "invalid strategy policy")
		return
	}
	policy = s.strategyPolicies.Put(tenantID, policy)
	log.Printf("strategy policy updated tenant=%d rules=%d addressing=%d mode=%s", tenantID, len(policy.Rules), len(policy.Addressing), policy.AddressingMode)
	writeJSON(w, http.StatusOK, policy)
}

func (s *Server) roomInteractionPreferences(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.strategyPolicies.RoomInteractionPreferences(roomID))
		return
	}
	var input strategycenter.RoomInteractionPreferences
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid interaction preferences")
		return
	}
	input.TenantID = tenantID
	input.RoomID = roomID
	item := s.strategyPolicies.PutRoomInteractionPreferences(roomID, input)
	log.Printf(
		"room interaction preferences updated room=%d tenant=%d overall=%s question=%s welcome=%s engagement=%s chat=%s conversion=%s",
		roomID,
		tenantID,
		item.OverallInteraction,
		item.QuestionPreference,
		item.WelcomePreference,
		item.EngagementPreference,
		item.ChatPreference,
		item.ConversionPreference,
	)
	writeJSON(w, http.StatusOK, item)
}

type roomStrategySelectInput struct {
	Category    string                 `json:"category"`
	Candidates  []string               `json:"candidates,omitempty"`
	Seed        string                 `json:"seed,omitempty"`
	Topic       string                 `json:"topic,omitempty"`
	EstimatedMS int                    `json:"estimated_ms,omitempty"`
	Signals     strategycenter.Signals `json:"signals,omitempty"`
}

type roomStrategySelectResponse struct {
	strategycenter.Selection
	PlannedCutMS    int      `json:"planned_cut_ms,omitempty"`
	ResumeOffsetMS  int      `json:"resume_offset_ms,omitempty"`
	ResumeReason    string   `json:"resume_reason,omitempty"`
	ResumePreview   string   `json:"resume_preview,omitempty"`
	ResumeSegmentID string   `json:"resume_segment_id,omitempty"`
	SkippedPreviews []string `json:"skipped_previews,omitempty"`
}

func (s *Server) roomStrategyStage(ctx context.Context, roomID int64) (string, time.Time, bool) {
	if s == nil || s.events == nil || roomID <= 0 {
		return "", time.Time{}, false
	}
	stats, err := s.events.GetSessionStats(ctx, roomID)
	if err != nil || stats.StartedAt.IsZero() {
		return "", time.Time{}, false
	}
	startedAt := stats.StartedAt.UTC()
	return "live:" + strconv.FormatInt(roomID, 10) + ":" + startedAt.Format(time.RFC3339Nano), startedAt, true
}

func (s *Server) roomStrategyStatsSnapshot(ctx context.Context, roomID, tenantID int64) strategycenter.StageStats {
	result := s.strategyPolicies.StageStatsForTenant(roomID, tenantID)
	if stageID, startedAt, active := s.roomStrategyStage(ctx, roomID); active {
		result.StageID = stageID
		result.StartedAt = startedAt
		if result.UpdatedAt.IsZero() {
			result.UpdatedAt = time.Now().UTC()
		}
	}
	return result
}

func (s *Server) getRoomStrategyStats(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	writeJSON(w, http.StatusOK, s.roomStrategyStatsSnapshot(r.Context(), roomID, tenantID))
}

func writeStrategyStatsEvent(w http.ResponseWriter, event string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
	return err
}

func (s *Server) streamRoomStrategyStats(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	updates, cancel := s.strategyPolicies.SubscribeStats(roomID)
	defer cancel()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-updates:
			if !open {
				return
			}
			if err := writeStrategyStatsEvent(w, "stats", s.roomStrategyStatsSnapshot(r.Context(), roomID, tenantID)); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) selectRoomStrategy(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	var input roomStrategySelectInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid strategy select body")
		return
	}
	if s.strategyPolicies == nil {
		s.strategyPolicies = strategycenter.New()
	}
	if s.brain != nil {
		if view, err := s.brain.Snapshot(roomID); err == nil {
			if input.Signals.Entries30s == 0 {
				input.Signals.Entries30s = view.Intelligence.Entries30s
			}
			if input.Signals.Likes30s == 0 {
				input.Signals.Likes30s = view.Intelligence.Likes30s
			}
			if input.Signals.Follows30s == 0 {
				input.Signals.Follows30s = view.Intelligence.Follows30s
			}
			if strings.TrimSpace(input.Signals.Heat) == "" {
				input.Signals.Heat = view.Intelligence.Heat
			}
		}
	}
	var resumeProgram *audioout.RoomProgramSnapshot
	plannedCutMS := 0
	if strings.EqualFold(strings.TrimSpace(input.Category), "resume") && len(input.Candidates) == 0 {
		input.Candidates = []string{"DIRECT", "BRIDGE"}
		if state := s.audioDevState(); state != nil && state.client != nil && state.client.Enabled() {
			if program, programErr := state.client.ProgramSnapshot(r.Context(), roomID); programErr == nil && program.Running && program.Task != nil {
				currentMS := program.CurrentMS
				if currentMS <= 0 {
					currentMS = program.Task.StartMS
				}
				cutMS := program.NextSafeCutMS
				if cutMS <= currentMS {
					if resolved, ok := resolveRoomProgramSafeCutAfterGeneration(program, 0, currentMS); ok {
						cutMS = resolved
					}
				}
				estimatedMS := input.EstimatedMS
				if estimatedMS <= 0 {
					estimatedMS = 10000
				}
				input.Candidates = resumeCandidatesForInteraction(program, cutMS, estimatedMS, input.Topic)
				resumeProgram = &program
				plannedCutMS = cutMS
			}
		}
	}
	var selected strategycenter.Selection
	if strings.EqualFold(strings.TrimSpace(input.Category), "addressing") {
		selected = s.strategyPolicies.PickAddressing(tenantID, input.Seed)
	} else {
		selected = s.strategyPolicies.Pick(tenantID, input.Category, input.Candidates, input.Signals, input.Seed)
	}
	if stageID, startedAt, active := s.roomStrategyStage(r.Context(), roomID); active {
		s.strategyPolicies.RecordSelection(roomID, stageID, startedAt, selected)
	}
	log.Printf("strategy select tenant=%d room=%d category=%s candidates=%v weights=%v selected=%s roll=%d/%d heat=%s entries30s=%d likes30s=%d", tenantID, roomID, selected.Category, input.Candidates, selected.Candidates, selected.Key, selected.Roll, selected.Total, input.Signals.Heat, input.Signals.Entries30s, input.Signals.Likes30s)
	response := roomStrategySelectResponse{Selection: selected}
	if resumeProgram != nil && strings.EqualFold(strings.TrimSpace(input.Category), "resume") {
		response.PlannedCutMS = plannedCutMS
		response.ResumeOffsetMS, response.ResumeReason = resumeOffsetForStrategy(*resumeProgram, plannedCutMS, selected.Key, input.Topic)
		if point, ok := resumePointAtOrAfter(*resumeProgram, response.ResumeOffsetMS); ok {
			response.ResumePreview = resumePreview(*resumeProgram, point)
			response.ResumeSegmentID = point.SentenceID
		}
		if response.ResumeOffsetMS > plannedCutMS {
			for _, point := range effectiveRoomProgramSafePoints(*resumeProgram) {
				if point.CutMS <= plannedCutMS || point.CutMS >= response.ResumeOffsetMS {
					continue
				}
				if preview := strings.TrimSpace(resumePreview(*resumeProgram, point)); preview != "" {
					response.SkippedPreviews = append(response.SkippedPreviews, preview)
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, response)
}
