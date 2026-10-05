package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientEmbedsAndPreservesOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("missing authorization")
		}
		var payload embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != defaultModel || len(payload.Input) != 2 {
			t.Fatalf("payload=%+v", payload)
		}
		first := make([]float32, defaultDimension)
		second := make([]float32, defaultDimension)
		first[0] = 1
		second[1] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": second},
				{"index": 0, "embedding": first},
			},
		})
	}))
	defer server.Close()

	client := New(Config{
		Enabled:    true,
		BaseURL:    server.URL,
		APIKey:     "secret",
		Model:      defaultModel,
		BatchSize:  20,
		Timeout:    time.Second,
		Dimensions: defaultDimension,
	})
	vectors, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("unexpected vector ordering")
	}
}

func TestClientFallsBackToResponseOrderWhenProviderRepeatsIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Dimensions != defaultDimension {
			t.Fatalf("dimensions=%d want=%d", payload.Dimensions, defaultDimension)
		}
		first := make([]float32, defaultDimension)
		second := make([]float32, defaultDimension)
		first[0] = 1
		second[1] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": first},
				{"index": 0, "embedding": second},
			},
		})
	}))
	defer server.Close()

	client := New(Config{
		Enabled: true, BaseURL: server.URL, APIKey: "secret", Model: defaultModel,
		BatchSize: 20, Timeout: time.Second, Dimensions: defaultDimension,
	})
	vectors, err := client.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("provider response-order fallback failed")
	}
}

func TestClientBatchesAtTwenty(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		data := make([]map[string]any, len(payload.Input))
		for index := range payload.Input {
			vector := make([]float32, defaultDimension)
			vector[0] = float32(index + 1)
			data[index] = map[string]any{"index": index, "embedding": vector}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer server.Close()

	client := New(Config{
		Enabled:    true,
		BaseURL:    server.URL,
		APIKey:     "secret",
		Model:      defaultModel,
		BatchSize:  99,
		Timeout:    time.Second,
		Dimensions: defaultDimension,
	})
	texts := make([]string, 21)
	for index := range texts {
		texts[index] = "x"
	}
	if _, err := client.Embed(context.Background(), texts); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d want=2", calls.Load())
	}
}

func TestDisabledClientFailsClosed(t *testing.T) {
	client := New(Config{Enabled: false, APIKey: "secret"})
	if client.Enabled() {
		t.Fatal("disabled client must not report enabled")
	}
	if _, err := client.Embed(context.Background(), []string{"x"}); err != ErrDisabled {
		t.Fatalf("err=%v", err)
	}
}

func TestCosineSimilarity(t *testing.T) {
	if score := CosineSimilarity([]float32{1, 0}, []float32{1, 0}); score != 1 {
		t.Fatalf("same vector score=%v", score)
	}
	if score := CosineSimilarity([]float32{1, 0}, []float32{0, 1}); score != 0 {
		t.Fatalf("orthogonal score=%v", score)
	}
}
