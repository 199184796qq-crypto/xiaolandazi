package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/auth"
)

func TestCustomerAgentImageValidatesBytes(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9ZrL0AAAAASUVORK5CYII="
	for _, test := range []struct {
		image customerAgentImage
		valid bool
	}{
		{customerAgentImage{"image/png", png}, true},
		{customerAgentImage{"image/jpeg", png}, false},
		{customerAgentImage{"image/png", "not-base64"}, false},
		{customerAgentImage{"image/svg+xml", "PHN2Zz4="}, false},
		{customerAgentImage{"image/png", strings.Repeat("A", 6<<20)}, false},
	} {
		if valid := test.image.valid(); valid != test.valid {
			t.Fatalf("unexpected validity for %s (%d encoded bytes): %t", test.image.MIME, len(test.image.Data), valid)
		}
	}
}

func TestCustomerAgentChatRequiresLogin(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{auth: auth.NewResolver("test", nil)}).registerCustomerAgentChat(mux)
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, customerAgentChatPath},
		{http.MethodPost, customerAgentChatPath},
		{http.MethodPost, customerAgentChatOpenPath},
		{http.MethodPost, customerAgentFactEditPath + "/preview"},
		{http.MethodPost, customerAgentFactEditPath + "/confirm"},
		{http.MethodPost, customerFactEditPath + "/confirm"},
		{http.MethodGet, customerFactEditPath + "/results"},
	} {
		request := httptest.NewRequest(test.method, test.path+"?user_id=42", nil)
		request.Header.Set("X-Xiaolan-Account", "42")
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s response = %d %s", test.method, test.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestCustomerAgentPlanSupportEndpointScope(t *testing.T) {
	for _, route := range []struct {
		method, path, slug string
		allowed            bool
	}{
		{http.MethodPost, customerAgentChatOpenPath, "", true},
		{http.MethodPost, customerAgentChatPath, "", true},
		{http.MethodPost, customerAgentFactEditPath + "/preview", "", true},
		{http.MethodPost, customerAgentFactEditPath + "/confirm", "", true},
		{http.MethodGet, customerAgentFactEditPath + "/confirm", "", false},
		{http.MethodGet, customerAgentChatPath, "", false},
		{http.MethodPost, customerFactEditPath + "/confirm", "", true},
		{http.MethodGet, customerFactEditPath + "/results", "", true},
		{http.MethodPost, customerFactEditPath + "/results", "", false},
		{http.MethodGet, customerAgentSkillsPath, "", true},
		{http.MethodPost, customerAgentSkillsPath, "", true},
		{http.MethodGet, customerAgentSkillsPath + "/mine", "mine", true},
		{http.MethodPatch, customerAgentSkillsPath + "/mine", "mine", true},
		{http.MethodDelete, customerAgentSkillsPath + "/mine", "mine", true},
		{http.MethodPost, customerAgentChatOpenPath + "?support_room_id=29", "", false},
	} {
		r := httptest.NewRequest(route.method, route.path, nil)
		r.SetPathValue("slug", route.slug)
		if got := livePlanSupportEndpointAllowed(r, 9); got != route.allowed {
			t.Errorf("%s %s scoped permission = %v, want %v", route.method, route.path, got, route.allowed)
		}
	}
}

type agentChatTestTransport func(*http.Request) (*http.Response, error)

func (fn agentChatTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func TestCustomerAgentChatRelayUsesAuthenticatedAccount(t *testing.T) {
	original := customerAgentClient
	t.Cleanup(func() { customerAgentClient = original })
	customerAgentClient = &http.Client{Transport: agentChatTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/conversation" || r.URL.RawQuery != "" || r.Header.Get("X-Xiaolan-Account") != "7" {
			t.Fatalf("unexpected internal routing or account")
		}
		if r.Header.Get("Cookie") != "" {
			t.Fatal("forwarded browser credentials")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"message":"你好"}` {
			t.Fatalf("unexpected body = %s, %v", body, err)
		}
		return &http.Response{StatusCode: http.StatusTooManyRequests,
			Body: io.NopCloser(strings.NewReader(`{"error":"智能体正在回复其他消息"}`))}, nil
	})}

	request := httptest.NewRequest(http.MethodPost, customerAgentChatPath+"?user_id=42", nil)
	request.Header.Set("X-Xiaolan-Account", "42")
	request.Header.Set("Cookie", "session=private")
	recorder := httptest.NewRecorder()
	relayCustomerAgentChat(recorder, request, 7, []byte(`{"message":"你好"}`))
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestCustomerAgentChatRelayFlushesThinkingBeforeCompletion(t *testing.T) {
	client := customerAgentClient
	t.Cleanup(func() { customerAgentClient = client })
	started := make(chan struct{})
	finish := make(chan struct{})
	complete := "event: complete\ndata: {\"messages\":[]}\n\n"
	progress := "event: progress\ndata: {\"thinking_delta\":\"正在整理\"}\n\n"
	customerAgentClient = &http.Client{Transport: agentChatTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-Xiaolan-Account") != "7" || r.Header.Get("Accept") != "text/event-stream" {
			t.Error("stream negotiation did not use authenticated account")
		}
		in, out := io.Pipe()
		go func() {
			defer out.Close()
			_, _ = io.WriteString(out, progress)
			close(started)
			select {
			case <-finish:
				_, _ = io.WriteString(out, complete)
			case <-r.Context().Done():
			}
		}()
		return &http.Response{StatusCode: http.StatusOK, Body: in,
			Header: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}}, nil
	})}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relayCustomerAgentChat(w, r, 7, nil)
	}))
	defer server.Close()
	defer close(finish)
	request, err := http.NewRequest(http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/event-stream")
	response, err := (&http.Client{Timeout: 2 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	chunk := make([]byte, len(progress))
	if _, err := io.ReadFull(response.Body, chunk); err != nil || string(chunk) != progress {
		t.Fatalf("expected progress before completion, got %q (%v)", chunk, err)
	}
	<-started
	if response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatal("proxy buffering not disabled")
	}
}
