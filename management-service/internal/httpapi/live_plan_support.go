package httpapi

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) requireCustomerSupportPlan(w http.ResponseWriter, r *http.Request) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	if actor.Role != "customer" || actor.TenantID == nil || *actor.TenantID <= 0 {
		writeError(w, http.StatusForbidden, "只有方案所属客户可以申请或撤销协助")
		return actor, 0, 0, false
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	tenantID := *actor.TenantID
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, db.ErrLiveAgentPlanNotFound) || (err == nil && plan.Status != "active") {
		writeError(w, http.StatusNotFound, "方案不存在或已停用")
		return actor, 0, 0, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案失败")
		return actor, 0, 0, false
	}
	return actor, tenantID, planID, true
}

func writeLivePlanSupportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrLiveSupportRequestAccess), errors.Is(err, db.ErrLiveSupportL3Ineligible):
		writeError(w, http.StatusForbidden, "当前人员没有处理此方案协助的权限")
	case errors.Is(err, db.ErrLivePlanSupportRoomUnavailable):
		writeError(w, http.StatusNotFound, "所选直播间不存在或已删除，请重新选择")
	case errors.Is(err, db.ErrLiveAgentPlanNotFound):
		writeError(w, http.StatusNotFound, "方案不存在或已停用")
	case errors.Is(err, db.ErrLiveSupportRequestState):
		writeError(w, http.StatusConflict, "协助申请状态已变化，请刷新后重试")
	default:
		writeError(w, http.StatusInternalServerError, "处理方案协助失败")
	}
}

func (s *Server) livePlanSupportRequests(w http.ResponseWriter, r *http.Request) {
	_, tenantID, planID, ok := s.requireCustomerSupportPlan(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListLivePlanSupportForPlan(r.Context(), tenantID, planID)
	if err != nil {
		writeLivePlanSupportError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) livePlanCreateSupportRequest(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, planID, ok := s.requireCustomerSupportPlan(w, r)
	if !ok {
		return
	}
	staffID, ok := namedPathID(w, r, "staffUserID", "运维员工")
	if !ok {
		return
	}
	var input struct {
		RoomID int64 `json:"authorized_room_id"`
	}
	// Older customer pages send an empty POST for an edit-only grant.
	if err := readJSON(w, r, &input); (err != nil && !errors.Is(err, io.EOF)) || input.RoomID < 0 {
		writeError(w, http.StatusBadRequest, "授权直播间参数无效")
		return
	}
	item, err := s.store.CreateLivePlanSupport(r.Context(), tenantID, planID, staffID, actor.UserID, input.RoomID)
	if err != nil {
		writeLivePlanSupportError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) livePlanRevokeSupportRequest(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, planID, ok := s.requireCustomerSupportPlan(w, r)
	if !ok {
		return
	}
	requestID, ok := namedPathID(w, r, "requestID", "协助申请")
	if !ok {
		return
	}
	if err := s.store.RevokeLivePlanSupport(r.Context(), tenantID, planID, requestID, actor.UserID); err != nil {
		writeLivePlanSupportError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) liveOpsPlanSupportRequests(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireEligibleLiveSupportStaff(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListLivePlanSupportForStaff(r.Context(), actor.UserID)
	if err != nil {
		writeLivePlanSupportError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveOpsDecidePlanSupport(accept bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.requireEligibleLiveSupportStaff(w, r)
		if !ok {
			return
		}
		requestID, ok := namedPathID(w, r, "requestID", "协助申请")
		if !ok {
			return
		}
		item, err := s.store.DecideLivePlanSupport(r.Context(), requestID, actor.UserID, accept)
		if err != nil {
			writeLivePlanSupportError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}

// A scope marker is only a selector. The accepted customer grant is verified
// for the authenticated employee on every scoped request.
func livePlanSupportScope(r *http.Request) (planID int64, present, valid bool) {
	for _, raw := range append(r.Header.Values("X-Live-Support-Plan-ID"), r.URL.Query()["support_plan_id"]...) {
		present = true
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 || (planID != 0 && planID != id) {
			return 0, true, false
		}
		planID = id
	}
	return planID, present, present
}

func livePlanSupportEndpointAllowed(r *http.Request, planID int64) bool {
	if r.Header.Get("X-Live-Support-Room-ID") != "" || r.URL.Query().Has("support_room_id") {
		return false
	}
	// Current plan workspace calls use room_id=0 for a shared account plan.
	for _, room := range r.URL.Query()["room_id"] {
		if room != "" && room != "0" {
			return false
		}
	}
	if raw := r.PathValue("planID"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id != planID {
			return false
		}
		room := r.PathValue("roomID")
		statsRooms := customerAgentQuestionStatsPrefix + raw + "/rooms"
		if r.Method == http.MethodGet {
			if room == "" && r.URL.Path == statsRooms {
				return true
			}
			if roomID, err := strconv.ParseInt(room, 10, 64); err == nil && roomID > 0 &&
				r.URL.Path == statsRooms+"/"+room+"/sessions" {
				return true
			}
		}
		prefix := "/api/v1/live-agent-plans/" + raw
		suffix, found := strings.CutPrefix(r.URL.Path, prefix)
		if room != "" || !found || (suffix != "" && !strings.HasPrefix(suffix, "/")) {
			return false
		}
		return suffix != "/archive" && !strings.HasPrefix(suffix, "/room-bindings") &&
			!strings.HasSuffix(suffix, "/publish")
	}
	if r.PathValue("roomID") != "" {
		return false
	}
	switch r.URL.Path {
	case customerAgentChatPath, customerAgentChatOpenPath, customerAgentFactEditPath + "/preview", customerAgentFactEditPath + "/confirm", customerFactEditPath + "/confirm":
		// The old unscoped GET reads the account's most recent conversation.
		// Only the scoped open and send requests include an exact plan ID.
		return r.Method == http.MethodPost
	case customerFactEditPath + "/results":
		return r.Method == http.MethodGet
	case customerAgentSkillsPath:
		return r.Method == http.MethodGet || r.Method == http.MethodPost
	}
	if slug := r.PathValue("slug"); slug != "" && r.URL.Path == customerAgentSkillsPath+"/"+slug {
		return r.Method == http.MethodGet || r.Method == http.MethodPatch || r.Method == http.MethodDelete
	}
	// The editor may read its own saved media and referenced private voices.
	// Their handlers apply an additional per-asset check after authorization.
	if r.Method == http.MethodPost {
		for _, resource := range []struct{ prefix, pathID string }{
			{"/api/v1/live/official-voices/", "voiceID"},
			{"/api/v1/live/voice-profiles/", "profileID"},
			{"/api/v1/live/voice-model-bindings/", "bindingID"},
		} {
			if id := r.PathValue(resource.pathID); id != "" && r.URL.Path == resource.prefix+id+"/preview" {
				return true
			}
		}
	}
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/live/voice-profiles", "/api/v1/live/voice-model-bindings":
		return true
	}
	assetID := r.PathValue("assetID")
	return assetID != "" && r.URL.Path == "/api/v1/live/media-assets/"+assetID+"/content"
}

func (s *Server) resolveScopedLivePlanSupport(w http.ResponseWriter, r *http.Request, actor model.Actor, requestedTenantID, planID int64) (int64, bool) {
	if !actor.IsInternalStaff() || !livePlanSupportEndpointAllowed(r, planID) {
		writeError(w, http.StatusForbidden, "协助仅限客户授权的当前方案")
		return 0, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil || !access.CanDelegateLivePolicyL3() {
		writeError(w, http.StatusForbidden, "当前运维账号没有方案协助权限")
		return 0, false
	}
	tenant, err := s.store.AuthorizedLivePlanSupportTenant(r.Context(), planID, actor.UserID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && requestedTenantID > 0 && tenant != requestedTenantID) {
		writeError(w, http.StatusForbidden, "客户未授权此方案，或已撤销授权")
		return 0, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验方案协助授权失败")
		return 0, false
	}
	return tenant, true
}

// Private voice resources are shared across a customer's plans. Give support
// access only to voices already referenced by the authorized plan's versions.
func (s *Server) livePlanSupportVoiceProfiles(r *http.Request, tenantID, planID int64) (map[int64]bool, error) {
	versions, err := s.store.ListLiveAgentPlanVersionsForPlan(r.Context(), tenantID, planID)
	if err != nil {
		return nil, err
	}
	ids := make(map[int64]bool)
	for _, version := range versions {
		if version.VoiceIdentity.ProfileID > 0 {
			ids[version.VoiceIdentity.ProfileID] = true
		}
		for _, voice := range scriptVoicesForPlayback(version) {
			if voice.Identity != nil && voice.Identity.ProfileID > 0 {
				ids[voice.Identity.ProfileID] = true
			}
		}
	}
	return ids, nil
}

func (s *Server) livePlanSupportMediaAllowed(r *http.Request, asset model.MediaAsset, planID int64) bool {
	if supportInt64(asset.Metadata["plan_id"]) == planID {
		return true
	}
	versions, err := s.store.ListLiveAgentPlanVersionsForPlan(r.Context(), asset.TenantID, planID)
	if err != nil {
		return false
	}
	for _, version := range versions {
		for _, variant := range version.Variants {
			if variant.AudioAssetID > 0 && variant.AudioAssetID == asset.ID {
				return true
			}
		}
	}
	return false
}
