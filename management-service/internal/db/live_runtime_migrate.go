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

	for _, column := range []struct {
		name string
		sql  string
	}{
		{"input_tokens", "ALTER TABLE ai_single_use_events ADD COLUMN input_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER latency_ms"},
		{"output_tokens", "ALTER TABLE ai_single_use_events ADD COLUMN output_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER input_tokens"},
		{"total_tokens", "ALTER TABLE ai_single_use_events ADD COLUMN total_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER output_tokens"},
	} {
		exists, err := s.columnExists(ctx, "ai_single_use_events", column.name)
		if err != nil {
			return fmt.Errorf("check ai usage column %s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("add ai usage column %s: %w", column.name, err)
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

	indexColumns, err := s.indexColumns(ctx, "live_agent_plan_room_bindings", "uk_live_agent_plan_active_room")
	if err != nil {
		return fmt.Errorf("check live agent room binding unique key: %w", err)
	}
	if strings.Join(indexColumns, ",") != "tenant_id,plan_id,active_room_id" {
		if len(indexColumns) > 0 {
			if _, err := s.db.ExecContext(ctx, "ALTER TABLE live_agent_plan_room_bindings DROP INDEX uk_live_agent_plan_active_room"); err != nil {
				return fmt.Errorf("drop legacy live agent room unique key: %w", err)
			}
		}
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE live_agent_plan_room_bindings ADD UNIQUE KEY uk_live_agent_plan_active_room (tenant_id, plan_id, active_room_id)"); err != nil {
			return fmt.Errorf("add multi-plan room unique key: %w", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO live_agent_room_plan_selections (
			tenant_id, room_id, plan_id, selected_by_user_id, selected_at
		)
		SELECT b.tenant_id, b.room_id, b.plan_id, b.bound_by_user_id, b.bound_at
		FROM live_agent_plan_room_bindings b
		INNER JOIN live_agent_plans p ON p.id=b.plan_id AND p.tenant_id=b.tenant_id
		WHERE b.status='active' AND p.status='active'
		ORDER BY b.id DESC
	`); err != nil {
		return fmt.Errorf("backfill live agent room plan selections: %w", err)
	}
	return nil
}

func (s *Store) indexColumns(ctx context.Context, tableName, indexName string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COLUMN_NAME
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?
		ORDER BY SEQ_IN_INDEX
	`, tableName, indexName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, err
		}
		columns = append(columns, column)
	}
	return columns, rows.Err()
}
