package planprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Templates stores originals; rendering never mutates these platform defaults.
type Templates struct {
	Sections
	RewritePlanningMD string `json:"rewrite_planning_md"`
}

var TemplateKeys = []string{"persona_md", "facts_md", "red_lines_md", "speech_requirements_md", "interaction_requirements_md", "rewrite_planning_md"}

func (t Templates) Values() map[string]string {
	return map[string]string{
		"persona_md": t.PersonaMD, "facts_md": t.FactsMD, "red_lines_md": t.RedLinesMD,
		"speech_requirements_md": t.SpeechRequirementsMD, "interaction_requirements_md": t.InteractionRequirementsMD,
		"rewrite_planning_md": t.RewritePlanningMD,
	}
}

// Missing overrides use the installed mother; an explicitly empty value stays empty.
func TemplatesWithOverrides(defaults Templates, overrides map[string]string) Templates {
	values := defaults.Values()
	for _, key := range TemplateKeys {
		if value, ok := overrides[key]; ok {
			values[key] = value
		}
	}
	return Templates{Sections: Sections{
		PersonaMD: values["persona_md"], FactsMD: values["facts_md"], RedLinesMD: values["red_lines_md"],
		SpeechRequirementsMD: values["speech_requirements_md"], InteractionRequirementsMD: values["interaction_requirements_md"],
	}, RewritePlanningMD: values["rewrite_planning_md"]}
}

func (t Templates) Revision() string {
	data, _ := json.Marshal(t)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (t Templates) Validate() error {
	for key, value := range t.Values() {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxPromptRunes || len(value) > 60000 {
			return fmt.Errorf("%s 母版过长，每份最多20000字且不超过60000字节", key)
		}
	}
	if err := t.Sections.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(CompileRewritePlanning(t.Sections, t.RewritePlanningMD)) > MaxPromptRunes {
		return errors.New("合成后的话术修改元提示词不能超过20000字，请缩短母版")
	}
	return nil
}
