package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func inventoryPageQuery(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	z, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	return p, z
}

func (s *Server) inventoryStockProducts(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	p, z := inventoryPageQuery(r)
	items, total, p, z, err := s.store.ListInventoryStockProducts(r.Context(), r.URL.Query().Get("q"), p, z)
	if err != nil {
		writeError(w, 500, "读取产品库存失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": p, "page_size": z})
}
func (s *Server) inventoryStockDevices(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	sku := strings.TrimSpace(r.URL.Query().Get("sku_code"))
	if sku == "" {
		writeError(w, 400, "请选择产品 SKU")
		return
	}
	p, z := inventoryPageQuery(r)
	items, total, p, z, err := s.store.ListInventoryDevicesPage(r.Context(), sku, r.URL.Query().Get("q"), r.URL.Query().Get("status"), p, z)
	if err != nil {
		writeError(w, 500, "读取产品实物设备失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total, "page": p, "page_size": z})
}
func (s *Server) inventoryCheckBatch(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "inventory.view"); !ok {
		return
	}
	sku := strings.TrimSpace(r.URL.Query().Get("sku_code"))
	if sku == "" {
		writeError(w, 400, "请选择产品 SKU")
		return
	}
	batch, err := model.ComposeInventoryBatch("", "", r.URL.Query().Get("batch_no"), time.Now())
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	exists, err := s.store.InventoryBatchExists(r.Context(), sku, batch)
	if err != nil {
		writeError(w, 500, "批次重复检查失败")
		return
	}
	writeJSON(w, 200, map[string]any{"exists": exists, "batch_no": batch})
}
