package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

func TestScriptHumanVoiceLifecycleMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	const tenant, plan, otherPlan int64 = 14, 91, 92
	for _, query := range []string{
		"CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY, tenant_id BIGINT, status VARCHAR(16))",
		"INSERT INTO live_agent_plans VALUES(91,14,'active'),(92,14,'active')",
		`CREATE TABLE media_assets(id BIGINT PRIMARY KEY AUTO_INCREMENT, tenant_id BIGINT, agent_id BIGINT NULL,
			asset_type VARCHAR(64), original_name TEXT, storage_driver VARCHAR(24), storage_bucket VARCHAR(128), object_key VARCHAR(511),
			mime_type VARCHAR(64), size_bytes BIGINT, duration_ms BIGINT, checksum_sha256 CHAR(64), status VARCHAR(24), metadata_json JSON,
			created_by_user_id BIGINT, created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ensureVoiceAssetCleanupSchema(ctx); err != nil {
		t.Fatal(err)
	}
	piece := voiceasset.Digest([]string{"group", "a"})
	otherPiece := voiceasset.Digest([]string{"group", "b"})
	ms := uint64(2300)
	input := func(name string) *model.CreateMediaAssetInput {
		return &model.CreateMediaAssetInput{OriginalName: name, StorageDriver: "oss", StorageBucket: "test-bucket",
			ObjectKey: ScriptHumanVoicePrefix(tenant, plan, piece) + name, SizeBytes: 20, DurationMS: &ms, ChecksumSHA256: voiceasset.Digest(name)}
	}
	change := func(old int64, in *model.CreateMediaAssetInput) {
		t.Helper()
		if err := s.ChangeScriptHumanVoice(ctx, tenant, plan, piece, old, 20, in); err != nil {
			t.Fatal(err)
		}
	}
	get := func(n int) []model.MediaAsset {
		t.Helper()
		items, err := s.ListScriptHumanVoices(ctx, tenant, plan, piece)
		if err != nil || len(items) != n {
			t.Fatalf("recordings: %v %v", items, err)
		}
		return items
	}
	change(0, input("a.wav"))
	change(0, input("b.wav"))
	items := get(2)
	old := items[0]
	const displayName = "直播原声 · 第二遍"
	for _, name := range []string{displayName, displayName} {
		if err := s.RenameScriptHumanVoice(ctx, tenant, plan, piece, old.ID, name); err != nil {
			t.Fatalf("rename or identical retry: %v", err)
		}
	}
	updated, err := s.ScriptHumanVoice(ctx, tenant, plan, piece, old.ID)
	if err != nil || updated.OriginalName != displayName || updated.ObjectKey != old.ObjectKey ||
		updated.ChecksumSHA256 != old.ChecksumSHA256 || updated.SizeBytes != old.SizeBytes ||
		updated.DurationMS == nil || old.DurationMS == nil || *updated.DurationMS != *old.DurationMS {
		t.Fatalf("rename changed recording identity or content metadata: %+v, %v", updated, err)
	}
	choices, err := s.ListPlanScriptHumanVoices(ctx, tenant, plan)
	if err != nil || len(choices[piece]) != 2 || choices[piece][0].Name != displayName {
		t.Fatalf("playback choices did not reflect rename: %v, %v", choices, err)
	}
	for _, use := range []struct {
		tenant, plan int64
		piece        string
	}{
		{tenant, otherPlan, piece}, {tenant, plan, otherPiece}, {99, plan, piece},
	} {
		if _, err := s.ScriptHumanVoice(ctx, use.tenant, use.plan, use.piece, old.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("recording leaked: %v", err)
		}
		if err := s.RenameScriptHumanVoice(ctx, use.tenant, use.plan, use.piece, old.ID, "unrelated"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("rename crossed recording scope: %v", err)
		}
		if err := s.ChangeScriptHumanVoice(ctx, use.tenant, use.plan, use.piece, old.ID, 20, nil); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("cross-scope mutation: %v", err)
		}
	}
	if err := s.LeaseVoiceAsset(ctx, tenant, old.ID, 29, 3, "program", "track"); err != nil {
		t.Fatal(err)
	}
	sent := false
	if err := s.DispatchVoiceAsset(ctx, tenant, old.ID, 3, "track", func() (string, error) { sent = true; return "program", nil }); err != nil || !sent {
		t.Fatal(sent, err)
	}
	change(old.ID, input("new.wav"))
	items = get(2)
	if items[0].OriginalName != "b.wav" || items[1].OriginalName != "new.wav" {
		t.Fatalf("replacement: %+v", items)
	}
	if err := s.LeaseVoiceAsset(ctx, tenant, old.ID, 29, 3, "program", "new-track"); !errors.Is(err, voiceasset.ErrCleared) {
		t.Fatalf("retired reselected: %v", err)
	}
	leased, err := s.VoiceLeasedAsset(ctx, tenant, old.ID, 3, "track")
	if err != nil || leased.ObjectKey != old.ObjectKey {
		t.Fatalf("accepted playback did not retain original asset: %v %v", leased, err)
	}
	change(items[0].ID, nil)
	get(1)
	cleaned := false
	remove := func(_ context.Context, a model.MediaAsset) (uint64, error) { cleaned = true; return a.SizeBytes, nil }
	jobs, err := s.DueVoiceCleanupJobs(ctx)
	if err != nil || len(jobs) != 2 {
		t.Fatal(jobs, err)
	}
	for _, job := range jobs {
		items, err := s.DueVoiceCleanupItems(ctx, job)
		if err != nil || len(items) != 1 {
			t.Fatal(items, err)
		}
		if items[0].Asset != old.ID {
			continue
		}
		if err = s.DeleteVoiceCleanupObject(ctx, job, items[0], remove); !errors.Is(err, voiceasset.ErrBusy) || cleaned {
			t.Fatalf("active playback failed to protect asset: %v", err)
		}
		if err = s.ReleaseVoiceAssetLease(ctx, VoiceAssetLease{Tenant: tenant, Asset: old.ID, Session: 3, Track: "track"}); err != nil {
			t.Fatal(err)
		}
		if err = s.DeleteVoiceCleanupObject(ctx, job, items[0], remove); err != nil || !cleaned {
			t.Fatal(cleaned, err)
		}
	}
	if !cleaned {
		t.Fatal("retired asset was not cleaned")
	}
}
