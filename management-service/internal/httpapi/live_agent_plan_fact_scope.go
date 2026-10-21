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
// 多链接归属渲染成数组（"1号链接","2号链接"），单链接保持旧的字符串形式，
// 这样历史快照仍然可解析。
func renderFactScopePrefix(fact model.LiveAgentPlanFact, links []model.LiveAgentPlanProductLink) string {
	if fact.Scope == nil {
		return "- ["
	}
	if fact.Scope.Kind == "common" {
		return explicitCommonFactPrefix
	}
	labels := make([]string, 0, 2)
	for _, linkID := range fact.Scope.ProductScopeLinkIDs() {
		for _, p := range links {
			if p.ID == linkID && p.PlanID == fact.PlanID && p.TenantID == fact.TenantID && p.Status == "active" {
				labels = append(labels, p.LinkKey)
				break
			}
		}
	}
	if len(labels) > 1 {
		encoded, _ := json.Marshal(labels)
		return explicitProductFactPrefix + string(encoded) + "；"
	}
	label := ""
	if len(labels) == 1 {
		label = labels[0]
	}
	encoded, _ := json.Marshal(label)
	return explicitProductFactPrefix + string(encoded) + "；"
}

// decodeScopeLinks 解析「用户指定商品=」后面的 JSON 值，兼容历史单字符串与新多链接数组。
func decodeScopeLinks(raw string) ([]string, string, bool) {
	if links, rest, ok := cutJSONValue[[]string](raw); ok {
		trimmed := make([]string, 0, len(links))
		for _, link := range links {
			if link = strings.TrimSpace(link); link != "" {
				trimmed = append(trimmed, link)
			}
		}
		if len(trimmed) > 0 {
			return trimmed, rest, true
		}
		return nil, rest, false
	}
	if link, rest, ok := cutJSONValue[string](raw); ok && strings.TrimSpace(link) != "" {
		return []string{strings.TrimSpace(link)}, rest, true
	}
	return nil, raw, false
}

// cutJSONValue 解码 raw 开头的一个 JSON 值，返回剩余内容和是否成功。
func cutJSONValue[T any](raw string) (T, string, bool) {
	var value T
	decoder := json.NewDecoder(strings.NewReader(raw))
	if decoder.Decode(&value) != nil {
		return value, raw, false
	}
	return value, raw[decoder.InputOffset():], true
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
	links, rest, ok := decodeScopeLinks(raw)
	if !ok || !strings.HasPrefix(rest, "；") {
		return fact, true, errors.New("用户指定的归属商品已不可用，请在事实依据中重新选择归属范围")
	}
	fact.Links = links
	return fact, true, nil
}
