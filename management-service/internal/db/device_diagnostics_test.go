package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/deviceops"
)

type diagnosticSQLState struct {
	mu         sync.Mutex
	tenant     int64
	claim      time.Time
	cid        string
	entries    []deviceops.StoredBatch
	failCommit bool
	quota      bool
	schema     string
}
type diagnosticConnector struct{ s *diagnosticSQLState }

func (c diagnosticConnector) Connect(context.Context) (driver.Conn, error) {
	return &diagnosticConn{s: c.s}, nil
}
func (c diagnosticConnector) Driver() driver.Driver { return diagnosticDriver{} }

type diagnosticDriver struct{}

func (diagnosticDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("只允许测试连接器")
}

type diagnosticConn struct {
	s       *diagnosticSQLState
	entries []deviceops.StoredBatch
	inTx    bool
}

func (c *diagnosticConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("不应预备查询")
}
func (c *diagnosticConn) Close() error { return nil }
func (c *diagnosticConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *diagnosticConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	c.s.mu.Lock()
	c.inTx = true
	c.entries = append([]deviceops.StoredBatch(nil), c.s.entries...)
	return diagnosticTx{c}, nil
}

type diagnosticTx struct{ c *diagnosticConn }

func (t diagnosticTx) Commit() error {
	defer t.c.s.mu.Unlock()
	t.c.inTx = false
	if t.c.s.failCommit {
		return errors.New("commit unavailable")
	}
	t.c.s.entries = t.c.entries
	return nil
}
func (t diagnosticTx) Rollback() error {
	if t.c.inTx {
		t.c.inTx = false
		t.c.s.mu.Unlock()
	}
	return nil
}

type diagnosticRows struct {
	values [][]driver.Value
	n      int
}

func (r *diagnosticRows) Columns() []string {
	columns := make([]string, r.n)
	for i := range columns {
		columns[i] = "column"
	}
	return columns
}
func (r *diagnosticRows) Close() error { return nil }
func (r *diagnosticRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func diagnosticRow(v ...driver.Value) driver.Rows {
	return &diagnosticRows{values: [][]driver.Value{v}, n: len(v)}
}
func diagnosticValues(b deviceops.StoredBatch) []driver.Value {
	return []driver.Value{b.ID, b.TenantID, b.DeviceID, b.OwnerEpoch, b.BatchID, b.BootID, b.FirmwareVersion, b.FirmwareSHA256, b.Storage, int64(b.SeqStart), int64(b.SeqEnd), int64(b.EventCount), b.SizeBytes, b.ChecksumSHA256, b.StorageDriver, b.ObjectKey, b.ReceivedAt}
}
func (c *diagnosticConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "SELECT device_id FROM device_hardware_profiles"):
		return diagnosticRow(int64(7)), nil
	case strings.Contains(q, "SELECT current_customer_id,quality_status,lifecycle_status"):
		return diagnosticRow(c.s.tenant, "qualified", "ACTIVE"), nil
	case strings.Contains(q, "SELECT claimed_tenant_id"):
		return diagnosticRow(c.s.tenant), nil
	case strings.Contains(q, "SELECT b.room_id,b.binding_role"):
		return &diagnosticRows{n: 2}, nil
	case strings.Contains(q, "SELECT claimed_at"):
		return diagnosticRow(c.s.claim), nil
	case strings.Contains(q, "SELECT hardware_mac"):
		return diagnosticRow("1c:29:04:31:0e:b8"), nil
	case strings.Contains(q, "SELECT connection_status"):
		return diagnosticRow("online", `{"gateway_connection_id":"`+c.s.cid+`","transport":"xiaozhi_websocket_proven"}`, time.Now()), nil
	case strings.Contains(q, "COUNT(*) FROM live_runtime_sessions"):
		return diagnosticRow(int64(0)), nil
	case strings.Contains(q, "COALESCE(SUM(received_at"):
		if c.s.quota {
			return diagnosticRow(int64(6), int64(6), int64(1)), nil
		}
		return diagnosticRow(int64(0), int64(len(c.entries)), int64(0)), nil
	case strings.Contains(q, "FROM device_diagnostic_batches"):
		rows := &diagnosticRows{n: 17}
		for _, b := range c.entries {
			if b.TenantID != a[0].Value.(int64) || b.DeviceID != a[1].Value.(int64) {
				continue
			}
			if strings.Contains(q, "owner_epoch=?") && (b.OwnerEpoch != a[2].Value.(string) || b.BatchID != a[3].Value.(string)) {
				continue
			}
			rows.values = append(rows.values, diagnosticValues(b))
		}
		return rows, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (c *diagnosticConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "CREATE TABLE") {
		c.s.schema = q
		return driver.RowsAffected(0), nil
	}
	if !c.inTx || !strings.HasPrefix(q, "INSERT INTO device_diagnostic_batches") {
		return nil, errors.New("unexpected exec")
	}
	b := deviceops.StoredBatch{ID: int64(len(c.entries) + 1), TenantID: a[0].Value.(int64), DeviceID: a[1].Value.(int64), OwnerEpoch: a[2].Value.(string), BatchID: a[3].Value.(string), BootID: a[4].Value.(string), FirmwareVersion: a[5].Value.(string), FirmwareSHA256: a[6].Value.(string), Storage: a[7].Value.(string), SeqStart: uint64(a[8].Value.(int64)), SeqEnd: uint64(a[9].Value.(int64)), EventCount: int(a[10].Value.(int64)), SizeBytes: a[11].Value.(int64), ChecksumSHA256: a[12].Value.(string), ObjectKey: a[13].Value.(string), ReceivedAt: a[14].Value.(time.Time)}
	c.entries = append(c.entries, b)
	c.entries[len(c.entries)-1].StorageDriver = "local"
	return diagnosticResult(b.ID), nil
}

type diagnosticResult int64

func (r diagnosticResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r diagnosticResult) RowsAffected() (int64, error) { return 1, nil }
func diagnosticStore(t *testing.T) (*Store, *diagnosticSQLState) {
	t.Helper()
	state := &diagnosticSQLState{tenant: 14, claim: time.Now().UTC().Truncate(time.Millisecond), cid: "socket-current-0001"}
	database := sql.OpenDB(diagnosticConnector{state})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}, state
}
func diagnosticMeta(state *diagnosticSQLState) deviceops.StoredBatch {
	b := deviceops.StoredBatch{TenantID: 14, DeviceID: 7, OwnerEpoch: deviceops.OwnerEpoch("maintenance-test-secret", 14, 7, state.claim), BatchID: strings.Repeat("a", 32), BootID: strings.Repeat("b", 32), FirmwareVersion: "2.2.7", FirmwareSHA256: strings.Repeat("f", 64), Storage: "rtc", SeqStart: 1, SeqEnd: 2, EventCount: 2, SizeBytes: 300, ChecksumSHA256: strings.Repeat("c", 64)}
	b.ObjectKey = deviceops.BatchObjectKey(deviceops.Identity{TenantID: 14, DeviceID: 7}, deviceops.Batch{OwnerEpoch: b.OwnerEpoch, BatchID: b.BatchID}, b.ChecksumSHA256)
	return b
}
func TestDeviceDiagnosticsOwnershipDedupeCommitAndQuota(t *testing.T) {
	s, state := diagnosticStore(t)
	ctx := context.Background()
	const secret = "maintenance-test-secret"
	writes := 0
	write := func() error { writes++; return nil }
	b := diagnosticMeta(state)
	if err := s.MigrateDeviceDiagnostics(ctx); err != nil || !strings.Contains(state.schema, "UNIQUE KEY uk_device_diagnostic_batch(tenant_id,device_id,owner_epoch,batch_id)") {
		t.Fatal(err, state.schema)
	}
	got, duplicate, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, write)
	if err != nil || duplicate || got.ID == 0 || writes != 1 {
		t.Fatal(got, duplicate, err, writes)
	}
	_, duplicate, err = s.RecordDeviceDiagnosticBatch(ctx, b, secret, write)
	if err != nil || !duplicate || len(state.entries) != 1 || writes != 2 {
		t.Fatal(duplicate, err, writes)
	}
	conflict := b
	conflict.ChecksumSHA256 = strings.Repeat("d", 64)
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, conflict, secret, write); !errors.Is(err, deviceops.ErrConflict) || writes != 2 {
		t.Fatal("different payload not rejected", err, writes)
	}
	state.tenant = 15
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, write); !errors.Is(err, deviceops.ErrForbidden) || writes != 2 {
		t.Fatal("old owner accepted", err, writes)
	}
	state.tenant = 14
	state.claim = state.claim.Add(time.Second)
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, write); !errors.Is(err, deviceops.ErrForbidden) {
		t.Fatal("old claim epoch accepted", err)
	}
	b = diagnosticMeta(state)
	b.BatchID = strings.Repeat("d", 32)
	state.quota = true
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, write); !errors.Is(err, deviceops.ErrQuota) || writes != 2 {
		t.Fatal("quota not enforced before object write", err, writes)
	}
	state.quota = false
	state.failCommit = true
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, write); err == nil || len(state.entries) != 1 {
		t.Fatal("failed index commit accepted", err)
	}
	state.failCommit = false
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, write); err != nil || len(state.entries) != 2 {
		t.Fatal("uncertain commit retry failed", err)
	}
}
func TestDeviceDiagnosticsCurrentCIDAndTenantReadIsolation(t *testing.T) {
	s, state := diagnosticStore(t)
	ctx := context.Background()
	const secret = "maintenance-test-secret"
	id, err := s.ResolveDeviceMaintenanceIdentity(ctx, "1c:29:04:31:0e:b8", state.cid, secret)
	if err != nil || id.TenantID != 14 {
		t.Fatal(id, err)
	}
	if _, err := s.ResolveDeviceMaintenanceIdentity(ctx, "1c:29:04:31:0e:b8", "socket-previous-0001", secret); !errors.Is(err, deviceops.ErrForbidden) {
		t.Fatal("old CID accepted", err)
	}
	b := diagnosticMeta(state)
	if _, _, err := s.RecordDeviceDiagnosticBatch(ctx, b, secret, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListDeviceDiagnosticBatches(ctx, 15, 7, 0, 50)
	if err != nil || len(rows) != 0 {
		t.Fatal("cross-tenant list", rows, err)
	}
	if _, err := s.GetDeviceDiagnosticBatch(ctx, 15, 7, b.OwnerEpoch, b.BatchID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant download", err)
	}
	rows, err = s.ListDeviceDiagnosticBatches(ctx, 14, 7, 0, 50)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}
