package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

func (s *Server) systemModelPoolGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	entries, err := s.store.ListModelPoolEntries(r.Context())
	if err != nil {
		writeError(w, 500, "读取模型池失败")
		return
	}
	leases, err := s.store.ListModelPoolLeases(r.Context())
	if err != nil {
		writeError(w, 500, "读取模型分配失败")
		return
	}
	writeJSON(w, 200, map[string]any{"entries": entries, "leases": leases})
}
func modelPoolError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, appdb.ErrModelPoolRevision) || errors.Is(err, appdb.ErrModelPoolInUse) {
		status = http.StatusConflict
	}
	writeError(w, status, err.Error())
}
func (s *Server) systemModelPoolSave(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	var input appdb.ModelPoolEntryInput
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "模型池配置格式错误")
		return
	}
	entry, err := s.store.SaveModelPoolEntry(r.Context(), input, actor.UserID)
	if err != nil {
		modelPoolError(w, err)
		return
	}
	writeJSON(w, 200, entry)
}
func (s *Server) systemModelPoolDelete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	revision, err := strconv.ParseUint(r.URL.Query().Get("revision"), 10, 64)
	if err != nil {
		writeError(w, 400, "请刷新后重新删除")
		return
	}
	if err = s.store.DeleteModelPoolEntry(r.Context(), r.PathValue("entryID"), revision); err != nil {
		modelPoolError(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
func (s *Server) systemModelPoolModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	var input struct {
		ID      string                        `json:"id"`
		Profile agentgateway.SpeechModelInput `json:"profile"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "模型连接参数格式错误")
		return
	}
	p := input.Profile.SpeechModel
	p.APIKey = strings.TrimSpace(input.Profile.NewAPIKey)
	if p.APIKey == "" && input.ID != "" {
		stored, err := s.store.ModelPoolCatalogProfile(r.Context(), input.ID)
		if err != nil {
			writeError(w, 400, "账号Key不可读取，请重新填写")
			return
		}
		p.APIKey = stored.APIKey
	}
	if p.APIKey == "" || len(p.APIKey) > 4096 {
		writeError(w, 400, "请填写账号API Key")
		return
	}
	if err := agentgateway.ValidateSpeechConnection(p); err != nil {
		modelPoolError(w, err)
		return
	}
	models, err := agentgateway.ListSpeechEndpointModels(r.Context(), p)
	if err != nil {
		writeError(w, 502, "读取模型列表失败，请检查账号Key和接口地址")
		return
	}
	writeJSON(w, 200, models)
}

// Import is an explicit admin action; it copies credentials server-side without
// exposing them. Capacity remains unset until the administrator supplies X.
func (s *Server) systemModelPoolImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	p, err := s.store.ResolveSpeechModel(r.Context(), "")
	if err != nil || p == nil {
		writeError(w, 409, "当前没有可导入的口播模型账号")
		return
	}
	input := appdb.ModelPoolEntryInput{AccountName: "当前口播模型账号", Profile: agentgateway.SpeechModelInput{SpeechModel: *p, NewAPIKey: p.APIKey}, MaxRooms: 0, Enabled: false}
	input.Profile.ID = ""
	entry, err := s.store.SaveModelPoolEntry(r.Context(), input, actor.UserID)
	if err != nil {
		modelPoolError(w, err)
		return
	}
	writeJSON(w, 200, entry)
}
