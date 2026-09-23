<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import { createCustomerRechargeRequest, getFinanceDashboard } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import type { FinanceDashboard } from '../types'

type FinanceTab = 'ledger' | 'recharge' | 'payment' | 'purchase' | 'refund'

const data = ref<FinanceDashboard | null>(null)
const loading = ref(true)
const error = useFeedbackErrorRef()
const activeTab = ref<FinanceTab>('ledger')
const showRecharge = ref(false)
const rechargeAmount = ref('')
const rechargeReason = ref('')
const rechargeSubmitting = ref(false)
const rechargeMessage = ref('')

const tabItems = computed(() => [
  { key: 'ledger' as const, label: '资金流水', count: data.value?.ledger.length ?? 0 },
  { key: 'recharge' as const, label: '充值记录', count: data.value?.recharges.length ?? 0 },
  { key: 'payment' as const, label: '支付记录', count: data.value?.payments.length ?? 0 },
  { key: 'purchase' as const, label: '消费 / 购买记录', count: data.value?.purchases.length ?? 0 },
  { key: 'refund' as const, label: '退款记录', count: data.value?.refunds.length ?? 0 },
])

async function loadFinance() {
  loading.value = true
  error.value = ''
  try {
    data.value = await getFinanceDashboard(50)
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

function formatSeconds(value: number) {
  if (!value) return '0 小时'
  const hours = Math.floor(value / 3600)
  const minutes = Math.floor((value % 3600) / 60)
  if (hours > 0 && minutes > 0) return hours + ' 小时 ' + minutes + ' 分钟'
  if (hours > 0) return hours + ' 小时'
  return minutes + ' 分钟'
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
    rejected: '已驳回',
    paid: '已支付',
    success: '成功',
    completed: '已完成',
    refunded: '已退款',
    partially_refunded: '部分退款',
    failed: '失败',
    cancelled: '已取消',
  }
  return labels[value] || value || '—'
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
    <ModulePageNav context="workspace-customer" active-title="财务管理" />
    <section class="page-hero finance-hero">
      <div>
        <p class="section-kicker">FINANCE CENTER</p>
        <h2>财务管理</h2>
        <p>查看账户余额、充值、消费、时长卡购买、模拟支付和退款记录。Sandbox 支付会明确标记，不会与未来真实支付混淆。</p>
      </div>
    </section>

    <div v-if="loading" class="panel-loading">正在读取财务信息...</div>
    <div v-else-if="error && !data" class="global-error">{{ error }}</div>

    <template v-if="data">
      <p v-if="error" class="settings-error">{{ error }}</p>

      <section class="finance-summary-grid">
        <article class="wallet-hero-card">
          <div class="wallet-card-top">
            <div>
              <span class="wallet-label">账户可用余额</span>
              <strong>{{ formatMoney(data.total_balance_cents) }}</strong>
            </div>
            <button class="wallet-recharge-button" type="button" @click="showRecharge = true">
              充值
            </button>
          </div>

          <div class="wallet-balance-split">
            <div>
              <span>现金余额</span>
              <strong>{{ formatMoney(data.cash_balance_cents) }}</strong>
            </div>
            <div>
              <span>奖励余额</span>
              <strong>{{ formatMoney(data.reward_balance_cents) }}</strong>
            </div>
          </div>

          <p>钱包支付会按订单规则扣减余额；当前商城 Sandbox 模拟支付为独立测试渠道，不会扣减现金余额。</p>
        </article>

        <article class="finance-stat-card">
          <span class="finance-stat-icon">↘</span>
          <div>
            <span>本月消费</span>
            <strong>{{ formatMoney(data.month_spent_cents) }}</strong>
            <small>统计已支付订单净额，包含明确标记的 Sandbox 测试支付</small>
          </div>
        </article>

        <article class="finance-stat-card">
          <span class="finance-stat-icon">◷</span>
          <div>
            <span>当前可用时长</span>
            <strong>{{ formatSeconds(data.available_seconds) }}</strong>
            <small>所有未过期时长资产合计</small>
          </div>
        </article>

        <article class="finance-stat-card">
          <span class="finance-stat-icon">◇</span>
          <div>
            <span>当前会员</span>
            <strong>{{ data.membership_name || '暂未开通' }}</strong>
            <small>会员等级由商业后台统一配置</small>
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
            @click="activeTab = item.key"
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
              <tr v-for="item in data.ledger" :key="item.id">
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
              <tr v-for="item in data.recharges" :key="item.id">
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
              <tr v-for="item in data.payments" :key="item.id">
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
            <span>商城模拟支付或未来真实支付发生后，会在这里保留支付渠道和来源订单。</span>
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
              <tr v-for="item in data.purchases" :key="item.id">
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
              <tr v-for="item in data.refunds" :key="item.id">
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
  </div>
</template>