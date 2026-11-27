package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

type answerSnapshotState struct {
	revision         int64
	reads            int
	rollbacks        int
	mutateDuringRead bool
}
type answerSnapshotDriver struct{ state *answerSnapshotState }
type answerSnapshotConn struct {
	state    *answerSnapshotState
	active   bool
	revision int64
}
type answerSnapshotRows struct {
	columns []string
	values  [][]driver.Value
}
type answerSnapshotTx struct{ conn *answerSnapshotConn }

var answerSnapshotDriverID atomic.Int64

func (d answerSnapshotDriver) Open(string) (driver.Conn, error) {
	return &answerSnapshotConn{state: d.state}, nil
}
func (*answerSnapshotConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("unexpected prepare/write")
}
func (*answerSnapshotConn) Close() error { return nil }
func (*answerSnapshotConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("must use read-only BeginTx")
}
func (c *answerSnapshotConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		return nil, fmt.Errorf("incorrect snapshot options")
	}
	c.active = true
	c.revision = c.state.revision
	return &answerSnapshotTx{c}, nil
}
func (*answerSnapshotTx) Commit() error { return fmt.Errorf("read snapshot must roll back") }
func (tx *answerSnapshotTx) Rollback() error {
	tx.conn.active = false
	tx.conn.state.rollbacks++
	return nil
}
func (c *answerSnapshotConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !c.active || len(args) != 2 {
		return nil, fmt.Errorf("query outside scoped read transaction")
	}
	if !strings.HasPrefix(query, "SELECT ") || strings.Contains(query, "FOR UPDATE") {
		return nil, fmt.Errorf("unexpected locking/write query")
	}
	c.state.reads++
	valid := args[0].Value == int64(14) && args[1].Value == int64(4)
	empty := func() *answerSnapshotRows { return &answerSnapshotRows{columns: []string{"empty"}} }
	if strings.Contains(query, "SELECT id FROM live_agent_plans") {
		if !valid {
			return empty(), nil
		}
		if c.state.mutateDuringRead {
			c.state.revision++
			c.state.mutateDuringRead = false
		}
		return &answerSnapshotRows{columns: []string{"id"}, values: [][]driver.Value{{int64(4)}}}, nil
	}
	if !valid {
		return nil, fmt.Errorf("tenant/plan arguments escaped scope")
	}
	if strings.Contains(query, "SELECT revision,items_json") {
		return &answerSnapshotRows{columns: []string{"revision", "items_json"}, values: [][]driver.Value{{c.revision, []byte(`[]`)}}}, nil
	}
	return empty(), nil
}
func (r *answerSnapshotRows) Columns() []string { return r.columns }
func (*answerSnapshotRows) Close() error        { return nil }
func (r *answerSnapshotRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}

func snapshotFakeStore(t *testing.T) (*Store, *answerSnapshotState) {
	t.Helper()
	state := &answerSnapshotState{revision: 1}
	name := fmt.Sprintf("answer-snapshot-%d", answerSnapshotDriverID.Add(1))
	sql.Register(name, answerSnapshotDriver{state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Store{db: db}, state
}
func TestAnswerSnapshotReadOnlyAtomicAndKnowledgeRevision(t *testing.T) {
	store, state := snapshotFakeStore(t)
	state.mutateDuringRead = true
	first, err := store.PlanAnswerSnapshot(context.Background(), 14, 4)
	if err != nil || first.Words.Revision != 1 || state.revision != 2 || state.rollbacks != 1 || state.reads != 7 {
		t.Fatal(first, err, *state)
	}
	second, err := store.PlanAnswerSnapshot(context.Background(), 14, 4)
	if err != nil || second.Words.Revision != 2 || first.Revision == second.Revision || state.rollbacks != 2 {
		t.Fatal(first, second, err, *state)
	}
	third, err := store.PlanAnswerSnapshot(context.Background(), 14, 4)
	if err != nil || third.Revision != second.Revision {
		t.Fatal("stable snapshot changed fingerprint", second, third, err)
	}
}
func TestAnswerSnapshotWrongTenantAndInvalidScopeDoNotLeak(t *testing.T) {
	store, state := snapshotFakeStore(t)
	if _, err := store.PlanAnswerSnapshot(context.Background(), 15, 4); err == nil || state.reads != 1 || state.rollbacks != 1 {
		t.Fatal(err, *state)
	}
	if _, err := store.PlanAnswerSnapshot(context.Background(), 0, 4); err == nil || state.reads != 1 {
		t.Fatal(err, *state)
	}
}
