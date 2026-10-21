import { customRef, reactive, type Ref } from 'vue'
import { requestFailureDiagnostic, safeDiagnosticText, userFacingErrorText } from './errorDiagnostic'

export type ToastTone = 'info' | 'success' | 'warning' | 'error'

export interface ConfirmOptions {
  title: string
  message: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

interface ToastItem {
  id: number
  title: string
  message: string
  tone: ToastTone
}

interface FeedbackState {
  alertOpen: boolean
  alertTitle: string
  alertMessage: string
  confirmOpen: boolean
  confirmOptions: ConfirmOptions
  toasts: ToastItem[]
  errors: { id: number; title: string; message: string; occurredAt: string }[]
}

let nextToastID = 1
let confirmResolver: ((value: boolean) => void) | null = null
const requestFailures = new Map<string, string>()
const pendingAlerts: { title: string; message: string }[] = []
let lastReadFailureNoticeAt = 0

export const feedbackState = reactive<FeedbackState>({
  alertOpen: false,
  alertTitle: '操作提示',
  alertMessage: '',
  confirmOpen: false,
  confirmOptions: {
    title: '确认操作',
    message: '',
    confirmText: '确认',
    cancelText: '取消',
    danger: false,
  },
  toasts: [],
  errors: [],
})

function rememberError(title: string, message: string) {
  const previous = feedbackState.errors.find(item => item.message === message)
  if (previous) return
  feedbackState.errors.unshift({ id: nextToastID++, title, message, occurredAt: new Date().toLocaleString() })
  feedbackState.errors.splice(20)
}

export function clearErrorHistory() { feedbackState.errors.splice(0) }

export function showAlert(message: string, title = '操作提示') {
  if (!message?.trim()) return
  const normalized = userFacingErrorText(message)
  if (!normalized) return
  title = userFacingErrorText(title, '操作提示')
  if (/错误|失败|未完成|停止/.test(title)) rememberError(title, normalized)
  if (feedbackState.alertOpen) {
    if (feedbackState.alertMessage !== normalized && !pendingAlerts.some(item => item.message === normalized)) pendingAlerts.push({ title, message: normalized })
    return
  }
  feedbackState.alertTitle = String(title || '操作提示').trim() || '操作提示'
  feedbackState.alertMessage = normalized
  feedbackState.alertOpen = true
}

// GET is used heavily by background polling. A deployment or brief Core restart
// can make many different reads fail together; never queue one blocking dialog
// per endpoint. Writes still report their exact failure as a modal.
export function showRequestError(url: string, method: string, message: string) {
  const normalizedMethod = method.toUpperCase()
  const key = `${normalizedMethod} ${url}`
  // Use the redacted original only as a private deduplication key: distinct
  // unknown failures may share a Chinese fallback, but are separate events.
  const reason = safeDiagnosticText(message)
  if (normalizedMethod === 'GET' && requestFailures.get(key) === reason) return
  requestFailures.set(key, reason)
  const diagnostic = requestFailureDiagnostic(url, normalizedMethod, reason)
  rememberError('请求未完成', diagnostic)
  if (requestFailures.size > 200) requestFailures.delete(requestFailures.keys().next().value!)
  if (normalizedMethod === 'GET') {
    const now = Date.now()
    if (now - lastReadFailureNoticeAt >= 15_000) {
      lastReadFailureNoticeAt = now
      showToast('数据读取失败', diagnostic, 'warning', 6000)
    }
    return
  }
  showAlert(diagnostic, '请求未完成')
}

export function clearRequestError(url: string, method: string) {
  requestFailures.delete(`${method.toUpperCase()} ${url}`)
}

export function dismissAlert() {
  feedbackState.alertOpen = false
  const next = pendingAlerts.shift()
  if (next) showAlert(next.message, next.title)
}

export function useFeedbackErrorRef(initial = ''): Ref<string> {
  let value = initial ? userFacingErrorText(initial) : ''
  return customRef<string>((track, trigger) => ({
    get() {
      track()
      return value
    },
    set(next) {
      const previous = value
      value = next ? userFacingErrorText(next) : ''
      trigger()
      if (value.trim() && value !== previous && ![...requestFailures.values()].includes(safeDiagnosticText(next))) showAlert(value, '操作未完成')
    },
  }))
}

export function confirmAction(options: ConfirmOptions) {
  if (confirmResolver) {
    confirmResolver(false)
    confirmResolver = null
  }
  feedbackState.confirmOptions = {
    confirmText: '确认',
    cancelText: '取消',
    danger: false,
    ...options,
  }
  feedbackState.confirmOpen = true
  return new Promise<boolean>((resolve) => {
    confirmResolver = resolve
  })
}

export function resolveConfirm(value: boolean) {
  feedbackState.confirmOpen = false
  if (confirmResolver) {
    const resolver = confirmResolver
    confirmResolver = null
    resolver(value)
  }
}

export function showToast(
  title: string,
  message = '',
  tone: ToastTone = 'info',
  duration = 3200,
) {
  if (tone === 'error') {
    showAlert(message || title, message ? title : '操作未完成')
    return 0
  }
  const id = nextToastID++
  if (tone === 'warning') {
    title = userFacingErrorText(title, '操作提示')
    message = message ? userFacingErrorText(message) : ''
  }
  feedbackState.toasts.push({ id, title, message, tone })
  window.setTimeout(() => dismissToast(id), duration)
  return id
}

export function dismissToast(id: number) {
  const index = feedbackState.toasts.findIndex((item) => item.id === id)
  if (index >= 0) feedbackState.toasts.splice(index, 1)
}

export function showPermissionToast(message = '当前账号仅有查看权限，不能执行此操作。') {
  showAlert(message, '无操作权限')
  return 0
}
