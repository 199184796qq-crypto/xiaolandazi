package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) liveRoomSupportRequests(w http.ResponseWriter, r *http.Request) {
	_, tenantID, roomID, ok := s.requireCustomerOwnedSupportRoom(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListLiveSupportRequestsForRoom(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取协助申请失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveRoomCreateSupportRequest(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerOwnedSupportRoom(w, r)
	if !ok {
		return
	}
	staffUserID, ok := namedPathID(w, r, "staffUserID", "运维员工")
	if !ok {
		return
	}
	var input model.LiveSupportRequestInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "协助申请格式错误")
		return
	}
	item, err := s.store.CreateLiveSupportRequest(
		r.Context(),
		tenantID,
		roomID,
		staffUserID,
		actor.UserID,
		input.Capabilities,
	)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrLiveSupportRequestEmpty):
			writeError(w, http.StatusBadRequest, "请至少选择一项协助权限")
		case errors.Is(err, db.ErrLiveSupportRequestAccess),
			errors.Is(err, db.ErrLiveSupportL3Ineligible):
			writeError(w, http.StatusForbidden, "所选协助员不具备申请中的全部协助能力")
		default:
			writeError(w, http.StatusInternalServerError, "提交协助申请失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) requireEligibleLiveSupportStaff(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅营销运维员工可以处理协助申请")
		return model.Actor{}, false
	}
	eligible, err := s.store.IsEligibleLiveSupportStaff(r.Context(), actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验运维人员身份失败")
		return model.Actor{}, false
	}
	if !eligible {
		writeError(w, http.StatusForbidden, "当前员工不属于可协助的营销运维人员")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) liveOpsSupportRequests(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireEligibleLiveSupportStaff(w, r)
	if !ok {
		return
	}
	items, err := s.store.ListLiveSupportRequestsForStaff(r.Context(), actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取协助申请失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveOpsAcceptSupportRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireEligibleLiveSupportStaff(w, r)
	if !ok {
		return
	}
	requestID, ok := namedPathID(w, r, "requestID", "协助申请")
	if !ok {
		return
	}
	var input model.LiveSupportRequestDecisionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "处理意见格式错误")
		return
	}
	item, err := s.store.AcceptLiveSupportRequest(r.Context(), requestID, actor.UserID, input.Note)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrLiveSupportRequestAccess):
			writeError(w, http.StatusForbidden, "这条协助申请不是发给当前员工的")
		case errors.Is(err, db.ErrLiveSupportRequestState):
			writeError(w, http.StatusConflict, "这条协助申请已经处理")
		case errors.Is(err, db.ErrLiveSupportL3Ineligible):
			writeError(w, http.StatusForbidden, "当前员工已不具备申请中的协助能力")
		default:
			writeError(w, http.StatusInternalServerError, "接受协助申请失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) liveOpsRejectSupportRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireEligibleLiveSupportStaff(w, r)
	if !ok {
		return
	}
	requestID, ok := namedPathID(w, r, "requestID", "协助申请")
	if !ok {
		return
	}
	var input model.LiveSupportRequestDecisionInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "处理意见格式错误")
		return
	}
	item, err := s.store.RejectLiveSupportRequest(r.Context(), requestID, actor.UserID, input.Note)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrLiveSupportRequestAccess):
			writeError(w, http.StatusForbidden, "这条协助申请不是发给当前员工的")
		case errors.Is(err, db.ErrLiveSupportRequestState):
			writeError(w, http.StatusConflict, "这条协助申请已经处理")
		default:
			writeError(w, http.StatusInternalServerError, "拒绝协助申请失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}
