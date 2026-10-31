package httpapi

import (
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"strings"
	"unicode"
	"unicode/utf8"
)

func validateVolcengineImportedVoice(input *model.VoiceProfileInput) error {
	if input.Provider != ttsgateway.ProviderVolcengine || !ttsgateway.ValidVolcengineVoiceID(input.VoiceID) ||
		input.Config["target_model"] != ttsgateway.VolcengineICL2 || strings.TrimSpace(input.Name) == "" || utf8.RuneCountInString(input.Name) > 80 || strings.IndexFunc(input.Name, unicode.IsControl) >= 0 {
		return errors.New("导入火山声音需要有效名称、seed-icl-2.0模型和S_音色ID")
	}
	if input.SampleAssetID != nil || (input.CloneStatus != "" && input.CloneStatus != "ready") {
		return errors.New("导入已有音色无需上传样本，状态必须为可用")
	}
	avatar, _ := input.Config["avatar_key"].(string)
	if avatar != "" && !validVoiceAvatarKey(avatar) {
		return errors.New("请选择有效的声音头像")
	}
	input.CloneStatus = "ready"
	// No credentials or client-supplied ownership flags enter the voice profile.
	input.Config = map[string]any{"target_model": ttsgateway.VolcengineICL2, "source": "imported_existing_voice", "avatar_key": avatar, "rate": 1}
	return nil
}
