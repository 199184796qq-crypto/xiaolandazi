<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  createCommercialBeanRule,
  createStaffBeanConversion,
  getCommercialBeans,
  getStaffBeanWallet,
  updateCommercialBeanRule,
  updateCommercialBeanSettings,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type {
  BeanCommerceSettingsInput,
  BeanCommercialDashboard,
  BeanPricingRule,
  BeanPricingRuleInput,
  BeanWalletDashboard,
} from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'

const dashboard = ref<BeanCommercialDashboard | null>(null)
const loading = ref(true)
const savingSettings = ref(false)
const savingRule = ref(false)
const error = useFeedbackErrorRef()
const message = ref('')
const editingRuleID = ref(0)
const staffWallet = ref<BeanWalletDashboard | null>(null)
const conversionAmount = ref('')
const converting = ref(false)

const settings = reactive<BeanCommerceSettingsInput>({
  purchase_beans_per_yuan: 100,
  minimum_purchase_cents: 100,
  staff_cash_fen_per_100_beans: 50,
  minimum_staff_conversion_beans: 1000,
  enabled: false,
  version: 0,
})

const emptyRule = (): BeanPricingRuleInput => ({
  action_code: '',
  action_name: '',
  category: 'ai',
  description: '',
  charge_mode: 'fixed',
  beans_per_unit: 1,
  unit_size: 1,
  minimum_charge_beans: 1,
  maximum_charge_beans: 0,
  staff_reward_bps: 0,
  enabled: false,
  version: 0,
})
const ruleForm = reactive<BeanPricingRuleInput>(emptyRule())

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  return bootstrap?.actor.role === 'platform_admin'
    || Boolean(bootstrap?.staff_access?.is_super_admin)
    || Boolean(bootstrap?.staff_access?.permissions.includes('commercial.beans.manage'))
})

function assignSettings(value: BeanCommercialDashboard['settings']) {
  settings.purchase_beans_per_yuan = value.purchase_beans_per_yuan
  settings.minimum_purchase_cents = value.minimum_purchase_cents
  settings.staff_cash_fen_per_100_beans = value.staff_cash_fen_per_100_beans
  settings.minimum_staff_conversion_beans = value.minimum_staff_conversion_beans
  settings.enabled = value.enabled
  settings.version = value.version
}

async function loadDashboard() {
  loading.value = true
  error.value = ''
  try {
    const [commercial, wallet] = await Promise.all([getCommercialBeans(), getStaffBeanWallet()])
    dashboard.value = commercial
    staffWallet.value = wallet
    assignSettings(dashboard.value.settings)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取小蓝豆运营配置失败'
  } finally {
    loading.value = false
  }
}

async function submitConversion() {
  const amount = Math.floor(Number(conversionAmount.value))
  if (!Number.isFinite(amount) || amount <= 0) {
    error.value = '请输入正确的兑付豆数'
    return
  }
  converting.value = true
  error.value = ''
  message.value = ''
  try {
    await createStaffBeanConversion(amount)
    message.value = '兑付申请已提交，冻结豆数将在财务完成打款后扣除。'
    conversionAmount.value = ''
    staffWallet.value = await getStaffBeanWallet()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '提交兑付申请失败'
  } finally {
    converting.value = false
  }
}

async function saveSettings() {
  savingSettings.value = true
  error.value = ''
  message.value = ''
  try {
    const value = await updateCommercialBeanSettings({ ...settings })
    assignSettings(value)
    if (dashboard.value) dashboard.value.settings = value
    message.value = '兑换比例与兑付规则已发布。'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存小蓝豆配置失败'
  } finally {
    savingSettings.value = false
  }
}

function resetRuleForm() {
  editingRuleID.value = 0
  Object.assign(ruleForm, emptyRule())
}

function editRule(rule: BeanPricingRule) {
  editingRuleID.value = rule.id
  Object.assign(ruleForm, {
    action_code: rule.action_code,
    action_name: rule.action_name,
    category: rule.category,
    description: rule.description,
    charge_mode: rule.charge_mode,
    beans_per_unit: rule.beans_per_unit,
    unit_size: rule.unit_size,
    minimum_charge_beans: rule.minimum_charge_beans,
    maximum_charge_beans: rule.maximum_charge_beans,
    staff_reward_bps: rule.staff_reward_bps,
    enabled: rule.enabled,
    version: rule.version,
  })
  document.getElementById('bean-rule-editor')?.scrollIntoView({ behavior: 'smooth', block: 'center' })
}

async function saveRule() {
  savingRule.value = true
  error.value = ''
  message.value = ''
  try {
    if (editingRuleID.value) {
      await updateCommercialBeanRule(editingRuleID.value, { ...ruleForm })
      message.value = '行为扣豆规则已更新。'
    } else {
      await createCommercialBeanRule({ ...ruleForm })
      message.value = '行为扣豆规则已创建。'
    }
    resetRuleForm()
    await loadDashboard()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存行为扣豆规则失败'
  } finally {
    savingRule.value = false
  }
}

function money(cents: number) {
  return '¥' + (cents / 100).toFixed(2)
}

function percent(bps: number) {
  return (bps / 100).toFixed(bps % 100 ? 2 : 0) + '%'
}

onMounted(loadDashboard)
</script>

<template>
  <div class="bean-admin-page">
    <ModulePageNav context="activityMarketing" active-title="小蓝豆运营" />

    <section class="bean-admin-hero">
      <div>
        <p class="section-kicker">XIAOLAN BEAN OPERATIONS</p>
        <h2>小蓝豆运营</h2>
        <p>统一配置现金兑换比例和非直播 AI、录音转写、运维协助等行为的扣豆规则。</p>
      </div>
      <span :class="['bean-switch-state', dashboard?.settings.enabled ? 'online' : 'offline']">
        {{ dashboard?.settings.enabled ? '计费已启用' : '计费未启用' }}
      </span>
    </section>

    <div v-if="loading" class="panel-loading">正在读取小蓝豆配置...</div>
    <p v-else-if="error && !dashboard" class="global-error">{{ error }}</p>

    <template v-if="dashboard">
      <p v-if="error" class="settings-error">{{ error }}</p>
      <p v-if="message" class="bean-success">{{ message }}</p>

      <section class="bean-summary-grid">
        <article><span>客户可用豆</span><strong>{{ dashboard.summary.customer_available_beans.toLocaleString() }}</strong></article>
        <article><span>累计购豆</span><strong>{{ dashboard.summary.purchased_beans.toLocaleString() }}</strong><small>{{ money(dashboard.summary.purchase_cash_cents) }}</small></article>
        <article><span>累计扣豆</span><strong>{{ dashboard.summary.charged_beans.toLocaleString() }}</strong></article>
        <article><span>待兑付金额</span><strong>{{ money(dashboard.summary.pending_conversion_cents) }}</strong><small>财务处理</small></article>
      </section>

      <section v-if="staffWallet" class="staff-bean-card">
        <div class="staff-bean-balance">
          <span class="bean-orb">豆</span>
          <div><small>我的服务收益</small><strong>{{ staffWallet.wallet.available_beans.toLocaleString() }} 小蓝豆</strong><em>冻结 {{ staffWallet.wallet.frozen_beans.toLocaleString() }} 豆</em></div>
        </div>
        <form @submit.prevent="submitConversion">
          <label><span>申请兑付豆数</span><input v-model="conversionAmount" type="number" :min="staffWallet.settings.minimum_staff_conversion_beans" :max="staffWallet.wallet.available_beans" :placeholder="`最低 ${staffWallet.settings.minimum_staff_conversion_beans} 豆`" required /></label>
          <div><strong>100 豆 = {{ money(staffWallet.settings.staff_cash_fen_per_100_beans) }}</strong><small>提交后由财务审核并确认打款</small></div>
          <button class="primary-button" type="submit" :disabled="converting || !staffWallet.settings.enabled">{{ converting ? '提交中...' : '申请兑付' }}</button>
        </form>
      </section>

      <section class="bean-config-card">
        <header>
          <div><span class="section-kicker">EXCHANGE SETTINGS</span><h3>兑换与兑付配置</h3></div>
          <label class="bean-toggle"><input v-model="settings.enabled" type="checkbox" :disabled="!canManage" /><span></span>{{ settings.enabled ? '启用' : '停用' }}</label>
        </header>
        <form class="bean-config-form" @submit.prevent="saveSettings">
          <label><span>1 元购买</span><div><input v-model.number="settings.purchase_beans_per_yuan" type="number" min="1" required /><em>小蓝豆</em></div></label>
          <label><span>最低购买金额</span><div><input v-model.number="settings.minimum_purchase_cents" type="number" min="1" required /><em>分</em></div></label>
          <label><span>员工每 100 豆兑付</span><div><input v-model.number="settings.staff_cash_fen_per_100_beans" type="number" min="1" required /><em>分</em></div></label>
          <label><span>最低兑付豆数</span><div><input v-model.number="settings.minimum_staff_conversion_beans" type="number" min="1" required /><em>豆</em></div></label>
          <button v-if="canManage" class="primary-button" type="submit" :disabled="savingSettings">{{ savingSettings ? '发布中...' : '发布兑换配置' }}</button>
        </form>
        <p class="bean-config-note">营销端负责定价，财务端负责员工豆子兑付审核和打款确认；修改后新交易按新比例执行，历史订单保留快照。</p>
      </section>

      <section class="bean-rules-card">
        <header><div><span class="section-kicker">USAGE PRICING</span><h3>行为扣豆规则</h3></div><span>{{ dashboard.rules.length }} 项</span></header>
        <div class="bean-rule-table-wrap">
          <table class="bean-rule-table">
            <thead><tr><th>行为</th><th>计价方式</th><th>扣豆</th><th>员工分成</th><th>状态</th><th></th></tr></thead>
            <tbody>
              <tr v-for="rule in dashboard.rules" :key="rule.id">
                <td><strong>{{ rule.action_name }}</strong><small>{{ rule.action_code }} · {{ rule.description }}</small></td>
                <td>{{ rule.charge_mode === 'fixed' ? '按次' : `每 ${rule.unit_size} 单位` }}</td>
                <td><b>{{ rule.beans_per_unit }}</b> 豆</td>
                <td>{{ percent(rule.staff_reward_bps) }}</td>
                <td><span :class="['rule-status', rule.enabled ? 'online' : 'offline']">{{ rule.enabled ? '已启用' : '未启用' }}</span></td>
                <td><button v-if="canManage" class="ghost-button" type="button" @click="editRule(rule)">编辑</button></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="canManage" id="bean-rule-editor" class="bean-config-card rule-editor">
        <header><div><span class="section-kicker">RULE EDITOR</span><h3>{{ editingRuleID ? '编辑行为规则' : '新增行为规则' }}</h3></div><button v-if="editingRuleID" class="ghost-button" type="button" @click="resetRuleForm">取消编辑</button></header>
        <form class="bean-rule-form" @submit.prevent="saveRule">
          <label><span>行为编码</span><input v-model.trim="ruleForm.action_code" placeholder="例如 audio.transcription" :disabled="Boolean(editingRuleID)" required /></label>
          <label><span>行为名称</span><input v-model.trim="ruleForm.action_name" placeholder="录音转文字" required /></label>
          <label><span>分类</span><input v-model.trim="ruleForm.category" placeholder="ai / support" required /></label>
          <label><span>计价方式</span><select v-model="ruleForm.charge_mode"><option value="fixed">按次</option><option value="per_unit">按用量</option></select></label>
          <label><span>每单位扣豆</span><input v-model.number="ruleForm.beans_per_unit" type="number" min="1" required /></label>
          <label><span>单位大小</span><input v-model.number="ruleForm.unit_size" type="number" min="1" required /></label>
          <label><span>最低扣豆</span><input v-model.number="ruleForm.minimum_charge_beans" type="number" min="1" /></label>
          <label><span>最高扣豆（0 不限）</span><input v-model.number="ruleForm.maximum_charge_beans" type="number" min="0" /></label>
          <label><span>服务员工分成（万分比）</span><input v-model.number="ruleForm.staff_reward_bps" type="number" min="0" max="10000" /></label>
          <label class="wide"><span>说明</span><input v-model.trim="ruleForm.description" placeholder="说明计费单位和适用场景" /></label>
          <label class="bean-checkbox"><input v-model="ruleForm.enabled" type="checkbox" /> 发布后立即启用该规则</label>
          <button class="primary-button" type="submit" :disabled="savingRule">{{ savingRule ? '保存中...' : (editingRuleID ? '保存规则' : '创建规则') }}</button>
        </form>
      </section>
    </template>
  </div>
</template>

<style scoped>
.bean-admin-page{display:grid;gap:20px}.bean-admin-hero,.bean-config-card,.bean-rules-card,.staff-bean-card{border:1px solid #dce6f5;border-radius:26px;background:rgba(255,255,255,.92);box-shadow:0 18px 50px rgba(72,103,161,.09)}.bean-admin-hero{display:flex;align-items:center;justify-content:space-between;padding:30px 34px;background:linear-gradient(135deg,#fff 30%,#edf5ff)}.bean-admin-hero h2,.bean-config-card h3,.bean-rules-card h3{margin:3px 0 8px;color:#14213d}.bean-admin-hero p:last-child{margin:0;color:#71809c}.bean-switch-state,.rule-status{display:inline-flex;align-items:center;border-radius:999px;padding:7px 12px;font-size:13px}.online{background:#e8fbf3;color:#07966f}.offline{background:#f2f4f8;color:#7a8497}.bean-summary-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:16px}.bean-summary-grid article{display:grid;gap:7px;padding:22px;border:1px solid #e1e9f6;border-radius:21px;background:linear-gradient(145deg,#fff,#f6f9ff)}.bean-summary-grid span,.bean-summary-grid small{color:#8290a9}.bean-summary-grid strong{font-size:28px;color:#13264c}.staff-bean-card{display:flex;align-items:center;justify-content:space-between;gap:26px;padding:24px 27px;background:linear-gradient(135deg,#f7faff,#eef3ff)}.staff-bean-balance{display:flex;align-items:center;gap:15px}.bean-orb{display:grid;place-items:center;width:52px;height:52px;border-radius:18px;background:linear-gradient(145deg,#78a5ff,#315ce7);color:#fff;font-size:22px;box-shadow:0 12px 24px #4071e94d}.staff-bean-balance div{display:grid;gap:3px}.staff-bean-balance small,.staff-bean-balance em,.staff-bean-card form small{color:#8190aa;font-style:normal}.staff-bean-balance strong{font-size:22px;color:#13284f}.staff-bean-card form{display:flex;align-items:end;gap:14px}.staff-bean-card form label{display:grid;gap:7px;color:#667692;font-size:13px}.staff-bean-card form input{border:1px solid #d5e0f1;border-radius:12px;padding:11px;background:#fff}.staff-bean-card form>div{display:grid;gap:3px}.bean-config-card,.bean-rules-card{padding:26px}.bean-config-card>header,.bean-rules-card>header{display:flex;align-items:center;justify-content:space-between;margin-bottom:20px}.bean-config-form{display:grid;grid-template-columns:repeat(4,minmax(0,1fr)) auto;gap:14px;align-items:end}.bean-config-form label,.bean-rule-form label{display:grid;gap:8px;color:#596a89;font-size:13px}.bean-config-form label>div{display:flex;align-items:center;gap:8px}.bean-config-form input,.bean-rule-form input,.bean-rule-form select{width:100%;box-sizing:border-box;border:1px solid #d8e2f2;border-radius:12px;padding:12px;background:#f9fbff;color:#172641}.bean-config-form em{font-style:normal;white-space:nowrap}.bean-config-note{margin:17px 0 0;color:#7d89a0;font-size:13px}.bean-toggle{display:flex;align-items:center;gap:8px;color:#5e6c86}.bean-toggle input{width:18px;height:18px}.bean-success{margin:0;border-radius:14px;padding:12px 16px;background:#eafbf4;color:#087b5c}.bean-rule-table-wrap{overflow:auto}.bean-rule-table{width:100%;border-collapse:collapse}.bean-rule-table th,.bean-rule-table td{padding:15px 12px;border-bottom:1px solid #edf1f7;text-align:left}.bean-rule-table th{color:#8a96ab;font-size:12px}.bean-rule-table td{color:#4e5d77}.bean-rule-table td:first-child{min-width:260px}.bean-rule-table td strong,.bean-rule-table td small{display:block}.bean-rule-table td small{margin-top:5px;color:#8a96aa}.bean-rule-table b{color:#315eea}.bean-rule-form{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:15px}.bean-rule-form .wide{grid-column:span 2}.bean-checkbox{align-self:center;display:flex!important;grid-template-columns:auto 1fr!important;align-items:center}.bean-checkbox input{width:auto}.bean-rule-form button{align-self:end}.rule-editor{scroll-margin-top:25px}@media(max-width:1100px){.bean-summary-grid,.bean-config-form{grid-template-columns:repeat(2,minmax(0,1fr))}.bean-rule-form{grid-template-columns:repeat(2,minmax(0,1fr))}.staff-bean-card,.staff-bean-card form{align-items:flex-start;flex-direction:column}}@media(max-width:720px){.bean-admin-hero{align-items:flex-start;gap:20px}.bean-summary-grid,.bean-config-form,.bean-rule-form{grid-template-columns:1fr}.bean-rule-form .wide{grid-column:auto}.staff-bean-card form{width:100%}}
</style>
