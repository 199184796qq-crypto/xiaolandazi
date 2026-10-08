import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'
import './uiSettings'
import { unlockSharedAudioContext } from './audioRuntime'
import { installBrowserTitle } from './browserTitle'
import { installUnhandledErrorFeedback } from './unhandledErrorFeedback'

const stopBrowserTitle = installBrowserTitle()
const stopErrorFeedback = installUnhandledErrorFeedback()
if (import.meta.hot) import.meta.hot.dispose(stopBrowserTitle)
if (import.meta.hot) import.meta.hot.dispose(stopErrorFeedback)

function unlockBrowserAudioFromFirstGesture() {
  void unlockSharedAudioContext().catch(() => undefined)
}

window.addEventListener('pointerdown', unlockBrowserAudioFromFirstGesture, {
  capture: true,
  passive: true,
})

if (import.meta.env.DEV) {
  import.meta.hot?.on('vite:beforeUpdate', (payload) => {
    const shouldReload = payload.updates.some((update) =>
      update.path.includes('LivePolicyAdminView.vue') ||
      update.path.includes('LiveStrategyEntryView.vue') ||
      update.path.includes('SystemAgentLayer.vue') ||
      update.path.includes('ModulePageNav.vue') ||
      update.path.endsWith('/App.vue'),
    )
    if (shouldReload) {
      window.location.reload()
    }
  })
}
const app = createApp(App)
app.config.errorHandler = (error) => {
  console.error(error)
  window.dispatchEvent(new ErrorEvent('error', { message: 'component error' }))
}
app.use(router).mount('#app')
