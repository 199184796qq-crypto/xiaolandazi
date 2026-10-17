package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

type categoryTestConnector struct{ c *categoryTestConn }

func (c categoryTestConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (c categoryTestConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type categoryTestConn struct {
	category                        model.LiveFactCategory
	failRevision, committed, rolled bool
	writes                          []string
	factArgs                        []driver.Value
}

func (*categoryTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*categoryTestConn) Close() error                { return nil }
func (c *categoryTestConn) Begin() (driver.Tx, error) { return categoryTestTx{c}, nil }

type categoryTestTx struct{ c *categoryTestConn }

func (t categoryTestTx) Commit() error   { t.c.committed = true; return nil }
func (t categoryTestTx) Rollback() error { t.c.rolled = true; return nil }
func (c *categoryTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "SELECT label,description,family") {
		if len(args) != 2 || args[0].Value != int64(101) || args[1].Value != c.category.Value {
			return nil, errors.New("unscoped category lookup")
		}
		return &contentPolicyTestRows{columns: []string{"label", "description", "family"}, values: [][]driver.Value{{c.category.Label, c.category.Description, c.category.Family}}}, nil
	}
	if strings.Contains(q, "FROM live_fact_categories") {
		if len(args) != 1 {
			return nil, errors.New("unscoped catalog")
		}
		rows := &contentPolicyTestRows{columns: []string{"value", "label", "description", "family"}}
		if args[0].Value == int64(101) {
			rows.values = [][]driver.Value{{c.category.Value, c.category.Label, c.category.Description, c.category.Family}}
		}
		return rows, nil
	}
	if strings.Contains(q, "SELECT 1 FROM live_agent_plans") {
		return &contentPolicyTestRows{columns: []string{"exists"}, values: [][]driver.Value{{int64(1)}}}, nil
	}
	if strings.Contains(q, "FROM live_agent_plan_facts") {
		rows := &contentPolicyTestRows{columns: strings.Fields("id tenant plan category key value forbidden rewrite quote bucket reason source ref status version creator updater created updated scope")}
		if c.committed {
			a := c.factArgs
			rows.values = [][]driver.Value{{int64(7), a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7], a[8], a[9], a[10], a[11], "active", int64(1), a[12], a[13], time.Now(), time.Now(), "null"}}
		}
		return rows, nil
	}
	return nil, fmt.Errorf("unexpected query %s", q)
}

type categoryInsertResult struct{}

func (categoryInsertResult) LastInsertId() (int64, error) { return 7, nil }
func (categoryInsertResult) RowsAffected() (int64, error) { return 1, nil }
func (c *categoryTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Count(q, "?") != len(args) {
		return nil, errors.New("SQL arguments mismatch")
	}
	if len(args) == 0 || args[0].Value != int64(101) {
		return nil, errors.New("unscoped category/fact write")
	}
	if strings.Contains(q, "fact_revisions") && c.failRevision {
		return nil, errors.New("revision failed")
	}
	if strings.Contains(q, "INSERT INTO live_agent_plan_facts") {
		c.factArgs = nil
		for _, arg := range args {
			c.factArgs = append(c.factArgs, arg.Value)
		}
	}
	c.writes = append(c.writes, q)
	return categoryInsertResult{}, nil
}

func TestCustomCategoryAndFactSaveAtomicallyWithSeparateWording(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			category, err := model.NewLiveFactCategory("算账口径", "product", "商品价格规则")
			if err != nil {
				t.Fatal(err)
			}
			conn := &categoryTestConn{category: category, failRevision: fail}
			database := sql.OpenDB(categoryTestConnector{conn})
			defer database.Close()
			store := &Store{db: database}
			candidate := model.LiveAgentPlanFactCandidate{Category: category.Value, Key: "价格算法", Value: "按实际价格计算", ForbiddenWording: "不能虚构差价", SafeRewrite: "按现价算账", ReviewBucket: "adoptable", SourceQuote: "用户原话"}
			result, err := store.AdoptLiveAgentPlanFact(context.Background(), 101, 202, 303, candidate, "system-agent:intent-fact", category)
			if fail {
				if err == nil || conn.committed || !conn.rolled {
					t.Fatal("failed revision left category/fact committed", err, conn)
				}
				return
			}
			if err != nil || !conn.committed || len(conn.writes) != 3 || result.Saved == nil || result.Saved.ForbiddenWording != candidate.ForbiddenWording || result.Saved.SafeRewrite != candidate.SafeRewrite {
				t.Fatal(result, err, conn)
			}
			own, err := store.ListLiveFactCategories(context.Background(), 101)
			if err != nil || len(own) != 1 {
				t.Fatal(own, err)
			}
			other, err := store.ListLiveFactCategories(context.Background(), 404)
			if err != nil || len(other) != 0 {
				t.Fatal("category leaked across tenants", other, err)
			}
		})
	}
}
