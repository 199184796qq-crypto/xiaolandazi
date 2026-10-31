package interactionruntime

import (
	"fmt"
	"testing"
	"time"
)

func behaviorJobs(e *Engine, at time.Time) []Work {
	e.Batches(at, 0)
	var out []Work
	for _, b := range e.TemplateBatches(at, 10) {
		out = append(out, b.Works...)
	}
	return out
}
func acceptBehavior(t *testing.T, e *Engine, w Work, at time.Time) {
	t.Helper()
	if !e.Accept(w, Content{Body: "谢谢支持。", Intent: Intent{Relevant: true}}, nil, at) {
		t.Fatal("not accepted", w)
	}
}
func TestCompletedOrdersNeverEnterIntentOrSemanticQuota(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "1", "u", "chat", "我拍了不知道多久到", base)
	add(e, s, "2", "u", "chat", "已经买了", base.Add(time.Second))
	if got := e.Batches(base.Add(2*time.Second), 4); len(got) != 0 {
		t.Fatal("model was requested", got)
	}
	jobs := behaviorJobs(e, base.Add(2*time.Second))
	if len(jobs) != 1 || jobs[0].Count != 1 || jobs[0].Template != "order_single_v1" {
		t.Fatal(jobs)
	}
	if len(e.rooms[29].SemanticUsers) != 0 {
		t.Fatal("claim inflated semantic load")
	}
}
func TestOrderRetryKeepsOriginalWindowAndOnlyAcknowledgesFrozenReport(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "wang", "wang", "chat", "已下单", base)
	first := behaviorJobs(e, base)[0]
	acceptBehavior(t, e, first, base)
	v := selectTask(t, e, s, base)
	e.Batches(base.Add(30*time.Second), 0)
	for i := 0; i < 3; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", "买了", base.Add(35*time.Second))
	}
	if len(behaviorJobs(e, base.Add(39*time.Second))) != 0 {
		t.Fatal("ignored ten second retry delay")
	}
	jobs := behaviorJobs(e, base.Add(40*time.Second))
	if len(jobs) != 1 || jobs[0].Count != 4 || jobs[0].Template != "order_count_v1" {
		t.Fatal("lost original person", jobs)
	}
	// A second expiry still owes all four people beyond the initial 60s window.
	acceptBehavior(t, e, jobs[0], base.Add(40*time.Second))
	v = selectTask(t, e, s, base.Add(40*time.Second))
	e.Batches(base.Add(70*time.Second), 0)
	jobs = behaviorJobs(e, base.Add(80*time.Second))
	if len(jobs) != 1 || jobs[0].Count != 4 {
		t.Fatal("reset original window", jobs)
	}
	acceptBehavior(t, e, jobs[0], base.Add(80*time.Second))
	v = selectTask(t, e, s, base.Add(80*time.Second))
	add(e, s, "late", "late", "chat", "已买", base.Add(81*time.Second))
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(82*time.Second))
	e.Receipts(29, v.Work.ID, "PROGRESS", base.Add(83*time.Second))
	w := e.rooms[29].Behaviors["order"]
	if w.Pending || len(w.Seen) != 4 || len(w.Entries) != 1 || w.Entries["late"].UID == "" {
		t.Fatal("ack included an unheard new event", w)
	}
	e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(84*time.Second))
	add(e, s, "repeat", "wang", "chat", "拍了", base.Add(86*time.Second))
	jobs = behaviorJobs(e, base.Add(88*time.Second))
	if len(jobs) != 1 || jobs[0].Count != 1 || jobs[0].People[0].UID != "late" || jobs[0].Template != "order_single_v1" {
		t.Fatal("duplicate or dropped next batch", jobs)
	}
}
func TestOrderFreshWindowFailureAndEpoch(t *testing.T) {
	e := newWelcomeEngine()
	s := scope(29)
	add(e, s, "stale", "old", "chat", "已拍", base.Add(-time.Minute))
	add(e, s, "fresh", "fresh", "chat", "已买", base)
	job := behaviorJobs(e, base)[0]
	if job.Count != 1 {
		t.Fatal(job)
	}
	e.Accept(job, Content{}, fmt.Errorf("upstream unavailable"), base)
	if len(behaviorJobs(e, base.Add(time.Second))) != 0 {
		t.Fatal("retry loop")
	}
	jobs := behaviorJobs(e, base.Add(11*time.Second))
	if len(jobs) != 1 || jobs[0].Template != "order_count_v1" {
		t.Fatal(jobs)
	}
	s.Epoch = "next"
	e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"guide": false}}, base.Add(12*time.Second))
	if len(behaviorJobs(e, base.Add(13*time.Second))) != 0 {
		t.Fatal("old quota crossed epoch")
	}
}
func TestLikeFollowThirtySecondOriginalTTLAndIndependentCooldowns(t *testing.T) {
	for _, kind := range []string{"like", "follow"} {
		t.Run(kind, func(t *testing.T) {
			e := newWelcomeEngine()
			s := scope(29)
			add(e, s, "a", "a", kind, "", base)
			add(e, s, "dup", "a", kind, "", base.Add(time.Second))
			add(e, s, "b", "b", kind, "", base.Add(2*time.Second))
			jobs := behaviorJobs(e, base.Add(3*time.Second))
			if len(jobs) != 1 || jobs[0].Count != 2 || !jobs[0].Expires.Equal(base.Add(31*time.Second)) {
				t.Fatal(jobs)
			}
			acceptBehavior(t, e, jobs[0], base.Add(3*time.Second))
			v := selectTask(t, e, s, base.Add(3*time.Second))
			if e.ReserveAudio(29, v.Work.ID, 2000, base.Add(31*time.Second)) {
				t.Fatal("renewed source TTL")
			}
			e.Batches(base.Add(33*time.Second), 0)
			if len(behaviorJobs(e, base.Add(35*time.Second))) != 0 {
				t.Fatal("retained an owed like/follow quota")
			}
			add(e, s, "fresh", "c", kind, "", base.Add(40*time.Second))
			jobs = behaviorJobs(e, base.Add(40*time.Second))
			acceptBehavior(t, e, jobs[0], base.Add(40*time.Second))
			v = selectTask(t, e, s, base.Add(40*time.Second))
			e.Receipts(29, v.Work.ID, "PLAYING", base.Add(42*time.Second))
			e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(44*time.Second))
			period := behaviorInterval(kind, 0, 0)
			at := base.Add(42 * time.Second).Add(period - time.Second)
			add(e, s, "next", "d", kind, "", at)
			if len(behaviorJobs(e, at)) != 0 {
				t.Fatal("cooldown anchored to generation instead of playback")
			}
			if len(behaviorJobs(e, at.Add(time.Second))) != 1 {
				t.Fatal("cooldown missing")
			}
		})
	}
}
func TestBehaviorMatrixAndIsolation(t *testing.T) {
	likes := [4][3]int{{30, 60, 120}, {90, 180, 300}, {180, 300, 600}, {300, 600, 600}}
	follows := [4][3]int{{20, 45, 90}, {60, 120, 180}, {120, 180, 300}, {180, 300, 600}}
	for i, s := range []int{0, 5, 11, 21} {
		for j, e := range []int{0, 6, 31} {
			if behaviorInterval("like", e, s) != time.Duration(likes[i][j])*time.Second || behaviorInterval("follow", e, s) != time.Duration(follows[i][j])*time.Second {
				t.Fatal(i, j)
			}
		}
	}
	e := newWelcomeEngine()
	a, b := scope(29), scope(33)
	add(e, a, "same", "same", "like", "", base)
	add(e, b, "same", "same", "follow", "", base)
	jobs := behaviorJobs(e, base)
	if len(jobs) != 2 {
		t.Fatal(jobs)
	}
	e.CancelUser(a, "same", base.Add(time.Second))
	if len(e.rooms[29].Behaviors["like"].Entries) != 0 || len(e.rooms[33].Behaviors["follow"].Entries) != 1 {
		t.Fatal("mixed rooms")
	}
}

func TestHotLikeFollowCooldownUsesEntryAndSemanticDimensions(t *testing.T) {
	for _, kind := range []string{"like", "follow"} {
		t.Run(kind, func(t *testing.T) {
			e := newWelcomeEngine()
			s := scope(29)
			e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"guide": false, "welcome": false}}, base)
			r := e.rooms[29]
			r.SemanticUsers = map[string]time.Time{}
			feed := func(at time.Time) {
				for i := 0; i < 80; i++ {
					add(e, s, fmt.Sprintf("enter-%d-%d", at.Unix(), i), fmt.Sprint(i), "member", "", at)
				}
				for i := 0; i < 25; i++ {
					r.SemanticUsers[fmt.Sprint(i)] = at
				}
			}
			feed(base)
			add(e, s, "first", "first", kind, "", base)
			job := behaviorJobs(e, base)[0]
			acceptBehavior(t, e, job, base)
			v := selectTask(t, e, s, base)
			e.Receipts(29, v.Work.ID, "PLAYING", base.Add(time.Second))
			e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(3*time.Second))
			for second := 10; second <= 600; second += 10 {
				at := base.Add(time.Duration(second) * time.Second)
				feed(at)
				add(e, s, fmt.Sprint(second), fmt.Sprintf("new%d", second), kind, "", at)
				if len(behaviorJobs(e, at)) != 0 {
					t.Fatal("hot room thanked too often", second)
				}
			}
			at := base.Add(601 * time.Second)
			feed(at)
			if len(behaviorJobs(e, at)) != 1 {
				t.Fatal("ten minute cooldown missing")
			}
		})
	}
}
