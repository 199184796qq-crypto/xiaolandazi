package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"livecompanion/management/internal/voiceasset"
)

// Inspect only generated-paragraph namespaces belonging to this tenant/plan.
// The cache must name a real matching DB owner; directory names alone are not
// deletion authority. Old room-specific and shared plan layouts are both read.
func (s *Server) discoverVoiceLocalSources(ctx context.Context, j voiceasset.Job) error {
	root := recordingCacheMediaRoot()
	tenant := filepath.Join(root, strconv.FormatInt(j.Scope.Tenant, 10))
	entries, e := os.ReadDir(tenant)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	bases := []string{filepath.Join(tenant, "plans", strconv.FormatInt(j.Scope.Plan, 10), "recording-cache")}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "plans" {
			continue
		}
		if _, e := strconv.ParseInt(entry.Name(), 10, 64); e == nil {
			bases = append(bases, filepath.Join(tenant, entry.Name(), strconv.FormatInt(j.Scope.Plan, 10), "recording-cache"))
		}
	}
	for _, base := range bases {
		for _, ns := range []string{"script-first-v1", "volcengine-longtext-v1"} {
			for _, part := range []string{"paragraphs", "plan-paragraphs", "plan-paragraphs/paragraphs"} {
				parent := filepath.Join(base, ns, filepath.FromSlash(part))
				dirs, e := os.ReadDir(parent)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil {
					return e
				}
				sort.Slice(dirs, func(i, k int) bool { return dirs[i].Name() < dirs[k].Name() })
				for _, dir := range dirs {
					if e = ctx.Err(); e != nil {
						return e
					}
					if !dir.IsDir() || !voiceasset.DigestValid(dir.Name()) {
						continue
					}
					path := filepath.Join(parent, dir.Name())
					if e = validateVoiceSourcePath(root, path, j.Scope); e != nil {
						return e
					}
					raw, e := os.ReadFile(filepath.Join(path, "cache.json"))
					if os.IsNotExist(e) {
						continue
					}
					if e != nil {
						return e
					}
					if len(raw) > 8<<20 {
						return errors.New("legacy_cache_index_oversized")
					}
					var cache volcengineParagraphCache
					if json.Unmarshal(raw, &cache) != nil || cache.Key != dir.Name() {
						return errors.New("legacy_cache_index_unclassified")
					}
					ids := map[int64]bool{}
					for _, audio := range cache.Audio {
						if audio.AssetID > 0 {
							ids[audio.AssetID] = true
						}
						for _, block := range audio.Blocks {
							if block.AssetID > 0 {
								ids[block.AssetID] = true
							}
						}
					}
					for id := range ids {
						a, e := s.store.RawVoiceAsset(ctx, j.Scope.Tenant, id)
						if e != nil {
							return e
						}
						st, known := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
						if !known || st.Scope.Plan != j.Scope.Plan || st.Scope.Category != j.Scope.Category {
							continue
						}
						if j.Scope.Voice != "" && st.Scope.Voice != j.Scope.Voice {
							continue
						}
						if key, ok := a.Metadata["paragraph_key"].(string); ok && key != "" && key != dir.Name() {
							return voiceasset.ErrConflict
						}
						info, statErr := os.Stat(filepath.Join(path, "cache.json"))
						if statErr != nil {
							return statErr
						}
						if e = s.store.RegisterLegacyVoiceLocalSource(ctx, st, path, id, info.ModTime()); e != nil {
							return e
						}
					}
				}
			}
		}
	}
	return nil
}

// Keep provider task/request IDs and file metadata for cost reconciliation;
// never retain an audio backup, credential, prompt or signed supplier URL.
func preserveVoiceLocalEvidence(root, path string, j voiceasset.Job, hash string) error {
	files := []map[string]any{}
	allowed := map[string]bool{"task_id": true, "request_id": true, "provider_request_id": true, "complete": true, "version": true, "key": true, "input_sha": true, "source_sha256": true, "audio_sha": true, "status": true, "voice_id": true}
	err := filepath.WalkDir(path, func(name string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		info, e := entry.Info()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(path, name)
		if e != nil {
			return e
		}
		item := map[string]any{"name": rel, "bytes": info.Size(), "modified_at": info.ModTime().UTC()}
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".json") && info.Size() <= 1<<20 {
			raw, e := os.ReadFile(name)
			if e != nil {
				return e
			}
			var record map[string]any
			if json.Unmarshal(raw, &record) == nil {
				safe := map[string]any{}
				for key, value := range record {
					if !allowed[key] {
						continue
					}
					switch v := value.(type) {
					case string:
						if len(v) <= 512 && !strings.Contains(v, "://") {
							safe[key] = v
						}
					case bool, float64:
						safe[key] = value
					}
				}
				item["receipt_fields"] = safe
			}
		}
		files = append(files, item)
		return nil
	})
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return writeVolcengineJSON(filepath.Join(root, "voice-cleanup-evidence", j.ID, hash+".json"), map[string]any{"operation_id": j.ID, "scope": j.Scope, "path_hash": hash, "files": files, "audio_backup": false})
}
