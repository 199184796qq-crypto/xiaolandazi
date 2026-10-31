package interactionruntime

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestPoliciesPreventPaidJobsAndSeparateQuestionFromChat(t *testing.T) {
	e := New()
	s := scope(29)
	e.SetPolicy(s, Policy{Active: false}, base)
	add(e, s, "1", "u", "chat", "哪里发货", base)
	if len(e.Jobs(base.Add(time.Second), 10)) != 0 {
		t.Fatal("opening observation generated models")
	}
	e.SetPolicy(s, Policy{Active: true, Enabled: map[string]bool{"question": false, "chat": true}}, base)
	jobs := e.Jobs(base.Add(2*time.Second), 10)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	if e.Accept(jobs[0], Content{Body: "回答。", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(2*time.Second)) {
		t.Fatal("disabled question accepted")
	}
}

func TestPreparationFairnessAcrossBusyRooms(t *testing.T) {
	e := New()
	a, b := scope(29), scope(33)
	for i := 0; i < 10; i++ {
		add(e, a, fmt.Sprint(i), fmt.Sprint(i), "chat", "问题", base)
	}
	add(e, b, "x", "other", "chat", "另一间问题", base)
	jobs := e.Jobs(base.Add(time.Second), 2)
	if len(jobs) != 2 || jobs[0].Scope.Room == jobs[1].Scope.Room {
		t.Fatal(jobs)
	}
}

func TestActualAudioBudgetExpiryAndUrgentPreemption(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "1", "u", "chat", "问题", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	if e.ReserveAudio(29, v.Work.ID, 7000, base.Add(60*time.Second)) {
		t.Fatal("audio starts after original TTL")
	}
	if !e.ReserveAudio(29, v.Work.ID, 39000, base.Add(2*time.Second)) {
		t.Fatal("actual playable duration rejected")
	}
	e.Add(s, Event{ID: "urgent", UID: "u2", Kind: "chat", Text: "售后急事", Urgent: true, At: base.Add(2 * time.Second)}, base.Add(2*time.Second))
	ready(t, e, base.Add(2*time.Second), Intent{Relevant: true, Question: true})
	urgent := selectTask(t, e, s, base.Add(2*time.Second))
	if urgent.Work.Kind != "urgent" || len(e.Cancelled(29)) != 1 {
		t.Fatal(urgent)
	}
	if !e.ReserveAudio(29, urgent.Work.ID, 45000, base.Add(2*time.Second)) {
		t.Fatal("urgent budget must bypass normal cap")
	}
	e.Receipts(29, urgent.Work.ID, "PLAYING", base.Add(2*time.Second))
	e.Lost(29, urgent.Work.ID, base.Add(3*time.Second))
	for _, task := range e.Snapshot(29) {
		if task.State == "playing" || task.State == "selected" {
			t.Fatal("lost item blocked lane", task)
		}
	}
}

func TestNamedCooldownCommittedOnlyWhenAudioPlays(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "1", "u", "chat", "问题", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	e.Lost(29, v.Work.ID, base.Add(2*time.Second))
	add(e, s, "2", "u", "chat", "问题", base.Add(2*time.Second))
	ready(t, e, base.Add(4*time.Second), Intent{Relevant: true})
	v = selectTask(t, e, s, base.Add(4*time.Second))
	if v.Work.People[0].Name == "" {
		t.Fatal("unplayed name consumed cooldown")
	}
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(5*time.Second))
	e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(6*time.Second))
	add(e, s, "3", "u", "chat", "问题", base.Add(7*time.Second))
	ready(t, e, base.Add(11*time.Second), Intent{Relevant: true})
	v = selectTask(t, e, s, base.Add(11*time.Second))
	if v.Work.People[0].Name != "" {
		t.Fatal("name repeated within actual-play cooldown")
	}
}

func TestRollingAudioBudgetCountsOnlyOverlap(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "1", "u", "chat", "问题", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(2*time.Second))
	e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(32*time.Second))
	add(e, s, "2", "u2", "chat", "问题", base.Add(70*time.Second))
	ready(t, e, base.Add(72*time.Second), Intent{Relevant: true})
	v = selectTask(t, e, s, base.Add(72*time.Second))
	// Previous 30-second audio overlaps this minute for only 20 seconds.
	if !e.ReserveAudio(29, v.Work.ID, 10000, base.Add(72*time.Second)) {
		t.Fatal("counted old audio outside rolling minute")
	}
	if e.ReserveAudio(29, v.Work.ID, 11000, base.Add(72*time.Second)) {
		t.Fatal("allowed actual duration over remaining budget")
	}
}

var base = time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)

func scope(room int64) Scope { return Scope{14, room, 7, "boot:7:1"} }
func add(e *Engine, s Scope, id, uid, kind, text string, at time.Time) {
	e.Add(s, Event{ID: id, UID: uid, Name: uid, Kind: kind, Text: text, At: at}, at)
}
func ready(t *testing.T, e *Engine, now time.Time, intent Intent) []Work {
	t.Helper()
	jobs := e.Jobs(now, 10)
	for _, w := range jobs {
		if !e.Accept(w, Content{Body: "正文。", Intent: intent}, nil, now) {
			t.Fatal("accept", w)
		}
	}
	return jobs
}
func selectTask(t *testing.T, e *Engine, s Scope, at time.Time) *Task {
	t.Helper()
	v := e.Select(s, at)
	if v == nil {
		t.Fatal("missing selected task", e.Snapshot(s.Room))
	}
	e.Bind(v.Work, v.Work.ID, at)
	return v
}
func TestEightSourcesAndStableUID(t *testing.T) {
	for _, kind := range []string{"member", "like", "follow", "order", "chat"} {
		t.Run(kind, func(t *testing.T) {
			e := New()
			s := scope(29)
			add(e, s, "1", "u", kind, "问题", base)
			jobs := ready(t, e, base.Add(time.Second), Intent{Relevant: true})
			if len(jobs) != 1 {
				t.Fatal(jobs)
			}
			v := selectTask(t, e, s, base.Add(time.Second))
			if len(v.Work.People) != 1 || v.Work.People[0].UID != "u" {
				t.Fatal(v)
			}
			if e.Select(s, base.Add(time.Second)) != nil {
				t.Fatal("overlapping selected lane")
			}
		})
	}
}
func TestDialogueOnlyOpensOnActualPlaying(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "a", "u", "chat", "新建发货吗", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true, NeedClarify: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	add(e, s, "b", "u", "chat", "2", base.Add(2*time.Second))
	if n := len(e.Jobs(base.Add(4*time.Second), 10)); n != 0 || len(e.rooms[29].Dialogues) != 0 {
		t.Fatal("before clarification playback numeric input must not invent a dialogue or semantic question", n)
	}
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(5*time.Second))
	add(e, s, "c", "u", "chat", "新建区", base.Add(6*time.Second))
	e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(7*time.Second))
	jobs := e.Jobs(base.Add(11*time.Second), 10)
	if len(jobs) != 1 || jobs[0].Kind != "dialogue" || jobs[0].Original != "新建发货吗" || jobs[0].AllowClarify {
		t.Fatal(jobs)
	}
}
func TestDialogueSameNicknameDifferentUID(t *testing.T) {
	e := New()
	s := scope(29)
	e.Add(s, Event{ID: "1", UID: "first", Name: "小王", Kind: "chat", Text: "新建", At: base}, base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, NeedClarify: true})
	v := selectTask(t, e, s, base.Add(time.Second))
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(2*time.Second))
	e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(3*time.Second))
	e.Add(s, Event{ID: "2", UID: "second", Name: "小王", Kind: "chat", Text: "新订单", At: base.Add(4 * time.Second)}, base.Add(4*time.Second))
	jobs := e.Jobs(base.Add(6*time.Second), 10)
	if len(jobs) != 1 || jobs[0].Kind != "semantic" {
		t.Fatal(jobs)
	}
}
func TestDialogueInputInvalidatesGeneratedAndSelected(t *testing.T) {
	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			e := New()
			s := scope(29)
			add(e, s, "a", "u", "chat", "新建", base)
			ready(t, e, base.Add(time.Second), Intent{Relevant: true, NeedClarify: true})
			q := selectTask(t, e, s, base.Add(time.Second))
			e.Receipts(29, q.Work.ID, "PLAYING", base.Add(2*time.Second))
			e.Receipts(29, q.Work.ID, "COMPLETED", base.Add(3*time.Second))
			add(e, s, "b", "u", "chat", "新建区", base.Add(4*time.Second))
			jobs := e.Jobs(base.Add(9*time.Second), 10)
			if len(jobs) != 1 {
				t.Fatal(jobs)
			}
			old := jobs[0]
			if selected {
				e.Accept(old, Content{Body: "旧回复。", Intent: Intent{Relevant: true}}, nil, base.Add(9*time.Second))
				selectTask(t, e, s, base.Add(9*time.Second))
			}
			add(e, s, "c", "u", "chat", "晚几天发", base.Add(10*time.Second))
			if e.Accept(old, Content{Body: "迟到旧回复。", Intent: Intent{Relevant: true}}, nil, base.Add(10*time.Second)) {
				t.Fatal("accepted stale revision")
			}
			if selected && len(e.Cancelled(29)) != 1 {
				t.Fatal("selected task not withdrawn")
			}
			next := e.Jobs(base.Add(15*time.Second), 10)
			if len(next) != 1 || next[0].Text != "晚几天发" {
				t.Fatal(next)
			}
		})
	}
}
func TestDialogueDebounceMaximumTenSeconds(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "a", "u", "chat", "新建", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, NeedClarify: true})
	q := selectTask(t, e, s, base.Add(time.Second))
	e.Receipts(29, q.Work.ID, "PLAYING", base.Add(2*time.Second))
	e.Receipts(29, q.Work.ID, "COMPLETED", base.Add(3*time.Second))
	for i := 4; i <= 14; i += 2 {
		add(e, s, fmt.Sprint(i), "u", "chat", fmt.Sprint(i), base.Add(time.Duration(i)*time.Second))
	}
	jobs := e.Jobs(base.Add(14*time.Second), 10)
	if len(jobs) != 1 || jobs[0].Count != 5 {
		t.Fatal(jobs)
	}
}
func TestBehaviorEarliestDeadlineAndBodyReadyBypass(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "like", "l", "like", "", base)
	add(e, s, "wel", "w", "member", "", base)
	jobs := e.Jobs(base.Add(time.Second), 10)
	if len(jobs) != 2 {
		t.Fatal(jobs)
	}
	for _, w := range jobs {
		if w.Kind == "welcome" {
			e.Accept(w, Content{Body: "欢迎。", Intent: Intent{Relevant: true}}, nil, base.Add(time.Second))
		}
	}
	v := selectTask(t, e, s, base.Add(time.Second))
	if v.Work.Kind != "welcome" {
		t.Fatal(v)
	}
}
func TestNoLifetimeRenewalOrLateModelResult(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "1", "u", "member", "", base)
	jobs := e.Jobs(base.Add(time.Second), 10)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	if e.Accept(jobs[0], Content{Body: "欢迎。", Intent: Intent{Relevant: true}}, nil, base.Add(20*time.Second)) {
		t.Fatal("accepted expired body")
	}
	e.Jobs(base.Add(20*time.Second), 10)
	if e.Select(s, base.Add(20*time.Second)) != nil {
		t.Fatal("selected expired")
	}
}
func TestSessionFenceAndTwoRooms(t *testing.T) {
	e := New()
	a, b := scope(29), scope(33)
	add(e, a, "1", "u", "chat", "哪里发", base)
	add(e, b, "1", "u", "chat", "多少钱", base)
	jobs := e.Jobs(base.Add(time.Second), 10)
	if len(jobs) != 2 {
		t.Fatal(jobs)
	}
	newScope := a
	newScope.Epoch = "boot:7:2"
	e.Select(newScope, base.Add(2*time.Second))
	for _, w := range jobs {
		accepted := e.Accept(w, Content{Body: "回答。", Intent: Intent{Relevant: true}}, nil, base.Add(2*time.Second))
		if accepted != (w.Scope.Room == 33) {
			t.Fatal(w, accepted)
		}
	}
}
func TestBoundedRetryAndCancellation(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "1", "u", "chat", "哪里发", base)
	jobs := e.Jobs(base.Add(time.Second), 10)
	e.Accept(jobs[0], Content{}, errors.New("provider failure"), base.Add(time.Second))
	jobs = e.Jobs(base.Add(3*time.Second), 10)
	if len(jobs) != 1 {
		t.Fatal(jobs)
	}
	e.Accept(jobs[0], Content{}, errors.New("provider failure"), base.Add(3*time.Second))
	if len(e.Jobs(base.Add(6*time.Second), 10)) != 0 {
		t.Fatal("unbounded retries")
	}
	add(e, s, "2", "u", "chat", "哪里发", base.Add(6*time.Second))
	e.CancelUser(s, "u", base.Add(6*time.Second))
	if len(e.Jobs(base.Add(8*time.Second), 10)) != 0 {
		t.Fatal("blocked user generated")
	}
}
func TestEngagementExactTokenDistinctUIDAndOtherQuestions(t *testing.T) {
	e := New()
	s := scope(29)
	e.Select(s, base)
	e.Jobs(base.Add(60*time.Second), 10)
	jobs := e.Jobs(base.Add(61*time.Second), 10)
	if len(jobs) != 1 || jobs[0].Kind != "guide" {
		t.Fatal(jobs)
	}
	e.Accept(jobs[0], Content{Body: "想了解就打1。", Intent: Intent{Relevant: true, Command: "1"}}, nil, base.Add(61*time.Second))
	v := selectTask(t, e, s, base.Add(61*time.Second))
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(62*time.Second))
	e.Receipts(29, v.Work.ID, "COMPLETED", base.Add(64*time.Second))
	for i := 0; i < 5; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i%3), "chat", "1", base.Add(65*time.Second))
	}
	add(e, s, "other", "other", "chat", "1号多少钱", base.Add(66*time.Second))
	jobs = e.Jobs(base.Add(76*time.Second), 10)
	found := map[string]bool{}
	for _, w := range jobs {
		found[w.Kind] = true
		if w.Kind == "engagement" && w.Count != 3 {
			t.Fatal(w)
		}
	}
	if !found["engagement"] || !found["semantic"] {
		t.Fatal(jobs)
	}
}
func TestRollingBudgetUsesActualPlaybackAndUrgentBypasses(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 3; i++ {
		at := base.Add(time.Duration(i*15) * time.Second)
		add(e, s, fmt.Sprint(i), "u", "chat", "问题", at)
		ready(t, e, at.Add(4*time.Second), Intent{Relevant: true})
		v := selectTask(t, e, s, at.Add(4*time.Second))
		e.Receipts(29, v.Work.ID, "PLAYING", at.Add(4*time.Second))
		e.Receipts(29, v.Work.ID, "COMPLETED", at.Add(15*time.Second))
	}
	add(e, s, "normal", "u", "chat", "普通", base.Add(46*time.Second))
	ready(t, e, base.Add(50*time.Second), Intent{Relevant: true})
	if e.Select(s, base.Add(50*time.Second)) != nil {
		t.Fatal("ignored actual occupied 33 seconds")
	}
	e.Add(s, Event{ID: "urgent", UID: "u", Kind: "chat", Text: "投诉", At: base.Add(48 * time.Second), Urgent: true}, base.Add(51*time.Second))
	ready(t, e, base.Add(51*time.Second), Intent{Relevant: true})
	v := selectTask(t, e, s, base.Add(51*time.Second))
	if v.Work.Kind != "urgent" {
		t.Fatal(v)
	}
}
func TestBurstQuestionsKeepIndependentRequestContexts(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 24; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", fmt.Sprintf("问题%d", i), base)
	}
	jobs := e.Jobs(base.Add(time.Second), 10)
	if len(jobs) != 10 {
		t.Fatal(len(jobs))
	}
	seen := map[string]bool{}
	for _, w := range jobs {
		if seen[w.Text] || len(w.People) != 1 {
			t.Fatal(w)
		}
		seen[w.Text] = true
	}
	if len(e.Jobs(base.Add(time.Second), 10)) != 10 {
		t.Fatal("batch bound incorrectly discards remaining tasks")
	}
}
