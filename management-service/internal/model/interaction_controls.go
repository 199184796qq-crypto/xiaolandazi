package model

import (
	"fmt"
	"time"
)

var InteractionControlKinds = []string{"welcome", "like", "follow", "order", "guide", "semantic", "urgent", "time_report", "dialogue"}

type RoomInteractionControls struct {
	Active     *bool             `json:"interaction_active,omitempty"`
	Switches   map[string]bool   `json:"interaction_switches,omitempty"`
	Cooldowns  map[string]int    `json:"interaction_cooldowns,omitempty"`
	DisabledAt map[string]uint64 `json:"-"`
	Revision   uint64            `json:"interaction_controls_revision"`
	UpdatedAt  time.Time         `json:"updated_at,omitempty"`
	UpdatedBy  int64             `json:"updated_by,omitempty"`
}

func InteractionControlKind(kind string) string {
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
func (c RoomInteractionControls) Enabled(kind string) bool {
	if c.Active != nil && !*c.Active {
		return false
	}
	any := false
	for _, key := range InteractionControlKinds {
		on, ok := c.Switches[key]
		if !ok || on {
			any = true
			break
		}
	}
	if !any {
		return false
	}
	on, ok := c.Switches[InteractionControlKind(kind)]
	return !ok || on
}
func ValidateInteractionControls(switches map[string]bool, cooldowns map[string]int) error {
	known := map[string]bool{}
	for _, k := range InteractionControlKinds {
		known[k] = true
	}
	for k := range switches {
		if !known[k] {
			return fmt.Errorf("未知互动类型：%s", k)
		}
	}
	for k, seconds := range cooldowns {
		if !known[k] || k == "urgent" {
			return fmt.Errorf("该互动不支持冷却设置：%s", k)
		}
		if seconds < 0 || seconds > 3600 {
			return fmt.Errorf("互动冷却必须为0到3600秒的整数")
		}
	}
	return nil
}
