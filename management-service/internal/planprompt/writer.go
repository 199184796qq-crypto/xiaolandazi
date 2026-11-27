package planprompt

import (
	"regexp"
	"strings"
)

// WriterPage 是一份可生成的提示词页。事实依据不在此列：事实必须由程序渲染，
// 模型只允许做排版整理，不允许生成。
type WriterPage struct {
	Key     string // settings 里的键名后缀
	Section string // planprompt.Sections 里的字段名
	Title   string // 给模型看的页名
	MinLen  int
	MaxLen  int
}

// WriterPages 顺序与前端六个文本框一致（事实依据除外）。
var WriterPages = []WriterPage{
	{Key: "persona", Section: "persona_md", Title: "主播人设", MinLen: 120, MaxLen: 400},
	{Key: "red_lines", Section: "red_lines_md", Title: "红线禁止", MinLen: 120, MaxLen: 600},
	{Key: "speech", Section: "speech_requirements_md", Title: "直播话术要求", MinLen: 100, MaxLen: 400},
	{Key: "interaction", Section: "interaction_requirements_md", Title: "互动回答要求", MinLen: 120, MaxLen: 600},
	{Key: "planning", Section: "rewrite_planning_md", Title: "话术规划", MinLen: 100, MaxLen: 400},
}

// WriterPlaceholders 是写手母版允许出现的占位符白名单。保存母版时用它校验，
// 生成时用它判断是否有未替换的占位。
var WriterPlaceholders = []string{
	"{{方案名称}}", "{{主播名}}", "{{主播身份}}", "{{直播赛道}}", "{{业务模式}}",
	"{{商品卡}}", "{{活动福利}}", "{{有效事实}}", "{{本行业事实类}}", "{{现有提示词}}", "{{本页要求}}",
}

var writerPlaceholderPattern = regexp.MustCompile(`\{\{[^{}]{1,20}\}\}`)

// FillWriter 替换白名单内的占位符。未知占位符原样保留，交由 AuditWriterMaster 拒绝，
// 绝不因为一个占位符丢整行——这是旧模板渲染踩过的坑。
func FillWriter(master string, values map[string]string) string {
	out := master
	for _, key := range WriterPlaceholders {
		if v, ok := values[strings.TrimSuffix(strings.TrimPrefix(key, "{{"), "}}")]; ok {
			out = strings.ReplaceAll(out, key, v)
		}
	}
	return out
}

// UnknownWriterPlaceholders 返回母版里不在白名单的占位符，用于保存时拒绝。
func UnknownWriterPlaceholders(master string) []string {
	var bad []string
	seen := map[string]bool{}
	for _, m := range writerPlaceholderPattern.FindAllString(master, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		ok := false
		for _, allowed := range WriterPlaceholders {
			if m == allowed {
				ok = true
				break
			}
		}
		if !ok {
			bad = append(bad, m)
		}
	}
	return bad
}

var writerFence = regexp.MustCompile("(?s)^\\s*```[a-zA-Z]*\\s*\n(.*?)\n?```\\s*$")

// NormalizeWriterOutput 去掉模型常见的包装：代码围栏、开场解释、末尾废话。
func NormalizeWriterOutput(text string) string {
	out := strings.TrimSpace(text)
	if m := writerFence.FindStringSubmatch(out); len(m) == 2 {
		out = strings.TrimSpace(m[1])
	}
	titles := []string{"主播人设", "红线禁止", "直播话术要求", "互动回答要求", "话术规划"}
	if hasAnyHeading(out, titles) {
		// 有分节标题时，标题之前的一切都是寒暄，直接丢掉。
		lines := strings.Split(out, "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "【") {
				out = strings.TrimSpace(strings.Join(lines[i:], "\n"))
				break
			}
		}
	} else {
		// 没有分节标题时，裁掉第一行的寒暄，例如"好的，以下是……"。
		lines := strings.Split(out, "\n")
		if len(lines) > 1 && (strings.HasPrefix(lines[0], "好的") || strings.Contains(lines[0], "以下是") || strings.HasSuffix(strings.TrimSpace(lines[0]), "：")) {
			out = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
	}
	return strings.TrimSpace(out)
}

func hasAnyHeading(text string, titles []string) bool {
	for _, t := range titles {
		if strings.Contains(text, "【"+t+"】") {
			return true
		}
	}
	return false
}

// writerInternalTerms 是不能出现在人设/要求类正文里的元语言。模型偶尔会把
// 提示词机制写进人设（"称呼交给系统处理"），这类内容进播出会出戏。
var writerInternalTerms = []string{"系统", "后置", "播出", "后台", "字段", "流程", "商品卡", "链接页"}

var writerAddressTerms = []string{"老乡们", "家人们", "朋友们", "宝宝", "各位"}

var writerNumberPattern = regexp.MustCompile(`\d`)

// AuditWriterOutput 检查模型返回。fatal 为 true 时不采用模型结果，回退母版渲染。
func AuditWriterOutput(page string, text string) (fatal bool, warnings []string) {
	runes := []rune(text)
	var spec *WriterPage
	for i := range WriterPages {
		if WriterPages[i].Key == page {
			spec = &WriterPages[i]
			break
		}
	}
	if spec != nil {
		if len(runes) < spec.MinLen {
			warnings = append(warnings, "内容偏短，建议人工补充")
		}
		if len(runes) > spec.MaxLen*2 {
			return true, append(warnings, "内容超长，疑似模型跑题")
		}
	}
	for _, term := range writerInternalTerms {
		if strings.Contains(text, term) {
			if page == "persona" {
				return true, append(warnings, "出现内部术语“"+term+"”")
			}
			warnings = append(warnings, "出现内部术语“"+term+"”")
		}
	}
	if page == "persona" {
		for _, term := range writerAddressTerms {
			if strings.Contains(text, term) {
				warnings = append(warnings, "出现称呼词“"+term+"”")
			}
		}
		if writerNumberPattern.MatchString(text) {
			warnings = append(warnings, "出现数字，人设里通常不该有具体数值")
		}
	}
	return false, warnings
}

// WriterOutputRule 是程序追加在母版末尾的输出要求。母版可以只写"要什么"，
// 输出形态由程序统一规定，避免每份母版各写一套格式。
func WriterOutputRule(title string) string {
	return "【输出要求】\n上面的内容是写作要求，不是要输出的内容。" +
		"请据此独立写出一份「" + title + "」，第一行用【" + title + "】开头。" +
		"不要复述任何要求、标题或说明，不要解释，不要输出自检过程。"
}

// CleanWriterOutput 清理模型输出里的"指令残留"。
//
// 模型偶尔会把母版里的说明行一起吐回来。整页丢弃会让用户看到母版原文，
// 所以这里按行剥离：说明行删掉，生成出来的分节正文保留。
func CleanWriterOutput(page, text string) (string, []string, bool) {
	var notes []string
	// 模型复制母版时，通常把要求段整块堆在正文后面：从这些标题起直接截断。
	for _, cut := range []string{"\n【硬性约束】", "\n【本页要求】", "\n【输出要求】", "\n【自检】", "\n【方案资料】", "\n【输出】"} {
		if i := strings.Index(text, cut); i >= 0 {
			text = text[:i]
			notes = append(notes, "已截断模型复述的要求段落")
			break
		}
	}
	instructionPrefixes := []string{
		"你是直播方案提示词撰写人", "【方案资料】", "【本页要求】", "【输出要求】",
		"【输出】", "【自检】", "【硬性约束】", "按下面", "只输出正文", "不要输出",
	}
	kept := make([]string, 0, 32)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			kept = append(kept, line)
			continue
		}
		skip := strings.Contains(trimmed, "{{")
		for _, prefix := range instructionPrefixes {
			if strings.HasPrefix(trimmed, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			for _, term := range writerInternalTerms {
				if strings.Contains(trimmed, term) {
					skip = true
					notes = append(notes, "已删去含内部用语的句子（"+term+"）")
					break
				}
			}
		}
		if page == "persona" && !skip {
			// 只删"称呼规则"这类元语言，正文里出现的称呼另行告警。
			if strings.Contains(trimmed, "称呼") && !strings.Contains(trimmed, "不出现") && !strings.Contains(trimmed, "不写") {
				for _, term := range writerAddressTerms {
					if strings.Contains(trimmed, term) {
						skip = true
						notes = append(notes, "已删去含称呼词的规则句（"+term+"）")
						break
					}
				}
			}
		}
		if skip && !strings.HasPrefix(trimmed, "【") {
			notes = append(notes, "")
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if !strings.Contains(out, "【") {
		return "", []string{"模型没有返回分节正文"}, true
	}
	min := 0
	for _, spec := range WriterPages {
		if spec.Key == page {
			min = spec.MinLen
		}
	}
	if min > 0 && len([]rune(out)) < min/2 {
		return "", []string{"清理后内容过短"}, true
	}
	notes = compactNotes(notes)
	return out, notes, false
}

// compactNotes 去重并去掉空串，避免同一条告警重复出现。
func compactNotes(notes []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(notes))
	for _, n := range notes {
		if strings.TrimSpace(n) == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// IndustryFactFields 给出某个行业属于"可核对事实"的字段提醒。认不出的行业返回
// 最宽的一组，宁可比实际更宽，也不要漏。
func IndustryFactFields(domain, industry string) string {
	byIndustry := map[string]string{
		"food":          "价格、克重、规格、保质期、产地、配料、工艺、储存方式",
		"apparel":       "价格、尺码、面料成分、产地、洗涤方式、版型",
		"beauty":        "价格、容量、成分、备案号、适用肤质、保质期",
		"digital":       "价格、型号、参数、保修期、配件清单",
		"baby":          "价格、适用年龄、材质、认证、保质期",
		"home":          "价格、尺寸、材质、承重、安装方式",
		"local_service": "门店地址、营业时间、套餐内容、有效期、预约规则、使用限制",
	}
	if v, ok := byIndustry[strings.ToLower(strings.TrimSpace(industry))]; ok {
		return v
	}
	if v, ok := byIndustry[strings.ToLower(strings.TrimSpace(domain))]; ok {
		return v
	}
	return "价格、数量、时间、地点、资质、参数、承诺"
}
