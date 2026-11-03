package httpapi

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/sharedfilelock"
)

const scriptPolicyTargetMS int64 = 80 * 60000
const scriptPolicyWindowMS int64 = 90 * 60000
const scriptPolicyCooldownMS int64 = 72 * 60000

type scriptPoolPlan struct {
	Caps  []int
	Pools []scriptplay.Pool
	Full  bool
}

// Capacity is an exposure calculation, not a fixed number of assets. Shorter
// actual rewrites increase the required count; single-mode pieces occur less
// often. Neither legacy VersionCap nor the old total-80 fuse is a budget now.
func scriptPlanPools(root string, v model.LiveAgentPlanVersion, units []volcengineScriptUnit) scriptPoolPlan {
	p := scriptPoolPlan{Caps: make([]int, len(units)), Pools: make([]scriptplay.Pool, len(units)), Full: len(units) > 0}
	weights := make([]float64, len(units))
	for i := range weights {
		weights[i] = 1
	}
	if script, ok := recordingScriptFromContext(v.GenerationContext); ok && script.EffectivePlayMode() == "single" {
		for _, group := range scriptPlayGroups(v, units) {
			for _, i := range group {
				weights[i] = 1 / float64(len(group))
			}
		}
	}
	means := make([]float64, len(units))
	round := float64(0)
	for i, u := range units {
		p.Pools[i] = scriptCompatiblePool(root, v, u)
		// A 4000-character source can become a 400..1600-character rewrite.
		estimate := float64(max(1, min(1600, hanCount(u.Text)))) * 60000 / 320
		var total float64
		count := 0
		for _, x := range p.Pools[i].Versions {
			total += float64(x.DurationMS)
			count++
		}
		if count > 0 {
			estimate = total / float64(count)
		}
		means[i] = math.Max(1000, estimate)
		round += means[i] * weights[i]
	}
	if round <= 0 {
		return p
	}
	for i := range units {
		p.Caps[i] = max(1, int(math.Ceil(float64(scriptPolicyTargetMS)*weights[i]/round)))
		// Correct the original/rewrite duration difference with measured inventory.
		inventory := int64(0)
		for _, x := range p.Pools[i].Versions {
			inventory += x.DurationMS
		}
		required := float64(scriptPolicyTargetMS) * means[i] * weights[i] / round
		if float64(inventory) < required && len(p.Pools[i].Versions) >= p.Caps[i] {
			p.Caps[i] = len(p.Pools[i].Versions) + int(math.Ceil((required-float64(inventory))/means[i]))
		}
		if len(p.Pools[i].Versions) < p.Caps[i] {
			p.Full = false
		}
	}
	return p
}

type scriptPolicyRetirement struct{ Piece, Ref string }
type scriptPoolPolicyState struct {
	InitialAudioMS    int64                    `json:"initial_audio_ms"`
	FirstObserved     time.Time                `json:"first_observed"`
	Carry             int                      `json:"carry"`
	Batch             uint64                   `json:"batch"`
	Retire            []scriptPolicyRetirement `json:"retire,omitempty"`
	LastAttempt       time.Time                `json:"last_attempt"`
	AttemptInventory  string                   `json:"attempt_inventory,omitempty"`
	UnchangedAttempts int                      `json:"unchanged_attempts,omitempty"`
}

func scriptPoolPolicyPath(root string, v model.LiveAgentPlanVersion) string {
	return filepath.Join(root, "script-pool-policy", strconv.FormatInt(v.TenantID, 10), strconv.FormatInt(v.PlanID, 10), shortScriptIdentity(scriptRewriteFactsKey(v)+scriptFirstParagraphKey(v, "policy"))+".json")
}

// One finite batch belongs to the whole plan/voice, not independently to every
// piece. The caller holds a plan-wide generation lock through synthesis/commit.
func scriptPolicyGeneration(root string, v model.LiveAgentPlanVersion, units []volcengineScriptUnit, now time.Time, attempt bool, reuseWarmup ...bool) (index, cap int, retire string, err error) {
	index = -1
	path := scriptPoolPolicyPath(root, v)
	release, e := sharedfilelock.Lock(context.Background(), path+".lock")
	if e != nil {
		err = e
		return
	}
	defer release()
	var st scriptPoolPolicyState
	if e = readVolcengineJSON(path, &st); e != nil && !errors.Is(e, os.ErrNotExist) {
		err = e
		return
	}
	if st.FirstObserved.IsZero() {
		st.FirstObserved = now
	}
	p := scriptPlanPools(root, v, units)
	inventory := int64(0)
	originals := int64(0)
	for _, pool := range p.Pools {
		for _, x := range pool.Versions {
			inventory += x.DurationMS
			if x.Kind == scriptplay.KindOriginal {
				originals += x.DurationMS
			}
		}
	}
	if st.InitialAudioMS == 0 {
		st.InitialAudioMS = inventory
	}
	// A fresh room reuses a sufficient warm pool even when aggregate historical
	// play counts already qualify it for refresh. Other mature rooms may still
	// service their shared batch; this room does not buy an unnecessary one.
	if p.Full && len(reuseWarmup) > 0 && reuseWarmup[0] {
		err = writeVolcengineJSON(path, st)
		return
	}
	// Five minutes of initial runway; replenish gradually toward the 80-minute
	// target in 75 minutes. Existing inventory is reused, not purchased again.
	fillDue := inventory < max(originals+75000, st.InitialAudioMS+5*60000+int64(now.Sub(st.FirstObserved).Milliseconds()*80/75))

	// For very large original inventories, defer refresh-slot procurement during
	// the first reusable 90 minutes. Afterwards reserve enough rewrites that a
	// protected original does not permanently make 30% replacement impossible.
	if p.Full && now.Sub(st.FirstObserved) >= 90*time.Minute {
		n, rewrites := 0, 0
		for _, pool := range p.Pools {
			for _, x := range pool.Versions {
				n++
				if x.Kind == scriptplay.KindRewrite {
					rewrites++
				}
			}
		}
		missing := max(0, int(math.Ceil((float64(n)*.3-float64(rewrites))/.7)))
		for i := 0; missing > 0 && len(units) > 0; i = (i + 1) % len(units) {
			p.Caps[i]++
			missing--
		}
	}
	// Fill the shallowest exposure layer first. A ready original is required;
	// unready originals remain the foreground producer's responsibility.
	best := math.Inf(1)
	for i, pool := range p.Pools {
		if len(pool.Versions) >= p.Caps[i] || !pool.HasOriginal(v.VoiceIdentity.VoiceID) {
			continue
		}
		score := float64(len(pool.Versions)) / float64(p.Caps[i])
		if score < best {
			index = i
			cap = p.Caps[i]
			best = score
		}
	}
	if index >= 0 && !fillDue && retire == "" {
		index = -1
	}
	if index < 0 && p.Full {
		present := map[string]bool{}
		total := 0
		qualified := true
		var eligible []scriptPolicyRetirement
		for i, pool := range p.Pools {
			for _, x := range pool.Versions {
				total++
				present[units[i].poolID()+"|"+x.AudioRef] = true
				if x.CompleteCount < rewriteRetireAfterPlays {
					qualified = false
				}
				if x.Kind == scriptplay.KindRewrite {
					eligible = append(eligible, scriptPolicyRetirement{units[i].poolID(), x.AudioRef})
				}
			}
		}
		remaining := st.Retire[:0]
		for _, x := range st.Retire {
			if present[x.Piece+"|"+x.Ref] {
				remaining = append(remaining, x)
			}
		}
		st.Retire = remaining
		if len(st.Retire) == 0 && qualified && len(eligible) > 0 {
			count := (total*30 + st.Carry) / 100
			// Carry is committed only on freezing a batch, never per failed attempt.
			st.Carry = (total*30 + st.Carry) % 100
			// Fewer than four extremely long assets cannot represent30% as
			// an integer nonempty batch; use one slot without negative debt.
			if count == 0 {
				count = 1
				st.Carry = 0
			}

			count = min(count, len(eligible))
			st.Retire = append([]scriptPolicyRetirement(nil), rotatePolicyRetirements(eligible, int(st.Batch)%len(eligible))[:count]...)
			st.Batch++
		}
		if len(st.Retire) > 0 {
			x := st.Retire[0]
			for i, u := range units {
				if u.poolID() == x.Piece {
					index = i
					cap = len(p.Pools[i].Versions)
					retire = x.Ref
					break
				}
			}
		}
	}
	// Failure/back-pressure does not turn the fast producer poll into a new paid
	// request on every tick. Successful cache additions naturally advance supply.
	if attempt && index >= 0 {
		inventoryID := ""
		for _, pool := range p.Pools {
			for _, x := range pool.Versions {
				inventoryID += x.AudioRef + ";"
			}
		}
		inventoryID = shortScriptIdentity(inventoryID)
		if inventoryID != st.AttemptInventory {
			st.UnchangedAttempts = 0
		}
		delay := 10 * time.Second * time.Duration(1<<min(5, st.UnchangedAttempts))
		if now.Sub(st.LastAttempt) < delay {
			index = -1
		} else {
			st.LastAttempt = now
			if inventoryID == st.AttemptInventory {
				st.UnchangedAttempts++
			}
			st.AttemptInventory = inventoryID
		}
	}
	err = writeVolcengineJSON(path, st)
	return
}

func (s *Server) tickScriptPoolPolicy(ctx context.Context, tenant, room int64, v model.LiveAgentPlanVersion, units []volcengineScriptUnit) {
	key := "policy|" + scriptPoolPolicyPath(recordingCacheMediaRoot(), v)
	scriptPolicyPoll.Lock()
	if time.Since(scriptPolicyPoll.last[key]) < 5*time.Second {
		scriptPolicyPoll.Unlock()
		return
	}
	scriptPolicyPoll.last[key] = time.Now()
	scriptPolicyPoll.Unlock()
	index, _, _, err := scriptPolicyGeneration(recordingCacheMediaRoot(), v, units, time.Now().UTC(), false, scriptPolicyRoomWarmup(recordingCacheMediaRoot(), tenant, room, v.PlanID, time.Now().UTC()))
	if err != nil || index < 0 {
		return
	}
	// No background queue of stale per-piece decisions. Coalesce the whole plan.
	rewriteWorker.Lock()
	if rewriteWorker.inFlight[key] {
		rewriteWorker.Unlock()
		return
	}
	rewriteWorker.inFlight[key] = true
	rewriteWorker.Unlock()
	go func() {
		defer func() { rewriteWorker.Lock(); delete(rewriteWorker.inFlight, key); rewriteWorker.Unlock() }()
		s.maybeGenerateRewriteVersion(ctx, tenant, room, v, units[index])
	}()
}

var scriptPolicyPoll = struct {
	sync.Mutex
	last map[string]time.Time
}{last: map[string]time.Time{}}

func rotatePolicyRetirements(items []scriptPolicyRetirement, start int) []scriptPolicyRetirement {
	return append(append([]scriptPolicyRetirement(nil), items[start:]...), items[:start]...)
}
