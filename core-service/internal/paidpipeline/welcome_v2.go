package paidpipeline

import (
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/messagecaptain"
)

func (p *Processor) readyWelcomeV2(m messagecaptain.Message) error {
	v := p.interactionV2.Load()
	if v == nil {
		return fmt.Errorf("welcome scheduler unavailable")
	}
	var input struct {
		Decision string `json:"decision_id"`
	}
	if json.Unmarshal(m.Payload, &input) != nil || input.Decision == "" {
		return fmt.Errorf("invalid welcome preparation")
	}
	t, ok := v.engine.ReadyWelcome(m.RoomID, input.Decision, p.clock())
	if !ok {
		return fmt.Errorf("welcome preparation expired or changed")
	}
	if !p.decisions.AdmitWelcome(m.RoomID, input.Decision, t.Work.Expires, t.Content.Knowledge) {
		v.engine.Lost(m.RoomID, input.Decision, p.clock())
		return fmt.Errorf("welcome queue ownership changed")
	}
	return nil
}
