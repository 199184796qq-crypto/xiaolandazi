package httpapi

import (
	"context"
	"errors"
	"sort"
	"strings"

	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
	"livecompanion/management/internal/voicecatalog"
)

// Account mappings are the administrator's pool catalogue, not customer-owned
// voice profiles. Only publish logical source IDs; backup-account target IDs,
// credentials and account identities must never reach the customer catalogue.
func highFidelityPoolVoices(accounts []ttsscheduler.Account) []voicecatalog.Voice {
	voices := map[string]voicecatalog.Voice{}
	for _, account := range accounts {
		if account.Provider != ttsgateway.ProviderVolcengine || !account.Enabled || !account.Configured {
			continue
		}
		modelEnabled := false
		for _, model := range account.Models {
			if model == ttsgateway.VolcengineICL2 {
				modelEnabled = true
			}
		}
		if !modelEnabled {
			continue
		}
		for key, target := range account.Voices {
			model, source, ok := strings.Cut(key, "/")
			if !ok || model != ttsgateway.VolcengineICL2 || !ttsgateway.ValidVolcengineVoiceID(source) || !ttsgateway.ValidVolcengineVoiceID(target) {
				continue
			}
			label := strings.TrimPrefix(source, "S_")
			if len(label) > 24 {
				label = label[:12] + "…" + label[len(label)-12:]
			}
			voices[source] = voicecatalog.Voice{ID: source, Name: "高仿真音色 " + label, Gender: "", Description: "已配置的复刻音色，可试听后选入方案。", Tags: []string{"高仿真"}, Model: ttsgateway.VolcengineICL2, Provider: ttsgateway.ProviderVolcengine}
		}
	}
	result := make([]voicecatalog.Voice, 0, len(voices))
	for _, voice := range voices {
		result = append(result, voice)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func (s *Server) availableHighFidelityVoices(ctx context.Context) ([]voicecatalog.Voice, error) {
	snapshot, err := s.store.TTSPoolSnapshot(ctx)
	if err != nil {
		return nil, errors.New("读取高仿真声音失败")
	}
	return highFidelityPoolVoices(snapshot.Accounts), nil
}

func (s *Server) findHighFidelityPoolVoice(ctx context.Context, voiceID string) (voicecatalog.Voice, error) {
	if !ttsgateway.ValidVolcengineVoiceID(voiceID) {
		return voicecatalog.Voice{}, errors.New("高仿真音色ID无效")
	}
	items, err := s.availableHighFidelityVoices(ctx)
	if err != nil {
		return voicecatalog.Voice{}, err
	}
	for _, item := range items {
		if item.ID == voiceID {
			return item, nil
		}
	}
	return voicecatalog.Voice{}, errors.New("该高仿真音色当前不可用，请重新选择声音后保存")
}
