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
	return nil
}
