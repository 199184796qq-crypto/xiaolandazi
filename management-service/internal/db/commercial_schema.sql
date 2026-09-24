CREATE TABLE IF NOT EXISTS iam_roles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_iam_roles_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS iam_permissions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(128) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_iam_permissions_code (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS iam_user_roles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    role_id BIGINT UNSIGNED NOT NULL,
    granted_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_iam_user_roles (user_id, role_id),
    KEY idx_iam_user_roles_role (role_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS iam_role_permissions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    role_id BIGINT UNSIGNED NOT NULL,
    permission_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_iam_role_permissions (role_id, permission_id),
    KEY idx_iam_role_permissions_permission (permission_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_customer_profiles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'direct',
    source_sales_staff_id BIGINT UNSIGNED NULL,
    source_agent_org_id BIGINT UNSIGNED NULL,
    source_note VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_customer_profiles_tenant (tenant_id),
    KEY idx_crm_customer_profiles_source (source_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_sales_teams (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    parent_team_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_sales_teams_code (code),
    KEY idx_crm_sales_teams_parent (parent_team_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_sales_staff (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    employee_code VARCHAR(64) NOT NULL,
    team_id BIGINT UNSIGNED NULL,
    manager_staff_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_sales_staff_user (user_id),
    UNIQUE KEY uk_crm_sales_staff_code (employee_code),
    KEY idx_crm_sales_staff_team (team_id),
    KEY idx_crm_sales_staff_manager (manager_staff_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_customer_sales_assignments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    sales_staff_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    effective_from DATETIME(3) NOT NULL,
    effective_to DATETIME(3) NULL,
    assigned_by_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_crm_sales_assignment_tenant (tenant_id, status),
    KEY idx_crm_sales_assignment_staff (sales_staff_id, status),
    KEY idx_crm_sales_assignment_period (effective_from, effective_to)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_orgs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    parent_agent_org_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_agent_orgs_code (code),
    KEY idx_crm_agent_orgs_parent (parent_agent_org_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_members (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    agent_org_id BIGINT UNSIGNED NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    member_role VARCHAR(64) NOT NULL DEFAULT 'agent_staff',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_agent_members (agent_org_id, user_id),
    KEY idx_crm_agent_members_user (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_customer_agent_relations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    agent_org_id BIGINT UNSIGNED NOT NULL,
    relation_type VARCHAR(32) NOT NULL DEFAULT 'primary',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    effective_from DATETIME(3) NOT NULL,
    effective_to DATETIME(3) NULL,
    assigned_by_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_crm_agent_relation_tenant (tenant_id, status),
    KEY idx_crm_agent_relation_org (agent_org_id, status),
    KEY idx_crm_agent_relation_period (effective_from, effective_to)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS crm_referral_relations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    referrer_tenant_id BIGINT UNSIGNED NOT NULL,
    referred_tenant_id BIGINT UNSIGNED NOT NULL,
    program_version_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    bound_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    commission_start_at DATETIME(3) NULL,
    commission_end_at DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_referral_referred (referred_tenant_id),
    KEY idx_crm_referral_referrer (referrer_tenant_id, status),
    KEY idx_crm_referral_period (commission_start_at, commission_end_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS catalog_membership_plans (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_membership_plans_code (code),
    KEY idx_catalog_membership_plans_status (status, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS catalog_membership_plan_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    plan_id BIGINT UNSIGNED NOT NULL,
    version_no INT UNSIGNED NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    recurring_month_discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    recurring_quarter_discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    annual_discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    billing_period_unit VARCHAR(16) NOT NULL DEFAULT 'month',
    billing_period_count INT UNSIGNED NOT NULL DEFAULT 1,
    included_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
    default_time_card_discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    default_device_discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    allow_auto_renew TINYINT(1) NOT NULL DEFAULT 0,
    entitlements_json JSON NULL,
    effective_from DATETIME(3) NULL,
    effective_to DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_membership_plan_versions (plan_id, version_no),
    KEY idx_catalog_membership_plan_version_status (plan_id, lifecycle_status),
    KEY idx_catalog_membership_plan_version_period (effective_from, effective_to)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS catalog_time_card_products (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_time_card_products_code (code),
    KEY idx_catalog_time_card_products_status (status, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS catalog_time_card_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_id BIGINT UNSIGNED NOT NULL,
    version_no INT UNSIGNED NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    duration_seconds BIGINT UNSIGNED NOT NULL,
    validity_days INT UNSIGNED NOT NULL,
    activation_mode VARCHAR(32) NOT NULL DEFAULT 'first_use',
    activation_deadline_days INT UNSIGNED NOT NULL DEFAULT 0,
    participates_referral TINYINT(1) NOT NULL DEFAULT 1,
    participates_sales_commission TINYINT(1) NOT NULL DEFAULT 1,
    participates_agent_settlement TINYINT(1) NOT NULL DEFAULT 1,
    effective_from DATETIME(3) NULL,
    effective_to DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_time_card_versions (product_id, version_no),
    KEY idx_catalog_time_card_version_status (product_id, lifecycle_status),
    KEY idx_catalog_time_card_version_period (effective_from, effective_to)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS catalog_membership_card_discounts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    plan_version_id BIGINT UNSIGNED NOT NULL,
    card_version_id BIGINT UNSIGNED NOT NULL,
    discount_bps INT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_membership_card_discount (plan_version_id, card_version_id),
    KEY idx_catalog_membership_card_discount_card (card_version_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_memberships (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    plan_version_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    cycle_start_at DATETIME(3) NOT NULL,
    cycle_end_at DATETIME(3) NOT NULL,
    auto_renew TINYINT(1) NOT NULL DEFAULT 0,
    source_order_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_biz_memberships_tenant (tenant_id, status),
    KEY idx_biz_memberships_plan (plan_id, plan_version_id),
    KEY idx_biz_memberships_cycle (cycle_end_at, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_no VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    order_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    list_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    discount_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    payable_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    paid_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    refunded_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    membership_id_snapshot BIGINT UNSIGNED NULL,
    membership_plan_version_id_snapshot BIGINT UNSIGNED NULL,
    sales_assignment_id_snapshot BIGINT UNSIGNED NULL,
    sales_staff_id_snapshot BIGINT UNSIGNED NULL,
    agent_relation_id_snapshot BIGINT UNSIGNED NULL,
    agent_org_id_snapshot BIGINT UNSIGNED NULL,
    referral_relation_id_snapshot BIGINT UNSIGNED NULL,
    referrer_tenant_id_snapshot BIGINT UNSIGNED NULL,
    pricing_snapshot_json JSON NULL,
    relation_snapshot_json JSON NULL,
    idempotency_key VARCHAR(128) NULL,
    paid_at DATETIME(3) NULL,
    cancelled_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_biz_orders_no (order_no),
    UNIQUE KEY uk_biz_orders_idempotency (idempotency_key),
    KEY idx_biz_orders_tenant (tenant_id, created_at),
    KEY idx_biz_orders_status (status, created_at),
    KEY idx_biz_orders_sales (sales_staff_id_snapshot, created_at),
    KEY idx_biz_orders_agent (agent_org_id_snapshot, created_at),
    KEY idx_biz_orders_referrer (referrer_tenant_id_snapshot, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_order_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_id BIGINT UNSIGNED NOT NULL,
    product_type VARCHAR(32) NOT NULL,
    product_id BIGINT UNSIGNED NOT NULL,
    product_version_id BIGINT UNSIGNED NOT NULL,
    product_name_snapshot VARCHAR(128) NOT NULL,
    quantity INT UNSIGNED NOT NULL DEFAULT 1,
    duration_seconds_snapshot BIGINT UNSIGNED NULL,
    validity_days_snapshot INT UNSIGNED NULL,
    activation_mode_snapshot VARCHAR(32) NULL,
    activation_deadline_days_snapshot INT UNSIGNED NULL,
    unit_list_price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    unit_paid_price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    discount_bps_snapshot INT UNSIGNED NOT NULL DEFAULT 10000,
    metadata_json JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_biz_order_items_order (order_id),
    KEY idx_biz_order_items_product (product_type, product_id, product_version_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_wallet_accounts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    account_type VARCHAR(32) NOT NULL DEFAULT 'cash',
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    balance_cents BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_wallet_accounts (tenant_id, account_type, currency),
    KEY idx_fin_wallet_accounts_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_wallet_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    wallet_account_id BIGINT UNSIGNED NOT NULL,
    direction VARCHAR(16) NOT NULL,
    amount_cents BIGINT UNSIGNED NOT NULL,
    balance_before_cents BIGINT NOT NULL,
    balance_after_cents BIGINT NOT NULL,
    business_type VARCHAR(64) NOT NULL,
    business_id BIGINT UNSIGNED NULL,
    reversal_of_entry_id BIGINT UNSIGNED NULL,
    order_no VARCHAR(64) NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    idempotency_key VARCHAR(128) NULL,
    occurred_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_wallet_ledger_external (external_id),
    UNIQUE KEY uk_fin_wallet_ledger_idempotency (idempotency_key),
    KEY idx_fin_wallet_ledger_tenant (tenant_id, occurred_at),
    KEY idx_fin_wallet_ledger_account (wallet_account_id, occurred_at),
    KEY idx_fin_wallet_ledger_business (business_type, business_id),
    KEY idx_fin_wallet_ledger_reversal (reversal_of_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_recharge_orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    recharge_no VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    requested_amount_cents BIGINT UNSIGNED NOT NULL,
    credited_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    payment_method VARCHAR(32) NOT NULL DEFAULT 'pending',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    external_trade_no VARCHAR(128) NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    idempotency_key VARCHAR(128) NULL,
    paid_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_recharge_orders_no (recharge_no),
    UNIQUE KEY uk_fin_recharge_orders_idempotency (idempotency_key),
    KEY idx_fin_recharge_orders_tenant (tenant_id, created_at),
    KEY idx_fin_recharge_orders_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_refund_orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    refund_no VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    source_type VARCHAR(32) NOT NULL,
    source_id BIGINT UNSIGNED NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    refund_amount_cents BIGINT UNSIGNED NOT NULL,
    refund_method VARCHAR(32) NOT NULL DEFAULT 'wallet',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    reason VARCHAR(1024) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    idempotency_key VARCHAR(128) NULL,
    processed_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_refund_orders_no (refund_no),
    UNIQUE KEY uk_fin_refund_orders_idempotency (idempotency_key),
    KEY idx_fin_refund_orders_tenant (tenant_id, created_at),
    KEY idx_fin_refund_orders_source (source_type, source_id),
    KEY idx_fin_refund_orders_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_time_card_assets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    asset_no VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    source_order_id BIGINT UNSIGNED NOT NULL,
    source_order_item_id BIGINT UNSIGNED NOT NULL,
    asset_sequence INT UNSIGNED NOT NULL,
    product_id BIGINT UNSIGNED NOT NULL,
    product_version_id BIGINT UNSIGNED NOT NULL,
    product_name_snapshot VARCHAR(128) NOT NULL,
    original_seconds BIGINT UNSIGNED NOT NULL,
    remaining_seconds BIGINT UNSIGNED NOT NULL,
    activation_mode VARCHAR(32) NOT NULL DEFAULT 'first_use',
    activation_deadline_at DATETIME(3) NULL,
    validity_days INT UNSIGNED NOT NULL,
    activated_at DATETIME(3) NULL,
    expires_at DATETIME(3) NULL,
    quota_bucket_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'unactivated',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_biz_time_card_assets_no (asset_no),
    UNIQUE KEY uk_biz_time_card_assets_order_item_seq (source_order_item_id, asset_sequence),
    UNIQUE KEY uk_biz_time_card_assets_bucket (quota_bucket_id),
    KEY idx_biz_time_card_assets_tenant_status (tenant_id, status, activation_deadline_at, id),
    KEY idx_biz_time_card_assets_order (source_order_id, source_order_item_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS quota_buckets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    source_type VARCHAR(64) NOT NULL,
    source_id BIGINT UNSIGNED NULL,
    original_seconds BIGINT UNSIGNED NOT NULL,
    remaining_seconds BIGINT UNSIGNED NOT NULL,
    effective_at DATETIME(3) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    priority INT NOT NULL DEFAULT 100,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata_json JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_quota_buckets_external (external_id),
    KEY idx_quota_buckets_tenant_fefo (tenant_id, status, expires_at, priority),
    KEY idx_quota_buckets_source (source_type, source_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS quota_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    bucket_id BIGINT UNSIGNED NOT NULL,
    change_seconds BIGINT NOT NULL,
    remaining_before_seconds BIGINT UNSIGNED NOT NULL,
    remaining_after_seconds BIGINT UNSIGNED NOT NULL,
    business_type VARCHAR(64) NOT NULL,
    business_id BIGINT UNSIGNED NULL,
    reversal_of_entry_id BIGINT UNSIGNED NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    idempotency_key VARCHAR(128) NULL,
    occurred_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_quota_ledger_external (external_id),
    UNIQUE KEY uk_quota_ledger_idempotency (idempotency_key),
    KEY idx_quota_ledger_tenant (tenant_id, occurred_at),
    KEY idx_quota_ledger_bucket (bucket_id, occurred_at),
    KEY idx_quota_ledger_business (business_type, business_id),
    KEY idx_quota_ledger_reversal (reversal_of_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS quota_usage_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NULL,
    billable_seconds BIGINT UNSIGNED NOT NULL,
    started_at DATETIME(3) NOT NULL,
    ended_at DATETIME(3) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'posted',
    idempotency_key VARCHAR(128) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_quota_usage_external (external_id),
    UNIQUE KEY uk_quota_usage_idempotency (idempotency_key),
    KEY idx_quota_usage_tenant (tenant_id, started_at),
    KEY idx_quota_usage_room (room_id, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inc_programs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    program_type VARCHAR(64) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    description VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inc_programs_code (code),
    KEY idx_inc_programs_type (program_type, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inc_program_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    program_id BIGINT UNSIGNED NOT NULL,
    version_no INT UNSIGNED NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    pending_days INT UNSIGNED NOT NULL DEFAULT 0,
    effective_from DATETIME(3) NULL,
    effective_to DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inc_program_versions (program_id, version_no),
    KEY idx_inc_program_version_status (program_id, lifecycle_status),
    KEY idx_inc_program_version_period (effective_from, effective_to)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inc_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    program_version_id BIGINT UNSIGNED NOT NULL,
    priority INT NOT NULL DEFAULT 100,
    event_type VARCHAR(64) NOT NULL,
    conditions_json JSON NULL,
    action_type VARCHAR(64) NOT NULL,
    action_config_json JSON NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_inc_rules_program (program_version_id, enabled, priority),
    KEY idx_inc_rules_event (event_type, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inc_earnings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    beneficiary_type VARCHAR(32) NOT NULL,
    beneficiary_id BIGINT UNSIGNED NOT NULL,
    earning_type VARCHAR(64) NOT NULL,
    source_order_id BIGINT UNSIGNED NULL,
    source_refund_id BIGINT UNSIGNED NULL,
    program_version_id BIGINT UNSIGNED NULL,
    rule_id BIGINT UNSIGNED NULL,
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    amount_cents BIGINT NOT NULL DEFAULT 0,
    quota_seconds BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    available_at DATETIME(3) NULL,
    reversal_of_earning_id BIGINT UNSIGNED NULL,
    calculation_snapshot_json JSON NULL,
    idempotency_key VARCHAR(128) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inc_earnings_external (external_id),
    UNIQUE KEY uk_inc_earnings_idempotency (idempotency_key),
    KEY idx_inc_earnings_beneficiary (beneficiary_type, beneficiary_id, status),
    KEY idx_inc_earnings_order (source_order_id),
    KEY idx_inc_earnings_refund (source_refund_id),
    KEY idx_inc_earnings_reversal (reversal_of_earning_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inc_settlement_batches (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    batch_no VARCHAR(64) NOT NULL,
    beneficiary_type VARCHAR(32) NOT NULL,
    beneficiary_id BIGINT UNSIGNED NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    period_start_at DATETIME(3) NOT NULL,
    period_end_at DATETIME(3) NOT NULL,
    gross_amount_cents BIGINT NOT NULL DEFAULT 0,
    adjustment_amount_cents BIGINT NOT NULL DEFAULT 0,
    settlement_amount_cents BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_by_user_id BIGINT UNSIGNED NULL,
    approved_by_user_id BIGINT UNSIGNED NULL,
    approved_at DATETIME(3) NULL,
    paid_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inc_settlement_batches_no (batch_no),
    KEY idx_inc_settlement_beneficiary (beneficiary_type, beneficiary_id, status),
    KEY idx_inc_settlement_period (period_start_at, period_end_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS inc_settlement_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    settlement_batch_id BIGINT UNSIGNED NOT NULL,
    earning_id BIGINT UNSIGNED NOT NULL,
    amount_cents BIGINT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_inc_settlement_items_earning (earning_id),
    KEY idx_inc_settlement_items_batch (settlement_batch_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_audit_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_user_id BIGINT UNSIGNED NULL,
    action VARCHAR(96) NOT NULL,
    entity_type VARCHAR(64) NOT NULL,
    entity_id VARCHAR(96) NOT NULL,
    reason VARCHAR(1024) NOT NULL DEFAULT '',
    before_json JSON NULL,
    after_json JSON NULL,
    request_id VARCHAR(96) NULL,
    client_ip VARCHAR(64) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_biz_audit_actor (actor_user_id, created_at),
    KEY idx_biz_audit_entity (entity_type, entity_id, created_at),
    KEY idx_biz_audit_action (action, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS sys_feature_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    feature_key VARCHAR(64) NOT NULL,
    record_key VARCHAR(128) NOT NULL,
    title VARCHAR(160) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    sort_order INT NOT NULL DEFAULT 0,
    payload_json JSON NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_sys_feature_records_key (feature_key, record_key),
    KEY idx_sys_feature_records_feature (feature_key, status, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci


-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaigns (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(128) NOT NULL,
    name VARCHAR(160) NOT NULL,
    description TEXT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    sort_order INT NOT NULL DEFAULT 0,
    pricing_rule VARCHAR(32) NOT NULL DEFAULT 'floor_yuan',
    starts_at DATETIME(3) NULL,
    ends_at DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_mkt_campaigns_code (code),
    KEY idx_mkt_campaigns_status_window (status, starts_at, ends_at),
    KEY idx_mkt_campaigns_sort (sort_order, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_placements (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    campaign_id BIGINT UNSIGNED NOT NULL,
    placement_code VARCHAR(64) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_mkt_campaign_placements_campaign_code (campaign_id, placement_code),
    KEY idx_mkt_campaign_placements_code (placement_code, campaign_id),
    CONSTRAINT fk_mkt_campaign_placements_campaign
        FOREIGN KEY (campaign_id) REFERENCES mkt_campaigns(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    campaign_id BIGINT UNSIGNED NOT NULL,
    target_type VARCHAR(32) NOT NULL,
    target_id BIGINT UNSIGNED NOT NULL,
    quantity INT UNSIGNED NOT NULL DEFAULT 1,
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_mkt_campaign_items_campaign (campaign_id, sort_order, id),
    KEY idx_mkt_campaign_items_target (target_type, target_id, campaign_id),
    CONSTRAINT fk_mkt_campaign_items_campaign
        FOREIGN KEY (campaign_id) REFERENCES mkt_campaigns(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_price_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    campaign_id BIGINT UNSIGNED NOT NULL,
    campaign_item_id BIGINT UNSIGNED NOT NULL,
    pricing_mode VARCHAR(32) NOT NULL DEFAULT 'discount',
    package_months INT UNSIGNED NOT NULL DEFAULT 1,
    discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    fixed_price_cents BIGINT UNSIGNED NULL,
    pricing_rule VARCHAR(32) NOT NULL DEFAULT 'floor_yuan',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_mkt_campaign_price_rules_item (campaign_item_id),
    KEY idx_mkt_campaign_price_rules_campaign (campaign_id),
    CONSTRAINT fk_mkt_campaign_price_rules_campaign
        FOREIGN KEY (campaign_id) REFERENCES mkt_campaigns(id) ON DELETE CASCADE,
    CONSTRAINT fk_mkt_campaign_price_rules_item
        FOREIGN KEY (campaign_item_id) REFERENCES mkt_campaign_items(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_scopes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    campaign_id BIGINT UNSIGNED NOT NULL,
    scope_type VARCHAR(32) NOT NULL DEFAULT 'all_customers',
    scope_ref_id BIGINT UNSIGNED NULL,
    scope_value VARCHAR(128) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_mkt_campaign_scopes_campaign (campaign_id),
    KEY idx_mkt_campaign_scopes_ref (scope_type, scope_ref_id),
    CONSTRAINT fk_mkt_campaign_scopes_campaign
        FOREIGN KEY (campaign_id) REFERENCES mkt_campaigns(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_inventory (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    campaign_id BIGINT UNSIGNED NOT NULL,
    campaign_item_id BIGINT UNSIGNED NOT NULL,
    stock_limit BIGINT UNSIGNED NULL,
    reserved_quantity BIGINT UNSIGNED NOT NULL DEFAULT 0,
    used_quantity BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_mkt_campaign_inventory_item (campaign_item_id),
    KEY idx_mkt_campaign_inventory_campaign (campaign_id),
    CONSTRAINT fk_mkt_campaign_inventory_campaign
        FOREIGN KEY (campaign_id) REFERENCES mkt_campaigns(id) ON DELETE CASCADE,
    CONSTRAINT fk_mkt_campaign_inventory_item
        FOREIGN KEY (campaign_item_id) REFERENCES mkt_campaign_items(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_usage (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    campaign_id BIGINT UNSIGNED NOT NULL,
    campaign_item_id BIGINT UNSIGNED NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    order_id BIGINT UNSIGNED NULL,
    quantity INT UNSIGNED NOT NULL DEFAULT 1,
    discount_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_mkt_campaign_usage_campaign (campaign_id, status, created_at),
    KEY idx_mkt_campaign_usage_tenant (tenant_id, created_at),
    KEY idx_mkt_campaign_usage_order (order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_order_snapshots (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_id BIGINT UNSIGNED NOT NULL,
    campaign_id BIGINT UNSIGNED NOT NULL,
    campaign_item_id BIGINT UNSIGNED NULL,
    campaign_code VARCHAR(128) NOT NULL,
    campaign_name VARCHAR(160) NOT NULL,
    target_type VARCHAR(32) NOT NULL,
    target_id BIGINT UNSIGNED NOT NULL,
    pricing_mode VARCHAR(32) NOT NULL,
    package_months INT UNSIGNED NOT NULL DEFAULT 1,
    quantity INT UNSIGNED NOT NULL DEFAULT 1,
    discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    list_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    payable_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    discount_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_mkt_campaign_order_snapshot (order_id, campaign_id, target_type, target_id),
    KEY idx_mkt_campaign_order_campaign (campaign_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci


-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_levels (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    entry_fee_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    included_devices INT UNSIGNED NOT NULL DEFAULT 0,
    device_discount_bps INT UNSIGNED NOT NULL DEFAULT 10000,
    consumer_share_bps INT UNSIGNED NOT NULL DEFAULT 0,
    reserve_bps INT UNSIGNED NOT NULL DEFAULT 0,
    settlement_cycle VARCHAR(32) NOT NULL DEFAULT 'monthly',
    hold_days INT UNSIGNED NOT NULL DEFAULT 0,
    oem_enabled TINYINT(1) NOT NULL DEFAULT 0,
    note VARCHAR(1024) NOT NULL DEFAULT '',
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_agent_levels_code (code),
    KEY idx_crm_agent_levels_status (status, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_level_history (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    agent_tenant_id BIGINT UNSIGNED NOT NULL,
    level_id BIGINT UNSIGNED NOT NULL,
    previous_level_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'scheduled',
    approved_by_user_id BIGINT UNSIGNED NULL,
    approved_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    effective_at DATETIME(3) NOT NULL,
    ended_at DATETIME(3) NULL,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_crm_agent_level_history_agent (agent_tenant_id, effective_at),
    KEY idx_crm_agent_level_history_level (level_id, status),
    KEY idx_crm_agent_level_history_status (status, effective_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_contracts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    contract_no VARCHAR(96) NOT NULL,
    agent_tenant_id BIGINT UNSIGNED NOT NULL,
    parent_contract_id BIGINT UNSIGNED NULL,
    contract_type VARCHAR(32) NOT NULL DEFAULT 'cooperation',
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    level_id BIGINT UNSIGNED NULL,
    level_snapshot_json JSON NULL,
    starts_on DATE NOT NULL,
    ends_on DATE NULL,
    contract_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    note VARCHAR(1024) NOT NULL DEFAULT '',
    signed_at DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_agent_contracts_no (contract_no),
    KEY idx_crm_agent_contracts_agent (agent_tenant_id, status, starts_on),
    KEY idx_crm_agent_contracts_level (level_id),
    KEY idx_crm_agent_contracts_parent (parent_contract_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_contract_attachments (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    contract_id BIGINT UNSIGNED NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    file_url VARCHAR(1024) NOT NULL,
    content_type VARCHAR(96) NOT NULL,
    size_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
    page_order INT NOT NULL DEFAULT 0,
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_crm_agent_contract_attachments_contract (contract_id, page_order, id),
    CONSTRAINT fk_crm_agent_contract_attachments_contract FOREIGN KEY (contract_id) REFERENCES crm_agent_contracts(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS fin_payment_transactions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    payment_no VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    order_id BIGINT UNSIGNED NOT NULL,
    order_no VARCHAR(64) NOT NULL,
    channel VARCHAR(32) NOT NULL DEFAULT 'sandbox',
    payment_method VARCHAR(32) NOT NULL DEFAULT 'sandbox_manual',
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    expected_amount_cents BIGINT UNSIGNED NOT NULL,
    input_amount_cents BIGINT UNSIGNED NOT NULL,
    paid_amount_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    failure_reason VARCHAR(256) NOT NULL DEFAULT '',
    external_trade_no VARCHAR(128) NOT NULL DEFAULT '',
    operator_user_id BIGINT UNSIGNED NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    paid_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_payment_transactions_no (payment_no),
    UNIQUE KEY uk_fin_payment_transactions_idempotency (idempotency_key),
    KEY idx_fin_payment_transactions_tenant (tenant_id, created_at),
    KEY idx_fin_payment_transactions_order (order_id, created_at),
    KEY idx_fin_payment_transactions_status (status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS catalog_device_products (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code VARCHAR(64) NOT NULL,
    sku_code VARCHAR(96) NOT NULL,
    name VARCHAR(128) NOT NULL,
    description VARCHAR(1024) NOT NULL DEFAULT '',
    image_url VARCHAR(2048) NOT NULL DEFAULT '',
    unit_code VARCHAR(96) NOT NULL DEFAULT 'unit',
    sales_stock INT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'draft',
    sort_order INT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_device_products_code (code),
    UNIQUE KEY uk_catalog_device_products_sku (sku_code),
    KEY idx_catalog_device_products_status (status, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS catalog_device_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    product_id BIGINT UNSIGNED NOT NULL,
    version_no INT UNSIGNED NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    cost_price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    list_price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    sale_price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0,
    participates_referral TINYINT(1) NOT NULL DEFAULT 1,
    participates_sales_commission TINYINT(1) NOT NULL DEFAULT 1,
    participates_agent_settlement TINYINT(1) NOT NULL DEFAULT 1,
    effective_from DATETIME(3) NULL,
    effective_to DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_catalog_device_versions (product_id, version_no),
    KEY idx_catalog_device_versions_status (product_id, lifecycle_status),
    KEY idx_catalog_device_versions_effective (effective_from, effective_to)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_order_shipping (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_id BIGINT UNSIGNED NOT NULL,
    recipient_name VARCHAR(128) NOT NULL,
    recipient_phone VARCHAR(64) NOT NULL,
    province VARCHAR(64) NOT NULL DEFAULT '',
    city VARCHAR(64) NOT NULL DEFAULT '',
    district VARCHAR(64) NOT NULL DEFAULT '',
    address VARCHAR(255) NOT NULL DEFAULT '',
    full_address VARCHAR(512) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_biz_order_shipping_order (order_id),
    KEY idx_biz_order_shipping_phone (recipient_phone)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS biz_order_devices (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_id BIGINT UNSIGNED NOT NULL,
    order_item_id BIGINT UNSIGNED NOT NULL,
    device_id BIGINT UNSIGNED NOT NULL,
    sn_snapshot VARCHAR(128) NOT NULL,
    sku_snapshot VARCHAR(96) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'reserved',
    shipment_id BIGINT UNSIGNED NULL,
    reserved_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    hold_expires_at DATETIME(3) NULL,
    shipped_at DATETIME(3) NULL,
    delivered_at DATETIME(3) NULL,
    returned_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_biz_order_devices_order_device (order_id, device_id),
    KEY idx_biz_order_devices_device (device_id, status),
    KEY idx_biz_order_devices_order (order_id, status),
    KEY idx_biz_order_devices_shipment (shipment_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS crm_agent_exit_records (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    record_key VARCHAR(96) NOT NULL,
    organization_id BIGINT UNSIGNED NOT NULL,
    agent_code VARCHAR(64) NOT NULL DEFAULT '',
    agent_name VARCHAR(160) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'completed',
    transferred_customer_count BIGINT NOT NULL DEFAULT 0,
    active_device_count BIGINT NOT NULL DEFAULT 0,
    open_rma_count BIGINT NOT NULL DEFAULT 0,
    unsettled_earning_count BIGINT NOT NULL DEFAULT 0,
    unsettled_earning_amount_cents BIGINT NOT NULL DEFAULT 0,
    open_settlement_batch_count BIGINT NOT NULL DEFAULT 0,
    nonzero_resource_account_count BIGINT NOT NULL DEFAULT 0,
    finalized_by_user_id BIGINT UNSIGNED NULL,
    finalized_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_crm_agent_exit_records_key (record_key),
    KEY idx_crm_agent_exit_records_agent (organization_id, finalized_at),
    KEY idx_crm_agent_exit_records_operator (finalized_by_user_id, finalized_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
INSERT IGNORE INTO crm_agent_exit_records (
    record_key, organization_id, agent_code, agent_name, status,
    transferred_customer_count, active_device_count, open_rma_count,
    unsettled_earning_count, unsettled_earning_amount_cents,
    open_settlement_batch_count, nonzero_resource_account_count,
    finalized_by_user_id, finalized_at, created_at
)
SELECT
    record_key,
    CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.organization_id')) AS UNSIGNED),
    COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.agent_code')), ''),
    COALESCE(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.agent_name')), title),
    'completed',
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.transferred_customer_count')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.active_device_count')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.open_rma_count')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.unsettled_earning_count')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.unsettled_earning_amount_cents')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.open_settlement_batch_count')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.nonzero_resource_account_count')) AS SIGNED), 0),
    COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(payload_json, '$.finalized_by_user_id')) AS UNSIGNED), created_by_user_id),
    created_at,
    created_at
FROM sys_feature_records
WHERE feature_key='agent-exit'
  AND JSON_EXTRACT(payload_json, '$.organization_id') IS NOT NULL
