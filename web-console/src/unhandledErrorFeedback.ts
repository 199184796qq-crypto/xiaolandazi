import { showAlert } from './uiFeedback'

export function installUnhandledErrorFeedback() {
  const onError = (event: ErrorEvent) => {
    if (event.message) showAlert('页面运行发生错误，请刷新页面；若持续出现，请联系后台。', '页面错误')
  }
  const onRejection = (event: PromiseRejectionEvent) => {
    if (event.reason instanceof Error && event.reason.name === 'AbortError') return
    showAlert('操作未能完成，请重试；若持续出现，请联系后台。', '操作错误')
  }
  window.addEventListener('error', onError)
  window.addEventListener('unhandledrejection', onRejection)
  return () => {
    window.removeEventListener('error', onError)
    window.removeEventListener('unhandledrejection', onRejection)
  }
}
