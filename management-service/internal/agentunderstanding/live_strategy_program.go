package agentunderstanding

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type LiveStrategyProduct struct {
	LinkKey     string
	ProductName string
	RoomRoles   []string
	Spec        string
	DailyPrice  string
	Quantity    string
	Audience    string
	Attributes  []LiveStrategyProductAttribute
}

type LiveStrategyProductAttribute struct {
	ID    int64
	Code  string
	Label string
	Value string
	Unit  string
}

type LiveStrategyBenefit struct {
	Key           string
	LinkKey       string
	ProductName   string
	ActivityPrice string
	Gift          string
	Activity      string
	StartsAt      string
	EndsAt        string
}

type LiveStrategyFact struct {
	Category string
	Key      string
	Value    string
}

type LiveStrategyScriptReference struct {
	ReferenceKey string
	Title        string
	ContentText  string
}

type LiveStrategyPlan struct {
	ID       int64
	Name     string
	Bound    bool
	Selected bool
}

type LiveStrategyProgramContext struct {
	CurrentMode      string
	Products         []LiveStrategyProduct
	Benefits         []LiveStrategyBenefit
	Facts            []LiveStrategyFact
	ScriptReferences []LiveStrategyScriptReference
	Plans            []LiveStrategyPlan
	HasImages        bool
	Now              time.Time
}

var liveStrategyLinkPattern = regexp.MustCompile(`([0-9]+)\s*号?\s*(商品)?\s*链接|链接\s*([0-9]+)\s*号?`)

func ProgramInterpretLiveStrategy(message string, ctx LiveStrategyProgramContext) UnifiedIntent {
	message = strings.TrimSpace(message)
	base := UnifiedIntent{
		ProtocolVersion: ProtocolVersion,
		Kind:            KindClarify,
		Intent:          "unknown",
		Confidence:      0.35,
		Engine:          "program",
		Target:          map[string]any{},
		Changes:         map[string]any{},
	}
	if message == "" {
		base.Reply = "请告诉我你想处理什么。"
		return base
	}
	compact := compactProgramText(message)
	if isCancelOnly(compact) {
		base.Kind = KindChat
		base.Intent = "chat"
		base.Confidence = 1
		base.Reply = "好，这次不执行修改。"
		return base
	}
	if ctx.HasImages {
		base.Missing = []string{"图片语义"}
		base.Reply = "当前使用程序理解，不能可靠读取图片语义。请用文字补充图片里的关键信息，或切换到大模型理解。"
		base.Confidence = 0.95
		return base
	}
	if out, ok := programAnswerLiveStrategyQuery(message, compact, ctx); ok {
		return out
	}
	if out, ok := programInterpretPlan(message, compact, ctx); ok {
		return out
	}
	if out, ok := programInterpretFact(message, compact, ctx); ok {
		return out
	}
	if out, ok := programInterpretScript(message, compact, ctx); ok {
		return out
	}
	if out, ok := programInterpretBenefit(message, compact, ctx); ok {
		return out
	}
	if out, ok := programInterpretProductAttribute(message, compact, ctx); ok {
		return out
	}
	if out, ok := programInterpretProduct(message, compact, ctx); ok {
		return out
	}

	// A mutation verb without a unique business object must never be guessed.
	if containsAnyProgram(compact, "添加", "新增", "修改", "调整", "更新", "删除", "停用", "解绑", "切换", "保存", "写入") {
		base.Reply = "我知道你想修改当前方案，但还不能唯一判断要处理商品链接、活动福利、事实依据、话术参考还是方案绑定。请把对象说清楚。"
		base.Missing = []string{"业务对象"}
		return base
	}
	base.Kind = KindChat
	base.Intent = "chat"
	base.Confidence = 0.55
	base.Reply = "这句话没有匹配到需要写入的业务动作。你可以继续聊天，或直接说明要新增、修改、停用哪一项。"
	return base
}

func extractProductAttribute(message string) (string, string) {
	marker := ""
	for _, candidate := range []string{"个性属性", "商品属性", "产品属性", "属性"} {
		if strings.Contains(message, candidate) {
			marker = candidate
			break
		}
	}
	if marker == "" {
		return "", ""
	}
	rest := strings.TrimSpace(message[strings.Index(message, marker)+len(marker):])
	rest = strings.TrimLeft(rest, " ：:，,、")
	separator := ""
	for _, candidate := range []string{"为", "=", ":", "："} {
		if index := strings.Index(rest, candidate); index > 0 {
			separator = candidate
			break
		}
	}
	if separator == "" {
		return strings.TrimSpace(strings.Trim(rest, "，,；;。")), ""
	}
	parts := strings.SplitN(rest, separator, 2)
	if len(parts) != 2 {
		return "", ""
	}
	label := strings.TrimSpace(strings.Trim(parts[0], "，,；;。"))
	value := strings.TrimSpace(strings.Trim(parts[1], "，,；;。"))
	return label, value
}

func programInterpretProductAttribute(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if !containsAnyProgram(compact, "个性属性", "商品属性", "产品属性") && !strings.Contains(compact, "属性") {
		return UnifiedIntent{}, false
	}
	action := mutationVerb(compact)
	if action == "" {
		return UnifiedIntent{}, false
	}
	linkKey := extractLinkKey(message)
	if linkKey == "" && len(ctx.Products) == 1 && ctx.CurrentMode == "products" {
		linkKey = ctx.Products[0].LinkKey
	}
	if linkKey == "" {
		out := programIntent(KindClarify, "product_attribute."+action, 0.65)
		out.Missing = []string{"商品链接"}
		out.Reply = "请说明要处理几号商品链接的个性属性。"
		return out, true
	}
	label, value := extractProductAttribute(message)
	if label == "" {
		out := programIntent(KindClarify, "product_attribute."+action, 0.72)
		out.Target["link_key"] = linkKey
		out.Missing = []string{"个性属性名称"}
		out.Reply = "请说明属性名称，例如“压榨工艺”或“面料”。"
		return out, true
	}
	out := programIntent(KindCommand, "product_attribute."+action, 0.96)
	out.Target["link_key"] = linkKey
	out.Target["attribute_label"] = label
	out.Changes["attribute_label"] = label
	if value != "" {
		out.Changes["attribute_value"] = value
	}
	if action != "disable" && value == "" {
		out.Kind = KindClarify
		out.Confidence = 0.74
		out.Missing = []string{"属性值"}
		out.Reply = "请说明“" + label + "”的属性值，例如“压榨工艺为传统熟榨”。"
	}
	return out, true
}

func programIntent(kind, intent string, confidence float64) UnifiedIntent {
	return UnifiedIntent{
		ProtocolVersion: ProtocolVersion,
		Kind:            kind,
		Intent:          intent,
		Confidence:      confidence,
		Engine:          "program",
		Target:          map[string]any{},
		Changes:         map[string]any{},
	}
}

func compactProgramText(value string) string {
	replacer := strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "",
		"，", "", ",", "", "。", "", ".", "", "！", "", "!", "", "？", "", "?", "",
	)
	return replacer.Replace(strings.TrimSpace(value))
}

func containsAnyProgram(value string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(value, word) {
			return true
		}
	}
	return false
}

func isCancelOnly(value string) bool {
	switch value {
	case "取消", "不用了", "不用", "算了", "先算了", "不弄了", "不改了", "先不改", "先不弄", "不要了", "停一下", "先这样", "到此为止":
		return true
	default:
		return false
	}
}

func looksLikeProgramQuestion(compact string) bool {
	return containsAnyProgram(
		compact,
		"什么", "多少", "哪家", "哪个", "哪里", "哪儿", "怎么", "怎样",
		"有没有", "有没", "是否", "是不是", "吗", "呢", "几号", "当前", "现在",
	)
}

func programAnswerLiveStrategyQuery(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if mutationVerb(compact) != "" || !looksLikeProgramQuestion(compact) {
		return UnifiedIntent{}, false
	}
	if out, ok := programAnswerFactQuery(message, compact, ctx); ok {
		return out, true
	}
	if out, ok := programAnswerBenefitQuery(message, compact, ctx); ok {
		return out, true
	}
	if out, ok := programAnswerProductQuery(message, compact, ctx); ok {
		return out, true
	}
	if out, ok := programAnswerPlanQuery(compact, ctx); ok {
		return out, true
	}
	return UnifiedIntent{}, false
}

func programAnswerFactQuery(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	topic := ""
	setTopic := func(name string, words ...string) {
		if topic == "" && containsAnyProgram(compact, words...) {
			topic = name
		}
	}
	setTopic("快递/物流", "快递", "物流")
	setTopic("发货", "发货地", "从哪里发", "从哪发", "哪里发货", "哪儿发货")
	setTopic("产地", "产地", "哪里产", "哪儿产", "哪产")
	setTopic("保质期", "保质期")
	setTopic("资质", "资质", "证书", "认证")
	setTopic("售后", "售后", "退换", "退款")
	setTopic("库存", "库存", "现货")

	type scoredFact struct {
		item  LiveStrategyFact
		score int
	}
	matches := make([]scoredFact, 0)
	best := 0
	for _, item := range ctx.Facts {
		score := 0
		key := strings.TrimSpace(item.Key)
		category := strings.TrimSpace(item.Category)
		if key != "" && strings.Contains(message, key) {
			score += 8
		}
		if category != "" && strings.Contains(message, category) {
			score += 5
		}
		switch topic {
		case "快递/物流":
			if containsAnyProgram(key+category, "快递", "物流") {
				score += 6
			}
		case "发货":
			if containsAnyProgram(key+category, "发货", "发出地", "仓库") {
				score += 6
			}
		case "产地":
			if containsAnyProgram(key+category, "产地", "原产地", "生产地") {
				score += 6
			}
		case "保质期":
			if strings.Contains(key+category, "保质期") {
				score += 6
			}
		case "资质":
			if containsAnyProgram(key+category, "资质", "证书", "认证") {
				score += 6
			}
		case "售后":
			if containsAnyProgram(key+category, "售后", "退换", "退款") {
				score += 6
			}
		case "库存":
			if containsAnyProgram(key+category, "库存", "现货") {
				score += 6
			}
		}
		if score <= 0 {
			continue
		}
		if score > best {
			best = score
			matches = matches[:0]
		}
		if score == best {
			matches = append(matches, scoredFact{item: item, score: score})
		}
	}
	if best == 0 {
		if topic == "" {
			return UnifiedIntent{}, false
		}
		out := programIntent(KindChat, "chat", 0.99)
		out.Reply = "当前方案里还没有已确认的" + topic + "事实，我不会猜。请先在“事实依据”里补充后再使用。"
		return out, true
	}
	parts := make([]string, 0, len(matches))
	for _, match := range matches {
		label := strings.TrimSpace(match.item.Key)
		if label == "" {
			label = strings.TrimSpace(match.item.Category)
		}
		if label == "" {
			label = "相关事实"
		}
		parts = append(parts, label+"："+strings.TrimSpace(match.item.Value))
	}
	out := programIntent(KindChat, "chat", 0.99)
	out.Reply = strings.Join(parts, "；") + "。"
	return out, true
}

func programAnswerProductQuery(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if !containsAnyProgram(compact, "商品", "链接", "规格", "日常价", "原价", "价格", "多少钱", "数量", "适用人群", "适用对象", "属性", "直播间定位", "商品定位") {
		return UnifiedIntent{}, false
	}
	linkKey := extractLinkKey(message)
	if linkKey == "" && ctx.CurrentMode == "products" && len(ctx.Products) == 1 {
		linkKey = ctx.Products[0].LinkKey
	}
	if linkKey == "" {
		return UnifiedIntent{}, false
	}
	var product *LiveStrategyProduct
	for i := range ctx.Products {
		if ctx.Products[i].LinkKey == linkKey {
			product = &ctx.Products[i]
			break
		}
	}
	if product == nil {
		out := programIntent(KindChat, "chat", 0.99)
		out.Reply = "当前方案里没有“" + linkKey + "”的正式商品资料，我不会猜。"
		return out, true
	}
	parts := make([]string, 0, 5)
	if containsAnyProgram(compact, "什么商品", "卖什么", "商品名", "商品名称") {
		parts = appendProgramAnswerPart(parts, "商品名称", product.ProductName)
	}
	if strings.Contains(compact, "规格") {
		parts = appendProgramAnswerPart(parts, "规格", product.Spec)
	}
	if containsAnyProgram(compact, "日常价", "原价", "价格", "多少钱") {
		parts = appendProgramAnswerPart(parts, "日常价", product.DailyPrice)
	}
	if strings.Contains(compact, "数量") {
		parts = appendProgramAnswerPart(parts, "数量", product.Quantity)
	}
	if containsAnyProgram(compact, "适用人群", "适用对象") {
		parts = appendProgramAnswerPart(parts, "适用人群", product.Audience)
	}
	if containsAnyProgram(compact, "直播间定位", "商品定位") {
		parts = appendProgramAnswerPart(parts, "直播间定位", liveRoomRoleLabels(product.RoomRoles))
	}
	if strings.Contains(compact, "属性") {
		for _, attribute := range product.Attributes {
			parts = appendProgramAnswerPart(parts, attribute.Label, attribute.Value+attribute.Unit)
		}
	}
	if strings.Contains(compact, "属性") && len(parts) == 0 {
		out := programIntent(KindChat, "chat", 0.99)
		out.Reply = linkKey + "当前还没有已确认的个性属性，我不会根据常识猜测。"
		return out, true
	}
	if len(parts) == 0 {
		parts = appendProgramAnswerPart(parts, "商品名称", product.ProductName)
		parts = appendProgramAnswerPart(parts, "规格", product.Spec)
		parts = appendProgramAnswerPart(parts, "日常价", product.DailyPrice)
	}
	out := programIntent(KindChat, "chat", 0.99)
	if len(parts) == 0 {
		out.Reply = "“" + linkKey + "”存在，但当前正式商品资料里没有你问的字段。"
	} else {
		out.Reply = linkKey + "：" + strings.Join(parts, "；") + "。"
	}
	return out, true
}

func liveRoomRoleLabels(values []string) string {
	labels := map[string]string{"main": "主推", "traffic": "引流", "benefit": "福利", "profit": "利润", "bundle": "搭配", "ordinary": "普通"}
	result := []string{}
	for _, value := range values {
		if label := labels[strings.ToLower(strings.TrimSpace(value))]; label != "" {
			result = append(result, label)
		}
	}
	return strings.Join(result, "、")
}

func extractLiveRoomRoles(message string, current []string) ([]string, bool) {
	roleWords := []struct{ label, value string }{
		{"主推", "main"}, {"引流", "traffic"}, {"福利", "benefit"}, {"利润", "profit"}, {"搭配", "bundle"}, {"普通", "ordinary"},
	}
	hasPosition := containsAnyProgram(message, "直播间定位", "商品定位", "定位", "主推款", "引流款", "福利款", "利润款", "搭配款", "普通款")
	if !hasPosition {
		return nil, false
	}
	remove := containsAnyProgram(message, "去掉", "移除", "取消", "不要", "删除")
	clear := containsAnyProgram(message, "清空定位", "不设置定位", "取消全部定位")
	if clear {
		return []string{}, true
	}
	mentioned := []string{}
	for _, item := range roleWords {
		if strings.Contains(message, item.label) {
			mentioned = append(mentioned, item.value)
		}
	}
	if len(mentioned) == 0 {
		return nil, true
	}
	if remove {
		removeSet := map[string]bool{}
		for _, role := range mentioned {
			removeSet[role] = true
		}
		result := []string{}
		for _, role := range current {
			if !removeSet[role] {
				result = append(result, role)
			}
		}
		return result, true
	}
	if len(mentioned) == 1 && mentioned[0] == "ordinary" {
		return mentioned, true
	}
	return mentioned, true
}

func programAnswerBenefitQuery(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if !containsAnyProgram(compact, "活动", "福利", "优惠", "赠品", "活动价", "秒杀价", "到手价", "什么时候结束", "截止时间") {
		return UnifiedIntent{}, false
	}
	linkKey := extractLinkKey(message)
	matches := make([]LiveStrategyBenefit, 0)
	for _, item := range ctx.Benefits {
		if linkKey == "" || item.LinkKey == linkKey {
			matches = append(matches, item)
		}
	}
	if linkKey == "" && ctx.CurrentMode != "benefits" {
		return UnifiedIntent{}, false
	}
	if len(matches) == 0 {
		out := programIntent(KindChat, "chat", 0.99)
		if linkKey != "" {
			out.Reply = "当前方案里没有找到“" + linkKey + "”的正式活动福利，我不会猜。"
		} else {
			out.Reply = "当前方案里还没有正式活动福利。"
		}
		return out, true
	}
	if len(matches) > 1 {
		out := programIntent(KindClarify, "chat", 0.92)
		out.Missing = []string{"活动福利目标"}
		out.Reply = "当前有多条活动福利，请再说明商品链接或活动名称。"
		return out, true
	}
	item := matches[0]
	parts := make([]string, 0, 5)
	if containsAnyProgram(compact, "活动价", "秒杀价", "到手价", "优惠价", "多少钱") {
		parts = appendProgramAnswerPart(parts, "活动价", item.ActivityPrice)
	}
	if containsAnyProgram(compact, "赠品", "送什么", "福利") {
		parts = appendProgramAnswerPart(parts, "赠品", item.Gift)
	}
	if containsAnyProgram(compact, "活动内容", "活动规则", "什么活动", "什么优惠") {
		parts = appendProgramAnswerPart(parts, "活动", item.Activity)
	}
	if containsAnyProgram(compact, "开始", "什么时候开始", "生效时间") {
		parts = appendProgramAnswerPart(parts, "开始时间", item.StartsAt)
	}
	if containsAnyProgram(compact, "结束", "截止", "什么时候结束", "失效时间") {
		parts = appendProgramAnswerPart(parts, "结束时间", item.EndsAt)
	}
	if len(parts) == 0 {
		parts = appendProgramAnswerPart(parts, "活动价", item.ActivityPrice)
		parts = appendProgramAnswerPart(parts, "赠品", item.Gift)
		parts = appendProgramAnswerPart(parts, "活动", item.Activity)
		parts = appendProgramAnswerPart(parts, "开始时间", item.StartsAt)
		parts = appendProgramAnswerPart(parts, "结束时间", item.EndsAt)
	}
	out := programIntent(KindChat, "chat", 0.99)
	label := strings.TrimSpace(item.LinkKey)
	if label == "" {
		label = strings.TrimSpace(item.ProductName)
	}
	if label == "" {
		label = "当前活动"
	}
	if len(parts) == 0 {
		out.Reply = label + "有正式活动记录，但当前记录里没有你问的字段。"
	} else {
		out.Reply = label + "：" + strings.Join(parts, "；") + "。"
	}
	return out, true
}

func programAnswerPlanQuery(compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if !containsAnyProgram(compact, "当前方案", "现在用哪个方案", "现在什么方案", "运行哪个方案", "使用哪个方案") {
		return UnifiedIntent{}, false
	}
	for _, item := range ctx.Plans {
		if item.Selected {
			out := programIntent(KindChat, "chat", 0.99)
			out.Reply = "当前直播间正在使用方案“" + item.Name + "”。"
			return out, true
		}
	}
	out := programIntent(KindChat, "chat", 0.99)
	out.Reply = "当前直播间还没有选中的运行方案。"
	return out, true
}

func appendProgramAnswerPart(parts []string, label, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return parts
	}
	return append(parts, label+"："+value)
}

func mutationVerb(compact string) string {
	switch {
	case containsAnyProgram(compact, "删除", "移除", "停用", "下掉"):
		return "disable"
	case containsAnyProgram(compact, "修改", "调整", "更新", "改成", "改为", "设为", "设置为", "变更"):
		return "update"
	case containsAnyProgram(compact, "添加", "新增", "增加", "新建", "录入", "写入", "保存"):
		return "add"
	default:
		return ""
	}
}

func extractLinkKey(value string) string {
	match := liveStrategyLinkPattern.FindStringSubmatch(value)
	if len(match) == 0 {
		return ""
	}
	number := ""
	if len(match) > 1 {
		number = match[1]
	}
	if number == "" && len(match) > 3 {
		number = match[3]
	}
	if number == "" {
		return ""
	}
	return number + "号链接"
}

func extractField(value string, labels ...string) string {
	for _, label := range labels {
		index := strings.Index(value, label)
		if index < 0 {
			continue
		}
		rest := strings.TrimSpace(value[index+len(label):])
		for _, prefix := range []string{"修改为", "调整为", "设置为", "改成", "改为", "设为", "：", ":"} {
			if strings.HasPrefix(rest, prefix) {
				rest = strings.TrimSpace(strings.TrimPrefix(rest, prefix))
				break
			}
		}
		if rest == "" {
			continue
		}
		if cut := strings.IndexAny(rest, "，,；;\n\r"); cut >= 0 {
			rest = rest[:cut]
		}
		return strings.TrimSpace(rest)
	}
	return ""
}

func extractBenefitProductNameChange(message string, matching []LiveStrategyBenefit) string {
	if value := extractField(message, "商品名称", "商品名", "福利名称", "福利标题", "活动名称", "活动标题"); value != "" {
		return value
	}
	for _, item := range matching {
		oldName := strings.TrimSpace(item.ProductName)
		if oldName == "" {
			continue
		}
		index := strings.Index(message, oldName)
		if index < 0 {
			continue
		}
		rest := strings.TrimSpace(message[index+len(oldName):])
		for _, middle := range []string{"的商品名称", "的福利名称", "的福利标题", "的活动名称", "的活动标题", "商品名称", "福利名称", "福利标题", "活动名称", "活动标题", "的福利", "的活动"} {
			if strings.HasPrefix(rest, middle) {
				rest = strings.TrimSpace(strings.TrimPrefix(rest, middle))
				break
			}
		}
		for _, prefix := range []string{"修改为", "调整为", "设置为", "改成", "改为", "换成", "更名为", "设为"} {
			if strings.HasPrefix(rest, prefix) {
				rest = strings.TrimSpace(strings.TrimPrefix(rest, prefix))
				break
			}
		}
		if rest == "" || rest == strings.TrimSpace(message[index+len(oldName):]) {
			continue
		}
		if cut := strings.IndexAny(rest, "，,；;。！？!?\n\r"); cut >= 0 {
			rest = rest[:cut]
		}
		if rest != "" {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func normalizeProgramTime(value string, now time.Time) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if now.IsZero() {
		now = time.Now()
	}
	switch value {
	case "现在", "立即", "马上", "即刻", "当前时间", "此刻", "立即生效", "马上生效":
		return now.Format("2006-01-02 15:04")
	}
	re := regexp.MustCompile(`[0-9]{4}-[0-9]{1,2}-[0-9]{1,2}([ T][0-9]{1,2}:[0-9]{2})?`)
	matched := re.FindString(value)
	if matched == "" {
		return ""
	}
	return strings.ReplaceAll(matched, "T", " ")
}

func programInterpretProduct(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	linkKey := extractLinkKey(message)
	productField := containsAnyProgram(compact, "商品名称", "商品名", "规格", "日常价", "原价", "数量", "适用人群", "适用对象", "直播间定位", "商品定位", "主推款", "引流款", "福利款", "利润款", "搭配款", "普通款")
	explicitProduct := strings.Contains(compact, "商品链接") || (strings.Contains(compact, "链接") && productField)
	if !explicitProduct && ctx.CurrentMode != "products" {
		return UnifiedIntent{}, false
	}
	action := mutationVerb(compact)
	roomPositionField := containsAnyProgram(compact, "直播间定位", "商品定位", "主推款", "引流款", "福利款", "利润款", "搭配款", "普通款") || (strings.Contains(compact, "定位") && containsAnyProgram(compact, "主推", "引流", "福利", "利润", "搭配", "普通"))
	if roomPositionField && (action == "" || action == "disable") {
		action = "update"
	}
	if action == "" && !productField {
		return UnifiedIntent{}, false
	}
	if action == "" {
		action = "update"
	}
	if linkKey == "" && len(ctx.Products) == 1 && ctx.CurrentMode == "products" {
		linkKey = ctx.Products[0].LinkKey
	}
	var currentRoles []string
	for _, product := range ctx.Products {
		if product.LinkKey == linkKey {
			currentRoles = product.RoomRoles
			break
		}
	}
	if linkKey == "" && action != "add" {
		out := programIntent(KindClarify, "product."+action, 0.55)
		out.Missing = []string{"商品链接"}
		out.Reply = "请说明要处理几号商品链接。"
		return out, true
	}

	changes := map[string]any{}
	if value := extractField(message, "商品名称", "商品名"); value != "" {
		changes["product_name"] = value
	}
	if value := extractField(message, "规格"); value != "" {
		changes["spec"] = value
	}
	if value := extractField(message, "日常价", "原价"); value != "" {
		changes["daily_price"] = value
	}
	if value := extractField(message, "数量"); value != "" {
		changes["quantity"] = value
	}
	if value := extractField(message, "适用人群", "适用对象"); value != "" {
		changes["audience"] = value
	}
	if roles, mentioned := extractLiveRoomRoles(message, currentRoles); mentioned {
		changes["room_roles"] = roles
	}
	if action == "update" && len(changes) == 0 {
		out := programIntent(KindClarify, "product.update", 0.82)
		out.Target["link_key"] = linkKey
		out.Missing = []string{"修改字段"}
		out.Reply = "你要修改“" + linkKey + "”，请说明是商品名称、规格、日常价、数量、适用人群还是直播间定位。"
		return out, true
	}
	out := programIntent(KindCommand, "product."+action, 0.98)
	if linkKey != "" {
		out.Target["link_key"] = linkKey
	}
	out.Changes = changes
	return out, true
}

func programInterpretBenefit(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	linkKey := extractLinkKey(message)
	matching := make([]LiveStrategyBenefit, 0)
	for _, item := range ctx.Benefits {
		if linkKey == "" || item.LinkKey == linkKey {
			matching = append(matching, item)
		}
	}
	if linkKey == "" && len(ctx.Benefits) == 1 && ctx.CurrentMode == "benefits" {
		linkKey = ctx.Benefits[0].LinkKey
		matching = ctx.Benefits[:1]
	}
	productNameChange := extractBenefitProductNameChange(message, matching)
	hasBenefit := containsAnyProgram(compact, "活动", "福利", "优惠", "赠品", "满减", "秒杀", "折扣", "优惠券")
	hasField := productNameChange != "" || containsAnyProgram(compact, "商品名称", "商品名", "福利名称", "福利标题", "活动名称", "活动标题", "活动价", "优惠价", "秒杀价", "到手价", "赠品", "活动内容", "活动规则", "开始时间", "生效时间", "结束时间", "失效时间", "截止时间")
	if !hasBenefit && !hasField && ctx.CurrentMode != "benefits" {
		return UnifiedIntent{}, false
	}
	// "修改1号链接" by itself is ambiguous and must not be stolen by benefit context.
	if !hasBenefit && !hasField && linkKey != "" {
		return UnifiedIntent{}, false
	}
	action := mutationVerb(compact)
	if action == "" && hasField {
		action = "update"
	}
	if action == "" {
		return UnifiedIntent{}, false
	}
	changes := map[string]any{}
	if productNameChange != "" {
		changes["product_name"] = productNameChange
	}
	if value := extractField(message, "活动价", "优惠价", "秒杀价", "到手价"); value != "" {
		changes["activity_price"] = value
	}
	if value := extractField(message, "赠品内容", "赠品"); value != "" {
		changes["gift"] = value
	}
	if value := extractField(message, "活动内容", "活动规则"); value != "" {
		changes["activity"] = value
	}
	if raw := extractField(message, "开始时间", "生效时间"); raw != "" {
		if value := normalizeProgramTime(raw, ctx.Now); value != "" {
			changes["starts_at"] = value
		} else {
			out := programIntent(KindClarify, "benefit."+action, 0.75)
			out.Target["link_key"] = linkKey
			out.Missing = []string{"开始时间"}
			out.Reply = "开始时间我没法可靠解析，请用“现在”或 YYYY-MM-DD HH:mm。"
			return out, true
		}
	} else if containsAnyProgram(compact, "现在开始", "立即生效", "马上生效", "从现在开始") {
		changes["starts_at"] = normalizeProgramTime("现在", ctx.Now)
	}
	if raw := extractField(message, "结束时间", "失效时间", "截止时间"); raw != "" {
		if value := normalizeProgramTime(raw, ctx.Now); value != "" {
			changes["ends_at"] = value
		} else {
			out := programIntent(KindClarify, "benefit."+action, 0.75)
			out.Target["link_key"] = linkKey
			out.Missing = []string{"结束时间"}
			out.Reply = "结束时间我没法可靠解析，请用 YYYY-MM-DD HH:mm。"
			return out, true
		}
	}

	if action == "update" || action == "disable" {
		if len(matching) == 1 {
			if matching[0].Key != "" {
				// benefit_key lets downstream uniquely re-check the formal object.
			}
		} else if len(matching) == 0 {
			out := programIntent(KindClarify, "benefit."+action, 0.72)
			out.Target["link_key"] = linkKey
			out.Missing = []string{"活动福利目标"}
			out.Reply = "当前正式活动里没有找到唯一对应项，请说明活动或链接。"
			return out, true
		} else {
			out := programIntent(KindClarify, "benefit."+action, 0.68)
			out.Target["link_key"] = linkKey
			out.Missing = []string{"活动福利目标"}
			out.Reply = "这个链接下有多条活动，请再说明要修改哪一条。"
			return out, true
		}
	}
	if action == "update" && len(changes) == 0 {
		out := programIntent(KindClarify, "benefit.update", 0.9)
		out.Target["link_key"] = linkKey
		if len(matching) == 1 {
			out.Target["benefit_key"] = matching[0].Key
		}
		out.Missing = []string{"修改字段"}
		out.Reply = "请说明要修改商品名称、活动价、赠品、活动内容、开始时间还是结束时间。"
		return out, true
	}
	out := programIntent(KindCommand, "benefit."+action, 0.99)
	if linkKey != "" {
		out.Target["link_key"] = linkKey
	}
	if len(matching) == 1 {
		out.Target["benefit_key"] = matching[0].Key
	}
	out.Changes = changes
	return out, true
}

func programInterpretFact(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	explicit := strings.Contains(compact, "事实") || containsAnyProgram(
		compact,
		"快递方式", "快递", "发货物流", "物流", "发货地", "产地",
		"产品卖点", "交易售后", "售后", "保质期", "资质", "库存", "现货",
	)
	if !explicit && ctx.CurrentMode != "knowledge" {
		return UnifiedIntent{}, false
	}
	action := mutationVerb(compact)
	if action == "" && !strings.Contains(compact, "事实") {
		return UnifiedIntent{}, false
	}
	if action == "" {
		action = "add"
	}
	category, key, value := parseFactTriple(message)
	if key == "" && action != "add" {
		matches := make([]LiveStrategyFact, 0)
		for _, item := range ctx.Facts {
			if strings.Contains(message, item.Key) ||
				(item.Category != "" && strings.Contains(message, item.Category)) ||
				factMatchesNaturalTopic(compact, item) {
				matches = append(matches, item)
			}
		}
		if len(matches) == 1 {
			category, key = matches[0].Category, matches[0].Key
			if value == "" {
				value = extractAfterChangeVerb(message)
			}
		}
	}
	if category == "" || key == "" {
		out := programIntent(KindClarify, "fact."+action, 0.7)
		out.Missing = []string{"事实分类", "事实名称"}
		out.Reply = "请按“事实分类-事实名称：内容”说明，例如“发货物流-快递方式：顺丰”。"
		return out, true
	}
	if action != "disable" && value == "" {
		out := programIntent(KindClarify, "fact."+action, 0.82)
		out.Target["fact_category"] = category
		out.Target["fact_key"] = key
		out.Missing = []string{"事实内容"}
		out.Reply = "已经定位到“" + category + "-" + key + "”，请补充新的事实内容。"
		return out, true
	}
	out := programIntent(KindCommand, "fact."+action, 0.99)
	out.Target["fact_category"] = category
	out.Target["fact_key"] = key
	if value != "" {
		out.Changes["fact_value"] = value
	}
	return out, true
}

func factMatchesNaturalTopic(compact string, item LiveStrategyFact) bool {
	formal := compactProgramText(item.Category + item.Key)
	topics := []struct {
		query  []string
		formal []string
	}{
		{query: []string{"快递", "物流"}, formal: []string{"快递", "物流"}},
		{query: []string{"发货地", "哪里发货", "从哪发"}, formal: []string{"发货", "仓库", "发出地"}},
		{query: []string{"产地", "哪里产", "哪产"}, formal: []string{"产地", "原产地", "生产地"}},
		{query: []string{"保质期"}, formal: []string{"保质期"}},
		{query: []string{"资质", "证书", "认证"}, formal: []string{"资质", "证书", "认证"}},
		{query: []string{"售后", "退换", "退款"}, formal: []string{"售后", "退换", "退款"}},
		{query: []string{"库存", "现货"}, formal: []string{"库存", "现货"}},
	}
	for _, topic := range topics {
		if containsAnyProgram(compact, topic.query...) && containsAnyProgram(formal, topic.formal...) {
			return true
		}
	}
	return false
}

func parseFactTriple(message string) (string, string, string) {
	value := strings.TrimSpace(message)
	for _, prefix := range []string{"添加事实", "新增事实", "录入事实", "修改事实", "更新事实", "调整事实", "删除事实", "停用事实"} {
		value = strings.TrimSpace(strings.TrimPrefix(value, prefix))
	}
	left, right := value, ""
	if colon := strings.Index(value, "："); colon >= 0 {
		left, right = value[:colon], value[colon+len("："):]
	} else if colon := strings.Index(value, ":"); colon >= 0 {
		left, right = value[:colon], value[colon+1:]
	}
	left = strings.TrimSpace(left)
	for _, sep := range []string{"-", "—", "/", "｜", "|"} {
		if index := strings.Index(left, sep); index > 0 {
			return strings.TrimSpace(left[:index]), strings.TrimSpace(left[index+len(sep):]), strings.TrimSpace(right)
		}
	}
	return "", "", strings.TrimSpace(right)
}

func extractAfterChangeVerb(message string) string {
	for _, verb := range []string{"修改为", "调整为", "设置为", "改成", "改为", "设为"} {
		if index := strings.LastIndex(message, verb); index >= 0 {
			return strings.TrimSpace(message[index+len(verb):])
		}
	}
	return ""
}

func programInterpretScript(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if !containsAnyProgram(compact, "话术参考", "参考说法", "主播怎么说", "口播参考") && ctx.CurrentMode != "script" {
		return UnifiedIntent{}, false
	}
	action := mutationVerb(compact)
	if action == "" {
		return UnifiedIntent{}, false
	}
	title := extractField(message, "话术标题", "标题")
	text := extractField(message, "话术内容", "参考内容", "正文", "内容")
	var existing *LiveStrategyScriptReference
	for i := range ctx.ScriptReferences {
		item := &ctx.ScriptReferences[i]
		if (title != "" && item.Title == title) || strings.Contains(message, item.Title) || strings.Contains(message, item.ReferenceKey) {
			if existing != nil && existing.ReferenceKey != item.ReferenceKey {
				existing = nil
				break
			}
			existing = item
		}
	}
	if action != "add" && existing == nil && len(ctx.ScriptReferences) == 1 && ctx.CurrentMode == "script" {
		existing = &ctx.ScriptReferences[0]
	}
	if action != "add" && existing == nil {
		out := programIntent(KindClarify, "script."+action, 0.7)
		out.Missing = []string{"话术参考目标"}
		out.Reply = "请说明要修改或停用哪一条话术参考。"
		return out, true
	}
	if action != "disable" && text == "" {
		out := programIntent(KindClarify, "script."+action, 0.75)
		out.Missing = []string{"话术内容"}
		out.Reply = "请补充要保存的话术参考正文。"
		return out, true
	}
	out := programIntent(KindCommand, "script."+action, 0.97)
	if existing != nil {
		out.Target["script_reference_key"] = existing.ReferenceKey
		out.Target["script_title"] = existing.Title
	} else {
		if title == "" {
			title = "话术参考"
		}
		out.Target["script_reference_key"] = title
		out.Target["script_title"] = title
	}
	if text != "" {
		out.Changes["script_text"] = text
	}
	return out, true
}

func programInterpretPlan(message, compact string, ctx LiveStrategyProgramContext) (UnifiedIntent, bool) {
	if !containsAnyProgram(compact, "方案", "绑定", "解绑", "切换方案", "使用方案", "运行方案") && ctx.CurrentMode != "plan" {
		return UnifiedIntent{}, false
	}
	action := ""
	switch {
	case containsAnyProgram(compact, "解绑", "取消绑定", "解除绑定"):
		action = "unbind"
	case containsAnyProgram(compact, "切换", "使用方案", "运行方案", "用这个方案", "改用"):
		action = "switch"
	case containsAnyProgram(compact, "绑定", "关联方案"):
		action = "bind"
	default:
		return UnifiedIntent{}, false
	}
	var matches []LiveStrategyPlan
	for _, item := range ctx.Plans {
		if item.Name != "" && strings.Contains(message, item.Name) {
			matches = append(matches, item)
			continue
		}
		idText := strconv.FormatInt(item.ID, 10)
		if strings.Contains(compact, "方案"+idText) || strings.Contains(compact, idText+"号方案") {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		out := programIntent(KindClarify, "plan."+action, 0.7)
		out.Missing = []string{"方案"}
		out.Reply = "请说清楚要" + map[string]string{"bind": "绑定", "unbind": "解绑", "switch": "切换到"}[action] + "哪个方案。"
		return out, true
	}
	out := programIntent(KindCommand, "plan."+action, 0.99)
	out.Target["plan_id"] = matches[0].ID
	out.Target["plan_name"] = matches[0].Name
	return out, true
}

func ProgramSummary(intent UnifiedIntent) string {
	return fmt.Sprintf("%s/%s %.2f", intent.Kind, intent.Intent, intent.Confidence)
}
