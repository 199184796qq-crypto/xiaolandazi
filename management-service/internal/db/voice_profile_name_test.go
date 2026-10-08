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

type voiceNameConnector struct{ c *voiceNameConn }

func (c voiceNameConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (c voiceNameConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type voiceNameConn struct {
	name   string
	writes int
}

func (*voiceNameConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*voiceNameConn) Close() error              { return nil }
func (*voiceNameConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *voiceNameConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(q, "WHERE tenant_id=? AND id=? AND BINARY name=?") || len(args) != 5 {
		return nil, errors.New("unscoped name change")
	}
	set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
	for _, field := range []string{"voice_id", "provider", "config_json", "sample_asset_id", "clone_status", "is_default"} {
		if strings.Contains(set, field) {
			return nil, errors.New("rename overwrote identity")
		}
	}
	if args[2].Value != int64(101) || args[3].Value != int64(41) || args[4].Value != c.name {
		return driver.RowsAffected(0), nil
	}
	c.name = args[0].Value.(string)
	c.writes++
	return driver.RowsAffected(1), nil
}
func (c *voiceNameConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	rows := &contentPolicyTestRows{columns: strings.Fields("id tenant agent name provider voice sample status config default creator updater created updated")}
	if !strings.Contains(q, "WHERE id=? AND tenant_id=?") || len(args) != 2 {
		return nil, errors.New("unscoped voice read")
	}
	if args[0].Value == int64(41) && args[1].Value == int64(101) {
		rows.values = [][]driver.Value{{int64(41), int64(101), nil, c.name, "supplier", "voice-immutable", int64(701), "ready", `{"target_model":"original-model"}`, false, int64(5), int64(5), time.Now(), time.Now()}}
	}
	return rows, nil
}
func TestVoiceRenameOnlyChangesNameAndRejectsOtherTenantOrStaleName(t *testing.T) {
	conn := &voiceNameConn{name: "小海"}
	database := sql.OpenDB(voiceNameConnector{conn})
	defer database.Close()
	store := &Store{db: database}
	result, err := store.RenameVoiceProfile(context.Background(), 101, 41, 5, " 新名字 ", "小海")
	if err != nil || result.Name != "新名字" || result.VoiceID != "voice-immutable" || result.Config["target_model"] != "original-model" || *result.SampleAssetID != 701 || result.CloneStatus != "ready" {
		t.Fatal(result, err)
	}
	if _, err = store.RenameVoiceProfile(context.Background(), 101, 41, 5, "旧确认", "小海"); !errors.Is(err, ErrVoiceProfileNameConflict) {
		t.Fatal("stale rename accepted", err)
	}
	if _, err = store.RenameVoiceProfile(context.Background(), 202, 41, 5, "跨账号", "新名字"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross tenant accepted", err)
	}
	for _, name := range []string{" ", strings.Repeat("字", 81), "不\n允许"} {
		if _, err = store.RenameVoiceProfile(context.Background(), 101, 41, 5, name, "新名字"); err == nil {
			t.Fatal("invalid name accepted")
		}
	}
	if conn.writes != 1 {
		t.Fatal(conn.writes)
	}
}
