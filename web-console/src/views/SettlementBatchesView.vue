<script setup lang="ts">
import { getSalesWithdrawals,reviewSalesWithdrawal } from '../commerce'
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref } from 'vue'
import {
  approveCustomerWalletWithdrawal,
  approveReferralWithdrawal,
  approveSettlementBatch,
  createSettlementBatch,
  getFinanceCustomerWithdrawals,
  getFinanceWechatRefunds,
  queryFinanceWechatRefund,
  getFinanceSettlementDashboard,
  getReferralWithdrawals,
  payCustomerWalletWithdrawal,
  payReferralWithdrawal,
  paySettlementBatch,
  rejectCustomerWalletWithdrawal,
  rejectReferralWithdrawal,
  rejectSettlementBatch,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  CreateSettlementBatchInput,
  IncentiveEarning,
  SettlementBatch,
  WithdrawalRequest,
  WechatCashRefund,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const earnings = ref<IncentiveEarning[]>([])
const batches = ref<SettlementBatch[]>([])
const withdrawals = ref<WithdrawalRequest[]>([])
const cashRefunds = ref<WechatCashRefund[]>([])
const tab = ref<'earnings' | 'batches' | 'withdrawals' | 'refunds'>('earnings')
const modalOpen = ref(false)
const earningsPage = ref(1)
const batchesPage = ref(1)
const withdrawalsPage = ref(1)
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
const earningsPageCount = computed(() => Math.max(1, Math.ceil(earnings.value.length / pageSize)))
const batchesPageCount = computed(() => Math.max(1, Math.ceil(batches.value.length / pageSize)))
const withdrawalsPageCount = computed(() => Math.max(1, Math.ceil(withdrawals.value.length / pageSize)))
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
const pagedWithdrawals = computed(() => {
  const page = Math.min(withdrawalsPage.value, withdrawalsPageCount.value)
  const start = (page - 1) * pageSize
  return withdrawals.value.slice(start, start + pageSize)
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

function withdrawalTypeLabel(value: string) {
  const map: Record<string, string> = {
    customer_cash: '现金余额',
    customer_reward: '奖励余额',
    customer_referrer: '返佣余额',
  }
  return map[value] || value
}

function earningTypeLabel(value: string) {
  const map: Record<string, string> = {
    referral_reward: '推荐奖励',
    commerce_sales: '成交销售提成', commerce_referral: '成交用户分佣', commerce_sales_refund_reversal: '销售提成退款冲回',commerce_referral_refund_reversal:'用户分佣退款冲回',
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

async function approveWithdrawal(item: WithdrawalRequest) {
  if (!(await confirmAction({ title: '审核提现', message: '确认审核通过提现单“' + item.withdrawal_no + '”？', confirmText: '审核通过' }))) return
  error.value = ''
  try {
    if (item.beneficiary_type === 'customer_referrer') await approveReferralWithdrawal(item.id)
    else if (item.beneficiary_type === 'sales_staff') await reviewSalesWithdrawal(item.id,'approve')
    else await approveCustomerWalletWithdrawal(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '提现审核失败'
  }
}

async function rejectWithdrawal(item: WithdrawalRequest) {
  const reason = window.prompt('请输入驳回原因', '财务审核驳回')
  if (reason === null) return
  error.value = ''
  try {
    if (item.beneficiary_type === 'customer_referrer') await rejectReferralWithdrawal(item.id, reason)
    else if (item.beneficiary_type === 'sales_staff') await reviewSalesWithdrawal(item.id,'reject',reason)
    else await rejectCustomerWalletWithdrawal(item.id, reason)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '提现驳回失败'
  }
}

async function payWithdrawal(item: WithdrawalRequest) {
  if (!(await confirmAction({ title: '确认提现打款', message: '确认提现单“' + item.withdrawal_no + '”已经完成实际打款？', confirmText: '确认已打款' }))) return
  error.value = ''
  try {
    if (item.beneficiary_type === 'customer_referrer') await payReferralWithdrawal(item.id)
    else if (item.beneficiary_type === 'sales_staff') await reviewSalesWithdrawal(item.id,'pay')
    else await payCustomerWalletWithdrawal(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '确认提现打款失败'
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [data, customerWithdrawalData, referralWithdrawalData, refundData, salesWithdrawalData] = await Promise.all([
      getFinanceSettlementDashboard(),
      getFinanceCustomerWithdrawals('all'),
      getReferralWithdrawals('all'),
      getFinanceWechatRefunds(),
      getSalesWithdrawals(),
    ])
    earnings.value = data.earnings
    batches.value = data.batches
    cashRefunds.value = refundData.items
    withdrawals.value = [
      ...(customerWithdrawalData.items || []),
      ...(referralWithdrawalData.items || []),
      ...(salesWithdrawalData.items || []),
    ].sort((a, b) => new Date(b.requested_at).getTime() - new Date(a.requested_at).getTime())
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取收益结算失败'
  } finally {
    loading.value = false
  }
}

async function queryCashRefund(id: number) {
  if (saving.value) return
  saving.value = true
  try { await queryFinanceWechatRefund(id); await load() }
  catch (value) { error.value = value instanceof Error ? value.message : '退款核验暂不可用' }
  finally { saving.value = false }
}
function refundStatus(value: string) {
  return ({ queued: '待受理 / 待核验', processing: '原路退回中', abnormal: '异常待人工处理，保持冻结', closed: '已关闭并解冻', success: '微信已确认成功', partially_refunded: '部分成功，其余已解冻' } as Record<string,string>)[value] || value
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

    <p v-if="error" class="inline-error">{{ error }}</p>

    <section class="settings-card settlement-workspace">
      <div class="settings-tabs settlement-tabs">
        <button type="button" :class="{ active: tab === 'earnings' }" @click="tab = 'earnings'">
          收益明细
        </button>
        <button type="button" :class="{ active: tab === 'batches' }" @click="tab = 'batches'">
          结算批次
        </button>
        <button type="button" :class="{ active: tab === 'withdrawals' }" @click="tab = 'withdrawals'">
          提现申请
        </button>
        <button type="button" :class="{ active: tab === 'refunds' }" @click="tab = 'refunds'">充值本金退回</button>
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

      <div v-else-if="tab === 'batches'" class="data-table-wrap">
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

      <div v-else-if="tab === 'refunds'" class="data-table-wrap">
        <p>仅按微信签名通知或查单结果结算，不能手工标记打款。异常退款请到微信商户平台按子退款单号核对处理，期间不能解冻。</p>
        <table class="data-table"><thead><tr><th>客户</th><th>退回单号</th><th>申请金额</th><th>已退回 / 冻结 / 解冻</th><th>状态及子退款单</th><th>操作</th></tr></thead><tbody>
          <tr v-for="item in cashRefunds" :key="item.id"><td>{{ item.tenant_name }} #{{ item.tenant_id }}</td><td>{{ item.refund_no }}</td><td>{{ formatMoney(item.amount_cents) }}</td><td>{{ formatMoney(item.refunded_cents) }} / {{ formatMoney(item.frozen_cents) }} / {{ formatMoney(item.released_cents) }}</td><td>{{ refundStatus(item.status) }}<div v-for="part in item.items" :key="part.id">{{ part.refund_no }} · {{ part.recharge_no }} · {{ refundStatus(part.status) }} · {{ part.message }}</div></td><td><button v-if="canApprove && item.frozen_cents > 0" type="button" :disabled="saving" @click="queryCashRefund(item.id)">核验微信结果</button></td></tr>
        </tbody></table><div v-if="!cashRefunds.length" class="empty-state">暂无充值本金退回记录。</div>
      </div>
      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>提现单号</th>
              <th>客户</th>
              <th>类型</th>
              <th>金额</th>
              <th>状态</th>
              <th>申请时间</th>
              <th>审核 / 打款</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedWithdrawals" :key="item.id">
              <td><strong>{{ item.withdrawal_no }}</strong></td>
              <td>客户 #{{ item.beneficiary_id }}</td>
              <td>{{ withdrawalTypeLabel(item.beneficiary_type) }}</td>
              <td><strong>{{ formatMoney(item.amount_cents) }}</strong></td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
              <td>{{ new Date(item.requested_at).toLocaleString('zh-CN') }}</td>
              <td>
                <span>{{ item.approved_by_user_id ? '审核 #' + item.approved_by_user_id : '未审核' }}</span>
                <small>{{ item.paid_by_user_id ? '打款 #' + item.paid_by_user_id : '未打款' }}</small>
              </td>
              <td>
                <div class="table-actions">
                  <button v-if="canApprove && item.status === 'reviewing' && item.requested_by_user_id !== actorUserId" class="text-action" type="button" @click="approveWithdrawal(item)">审核通过</button>
                  <button v-if="canApprove && (item.status === 'reviewing' || item.status === 'approved')" class="text-action danger" type="button" @click="rejectWithdrawal(item)">驳回</button>
                  <button v-if="canPay && item.status === 'approved'" class="text-action" type="button" @click="payWithdrawal(item)">确认打款</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="withdrawals.length === 0" class="empty-state">暂无提现申请。</div>
        <PaginationBar
          :page="Math.min(withdrawalsPage, withdrawalsPageCount)"
          :total-pages="withdrawalsPageCount"
          :total="withdrawals.length"
          :page-size="pageSize"
          @update:page="withdrawalsPage = $event"
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
