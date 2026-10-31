package httpapi

import (
	"net/http"
	"strings"

	"livecompanion/management/internal/planprompt"
)

func (s *Server) liveAgentPlanPromptGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	document, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "方案提示词读取失败")
		return
	}
	writeJSON(w, http.StatusOK, document)
}

func (s *Server) liveAgentPlanPromptPut(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	var input struct {
		PromptMD string               `json:"prompt_md"`
		Sections *planprompt.Sections `json:"sections"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "方案提示词请求格式错误")
		return
	}
	var sections planprompt.Sections
	if input.Sections != nil {
		sections = input.Sections.Normalize()
	} else {
		// Old clients still save their single draft; partition it without dropping text.
		sections, _ = planprompt.SplitLegacy(stripPlanPromptHints(input.PromptMD))
	}
	if err := sections.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	document, err := s.store.SaveLiveAgentPlanPromptSections(r.Context(), tenantID, planID, actor.UserID, sections)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "方案提示词保存失败")
		return
	}
	writeJSON(w, http.StatusOK, document)
}

// stripPlanPromptHints 去掉模板渲染时带出来的填写提示行。提示只用于让用户
// 看懂该节怎么写，一旦保存就不能再进库，否则模型会把它当作可播内容。
func stripPlanPromptHints(text string) string {
	if !strings.Contains(text, "〔提示〕") {
		return text
	}
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "〔提示〕") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}
