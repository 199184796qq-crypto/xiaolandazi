// Package anchorstylescore implements the deterministic rules in the project's
// dynamic scoring specification. It does not call models or mutate speech.
package anchorstylescore

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const Version = "dynamic-score-v2"
const CountingMethod = "Unicode字符数（含标点、不含空白）；同组词最长优先、不重叠；每百字=次数/字符数*100"

type Baseline struct {
	Identity struct {
		Particles struct {
			Main       string   `json:"主力语气词"`
			Other      []string `json:"辅助语气词"`
			Ratio      string   `json:"配比"`
			Candidates []string `json:"候选语气词"`
		} `json:"语气词系统"`
		Audience struct {
			Main       string              `json:"主力称呼"`
			Other      []string            `json:"辅助称呼"`
			Counts     map[string]int      `json:"各称呼频次"`
			Aliases    map[string][]string `json:"称呼别名,omitempty"`
			Candidates []string            `json:"候选称呼"`
		} `json:"称呼体系"`
		Self struct {
			Terms []string `json:"自指词"`
		} `json:"自指体系"`
		Rhythm struct {
			Length            string   `json:"平均分句长度"`
			LengthRange       string   `json:"平均分句长度区间"`
			Confirmations     []string `json:"确认句库"`
			QuestionFrequency string   `json:"问句使用频率"`
		} `json:"句式节奏"`
		Numbers struct {
			Style           string `json:"价格表述方式"`
			Pattern         string `json:"算账句式"`
			LikesArithmetic bool   `json:"是否爱帮观众算账"`
		} `json:"数字比价习惯"`
		Loops struct {
			Phrases  []string `json:"高频回环短语"`
			Strength string   `json:"回环强度"`
		} `json:"重复回环习惯"`
	} `json:"主播识别信息"`
	Quant struct {
		MainParticle        float64 `json:"每百字_主力语气词"`
		Aux1                float64 `json:"每百字_辅助语气词1"`
		Aux2                float64 `json:"每百字_辅助语气词2"`
		Audience            float64 `json:"每百字_主力称呼"`
		Self                float64 `json:"每百字_自指词"`
		Questions           float64 `json:"每百字_问号"`
		QuestionSentences   float64 `json:"每百字_问句,omitempty"`
		Exclamations        float64 `json:"每百字_感叹号"`
		Ratio               string  `json:"语气词配比目标"`
		AverageClauseLength float64 `json:"平均分句长度实测"`
		IncludeLe           bool    `json:"了_计入语气词"`
	} `json:"量化基准（供打分用）"`
	Forbidden struct {
		Addresses   []string `json:"禁用称呼"`
		Formal      []string `json:"禁用书面客服语"`
		Transitions []string `json:"禁用过渡词"`
	} `json:"禁用词（反推）"`
}

type Count struct {
	Term   string  `json:"term"`
	Count  int     `json:"count"`
	Per100 float64 `json:"per_100"`
}
type Stats struct {
	Chars               int     `json:"chars"`
	Particles           []Count `json:"particles"`
	Audience            []Count `json:"audience"`
	Self                []Count `json:"self"`
	SelfExcluded        []Count `json:"self_excluded"`
	AudienceGroups      []Count `json:"audience_groups"`
	LeCount             int     `json:"le_count"`
	LeIncluded          bool    `json:"le_included"`
	MainAudienceCount   int     `json:"main_audience_count"`
	SelfCount           int     `json:"self_count"`
	Questions           int     `json:"questions"`
	QuestionMarks       int     `json:"question_marks"`
	Forbidden           []Count `json:"forbidden_words"`
	Exclamations        int     `json:"exclamations"`
	AverageClauseLength float64 `json:"average_clause_length"`
	Confirmations       []Count `json:"confirmations"`
	Loops               []Count `json:"loops"`
	HasPrice            bool    `json:"has_price"`
	HasSaving           bool    `json:"has_saving"`
}
type Check struct {
	Label     string `json:"label"`
	Actual    string `json:"actual"`
	Target    string `json:"target"`
	Rule      string `json:"rule"`
	Deduction int    `json:"deduction"`
}
type Dimension struct {
	Name       string  `json:"name"`
	MaxScore   int     `json:"max_score"`
	Score      int     `json:"score"`
	Deduction  int     `json:"deduction"`
	Applicable bool    `json:"applicable"`
	Note       string  `json:"note"`
	Checks     []Check `json:"checks"`
}
type Result struct {
	OverallScore   int         `json:"overall_score"`
	Dimensions     []Dimension `json:"dimensions"`
	Stats          Stats       `json:"stats"`
	Version        string      `json:"version"`
	CountingMethod string      `json:"counting_method"`
}

func Parse(raw string) (Baseline, error) {
	var b Baseline
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil || root == nil {
		return b, fmt.Errorf("评分基准必须是JSON对象")
	}
	if root["量化基准"] != nil {
		root["量化基准（供打分用）"] = root["量化基准"]
	}
	for _, key := range []string{"主播识别信息", "量化基准（供打分用）", "禁用词（反推）"} {
		var object map[string]json.RawMessage
		if json.Unmarshal(root[key], &object) != nil || object == nil {
			return b, fmt.Errorf("评分基准缺少对象：%s", key)
		}
	}
	var identity, quant map[string]json.RawMessage
	_ = json.Unmarshal(root["主播识别信息"], &identity)
	_ = json.Unmarshal(root["量化基准（供打分用）"], &quant)
	for _, key := range []string{"语气词系统", "称呼体系", "自指体系", "句式节奏", "数字比价习惯", "方言口头禅", "重复回环习惯", "情绪特征"} {
		var object map[string]json.RawMessage
		if json.Unmarshal(identity[key], &object) != nil || object == nil {
			return b, fmt.Errorf("主播识别信息缺少对象：%s", key)
		}
	}
	for _, key := range []string{"每百字_主力语气词", "每百字_辅助语气词1", "每百字_辅助语气词2", "每百字_主力称呼", "每百字_自指词", "每百字_问号", "每百字_感叹号"} {
		var number float64
		if len(quant[key]) == 0 || string(quant[key]) == "null" || json.Unmarshal(quant[key], &number) != nil || number < 0 || number > 100 || math.IsNaN(number) || math.IsInf(number, 0) {
			return b, fmt.Errorf("%s必须是0到100的数字", key)
		}
	}
	// New qualitative schemas use a list of accounting phrases; old schemas
	// used one string. Normalize only the parser's private copy.
	var audience map[string]json.RawMessage
	_ = json.Unmarshal(identity["称呼体系"], &audience)
	if err := normalizeAudienceAliases(audience); err != nil {
		return b, err
	}
	identity["称呼体系"], _ = json.Marshal(audience)
	var numberStyle map[string]json.RawMessage
	_ = json.Unmarshal(identity["数字比价习惯"], &numberStyle)
	var patterns []string
	if json.Unmarshal(numberStyle["算账句式"], &patterns) == nil {
		numberStyle["算账句式"], _ = json.Marshal(strings.Join(patterns, "；"))
		identity["数字比价习惯"], _ = json.Marshal(numberStyle)
	}
	root["主播识别信息"], _ = json.Marshal(identity)
	if quant["语气词配比"] != nil {
		quant["语气词配比目标"] = quant["语气词配比"]
		root["量化基准（供打分用）"], _ = json.Marshal(quant)
	}
	normalized, _ := json.Marshal(root)
	if err := json.Unmarshal(normalized, &b); err != nil {
		return b, fmt.Errorf("评分基准字段类型错误：%w", err)
	}
	if b.Quant.MainParticle > 0 && strings.TrimSpace(b.Identity.Particles.Main) == "" {
		return b, fmt.Errorf("主力语气词频次非零，但没有指定词")
	}
	if b.Quant.Audience > 0 && strings.TrimSpace(b.Identity.Audience.Main) == "" {
		return b, fmt.Errorf("主力称呼频次非零，但没有指定词")
	}
	if b.Quant.Self > 0 && len(clean(b.Identity.Self.Terms)) == 0 {
		return b, fmt.Errorf("自指频次非零，但自指词为空")
	}
	return b, nil
}

func clean(terms []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term != "" && !seen[term] {
			seen[term] = true
			result = append(result, term)
		}
	}
	return result
}
func round(v float64) float64 { return math.Round(v*10000) / 10000 }
func density(count, chars int) float64 {
	if chars == 0 {
		return 0
	}
	return float64(count) * 100 / float64(chars)
}

// Terms in a group are matched longest first, so 我 cannot count again inside 我们家.
func countGroup(text string, terms []string, chars int) []Count {
	terms = clean(terms)
	ordered := append([]string{}, terms...)
	sort.SliceStable(ordered, func(i, j int) bool { return len([]rune(ordered[i])) > len([]rune(ordered[j])) })
	runes := []rune(text)
	values := map[string]int{}
	for i := 0; i < len(runes); {
		matched := false
		for _, term := range ordered {
			t := []rune(term)
			if i+len(t) <= len(runes) && string(runes[i:i+len(t)]) == term {
				values[term]++
				i += len(t)
				matched = true
				break
			}
		}
		if !matched {
			i++
		}
	}
	result := []Count{}
	for _, term := range terms {
		result = append(result, Count{term, values[term], round(density(values[term], chars))})
	}
	return result
}
func aliases(b Baseline) []string {
	main := b.Identity.Audience.Main
	return clean(append([]string{main}, b.Identity.Audience.Aliases[main]...))
}
func mainAudienceCount(b Baseline, values []Count) int {
	terms := aliases(b)
	total := 0
	for _, v := range values {
		for _, term := range terms {
			if v.Term == term {
				total += v.Count
				break
			}
		}
	}
	return total
}

var clauses = regexp.MustCompile("[，,。！？!?；;\\n]+")
var numbers = regexp.MustCompile("[0-9]+(?:\\.[0-9]+)?")
var prices = regexp.MustCompile("(?:[0-9一二三四五六七八九十百两]+(?:\\.[0-9]+)?)[块元]")
var hesitations = regexp.MustCompile("(?:省|便宜)[^，。！？!?]{0,10}(?:…|\\.\\.\\.|啊对|呃)")

func Measure(text string, b Baseline) Stats {
	s := Stats{Particles: []Count{}, Audience: []Count{}, Self: []Count{}, SelfExcluded: []Count{}, AudienceGroups: []Count{}, Confirmations: []Count{}, Loops: []Count{}}
	for _, r := range text {
		if !unicode.IsSpace(r) {
			s.Chars++
		}
		if r == '？' || r == '?' {
			s.QuestionMarks++
		}
		if r == '！' || r == '!' {
			s.Exclamations++
		}
	}
	s.Questions = CountQuestions(text)
	s.Forbidden = countGroup(text, clean(append(append(append([]string{}, b.Forbidden.Addresses...), b.Forbidden.Formal...), b.Forbidden.Transitions...)), s.Chars)
	s.LeCount = strings.Count(text, "了")
	s.LeIncluded = b.Quant.IncludeLe
	particleTerms := clean(append([]string{b.Identity.Particles.Main}, b.Identity.Particles.Other...))
	if !b.Quant.IncludeLe {
		particleTerms = withoutBareLe(particleTerms)
	}
	s.Particles = countGroup(text, particleTerms, s.Chars)
	addresses := clean(append([]string{b.Identity.Audience.Main}, b.Identity.Audience.Other...))
	allAddresses := append([]string{}, addresses...)
	for _, address := range addresses {
		allAddresses = append(allAddresses, b.Identity.Audience.Aliases[address]...)
	}
	s.Audience = countGroup(text, allAddresses, s.Chars)
	for _, address := range addresses {
		group := Count{Term: address}
		for _, v := range s.Audience {
			if v.Term == address || containsExact(b.Identity.Audience.Aliases[address], v.Term) {
				group.Count += v.Count
			}
		}
		group.Per100 = round(density(group.Count, s.Chars))
		s.AudienceGroups = append(s.AudienceGroups, group)
		if address == b.Identity.Audience.Main {
			s.MainAudienceCount = group.Count
		}
	}
	// Bare 我/我们 are always reported separately, not counted as branded self-reference.
	for _, v := range countGroup(text, append(structuredSelfTerms(b.Identity.Self.Terms), "我们", "我"), s.Chars) {
		if v.Term == "我" || v.Term == "我们" {
			s.SelfExcluded = append(s.SelfExcluded, v)
		} else {
			s.Self = append(s.Self, v)
		}
	}
	for _, v := range s.Self {
		s.SelfCount += v.Count
	}
	total, n := 0, 0
	for _, clause := range clauses.Split(text, -1) {
		count := 0
		for _, r := range clause {
			if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
				count++
			}
		}
		if count > 0 {
			total += count
			n++
		}
	}
	if n > 0 {
		s.AverageClauseLength = round(float64(total) / float64(n))
	}
	// A confirmation is matched without sentence-ending punctuation, as actual
	// transcripts can use a comma where the sample used a question mark.
	confirmationTerms := []string{}
	for _, phrase := range b.Identity.Rhythm.Confirmations {
		confirmationTerms = append(confirmationTerms, strings.TrimRight(strings.TrimSpace(phrase), "？?！!。.,，"))
	}
	s.Confirmations = countGroup(text, confirmationTerms, s.Chars)
	s.Loops = countGroup(text, b.Identity.Loops.Phrases, s.Chars)
	s.HasPrice = prices.MatchString(text)
	s.HasSaving = strings.Contains(text, "省") || strings.Contains(text, "便宜") || strings.Contains(text, "划算")
	return s
}

// VerifySample is retained for legacy callers; the default keeps 了 separate.
func VerifySample(raw, sample string) (string, error) {
	return Calibrate(raw, sample, false)
}

func Score(text string, b Baseline, task string) Result {
	s := Measure(text, b)
	r := Result{Stats: s, Dimensions: []Dimension{}, Version: Version, CountingMethod: CountingMethod}
	add := func(name string, maxScore int, applicable bool, note string, checks []Check) {
		d := Dimension{Name: name, MaxScore: maxScore, Applicable: applicable, Note: note, Checks: checks}
		if d.Checks == nil {
			d.Checks = []Check{}
		}
		if applicable {
			for _, c := range checks {
				d.Deduction += c.Deduction
			}
		}
		d.Deduction = min(maxScore, d.Deduction)
		d.Score = maxScore - d.Deduction
		r.Dimensions = append(r.Dimensions, d)
		r.OverallScore += d.Score
	}
	mainCount, maxOther := 0, 0
	for _, v := range s.Particles {
		if v.Term == b.Identity.Particles.Main {
			mainCount = v.Count
		} else {
			maxOther = max(maxOther, v.Count)
		}
	}
	actual := round(density(mainCount, s.Chars))
	deduction := 0
	if mainCount == 0 && b.Quant.MainParticle > 0 {
		deduction = 12
	} else if mainCount < maxOther {
		deduction = 8
	}
	checks := []Check{{"主力语气词 " + b.Identity.Particles.Main, fmt.Sprintf("%d次；每百字%.4g次", mainCount, actual), fmt.Sprintf("每百字%.4g次，且不被辅助词反超", b.Quant.MainParticle), "归零扣12；被反超扣8；两者取重项", deduction}}
	densityDeduction := 0
	if mainCount > 0 && actual < b.Quant.MainParticle*0.5 {
		densityDeduction = 4
	}
	checks = append(checks, Check{"主力密度", fmt.Sprintf("%.4g/百字", actual), fmt.Sprintf("%.4g/百字", b.Quant.MainParticle), "低于样本目标50%扣4；归零不重复扣密度", densityDeduction})
	// Compare main share with the extracted ratio. Counts in the persisted
	// verified ratio support zero auxiliary terms without dividing by zero.
	ratioText := b.Quant.Ratio
	targetNumbers := ratioText
	if i := strings.LastIndexAny(ratioText, "≈=："); i >= 0 {
		targetNumbers = ratioText[i+len(string([]rune(ratioText[i:])[0])):]
	}
	values := numbers.FindAllString(targetNumbers, -1)
	if len(values) >= 2 && mainCount > 0 && mainCount >= maxOther {
		totalTarget, mainTarget := 0.0, 0.0
		for i, v := range values {
			f, _ := strconv.ParseFloat(v, 64)
			totalTarget += f
			if i == 0 {
				mainTarget = f
			}
		}
		totalActual := 0
		for _, v := range s.Particles {
			totalActual += v.Count
		}
		if totalTarget > 0 && totalActual > 0 && mainTarget > 0 {
			targetShare := mainTarget / totalTarget
			share := float64(mainCount) / float64(totalActual)
			penalty := 0
			if math.Abs(share-targetShare) > targetShare*0.2 {
				penalty = 4
			}
			checks = append(checks, Check{"主力占比", fmt.Sprintf("%.2f%%", share*100), fmt.Sprintf("%.2f%%（%s）", targetShare*100, ratioText), "占比偏离目标超过20%扣4；被反超时不重复扣配比", penalty})
		}
	}
	add("语气词配比", 15, b.Identity.Particles.Main != "" && b.Quant.MainParticle > 0, "基准未定义主力词或样本频次为0时不扣分；本维累计扣分不超过15", checks)
	actual = round(density(s.MainAudienceCount, s.Chars))
	deduction = 0
	target := b.Quant.Audience
	if actual < target*0.3 {
		deduction = 12
	} else if actual < target*0.5 {
		deduction = 9
	} else if actual < target*0.8 {
		deduction = 5
	}
	add("主力称呼密度", 15, b.Identity.Audience.Main != "" && target > 0, "确认的称呼别名合并统计", []Check{{b.Identity.Audience.Main, fmt.Sprintf("%d次；%.4g/百字", s.MainAudienceCount, actual), fmt.Sprintf("%.4g/百字", target), "≥80%不扣；50%~80%扣5；30%~50%扣9；<30%扣12", deduction}})
	actual = round(density(s.SelfCount, s.Chars))
	target = b.Quant.Self
	deduction = 0
	if actual == 0 && target > 0 {
		deduction = 12
	} else if actual < target*0.5 {
		deduction = 8
	} else if actual < target {
		deduction = 4
	}
	add("自指词密度", 15, len(clean(b.Identity.Self.Terms)) > 0 && target > 0, "仅统计基准列出的自指词，最长优先避免重叠", []Check{{strings.Join(b.Identity.Self.Terms, " / "), fmt.Sprintf("%d次；%.4g/百字", s.SelfCount, actual), fmt.Sprintf("%.4g/百字", target), "归零扣12；<50%扣8；50%~100%扣4；达标不扣", deduction}})
	checks = []Check{}
	kinds, total := 0, 0
	for _, v := range s.Confirmations {
		if v.Count > 0 {
			kinds++
		}
		total += v.Count
	}
	if len(s.Confirmations) > 0 {
		deduction = 0
		if kinds == 0 {
			deduction = 10
		} else if kinds == 1 && total > 1 && len(s.Confirmations) > 1 {
			deduction = 5
		}
		checks = append(checks, Check{"确认句", fmt.Sprintf("%d种，共%d次", kinds, total), strings.Join(b.Identity.Rhythm.Confirmations, " / "), "样本有确认句但本段没有扣10；只一种反复用且基准有多种扣5", deduction})
	}
	length := b.Quant.AverageClauseLength
	if n := numbers.FindString(b.Identity.Rhythm.Length); length == 0 && n != "" {
		length, _ = strconv.ParseFloat(n, 64)
	}
	if length > 0 {
		deduction = 0
		if s.AverageClauseLength < length*0.5 || s.AverageClauseLength > length*1.5 {
			deduction = 3
		}
		checks = append(checks, Check{"平均分句长度", fmt.Sprintf("%.4g字", s.AverageClauseLength), fmt.Sprintf("%.4g字", length), "超出样本±50%扣3", deduction})
	}
	add("句式节奏", 15, len(checks) > 0, "按基准确认句和平均分句长度检测；不推断未定义的长短句规律", checks)
	qWeight, eWeight := 3, 12
	if b.Identity.Rhythm.QuestionFrequency == "高" {
		qWeight, eWeight = 8, 7
	}
	gap := func(actual, target float64, weight int) int {
		if target <= 0 || actual >= target || target*float64(s.Chars)/100 < 2 {
			return 0
		}
		return min(weight, int(math.Ceil(float64(weight)*(target-actual)/target)))
	}
	qDensity, eDensity := round(density(s.Questions, s.Chars)), round(density(s.Exclamations, s.Chars))
	qDeduction := gap(qDensity, questionTarget(b), qWeight)
	if b.Identity.Rhythm.QuestionFrequency == "低" {
		qDeduction = 0
	}
	checks = []Check{
		{"问号", fmt.Sprintf("%d个问句（其中%d个问号）；%.4g/百字", s.Questions, s.QuestionMarks, qDensity), fmt.Sprintf("%.4g/百字，权重%d", questionTarget(b), qWeight), "同句问号与口语正反问只计一次；不把陈述句的嘛算问句", qDeduction},
		{"感叹号", fmt.Sprintf("%d个；%.4g/百字", s.Exclamations, eDensity), fmt.Sprintf("%.4g/百字，权重%d", b.Quant.Exclamations, eWeight), "期望少于2个时仅观察；其余按密度缺口扣分；仍只数感叹号，不从语气词推测", gap(eDensity, b.Quant.Exclamations, eWeight)},
	}
	add("情绪（问号＋感叹号）", 15, questionTarget(b) > 0 || b.Quant.Exclamations > 0, "问句节奏与强调标点代理指标；不代表实际声音情绪", checks)
	priceTask := containsAny(task, []string{"价格", "比价", "算账", "优惠", "省钱", "售价", "组合价"})
	priceApplicable := b.Identity.Numbers.Style != "" && b.Identity.Numbers.Style != "无价格" && (s.HasPrice || priceTask)
	checks = []Check{}
	if priceApplicable {
		deduction = 0
		if !s.HasPrice {
			deduction = 7
		}
		checks = append(checks, Check{"价格表达", strconv.FormatBool(s.HasPrice), b.Identity.Numbers.Style, "价格环节无价格数字扣7", deduction})
		if s.HasPrice && b.Identity.Numbers.LikesArithmetic {
			deduction = 0
			if !s.HasSaving {
				deduction = 3
			}
			checks = append(checks, Check{"算账表达", strconv.FormatBool(s.HasSaving), b.Identity.Numbers.Pattern, "基准爱算账而本段没有省/便宜/划算等动作扣3", deduction})
			deduction = 0
			if hesitations.MatchString(text) {
				deduction = 3
			}
			checks = append(checks, Check{"数字卡壳", hesitations.FindString(text), "顺畅表达", "优惠数字附近有省…/呃/啊对等显式卡壳标记扣3；不判断价格真假或算术事实", deduction})
		}
	}
	add("数字比价", 10, priceApplicable, "基准没有价格习惯，或本轮不是价格段且文本没有价格时不适用", checks)
	loops := 0
	for _, v := range s.Loops {
		loops += v.Count
	}
	deduction = 0
	if loops == 0 {
		deduction = 5
	} else if loops == 1 {
		if b.Identity.Loops.Strength == "高" {
			deduction = 3
		} else {
			deduction = 2
		}
	}
	add("重复回环", 10, len(s.Loops) > 0 && b.Identity.Loops.Strength != "低", "缺失/仅一次规则互斥；样本低回环不要求每段重复", []Check{{"基准回环短语", fmt.Sprintf("%d次", loops), strings.Join(b.Identity.Loops.Phrases, " / "), "没有扣5；只有1次扣2；高回环只有1次扣3（不叠加）", deduction}})
	cues := []string{"直接拍", "去拍", "下单", "点链接", "点进", "别错过", "买好了", "拍到了", "赶紧", "名额", "没得了"}
	for _, v := range s.Audience {
		if v.Count > 0 {
			cues = append(cues, v.Term)
		}
	}
	hasCue := containsAny(text, cues)
	deduction = 0
	if !hasCue {
		deduction = 2
	}
	checks = []Check{{"呼告／行动提示", strconv.FormatBool(hasCue), "至少一种", "无呼告、行动提示或稀缺表达扣2；呼告词来自本主播基准", deduction}}
	cta := []string{"直接拍", "去拍", "下单", "点链接", "点进", "赶紧", "别错过"}
	body := []rune(text)
	head := string(body[:len(body)/3])
	rest := string(body[len(body)/3:])
	deduction = 0
	if containsAny(head, cta) && !containsAny(rest, cta) {
		deduction = 2
	}
	checks = append(checks, Check{"行动提示位置", "前1/3：" + strconv.FormatBool(containsAny(head, cta)) + "；后2/3：" + strconv.FormatBool(containsAny(rest, cta)), "避免全部集中于开头", "行动提示仅在前1/3出现扣2（词面检测）", deduction})
	add("环节位置", 5, true, "位置规则为程序词面检测", checks)
	return r
}
func containsAny(text string, terms []string) bool {
	for _, term := range terms {
		if term != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}
