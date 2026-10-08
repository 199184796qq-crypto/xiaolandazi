package httpapi

import (
	"net/http"
	"unicode/utf8"

	"livecompanion/management/internal/contentguard"
)

func loadRiskAnnotationMothers() (string, string, string, error) {
	p0, _, err := loadP0Mother()
	if err != nil {
		return "", "", "", err
	}
	p1, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_P1_CARD_POLICY_PATH", "configs/prompts/anchor-style/compliance/L2/food/p1-evidence-cards.md", "P1资质卡母版")
	if err != nil {
		return "", "", "", err
	}
	p2, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_P2_REWRITE_POLICY_PATH", "configs/prompts/anchor-style/compliance/L2/food/p2-wording-rewrite.md", "P2风险表达母版")
	return p0, p1, p2, err
}

func (s *Server) anchorStyleRiskAnnotations(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
		writeError(w, 404, "方案不存在或无权查看")
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if readJSON(w, r, &input) != nil || utf8.RuneCountInString(input.Text) > 100000 {
		writeError(w, 400, "风险标注文本格式错误或超过100000字符")
		return
	}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	result, err := contentguard.Annotate(input.Text, p0, p1, p2)
	if err != nil {
		writeError(w, 503, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
