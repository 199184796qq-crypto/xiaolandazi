import { getPublicSystemConfig } from './api'

// Public branding applies before login, and is independent of the active route.
export function installBrowserTitle(load = getPublicSystemConfig) {
  let revision = 0, disposed = false
  async function refresh() {
    const token = ++revision
    try {
      const config = await load()
      const title = config.browser_title?.trim()
      if (!disposed && token === revision && title && [...title].length <= 100 && !/[\u0000-\u001f\u007f-\u009f]/.test(title)) {
        document.title = title
      }
    } catch {
      // Keep the last valid title (or index.html fallback) during service outages.
    }
  }
  function visibleRefresh() { if (document.visibilityState === 'visible') void refresh() }
  window.addEventListener('system-config-updated', refresh)
  window.addEventListener('focus', visibleRefresh)
  document.addEventListener('visibilitychange', visibleRefresh)
  void refresh()
  return () => {
    disposed = true
    revision++
    window.removeEventListener('system-config-updated', refresh)
    window.removeEventListener('focus', visibleRefresh)
    document.removeEventListener('visibilitychange', visibleRefresh)
  }
}
