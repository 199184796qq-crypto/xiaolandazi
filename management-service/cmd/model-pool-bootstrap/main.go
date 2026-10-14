// One-time operator tool: explicitly import the existing text credential with
// a user-specified capacity. It never prints credentials or starts room AI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func run() error {
	capacity := flag.Int("capacity", 0, "operator-confirmed room capacity")
	flag.Parse()
	if *capacity < 1 || *capacity > 10000 {
		return fmt.Errorf("explicit capacity between 1 and 10000 is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := db.Open(config.Load())
	if err != nil {
		return fmt.Errorf("database unavailable")
	}
	defer s.Close()
	active, err := s.ListRunningLiveRuntimeSessions(ctx)
	if err != nil {
		return fmt.Errorf("cannot check AI sessions")
	}
	if len(active) != 0 {
		return fmt.Errorf("active or paused AI: refuse bootstrap")
	}
	if err = s.MigrateModelPool(ctx); err != nil {
		return fmt.Errorf("model pool migration failed: %w", err)
	}
	entries, err := s.ListModelPoolEntries(ctx)
	if err != nil {
		return fmt.Errorf("cannot check existing model pool")
	}
	if len(entries) != 0 {
		return fmt.Errorf("model pool already configured; use the administrator page")
	}
	p, err := s.ResolveSpeechModel(ctx, "")
	if err != nil || p == nil || p.APIKey == "" {
		return fmt.Errorf("no current speech credential available")
	}
	audit := model.AdminAuditLog{ActorType: "system", Source: "authorized_deployment", Action: "model_pool_import_current", ObjectType: "model_pool", ObjectName: "当前口播模型账号", Reason: "用户明确指定现有账号名额", DetailJSON: fmt.Sprintf(`{"capacity":%d}`, *capacity), Result: "pending"}
	auditID, err := s.BeginAdminAudit(ctx, audit)
	if err != nil {
		return fmt.Errorf("cannot record bootstrap audit")
	}
	entry, err := s.SaveModelPoolEntry(ctx, db.ModelPoolEntryInput{AccountName: "当前口播模型账号", Profile: agentgateway.SpeechModelInput{SpeechModel: *p, NewAPIKey: p.APIKey}, MaxRooms: *capacity, Enabled: true}, 0)
	if err != nil {
		audit.Result = "failed"
		_ = s.CompleteAdminAudit(ctx, auditID, audit)
		return fmt.Errorf("import existing account failed")
	}
	audit.Result = "success"
	audit.ObjectID = entry.ID
	if err = s.CompleteAdminAudit(ctx, auditID, audit); err != nil {
		return fmt.Errorf("account imported but final audit failed; inspect before retry")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"entry_id": entry.ID, "model": entry.Profile.Model, "max_rooms": entry.MaxRooms, "enabled": entry.Enabled, "credentials_returned": false})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
