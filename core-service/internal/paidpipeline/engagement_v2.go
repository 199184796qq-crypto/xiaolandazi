package paidpipeline

import (
	"fmt"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/roomaudio"
	"time"
)

// ObserveSpoken connects the actual PCM stream to the participation scheduler.
// It performs no network, database, generation or playback-control operation.
func (p *Processor) ObserveSpoken(segment roomaudio.SpokenSegment) {
	if p == nil || p.runtime == nil {
		return
	}
	v := p.interactionV2.Load()
	if v == nil {
		return
	}
	run := p.runtime.Get(segment.RoomID)
	if run.State != agentwork.StateWorking || run.PlanID <= 0 || run.WorkingSince == nil || segment.At.Before(*run.WorkingSince) {
		return
	}
	tenant := int64(0)
	v.mu.Lock()
	if s, ok := v.rooms[segment.RoomID]; ok && s.Epoch == p.cutEpoch(segment.RoomID) {
		tenant = s.Tenant
	}
	v.mu.Unlock()
	if tenant <= 0 && p.policies != nil {
		tenant = p.policies.RoomInteractionPreferences(segment.RoomID).TenantID
	}
	// The callback stays entirely in memory: do not use tenantResolver, which
	// may read a database. The next accepted event/tick establishes room scope.
	if tenant <= 0 {
		return
	}
	s := p.v2Scope(tenant, segment.RoomID)
	if s.Epoch != fmt.Sprintf("%s:%d:%d", run.BootID, run.PlanID, run.RunGeneration) {
		return
	}
	id := fmt.Sprintf("pcm:%s:%d:%d", segment.Source, segment.Sequence, segment.At.UnixNano())
	v.engine.ObserveSpokenWindow(s, id, segment.Text, segment.At, segment.At.Add(time.Duration(segment.RemainingMS)*time.Millisecond), p.clock())
}
