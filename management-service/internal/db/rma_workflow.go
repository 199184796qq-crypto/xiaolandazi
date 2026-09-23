package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func insertRMAEventTx(
	ctx context.Context,
	tx *sql.Tx,
	rmaID int64,
	eventCode string,
	status string,
	title string,
	description string,
	customerVisible bool,
	operatorUserID int64,
) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO inv_rma_events (
			rma_id, event_code, status, title, description,
			customer_visible, operator_user_id, occurred_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		rmaID,
		strings.TrimSpace(eventCode),
		strings.TrimSpace(status),
		strings.TrimSpace(title),
		strings.TrimSpace(description),
		customerVisible,
		operatorUserID,
		time.Now().UTC(),
	)
	return err
}

func rmaLogisticsEventCopy(shipmentType string, targetStatus string) (string, string) {
	switch shipmentType {
	case "return", "rma_return":
		switch targetStatus {
		case "ready_to_ship":
			return "等待设备寄回", "售后已受理，请按约定方式将设备寄回。"
		case "shipped", "in_transit":
			return "设备寄回中", "设备正在寄回售后中心，可继续关注物流状态。"
		case "delivered":
			return "售后已签收", "设备已签收并进入售后待检流程。"
		case "exception", "returned":
			return "寄回物流异常", "寄回物流出现异常，请等待售后人员处理。"
		case "cancelled":
			return "寄回已取消", "本次寄回物流已取消。"
		}
	case "repair_outbound":
		switch targetStatus {
		case "shipped", "in_transit":
			return "外送上游维修", "设备已寄往上游维修方处理。"
		case "delivered":
			return "上游维修方已签收", "设备已送达上游维修方，正在等待维修处理。"
		case "exception", "returned", "cancelled":
			return "外送维修物流异常", "外送维修物流已异常、退回或取消，售后人员将继续处理。"
		}
	case "repair_return":
		switch targetStatus {
		case "shipped", "in_transit":
			return "维修设备返回中", "上游维修完成后，设备正在返回售后中心。"
		case "delivered":
			return "维修返回已签收", "设备已从上游维修方返回，进入复检。"
		case "exception", "returned", "cancelled":
			return "维修返回物流异常", "维修返回物流出现异常，售后人员将继续处理。"
		}
	default:
		switch targetStatus {
		case "ready_to_ship":
			return "准备返还设备", "设备已处理完成，正在准备返还。"
		case "shipped", "in_transit":
			return "设备返还途中", "设备已发出，正在送往收件人。"
		case "exception":
			return "返还物流异常", "返还物流出现异常，请等待售后人员处理。"
		case "delivered":
			return "设备已送达", "设备已送达，本次售后流程完成。"
		}
	}
	return "售后物流已更新", "售后物流状态已更新。"
}

func (s *Store) AcceptRMA(
	ctx context.Context,
	userID int64,
	rmaID int64,
) (model.RMARecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RMARecord{}, err
	}
	defer tx.Rollback()

	var deviceID int64
	var rmaNo string
	var status string
	var issue string
	if err := tx.QueryRowContext(ctx, `
		SELECT device_id, rma_no, status, issue
		FROM inv_rmas
		WHERE id=?
		FOR UPDATE
	`, rmaID).Scan(&deviceID, &rmaNo, &status, &issue); err != nil {
		return model.RMARecord{}, err
	}
	if status != "SUBMITTED" {
		return model.RMARecord{}, fmt.Errorf("RMA is not awaiting acceptance")
	}

	device, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return model.RMARecord{}, err
	}
	if !rmaCanOpen(device.LifecycleStatus) {
		return model.RMARecord{}, fmt.Errorf(
			"device status %s cannot accept RMA",
			device.LifecycleStatus,
		)
	}

	if err := createRMAFulfillmentTx(
		ctx,
		tx,
		userID,
		rmaID,
		rmaNo,
		device,
		issue,
	); err != nil {
		return model.RMARecord{}, err
	}

	var nextStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM inv_rmas
		WHERE id=?
	`, rmaID).Scan(&nextStatus); err != nil {
		return model.RMARecord{}, err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_rmas
		SET accepted_by_user_id=?, accepted_at=?, operator_user_id=?
		WHERE id=?
	`, userID, now, userID, rmaID); err != nil {
		return model.RMARecord{}, err
	}
	if err := insertRMAEventTx(
		ctx,
		tx,
		rmaID,
		"accepted",
		nextStatus,
		"售后申请已受理",
		"售后人员已受理维修单，后续物流、签收和维修状态会持续更新。",
		true,
		userID,
	); err != nil {
		return model.RMARecord{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.RMARecord{}, err
	}
	return s.getRMA(ctx, rmaID)
}

func (s *Store) StartInternalRMARepair(
	ctx context.Context,
	userID int64,
	rmaID int64,
) (model.RMARecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RMARecord{}, err
	}
	defer tx.Rollback()

	var deviceID int64
	var rmaNo string
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT device_id, rma_no, status
		FROM inv_rmas
		WHERE id=?
		FOR UPDATE
	`, rmaID).Scan(&deviceID, &rmaNo, &status); err != nil {
		return model.RMARecord{}, err
	}
	if status != "PROCESSING" {
		return model.RMARecord{}, fmt.Errorf("RMA is not ready for internal repair")
	}

	device, err := lockDeviceTx(ctx, tx, deviceID)
	if err != nil {
		return model.RMARecord{}, err
	}
	if device.LifecycleStatus != "AFTER_SALES" {
		return model.RMARecord{}, fmt.Errorf("device is not in after-sales warehouse")
	}
	var repairWarehouseID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM inv_warehouses
		WHERE code='REPAIR' AND status='active'
		LIMIT 1
	`).Scan(&repairWarehouseID); err != nil {
		return model.RMARecord{}, err
	}
	if err := transitionShipmentDevicesTx(
		ctx,
		tx,
		userID,
		[]int64{deviceID},
		"REPAIRING",
		&repairWarehouseID,
		nil,
		"维修单 "+rmaNo+" · 检测后转入维修库",
	); err != nil {
		return model.RMARecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_rmas
		SET status='REPAIRING', operator_user_id=?
		WHERE id=?
	`, userID, rmaID); err != nil {
		return model.RMARecord{}, err
	}
	if err := insertRMAEventTx(
		ctx,
		tx,
		rmaID,
		"internal_repair_started",
		"REPAIRING",
		"设备进入内部维修",
		"设备已完成检测并进入内部维修流程。",
		true,
		userID,
	); err != nil {
		return model.RMARecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.RMARecord{}, err
	}
	return s.getRMA(ctx, rmaID)
}

func (s *Store) ListRMAEvents(
	ctx context.Context,
	rmaID int64,
	customerVisibleOnly bool,
) ([]model.RMAEvent, error) {
	query := `
		SELECT id, rma_id, event_code, status, title, description,
		       customer_visible, operator_user_id, occurred_at
		FROM inv_rma_events
		WHERE rma_id=?
	`
	if customerVisibleOnly {
		query += " AND customer_visible=1"
	}
	query += " ORDER BY occurred_at ASC, id ASC"

	rows, err := s.db.QueryContext(ctx, query, rmaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.RMAEvent, 0)
	for rows.Next() {
		var item model.RMAEvent
		if err := rows.Scan(
			&item.ID,
			&item.RMAID,
			&item.EventCode,
			&item.Status,
			&item.Title,
			&item.Description,
			&item.CustomerVisible,
			&item.OperatorUserID,
			&item.OccurredAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListRMACosts(
	ctx context.Context,
	rmaID int64,
) ([]model.RMACost, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, cost_no, rma_id, cost_type, amount_cents,
		       counterparty_name, payment_method, note,
		       operator_user_id, occurred_at, created_at
		FROM inv_rma_costs
		WHERE rma_id=?
		ORDER BY occurred_at DESC, id DESC
	`, rmaID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.RMACost, 0)
	for rows.Next() {
		var item model.RMACost
		if err := rows.Scan(
			&item.ID,
			&item.CostNo,
			&item.RMAID,
			&item.CostType,
			&item.AmountCents,
			&item.CounterpartyName,
			&item.PaymentMethod,
			&item.Note,
			&item.OperatorUserID,
			&item.OccurredAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateRMACost(
	ctx context.Context,
	userID int64,
	rmaID int64,
	input model.CreateRMACostInput,
) (model.RMACost, error) {
	costType := strings.ToLower(strings.TrimSpace(input.CostType))
	switch costType {
	case "parts", "labor", "external_repair", "inspection", "other":
	default:
		return model.RMACost{}, fmt.Errorf("unsupported RMA cost type")
	}
	if input.AmountCents == 0 {
		return model.RMACost{}, fmt.Errorf("RMA cost amount must be greater than zero")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RMACost{}, err
	}
	defer tx.Rollback()

	var rmaNo string
	if err := tx.QueryRowContext(ctx, `
		SELECT rma_no
		FROM inv_rmas
		WHERE id=?
		FOR UPDATE
	`, rmaID).Scan(&rmaNo); err != nil {
		return model.RMACost{}, err
	}

	costNo := nextInventoryNo("RMC")
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_rma_costs (
			cost_no, rma_id, cost_type, amount_cents,
			counterparty_name, payment_method, note,
			operator_user_id, occurred_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		costNo,
		rmaID,
		costType,
		input.AmountCents,
		strings.TrimSpace(input.CounterpartyName),
		strings.TrimSpace(input.PaymentMethod),
		strings.TrimSpace(input.Note),
		userID,
		now,
	)
	if err != nil {
		return model.RMACost{}, err
	}
	costID, err := result.LastInsertId()
	if err != nil {
		return model.RMACost{}, err
	}
	if err := insertOperatingEntryTx(
		ctx,
		tx,
		"expense",
		"after_sales_"+costType,
		input.AmountCents,
		"rma_cost",
		&costID,
		costNo,
		"rma_cost:"+fmt.Sprint(costID),
		input.CounterpartyName,
		input.PaymentMethod,
		fmt.Sprintf("售后维修费用 · %s · %s", rmaNo, costType),
		userID,
		now,
	); err != nil {
		return model.RMACost{}, err
	}
	if err := insertRMAEventTx(
		ctx,
		tx,
		rmaID,
		"cost_recorded",
		"",
		"维修费用已登记",
		fmt.Sprintf("%s · %.2f 元", costType, float64(input.AmountCents)/100),
		false,
		userID,
	); err != nil {
		return model.RMACost{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.RMACost{}, err
	}
	return s.getRMACost(ctx, costID)
}

func (s *Store) getRMACost(
	ctx context.Context,
	costID int64,
) (model.RMACost, error) {
	var item model.RMACost
	err := s.db.QueryRowContext(ctx, `
		SELECT id, cost_no, rma_id, cost_type, amount_cents,
		       counterparty_name, payment_method, note,
		       operator_user_id, occurred_at, created_at
		FROM inv_rma_costs
		WHERE id=?
	`, costID).Scan(
		&item.ID,
		&item.CostNo,
		&item.RMAID,
		&item.CostType,
		&item.AmountCents,
		&item.CounterpartyName,
		&item.PaymentMethod,
		&item.Note,
		&item.OperatorUserID,
		&item.OccurredAt,
		&item.CreatedAt,
	)
	return item, err
}
