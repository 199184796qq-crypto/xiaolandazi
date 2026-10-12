package decisionexecutor

import (
	"regexp"
	"strings"
)

// 支持中英文单双引号，提取商家在"禁说"里明确列出的具体禁说词。
var factBoundaryQuotePattern = regexp.MustCompile(`['‘’"“”]([^'‘’"“”]{1,30})['‘’"“”]`)

// 提取"不能说/禁止说/禁说 XX"里的具体禁说词（到"这些/等/，/。"截止）。
// 故意不匹配"不要提"——"不要提XX"有歧义（可能是"此场景不主动提"，XX 未必是禁词），
// 而"不能说/禁说"是明确的禁词语义。
var factBoundaryProhibitPattern = regexp.MustCompile(`(?:禁止说|不能说|禁说|禁止)\s*([^，,。；;这些等]{1,30})`)

// 数字+量词归一化：把"2次加工"里的"2次"转成"二次"，避免阿拉伯数字写法漏检。
var factBoundaryDigitUnitPattern = regexp.MustCompile(`([0-9])(次|遍)`)

var chineseDigits = map[string]string{"0": "零", "1": "一", "2": "二", "3": "三", "4": "四", "5": "五", "6": "六", "7": "七", "8": "八", "9": "九"}

func normalizeDigitUnit(s string) string {
	return factBoundaryDigitUnitPattern.ReplaceAllStringFunc(s, func(m string) string {
		d := chineseDigits[m[0:1]]
		if d == "" {
			d = m[0:1]
		}
		return d + m[1:]
	})
}

// 事实边界分类：把商家填的自然语言"禁说"（flash 不好理解）翻译成标准禁词。
// flash 理解不了"不拿速度类比/不说到货时间"这类语义边界，会照样编"时效快"；
// 程序把关：识别边界类型，映射到字面可校验的标准禁词表，生成与审核都消费，
// 这样即使商家没列全具体禁词，程序也能兜住同一类编造。
type factBoundaryCategory struct {
	keywords []string // forbidden_wording 里出现这些词 → 判定属于此类边界
	terms    []string // 标准禁词表：flash 会编的、本类边界禁止的字面表达
}

var factBoundaryCategories = []factBoundaryCategory{
	{
		// 发货时效 / 到货承诺类：事实只确认了物流，flash 常把"有物流"编成"时效快/很快到"
		keywords: []string{"速度", "时效", "到货", "送达", "多久", "快慢", "到货时间", "收货时间", "发货时间", "到货日期"},
		terms: []string{
			"时效快", "时效很快", "物流很快", "快递很快", "最快", "极速", "很快就到",
			"当天就能到", "当天到", "次日达", "即日达", "今天下单明天到", "明天就能吃",
			"今天发明天到", "很快收到", "隔天就能到", "马上到", "飞速",
		},
	},
	{
		// 门店地域外推类：事实只写"五十多家门店"（都在本地），flash 常外推成"全国/遍布全国"
		keywords: []string{"全国", "遍布", "各地", "覆盖全"},
		terms: []string{
			"全国五十多家", "遍布全国", "全国各地", "全国都有门店", "全国门店",
			"门店遍布全国", "覆盖全国", "全国范围", "全国连锁",
		},
	},
}

// translateFactBoundary 把一条 forbidden_wording 翻译成标准禁词。
func translateFactBoundary(forbidden string) []string {
	result := make([]string, 0)
	for _, cat := range factBoundaryCategories {
		for _, kw := range cat.keywords {
			if strings.Contains(forbidden, kw) {
				result = append(result, cat.terms...)
				break
			}
		}
	}
	return result
}

// factBoundaryRiskTerms 从事实上下文里提取"禁说="边界，翻译成标准禁词 +
// 提取引号内商家明确列出的具体禁说词，作为动态硬校验边界。
func factBoundaryRiskTerms(contextText string) []string {
	result := make([]string, 0)
	for _, line := range strings.Split(contextText, "\n") {
		idx := strings.Index(line, "禁说=")
		if idx < 0 {
			continue
		}
		forbidden := line[idx+len("禁说="):]
		// 1. 分类翻译：把语义边界映射成标准禁词，程序把关不遗漏
		result = append(result, translateFactBoundary(forbidden)...)
		// 2. 引号内商家明确列出的具体禁说词
		for _, m := range factBoundaryQuotePattern.FindAllStringSubmatch(forbidden, -1) {
			if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
				result = append(result, strings.TrimSpace(m[1]))
			}
		}
		// 3. "不能说/禁说 XX"模式：提取具体禁词，并做数字归一化（2次→二次）
		for _, m := range factBoundaryProhibitPattern.FindAllStringSubmatch(forbidden, -1) {
			term := strings.TrimRight(strings.TrimSpace(m[1]), "这些等")
			term = normalizeDigitUnit(term)
			if term != "" {
				result = append(result, term)
			}
		}
	}
	return uniqueNonEmptyStrings(result)
}
