package decisionexecutor

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/ttsgateway"
	"net/http"
	"os"
	"testing"
	"time"
)

// Explicit probe: ordinary interaction API, thinking off, same room voice.
// No session, decision, output stream or plan is created or modified.
func TestReturnBridgeProviderProbe(t *testing.T) {
	if os.Getenv("INTERRUPT_PHASE1_BRIDGE_PROBE") != "1" {
		t.Skip("explicit operational opt-in required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gateway := agentgateway.NewFromEnv().WithSpeechModels(store)
	w := New(store, nil, gateway, ttsgateway.NewFromEnv())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	profile, err := w.bridgeModel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	template, err := interactionpolicy.Read("interaction-return-bridge-generation.md")
	if err != nil {
		t.Fatal(err)
	}
	var plan returnBridgePlan
	json.Unmarshal([]byte(`{"before_text":"刚才讲到现卤现炸的做法，接下来讲保存方法。","reply_text":"发货的快递以当前商品页说明为准，具体地区可以再确认。","units":[{"id":"return1","text":"收到以后，吃不完的部分按商品页标注的保存要求处理。"},{"id":"return2","text":"接着讲一下食用方法。"}]}`), &plan)
	input, _ := json.Marshal(map[string]any{"切点前完整上下文": plan.BeforeText, "核心回答": plan.ReplyText, "实际回归及后续完整内容": plan.Units})
	began := time.Now()
	response, err := gateway.Complete(ctx, agentgateway.Request{Stage: "interaction_bridge", Provider: "anchor:" + profile.ID, Messages: []agentgateway.Message{{Role: "system", Content: string(template)}, {Role: "user", Content: string(input)}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: 128, EnableThinking: false, Timeout: 4 * time.Second})
	modelMS := time.Since(began).Milliseconds()
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := validateBridge(response.Text, plan)
	if err != nil {
		t.Fatal(err, response.Text)
	}
	result := map[string]any{"model": response.Model, "profile": profile.ID, "thinking": false, "model_ms": modelMS, "raw": response.Text, "type": bridge.Type, "text": bridge.Text}
	if bridge.Type != "none" {
		voice, ok, err := w.readyVoice(ctx, 14, 26)
		if err != nil || !ok {
			t.Fatal("room voice unavailable", err)
		}
		start := time.Now()
		audio, err := w.tts.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{Provider: voiceProvider(voice), Model: voiceModel(voice), VoiceID: voice.VoiceID, Text: bridge.Text, Rate: voiceRate(voice)})
		result["tts_ms"] = time.Since(start).Milliseconds()
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, audio.AudioURL, nil)
		httpResponse, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer httpResponse.Body.Close()
		bytes, err := io.ReadAll(io.LimitReader(httpResponse.Body, 4<<20))
		if err != nil || len(bytes) < 44 || string(bytes[0:4]) != "RIFF" {
			t.Fatal("bridge WAV unreadable", err)
		}
		var byteRate, dataSize uint32
		for at := 12; at+8 <= len(bytes); {
			size := int(binary.LittleEndian.Uint32(bytes[at+4 : at+8]))
			body := at + 8
			end := body + size
			if end > len(bytes) {
				end = len(bytes)
			}
			switch string(bytes[at : at+4]) {
			case "fmt ":
				if end-body >= 16 {
					byteRate = binary.LittleEndian.Uint32(bytes[body+8 : body+12])
				}
			case "data":
				dataSize = uint32(end - body)
			}
			if end == len(bytes) {
				break
			}
			at = end + size%2
		}
		if byteRate == 0 || dataSize == 0 {
			t.Fatal("bridge WAV has no PCM data")
		}
		result["audio_ms"] = uint64(dataSize) * 1000 / uint64(byteRate)
		result["audio_bytes"] = len(bytes)
	}
	result["total_ready_ms"] = time.Since(began).Milliseconds()
	if path := os.Getenv("INTERRUPT_PHASE1_BRIDGE_RESULT_PATH"); path != "" {
		raw, _ := json.MarshalIndent(result, "", "  ")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("bridge model=%s type=%s text=%q model_ms=%d total_ready_ms=%d", response.Model, bridge.Type, bridge.Text, modelMS, time.Since(began).Milliseconds())
}
