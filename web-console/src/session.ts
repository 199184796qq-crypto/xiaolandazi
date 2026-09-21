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
    session.bootstrap = await getBootstrap()
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

export function clearSession() {
  session.bootstrap = null
  session.initialized = true
  session.error = ''
}