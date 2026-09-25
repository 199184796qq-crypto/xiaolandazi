package timeline

import (
	"sort"
	"strings"
	"time"
)

type PinKind string
type Heat string
type DebtKind string

const (
	PinStrategy      PinKind = "STRATEGY"
	PinQuestion      PinKind = "QUESTION"
	PinAnswer        PinKind = "ANSWER"
	PinCTA           PinKind = "CTA"
	PinHumor         PinKind = "HUMOR"
	PinHumanization  PinKind = "HUMANIZATION"
	PinBridge        PinKind = "BRIDGE"
	PinResume        PinKind = "RESUME"
	PinNodeJump      PinKind = "NODE_JUMP"
	PinMainlineTopic PinKind = "MAINLINE_TOPIC"

	HeatCold       Heat = "COLD"
	HeatWarm       Heat = "WARM"
	HeatBusy       Heat = "BUSY"
	HeatHot        Heat = "HOT"
	HeatOverheated Heat = "OVERHEATED"

	DebtInteraction DebtKind = "INTERACTION"
	DebtLikeCTA     DebtKind = "LIKE_CTA"
	DebtFollowCTA   DebtKind = "FOLLOW_CTA"
	DebtConversion  DebtKind = "CONVERSION"
	DebtQuestion    DebtKind = "QUESTION"
)

type Pin struct {
	ID           string
	At           time.Time
	Kind         PinKind
	Strategy     string
	Topic        string
	Key          string
	TextDigest   string
	MainlineUnit string
	Metadata     map[string]string
}

type Summary struct {
	CountsByKind     map[PinKind]int
	CountsByStrategy map[string]int
	TopicCounts      map[string]int
	LastByKind       map[PinKind]time.Time
	LastByStrategy   map[string]time.Time
	LastByTopic      map[string]time.Time
}

type View struct {
	StartedAt time.Time
	HotWindow time.Duration
	HotPins   []Pin
	Summary   Summary
	Debts     map[DebtKind]DebtState
}

type DebtState struct {
	Value         float64
	LastRaised    time.Time
	LastSpent     time.Time
	CooldownUntil time.Time
}

type Session struct {
	StartedAt time.Time
	pins      []Pin
	summary   Summary
	debts     map[DebtKind]DebtState
}

func NewSession(startedAt time.Time) *Session {
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return &Session{
		StartedAt: startedAt,
		summary: Summary{
			CountsByKind:     map[PinKind]int{},
			CountsByStrategy: map[string]int{},
			TopicCounts:      map[string]int{},
			LastByKind:       map[PinKind]time.Time{},
			LastByStrategy:   map[string]time.Time{},
			LastByTopic:      map[string]time.Time{},
		},
		debts: map[DebtKind]DebtState{},
	}
}

func HotWindowForHeat(heat Heat) time.Duration {
	switch heat {
	case HeatHot, HeatOverheated:
		return 3 * time.Minute
	case HeatCold:
		return 10 * time.Minute
	case HeatWarm:
		return 8 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func (s *Session) Append(pin Pin) {
	if pin.At.IsZero() {
		pin.At = time.Now().UTC()
	}
	pin.Strategy = strings.TrimSpace(pin.Strategy)
	pin.Topic = strings.TrimSpace(strings.ToUpper(pin.Topic))
	s.pins = append(s.pins, pin)
	sort.SliceStable(s.pins, func(i, j int) bool { return s.pins[i].At.Before(s.pins[j].At) })
}

func (s *Session) View(now time.Time, heat Heat) View {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	window := HotWindowForHeat(heat)
	s.compact(now.Add(-window))
	hot := make([]Pin, len(s.pins))
	copy(hot, s.pins)
	summary := cloneSummary(s.summary)
	debts := map[DebtKind]DebtState{}
	for key, value := range s.debts {
		debts[key] = value
	}
	return View{
		StartedAt: s.StartedAt,
		HotWindow: window,
		HotPins:   hot,
		Summary:   summary,
		Debts:     debts,
	}
}

func (s *Session) compact(cutoff time.Time) {
	keep := s.pins[:0]
	for _, pin := range s.pins {
		if !pin.At.Before(cutoff) {
			keep = append(keep, pin)
			continue
		}
		s.summary.CountsByKind[pin.Kind]++
		s.summary.LastByKind[pin.Kind] = maxTime(s.summary.LastByKind[pin.Kind], pin.At)
		if pin.Strategy != "" {
			s.summary.CountsByStrategy[pin.Strategy]++
			s.summary.LastByStrategy[pin.Strategy] = maxTime(s.summary.LastByStrategy[pin.Strategy], pin.At)
		}
		if pin.Topic != "" {
			s.summary.TopicCounts[pin.Topic]++
			s.summary.LastByTopic[pin.Topic] = maxTime(s.summary.LastByTopic[pin.Topic], pin.At)
		}
	}
	s.pins = keep
}

func (s *Session) RaiseDebt(kind DebtKind, amount float64, now time.Time) DebtState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := s.debts[kind]
	if now.Before(state.CooldownUntil) {
		return state
	}
	state.Value += amount
	if state.Value > 1 {
		state.Value = 1
	}
	if state.Value < 0 {
		state.Value = 0
	}
	state.LastRaised = now
	s.debts[kind] = state
	return state
}

func (s *Session) SpendDebt(kind DebtKind, cooldown time.Duration, now time.Time) DebtState {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	state := s.debts[kind]
	state.Value = 0
	state.LastSpent = now
	state.CooldownUntil = now.Add(cooldown)
	s.debts[kind] = state
	return state
}

func cloneSummary(in Summary) Summary {
	out := Summary{
		CountsByKind:     map[PinKind]int{},
		CountsByStrategy: map[string]int{},
		TopicCounts:      map[string]int{},
		LastByKind:       map[PinKind]time.Time{},
		LastByStrategy:   map[string]time.Time{},
		LastByTopic:      map[string]time.Time{},
	}
	for k, v := range in.CountsByKind {
		out.CountsByKind[k] = v
	}
	for k, v := range in.CountsByStrategy {
		out.CountsByStrategy[k] = v
	}
	for k, v := range in.TopicCounts {
		out.TopicCounts[k] = v
	}
	for k, v := range in.LastByKind {
		out.LastByKind[k] = v
	}
	for k, v := range in.LastByStrategy {
		out.LastByStrategy[k] = v
	}
	for k, v := range in.LastByTopic {
		out.LastByTopic[k] = v
	}
	return out
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
