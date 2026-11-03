package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/planprompt"
)

func installedPlanPromptTemplates() (planprompt.Templates, error) {
	master, err := interactionpolicy.Read("plan-prompt-master.md")
	if err != nil {
		return planprompt.Templates{}, err
	}
	sections := stripPlanPromptSectionMarkerValues(splitPlanPromptSections(string(master)))
	planning, err := defaultPlanRewritePlanning()
	if err != nil {
		return planprompt.Templates{}, err
	}
	return planprompt.Templates{Sections: planprompt.Sections{
		PersonaMD: sections["persona"], FactsMD: sections["facts"], RedLinesMD: sections["redlines"],
		SpeechRequirementsMD: sections["speech"], InteractionRequirementsMD: sections["interaction"],
	}, RewritePlanningMD: planning}, nil
}

func (s *Server) planPromptTemplatesGet(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	defaults, err := installedPlanPromptTemplates()
	if err != nil {
		writeError(w, 500, "读取提示词母版失败，当前内容已保留")
		return
	}
	value, err := s.store.GetPlanPromptTemplates(r.Context(), defaults)
	if err != nil {
		writeError(w, 500, "读取提示词母版失败，当前内容已保留")
		return
	}
	writeJSON(w, 200, map[string]any{"templates": value, "revision": value.Revision()})
}

func (s *Server) planPromptTemplatesPut(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input struct {
		Templates map[string]*string `json:"templates"`
		Revision  string             `json:"revision"`
	}
	if readJSON(w, r, &input) != nil || len(input.Templates) != len(planprompt.TemplateKeys) || len(input.Revision) != 64 {
		writeError(w, 400, "请完整提交六份提示词母版及读取版本")
		return
	}
	values := map[string]string{}
	for _, key := range planprompt.TemplateKeys {
		value, ok := input.Templates[key]
		if !ok || value == nil {
			writeError(w, 400, "请完整提交六份提示词母版")
			return
		}
		values[key] = *value
	}
	next := planprompt.TemplatesWithOverrides(planprompt.Templates{}, values)
	if err := next.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	defaults, err := installedPlanPromptTemplates()
	if err != nil {
		writeError(w, 500, "读取原母版失败，本次未保存")
		return
	}
	value, err := s.store.SavePlanPromptTemplates(r.Context(), defaults, next, input.Revision, actor.UserID)
	if errors.Is(err, db.ErrPlanPromptTemplateRevision) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "保存提示词母版失败，草稿已保留")
		return
	}
	writeJSON(w, 200, map[string]any{"templates": value, "revision": value.Revision()})
}

func renderPlanPromptTemplates(templates planprompt.Templates, data planPromptRenderData) map[string]string {
	facts := renderPlanPrompt(templates.FactsMD, data)
	// Operators may replace the facts mother's layout. Current plan data must
	// still be filled when the editable mother no longer has loop markers.
	for _, block := range []struct{ key, title, text string }{
		{"商品", "商品卡", planCardBlock(data.Links)},
		{"福利", "活动福利", planBenefitBlock(data.Benefits)},
	} {
		if strings.Contains(templates.FactsMD, "{{#"+block.key+"}}") && strings.Contains(templates.FactsMD, "{{/"+block.key+"}}") {
			continue
		}
		if strings.TrimSpace(block.text) != "" {
			facts = strings.TrimSpace(facts + "\n\n【" + block.title + "】\n" + block.text)
		}
	}
	sections := map[string]string{
		"persona":     renderPlanPrompt(templates.PersonaMD, data),
		"facts":       facts,
		"redlines":    renderPlanPrompt(templates.RedLinesMD, data),
		"speech":      renderPlanPrompt(templates.SpeechRequirementsMD, data),
		"interaction": renderPlanPrompt(templates.InteractionRequirementsMD, data),
	}
	sections["facts"] = fillPlanPromptFactsBlock(sections["facts"], planFactBlock(data.Facts))
	return sections
}
