package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/db"
)

var materialReviewRevisionPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s *Server) liveAgentPlanEditDraft(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method == http.MethodPut)
	if !ok {
		return
	}
	var draft db.LiveAgentPlanEditDraft
	var err error
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		draft, err = s.store.GetLiveAgentPlanEditDraft(r.Context(), tenantID, planID)
	} else {
		if err := readJSONWithLimit(w, r, &draft, 4<<20); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "方案草稿超过四兆字节，请缩短内容后重试")
			} else {
				writeError(w, http.StatusBadRequest, "方案草稿格式无效，请刷新页面后重新保存")
			}
			return
		}
		if draft.Draft == nil || len(draft.Revision) > 64 ||
			(draft.ExpectedFactsRevision != "" && !materialReviewRevisionPattern.MatchString(draft.ExpectedFactsRevision)) {
			writeError(w, http.StatusBadRequest, "方案草稿格式无效，请刷新页面后重新保存")
			return
		}
		if len(draft.Draft.OpenIssues) > 100 {
			writeError(w, http.StatusBadRequest, "开播检查结果过多")
			return
		}
		for index, issue := range draft.Draft.OpenIssues {
			issue = strings.TrimSpace(issue)
			if issue == "" || utf8.RuneCountInString(issue) > 500 {
				writeError(w, http.StatusBadRequest, "开播检查结果格式无效")
				return
			}
			draft.Draft.OpenIssues[index] = issue
		}
		draft, err = s.store.SaveLiveAgentPlanEditDraft(r.Context(), tenantID, planID, draft)
	}
	switch {
	case errors.Is(err, db.ErrLiveAgentPlanEditConflict), errors.Is(err, db.ErrPlanReviewFactsChanged):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, db.ErrLiveAgentPlanNotFound):
		writeError(w, http.StatusNotFound, "智能体方案不存在")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "方案草稿读写失败，请重试")
	default:
		writeJSON(w, http.StatusOK, draft)
	}
}
