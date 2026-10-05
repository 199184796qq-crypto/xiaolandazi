package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

// This connector is entirely in-memory: deletion failure tests must never
// connect to a real business database or depend on local MySQL credentials.
type roomCleanupFixture struct {
	executed          []string
	failOn            string
	roomExists        bool
	deletionRequested bool
	committed         bool
	rolledBack        bool
}
type roomCleanupConnector struct{ fixture *roomCleanupFixture }
type roomCleanupDriver struct{}

func (c roomCleanupConnector) Connect(context.Context) (driver.Conn, error) {
	return &roomCleanupConn{fixture: c.fixture}, nil
}
func (c roomCleanupConnector) Driver() driver.Driver { return roomCleanupDriver{} }
func (roomCleanupDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("test connector only")
}

type roomCleanupConn struct{ fixture *roomCleanupFixture }

func (c *roomCleanupConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *roomCleanupConn) Close() error              { return nil }
func (c *roomCleanupConn) Begin() (driver.Tx, error) { return c, nil }
func (c *roomCleanupConn) Commit() error             { c.fixture.committed = true; return nil }
func (c *roomCleanupConn) Rollback() error           { c.fixture.rolledBack = true; return nil }
func (c *roomCleanupConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	q = strings.Join(strings.Fields(q), " ")
	c.fixture.executed = append(c.fixture.executed, q)
	if c.fixture.failOn != "" && strings.Contains(q, c.fixture.failOn) {
		return nil, errors.New("injected database outage")
	}
	return driver.RowsAffected(1), nil
}
func (c *roomCleanupConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	q = strings.Join(strings.Fields(q), " ")
	if strings.Contains(q, "SELECT id FROM core_rooms") && strings.HasSuffix(q, "FOR UPDATE") {
		rows := &roomCleanupRows{columns: []string{"id"}}
		if c.fixture.roomExists {
			rows.data = [][]driver.Value{{int64(15)}}
		}
		return rows, nil
	}
	if strings.Contains(q, "SELECT COUNT(*) FROM mgmt_room_deletions") {
		count := int64(0)
		if c.fixture.deletionRequested {
			count = 1
		}
		return &roomCleanupRows{columns: []string{"count"}, data: [][]driver.Value{{count}}}, nil
	}
	if strings.Contains(q, "SELECT COUNT(*) FROM core_rooms") {
		count := int64(0)
		if c.fixture.roomExists {
			count = 1
		}
		return &roomCleanupRows{columns: []string{"count"}, data: [][]driver.Value{{count}}}, nil
	}
	if strings.Contains(q, "FROM live_quota_leases") {
		return &roomCleanupRows{columns: []string{"id", "runtime_session_id", "core_working_start_seconds", "allocated_seconds"}}, nil
	}
	return nil, errors.New("unexpected query: " + q)
}

type roomCleanupRows struct {
	columns []string
	data    [][]driver.Value
	next    int
}

func (r *roomCleanupRows) Columns() []string { return r.columns }
func (r *roomCleanupRows) Close() error      { return nil }
func (r *roomCleanupRows) Next(dest []driver.Value) error {
	if r.next >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.next])
	r.next++
	return nil
}
func deletionFixtureStore(t *testing.T, f *roomCleanupFixture) *Store {
	t.Helper()
	db := sql.OpenDB(roomCleanupConnector{fixture: f})
	t.Cleanup(func() { _ = db.Close() })
	return &Store{db: db}
}

func TestDeletedRoomCleanupPreservesHistoryAndReleasesOperationalState(t *testing.T) {
	f := &roomCleanupFixture{}
	s := deletionFixtureStore(t, f)
	if err := s.CleanupDeletedRoomDerivedState(context.Background(), 27, 15); err != nil {
		t.Fatal(err)
	}
	if !f.committed || f.rolledBack {
		t.Fatal("cleanup transaction did not commit")
	}
	all := strings.Join(f.executed, "\n")
	for _, forbidden := range []string{"DELETE FROM live_runtime_sessions", "DELETE FROM live_runtime_events", "DELETE FROM quota_ledger", "DELETE FROM live_support_requests", "DELETE FROM live_support_authorizations", "DELETE FROM live_support_config_versions", "DELETE FROM live_policy_versions", "DELETE FROM live_room_event_archive", "DELETE FROM live_agent_plans"} {
		if strings.Contains(all, forbidden) {
			t.Fatalf("historical records must survive: %s", forbidden)
		}
	}
	for _, required := range []string{"stop_reason='room_deleted'", "current_room_id=NULL", "UPDATE live_device_room_bindings SET status='released'", "UPDATE live_agent_plan_room_bindings SET status='released'", "UPDATE live_support_authorizations SET status='revoked'", "DELETE FROM semantic_documents", "DELETE FROM live_content_policies", "DELETE FROM live_room_content_access"} {
		if !strings.Contains(all, required) {
			t.Fatalf("active resource was not released: %s", required)
		}
	}
	if strings.Contains(all, "UPDATE mgmt_room_deletions") {
		t.Fatal("local cleanup cannot complete a job before all Core replicas acknowledge")
	}
}

func TestDeletedRoomCleanupFailureRollsBackAndRemainsRetryable(t *testing.T) {
	f := &roomCleanupFixture{failOn: "DELETE FROM semantic_documents"}
	s := deletionFixtureStore(t, f)
	if err := s.CleanupDeletedRoomDerivedState(context.Background(), 27, 15); err == nil {
		t.Fatal("database failure was swallowed")
	}
	if f.committed || !f.rolledBack {
		t.Fatal("partial cleanup must roll back")
	}
	f.failOn = ""
	f.rolledBack = false
	if err := s.CleanupDeletedRoomDerivedState(context.Background(), 27, 15); err != nil {
		t.Fatal(err)
	}
	if !f.committed {
		t.Fatal("cleanup did not succeed on retry")
	}
}

func TestRoomCleanupCannotDeleteBindingsBeforeCoreRoomDeletion(t *testing.T) {
	f := &roomCleanupFixture{roomExists: true}
	s := deletionFixtureStore(t, f)
	if err := s.CleanupDeletedRoomDerivedState(context.Background(), 27, 15); err == nil {
		t.Fatal("cleanup accepted a still-existing room")
	}
	if len(f.executed) != 0 || !f.rolledBack {
		t.Fatal("live room resources were modified")
	}
}

func TestContentConfigurationCannotRecreateDeletedRoomState(t *testing.T) {
	for _, tc := range []struct {
		name              string
		exists, requested bool
	}{
		{name: "already deleted"},
		{name: "queued while Core unavailable", exists: true, requested: true},
	} {
		for _, operation := range []string{"policy", "mode"} {
			t.Run(tc.name+"/"+operation, func(t *testing.T) {
				f := &roomCleanupFixture{roomExists: tc.exists, deletionRequested: tc.requested}
				s := deletionFixtureStore(t, f)
				var err error
				if operation == "policy" {
					err = s.SaveLiveContentPolicy(context.Background(), 27, 15, 6, 0, model.DefaultLiveContentPolicy(), false, nil, model.LiveContentAIPregenerated)
				} else {
					err = s.SaveLiveContentMode(context.Background(), 27, 15, 6, model.LiveContentAIPregenerated, false)
				}
				if err == nil {
					t.Fatal("in-flight save recreated deleted room configuration")
				}
				if len(f.executed) != 0 || f.committed || !f.rolledBack {
					t.Fatal("save modified room state after deletion intent")
				}
			})
		}
	}
}
