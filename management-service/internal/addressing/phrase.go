package addressing

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"unicode/utf8"
)

// FormSet is every spoken shape one named viewer may take.
type FormSet struct {
	Base  string
	Short string
	Forms []string
}

// PhraseInput is everything the address generator needs for one interaction.
type PhraseInput struct {
	Mode        string // single | multi | group
	Scene       string // greeting | answer
	People      []FormSet
	Terms       []string // 称谓词：老乡、朋友、家人们…
	GroupLabels []string // 集体称呼
	Style       string   // 热情 / 俏皮 / 接地气
	Recent      []string // 最近用过的话术，避免重复
}

// StyleHints are rotated so consecutive welcomes do not sound identical.
var StyleHints = []string{"热情", "接地气", "俏皮", "亲切自然"}

// PickStyle returns a rotating spoken style for this interaction.
func PickStyle(seed *rand.Rand) string {
	if seed == nil {
		return StyleHints[0]
	}
	return StyleHints[seed.Intn(len(StyleHints))]
}

// FormsFor expands base and short into the only addressable shapes.
func FormsFor(base, short string, terms []string) FormSet {
	base = strings.TrimSpace(base)
	short = strings.TrimSpace(short)
	forms := []string{}
	seen := map[string]bool{}
	add := func(form string) {
		form = strings.TrimSpace(form)
		if form == "" || seen[form] {
			return
		}
		seen[form] = true
		forms = append(forms, form)
	}
	add(base)
	add(short)
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		add(base + term)
		if short != base {
			add(short + term)
		}
	}
	return FormSet{Base: base, Short: short, Forms: forms}
}

// PhrasePrompt builds the one-shot address sentence request.
func PhrasePrompt(in PhraseInput, r *rand.Rand) (string, string) {
	style := strings.TrimSpace(in.Style)
	if style == "" {
		style = PickStyle(r)
	}
	title := "称呼话术"
	sceneRule := "口语化、热情、接地气，结尾落到集体称呼或“你们”，不超过 20 个字。"
	examples := `{"address":"小陈哥，你们来啦"}
{"address":"李老乡、王老乡，你们来了"}
{"address":"老乡们，你们来啦"}
{"address":"小斌老乡来啦"}`
	switch in.Scene {
	case "answer":
		title = "回答开头称呼"
		sceneRule = "不要出现问候词（来啦、来了、欢迎、哈喽），不超过 16 个字。"
		examples = `{"address":"小斌老乡"}
{"address":"小斌老乡、阿芳老乡，几位朋友"}
{"address":"这位朋友"}`
	case "thanks":
		title = "感谢开头称呼"
		sceneRule = "用于“谢谢…的点赞/关注”开头，不要出现问候词，不超过 24 个字。"
		examples = `{"address":"丽子老乡、心房客老乡，几位朋友"}
{"address":"丽子老乡"}
{"address":"几位朋友"}`
	}
	system := fmt.Sprintf(`你是直播间主播的%s生成器。只输出一个称呼，严格 JSON：{"address":"..."}。

规则（所有场景一致）：
1. 只能用给定的人名形式和集体称呼，不能编造、不能改字、不能加数字。
2. 单人时只叫这一个人，不出现集体称呼；多人时点名人数固定：有 2 人点 2 人，有 3 人及以上点 3 人，不得随机少点或多点；每个人名后面必须带同一个称谓（如“王卫成老乡、红豆老乡”），最后用集体称呼覆盖其他人。
3. 同一条里称谓词统一，每个点名的人都要带同一个称谓；昵称本身已带称谓（哥/姐/老板/师傅）时不再叠加；同一个人名不出现两次。
4. 昵称本身已带称谓（哥/姐/老板/师傅）时不要再叠加；性别称谓必须成对（哥姐、哥哥姐姐），禁止单独用“哥”或“姐”。
5. 只能用一个集体称呼，不要同时用“老乡们”和“几位朋友”。
6. %s
7. 不要与 recent 里给出的话术雷同，换个开头、句式和称谓组合。
8. 不要输出解释、不要引号外的内容。

示例：
%s`, title, sceneRule, examples)
	people := make([]map[string]any, 0, len(in.People))
	for _, person := range in.People {
		people = append(people, map[string]any{"base": person.Base, "short": person.Short, "forms": person.Forms})
	}
	payload, _ := json.Marshal(map[string]any{
		"mode":         in.Mode,
		"people":       people,
		"terms":        in.Terms,
		"group_labels": in.GroupLabels,
		"style":        style,
		"recent":       in.Recent,
	})
	return system, string(payload)
}

// ParsePhrase validates the generated address against the program's own forms.
func ParsePhrase(raw string, in PhraseInput) (string, error) {
	var wire struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal([]byte(stripJSONFence(raw)), &wire); err != nil {
		return "", fmt.Errorf("称呼话术返回格式错误: %w", err)
	}
	address := strings.TrimSpace(wire.Address)
	if err := ValidatePhrase(address, in); err != nil {
		return "", err
	}
	return address, nil
}

// ValidatePhrase rejects invented names, stray digits and name-only answers.
func ValidatePhrase(address string, in PhraseInput) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("称呼话术为空")
	}
	length := utf8.RuneCountInString(address)
	if length < 2 || length > 24 {
		return fmt.Errorf("称呼话术长度 %d 不合适", length)
	}
	if hasDigit(address) {
		return fmt.Errorf("称呼话术含数字")
	}
	if in.Scene == "answer" {
		return validateAnswerAddress(address, in)
	}
	allowed := map[string]bool{}
	for _, label := range in.GroupLabels {
		if label = strings.TrimSpace(label); label != "" {
			allowed[label] = true
		}
	}
	for _, person := range in.People {
		for _, form := range person.Forms {
			allowed[form] = true
		}
	}
	named := false
	for form := range allowed {
		if strings.Contains(address, form) {
			named = true
			break
		}
	}
	if !named {
		for _, noun := range genericGroupNouns {
			if strings.Contains(address, noun) {
				named = true
				break
			}
		}
	}
	if !named {
		return fmt.Errorf("称呼话术没有使用给定称呼")
	}
	hasGroup := phraseHasGroup(address, in)
	if in.Mode == "single" && hasGroup {
		return fmt.Errorf("单人称呼不应使用集体称呼")
	}
	if in.Mode != "single" && !hasGroup {
		return fmt.Errorf("多人称呼缺少集体称呼")
	}
	if in.Mode != "single" {
		mentioned := 0
		for _, person := range in.People {
			matched := ""
			for _, form := range person.Forms {
				if form != "" && strings.Contains(address, form) && len([]rune(form)) > len([]rune(matched)) {
					matched = form
				}
			}
			if matched == "" {
				continue
			}
			mentioned++
			hasTerm := false
			for _, term := range in.Terms {
				if term != "" && strings.HasSuffix(matched, term) {
					hasTerm = true
					break
				}
			}
			if !hasTerm && !containsHonorific(matched) {
				return fmt.Errorf("多人称呼中每个人名后必须带统一称谓: %s", matched)
			}
		}
		expected := len(in.People)
		if expected > 3 {
			expected = 3
		}
		if expected > 0 && mentioned != expected {
			return fmt.Errorf("多人称呼点名人数应为 %d，实际 %d", expected, mentioned)
		}
	}
	if err := validateGenderPairs(address, in); err != nil {
		return err
	}
	if err := validateNoRepeats(address, in); err != nil {
		return err
	}
	return nil
}

// genericGroupNouns are collective words the model may use instead of a
// configured label.
var genericGroupNouns = []string{"你们", "大家", "几位", "老乡们", "朋友们", "家人们", "各位", "宝子们"}

func phraseHasGroup(address string, in PhraseInput) bool {
	for _, noun := range genericGroupNouns {
		if strings.Contains(address, noun) {
			return true
		}
	}
	for _, label := range in.GroupLabels {
		if label != "" && strings.Contains(address, label) {
			return true
		}
	}
	return false
}

// FallbackPhrase is used when the model is unavailable or its answer fails
// validation; it still produces a speakable address.
func FallbackPhrase(in PhraseInput) string {
	if in.Scene == "answer" {
		group := "这位朋友"
		if len(in.People) > 0 {
			return in.People[0].Base
		}
		return group
	}
	group := ""
	if len(in.GroupLabels) > 0 {
		group = in.GroupLabels[0]
	}
	if group == "" {
		group = "几位朋友"
	}
	switch in.Mode {
	case "single":
		if len(in.People) > 0 {
			return in.People[0].Base + "来啦"
		}
		return group + "来啦"
	default:
		if len(in.People) == 0 {
			return group + "，你们来啦"
		}
		names := make([]string, 0, 2)
		for i, person := range in.People {
			if i >= 2 {
				break
			}
			names = append(names, person.Base)
		}
		return strings.Join(names, "、") + "，" + group + "来啦"
	}
}

// validateAnswerAddress covers answer/thanks openings: no greeting words, one
// person when alone, 1-2 names plus a collective when several people speak.
func validateAnswerAddress(address string, in PhraseInput) error {
	for _, word := range []string{"来啦", "来了", "欢迎", "哈喽"} {
		if strings.Contains(address, word) {
			return fmt.Errorf("称呼不应含问候词：%s", word)
		}
	}
	if utf8.RuneCountInString(address) > 20 {
		return fmt.Errorf("称呼过长")
	}
	if len(in.People) <= 1 {
		if phraseHasGroup(address, in) {
			return fmt.Errorf("单人称呼不应使用集体称呼")
		}
		for _, person := range in.People {
			for _, form := range person.Forms {
				if form != "" && strings.Contains(address, form) {
					return nil
				}
			}
		}
		return fmt.Errorf("称呼没有使用提问者名字")
	}
	if !phraseHasGroup(address, in) {
		return fmt.Errorf("多人称呼缺少集体称呼")
	}
	return nil
}

// validateNoRepeats rejects the same name twice and stacked group labels.
func containsHonorific(text string) bool {
	for _, term := range []string{"哥", "姐", "老板", "师傅", "老师"} {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func validateNoRepeats(address string, in PhraseInput) error {
	for _, person := range in.People {
		for _, form := range person.Forms {
			if form != "" && strings.Count(address, form) > 1 {
				return fmt.Errorf("称呼重复：%s", form)
			}
		}
	}
	labels := 0
	for _, label := range in.GroupLabels {
		if label != "" && strings.Contains(address, label) {
			labels++
		}
	}
	if labels > 1 {
		return fmt.Errorf("集体称呼叠加")
	}
	return nil
}

// validateGenderPairs rejects a lone 哥/姐/弟/妹: when the title does not come
// from the person's own name, both genders must be addressed together (哥姐).
func validateGenderPairs(address string, in PhraseInput) error {
	pairs := map[rune]rune{'哥': '姐', '姐': '哥', '弟': '妹', '妹': '弟'}
	for _, ch := range address {
		counterpart, ok := pairs[ch]
		if !ok {
			continue
		}
		if strings.ContainsRune(address, counterpart) || nameCarries(address, ch, in) {
			continue
		}
		return fmt.Errorf("性别称谓必须成对出现：%c", ch)
	}
	return nil
}

// nameCarries reports whether this title is part of a person's own provided
// form, e.g. the nickname 王哥, so it is not a generic gender title.
func nameCarries(address string, title rune, in PhraseInput) bool {
	for _, person := range in.People {
		for _, form := range person.Forms {
			if strings.ContainsRune(form, title) && strings.Contains(address, form) {
				return true
			}
		}
	}
	return false
}
