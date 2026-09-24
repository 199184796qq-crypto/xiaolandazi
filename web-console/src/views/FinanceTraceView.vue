<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import {
  getStaffFinanceCustomer,
  getStaffFinanceOverview,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type {
  FinanceDashboard,
  StaffFinanceCustomerSummary,
  StaffFinanceTaskSummary,
} from '../types'

interface TraceRow {
  id: string
  occurred_at: string
  category: string
  title: string
  business_no: string
  amount_cents?: number
  balance_before_cents?: number
  balance_after_cents?: number
  requester?: string
  approver?: string
  status: string
  reason?: string
}

const loading = ref(false)
const error = useFeedbackErrorRef()
const customers = ref<StaffFinanceCustomerSummary[]>([])
const tasks = ref<StaffFinanceTaskSummary[]>([])
const selectedTenantId = ref<number | null>(null)
const dashboard = ref<FinanceDashboard | null>(null)
const customerSearch = ref('')
const traceSearch = ref('')
const categoryFilter = ref('all')
const customerPage = ref(1)
const customerPageSize = 10
const tracePage = ref(1)
const tracePageSize = 25

const filteredCustomers = computed(() => {
  const keyword = customerSearch.value.trim().toLowerCase()
  if (!keyword) return customers.value
  return customers.value.filter((item) =>
    [item.display_name, item.username, item.phone, String(item.tenant_id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const customerPageCount = computed(() => Math.max(1, Math.ceil(filteredCustomers.value.length / customerPageSize)))
const pagedCustomers = computed(() => {
  const page = Math.min(customerPage.value, customerPageCount.value)
  const start = (page - 1) * customerPageSize
  return filteredCustomers.value.slice(start, start + customerPageSize)
})

const selectedCustomer = computed(() =>
  customers.value.find((item) => item.tenant_id === selectedTenantId.value) || null,
)

const traceRows = computed<TraceRow[]>(() => {
  if (!dashboard.value || !selectedTenantId.value) return []
  const data = dashboard.value
  const result: TraceRow[] = []

  for (const item of data.ledger) {
    result.push({
      id: 'ledger-' + item.id,
      occurred_at: item.occurred_at,
      category: 'ledger',
      title: item.business_type,
      business_no: item.order_no || '流水 #' + item.id,
      amount_cents: item.direction === 'debit' ? -item.amount_cents : item.amount_cents,
      balance_before_cents: item.balance_before_cents,
      balance_after_cents: item.balance_after_cents,
      status: '已入账',
      reason: item.reason,
    })
  }

  for (const item of data.recharges) {
    result.push({
      id: 'recharge-' + item.id,
      occurred_at: item.paid_at || item.created_at,
      category: 'recharge',
      title: '充值',
      business_no: item.recharge_no,
      amount_cents: item.credited_amount_cents || item.requested_amount_cents,
      status: item.status,
      reason: item.payment_method,
    })
  }

  for (const item of data.purchases) {
    result.push({
      id: 'purchase-' + item.id,
      occurred_at: item.paid_at || item.created_at,
      category: 'purchase',
      title: item.order_type === 'time_card' ? '购买时长卡' : '购买会员',
      business_no: item.order_no,
      amount_cents: -item.paid_amount_cents,
      status: item.status,
      reason: item.discount_amount_cents
        ? '优惠 ' + formatMoney(item.discount_amount_cents)
        : '',
    })
  }

  for (const item of data.refunds) {
    result.push({
      id: 'refund-' + item.id,
      occurred_at: item.processed_at || item.created_at,
      category: 'refund',
      title: '退款',
      business_no: item.refund_no,
      amount_cents: item.refund_amount_cents,
      status: item.status,
      reason: item.reason || item.refund_method,
    })
  }

  for (const task of tasks.value.filter((item) => item.tenant_id === selectedTenantId.value)) {
    result.push({
      id: 'task-' + task.id,
      occurred_at: task.decided_at || task.created_at,
      category: 'approval',
      title: '审批 · ' + task.operation_code,
      business_no: '审批 #' + task.id,
      amount_cents: Math.round(task.amount_yuan * 100),
      requester: task.requester_name,
      approver: task.approver_name,
      status: task.status,
      reason: task.reason,
    })
  }

  return result.sort(
    (a, b) =>
      new Date(b.occurred_at).getTime() -
      new Date(a.occurred_at).getTime(),
  )
})

const filteredTraceRows = computed(() => {
  const keyword = traceSearch.value.trim().toLowerCase()
  return traceRows.value.filter((item) => {
    const matchesCategory =
      categoryFilter.value === 'all' || item.category === categoryFilter.value
    const matchesKeyword =
      !keyword ||
      [
        item.title,
        item.business_no,
        item.requester,
        item.approver,
        item.status,
        item.reason,
      ].some((value) => String(value || '').toLowerCase().includes(keyword))
    return matchesCategory && matchesKeyword
  })
})
const tracePageCount = computed(() => Math.max(1, Math.ceil(filteredTraceRows.value.length / tracePageSize)))
const pagedTraceRows = computed(() => {
  const page = Math.min(tracePage.value, tracePageCount.value)
  const start = (page - 1) * tracePageSize
  return filteredTraceRows.value.slice(start, start + tracePageSize)
})

watch(selectedTenantId, async (tenantId) => {
  if (!tenantId) {
    dashboard.value = null
    return
  }
  await loadCustomer(tenantId)
})

function formatMoney(cents?: number) {
  if (cents === undefined) return '—'
  const sign = cents > 0 ? '+' : ''
  return sign + '¥' + (cents / 100).toFixed(2)
}

function categoryLabel(value: string) {
  const map: Record<string, string> = {
    ledger: '钱包流水',
    recharge: '充值',
    purchase: '购买',
    refund: '退款',
    approval: '审批',
  }
  return map[value] || value
}

async function loadOverview() {
  loading.value = true
  error.value = ''
  try {
    const data = await getStaffFinanceOverview()
    customers.value = data.customers
    tasks.value = data.tasks
    if (!selectedTenantId.value && data.customers.length) {
      selectedTenantId.value = data.customers[0].tenant_id
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取财务总览失败'
  } finally {
    loading.value = false
  }
}

async function loadCustomer(tenantId: number) {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await getStaffFinanceCustomer(tenantId)
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取终端资金链失败'
  } finally {
    loading.value = false
  }
}

onMounted(loadOverview)
</script>

<template>
  <div class="management-page finance-trace-page">
    <ModulePageNav context="finance" active-title="全链路追溯" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">FINANCE TRACE</p>
        <h2>全链路追溯</h2>
        <p>按终端穿透钱包流水、充值、购买、退款与审批记录，核对资金前后余额、业务单号和责任人。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="loadOverview">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <section class="finance-trace-layout">
      <aside class="settings-card finance-trace-customers">
        <header>
          <strong>选择终端</strong>
          <span>{{ customers.length }} 个账户</span>
        </header>

        <input
          v-model="customerSearch"
          class="finance-trace-search"
          type="search"
          placeholder="终端 / 账号 / 手机号"
          @input="customerPage = 1"
        />

        <div class="finance-trace-customer-list">
          <button
            v-for="customer in pagedCustomers"
            :key="customer.tenant_id"
            type="button"
            :class="{ active: selectedTenantId === customer.tenant_id }"
            @click="selectedTenantId = customer.tenant_id"
          >
            <div>
              <strong>{{ customer.display_name }}</strong>
              <span>@{{ customer.username }} · {{ customer.phone || '无电话' }}</span>
            </div>
            <b>{{ formatMoney(customer.cash_balance_cents + customer.reward_balance_cents) }}</b>
          </button>
        </div>
        <PaginationBar
          :page="Math.min(customerPage, customerPageCount)"
          :total-pages="customerPageCount"
          :total="filteredCustomers.length"
          :page-size="customerPageSize"
          @update:page="customerPage = $event"
        />
      </aside>

      <section class="settings-card finance-trace-main">
        <template v-if="selectedCustomer && dashboard">
          <header class="finance-trace-summary">
            <div>
              <span>当前终端</span>
              <strong>{{ selectedCustomer.display_name }}</strong>
              <small>终端组织 #{{ selectedCustomer.tenant_id }} · {{ dashboard.membership_name || '无会员' }}</small>
            </div>
            <div>
              <span>现金余额</span>
              <strong>{{ formatMoney(dashboard.cash_balance_cents) }}</strong>
            </div>
            <div>
              <span>奖励余额</span>
              <strong>{{ formatMoney(dashboard.reward_balance_cents) }}</strong>
            </div>
            <div>
              <span>可用时长</span>
              <strong>{{ (dashboard.available_seconds / 3600).toFixed(1) }}h</strong>
            </div>
          </header>

          <div class="finance-trace-toolbar">
            <input v-model="traceSearch" type="search" placeholder="单号 / 经办人 / 审核人 / 原因" @input="tracePage = 1" />
            <select v-model="categoryFilter" @change="tracePage = 1">
              <option value="all">全部链路</option>
              <option value="ledger">钱包流水</option>
              <option value="recharge">充值</option>
              <option value="purchase">购买</option>
              <option value="refund">退款</option>
              <option value="approval">审批</option>
            </select>
          </div>

          <div class="data-table-wrap">
            <table class="data-table">
              <thead>
                <tr>
                  <th>时间</th>
                  <th>链路类型</th>
                  <th>业务</th>
                  <th>单号</th>
                  <th>金额</th>
                  <th>余额变化</th>
                  <th>经办 / 审核</th>
                  <th>状态 / 原因</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="row in pagedTraceRows" :key="row.id">
                  <td>{{ new Date(row.occurred_at).toLocaleString('zh-CN') }}</td>
                  <td><span class="status-pill">{{ categoryLabel(row.category) }}</span></td>
                  <td>{{ row.title }}</td>
                  <td><small>{{ row.business_no }}</small></td>
                  <td>
                    <strong :class="{ 'money-positive': (row.amount_cents || 0) > 0, 'money-negative': (row.amount_cents || 0) < 0 }">
                      {{ formatMoney(row.amount_cents) }}
                    </strong>
                  </td>
                  <td>
                    <small v-if="row.balance_before_cents !== undefined">
                      {{ formatMoney(row.balance_before_cents) }} → {{ formatMoney(row.balance_after_cents) }}
                    </small>
                    <span v-else>—</span>
                  </td>
                  <td>
                    <span>{{ row.requester || '系统/流水' }}</span>
                    <small>{{ row.approver ? '审核：' + row.approver : '' }}</small>
                  </td>
                  <td>
                    <span>{{ row.status }}</span>
                    <small>{{ row.reason || '—' }}</small>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="filteredTraceRows.length === 0" class="empty-state">暂无符合条件的资金链路。</div>
          </div>
          <PaginationBar
            :page="Math.min(tracePage, tracePageCount)"
            :total-pages="tracePageCount"
            :total="filteredTraceRows.length"
            :page-size="tracePageSize"
            @update:page="tracePage = $event"
          />
        </template>

        <div v-else-if="loading" class="panel-loading">正在读取终端资金链...</div>
        <div v-else class="empty-state">请选择终端查看资金全链。</div>
      </section>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>
  </div>
</template>
