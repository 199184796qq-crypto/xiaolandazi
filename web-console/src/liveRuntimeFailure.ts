import type { LiveRuntimeSnapshot } from './types'

export function liveRuntimeFailureMessage(snapshot: LiveRuntimeSnapshot): string {
  const failure = snapshot.failure
  if (failure && failure.session_id === snapshot.session?.id) return failure.message
  if (snapshot.agent_state === 'stopped' && snapshot.agent_stop_reason === 'system_error') {
    return '本场声音生成发生错误，已停止。请重新开始；如仍失败，请联系后台。'
  }
  return ''
}

// The first read restores history. Only an observed transition or a new
// session's failure represents a fresh stop that should interrupt the user.
export function isNewLiveRuntimeFailure(previous: LiveRuntimeSnapshot | null, snapshot: LiveRuntimeSnapshot): boolean {
  if (!previous || !liveRuntimeFailureMessage(snapshot)) return false
  return previous.session?.id !== snapshot.session?.id || !liveRuntimeFailureMessage(previous)
}
