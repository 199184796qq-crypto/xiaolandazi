package httpapi

import (
	"context"
	"errors"
	"math/rand"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

type recordingRefreshPosition struct{ Tab, Index int }

// One allowance is shared by the entire script pass. Pending quotes protect
// later tabs from having their allocation consumed by an earlier failed source.
// They are local scheduling reservations, never provider bill entries.
type recordingRefreshSchedule struct {
	Budget             recordingRefreshBudget
	Quotes             [][]int64
	Pending            map[recordingRefreshPosition]int64
	SpentMicros        int64
	Paused             bool
	PauseReason        string
	ForecastDurationMS int64
	InventoryFilling   bool
}

func newRecordingRefreshSchedule(budget recordingRefreshBudget, quotes [][]int64, plans []*recordingUpdatePlan) *recordingRefreshSchedule {
	s := &recordingRefreshSchedule{Budget: budget, Quotes: quotes, Pending: make(map[recordingRefreshPosition]int64)}
	// Round-robin across tabs preserves the per-tab due-work distribution while
	// each tab's list already follows version-count and age priority.
	lists := make([][]int, len(plans))
	for tab, plan := range plans {
		for _, index := range plan.order {
			if plan.Selected[index] {
				lists[tab] = append(lists[tab], index)
			}
		}
	}
	var ranked []recordingRefreshPosition
	for offset := 0; ; offset++ {
		more := false
		for tab, indices := range lists {
			if offset >= len(indices) {
				continue
			}
			more = true
			index := indices[offset]
			ranked = append(ranked, recordingRefreshPosition{tab, index})
		}
		if !more {
			break
		}
	}
	// Compare across tabs too. Stable ties retain the round-robin distribution.
	snapshot := func(position recordingRefreshPosition) (int, int64) {
		plan := plans[position.Tab]
		if position.Index >= len(plan.versions) || position.Index >= len(plan.freshAt) {
			return 0, 0
		}
		return plan.versions[position.Index], plan.freshAt[position.Index]
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		vi, ai := snapshot(ranked[i])
		vj, aj := snapshot(ranked[j])
		if vi != vj {
			return vi < vj
		}
		return ai < aj
	})
	remaining := budget.RoundLimitMicros
	moneyRejected := false
	for _, position := range ranked {
		tab, index := position.Tab, position.Index
		quote := s.quote(tab, index)
		// Accumulated sentence credit can admit one complete long source,
		// but never exceed the shared room's actual monetary headroom.
		if len(s.Pending) == 0 && plans[tab].SingleLong && quote > remaining && quote <= budget.HeadroomMicros {
			s.Budget.RoundLimitMicros = quote
			remaining = quote
		}
		if quote > 0 && quote <= remaining {
			s.Pending[position] = quote
			remaining -= quote
		} else {
			moneyRejected = moneyRejected || quote > remaining
			s.unselect(plans[tab], index)
		}
	}
	// Character-based fairness is only an accrual tool. After a definitively
	// unaffordable selection, consider other due whole sentences using money,
	// otherwise one expensive oldest source could freeze a warm library forever.
	if moneyRejected && remaining > 0 {
		var alternatives []recordingRefreshPosition
		for tab, plan := range plans {
			for _, index := range plan.order {
				if !plan.Selected[index] {
					alternatives = append(alternatives, recordingRefreshPosition{tab, index})
				}
			}
		}
		sort.SliceStable(alternatives, func(i, j int) bool {
			vi, ai := snapshot(alternatives[i])
			vj, aj := snapshot(alternatives[j])
			if vi != vj {
				return vi < vj
			}
			return ai < aj
		})
		for _, position := range alternatives {
			plan := plans[position.Tab]
			quote := s.quote(position.Tab, position.Index)
			if quote > 0 && quote <= remaining && plan.costs[position.Index] <= plan.available {
				plan.Selected[position.Index] = true
				plan.available -= plan.costs[position.Index]
				s.Pending[position] = quote
				remaining -= quote
			}
		}
	}
	for _, plan := range plans {
		n := 0
		for _, yes := range plan.Selected {
			if yes {
				n++
			}
		}
		plan.maxAttempts = min(len(plan.order), n+min(24, (n+1)/2))
	}
	return s
}

func (s *recordingRefreshSchedule) quote(tab, index int) int64 {
	if tab < 0 || tab >= len(s.Quotes) || index < 0 || index >= len(s.Quotes[tab]) {
		return 0
	}
	return s.Quotes[tab][index]
}

func (s *recordingRefreshSchedule) unselect(plan *recordingUpdatePlan, index int) {
	if plan.Selected[index] && !plan.attempted[index] {
		plan.Selected[index] = false
		plan.available += plan.costs[index]
	}
}

func (s *recordingRefreshSchedule) pendingMicros() int64 {
	var total int64
	for _, quote := range s.Pending {
		total += quote
	}
	return total
}

func (s *recordingRefreshSchedule) pause(reason string) { s.Paused, s.PauseReason = true, reason }

func (s *recordingRefreshSchedule) admit(tab, index int, current livecost.Summary, err error) bool {
	return s.admitWithPolicy(tab, index, current, err, livecost.DefaultPolicy())
}

func (s *recordingRefreshSchedule) admitWithPolicy(tab, index int, current livecost.Summary, err error, policy livecost.Policy) bool {
	if s.Paused {
		return false
	}
	if err != nil {
		s.pause("费用账务暂不可用")
		return false
	}
	quote := s.Pending[recordingRefreshPosition{tab, index}]
	if quote <= 0 {
		return false
	}
	headroom := recordingDynamicRefreshBudgetWithPolicy(current, 1, 1, policy.Window, policy).HeadroomMicros
	if quote > headroom || s.SpentMicros > s.Budget.RoundLimitMicros-s.pendingMicros() {
		s.pause("可选更新预算暂不足")
		return false
	}
	return true
}

// A precomputed pass never freezes a room's budget. The next optional source
// sees a fresh settings read and summary; the request transaction checks again.
func (s *Server) admitRecordingRefresh(ctx context.Context, schedule *recordingRefreshSchedule, tenant, room int64, tab, index int) bool {
	settings, valid := s.liveCostSettingsSnapshot(ctx)
	if !valid || settings.Validate() != nil {
		schedule.pause("更新费用设置暂不可用")
		return false
	}
	policy := settings.Policy()
	current, err := s.liveCostController().Summary(livecost.WithPolicy(ctx, policy), livecost.Scope{TenantID: tenant, RoomID: room})
	return schedule.admitWithPolicy(tab, index, current, err, policy)
}

func (s *recordingRefreshSchedule) complete(tab, index int, events *liveCostCallEvents) {
	delete(s.Pending, recordingRefreshPosition{tab, index})
	if events == nil {
		s.pause("更新请求缺少费用回执")
		return
	}
	s.SpentMicros += events.ChargedMicros.Load()
	switch {
	case events.AccountingUnavailable.Load():
		s.pause("更新请求记账暂不可用")
	case events.Uncertain.Load():
		s.pause("更新请求结果待核对")
	case events.OptionalDeclined.Load():
		s.pause("可选更新预算暂不足")
	case s.SpentMicros >= s.Budget.RoundLimitMicros:
		s.pause("本轮可选更新额度已用完")
	}
}

func (s *recordingRefreshSchedule) replace(planner *recordingUpdatePlanner, plan *recordingUpdatePlan, tab, index int) int {
	if s.Paused {
		plan.paused = true
		return 0
	}
	added := 0
	for _, candidate := range planner.ReplaceFailed(plan, index, index+1) {
		quote := s.quote(tab, candidate)
		if quote > 0 && quote <= s.Budget.RoundLimitMicros-s.SpentMicros-s.pendingMicros() {
			s.Pending[recordingRefreshPosition{tab, candidate}] = quote
			added++
		} else {
			s.unselect(plan, candidate)
		}
	}
	return added
}

// Planning is read-only: no model, TTS or asset download. The actual request
// transaction still validates the latest shared-room budget before sending.
func (s *Server) prepareRecordingRefreshSchedule(ctx context.Context, tenant, room int64, version model.LiveAgentPlanVersion, policy *anchorcycle.State,
	tabs [][]recordingSentenceState, hitCandidates func(int, int) []recordingcache.Entry, planner *recordingUpdatePlanner, tabIDs []string, key string, elapsed time.Duration, first bool, rng *rand.Rand) (*recordingRefreshSchedule, []*recordingUpdatePlan) {
	withoutOptional := func(reason string) (*recordingRefreshSchedule, []*recordingUpdatePlan) {
		plans := make([]*recordingUpdatePlan, len(tabs))
		for tab := range tabs {
			plans[tab] = planner.PlanBounded(tabIDs[tab], key, tabs[tab], 0, 0, rng)
		}
		result := newRecordingRefreshSchedule(recordingRefreshBudget{}, nil, plans)
		if reason != "" {
			result.pause(reason)
		}
		return result, plans
	}
	allFull, anyWarm, inventoryScanned := true, false, false
	var forecast time.Duration
	snapshotInventory := func() {
		if inventoryScanned {
			return
		}
		inventoryScanned = true
		for tab := range tabs {
			for index := range tabs[tab] {
				state := &tabs[tab][index]
				cached := hitCandidates(tab, index)
				state.VersionCount = recordingEffectiveVersionCount(state.Source, state.Versions, cached, policy)
				state.InventoryKnown = true
				state.UpdateEligible = state.VersionCount > 0
				anyWarm = anyWarm || state.UpdateEligible
				if state.VersionCount > 0 && state.VersionCount < recordingCacheMaxVersions {
					allFull = false
				}
				duration := recordingRefreshSourceDuration(state.Source, state.Versions, cached, policy)
				if duration > time.Duration(1<<63-1)-forecast {
					forecast = time.Duration(1<<63 - 1)
				} else {
					forecast += duration
				}
			}
		}
	}
	if first {
		// A new session can inherit an unfinished baseline pass. Only a truly
		// cold library skips optional planning; warm sources join the normal
		// whole-pass budget immediately, while missing audio stays necessary.
		// This inspects scoped metadata only, without pricing or asset I/O.
		snapshotInventory()
		if !anyWarm {
			return withoutOptional("")
		}
	}
	settings, settingsValid := s.liveCostSettingsSnapshot(ctx)
	if !settingsValid || settings.Validate() != nil {
		return withoutOptional("更新费用设置暂不可用")
	}
	budgetPolicy := settings.Policy()
	if budgetPolicy.MainlineTargetMicros == 0 || budgetPolicy.OptionalLimitMicros == 0 {
		return withoutOptional("当前可选更新额度不足")
	}
	summary, summaryErr := s.liveCostController().Summary(livecost.WithPolicy(ctx, budgetPolicy), livecost.Scope{TenantID: tenant, RoomID: room})
	if summaryErr != nil {
		return withoutOptional("更新费用预估或账务暂不可用")
	}
	snapshotInventory()
	quotes := make([][]int64, len(tabs))
	var totalQuote int64
	for tab := range tabs {
		quotes[tab] = make([]int64, len(tabs[tab]))
		for index := range tabs[tab] {
			state := &tabs[tab][index]
			// Fewer versions rank first in PlanBounded and across-tab allocation.
			// They must not exclude full pools: a few duplicate-only tail sources
			// can otherwise strand the entire round's refresh allowance for hours.
			if !state.UpdateEligible || summaryErr != nil {
				continue
			}
			quote, err := s.recordingRefreshQuoteWithSettings(state.Source, version, policy, settings)
			if err != nil {
				summaryErr = errors.New("更新费用预估暂不可用")
				continue
			}
			quotes[tab][index] = quote
			totalQuote += quote
		}
	}
	budget := recordingDynamicRefreshBudgetForDurationWithPolicy(summary, totalQuote, elapsed, forecast, budgetPolicy)
	if summaryErr != nil {
		budget = recordingRefreshBudget{}
	}
	plans := make([]*recordingUpdatePlan, len(tabs))
	for tab := range tabs {
		plans[tab] = planner.PlanBounded(tabIDs[tab], key, tabs[tab], budget.Percent, 100, rng)
	}
	result := newRecordingRefreshSchedule(budget, quotes, plans)
	result.ForecastDurationMS = forecast.Milliseconds()
	result.InventoryFilling = !allFull
	if summaryErr != nil {
		result.pause("更新费用预估或账务暂不可用")
	} else if totalQuote > 0 && budget.Percent == 0 {
		result.pause("当前可选更新额度不足")
	}
	return result, plans
}

// Existing audio duration paces the next pass; it never represents actual
// playback or a charge. Missing assets fall back to a text-duration forecast.
func recordingRefreshSourceDuration(source string, versions [][]recordingReadyUnit, cached []recordingcache.Entry, policy *anchorcycle.State) time.Duration {
	fromMS := func(values []int64) time.Duration {
		var sum int64
		for _, value := range values {
			if value <= 0 || value > int64((time.Duration(1<<63-1)/time.Millisecond))-sum {
				return 0
			}
			sum += value
		}
		return time.Duration(sum) * time.Millisecond
	}
	for index := len(versions) - 1; index >= 0; index-- {
		if recordingEffectiveVersionCount(source, versions[index:index+1], nil, policy) == 0 {
			continue
		}
		values := make([]int64, len(versions[index]))
		for i, unit := range versions[index] {
			values[i] = int64(unit.Track.DurationMS)
		}
		if duration := fromMS(values); duration > 0 {
			return duration
		}
	}
	for index := len(cached) - 1; index >= 0; index-- {
		if recordingEffectiveVersionCount(source, nil, cached[index:index+1], policy) == 0 {
			continue
		}
		entry := cached[index]
		values := []int64{entry.Duration}
		if len(entry.Parts) > 0 {
			values = make([]int64, len(entry.Parts))
			for i, part := range entry.Parts {
				values[i] = part.Duration
			}
		}
		if duration := fromMS(values); duration > 0 {
			return duration
		}
	}
	return time.Duration(max(1, utf8.RuneCountInString(source))) * time.Minute / 220
}

// Novelty is checked before buying audio. A new asset with identical words is
// not an effective optional wording update, even though a returned model call
// is still charged independently by its receipt.
func recordingKnownSpokenVersions(source string, versions [][]recordingReadyUnit, cached []recordingcache.Entry, policy *anchorcycle.State) []string {
	var texts []string
	for _, units := range versions {
		if recordingEffectiveVersionCount(source, [][]recordingReadyUnit{units}, nil, policy) > 0 {
			texts = append(texts, strings.Join(recordingUnitTexts(units), ""))
		}
	}
	for _, entry := range cached {
		if recordingEffectiveVersionCount(source, nil, []recordingcache.Entry{entry}, policy) == 0 {
			continue
		}
		if len(entry.Parts) == 0 {
			texts = append(texts, entry.Spoken)
			continue
		}
		var text strings.Builder
		for _, part := range entry.Parts {
			text.WriteString(part.Spoken)
		}
		texts = append(texts, text.String())
	}
	return texts
}

func recordingRefreshNoveltyError(ctx context.Context, source, spoken string, existing ...string) error {
	if liveCostScope(ctx).Priority != livecost.PriorityOptional {
		return nil
	}
	if sameSpeechText(source, spoken) {
		return errors.New("本次改写未产生新表达，保留已有声音")
	}
	for _, previous := range existing {
		if sameSpeechText(previous, spoken) {
			return errors.New("本次改写与已有版本重复，保留已有声音")
		}
	}
	return nil
}
