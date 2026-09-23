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
  feedbackState.alertTitle = String(title || '操作提示').trim() || '操作提示'
  feedbackState.alertMessage = normalized
  feedbackState.alertOpen = true
}

export function dismissAlert() {
  feedbackState.alertOpen = false
}

export function useFeedbackErrorRef(initial = ''): Ref<string> {
  let value = initial
  return customRef<string>((track, trigger) => ({
    get() {
      track()
      return value
    },
    set(next) {
      value = String(next || '')
      trigger()
      if (value.trim()) showAlert(value, '操作未完成')
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
