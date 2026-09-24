<script setup lang="ts">
const props = defineProps<{
  page: number
  totalPages: number
  total: number
  pageSize: number
}>()

const emit = defineEmits<{
  'update:page': [value: number]
}>()

function go(page: number) {
  const next = Math.min(Math.max(page, 1), Math.max(props.totalPages, 1))
  emit('update:page', next)
}
</script>

<template>
  <div v-if="total > 0" class="pagination-bar">
    <span class="pagination-bar-summary">
      共 {{ total }} 条 · 第 {{ page }} / {{ Math.max(totalPages, 1) }} 页
    </span>
    <div class="pagination-bar-actions">
      <button type="button" :disabled="page <= 1" @click="go(1)">首页</button>
      <button type="button" :disabled="page <= 1" @click="go(page - 1)">上一页</button>
      <button type="button" :disabled="page >= totalPages" @click="go(page + 1)">下一页</button>
      <button type="button" :disabled="page >= totalPages" @click="go(totalPages)">末页</button>
    </div>
  </div>
</template>
