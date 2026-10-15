package httpapi

import (
	"context"
	"net/http"
	"strings"

	appdb "livecompanion/management/internal/db"
)

type planIndustryResponse struct {
	BizDomain    string `json:"biz_domain"`
	IndustryCode string `json:"industry_code"`
	IndustryName string `json:"industry_name"`
	Source       string `json:"source,omitempty"`
}

// resolvePlanIndustry 返回方案已选的业务域 + 行业 code；未设置时 biz_domain 默认 ecommerce、code 空。
// 这是「文件即源」动态加载的入口：拿到 code 后 loadIndustryPack 加载四件套。
func (s *Server) resolvePlanIndustry(ctx context.Context, tenant, plan int64) (appdb.PlanIndustry, error) {
	if saved, err := s.store.GetPlanIndustry(ctx, tenant, plan); err == nil {
		return saved, nil
	}
	return appdb.PlanIndustry{BizDomain: "ecommerce", IndustryCode: ""}, nil
}

// loadIndustryPackForPlan 按方案行业加载四件套；行业未设置时返回 nil（调用方回退旧品类维度）。
func (s *Server) loadIndustryPackForPlan(ctx context.Context, tenant, plan int64) (*IndustryPack, error) {
	industry, err := s.resolvePlanIndustry(ctx, tenant, plan)
	if err != nil {
		return nil, err
	}
	if industry.IndustryCode == "" {
		return nil, nil
	}
	return loadIndustryPack(industry.BizDomain, industry.IndustryCode)
}

// listIndustryCatalog 返回某业务域的行业目录（前端「业务模式+行业」下拉框的数据源）。
// 读目录 json（文件即源），返回树形展开后的全部节点（code/name/name_en）。
func (s *Server) listIndustryCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("biz_domain"))
	if domain == "" {
		domain = "ecommerce"
	}
	if !appdbValidIndustryBizDomain(domain) {
		writeError(w, http.StatusBadRequest, "业务域必须是 ecommerce 或 local_service")
		return
	}
	catalog, err := loadIndustryCatalog(domain)
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
	var visit func(n industryCatalogNode)
	visit = func(n industryCatalogNode) {
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

func (s *Server) planIndustryGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	industry, err := s.resolvePlanIndustry(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案行业失败")
		return
	}
	name := ""
	if industry.IndustryCode != "" {
		if pack, e := loadIndustryPack(industry.BizDomain, industry.IndustryCode); e == nil {
			name = pack.IndustryName
		}
	}
	writeJSON(w, http.StatusOK, planIndustryResponse{BizDomain: industry.BizDomain, IndustryCode: industry.IndustryCode, IndustryName: name, Source: industry.Source})
}

func (s *Server) planIndustrySet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	var input struct {
		BizDomain    string `json:"biz_domain"`
		IndustryCode string `json:"industry_code"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "行业设置请求格式错误")
		return
	}
	domain := strings.TrimSpace(input.BizDomain)
	code := strings.TrimSpace(input.IndustryCode)
	if domain == "" {
		domain = "ecommerce"
	}
	if !appdb.ValidIndustryBizDomain(domain) {
		writeError(w, http.StatusBadRequest, "业务域必须是 ecommerce 或 local_service")
		return
	}
	if code != "" {
		if _, err := loadIndustryPack(domain, code); err != nil {
			writeError(w, http.StatusBadRequest, "行业不存在或配置不完整："+err.Error())
			return
		}
	}
	if err := s.store.SetPlanIndustry(r.Context(), tenantID, planID, domain, code, "manual", actor.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "行业保存失败")
		return
	}
	name := ""
	if code != "" {
		if pack, e := loadIndustryPack(domain, code); e == nil {
			name = pack.IndustryName
		}
	}
	writeJSON(w, http.StatusOK, planIndustryResponse{BizDomain: domain, IndustryCode: code, IndustryName: name, Source: "manual"})
}
