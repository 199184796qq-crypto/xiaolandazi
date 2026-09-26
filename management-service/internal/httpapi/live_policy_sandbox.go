package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type livePolicyTestInput struct {
	Layer        string                     `json:"layer"`
	IndustryCode string                     `json:"industry_code,omitempty"`
	RoomID       int64                      `json:"room_id,omitempty"`
	Message      string                     `json:"message"`
	History      []liveAgentChatHistoryItem `json:"history,omitempty"`
}

type livePolicyTestMatchedRule struct {
	Key           string `json:"key"`
	Title         string `json:"title,omitempty"`
	SourceLayer   string `json:"source_layer"`
	ExecutionMode string `json:"execution_mode"`
}

type livePolicyTestVersionSource struct {
	Layer           string `json:"layer"`
	VersionID       int64  `json:"version_id"`
	VersionNo       uint64 `json:"version_no"`
	LifecycleStatus string `json:"lifecycle_status"`
	IsTestTarget    bool   `json:"is_test_target"`
}

type livePolicyTestEffectiveSummary struct {
	IndustryCode  string                        `json:"industry_code"`
	RuleCount     int                           `json:"rule_count"`
	ConflictCount int                           `json:"conflict_count"`
	Sources       []livePolicyTestVersionSource `json:"sources"`
}

type livePolicyTestOutput struct {
	Sandbox      bool                           `json:"sandbox"`
	Reply        string                         `json:"reply"`
	Blocked      bool                           `json:"blocked"`
	BlockReason  string                         `json:"block_reason,omitempty"`
	MatchedRules []livePolicyTestMatchedRule    `json:"matched_rules"`
	DataSources  []string                       `json:"data_sources"`
	MissingData  []string                       `json:"missing_data"`
	Effective    livePolicyTestEffectiveSummary `json:"effective"`
	Model        string                         `json:"model,omitempty"`
	LatencyMS    int64                          `json:"latency_ms,omitempty"`
}

type livePolicyTestModelOutput struct {
	Reply       string   `json:"reply"`
	Blocked     bool     `json:"blocked"`
	BlockReason string   `json:"block_reason"`
	MatchedKeys []string `json:"matched_keys"`
	DataSources []string `json:"data_sources"`
	MissingData []string `json:"missing_data"`
}

func preferredPolicyTestVersion(ctx model.LivePolicyContext) *model.LivePolicyVersion {
	for index := range ctx.Versions {
		if ctx.Versions[index].LifecycleStatus == "draft" {
			value := ctx.Versions[index]
			return &value
		}
	}
	if ctx.Active != nil {
		value := *ctx.Active
		return &value
	}
	return nil
}

func resolvePolicyTestMatchedRules(keys []string, rules []model.LiveEffectivePolicyRule) []livePolicyTestMatchedRule {
	byKey := make(map[string]model.LiveEffectivePolicyRule, len(rules))
	for _, rule := range rules {
		if strings.TrimSpace(rule.Key) != "" {
			byKey[rule.Key] = rule
		}
	}
	seen := map[string]struct{}{}
	result := make([]livePolicyTestMatchedRule, 0, len(keys))
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		rule, exists := byKey[key]
		if !exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, livePolicyTestMatchedRule{
			Key: rule.Key, Title: rule.Title, SourceLayer: rule.SourceLayer,
			ExecutionMode: rule.ExecutionMode,
		})
	}
	return result
}

func buildPolicyTestSystemPrompt(instruction string, effective model.LiveEffectivePolicy) (string, error) {
	rulesJSON, err := json.Marshal(effective.Rules)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(instruction + "\n\n【当前测试上下文】\n当前行业：" + effective.IndustryCode + "\n当前有效规则：\n" + string(rulesJSON)), nil
}

func (s *Server) policyTestTargetVersion(ctx context.Context, layer, industryCode string) (*model.LivePolicyVersion, error) {
	contextValue, err := s.store.GetLivePolicyContext(ctx, layer, industryCode, 0, 0)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.store.GetActiveLivePolicyVersion(ctx, layer, industryCode, 0, 0)
		}
		return nil, err
	}
	return preferredPolicyTestVersion(contextValue), nil
}

func policyTestVersionSources(l1, l2, l3 *model.LivePolicyVersion, targetLayer string) []livePolicyTestVersionSource {
	items := make([]livePolicyTestVersionSource, 0, 3)
	appendVersion := func(layer string, version *model.LivePolicyVersion) {
		if version == nil {
			return
		}
		items = append(items, livePolicyTestVersionSource{
			Layer: layer, VersionID: version.ID, VersionNo: version.VersionNo,
			LifecycleStatus: version.LifecycleStatus, IsTestTarget: layer == targetLayer,
		})
	}
	appendVersion(model.LivePolicyLayerL1, l1)
	appendVersion(model.LivePolicyLayerL2, l2)
	appendVersion(model.LivePolicyLayerL3, l3)
	return items
}

func (s *Server) livePolicyAdminTest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireLivePolicyView(w, r)
	if !ok {
		return
	}
	var input livePolicyTestInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "规则测试请求格式错误")
		return
	}
	input.Layer = strings.ToUpper(strings.TrimSpace(input.Layer))
	input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
	input.Message = strings.TrimSpace(input.Message)
	if input.Layer != model.LivePolicyLayerL1 && input.Layer != model.LivePolicyLayerL2 {
		writeError(w, http.StatusBadRequest, "规则测试只支持规则层或行业层")
		return
	}
	if input.Layer == model.LivePolicyLayerL2 && input.IndustryCode == "" {
		input.IndustryCode = "general"
	}
	if input.Message == "" {
		writeError(w, http.StatusBadRequest, "请输入模拟观众问题")
		return
	}
	if utf8.RuneCountInString(input.Message) > 2000 {
		writeError(w, http.StatusBadRequest, "单次测试输入不能超过 2000 字")
		return
	}

	industryCode := input.IndustryCode
	if industryCode == "" {
		industryCode = "general"
	}
	var l1, l2, l3 *model.LivePolicyVersion

	if input.RoomID > 0 {
		tenantID, roomOK := s.tenantForRoom(w, r, actor, input.RoomID)
		if !roomOK {
			return
		}
		roomIndustry, activeL1, activeL2, activeL3, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, input.RoomID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取测试直播间策略上下文失败")
			return
		}
		industryCode = roomIndustry
		l1, l2, l3 = activeL1, activeL2, activeL3
	} else {
		l1, _ = s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
		l2, _ = s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL2, industryCode, 0, 0)
		if l2 == nil && industryCode != "general" {
			l2, _ = s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL2, "general", 0, 0)
		}
	}

	if input.Layer == model.LivePolicyLayerL1 {
		target, err := s.policyTestTargetVersion(r.Context(), model.LivePolicyLayerL1, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取规则层测试版本失败")
			return
		}
		if target == nil {
			writeError(w, http.StatusBadRequest, "当前没有可测试的规则层规则")
			return
		}
		l1 = target
	} else {
		testIndustry := input.IndustryCode
		if testIndustry == "" {
			testIndustry = industryCode
		}
		target, err := s.policyTestTargetVersion(r.Context(), model.LivePolicyLayerL2, testIndustry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取行业层测试版本失败")
			return
		}
		if target == nil {
			writeError(w, http.StatusBadRequest, "当前没有可测试的行业层规则")
			return
		}
		l2 = target
		industryCode = testIndustry
	}

	effective := policy.BuildEffective(industryCode, l1, l2, l3)
	testInstruction := s.store.AgentPromptValue(r.Context(), "policy.sandbox.system", "只模拟主播最终回答，不执行真实动作；严格返回 JSON。")
	systemPrompt, err := buildPolicyTestSystemPrompt(testInstruction, effective)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "构建规则测试上下文失败")
		return
	}
	modelOutput, modelName, latencyMS, err := callPolicyTestModel(
		r.Context(), systemPrompt, input.Message, input.History,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "规则测试模型暂时不可用，请稍后重试")
		return
	}
	if strings.TrimSpace(modelOutput.Reply) == "" {
		writeError(w, http.StatusBadGateway, "规则测试没有生成有效回复")
		return
	}

	writeJSON(w, http.StatusOK, livePolicyTestOutput{
		Sandbox: true, Reply: strings.TrimSpace(modelOutput.Reply),
		Blocked: modelOutput.Blocked, BlockReason: strings.TrimSpace(modelOutput.BlockReason),
		MatchedRules: resolvePolicyTestMatchedRules(modelOutput.MatchedKeys, effective.Rules),
		DataSources:  compactPolicyTestStrings(modelOutput.DataSources),
		MissingData:  compactPolicyTestStrings(modelOutput.MissingData),
		Effective: livePolicyTestEffectiveSummary{
			IndustryCode: effective.IndustryCode, RuleCount: len(effective.Rules),
			ConflictCount: len(effective.Conflicts),
			Sources:       policyTestVersionSources(l1, l2, l3, input.Layer),
		},
		Model: modelName, LatencyMS: latencyMS,
	})
}

func compactPolicyTestStrings(items []string) []string {
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, raw := range items {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if len(result) >= 12 {
			break
		}
	}
	return result
}

func buildPolicyTestMessages(
	systemPrompt string,
	message string,
	history []liveAgentChatHistoryItem,
) []map[string]string {
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	messages := []map[string]string{{"role": "system", "content": systemPrompt}}
	for _, item := range history {
		role := strings.TrimSpace(item.Role)
		if role == "agent" {
			role = "assistant"
		}
		if role != "assistant" && role != "user" {
			continue
		}
		content := strings.TrimSpace(item.Text)
		if content == "" {
			continue
		}
		if utf8.RuneCountInString(content) > 1600 {
			content = string([]rune(content)[:1600])
		}
		messages = append(messages, map[string]string{
			"role": role, "content": content,
		})
	}
	messages = append(messages, map[string]string{
		"role": "user", "content": strings.TrimSpace(message),
	})
	return messages
}

func callPolicyTestModel(
	ctx context.Context,
	systemPrompt, message string,
	history []liveAgentChatHistoryItem,
) (livePolicyTestModelOutput, string, int64, error) {
	rawMessages := buildPolicyTestMessages(systemPrompt, message, history)
	messages := make([]agentgateway.Message, 0, len(rawMessages))
	for _, item := range rawMessages {
		messages = append(messages, agentgateway.Message{
			Role:    item["role"],
			Content: item["content"],
		})
	}
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages:       messages,
		MaxTokens:      1200,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return livePolicyTestModelOutput{}, "", 0, err
	}
	raw := stripPolicyJSONFence(result.Text)
	var output livePolicyTestModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return livePolicyTestModelOutput{}, "", result.LatencyMS, fmt.Errorf("decode policy test output: %w", err)
	}
	return output, result.Model, result.LatencyMS, nil
}
