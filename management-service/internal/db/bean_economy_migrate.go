package db

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed bean_economy_schema.sql
var beanEconomySchema string

func (s *Store) MigrateBeanEconomy(ctx context.Context) error {
	for _, raw := range strings.Split(strings.ReplaceAll(beanEconomySchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply bean economy schema: %w", err)
		}
	}
	return nil
}
