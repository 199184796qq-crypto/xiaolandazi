package httpapi

import (
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

// industryConfigPackInput 是创建/更新行业配置包的请求体。
// SellingDimensionsMD 与 P0ExtraMD 是 Markdown（给模型），SidesJSON 是结构化配置（给程序）。
type industryConfigPackInput struct {
	BizDomain           string `json:"biz_domain"`
	IndustryCode        string `json:"industry_code"`
	IndustryName        string `json:"industry_name"`
	SellingDimensionsMD string `json:"selling_dimensions_md"`
	SidesJSON           string `json:"sides_json"`
	P0ExtraMD           string `json:"p0_extra_md,omitempty"`
	Version             string `json:"version,omitempty"`
	Status              string `json:"status,omitempty"`
}

// listIndustryConfigPacks 列出某业务域下的全部行业配置包（管理界面用）。
func (s *Server) listIndustryConfigPacks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("biz_domain"))
	if domain == "" {
		domain = "local_service"
	}
	if !appdbValidIndustryBizDomain(domain) {
		writeError(w, http.StatusBadRequest, "业务域须为 ecommerce 或 local_service")
		return
	}
	packs, err := s.store.ListIndustryConfigPacks(r.Context(), domain)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取行业配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": packs})
}

// getIndustryConfigPack 取单个行业配置包，供方案选择行业后加载。
func (s *Server) getIndustryConfigPack(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	domain := strings.TrimSpace(r.URL.Query().Get("biz_domain"))
	if domain == "" {
		domain = "local_service"
	}
	code := strings.TrimSpace(r.PathValue("industryCode"))
	if code == "" || !appdbValidIndustryBizDomain(domain) {
		writeError(w, http.StatusBadRequest, "参数不合法")
		return
	}
	pack, err := s.store.GetIndustryConfigPack(r.Context(), domain, code)
	if err != nil {
		writeError(w, http.StatusNotFound, "行业配置不存在")
		return
	}
	writeJSON(w, http.StatusOK, pack)
}

// upsertIndustryConfigPack 创建/更新行业配置包。
func (s *Server) upsertIndustryConfigPack(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.resolveActor(w, r); !ok {
		return
	}
	var input industryConfigPackInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求体格式错误")
		return
	}
	input.BizDomain = strings.TrimSpace(input.BizDomain)
	input.IndustryCode = strings.TrimSpace(input.IndustryCode)
	input.IndustryName = strings.TrimSpace(input.IndustryName)
	input.SellingDimensionsMD = strings.TrimSpace(input.SellingDimensionsMD)
	input.SidesJSON = strings.TrimSpace(input.SidesJSON)
	if !appdbValidIndustryBizDomain(input.BizDomain) {
		writeError(w, http.StatusBadRequest, "业务域须为 ecommerce 或 local_service")
		return
	}
	if input.IndustryCode == "" || input.IndustryName == "" {
		writeError(w, http.StatusBadRequest, "行业 code 与名称不能为空")
		return
	}
	if len([]rune(input.SellingDimensionsMD)) < 50 || len([]rune(input.SellingDimensionsMD)) > 64*1024 {
		writeError(w, http.StatusBadRequest, "挖掘维度 md 须为 50 字到 64KB")
		return
	}
	if _, err := model.ParseIndustrySides(input.SidesJSON); err != nil {
		writeError(w, http.StatusBadRequest, "侧面配置无效："+err.Error())
		return
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	if status != "active" && status != "disabled" {
		writeError(w, http.StatusBadRequest, "状态须为 active 或 disabled")
		return
	}
	pack := model.IndustryConfigPack{
		BizDomain:           input.BizDomain,
		IndustryCode:        input.IndustryCode,
		IndustryName:        input.IndustryName,
		SellingDimensionsMD: input.SellingDimensionsMD,
		SidesJSON:           input.SidesJSON,
		P0ExtraMD:           strings.TrimSpace(input.P0ExtraMD),
		Version:             strings.TrimSpace(input.Version),
		Status:              status,
	}
	if err := s.store.UpsertIndustryConfigPack(r.Context(), pack); err != nil {
		writeError(w, http.StatusInternalServerError, "保存行业配置失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "industry_code": input.IndustryCode, "biz_domain": input.BizDomain})
}

// 复用 db 层的业务域校验，避免 httpapi 直接 import db 常量以外的判断。
func appdbValidIndustryBizDomain(domain string) bool {
	return domain == "ecommerce" || domain == "local_service"
}
