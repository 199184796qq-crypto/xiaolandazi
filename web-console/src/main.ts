import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import './style.css'
import './uiSettings'

if (import.meta.env.DEV) {
  import.meta.hot?.on('vite:beforeUpdate', (payload) => {
    const shouldReload = payload.updates.some((update) =>
      update.path.includes('LivePolicyAdminView.vue') ||
      update.path.includes('LiveStrategyEntryView.vue'),
    )
    if (shouldReload) {
      window.location.reload()
    }
  })
}
createApp(App).use(router).mount('#app')