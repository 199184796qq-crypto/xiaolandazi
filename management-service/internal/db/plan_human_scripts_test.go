package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

func TestPlanHumanScriptsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	const tenant, plan int64 = 14, 91
	for _, query := range []string{
		"CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY, tenant_id BIGINT, status VARCHAR(16))",
		"INSERT INTO live_agent_plans VALUES(91,14,'active'),(92,14,'active')",
		`CREATE TABLE media_assets(id BIGINT PRIMARY KEY AUTO_INCREMENT, tenant_id BIGINT, agent_id BIGINT NULL,
 asset_type VARCHAR(64), original_name TEXT, storage_driver VARCHAR(24), storage_bucket VARCHAR(128), object_key VARCHAR(511),
 mime_type VARCHAR(64), size_bytes BIGINT, duration_ms BIGINT, checksum_sha256 CHAR(64), status VARCHAR(24), metadata_json JSON,
 created_by_user_id BIGINT, created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`,
	} {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ensureVoiceAssetCleanupSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateHumanFission(ctx); err != nil {
		t.Fatal(err)
	}
	ms := uint64(3000)
	for _, name := range []string{"first.wav", "second.wav"} {
		in := &model.CreateMediaAssetInput{OriginalName: name, StorageDriver: "oss", StorageBucket: "fixture", ObjectKey: ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope) + name, DurationMS: &ms, SizeBytes: 100, ChecksumSHA256: voiceasset.Digest(name)}
		if err := s.ChangeScriptHumanVoice(ctx, tenant, plan, PlanHumanScriptScope, 0, 4, in); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.ListScriptHumanVoices(ctx, tenant, plan, PlanHumanScriptScope)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	a, b := list[0].ID, list[1].ID
	job, err := s.NextPlanHumanScriptASR(ctx)
	if err != nil || job != (PlanHumanScriptASRJob{TenantID: tenant, PlanID: plan, AssetID: a}) {
		t.Fatal(job, err)
	}
	if err = s.ReorderPlanHumanScripts(ctx, tenant, plan, []int64{b, b}); !errors.Is(err, ErrPlanHumanScriptOrder) {
		t.Fatal(err)
	}
	if err = s.ReorderPlanHumanScripts(ctx, tenant, plan, []int64{b, a}); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListScriptHumanVoices(ctx, tenant, plan, PlanHumanScriptScope)
	if err != nil || list[0].ID != b || list[1].ID != a {
		t.Fatal(list, err)
	}
	sources, err := s.ListPlanHumanScriptSources(ctx, tenant, plan)
	if err != nil || len(sources) != 2 || sources[0].ID != b || sources[1].ID != a || sources[0].Duration != ms {
		t.Fatal(sources, err)
	}
	if outside, err := s.ListPlanHumanScriptSources(ctx, tenant, plan+1); err != nil || len(outside) != 0 {
		t.Fatal(outside, err)
	}
	if outside, err := s.ListPlanHumanScriptSources(ctx, tenant+1, plan); err != nil || len(outside) != 0 {
		t.Fatal(outside, err)
	}
	choices, err := s.ListPlanScriptHumanVoices(ctx, tenant, plan)
	if err != nil || len(choices) != 0 {
		t.Fatal(choices, err)
	}
	if ok, err := s.ClaimPlanHumanScriptASR(ctx, tenant, 92, a, "wrong", time.Now().Add(time.Minute)); ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := s.ClaimPlanHumanScriptASR(ctx, tenant, plan, a, "token", time.Now().Add(time.Minute)); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := s.ClaimPlanHumanScriptASR(ctx, tenant, plan, a, "duplicate", time.Now().Add(time.Minute)); ok || err != nil {
		t.Fatal(ok, err)
	}
	if err = s.SavePlanHumanScriptTranscript(ctx, tenant, plan, a, "手工修改"); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishPlanHumanScriptASR(ctx, tenant, plan, a, "token", "较晚返回的自动结果"); err != nil {
		t.Fatal(err)
	}
	asset, err := s.ScriptHumanVoice(ctx, tenant, plan, PlanHumanScriptScope, a)
	if err != nil || asset.Metadata["script_transcript"] != "手工修改" {
		t.Fatal(asset.Metadata, err)
	}
	if err = s.QueuePlanHumanScriptASR(ctx, tenant, plan, a); err != nil {
		t.Fatal(err)
	}
	job, err = s.NextPlanHumanScriptASR(ctx)
	if err != nil || job.AssetID != b {
		t.Fatal(job, err)
	}
	if ok, err := s.ClaimPlanHumanScriptASR(ctx, tenant, plan, b, "failed-job", time.Now().Add(time.Minute)); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if err = s.FinishPlanHumanScriptASR(ctx, tenant, plan, b, "failed-job", ""); err != nil {
		t.Fatal(err)
	}
	if err = s.QueuePlanHumanScriptASR(ctx, tenant, plan, b); err != nil {
		t.Fatal(err)
	}
	job, err = s.NextPlanHumanScriptASR(ctx)
	if err != nil || job.AssetID != b {
		t.Fatal(job, err)
	}
	srt := "1\n00:00:00,000 --> 00:00:03,000\n录音文稿。"
	if err = s.SavePlanHumanScriptTranscript(ctx, tenant, plan, a, srt); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = s.QueueHumanFission(ctx, tenant, plan, a, "plan"); err != nil {
			t.Fatal(err)
		}
	}
	fission, err := s.ClaimHumanFission(ctx, "fission", time.Now().UTC().Add(time.Minute))
	if err != nil || fission.Source != a || fission.Status != "running" {
		t.Fatal(fission, err)
	}
	if err = s.ClearHumanFission(ctx, tenant, plan, a, 4, fission.ClearRevision()); !errors.Is(err, ErrFissionClearBusy) {
		t.Fatalf("clear must protect the active job: %v", err)
	}
	if _, err = s.ClaimHumanFission(ctx, "duplicate", time.Now().UTC().Add(time.Minute)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	progress := "正在切分第 1 / 20 片…"
	if err = s.UpdateHumanFissionProgress(ctx, fission, progress); err != nil {
		t.Fatal(err)
	}
	stale := fission
	stale.Token = "stale"
	if err = s.UpdateHumanFissionProgress(ctx, stale, "旧任务"); !errors.Is(err, ErrFissionUpdated) {
		t.Fatal(err)
	}
	found, err := s.HumanFission(ctx, tenant, plan, a)
	if err != nil || found.Status != "running" || found.Detail != progress {
		t.Fatal(found, err)
	}
	derived := model.CreateMediaAssetInput{
		OriginalName: "slice", StorageDriver: "oss", StorageBucket: "fixture",
		ObjectKey:  fission.ObjectKey("slice"),
		DurationMS: &ms, SizeBytes: 100,
	}
	stored := false
	if err = s.SaveHumanFissionMedia(ctx, fission, derived, func() error { stored = true; return nil }); err != nil || !stored {
		t.Fatal(stored, err)
	}
	choices, err = s.ListPlanScriptHumanVoices(ctx, tenant, plan)
	if err != nil || len(choices) != 0 {
		t.Fatalf("derived audio leaked into the room selection: %v %v", choices, err)
	}
	fission.State.Parts = []humanfission.Part{{Object: derived.ObjectKey, Duration: int64(ms), Text: "录音文稿。"}}
	if err = s.SaveHumanFission(ctx, fission, "running", "", 0); err != nil {
		t.Fatal(err)
	}
	parts, err := s.ListHumanFissionPartsStatus(ctx, tenant, plan)
	if err != nil || !parts[a].Pending || parts[a].Count != 0 {
		t.Fatal(parts, err)
	}
	fission.State.Parts[0].SRT = srt
	if err = s.SaveHumanFission(ctx, fission, "running", "", 0); err != nil {
		t.Fatal(err)
	}
	parts, err = s.ListHumanFissionPartsStatus(ctx, tenant, plan)
	if err != nil || parts[a].Count != 1 || parts[a].Input != fission.State.Input {
		t.Fatal(parts, err)
	}
	if _, err = s.ScriptHumanVoiceByObject(ctx, tenant, plan, HumanFissionScope(a), derived.ObjectKey); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ScriptHumanVoiceByObject(ctx, tenant, plan, HumanFissionScope(b), derived.ObjectKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	fission.State.Recipes = []humanfission.Recipe{{ID: humanfission.Digest("named-recipe"), Order: []int{1}}}
	if err = s.SaveHumanFission(ctx, fission, "ready", "", 0); err != nil {
		t.Fatal(err)
	}
	partEdit := HumanFissionNameEdit{Part: 1, Version: fission.State.Parts[0].Version(),
		Name: "用户自定语音片名称", ExpectedName: fission.State.Parts[0].DisplayName(1)}
	if err = s.RenameHumanFission(ctx, tenant, plan, a, partEdit); err != nil {
		t.Fatal(err)
	}
	if err = s.RenameHumanFission(ctx, tenant, plan, a, partEdit); !errors.Is(err, ErrFissionUpdated) {
		t.Fatalf("stale name edit overwrote another editor: %v", err)
	}
	if err = s.QueueHumanFissionNames(ctx, tenant, plan, a); err != nil {
		t.Fatal(err)
	}
	naming, err := s.ClaimHumanFission(ctx, "names", time.Now().UTC().Add(time.Minute))
	if err != nil || naming.Action != "names" || naming.State.Parts[0].Name != partEdit.Name {
		t.Fatal(naming, err)
	}
	recipeID := humanfission.Digest("named-recipe")
	naming.State.Recipes = []humanfission.Recipe{{ID: recipeID, Name: "鲜果和用券", Order: []int{1}}}
	if err = s.SaveHumanFission(ctx, naming, "ready", "", 0); err != nil {
		t.Fatal(err)
	}
	recipeEdit := HumanFissionNameEdit{Recipe: recipeID, Version: naming.State.RecipeVersion(),
		Name: "鲜果点播", ExpectedName: "鲜果和用券"}
	if err = s.RenameHumanFission(ctx, tenant, plan, a, recipeEdit); err != nil {
		t.Fatal(err)
	}
	visible, err := s.ListHumanFissionChoices(ctx, tenant, plan)
	if err != nil || len(visible) != 1 || visible[0].State.Recipes[0].Name != recipeEdit.Name ||
		visible[0].State.RecipeVersion() != recipeEdit.Version {
		t.Fatal(visible, err)
	}
	if other, err := s.ListHumanFissionChoices(ctx, tenant+1, plan); err != nil || len(other) != 0 {
		t.Fatal(other, err)
	}
	exact := humanfission.ExactRequest{Recipe: recipeID, Version: recipeEdit.Version, Stamp: "same-facts", RequestID: "manual-request-123"}
	if err = s.QueueExactHumanFission(ctx, tenant, plan, a, exact); err != nil {
		t.Fatal(err)
	}
	if err = s.RenameHumanFission(ctx, tenant, plan, a, recipeEdit); !errors.Is(err, ErrFissionBusy) {
		t.Fatal(err)
	}
	exactJob, err := s.ClaimHumanFission(ctx, "exact", time.Now().UTC().Add(time.Minute))
	if err != nil || exactJob.Action != "render_exact" || exactJob.State.Exact == nil || *exactJob.State.Exact != exact {
		t.Fatal(exactJob, err)
	}
	if err = s.SaveHumanFission(ctx, exactJob, "ready", "", 0); err != nil {
		t.Fatal(err)
	}
	badPair := humanfission.MergeRequest{First: 1, Second: 3}
	if err = s.QueueHumanFissionMerge(ctx, tenant, plan, a, badPair); !errors.Is(err, humanfission.ErrMergeSelection) {
		t.Fatalf("nonadjacent merge must be rejected: %v", err)
	}
	selection := humanfission.SplitRequest{Number: 1, Version: "stale", Start: 0, End: 2}
	if err = s.QueueHumanFissionSplit(ctx, tenant, plan, a, selection); !errors.Is(err, ErrFissionUpdated) {
		t.Fatal(err)
	}
	selection.Version = fission.State.Parts[0].Version()
	for range 2 {
		if err = s.QueueHumanFissionSplit(ctx, tenant, plan, a, selection); err != nil {
			t.Fatal(err)
		}
	}
	other := selection
	other.Start, other.End = 2, 4
	if err = s.QueueHumanFissionSplit(ctx, tenant, plan, a, other); !errors.Is(err, ErrFissionBusy) {
		t.Fatal(err)
	}
	split, err := s.ClaimHumanFission(ctx, "split", time.Now().UTC().Add(time.Minute))
	if err != nil || split.Action != "split" || split.State.Split == nil || *split.State.Split != selection {
		t.Fatal(split, err)
	}
	derivedKeys := map[string]bool{derived.ObjectKey: true}
	for _, name := range []string{"mix", "tts"} {
		asset := derived
		asset.OriginalName, asset.ObjectKey = name, split.ObjectKey(name)
		derivedKeys[asset.ObjectKey] = true
		if err = s.SaveHumanFissionMedia(ctx, split, asset, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	split.State.Parts = append(split.State.Parts, humanfission.Part{Object: split.ObjectKey("tts"), SRT: srt})
	split.State.Recipes = []humanfission.Recipe{{ID: "preview", Order: []int{1}}}
	split.State.Render = &humanfission.Render{RecipeID: "preview", Object: split.ObjectKey("mix"), SRT: srt}
	if err = s.SaveHumanFission(ctx, split, "failed", "sample", 0); err != nil {
		t.Fatal(err)
	}
	pair := humanfission.MergeRequest{First: 1, Second: 2, FirstVersion: split.State.Parts[0].Version(), SecondVersion: "stale"}
	if err = s.QueueHumanFissionMerge(ctx, tenant, plan, a, pair); !errors.Is(err, ErrFissionUpdated) {
		t.Fatalf("stale merge must be rejected: %v", err)
	}
	pair.SecondVersion = split.State.Parts[1].Version()
	for range 2 {
		if err = s.QueueHumanFissionMerge(ctx, tenant, plan, a, pair); err != nil {
			t.Fatal(err)
		}
	}
	merge, err := s.ClaimHumanFission(ctx, "merge", time.Now().UTC().Add(time.Minute))
	if err != nil || merge.Action != "merge" || merge.State.Merge == nil || *merge.State.Merge != pair {
		t.Fatalf("expected one versioned merge job: %+v, %v", merge, err)
	}
	if err = s.QueueHumanFissionSplit(ctx, tenant, plan, a, selection); !errors.Is(err, ErrFissionBusy) {
		t.Fatalf("split must not overlap a merge: %v", err)
	}
	if err = s.SaveHumanFission(ctx, merge, "ready", "", 0); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.HumanFission(ctx, tenant, plan, a)
	if err != nil {
		t.Fatal(err)
	}
	revision := snapshot.ClearRevision()
	if err = s.ClearHumanFission(ctx, tenant, 92, a, 4, revision); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cross-plan clear should be rejected: %v", err)
	}
	if err = s.ClearHumanFission(ctx, tenant, plan, a, 4, "stale"); !errors.Is(err, ErrFissionClearChanged) {
		t.Fatalf("stale confirmation should be rejected: %v", err)
	}
	for range 2 {
		if err = s.ClearHumanFission(ctx, tenant, plan, a, 4, revision); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.HumanFission(ctx, tenant, plan, a); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("derived documents and plans still exist: %v", err)
	}
	for _, source := range []int64{a, b} {
		recording, readErr := s.ScriptHumanVoice(ctx, tenant, plan, PlanHumanScriptScope, source)
		if readErr != nil || source == a && recording.Metadata["script_transcript"] != srt {
			t.Fatalf("original recording or transcript changed: %v %v", recording.ID, readErr)
		}
	}
	for key := range derivedKeys {
		if _, err = s.ScriptHumanVoiceByObject(ctx, tenant, plan, HumanFissionScope(a), key); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("cleared derived object still available: %v", err)
		}
	}
	if err = s.QueueHumanFission(ctx, tenant, plan, a, "plan"); err != nil {
		t.Fatal(err)
	}
	next, err := s.ClaimHumanFission(ctx, "recreated", time.Now().UTC().Add(time.Minute))
	if err != nil || next.State.Input != fission.State.Input || next.ObjectKey("slice") == derived.ObjectKey {
		t.Fatalf("regeneration must use a new object key for the same original: %v, %v", next.State.Generation, err)
	}
	rebuilt := derived
	rebuilt.ObjectKey = next.ObjectKey("slice")
	if err = s.SaveHumanFissionMedia(ctx, next, rebuilt, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveHumanFission(ctx, next, "ready", "", 0); err != nil {
		t.Fatal(err)
	}
	if err = s.ClearHumanFission(ctx, tenant, plan, a, 4, revision); !errors.Is(err, ErrFissionClearChanged) {
		t.Fatalf("an older confirmation must not clear a newly generated result: %v", err)
	}
	if _, err = s.ScriptHumanVoiceByObject(ctx, tenant, plan, HumanFissionScope(a), rebuilt.ObjectKey); err != nil {
		t.Fatalf("regenerated object is unavailable: %v", err)
	}
	derivedKeys[rebuilt.ObjectKey] = true
	if err = s.ChangeScriptHumanVoice(ctx, tenant, plan, PlanHumanScriptScope, b, 4, nil); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = retirePlanHumanScripts(ctx, tx, tenant, plan, 4); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM live_agent_plans WHERE id=?", plan); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	list, err = s.ListScriptHumanVoices(ctx, tenant, plan, PlanHumanScriptScope)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	if _, err = s.HumanFission(ctx, tenant, plan, a); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	jobs, err := s.DueVoiceCleanupJobs(ctx)
	if err != nil || len(jobs) != len(derivedKeys)+2 {
		t.Fatal(jobs, err)
	}
	removed := map[int64]bool{}
	removedDerived := map[string]bool{}
	for _, job := range jobs {
		items, err := s.DueVoiceCleanupItems(ctx, job)
		if err != nil || len(items) != 1 {
			t.Fatal(items, err)
		}
		if err = s.DeleteVoiceCleanupObject(ctx, job, items[0], func(_ context.Context, asset model.MediaAsset) (uint64, error) {
			removed[asset.ID] = true
			if derivedKeys[asset.ObjectKey] {
				removedDerived[asset.ObjectKey] = true
			}
			return asset.SizeBytes, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !removed[a] || !removed[b] || len(removedDerived) != len(derivedKeys) {
		t.Fatal(removed)
	}
}
