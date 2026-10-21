package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
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

// factNumbers 取出文本里的数值，跳过「1号链接」「2号套餐」这类编号。
//
// 不跳过的话，链接编号会被当成数值参与减差：报错会写成「差值是 1、2、12、7、150」，
// 用户完全看不懂这些数字指什么（1 和 2 只是链接序号）。
func factNumbers(text string) []string {
	indexes := factNumberPattern.FindAllStringIndex(text, -1)
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		rest := strings.TrimLeft(text[index[1]:], " ")
		if strings.HasPrefix(rest, "号") {
			continue
		}
		out = append(out, text[index[0]:index[1]])
	}
	return out
}

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
	// 规格与价格多半只存在商品链接上（如「含22道荤素菜」「团购337」），事实依据里往往没抄一份。
	// 不带上它们，模型只能回「事实不足」，用户就只能自己拍数字（实测写成了「多10道」，实际差7道）。
	specText := appendProductSpecs(perLink, links, wantedLinkKeys(linkIDs, nameByID))
	allText += specText
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
			analysis.Digits = factNumbers(analysis.Value)
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

// validateFactNumbers 在「挂在多个链接上」的事实保存前核对数字。
//
// 起因：商品资料是 22 道对 15 道，事实里却写成「多了10道菜」。10 在「10人餐」「10道凉菜」
// 里出现过，纯看材料发现不了，只能靠算。这里把差异分析用的那套数字核对前移到保存前：
// 数字必须能直接从资料/事实里读到，或者是两个来源数字的差（差额本身是合法结论）。
func (s *Server) validateFactNumbers(ctx context.Context, tenant, plan int64, fact model.LiveAgentPlanFact, value string, items []model.LiveAgentPlanFact) error {
	if fact.Scope == nil || fact.Scope.Kind != "product" {
		return nil
	}
	ids := fact.Scope.ProductScopeLinkIDs()
	if len(ids) < 2 {
		return nil
	}
	links, err := s.store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		// 读不到资料就不拦，宁可放过也不要因为基础设施问题挡住正常保存。
		return nil
	}
	nameByID := map[int64]string{}
	for _, link := range links {
		if link.Status == "active" {
			nameByID[link.ID] = link.LinkKey
		}
	}
	perLink, source := s.groupFactsByLink(items, ids, nameByID, fact.ID)
	source += appendProductSpecs(perLink, links, wantedLinkKeys(ids, nameByID))
	if strings.TrimSpace(source) == "" {
		return nil
	}
	// 只算「链接之间」的差值：同一个链接内部的数字相减（原价减团购价）不是对比结论，
	// 混进来会让提示变成一串没头没脑的数字。
	gaps := crossLinkGaps(perLink, orderedLinkKeys(ids, nameByID))
	missing := unknownDigits(value, source)
	if len(missing) > 0 {
		hint := "资料里找不到「" + strings.Join(missing, "」「") + "」这个数字"
		if len(gaps) > 0 {
			hint += "。这几个链接之间能对上的差值是 " + gapClause(gaps, claimCheck{})
		}
		return fmt.Errorf("这条事实的数字和商品资料对不上：%s，请核对后再保存", hint)
	}
	// 光看「数字有没有出现过」不够：写成「多了10道菜」时，10 在「10人餐」「10道凉菜」里都出现过，
	// 但差额结论只能是两个链接的数字相减，所以差额语境要单独核对。
	if claim, bad := checkDifferenceClaims(value, gaps); bad {
		if len(gaps) == 0 {
			return fmt.Errorf("这条事实写了差额「%s」，但这几个链接的商品资料之间没有能对上的同一项，建议先补两边的明细再写差额", claim.Claimed)
		}
		return fmt.Errorf("这条事实写的差额「%s」和商品资料对不上：%s，请核对后再保存", claim.Claimed, gapClause(gaps, claim))
	}
	return nil
}

// gapClause 把差值写成「22道-15道=7道、337-187=150」这种带算式的一句，最多列 3 个。
// 只列裸数字（「7、150、210」）用户看不懂谁减谁；优先列同一项对得上的算式。
func gapClause(gaps []sourceGap, claim claimCheck) string {
	// 用户写的是「差10道」，就只摆「道」这一项的算式，别的项目不必让他看。
	if claim.HasUnit {
		filtered := make([]sourceGap, 0, len(gaps))
		for _, gap := range gaps {
			if gap.Unit == claim.Unit {
				filtered = append(filtered, gap)
			}
		}
		if len(filtered) > 0 {
			gaps = filtered
		}
	}
	texts := make([]string, 0, 3)
	for _, gap := range gaps {
		if gap.Primary {
			texts = append(texts, gap.Text)
			if len(texts) >= 3 {
				return strings.Join(texts, "、")
			}
		}
	}
	if len(texts) > 0 {
		// 有「同一项对得上」的算式就只给这些：再摆「22道-10道=12道」只会让人更糊涂。
		return strings.Join(texts, "、")
	}
	for _, gap := range gaps {
		texts = append(texts, gap.Text)
		if len(texts) >= 2 {
			break
		}
	}
	return strings.Join(texts, "、")
}

// orderedLinkKeys 按方案里选中的顺序返回链接名，保证差异提示的顺序稳定。
func orderedLinkKeys(ids []int64, nameByID map[int64]string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if key := nameByID[id]; key != "" {
			out = append(out, key)
		}
	}
	return out
}

// differenceClaimPattern 抓「多了N」「少了N」「相差N」「省N」「贵N」这类差额结论里的数字。
var differenceClaimPattern = regexp.MustCompile(`(?:多出|多了|少出|少了|多|少|相差约|相差|省下|省|贵|便宜|高出|低于)[^0-9，。；、]{0,4}([0-9]+(?:\.[0-9]+)?)`)

// claimCheck 是一条差额断言的核对结果：写的数字、它的计量单位。
type claimCheck struct {
	Claimed string
	Unit    string
	HasUnit bool
}

// checkDifferenceClaims 核对差额语境里的数字，是不是这几个链接之间真实存在的差值。
func checkDifferenceClaims(value string, gaps []sourceGap) (claimCheck, bool) {
	allowed := map[float64]bool{}
	for _, gap := range gaps {
		allowed[gap.Diff] = true
	}
	indexes := differenceClaimPattern.FindAllStringSubmatchIndex(value, -1)
	for _, index := range indexes {
		numberStart, numberEnd := index[2], index[3]
		// 「多了1号链接」这种，1 是链接序号不是差额。
		if strings.HasPrefix(strings.TrimLeft(value[numberEnd:], " "), "号") {
			continue
		}
		f, err := strconv.ParseFloat(value[numberStart:numberEnd], 64)
		if err != nil {
			continue
		}
		if !allowed[f] {
			unit, ok := unitAfter(value, numberEnd)
			return claimCheck{Claimed: value[numberStart:numberEnd], Unit: unit, HasUnit: ok}, true
		}
	}
	return claimCheck{}, false
}

// unitAfter 取数字后面的计量单位，用于把提示收窄到同一个项目上。
// 第二个返回值表示「确实带了单位」——价格常写成「团购价=337」没有单位，
// 与「解析不出单位」要区分开，否则价格类提示会退化成列出所有项目。
func unitAfter(text string, end int) (string, bool) {
	rest := strings.TrimLeft(text[end:], " ")
	for _, word := range unitWords {
		if strings.HasPrefix(rest, word) {
			return normalizeUnit(word), true
		}
	}
	for _, r := range rest {
		if unitRunes[r] {
			return normalizeUnit(string(r)), true
		}
		break
	}
	return "", false
}

// normalizeUnit 把「元/块」归到无单位一组：商品资料里价格常写成「团购价=337」，
// 用户却会写「贵200元」，不归一会导致提示里比对不上价格这一项。
func normalizeUnit(unit string) string {
	if unit == "元" || unit == "块" {
		return ""
	}
	return unit
}

// sourceGap 是一个可以拿来讲的差值，Text 带算式（「22道-15道=7道」），便于用户自己核对。
type sourceGap struct {
	Diff float64
	Text string
	Unit string
	// Primary 表示两边取的都是「该单位下第一个数」，是真正同一项的对比，
	// 报错时优先展示它，避免把「22道荤素菜 - 10道凉菜」这种跨项目算式摆给用户看。
	Primary bool
}

// unitRunes / unitWords 用来判断数字后面的计量单位。只有单位相同的数字才能互相减，
// 否则会算出「22道荤素菜 - 187元 = 165」这种毫无意义的结果。
var unitRunes = map[rune]bool{
	'道': true, '人': true, '元': true, '种': true, '个': true, '份': true, '斤': true, '克': true,
	'瓶': true, '盒': true, '张': true, '次': true, '天': true, '年': true, '月': true, '只': true,
	'杯': true, '包': true, '袋': true, '块': true, '串': true, '位': true, '件': true, '套': true,
	'条': true, '支': true, '片': true, '颗': true, '粒': true, '折': true, '层': true, '寸': true,
}

var unitWords = []string{"小时", "分钟", "公斤", "毫升", "千克", "厘米"}

type numbered struct {
	Value float64
	Unit  string
}

// numbersWithUnit 取出文本里的数字以及它紧跟的单位，链接编号（1号链接的 1）不算。
func numbersWithUnit(text string) []numbered {
	indexes := factNumberPattern.FindAllStringIndex(text, -1)
	out := make([]numbered, 0, len(indexes))
	for _, index := range indexes {
		rest := strings.TrimLeft(text[index[1]:], " ")
		value, err := strconv.ParseFloat(text[index[0]:index[1]], 64)
		if err != nil {
			continue
		}
		unit := ""
		if strings.HasPrefix(rest, "号") {
			continue
		}
		for _, word := range unitWords {
			if strings.HasPrefix(rest, word) {
				unit = word
				break
			}
		}
		if unit == "" {
			for _, r := range rest {
				if unitRunes[r] {
					unit = string(r)
				}
				break
			}
		}
		out = append(out, numbered{Value: value, Unit: normalizeUnit(unit)})
	}
	return out
}

// crossLinkGaps 只算「不同链接、同一单位」的差值：同链接内部（原价减团购价）和跨单位
// （道数减价格）都不是对比结论，混进来只会让提示变成一串看不懂的数字。
func crossLinkGaps(perLink map[string][]string, order []string) []sourceGap {
	type linkNums struct {
		byUnit map[string][]float64
	}
	groups := make([]linkNums, 0, len(order))
	seenLink := map[string]bool{}
	for _, key := range order {
		if seenLink[key] {
			continue
		}
		seenLink[key] = true
		byUnit := map[string][]float64{}
		seen := map[string]map[float64]bool{}
		for _, line := range perLink[key] {
			for _, item := range numbersWithUnit(line) {
				if item.Value == 0 {
					continue
				}
				if seen[item.Unit] == nil {
					seen[item.Unit] = map[float64]bool{}
				}
				if seen[item.Unit][item.Value] {
					continue
				}
				seen[item.Unit][item.Value] = true
				byUnit[item.Unit] = append(byUnit[item.Unit], item.Value)
			}
		}
		groups = append(groups, linkNums{byUnit: byUnit})
	}
	primary := []sourceGap{}
	others := []sourceGap{}
	used := map[float64]bool{}
	for i := 0; i < len(groups); i++ {
		for j := i + 1; j < len(groups); j++ {
			units := []string{}
			seenUnit := map[string]bool{}
			for unit := range groups[i].byUnit {
				if !seenUnit[unit] {
					seenUnit[unit] = true
					units = append(units, unit)
				}
			}
			for unit := range groups[j].byUnit {
				if !seenUnit[unit] {
					seenUnit[unit] = true
					units = append(units, unit)
				}
			}
			// 固定顺序，保证每次提示的算式排序一致（map 遍历是随机的）。
			sort.Strings(units)
			for _, unit := range units {
				left, right := groups[i].byUnit[unit], groups[j].byUnit[unit]
				for a := range left {
					for b := range right {
						diff := left[a] - right[b]
						if diff < 0 {
							diff = -diff
						}
						if diff == 0 || used[diff] {
							continue
						}
						used[diff] = true
						gap := sourceGap{Diff: diff, Unit: unit, Text: formatNumber(left[a]) + unit + "-" + formatNumber(right[b]) + unit + "=" + formatNumber(diff) + unit}
						if a == 0 && b == 0 {
							gap.Primary = true
							primary = append(primary, gap)
						} else {
							others = append(others, gap)
						}
					}
				}
			}
		}
	}
	return append(primary, others...)
}

func formatNumber(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// wantedLinkKeys 把方案里选中的链接 id 换成 link_key 集合。
func wantedLinkKeys(linkIDs []int64, nameByID map[int64]string) map[string]bool {
	out := map[string]bool{}
	for _, id := range linkIDs {
		if key := nameByID[id]; key != "" {
			out[key] = true
		}
	}
	return out
}

// appendProductSpecs 把选中的商品链接资料（规格/团购价/数量/适用人数）并进分组，
// 返回同样的内容用于数字核对。差异分析最常对上的就是这几项，而它们通常不在「事实依据」里。
func appendProductSpecs(perLink map[string][]string, links []model.LiveAgentPlanProductLink, wanted map[string]bool) string {
	var all strings.Builder
	for _, link := range links {
		if link.Status != "active" || !wanted[link.LinkKey] {
			continue
		}
		parts := []string{}
		if name := strings.TrimSpace(link.ProductName); name != "" {
			parts = append(parts, "商品="+name)
		}
		if spec := strings.TrimSpace(link.Spec); spec != "" {
			parts = append(parts, "规格="+spec)
		}
		if price := strings.TrimSpace(link.DailyPrice); price != "" {
			parts = append(parts, "团购价="+price)
		}
		if qty := strings.TrimSpace(link.Quantity); qty != "" {
			parts = append(parts, "数量="+qty)
		}
		if audience := strings.TrimSpace(link.Audience); audience != "" {
			parts = append(parts, "适用人数="+audience)
		}
		if len(parts) == 0 {
			continue
		}
		line := "商品资料：" + strings.Join(parts, "；")
		perLink[link.LinkKey] = append(perLink[link.LinkKey], line)
		all.WriteString(line)
		all.WriteString("\n")
	}
	return all.String()
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

// unknownDigits 返回 text 里出现、但事实来源里推不出来的数字。
//
// 「相差7道」「省150元」本身不在事实里，是由两个数字相减得到的，
// 所以合法集合里要加上「来源里任意两数之差」，否则正确结论会被误杀。
// 链接编号（1号链接的 1）两边都会被跳过，不参与比较。
func unknownDigits(text, source string) []string {
	source = strings.ReplaceAll(source, "，", ",")
	have := map[string]bool{}
	values := []float64{}
	for _, d := range factNumbers(source) {
		have[d] = true
		if f, err := strconv.ParseFloat(d, 64); err == nil {
			have[strconv.FormatFloat(f, 'f', -1, 64)] = true
			values = append(values, f)
		}
	}
	for i := 0; i < len(values); i++ {
		for j := i + 1; j < len(values); j++ {
			diff := values[i] - values[j]
			if diff < 0 {
				diff = -diff
			}
			have[strconv.FormatFloat(diff, 'f', -1, 64)] = true
		}
	}
	var missing []string
	seen := map[string]bool{}
	for _, d := range factNumbers(text) {
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
