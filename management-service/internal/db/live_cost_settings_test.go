package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
)

type liveCostSettingState struct {
	mu          sync.Mutex
	raw         *string
	actor       int64
	lockedReads int
}
type liveCostSettingConnector struct{ state *liveCostSettingState }

func (c liveCostSettingConnector) Connect(context.Context) (driver.Conn, error) {
	return &liveCostSettingConn{state: c.state}, nil
}
func (liveCostSettingConnector) Driver() driver.Driver { return liveCostSettingDriver{} }

type liveCostSettingDriver struct{}

func (liveCostSettingDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("只能通过测试连接器使用")
}

type liveCostSettingConn struct {
	state *liveCostSettingState
	inTx  bool
	raw   *string
	actor int64
}

func (*liveCostSettingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("不应准备查询")
}
func (*liveCostSettingConn) Close() error { return nil }
func (c *liveCostSettingConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *liveCostSettingConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if opts.Isolation != driver.IsolationLevel(sql.LevelReadCommitted) {
		return nil, errors.New("必须使用读已提交事务")
	}
	c.state.mu.Lock()
	c.inTx = true
	c.raw = c.state.raw
	c.actor = c.state.actor
	return liveCostSettingTx{c}, nil
}

type liveCostSettingTx struct{ c *liveCostSettingConn }

func (tx liveCostSettingTx) Commit() error {
	tx.c.state.raw = tx.c.raw
	tx.c.state.actor = tx.c.actor
	tx.c.inTx = false
	tx.c.state.mu.Unlock()
	return nil
}
func (tx liveCostSettingTx) Rollback() error { tx.c.inTx = false; tx.c.state.mu.Unlock(); return nil }
func (c *liveCostSettingConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !c.inTx {
		return nil, errors.New("保存必须持事务锁")
	}
	switch {
	case strings.Contains(query, "INSERT INTO mgmt_system_settings"):
		if len(args) != 6 || args[0].Value != LiveCostSetting || !strings.Contains(query, "ON DUPLICATE KEY UPDATE setting_key=VALUES(setting_key)") {
			return nil, errors.New("初始化不能覆盖已有配置")
		}
		if c.raw == nil {
			value := args[3].Value.(string)
			c.raw = &value
		}
	case strings.Contains(query, "UPDATE mgmt_system_settings SET value_text="):
		if len(args) != 3 || args[2].Value != LiveCostSetting {
			return nil, errors.New("更新缺少配置键")
		}
		value := args[0].Value.(string)
		c.raw = &value
		c.actor = args[1].Value.(int64)
	default:
		return nil, errors.New("未预期的配置写入")
	}
	return driver.RowsAffected(1), nil
}
func (c *liveCostSettingConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) != 1 || args[0].Value != LiveCostSetting || !strings.Contains(query, "SELECT value_text FROM mgmt_system_settings") {
		return nil, errors.New("未预期的配置查询")
	}
	if !c.inTx {
		c.state.mu.Lock()
		defer c.state.mu.Unlock()
	} else if !strings.Contains(query, "FOR UPDATE") {
		return nil, errors.New("保存缺少当前读锁")
	}
	var raw *string
	if c.inTx {
		raw = c.raw
		c.state.lockedReads++
	} else {
		raw = c.state.raw
	}
	return &liveCostSettingRows{raw: raw}, nil
}

type liveCostSettingRows struct {
	raw  *string
	done bool
}

func (*liveCostSettingRows) Columns() []string { return []string{"value_text"} }
func (*liveCostSettingRows) Close() error      { return nil }
func (r *liveCostSettingRows) Next(out []driver.Value) error {
	if r.done || r.raw == nil {
		return io.EOF
	}
	out[0] = *r.raw
	r.done = true
	return nil
}

func liveCostSettingsFixture(t *testing.T, raw *string) (*Store, *liveCostSettingState) {
	t.Helper()
	state := &liveCostSettingState{raw: raw}
	database := sql.OpenDB(liveCostSettingConnector{state})
	t.Cleanup(func() { _ = database.Close() })
	return &Store{db: database}, state
}

func TestLiveCostSettingsStoreDistinguishesAbsentFromCorruption(t *testing.T) {
	store, _ := liveCostSettingsFixture(t, nil)
	got, err := store.LiveCostSettings(context.Background())
	if err != nil || !reflect.DeepEqual(got, livecost.DefaultSettings()) {
		t.Fatal(got, err)
	}
	for _, raw := range []string{"", `{}`, `not-json`, `{"tts_price_micros_per_10k":0}`} {
		store, _ := liveCostSettingsFixture(t, &raw)
		if _, err := store.LiveCostSettings(context.Background()); err == nil {
			t.Fatal("损坏配置被当作默认值", raw)
		}
	}
}

func TestLiveCostSettingsStoreCASAllowsOnlyOneConcurrentAdministrator(t *testing.T) {
	store, state := liveCostSettingsFixture(t, nil)
	first, second := livecost.DefaultSettings(), livecost.DefaultSettings()
	first.TTSPriceMicrosPer10K, second.TTSPriceMicrosPer10K = 400_000, 600_000
	errs := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for index, edit := range []livecost.Settings{first, second} {
		go func(edit livecost.Settings, actor int64) {
			start.Wait()
			_, err := store.SaveLiveCostSettings(context.Background(), edit, actor)
			errs <- err
		}(edit, int64(index+17))
	}
	start.Done()
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-errs
		if err == nil {
			successes++
		} else if errors.Is(err, ErrLiveCostRevision) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	got, err := store.LiveCostSettings(context.Background())
	if err != nil || successes != 1 || conflicts != 1 || got.Revision != 1 || state.lockedReads != 2 || (state.actor != 17 && state.actor != 18) {
		t.Fatal(got, err, successes, conflicts, state)
	}
	if got.TTSPriceMicrosPer10K != 400_000 && got.TTSPriceMicrosPer10K != 600_000 {
		t.Fatal("保存未保留较低配置价", got)
	}
	if ErrLiveCostRevision.Error() != "费用配置已变化，请重新读取后再保存" {
		t.Fatal(ErrLiveCostRevision)
	}
}

func TestLiveCostSettingsStoreRejectsInvalidOrGenericWriteWithoutTouchingDatabase(t *testing.T) {
	store := &Store{}
	invalid := livecost.DefaultSettings()
	invalid.TTSPriceMicrosPer10K = 0
	if _, err := store.SaveLiveCostSettings(context.Background(), invalid, 17); err == nil {
		t.Fatal("零价被保存")
	}
	for _, key := range []string{LiveCostSetting, " " + LiveCostSetting + " "} {
		if err := store.UpdateSystemSettings(context.Background(), []model.SystemSettingUpdate{{Key: key, Value: `{}`}}, 17); err == nil || !strings.Contains(err.Error(), "专用配置面板") {
			t.Fatal("通用写入绕过配置校验", err)
		}
	}
}

func TestLiveCostSettingsStoreCorruptionCannotBeOverwrittenAsNewDefault(t *testing.T) {
	raw := `{}`
	store, state := liveCostSettingsFixture(t, &raw)
	if _, err := store.SaveLiveCostSettings(context.Background(), livecost.DefaultSettings(), 17); err == nil {
		t.Fatal("损坏配置被默默覆盖")
	}
	if *state.raw != raw || state.actor != 0 {
		t.Fatal("拒绝后仍写入配置", state)
	}
}

func TestLiveCostSettingsMigrationPreservesAdministratorChoiceMySQL(t *testing.T) {
	store := salesIsolatedMySQL(t)
	ctx := context.Background()
	if err := store.MigrateSystemSettings(ctx); err != nil {
		t.Fatal(err)
	}
	config, err := store.LiveCostSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	config.TTSPriceMicrosPer10K = 400_000
	saved, err := store.SaveLiveCostSettings(ctx, config, 17)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateSystemSettings(ctx); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LiveCostSettings(ctx)
	if err != nil || !reflect.DeepEqual(saved, loaded) {
		t.Fatal("迁移覆盖已保存的费率", loaded, err)
	}
}
