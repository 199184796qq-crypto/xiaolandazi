package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
)

type supportConfigDB struct {
	consent, deleted, auditFail, committed, rolledBack bool
	writes                                             []string
	args                                               [][]driver.NamedValue
}
type supportConfigConnector struct{ state *supportConfigDB }

func (c supportConfigConnector) Connect(context.Context) (driver.Conn, error) {
	return &supportConfigConn{c.state}, nil
}
func (c supportConfigConnector) Driver() driver.Driver { return supportConfigDriver{} }

type supportConfigDriver struct{}

func (supportConfigDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type supportConfigConn struct{ state *supportConfigDB }

func (c *supportConfigConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not supported")
}
func (c *supportConfigConn) Close() error              { return nil }
func (c *supportConfigConn) Begin() (driver.Tx, error) { return supportConfigTx{c.state}, nil }

type supportConfigTx struct{ state *supportConfigDB }

func (t supportConfigTx) Commit() error   { t.state.committed = true; return nil }
func (t supportConfigTx) Rollback() error { t.state.rolledBack = true; return nil }
func (c *supportConfigConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case strings.Contains(q, "SELECT id FROM core_rooms"):
		return &contentPolicyTestRows{columns: []string{"id"}, values: [][]driver.Value{{int64(12)}}}, nil
	case strings.Contains(q, "SELECT COUNT(*) FROM mgmt_room_deletions"):
		count := int64(0)
		if c.state.deleted {
			count = 1
		}
		return &contentPolicyTestRows{columns: []string{"n"}, values: [][]driver.Value{{count}}}, nil
	case strings.Contains(q, "FROM live_support_authorizations"):
		if len(args) != 4 || args[0].Value != int64(7) || args[1].Value != int64(12) || args[2].Value != int64(9) {
			return nil, errors.New("wrong customer consent scope")
		}
		rows := &contentPolicyTestRows{columns: []string{"id"}}
		if c.state.consent {
			rows.values = [][]driver.Value{{int64(1)}}
		}
		return rows, nil
	case strings.Contains(q, "FROM live_agent_config_versions v"):
		return &contentPolicyTestRows{columns: []string{"agent_id", "speech", "style"}, values: [][]driver.Value{{int64(5), `{"rooms":{"12":{"selected_voice":{"voice_id":"new"}},"13":{"selected_voice":{"voice_id":"stale-private"}}}}`, `{"rooms":{"12":{"training_entries":[]},"13":{"private":true}}}`}}}, nil
	case strings.Contains(q, "SELECT COALESCE(current_version_id"):
		return &contentPolicyTestRows{columns: []string{"version"}, values: [][]driver.Value{{int64(44)}}}, nil
	case strings.Contains(q, "SELECT COALESCE(MAX(version_no)"):
		return &contentPolicyTestRows{columns: []string{"version"}, values: [][]driver.Value{{int64(8)}}}, nil
	default:
		return nil, errors.New("unexpected query: " + q)
	}
}

type supportConfigResult struct{}

func (supportConfigResult) LastInsertId() (int64, error) { return 77, nil }
func (supportConfigResult) RowsAffected() (int64, error) { return 1, nil }
func (c *supportConfigConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.state.writes = append(c.state.writes, q)
	c.state.args = append(c.state.args, append([]driver.NamedValue(nil), args...))
	if c.state.auditFail && strings.Contains(q, "live_support_authorization_events") {
		return nil, errors.New("audit failed")
	}
	return supportConfigResult{}, nil
}

func TestSupportConfigPublishRechecksConsentAndDeletedRoom(t *testing.T) {
	for _, tc := range []struct {
		name             string
		consent, deleted bool
	}{{"revoked", false, false}, {"deleted", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			state := &supportConfigDB{consent: tc.consent, deleted: tc.deleted}
			database := sql.OpenDB(supportConfigConnector{state})
			defer database.Close()
			store := &Store{db: database}
			_, err := store.ActivateLiveSupportSpeechConfig(context.Background(), 7, 12, 10, 9)
			if err == nil || state.committed || len(state.writes) > 0 || !state.rolledBack {
				t.Fatalf("unauthorized write: %#v err=%v", state, err)
			}
		})
	}
}

func TestSupportConfigPublishCopiesOnlyAuthorizedRoomIntoCurrentVersion(t *testing.T) {
	state := &supportConfigDB{consent: true}
	database := sql.OpenDB(supportConfigConnector{state})
	defer database.Close()
	store := &Store{db: database}
	id, err := store.ActivateLiveSupportSpeechConfig(context.Background(), 7, 12, 10, 9)
	if err != nil || id != 77 || !state.committed {
		t.Fatalf("publish failed %d %v", id, err)
	}
	if len(state.writes) != 4 {
		t.Fatalf("unexpected writes: %#v", state.writes)
	}
	args := state.args[0]
	if args[3].Value != `$."12"` || args[4].Value != `{"selected_voice":{"voice_id":"new"}}` || args[10].Value != int64(44) {
		t.Fatalf("not scoped to current room/current version: %#v", args)
	}
	if strings.Contains(state.writes[1], "lifecycle_status IN") || state.args[1][1].Value != int64(44) || state.args[1][2].Value != int64(10) {
		t.Fatal("unrelated customer drafts would be archived")
	}
}

func TestSupportConfigPublishRollsBackWhenAuditFails(t *testing.T) {
	state := &supportConfigDB{consent: true, auditFail: true}
	database := sql.OpenDB(supportConfigConnector{state})
	defer database.Close()
	store := &Store{db: database}
	_, err := store.ActivateLiveSupportSpeechConfig(context.Background(), 7, 12, 10, 9)
	if err == nil || state.committed || !state.rolledBack {
		t.Fatalf("audit failure committed publication: %#v err=%v", state, err)
	}
}
