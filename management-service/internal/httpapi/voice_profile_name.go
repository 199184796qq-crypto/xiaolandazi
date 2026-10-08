package httpapi

import (
	"database/sql"
	"errors"
	appdb "livecompanion/management/internal/db"
	"net/http"
)

func (s *Server) liveRenameVoiceProfile(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "共享声音档案的改名须由客户完成")
		return
	}
	id, ok := namedPathID(w, r, "profileID", "声音档案")
	if !ok {
		return
	}
	var input struct {
		Name         string  `json:"name"`
		ExpectedName *string `json:"expected_name"`
	}
	if err := readJSON(w, r, &input); err != nil || input.ExpectedName == nil {
		writeError(w, 400, "请提供声音名称及当前名称")
		return
	}
	if err := appdb.ValidateVoiceProfileName(input.Name); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	profile, err := s.store.RenameVoiceProfile(r.Context(), tenant, id, actor.UserID, input.Name, *input.ExpectedName)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, 404, "声音档案不存在或已删除")
		return
	}
	if errors.Is(err, appdb.ErrVoiceProfileNameConflict) {
		writeError(w, 409, "声音名称已被其他操作修改，请刷新后重试")
		return
	}
	if err != nil {
		writeError(w, 500, "声音名称没有保存，请重试")
		return
	}
	writeJSON(w, 200, profile)
}
