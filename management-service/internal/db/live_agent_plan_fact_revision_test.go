package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type factRevisionConnector struct{ conn *factRevisionConn }

func (c factRevisionConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c factRevisionConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type factRevisionConn struct {
	row                             []driver.Value
	writes                          []string
	args                            [][]driver.NamedValue
	committed, rolled, failRevision bool
}

func (*factRevisionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*factRevisionConn) Close() error                { return nil }
func (c *factRevisionConn) Begin() (driver.Tx, error) { return factRevisionTx{c}, nil }

type factRevisionTx struct{ conn *factRevisionConn }

func (t factRevisionTx) Commit() error   { t.conn.committed = true; return nil }
func (t factRevisionTx) Rollback() error { t.conn.rolled = true; return nil }
func (c *factRevisionConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(query, "WHERE id=? AND tenant_id=? AND plan_id=?") || len(args) != 3 {
		return nil, errors.New("unscoped fact query")
	}
	rows := &contentPolicyTestRows{columns: strings.Fields("id tenant plan category key value forbidden rewrite quote bucket reason source ref status version creator updater created updated scope")}
	if args[0].Value == int64(7) && args[1].Value == int64(14) && args[2].Value == int64(4) {
		row := append([]driver.Value(nil), c.row...)
		if c.committed {
			row[5] = "修改后的内容"
			row[14] = int64(2)
		}
		rows.values = [][]driver.Value{row}
	}
	return rows, nil
}
func (c *factRevisionConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	// Match real SQL driver's parameter validation, not a permissive SQL mock.
	if want := strings.Count(query, "?"); want != len(args) {
		return nil, fmt.Errorf("SQL placeholders=%d but arguments=%d", want, len(args))
	}
	if strings.Contains(query, "INSERT INTO live_agent_plan_fact_revisions") {
		columns := strings.Split(strings.Split(strings.Split(query, "(")[1], ")")[0], ",")
		values := strings.Split(strings.Split(strings.Split(query, "VALUES (")[1], ")")[0], ",")
		if len(columns) != len(values) {
			return nil, errors.New("revision column/value count mismatch")
		}
		if args[0].Value != int64(14) || args[1].Value != int64(4) || args[2].Value != int64(7) || args[3].Value != int64(2) {
			return nil, errors.New("unscoped history or wrong next version")
		}
		if c.failRevision {
			return nil, errors.New("revision unavailable")
		}
	} else if !strings.Contains(query, "WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'") {
		return nil, errors.New("unscoped fact update")
	}
	c.writes = append(c.writes, query)
	c.args = append(c.args, args)
	return driver.RowsAffected(1), nil
}
func factRevisionStore(t *testing.T) (*Store, *factRevisionConn) {
	t.Helper()
	c := &factRevisionConn{row: []driver.Value{int64(7), int64(14), int64(4), "identity_location", "来源说明", "原始内容", "不要这样说", "改成这样说", "原素材", "adoptable", "原审核理由", "manual_create", "manual:fact-editor", "active", int64(1), int64(8), int64(8), time.Now(), time.Now(), "null"}}
	database := sql.OpenDB(factRevisionConnector{c})
	t.Cleanup(func() { _ = database.Close() })
	return &Store{db: database}, c
}
func TestFactRevisionEditCommitsNewVersion(t *testing.T) {
	s, c := factRevisionStore(t)
	updated, err := s.UpdateLiveAgentPlanFactWithExpectedVersion(context.Background(), 14, 4, 7, 8, 1, "identity_location", "来源说明", "修改后的内容", "不允许说的原句", "安全建议")
	if err != nil {
		t.Fatal(err)
	}
	if !c.committed || len(c.writes) != 2 || updated.VersionNo != 2 || updated.Value != "修改后的内容" {
		t.Fatal("edit not committed", updated, c)
	}
	args := c.args[1]
	if args[7].Value != "不允许说的原句" || args[8].Value != "安全建议" || args[9].Value != "原素材" || args[13].Value != int64(8) {
		t.Fatal("history metadata lost", args)
	}
}
func TestFactRevisionDeleteKeepsHistory(t *testing.T) {
	s, c := factRevisionStore(t)
	if err := s.DeleteLiveAgentPlanFactWithExpectedVersion(context.Background(), 14, 4, 7, 8, 1); err != nil {
		t.Fatal(err)
	}
	if !c.committed || len(c.writes) != 2 || !strings.Contains(c.writes[0], "DELETE FROM live_agent_plan_facts") || !strings.Contains(c.writes[1], "'delete'") {
		t.Fatal("delete lost recovery history")
	}
}
func TestFactRevisionLinksNormalizationArchive(t *testing.T) {
	s, c := factRevisionStore(t)
	_, err := s.UpdateLiveAgentPlanFactWithExpectedVersion(context.Background(), 14, 4, 7, 8, 1, "identity_location", "来源说明", "修改后的内容", "", "", "manual:fact-normalization:owned-archive")
	if err != nil {
		t.Fatal(err)
	}
	if c.args[1][12].Value != "manual:fact-normalization:owned-archive" {
		t.Fatal("revision lost original-input archive association")
	}
}
func TestFactRevisionEditFailureRollsBack(t *testing.T) {
	s, c := factRevisionStore(t)
	c.failRevision = true
	_, err := s.UpdateLiveAgentPlanFactWithExpectedVersion(context.Background(), 14, 4, 7, 8, 1, "identity_location", "来源说明", "修改后的内容", "", "")
	if err == nil || c.committed || !c.rolled {
		t.Fatal("partial edit committed")
	}
}
func TestFactRevisionConflictAndTenantScopePreventWrites(t *testing.T) {
	for _, input := range []struct {
		tenant, expected int64
		want             error
	}{{14, 2, ErrLiveAgentPlanFactVersionConflict}, {99, 1, ErrLiveAgentPlanFactNotFound}} {
		s, c := factRevisionStore(t)
		_, err := s.UpdateLiveAgentPlanFactWithExpectedVersion(context.Background(), input.tenant, 4, 7, 8, input.expected, "identity_location", "来源说明", "修改后的内容", "", "")
		if !errors.Is(err, input.want) || c.committed || len(c.writes) != 0 {
			t.Fatal("stale/cross-tenant edit allowed", err)
		}
	}
}
