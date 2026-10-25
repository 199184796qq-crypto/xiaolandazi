<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { request } from '../api'
import { userFacingErrorText } from '../errorDiagnostic'
import {
  addLiveCostTier,
  liveCostDraftToSettings,
  liveCostSettingsToDraft,
  newLiveCostTier,
  type LiveCostDraft,
  type LiveCostSettings,
} from '../liveCostSettings'

const endpoint = '/api/v1/system/live-cost-settings'
const draft = ref<LiveCostDraft | null>(null)
const loading = ref(false), saving = ref(false), error = ref(''), notice = ref(''), needsReload = ref(false)
const busy = computed(() => loading.value || saving.value)
const hanPrice = computed(() => {
  const value = Number(draft.value?.tts)
  return Number.isFinite(value) && value >= 0 ? (value * 2).toFixed(6).replace(/\.?0+$/, '') || '0' : '—'
})

async function load() {
  if (busy.value) return
  loading.value = true; error.value = ''; notice.value = ''
  try {
    const response = await request<{ config: LiveCostSettings }>(endpoint, { cache: 'no-store', suppressErrorDialog: true })
    draft.value = liveCostSettingsToDraft(response.config)
    needsReload.value = false
  } catch (value) {
    needsReload.value = true
    error.value = userFacingErrorText(value, '读取直播成本设置失败，请稍后重新读取')
  } finally { loading.value = false }
}

async function save() {
  if (busy.value || needsReload.value || !draft.value) return
  error.value = ''; notice.value = ''
  let config: LiveCostSettings
  try { config = liveCostDraftToSettings(draft.value) }
  catch (value) { error.value = userFacingErrorText(value, '请检查成本设置中的金额和档位'); return }
  saving.value = true
  try {
    const response = await request<{ config: LiveCostSettings }>(endpoint, { method: 'PUT', body: JSON.stringify(config), suppressErrorDialog: true })
    draft.value = liveCostSettingsToDraft(response.config)
    notice.value = '已保存。后续请求与每轮规划采用新设置；已发送的请求沿用原价，历史费用不回算，成品缓存继续保留。'
  } catch (value) {
    error.value = userFacingErrorText(value, '保存直播成本设置失败，请稍后重试')
    if (/配置已(?:变化|更新|修改)|版本.*(?:冲突|不一致|过期)/.test(error.value)) {
      needsReload.value = true
      error.value = '配置已变化，请重新读取后再保存。当前修改尚未保存。'
    } else if (/结果待核对|重新(?:读取|加载|刷新)|刷新.*再保存/.test(error.value)) {
      needsReload.value = true
    }
  } finally { saving.value = false }
}

function addModel() {
  if (!draft.value || draft.value.models.length >= 32) return
  draft.value.models.push({ provider: '*', model: '', tiers: [newLiveCostTier()] })
}

onMounted(load)
</script>

<template>
  <section id="live-cost-settings" class="live-cost-settings" aria-labelledby="live-cost-title">
    <header class="cost-header">
      <div><span class="cost-kicker">成本与更新</span><h2 id="live-cost-title">直播成本设置</h2><p>统一管理声音与文字模型单价，以及直播间每小时的更新预算。</p></div>
      <button type="button" class="secondary" :disabled="busy" @click="load">{{ loading ? '读取中…' : '重新读取成本设置' }}</button>
    </header>
    <p v-if="error" class="cost-alert error" role="alert">{{ error }}</p>
    <p v-if="notice" class="cost-alert success" role="status">{{ notice }}</p>
    <p v-if="!draft && loading" class="cost-loading">正在读取直播成本设置…</p>
    <form v-if="draft" @submit.prevent="save">
      <p v-if="draft.revision === 0" class="cost-alert explanation">首次保存将统一启用本页全部价格，原环境中的单价将不再使用。只调整预算时，也请先核对本页单价。</p>
      <fieldset :disabled="busy || needsReload">
        <section class="cost-block">
          <div class="block-heading"><div><h3>每小时预算</h3><p>三项预算共用直播间的滚动一小时费用记录。</p></div><span class="unit-pill">人民币 · 元／小时</span></div>
          <div class="budget-grid">
            <label>主线更新目标<input v-model="draft.mainline" inputmode="decimal" autocomplete="off" aria-label="主线更新小时目标（元）" /><small>用于安排可选文案更新。</small></label>
            <label>全部可选工作软线<input v-model="draft.optional" inputmode="decimal" autocomplete="off" aria-label="全部可选工作小时软线（元）" /><small>覆盖主线及其他可选工作。</small></label>
            <label>直播间总目标<input v-model="draft.total" inputmode="decimal" autocomplete="off" aria-label="直播间小时总目标（元）" /><small>包含必要供料与全部可选工作。</small></label>
          </div>
          <p class="explanation">主线目标 ≤ 可选软线 ≤ 总目标。可选软线设为零时暂停可选更新；必要供料仍可超过总目标，保障业务继续运行。</p>
        </section>

        <section class="cost-block">
          <div class="block-heading"><div><h3>声音合成单价</h3><p>按实际发送文本核算，汉字计两个字符，其他字符计一个。</p></div></div>
          <div class="tts-grid"><label>每万计费字符（元）<input v-model="draft.tts" inputmode="decimal" autocomplete="off" aria-label="每万计费字符声音单价（元）" /></label><div class="price-equivalent"><strong>约 {{ hanPrice }} 元／万汉字</strong><span>不含标点等其他字符，便于比较计价口径。</span></div></div>
        </section>

        <section class="cost-block">
          <div class="block-heading"><div><h3>默认模型价格</h3><p>未命中特定模型规则时使用。单价单位均为元／百万 Token。</p></div><button type="button" class="secondary small" :disabled="draft.fallback.length >= 16" @click="addLiveCostTier(draft.fallback)">添加默认档位</button></div>
          <div class="tier-list">
            <div v-for="(tier, index) in draft.fallback" :key="index" class="tier-row">
              <span class="tier-index">{{ index + 1 }} 档</span>
              <label>完整输入上限<input v-model="tier.maxInput" inputmode="numeric" :aria-label="`默认第${index + 1}档完整输入上限`" /></label>
              <label>输入单价<input v-model="tier.input" inputmode="decimal" :aria-label="`默认第${index + 1}档输入单价`" /></label>
              <label>缓存输入单价<input v-model="tier.cached" inputmode="decimal" :aria-label="`默认第${index + 1}档缓存输入单价`" /></label>
              <label>输出单价<input v-model="tier.output" inputmode="decimal" :aria-label="`默认第${index + 1}档输出单价`" /></label>
              <button type="button" class="remove small" :disabled="draft.fallback.length === 1" :aria-label="`删除默认第${index + 1}档`" @click="draft.fallback.splice(index, 1)">删除档位</button>
            </div>
          </div>
        </section>

        <section class="cost-block">
          <div class="block-heading"><div><h3>按模型配置价格</h3><p>供应商填 * 表示通用匹配；这里只填写模型标识，无需密钥或访问凭据。</p></div><button type="button" class="secondary small" :disabled="draft.models.length >= 32" @click="addModel">添加模型规则</button></div>
          <p class="explanation">档位按完整输入 Token 数选择，缓存输入属于输入的一部分，不从完整输入中扣除。上限须逐档递增，最后一档填 0 表示超过前档的其余输入。</p>
          <p v-if="!draft.models.length" class="empty-state">暂未设置特定模型规则，使用上方默认价格。</p>
          <article v-for="(rule, ruleIndex) in draft.models" :key="ruleIndex" class="model-rule">
            <div class="model-heading"><span class="rule-number">规则 {{ ruleIndex + 1 }}</span><label>供应商<input v-model="rule.provider" :aria-label="`模型规则${ruleIndex + 1}供应商`" autocomplete="off" /></label><label class="model-name">模型名称<input v-model="rule.model" :aria-label="`模型规则${ruleIndex + 1}模型名称`" autocomplete="off" /></label><button type="button" class="remove small" :aria-label="`删除模型规则${ruleIndex + 1}`" @click="draft.models.splice(ruleIndex, 1)">删除模型</button></div>
            <div class="tier-list">
              <div v-for="(tier, tierIndex) in rule.tiers" :key="tierIndex" class="tier-row">
                <span class="tier-index">{{ tierIndex + 1 }} 档</span>
                <label>完整输入上限<input v-model="tier.maxInput" inputmode="numeric" :aria-label="`模型规则${ruleIndex + 1}第${tierIndex + 1}档完整输入上限`" /></label>
                <label>输入单价<input v-model="tier.input" inputmode="decimal" :aria-label="`模型规则${ruleIndex + 1}第${tierIndex + 1}档输入单价`" /></label>
                <label>缓存输入单价<input v-model="tier.cached" inputmode="decimal" :aria-label="`模型规则${ruleIndex + 1}第${tierIndex + 1}档缓存输入单价`" /></label>
                <label>输出单价<input v-model="tier.output" inputmode="decimal" :aria-label="`模型规则${ruleIndex + 1}第${tierIndex + 1}档输出单价`" /></label>
                <button type="button" class="remove small" :disabled="rule.tiers.length === 1" :aria-label="`删除模型规则${ruleIndex + 1}第${tierIndex + 1}档`" @click="rule.tiers.splice(tierIndex, 1)">删除档位</button>
              </div>
            </div>
            <button type="button" class="add-tier small" :disabled="rule.tiers.length >= 16" :aria-label="`模型规则${ruleIndex + 1}添加档位`" @click="addLiveCostTier(rule.tiers)">＋ 添加价格档位</button>
          </article>
        </section>
      </fieldset>
      <footer class="cost-footer"><div><strong>参考核算，不等于供应商实付账单</strong><p>保存后用于后续请求与每轮规划；历史费用不回算，调价不清除已有成品缓存。金额最多保留六位小数。</p></div><button type="submit" class="primary" :disabled="busy || needsReload">{{ saving ? '保存中…' : '保存成本设置' }}</button></footer>
    </form>
  </section>
</template>

<style scoped>
.live-cost-settings{margin:20px 0;padding:26px;border:1px solid #dfe5f1;border-radius:20px;background:#fff;color:#24344e;min-width:0;scroll-margin-top:24px}.cost-header,.block-heading,.cost-footer,.model-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;flex-wrap:wrap}.cost-header{margin-bottom:24px}.cost-kicker{color:#7365dc;font-size:12px;font-weight:700}h2{font-size:22px;margin:7px 0}h3{font-size:17px;margin:0 0 5px}p{margin:0;color:#6c7890;font-size:13px;line-height:1.7}.cost-header>div,.block-heading>div,.cost-footer>div{flex:1;min-width:180px}button,input{font:inherit;box-sizing:border-box}button{border:1px solid #d9dfef;border-radius:10px;padding:11px 16px;background:#f5f6fd;color:#5361b3;cursor:pointer;font-size:13px;line-height:1.4;flex-shrink:0}button:disabled{cursor:default;opacity:.5}button:focus-visible,input:focus-visible{outline:3px solid #b9bcff;outline-offset:2px}fieldset{min-width:0;border:0;padding:0;margin:0}fieldset:disabled{opacity:.65}.cost-block{border:1px solid #e4e9f3;padding:20px;border-radius:15px;margin-bottom:16px}.block-heading{margin-bottom:18px}.unit-pill{border-radius:20px;background:#f1efff;color:#7060c8;padding:6px 11px;font-size:12px;white-space:nowrap}.budget-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}label{display:grid;gap:7px;font-size:12px;font-weight:600;color:#55647e;min-width:0}input{width:100%;min-width:0;border:1px solid #dce2ef;border-radius:9px;background:#fcfdff;color:#273a59;padding:10px 11px;font-size:14px;line-height:1.5}small{color:#8a96aa;font-size:11px;font-weight:400}.explanation{margin-top:14px;background:#f7f8fd;padding:10px 13px;border-radius:9px}.tts-grid{display:grid;grid-template-columns:minmax(180px,280px) 1fr;gap:24px;align-items:center}.price-equivalent{display:grid;gap:7px;color:#5366b4;font-size:14px}.price-equivalent span{font-size:12px;color:#8792a4}.tier-list{display:grid;gap:12px}.tier-row{display:grid;grid-template-columns:38px minmax(110px,1.1fr) repeat(3,minmax(95px,1fr)) auto;gap:12px;align-items:end;min-width:0}.tier-index{font-size:12px;color:#8b97b2;align-self:center}.small{padding:9px 12px;font-size:12px}.remove{background:#fff7f7;border-color:#f2dfe2;color:#b55665}.model-rule{padding:18px;border:1px solid #e5e8f3;border-radius:13px;margin-top:16px;background:linear-gradient(180deg,#fafaff,#fff 140px)}.model-heading{margin-bottom:18px;align-items:end}.rule-number{font-size:12px;color:#7a6acb;align-self:center}.model-heading label{flex:1;min-width:140px}.model-heading .model-name{flex:2}.add-tier{margin-top:14px;border-style:dashed;background:#fff}.empty-state{padding:20px;text-align:center;background:#f8f9fd;border-radius:10px}.cost-footer{padding:8px 2px 0}.cost-footer strong{font-size:13px}.cost-footer p{margin-top:5px;max-width:740px}.primary{background:#6455df;color:#fff;border-color:#6455df;box-shadow:0 6px 16px #6455df20}.cost-alert{padding:13px 16px;margin:0 0 18px;border-radius:11px;line-height:1.7;font-size:13px;white-space:pre-line}.error{color:#ae3d53;background:#fff1f4;border:1px solid #f6dce2}.success{color:#177b5b;background:#edfaf3;border:1px solid #d4eddf}.cost-loading{padding:20px 0}@media(max-width:1100px){.tier-row{grid-template-columns:32px repeat(2,minmax(0,1fr))}.tier-index{grid-row:span 2}.tier-row>.remove{grid-column:3;justify-self:end}.tier-row{padding:13px;background:#f7f8fd;border-radius:10px}.budget-grid{gap:12px}}@media(max-width:600px){.live-cost-settings{padding:16px;border-radius:15px}.cost-block{padding:15px}.cost-header{gap:12px}.cost-header>button{width:100%}.budget-grid,.tts-grid{grid-template-columns:1fr}.tts-grid{gap:15px}.block-heading{gap:12px}.unit-pill{white-space:normal}.tier-row{grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:10px}.tier-index{grid-row:auto;grid-column:1/-1}.tier-row>.remove{grid-column:2}.model-rule{padding:13px}.model-heading label{flex-basis:100%;min-width:0}.model-heading{gap:12px}.model-heading>.remove{margin-left:auto}.cost-footer>button{width:100%;margin-top:4px}.cost-footer>div,.block-heading>div,.cost-header>div{min-width:0}.cost-footer{gap:10px}}
</style>
