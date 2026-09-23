package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type shipmentDeviceSnapshot struct {
	ID          int64
	SN          string
	SKU         string
	Status      string
	WarehouseID *int64
	OwnerOrgID  *int64
	CustomerID  *int64
}

func (s *Store) ListShipments(ctx context.Context) ([]model.Shipment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, shipment_no, shipment_type, business_type, business_id,
		       business_no, from_warehouse_id, to_warehouse_id,
		       recipient_customer_id, recipient_org_id, recipient_type, delivery_method,
		       logistics_fee_cents, recipient_name, recipient_phone,
		       recipient_address, carrier_code, carrier_name, tracking_no,
		       status, note, operator_user_id, shipped_at, delivered_at,
		       created_at, updated_at
		FROM inv_shipments
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Shipment, 0)
	for rows.Next() {
		var item model.Shipment
		if err := rows.Scan(
			&item.ID,
			&item.ShipmentNo,
			&item.ShipmentType,
			&item.BusinessType,
			&item.BusinessID,
			&item.BusinessNo,
			&item.FromWarehouseID,
			&item.ToWarehouseID,
			&item.RecipientCustomerID,
			&item.RecipientOrgID,
			&item.RecipientType,
			&item.DeliveryMethod,
			&item.LogisticsFeeCents,
			&item.RecipientName,
			&item.RecipientPhone,
			&item.RecipientAddress,
			&item.CarrierCode,
			&item.CarrierName,
			&item.TrackingNo,
			&item.Status,
			&item.Note,
			&item.OperatorUserID,
			&item.ShippedAt,
			&item.DeliveredAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		if err := s.hydrateShipment(ctx, &items[i]); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Store) GetShipment(ctx context.Context, shipmentID int64) (model.Shipment, error) {
	var item model.Shipment
	err := s.db.QueryRowContext(ctx, `
		SELECT id, shipment_no, shipment_type, business_type, business_id,
		       business_no, from_warehouse_id, to_warehouse_id,
		       recipient_customer_id, recipient_org_id, recipient_type, delivery_method,
		       logistics_fee_cents, recipient_name, recipient_phone,
		       recipient_address, carrier_code, carrier_name, tracking_no,
		       status, note, operator_user_id, shipped_at, delivered_at,
		       created_at, updated_at
		FROM inv_shipments
		WHERE id=?
	`, shipmentID).Scan(
		&item.ID,
		&item.ShipmentNo,
		&item.ShipmentType,
		&item.BusinessType,
		&item.BusinessID,
		&item.BusinessNo,
		&item.FromWarehouseID,
		&item.ToWarehouseID,
		&item.RecipientCustomerID,
		&item.RecipientOrgID,
		&item.RecipientType,
		&item.DeliveryMethod,
		&item.LogisticsFeeCents,
		&item.RecipientName,
		&item.RecipientPhone,
		&item.RecipientAddress,
		&item.CarrierCode,
		&item.CarrierName,
		&item.TrackingNo,
		&item.Status,
		&item.Note,
		&item.OperatorUserID,
		&item.ShippedAt,
		&item.DeliveredAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.Shipment{}, err
	}
	if err := s.hydrateShipment(ctx, &item); err != nil {
		return model.Shipment{}, err
	}
	return item, nil
}

func (s *Store) hydrateShipment(ctx context.Context, item *model.Shipment) error {
	itemRows, err := s.db.QueryContext(ctx, `
		SELECT id, shipment_id, device_id, sn_snapshot, sku_snapshot, created_at
		FROM inv_shipment_items
		WHERE shipment_id=?
		ORDER BY id ASC
	`, item.ID)
	if err != nil {
		return err
	}
	item.Items = make([]model.ShipmentItem, 0)
	for itemRows.Next() {
		var row model.ShipmentItem
		if err := itemRows.Scan(
			&row.ID,
			&row.ShipmentID,
			&row.DeviceID,
			&row.SN,
			&row.SKUCode,
			&row.CreatedAt,
		); err != nil {
			itemRows.Close()
			return err
		}
		item.Items = append(item.Items, row)
	}
	if err := itemRows.Err(); err != nil {
		itemRows.Close()
		return err
	}
	itemRows.Close()

	eventRows, err := s.db.QueryContext(ctx, `
		SELECT id, shipment_id, event_code, status, location, description,
		       operator_user_id, occurred_at, created_at
		FROM inv_logistics_events
		WHERE shipment_id=?
		ORDER BY occurred_at DESC, id DESC
	`, item.ID)
	if err != nil {
		return err
	}
	defer eventRows.Close()
	item.Events = make([]model.LogisticsEvent, 0)
	for eventRows.Next() {
		var event model.LogisticsEvent
		if err := eventRows.Scan(
			&event.ID,
			&event.ShipmentID,
			&event.EventCode,
			&event.Status,
			&event.Location,
			&event.Description,
			&event.OperatorUserID,
			&event.OccurredAt,
			&event.CreatedAt,
		); err != nil {
			return err
		}
		item.Events = append(item.Events, event)
	}
	return eventRows.Err()
}

func (s *Store) CreateShipment(
	ctx context.Context,
	userID int64,
	input model.CreateShipmentInput,
) (model.Shipment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Shipment{}, err
	}
	defer tx.Rollback()

	if input.FromWarehouseID != nil {
		if err := ensureWarehouseActiveTx(ctx, tx, *input.FromWarehouseID); err != nil {
			return model.Shipment{}, err
		}
	}
	if input.ToWarehouseID != nil {
		if err := ensureWarehouseActiveTx(ctx, tx, *input.ToWarehouseID); err != nil {
			return model.Shipment{}, err
		}
	}

	shipmentNo := nextInventoryNo("SHP")
	deliveryMethod := strings.ToLower(strings.TrimSpace(input.DeliveryMethod))
	if deliveryMethod == "" {
		deliveryMethod = "courier"
	}
	recipientType := strings.ToLower(strings.TrimSpace(input.RecipientType))
	if recipientType == "" {
		recipientType = "individual"
	}
	carrierCode := strings.TrimSpace(input.CarrierCode)
	carrierName := strings.TrimSpace(input.CarrierName)
	trackingNo := strings.TrimSpace(input.TrackingNo)
	var logisticsFeeCents uint64
	if input.LogisticsFeeCents != nil {
		logisticsFeeCents = *input.LogisticsFeeCents
	}
	if deliveryMethod == "pickup" {
		carrierCode = "pickup"
		carrierName = "直接领取"
		trackingNo = ""
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_shipments (
			shipment_no, shipment_type, business_type, business_id, business_no,
			from_warehouse_id, to_warehouse_id, recipient_customer_id,
			recipient_org_id, recipient_type, delivery_method, logistics_fee_cents,
			recipient_name, recipient_phone, recipient_address,
			carrier_code, carrier_name, tracking_no, status, note,
			operator_user_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)
	`,
		shipmentNo,
		strings.TrimSpace(input.ShipmentType),
		strings.TrimSpace(input.BusinessType),
		input.BusinessID,
		strings.TrimSpace(input.BusinessNo),
		input.FromWarehouseID,
		input.ToWarehouseID,
		input.RecipientCustomerID,
		input.RecipientOrgID,
		recipientType,
		deliveryMethod,
		logisticsFeeCents,
		strings.TrimSpace(input.RecipientName),
		strings.TrimSpace(input.RecipientPhone),
		strings.TrimSpace(input.RecipientAddress),
		carrierCode,
		carrierName,
		trackingNo,
		strings.TrimSpace(input.Note),
		userID,
	)
	if err != nil {
		return model.Shipment{}, err
	}
	shipmentID, err := result.LastInsertId()
	if err != nil {
		return model.Shipment{}, err
	}

	if strings.EqualFold(strings.TrimSpace(input.BusinessType), "rma") && input.BusinessID != nil {
		switch strings.ToLower(strings.TrimSpace(input.ShipmentType)) {
		case "rma_return", "return":
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_rma_links
				SET return_shipment_id=?, return_shipment_no=?
				WHERE rma_id=?
			`, shipmentID, shipmentNo, *input.BusinessID); err != nil {
				return model.Shipment{}, err
			}
		case "repair_outbound":
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_rma_links
				SET repair_outbound_shipment_id=?, repair_outbound_shipment_no=?
				WHERE rma_id=?
			`, shipmentID, shipmentNo, *input.BusinessID); err != nil {
				return model.Shipment{}, err
			}
		case "repair_return":
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_rma_links
				SET repair_return_shipment_id=?, repair_return_shipment_no=?
				WHERE rma_id=?
			`, shipmentID, shipmentNo, *input.BusinessID); err != nil {
				return model.Shipment{}, err
			}
		case "exchange", "resend", "outbound":
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_rma_links
				SET outbound_shipment_id=?, outbound_shipment_no=?
				WHERE rma_id=?
			`, shipmentID, shipmentNo, *input.BusinessID); err != nil {
				return model.Shipment{}, err
			}
		}
	}

	seen := map[int64]bool{}
	for _, deviceID := range input.DeviceIDs {
		if deviceID <= 0 || seen[deviceID] {
			continue
		}
		seen[deviceID] = true

		var snapshot shipmentDeviceSnapshot
		if err := tx.QueryRowContext(ctx, `
			SELECT id, sn, sku_code, lifecycle_status, custody_warehouse_id,
			       owner_org_id, current_customer_id
			FROM inv_devices
			WHERE id=?
			FOR UPDATE
		`, deviceID).Scan(
			&snapshot.ID,
			&snapshot.SN,
			&snapshot.SKU,
			&snapshot.Status,
			&snapshot.WarehouseID,
			&snapshot.OwnerOrgID,
			&snapshot.CustomerID,
		); err != nil {
			return model.Shipment{}, err
		}

		if input.FromWarehouseID != nil &&
			snapshot.WarehouseID != nil &&
			*snapshot.WarehouseID != *input.FromWarehouseID {
			return model.Shipment{}, fmt.Errorf("device %s is not in selected source warehouse", snapshot.SN)
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_shipment_items (
				shipment_id, device_id, sn_snapshot, sku_snapshot
			)
			VALUES (?, ?, ?, ?)
		`, shipmentID, snapshot.ID, snapshot.SN, snapshot.SKU); err != nil {
			return model.Shipment{}, err
		}
	}
	if len(seen) == 0 {
		return model.Shipment{}, fmt.Errorf("shipment requires at least one device")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_logistics_events (
			shipment_id, event_code, status, location, description,
			operator_user_id, occurred_at
		)
		VALUES (?, 'created', 'pending', '', ?, ?, ?)
	`,
		shipmentID,
		"物流单创建 · "+shipmentNo,
		userID,
		time.Now().UTC(),
	); err != nil {
		return model.Shipment{}, err
	}

	if err := insertOperatingEntryTx(
		ctx,
		tx,
		"expense",
		"logistics",
		logisticsFeeCents,
		"shipment",
		&shipmentID,
		shipmentNo,
		"logistics:"+fmt.Sprint(shipmentID),
		carrierName,
		"",
		"物流费用 · "+shipmentNo,
		userID,
		time.Now().UTC(),
	); err != nil {
		return model.Shipment{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.Shipment{}, err
	}
	return s.GetShipment(ctx, shipmentID)
}

func (s *Store) UpdateShipmentStatus(
	ctx context.Context,
	userID int64,
	shipmentID int64,
	input model.ShipmentStatusInput,
) (model.Shipment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Shipment{}, err
	}
	defer tx.Rollback()

	var shipmentNo string
	var shipmentType string
	var currentStatus string
	var fromWarehouseID *int64
	var toWarehouseID *int64
	var recipientCustomerID *int64
	var recipientOrgID *int64
	var recipientType string
	var deliveryMethod string
	var businessType string
	var businessID *int64
	if err := tx.QueryRowContext(ctx, `
		SELECT shipment_no, shipment_type, status, from_warehouse_id,
		       to_warehouse_id, recipient_customer_id, recipient_org_id,
		       recipient_type, delivery_method, business_type, business_id
		FROM inv_shipments
		WHERE id=?
		FOR UPDATE
	`, shipmentID).Scan(
		&shipmentNo,
		&shipmentType,
		&currentStatus,
		&fromWarehouseID,
		&toWarehouseID,
		&recipientCustomerID,
		&recipientOrgID,
		&recipientType,
		&deliveryMethod,
		&businessType,
		&businessID,
	); err != nil {
		return model.Shipment{}, err
	}

	targetStatus := strings.TrimSpace(input.Status)
	if deliveryMethod == "pickup" && (targetStatus == "shipped" || targetStatus == "in_transit") {
		return model.Shipment{}, fmt.Errorf("pickup shipment cannot enter transit status")
	}
	if !shipmentStatusAllowed(currentStatus, targetStatus) {
		return model.Shipment{}, fmt.Errorf(
			"invalid shipment transition: %s -> %s",
			currentStatus,
			targetStatus,
		)
	}

	deviceIDs, err := shipmentDeviceIDsTx(ctx, tx, shipmentID)
	if err != nil {
		return model.Shipment{}, err
	}

	reason := "物流单 " + shipmentNo + " · " + strings.TrimSpace(input.Description)
	switch targetStatus {
	case "ready_to_ship":
		if shipmentType != "return" && shipmentType != "rma_return" &&
			shipmentType != "repair_outbound" && shipmentType != "repair_return" {
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "RESERVED", nil, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		}
	case "shipped":
		deviceTarget := "IN_TRANSIT"
		if shipmentType == "return" || shipmentType == "rma_return" {
			deviceTarget = "RMA_TRANSIT"
		} else if shipmentType == "repair_outbound" {
			deviceTarget = "REPAIR_TRANSIT"
		} else if shipmentType == "repair_return" {
			deviceTarget = "REPAIR_RETURN_TRANSIT"
		}
		if err := transitionShipmentDevicesTx(
			ctx, tx, userID, deviceIDs, deviceTarget, nil, recipientCustomerID, reason,
		); err != nil {
			return model.Shipment{}, err
		}
	case "delivered":
		switch shipmentType {
		case "return", "rma_return":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "AFTER_SALES", toWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		case "transfer":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "IN_STOCK", toWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		case "repair_outbound":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "EXTERNAL_REPAIR", nil, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		case "repair_return":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "REPAIRING", toWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		default:
			if recipientType == "agent" && recipientOrgID != nil {
				if err := transitionShipmentDevicesToAgentTx(
					ctx, tx, userID, deviceIDs, *recipientOrgID, reason,
				); err != nil {
					return model.Shipment{}, err
				}
			} else {
				if err := transitionShipmentDevicesTx(
					ctx, tx, userID, deviceIDs, "SOLD", nil, recipientCustomerID, reason,
				); err != nil {
					return model.Shipment{}, err
				}
				if recipientCustomerID != nil {
					if err := transitionShipmentDevicesTx(
						ctx, tx, userID, deviceIDs, "CUSTOMER_BOUND", nil, recipientCustomerID, reason,
					); err != nil {
						return model.Shipment{}, err
					}
					if businessType == "rma" {
						if err := transitionShipmentDevicesTx(
							ctx, tx, userID, deviceIDs, "ACTIVE", nil, recipientCustomerID, reason,
						); err != nil {
							return model.Shipment{}, err
						}
					}
				}
			}
		}
	case "returned":
		switch shipmentType {
		case "return", "rma_return":
			if businessType == "rma" && businessID != nil {
				if err := restoreFailedRMAReturnTx(
					ctx, tx, userID, *businessID, deviceIDs, reason,
				); err != nil {
					return model.Shipment{}, err
				}
			} else {
				if err := transitionShipmentDevicesTx(
					ctx, tx, userID, deviceIDs, "AFTER_SALES", toWarehouseID, nil, reason,
				); err != nil {
					return model.Shipment{}, err
				}
			}
		case "repair_outbound":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "REPAIRING", fromWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		case "repair_return":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "EXTERNAL_REPAIR", nil, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		default:
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "IN_STOCK", fromWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		}
	case "cancelled":
		switch shipmentType {
		case "return", "rma_return":
			// Return cancellation is restored by the RMA/order workflow.
		case "repair_outbound":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "REPAIRING", fromWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		case "repair_return":
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "EXTERNAL_REPAIR", nil, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		default:
			if err := transitionShipmentDevicesTx(
				ctx, tx, userID, deviceIDs, "IN_STOCK", fromWarehouseID, nil, reason,
			); err != nil {
				return model.Shipment{}, err
			}
		}
	}

	now := time.Now().UTC()
	var shippedAt any
	var deliveredAt any
	if targetStatus == "shipped" {
		shippedAt = now
	}
	if targetStatus == "delivered" {
		deliveredAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_shipments
		SET status=?,
		    shipped_at=COALESCE(?, shipped_at),
		    delivered_at=COALESCE(?, delivered_at),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, targetStatus, shippedAt, deliveredAt, shipmentID); err != nil {
		return model.Shipment{}, err
	}

	if businessType == "order" && businessID != nil {
		deviceStatus := ""
		switch targetStatus {
		case "shipped", "in_transit":
			deviceStatus = "shipped"
		case "delivered":
			deviceStatus = "delivered"
		case "returned":
			deviceStatus = "returned"
		case "cancelled":
			deviceStatus = "cancelled"
		}
		if deviceStatus != "" {
			_, err := tx.ExecContext(ctx, `
				UPDATE biz_order_devices
				SET status=?,
				    shipped_at=CASE WHEN ?='shipped' THEN COALESCE(shipped_at, CURRENT_TIMESTAMP(3)) ELSE shipped_at END,
				    delivered_at=CASE WHEN ?='delivered' THEN COALESCE(delivered_at, CURRENT_TIMESTAMP(3)) ELSE delivered_at END,
				    returned_at=CASE WHEN ?='returned' THEN COALESCE(returned_at, CURRENT_TIMESTAMP(3)) ELSE returned_at END
				WHERE order_id=? AND shipment_id=?
			`, deviceStatus, deviceStatus, deviceStatus, deviceStatus, *businessID, shipmentID)
			if err != nil {
				return model.Shipment{}, err
			}
		}
		switch targetStatus {
		case "delivered":
			if _, err := tx.ExecContext(ctx, `
				UPDATE biz_orders
				SET status='fulfilled'
				WHERE id=? AND order_type='device'
			`, *businessID); err != nil {
				return model.Shipment{}, err
			}
		case "returned", "cancelled":
			if _, err := tx.ExecContext(ctx, `
				UPDATE biz_orders
				SET status='paid'
				WHERE id=? AND order_type='device' AND paid_amount_cents>0
			`, *businessID); err != nil {
				return model.Shipment{}, err
			}
		}
	}

	if businessType == "rma" && businessID != nil {
		rmaStatus := ""
		switch shipmentType {
		case "return", "rma_return":
			switch targetStatus {
			case "ready_to_ship":
				rmaStatus = "RETURN_PENDING"
			case "shipped", "in_transit":
				rmaStatus = "RETURNING"
			case "delivered":
				rmaStatus = "PROCESSING"
			case "exception", "returned":
				rmaStatus = "RETURN_EXCEPTION"
			case "cancelled":
				rmaStatus = "RETURN_CANCELLED"
			}
		case "repair_outbound":
			switch targetStatus {
			case "shipped", "in_transit":
				rmaStatus = "REPAIR_OUTBOUND"
			case "delivered":
				rmaStatus = "EXTERNAL_REPAIR"
			case "exception", "returned", "cancelled":
				rmaStatus = "PROCESSING"
			}
		case "repair_return":
			switch targetStatus {
			case "shipped", "in_transit":
				rmaStatus = "REPAIR_RETURNING"
			case "delivered", "exception", "returned", "cancelled":
				rmaStatus = "PROCESSING"
			}
		default:
			switch targetStatus {
			case "ready_to_ship":
				rmaStatus = "OUTBOUND_PENDING"
			case "shipped", "in_transit":
				rmaStatus = "OUTBOUND_SHIPPING"
			case "exception":
				rmaStatus = "OUTBOUND_EXCEPTION"
			case "returned", "cancelled":
				rmaStatus = "PROCESSING"
			case "delivered":
				rmaStatus = "COMPLETED"
			}
		}
		if rmaStatus != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE inv_rmas
				SET status=?,
				    completed_at=CASE
				      WHEN ?='COMPLETED' THEN COALESCE(completed_at, CURRENT_TIMESTAMP(3))
				      ELSE completed_at
				    END
				WHERE id=? AND status <> 'CANCELLED'
			`, rmaStatus, rmaStatus, *businessID); err != nil {
				return model.Shipment{}, err
			}
			title, detail := rmaLogisticsEventCopy(shipmentType, targetStatus)
			if err := insertRMAEventTx(
				ctx, tx, *businessID, "logistics_"+targetStatus, rmaStatus,
				title, detail, true, userID,
			); err != nil {
				return model.Shipment{}, err
			}
		}
	}

	description := strings.TrimSpace(input.Description)
	if description == "" {
		description = shipmentStatusDescription(targetStatus)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_logistics_events (
			shipment_id, event_code, status, location, description,
			operator_user_id, occurred_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		shipmentID,
		targetStatus,
		targetStatus,
		strings.TrimSpace(input.Location),
		description,
		userID,
		now,
	); err != nil {
		return model.Shipment{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.Shipment{}, err
	}
	return s.GetShipment(ctx, shipmentID)
}

func shipmentDeviceIDsTx(ctx context.Context, tx *sql.Tx, shipmentID int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT device_id
		FROM inv_shipment_items
		WHERE shipment_id=?
		ORDER BY id ASC
	`, shipmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]int64, 0)
	for rows.Next() {
		var deviceID int64
		if err := rows.Scan(&deviceID); err != nil {
			return nil, err
		}
		items = append(items, deviceID)
	}
	return items, rows.Err()
}

func transitionShipmentDevicesTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	deviceIDs []int64,
	targetStatus string,
	targetWarehouseID *int64,
	targetCustomerID *int64,
	reason string,
) error {
	snapshots := make([]shipmentDeviceSnapshot, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		var snapshot shipmentDeviceSnapshot
		if err := tx.QueryRowContext(ctx, `
			SELECT id, sn, sku_code, lifecycle_status, custody_warehouse_id,
			       owner_org_id, current_customer_id
			FROM inv_devices
			WHERE id=?
			FOR UPDATE
		`, deviceID).Scan(
			&snapshot.ID,
			&snapshot.SN,
			&snapshot.SKU,
			&snapshot.Status,
			&snapshot.WarehouseID,
			&snapshot.OwnerOrgID,
			&snapshot.CustomerID,
		); err != nil {
			return err
		}
		if snapshot.Status == targetStatus {
			continue
		}
		if !deviceTransitionAllowed(snapshot.Status, targetStatus) {
			// A ready-to-ship action may include AGENT_STOCK devices that do not
			// support RESERVED. They remain unchanged until the shipment is sent.
			if targetStatus == "RESERVED" && snapshot.Status == "AGENT_STOCK" {
				continue
			}
			return fmt.Errorf(
				"invalid device transition: %s -> %s",
				snapshot.Status,
				targetStatus,
			)
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return nil
	}

	documentType := actionForDeviceTransition(snapshots[0].Status, targetStatus)
	docID, err := createStockDocumentTx(
		ctx,
		tx,
		documentType,
		snapshots[0].WarehouseID,
		targetWarehouseID,
		nil,
		reason,
		userID,
	)
	if err != nil {
		return err
	}

	for _, snapshot := range snapshots {
		toWarehouseID := targetWarehouseID
		if targetStatus == "IN_TRANSIT" || targetStatus == "RMA_TRANSIT" ||
			targetStatus == "REPAIR_TRANSIT" || targetStatus == "EXTERNAL_REPAIR" ||
			targetStatus == "REPAIR_RETURN_TRANSIT" ||
			targetStatus == "SOLD" || targetStatus == "CUSTOMER_BOUND" {
			toWarehouseID = nil
		}
		toCustomerID := snapshot.CustomerID
		if targetCustomerID != nil &&
			(targetStatus == "SOLD" || targetStatus == "CUSTOMER_BOUND") {
			toCustomerID = targetCustomerID
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE inv_devices
			SET lifecycle_status=?, custody_warehouse_id=?, current_customer_id=?,
			    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`,
			targetStatus,
			toWarehouseID,
			toCustomerID,
			userID,
			snapshot.ID,
		); err != nil {
			return err
		}

		if err := insertStockItemAndLedgerTx(
			ctx,
			tx,
			snapshot.ID,
			docID,
			snapshot.SN,
			snapshot.SKU,
			actionForDeviceTransition(snapshot.Status, targetStatus),
			snapshot.Status,
			targetStatus,
			snapshot.WarehouseID,
			toWarehouseID,
			snapshot.OwnerOrgID,
			snapshot.OwnerOrgID,
			snapshot.CustomerID,
			toCustomerID,
			userID,
			reason,
		); err != nil {
			return err
		}
	}
	return nil
}

func transitionShipmentDevicesToAgentTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	deviceIDs []int64,
	targetOrgID int64,
	reason string,
) error {
	snapshots := make([]shipmentDeviceSnapshot, 0, len(deviceIDs))
	for _, deviceID := range deviceIDs {
		var snapshot shipmentDeviceSnapshot
		if err := tx.QueryRowContext(ctx, `
			SELECT id, sn, sku_code, lifecycle_status, custody_warehouse_id,
			       owner_org_id, current_customer_id
			FROM inv_devices
			WHERE id=?
			FOR UPDATE
		`, deviceID).Scan(
			&snapshot.ID,
			&snapshot.SN,
			&snapshot.SKU,
			&snapshot.Status,
			&snapshot.WarehouseID,
			&snapshot.OwnerOrgID,
			&snapshot.CustomerID,
		); err != nil {
			return err
		}
		if !deviceTransitionAllowed(snapshot.Status, "AGENT_STOCK") {
			return fmt.Errorf("invalid device transition: %s -> AGENT_STOCK", snapshot.Status)
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return nil
	}

	docID, err := createStockDocumentTx(
		ctx,
		tx,
		"agent_inbound",
		snapshots[0].WarehouseID,
		nil,
		&targetOrgID,
		reason,
		userID,
	)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inv_devices
			SET lifecycle_status='AGENT_STOCK', custody_warehouse_id=NULL,
			    owner_org_id=?, current_customer_id=NULL,
			    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, targetOrgID, userID, snapshot.ID); err != nil {
			return err
		}
		if err := insertStockItemAndLedgerTx(
			ctx,
			tx,
			snapshot.ID,
			docID,
			snapshot.SN,
			snapshot.SKU,
			"agent_inbound",
			snapshot.Status,
			"AGENT_STOCK",
			snapshot.WarehouseID,
			nil,
			snapshot.OwnerOrgID,
			&targetOrgID,
			snapshot.CustomerID,
			nil,
			userID,
			reason,
		); err != nil {
			return err
		}
	}
	return nil
}

func shipmentStatusAllowed(from string, to string) bool {
	transitions := map[string]map[string]bool{
		"pending": {
			"ready_to_ship": true,
			"cancelled":     true,
		},
		"ready_to_ship": {
			"shipped":   true,
			"delivered": true,
			"cancelled": true,
		},
		"shipped": {
			"in_transit": true,
			"delivered":  true,
			"exception":  true,
			"returned":   true,
		},
		"in_transit": {
			"delivered": true,
			"exception": true,
			"returned":  true,
		},
		"exception": {
			"in_transit": true,
			"delivered":  true,
			"returned":   true,
		},
	}
	return transitions[from][to]
}

func shipmentStatusDescription(status string) string {
	switch status {
	case "ready_to_ship":
		return "仓库已完成备货，等待发出"
	case "shipped":
		return "仓库已出库并交给承运商"
	case "in_transit":
		return "运输途中"
	case "delivered":
		return "已签收"
	case "exception":
		return "物流异常，等待处理"
	case "returned":
		return "包裹已退回"
	case "cancelled":
		return "物流单已取消"
	default:
		return status
	}
}
