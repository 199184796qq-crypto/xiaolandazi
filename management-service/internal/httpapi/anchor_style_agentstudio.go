package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentstudio"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type anchorStyleAgentStudioSessionResponse struct {
	SessionID         string `json:"session_id"`
	Status            string `json:"status"`
	WorkspaceID       string `json:"workspace_id"`
	Region            string `json:"region"`
	AgentID           string `json:"agent_id"`
	AgentName         string `json:"agent_name"`
	AgentVersion      int    `json:"agent_version"`
	AgentModel        string `json:"agent_model"`
	EnvironmentID     string `json:"environment_id"`
	EnvironmentName   string `json:"environment_name"`
	EnvironmentAuto   bool   `json:"environment_auto_selected"`
	CredentialProfile string `json:"credential_profile_id"`
	CredentialName    string `json:"credential_name"`
	CreatedAt         string `json:"created_at"`
}

type anchorStyleAgentStudioTurnResponse struct {
	SessionID    string                    `json:"session_id"`
	FirstTurn    bool                      `json:"first_turn"`
	Task         anchorStyleWheelTask      `json:"task"`
	NextPointer  int                       `json:"next_pointer"`
	Request      anchorStyleWorkbenchTrace `json:"request"`
	RawResponse  string                    `json:"raw_response"`
	Provider     string                    `json:"provider"`
	LatencyMS    int64                     `json:"latency_ms"`
	MaxChars     int                       `json:"max_chars"`
	Unlimited    bool                      `json:"unlimited_chars"`
	ActualChars  int                       `json:"actual_chars"`
	Exceeded     bool                      `json:"exceeded_limit"`
	StopReason   string                    `json:"stop_reason"`
	EventIDs     []string                  `json:"event_ids"`
	PromptMode   string                    `json:"prompt_mode"`
	StyleAssetID int64                     `json:"style_asset_id"`
	WheelAssetID int64                     `json:"wheel_asset_id"`
	FeedbackUsed bool                      `json:"feedback_applied"`
}

func (s *Server) anchorStyleWorkbenchAgentStudioSessionCreate(w http.ResponseWriter, r *http.Request) {
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
		AgentID           string `json:"agent_id"`
		EnvironmentID     string `json:"environment_id"`
		CredentialProfile string `json:"credential_profile_id"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "创建 Agent Studio 会话的请求格式错误")
		return
	}
	if strings.TrimSpace(input.Region) == "" {
		input.Region = "cn-beijing"
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	plan, _, _, _, ok := s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID)
	if !ok {
		return
	}
	if _, _, _, err := s.confirmedAnchorStyleAgentStudioAssets(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
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
	environmentID := strings.TrimSpace(input.EnvironmentID)
	environmentName := ""
	environmentAuto := environmentID == ""
	if environmentAuto {
		environments, listErr := client.ListEnvironments(r.Context())
		if listErr != nil {
			writeError(w, http.StatusBadGateway, "自动读取 Agent Studio 环境失败："+listErr.Error())
			return
		}
		if len(environments) == 0 {
			writeError(w, http.StatusConflict, "当前 Workspace 没有可用环境，请先在阿里 Agent Studio 创建一个 Environment")
			return
		}
		selected := environments[0]
		for _, candidate := range environments {
			name := strings.ToLower(strings.TrimSpace(candidate.Name))
			if strings.Contains(name, "默认") || strings.Contains(name, "default") {
				selected = candidate
				break
			}
		}
		environmentID = selected.ID
		environmentName = selected.Name
	}
	title := fmt.Sprintf("主播口播测试 · %s · %s", plan.Name, time.Now().In(time.Local).Format("01-02 15:04"))
	session, err := client.CreateSession(r.Context(), input.AgentID, environmentID, title, map[string]string{
		"source":    "anchor-style-workbench",
		"tenant_id": strconv.FormatInt(tenantID, 10),
		"plan_id":   strconv.FormatInt(planID, 10),
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "创建阿里连续会话失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, anchorStyleAgentStudioSessionResponse{
		SessionID: session.ID, Status: session.Status,
		WorkspaceID: strings.TrimSpace(input.WorkspaceID), Region: strings.TrimSpace(strings.ToLower(input.Region)),
		AgentID: session.Agent.ID, AgentName: session.Agent.Name, AgentVersion: session.Agent.Version,
		AgentModel: session.Agent.Model.ID, EnvironmentID: session.EnvironmentID,
		EnvironmentName: environmentName, EnvironmentAuto: environmentAuto,
		CredentialProfile: profile.ID, CredentialName: profile.Name, CreatedAt: session.CreatedAt,
	})
}

func (s *Server) anchorStyleWorkbenchAgentStudioTurn(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(r.PathValue("sessionID"))
	if !agentstudio.ValidResourceID(sessionID, "sesn_") {
		writeError(w, http.StatusBadRequest, "Session ID 无效")
		return
	}
	var input struct {
		TenantID          int64  `json:"tenant_id,omitempty"`
		WorkspaceID       string `json:"workspace_id"`
		Region            string `json:"region"`
		CredentialProfile string `json:"credential_profile_id"`
		Pointer           int    `json:"pointer"`
		MaxChars          int    `json:"max_chars"`
		UnlimitedChars    bool   `json:"unlimited_chars"`
		FirstTurn         bool   `json:"first_turn"`
		FeedbackPrompt    string `json:"feedback_prompt,omitempty"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "Agent Studio 单格测试请求格式错误")
		return
	}
	if strings.TrimSpace(input.Region) == "" {
		input.Region = "cn-beijing"
	}
	if input.Pointer < 0 {
		writeError(w, http.StatusBadRequest, "轮盘指针不能小于0")
		return
	}
	if !input.UnlimitedChars && input.MaxChars == 0 {
		input.MaxChars = 300
	}
	if !input.UnlimitedChars && (input.MaxChars < 80 || input.MaxChars > 1000) {
		writeError(w, http.StatusBadRequest, "单段最大字数须为80到1000字")
		return
	}
	if input.UnlimitedChars {
		input.MaxChars = 0
	}
	feedbackPrompt := strings.TrimSpace(input.FeedbackPrompt)
	if utf8.RuneCountInString(feedbackPrompt) > 4000 {
		writeError(w, http.StatusBadRequest, "单次回灌提示词不能超过4000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	meta, wheelAsset, tasks, err := s.confirmedAnchorStyleAgentStudioAssets(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	position := input.Pointer % len(tasks)
	task := tasks[position]
	task.Pointer = input.Pointer
	task.Cycle = input.Pointer/len(tasks) + 1
	task.Position = position + 1
	task.Total = len(tasks)

	message := renderAnchorStyleWheelContinuationPrompt(task, input.MaxChars, feedbackPrompt)
	promptMode := "continuation"
	if input.FirstTurn {
		plan, facts, benefits, links, dataOK := s.anchorStyleWorkbenchPlanData(w, r, tenantID, planID)
		if !dataOK {
			return
		}
		message = appendAnchorStyleFeedbackPrompt(renderAnchorStyleWheelTestPrompt(meta.EffectiveMetaPromptMD(), renderAnchorStyleWorkbenchFacts(plan, facts, benefits, links), task, input.MaxChars), feedbackPrompt)
		promptMode = "bootstrap_style_and_facts"
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
	started := time.Now()
	result, err := client.SendMessageAndWait(r.Context(), sessionID, message)
	latencyMS := time.Since(started).Milliseconds()
	if err != nil {
		writeError(w, http.StatusBadGateway, "阿里连续会话生成失败："+err.Error())
		return
	}
	raw := strings.TrimSpace(result.Text)
	actualChars := utf8.RuneCountInString(raw)
	writeJSON(w, http.StatusOK, anchorStyleAgentStudioTurnResponse{
		SessionID: sessionID, FirstTurn: input.FirstTurn, Task: task, NextPointer: input.Pointer + 1,
		Request: anchorStyleWorkbenchTrace{User: message}, RawResponse: raw,
		Provider: "aliyun-agentstudio", LatencyMS: latencyMS, MaxChars: input.MaxChars, Unlimited: input.UnlimitedChars,
		ActualChars: actualChars, Exceeded: !input.UnlimitedChars && actualChars > input.MaxChars,
		StopReason: result.StopReason, EventIDs: result.EventIDs, PromptMode: promptMode,
		StyleAssetID: meta.ID, WheelAssetID: wheelAsset.ID, FeedbackUsed: feedbackPrompt != "",
	})
}

func (s *Server) anchorStyleWorkbenchAgentStudioSessionArchive(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	sessionID := strings.TrimSpace(r.PathValue("sessionID"))
	if !agentstudio.ValidResourceID(sessionID, "sesn_") {
		writeError(w, http.StatusBadRequest, "Session ID 无效")
		return
	}
	var input struct {
		TenantID          int64  `json:"tenant_id,omitempty"`
		WorkspaceID       string `json:"workspace_id"`
		Region            string `json:"region"`
		CredentialProfile string `json:"credential_profile_id"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "归档 Agent Studio 会话的请求格式错误")
		return
	}
	if strings.TrimSpace(input.Region) == "" {
		input.Region = "cn-beijing"
	}
	if _, ok = s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false); !ok {
		return
	}
	profile, err := s.store.ResolveSpeechModel(r.Context(), strings.TrimSpace(input.CredentialProfile))
	if err != nil || profile == nil {
		writeError(w, http.StatusBadRequest, "读取阿里云 API Key 配置失败")
		return
	}
	client, err := agentstudio.New(input.WorkspaceID, input.Region, profile.APIKey, agentStudioTimeout(profile.TimeoutMS))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err = client.ArchiveSession(r.Context(), sessionID); err != nil {
		writeError(w, http.StatusBadGateway, "归档阿里连续会话失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "session_id": sessionID, "plan_id": planID})
}

func (s *Server) confirmedAnchorStyleAgentStudioAssets(ctx context.Context, tenantID, planID int64) (model.LiveAnchorStyleMetaPromptAsset, model.LiveSellingPointWheelAsset, []anchorStyleWheelTask, error) {
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(ctx, tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleMetaPromptNotFound) || (err == nil && meta.Status != "confirmed") {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("请先确认并保存当前方案的主播风格元提示词")
	}
	if err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("读取主播风格元提示词失败")
	}
	wheelAsset, err := s.store.GetActiveLiveSellingPointWheel(ctx, tenantID, planID)
	if errors.Is(err, appdb.ErrLiveSellingPointWheelNotFound) || (err == nil && wheelAsset.Status != "confirmed") {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("请先确认并保存当前方案的卖点轮盘")
	}
	if err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("读取卖点轮盘失败")
	}
	if _, _, _, err = normalizeAnchorStyleSellingPointWheelJSON(wheelAsset.EffectiveWheelJSON()); err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("当前卖点轮盘格式无效")
	}
	var wheel anchorStyleSellingPointWheel
	if json.Unmarshal([]byte(wheelAsset.EffectiveWheelJSON()), &wheel) != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("当前卖点轮盘无法解析")
	}
	tasks := flattenAnchorStyleWheelTasks(wheel)
	if len(tasks) == 0 {
		return model.LiveAnchorStyleMetaPromptAsset{}, model.LiveSellingPointWheelAsset{}, nil, errors.New("当前卖点轮盘没有可测试组合")
	}
	return meta, wheelAsset, tasks, nil
}

func renderAnchorStyleWheelContinuationPrompt(task anchorStyleWheelTask, maxChars int, feedbackPrompt string) string {
	base := fmt.Sprintf("%s × %s", task.SellingPoint, task.Angle)
	if maxChars > 0 {
		base = fmt.Sprintf("%s\n最多%d字", base, maxChars)
	}
	return appendAnchorStyleFeedbackPrompt(base, feedbackPrompt)
}

func appendAnchorStyleFeedbackPrompt(base, feedbackPrompt string) string {
	feedbackPrompt = strings.TrimSpace(feedbackPrompt)
	if feedbackPrompt == "" {
		return base
	}
	return strings.TrimSpace(base) + "\n\n# 本轮回灌提示词\n" + feedbackPrompt
}

func agentStudioTimeout(profileTimeoutMS int) time.Duration {
	timeout := time.Duration(profileTimeoutMS) * time.Millisecond
	if timeout < 120*time.Second {
		return 120 * time.Second
	}
	return timeout
}
