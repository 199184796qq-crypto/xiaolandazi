package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

//go:embed inventory_schema.sql
var inventorySchema string

func (s *Store) MigrateInventory(ctx context.Context) error {
	schema := strings.ReplaceAll(inventorySchema, "\r\n", "\n")
	for _, raw := range strings.Split(schema, "\n-- +statement\n") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply inventory schema: %w", err)
		}
	}
	columns := []struct {
		table      string
		column     string
		definition string
	}{
		{"inv_stock_documents", "reference_no", "VARCHAR(96) NOT NULL DEFAULT '' AFTER counterparty_org_id"},
		{"inv_stock_documents", "product_id", "BIGINT UNSIGNED NULL AFTER reference_no"},
		{"inv_stock_documents", "product_name_snapshot", "VARCHAR(160) NOT NULL DEFAULT '' AFTER product_id"},
		{"inv_stock_documents", "expected_quantity", "INT NOT NULL DEFAULT 0 AFTER product_name_snapshot"},
		{"inv_stock_documents", "actual_quantity", "INT NOT NULL DEFAULT 0 AFTER expected_quantity"},
		{"inv_stock_documents", "business_amount_cents", "BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER actual_quantity"},
		{"inv_stock_documents", "counterparty_name", "VARCHAR(160) NOT NULL DEFAULT '' AFTER business_amount_cents"},
		{"inv_stock_documents", "payment_method", "VARCHAR(64) NOT NULL DEFAULT '' AFTER counterparty_name"},
		{"inv_shipments", "recipient_org_id", "BIGINT UNSIGNED NULL AFTER recipient_customer_id"},
		{"inv_shipments", "recipient_type", "VARCHAR(32) NOT NULL DEFAULT 'individual' AFTER recipient_org_id"},
		{"inv_shipments", "delivery_method", "VARCHAR(32) NOT NULL DEFAULT 'courier' AFTER recipient_type"},
		{"inv_shipments", "logistics_fee_cents", "BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER delivery_method"},
		{"inv_rma_links", "repair_outbound_shipment_id", "BIGINT UNSIGNED NULL AFTER outbound_shipment_no"},
		{"inv_rma_links", "repair_outbound_shipment_no", "VARCHAR(64) NOT NULL DEFAULT '' AFTER repair_outbound_shipment_id"},
		{"inv_rma_links", "repair_return_shipment_id", "BIGINT UNSIGNED NULL AFTER repair_outbound_shipment_no"},
		{"inv_rma_links", "repair_return_shipment_no", "VARCHAR(64) NOT NULL DEFAULT '' AFTER repair_return_shipment_id"},
		{"inv_rmas", "source_type", "VARCHAR(32) NOT NULL DEFAULT 'staff_manual' AFTER status"},
		{"inv_rmas", "source_user_id", "BIGINT UNSIGNED NULL AFTER source_type"},
		{"inv_rmas", "source_tenant_id", "BIGINT UNSIGNED NULL AFTER source_user_id"},
		{"inv_rmas", "source_agent_org_id", "BIGINT UNSIGNED NULL AFTER source_tenant_id"},
		{"inv_rmas", "contact_phone", "VARCHAR(64) NOT NULL DEFAULT '' AFTER customer_name"},
		{"inv_rmas", "accepted_by_user_id", "BIGINT UNSIGNED NULL AFTER operator_user_id"},
		{"inv_rmas", "accepted_at", "DATETIME(3) NULL AFTER accepted_by_user_id"},
	}
	for _, column := range columns {
		if err := s.ensureInventoryColumn(
			ctx,
			column.table,
			column.column,
			column.definition,
		); err != nil {
			return err
		}
	}
	indexes := []struct {
		table   string
		index   string
		columns string
	}{
		{"inv_rma_links", "idx_inv_rma_links_repair_outbound", "repair_outbound_shipment_id"},
		{"inv_rma_links", "idx_inv_rma_links_repair_return", "repair_return_shipment_id"},
	}
	for _, index := range indexes {
		if err := s.ensureInventoryIndex(
			ctx,
			index.table,
			index.index,
			index.columns,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureInventoryColumn(
	ctx context.Context,
	table string,
	column string,
	definition string,
) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=DATABASE()
		  AND TABLE_NAME=?
		  AND COLUMN_NAME=?
	`, table, column).Scan(&count); err != nil {
		return fmt.Errorf("inspect inventory column %s.%s: %w", table, column, err)
	}
	if count > 0 {
		return nil
	}
	query := fmt.Sprintf(
		"ALTER TABLE `%s` ADD COLUMN `%s` %s",
		table,
		column,
		definition,
	)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("add inventory column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) ensureInventoryIndex(
	ctx context.Context,
	table string,
	index string,
	columns string,
) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=DATABASE()
		  AND TABLE_NAME=?
		  AND INDEX_NAME=?
	`, table, index).Scan(&count); err != nil {
		return fmt.Errorf("inspect inventory index %s.%s: %w", table, index, err)
	}
	if count > 0 {
		return nil
	}
	query := fmt.Sprintf(
		"ALTER TABLE `%s` ADD INDEX `%s` (%s)",
		table,
		index,
		columns,
	)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("add inventory index %s.%s: %w", table, index, err)
	}
	return nil
}

func (s *Store) EnsureDefaultWarehouse(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO inv_warehouses (code, name, status)
		VALUES
			('HQ_MAIN', '总部主仓', 'active'),
			('AFTER_SALES_PENDING', '售后待检库', 'active'),
			('REPAIR', '维修库', 'active'),
			('SCRAP_HOLD', '报废待处置库', 'active')
		ON DUPLICATE KEY UPDATE status='active'
	`)
	if err != nil {
		return fmt.Errorf("seed default warehouse: %w", err)
	}
	return nil
}

func (s *Store) ListInventoryDeviceProducts(ctx context.Context) ([]model.InventoryDeviceProduct, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, sku_code, name, status
		FROM catalog_device_products
		WHERE status <> 'retired'
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.InventoryDeviceProduct, 0)
	for rows.Next() {
		var item model.InventoryDeviceProduct
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.SKUCode,
			&item.Name,
			&item.Status,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListInventoryDeviceSKUTypes(ctx context.Context) ([]model.InventoryDeviceSKUType, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.sku_code,
		       COUNT(*) AS total_quantity,
		       SUM(CASE WHEN d.lifecycle_status='IN_STOCK' THEN 1 ELSE 0 END) AS in_stock_quantity,
		       COUNT(DISTINCT CASE WHEN d.lifecycle_status='IN_STOCK' THEN d.custody_warehouse_id END) AS warehouse_count,
		       MIN(d.sn) AS sample_sn,
		       COALESCE(MAX(d.batch_no), '') AS sample_batch_no,
		       COALESCE(MAX(p.id), 0) AS bound_product_id,
		       COALESCE(MAX(p.name), '') AS bound_product_name
		FROM inv_devices d
		LEFT JOIN catalog_device_products p ON p.sku_code=d.sku_code
		WHERE TRIM(d.sku_code) <> ''
		GROUP BY d.sku_code
		ORDER BY in_stock_quantity DESC, total_quantity DESC, d.sku_code ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.InventoryDeviceSKUType, 0)
	for rows.Next() {
		var item model.InventoryDeviceSKUType
		if err := rows.Scan(
			&item.SKUCode,
			&item.TotalQuantity,
			&item.InStockQuantity,
			&item.WarehouseCount,
			&item.SampleSN,
			&item.SampleBatchNo,
			&item.BoundProductID,
			&item.BoundProductName,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListWarehouses(ctx context.Context) ([]model.Warehouse, error) {
	return s.listWarehouses(ctx, false)
}

func (s *Store) ListAllWarehouses(ctx context.Context) ([]model.Warehouse, error) {
	return s.listWarehouses(ctx, true)
}

func (s *Store) listWarehouses(ctx context.Context, includeInactive bool) ([]model.Warehouse, error) {
	query := `
		SELECT id, code, name, organization_id, status, created_at, updated_at
		FROM inv_warehouses
	`
	if !includeInactive {
		query += " WHERE status='active'"
	}
	query += " ORDER BY CASE WHEN status='active' THEN 0 ELSE 1 END, id ASC"

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Warehouse, 0)
	for rows.Next() {
		var item model.Warehouse
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.OrganizationID,
			&item.Status,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetWarehouse(ctx context.Context, warehouseID int64) (model.Warehouse, error) {
	var item model.Warehouse
	err := s.db.QueryRowContext(ctx, `
		SELECT id, code, name, organization_id, status, created_at, updated_at
		FROM inv_warehouses
		WHERE id=?
	`, warehouseID).Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&item.OrganizationID,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (s *Store) CreateWarehouse(ctx context.Context, input model.WarehouseInput) (model.Warehouse, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO inv_warehouses (code, name, status)
		VALUES (?, ?, ?)
	`, input.Code, input.Name, input.Status)
	if err != nil {
		return model.Warehouse{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.Warehouse{}, err
	}
	return s.GetWarehouse(ctx, id)
}

func (s *Store) UpdateWarehouse(ctx context.Context, warehouseID int64, input model.WarehouseInput) (model.Warehouse, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE inv_warehouses
		SET code=?, name=?, status=?
		WHERE id=?
	`, input.Code, input.Name, input.Status, warehouseID)
	if err != nil {
		return model.Warehouse{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		if _, err := s.GetWarehouse(ctx, warehouseID); err != nil {
			return model.Warehouse{}, err
		}
	}
	return s.GetWarehouse(ctx, warehouseID)
}

func (s *Store) ListDevices(ctx context.Context) ([]model.Device, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.sn, d.sku_code, d.batch_no, d.owner_org_id,
		       d.custody_warehouse_id, COALESCE(w.name, ''),
		       d.current_customer_id, d.lifecycle_status, d.quality_status,
		       d.created_by_user_id, d.updated_by_user_id,
		       d.created_at, d.updated_at
		FROM inv_devices d
		LEFT JOIN inv_warehouses w ON w.id=d.custody_warehouse_id
		ORDER BY d.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Device, 0)
	for rows.Next() {
		var item model.Device
		if err := rows.Scan(
			&item.ID,
			&item.SN,
			&item.SKUCode,
			&item.BatchNo,
			&item.OwnerOrgID,
			&item.CustodyWarehouseID,
			&item.CustodyWarehouse,
			&item.CurrentCustomerID,
			&item.LifecycleStatus,
			&item.QualityStatus,
			&item.CreatedByUserID,
			&item.UpdatedByUserID,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, deviceID int64) (model.Device, error) {
	var item model.Device
	err := s.db.QueryRowContext(ctx, `
		SELECT d.id, d.sn, d.sku_code, d.batch_no, d.owner_org_id,
		       d.custody_warehouse_id, COALESCE(w.name, ''),
		       d.current_customer_id, d.lifecycle_status, d.quality_status,
		       d.created_by_user_id, d.updated_by_user_id,
		       d.created_at, d.updated_at
		FROM inv_devices d
		LEFT JOIN inv_warehouses w ON w.id=d.custody_warehouse_id
		WHERE d.id=?
	`, deviceID).Scan(
		&item.ID,
		&item.SN,
		&item.SKUCode,
		&item.BatchNo,
		&item.OwnerOrgID,
		&item.CustodyWarehouseID,
		&item.CustodyWarehouse,
		&item.CurrentCustomerID,
		&item.LifecycleStatus,
		&item.QualityStatus,
		&item.CreatedByUserID,
		&item.UpdatedByUserID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (s *Store) ListDeviceLedger(ctx context.Context, deviceID int64) ([]model.DeviceLedgerEntry, error) {
	query := `
		SELECT l.id, l.device_id, l.document_id, d.document_no, l.action,
		       l.from_status, l.to_status, l.from_warehouse_id, l.to_warehouse_id,
		       l.from_owner_org_id, l.to_owner_org_id,
		       l.from_customer_id, l.to_customer_id,
		       l.operator_user_id, l.reason, l.created_at
		FROM inv_device_ledger l
		INNER JOIN inv_stock_documents d ON d.id=l.document_id
	`
	args := make([]any, 0, 1)
	if deviceID > 0 {
		query += " WHERE l.device_id=?"
		args = append(args, deviceID)
	}
	query += " ORDER BY l.id DESC LIMIT 2000"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.DeviceLedgerEntry, 0)
	for rows.Next() {
		var item model.DeviceLedgerEntry
		if err := rows.Scan(
			&item.ID,
			&item.DeviceID,
			&item.DocumentID,
			&item.DocumentNo,
			&item.Action,
			&item.FromStatus,
			&item.ToStatus,
			&item.FromWarehouseID,
			&item.ToWarehouseID,
			&item.FromOwnerOrgID,
			&item.ToOwnerOrgID,
			&item.FromCustomerID,
			&item.ToCustomerID,
			&item.OperatorUserID,
			&item.Reason,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListStockDocuments(ctx context.Context) ([]model.StockDocument, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.id, d.document_no, d.document_type, d.status,
		       d.from_warehouse_id, d.to_warehouse_id, d.counterparty_org_id,
		       d.reference_no, d.product_id, d.product_name_snapshot,
		       d.expected_quantity, d.actual_quantity,
		       d.business_amount_cents, d.counterparty_name, d.payment_method,
		       d.reason, d.operator_user_id, d.approved_by_user_id,
		       d.effective_at, d.created_at, COUNT(i.id)
		FROM inv_stock_documents d
		LEFT JOIN inv_stock_document_items i ON i.document_id=d.id
		GROUP BY d.id
		ORDER BY d.id DESC
		LIMIT 2000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StockDocument, 0)
	for rows.Next() {
		var item model.StockDocument
		if err := rows.Scan(
			&item.ID,
			&item.DocumentNo,
			&item.DocumentType,
			&item.Status,
			&item.FromWarehouseID,
			&item.ToWarehouseID,
			&item.CounterpartyOrgID,
			&item.ReferenceNo,
			&item.ProductID,
			&item.ProductName,
			&item.ExpectedQuantity,
			&item.ActualQuantity,
			&item.BusinessAmountCents,
			&item.CounterpartyName,
			&item.PaymentMethod,
			&item.Reason,
			&item.OperatorUserID,
			&item.ApprovedByUserID,
			&item.EffectiveAt,
			&item.CreatedAt,
			&item.ItemCount,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetInventorySummary(ctx context.Context) ([]model.InventorySummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT COALESCE(w.id, 0), COALESCE(w.name, '无仓库'),
		       d.lifecycle_status, COUNT(*)
		FROM inv_devices d
		LEFT JOIN inv_warehouses w ON w.id=d.custody_warehouse_id
		GROUP BY COALESCE(w.id, 0), COALESCE(w.name, '无仓库'), d.lifecycle_status
		ORDER BY COALESCE(w.id, 0), d.lifecycle_status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.InventorySummary, 0)
	for rows.Next() {
		var item model.InventorySummary
		if err := rows.Scan(
			&item.WarehouseID,
			&item.WarehouseName,
			&item.LifecycleStatus,
			&item.Quantity,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateBatchInbound(
	ctx context.Context,
	userID int64,
	input model.BatchInboundInput,
) (model.BatchInboundResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BatchInboundResult{}, err
	}
	defer tx.Rollback()

	if err := ensureWarehouseActiveTx(ctx, tx, input.WarehouseID); err != nil {
		return model.BatchInboundResult{}, err
	}

	var product model.InventoryDeviceProduct
	if err := tx.QueryRowContext(ctx, `
		SELECT id, code, sku_code, name, status
		FROM catalog_device_products
		WHERE id=? AND status <> 'retired'
		FOR UPDATE
	`, input.ProductID).Scan(
		&product.ID,
		&product.Code,
		&product.SKUCode,
		&product.Name,
		&product.Status,
	); err != nil {
		return model.BatchInboundResult{}, err
	}

	seen := make(map[string]bool, len(input.SNs))
	sns := make([]string, 0, len(input.SNs))
	for _, raw := range input.SNs {
		sn := strings.TrimSpace(raw)
		if sn == "" || seen[sn] {
			continue
		}
		seen[sn] = true
		sns = append(sns, sn)
	}
	if len(sns) == 0 {
		return model.BatchInboundResult{}, fmt.Errorf("batch inbound requires at least one SN")
	}
	if input.ExpectedQuantity <= 0 {
		input.ExpectedQuantity = len(sns)
	}
	if len(sns) > input.ExpectedQuantity {
		return model.BatchInboundResult{}, fmt.Errorf("actual quantity exceeds expected quantity")
	}

	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "采购入库"
	}
	docID, err := createStockDocumentTx(
		ctx,
		tx,
		"inbound",
		nil,
		&input.WarehouseID,
		nil,
		reason,
		userID,
	)
	if err != nil {
		return model.BatchInboundResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_stock_documents
		SET reference_no=?, product_id=?, product_name_snapshot=?,
		    expected_quantity=?, actual_quantity=?,
		    business_amount_cents=?, counterparty_name=?, payment_method=?
		WHERE id=?
	`,
		strings.TrimSpace(input.PurchaseNo),
		product.ID,
		product.Name,
		input.ExpectedQuantity,
		len(sns),
		input.PurchaseAmountCents,
		strings.TrimSpace(input.SupplierName),
		strings.TrimSpace(input.PaymentMethod),
		docID,
	); err != nil {
		return model.BatchInboundResult{}, err
	}

	quality := strings.TrimSpace(input.QualityStatus)
	if quality == "" {
		quality = "qualified"
	}
	deviceIDs := make([]int64, 0, len(sns))
	for _, sn := range sns {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO inv_devices (
				sn, sku_code, batch_no, custody_warehouse_id,
				lifecycle_status, quality_status, created_by_user_id, updated_by_user_id
			)
			VALUES (?, ?, ?, ?, 'IN_STOCK', ?, ?, ?)
		`,
			sn,
			product.SKUCode,
			strings.TrimSpace(input.BatchNo),
			input.WarehouseID,
			quality,
			userID,
			userID,
		)
		if err != nil {
			return model.BatchInboundResult{}, err
		}
		deviceID, err := result.LastInsertId()
		if err != nil {
			return model.BatchInboundResult{}, err
		}
		deviceIDs = append(deviceIDs, deviceID)
		if err := insertStockItemAndLedgerTx(
			ctx,
			tx,
			deviceID,
			docID,
			sn,
			product.SKUCode,
			"inbound",
			"",
			"IN_STOCK",
			nil,
			&input.WarehouseID,
			nil,
			nil,
			nil,
			nil,
			userID,
			reason,
		); err != nil {
			return model.BatchInboundResult{}, err
		}
	}

	var documentNo string
	if err := tx.QueryRowContext(
		ctx,
		"SELECT document_no FROM inv_stock_documents WHERE id=?",
		docID,
	).Scan(&documentNo); err != nil {
		return model.BatchInboundResult{}, err
	}
	if err := insertOperatingEntryTx(
		ctx,
		tx,
		"expense",
		"device_purchase",
		input.PurchaseAmountCents,
		"inventory_inbound",
		&docID,
		documentNo,
		"device_purchase:"+fmt.Sprint(docID),
		input.SupplierName,
		input.PaymentMethod,
		fmt.Sprintf("设备采购入库 · %s · %d 台", product.Name, len(sns)),
		userID,
		time.Now().UTC(),
	); err != nil {
		return model.BatchInboundResult{}, err
	}
	if err := s.commitInboxTx(ctx, tx, "inventory"); err != nil {
		return model.BatchInboundResult{}, err
	}

	return model.BatchInboundResult{
		DocumentID:          docID,
		DocumentNo:          documentNo,
		ProductID:           product.ID,
		ProductName:         product.Name,
		SKUCode:             product.SKUCode,
		BatchNo:             strings.TrimSpace(input.BatchNo),
		PurchaseNo:          strings.TrimSpace(input.PurchaseNo),
		ExpectedQuantity:    input.ExpectedQuantity,
		ActualQuantity:      len(sns),
		PurchaseAmountCents: input.PurchaseAmountCents,
		SupplierName:        strings.TrimSpace(input.SupplierName),
		PaymentMethod:       strings.TrimSpace(input.PaymentMethod),
		DeviceIDs:           deviceIDs,
		SNs:                 sns,
	}, nil
}

func (s *Store) CreateDevice(
	ctx context.Context,
	userID int64,
	input model.CreateDeviceInput,
) (model.Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Device{}, err
	}
	defer tx.Rollback()

	if err := ensureWarehouseActiveTx(ctx, tx, input.WarehouseID); err != nil {
		return model.Device{}, err
	}

	quality := strings.TrimSpace(input.QualityStatus)
	if quality == "" {
		quality = "qualified"
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_devices (
			sn, sku_code, batch_no, owner_org_id, custody_warehouse_id,
			lifecycle_status, quality_status, created_by_user_id, updated_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, 'IN_STOCK', ?, ?, ?)
	`,
		strings.TrimSpace(input.SN),
		strings.TrimSpace(input.SKUCode),
		strings.TrimSpace(input.BatchNo),
		input.OwnerOrgID,
		input.WarehouseID,
		quality,
		userID,
		userID,
	)
	if err != nil {
		return model.Device{}, err
	}
	deviceID, err := result.LastInsertId()
	if err != nil {
		return model.Device{}, err
	}

	docID, err := createStockDocumentTx(
		ctx,
		tx,
		"inbound",
		nil,
		&input.WarehouseID,
		input.OwnerOrgID,
		strings.TrimSpace(input.Reason),
		userID,
	)
	if err != nil {
		return model.Device{}, err
	}
	if err := insertStockItemAndLedgerTx(
		ctx,
		tx,
		deviceID,
		docID,
		strings.TrimSpace(input.SN),
		strings.TrimSpace(input.SKUCode),
		"inbound",
		"",
		"IN_STOCK",
		nil,
		&input.WarehouseID,
		nil,
		input.OwnerOrgID,
		nil,
		nil,
		userID,
		strings.TrimSpace(input.Reason),
	); err != nil {
		return model.Device{}, err
	}

	if err := s.commitInboxTx(ctx, tx, "inventory"); err != nil {
		return model.Device{}, err
	}
	return s.GetDevice(ctx, deviceID)
}

func (s *Store) TransitionDevice(
	ctx context.Context,
	userID int64,
	deviceID int64,
	input model.DeviceTransitionInput,
) (model.Device, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Device{}, err
	}
	defer tx.Rollback()

	current, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return model.Device{}, err
	}

	toStatus := strings.ToUpper(strings.TrimSpace(input.ToStatus))
	if !deviceTransitionAllowed(current.LifecycleStatus, toStatus) {
		return model.Device{}, fmt.Errorf(
			"invalid device transition: %s -> %s",
			current.LifecycleStatus,
			toStatus,
		)
	}

	toWarehouseID := current.CustodyWarehouseID
	if input.ToWarehouseID != nil {
		if err := ensureWarehouseActiveTx(ctx, tx, *input.ToWarehouseID); err != nil {
			return model.Device{}, err
		}
		toWarehouseID = input.ToWarehouseID
	}

	toOwnerOrgID := current.OwnerOrgID
	if input.ToOwnerOrgID != nil {
		toOwnerOrgID = input.ToOwnerOrgID
	}
	toCustomerID := current.CurrentCustomerID
	if input.ToCustomerID != nil {
		toCustomerID = input.ToCustomerID
	}
	if toStatus == "SCRAPPED" {
		toCustomerID = nil
	}

	action := actionForDeviceTransition(current.LifecycleStatus, toStatus)
	reason := strings.TrimSpace(input.Reason)
	if ref := strings.TrimSpace(input.ReferenceNo); ref != "" {
		if reason != "" {
			reason += " · "
		}
		reason += "关联单号 " + ref
	}

	docID, err := createStockDocumentTx(
		ctx,
		tx,
		action,
		current.CustodyWarehouseID,
		toWarehouseID,
		toOwnerOrgID,
		reason,
		userID,
	)
	if err != nil {
		return model.Device{}, err
	}

	quality := current.QualityStatus
	if toStatus == "SCRAPPED" {
		quality = "scrapped"
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_devices
		SET owner_org_id=?, custody_warehouse_id=?, current_customer_id=?,
		    lifecycle_status=?, quality_status=?, updated_by_user_id=?
		WHERE id=?
	`,
		toOwnerOrgID,
		toWarehouseID,
		toCustomerID,
		toStatus,
		quality,
		userID,
		deviceID,
	); err != nil {
		return model.Device{}, err
	}

	if err := insertStockItemAndLedgerTx(
		ctx,
		tx,
		deviceID,
		docID,
		current.SN,
		current.SKUCode,
		action,
		current.LifecycleStatus,
		toStatus,
		current.CustodyWarehouseID,
		toWarehouseID,
		current.OwnerOrgID,
		toOwnerOrgID,
		current.CurrentCustomerID,
		toCustomerID,
		userID,
		reason,
	); err != nil {
		return model.Device{}, err
	}

	if err := s.commitInboxTx(ctx, tx, "inventory", "logistics"); err != nil {
		return model.Device{}, err
	}
	return s.GetDevice(ctx, deviceID)
}

func (s *Store) DisposeScrapDevice(
	ctx context.Context,
	userID int64,
	deviceID int64,
	input model.ScrapDisposalInput,
) (model.ScrapDisposal, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ScrapDisposal{}, err
	}
	defer tx.Rollback()

	device, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return model.ScrapDisposal{}, err
	}
	if device.LifecycleStatus != "SCRAP_PENDING" {
		return model.ScrapDisposal{}, fmt.Errorf("device is not pending scrap disposal")
	}

	var scrapWarehouseID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM inv_warehouses
		WHERE code='SCRAP_HOLD' AND status='active'
		LIMIT 1
	`).Scan(&scrapWarehouseID); err != nil {
		return model.ScrapDisposal{}, err
	}

	note := strings.TrimSpace(input.Note)
	if note == "" {
		if input.AmountCents > 0 {
			note = "报废设备回收出售"
		} else {
			note = "报废设备无偿销毁"
		}
	}
	if err := transitionShipmentDevicesTx(
		ctx,
		tx,
		userID,
		[]int64{deviceID},
		"SCRAPPED",
		&scrapWarehouseID,
		nil,
		note,
	); err != nil {
		return model.ScrapDisposal{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_devices
		SET quality_status='scrapped'
		WHERE id=?
	`, deviceID); err != nil {
		return model.ScrapDisposal{}, err
	}

	disposalNo := nextInventoryNo("SCRAP")
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_scrap_disposals (
			disposal_no, device_id, amount_cents, buyer_name,
			payment_method, note, operator_user_id, disposed_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		disposalNo,
		deviceID,
		input.AmountCents,
		strings.TrimSpace(input.BuyerName),
		strings.TrimSpace(input.PaymentMethod),
		note,
		userID,
		now,
	)
	if err != nil {
		return model.ScrapDisposal{}, err
	}
	disposalID, err := result.LastInsertId()
	if err != nil {
		return model.ScrapDisposal{}, err
	}
	if err := insertOperatingEntryTx(
		ctx,
		tx,
		"income",
		"scrap_disposal",
		input.AmountCents,
		"scrap_disposal",
		&disposalID,
		disposalNo,
		"scrap_disposal:"+fmt.Sprint(deviceID),
		input.BuyerName,
		input.PaymentMethod,
		fmt.Sprintf("报废处置 · SN %s · %s", device.SN, note),
		userID,
		now,
	); err != nil {
		return model.ScrapDisposal{}, err
	}
	if err := s.commitInboxTx(ctx, tx, "inventory"); err != nil {
		return model.ScrapDisposal{}, err
	}
	return s.getScrapDisposal(ctx, disposalID)
}

func (s *Store) getScrapDisposal(
	ctx context.Context,
	disposalID int64,
) (model.ScrapDisposal, error) {
	var item model.ScrapDisposal
	err := s.db.QueryRowContext(ctx, `
		SELECT d.id, d.disposal_no, d.device_id, v.sn,
		       d.amount_cents, d.buyer_name, d.payment_method,
		       d.note, d.operator_user_id, d.disposed_at, d.created_at
		FROM inv_scrap_disposals d
		INNER JOIN inv_devices v ON v.id=d.device_id
		WHERE d.id=?
	`, disposalID).Scan(
		&item.ID,
		&item.DisposalNo,
		&item.DeviceID,
		&item.DeviceSN,
		&item.AmountCents,
		&item.BuyerName,
		&item.PaymentMethod,
		&item.Note,
		&item.OperatorUserID,
		&item.DisposedAt,
		&item.CreatedAt,
	)
	return item, err
}

func (s *Store) ListRMAs(
	ctx context.Context,
	page int,
	pageSize int,
) ([]model.RMARecord, int, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var total int
	var openTotal int
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status NOT IN ('COMPLETED', 'CANCELLED') THEN 1 ELSE 0 END), 0)
		FROM inv_rmas
	`).Scan(&total, &openTotal); err != nil {
		return nil, 0, 0, err
	}

	offset := (page - 1) * pageSize
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.id, r.rma_no, r.device_id, d.sn, r.service_type,
		       r.status, r.source_type, r.source_user_id, r.source_tenant_id,
		       r.source_agent_org_id, r.customer_name, r.contact_phone,
		       r.issue, r.resolution, r.replacement_device_id,
		       l.source_order_id, COALESCE(l.source_order_no, ''),
		       l.source_shipment_id, COALESCE(l.source_shipment_no, ''),
		       l.return_shipment_id, COALESCE(l.return_shipment_no, ''),
		       l.outbound_shipment_id, COALESCE(l.outbound_shipment_no, ''),
		       l.repair_outbound_shipment_id, COALESCE(l.repair_outbound_shipment_no, ''),
		       l.repair_return_shipment_id, COALESCE(l.repair_return_shipment_no, ''),
		       l.refund_id, COALESCE(l.refund_no, ''),
		       r.operator_user_id, r.accepted_by_user_id, r.accepted_at,
		       r.completed_at, r.created_at, r.updated_at
		FROM inv_rmas r
		INNER JOIN inv_devices d ON d.id=r.device_id
		LEFT JOIN inv_rma_links l ON l.rma_id=r.id
		ORDER BY r.id DESC
		LIMIT ? OFFSET ?
	`, pageSize, offset)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	items := make([]model.RMARecord, 0)
	for rows.Next() {
		var item model.RMARecord
		if err := rows.Scan(
			&item.ID,
			&item.RMANo,
			&item.DeviceID,
			&item.DeviceSN,
			&item.ServiceType,
			&item.Status,
			&item.SourceType,
			&item.SourceUserID,
			&item.SourceTenantID,
			&item.SourceAgentOrgID,
			&item.CustomerName,
			&item.ContactPhone,
			&item.Issue,
			&item.Resolution,
			&item.ReplacementDeviceID,
			&item.SourceOrderID,
			&item.SourceOrderNo,
			&item.SourceShipmentID,
			&item.SourceShipmentNo,
			&item.ReturnShipmentID,
			&item.ReturnShipmentNo,
			&item.OutboundShipmentID,
			&item.OutboundShipmentNo,
			&item.RepairOutboundShipmentID,
			&item.RepairOutboundShipmentNo,
			&item.RepairReturnShipmentID,
			&item.RepairReturnShipmentNo,
			&item.RefundID,
			&item.RefundNo,
			&item.OperatorUserID,
			&item.AcceptedByUserID,
			&item.AcceptedAt,
			&item.CompletedAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, 0, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}
	return items, total, openTotal, nil
}

func (s *Store) CreateRMA(
	ctx context.Context,
	userID int64,
	input model.CreateRMAInput,
) (model.RMARecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RMARecord{}, err
	}
	defer tx.Rollback()

	device, err := lockDeviceTx(ctx, tx, input.DeviceID)
	if err != nil {
		return model.RMARecord{}, err
	}
	if !rmaCanOpen(device.LifecycleStatus) {
		return model.RMARecord{}, fmt.Errorf(
			"device status %s cannot open RMA",
			device.LifecycleStatus,
		)
	}
	var activeCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inv_rmas
		WHERE device_id=? AND status NOT IN ('COMPLETED', 'CANCELLED')
	`, input.DeviceID).Scan(&activeCount); err != nil {
		return model.RMARecord{}, err
	}
	if activeCount > 0 {
		return model.RMARecord{}, fmt.Errorf("device already has active RMA")
	}

	sourceType := strings.ToLower(strings.TrimSpace(input.SourceType))
	if sourceType == "" {
		sourceType = "staff_manual"
	}
	sourceTenantID := input.SourceTenantID
	if sourceTenantID == nil && device.CurrentCustomerID != nil {
		value := *device.CurrentCustomerID
		sourceTenantID = &value
	}
	sourceAgentOrgID := input.SourceAgentOrgID
	if sourceAgentOrgID == nil && sourceTenantID != nil {
		var agentOrgID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT agent_org_id
			FROM crm_customer_agent_relations
			WHERE tenant_id=? AND status='active'
			ORDER BY id DESC
			LIMIT 1
		`, *sourceTenantID).Scan(&agentOrgID); err == nil {
			sourceAgentOrgID = &agentOrgID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return model.RMARecord{}, err
		}
	}
	if sourceAgentOrgID == nil && device.OwnerOrgID != nil {
		var agentOrgID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM crm_agent_orgs
			WHERE id=? OR mgmt_tenant_id=?
			ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END
			LIMIT 1
		`, *device.OwnerOrgID, *device.OwnerOrgID, *device.OwnerOrgID).Scan(&agentOrgID); err == nil {
			sourceAgentOrgID = &agentOrgID
		} else if !errors.Is(err, sql.ErrNoRows) {
			return model.RMARecord{}, err
		}
	}
	rmaNo := nextInventoryNo("RMA")
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_rmas (
			rma_no, device_id, service_type, status,
			source_type, source_user_id, source_tenant_id, source_agent_org_id,
			customer_name, contact_phone, issue, operator_user_id
		)
		VALUES (?, ?, ?, 'SUBMITTED', ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rmaNo,
		input.DeviceID,
		strings.ToLower(strings.TrimSpace(input.ServiceType)),
		sourceType,
		input.SourceUserID,
		sourceTenantID,
		sourceAgentOrgID,
		strings.TrimSpace(input.CustomerName),
		strings.TrimSpace(input.ContactPhone),
		strings.TrimSpace(input.Issue),
		userID,
	)
	if err != nil {
		return model.RMARecord{}, err
	}
	rmaID, err := result.LastInsertId()
	if err != nil {
		return model.RMARecord{}, err
	}
	if err := insertRMAEventTx(
		ctx, tx, rmaID, "submitted", "SUBMITTED",
		"售后申请已提交", strings.TrimSpace(input.Issue), true, userID,
	); err != nil {
		return model.RMARecord{}, err
	}

	if err := s.commitInboxTx(ctx, tx, "inventory", "logistics"); err != nil {
		return model.RMARecord{}, err
	}
	return s.getRMA(ctx, rmaID)
}

func (s *Store) CompleteRMA(
	ctx context.Context,
	userID int64,
	rmaID int64,
	input model.CompleteRMAInput,
) (model.RMARecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RMARecord{}, err
	}
	defer tx.Rollback()

	var deviceID int64
	var rmaNo string
	var serviceType string
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT device_id, rma_no, service_type, status
		FROM inv_rmas
		WHERE id=?
		FOR UPDATE
	`, rmaID).Scan(&deviceID, &rmaNo, &serviceType, &status); err != nil {
		return model.RMARecord{}, err
	}
	if status == "COMPLETED" || status == "CANCELLED" {
		return model.RMARecord{}, fmt.Errorf("RMA already closed")
	}

	device, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return model.RMARecord{}, err
	}
	toStatus := strings.ToUpper(strings.TrimSpace(input.ToStatus))
	allowedTargets := map[string]bool{
		"ACTIVE":        true,
		"IN_STOCK":      true,
		"SCRAP_PENDING": true,
		"REPLACED":      true,
		"AFTER_SALES":   true,
	}
	if !allowedTargets[toStatus] {
		return model.RMARecord{}, fmt.Errorf("invalid RMA result status")
	}

	toWarehouseID := device.CustodyWarehouseID
	if input.ToWarehouseID != nil {
		if err := ensureWarehouseActiveTx(ctx, tx, *input.ToWarehouseID); err != nil {
			return model.RMARecord{}, err
		}
		toWarehouseID = input.ToWarehouseID
	}
	if toStatus == "SCRAP_PENDING" {
		var scrapWarehouseID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM inv_warehouses
			WHERE code='SCRAP_HOLD' AND status='active'
			LIMIT 1
		`).Scan(&scrapWarehouseID); err != nil {
			return model.RMARecord{}, fmt.Errorf("scrap holding warehouse is unavailable: %w", err)
		}
		toWarehouseID = &scrapWarehouseID
	}

	rmaStatus, completed, err := completeRMAFulfillmentTx(
		ctx,
		tx,
		userID,
		rmaID,
		rmaNo,
		serviceType,
		device,
		toStatus,
		toWarehouseID,
		input.ReplacementDeviceID,
		input.Resolution,
	)
	if err != nil {
		return model.RMARecord{}, err
	}

	now := time.Now().UTC()
	var completedAt any
	if completed {
		completedAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_rmas
		SET status=?, resolution=?, replacement_device_id=?,
		    operator_user_id=?, completed_at=?
		WHERE id=?
	`,
		rmaStatus,
		strings.TrimSpace(input.Resolution),
		input.ReplacementDeviceID,
		userID,
		completedAt,
		rmaID,
	); err != nil {
		return model.RMARecord{}, err
	}

	eventCode := "processing_update"
	eventTitle := "售后状态已更新"
	if completed {
		eventCode = "completed"
		eventTitle = "售后处理已完成"
	}
	if err := insertRMAEventTx(
		ctx, tx, rmaID, eventCode, rmaStatus, eventTitle,
		strings.TrimSpace(input.Resolution), true, userID,
	); err != nil {
		return model.RMARecord{}, err
	}

	if err := s.commitInboxTx(ctx, tx, "inventory", "logistics"); err != nil {
		return model.RMARecord{}, err
	}
	return s.getRMA(ctx, rmaID)
}

func (s *Store) getRMA(ctx context.Context, rmaID int64) (model.RMARecord, error) {
	var item model.RMARecord
	err := s.db.QueryRowContext(ctx, `
		SELECT r.id, r.rma_no, r.device_id, d.sn, r.service_type,
		       r.status, r.source_type, r.source_user_id, r.source_tenant_id,
		       r.source_agent_org_id, r.customer_name, r.contact_phone,
		       r.issue, r.resolution, r.replacement_device_id,
		       l.source_order_id, COALESCE(l.source_order_no, ''),
		       l.source_shipment_id, COALESCE(l.source_shipment_no, ''),
		       l.return_shipment_id, COALESCE(l.return_shipment_no, ''),
		       l.outbound_shipment_id, COALESCE(l.outbound_shipment_no, ''),
		       l.repair_outbound_shipment_id, COALESCE(l.repair_outbound_shipment_no, ''),
		       l.repair_return_shipment_id, COALESCE(l.repair_return_shipment_no, ''),
		       l.refund_id, COALESCE(l.refund_no, ''),
		       r.operator_user_id, r.accepted_by_user_id, r.accepted_at,
		       r.completed_at, r.created_at, r.updated_at
		FROM inv_rmas r
		INNER JOIN inv_devices d ON d.id=r.device_id
		LEFT JOIN inv_rma_links l ON l.rma_id=r.id
		WHERE r.id=?
	`, rmaID).Scan(
		&item.ID,
		&item.RMANo,
		&item.DeviceID,
		&item.DeviceSN,
		&item.ServiceType,
		&item.Status,
		&item.SourceType,
		&item.SourceUserID,
		&item.SourceTenantID,
		&item.SourceAgentOrgID,
		&item.CustomerName,
		&item.ContactPhone,
		&item.Issue,
		&item.Resolution,
		&item.ReplacementDeviceID,
		&item.SourceOrderID,
		&item.SourceOrderNo,
		&item.SourceShipmentID,
		&item.SourceShipmentNo,
		&item.ReturnShipmentID,
		&item.ReturnShipmentNo,
		&item.OutboundShipmentID,
		&item.OutboundShipmentNo,
		&item.RepairOutboundShipmentID,
		&item.RepairOutboundShipmentNo,
		&item.RepairReturnShipmentID,
		&item.RepairReturnShipmentNo,
		&item.RefundID,
		&item.RefundNo,
		&item.OperatorUserID,
		&item.AcceptedByUserID,
		&item.AcceptedAt,
		&item.CompletedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func lockDeviceTx(ctx context.Context, tx *sql.Tx, deviceID int64) (model.Device, error) {
	var item model.Device
	err := tx.QueryRowContext(ctx, `
		SELECT id, sn, sku_code, batch_no, owner_org_id,
		       custody_warehouse_id, current_customer_id,
		       lifecycle_status, quality_status,
		       created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM inv_devices
		WHERE id=?
		FOR UPDATE
	`, deviceID).Scan(
		&item.ID,
		&item.SN,
		&item.SKUCode,
		&item.BatchNo,
		&item.OwnerOrgID,
		&item.CustodyWarehouseID,
		&item.CurrentCustomerID,
		&item.LifecycleStatus,
		&item.QualityStatus,
		&item.CreatedByUserID,
		&item.UpdatedByUserID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func ensureWarehouseActiveTx(ctx context.Context, tx *sql.Tx, warehouseID int64) error {
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT status FROM inv_warehouses WHERE id=?
	`, warehouseID).Scan(&status); err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("warehouse is not active")
	}
	return nil
}

func createStockDocumentTx(
	ctx context.Context,
	tx *sql.Tx,
	documentType string,
	fromWarehouseID *int64,
	toWarehouseID *int64,
	counterpartyOrgID *int64,
	reason string,
	userID int64,
) (int64, error) {
	documentNo := nextInventoryNo(documentPrefix(documentType))
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_stock_documents (
			document_no, document_type, status, from_warehouse_id,
			to_warehouse_id, counterparty_org_id, reason,
			operator_user_id, approved_by_user_id, effective_at
		)
		VALUES (?, ?, 'effective', ?, ?, ?, ?, ?, ?, ?)
	`,
		documentNo,
		documentType,
		fromWarehouseID,
		toWarehouseID,
		counterpartyOrgID,
		reason,
		userID,
		userID,
		now,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func insertStockItemAndLedgerTx(
	ctx context.Context,
	tx *sql.Tx,
	deviceID int64,
	documentID int64,
	sn string,
	sku string,
	action string,
	fromStatus string,
	toStatus string,
	fromWarehouseID *int64,
	toWarehouseID *int64,
	fromOwnerOrgID *int64,
	toOwnerOrgID *int64,
	fromCustomerID *int64,
	toCustomerID *int64,
	userID int64,
	reason string,
) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_stock_document_items (
			document_id, device_id, sn_snapshot, sku_snapshot,
			from_status, to_status
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`, documentID, deviceID, sn, sku, fromStatus, toStatus); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_device_ledger (
			device_id, document_id, action, from_status, to_status,
			from_warehouse_id, to_warehouse_id,
			from_owner_org_id, to_owner_org_id,
			from_customer_id, to_customer_id,
			operator_user_id, reason
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		deviceID,
		documentID,
		action,
		fromStatus,
		toStatus,
		fromWarehouseID,
		toWarehouseID,
		fromOwnerOrgID,
		toOwnerOrgID,
		fromCustomerID,
		toCustomerID,
		userID,
		reason,
	); err != nil {
		return err
	}
	return nil
}

func nextInventoryNo(prefix string) string {
	return fmt.Sprintf(
		"%s-%d",
		strings.ToUpper(prefix),
		time.Now().UTC().UnixNano(),
	)
}

func documentPrefix(documentType string) string {
	switch documentType {
	case "inbound":
		return "IN"
	case "reserve":
		return "RSV"
	case "unreserve":
		return "URS"
	case "outbound":
		return "OUT"
	case "transfer":
		return "TRF"
	case "customer_bind":
		return "BND"
	case "activate":
		return "ACT"
	case "rma_open":
		return "RMA"
	case "rma_complete":
		return "RMC"
	case "scrap":
		return "SCR"
	default:
		return "ADJ"
	}
}

func actionForDeviceTransition(fromStatus string, toStatus string) string {
	switch toStatus {
	case "RESERVED":
		return "reserve"
	case "IN_TRANSIT":
		return "outbound"
	case "AGENT_STOCK":
		return "agent_inbound"
	case "SOLD":
		return "sale"
	case "CUSTOMER_BOUND":
		return "customer_bind"
	case "ACTIVE":
		return "activate"
	case "RMA_TRANSIT", "AFTER_SALES", "REPAIRING", "REPLACED":
		return "after_sales"
	case "REPAIR_TRANSIT", "EXTERNAL_REPAIR", "REPAIR_RETURN_TRANSIT":
		return "repair"
	case "SCRAP_PENDING", "SCRAPPED":
		return "scrap"
	case "IN_STOCK":
		if fromStatus == "RESERVED" {
			return "unreserve"
		}
		return "inbound"
	default:
		return "adjust"
	}
}

func deviceTransitionAllowed(fromStatus string, toStatus string) bool {
	if fromStatus == toStatus {
		return false
	}
	transitions := map[string]map[string]bool{
		"INBOUND_PENDING": {"IN_STOCK": true},
		"IN_STOCK": {
			"RESERVED":      true,
			"IN_TRANSIT":    true,
			"AFTER_SALES":   true,
			"SCRAP_PENDING": true,
		},
		"RESERVED": {
			"IN_STOCK":    true,
			"IN_TRANSIT":  true,
			"SOLD":        true,
			"AGENT_STOCK": true,
		},
		"IN_TRANSIT": {
			"AGENT_STOCK": true,
			"SOLD":        true,
			"IN_STOCK":    true,
		},
		"AGENT_STOCK": {
			"SOLD":        true,
			"IN_TRANSIT":  true,
			"AFTER_SALES": true,
		},
		"SOLD": {
			"CUSTOMER_BOUND": true,
			"RMA_TRANSIT":    true,
			"AFTER_SALES":    true,
		},
		"CUSTOMER_BOUND": {
			"ACTIVE":      true,
			"RMA_TRANSIT": true,
			"AFTER_SALES": true,
		},
		"ACTIVE": {
			"RMA_TRANSIT": true,
			"AFTER_SALES": true,
		},
		"RMA_TRANSIT": {
			"AFTER_SALES":    true,
			"ACTIVE":         true,
			"CUSTOMER_BOUND": true,
			"SOLD":           true,
		},
		"AFTER_SALES": {
			"REPAIRING":     true,
			"REPLACED":      true,
			"SCRAP_PENDING": true,
			"IN_STOCK":      true,
			"ACTIVE":        true,
		},
		"REPAIRING": {
			"ACTIVE":          true,
			"IN_STOCK":        true,
			"REPLACED":        true,
			"SCRAP_PENDING":   true,
			"REPAIR_TRANSIT":  true,
			"EXTERNAL_REPAIR": true,
		},
		"REPAIR_TRANSIT": {
			"EXTERNAL_REPAIR": true,
			"REPAIRING":       true,
		},
		"EXTERNAL_REPAIR": {
			"REPAIR_RETURN_TRANSIT": true,
			"REPAIRING":             true,
		},
		"REPAIR_RETURN_TRANSIT": {
			"REPAIRING": true,
		},
		"SCRAP_PENDING": {
			"SCRAPPED": true,
		},
		"REPLACED": {
			"SCRAP_PENDING": true,
			"AFTER_SALES":   true,
		},
	}
	return transitions[fromStatus][toStatus]
}

func rmaCanOpen(status string) bool {
	switch status {
	case "ACTIVE", "CUSTOMER_BOUND", "SOLD", "AGENT_STOCK", "IN_STOCK":
		return true
	default:
		return false
	}
}

func isDuplicateInventoryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "error 1062")
}
