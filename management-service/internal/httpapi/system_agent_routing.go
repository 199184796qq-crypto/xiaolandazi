package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentrouting"
	"livecompanion/management/internal/model"
)

const agentRoutingManagePermission = "system.settings.agent_routing.manage"

type agentRoutingDraftRequest struct {
	Value string `json:"value"`
}

type agentRoutingAssistRequest struct {
	Instruction string `json:"instruction"`
	CurrentJSON string `json:"current_json"`
}

func (s *Server) requireAgentRoutingMaintainer(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, _, ok := s.requireStaffPermission(w, r, agentRoutingManagePermission)
	if !ok {
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) agentRoutingPromptConfig(ctx context.Context) (model.AgentPromptConfig, error) {
	items, err := s.store.ListAgentPromptConfigs(ctx)
	if err != nil {
		return model.AgentPromptConfig{}, err
	}
	for _, item := range items {
		if item.Key == agentrouting.ConfigKey {
			return item, nil
		}
	}
	return model.AgentPromptConfig{}, fmt.Errorf("agent routing config not found")
}

func (s *Server) systemAgentRoutingGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentRoutingMaintainer(w, r); !ok {
		return
	}
	item, err := s.agentRoutingPromptConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取智能体路由配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": item})
}

func (s *Server) systemAgentRoutingSaveDraft(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentRoutingMaintainer(w, r)
	if !ok {
		return
	}
	var input agentRoutingDraftRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "路由配置请求格式错误")
		return
	}
	input.Value = strings.TrimSpace(input.Value)
	if utf8.RuneCountInString(input.Value) > 40000 {
		writeError(w, http.StatusBadRequest, "路由 JSON 不能超过 40000 字")
		return
	}
	if _, err := agentrouting.Parse(input.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.UpdateAgentPromptConfigs(r.Context(), []model.AgentPromptConfigUpdate{{
		Key: agentrouting.ConfigKey, CurrentValue: input.Value, Enabled: true,
	}}, actor.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "保存智能体路由草稿失败")
		return
	}
	item, err := s.agentRoutingPromptConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "草稿已保存，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": item})
}

func (s *Server) systemAgentRoutingPublish(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentRoutingMaintainer(w, r)
	if !ok {
		return
	}
	item, err := s.agentRoutingPromptConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取智能体路由草稿失败")
		return
	}
	if _, err := agentrouting.Parse(item.DraftValue); err != nil {
		writeError(w, http.StatusBadRequest, "草稿校验失败："+err.Error())
		return
	}
	if err := s.store.PublishAgentPromptConfig(r.Context(), agentrouting.ConfigKey, actor.UserID, "publish"); err != nil {
		writeError(w, http.StatusInternalServerError, "发布智能体路由配置失败")
		return
	}
	item, err = s.agentRoutingPromptConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "已发布，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": item})
}

func (s *Server) systemAgentRoutingHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentRoutingMaintainer(w, r); !ok {
		return
	}
	items, err := s.store.ListAgentPromptHistory(r.Context(), agentrouting.ConfigKey, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取智能体路由版本历史失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemAgentRoutingRollback(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentRoutingMaintainer(w, r)
	if !ok {
		return
	}
	var input rollbackAgentPromptRequest
	if err := readJSON(w, r, &input); err != nil || input.Version == 0 {
		writeError(w, http.StatusBadRequest, "回滚版本无效")
		return
	}
	if err := s.store.RollbackAgentPromptConfig(r.Context(), agentrouting.ConfigKey, input.Version, actor.UserID); err != nil {
		writeError(w, http.StatusBadRequest, "回滚智能体路由配置失败")
		return
	}
	item, err := s.agentRoutingPromptConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "已回滚，但重新读取失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"config": item})
}

func (s *Server) systemAgentRoutingAssist(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentRoutingMaintainer(w, r)
	if !ok {
		return
	}
	var input agentRoutingAssistRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "智能体辅助请求格式错误")
		return
	}
	input.Instruction = strings.TrimSpace(input.Instruction)
	input.CurrentJSON = strings.TrimSpace(input.CurrentJSON)
	if input.Instruction == "" {
		writeError(w, http.StatusBadRequest, "请输入希望智能体如何调整路由配置")
		return
	}
	if utf8.RuneCountInString(input.Instruction) > 4000 {
		writeError(w, http.StatusBadRequest, "单次维护说明不能超过 4000 字")
		return
	}
	if input.CurrentJSON == "" {
		item, err := s.agentRoutingPromptConfig(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取当前路由配置失败")
			return
		}
		input.CurrentJSON = item.DraftValue
	}
	if _, err := agentrouting.Parse(input.CurrentJSON); err != nil {
		writeError(w, http.StatusBadRequest, "当前编辑区 JSON 无效："+err.Error())
		return
	}

	prompt := fmt.Sprintf(`你是后台智能体路由配置维护助手。维护人员会给你当前 JSON 和自然语言修改要求。
你只能修改 JSON 配置，不执行发布，不声称已经生效，不删除未被要求修改的配置。
必须保持 schema_version=1，并保留这些顶层字段：
schema_version、fallback_intent、model_enabled、min_model_confidence、allow_natural_actions、priority、classifier_prompt、intents。
intents 的 match 仅允许 exact_any、contains_any、prefix_any 三种字符串数组。
只返回完整、可解析的 JSON，不要 Markdown，不要解释。

【当前 JSON】
%s

【维护要求】
%s
`, input.CurrentJSON, input.Instruction)
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	invocationID := s.beginAISingleUse(r.Context(), actor, nil, "agent_routing_maintenance", nil)
	response, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只负责生成结构化路由 JSON 草稿，不执行任何真实系统动作。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      2200,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        20 * time.Second,
	})
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", response.Provider, response.Model, response.LatencyMS, map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "智能体暂时无法生成路由草稿")
		return
	}
	candidateRaw := stripPolicyJSONFence(response.Text)
	cfg, err := agentrouting.Parse(candidateRaw)
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", response.Provider, response.Model, response.LatencyMS, map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "智能体生成的 JSON 未通过结构校验，请换一种说法再试")
		return
	}
	s.finishAISingleUse(r.Context(), invocationID, "succeeded", response.Provider, response.Model, response.LatencyMS, nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"proposed_json": agentrouting.Format(cfg),
		"model":         response.Model,
		"latency_ms":    response.LatencyMS,
	})
}
