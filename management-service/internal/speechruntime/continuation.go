package speechruntime

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const ContinuationWindow = 8

// Continuation is a bounded preview checkpoint, not an authoritative fact or
// playback record. Carrying it between requests must not carry the old length
// budget, mutate Core, or make an unplayed preview a live-room observation.
type Continuation struct {
	CompletedUnits int          `json:"completed_units"`
	RecentUnits    []RecentUnit `json:"recent_units"`
	Finish         bool         `json:"finish,omitempty"`
}

type RecentUnit struct {
	PrimaryFactID       string   `json:"primary_fact_id,omitempty"`
	SupportFactIDs      []string `json:"support_fact_ids,omitempty"`
	ContentRole         string   `json:"content_role,omitempty"`
	FactKeys            []string `json:"fact_keys,omitempty"`
	SpeechAct           string   `json:"speech_act,omitempty"`
	ExpressionMove      string   `json:"expression_move,omitempty"`
	ExpressionSignature string   `json:"expression_signature,omitempty"`
	TextTail            string   `json:"text_tail,omitempty"`
}

func (c *Continuation) Validate() error {
	if c == nil {
		return nil
	}
	if c.CompletedUnits < 0 || c.CompletedUnits > 1440 || len(c.RecentUnits) > ContinuationWindow || len(c.RecentUnits) > c.CompletedUnits {
		return errors.New("连续测试游标或最近记录超过允许范围")
	}
	for _, unit := range c.RecentUnits {
		if len(unit.FactKeys) > 3 || len(unit.SupportFactIDs) > 2 || utf8.RuneCountInString(unit.TextTail) > 240 || utf8.RuneCountInString(unit.ExpressionSignature) > 1000 {
			return errors.New("连续测试最近记录过长")
		}
		identifiers := append([]string{unit.PrimaryFactID, unit.ContentRole, unit.SpeechAct, unit.ExpressionMove}, unit.FactKeys...)
		identifiers = append(identifiers, unit.SupportFactIDs...)
		for _, value := range identifiers {
			if utf8.RuneCountInString(value) > 200 {
				return errors.New("连续测试记录标识过长")
			}
		}
	}
	return nil
}

func NewContinuingLedger(target int, previous *Continuation) *Ledger {
	l := NewLedger(target)
	if previous == nil {
		return l
	}
	copy := *previous
	copy.RecentUnits = append([]RecentUnit(nil), previous.RecentUnits...)
	l.continuation = &copy
	for index, unit := range copy.RecentUnits {
		// Indices are local to the new request. Prior accepted units are
		// negative, preserving recency without adding them to this budget.
		lastIndex := index - len(copy.RecentUnits)
		add := func(usage map[string]Usage, key string) {
			if key = strings.TrimSpace(key); key != "" {
				entry := usage[key]
				entry.Count++
				entry.LastIndex = lastIndex
				usage[key] = entry
			}
		}
		for _, key := range unit.FactKeys {
			add(l.factUsage, key)
		}
		add(l.actUsage, unit.SpeechAct)
		add(l.moveUsage, unit.ExpressionMove)
		add(l.signatureUsage, unit.ExpressionSignature)
		for _, marker := range conversationalMarkers {
			if strings.Contains(unit.TextTail, marker) {
				add(l.markerUsage, marker)
			}
		}
		l.tail = runeTail(unit.TextTail, 240)
	}
	return l
}

// Checkpoint advances only after committed units. Pending/failed candidates
// never change future scheduling. The finish instruction is request-local.
func (l *Ledger) Checkpoint() Continuation {
	result := Continuation{RecentUnits: []RecentUnit{}}
	if l.continuation != nil {
		result.CompletedUnits = l.continuation.CompletedUnits
		result.RecentUnits = append(result.RecentUnits, l.continuation.RecentUnits...)
	}
	for _, record := range l.records {
		if record.Status != SegmentCommitted {
			continue
		}
		result.CompletedUnits++
		result.RecentUnits = append(result.RecentUnits, RecentUnit{
			PrimaryFactID: record.Spec.PrimaryFactID, SupportFactIDs: append([]string(nil), record.Spec.SupportFactIDs...), ContentRole: record.Spec.ContentRole,
			FactKeys: append([]string(nil), record.Spec.FactKeys...), SpeechAct: record.Spec.SpeechAct,
			ExpressionMove: record.Spec.ExpressionMove, ExpressionSignature: record.Spec.ExpressionSignature,
			TextTail: runeTail(record.Text, 240),
		})
	}
	if len(result.RecentUnits) > ContinuationWindow {
		result.RecentUnits = result.RecentUnits[len(result.RecentUnits)-ContinuationWindow:]
	}
	return result
}
