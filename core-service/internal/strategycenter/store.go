package strategycenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"
)

type Rule struct {
	Category        string         `json:"category"`
	Key             string         `json:"key"`
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	Enabled         bool           `json:"enabled"`
	BaseProbability int            `json:"base_probability"`
	MinProbability  int            `json:"min_probability"`
	SystemDefault   bool           `json:"system_default,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
}

type AddressingOption struct {
	Key           string `json:"key"`
	Text          string `json:"text"`
	Enabled       bool   `json:"enabled"`
	Probability   int    `json:"probability"`
	SystemDefault bool   `json:"system_default"`
}

type Policy struct {
	TenantID       int64              `json:"tenant_id"`
	Rules          []Rule             `json:"rules"`
	AddressingMode string             `json:"addressing_mode"`
	Addressing     []AddressingOption `json:"addressing"`
}

type Signals struct {
	Entries30s int    `json:"entries_30s,omitempty"`
	Likes30s   int64  `json:"likes_30s,omitempty"`
	Follows30s int    `json:"follows_30s,omitempty"`
	Heat       string `json:"heat,omitempty"`
}

type Candidate struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Weight int    `json:"weight"`
}

type Selection struct {
	Category   string      `json:"category"`
	Key        string      `json:"key"`
	Name       string      `json:"name"`
	Candidates []Candidate `json:"candidates"`
	Roll       int         `json:"roll"`
	Total      int         `json:"total_weight"`
	Seed       string      `json:"seed"`
}

type TriggerDecision struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Probability int    `json:"probability"`
	Roll        int    `json:"roll"`
	Triggered   bool   `json:"triggered"`
}

type Store struct {
	mu       sync.RWMutex
	policies map[int64]Policy
}

func DefaultPolicy() Policy {
	return Policy{
		Rules: []Rule{
			{Category: "interrupt", Key: "read_comment_softly", Name: "小声读一次弹幕", Enabled: true, BaseProbability: 20, MinProbability: 10},
			{Category: "interrupt", Key: "hard_cut", Name: "直接硬切", Enabled: true, BaseProbability: 20, MinProbability: 10},
			{Category: "interrupt", Key: "ask_controller", Name: "询问中控", Enabled: true, BaseProbability: 10, MinProbability: 10},
			{Category: "interrupt", Key: "thinking_pause", Name: "短暂停顿", Enabled: true, BaseProbability: 20, MinProbability: 10},
			{Category: "interrupt", Key: "repeat_confirm", Name: "重复确认", Enabled: true, BaseProbability: 30, MinProbability: 10},
			{Category: "resume", Key: "DIRECT", Name: "直接续播", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "BRIDGE", Name: "桥接恢复", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "FUSION_SKIP", Name: "融合跳过", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "CROSS_RESUME", Name: "跨段恢复", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "RE_ANCHOR", Name: "重新锚定", Enabled: true, BaseProbability: 20, MinProbability: 20},
			{Category: "resume", Key: "SWITCH_PLAN", Name: "切换主线方案", Enabled: false, BaseProbability: 0, MinProbability: 20},
			{Category: "interaction", Key: "welcome_named", Name: "点名欢迎", Enabled: true, BaseProbability: 10, MinProbability: 5},
			{Category: "interaction", Key: "welcome_batch", Name: "打包欢迎", Enabled: true, BaseProbability: 15, MinProbability: 5},
			{Category: "interaction", Key: "reply_like", Name: "回应点赞", Enabled: true, BaseProbability: 8, MinProbability: 5},
			{Category: "interaction", Key: "reply_follow", Name: "回应关注", Enabled: true, BaseProbability: 8, MinProbability: 5},
			{Category: "interaction", Key: "reply_chat", Name: "回复弹幕", Enabled: true, BaseProbability: 50, MinProbability: 20},
		},
		AddressingMode: "system",
		Addressing: []AddressingOption{
			{Key: "friend", Text: "朋友", Enabled: true, Probability: 40, SystemDefault: true},
			{Key: "boss", Text: "老板", Enabled: true, Probability: 20, SystemDefault: true},
			{Key: "everyone", Text: "大家", Enabled: true, Probability: 20, SystemDefault: true},
			{Key: "baozi", Text: "宝子", Enabled: true, Probability: 20, SystemDefault: true},
		},
	}
}

func New() *Store {
	defaults := DefaultPolicy()
	defaults.TenantID = 0
	return &Store{policies: map[int64]Policy{0: defaults}}
}

func (s *Store) Load(ctx context.Context, db *sql.DB) error {
	if s == nil || db == nil {
		return nil
	}
	rows, err := db.QueryContext(ctx, `SELECT tenant_id, CAST(config_json AS CHAR) FROM live_strategy_center_configs`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID int64
		var raw string
		if err := rows.Scan(&tenantID, &raw); err != nil {
			return err
		}
		var policy Policy
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			return fmt.Errorf("decode strategy policy tenant %d: %w", tenantID, err)
		}
		s.Put(tenantID, policy)
	}
	return rows.Err()
}

func (s *Store) Put(tenantID int64, policy Policy) Policy {
	if s == nil {
		return policy
	}
	policy.TenantID = tenantID
	s.mu.Lock()
	s.policies[tenantID] = policy
	s.mu.Unlock()
	return policy
}

func (s *Store) Resolve(tenantID int64) Policy {
	if s == nil {
		return Policy{TenantID: tenantID}
	}
	s.mu.RLock()
	global, globalOK := s.policies[0]
	policy, ok := s.policies[tenantID]
	if !ok {
		policy, ok = global, globalOK
	} else if tenantID != 0 && globalOK {
		// Tenant configuration only owns user-facing addressing choices today.
		// Always inherit the latest global behavior/resume/interaction rules so
		// a customer who customized a nickname never freezes old strategy odds.
		policy.Rules = global.Rules
		if len(policy.Addressing) == 0 || !strings.EqualFold(strings.TrimSpace(policy.AddressingMode), "custom") {
			policy.Addressing = global.Addressing
			policy.AddressingMode = "system"
		}
	}
	s.mu.RUnlock()
	if !ok {
		return Policy{TenantID: tenantID}
	}
	policy.TenantID = tenantID
	return policy
}

func (s *Store) Allowed(tenantID int64, category, key string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, rule := range s.Resolve(tenantID).Rules {
		if rule.Enabled && rule.BaseProbability > 0 && strings.ToLower(strings.TrimSpace(rule.Category)) == category && strings.TrimSpace(rule.Key) == key {
			return true
		}
	}
	return false
}

func (s *Store) Pick(tenantID int64, category string, candidates []string, signals Signals, seed string) Selection {
	category = strings.ToLower(strings.TrimSpace(category))
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	policy := s.Resolve(tenantID)
	allowed := map[string]struct{}{}
	for _, key := range candidates {
		key = strings.TrimSpace(key)
		if key != "" {
			allowed[key] = struct{}{}
		}
	}
	weighted := make([]Candidate, 0)
	for _, rule := range policy.Rules {
		if !rule.Enabled || strings.ToLower(strings.TrimSpace(rule.Category)) != category {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[rule.Key]; !ok {
				continue
			}
		}
		weight := rule.BaseProbability
		if weight <= 0 {
			continue
		}
		weight = adjustWeight(rule.Key, weight, signals)
		if weight <= 0 {
			continue
		}
		weighted = append(weighted, Candidate{Key: rule.Key, Name: rule.Name, Weight: weight})
	}
	if len(weighted) == 0 {
		for _, key := range candidates {
			key = strings.TrimSpace(key)
			if key != "" {
				weighted = append(weighted, Candidate{Key: key, Name: key, Weight: 1})
			}
		}
	}
	selection := Selection{Category: category, Candidates: weighted, Seed: seed}
	if len(weighted) == 0 {
		return selection
	}
	for _, item := range weighted {
		selection.Total += item.Weight
	}
	if selection.Total <= 0 {
		selection.Key = weighted[0].Key
		selection.Name = weighted[0].Name
		return selection
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:%s:%s", tenantID, category, seed)
	selection.Roll = int(h.Sum64()%uint64(selection.Total)) + 1
	cursor := 0
	for _, item := range weighted {
		cursor += item.Weight
		if selection.Roll <= cursor {
			selection.Key = item.Key
			selection.Name = item.Name
			return selection
		}
	}
	selection.Key = weighted[len(weighted)-1].Key
	selection.Name = weighted[len(weighted)-1].Name
	return selection
}

func (s *Store) PickAddressing(tenantID int64, seed string) Selection {
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	policy := s.Resolve(tenantID)
	wantedSystem := !strings.EqualFold(strings.TrimSpace(policy.AddressingMode), "custom")
	items := make([]Candidate, 0)
	for _, option := range policy.Addressing {
		if !option.Enabled || option.Probability <= 0 {
			continue
		}
		if wantedSystem != option.SystemDefault {
			continue
		}
		items = append(items, Candidate{Key: option.Key, Name: option.Text, Weight: option.Probability})
	}
	selection := Selection{Category: "addressing", Candidates: items, Seed: seed}
	if len(items) == 0 {
		return selection
	}
	for _, item := range items {
		selection.Total += item.Weight
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:addressing:%s", tenantID, seed)
	selection.Roll = int(h.Sum64()%uint64(selection.Total)) + 1
	cursor := 0
	for _, item := range items {
		cursor += item.Weight
		if selection.Roll <= cursor {
			selection.Key = item.Key
			selection.Name = item.Name
			return selection
		}
	}
	return selection
}

func (s *Store) Trigger(tenantID int64, category, key string, signals Signals, seed string) TriggerDecision {
	category = strings.ToLower(strings.TrimSpace(category))
	key = strings.TrimSpace(key)
	decision := TriggerDecision{Key: key}
	policy := s.Resolve(tenantID)
	found := false
	for _, rule := range policy.Rules {
		if !rule.Enabled || strings.ToLower(strings.TrimSpace(rule.Category)) != category || rule.Key != key {
			continue
		}
		found = true
		decision.Name = rule.Name
		decision.Probability = adjustWeight(rule.Key, rule.BaseProbability, signals)
		if decision.Probability > 100 {
			decision.Probability = 100
		}
		break
	}
	if !found {
		// Core may restart before Management has re-synchronized the persisted
		// strategy policy. Preserve the legacy behavior instead of muting all
		// interactions during that short bootstrap window.
		decision.Name = key
		decision.Probability = 100
		decision.Roll = 1
		decision.Triggered = true
		return decision
	}
	if decision.Probability <= 0 {
		return decision
	}
	if seed == "" {
		seed = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%d:trigger:%s:%s:%s", tenantID, category, key, seed)
	decision.Roll = int(h.Sum64()%100) + 1
	decision.Triggered = decision.Roll <= decision.Probability
	return decision
}

func adjustWeight(key string, weight int, signals Signals) int {
	key = strings.TrimSpace(key)
	switch key {
	case "welcome_named":
		switch {
		case signals.Entries30s >= 60:
			weight = weight * 20 / 100
		case signals.Entries30s >= 20:
			weight = weight * 50 / 100
		case signals.Entries30s <= 3:
			weight = weight * 180 / 100
		case signals.Entries30s <= 10:
			weight = weight * 130 / 100
		}
	case "welcome_batch":
		switch {
		case signals.Entries30s >= 60:
			// High entry flow lowers total welcome frequency, but batch welcome
			// still stays relatively more likely than named welcome.
			weight = weight * 60 / 100
		case signals.Entries30s >= 20:
			weight = weight * 80 / 100
		case signals.Entries30s <= 3:
			weight = weight * 110 / 100
		case signals.Entries30s <= 10:
			weight = weight * 90 / 100
		}
	case "reply_like":
		switch {
		case signals.Likes30s >= 300:
			weight = weight * 25 / 100
		case signals.Likes30s >= 100:
			weight = weight * 50 / 100
		}
	}
	if strings.EqualFold(strings.TrimSpace(signals.Heat), "OVERHEATED") && key != "reply_chat" {
		weight = weight * 60 / 100
	}
	if weight < 1 {
		return 1
	}
	return weight
}
