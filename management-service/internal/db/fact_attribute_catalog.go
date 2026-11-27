package db

import (
	"context"
	"encoding/json"
	"strings"
)

// FactAttributeCatalogEntry is one active canonical attribute. Vector is the
// stored embedding of "属性名 + 说明"; it is only used as a fallback when the
// model's raw attribute has no alias match.
type FactAttributeCatalogEntry struct {
	ID        int64
	Key       string
	Name      string
	ScopeType string
	Industry  string
	Vector    []float32
}

// FactAttributeCatalog is a read-only snapshot of the system attribute catalog.
// Aliases are keyed by normalized alias text.
type FactAttributeCatalog struct {
	Attributes []FactAttributeCatalogEntry
	Aliases    map[string]int64
	ByName     map[string]int64
}

func NormalizeAttributeKey(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(text)), ""))
}

// LoadFactAttributeCatalog loads the global/industry attribute catalog created
// for fact import normalization. Missing tables are treated as an empty catalog
// so the caller can fail open instead of blocking fact drafts.
func (s *Store) LoadFactAttributeCatalog(ctx context.Context) (FactAttributeCatalog, error) {
	out := FactAttributeCatalog{Aliases: map[string]int64{}, ByName: map[string]int64{}}
	rows, err := s.db.QueryContext(ctx, `SELECT id,attribute_key,canonical_name,scope_type,COALESCE(industry,''),COALESCE(embedding_json,'')
		FROM live_fact_attributes WHERE status='active'`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item FactAttributeCatalogEntry
		var raw string
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &item.ScopeType, &item.Industry, &raw); err != nil {
			return out, err
		}
		if strings.TrimSpace(raw) != "" {
			_ = json.Unmarshal([]byte(raw), &item.Vector)
		}
		out.Attributes = append(out.Attributes, item)
		out.ByName[NormalizeAttributeKey(item.Name)] = item.ID
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	aliasRows, err := s.db.QueryContext(ctx, `SELECT alias,attribute_id FROM live_fact_attribute_aliases WHERE status='active'`)
	if err != nil {
		return out, err
	}
	defer aliasRows.Close()
	for aliasRows.Next() {
		var alias string
		var id int64
		if err := aliasRows.Scan(&alias, &id); err != nil {
			return out, err
		}
		if key := NormalizeAttributeKey(alias); key != "" {
			out.Aliases[key] = id
		}
	}
	return out, aliasRows.Err()
}
