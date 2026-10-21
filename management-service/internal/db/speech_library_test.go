package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	lib "livecompanion/management/internal/speechlibrary"
	"sync"
	"testing"
	"time"
)

func speechLibraryFixture(t *testing.T) (*Store, context.Context, lib.Scope) {
	t.Helper()
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	for _, query := range []string{`CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT)`, `INSERT INTO live_agent_plans VALUES(4,14)`, `CREATE TABLE core_rooms(id BIGINT PRIMARY KEY,tenant_id BIGINT)`, `INSERT INTO core_rooms VALUES(15,14),(16,14)`} {
		if _, e := s.db.ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.db.ExecContext(ctx, `CREATE TABLE live_runtime_sessions(id BIGINT PRIMARY KEY,tenant_id BIGINT,room_id BIGINT,status VARCHAR(20),ended_at DATETIME(3) NULL)`); e != nil {
		t.Fatal(e)
	}
	for i := 1; i <= 20; i++ {
		room := 15
		if i == 3 {
			room = 16
		}
		if _, e := s.db.ExecContext(ctx, `INSERT INTO live_runtime_sessions VALUES(?,14,?,'running',NULL)`, i, room); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.MigrateSpeechLibrary(ctx); e != nil {
		t.Fatal(e)
	}
	return s, ctx, lib.Scope{Realm: "fixture", Tenant: 14, Room: 15, Plan: 4, Runtime: 1, Slot: "规格×算账", TextContext: lib.Digest("facts-a"), VoiceContext: lib.Digest("voice-a"), RoomContext: lib.Digest("room-a")}
}
func libraryWrite(t *testing.T, s *Store, ctx context.Context, sc lib.Scope, p lib.Policy, n int) (lib.Entry, lib.Audio, string) {
	t.Helper()
	decision := lib.Digest(fmt.Sprint("fresh", n, sc.Runtime))
	track := fmt.Sprint("track-", sc.Runtime, "-", n)
	// Fresh choice is persisted once. Tests never contact a real model or OSS.
	fresh := p
	fresh.OneRate = 0
	fresh.TwoRate = 0
	fresh.FullRate = 0
	if _, e := s.Select(ctx, sc, fresh, decision, track); e != nil {
		t.Fatal(e)
	}
	entry := lib.Entry{ID: fmt.Sprintf("%032x", n), Candidate: anchorcycle.Candidate{ID: track, Text: fmt.Sprintf("第%d款产品开袋即可食用。", n), FinalContentAudit: &anchorcycle.FinalContentAudit{Decision: "通过"}}}
	entry.Candidate.FinalContentAudit.TextSHA = lib.Digest(entry.Candidate.Text)
	entry.Candidate.FinalContentAudit.ContextSHA, entry.Candidate.FinalContentAudit.ScopeSHA = lib.Digest("review"), sc.TextContext
	entry.Admission = lib.NewAdmission(sc, entry.Candidate, lib.ReviewPassed)
	audio := lib.Audio{ID: fmt.Sprintf("%032x", 100+n), EntryID: entry.ID, Bucket: "fixture-bucket", Prefix: lib.ObjectPrefix(sc, fmt.Sprintf("%032x", 100+n)), VoiceContext: sc.VoiceContext, Bundle: model.LiveAgentPlanVersionVariant{Text: entry.Candidate.Text, AudioDurationMS: 3400}}
	if e := s.ReserveAudio(ctx, sc, decision, track, audio); e != nil {
		t.Fatal(e)
	}
	if e := s.Publish(ctx, sc, p, decision, track, entry, audio); e != nil {
		t.Fatal(e)
	}
	return entry, audio, decision
}

func TestSpeechLibraryMySQLGenerationAdmissionSurvivesNextRuntime(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	p := lib.Policy{Capacity: 3, OneRate: 100, TwoRate: 100, FullRate: 100, CooldownSeconds: 0}
	decision, track := lib.Digest("generation-admission"), "accepted-no-final-audit"
	if _, err := s.Select(ctx, sc, p, decision, track); err != nil {
		t.Fatal(err)
	}
	c := anchorcycle.Candidate{ID: track, OutputText: "按当前商品资料选择规格。"}
	e := lib.Entry{ID: fmt.Sprintf("%032x", 51), Candidate: c, Admission: lib.NewAdmission(sc, c, lib.GenerationAccepted)}
	a := lib.Audio{ID: fmt.Sprintf("%032x", 151), EntryID: e.ID, Bucket: "fixture", Prefix: lib.ObjectPrefix(sc, fmt.Sprintf("%032x", 151)), VoiceContext: sc.VoiceContext, Bundle: model.LiveAgentPlanVersionVariant{Text: c.OutputText, AudioDurationMS: 3400}}
	if err := s.ReserveAudio(ctx, sc, decision, track, a); err != nil {
		t.Fatal(err)
	}
	if err := s.Publish(ctx, sc, p, decision, track, e, a); err != nil {
		t.Fatal("current accepted generation cannot enter durable library", err)
	}
	if err := s.MarkPlayed(ctx, sc.Tenant, sc.Room, sc.Runtime, track, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.ReleaseRuntime(ctx, sc.Tenant, sc.Room, sc.Runtime); err != nil {
		t.Fatal(err)
	}
	sc.Runtime++
	choice, err := s.Select(ctx, sc, p, lib.Digest("next-generation"), "next-track")
	if err != nil || choice.Entry == nil || choice.Reason != "text_hit" || choice.Entry.Candidate.FinalContentAudit != nil || choice.Entry.ValidateAdmission(sc, lib.GenerationAccepted) != nil {
		t.Fatal("next runtime lost honest admission", err, choice)
	}
	audio, err := s.ClaimAudio(ctx, sc, lib.Digest("next-generation"), e.ID)
	if err != nil || audio == nil || audio.ID != a.ID {
		t.Fatal("complete audio not reusable", err)
	}
	changed := *choice.Entry
	changed.Candidate.OutputText += "价格变更。"
	if err = s.Publish(ctx, sc, p, decision, track, changed, a); err == nil {
		t.Fatal("changed text reused old admission/audio")
	}
	changed = *choice.Entry
	changed.Admission.Mode = lib.ReviewPassed
	if err = s.Publish(ctx, sc, p, decision, track, changed, a); err == nil {
		t.Fatal("review mode bypassed real final audit")
	}
}

func TestSpeechLibraryMySQLDeletedSourceWaitsForAITerminal(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	p := lib.DefaultPolicy()
	entry, audio, _ := libraryWrite(t, s, ctx, sc, p, 1)
	if _, e := s.db.ExecContext(ctx, `DELETE FROM live_agent_plans WHERE id=4`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Due(ctx, 20); e != nil {
		t.Fatal(e)
	}
	var active, retired int
	s.db.QueryRowContext(ctx, `SELECT active FROM mgmt_speech_library_entries WHERE id=?`, entry.ID).Scan(&active)
	s.db.QueryRowContext(ctx, `SELECT retired FROM mgmt_speech_library_audio WHERE id=?`, audio.ID).Scan(&retired)
	if active != 1 || retired != 0 {
		t.Fatal("deleted source invalidated active AI's saved snapshot")
	}
	if _, e := s.db.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='stopped',ended_at=UTC_TIMESTAMP(3) WHERE room_id=15`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Due(ctx, 20); e != nil {
		t.Fatal(e)
	}
	s.db.QueryRowContext(ctx, `SELECT active FROM mgmt_speech_library_entries WHERE id=?`, entry.ID).Scan(&active)
	s.db.QueryRowContext(ctx, `SELECT retired FROM mgmt_speech_library_audio WHERE id=?`, audio.ID).Scan(&retired)
	if active != 0 || retired != 1 {
		t.Fatal("deleted source left immortal library assets")
	}
}
func TestSpeechLibraryMySQLRoomSnapshotVoiceAndPlayback(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	p := lib.Policy{Capacity: 3, OneRate: 100, TwoRate: 100, FullRate: 100, CooldownSeconds: 0}
	e, a, _ := libraryWrite(t, s, ctx, sc, p, 1)
	// Selection/adoption has not incremented use counts.
	var uses int
	if err := s.db.QueryRowContext(ctx, `SELECT use_count FROM mgmt_speech_library_entries WHERE id=?`, e.ID).Scan(&uses); err != nil || uses != 0 {
		t.Fatal("selected content counted as played", err, uses)
	}
	at := time.Now().Add(-10 * time.Minute)
	for i := 0; i < 3; i++ {
		if err := s.MarkPlayed(ctx, 14, 15, 1, "track-1-1", at); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReleaseLibraryTrack(ctx, 14, 15, 1, "track-1-1"); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRowContext(ctx, `SELECT use_count FROM mgmt_speech_library_entries WHERE id=?`, e.ID).Scan(&uses)
	if uses != 1 {
		t.Fatal("receipt was not idempotent", uses)
	}
	sc.Runtime = 2
	decision := lib.Digest("scene2")
	choice, err := s.Select(ctx, sc, p, decision, "track2")
	if err != nil || choice.Entry == nil || choice.Entry.ID != e.ID {
		t.Fatal("unchanged cross-session text not reused", choice, err)
	}
	hit, err := s.ClaimAudio(ctx, sc, decision, e.ID)
	if err != nil || hit == nil || hit.ID != a.ID {
		t.Fatal("unchanged full audio not reused", hit, err)
	}
	if again, err := s.ClaimAudio(ctx, sc, decision, e.ID); err != nil || again == nil {
		t.Fatal("audio claim was not idempotent", err)
	}
	// Restart is just another Store over the durable database.
	restarted := &Store{db: s.db}
	again, err := restarted.Select(ctx, sc, p, decision, "retry-track")
	if err != nil || again.TrackID != "track2" || again.Entry == nil {
		t.Fatal("retry changed the logical decision", again, err)
	}
	if err = s.Cancel(ctx, decision); err != nil {
		t.Fatal(err)
	}
	other := sc
	other.Runtime = 3
	other.Room = 16
	c, err := s.Select(ctx, other, p, lib.Digest("other-room"), "other")
	if err != nil || c.Entry != nil {
		t.Fatal("another room inherited the library", c, err)
	}
	voice := sc
	voice.Runtime = 4
	voice.VoiceContext = lib.Digest("new-voice")
	vd := lib.Digest("voice-only")
	c, err = s.Select(ctx, voice, p, vd, "voice-track")
	if err != nil || c.Entry == nil || c.Entry.ID != e.ID {
		t.Fatal("voice-only change discarded text", c, err)
	}
	hit, err = s.ClaimAudio(ctx, voice, vd, e.ID)
	if err != nil || hit != nil {
		t.Fatal("old voice survived a voice change", hit, err)
	}
	if err = s.ReserveAudio(ctx, sc, lib.Digest("late"), "late", a); !errors.Is(err, lib.ErrStale) {
		t.Fatal("old runtime could write into new scene", err)
	}
	changed := voice
	changed.Runtime = 5
	changed.TextContext = lib.Digest("facts-b")
	changed.RoomContext = lib.Digest("room-b")
	c, err = s.Select(ctx, changed, p, lib.Digest("facts-change"), "new")
	if err != nil || c.Entry != nil {
		t.Fatal("changed facts reused old text", c, err)
	}
	changed.Runtime = 6
	changed.TextContext = sc.TextContext
	changed.RoomContext = sc.RoomContext
	c, err = s.Select(ctx, changed, p, lib.Digest("facts-reverted"), "reverted")
	if err != nil || c.Entry != nil {
		t.Fatal("retired content resurrected when facts reverted", c, err)
	}
}
func TestSpeechLibraryMySQLFIFOAndActiveAudioReferences(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	p := lib.DefaultPolicy()
	var first lib.Audio
	for i := 1; i <= 4; i++ {
		_, a, _ := libraryWrite(t, s, ctx, sc, p, i)
		if i == 1 {
			first = a
		}
		time.Sleep(2 * time.Millisecond)
	}
	stats, err := s.Statistics(ctx)
	if err != nil || stats.Texts != 3 || stats.Audio != 3 {
		t.Fatal("bounded library exceeded capacity", stats, err)
	}
	// Force only retired audio due, while its queued run still holds a lease.
	s.db.ExecContext(ctx, `UPDATE mgmt_speech_library_audio SET delete_after_ms=1 WHERE retired=1`)
	items, err := s.Due(ctx, 20)
	if err != nil || len(items) != 0 {
		t.Fatal("queued audio became deletable", items, err)
	}
	s.db.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='paused' WHERE id=1`)
	items, err = s.Due(ctx, 20)
	if err != nil || len(items) != 0 {
		t.Fatal("pause released audio references", items, err)
	}
	s.db.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='stopped',ended_at=NOW() WHERE id=1`)
	items, err = s.Due(ctx, 20)
	if err != nil || len(items) != 1 || items[0].ID != first.ID {
		t.Fatal("terminal lifecycle failed to release retired audio", items, err)
	}
	if err = s.ForgetAudio(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	items, err = s.Due(ctx, 20)
	if err != nil || len(items) != 0 {
		t.Fatal("deleted object remained due", items, err)
	}
}
func TestSpeechLibraryMySQLConcurrentSelection(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	p := lib.Policy{Capacity: 3, OneRate: 100, TwoRate: 100, FullRate: 100, CooldownSeconds: 0}
	_, _, d := libraryWrite(t, s, ctx, sc, p, 1)
	if e := s.Cancel(ctx, d); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan lib.Choice, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, e := s.Select(ctx, sc, p, lib.Digest(fmt.Sprint("parallel", i)), fmt.Sprint("parallel", i))
			results <- c
			errs <- e
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	hits := 0
	for c := range results {
		if c.Entry != nil {
			hits++
		}
	}
	if hits != 1 {
		t.Fatal("same text concurrently scheduled multiple times", hits)
	}
}
func TestSpeechLibraryMySQLPolicyCASCapacityAndReceiptRelease(t *testing.T) {
	s, ctx, sc := speechLibraryFixture(t)
	p := lib.DefaultPolicy()
	raw, _ := json.Marshal(p)
	if _, e := s.db.ExecContext(ctx, `INSERT INTO mgmt_system_settings(setting_key,value_text) VALUES(?,?)`, lib.SettingKey, string(raw)); e != nil {
		t.Fatal(e)
	}
	next := p
	next.Capacity = 1
	if e := s.SaveSpeechLibraryPolicy(ctx, next, 27, lib.Digest(p)); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveSpeechLibraryPolicy(ctx, p, 27, lib.Digest(p)); !errors.Is(e, lib.ErrStale) {
		t.Fatal("stale settings overwrote newer configuration", e)
	}
	for i := 1; i <= 3; i++ {
		libraryWrite(t, s, ctx, sc, p, i)
		time.Sleep(2 * time.Millisecond)
	}
	_, e := s.Select(ctx, sc, next, lib.Digest("reduced-capacity"), "track-reduced")
	if e != nil {
		t.Fatal(e)
	}
	stats, e := s.Statistics(ctx)
	if e != nil || stats.Texts != 1 {
		t.Fatal("capacity reduction wasn't applied on next request", stats, e)
	}
	if e = s.MarkPlayed(ctx, 14, 15, 1, "track-1-3", time.Now()); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseLibraryBefore(ctx, 14, 15, 1, "next-actual-mainline", time.Now().Add(time.Second)); e != nil {
		t.Fatal(e)
	}
	var released bool
	if e = s.db.QueryRowContext(ctx, `SELECT released FROM mgmt_speech_library_leases WHERE track_id='track-1-3'`).Scan(&released); e != nil || !released {
		t.Fatal("actual next mainline did not release prior source audio", e)
	}
}
