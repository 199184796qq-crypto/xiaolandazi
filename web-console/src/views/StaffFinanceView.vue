<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import {
  approveStaffFinanceTask,
  createStaffFinanceRecharge,
  createStaffFinanceRefund,
  createStaffFinanceReward,
  getStaffFinanceCustomer,
  getStaffFinanceOverview,
  rejectStaffFinanceTask,
} from '../api'
import { session } from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type {
  FinanceDashboard,
  StaffFinanceOverview,
  StaffFinanceTaskSummary,
} from '../types'

type OperationType = 'recharge' | 'refund' | 'reward'
type ApprovalType = OperationType | 'ai_time_grant'
type FinanceFocus = 'accounts' | 'approvals' | 'ledger' | 'history'

const props = withDefaults(defineProps<{ focus?: FinanceFocus }>(), { focus: 'accounts' })

const overview = ref<StaffFinanceOverview | null>(null)
const customerDashboard = ref<FinanceDashboard | null>(null)
const selectedTenantId = ref<number | null>(null)
const loading = ref(false)
const customerLoading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')

const operationOpen = ref(false)
const operationType = ref<OperationType>('recharge')
const amountYuan = ref<number | null>(null)
const reason = ref('')
const paymentMethod = ref('manual')
const submitting = ref(false)
const reviewingTaskId = ref<number | null>(null)
const approvalKeyword = ref('')
const approvalType = ref<'all' | ApprovalType>('all')
const approvalPage = ref(1)
const approvalPageSize = 20
const customerSearch = ref('')
const customerPage = ref(1)
const customerPageSize = 8
const ledgerSearch = ref('')
const ledgerPage = ref(1)
const ledgerPageSize = 20
const historySearch = ref('')
const historyPage = ref(1)
const historyPageSize = 20

const access = computed(() => session.bootstrap?.staff_access ?? null)
const showCustomerPanel = computed(() => props.focus === 'accounts' || props.focus === 'ledger')
const showAccounts = computed(() => props.focus === 'accounts')
const showApprovals = computed(() => props.focus === 'approvals')
const showLedger = computed(() => props.focus === 'ledger')
const showHistory = computed(() => props.focus === 'history')
const pageTitle = computed(() => ({ accounts: '终端账户', approvals: '财务待审核', ledger: '钱包流水', history: '财务操作记录' }[props.focus]))
const currentUserId = computed(() => session.bootstrap?.actor.user_id ?? 0)

function hasPermission(code: string) {
  return Boolean(
    access.value &&
      (access.value.is_super_admin ||
        access.value.permissions.includes(code)),
  )
}

const canRecharge = computed(() =>
  hasPermission('finance.recharge.create'),
)
const canRefund = computed(() =>
  hasPermission('finance.refund.create'),
)
const canReward = computed(() =>
  hasPermission('finance.reward.grant'),
)

const customers = computed(() => overview.value?.customers ?? [])
const tasks = computed(() => overview.value?.tasks ?? [])
const filteredCustomers = computed(() => {
  const keyword = customerSearch.value.trim().toLowerCase()
  if (!keyword) return customers.value
  return customers.value.filter((item) =>
    [item.display_name, item.username, item.phone, item.parent_org_name, String(item.tenant_id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const customerPageCount = computed(() => Math.max(1, Math.ceil(filteredCustomers.value.length / customerPageSize)))
const pagedCustomers = computed(() => {
  const page = Math.min(customerPage.value, customerPageCount.value)
  const start = (page - 1) * customerPageSize
  return filteredCustomers.value.slice(start, start + customerPageSize)
})
const selectedCustomer = computed(
  () =>
    customers.value.find(
      (item) => item.tenant_id === selectedTenantId.value,
    ) ?? null,
)

const pendingTasks = computed(() =>
  tasks.value.filter((item) => item.status === 'pending'),
)
const approvalRechargeCount = computed(() => pendingTasks.value.filter((item) => item.operation_code === 'finance.recharge').length)
const approvalRefundCount = computed(() => pendingTasks.value.filter((item) => item.operation_code === 'finance.refund').length)
const approvalRewardCount = computed(() => pendingTasks.value.filter((item) => item.operation_code === 'finance.reward').length)
const approvalAITimeCount = computed(() => pendingTasks.value.filter((item) => item.operation_code === 'finance.ai_time_grant').length)
const filteredPendingTasks = computed(() => {
  const keyword = approvalKeyword.value.trim().toLowerCase()
  const operationCode = approvalType.value === 'all' ? '' : 'finance.' + approvalType.value
  return pendingTasks.value.filter((item) => {
    if (operationCode && item.operation_code !== operationCode) return false
    if (!keyword) return true
    return [item.customer_name, item.requester_name, item.reason, operationLabel(item.operation_code), String(item.id)].some((value) => String(value || '').toLowerCase().includes(keyword))
  })
})
const approvalPageCount = computed(() => Math.max(1, Math.ceil(filteredPendingTasks.value.length / approvalPageSize)))
const pagedPendingTasks = computed(() => {
  const page = Math.min(approvalPage.value, approvalPageCount.value)
  const start = (page - 1) * approvalPageSize
  return filteredPendingTasks.value.slice(start, start + approvalPageSize)
})
const walletLedger = computed(() => customerDashboard.value?.ledger ?? [])
const filteredWalletLedger = computed(() => {
  const keyword = ledgerSearch.value.trim().toLowerCase()
  if (!keyword) return walletLedger.value
  return walletLedger.value.filter((item) =>
    [item.business_type, item.reason, item.order_no, item.direction, String(item.id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const ledgerPageCount = computed(() => Math.max(1, Math.ceil(filteredWalletLedger.value.length / ledgerPageSize)))
const pagedWalletLedger = computed(() => {
  const page = Math.min(ledgerPage.value, ledgerPageCount.value)
  const start = (page - 1) * ledgerPageSize
  return filteredWalletLedger.value.slice(start, start + ledgerPageSize)
})
const historyTasks = computed(() => tasks.value.filter((item) => item.status !== 'pending'))
const filteredHistoryTasks = computed(() => {
  const keyword = historySearch.value.trim().toLowerCase()
  if (!keyword) return historyTasks.value
  return historyTasks.value.filter((item) =>
    [item.customer_name, item.requester_name, item.approver_name, item.reason, operationLabel(item.operation_code), String(item.id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const historyPageCount = computed(() => Math.max(1, Math.ceil(filteredHistoryTasks.value.length / historyPageSize)))
const pagedHistoryTasks = computed(() => {
  const page = Math.min(historyPage.value, historyPageCount.value)
  const start = (page - 1) * historyPageSize
  return filteredHistoryTasks.value.slice(start, start + historyPageSize)
})

function operationLabel(value: string) {
  if (value === 'finance.recharge') return '充值'
  if (value === 'finance.refund') return '退款'
  if (value === 'finance.reward') return '奖励发放'
  if (value === 'finance.ai_time_grant') return 'AI 时长增加'
  return value
}

function taskStatusLabel(value: string) {
  if (value === 'pending') return '待审核'
  if (value === 'approved') return '已审核执行'
  if (value === 'completed') return '已直接执行'
  if (value === 'rejected') return '已拒绝'
  return value
}

function taskStatusClass(value: string) {
  if (value === 'pending') return 'pending'
  if (value === 'rejected') return 'inactive'
  return ''
}

function canApproveTask(item: StaffFinanceTaskSummary) {
  if (item.requester_user_id === currentUserId.value) return false
  if (item.operation_code === 'finance.recharge') {
    return hasPermission('finance.recharge.approve')
  }
  if (item.operation_code === 'finance.refund') {
    return hasPermission('finance.refund.approve')
  }
  if (item.operation_code === 'finance.reward') {
    return hasPermission('finance.reward.approve')
  }
  if (item.operation_code === 'finance.ai_time_grant') {
    return hasPermission('finance.ai_time.approve')
  }
  return false
}

function formatMoney(cents: number) {
  return '¥' + (Number(cents || 0) / 100).toFixed(2)
}

function formatTime(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function changeApprovalType(value: 'all' | ApprovalType) {
  approvalType.value = value
  approvalPage.value = 1
}

function changeApprovalPage(delta: number) {
  approvalPage.value = Math.min(approvalPageCount.value, Math.max(1, approvalPage.value + delta))
}

async function loadOverview() {
  loading.value = true
  error.value = ''
  try {
    overview.value = await getStaffFinanceOverview()
    if (
      selectedTenantId.value &&
      !overview.value.customers.some(
        (item) => item.tenant_id === selectedTenantId.value,
      )
    ) {
      selectedTenantId.value = null
    }
    if (!selectedTenantId.value && overview.value.customers.length) {
      selectedTenantId.value = overview.value.customers[0].tenant_id
    }
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取财务工作台失败'
  } finally {
    loading.value = false
  }
}

async function loadCustomer() {
  if (!selectedTenantId.value) {
    customerDashboard.value = null
    return
  }
  customerLoading.value = true
  error.value = ''
  try {
    customerDashboard.value = await getStaffFinanceCustomer(
      selectedTenantId.value,
    )
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取终端财务信息失败'
  } finally {
    customerLoading.value = false
  }
}

async function refreshAll() {
  await loadOverview()
  await loadCustomer()
}

async function selectCustomer(tenantId: number) {
  selectedTenantId.value = tenantId
  ledgerPage.value = 1
  ledgerSearch.value = ''
  await loadCustomer()
}

function openOperation(type: OperationType) {
  if (!selectedCustomer.value) {
    error.value = '请先选择终端'
    return
  }
  operationType.value = type
  amountYuan.value = null
  reason.value = ''
  paymentMethod.value = 'manual'
  operationOpen.value = true
  error.value = ''
  notice.value = ''
}

async function submitOperation() {
  if (
    !selectedCustomer.value ||
    !amountYuan.value ||
    amountYuan.value <= 0 ||
    submitting.value
  ) {
    error.value = '请输入大于 0 的金额'
    return
  }

  const amountCents = Math.round(amountYuan.value * 100)
  if (amountCents <= 0) {
    error.value = '金额无效'
    return
  }

  if (
    (operationType.value === 'refund' ||
      operationType.value === 'reward') &&
    !reason.value.trim()
  ) {
    error.value =
      operationType.value === 'refund'
        ? '退款原因不能为空'
        : '奖励发放原因不能为空'
    return
  }

  submitting.value = true
  error.value = ''
  try {
    let result
    if (operationType.value === 'recharge') {
      result = await createStaffFinanceRecharge({
        tenant_id: selectedCustomer.value.tenant_id,
        amount_cents: amountCents,
        payment_method: paymentMethod.value,
        reason: reason.value.trim(),
      })
    } else if (operationType.value === 'refund') {
      result = await createStaffFinanceRefund({
        tenant_id: selectedCustomer.value.tenant_id,
        amount_cents: amountCents,
        reason: reason.value.trim(),
      })
    } else {
      result = await createStaffFinanceReward({
        tenant_id: selectedCustomer.value.tenant_id,
        amount_cents: amountCents,
        reason: reason.value.trim(),
      })
    }

    notice.value = result.requires_approval
      ? operationLabel(result.task.operation_code) + '已提交审核，余额暂未变动。'
      : operationLabel(result.task.operation_code) + '已直接执行并写入钱包流水。'
    operationOpen.value = false
    await refreshAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '财务操作失败'
  } finally {
    submitting.value = false
  }
}

async function reviewTask(
  item: StaffFinanceTaskSummary,
  action: 'approve' | 'reject',
) {
  if (reviewingTaskId.value) return

  const message =
    action === 'approve'
      ? item.operation_code === 'finance.ai_time_grant'
        ? '确认审核通过这笔 AI 时长增加申请吗？通过后将立即增加对应代理或终端的 AI 时长。'
        : '确认审核通过这笔' + operationLabel(item.operation_code) + '吗？通过后将立即影响终端余额。'
      : '确认拒绝这笔' + operationLabel(item.operation_code) + '吗？'

  if (!(await confirmAction({
    title: action === 'approve' ? '确认审核通过' : '确认拒绝',
    message,
    confirmText: action === 'approve' ? '审核通过' : '确认拒绝',
    danger: action === 'reject',
  }))) return

  reviewingTaskId.value = item.id
  error.value = ''
  notice.value = ''
  try {
    if (action === 'approve') {
      await approveStaffFinanceTask(item.id)
      notice.value = '审批已通过并执行。'
    } else {
      await rejectStaffFinanceTask(item.id)
      notice.value = '审批已拒绝。'
    }
    await refreshAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '审批失败'
  } finally {
    reviewingTaskId.value = null
  }
}

onMounted(async () => {
  await loadOverview()
  await loadCustomer()
})
</script>

<template>
  <div class="management-page staff-finance-page">
    <ModulePageNav hub="finance" :active-title="pageTitle" />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">FINANCE WORKSPACE</p>
        <h2>{{ pageTitle }}</h2>
      </div>
      <button
        class="ghost-button"
        type="button"
        :disabled="loading"
        @click="refreshAll"
      >
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <section class="finance-workspace-grid" :class="{ 'finance-workspace-grid--full': !showCustomerPanel }">
      <article v-if="showCustomerPanel" class="settings-card finance-customer-panel">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">CUSTOMERS</span>
            <h3>终端账户</h3>
          </div>
          <span>{{ customers.length }} 个终端</span>
        </div>

        <div class="finance-customer-tools">
          <input
            v-model="customerSearch"
            type="search"
            placeholder="搜索终端名称 / 账号 / 手机号"
            @input="customerPage = 1"
          />
          <span>匹配 {{ filteredCustomers.length }} 个</span>
        </div>

        <div class="finance-customer-list">
          <button
            v-for="item in pagedCustomers"
            :key="item.tenant_id"
            type="button"
            class="finance-customer-row"
            :class="{ active: selectedTenantId === item.tenant_id }"
            @click="selectCustomer(item.tenant_id)"
          >
            <div>
              <strong>{{ item.display_name }}</strong>
              <span>@{{ item.username }} · {{ item.phone }}</span>
            </div>
            <div>
              <span>现金 {{ formatMoney(item.cash_balance_cents) }}</span>
              <span>奖励 {{ formatMoney(item.reward_balance_cents) }}</span>
            </div>
          </button>

          <div v-if="!filteredCustomers.length && !loading" class="empty-state">
            暂无符合条件的终端财务账户。
          </div>
        </div>
        <PaginationBar
          :page="customerPage"
          :total-pages="customerPageCount"
          :total="filteredCustomers.length"
          :page-size="customerPageSize"
          @update:page="customerPage = $event"
        />
      </article>

      <div class="finance-main-column">
        <section
          v-if="selectedCustomer && showCustomerPanel"
          class="finance-balance-grid"
        >
          <article class="finance-balance-card">
            <span>终端</span>
            <strong class="finance-customer-name">
              {{ selectedCustomer.display_name }}
            </strong>
            <small>{{ selectedCustomer.parent_org_name || '平台直营' }}</small>
          </article>
          <article class="finance-balance-card">
            <span>现金余额</span>
            <strong>{{ formatMoney(customerDashboard?.cash_balance_cents || 0) }}</strong>
            <small>充值 / 退款账户</small>
          </article>
          <article class="finance-balance-card">
            <span>奖励余额</span>
            <strong>{{ formatMoney(customerDashboard?.reward_balance_cents || 0) }}</strong>
            <small>奖励发放账户</small>
          </article>
          <article class="finance-balance-card">
            <span>本月支出</span>
            <strong>{{ formatMoney(customerDashboard?.month_spent_cents || 0) }}</strong>
            <small>{{ customerDashboard?.membership_name || '暂无会员' }}</small>
          </article>
        </section>

        <section v-if="selectedCustomer && showAccounts" class="settings-card finance-actions-card">
          <div class="settings-card-header">
            <div>
              <span class="section-kicker">OPERATIONS</span>
              <h3>财务操作</h3>
            </div>
            <span>所有操作写审计与钱包流水</span>
          </div>

          <div class="finance-action-buttons">
            <button
              v-if="canRecharge"
              class="primary-button"
              type="button"
              @click="openOperation('recharge')"
            >
              充值
            </button>
            <button
              v-if="canRefund"
              class="ghost-button"
              type="button"
              @click="openOperation('refund')"
            >
              退款
            </button>
            <button
              v-if="canReward"
              class="ghost-button"
              type="button"
              @click="openOperation('reward')"
            >
              奖励发放
            </button>
          </div>
        </section>

        <template v-if="showApprovals">
          <section class="finance-approval-metrics">
            <article>
              <span>待审核总数</span>
              <strong>{{ pendingTasks.length }}</strong>
              <small>全部未处理财务申请</small>
            </article>
            <article>
              <span>充值审核</span>
              <strong>{{ approvalRechargeCount }}</strong>
              <small>充值申请</small>
            </article>
            <article>
              <span>退款审核</span>
              <strong>{{ approvalRefundCount }}</strong>
              <small>退款申请</small>
            </article>
            <article>
              <span>奖励审核</span>
              <strong>{{ approvalRewardCount }}</strong>
              <small>奖励发放申请</small>
            </article>
            <article>
              <span>AI 时长审核</span>
              <strong>{{ approvalAITimeCount }}</strong>
              <small>营销运维增加时长申请</small>
            </article>
          </section>

          <section class="settings-card finance-approval-card finance-approval-table-card">
            <div class="settings-card-header finance-approval-header">
              <div>
                <span class="section-kicker">APPROVALS</span>
                <h3>待审核财务申请</h3>
                <small>审核通过后立即执行并写入对应资金或 AI 时长流水；发起人与审核人必须分离。</small>
              </div>
              <span>{{ filteredPendingTasks.length }} / {{ pendingTasks.length }} 条</span>
            </div>

            <div class="finance-approval-toolbar">
              <div class="finance-approval-tabs">
                <button type="button" :class="{ active: approvalType === 'all' }" @click="changeApprovalType('all')">全部 {{ pendingTasks.length }}</button>
                <button type="button" :class="{ active: approvalType === 'recharge' }" @click="changeApprovalType('recharge')">充值 {{ approvalRechargeCount }}</button>
                <button type="button" :class="{ active: approvalType === 'refund' }" @click="changeApprovalType('refund')">退款 {{ approvalRefundCount }}</button>
                <button type="button" :class="{ active: approvalType === 'reward' }" @click="changeApprovalType('reward')">奖励 {{ approvalRewardCount }}</button>
                <button type="button" :class="{ active: approvalType === 'ai_time_grant' }" @click="changeApprovalType('ai_time_grant')">AI 时长 {{ approvalAITimeCount }}</button>
              </div>
              <input
                v-model="approvalKeyword"
                class="finance-approval-search"
                type="search"
                placeholder="搜索终端、发起人、备注或单号"
                @input="approvalPage = 1"
              />
            </div>

            <div v-if="!pendingTasks.length" class="empty-state">
              当前没有待审核财务操作。
            </div>
            <div v-else-if="!filteredPendingTasks.length" class="empty-state">
              没有符合当前筛选条件的待审核申请。
            </div>

            <div v-else class="finance-approval-table-wrap">
              <table class="finance-approval-table">
                <thead>
                  <tr>
                    <th>单号</th>
                    <th>业务类型</th>
                    <th>目标</th>
                    <th>申请量</th>
                    <th>申请说明</th>
                    <th>发起人</th>
                    <th>发起时间</th>
                    <th>状态</th>
                    <th class="finance-approval-action-head">审核操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="item in pagedPendingTasks" :key="item.id">
                    <td><span class="finance-approval-id">#{{ item.id }}</span></td>
                    <td><strong>{{ operationLabel(item.operation_code) }}</strong></td>
                    <td><strong>{{ item.operation_code === 'finance.ai_time_grant' ? (item.target_type === 'agent' ? '代理 · ' : '终端 · ') + item.customer_name : item.customer_name }}</strong></td>
                    <td class="finance-approval-amount">{{ item.operation_code === 'finance.ai_time_grant' ? ((item.resource_seconds || 0) / 3600).toFixed(1) + ' 小时' : '¥' + item.amount_yuan.toFixed(2) }}</td>
                    <td>
                      <span class="finance-approval-reason" :title="item.reason || '无备注'">{{ item.reason || '无备注' }}</span>
                    </td>
                    <td>{{ item.requester_name }}</td>
                    <td>{{ formatTime(item.created_at) }}</td>
                    <td>
                      <span class="status-pill" :class="taskStatusClass(item.status)">{{ taskStatusLabel(item.status) }}</span>
                    </td>
                    <td class="finance-approval-actions-cell">
                      <div v-if="canApproveTask(item)" class="finance-approval-row-actions">
                        <button
                          class="finance-approval-action approve"
                          type="button"
                          :disabled="reviewingTaskId === item.id"
                          @click="reviewTask(item, 'approve')"
                        >
                          {{ reviewingTaskId === item.id ? '处理中' : '审核通过' }}
                        </button>
                        <button
                          class="finance-approval-action reject"
                          type="button"
                          :disabled="reviewingTaskId === item.id"
                          @click="reviewTask(item, 'reject')"
                        >
                          驳回
                        </button>
                      </div>
                      <span v-else class="finance-review-hint">
                        {{ item.requester_user_id === currentUserId ? '本人发起，等待他人审核' : '无审核权限' }}
                      </span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <PaginationBar
              :page="Math.min(approvalPage, approvalPageCount)"
              :total-pages="approvalPageCount"
              :total="filteredPendingTasks.length"
              :page-size="approvalPageSize"
              @update:page="approvalPage = $event"
            />
          </section>
        </template>

        <section v-if="showLedger" class="settings-card finance-ledger-card">
          <div class="settings-card-header">
            <div>
              <span class="section-kicker">WALLET LEDGER</span>
              <h3>终端钱包流水</h3>
            </div>
            <span>{{ customerLoading ? '读取中...' : filteredWalletLedger.length + ' 条' }}</span>
          </div>

          <div class="finance-ledger-toolbar">
            <input
              v-model="ledgerSearch"
              type="search"
              placeholder="搜索业务类型 / 原因 / 订单号"
              @input="ledgerPage = 1"
            />
          </div>

          <div v-if="!filteredWalletLedger.length" class="empty-state">
            当前终端暂无符合条件的钱包流水。
          </div>

          <div v-else class="finance-ledger-list">
            <article
              v-for="item in pagedWalletLedger"
              :key="item.id"
              class="finance-ledger-row"
            >
              <div>
                <strong>{{ item.business_type }}</strong>
                <span>{{ item.reason || item.order_no || '—' }}</span>
              </div>
              <strong
                :class="{
                  credit: item.direction === 'credit',
                  debit: item.direction === 'debit',
                }"
              >
                {{ item.direction === 'credit' ? '+' : '-' }}
                {{ formatMoney(item.amount_cents) }}
              </strong>
              <span>{{ formatMoney(item.balance_after_cents) }}</span>
              <span>{{ formatTime(item.occurred_at) }}</span>
            </article>
          </div>
          <PaginationBar
            :page="Math.min(ledgerPage, ledgerPageCount)"
            :total-pages="ledgerPageCount"
            :total="filteredWalletLedger.length"
            :page-size="ledgerPageSize"
            @update:page="ledgerPage = $event"
          />
        </section>

        <section v-if="showHistory" class="settings-card finance-history-card">
          <div class="settings-card-header">
            <div>
              <span class="section-kicker">OPERATION HISTORY</span>
              <h3>财务操作记录</h3>
            </div>
          </div>

          <div class="finance-ledger-toolbar">
            <input
              v-model="historySearch"
              type="search"
              placeholder="搜索终端 / 发起人 / 审核人 / 原因"
              @input="historyPage = 1"
            />
          </div>

          <div class="finance-task-list">
            <article
              v-for="item in pagedHistoryTasks"
              :key="item.id"
              class="finance-task-row"
            >
              <div>
                <span class="muted-label">{{ operationLabel(item.operation_code) }}</span>
                <strong>{{ item.customer_name }}</strong>
              </div>
              <strong>{{ item.operation_code === 'finance.ai_time_grant' ? ((item.resource_seconds || 0) / 3600).toFixed(1) + ' 小时' : '¥' + item.amount_yuan.toFixed(2) }}</strong>
              <div>
                <span class="muted-label">发起 / 审核</span>
                <strong>
                  {{ item.requester_name }}
                  <template v-if="item.approver_name">
                    → {{ item.approver_name }}
                  </template>
                </strong>
              </div>
              <span
                class="status-pill"
                :class="taskStatusClass(item.status)"
              >
                {{ taskStatusLabel(item.status) }}
              </span>
            </article>
          </div>
          <PaginationBar
            :page="Math.min(historyPage, historyPageCount)"
            :total-pages="historyPageCount"
            :total="filteredHistoryTasks.length"
            :page-size="historyPageSize"
            @update:page="historyPage = $event"
          />
        </section>
      </div>
    </section>

    <div
      v-if="operationOpen && selectedCustomer"
      class="modal-backdrop"
      @click.self="operationOpen = false"
    >
      <form class="modal-card" @submit.prevent="submitOperation">
        <div class="modal-header">
          <div>
            <p class="section-kicker">FINANCE OPERATION</p>
            <h3>
              {{
                operationType === 'recharge'
                  ? '终端充值'
                  : operationType === 'refund'
                    ? '终端退款'
                    : '奖励发放'
              }}
            </h3>
          </div>
          <button
            class="icon-button"
            type="button"
            @click="operationOpen = false"
          >
            ×
          </button>
        </div>

        <div class="credential-card">
          <div>
            <span>终端</span>
            <strong>{{ selectedCustomer.display_name }}</strong>
          </div>
          <div>
            <span>当前现金余额</span>
            <strong>{{ formatMoney(customerDashboard?.cash_balance_cents || 0) }}</strong>
          </div>
        </div>

        <div class="form-grid finance-operation-form">
          <label>
            <span>金额（元）</span>
            <input
              v-model.number="amountYuan"
              type="number"
              min="0.01"
              step="0.01"
              required
              placeholder="0.00"
            />
          </label>

          <label v-if="operationType === 'recharge'">
            <span>充值方式</span>
            <select v-model="paymentMethod" class="text-input">
              <option value="manual">人工充值</option>
              <option value="bank">银行转账</option>
              <option value="offline">线下收款</option>
            </select>
          </label>

          <label class="form-span-2">
            <span>
              {{
                operationType === 'refund'
                  ? '退款原因'
                  : operationType === 'reward'
                    ? '奖励原因'
                    : '备注'
              }}
            </span>
            <textarea
              v-model="reason"
              rows="3"
              :required="operationType !== 'recharge'"
              placeholder="填写本次操作原因或备注"
            ></textarea>
          </label>
        </div>

        <div class="account-opening-note">
          <strong>审批规则</strong>
          <span>
            系统会按照“组织架构 → 财务部 → 审批规则”自动判断直接执行或进入待审核；待审核状态不会提前修改终端余额。
          </span>
        </div>

        <div class="modal-actions">
          <button
            class="ghost-button"
            type="button"
            @click="operationOpen = false"
          >
            取消
          </button>
          <button
            class="primary-button"
            type="submit"
            :disabled="submitting"
          >
            {{ submitting ? '处理中...' : '确认提交' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
