package scriptchain

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"
)

// KV 是缓存后端的最小接口。Redis 和内存实现都满足它。
type KV interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	ClearPrefix(ctx context.Context, prefix string) error
}

// Store 保存每场直播里每条话术片的分析结果和已用链记录。
type Store struct {
	kv  KV
	ttl time.Duration
}

// NewStore 构造缓存；ttl 建议按场次时长给。
func NewStore(kv KV, ttl time.Duration) *Store {
	return &Store{kv: kv, ttl: ttl}
}

func sessionPrefix(tenant, session int64) string {
	return "scriptchain:" + strconv.FormatInt(tenant, 10) + ":session:" + strconv.FormatInt(session, 10) + ":"
}

// Piece identifies one saved text within its tenant, plan and live session.
type Piece struct {
	Tenant, Session, Plan int64
	ID                    int64
	ContentHash           string
}

func (p Piece) key(suffix string) string {
	return sessionPrefix(p.Tenant, p.Session) + strconv.FormatInt(p.Plan, 10) + ":" +
		strconv.FormatInt(p.ID, 10) + ":" + p.ContentHash + ":" + suffix
}

func (s *Store) ClearSession(ctx context.Context, tenant, session int64) error {
	return s.kv.ClearPrefix(ctx, sessionPrefix(tenant, session))
}

// Load 读分析结果。
func (s *Store) Load(ctx context.Context, piece Piece) (Analysis, bool, error) {
	var a Analysis
	raw, ok, err := s.kv.Get(ctx, piece.key("analysis"))
	if err != nil || !ok {
		return a, false, err
	}
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return Analysis{}, false, nil
	}
	a.Chains = completeChains(a.Chains)
	return a, len(a.Chains) > 0, nil
}

// Save 写分析结果。
func (s *Store) Save(ctx context.Context, piece Piece, a Analysis) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return s.kv.Set(ctx, piece.key("analysis"), string(raw), s.ttl)
}

// RecentUsed 读最近用过的链下标（最多保留 3 条）。
func (s *Store) RecentUsed(ctx context.Context, piece Piece) []int {
	raw, ok, err := s.kv.Get(ctx, piece.key("used"))
	if err != nil || !ok {
		return nil
	}
	out := make([]int, 0, 3)
	for _, part := range strings.Split(raw, ",") {
		if v, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
			out = append(out, v)
		}
	}
	return out
}

// MarkUsed 记录一条链被用过。
func (s *Store) MarkUsed(ctx context.Context, piece Piece, previous []int, idx int) {
	used := append(previous, idx)
	if len(used) > 3 {
		used = used[len(used)-3:]
	}
	parts := make([]string, 0, len(used))
	for _, v := range used {
		parts = append(parts, strconv.Itoa(v))
	}
	_ = s.kv.Set(ctx, piece.key("used"), strings.Join(parts, ","), s.ttl)
}

// MemoryKV 是进程内实现，Redis 不可用时兜底。
type MemoryKV struct {
	mu    sync.Mutex
	items map[string]memItem
}

type memItem struct {
	value   string
	expires time.Time
}

// NewMemoryKV 构造内存缓存。
func NewMemoryKV() *MemoryKV { return &MemoryKV{items: map[string]memItem{}} }

// Get 读取未过期值。
func (m *MemoryKV) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[key]
	if !ok || (!it.expires.IsZero() && time.Now().After(it.expires)) {
		delete(m.items, key)
		return "", false, nil
	}
	return it.value, true, nil
}

// Set 写入值。
func (m *MemoryKV) Set(_ context.Context, key, value string, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it := memItem{value: value}
	if ttl > 0 {
		it.expires = time.Now().Add(ttl)
	}
	m.items[key] = it
	return nil
}

// Del 删除值。
func (m *MemoryKV) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, key)
	return nil
}

// ClearPrefix 按前缀清理，场次结束时调用。
func (m *MemoryKV) ClearPrefix(_ context.Context, prefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.items {
		if strings.HasPrefix(k, prefix) {
			delete(m.items, k)
		}
	}
	return nil
}
