package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Server) liveSupportMediaAllowed(ctx context.Context, roomID int64, asset model.MediaAsset) bool {
	return roomID > 0 && (supportInt64(asset.Metadata["room_id"]) == roomID || supportInt64(asset.Metadata["support_room_id"]) == roomID)
}

func (s *Server) requireLiveSupportMedia(w http.ResponseWriter, r *http.Request, actor model.Actor, asset model.MediaAsset) bool {
	if !actor.IsInternalStaff() {
		return true
	}
	roomID, _ := liveSupportScopeRoomID(r)
	if !s.liveSupportMediaAllowed(r.Context(), roomID, asset) {
		writeError(w, http.StatusForbidden, "该媒体资产不属于当前授权直播间")
		return false
	}
	return true
}

func (s *Server) liveSupportVoiceAllowed(ctx context.Context, tenantID, roomID int64, profile model.VoiceProfile) bool {
	if roomID <= 0 {
		return false
	}
	if supportVoiceProfileRoomID(profile) == roomID {
		return true
	}
	if profile.SampleAssetID != nil {
		asset, err := s.store.GetMediaAsset(ctx, tenantID, *profile.SampleAssetID)
		if err == nil && s.liveSupportMediaAllowed(ctx, roomID, asset) {
			return true
		}
	}
	version, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenantID, roomID)
	if err == nil && version.VoiceIdentity.ProfileID == profile.ID {
		return true
	}
	versions, err := s.store.ListLiveAgentConfigVersions(ctx, tenantID)
	if err != nil {
		return false
	}
	for _, item := range versions {
		if item.LifecycleStatus != "active" {
			continue
		}
		rooms, _ := item.SpeechConfig["rooms"].(map[string]any)
		room, _ := rooms[strconv.FormatInt(roomID, 10)].(map[string]any)
		voice, _ := room["selected_voice"].(map[string]any)
		return supportInt64(voice["profile_id"]) == profile.ID
	}
	return false
}

func (s *Server) requireLiveSupportVoice(w http.ResponseWriter, r *http.Request, actor model.Actor, tenantID int64, profile model.VoiceProfile, write bool) bool {
	if !actor.IsInternalStaff() {
		return true
	}
	roomID, _ := liveSupportScopeRoomID(r)
	allowed := s.liveSupportVoiceAllowed(r.Context(), tenantID, roomID, profile)
	// Existing customer voices can be selected and previewed; destructive
	// library changes are reserved for the customer because voices are shared.
	if write {
		allowed = false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "该声音不在当前直播间授权范围内；共享声音库修改须由客户操作")
		return false
	}
	return true
}

func supportConfigProjection(item model.AgentConfigVersion, roomID int64) model.AgentConfigVersion {
	rooms, _ := item.SpeechConfig["rooms"].(map[string]any)
	key := strconv.FormatInt(roomID, 10)
	item.SpeechConfig = map[string]any{"rooms": map[string]any{key: cloneSupportJSONMap(asSupportMap(rooms[key]))}}
	item.Layer1, item.Layer2, item.Layer3 = nil, nil, nil
	item.Persona, item.ModelConfig, item.StyleProfile, item.SafetyConfig = nil, nil, nil, nil
	return item
}

func asSupportMap(value any) map[string]any { result, _ := value.(map[string]any); return result }

func (s *Server) requireLiveSupportSelectedVoice(w http.ResponseWriter, r *http.Request, actor model.Actor, tenantID int64, voice map[string]any) bool {
	if !actor.IsInternalStaff() {
		return true
	}
	profileID := supportInt64(voice["profile_id"])
	if profileID <= 0 {
		voiceID, _ := voice["voice_id"].(string)
		if _, exists := findOfficialVoice(voiceID); exists {
			return true
		}
		writeError(w, http.StatusForbidden, "请选择当前授权直播间可用的声音")
		return false
	}
	profile, err := s.store.GetVoiceProfile(r.Context(), tenantID, profileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "声音档案不存在")
		return false
	}
	if !s.requireLiveSupportVoice(w, r, actor, tenantID, profile, false) {
		return false
	}
	if bindingID := supportInt64(voice["binding_id"]); bindingID > 0 {
		binding, err := s.store.GetVoiceModelBinding(r.Context(), tenantID, bindingID)
		if err != nil || binding.ProfileID != profile.ID {
			writeError(w, http.StatusForbidden, "声音模型绑定不属于当前授权声音")
			return false
		}
		if voiceID, _ := voice["voice_id"].(string); voiceID != "" && voiceID != binding.VoiceID {
			writeError(w, http.StatusForbidden, "声音标识与授权的声音模型不一致")
			return false
		}
	} else if voiceID, _ := voice["voice_id"].(string); voiceID != "" && voiceID != profile.VoiceID {
		writeError(w, http.StatusForbidden, "声音标识与授权声音档案不一致")
		return false
	}
	return true
}

// A staff support session is scoped to one customer-authorized room. A tenant
// identifier alone must never grant access to all customer strategy content.
func liveSupportScopeRoomID(r *http.Request) (int64, bool) {
	var roomID int64
	for _, raw := range []string{
		r.Header.Get("X-Live-Support-Room-ID"), r.URL.Query().Get("support_room_id"),
		r.PathValue("roomID"), r.URL.Query().Get("room_id"),
	} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || value <= 0 || (roomID > 0 && roomID != value) {
			return 0, false
		}
		roomID = value
	}
	return roomID, roomID > 0
}

// requireLiveStrategyRoomAccess is also used by customer room configuration
// endpoints. Platform administration is not customer consent.
func (s *Server) requireLiveStrategyRoomAccess(w http.ResponseWriter, r *http.Request, actor model.Actor, tenantID, roomID int64) bool {
	if actor.Role == "customer" && actor.TenantID != nil && *actor.TenantID == tenantID {
		deleted, err := s.store.IsRoomDeletionRequested(r.Context(), tenantID, roomID)
		if err != nil || deleted {
			writeError(w, http.StatusNotFound, "直播间已删除或正在清理")
			return false
		}
		return true
	}
	if !actor.IsInternalStaff() || tenantID <= 0 || roomID <= 0 {
		writeError(w, http.StatusForbidden, "没有查看或修改该直播间策略的客户授权")
		return false
	}
	if scope, valid := liveSupportScopeRoomID(r); !valid || scope != roomID {
		writeError(w, http.StatusForbidden, "客户协助必须限定在当前授权直播间")
		return false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil || !access.CanDelegateLivePolicyL3() {
		writeError(w, http.StatusForbidden, "当前运维账号没有直播策略授权协助权限")
		return false
	}
	authorized, err := s.store.HasLiveSupportAuthorization(r.Context(), tenantID, roomID, actor.UserID, model.LiveSupportCapabilityL3Policy)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验客户直播间授权失败")
		return false
	}
	if !authorized {
		writeError(w, http.StatusForbidden, "客户尚未授权此直播间，或授权已经撤回")
		return false
	}
	deleted, err := s.store.IsRoomDeletionRequested(r.Context(), tenantID, roomID)
	if err != nil || deleted {
		writeError(w, http.StatusNotFound, "直播间已删除或正在清理")
		return false
	}
	return true
}

func (s *Server) requireLiveSupportPlanScope(w http.ResponseWriter, r *http.Request, actor model.Actor, tenantID, planID int64, write bool) bool {
	if !actor.IsInternalStaff() {
		return true
	}
	roomID, valid := liveSupportScopeRoomID(r)
	if !valid {
		writeError(w, http.StatusForbidden, "请选择已获得客户授权的直播间")
		return false
	}
	allowed, err := s.store.CanAccessLiveSupportPlan(r.Context(), tenantID, roomID, planID, actor.UserID, write)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验直播智能体方案授权范围失败")
		return false
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "该方案不在当前直播间授权范围内；共享方案须获得全部关联直播间授权后才能修改")
		return false
	}
	return true
}
