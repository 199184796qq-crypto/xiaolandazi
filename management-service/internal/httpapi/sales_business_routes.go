package httpapi

import "net/http"

func (s *Server) registerSalesBusinessRoutes(mux *http.ServeMux) {
	s.registerCustomerBusinessRoutes(mux)
	mux.HandleFunc("GET /api/v1/sales/customers/page", s.salesCustomerPage)
	mux.HandleFunc("GET /api/v1/sales/leads", s.salesListLeads)
	mux.HandleFunc("POST /api/v1/sales/leads", s.salesCreateLead)
	mux.HandleFunc("GET /api/v1/sales/leads/{leadID}", s.salesGetLead)
	mux.HandleFunc("PUT /api/v1/sales/leads/{leadID}", s.salesUpdateLead)
	mux.HandleFunc("GET /api/v1/sales/leads/{leadID}/activities", s.salesListLeadActivities)
	mux.HandleFunc("POST /api/v1/sales/leads/{leadID}/activities", s.salesCreateLeadActivity)
	mux.HandleFunc("POST /api/v1/sales/leads/{leadID}/lost", s.salesLoseLead)
	mux.HandleFunc("POST /api/v1/sales/leads/{leadID}/convert", s.salesConvertLead)
	mux.HandleFunc("GET /api/v1/admin/sales/handover-people", s.adminSalesHandoverPeople)
	mux.HandleFunc("GET /api/v1/admin/sales/handovers", s.adminSalesHandoverHistory)
	mux.HandleFunc("GET /api/v1/admin/sales/{salesStaffID}/handover-preview", s.adminSalesHandoverPreview)
	mux.HandleFunc("POST /api/v1/admin/sales/{salesStaffID}/handover", s.adminSalesHandover)
	mux.HandleFunc("GET /api/v1/liveops/customer-handoffs", s.liveOpsListCustomerHandoffs)
	mux.HandleFunc("PATCH /api/v1/liveops/customer-handoffs/{handoffID}", s.liveOpsUpdateCustomerHandoff)
	mux.HandleFunc("GET /api/v1/liveops/customer-handoffs/{handoffID}/events", s.liveOpsHandoffEvents)
}

func (s *Server) adminSalesHandoverPeople(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "sales.assignment.manage")
	if !ok {
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	q := r.URL.Query()
	items, total, err := s.store.ListSalesHandoverPeople(r.Context(), staffBusinessScope(actor, access, "sales.assignment.manage"), q.Get("search"), q.Get("status"), page, size)
	if err != nil {
		writeSalesBusinessError(w, err, "读取销售交接人员失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}

func (s *Server) adminSalesHandoverHistory(w http.ResponseWriter, r *http.Request) {
	actor, access, ok := s.requireStaffPermission(w, r, "sales.assignment.manage")
	if !ok {
		return
	}
	page, size := normalizedPageQuery(r, 12, 100)
	items, total, err := s.store.ListSalesHandoverHistory(r.Context(), staffBusinessScope(actor, access, "sales.assignment.manage"), page, size)
	if err != nil {
		writeSalesBusinessError(w, err, "读取销售交接历史失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "page_size": size, "total_pages": totalPages(total, size)})
}
