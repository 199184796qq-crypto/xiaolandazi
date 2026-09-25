package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

var salesLeadStages = map[string]bool{"new": true, "visit_planned": true, "contacted": true, "interested": true, "quoted": true, "trial": true, "negotiation": true}
var salesLeadActivityTypes = map[string]bool{"phone": true, "wechat": true, "visit": true, "message": true, "note": true}

func salesText(value, label string, max int, required bool) error {
	n := utf8.RuneCountInString(strings.TrimSpace(value))
	if (required && n == 0) || n > max {
		return errors.New(label + "不能为空或超过允许长度")
	}
	return nil
}

func validateSalesLeadInput(p *model.SalesLeadInput) error {
	p.BusinessName = strings.TrimSpace(p.BusinessName)
	p.Phone = strings.TrimSpace(p.Phone)
	p.Wechat = strings.TrimSpace(p.Wechat)
	p.Stage = strings.TrimSpace(p.Stage)
	if p.Stage != "" && !salesLeadStages[p.Stage] {
		return errors.New("意向阶段无效")
	}
	if p.Phone == "" && p.Wechat == "" {
		return errors.New("电话或微信至少填写一项")
	}
	for _, f := range []struct {
		v, label string
		max      int
		required bool
	}{
		{p.BusinessName, "商家/顾客名称", 160, true}, {p.ContactName, "联系人", 128, false},
		{p.Phone, "电话", 64, false}, {p.Wechat, "微信", 128, false}, {p.Email, "邮箱", 255, false},
		{p.IndustryName, "行业", 128, false}, {p.Province, "省", 64, false}, {p.City, "市", 64, false},
		{p.District, "区县", 64, false}, {p.Address, "地址", 255, false}, {p.SourceType, "来源", 32, false},
	} {
		if err := salesText(f.v, f.label, f.max, f.required); err != nil {
			return err
		}
	}
	if _, ok := normalizeCredentialEmail(p.Email); !ok {
		return errors.New("邮箱格式不正确")
	}
	return nil
}

func validateSalesActivity(p *model.SalesLeadActivityInput) error {
	p.Content = strings.TrimSpace(p.Content)
	if !salesLeadActivityTypes[p.ActivityType] {
		return errors.New("跟进方式无效")
	}
	if p.Stage != "" && !salesLeadStages[p.Stage] {
		return errors.New("意向阶段无效")
	}
	if err := salesText(p.Content, "跟进内容", 2000, true); err != nil {
		return err
	}
	return salesText(p.Outcome, "沟通结果", 64, false)
}

func (s *Server) requireSalesLeadActor(w http.ResponseWriter, r *http.Request) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, false
	}
	if !actor.IsSalesStaff() {
		writeError(w, 403, "仅销售人员可操作本人意向顾客")
		return actor, false
	}
	return actor, true
}

func writeSalesBusinessError(w http.ResponseWriter, err error, message string) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, 404, "记录不存在或不在你的负责范围")
	case strings.Contains(err.Error(), "lead is closed"), strings.Contains(err.Error(), "invalid handoff"), strings.Contains(err.Error(), "portfolio changed"):
		writeError(w, 409, "状态已变化或已结束，请刷新后重试")
	case strings.Contains(err.Error(), "duplicate:"):
		writeError(w, 409, "账号或顾客资料已存在，请核实，不能重复创建或抢占已有客户")
	default:
		writeError(w, 500, message)
	}
}

func (s *Server) salesListLeads(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	q := r.URL.Query()
	items, total, summary, err := s.store.ListSalesLeadsByUser(r.Context(), actor.UserID, q.Get("search"), q.Get("status"), q.Get("stage"), q.Get("sort"), page, size)
	if err != nil {
		writeSalesBusinessError(w, err, "读取意向顾客失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size), "summary": summary})
}

func (s *Server) salesGetLead(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("leadID"))
	if !ok {
		return
	}
	item, err := s.store.GetSalesLeadByUser(r.Context(), actor.UserID, id)
	if err != nil {
		writeSalesBusinessError(w, err, "读取意向顾客失败")
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) salesCreateLead(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	var p model.SalesLeadInput
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validateSalesLeadInput(&p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	item, err := s.store.CreateSalesLeadByUser(r.Context(), actor.UserID, p)
	if err != nil {
		writeSalesBusinessError(w, err, "创建意向顾客失败")
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) salesUpdateLead(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("leadID"))
	if !ok {
		return
	}
	var p model.SalesLeadInput
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validateSalesLeadInput(&p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	item, err := s.store.UpdateSalesLeadByUser(r.Context(), actor.UserID, id, p)
	if err != nil {
		writeSalesBusinessError(w, err, "修改意向顾客失败")
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) salesListLeadActivities(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("leadID"))
	if !ok {
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	items, total, err := s.store.ListSalesLeadActivitiesByUser(r.Context(), actor.UserID, id, page, size)
	if err != nil {
		writeSalesBusinessError(w, err, "读取拜访记录失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}

func (s *Server) salesCreateLeadActivity(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("leadID"))
	if !ok {
		return
	}
	var p model.SalesLeadActivityInput
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validateSalesActivity(&p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	item, err := s.store.CreateSalesLeadActivityByUser(r.Context(), actor.UserID, id, p)
	if err != nil {
		writeSalesBusinessError(w, err, "保存拜访记录失败")
		return
	}
	writeJSON(w, 201, item)
}

func (s *Server) salesLoseLead(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("leadID"))
	if !ok {
		return
	}
	var p struct {
		Reason string `json:"reason"`
	}
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := salesText(p.Reason, "失败原因", 1024, true); err != nil {
		writeError(w, 400, "未成交必须填写失败原因（最多1024字）")
		return
	}
	item, err := s.store.CloseSalesLeadLostByUser(r.Context(), actor.UserID, id, p.Reason)
	if err != nil {
		writeSalesBusinessError(w, err, "结束意向顾客失败")
		return
	}
	writeJSON(w, 200, item)
}

func validateSalesConversion(p *model.ConvertSalesLeadInput) error {
	p.Username = strings.TrimSpace(p.Username)
	p.DisplayName = strings.TrimSpace(p.DisplayName)
	p.Phone = strings.TrimSpace(p.Phone)
	p.DeliveryMethod = normalizeDeliveryMethod(p.DeliveryMethod)
	for _, f := range []struct {
		v, label string
		max      int
		required bool
	}{
		{p.Username, "登录账号", 128, true}, {p.DisplayName, "客户名称", 128, true}, {p.Phone, "电话", 64, true},
		{p.Email, "邮箱", 254, false}, {p.Province, "省", 64, true}, {p.City, "市", 64, true}, {p.District, "区县", 64, true},
		{p.Address, "地址", 255, false}, {p.HandoffSummary, "开户说明", 2000, false},
	} {
		if err := salesText(f.v, f.label, f.max, f.required); err != nil {
			return err
		}
	}
	email, ok := normalizeCredentialEmail(p.Email)
	if !ok {
		return errors.New("邮箱格式不正确")
	}
	p.Email = email
	if p.DeliveryMethod == "email" && email == "" {
		return errors.New("邮件交付需要填写邮箱")
	}
	return nil
}

func (s *Server) salesConvertLead(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireSalesLeadActor(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("leadID"))
	if !ok {
		return
	}
	var p model.ConvertSalesLeadInput
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if err := validateSalesConversion(&p); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// Check ownership before generating any credential; neither client nor LLM supplies a user/tenant owner.
	lead, err := s.store.GetSalesLeadByUser(r.Context(), actor.UserID, id)
	if err != nil {
		writeSalesBusinessError(w, err, "读取顾客失败")
		return
	}
	if lead.Status != "open" {
		writeError(w, 409, "顾客已经结束或转换，不能重复开户")
		return
	}
	password, err := auth.GenerateInitialPassword()
	if err != nil {
		writeError(w, 500, "生成初始密码失败")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		writeError(w, 500, "生成登录凭证失败")
		return
	}
	updated, user, _, err := s.store.ConvertSalesLeadByUser(r.Context(), actor.UserID, id, p, hash)
	if err != nil {
		writeSalesBusinessError(w, err, "转正式客户失败")
		return
	}
	credential := s.deliverInitialCredential(p.DeliveryMethod, p.Email, user.DisplayName, user.Username, password)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, map[string]any{"lead": updated, "customer": map[string]any{"user_id": user.ID, "tenant_id": user.TenantID, "username": user.Username, "display_name": user.DisplayName}, "handoff": nil, "qualification": "unconfirmed", "credential": credential})
}
