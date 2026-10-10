package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"
)

type avatarConnector struct{ conn *avatarConn }

func (c avatarConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c avatarConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type avatarConn struct {
	kind   string
	writes int
	fail   bool
}

func (*avatarConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (*avatarConn) Close() error                        { return nil }
func (*avatarConn) Begin() (driver.Tx, error)           { return nil, errors.New("unexpected transaction") }
func (c *avatarConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, "FROM live_anchor_style_plugins") {
		if len(args) != 2 || args[0].Value != int64(14) || args[1].Value != int64(7) {
			return nil, errors.New("unscoped plugin read")
		}
		return &contentPolicyTestRows{columns: strings.Fields("id plugin version enabled parameters updated")}, nil
	}
	if !strings.Contains(q, "s.id=? AND s.tenant_id=? AND s.status='active'") || len(args) != 2 {
		return nil, errors.New("unscoped avatar read")
	}
	rows := &contentPolicyTestRows{columns: strings.Fields("id tenant name description status profile samples trainings plans creator updater created updated avatar avatar_data source")}
	if args[0].Value == int64(7) && args[1].Value == int64(14) {
		rows.values = [][]driver.Value{{int64(7), int64(14), "原主播", "原介绍", "active", `{"summary":"原风格"}`, int64(1), int64(3), int64(2), int64(8), int64(8), time.Now(), time.Now(), c.kind, "", int64(0)}}
	}
	return rows, nil
}
func (c *avatarConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.fail {
		return nil, errors.New("database unavailable")
	}
	if q != `UPDATE live_anchor_styles SET avatar_type=?, avatar_data=NULL, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'` || len(args) != 4 || args[1].Value != int64(8) || args[2].Value != int64(7) || args[3].Value != int64(14) {
		return nil, errors.New("avatar update touched unrelated fields or tenant")
	}
	c.kind = args[0].Value.(string)
	c.writes++
	return driver.RowsAffected(0), nil
}
func avatarStore(t *testing.T) (*Store, *avatarConn) {
	t.Helper()
	c := &avatarConn{kind: "female"}
	database := sql.OpenDB(avatarConnector{c})
	t.Cleanup(func() { _ = database.Close() })
	return &Store{db: database}, c
}
func TestAvatarSaveIsNarrowPersistentAndTenantScoped(t *testing.T) {
	for _, kind := range []string{"male", "female", "middle_aged_male", "middle_aged_female"} {
		s, c := avatarStore(t)
		item, err := s.UpdateLiveAnchorStyleAvatar(context.Background(), 14, 7, 8, kind)
		if err != nil {
			t.Fatal(err)
		}
		read, err := s.GetLiveAnchorStyle(context.Background(), 14, 7)
		if err != nil || read.AvatarType != kind || item.Name != "原主播" || item.Description != "原介绍" || item.SampleCount != 1 || item.TrainingCount != 3 || c.writes != 1 {
			t.Fatal("presentation save corrupted presenter or did not persist", item, read, err)
		}
	}
}
func TestAvatarInvalidOrCrossTenantCannotWrite(t *testing.T) {
	for _, input := range []struct {
		tenant, id int64
		kind       string
	}{{14, 7, "invalid"}, {99, 7, "male"}, {14, 99, "male"}} {
		s, c := avatarStore(t)
		_, err := s.UpdateLiveAnchorStyleAvatar(context.Background(), input.tenant, input.id, 8, input.kind)
		if err == nil || c.writes != 0 {
			t.Fatal("invalid/cross-tenant avatar write allowed")
		}
	}
}
func TestAvatarDatabaseFailureDoesNotReportSuccess(t *testing.T) {
	s, c := avatarStore(t)
	c.fail = true
	_, err := s.UpdateLiveAnchorStyleAvatar(context.Background(), 14, 7, 8, "male")
	if err == nil || c.kind != "female" {
		t.Fatal("failed save changed avatar")
	}
}
