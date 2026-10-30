package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionplan"
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
		IncludeAnswerOutline bool                                       `json:"include_answer_outline,omitempty"`
		Question             string                                     `json:"question"`
		PlanID               int64                                      `json:"plan_id,omitempty"`
		PreviewInstruction   string                                     `json:"preview_instruction,omitempty"`
		RecentAnswers        []decisionexecutor.InteractionRecentAnswer `json:"recent_answers,omitempty"`
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
	if input.PlanID < 0 || utf8.RuneCountInString(input.Question) > 1000 || utf8.RuneCountInString(input.PreviewInstruction) > 4000 {
		writeError(w, http.StatusBadRequest, "测试问题或纠正内容过长，或方案编号无效")
		return
	}
	if input.PreviewInstruction != "" && input.PlanID == 0 {
		writeError(w, http.StatusBadRequest, "请明确选择测试方案")
		return
	}
	if err := decisionexecutor.ValidateInteractionRecentAnswers(input.RecentAnswers); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(input.RecentAnswers) > 0 && input.PlanID == 0 {
		writeError(w, http.StatusBadRequest, "请明确选择测试方案")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()

	if input.PlanID > 0 {
		_, authorizedTenant, authorizedRoom, allowed := s.requireCustomerPolicyRoom(w, r)
		if !allowed {
			return
		}
		if authorizedTenant != tenantID || authorizedRoom != roomID {
			writeError(w, 403, "测试范围不一致")
			return
		}
	} else {
		published, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenantID, roomID)
		if err != nil {
			writeError(w, 422, "请先选择已保存的测试方案")
			return
		}
		input.PlanID = published.PlanID
	}
	plan, err := s.store.GetLiveAgentPlan(ctx, tenantID, input.PlanID)
	if err != nil || plan.Status != "active" {
		writeError(w, 404, "当前方案不存在或不可测试")
		return
	}
	recent := make([]interactionplan.Turn, 0, len(input.RecentAnswers))
	for _, turn := range input.RecentAnswers {
		recent = append(recent, interactionplan.Turn{Question: turn.Question, Answer: turn.Reply})
	}
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	s.testInteractionV2(w, r.WithContext(ctx), interactionV2Input{Tenant: tenantID, Room: roomID, Plan: input.PlanID, Text: input.Question, Kind: "semantic", Test: true, Protocol: interactionplan.Protocol, Task: fmt.Sprintf("test-%d", time.Now().UnixNano()), Epoch: "test", Revision: 1, AllowClarify: true, Recent: recent, PreviewInstruction: input.PreviewInstruction, ExpiresAt: time.Now().Add(50 * time.Second)}, plan.Name)
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
		Question      string   `json:"question"`
		Topic         string   `json:"topic"`
		Title         string   `json:"title"`
		Summary       string   `json:"summary"`
		ReplyHint     string   `json:"reply_hint"`
		EventID       int64    `json:"event_id"`
		UserID        string   `json:"user_id"`
		Nickname      string   `json:"nickname,omitempty"`
		Nicknames     []string `json:"nicknames,omitempty"`
		ForceReopen   bool     `json:"force_reopen"`
		ManualAction  string   `json:"manual_action"`
		ManualOrigin  string   `json:"manual_origin"`
		MissionKind   string   `json:"mission_kind"`
		ExecutionMode string   `json:"execution_mode"`
		FixedText     string   `json:"fixed_text"`
		TTLSeconds    int      `json:"ttl_seconds"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "人工挑选信息格式错误")
		return
	}
	input.Question = strings.TrimSpace(input.Question)
	input.Topic = strings.TrimSpace(input.Topic)
	input.Nickname = strings.TrimSpace(input.Nickname)
	if input.Nickname != "" {
		input.Nicknames = append([]string{input.Nickname}, input.Nicknames...)
	}
	// core Candidate 只认 nicknames 复数且 readJSON 开了 DisallowUnknownFields，
	// 转发前清空单数字段避免序列化出 "nickname" 被 core 拒绝（invalid request body）。
	input.Nickname = ""
	input.ManualAction = strings.ToLower(strings.TrimSpace(input.ManualAction))
	input.ManualOrigin = strings.ToLower(strings.TrimSpace(input.ManualOrigin))
	input.ExecutionMode = strings.ToLower(strings.TrimSpace(input.ExecutionMode))
	input.FixedText = strings.TrimSpace(input.FixedText)
	if input.ManualOrigin != "agent_input" &&
		input.ManualOrigin != "agent_input_preview" &&
		input.ManualOrigin != "question_cluster" &&
		input.ManualOrigin != "test_simulation" {
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
	// Manual quick answers bypass danmaku classification, so no kind arrives with
	// them. Fill one here: a chosen question is a question, anything else is chat.
	input.MissionKind = strings.ToLower(strings.TrimSpace(input.MissionKind))
	if input.MissionKind == "" {
		if input.Question != "" {
			input.MissionKind = "reply_question"
		} else {
			input.MissionKind = "reply_chat"
		}
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

// recordInteractionReplay 把一次互动（问题→意图→回答）落库，供回测使用。
// 记录失败不阻断主流程，静默忽略。
func (s *Server) recordInteractionReplay(r *http.Request, tenantID, roomID, planID int64, origin string, result decisionexecutor.SimulationOutput) {
	// 旧「意图→规划→回答」链路的 answer_outline 已下线，回测只记问题与回答正文。
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	_ = s.store.RecordInteractionReplay(persistCtx, tenantID, roomID, planID, origin, result.Question, result.Reply, result.ExecutionMode, result.UserLayerVersion, nil, "")
}
