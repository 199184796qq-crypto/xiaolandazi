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

type planAssetConnector struct{ conn *planAssetConn }

func (c planAssetConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c planAssetConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type planAssetConn struct {
	mode, rules string
	stored      bool
}

func (*planAssetConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*planAssetConn) Close() error              { return nil }
func (*planAssetConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *planAssetConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "room") || len(args) != 2 {
		return nil, errors.New("room-dependent plan settings")
	}
	rows := &contentPolicyTestRows{}
	valid := args[0].Value == int64(14) && args[1].Value == int64(4)
	switch {
	case strings.Contains(q, "COUNT(*)"):
		rows.columns = []string{"count"}
		n := int64(0)
		if valid {
			n = 1
		}
		rows.values = [][]driver.Value{{n}}
	case strings.Contains(q, "live_agent_plan_asset_settings"):
		rows.columns = []string{"mode", "rules"}
		if valid && c.stored {
			rows.values = [][]driver.Value{{c.mode, c.rules}}
		}
	case strings.Contains(q, "live_agent_plan_versions"):
		rows.columns = []string{"mode"}
		if valid {
			rows.values = [][]driver.Value{{model.LiveContentUserAudio}}
		}
	default:
		return nil, errors.New("unexpected query")
	}
	return rows, nil
}
func (c *planAssetConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(q, "room") || len(args) != 5 || args[0].Value != int64(14) || args[1].Value != int64(4) {
		return nil, errors.New("invalid plan scope")
	}
	if !c.stored {
		c.mode = args[2].Value.(string)
		c.rules = args[3].Value.(string)
		c.stored = true
	}
	if strings.Contains(q, "UPDATE content_mode=") {
		c.mode = args[2].Value.(string)
	}
	if strings.Contains(q, "UPDATE reading_rules=") {
		c.rules = args[3].Value.(string)
	}
	return driver.RowsAffected(1), nil
}

func TestPlanAssetSettingsWithoutRoomsPreserveModeAndRules(t *testing.T) {
	c := &planAssetConn{}
	database := sql.OpenDB(planAssetConnector{c})
	defer database.Close()
	s := &Store{db: database}
	ctx := context.Background()
	initial, err := s.GetPlanAssetSettings(ctx, 14, 4)
	if err != nil || initial.ContentMode != model.LiveContentUserAudio {
		t.Fatal(initial, err)
	}
	if _, err = s.SetPlanReadingRules(ctx, 14, 4, 8, "绸都 => chou2 du1"); err != nil {
		t.Fatal(err)
	}
	changed, err := s.SetPlanAssetMode(ctx, 14, 4, 8, model.LiveContentAIDynamic)
	if err != nil || changed.ReadingRules != "绸都 => chou2 du1" {
		t.Fatal(changed, err)
	}
	if _, err = s.SetPlanReadingRules(ctx, 14, 4, 8, ""); err != nil {
		t.Fatal(err)
	}
	changed, err = s.GetPlanAssetSettings(ctx, 14, 4)
	if err != nil || changed.ContentMode != model.LiveContentAIDynamic || changed.ReadingRules != "" {
		t.Fatal(changed, err)
	}
	for _, scope := range [][2]int64{{24, 4}, {14, 5}} {
		if _, err = s.SetPlanReadingRules(ctx, scope[0], scope[1], 8, "wrong account"); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("cross-plan write", err)
		}
	}
	if _, err = s.SetPlanAssetMode(ctx, 14, 4, 8, "unknown"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
