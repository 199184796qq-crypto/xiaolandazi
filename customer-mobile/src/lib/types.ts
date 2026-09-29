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
