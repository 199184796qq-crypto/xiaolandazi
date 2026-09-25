package events

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

const (
	AgentModeControl = "control"
	AgentModeAnchor  = "anchor"
)

func normalizeAgentMode(mode string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case AgentModeControl, AgentModeAnchor:
		return mode, nil
	default:
		return "", fmt.Errorf("mode must be control or anchor")
	}
}

// SetAgentMode persists the operator's room mode independently from the
// livestream session cache. A room can start a fresh broadcast without losing
// whether the user selected control mode or anchor mode.
func (s *Store) SetAgentMode(ctx context.Context, roomID int64, mode string) error {
	if roomID <= 0 {
		return fmt.Errorf("invalid room id")
	}
	normalized, err := normalizeAgentMode(mode)
	if err != nil {
		return err
	}
	if s.redis == nil {
		return nil
	}
	return s.redis.Set(ctx, agentModeKey(roomID), normalized, 0).Err()
}

func (s *Store) GetAgentMode(ctx context.Context, roomID int64) (string, error) {
	if roomID <= 0 {
		return "", fmt.Errorf("invalid room id")
	}
	if s.redis == nil {
		return AgentModeControl, nil
	}
	value, err := s.redis.Get(ctx, agentModeKey(roomID)).Result()
	if err == redis.Nil {
		return AgentModeControl, nil
	}
	if err != nil {
		return "", err
	}
	normalized, normalizeErr := normalizeAgentMode(value)
	if normalizeErr != nil {
		// Corrupted/legacy values fail closed to control mode rather than
		// enabling autonomous speech unexpectedly.
		return AgentModeControl, nil
	}
	return normalized, nil
}

func agentModeKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":agent_mode"
}
