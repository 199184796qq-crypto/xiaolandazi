package httpapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

type agentUnderstandingPolicyRequest struct {
	Policy model.AgentUnderstandingPolicyInput `json:"policy"`
}

func (s *Server) systemAgentUnderstandingList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentRoutingMaintainer(w, r)
	if !ok {
		return
	}
	if err := s.store.EnsureAgentUnderstandingDefault(r.Context(), actor.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "初始化智能体理解配置失败")
		return
	}
	items, err := s.store.ListAgentUnderstandingPolicies(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取智能体理解配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) systemAgentUnderstandingEffective(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentRoutingMaintainer(w, r); !ok {
		return
	}
	tenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	policy, err := s.store.ResolveAgentUnderstandingPolicy(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "解析智能体理解生效策略失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": policy})
}

func (s *Server) systemAgentUnderstandingUpsert(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentRoutingMaintainer(w, r)
	if !ok {
		return
	}
	var input agentUnderstandingPolicyRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "智能体理解配置格式错误")
		return
	}
	item, err := s.store.UpsertAgentUnderstandingPolicy(r.Context(), input.Policy, actor.UserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": item})
}

func (s *Server) systemAgentUnderstandingDelete(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentRoutingMaintainer(w, r); !ok {
		return
	}
	scopeType := strings.TrimSpace(r.PathValue("scopeType"))
	scopeID, _ := strconv.ParseInt(strings.TrimSpace(r.PathValue("scopeID")), 10, 64)
	if err := s.store.DeleteAgentUnderstandingPolicy(r.Context(), scopeType, scopeID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) systemAgentUnderstandingModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentRoutingMaintainer(w, r); !ok {
		return
	}
	provider := strings.TrimSpace(r.URL.Query().Get("provider"))
	if provider == "" {
		provider = agentgateway.ProviderQwen
	}
	items, err := agentgateway.NewFromEnv().ListModels(r.Context(), provider)
	if err != nil {
		writeError(w, http.StatusBadGateway, "读取模型目录失败："+err.Error())
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].ID) < strings.ToLower(items[j].ID)
	})
	writeJSON(w, http.StatusOK, map[string]any{"provider": provider, "items": items})
}
