<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref } from 'vue'
import {
  approveSettlementBatch,
  createSettlementBatch,
  getFinanceSettlementDashboard,
  paySettlementBatch,
  rejectSettlementBatch,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  CreateSettlementBatchInput,
  IncentiveEarning,
  SettlementBatch,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const earnings = ref<IncentiveEarning[]>([])
const batches = ref<SettlementBatch[]>([])
const tab = ref<'earnings' | 'batches'>('earnings')
const modalOpen = ref(false)
const earningsPage = ref(1)
const batchesPage = ref(1)
const pageSize = 20

const now = new Date()
const monthStart = new Date(now.getFullYear(), now.getMonth(), 1)
const nextMonthStart = new Date(now.getFullYear(), now.getMonth() + 1, 1)

const form = reactive({
  beneficiary_type: 'sales_staff',
  beneficiary_id: '',
  period_start_at: dateInput(monthStart),
  period_end_at: dateInput(nextMonthStart),
})

const access = computed(() => session.bootstrap?.staff_access)
const actorUserId = computed(() => session.bootstrap?.actor.user_id || 0)

function hasPermission(code: string) {
  const value = access.value
  return Boolean(
    session.bootstrap?.actor.role === 'platform_admin' ||
      (value && (value.is_super_admin || value.permissions.includes(code))),
  )
}

const canCreate = computed(() => hasPermission('finance.settlement.create'))
const canApprove = computed(() => hasPermission('finance.settlement.approve'))
const canPay = computed(() => hasPermission('finance.settlement.pay'))

const availableAmount = computed(() =>
  earnings.value
    .filter((item) => item.status === 'available')
    .reduce((sum, item) => sum + item.amount_cents, 0),
)
const pendingAmount = computed(() =>
  earnings.value
    .filter((item) => item.status === 'pending')
    .reduce((sum, item) => sum + item.amount_cents, 0),
)
const settlingAmount = computed(() =>
  earnings.value
    .filter((item) => item.status === 'settling')
    .reduce((sum, item) => sum + item.amount_cents, 0),
)
const paidAmount = computed(() =>
  earnings.value
    .filter((item) => item.status === 'paid')
    .reduce((sum, item) => sum + item.amount_cents, 0),
)

const reviewCount = computed(() =>
  batches.value.filter((item) => item.status === 'reviewing').length,
)
const earningsPageCount = computed(() => Math.max(1, Math.ceil(earnings.value.length / pageSize)))
const batchesPageCount = computed(() => Math.max(1, Math.ceil(batches.value.length / pageSize)))
const pagedEarnings = computed(() => {
  const page = Math.min(earningsPage.value, earningsPageCount.value)
  const start = (page - 1) * pageSize
  return earnings.value.slice(start, start + pageSize)
})
const pagedBatches = computed(() => {
  const page = Math.min(batchesPage.value, batchesPageCount.value)
  const start = (page - 1) * pageSize
  return batches.value.slice(start, start + pageSize)
})

function dateInput(value: Date) {
  const y = value.getFullYear()
  const m = String(value.getMonth() + 1).padStart(2, '0')
  const d = String(value.getDate()).padStart(2, '0')
  return y + '-' + m + '-' + d
}

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function beneficiaryLabel(value: string) {
  const map: Record<string, string> = {
    sales_staff: '销售员工',
    agent: '代理',
    referrer: '推荐人',
  }
  return map[value] || value
}

function earningTypeLabel(value: string) {
  const map: Record<string, string> = {
    referral_reward: '推荐奖励',
    sales_commission: '销售提成',
    agent_settlement: '代理返佣',
    reversal: '冲回',
  }
  return map[value] || value
}

function statusLabel(value: string) {
  const map: Record<string, string> = {
    pending: '冻结中',
    available: '可结算',
    settling: '结算中',
    settled: '已结算',
    paid: '已支付',
    reversed: '已冲回',
    reviewing: '待审核',
    approved: '已审核',
    rejected: '已驳回',
  }
  return map[value] || value
}

function openCreate() {
  form.beneficiary_type = 'sales_staff'
  form.beneficiary_id = ''
  form.period_start_at = dateInput(monthStart)
  form.period_end_at = dateInput(nextMonthStart)
  error.value = ''
  modalOpen.value = true
}

async function submitCreate() {
  const beneficiaryId = Number(form.beneficiary_id)
  if (!beneficiaryId) {
    error.value = '结算对象 ID 不能为空'
    return
  }
  const payload: CreateSettlementBatchInput = {
    beneficiary_type: form.beneficiary_type,
    beneficiary_id: beneficiaryId,
    period_start_at: new Date(form.period_start_at + 'T00:00:00').toISOString(),
    period_end_at: new Date(form.period_end_at + 'T00:00:00').toISOString(),
  }

  saving.value = true
  error.value = ''
  try {
    await createSettlementBatch(payload)
    modalOpen.value = false
    tab.value = 'batches'
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '生成结算批次失败'
  } finally {
    saving.value = false
  }
}

async function approve(item: SettlementBatch) {
  if (!(await confirmAction({ title: '审核结算批次', message: '确认审核通过结算批次“' + item.batch_no + '”？', confirmText: '审核通过' }))) return
  error.value = ''
  try {
    await approveSettlementBatch(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '审核失败'
  }
}

async function reject(item: SettlementBatch) {
  if (!(await confirmAction({ title: '驳回结算批次', message: '确认驳回结算批次“' + item.batch_no + '”？对应收益会退回可结算状态。', confirmText: '确认驳回', danger: true }))) return
  error.value = ''
  try {
    await rejectSettlementBatch(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '驳回失败'
  }
}

async function pay(item: SettlementBatch) {
  if (!(await confirmAction({ title: '确认结算已支付', message: '确认结算批次“' + item.batch_no + '”已经完成支付？此操作会把对应收益标记为已支付。', confirmText: '确认已支付' }))) return
  error.value = ''
  try {
    await paySettlementBatch(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '确认支付失败'
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getFinanceSettlementDashboard()
    earnings.value = data.earnings
    batches.value = data.batches
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取收益结算失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page settlement-batches-page">
    <ModulePageNav context="finance" active-title="收益结算" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">EARNING SETTLEMENT</p>
        <h2>收益结算</h2>
        <p>销售提成、代理返佣等收益先进入收益明细；到可结算状态后，由经办人生成批次、审核员审核、财务主管确认支付。</p>
      </div>
      <div class="inventory-hero-actions">
        <button class="ghost-button" type="button" :disabled="loading" @click="load">
          {{ loading ? '刷新中...' : '刷新数据' }}
        </button>
        <button v-if="canCreate" class="primary-button" type="button" @click="openCreate">
          ＋ 生成结算批次
        </button>
      </div>
    </section>

    <section class="module-hub-metrics-v2">
      <article class="module-hub-metric-v2 tone-warning">
        <span>冻结中收益</span><strong>{{ formatMoney(pendingAmount) }}</strong><small>等待冻结期结束</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>可结算收益</span><strong>{{ formatMoney(availableAmount) }}</strong><small>可生成结算批次</small>
      </article>
      <article class="module-hub-metric-v2 tone-primary">
        <span>结算中</span><strong>{{ formatMoney(settlingAmount) }}</strong><small>{{ reviewCount }} 个批次待审核</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>累计已支付</span><strong>{{ formatMoney(paidAmount) }}</strong><small>已完成支付收益</small>
      </article>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>

    <section class="settings-card settlement-workspace">
      <div class="settings-tabs settlement-tabs">
        <button type="button" :class="{ active: tab === 'earnings' }" @click="tab = 'earnings'">
          收益明细
        </button>
        <button type="button" :class="{ active: tab === 'batches' }" @click="tab = 'batches'">
          结算批次
        </button>
      </div>

      <div v-if="loading" class="panel-loading">正在读取收益结算...</div>

      <div v-else-if="tab === 'earnings'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>收益编号</th>
              <th>受益人</th>
              <th>收益类型</th>
              <th>金额</th>
              <th>来源订单/退款</th>
              <th>状态</th>
              <th>可结算时间</th>
              <th>创建时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedEarnings" :key="item.id">
              <td><strong>{{ item.external_id }}</strong></td>
              <td>{{ beneficiaryLabel(item.beneficiary_type) }} #{{ item.beneficiary_id }}</td>
              <td>{{ earningTypeLabel(item.earning_type) }}</td>
              <td><strong :class="{ 'money-positive': item.amount_cents > 0, 'money-negative': item.amount_cents < 0 }">{{ formatMoney(item.amount_cents) }}</strong></td>
              <td>
                <span v-if="item.source_order_id">订单 #{{ item.source_order_id }}</span>
                <span v-else-if="item.source_refund_id">退款 #{{ item.source_refund_id }}</span>
                <span v-else>—</span>
              </td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
              <td>{{ item.available_at ? new Date(item.available_at).toLocaleString('zh-CN') : '—' }}</td>
              <td>{{ new Date(item.created_at).toLocaleString('zh-CN') }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="earnings.length === 0" class="empty-state">暂无收益明细。</div>
        <PaginationBar
          :page="Math.min(earningsPage, earningsPageCount)"
          :total-pages="earningsPageCount"
          :total="earnings.length"
          :page-size="pageSize"
          @update:page="earningsPage = $event"
        />
      </div>

      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>批次</th>
              <th>结算对象</th>
              <th>周期</th>
              <th>收益项</th>
              <th>结算金额</th>
              <th>状态</th>
              <th>经办 / 审核</th>
              <th>支付时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedBatches" :key="item.id">
              <td><strong>{{ item.batch_no }}</strong></td>
              <td>{{ beneficiaryLabel(item.beneficiary_type) }} #{{ item.beneficiary_id }}</td>
              <td>{{ new Date(item.period_start_at).toLocaleDateString('zh-CN') }} → {{ new Date(item.period_end_at).toLocaleDateString('zh-CN') }}</td>
              <td>{{ item.item_count }}</td>
              <td><strong>{{ formatMoney(item.settlement_amount_cents) }}</strong></td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
              <td>
                <span>{{ item.created_by_user_id ? '经办 #' + item.created_by_user_id : '历史批次' }}</span>
                <small>{{ item.approved_by_user_id ? '审核 #' + item.approved_by_user_id : '未审核' }}</small>
              </td>
              <td>{{ item.paid_at ? new Date(item.paid_at).toLocaleString('zh-CN') : '—' }}</td>
              <td>
                <div class="table-actions">
                  <button
                    v-if="canApprove && item.status === 'reviewing' && item.created_by_user_id !== actorUserId"
                    class="text-action"
                    type="button"
                    @click="approve(item)"
                  >
                    审核通过
                  </button>
                  <button
                    v-if="canApprove && item.status === 'reviewing'"
                    class="text-action danger"
                    type="button"
                    @click="reject(item)"
                  >
                    驳回
                  </button>
                  <button
                    v-if="canPay && item.status === 'approved'"
                    class="text-action"
                    type="button"
                    @click="pay(item)"
                  >
                    确认支付
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="batches.length === 0" class="empty-state">暂无结算批次。</div>
        <PaginationBar
          :page="Math.min(batchesPage, batchesPageCount)"
          :total-pages="batchesPageCount"
          :total="batches.length"
          :page-size="pageSize"
          @update:page="batchesPage = $event"
        />
      </div>
    </section>

    <div v-if="modalOpen" class="feature-editor-backdrop" @click.self="modalOpen = false">
      <section class="feature-editor-panel">
        <header>
          <div><span class="section-kicker">CREATE SETTLEMENT</span><h3>生成结算批次</h3></div>
          <button class="icon-button" type="button" @click="modalOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label>
            <span>结算对象类型</span>
            <select v-model="form.beneficiary_type">
              <option value="sales_staff">销售员工</option>
              <option value="agent">代理</option>
            </select>
          </label>
          <label><span>结算对象 ID *</span><input v-model="form.beneficiary_id" type="number" /></label>
          <label><span>周期开始</span><input v-model="form.period_start_at" type="date" /></label>
          <label><span>周期结束（不含）</span><input v-model="form.period_end_at" type="date" /></label>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>

        <footer>
          <button class="ghost-button" type="button" @click="modalOpen = false">取消</button>
          <button class="primary-button" type="button" :disabled="saving" @click="submitCreate">
            {{ saving ? '生成中...' : '生成批次' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>
