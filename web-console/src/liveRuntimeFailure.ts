import type { LiveRuntimeSnapshot } from './types'
import { safeDiagnosticText } from './errorDiagnostic'

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
    const lines = [safeDiagnosticText(failure.message)]
    if (detail.stage) lines.push(`发生环节：${safeDiagnosticText(detail.stage)}`)
    if (detail.action) lines.push(`处理建议：${safeDiagnosticText(detail.action)}`)
    lines.push(`场次编号：${failure.session_id}；错误码：${safeDiagnosticText(failure.code)}`)
    if (detail.http_status) lines.push(`上游HTTP：${detail.http_status}`)
    if (detail.request_id) lines.push(`上游请求编号：${safeDiagnosticText(detail.request_id)}`)
    return lines.join('\n')
  }
  if (snapshot.agent_state === 'stopped' && snapshot.agent_stop_reason === 'system_error') {
    return `直播已停止，后台未返回具体失败详情。\n发生环节：运行状态\n场次编号：${snapshot.session?.id || '未知'}；错误码：system_error\n请按场次编号核对后台日志，当前信息不足以判断是否为声音合成错误。`
  }
  return ''
}

// The first read restores history. Only an observed transition or a new
// session's failure represents a fresh stop that should interrupt the user.
export function isNewLiveRuntimeFailure(previous: LiveRuntimeSnapshot | null, snapshot: LiveRuntimeSnapshot): boolean {
  if (!previous || !liveRuntimeFailureMessage(snapshot)) return false
  return previous.session?.id !== snapshot.session?.id || !liveRuntimeFailureMessage(previous)
}
