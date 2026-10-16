package httpapi

import (
	"errors"
	"net/http"
	"strings"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// liveAgentPlanProductSellingPointList 返回某商品链接（productLinkID）的卖点池。
// 卖点池已在 product-links 列表接口随商品卡返回，这里提供单商品维度的读取入口。
func (s *Server) liveAgentPlanProductSellingPointList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlanProductSellingPoints(r.Context(), tenantID, planID, productLinkID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取商品卖点池失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// liveAgentPlanProductSellingPointSave 整体替换某商品链接的卖点池。
// 归属由 productLinkID 程序保证，模型不回传链接字段。
func (s *Server) liveAgentPlanProductSellingPointSave(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	var input model.SaveLiveAgentPlanProductSellingPointsInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "商品卖点池格式错误")
		return
	}
	if len(input.SellingPoints) == 0 {
		writeError(w, http.StatusBadRequest, "商品卖点池不能为空")
		return
	}
	if len(input.SellingPoints) > 40 {
		writeError(w, http.StatusBadRequest, "单个商品卖点不能超过40个")
		return
	}
	for _, p := range input.SellingPoints {
		if strings.TrimSpace(p.SellingPoint) == "" {
			writeError(w, http.StatusBadRequest, "卖点名不能为空")
			return
		}
		if len(p.Angles) == 0 {
			writeError(w, http.StatusBadRequest, "卖点「"+strings.TrimSpace(p.SellingPoint)+"」缺少角度")
			return
		}
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	items, err := s.store.SaveLiveAgentPlanProductSellingPoints(r.Context(), tenantID, planID, productLinkID, actor.UserID, input.SellingPoints, "system_agent_edit", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存商品卖点池失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// liveAgentPlanProductLinkFrequencyWeightSave 设置单个商品链接的手动频率系数（0=不干预；50~200=0.5~2.0 系数）。
func (s *Server) liveAgentPlanProductLinkFrequencyWeightSave(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	productLinkID, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID int64 `json:"tenant_id,omitempty"`
		Weight   int   `json:"weight"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "频率系数格式错误")
		return
	}
	if input.Weight != 0 && (input.Weight < 50 || input.Weight > 200) {
		writeError(w, http.StatusBadRequest, "商品频率系数须为 0（不干预）或 50~200（0.5~2.0）")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.SetLiveAgentPlanProductLinkFrequencyWeight(r.Context(), tenantID, planID, productLinkID, actor.UserID, input.Weight)
	if errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound) {
		writeError(w, http.StatusNotFound, "正式商品链接不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "保存频率权重失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}
