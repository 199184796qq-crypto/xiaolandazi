export interface Actor {
  user_id: number;
  username: string;
  display_name?: string;
  avatar_url?: string;
  role: string;
  tenant_id?: number;
}

export interface Bootstrap {
  actor: Actor;
  [key: string]: unknown;
}

export interface PublicSystemConfig {
  site_name: string;
  internal_agent_name: string;
  client_agent_name: string;
}

export interface ListResponse<T> {
  items: T[];
  has_more?: boolean;
  next_before_id?: number;
}

export interface ResourceAccount {
  id: number;
  organization_id: number;
  resource_type: string;
  unit: string;
  balance: number;
  reserved: number;
  status: string;
  updated_at: string;
}

export interface ResourceDashboard {
  organization_id: number;
  organization: string;
  org_type: string;
  accounts: ResourceAccount[];
  ledger: unknown[];
}

export interface Room {
  id: number;
  tenant_id?: number;
  platform?: string;
  external_room_id?: string;
  source_url?: string;
  name: string;
  status: string;
  collector_mode?: string;
  device_online?: boolean;
  online_count?: number;
}

export interface RoomEvent {
  id: number;
  tenant_id: number;
  room_id: number;
  event_type: string;
  user_id?: string;
  nickname?: string;
  content?: string;
  occurred_at: string;
  payload?: unknown;
}

export interface LiveAgentPlan {
  id: number;
  tenant_id: number;
  name: string;
  description?: string;
  status: string;
  room_count?: number;
}

export interface LiveRuntimeSnapshot {
  agent_state: 'stopped' | 'working' | 'paused' | string;
  agent_mode: 'control' | 'anchor' | string;
  agent_plan_id?: number;
  agent_plan_name?: string;
  agent_working_seconds?: number;
  quota_remaining_seconds?: number;
  room_live?: boolean;
}

export interface LiveQuotaSourceSummary {
  source_type: string;
  source_id?: number;
  source_label: string;
  asset_no?: string;
  remaining_seconds: number;
  expires_at?: string;
}

export interface LiveTimeCardSummary {
  id: number;
  asset_no: string;
  product_name: string;
  status: string;
  original_seconds: number;
  remaining_seconds: number;
  activation_mode: string;
  validity_days: number;
  activation_deadline_at?: string;
  activated_at?: string;
  expires_at?: string;
  purchased_at: string;
}

export interface LiveTimeCardPage {
  items: LiveTimeCardSummary[];
  page: number;
  page_size: number;
  total: number;
}

export interface LiveBillingRoomSummary {
  room_id: number;
  room_name: string;
  session_id: number;
  billed_seconds: number;
  started_at: string;
}

export interface LiveQuotaSummary {
  active_seconds: number;
  active_time_card_seconds: number;
  reserve_time_card_seconds: number;
  reserve_time_card_count: number;
  current?: LiveQuotaSourceSummary;
  sources: LiveQuotaSourceSummary[];
  time_cards: LiveTimeCardSummary[];
  active_billing_rooms: LiveBillingRoomSummary[];
}

export interface RoomSessionStats {
  started_at?: string;
  ended_at?: string;
  resume_pending: boolean;
  reopened_at?: string;
  interrupted_seconds: number;
  live_seconds: number;
  event_count: number;
  entries: number;
  chats: number;
  likes: number;
  follows: number;
  gifts: number;
  order_signals: number;
}

export interface LiveDevice {
	device_name: string;
	display_status: string;
  id: number;
  sn: string;
  sku_code: string;
  lifecycle_status: string;
  tenant_id: number;
  room_id?: number;
  binding_role: string;
  connection_status: string;
  work_status: string;
  stop_reason: string;
  last_heartbeat_at?: string;
}

export interface LiveAddressingOption {
  key: string;
  text: string;
  enabled: boolean;
  probability: number;
  system_default: boolean;
}

export interface LiveAddressingStrategy {
  addressing_mode: 'system' | 'custom' | string;
  addressing: LiveAddressingOption[];
}

export interface CatalogItem {
  id: number;
  name: string;
  description?: string;
  status?: string;
  price_cents?: number;
  current_price_cents?: number;
  duration_seconds?: number;
}

export interface MembershipOffer {
  id: number;
  code: string;
  name: string;
  description: string;
  monthly_price_cents: number;
  recurring_month_discount_bps: number;
  recurring_quarter_discount_bps: number;
  annual_discount_bps: number;
  included_seconds: number;
  time_card_discount_bps: number;
  device_discount_bps: number;
  allow_auto_renew: boolean;
  version_no: number;
}

export interface TimeCardOffer {
  id: number;
  code: string;
  name: string;
  description: string;
  duration_seconds: number;
  validity_days: number;
  activation_mode: string;
  activation_deadline_days: number;
  original_price_cents: number;
  discount_bps: number;
  sale_price_cents: number;
  version_no: number;
}

export interface DeviceOffer {
  id: number;
  code: string;
  sku_code: string;
  name: string;
  description: string;
  image_url: string;
  unit_code: string;
  unit_label: string;
  original_price_cents: number;
  base_sale_price_cents: number;
  sale_price_cents: number;
  membership_discount_bps: number;
  discount_bps: number;
  version_no: number;
  available_stock: number;
}

export interface ShopOrder {
  id: number;
  order_no?: string;
  order_type: string;
  status: string;
  payment_status: string;
  fulfillment_status: string;
  currency: string;
  list_amount_cents: number;
  discount_amount_cents: number;
  payable_amount_cents: number;
  total_amount_cents?: number;
  paid_amount_cents: number;
  refunded_amount_cents: number;
  created_at?: string;
  updated_at?: string;
  items?: Array<{
    id: number;
    product_type: string;
    product_id: number;
    product_name: string;
    quantity: number;
    duration_seconds: number;
    validity_days: number;
    unit_list_price_cents: number;
    unit_paid_price_cents: number;
  }>;
}

export interface CreateShopOrderInput {
  marketing_campaign_id?: number;
  product_type: 'membership' | 'time_card' | 'device';
  product_id: number;
  quantity: number;
  membership_cycle?: 'single_month' | 'recurring_month' | 'quarter' | 'half_year' | 'annual';
  marketing_placement?: 'shop' | 'membership';
  idempotency_key: string;
}

export interface WechatPaymentParams {
  appId: string;
  timeStamp: string;
  nonceStr: string;
  package: string;
  signType: 'RSA';
  paySign: string;
}

export interface WechatPrepayResponse {
  order: ShopOrder;
  authorization_required?: boolean;
  authorization_url?: string;
  payment_no?: string;
  payment_params?: WechatPaymentParams;
}

export interface WechatRechargePrepayResponse {
  recharge: RechargeRecord;
  authorization_required?: boolean;
  authorization_url?: string;
  payment_no?: string;
  payment_params?: WechatPaymentParams;
}

export interface WechatCashRefund {
  id: number; refund_no: string; amount_cents: number; refunded_cents: number;
  released_cents: number; frozen_cents: number; status: string; created_at: string;
  items: { id: number; refund_no: string; recharge_no: string; amount_cents: number; status: string; received_account: string; message: string }[];
}
export interface WechatRefundWallet {
  available_cents: number; frozen_cents: number; refundable_cents: number; records: WechatCashRefund[];
}
export interface FinanceDashboard {
  cash_balance_cents: number;
  reward_balance_cents: number;
  commission_balance_cents: number;
  commission_frozen_cents: number;
  bean_balance: number;
  bean_frozen: number;
  total_balance_cents: number;
  month_spent_cents: number;
  available_seconds: number;
  membership_name?: string;
  ledger: WalletLedgerRecord[];
  recharges: RechargeRecord[];
  purchases: PurchaseRecord[];
  refunds: RefundRecord[];
  payments: PaymentRecord[];
}

export interface WalletLedgerRecord {
  id: number;
  direction: string;
  amount_cents: number;
  balance_before_cents: number;
  balance_after_cents: number;
  business_type: string;
  order_no?: string;
  reason?: string;
  occurred_at: string;
}

export interface RechargeRecord {
  id: number;
  recharge_no: string;
  requested_amount_cents: number;
  credited_amount_cents: number;
  payment_method: string;
  status: string;
  paid_at?: string;
  created_at: string;
}

export interface PurchaseRecord {
  id: number;
  order_no: string;
  order_type: string;
  status: string;
  list_amount_cents: number;
  discount_amount_cents: number;
  paid_amount_cents: number;
  refunded_amount_cents: number;
  paid_at?: string;
  created_at: string;
}

export interface RefundRecord {
  id: number;
  refund_no: string;
  source_type: string;
  source_id: number;
  refund_amount_cents: number;
  refund_method: string;
  status: string;
  reason: string;
  processed_at?: string;
  created_at: string;
}

export interface PaymentRecord {
  id: number;
  payment_no: string;
  order_no: string;
  payment_method: string;
  paid_amount_cents: number;
  status: string;
  paid_at?: string;
  created_at: string;
}

export interface BeanWalletDashboard {
  wallet: {
    id: number;
    available_beans: number;
    frozen_beans: number;
    status: string;
  };
  ledger: BeanLedgerEntry[];
  settings: {
    purchase_beans_per_yuan: number;
    minimum_purchase_cents: number;
    enabled: boolean;
  };
}

export interface BeanLedgerEntry {
  id: number;
  available_delta: number;
  available_before: number;
  available_after: number;
  business_type: string;
  reason: string;
  created_at: string;
}

export interface BeanPurchaseOrder {
  id: number;
  purchase_no: string;
  cash_amount_cents: number;
  credited_beans: number;
  status: string;
  created_at: string;
}

export interface WithdrawalRequest {
  id: number;
  withdrawal_no: string;
  beneficiary_type: string;
  amount_cents: number;
  status: string;
  reject_reason: string;
  requested_at: string;
  approved_at?: string;
  paid_at?: string;
}

export interface ReferralWalletDashboard {
  wallet: {
    available_balance_cents: number;
    frozen_balance_cents: number;
  };
  ledger: BeneficiaryWalletLedger[];
  withdrawals: WithdrawalRequest[];
}

export interface BeneficiaryWalletLedger {
  id: number;
  wallet_id: number;
  external_id: string;
  business_type: string;
  reference_type: string;
  reference_id?: number;
  available_delta_cents: number;
  frozen_delta_cents: number;
  available_before_cents: number;
  available_after_cents: number;
  frozen_before_cents: number;
  frozen_after_cents: number;
  operator_user_id?: number;
  reason: string;
  created_at: string;
}

export interface InviteCodeSummary {
  id: number;
  code: string;
  owner_user_id: number;
  owner_tenant_id?: number;
  owner_role: string;
  owner_username: string;
  owner_name: string;
  status: string;
  max_uses: number;
  used_count: number;
  expires_at?: string;
  created_at: string;
}

export interface InvitePreview {
  code: string;
  inviter_name: string;
  inviter_role: string;
  source_type: string;
}

export interface InvitationRecord {
  id: number;
  invite_code_id: number;
  invite_code: string;
  inviter_user_id: number;
  inviter_tenant_id?: number;
  inviter_username: string;
  inviter_display_name: string;
  referred_user_id: number;
  referred_tenant_id: number;
  referred_username: string;
  referred_display_name: string;
  source_type: string;
  parent_org_id: number;
  parent_org_name: string;
  bound_at: string;
}

export interface InvitationDashboard {
  my_code: InviteCodeSummary;
  codes: InviteCodeSummary[];
  codes_total: number;
  records: InvitationRecord[];
  records_total: number;
  own_referral_count: number;
}

export interface RechargeRequestResult {
  task: { status: string };
  requires_approval: boolean;
  applied: boolean;
}

export interface AgentChatResponse {
  reply: string;
  capabilities: string[];
  model?: string;
  latency_ms?: number;
}

export interface AgentDecisionSimulationResult {
  decision_id: string;
  question: string;
  reply: string;
  execution_mode: string;
  plan_name?: string;
  user_layer_version?: number;
  created_at: string;
}

export interface AgentDecisionItem {
  id: string;
  manual_action?: 'answer' | 'quick' | string;
  manual_origin?: string;
}

export interface AgentDecisionEnqueueResult {
  item?: AgentDecisionItem;
  merged: boolean;
  promoted: boolean;
  suppressed: boolean;
}

export interface AgentDecisionSnapshot {
  room_id: number;
  simulation_results?: AgentDecisionSimulationResult[];
}
export interface MarketingCampaignItem {target_type:string;target_id:number;pricing_mode:string;fixed_price_cents?:number;quantity:number;package_months:number;discount_bps:number}
export interface MarketingCampaign {id:number;name:string;description:string;items:MarketingCampaignItem[];eligible?:boolean;ineligible_reason?:string;controls?:{audience:string;max_claims:number;max_units:number}}
