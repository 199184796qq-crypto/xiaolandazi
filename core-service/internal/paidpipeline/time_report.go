package paidpipeline

import (
	"fmt"
	"strings"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/strategycenter"
)

const timeReportRetryDelay = 10 * time.Second

// A scheduling period owes exactly one completed report. Attempts (including
// successful dispatch/start) do not advance the period. State is run scoped.
type timeReportSchedule struct {
	Epoch, Topic, Decision string
	Due, RetryAt           time.Time
	Sequence               uint64
}

func (p *Processor) flushTimeReports(now time.Time) {
	if p.brain == nil || p.runtime == nil || p.decisions == nil {
		return
	}
	p.timeReportMu.Lock()
	defer p.timeReportMu.Unlock()
	if p.timeReports == nil {
		p.timeReports = make(map[int64]*timeReportSchedule)
	}
	active := make(map[int64]bool)
	for _, r := range p.runtime.Snapshots() {
		if r.State != agentwork.StateWorking || r.Mode != agentwork.ModeAnchor || r.PlanID <= 0 {
			continue
		}
		active[r.RoomID] = true
		epoch := p.cutEpoch(r.RoomID)
		state := p.timeReports[r.RoomID]
		if state == nil || state.Epoch != epoch {
			if state != nil && state.Decision != "" {
				p.decisions.Remove(r.RoomID, state.Decision)
			}
			base := now
			if r.WorkingSince != nil {
				base = *r.WorkingSince
			}
			state = &timeReportSchedule{Epoch: epoch, Due: base.Add(nextTimeReportDelay())}
			p.timeReports[r.RoomID] = state
		}
		if state.Decision != "" {
			view := p.decisions.Snapshot(r.RoomID)
			finished := false
			for _, receipt := range view.TerminalHistory {
				if receipt.DecisionID == state.Decision && receipt.Status == "COMPLETED" && receipt.Reason == "actual_mainline_return" {
					state.Due = receipt.At.Add(nextTimeReportDelay())
					state.Topic, state.Decision, state.RetryAt = "", "", time.Time{}
					finished = true
					break
				}
			}
			if finished {
				continue
			}
			waiting := false
			for _, item := range view.Queue {
				if item.ID == state.Decision {
					waiting = true
					break
				}
			}
			if waiting {
				continue
			}
			// Expired, removed or failed: keep the same entitlement/content key.
			state.Decision, state.RetryAt = "", now.Add(timeReportRetryDelay)
		}
		if now.Before(state.Due) || now.Before(state.RetryAt) {
			continue
		}
		if view, err := p.brain.Snapshot(r.RoomID); err != nil || view.Intelligence.OnlineCount < 0 {
			continue
		}
		prefs := strategycenter.RoomInteractionPreferences{}
		if p.policies != nil {
			prefs = p.policies.RoomInteractionPreferences(r.RoomID)
		}
		if prefs.TenantID <= 0 {
			p.scheduleMu.Lock()
			resolver := p.tenantResolver
			p.scheduleMu.Unlock()
			if resolver == nil {
				continue
			}
			tenant, ok := resolver(r.RoomID)
			if !ok || tenant <= 0 {
				continue
			}
			prefs.TenantID = tenant
		}
		if state.Topic == "" {
			state.Sequence++
			state.Topic = strings.ToUpper(fmt.Sprintf("INTERACTION:TIME_REPORT:%s:%d", epoch, state.Sequence))
		}
		candidate := agentdecision.Candidate{
			Source: agentdecision.SourceAgent, Topic: state.Topic, Title: "时间互动",
			MissionKind: "time_report", Scene: "time_report", Priority: 100,
			Question:            "结合当前时间自然提一句时间，不要像报时钟",
			InteractionDecision: agentdecision.InteractionDecision{Handle: true, PrimaryEvent: "time_report", Reason: "本场自然时间互动，完成播报后核销本次配额"},
		}
		if p.decorateCandidate(r.RoomID, prefs.TenantID, &candidate) {
			candidate.Priority = 100
			if admitted := p.decisions.Enqueue(r.RoomID, candidate).Item; admitted != nil {
				state.Decision = admitted.ID
			}
		}
		state.RetryAt = now.Add(timeReportRetryDelay)
	}
	for room, state := range p.timeReports {
		if !active[room] {
			if state.Decision != "" {
				p.decisions.Remove(room, state.Decision)
			}
			delete(p.timeReports, room)
		}
	}
}
