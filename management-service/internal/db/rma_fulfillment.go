package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

type rmaSourceSnapshot struct {
	OrderID             *int64
	OrderNo             string
	ShipmentID          *int64
	ShipmentNo          string
	ReturnWarehouseID   *int64
	ReturnWarehouseName string
	CustomerID          *int64
}

func resolveRMASourceTx(
	ctx context.Context,
	tx *sql.Tx,
	device model.Device,
) (rmaSourceSnapshot, error) {
	var result rmaSourceSnapshot
	var orderID sql.NullInt64
	var orderNo sql.NullString
	var shipmentID sql.NullInt64
	var shipmentNo sql.NullString
	var warehouseID sql.NullInt64
	var customerID sql.NullInt64

	err := tx.QueryRowContext(ctx, `
		SELECT o.id, o.order_no, od.shipment_id,
		       COALESCE(sh.shipment_no, ''),
		       sh.from_warehouse_id,
		       o.tenant_id
		FROM biz_order_devices od
		INNER JOIN biz_orders o ON o.id=od.order_id
		LEFT JOIN inv_shipments sh ON sh.id=od.shipment_id
		WHERE od.device_id=?
		ORDER BY od.id DESC
		LIMIT 1
	`, device.ID).Scan(
		&orderID,
		&orderNo,
		&shipmentID,
		&shipmentNo,
		&warehouseID,
		&customerID,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}

	if orderID.Valid {
		value := orderID.Int64
		result.OrderID = &value
	}
	if orderNo.Valid {
		result.OrderNo = orderNo.String
	}
	if shipmentID.Valid {
		value := shipmentID.Int64
		result.ShipmentID = &value
	}
	if shipmentNo.Valid {
		result.ShipmentNo = shipmentNo.String
	}
	if warehouseID.Valid {
		value := warehouseID.Int64
		result.ReturnWarehouseID = &value
	}
	if customerID.Valid {
		value := customerID.Int64
		result.CustomerID = &value
	} else if device.CurrentCustomerID != nil {
		result.CustomerID = device.CurrentCustomerID
	}

	if result.ReturnWarehouseID == nil && device.CustodyWarehouseID != nil {
		result.ReturnWarehouseID = device.CustodyWarehouseID
	}
	if result.ReturnWarehouseID == nil {
		var fallbackID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM inv_warehouses
			WHERE status='active'
			ORDER BY id ASC
			LIMIT 1
		`).Scan(&fallbackID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if err == nil {
			result.ReturnWarehouseID = &fallbackID
		}
	}
	if result.ReturnWarehouseID != nil {
		_ = tx.QueryRowContext(ctx, `
			SELECT name
			FROM inv_warehouses
			WHERE id=?
		`, *result.ReturnWarehouseID).Scan(&result.ReturnWarehouseName)
	}
	if strings.TrimSpace(result.ReturnWarehouseName) == "" {
		result.ReturnWarehouseName = "售后中心"
	}

	return result, nil
}

func createRMAFulfillmentTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	rmaID int64,
	rmaNo string,
	device model.Device,
	issue string,
) error {
	source, err := resolveRMASourceTx(ctx, tx, device)
	if err != nil {
		return err
	}

	var afterSalesWarehouseID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM inv_warehouses
		WHERE code='AFTER_SALES_PENDING' AND status='active'
		LIMIT 1
	`).Scan(&afterSalesWarehouseID); err != nil {
		return fmt.Errorf("after-sales pending warehouse is unavailable: %w", err)
	}

	customerSide := map[string]bool{
		"SOLD":           true,
		"CUSTOMER_BOUND": true,
		"ACTIVE":         true,
		"AGENT_STOCK":    true,
	}

	if customerSide[device.LifecycleStatus] {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inv_rmas
			SET status='RETURN_PENDING'
			WHERE id=?
		`, rmaID); err != nil {
			return err
		}

		docID, err := createStockDocumentTx(
			ctx,
			tx,
			"rma_open",
			device.CustodyWarehouseID,
			device.CustodyWarehouseID,
			device.OwnerOrgID,
			"售后单 "+rmaNo+" 已创建，等待登记真实退回物流或现场送达",
			userID,
		)
		if err != nil {
			return err
		}
		if err := insertStockItemAndLedgerTx(
			ctx,
			tx,
			device.ID,
			docID,
			device.SN,
			device.SKUCode,
			"rma_open",
			device.LifecycleStatus,
			device.LifecycleStatus,
			device.CustodyWarehouseID,
			device.CustodyWarehouseID,
			device.OwnerOrgID,
			device.OwnerOrgID,
			device.CurrentCustomerID,
			device.CurrentCustomerID,
			userID,
			"售后单 "+rmaNo+" · 等待寄回/送达售后待检库 · "+strings.TrimSpace(issue),
		); err != nil {
			return err
		}
	} else {
		if device.LifecycleStatus != "AFTER_SALES" {
			if err := transitionShipmentDevicesTx(
				ctx,
				tx,
				userID,
				[]int64{device.ID},
				"AFTER_SALES",
				&afterSalesWarehouseID,
				nil,
				"售后单 "+rmaNo+" · 设备进入售后待检库",
			); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE inv_rmas
			SET status='PROCESSING'
			WHERE id=?
		`, rmaID); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_rma_links (
			rma_id,
			source_order_id, source_order_no,
			source_shipment_id, source_shipment_no,
			source_device_status,
			return_shipment_id, return_shipment_no
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rmaID,
		source.OrderID,
		source.OrderNo,
		source.ShipmentID,
		source.ShipmentNo,
		device.LifecycleStatus,
		nil,
		"",
	); err != nil {
		return err
	}

	return nil
}

func restoreFailedRMAReturnTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	rmaID int64,
	deviceIDs []int64,
	reason string,
) error {
	var (
		sourceStatus  string
		sourceOrderID sql.NullInt64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT source_device_status, source_order_id
		FROM inv_rma_links
		WHERE rma_id=?
	`, rmaID).Scan(&sourceStatus, &sourceOrderID); err != nil {
		return err
	}

	switch sourceStatus {
	case "ACTIVE", "CUSTOMER_BOUND", "SOLD":
	default:
		sourceStatus = "CUSTOMER_BOUND"
	}

	var customerID *int64
	if sourceOrderID.Valid {
		var tenantID int64
		if err := tx.QueryRowContext(ctx, `
			SELECT tenant_id
			FROM biz_orders
			WHERE id=?
		`, sourceOrderID.Int64).Scan(&tenantID); err == nil {
			customerID = &tenantID
		}
	}

	return transitionShipmentDevicesTx(
		ctx,
		tx,
		userID,
		deviceIDs,
		sourceStatus,
		nil,
		customerID,
		reason,
	)
}

func rmaSourceOrderExistsTx(
	ctx context.Context,
	tx *sql.Tx,
	rmaID int64,
) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inv_rma_links
		WHERE rma_id=? AND source_order_id IS NOT NULL
	`, rmaID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func completeRMAFulfillmentTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	rmaID int64,
	rmaNo string,
	serviceType string,
	device model.Device,
	toStatus string,
	toWarehouseID *int64,
	replacementDeviceID *int64,
	resolution string,
) (string, bool, error) {
	serviceType = strings.ToLower(strings.TrimSpace(serviceType))
	toStatus = strings.ToUpper(strings.TrimSpace(toStatus))
	hasSourceOrder, err := rmaSourceOrderExistsTx(ctx, tx, rmaID)
	if err != nil {
		return "", false, err
	}

	if serviceType == "return" {
		switch toStatus {
		case "IN_STOCK", "SCRAP_PENDING", "AFTER_SALES":
		default:
			return "", false, fmt.Errorf("return RMA must finish in stock, after-sales or scrap pending")
		}

		targetWarehouseID := toWarehouseID
		if targetWarehouseID == nil {
			targetWarehouseID = device.CustodyWarehouseID
		}
		if targetWarehouseID == nil {
			return "", false, fmt.Errorf("return RMA requires warehouse")
		}
		if device.LifecycleStatus != toStatus {
			if !deviceTransitionAllowed(device.LifecycleStatus, toStatus) {
				return "", false, fmt.Errorf(
					"device status %s cannot complete return to %s",
					device.LifecycleStatus,
					toStatus,
				)
			}
			if err := transitionShipmentDevicesTx(
				ctx,
				tx,
				userID,
				[]int64{device.ID},
				toStatus,
				targetWarehouseID,
				nil,
				"退货验收完成 · "+rmaNo+" · "+strings.TrimSpace(resolution),
			); err != nil {
				return "", false, err
			}
		}

		if hasSourceOrder {
			if _, _, err := createDeviceOrderRefundRequestTx(
				ctx,
				tx,
				userID,
				rmaID,
				rmaNo,
			); err != nil {
				return "", false, err
			}
			return "REFUND_PENDING", false, nil
		}
		return "COMPLETED", true, nil
	}

	if serviceType == "exchange" {
		if replacementDeviceID == nil {
			return "", false, fmt.Errorf("exchange requires replacement device")
		}
		replacement, err := lockDeviceTx(ctx, tx, *replacementDeviceID)
		if err != nil {
			return "", false, err
		}
		if replacement.LifecycleStatus != "IN_STOCK" {
			return "", false, fmt.Errorf("replacement device must be in stock")
		}
		if replacement.ID == device.ID {
			return "", false, fmt.Errorf("replacement device cannot equal source device")
		}

		originalTarget := "REPLACED"
		if !deviceTransitionAllowed(device.LifecycleStatus, originalTarget) {
			return "", false, fmt.Errorf(
				"device status %s cannot complete exchange",
				device.LifecycleStatus,
			)
		}
		if err := transitionShipmentDevicesTx(
			ctx,
			tx,
			userID,
			[]int64{device.ID},
			originalTarget,
			device.CustodyWarehouseID,
			nil,
			"售后换机原设备处理 · "+rmaNo+" · "+strings.TrimSpace(resolution),
		); err != nil {
			return "", false, err
		}

		if hasSourceOrder {
			// Keep the replacement device in stock. The warehouse must create a
			// real exchange shipment (courier or pickup) in Logistics. That
			// shipment will bind itself back to this RMA through business_id.
			return "REPLACEMENT_PENDING", false, nil
		}
		return "COMPLETED", true, nil
	}

	if (serviceType == "repair" || serviceType == "refurbish") &&
		toStatus == "ACTIVE" &&
		hasSourceOrder {
		targetWarehouseID := toWarehouseID
		if targetWarehouseID == nil {
			targetWarehouseID = device.CustodyWarehouseID
		}
		if targetWarehouseID == nil {
			return "", false, fmt.Errorf("repair return requires service warehouse")
		}

		if device.LifecycleStatus != "IN_STOCK" {
			if !deviceTransitionAllowed(device.LifecycleStatus, "IN_STOCK") {
				return "", false, fmt.Errorf(
					"device status %s cannot return to stock after repair",
					device.LifecycleStatus,
				)
			}
			if err := transitionShipmentDevicesTx(
				ctx,
				tx,
				userID,
				[]int64{device.ID},
				"IN_STOCK",
				targetWarehouseID,
				nil,
				"售后维修完成回到可出库状态 · "+rmaNo,
			); err != nil {
				return "", false, err
			}
		}

		// The repaired device is back in a warehouse and ready for a real
		// courier/pickup shipment. Do not create a synthetic tracking number.
		return "RETURN_TO_CUSTOMER_PENDING", false, nil
	}

	targetWarehouseID := toWarehouseID
	if targetWarehouseID == nil {
		targetWarehouseID = device.CustodyWarehouseID
	}
	if toStatus == "ACTIVE" || toStatus == "CUSTOMER_BOUND" || toStatus == "SOLD" {
		targetWarehouseID = nil
	}
	if device.LifecycleStatus != toStatus {
		if !deviceTransitionAllowed(device.LifecycleStatus, toStatus) {
			return "", false, fmt.Errorf(
				"device status %s cannot complete RMA to %s",
				device.LifecycleStatus,
				toStatus,
			)
		}
		if err := transitionShipmentDevicesTx(
			ctx,
			tx,
			userID,
			[]int64{device.ID},
			toStatus,
			targetWarehouseID,
			device.CurrentCustomerID,
			"售后处理完成 · "+rmaNo+" · "+strings.TrimSpace(resolution),
		); err != nil {
			return "", false, err
		}
	}
	return "COMPLETED", true, nil
}
