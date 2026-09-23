<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import { getAdminSalesPerformance } from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type { SalesPerformanceSummary } from '../types'

const now = new Date()
const period = ref(now.getFullYear() + '-' + String(now.getMonth() + 1).padStart(2, '0'))
const loading = ref(false)
const error = useFeedbackErrorRef()
const items = ref<SalesPerformanceSummary[]>([])
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

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) =>
    !keyword ||
    [item.display_name, item.employee_code, item.team_name]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )

  return [...result].sort((a, b) => {
    if (sortMode.value === 'earning-desc') return b.earning_amount_cents - a.earning_amount_cents
    if (sortMode.value === 'orders-desc') return b.paid_order_count - a.paid_order_count
    if (sortMode.value === 'name-asc') return a.display_name.localeCompare(b.display_name, 'zh-CN')
    return b.net_revenue_cents - a.net_revenue_cents
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

const totals = computed(() =>
  items.value.reduce(
    (acc, item) => {
      acc.orders += item.paid_order_count
      acc.customers += item.customer_count
      acc.revenue += item.net_revenue_cents
      acc.earnings += item.earning_amount_cents
      return acc
    },
    { orders: 0, customers: 0, revenue: 0, earnings: 0 },
  ),
)

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

async function load() {
  loading.value = true
  error.value = ''
  page.value = 1
  try {
    const data = await getAdminSalesPerformance(period.value)
    items.value = data.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取销售业绩失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page sales-performance-page">
    <ModulePageNav context="sales" active-title="业绩与提成" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">SALES PERFORMANCE</p>
        <h2>业绩与提成</h2>
        <p>按订单创建时保存的销售归属快照统计业绩；后续终端转交不会改写历史订单归属。</p>
      </div>

      <div class="sales-period-control">
        <label>
          <span>统计月份</span>
          <input v-model="period" type="month" @change="load" />
        </label>
        <button class="ghost-button" type="button" :disabled="loading" @click="load">
          {{ loading ? '统计中...' : '重新统计' }}
        </button>
      </div>
    </section>

    <section class="module-hub-metrics-v2">
      <article class="module-hub-metric-v2 tone-primary">
        <span>成交订单</span><strong>{{ totals.orders }}</strong><small>{{ period }}</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>成交终端</span><strong>{{ totals.customers }}</strong><small>按销售分别去重后合计</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>净销售额</span><strong>{{ formatMoney(totals.revenue) }}</strong><small>支付金额 - 已退款</small>
      </article>
      <article class="module-hub-metric-v2 tone-warning">
        <span>提成收益</span><strong>{{ formatMoney(totals.earnings) }}</strong><small>含待结算与已结算</small>
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
            <tr v-for="item in pagedItems" :key="item.sales_staff_id">
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
        <div v-if="filteredItems.length === 0" class="empty-state">当前月份暂无销售业绩。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in pagedItems" :key="item.sales_staff_id" class="feature-record-card">
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
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>
  </div>
</template>
