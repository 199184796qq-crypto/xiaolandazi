package speechexpander

import (
	"strings"

	"livecompanion/management/internal/model"
)

const ContentStrategyVersion = "mainline-content-strategy/v1"

// ContentScheduleCursor is the bounded runtime layer carried across preview
// requests. It contains scheduling memory only; current business facts are
// always recompiled from the plan before the next unit is generated.
type ContentScheduleCursor struct {
	CompletedUnits int
	RecentFactIDs  []string
	RecentRoles    []string
}

// ContentStrategyInput is the five-layer input used by every mainline mode.
// LiveType and IndustryCode choose reusable defaults, while PlanGoal and
// ConversionIntensity are customer/plan choices. Cursor is session state.
type ContentStrategyInput struct {
	LiveType            string
	IndustryCode        string
	PlanGoal            string
	ConversionIntensity int
	ExpansionFreedom    int
	ProductLinks        []model.LiveAgentPlanProductLink
	Cursor              ContentScheduleCursor
}

type ProductTransition struct {
	TargetLinkKey string `json:"target_link_key"`
	Reason        string `json:"reason"`
}

// ProductOperatingDirection is a generated plan direction, not a product fact.
// RoomRoles are stable product-card metadata; emphasis, revisit and transitions
// are recalculated for each plan and may change without rewriting the card.
type ProductOperatingDirection struct {
	LinkKey        string              `json:"link_key"`
	ProductName    string              `json:"product_name,omitempty"`
	RoomRoles      []string            `json:"room_roles"`
	Emphasis       string              `json:"emphasis"`
	Revisit        string              `json:"revisit"`
	PrimaryAngles  []string            `json:"primary_angles,omitempty"`
	Transitions    []ProductTransition `json:"transitions,omitempty"`
	PositionSource string              `json:"position_source"`
	Reason         string              `json:"reason"`
}

type ContentStrategyLayer struct {
	Layer   string   `json:"layer"`
	Source  string   `json:"source"`
	Effects []string `json:"effects"`
}

type ResolvedContentStrategy struct {
	Version             string                      `json:"version"`
	LiveType            string                      `json:"live_type"`
	IndustryCode        string                      `json:"industry_code"`
	PlanGoal            string                      `json:"plan_goal,omitempty"`
	ConversionIntensity int                         `json:"conversion_intensity"`
	ExpansionFreedom    int                         `json:"expansion_freedom"`
	RoleSequence        []string                    `json:"role_sequence"`
	SchedulingMode      string                      `json:"scheduling_mode,omitempty"`
	ActualSteps         []ContentDecisionReceipt    `json:"actual_steps,omitempty"`
	ProductPlan         []ProductOperatingDirection `json:"product_plan,omitempty"`
	Layers              []ContentStrategyLayer      `json:"layers"`
}

type contentRole struct {
	key  string
	goal string
}

var contentRoleCatalog = map[string]contentRole{
	"orient":       {"orient", "让观众知道当前主题，用商品或内容本身接住主线，不默认从配送、售后开场"},
	"value":        {"value", "围绕当前重点说明为什么值得继续听；可以解释，不要只换同义词"},
	"scenario":     {"scenario", "用假设使用场景或选择情境展开；不要把通用知识推成新的商品效果或真实顾客事件"},
	"difference":   {"difference", "换一个判断角度解释差异；没有依据时不虚构竞品、优势或测评结论"},
	"evidence":     {"evidence", "用来源、材料、工艺或已有证据支撑重点，不从年限或产地推导未证实的品质结论"},
	"decision":     {"decision", "帮助观众判断规格、组合、适用情境或已确认保障，不替观众虚构需求"},
	"action":       {"action", "有正式链接、活动或行动依据时自然提出下一步；不制造库存、销量或倒计时"},
	"recap_bridge": {"recap_bridge", "简短回顾当前重点并自然接到下一角度；不把小段当成整场结束"},
}

func clampContentValue(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func containsContentWord(text string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func normalizedLiveType(value string, manifest []model.LiveAgentGenerationFact) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "" {
		return value
	}
	for _, fact := range manifest {
		if fact.SourceKind == "product" || fact.SourceKind == "benefit" {
			return "commerce"
		}
	}
	return "conversation"
}

func normalizedIndustry(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch {
	case containsContentWord(lower, "服装", "鞋服", "fashion", "apparel", "clothing"):
		return "apparel"
	case containsContentWord(lower, "食品", "食用", "food", "grocery", "生鲜"):
		return "food"
	case containsContentWord(lower, "家电", "数码", "3c", "appliance", "electronics"):
		return "durable_goods"
	case lower == "":
		return "general"
	default:
		return lower
	}
}

func hasRoomRole(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}

func productPrimaryAngles(linkKey string, manifest []model.LiveAgentGenerationFact) []string {
	labels := map[string]string{
		"content": "商品特点", "scenario": "使用场景", "choice": "规格与选择",
		"evidence": "材料与依据", "benefit": "有效福利", "assurance": "保障说明", "fulfillment": "履约说明",
	}
	seen := map[string]bool{}
	result := []string{}
	for _, fact := range usableGenerationFacts(manifest) {
		if strings.TrimSpace(linkKey) == "" || strings.TrimSpace(fact.LinkKey) != strings.TrimSpace(linkKey) {
			continue
		}
		family := contentFactFamily(fact)
		label := labels[family]
		if label != "" && !seen[label] {
			seen[label] = true
			result = append(result, label)
		}
	}
	return result
}

func resolveProductOperatingPlan(products []model.LiveAgentPlanProductLink, manifest []model.LiveAgentGenerationFact) []ProductOperatingDirection {
	mainLink := ""
	for _, product := range products {
		if hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleMain) {
			mainLink = product.LinkKey
			break
		}
	}
	result := make([]ProductOperatingDirection, 0, len(products))
	for _, product := range products {
		if !strings.EqualFold(strings.TrimSpace(product.Status), "active") {
			continue
		}
		item := ProductOperatingDirection{
			LinkKey: product.LinkKey, ProductName: product.ProductName,
			RoomRoles: append([]string(nil), product.RoomRoles...), Emphasis: "normal", Revisit: "adaptive",
			PrimaryAngles: productPrimaryAngles(product.LinkKey, manifest), PositionSource: "room_position",
			Reason: "根据商品卡的直播间定位生成当次讲解方向，具体顺序仍逐段动态判断",
		}
		switch {
		case hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleMain):
			item.Emphasis, item.Revisit = "high", "frequent"
			item.Reason = "主推定位提高讲解深度与返场优先级，但不固定开场或环节顺序"
		case hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleTraffic):
			item.Emphasis, item.Revisit = "normal", "frequent_short"
			item.Reason = "引流定位适合短切吸引与返场，不长期占满主线"
		case hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleBenefit):
			item.Emphasis, item.Revisit = "light", "timely"
			item.Reason = "福利定位优先提醒已确认活动条件，不把定位本身说成免费或亏本"
		case hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleBundle):
			item.Emphasis, item.Revisit = "light", "related"
			item.Reason = "搭配定位在相关场景出现，不强行独立反复讲解"
		case hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleProfit):
			item.Reason = "利润定位只作为内部经营方向，不自动增加促单或生成利润话术"
		case len(product.RoomRoles) == 0:
			item.PositionSource = "model_dynamic"
			item.Reason = "商品卡尚未设置直播间定位，本次由模型根据已确认属性动态安排，不回写长期定位"
		}
		if mainLink != "" && product.LinkKey != mainLink && (hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleTraffic) || hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleBenefit) || hasRoomRole(product.RoomRoles, model.LiveRoomProductRoleBundle)) {
			item.Transitions = append(item.Transitions, ProductTransition{TargetLinkKey: mainLink, Reason: "完成当前商品作用后，可按观众需求自然承接主推商品"})
		}
		result = append(result, item)
	}
	return result
}

func productPlanWeight(linkKey, contentRole string, plan []ProductOperatingDirection) int {
	for _, item := range plan {
		if item.LinkKey != linkKey {
			continue
		}
		weight := 0
		if hasRoomRole(item.RoomRoles, model.LiveRoomProductRoleMain) {
			weight += 90
		}
		if hasRoomRole(item.RoomRoles, model.LiveRoomProductRoleTraffic) && (contentRole == "orient" || contentRole == "recap_bridge") {
			weight += 65
		}
		if hasRoomRole(item.RoomRoles, model.LiveRoomProductRoleBenefit) && (contentRole == "action" || contentRole == "decision") {
			weight += 80
		}
		if hasRoomRole(item.RoomRoles, model.LiveRoomProductRoleBundle) && (contentRole == "scenario" || contentRole == "decision") {
			weight += 45
		}
		return weight
	}
	return 0
}

// ResolveContentStrategy makes the discussed layering executable and
// inspectable. The universal runtime stays fixed; type/industry are reusable
// packages; plan controls conversion and freedom; cursor supplies run state.
func ResolveContentStrategy(input ContentStrategyInput, manifest []model.LiveAgentGenerationFact) ResolvedContentStrategy {
	liveType := normalizedLiveType(input.LiveType, manifest)
	industry := normalizedIndustry(input.IndustryCode)
	// Zero is a deliberate user choice (for example, a no-pressure test), not
	// an omitted value. API adapters apply their own defaults before entering
	// the scheduler so this layer can preserve the full 0-100 range.
	intensity := clampContentValue(input.ConversionIntensity)
	freedom := clampContentValue(input.ExpansionFreedom)
	sequence := []string{"orient", "value", "difference", "evidence", "scenario", "decision", "recap_bridge", "action"}
	typeEffects := []string{"通用内容目的按时间轮换"}
	switch liveType {
	case "commerce", "ecommerce":
		sequence = []string{"orient", "value", "scenario", "evidence", "difference", "decision", "recap_bridge", "action"}
		typeEffects = []string{"启用价值、证据、场景、选择和转化目的", "行动目的只能使用当前有效事实"}
	case "education":
		sequence = []string{"orient", "value", "evidence", "scenario", "difference", "recap_bridge"}
		typeEffects = []string{"以解释、示例和确认代替商品促单"}
	case "performance":
		sequence = []string{"orient", "value", "scenario", "recap_bridge"}
		typeEffects = []string{"弱化商品选择和促单，保留参与与关注引导"}
	case "conversation", "chat":
		sequence = []string{"orient", "value", "scenario", "difference", "recap_bridge"}
		typeEffects = []string{"以主题、观点、场景和转场维持主线"}
	}
	industryEffects := []string{"使用通用行业包"}
	if liveType == "commerce" || liveType == "ecommerce" {
		switch industry {
		case "apparel":
			sequence = []string{"orient", "scenario", "value", "decision", "evidence", "difference", "recap_bridge", "action"}
			industryEffects = []string{"提高穿着场景和尺码/颜色选择的优先级", "材质只承担有依据的证据作用"}
		case "food":
			sequence = []string{"orient", "value", "evidence", "scenario", "difference", "decision", "recap_bridge", "action"}
			industryEffects = []string{"提高原料、工艺、来源和食用场景的优先级", "功效仍必须有正式依据"}
		case "durable_goods":
			sequence = []string{"orient", "value", "difference", "evidence", "scenario", "decision", "recap_bridge", "action"}
			industryEffects = []string{"提高参数证据、差异和选择判断的优先级"}
		}
	}
	// Conversion intensity changes the density of the action purpose. It never
	// relaxes fact or policy gates and is deliberately separate from freedom.
	if liveType == "commerce" || liveType == "ecommerce" {
		switch {
		case intensity >= 75:
			sequence = insertAfterEvery(sequence, "action", 3)
		case intensity >= 50:
			sequence = insertAfterEvery(sequence, "action", 5)
		case intensity < 25:
			sequence = removeRole(sequence, "action")
			sequence = append(sequence, "action")
		}
	}
	return ResolvedContentStrategy{
		Version: ContentStrategyVersion, LiveType: liveType, IndustryCode: industry,
		PlanGoal: strings.TrimSpace(input.PlanGoal), ConversionIntensity: intensity, ExpansionFreedom: freedom,
		RoleSequence: append([]string(nil), sequence...), ProductPlan: resolveProductOperatingPlan(input.ProductLinks, manifest),
		Layers: []ContentStrategyLayer{
			{Layer: "universal_runtime", Source: "system", Effects: []string{"时间小段推进", "渐变记忆与重复冷却", "事实与表达分离"}},
			{Layer: "live_type", Source: liveType, Effects: typeEffects},
			{Layer: "industry_package", Source: industry, Effects: industryEffects},
			{Layer: "plan", Source: "current_plan", Effects: []string{"成交推进力度与内容扩展授权由用户配置", "主题作为本轮方向而不是新增事实"}},
			{Layer: "runtime", Source: "continuation", Effects: []string{"最近内容目的和事实跨请求续接", "请求边界不重新开场或收尾"}},
		},
	}
}

func insertAfterEvery(values []string, role string, every int) []string {
	if every < 1 {
		return values
	}
	result := make([]string, 0, len(values)+len(values)/every)
	for _, value := range values {
		result = append(result, value)
		if value != role && len(result)%every == 0 {
			result = append(result, role)
		}
	}
	return result
}

func removeRole(values []string, role string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != role {
			result = append(result, value)
		}
	}
	return result
}

// Metadata describes a fact family without binding the scheduler to a product
// category. Unknown attributes remain general content.
func contentFactFamily(fact model.LiveAgentGenerationFact) string {
	metadata := strings.ToLower(strings.Join([]string{fact.SourceKey, fact.Predicate, fact.Label}, " "))
	switch {
	case containsContentWord(metadata, "shipping", "logistics", "delivery", "fulfillment", "快递", "物流", "发货", "配送", "运送"):
		return "fulfillment"
	case containsContentWord(metadata, "after_sale", "refund", "return", "售后", "退款", "退货", "试吃"):
		return "assurance"
	case fact.SourceKind == "benefit":
		return "benefit"
	case containsContentWord(metadata, "origin", "proof", "material", "process", "产地", "背书", "来源", "工艺", "原料", "面料", "材质", "检测", "资质"):
		return "evidence"
	case containsContentWord(metadata, "spec", "quantity", "price", "size", "color", "规格", "尺码", "颜色", "数量", "组合", "价格", "日常价"):
		return "choice"
	case containsContentWord(metadata, "audience", "scenario", "适用", "场景", "穿搭"):
		return "scenario"
	case fact.Predicate == "product_name":
		return "identity"
	default:
		return "content"
	}
}

func contentRoleFit(role, family string) int {
	switch family {
	case "fulfillment":
		if role == "action" || role == "decision" {
			return 4
		}
		return 0
	case "assurance":
		if role == "decision" || role == "action" {
			return 5
		}
		return 0
	case "benefit":
		if role == "action" || role == "decision" {
			return 6
		}
		return 0
	case "evidence":
		if role == "evidence" || role == "difference" {
			return 6
		}
	case "choice":
		if role == "decision" || role == "action" {
			return 6
		}
	case "scenario":
		if role == "scenario" || role == "value" {
			return 6
		}
	case "identity":
		if role == "orient" || role == "value" || role == "recap_bridge" {
			return 6
		}
	case "content":
		if role == "value" || role == "difference" || role == "scenario" || role == "recap_bridge" {
			return 5
		}
	}
	return 1
}

func usableGenerationFacts(manifest []model.LiveAgentGenerationFact) []model.LiveAgentGenerationFact {
	result := make([]model.LiveAgentGenerationFact, 0, len(manifest))
	seen := map[string]bool{}
	for _, fact := range manifest {
		value := strings.TrimSpace(fact.Value)
		if !fact.CanGenerate || fact.FactID == "" || seen[fact.FactID] || value == "" || value == "—" || value == "-" || value == "未设置" {
			continue
		}
		seen[fact.FactID] = true
		result = append(result, fact)
	}
	return result
}

func hasActionMaterial(facts []model.LiveAgentGenerationFact) bool {
	for _, fact := range facts {
		family := contentFactFamily(fact)
		if family == "choice" || family == "benefit" || family == "assurance" || fact.Predicate == "product_name" {
			return true
		}
	}
	return false
}

func recentDistance(values []string, value string) int {
	for index := len(values) - 1; index >= 0; index-- {
		if values[index] == value {
			return len(values) - index
		}
	}
	return -1
}

func addFactRouting(step *model.LiveSpeechExpansionStep, fact model.LiveAgentGenerationFact, primary bool) {
	if primary {
		step.PrimaryFactID = fact.FactID
	} else {
		step.SupportFactIDs = append(step.SupportFactIDs, fact.FactID)
	}
	switch fact.SourceKind {
	case "supplemental_fact":
		step.FactKeys = appendUnique(step.FactKeys, fact.SourceKey)
	case "benefit":
		step.BenefitKeys = appendUnique(step.BenefitKeys, fact.SourceKey)
	case "product":
		step.LinkKeys = appendUnique(step.LinkKeys, fact.LinkKey)
	}
	if fact.SourceKind == "benefit" && fact.LinkKey != "" {
		step.LinkKeys = appendUnique(step.LinkKeys, fact.LinkKey)
	}
}

func compatibleSupport(primary, support model.LiveAgentGenerationFact, role string) bool {
	if support.FactID == primary.FactID || contentRoleFit(role, contentFactFamily(support)) <= 1 {
		return false
	}
	if primary.LinkKey != "" && support.LinkKey != "" && primary.LinkKey != support.LinkKey {
		return false
	}
	return contentFactFamily(support) != "fulfillment" || role == "action" || role == "decision"
}

// ScheduleContent is purpose-driven: it first resolves a content role, then
// chooses one primary fact and at most one compatible supporting fact. It never
// starts from database row order and never treats a request boundary as a new
// show. The returned strategy is included in preview diagnostics.
func ScheduleContent(plans []model.LiveSpeechExpansionPlan, manifest []model.LiveAgentGenerationFact, input ContentStrategyInput) ([]model.LiveSpeechExpansionPlan, ResolvedContentStrategy) {
	facts := usableGenerationFacts(manifest)
	strategy := ResolveContentStrategy(input, facts)
	if len(strategy.RoleSequence) == 0 {
		strategy.RoleSequence = []string{"orient", "value", "recap_bridge"}
	}
	for planIndex := range plans {
		plan := &plans[planIndex]
		recentFacts := append([]string(nil), input.Cursor.RecentFactIDs...)
		recentRoles := append([]string(nil), input.Cursor.RecentRoles...)
		for index := range plan.Steps {
			step := &plan.Steps[index]
			position := input.Cursor.CompletedUnits + index
			roleKey := strategy.RoleSequence[(position+planIndex)%len(strategy.RoleSequence)]
			if roleKey == "action" && !hasActionMaterial(facts) {
				roleKey = "value"
			}
			if len(recentRoles) > 0 && recentRoles[len(recentRoles)-1] == roleKey && len(strategy.RoleSequence) > 1 {
				roleKey = strategy.RoleSequence[(position+planIndex+1)%len(strategy.RoleSequence)]
			}
			role := contentRoleCatalog[roleKey]
			step.ContentRole, step.Stage, step.Goal = role.key, role.key, role.goal
			step.FactKeys, step.BenefitKeys, step.LinkKeys = nil, nil, nil
			step.PrimaryFactID, step.SupportFactIDs = "", nil
			move := (position + planIndex) % len(expressionMoves)
			step.ExpressionMoves = []string{expressionMoves[move], expressionMoves[(move+2)%len(expressionMoves)]}
			bestIndex, bestScore := -1, -1<<30
			for factIndex, fact := range facts {
				fit := contentRoleFit(role.key, contentFactFamily(fact))
				if fit == 0 {
					continue
				}
				score := fit*30 + productPlanWeight(fact.LinkKey, role.key, strategy.ProductPlan) + ((position + factIndex*7) % 17)
				distance := recentDistance(recentFacts, fact.FactID)
				if distance < 0 {
					score += 220
				} else {
					score += distance * 10
					if distance <= 3 {
						score -= 280
					}
				}
				if score > bestScore {
					bestIndex, bestScore = factIndex, score
				}
			}
			if bestIndex < 0 {
				step.Goal += "；当前没有匹配的正式事实，只在用户扩展授权内做无商品承诺的自然承接"
				recentRoles = append(recentRoles, role.key)
				continue
			}
			primary := facts[bestIndex]
			addFactRouting(step, primary, true)
			supportIndex, supportScore := -1, -1<<30
			for factIndex, fact := range facts {
				if !compatibleSupport(primary, fact, role.key) {
					continue
				}
				score := contentRoleFit(role.key, contentFactFamily(fact)) * 20
				if primary.LinkKey != "" && fact.LinkKey == primary.LinkKey {
					score += 30
				}
				distance := recentDistance(recentFacts, fact.FactID)
				if distance < 0 {
					score += 80
				} else if distance <= 3 {
					score -= 160
				}
				if score > supportScore {
					supportIndex, supportScore = factIndex, score
				}
			}
			if supportIndex >= 0 && supportScore >= 60 {
				addFactRouting(step, facts[supportIndex], false)
			}
			recentFacts = append(recentFacts, primary.FactID)
			recentRoles = append(recentRoles, role.key)
			if len(recentFacts) > 16 {
				recentFacts = recentFacts[len(recentFacts)-16:]
			}
			if len(recentRoles) > 16 {
				recentRoles = recentRoles[len(recentRoles)-16:]
			}
		}
	}
	return plans, strategy
}
