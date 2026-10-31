package interactionruntime

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func welcomeBody(t *testing.T, e *Engine, w Work, at time.Time) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"protocol": Protocol, "task": w.ID, "epoch": w.Scope.Epoch, "revision": w.Revision, "tenant_id": w.Scope.Tenant, "room_id": w.Scope.Room, "plan_id": w.Scope.Plan, "expires_at": w.Expires, "body": "欢迎新来的朋友。"})
	intent := Intent{Relevant: true}
	if semanticKind(w.Kind) {
		intent.Question = true
		intent.Type = "提问"
	}
	if !e.Accept(w, Content{Body: "欢迎新来的朋友。", Intent: intent, Knowledge: raw}, nil, at) {
		t.Fatal("body rejected", w)
	}
}

func TestWelcomePeriodMatrixAndBoundaries(t *testing.T) {
	for row, s := range []int{0, 5, 11, 21} {
		for col, entries := range []int{1, 6, 31} {
			want := [4][3]int{{0, 30, 30}, {60, 120, 180}, {120, 180, 300}, {180, 300, 300}}[row][col]
			t.Run(fmt.Sprintf("%d-%d", entries, s), func(t *testing.T) {
				if got := welcomeInterval(entries, s); got != time.Duration(want)*time.Second {
					t.Fatal(got, want)
				}
			})
		}
	}
	for _, v := range []struct {
		e, s int
		d    time.Duration
	}{{5, 4, 0}, {30, 10, 120 * time.Second}, {31, 10, 180 * time.Second}, {31, 20, 300 * time.Second}} {
		if welcomeInterval(v.e, v.s) != v.d {
			t.Fatal(v)
		}
	}
}

func TestWelcomeLatestThirtySecondsDistinctPeopleSingleTask(t *testing.T) {
	for _, n := range []int{1, 2, 10, 14} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			e := newWelcomeEngine()
			s := scope(29)
			now := base.Add(time.Minute)
			for i := 0; i < n; i++ {
				at := now.Add(-time.Duration(n-i) * time.Second)
				e.Add(s, Event{ID: fmt.Sprintf("e%d", i), UID: fmt.Sprintf("u%d", i), Name: "同名", Kind: "member", At: at}, now)
			}
			e.Add(s, Event{ID: "repeat", UID: "u0", Name: "最新昵称", Kind: "member", At: now}, now)
			e.Add(s, Event{ID: "stale", UID: "stale", Kind: "member", At: now.Add(-30 * time.Second)}, now)
			e.Add(s, Event{ID: "anonymous", Name: "同名", Kind: "member", At: now}, now)
			if len(e.Snapshot(s.Room)) != 0 {
				t.Fatal("arrival created a playback task")
			}
			jobs := e.Jobs(now, 10)
			if len(jobs) != 1 || jobs[0].Count != min(n, 10) || len(jobs[0].People) != min(n, 10) || !jobs[0].WelcomeQuota {
				t.Fatal(jobs)
			}
			if jobs[0].People[0].UID != "u0" || jobs[0].People[0].Name != "最新昵称" {
				t.Fatal("not newest distinct person", jobs)
			}
			if len(e.Jobs(now, 10)) != 0 {
				t.Fatal("duplicate in-flight welcome")
			}
		})
	}
}

func TestWelcomeEntitlementSurvivesRepeatedExpiryUntilActualPlayback(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	now := base
	for attempt := 0; attempt < 4; attempt++ {
		add(e, s, fmt.Sprint(attempt), fmt.Sprintf("new%d", attempt), "member", "", now)
		jobs := e.Jobs(now, 10)
		if len(jobs) != 1 {
			t.Fatal("missing retry", attempt, e.Snapshot(29))
		}
		welcomeBody(t, e, jobs[0], now)
		chosen := selectTask(t, e, s, now)
		if !e.rooms[29].Welcome.Pending || !e.rooms[29].Welcome.LastStart.IsZero() {
			t.Fatal("spent before playback")
		}
		ready, ok := e.ReadyWelcome(29, chosen.Work.ID, now.Add(time.Second))
		if !ok {
			t.Fatal("ready rejected")
		}
		again, ok := e.ReadyWelcome(29, chosen.Work.ID, now.Add(5*time.Second))
		if !ok || !again.Work.Expires.Equal(ready.Work.Expires) {
			t.Fatal("ready retry renewed deadline")
		}
		if attempt == 3 {
			start := now.Add(10 * time.Second)
			e.Receipts(29, chosen.Work.ID, "PLAYING", start)
			e.Receipts(29, chosen.Work.ID, "PROGRESS", start.Add(time.Second))
			if e.rooms[29].Welcome.Pending || !e.rooms[29].Welcome.LastStart.Equal(start) {
				t.Fatal("quota not spent exactly once")
			}
			e.Receipts(29, chosen.Work.ID, "COMPLETED", start.Add(35*time.Second))
			if e.rooms[29].Tasks[chosen.Work.ID].State != "completed" {
				t.Fatal("cut off started audio at TTL")
			}
			return
		}
		expired := ready.Work.Expires
		if e.ReserveAudio(29, chosen.Work.ID, 3000, expired) {
			t.Fatal("allowed playback at deadline")
		}
		e.Jobs(expired, 10)
		if !e.rooms[29].Welcome.Pending || !e.rooms[29].Welcome.LastStart.IsZero() {
			t.Fatal("expiry spent quota")
		}
		if len(e.Jobs(expired.Add(40*time.Second), 10)) != 0 {
			t.Fatal("replayed stale people")
		}
		now = expired.Add(45 * time.Second)
	}
}

func TestWelcomeYieldsToDialogueAndPlayingButPreemptsUnclaimedQuestion(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "q", "old", "chat", "问题", base)
	jobs := e.Jobs(base.Add(time.Second), 1)
	welcomeBody(t, e, jobs[0], base.Add(time.Second))
	old := selectTask(t, e, s, base.Add(time.Second))
	add(e, s, "entry", "new", "member", "", base.Add(2*time.Second))
	jobs = e.Jobs(base.Add(2*time.Second), 1)
	if len(jobs) != 1 || jobs[0].Kind != "welcome" {
		t.Fatal("could not prepare ahead of a waiting question", jobs)
	}
	welcomeBody(t, e, jobs[0], base.Add(3*time.Second))
	chosen := selectTask(t, e, s, base.Add(3*time.Second))
	if chosen.Work.Kind != "welcome" || e.rooms[29].Tasks[old.Work.ID].State != "body_ready" {
		t.Fatal("did not preserve deferred question")
	}
	if ids := e.Cancelled(29); len(ids) != 1 || ids[0] != old.Work.ID {
		t.Fatal("old queue reservation not removed", ids)
	}
	e.ReadyWelcome(29, chosen.Work.ID, base.Add(4*time.Second))
	e.Receipts(29, chosen.Work.ID, "PLAYING", base.Add(5*time.Second))
	if e.Select(s, base.Add(6*time.Second)) != nil {
		t.Fatal("overlapped playing audio")
	}
	if welcomeSupersedes(chosen, &Task{Work: Work{Kind: "dialogue", Priority: band("dialogue")}}) {
		t.Fatal("welcome preempted a dialogue")
	}
	if welcomeSupersedes(chosen, &Task{Work: Work{Kind: "urgent", Priority: band("urgent")}}) {
		t.Fatal("welcome preempted urgent complaint")
	}
}

func TestWelcomeDisabledOrRemovedUserCannotReenterFromOldPool(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "e", "u", "member", "", base)
	w := e.Jobs(base, 1)[0]
	welcomeBody(t, e, w, base)
	v := selectTask(t, e, s, base)
	e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"welcome": false}}, base.Add(time.Second))
	if _, ok := e.ReadyWelcome(29, v.Work.ID, base.Add(2*time.Second)); ok {
		t.Fatal("disabled welcome admitted")
	}
	e.Jobs(base.Add(2*time.Second), 1)
	e.CancelUser(s, "u", base.Add(3*time.Second))
	e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"guide": false}}, base.Add(4*time.Second))
	if len(e.Jobs(base.Add(10*time.Second), 1)) != 0 {
		t.Fatal("removed user returned from old welcome pool")
	}
	other := scope(33)
	e.SetPolicy(other, Policy{Active: true}, base)
	add(e, other, "same-id", "u", "member", "", base.Add(10*time.Second))
	jobs := e.Jobs(base.Add(10*time.Second), 1)
	if len(jobs) != 1 || jobs[0].Scope.Room != 33 {
		t.Fatal("room entitlement mixed", jobs)
	}
}

func TestWelcomeFiveMinuteCooldownAnchoredToRetryPlayback(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	now := base
	for i := 0; i < 40; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "member", "", now)
	}
	r := e.rooms[29]
	r.SemanticUsers = map[string]time.Time{}
	for i := 0; i < 21; i++ {
		r.SemanticUsers[fmt.Sprint(i)] = now
	}
	jobs := e.Jobs(now, 1)
	welcomeBody(t, e, jobs[0], now)
	v := selectTask(t, e, s, now)
	e.ReadyWelcome(29, v.Work.ID, now.Add(time.Second))
	e.Jobs(now.Add(31*time.Second), 1)
	add(e, s, "new", "new", "member", "", now.Add(40*time.Second))
	jobs = e.Jobs(now.Add(40*time.Second), 1)
	if len(jobs) != 1 {
		t.Fatal("waited five minutes after a failed welcome")
	}
	welcomeBody(t, e, jobs[0], now.Add(40*time.Second))
	v = selectTask(t, e, s, now.Add(40*time.Second))
	e.ReadyWelcome(29, v.Work.ID, now.Add(42*time.Second))
	start := now.Add(45 * time.Second)
	e.Receipts(29, v.Work.ID, "PLAYING", start)
	e.Receipts(29, v.Work.ID, "COMPLETED", start.Add(5*time.Second))
	// Keep the same busy matrix cell; new arrivals cannot accumulate quota debt.
	for second := 60; second < 345; second += 10 {
		at := now.Add(time.Duration(second) * time.Second)
		for uid := range r.SemanticUsers {
			r.SemanticUsers[uid] = at
		}
		for i := 0; i < 40; i++ {
			add(e, s, fmt.Sprintf("%d-%d", second, i), fmt.Sprintf("%d-%d", second, i), "member", "", at)
		}
		if len(e.Jobs(at, 1)) != 0 {
			t.Fatal("welcome before successful start plus five minutes", second)
		}
	}
	at := start.Add(5 * time.Minute)
	add(e, s, "due", "due", "member", "", at)
	if len(e.Jobs(at, 1)) != 1 {
		t.Fatal("missing due welcome")
	}
}

func TestWelcomeFailureBackoffDisabledAndEpochIsolation(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "one", "u", "member", "", base)
	job := e.Jobs(base, 1)[0]
	e.Accept(job, Content{}, fmt.Errorf("provider unavailable"), base)
	if len(e.Jobs(base.Add(time.Second), 1)) != 0 {
		t.Fatal("unbounded paid retry")
	}
	if len(e.Jobs(base.Add(6*time.Second), 1)) != 1 {
		t.Fatal("quota lost after model failure")
	}
	e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"welcome": false}}, base.Add(7*time.Second))
	if e.Select(s, base.Add(8*time.Second)) != nil {
		t.Fatal("disabled welcome selected")
	}
	s.Epoch = "next"
	e.SetPolicy(s, Policy{Active: true}, base.Add(9*time.Second))
	if len(e.Jobs(base.Add(10*time.Second), 1)) != 0 || e.rooms[29].Welcome != nil {
		t.Fatal("old entitlement crossed session")
	}
}

func TestWelcomePriorityAndReadyGate(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "q", "q", "chat", "问题", base)
	add(e, s, "entry", "n", "member", "", base)
	jobs := e.Jobs(base.Add(time.Second), 10)
	for _, w := range jobs {
		welcomeBody(t, e, w, base.Add(time.Second))
	}
	v := selectTask(t, e, s, base.Add(time.Second))
	if v.Work.Kind != "welcome" {
		t.Fatal("ordinary question outranked welcome", v)
	}
	if e.ReserveAudio(29, v.Work.ID, 3000, base.Add(2*time.Second)) {
		t.Fatal("unfinished welcome reserved audio")
	}
	if _, ok := e.ReadyWelcome(29, v.Work.ID, base.Add(2*time.Second)); !ok {
		t.Fatal("could not admit audio")
	}
	if !e.ReserveAudio(29, v.Work.ID, 3000, base.Add(3*time.Second)) {
		t.Fatal("ready audio not admitted")
	}
	if band("urgent") <= band("dialogue") || band("dialogue") <= band("welcome") || band("welcome") <= band("engagement") {
		t.Fatal("priority order")
	}
}

func newWelcomeEngine() *Engine {
	e := New()
	e.SetPolicy(scope(29), Policy{Active: true, Enabled: map[string]bool{"guide": false}}, base)
	return e
}
