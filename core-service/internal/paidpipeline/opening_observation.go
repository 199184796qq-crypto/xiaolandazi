package paidpipeline

import (
	"livecompanion/core/internal/agentwork"
	"time"
)

func (p *Processor) SetAudioStartedReader(reader func(int64) time.Time) {
	p.scheduleMu.Lock()
	p.audioStarted = reader
	p.scheduleMu.Unlock()
}

func (p *Processor) InteractionsAllowed(roomID, tenantID int64) bool {
	if p.runtime.Get(roomID).Mode == agentwork.ModeControl {
		return true
	}
	p.scheduleMu.Lock()
	reader := p.audioStarted
	p.scheduleMu.Unlock()
	if reader == nil {
		return true
	}
	cfg, err := p.schedule(roomID, tenantID)
	if err != nil {
		return false
	}
	seconds := 120
	if cfg.OpeningObservationSeconds != nil {
		seconds = *cfg.OpeningObservationSeconds
	}
	started := reader(roomID)
	return !started.IsZero() && p.clock().Sub(started) >= time.Duration(seconds)*time.Second
}
