package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/ttsgateway"
)

type devSynthesizeTextInput struct {
	TTSProvider string  `json:"tts_provider"`
	TTSModel    string  `json:"tts_model"`
	VoiceID     string  `json:"voice_id"`
	TTSRate     float64 `json:"tts_rate"`
	Text        string  `json:"text"`
}

func (s *Server) devRuntimeSynthesizeText(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var input devSynthesizeTextInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.TTSProvider = strings.TrimSpace(input.TTSProvider)
	input.TTSModel = strings.TrimSpace(input.TTSModel)
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	input.Text = strings.TrimSpace(input.Text)
	if input.TTSRate == 0 {
		input.TTSRate = 1
	}
	if input.Text == "" || utf8.RuneCountInString(input.Text) > 6000 {
		writeError(w, http.StatusBadRequest, "text 只允许 1 到 6000 字")
		return
	}
	if input.VoiceID == "" {
		writeError(w, http.StatusBadRequest, "voice_id is required")
		return
	}
	if input.TTSRate < 0.5 || input.TTSRate > 2.0 {
		writeError(w, http.StatusBadRequest, "tts_rate 只允许 0.5 到 2.0")
		return
	}

	started := time.Now()
	result, err := ttsgateway.NewFromEnv().SynthesizeURL(r.Context(), ttsgateway.SynthesizeRequest{
		Provider: input.TTSProvider,
		Model:    input.TTSModel,
		VoiceID:  input.VoiceID,
		Text:     input.Text,
		Rate:     input.TTSRate,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "TTS 生成失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"audio_url":      result.AudioURL,
		"tts_provider":   result.Provider,
		"tts_model":      result.Model,
		"voice_id":       result.VoiceID,
		"tts_rate":       result.Rate,
		"tts_latency_ms": time.Since(started).Milliseconds(),
	})
}
