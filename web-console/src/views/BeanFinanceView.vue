<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getFinanceBeans, transitionBeanConversion } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type { BeanConversionRequest, BeanFinanceDashboard } from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'

const data = ref<BeanFinanceDashboard | null>(null)
const loading = ref(true)
const processingID = ref(0)
const statusFilter = ref('all')
const error = useFeedbackErrorRef()
const message = ref('')

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  return bootstrap?.actor.role === 'platform_admin'
    || Boolean(bootstrap?.staff_access?.is_super_admin)
    || Boolean(bootstrap?.staff_access?.permissions.includes('finance.beans.manage'))
})
const conversions = computed(() => {
  const items = data.value?.conversions || []
  return statusFilter.value === 'all' ? items : items.filter((item) => item.status === statusFilter.value)
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    data.value = await getFinanceBeans()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取小蓝豆财务数据失败'
  } finally {
    loading.value = false
  }
}

async function transition(item: BeanConversionRequest, action: 'approve' | 'reject' | 'pay') {
  let reason = ''
  if (action === 'reject') {
    reason = window.prompt('请输入驳回原因，驳回后冻结的小蓝豆将退回员工钱包。')?.trim() || ''
    if (!reason) return
  }
  const confirmText = action === 'approve'
    ? '确认审核通过这笔兑付申请？'
    : action === 'pay'
      ? '请先确认线下款项已经支付。确认后将完成兑付并扣除冻结豆。'
      : '确认驳回这笔兑付申请？'
  if (!window.confirm(confirmText)) return
  processingID.value = item.id
  error.value = ''
  message.value = ''
  try {
    await transitionBeanConversion(item.id, action, reason)
    message.value = action === 'approve' ? '兑付申请已审核通过。' : action === 'pay' ? '已确认打款，兑付完成。' : '申请已驳回，小蓝豆已退回。'
    await load()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '处理兑付申请失败'
  } finally {
    processingID.value = 0
  }
}

function money(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })
}

function date(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN') : '—'
}

function statusLabel(status: string) {
  return ({ reviewing: '待审核', approved: '待打款', rejected: '已驳回', paid: '已兑付' } as Record<string, string>)[status] || status
}

onMounted(load)
</script>

<template>
  <div class="bean-finance-page">
    <ModulePageNav context="finance" active-title="小蓝豆兑付" />
    <section class="bean-finance-hero">
      <div><p class="section-kicker">BEAN FINANCE</p><h2>小蓝豆财务与兑付</h2><p>核对购豆资金、豆子流通和员工服务收益，完成审核与打款闭环。</p></div>
      <button class="ghost-button" type="button" @click="load">刷新</button>
    </section>

    <div v-if="loading" class="panel-loading">正在读取小蓝豆财务数据...</div>
    <p v-else-if="error && !data" class="global-error">{{ error }}</p>

    <template v-if="data">
      <p v-if="error" class="settings-error">{{ error }}</p>
      <p v-if="message" class="finance-success">{{ message }}</p>
      <section class="bean-finance-summary">
        <article class="blue"><span>购豆现金收入</span><strong>{{ money(data.summary.purchase_cash_cents) }}</strong><small>累计售出 {{ data.summary.purchased_beans.toLocaleString() }} 豆</small></article>
        <article class="purple"><span>客户流通余额</span><strong>{{ data.summary.customer_available_beans.toLocaleString() }}</strong><small>冻结 {{ data.summary.customer_frozen_beans.toLocaleString() }} 豆</small></article>
        <article class="orange"><span>员工服务收益</span><strong>{{ data.summary.staff_available_beans.toLocaleString() }}</strong><small>冻结 {{ data.summary.staff_frozen_beans.toLocaleString() }} 豆</small></article>
        <article class="green"><span>待兑付金额</span><strong>{{ money(data.summary.pending_conversion_cents) }}</strong><small>财务负债待处理</small></article>
      </section>

      <section class="bean-finance-card">
        <header>
          <div><p class="section-kicker">CONVERSION REQUESTS</p><h3>员工兑付申请</h3></div>
          <select v-model="statusFilter"><option value="all">全部状态</option><option value="reviewing">待审核</option><option value="approved">待打款</option><option value="paid">已兑付</option><option value="rejected">已驳回</option></select>
        </header>
        <div class="bean-finance-table-wrap">
          <table v-if="conversions.length" class="bean-finance-table">
            <thead><tr><th>申请时间</th><th>员工</th><th>兑付单号</th><th>豆数</th><th>兑付金额</th><th>状态</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="item in conversions" :key="item.id">
                <td>{{ date(item.requested_at) }}</td><td><strong>{{ item.user_name || `员工 #${item.user_id}` }}</strong></td><td>{{ item.conversion_no }}</td><td>{{ item.bean_amount.toLocaleString() }}</td><td><strong>{{ money(item.cash_amount_cents) }}</strong></td>
                <td><span :class="['bean-finance-status', item.status]">{{ statusLabel(item.status) }}</span><small v-if="item.reject_reason">{{ item.reject_reason }}</small></td>
                <td><div v-if="canManage" class="bean-finance-actions"><button v-if="item.status === 'reviewing'" type="button" @click="transition(item, 'approve')" :disabled="processingID === item.id">通过</button><button v-if="item.status === 'reviewing'" class="danger" type="button" @click="transition(item, 'reject')" :disabled="processingID === item.id">驳回</button><button v-if="item.status === 'approved'" class="pay" type="button" @click="transition(item, 'pay')" :disabled="processingID === item.id">确认打款</button></div><span v-else>只读</span></td>
              </tr>
            </tbody>
          </table>
          <div v-else class="bean-empty">当前筛选条件下暂无兑付申请。</div>
        </div>
      </section>

      <section class="bean-finance-card">
        <header><div><p class="section-kicker">RECENT LEDGER</p><h3>最近豆账流水</h3></div><span>不可变流水</span></header>
        <div class="bean-finance-table-wrap">
          <table v-if="data.recent_ledger.length" class="bean-finance-table">
            <thead><tr><th>发生时间</th><th>业务</th><th>外部编号</th><th>可用变化</th><th>冻结变化</th><th>变化后余额</th></tr></thead>
            <tbody><tr v-for="item in data.recent_ledger" :key="item.id"><td>{{ date(item.created_at) }}</td><td><strong>{{ item.reason || item.business_type }}</strong><small>{{ item.business_type }}</small></td><td>{{ item.external_id }}</td><td :class="item.available_delta >= 0 ? 'income' : 'expense'">{{ item.available_delta > 0 ? '+' : '' }}{{ item.available_delta }}</td><td>{{ item.frozen_delta > 0 ? '+' : '' }}{{ item.frozen_delta }}</td><td>{{ item.available_after.toLocaleString() }} / 冻结 {{ item.frozen_after.toLocaleString() }}</td></tr></tbody>
          </table>
          <div v-else class="bean-empty">暂无小蓝豆流水。</div>
        </div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.bean-finance-page{display:grid;gap:20px}.bean-finance-hero,.bean-finance-card{border:1px solid #dbe5f2;border-radius:26px;background:#fff;box-shadow:0 16px 50px rgba(49,79,137,.08)}.bean-finance-hero{display:flex;align-items:center;justify-content:space-between;padding:30px 34px;background:linear-gradient(135deg,#fff,#f0f6ff)}.bean-finance-hero h2,.bean-finance-card h3{margin:4px 0 8px;color:#14233f}.bean-finance-hero p:last-child{margin:0;color:#7e8ba2}.bean-finance-summary{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:15px}.bean-finance-summary article{display:grid;gap:7px;padding:23px;border:1px solid #e0e7f2;border-radius:22px}.bean-finance-summary article.blue{background:linear-gradient(145deg,#f7fbff,#eaf3ff)}.bean-finance-summary article.purple{background:linear-gradient(145deg,#fbfaff,#efefff)}.bean-finance-summary article.orange{background:linear-gradient(145deg,#fffaf5,#fff0e3)}.bean-finance-summary article.green{background:linear-gradient(145deg,#f8fffc,#e9faf3)}.bean-finance-summary span,.bean-finance-summary small{color:#7787a0}.bean-finance-summary strong{font-size:28px;color:#10284f}.bean-finance-card{padding:26px}.bean-finance-card>header{display:flex;align-items:center;justify-content:space-between;margin-bottom:18px}.bean-finance-card select{border:1px solid #d6e1f0;border-radius:12px;padding:10px 13px;background:#f8faff;color:#35445f}.bean-finance-table-wrap{overflow:auto}.bean-finance-table{width:100%;border-collapse:collapse}.bean-finance-table th,.bean-finance-table td{padding:14px 11px;border-bottom:1px solid #edf1f6;text-align:left;white-space:nowrap}.bean-finance-table th{font-size:12px;color:#8995aa}.bean-finance-table td{font-size:13px;color:#516078}.bean-finance-table td small{display:block;margin-top:4px;color:#929caf}.bean-finance-status{display:inline-flex;padding:6px 10px;border-radius:999px;background:#f0f2f6}.bean-finance-status.pending{background:#fff3df;color:#bf6b00}.bean-finance-status.approved{background:#e8f2ff;color:#2467d4}.bean-finance-status.paid{background:#e6faef;color:#078454}.bean-finance-status.rejected{background:#ffeded;color:#bd4141}.bean-finance-actions{display:flex;gap:7px}.bean-finance-actions button{border:0;border-radius:9px;padding:7px 11px;background:#e7efff;color:#275ccb;cursor:pointer}.bean-finance-actions .danger{background:#fff0f0;color:#bf4545}.bean-finance-actions .pay{background:#e6faef;color:#087c53}.income{color:#099468!important}.expense{color:#d55353!important}.finance-success{margin:0;padding:12px 16px;border-radius:14px;background:#e9fbf3;color:#087b59}.bean-empty{padding:50px;text-align:center;color:#8895aa}@media(max-width:1050px){.bean-finance-summary{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:650px){.bean-finance-summary{grid-template-columns:1fr}.bean-finance-hero{align-items:flex-start}}
.bean-finance-status.reviewing{background:#fff3df;color:#bf6b00}
</style>
