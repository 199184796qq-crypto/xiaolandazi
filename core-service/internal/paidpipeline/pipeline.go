package paidpipeline

import (
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/basepipeline"
	"livecompanion/core/internal/model"
	"livecompanion/core/internal/roombrain"
	"livecompanion/core/internal/strategycenter"
	"sync"
	"sync/atomic"
	"time"
)

// Processor owns the single automatic interaction runtime. Missing model
// configuration never falls back to a second heuristic scheduler.
type Processor struct {
	interactionV2  atomic.Pointer[interactionV2State]
	cutMessages    cutMessageState
	scheduleMu     sync.Mutex
	schedules      map[int64]sessionSchedule
	timeReportMu   sync.Mutex
	timeReports    map[int64]*timeReportSchedule
	contextReader  func(int64) (string, string)
	tenantResolver func(int64) (int64, bool)
	warmupEligible func(int64) bool
	audioStarted   func(int64) time.Time
	runtime        *agentwork.Registry
	decisions      *agentdecision.Queue
	policies       *strategycenter.Store
	brain          *roombrain.Manager
	now            func() time.Time
}

func New(runtime *agentwork.Registry, decisions *agentdecision.Queue, policies ...*strategycenter.Store) *Processor {
	p := &Processor{runtime: runtime, decisions: decisions, schedules: make(map[int64]sessionSchedule), timeReports: make(map[int64]*timeReportSchedule), now: func() time.Time { return time.Now().UTC() }}
	if len(policies) > 0 {
		p.policies = policies[0]
	}
	p.setInteractionV2(HTTPPurposeResolver{})
	return p
}

func (p *Processor) SetBrain(brain *roombrain.Manager) {
	if p != nil {
		p.brain = brain
	}
}

func (p *Processor) Handle(event model.RoomEvent, signal basepipeline.Signal) {
	if p == nil || p.runtime == nil || p.decisions == nil {
		return
	}
	switch event.EventType {
	case "session_start":
		p.clearInteractionRoom(event.RoomID)
		return
	case "session_end":
		p.clearInteractionRoom(event.RoomID)
		_, _ = p.runtime.StopAgent(event.RoomID, agentwork.StopReasonLiveFinished)
		return
	}
	if !p.runtime.IsWorking(event.RoomID) {
		return
	}
	p.handleInteractionV2(event, signal)
}

func eventSeed(event model.RoomEvent) string {
	if event.ID > 0 {
		return fmt.Sprintf("event-%d", event.ID)
	}
	return event.OccurredAt.UTC().Format(time.RFC3339Nano) + ":" + event.UserID + ":" + event.EventType
}
