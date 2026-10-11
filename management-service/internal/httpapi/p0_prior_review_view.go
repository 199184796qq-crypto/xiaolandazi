package httpapi

import "livecompanion/management/internal/anchorcycle"

// Model-facing content summary only. The program validates text and policy
// binding; hashes, rule IDs, hit IDs and internal candidate IDs stay local.
func priorP0ReviewView(state *anchorcycle.State, c anchorcycle.Candidate) map[string]any {
	if c.P0Report == nil {
		return map[string]any{"可复用": false, "说明": "本次尚未取得完成的P0审核结果"}
	}
	if !c.P0Report.Passed {
		return map[string]any{"可复用": false, "已通过": false, "说明": "当前P0未通过，后续内容判断不能授权播出"}
	}
	if state.CheckApprovedText(c, cycleVoiceText(c)) != nil {
		return map[string]any{"可复用": false, "说明": "正文或适用P0已变化，已有判断不作本次依据"}
	}
	out := map[string]any{"可复用": true, "已通过": true, "说明": "程序已确认此前P0审核对应本次完全相同正文、规则、范围与例外"}
	if review := c.P0Report.ContextReview; review != nil {
		items := []map[string]string{}
		for i, item := range review.Output.Items {
			term := ""
			if i < len(review.LexicalHits) {
				term = review.LexicalHits[i].Text
			}
			items = append(items, map[string]string{"命中表述": term, "语境结论": item.Decision, "原因": item.Reason})
		}
		out["已完成的语境核对"] = items
	}
	return out
}
