// Package fallbackline builds and checks the spoken one-liner used when the
// facts cannot answer. The model writes it in a rotating human tone; the program
// still owns the safety rules, so a funny line can never turn into a promise.
package fallbackline

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/factbound"
)

// Request describes one boundary line.
type Request struct {
	Class     string   // identity | no_dimension | partial_value
	Question  string   // viewer's original words
	Uncovered []string // values the facts do not cover
	Recent    []string // lines already spoken in this room, newest last
}

// Prompt returns the rotating tone, sentence pattern, rhetorical device, the
// system prompt and the user payload.
func Prompt(req Request) (string, string, string) {
	styles := factbound.BoundaryStyles()
	style := "直说型"
	if len(styles) > 0 {
		style = styles[rand.Intn(len(styles))]
	}
	patterns := factbound.BoundaryPatterns()
	pattern := "先接住再转折"
	if len(patterns) > 0 {
		pattern = patterns[rand.Intn(len(patterns))]
	}
	rhetorics := factbound.BoundaryRhetorics()
	rhetoric := "像跟熟人唠嗑"
	if len(rhetorics) > 0 {
		rhetoric = rhetorics[rand.Intn(len(rhetorics))]
	}
	var b strings.Builder
	b.WriteString("你是直播间的真人主播，正在回应弹幕。\n")
	b.WriteString("这次要回应的点，你手上没有确认信息，只能如实说“暂时说不好”，不能给结论。\n")
	b.WriteString("要求：\n")
	b.WriteString("- 只输出一句话，十五到四十个字，口语，像真人随口说的；\n")
	b.WriteString("- 句子不要太工整，可以有停顿、语气词、口头禅；\n")
	b.WriteString("- 越像跟熟人发语音越好：可以自嘲、可以夸张、可以损自己一句，但马上收住；\n")
	b.WriteString("- 不要成语堆砌、不要书面语、不要播音腔、不要一本正经解释；\n")
	b.WriteString("- 不要客服腔、不要官腔、不要播音腔，不要“亲”“尊敬的观众”；\n")
	b.WriteString("- 不要解释规则、系统、资料、页面，不要说“以实际为准”；\n")
	b.WriteString("- 不要编造名字、身份、数字、价格、时效、承诺；\n")
	b.WriteString("- 不要重复最近说过的类似说法。\n")
	if req.Class == "identity" {
		b.WriteString("- 观众在问主播的名字，主播不公开名字；可以让他喊“主播”，但绝对不能给自己起名字。\n")
	}
	if req.Class == "location" {
		b.WriteString("- 观众在问位置（店在哪、发货地、地址），你手上没有确认的位置信息；直接说位置这块还没核实，不要拿自己的记性开玩笑，也不要绕开。\n")
	}
	if req.Class == "partial_value" && len(req.Uncovered) > 0 {
		b.WriteString("- 一定要提到「" + strings.Join(req.Uncovered, "、") + "」，让观众知道是这一点没确认。\n")
		b.WriteString("- 但不要出现“包邮、免邮、能到、能发、送到”这些词，用“这个范围”“这块”来说。\n")
	}
	b.WriteString("这次的语气：")
	b.WriteString(style)
	b.WriteString("；句式：")
	b.WriteString(pattern)
	b.WriteString("；修辞：")
	b.WriteString(rhetoric)
	b.WriteString("。只输出这句话本身，不要引号，不要解释。")

	user := "观众问题：" + strings.TrimSpace(req.Question)
	if len(req.Uncovered) > 0 {
		user += "\n没确认的点：" + strings.Join(req.Uncovered, "、")
	}
	if len(req.Recent) > 0 {
		user += "\n最近说过、不要重复：" + strings.Join(req.Recent, " / ")
	}
	return style, b.String(), user
}

var digitPattern = regexp.MustCompile(`[0-9０-９]`)

var softHedges = []string{
	"没确认", "没核实", "不确定", "说不好", "不清楚", "不知道", "不敢", "拿不准",
	"没底", "没法确认", "确定不了", "先不说死", "别瞎猜", "等我弄清楚", "还没准话",
	"不敢乱说", "不敢乱讲", "等我问清楚", "没问到", "瞎说", "说不准", "没打听", "没消息",
}

// CandidatesPrompt asks for several different lines in one call, so the program
// can pick the best one instead of paying for retries.
func CandidatesPrompt(req Request, count int) (string, string) {
	_, system, user := Prompt(req)
	system += fmt.Sprintf("\n一次给%d句候选，四句切入点或句式必须明显不同，不要互相改写。"+
		"只输出严格 JSON：{\"候选\":[\"…\",\"…\",\"…\",\"…\"]}，数组长度%d，每句都是可直接播出的完整一句话。", count, count)
	return system, user
}

// ParseCandidates reads the candidate array out of the model answer.
func ParseCandidates(raw string) []string {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil
	}
	var doc struct {
		Candidates []string `json:"候选"`
	}
	if json.Unmarshal([]byte(raw[start:end+1]), &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Candidates))
	for _, item := range doc.Candidates {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Candidate is one model line with the program's verdict and score.
type Candidate struct {
	Text     string   `json:"text"`
	OK       bool     `json:"ok"`
	Reason   string   `json:"reason,omitempty"`
	Score    float64  `json:"score"`
	Features []string `json:"features,omitempty"`
}

// Human-likeness markers: they do not change safety, they decide which safe line
// sounds like a person rather than a script.
var (
	colloquialParticles = []string{"啊", "呀", "哈", "呢", "嘛", "呗", "哎", "啧", "哦", "啦", "咯", "嘿"}
	rhetoricalMarkers   = []string{"吗", "吧", "是不是", "咋", "难道", "你说", "对吧", "要不要"}
	metaphorMarkers     = []string{"像", "似的", "好比", "就跟", "跟问", "跟考试"}
	selfMockMarkers     = []string{"我这脑子", "记性", "嘴笨", "笨", "没记住", "挠头", "犯迷糊", "漏勺"}
	serviceToneMarkers  = []string{"亲", "尊敬", "为您", "请您", "感谢您的", "稍等", "马上"}
	humorMarkers        = []string{"哈哈", "笑死", "离谱", "绝了", "好家伙", "绷不住", "裂开", "拍胸脯", "嘴瓢", "整不会", "抓瞎", "两眼一黑", "怂", "翻车", "挠头"}
	writtenToneMarkers  = []string{"因此", "然而", "此外", "综上所述", "建议您", "特此", "敬请", "务必"}
)

func humanScore(text string) (float64, []string) {
	score := 0.0
	var features []string
	particles := 0
	for _, word := range colloquialParticles {
		if strings.Contains(text, word) {
			particles++
		}
	}
	if particles >= 2 {
		score += 0.3
		features = append(features, "口语词")
	} else if particles == 1 {
		score += 0.15
		features = append(features, "口语词")
	}
	if mentionsAny(text, rhetoricalMarkers) {
		score += 0.2
		features = append(features, "反问")
	}
	if mentionsAny(text, metaphorMarkers) {
		score += 0.25
		features = append(features, "比喻")
	}
	if mentionsAny(text, selfMockMarkers) {
		score += 0.2
		features = append(features, "自嘲")
	}
	if mentionsAny(text, serviceToneMarkers) {
		score -= 0.5
		features = append(features, "客服腔")
	}
	if mentionsAny(text, humorMarkers) {
		score += 0.25
		features = append(features, "有梗")
	}
	if mentionsAny(text, writtenToneMarkers) {
		score -= 0.3
		features = append(features, "书面语")
	}
	if strings.ContainsAny(text, "，…") && strings.Count(text, "，") >= 2 {
		score += 0.1
		features = append(features, "短句")
	}
	return score, features
}

// Evaluate runs the program checks once per candidate and scores the survivors.
func Evaluate(req Request, candidates []string) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		verdict := Candidate{Text: candidate}
		if err := Validate(candidate, req); err != nil {
			verdict.Reason = err.Error()
			out = append(out, verdict)
			continue
		}
		score := 1.0
		if req.Class == "partial_value" && mentionsAny(candidate, req.Uncovered) {
			score += 0.4
		}
		length := utf8.RuneCountInString(candidate)
		if length >= 16 && length <= 36 {
			score += 0.3
		} else if length < 10 || length > 50 {
			score -= 0.3
		}
		score -= 0.8 * maxSimilarity(candidate, req.Recent)
		if sharesOpening(candidate, req.Recent) {
			score -= 0.3
		}
		human, features := humanScore(candidate)
		score += human
		verdict.OK, verdict.Score, verdict.Features = true, score, features
		out = append(out, verdict)
	}
	return out
}

// Pick validates every candidate and returns the best one: safe, on-topic, close
// to the target length and as different as possible from what was just said.
func Pick(req Request, candidates []string) (string, error) {
	best, bestScore := "", -1.0
	for _, candidate := range Evaluate(req, candidates) {
		if !candidate.OK {
			continue
		}
		if candidate.Score > bestScore {
			best, bestScore = candidate.Text, candidate.Score
		}
	}
	if best == "" {
		return "", fmt.Errorf("没有合格候选")
	}
	return best, nil
}

func mentionsAny(text string, terms []string) bool {
	for _, term := range terms {
		if strings.TrimSpace(term) != "" && strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func maxSimilarity(text string, recent []string) float64 {
	max := 0.0
	for _, previous := range recent {
		if value := similarity(text, previous); value > max {
			max = value
		}
	}
	return max
}

func sharesOpening(text string, recent []string) bool {
	runes := []rune(text)
	if len(runes) < 3 {
		return false
	}
	opening := string(runes[:3])
	for _, previous := range recent {
		other := []rune(previous)
		if len(other) >= 3 && string(other[:3]) == opening {
			return true
		}
	}
	return false
}

// Validate rejects lines that promise, explain the system, invent a name or
// repeat a recent line.
func Validate(text string, req Request) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("空话术")
	}
	if strings.ContainsAny(text, "\n\r") {
		return fmt.Errorf("只能一句话")
	}
	runes := utf8.RuneCountInString(text)
	if runes < 6 || runes > 60 {
		return fmt.Errorf("长度不合适：%d", runes)
	}
	if digitPattern.MatchString(text) {
		return fmt.Errorf("不得出现数字")
	}
	if factbound.FabricatedIdentity(text) {
		return fmt.Errorf("编造身份")
	}
	for _, ban := range factbound.MetaBans() {
		if strings.Contains(text, ban) {
			return fmt.Errorf("元话语：%s", ban)
		}
	}
	if req.Class != "identity" && factbound.HasClaim(text) {
		return fmt.Errorf("出现承诺词")
	}
	hedged := false
	for _, hedge := range softHedges {
		if strings.Contains(text, hedge) {
			hedged = true
			break
		}
	}
	if !hedged {
		return fmt.Errorf("没有表达不确定")
	}
	if TooSimilar(text, req.Recent) {
		return fmt.Errorf("与最近说法太像")
	}
	return nil
}

// TooSimilar compares character bigrams against the recent lines.
func TooSimilar(text string, recent []string) bool {
	for _, previous := range recent {
		if strings.TrimSpace(previous) == text {
			return true
		}
		if similarity(text, previous) >= 0.6 {
			return true
		}
	}
	return false
}

func similarity(a, b string) float64 {	left, right := bigrams(a), bigrams(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	common := 0
	for gram := range left {
		if right[gram] {
			common++
		}
	}
	return 2 * float64(common) / float64(len(left)+len(right))
}

func bigrams(text string) map[string]bool {
	runes := []rune(text)
	out := map[string]bool{}
	for i := 0; i+1 < len(runes); i++ {
		out[string(runes[i:i+2])] = true
	}
	return out
}

// HumanizePrompt rewrites an already factual welcome in the same rotating human
// tone. Facts must survive: the program checks numbers afterwards.
func HumanizePrompt(text string) (string, string) {
	styles := factbound.BoundaryStyles()
	style := "像跟熟人唠嗑"
	if len(styles) > 0 {
		style = styles[rand.Intn(len(styles))]
	}
	patterns := factbound.BoundaryPatterns()
	pattern := "短句连打"
	if len(patterns) > 0 {
		pattern = patterns[rand.Intn(len(patterns))]
	}
	rhetorics := factbound.BoundaryRhetorics()
	rhetoric := "像跟熟人唠嗑"
	if len(rhetorics) > 0 {
		rhetoric = rhetorics[rand.Intn(len(rhetorics))]
	}
	system := "你是直播间的真人主播，把下面这段欢迎话改得口语、自然、有活人味。\n" +
		"要求：\n- 保留全部事实：品牌、商品、价格、福利、时效一个字都不能改，不能新增；\n" +
		"- 不许出现任何原文没有的数字；\n" +
		"- 不要客服腔、官腔、播音腔，不要“亲”“尊敬的观众”；\n" +
		"- 不要解释规则或系统；\n" +
		"- 可以拆句、加语气词、调整语序，但意思不变；\n" +
		"- 语气：" + style + "；句式：" + pattern + "；修辞：" + rhetoric + "。\n" +
		"只输出改写后的话本身，不要引号，不要解释。"
	user := "原文：\n" + strings.TrimSpace(text)
	return system, user
}

var numberPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// ValidateHumanized rejects a rewrite that invents numbers or drifts in length.
func ValidateHumanized(original, polished string) error {
	polished = strings.TrimSpace(polished)
	if polished == "" {
		return fmt.Errorf("空话术")
	}
	if utf8.RuneCountInString(polished) > utf8.RuneCountInString(original)*2+20 {
		return fmt.Errorf("改写过长")
	}
	allowed := map[string]bool{}
	for _, number := range numberPattern.FindAllString(original, -1) {
		allowed[number] = true
	}
	for _, number := range numberPattern.FindAllString(polished, -1) {
		if !allowed[number] {
			return fmt.Errorf("新增数字：%s", number)
		}
	}
	return nil
}

// Similar reports how alike two spoken lines are, for de-duplicating welcome parts.
func Similar(a, b string) float64 { return similarity(a, b) }
