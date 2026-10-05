package agentgateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpeechDiscoveryProtocols(t *testing.T) {
	for _, tc := range []struct{ protocol, suffix, header, value, reply string }{
		{"openai_chat", "/chat/completions", "Authorization", "Bearer fixture-only-secret", `{"data":[{"id":"z","name":"Z model"},{"id":"a"},{"id":"a"},{"id":""},{"id":"fixture-only-secret"}]}`},
		{"openai_responses", "/responses", "Authorization", "Bearer fixture-only-secret", `{"data":[{"id":"z","name":"Z model"},{"id":"a"}]}`},
		{"anthropic", "/messages", "x-api-key", "fixture-only-secret", `{"data":[{"id":"z","display_name":"Z model"},{"id":"a"}],"has_more":false}`},
		{"gemini", "/models/old:generateContent", "x-goog-api-key", "fixture-only-secret", `{"models":[{"name":"models/z","displayName":"Z model","supportedGenerationMethods":["generateContent"]},{"name":"models/a","supportedGenerationMethods":["generateContent"]},{"name":"models/embedding","supportedGenerationMethods":["embedContent"]}]}`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/v1/models" || r.Header.Get(tc.header) != tc.value || strings.Contains(r.URL.String(), "fixture-only-secret") {
					t.Errorf("bad discovery request: %s %s", r.Method, r.URL.Path)
				}
				if tc.protocol == "anthropic" && r.Header.Get("anthropic-version") == "" {
					t.Error("missing version")
				}
				_, _ = io.WriteString(w, tc.reply)
			}))
			defer server.Close()
			p := speechFixture()
			p.Protocol = tc.protocol
			p.BaseURL = server.URL + "/v1" + tc.suffix
			p.Model = ""
			p.Name = ""
			p.TimeoutMS = 0
			p.MaxTokens = 0
			if err := ValidateSpeechConnection(p); err != nil {
				t.Fatal(err)
			}
			got, err := listSpeechEndpointModels(context.Background(), p, server.Client())
			if err != nil || calls != 1 || len(got.Models) != 2 || got.Models[0].ID != "a" || got.Models[1].Name != "Z model" || got.Truncated {
				t.Fatalf("discovery: %+v %v", got, err)
			}
			if p.Model != "" {
				t.Fatal("discovery changed selected model")
			}
		})
	}
}

func TestSpeechDiscoveryPagination(t *testing.T) {
	for _, protocol := range []string{"gemini", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				cursor := r.URL.Query().Get("after_id")
				if protocol == "gemini" {
					cursor = r.URL.Query().Get("pageToken")
				}
				if calls == 2 && cursor != "a" {
					t.Errorf("bad cursor %q", cursor)
				}
				if protocol == "gemini" {
					if calls == 1 {
						_, _ = io.WriteString(w, `{"models":[{"name":"models/a","supportedGenerationMethods":["generateContent"]}],"nextPageToken":"a"}`)
					} else {
						_, _ = io.WriteString(w, `{"models":[{"name":"models/b","supportedGenerationMethods":["generateContent"]}]}`)
					}
				} else {
					if calls == 1 {
						_, _ = io.WriteString(w, `{"data":[{"id":"a"}],"has_more":true,"last_id":"a"}`)
					} else {
						_, _ = io.WriteString(w, `{"data":[{"id":"b"}],"has_more":false}`)
					}
				}
			}))
			defer s.Close()
			p := speechFixture()
			p.Protocol = protocol
			p.BaseURL = s.URL + "/v1"
			got, err := listSpeechEndpointModels(context.Background(), p, s.Client())
			if err != nil || calls != 2 || len(got.Models) != 2 {
				t.Fatalf("pagination: %+v %v", got, err)
			}
		})
	}
}

func TestSpeechDiscoveryFailureAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"unauthorized", "fixture-only-secret", 401, true},
		{"unsupported", "fixture-only-secret", 404, true},
		{"malformed", "not-json fixture-only-secret", 200, true},
		{"wrong-format", `{"error":"fixture-only-secret"}`, 200, true},
		{"empty", `{"data":[]}`, 200, false},
		{"oversized", strings.Repeat("x", (4<<20)+1), 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			p := speechFixture()
			p.BaseURL = s.URL
			_, err := listSpeechEndpointModels(context.Background(), p, s.Client())
			if (err != nil) != tc.wantError {
				t.Fatalf("error: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), p.APIKey) {
				t.Fatal("upstream credential leaked")
			}
		})
	}
	p := speechFixture()
	p.BaseURL = "https://127.0.0.1/v1"
	if _, err := ListSpeechEndpointModels(context.Background(), p); err == nil {
		t.Fatal("private address accepted")
	}
	p.BaseURL = "http://api.example.com/v1"
	if _, err := ListSpeechEndpointModels(context.Background(), p); err == nil {
		t.Fatal("insecure URL accepted")
	}
	p.BaseURL = "https://api.example.com/v1?key=secret"
	if _, err := ListSpeechEndpointModels(context.Background(), p); err == nil {
		t.Fatal("key URL accepted")
	}
	p = speechFixture()
	p.APIKey = ""
	if _, err := ListSpeechEndpointModels(context.Background(), p); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestSpeechDiscoveryStopsRepeatedCursor(t *testing.T) {
	calls := 0
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"models":[],"nextPageToken":"same"}`)
	}))
	defer s.Close()
	p := speechFixture()
	p.Protocol = "gemini"
	p.BaseURL = s.URL
	if _, err := listSpeechEndpointModels(context.Background(), p, s.Client()); err == nil || calls != 2 {
		t.Fatalf("pagination loop: calls=%d err=%v", calls, err)
	}
}
