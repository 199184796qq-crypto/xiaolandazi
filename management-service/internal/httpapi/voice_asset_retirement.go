package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
)

func (s *Server) commitVoiceRewrite(ctx context.Context, tenant int64, root string, v model.LiveAgentPlanVersion, unit volcengineScriptUnit, version scriptplay.Version, cap int, refresh bool, retireRef ...string) (bool, error) {
	return commitScriptRewriteVersionRetiring(root, v, unit, version, cap, refresh, func(old scriptplay.Version) error {
		if s.store == nil {
			return nil
		}
		// Another current piece may share this exact paid sound. Keep its owner
		// active and retain the physical object rather than treating pool removal as
		// exclusive ownership. The removed piece is no longer selected from its pool.
		files, e := filepath.Glob(filepath.Join(scriptVoicePoolDir(root, v.PlanID), "*.json"))
		if e != nil {
			return e
		}
		current := scriptVoicePoolPath(root, v.PlanID, unit.poolID())
		for _, path := range files {
			if path == current {
				continue
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			var pool scriptVoicePoolFile
			if e = json.Unmarshal(b, &pool); e != nil {
				return e
			}
			for _, entry := range pool.Versions {
				if entry.AudioRef == old.AudioRef {
					return nil
				}
			}
		}
		id, e := strconv.ParseInt(old.AudioRef, 10, 64)
		if e != nil {
			return e
		}
		return s.store.RetireVoiceAsset(ctx, tenant, v.PlanID, id, filepath.Base(old.SourcePath))
	}, retireRef...)
}
