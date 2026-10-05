package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	baseURL := flag.String("base-url", "https://www.xiaolandaizi.cn", "public HTTPS base URL")
	deviceID := flag.String("device-id", "", "Device-Id used for the smoke client")
	clientID := flag.String("client-id", "", "Client-Id used for the smoke client")
	timeout := flag.Duration("timeout", 12*time.Second, "overall smoke timeout")
	flag.Parse()

	if strings.TrimSpace(*deviceID) == "" || strings.TrimSpace(*clientID) == "" {
		fmt.Fprintln(os.Stderr, "device-id and client-id are required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	base := strings.TrimRight(strings.TrimSpace(*baseURL), "/")
	otaURL := base + "/xiaozhi/ota/"
	payload := []byte(`{"application":{"version":"production-smoke"}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, otaURL, bytes.NewReader(payload))
	if err != nil {
		fail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Device-Id", *deviceID)
	req.Header.Set("Client-Id", *clientID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(fmt.Errorf("ota request: %w", err))
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	if readErr != nil {
		fail(readErr)
	}
	if resp.StatusCode != http.StatusOK {
		fail(fmt.Errorf("ota http=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	var ota struct {
		WebSocket struct {
			URL     string `json:"url"`
			Token   string `json:"token"`
			Version int    `json:"version"`
		} `json:"websocket"`
		Xiaolan struct {
			Bound  bool  `json:"bound"`
			RoomID int64 `json:"room_id"`
		} `json:"xiaolan"`
	}
	if err := json.Unmarshal(body, &ota); err != nil {
		fail(fmt.Errorf("decode ota: %w", err))
	}
	if ota.WebSocket.URL == "" || ota.WebSocket.Token == "" || ota.WebSocket.Version != 1 {
		fail(errors.New("ota did not return websocket v1 configuration"))
	}
	if !ota.Xiaolan.Bound || ota.Xiaolan.RoomID <= 0 {
		fail(errors.New("smoke device is not bound to a room"))
	}

	wsURL, err := url.Parse(ota.WebSocket.URL)
	if err != nil {
		fail(err)
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+ota.WebSocket.Token)
	headers.Set("Protocol-Version", "1")
	headers.Set("Device-Id", *deviceID)
	headers.Set("Client-Id", *clientID)

	dialer := *websocket.DefaultDialer
	conn, response, err := dialer.DialContext(ctx, wsURL.String(), headers)
	if err != nil {
		if response != nil {
			fail(fmt.Errorf("websocket dial: %w http=%d", err, response.StatusCode))
		}
		fail(fmt.Errorf("websocket dial: %w", err))
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(*timeout))

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
		fail(err)
	}

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			fail(fmt.Errorf("read websocket: %w", err))
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var hello struct {
			Type       string `json:"type"`
			Transport  string `json:"transport"`
			SessionID  string `json:"session_id"`
			AudioParams struct {
				Format      string `json:"format"`
				SampleRate  int    `json:"sample_rate"`
				Channels    int    `json:"channels"`
				FrameMS     int    `json:"frame_duration"`
			} `json:"audio_params"`
		}
		if json.Unmarshal(message, &hello) != nil || hello.Type != "hello" {
			continue
		}
		if hello.Transport != "websocket" || hello.AudioParams.Format != "opus" || hello.AudioParams.SampleRate != 24000 {
			fail(fmt.Errorf("unexpected server hello: %s", strings.TrimSpace(string(message))))
		}
		_ = conn.WriteJSON(map[string]any{"type": "goodbye", "session_id": hello.SessionID})
		fmt.Printf("xiaozhi production websocket smoke ok room=%d session=%s sample_rate=%d frame_ms=%d\n",
			ota.Xiaolan.RoomID, hello.SessionID, hello.AudioParams.SampleRate, hello.AudioParams.FrameMS)
		return
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
