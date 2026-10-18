package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// factTypeSuggestResult 是模型对一条事实的类型判断结果。
type factTypeSuggestResult struct {
	Category   string  `json:"category"`
	Label      string  `json:"label"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
	LowConf    bool    `json:"low_conf"`
	// NewLabel/NewFamily/NewDescription 仅在模型判定现有分类都不贴切时填充，
	// 表示应新增一个分类（后端据此自动写入租户分类库）。
	NewLabel       string `json:"new_label,omitempty"`
	NewFamily      string `json:"new_family,omitempty"`
	NewDescription string `json:"new_description,omitempty"`
}

const factTypeLowConfidenceThreshold = 0.8

// buildFactTypeSuggestSystemPrompt 把当前方案允许的分类枚举拼成模型可读的候选清单。
func buildFactTypeSuggestSystemPrompt(catalog factCategoryCatalog) string {
	var b strings.Builder
	b.WriteString("你是直播带货事实分类助手。下面给出可选分类，先判断事实内容最贴切的分类。")
	b.WriteString("若确实没有贴切分类，可以新建一个分类（给出中文分类名和简要说明）。")
	b.WriteString("输出 JSON，字段固定为：category（命中的分类 value，新建时留空字符串）、")
	b.WriteString("confidence（0~1 小数，把握程度）、reason（一句话理由）。")
	b.WriteString("新建分类时额外输出 new_label（中文分类名，1~10 字）、")
	b.WriteString("new_family（所属类型，只能取 product/link/trade/fulfillment/identity_location/other 之一）、")
	b.WriteString("new_description（一句话说明，不超过 50 字）。")
	b.WriteString("如果两个分类都有可能、拿不准，把 confidence 调低到 0.7 以下，并在 reason 里说明疑点。\n\n可选分类：\n")
	for _, c := range catalog.Categories {
		b.WriteString("- ")
		b.WriteString(c.Value)
		b.WriteString("（")
		b.WriteString(c.Label)
		b.WriteString("）：")
		b.WriteString(c.Description)
		b.WriteString("\n")
	}
	return b.String()
}

func parseFactTypeSuggest(raw string) (factTypeSuggestResult, error) {
	var out struct {
		Category       string  `json:"category"`
		Confidence     float64 `json:"confidence"`
		Reason         string  `json:"reason"`
		NewLabel       string  `json:"new_label"`
		NewFamily      string  `json:"new_family"`
		NewDescription string  `json:"new_description"`
	}
	decoder := json.NewDecoder(strings.NewReader(stripPolicyJSONFence(raw)))
	if err := decoder.Decode(&out); err != nil {
		return factTypeSuggestResult{}, errors.New("事实类型判断返回格式无效")
	}
	out.Category = strings.TrimSpace(out.Category)
	out.NewLabel = strings.TrimSpace(out.NewLabel)
	out.NewFamily = strings.TrimSpace(out.NewFamily)
	out.NewDescription = strings.TrimSpace(out.NewDescription)
	if out.Category == "" && out.NewLabel == "" {
		return factTypeSuggestResult{}, errors.New("事实类型判断为空")
	}
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 1 {
		out.Confidence = 1
	}
	return factTypeSuggestResult{
		Category:       out.Category,
		Confidence:     out.Confidence,
		Reason:         strings.TrimSpace(out.Reason),
		LowConf:        out.Confidence < factTypeLowConfidenceThreshold,
		NewLabel:       out.NewLabel,
		NewFamily:      out.NewFamily,
		NewDescription: out.NewDescription,
	}, nil
}

// suggestFactType 对一条事实（key + value）调用模型判断类型。
// suggestFactType 对一条事实（key + value）调用「系统智能体模型」判断类型。
// 通过 completeAnchorStyleWorkbench 空 profileID 路由：stage 非口播/审核，
// 会落到后台配置的 SystemDefaultID（系统智能体模型），而不是环境变量模型。
// 若模型判定现有分类都不贴切并给出 new_label，则自动写入租户分类库，
// 返回新建分类的 value 与中文 label。
func (s *Server) suggestFactType(ctx context.Context, tenantID, planID int64, actorUserID int64, factKey, factValue string, catalog factCategoryCatalog) (factTypeSuggestResult, error) {
	system := buildFactTypeSuggestSystemPrompt(catalog)
	user := "事实名称：" + factKey + "\n事实内容：" + factValue + "\n\n请判断事实类型："

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: tenantID, PlanID: planID})

	messages := []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
	result, _, err := s.completeAnchorStyleWorkbench(ctx, "", "fact_type_suggest", agentgateway.ResponseJSON, messages, 200, false)
	if err != nil {
		return factTypeSuggestResult{}, err
	}
	parsed, err := parseFactTypeSuggest(result.Text)
	if err != nil {
		return factTypeSuggestResult{}, err
	}

	// 新建分类：模型判定现有都不贴切，给出中文名 + 所属类型，自动写入租户分类库。
	if parsed.NewLabel != "" {
		cat, err := model.NewLiveFactCategory(parsed.NewLabel, parsed.NewFamily, parsed.NewDescription)
		if err != nil {
			return factTypeSuggestResult{}, errors.New("模型建议的新分类无效：" + err.Error())
		}
		if err := s.store.CreateLiveFactCategory(ctx, tenantID, actorUserID, cat); err != nil {
			return factTypeSuggestResult{}, errors.New("自动新增分类失败，请手动选择")
		}
		parsed.Category = cat.Value
		parsed.Label = cat.Label
		return parsed, nil
	}

	// 命中现有分类：必须落在当前方案允许的分类枚举内，否则视为失败。
	if !catalog.contains(parsed.Category) {
		return factTypeSuggestResult{}, errors.New("事实类型判断不在可选范围内")
	}
	parsed.Label = catalog.labelOf(parsed.Category)
	return parsed, nil
}

// liveAgentPlanFactTypeSuggest 批量判断事实类型：入参为事实列表（id+key+value），
// 返回逐条建议（category + confidence + reason），前端据此预填下拉并对低把握项标红。
func (s *Server) liveAgentPlanFactTypeSuggest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID int64 `json:"tenant_id"`
		Facts    []struct {
			ID    int64  `json:"id"`
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"facts"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "批量归类请求格式错误")
		return
	}
	if len(input.Facts) == 0 {
		writeError(w, 400, "没有需要归类的事实")
		return
	}
	if len(input.Facts) > 50 {
		writeError(w, 400, "单次最多归类 50 条事实")
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
	catalog, _, err := s.planFactCategoryCatalog(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}

	results := make([]map[string]any, 0, len(input.Facts))
	for _, fact := range input.Facts {
		key := strings.TrimSpace(fact.Key)
		value := strings.TrimSpace(fact.Value)
		if value == "" || utf8.RuneCountInString(value) > 4000 {
			results = append(results, map[string]any{
				"id": fact.ID, "category": "", "confidence": 0, "reason": "事实内容为空或超长，无法判断", "low_conf": true, "error": true,
			})
			continue
		}
		suggestion, err := s.suggestFactType(r.Context(), tenantID, planID, actor.UserID, key, value, catalog)
		if err != nil {
			results = append(results, map[string]any{
				"id": fact.ID, "category": "", "confidence": 0, "reason": "判断失败，请手动选择", "low_conf": true, "error": true,
			})
			continue
		}
		results = append(results, map[string]any{
			"id": fact.ID, "category": suggestion.Category, "label": suggestion.Label,
			"confidence": suggestion.Confidence, "reason": suggestion.Reason, "low_conf": suggestion.LowConf,
		})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"results": results})
}
