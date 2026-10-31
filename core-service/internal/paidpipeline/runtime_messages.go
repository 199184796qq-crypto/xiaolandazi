package paidpipeline

import (
	"context"
	"fmt"
	"livecompanion/core/internal/messagecaptain"
	"sync"
)

type cutMessageState struct {
	mu         sync.Mutex
	captain    *messagecaptain.Captain
	unregister func()
}

func (p *Processor) SetMessageCaptain(c *messagecaptain.Captain) {
	p.cutMessages.mu.Lock()
	defer p.cutMessages.mu.Unlock()
	if p.cutMessages.unregister != nil {
		p.cutMessages.unregister()
	}
	p.cutMessages.captain = c
	if c != nil {
		p.cutMessages.unregister = c.Register("interaction-scheduler", p.handleRuntimeMessage)
	}
}
func (p *Processor) handleRuntimeMessage(_ context.Context, m messagecaptain.Message) error {
	switch m.Kind {
	case "interaction.v2.welcome-ready":
		return p.readyWelcomeV2(m)
	case "interaction.v2.reserve-audio":
		return p.reserveAudioV2(m)
	}
	p.interactionV2Receipt(m)
	return nil
}
func (p *Processor) cutEpoch(roomID int64) string {
	r := p.runtime.Get(roomID)
	return fmt.Sprintf("%s:%d:%d", r.BootID, r.PlanID, r.RunGeneration)
}
