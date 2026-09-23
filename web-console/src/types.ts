export interface StaffFinanceCustomerSummary {
  tenant_id: number
  user_id: number
  username: string
  display_name: string
  phone: string
  parent_org_name: string
  parent_org_type: string
  cash_balance_cents: number
  reward_balance_cents: number
}

export interface StaffFinanceTaskSummary {
  id: number
  policy_id?: number
  operation_code: string
  requester_user_id: number
  requester_name: string
  approver_user_id?: number
  approver_name?: string
  tenant_id: number
  customer_name: string
  amount_yuan: number
  status: string
  reason: string
  approver_role_code: string
  created_at: string
  decided_at?: string
}

export interface StaffFinanceOperationResult {
  task: StaffFinanceTaskSummary
  requires_approval: boolean
  applied: boolean
}

export interface StaffFinanceOverview {
  customers: StaffFinanceCustomerSummary[]
  tasks: StaffFinanceTaskSummary[]
}
export interface StaffAccessContext {
  is_super_admin: boolean
  employee_id?: number
  primary_group_id?: number
  primary_group_code?: string
  primary_group_name?: string
  role_codes: string[]
  permissions: string[]
  permission_scopes: Record<string, string>
  managed_group_ids: number[]
}

export interface StaffGroupSummary {
  id: number
  code: string
  name: string
  description: string
  status: string
  sort_order: number
  system_managed: boolean
  member_count: number
  manager_count: number
  created_at: string
}

export interface StaffPermissionSummary {
  id: number
  code: string
  module: string
  action: string
  description: string
}

export interface StaffRoleSummary {
  id: number
  group_id: number
  group_code: string
  group_name: string
  code: string
  name: string
  description: string
  is_group_manager: boolean
  default_scope_type: string
  status: string
  permissions: StaffPermissionSummary[]
  permission_codes: string[]
  created_at: string
}

export interface StaffEmployeeRoleSummary {
  role_id: number
  code: string
  name: string
  scope_type: string
  is_group_manager: boolean
}

export interface StaffEmployeeSummary {
  id: number
  user_id: number
  employee_no: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  primary_group_id: number
  primary_group_code: string
  primary_group_name: string
  employment_status: string
  user_status: string
  roles: StaffEmployeeRoleSummary[]
  created_at: string
}

export interface StaffApprovalPolicySummary {
  id: number
  code: string
  name: string
  operation_code: string
  mode: string
  threshold_amount: number
  approver_role_code: string
  status: string
}

export interface StaffDashboard {
  access: StaffAccessContext
  groups: StaffGroupSummary[]
  roles: StaffRoleSummary[]
  permissions: StaffPermissionSummary[]
  employees: StaffEmployeeSummary[]
  approval_policies: StaffApprovalPolicySummary[]
}
export interface InitialCredential {
  initial_password: string
  login_url: string
  delivery_method: 'copy' | 'email' | string
  email?: string
  email_sent: boolean
  email_error?: string
}
export interface Actor {
  user_id: number
  username: string
  role: string
  must_change_password: boolean
  phone: string
  province: string
  city: string
  district: string
  tenant_id?: number
  display_name: string
  avatar_url?: string
}
export interface Tenant {
  id: number
  code: string
  name: string
  status: string
  created_at: string
}

export interface Bootstrap {
  actor: Actor
  staff_access?: StaffAccessContext | null
  tenants: Tenant[]
  environment: string
}

export interface Room {
  id: number
  tenant_id: number
  platform: string
  external_room_id: string
  source_url?: string
  name: string
  status: 'pending' | 'connecting' | 'live' | 'offline' | 'error' | string
  collector_mode: string
  online_count: number
  last_event_at?: string
  created_at: string
  updated_at: string
}

export interface RoomEvent {
  id: number
  tenant_id: number
  room_id: number
  event_type: string
  user_id?: string
  nickname?: string
  content?: string
  occurred_at: string
  payload?: unknown
}

export interface CreateRoomPayload {
  tenant_id?: number
  platform: string
  external_room_id: string
  name: string
  collector_mode: string
}

export interface AdminCustomer {
  user_id: number
  tenant_id: number
  username: string
  display_name: string
  avatar_url?: string
  phone: string
  email: string
  status: string
  source_type: string
  parent_org_id: number
  parent_org_name: string
  parent_org_type: string
  inviter_user_id: number
  inviter_username: string
  inviter_display_name: string
  sales_staff_id: number
  sales_user_id: number
  sales_employee_code: string
  sales_username: string
  sales_display_name: string
  created_at: string
}
export interface AdminAuditLog {
  id: string
  occurred_at: string
  actor_user_id: number
  actor_username: string
  action: string
  target_user_id?: number
  target_username?: string
  target_tenant_id?: number
  http_method?: string
  path?: string
  client_ip?: string
  result: string
}
export interface AccountProfile {
  user_id: number
  tenant_id?: number
  username: string
  display_name: string
  avatar_url: string
  role: string
  phone: string
  email: string
  qq: string
  wechat: string
  province: string
  city: string
  district: string
  address: string
  status: string
  created_at: string
}
export interface AuthSessionSummary {
  id: number
  client_ip: string
  user_agent: string
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}
export interface MembershipSummary {
  id: number
  plan_id: number
  plan_version_id: number
  plan_name: string
  status: string
  cycle_start_at: string
  cycle_end_at: string
  included_seconds: number
  auto_renew: boolean
}

export interface QuotaSummary {
  membership_seconds: number
  purchased_seconds: number
  reward_seconds: number
  total_seconds: number
}

export interface AccountDashboard {
  profile: AccountProfile
  membership?: MembershipSummary
  quota: QuotaSummary
}

export interface WalletLedgerRecord {
  id: number
  direction: string
  amount_cents: number
  balance_before_cents: number
  balance_after_cents: number
  business_type: string
  order_no?: string
  reason?: string
  occurred_at: string
}

export interface RechargeRecord {
  id: number
  recharge_no: string
  requested_amount_cents: number
  credited_amount_cents: number
  payment_method: string
  status: string
  paid_at?: string
  created_at: string
}

export interface PurchaseRecord {
  id: number
  order_no: string
  order_type: string
  status: string
  list_amount_cents: number
  discount_amount_cents: number
  paid_amount_cents: number
  refunded_amount_cents: number
  paid_at?: string
  created_at: string
}

export interface RefundRecord {
  id: number
  refund_no: string
  source_type: string
  source_id: number
  refund_amount_cents: number
  refund_method: string
  status: string
  reason: string
  processed_at?: string
  created_at: string
}

export interface FinanceDashboard {
  cash_balance_cents: number
  reward_balance_cents: number
  total_balance_cents: number
  month_spent_cents: number
  available_seconds: number
  membership_name?: string
  ledger: WalletLedgerRecord[]
  recharges: RechargeRecord[]
  purchases: PurchaseRecord[]
  refunds: RefundRecord[]
  payments: SandboxPaymentRecord[]
}
export interface CommercialMembershipVersion {
  id: number
  plan_id: number
  version_no: number
  lifecycle_status: string
  currency: string
  price_cents: number
  billing_period_unit: string
  billing_period_count: number
  included_seconds: number
  default_time_card_discount_bps: number
  allow_auto_renew: boolean
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
}

export interface CommercialMembershipPlan {
  id: number
  code: string
  name: string
  description: string
  status: string
  sort_order: number
  created_at: string
  updated_at: string
  latest_version?: CommercialMembershipVersion
  active_version?: CommercialMembershipVersion
  draft_version?: CommercialMembershipVersion
}

export interface CommercialMembershipInput {
  code: string
  name: string
  description: string
  sort_order: number
  price_cents: number
  included_seconds: number
  default_time_card_discount_bps: number
  allow_auto_renew: boolean
}
export interface AgentSummary {
  organization_id: number
  code: string
  name: string
  status: string
  created_at: string
  admin_user_id: number
  username: string
  display_name: string
  phone: string
  email: string
  user_status: string
  customer_count: number
}
export interface InviteCodeSummary {
  id: number
  code: string
  owner_user_id: number
  owner_tenant_id?: number
  owner_role: string
  owner_username: string
  owner_name: string
  status: string
  max_uses: number
  used_count: number
  expires_at?: string
  created_at: string
}

export interface InvitePreview {
  code: string
  inviter_name: string
  inviter_role: string
  source_type: string
}

export interface InvitationRecord {
  id: number
  invite_code_id: number
  invite_code: string
  inviter_user_id: number
  inviter_tenant_id?: number
  inviter_username: string
  inviter_display_name: string
  referred_user_id: number
  referred_tenant_id: number
  referred_username: string
  referred_display_name: string
  source_type: string
  parent_org_id: number
  parent_org_name: string
  bound_at: string
}

export interface InvitationDashboard {
  my_code: InviteCodeSummary
  codes: InviteCodeSummary[]
  records: InvitationRecord[]
}
export interface ResourceAccount {
  id: number
  organization_id: number
  resource_type: string
  unit: string
  balance: number
  reserved: number
  status: string
  updated_at: string
}

export interface ResourceLedgerEntry {
  id: number
  organization_id: number
  counterparty_org_id?: number
  resource_type: string
  change_quantity: number
  balance_before: number
  balance_after: number
  business_type: string
  operator_user_id?: number
  reason: string
  created_at: string
}

export interface ResourceDashboard {
  organization_id: number
  organization: string
  org_type: string
  accounts: ResourceAccount[]
  ledger: ResourceLedgerEntry[]
}
export interface SalesStaffSummary {
  staff_id: number
  user_id: number
  employee_code: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  status: string
  team_id?: number
  team_name: string
  customer_count: number
  created_at: string
}


export interface CommercialTimeCardVersion {
  id: number
  product_id: number
  version_no: number
  lifecycle_status: string
  currency: string
  price_cents: number
  duration_seconds: number
  validity_days: number
  participates_referral: boolean
  participates_sales_commission: boolean
  participates_agent_settlement: boolean
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
}

export interface CommercialTimeCardProduct {
  id: number
  code: string
  name: string
  description: string
  status: string
  sort_order: number
  created_at: string
  updated_at: string
  latest_version?: CommercialTimeCardVersion
  active_version?: CommercialTimeCardVersion
  draft_version?: CommercialTimeCardVersion
}

export interface CommercialTimeCardInput {
  code: string
  name: string
  description: string
  sort_order: number
  price_cents: number
  duration_seconds: number
  validity_days: number
  participates_referral: boolean
  participates_sales_commission: boolean
  participates_agent_settlement: boolean
}

export interface CustomerTimeCardOffer {
  id: number
  code: string
  name: string
  description: string
  duration_seconds: number
  validity_days: number
  original_price_cents: number
  discount_bps: number
  sale_price_cents: number
  version_no: number
}

export interface CommercialDeviceVersion {
  id: number
  product_id: number
  version_no: number
  lifecycle_status: string
  currency: string
  list_price_cents: number
  sale_price_cents: number
  participates_referral: boolean
  participates_sales_commission: boolean
  participates_agent_settlement: boolean
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
}

export interface CommercialDeviceProduct {
  id: number
  code: string
  sku_code: string
  name: string
  description: string
  status: string
  sort_order: number
  available_stock: number
  created_at: string
  updated_at: string
  latest_version?: CommercialDeviceVersion
  active_version?: CommercialDeviceVersion
  draft_version?: CommercialDeviceVersion
}

export interface CommercialDeviceInput {
  code: string
  sku_code: string
  name: string
  description: string
  sort_order: number
  list_price_cents: number
  sale_price_cents: number
  participates_referral: boolean
  participates_sales_commission: boolean
  participates_agent_settlement: boolean
}

export interface CustomerDeviceOffer {
  id: number
  code: string
  sku_code: string
  name: string
  description: string
  original_price_cents: number
  sale_price_cents: number
  discount_bps: number
  version_no: number
  available_stock: number
}

export interface FeatureRecord {
  id: number
  feature_key: string
  record_key: string
  title: string
  status: string
  sort_order: number
  payload_json: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface FeatureRecordInput {
  record_key: string
  title: string
  status: string
  sort_order: number
  payload_json: string
}


export interface InventoryWarehouse {
  id: number
  code: string
  name: string
  organization_id?: number
  status: string
  created_at: string
  updated_at: string
}

export interface InventoryDeviceProduct {
  id: number
  code: string
  sku_code: string
  name: string
  status: string
}

export interface InventoryBatchInboundInput {
  product_id: number
  batch_no: string
  purchase_no: string
  warehouse_id: number
  expected_quantity: number
  purchase_amount_cents: number
  supplier_name: string
  payment_method: string
  sns: string[]
  quality_status: string
  reason: string
}

export interface InventoryBatchInboundResult {
  document_id: number
  document_no: string
  product_id: number
  product_name: string
  sku_code: string
  batch_no: string
  purchase_no: string
  expected_quantity: number
  actual_quantity: number
  purchase_amount_cents: number
  supplier_name: string
  payment_method: string
  device_ids: number[]
  sns: string[]
}

export interface InventoryDevice {
  id: number
  sn: string
  sku_code: string
  batch_no: string
  owner_org_id?: number
  custody_warehouse_id?: number
  custody_warehouse: string
  current_customer_id?: number
  lifecycle_status: string
  quality_status: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface InventoryDeviceLedgerEntry {
  id: number
  device_id: number
  document_id: number
  document_no: string
  action: string
  from_status: string
  to_status: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  from_owner_org_id?: number
  to_owner_org_id?: number
  from_customer_id?: number
  to_customer_id?: number
  operator_user_id?: number
  reason: string
  created_at: string
}

export interface InventoryStockDocument {
  id: number
  document_no: string
  document_type: string
  status: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  counterparty_org_id?: number
  reference_no: string
  product_id?: number
  product_name: string
  expected_quantity: number
  actual_quantity: number
  business_amount_cents: number
  counterparty_name: string
  payment_method: string
  reason: string
  operator_user_id?: number
  approved_by_user_id?: number
  effective_at?: string
  created_at: string
  item_count: number
}

export interface InventorySummary {
  warehouse_id: number
  warehouse_name: string
  lifecycle_status: string
  quantity: number
}

export interface InventoryRMA {
  id: number
  rma_no: string
  device_id: number
  device_sn: string
  service_type: string
  status: string
  source_type: string
  source_user_id?: number
  source_tenant_id?: number
  source_agent_org_id?: number
  customer_name: string
  contact_phone: string
  issue: string
  resolution: string
  replacement_device_id?: number
  source_order_id?: number
  source_order_no: string
  source_shipment_id?: number
  source_shipment_no: string
  return_shipment_id?: number
  return_shipment_no: string
  outbound_shipment_id?: number
  outbound_shipment_no: string
  repair_outbound_shipment_id?: number
  repair_outbound_shipment_no: string
  repair_return_shipment_id?: number
  repair_return_shipment_no: string
  operator_user_id?: number
  accepted_by_user_id?: number
  accepted_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export interface InventoryRMAPage {
  items: InventoryRMA[]
  total: number
  open_total: number
  page: number
  page_size: number
}

export interface AfterSalesRequestPage {
  items: InventoryRMA[]
  total: number
  page: number
  page_size: number
}

export interface RMAEvent {
  id: number
  rma_id: number
  event_code: string
  status: string
  title: string
  description: string
  customer_visible: boolean
  operator_user_id?: number
  occurred_at: string
}

export interface RMACost {
  id: number
  cost_no: string
  rma_id: number
  cost_type: string
  amount_cents: number
  counterparty_name: string
  payment_method: string
  note: string
  operator_user_id?: number
  occurred_at: string
  created_at: string
}

export interface CreateRMACostInput {
  cost_type: string
  amount_cents: number
  counterparty_name: string
  payment_method: string
  note: string
}

export interface AfterSalesRequestInput {
  sn: string
  service_type: string
  customer_name: string
  contact_phone: string
  issue: string
}

export interface InventoryCreateDeviceInput {
  sn: string
  sku_code: string
  batch_no: string
  owner_org_id?: number
  warehouse_id: number
  quality_status: string
  reason: string
}

export interface InventoryDeviceTransitionInput {
  to_status: string
  to_warehouse_id?: number
  to_owner_org_id?: number
  to_customer_id?: number
  reason: string
  reference_no: string
}

export interface InventoryCreateRMAInput {
  device_id: number
  service_type: string
  customer_name: string
  contact_phone: string
  issue: string
}

export interface InventoryCompleteRMAInput {
  resolution: string
  to_status: string
  to_warehouse_id?: number
  replacement_device_id?: number
}


export interface SalesPerformanceSummary {
  sales_staff_id: number
  user_id: number
  employee_code: string
  display_name: string
  team_name: string
  paid_order_count: number
  customer_count: number
  paid_amount_cents: number
  refunded_amount_cents: number
  net_revenue_cents: number
  earning_amount_cents: number
  pending_earning_cents: number
  settled_earning_cents: number
}

export interface SalesPerformanceResponse {
  period: string
  period_start: string
  period_end: string
  items: SalesPerformanceSummary[]
}


export interface IncentiveRule {
  id: number
  program_version_id: number
  priority: number
  event_type: string
  conditions_json: string
  action_type: string
  action_config_json: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface IncentiveProgramVersion {
  id: number
  program_id: number
  version_no: number
  lifecycle_status: string
  pending_days: number
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
  rules: IncentiveRule[]
}

export interface IncentiveProgram {
  id: number
  code: string
  name: string
  program_type: string
  status: string
  description: string
  created_at: string
  updated_at: string
  active_version?: IncentiveProgramVersion
  draft_version?: IncentiveProgramVersion
  latest_version?: IncentiveProgramVersion
}

export interface IncentiveRuleInput {
  priority: number
  event_type: string
  conditions_json: string
  action_type: string
  action_config_json: string
  enabled: boolean
}

export interface IncentiveProgramInput {
  code: string
  name: string
  program_type: string
  description: string
  pending_days: number
  rules: IncentiveRuleInput[]
}

export interface IncentiveEarning {
  id: number
  external_id: string
  beneficiary_type: string
  beneficiary_id: number
  earning_type: string
  source_order_id?: number
  source_refund_id?: number
  program_version_id?: number
  rule_id?: number
  currency: string
  amount_cents: number
  quota_seconds: number
  status: string
  available_at?: string
  created_at: string
  updated_at: string
}

export interface SettlementBatch {
  id: number
  batch_no: string
  beneficiary_type: string
  beneficiary_id: number
  currency: string
  period_start_at: string
  period_end_at: string
  gross_amount_cents: number
  adjustment_amount_cents: number
  settlement_amount_cents: number
  status: string
  created_by_user_id?: number
  approved_by_user_id?: number
  approved_at?: string
  paid_at?: string
  created_at: string
  updated_at: string
  item_count: number
}

export interface SettlementDashboard {
  earnings: IncentiveEarning[]
  batches: SettlementBatch[]
}

export interface CreateSettlementBatchInput {
  beneficiary_type: string
  beneficiary_id: number
  period_start_at: string
  period_end_at: string
}


export interface AgentExitCheck {
  organization_id: number
  active_device_count: number
  open_rma_count: number
  unsettled_earning_count: number
  unsettled_earning_amount_cents: number
  open_settlement_batch_count: number
  nonzero_resource_account_count: number
  active_customer_count: number
  can_exit: boolean
  blockers: string[]
}

export interface AgentExitRecord {
  id: number
  record_key: string
  organization_id: number
  agent_code: string
  agent_name: string
  status: string
  transferred_customer_count: number
  active_device_count: number
  open_rma_count: number
  unsettled_earning_count: number
  unsettled_earning_amount_cents: number
  open_settlement_batch_count: number
  nonzero_resource_account_count: number
  finalized_by_user_id?: number
  finalized_by_name: string
  finalized_at: string
  created_at: string
}


export interface AgentLevel {
  id: number
  code: string
  name: string
  status: string
  entry_fee_cents: number
  included_devices: number
  device_discount_bps: number
  consumer_share_bps: number
  reserve_bps: number
  settlement_cycle: string
  hold_days: number
  oem_enabled: boolean
  note: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface AgentLevelHistory {
  id: number
  agent_tenant_id: number
  agent_name: string
  level_id: number
  level_code: string
  level_name: string
  previous_level_id?: number
  previous_level_name?: string
  status: string
  approved_by_user_id?: number
  approved_at: string
  effective_at: string
  ended_at?: string
  reason: string
  created_at: string
}

export interface AgentContractAttachment {
  id: number
  contract_id: number
  file_name: string
  file_url: string
  content_type: string
  size_bytes: number
  page_order: number
  created_by_user_id?: number
  created_at: string
}
export interface AgentContract {
  id: number
  contract_no: string
  external_contract_no: string
  agent_tenant_id: number
  agent_name: string
  parent_contract_id?: number
  contract_type: string
  status: string
  level_id?: number
  level_name?: string
  level_snapshot_json: string
  starts_on: string
  ends_on?: string
  contract_amount_cents: number
  note: string
  signed_at?: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
  attachments: AgentContractAttachment[]
}


export interface LogisticsShipmentItem {
  id: number
  shipment_id: number
  device_id: number
  sn: string
  sku_code: string
  created_at: string
}

export interface LogisticsEvent {
  id: number
  shipment_id: number
  event_code: string
  status: string
  location: string
  description: string
  operator_user_id?: number
  occurred_at: string
  created_at: string
}

export interface LogisticsShipment {
  id: number
  shipment_no: string
  shipment_type: string
  business_type: string
  business_id?: number
  business_no: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  recipient_customer_id?: number
  recipient_org_id?: number
  recipient_type: string
  delivery_method: string
  logistics_fee_cents: number
  recipient_name: string
  recipient_phone: string
  recipient_address: string
  carrier_code: string
  carrier_name: string
  tracking_no: string
  status: string
  note: string
  operator_user_id?: number
  shipped_at?: string
  delivered_at?: string
  created_at: string
  updated_at: string
  items: LogisticsShipmentItem[]
  events: LogisticsEvent[]
}

export interface CreateLogisticsShipmentInput {
  shipment_type: string
  business_type: string
  business_id?: number
  business_no: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  recipient_customer_id?: number
  recipient_org_id?: number
  recipient_type: string
  delivery_method: string
  logistics_fee_cents?: number
  recipient_name: string
  recipient_phone: string
  recipient_address: string
  carrier_code: string
  carrier_name: string
  tracking_no: string
  device_ids: number[]
  note: string
}

export interface UpdateLogisticsShipmentStatusInput {
  status: string
  location: string
  description: string
}

export interface ScrapDisposalInput {
  amount_cents: number
  buyer_name: string
  payment_method: string
  note: string
}

export interface ScrapDisposal {
  id: number
  disposal_no: string
  device_id: number
  device_sn: string
  amount_cents: number
  buyer_name: string
  payment_method: string
  note: string
  operator_user_id?: number
  disposed_at: string
  created_at: string
}

export interface OperatingFinanceEntry {
  id: number
  entry_no: string
  direction: 'income' | 'expense' | string
  category: string
  amount_cents: number
  currency: string
  business_type: string
  business_id?: number
  business_no: string
  counterparty_name: string
  payment_method: string
  description: string
  operator_user_id?: number
  operator_name: string
  occurred_at: string
  created_at: string
}

export interface TokenPurchase {
  id: number
  purchase_no: string
  provider_name: string
  model_scope: string
  token_quantity: number
  amount_cents: number
  payment_method: string
  invoice_no: string
  purchased_at: string
  note: string
  operator_user_id?: number
  operator_name: string
  created_at: string
}

export interface TokenPurchaseInput {
  provider_name: string
  model_scope: string
  token_quantity: number
  amount_cents: number
  payment_method: string
  invoice_no: string
  purchased_at?: string
  note: string
}

export interface OperatingFinanceOverview {
  total_income_cents: number
  total_expense_cents: number
  month_income_cents: number
  month_expense_cents: number
  entries: OperatingFinanceEntry[]
  token_purchases: TokenPurchase[]
}

export interface CustomerShopOrderItem {
  id: number
  order_id: number
  product_type: string
  product_id: number
  product_version_id: number
  product_name: string
  quantity: number
  duration_seconds: number
  validity_days: number
  unit_list_price_cents: number
  unit_paid_price_cents: number
  discount_bps: number
  created_at: string
}

export interface SandboxPaymentRecord {
  id: number
  payment_no: string
  tenant_id: number
  order_id: number
  order_no: string
  channel: string
  payment_method: string
  currency: string
  expected_amount_cents: number
  input_amount_cents: number
  paid_amount_cents: number
  status: string
  failure_reason: string
  external_trade_no: string
  operator_user_id?: number
  idempotency_key: string
  paid_at?: string
  created_at: string
  updated_at: string
}

export interface CustomerOrderShipping {
  recipient_name: string
  recipient_phone: string
  province: string
  city: string
  district: string
  address: string
  full_address: string
}

export interface CustomerOrderDevice {
  id: number
  order_id: number
  order_item_id: number
  device_id: number
  sn: string
  sku_code: string
  status: string
  shipment_id?: number
  reserved_at: string
  shipped_at?: string
  delivered_at?: string
  returned_at?: string
}

export interface CustomerShopOrder {
  id: number
  order_no: string
  tenant_id: number
  order_type: string
  status: string
  payment_status: string
  fulfillment_status: string
  currency: string
  list_amount_cents: number
  discount_amount_cents: number
  payable_amount_cents: number
  paid_amount_cents: number
  refunded_amount_cents: number
  refundable_amount_cents: number
  refundable_seconds: number
  paid_at?: string
  cancelled_at?: string
  created_at: string
  updated_at: string
  items: CustomerShopOrderItem[]
  payments: SandboxPaymentRecord[]
  shipping?: CustomerOrderShipping
  devices: CustomerOrderDevice[]
  shipments: LogisticsShipment[]
}

export interface CreateCustomerShopOrderInput {
  product_type: string
  product_id: number
  quantity: number
  idempotency_key: string
  recipient_name?: string
  recipient_phone?: string
  province?: string
  city?: string
  district?: string
  address?: string
}

export interface SandboxPayOrderInput {
  amount_cents: number
  simulate_result: 'success' | 'failure'
  idempotency_key: string
}

export interface SandboxRefundOrderInput {
  amount_cents: number
  reason: string
  idempotency_key: string
}
