package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorgeneration"
)

// Only these customer-owned scheduling values may override platform settings.
// Credentials, model roles, style assets, risk rules and retry limits stay server-owned.
type realtimeGenerationOptions struct {
	BufferSeconds int `json:"buffer_seconds"`
	BodyMin       int `json:"body_min"`
	BodyMax       int `json:"body_max"`
	MaxChars      int `json:"max_chars"`
}

func realtimeOptionsDefaults(settings anchorgeneration.Settings) realtimeGenerationOptions {
	return realtimeGenerationOptions{settings.MainlineBufferSeconds, settings.Cycle.BodyMin, settings.Cycle.BodyMax, settings.Cycle.MaxChars}
}

func (o realtimeGenerationOptions) validate() error {
	if o.BufferSeconds < 20 || o.BufferSeconds > 600 {
		return errors.New("正式主线缓存须为20～600秒")
	}
	if o.BodyMin < 2 || o.BodyMax > 6 || o.BodyMax < o.BodyMin {
		return errors.New("中间讲解最少/最多段数须为2～6，且最少不能超过最多")
	}
	if o.MaxChars < 80 || o.MaxChars > 1000 {
		return errors.New("单段最大字数须为80～1000")
	}
	return nil
}

func parseRealtimeOptions(raw json.RawMessage, defaults realtimeGenerationOptions) (realtimeGenerationOptions, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return defaults, defaults.validate()
	}
	var options realtimeGenerationOptions
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || decoder.Decode(&options) != nil {
		return options, errors.New("实时声音参数格式错误，只能设置缓存、讲解段数和单段字数")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return options, errors.New("实时声音参数格式错误")
	}
	return options, options.validate()
}

func realtimeOptionsFromContext(context map[string]any, defaults realtimeGenerationOptions) (realtimeGenerationOptions, error) {
	value, exists := context["anchor_generation_options"]
	if !exists {
		return parseRealtimeOptions(nil, defaults)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return defaults, errors.New("实时声音参数格式错误")
	}
	return parseRealtimeOptions(raw, defaults)
}

func applyRealtimeOptions(config anchorcycle.Config, options realtimeGenerationOptions) anchorcycle.Config {
	config.BodyMin, config.BodyMax, config.MaxChars = options.BodyMin, options.BodyMax, options.MaxChars
	if config.TTS != nil {
		voice := *config.TTS
		voice.BufferSeconds = options.BufferSeconds
		config.TTS = &voice
	}
	return config
}

func (s *Server) liveRealtimeGenerationOptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	if _, _, _, _, ok = s.anchorStyleWorkbenchPlanData(w, r, tenant, plan); !ok {
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取实时声音默认参数失败")
		return
	}
	options := realtimeOptionsDefaults(settings)
	if err = options.validate(); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	// This projection deliberately excludes every model/Agent/credential field.
	writeJSON(w, http.StatusOK, options)
}
