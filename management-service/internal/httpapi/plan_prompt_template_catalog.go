package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/planprompt"
)

func (s *Server) planPromptCatalogGet(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	c, err := s.store.GetPlanPromptTemplateCatalog(r.Context())
	if err != nil {
		writeError(w, 500, "读取模板目录失败")
		return
	}
	writeJSON(w, 200, map[string]any{"catalog": c, "revision": c.Revision()})
}
func (s *Server) planPromptCatalogPut(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input struct {
		Catalog  planprompt.TemplateCatalog `json:"catalog"`
		Revision string                     `json:"revision"`
	}
	// At most 120 templates of 60KB UTF-8 text, allowing JSON escaping overhead.
	if readJSONWithLimit(w, r, &input, 48<<20) != nil || len(input.Revision) != 64 {
		writeError(w, 400, "请完整提交模板目录及读取版本")
		return
	}
	if err := input.Catalog.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	c, err := s.store.SavePlanPromptTemplateCatalog(r.Context(), input.Catalog, input.Revision, actor.UserID)
	if errors.Is(err, db.ErrPlanPromptCatalogRevision) {
		writeError(w, 409, err.Error())
		return
	}
	if err != nil {
		writeError(w, 500, "保存模板目录失败，草稿已保留")
		return
	}
	writeJSON(w, 200, map[string]any{"catalog": c, "revision": c.Revision()})
}

// Selection is read-only and uses the same plan ownership check as prompt reads.
// No render/generation, placeholder substitution or customer writes occur here.
func (s *Server) liveAgentPlanPromptTemplates(w http.ResponseWriter, r *http.Request) {
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
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, 404, "方案不存在")
		return
	}
	page := r.URL.Query().Get("page")
	if !planprompt.IsTemplatePage(page) {
		writeError(w, 400, "请选择有效的提示词标签")
		return
	}
	c, err := s.store.GetPlanPromptTemplateCatalog(r.Context())
	if err != nil {
		writeError(w, 500, "读取模板目录失败")
		return
	}
	writeJSON(w, 200, map[string]any{"page": page, "templates": c.Pages[page], "revision": c.Revision()})
}
