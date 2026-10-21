package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

// 事实差异分析：用户给一条挂在多个链接上的事实时，往往是「这两个品有什么不同」这种题目。
// 让他凭空写具体差异很难，所以给一个按钮，让模型先从方案已有事实里找能对上的差别：
// 找得到就给一句可直接保存的写法，找不到就说清两边各缺什么，并给一条示例格式。
//
// 防编造靠程序：返回内容里出现的每个数字都必须在本方案事实里出现过，否则整条作废。

var factNumberPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

type factDiffAnalysisResult struct {
	Found   bool   `json:"found"`
	Value   string `json:"value,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Example string `json:"example,omitempty"`
	// Digits 是返回内容里用到的数字，便于前端和人工核对来源。
	Digits []string `json:"digits,omitempty"`
}

func (s *Server) liveAgentPlanFactDiffAnalysis(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	factID, ok := liveAgentPlanFactPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID int64 `json:"tenant_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	ctx := r.Context()
	items, err := s.store.ListLiveAgentPlanFacts(ctx, tenant, plan)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取事实失败，请稍后重试")
		return
	}
	var target *model.LiveAgentPlanFact
	for i := range items {
		if items[i].ID == factID {
			target = &items[i]
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "正式事实不存在")
		return
	}
	if target.Scope == nil || target.Scope.Kind != "product" {
		writeError(w, http.StatusBadRequest, "先把这条事实的适用商品选成两个及以上链接，再来找差异")
		return
	}
	linkIDs := target.Scope.ProductScopeLinkIDs()
	if len(linkIDs) < 2 {
		writeError(w, http.StatusBadRequest, "这条事实只挂了一个链接，没有可对比的另一边")
		return
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取商品链接失败，请稍后重试")
		return
	}
	nameByID := map[int64]string{}
	for _, link := range links {
		if link.Status == "active" {
			nameByID[link.ID] = link.LinkKey
		}
	}
	ordered := make([]string, 0, len(linkIDs))
	for _, id := range linkIDs {
		if key, ok := nameByID[id]; ok && key != "" {
			ordered = append(ordered, key)
		}
	}
	if len(ordered) < 2 {
		writeError(w, http.StatusBadRequest, "这条事实关联的商品链接已不可用，请重新选择适用商品")
		return
	}
	perLink, allText := s.groupFactsByLink(items, linkIDs, nameByID, factID)
	if strings.TrimSpace(allText) == "" {
		writeError(w, http.StatusBadRequest, "这几个链接名下还没有任何事实，先补商品事实再来找差异")
		return
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_DIFF_SUGGEST_PATH", "configs/prompts/anchor-style/fact-diff-suggest.md", "事实差异分析母版")
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "差异分析母版不可用")
		return
	}
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取模型配置失败")
		return
	}
	packet, _ := json.Marshal(map[string]any{"要对比的链接": ordered, "各链接已有事实": perLink})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, settings.SellingPointProfileID, "fact_diff_suggest", agentgateway.ResponseJSON,
		[]agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(packet)}}, 1200, false)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "差异分析模型暂时不可用，请稍后重试")
		return
	}
	analysis, err := parseFactDiffAnalysis(result.Text)
	if err != nil {
		writeError(w, http.StatusBadGateway, "差异分析结果无法解析，请重试")
		return
	}
	if analysis.Found {
		// 硬约束：写进事实的每个数字都必须在本方案事实里出现过，避免模型把两边的数字算错或编造。
		if missing := unknownDigits(analysis.Value, allText); len(missing) > 0 {
			analysis = factDiffAnalysisResult{
				Found:  false,
				Reason: "模型给出的写法里出现了事实中没有的数字（" + strings.Join(missing, "、") + "），本条不建议采纳。",
				Example: "1号链接含X道荤素菜，2号链接含Y道，相差Z道",
			}
		} else {
			analysis.Digits = factNumberPattern.FindAllString(analysis.Value, -1)
		}
	}
	if !analysis.Found {
		if strings.TrimSpace(analysis.Reason) == "" {
			analysis.Reason = "两边事实里没有能直接对上的差异点。"
		}
		if strings.TrimSpace(analysis.Example) == "" {
			analysis.Example = "1号链接含X道荤素菜，2号链接含Y道，相差Z道"
		}
	}
	writeJSON(w, http.StatusOK, analysis)
}

// groupFactsByLink 把方案事实按链接分组，返回「链接 → 事实行」和全量文本（用于数字核对）。
// 公共事实两边都一样，不能作为差异依据，所以不放进分组；多链接事实本身是待改的那条，跳过。
func (s *Server) groupFactsByLink(items []model.LiveAgentPlanFact, linkIDs []int64, nameByID map[int64]string, skipFactID int64) (map[string][]string, string) {
	wanted := map[int64]bool{}
	for _, id := range linkIDs {
		wanted[id] = true
	}
	perLink := map[string][]string{}
	var all strings.Builder
	for _, item := range items {
		if item.ID == skipFactID || item.Status != "active" || item.Scope == nil {
			continue
		}
		if item.Scope.Kind != "product" {
			continue
		}
		ids := item.Scope.ProductScopeLinkIDs()
		if len(ids) == 0 {
			continue
		}
		mine := true
		for _, id := range ids {
			if !wanted[id] {
				mine = false
				break
			}
		}
		if !mine {
			continue
		}
		line := strings.TrimSpace(item.Key) + "：" + strings.TrimSpace(item.Value)
		for _, id := range ids {
			key := nameByID[id]
			if key == "" {
				continue
			}
			perLink[key] = append(perLink[key], line)
		}
		all.WriteString(item.Key)
		all.WriteString(item.Value)
		all.WriteString("\n")
	}
	return perLink, all.String()
}

func parseFactDiffAnalysis(raw string) (factDiffAnalysisResult, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return factDiffAnalysisResult{}, errors.New("no json object")
	}
	var wire struct {
		Found   *bool  `json:"found"`
		Value   string `json:"value"`
		Reason  string `json:"reason"`
		Example string `json:"example"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wire); err != nil {
		return factDiffAnalysisResult{}, err
	}
	if wire.Found == nil {
		return factDiffAnalysisResult{}, errors.New("missing found")
	}
	out := factDiffAnalysisResult{Found: *wire.Found, Value: strings.TrimSpace(wire.Value), Reason: strings.TrimSpace(wire.Reason), Example: strings.TrimSpace(wire.Example)}
	if out.Found && out.Value == "" {
		return factDiffAnalysisResult{}, errors.New("found without value")
	}
	return out, nil
}

// unknownDigits 返回 text 里出现、但事实来源里找不到的数字。
func unknownDigits(text, source string) []string {
	source = strings.ReplaceAll(source, "，", ",")
	have := map[string]bool{}
	for _, d := range factNumberPattern.FindAllString(source, -1) {
		have[d] = true
		// 1.0 与 1 视为同一个数，避免小数写法差异误判。
		if f, err := strconv.ParseFloat(d, 64); err == nil {
			have[strconv.FormatFloat(f, 'f', -1, 64)] = true
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, d := range factNumberPattern.FindAllString(text, -1) {
		if seen[d] {
			continue
		}
		seen[d] = true
		if have[d] {
			continue
		}
		if f, err := strconv.ParseFloat(d, 64); err == nil && have[strconv.FormatFloat(f, 'f', -1, 64)] {
			continue
		}
		missing = append(missing, d)
	}
	return missing
}
