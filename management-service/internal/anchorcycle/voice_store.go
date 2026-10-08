package anchorcycle

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/redis/go-redis/v9"
)

// Voice completion has its own Redis hash, so concurrent TTS cannot advance
// or overwrite the generation cursor, attempts, feedback, or revision.
type VoiceRepository interface {
	ClaimVoice(context.Context, int64, int64, int64, string, string, string, bool) (*VoiceAsset, bool, error)
	CompleteVoice(context.Context, int64, int64, int64, string, string, string, VoiceAsset) error
}

// Only after a formal batch is acknowledged and the adoption tail is saved.
// Does not remove OSS reuse assets; it bounds this run's transient voice hash.
func (s *RedisStore) PruneDeliveredVoices(ctx context.Context, tenant, plan, room int64, run string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.client.HDel(ctx, s.voiceKey(tenant, plan, room, run), ids...).Err()
}

func (s *RedisStore) voiceKey(tenant, plan, room int64, run string) string {
	return s.key(tenant, plan, room) + ":voices:" + run
}

func marshalRun(state *State) ([]byte, error) {
	copy := *state
	copy.Accepted = append([]Segment{}, state.Accepted...)
	for i := range copy.Accepted {
		copy.Accepted[i].Voice = nil
	}
	return json.Marshal(copy)
}

func (s *RedisStore) loadVoices(ctx context.Context, state *State) error {
	if state.Config.TTS == nil || len(state.Accepted) == 0 {
		return nil
	}
	ids := make([]string, len(state.Accepted))
	for i, segment := range state.Accepted {
		ids[i] = segment.Selected.ID
	}
	values, err := s.client.HMGet(ctx, s.voiceKey(state.TenantID, state.PlanID, state.RoomID, state.RunID), ids...).Result()
	if err != nil {
		return err
	}
	for i, value := range values {
		if value == nil {
			continue
		}
		raw, ok := value.(string)
		if !ok {
			return errors.New("声音缓存格式错误")
		}
		var voice VoiceAsset
		if err := json.Unmarshal([]byte(raw), &voice); err != nil {
			return err
		}
		state.Accepted[i].Voice = &voice
	}
	return nil
}

func (s *RedisStore) ClaimVoice(ctx context.Context, tenant, plan, room int64, run, candidate, job string, retry bool) (*VoiceAsset, bool, error) {
	initial, _ := json.Marshal(VoiceAsset{JobID: job, Status: "generating"})
	result, err := s.client.Eval(ctx, `local raw=redis.call('GET',KEYS[1]); if not raw then return '' end
local state=cjson.decode(raw); if state.run_id~=ARGV[1] or not state.config.tts then return '' end
local found=false; for _,segment in ipairs(state.accepted) do if segment.selected.id==ARGV[2] then found=true; break end end
if not found then return '' end
local prior=redis.call('HGET',KEYS[2],ARGV[2]); if prior then local v=cjson.decode(prior); if v.status~='failed' or ARGV[4]~='1' then return cjson.encode({claimed=false,voice=v}) end end
local voice=cjson.decode(ARGV[3]); local limit=tonumber(state.preview_limit_ms or 0); if state.stream_controlled then limit=0 end
if limit>0 then local used=0; for _,saved in ipairs(redis.call('HVALS',KEYS[2])) do local v=cjson.decode(saved); if v.status=='generating' then return 'preview_voice_busy' end; if v.status=='ready' then used=used+tonumber(v.bundle.audio_duration_ms or 0) end end
if used>=limit then return 'budget_exhausted' end; voice.preview_reserved_ms=limit-used end
redis.call('HSET',KEYS[2],ARGV[2],cjson.encode(voice)); redis.call('EXPIRE',KEYS[2],ARGV[5]); return cjson.encode({claimed=true,voice=voice})`, []string{s.key(tenant, plan, room), s.voiceKey(tenant, plan, room, run)}, run, candidate, string(initial), boolInt(retry), int(TTL.Seconds())).Text()
	if err != nil {
		return nil, false, err
	}
	if result == "" {
		return nil, false, ErrStale
	}
	var decoded struct {
		Claimed bool       `json:"claimed"`
		Voice   VoiceAsset `json:"voice"`
	}
	if result == "budget_exhausted" {
		return nil, false, ErrPreviewBudgetExhausted
	}
	if result == "preview_voice_busy" {
		return nil, false, ErrBusy
	}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		return nil, false, err
	}
	return &decoded.Voice, decoded.Claimed, nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *RedisStore) CompleteVoice(ctx context.Context, tenant, plan, room int64, run, candidate, job string, voice VoiceAsset) error {
	if voice.Status != "ready" && voice.Status != "failed" {
		return errors.New("声音完成状态无效")
	}
	voice.JobID = job
	data, err := json.Marshal(voice)
	if err != nil {
		return err
	}
	if len(data) > 2<<20 {
		return errors.New("声音资产引用包过大")
	}
	value, err := s.client.Eval(ctx, `local raw=redis.call('GET',KEYS[1]); if not raw then return 0 end
if cjson.decode(raw).run_id~=ARGV[1] then return 0 end
local old=redis.call('HGET',KEYS[2],ARGV[2]); if not old then return 0 end
local v=cjson.decode(old); if v.job_id~=ARGV[3] or v.status~='generating' then return 0 end
local new=cjson.decode(ARGV[4]); local remaining=tonumber(v.preview_reserved_ms or 0); new.preview_reserved_ms=remaining
if remaining>0 and new.status=='ready' then local duration=tonumber(new.bundle.audio_duration_ms or 0); if duration<=0 then return 2 end
if duration>remaining then local timeline=new.bundle.timeline or {}; local last=timeline[#timeline]; if not last or not last.safe_cut or tonumber(last.end_ms or 0)~=duration then return 2 end end end
redis.call('HSET',KEYS[2],ARGV[2],cjson.encode(new)); redis.call('EXPIRE',KEYS[2],ARGV[5]); return 1`, []string{s.key(tenant, plan, room), s.voiceKey(tenant, plan, room, run)}, run, candidate, job, string(data), int(TTL.Seconds())).Int()
	if errors.Is(err, redis.Nil) {
		return ErrStale
	}
	if err != nil {
		return err
	}
	if value != 1 {
		if value == 2 {
			return errors.New("试听生成声音缺少有效时长或完整结尾")
		}
		return ErrStale
	}
	return nil
}
