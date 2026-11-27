package ttsgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteOwnedVoiceUsesEnrollmentAndRetries(t *testing.T) {
	for _, test := range []struct{ provider, model string }{{ProviderQwen, DefaultQwenTTSModel}, {ProviderCosyVoice, CosyVoicePlus}} {
		t.Run(test.model, func(t *testing.T) {
			voice := test.model + "-archive-fixture"
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload struct {
					Model string            `json:"model"`
					Input map[string]string `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || payload.Model != "voice-enrollment" || payload.Input["action"] != "delete_voice" || payload.Input["voice_id"] != voice {
					t.Errorf("invalid request %#v", payload)
				}
				if calls == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = w.Write([]byte(`{"code":"BadRequest.ResourceNotExist"}`))
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":"BadRequest.ResourceNotExist"}`))
			}))
			defer srv.Close()
			cfg := QwenConfig{APIKey: "fixture", CustomizationURL: srv.URL}
			g := New(ProviderQwen, DefaultQwenTTSModel)
			g.Register(NewQwenProvider(cfg))
			g.Register(NewCosyVoiceProvider(cfg))
			if err := g.DeleteOwnedClone(t.Context(), test.provider, test.model, "public-voice"); err == nil || calls != 0 {
				t.Fatal("public voice delete", err, calls)
			}
			if err := g.DeleteOwnedClone(t.Context(), test.provider, test.model, voice); err == nil {
				t.Fatal("temporary error ignored")
			}
			if err := g.DeleteOwnedClone(t.Context(), test.provider, test.model, voice); err != nil || calls != 2 {
				t.Fatal(err, calls)
			}
		})
	}
}
