package semantic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCosineSimilarity(t *testing.T) {
	if score := CosineSimilarity([]float32{1, 0}, []float32{1, 0}); score != 1 {
		t.Fatalf("same vector score=%v", score)
	}
	if score := CosineSimilarity([]float32{1, 0}, []float32{0, 1}); score != 0 {
		t.Fatalf("orthogonal score=%v", score)
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

func TestLiveEmbeddingSmoke(t *testing.T) {
	if os.Getenv("SEMANTIC_LIVE_TEST") != "1" {
		t.Skip("set SEMANTIC_LIVE_TEST=1 to call the configured embedding provider")
	}
	config := ConfigFromEnv()
	config.Enabled = true
	client := New(config)
	if !client.Enabled() {
		t.Fatal("live semantic test requested but embedding provider is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vectors, err := client.Embed(ctx, []string{"这个多少钱", "什么价"})
	if err != nil {
		t.Fatalf("live embedding request failed: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("vector count=%d want=2", len(vectors))
	}
	for index, vector := range vectors {
		if len(vector) != defaultDimension {
			t.Fatalf("vector[%d] dimensions=%d want=%d", index, len(vector), defaultDimension)
		}
	}
	t.Logf("live embedding model=%s dimensions=%d pair_similarity=%.4f", client.Model(), len(vectors[0]), CosineSimilarity(vectors[0], vectors[1]))
}
