package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

const maxMediaAssetBytes int64 = 2 << 30

var allowedMediaAssetTypes = map[string]bool{
	"video":         true,
	"audio":         true,
	"voice_sample":  true,
	"tts_generated": true,
	"image":         true,
	"document":      true,
	"other":         true,
}

func (s *Server) liveAgentConfigVersions(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentConfigVersions(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取智能体配置版本失败")
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		filtered := make([]model.AgentConfigVersion, 0, len(items))
		for _, item := range items {
			if item.LifecycleStatus != "active" {
				continue
			}
			filtered = append(filtered, supportConfigProjection(item, roomID))
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) liveCreateAgentConfigDraft(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	var input model.AgentConfigInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "智能体配置格式错误")
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		if len(input.Layer1)+len(input.Layer2)+len(input.Layer3)+len(input.Persona)+len(input.ModelConfig)+len(input.StyleProfile)+len(input.SafetyConfig) != 0 || len(input.SpeechConfig) != 1 {
			writeError(w, http.StatusForbidden, "授权协助只能保存当前直播间的声音设置")
			return
		}
		rooms := asSupportMap(input.SpeechConfig["rooms"])
		if len(rooms) != 1 || asSupportMap(rooms[strconv.FormatInt(roomID, 10)]) == nil {
			writeError(w, http.StatusForbidden, "声音设置超出当前直播间授权范围")
			return
		}
		room := asSupportMap(rooms[strconv.FormatInt(roomID, 10)])
		if len(room) != 1 || asSupportMap(room["selected_voice"]) == nil {
			writeError(w, http.StatusForbidden, "授权声音配置只支持当前直播间的选中声音")
			return
		}
		if !s.requireLiveSupportSelectedVoice(w, r, actor, tenantID, asSupportMap(room["selected_voice"])) {
			return
		}
	}
	item, err := s.store.CreateLiveAgentConfigDraft(r.Context(), tenantID, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存智能体配置草稿失败")
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		if err := s.store.RegisterLiveSupportConfigVersion(r.Context(), item.ID, tenantID, roomID, actor.UserID, model.LiveSupportCapabilityL3Policy); err != nil {
			writeError(w, http.StatusInternalServerError, "登记直播间声音草稿失败")
			return
		}
		item = supportConfigProjection(item, roomID)
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveActivateAgentConfigVersion(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	versionID, ok := namedPathID(w, r, "versionID", "配置版本")
	if !ok {
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		activeID, err := s.store.ActivateLiveSupportSpeechConfig(r.Context(), tenantID, roomID, versionID, actor.UserID)
		if err != nil {
			writeError(w, http.StatusForbidden, "不能发布该声音草稿：授权已撤回或草稿不属于当前直播间")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"active_version_id": activeID})
		return
	}
	if err := s.store.ActivateLiveAgentConfigVersion(r.Context(), tenantID, versionID, actor.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "配置版本不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "切换智能体配置版本失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active_version_id": versionID})
}

func (s *Server) liveListMediaAssets(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	items, err := s.store.ListMediaAssets(r.Context(), tenantID, strings.TrimSpace(r.URL.Query().Get("asset_type")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取媒体资产失败")
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		filtered := make([]model.MediaAsset, 0, len(items))
		for _, item := range items {
			if s.liveSupportMediaAllowed(r.Context(), roomID, item) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) liveGetMediaAsset(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	assetID, ok := namedPathID(w, r, "assetID", "媒体资产")
	if !ok {
		return
	}
	item, err := s.store.GetMediaAsset(r.Context(), tenantID, assetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "媒体资产不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取媒体资产失败")
		return
	}
	if !s.requireLiveSupportMedia(w, r, actor, item) {
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveUploadMediaAsset(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	if s.assetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体存储尚未初始化")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxMediaAssetBytes+4*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "媒体文件过大或上传格式错误")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "请选择要上传的媒体文件")
		return
	}
	defer file.Close()
	if header.Size < 0 || header.Size > maxMediaAssetBytes {
		writeError(w, http.StatusBadRequest, "媒体文件大小不合法")
		return
	}

	assetType := strings.ToLower(strings.TrimSpace(r.FormValue("asset_type")))
	if !allowedMediaAssetTypes[assetType] {
		writeError(w, http.StatusBadRequest, "不支持的媒体资产类型")
		return
	}
	agentID, err := optionalFormInt64(r.FormValue("agent_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "agent_id 格式错误")
		return
	}
	durationMS, err := optionalFormUint64(r.FormValue("duration_ms"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "duration_ms 格式错误")
		return
	}
	metadata := map[string]any{}
	if raw := strings.TrimSpace(r.FormValue("metadata_json")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			writeError(w, http.StatusBadRequest, "metadata_json 格式错误")
			return
		}
	}

	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		metadata["room_id"] = roomID
		metadata["support_room_id"] = roomID
		metadata["staff_user_id"] = actor.UserID
	}
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" || contentType == "application/octet-stream" {
		buffer := make([]byte, 512)
		n, readErr := file.Read(buffer)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			writeError(w, http.StatusBadRequest, "无法读取媒体文件")
			return
		}
		contentType = http.DetectContentType(buffer[:n])
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			writeError(w, http.StatusInternalServerError, "无法重置媒体文件读取位置")
			return
		}
	}

	objectKey, err := newMediaObjectKey(tenantID, assetType, header.Filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成媒体对象编号失败")
		return
	}
	hasher := sha256.New()
	assetStore := s.assetStorage.Current()
	if err := assetStore.Put(r.Context(), objectKey, io.TeeReader(file, hasher), contentType); err != nil {
		writeError(w, http.StatusBadGateway, "保存媒体文件失败")
		return
	}

	item, err := s.store.CreateMediaAsset(r.Context(), model.CreateMediaAssetInput{
		TenantID: tenantID, AgentID: agentID, AssetType: assetType,
		OriginalName:  filepath.Base(header.Filename),
		StorageDriver: assetStore.Driver(), StorageBucket: assetStore.Bucket(),
		ObjectKey: objectKey, MIMEType: contentType, SizeBytes: uint64(header.Size),
		DurationMS: durationMS, ChecksumSHA256: hex.EncodeToString(hasher.Sum(nil)),
		Metadata: metadata, CreatedByUserID: actor.UserID,
	})
	if err != nil {
		_ = assetStore.Delete(r.Context(), objectKey)
		writeError(w, http.StatusInternalServerError, "保存媒体资产记录失败")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"asset":       item,
		"content_url": fmt.Sprintf("/api/v1/live/media-assets/%d/content", item.ID),
	})
}

func (s *Server) liveMediaAssetContent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	assetID, ok := namedPathID(w, r, "assetID", "媒体资产")
	if !ok {
		return
	}
	item, err := s.store.GetMediaAsset(r.Context(), tenantID, assetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "媒体资产不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取媒体资产失败")
		return
	}
	if !s.requireLiveSupportMedia(w, r, actor, item) {
		return
	}
	assetStore, err := s.assetStorage.For(item.StorageDriver)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "媒体存储驱动暂不可用")
		return
	}
	if signedURL, err := assetStore.SignedURL(r.Context(), item.ObjectKey, s.assetURLTTL); err == nil && signedURL != "" {
		http.Redirect(w, r, signedURL, http.StatusTemporaryRedirect)
		return
	}
	reader, err := assetStore.Open(r.Context(), item.ObjectKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "媒体文件不存在")
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", item.MIMEType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", safeHeaderFilename(item.OriginalName)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, reader)
}

func (s *Server) liveDeleteMediaAsset(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	assetID, ok := namedPathID(w, r, "assetID", "媒体资产")
	if !ok {
		return
	}
	item, err := s.store.GetMediaAsset(r.Context(), tenantID, assetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "媒体资产不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取媒体资产失败")
		return
	}
	if !s.requireLiveSupportMedia(w, r, actor, item) {
		return
	}
	if actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "媒体资产可能由多个方案使用，请由客户删除")
		return
	}
	if err := s.store.DeleteMediaAsset(r.Context(), tenantID, assetID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "媒体资产不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除媒体资产失败")
		return
	}
	if s.assetStorage != nil {
		assetStore, storageErr := s.assetStorage.For(item.StorageDriver)
		if storageErr != nil {
			log.Printf("media asset %d cleanup skipped: %v", item.ID, storageErr)
		} else if storageErr = assetStore.Delete(r.Context(), item.ObjectKey); storageErr != nil {
			log.Printf("media asset %d object cleanup failed: %v", item.ID, storageErr)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) liveListVoiceProfiles(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	items, err := s.store.ListVoiceProfiles(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取声音克隆档案失败")
		return
	}
	if actor.IsInternalStaff() {
		roomID, _ := liveSupportScopeRoomID(r)
		filtered := make([]model.VoiceProfile, 0, len(items))
		for _, item := range items {
			if s.liveSupportVoiceAllowed(r.Context(), tenantID, roomID, item) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) liveVoiceProfileQuota(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	limit, err := s.store.GetEffectiveCustomerRoomLimit(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间权限数量失败")
		return
	}
	used, err := s.store.CountActiveVoiceProfiles(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取自定义声音数量失败")
		return
	}
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	writeJSON(w, http.StatusOK, map[string]int64{
		"limit": limit, "used": used, "remaining": remaining,
	})
}

func (s *Server) liveCreateVoiceProfile(w http.ResponseWriter, r *http.Request) {
	s.liveSaveVoiceProfile(w, r, 0)
}

func (s *Server) liveUpdateVoiceProfile(w http.ResponseWriter, r *http.Request) {
	profileID, ok := namedPathID(w, r, "profileID", "声音档案")
	if !ok {
		return
	}
	s.liveSaveVoiceProfile(w, r, profileID)
}

func (s *Server) liveDeleteVoiceProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	profileID, ok := namedPathID(w, r, "profileID", "声音档案")
	if !ok {
		return
	}
	if actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "共享声音库的删除操作须由客户完成")
		return
	}
	if err := s.store.DisableVoiceProfile(r.Context(), tenantID, profileID, actor.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "自定义声音不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除自定义声音失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) liveSaveVoiceProfile(w http.ResponseWriter, r *http.Request, profileID int64) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
	if !ok {
		return
	}
	var input model.VoiceProfileInput
	if actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "共享声音档案的修改须由客户完成；授权协助可创建新声音")
		return
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "声音克隆档案格式错误")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.TrimSpace(input.Provider)
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	input.CloneStatus = strings.TrimSpace(input.CloneStatus)
	if input.Name == "" || input.Provider == "" {
		writeError(w, http.StatusBadRequest, "声音名称和服务商不能为空")
		return
	}
	if profileID == 0 && input.CloneStatus != "disabled" {
		limit, limitErr := s.store.GetEffectiveCustomerRoomLimit(r.Context(), tenantID)
		used, usedErr := s.store.CountActiveVoiceProfiles(r.Context(), tenantID)
		if limitErr != nil || usedErr != nil {
			writeError(w, http.StatusInternalServerError, "读取自定义声音额度失败")
			return
		}
		if used >= limit {
			writeError(w, http.StatusConflict, fmt.Sprintf("自定义声音已达到直播间权限上限（%d 个），请先删除一个已有声音再生成", limit))
			return
		}
	}
	switch input.CloneStatus {
	case "", "pending", "training", "ready", "failed", "disabled":
	default:
		writeError(w, http.StatusBadRequest, "声音克隆状态不支持")
		return
	}
	item, err := s.store.SaveVoiceProfile(r.Context(), tenantID, actor.UserID, profileID, input)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "声音档案或声音样本不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "保存声音克隆档案失败")
		return
	}
	status := http.StatusOK
	if profileID == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, item)
}

func newMediaObjectKey(tenantID int64, assetType, filename string) (string, error) {
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if len(ext) > 16 {
		ext = ""
	}
	now := time.Now().UTC()
	return path.Join(
		"tenants", strconv.FormatInt(tenantID, 10), sanitizeObjectSegment(assetType),
		now.Format("2006"), now.Format("01"), hex.EncodeToString(token[:])+ext,
	), nil
}

func sanitizeObjectSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "other"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	cleaned := strings.Trim(b.String(), "-")
	if cleaned == "" {
		return "other"
	}
	return cleaned
}

func optionalFormInt64(raw string) (*int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return nil, fmt.Errorf("invalid positive integer")
	}
	return &value, nil
}

func optionalFormUint64(raw string) (*uint64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func safeHeaderFilename(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	value = strings.ReplaceAll(value, "\"", "'")
	if value == "" {
		return "media"
	}
	return value
}
