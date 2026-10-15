package httpapi

import (
	"context"
	"net/http"
	"strings"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// 行业选择的持久化由方案业务模式存储承载（live_plan_biz_domain + live_plan_business_industry，
// 接口 /biz-domain 与 /business-industry）。本文件只提供两件事：
//  1. GET /api/v1/industries —— 行业目录接口（前端「业务模式+行业」下拉框数据源，读目录 json）
//  2. loadIndustryPackForPlan —— 按方案已选业务域+行业加载四件套（动态加载入口）

// listIndustryCatalog 返回某业务域的行业目录（前端下拉框数据源）。
// 读目录 json（文件即源），返回树形展开后的全部节点（code/name/name_en）。
func (s *Server) listIndustryCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("biz_domain"))
	if domain == "" {
		domain = "ecommerce"
	}
	if !appdb.ValidIndustryBizDomain(domain) {
		writeError(w, http.StatusBadRequest, "业务域必须是 ecommerce 或 local_service")
		return
	}
	catalog, err := model.LoadIndustryCatalog(domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type industryItem struct {
		Code   string `json:"code"`
		Name   string `json:"name"`
		NameEN string `json:"name_en,omitempty"`
	}
	items := []industryItem{}
	var visit func(n model.IndustryCatalogNode)
	visit = func(n model.IndustryCatalogNode) {
		items = append(items, industryItem{Code: n.Code, Name: n.Name, NameEN: n.NameEN})
		for _, c := range n.Children {
			visit(c)
		}
	}
	for _, n := range catalog.Nodes {
		visit(n)
	}
	writeJSON(w, http.StatusOK, map[string]any{"biz_domain": domain, "name": catalog.Name, "name_en": catalog.NameEN, "industries": items})
}

// loadIndustryPackForPlan 按方案已选业务域+行业加载四件套（文件即源）。
// 未选业务模式或选「综合/其他」时返回 nil（调用方回退旧品类维度/默认侧面）。
func (s *Server) loadIndustryPackForPlan(ctx context.Context, tenant, plan int64) (*IndustryPack, error) {
	settings, err := s.store.GetPlanBusinessSettings(ctx, tenant, plan)
	if err != nil {
		return nil, nil // 未设置业务模式，回退
	}
	industry := strings.TrimSpace(settings.Industries[settings.BizDomain])
	if industry == "" || industry == "general" {
		return nil, nil // 综合/其他：无专属行业包，回退
	}
	return loadIndustryPack(settings.BizDomain, industry)
}
