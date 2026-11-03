package addressing

import (
	"math/rand"
	"strings"
	"unicode"
)

// 本文件补齐称呼层的三条业务规则：
//   1. 个体称谓去「们」，群体专用词不能连写；
//   2. 纯英文昵称的可念性判定，不可念时用首字母；
//   3. 裸称与带称谓的骰子比例。
// 规则只处理字符串，不涉及模型与缓存。

// 群体专用词：本身是复数，不能与昵称连写，只能单独出现或用逗号隔开。
var groupOnlyTerms = map[string]bool{
	"各位": true, "大家": true, "大伙": true, "诸位": true,
}

// 职业敬称：与亲属称谓一样，昵称结尾带这些时不再叠加称谓。
var professionSuffixes = []string{
	"老板", "老师", "师傅", "大夫", "先生", "女士", "经理", "主任", "总",
}

// SingularTerm 去掉称谓末尾的「们」，用于接在单个昵称后面。
func SingularTerm(term string) string {
	term = strings.TrimSpace(term)
	return strings.TrimSuffix(term, "们")
}

// IsGroupOnlyTerm 判断是否只能用于群体的称谓词。
func IsGroupOnlyTerm(term string) bool {
	return groupOnlyTerms[strings.TrimSpace(term)]
}

// HasAddressSuffix 判断昵称结尾是否已经带了称谓（亲属或职业）。
func HasAddressSuffix(name string) bool {
	if HasKinshipSuffix(name) {
		return true
	}
	name = strings.TrimSpace(name)
	for _, suffix := range professionSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// SpeakableLatin 判断一串英文字母能否自然念出来。
// 判据：至少一个元音（a/e/i/o/u），且最长连续辅音不超过 3 个。y 按辅音算。
func SpeakableLatin(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	hasVowel := false
	run, longest := 0, 0
	for _, r := range strings.ToLower(name) {
		if r < 'a' || r > 'z' {
			return false
		}
		switch r {
		case 'a', 'e', 'i', 'o', 'u':
			hasVowel = true
			run = 0
		default:
			run++
			if run > longest {
				longest = run
			}
		}
	}
	return hasVowel && longest <= 3
}

// allLatinLetters 判断是否全部由英文字母组成。
func allLatinLetters(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// InitialAddress 取英文昵称首字母（大写），用于不可念的随机字母组合。
func InitialAddress(name string) string {
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsLetter(r) && r <= unicode.MaxASCII {
			return strings.ToUpper(string(r))
		}
	}
	return ""
}

// normalizeLatinAddress 把不可念的纯字母昵称换成首字母；其它原样返回。
func normalizeLatinAddress(name string) string {
	name = strings.TrimSpace(name)
	if !allLatinLetters(name) || SpeakableLatin(name) {
		return name
	}
	if initial := InitialAddress(name); initial != "" {
		return initial
	}
	return name
}

// WithTitle 按骰子决定这一次是否给个体加称谓。
// 默认裸称 40%、带称谓 60%；r 为空时按裸称处理。
func WithTitle(r *rand.Rand) bool {
	if r == nil {
		return false
	}
	return r.Intn(100) >= 40
}

// WorthAsking 判断灰色昵称是否值得交给模型判定：
// 去掉英文和数字后还剩至少两个汉字，才有救回来的价值。
// 纯符号、纯字母、纯数字这类直接走群体词，省一次调用。
func WorthAsking(name string) bool {
	han := 0
	for _, r := range strings.TrimSpace(name) {
		if unicode.Is(unicode.Han, r) {
			han++
		}
	}
	return han >= 2
}
