package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestLiveContentRefreshMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,name VARCHAR(128) NOT NULL,platform VARCHAR(32) NOT NULL DEFAULT 'douyin',external_room_id VARCHAR(128) NOT NULL DEFAULT '')`)
	wanted := map[string]bool{}
	for _, name := range []string{"live_runtime_sessions", "live_agent_plan_room_bindings", "live_agent_room_plan_selections", "live_agent_room_plan_publications", "live_agent_plan_versions", "live_agent_plan_facts", "live_agent_plan_product_links", "live_agent_plan_benefits", "live_agent_plan_scripts", "live_agent_plan_script_references", "live_agent_plan_terms", "live_policy_scopes"} {
		wanted[name] = true
	}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(raw)
		if len(fields) > 5 && fields[0] == "CREATE" && wanted[fields[5]] {
			exec(raw)
			delete(wanted, fields[5])
		}
	}
	if len(wanted) != 0 {
		t.Fatal("missing production fixture tables", wanted)
	}
	for _, migration := range []func(context.Context) error{s.MigrateRoomDeletions, s.migrateLiveContentPolicies, s.MigrateLiveContentRefresh, s.MigrateLiveContentRefresh} {
		if err := migration(ctx); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	seed := func(room int64) (model.LiveContentRefreshCandidate, model.CreateLiveAgentPlanVersionInput) {
		t.Helper()
		anchor := now.Add(-2 * time.Hour)
		input := model.CreateLiveAgentPlanVersionInput{RoomID: room, DurationMinutes: 1, RoundMinutes: 1, Variants: []model.LiveAgentPlanVersionVariant{{VariantKey: "formal", IsFormal: true, Text: "哥哥姐姐们，我们家的油，您先看看。", AudioURL: "/test/original.wav", AudioAssetID: 101, GenerationNo: 1}}}
		variants, _ := json.Marshal(input.Variants)
		exec(`INSERT INTO core_rooms(id,tenant_id,name) VALUES(?,7,'隔离调度测试')`, room)
		exec(`INSERT INTO live_runtime_sessions(id,external_id,tenant_id,room_id,status,execution_realm,started_at,last_billed_at) VALUES(?,?,7,?,'running','refresh-test',?,?)`, room, fmt.Sprintf("refresh-%d", room), room, anchor, anchor)
		exec(`INSERT INTO live_agent_plan_room_bindings(tenant_id,room_id,plan_id,status) VALUES(7,?,?,'active')`, room, room)
		exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES(7,?,?)`, room, room)
		exec(`INSERT INTO live_agent_plan_versions(id,tenant_id,room_id,plan_id,version_no,lifecycle_status,voice_identity_json,variants_json,generation_context_json,published_at) VALUES(?,7,?,?,1,'published','{}',?,'{}',?)`, room*1000, room, room, string(variants), anchor)
		exec(`INSERT INTO live_agent_room_plan_publications(tenant_id,room_id,plan_id,version_id,published_at) VALUES(7,?,?,?,?)`, room, room, room*1000, anchor)
		exec(`INSERT INTO live_room_content_access(tenant_id,room_id,selected_mode,dynamic_authorized,updated_by_user_id) VALUES(7,?,'ai_dynamic',1,1)`, room)
		return model.LiveContentRefreshCandidate{TenantID: 7, RoomID: room, PlanID: room, BaseVersionID: room * 1000, RuntimeSessionID: room, ExecutionRealm: "refresh-test", StartedAt: anchor, PublishedAt: anchor}, input
	}
	create := func(c model.LiveContentRefreshCandidate) model.LiveContentRefreshJob {
		t.Helper()
		j, created, err := s.EnsureLiveContentRefreshJob(ctx, c, now)
		if err != nil || !created || j.ID <= 0 {
			t.Fatalf("ensure job: %+v %v %v", j, created, err)
		}
		return j
	}
	claim := func(at time.Time) model.LiveContentRefreshJob {
		t.Helper()
		j, found, err := s.ClaimLiveContentRefreshJob(ctx, "generator", "refresh-test", at, time.Minute)
		if err != nil || !found {
			t.Fatalf("claim: %+v %v %v", j, found, err)
		}
		return j
	}
	t.Run("durable preparation and boundary publication", func(t *testing.T) {
		c, input := seed(11)
		j := create(c)
		if _, created, err := s.EnsureLiveContentRefreshJob(ctx, c, now); err != nil || created {
			t.Fatal("duplicate created", err)
		}
		if _, found, err := s.ClaimLiveContentRefreshJob(ctx, "too-early", "refresh-test", j.ScheduledAt.Add(-time.Millisecond), time.Minute); err != nil || found {
			t.Fatal("generated before preparation time", err)
		}
		job := claim(now.Add(-5 * time.Minute))
		if _, found, err := s.ClaimLiveContentRefreshJob(ctx, "second", "refresh-test", now.Add(-5*time.Minute), time.Minute); err != nil || found {
			t.Fatal("double lease", err)
		}
		input.Variants[0].GenerationNo++
		input.Variants[0].AudioAssetID = 102
		if err := s.TrackLiveContentRefreshAsset(ctx, job, 102, now.Add(-5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveLiveContentRefreshDraft(ctx, job, input, now.Add(-5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		reclaimed := claim(now.Add(-3 * time.Minute))
		if reclaimed.LeaseToken == job.LeaseToken || reclaimed.Draft == nil || reclaimed.Draft.Variants[0].GenerationNo != 2 || len(reclaimed.GeneratedAssets) != 1 {
			t.Fatal("checkpoint or ownership lost on restart", reclaimed)
		}
		if err := s.SaveLiveContentRefreshDraft(ctx, job, input, now.Add(-3*time.Minute)); !errors.Is(err, ErrLiveContentRefreshLeaseLost) {
			t.Fatal("old lease accepted", err)
		}
		version, err := s.CommitLiveContentRefresh(ctx, reclaimed, input, now.Add(-3*time.Minute))
		if err != nil || version.LifecycleStatus != "prepared" {
			t.Fatal(version, err)
		}
		if scalar(`SELECT version_id FROM live_agent_room_plan_publications WHERE tenant_id=7 AND room_id=11`) != c.BaseVersionID || scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE id=? AND lifecycle_status='published'`, c.BaseVersionID) != 1 {
			t.Fatal("prepared result published before Core boundary")
		}
		listed, err := s.ListLiveAgentPlanVersions(ctx, 7, c.PlanID, c.RoomID)
		if err != nil || len(listed) != 1 || listed[0].ID != c.BaseVersionID {
			t.Fatal("prepared leaked into editor versions", listed, err)
		}
		if _, err = s.PublishLiveAgentPlanVersion(ctx, 7, c.PlanID, c.RoomID, version.ID, 900); !errors.Is(err, ErrLiveAgentPlanVersionNotFound) {
			t.Fatal("manual publish bypassed prepared TTL", err)
		}
		if _, created, err := s.EnsureLiveContentRefreshJob(ctx, c, now); err != nil || created {
			t.Fatal("another job created while ready", err)
		}
		if _, found, err := s.ClaimReadyLiveContentRefreshJob(ctx, "ready", "refresh-test", now.Add(-time.Millisecond), time.Minute); err != nil || found {
			t.Fatal("ready claimed before TTL", err)
		}
		ready, found, err := s.ClaimReadyLiveContentRefreshJob(ctx, "ready", "refresh-test", now, time.Minute)
		if err != nil || !found || ready.ResultVersionID != version.ID {
			t.Fatal(ready, found, err)
		}
		if err := s.DeferLiveContentRefreshJob(ctx, ready, now, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		ready, found, err = s.ClaimReadyLiveContentRefreshJob(ctx, "ready-after-restart", "refresh-test", now.Add(6*time.Second), time.Minute)
		if err != nil || !found || ready.ResultVersionID != version.ID {
			t.Fatal(ready, found, err)
		}
		if err := s.CompleteLiveContentRefreshJob(ctx, ready, now.Add(6*time.Second)); err != nil {
			t.Fatal(err)
		}
		if scalar(`SELECT version_id FROM live_agent_room_plan_publications WHERE tenant_id=7 AND room_id=11`) != version.ID || scalar(`SELECT COUNT(*) FROM live_content_refresh_events WHERE job_id=? AND action IN ('prepared','applied')`, job.ID) != 2 {
			t.Fatal("application not atomically published/audited")
		}
		status, err := s.GetLatestLiveContentRefreshStatus(ctx, 7, 11)
		if err != nil || status["status"] != "completed" || len(status) != 8 {
			t.Fatal("unsafe or missing operator status", status, err)
		}
		items, err := s.ListLiveContentRefreshCandidatesAfter(ctx, "refresh-test", 0, 1)
		if err != nil || len(items) != 1 || items[0].BaseVersionID != version.ID {
			t.Fatal(items, err)
		}
		prepare, apply := refreshTimes(items[0], model.DefaultLiveContentPolicy())
		if !prepare.Equal(now.Add(110*time.Minute+6*time.Second)) || !apply.Equal(now.Add(2*time.Hour+6*time.Second)) {
			t.Fatal("next TTL did not use actual application", prepare, apply)
		}
	})

	for index, mutation := range []string{"facts", "stop", "delete", "revoke", "manual_publish"} {
		t.Run("cancel "+mutation, func(t *testing.T) {
			room := int64(20 + index)
			c, input := seed(room)
			create(c)
			job := claim(now)
			version, err := s.CommitLiveContentRefresh(ctx, job, input, now)
			if err != nil {
				t.Fatal(err)
			}
			ready, found, err := s.ClaimReadyLiveContentRefreshJob(ctx, "ready", "refresh-test", now, time.Minute)
			if err != nil || !found || ready.ID != job.ID {
				t.Fatal(ready, found, err)
			}
			switch mutation {
			case "facts":
				exec(`INSERT INTO live_agent_plan_facts(tenant_id,plan_id,fact_key,fact_value,source_quote) VALUES(7,?,'price','79','verified')`, room)
			case "stop":
				exec(`UPDATE live_runtime_sessions SET status='stopped' WHERE id=?`, room)
			case "delete":
				if err := s.QueueRoomDeletion(ctx, 7, room, 900); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				exec(`UPDATE live_room_content_access SET dynamic_authorized=0 WHERE room_id=?`, room)
			case "manual_publish":
				exec(`UPDATE live_agent_plan_versions SET lifecycle_status='superseded' WHERE id=?`, c.BaseVersionID)
				exec(`INSERT INTO live_agent_plan_versions(tenant_id,room_id,plan_id,version_no,lifecycle_status,voice_identity_json,variants_json,generation_context_json,published_at) VALUES(7,?,?,3,'published','{}','[]','{}',?)`, room, room, now)
			}
			if err := s.ValidateLiveContentRefreshJob(ctx, ready, now); !errors.Is(err, ErrLiveContentRefreshObsolete) {
				t.Fatal("stale ready survived", err)
			}
			clean, found, err := s.ClaimCancelledLiveContentRefreshJob(ctx, "cleaner", "refresh-test", now.Add(time.Minute), time.Minute)
			if err != nil || !found || clean.ID != job.ID {
				t.Fatal("cancellation outbox missing", clean, found, err)
			}
			if err := s.AcknowledgeLiveContentRefreshCancellation(ctx, clean, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			if scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE id=? AND lifecycle_status='abandoned'`, version.ID) != 1 || scalar(`SELECT version_id FROM live_agent_room_plan_publications WHERE tenant_id=7 AND room_id=?`, room) != c.BaseVersionID {
				t.Fatal("cancellation modified original publication")
			}
			listed, err := s.ListLiveAgentPlanVersionsForPlan(ctx, 7, c.PlanID)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range listed {
				if v.ID == version.ID {
					t.Fatal("cancelled preparation leaked into reusable versions")
				}
			}
		})
	}

	t.Run("bounded paid attempts and pause deferral", func(t *testing.T) {
		c, _ := seed(30)
		create(c)
		job := claim(now)
		if err := s.DeferLiveContentRefreshJob(ctx, job, now, time.Second); err != nil {
			t.Fatal(err)
		}
		if scalar(`SELECT attempts FROM live_content_refresh_jobs WHERE id=?`, job.ID) != 0 {
			t.Fatal("paused audio consumed paid retry budget")
		}
		at := now.Add(time.Second)
		for i := 0; i < maxRefreshGenerationAttempts; i++ {
			job = claim(at)
			if err := s.FailLiveContentRefreshJob(ctx, job, "provider unavailable", at); err != nil {
				t.Fatal(err)
			}
			at = at.Add(10 * time.Minute)
		}
		if _, found, err := s.ClaimLiveContentRefreshJob(ctx, "over-budget", "refresh-test", at, time.Minute); err != nil || found {
			t.Fatal("unbounded paid attempts", err)
		}
		clean, found, err := s.ClaimCancelledLiveContentRefreshJob(ctx, "cleaner", "refresh-test", at, time.Minute)
		if err != nil || !found {
			t.Fatal(clean, found, err)
		}
		if err := s.AcknowledgeLiveContentRefreshCancellation(ctx, clean, at); err != nil {
			t.Fatal(err)
		}
		if _, created, err := s.EnsureLiveContentRefreshJob(ctx, c, at); err != nil || created {
			t.Fatal("same exhausted source scheduled again", err)
		}
	})

	t.Run("concurrent claims and changed-fact CAS", func(t *testing.T) {
		c, input := seed(40)
		create(c)
		var wg sync.WaitGroup
		jobs := make(chan model.LiveContentRefreshJob, 2)
		errorsCh := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				job, found, err := s.ClaimLiveContentRefreshJob(ctx, "parallel", "refresh-test", now, time.Minute)
				if err != nil {
					errorsCh <- err
				}
				if found {
					jobs <- job
				}
			}()
		}
		wg.Wait()
		close(jobs)
		close(errorsCh)
		for err := range errorsCh {
			t.Fatal(err)
		}
		if len(jobs) != 1 {
			t.Fatal("same job acquired by two workers", len(jobs))
		}
		job := <-jobs
		exec(`INSERT INTO live_agent_plan_facts(tenant_id,plan_id,fact_key,fact_value,source_quote) VALUES(7,40,'stock','0','verified')`)
		if _, err := s.CommitLiveContentRefresh(ctx, job, input, now); !errors.Is(err, ErrLiveContentRefreshObsolete) {
			t.Fatal("stale facts committed", err)
		}
		if scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE room_id=40`) != 1 {
			t.Fatal("CAS inserted unsafe audio version")
		}
		if err := s.CancelInactiveLiveContentRefreshJobs(ctx, "refresh-test", now); err != nil {
			t.Fatal("inactive cleanup SQL invalid", err)
		}
		items, err := s.ListLiveContentRefreshCandidatesAfter(ctx, "refresh-test", 30, 1)
		if err != nil || len(items) != 1 || items[0].RoomID != 40 {
			t.Fatal("cursor cannot reach later rooms", items, err)
		}
	})
}
