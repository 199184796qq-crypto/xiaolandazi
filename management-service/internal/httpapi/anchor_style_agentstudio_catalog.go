package httpapi

import (
	"net/http"
	"strings"

	"livecompanion/management/internal/agentstudio"
)

type anchorStyleAgentStudioCatalogAgent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	System      string `json:"system"`
	Model       string `json:"model"`
	Version     int    `json:"version"`
}

type anchorStyleAgentStudioCatalogEnvironment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type anchorStyleAgentStudioCatalogResponse struct {
	WorkspaceID  string                                     `json:"workspace_id"`
	Region       string                                     `json:"region"`
	Agents       []anchorStyleAgentStudioCatalogAgent       `json:"agents"`
	Environments []anchorStyleAgentStudioCatalogEnvironment `json:"environments"`
}

func (s *Server) anchorStyleWorkbenchAgentStudioCatalog(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID          int64  `json:"tenant_id,omitempty"`
		WorkspaceID       string `json:"workspace_id"`
		Region            string `json:"region"`
		CredentialProfile string `json:"credential_profile_id"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "读取 Agent Studio 资源的请求格式错误")
		return
	}
	if strings.TrimSpace(input.Region) == "" {
		input.Region = "cn-beijing"
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	if _, _, _, _, ok = s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID); !ok {
		return
	}
	profile, err := s.store.ResolveSpeechModel(r.Context(), strings.TrimSpace(input.CredentialProfile))
	if err != nil {
		writeError(w, http.StatusBadRequest, "读取阿里云 API Key 配置失败："+err.Error())
		return
	}
	if profile == nil || !profile.Enabled {
		writeError(w, http.StatusBadRequest, "请选择一个已启用并保存 API Key 的模型配置")
		return
	}
	client, err := agentstudio.New(input.WorkspaceID, input.Region, profile.APIKey, agentStudioTimeout(profile.TimeoutMS))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	type agentResult struct {
		items []agentstudio.AgentSnapshot
		err   error
	}
	type environmentResult struct {
		items []agentstudio.Environment
		err   error
	}
	agentsChannel := make(chan agentResult, 1)
	environmentsChannel := make(chan environmentResult, 1)
	go func() {
		items, listErr := client.ListAgents(r.Context())
		agentsChannel <- agentResult{items: items, err: listErr}
	}()
	go func() {
		items, listErr := client.ListEnvironments(r.Context())
		environmentsChannel <- environmentResult{items: items, err: listErr}
	}()
	agentsResult := <-agentsChannel
	environmentsResult := <-environmentsChannel
	if agentsResult.err != nil {
		writeError(w, http.StatusBadGateway, "读取 Agent Studio 智能体列表失败："+agentsResult.err.Error())
		return
	}
	if environmentsResult.err != nil {
		writeError(w, http.StatusBadGateway, "读取 Agent Studio 环境列表失败："+environmentsResult.err.Error())
		return
	}
	agents := make([]anchorStyleAgentStudioCatalogAgent, 0, len(agentsResult.items))
	for _, item := range agentsResult.items {
		agents = append(agents, anchorStyleAgentStudioCatalogAgent{
			ID: item.ID, Name: item.Name, Description: item.Description, System: item.System,
			Model: item.Model.ID, Version: item.Version,
		})
	}
	environments := make([]anchorStyleAgentStudioCatalogEnvironment, 0, len(environmentsResult.items))
	for _, item := range environmentsResult.items {
		environments = append(environments, anchorStyleAgentStudioCatalogEnvironment{
			ID: item.ID, Name: item.Name, Description: item.Description,
		})
	}
	writeJSON(w, http.StatusOK, anchorStyleAgentStudioCatalogResponse{
		WorkspaceID: strings.TrimSpace(input.WorkspaceID),
		Region:      strings.TrimSpace(strings.ToLower(input.Region)),
		Agents:      agents, Environments: environments,
	})
}
