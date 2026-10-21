import type { LiveRuntimeSnapshot } from './types'
import { userFacingErrorText } from './errorDiagnostic'

interface RuntimeDiagnostic {
  stage?: string
  action?: string
  request_id?: string
  http_status?: number
}

export function liveRuntimeFailureMessage(snapshot: LiveRuntimeSnapshot): string {
  const failure = snapshot.failure
  if (failure && failure.session_id === snapshot.session?.id) {
    const detail = failure as typeof failure & RuntimeDiagnostic
    const lines = [userFacingErrorText(failure.message, '') || userFacingErrorText(failure.code)]
    const stage = userFacingErrorText(detail.stage, '')
    const action = userFacingErrorText(detail.action, '')
    if (stage) lines.push(`发生环节：${stage}`)
    if (action) lines.push(`处理建议：${action}`)
    lines.push(`场次编号：${failure.session_id}`)
    return lines.join('\n')
  }
  if (snapshot.agent_state === 'stopped' && snapshot.agent_stop_reason === 'system_error') {
    return `直播已停止，后台未返回具体失败详情。\n发生环节：运行状态\n场次编号：${snapshot.session?.id || '未知'}\n请联系管理员按场次编号核查，当前信息不足以判断具体故障原因。`
  }
  return ''
}

// The first read restores history. Only an observed transition or a new
// session's failure represents a fresh stop that should interrupt the user.
export function isNewLiveRuntimeFailure(previous: LiveRuntimeSnapshot | null, snapshot: LiveRuntimeSnapshot): boolean {
  if (!previous || !liveRuntimeFailureMessage(snapshot)) return false
  return previous.session?.id !== snapshot.session?.id || !liveRuntimeFailureMessage(previous)
}
