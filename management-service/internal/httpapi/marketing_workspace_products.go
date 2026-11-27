package httpapi

import "net/http"

type workspaceMarketingProduct struct {
	ID     int64   `json:"id"`
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Cents  *uint64 `json:"cents,omitempty"`
}

func (s *Server) commercialMarketingWorkspaceProducts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.view"); !ok {
		return
	}
	ctx := r.Context()
	items := make([]workspaceMarketingProduct, 0)
	memberships, err := s.store.ListCommercialMembershipPlans(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取会员标的失败")
		return
	}
	for _, item := range memberships {
		version := item.ActiveVersion
		if version == nil {
			version = item.DraftVersion
		}
		if version == nil {
			version = item.LatestVersion
		}
		row := workspaceMarketingProduct{ID: item.ID, Kind: "membership", Name: item.Name, Status: item.Status}
		if version != nil {
			row.Cents = &version.PriceCents
		}
		items = append(items, row)
	}
	cards, err := s.store.ListCommercialTimeCards(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取时长卡标的失败")
		return
	}
	for _, item := range cards {
		version := item.ActiveVersion
		if version == nil {
			version = item.DraftVersion
		}
		if version == nil {
			version = item.LatestVersion
		}
		row := workspaceMarketingProduct{ID: item.ID, Kind: "time_card", Name: item.Name, Status: item.Status}
		if version != nil {
			row.Cents = &version.PriceCents
		}
		items = append(items, row)
	}
	devices, err := s.store.ListCommercialDeviceProducts(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取设备标的失败")
		return
	}
	for _, item := range devices {
		version := item.ActiveVersion
		if version == nil {
			version = item.DraftVersion
		}
		if version == nil {
			version = item.LatestVersion
		}
		row := workspaceMarketingProduct{ID: item.ID, Kind: "device_product", Name: item.Name, Status: item.Status}
		if version != nil {
			row.Cents = &version.SalePriceCents
		}
		items = append(items, row)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
