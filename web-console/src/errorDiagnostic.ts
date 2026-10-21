// Raw diagnostics belong in service logs. All user-facing errors pass through
// the Chinese-only boundary below, including inline text and copied history.
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

function diagnosticLocation(url: string): string {
  try {
    // A query may contain credentials or signed resource addresses. The route
    // keeps room/plan IDs and identifies the failed operation without it.
    return new URL(url, 'http://diagnostic.local').pathname
  } catch {
    return safeDiagnosticText(url.split('?')[0].split('#')[0]) || '未知接口'
  }
}

const unknownReason = '当前操作未完成，后台尚未提供可显示的中文原因。请联系管理员核查。'

function knownErrorReason(raw: string): string {
  const rules: [RegExp, string][] = [
    [/voice_disabled|声音已停用|声音.*被停用/i, '所选声音已停用，请重新选择可用声音。'],
    [/voice_(?:creating|pending)|声音.*制作中/i, '所选声音仍在制作中，请等待制作完成后再使用。'],
    [/voice_(?:failed|creation_failed)|声音.*制作失败/i, '所选声音制作失败，请重新制作或选择其他声音。'],
    [/voice_(?:missing|not_found)|缺少音色编号/i, '所选声音资料不完整，请重新选择声音并保存方案。'],
    [/insufficient[_ .-]?(?:quota|balance)|quota.*exhausted|余额不足|额度已用尽/i, '模型服务可用额度不足，请联系管理员检查账号额度。'],
    [/data_?inspection_?failed|provider_content_rejected|content[_ .-]?(?:rejected|filter)/i, '上游模型服务拒绝了本次内容，请联系管理员核查提交内容。'],
    [/deadline exceeded|timed?\s*out|request_timeout|timeout/i, '服务响应超时，请稍后重试；仍失败时请联系管理员。'],
    [/connection refused|connection reset|no such host|connection_failed/i, '服务连接失败，请联系管理员检查服务连接。'],
    [/failed to fetch|networkerror|network_error|network request failed|load failed/i, '网络连接失败，未收到服务响应，请检查网络后重试。'],
    [/provider_access_denied|invalid[_ .-]?(?:api[_ .-]?)?key|authentication failed/i, '服务授权校验未通过，请联系管理员检查授权配置。'],
    [/provider_request_limited|rate.?limit|too many requests/i, '服务请求过于频繁，请稍后重试或联系管理员检查使用限制。'],
    [/permission denied|access denied|forbidden/i, '当前操作没有访问权限，请联系管理员确认权限。'],
    [/unauthorized|session expired|not authenticated/i, '登录状态已失效，请重新登录。'],
    [/bad gateway|gateway timeout|service unavailable/i, '上游服务暂时无法正常响应，请稍后重试。'],
    [/unexpected token|not valid json|response_parse_failed|syntaxerror/i, '后台返回的数据格式异常，请稍后重试；仍失败时请联系管理员。'],
    [/cannot read propert|is not a function|referenceerror|typeerror/i, '页面读取或处理数据时发生异常，请刷新页面；仍失败时请联系管理员。'],
    [/sqlstate|sql:|mysql|redis|database|\b(?:select|insert|update|delete)\b.*\b(?:from|into|table|set)\b/i, '后台数据处理失败，请联系管理员核查数据服务。'],
    [/tts_failed|cosyvoice|text.to.speech/i, '声音合成失败，请稍后重试或联系管理员检查声音服务。'],
    [/core_request_failed|core handoff/i, '音频交付失败，请联系管理员检查播放服务。'],
    [/audio_storage_failed|oss.*(?:failed|error)/i, '音频保存失败，请联系管理员检查音频存储服务。'],
    [/model_request_failed|provider.*(?:failed|error)/i, '模型调用失败，请联系管理员检查模型服务。'],
  ]
  return rules.find(([pattern]) => pattern.test(raw))?.[1] || ''
}

export function userFacingErrorText(value: unknown, fallback = unknownReason): string {
  const original = value instanceof Error ? value.message : typeof value === 'string' ? value : ''
  const raw = original.normalize('NFKC')
  const lines = safeDiagnosticText(raw).split(/\r?\n/).flatMap(line => {
    // Technical metadata and stack frames must not survive into the dialog,
    // inline errors, history, or the clipboard, even if supplied in Chinese.
    if (/^\s*(?:错误码|状态码|服务状态|HTTP状态|上游HTTP|上游请求编号|接口请求编号|请求编号|请求位置|发生位置|堆栈|调用栈|文件路径|内部代码|执行语句)\s*[:：]|^\s*at\s|^\s*<|^\s*[\[{]/i.test(line)) return []
    let text = line
      .replace(/\b(?:api[_-]?key|token|authorization|cookie|password|request[_-]?id|signature)\s*[:=]\s*(?:\[[^\]]*\]|"[^"]*"|'[^']*'|\S+)/gi, '')
      .replace(/Bearer\s*\[[^\]]*\]/gi, '')
      .replace(/(?:[A-Za-z]:[\\/]|\/)[^\s，。；、]*|[\w.-]+\.(?:vue|[cm]?jsx?|tsx?|go|py)(?::\d+){0,2}/gi, '')
      .replace(/[“「『"'][A-Za-z][^“”「」『』"'\n]*[”」』"']/g, '所选项目')
      .replace(/\bTTS\b/gi, '声音合成').replace(/\bCore\b/gi, '音频播放服务')
      .replace(/\bOSS\b/gi, '音频存储服务').replace(/\bJSON\b/gi, '数据格式')
      .replace(/\bHTTP\b/gi, '服务响应').replace(/\bWebSocket\b/gi, '实时连接')
      .replace(/\[(?:已脱敏|链接已脱敏|数据库连接凭据已脱敏|密钥已脱敏|请求内容已脱敏)\]/g, '')
      .replace(/[ \t]{2,}/g, ' ').trim()
    // Do not publish fragments left after deleting unknown internal names.
    // Translate recognized failures; otherwise state the information gap.
    if (/\p{L}/u.test(text.replace(/\p{Script=Han}/gu, ''))) text = knownErrorReason(line)
    if (!/\p{Script=Han}/u.test(text) || /^[\s，。；：:、]+$/.test(text)) return []
    return [text.replace(/:/g, '：').replace(/,/g, '，').replace(/;/g, '；')]
  })
  return [...new Set(lines)].join('\n') || knownErrorReason(raw) || fallback
}

function requestOperation(url: string, method: string): string {
  const path = diagnosticLocation(url)
  if (/initialize|init-anchor/.test(path)) return '方案初始化'
  if (/voice-rate/.test(path)) return '语速设置'
  if (/live-runtime|live-sessions/.test(path)) return '直播控制与状态读取'
  if (/model-pool|speech-model/.test(path)) return '模型服务设置'
  if (/voice|speaker/.test(path)) return '声音设置'
  if (/facts/.test(path)) return '事实资料处理'
  if (/live-agent-plan|\/plans/.test(path)) return '直播方案处理'
  if (/bootstrap|login|logout/.test(path)) return '账号登录'
  return method.toUpperCase() === 'GET' ? '数据读取' : '当前操作'
}

export function requestFailureDiagnostic(url: string, method: string, message: string): string {
  const reason = userFacingErrorText(message)
  const location = `发生操作：${requestOperation(url, method)}`
  return reason.includes(location) ? reason : `${reason}\n${location}`
}

export function httpFailureDiagnostic(status: number, _statusText: string, raw: string, _requestID = ''): string {
  let message = '', code = '', stage = ''
  try {
    const parsed = JSON.parse(raw)
    const body = parsed && typeof parsed === 'object' ? parsed : {}
    const detail = body && typeof body.error === 'object' ? body.error : body
    const field = (value: unknown) => typeof value === 'string' ? safeDiagnosticText(value) : ''
    message = field(typeof body.error === 'string' ? body.error : detail?.message || detail?.detail || body.message)
    code = field(detail?.code || body.code)
    stage = field(detail?.stage || body.stage)
  } catch {
    // A gateway may return HTML. Only its short title is diagnostic; never
    // display an HTML page or a request dump as an error body.
    if (/^\s*</.test(raw)) message = safeDiagnosticText(raw.match(/<title[^>]*>([^<]*)<\/title>/i)?.[1] || '')
    else message = safeDiagnosticText(raw)
  }
  const statusReason = status === 401 ? '登录状态已失效，请重新登录。'
    : status === 403 ? '当前操作没有访问权限，请联系管理员确认权限。'
    : status === 404 ? '请求的内容不存在或已移除，请刷新后重试。'
    : status === 409 ? '资料已发生变化，请刷新后再操作。'
    : status === 429 ? '服务请求过于频繁，请稍后重试。'
    : status >= 500 ? '后台服务处理失败，尚未返回具体原因。请稍后重试或联系管理员。'
    : '本次请求未完成，后台尚未返回具体中文原因。请联系管理员核查。'
  const reason = userFacingErrorText(message, '') || knownErrorReason(code) || statusReason
  const lines = [reason]
  const publicStage = userFacingErrorText(stage, '')
  if (publicStage) lines.push(`发生环节：${publicStage}`)
  return lines.join('\n')
}

export function networkFailureDiagnostic(reason: unknown): string {
  return userFacingErrorText(reason, '网络连接失败，未收到服务响应，请检查网络后重试。')
}

export function browserFailureDiagnostic(reason: unknown, _source = '', _line = 0, _column = 0): string {
  return `发生环节：页面运行\n具体原因：${userFacingErrorText(reason)}`
}
