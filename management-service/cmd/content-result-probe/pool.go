package main

import "sync"

type directionPool struct {
	Owner       binding     `json:"program_binding"`
	Fingerprint string      `json:"source_fingerprint"`
	RequestID   string      `json:"planning_request_id"`
	Items       []direction `json:"items"`
}

type usage struct {
	Total         int             `json:"total_played"`
	SinceRefresh  int             `json:"played_since_refresh"`
	Confirmed     map[string]bool `json:"confirmed_playback_ids"`
	DirectionUses map[string]int  `json:"direction_plays"`
}

type poolSnapshot struct {
	Pools           map[string]directionPool    `json:"pools"`
	Scenes          map[string]map[string]usage `json:"scene_usage"`
	History         map[string][]directionPool  `json:"saved_pool_generations"`
	DirectionTotals map[string]int              `json:"lifetime_direction_plays"`
}

type poolCache struct {
	mu   sync.Mutex
	data poolSnapshot
}

func newPoolCache() *poolCache {
	return &poolCache{data: poolSnapshot{Pools: map[string]directionPool{}, Scenes: map[string]map[string]usage{}, History: map[string][]directionPool{}, DirectionTotals: map[string]int{}}}
}

func (p *poolCache) lookup(scene, angle, fingerprint string, limit int) (directionPool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pool, ok := p.data.Pools[angle]
	if !ok || pool.Fingerprint != fingerprint || len(pool.Items) == 0 {
		return directionPool{}, false
	}
	if limit > 0 && p.data.Scenes[scene][angle].SinceRefresh >= limit {
		return directionPool{}, false
	}
	// Reuse all generations still matching the effective facts/rules. Retain
	// older fingerprints in history, but never mix them into the usable bank.
	seen := map[string]bool{}
	items := []direction{}
	add := func(g directionPool) {
		if g.Fingerprint != fingerprint {
			return
		}
		for _, item := range g.Items {
			if !seen[item.ID] {
				items = append(items, item)
				seen[item.ID] = true
			}
		}
	}
	add(pool)
	for i := len(p.data.History[angle]) - 1; i >= 0; i-- {
		add(p.data.History[angle][i])
	}
	pool.Items = items
	return pool, true
}

func (p *poolCache) put(scene string, pool directionPool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pool.Owner = freezeBinding(pool.Owner)
	pool.Items = append([]direction(nil), pool.Items...)
	angle := pool.Owner.TaskKey
	if p.data.History == nil {
		p.data.History = map[string][]directionPool{}
	}
	// Preserve an older flat snapshot when first upgrading to a version bank.
	if old, ok := p.data.Pools[angle]; ok && len(p.data.History[angle]) == 0 {
		p.data.History[angle] = append(p.data.History[angle], old)
	}
	exists := false
	for _, old := range p.data.History[angle] {
		if old.RequestID == pool.RequestID {
			exists = true
		}
	}
	if !exists {
		p.data.History[angle] = append(p.data.History[angle], pool)
	}
	p.data.Pools[angle] = pool
	if p.data.Scenes[scene] == nil {
		p.data.Scenes[scene] = map[string]usage{}
	}
	u := p.data.Scenes[scene][angle]
	u.SinceRefresh = 0
	p.data.Scenes[scene][angle] = u
}

// Prefer directions less used in this scene, then less used across scenes.
// Saving a new generation adds options; it never deletes valid old content.
func (p *poolCache) choose(scene, angle string, pool directionPool) direction {
	p.mu.Lock()
	defer p.mu.Unlock()
	u := p.data.Scenes[scene][angle]
	best := pool.Items[0]
	for _, item := range pool.Items[1:] {
		if u.DirectionUses[item.ID] < u.DirectionUses[best.ID] ||
			(u.DirectionUses[item.ID] == u.DirectionUses[best.ID] && p.data.DirectionTotals[item.ID] < p.data.DirectionTotals[best.ID]) {
			best = item
		}
	}
	return best
}

func (p *poolCache) uses(scene, angle string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.data.Scenes[scene][angle].SinceRefresh
}

// Call only after an actual output acknowledgement in a production adapter.
// The offline probe explicitly simulates this acknowledgement; generation and
// staging do not count. Duplicate acknowledgements are idempotent.
func (p *poolCache) confirmPlayed(scene, angle, playbackID string, selected ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.data.Scenes[scene] == nil {
		p.data.Scenes[scene] = map[string]usage{}
	}
	u := p.data.Scenes[scene][angle]
	if u.Confirmed == nil {
		u.Confirmed = map[string]bool{}
	}
	if u.Confirmed[playbackID] {
		return
	}
	u.Confirmed[playbackID] = true
	u.Total++
	u.SinceRefresh++
	if len(selected) > 0 && selected[0] != "" {
		if u.DirectionUses == nil {
			u.DirectionUses = map[string]int{}
		}
		if p.data.DirectionTotals == nil {
			p.data.DirectionTotals = map[string]int{}
		}
		u.DirectionUses[selected[0]]++
		p.data.DirectionTotals[selected[0]]++
	}
	p.data.Scenes[scene][angle] = u
}

func (p *poolCache) snapshot() poolSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := poolSnapshot{Pools: map[string]directionPool{}, Scenes: map[string]map[string]usage{}, History: map[string][]directionPool{}, DirectionTotals: map[string]int{}}
	for k, v := range p.data.Pools {
		v.Owner = freezeBinding(v.Owner)
		v.Items = append([]direction(nil), v.Items...)
		d.Pools[k] = v
	}
	for angle, generations := range p.data.History {
		for _, g := range generations {
			g.Owner = freezeBinding(g.Owner)
			g.Items = append([]direction(nil), g.Items...)
			d.History[angle] = append(d.History[angle], g)
		}
	}
	for id, count := range p.data.DirectionTotals {
		d.DirectionTotals[id] = count
	}
	for scene, angles := range p.data.Scenes {
		d.Scenes[scene] = map[string]usage{}
		for angle, u := range angles {
			confirmed := map[string]bool{}
			for k, v := range u.Confirmed {
				confirmed[k] = v
			}
			u.Confirmed = confirmed
			uses := map[string]int{}
			for id, count := range u.DirectionUses {
				uses[id] = count
			}
			u.DirectionUses = uses
			d.Scenes[scene][angle] = u
		}
	}
	return d
}
