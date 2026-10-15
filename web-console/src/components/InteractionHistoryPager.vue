<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = withDefaults(defineProps<{
  page: number
  total: number
  disabled?: boolean
  position: 'top' | 'bottom'
}>(), { disabled: false })
const emit = defineEmits<{ change: [page: number] }>()
const pageCount = computed(() => Math.max(1, Math.ceil(props.total / 30)))
const jumpPage = ref(1)
watch(() => [props.page, pageCount.value], () => { jumpPage.value = props.page }, { immediate: true })
function go(page: number) {
  if (props.disabled || !props.total || !Number.isInteger(page)) return
  const next = Math.min(Math.max(page, 1), pageCount.value)
  if (next !== props.page) emit('change', next)
}
</script>

<template>
  <nav class="interaction-history-pager" :aria-label="position === 'top' ? '处理历史顶部分页' : '处理历史底部分页'">
    <span class="history-page-summary" role="status">共 {{ total }} 条 · 第 {{ page }} / {{ pageCount }} 页 · 每页 30 条</span>
    <div class="history-page-buttons">
      <button type="button" :disabled="disabled || page <= 1 || !total" @click="go(1)">首页</button>
      <button type="button" :disabled="disabled || page <= 1 || !total" @click="go(page - 1)">上一页</button>
      <button type="button" :disabled="disabled || page >= pageCount || !total" @click="go(page + 1)">下一页</button>
      <button type="button" :disabled="disabled || page >= pageCount || !total" @click="go(pageCount)">末页</button>
    </div>
    <form class="history-page-jump" @submit.prevent="go(Number(jumpPage))">
      <label>跳至 <input v-model.number="jumpPage" type="number" inputmode="numeric" min="1" :max="pageCount" step="1" aria-label="跳转页码" :disabled="disabled || !total" /> 页</label>
      <button type="submit" :disabled="disabled || !total">跳转</button>
    </form>
  </nav>
</template>

<style scoped>
.interaction-history-pager{display:flex;align-items:center;justify-content:space-between;gap:12px 18px;flex-wrap:wrap;margin:16px 0;padding:14px 16px;border:1px solid #dce4f5;border-radius:12px;background:#fff;color:#536884;font-size:16px;line-height:1.6}
.history-page-summary{font-size:16px;overflow-wrap:anywhere}
.history-page-buttons,.history-page-jump,.history-page-jump label{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin:0;font-size:16px}
.interaction-history-pager button,.history-page-jump input{box-sizing:border-box;min-height:42px;padding:7px 12px;border:1px solid #d5def3;border-radius:9px;background:#fff;color:#4d54d5;font:inherit;font-size:16px}
.interaction-history-pager button{cursor:pointer;white-space:nowrap}
.interaction-history-pager button:disabled{opacity:.5;cursor:default}
.history-page-jump input{width:74px;color:#293851}
.interaction-history-pager button:focus-visible,.history-page-jump input:focus-visible{outline:2px solid #8d8ae9;outline-offset:2px}
@media(max-width:600px){.interaction-history-pager{padding:12px}.history-page-buttons{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));width:100%}.history-page-jump{width:100%}.history-page-jump input{width:68px}}
</style>
