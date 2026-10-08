package agentstudio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestInterruptUsesUserControlEventWithoutNewMessage(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost || r.URL.Path != "/sessions/sesn_fixture/events" || r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatal(r.Method, r.URL.Path)
		}
		var body struct {
			Input []map[string]any `json:"input"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Input) != 1 || body.Input[0]["type"] != "interrupt" || body.Input[0]["role"] != "user" {
			t.Fatal(body)
		}
		if _, ok := body.Input[0]["content"]; ok {
			t.Fatal("interrupt must not send another generation message")
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	client, err := NewWithBaseURL(server.URL, "fixture-key", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.InterruptSession(context.Background(), "sesn_fixture"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("upstream not interrupted")
	}
	if err = client.InterruptSession(context.Background(), "invalid"); err == nil {
		t.Fatal("invalid session accepted")
	}
}
