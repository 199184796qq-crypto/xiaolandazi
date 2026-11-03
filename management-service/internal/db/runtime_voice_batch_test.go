package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voicebatch"
)

// SQL-boundary tests: no production connection or credentials are used.
type hostVoiceConnector struct{ conn *hostVoiceConn }

func (c hostVoiceConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (hostVoiceConnector) Driver() driver.Driver                          { return openingAssetTestDriver{} }

type hostVoiceConn struct {
	raw           string
	revision      int64
	active        bool
	reads, writes int
}

func (*hostVoiceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*hostVoiceConn) Close() error              { return nil }
func (*hostVoiceConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *hostVoiceConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.reads++
	if strings.Contains(q, "FROM live_runtime_sessions s") {
		if len(args) != 2 || args[0].Value != int64(14) || args[1].Value != int64(29) {
			return nil, errors.New("session read escaped tenant/room")
		}
		status := "running"
		if !c.active {
			status = "stopped"
		}
		now := time.Now().UTC()
		return &contentPolicyTestRows{columns: []string{"id", "external_id", "tenant_id", "room_id", "device_id", "sn", "realm", "status", "reason", "started_by", "stopped_by", "started_at", "last_billed_at", "ended_at", "billed", "version"}, values: [][]driver.Value{{int64(600), "live-600", int64(14), int64(29), nil, "", "prod", status, "", int64(28), nil, now, now, nil, int64(0), int64(1)}}}, nil
	}
	if !strings.Contains(q, "WHERE tenant_id=? AND room_id=? AND session_id=? AND version_id=?") || len(args) != 4 {
		return nil, errors.New("voice read missing exact scope")
	}
	rows := &contentPolicyTestRows{columns: []string{"state_json", "revision"}}
	if args[0].Value == int64(14) && args[1].Value == int64(29) && args[2].Value == int64(600) && args[3].Value == int64(44) && c.revision > 0 {
		rows.values = [][]driver.Value{{c.raw, c.revision}}
	}
	return rows, nil
}
func (c *hostVoiceConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.writes++
	if !strings.Contains(q, "ended_at IS NULL") || !strings.Contains(q, "status IN ('running','paused')") {
		return nil, errors.New("late write escaped active session fence")
	}
	if strings.HasPrefix(q, "INSERT IGNORE") {
		if len(args) != 8 || args[0].Value != int64(14) || args[1].Value != int64(29) || args[2].Value != int64(600) || args[3].Value != int64(44) || args[5].Value != int64(600) || args[6].Value != int64(14) || args[7].Value != int64(29) {
			return nil, errors.New("insert escaped scoped session")
		}
		if c.revision > 0 || !c.active {
			return driver.RowsAffected(0), nil
		}
		c.raw = args[4].Value.(string)
		c.revision = 1
		return driver.RowsAffected(1), nil
	}
	if !strings.Contains(q, "version_id=? AND revision=?") || !strings.Contains(q, "r.id=b.session_id AND r.tenant_id=b.tenant_id AND r.room_id=b.room_id") || len(args) != 6 {
		return nil, errors.New("update lacks compare-and-swap or owner fence")
	}
	if !c.active || args[1].Value != int64(14) || args[2].Value != int64(29) || args[3].Value != int64(600) || args[4].Value != int64(44) || args[5].Value != c.revision {
		return driver.RowsAffected(0), nil
	}
	c.raw = args[0].Value.(string)
	c.revision++
	return driver.RowsAffected(1), nil
}

func TestHostStateCASPreservesManualCommandAndSessionScope(t *testing.T) {
	c := &hostVoiceConn{active: true}
	database := sql.OpenDB(hostVoiceConnector{c})
	defer database.Close()
	s := &Store{db: database}
	ctx := t.Context()
	initial := voicebatch.State{Tenant: 14, Room: 29, Session: 600, Version: 44, Active: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "m"}, Batch: 1}
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, initial); err != nil || !ok {
		t.Fatal(ok, err)
	}
	stale, err := s.RuntimeVoiceBatch(ctx, 14, 29, 600, 44)
	if err != nil {
		t.Fatal(err)
	}
	manual := stale
	b := model.LiveAgentVoiceIdentity{VoiceID: "B", Model: "m"}
	manual.Requested = &b
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, manual); err != nil || !ok {
		t.Fatal(ok, err)
	}
	stale.ElapsedMS = 4000
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, stale); err != nil || ok {
		t.Fatal("stale observer erased command", ok, err)
	}
	latest, err := s.RuntimeVoiceBatch(ctx, 14, 29, 600, 44)
	if err != nil || latest.Requested == nil || latest.Requested.VoiceID != "B" || latest.Revision != 2 {
		t.Fatal(latest, err)
	}
	for _, scope := range [][4]int64{{15, 29, 600, 44}, {14, 30, 600, 44}, {14, 29, 601, 44}, {14, 29, 600, 45}} {
		if _, err := s.RuntimeVoiceBatch(ctx, scope[0], scope[1], scope[2], scope[3]); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("cross-scope state read", scope, err)
		}
	}
	c.active = false
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, latest); err != nil || ok {
		t.Fatal("stopped session accepted late voice write", ok, err)
	}
}

func TestRuntimeVoiceIdentityUsesAudibleHostAndBlocksPending(t *testing.T) {
	c := &hostVoiceConn{active: true, revision: 3}
	database := sql.OpenDB(hostVoiceConnector{c})
	defer database.Close()
	s := &Store{db: database}
	state := voicebatch.State{Active: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "m"}}
	set := func() { raw, _ := json.Marshal(state); c.raw = string(raw) }
	set()
	if voice, err := s.RuntimeVoiceIdentity(t.Context(), 14, 29, 44); err != nil || voice.VoiceID != "A" {
		t.Fatal(voice, err)
	}
	b := model.LiveAgentVoiceIdentity{VoiceID: "B", Model: "m"}
	state.Requested = &b
	set()
	if voice, err := s.RuntimeVoiceIdentity(t.Context(), 14, 29, 44); err != nil || voice.VoiceID != "A" {
		t.Fatal("unready requested voice leaked into interaction", voice, err)
	}
	state.Pending = &b
	state.PendingInstance = "next"
	set()
	if _, err := s.RuntimeVoiceIdentity(t.Context(), 14, 29, 44); !errors.Is(err, voicebatch.ErrSwitching) {
		t.Fatal(err)
	}
	state.Started("next", time.Now())
	set()
	if voice, err := s.RuntimeVoiceIdentity(t.Context(), 14, 29, 44); err != nil || voice.VoiceID != "B" {
		t.Fatal(voice, err)
	}
	c.active = false
	if voice, err := s.RuntimeVoiceIdentity(t.Context(), 14, 29, 44); err != nil || voice.VoiceID != "" {
		t.Fatal("ended host was reused", voice, err)
	}
}
