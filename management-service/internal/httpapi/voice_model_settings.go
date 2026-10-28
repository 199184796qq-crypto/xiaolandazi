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

type voiceModelPrice struct {
	Model                string  `json:"model"`
	Unit                 string  `json:"unit"`
	YuanPer10K           float64 `json:"yuan_per_10k,omitempty"`
	InputYuanPerMillion  float64 `json:"input_yuan_per_million,omitempty"`
	OutputYuanPerMillion float64 `json:"output_yuan_per_million,omitempty"`
	Label                string  `json:"label"`
}

func voiceModelPriceCatalog() []voiceModelPrice {
	return []voiceModelPrice{
		{Model: "cosyvoice-v3.5-flash", Unit: "characters", YuanPer10K: 0.8, Label: "0.8 元/万计费字符"},
		{Model: "cosyvoice-v3.5-plus", Unit: "characters", YuanPer10K: 1.5, Label: "1.5 元/万计费字符"},
		{Model: "qwen-audio-3.0-tts-plus", Unit: "characters", YuanPer10K: 1.4, Label: "1.4 元/万计费字符"},
		{Model: "qwen-audio-3.0-tts-flash", Unit: "characters", YuanPer10K: 1.0, Label: "1.0 元/万计费字符"},
		{Model: "qwen3-tts-flash", Unit: "characters", YuanPer10K: 0.8, Label: "0.8 元/万计费字符"},
		{Model: "qwen-audio-3.1-tts-flash", Unit: "tokens", InputYuanPerMillion: 1.5, OutputYuanPerMillion: 12, Label: "按 Token：输入 1.5 元/百万 Token，输出 12 元/百万 Token"},
	}
}

func (s *Server) systemVoiceModelsResponse(c voicemodels.Settings) map[string]any {
	return map[string]any{
		"config":       c,
		"credentials":  voiceCredentialsStatus(),
		"prices":       voiceModelPriceCatalog(),
		"billing_rule": "计费字符：汉字 2、英文/数字/标点 1",
	}
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
	writeJSON(w, 200, s.systemVoiceModelsResponse(c))
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
	writeJSON(w, 200, s.systemVoiceModelsResponse(c))
}
