package httpapi

import (
	"math/rand"
	"sort"
	"strings"
	"time"
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
	SentenceFreshAt []int64  `json:"sentence_fresh_at,omitempty"`
}

type recordingUpdatePlan struct {
	Selected     []bool
	TabID        string
	ContentKey   string
	Revision     uint64
	costs        []int64
	committed    map[int]bool
	SingleLong   bool
	order        []int
	attempted    map[int]bool
	failed       map[int]bool
	available    int64
	maxAttempts  int
	attemptCount int
	paused       bool
	versions     []int   // Immutable inventory snapshot for cross-tab ordering.
	freshAt      []int64 // Owned copy; later commits cannot rewrite this plan.
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
	next.SentenceFreshAt = make([]int64, len(newSources))
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
		if len(previous.SentenceFreshAt) == len(newSources) {
			copy(next.SentenceFreshAt, previous.SentenceFreshAt)
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
			// A freshly edited source must not jump ahead of unchanged old
			// content merely because its optional rewrite sequence is zero.
			next.SentenceFreshAt[index] = time.Now().UTC().UnixMilli()
			if oldCounts[source] == 1 && newCounts[source] == 1 && len(previous.SentenceUpdated) == len(oldSources) {
				next.SentenceUpdated[index] = previous.SentenceUpdated[oldIndices[source]]
				next.SentenceFreshAt[index] = 0
				if len(previous.SentenceFreshAt) == len(oldSources) {
					next.SentenceFreshAt[index] = previous.SentenceFreshAt[oldIndices[source]]
				}
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
	return planner.PlanBounded(tabID, accrualKey, states, percent, 100, rng)
}

// PlanBounded caps this pass even when earlier failures accumulated a full
// tab of debt. One oldest complete sentence may exceed the percentage cap,
// once its credit has accrued; its monetary quote still needs admission.
func (planner *recordingUpdatePlanner) PlanBounded(tabID, accrualKey string, states []recordingSentenceState, percent, maxPercent int, rng *rand.Rand) *recordingUpdatePlan {
	plan := &recordingUpdatePlan{Selected: make([]bool, len(states)), TabID: strings.TrimSpace(tabID),
		costs: make([]int64, len(states)), committed: make(map[int]bool), attempted: make(map[int]bool), failed: make(map[int]bool),
		versions: make([]int, len(states)), freshAt: make([]int64, len(states))}
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
		if state.InventoryKnown {
			plan.versions[index] = max(0, state.VersionCount)
		}
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
	if len(credit.SentenceFreshAt) != len(states) {
		credit.SentenceFreshAt = make([]int64, len(states))
	}
	copy(plan.freshAt, credit.SentenceFreshAt)
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
	shuffled := make([]int, len(states))
	if rng != nil {
		shuffled = rng.Perm(len(states))
	} else {
		for index := range shuffled {
			shuffled[index] = index
		}
	}
	// Inventory admission is one snapshot shared by the initial selection and
	// failed-work replacement. Missing inventory metadata keeps legacy callers
	// compatible; a known ineligible source must never block an eligible long
	// sentence or become an unplanned replacement later in the same pass.
	order := make([]int, 0, len(shuffled))
	for _, index := range shuffled {
		if plan.costs[index] > 0 && (!states[index].InventoryKnown || states[index].UpdateEligible) {
			order = append(order, index)
		}
	}
	sort.SliceStable(order, func(left, right int) bool {
		if plan.versions[order[left]] != plan.versions[order[right]] {
			return plan.versions[order[left]] < plan.versions[order[right]]
		}
		if credit.SentenceFreshAt[order[left]] != credit.SentenceFreshAt[order[right]] {
			return credit.SentenceFreshAt[order[left]] < credit.SentenceFreshAt[order[right]]
		}
		if credit.SentenceUpdated[order[left]] != credit.SentenceUpdated[order[right]] {
			return credit.SentenceUpdated[order[left]] < credit.SentenceUpdated[order[right]]
		}
		return states[order[left]].Refreshed < states[order[right]].Refreshed
	})
	plan.order = order
	maxPercent = max(0, min(100, maxPercent))
	available := min(credit.CreditMicros, total*int64(maxPercent)/100)
	if maxPercent > 0 && len(order) > 0 && plan.costs[order[0]] > available && credit.CreditMicros >= plan.costs[order[0]] {
		available = plan.costs[order[0]]
		plan.SingleLong = true
	}
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
	plan.available = available
	selected := 0
	for _, yes := range plan.Selected {
		if yes {
			selected++
		}
	}
	plan.maxAttempts = min(len(order), selected+min(24, (selected+1)/2))
	return plan
}

func (planner *recordingUpdatePlanner) Commit(plan *recordingUpdatePlan, index int) bool {
	return planner.CommitAt(plan, index, time.Now().UTC())
}

func (planner *recordingUpdatePlanner) CommitAt(plan *recordingUpdatePlan, index int, at time.Time) bool {
	if planner == nil || plan == nil || index < 0 || index >= len(plan.Selected) || !plan.Selected[index] || plan.committed[index] || plan.failed[index] {
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
	if len(credit.SentenceFreshAt) != len(plan.Selected) {
		credit.SentenceFreshAt = make([]int64, len(plan.Selected))
	}
	credit.SentenceFreshAt[index] = max(int64(1), at.UTC().UnixMilli())
	planner.Tabs[plan.TabID] = credit
	plan.committed[index] = true
	return true
}

func (planner *recordingUpdatePlanner) currentPlan(plan *recordingUpdatePlan) bool {
	if planner == nil || plan == nil {
		return false
	}
	credit, ok := planner.Tabs[plan.TabID]
	return ok && credit.ContentKey == plan.ContentKey && credit.Revision == plan.Revision
}

// BeginAttempt fences actual optional source attempts. It does not imply a
// model request was sent, a charge was incurred or a rewrite was successful.
func (planner *recordingUpdatePlanner) BeginAttempt(plan *recordingUpdatePlan, index int) bool {
	if !planner.currentPlan(plan) || plan.paused || index < 0 || index >= len(plan.Selected) || !plan.Selected[index] || plan.attempted[index] || plan.committed[index] || plan.attemptCount >= plan.maxAttempts {
		return false
	}
	plan.attempted[index] = true
	plan.attemptCount++
	return true
}

// ReplaceFailed spends no successful-update credit and never revisits content
// already passed by the producer. The failed model's real charge stays in the
// independent request ledger. Each candidate is attempted at most once/pass.
func (planner *recordingUpdatePlanner) ReplaceFailed(plan *recordingUpdatePlan, index, nextIndex int) []int {
	if !planner.currentPlan(plan) || plan.paused || index < 0 || index >= len(plan.Selected) || !plan.attempted[index] || plan.committed[index] || plan.failed[index] {
		return nil
	}
	plan.failed[index] = true
	plan.available += plan.costs[index]
	var added []int
	pending := 0
	for i, yes := range plan.Selected {
		if yes && !plan.attempted[i] {
			pending++
		}
	}
	for _, candidate := range plan.order {
		if candidate < max(index+1, nextIndex) || plan.Selected[candidate] || plan.attempted[candidate] || plan.costs[candidate] <= 0 {
			continue
		}
		if plan.attemptCount+pending >= plan.maxAttempts {
			break
		}
		if plan.costs[candidate] > plan.available {
			continue
		}
		plan.Selected[candidate] = true
		plan.available -= plan.costs[candidate]
		pending++
		added = append(added, candidate)
	}
	return added
}
