// Package anchorvoicecache owns reusable, expiring OSS bundles, not livestream
// progress. Its stable Redis namespace survives process restarts for cleanup.
package anchorvoicecache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/model"
)

const DefaultTTL = 5 * time.Hour

type Entry struct {
	ID           string                            `json:"id"`
	Group        string                            `json:"group"`
	TenantID     int64                             `json:"tenant_id"`
	Bucket       string                            `json:"bucket"`
	ObjectPrefix string                            `json:"object_prefix"`
	CreatedAt    time.Time                         `json:"created_at"`
	ExpiresAt    time.Time                         `json:"expires_at"`
	Text         string                            `json:"text"`
	OutputText   string                            `json:"output_text"`
	Bundle       model.LiveAgentPlanVersionVariant `json:"bundle"`
}

type Repository interface {
	NewEntry(int64, string, string, string, time.Duration) Entry
	Reserve(context.Context, Entry) error
	Publish(context.Context, Entry) error
	Lookup(context.Context, string) ([]Entry, error)
	Due(context.Context, int) ([]Entry, error)
	Forget(context.Context, Entry) error
}

type RedisStore struct {
	client          *redis.Client
	prefix, objects string
}

func Digest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func New(client *redis.Client, realm string) *RedisStore {
	return &RedisStore{client: client, prefix: "livecompanion:" + realm + ":anchor-voice-cache:v1:", objects: "anchor-voice-cache/v1/" + Digest(realm)[:16] + "/"}
}

func (s *RedisStore) NewEntry(tenant int64, group, id, bucket string, ttl time.Duration) Entry {
	now := time.Now().UTC()
	return Entry{ID: id, Group: group, TenantID: tenant, Bucket: bucket, ObjectPrefix: fmt.Sprintf("%st%d/%s/", s.objects, tenant, id), CreatedAt: now, ExpiresAt: now.Add(ttl)}
}

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Cleanup may delete only the five explicit keys belonging to this cache entry.
// It never trusts arbitrary paths in the manifest or scans a bucket for deletion.
func (s *RedisStore) validate(e Entry) error {
	if !hashPattern.MatchString(e.Group) || !idPattern.MatchString(e.ID) || e.TenantID <= 0 || e.Bucket == "" || e.ObjectPrefix != fmt.Sprintf("%st%d/%s/", s.objects, e.TenantID, e.ID) || e.ExpiresAt.IsZero() {
		return errors.New("声音缓存范围无效")
	}
	return nil
}

func ObjectKeys(e Entry) []string {
	return []string{e.ObjectPrefix + "audio.wav", e.ObjectPrefix + "subtitle.srt", e.ObjectPrefix + "timeline.json", e.ObjectPrefix + "safe-points.json", e.ObjectPrefix + "manifest.json"}
}

// Register cleanup BEFORE the first upload. A failed/partial upload is still
// collectable after restart. Cleanup records deliberately have no TTL.
func (s *RedisStore) Reserve(ctx context.Context, e Entry) error {
	if err := s.validate(e); err != nil {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = s.client.Eval(ctx, `redis.call('HSET',KEYS[1],ARGV[1],ARGV[2]); redis.call('ZADD',KEYS[2],ARGV[3],ARGV[1]); return 1`, []string{s.prefix + "cleanup", s.prefix + "due"}, e.ID, string(raw), e.ExpiresAt.UnixMilli()).Result()
	return err
}

func (s *RedisStore) Publish(ctx context.Context, e Entry) error {
	if err := s.validate(e); err != nil {
		return err
	}
	if time.Until(e.ExpiresAt) <= 0 || e.Bundle.AudioDurationMS <= 0 || e.OutputText == "" {
		return errors.New("声音缓存已过期或资产不完整")
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(raw) > 2<<20 {
		return errors.New("声音缓存过大")
	}
	// Absolute expiry is never renewed by hits. Multiple expressions may coexist.
	_, err = s.client.Eval(ctx, `redis.call('SET',KEYS[1],ARGV[2]); redis.call('PEXPIREAT',KEYS[1],ARGV[3]); redis.call('ZADD',KEYS[2],ARGV[4],ARGV[1]); local ttl=redis.call('PTTL',KEYS[2]); local remaining=tonumber(ARGV[3])-tonumber(ARGV[5]); if ttl<remaining then redis.call('PEXPIREAT',KEYS[2],ARGV[3]) end; return 1`, []string{s.prefix + "entry:" + e.ID, s.prefix + "group:" + e.Group}, e.ID, string(raw), e.ExpiresAt.UnixMilli(), e.CreatedAt.UnixMilli(), time.Now().UnixMilli()).Result()
	return err
}

func (s *RedisStore) Lookup(ctx context.Context, group string) ([]Entry, error) {
	if !hashPattern.MatchString(group) {
		return nil, errors.New("声音缓存指纹无效")
	}
	ids, err := s.client.ZRevRange(ctx, s.prefix+"group:"+group, 0, 15).Result()
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, id := range ids {
		raw, err := s.client.Get(ctx, s.prefix+"entry:"+id).Bytes()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var e Entry
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		if err = s.validate(e); err != nil {
			return nil, err
		}
		if e.Group == group && time.Now().Before(e.ExpiresAt) {
			result = append(result, e)
		}
	}
	return result, nil
}

// Reads an immutable output for download history, without authorizing reuse.
func (s *RedisStore) FindCandidate(ctx context.Context, tenant int64, candidate string) (model.LiveAgentPlanVersionVariant, bool) {
	var cursor uint64
	visited := 0
	for {
		values, next, err := s.client.HScan(ctx, s.prefix+"cleanup", cursor, "", 128).Result()
		if err != nil {
			return model.LiveAgentPlanVersionVariant{}, false
		}
		for i := 1; i < len(values); i += 2 {
			visited++
			var e Entry
			if json.Unmarshal([]byte(values[i]), &e) != nil || e.TenantID != tenant || s.validate(e) != nil {
				continue
			}
			matched := false
			for _, segment := range e.Bundle.Timeline {
				if strings.HasPrefix(segment.SegmentID, "test-"+candidate+"-") {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			raw, err := s.client.Get(ctx, s.prefix+"entry:"+e.ID).Bytes()
			if err != nil || json.Unmarshal(raw, &e) != nil || e.TenantID != tenant || !time.Now().Before(e.ExpiresAt) {
				continue
			}
			return e.Bundle, true
		}
		cursor = next
		if cursor == 0 || visited >= 4096 {
			return model.LiveAgentPlanVersionVariant{}, false
		}
	}
}

func (s *RedisStore) Due(ctx context.Context, limit int) ([]Entry, error) {
	ids, err := s.client.ZRangeByScore(ctx, s.prefix+"due", &redis.ZRangeBy{Min: "-inf", Max: fmt.Sprint(time.Now().UnixMilli()), Count: int64(limit)}).Result()
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, id := range ids {
		raw, err := s.client.HGet(ctx, s.prefix+"cleanup", id).Bytes()
		if err != nil {
			return nil, err
		}
		var e Entry
		if err = json.Unmarshal(raw, &e); err != nil {
			return nil, err
		}
		if err = s.validate(e); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, nil
}

func (s *RedisStore) Forget(ctx context.Context, e Entry) error {
	if err := s.validate(e); err != nil {
		return err
	}
	_, err := s.client.Eval(ctx, `redis.call('DEL',KEYS[1]);redis.call('ZREM',KEYS[2],ARGV[1]);redis.call('HDEL',KEYS[3],ARGV[1]);redis.call('ZREM',KEYS[4],ARGV[1]);return 1`, []string{s.prefix + "entry:" + e.ID, s.prefix + "group:" + e.Group, s.prefix + "cleanup", s.prefix + "due"}, e.ID).Result()
	return err
}
