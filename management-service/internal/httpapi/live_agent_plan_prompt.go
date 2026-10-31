package httpapi

import (
	"net/http"
	"strings"
	"unicode/utf8"

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
	if input.Sections == nil {
		// A legacy client has one original field, not four. Preserve that document
		// without guessing which page each of its headings belongs to.
		prompt := stripPlanPromptHints(input.PromptMD)
		if utf8.RuneCountInString(prompt) > planprompt.MaxPromptRunes {
			writeError(w, http.StatusBadRequest, "方案提示词不能超过20000字")
			return
		}
		if _, err := s.store.SaveLiveAgentPlanPrompt(r.Context(), tenantID, planID, actor.UserID, prompt); err != nil {
			writeError(w, http.StatusInternalServerError, "方案提示词保存失败")
			return
		}
		document, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "方案提示词读取失败")
			return
		}
		writeJSON(w, http.StatusOK, document)
		return
	}
	sections := *input.Sections
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

// stripPlanPromptHints 仅用于旧单字段客户端。四分区原文原样保存，提示行
// 只在编译派生副本时过滤，不能从可编辑原文中删除。
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
