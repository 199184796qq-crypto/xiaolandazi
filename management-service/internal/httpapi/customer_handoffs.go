package httpapi

import (
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) requireCustomerHandoffAccess(w http.ResponseWriter, r *http.Request) (model.Actor, bool, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, false, false
	}
	if !actor.IsInternalStaff() {
		writeError(w, 403, "仅营销运维岗位可处理客户交接")
		return actor, false, false
	}
	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, 503, "读取岗位权限失败")
		return actor, false, false
	}
	manager := actor.IsPlatformAdmin() || staffHasPermission(access, "liveops.ticket.manage")
	if !manager && !staffHasPermission(access, "liveops.configure") {
		writeError(w, 403, "没有客户交接权限")
		return actor, false, false
	}
	return actor, manager, true
}

func (s *Server) liveOpsListCustomerHandoffs(w http.ResponseWriter, r *http.Request) {
	actor, manager, ok := s.requireCustomerHandoffAccess(w, r)
	if !ok {
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	q := r.URL.Query()
	items, total, err := s.store.ListCustomerHandoffs(r.Context(), actor.UserID, manager, q.Get("search"), q.Get("status"), q.Get("sort"), page, size)
	if err != nil {
		writeSalesBusinessError(w, err, "读取运维交接失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size), "manager_view": manager})
}

func (s *Server) liveOpsUpdateCustomerHandoff(w http.ResponseWriter, r *http.Request) {
	actor, manager, ok := s.requireCustomerHandoffAccess(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("handoffID"))
	if !ok {
		return
	}
	var p struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if readJSON(w, r, &p) != nil {
		writeError(w, 400, "请求格式错误")
		return
	}
	if p.Status != "accepted" && p.Status != "in_progress" && p.Status != "completed" {
		writeError(w, 400, "交接状态无效")
		return
	}
	if err := salesText(p.Note, "处理说明", 2000, p.Status == "completed"); err != nil {
		writeError(w, 400, "完成交接必须填写处理说明（最多2000字）")
		return
	}
	item, err := s.store.UpdateCustomerHandoffStatus(r.Context(), manager, p.Note, id, actor.UserID, p.Status)
	if err != nil {
		writeSalesBusinessError(w, err, "更新运维交接失败")
		return
	}
	writeJSON(w, 200, item)
}

func (s *Server) liveOpsHandoffEvents(w http.ResponseWriter, r *http.Request) {
	actor, manager, ok := s.requireCustomerHandoffAccess(w, r)
	if !ok {
		return
	}
	id, ok := parsePositivePathID(w, r.PathValue("handoffID"))
	if !ok {
		return
	}
	item, err := s.store.GetCustomerHandoff(r.Context(), id)
	if err != nil {
		writeSalesBusinessError(w, err, "读取交接失败")
		return
	}
	if !manager && item.Status != "pending" && (item.AcceptedByUserID == nil || *item.AcceptedByUserID != actor.UserID) {
		writeError(w, 404, "交接记录不存在")
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	items, total, err := s.store.ListCustomerHandoffEvents(r.Context(), id, page, size)
	if err != nil {
		writeSalesBusinessError(w, err, "读取处理历史失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}
