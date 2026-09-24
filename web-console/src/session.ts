import { reactive } from 'vue'
import { getBootstrap } from './api'
import type { Bootstrap } from './types'

interface SessionState {
  bootstrap: Bootstrap | null
  loading: boolean
  initialized: boolean
  error: string
}

export const session = reactive<SessionState>({
  bootstrap: null,
  loading: false,
  initialized: false,
  error: '',
})

export async function loadSession() {
  session.loading = true
  session.error = ''

  try {
    const bootstrap = await getBootstrap()
    if (!bootstrap?.actor?.user_id || !bootstrap.actor.role) {
      throw new Error('登录信息不完整，请重新登录')
    }
    session.bootstrap = bootstrap
    return session.bootstrap
  } catch (error) {
    session.bootstrap = null
    session.error = error instanceof Error ? error.message : '加载登录信息失败'
    throw error
  } finally {
    session.loading = false
    session.initialized = true
  }
}

export function applySession(bootstrap: Bootstrap) {
  session.bootstrap = bootstrap
  session.loading = false
  session.initialized = true
  session.error = ''
  return session.bootstrap
}

export function clearSession() {
  session.bootstrap = null
  session.initialized = true
  session.error = ''
}
