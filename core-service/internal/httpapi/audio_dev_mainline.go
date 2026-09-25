package httpapi

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultDevMainlineSRTPath    = `E:\直播伴播\测试素材\母带时间轴测试\mainline_same_tts.srt`
	defaultDevSafePointsCSVPath  = `E:\直播伴播\测试素材\母带时间轴测试\safe_points_v3.csv`
	defaultDevTimelineJSONPath   = `E:\直播伴播\测试素材\母带时间轴测试\mainline_same_tts_timeline.json`
	defaultDevInteractionLogPath = `E:\直播伴播\data\logs\live-interaction-evaluations.jsonl`
)

type devMainlineSentence struct {
	ID          string   `json:"id"`
	StartMS     int      `json:"start_ms"`
	EndMS       int      `json:"end_ms"`
	PlayStartMS int      `json:"play_start_ms,omitempty"`
	PlayEndMS   int      `json:"play_end_ms,omitempty"`
	Text        string   `json:"text"`
	Topics      []string `json:"topics,omitempty"`
}

type devMainlineSafePoint struct {
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

type devMainlineMap struct {
	DurationMS int                    `json:"duration_ms"`
	Sentences  []devMainlineSentence  `json:"sentences"`
	SafePoints []devMainlineSafePoint `json:"safe_points"`
}

type devTimelineJSON struct {
	AudioDurationMS int                    `json:"audio_duration_ms"`
	Sentences       []devMainlineSentence  `json:"sentences"`
	SafePoints      []devMainlineSafePoint `json:"safe_points"`
}

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

var devMainlineCache struct {
	once sync.Once
	data devMainlineMap
	err  error
}

func devMainlineFiles() (string, string) {
	srt := strings.TrimSpace(os.Getenv("CORE_DEV_MAINLINE_SRT"))
	if srt == "" {
		srt = defaultDevMainlineSRTPath
	}
	csvPath := strings.TrimSpace(os.Getenv("CORE_DEV_MAINLINE_SAFE_POINTS"))
	if csvPath == "" {
		csvPath = defaultDevSafePointsCSVPath
	}
	return srt, csvPath
}

func loadDevMainlineMap() (devMainlineMap, error) {
	devMainlineCache.once.Do(func() {
		timelinePath := strings.TrimSpace(os.Getenv("CORE_DEV_MAINLINE_TIMELINE"))
		if timelinePath == "" {
			timelinePath = defaultDevTimelineJSONPath
		}
		if raw, err := os.ReadFile(timelinePath); err == nil {
			var timeline devTimelineJSON
			if err := json.Unmarshal(raw, &timeline); err == nil && len(timeline.Sentences) > 0 {
				devMainlineCache.data = devMainlineMap{
					DurationMS: timeline.AudioDurationMS,
					Sentences:  timeline.Sentences,
					SafePoints: timeline.SafePoints,
				}
				return
			}
		}

		srtPath, csvPath := devMainlineFiles()
		sentences, err := parseDevSRT(srtPath)
		if err != nil {
			devMainlineCache.err = err
			return
		}
		points, err := parseDevSafePoints(csvPath)
		if err != nil {
			devMainlineCache.err = err
			return
		}
		duration := 0
		if len(sentences) > 0 {
			duration = sentences[len(sentences)-1].EndMS
		}
		if len(points) > 0 && points[len(points)-1].CutMS > duration {
			duration = points[len(points)-1].CutMS
		}
		devMainlineCache.data = devMainlineMap{DurationMS: duration, Sentences: sentences, SafePoints: points}
	})
	return devMainlineCache.data, devMainlineCache.err
}

func parseDevSRT(path string) ([]devMainlineSentence, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	blocks := strings.Split(text, "\n\n")
	items := make([]devMainlineSentence, 0, len(blocks))
	for _, block := range blocks {
		lines := strings.Split(strings.TrimSpace(block), "\n")
		if len(lines) < 3 {
			continue
		}
		parts := strings.Split(lines[1], " --> ")
		if len(parts) != 2 {
			continue
		}
		start, err1 := parseDevSRTTime(parts[0])
		end, err2 := parseDevSRTTime(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}
		items = append(items, devMainlineSentence{
			ID:      "S" + strings.TrimSpace(lines[0]),
			StartMS: start,
			EndMS:   end,
			Text:    strings.TrimSpace(strings.Join(lines[2:], " ")),
		})
	}
	if len(items) == 0 {
		return nil, errors.New("mainline SRT has no usable cues")
	}
	return items, nil
}

func parseDevSRTTime(value string) (int, error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, errors.New("invalid srt time")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	secondParts := strings.Split(parts[2], ",")
	if len(secondParts) != 2 {
		return 0, errors.New("invalid srt seconds")
	}
	second, err := strconv.Atoi(secondParts[0])
	if err != nil {
		return 0, err
	}
	ms, err := strconv.Atoi(secondParts[1])
	if err != nil {
		return 0, err
	}
	return ((hour*60+minute)*60+second)*1000 + ms, nil
}

func parseDevSafePoints(path string) ([]devMainlineSafePoint, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(bufio.NewReader(file))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	items := make([]devMainlineSafePoint, 0, len(records))
	for i, row := range records {
		if i == 0 || len(row) < 8 {
			continue
		}
		cutMS, err1 := strconv.Atoi(strings.TrimSpace(row[1]))
		score, err2 := strconv.Atoi(strings.TrimSpace(row[2]))
		if err1 != nil || err2 != nil {
			continue
		}
		items = append(items, devMainlineSafePoint{
			ID:          strings.TrimSpace(strings.TrimPrefix(row[0], "\ufeff")),
			CutMS:       cutMS,
			Score:       score,
			Grade:       strings.TrimSpace(row[3]),
			Kind:        strings.TrimSpace(row[4]),
			SentenceID:  strings.TrimSpace(row[5]),
			LeftPreview: strings.TrimSpace(row[6]),
			NextPreview: strings.TrimSpace(row[7]),
		})
	}
	if len(items) == 0 {
		return nil, errors.New("safe point csv has no usable rows")
	}
	return items, nil
}

func devMainlineContextAt(cutMS int) (devMainlineSafePoint, bool) {
	data, err := loadDevMainlineMap()
	if err != nil || len(data.SafePoints) == 0 {
		return devMainlineSafePoint{}, false
	}
	best := data.SafePoints[0]
	bestDistance := absInt(best.CutMS - cutMS)
	for _, point := range data.SafePoints[1:] {
		distance := absInt(point.CutMS - cutMS)
		if distance < bestDistance {
			best = point
			bestDistance = distance
		}
	}
	return best, true
}

func devMainlineNextSafePoint(afterMS int, maxWait time.Duration) (devMainlineSafePoint, bool) {
	data, err := loadDevMainlineMap()
	if err != nil || len(data.SafePoints) == 0 {
		return devMainlineSafePoint{}, false
	}
	maxDelta := int(maxWait.Milliseconds())
	var fallback devMainlineSafePoint
	for _, point := range data.SafePoints {
		delta := point.CutMS - afterMS
		if delta < 120 {
			continue
		}
		if maxDelta > 0 && delta > maxDelta {
			break
		}
		if fallback.ID == "" {
			fallback = point
		}
		grade := strings.ToUpper(strings.TrimSpace(point.Grade))
		if grade == "A" || grade == "B" {
			return point, true
		}
	}
	if fallback.ID != "" {
		return fallback, true
	}
	return devMainlineSafePoint{}, false
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
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
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	data, err := loadDevMainlineMap()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取主线时间轴失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, data)
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
