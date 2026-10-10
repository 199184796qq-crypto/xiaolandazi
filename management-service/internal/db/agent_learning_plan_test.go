package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

type learningPlanConnector struct{ conn *learningPlanConn }

func (c learningPlanConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c learningPlanConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type learningPlanConn struct{ args []driver.NamedValue }

func (*learningPlanConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*learningPlanConn) Close() error              { return nil }
func (*learningPlanConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }

type learningPlanResult struct{}

func (learningPlanResult) LastInsertId() (int64, error) { return 88, nil }
func (learningPlanResult) RowsAffected() (int64, error) { return 1, nil }
func (c *learningPlanConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(q, "review_metadata_json") || strings.Count(q, "?") != len(a) {
		return nil, errors.New("invalid plan metadata insert")
	}
	c.args = a
	return learningPlanResult{}, nil
}
func (c *learningPlanConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "WHERE id=? AND tenant_id=? AND room_id=?") || len(a) != 3 || a[0].Value != int64(88) || a[1].Value != int64(14) || a[2].Value != int64(26) {
		return nil, errors.New("unscoped learning read")
	}
	now := time.Now()
	return &contentPolicyTestRows{columns: strings.Fields("id tenant room source ref question reply target status type memory creator created updated adopted review"), values: [][]driver.Value{{int64(88), int64(14), int64(26), "plan_answer_confirmation", "manual:plan-speech-test", "发什么快递？", "使用京东物流。", "发什么快递？", "editing", "", nil, int64(9), now, now, nil, c.args[len(c.args)-1].Value}}}, nil
}
func TestCreateAgentLearningSessionPreservesPlanScopeAndLegacyNull(t *testing.T) {
	for _, planID := range []int64{0, 4} {
		t.Run(string(rune('0'+planID)), func(t *testing.T) {
			c := &learningPlanConn{}
			database := sql.OpenDB(learningPlanConnector{c})
			defer database.Close()
			store := &Store{db: database}
			session, err := store.CreateAgentLearningSession(context.Background(), 9, 14, 26, model.CreateAgentLearningSessionInput{PlanID: planID, SourceType: "plan_answer_confirmation", Question: "发什么快递？", OriginalReply: "使用京东物流。", Target: "发什么快递？"})
			if err != nil {
				t.Fatal(err)
			}
			metadata := c.args[len(c.args)-1].Value
			if planID == 0 {
				if metadata != nil || session.Review != nil {
					t.Fatal("legacy unscoped session changed")
				}
				return
			}
			var review model.InteractionLearningReview
			if err := json.Unmarshal([]byte(metadata.(string)), &review); err != nil {
				t.Fatal(err)
			}
			if review.PlanID != 4 || session.Review == nil || session.Review.PlanID != 4 {
				t.Fatalf("plan metadata lost: %#v", session)
			}
		})
	}
}
