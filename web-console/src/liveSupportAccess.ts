import type { Bootstrap, LiveSupportAuthorization } from './types'

export function hasRoomStrategyAuthorization(
  items: LiveSupportAuthorization[],
  userId: number,
  roomId: number,
  tenantId?: number,
): boolean {
  return userId > 0 && roomId > 0 && items.some((item) =>
    item.staff_user_id === userId && item.room_id === roomId &&
    (tenantId === undefined || item.tenant_id === tenantId) &&
    item.capability === 'l3_policy' && item.status === 'active',
  )
}

export function canDeleteOperationsRoom(bootstrap: Bootstrap | null | undefined): boolean {
  if (bootstrap?.actor.role === 'platform_admin') return true
  if (bootstrap?.actor.role !== 'staff') return false
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes('*') || access.permissions.includes('liveops.configure')))
}

// The header identifies an explicit room-scoped support session. The server
// rechecks the current customer grant for every read and write.
let activeSupportScope: { roomId: number; token: symbol } | null = null

export function beginLiveSupportScope(roomId: number): () => void {
  if (!Number.isSafeInteger(roomId) || roomId <= 0) throw new Error('请选择已授权的直播间')
  const token = Symbol('live-support')
  activeSupportScope = { roomId, token }
  return () => {
    if (activeSupportScope?.token === token) activeSupportScope = null
  }
}

export function liveSupportRoomForRequest(url: string): number | null {
  if (!activeSupportScope) return null
  const path = url.split('?')[0] || ''
  const resources = [
    '/api/v1/live-agent-plans',
    '/api/v1/live-anchor-styles',
    '/api/v1/live/rooms',
    '/api/v1/live/agent',
    '/api/v1/live/media-assets',
    '/api/v1/live/voice-profiles',
    '/api/v1/live/voice-model-bindings',
    '/api/v1/live/official-voices',
    '/api/v1/liveops/support/rooms',
  ]
  if (resources.some((resource) => path === resource || path.startsWith(resource + '/')) ||
      /^\/api\/v1\/rooms\/\d+(?:\/|$)/.test(path)) return activeSupportScope.roomId
  return null
}

// Audio elements cannot send the support header. Scope only our own media
// endpoint at playback time, never persisted or externally signed asset URLs.
export function liveSupportMediaPlaybackURL(url: string, origin?: string): string {
  if (!activeSupportScope || !url) return url
  const appOrigin = origin || (typeof window !== 'undefined' ? window.location.origin : '')
  if (!appOrigin) return url
  try {
    const target = new URL(url, appOrigin)
    if (target.origin !== new URL(appOrigin).origin ||
        !/^\/api\/v1\/live\/media-assets\/\d+\/content$/.test(target.pathname)) return url
    target.searchParams.set('support_room_id', String(activeSupportScope.roomId))
    return target.href
  } catch {
    return url
  }
}
