<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
  models: { id: string; name: string }[]
  loading: boolean
  error: string
  truncated: boolean
  initialId: string
}>()
const emit = defineEmits<{ close: []; refresh: []; select: [id: string] }>()
const dialog = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const search = ref(''), page = ref(1), chosenId = ref(props.initialId)
const pageSize = 10
const filtered = computed(() => {
  const query = search.value.trim().toLowerCase()
  return props.models.filter(m => `${m.id} ${m.name}`.toLowerCase().includes(query))
})
const pageCount = computed(() => Math.max(1, Math.ceil(filtered.value.length / pageSize)))
const rows = computed(() => filtered.value.slice((page.value - 1) * pageSize, page.value * pageSize))
const canSelect = computed(() => !props.loading && !props.error && props.models.some(m => m.id === chosenId.value))
watch(search, () => { page.value = 1 })
watch(pageCount, total => { page.value = Math.min(page.value, total) })
let previousFocus: HTMLElement | null = null
let previousOverflow = ''
onMounted(async () => {
  previousFocus = document.activeElement as HTMLElement | null
  previousOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  await nextTick()
  searchInput.value?.focus()
})
onBeforeUnmount(() => {
  document.body.style.overflow = previousOverflow
  if (previousFocus?.isConnected) previousFocus.focus()
})
function keys(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); emit('close'); return }
  if (event.key !== 'Tab') return
  const targets = Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]') || []).filter(el => el.getClientRects().length)
  const first = targets[0], last = targets[targets.length - 1]
  if (!first || !last) { event.preventDefault(); dialog.value?.focus(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) { event.preventDefault(); first.focus() }
}
</script>

<template>
  <Teleport to="body">
    <div class="model-picker-overlay" @click.self="emit('close')">
      <section ref="dialog" class="model-picker" role="dialog" aria-modal="true" aria-label="选择模型" tabindex="-1" @keydown="keys">
        <header class="picker-header">
          <div><small>MODEL LIBRARY</small><h2>选择模型</h2><p>选择服务商返回的模型，确认后填入模型 ID。</p></div>
          <button class="picker-close" type="button" aria-label="关闭模型选择面板" @click="emit('close')">×</button>
        </header>
        <div class="picker-search">
          <label><span>搜索模型</span><input ref="searchInput" v-model="search" type="search" placeholder="搜索模型名称或 ID" /></label>
          <button class="picker-secondary" type="button" :disabled="loading" @click="emit('refresh')">{{ loading ? '正在获取…' : '刷新列表' }}</button>
        </div>
        <div class="picker-table-wrap" :aria-busy="loading">
          <table class="picker-table">
            <thead><tr><th scope="col" class="choice-column">选择</th><th scope="col">模型名称</th><th scope="col">模型 ID</th></tr></thead>
            <tbody>
              <tr v-if="loading"><td colspan="3" class="picker-empty" role="status">正在获取服务商的模型列表…</td></tr>
              <tr v-else-if="error"><td colspan="3" class="picker-empty picker-error" role="alert">{{ error }}<small>可以重试，或关闭面板后手动填写模型 ID。</small></td></tr>
              <tr v-else-if="!rows.length"><td colspan="3" class="picker-empty">{{ models.length ? '没有匹配的模型，请换个关键词。' : '服务商未返回可选模型，可关闭面板后手动填写。' }}</td></tr>
              <tr v-for="m in loading || error ? [] : rows" :key="m.id" :class="{ chosen: chosenId === m.id }" @click="chosenId = m.id">
                <td class="choice-column"><input type="radio" name="available-model" :value="m.id" :checked="chosenId === m.id" :aria-label="`选择 ${m.name}（${m.id}）`" @change="chosenId = m.id" /></td>
                <td class="model-name">{{ m.name }}</td><td class="model-id">{{ m.id }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="picker-pagination">
          <span aria-live="polite">共 {{ filtered.length }} 个{{ search.trim() ? ` / 全部 ${models.length} 个` : '' }} · 每页 {{ pageSize }} 个</span>
          <nav aria-label="模型列表分页"><button type="button" class="picker-secondary" :disabled="loading || page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ pageCount }}</span><button type="button" class="picker-secondary" :disabled="loading || page >= pageCount" @click="page++">下一页</button></nav>
        </div>
        <p v-if="truncated" class="picker-hint">列表较大，仅展示已获取的部分模型，其它模型仍可手动填写 ID。</p>
        <p class="picker-hint">获取和选择不会保存配置或切换默认模型；可调用性请通过“测试连通性”验证。</p>
        <footer class="picker-footer">
          <div><small>已勾选</small><strong>{{ models.some(m => m.id === chosenId) ? chosenId : '暂未选择模型' }}</strong></div>
          <div class="picker-actions"><button class="picker-secondary" type="button" @click="emit('close')">取消</button><button class="picker-primary" type="button" :disabled="!canSelect" @click="emit('select', chosenId)">选中</button></div>
        </footer>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.model-picker-overlay{position:fixed;inset:0;z-index:2400;background:#18243e66;backdrop-filter:blur(5px);display:flex;align-items:center;justify-content:center;padding:24px;box-sizing:border-box}
.model-picker{box-sizing:border-box;width:min(920px,100%);max-height:90dvh;display:flex;flex-direction:column;gap:18px;padding:26px;border:1px solid #e5e9ff;border-radius:24px;background:linear-gradient(145deg,#fff,#f6f8ff);box-shadow:0 24px 80px #15294640;color:#28334d;overflow:hidden}
.picker-header{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.picker-header small{color:#6876d3;letter-spacing:.12em;font-size:12px;font-weight:700}.picker-header h2{margin:6px 0;font-size:26px}.picker-header p{margin:0;color:#7a8499;font-size:15px;line-height:1.6}.picker-close{border:1px solid #e1e6f4;border-radius:12px;background:#fff;color:#6e7890;width:38px;height:38px;font-size:26px;cursor:pointer;flex-shrink:0}
.picker-search{display:flex;gap:12px;align-items:flex-end}.picker-search label{display:grid;gap:8px;flex:1;min-width:0;font-weight:700}.picker-search input{box-sizing:border-box;width:100%;border:1px solid #d8dfed;border-radius:12px;padding:12px 14px;font:inherit;color:inherit;background:#fff}
.picker-table-wrap{overflow:auto;min-height:120px;flex:1;border:1px solid #e0e5f3;border-radius:16px;background:#fff}.picker-table{border-collapse:collapse;width:100%;table-layout:fixed;font-size:16px}.picker-table th{position:sticky;top:0;padding:14px 16px;text-align:left;background:#edf0fc;color:#65708b;font-size:14px;z-index:1}.picker-table td{padding:14px 16px;border-top:1px solid #edf0f7;overflow-wrap:anywhere;line-height:1.5}.picker-table tr:has(input){cursor:pointer}.picker-table tr:has(input):hover{background:#f6f7ff}.picker-table tr.chosen{background:#eef0ff}.choice-column{width:58px;text-align:center!important}.choice-column input{width:18px;height:18px;accent-color:#6264db;cursor:pointer}.model-name{font-weight:700}.model-id{color:#79849d;font-family:ui-monospace,monospace;font-size:14px}.picker-table td.picker-empty{padding:40px 20px;text-align:center;color:#8390a8}.picker-table td.picker-error{color:#b34359}.picker-empty small{display:block;margin-top:8px}
.picker-pagination{display:flex;align-items:center;justify-content:space-between;gap:12px;color:#7a8499;font-size:14px}.picker-pagination nav{display:flex;align-items:center;gap:12px;white-space:nowrap}.choice-column{white-space:nowrap}.picker-secondary,.picker-primary{font:inherit;font-weight:700;border-radius:12px;padding:10px 16px;cursor:pointer;white-space:nowrap}.picker-secondary{border:1px solid #dce2f2;background:#fff;color:#66708b}.picker-primary{border:0;color:#fff;background:linear-gradient(135deg,#7777ef,#5654ce);box-shadow:0 8px 18px #6264db30;min-width:90px}.model-picker button:disabled{opacity:.45;cursor:not-allowed}.model-picker :is(button,input):focus-visible{outline:3px solid #a5acff;outline-offset:3px}.picker-hint{margin:0;color:#8993a7;font-size:13px;line-height:1.6}.picker-footer{display:flex;align-items:center;justify-content:space-between;gap:16px;border-top:1px solid #e3e8f4;padding-top:18px}.picker-footer>div:first-child{display:grid;gap:5px;min-width:0}.picker-footer small{color:#7a8499;font-size:13px}.picker-footer strong{font-size:15px;overflow-wrap:anywhere;color:#5d60c9}.picker-actions{display:flex;gap:10px;flex-shrink:0}
@media(max-width:600px){.model-picker-overlay{padding:10px}.model-picker{padding:18px;gap:14px;max-height:94dvh;border-radius:20px}.picker-table th,.picker-table td{padding:12px 8px}.choice-column{width:36px}.picker-table{font-size:14px}.model-id{font-size:12px}.picker-pagination{align-items:flex-start;flex-direction:column}.picker-pagination nav{align-self:flex-end}.picker-footer{align-items:flex-start;flex-wrap:wrap}.picker-actions{margin-left:auto}.picker-secondary,.picker-primary{padding:10px 12px}}
</style>
