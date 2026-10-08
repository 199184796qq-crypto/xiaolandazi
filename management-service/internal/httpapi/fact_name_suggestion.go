package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

const factNameSuggestionPromptRelativePath = "configs/prompts/anchor-style/fact-name-suggestion.md"

func loadFactNameSuggestionPrompt() (string, string, error) {
	return loadReplaceablePromptAsset("FACT_NAME_SUGGESTION_PROMPT_PATH", factNameSuggestionPromptRelativePath, "事实名称提炼母版")
}

func parseFactNameSuggestion(raw string) (string, error) {
	var output struct {
		Name string `json:"name"`
	}
	decoder := json.NewDecoder(strings.NewReader(stripPolicyJSONFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return "", errors.New("事实名称返回格式无效，请手动填写")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return "", errors.New("事实名称返回格式无效，请手动填写")
	}
	name := strings.TrimSpace(output.Name)
	if name == "" || utf8.RuneCountInString(name) > 24 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("未能提炼有效的事实名称，请手动填写")
	}
	return name, nil
}

// Naming only: no fact adoption, content rewriting or risk confirmation.
func (s *Server) suggestLiveAgentPlanFactName(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID int64  `json:"tenant_id,omitempty"`
		Category string `json:"category"`
		Content  string `json:"content"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "事实名称请求格式错误")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	input.Category = strings.TrimSpace(input.Category)
	if input.Content == "" || utf8.RuneCountInString(input.Content) > 4000 {
		writeError(w, 400, "事实内容不能为空或超过4000字")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, 404, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, 500, "读取直播智能体方案失败")
		return
	}
	catalog, _, err := s.tenantFactCategoryCatalog(r.Context(), tenantID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	var category *factCategoryDefinition
	for i := range catalog.Categories {
		if catalog.Categories[i].Value == input.Category {
			category = &catalog.Categories[i]
			break
		}
	}
	if category == nil {
		writeError(w, 400, "事实分类无效，请重新选择")
		return
	}
	prompt, source, err := loadFactNameSuggestionPrompt()
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil || settings.SellingPointProfileID == "" {
		writeError(w, 409, "未配置事实提炼模型，请手动填写名称")
		return
	}
	payload, _ := json.Marshal(map[string]any{"fact_content": input.Content, "category": category})
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenantID, PlanID: planID})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "fact_name_suggestion", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(payload)}}, 160, false)
	if err != nil {
		writeError(w, 502, "名称提炼暂时失败，可重试或手动填写")
		return
	}
	name, err := parseFactNameSuggestion(result.Text)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]string{"name": name, "prompt_source": source})
}
