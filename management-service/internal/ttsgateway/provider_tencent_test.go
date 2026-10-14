package ttsgateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func fakeTencent(t *testing.T, scenario string) (*Gateway, <-chan map[string]string) {
	t.Helper()
	observed := make(chan map[string]string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		signature := query.Get("Signature")
		query.Del("Signature")
		signed, err := tencentSignedURL("ws://"+r.Host+r.URL.Path, "fake-secret", query)
		parsed, _ := url.Parse(signed)
		if err != nil || signature != parsed.Query().Get("Signature") || query.Get("VoiceType") != "101030" || query.Get("SampleRate") != "16000" || query.Get("Codec") != "pcm" || query.Get("Action") != "TextToStreamAudioWSv2" {
			t.Error("incorrect handshake")
			http.Error(w, "bad", 400)
			return
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		c.WriteJSON(map[string]any{"code": 0})
		c.WriteJSON(map[string]any{"code": 0, "ready": 1})
		for i := 0; i < 2; i++ {
			var msg map[string]string
			if c.ReadJSON(&msg) != nil {
				return
			}
			observed <- msg
		}
		if scenario == "cancel" {
			c.ReadMessage()
			return
		}
		if scenario == "error" {
			c.WriteJSON(map[string]any{"code": 10003, "message": "fake-secret SecretId=fake-id"})
			return
		}
		c.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3, 4})
		if scenario == "partial" {
			return
		}
		c.WriteJSON(map[string]any{"code": 0, "heartbeat": 1})
		c.WriteMessage(websocket.BinaryMessage, []byte{5, 6, 7, 8})
		c.WriteJSON(map[string]any{"code": 0, "final": 1})
	}))
	t.Cleanup(server.Close)
	gateway := New("qwen_audio", "qwen-audio-3.1-tts-flash")
	gateway.Register(NewTencentProvider(TencentConfig{AppID: "1234", SecretID: "fake-id", SecretKey: "fake-secret", Endpoint: "ws" + strings.TrimPrefix(server.URL, "http") + "/stream_wsv2", Publish: func(ctx context.Context, wav []byte) (string, error) {
		if len(wav) != 52 || string(wav[:4]) != "RIFF" || binary.LittleEndian.Uint32(wav[24:]) != 16000 || binary.LittleEndian.Uint32(wav[28:]) != 32000 || !bytes.Equal(wav[44:], []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
			t.Error("incomplete/wrong format PCM")
		}
		return "http://example.test/audio.wav", nil
	}}))
	return gateway, observed
}
func TestTencentPremiumStreamProtocolAndModelRouting(t *testing.T) {
	g, messages := fakeTencent(t, "complete")
	out, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Model: TencentPremiumModel, VoiceID: "101030", Text: "欢迎来到直播间。", Rate: 1.2, Instruction: "不要念这条指导"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Provider != ProviderTencent || out.Model != TencentPremiumModel || out.AudioURL == "" || out.Rate != 1.2 {
		t.Fatalf("metadata: %+v", out)
	}
	synthesis := <-messages
	complete := <-messages
	if synthesis["action"] != "ACTION_SYNTHESIS" || synthesis["data"] != "欢迎来到直播间。" || complete["action"] != "ACTION_COMPLETE" || complete["data"] != "" || synthesis["session_id"] == "" || synthesis["message_id"] == complete["message_id"] {
		t.Fatalf("protocol mismatch: %v %v", synthesis, complete)
	}
}
func TestTencentNeverPublishesPartialAudioOrSecrets(t *testing.T) {
	for _, scenario := range []string{"partial", "error"} {
		t.Run(scenario, func(t *testing.T) {
			g, _ := fakeTencent(t, scenario)
			out, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Model: TencentPremiumModel, VoiceID: "101030", Text: "测试。"})
			if err == nil || out.AudioURL != "" {
				t.Fatalf("partial accepted: %+v %v", out, err)
			}
			if strings.Contains(err.Error(), "fake-secret") || strings.Contains(err.Error(), "fake-id") || strings.Contains(err.Error(), "Signature") {
				t.Fatal("credential leaked")
			}
		})
	}
}
func TestTencentCancellationAndInvalidVoices(t *testing.T) {
	g, messages := fakeTencent(t, "cancel")
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := g.SynthesizeURL(ctx, SynthesizeRequest{Model: TencentPremiumModel, VoiceID: "101030", Text: "测试。"})
		result <- err
	}()
	<-messages
	<-messages
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cancel passed")
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close socket")
	}
	for _, voice := range []string{"402000", "longanlingxin", "999999"} {
		if _, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Model: TencentPremiumModel, VoiceID: voice, Text: "测试。"}); err == nil {
			t.Fatalf("other tier accepted: %s", voice)
		}
	}
	if _, err := g.SynthesizeURL(t.Context(), SynthesizeRequest{Provider: ProviderQwen, Model: TencentPremiumModel, VoiceID: "101030", Text: "测试。"}); err == nil {
		t.Fatal("Tencent routed to Qwen")
	}
}
func TestTencentRateConversion(t *testing.T) {
	for _, v := range [][2]float64{{.6, -2}, {.8, -1}, {1, 0}, {1.2, 1}, {1.5, 2}, {2, 4}, {1.35, 1.5}} {
		speed, err := tencentSpeed(v[0])
		if err != nil || speed != v[1] {
			t.Fatalf("rate%v speed%v err%v", v[0], speed, err)
		}
	}
	if _, err := tencentSpeed(.5); err == nil {
		t.Fatal("unsupported rate accepted")
	}
}

func TestTencentSignatureKnownVector(t *testing.T) {
	params := url.Values{"Action": {"TextToStreamAudioWSv2"}, "AppId": {"1234"}, "Codec": {"pcm"}, "Timestamp": {"1700000000"}}
	address, err := tencentSignedURL("wss://tts.cloud.tencent.com/stream_wsv2", "test-key", params)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(address)
	if u.Query().Get("Signature") != "Y3HgkuyrQUSlnIR5O5VJgcVrEzs=" {
		t.Fatal("known HMAC-SHA1 vector mismatch")
	}
}
func TestTencentAudioCachePrivateRangeAndExpiry(t *testing.T) {
	raw := tencentPCMToWAV([]byte{1, 2, 3, 4})
	address, err := cacheTencentWAV(t.Context(), raw)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(address)
	id := strings.TrimPrefix(u.Path, "/internal/v1/tts-audio/")
	req := httptest.NewRequest("GET", u.String(), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.SetPathValue("audioID", id)
	req.Header.Set("Range", "bytes=44-47")
	w := httptest.NewRecorder()
	ServeTencentAudio(w, req)
	if w.Code != 206 || !bytes.Equal(w.Body.Bytes(), raw[44:]) {
		t.Fatalf("range failed: %d", w.Code)
	}
	req.RemoteAddr = "203.0.113.1:12345"
	w = httptest.NewRecorder()
	ServeTencentAudio(w, req)
	if w.Code != 404 {
		t.Fatal("external audio access allowed")
	}
	tencentAudio.Lock()
	item := tencentAudio.items[id]
	item.expiry = time.Now().Add(-time.Second)
	tencentAudio.items[id] = item
	tencentAudio.Unlock()
	req.RemoteAddr = "127.0.0.1:12345"
	w = httptest.NewRecorder()
	ServeTencentAudio(w, req)
	if w.Code != 404 {
		t.Fatal("expired audio served")
	}
}
