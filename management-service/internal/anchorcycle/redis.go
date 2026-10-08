package anchorcycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrMissing = errors.New("本场大循环不存在，服务器重启后须重新开始")
var ErrStale = errors.New("本场已结束、被替换或状态已变化，晚到结果未采纳")
var ErrBusy = errors.New("本场已有请求进行中，请等待完成，不要重复点击")
var ErrPreviewBudgetExhausted = errors.New("本场试听声音已生成完成")

const TTL = 24 * time.Hour

// The two-minute preview is a minimum listening target, not a license to cut a
// spoken sentence in half. The final clip may run to the next verified semantic
// boundary, within this small bounded overrun.
const PreviewSafeOverrunMS = int64(25_000)

type Repository interface {
	Get(context.Context, int64, int64, int64) (*State, error)
	Acquire(context.Context, int64, int64, int64, string) error
	Release(context.Context, int64, int64, int64, string)
	Create(context.Context, *State, string) error
	Save(context.Context, *State) error
	Delete(context.Context, int64, int64, int64, string) error
	EndRoom(context.Context, int64, int64) error
	RoomStates(context.Context, int64, int64) ([]*State, error)
}

// A process-specific namespace deliberately makes old Redis keys unrecoverable
// after a crash/restart. No Redis flush or stable asset deletion is performed.
type RedisStore struct {
	client      *redis.Client
	prefix      string
	ownerPrefix string
}

func NewRedis(client *redis.Client, realm string) *RedisStore {
	return &RedisStore{client: client, prefix: "livecompanion:" + realm + ":anchor-cycle:" + ID(), ownerPrefix: "livecompanion:" + realm + ":anchor-mainline-owner"}
}
func (s *RedisStore) PersistenceTimeout() time.Duration {
	return PersistenceTimeout(s.client.Options().ReadTimeout)
}
func (s *RedisStore) key(tenant, plan, room int64) string {
	return fmt.Sprintf("%s:%d:%d:%d", s.prefix, tenant, room, plan)
}
func (s *RedisStore) Get(ctx context.Context, tenant, plan, room int64) (*State, error) {
	raw, err := s.client.Get(ctx, s.key(tenant, plan, room)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMissing
	}
	if err != nil {
		return nil, err
	}
	var state State
	if err = json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if state.Busy {
		leased, e := s.client.Exists(ctx, s.key(tenant, plan, room)+":lease").Result()
		if e != nil {
			return nil, e
		}
		if leased == 0 {
			state.Busy = false
			// Adoption was already committed; only the optional feedback may
			// need repeating. Without adoption, never infer a lost generation failed.
			if !state.FeedbackPending {
				state.Uncertain = true
				state.Error = "上一轮结果未确认，请结束本场重新开始，不能盲目重发"
			}
		}
	}
	if err := s.loadVoices(ctx, &state); err != nil {
		return nil, err
	}
	return &state, nil
}
func (s *RedisStore) Acquire(ctx context.Context, tenant, plan, room int64, token string) error {
	ok, err := s.client.SetNX(ctx, s.key(tenant, plan, room)+":lease", token, 10*time.Minute).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrBusy
	}
	return nil
}
func (s *RedisStore) Release(ctx context.Context, tenant, plan, room int64, token string) {
	_, _ = s.client.Eval(ctx, `if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`, []string{s.key(tenant, plan, room) + ":lease"}, token).Result()
}
func (s *RedisStore) Create(ctx context.Context, state *State, token string) error {
	data, err := marshalRun(state)
	if err != nil {
		return err
	}
	value, err := s.client.Eval(ctx, `if redis.call('GET',KEYS[2])~=ARGV[2] or redis.call('EXISTS',KEYS[1])==1 then return 0 end redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[3]); return 1`, []string{s.key(state.TenantID, state.PlanID, state.RoomID), s.key(state.TenantID, state.PlanID, state.RoomID) + ":lease"}, string(data), token, int(TTL.Seconds())).Int()
	if err != nil {
		return err
	}
	if value != 1 {
		return ErrStale
	}
	return nil
}
func (s *RedisStore) Save(ctx context.Context, state *State) error {
	previous := state.Revision
	state.Revision++
	data, err := marshalRun(state)
	if err != nil {
		state.Revision = previous
		return err
	}
	if len(data) > 16<<20 {
		state.Revision = previous
		return errors.New("大循环调试记录过大，请结束本场并导出")
	}
	value, err := s.client.Eval(ctx, `local raw=redis.call('GET',KEYS[1]); if not raw then return 0 end local old=cjson.decode(raw); if old.run_id~=ARGV[2] or old.revision~=tonumber(ARGV[3]) then return 0 end redis.call('SET',KEYS[1],ARGV[1],'EX',ARGV[4]); return 1`, []string{s.key(state.TenantID, state.PlanID, state.RoomID)}, string(data), state.RunID, previous, int(TTL.Seconds())).Int()
	if err != nil || value != 1 {
		state.Revision = previous
		if err != nil {
			return err
		}
		return ErrStale
	}
	return nil
}
func (s *RedisStore) Delete(ctx context.Context, tenant, plan, room int64, run string) error {
	_, err := s.client.Eval(ctx, `local raw=redis.call('GET',KEYS[1]); if not raw then if ARGV[1]~='' then return redis.call('DEL',KEYS[3]) end return redis.call('DEL',KEYS[2]) end local old=cjson.decode(raw); if ARGV[1]~='' and old.run_id~=ARGV[1] then return 0 end local voice_key=KEYS[3]; if ARGV[1]=='' then voice_key=KEYS[1]..':voices:'..old.run_id end return redis.call('DEL',KEYS[1],KEYS[2],voice_key)`, []string{s.key(tenant, plan, room), s.key(tenant, plan, room) + ":lease", s.voiceKey(tenant, plan, room, run)}, run).Result()
	return err
}
func (s *RedisStore) EndRoom(ctx context.Context, tenant, room int64) error {
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, fmt.Sprintf("%s:%d:%d:*", s.prefix, tenant, room), 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err = s.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			return nil
		}
	}
}

func (s *RedisStore) RoomStates(ctx context.Context, tenant, room int64) ([]*State, error) {
	result := []*State{}
	var cursor uint64
	for {
		keys, next, err := s.client.Scan(ctx, cursor, fmt.Sprintf("%s:%d:%d:*", s.prefix, tenant, room), 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			if strings.HasSuffix(key, ":lease") || strings.Contains(key, ":voices:") || strings.Contains(key, ":failure:") || strings.Contains(key, ":startup:") {
				continue
			}
			raw, e := s.client.Get(ctx, key).Bytes()
			if errors.Is(e, redis.Nil) {
				continue
			}
			if e != nil {
				return nil, e
			}
			var state State
			if e = json.Unmarshal(raw, &state); e != nil {
				return nil, e
			}
			if state.TenantID == tenant && state.RoomID == room {
				result = append(result, &state)
			}
		}
		cursor = next
		if cursor == 0 {
			return result, nil
		}
	}
}
