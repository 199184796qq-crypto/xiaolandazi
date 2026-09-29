import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'
import './uiSettings'
import { unlockSharedAudioContext } from './audioRuntime'

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
createApp(App).use(router).mount('#app')
