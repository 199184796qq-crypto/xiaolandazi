package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
)

// cycleInstructionSources 列出可能被模型原样抄进正文的内部控制字段来源，
// 供 cleanCycleOpeningInstruction 做「删除开头指导文字」的程序判断。
// 该逻辑不依赖语境复核模型（P0 语境复核已下线）。
func cycleInstructionSources(state *anchorcycle.State) []string {
	sources := []string{state.Task().SellingPoint, state.Task().Angle, state.SegmentDirective, cycleExpressionMethod(state)}
	if plan := state.CurrentAnglePlan(); plan != nil {
		sources = append(sources, plan.Goal, plan.Action)
		sources = append(sources, plan.Hooks...)
		if h := state.CurrentAngleHook(); h != nil {
			sources = append(sources, h.Concern, h.Entry, h.Action)
		}
	}
	return sources
}

// cleanCycleOpeningInstruction 删除正文开头被原样抄入的指导/控制文字。
// quote 为语境复核模型提供的指导泄漏引用；语境复核下线后固定传空串，
// 走「精确匹配控制源」分支。纯程序逻辑，不调模型。
func cleanCycleOpeningInstruction(state *anchorcycle.State, candidate *anchorcycle.Candidate, quote string) bool {
	labels := []string{}
	if state.Phase == "closing" && state.Task().SellingPoint == "本轮收口" {
		labels = []string{"本轮收口", "这轮收口", "这一轮收口", "本段收口", "这段收口", "本次收口", "这次收口"}
	}
	changed := false
	if quote == "" {
		if state.TotalAccepted()%3 != 0 {
			labels := []string{}
			for _, link := range state.HookProducts() {
				if link != "current" {
					labels = append(labels, link+"号链接")
				}
			}
			text, edit := contentguard.StripLeadingProductControl(cycleVoiceText(*candidate), "直接说", labels)
			if edit != nil {
				candidate.OutputText = text
				candidate.OpeningInstructionEdits = append(candidate.OpeningInstructionEdits, *edit)
				changed = true
			}
		}
		text, edits, e := contentguard.StripRequestInstructions(cycleVoiceText(*candidate), cycleInstructionSources(state), labels)
		if e != nil {
			candidate.P0PatchError = e.Error()
			return false
		}
		if len(edits) > 0 {
			candidate.OutputText = text
			candidate.OpeningInstructionEdits = append(candidate.OpeningInstructionEdits, edits...)
			changed = true
		}
	}
	cleaned, edit, err := contentguard.StripOpeningInstruction(cycleVoiceText(*candidate), cycleInstructionSources(state), quote)
	if err != nil {
		candidate.P0PatchError = err.Error()
		return false
	}
	if edit == nil {
		return changed
	}
	candidate.OutputText = cleaned
	candidate.OpeningInstructionEdits = append(candidate.OpeningInstructionEdits, *edit)
	return true
}
