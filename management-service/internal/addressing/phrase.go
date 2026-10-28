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
	People      []FormSet
	Terms       []string // 称谓词：老乡、朋友、家人们…
	GroupLabels []string // 集体称呼
	Style       string   // 热情 / 俏皮 / 接地气
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
	system := `你是直播间主播的称呼生成器。只输出一句可播的称呼话术，严格 JSON：{"address":"..."}。

规则：
1. 只能用给定的人名形式和集体称呼，不能编造名字、不能改字、不能加数字。
2. 多人时最多点 1-2 个人名，其余人用集体称呼或“你们”覆盖。
3. 单人时只叫这一个人，禁止出现“你们/几位/大家”或集体称呼，直接叫他/她。
4. 多人时最多点 1-2 个人名，其余人用集体称呼或“你们”覆盖。
5. 同一个称谓词不要出现两次（例如“梅梅朋友，几位朋友”）。
6. 结尾要落到集体称呼或“你们”，不要只说名字就结束。
7. 口语化、热情、接地气，不要书面腔；不超过 20 个字。
8. 不要输出解释、不要引号外的内容。

示例：
{"address":"小陈哥，你们来啦"}
{"address":"李老乡、王老乡，你们来了"}
{"address":"老乡们，你们来啦"}
{"address":"小斌老乡来啦"}`
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
		return fmt.Errorf("称呼话术没有使用给定称呼")
	}
	hasGroup := strings.Contains(address, "你们") || strings.Contains(address, "几位") || strings.Contains(address, "大家")
	for _, label := range in.GroupLabels {
		if label != "" && strings.Contains(address, label) {
			hasGroup = true
			break
		}
	}
	if in.Mode == "single" && hasGroup {
		return fmt.Errorf("单人称呼不应使用集体称呼")
	}
	if in.Mode != "single" && !hasGroup {
		return fmt.Errorf("多人称呼缺少集体称呼")
	}
	for _, term := range in.Terms {
		if term != "" && strings.Count(address, term) > 1 {
			return fmt.Errorf("称谓词重复：%s", term)
		}
	}
	return nil
}
