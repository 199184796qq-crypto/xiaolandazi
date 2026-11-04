package httpapi

import (
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Stable piece IDs retain paid text variants after an edit. Synthesis identity
// is still strict: the saved spoken text, model, voice, instructions and rate
// must resolve to the cache we are about to use. Metadata alone is not readiness.
func scriptVersionAudio(root string, unit volcengineScriptUnit, v model.LiveAgentPlanVersion, version scriptplay.Version) (volcengineCachedAudio, bool) {
	if current := scriptRewriteFactsKey(v); current != "" && version.Kind == scriptplay.KindOriginal &&
		((version.FactsKey != "" && version.FactsKey != current) || (version.FactsKey == "" && strings.TrimSpace(version.Text) != strings.TrimSpace(unit.Text))) {
		return volcengineCachedAudio{}, false
	}
	if current := scriptRewriteFactsKey(v); version.Kind == scriptplay.KindRewrite && current != "" && version.FactsKey != current {
		return volcengineCachedAudio{}, false
	}
	if version.VoiceID != v.VoiceIdentity.VoiceID || version.Model != v.VoiceIdentity.Model || strings.TrimSpace(version.Text) == "" || version.SourcePath == "" {
		return volcengineCachedAudio{}, false
	}
	relative, err := filepath.Rel(root, version.SourcePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return volcengineCachedAudio{}, false
	}
	key := scriptFirstParagraphKey(v, version.Text)
	if filepath.Base(version.SourcePath) != key {
		return volcengineCachedAudio{}, false
	}
	cache, err := loadVolcengineParagraphCache(filepath.Join(version.SourcePath, "cache.json"), key)
	if err != nil {
		return volcengineCachedAudio{}, false
	}
	audio, ok := cache.Audio[100]
	if !ok || audio.DurationMS <= 0 || audio.VoiceID != version.VoiceID || strconv.FormatInt(audio.AssetID, 10) != version.AudioRef || audio.AssetID <= 0 {
		return volcengineCachedAudio{}, false
	}
	audio.Text = version.Text
	return audio, true
}

func scriptCompatiblePool(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit) scriptplay.Pool {
	pool, _ := loadScriptVoicePool(root, v.PlanID, unit.poolID())
	var compatible scriptplay.Pool
	for _, version := range pool.Versions {
		if _, ok := scriptVersionAudio(root, unit, v, version); ok {
			compatible.Versions = append(compatible.Versions, version)
		}
	}
	return compatible
}

func scriptCompatiblePoolAudio(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, scope string, rng *rand.Rand, history ...map[string]scriptRoomPlayState) (volcengineCachedAudio, string, bool) {
	pool := scriptCompatiblePool(root, v, unit)
	if len(pool.Versions) == 0 {
		return volcengineCachedAudio{}, "", false
	}
	// Other rooms do not move this room's playback history. The shared pool
	// counters remain aggregate evidence for retirement only.
	if len(history) > 0 {
		for i := range pool.Versions {
			entry := history[0][scriptRoomAssetKey(unit.poolID(), pool.Versions[i].AudioRef)]
			pool.Versions[i].LastPlayedAt = entry.LastPlayedAt
			pool.Versions[i].PlayCount = entry.PlayCount
		}
	}
	key := root + "|" + scope + "|" + strconv.FormatInt(v.PlanID, 10) + "|" + unit.poolID() + "|" + v.VoiceIdentity.Model + "|" + v.VoiceIdentity.VoiceID
	scriptPoolRotation.Lock()
	defer scriptPoolRotation.Unlock()
	used := scriptPoolRotation.used[key]
	var candidates []scriptplay.Version
	for _, version := range pool.Versions {
		if !used[scriptVersionKey(version)] {
			candidates = append(candidates, version)
		}
	}
	if len(candidates) == 0 {
		used = nil
		candidates = pool.Versions
	}
	if used == nil {
		used = map[string]bool{}
		scriptPoolRotation.used[key] = used
	}
	oldest := candidates[0].LastPlayedAt
	for _, version := range candidates {
		if scriptPlaybackBefore(version.LastPlayedAt, oldest) {
			oldest = version.LastPlayedAt
		}
	}
	var picks []scriptplay.Version
	for _, version := range candidates {
		if !scriptPlaybackBefore(version.LastPlayedAt, oldest) && !scriptPlaybackBefore(oldest, version.LastPlayedAt) {
			picks = append(picks, version)
		}
	}
	picked := picks[rng.Intn(len(picks))]
	audio, ok := scriptVersionAudio(root, unit, v, picked)
	if !ok {
		return volcengineCachedAudio{}, "", false
	}
	used[scriptVersionKey(picked)] = true
	return audio, picked.Text, true
}

func scriptPlaybackBefore(a, b string) bool {
	if a == "" || b == "" {
		return a == "" && b != ""
	}
	at, ae := time.Parse(time.RFC3339Nano, a)
	bt, be := time.Parse(time.RFC3339Nano, b)
	if ae == nil && be == nil {
		return at.Before(bt)
	}
	return a < b
}

// Commit replacement atomically. An identical model reply can reuse a paid
// original; that is not a newly added version and must not retire anything.
func commitScriptRewriteVersion(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, version scriptplay.Version, cap int, refresh bool, retireRef ...string) (bool, error) {
	return commitScriptRewriteVersionRetiring(root, v, unit, version, cap, refresh, nil, retireRef...)
}
func commitScriptRewriteVersionRetiring(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, version scriptplay.Version, cap int, refresh bool, retire func(scriptplay.Version) error, retireRef ...string) (bool, error) {
	release, err := lockScriptPoolStore(root)
	if err != nil {
		return false, err
	}
	defer release()
	pool, err := loadScriptVoicePoolLocked(root, v.PlanID, unit.poolID())
	if err != nil {
		return false, err
	}
	compatible := []int{}
	for i, existing := range pool.Versions {
		if existing.Model == version.Model && existing.VoiceID == version.VoiceID && existing.AudioRef == version.AudioRef {
			// A freshly accepted reply can match an old paid WAV exactly. Refresh
			// its rule approval without buying again or resetting real play counts.
			if version.FactsKey != "" && version.FactsKey == scriptRewriteFactsKey(v) && existing.Text == version.Text &&
				filepath.Base(version.SourcePath) == scriptFirstParagraphKey(v, version.Text) && existing.FactsKey != version.FactsKey {
				pool.Versions[i].FactsKey = version.FactsKey
				return false, saveScriptVoicePoolLocked(root, v.PlanID, unit.poolID(), pool)
			}
			return false, nil
		}
		if _, ok := scriptVersionAudio(root, unit, v, existing); ok {
			compatible = append(compatible, i)
		}
	}
	retireIndex := -1
	if len(compatible) >= cap {
		if !refresh {
			return false, errors.New("改写槽位已满，保留已付费声音")
		}
		for _, i := range compatible {
			old := pool.Versions[i]
			if len(retireRef) > 0 && old.AudioRef != retireRef[0] {
				continue
			}
			if len(retireRef) > 0 && old.CompleteCount < rewriteRetireAfterPlays {
				continue
			}
			if old.Kind == scriptplay.KindRewrite && old.PlayCount >= rewriteRetireAfterPlays && (retireIndex < 0 || scriptPlaybackBefore(old.LastPlayedAt, pool.Versions[retireIndex].LastPlayedAt)) {
				retireIndex = i
			}
		}
		if retireIndex < 0 {
			return false, errors.New("没有已播足次数的可替换版本")
		}
	}
	if version.CreatedAt == "" {
		version.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := pool.Add(version); err != nil {
		return false, err
	}
	if retireIndex >= 0 {
		if retire != nil {
			if err := retire(pool.Versions[retireIndex]); err != nil {
				return false, err
			}
		}
		pool.Versions = append(pool.Versions[:retireIndex], pool.Versions[retireIndex+1:]...)
	}
	return true, saveScriptVoicePoolLocked(root, v.PlanID, unit.poolID(), pool)
}

func scriptVersionForVoice(v model.LiveAgentPlanVersion, voice recordingScriptVoice) model.LiveAgentPlanVersion {
	// PCM receipts carry only model/id. Restore the saved selection's complete
	// identity before bookkeeping or background rewriting for a secondary voice.
	if voice.Identity == nil {
		if script, ok := recordingScriptFromContext(v.GenerationContext); ok {
			for _, selected := range script.Voices {
				if selected.Identity != nil && selected.VoiceID == voice.VoiceID && (voice.Model == "" || selected.Model == voice.Model) {
					voice.Identity = selected.Identity
					break
				}
			}
		}
	}
	if voice.Identity != nil {
		rate := v.VoiceIdentity.Rate
		v.VoiceIdentity = *voice.Identity
		v.VoiceIdentity.Rate = rate
	}
	v.VoiceIdentity.VoiceID = voice.VoiceID
	if voice.Model != "" {
		v.VoiceIdentity.Model = voice.Model
	}
	return v
}

func scriptVoiceBlockLength(v model.LiveAgentPlanVersion, units []volcengineScriptUnit) int {
	block := 4
	if script, ok := recordingScriptFromContext(v.GenerationContext); ok {
		block = script.EffectiveMinVoiceBlock()
	}
	// Every piece must have enough distinct versions to sustain the block.
	available := 0
	for i, unit := range units {
		count := len(scriptCompatiblePool(recordingCacheMediaRoot(), v, unit).Versions)
		if i == 0 || count < available {
			available = count
		}
	}
	return max(block, available)
}
