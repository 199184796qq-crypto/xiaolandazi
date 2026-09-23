package db

import (
	"context"
	"database/sql"
	"fmt"

	"livecompanion/management/internal/model"
)

func (s *Store) ResolveAfterSalesDevice(
	ctx context.Context,
	actor model.Actor,
	sn string,
) (model.Device, *int64, *int64, error) {
	if actor.TenantID == nil {
		return model.Device{}, nil, nil, sql.ErrNoRows
	}

	if actor.Role == "customer" {
		item, err := s.getAfterSalesDeviceByQuery(ctx, `
			SELECT id, sn, sku_code, batch_no, owner_org_id,
			       custody_warehouse_id, current_customer_id,
			       lifecycle_status, quality_status,
			       created_by_user_id, updated_by_user_id, created_at, updated_at
			FROM inv_devices
			WHERE sn=? AND current_customer_id=?
			LIMIT 1
		`, sn, *actor.TenantID)
		if err != nil {
			return model.Device{}, nil, nil, err
		}
		tenantID := *actor.TenantID
		var agentOrgID *int64
		var relatedAgent int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT agent_org_id
			FROM crm_customer_agent_relations
			WHERE tenant_id=? AND status='active'
			ORDER BY id DESC
			LIMIT 1
		`, tenantID).Scan(&relatedAgent); err == nil {
			agentOrgID = &relatedAgent
		}
		return item, &tenantID, agentOrgID, nil
	}

	if actor.IsAgentAdmin() {
		var agentOrgID int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT id
			FROM crm_agent_orgs
			WHERE mgmt_tenant_id=?
			LIMIT 1
		`, *actor.TenantID).Scan(&agentOrgID); err != nil {
			return model.Device{}, nil, nil, err
		}

		item, err := s.getAfterSalesDeviceByQuery(ctx, `
			SELECT d.id, d.sn, d.sku_code, d.batch_no, d.owner_org_id,
			       d.custody_warehouse_id, d.current_customer_id,
			       d.lifecycle_status, d.quality_status,
			       d.created_by_user_id, d.updated_by_user_id, d.created_at, d.updated_at
			FROM inv_devices d
			WHERE d.sn=?
			  AND (
			    d.owner_org_id=? OR
			    d.owner_org_id=? OR
			    d.current_customer_id IN (
			      SELECT rel.tenant_id
			      FROM crm_customer_agent_relations rel
			      WHERE rel.agent_org_id=? AND rel.status='active'
			    )
			  )
			LIMIT 1
		`, sn, *actor.TenantID, agentOrgID, agentOrgID)
		if err != nil {
			return model.Device{}, nil, nil, err
		}
		return item, item.CurrentCustomerID, &agentOrgID, nil
	}

	return model.Device{}, nil, nil, sql.ErrNoRows
}

func (s *Store) getAfterSalesDeviceByQuery(
	ctx context.Context,
	query string,
	args ...any,
) (model.Device, error) {
	var item model.Device
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
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

func (s *Store) ListPortalRMAs(
	ctx context.Context,
	actor model.Actor,
	page int,
	pageSize int,
) ([]model.RMARecord, int, error) {
	if actor.TenantID == nil {
		return nil, 0, sql.ErrNoRows
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var where string
	var filterArgs []any
	switch {
	case actor.Role == "customer":
		where = "r.source_tenant_id=?"
		filterArgs = []any{*actor.TenantID}
	case actor.IsAgentAdmin():
		var agentOrgID int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT id
			FROM crm_agent_orgs
			WHERE mgmt_tenant_id=?
			LIMIT 1
		`, *actor.TenantID).Scan(&agentOrgID); err != nil {
			return nil, 0, err
		}
		where = `(
			r.source_agent_org_id=? OR
			r.source_tenant_id IN (
				SELECT rel.tenant_id
				FROM crm_customer_agent_relations rel
				WHERE rel.agent_org_id=? AND rel.status='active'
			)
		)`
		filterArgs = []any{agentOrgID, agentOrgID}
	default:
		return nil, 0, fmt.Errorf("actor cannot access after-sales portal")
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM inv_rmas r WHERE " + where
	if err := s.db.QueryRowContext(ctx, countQuery, filterArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
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
		WHERE ` + where + `
		ORDER BY r.id DESC
		LIMIT ? OFFSET ?
	`
	args := append([]any{}, filterArgs...)
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
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
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Store) ActorCanAccessRMA(
	ctx context.Context,
	actor model.Actor,
	rmaID int64,
) (bool, error) {
	if actor.TenantID == nil {
		return false, nil
	}
	var count int
	switch {
	case actor.Role == "customer":
		err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM inv_rmas
			WHERE id=? AND source_tenant_id=?
		`, rmaID, *actor.TenantID).Scan(&count)
		return count > 0, err
	case actor.IsAgentAdmin():
		var agentOrgID int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT id FROM crm_agent_orgs WHERE mgmt_tenant_id=? LIMIT 1
		`, *actor.TenantID).Scan(&agentOrgID); err != nil {
			return false, err
		}
		err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM inv_rmas r
			WHERE r.id=?
			  AND (
			    r.source_agent_org_id=? OR
			    r.source_tenant_id IN (
			      SELECT rel.tenant_id
			      FROM crm_customer_agent_relations rel
			      WHERE rel.agent_org_id=? AND rel.status='active'
			    )
			  )
		`, rmaID, agentOrgID, agentOrgID).Scan(&count)
		return count > 0, err
	default:
		return false, nil
	}
}
