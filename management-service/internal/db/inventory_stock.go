package db

import (
	"context"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrInventoryBatchExists = errors.New("该产品的采购批次已存在，请修改前缀或后缀")

func inventoryPaging(page, size, total int) (int, int) {
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	if page < 1 {
		page = 1
	}
	last := (total + size - 1) / size
	if last < 1 {
		last = 1
	}
	if page > last {
		page = last
	}
	return page, size
}

func (s *Store) InventoryBatchExists(ctx context.Context, sku, batch string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT (EXISTS(SELECT 1 FROM inv_batch_registry WHERE sku_code=? AND batch_no=?) OR EXISTS(SELECT 1 FROM inv_devices WHERE sku_code=? AND batch_no=?))`, sku, batch, sku, batch).Scan(&n)
	return n != 0, err
}

func (s *Store) ListInventoryStockProducts(ctx context.Context, q string, page, size int) ([]model.InventoryStockProduct, int, int, int, error) {
	from := ` FROM (SELECT sku_code FROM catalog_device_products WHERE status<>'retired' UNION SELECT sku_code FROM inv_devices) sk LEFT JOIN catalog_device_products p ON p.sku_code=sk.sku_code `
	where := ` WHERE 1=1 `
	args := []any{}
	if q = strings.TrimSpace(q); q != "" {
		where += ` AND (sk.sku_code LIKE ? OR p.name LIKE ? OR EXISTS(SELECT 1 FROM inv_devices sd LEFT JOIN device_hardware_profiles hp ON hp.device_id=sd.id WHERE sd.sku_code=sk.sku_code AND (sd.sn LIKE ? OR hp.hardware_mac LIKE ?))) `
		for i := 0; i < 4; i++ {
			args = append(args, "%"+q+"%")
		}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, 0, 0, err
	}
	page, size = inventoryPaging(page, size, total)
	query := `SELECT COALESCE(p.id,0),COALESCE(p.name,sk.sku_code),sk.sku_code,COUNT(d.id),
	COALESCE(SUM(d.lifecycle_status='IN_STOCK' AND d.quality_status='qualified'),0),
	COALESCE(SUM(d.lifecycle_status IN ('RESERVED','IN_TRANSIT','RMA_TRANSIT','REPAIR_TRANSIT','REPAIR_RETURN_TRANSIT')),0),
	COALESCE(SUM(d.lifecycle_status IN ('AFTER_SALES','REPAIRING','EXTERNAL_REPAIR')),0),
	COALESCE(SUM(d.lifecycle_status IN ('SCRAP_PENDING','SCRAPPED')),0),
	COALESCE(SUM(d.lifecycle_status IN ('SOLD','CUSTOMER_BOUND','ACTIVE')),0)` + from + ` LEFT JOIN inv_devices d ON d.sku_code=sk.sku_code ` + where + ` GROUP BY p.id,p.name,sk.sku_code ORDER BY COALESCE(p.id,0) DESC,sk.sku_code LIMIT ? OFFSET ?`
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer rows.Close()
	items := make([]model.InventoryStockProduct, 0)
	for rows.Next() {
		var d model.InventoryStockProduct
		if err := rows.Scan(&d.ProductID, &d.Name, &d.SKUCode, &d.Total, &d.Available, &d.Transit, &d.Repair, &d.Scrap, &d.Delivered); err != nil {
			return nil, 0, 0, 0, err
		}
		items = append(items, d)
	}
	return items, total, page, size, rows.Err()
}

func (s *Store) ListInventoryDevicesPage(ctx context.Context, sku, q, status string, page, size int) ([]model.Device, int, int, int, error) {
	where := ` WHERE d.sku_code=? `
	args := []any{sku}
	if q = strings.TrimSpace(q); q != "" {
		where += ` AND (d.sn LIKE ? OR d.batch_no LIKE ? OR p.hardware_mac LIKE ? OR w.name LIKE ?) `
		for i := 0; i < 4; i++ {
			args = append(args, "%"+q+"%")
		}
	}
	if status != "" && status != "all" {
		where += ` AND d.lifecycle_status=? `
		args = append(args, status)
	}
	from := ` FROM inv_devices d LEFT JOIN inv_warehouses w ON w.id=d.custody_warehouse_id LEFT JOIN device_hardware_profiles p ON p.device_id=d.id `
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, 0, 0, err
	}
	page, size = inventoryPaging(page, size, total)
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.sn,d.sku_code,d.batch_no,d.owner_org_id,d.custody_warehouse_id,COALESCE(w.name,''),d.current_customer_id,d.lifecycle_status,d.quality_status,d.created_by_user_id,d.updated_by_user_id,d.created_at,d.updated_at,COALESCE(p.hardware_mac,''),COALESCE(p.claim_enabled,FALSE)`+from+where+` ORDER BY d.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	defer rows.Close()
	items := make([]model.Device, 0)
	for rows.Next() {
		var d model.Device
		if err := rows.Scan(&d.ID, &d.SN, &d.SKUCode, &d.BatchNo, &d.OwnerOrgID, &d.CustodyWarehouseID, &d.CustodyWarehouse, &d.CurrentCustomerID, &d.LifecycleStatus, &d.QualityStatus, &d.CreatedByUserID, &d.UpdatedByUserID, &d.CreatedAt, &d.UpdatedAt, &d.HardwareMAC, &d.ClaimEnabled); err != nil {
			return nil, 0, 0, 0, err
		}
		items = append(items, d)
	}
	return items, total, page, size, rows.Err()
}
