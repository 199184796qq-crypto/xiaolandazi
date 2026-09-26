package events

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

type AgentPlanRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func (s *Store) SetAgentPlan(ctx context.Context, roomID, planID int64, planName string) error {
	if roomID <= 0 {
		return fmt.Errorf("invalid room id")
	}
	if planID < 0 {
		return fmt.Errorf("invalid plan id")
	}
	if s.redis == nil {
		return nil
	}
	key := agentPlanKey(roomID)
	if planID == 0 {
		return s.redis.Del(ctx, key).Err()
	}
	payload, err := json.Marshal(AgentPlanRef{ID: planID, Name: strings.TrimSpace(planName)})
	if err != nil {
		return err
	}
	return s.redis.Set(ctx, key, payload, 0).Err()
}

func (s *Store) GetAgentPlan(ctx context.Context, roomID int64) (AgentPlanRef, error) {
	if roomID <= 0 {
		return AgentPlanRef{}, fmt.Errorf("invalid room id")
	}
	if s.redis == nil {
		return AgentPlanRef{}, nil
	}
	value, err := s.redis.Get(ctx, agentPlanKey(roomID)).Bytes()
	if err == redis.Nil {
		return AgentPlanRef{}, nil
	}
	if err != nil {
		return AgentPlanRef{}, err
	}
	var ref AgentPlanRef
	if err := json.Unmarshal(value, &ref); err != nil || ref.ID <= 0 {
		return AgentPlanRef{}, nil
	}
	ref.Name = strings.TrimSpace(ref.Name)
	return ref, nil
}

func agentPlanKey(roomID int64) string {
	return "livecompanion:room:" + strconv.FormatInt(roomID, 10) + ":agent_plan"
}
