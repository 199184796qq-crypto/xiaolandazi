package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

type planSaveConnector struct{ c *planSaveConn }

func (c planSaveConnector) Connect(context.Context) (driver.Conn, error) { return c.c, nil }
func (c planSaveConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type planSaveConn struct {
	active, failPointer, committed, rolled, locked bool
	writes                                         []string
	deleted                                        []int64
	retention                                      []driver.Value
}

func (*planSaveConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*planSaveConn) Close() error                { return nil }
func (c *planSaveConn) Begin() (driver.Tx, error) { return planSaveTx{c}, nil }

type planSaveTx struct{ c *planSaveConn }

func (t planSaveTx) Commit() error   { t.c.committed = true; return nil }
func (t planSaveTx) Rollback() error { t.c.rolled = true; return nil }
func (c *planSaveConn) QueryContext(_ context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "tenant_id=?") {
		return nil, errors.New("tenant scope missing")
	}
	if strings.Contains(q, "SELECT COUNT(*)") {
		v := int64(1)
		if strings.Contains(q, "live_runtime_sessions") {
			v = 0
			if c.active {
				v = 1
			}
		}
		return &contentPolicyTestRows{columns: []string{"count"}, values: [][]driver.Value{{v}}}, nil
	}
	if strings.Contains(q, "SELECT id,voice_rate FROM live_agent_plans") {
		c.locked = strings.Contains(q, "FOR UPDATE")
		return &contentPolicyTestRows{columns: []string{"id", "voice_rate"}, values: [][]driver.Value{{int64(4), nil}}}, nil
	}
	if strings.Contains(q, "SELECT voice_rate FROM live_agent_plans") {
		return &contentPolicyTestRows{columns: []string{"voice_rate"}, values: [][]driver.Value{{nil}}}, nil
	}
	if strings.Contains(q, "SELECT CAST(voice_identity_json AS CHAR)") {
		return &contentPolicyTestRows{columns: []string{"voice"}, values: [][]driver.Value{{`{"name":"原始音色","voice_id":"original-voice"}`}}}, nil
	}
	if strings.Contains(q, "SELECT id FROM live_agent_plans") {
		c.locked = strings.Contains(q, "FOR UPDATE")
		return &contentPolicyTestRows{columns: []string{"id"}, values: [][]driver.Value{{int64(4)}}}, nil
	}
	if strings.Contains(q, "MAX(version_no)") {
		if !c.locked {
			return nil, errors.New("allocation not locked")
		}
		return &contentPolicyTestRows{columns: []string{"next"}, values: [][]driver.Value{{int64(8)}}}, nil
	}
	if strings.Contains(q, "SELECT id,COALESCE") {
		if strings.Contains(q, "room_id=?") || !strings.Contains(q, "plan_id=?") || !strings.Contains(q, "FOR UPDATE") {
			return nil, errors.New("unscoped pruning")
		}
		return &contentPolicyTestRows{columns: []string{"id", "mode"}, values: [][]driver.Value{{int64(48), "ai_dynamic"}, {int64(47), "ai_dynamic"}, {int64(46), "ai_dynamic"}, {int64(45), "ai_dynamic"}, {int64(44), "ai_dynamic"}, {int64(43), "user_audio"}, {int64(42), "ai_dynamic"}}}, nil
	}
	if strings.Contains(q, "selection.tenant_id=?") {
		if strings.Contains(q, "published_at IS NOT NULL") || strings.Contains(q, "publication.version_id=") || !strings.Contains(q, "ORDER BY live_agent_plan_versions.id DESC") || strings.Contains(q, "selection.room_id=live_agent_plan_versions.room_id") || !strings.Contains(q, "access.selected_mode") {
			return nil, errors.New("not latest scoped saved version")
		}
	}
	now := time.Now().UTC()
	return &contentPolicyTestRows{columns: strings.Fields("id tenant plan room version status duration round voice variants context creator publisher published created updated"), values: [][]driver.Value{{int64(48), int64(14), int64(4), int64(26), int64(8), "published", int64(60), int64(6), `{"name":"原始音色","voice_id":"original-voice"}`, `[]`, `{"content_mode":"ai_dynamic"}`, int64(8), int64(8), now, now, now}}}, nil
}
func (c *planSaveConn) ExecContext(_ context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if strings.Count(q, "?") != len(a) {
		return nil, errors.New("placeholder mismatch")
	}
	c.writes = append(c.writes, q)
	if strings.Contains(q, "INSERT INTO live_agent_room_plan_publications") {
		if c.failPointer {
			return nil, errors.New("injected pointer failure")
		}
		if !strings.Contains(q, "selection.plan_id=binding.plan_id") {
			return nil, errors.New("saving non-selected plan switched room")
		}
	}
	if strings.Contains(q, "DELETE FROM") {
		if !strings.Contains(q, "plan_id=? AND id=?") || !strings.Contains(q, "live_content_refresh_jobs") || !strings.Contains(q, "live_agent_room_plan_publications") {
			return nil, errors.New("unsafe deletion")
		}
		c.deleted = append(c.deleted, a[2].Value.(int64))
	}
	return cardCopyResult{id: 48}, nil
}
func planSaveStore(t *testing.T, c *planSaveConn) *Store {
	t.Helper()
	database := sql.OpenDB(planSaveConnector{c})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}
}
func TestSavePlanAtomicallyBecomesEffectiveAndRetainsThreeOldSnapshots(t *testing.T) {
	c := &planSaveConn{}
	s := planSaveStore(t, c)
	v, err := s.CreateLiveAgentPlanVersion(context.Background(), 14, 4, 8, model.CreateLiveAgentPlanVersionInput{RoomID: 26, DurationMinutes: 60, RoundMinutes: 6, GenerationContext: map[string]any{"content_mode": "ai_dynamic"}})
	if err != nil || !c.committed || c.rolled || v.LifecycleStatus != "published" {
		t.Fatal(v, err, c)
	}
	if len(c.deleted) != 2 || c.deleted[0] != 44 || c.deleted[1] != 42 {
		t.Fatal("current plus three old / other mode preservation failed", c.deleted)
	}
}
func TestSavePlanPointerFailureRollsBackEntireSave(t *testing.T) {
	c := &planSaveConn{failPointer: true}
	s := planSaveStore(t, c)
	_, err := s.CreateLiveAgentPlanVersion(context.Background(), 14, 4, 8, model.CreateLiveAgentPlanVersionInput{RoomID: 26})
	if err == nil || c.committed || !c.rolled || len(c.deleted) != 0 {
		t.Fatal("failed activation did not rollback", err, c)
	}
}
func TestSaveDuringLiveDoesNotDeletePinnedVersion(t *testing.T) {
	c := &planSaveConn{active: true}
	s := planSaveStore(t, c)
	_, err := s.CreateLiveAgentPlanVersion(context.Background(), 14, 4, 8, model.CreateLiveAgentPlanVersionInput{RoomID: 26})
	if err != nil || !c.committed || len(c.deleted) != 0 {
		t.Fatal(err, c)
	}
}
func TestEffectivePlanUsesLatestSavedSnapshotWithoutPublication(t *testing.T) {
	c := &planSaveConn{}
	s := planSaveStore(t, c)
	v, err := s.GetPublishedLiveAgentPlanVersionForRoom(context.Background(), 14, 26)
	if err != nil || v.ID != 48 || v.VoiceIdentity.VoiceID != "original-voice" {
		t.Fatal(v, err)
	}
}
