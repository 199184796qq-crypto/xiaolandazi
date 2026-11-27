package httpapi

import (
	"context"
	"log"
	"math"
	"strings"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// attributeEmbeddingThreshold accepts an embedding fallback only when it is
// close enough to a canonical attribute; weaker matches stay as pending raw
// attributes for the draft editor.
const attributeEmbeddingThreshold = 0.55

// normalizeAgentMaterialAttributes converts the model's free-form attribute
// names into canonical catalog names before the draft preview is returned.
// Raw names are preserved in details_json for audit and later re-normalization.
func (s *Server) normalizeAgentMaterialAttributes(ctx context.Context, items []model.PlanMaterialClaimInput) []model.PlanMaterialClaimInput {
	if len(items) == 0 {
		return items
	}
	catalog, err := s.store.LoadFactAttributeCatalog(ctx)
	if err != nil || len(catalog.Attributes) == 0 {
		if err != nil {
			log.Printf("fact attribute catalog unavailable, draft keeps raw attributes: %v", err)
		}
		return items
	}
	entryByID := make(map[int64]db.FactAttributeCatalogEntry, len(catalog.Attributes))
	for _, entry := range catalog.Attributes {
		entryByID[entry.ID] = entry
	}
	type pendingIndex struct {
		index int
		text  string
	}
	pending := []pendingIndex{}
	for index := range items {
		item := &items[index]
		// 营销表达等非事实条目不做属性归一，避免把“营销表达”映射到政策属性。
		if item.Destination != "" && item.Destination != "fact" {
			continue
		}
		raw := strings.TrimSpace(item.Attribute)
		if raw == "" {
			continue
		}
		item.RawAttribute = raw
		key := db.NormalizeAttributeKey(raw)
		if id, ok := catalog.Aliases[key]; ok {
			applyCanonicalAttribute(item, entryByID[id].Name, "alias")
			continue
		}
		if id, ok := catalog.ByName[key]; ok {
			applyCanonicalAttribute(item, entryByID[id].Name, "canonical")
			continue
		}
		pending = append(pending, pendingIndex{index: index, text: raw})
	}
	if len(pending) == 0 {
		return items
	}
	embedder, ok := s.semanticMetrics.(danmakuEmbedder)
	if !ok || !embedder.Enabled() || embedder.Model() == "" {
		return items
	}
	texts := make([]string, 0, len(pending))
	for _, entry := range pending {
		texts = append(texts, "属性："+entry.text)
	}
	vectors, err := embedMaterialTexts(ctx, embedder, texts)
	if err != nil || len(vectors) != len(texts) {
		log.Printf("fact attribute fallback embedding failed, draft keeps raw attributes: %v", err)
		return items
	}
	for offset, entry := range pending {
		bestName := ""
		bestScore := 0.0
		for _, attribute := range catalog.Attributes {
			if len(attribute.Vector) != len(vectors[offset]) {
				continue
			}
			if score := cosineFloat32(vectors[offset], attribute.Vector); score > bestScore {
				bestScore = score
				bestName = attribute.Name
			}
		}
		if bestName != "" && bestScore >= attributeEmbeddingThreshold {
			applyCanonicalAttribute(&items[entry.index], bestName, "embedding")
		}
	}
	return items
}

func applyCanonicalAttribute(item *model.PlanMaterialClaimInput, canonical, match string) {
	if canonical == "" {
		return
	}
	if item.RawAttribute == "" {
		item.RawAttribute = item.Attribute
	}
	item.AttributeMatch = match
	item.Attribute = canonical
	if strings.TrimSpace(item.Topic) == "" || item.Topic == item.RawAttribute {
		item.Topic = canonical
	}
	if strings.TrimSpace(item.ClaimKey) == "" || item.ClaimKey == item.RawAttribute || item.ClaimKey == "claim" {
		item.ClaimKey = canonical
	}
}

func cosineFloat32(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
