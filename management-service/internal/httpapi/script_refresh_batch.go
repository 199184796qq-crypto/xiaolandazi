package httpapi

import (
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type scriptRefreshBatch struct {
	Retire []string `json:"retire"`
}

// Thirty percent is a finite replacement batch, not a probability per play.
// A batch survives budget deferral. New audio must complete four real plays
// before all pool members qualify for another batch.
func scriptRefreshRetire(root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, pool scriptplay.Pool, cap int) (string, bool, error) {
	path := filepath.Join(root, "script-refresh", strconv.FormatInt(v.PlanID, 10), unit.poolID()+"-"+scriptplay.Fingerprint(v.VoiceIdentity.Model+"/"+v.VoiceIdentity.VoiceID)+".json")
	var batch scriptRefreshBatch
	if err := readVolcengineJSON(path, &batch); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", false, err
	}
	present := map[string]scriptplay.Version{}
	for _, entry := range pool.Versions {
		present[entry.AudioRef] = entry
	}
	var remaining []string
	for _, ref := range batch.Retire {
		if _, ok := present[ref]; ok {
			remaining = append(remaining, ref)
		}
	}
	if len(remaining) == 0 {
		if len(pool.Versions) < cap {
			return "", false, nil
		}
		var candidates []scriptplay.Version
		for _, entry := range pool.Versions {
			if entry.CompleteCount < rewriteRetireAfterPlays {
				return "", false, nil
			}
			if entry.Kind == scriptplay.KindRewrite {
				candidates = append(candidates, entry)
			}
		}
		if len(candidates) == 0 {
			return "", false, nil
		}
		sort.Slice(candidates, func(i, j int) bool {
			return scriptPlaybackBefore(candidates[i].LastPlayedAt, candidates[j].LastPlayedAt)
		})
		count := min(len(candidates), max(1, cap*30/100))
		for _, entry := range candidates[:count] {
			remaining = append(remaining, entry.AudioRef)
		}
	}
	batch.Retire = remaining
	if err := writeVolcengineJSON(path, batch); err != nil {
		return "", false, err
	}
	return remaining[0], true, nil
}
