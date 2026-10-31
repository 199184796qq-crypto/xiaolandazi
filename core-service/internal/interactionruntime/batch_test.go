package interactionruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestProductionBatchesOneThroughTenKeepSeparatePeople(t *testing.T) {
	for n := 1; n <= 10; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			e := New()
			s := scope(29)
			for i := 0; i < n; i++ {
				add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", fmt.Sprintf("个人%d的问题", i), base)
			}
			batches := e.Batches(base.Add(time.Second), 1)
			if len(batches) != 1 || len(batches[0].Works) != n {
				t.Fatal(batches)
			}
			seen := map[string]bool{}
			for _, w := range batches[0].Works {
				if len(w.People) != 1 || seen[w.People[0].UID] || !w.BatchPrepared {
					t.Fatal(w)
				}
				seen[w.People[0].UID] = true
			}
		})
	}
}

func TestBatchFreshestSixtySecondsTenDistinctOwnFiveMessages(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 6; i++ {
		at := base.Add(time.Duration(i) * time.Second)
		add(e, s, fmt.Sprintf("old%d", i), "same", "chat", fmt.Sprintf("本人的上下文%d", i), at)
	}
	add(e, s, "other", "other", "chat", "其他人的秘密", base.Add(4*time.Second))
	for i := 0; i < 12; i++ {
		add(e, s, fmt.Sprintf("new%d", i), fmt.Sprint(i), "chat", "其他候选", base.Add(time.Duration(10+i)*time.Second))
	}
	add(e, s, "latest", "same", "chat", "当前问题", base.Add(30*time.Second))
	bs := e.Batches(base.Add(35*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 10 {
		t.Fatal(bs)
	}
	for _, w := range bs[0].Works {
		if w.People[0].UID == "same" {
			if w.Text != "当前问题" || len(w.Context) != 5 {
				t.Fatal(w)
			}
			for _, m := range w.Context {
				if !strings.HasPrefix(m.Text, "本人的上下文") {
					t.Fatal("cross-user history", w)
				}
			}
		}
		if w.At.Before(base.Add(10 * time.Second)) {
			t.Fatal("not freshest ten", w)
		}
	}
	// Nothing may be pulled from before the exact 60-second candidate boundary.
	e2 := New()
	add(e2, s, "old", "old", "chat", "过期问题", base)
	add(e2, s, "fresh", "fresh", "chat", "新问题", base.Add(59*time.Second))
	bs = e2.Batches(base.Add(60*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 1 || bs[0].Works[0].Text != "新问题" {
		t.Fatal(bs)
	}
}

func TestBatchRoomsAndEpochNeverMixOrAcceptLateResults(t *testing.T) {
	e := New()
	a, b := scope(29), scope(30)
	for i := 0; i < 12; i++ {
		add(e, a, fmt.Sprint(i), fmt.Sprint(i), "chat", "甲房问题", base)
	}
	add(e, b, "b", "u", "chat", "乙房问题", base)
	bs := e.Batches(base.Add(time.Second), 2)
	if len(bs) != 2 || bs[0].Works[0].Scope.Room == bs[1].Works[0].Scope.Room {
		t.Fatal(bs)
	}
	for _, batch := range bs {
		for _, w := range batch.Works {
			if w.Scope != batch.Works[0].Scope {
				t.Fatal("mixed room", batch)
			}
		}
	}
	newScope := a
	newScope.Epoch = "next"
	e.SetPolicy(newScope, Policy{Active: true}, base.Add(2*time.Second))
	for _, batch := range bs {
		for _, w := range batch.Works {
			got := e.Accept(w, Content{Body: "回答。", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(2*time.Second))
			if got != (w.Scope.Room == b.Room) {
				t.Fatal("late epoch result", w, got)
			}
		}
	}
}

func TestBatchPrioritySelectionAndAdmissionTTL50(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 3; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", "问题", base)
	}
	bs := e.Batches(base.Add(2*time.Second), 1)
	for _, w := range bs[0].Works {
		typeName := "提问"
		hint := "紧急"
		if w.People[0].UID == "2" {
			typeName = "投诉"
			hint = "普通"
		}
		raw, _ := json.Marshal(map[string]any{"protocol": Protocol, "task": w.ID, "expires_at": w.Expires, "body": "回答。"})
		if !e.Accept(w, Content{Body: "回答。", Intent: Intent{Relevant: true, Question: true, Type: typeName, Priority: hint, PriorityReason: "内容原因"}, Knowledge: raw}, nil, base.Add(55*time.Second)) {
			t.Fatal("late but live candidate rejected")
		}
	}
	v := e.Select(s, base.Add(58*time.Second))
	if v == nil || v.Content.Intent.Type != "投诉" || !v.Work.Expires.Equal(base.Add(108*time.Second)) || !v.Work.CandidateExpires.Equal(base.Add(60*time.Second)) {
		t.Fatal(v)
	}
	e.Bind(v.Work, "decision", base.Add(58*time.Second))
	var env struct {
		Expires time.Time `json:"expires_at"`
	}
	json.Unmarshal(v.Content.Knowledge, &env)
	if !env.Expires.Equal(v.Work.Expires) {
		t.Fatal("queue knowledge retained old expiry", env)
	}
	if !e.ReserveAudio(29, "decision", 2000, base.Add(80*time.Second)) {
		t.Fatal("valid selected task killed by original candidate TTL")
	}
	if e.ReserveAudio(29, "decision", 2000, base.Add(108*time.Second)) {
		t.Fatal("50s audio start boundary ignored")
	}
	e.Batches(base.Add(61*time.Second), 1)
	for _, task := range e.Snapshot(29) {
		if task.Work.ID == v.Work.ID && task.State != "selected" {
			t.Fatal("selected incorrectly expired", task)
		}
	}
	e.Receipts(29, "decision", "PLAYING", base.Add(107*time.Second))
	e.Receipts(29, "decision", "COMPLETED", base.Add(120*time.Second))
	for _, task := range e.Snapshot(29) {
		if task.Work.ID == v.Work.ID && task.State != "completed" {
			t.Fatal("started speech cut by TTL", task)
		}
	}
}

func TestBatchQuotaBoundariesUseDistinctUsersAndPlaybackStarts(t *testing.T) {
	for _, tc := range []struct {
		n      int
		window time.Duration
	}{{4, 0}, {5, 60 * time.Second}, {10, 60 * time.Second}, {11, 180 * time.Second}, {20, 180 * time.Second}, {21, 300 * time.Second}} {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			r := &room{SemanticUsers: map[string]time.Time{}, Tasks: map[string]*Task{}}
			for i := 0; i < tc.n; i++ {
				r.SemanticUsers[fmt.Sprint(i)] = base
			}
			r.SemanticStarts = []time.Time{base, base.Add(time.Second)}
			if got := semanticQuotaAvailable(r, base.Add(2*time.Second), ""); got != (tc.window == 0) {
				t.Fatal("quota band", got, tc)
			}
			if tc.window > 0 {
				// Keep the current audience density live while checking the rolling boundary.
				for uid := range r.SemanticUsers {
					r.SemanticUsers[uid] = base.Add(tc.window)
				}
				if !semanticQuotaAvailable(r, base.Add(tc.window), "") {
					t.Fatal("boundary did not release first slot")
				}
			}
		})
	}
	e := New()
	s := scope(29)
	for i := 0; i < 7; i++ {
		add(e, s, fmt.Sprint(i), "same", "chat", "重复发言", base.Add(time.Duration(i)*time.Second))
	}
	if len(e.rooms[29].SemanticUsers) != 0 {
		t.Fatal("raw pending messages counted as confirmed semantic users")
	}
	for _, b := range e.Batches(base.Add(12*time.Second), 1) {
		for _, w := range b.Works {
			e.Accept(w, Content{Body: "回答。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(12*time.Second))
		}
	}
	if len(e.rooms[29].SemanticUsers) != 1 {
		t.Fatal("same person inflated density")
	}
}

func TestBatchQuotaReservationReleasedWithoutPlayback(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 5; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", "问题", base)
	}
	bs := e.Batches(base.Add(2*time.Second), 1)
	for _, w := range bs[0].Works {
		e.Accept(w, Content{Body: "回答。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(2*time.Second))
	}
	v := selectTask(t, e, s, base.Add(3*time.Second))
	e.Lost(29, v.Work.ID, base.Add(4*time.Second))
	if len(e.rooms[29].SemanticStarts) != 0 {
		t.Fatal("unplayed item consumed quota")
	}
	v = selectTask(t, e, s, base.Add(4*time.Second))
	e.Receipts(29, v.Work.ID, "PLAYING", base.Add(5*time.Second))
	e.Receipts(29, v.Work.ID, "PROGRESS", base.Add(6*time.Second))
	e.Receipts(29, v.Work.ID, "FAILED", base.Add(7*time.Second))
	if len(e.rooms[29].SemanticStarts) != 1 {
		t.Fatal("start not counted exactly once")
	}
}

func TestBatchSameTimestampLatestWinsAndBurstCapacityKeepsFresh(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 13; i++ {
		add(e, s, fmt.Sprint(i), "same", "chat", fmt.Sprintf("当前问题%d", i), base)
	}
	bs := e.Batches(base.Add(2*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 1 || bs[0].Works[0].Text != "当前问题12" {
		t.Fatal("lexicographic ID changed newest person context", bs)
	}
	e = New()
	for i := 0; i < 300; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", fmt.Sprintf("问题%d", i), base)
	}
	bs = e.Batches(base.Add(2*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 10 {
		t.Fatal(bs)
	}
	for i, w := range bs[0].Works {
		if w.Text != fmt.Sprintf("问题%d", 299-i) {
			t.Fatal("burst kept stale tasks", i, w)
		}
	}
}

func TestBatchSemanticDensityExcludesKnownCommandEcho(t *testing.T) {
	e := New()
	s := scope(29)
	e.Select(s, base)
	e.rooms[29].Command = &command{Token: "1", Last: base, Users: map[string]bool{}}
	for i := 0; i < 30; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", "1", base)
	}
	if len(e.rooms[29].SemanticUsers) != 0 {
		t.Fatal("command participation inflated semantic density")
	}
	add(e, s, "question", "buyer", "chat", "1号多少钱", base.Add(time.Second))
	if len(e.rooms[29].SemanticUsers) != 0 {
		t.Fatal("pending question counted before model classification")
	}
	for _, b := range e.Batches(base.Add(6*time.Second), 1) {
		for _, w := range b.Works {
			e.Accept(w, Content{Body: "按商品卡价格。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(6*time.Second))
		}
	}
	if len(e.rooms[29].SemanticUsers) != 1 {
		t.Fatal("actual question missing")
	}
}

func TestBatchTTLStartsOnAdmissionAndCannotRenewForAudioRetry(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "one", "u", "chat", "问题", base)
	bs := e.Batches(base.Add(time.Second), 1)
	w := bs[0].Works[0]
	e.Accept(w, Content{Body: "回答。", Intent: Intent{Relevant: true, Question: true}}, nil, base.Add(2*time.Second))
	v := e.Select(s, base.Add(3*time.Second))
	if v == nil {
		t.Fatal("not selected")
	}
	e.Bind(v.Work, "", base.Add(3*time.Second))
	for _, task := range e.Snapshot(29) {
		if !task.Work.Expires.Equal(base.Add(time.Minute)) || !task.SelectedAt.IsZero() {
			t.Fatal("failed enqueue started execution clock", task)
		}
	}
	v = e.Select(s, base.Add(10*time.Second))
	e.Bind(v.Work, "admitted", base.Add(10*time.Second))
	if !v.Work.Expires.Equal(base.Add(60 * time.Second)) {
		t.Fatal(v)
	}
	if !e.ReserveAudio(29, "admitted", 1000, base.Add(30*time.Second)) || e.ReserveAudio(29, "admitted", 1000, base.Add(60*time.Second)) {
		t.Fatal("audio retry renewed execution deadline")
	}
}

func TestBatchProgramEscalatesOnlyQualifiedUrgentComplaint(t *testing.T) {
	for _, tc := range []struct {
		typ, reason string
		preempt     bool
	}{{"提问", "催促回复", false}, {"投诉", "", false}, {"投诉", "已收到损坏商品且需要尽快处理的纠纷", true}} {
		e := New()
		s := scope(29)
		add(e, s, "one", "u1", "chat", "普通问题", base)
		bs := e.Batches(base.Add(time.Second), 1)
		e.Accept(bs[0].Works[0], Content{Body: "普通答复。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(time.Second))
		old := selectTask(t, e, s, base.Add(2*time.Second))
		add(e, s, "two", "u2", "chat", "另一个问题", base.Add(3*time.Second))
		bs = e.Batches(base.Add(5*time.Second), 1)
		if len(bs) != 1 {
			t.Fatal(bs)
		}
		e.Accept(bs[0].Works[0], Content{Body: "联系客服核实处理。", Intent: Intent{Relevant: true, Question: true, Type: tc.typ, Priority: "紧急", PriorityReason: tc.reason}}, nil, base.Add(5*time.Second))
		v := e.Select(s, base.Add(6*time.Second))
		if (v != nil) != tc.preempt {
			t.Fatal(tc, v)
		}
		if tc.preempt && (v.Work.Kind != "urgent" || len(e.Cancelled(29)) != 1 || v.Work.ID == old.Work.ID) {
			t.Fatal("urgent preemption not controlled", v)
		}
	}
}

func TestBatchUnknownIdentityNeverInventsDifferentPeopleOrSharesHistory(t *testing.T) {
	e := New()
	s := scope(29)
	for i := 0; i < 5; i++ {
		add(e, s, fmt.Sprint(i), "", "chat", fmt.Sprintf("匿名问题%d", i), base)
	}
	add(e, s, "known", "known", "chat", "已知用户的问题", base)
	bs := e.Batches(base.Add(2*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 2 {
		t.Fatal("unknown identities counted as different people", bs)
	}
	for _, w := range bs[0].Works {
		if len(w.People) == 0 && (w.Text != "匿名问题4" || len(w.Context) != 0) {
			t.Fatal("anonymous histories mixed", w)
		}
	}
}
