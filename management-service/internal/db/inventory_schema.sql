CREATE TABLE IF NOT EXISTS inv_batch_registry (
    sku_code VARCHAR(96) NOT NULL,
    batch_no VARCHAR(96) NOT NULL,
    document_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (sku_code, batch_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_warehouses (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    organization_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_warehouses_code (code),
    KEY idx_inv_warehouses_org (organization_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_devices (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    sn VARCHAR(128) NOT NULL,
    sku_code VARCHAR(96) NOT NULL,
    batch_no VARCHAR(96) NOT NULL DEFAULT '',
    owner_org_id BIGINT UNSIGNED NULL,
    custody_warehouse_id BIGINT UNSIGNED NULL,
    current_customer_id BIGINT UNSIGNED NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'INBOUND_PENDING',
    quality_status VARCHAR(32) NOT NULL DEFAULT 'qualified',
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_devices_sn (sn),
    KEY idx_inv_devices_sku (sku_code, lifecycle_status),
    KEY idx_inv_devices_warehouse (custody_warehouse_id, lifecycle_status),
    KEY idx_inv_devices_owner (owner_org_id, lifecycle_status),
    KEY idx_inv_devices_customer (current_customer_id, lifecycle_status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_stock_documents (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    document_no VARCHAR(64) NOT NULL,
    document_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'effective',
    from_warehouse_id BIGINT UNSIGNED NULL,
    to_warehouse_id BIGINT UNSIGNED NULL,
    counterparty_org_id BIGINT UNSIGNED NULL,
    reference_no VARCHAR(96) NOT NULL DEFAULT '',
    product_id BIGINT UNSIGNED NULL,
    product_name_snapshot VARCHAR(160) NOT NULL DEFAULT '',
    expected_quantity INT NOT NULL DEFAULT 0,
    actual_quantity INT NOT NULL DEFAULT 0,
    business_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    counterparty_name VARCHAR(160) NOT NULL DEFAULT '',
    payment_method VARCHAR(64) NOT NULL DEFAULT '',
    reason VARCHAR(1024) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    approved_by_user_id BIGINT UNSIGNED NULL,
    effective_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_stock_documents_no (document_no),
    KEY idx_inv_stock_documents_type (document_type, created_at),
    KEY idx_inv_stock_documents_warehouse (to_warehouse_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_stock_document_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    document_id BIGINT UNSIGNED NOT NULL,
    device_id BIGINT UNSIGNED NOT NULL,
    sn_snapshot VARCHAR(128) NOT NULL,
    sku_snapshot VARCHAR(96) NOT NULL,
    from_status VARCHAR(32) NOT NULL DEFAULT '',
    to_status VARCHAR(32) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_inv_stock_document_items_document (document_id),
    KEY idx_inv_stock_document_items_device (device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_device_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    device_id BIGINT UNSIGNED NOT NULL,
    document_id BIGINT UNSIGNED NOT NULL,
    action VARCHAR(48) NOT NULL,
    from_status VARCHAR(32) NOT NULL DEFAULT '',
    to_status VARCHAR(32) NOT NULL,
    from_warehouse_id BIGINT UNSIGNED NULL,
    to_warehouse_id BIGINT UNSIGNED NULL,
    from_owner_org_id BIGINT UNSIGNED NULL,
    to_owner_org_id BIGINT UNSIGNED NULL,
    from_customer_id BIGINT UNSIGNED NULL,
    to_customer_id BIGINT UNSIGNED NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_inv_device_ledger_device (device_id, created_at),
    KEY idx_inv_device_ledger_document (document_id),
    KEY idx_inv_device_ledger_action (action, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_rmas (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rma_no VARCHAR(64) NOT NULL,
    device_id BIGINT UNSIGNED NOT NULL,
    service_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'SUBMITTED',
    source_type VARCHAR(32) NOT NULL DEFAULT 'staff_manual',
    source_user_id BIGINT UNSIGNED NULL,
    source_tenant_id BIGINT UNSIGNED NULL,
    source_agent_org_id BIGINT UNSIGNED NULL,
    customer_name VARCHAR(160) NOT NULL DEFAULT '',
    contact_phone VARCHAR(64) NOT NULL DEFAULT '',
    issue VARCHAR(2048) NOT NULL DEFAULT '',
    resolution VARCHAR(2048) NOT NULL DEFAULT '',
    replacement_device_id BIGINT UNSIGNED NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    accepted_by_user_id BIGINT UNSIGNED NULL,
    accepted_at DATETIME(3) NULL,
    completed_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_rmas_no (rma_no),
    KEY idx_inv_rmas_device (device_id, status),
    KEY idx_inv_rmas_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS inv_rma_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rma_id BIGINT UNSIGNED NOT NULL,
    event_code VARCHAR(48) NOT NULL,
    status VARCHAR(32) NOT NULL,
    title VARCHAR(160) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    customer_visible TINYINT(1) NOT NULL DEFAULT 1,
    operator_user_id BIGINT UNSIGNED NULL,
    occurred_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_inv_rma_events_rma (rma_id, occurred_at),
    KEY idx_inv_rma_events_status (status, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_rma_costs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    cost_no VARCHAR(64) NOT NULL,
    rma_id BIGINT UNSIGNED NOT NULL,
    cost_type VARCHAR(48) NOT NULL,
    amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    counterparty_name VARCHAR(160) NOT NULL DEFAULT '',
    payment_method VARCHAR(64) NOT NULL DEFAULT '',
    note VARCHAR(1024) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    occurred_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_rma_costs_no (cost_no),
    KEY idx_inv_rma_costs_rma (rma_id, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_shipments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    shipment_no VARCHAR(64) NOT NULL,
    shipment_type VARCHAR(32) NOT NULL DEFAULT 'outbound',
    business_type VARCHAR(32) NOT NULL DEFAULT 'manual',
    business_id BIGINT UNSIGNED NULL,
    business_no VARCHAR(96) NOT NULL DEFAULT '',
    from_warehouse_id BIGINT UNSIGNED NULL,
    to_warehouse_id BIGINT UNSIGNED NULL,
    recipient_customer_id BIGINT UNSIGNED NULL,
    recipient_org_id BIGINT UNSIGNED NULL,
    recipient_type VARCHAR(32) NOT NULL DEFAULT 'individual',
    delivery_method VARCHAR(32) NOT NULL DEFAULT 'courier',
    logistics_fee_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    recipient_name VARCHAR(128) NOT NULL DEFAULT '',
    recipient_phone VARCHAR(64) NOT NULL DEFAULT '',
    recipient_address VARCHAR(512) NOT NULL DEFAULT '',
    carrier_code VARCHAR(64) NOT NULL DEFAULT '',
    carrier_name VARCHAR(128) NOT NULL DEFAULT '',
    tracking_no VARCHAR(128) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    note VARCHAR(1024) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    shipped_at DATETIME(3) NULL,
    delivered_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_shipments_no (shipment_no),
    KEY idx_inv_shipments_status (status, created_at),
    KEY idx_inv_shipments_business (business_type, business_id),
    KEY idx_inv_shipments_tracking (tracking_no),
    KEY idx_inv_shipments_customer (recipient_customer_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_shipment_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    shipment_id BIGINT UNSIGNED NOT NULL,
    device_id BIGINT UNSIGNED NOT NULL,
    sn_snapshot VARCHAR(128) NOT NULL,
    sku_snapshot VARCHAR(96) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_shipment_items_shipment_device (shipment_id, device_id),
    KEY idx_inv_shipment_items_device (device_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_logistics_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    shipment_id BIGINT UNSIGNED NOT NULL,
    event_code VARCHAR(48) NOT NULL,
    status VARCHAR(32) NOT NULL,
    location VARCHAR(160) NOT NULL DEFAULT '',
    description VARCHAR(512) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    occurred_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_inv_logistics_events_shipment (shipment_id, occurred_at),
    KEY idx_inv_logistics_events_status (status, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS inv_rma_links (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    rma_id BIGINT UNSIGNED NOT NULL,
    source_order_id BIGINT UNSIGNED NULL,
    source_order_no VARCHAR(64) NOT NULL DEFAULT '',
    source_shipment_id BIGINT UNSIGNED NULL,
    source_shipment_no VARCHAR(64) NOT NULL DEFAULT '',
    source_device_status VARCHAR(32) NOT NULL DEFAULT '',
    return_shipment_id BIGINT UNSIGNED NULL,
    return_shipment_no VARCHAR(64) NOT NULL DEFAULT '',
    outbound_shipment_id BIGINT UNSIGNED NULL,
    outbound_shipment_no VARCHAR(64) NOT NULL DEFAULT '',
    repair_outbound_shipment_id BIGINT UNSIGNED NULL,
    repair_outbound_shipment_no VARCHAR(64) NOT NULL DEFAULT '',
    repair_return_shipment_id BIGINT UNSIGNED NULL,
    repair_return_shipment_no VARCHAR(64) NOT NULL DEFAULT '',
    refund_id BIGINT UNSIGNED NULL,
    refund_no VARCHAR(64) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_rma_links_rma (rma_id),
    KEY idx_inv_rma_links_order (source_order_id),
    KEY idx_inv_rma_links_source_shipment (source_shipment_id),
    KEY idx_inv_rma_links_return_shipment (return_shipment_id),
    KEY idx_inv_rma_links_outbound_shipment (outbound_shipment_id),
    KEY idx_inv_rma_links_repair_outbound (repair_outbound_shipment_id),
    KEY idx_inv_rma_links_repair_return (repair_return_shipment_id),
    KEY idx_inv_rma_links_refund (refund_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inv_scrap_disposals (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    disposal_no VARCHAR(64) NOT NULL,
    device_id BIGINT UNSIGNED NOT NULL,
    amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    buyer_name VARCHAR(160) NOT NULL DEFAULT '',
    payment_method VARCHAR(64) NOT NULL DEFAULT '',
    note VARCHAR(1024) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    disposed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inv_scrap_disposals_no (disposal_no),
    UNIQUE KEY uk_inv_scrap_disposals_device (device_id),
    KEY idx_inv_scrap_disposals_time (disposed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
