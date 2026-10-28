package httpapi

import (
	"net/http"
	"sort"
	"strings"

	"livecompanion/management/internal/purposelib"
)

// roomPlanPurposes returns the auto-generated purpose list for the room's plan,
// rebuilding it from facts, links and benefits on each read (pure grouping).
func (s *Server) roomPlanPurposes(w http.ResponseWriter, r *http.Request) {
	_, tenantID, planID, ok := s.planAssetScope(w, r)
	if !ok {
		return
	}

	specs, err := s.store.BuildPlanPurposes(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成目的清单失败")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"plan_id": planID, "items": specs})
		return
	}
	type suggestion struct {
		Key      string   `json:"key"`
		Name     string   `json:"name"`
		Keywords []string `json:"keywords"`
		Score    int      `json:"score"`
		Excludes []string `json:"suggested_excludes"`
		InOut    bool     `json:"in_out"`
	}
	normalized := normalizePurposeQuery(query)
	scored := make([]suggestion, 0, len(specs))
	for _, spec := range specs {
		score := 0
		for _, keyword := range spec.Keywords {
			key := normalizePurposeQuery(keyword)
			if key == "" {
				continue
			}
			if strings.Contains(normalized, key) {
				score += len([]rune(key))
			}
		}
		if score > 0 {
			scored = append(scored, suggestion{Key: spec.Key, Name: spec.Name, Keywords: spec.Keywords, Score: score, Excludes: commonExcludes(spec.Key, specs), InOut: spec.InOut})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if len(scored) > 3 {
		scored = scored[:3]
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan_id": planID, "query": query, "suggestions": scored})
}

func normalizePurposeQuery(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch r {
		case ' ', '\t', '\n', '，', '。', '！', '？', '、', '；', '：', '?', '!', ',', '.', '~', '～':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// commonExcludes suggests words that usually mean a different purpose.
func commonExcludes(chosen string, specs []purposelib.Spec) []string {
	ambiguous := map[string]bool{"price": true, "promotion": true, "shipping_fee": true, "benefit": true}
	out := make([]string, 0, 6)
	seen := map[string]bool{}
	for _, spec := range specs {
		if spec.Key == chosen || !ambiguous[spec.Key] {
			continue
		}
		for _, keyword := range spec.Keywords {
			if len([]rune(keyword)) > 3 || seen[keyword] {
				continue
			}
			seen[keyword] = true
			out = append(out, keyword)
			if len(out) >= 6 {
				return out
			}
		}
	}
	return out
}
