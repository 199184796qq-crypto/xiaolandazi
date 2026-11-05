package room

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

type numberFixture struct {
	number, status             string
	monitor, failCommit        bool
	updateErr                  error
	writes, commits, rollbacks int
	roomID, tenantID           int64
	url                        string
}
type numberDriver struct{ state *numberFixture }
type numberConn struct {
	state                     *numberFixture
	pendingNumber, pendingURL string
}
type numberTx struct{ conn *numberConn }
type numberRows struct {
	values []driver.Value
	done   bool
}

func (d numberDriver) Open(string) (driver.Conn, error) { return &numberConn{state: d.state}, nil }
func (*numberConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (*numberConn) Close() error                        { return nil }
func (c *numberConn) Begin() (driver.Tx, error)         { return &numberTx{c}, nil }
func (c *numberConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "FROM core_rooms") {
		return nil, fmt.Errorf("unexpected query: %s", q)
	}
	f := c.state
	r := &numberRows{}
	if len(args) != 2 || args[0].Value != f.roomID || args[1].Value != f.tenantID {
		return r, nil
	}
	r.values = []driver.Value{f.roomID, f.tenantID, "douyin", f.number, f.url, "鹅", f.status, "auto", f.monitor, nil, false, int64(0), nil, time.Now(), time.Now()}
	return r, nil
}
func (c *numberConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	f := c.state
	f.writes++
	if !strings.Contains(q, "UPDATE core_rooms SET external_room_id") || len(args) != 4 || args[2].Value != f.roomID || args[3].Value != f.tenantID {
		return nil, errors.New("unsafe identity write")
	}
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	c.pendingNumber = args[0].Value.(string)
	c.pendingURL = args[1].Value.(string)
	return driver.RowsAffected(1), nil
}
func (t *numberTx) Commit() error {
	f := t.conn.state
	if f.failCommit {
		return errors.New("commit failed")
	}
	f.commits++
	f.number = t.conn.pendingNumber
	f.url = t.conn.pendingURL
	return nil
}
func (t *numberTx) Rollback() error { t.conn.state.rollbacks++; return nil }
func (*numberRows) Columns() []string {
	return strings.Split("id,tenant,platform,external,source,name,status,collector,monitor,monitor_started,device,online,last_event,created,updated", ",")
}
func (*numberRows) Close() error { return nil }
func (r *numberRows) Next(v []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	r.done = true
	copy(v, r.values)
	return nil
}

var numberDriverSequence int

func numberStore(t *testing.T) (*Store, *numberFixture) {
	t.Helper()
	f := &numberFixture{number: "521958972384", status: "stopped", roomID: 29, tenantID: 14}
	numberDriverSequence++
	name := fmt.Sprintf("room-number-%d", numberDriverSequence)
	sql.Register(name, numberDriver{f})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	s := NewStore(db)
	t.Cleanup(func() { s.lifecycle.Close(); db.Close() })
	return s, f
}
func TestUpdateNumberPreservesRoomIdentityAndRefreshesSource(t *testing.T) {
	s, f := numberStore(t)
	item, err := s.UpdateNumber(context.Background(), 14, 29, NumberUpdate{" 889900 ", f.number})
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != 29 || item.TenantID != 14 || item.Name != "鹅" || item.MonitorEnabled || item.ExternalRoomID != "889900" || item.SourceURL != "https://live.douyin.com/889900?from=web_code_link" || f.commits != 1 {
		t.Fatal(item, f)
	}
	if state := s.lifecycle.Snapshot(item); state.Collection != "stopped" {
		t.Fatal(state)
	}
	_, err = s.UpdateNumber(context.Background(), 14, 29, NumberUpdate{"123", "521958972384"})
	if !errors.Is(err, ErrRoomNumberChanged) || f.writes != 1 {
		t.Fatal("stale write replaced number", err, f)
	}
}
func TestUpdateNumberGuardsAndRollback(t *testing.T) {
	for _, kind := range []string{"foreign_tenant", "deleted", "connected", "live", "invalid", "duplicate", "commit", "unchanged"} {
		t.Run(kind, func(t *testing.T) {
			s, f := numberStore(t)
			tenant, id := int64(14), int64(29)
			in := NumberUpdate{"888", f.number}
			var want error
			switch kind {
			case "foreign_tenant":
				tenant = 15
				want = sql.ErrNoRows
			case "deleted":
				id = 30
				want = sql.ErrNoRows
			case "connected":
				f.monitor = true
				want = ErrRoomNumberBusy
			case "live":
				f.status = "live"
				want = ErrRoomNumberBusy
			case "invalid":
				in.ExternalRoomID = "../bad"
				want = ErrRoomNumberInvalid
			case "duplicate":
				f.updateErr = &mysql.MySQLError{Number: 1062}
				want = ErrConflict
			case "commit":
				f.failCommit = true
			case "unchanged":
				in.ExternalRoomID = f.number
			}
			_, err := s.UpdateNumber(context.Background(), tenant, id, in)
			if kind == "commit" {
				if err == nil {
					t.Fatal("failed commit accepted")
				}
			} else if !errors.Is(err, want) {
				t.Fatalf("got %v want %v", err, want)
			}
			if f.number != "521958972384" || f.commits != 0 {
				t.Fatal("rejected/no-op update changed persisted identity", f)
			}
		})
	}
}
func TestValidRoomNumberBoundaries(t *testing.T) {
	for _, value := range []string{"123", "0", "Ab_-19", strings.Repeat("1", 128)} {
		if !ValidRoomNumber(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"", " ", "房间", "1/2", "a?b", "a\nb", strings.Repeat("1", 129)} {
		if ValidRoomNumber(value) {
			t.Fatal(value)
		}
	}
}
