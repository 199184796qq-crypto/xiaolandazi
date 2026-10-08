import { customRef, reactive, type Ref } from 'vue'

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
})

export function showAlert(message: string, title = '操作提示') {
  const normalized = String(message || '').trim()
  if (!normalized) return
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
  if (normalizedMethod === 'GET' && requestFailures.get(key) === message) return
  requestFailures.set(key, message)
  if (requestFailures.size > 200) requestFailures.delete(requestFailures.keys().next().value!)
  if (normalizedMethod === 'GET') {
    const now = Date.now()
    if (now - lastReadFailureNoticeAt >= 15_000) {
      lastReadFailureNoticeAt = now
      showToast('数据连接波动', '后台正在自动重连，不影响已经保存的内容。', 'warning', 3600)
    }
    return
  }
  showAlert(message, '请求未完成')
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
  let value = initial
  return customRef<string>((track, trigger) => ({
    get() {
      track()
      return value
    },
    set(next) {
      const previous = value
      value = String(next || '')
      trigger()
      if (value.trim() && value !== previous && ![...requestFailures.values()].includes(value)) showAlert(value, '操作未完成')
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
