package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentunderstanding"
	"livecompanion/management/internal/model"
)

type liveStrategyIntentInput struct {
	Message     string                     `json:"message"`
	PlanID      int64                      `json:"plan_id,omitempty"`
	CurrentMode string                     `json:"current_mode,omitempty"`
	History     []liveAgentChatHistoryItem `json:"history,omitempty"`
	ImageURLs   []string                   `json:"image_urls,omitempty"`
}

type liveStrategyIntentTarget struct {
	LinkKey            string `json:"link_key,omitempty"`
	BenefitKey         string `json:"benefit_key,omitempty"`
	FactCategory       string `json:"fact_category,omitempty"`
	FactKey            string `json:"fact_key,omitempty"`
	ScriptReferenceKey string `json:"script_reference_key,omitempty"`
	ScriptTitle        string `json:"script_title,omitempty"`
	PlanID             int64  `json:"plan_id,omitempty"`
	PlanName           string `json:"plan_name,omitempty"`
}

type liveStrategyIntentChanges struct {
	ProductName   string `json:"product_name,omitempty"`
	Spec          string `json:"spec,omitempty"`
	DailyPrice    string `json:"daily_price,omitempty"`
	Quantity      string `json:"quantity,omitempty"`
	Audience      string `json:"audience,omitempty"`
	ActivityPrice string `json:"activity_price,omitempty"`
	Gift          string `json:"gift,omitempty"`
	Activity      string `json:"activity,omitempty"`
	StartsAt      string `json:"starts_at,omitempty"`
	EndsAt        string `json:"ends_at,omitempty"`
	FactValue     string `json:"fact_value,omitempty"`
	ScriptText    string `json:"script_text,omitempty"`
}

type liveStrategyIntentModelOutput struct {
	Kind       string                    `json:"kind"`
	Intent     string                    `json:"intent"`
	Reply      string                    `json:"reply,omitempty"`
	Target     liveStrategyIntentTarget  `json:"target,omitempty"`
	Changes    liveStrategyIntentChanges `json:"changes,omitempty"`
	Missing    []string                  `json:"missing,omitempty"`
	Confidence float64                   `json:"confidence,omitempty"`
}

type liveStrategyIntentOutput struct {
	ProtocolVersion    string                    `json:"protocol_version"`
	State              string                    `json:"state"`
	Code               string                    `json:"code,omitempty"`
	RequiredPermission string                    `json:"required_permission,omitempty"`
	Kind               string                    `json:"kind"`
	Intent             string                    `json:"intent"`
	Reply              string                    `json:"reply,omitempty"`
	Target             liveStrategyIntentTarget  `json:"target,omitempty"`
	Changes            liveStrategyIntentChanges `json:"changes,omitempty"`
	Missing            []string                  `json:"missing,omitempty"`
	Confidence         float64                   `json:"confidence,omitempty"`
	Provider           string                    `json:"provider,omitempty"`
	Model              string                    `json:"model,omitempty"`
	LatencyMS          int64                     `json:"latency_ms,omitempty"`
	Engine             string                    `json:"engine,omitempty"`
	PolicySource       string                    `json:"policy_source,omitempty"`
}

type liveStrategyIntentProductContext struct {
	LinkKey     string `json:"link_key"`
	ProductName string `json:"product_name,omitempty"`
	Spec        string `json:"spec,omitempty"`
	DailyPrice  string `json:"daily_price,omitempty"`
	Quantity    string `json:"quantity,omitempty"`
	Audience    string `json:"audience,omitempty"`
	VersionNo   int64  `json:"version_no"`
}

type liveStrategyIntentBenefitContext struct {
	Key           string `json:"key"`
	LinkKey       string `json:"link_key,omitempty"`
	ProductName   string `json:"product_name,omitempty"`
	ActivityPrice string `json:"activity_price,omitempty"`
	Gift          string `json:"gift,omitempty"`
	Activity      string `json:"activity,omitempty"`
	StartsAt      string `json:"starts_at,omitempty"`
	EndsAt        string `json:"ends_at,omitempty"`
	Status        string `json:"status"`
	VersionNo     int64  `json:"version_no"`
}

type liveStrategyIntentFactContext struct {
	Category  string `json:"category"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	VersionNo int64  `json:"version_no"`
}

type liveStrategyIntentPlanContext struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Bound    bool   `json:"bound"`
	Selected bool   `json:"selected"`
}

type liveStrategyIntentScriptReferenceContext struct {
	ReferenceKey  string `json:"reference_key"`
	Title         string `json:"title"`
	ContentText   string `json:"content_text"`
	Goal          string `json:"goal,omitempty"`
	Transition    string `json:"transition,omitempty"`
	ExecutionMode string `json:"execution_mode"`
	VersionNo     int64  `json:"version_no"`
}

func normalizeLiveStrategyIntentKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat":
		return "chat"
	case "command":
		return "command"
	case "clarify":
		return "clarify"
	default:
		return "clarify"
	}
}

func normalizeLiveStrategyIntent(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "chat":
		return "chat"
	case "product.add", "product.update", "product.disable",
		"benefit.add", "benefit.update", "benefit.disable",
		"fact.add", "fact.update", "fact.disable",
		"script.add", "script.update", "script.disable",
		"plan.bind", "plan.unbind", "plan.switch":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "unknown"
	}
}

func formatIntentTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(liveAgentLocation()).Format("2006-01-02 15:04")
}

func liveStrategyProgramOutput(value agentunderstanding.UnifiedIntent) liveStrategyIntentModelOutput {
	result := liveStrategyIntentModelOutput{
		Kind:       normalizeLiveStrategyIntentKind(value.Kind),
		Intent:     normalizeLiveStrategyIntent(value.Intent),
		Reply:      strings.TrimSpace(value.Reply),
		Missing:    append([]string(nil), value.Missing...),
		Confidence: value.Confidence,
	}
	if target := value.Target; target != nil {
		result.Target.LinkKey = intentMapString(target, "link_key")
		result.Target.BenefitKey = intentMapString(target, "benefit_key")
		result.Target.FactCategory = intentMapString(target, "fact_category")
		result.Target.FactKey = intentMapString(target, "fact_key")
		result.Target.ScriptReferenceKey = intentMapString(target, "script_reference_key")
		result.Target.ScriptTitle = intentMapString(target, "script_title")
		result.Target.PlanID = intentMapInt64(target, "plan_id")
		result.Target.PlanName = intentMapString(target, "plan_name")
	}
	if changes := value.Changes; changes != nil {
		result.Changes.ProductName = intentMapString(changes, "product_name")
		result.Changes.Spec = intentMapString(changes, "spec")
		result.Changes.DailyPrice = intentMapString(changes, "daily_price")
		result.Changes.Quantity = intentMapString(changes, "quantity")
		result.Changes.Audience = intentMapString(changes, "audience")
		result.Changes.ActivityPrice = intentMapString(changes, "activity_price")
		result.Changes.Gift = intentMapString(changes, "gift")
		result.Changes.Activity = intentMapString(changes, "activity")
		result.Changes.StartsAt = intentMapString(changes, "starts_at")
		result.Changes.EndsAt = intentMapString(changes, "ends_at")
		result.Changes.FactValue = intentMapString(changes, "fact_value")
		result.Changes.ScriptText = intentMapString(changes, "script_text")
	}
	if result.Kind == "chat" {
		result.Intent = "chat"
	}
	return result
}

func intentMapString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intentMapInt64(values map[string]any, key string) int64 {
	value, ok := values[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
		return parsed
	}
}

func (s *Server) liveStrategyIntentInterpret(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	if actor.IsInternalStaff() && !actor.IsPlatformAdmin() {
		access, err := s.staffAccessForActor(r, actor)
		if err != nil || !staffHasPermission(access, "liveops.configure") {
			s.auditLiveStrategyAgentCommand(r, actor, "interpret", agentStatePermissionDenied, liveStrategyExecutePayload{RoomID: roomID}, "permission_denied")
			writeJSON(w, http.StatusOK, liveStrategyIntentOutput{
				ProtocolVersion:    agentProtocolVersion,
				State:              agentStatePermissionDenied,
				Code:               "permission_denied",
				RequiredPermission: "liveops.configure",
				Kind:               "chat",
				Intent:             "chat",
				Reply:              "你当前没有直播智能体方案配置权限，不能通过智能体读取或修改客户直播策略。",
			})
			return
		}
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前账号范围")
		return
	}

	var input liveStrategyIntentInput
	if err := readAgentCompatibleJSON(w, r, &input); err != nil {
		log.Printf("live strategy intent decode failed room=%d content_length=%d: %v", roomID, r.ContentLength, err)
		writeError(w, http.StatusBadRequest, "直播智能体意图请求解析失败，请刷新页面后重试")
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	input.CurrentMode = strings.ToLower(strings.TrimSpace(input.CurrentMode))
	if input.Message == "" {
		writeError(w, http.StatusBadRequest, "请输入内容")
		return
	}
	if utf8.RuneCountInString(input.Message) > 4000 {
		writeError(w, http.StatusBadRequest, "单次输入不能超过 4000 字")
		return
	}
	var imageErr error
	input.ImageURLs, imageErr = sanitizeAgentChatImageURLs(input.ImageURLs)
	if imageErr != nil {
		writeError(w, http.StatusBadRequest, imageErr.Error())
		return
	}
	if !actor.IsInternalStaff() && customerAgentRestrictedRequest(input.Message) {
		writeJSON(w, http.StatusOK, liveStrategyIntentOutput{
			ProtocolVersion: agentProtocolVersion,
			State:           agentStatePermissionDenied,
			Code:            "domain_forbidden",
			Kind:            "chat",
			Intent:          "chat",
			Reply:           clientAgentBoundaryReply,
		})
		return
	}
	understandingPolicy, err := s.store.ResolveAgentUnderstandingPolicy(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取智能体理解策略失败")
		return
	}
	contextLimit := understandingPolicy.MaxContextMessages
	if contextLimit <= 0 {
		contextLimit = 10
	}
	if len(input.History) > contextLimit {
		input.History = input.History[len(input.History)-contextLimit:]
	}

	products := make([]liveStrategyIntentProductContext, 0)
	benefits := make([]liveStrategyIntentBenefitContext, 0)
	facts := make([]liveStrategyIntentFactContext, 0)
	scriptReferences := make([]liveStrategyIntentScriptReferenceContext, 0)
	planName := ""
	if input.PlanID > 0 {
		plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, input.PlanID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "当前直播智能体方案不存在")
			return
		}
		bound, err := s.store.IsRoomBoundToLiveAgentPlan(r.Context(), tenantID, roomID, input.PlanID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "校验直播间方案绑定失败")
			return
		}
		if !bound {
			writeError(w, http.StatusForbidden, "当前方案未绑定到这个直播间")
			return
		}
		planName = plan.Name

		productItems, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, input.PlanID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取正式商品链接失败")
			return
		}
		for _, item := range productItems {
			products = append(products, liveStrategyIntentProductContext{
				LinkKey: item.LinkKey, ProductName: item.ProductName, Spec: item.Spec,
				DailyPrice: item.DailyPrice, Quantity: item.Quantity, Audience: item.Audience,
				VersionNo: item.VersionNo,
			})
		}

		benefitItems, err := s.store.ListLiveAgentPlanBenefits(r.Context(), tenantID, input.PlanID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取正式活动福利失败")
			return
		}
		for _, item := range benefitItems {
			benefits = append(benefits, liveStrategyIntentBenefitContext{
				Key: item.Key, LinkKey: item.LinkKey, ProductName: item.ProductName,
				ActivityPrice: item.ActivityPrice, Gift: item.Gift, Activity: item.Activity,
				StartsAt: formatIntentTime(item.StartsAt), EndsAt: formatIntentTime(item.EndsAt),
				Status: item.Status, VersionNo: item.VersionNo,
			})
		}

		factItems, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, input.PlanID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取正式事实依据失败")
			return
		}
		for _, item := range factItems {
			facts = append(facts, liveStrategyIntentFactContext{
				Category: item.Category, Key: item.Key, Value: item.Value, VersionNo: item.VersionNo,
			})
		}

		scriptItems, err := s.store.ListLiveAgentPlanScriptReferences(r.Context(), tenantID, input.PlanID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取正式话术参考失败")
			return
		}
		for _, item := range scriptItems {
			scriptReferences = append(scriptReferences, liveStrategyIntentScriptReferenceContext{
				ReferenceKey:  item.ReferenceKey,
				Title:         item.Title,
				ContentText:   item.ContentText,
				Goal:          item.Goal,
				Transition:    item.Transition,
				ExecutionMode: item.ExecutionMode,
				VersionNo:     item.VersionNo,
			})
		}
	}

	allPlans, err := s.store.ListLiveAgentPlans(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	boundPlans, err := s.store.ListLiveAgentPlansForRoom(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间已绑定方案失败")
		return
	}
	boundPlanIDs := make(map[int64]bool, len(boundPlans))
	for _, item := range boundPlans {
		boundPlanIDs[item.ID] = true
	}
	selectedPlanID := int64(0)
	if selected, selectedErr := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID); selectedErr == nil {
		selectedPlanID = selected.ID
	}
	plans := make([]liveStrategyIntentPlanContext, 0, len(allPlans))
	for _, item := range allPlans {
		plans = append(plans, liveStrategyIntentPlanContext{
			ID: item.ID, Name: item.Name, Status: item.Status,
			Bound: boundPlanIDs[item.ID], Selected: item.ID == selectedPlanID,
		})
	}

	now := time.Now().In(liveAgentLocation())
	programContext := agentunderstanding.LiveStrategyProgramContext{
		CurrentMode: input.CurrentMode,
		HasImages:   len(input.ImageURLs) > 0,
		Now:         now,
	}
	for _, item := range products {
		programContext.Products = append(programContext.Products, agentunderstanding.LiveStrategyProduct{
			LinkKey: item.LinkKey, ProductName: item.ProductName, Spec: item.Spec,
			DailyPrice: item.DailyPrice, Quantity: item.Quantity, Audience: item.Audience,
		})
	}
	for _, item := range benefits {
		if item.Status == "disabled" {
			continue
		}
		programContext.Benefits = append(programContext.Benefits, agentunderstanding.LiveStrategyBenefit{
			Key: item.Key, LinkKey: item.LinkKey, ProductName: item.ProductName,
			ActivityPrice: item.ActivityPrice, Gift: item.Gift, Activity: item.Activity,
			StartsAt: item.StartsAt, EndsAt: item.EndsAt,
		})
	}
	for _, item := range facts {
		programContext.Facts = append(programContext.Facts, agentunderstanding.LiveStrategyFact{
			Category: item.Category, Key: item.Key, Value: item.Value,
		})
	}
	for _, item := range scriptReferences {
		programContext.ScriptReferences = append(programContext.ScriptReferences, agentunderstanding.LiveStrategyScriptReference{
			ReferenceKey: item.ReferenceKey, Title: item.Title, ContentText: item.ContentText,
		})
	}
	for _, item := range plans {
		programContext.Plans = append(programContext.Plans, agentunderstanding.LiveStrategyPlan{
			ID: item.ID, Name: item.Name, Bound: item.Bound, Selected: item.Selected,
		})
	}
	programIntent := agentunderstanding.ProgramInterpretLiveStrategy(input.Message, programContext)
	programResolved := programIntent.Kind != agentunderstanding.KindClarify && programIntent.Intent != "unknown"
	if !agentunderstanding.UseModel(understandingPolicy.AgentUnderstandingPolicy, programResolved, programIntent.Confidence) {
		output := liveStrategyProgramOutput(programIntent)
		writeJSON(w, http.StatusOK, liveStrategyIntentOutput{
			ProtocolVersion: agentProtocolVersion, State: agentStateResponded,
			Kind: output.Kind, Intent: output.Intent, Reply: output.Reply,
			Target: output.Target, Changes: output.Changes, Missing: output.Missing,
			Confidence: output.Confidence, Engine: "program", PolicySource: understandingPolicy.ResolvedFrom,
		})
		return
	}

	productsJSON, _ := json.Marshal(products)
	benefitsJSON, _ := json.Marshal(benefits)
	factsJSON, _ := json.Marshal(facts)
	scriptReferencesJSON, _ := json.Marshal(scriptReferences)
	plansJSON, _ := json.Marshal(plans)
	assistantName := s.configuredAgentName(r.Context(), actor.IsInternalStaff())
	systemPrompt := fmt.Sprintf(`
你是%s的“直播策略意图解释器”。你的职责只有：理解用户自然语言并输出结构化意图；绝不执行写库、发布、停用或保存。

当前时间：%s
当前模块：%s
当前方案：%s
正式商品链接：%s
正式活动福利：%s
正式事实依据：%s
正式话术参考：%s
直播间可见方案（bound=已绑定，selected=当前运行选择）：%s

【输出】只输出 JSON，不要 Markdown：
{
  "kind": "chat|command|clarify",
  "intent": "chat|product.add|product.update|product.disable|benefit.add|benefit.update|benefit.disable|fact.add|fact.update|fact.disable|script.add|script.update|script.disable|plan.bind|plan.unbind|plan.switch|unknown",
  "reply": "普通聊天时直接回复；需要澄清时给一句简短问题；command 时可留空",
  "target": {"link_key":"", "benefit_key":"", "fact_category":"", "fact_key":"", "script_reference_key":"", "script_title":"", "plan_id":0, "plan_name":""},
  "changes": {
    "product_name":"", "spec":"", "daily_price":"", "quantity":"", "audience":"",
    "activity_price":"", "gift":"", "activity":"", "starts_at":"", "ends_at":"",
    "fact_value":"", "script_text":""
  },
  "missing": [],
  "confidence": 0.0
}

【核心规则】
1. 当前模块只是上下文提示，不等于用户每句话都是修改命令。普通聊天必须 kind=chat、intent=chat。
2. “1号链接活动”“1号链接的福利”表示以1号链接定位活动福利；如果修改的是赠品、活动价、活动内容、开始/结束/截止时间，必须是 benefit.*，不是 product.*。
3. 只有修改商品名称、规格、日常价、数量、适用人群等稳定商品资料时才是 product.*。
4. update 的 changes 只放用户明确想改的字段，禁止为了凑完整对象复制旧值。
5. 用户说“现在/立即/马上/立即生效”作为开始时间时，将 starts_at 解析成当前时间，格式 YYYY-MM-DD HH:mm。其它相对时间也尽量结合当前时间解析；无法可靠确定就放进 missing 并 kind=clarify。
6. 用户说“修改活动”但没说字段时，benefit.update + kind=clarify；用户说“修改1号链接”且上下文无法判断是商品还是活动时，kind=clarify、intent=unknown。
7. 用户说“取消/不用了/算了”属于上层状态机处理；如果来到这里，当普通 chat 处理，不生成业务 command。
8. 不得说“已修改/已保存/已发布/已生效”。command 只代表理解结果，真正执行由外层确认和业务接口完成。
9. 优先利用正式商品链接和活动福利解析“这个/刚才那个/1号链接”等指代；仍有歧义就 clarify，不猜。
10. 发货地、快递、产地、保质期、资质、稳定库存说明等可核对信息属于 fact.*。fact.update 必须用 target.fact_category + target.fact_key 唯一定位；changes.fact_value 只放新值。
11. “绑定方案”=plan.bind；“解绑方案”=plan.unbind；“切换/使用/运行某方案”=plan.switch。优先用方案列表匹配 plan_id 和 plan_name。切换只针对已经 bound 的方案；未绑定时仍输出 plan.switch，外层会提示先绑定，不要偷偷变成 bind。
12. “话术参考/参考说法/主播怎么说”属于 script.*。新增用 script.add；修改已有参考用 script.update；停用/删除已有参考用 script.disable。target.script_reference_key 和 target.script_title 用正式话术参考上下文唯一定位；changes.script_text 只放用户想保存或修改的正文。话术参考只控制怎么组织、怎么说，不能把其中出现的商品事实自动升级成事实依据。
13. 有图片时必须结合图片可见内容理解。用户只是“看看/识别/这是什么”而没有要求写入时，kind=chat，直接说明图片可见信息；用户明确要求把图片内容添加/修改到商品、活动或事实时，再输出相应 command。看不清、被遮挡或图片没有提供的字段禁止猜测。
`, assistantName, now.Format("2006-01-02 15:04:05 MST"), input.CurrentMode, planName, string(productsJSON), string(benefitsJSON), string(factsJSON), string(scriptReferencesJSON), string(plansJSON))

	roomRef := roomID
	invocationID := s.beginAISingleUse(r.Context(), actor, &roomRef, "live_strategy_intent", map[string]any{
		"plan_id": input.PlanID, "current_mode": input.CurrentMode,
		"understanding_mode": understandingPolicy.Mode, "policy_source": understandingPolicy.ResolvedFrom,
	})
	output, modelResponse, err := callLiveStrategyIntentModel(
		r.Context(), systemPrompt, input.Message, input.History, input.ImageURLs, understandingPolicy.AgentUnderstandingPolicy,
	)
	if err != nil {
		s.finishAISingleUseWithUsage(
			r.Context(), invocationID, "failed", modelResponse.Provider, modelResponse.Model, modelResponse.LatencyMS,
			modelResponse.InputTokens, modelResponse.OutputTokens, modelResponse.TotalTokens, map[string]any{"error": err.Error()},
		)
		fallback := liveStrategyProgramOutput(programIntent)
		if fallback.Reply == "" {
			fallback.Reply = "大模型理解暂时不可用，我没有执行任何修改。请把目标和字段说得更明确一些。"
		}
		writeJSON(w, http.StatusOK, liveStrategyIntentOutput{
			ProtocolVersion: agentProtocolVersion, State: agentStateResponded,
			Kind: fallback.Kind, Intent: fallback.Intent, Reply: fallback.Reply,
			Target: fallback.Target, Changes: fallback.Changes, Missing: fallback.Missing,
			Confidence: fallback.Confidence, Engine: "program_fallback", PolicySource: understandingPolicy.ResolvedFrom,
		})
		return
	}
	if output.Confidence < understandingPolicy.MinConfidence {
		output.Kind = "clarify"
		if strings.TrimSpace(output.Reply) == "" {
			output.Reply = "我还不能可靠确定你的真实意图，请再明确一下要处理的对象和字段。"
		}
		if output.Intent == "chat" {
			output.Intent = "unknown"
		}
	}
	s.finishAISingleUseWithUsage(
		r.Context(), invocationID, "succeeded", modelResponse.Provider, modelResponse.Model, modelResponse.LatencyMS,
		modelResponse.InputTokens, modelResponse.OutputTokens, modelResponse.TotalTokens, nil,
	)
	writeJSON(w, http.StatusOK, liveStrategyIntentOutput{
		ProtocolVersion: agentProtocolVersion, State: agentStateResponded,
		Kind: output.Kind, Intent: output.Intent, Reply: output.Reply,
		Target: output.Target, Changes: output.Changes, Missing: output.Missing,
		Confidence: output.Confidence, Provider: modelResponse.Provider, Model: modelResponse.Model, LatencyMS: modelResponse.LatencyMS,
		Engine: "model", PolicySource: understandingPolicy.ResolvedFrom,
	})
}

func callLiveStrategyIntentModel(
	ctx context.Context,
	systemPrompt, message string,
	history []liveAgentChatHistoryItem,
	imageURLs []string,
	policy model.AgentUnderstandingPolicy,
) (liveStrategyIntentModelOutput, agentgateway.Response, error) {
	policy = agentunderstanding.NormalizePolicy(policy)
	if len(history) > policy.MaxContextMessages {
		history = history[len(history)-policy.MaxContextMessages:]
	}
	messages := []agentgateway.Message{{Role: "system", Content: strings.TrimSpace(systemPrompt)}}
	for _, item := range history {
		role := strings.TrimSpace(item.Role)
		if role == "agent" {
			role = "assistant"
		}
		if role != "assistant" && role != "user" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > 1000 {
			text = string([]rune(text)[:1000])
		}
		messages = append(messages, agentgateway.Message{Role: role, Content: text})
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: message, ImageURLs: imageURLs})

	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Provider:       policy.Provider,
		Model:          policy.Model,
		Messages:       messages,
		MaxTokens:      policy.MaxTokens,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        agentunderstanding.Timeout(policy),
	})
	if err != nil {
		return liveStrategyIntentModelOutput{}, result, err
	}
	var output liveStrategyIntentModelOutput
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &output); err != nil {
		return liveStrategyIntentModelOutput{}, result, fmt.Errorf("decode live strategy intent: %w", err)
	}
	output.Kind = normalizeLiveStrategyIntentKind(output.Kind)
	output.Intent = normalizeLiveStrategyIntent(output.Intent)
	if output.Kind == "chat" {
		output.Intent = "chat"
	}
	if output.Kind == "command" && output.Intent == "unknown" {
		output.Kind = "clarify"
	}
	return output, result, nil
}
