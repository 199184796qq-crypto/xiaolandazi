// Package stylecontract defines the model-independent boundary between style
// analysis and speech generation. Models infer rules; code validates and renders
// those rules. No merchant, product, audience nickname or catchphrase is preset.
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
literal_habits 提取真实出现的原词，kind 只允许 self_address、audience_address、particle、connector、catchphrase。
逐项区分主播自称和观众称呼；保留完整原词及变体，不能把原词泛化成同义词或仅写“语气词”。
于是“我们家/我们这边”这类稳定的主播方、店铺方第一方自指可以归入 self_address，但团队成员、亲属或第三人称称谓不能因为出现在稿件里就当作主播自称。
高频句末语气词不得遗漏；同时提取常用连接词、口头禅、称呼位置。样本没有的不能凭行业习惯补充。
text 是原文原词；position 是句首/句中/句尾/混合；when 是适用场景；avoid 是不宜使用的场景；count 不必估算，程序会依据原文重新计算。
instructions 返回8到16条可直接约束另一个生成模型的规则，每条描述具体句式、位置、密度、转场或禁忌，不要空泛形容词。
这是纯表达风格协议，不是销售策略协议。商品、品牌、数字、产地、主播身份、价格、库存、物流、售后、口碑、社会证明、紧迫感、保证承诺、购买状态、促单动作和履约动作都不得进入 instructions 或 literal_habits。
“一定不会让你失望”“新用户跟随老用户”“都买好了吗”这类话即使重复出现，也携带承诺、社会证明或购买状态语义，不是纯口头禅；只能进入 excluded_from_style，不能进入 delivery_spec。
instructions 只描述称呼/自称位置、长短句组合、连接与转折、确认方式、停顿断句、无业务含义的换词复述，以及主线/短答/严肃场景的表达差异。不得复刻整篇稿件的销售步骤、证据顺序或转化链路。
不得把假装查库存、装车、读真实弹幕等行为作为风格规则。每条规则应注明适用的主线、短答或严肃场景，句式举例使用中性的<内容>占位符，不携带任何业务断言。
没有证据时明确保留不确定性，不能强行推断方言、人格或声音音高。纯文本只判断文字表现。
同一协议应适用于克制讲解、热情促销、故事表达等不同主播；词表必须随样本变化。
严格输出 delivery_spec: {"version":"anchor-delivery/v1","instructions":["可执行规则"],"literal_habits":[{"kind":"类别","text":"原词","position":"位置","when":"场景","avoid":"不适用场景"}]}。`

var commercial = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?\s*(?:元|块|斤|公斤|克|升|毫升|桶|件|盒|袋))|([0-9一二三四五六七八九十]+\s*号\s*(?:链接|商品))`)
var sentenceBreak = regexp.MustCompile(`[。！？!?；;\n]+`)

func trim(s string, limit int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > limit {
		s = string(r[:limit])
	}
	return s
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
	spec := &model.LiveAnchorDeliverySpec{Version: Version, Instructions: []string{}, Habits: []model.LiveAnchorLiteralHabit{}}
	seen := map[string]bool{}
	for _, h := range input.Habits {
		switch h.Kind {
		case "self_address", "audience_address", "particle", "connector", "catchphrase":
		default:
			continue
		}
		h.Text = trim(h.Text, 24)
		key := h.Kind + ":" + h.Text
		if !pureStyleText(h.Text) || seen[key] || !strings.Contains(source, h.Text) {
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
		// A single mention of a person or audience label is too weak to become a
		// reusable identity habit. It may be a team member, quoted customer or an
		// accidental one-off address. Repeated evidence is required for runtime.
		if (h.Kind == "self_address" || h.Kind == "audience_address") && h.Count < 2 {
			continue
		}
		seen[key] = true
		spec.Habits = append(spec.Habits, h)
		// Leave room for source-derived terminal particles. Those counts are
		// deterministic and should not be crowded out by a verbose model list.
		if len(spec.Habits) == 24 {
			break
		}
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
			Kind:     "particle",
			Text:     word,
			Position: "句尾",
			When:     "自然口语停顿、确认或句末收束时按样本密度使用",
			Avoid:    "严肃说明、投诉或精确事实陈述时不机械添加",
			Count:    count,
		})
	}
	spec.SampleChars = utf8.RuneCountInString(strings.TrimSpace(source))
	for _, sentence := range sentenceBreak.Split(source, -1) {
		if strings.TrimSpace(sentence) != "" {
			spec.SentenceCount++
		}
	}
	if spec.SentenceCount > 0 {
		spec.AverageSentenceChars = spec.SampleChars / spec.SentenceCount
	}
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
	b.WriteString("主播口播规范 · " + Version + "\n\n【执行边界】\n只模仿表达，不复制样本商品事实。正式事实与业务规则优先。不得虚构主播身份、观众发言、库存或已执行的业务动作。原词不足时不要发明新口头禅。所有规则只供静默执行，正文不得向观众播报规则、事实边界、审核过程或写作过程。事实有限时允许围绕同一正式事实做多轮口语展开和非连续回环，每次应改变表达动作；内容扩展范围由生成上下文中的fact_expansion用户授权决定，不由主播风格规则擅自放宽或收紧。\n")
	labels := map[string]string{"self_address": "主播方自称/自指", "audience_address": "观众称呼", "particle": "语气词", "connector": "连接词", "catchphrase": "口头禅"}
	b.WriteString("\n【本样本原词与使用规范】\n")
	if len(d.Habits) == 0 {
		b.WriteString("未发现有充分证据的固定词表，不强制添加称呼或语气词。\n")
	}
	for _, h := range d.Habits {
		fmt.Fprintf(&b, "- %s：%q；原文%d次/%d字；位置：%s；适用：%s", labels[h.Kind], h.Text, h.Count, d.SampleChars, h.Position, h.When)
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
		if h.Kind == "self_address" || h.Kind == "audience_address" {
			if kinds[h.Kind] > 0 || h.Count < 3 || float64(h.Count)*float64(utf8.RuneCountInString(text))/float64(d.SampleChars) < 1.2 {
				continue
			}
			kinds[h.Kind]++
			check.Checked++
			found := strings.Contains(text, h.Text)
			if h.Kind == "audience_address" {
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
