package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestManagedUnclaimedDeviceDisplaysCodeWithoutRoomAudio(t *testing.T) {
	var audioCalls atomic.Int64
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { audioCalls.Add(1); http.Error(w, "must not start", 500) }))
	defer core.Close()
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Xiaozhi-Internal-Token") != "test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path != "/internal/v1/xiaozhi/provision" {
			t.Errorf("unexpected management path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		writeJSON(w, 200, provisioning{DeviceID: 25, State: "claimable", Message: "请添加设备", Code: "032518"})
	}))
	defer management.Close()
	g := newGateway(config{managementBaseURL: management.URL, coreBaseURL: core.URL, internalToken: "test-token", secret: "secret"}, &bindingStore{data: map[string]int64{"1c:29:04:31:0e:b8": 99}})
	server := httptest.NewServer(g.handler())
	defer server.Close()
	headers := http.Header{"Device-Id": []string{"1c:29:04:31:0e:b8"}, "Client-Id": []string{"client"}, "Authorization": []string{"Bearer " + g.deviceToken("1c:29:04:31:0e:b8", "client")}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/xiaozhi/v1/", headers)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteJSON(map[string]any{"type": "hello", "version": 1, "transport": "websocket"}); err != nil {
		t.Fatal(err)
	}
	var hello map[string]any
	if err := conn.ReadJSON(&hello); err != nil || hello["type"] != "hello" {
		t.Fatal(hello, err)
	}
	var status map[string]any
	if err := conn.ReadJSON(&status); err != nil || status["binding_code"] != "032518" || status["type"] != "device_status" {
		t.Fatal(status, err)
	}
	if audioCalls.Load() != 0 {
		t.Fatal("legacy local room bypassed claim requirement")
	}
}

func TestManagedOTAContainsNoMACAndFailsClosed(t *testing.T) {
	management := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, provisioning{DeviceID: 25, State: "claimable", Code: "032518"})
	}))
	defer management.Close()
	g := newGateway(config{managementBaseURL: management.URL, secret: "secret"}, &bindingStore{data: map[string]int64{}})
	r := httptest.NewRequest(http.MethodPost, "/xiaozhi/ota/", strings.NewReader(`{"application":{"version":"1.2.3"}}`))
	r.Header.Set("Device-Id", "1c:29:04:31:0e:b8")
	r.Header.Set("Client-Id", "client")
	w := httptest.NewRecorder()
	g.otaPost(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "1c:29") {
		t.Fatal(w.Code, w.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["activation"]; ok {
		t.Fatal("official activation loop must not run")
	}
	management.Close()
	w = httptest.NewRecorder()
	g.otaPost(w, r)
	if w.Code != 503 {
		t.Fatal("management failure accepted", w.Code)
	}
}
