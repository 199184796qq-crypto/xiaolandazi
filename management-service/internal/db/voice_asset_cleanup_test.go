package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

func TestVoiceCleanupMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if e := s.ensureVoiceAssetCleanupSchema(ctx); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("live_runtime_schema.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range strings.Split(string(raw), "-- +statement") {
		if strings.HasPrefix(strings.TrimSpace(q), "CREATE TABLE IF NOT EXISTS media_assets (") {
			if _, e = s.db.ExecContext(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
	}
	if output := os.Getenv("VOICE_CLEANUP_SCHEMA_EVIDENCE"); output != "" {
		tables := []string{"voice_cache_epochs", "voice_cleanup_jobs", "voice_cleanup_items", "voice_asset_leases", "voice_local_sources", "voice_asset_retirements", "voice_object_locks", "voice_upload_journal", "voice_asset_usage"}
		evidence := map[string]any{"tables": tables}
		for label, q := range map[string]string{
			"columns": `SELECT CONCAT(TABLE_NAME,':',COLUMN_NAME,':',COLUMN_TYPE,':',IS_NULLABLE) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN('voice_cache_epochs','voice_cleanup_jobs','voice_cleanup_items','voice_asset_leases','voice_local_sources','voice_asset_retirements','voice_object_locks','voice_upload_journal','voice_asset_usage') ORDER BY TABLE_NAME,ORDINAL_POSITION`,
			"indices": `SELECT CONCAT(TABLE_NAME,':',INDEX_NAME,':',SEQ_IN_INDEX,':',COLUMN_NAME,':',NON_UNIQUE) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN('voice_cache_epochs','voice_cleanup_jobs','voice_cleanup_items','voice_asset_leases','voice_local_sources','voice_asset_retirements','voice_object_locks','voice_upload_journal','voice_asset_usage') ORDER BY TABLE_NAME,INDEX_NAME,SEQ_IN_INDEX`,
		} {
			rows, e := s.db.QueryContext(ctx, q)
			if e != nil {
				t.Fatal(e)
			}
			values := []string{}
			for rows.Next() {
				var v string
				if e = rows.Scan(&v); e != nil {
					t.Fatal(e)
				}
				values = append(values, v)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				t.Fatal(e)
			}
			evidence[label] = values
		}
		b, e := json.MarshalIndent(evidence, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(output, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	scope := voiceasset.Scope{Tenant: 14, Plan: 4, Category: "mainline"}
	asset := func(scope voiceasset.Scope, stamp *voiceasset.Stamp, key string) model.MediaAsset {
		t.Helper()
		meta := map[string]any{"plan_id": scope.Plan, "purpose": "live_agent_formal_voice_timeline", "paragraph_key": voiceasset.Digest(key)}
		c := ctx
		if stamp != nil {
			stamp.Metadata(meta)
			c = voiceasset.WithStamp(c, *stamp, s.CheckVoiceCacheStamp)
		}
		a, e := s.CreateMediaAsset(c, model.CreateMediaAssetInput{TenantID: scope.Tenant, AssetType: "generated_voice", OriginalName: "paid.wav", StorageDriver: "oss", StorageBucket: "fixture", ObjectKey: key, SizeBytes: 88, Metadata: meta})
		if e != nil {
			t.Fatal(e)
		}
		return a
	}
	clear := func(scope voiceasset.Scope, id string) voiceasset.Job {
		t.Helper()
		before, e := s.VoiceCacheStamp(ctx, scope)
		if e != nil {
			t.Fatal(e)
		}
		j, e := s.ClearVoiceAssets(ctx, voiceasset.ClearRequest{Scope: scope, Expected: before, RequestID: voiceasset.Digest(id), Actor: 19})
		if e != nil {
			t.Fatal(e)
		}
		return j
	}
	scan := func(j voiceasset.Job) []voiceasset.Candidate {
		t.Helper()
		if e := s.ScanVoiceCleanupJob(ctx, j); e != nil {
			t.Fatal(e)
		}
		out, e := s.DueVoiceCleanupItems(ctx, j)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}

	t.Run("legacy immediately invalid and restart preserves epoch", func(t *testing.T) {
		a := asset(scope, nil, "old.wav")
		j := clear(scope, "clear1")
		if _, e = s.GetMediaAsset(ctx, 14, a.ID); !errors.Is(e, voiceasset.ErrCleared) {
			t.Fatal("old audio still readable before scan", e)
		}
		restarted := &Store{db: s.db}
		st, e := restarted.VoiceCacheStamp(ctx, scope)
		if e != nil || st.PlanEpoch != 1 {
			t.Fatal(st, e)
		}
		repeated, e := s.ClearVoiceAssets(ctx, voiceasset.ClearRequest{Scope: scope, Expected: j.Before, RequestID: j.ID})
		if e != nil || repeated.ID != j.ID {
			t.Fatal("retry changed epoch/job", e)
		}
		items := scan(j)
		if len(items) != 1 {
			t.Fatal(items)
		}
		calls := 0
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], func(context.Context, model.MediaAsset) (uint64, error) { calls++; return 88, nil }); e != nil {
			t.Fatal(e)
		}
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], func(context.Context, model.MediaAsset) (uint64, error) { calls++; return 88, nil }); e != nil {
			t.Fatal(e)
		}
		if calls != 1 {
			t.Fatal("duplicate physical delete", calls)
		}
	})
	t.Run("accepted playback blocks delete and rejects new references", func(t *testing.T) {
		sc := scope
		sc.Plan = 5
		a := asset(sc, nil, "playing.wav")
		if e = s.LeaseVoiceAsset(ctx, 14, a.ID, 29, 641, "program", "long-641-1"); e != nil {
			t.Fatal(e)
		}
		j := clear(sc, "clear-playing")
		items := scan(j)
		if e = s.LeaseVoiceAsset(ctx, 14, a.ID, 30, 642, "other", "long-642-1"); !errors.Is(e, voiceasset.ErrCleared) {
			t.Fatal("new stale reference accepted", e)
		}
		calls := 0
		remove := func(context.Context, model.MediaAsset) (uint64, error) { calls++; return 88, nil }
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], remove); !errors.Is(e, voiceasset.ErrBusy) || calls != 0 {
			t.Fatal("active audio deleted", e)
		}
		if _, e = s.VoiceLeasedAsset(ctx, 14, a.ID, 641, "long-641-1"); e != nil {
			t.Fatal("accepted retry lost audio", e)
		}
		if e = s.ReleaseVoiceAssetLease(ctx, VoiceAssetLease{Tenant: 14, Asset: a.ID, Session: 641, Track: "long-641-1"}); e != nil {
			t.Fatal(e)
		}
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], remove); e != nil || calls != 1 {
			t.Fatal(e, calls)
		}
	})
	t.Run("other plan reference retains shared object", func(t *testing.T) {
		aScope, bScope := scope, scope
		aScope.Plan = 6
		bScope.Plan = 7
		a := asset(aScope, nil, "shared.wav")
		b := asset(bScope, nil, "shared.wav")
		j := clear(aScope, "clear-shared")
		items := scan(j)
		if len(items) != 1 || items[0].Asset != a.ID {
			t.Fatal(items)
		}
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], func(context.Context, model.MediaAsset) (uint64, error) {
			t.Fatal("other owner's object deleted")
			return 0, nil
		}); e != nil {
			t.Fatal(e)
		}
		if _, e = s.GetMediaAsset(ctx, 14, b.ID); e != nil {
			t.Fatal("other owner lost audio", e)
		}
	})
	t.Run("clear is serialized with actual handoff and pending is not accepted", func(t *testing.T) {
		sc := scope
		sc.Plan = 24
		a := asset(sc, nil, "handoff24.wav")
		if e = s.LeaseVoiceAsset(ctx, 14, a.ID, 29, 641, "", "pending24"); e != nil {
			t.Fatal(e)
		}
		_ = clear(sc, "pending24-clear")
		if e = s.DispatchVoiceAsset(ctx, 14, a.ID, 641, "pending24", func() (string, error) { t.Fatal("unaccepted old candidate sent after clear"); return "p", nil }); !errors.Is(e, voiceasset.ErrCleared) {
			t.Fatal(e)
		}
		sc.Plan = 25
		a = asset(sc, nil, "handoff25.wav")
		if e = s.LeaseVoiceAsset(ctx, 14, a.ID, 29, 641, "", "accepted25"); e != nil {
			t.Fatal(e)
		}
		entered := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		defer releaseOnce.Do(func() { close(release) })
		sent := make(chan error, 1)
		go func() {
			sent <- s.DispatchVoiceAsset(ctx, 14, a.ID, 641, "accepted25", func() (string, error) { close(entered); <-release; return "p25", nil })
		}()
		<-entered
		cleared := make(chan error, 1)
		before, _ := s.VoiceCacheStamp(ctx, sc)
		go func() {
			_, err := s.ClearVoiceAssets(ctx, voiceasset.ClearRequest{Scope: sc, Expected: before, RequestID: voiceasset.Digest("handoff25-clear")})
			cleared <- err
		}()
		select {
		case e := <-cleared:
			t.Fatal("clear crossed in-flight Core handoff", e)
		case <-time.After(25 * time.Millisecond):
		}
		releaseOnce.Do(func() { close(release) })
		if e = <-sent; e != nil {
			t.Fatal(e)
		}
		if e = <-cleared; e != nil {
			t.Fatal(e)
		}
		if e = s.DispatchVoiceAsset(ctx, 14, a.ID, 641, "accepted25", func() (string, error) { return "p25", nil }); e != nil {
			t.Fatal("accepted exact retry lost its sound", e)
		}
	})
	t.Run("late receipt after completed scan is quarantined and queued", func(t *testing.T) {
		sc := scope
		sc.Plan = 8
		old, _ := s.VoiceCacheStamp(ctx, sc)
		j := clear(sc, "clear-late")
		_ = scan(j)
		fresh, _ := s.VoiceCacheStamp(ctx, sc)
		a := asset(sc, &old, "late.wav")
		_ = asset(sc, &fresh, "new.wav")
		if a.Status != "quarantined" {
			t.Fatal("late result activated", a.Status)
		}
		items, e := s.DueVoiceCleanupItems(ctx, j)
		if e != nil || len(items) != 1 || items[0].Asset != a.ID {
			t.Fatal("late result escaped completed scan", items, e)
		}
	})
	t.Run("failure retry preserves invalidation", func(t *testing.T) {
		sc := scope
		sc.Plan = 9
		a := asset(sc, nil, "retry.wav")
		j := clear(sc, "clear-fail")
		items := scan(j)
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], func(context.Context, model.MediaAsset) (uint64, error) { return 0, fmt.Errorf("fixture failure") }); e == nil {
			t.Fatal("failure hidden")
		}
		if _, e = s.GetMediaAsset(ctx, 14, a.ID); !errors.Is(e, voiceasset.ErrCleared) {
			t.Fatal("failure revived asset", e)
		}
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], func(context.Context, model.MediaAsset) (uint64, error) { return 88, nil }); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("concurrent clear has exactly one CAS winner", func(t *testing.T) {
		sc := scope
		sc.Plan = 10
		before, _ := s.VoiceCacheStamp(ctx, sc)
		start := make(chan struct{})
		results := make(chan error, 16)
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, e := s.ClearVoiceAssets(ctx, voiceasset.ClearRequest{Scope: sc, Expected: before, RequestID: voiceasset.Digest(fmt.Sprint("concurrent", i))})
				results <- e
			}(i)
		}
		close(start)
		wg.Wait()
		close(results)
		wins := 0
		for e := range results {
			if e == nil {
				wins++
			} else if !errors.Is(e, voiceasset.ErrConflict) {
				t.Fatal(e)
			}
		}
		if wins != 1 {
			t.Fatal("lost update", wins)
		}
	})
	t.Run("selected voice does not clear a different complete identity", func(t *testing.T) {
		sc := scope
		sc.Plan = 21
		sc.Voice = voiceasset.Digest("voice A")
		sa, _ := s.VoiceCacheStamp(ctx, sc)
		a := asset(sc, &sa, "a21.wav")
		other := sc
		other.Voice = voiceasset.Digest("voice B")
		sb, _ := s.VoiceCacheStamp(ctx, other)
		b := asset(other, &sb, "b21.wav")
		j := clear(sc, "selected21")
		items := scan(j)
		if len(items) != 1 || items[0].Asset != a.ID {
			t.Fatal(items)
		}
		if _, e = s.GetMediaAsset(ctx, 14, b.ID); e != nil {
			t.Fatal("other voice cleared", e)
		}
		// A forged item for the fresh generation cannot bypass this job's cutoff.
		now, _ := s.VoiceCacheStamp(ctx, sc)
		fresh := asset(sc, &now, "new21.wav")
		bad := items[0]
		bad.Asset = fresh.ID
		bad.Key = fresh.ObjectKey
		bad.Stamp = now
		if e = s.DeleteVoiceCleanupObject(ctx, j, bad, func(context.Context, model.MediaAsset) (uint64, error) { t.Fatal("new epoch deleted"); return 0, nil }); !errors.Is(e, voiceasset.ErrConflict) {
			t.Fatal(e)
		}
	})
	t.Run("local only keeps OSS eligibility and shared local owner", func(t *testing.T) {
		sc := scope
		sc.Plan = 22
		st, _ := s.VoiceCacheStamp(ctx, sc)
		a := asset(sc, &st, "local22.wav")
		other := sc
		other.Voice = voiceasset.Digest("different owner")
		so, _ := s.VoiceCacheStamp(ctx, other)
		b := asset(other, &so, "local-other22.wav")
		path := "/fixture/paid-shared-directory"
		if e = s.RegisterLegacyVoiceLocalSource(ctx, st, path, a.ID); e != nil {
			t.Fatal(e)
		}
		if e = s.RegisterLegacyVoiceLocalSource(ctx, so, path, b.ID); e != nil {
			t.Fatal(e)
		}
		j, e := s.ClearVoiceAssets(ctx, voiceasset.ClearRequest{Scope: sc, Expected: st, RequestID: voiceasset.Digest("local22"), Mode: "local_only"})
		if e != nil {
			t.Fatal(e)
		}
		sources, e := s.ClearedVoiceLocalSources(ctx, j)
		if e != nil || len(sources) != 2 {
			t.Fatal(sources, e)
		}
		if e = s.DeleteVoiceLocalSource(ctx, j, sources[0], func() error { t.Fatal("shared local directory removed"); return nil }); e != nil {
			t.Fatal(e)
		}
		if _, e = s.GetMediaAsset(ctx, 14, a.ID); e != nil {
			t.Fatal("local clear invalidated OSS", e)
		}
		current, _ := s.VoiceCacheStamp(ctx, sc)
		if current != st {
			t.Fatal("local clear changed epoch")
		}
	})
	t.Run("retirement survives restart without rearchiving and stays dry run", func(t *testing.T) {
		sc := scope
		sc.Plan = 23
		st, _ := s.VoiceCacheStamp(ctx, sc)
		a := asset(sc, &st, "retire23.wav")
		key := voiceasset.Digest("retire-key23")
		if e = s.RetireVoiceAsset(ctx, 14, 23, a.ID, key); e != nil {
			t.Fatal(e)
		}
		restarted := &Store{db: s.db}
		if e = restarted.CheckVoiceParagraph(ctx, 14, 23, key); !errors.Is(e, voiceasset.ErrCleared) {
			t.Fatal(e)
		}
		if e = s.LeaseVoiceAsset(ctx, 14, a.ID, 29, 641, "p", "retired"); !errors.Is(e, voiceasset.ErrCleared) {
			t.Fatal("retired sound leased", e)
		}
		jobs, e := s.DueVoiceCleanupJobs(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, j := range jobs {
			if j.Mode == "retired" {
				t.Fatal("automatic deletion enabled before review")
			}
		}
	})
	t.Run("review and backoff cannot starve actionable jobs", func(t *testing.T) {
		for i := 0; i < 24; i++ {
			sc := scope
			sc.Plan = int64(100 + i)
			j := clear(sc, fmt.Sprint("review", i))
			if _, e = s.db.ExecContext(ctx, `UPDATE voice_cleanup_jobs SET state='review_required' WHERE id=?`, j.ID); e != nil {
				t.Fatal(e)
			}
		}
		sc := scope
		sc.Plan = 150
		j := clear(sc, "fair150")
		due, e := s.DueVoiceCleanupJobs(ctx)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, v := range due {
			found = found || v.ID == j.ID
		}
		if !found {
			t.Fatal("review queue starved new clear")
		}
		if e = s.DeferVoiceCleanupJob(ctx, j, true); e != nil {
			t.Fatal(e)
		}
		due, e = s.DueVoiceCleanupJobs(ctx)
		if e != nil {
			t.Fatal(e)
		}
		for _, v := range due {
			if v.ID == j.ID {
				t.Fatal("backoff ignored")
			}
		}
	})
	t.Run("epoch overflow and malformed owner fail closed", func(t *testing.T) {
		sc := scope
		sc.Plan = 151
		if _, e = s.db.ExecContext(ctx, `INSERT INTO voice_cache_epochs(tenant_id,plan_id,category,voice_key,epoch) VALUES(14,151,'mainline','',?)`, uint64(math.MaxUint64)); e != nil {
			t.Fatal(e)
		}
		before, _ := s.VoiceCacheStamp(ctx, sc)
		if _, e = s.ClearVoiceAssets(ctx, voiceasset.ClearRequest{Scope: sc, Expected: before, RequestID: voiceasset.Digest("overflow")}); !errors.Is(e, voiceasset.ErrConflict) {
			t.Fatal("epoch wrapped", e)
		}
	})
	t.Run("current object missing after DB write failure converges conservatively", func(t *testing.T) {
		sc := scope
		sc.Plan = 152
		a := asset(sc, nil, "missing152.wav")
		j := clear(sc, "missing152")
		items := scan(j)
		if e = s.DeleteVoiceCleanupObject(ctx, j, items[0], func(context.Context, model.MediaAsset) (uint64, error) { return 0, nil }); e != nil {
			t.Fatal(e)
		}
		done, e := s.VoiceCleanupJob(ctx, 14, j.ID)
		if e != nil || done.Counts["confirmed_bytes"] != 0 {
			t.Fatal("unconfirmed bytes overstated", done, e)
		}
		if _, e = s.GetMediaAsset(ctx, 14, a.ID); !errors.Is(e, voiceasset.ErrCleared) && !errors.Is(e, sql.ErrNoRows) {
			t.Fatal("deleted media remained readable", e)
		}
	})
}
