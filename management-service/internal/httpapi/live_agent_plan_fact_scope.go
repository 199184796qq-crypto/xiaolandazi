package httpapi

import (
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
)

func (s *Server) liveAgentPlanFactScopeUpdate(w http.ResponseWriter, r *http.Request) {
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
		TenantID          int64 `json:"tenant_id"`
		ExpectedVersionNo int64 `json:"expected_version_no"`
		model.LiveAgentPlanFactScope
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "事实归属格式错误")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	updated, err := s.store.UpdateLiveAgentPlanFactScope(r.Context(), tenant, plan, fact, actor.UserID, input.ExpectedVersionNo, input.LiveAgentPlanFactScope)
	switch {
	case errors.Is(err, appdb.ErrLiveFactScopeInvalid):
		writeError(w, 400, err.Error())
	case errors.Is(err, appdb.ErrLiveFactScopeProductNotFound), errors.Is(err, appdb.ErrLiveAgentPlanFactNotFound), errors.Is(err, appdb.ErrLiveAgentPlanNotFound):
		writeError(w, 404, "事实或归属商品不存在、已停用或不属于当前方案")
	case errors.Is(err, appdb.ErrLiveAgentPlanFactVersionConflict):
		writeError(w, 409, "事实已被修改，请刷新后重新选择归属")
	case err != nil:
		writeError(w, 500, "保存事实归属失败")
	default:
		writeJSON(w, 200, updated)
	}
}

const explicitCommonFactPrefix = "- [用户指定公共；"
const explicitProductFactPrefix = "- [用户指定商品="

// The program-owned prefix stays in the safety snapshot's metadata, outside
// editable claim text. Bind by record ID, then resolve the current link label.
func renderFactScopePrefix(fact model.LiveAgentPlanFact, links []model.LiveAgentPlanProductLink) string {
	if fact.Scope == nil {
		return "- ["
	}
	if fact.Scope.Kind == "common" {
		return explicitCommonFactPrefix
	}
	var link string
	for _, p := range links {
		if p.ID == fact.Scope.ProductLinkID && p.PlanID == fact.PlanID && p.TenantID == fact.TenantID && p.Status == "active" {
			link = p.LinkKey
			break
		}
	}
	encoded, _ := json.Marshal(link)
	return explicitProductFactPrefix + string(encoded) + "；"
}

func explicitPlanFactScope(block string) (anchorcycle.ScopedFact, bool, error) {
	fact := anchorcycle.ScopedFact{Text: block, Applicable: true}
	if strings.HasPrefix(block, explicitCommonFactPrefix) {
		fact.Shared = true
		return fact, true, nil
	}
	if !strings.HasPrefix(block, explicitProductFactPrefix) {
		return fact, false, nil
	}
	raw := strings.TrimPrefix(block, explicitProductFactPrefix)
	decoder := json.NewDecoder(strings.NewReader(raw))
	var link string
	if decoder.Decode(&link) != nil || strings.TrimSpace(link) == "" || !strings.HasPrefix(raw[decoder.InputOffset():], "；") {
		return fact, true, errors.New("用户指定的归属商品已不可用，请在事实依据中重新选择归属范围")
	}
	fact.Links = []string{link}
	return fact, true, nil
}
