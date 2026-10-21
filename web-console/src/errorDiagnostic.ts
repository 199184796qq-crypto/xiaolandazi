// A shared display boundary for HTTP, stream, component and runtime errors.
// Preserve the reason; redact credential values and signed links, never HTML-render it.
export function safeDiagnosticText(value: unknown): string {
  let text = value instanceof Error ? value.message : typeof value === 'string' ? value : ''
  text = text
    .replace(/\bBearer\s+[a-z0-9._~+/=\-]+/gi, 'Bearer [已脱敏]')
    .replace(/\b(?:https?|wss?):\/\/[^\s"'<>）)]+/gi, '[链接已脱敏]')
    .replace(/[^\s:/]+:[^\s@]+@(?:tcp|unix)\([^)]*\)/g, '[数据库连接凭据已脱敏]')
    .replace(/\b(?:sk-[a-z0-9_-]{16,}|LTAI[a-z0-9]{12,})/gi, '[密钥已脱敏]')
    .replace(/((?:"|')?(?:messages|request_body|request|input|audio_data)(?:"|')?\s*[:=]\s*)[\[{][\s\S]*/gi, '$1[请求内容已脱敏]')
    .replace(/((?:"|')?(?:api[_-]?key|access[_-]?key(?:[_-]?secret|[_-]?id)?|secret[_-]?key|password|passwd|token|authorization|cookie|signature|prompt|audio_data)(?:"|')?\s*[:=]\s*)(?:"(?:\\.|[^"\\])*"|'[^']*'|[^\s,;}]+)/gi, '$1[已脱敏]')
    .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f]/g, '').trim()
  return [...text].length > 1600 ? [...text].slice(0, 1600).join('') + '…（详情见后台日志）' : text
}

export function diagnosticLocation(url: string): string {
  try {
    // A query may contain credentials or signed resource addresses. The route
    // keeps room/plan IDs and identifies the failed operation without it.
    return new URL(url, 'http://diagnostic.local').pathname
  } catch {
    return safeDiagnosticText(url.split('?')[0].split('#')[0]) || '未知接口'
  }
}

export function requestFailureDiagnostic(url: string, method: string, message: string): string {
  const reason = safeDiagnosticText(message) || '后台未返回具体错误原因。'
  const location = `请求位置：${method.toUpperCase()} ${diagnosticLocation(url)}`
  return reason.includes(location) ? reason : `${reason}\n${location}`
}

export function httpFailureDiagnostic(status: number, statusText: string, raw: string, requestID = ''): string {
  let message = '', code = '', stage = '', upstreamID = ''
  try {
    const parsed = JSON.parse(raw)
    const body = parsed && typeof parsed === 'object' ? parsed : {}
    const detail = body && typeof body.error === 'object' ? body.error : body
    const field = (value: unknown) => typeof value === 'string' ? safeDiagnosticText(value) : ''
    message = field(typeof body.error === 'string' ? body.error : detail?.message || detail?.detail || body.message)
    code = field(detail?.code || body.code)
    stage = field(detail?.stage || body.stage)
    upstreamID = field(detail?.request_id || body.request_id)
  } catch {
    // A gateway may return HTML. Only its short title is diagnostic; never
    // display an HTML page or a request dump as an error body.
    if (/^\s*</.test(raw)) message = safeDiagnosticText(raw.match(/<title[^>]*>([^<]*)<\/title>/i)?.[1] || '')
    else message = safeDiagnosticText(raw)
  }
  const lines = [message ? [...message].slice(0, 1000).join('') : '后台未返回具体错误原因。']
  if (stage) lines.push(`发生环节：${stage}`)
  if (code) lines.push(`错误码：${code}`)
  lines.push(`HTTP状态：${status}${statusText ? ` ${safeDiagnosticText(statusText)}` : ''}`)
  if (upstreamID) lines.push(`上游请求编号：${upstreamID}`)
  if (requestID) lines.push(`接口请求编号：${safeDiagnosticText(requestID)}`)
  return lines.join('\n')
}

export function networkFailureDiagnostic(reason: unknown): string {
  return `网络请求失败，未收到HTTP响应。\n具体原因：${safeDiagnosticText(reason) || '浏览器未提供原因'}\n错误码：network_error`
}

export function browserFailureDiagnostic(reason: unknown, source = '', line = 0, column = 0): string {
  const message = safeDiagnosticText(reason) || '浏览器未提供具体异常信息。'
  const position = source ? `${diagnosticLocation(source)}${line > 0 ? `:${line}${column > 0 ? `:${column}` : ''}` : ''}` : '当前页面'
  return `发生环节：页面运行\n具体原因：${message}\n发生位置：${position}`
}
