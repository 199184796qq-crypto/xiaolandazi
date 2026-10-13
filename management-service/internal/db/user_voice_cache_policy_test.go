package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

type userCacheTestDB struct {
	values    map[string]string
	snapshot  map[string]string
	failWrite bool
}
type userCacheConnector struct{ state *userCacheTestDB }

func (c userCacheConnector) Connect(context.Context) (driver.Conn, error) {
	return &userCacheConn{c.state}, nil
}
func (c userCacheConnector) Driver() driver.Driver { return userCacheDriver{} }

type userCacheDriver struct{}

func (userCacheDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type userCacheConn struct{ state *userCacheTestDB }

func (c *userCacheConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *userCacheConn) Close() error { return nil }
func (c *userCacheConn) Begin() (driver.Tx, error) {
	c.state.snapshot = map[string]string{}
	for k, v := range c.state.values {
		c.state.snapshot[k] = v
	}
	return userCacheTx{c.state}, nil
}
func (c *userCacheConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	r := &userCacheRows{columns: []string{"value_text"}}
	if strings.Contains(q, "FROM mgmt_users") {
		if args[0].Value == int64(14) || args[0].Value == int64(15) {
			r.values = [][]driver.Value{{int64(1)}}
		}
		return r, nil
	}
	if value, ok := c.state.values[args[0].Value.(string)]; ok {
		r.values = [][]driver.Value{{value}}
	}
	return r, nil
}
func (c *userCacheConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if c.state.failWrite {
		return nil, errors.New("write unavailable")
	}
	key := args[0].Value.(string)
	if strings.HasPrefix(q, "DELETE") {
		delete(c.state.values, key)
	} else {
		c.state.values[key] = args[2].Value.(string)
	}
	return driver.RowsAffected(1), nil
}

type userCacheTx struct{ state *userCacheTestDB }

func (t userCacheTx) Commit() error { t.state.snapshot = nil; return nil }
func (t userCacheTx) Rollback() error {
	if t.state.snapshot != nil {
		t.state.values = t.state.snapshot
		t.state.snapshot = nil
	}
	return nil
}

type userCacheRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *userCacheRows) Columns() []string { return r.columns }
func (r *userCacheRows) Close() error      { return nil }
func (r *userCacheRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func userCacheStore(t *testing.T) (*Store, *userCacheTestDB) {
	t.Helper()
	data := &userCacheTestDB{values: map[string]string{model.MainlineGenerationCacheReuseSetting: "false", model.InteractionGenerationCacheReuseSetting: "true", AnchorVoiceCacheRetentionSetting: "5"}}
	database := sql.OpenDB(userCacheConnector{data})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}, data
}

func TestUserVoiceCacheOverrideIsolationAndRestore(t *testing.T) {
	s, _ := userCacheStore(t)
	ctx := context.Background()
	settings := &model.UserVoiceCacheSettings{MainlineReuse: true, InteractionReuse: false, RetentionHours: 12}
	if err := s.UpdateUserVoiceCachePolicy(ctx, 14, 42, settings); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		tenant                int64
		mainline, interaction bool
		hours                 int
	}{{14, true, false, 12}, {15, false, true, 5}} {
		mainline, e1 := s.GenerationCacheReuseEnabledForTenant(ctx, test.tenant, model.MainlineGenerationCacheReuseSetting)
		interaction, e2 := s.GenerationCacheReuseEnabledForTenant(ctx, test.tenant, model.InteractionGenerationCacheReuseSetting)
		hours, e3 := s.AnchorVoiceCacheRetentionHoursForTenant(ctx, test.tenant)
		if e1 != nil || e2 != nil || e3 != nil || mainline != test.mainline || interaction != test.interaction || hours != test.hours {
			t.Fatalf("tenant %d leaked policy: %v %v %d", test.tenant, mainline, interaction, hours)
		}
	}
	if global, err := s.GenerationCacheReuseEnabled(ctx, model.MainlineGenerationCacheReuseSetting); err != nil || global {
		t.Fatal("user override changed platform default")
	}
	settings.MainlineReuse = false
	if err := s.UpdateUserVoiceCachePolicy(ctx, 14, 42, settings); err != nil {
		t.Fatal(err)
	}
	if enabled, _ := s.GenerationCacheReuseEnabledForTenant(ctx, 14, model.MainlineGenerationCacheReuseSetting); enabled {
		t.Fatal("explicit false was lost")
	}
	if err := s.UpdateUserVoiceCachePolicy(ctx, 14, 42, nil); err != nil {
		t.Fatal(err)
	}
	if hours, err := s.AnchorVoiceCacheRetentionHoursForTenant(ctx, 14); err != nil || hours != 5 {
		t.Fatal("reset did not inherit default", hours, err)
	}
}

func TestUserVoiceCacheInvalidAndFailedWritesPreserveSettings(t *testing.T) {
	s, data := userCacheStore(t)
	ctx := context.Background()
	for _, hours := range []int{0, 169} {
		if err := s.UpdateUserVoiceCachePolicy(ctx, 14, 42, &model.UserVoiceCacheSettings{RetentionHours: hours}); err == nil {
			t.Fatal("invalid lifetime saved")
		}
	}
	if err := s.UpdateUserVoiceCachePolicy(ctx, 999, 42, &model.UserVoiceCacheSettings{RetentionHours: 5}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("noncustomer policy saved", err)
	}
	if _, err := decodeUserVoiceCacheSettings(`{"retention_hours":5}`); err == nil {
		t.Fatal("missing toggles silently became false")
	}
	key, _ := userVoiceCacheKey(14)
	data.values[key] = `{"retention_hours":12,"mainline_cache_reuse_enabled":true,"interaction_cache_reuse_enabled":false}`
	data.failWrite = true
	if err := s.UpdateUserVoiceCachePolicy(ctx, 14, 42, nil); err == nil {
		t.Fatal("failed reset reported success")
	}
	if hours, err := s.AnchorVoiceCacheRetentionHoursForTenant(ctx, 14); err != nil || hours != 12 {
		t.Fatal("failed reset changed settings")
	}
	data.values[key] = `{"retention_hours":5}`
	if enabled, err := s.GenerationCacheReuseEnabledForTenant(ctx, 14, model.MainlineGenerationCacheReuseSetting); err == nil || enabled {
		t.Fatal("invalid stored policy authorized reuse")
	}
	if _, err := s.GenerationCacheReuseEnabledForTenant(ctx, 0, model.MainlineGenerationCacheReuseSetting); err == nil {
		t.Fatal("invalid tenant fell back to global")
	}
}
