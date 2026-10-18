package httpapi

import (
	"errors"
	appdb "livecompanion/management/internal/db"
	"net/http"
	"strings"
)

// liveAgentPlanFactCategoryUpdate 轻量更新单条正式事实的「事实类型」，镜像 scope 端点：
// 只改 category，不触发事实归一化/四层整理。分类须落在当前方案「行业包 + 租户自定义」枚举内。
func (s *Server) liveAgentPlanFactCategoryUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	fact, ok := liveAgentPlanFactPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID          int64  `json:"tenant_id"`
		ExpectedVersionNo int64  `json:"expected_version_no"`
		Category          string `json:"category"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "事实类型格式错误")
		return
	}
	input.Category = strings.TrimSpace(input.Category)
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	// 校验新分类是否在当前方案允许的枚举内（行业包 + 租户自定义）。
	if err := s.validatePlanFactCategory(r.Context(), tenant, plan, input.Category); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	updated, err := s.store.UpdateLiveAgentPlanFactCategory(r.Context(), tenant, plan, fact, actor.UserID, input.ExpectedVersionNo, input.Category)
	switch {
	case errors.Is(err, appdb.ErrLiveFactCategoryInvalid):
		writeError(w, 400, err.Error())
	case errors.Is(err, appdb.ErrLiveAgentPlanFactNotFound), errors.Is(err, appdb.ErrLiveAgentPlanNotFound):
		writeError(w, 404, "事实或方案不存在、已停用或不属于当前方案")
	case errors.Is(err, appdb.ErrLiveAgentPlanFactVersionConflict):
		writeError(w, 409, "事实已被修改，请刷新后重新选择事实类型")
	case err != nil:
		writeError(w, 500, "保存事实类型失败")
	default:
		writeJSON(w, 200, updated)
	}
}
