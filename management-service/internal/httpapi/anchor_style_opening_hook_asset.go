package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
)

func (s *Server) anchorOpeningHookGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, _, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	md, source, err := loadOpeningHookMother()
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if _, err = anchorcycle.ParseOpeningHooks(md); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	asset, err := s.store.GetLiveOpeningHook(r.Context(), tenant, plan)
	if errors.Is(err, appdb.ErrLiveOpeningHookNotFound) {
		writeJSON(w, 200, map[string]any{"asset": nil, "mother_md": md, "mother_source": source})
		return
	}
	if err != nil {
		writeError(w, 500, "读取方案开场钩子失败")
		return
	}
	writeJSON(w, 200, map[string]any{"asset": asset, "mother_md": md, "mother_source": source})
}

func (s *Server) anchorOpeningHookSave(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, plan, _, ok := s.anchorCycleScope(w, r, actor)
	if !ok {
		return
	}
	var input struct {
		Mode string `json:"mode"`
		MD   string `json:"library_md"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "钩子库请求格式错误")
		return
	}
	md, source, err := resolveRequestPrompt(input.Mode, input.MD, loadOpeningHookMother)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if _, err = anchorcycle.ParseOpeningHooks(md); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	asset, err := s.store.SaveLiveOpeningHook(r.Context(), tenant, plan, actor.UserID, md, source)
	if err != nil {
		writeError(w, 500, "保存方案开场钩子失败")
		return
	}
	writeJSON(w, 200, map[string]any{"asset": asset})
}
