package decisionexecutor

import (
	"encoding/json"
	"strings"
	"testing"
)

func interactionAnswerFixture(angle, reply string) string {
	raw, _ := json.Marshal(map[string]any{"reply": reply, "answer_outline": InteractionAnswerOutline{Purpose: "确认保存方式", Angle: angle, Aspects: []string{"是否需要冷藏", "包装对保存的影响"}, Avoid: []string{"复热吃法", "未经确认的保质天数"}}})
	return string(raw)
}

func TestInteractionAnswerMetadataFailureRetainsOnlyValidSpeech(t *testing.T) {
	valid := interactionAnswerFixture("保存环境", "需要冷藏。")
	for _, raw := range []string{
		`{"reply":"需要冷藏。"}`,
		strings.Replace(valid, `"purpose":`, `"fact_ids":[],"purpose":`, 1),
		strings.Replace(valid, `"aspects":["是否需要冷藏","包装对保存的影响"]`, `"aspects":null`, 1),
	} {
		reply, outline, warning, err := decodeInteractionAnswer(raw)
		if err != nil || reply != "需要冷藏。" || outline != nil || warning == "" {
			t.Fatal("invalid advisory vetoed speech or became a fake plan", reply, outline, warning, err)
		}
	}
	for _, raw := range []string{`{"reply":null}`, valid + `{}`, `{"reply":"未闭合`} {
		if _, _, _, err := decodeInteractionAnswer(raw); err == nil {
			t.Fatal("invalid reply transport accepted", raw)
		}
	}
	if reply, outline, warning, err := decodeInteractionAnswer("旧协议正文。"); err != nil || reply != "旧协议正文。" || outline != nil || warning == "" {
		t.Fatal(reply, outline, warning, err)
	}
}

func TestInteractionRecentAnswersAreBounded(t *testing.T) {
	if ValidateInteractionRecentAnswers(make([]InteractionRecentAnswer, 9)) == nil || ValidateInteractionRecentAnswers([]InteractionRecentAnswer{{Question: ""}}) == nil || ValidateInteractionRecentAnswers([]InteractionRecentAnswer{{Question: "问", Reply: strings.Repeat("字", 1001)}}) == nil {
		t.Fatal("unbounded or empty history accepted")
	}
	if err := ValidateInteractionRecentAnswers([]InteractionRecentAnswer{{Question: "放冰箱不", Purpose: "确认保存", Angle: "包装条件", Reply: "需要冷藏。"}}); err != nil {
		t.Fatal(err)
	}
}

func TestInteractionBusinessPlanRejectsModelOwnedRelations(t *testing.T) {
	valid := `{"answer_outline":{"purpose":"确认保存方式","angle":"存放环境","aspects":["是否冷藏"],"avoid":["复热吃法"]},"basis":["需要冷藏。"],"requirements":[]}`
	if _, err := decodeInteractionBusinessPlan(valid); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{strings.Replace(valid, `"basis":`, `"fact_ids":["F001"],"basis":`, 1), strings.Replace(valid, `"requirements":[]`, `"requirements":null`, 1), strings.Replace(valid, `"aspects":["是否冷藏"]`, `"aspects":[]`, 1)} {
		if _, err := decodeInteractionBusinessPlan(raw); err == nil {
			t.Fatal("invalid planner contract accepted", raw)
		}
	}
}

func TestInteractionBusinessPlanKeepsValidPrimaryWhenOptionalCandidateIsMalformed(t *testing.T) {
	// Replay the actual provider's invalid optional placeholder; it must not
	// discard the valid business plan or pretend the optional task completed.
	raw := `{"answer_outline":{"purpose":"确认保存方式","angle":"存放环境","aspects":["是否冷藏"],"avoid":["复热吃法"]},"basis":["收到后冷藏。"],"requirements":[],"alternatives":[{"answer_outline":{"useful":false,"data":null}}]}`
	plan, err := decodeInteractionBusinessPlan(raw)
	if err != nil || plan == nil || plan.Outline.Angle != "存放环境" || len(plan.Basis) != 1 || len(plan.Alternatives) != 0 || plan.FormatWarning == "" {
		t.Fatal("optional format error discarded valid plan or hid incomplete output", plan, err)
	}
}

func TestInteractionPlanChoosesOtherSupportedAngle(t *testing.T) {
	plan := &interactionBusinessPlan{Outline: InteractionAnswerOutline{Purpose: "确认保存", Angle: "存放环境"}, Basis: []string{"收到后冷藏"}, Alternatives: []interactionPlanCandidate{{Outline: InteractionAnswerOutline{Purpose: "确认保存", Angle: "包装允许的保存条件"}, Basis: []string{"不同包装的保存条件"}}}}
	selectInteractionPlanAngle(plan, []string{"存放环境"})
	if plan.Outline.Angle != "包装允许的保存条件" || len(plan.Basis) != 1 || plan.Basis[0] != "不同包装的保存条件" || plan.Alternatives != nil {
		t.Fatal("only words changed instead of selecting another content task", plan)
	}
	selectInteractionPlanAngle(plan, []string{"包装允许的保存条件"})
	if plan.Outline.Angle != "包装允许的保存条件" {
		t.Fatal("single valid angle should retain correct answer")
	}
}
