package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDeviceTokenRoundTrip(t *testing.T) {
	g := newGateway(config{secret: "test-secret"}, &bindingStore{data: map[string]int64{}})
	token := g.deviceToken("AA:BB:CC:DD:EE:FF", "client-1")
	if token == "" {
		t.Fatal("token is empty")
	}
	if !g.validAuthorization("Bearer "+token, "aa:bb:cc:dd:ee:ff", "client-1") {
		t.Fatal("generated token should validate")
	}
	if g.validAuthorization("Bearer "+token, "aa:bb:cc:dd:ee:ff", "client-2") {
		t.Fatal("token must be bound to client id")
	}
}

func TestWebSocketV1StreamsOpusFromCompositePCM(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	encoders, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil || !strings.Contains(string(encoders), "libopus") {
		t.Skip("ffmpeg libopus encoder is not available")
	}

	pcm := make([]byte, 24000*2*180/1000)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rooms/15/composite.pcm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/L16;rate=24000;channels=1")
		_, _ = w.Write(pcm)
	}))
	defer core.Close()

	store := &bindingStore{data: map[string]int64{"aa:bb": 15}}
	cfg := config{
		coreBaseURL: core.URL,
		secret:      "test-secret",
		ffmpegPath:  ffmpegPath,
	}
	g := newGateway(cfg, store)
	server := httptest.NewServer(g.handler())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/xiaozhi/v1/"
	headers := http.Header{}
	headers.Set("Device-Id", "AA:BB")
	headers.Set("Client-Id", "client-1")
	headers.Set("Protocol-Version", "1")
	headers.Set("Authorization", "Bearer "+g.deviceToken("AA:BB", "client-1"))

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial: %v status=%d", err, resp.StatusCode)
		}
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	err = conn.WriteJSON(map[string]any{
		"type":      "hello",
		"version":   1,
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	gotHello := false
	gotStart := false
	gotAudio := false
	gotStop := false
	for i := 0; i < 12; i++ {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		switch messageType {
		case websocket.TextMessage:
			var message map[string]any
			if json.Unmarshal(payload, &message) != nil {
				continue
			}
			switch message["type"] {
			case "hello":
				gotHello = message["transport"] == "websocket"
			case "tts":
				if message["state"] == "start" {
					gotStart = true
				}
				if message["state"] == "stop" {
					gotStop = true
				}
			}
		case websocket.BinaryMessage:
			if len(payload) > 0 {
				gotAudio = true
			}
		}
		if gotHello && gotStart && gotAudio && gotStop {
			break
		}
	}

	if !gotHello || !gotStart || !gotAudio || !gotStop {
		t.Fatalf("hello=%v start=%v audio=%v stop=%v", gotHello, gotStart, gotAudio, gotStop)
	}
}

func TestWebSocketV1StreamsSentenceTextFromAudioEngine(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	encoders, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil || !strings.Contains(string(encoders), "libopus") {
		t.Skip("ffmpeg libopus encoder is not available")
	}

	pcm := make([]byte, 24000*2*600/1000)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/rooms/25/composite.pcm":
			w.Header().Set("Content-Type", "audio/L16;rate=24000;channels=1")
			flusher, _ := w.(http.Flusher)
			for offset := 0; offset < len(pcm); offset += 24000 * 2 * 20 / 1000 {
				end := offset + 24000*2*20/1000
				if end > len(pcm) {
					end = len(pcm)
				}
				_, _ = w.Write(pcm[offset:end])
				if flusher != nil {
					flusher.Flush()
				}
				time.Sleep(20 * time.Millisecond)
			}
		case "/v1/rooms/25/audio-engine":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"speech_feed": map[string]any{
					"current": map[string]any{
						"segment_id": "S001",
						"text":       "欢迎来到25号直播间",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer core.Close()

	store := &bindingStore{data: map[string]int64{"aa:25": 25}}
	g := newGateway(config{
		coreBaseURL: core.URL,
		secret:      "test-secret",
		ffmpegPath:  ffmpegPath,
	}, store)
	server := httptest.NewServer(g.handler())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/xiaozhi/v1/"
	headers := http.Header{}
	headers.Set("Device-Id", "AA:25")
	headers.Set("Client-Id", "client-text")
	headers.Set("Protocol-Version", "1")
	headers.Set("Authorization", "Bearer "+g.deviceToken("AA:25", "client-text"))

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial: %v status=%d", err, resp.StatusCode)
		}
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(7 * time.Second))
	if err := conn.WriteJSON(map[string]any{
		"type":      "hello",
		"version":   1,
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	}); err != nil {
		t.Fatal(err)
	}

	gotText := false
	for i := 0; i < 30; i++ {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message map[string]any
		if json.Unmarshal(payload, &message) != nil {
			continue
		}
		if message["type"] == "tts" && message["state"] == "sentence_start" && message["text"] == "欢迎来到25号直播间" {
			gotText = true
			break
		}
	}
	if !gotText {
		t.Fatal("xiaozhi sentence_start text was not forwarded")
	}
}

func TestBindingStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bindings.json")
	store, err := newBindingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.bind("AA:BB", 15); err != nil {
		t.Fatal(err)
	}
	again, err := newBindingStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if room, ok := again.room("aa:bb"); !ok || room != 15 {
		t.Fatalf("room=%d ok=%v", room, ok)
	}
}

func TestOTAPostReturnsBoundRoomAndSignedWebSocket(t *testing.T) {
	store := &bindingStore{data: map[string]int64{"aa:bb": 15}}
	g := newGateway(config{
		secret:      "test-secret",
		publicWSURL: "wss://www.xiaolandaizi.cn/xiaozhi/v1/",
	}, store)

	req := httptest.NewRequest(
		http.MethodPost,
		"/xiaozhi/ota/",
		bytes.NewBufferString(`{"application":{"version":"1.2.3"}}`),
	)
	req.Header.Set("Device-Id", "AA:BB")
	req.Header.Set("Client-Id", "client-1")
	rec := httptest.NewRecorder()

	g.otaPost(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	websocketConfig, ok := got["websocket"].(map[string]any)
	if !ok {
		t.Fatalf("missing websocket config: %#v", got)
	}
	if websocketConfig["url"] != "wss://www.xiaolandaizi.cn/xiaozhi/v1/" {
		t.Fatalf("unexpected websocket url: %#v", websocketConfig)
	}
	if websocketConfig["token"] == "" || websocketConfig["version"] != float64(1) {
		t.Fatalf("unexpected websocket auth: %#v", websocketConfig)
	}
	xiaolan, ok := got["xiaolan"].(map[string]any)
	if !ok || xiaolan["bound"] != true || xiaolan["room_id"] != float64(15) {
		t.Fatalf("unexpected binding: %#v", xiaolan)
	}
}

func TestReadOggPacketsReassemblesLacing(t *testing.T) {
	page := func(laces []byte, body []byte) []byte {
		header := make([]byte, 27)
		copy(header[:4], "OggS")
		header[4] = 0
		header[26] = byte(len(laces))
		out := append(header, laces...)
		return append(out, body...)
	}

	first := bytes.Repeat([]byte{1}, 255)
	second := []byte{2, 3, 4}
	stream := append(page([]byte{255}, first), page([]byte{3, 1}, append(second, 9))...)

	var packets [][]byte
	err := readOggPackets(bytes.NewReader(stream), func(packet []byte, _ int) error {
		packets = append(packets, packet)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 2 {
		t.Fatalf("packet count=%d", len(packets))
	}
	if len(packets[0]) != 258 || packets[0][257] != 4 {
		t.Fatalf("first packet len=%d tail=%d", len(packets[0]), packets[0][len(packets[0])-1])
	}
	if len(packets[1]) != 1 || packets[1][0] != 9 {
		t.Fatalf("second packet=%v", packets[1])
	}
}

func TestWebSocketV1RestartsTTSAfterCompositeIdleGap(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not available")
	}
	encoders, err := exec.Command(ffmpegPath, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil || !strings.Contains(string(encoders), "libopus") {
		t.Skip("ffmpeg libopus encoder is not available")
	}

	pcm := make([]byte, 24000*2*180/1000)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rooms/16/composite.pcm" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/L16;rate=24000;channels=1")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write(pcm)
		if flusher != nil {
			flusher.Flush()
		}
		time.Sleep(1600 * time.Millisecond)
		_, _ = w.Write(pcm)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer core.Close()

	store := &bindingStore{data: map[string]int64{"aa:cc": 16}}
	g := newGateway(config{
		coreBaseURL: core.URL,
		secret:      "test-secret",
		ffmpegPath:  ffmpegPath,
	}, store)
	server := httptest.NewServer(g.handler())
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/xiaozhi/v1/"
	headers := http.Header{}
	headers.Set("Device-Id", "AA:CC")
	headers.Set("Client-Id", "client-gap")
	headers.Set("Protocol-Version", "1")
	headers.Set("Authorization", "Bearer "+g.deviceToken("AA:CC", "client-gap"))

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		if resp != nil {
			t.Fatalf("websocket dial: %v status=%d", err, resp.StatusCode)
		}
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(7 * time.Second))

	if err := conn.WriteJSON(map[string]any{
		"type":      "hello",
		"version":   1,
		"transport": "websocket",
		"audio_params": map[string]any{
			"format":         "opus",
			"sample_rate":    16000,
			"channels":       1,
			"frame_duration": 60,
		},
	}); err != nil {
		t.Fatal(err)
	}

	starts := 0
	stops := 0
	audioFrames := 0
	for i := 0; i < 40; i++ {
		messageType, payload, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		if messageType == websocket.BinaryMessage {
			if len(payload) > 0 {
				audioFrames++
			}
			continue
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var message map[string]any
		if json.Unmarshal(payload, &message) != nil || message["type"] != "tts" {
			continue
		}
		if message["state"] == "start" {
			starts++
		}
		if message["state"] == "stop" {
			stops++
		}
		if starts >= 2 && stops >= 2 && audioFrames >= 2 {
			break
		}
	}

	if starts < 2 || stops < 2 || audioFrames < 2 {
		t.Fatalf("starts=%d stops=%d audio_frames=%d", starts, stops, audioFrames)
	}
}
