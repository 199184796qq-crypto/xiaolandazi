package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

func validArchivedVoiceRate(rate float64) bool {
	return !math.IsNaN(rate) && !math.IsInf(rate, 0) && rate >= 0.5 && rate <= 2
}

// Zero means unknown, not the provider's default. In particular, editing a
// legacy recording's subtitles must not invent an original synthesis rate.
func optionalArchivedVoiceRate(rates []float64) (float64, error) {
	if len(rates) == 0 {
		return 0, nil
	}
	if len(rates) != 1 || !validArchivedVoiceRate(rates[0]) {
		return 0, errors.New("声音归档必须记录实际语速，范围为0.5到2.0")
	}
	return rates[0], nil
}

func archivedVoiceRate(metadata map[string]any) (float64, bool) {
	var rate float64
	switch value := metadata["voice_rate"].(type) {
	case float64:
		rate = value
	case float32:
		rate = float64(value)
	case int:
		rate = float64(value)
	case int64:
		rate = float64(value)
	case json.Number:
		var err error
		rate, err = value.Float64()
		if err != nil {
			return 0, false
		}
	case string:
		var err error
		rate, err = strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return rate, validArchivedVoiceRate(rate)
}

// A realtime voice/rate setting may have changed while the version kept its
// original audio assets. Compare against the audio's immutable identity before
// paying for text or TTS, otherwise a partial refresh can mix different voices.
func validateContentRefreshVoiceIdentity(asset model.MediaAsset, voice model.LiveAgentVoiceIdentity) error {
	voiceID, idOK := asset.Metadata["voice_id"].(string)
	modelName, modelOK := asset.Metadata["tts_model"].(string)
	voiceID, modelName = strings.TrimSpace(voiceID), strings.TrimSpace(modelName)
	if !idOK || !modelOK || voiceID == "" || modelName == "" ||
		strings.TrimSpace(voice.VoiceID) == "" || strings.TrimSpace(voice.Model) == "" {
		return errors.New("原成品缺少完整音色或模型记录，请重新生成并发布正式声音后启用局部更新")
	}
	if voiceID != strings.TrimSpace(voice.VoiceID) || modelName != strings.TrimSpace(voice.Model) {
		return errors.New("原成品音色或模型与当前发布声音不一致，请重新生成并发布正式声音，不能混拼不同音色")
	}
	rate, ok := archivedVoiceRate(asset.Metadata)
	if !ok {
		return errors.New("原成品缺少可靠的原始语速记录，请重新生成并发布一次正式声音后启用局部更新")
	}
	if !validArchivedVoiceRate(voice.Rate) || math.Abs(rate-voice.Rate) > 0.000001 {
		return errors.New("原成品语速与当前发布语速不一致，请重新生成并发布正式声音，不能混拼不同语速")
	}
	return nil
}
