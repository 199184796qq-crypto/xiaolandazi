// Package speechruntime plans the next small unit of a continuous live speech.
//
// It deliberately owns no playback or interruption behavior. Core remains the
// authority for audio scheduling, interruption and resume. This package only
// keeps the generation cursor: what has actually been accepted, which facts
// and expression moves were used recently, and how much of the text budget is
// still outstanding.
package speechruntime

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/model"
)

type SegmentStatus string

const (
	SegmentPending   SegmentStatus = "pending"
	SegmentCommitted SegmentStatus = "committed"
	SegmentDiscarded SegmentStatus = "discarded"
)

type SegmentSpec struct {
	Index                   int      `json:"index"`
	Count                   int      `json:"count"`
	StartSecond             int      `json:"start_second"`
	EndSecond               int      `json:"end_second"`
	SpeechAct               string   `json:"speech_act"`
	Goal                    string   `json:"goal"`
	ContentRole             string   `json:"content_role,omitempty"`
	PrimaryFactID           string   `json:"primary_fact_id,omitempty"`
	SupportFactIDs          []string `json:"support_fact_ids,omitempty"`
	FactKeys                []string `json:"fact_keys,omitempty"`
	BenefitKeys             []string `json:"benefit_keys,omitempty"`
	LinkKeys                []string `json:"link_keys,omitempty"`
	ExpressionMove          string   `json:"expression_move,omitempty"`
	StyleCapabilities       []string `json:"style_capabilities,omitempty"`
	InteractionOpportunity  bool     `json:"interaction_opportunity"`
	SegmentRole             string   `json:"segment_role"`
	ContinuationMode        string   `json:"continuation_mode"`
	OpeningAllowed          bool     `json:"opening_allowed"`
	ClosingAllowed          bool     `json:"closing_allowed"`
	NewcomerReentryAllowed  bool     `json:"newcomer_reentry_allowed"`
	InteractionMode         string   `json:"interaction_mode"`
	PreviousInteractionOpen bool     `json:"previous_interaction_open"`
	PrimaryFactKey          string   `json:"primary_fact_key,omitempty"`
	PreviouslyCoveredFacts  []string `json:"previously_covered_fact_keys,omitempty"`
	ExpressionSignature     string   `json:"expression_signature,omitempty"`
	TargetChars             int      `json:"target_chars"`
	MinChars                int      `json:"min_chars"`
	MaxChars                int      `json:"max_chars"`
	RemainingChars          int      `json:"remaining_chars"`
	ConstraintLevel         string   `json:"constraint_level"`
	FinishMode              string   `json:"finish_mode"`
	PreviousTail            string   `json:"previous_tail,omitempty"`
	AvoidRecent             []string `json:"avoid_recent,omitempty"`
}

type SegmentRecord struct {
	ID          string        `json:"id"`
	Spec        SegmentSpec   `json:"spec"`
	Text        string        `json:"text"`
	ActualChars int           `json:"actual_chars"`
	Status      SegmentStatus `json:"status"`
}

type Usage struct {
	Count     int `json:"count"`
	LastIndex int `json:"last_index"`
}

type Ledger struct {
	totalTarget         int
	records             []SegmentRecord
	factUsage           map[string]Usage
	actUsage            map[string]Usage
	moveUsage           map[string]Usage
	signatureUsage      map[string]Usage
	markerUsage         map[string]Usage
	committed           strings.Builder
	chars               int
	tail                string
	lastInteractionOpen bool
	continuation        *Continuation
}

func NewLedger(totalTarget int) *Ledger {
	if totalTarget < 1 {
		totalTarget = 1
	}
	return &Ledger{
		totalTarget:    totalTarget,
		factUsage:      map[string]Usage{},
		actUsage:       map[string]Usage{},
		moveUsage:      map[string]Usage{},
		signatureUsage: map[string]Usage{},
		markerUsage:    map[string]Usage{},
	}
}

var conversationalMarkers = []string{"嗯", "对呀", "没错", "你看嘛", "所以"}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func expressionSignature(step model.LiveSpeechExpansionStep, primaryFact, move string) string {
	return strings.Join([]string{
		strings.TrimSpace(step.Stage),
		primaryFact,
		firstNonEmpty(step.BenefitKeys),
		firstNonEmpty(step.LinkKeys),
		strings.TrimSpace(move),
	}, "|")
}

func runeTail(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[len(runes)-limit:])
}

func (l *Ledger) CommittedText() string {
	return l.committed.String()
}

func (l *Ledger) CommittedChars() int {
	return l.chars
}

func (l *Ledger) RemainingChars() int {
	remaining := l.totalTarget - l.CommittedChars()
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (l *Ledger) Records() []SegmentRecord {
	return append([]SegmentRecord(nil), l.records...)
}

func (l *Ledger) coveredFactKeys(limit int) []string {
	if limit <= 0 {
		return nil
	}
	result := make([]string, 0, limit)
	seen := map[string]bool{}
	for recordIndex := len(l.records) - 1; recordIndex >= 0; recordIndex-- {
		record := l.records[recordIndex]
		if record.Status != SegmentCommitted {
			continue
		}
		for keyIndex := len(record.Spec.FactKeys) - 1; keyIndex >= 0; keyIndex-- {
			key := strings.TrimSpace(record.Spec.FactKeys[keyIndex])
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, key)
			if len(result) == limit {
				return result
			}
		}
	}
	if l.continuation != nil {
		for index := len(l.continuation.RecentUnits) - 1; index >= 0; index-- {
			for _, key := range l.continuation.RecentUnits[index].FactKeys {
				if key != "" && !seen[key] {
					seen[key] = true
					result = append(result, key)
					if len(result) == limit {
						return result
					}
				}
			}
		}
	}
	return result
}

// AntiChecklistIssues catches the common failure mode where a renderer treats
// conversational particles as a per-segment checklist. One repeated marker can
// be natural; repeating two or more of the same markers in adjacent units is a
// strong mechanical-template signal and should trigger a local rewrite.
func (l *Ledger) AntiChecklistIssues(spec SegmentSpec, text string) []string {
	currentIndex := spec.Index - 1
	repeated := make([]string, 0, len(conversationalMarkers))
	for _, marker := range conversationalMarkers {
		usage, ok := l.markerUsage[marker]
		if ok && usage.LastIndex == currentIndex-1 && strings.Contains(text, marker) {
			repeated = append(repeated, marker)
		}
	}
	if len(repeated) < 2 {
		return nil
	}
	return []string{fmt.Sprintf("与上一小段重复使用多个口头标记（%s），像按清单打卡；保留最多一个，其余改用自然句法承接", strings.Join(repeated, "、"))}
}

func (l *Ledger) pickMove(step model.LiveSpeechExpansionStep) string {
	if len(step.ExpressionMoves) == 0 {
		return ""
	}
	best := strings.TrimSpace(step.ExpressionMoves[0])
	bestLast := int(^uint(0) >> 1)
	for _, candidate := range step.ExpressionMoves {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		usage, ok := l.moveUsage[candidate]
		if !ok {
			return candidate
		}
		if usage.LastIndex < bestLast {
			best, bestLast = candidate, usage.LastIndex
		}
	}
	return best
}

func segmentBounds(target, remainingUnits int) (int, int, string, string) {
	if target < 1 {
		target = 1
	}
	deltaPercent, level, finish := 25, "soft", "continue"
	if remainingUnits <= 2 {
		deltaPercent, level, finish = 12, "tight", "prepare_close"
	}
	if remainingUnits == 1 {
		deltaPercent, level, finish = 5, "closing", "close"
	}
	delta := target * deltaPercent / 100
	if delta < 6 {
		delta = 6
	}
	return max(1, target-delta), target + delta, level, finish
}

// Next converts one virtual-clock step into a small, adaptive generation task.
// Earlier length variance is automatically carried into the remaining units.
func (l *Ledger) Next(step model.LiveSpeechExpansionStep, index, count int) SegmentSpec {
	if count < 1 {
		count = 1
	}
	if index < 0 {
		index = 0
	}
	remainingUnits := count - index
	if remainingUnits < 1 {
		remainingUnits = 1
	}
	remaining := l.RemainingChars()
	target := (remaining + remainingUnits - 1) / remainingUnits
	minChars, maxChars, level, finish := segmentBounds(target, remainingUnits)
	avoid := make([]string, 0, 10)
	for _, key := range step.FactKeys {
		if usage, ok := l.factUsage[key]; ok && index-usage.LastIndex <= 3 {
			avoid = append(avoid, fmt.Sprintf("事实%s最近三小段讲过；若本段仍需使用，必须改变角度、话语动作和链接组合，不能按固定周期复读", key))
		}
	}
	if usage, ok := l.actUsage[step.Stage]; ok && index-usage.LastIndex <= 1 {
		avoid = append(avoid, "上一小段刚使用同类讲话动作，本段应自然换一种推进方式")
	}
	move := l.pickMove(step)
	primaryFact := firstNonEmpty(step.FactKeys)
	signature := expressionSignature(step, primaryFact, move)
	if usage, ok := l.signatureUsage[signature]; signature != "||||" && ok && index-usage.LastIndex <= 3 {
		avoid = append(avoid, "最近三小段已出现相同的事实、角度、链接和话语动作组合；本段必须更换组合，不能只换连接词")
	}
	for _, marker := range conversationalMarkers {
		if usage, ok := l.markerUsage[marker]; ok && index-usage.LastIndex <= 1 {
			avoid = append(avoid, fmt.Sprintf("上一小段已使用口头标记“%s”；本段不要为了模仿再次固定打卡", marker))
		}
	}
	role, continuation := "middle", "semantic_continuation"
	openingAllowed, closingAllowed := false, false
	if index == 0 {
		role, continuation, openingAllowed = "opening", "fresh_open", true
	}
	if index == count-1 {
		role, continuation, closingAllowed = "closing", "semantic_continuation", true
	}
	reentryAllowed := index == 0 || (role == "middle" && step.Stage == "reentry" && step.Room.EntriesPerMinute >= 10)
	if l.continuation != nil {
		openingAllowed = index == 0 && l.continuation.CompletedUnits == 0
		closingAllowed = index == count-1 && l.continuation.Finish
		role, continuation = "middle", "semantic_continuation"
		if openingAllowed {
			role, continuation = "opening", "fresh_open"
		}
		if closingAllowed {
			role = "closing"
		}
		// A request boundary tightens the length budget, not the live show's
		// discourse. Only the caller's final batch may close the show.
		if !l.continuation.Finish {
			finish = "continue"
		}
		reentryAllowed = openingAllowed
	}
	interactionMode := "none"
	if step.InteractionOpportunity && role != "closing" {
		interactionMode = "offer_without_fake_reply"
	}
	return SegmentSpec{
		Index: index + 1, Count: count, StartSecond: step.StartSecond, EndSecond: step.EndSecond,
		SpeechAct: step.Stage, Goal: step.Goal,
		ContentRole: step.ContentRole, PrimaryFactID: step.PrimaryFactID, SupportFactIDs: append([]string(nil), step.SupportFactIDs...),
		FactKeys: append([]string(nil), step.FactKeys...), BenefitKeys: append([]string(nil), step.BenefitKeys...), LinkKeys: append([]string(nil), step.LinkKeys...),
		ExpressionMove: move, StyleCapabilities: append([]string(nil), step.StyleCapabilities...),
		InteractionOpportunity: step.InteractionOpportunity,
		SegmentRole:            role, ContinuationMode: continuation, OpeningAllowed: openingAllowed, ClosingAllowed: closingAllowed,
		NewcomerReentryAllowed: reentryAllowed, InteractionMode: interactionMode, PreviousInteractionOpen: l.lastInteractionOpen,
		PrimaryFactKey: primaryFact, PreviouslyCoveredFacts: l.coveredFactKeys(8), ExpressionSignature: signature,
		TargetChars: target, MinChars: minChars, MaxChars: maxChars, RemainingChars: remaining,
		ConstraintLevel: level, FinishMode: finish, PreviousTail: l.tail, AvoidRecent: avoid,
	}
}

// Enqueue records generated text as pending. Pending text is intentionally not
// part of committed memory until Commit is called. This preserves the boundary
// between generated content and content that was accepted/played.
func (l *Ledger) Enqueue(spec SegmentSpec, text string) string {
	id := fmt.Sprintf("segment-%04d", len(l.records)+1)
	text = strings.TrimSpace(text)
	l.records = append(l.records, SegmentRecord{ID: id, Spec: spec, Text: text, ActualChars: utf8.RuneCountInString(text), Status: SegmentPending})
	return id
}

func (l *Ledger) Commit(id string) bool {
	for index := range l.records {
		record := &l.records[index]
		if record.ID != id || record.Status != SegmentPending {
			continue
		}
		record.Status = SegmentCommitted
		if l.committed.Len() > 0 {
			l.committed.WriteString("\n\n")
			l.chars += 2
		}
		l.committed.WriteString(record.Text)
		l.chars += record.ActualChars
		l.tail = runeTail(l.tail+"\n\n"+record.Text, 240)
		committedIndex := record.Spec.Index - 1
		for _, key := range record.Spec.FactKeys {
			usage := l.factUsage[key]
			usage.Count++
			usage.LastIndex = committedIndex
			l.factUsage[key] = usage
		}
		if record.Spec.SpeechAct != "" {
			usage := l.actUsage[record.Spec.SpeechAct]
			usage.Count++
			usage.LastIndex = committedIndex
			l.actUsage[record.Spec.SpeechAct] = usage
		}
		if record.Spec.ExpressionMove != "" {
			usage := l.moveUsage[record.Spec.ExpressionMove]
			usage.Count++
			usage.LastIndex = committedIndex
			l.moveUsage[record.Spec.ExpressionMove] = usage
		}
		if record.Spec.ExpressionSignature != "" {
			usage := l.signatureUsage[record.Spec.ExpressionSignature]
			usage.Count++
			usage.LastIndex = committedIndex
			l.signatureUsage[record.Spec.ExpressionSignature] = usage
		}
		for _, marker := range conversationalMarkers {
			if strings.Contains(record.Text, marker) {
				usage := l.markerUsage[marker]
				usage.Count++
				usage.LastIndex = committedIndex
				l.markerUsage[marker] = usage
			}
		}
		l.lastInteractionOpen = record.Spec.InteractionMode == "offer_without_fake_reply"
		return true
	}
	return false
}

func (l *Ledger) Discard(id string) bool {
	for index := range l.records {
		if l.records[index].ID == id && l.records[index].Status == SegmentPending {
			l.records[index].Status = SegmentDiscarded
			return true
		}
	}
	return false
}
