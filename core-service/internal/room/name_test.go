package room

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type nameFixture struct {
	mu                             sync.Mutex
	name, status                   string
	monitor, failCommit, failWrite bool
	writes, commits                int
}
type nameDriver struct{ f *nameFixture }
type nameConn struct {
	f       *nameFixture
	pending *string
}
type nameTx struct{ c *nameConn }
type nameRows struct {
	columns []string
	values  []driver.Value
	done    bool
}

func (d nameDriver) Open(string) (driver.Conn, error) { return &nameConn{f: d.f}, nil }
func (*nameConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unexpected prepare") }
func (*nameConn) Close() error                        { return nil }
func (c *nameConn) Begin() (driver.Tx, error)         { c.pending = nil; return &nameTx{c}, nil }
func (c *nameConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	r := &nameRows{columns: strings.Split("id,tenant,platform,external,source,name,status,collector,monitor,started,device,online,last_event,created,updated", ",")}
	if strings.HasPrefix(q, "SELECT name ") {
		r.columns = []string{"name"}
	}
	if len(a) != 2 || a[0].Value != int64(29) || a[1].Value != int64(14) {
		return r, nil
	}
	if len(r.columns) == 1 {
		r.values = []driver.Value{c.f.name}
	} else {
		r.values = []driver.Value{int64(29), int64(14), "douyin", "521958972384", "https://live.douyin.com/521958972384", c.f.name, c.f.status, "auto", c.f.monitor, nil, true, int64(7), nil, time.Now(), time.Now()}
	}
	return r, nil
}
func (c *nameConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if q != `UPDATE core_rooms SET name = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND tenant_id = ?` || len(a) != 3 || a[1].Value != int64(29) || a[2].Value != int64(14) {
		return nil, errors.New("unsafe non-name write")
	}
	if c.f.failWrite {
		return nil, errors.New("write failed")
	}
	c.f.writes++
	name := a[0].Value.(string)
	c.pending = &name
	return driver.RowsAffected(1), nil
}
func (t *nameTx) Commit() error {
	t.c.f.mu.Lock()
	defer t.c.f.mu.Unlock()
	if t.c.f.failCommit {
		return errors.New("commit failed")
	}
	if t.c.pending != nil {
		t.c.f.name = *t.c.pending
		t.c.f.commits++
	}
	return nil
}
func (t *nameTx) Rollback() error     { t.c.pending = nil; return nil }
func (r *nameRows) Columns() []string { return r.columns }
func (*nameRows) Close() error        { return nil }
func (r *nameRows) Next(v []driver.Value) error {
	if r.done || r.values == nil {
		return io.EOF
	}
	r.done = true
	copy(v, r.values)
	return nil
}

var nameFixtureSequence int

func nameStore(t *testing.T) (*Store, *nameFixture) {
	t.Helper()
	f := &nameFixture{name: "鹅", status: "live", monitor: true}
	nameFixtureSequence++
	label := fmt.Sprintf("room-name-%d", nameFixtureSequence)
	sql.Register(label, nameDriver{f})
	db, err := sql.Open(label, "")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(db)
	t.Cleanup(func() { s.lifecycle.Close(); db.Close() })
	return s, f
}
func textPointer(value string) *string { return &value }

func TestUpdateNameWhileLivePreservesCollectorAndIdentity(t *testing.T) {
	s, f := nameStore(t)
	item, err := s.Get(context.Background(), textID(14), 29)
	if err != nil {
		t.Fatal(err)
	}
	current := s.lifecycle.entry(item)
	current.generation = 9
	current.endRevision = 3
	current.owned = true
	current.collection = "live"
	current.timer = time.AfterFunc(time.Hour, func() {})
	timer := current.timer
	name, err := s.UpdateName(context.Background(), 14, 29, NameUpdate{" 新名字🦆 ", textPointer("鹅")})
	if err != nil || name != "新名字🦆" || f.commits != 1 {
		t.Fatal(name, err, f)
	}
	if current.generation != 9 || current.endRevision != 3 || !current.owned || !current.enabled || current.collection != "live" || current.status != "live" || current.timer != timer {
		t.Fatal("rename disturbed collector")
	}
	actual, err := s.Get(context.Background(), textID(14), 29)
	if err != nil || actual.Name != name || actual.ExternalRoomID != item.ExternalRoomID || actual.SourceURL != item.SourceURL || actual.MonitorEnabled != item.MonitorEnabled || actual.OnlineCount != 7 {
		t.Fatal(actual, err)
	}
}
func textID(n int64) *int64 { return &n }
func TestUpdateNameValidationCASAndFailures(t *testing.T) {
	for _, kind := range []string{"blank", "too_long", "control", "missing_expected", "wrong_tenant", "missing_room", "stale", "write_failure", "commit_failure", "noop", "empty_original", "unicode_limit"} {
		t.Run(kind, func(t *testing.T) {
			s, f := nameStore(t)
			in := NameUpdate{"新名字", textPointer("鹅")}
			tenant, id := int64(14), int64(29)
			wantErr := true
			switch kind {
			case "blank":
				in.Name = "  "
			case "too_long":
				in.Name = strings.Repeat("名", 129)
			case "control":
				in.Name = "名\n字"
			case "missing_expected":
				in.ExpectedName = nil
			case "wrong_tenant":
				tenant = 15
			case "missing_room":
				id = 999
			case "stale":
				in.ExpectedName = textPointer("旧名字")
			case "write_failure":
				f.failWrite = true
			case "commit_failure":
				f.failCommit = true
			case "noop":
				in.Name = "鹅"
				wantErr = false
			case "empty_original":
				f.name = ""
				in.ExpectedName = textPointer("")
				wantErr = false
			case "unicode_limit":
				in.Name = strings.Repeat("鸭", 128)
				wantErr = false
			}
			before := f.name
			_, err := s.UpdateName(context.Background(), tenant, id, in)
			if (err != nil) != wantErr {
				t.Fatal(kind, err)
			}
			if wantErr && (f.name != before || f.commits != 0) {
				t.Fatal("failure committed", f)
			}
			if kind == "noop" && f.writes != 0 {
				t.Fatal("noop wrote")
			}
			if kind == "stale" && !errors.Is(err, ErrRoomNameChanged) {
				t.Fatal(err)
			}
		})
	}
}
func TestUpdateNameConcurrentSameOriginalHasOneWinner(t *testing.T) {
	s, f := nameStore(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := s.UpdateName(context.Background(), 14, 29, NameUpdate{fmt.Sprint("名字", n), textPointer("鹅")})
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if !errors.Is(err, ErrRoomNameChanged) {
				t.Errorf("unexpected: %v", err)
			}
		}(n)
	}
	wg.Wait()
	if success != 1 || f.commits != 1 {
		t.Fatal(success, f.commits)
	}
}
