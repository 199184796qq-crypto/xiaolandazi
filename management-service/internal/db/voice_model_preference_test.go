package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestVoiceModelPreferencePreservesIdentityAndOtherRooms(t *testing.T) {
	config := map[string]any{"target_model": "qwen-audio-3.0-tts-plus", "voice_id": "original", "sample_asset_id": float64(7), "avatar_key": "female_young", "ui_model_preferences": map[string]any{"25": "qwen-audio-3.0-tts-plus"}}
	if err := setVoiceModelPreference(config, 26, "qwen-audio-3.1-tts-flash"); err != nil {
		t.Fatal(err)
	}
	expected := map[string]any{"target_model": "qwen-audio-3.0-tts-plus", "voice_id": "original", "sample_asset_id": float64(7), "avatar_key": "female_young", "ui_model_preferences": map[string]any{"25": "qwen-audio-3.0-tts-plus", "26": "qwen-audio-3.1-tts-flash"}}
	if !reflect.DeepEqual(config, expected) {
		t.Fatalf("identity or another room overwritten: %#v", config)
	}
	if err := setVoiceModelPreference(config, 26, "qwen-audio-3.0-tts-plus"); err != nil {
		t.Fatal(err)
	}
	if config["ui_model_preferences"].(map[string]any)["25"] != "qwen-audio-3.0-tts-plus" {
		t.Fatal("other room changed")
	}
}

type voicePreferenceConnector struct{ conn *voicePreferenceConn }

func (c voicePreferenceConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c voicePreferenceConnector) Driver() driver.Driver                        { return openingAssetTestDriver{} }

type voicePreferenceConn struct {
	raw             string
	writes, commits int
}

func (*voicePreferenceConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*voicePreferenceConn) Close() error                { return nil }
func (c *voicePreferenceConn) Begin() (driver.Tx, error) { return &voicePreferenceTx{c}, nil }

type voicePreferenceTx struct{ c *voicePreferenceConn }

func (tx *voicePreferenceTx) Commit() error { tx.c.commits++; return nil }
func (*voicePreferenceTx) Rollback() error  { return nil }
func (c *voicePreferenceConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	rows := &contentPolicyTestRows{}
	if strings.Contains(q, "FROM rooms") {
		return nil, errors.New("unknown table rooms: production schema uses core_rooms")
	}
	if strings.Contains(q, "FROM core_rooms") {
		if !strings.Contains(q, "WHERE id=? AND tenant_id=?") || len(args) != 2 {
			return nil, errors.New("unscoped room")
		}
		rows.columns = []string{"id"}
		if args[0].Value == int64(26) && args[1].Value == int64(14) {
			rows.values = [][]driver.Value{{int64(26)}}
		}
		return rows, nil
	}
	if !strings.Contains(q, "WHERE id=? AND tenant_id=?") || len(args) != 2 {
		return nil, errors.New("unscoped voice")
	}
	if strings.HasPrefix(q, "SELECT CAST(config_json AS CHAR)") {
		if !strings.Contains(q, "FOR UPDATE") || !strings.Contains(q, "clone_status<>'disabled'") {
			return nil, errors.New("voice not locked/disabled filter missing")
		}
		rows.columns = []string{"config"}
		if args[0].Value == int64(2) && args[1].Value == int64(14) {
			rows.values = [][]driver.Value{{c.raw}}
		}
		return rows, nil
	}
	rows.columns = strings.Fields("id tenant agent name provider voice sample status config default creator updater created updated")
	if args[0].Value == int64(2) && args[1].Value == int64(14) {
		rows.values = [][]driver.Value{{int64(2), int64(14), nil, "海妹", "supplier", "original-plus", int64(9), "ready", c.raw, false, int64(5), int64(5), time.Now(), time.Now()}}
	}
	return rows, nil
}
func (c *voicePreferenceConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !strings.Contains(q, "WHERE id=? AND tenant_id=? AND clone_status<>'disabled'") || len(args) != 4 || args[2].Value != int64(2) || args[3].Value != int64(14) {
		return nil, errors.New("unscoped update")
	}
	set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
	for _, field := range []string{"voice_id", "provider", "sample_asset_id", "clone_status", "is_default", "name="} {
		if strings.Contains(set, field) {
			return nil, errors.New("identity overwritten")
		}
	}
	c.raw = args[0].Value.(string)
	c.writes++
	return driver.RowsAffected(1), nil
}
func TestSaveVoiceModelPreferenceScopesTransactionAndKeepsVoiceIdentity(t *testing.T) {
	conn := &voicePreferenceConn{raw: `{"target_model":"qwen-audio-3.0-tts-plus","avatar_key":"female_young"}`}
	database := sql.OpenDB(voicePreferenceConnector{conn})
	defer database.Close()
	store := &Store{db: database}
	result, err := store.SaveVoiceModelPreference(context.Background(), 14, 26, 2, 5, "qwen-audio-3.1-tts-flash")
	if err != nil || result.VoiceID != "original-plus" || result.Config["target_model"] != "qwen-audio-3.0-tts-plus" || result.Config["avatar_key"] != "female_young" {
		t.Fatal(result, err)
	}
	prefs := result.Config["ui_model_preferences"].(map[string]any)
	if prefs["26"] != "qwen-audio-3.1-tts-flash" || conn.writes != 1 || conn.commits != 1 {
		t.Fatal(prefs, conn)
	}
	for _, scope := range [][3]int64{{15, 26, 2}, {14, 25, 2}, {14, 26, 3}} {
		if _, err := store.SaveVoiceModelPreference(context.Background(), scope[0], scope[1], scope[2], 5, "qwen-audio-3.1-tts-flash"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("cross-scope write accepted", scope, err)
		}
	}
	if conn.writes != 1 || conn.commits != 1 {
		t.Fatal("rejected write committed", conn)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(conn.raw), &config); err != nil {
		t.Fatal(err)
	}
	conn.raw = `null`
	if _, err := store.SaveVoiceModelPreference(context.Background(), 14, 26, 2, 5, "qwen-audio-3.1-tts-flash"); err == nil {
		t.Fatal("null config overwritten")
	}
	if conn.writes != 1 || conn.commits != 1 {
		t.Fatal("invalid JSON committed", conn)
	}
}

func TestVoiceModelPreferenceRejectsInvalidWithoutOverwrite(t *testing.T) {
	for _, config := range []map[string]any{nil, {"ui_model_preferences": "broken"}, {"ui_model_preferences": []any{}}} {
		if err := setVoiceModelPreference(config, 26, "qwen-audio-3.1-tts-flash"); err == nil {
			t.Fatal("invalid preference accepted", config)
		}
	}
	for _, input := range []struct {
		room  int64
		model string
	}{{0, "qwen-audio-3.1-tts-flash"}, {26, "unknown"}, {26, ""}} {
		config := map[string]any{"target_model": "original"}
		if err := setVoiceModelPreference(config, input.room, input.model); err == nil {
			t.Fatal("invalid input accepted")
		}
		if !reflect.DeepEqual(config, map[string]any{"target_model": "original"}) {
			t.Fatal("invalid input overwrote config")
		}
	}
}
