package httpapi

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) requireCustomerOwnedSupportRoom(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端客户可以管理运维协助授权")
		return model.Actor{}, 0, 0, false
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	tenantID := *actor.TenantID
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
		return model.Actor{}, 0, 0, false
	}
	return actor, tenantID, roomID, true
}

func liveSupportPermissionForCapability(capability string) string {
	switch capability {
	case model.LiveSupportCapabilityL3Policy:
		return "livepolicy.manage_l3_authorized"
	case model.LiveSupportCapabilityAnchorTraining:
		return "livecoach.anchor_authorized"
	case model.LiveSupportCapabilityVoiceClone:
		return "livevoice.clone_authorized"
	default:
		return ""
	}
}

func (s *Server) requireLiveSupportRoomCapability(
	w http.ResponseWriter,
	r *http.Request,
	capability string,
) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅营销运维员工可使用客户协助功能")
		return model.Actor{}, 0, 0, false
	}
	permission := liveSupportPermissionForCapability(capability)
	if permission == "" {
		writeError(w, http.StatusBadRequest, "不支持的客户协助能力")
		return model.Actor{}, 0, 0, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil || !access.CanUseLiveSupportCapability(capability) {
		writeError(w, http.StatusForbidden, "当前岗位没有该客户协助能力")
		return model.Actor{}, 0, 0, false
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	tenantID, err := s.store.GetLiveSupportAuthorizedTenant(
		r.Context(),
		roomID,
		actor.UserID,
		capability,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusForbidden, "客户尚未授权你执行该项协助")
			return model.Actor{}, 0, 0, false
		}
		writeError(w, http.StatusInternalServerError, "校验客户授权失败")
		return model.Actor{}, 0, 0, false
	}
	if _, err := s.getCoreRoomState(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "授权直播间不存在或已失效")
		return model.Actor{}, 0, 0, false
	}
	return actor, tenantID, roomID, true
}

func (s *Server) liveSupportStaff(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "仅终端客户可以选择运维协助人员")
		return
	}
	items, err := s.store.ListLiveSupportStaff(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取营销运维人员失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveRoomSupportAuthorizations(w http.ResponseWriter, r *http.Request) {
	_, tenantID, roomID, ok := s.requireCustomerOwnedSupportRoom(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListLiveSupportAuthorizationsForRoom(
		r.Context(),
		tenantID,
		roomID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取运维协助授权失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveRoomUpdateSupportAuthorizations(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerOwnedSupportRoom(w, r)
	if !ok {
		return
	}
	staffUserID, ok := namedPathID(w, r, "staffUserID", "运维员工")
	if !ok {
		return
	}
	var input model.LiveSupportAuthorizationInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "授权内容格式错误")
		return
	}
	if len(input.Capabilities) > 0 {
		writeError(w, http.StatusConflict, "新增或扩大协助权限请通过协助申请，由对应运维人员接受后生效")
		return
	}

	allowed := map[string]bool{
		model.LiveSupportCapabilityL3Policy:       true,
		model.LiveSupportCapabilityAnchorTraining: true,
		model.LiveSupportCapabilityVoiceClone:     true,
	}
	seen := make(map[string]struct{})
	capabilities := make([]string, 0, len(input.Capabilities))
	for _, raw := range input.Capabilities {
		value := strings.TrimSpace(raw)
		if !allowed[value] {
			writeError(w, http.StatusBadRequest, "授权能力不支持")
			return
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		capabilities = append(capabilities, value)
	}
	sort.Strings(capabilities)

	items, err := s.store.SetLiveSupportAuthorizations(
		r.Context(),
		tenantID,
		roomID,
		staffUserID,
		actor.UserID,
		capabilities,
	)
	if err != nil {
		if errors.Is(err, db.ErrLiveSupportL3Ineligible) {
			writeError(w, http.StatusForbidden, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "保存运维协助授权失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func cloneSupportJSONMap(source map[string]any) map[string]any {
	if source == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(source)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil || result == nil {
		return map[string]any{}
	}
	return result
}

func appendSupportRoomEntry(
	source map[string]any,
	roomID int64,
	key string,
	entry map[string]any,
) map[string]any {
	root := cloneSupportJSONMap(source)
	rooms, _ := root["rooms"].(map[string]any)
	if rooms == nil {
		rooms = map[string]any{}
	}
	roomKey := strconv.FormatInt(roomID, 10)
	roomConfig, _ := rooms[roomKey].(map[string]any)
	if roomConfig == nil {
		roomConfig = map[string]any{}
	}
	entries, _ := roomConfig[key].([]any)
	entries = append(entries, entry)
	roomConfig[key] = entries
	rooms[roomKey] = roomConfig
	root["rooms"] = rooms
	return root
}

func supportInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		result, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return result
	default:
		return 0
	}
}

func supportAssetMatches(
	asset model.MediaAsset,
	roomID int64,
	capability string,
) bool {
	if supportInt64(asset.Metadata["room_id"]) != roomID {
		return false
	}
	value, _ := asset.Metadata["support_capability"].(string)
	return strings.TrimSpace(value) == capability
}

func supportVoiceProfileRoomID(profile model.VoiceProfile) int64 {
	if profile.Config == nil {
		return 0
	}
	return supportInt64(profile.Config["support_room_id"])
}

func (s *Server) liveOpsSupportAnchorTraining(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireLiveSupportRoomCapability(
		w, r, model.LiveSupportCapabilityAnchorTraining,
	)
	if !ok {
		return
	}
	var input model.LiveSupportTrainingInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "主播训练内容格式错误")
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" && len(input.AssetIDs) == 0 {
		writeError(w, http.StatusBadRequest, "请提供训练文案或训练素材")
		return
	}
	for _, assetID := range input.AssetIDs {
		if assetID <= 0 {
			writeError(w, http.StatusBadRequest, "训练素材编号无效")
			return
		}
		asset, err := s.store.GetMediaAsset(r.Context(), tenantID, assetID)
		if err != nil ||
			!supportAssetMatches(asset, roomID, model.LiveSupportCapabilityAnchorTraining) {
			writeError(w, http.StatusBadRequest, "训练素材必须是本次授权直播间上传的素材")
			return
		}
	}

	versions, err := s.store.ListLiveAgentConfigVersions(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取客户主播训练配置失败")
		return
	}
	styleProfile := map[string]any{}
	for index := range versions {
		version := versions[index]
		if version.LifecycleStatus == "draft" {
			owned, checkErr := s.store.IsLiveSupportConfigVersion(
				r.Context(),
				version.ID,
				tenantID,
				roomID,
				actor.UserID,
				model.LiveSupportCapabilityAnchorTraining,
			)
			if checkErr != nil {
				writeError(w, http.StatusInternalServerError, "校验现有客户配置草稿失败")
				return
			}
			if !owned {
				writeError(w, http.StatusConflict, "客户当前存在其他未发布配置草稿，请先由客户处理后再继续主播训练")
				return
			}
			styleProfile = version.StyleProfile
			break
		}
		if version.LifecycleStatus == "active" && len(styleProfile) == 0 {
			styleProfile = version.StyleProfile
		}
	}
	entry := map[string]any{
		"text":          input.Text,
		"asset_ids":     input.AssetIDs,
		"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
		"source":        "authorized_live_operations",
		"staff_user_id": actor.UserID,
	}
	styleProfile = appendSupportRoomEntry(
		styleProfile,
		roomID,
		"training_entries",
		entry,
	)
	draft, err := s.store.CreateLiveAgentConfigDraft(
		r.Context(),
		tenantID,
		actor.UserID,
		model.AgentConfigInput{StyleProfile: styleProfile},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存主播训练草稿失败")
		return
	}
	if err := s.store.RegisterLiveSupportConfigVersion(
		r.Context(),
		draft.ID,
		tenantID,
		roomID,
		actor.UserID,
		model.LiveSupportCapabilityAnchorTraining,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "登记主播训练草稿失败")
		return
	}
	_ = s.store.RecordLiveSupportEvent(
		r.Context(),
		"anchor_training.draft",
		tenantID,
		roomID,
		actor.UserID,
		actor.UserID,
		model.LiveSupportCapabilityAnchorTraining,
		map[string]any{"version_id": draft.ID, "version_no": draft.VersionNo},
	)
	writeJSON(w, http.StatusCreated, map[string]any{
		"version_id": draft.ID,
		"version_no": draft.VersionNo,
		"status":     draft.LifecycleStatus,
	})
}

func (s *Server) liveOpsSupportActivateConfigVersion(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireLiveSupportRoomCapability(
		w, r, model.LiveSupportCapabilityAnchorTraining,
	)
	if !ok {
		return
	}
	versionID, ok := namedPathID(w, r, "versionID", "训练配置版本")
	if !ok {
		return
	}
	registered, err := s.store.IsLiveSupportConfigVersion(
		r.Context(),
		versionID,
		tenantID,
		roomID,
		actor.UserID,
		model.LiveSupportCapabilityAnchorTraining,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验训练配置版本失败")
		return
	}
	if !registered {
		writeError(w, http.StatusForbidden, "该配置版本不是你为此直播间创建的授权训练草稿")
		return
	}
	version, err := s.store.GetLiveAgentConfigVersion(r.Context(), tenantID, versionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "主播训练草稿不存在")
		return
	}
	if version.LifecycleStatus != "draft" {
		writeError(w, http.StatusConflict, "该主播训练草稿已失效或客户已生成更新草稿，请重新生成")
		return
	}
	if err := s.store.ActivateLiveAgentConfigVersion(
		r.Context(), tenantID, versionID, actor.UserID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "发布主播训练配置失败")
		return
	}
	_ = s.store.RecordLiveSupportEvent(
		r.Context(),
		"anchor_training.publish",
		tenantID,
		roomID,
		actor.UserID,
		actor.UserID,
		model.LiveSupportCapabilityAnchorTraining,
		map[string]any{"version_id": versionID},
	)
	writeJSON(w, http.StatusOK, map[string]any{"status": "active", "version_id": versionID})
}

func (s *Server) liveOpsSupportMediaUpload(w http.ResponseWriter, r *http.Request) {
	if s.assetStorage == nil {
		writeError(w, http.StatusServiceUnavailable, "媒体存储尚未初始化")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaAssetBytes+4*1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "媒体文件过大或上传格式错误")
		return
	}
	assetType := strings.ToLower(strings.TrimSpace(r.FormValue("asset_type")))
	capability := ""
	switch assetType {
	case "document", "audio":
		capability = model.LiveSupportCapabilityAnchorTraining
	case "voice_sample":
		capability = model.LiveSupportCapabilityVoiceClone
	default:
		writeError(w, http.StatusBadRequest, "客户协助仅支持训练文案、训练录音和声音复刻样本")
		return
	}
	actor, tenantID, roomID, ok := s.requireLiveSupportRoomCapability(w, r, capability)
	if !ok {
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
	metadata := map[string]any{
		"room_id":            roomID,
		"source":             "authorized_live_operations",
		"staff_user_id":      actor.UserID,
		"support_capability": capability,
	}
	item, err := s.store.CreateMediaAsset(r.Context(), model.CreateMediaAssetInput{
		TenantID:        tenantID,
		AssetType:       assetType,
		OriginalName:    filepath.Base(header.Filename),
		StorageDriver:   assetStore.Driver(),
		StorageBucket:   assetStore.Bucket(),
		ObjectKey:       objectKey,
		MIMEType:        contentType,
		SizeBytes:       uint64(header.Size),
		ChecksumSHA256:  hex.EncodeToString(hasher.Sum(nil)),
		Metadata:        metadata,
		CreatedByUserID: actor.UserID,
	})
	if err != nil {
		_ = assetStore.Delete(r.Context(), objectKey)
		writeError(w, http.StatusInternalServerError, "保存媒体资产记录失败")
		return
	}
	_ = s.store.RecordLiveSupportEvent(
		r.Context(),
		"media.upload",
		tenantID,
		roomID,
		actor.UserID,
		actor.UserID,
		capability,
		map[string]any{"asset_id": item.ID, "asset_type": assetType},
	)
	writeJSON(w, http.StatusCreated, map[string]any{"asset": item})
}

func validateSupportVoiceInput(input *model.VoiceProfileInput) bool {
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.TrimSpace(input.Provider)
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	input.CloneStatus = strings.TrimSpace(input.CloneStatus)
	if input.Name == "" || input.Provider == "" {
		return false
	}
	switch input.CloneStatus {
	case "", "pending", "training", "ready", "failed", "disabled":
		return true
	default:
		return false
	}
}

func (s *Server) liveOpsSupportVoiceProfiles(w http.ResponseWriter, r *http.Request) {
	_, tenantID, roomID, ok := s.requireLiveSupportRoomCapability(
		w, r, model.LiveSupportCapabilityVoiceClone,
	)
	if !ok {
		return
	}
	items, err := s.store.ListVoiceProfiles(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取客户声音复刻档案失败")
		return
	}
	filtered := make([]model.VoiceProfile, 0)
	for _, item := range items {
		if supportVoiceProfileRoomID(item) == roomID {
			filtered = append(filtered, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered})
}

func (s *Server) liveOpsSupportCreateVoiceProfile(w http.ResponseWriter, r *http.Request) {
	s.liveOpsSupportSaveVoiceProfile(w, r, 0)
}

func (s *Server) liveOpsSupportUpdateVoiceProfile(w http.ResponseWriter, r *http.Request) {
	profileID, ok := namedPathID(w, r, "profileID", "声音档案")
	if !ok {
		return
	}
	s.liveOpsSupportSaveVoiceProfile(w, r, profileID)
}

func (s *Server) liveOpsSupportSaveVoiceProfile(
	w http.ResponseWriter,
	r *http.Request,
	profileID int64,
) {
	actor, tenantID, roomID, ok := s.requireLiveSupportRoomCapability(
		w, r, model.LiveSupportCapabilityVoiceClone,
	)
	if !ok {
		return
	}
	var input model.VoiceProfileInput
	if err := readJSON(w, r, &input); err != nil || !validateSupportVoiceInput(&input) {
		writeError(w, http.StatusBadRequest, "声音复刻档案内容无效")
		return
	}
	if profileID == 0 && input.SampleAssetID == nil {
		writeError(w, http.StatusBadRequest, "新建声音复刻档案必须上传当前直播间的声音样本")
		return
	}
	if input.SampleAssetID != nil {
		asset, err := s.store.GetMediaAsset(r.Context(), tenantID, *input.SampleAssetID)
		if err != nil ||
			!supportAssetMatches(asset, roomID, model.LiveSupportCapabilityVoiceClone) {
			writeError(w, http.StatusBadRequest, "声音样本必须是本次授权直播间上传的复刻样本")
			return
		}
	}
	if profileID > 0 {
		existing, err := s.store.GetVoiceProfile(r.Context(), tenantID, profileID)
		if err != nil || supportVoiceProfileRoomID(existing) != roomID {
			writeError(w, http.StatusForbidden, "当前授权不能修改其他直播间的声音档案")
			return
		}
	}
	if input.Config == nil {
		input.Config = map[string]any{}
	}
	input.Config["support_room_id"] = roomID
	input.Config["source"] = "authorized_live_operations"
	input.Config["staff_user_id"] = actor.UserID
	item, err := s.store.SaveVoiceProfile(
		r.Context(), tenantID, actor.UserID, profileID, input,
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "保存声音复刻档案失败")
		return
	}
	action := "voice_clone.create"
	if profileID > 0 {
		action = "voice_clone.update"
	}
	_ = s.store.RecordLiveSupportEvent(
		r.Context(),
		action,
		tenantID,
		roomID,
		actor.UserID,
		actor.UserID,
		model.LiveSupportCapabilityVoiceClone,
		map[string]any{"voice_profile_id": item.ID, "sample_asset_id": input.SampleAssetID},
	)
	status := http.StatusOK
	if profileID == 0 {
		status = http.StatusCreated
	}
	writeJSON(w, status, item)
}

func (s *Server) liveOpsSupportAuthorizations(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅内部运维员工可查看客户授权")
		return
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, http.StatusForbidden, "读取员工权限失败")
		return
	}
	hasSupportPermission :=
		access.CanDelegateLivePolicyL3() ||
			staffHasPermission(access, "livecoach.anchor_authorized") ||
			staffHasPermission(access, "livevoice.clone_authorized")
	if !hasSupportPermission {
		writeError(w, http.StatusForbidden, "当前岗位没有客户协助权限")
		return
	}

	items, err := s.store.ListLiveSupportAuthorizationsForStaff(r.Context(), actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取客户授权失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
