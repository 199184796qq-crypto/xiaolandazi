package voicemodels

import (
	"errors"
	"strings"

	"livecompanion/management/internal/ttsgateway"
)

type Settings struct {
	CustomModel     string `json:"custom_model"`
	OfficialLibrary string `json:"official_library"`
	Revision        uint64 `json:"revision"`
}

func Default() Settings {
	return Settings{CustomModel: ttsgateway.CosyVoiceFlash, OfficialLibrary: ttsgateway.TencentPremiumModel}
}

func (s Settings) Validate() error {
	if !ttsgateway.SupportsCloneModel(s.CustomModel) {
		return errors.New("请选择支持的自定义声音模型")
	}
	if s.OfficialLibrary != ttsgateway.TencentPremiumModel && s.OfficialLibrary != ttsgateway.ProviderQwen {
		return errors.New("请选择支持的官方声音库")
	}
	return nil
}

// Customer requests may omit the model. An old page must refresh after an
// administrator changes it, rather than silently creating a different voice.
func (s Settings) ResolveCustom(requested string) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if requested = strings.TrimSpace(requested); requested != "" && requested != s.CustomModel {
		return "", errors.New("声音设置已更新，请刷新页面后重试")
	}
	return s.CustomModel, nil
}
