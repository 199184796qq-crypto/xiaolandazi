<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import {
  createCustomerRechargeRequest,
  createCustomerWalletWithdrawal,
  createReferralWithdrawal,
  getCustomerWithdrawals,
  getFinanceDashboard,
  getReferralWallet,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type { BeneficiaryWalletDashboard, FinanceDashboard, WithdrawalRequest } from '../types'

type FinanceTab = 'ledger' | 'recharge' | 'payment' | 'purchase' | 'refund'

const data = ref<FinanceDashboard | null>(null)
const referralWallet = ref<BeneficiaryWalletDashboard | null>(null)
const customerWithdrawals = ref<WithdrawalRequest[]>([])
const loading = ref(true)
const error = useFeedbackErrorRef()
const activeTab = ref<FinanceTab>('ledger')
const showRecharge = ref(false)
const rechargeAmount = ref('')
const rechargeReason = ref('')
const rechargeSubmitting = ref(false)
const rechargeMessage = ref('')
const showWithdrawal = ref(false)
const withdrawalType = ref<'cash' | 'reward' | 'commission'>('cash')
const withdrawalAmount = ref('')
const withdrawalSubmitting = ref(false)
const withdrawalMessage = ref('')
const page = ref(1)
const pageSize = 20

const tabItems = computed(() => [
  { key: 'ledger' as const, label: '资金流水', count: data.value?.ledger.length ?? 0 },
  { key: 'recharge' as const, label: '充值记录', count: data.value?.recharges.length ?? 0 },
  { key: 'payment' as const, label: '支付记录', count: data.value?.payments.length ?? 0 },
  { key: 'purchase' as const, label: '消费 / 购买记录', count: data.value?.purchases.length ?? 0 },
  { key: 'refund' as const, label: '退款记录', count: data.value?.refunds.length ?? 0 },
])
const currentTotal = computed(() => {
  if (!data.value) return 0
  if (activeTab.value === 'ledger') return data.value.ledger.length
  if (activeTab.value === 'recharge') return data.value.recharges.length
  if (activeTab.value === 'payment') return data.value.payments.length
  if (activeTab.value === 'purchase') return data.value.purchases.length
  return data.value.refunds.length
})
const totalPages = computed(() => Math.max(1, Math.ceil(currentTotal.value / pageSize)))
const pageStart = computed(() => (Math.min(page.value, totalPages.value) - 1) * pageSize)
const pagedLedger = computed(() => data.value?.ledger.slice(pageStart.value, pageStart.value + pageSize) ?? [])
const pagedRecharges = computed(() => data.value?.recharges.slice(pageStart.value, pageStart.value + pageSize) ?? [])
const pagedPayments = computed(() => data.value?.payments.slice(pageStart.value, pageStart.value + pageSize) ?? [])
const pagedPurchases = computed(() => data.value?.purchases.slice(pageStart.value, pageStart.value + pageSize) ?? [])
const pagedRefunds = computed(() => data.value?.refunds.slice(pageStart.value, pageStart.value + pageSize) ?? [])

const allWithdrawals = computed(() => [
  ...customerWithdrawals.value,
  ...(referralWallet.value?.withdrawals || []),
].sort((a, b) => new Date(b.requested_at).getTime() - new Date(a.requested_at).getTime()))

function withdrawalCategory(item: WithdrawalRequest) {
  if (item.beneficiary_type === 'customer_cash') return 'cash'
  if (item.beneficiary_type === 'customer_reward') return 'reward'
  return 'commission'
}

function withdrawalLabel(type: 'cash' | 'reward' | 'commission') {
  if (type === 'cash') return '现金余额'
  if (type === 'reward') return '奖励余额'
  return '返佣余额'
}

const cashWithdrawals = computed(() => allWithdrawals.value.filter((item) => withdrawalCategory(item) === 'cash'))
const rewardWithdrawals = computed(() => allWithdrawals.value.filter((item) => withdrawalCategory(item) === 'reward'))
const commissionWithdrawals = computed(() => allWithdrawals.value.filter((item) => withdrawalCategory(item) === 'commission'))

function paidWithdrawalTotal(items: WithdrawalRequest[]) {
  return items
    .filter((item) => item.status === 'paid')
    .reduce((sum, item) => sum + Number(item.amount_cents || 0), 0)
}

const cashWithdrawalTotal = computed(() => paidWithdrawalTotal(cashWithdrawals.value))
const rewardWithdrawalTotal = computed(() => paidWithdrawalTotal(rewardWithdrawals.value))
const commissionWithdrawalTotal = computed(() => paidWithdrawalTotal(commissionWithdrawals.value))
const allWithdrawalTotal = computed(
  () => cashWithdrawalTotal.value + rewardWithdrawalTotal.value + commissionWithdrawalTotal.value,
)

const withdrawalGroups = computed(() => [
  { key: 'cash', label: '现金余额提现', total: cashWithdrawalTotal.value, items: cashWithdrawals.value },
  { key: 'reward', label: '奖励余额提现', total: rewardWithdrawalTotal.value, items: rewardWithdrawals.value },
  { key: 'commission', label: '返佣余额提现', total: commissionWithdrawalTotal.value, items: commissionWithdrawals.value },
])

async function loadFinance() {
  loading.value = true
  error.value = ''
  try {
    const [dashboard, referral, withdrawals] = await Promise.all([
      getFinanceDashboard(200),
      getReferralWallet(200),
      getCustomerWithdrawals('all'),
    ])
    data.value = dashboard
    referralWallet.value = referral
    customerWithdrawals.value = withdrawals.items || []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取财务信息失败'
  } finally {
    loading.value = false
  }
}

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function businessLabel(value: string) {
  const labels: Record<string, string> = {
    recharge: '账户充值',
    membership_purchase: '购买会员',
    membership_renewal: '会员续费',
    time_card_purchase: '购买时长卡',
    refund: '退款',
    manual_adjustment: '人工调账',
    referral_commission: '推荐返佣',
    referral_commission_reversal: '推荐返佣冲正',
    reward: '奖励',
  }
  return labels[value] || value || '资金变动'
}

function paymentLabel(value: string) {
  const labels: Record<string, string> = {
    wechat: '微信支付',
    alipay: '支付宝',
    manual: '后台人工充值',
    customer_request: '终端充值申请',
    agent: '代理充值',
    sandbox_manual: 'Sandbox 模拟支付',
    sandbox: 'Sandbox 模拟支付',
    pending: '待确认',
  }
  return labels[value] || value || '—'
}

function orderTypeLabel(value: string) {
  if (value === 'membership') return '会员订单'
  if (value === 'time_card') return '时长卡订单'
  return value || '消费订单'
}

function statusLabel(value: string) {
  const labels: Record<string, string> = {
    pending: '待处理',
    pending_approval: '待财务审核',
    reviewing: '待财务审核',
    approved: '已审核待打款',
    rejected: '已驳回',
    paid: '已打款',
    success: '成功',
    completed: '已完成',
    refunded: '已退款',
    partially_refunded: '部分退款',
    failed: '失败',
    cancelled: '已取消',
  }
  return labels[value] || value || '—'
}

function withdrawalAvailable(type: 'cash' | 'reward' | 'commission') {
  if (!data.value) return 0
  if (type === 'cash') return data.value.cash_balance_cents
  if (type === 'reward') return data.value.reward_balance_cents
  return data.value.commission_balance_cents
}

function openWithdrawal(type: 'cash' | 'reward' | 'commission') {
  withdrawalType.value = type
  withdrawalAmount.value = ''
  withdrawalMessage.value = ''
  showWithdrawal.value = true
}

async function submitWithdrawal() {
  const amountYuan = Number(withdrawalAmount.value)
  if (!Number.isFinite(amountYuan) || amountYuan <= 0) {
    error.value = '请输入正确的提现金额'
    return
  }
  const amountCents = Math.round(amountYuan * 100)
  if (amountCents > withdrawalAvailable(withdrawalType.value)) {
    error.value = '提现金额不能超过当前可提现余额'
    return
  }
  withdrawalSubmitting.value = true
  withdrawalMessage.value = ''
  error.value = ''
  try {
    if (withdrawalType.value === 'commission') {
      await createReferralWithdrawal(amountCents)
    } else {
      await createCustomerWalletWithdrawal(withdrawalType.value, amountCents)
    }
    withdrawalMessage.value = '提现申请已提交，等待财务审核。'
    withdrawalAmount.value = ''
    await loadFinance()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '提交提现申请失败'
  } finally {
    withdrawalSubmitting.value = false
  }
}

async function submitRechargeRequest() {
  const amountYuan = Number(rechargeAmount.value)
  if (!Number.isFinite(amountYuan) || amountYuan <= 0) {
    error.value = '请输入正确的充值金额'
    return
  }
  const reason = rechargeReason.value.trim()
  if (!reason) {
    error.value = '请填写充值申请说明'
    return
  }

  rechargeSubmitting.value = true
  rechargeMessage.value = ''
  error.value = ''
  try {
    const result = await createCustomerRechargeRequest({
      amount_cents: Math.round(amountYuan * 100),
      reason,
    })
    rechargeMessage.value =
      '充值申请已提交，等待财务审核。申请金额 ' +
      formatMoney(Math.round(amountYuan * 100)) +
      '。'
    activeTab.value = 'recharge'
    await loadFinance()
    if (result.task.status === 'pending') {
      rechargeAmount.value = ''
      rechargeReason.value = ''
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '提交充值申请失败'
  } finally {
    rechargeSubmitting.value = false
  }
}

onMounted(loadFinance)
</script>

<template>
  <div class="finance-page">
    <ModulePageNav context="workspace-customer" active-title="我的钱包" />
    <section class="feature-workspace-hero finance-hero finance-hero-compact">
      <div>
        <p class="section-kicker">MY WALLET</p>
        <h2>我的钱包</h2>
      </div>
    </section>

    <div v-if="loading" class="panel-loading">正在读取财务信息...</div>
    <div v-else-if="error && !data" class="global-error">{{ error }}</div>

    <template v-if="data">
      <p v-if="error" class="settings-error">{{ error }}</p>

      <section class="wallet-balance-grid">
        <article class="wallet-balance-card wallet-balance-card-cash">
          <div>
            <span class="wallet-label">现金余额</span>
            <strong>{{ formatMoney(data.cash_balance_cents) }}</strong>
          </div>
          <div class="wallet-card-actions">
            <button type="button" @click="showRecharge = true">充值</button>
            <button type="button" class="primary-button" @click="openWithdrawal('cash')">提现</button>
          </div>
        </article>

        <article class="wallet-balance-card wallet-balance-card-reward">
          <div>
            <span class="wallet-label">奖励余额</span>
            <strong>{{ formatMoney(data.reward_balance_cents) }}</strong>
          </div>
          <div class="wallet-card-actions">
            <button type="button" class="primary-button" @click="openWithdrawal('reward')">提现</button>
          </div>
        </article>

        <article class="wallet-balance-card wallet-balance-card-commission">
          <div>
            <span class="wallet-label">返佣余额</span>
            <strong>{{ formatMoney(data.commission_balance_cents) }}</strong>
            <small v-if="data.commission_frozen_cents > 0">
              冻结中 {{ formatMoney(data.commission_frozen_cents) }}
            </small>
          </div>
          <div class="wallet-card-actions">
            <button type="button" class="primary-button" @click="openWithdrawal('commission')">提现</button>
          </div>
        </article>
      </section>

      <section class="finance-records-card">
        <div class="finance-records-header">
          <div>
            <span class="section-kicker">ACCOUNT HISTORY</span>
            <h3>账户记录</h3>
          </div>
          <button class="ghost-button" type="button" @click="loadFinance">刷新</button>
        </div>

        <div class="finance-tabs" role="tablist">
          <button
            v-for="item in tabItems"
            :key="item.key"
            class="finance-tab"
            :class="{ active: activeTab === item.key }"
            type="button"
            @click="activeTab = item.key; page = 1"
          >
            {{ item.label }}
            <span>{{ item.count }}</span>
          </button>
        </div>

        <div v-if="activeTab === 'ledger'" class="finance-table-wrap">
          <table v-if="data.ledger.length" class="finance-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>项目</th>
                <th>订单号</th>
                <th>金额</th>
                <th>变动后余额</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in pagedLedger" :key="item.id">
                <td>{{ formatDate(item.occurred_at) }}</td>
                <td>
                  <strong>{{ businessLabel(item.business_type) }}</strong>
                  <small v-if="item.reason">{{ item.reason }}</small>
                </td>
                <td>{{ item.order_no || '—' }}</td>
                <td
                  class="money-change"
                  :class="{ income: item.direction === 'credit', expense: item.direction === 'debit' }"
                >
                  {{ item.direction === 'credit' ? '+' : '-' }}{{ formatMoney(item.amount_cents) }}
                </td>
                <td>{{ formatMoney(item.balance_after_cents) }}</td>
              </tr>
            </tbody>
          </table>
          <div v-else class="finance-empty">
            <strong>暂无资金流水</strong>
            <span>充值、消费或退款发生后会自动记录在这里。</span>
          </div>
        </div>

        <div v-else-if="activeTab === 'recharge'" class="finance-table-wrap">
          <table v-if="data.recharges.length" class="finance-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>充值单号</th>
                <th>充值金额</th>
                <th>到账金额</th>
                <th>方式</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in pagedRecharges" :key="item.id">
                <td>{{ formatDate(item.paid_at || item.created_at) }}</td>
                <td>{{ item.recharge_no }}</td>
                <td>{{ formatMoney(item.requested_amount_cents) }}</td>
                <td>{{ formatMoney(item.credited_amount_cents) }}</td>
                <td>{{ paymentLabel(item.payment_method) }}</td>
                <td><span class="record-status">{{ statusLabel(item.status) }}</span></td>
              </tr>
            </tbody>
          </table>
          <div v-else class="finance-empty">
            <strong>暂无充值记录</strong>
            <span>还没有充值记录。提交充值申请并经财务审核后，会在这里保留永久记录。</span>
          </div>
        </div>

        <div v-else-if="activeTab === 'payment'" class="finance-table-wrap">
          <table v-if="data.payments.length" class="finance-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>支付单号</th>
                <th>订单号</th>
                <th>方式</th>
                <th>订单应付</th>
                <th>输入金额</th>
                <th>实际支付</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in pagedPayments" :key="item.id">
                <td>{{ formatDate(item.paid_at || item.created_at) }}</td>
                <td>
                  <strong>{{ item.payment_no }}</strong>
                  <small v-if="item.external_trade_no">{{ item.external_trade_no }}</small>
                </td>
                <td>{{ item.order_no }}</td>
                <td>
                  <span class="record-status sandbox-record-status">
                    {{ paymentLabel(item.payment_method) }}
                  </span>
                </td>
                <td>{{ formatMoney(item.expected_amount_cents) }}</td>
                <td>{{ formatMoney(item.input_amount_cents) }}</td>
                <td><strong>{{ formatMoney(item.paid_amount_cents) }}</strong></td>
                <td>
                  <span class="record-status">{{ statusLabel(item.status) }}</span>
                  <small v-if="item.failure_reason === 'amount_mismatch'">金额不一致</small>
                  <small v-else-if="item.failure_reason === 'simulated_failure'">测试模拟失败</small>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-else class="finance-empty">
            <strong>暂无支付记录</strong>
            <span>终端商城模拟支付或未来真实支付发生后，会在这里保留支付渠道和来源订单。</span>
          </div>
        </div>

        <div v-else-if="activeTab === 'purchase'" class="finance-table-wrap">
          <table v-if="data.purchases.length" class="finance-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>订单类型</th>
                <th>订单号</th>
                <th>原价</th>
                <th>优惠</th>
                <th>实付</th>
                <th>退款</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in pagedPurchases" :key="item.id">
                <td>{{ formatDate(item.paid_at || item.created_at) }}</td>
                <td>{{ orderTypeLabel(item.order_type) }}</td>
                <td>{{ item.order_no }}</td>
                <td>{{ formatMoney(item.list_amount_cents) }}</td>
                <td>{{ formatMoney(item.discount_amount_cents) }}</td>
                <td><strong>{{ formatMoney(item.paid_amount_cents) }}</strong></td>
                <td>{{ formatMoney(item.refunded_amount_cents) }}</td>
                <td><span class="record-status">{{ statusLabel(item.status) }}</span></td>
              </tr>
            </tbody>
          </table>
          <div v-else class="finance-empty">
            <strong>暂无消费 / 购买记录</strong>
            <span>购买会员或时长卡后，这里会保留成交价格、优惠和订单状态。</span>
          </div>
        </div>

        <div v-else class="finance-table-wrap">
          <table v-if="data.refunds.length" class="finance-table">
            <thead>
              <tr>
                <th>时间</th>
                <th>退款单号</th>
                <th>来源</th>
                <th>退款金额</th>
                <th>方式</th>
                <th>原因</th>
                <th>状态</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in pagedRefunds" :key="item.id">
                <td>{{ formatDate(item.processed_at || item.created_at) }}</td>
                <td>{{ item.refund_no }}</td>
                <td>{{ item.source_type }} #{{ item.source_id }}</td>
                <td><strong>{{ formatMoney(item.refund_amount_cents) }}</strong></td>
                <td>{{ item.refund_method }}</td>
                <td>{{ item.reason || '—' }}</td>
                <td><span class="record-status">{{ statusLabel(item.status) }}</span></td>
              </tr>
            </tbody>
          </table>
          <div v-else class="finance-empty">
            <strong>暂无退款记录</strong>
            <span>以后任何退款都会保留原订单关系和独立退款流水，不会删除历史消费。</span>
          </div>
        </div>
        <PaginationBar
          :page="Math.min(page, totalPages)"
          :total-pages="totalPages"
          :total="currentTotal"
          :page-size="pageSize"
          @update:page="page = $event"
        />
      </section>

      <section class="withdrawal-records-card">
        <div class="withdrawal-records-head">
          <div>
            <span class="section-kicker">WITHDRAWAL HISTORY</span>
            <h3>提现记录</h3>
          </div>
          <div class="withdrawal-total-box">
            <span>累计已提现</span>
            <strong>{{ formatMoney(allWithdrawalTotal) }}</strong>
          </div>
        </div>

        <div class="withdrawal-summary-grid">
          <div v-for="group in withdrawalGroups" :key="group.key">
            <span>{{ group.label }}</span>
            <strong>{{ formatMoney(group.total) }}</strong>
          </div>
        </div>

        <article v-for="group in withdrawalGroups" :key="'table-' + group.key" class="withdrawal-category-block">
          <header>
            <div>
              <strong>{{ group.label }}</strong>
              <small>已提现合计 {{ formatMoney(group.total) }}</small>
            </div>
            <span>{{ group.items.length }} 条</span>
          </header>
          <div class="finance-table-wrap">
            <table v-if="group.items.length" class="finance-table withdrawal-table">
              <thead>
                <tr>
                  <th>申请时间</th>
                  <th>提现单号</th>
                  <th>金额</th>
                  <th>状态</th>
                  <th>完成时间</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="item in group.items" :key="item.id">
                  <td>{{ formatDate(item.requested_at) }}</td>
                  <td>{{ item.withdrawal_no }}</td>
                  <td><strong>{{ formatMoney(item.amount_cents) }}</strong></td>
                  <td><span class="record-status">{{ statusLabel(item.status) }}</span></td>
                  <td>{{ formatDate(item.paid_at || item.approved_at) }}</td>
                </tr>
              </tbody>
            </table>
            <div v-else class="finance-empty compact"><strong>暂无提现记录</strong></div>
          </div>
        </article>
      </section>
    </template>

    <div v-if="showRecharge" class="modal-backdrop" @click.self="showRecharge = false">
      <section class="dialog-card recharge-dialog">
        <div class="dialog-icon">¥</div>
        <h3>提交充值申请</h3>
        <p>
          当前暂未接入在线支付。你可以先提交充值申请，财务审核通过后才会入账，整个过程会生成充值单、审批记录和钱包流水。
        </p>

        <form class="recharge-request-form" @submit.prevent="submitRechargeRequest">
          <label>
            <span>申请金额（元）</span>
            <input
              v-model="rechargeAmount"
              type="number"
              min="0.01"
              step="0.01"
              placeholder="例如：500.00"
              required
            />
          </label>

          <label>
            <span>申请说明</span>
            <textarea
              v-model="rechargeReason"
              rows="3"
              maxlength="512"
              placeholder="例如：线下转账充值，请财务核对后入账"
              required
            ></textarea>
          </label>

          <p v-if="rechargeMessage" class="recharge-request-success">
            {{ rechargeMessage }}
          </p>

          <div class="dialog-actions">
            <button class="ghost-button" type="button" @click="showRecharge = false">
              关闭
            </button>
            <button class="primary-button" type="submit" :disabled="rechargeSubmitting">
              {{ rechargeSubmitting ? '提交中...' : '提交充值申请' }}
            </button>
          </div>
        </form>
      </section>
    </div>
    <div v-if="showWithdrawal" class="modal-backdrop" @click.self="showWithdrawal = false">
      <section class="dialog-card withdrawal-dialog">
        <div class="dialog-icon">提</div>
        <h3>{{ withdrawalLabel(withdrawalType) }}提现</h3>
        <div class="withdrawal-available-line">
          <span>当前可提现</span>
          <strong>{{ formatMoney(withdrawalAvailable(withdrawalType)) }}</strong>
        </div>
        <form class="recharge-request-form" @submit.prevent="submitWithdrawal">
          <label>
            <span>提现金额（元）</span>
            <input
              v-model="withdrawalAmount"
              type="number"
              min="0.01"
              step="0.01"
              :max="withdrawalAvailable(withdrawalType) / 100"
              placeholder="请输入提现金额"
              required
            />
          </label>
          <p v-if="withdrawalMessage" class="recharge-request-success">{{ withdrawalMessage }}</p>
          <div class="dialog-actions">
            <button class="ghost-button" type="button" @click="showWithdrawal = false">取消</button>
            <button class="primary-button" type="submit" :disabled="withdrawalSubmitting">
              {{ withdrawalSubmitting ? '提交中...' : '提交提现申请' }}
            </button>
          </div>
        </form>
      </section>
    </div>

  </div>
</template>
