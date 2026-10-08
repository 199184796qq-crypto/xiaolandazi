package anchorcycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"time"
)

// Public startup milestones, not estimated completion time or internal names.
type LiveStartup struct {
	SessionID int64     `json:"session_id"`
	Stage     string    `json:"stage"`
	Percent   int       `json:"percent"`
	UpdatedAt time.Time `json:"updated_at"`
}
type LiveStartupRepository interface {
	SaveLiveStartup(context.Context, int64, int64, LiveStartup) error
	GetLiveStartup(context.Context, int64, int64, int64) (*LiveStartup, error)
}

func StartupPercent(stage string) int {
	switch stage {
	case "reading_plan":
		return 12
	case "connecting_service":
		return 25
	case "preparing_content":
		return 40
	case "checking_content":
		return 55
	case "generating_voice":
		return 65
	case "voice_ready":
		return 80
	case "sending_audio":
		return 90
	case "waiting_playback":
		return 96
	default:
		return 0
	}
}
func (s *RedisStore) liveStartupKey(tenant, room, session int64) string {
	return fmt.Sprintf("%s:%d:%d:startup:%d", s.prefix, tenant, room, session)
}
func (s *RedisStore) SaveLiveStartup(ctx context.Context, tenant, room int64, p LiveStartup) error {
	if tenant <= 0 || room <= 0 || p.SessionID <= 0 || StartupPercent(p.Stage) == 0 || p.Percent != StartupPercent(p.Stage) {
		return errors.New("invalid startup milestone scope")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	// Concurrent text and voice jobs cannot move the visible milestone backwards.
	return s.client.Eval(ctx, `local old=redis.call('GET',KEYS[1]); if old then local p=cjson.decode(old); if tonumber(p.percent)>=tonumber(ARGV[2]) then return 0 end end; redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[3]); return 1`, []string{s.liveStartupKey(tenant, room, p.SessionID)}, string(raw), p.Percent, int(TTL.Seconds())).Err()
}
func (s *RedisStore) GetLiveStartup(ctx context.Context, tenant, room, session int64) (*LiveStartup, error) {
	raw, err := s.client.Get(ctx, s.liveStartupKey(tenant, room, session)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p LiveStartup
	if err = json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.SessionID != session || p.Percent != StartupPercent(p.Stage) || p.Percent == 0 {
		return nil, errors.New("invalid startup milestone")
	}
	return &p, nil
}
