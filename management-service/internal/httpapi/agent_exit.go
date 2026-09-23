package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	storedb "livecompanion/management/internal/db"
)

func agentExitOrganizationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	organizationID, err := strconv.ParseInt(
		strings.TrimSpace(r.PathValue("organizationID")),
		10,
		64,
	)
	if err != nil || organizationID <= 0 {
		writeError(w, http.StatusBadRequest, "代理组织 ID 无效")
		return 0, false
	}
	return organizationID, true
}

func (s *Server) adminAgentExitCheck(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "agent.view_all"); !ok {
		return
	}

	organizationID, ok := agentExitOrganizationID(w, r)
	if !ok {
		return
	}

	result, err := s.store.GetAgentExitCheck(r.Context(), organizationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "代理组织不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取代理退出清算状态失败")
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) adminAgentExitHistory(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "agent.view_all"); !ok {
		return
	}

	organizationID, ok := agentExitOrganizationID(w, r)
	if !ok {
		return
	}

	items, err := s.store.ListAgentExitHistory(r.Context(), organizationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取代理退出历史失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
func (s *Server) adminFinalizeAgentExit(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "agent.exit.finalize")
	if !ok {
		return
	}

	organizationID, ok := agentExitOrganizationID(w, r)
	if !ok {
		return
	}

	result, err := s.store.FinalizeAgentExit(
		r.Context(),
		organizationID,
		actor.UserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, storedb.ErrAgentExitBlocked):
			writeJSON(w, http.StatusConflict, map[string]any{
				"error": "代理仍有未完成清算事项，暂不能退出",
				"check": result,
			})
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "代理组织不存在")
		case strings.Contains(err.Error(), "agent is not active"):
			writeError(w, http.StatusConflict, "代理组织已不是正常经营状态")
		default:
			writeError(w, http.StatusInternalServerError, "完成代理退出失败")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"check": result,
	})
}
