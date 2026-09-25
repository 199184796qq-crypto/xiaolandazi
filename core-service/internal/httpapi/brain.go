package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/timeline"
)

type roomBrain interface {
	Ingest(event model.RoomEvent)
	Snapshot(roomID int64) (roombrain.View, error)
	RecordPin(roomID int64, pin timeline.Pin, spend timeline.DebtKind, cooldown time.Duration)
	Reset(roomID int64)
}

type roomBrainTopicMerger interface {
	MergeTopics(roomID int64, representative string, sources []string) int
}

func (s *Server) SetRoomBrain(brain roomBrain) {
	s.brain = brain
}

func (s *Server) getRoomBrain(w http.ResponseWriter, r *http.Request) {
	if s.brain == nil {
		writeError(w, http.StatusServiceUnavailable, "room brain is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type roomBrainTopicMergeInput struct {
	Groups []struct {
		RepresentativeTopic string   `json:"representative_topic"`
		SourceTopics        []string `json:"source_topics"`
	} `json:"groups"`
}

func (s *Server) mergeRoomBrainTopics(w http.ResponseWriter, r *http.Request) {
	if s.brain == nil {
		writeError(w, http.StatusServiceUnavailable, "room brain is not configured")
		return
	}
	merger, ok := s.brain.(roomBrainTopicMerger)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "room brain topic merge is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	var input roomBrainTopicMergeInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(input.Groups) == 0 || len(input.Groups) > 20 {
		writeError(w, http.StatusBadRequest, "groups must contain 1..20 items")
		return
	}
	merged := 0
	for _, group := range input.Groups {
		representative := strings.TrimSpace(group.RepresentativeTopic)
		if representative == "" || len(group.SourceTopics) == 0 || len(group.SourceTopics) > 20 {
			continue
		}
		sources := make([]string, 0, len(group.SourceTopics))
		seen := map[string]struct{}{}
		for _, source := range group.SourceTopics {
			source = strings.TrimSpace(source)
			if source == "" || source == representative {
				continue
			}
			if _, exists := seen[source]; exists {
				continue
			}
			seen[source] = struct{}{}
			sources = append(sources, source)
		}
		if len(sources) == 0 {
			continue
		}
		merged += merger.MergeTopics(roomID, representative, sources)
	}
	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"merged_topics": merged,
		"brain":         view,
	})
}

type roomBrainPinInput struct {
	Kind            string
	Strategy        string
	Topic           string
	Key             string
	TextDigest      string
	MainlineUnit    string
	Metadata        map[string]string
	OccurredAt      time.Time
	SpendDebt       string
	CooldownSeconds int
}

func (s *Server) recordRoomBrainPin(w http.ResponseWriter, r *http.Request) {
	if s.brain == nil {
		writeError(w, http.StatusServiceUnavailable, "room brain is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	var input roomBrainPinInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.Kind = strings.TrimSpace(strings.ToUpper(input.Kind))
	if input.Kind == "" {
		writeError(w, http.StatusBadRequest, "Kind is required")
		return
	}
	if input.CooldownSeconds < 0 || input.CooldownSeconds > 86400 {
		writeError(w, http.StatusBadRequest, "CooldownSeconds is invalid")
		return
	}

	spend := timeline.DebtKind(strings.TrimSpace(strings.ToUpper(input.SpendDebt)))
	if spend != "" && !validDebtKind(spend) {
		writeError(w, http.StatusBadRequest, "SpendDebt is invalid")
		return
	}

	s.brain.RecordPin(roomID, timeline.Pin{
		At:           input.OccurredAt,
		Kind:         timeline.PinKind(input.Kind),
		Strategy:     strings.TrimSpace(input.Strategy),
		Topic:        strings.TrimSpace(input.Topic),
		Key:          strings.TrimSpace(input.Key),
		TextDigest:   strings.TrimSpace(input.TextDigest),
		MainlineUnit: strings.TrimSpace(input.MainlineUnit),
		Metadata:     input.Metadata,
	}, spend, time.Duration(input.CooldownSeconds)*time.Second)

	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func validDebtKind(kind timeline.DebtKind) bool {
	switch kind {
	case timeline.DebtInteraction,
		timeline.DebtLikeCTA,
		timeline.DebtFollowCTA,
		timeline.DebtConversion,
		timeline.DebtQuestion:
		return true
	default:
		return false
	}
}

type roomBrainScenarioInput struct {
	Scenario string
}

func (s *Server) simulateRoomBrainScenario(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if s.brain == nil {
		writeError(w, http.StatusServiceUnavailable, "room brain is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	var input roomBrainScenarioInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	scenario := strings.ToLower(strings.TrimSpace(input.Scenario))
	if scenario == "" {
		writeError(w, http.StatusBadRequest, "Scenario is required")
		return
	}

	s.brain.Reset(roomID)
	now := time.Now().UTC()
	emit := func(eventType, userID, content string, at time.Time, payload map[string]any) {
		raw, _ := json.Marshal(payload)
		s.brain.Ingest(model.RoomEvent{
			RoomID: roomID, EventType: eventType, UserID: userID,
			Content: content, OccurredAt: at, Payload: raw,
		})
	}

	switch scenario {
	case "cold":
		emit("room", "", "", now.Add(-20*time.Second), map[string]any{"online_count": 18})
		emit("member", "u1", "进入直播间", now.Add(-12*time.Second), map[string]any{"member_count": 18})
		emit("chat", "u2", "主播你好", now.Add(-3*time.Second), nil)
	case "warm":
		emit("room", "", "", now.Add(-20*time.Second), map[string]any{"online_count": 80})
		for i := 0; i < 8; i++ {
			emit("member", "m"+strconv.Itoa(i), "进入直播间", now.Add(-15*time.Second), nil)
		}
		emit("chat", "u1", "这个怎么吃？", now.Add(-4*time.Second), nil)
		emit("like", "u2", "点赞", now.Add(-2*time.Second), map[string]any{"count": 12})
	case "hot":
		emit("room", "", "", now.Add(-20*time.Second), map[string]any{"online_count": 1000})
		for i := 0; i < 80; i++ {
			emit("member", "m"+strconv.Itoa(i), "进入直播间", now.Add(-15*time.Second), nil)
		}
		for i := 0; i < 22; i++ {
			emit("chat", "p"+strconv.Itoa(i), "现在多少钱？", now.Add(-6*time.Second), nil)
		}
		for i := 0; i < 15; i++ {
			emit("chat", "s"+strconv.Itoa(i), "什么时候发货？", now.Add(-5*time.Second), nil)
		}
		for i := 0; i < 8; i++ {
			emit("chat", "e"+strconv.Itoa(i), "这个怎么吃？", now.Add(-4*time.Second), nil)
		}
		emit("like", "u-like", "点赞", now.Add(-3*time.Second), map[string]any{"count": 300})
		for i := 0; i < 10; i++ {
			emit("follow", "f"+strconv.Itoa(i), "关注了主播", now.Add(-3*time.Second), nil)
		}
		emit("order", "buyer", "下单", now.Add(-2*time.Second), map[string]any{"count": 3})
	case "complaint":
		emit("room", "", "", now.Add(-20*time.Second), map[string]any{"online_count": 55})
		emit("chat", "u1", "收到坏了，我要退款投诉，可以吗？", now.Add(-2*time.Second), nil)
	default:
		writeError(w, http.StatusBadRequest, "unknown Scenario")
		return
	}

	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) resetRoomBrain(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if s.brain == nil {
		writeError(w, http.StatusServiceUnavailable, "room brain is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	s.brain.Reset(roomID)
	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, view)
}
