package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type openingAssetTestConnector struct{ conn *openingAssetTestConn }

func (c openingAssetTestConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c openingAssetTestConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type openingAssetTestDriver struct{}

func (openingAssetTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type openingAssetTestConn struct{ records [][]driver.Value }

func (*openingAssetTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*openingAssetTestConn) Close() error { return nil }
func (*openingAssetTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c *openingAssetTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(query, "INSERT INTO live_opening_hook_assets") || len(args) != 5 {
		return nil, errors.New("not an append-only insert")
	}
	id := int64(len(c.records) + 1)
	c.records = append(c.records, []driver.Value{id, args[0].Value, args[1].Value, args[2].Value, args[3].Value, args[4].Value, time.Now().UTC()})
	return openingAssetTestResult(id), nil
}

type openingAssetTestResult int64

func (r openingAssetTestResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r openingAssetTestResult) RowsAffected() (int64, error) { return 1, nil }
func (c *openingAssetTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "tenant_id=? AND plan_id=?") {
		return nil, errors.New("missing scope")
	}
	rows := &openingAssetTestRows{}
	for i := len(c.records) - 1; i >= 0; i-- {
		r := c.records[i]
		if len(args) == 2 && r[1] == args[0].Value && r[2] == args[1].Value {
			rows.row = r
			break
		}
		if len(args) == 3 && r[0] == args[0].Value && r[1] == args[1].Value && r[2] == args[2].Value {
			rows.row = r
			break
		}
	}
	return rows, nil
}

type openingAssetTestRows struct{ row []driver.Value }

func (*openingAssetTestRows) Columns() []string {
	return []string{"id", "tenant_id", "plan_id", "library_md", "prompt_source", "created_by_user_id", "created_at"}
}
func (*openingAssetTestRows) Close() error { return nil }
func (r *openingAssetTestRows) Next(dest []driver.Value) error {
	if r.row == nil {
		return io.EOF
	}
	copy(dest, r.row)
	r.row = nil
	return nil
}

func TestOpeningHookPlanVersionsAreAppendOnlyAndTenantScoped(t *testing.T) {
	conn := &openingAssetTestConn{}
	database := sql.OpenDB(openingAssetTestConnector{conn})
	defer database.Close()
	s := &Store{db: database}
	ctx := context.Background()
	md := strings.Repeat("母版正文", 30)
	first, err := s.SaveLiveOpeningHook(ctx, 1, 2, 10, md, "mother-v1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.SaveLiveOpeningHook(ctx, 1, 2, 10, md+"新版", "manual-v2")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.SaveLiveOpeningHook(ctx, 2, 2, 20, md+"另一租户", "manual-other")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetLiveOpeningHook(ctx, 1, 2)
	if err != nil || got.ID != second.ID || first.ID == second.ID || conn.records[0][3] != md || len(conn.records) != 3 {
		t.Fatal(got, err)
	}
	got, err = s.GetLiveOpeningHook(ctx, 2, 2)
	if err != nil || got.ID != other.ID {
		t.Fatal(got, err)
	}
	if _, err = s.GetLiveOpeningHook(ctx, 1, 3); !errors.Is(err, ErrLiveOpeningHookNotFound) {
		t.Fatal("plan scope leaked", err)
	}
}
