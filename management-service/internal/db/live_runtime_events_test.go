package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

type runtimeEventConnector struct{ conn *runtimeEventConn }

func (c runtimeEventConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (runtimeEventConnector) Driver() driver.Driver                          { return openingAssetTestDriver{} }

type runtimeEventConn struct {
	args []driver.NamedValue
	err  error
}

func (*runtimeEventConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*runtimeEventConn) Close() error              { return nil }
func (*runtimeEventConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected transaction") }
func (c *runtimeEventConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(query, "INSERT INTO live_runtime_events") || strings.Contains(query, "UPDATE") {
		return nil, errors.New("operation log must not mutate runtime or plan selection")
	}
	c.args = args
	if c.err != nil {
		return nil, c.err
	}
	return cardCopyResult{id: 123}, nil
}

func runtimeEventStore(t *testing.T, conn *runtimeEventConn) *Store {
	t.Helper()
	database := sql.OpenDB(runtimeEventConnector{conn})
	t.Cleanup(func() { database.Close() })
	return &Store{db: database}
}

func TestRecordLiveRuntimeEventAcceptsEveryCurrentRoomAction(t *testing.T) {
	// Exercise the same writes used by the shipped room page. Adding a new UI
	// action without updating the API contract must fail this regression.
	_, file, _, _ := runtime.Caller(0)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../web-console/src/views/RoomDetailView.vue"))
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`logUserAction\('([A-Z_]+)'`).FindAllSubmatch(source, -1)
	if len(matches) < 9 {
		t.Fatalf("no room actions found: %d", len(matches))
	}
	codes := []string{"QUESTION_ADDED", "QUESTIONS_MENTIONED", "AGENT_DRAWER_OPENED", "DEVICE_SIM_STARTED", "DEVICE_SIM_PAUSED", "DEVICE_SIM_CLOSED"}
	for _, match := range matches {
		codes = append(codes, string(match[1]))
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			conn := &runtimeEventConn{}
			store := runtimeEventStore(t, conn)
			detail := map[string]any{"plan_id": float64(5), "plan_name": "巴国布衣本地生活"}
			event, err := store.RecordLiveRuntimeEvent(context.Background(), 14, 26, 28, model.RecordLiveRuntimeEventInput{EventCode: " " + strings.ToLower(code) + " ", Detail: detail})
			if err != nil {
				t.Fatal(err)
			}
			if event.ID != 123 || event.EventCode != code || event.Title == "" || event.ActorType != "user" || event.ActorUserID == nil || *event.ActorUserID != 28 || event.TenantID != 14 || event.RoomID == nil || *event.RoomID != 26 {
				t.Fatalf("incorrect event identity: %#v", event)
			}
			if len(conn.args) != 7 || conn.args[0].Value != int64(14) || conn.args[1].Value != int64(26) || conn.args[2].Value != int64(28) || conn.args[3].Value != code || conn.args[4].Value != event.Title {
				t.Fatalf("incorrect persisted scope: %#v", conn.args)
			}
			var saved map[string]any
			if err := json.Unmarshal(conn.args[5].Value.([]byte), &saved); err != nil || saved["plan_id"] != detail["plan_id"] || saved["plan_name"] != detail["plan_name"] {
				t.Fatalf("lost plan details: %#v, %v", saved, err)
			}
		})
	}
}

func TestRecordLiveRuntimeEventRejectsUntrustedLifecycleEvents(t *testing.T) {
	for _, code := range []string{"", "UNKNOWN", "AI_STOPPED", "LIVE_STARTED", "RUNTIME_STOPPED"} {
		conn := &runtimeEventConn{}
		_, err := runtimeEventStore(t, conn).RecordLiveRuntimeEvent(context.Background(), 14, 26, 28, model.RecordLiveRuntimeEventInput{EventCode: code})
		if !errors.Is(err, ErrUnsupportedLiveRuntimeEvent) || conn.args != nil {
			t.Fatalf("untrusted event %q reached database: %v", code, err)
		}
	}
}

func TestRecordLiveRuntimeEventPreservesDatabaseFailure(t *testing.T) {
	want := errors.New("database unavailable")
	conn := &runtimeEventConn{err: want}
	_, err := runtimeEventStore(t, conn).RecordLiveRuntimeEvent(context.Background(), 14, 26, 28, model.RecordLiveRuntimeEventInput{EventCode: "LIVE_AGENT_PLAN_SELECTED"})
	if !errors.Is(err, want) || errors.Is(err, ErrUnsupportedLiveRuntimeEvent) {
		t.Fatal(err)
	}
}
