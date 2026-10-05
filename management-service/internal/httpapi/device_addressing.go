package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"

	"livecompanion/management/internal/audioaddressing"
	"livecompanion/management/internal/model"
)

func (s *Server) liveDeviceAddressing(w http.ResponseWriter, r *http.Request) {
	_, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	mode, err := s.store.DeviceAddressingMode(r.Context(), tenant, id)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, model.DeviceAddressingPreference{Mode: mode})
}

func (s *Server) liveSetDeviceAddressing(w http.ResponseWriter, r *http.Request) {
	actor, tenant, ok := s.deviceCustomer(w, r)
	if !ok {
		return
	}
	id, ok := namedPathID(w, r, "deviceID", "设备")
	if !ok {
		return
	}
	var input model.DeviceAddressingPreference
	if err := readJSON(w, r, &input); err != nil || !model.ValidDeviceAddressingMode(input.Mode) {
		writeError(w, 400, "称呼模式需为 auto、female、male、child 或 neutral")
		return
	}
	out, err := s.store.SetDeviceAddressingMode(r.Context(), tenant, actor.UserID, id, input.Mode)
	if err != nil {
		s.deviceBusinessError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, out)
}

func wakeGreetingOnly(text string) bool {
	text = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || strings.ContainsRune("，,。！!？?、：:", r) {
			return -1
		}
		return r
	}, strings.TrimSpace(text))
	for _, prefix := range []string{"你好小蓝", "小蓝小蓝", "小蓝搭子", "小蓝"} {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimPrefix(text, prefix)
			return strings.TrimPrefix(text, "请") == ""
		}
	}
	return false
}

func (s *Server) selectVoiceAddressing(ctx context.Context, event model.DeviceBusinessEvent, data []byte, format string, wakeOnly bool) model.DeviceVoiceAddressing {
	neutral := model.DeviceVoiceAddressing{Addressing: "neutral", Source: "neutral"}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	mode, err := s.store.DeviceAddressingMode(ctx, event.TenantID, event.DeviceID)
	if err != nil {
		return neutral
	}
	if mode != "auto" {
		return model.DeviceVoiceAddressing{Addressing: mode, Source: "preference"}
	}
	if wakeOnly {
		return neutral
	}
	result, err := audioaddressing.NewFromEnv().Analyze(ctx, data, format)
	if err != nil {
		return neutral
	}
	if result.Addressing == "neutral" {
		return neutral
	}
	return model.DeviceVoiceAddressing{Addressing: result.Addressing, Source: "acoustic"}
}

func (s *Server) cachedVoiceAddressing(ctx context.Context, event model.DeviceBusinessEvent) string {
	mode, err := s.store.DeviceAddressingMode(ctx, event.TenantID, event.DeviceID)
	if err == nil && mode != "auto" {
		return mode
	}
	stored, err := s.store.VoiceSpeakerAddressing(ctx, event.TenantID, event.DeviceID, event.ID)
	if err != nil {
		return "neutral"
	}
	return stored.Addressing
}
