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

type contentPolicyTestDB struct {
	grant                 bool
	mode                  string
	consent               bool
	revision              int64
	auditFail             bool
	writes                []string
	writeArgs             [][]driver.NamedValue
	committed, rolledBack bool
}
type contentPolicyTestConnector struct{ state *contentPolicyTestDB }

func (c contentPolicyTestConnector) Connect(context.Context) (driver.Conn, error) {
	return &contentPolicyTestConn{c.state}, nil
}
func (c contentPolicyTestConnector) Driver() driver.Driver { return contentPolicyTestDriver{} }

type contentPolicyTestDriver struct{}

func (contentPolicyTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use connector")
}

type contentPolicyTestConn struct{ state *contentPolicyTestDB }

func (c *contentPolicyTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("no prepared statements")
}
func (c *contentPolicyTestConn) Close() error              { return nil }
func (c *contentPolicyTestConn) Begin() (driver.Tx, error) { return contentPolicyTestTx{c.state}, nil }
func (c *contentPolicyTestConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if len(args) < 2 {
		return nil, errors.New("missing tenant/room scope")
	}
	for i, want := range []int64{7, 11} {
		if args[i].Value != want {
			return nil, errors.New("incorrect tenant/room scope")
		}
	}
	switch {
	case strings.Contains(q, "SELECT id FROM core_rooms") && strings.HasSuffix(q, "FOR UPDATE"):
		return &contentPolicyTestRows{columns: []string{"id"}, values: [][]driver.Value{{int64(11)}}}, nil
	case strings.Contains(q, "SELECT COUNT(*) FROM mgmt_room_deletions"):
		return &contentPolicyTestRows{columns: []string{"count"}, values: [][]driver.Value{{int64(0)}}}, nil
	case strings.Contains(q, "FROM live_support_authorizations"):
		if len(args) != 3 || args[2].Value != int64(5) {
			return nil, errors.New("incorrect staff consent scope")
		}
		rows := &contentPolicyTestRows{columns: []string{"id"}}
		if c.state.consent {
			rows.values = [][]driver.Value{{int64(9)}}
		}
		return rows, nil
	case strings.Contains(q, "SELECT policy_json,revision"):
		rows := &contentPolicyTestRows{columns: []string{"policy_json", "revision"}}
		if c.state.revision > 0 {
			rows.values = [][]driver.Value{{"{}", c.state.revision}}
		}
		return rows, nil
	case strings.Contains(q, "SELECT dynamic_authorized"):
		return &contentPolicyTestRows{columns: []string{"dynamic_authorized"}, values: [][]driver.Value{{c.state.grant}}}, nil
	case strings.Contains(q, "SELECT selected_mode,dynamic_authorized"):
		mode := c.state.mode
		if mode == "" {
			mode = model.LiveContentAIPregenerated
		}
		return &contentPolicyTestRows{columns: []string{"selected_mode", "dynamic_authorized"}, values: [][]driver.Value{{mode, c.state.grant}}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}
func (c *contentPolicyTestConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.state.writes = append(c.state.writes, q)
	c.state.writeArgs = append(c.state.writeArgs, append([]driver.NamedValue(nil), args...))
	if c.state.auditFail && strings.Contains(q, "live_support_authorization_events") {
		return nil, errors.New("audit unavailable")
	}
	return driver.RowsAffected(1), nil
}

type contentPolicyTestTx struct{ state *contentPolicyTestDB }

func (t contentPolicyTestTx) Commit() error   { t.state.committed = true; return nil }
func (t contentPolicyTestTx) Rollback() error { t.state.rolledBack = true; return nil }

type contentPolicyTestRows struct {
	columns []string
	values  [][]driver.Value
}

func (r *contentPolicyTestRows) Columns() []string { return r.columns }
func (r *contentPolicyTestRows) Close() error      { return nil }
func (r *contentPolicyTestRows) Next(dest []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(dest, r.values[0])
	r.values = r.values[1:]
	return nil
}
func contentPolicyStore(t *testing.T, state *contentPolicyTestDB) *Store {
	t.Helper()
	database := sql.OpenDB(contentPolicyTestConnector{state})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}
}

func TestContentModeRejectsAdvancedWithoutGrant(t *testing.T) {
	state := &contentPolicyTestDB{}
	err := contentPolicyStore(t, state).SaveLiveContentMode(context.Background(), 7, 11, 5, model.LiveContentAIDynamic, false)
	if !errors.Is(err, ErrContentModeNotAuthorized) || len(state.writes) > 0 || state.committed || !state.rolledBack {
		t.Fatalf("denied mode changed storage: %+v err=%v", state, err)
	}
}

func TestContentModeSelectionDoesNotGrantFeatures(t *testing.T) {
	for _, mode := range []string{model.LiveContentAIPregenerated, model.LiveContentUserAudio, model.LiveContentAIDynamic} {
		t.Run(mode, func(t *testing.T) {
			state := &contentPolicyTestDB{grant: true}
			if err := contentPolicyStore(t, state).SaveLiveContentMode(context.Background(), 7, 11, 5, mode, false); err != nil {
				t.Fatal(err)
			}
			if !state.committed || len(state.writes) != 2 {
				t.Fatalf("mode/audit not atomic: %+v", state)
			}
			if strings.Contains(state.writes[0], "dynamic_authorized=") || strings.Contains(state.writes[0], "authorization_source=") {
				t.Fatal("customer can modify entitlement")
			}
		})
	}
}

func TestContentPolicyRechecksConsentAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state contentPolicyTestDB
		want  error
	}{
		{"revoked after handler", contentPolicyTestDB{consent: false}, ErrContentPolicyConsent},
		{"stale editor", contentPolicyTestDB{consent: true, revision: 2}, ErrContentPolicyConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := contentPolicyStore(t, &tc.state).SaveLiveContentPolicy(context.Background(), 7, 11, 5, 0, model.DefaultLiveContentPolicy(), false, nil, model.LiveContentAIPregenerated)
			if !errors.Is(err, tc.want) || len(tc.state.writes) > 0 || !tc.state.rolledBack {
				t.Fatalf("unsafe save: %+v err=%v", tc.state, err)
			}
		})
	}
}

func TestContentModeAuditFailureRollsBack(t *testing.T) {
	state := &contentPolicyTestDB{auditFail: true}
	err := contentPolicyStore(t, state).SaveLiveContentMode(context.Background(), 7, 11, 5, model.LiveContentUserAudio, false)
	if err == nil || state.committed || !state.rolledBack {
		t.Fatal("mode saved without journal")
	}
}

func TestLegacyContentModeOnlyInfersRegisteredRecordings(t *testing.T) {
	uploaded := model.MediaAsset{Metadata: map[string]any{"purpose": "live_agent_custom_mainline_audio"}}
	generated := model.MediaAsset{Metadata: map[string]any{"purpose": "live_agent_full_show_audio"}}
	for _, tc := range []struct {
		name   string
		assets []model.MediaAsset
		want   string
	}{
		{"no prior audio", nil, model.LiveContentAIPregenerated},
		{"legacy original recording", []model.MediaAsset{uploaded, uploaded}, model.LiveContentUserAudio},
		{"generated audio", []model.MediaAsset{generated}, model.LiveContentAIPregenerated},
		{"mixed is not original recording", []model.MediaAsset{uploaded, generated}, model.LiveContentAIPregenerated},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := legacyLiveContentModeFromAssets(tc.assets, model.LiveContentAIPregenerated); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestContentPolicyGrantAndRevokeAreAudited(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "grant", false: "revoke"}[allowed], func(t *testing.T) {
			state := &contentPolicyTestDB{consent: true, grant: !allowed}
			p := model.DefaultLiveContentPolicy()
			p.ContentMode = model.LiveContentAIDynamic
			if err := contentPolicyStore(t, state).SaveLiveContentPolicy(context.Background(), 7, 11, 5, 0, p, false, &allowed, model.LiveContentAIPregenerated); err != nil {
				t.Fatal(err)
			}
			if !state.committed || len(state.writes) != 3 || !strings.Contains(state.writes[2], "live_support_authorization_events") {
				t.Fatalf("entitlement not saved atomically with audit: %+v", state)
			}
			args := state.writeArgs[0]
			wantMode := model.LiveContentAIPregenerated
			if allowed {
				wantMode = model.LiveContentAIDynamic
			}
			if args[2].Value != wantMode || args[3].Value != allowed {
				t.Fatalf("unsafe grant/revoke payload: %+v", args)
			}
		})
	}
}

func TestContentPolicyCannotGrantGlobalDynamicMode(t *testing.T) {
	state := &contentPolicyTestDB{}
	p := model.DefaultLiveContentPolicy()
	p.ContentMode = model.LiveContentAIDynamic
	err := contentPolicyStore(t, state).SaveLiveContentPolicy(context.Background(), 0, 0, 5, 0, p, false, nil, model.LiveContentAIPregenerated)
	if !errors.Is(err, ErrContentModeNotAuthorized) || state.committed || len(state.writes) != 0 {
		t.Fatalf("global default granted advanced mode: %+v err=%v", state, err)
	}
}

func TestContentPolicyDoesNotOverwriteNewCustomerMode(t *testing.T) {
	state := &contentPolicyTestDB{consent: true, mode: model.LiveContentUserAudio}
	err := contentPolicyStore(t, state).SaveLiveContentPolicy(context.Background(), 7, 11, 5, 0, model.DefaultLiveContentPolicy(), false, nil, model.LiveContentAIPregenerated)
	if !errors.Is(err, ErrContentPolicyConflict) || state.committed || len(state.writes) > 0 || !state.rolledBack {
		t.Fatalf("stale operator form overwrote customer mode: %+v err=%v", state, err)
	}
}

func TestStaffContentModeSaveRechecksExactRoomConsent(t *testing.T) {
	for _, consent := range []bool{false, true} {
		state := &contentPolicyTestDB{consent: consent}
		err := contentPolicyStore(t, state).SaveLiveContentMode(context.Background(), 7, 11, 5, model.LiveContentUserAudio, true)
		if !consent {
			if !errors.Is(err, ErrContentPolicyConsent) || len(state.writes) != 0 || state.committed || !state.rolledBack {
				t.Fatalf("revoked consent allowed a mode update: %+v %v", state, err)
			}
		} else if err != nil || !state.committed {
			t.Fatalf("valid consent update failed: %+v %v", state, err)
		}
	}
}
