package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"strings"
)

type factPreviewRepairKey struct{}

func factSpeechRepairMessages(state *anchorcycle.State, previous map[string]any) ([]agentgateway.Message, error) {
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_FACT_SPEECH_REPAIR_PATH", "configs/prompts/anchor-style/fact-speech-repair.md", "话术修正母版")
	if err != nil {
		return nil, err
	}
	expression, err := cycleExpressionView(state)
	if err != nil {
		return nil, err
	}
	task := state.Task()
	facts, guidance := generationMaterialViews(state.HookFacts())
	var content any = facts
	if task.ProductMode == "lecture" {
		content = task.LectureFacts
	}
	packet := map[string]any{"confirmed_facts": content, "expression_guidance": guidance, "requested_purpose": task.LecturePurpose, "requested_angle": task.Angle, "expression": expression, "current_rules": generationP0View(state.P0PolicyMD, state.Config.ComplianceScope), "unapproved_text": previous["text"], "review_issues": previous["issues"], "max_chars": state.Config.MaxChars}
	if task.FactScope != nil || len(state.EffectiveCorrections()) > 0 {
		packet["user_confirmed_corrections"] = state.EffectiveCorrections()
	}
	if task.ProductMode != "lecture" {
		packet["requested_purpose"] = task.PainPoint
	}
	if task.LecturePlanJSON != "" {
		plan, err := parseLectureSegmentPlan(task.LecturePlanJSON, task.LectureFacts)
		if err != nil {
			return nil, err
		}
		packet["segment_plan"] = plan
		packet["requested_purpose"], packet["requested_angle"] = plan.Purpose, plan.Angle
		relation := []string{}
		for _, line := range strings.Split(task.LectureFacts, "\n") {
			if strings.HasPrefix(line, "资料对象：") || strings.HasPrefix(line, "与商品的已确认关系：") {
				relation = append(relation, line)
			}
		}
		packet["confirmed_facts"] = strings.Join(append(relation, plan.Quotes...), "\n")
	}
	raw, _ := json.Marshal(packet)
	return []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(raw)}}, nil
}
