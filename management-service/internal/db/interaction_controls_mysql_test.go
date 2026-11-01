package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"livecompanion/management/internal/model"
)

func TestRoomInteractionControlsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	names := []string{"live_room_interaction_preferences", "live_room_interaction_controls", "live_room_interaction_control_history", "live_room_interaction_config_versions", "live_agent_plans", "live_agent_room_plan_selections"}
	for _, name := range names {
		found := false
		for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
			if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS "+name+" (") {
				exec(strings.TrimSpace(statement))
				found = true
			}
		}
		if !found {
			t.Fatal("missing production DDL", name)
		}
	}
	exec(`INSERT INTO core_rooms(id,tenant_id) VALUES(29,14),(30,14),(31,15)`)
	item, err := s.GetRoomInteractionPreferences(ctx, 14, 29)
	if err != nil {
		t.Fatal(err)
	}
	if item.InteractionActive != nil || item.ControlsRevision != 0 {
		t.Fatal("legacy defaults changed")
	}
	on := true
	revision := uint64(0)
	in := model.RoomInteractionPreferencesInput{OverallInteraction: 50, QuestionPreference: 50, WelcomePreference: 50, EngagementPreference: 50, ChatPreference: 50, ConversionPreference: 50, AutoHeat: true, InteractionActive: &on, Switches: map[string]bool{"welcome": false}, Cooldowns: map[string]int{"like": 120, "welcome": 180}, ControlsRevision: &revision}
	saved, err := s.UpsertRoomInteractionPreferences(ctx, 14, 29, 7, in)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ControlsRevision != 1 || saved.Switches["welcome"] || saved.Cooldowns["like"] != 120 || saved.ControlsUpdatedByUserID == nil || *saved.ControlsUpdatedByUserID != 7 {
		t.Fatal("save/read differs", saved)
	}
	restarted := &Store{db: s.db}
	restored, err := restarted.GetRoomInteractionControls(ctx, 14, 29)
	if err != nil || restored.Enabled("welcome") || !restored.Enabled("follow") {
		t.Fatal("restart restore", restored, err)
	}
	oldClient := in
	oldClient.InteractionActive = nil
	oldClient.Switches = nil
	oldClient.Cooldowns = nil
	oldClient.ControlsRevision = nil
	oldClient.QuestionPreference = 75
	saved, err = s.UpsertRoomInteractionPreferences(ctx, 14, 29, 8, oldClient)
	if err != nil || saved.ControlsRevision != 1 || saved.Cooldowns["like"] != 120 || saved.Switches["welcome"] {
		t.Fatal("legacy save reset controls", saved, err)
	}
	// Two callers editing the same version must not silently overwrite each other.
	revision = 1
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, cool := range []int{150, 180} {
		wg.Add(1)
		go func(c int) {
			defer wg.Done()
			next := in
			next.ControlsRevision = &revision
			next.Cooldowns = map[string]int{"like": c}
			_, err := s.UpsertRoomInteractionPreferences(ctx, 14, 29, 9, next)
			results <- err
		}(cool)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrInteractionControlsConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal("CAS lost update", success, conflicts)
	}
	controls, err := s.GetRoomInteractionControls(ctx, 14, 29)
	if err != nil {
		t.Fatal(err)
	}
	revision = controls.Revision
	off := false
	in.InteractionActive = &off
	in.ControlsRevision = &revision
	in.Switches = map[string]bool{}
	in.Cooldowns = map[string]int{}
	saved, err = s.UpsertRoomInteractionPreferences(ctx, 14, 29, 10, in)
	if err != nil {
		t.Fatal(err)
	}
	revision = saved.ControlsRevision
	in.InteractionActive = &on
	saved, err = s.UpsertRoomInteractionPreferences(ctx, 14, 29, 11, in)
	if err != nil {
		t.Fatal(err)
	}
	controls, err = s.GetRoomInteractionControls(ctx, 14, 29)
	if err != nil {
		t.Fatal(err)
	}
	if !controls.Enabled("welcome") || controls.DisabledAt["welcome"] == 0 || controls.DisabledAt["semantic"] == 0 {
		t.Fatal("off/on tombstone lost", controls)
	}
	other, err := s.GetRoomInteractionControls(ctx, 15, 29)
	if err != nil || other.Revision != 0 {
		t.Fatal("cross tenant read leaked")
	}
	if _, err = s.UpsertRoomInteractionPreferences(ctx, 15, 29, 1, in); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross tenant save allowed", err)
	}
	var history int
	var before, after string
	var who int64
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_room_interaction_control_history WHERE tenant_id=14 AND room_id=29`).Scan(&history); err != nil || history != int(saved.ControlsRevision) {
		t.Fatal("audit revisions missing", history, err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT before_json,after_json,updated_by FROM live_room_interaction_control_history WHERE tenant_id=14 AND room_id=29 ORDER BY revision DESC LIMIT 1`).Scan(&before, &after, &who); err != nil || who != 11 {
		t.Fatal("audit actor missing", err)
	}
	var b, a model.RoomInteractionControls
	json.Unmarshal([]byte(before), &b)
	json.Unmarshal([]byte(after), &a)
	if b.Active == nil || *b.Active || a.Active == nil || !*a.Active {
		t.Fatal("audit before/after wrong")
	}
	// An audit failure must roll back both controls and legacy scalar edits.
	exec(`CREATE TRIGGER fail_control_audit BEFORE INSERT ON live_room_interaction_control_history FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test rollback'`)
	revision = saved.ControlsRevision
	in.InteractionActive = &off
	in.QuestionPreference = 1
	if _, err = s.UpsertRoomInteractionPreferences(ctx, 14, 29, 12, in); err == nil {
		t.Fatal("audit failure swallowed")
	}
	afterRollback, err := s.GetRoomInteractionPreferences(ctx, 14, 29)
	if err != nil || afterRollback.ControlsRevision != saved.ControlsRevision || !*afterRollback.InteractionActive || afterRollback.QuestionPreference != 50 {
		t.Fatal("transaction partially committed", afterRollback, err)
	}
}
