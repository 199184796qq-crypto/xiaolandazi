package interactionruntime

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSpokenCommandPatternsDoNotInventPrompts(t *testing.T) {
	for _, tc := range []struct {
		text   string
		tokens []string
	}{
		{"想了解就打1。", []string{"1"}},
		{"想要麻辣的扣1，五香的扣2。", []string{"1", "2"}},
		{"把“想要麻辣的”打在公屏上。", []string{"想要麻辣的"}},
		{"回复“收到”。", []string{"收到"}},
		{"扣 ６６６！", []string{"666"}},
		{"想要的打个想要。", []string{"想要"}},
		{"想要的扣个一。", []string{"1"}},
		{"特别想了解的扣1。", []string{"1"}},
		{"不要太辣的扣1。", []string{"1"}},
		{"不要扣1。", nil}, {"别刷666。", nil}, {"不用扣1，想了解就打2。", []string{"2"}},
		{"今天是1号，这个48块8。", nil}, {"给商品打1折。", nil}, {"扣6块钱。", nil},
		{"想要。", nil}, {"哪里发货？", nil},
	} {
		t.Run(tc.text, func(t *testing.T) {
			if got := ExtractEngagementTokens(tc.text); !reflect.DeepEqual(got, tc.tokens) {
				t.Fatalf("got %q want %q", got, tc.tokens)
			}
		})
	}
}

func TestLongPromptSegmentDoesNotCloseBeforeItFinishes(t *testing.T) {
	e := New()
	s := scope(29)
	e.ObserveSpokenWindow(s, "pcm", "想要的扣1。", base, base.Add(25*time.Second), base)
	if len(e.Batches(base.Add(12*time.Second), 1)) != 0 || e.rooms[29].Command == nil {
		t.Fatal("long spoken segment closed at its first frame")
	}
	for i := 0; i < 3; i++ {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", "1", base.Add(26*time.Second))
	}
	if len(e.Batches(base.Add(35*time.Second), 1)) != 0 {
		t.Fatal("closed before the quiet tail")
	}
	bs := e.Batches(base.Add(36*time.Second), 1)
	if len(bs) != 1 || bs[0].Works[0].Kind != "engagement" || bs[0].Works[0].Count != 3 {
		t.Fatal(bs)
	}
}

func TestHundredVotesPlusFiveQuestionsOnlyCountFiveSemanticPeople(t *testing.T) {
	e := New()
	s := scope(29)
	e.ObserveSpoken(s, "pcm", "想了解就扣1。", base, base)
	for i := 0; i < 100; i++ {
		add(e, s, fmt.Sprintf("v%d", i), fmt.Sprintf("voter%d", i), "chat", "1", base.Add(time.Second))
	}
	for i := 0; i < 5; i++ {
		add(e, s, fmt.Sprintf("q%d", i), fmt.Sprintf("asker%d", i), "chat", "哪里发货", base.Add(2*time.Second))
	}
	bs := e.Batches(base.Add(8*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 5 {
		t.Fatal("votes took semantic batch slots", bs)
	}
	for _, w := range bs[0].Works {
		e.Accept(w, Content{Body: "按方案说明。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(8*time.Second))
	}
	if len(e.rooms[29].SemanticUsers) != 5 {
		t.Fatal(e.rooms[29].SemanticUsers)
	}
	bs = e.Batches(base.Add(11*time.Second), 1)
	if len(bs) != 1 || bs[0].Works[0].Kind != "engagement" || bs[0].Works[0].Count != 100 {
		t.Fatal("votes were not aggregated once", bs)
	}
	t.Log("100位口令参与者 + 5位真实提问者：语义人数5；语义批次5项；主动互动1项/100人")
}

func TestUnassociatedNumbersNeverCreateSemanticTasksOrSpamVerdicts(t *testing.T) {
	e := New()
	s := scope(29)
	for i, text := range []string{"1", "６６６", "666666", "2", "1.5", "123456"} {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", text, base)
	}
	if len(e.Snapshot(29)) != 0 || len(e.Batches(base.Add(5*time.Second), 1)) != 0 || len(e.rooms[29].SemanticUsers) != 0 {
		t.Fatal("numbers entered semantic processing")
	}
	if len(e.rooms[29].History) != 6 {
		t.Fatal("numeric evidence was lost")
	}
	if len(e.Batches(base.Add(12*time.Second), 1)) != 0 {
		t.Fatal("unassociated numbers produced a vote announcement")
	}
}

func TestKnownParticipationDistinctPeopleAndSilentTail(t *testing.T) {
	e := New()
	s := scope(29)
	e.ObserveSpoken(s, "pcm1", "想了解就扣666。", base, base)
	for i, text := range []string{"666", "６６６６！", "666666"} {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", text, base.Add(time.Second))
	}
	add(e, s, "repeat", "0", "chat", "666", base.Add(9*time.Second))
	if len(e.rooms[29].Command.Users) != 3 || !e.rooms[29].Command.Last.Equal(base.Add(time.Second)) {
		t.Fatal("repeat user extended counting window")
	}
	add(e, s, "question", "buyer", "chat", "1号多少钱", base.Add(2*time.Second))
	var count int
	for _, b := range e.Batches(base.Add(11*time.Second), 4) {
		for _, w := range b.Works {
			if w.Kind == "engagement" {
				count++
				if w.Count != 3 {
					t.Fatal(w)
				}
			}
			if w.Kind == "semantic" {
				e.Accept(w, Content{Body: "按商品卡。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(11*time.Second))
			}
		}
	}
	if count != 1 || len(e.rooms[29].SemanticUsers) != 1 {
		t.Fatal(count, e.rooms[29].SemanticUsers)
	}
	for _, b := range e.Batches(base.Add(12*time.Second), 4) {
		for _, w := range b.Works {
			if w.Kind == "engagement" {
				t.Fatal("duplicate aggregate")
			}
		}
	}
}

func TestPromptSupportsMultipleOptionsWithoutDoubleCounting(t *testing.T) {
	e := New()
	s := scope(29)
	e.ObserveSpoken(s, "pcm", "麻辣扣1，五香扣2。", base, base)
	for i, text := range []string{"1", "2", "２", "1"} {
		add(e, s, fmt.Sprint(i), fmt.Sprint(i%3), "chat", text, base.Add(time.Second))
	}
	if len(e.rooms[29].Command.Users) != 3 {
		t.Fatal("same person counted twice across options")
	}
	add(e, s, "q", "buyer", "chat", "2号多少钱", base.Add(2*time.Second))
	bs := e.Batches(base.Add(7*time.Second), 1)
	if len(bs) != 1 || len(bs[0].Works) != 1 || bs[0].Works[0].Text != "2号多少钱" || bs[0].Works[0].EngagementToken != "1、2" {
		t.Fatal(bs)
	}
}

func TestIdenticalRealQuestionsRemainIndependentSemanticPeople(t *testing.T) {
	for _, typeName := range []string{"提问", "主动互动", "刷屏"} {
		t.Run(typeName, func(t *testing.T) {
			e := New()
			s := scope(29)
			for i := 0; i < 10; i++ {
				add(e, s, fmt.Sprint(i), fmt.Sprint(i), "chat", "哪里发货", base)
			}
			bs := e.Batches(base.Add(6*time.Second), 1)
			if len(bs) != 1 || len(bs[0].Works) != 10 || len(e.rooms[29].SemanticUsers) != 0 {
				t.Fatal(bs)
			}
			for _, w := range bs[0].Works {
				c := Content{Body: "按当前方案。", Intent: Intent{Relevant: true, Question: true, Type: typeName}}
				if typeName == "刷屏" {
					c.Intent.Relevant = false
					c.Body = ""
				}
				if typeName == "主动互动" {
					c.Body = ""
				}
				e.Accept(w, c, nil, base.Add(6*time.Second))
			}
			want := 0
			if typeName == "提问" {
				want = 10
			}
			if len(e.rooms[29].SemanticUsers) != want {
				t.Fatal(e.rooms[29].SemanticUsers)
			}
			if typeName == "主动互动" {
				aggregates := 0
				for _, b := range e.Batches(base.Add(11*time.Second), 1) {
					for _, w := range b.Works {
						if w.Kind == "engagement" && w.Count == 10 {
							aggregates++
						}
					}
				}
				if aggregates != 1 {
					t.Fatal("model participation was answered per person", aggregates)
				}
			}
		})
	}
}

func TestNumericDialogueOwnsReplyBeforePublicCommand(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "ask", "buyer", "chat", "要哪个规格", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true, NeedClarify: true})
	w := selectTask(t, e, s, base.Add(time.Second))
	e.Receipts(29, w.Work.ID, "PLAYING", base.Add(2*time.Second))
	e.Receipts(29, w.Work.ID, "COMPLETED", base.Add(3*time.Second))
	e.ObserveSpoken(s, "public", "想了解就扣2。", base.Add(3*time.Second), base.Add(3*time.Second))
	add(e, s, "reply", "buyer", "chat", "2", base.Add(4*time.Second))
	add(e, s, "vote", "someone", "chat", "2", base.Add(4*time.Second))
	bs := e.Batches(base.Add(9*time.Second), 1)
	if len(bs) != 1 || bs[0].Works[0].Kind != "dialogue" || bs[0].Works[0].Text != "2" || len(e.rooms[29].Command.Users) != 1 {
		t.Fatal(bs)
	}
	if !e.Accept(bs[0].Works[0], Content{Body: "明白，按这个规格。", Intent: Intent{Relevant: true, Question: true, Type: "提问", Relation: "related"}}, nil, base.Add(9*time.Second)) {
		t.Fatal("numeric dialogue discarded")
	}
}

func TestClassificationContinuesWhenPlaybackQuotaAndCooldownExhausted(t *testing.T) {
	e := New()
	s := scope(29)
	r := e.room(s, base)
	r.SemanticUsers = map[string]time.Time{}
	for i := 0; i < 21; i++ {
		r.SemanticUsers[fmt.Sprint(i)] = base
	}
	r.SemanticStarts = []time.Time{base, base.Add(time.Second)}
	r.Cool["semantic"] = base.Add(time.Minute)
	add(e, s, "new", "new-person", "chat", "哪里发货", base.Add(2*time.Second))
	bs := e.Batches(base.Add(8*time.Second), 1)
	if len(bs) != 1 {
		t.Fatal("playback quota blocked classification", bs)
	}
	e.Accept(bs[0].Works[0], Content{Body: "按方案说明。", Intent: Intent{Relevant: true, Question: true, Type: "提问"}}, nil, base.Add(8*time.Second))
	if len(e.rooms[29].SemanticUsers) != 22 || e.Select(s, base.Add(8*time.Second)) != nil {
		t.Fatal("classification and playback quotas were coupled")
	}
}

func TestParticipationDoesNotErasePreviousSemanticActivity(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "q", "buyer", "chat", "哪里发货", base)
	ready(t, e, base.Add(time.Second), Intent{Relevant: true, Question: true, Type: "提问"})
	e.ObserveSpoken(s, "pcm", "想要就打1。", base.Add(2*time.Second), base.Add(2*time.Second))
	add(e, s, "echo", "buyer", "chat", "1", base.Add(3*time.Second))
	if !e.rooms[29].SemanticUsers["buyer"].Equal(base) {
		t.Fatal("participation refreshed or erased prior semantic time")
	}
}

func TestSpokenObservationDedupEpochAndPromptSwitch(t *testing.T) {
	e := New()
	s := scope(29)
	e.ObserveSpoken(s, "pcm1", "想要就打1。", base, base)
	add(e, s, "v", "u", "chat", "1", base.Add(time.Second))
	e.ObserveSpoken(s, "pcm1", "想要就打1。", base.Add(2*time.Second), base.Add(2*time.Second))
	if len(e.rooms[29].Command.Users) != 1 {
		t.Fatal("duplicate PCM reset the votes")
	}
	e.ObserveSpoken(s, "pcm2", "把“收到”打在公屏上。", base.Add(3*time.Second), base.Add(3*time.Second))
	if e.rooms[29].Command.Token != "收到" || !strings.Contains(e.rooms[29].Carry, "1位") {
		t.Fatal("prompt switch did not close previous group")
	}
	newScope := s
	newScope.Epoch = "new-session"
	e.SetPolicy(newScope, Policy{Active: true}, base.Add(4*time.Second))
	e.ObserveSpoken(s, "late", "扣1。", base.Add(4*time.Second), base.Add(4*time.Second))
	if e.rooms[29].Scope != newScope || e.rooms[29].Command != nil {
		t.Fatal("old epoch activated a new-session command")
	}
}

func TestConfirmedPurchaseTypeDrivesSelectionWithoutKeywordOverride(t *testing.T) {
	e := New()
	s := scope(29)
	add(e, s, "q", "question", "chat", "2号多少钱", base)
	add(e, s, "buy", "buyer", "chat", "准备买礼盒，2号多少钱", base)
	bs := e.Batches(base.Add(time.Second), 1)
	for _, w := range bs[0].Works {
		typ := "提问"
		if firstUID(w) == "buyer" {
			typ = "下单意向"
		}
		e.Accept(w, Content{Body: "按商品卡。", Intent: Intent{Relevant: true, Question: true, Type: typ, Priority: "普通"}}, nil, base.Add(time.Second))
	}
	if got := e.Select(s, base.Add(2*time.Second)); got == nil || firstUID(got.Work) != "buyer" {
		t.Fatal(got)
	}
}
