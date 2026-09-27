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
	reservedExists, err := s.columnExists(ctx, "quota_buckets", "reserved_seconds")
	if err != nil {
		return fmt.Errorf("check quota reserved column: %w", err)
	}
	if !reservedExists {
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE quota_buckets ADD COLUMN reserved_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER remaining_seconds"); err != nil {
			return fmt.Errorf("add quota reserved column: %w", err)
		}
	}

	exists, err := s.columnExists(ctx, "agent_learning_results", "matched_memory_item_id")
	if err != nil {
		return fmt.Errorf("check agent learning matched memory column: %w", err)
	}
	if !exists {
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE agent_learning_results ADD COLUMN matched_memory_item_id BIGINT UNSIGNED NULL AFTER memory_key"); err != nil {
			return fmt.Errorf("add agent learning matched memory column: %w", err)
		}
	}
	return nil
}
