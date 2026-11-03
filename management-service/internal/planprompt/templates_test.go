package planprompt

import (
	"strings"
	"testing"
)

func TestTemplatesOriginalsRevisionAndEmptyOverrides(t *testing.T) {
	base := Templates{Sections: Sections{PersonaMD: "默认人设", FactsMD: "默认事实", RedLinesMD: "默认红线", SpeechRequirementsMD: "默认主线", InteractionRequirementsMD: "默认互动"}, RewritePlanningMD: "默认规划"}
	value := "  手动原文\n{{已有开场}}\n😀  "
	next := TemplatesWithOverrides(base, map[string]string{"persona_md": "", "rewrite_planning_md": value})
	if next.PersonaMD != "" || next.RewritePlanningMD != value || next.FactsMD != base.FactsMD {
		t.Fatal("missing and empty values or originals were conflated", next)
	}
	if next.Revision() == base.Revision() || next.Revision() != TemplatesWithOverrides(base, next.Values()).Revision() {
		t.Fatal("revision must represent the six exact originals")
	}
	for _, key := range TemplateKeys {
		if TemplatesWithOverrides(base, map[string]string{key: "changed"}).Revision() == base.Revision() {
			t.Fatal("revision ignored", key)
		}
	}
	if err := next.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestTemplatesRejectStorageAndCombinedOverflow(t *testing.T) {
	for _, content := range []string{strings.Repeat("字", MaxPromptRunes+1), strings.Repeat("😀", 16000)} {
		if err := (Templates{RewritePlanningMD: content}).Validate(); err == nil {
			t.Fatal("oversize template accepted")
		}
	}
	if err := (Templates{Sections: Sections{PersonaMD: strings.Repeat("a", 11000), FactsMD: strings.Repeat("b", 11000)}}).Validate(); err == nil {
		t.Fatal("unusable combined prompt accepted")
	}
}
