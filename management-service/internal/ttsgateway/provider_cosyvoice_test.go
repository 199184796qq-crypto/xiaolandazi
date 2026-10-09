package ttsgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCosyVoiceModelsUseTheirOwnProviderAndCustomization(t *testing.T) {
	for _, target := range []string{CosyVoiceFlash, CosyVoicePlus} {
		t.Run(target, func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Error("missing authorization")
				}
				var payload struct {
					Model string         `json:"model"`
					Input map[string]any `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				calls = append(calls, r.URL.Path)
				if r.URL.Path == "/services/audio/tts/SpeechSynthesizer" {
					if payload.Model != target || payload.Input["voice"] != "cosy-voice" || payload.Input["instruction"] != "亲切自然地讲解。" || payload.Input["rate"] != 1.2 {
						t.Errorf("incorrect synthesis: %#v", payload)
					}
					w.Write([]byte(`{"output":{"audio":{"url":"https://example.com/test.wav"}}}`))
					return
				}
				if payload.Model != "voice-enrollment" {
					t.Errorf("wrong enrollment model: %s", payload.Model)
				}
				if payload.Input["action"] == "query_voice" {
					w.Write([]byte(`{"output":{"status":"OK","target_model":"` + target + `"}}`))
					return
				}
				if payload.Input["action"] != "create_voice" || payload.Input["target_model"] != target || payload.Input["url"] != "https://example.com/sample.wav" {
					t.Errorf("incorrect clone: %#v", payload)
				}
				w.Write([]byte(`{"output":{"voice_id":"cosy-voice"}}`))
			}))
			defer server.Close()
			g := New(ProviderQwen, DefaultQwenTTSModel)
			g.Register(NewCosyVoiceProvider(QwenConfig{APIKey: "test-key", TTSBaseURL: server.URL, CustomizationURL: server.URL + "/customization", Client: server.Client()}))
			clone, err := g.CloneVoice(t.Context(), CloneRequest{TargetModel: target, PreferredName: "sample", AudioURL: "https://example.com/sample.wav"})
			if err != nil || clone.Provider != ProviderCosyVoice || clone.Model != target {
				t.Fatal(clone, err)
			}
			voice, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Model: target, VoiceID: clone.VoiceID, Text: "商品价格八十五元五角。", Rate: 1.2, Instruction: "亲切自然地讲解。"})
			if err != nil || voice.Provider != ProviderCosyVoice || voice.Model != target || voice.Rate != 1.2 {
				t.Fatal(voice, err)
			}
			if len(calls) != 3 {
				t.Fatal("must check voice readiness before using", calls)
			}
		})
	}
}

func TestCosyVoiceDesignAndModelMismatch(t *testing.T) {
	for _, status := range []string{"OK", "UNDEPLOYED", "wrong-model"} {
		t.Run(status, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Input map[string]any `json:"input"`
				}
				json.NewDecoder(r.Body).Decode(&payload)
				if payload.Input["action"] == "query_voice" {
					model := CosyVoiceFlash
					s := status
					if status == "wrong-model" {
						model = CosyVoicePlus
						s = "OK"
					}
					w.Write([]byte(`{"output":{"status":"` + s + `","target_model":"` + model + `"}}`))
					return
				}
				if payload.Input["voice_prompt"] != "温暖明亮的女声" || payload.Input["target_model"] != CosyVoiceFlash || payload.Input["url"] != nil {
					t.Errorf("wrong design payload: %v", payload.Input)
				}
				w.Write([]byte(`{"output":{"voice_id":"designed-flash","preview_audio":{"data":"UklGRg==","response_format":"wav"}}}`))
			}))
			defer server.Close()
			g := New(ProviderQwen, DefaultQwenTTSModel)
			g.Register(NewCosyVoiceProvider(QwenConfig{APIKey: "key", CustomizationURL: server.URL, Client: server.Client()}))
			out, err := g.DesignVoice(t.Context(), DesignRequest{Model: CosyVoiceFlash, Description: "温暖明亮的女声", PreviewText: "欢迎来到直播间，今天给大家介绍这款产品。"})
			if status == "OK" {
				if err != nil || out.VoiceID != "designed-flash" || string(out.PreviewAudio) != "RIFF" {
					t.Fatal(out, err)
				}
			} else if err == nil {
				t.Fatal("must reject unready/mismatched voice")
			}
		})
	}
}

func TestCosyVoiceRejectsWrongModelAndOversizedInstructionBeforeCalling(t *testing.T) {
	p := NewCosyVoiceProvider(QwenConfig{APIKey: "key"})
	for _, request := range []SynthesizeRequest{{Model: DefaultQwenTTSModel}, {Model: CosyVoiceFlash, Instruction: strings.Repeat("汉", 51)}} {
		if _, err := p.SynthesizeURL(t.Context(), request); err == nil {
			t.Fatal("accepted invalid request")
		}
	}
	if CharacterCount("你好A1。") != 7 {
		t.Fatal("incorrect character budget")
	}
}
