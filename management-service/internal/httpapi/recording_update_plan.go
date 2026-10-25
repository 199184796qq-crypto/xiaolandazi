package httpapi

import (
	"math/rand"
	"sort"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/recordingcache"
)

const recordingUpdateCreditScale int64 = 1_000_000

// recordingUpdatePlanner is persisted with the cache manifest. Credit measures
// source characters due for optional refresh, not money or a provider bill.
// Stable tab IDs keep balances attached to content when playback order changes.
type recordingUpdatePlanner struct {
	Version int                              `json:"version"`
	Tabs    map[string]recordingUpdateCredit `json:"tabs"`
}

type recordingUpdateCredit struct {
	ContentKey      string   `json:"content_key"`
	CreditMicros    int64    `json:"credit_micros"`
	LastAccrualKey  string   `json:"last_accrual_key"`
	Revision        uint64   `json:"revision"`
	UpdateSequence  uint64   `json:"update_sequence,omitempty"`
	SentenceUpdated []uint64 `json:"sentence_updated,omitempty"`
}

type recordingUpdatePlan struct {
	Selected   []bool
	TabID      string
	ContentKey string
	Revision   uint64
	costs      []int64
	committed  map[int]bool
}

// MigrateTab carries a stable tab's existing update balance into an explicitly
// compatible content revision. The caller must establish fact/voice/policy
// compatibility before calling; matching text alone cannot establish that.
// A nil oldSources means the previous source snapshot is unavailable.
// Migration never accrues credit and always invalidates older plan handles.
func (planner *recordingUpdatePlanner) MigrateTab(tabID string, oldSources, newSources []string) bool {
	if planner == nil || strings.TrimSpace(tabID) == "" {
		return false
	}
	tabID = strings.TrimSpace(tabID)
	previous, ok := planner.Tabs[tabID]
	if !ok {
		return false
	}
	contentKey := recordingcache.ScriptHash(newSources)
	var total int64
	for _, source := range newSources {
		total += int64(utf8.RuneCountInString(strings.TrimSpace(source))) * recordingUpdateCreditScale
	}
	next := previous
	next.ContentKey = contentKey
	next.Revision++
	// Own the new slice: a migrated state must not mutate the old snapshot or
	// another tab's planning state through a shared backing array.
	next.SentenceUpdated = make([]uint64, len(newSources))
	for _, sequence := range previous.SentenceUpdated {
		if sequence > next.UpdateSequence {
			next.UpdateSequence = sequence
		}
	}
	sameContent := previous.ContentKey == contentKey
	trustedSnapshot := oldSources != nil && previous.ContentKey == recordingcache.ScriptHash(oldSources)
	if sameContent {
		if len(previous.SentenceUpdated) == len(newSources) {
			copy(next.SentenceUpdated, previous.SentenceUpdated)
		}
	} else if trustedSnapshot {
		oldCounts := make(map[string]int, len(oldSources))
		newCounts := make(map[string]int, len(newSources))
		oldIndices := make(map[string]int, len(oldSources))
		for index, source := range oldSources {
			oldCounts[source]++
			oldIndices[source] = index
		}
		for _, source := range newSources {
			newCounts[source]++
		}
		for index, source := range newSources {
			if oldCounts[source] == 1 && newCounts[source] == 1 && len(previous.SentenceUpdated) == len(oldSources) {
				next.SentenceUpdated[index] = previous.SentenceUpdated[oldIndices[source]]
			}
		}
	} else {
		// No reliable old snapshot: retain only a completely unchanged tab.
		// An edited tab cannot borrow balance/order from an unverified layout.
		next.CreditMicros = 0
		next.UpdateSequence = 0
	}
	if next.CreditMicros < 0 {
		next.CreditMicros = 0
	}
	if next.CreditMicros > total {
		next.CreditMicros = total
	}
	planner.Version = 1
	planner.Tabs[tabID] = next
	return true
}

// Plan accrues once for a caller-supplied session+round key and selects the
// oldest eligible source sentences whose character costs fit the balance.
// Selection reserves local planning space only. Commit deducts a sentence only
// after a new playable version actually succeeds. Failed or budget-denied work
// leaves credit intact; credit never exceeds one complete pass of this tab.
func (planner *recordingUpdatePlanner) Plan(tabID, accrualKey string, states []recordingSentenceState, percent int, rng *rand.Rand) *recordingUpdatePlan {
	plan := &recordingUpdatePlan{Selected: make([]bool, len(states)), TabID: strings.TrimSpace(tabID),
		costs: make([]int64, len(states)), committed: make(map[int]bool)}
	if planner == nil || plan.TabID == "" || strings.TrimSpace(accrualKey) == "" {
		return plan
	}
	if planner.Tabs == nil {
		planner.Tabs = make(map[string]recordingUpdateCredit)
	}
	planner.Version = 1
	sources := make([]string, len(states))
	var total int64
	for index, state := range states {
		sources[index] = state.Source
		plan.costs[index] = int64(utf8.RuneCountInString(strings.TrimSpace(state.Source))) * recordingUpdateCreditScale
		total += plan.costs[index]
	}
	plan.ContentKey = recordingcache.ScriptHash(sources)
	credit := planner.Tabs[plan.TabID]
	if credit.ContentKey != plan.ContentKey {
		credit = recordingUpdateCredit{ContentKey: plan.ContentKey, Revision: credit.Revision}
	}
	if len(credit.SentenceUpdated) != len(states) {
		credit.SentenceUpdated = make([]uint64, len(states))
		credit.UpdateSequence = 0
	}
	if credit.CreditMicros < 0 {
		credit.CreditMicros = 0
	}
	if credit.CreditMicros > total {
		credit.CreditMicros = total
	}
	if percent > 100 {
		percent = 100
	}
	if percent > 0 && credit.LastAccrualKey != accrualKey {
		credit.CreditMicros += total * int64(percent) / 100
		if credit.CreditMicros > total {
			credit.CreditMicros = total
		}
		credit.LastAccrualKey = accrualKey
	}
	credit.Revision++ // A later plan invalidates uncommitted older plan handles.
	plan.Revision = credit.Revision
	planner.Tabs[plan.TabID] = credit
	if percent <= 0 || total == 0 {
		return plan
	}
	order := make([]int, len(states))
	if rng != nil {
		order = rng.Perm(len(states))
	} else {
		for index := range order {
			order[index] = index
		}
	}
	sort.SliceStable(order, func(left, right int) bool {
		if credit.SentenceUpdated[order[left]] != credit.SentenceUpdated[order[right]] {
			return credit.SentenceUpdated[order[left]] < credit.SentenceUpdated[order[right]]
		}
		return states[order[left]].Refreshed < states[order[right]].Refreshed
	})
	available := credit.CreditMicros
	for _, index := range order {
		cost := plan.costs[index]
		if cost <= 0 {
			continue
		}
		// Save for the oldest full sentence instead of repeatedly spending
		// the balance on shorter, newer ones and starving a long sentence.
		if cost > available {
			break
		}
		if cost <= available {
			plan.Selected[index] = true
			available -= cost
		}
	}
	return plan
}

func (planner *recordingUpdatePlanner) Commit(plan *recordingUpdatePlan, index int) bool {
	if planner == nil || plan == nil || index < 0 || index >= len(plan.Selected) || !plan.Selected[index] || plan.committed[index] {
		return false
	}
	credit, ok := planner.Tabs[plan.TabID]
	if !ok || credit.ContentKey != plan.ContentKey || credit.Revision != plan.Revision ||
		plan.costs[index] <= 0 || credit.CreditMicros < plan.costs[index] {
		return false
	}
	credit.CreditMicros -= plan.costs[index]
	credit.UpdateSequence++
	credit.SentenceUpdated[index] = credit.UpdateSequence
	planner.Tabs[plan.TabID] = credit
	plan.committed[index] = true
	return true
}
