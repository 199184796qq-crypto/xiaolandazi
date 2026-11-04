package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultDevInteractionLogPath = `E:\直播伴播\data\logs\live-interaction-evaluations.jsonl`

type audioInteractionRecord struct {
	TaskID             string     `json:"task_id"`
	RoomID             int64      `json:"room_id"`
	SessionID          string     `json:"session_id"`
	CreatedAt          time.Time  `json:"created_at"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	Status             string     `json:"status"`
	Question           string     `json:"question"`
	StrategyChain      []string   `json:"strategy_chain"`
	DirectorProgress   string     `json:"director_progress"`
	DirectorAtmosphere string     `json:"director_atmosphere"`
	Humanization       string     `json:"humanization"`
	EntryMode          string     `json:"entry_mode,omitempty"`
	EntryLead          string     `json:"entry_lead,omitempty"`
	ResumeMode         string     `json:"resume_mode"`
	ResumeUnit         string     `json:"resume_unit"`
	CoveredTopics      []string   `json:"covered_topics,omitempty"`
	SkipUnits          []string   `json:"skip_units,omitempty"`
	ResumeOffsetMS     int        `json:"resume_offset_ms,omitempty"`
	StopMS             int        `json:"stop_ms"`
	StopSafePointID    string     `json:"stop_safe_point_id"`
	StopGrade          string     `json:"stop_grade"`
	StopKind           string     `json:"stop_kind"`
	StopText           string     `json:"stop_text"`
	ResumeText         string     `json:"resume_text"`
	ReplyCore          string     `json:"reply_core"`
	ResumeTail         string     `json:"resume_tail"`
	FinalText          string     `json:"final_text"`
	TargetSeconds      int        `json:"target_seconds"`
	TTSRate            float64    `json:"tts_rate"`
	ActualDurationMS   int        `json:"actual_duration_ms"`
	QualityScore       int        `json:"quality_score,omitempty"`
	QualityLevel       string     `json:"quality_level,omitempty"`
	QualitySummary     string     `json:"quality_summary,omitempty"`
	QualityIssues      []string   `json:"quality_issues,omitempty"`
	QualityAttempts    int        `json:"quality_attempts,omitempty"`
}

func appendDevInteractionRecord(record audioInteractionRecord) {
	path := strings.TrimSpace(os.Getenv("CORE_DEV_INTERACTION_LOG"))
	if path == "" {
		path = defaultDevInteractionLogPath
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.Write(append(raw, '\n'))
}

func (s *Server) getDevAudioMainlineMap(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusGone, "旧文字时间轴与CSV切点已永久移除")
}

func (s *Server) listDevAudioInteractions(w http.ResponseWriter, r *http.Request) {
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("room_id")), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	state.mu.Lock()
	source := state.history[roomID]
	items := make([]audioInteractionRecord, 0, len(source))
	for i := len(source) - 1; i >= 0; i-- {
		items = append(items, *source[i])
	}
	state.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) clearDevAudioInteractions(w http.ResponseWriter, r *http.Request) {
	state, ok := s.requireDevAudio(w)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("room_id")), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "room_id is required")
		return
	}
	state.mu.Lock()
	delete(state.history, roomID)
	for taskID, meta := range state.interactions {
		if meta != nil && meta.RoomID == roomID {
			delete(state.interactions, taskID)
		}
	}
	state.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"cleared": true})
}
