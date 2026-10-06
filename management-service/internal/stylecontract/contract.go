// Package stylecontract defines the model-independent boundary between style
// analysis and speech generation. Models infer rules; code validates and renders
// those rules. No merchant/product vocabulary or merchant-specific nickname is
// preset. A small source-grounded Mandarin live-address grammar prevents a model
// omission from erasing repeated audience addresses or first-party self-reference.
package stylecontract

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

const Version = "anchor-delivery/v1"
const AnalysisInstructions = `【跨模型口播规范协议 anchor-delivery/v1】
你必须根据本次样本独立生成 delivery_spec，不复用示例主播或其它方案的词表。
先把样本分成三层：腔调层（提取并复用）、逻辑层（只作可变参考）、内容层（商品事实、价格、产地、售后等丢弃并由正式事实替换）。催单、发货、稀缺、售后、算账是业务环节，不得因为出现在样本中就固化成主播底层风格。
分析方法与结果必须分开：语气词、称呼、自指、句式、方言、数字表达、回环和情绪三指标是通用测量维度；具体词、密度、比例和情绪映射只属于本次主播样本，不能写成跨主播常量。
literal_habits 提取真实出现的原词，kind 只允许 self_address、audience_address、audience_pronoun、particle、connector、catchphrase、dialect_marker。
逐项区分主播自称和观众称呼；保留完整原词及变体，不能把原词泛化成同义词或仅写“语气词”。
于是“我们家/我们这边”这类稳定的主播方、店铺方第一方自指可以归入 self_address，但团队成员、亲属或第三人称称谓不能因为出现在稿件里就当作主播自称。重复出现的观众称呼和第一方自指不得因为所在句包含销售内容而整类遗漏；只截取称呼或自指原词。
高频句末语气词不得遗漏；同时提取常用连接词、口头禅、称呼位置。样本没有的不能凭行业习惯补充。长样本（约1500字以上）才用频次和密度判断稳定性；几百字短样本先判断存在性和种类，频次只能作为低置信候选，不得据此硬性补齐。
text 是原文原词；position 是句首/句中/句尾/混合；when 是适用场景；avoid 是不宜使用的场景；count 不必估算，程序会依据原文重新计算。
instructions 返回8到16条可直接约束另一个生成模型的规则，每条描述具体句式、位置、密度、转场或禁忌，不要空泛形容词。
情绪只描述样本中可观察的强弱、热度、亲和、舒缓/推进变化及其证据；先按本主播样本重新映射情绪标记，不把“啊=高亢”“嘛=舒缓”这类结论写成跨主播通用规则。单次出现或短样本偶发词只能视为候选/噪声，不得变成硬约束。
这是纯表达风格协议，不是销售策略协议。商品、品牌、数字、产地、主播身份、价格、库存、物流、售后、口碑、社会证明、紧迫感、保证承诺、购买状态、促单动作和履约动作都不得进入 instructions 或 literal_habits。
“一定不会让你失望”“新用户跟随老用户”“都买好了吗”这类话即使重复出现，也携带承诺、社会证明或购买状态语义，不是纯口头禅；只能进入 excluded_from_style，不能进入 delivery_spec。
instructions 只描述称呼/自称位置、长短句组合、连接与转折、确认方式、停顿断句、无业务含义的换词复述，以及主线/短答/严肃场景的表达差异。不得复刻整篇稿件的销售步骤、证据顺序或转化链路。
不得把假装查库存、装车、读真实弹幕等行为作为风格规则。每条规则应注明适用的主线、短答或严肃场景，句式举例使用中性的<内容>占位符，不携带任何业务断言。
没有证据时明确保留不确定性，不能强行推断方言、人格或声音音高。纯文本只判断文字表现。
同一协议应适用于克制讲解、热情促销、故事表达等不同主播；词表必须随样本变化。
严格输出 delivery_spec: {"version":"anchor-delivery/v1","instructions":["可执行规则"],"literal_habits":[{"kind":"类别","text":"原词","position":"位置","when":"场景","avoid":"不适用场景"}]}。`

var commercial = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?\s*(?:元|块|斤|公斤|克|升|毫升|桶|件|盒|袋))|([0-9一二三四五六七八九十]+\s*号\s*(?:链接|商品))`)
var sentenceBreak = regexp.MustCompile(`[。！？!?；;\n]+`)
var liveAudienceAddress = regexp.MustCompile(`哥哥姐姐们|哥哥姐们|叔叔阿姨们|新粉丝们|老粉丝们|家人们|朋友们|姐妹们|兄弟们|粉丝们|乡亲们|哥哥们|姐姐们|叔叔们|阿姨们|新粉丝|老粉丝|新粉|老粉|老乡|乡亲`)
var liveAudiencePronoun = regexp.MustCompile(`你们|您|大家`)
var liveSelfAddress = regexp.MustCompile(`我们自家|咱们自家|我们家|咱们家|我们这边|咱们这边|我们这儿|咱们这儿|我们这里|咱们这里|咱家|我家`)
var liveDialectMarker = regexp.MustCompile(`跟到|晓得|啥子|巴适|屋头|锅头|没得|一哈|紧到|要得|莫得|啷个|咋个|撒子|娃儿|雄起`)

func sampleConfidence(chars, sentences int) string {
	if chars >= 1500 && sentences >= 20 {
		return "high"
	}
	if chars >= 800 && sentences >= 10 {
		return "medium"
	}
	return "low"
}

func habitStability(kind string, count, sampleChars int) string {
	if count <= 1 {
		return "noise"
	}
	if (kind == "self_address" || kind == "audience_address" || kind == "audience_pronoun") && count >= 3 && sampleChars >= 800 {
		return "stable"
	}
	if count >= 3 && sampleChars >= 1500 {
		return "stable"
	}
	return "candidate"
}

func trim(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit])
	}
	return s
}

func literalHabitTextAllowed(kind, text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	// Audience and self-address are lexical identity markers. A word such as
	// “老粉” may also participate in social proof in another sentence, but the
	// grounded word itself must not be discarded when its declared role is an
	// address. Concrete product, price and business-action wording remains barred.
	if kind == "audience_address" || kind == "audience_pronoun" || kind == "self_address" {
		if commercial.MatchString(text) || styleBusinessFactPattern.MatchString(text) || styleBusinessActionPattern.MatchString(text) || stylePromisePattern.MatchString(text) || styleSyntheticStatePattern.MatchString(text) || styleConversionLogicPattern.MatchString(text) || styleContentExamplePattern.MatchString(text) {
			return false
		}
		return !strings.ContainsAny(text, "。！？!?；;\n\r")
	}
	return pureStyleText(text)
}

func literalHabitPosition(source, text string) string {
	start, middle, end := false, false, false
	for offset := 0; offset < len(source); {
		index := strings.Index(source[offset:], text)
		if index < 0 {
			break
		}
		index += offset
		before := strings.TrimRight(source[:index], " \t\"'“‘（(")
		after := strings.TrimLeft(source[index+len(text):], " \t\"'”’）)")
		atStart := before == ""
		if !atStart {
			previous, _ := utf8.DecodeLastRuneInString(before)
			atStart = strings.ContainsRune("，。！？!?；;：:\n\r", previous)
		}
		atEnd := after == ""
		if !atEnd {
			next, _ := utf8.DecodeRuneInString(after)
			atEnd = strings.ContainsRune("，。！？!?；;：:\n\r", next)
		}
		if atStart {
			start = true
		} else if atEnd {
			end = true
		} else {
			middle = true
		}
		offset = index + len(text)
	}
	if start && !middle && !end {
		return "句首"
	}
	if end && !start && !middle {
		return "句尾"
	}
	if middle && !start && !end {
		return "句中"
	}
	return "混合"
}

func deriveRepeatedLiveHabits(source, kind string, pattern *regexp.Regexp, limit int) []model.LiveAnchorLiteralHabit {
	matches := pattern.FindAllString(source, -1)
	// Repeated evidence is assessed at category level. Once the speaker clearly
	// has a stable address/self-reference system, a real one-off variant is kept
	// as a variant instead of being falsely declared absent.
	if len(matches) < 2 {
		return nil
	}
	counts := map[string]int{}
	first := map[string]int{}
	for index, text := range matches {
		counts[text]++
		if _, exists := first[text]; !exists {
			first[text] = index
		}
	}
	items := make([]model.LiveAnchorLiteralHabit, 0, len(counts))
	for text, count := range counts {
		habit := model.LiveAnchorLiteralHabit{Kind: kind, Text: text, Count: count, Position: literalHabitPosition(source, text), Stability: habitStability(kind, count, utf8.RuneCountInString(strings.TrimSpace(source)))}
		if kind == "audience_address" {
			habit.When = "直播中自然提醒、转场或面向对应观众群体时按样本密度使用"
			habit.Avoid = "投诉、严肃说明、对观众身份不确定或相邻分句已经称呼时避免使用"
		} else if kind == "audience_pronoun" {
			habit.When = "面向观众解释、提问或给出行动建议时按样本比例自然使用"
			habit.Avoid = "严肃说明中避免反复点名；不得把泛指代词改造成不存在的观众身份"
		} else {
			habit.When = "说明当前主播方、商家方或已有事实时按样本密度使用"
			habit.Avoid = "跨商家、跨主播或当前主体不一致时须替换为当前主体，不得照搬身份"
		}
		items = append(items, habit)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		if first[items[i].Text] != first[items[j].Text] {
			return first[items[i].Text] < first[items[j].Text]
		}
		return utf8.RuneCountInString(items[i].Text) > utf8.RuneCountInString(items[j].Text)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// Normalize discards unsupported vocabulary and derives counts from the actual
// sample. Never trusts model-generated counts, statistics, hashes or rulebooks.
func Normalize(profile model.LiveAgentPlanAnchorStyleProfile, source string) model.LiveAgentPlanAnchorStyleProfile {
	if profile.Delivery == nil {
		return profile
	}
	input := profile.Delivery
	if input.Version != Version {
		profile.Delivery = nil
		return profile
	}
	// Model-authored instructions are evidence-analysis output, not executable
	// policy. Only literal habits that can be grounded back to the source are
	// admitted; code compiles the final rules after deriving source statistics.
	sampleChars := utf8.RuneCountInString(strings.TrimSpace(source))
	spec := &model.LiveAnchorDeliverySpec{Version: Version, Instructions: []string{}, Habits: []model.LiveAnchorLiteralHabit{}, SampleChars: sampleChars}
	seen := map[string]bool{}
	for _, h := range input.Habits {
		switch h.Kind {
		case "self_address", "audience_address", "audience_pronoun", "particle", "connector", "catchphrase", "dialect_marker":
		default:
			continue
		}
		h.Text = trim(h.Text, 24)
		key := h.Kind + ":" + h.Text
		if !literalHabitTextAllowed(h.Kind, h.Text) || seen[key] || !strings.Contains(source, h.Text) {
			continue
		}
		switch h.Position {
		case "句首", "句中", "句尾", "混合":
		default:
			h.Position = "混合"
		}
		h.When, h.Avoid = trim(h.When, 100), trim(h.Avoid, 100)
		if (h.When != "" && !pureStyleText(h.When)) || (h.Avoid != "" && !pureStyleText(h.Avoid)) {
			continue
		}
		h.Count = strings.Count(source, h.Text)
		h.Stability = habitStability(h.Kind, h.Count, sampleChars)
		// A single mention of a person or audience label is too weak to become a
		// reusable identity habit. It may be a team member, quoted customer or an
		// accidental one-off address. Repeated evidence is required for runtime.
		if (h.Kind == "self_address" || h.Kind == "audience_address" || h.Kind == "audience_pronoun") && h.Count < 2 {
			continue
		}
		seen[key] = true
		spec.Habits = append(spec.Habits, h)
		// Leave room for source-derived terminal particles. Those counts are
		// deterministic and should not be crowded out by a verbose model list.
		if len(spec.Habits) == 20 {
			break
		}
	}
	// Live address and first-party identity are as characteristic as particles.
	// Recover source-grounded terms deterministically when the model omits them.
	for _, derived := range append(
		append(
			deriveRepeatedLiveHabits(source, "audience_address", liveAudienceAddress, 8),
			deriveRepeatedLiveHabits(source, "audience_pronoun", liveAudiencePronoun, 6)...,
		),
		deriveRepeatedLiveHabits(source, "self_address", liveSelfAddress, 6)...,
	) {
		key := derived.Kind + ":" + derived.Text
		if seen[key] || len(spec.Habits) >= 28 {
			continue
		}
		seen[key] = true
		spec.Habits = append(spec.Habits, derived)
	}
	// Sentence-final particles are an observable property of the source, not a
	// semantic guess. Derive repeated ones in Go so a model omission cannot make
	// an otherwise valid style analysis fail or trigger another model round-trip.
	for _, marker := range []rune("啊呀哟哦呢嘛啦吧哈呗噢嘞咯") {
		word := string(marker)
		count := particleCount(source, word)
		if count < 3 {
			continue
		}
		found := false
		for _, habit := range spec.Habits {
			if habit.Kind == "particle" && strings.Contains(habit.Text, word) {
				found = true
				break
			}
		}
		if found || len(spec.Habits) >= 32 {
			continue
		}
		spec.Habits = append(spec.Habits, model.LiveAnchorLiteralHabit{
			Kind:      "particle",
			Text:      word,
			Position:  "句尾",
			When:      "自然口语停顿、确认或句末收束时按样本密度使用",
			Avoid:     "严肃说明、投诉或精确事实陈述时不机械添加",
			Count:     count,
			Stability: habitStability("particle", count, sampleChars),
		})
	}
	// Regional wording is style only when the exact marker appears in the
	// source. It is collected after high-frequency core habits so sparse dialect
	// can never crowd addresses, self-reference or particles out of the budget.
	for _, text := range liveDialectMarker.FindAllString(source, -1) {
		key := "dialect_marker:" + text
		count := strings.Count(source, text)
		if count < 2 || seen[key] || len(spec.Habits) >= 40 {
			continue
		}
		seen[key] = true
		spec.Habits = append(spec.Habits, model.LiveAnchorLiteralHabit{
			Kind:      "dialect_marker",
			Text:      text,
			Position:  literalHabitPosition(source, text),
			When:      "只在样本相同口语语境中按原有稀疏密度点缀",
			Avoid:     "不得为了强化地域感集中堆叠，也不得添加样本没有的地域词",
			Count:     count,
			Stability: habitStability("dialect_marker", count, sampleChars),
		})
	}
	for _, sentence := range sentenceBreak.Split(source, -1) {
		if strings.TrimSpace(sentence) != "" {
			spec.SentenceCount++
		}
	}
	if spec.SentenceCount > 0 {
		spec.AverageSentenceChars = spec.SampleChars / spec.SentenceCount
	}
	spec.SampleConfidence = sampleConfidence(spec.SampleChars, spec.SentenceCount)
	spec.Instructions = compilePureInstructions(spec, source, profile.Dimensions)
	spec.SourceSHA256 = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(source))))
	profile.Delivery = spec
	// The legacy display fields used to preserve free-form model prose. Keeping
	// them would make the saved profile look impure even though runtime only uses
	// the compiled rulebook. Publish one canonical, code-authored view instead.
	profile.Summary = fmt.Sprintf("已从%d字、%d个分句中编译纯表达风格；只保留有原文证据的称呼、自指、语气与连接习惯。", spec.SampleChars, spec.SentenceCount)
	profile.ReusableRules = append([]string(nil), spec.Instructions...)
	profile.CandidatePatterns = []string{}
	profile.ExcludedFromStyle = []string{"商品与交易事实", "销售与转化策略", "互动打断与回归策略", "库存、物流、售后与执行状态", "功效、口碑、承诺与社会证明"}
	for index := range profile.Dimensions {
		if !pureStyleText(profile.Dimensions[index].Rule) {
			profile.Dimensions[index].Rule = ""
			profile.Dimensions[index].Confidence = "low"
		}
	}
	spec.Rulebook = Render(profile)
	return profile
}

func Valid(profile model.LiveAgentPlanAnchorStyleProfile) bool {
	return profile.Delivery != nil && profile.Delivery.Version == Version && len(profile.Delivery.Instructions) >= 8 && profile.Delivery.SampleChars > 0
}

// Only count sentence/segment-final particles, not characters within nouns such
// as 酒吧 or 哈密瓜. This grammar check never inserts vocabulary into the rules.
func particleCount(source, word string) int {
	runes := []rune(source)
	count := 0
	for i, r := range runes {
		if string(r) != word {
			continue
		}
		j := i + 1
		for j < len(runes) && strings.ContainsRune("啊呀哟哦呢嘛啦吧哈呗噢嘞咯", runes[j]) {
			j++
		}
		if j == len(runes) || strings.ContainsRune("，。！？!?；;、\n\r \t", runes[j]) {
			count++
		}
	}
	return count
}

// A general Mandarin grammar coverage check, not a preset speaker dictionary.
// A marker becomes required only when this source contains it repeatedly.
func CoverageErrors(profile model.LiveAgentPlanAnchorStyleProfile, source string) []string {
	issues := []string{}
	if !Valid(profile) {
		return []string{"缺少有效的 anchor-delivery/v1 规范或可执行规则不足"}
	}
	// A model that already reports well-evidenced address habits must compile
	// them into the execution contract rather than leave them in a hidden table.
	for _, dimension := range profile.Dimensions {
		kind := ""
		switch dimension.Key {
		case "self_address":
			kind = "self_address"
		case "audience_address":
			kind = "audience_address"
		}
		if kind == "" || dimension.Confidence != "high" || len(dimension.EvidenceQuotes) == 0 {
			continue
		}
		found := false
		for _, h := range profile.Delivery.Habits {
			if h.Kind == kind {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, "分析已识别的"+dimension.Label+"原词未进入执行词表")
		}
	}
	for _, marker := range []rune("啊呀哟哦呢嘛啦吧哈呗噢嘞咯") {
		word := string(marker)
		if particleCount(source, word) < 3 {
			continue
		}
		found := false
		for _, h := range profile.Delivery.Habits {
			if h.Kind == "particle" && strings.Contains(h.Text, word) {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, "遗漏样本重复出现的语气原词："+word)
		}
	}
	return issues
}

// Render is formatting of model-inferred rules, not authoring a speaker style.
func Render(profile model.LiveAgentPlanAnchorStyleProfile) string {
	d := profile.Delivery
	if d == nil || d.Version != Version {
		return ""
	}
	var b strings.Builder
	b.WriteString("主播口播规范 · " + Version + "\n\n【执行边界】\n只模仿跨场次稳定的表达统计，不复制样本商品事实或本场销售策略。正式事实、时间调度、商品讲解顺序与方案级策略外挂优先。不得虚构主播身份、观众发言、库存或已执行的业务动作。原词不足时不要发明新口头禅。所有规则只供静默执行，正文不得向观众播报规则、事实边界、审核过程或写作过程。数字比价、连续算账、事实回环、促单强弱等属于可叠加策略，不由底层主播风格擅自继承。\n")
	fmt.Fprintf(&b, "样本证据：%d字、%d分句；统计置信度=%s。稳定原词才进入高还原硬约束，候选原词用于自然参考，偶发原词忽略。\n", d.SampleChars, d.SentenceCount, d.SampleConfidence)
	labels := map[string]string{"self_address": "主播方自称/自指", "audience_address": "观众称呼", "audience_pronoun": "观众指代", "particle": "语气词", "connector": "连接词", "catchphrase": "口头禅", "dialect_marker": "方言标记"}
	b.WriteString("\n【本样本原词与使用规范】\n")
	if len(d.Habits) == 0 {
		b.WriteString("未发现有充分证据的固定词表，不强制添加称呼或语气词。\n")
	}
	for _, h := range d.Habits {
		fmt.Fprintf(&b, "- %s：%q；原文%d次/%d字；证据：%s；位置：%s；适用：%s", labels[h.Kind], h.Text, h.Count, d.SampleChars, h.Stability, h.Position, h.When)
		if h.Avoid != "" {
			b.WriteString("；避免：" + h.Avoid)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n【表达组织规范】\n")
	for i, rule := range d.Instructions {
		fmt.Fprintf(&b, "%d. %s\n", i+1, rule)
	}
	b.WriteString("\n【生成场景】\n主线及测试：高频原词按样本密度和适用场景自然分布，不得整体遗漏，不要每句堆叠。\n短互动：先准确回答再自然承接，只使用适合本轮的少量原词，不要求每条回答覆盖全词表；投诉、严肃说明避免不适当的促销腔调。\n纯文本统计不代表真实语速、音高或声音克隆参数。\n")
	return b.String()
}

type CheckResult struct {
	Passed  bool     `json:"passed"`
	Missing []string `json:"missing"`
	Checked int      `json:"checked"`
}

// CheckLongText only verifies sufficiently evidenced, frequent lexical habits.
// It is deliberately not a fabricated similarity score or semantic guarantee.
func CheckLongText(profile model.LiveAgentPlanAnchorStyleProfile, text string) CheckResult {
	check := CheckResult{Passed: true, Missing: []string{}}
	if !Valid(profile) || utf8.RuneCountInString(text) < 150 {
		return check
	}
	d := profile.Delivery
	habits := append([]model.LiveAnchorLiteralHabit(nil), d.Habits...)
	sort.SliceStable(habits, func(i, j int) bool {
		if habits[i].Count == habits[j].Count {
			return utf8.RuneCountInString(habits[i].Text) > utf8.RuneCountInString(habits[j].Text)
		}
		return habits[i].Count > habits[j].Count
	})
	kinds := map[string]int{}
	for _, h := range habits {
		if check.Checked >= 6 {
			break
		}
		// Address variants are alternatives, not mandatory words to stack.
		if h.Kind == "self_address" || h.Kind == "audience_address" || h.Kind == "audience_pronoun" {
			if kinds[h.Kind] > 0 || h.Count < 3 || float64(h.Count)*float64(utf8.RuneCountInString(text))/float64(d.SampleChars) < 1.2 {
				continue
			}
			kinds[h.Kind]++
			check.Checked++
			found := strings.Contains(text, h.Text)
			if h.Kind == "audience_address" || h.Kind == "audience_pronoun" {
				for _, alternative := range habits {
					if alternative.Kind == h.Kind && strings.Contains(text, alternative.Text) {
						found = true
						break
					}
				}
			}
			if !found {
				check.Missing = append(check.Missing, h.Text)
			}
			continue
		}
		if h.Count < 3 || kinds[h.Kind] >= 2 || float64(h.Count)*float64(utf8.RuneCountInString(text))/float64(d.SampleChars) < 1.2 {
			continue
		}
		kinds[h.Kind]++
		check.Checked++
		if !strings.Contains(text, h.Text) {
			check.Missing = append(check.Missing, h.Text)
		}
		if check.Checked == 6 {
			break
		}
	}
	check.Passed = len(check.Missing) == 0
	return check
}
