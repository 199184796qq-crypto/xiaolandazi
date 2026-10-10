package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorgeneration"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) systemAnchorGenerationSettingsGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	config, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil {
		writeError(w, 503, "读取生成配置失败")
		return
	}
	writeJSON(w, 200, config)
}
func (s *Server) systemAnchorGenerationSettingsPut(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	var config anchorgeneration.Settings
	if readJSON(w, r, &config) != nil {
		writeError(w, 400, "生成配置格式错误")
		return
	}
	models, err := s.store.SpeechModels(r.Context())
	if err != nil {
		writeError(w, 503, "读取统一模型配置失败")
		return
	}
	config.ApplyModelRoles(models.SystemDefaultID, models.DefaultID)
	config.NormalizeSharedFeedback()
	if err := config.Validate(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	ids := []string{config.StyleProfileID, config.ScorecardProfileID, config.SellingPointProfileID, config.Cycle.FeedbackID}
	if config.Cycle.TextProfileID == "" {
		ids = append(ids, config.Cycle.CredentialID)
	}
	if config.SafetyProfileID != "" {
		ids = append(ids, config.SafetyProfileID)
	}
	for _, id := range []string{config.Cycle.TextProfileID, config.InteractionProfileID} {
		if id != "" {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		profile, err := s.store.ResolveSpeechModel(r.Context(), id)
		if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
			writeError(w, 400, "所选模型配置不可用："+id)
			return
		}
	}
	raw, _ := json.Marshal(config)
	if err := s.store.UpdateSystemSettings(r.Context(), []model.SystemSettingUpdate{{Key: db.AnchorGenerationSetting, Value: string(raw)}}, actor.UserID); err != nil {
		writeError(w, 503, "保存生成配置失败")
		return
	}
	writeJSON(w, 200, config)
}
