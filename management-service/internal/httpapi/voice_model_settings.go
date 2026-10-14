package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicemodels"
)

func (s *Server) configuredCustomVoiceModel(w http.ResponseWriter, r *http.Request, requested string) (string, bool) {
	c, err := s.store.VoiceModelSettings(r.Context())
	if err != nil {
		writeError(w, 500, "读取声音配置失败，请稍后重试")
		return "", false
	}
	m, err := c.ResolveCustom(requested)
	if err != nil {
		writeError(w, 409, err.Error())
		return "", false
	}
	return m, true
}

func (s *Server) liveVoiceSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	c, err := s.store.VoiceModelSettings(r.Context())
	if err != nil {
		writeError(w, 500, "读取声音配置失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"custom_model": c.CustomModel, "can_design": ttsgateway.IsCosyVoiceModel(c.CustomModel)})
}

func voiceCredentialsStatus() map[string]bool {
	return map[string]bool{"tencent": ttsgateway.TencentConfigured(), "aliyun": ttsgateway.QwenConfigFromEnv().APIKey != ""}
}

func (s *Server) systemVoiceModelsGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.requireAgentPromptAdmin(w, r); !ok {
		return
	}
	c, err := s.store.VoiceModelSettings(r.Context())
	if err != nil {
		writeError(w, 500, "读取声音模型配置失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"config": c, "credentials": voiceCredentialsStatus()})
}

func (s *Server) systemVoiceModelsPut(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireAgentPromptAdmin(w, r)
	if !ok {
		return
	}
	var input voicemodels.Settings
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "声音模型配置格式错误")
		return
	}
	c, err := s.store.SaveVoiceModelSettings(r.Context(), input, actor.UserID)
	if err != nil {
		if errors.Is(err, db.ErrVoiceModelRevision) {
			writeError(w, 409, err.Error())
			return
		}
		if input.Validate() != nil {
			writeError(w, 400, input.Validate().Error())
			return
		}
		writeError(w, 500, "保存声音模型配置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"config": c, "credentials": voiceCredentialsStatus()})
}
