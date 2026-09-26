package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/decisionexecutor"
)

func (s *Server) getRoomAgentDecisions(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodGet,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions", roomID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "小蓝Agent决策状态暂时不可用")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) simulateRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	var input struct {
		Question string `json:"question"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "测试问题格式错误")
		return
	}
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" {
		writeError(w, http.StatusBadRequest, "测试问题不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	worker := decisionexecutor.New(s.store, nil, agentgateway.NewFromEnv(), nil)
	result, err := worker.SimulateAnswer(ctx, tenantID, roomID, input.Question)
	if err != nil {
		writeError(w, http.StatusBadGateway, "测试智能体处理失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) enqueueRoomManualAgentDecision(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	var input struct {
		Question      string `json:"question"`
		Topic         string `json:"topic"`
		Title         string `json:"title"`
		Summary       string `json:"summary"`
		ReplyHint     string `json:"reply_hint"`
		EventID       int64  `json:"event_id"`
		UserID        string `json:"user_id"`
		ForceReopen   bool   `json:"force_reopen"`
		ManualAction  string `json:"manual_action"`
		ManualOrigin  string `json:"manual_origin"`
		ExecutionMode string `json:"execution_mode"`
		FixedText     string `json:"fixed_text"`
		TTLSeconds    int    `json:"ttl_seconds"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "人工挑选信息格式错误")
		return
	}
	input.Question = strings.TrimSpace(input.Question)
	input.Topic = strings.TrimSpace(input.Topic)
	input.ManualAction = strings.ToLower(strings.TrimSpace(input.ManualAction))
	input.ManualOrigin = strings.ToLower(strings.TrimSpace(input.ManualOrigin))
	input.ExecutionMode = strings.ToLower(strings.TrimSpace(input.ExecutionMode))
	input.FixedText = strings.TrimSpace(input.FixedText)
	if input.ManualOrigin != "agent_input" && input.ManualOrigin != "question_cluster" && input.ManualOrigin != "test_simulation" {
		input.ManualOrigin = ""
	}
	if input.ExecutionMode != "verbatim" {
		input.ExecutionMode = "intent"
		input.FixedText = ""
	}
	if input.TTLSeconds < 0 {
		input.TTLSeconds = 0
	}
	if input.ManualAction != "quick" {
		input.ManualAction = "answer"
	}
	if input.Question == "" && input.Topic == "" {
		writeError(w, http.StatusBadRequest, "请选择问题或问题桶")
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/manual", roomID),
		query,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "人工加入打断队列失败")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) removeRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	decisionID := strings.TrimSpace(r.PathValue("decisionID"))
	if decisionID == "" {
		writeError(w, http.StatusBadRequest, "缺少决策ID")
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodDelete,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s", roomID, decisionID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "移除待打断任务失败")
		return
	}
	s.copyCoreResponse(w, resp)
}

func (s *Server) completeRoomAgentDecision(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	decisionID := strings.TrimSpace(r.PathValue("decisionID"))
	if decisionID == "" {
		writeError(w, http.StatusBadRequest, "缺少决策ID")
		return
	}
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))
	resp, err := s.core.DoRoom(
		r.Context(),
		tenantID,
		roomID,
		http.MethodPost,
		fmt.Sprintf("/internal/v1/rooms/%d/agent-decisions/%s/complete", roomID, decisionID),
		query,
		nil,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "记录回答完成失败")
		return
	}
	s.copyCoreResponse(w, resp)
}
