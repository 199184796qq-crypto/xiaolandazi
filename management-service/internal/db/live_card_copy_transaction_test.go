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

type cardCopyConnector struct{ conn *cardCopyConn }

func (c cardCopyConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c cardCopyConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type cardCopyConn struct {
	benefit           bool
	missingNext       bool
	committed, rolled bool
	failAt            int
	writes            []string
	inserts           map[int64][]driver.NamedValue
	nextID            int64
}

func (*cardCopyConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*cardCopyConn) Close() error { return nil }
func (c *cardCopyConn) Begin() (driver.Tx, error) {
	c.committed = false
	c.rolled = false
	return cardCopyTx{c}, nil
}

type cardCopyTx struct{ conn *cardCopyConn }

func (t cardCopyTx) Commit() error   { t.conn.committed = true; return nil }
func (t cardCopyTx) Rollback() error { t.conn.rolled = true; return nil }

type cardCopyResult struct{ id int64 }

func (r cardCopyResult) LastInsertId() (int64, error) { return r.id, nil }
func (cardCopyResult) RowsAffected() (int64, error)   { return 1, nil }

func (c *cardCopyConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.HasPrefix(q, "SELECT id FROM live_agent_plans") {
		if !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("plan allocation not serialized")
		}
		rows := &contentPolicyTestRows{columns: []string{"id"}}
		if len(args) == 2 && args[0].Value == int64(4) && args[1].Value == int64(14) {
			rows.values = [][]driver.Value{{int64(4)}}
		}
		return rows, nil
	}
	if strings.HasPrefix(q, "SELECT link_key, product_name") || strings.HasPrefix(q, "SELECT benefit_key, product_name") {
		if len(args) != 2 || args[0].Value != int64(14) || args[1].Value != int64(4) || !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("unscoped database allocation")
		}
		rows := &contentPolicyTestRows{columns: []string{"key", "name"}, values: [][]driver.Value{{"1号链接", "杨鸭子"}, {"9号链接", "停用商品"}}}
		if c.benefit {
			rows.values = [][]driver.Value{{"活动A", "杨鸭子"}}
		}
		for _, insert := range c.inserts {
			row := []driver.Value{insert[0].Value, insert[1].Value}
			if c.benefit {
				row[1] = insert[2].Value
			}
			rows.values = append(rows.values, row)
		}
		return rows, nil
	}
	if strings.HasPrefix(q, "SELECT link_key FROM") {
		if len(args) != 2 || args[0].Value != int64(14) || args[1].Value != int64(4) || !strings.Contains(q, "status='active'") {
			return nil, errors.New("benefit associated unscoped/inactive product")
		}
		rows := &contentPolicyTestRows{columns: []string{"key"}, values: [][]driver.Value{{"1号链接"}}}
		if !c.missingNext {
			rows.values = append(rows.values, []driver.Value{"3号链接"})
		}
		return rows, nil
	}
	if len(args) != 3 || args[1].Value != int64(14) || args[2].Value != int64(4) {
		return nil, errors.New("unscoped card read")
	}
	id := args[0].Value.(int64)
	if strings.Contains(q, "FROM live_agent_plan_product_attributes") {
		return &contentPolicyTestRows{columns: strings.Fields("id tenant plan product code label value unit display priority quote type ref status version creator updater created updated"), values: [][]driver.Value{{int64(101), int64(14), int64(4), id, "flavour", "口味", "麻辣", "", "text", int64(10), "原属性证据", "manual_copy", "copy:product:7:v3", "active", int64(1), int64(8), int64(8), time.Now(), time.Now()}}}, nil
	}
	columns := strings.Fields("id tenant plan key name roles spec price quantity audience quote bucket reason type ref status version creator updater created updated")
	row := []driver.Value{id, int64(14), int64(4), "1号链接", "杨鸭子", `["main"]`, "900g", "87.6", "1", "喜欢麻辣和五香", "原始证据", "discuss", "原审核信息", "manual", "original", "active", int64(3), int64(8), int64(8), time.Now(), time.Now()}
	if c.benefit {
		columns = strings.Fields("id tenant plan key link name price gift activity mode starts ends quote bucket reason type ref status version creator updater created updated")
		row = []driver.Value{id, int64(14), int64(4), "活动A", "1号链接", "杨鸭子", "79.9", "赠试吃装", "原福利", "permanent", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), "原始证据", "discuss", "原审核信息", "manual", "original", "active", int64(3), int64(8), int64(8), time.Now(), time.Now()}
	}
	rows := &contentPolicyTestRows{columns: columns}
	if id == 7 {
		rows.values = [][]driver.Value{row}
	} else if insert, ok := c.inserts[id]; ok {
		if c.benefit {
			row[3], row[4], row[5], row[16], row[18] = insert[0].Value, insert[1].Value, insert[2].Value, insert[3].Value, int64(1)
		} else {
			row[3], row[4], row[14], row[16] = insert[0].Value, insert[1].Value, insert[2].Value, int64(1)
		}
		rows.values = [][]driver.Value{row}
	}
	return rows, nil
}
func (c *cardCopyConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(q, "INSERT INTO") || !strings.Contains(q, "tenant_id=? AND plan_id=?") {
		return nil, errors.New("copy mutated original or lost scope")
	}
	if strings.Count(q, "?") != len(args) {
		return nil, errors.New("SQL placeholder mismatch")
	}
	columns := strings.Split(strings.Split(strings.Split(q, "(")[1], ")")[0], ",")
	selection := strings.Split(strings.Split(strings.Split(q, "SELECT ")[1], "FROM ")[0], ",")
	if len(columns) != len(selection) {
		return nil, fmt.Errorf("copy INSERT/SELECT width mismatch %d vs %d", len(columns), len(selection))
	}
	if args[len(args)-2].Value != int64(14) || args[len(args)-1].Value != int64(4) {
		return nil, errors.New("wrong destination scope")
	}
	if c.failAt > 0 && len(c.writes)+1 == c.failAt {
		return nil, errors.New("injected copy failure")
	}
	c.writes = append(c.writes, q)
	if strings.Contains(q, "INSERT INTO live_agent_plan_product_links (") || strings.Contains(q, "INSERT INTO live_agent_plan_benefits (") {
		c.nextID++
		c.inserts[c.nextID] = append([]driver.NamedValue(nil), args...)
		return cardCopyResult{c.nextID}, nil
	}
	return cardCopyResult{0}, nil
}
func cardCopyStore(t *testing.T, benefit bool) (*Store, *cardCopyConn) {
	t.Helper()
	c := &cardCopyConn{benefit: benefit, inserts: map[int64][]driver.NamedValue{}, nextID: 70}
	database := sql.OpenDB(cardCopyConnector{c})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}, c
}
func TestProductCardCopyCommitsIndependentRowsAndNextDatabaseNumber(t *testing.T) {
	s, c := cardCopyStore(t, false)
	first, err := s.CopyLiveAgentPlanProductLink(context.Background(), 14, 4, 7, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !c.committed || c.rolled || len(c.writes) != 4 || first.ID == 7 || first.LinkKey != "10号链接" || first.ProductName != "杨鸭子01" || first.VersionNo != 1 || first.Spec != "900g" || len(first.Attributes) != 1 || first.Attributes[0].ProductLinkID != first.ID {
		t.Fatal("copy lost original fields, attributes or history", first, c)
	}
	second, err := s.CopyLiveAgentPlanProductLink(context.Background(), 14, 4, 7, 8, 3)
	if err != nil || second.LinkKey != "11号链接" || second.ProductName != "杨鸭子02" || second.ID == first.ID {
		t.Fatal("second copy reused name/link/id", second, err)
	}
}
func TestProductCardCopyConflictTenantAndFailureCannotCommitPartialCard(t *testing.T) {
	for _, tc := range []struct {
		tenant, expected int64
		fail             int
	}{{99, 3, 0}, {14, 2, 0}, {14, 3, 2}, {14, 3, 3}, {14, 3, 4}} {
		s, c := cardCopyStore(t, false)
		c.failAt = tc.fail
		_, err := s.CopyLiveAgentPlanProductLink(context.Background(), tc.tenant, 4, 7, 8, tc.expected)
		if err == nil || c.committed || !c.rolled {
			t.Fatal("failed copy committed", tc, err)
		}
	}
}
func TestBenefitCardCopyPreservesPermanentWindowAndUsesExistingProduct(t *testing.T) {
	s, c := cardCopyStore(t, true)
	item, err := s.CopyLiveAgentPlanBenefit(context.Background(), 14, 4, 7, 8, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !c.committed || len(c.writes) != 2 || item.Key != "活动A01" || item.ProductName != "杨鸭子01" || item.LinkKey != "3号链接" || item.ValidityMode != "permanent" || item.StartsAt == nil || item.EndsAt == nil || item.Gift != "赠试吃装" || item.VersionNo != 1 {
		t.Fatal("benefit content/status/time/history was lost", item, c)
	}
}
func TestBenefitCardCopyMissingLinkConflictAndAuditFailureRollBack(t *testing.T) {
	for _, tc := range []struct {
		tenant, expected int64
		missing          bool
		fail             int
	}{{99, 3, false, 0}, {14, 2, false, 0}, {14, 3, true, 0}, {14, 3, false, 2}} {
		s, c := cardCopyStore(t, true)
		c.missingNext = tc.missing
		c.failAt = tc.fail
		_, err := s.CopyLiveAgentPlanBenefit(context.Background(), tc.tenant, 4, 7, 8, tc.expected)
		if err == nil || c.committed || !c.rolled {
			t.Fatal("invalid or incomplete benefit copy committed", tc, err)
		}
	}
}
