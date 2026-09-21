export interface Actor {
  user_id: number
  username: string
  role: 'platform_admin' | 'customer'
  tenant_id?: number
  display_name: string
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
  status: string
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