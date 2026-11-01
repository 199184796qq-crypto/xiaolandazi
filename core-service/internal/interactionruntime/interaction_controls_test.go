package interactionruntime

import (
	"fmt"
	"testing"
	"time"
)

func TestControlsCancelUnplayedKeepPlayingAndRejectLateModels(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "chat", "u", "chat", "哪里发货", base)
	job := e.Jobs(base.Add(time.Second), 10)[0]
	e.SetPolicy(s, Policy{Active: true, Disabled: map[string]bool{"semantic": true, "urgent": true}, Enabled: map[string]bool{"semantic": false, "urgent": false}}, base.Add(time.Second))
	if e.HasPreparingWork([]Work{job}, base.Add(2*time.Second)) {
		t.Fatal("revoked batch still submitted to model")
	}
	if e.Accept(job, Content{Body: "迟到回答", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(2*time.Second)) {
		t.Fatal("late generation revived disabled work")
	}
	e.SetPolicy(s, Policy{Active: true}, base.Add(3*time.Second))
	if len(e.Jobs(base.Add(3*time.Second), 10)) != 0 {
		t.Fatal("re-enable revived cancelled work")
	}
	r := e.rooms[s.Room]
	for _, kind := range []string{"welcome", "like", "follow", "order", "guide", "semantic", "urgent", "dialogue"} {
		for _, state := range []string{"collecting", "generating", "body_ready", "selected", "playing"} {
			id := kind + state
			r.Tasks[id] = &Task{Work: Work{ID: id, Kind: kind}, Decision: id, State: state}
		}
	}
	disabled := map[string]bool{}
	enabledMap := map[string]bool{}
	for _, kind := range []string{"welcome", "like", "follow", "order", "guide", "semantic", "urgent", "dialogue", "engagement"} {
		disabled[kind] = true
		enabledMap[kind] = false
	}
	e.SetPolicy(s, Policy{Active: false, Disabled: disabled, Enabled: enabledMap}, base.Add(4*time.Second))
	for _, task := range e.Snapshot(s.Room) {
		if task.State == "playing" {
			continue
		}
		if task.State != "cancelled" {
			t.Fatal("unplayed task survived", task)
		}
	}
	if got := e.Jobs(base.Add(5*time.Second), 100); len(got) != 0 {
		t.Fatal("disabled schedulers produced jobs", got)
	}
}

func TestConfiguredWelcomeFloorAndZeroAdaptive(t *testing.T) {
	for _, floor := range []time.Duration{0, 180 * time.Second} {
		t.Run(fmt.Sprint(floor), func(t *testing.T) {
			e := newWelcomeEngine()
			s := scope(29)
			e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"guide": false}, Minimum: map[string]time.Duration{"welcome": floor}}, base)
			add(e, s, "one", "one", "member", "", base)
			job := e.Jobs(base, 10)[0]
			welcomeBody(t, e, job, base)
			first := selectTask(t, e, s, base)
			e.ReadyWelcome(s.Room, first.Work.ID, base)
			e.Receipts(s.Room, first.Work.ID, "PLAYING", base)
			e.Receipts(s.Room, first.Work.ID, "COMPLETED", base.Add(time.Second))
			at := base.Add(10 * time.Second)
			add(e, s, "two", "two", "member", "", at)
			jobs := e.Jobs(at, 10)
			if floor == 0 && len(jobs) != 1 {
				t.Fatal("zero did not retain cold-room immediate welcome", jobs)
			}
			if floor > 0 && len(jobs) != 0 {
				t.Fatal("welcome bypassed configured floor", jobs)
			}
			if floor > 0 {
				at = base.Add(floor)
				add(e, s, "three", "three", "member", "", at)
				if len(e.Jobs(at, 10)) != 1 {
					t.Fatal("floor not released")
				}
			}
		})
	}
}

func TestConfiguredLikeAndFollowFloorBeginsAtPlayback(t *testing.T) {
	for _, kind := range []string{"like", "follow"} {
		t.Run(kind, func(t *testing.T) {
			e := newWelcomeEngine()
			s := scope(29)
			e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"guide": false}, Minimum: map[string]time.Duration{kind: 120 * time.Second}}, base)
			add(e, s, "a", "a", kind, "", base)
			job := behaviorJobs(e, base)[0]
			acceptBehavior(t, e, job, base)
			v := selectTask(t, e, s, base)
			e.Receipts(s.Room, v.Work.ID, "PLAYING", base)
			e.Receipts(s.Room, v.Work.ID, "COMPLETED", base.Add(time.Second))
			add(e, s, "b", "b", kind, "", base.Add(119*time.Second))
			if len(behaviorJobs(e, base.Add(119*time.Second))) != 0 {
				t.Fatal("played twice before floor")
			}
			if len(behaviorJobs(e, base.Add(120*time.Second))) != 1 {
				t.Fatal("floor did not release")
			}
		})
	}
}

func TestSemanticDisabledStillFindsUrgentWithoutPlayingOrdinary(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true, Disabled: map[string]bool{"semantic": true}, Enabled: map[string]bool{"semantic": false, "question": false, "chat": false}}, base)
	add(e, s, "a", "a", "chat", "售后急事", base)
	job := e.Jobs(base.Add(time.Second), 10)[0]
	if !e.Accept(job, Content{Body: "马上处理。", Intent: Intent{Relevant: true, Question: true, Type: "投诉", Priority: "紧急", PriorityReason: "紧急售后"}}, nil, base.Add(time.Second)) {
		t.Fatal("urgent classification was blocked with semantic off")
	}
	if got := selectTask(t, e, s, base.Add(time.Second)); got.Work.Kind != "urgent" {
		t.Fatal(got)
	}
	add(e, s, "b", "b", "chat", "一般问题", base.Add(2*time.Second))
	job = e.Jobs(base.Add(3*time.Second), 10)[0]
	if e.Accept(job, Content{Body: "普通答案", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(3*time.Second)) {
		t.Fatal("ordinary semantic response played while off")
	}
}

func TestConfiguredMinimumRecheckedAfterSelection(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "a", "a", "chat", "问题", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	e.rooms[s.Room].Starts = map[string]time.Time{"semantic": base}
	e.SetPolicy(s, Policy{Active: true, Minimum: map[string]time.Duration{"semantic": 120 * time.Second}}, base.Add(2*time.Second))
	if e.ReserveAudio(s.Room, v.Work.ID, 1000, base.Add(3*time.Second)) {
		t.Fatal("selected old task bypassed hot minimum")
	}
}

func TestControlsGenerationRetiresWorkEvenWhenOffPushWasSkipped(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: true, ControlsRevision: 1}, base)
	add(e, s, "q", "u", "chat", "哪里发货", base)
	job := e.Jobs(base.Add(time.Second), 10)[0]
	e.SetPolicy(s, Policy{Active: true, ControlsRevision: 3, DisabledAt: map[string]uint64{"semantic": 2}}, base.Add(2*time.Second))
	if e.Accept(job, Content{Body: "旧回答", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(3*time.Second)) {
		t.Fatal("missed off push revived old model work")
	}
	e.SetPolicy(s, Policy{Active: true, ControlsRevision: 4, Disabled: map[string]bool{"semantic": true}, DisabledAt: map[string]uint64{"semantic": 4}, Enabled: map[string]bool{"semantic": false, "question": false, "chat": false}}, base.Add(4*time.Second))
	add(e, s, "q2", "u2", "chat", "普通问题", base.Add(4*time.Second))
	job = e.Jobs(base.Add(5*time.Second), 10)[0]
	e.SetPolicy(s, Policy{Active: true, ControlsRevision: 5, DisabledAt: map[string]uint64{"semantic": 4}}, base.Add(6*time.Second))
	if e.Accept(job, Content{Body: "关闭时采集的普通回答", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(7*time.Second)) {
		t.Fatal("urgent-only analysis became ordinary playback after re-enable")
	}
}
