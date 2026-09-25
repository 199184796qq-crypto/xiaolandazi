export interface Actor {
  user_id: number;
  username: string;
  display_name?: string;
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
  name: string;
  status: string;
  device_online?: boolean;
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

export interface ShopOrder {
  id: number;
  order_no?: string;
  status: string;
  total_amount_cents?: number;
  paid_amount_cents?: number;
  created_at?: string;
}

export interface AgentChatResponse {
  reply: string;
  capabilities: string[];
  model?: string;
  latency_ms?: number;
}
