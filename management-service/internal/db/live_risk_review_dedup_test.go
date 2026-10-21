package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

type riskDedupState struct {
	writes            []string
	args              [][]driver.NamedValue
	committed, rolled bool
	failBackup        bool
	liveRows          [][]driver.Value
	backupRows        [][]driver.Value
	customRows        bool
}
type riskDedupConnector struct{ state *riskDedupState }

func (c riskDedupConnector) Connect(context.Context) (driver.Conn, error) {
	return &riskDedupConn{c.state}, nil
}
func (c riskDedupConnector) Driver() driver.Driver { return contentPolicyTestDriver{} }

type riskDedupConn struct{ state *riskDedupState }

func (c *riskDedupConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("no statements") }
func (c *riskDedupConn) Close() error                        { return nil }
func (c *riskDedupConn) Begin() (driver.Tx, error)           { return riskDedupTx{c.state}, nil }
func (c *riskDedupConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) != 2 || args[0].Value != int64(7) || args[1].Value != int64(17) {
		return nil, errors.New("unscoped read")
	}
	if strings.Contains(q, "SELECT id FROM live_agent_plans") {
		return &contentPolicyTestRows{columns: []string{"id"}, values: [][]driver.Value{{int64(17)}}}, nil
	}
	if strings.Contains(q, "SELECT claim_sha256,status") {
		columns := []string{"hash", "status", "evidence", "semantic_decision", "semantic_prompt_sha", "semantic_reason", "semantic_suggestion", "semantic_provider", "semantic_model", "semantic_at", "appeal_status", "appeal_reason", "appeal_by", "appeal_at", "by", "at"}
		if strings.Contains(q, "FROM live_fact_risk_reviews_dedup_backup") {
			return &contentPolicyTestRows{columns: columns, values: c.state.backupRows}, nil
		}
		columns = append(columns, "active")
		if c.state.customRows {
			return &contentPolicyTestRows{columns: columns, values: c.state.liveRows}, nil
		}
		return &contentPolicyTestRows{columns: columns, values: [][]driver.Value{{"canonical", "confirmed", "原报告", "not_applicable", "", "", "", "", "", nil, "", "", nil, nil, int64(9), time.Now(), true}, {"legacy-duplicate", "confirmed", "旧范围未知报告", "not_applicable", "", "", "", "", "", nil, "", "", nil, nil, int64(9), time.Now(), true}}}, nil
	}
	return nil, errors.New("unexpected query")
}
func (c *riskDedupConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if len(args) < 2 || args[0].Value != int64(7) || args[1].Value != int64(17) {
		return nil, errors.New("unscoped write")
	}
	c.state.writes = append(c.state.writes, q)
	c.state.args = append(c.state.args, args)
	if strings.Contains(q, "dedup_backup") && c.state.failBackup {
		return nil, errors.New("backup failed")
	}
	return driver.RowsAffected(1), nil
}

type riskDedupTx struct{ state *riskDedupState }

func (t riskDedupTx) Commit() error   { t.state.committed = true; return nil }
func (t riskDedupTx) Rollback() error { t.state.rolled = true; return nil }

func TestRiskDedupPreservesConfirmationAndHidesAbsentRows(t *testing.T) {
	state := &riskDedupState{}
	conn := sql.OpenDB(riskDedupConnector{state})
	defer conn.Close()
	store := &Store{db: conn}
	item := model.LiveRiskReview{ClaimSHA256: "canonical", SourceKey: "facts/2/0", Text: "非转基因属性依据", RuleID: "example.attribute", Severity: "P1", RuleSHA256: "policy", SemanticKey: "atomic/v1/product/batch", Sources: []model.LiveRiskReviewSource{{SourceKey: "facts/2/0", Text: "非转基因"}, {SourceKey: "wheel/0/name", Text: "非转基因原料"}}}
	result, err := store.SyncLiveRiskReviews(context.Background(), 7, 17, []model.LiveRiskReview{item, item})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result["canonical"].Evidence != "原报告" || result["canonical"].Status != "confirmed" {
		t.Fatal("confirmation lost", result)
	}
	if !state.committed || len(state.writes) != 3 || !strings.Contains(state.writes[1], "dedup_backup") || !strings.Contains(state.writes[2], "SET active=0") {
		t.Fatal("not archive-before-hide", state)
	}
	if len(state.args[0]) != 28 || !strings.Contains(state.args[0][27].Value.(string), "wheel/0/name") {
		t.Fatal("source refs not stored")
	}
	if state.args[2][2].Value != "legacy-duplicate" {
		t.Fatal("hid valid canonical row")
	}
}

func TestRiskDedupBackupFailureRollsBackWithoutDeletion(t *testing.T) {
	state := &riskDedupState{failBackup: true}
	conn := sql.OpenDB(riskDedupConnector{state})
	defer conn.Close()
	store := &Store{db: conn}
	_, err := store.SyncLiveRiskReviews(context.Background(), 7, 17, nil)
	if err == nil || state.committed || !state.rolled {
		t.Fatal("failed backup committed")
	}
	for _, q := range state.writes {
		if strings.Contains(q, "DELETE FROM") || strings.Contains(q, "SET active=0") {
			t.Fatal("removed before backup")
		}
	}
}

func reviewFixture(hash, status, decision, prompt, reason string) []driver.Value {
	return []driver.Value{hash, status, "用户确认说明", decision, prompt, reason, "", "provider", "model", time.Date(2026, 10, 8, 13, 0, 0, 0, time.UTC), "", "", nil, nil, int64(9), time.Date(2026, 10, 8, 13, 30, 0, 0, time.UTC)}
}

func TestRiskConfirmationSurvivesAbsenceAndLegacyBackup(t *testing.T) {
	for _, kind := range []string{"inactive", "backup", "recreated-pending"} {
		t.Run(kind, func(t *testing.T) {
			old := reviewFixture("same", "confirmed", "pass", "prompt", "保存建议")
			state := &riskDedupState{customRows: true}
			switch kind {
			case "inactive":
				state.liveRows = [][]driver.Value{append(old, false)}
			case "backup":
				state.backupRows = [][]driver.Value{old}
			case "recreated-pending":
				state.backupRows = [][]driver.Value{old}
				state.liveRows = [][]driver.Value{append(reviewFixture("same", "pending", "pass", "prompt", "保存建议"), true)}
			}
			conn := sql.OpenDB(riskDedupConnector{state})
			defer conn.Close()
			item := model.LiveRiskReview{ClaimSHA256: "same", SourceKey: "wheel/8/angle/1", Severity: "P0", SemanticDecision: "pending", SemanticPromptSHA: "prompt"}
			result, err := (&Store{db: conn}).SyncLiveRiskReviews(context.Background(), 7, 17, []model.LiveRiskReview{item})
			if err != nil || result["same"].Status != "confirmed" || result["same"].ConfirmedAt == nil || result["same"].ConfirmedAt.Hour() != 13 || len(state.writes) != 1 {
				t.Fatalf("confirmation was not reused: %+v, %v, writes=%v", result, err, state.writes)
			}
		})
	}
}

func TestRiskBackupNeverOverridesChangedIdentityOrPendingAppeal(t *testing.T) {
	for _, kind := range []string{"changed-claim", "appeal"} {
		t.Run(kind, func(t *testing.T) {
			state := &riskDedupState{customRows: true, backupRows: [][]driver.Value{reviewFixture("same", "confirmed", "pass", "prompt", "旧通过")}}
			item := model.LiveRiskReview{ClaimSHA256: "same", Severity: "P0", SemanticDecision: "not_required"}
			switch kind {
			case "changed-claim":
				item.ClaimSHA256 = "new"
			case "appeal":
				row := reviewFixture("same", "pending", "not_required", "prompt", "")
				row[10] = "pending"
				state.liveRows = [][]driver.Value{append(row, true)}
			}
			conn := sql.OpenDB(riskDedupConnector{state})
			defer conn.Close()
			result, err := (&Store{db: conn}).SyncLiveRiskReviews(context.Background(), 7, 17, []model.LiveRiskReview{item})
			if err != nil || result[item.ClaimSHA256].Status == "confirmed" {
				t.Fatalf("unsafe confirmation restored: %+v %v", result, err)
			}
		})
	}
}

