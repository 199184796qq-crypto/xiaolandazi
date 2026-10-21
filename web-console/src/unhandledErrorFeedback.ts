import { showAlert } from './uiFeedback'
import { browserFailureDiagnostic } from './errorDiagnostic'

export function installUnhandledErrorFeedback() {
  const onError = (event: ErrorEvent) => {
    if (event.message || event.error) showAlert(browserFailureDiagnostic(event.error || event.message, event.filename, event.lineno, event.colno), '页面错误')
  }
  const onRejection = (event: PromiseRejectionEvent) => {
    if (event.reason instanceof Error && event.reason.name === 'AbortError') return
    showAlert(browserFailureDiagnostic(event.reason), '异步操作错误')
  }
  window.addEventListener('error', onError)
  window.addEventListener('unhandledrejection', onRejection)
  return () => {
    window.removeEventListener('error', onError)
    window.removeEventListener('unhandledrejection', onRejection)
  }
}
