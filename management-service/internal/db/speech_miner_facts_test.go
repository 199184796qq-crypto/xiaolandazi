package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

type minerFactConnector struct{ conn *minerFactConn }

func (c minerFactConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (minerFactConnector) Driver() driver.Driver                          { return openingAssetTestDriver{} }

type minerFactConn struct {
	committed, rolled, conflict, identical bool
	failRevision                           bool
	writes                                 []string
	args                                   [][]driver.NamedValue
}

func (*minerFactConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*minerFactConn) Close() error                { return nil }
func (c *minerFactConn) Begin() (driver.Tx, error) { return minerFactTx{c}, nil }

type minerFactTx struct{ c *minerFactConn }

func (t minerFactTx) Commit() error   { t.c.committed = true; return nil }
func (t minerFactTx) Rollback() error { t.c.rolled = true; return nil }
func (c *minerFactConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "FOR UPDATE") {
		return nil, errors.New("batch not serialized")
	}
	if strings.HasPrefix(q, "SELECT 1 FROM live_agent_plans") {
		rows := &contentPolicyTestRows{columns: []string{"exists"}}
		if args[0].Value == int64(4) && args[1].Value == int64(14) {
			rows.values = [][]driver.Value{{int64(1)}}
		}
		return rows, nil
	}
	if !strings.HasPrefix(q, "SELECT id,fact_value,status FROM live_agent_plan_facts") || args[0].Value != int64(14) || args[1].Value != int64(4) {
		return nil, errors.New("unscoped fact read")
	}
	rows := &contentPolicyTestRows{columns: []string{"id", "value", "status"}}
	if c.identical {
		rows.values = [][]driver.Value{{int64(7), "常温保存", "active"}}
	}
	if c.conflict && args[3].Value == "冲突事实" {
		rows.values = [][]driver.Value{{int64(8), "旧内容", "active"}}
	}
	return rows, nil
}
func (c *minerFactConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(q, "INSERT INTO live_agent_plan_fact") || args[0].Value != int64(14) || args[1].Value != int64(4) || strings.Count(q, "?") != len(args) {
		return nil, errors.New("unexpected write or tenant scope")
	}
	if c.failRevision && strings.Contains(q, "fact_revisions") {
		return nil, errors.New("audit write failed")
	}
	c.writes = append(c.writes, q)
	c.args = append(c.args, args)
	return cardCopyResult{id: 71}, nil
}
func minerStore(t *testing.T) (*Store, *minerFactConn) {
	t.Helper()
	c := &minerFactConn{}
	database := sql.OpenDB(minerFactConnector{c})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}, c
}
func minerFact() model.LiveAgentPlanFactCandidate {
	return model.LiveAgentPlanFactCandidate{Category: "product", Key: "保存方式", Value: "常温保存", SourceQuote: "本品常温保存"}
}
func TestMinerFactsCommitScopedRowsAndAuditOnly(t *testing.T) {
	s, c := minerStore(t)
	n, err := s.SaveSpeechMinerFacts(context.Background(), 14, 4, 8, []model.LiveAgentPlanFactCandidate{minerFact()}, "speech-miner:guided:session-1")
	if err != nil || n != 1 || !c.committed || c.rolled || len(c.writes) != 2 {
		t.Fatal(n, err, c)
	}
	for _, args := range c.args {
		if args[len(args)-1].Value != int64(8) {
			t.Fatal("actor audit lost", args)
		}
		found := false
		for _, a := range args {
			if a.Value == "speech-miner:guided:session-1" {
				found = true
			}
		}
		if !found {
			t.Fatal("session provenance lost")
		}
	}
}
func TestMinerFactsIdenticalRetryDoesNotDuplicate(t *testing.T) {
	s, c := minerStore(t)
	c.identical = true
	n, err := s.SaveSpeechMinerFacts(context.Background(), 14, 4, 8, []model.LiveAgentPlanFactCandidate{minerFact()}, "session-1")
	if err != nil || n != 0 || !c.committed || len(c.writes) != 0 {
		t.Fatal(n, err, c)
	}
}
func TestMinerFactsConflictOrRevisionFailureRollsBackWholeBatch(t *testing.T) {
	for _, failAudit := range []bool{false, true} {
		s, c := minerStore(t)
		c.conflict = !failAudit
		c.failRevision = failAudit
		facts := []model.LiveAgentPlanFactCandidate{minerFact(), {Category: "product", Key: "冲突事实", Value: "新内容", SourceQuote: "新内容"}}
		n, err := s.SaveSpeechMinerFacts(context.Background(), 14, 4, 8, facts, "session-1")
		if err == nil || n != 0 || c.committed || !c.rolled {
			t.Fatal("partial batch committed", n, err, c)
		}
		if !failAudit && !errors.Is(err, ErrSpeechMinerFactConflict) {
			t.Fatal(err)
		}
	}
}
func TestMinerFactsCannotWriteOtherTenantPlan(t *testing.T) {
	s, c := minerStore(t)
	_, err := s.SaveSpeechMinerFacts(context.Background(), 99, 4, 8, []model.LiveAgentPlanFactCandidate{minerFact()}, "session-1")
	if !errors.Is(err, ErrLiveAgentPlanNotFound) || c.committed || len(c.writes) != 0 || !c.rolled {
		t.Fatal(err, c)
	}
}
