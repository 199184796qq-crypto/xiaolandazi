package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

const planScriptAutoPlannerPrompt = `你是直播话术思路策划。只输出写作方向，不写正文。
要求：
1. objects 从当前可选商品对象里选 1 到 2 个，除非确实需要三款对比，不要每次都全选。
2. attributes 只能从属性目录里选 attribute 键，选 4 到 6 个；商品名/规格/价格由程序按 objects 补齐；必须至少选一个福利属性（shipping_fee 或 voucher_douyin）。写作要求中提到的属性也必须选入 attributes。
3. 自由决定写作角度，不要套固定模板，不要只写“介绍商品”。
4. hook 必须是短句，场景、问题、利益、反差、信任都可以，不能含抢、最后、手慢无、库存、限时、涨价、绝对、保证、百分百。
5. outline 固定七拍，顺序不能变，但只输出这七个标签，不写解释、不写要求：
   ["钩子","福利亮点","门槛说明","下单指引","发货时效","引导分享","回钩子"]
6. 写作思路只放在 writing_requirements 里；用 3 到 6 条短句写清写作模型必须怎么做。
7. style 描述语气、节奏、口语习惯；close 描述收口方式。
8. 不能计划事实里没有的实测、实验、库存、限时、涨价、适用人数。不得在要求中增加未选属性或其他链接的事实。
9. 福利亮点必须写具体福利，不能拿品牌归属或工艺冒充福利；门槛说明、发货时效只能使用当前 facts 或属性目录里已有的事实。有 ship_time_default / ship_time_chuanyu / ship_time_other 时必须选入 attributes，并在 writing_requirements 里要求按事实说；没有对应事实就跳过这一拍，不要写“以物流为准、页面为准、问客服、我不编”这类免责句。
10. 引导分享只邀请观众把直播间或链接分享给需要的人，不能编造分享奖励；回钩子不要重复整句 hook，也不加新事实。
11. 近期已用的开场或对象组合不要重复；可用事实不足时不要强行凑齐数量。
12. 输出严格 JSON：{"objects":["1号链接","3号链接"],"focus":"...","attributes":["..."],"hook":"...","outline":["钩子","福利亮点","门槛说明","下单指引","发货时效","引导分享","回钩子"],"writing_requirements":["..."],"style":"...","close":"..."}`

type planAutoScriptInput struct {
	Text        string   `json:"text"`
	RecentTexts []string `json:"recent_texts"`
}

type planAutoScriptOutput struct {
	Text      string `json:"text"`
	Chars     int    `json:"chars"`
	P0Pending bool   `json:"p0_pending"`
}

type planAutoScriptPlan struct {
	Objects             []string `json:"objects"`
	Focus               string   `json:"focus"`
	Attributes          []string `json:"attributes"`
	Hook                string   `json:"hook"`
	Outline             []string `json:"outline"`
	WritingRequirements []string `json:"writing_requirements"`
	Style               string   `json:"style"`
	Close               string   `json:"close"`
}

type planAutoCatalogItem struct {
	Attribute    string `json:"attribute"`
	Name         string `json:"name"`
	Object       string `json:"object"`
	HasCondition bool   `json:"has_condition"`
	IsBenefit    bool   `json:"is_benefit"`
}

type planAutoLink struct {
	ID    int64
	Label string
	Key   string
	Name  string
	Spec  string
	Price string
}

func (s *Server) planAutoScript(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	var input planAutoScriptInput
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&input)
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "方案不存在或无权查看")
		return
	}
	document, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案提示词失败")
		return
	}
	if strings.TrimSpace(document.Sections.PersonaMD) == "" || strings.TrimSpace(document.Sections.RedLinesMD) == "" {
		writeError(w, http.StatusConflict, "方案还没有主播人设或红线禁止，请先保存方案提示词")
		return
	}
	text, p0Pending, err := s.generateAutoPlanScript(r.Context(), tenantID, planID, document, input)
	if err != nil {
		log.Printf("auto script generation failed tenant=%d plan=%d stage=%s", tenantID, planID, autoScriptFailureStage(err))
		writeError(w, http.StatusBadGateway, autoScriptPublicError(err))
		return
	}
	writeJSON(w, http.StatusOK, planAutoScriptOutput{Text: text, Chars: utf8.RuneCountInString(text), P0Pending: p0Pending})
}

func (s *Server) generateAutoPlanScript(ctx context.Context, tenant, planID int64, document appdb.LiveAgentPlanPromptDocument, input planAutoScriptInput) (string, bool, error) {
	claims, err := s.autoScriptClaims(ctx, tenant, planID)
	if err != nil {
		return "", false, err
	}
	links, err := s.autoScriptLinks(ctx, tenant, planID)
	if err != nil {
		return "", false, err
	}
	catalog := autoScriptCatalog(claims, links)
	if len(catalog) == 0 {
		return "", false, fmt.Errorf("方案没有可用语义属性")
	}
	linkLabels := make([]string, 0, len(links))
	for _, link := range links {
		if link.Label != "" {
			linkLabels = append(linkLabels, link.Label)
		}
	}
	roomIntro, err := s.autoScriptRoomIntro(ctx, tenant, planID)
	if err != nil {
		return "", false, err
	}
	plannerUser := "直播间简介：\n" + roomIntro + "\n\n可写对象：\n" + strings.Join(linkLabels, "、") + "\n\n属性目录：\n"
	for _, item := range catalog {
		benefit := ""
		if item.IsBenefit {
			benefit = "｜福利"
		}
		plannerUser += fmt.Sprintf("- %s｜%s｜对象：%s%s\n", item.Attribute, item.Name, item.Object, benefit)
	}
	plannerUser += autoScriptRecentDirections(input.RecentTexts, links)
	plannerUser += "\n" + planScriptAutoPlannerPrompt
	gateway := s.speechGateway()
	plannerResp, err := s.autoScriptComplete(ctx, tenant, planID, gateway, agentgateway.Request{
		Stage: "script_auto_planner", Messages: autoScriptMessages("你是直播话术思路策划。只返回严格 JSON，不要解释。", plannerUser),
		ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 1000, Timeout: 180 * time.Second,
	})
	if err != nil {
		return "", false, fmt.Errorf("planner_call: %w", err)
	}
	var plan planAutoScriptPlan
	if err := autoScriptJSON(plannerResp.Text, &plan); err != nil {
		return "", false, fmt.Errorf("planner_parse: %w", err)
	}
	plan.Objects = normalizeAutoObjects(plan.Objects, links)
	if len(plan.Objects) == 0 {
		for i, link := range links {
			if i >= 2 {
				break
			}
			plan.Objects = append(plan.Objects, link.Label)
		}
	}
	plan.Attributes = normalizeAutoAttributes(plan.Attributes, catalog)
	plan.Attributes = autoScriptRelevantAttributes(plan.Attributes, catalog, plan.Objects)
	if len(plan.Attributes) == 0 {
		for _, item := range catalog {
			if len(plan.Attributes) >= 4 {
				break
			}
			if autoScriptCatalogRelevant(item, plan.Objects) && !autoScriptContains(plan.Attributes, item.Attribute) {
				plan.Attributes = append(plan.Attributes, item.Attribute)
			}
		}
	}
	plan.Attributes = ensureAutoScriptShippingAttribute(plan.Attributes, plan.Outline, catalog)
	if autoScriptContains(plan.Attributes, "ship_time_default") {
		requirements := make([]string, 0, len(plan.WritingRequirements)+1)
		for _, requirement := range plan.WritingRequirements {
			if strings.Contains(requirement, "发货") {
				continue
			}
			requirements = append(requirements, requirement)
		}
		requirements = append(requirements, "发货时效必须说明下单后第二天上午发货；没有其它地区事实就不要扩展，不要写免责句。")
		plan.WritingRequirements = requirements
	}
	if len(plan.Outline) != 7 {
		plan.Outline = []string{"钩子", "福利亮点", "门槛说明", "下单指引", "发货时效", "引导分享", "回钩子"}
	}
	if len(plan.WritingRequirements) == 0 {
		plan.WritingRequirements = []string{"先讲钩子，再讲事实，最后给明确选择", "只使用可用事实，不编造适用人数、库存、时效和承诺"}
	}
	facts := buildAutoFacts(claims, links, plan)
	if strings.TrimSpace(facts) == "" {
		return "", false, fmt.Errorf("facts_empty")
	}
	writerPrompt := autoScriptWriterPrompt(document.Sections.PersonaMD, document.Sections.RedLinesMD, plan, facts)
	for attempt := 0; attempt < 2; attempt++ {
		prompt := writerPrompt
		if attempt > 0 {
			prompt += "\n\n上一次经审核处理后没有可用正文，请依据事实重新写一份完整话术。"
		}
		resp, err := s.autoScriptComplete(ctx, tenant, planID, gateway, agentgateway.Request{
			Stage: "script_auto_writer", Messages: autoScriptMessages("你是真人主播。只返回严格 JSON。", prompt),
			ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 3000, Timeout: 180 * time.Second,
		})
		if err != nil {
			return "", false, fmt.Errorf("writer_call: %w", err)
		}
		var parsed struct {
			Body string `json:"body"`
		}
		if err := autoScriptJSON(resp.Text, &parsed); err != nil {
			return "", false, fmt.Errorf("writer_parse: %w", err)
		}
		body := strings.TrimSpace(parsed.Body)
		if body == "" {
			return "", false, fmt.Errorf("writer_parse: empty body")
		}
		repaired, pending := s.repairAutoScriptP0(ctx, tenant, planID, gateway, body)
		repaired = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(repaired, "{"), "}"))
		if repaired != "" {
			return repaired, pending, nil
		}
	}
	return "", false, fmt.Errorf("writer_parse: empty body")
}

func (s *Server) autoScriptRoomIntro(ctx context.Context, tenant, plan int64) (string, error) {
	versions, err := s.store.ListLiveAgentPlanVersionsForPlan(ctx, tenant, plan)
	if err != nil {
		return "", err
	}
	var published *model.LiveAgentPlanVersion
	for i := range versions {
		if versions[i].LifecycleStatus != "published" {
			continue
		}
		if published == nil || versions[i].ID > published.ID {
			published = &versions[i]
		}
	}
	if published == nil {
		return "", fmt.Errorf("方案还没有已发布版本，无法读取直播间简介")
	}
	intro, _ := published.GenerationContext["room_intro"].(string)
	intro = strings.TrimSpace(intro)
	if intro == "" {
		return "", fmt.Errorf("方案没有直播间简介，请先在方案配置中填写")
	}
	return intro, nil
}

func (s *Server) autoScriptClaims(ctx context.Context, tenant, plan int64) ([]model.PlanMaterialClaim, error) {
	claims, err := s.store.ListPlanMaterialClaims(ctx, tenant, plan)
	return slices.DeleteFunc(claims, func(claim model.PlanMaterialClaim) bool { return claim.SharedSourceNeedsReview }), err
}

func (s *Server) autoScriptLinks(ctx context.Context, tenant, plan int64) ([]planAutoLink, error) {
	rows, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	out := make([]planAutoLink, 0, len(rows))
	for _, row := range rows {
		label := strings.TrimSpace(row.LinkKey)
		if n, e := strconv.Atoi(label); e == nil && n > 0 {
			label = fmt.Sprintf("%d号链接", n)
		} else if label != "" {
			label += "号链接"
		}
		out = append(out, planAutoLink{ID: row.ID, Label: label, Key: row.LinkKey, Name: row.ProductName, Spec: row.Spec, Price: row.DailyPrice})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func autoScriptCatalog(claims []model.PlanMaterialClaim, links []planAutoLink) []planAutoCatalogItem {
	linkLabel := map[int64]string{}
	for _, l := range links {
		linkLabel[l.ID] = l.Label
	}
	seen := map[string]int{}
	out := []planAutoCatalogItem{}
	for _, c := range claims {
		if !autoScriptClaimUsable(c) {
			continue
		}
		key := strings.TrimSpace(c.ClaimKey)
		if key == "" || key == "product_name" || key == "spec" || key == "price" {
			continue
		}
		for _, object := range autoScriptClaimObjects(c, linkLabel) {
			unique := key + "|" + object
			if i, ok := seen[unique]; ok {
				out[i].IsBenefit = out[i].IsBenefit || c.IsBenefit
				continue
			}
			seen[unique] = len(out)
			out = append(out, planAutoCatalogItem{Attribute: key, Name: strings.TrimSpace(c.Topic), Object: object, IsBenefit: c.IsBenefit})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsBenefit != out[j].IsBenefit {
			return out[i].IsBenefit
		}
		if out[i].Attribute == out[j].Attribute {
			return out[i].Object < out[j].Object
		}
		return out[i].Attribute < out[j].Attribute
	})
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func autoScriptJSON(raw string, v any) error {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	return json.Unmarshal([]byte(raw), v)
}

func autoScriptConditions(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "[]" || text == "null" {
		return ""
	}
	return text
}

func autoScriptContainsText(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func ensureAutoScriptShippingAttribute(attributes, outline []string, catalog []planAutoCatalogItem) []string {
	if !autoScriptContainsText(outline, "发货时效") {
		return attributes
	}
	available := map[string]bool{}
	for _, item := range catalog {
		available[item.Attribute] = true
	}
	if !autoScriptContains(attributes, "ship_time_default") {
		attributes = append(attributes, "ship_time_default")
	}
	for _, key := range []string{"ship_time_default", "ship_time_chuanyu", "ship_time_other"} {
		if autoScriptContains(attributes, key) {
			return attributes
		}
		if available[key] {
			return append(attributes, key)
		}
	}
	return attributes
}

func autoScriptClaimConditionText(claim model.PlanMaterialClaim) string {
	parts := []string{}
	for _, raw := range []json.RawMessage{claim.Conditions, claim.Exceptions} {
		text := strings.TrimSpace(string(raw))
		if text == "" || text == "[]" || text == "null" {
			continue
		}
		var rows []map[string]any
		if json.Unmarshal(raw, &rows) == nil && len(rows) > 0 {
			for _, row := range rows {
				if value, ok := row["raw"].(string); ok && strings.TrimSpace(value) != "" {
					parts = append(parts, strings.TrimSpace(value))
					continue
				}
				if compact, err := json.Marshal(row); err == nil {
					parts = append(parts, string(compact))
				}
			}
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "；")
}

func autoScriptFactLine(label, text, condition string) string {
	line := label + "：" + text
	if condition != "" {
		line += "；条件：" + condition
	}
	return line
}

func autoScriptMessages(system, user string) []agentgateway.Message {
	return []agentgateway.Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
}

func autoScriptWriterPrompt(persona, redlines string, plan planAutoScriptPlan, facts string) string {
	return `你是真人主播。像在直播间跟熟人唠家常一样说话。

【主播人设】
` + persona + `

【直播话术结构】
` + strings.Join(plan.Outline, " → ") + `

【写作目的】
` + plan.Focus + `

【红线禁止】
` + redlines + `

【事实】
` + facts + `

按这个结构、写作目的和事实，写一条直播口播。只输出 JSON：{"body":"正文"}`
}

var autoScriptBadWords = []string{"抢", "最后一天", "手慢无", "库存告急", "马上涨价", "百分百", "绝对", "保证", "唯一", "顶级", "全网第一"}

func autoScriptHasBad(text string) bool {
	for _, w := range autoScriptBadWords {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

func normalizeAutoObjects(objects []string, links []planAutoLink) []string {
	out := []string{}
	for _, object := range objects {
		object = strings.TrimSpace(object)
		for _, link := range links {
			if object == link.Label || object == link.Key || object == strconv.FormatInt(link.ID, 10) {
				found := false
				for _, existing := range out {
					if existing == link.Label {
						found = true
					}
				}
				if !found {
					out = append(out, link.Label)
				}
			}
		}
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func normalizeAutoAttributes(attributes []string, catalog []planAutoCatalogItem) []string {
	out := []string{}
	for _, attribute := range attributes {
		attribute = strings.TrimSpace(attribute)
		for _, item := range catalog {
			if attribute == item.Attribute || attribute == item.Name {
				found := false
				for _, existing := range out {
					if existing == item.Attribute {
						found = true
					}
				}
				if !found {
					out = append(out, item.Attribute)
				}
			}
		}
	}
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func autoScriptClaimText(claim model.PlanMaterialClaim) string {
	for _, value := range []string{claim.SourceSentence, claim.SourceQuote, claim.Content} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func autoScriptCompanionAttribute(attribute string) []string {
	switch attribute {
	case "shelf_life_vacuum":
		return []string{"shelf_life_non_vacuum"}
	case "shelf_life_non_vacuum":
		return []string{"shelf_life_vacuum"}
	case "shipping_fee":
		return []string{"shipping_remote_surcharge"}
	}
	return nil
}

func buildAutoFacts(claims []model.PlanMaterialClaim, links []planAutoLink, plan planAutoScriptPlan) string {
	selected := map[int64]bool{}
	confirmed := []string{}
	recall := []string{}
	conditional := []string{}
	linkByName := map[string]planAutoLink{}
	for _, link := range links {
		linkByName[link.Label] = link
	}
	for _, object := range plan.Objects {
		link, ok := linkByName[object]
		if !ok {
			continue
		}
		name, spec, price := link.Name, link.Spec, link.Price
		for _, claim := range claims {
			if !autoScriptClaimUsable(claim) || !autoScriptClaimIsProductSpecific(claim, link.ID) {
				continue
			}
			switch strings.TrimSpace(claim.ClaimKey) {
			case "product_name":
				name = strings.TrimSpace(claim.Content)
			case "spec":
				spec = strings.TrimSpace(claim.Content)
			case "price":
				price = strings.TrimSpace(claim.Content)
			}
		}
		parts := []string{}
		if name != "" {
			parts = append(parts, "商品名："+name)
		}
		if spec != "" {
			parts = append(parts, "规格："+spec)
		}
		if price != "" {
			parts = append(parts, "价格："+price)
		}
		if len(parts) > 0 {
			confirmed = append(confirmed, object+"："+strings.Join(parts, "；"))
		}
	}
	addClaim := func(claim model.PlanMaterialClaim, target string) {
		if selected[claim.ID] || !autoScriptClaimUsable(claim) {
			return
		}
		labels := autoScriptSelectedClaimLabels(claim, plan.Objects, links)
		if len(labels) == 0 {
			return
		}
		selected[claim.ID] = true
		line := autoScriptFactLine(strings.Join(labels, "、")+"·"+strings.TrimSpace(claim.Topic), autoScriptClaimText(claim), autoScriptClaimConditionText(claim))
		switch target {
		case "recall":
			recall = append(recall, line)
		case "conditional":
			conditional = append(conditional, line)
		default:
			confirmed = append(confirmed, line)
		}
	}
	for _, attribute := range plan.Attributes {
		for _, claim := range claims {
			if strings.TrimSpace(claim.ClaimKey) != attribute {
				continue
			}
			if autoScriptClaimConditionText(claim) != "" {
				addClaim(claim, "conditional")
			} else {
				addClaim(claim, "confirmed")
			}
		}
		for _, companion := range autoScriptCompanionAttribute(attribute) {
			for _, claim := range claims {
				if strings.TrimSpace(claim.ClaimKey) == companion {
					addClaim(claim, "recall")
				}
			}
		}
	}
	for _, claim := range claims {
		if claim.IsBenefit && autoScriptClaimUsable(claim) {
			if autoScriptClaimConditionText(claim) != "" {
				addClaim(claim, "conditional")
			} else {
				addClaim(claim, "confirmed")
			}
		}
	}
	sections := []string{}
	if len(confirmed) > 0 {
		sections = append(sections, "【确定事实】\n"+strings.Join(confirmed, "\n"))
	}
	if len(recall) > 0 {
		sections = append(sections, "【召回事实】\n"+strings.Join(recall, "\n"))
	}
	if len(conditional) > 0 {
		sections = append(sections, "【有条件事实】\n"+strings.Join(conditional, "\n"))
	}
	return strings.Join(sections, "\n\n")
}
