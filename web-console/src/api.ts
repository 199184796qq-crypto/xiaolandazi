import type {
  AdminAuditLog,
  AdminCustomer,
  Bootstrap,
  CreateRoomPayload,
  Room,
  RoomEvent,
} from './types'

interface ListResponse<T> {
  items: T[]
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, {
    credentials: 'include',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  })

  if (!response.ok) {
    let message = '请求失败'
    try {
      const body = await response.json() as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Keep the fallback message.
    }
    throw new Error(message)
  }

  if (response.status === 204) {
    return undefined as T
  }

  return response.json() as Promise<T>
}

export function getBootstrap() {
  return request<Bootstrap>('/api/v1/bootstrap')
}

export function getRooms(tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<ListResponse<Room>>('/api/v1/rooms' + query)
}

export function createRoom(payload: CreateRoomPayload) {
  return request<Room>('/api/v1/rooms', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteRoom(roomId: number) {
  return request<void>('/api/v1/rooms/' + roomId, {
    method: 'DELETE',
  })
}

export function getRoom(roomId: number) {
  return request<Room>('/api/v1/rooms/' + roomId)
}

export function getRoomEvents(roomId: number, limit = 200) {
  return request<ListResponse<RoomEvent>>(
    '/api/v1/rooms/' + roomId + '/events?limit=' + limit,
  )
}
export function login(payload: {
  username: string
  password: string
  captcha: string
}) {
  return request<{ actor: import('./types').Actor }>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function register(payload: {
  username: string
  display_name: string
  password: string
  confirm_password: string
  captcha: string
}) {
  return request<{ actor: import('./types').Actor }>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function logout() {
  return request<void>('/api/v1/auth/logout', {
    method: 'POST',
  })
}

export function changePassword(payload: {
  current_password: string
  new_password: string
  confirm_password: string
}) {
  return request<{ ok: boolean }>('/api/v1/auth/change-password', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAdminCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/admin/customers')
}

export function adminResetCustomerPassword(
  userId: number,
  payload: {
    new_password: string
    confirm_password: string
  },
) {
  return request<{ ok: boolean; customer: AdminCustomer }>(
    '/api/v1/admin/customers/' + userId + '/reset-password',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function adminDeleteCustomer(userId: number) {
  return request<void>('/api/v1/admin/customers/' + userId, {
    method: 'DELETE',
  })
}

export function getAdminAuditLogs(limit = 50) {
  return request<ListResponse<AdminAuditLog>>(
    '/api/v1/admin/audit-logs?limit=' + encodeURIComponent(String(limit)),
  )
}