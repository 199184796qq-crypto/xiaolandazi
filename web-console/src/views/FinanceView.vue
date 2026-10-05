<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import {
  createWechatRecharge,
  getWechatRecharge,
  prepayWechatNativeRecharge,
  getWechatRefundWallet,
  createWechatCashRefund,
  queryWechatCashRefund,
  queryWechatRecharge,
  createCustomerWalletWithdrawal,
  createReferralWithdrawal,
  getCustomerBeanWallet,
  getCustomerWithdrawals,
  getFinanceDashboard,
  getReferralWallet,
  purchaseCustomerBeans,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type { BeanWalletDashboard, BeneficiaryWalletDashboard, FinanceDashboard, WithdrawalRequest, RechargeRecord, WechatRefundWallet } from '../types'
import { refundAmountCents, refundDraft, clearRefundDraft, savedRefundAmount } from '../wechatRefund'
import { nativeRechargeAmount, nativeRechargeDraft, savedNativeRechargeDraft, rememberNativeRechargeID, clearNativeRechargeDraft, nativeQRIsValid } from '../wechatNativeRecharge'
import { readRefundAwareWallet, refundNeedsSync, refundProgressMessage, type RefundProgress } from '../../../shared/wechatRefundSync'

type FinanceTab = 'ledger' | 'bean' | 'recharge' | 'payment' | 'purchase' | 'refund'

const data = ref<FinanceDashboard | null>(null)
const referralWallet = ref<BeneficiaryWalletDashboard | null>(null)
const customerWithdrawals = ref<WithdrawalRequest[]>([])
const refundWallet = ref<WechatRefundWallet | null>(null)
const walletSyncWarning = ref('')
const cashRefundWarning = ref('')
const acceptedCashRefund = ref<RefundProgress | null>(null)
let financeReadSequence = 0
let financeMounted = true
const beanWallet = ref<BeanWalletDashboard | null>(null)
const loading = ref(true)
const error = useFeedbackErrorRef()
const activeTab = ref<FinanceTab>('ledger')
const showRecharge = ref(false)
const rechargeAmount = ref('')
const rechargeOrder = ref<RechargeRecord | null>(null)
const rechargeQRCode = ref('')
const rechargeRestoring = ref(true)
const rechargeQRExpires = ref('')
const rechargeQRError = ref('')
const rechargeClock = ref(Date.now())
const rechargeQRValid = computed(() => nativeQRIsValid(rechargeQRCode.value, rechargeQRExpires.value, rechargeClock.value))
const rechargeCountdown = computed(() => {
  const seconds = Math.max(0, Math.ceil((Date.parse(rechargeQRExpires.value) - rechargeClock.value) / 1000))
  return Number.isFinite(seconds) ? `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}` : ''
})
let rechargeTimer: ReturnType<typeof setInterval> | undefined
let rechargeClockTimer: ReturnType<typeof setInterval> | undefined
let refundTimer: ReturnType<typeof setInterval> | undefined
const rechargeSubmitting = ref(false)
const rechargeMessage = ref('')
const showWithdrawal = ref(false)
const withdrawalType = ref<'cash' | 'reward' | 'commission'>('cash')
const withdrawalAmount = ref('')
const withdrawalSubmitting = ref(false)
const withdrawalMessage = ref('')
const showBeanPurchase = ref(false)
const beanPurchaseYuan = ref('')
const beanPurchaseSubmitting = ref(false)
const beanPurchaseMessage = ref('')
const page = ref(1)
const pageSize = 20

const tabItems = computed(() => [
  { key: 'ledger' as const, label: '资金流水', count: data.value?.ledger.length ?? 0 },
  { key: 'bean' as const, label: '小蓝豆流水', count: beanWallet.value?.ledger.length ?? 0 },
  { key: 'recharge' as const, label: '充值记录', count: data.value?.recharges.length ?? 0 },
  { key: 'payment' as const, label: '支付记录', count: data.value?.payments.length ?? 0 },
  { key: 'purchase' as const, label: '消费 / 购买记录', count: data.value?.purchases.length ?? 0 },
  { key: 'refund' as const, label: '退款记录', count: data.value?.refunds.length ?? 0 },
])
const currentTotal = computed(() => {
  if (!data.value) return 0
  if (activeTab.value === 'ledger') return data.value.ledger.length
  if (activeTab.value === 'bean') return beanWallet.value?.ledger.length ?? 0
  if (activeTab.value === 'recharge') return data.value.recharges.length
  if (activeTab.value === 'payment') return data.value.payments.length
  if (activeTab.value === 'purchase') return data.value.purchases.length
  return data.value.refunds.length
})
const totalPages = computed(() => Math.max(1, Math.ceil(currentTotal.value / pageSize)))
const pageStart = computed(() => (Math.min(page.value, totalPages.value) - 1) * pageSize)
const pagedLedger = computed(() => data.value?.ledger.slice(pageStart.value, pageStart.value + pageSize) ?? [])
const pagedBeanLedger = computed(() => beanWallet.value?.ledger.slice(pageStart.value, pageStart.value + pageSize) ?? [])
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
  const sequence = ++financeReadSequence
  loading.value = true
  const [wallet, referral, withdrawals, beans] = await Promise.all([
    readRefundAwareWallet(() => getFinanceDashboard(200), getWechatRefundWallet, data.value),
    ...awaitableWalletExtras(),
  ])
  if (!financeMounted || sequence !== financeReadSequence) return
  data.value = wallet.dashboard
  refundWallet.value = wallet.refunds
  if (referral.status === 'fulfilled') referralWallet.value = referral.value
  if (withdrawals.status === 'fulfilled') customerWithdrawals.value = withdrawals.value.items || []
  if (beans.status === 'fulfilled') beanWallet.value = beans.value
  walletSyncWarning.value = wallet.warning || ([referral, withdrawals, beans].some(r => r.status === 'rejected') ? '部分账户记录正在同步，页面会自动重试。' : '')
  const current = acceptedCashRefund.value
    ? wallet.refunds?.records.find(r => r.id === acceptedCashRefund.value?.id)
    : wallet.refunds?.records.find(refundNeedsSync)
  if (current) {
    acceptedCashRefund.value = current
    if (withdrawalType.value === 'cash') withdrawalMessage.value = refundProgressMessage(current)
    cashRefundWarning.value = ''
  }
  loading.value = false
}

function awaitableWalletExtras() {
  const settled = <T,>(promise: Promise<T>): Promise<PromiseSettledResult<T>> => promise.then(
    value => ({ status: 'fulfilled', value }), reason => ({ status: 'rejected', reason }),
  )
  return [settled(getReferralWallet(200)), settled(getCustomerWithdrawals('all')), settled(getCustomerBeanWallet())] as const
}

const beanPurchasePreview = computed(() => {
  const yuan = Number(beanPurchaseYuan.value)
  if (!Number.isFinite(yuan) || yuan <= 0 || !beanWallet.value) return 0
  return Math.floor(yuan * beanWallet.value.settings.purchase_beans_per_yuan)
})

async function submitBeanPurchase() {
  const yuan = Number(beanPurchaseYuan.value)
  if (!Number.isFinite(yuan) || yuan <= 0 || !beanWallet.value) {
    error.value = '请输入正确的购买金额'
    return
  }
  const amountCents = Math.round(yuan * 100)
  if (amountCents < beanWallet.value.settings.minimum_purchase_cents) {
    error.value = `最低购买金额为 ${formatMoney(beanWallet.value.settings.minimum_purchase_cents)}`
    return
  }
  beanPurchaseSubmitting.value = true
  beanPurchaseMessage.value = ''
  error.value = ''
  try {
    const idempotencyKey = typeof crypto !== 'undefined' && crypto.randomUUID
      ? crypto.randomUUID()
      : `bean-${Date.now()}-${Math.random().toString(16).slice(2)}`
    const order = await purchaseCustomerBeans(amountCents, idempotencyKey)
    beanPurchaseMessage.value = `购买成功，${order.credited_beans.toLocaleString()} 小蓝豆已到账。`
    beanPurchaseYuan.value = ''
    await loadFinance()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '购买小蓝豆失败'
  } finally {
    beanPurchaseSubmitting.value = false
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
    bean_purchase: '购买小蓝豆',
    refund: '退款',
    wechat_refund_hold: '原路退回冻结', wechat_refund_release: '退回关闭解冻',
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
    wechat_jsapi: '微信在线充值',
    wechat_native: '微信扫码充值',
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
    queued: '待微信受理', processing: '原路退回中', abnormal: '退款异常，请联系客服', closed: '已关闭，金额已解冻',
  }
  return labels[value] || value || '—'
}

function withdrawalAvailable(type: 'cash' | 'reward' | 'commission') {
  if (!data.value) return 0
  if (type === 'cash') return refundWallet.value?.refundable_cents || 0
  if (type === 'reward') return data.value.reward_balance_cents
  return data.value.commission_balance_cents
}

function openWithdrawal(type: 'cash' | 'reward' | 'commission') {
  withdrawalType.value = type
  cashRefundWarning.value = ''
  if (type === 'cash' && refundNeedsSync(acceptedCashRefund.value)) {
    withdrawalAmount.value = (acceptedCashRefund.value!.amount_cents / 100).toFixed(2)
    withdrawalMessage.value = refundProgressMessage(acceptedCashRefund.value!)
  } else {
    if (type === 'cash') acceptedCashRefund.value = null
    withdrawalAmount.value = type === 'cash' ? savedRefundAmount() : ''
    withdrawalMessage.value = ''
  }
  showWithdrawal.value = true
}

function openBeanPurchase() {
  if (!beanWallet.value?.settings.enabled) return
  beanPurchaseMessage.value = ''
  showBeanPurchase.value = true
}

async function submitWithdrawal() {
  if (withdrawalSubmitting.value) return
  if (withdrawalType.value === 'cash') { await submitCashRefund(); return }
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

async function submitCashRefund() {
  if (acceptedCashRefund.value) { await checkCashRefund(acceptedCashRefund.value.id); return }
  let draft: ReturnType<typeof refundDraft>
  try { draft = refundDraft(refundAmountCents(String(withdrawalAmount.value)), refundWallet.value?.refundable_cents || 0) }
  catch (err) { cashRefundWarning.value = err instanceof Error ? err.message : '请输入正确的退回金额'; return }
  withdrawalSubmitting.value = true; withdrawalMessage.value = ''; cashRefundWarning.value = ''; error.value = ''
  try {
    const result = await createWechatCashRefund(draft.cents, draft.key)
    acceptedCashRefund.value = result
    try { clearRefundDraft() } catch { /* Acceptance must not be hidden by a storage failure. */ }
    withdrawalMessage.value = refundProgressMessage(result)
    await loadFinance()
  } catch { cashRefundWarning.value = '退回结果暂未获取，请先刷新退款记录核验。原请求标识已保留，勿另发一笔。'; walletSyncWarning.value = '退回状态待核验，页面会自动同步记录。' }
  finally { withdrawalSubmitting.value = false }
}

async function checkCashRefund(id: number) {
  if (withdrawalSubmitting.value) return
  withdrawalSubmitting.value = true
  try {
    const result = await queryWechatCashRefund(id)
    acceptedCashRefund.value = result
    if (withdrawalType.value === 'cash') withdrawalMessage.value = refundProgressMessage(result)
    cashRefundWarning.value = ''
    await loadFinance()
  }
  catch { cashRefundWarning.value = '暂时无法核验退回进度，不代表退款失败，请勿重复提交。'; walletSyncWarning.value = '退回状态正在同步，页面会自动重试。' }
  finally { withdrawalSubmitting.value = false }
}

async function submitRechargeRequest() {
  if (rechargeSubmitting.value || rechargeRestoring.value) return
  if (rechargeOrder.value && rechargeQRValid.value) { await checkRechargeResult(); return }
  rechargeSubmitting.value = true
  rechargeMessage.value = ''
  rechargeQRError.value = ''
  error.value = ''
  try {
    if (!rechargeOrder.value) {
      const amountCents = nativeRechargeAmount(rechargeAmount.value)
      const draft = nativeRechargeDraft(amountCents)
      rechargeOrder.value = await createWechatRecharge(amountCents, draft.key)
      rememberNativeRechargeID(draft, rechargeOrder.value.id)
    }
    const result = await prepayWechatNativeRecharge(rechargeOrder.value.id)
    rechargeOrder.value = result.recharge
    rechargeClock.value = Date.now()
    if (result.recharge.status === 'paid') {
      if (result.recharge.credited_amount_cents !== result.recharge.requested_amount_cents) throw new Error('充值入账金额异常，请先联系财务核对，不要重复付款')
      rechargeQRCode.value = ''; rechargeQRExpires.value = ''
      clearNativeRechargeDraft()
      rechargeMessage.value = `充值成功，${formatMoney(result.recharge.credited_amount_cents)}已到账。`
    } else {
      if (!nativeQRIsValid(result.qr_code_data_url || '', result.expires_at || '')) throw new Error('付款二维码未生成或已过期，请重试原充值单')
      rechargeQRCode.value = result.qr_code_data_url!
      rechargeQRExpires.value = result.expires_at!
      rechargeMessage.value = '请用手机微信扫一扫上方二维码，确认金额后付款，无需在手机登录。'
    }
    activeTab.value = 'recharge'
    await loadFinance()
  } catch (err) {
    rechargeQRError.value = err instanceof Error ? err.message : '获取微信付款二维码失败，请重试原充值单'
  } finally {
    rechargeSubmitting.value = false
  }
}

async function checkRechargeResult() {
  if (!rechargeOrder.value || rechargeSubmitting.value) return
  rechargeSubmitting.value = true
  try {
    const result = await queryWechatRecharge(rechargeOrder.value.id)
    rechargeOrder.value = result.recharge
    rechargeQRError.value = ''
    if (result.recharge.status === 'paid' && result.recharge.credited_amount_cents !== result.recharge.requested_amount_cents) throw new Error('充值入账金额异常，请先联系财务核对，不要重复付款')
    if (result.recharge.status === 'paid' && result.recharge.credited_amount_cents === result.recharge.requested_amount_cents) {
      rechargeMessage.value = `充值成功，${formatMoney(result.recharge.credited_amount_cents)} 已到账。`
      rechargeQRCode.value = ''; rechargeQRExpires.value = ''; rechargeQRError.value = ''
      clearNativeRechargeDraft()
      await loadFinance()
    } else rechargeMessage.value = result.trade_state === 'USERPAYING' ? '微信支付确认中，请稍候，不要重复付款。' : '等待微信付款，确认到账后会自动更新余额。'
  } catch (err) { rechargeQRError.value = err instanceof Error ? err.message : '微信到账结果暂未确认，请稍后查询，不要重复付款' }
  finally { rechargeSubmitting.value = false }
}

async function resumeNativeRecharge() {
  try {
    const draft = savedNativeRechargeDraft()
    if (!draft) return
    rechargeAmount.value = (draft.cents / 100).toFixed(2)
    if (draft.id) {
      rechargeOrder.value = await getWechatRecharge(draft.id)
      if (rechargeOrder.value.status === 'paid' && rechargeOrder.value.credited_amount_cents === rechargeOrder.value.requested_amount_cents) clearNativeRechargeDraft()
    }
  } catch { rechargeQRError.value = '原充值状态暂不可读取，请先核对充值记录，勿重复付款' }
  finally { rechargeRestoring.value = false }
}

function refreshWalletOnReturn() { if (!document.hidden && !loading.value && !withdrawalSubmitting.value) void loadFinance() }

onMounted(() => {
  void loadFinance()
  void resumeNativeRecharge()
  rechargeClockTimer = setInterval(() => { rechargeClock.value = Date.now() }, 1000)
  refundTimer = setInterval(() => {
    if (refundNeedsSync(acceptedCashRefund.value) || refundWallet.value?.records.some(refundNeedsSync) || walletSyncWarning.value) refreshWalletOnReturn()
  }, 5000)
  document.addEventListener('visibilitychange', refreshWalletOnReturn)
  window.addEventListener('focus', refreshWalletOnReturn)
  rechargeTimer = setInterval(() => {
    if (showRecharge.value && rechargeOrder.value?.status === 'pending' && !document.hidden) void checkRechargeResult()
  }, 5000)
})
onUnmounted(() => {
  financeMounted = false
  document.removeEventListener('visibilitychange', refreshWalletOnReturn)
  window.removeEventListener('focus', refreshWalletOnReturn)
  if (rechargeTimer) clearInterval(rechargeTimer); if (rechargeClockTimer) clearInterval(rechargeClockTimer); if (refundTimer) clearInterval(refundTimer)
})
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
    <div v-else-if="!data" class="global-error">{{ error || '钱包数据暂未读取，请稍后重试。' }}</div>
    <p v-if="walletSyncWarning" class="wallet-sync-warning" role="status">{{ walletSyncWarning }}</p>

    <template v-if="data">
      <p v-if="error" class="settings-error">{{ error }}</p>

      <section class="wallet-balance-grid">
        <article class="wallet-balance-card wallet-balance-card-cash wallet-dashboard-card">
          <header>
            <span class="wallet-type-icon" aria-hidden="true"><svg viewBox="0 0 32 32"><path d="M7 9.2h16.5a3.8 3.8 0 0 1 3.8 3.8v11H7a4 4 0 0 1-4-4V8.2A4.2 4.2 0 0 1 7.2 4H23v4H7.2a1.2 1.2 0 1 0 0 2.4"/><path d="M19 14h9v7h-9a3.5 3.5 0 1 1 0-7Z"/><circle cx="20" cy="17.5" r="1.2"/></svg></span>
            <div><span class="wallet-label">现金余额</span><small>可用于充值、购买等支付场景</small></div><b>•••</b>
          </header>
          <strong class="wallet-balance-amount">{{ formatMoney(data.cash_balance_cents) }}</strong>
          <img class="wallet-visual cash-visual" src="/assets/wallet/cash-wallet-3d.png" alt="" />
          <p class="wallet-refund-summary"><span>可退充值本金 {{ refundWallet ? formatMoney(refundWallet.refundable_cents) : '核验中' }}</span><span>冻结 {{ refundWallet ? formatMoney(refundWallet.frozen_cents) : '核验中' }}</span></p>
          <div class="wallet-card-actions"><button type="button" class="wallet-action primary" @click="showRecharge = true">充值 ›</button><button type="button" class="wallet-action" @click="openWithdrawal('cash')">余额退回</button></div>
        </article>

        <article
          class="wallet-balance-card wallet-balance-card-bean wallet-dashboard-card bean-clickable"
          :class="{ disabled: !beanWallet?.settings.enabled }"
          :title="beanWallet?.settings.enabled ? '点击购买小蓝豆' : '小蓝豆购买暂未开放'"
          role="button"
          tabindex="0"
          @click="openBeanPurchase"
          @keydown.enter="openBeanPurchase"
        >
          <header>
            <span class="wallet-type-icon" aria-hidden="true"><span class="bean-icon-core"></span></span>
            <div><span class="wallet-label">小蓝豆</span><small>可在商城兑换商品</small></div><b>•••</b>
          </header>
          <strong class="wallet-balance-amount bean-balance-amount"><i class="bean-mark"></i>{{ (beanWallet?.wallet.available_beans ?? data.bean_balance ?? 0).toLocaleString() }}</strong>
          <small v-if="(beanWallet?.wallet.frozen_beans ?? data.bean_frozen ?? 0) > 0" class="wallet-frozen">冻结 {{ (beanWallet?.wallet.frozen_beans ?? data.bean_frozen).toLocaleString() }} 豆</small>
          <img class="wallet-visual bean-visual" src="/assets/wallet/xiaolan-beans-3d.png" alt="" />
        </article>

        <article class="wallet-balance-card wallet-balance-card-reward wallet-dashboard-card">
          <header>
            <span class="wallet-type-icon" aria-hidden="true"><svg viewBox="0 0 32 32"><path d="M4 13h24v15H4zM2 8h28v7H2zM14 8h4v20h-4z"/><path d="M16 8c-5 0-8-1.2-8-4 0-2 2.1-3.1 3.8-2.1C14 3.2 16 8 16 8Zm0 0c5 0 8-1.2 8-4 0-2-2.1-3.1-3.8-2.1C18 3.2 16 8 16 8Z"/></svg></span>
            <div><span class="wallet-label">奖励余额</span><small>完成任务、活动获得的奖励</small></div><b>•••</b>
          </header>
          <strong class="wallet-balance-amount">{{ formatMoney(data.reward_balance_cents) }}</strong>
          <img class="wallet-visual reward-visual" src="/assets/wallet/reward-gift-3d.png" alt="" />
          <div class="wallet-card-actions"><button type="button" class="wallet-action reward-button" @click="openWithdrawal('reward')">提现 ›</button></div>
        </article>

        <article class="wallet-balance-card wallet-balance-card-commission wallet-dashboard-card">
          <header>
            <span class="wallet-type-icon" aria-hidden="true"><svg viewBox="0 0 32 32"><path fill="none" stroke="currentColor" stroke-width="4" stroke-linecap="round" d="M7 10a11 11 0 1 1-1.5 10"/><path d="M3 5v9h9Z"/></svg></span>
            <div><span class="wallet-label">返佣余额</span><small>订单返佣、推广获得的收益</small></div><b>•••</b>
          </header>
          <strong class="wallet-balance-amount">{{ formatMoney(data.commission_balance_cents) }}</strong>
          <small v-if="data.commission_frozen_cents > 0" class="wallet-frozen">冻结中 {{ formatMoney(data.commission_frozen_cents) }}</small>
          <img class="wallet-visual commission-visual" src="/assets/wallet/commission-coins-3d.png" alt="" />
          <div class="wallet-card-actions"><button type="button" class="wallet-action commission-button" @click="openWithdrawal('commission')">提现 ›</button></div>
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

        <div v-else-if="activeTab === 'bean'" class="finance-table-wrap">
          <table v-if="beanWallet?.ledger.length" class="finance-table">
            <thead><tr><th>时间</th><th>项目</th><th>可用豆变化</th><th>冻结豆变化</th><th>变动后余额</th></tr></thead>
            <tbody>
              <tr v-for="item in pagedBeanLedger" :key="item.id">
                <td>{{ formatDate(item.created_at) }}</td>
                <td><strong>{{ item.reason || item.business_type }}</strong><small>{{ item.business_type }}</small></td>
                <td class="money-change" :class="{ income: item.available_delta > 0, expense: item.available_delta < 0 }">{{ item.available_delta > 0 ? '+' : '' }}{{ item.available_delta }}</td>
                <td>{{ item.frozen_delta > 0 ? '+' : '' }}{{ item.frozen_delta }}</td>
                <td>{{ item.available_after.toLocaleString() }} 豆<small v-if="item.frozen_after">冻结 {{ item.frozen_after.toLocaleString() }}</small></td>
              </tr>
            </tbody>
          </table>
          <div v-else class="finance-empty"><strong>暂无小蓝豆流水</strong><span>购买或使用小蓝豆后，每次余额变化都会记录在这里。</span></div>
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
            <span>还没有充值记录。微信充值到账后会自动增加现金余额，并保留充值记录。</span>
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
            <span>小蓝商城模拟支付或未来真实支付发生后，会在这里保留支付渠道和来源订单。</span>
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
        <div class="withdrawal-records-head"><h3>充值本金原路退回</h3><button type="button" @click="loadFinance">刷新记录</button></div>
        <div class="finance-table-wrap">
          <table v-if="refundWallet?.records.length" class="finance-table">
            <thead><tr><th>退回单号</th><th>提交时间</th><th>申请金额</th><th>已退回</th><th>仍冻结</th><th>已解冻</th><th>状态及原充值</th><th>操作</th></tr></thead>
            <tbody><tr v-for="item in refundWallet.records" :key="item.id"><td>{{ item.refund_no }}</td><td>{{ formatDate(item.created_at) }}</td><td>{{ formatMoney(item.amount_cents) }}</td><td>{{ formatMoney(item.refunded_cents) }}</td><td>{{ formatMoney(item.frozen_cents) }}</td><td>{{ formatMoney(item.released_cents) }}</td><td>{{ statusLabel(item.status) }}<div v-for="part in item.items" :key="part.id">{{ part.recharge_no }} · {{ statusLabel(part.status) }} · {{ part.received_account }} · {{ part.message }}</div></td><td><button v-if="item.frozen_cents > 0" type="button" :disabled="withdrawalSubmitting" @click="checkCashRefund(item.id)">查询进度</button></td></tr></tbody>
          </table>
          <div v-else class="finance-empty"><strong>{{ refundWallet ? '暂无本金退回记录' : '可退本金暂无法核验，请刷新或联系客服' }}</strong></div>
        </div>
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
        <h3>微信扫码充值</h3>
        <p>
          输入金额后点击充值，使用手机微信扫描付款二维码。确认到账后自动增加现金余额，无需财务审核。
        </p>

        <form class="recharge-request-form" @submit.prevent="submitRechargeRequest">
          <label>
            <span>充值金额（元）</span>
            <input
              v-model="rechargeAmount"
              type="text"
              inputmode="decimal"
              :disabled="rechargeSubmitting || rechargeRestoring || rechargeOrder !== null"
              placeholder="例如：500.00"
              required
            />
          </label>

          <div v-if="rechargeOrder && rechargeOrder.status !== 'paid'" class="native-recharge-payment" aria-live="polite">
            <strong class="native-recharge-amount">{{ formatMoney(rechargeOrder.requested_amount_cents) }}</strong>
            <div class="native-recharge-qr-frame">
              <img v-if="rechargeQRValid" :src="rechargeQRCode" alt="微信充值付款二维码" width="256" height="256" />
              <div v-else class="native-recharge-qr-placeholder">{{ rechargeSubmitting ? '正在生成付款二维码…' : rechargeQRCode ? '二维码已过期，请刷新' : '点击下方按钮获取付款二维码' }}</div>
            </div>
            <span v-if="rechargeQRValid" class="native-recharge-qr-hint">请使用微信扫一扫 · 剩余 {{ rechargeCountdown }}</span>
            <small>充值单：{{ rechargeOrder.recharge_no }}</small>
          </div>
          <p v-if="rechargeQRError" class="native-recharge-error" role="alert">{{ rechargeQRError }}</p>

          <p v-if="rechargeMessage" class="recharge-request-success">
            {{ rechargeMessage }}
          </p>

          <div class="dialog-actions">
            <button class="ghost-button" type="button" @click="showRecharge = false">
              关闭
            </button>
            <button v-if="rechargeOrder?.status === 'paid'" class="primary-button" type="button" @click="rechargeOrder = null; rechargeAmount = ''; rechargeMessage = ''; rechargeQRError = ''">再充一笔</button>
            <button v-else class="primary-button" type="submit" :disabled="rechargeSubmitting || rechargeRestoring">
              {{ rechargeRestoring ? '读取充值记录...' : rechargeSubmitting ? '处理中...' : rechargeQRValid ? '查询到账结果' : rechargeOrder ? '重新获取二维码' : '充值' }}
            </button>
          </div>
        </form>
      </section>
    </div>
    <div v-if="showWithdrawal" class="modal-backdrop" @click.self="showWithdrawal = false">
      <section class="dialog-card withdrawal-dialog">
        <div class="dialog-icon">提</div>
        <h3>{{ withdrawalType === 'cash' ? '充值余额原路退回' : withdrawalLabel(withdrawalType) + '提现' }}</h3>
        <template v-if="withdrawalType === 'cash'"><p>仅退回未消费的微信充值本金，现金消费优先扣充值本金；赠送、奖励与返佣不能原路退回。过期交易或来源不明的款项请联系客服。</p><p>将退回原付款人的微信支付账户或银行卡，不能指定新账户；他人代付将退给代付人。处理中金额冻结，以微信实际到账结果为准。</p></template>
        <div class="withdrawal-available-line">
          <span>{{ withdrawalType === 'cash' ? '当前可退本金' : '当前可提现' }}</span>
          <strong>{{ formatMoney(withdrawalAvailable(withdrawalType)) }}</strong>
        </div>
        <form class="recharge-request-form" @submit.prevent="submitWithdrawal">
          <label>
            <span>{{ withdrawalType === 'cash' ? '退回金额（元）' : '提现金额（元）' }}</span>
            <input
              v-model="withdrawalAmount"
              type="text"
              inputmode="decimal"
              :disabled="withdrawalSubmitting || (withdrawalType === 'cash' && !!acceptedCashRefund)"
              placeholder="请输入金额，最多两位小数"
              required
            />
          </label>
          <p v-if="withdrawalMessage" class="recharge-request-success">{{ withdrawalMessage }}</p>
          <p v-if="withdrawalType === 'cash' && cashRefundWarning" class="wallet-sync-warning" role="status">{{ cashRefundWarning }}</p>
          <div class="dialog-actions">
            <button class="ghost-button" type="button" @click="showWithdrawal = false">{{ withdrawalType === 'cash' && acceptedCashRefund ? '关闭' : '取消' }}</button>
            <button v-if="withdrawalType === 'cash' && acceptedCashRefund" class="primary-button" type="button" :disabled="withdrawalSubmitting" @click="refundNeedsSync(acceptedCashRefund) ? checkCashRefund(acceptedCashRefund.id) : showWithdrawal = false">{{ withdrawalSubmitting ? '查询中...' : refundNeedsSync(acceptedCashRefund) ? '查询退回进度' : '完成' }}</button>
            <button v-else class="primary-button" type="submit" :disabled="withdrawalSubmitting || (withdrawalType === 'cash' && !refundWallet)">
              {{ withdrawalSubmitting ? '提交中...' : withdrawalType === 'cash' ? '确认原路退回' : '提交提现申请' }}
            </button>
          </div>
        </form>
      </section>
    </div>

    <div v-if="showBeanPurchase && beanWallet" class="modal-backdrop" @click.self="showBeanPurchase = false">
      <section class="dialog-card bean-purchase-dialog">
        <div class="dialog-icon bean-dialog-icon">豆</div>
        <h3>购买小蓝豆</h3>
        <p>小蓝豆用于录音转文字、非直播 AI 和付费运维协助等服务。购买金额从现金余额扣除并生成独立豆账流水。</p>
        <div class="bean-rate-line"><span>当前兑换比例</span><strong>1 元 = {{ beanWallet.settings.purchase_beans_per_yuan }} 小蓝豆</strong></div>
        <form class="recharge-request-form" @submit.prevent="submitBeanPurchase">
          <label><span>购买金额（元）</span><input v-model="beanPurchaseYuan" type="number" :min="beanWallet.settings.minimum_purchase_cents / 100" step="0.01" placeholder="请输入购买金额" required /></label>
          <div class="bean-purchase-preview"><span>预计到账</span><strong>{{ beanPurchasePreview.toLocaleString() }} 小蓝豆</strong></div>
          <p v-if="beanPurchaseMessage" class="recharge-request-success">{{ beanPurchaseMessage }}</p>
          <div class="dialog-actions"><button class="ghost-button" type="button" @click="showBeanPurchase = false">关闭</button><button class="primary-button" type="submit" :disabled="beanPurchaseSubmitting">{{ beanPurchaseSubmitting ? '购买中...' : '确认购买' }}</button></div>
        </form>
      </section>
    </div>

  </div>
</template>

<style scoped>
.wallet-sync-warning { margin: 12px 0; padding: 10px 14px; border-radius: 12px; background: #eef3ff; color: #5d709b; font-size: 13px; line-height: 1.6; }
.finance-page .wallet-balance-card-cash { min-height: 270px; }
.finance-page .wallet-balance-card-cash > .wallet-balance-amount { top: 46%; }
.wallet-refund-summary { position: absolute; z-index: 3; top: calc(46% + 27px); left: 27px; right: 27px; display: flex; flex-wrap: wrap; gap: 2px 10px; margin: 0; color: #75859e; font-size: 11px; line-height: 1.5; }
.native-recharge-payment { display: grid; justify-items: center; gap: 10px; padding: 16px; border-radius: 20px; background: linear-gradient(145deg, #f4f7ff, #f1fafa); }
.native-recharge-amount { color: #1c2335; font-size: 30px; font-weight: 750; }
.native-recharge-qr-frame { width: 256px; height: 256px; max-width: 100%; border: 1px solid #e5eaf3; border-radius: 12px; background: white; overflow: hidden; }
.native-recharge-qr-frame img { display: block; width: 100%; height: 100%; object-fit: contain; }
.native-recharge-qr-placeholder { display: grid; place-items: center; height: 100%; padding: 20px; box-sizing: border-box; color: #78849a; text-align: center; }
.native-recharge-qr-hint { color: #20906d; font-weight: 600; font-size: 14px; }
.native-recharge-payment small { color: #78849a; font-size: 12px; overflow-wrap: anywhere; }
.native-recharge-error { color: #c13f4f; font-size: 14px; line-height: 1.6; }
.recharge-dialog { max-height: calc(100dvh - 48px); overflow-y: auto; }
</style>
