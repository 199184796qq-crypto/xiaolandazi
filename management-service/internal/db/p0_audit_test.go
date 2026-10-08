package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

type p0TestConnector struct{ conn *p0TestConn }

func (c p0TestConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c p0TestConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type p0TestConn struct {
	openingAssetTestConn
	queries int
	events  [][]driver.Value
}

func (c *p0TestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT IGNORE INTO live_p0_policy_versions") && len(args) == 4 {
		return openingAssetTestResult(1), nil
	}
	if !strings.HasPrefix(q, "INSERT INTO live_p0_audit_events") || len(args) != 14 {
		return nil, errors.New("not append-only scoped audit")
	}
	row := []driver.Value{int64(len(c.events) + 1)}
	for _, a := range args {
		row = append(row, a.Value)
	}
	row = append(row, time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC))
	c.events = append(c.events, row)
	return openingAssetTestResult(len(c.events)), nil
}
func (c *p0TestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.queries++
	if !strings.Contains(q, "WHERE tenant_id=? AND plan_id=?") || len(args) != 5 || args[2].Value != args[3].Value {
		return nil, errors.New("missing tenant/plan/run bounds")
	}
	rows := &p0TestRows{}
	for _, row := range c.events {
		if row[1] == args[0].Value && row[2] == args[1].Value && (args[2].Value == "" || row[4] == args[2].Value) {
			rows.rows = append(rows.rows, row)
		}
	}
	return rows, nil
}

type p0TestRows struct{ rows [][]driver.Value }

func (*p0TestRows) Columns() []string {
	return []string{"id", "tenant", "plan", "room", "run", "candidate", "sha", "phase", "task", "rewrite", "decision", "text", "raw", "prompt", "report", "created"}
}
func (*p0TestRows) Close() error { return nil }
func (r *p0TestRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func TestP0AuditAppendOnlyAndScope(t *testing.T) {
	conn := &p0TestConn{}
	database := sql.OpenDB(p0TestConnector{conn})
	defer database.Close()
	s := &Store{db: database}
	ctx := context.Background()
	sha := strings.Repeat("a", 64)
	if err := s.SaveP0PolicyVersion(ctx, sha, "v1", "fixture", "mother snapshot"); err != nil {
		t.Fatal(err)
	}
	e := model.P0AuditEvent{TenantID: 1, PlanID: 2, RoomID: 3, RunID: "run-one", CandidateID: "c1", RuleSHA256: sha, Phase: "body", TaskKey: "point×angle", Decision: "rewrite_required", Text: "assembled", RawResponse: "raw model", Prompt: "actual request", Report: json.RawMessage(`{"passed":false}`)}
	if err := s.SaveP0AuditEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.TenantID = 9
	e.CandidateID = "c2"
	if err := s.SaveP0AuditEvent(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListP0AuditEvents(ctx, 1, 2, "run-one", 50)
	if err != nil || len(got) != 1 || got[0].RawResponse != "raw model" || got[0].Prompt != "actual request" {
		t.Fatal(got, err)
	}
	got, err = s.ListP0AuditEvents(ctx, 1, 20, "", 50)
	if err != nil || len(got) != 0 {
		t.Fatal("plan leak", got, err)
	}
	before := conn.queries
	if _, err = s.ListP0AuditEvents(ctx, 0, 2, "", 50); err == nil || conn.queries != before {
		t.Fatal("invalid scope hit database")
	}
	if _, err = s.ListP0AuditEvents(ctx, 1, 2, "", 201); err == nil {
		t.Fatal("unbounded query")
	}
}
