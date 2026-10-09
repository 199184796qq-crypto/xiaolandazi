import { onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import { request } from './api'

export function useInteractionReviewCount(room: Ref<number | null>, plan: Ref<number | null>) {
  const count = ref(0)
  let epoch = 0, timer: ReturnType<typeof setInterval> | undefined
  async function refresh() {
    const r = room.value, p = plan.value, current = epoch
    if (!r || !p || document.hidden) return
    try {
      const result = await request<{ pending_count: number }>(`/api/v1/live/rooms/${r}/interaction-learning?plan_id=${p}`, { suppressErrorDialog: true })
      if (epoch === current) count.value = result.pending_count || 0
    } catch { /* Keep the last valid count; never fabricate a zero on failure. */ }
  }
  watch([room, plan], () => { epoch++; count.value = 0; void refresh() }, { immediate: true })
  const changed = () => { void refresh() }
  onMounted(() => { timer = setInterval(changed, 30_000); window.addEventListener('interaction-review-updated', changed); document.addEventListener('visibilitychange', changed) })
  onBeforeUnmount(() => { epoch++; if (timer) clearInterval(timer); window.removeEventListener('interaction-review-updated', changed); document.removeEventListener('visibilitychange', changed) })
  return count
}
