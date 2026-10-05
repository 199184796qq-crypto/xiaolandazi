export interface Actor {
  user_id: number;
  // Shared bootstrap shape; internal sales users may have no tenant.
  tenant_id?: number | null;
  username: string;
  display_name?: string;
  role: string;
}

export interface StaffAccess {
  is_super_admin?: boolean;
  permissions?: string[];
}

export interface Bootstrap {
  actor: Actor;
  staff_access?: StaffAccess;
  [key: string]: unknown;
}

export interface PublicSystemConfig {
  site_name: string;
  internal_agent_name: string;
  client_agent_name: string;
}

export interface ListResponse<T> {
  items: T[];
}

export interface SalesCustomer {
  user_id: number;
  tenant_id: number;
  username: string;
  display_name: string;
  phone: string;
  email: string;
  status: string;
  source_type: string;
  industry_name: string;
  created_at: string;
}

export interface SalesPerformanceSummary {
  sales_staff_id: number;
  user_id: number;
  employee_code: string;
  display_name: string;
  team_name: string;
  paid_order_count: number;
  customer_count: number;
  paid_amount_cents: number;
  refunded_amount_cents: number;
  net_revenue_cents: number;
  earning_amount_cents: number;
  pending_earning_cents: number;
  settled_earning_cents: number;
}

export interface SalesPerformanceTotals {
  paid_order_count: number;
  customer_count: number;
  paid_amount_cents: number;
  refunded_amount_cents: number;
  net_revenue_cents: number;
  earning_amount_cents: number;
  pending_earning_cents: number;
  settled_earning_cents: number;
}

export interface SalesPerformanceResponse {
  period: string;
  items: SalesPerformanceSummary[];
  totals: SalesPerformanceTotals;
}

export interface AgentChatResponse {
  reply: string;
  capabilities: string[];
  model?: string;
  latency_ms?: number;
}

export interface SalesCommissionDashboard {
  wallet: { available_balance_cents: number; frozen_balance_cents: number };
  earnings: Array<{ id: number; order_no: string; product_name: string; rule_name: string; rule_version_id: number; amount_cents: number; status: string; available_at?: string; created_at: string }>;
  withdrawals: Array<{ id: number; withdrawal_no: string; amount_cents: number; status: string; requested_at: string; reject_reason: string }>;
}
