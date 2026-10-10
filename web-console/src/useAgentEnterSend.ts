import { computed, reactive, watch } from 'vue'
import { session } from './session'

const preferences = reactive<Record<string, boolean>>({})

// Both conversation modes use the same preference, isolated by tenant and user.
export function useAgentEnterSend() {
  const key = computed(() => {
    const actor = session.bootstrap?.actor
    return actor ? `xiaolan-ui:${actor.tenant_id}:${actor.user_id}:agent-enter-send` : ''
  })
  watch(key, value => {
    if (!value || value in preferences) return
    try { preferences[value] = localStorage.getItem(value) === '1' }
    catch { preferences[value] = false }
  }, { immediate: true, flush: 'sync' })
  return computed({
    get: () => !!preferences[key.value],
    set: value => {
      if (!key.value) return
      preferences[key.value] = value
      try { localStorage.setItem(key.value, value ? '1' : '0') } catch { /* Optional local preference. */ }
    },
  })
}
