<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { request } from '../api'
import { session } from '../session'
import type { RiskSource } from '../riskSources'
interface Review { claim_sha256: string; source_key: string; text: string; rule_id: string; severity: string; status: string; evidence: string; reason: string; program_decision: string; semantic_decision: string; appeal_status?: string; appeal_reason?: string; confirmed_by_user_id?: number; confirmed_at?: string; subject?: string; sources?: RiskSource[] }
const props = withDefaults(defineProps<{ planId: number; tenantId?: number; refreshKey?: string; visible: boolean; headless?: boolean }>(), { headless: false })
const emit = defineEmits<{ count: [value: number]; state: [value: {loading: boolean; error: string}]; editSource: [value: {sourceKey: string; text: string}]; reviews: [value: Review[]] }>()
const items = ref<Review[]>([]), loading = ref(false), error = ref(''), notice = ref(''), drafts = ref<Record<string,string>>({}), appealDrafts = ref<Record<string,string>>({}), appealOpen = ref<Record<string,boolean>>({})
const customerCanConfirm = computed(() => session.bootstrap?.actor.role === 'customer')
let epoch = 0, active = true
let currentScope = ''
watch([loading, error], () => emit('state', {loading: loading.value, error: error.value}), { immediate: true })
function baseUrl() { return '/api/v1/live-agent-plans/' + props.planId + '/fact-risk-reviews' }
function url(suffix = '') { return baseUrl() + suffix + (props.tenantId ? '?tenant_id=' + props.tenantId : '') }
async function load() {
  const generation = ++epoch
  if (!props.planId) { items.value = []; error.value = ''; loading.value = false; emit('count', 0); emit('reviews', []); return }
  loading.value = true
  try {
    const response = await request<{ items: Review[]; unresolved_count: number }>(url(), { cache: 'no-store', suppressErrorDialog: true })
    if (!active || generation !== epoch) return
    items.value = response.items
    emit('reviews', [...items.value])
    emit('count', Number.isInteger(response.unresolved_count) && response.unresolved_count >= 0 ? response.unresolved_count : items.value.filter((item) => item.status !== 'confirmed').length)
    error.value = ''
  } catch (e) { if (active && generation === epoch) error.value = e instanceof Error ? e.message : '读取风险登记失败' }
  finally { if (generation === epoch) loading.value = false }
}
async function confirm(item: Review, evidenceOverride = '') {
  if (!customerCanConfirm.value) { error.value = '必须由当前方案所属用户本人确认'; return }
  if (item.severity === 'P0' && item.semantic_decision === 'hard') { error.value = '该项为P0硬拦截规则，不能由用户直接放行，请提交后台复核'; return }
  const needsEvidence = item.severity === 'P1' || item.severity === 'P0'
  const evidence = needsEvidence ? (evidenceOverride || drafts.value[item.claim_sha256] || '').trim() : ''
  if (needsEvidence && !evidence) { error.value = '请填写实际依据、适用商品、主体或批次说明'; return }
  ++epoch
  loading.value = true; error.value = ''
  try {
    await request(url(), { method: 'PUT', body: JSON.stringify({ claim_sha256: item.claim_sha256, action: 'confirm', confirmed: true, evidence }) })
    notice.value = needsEvidence
      ? '实际依据已由用户确认并备案；商品、批次、关键数值或规则变化时需重新确认。'
      : 'P2表达已由用户确认并备案，可以继续使用。'
    await load()
  } catch (e) { error.value = e instanceof Error ? e.message : '确认失败' }
  finally { loading.value = false }
}
async function submitAppeal(item: Review, reasonOverride = '') {
  if (!customerCanConfirm.value) { error.value = '必须由当前方案所属用户本人提交'; return }
  const reason = (reasonOverride || appealDrafts.value[item.claim_sha256] || '').trim()
  if (!reason) { error.value = '请填写认为误拦的原因或可供后台核对的依据'; return }
  loading.value = true; error.value = ''
  try {
    await request(url(), { method: 'PUT', body: JSON.stringify({ claim_sha256: item.claim_sha256, action: 'appeal', reason }) })
    notice.value = '后台复核申请已提交；审核完成前该表达仍不会放行。'
    appealOpen.value[item.claim_sha256] = false
    await load()
  } catch (e) { error.value = e instanceof Error ? e.message : '提交后台复核申请失败' }
  finally { loading.value = false }
}
function riskLabel(item: Review) {
  if (item.status === 'confirmed') return '用户已确认并备案'
  if (item.severity !== 'P0') return '等待用户确认'
  if (item.semantic_decision === 'hard') return 'P0 硬拦截 · 禁止直接放行'
  return 'P0 · 等待用户确认依据'
}
function formatTime(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN', { hour12: false })
}
watch(() => [props.planId, props.tenantId, props.refreshKey], () => {
  const scope = JSON.stringify([props.tenantId, props.planId])
  if (scope !== currentScope) { currentScope = scope; items.value = []; error.value = ''; emit('count', 0); emit('reviews', []) }
  drafts.value = {}; appealDrafts.value = {}; appealOpen.value = {}; notice.value = ''; void load()
}, { immediate: true })
const timer = setInterval(() => { if (!loading.value && props.planId) void load() }, 30000)
onBeforeUnmount(() => { active = false; epoch++; clearInterval(timer) })
defineExpose({ confirmReview: confirm, submitReviewAppeal: submitAppeal })
</script>
<template>
  <section v-show="visible && !headless && (items.length || error)" class="fact-risk-evidence">
    <h3>事实依据 · 风险与资质待确认</h3>
    <p>程序找出风险词后，由你确认或举证备案；P0 硬拦截规则不能直接放行，只能修改原文或申请后台复核。</p>
    <p v-if="error" class="error">{{ error }} <button @click="load">重新读取</button></p>
    <article v-for="item in items" :key="item.claim_sha256" :class="['risk-' + item.severity]">
      <header><strong>{{ item.severity }} · {{ riskLabel(item) }}</strong><small>{{ item.rule_id }}</small></header>
      <p>{{ item.text }}</p>
      <p v-if="item.subject">适用商品：{{ item.subject }}</p>
      <p class="risk-reason"><strong>处理原因：</strong>{{ item.reason || '命中当前风险规则，需要按对应档位处理。' }}</p>
      <details v-if="item.sources?.length"><summary>合并 {{ item.sources.length }} 处来源，只需确认一次</summary><p v-for="source in item.sources" :key="source.source_key">{{ source.text }}</p></details>
      <template v-if="item.severity === 'P0' && item.semantic_decision === 'hard' && item.status !== 'confirmed'">
        <p class="p0-blocked">该项为 P0 硬拦截规则，不能由商户直接放行。可以修改原始内容重新检测，或提交后台复核申请。</p>
        <div v-if="customerCanConfirm" class="risk-actions"><button type="button" :disabled="loading" @click="emit('editSource', { sourceKey: item.source_key, text: item.text })">修改原始内容</button><button v-if="item.appeal_status !== 'pending'" type="button" :disabled="loading" @click="appealOpen[item.claim_sha256] = true">申请后台复核</button></div>
        <p v-if="item.appeal_status === 'pending'" class="appeal-pending">后台复核申请已提交，审核前继续保持拦截。</p>
        <div v-else-if="appealOpen[item.claim_sha256]" class="appeal-form"><textarea v-model="appealDrafts[item.claim_sha256]" placeholder="说明为什么认为是误拦，并填写可核对的事实或资质" maxlength="2000" /><div><button type="button" :disabled="loading" @click="appealOpen[item.claim_sha256] = false">取消</button><button type="button" class="confirm" :disabled="loading || !(appealDrafts[item.claim_sha256] || '').trim()" @click="submitAppeal(item)">确认提交申请</button></div></div>
      </template>
      <template v-else-if="item.severity === 'P0' && item.semantic_decision !== 'hard' && item.status !== 'confirmed' && customerCanConfirm">
        <p class="semantic-pass-note">该表达命中风险规则，请填写实际依据（如经营年限、资质、数据来源等）并确认备案。</p>
        <textarea v-model="drafts[item.claim_sha256]" placeholder="填写实际持有的经营年限记录、资质名称、适用主体/商品及有效范围" maxlength="2000" />
        <div class="risk-actions"><button type="button" :disabled="loading" @click="emit('editSource', { sourceKey: item.source_key, text: item.text })">修改原始内容</button><button type="button" class="confirm" :disabled="loading || !(drafts[item.claim_sha256] || '').trim()" @click="confirm(item)">提交依据并确认</button></div>
      </template>
      <template v-else-if="item.severity === 'P1' && item.status !== 'confirmed' && customerCanConfirm">
        <textarea v-model="drafts[item.claim_sha256]" placeholder="实际持有的证据名称、适用商品/批次、有效期或检测指标。默认申报有纸质材料仍需在此确认。" maxlength="2000" />
        <button :disabled="loading || !(drafts[item.claim_sha256] || '').trim()" @click="confirm(item)">确认并备案</button>
      </template>
      <template v-else-if="item.severity === 'P2' && item.status !== 'confirmed' && customerCanConfirm">
        <p>请核对该表达及上下文；确认后会记录确认人和时间。</p>
        <button :disabled="loading" @click="confirm(item)">确认可以使用并备案</button>
      </template>
      <p v-else-if="item.status === 'confirmed'">{{ item.severity === 'P1' ? '依据说明' : '确认记录' }}：{{ item.evidence }}<template v-if="item.confirmed_at"> · {{ formatTime(item.confirmed_at) }}</template></p>
      <p v-else>需要当前方案所属用户本人登录后确认；运维人员不能代替用户放行。</p>
    </article>
    <p v-if="notice">{{ notice }}</p>
  </section>
</template>
<style scoped>
.fact-risk-evidence{margin:16px 0;padding:20px;border:1px solid #eea5b3;border-radius:16px;background:#fff9fa}article{border-left:4px solid #d00000;padding:14px;margin:12px 0;background:white;border-radius:10px}.risk-P1{border-color:#db2777}.risk-P1 strong{color:#db2777}.risk-P2{border-color:#e45632}.risk-P2 strong{color:#c64a2a}strong,.error{color:#b91c32}header{display:flex;justify-content:space-between;gap:12px}small{color:#81909c}p{line-height:1.6}.risk-reason{padding:10px 12px;border-radius:9px;background:#f6f7fb;color:#3d485b}.risk-reason strong{color:inherit}.semantic-pass-note{padding:10px 12px;border-radius:9px;background:#fff7df;color:#785a16}.p0-blocked{padding:10px 12px;border-radius:9px;background:#fff0f1;color:#a7192e;font-weight:700}.appeal-pending{padding:10px 12px;border-radius:9px;background:#eef4ff;color:#536a9b;font-weight:700}.risk-actions{display:flex;flex-wrap:wrap;gap:8px}.appeal-form{display:grid;gap:8px;margin-top:10px;padding:12px;border:1px solid #efc8cd;border-radius:10px;background:#fff8f8}.appeal-form>div{display:flex;justify-content:flex-end;gap:8px}textarea{width:100%;min-height:76px;border:1px solid #ced8df;border-radius:8px;padding:10px;box-sizing:border-box}button{border:1px solid #cad8df;border-radius:8px;padding:8px 12px;margin-top:8px;background:#fff;color:#13786d;cursor:pointer}button.confirm{border-color:#5788e8;background:#5788e8;color:#fff}button:disabled{opacity:.5;cursor:not-allowed}
</style>
