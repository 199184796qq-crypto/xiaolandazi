package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/anchorvoicecache"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechlibrary"
	"livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
)

func (s *Server) canDeleteLiveOpsCustomer(ctx context.Context, actor model.Actor, access model.StaffAccessContext) (bool, error) {
	if actor.IsPlatformAdmin() {
		return true, nil
	}
	return s.store.HasCustomerDeletionGrant(ctx, access.EmployeeID, actor.UserID)
}

func (s *Server) adminCustomerDeletionAccess(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, access, ok := s.adminWorkspaceLiveOpsAccess(w, r)
	if !ok {
		return actor, false
	}
	granted, err := s.canDeleteLiveOpsCustomer(r.Context(), actor, access)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "校验删除用户权限失败")
		return actor, false
	}
	if !granted {
		writeError(w, http.StatusForbidden, "当前账号没有删除用户权限")
		return actor, false
	}
	return actor, true
}

func (s *Server) adminArchiveCustomer(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.adminCustomerDeletionAccess(w, r)
	if !ok {
		return
	}
	userID, ok := adminUserID(w, r)
	if !ok {
		return
	}
	var input struct {
		Username string `json:"username"`
	}
	if err := readJSONWithLimitCompatible(w, r, &input, 4096); err != nil || input.Username == "" {
		writeError(w, http.StatusBadRequest, "请输入用户登录账号确认删除")
		return
	}
	job, err := s.store.QueueCustomerDeletion(r.Context(), userID, actor.UserID, input.Username)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "终端不存在")
	case errors.Is(err, db.ErrSharedCustomerTenant), errors.Is(err, db.ErrCustomerConfirmation):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		log.Printf("archive customer %d: %v", userID, err)
		writeError(w, http.StatusServiceUnavailable, "归档失败，无法启动资产清理")
	default:
		writeJSON(w, http.StatusAccepted, job)
	}
}

func (s *Server) adminCustomerDeletionStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminCustomerDeletionAccess(w, r); !ok {
		return
	}
	userID, ok := adminUserID(w, r)
	if !ok {
		return
	}
	job, err := s.store.GetCustomerDeletion(r.Context(), userID)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "没有删除记录")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取删除进度失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) RunCustomerDeletionCleanup(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if s.leader != nil && !s.leader.IsLeader() {
				continue
			}
			job, err := s.store.ClaimCustomerDeletion(ctx)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				log.Printf("claim customer cleanup: %v", err)
				continue
			}
			work, cancel := context.WithTimeout(ctx, 8*time.Minute)
			err = s.finishCustomerDeletion(work, job)
			cancel()
			if err != nil {
				log.Printf("customer cleanup pending user=%d tenant=%d: %v", job.UserID, job.TenantID, err)
				if err := s.store.RetryCustomerDeletion(ctx, job); err != nil {
					log.Printf("reschedule customer cleanup user=%d: %v", job.UserID, err)
				}
			}
		}
	}
}

func customerAssetPrefixes(tenantID int64, realm string) []string {
	tenant := strconv.FormatInt(tenantID, 10)
	return []string{
		"tenants/" + tenant + "/",
		"custom-mainline-uploads/tenant-" + tenant + "/",
		"live-downloads/tenant-" + tenant + "/",
		"live-recordings/tenant-" + tenant + "/",
		speechAnalysisResultPrefix + "/tenant-" + tenant + "/",
		speechAnalysisTempAudioPrefix + "/tenant-" + tenant + "/",
		"anchor-voice-cache/v1/" + anchorvoicecache.Digest(realm)[:16] + "/t" + tenant + "/",
		"mainline-speech-library/v1/" + speechlibrary.Digest(realm)[:16] + "/t" + tenant + "/",
	}
}

func customerLocalPaths(tenantID int64, plans []int64) []string {
	tenant := strconv.FormatInt(tenantID, 10)
	paths := []string{tenant}
	for _, directory := range []string{"supply-checkpoints", "script-generation-plays", "script-playback-policy", "script-room-playback", "script-generation-locks", "script-pool-policy", "script-rewrite-contexts"} {
		paths = append(paths, filepath.Join(directory, tenant))
	}
	for _, planID := range plans {
		plan := strconv.FormatInt(planID, 10)
		for _, directory := range []string{"script-voices", "script-texts", "script-refresh"} {
			paths = append(paths, filepath.Join(directory, plan))
		}
		paths = append(paths, filepath.Join("script-voice-budget", plan+".json"))
	}
	return paths
}

func (s *Server) finishCustomerDeletion(ctx context.Context, job db.CustomerDeletion) error {
	if job.TenantID <= 0 || job.UserID <= 0 || job.ActorID <= 0 || s.core == nil || s.assetStorage == nil {
		return errors.New("customer cleanup configuration or identity is invalid")
	}
	if err := s.store.VerifyCustomerDeletionOwner(ctx, job); err != nil {
		return err
	}
	if s.executionRealm == "prod" || os.Getenv("XIAOLAN_AGENT_CHAT_SOCKET") != "" {
		if err := purgeCustomerAgentAccount(ctx, job.UserID); err != nil {
			return fmt.Errorf("清理用户智能体数据: %w", err)
		}
	}
	prefixes := customerAssetPrefixes(job.TenantID, s.executionRealm)
	if err := s.checkCustomerMediaScope(ctx, job.TenantID, prefixes); err != nil {
		return err
	}
	plans, rooms, err := s.store.CustomerDeletionIDs(ctx, job.TenantID)
	if err != nil {
		return err
	}
	for _, room := range rooms {
		if err := s.store.QueueRoomDeletion(ctx, job.TenantID, room, job.ActorID); err != nil {
			return err
		}
		if err := s.finishRoomDeletion(ctx, job.TenantID, room); err != nil {
			return err
		}
		if err := s.core.PurgeCustomerRoomRecordings(ctx, job.TenantID, room); err != nil {
			return err
		}
	}

	// Existing direct-upload forms can still write until they expire.
	busy, err := s.store.CustomerDeletionUploadsBusy(ctx, job.TenantID)
	if err != nil {
		return err
	}
	if busy {
		return errors.New("等待录音上传授权过期或处理任务完成")
	}
	voices, err := s.store.CustomerVoiceClones(ctx, job.TenantID)
	if err != nil {
		return err
	}
	gateway := ttsgateway.NewFromEnv()
	deleted := make(map[string]bool)
	var cleanupErr error
	for _, voice := range voices {
		if deleted[voice.VoiceID] {
			continue
		}
		// Volcengine IDs are imported platform allocations, not trained by this
		// application. Their customer library references are deleted in SQL.
		if voice.Provider == ttsgateway.ProviderVolcengine || voice.Model == ttsgateway.VolcengineICL2 {
			continue
		}
		provider := voice.Provider
		if provider == "" {
			provider = ttsgateway.ProviderForModel(voice.Model)
		}
		if err := gateway.DeleteOwnedClone(ctx, provider, voice.Model, voice.VoiceID); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		deleted[voice.VoiceID] = true
	}

	localStore, err := s.assetStorage.For("local")
	if err != nil {
		return errors.Join(cleanupErr, err)
	}
	local, ok := localStore.(*storage.Local)
	if !ok {
		return errors.Join(cleanupErr, errors.New("本地媒体存储配置无效"))
	}
	cleanupErr = errors.Join(cleanupErr, local.DeletePrivatePaths(ctx, append(customerLocalPaths(job.TenantID, plans), prefixes...)))
	cleanupErr = errors.Join(cleanupErr, purgeCustomerAvatars(ctx, s.avatarDir, job.UserID))
	cache := ttsgateway.NewInteractionAudioCache(recordingCacheMediaRoot(), s.executionRealm)
	cleanupErr = errors.Join(cleanupErr, cache.PurgeCustomerFiles(ctx, job.TenantID))
	filePrefix := strconv.FormatInt(job.TenantID, 10) + "-"
	for _, directory := range []string{"playback-ledger", "script-manual-jumps", "volcengine-supply"} {
		cleanupErr = errors.Join(cleanupErr, local.DeletePrivateFilesWithPrefix(ctx, directory, filePrefix))
	}
	if candidate, err := s.assetStorage.For("oss"); err == nil {
		oss, ok := candidate.(*storage.OSS)
		if !ok {
			return errors.Join(cleanupErr, errors.New("OSS存储配置无效"))
		}
		for _, prefix := range prefixes {
			cleanupErr = errors.Join(cleanupErr, oss.DeletePrivatePrefix(ctx, prefix))
		}
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return s.store.CompleteCustomerDeletion(ctx, job, plans)
}

func (s *Server) checkCustomerMediaScope(ctx context.Context, tenantID int64, prefixes []string) error {
	for _, prefix := range prefixes {
		shared, err := s.store.CustomerMediaPrefixShared(ctx, tenantID, prefix)
		if err != nil {
			return err
		}
		if shared {
			return errors.New("媒体目录被其他客户引用，需先核验归属")
		}
	}
	assets, err := s.store.CustomerMediaLocations(ctx, tenantID)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		store, err := s.assetStorage.For(asset.Driver)
		if err != nil || store.Bucket() != asset.Bucket {
			return fmt.Errorf("媒体资产 %d 的存储配置不可用", asset.ID)
		}
		covered := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(asset.Key, prefix) {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("媒体资产 %d 的所有权路径待核验", asset.ID)
		}
	}
	return nil
}
