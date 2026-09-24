<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getAdminSalesPerformance } from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type {
  SalesPerformanceSummary,
  SalesPerformanceTotals,
  StaffBusinessScope,
} from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'

const now = new Date()
const period = ref(now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0'))
const loading = ref(false)
const error = useFeedbackErrorRef()
const items = ref<SalesPerformanceSummary[]>([])
const totals = ref<SalesPerformanceTotals>({
  paid_order_count: 0,
  customer_count: 0,
  paid_amount_cents: 0,
  refunded_amount_cents: 0,
  net_revenue_cents: 0,
  earning_amount_cents: 0,
  pending_earning_cents: 0,
  settled_earning_cents: 0,
})
const scope = ref<StaffBusinessScope | null>(null)
const total = ref(0)
const totalPages = ref(1)
const search = ref('')
const sortMode = ref('revenue-desc')
const page = ref(1)
const pageSize = ref(12)
const viewMode = ref<'card' | 'table'>('table')

const sortOptions = [
  { label: '净销售额从高到低', value: 'revenue-desc' },
  { label: '提成从高到低', value: 'earning-desc' },
  { label: '订单数从高到低', value: 'orders-desc' },
  { label: '姓名 A-Z', value: 'name-asc' },
]

const scopeHint = computed(() => {
  if (!scope.value) return ''
  return scope.value.manager_view
    ? '当前显示你管理范围内的销售人员'
    : '当前仅显示你自己的业绩与提成'
})

let searchTimer: ReturnType<typeof setTimeout> | undefined

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

async function load(resetPage = false) {
  if (resetPage) page.value = 1
  loading.value = true
  error.value = ''
  try {
    const data = await getAdminSalesPerformance(period.value, {
      search: search.value.trim(),
      sort: sortMode.value,
      page: page.value,
      page_size: pageSize.value,
    })
    items.value = data.items
    totals.value = data.totals
    scope.value = data.scope
    total.value = data.total
    totalPages.value = data.total_pages
    if (page.value > data.total_pages) page.value = data.total_pages
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取销售业绩失败'
  } finally {
    loading.value = false
  }
}

watch(search, () => {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void load(true), 280)
})

watch([sortMode, pageSize], () => void load(true))
watch(page, () => void load(false))

onMounted(() => void load(false))
</script>

<template>
  <div class="management-page sales-performance-page">
    <ModulePageNav context="sales" active-title="业绩与提成" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">SALES PERFORMANCE</p>
        <h2>业绩与提成</h2>
        <p>按订单创建时保存的销售归属快照统计业绩；后续终端转交不会改写历史订单归属。</p>
        <span v-if="scopeHint" class="sales-performance-scope-hint">{{ scopeHint }}</span>
      </div>

      <div class="sales-period-control">
        <label>
          <span>统计月份</span>
          <input v-model="period" type="month" @change="load(true)" />
        </label>
        <button class="ghost-button" type="button" :disabled="loading" @click="load(false)">
          {{ loading ? '统计中...' : '重新统计' }}
        </button>
      </div>
    </section>

    <section class="module-hub-metrics-v2">
      <article class="module-hub-metric-v2 tone-primary">
        <span>成交订单</span><strong>{{ totals.paid_order_count }}</strong><small>{{ period }}</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>成交终端</span><strong>{{ totals.customer_count }}</strong><small>按当前权限范围统计</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>净销售额</span><strong>{{ formatMoney(totals.net_revenue_cents) }}</strong><small>支付金额 - 已退款</small>
      </article>
      <article class="module-hub-metric-v2 tone-warning">
        <span>提成收益</span><strong>{{ formatMoney(totals.earning_amount_cents) }}</strong><small>含待结算与已结算</small>
      </article>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="销售姓名 / 员工编号 / 团队"
        :sort-options="sortOptions"
      />

      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在统计销售业绩...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>销售员工</th>
              <th>团队</th>
              <th>成交订单</th>
              <th>成交终端</th>
              <th>支付金额</th>
              <th>退款金额</th>
              <th>净销售额</th>
              <th>提成收益</th>
              <th>待结算</th>
              <th>已结算</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in items" :key="item.sales_staff_id">
              <td><strong>{{ item.display_name }}</strong><small>{{ item.employee_code }}</small></td>
              <td>{{ item.team_name || '销售部' }}</td>
              <td>{{ item.paid_order_count }}</td>
              <td>{{ item.customer_count }}</td>
              <td>{{ formatMoney(item.paid_amount_cents) }}</td>
              <td>{{ formatMoney(item.refunded_amount_cents) }}</td>
              <td><strong>{{ formatMoney(item.net_revenue_cents) }}</strong></td>
              <td>{{ formatMoney(item.earning_amount_cents) }}</td>
              <td>{{ formatMoney(item.pending_earning_cents) }}</td>
              <td>{{ formatMoney(item.settled_earning_cents) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="!items.length" class="empty-state">当前月份暂无销售业绩。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in items" :key="item.sales_staff_id" class="feature-record-card">
          <header>
            <div><span>{{ item.employee_code }}</span><h3>{{ item.display_name }}</h3></div>
            <span class="status-pill">{{ item.team_name || '销售部' }}</span>
          </header>
          <dl>
            <div><dt>成交订单</dt><dd>{{ item.paid_order_count }}</dd></div>
            <div><dt>成交终端</dt><dd>{{ item.customer_count }}</dd></div>
            <div><dt>净销售额</dt><dd>{{ formatMoney(item.net_revenue_cents) }}</dd></div>
            <div><dt>提成收益</dt><dd>{{ formatMoney(item.earning_amount_cents) }}</dd></div>
            <div><dt>待结算</dt><dd>{{ formatMoney(item.pending_earning_cents) }}</dd></div>
            <div><dt>已结算</dt><dd>{{ formatMoney(item.settled_earning_cents) }}</dd></div>
          </dl>
        </article>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="total"
        :page-size="pageSize"
      />
    </section>
  </div>
</template>
