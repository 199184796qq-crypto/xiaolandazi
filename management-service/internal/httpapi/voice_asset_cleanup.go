package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/sharedfilelock"
	"livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voiceasset"
)

func (s *Server) registerVoiceAssetCleanupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/liveops/voice-assets/voices", s.voiceAssetVoices)
	mux.HandleFunc("POST /api/v1/liveops/voice-assets/scan", s.scanVoiceAssetPolicy)
	mux.HandleFunc("GET /api/v1/liveops/voice-assets/plans", s.voiceAssetCleanupPlans)
	mux.HandleFunc("POST /api/v1/liveops/voice-assets/preview", s.previewVoiceAssetCleanup)
	mux.HandleFunc("POST /api/v1/liveops/voice-assets/clear", s.clearVoiceAssets)
	mux.HandleFunc("GET /api/v1/liveops/voice-assets/jobs", s.voiceAssetCleanupJobs)
}
func (s *Server) voiceAssetCleanupPlans(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	tenant, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
	if tenant <= 0 {
		writeError(w, 400, "用户无效")
		return
	}
	plans, e := s.store.ListLiveAgentPlans(r.Context(), tenant)
	if e != nil {
		writeError(w, 503, "读取方案失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": plans})
}
func (s *Server) voiceCleanupPlan(w http.ResponseWriter, r *http.Request, scope voiceasset.Scope) (model.LiveAgentPlan, bool) {
	if e := scope.Validate(); e != nil {
		writeError(w, 400, e.Error())
		return model.LiveAgentPlan{}, false
	}
	plan, e := s.store.GetLiveAgentPlan(r.Context(), scope.Tenant, scope.Plan)
	if e != nil {
		writeError(w, 404, "方案不存在或无权操作")
		return plan, false
	}
	return plan, true
}
func (s *Server) previewVoiceAssetCleanup(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	var input struct {
		voiceasset.Scope
		Mode string `json:"mode"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "清理范围格式错误")
		return
	}
	scope := input.Scope
	plan, ok := s.voiceCleanupPlan(w, r, scope)
	if !ok {
		return
	}
	stamp, e := s.store.VoiceCacheStamp(r.Context(), scope)
	if e != nil {
		writeError(w, 503, "读取声音清理状态失败")
		return
	}
	counts := map[string]uint64{"database_records": 0, "candidate_records": 0, "review_required": 0, "object_count_unverified": 0, "expected_bytes_unverified": 0}
	objects := map[string]uint64{}
	inventoryObjects := map[string]model.MediaAsset{}
	var after int64
	for {
		assets, e := s.store.VoiceCleanupAssets(r.Context(), scope, after, 200)
		if e != nil {
			writeError(w, 503, "读取声音清单失败")
			return
		}
		for _, a := range assets {
			after = a.ID
			counts["database_records"]++
			st, known := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
			if !known {
				counts["review_required"]++
				continue
			}
			if st.Scope.Category != scope.Category {
				continue
			}
			if scope.Voice != "" && st.Scope.Voice != scope.Voice {
				if st.Scope.Voice == "" {
					counts["review_required"]++
				}
				continue
			}
			if a.Status == "deleted" {
				continue
			}
			counts["candidate_records"]++
			objects[voiceasset.Digest([]string{a.StorageDriver, a.StorageBucket, a.ObjectKey})] = a.SizeBytes
			inventoryObjects[voiceasset.Digest([]string{a.StorageDriver, a.StorageBucket, a.ObjectKey})] = a
			for _, field := range voiceasset.SidecarFields {
				if key, _ := a.Metadata[field].(string); key != "" {
					hash := voiceasset.Digest([]string{a.StorageDriver, a.StorageBucket, key})
					objects[hash] = 0
					side := a
					side.ObjectKey = key
					side.SizeBytes = 0
					inventoryObjects[hash] = side
				}
			}
		}
		if len(assets) < 200 {
			break
		}
	}
	for _, bytes := range objects {
		counts["expected_bytes_unverified"] += bytes
	}
	counts["object_count_unverified"] = uint64(len(objects))
	verified := map[string]uint64{}
	versionState := "unknown"
	if input.Mode != "local_only" {
		verified, versionState, e = s.inventoryVoiceObjects(r.Context(), scope, inventoryObjects)
	}
	if e != nil {
		writeError(w, 503, "OSS清单未完成，未允许历史删除；请重新预览")
		return
	}
	for key, n := range verified {
		counts[key] = n
	}
	report := voiceCleanupInventory{Scope: scope, Expected: stamp, At: time.Now().UTC(), Complete: input.Mode != "local_only", Counts: counts}
	if e = writeVolcengineJSON(voiceCleanupInventoryPath(scope), report); e != nil {
		writeError(w, 503, "保存盘点证据失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"scope": scope, "expected": stamp, "plan_name": plan.Name, "room_ids": plan.RoomIDs, "counts": counts, "physical_inventory_complete": input.Mode != "local_only", "bucket_version_state": versionState, "backup_state": "not_deleted", "applies_to": "旧代生成声音；上传原始录音、克隆样本和直播录音保留", "playback_policy": "已下发声音等待实际引用释放，其余旧声音禁止新使用"})
}
func (s *Server) clearVoiceAssets(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage")
	if !ok {
		return
	}
	var input voiceasset.ClearRequest
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "清除请求格式错误")
		return
	}
	input.Actor = actor.UserID
	if e := input.Validate(); e != nil {
		writeError(w, 400, e.Error())
		return
	}
	if input.Mode == "" {
		input.Mode = "history"
	}
	if _, ok = s.voiceCleanupPlan(w, r, input.Scope); !ok {
		return
	}
	if existing, e := s.store.VoiceCleanupJob(r.Context(), input.Scope.Tenant, input.RequestID); e == nil {
		if existing.Scope != input.Scope || existing.Mode != input.Mode {
			writeError(w, 409, "操作编号范围不一致")
			return
		}
	} else if input.Mode != "local_only" {
		var proof voiceCleanupInventory
		if readVolcengineJSON(voiceCleanupInventoryPath(input.Scope), &proof) != nil || !proof.Complete || proof.Expected != input.Expected || proof.Scope != input.Scope || time.Since(proof.At) > 15*time.Minute || proof.At.After(time.Now().UTC()) {
			writeError(w, 409, "清理清单已过期或未完整盘点，请重新预览")
			return
		}
	}
	job, e := s.store.ClearVoiceAssets(r.Context(), input)
	if errors.Is(e, voiceasset.ErrConflict) {
		writeError(w, 409, e.Error())
		return
	}
	if e != nil {
		writeError(w, 503, "提交声音清除失败，请凭同一操作编号重试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job, "reuse_disabled": job.Mode != "local_only", "physical_delete_complete": false})
}
func (s *Server) voiceAssetCleanupJobs(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "system.settings.liveops.manage"); !ok {
		return
	}
	tenant, _ := strconv.ParseInt(r.URL.Query().Get("tenant_id"), 10, 64)
	plan, _ := strconv.ParseInt(r.URL.Query().Get("plan_id"), 10, 64)
	if tenant <= 0 || plan <= 0 {
		writeError(w, 400, "租户或方案无效")
		return
	}
	jobs, e := s.store.ListVoiceCleanupJobs(r.Context(), tenant, plan)
	if e != nil {
		writeError(w, 503, "读取清理进度失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"items": jobs})
}

// Only records tied to an exact accepted task are released. Core query errors
// preserve leases; neither an idle device flag nor a wall-clock duration is EOF.
func (s *Server) reconcileVoiceAssetLeases(ctx context.Context) error {
	leases, e := s.store.OutstandingVoiceAssetLeases(ctx)
	if e != nil {
		return e
	}
	if len(leases) == 0 {
		return nil
	}
	if s.core == nil {
		return voiceasset.ErrBusy
	}
	programs := map[[2]int64]coreclient.ProgramRefreshSnapshot{}
	failed := map[[2]int64]bool{}
	sessions := map[[2]int64]model.LiveRuntimeSession{}
	for _, l := range leases {
		_ = s.store.TouchVoiceAssetLease(ctx, l)
		key := [2]int64{l.Tenant, l.Room}
		p, exists := programs[key]
		if !exists && !failed[key] {
			p, e = s.core.GetRoomProgramSnapshot(ctx, l.Tenant, l.Room)
			if e != nil {
				failed[key] = true
			} else {
				programs[key] = p
			}
		}
		if failed[key] {
			continue
		}
		release := false
		if p.ProgramID == l.Program && l.Program != "" {
			for _, receipt := range p.PlaybackReceipts {
				if receipt.TrackID == l.Track && receipt.Terminal {
					release = true
					break
				}
			}
		}
		sk := [2]int64{l.Tenant, l.Session}
		session, exists := sessions[sk]
		if !exists {
			session, e = s.store.GetLiveRuntimeSession(ctx, l.Tenant, l.Session)
			if e == nil {
				sessions[sk] = session
			}
		}
		if e == nil && session.Status != "running" && session.Status != "paused" && (!p.Running || p.ProgramID != l.Program) {
			release = true
		}
		if release {
			if e = s.store.ReleaseVoiceAssetLease(ctx, l); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Server) removeVoiceObject(ctx context.Context, a model.MediaAsset) (uint64, error) {
	// Audio and all registered derived objects share the same ownership checks.
	// A sidecar still referenced by another asset is omitted by the DB transaction.
	keys := []string{a.ObjectKey}
	for _, field := range voiceasset.SidecarFields {
		if key, ok := a.Metadata[field].(string); ok && key != "" {
			keys = append(keys, key)
		}
	}
	seen := map[string]bool{}
	var bytes uint64
	for _, key := range keys {
		if seen[key] {
			continue
		}
		seen[key] = true
		v := a
		v.ObjectKey = key
		if key != a.ObjectKey {
			v.SizeBytes = 0
		}
		n, e := s.removeOneVoiceObject(ctx, v)
		if e != nil {
			return 0, e
		}
		bytes += n
	}
	return bytes, nil
}
func (s *Server) removeOneVoiceObject(ctx context.Context, a model.MediaAsset) (uint64, error) {
	if s.assetStorage == nil {
		return 0, errors.New("storage_unavailable")
	}
	prefix := fmt.Sprintf("tenants/%d/generated_voice/", a.TenantID)
	if !strings.HasPrefix(a.ObjectKey, prefix) || strings.Contains(a.ObjectKey, "..") || strings.Contains(a.ObjectKey, "\\") {
		return 0, errors.New("object_scope_invalid")
	}
	backend, e := s.assetStorage.For(a.StorageDriver)
	if e != nil || backend.Bucket() != a.StorageBucket {
		return 0, errors.New("storage_identity_mismatch")
	}
	exists, ok := backend.(interface {
		Exists(context.Context, string) (bool, error)
	})
	if !ok {
		return 0, errors.New("storage_existence_unverified")
	}
	present, e := exists.Exists(ctx, a.ObjectKey)
	if e != nil {
		return 0, errors.New("object_check_failed")
	}
	if !present {
		return 0, nil
	}
	if e = backend.Delete(ctx, a.ObjectKey); e != nil {
		return 0, errors.New("object_delete_failed")
	}
	present, e = exists.Exists(ctx, a.ObjectKey)
	if e != nil || present {
		return 0, errors.New("object_delete_unconfirmed")
	}
	return a.SizeBytes, nil
}

// Full paths and every existing component are validated before recursive local
// deletion. Lock names remain outside the removed directory when possible.
func validateVoiceSourcePath(root, path string, scope voiceasset.Scope) error {
	abs, e := filepath.Abs(root)
	if e != nil {
		return e
	}
	target, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(abs, target)
	if e != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("local_path_outside_root")
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) < 6 || parts[0] != strconv.FormatInt(scope.Tenant, 10) {
		return errors.New("local_scope_mismatch")
	}
	if parts[4] != "script-first-v1" && parts[4] != "volcengine-longtext-v1" {
		return errors.New("local_namespace_invalid")
	}
	if (len(parts) != 7 && len(parts) != 8) || (parts[5] != "paragraphs" && parts[5] != "plan-paragraphs") || (len(parts) == 8 && parts[6] != "paragraphs") {
		return errors.New("local_layout_invalid")
	}
	// Both plan-shared and old room-specific paragraph namespaces are supported.
	if parts[1] == "plans" {
		if parts[2] != strconv.FormatInt(scope.Plan, 10) || parts[3] != "recording-cache" {
			return errors.New("local_plan_mismatch")
		}
	} else {
		if parts[2] != strconv.FormatInt(scope.Plan, 10) || parts[3] != "recording-cache" {
			return errors.New("local_plan_mismatch")
		}
	}
	if !voiceasset.DigestValid(filepath.Base(target)) {
		return errors.New("local_paragraph_key_invalid")
	}
	current := abs
	if info, e := os.Lstat(abs); e != nil {
		return e
	} else if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("local_root_symlink_rejected")
	}
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("local_symlink_rejected")
		}
	}
	return nil
}
func (s *Server) cleanupVoiceLocalSources(ctx context.Context, j voiceasset.Job) error {
	sources, e := s.store.ClearedVoiceLocalSources(ctx, j)
	if e != nil {
		return e
	}
	for _, source := range sources {
		if e = validateVoiceSourcePath(recordingCacheMediaRoot(), source.Path, source.Stamp.Scope); e != nil {
			return e
		}
		// Paragraph writers use the same durable lock. Never remove an in-flight
		// paid WAV while its supplier receipt is still being drained.
		lockPath, e := volcengineParagraphLockPath(source.Path)
		if e != nil {
			return e
		}
		release, acquired, e := sharedfilelock.TryLock(lockPath)
		if e != nil {
			return e
		}
		if !acquired {
			continue
		}
		e = s.store.DeleteVoiceLocalSource(ctx, j, source, func() error {
			if e := preserveVoiceLocalEvidence(recordingCacheMediaRoot(), source.Path, j, source.Hash); e != nil {
				return e
			}
			return os.RemoveAll(source.Path)
		})
		release()
		if errors.Is(e, voiceasset.ErrBusy) {
			continue
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Server) runVoiceCleanupOnce(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	if e := s.reconcileVoiceAssetLeases(ctx); e != nil {
		return e
	}
	jobs, e := s.store.DueVoiceCleanupJobs(ctx)
	if e != nil {
		return e
	}
	for _, j := range jobs {
		e = s.runVoiceCleanupJob(ctx, j)
		if err := s.store.DeferVoiceCleanupJob(ctx, j, e != nil); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
func (s *Server) runVoiceCleanupJob(ctx context.Context, j voiceasset.Job) error {
	if j.Scope.Category == "interaction" {
		if e := ttsgateway.NewInteractionAudioCache(recordingCacheMediaRoot(), s.executionRealm).WithEpochReader(s.store).ClearScopedFiles(ctx, j); e != nil {
			return e
		}
	}
	if e := s.discoverVoiceLocalSources(ctx, j); e != nil {
		return e
	}
	var e error
	if e = s.store.ScanVoiceCleanupJob(ctx, j); e != nil {
		return e
	}
	items, e := s.store.DueVoiceCleanupItems(ctx, j)
	if e != nil {
		return e
	}
	for _, c := range items {
		work, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = s.store.DeleteVoiceCleanupObject(work, j, c, s.removeVoiceObject)
		cancel()
		if e != nil {
			state, reason := "retry", "storage_or_database_error"
			if errors.Is(e, voiceasset.ErrBusy) {
				state, reason = "waiting_refs", "active_reference"
			}
			if errors.Is(e, voiceasset.ErrConflict) {
				state, reason = "review_required", "scope_changed"
			}
			if e = s.store.SetVoiceCleanupItem(ctx, j, c, state, reason, 0); e != nil {
				return e
			}
		}
	}
	if e = s.cleanupVoiceLocalSources(ctx, j); e != nil {
		return e
	}
	if e = s.store.FinishVoiceCleanupJob(ctx, j); e != nil {
		return e
	}
	return nil
}
func (s *Server) RunVoiceAssetCleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if s.leader == nil || s.leader.IsLeader() {
			work, cancel := context.WithTimeout(ctx, 40*time.Second)
			e := s.runVoiceCleanupOnce(work)
			cancel()
			if e != nil && ctx.Err() == nil {
				log.Printf("voice asset cleanup deferred: state check or bounded storage operation failed")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type voiceCleanupInventory struct {
	Scope    voiceasset.Scope  `json:"scope"`
	Expected voiceasset.Stamp  `json:"expected"`
	At       time.Time         `json:"at"`
	Complete bool              `json:"complete"`
	Counts   map[string]uint64 `json:"counts"`
}

func voiceCleanupInventoryPath(scope voiceasset.Scope) string {
	return filepath.Join(recordingCacheMediaRoot(), "voice-cleanup-inventory", voiceasset.Digest(scope)+".json")
}
func (s *Server) inventoryVoiceObjects(ctx context.Context, scope voiceasset.Scope, objects map[string]model.MediaAsset) (map[string]uint64, string, error) {
	result := map[string]uint64{"confirmed_objects": 0, "confirmed_missing_objects": 0, "confirmed_current_bytes": 0, "inventory_pages": 0}
	if len(objects) == 0 {
		return result, "unknown", nil
	}
	if s.assetStorage == nil {
		return nil, "unknown", errors.New("storage_unavailable")
	}
	groups := map[string][]model.MediaAsset{}
	for _, a := range objects {
		groups[a.StorageDriver+"|"+a.StorageBucket] = append(groups[a.StorageDriver+"|"+a.StorageBucket], a)
	}
	versions := "unknown"
	for _, group := range groups {
		first := group[0]
		backend, e := s.assetStorage.For(first.StorageDriver)
		if e != nil || backend.Bucket() != first.StorageBucket {
			return nil, versions, errors.New("inventory_storage_identity_unknown")
		}
		if oss, ok := backend.(*storage.OSS); ok {
			versions = oss.VoiceBucketVersionState(ctx)
			marker := ""
			wanted := map[string]bool{}
			for _, a := range group {
				wanted[a.ObjectKey] = true
			}
			for {
				page, e := oss.ListGeneratedVoices(ctx, scope.Tenant, marker)
				if e != nil {
					return nil, versions, e
				}
				result["inventory_pages"]++
				for _, object := range page.Objects {
					if wanted[object.Key] {
						result["confirmed_objects"]++
						result["confirmed_current_bytes"] += object.Bytes
						delete(wanted, object.Key)
					}
				}
				if !page.Truncated {
					break
				}
				marker = page.Next
			}
			result["confirmed_missing_objects"] += uint64(len(wanted))
		} else {
			checker, ok := backend.(interface {
				Exists(context.Context, string) (bool, error)
			})
			if !ok {
				return nil, versions, errors.New("inventory_exists_unavailable")
			}
			for _, a := range group {
				exists, e := checker.Exists(ctx, a.ObjectKey)
				if e != nil {
					return nil, versions, e
				}
				if exists {
					result["confirmed_objects"]++
					result["confirmed_current_bytes"] += a.SizeBytes
				} else {
					result["confirmed_missing_objects"]++
				}
			}
		}
	}
	return result, versions, nil
}
