<script setup lang="ts">
import type { SalesStaffSummary } from '../types'

defineProps<{
  items: SalesStaffSummary[]
  total: number
  page: number
  totalPages: number
  search: string
  selectedStaffId: number
  loading?: boolean
}>()

const emit = defineEmits<{
  (event: 'update:search', value: string): void
  (event: 'update:page', value: number): void
  (event: 'select', value: number): void
}>()

function updateSearch(event: Event) {
  const target = event.target as HTMLInputElement | null
  emit('update:search', target?.value || '')
}
</script>

<template>
  <aside class="sales-scope-selector">
    <div class="sales-scope-head">
      <div>
        <span class="section-kicker">SALES SCOPE</span>
        <strong>销售人员</strong>
      </div>
      <span>{{ total }} 人</span>
    </div>

    <label class="sales-scope-search">
      <span>检索销售</span>
      <input
        :value="search"
        type="search"
        placeholder="姓名 / 工号 / 团队"
        @input="updateSearch"
      />
    </label>

    <button
      class="sales-scope-all"
      :class="{ active: selectedStaffId === 0 }"
      type="button"
      @click="emit('select', 0)"
    >
      <span>全部管理范围</span>
      <small>查看当前权限内全部客户</small>
    </button>

    <div class="sales-scope-table-wrap">
      <table class="sales-scope-table">
        <thead>
          <tr>
            <th>销售</th>
            <th>客户</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="item.staff_id"
            :class="{ active: selectedStaffId === item.staff_id }"
            @click="emit('select', item.staff_id)"
          >
            <td>
              <strong>{{ item.display_name }}</strong>
              <small>{{ item.employee_code }}</small>
            </td>
            <td>{{ item.customer_count }}</td>
          </tr>
        </tbody>
      </table>
      <div v-if="!loading && !items.length" class="sales-scope-empty">没有符合条件的销售人员</div>
      <div v-if="loading" class="sales-scope-empty">正在读取...</div>
    </div>

    <div class="sales-scope-pagination">
      <button
        type="button"
        :disabled="loading || page <= 1"
        @click="emit('update:page', page - 1)"
      >
        上一页
      </button>
      <span>{{ page }} / {{ totalPages }}</span>
      <button
        type="button"
        :disabled="loading || page >= totalPages"
        @click="emit('update:page', page + 1)"
      >
        下一页
      </button>
    </div>
    <small class="sales-scope-page-note">每页固定 10 人</small>
  </aside>
</template>
