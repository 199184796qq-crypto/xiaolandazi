package strategycenter

import "fmt"

var InteractionControlKinds = []string{"welcome", "like", "follow", "order", "guide", "semantic", "urgent", "time_report", "dialogue"}

// Missing controls retain the previous room behavior. An explicit false is
// different from an omitted key. Participation shares the semantic control.
func ControlKind(kind string) string {
	switch kind {
	case "question", "chat", "engagement", "reply_question", "reply_chat":
		return "semantic"
	case "welcome_named", "welcome_batch":
		return "welcome"
	case "reply_like":
		return "like"
	case "reply_follow":
		return "follow"
	case "conversion_signal", "order_single", "order_count":
		return "order"
	case "warmup":
		return "guide"
	case "complaint":
		return "urgent"
	case "clarify", "dialogue_continue":
		return "dialogue"
	}
	return kind
}
func (p RoomInteractionPreferences) ControlsActive() bool {
	if p.InteractionActive != nil && !*p.InteractionActive {
		return false
	}
	for _, k := range InteractionControlKinds {
		if on, ok := p.Switches[k]; !ok || on {
			return true
		}
	}
	return false
}
func (p RoomInteractionPreferences) ControlEnabled(kind string) bool {
	if !p.ControlsActive() {
		return false
	}
	on, ok := p.Switches[ControlKind(kind)]
	return !ok || on
}
func ValidateInteractionControls(p RoomInteractionPreferences) error {
	known := map[string]bool{}
	for _, k := range InteractionControlKinds {
		known[k] = true
	}
	for k, v := range p.DisabledAt {
		if !known[k] || v > p.ControlsRevision {
			return fmt.Errorf("无效的互动作废版本")
		}
	}
	for k := range p.Switches {
		if !known[k] {
			return fmt.Errorf("未知互动类型：%s", k)
		}
	}
	for k, seconds := range p.Cooldowns {
		if !known[k] || k == "urgent" {
			return fmt.Errorf("该互动不支持冷却设置：%s", k)
		}
		if seconds < 0 || seconds > 3600 {
			return fmt.Errorf("互动冷却必须为0到3600秒的整数")
		}
	}
	return nil
}
func cloneInteractionControls(p RoomInteractionPreferences) RoomInteractionPreferences {
	if p.DisabledAt != nil {
		m := map[string]uint64{}
		for k, v := range p.DisabledAt {
			m[k] = v
		}
		p.DisabledAt = m
	}
	if p.InteractionActive != nil {
		v := *p.InteractionActive
		p.InteractionActive = &v
	}
	if p.Switches != nil {
		m := make(map[string]bool, len(p.Switches))
		for k, v := range p.Switches {
			m[k] = v
		}
		p.Switches = m
	}
	if p.Cooldowns != nil {
		m := make(map[string]int, len(p.Cooldowns))
		for k, v := range p.Cooldowns {
			m[k] = v
		}
		p.Cooldowns = m
	}
	return p
}
