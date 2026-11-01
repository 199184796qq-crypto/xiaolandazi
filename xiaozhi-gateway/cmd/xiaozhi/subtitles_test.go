package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRoomTextClearsMissingCaptionsWithoutRepeatingOrStoppingAudio(t *testing.T) {
	var mu sync.Mutex
	body := `{"speech_feed":{}}`
	status := 200
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer core.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		started := make(chan struct{})
		close(started)
		g := newGateway(config{coreBaseURL: core.URL}, &bindingStore{data: map[string]int64{}})
		g.streamRoomText(ctx, &lockedConn{conn: conn}, 29, "session", started)
	}))
	defer server.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	messages := make(chan map[string]any, 16)
	go func() {
		defer close(messages)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg map[string]any
			if json.Unmarshal(raw, &msg) == nil {
				messages <- msg
			}
		}
	}()
	expect := func(want string) {
		t.Helper()
		select {
		case msg := <-messages:
			if msg["type"] != "tts" || msg["state"] != "sentence_start" || msg["text"] != want {
				t.Fatalf("want=%q got=%v", want, msg)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no caption update %q", want)
		}
	}
	quiet := func() {
		t.Helper()
		select {
		case msg := <-messages:
			t.Fatalf("unexpected caption/control %v", msg)
		case <-time.After(240 * time.Millisecond):
		}
	}
	set := func(raw string, code int) { mu.Lock(); body, status = raw, code; mu.Unlock() }
	expect("") // Also clear retained terminal text on a new connection.
	quiet()
	set(`{"speech_feed":{"current":{"segment_id":"real","text":"真实字幕"}}}`, 200)
	expect("真实字幕")
	quiet()
	set(`{"error":"temporarily unavailable"}`, 503)
	quiet()
	set(`{invalid`, 200)
	quiet()
	set(`{"speech_feed":{}}`, 200)
	expect("")
	quiet()
	set(`{"speech_feed":{"current":{"segment_id":"real","text":"真实字幕"}}}`, 200)
	expect("真实字幕")
	set(`{"speech_feed":{"current":{"segment_id":"blank","text":" "}}}`, 200)
	expect("")
}
