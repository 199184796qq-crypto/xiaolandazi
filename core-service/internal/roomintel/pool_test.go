package roomintel

import (
	"testing"
	"time"
)

func TestHotRoomPrefersAggregateQuestions(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	for i := 0; i < 50; i++ {
		p.Add(Event{Type: EventChat, UserID: "u", Topic: "PRICE", Content: "多少钱", Question: true, OccurredAt: now.Add(-10 * time.Second)})
	}
	for i := 0; i < 80; i++ {
		p.Add(Event{Type: EventEnter, OccurredAt: now.Add(-10 * time.Second)})
	}
	p.Add(Event{Type: EventEnter, Online: 100, OccurredAt: now})
	s := p.Snapshot(now, 6)
	if s.Heat != HeatOverheated {
		t.Fatalf("heat=%s", s.Heat)
	}
	if !s.PreferAggregateQNA || s.PreferOneToOneQNA {
		t.Fatalf("snapshot=%#v", s)
	}
	if len(s.TopTopics) == 0 || s.TopTopics[0].Topic != "PRICE" {
		t.Fatalf("topics=%#v", s.TopTopics)
	}
}

func TestColdRoomPrefersOneToOne(t *testing.T) {
	now := time.Now().UTC()
	p := NewPool()
	p.Add(Event{Type: EventEnter, Online: 20, OccurredAt: now})
	s := p.Snapshot(now, 6)
	if s.Heat != HeatCold || !s.PreferOneToOneQNA {
		t.Fatalf("snapshot=%#v", s)
	}
}
