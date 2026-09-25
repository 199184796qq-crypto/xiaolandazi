package roomintel

import (
	"sort"
	"strings"
	"time"
)

type Heat string

const (
	HeatCold       Heat = "COLD"
	HeatWarm       Heat = "WARM"
	HeatBusy       Heat = "BUSY"
	HeatHot        Heat = "HOT"
	HeatOverheated Heat = "OVERHEATED"
)

type EventType string

const (
	EventEnter  EventType = "member"
	EventChat   EventType = "chat"
	EventLike   EventType = "like"
	EventFollow EventType = "follow"
	EventOrder  EventType = "order"
	EventGift   EventType = "gift"
	EventRoom   EventType = "room"
)

type Event struct {
	Type       EventType
	UserID     string
	Topic      string
	Content    string
	Question   bool
	Negative   bool
	Count      int64
	Online     int
	OccurredAt time.Time
}

type TopicBucket struct {
	Topic           string
	Count           int
	UniqueUsers     int
	LastSeenAt      time.Time
	LastAnsweredAt  time.Time
	SampleQuestions []string
}

type Snapshot struct {
	Heat                Heat
	OnlineCount         int
	Entries30s          int
	Entries60s          int
	Chats30s            int
	Likes30s            int64
	Follows30s          int
	Orders30s           int
	UniqueChatters30s   int
	QuestionCount30s    int
	NegativeFeedback30s int
	QuestionPressure    float64
	AudienceTurnover5m  float64
	TopTopics           []TopicBucket
	PreferAggregateQNA  bool
	PreferOneToOneQNA   bool
}

type Pool struct {
	events  []Event
	buckets map[string]*bucketState
	online  int
}

type bucketState struct {
	topic          string
	events         []Event
	lastAnsweredAt time.Time
}

func NewPool() *Pool {
	return &Pool{buckets: map[string]*bucketState{}}
}

func (p *Pool) Add(event Event) {
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.Count <= 0 {
		event.Count = 1
	}
	p.events = append(p.events, event)
	if event.Online > 0 {
		p.online = event.Online
	}
	topic := strings.TrimSpace(strings.ToUpper(event.Topic))
	if event.Type == EventChat && event.Question && topic != "" {
		state := p.buckets[topic]
		if state == nil {
			state = &bucketState{topic: topic}
			p.buckets[topic] = state
		}
		state.events = append(state.events, event)
	}
}

func (p *Pool) MarkAnswered(topic string, at time.Time) {
	topic = strings.TrimSpace(strings.ToUpper(topic))
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if state := p.buckets[topic]; state != nil {
		state.lastAnsweredAt = at
	}
}

func (p *Pool) Snapshot(now time.Time, answerCapacityPerMinute float64) Snapshot {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if answerCapacityPerMinute <= 0 {
		answerCapacityPerMinute = 6
	}
	cut30 := now.Add(-30 * time.Second)
	cut60 := now.Add(-60 * time.Second)
	cut5m := now.Add(-5 * time.Minute)

	uniqueChatters := map[string]struct{}{}
	entries5m := 0
	out := Snapshot{OnlineCount: p.online}

	kept := p.events[:0]
	for _, event := range p.events {
		if event.OccurredAt.Before(now.Add(-15 * time.Minute)) {
			continue
		}
		kept = append(kept, event)
		if !event.OccurredAt.Before(cut30) {
			switch event.Type {
			case EventEnter:
				out.Entries30s += int(event.Count)
			case EventChat:
				out.Chats30s += int(event.Count)
				if event.Question {
					out.QuestionCount30s += int(event.Count)
				}
				if event.Negative {
					out.NegativeFeedback30s += int(event.Count)
				}
				if event.UserID != "" {
					uniqueChatters[event.UserID] = struct{}{}
				}
			case EventLike:
				out.Likes30s += event.Count
			case EventFollow:
				out.Follows30s += int(event.Count)
			case EventOrder:
				out.Orders30s += int(event.Count)
			}
		}
		if !event.OccurredAt.Before(cut60) && event.Type == EventEnter {
			out.Entries60s += int(event.Count)
		}
		if !event.OccurredAt.Before(cut5m) && event.Type == EventEnter {
			entries5m += int(event.Count)
		}
	}
	p.events = kept
	out.UniqueChatters30s = len(uniqueChatters)
	out.QuestionPressure = float64(out.QuestionCount30s*2) / answerCapacityPerMinute
	if out.OnlineCount > 0 {
		out.AudienceTurnover5m = float64(entries5m) / float64(out.OnlineCount)
	}

	for key, state := range p.buckets {
		questionEvents := state.events[:0]
		users := map[string]struct{}{}
		bucket := TopicBucket{Topic: state.topic, LastAnsweredAt: state.lastAnsweredAt}
		for _, event := range state.events {
			if event.OccurredAt.Before(cut5m) {
				continue
			}
			questionEvents = append(questionEvents, event)
			bucket.Count += int(event.Count)
			if event.UserID != "" {
				users[event.UserID] = struct{}{}
			}
			if event.OccurredAt.After(bucket.LastSeenAt) {
				bucket.LastSeenAt = event.OccurredAt
			}
			if strings.TrimSpace(event.Content) != "" && len(bucket.SampleQuestions) < 3 {
				bucket.SampleQuestions = append(bucket.SampleQuestions, strings.TrimSpace(event.Content))
			}
		}
		state.events = questionEvents
		if len(questionEvents) == 0 {
			delete(p.buckets, key)
			continue
		}
		bucket.UniqueUsers = len(users)
		out.TopTopics = append(out.TopTopics, bucket)
	}
	sort.Slice(out.TopTopics, func(i, j int) bool {
		if out.TopTopics[i].Count == out.TopTopics[j].Count {
			return out.TopTopics[i].LastSeenAt.After(out.TopTopics[j].LastSeenAt)
		}
		return out.TopTopics[i].Count > out.TopTopics[j].Count
	})
	if len(out.TopTopics) > 8 {
		out.TopTopics = out.TopTopics[:8]
	}

	out.Heat = classify(out)
	out.PreferAggregateQNA = out.Heat == HeatHot || out.Heat == HeatOverheated || out.QuestionPressure >= 1.5
	out.PreferOneToOneQNA = (out.Heat == HeatCold || out.Heat == HeatWarm) && out.QuestionPressure < 0.8
	return out
}

func classify(s Snapshot) Heat {
	if s.QuestionPressure >= 3 || s.Chats30s >= 40 || s.Entries30s >= 120 {
		return HeatOverheated
	}
	if s.QuestionPressure >= 1.5 || s.Chats30s >= 20 || s.Entries30s >= 60 {
		return HeatHot
	}
	if s.Chats30s >= 8 || s.Entries30s >= 20 || s.Follows30s >= 5 {
		return HeatBusy
	}
	if s.Chats30s >= 2 || s.Entries30s >= 5 {
		return HeatWarm
	}
	return HeatCold
}
