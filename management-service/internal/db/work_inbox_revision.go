package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// A bounded, coalescing transactional outbox. Only six revision rows are kept.
// Original business ledgers remain the audit trail; no second task ledger is written.
const inboxRevisionSchema = `CREATE TABLE IF NOT EXISTS work_inbox_revisions (
 topic VARCHAR(32) NOT NULL PRIMARY KEY,
 revision BIGINT UNSIGNED NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`

func (s *Store) MigrateWorkInbox(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, inboxRevisionSchema); err != nil {
		return err
	}
	for _, topic := range []string{"access", "finance", "inventory", "logistics", "sales", "support"} {
		if _, err := s.db.ExecContext(ctx, "INSERT IGNORE INTO work_inbox_revisions(topic,revision) VALUES(?,0)", topic); err != nil {
			return err
		}
	}
	// Set only during startup, before handlers/background business writers start.
	s.inboxEventsEnabled = true
	return nil
}
func bumpInboxTx(ctx context.Context, tx *sql.Tx, topics ...string) error {
	sort.Strings(topics)
	previous := ""
	for _, topic := range topics {
		switch topic {
		case "access", "finance", "inventory", "logistics", "sales", "support":
		default:
			return fmt.Errorf("invalid inbox topic")
		}
		if topic == previous {
			continue
		}
		previous = topic
		result, err := tx.ExecContext(ctx, "UPDATE work_inbox_revisions SET revision=revision+1 WHERE topic=?", topic)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return fmt.Errorf("missing inbox revision %s", topic)
		}
	}
	return nil
}
func (s *Store) commitInboxTx(ctx context.Context, tx *sql.Tx, topics ...string) error {
	// Isolated legacy tests and pre-start migrations do not enable inbox publication.
	if s.inboxEventsEnabled {
		if err := bumpInboxTx(ctx, tx, topics...); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) TouchInbox(ctx context.Context, topics ...string) error {
	if !s.inboxEventsEnabled {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = bumpInboxTx(ctx, tx, topics...); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) InboxRevisions(ctx context.Context) (map[string]uint64, error) {
	out := map[string]uint64{}
	rows, err := s.db.QueryContext(ctx, "SELECT topic,revision FROM work_inbox_revisions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var topic string
		var revision uint64
		if err = rows.Scan(&topic, &revision); err != nil {
			return nil, err
		}
		out[topic] = revision
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != 6 {
		return nil, fmt.Errorf("inbox revisions incomplete")
	}
	return out, nil
}
