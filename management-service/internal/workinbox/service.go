package workinbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type Source interface {
	InboxTopic(context.Context, model.InboxScope, string) ([]model.InboxGroup, error)
	InboxRevisions(context.Context) (map[string]uint64, error)
}
type cacheValue struct {
	Groups []model.InboxGroup `json:"groups"`
	At     time.Time          `json:"at"`
}
type flight struct {
	done  chan struct{}
	value cacheValue
	err   error
}
type Service struct {
	source     Source
	redis      *redis.Client
	namespace  string
	mu         sync.Mutex
	revisions  map[string]uint64
	ready      bool
	memory     map[string]cacheValue
	flights    map[string]*flight
	redisRetry time.Time
	slots      chan struct{}
}

func New(source Source, client *redis.Client, namespace string) *Service {
	return &Service{source: source, redis: client, namespace: digest(namespace), revisions: map[string]uint64{}, memory: map[string]cacheValue{}, flights: map[string]*flight{}, slots: make(chan struct{}, 4)}
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s *Service) refresh(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	revisions, err := s.source.InboxRevisions(c)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = err == nil
	if err == nil {
		s.revisions = revisions
	}
}
func (s *Service) Run(ctx context.Context) {
	s.refresh(ctx)
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.refresh(ctx)
		}
	}
}

// Version reads memory only. Heartbeats and each browser connection never query tables.
func (s *Service) Version() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return digest(struct {
		Revisions map[string]uint64
		Ready     bool
		Minute    int64
	}{s.revisions, s.ready, time.Now().Unix() / 60})
}
func (s *Service) topic(ctx context.Context, sc model.InboxScope, topic string) (cacheValue, error) {
	s.mu.Lock()
	ready := s.ready
	rev := s.revisions[topic]
	access := s.revisions["access"]
	s.mu.Unlock()
	if !ready {
		return cacheValue{}, errors.New("待办同步暂不可用")
	}
	stamp := int64(0)
	if topic == "sales" {
		stamp = time.Now().Unix() / 60
	}
	key := "livecompanion:inbox:v1:" + s.namespace + ":" + db.InboxCacheIdentity(sc, topic) + ":" + topic + ":" + strconv.FormatUint(rev, 10) + ":" + strconv.FormatUint(access, 10) + ":" + strconv.FormatInt(stamp, 10)
	s.mu.Lock()
	if v, ok := s.memory[key]; ok && time.Since(v.At) < 5*time.Minute {
		s.mu.Unlock()
		return v, nil
	}
	if f, ok := s.flights[key]; ok {
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return cacheValue{}, ctx.Err()
		case <-f.done:
			return f.value, f.err
		}
	}
	f := &flight{done: make(chan struct{})}
	s.flights[key] = f
	s.mu.Unlock()
	v, err := s.load(ctx, key, sc, topic)
	s.mu.Lock()
	f.value = v
	f.err = err
	delete(s.flights, key)
	if err == nil {
		if len(s.memory) >= 2048 {
			for k, old := range s.memory {
				if time.Since(old.At) > time.Minute {
					delete(s.memory, k)
				}
			}
			if len(s.memory) >= 2048 {
				s.memory = map[string]cacheValue{}
			}
		}
		s.memory[key] = v
	}
	close(f.done)
	s.mu.Unlock()
	return v, err
}
func (s *Service) load(ctx context.Context, key string, sc model.InboxScope, topic string) (cacheValue, error) {
	s.mu.Lock()
	useRedis := s.redis != nil && !time.Now().Before(s.redisRetry)
	s.mu.Unlock()
	if useRedis {
		c, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
		raw, err := s.redis.Get(c, key).Bytes()
		cancel()
		if err == nil {
			var v cacheValue
			if json.Unmarshal(raw, &v) == nil && v.Groups != nil && time.Since(v.At) < 5*time.Minute {
				return v, nil
			}
		}
		if err != nil && !errors.Is(err, redis.Nil) {
			s.mu.Lock()
			s.redisRetry = time.Now().Add(15 * time.Second)
			s.mu.Unlock()
			useRedis = false
		}
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return cacheValue{}, ctx.Err()
	}
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	groups, err := s.source.InboxTopic(c, sc, topic)
	cancel()
	if err != nil {
		return cacheValue{}, err
	}
	v := cacheValue{Groups: groups, At: time.Now().UTC()}
	if useRedis {
		raw, _ := json.Marshal(v)
		c, cancel := context.WithTimeout(ctx, 350*time.Millisecond)
		_ = s.redis.Set(c, key, raw, 5*time.Minute).Err()
		cancel()
	}
	return v, nil
}
func (s *Service) Snapshot(ctx context.Context, sc model.InboxScope) (model.InboxSnapshot, error) {
	if sc.Actor.UserID <= 0 {
		return model.InboxSnapshot{}, errors.New("未登录")
	}
	if !model.WorkInboxAvailable(sc.Actor.Role) {
		return model.InboxSnapshot{}, errors.New("当前账号不提供统一待办入口")
	}
	out := model.InboxSnapshot{Groups: []model.InboxGroup{}, Unavailable: []string{}, AsOf: time.Now().UTC(), Version: s.Version()}
	for _, topic := range db.InboxTopics(sc) {
		v, err := s.topic(ctx, sc, topic)
		if err != nil {
			out.Stale = true
			out.Unavailable = append(out.Unavailable, topic)
			continue
		}
		if v.At.Before(out.AsOf) {
			out.AsOf = v.At
		}
		for _, g := range v.Groups {
			out.Groups = append(out.Groups, g)
			out.Total += g.Count
		}
	}
	return out, nil
}
func Summary(s model.InboxSnapshot, category, department string) string {
	labels := map[string]string{"review": "待审核", "accept": "待接单", "process": "待处理", "supplement": "待补充", "confirm": "待确认"}
	rows := []string{}
	total := int64(0)
	for _, g := range s.Groups {
		if g.Count <= 0 || (category != "" && g.Category != category) || (department != "" && !strings.Contains(g.Department, department)) {
			continue
		}
		rows = append(rows, fmt.Sprintf("%s · %s：%d 件（%s）", g.Department, g.Title, g.Count, labels[g.Category]))
		total += g.Count
	}
	sort.Strings(rows)
	if s.Stale {
		return "部分待办暂时无法同步，当前结果不完整，不能据此判断没有待办。请在待办面板重试。"
	}
	if total == 0 {
		return "当前已接入的业务中，没有符合条件、轮到你处理的待办。等待他人处理的进度不计入红色角标。"
	}
	return fmt.Sprintf("当前有 %d 件符合条件的待办：\n%s\n共享待接单事项不是重复分配；打开提醒不会完成任务。请在分类面板选择具体单据，涉及审核、付款或库存变更仍须核对后确认。", total, strings.Join(rows, "\n"))
}
