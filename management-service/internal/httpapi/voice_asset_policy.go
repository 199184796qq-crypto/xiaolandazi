package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voiceasset"
)

type voiceUploadTrackerKey struct{}

func (s *Server) voiceUploadContext(ctx context.Context, tenant, plan int64) context.Context {
	if s.store == nil {
		return ctx
	}
	return context.WithValue(ctx, voiceUploadTrackerKey{}, func(driver, bucket, key string) error {
		return s.store.RegisterVoiceUpload(ctx, tenant, plan, driver, bucket, key)
	})
}
func recordVoiceUpload(ctx context.Context, driver, bucket, key string) error {
	if f, ok := ctx.Value(voiceUploadTrackerKey{}).(func(string, string, string) error); ok {
		return f(driver, bucket, key)
	}
	return nil
}
func (s *Server) voiceAssetVoices(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	var scope voiceasset.Scope
	if readJSON(w, r, &scope) != nil {
		writeError(w, 400, "范围无效")
		return
	}
	if _, ok := s.voiceCleanupPlan(w, r, scope); !ok {
		return
	}
	items, e := s.store.VoiceCleanupVoices(r.Context(), scope)
	if e != nil {
		writeError(w, 503, "读取完整音色身份失败")
		return
	}
	if scope.Category == "interaction" {
		more, e := ttsgateway.NewInteractionAudioCache(recordingCacheMediaRoot(), s.executionRealm).ScopedVoices(r.Context(), scope)
		if e != nil {
			writeError(w, 503, "互动缓存身份待核对")
			return
		}
		items = append(items, more...)
	}
	writeJSON(w, 200, map[string]any{"items": items, "legacy_unknown": "旧记录缺少完整身份时仅支持全部音色清除"})
}

// Initial rollout intentionally records candidates only. Changing this to
// destructive automatic GC requires a separately reviewed ownership inventory.
func voiceAutomaticCandidate(now time.Time, lastUse *time.Time, strong bool, retiredAt *time.Time) string {
	if strong {
		return "protected_reference"
	}
	if retiredAt != nil {
		if now.Before(retiredAt.Add(7 * 24 * time.Hour)) {
			return "retired_observation"
		}
		return "retired_candidate"
	}
	if lastUse == nil {
		return "usage_unknown_review"
	}
	if now.Before(lastUse.Add(30 * 24 * time.Hour)) {
		return "recently_used"
	}
	if now.Before(lastUse.Add(37 * 24 * time.Hour)) {
		return "idle_observation"
	}
	return "idle_candidate"
}
func (s *Server) scanVoiceAssetPolicy(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	var scope voiceasset.Scope
	if readJSON(w, r, &scope) != nil {
		writeError(w, 400, "范围无效")
		return
	}
	if _, ok := s.voiceCleanupPlan(w, r, scope); !ok {
		return
	}
	// Every currently stored pool member is retained conservatively. A malformed
	// pool aborts the scan instead of reclassifying its members as unreferenced.
	strong := map[int64]bool{}
	files, e := filepath.Glob(filepath.Join(scriptVoicePoolDir(recordingCacheMediaRoot(), scope.Plan), "*.json"))
	if e != nil {
		writeError(w, 503, "读取池引用失败")
		return
	}
	for _, path := range files {
		b, e := os.ReadFile(path)
		if e != nil {
			writeError(w, 503, "读取池引用失败")
			return
		}
		var p scriptVoicePoolFile
		if json.Unmarshal(b, &p) != nil {
			writeError(w, 409, "池索引待核对")
			return
		}
		for _, v := range p.Versions {
			if id, e := strconv.ParseInt(v.AudioRef, 10, 64); e == nil {
				strong[id] = true
			}
		}
	}
	counts := map[string]int64{}
	var after int64
	for {
		assets, e := s.store.VoiceCleanupAssets(r.Context(), scope, after, 200)
		if e != nil {
			writeError(w, 503, "盘点失败，未执行删除")
			return
		}
		for _, a := range assets {
			after = a.ID
			if a.Status == "deleted" {
				continue
			}
			st, ok := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
			if !ok {
				counts["owner_unknown_review"]++
				continue
			}
			if st.Scope.Category != scope.Category || (scope.Voice != "" && st.Scope.Voice != scope.Voice) {
				continue
			}
			at, e := s.store.VoiceAssetLastPCM(r.Context(), scope.Tenant, a.ID)
			if e != nil {
				writeError(w, 503, "读取真实使用记录失败")
				return
			}
			var last *time.Time
			if t, e := time.ParseInLocation("2006-01-02 15:04:05.999", at, time.UTC); e == nil {
				last = &t
			}
			counts[voiceAutomaticCandidate(time.Now().UTC(), last, strong[a.ID], nil)]++
		}
		if len(assets) < 200 {
			break
		}
	}
	orphans, e := s.store.ScanVoiceUploadOrphans(r.Context(), scope.Tenant, scope.Plan)
	if e != nil {
		writeError(w, 503, "上传对账失败")
		return
	}
	for k, n := range orphans {
		counts[k] = n
	}
	result := map[string]any{"scope": scope, "dry_run": true, "deletion_enabled": false, "counts": counts, "retired_days": 7, "idle_days": 30, "observation_days": 7, "orphan_hours": 24, "orphan_independent_scans": 2, "legacy_objects_without_owner": "review_required", "bucket_versions_and_backups": "not_deleted_or_counted_as_freed"}
	// A durable local report supports operator review without storing URLs.
	path := filepath.Join(recordingCacheMediaRoot(), "voice-cleanup-reports", voiceasset.Digest(scope)+".json")
	if e = writeVolcengineJSON(path, result); e != nil {
		writeError(w, 503, "保存清单失败")
		return
	}
	writeJSON(w, 200, result)
}
