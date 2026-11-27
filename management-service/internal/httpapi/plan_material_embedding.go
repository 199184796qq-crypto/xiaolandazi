package httpapi

import (
	"context"
	"encoding/json"
	"log"

	"livecompanion/management/internal/model"
)

// materialClaimEmbedding computes vectors for facts using the same document
// text as retrieval. It is best-effort: an unavailable embedding provider must
// not block fact import or edits; a later backfill can fill the missing ones.
func (s *Server) materialClaimEmbedding(ctx context.Context, items []model.PlanMaterialClaimInput) (string, [][]float32) {
	embedder, ok := s.semanticMetrics.(danmakuEmbedder)
	if !ok || embedder == nil || !embedder.Enabled() || embedder.Model() == "" {
		return "", nil
	}
	texts := make([]string, len(items))
	for i := range items {
		texts[i] = materialDocumentText(items[i])
	}
	vectors, err := embedMaterialTexts(ctx, embedder, texts)
	if err != nil {
		log.Printf("material claim embedding failed, rows keep empty vector: %v", err)
		return "", nil
	}
	return embedder.Model(), vectors
}

// materialVectorsJSON converts the first vector to the JSON text stored in
// embedding_json, matching the existing material write helpers.
func materialVectorsJSON(vectors [][]float32) []byte {
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil
	}
	raw, _ := json.Marshal(vectors[0])
	return raw
}
