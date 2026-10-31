// Package planprompt separates editable business content from its two compiled uses.
package planprompt

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const MaxPromptRunes = 20000

type Sections struct {
	PersonaMD                 string `json:"persona_md"`
	FactsMD                   string `json:"facts_md"`
	SpeechRequirementsMD      string `json:"speech_requirements_md"`
	InteractionRequirementsMD string `json:"interaction_requirements_md"`
}

func clean(text string) string {
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "〔提示〕") {
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s Sections) Normalize() Sections {
	return Sections{clean(s.PersonaMD), clean(s.FactsMD), clean(s.SpeechRequirementsMD), clean(s.InteractionRequirementsMD)}
}

func compose(blocks ...string) string {
	var result []string
	for i := 0; i < len(blocks); i += 2 {
		if value := strings.TrimSpace(blocks[i+1]); value != "" {
			result = append(result, "【"+blocks[i]+"】\n"+value)
		}
	}
	return strings.Join(result, "\n\n")
}

func (s Sections) Interaction() string {
	s = s.Normalize()
	return compose("主播人设", s.PersonaMD, "事实依据", s.FactsMD, "互动回答要求", s.InteractionRequirementsMD)
}

func (s Sections) Speech() string {
	s = s.Normalize()
	return compose("主播人设", s.PersonaMD, "事实依据", s.FactsMD, "直播话术要求", s.SpeechRequirementsMD)
}

func (s Sections) Empty() bool {
	return strings.TrimSpace(s.PersonaMD+s.FactsMD+s.SpeechRequirementsMD+s.InteractionRequirementsMD) == ""
}

func (s Sections) Validate() error {
	if utf8.RuneCountInString(s.Interaction()) > MaxPromptRunes || utf8.RuneCountInString(s.Speech()) > MaxPromptRunes {
		return errors.New("合成后的每份元提示词不能超过20000字，请缩短对应文本框内容")
	}
	return nil
}

// SplitLegacy is available for explicit legacy migration, never editor readback.
// Unrecognised content stays with its current section, including unknown headings.
// Completely free-form legacy prompts remain intact in interaction requirements.
func SplitLegacy(text string) (Sections, bool) {
	groups := [4][]string{}
	target, recognised := 3, false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		title := strings.TrimSpace(line)
		if strings.HasPrefix(title, "【") && strings.HasSuffix(title, "】") {
			title = strings.TrimSuffix(strings.TrimPrefix(title, "【"), "】")
		} else if strings.HasPrefix(title, "#") {
			title = strings.TrimSpace(strings.TrimLeft(title, "#"))
		} else {
			title = ""
		}
		next := -1
		switch title {
		case "主播人设", "身份设定", "核心定位", "性格 & 语气设定", "性格与语气设定", "数据使用规则", "称呼规则", "禁忌边界":
			next = 0
		case "商品卡", "活动福利", "事实依据", "用词消歧":
			next = 1
		case "直播话术要求", "直播目标", "话术规则":
			next = 2
		case "互动回答要求", "互动约束", "输出格式要求", "输出格式":
			next = 3
		}
		if next >= 0 {
			target, recognised = next, true
			if title == "主播人设" || title == "事实依据" || title == "直播话术要求" || title == "互动回答要求" {
				continue
			}
		}
		groups[target] = append(groups[target], line)
	}
	return (Sections{strings.Join(groups[0], "\n"), strings.Join(groups[1], "\n"), strings.Join(groups[2], "\n"), strings.Join(groups[3], "\n")}).Normalize(), !recognised && strings.TrimSpace(text) != ""
}
