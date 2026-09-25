package db

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed live_runtime_schema.sql
var liveRuntimeSchema string

func (s *Store) MigrateLiveRuntime(ctx context.Context) error {
	schema := strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n")
	for _, raw := range strings.Split(schema, "\n-- +statement\n") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply live runtime schema: %w", err)
		}
	}
	columns := []struct {
		name string
		sql  string
	}{
		{"evidence_type", "ALTER TABLE live_policy_learning_candidates ADD COLUMN evidence_type VARCHAR(32) NOT NULL DEFAULT 'manual_feedback' AFTER id"},
		{"source_ref", "ALTER TABLE live_policy_learning_candidates ADD COLUMN source_ref VARCHAR(512) NOT NULL DEFAULT '' AFTER evidence_type"},
		{"observed_reply", "ALTER TABLE live_policy_learning_candidates ADD COLUMN observed_reply MEDIUMTEXT NULL AFTER question"},
		{"model_provider", "ALTER TABLE live_policy_learning_candidates ADD COLUMN model_provider VARCHAR(64) NOT NULL DEFAULT '' AFTER status"},
		{"learning_meta_json", "ALTER TABLE live_policy_learning_candidates ADD COLUMN learning_meta_json MEDIUMTEXT NULL AFTER latency_ms"},
	}
	for _, column := range columns {
		exists, err := s.columnExists(ctx, "live_policy_learning_candidates", column.name)
		if err != nil {
			return fmt.Errorf("check live learning column %s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("add live learning column %s: %w", column.name, err)
		}
	}
	return nil
}
