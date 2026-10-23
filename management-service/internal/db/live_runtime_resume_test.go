package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

type resumeFenceConnector struct{ conn *resumeFenceConn }

func (c resumeFenceConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (resumeFenceConnector) Driver() driver.Driver                          { return openingAssetTestDriver{} }

type resumeFenceConn struct {
	pausedID                       int64
	version                        int64
	queries, writes                int
	transaction, rolled, committed bool
	quotaReached                   error
}

func (*resumeFenceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*resumeFenceConn) Close() error { return nil }
func (c *resumeFenceConn) Begin() (driver.Tx, error) {
	c.transaction = true
	return resumeFenceTx{c}, nil
}
func (c *resumeFenceConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

type resumeFenceTx struct{ conn *resumeFenceConn }

func (tx resumeFenceTx) Commit() error {
	tx.conn.committed = true
	tx.conn.transaction = false
	return nil
}
func (tx resumeFenceTx) Rollback() error {
	tx.conn.rolled = true
	tx.conn.transaction = false
	return nil
}
func (c *resumeFenceConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries++
	if c.queries != 1 {
		return nil, c.quotaReached
	}
	if !c.transaction || !strings.Contains(q, "FROM live_runtime_sessions s") || !strings.Contains(q, "s.status='paused'") || !strings.Contains(q, "FOR UPDATE") || len(args) != 2 || args[0].Value != int64(14) || args[1].Value != int64(29) {
		return nil, errors.New("paused identity must be selected under a scoped row lock")
	}
	now := time.Now().UTC()
	return &contentPolicyTestRows{columns: []string{"id", "external_id", "tenant_id", "room_id", "device_id", "sn", "realm", "status", "reason", "started_by", "stopped_by", "started_at", "last_billed_at", "ended_at", "billed", "version"}, values: [][]driver.Value{{c.pausedID, "same-external", int64(14), int64(29), nil, "", "prod", "paused", "manual_pause", int64(28), int64(28), now, now, nil, int64(100), c.version}}}, nil
}
func (c *resumeFenceConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.writes++
	return nil, c.quotaReached
}

func TestResumeIfCurrentRejectsReplacementBeforeAnyQuotaPoolOrEventOperation(t *testing.T) {
	for _, sample := range []struct {
		name    string
		id      int64
		version uint64
	}{
		{"replacement-session", 386, 7},
		{"later-pause-of-same-session", 387, 6},
	} {
		t.Run(sample.name, func(t *testing.T) {
			conn := &resumeFenceConn{pausedID: 387, version: 7, quotaReached: errors.New("quota or pool must not be reached")}
			database := sql.OpenDB(resumeFenceConnector{conn})
			defer database.Close()
			store := &Store{db: database}
			_, err := store.ResumeLiveRuntimeSessionIfCurrent(context.Background(), 14, 29, 28, time.Now().UTC(), sample.id, sample.version)
			if !errors.Is(err, ErrLiveRuntimeResumeConflict) || conn.queries != 1 || conn.writes != 0 || !conn.rolled || conn.committed {
				t.Fatal(err, conn)
			}
		})
	}
}

func TestResumeIfCurrentMatchingRowMayReachExistingQuotaTransaction(t *testing.T) {
	quotaReached := errors.New("injected quota failure after identity lock")
	conn := &resumeFenceConn{pausedID: 387, version: 7, quotaReached: quotaReached}
	database := sql.OpenDB(resumeFenceConnector{conn})
	defer database.Close()
	_, err := (&Store{db: database}).ResumeLiveRuntimeSessionIfCurrent(context.Background(), 14, 29, 28, time.Now().UTC(), 387, 7)
	if !errors.Is(err, quotaReached) || !conn.rolled || conn.committed || conn.queries+conn.writes < 2 {
		t.Fatal(err, conn)
	}
}
