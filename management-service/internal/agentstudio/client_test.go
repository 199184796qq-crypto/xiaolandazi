package agentstudio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateSessionSendMessageAndArchive(t *testing.T) {
	var mu sync.Mutex
	posted := make(chan struct{})
	postedOnce := sync.Once{}
	var postedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatalf("missing authorization: %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/sessions":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"sesn_fixture","status":"idle","agent":{"id":"agent_fixture","version":2,"name":"带货主播","model":{"id":"qwen3.8-max"}},"environment_id":"env_fixture","created_at":"2026-10-07T00:00:00Z"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/sessions/sesn_fixture/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("response writer is not a flusher")
			}
			fmt.Fprint(w, ": connected\n\n")
			flusher.Flush()
			select {
			case <-posted:
			case <-time.After(2 * time.Second):
				t.Fatal("event was not posted after stream connected")
			}
			fmt.Fprint(w, "data: {\"object\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"text\",\"text\":\"  哥哥姐姐们，测试口播啊！  \"}]}\n\n")
			fmt.Fprint(w, "data: {\"object\":\"message\",\"id\":\"status_1\",\"type\":\"session_status\",\"status\":\"completed\",\"content\":[{\"type\":\"data\",\"data\":{\"session_status\":\"idle\",\"stop_reason\":{\"type\":\"end_turn\"}}}]}\n\n")
			flusher.Flush()
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/sesn_fixture/events":
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			postedBody = string(raw)
			mu.Unlock()
			postedOnce.Do(func() { close(posted) })
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"accepted":true}`)
		case r.Method == http.MethodPost && r.URL.Path == "/sessions/sesn_fixture/archive":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewWithBaseURL(server.URL, "fixture-key", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	session, err := client.CreateSession(context.Background(), "agent_fixture", "env_fixture", "测试", map[string]string{"plan_id": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "sesn_fixture" || session.Agent.Model.ID != "qwen3.8-max" || session.Agent.Version != 2 {
		t.Fatalf("unexpected session: %+v", session)
	}
	result, err := client.SendMessageAndWait(context.Background(), session.ID, "继续说下一段")
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "哥哥姐姐们，测试口播啊！" || result.StopReason != "end_turn" {
		t.Fatalf("unexpected turn: %+v", result)
	}
	if result.RawText != "  哥哥姐姐们，测试口播啊！  " {
		t.Fatal("raw model return was normalized")
	}
	mu.Lock()
	body := postedBody
	mu.Unlock()
	if !strings.Contains(body, "继续说下一段") || !strings.Contains(body, `"type":"message"`) {
		t.Fatalf("unexpected posted event: %s", body)
	}
	if err := client.ArchiveSession(context.Background(), session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsInvalidResourceIDs(t *testing.T) {
	client, err := NewWithBaseURL("https://example.com", "fixture", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.CreateSession(context.Background(), "bad", "env_ok", "", nil); err == nil {
		t.Fatal("invalid agent id should be rejected")
	}
	if _, err = client.SendMessageAndWait(context.Background(), "bad", "hello"); err == nil {
		t.Fatal("invalid session id should be rejected")
	}
}

func TestAcceptsConsoleBusinessWorkspaceID(t *testing.T) {
	client, err := New("llm-n40t10t2qmyemkx9", "cn-beijing", "fixture", time.Minute)
	if err != nil {
		t.Fatalf("llm business workspace id should be accepted: %v", err)
	}
	if !strings.Contains(client.baseURL, "llm-n40t10t2qmyemkx9.cn-beijing.maas.aliyuncs.com") {
		t.Fatalf("unexpected base url: %s", client.baseURL)
	}
}

func TestListEnvironmentsFiltersArchived(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/environments" || r.URL.Query().Get("limit") != "100" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"env_default","name":"默认环境","archived_at":null},{"id":"env_archived","name":"旧环境","archived_at":"2026-10-01T00:00:00Z"},{"id":"invalid","name":"坏数据","archived_at":null}]}`)
	}))
	defer server.Close()
	client, err := NewWithBaseURL(server.URL, "fixture", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.ListEnvironments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "env_default" {
		t.Fatalf("unexpected environments: %+v", items)
	}
}

func TestListAgentsFiltersArchivedAndKeepsModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agents" || r.URL.Query().Get("limit") != "100" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"agent_anchor","name":"带货主播","version":3,"model":{"id":"qwen3.8-max"},"archived_at":null},{"id":"agent_old","name":"旧主播","version":1,"model":{"id":"qwen-plus"},"archived_at":"2026-10-01T00:00:00Z"},{"id":"invalid","name":"坏数据","version":1,"model":{"id":"qwen-plus"},"archived_at":null}]}`)
	}))
	defer server.Close()
	client, err := NewWithBaseURL(server.URL, "fixture", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	items, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "agent_anchor" || items[0].Model.ID != "qwen3.8-max" {
		t.Fatalf("unexpected agents: %+v", items)
	}
}
