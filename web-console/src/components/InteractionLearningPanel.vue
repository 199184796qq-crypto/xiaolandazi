<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { request, createAgentLearningTurn, adoptAgentLearningSession, adoptLiveAgentPlanFacts } from '../api'
import type { AgentLearningSession, AgentLearningResult, LiveAgentPlanFact } from '../types'
import { session } from '../session'
import InteractionFactDialog from './InteractionFactDialog.vue'

const props = withDefaults(defineProps<{ roomId: number | null; planId: number | null; facts?: LiveAgentPlanFact[]; compact?: boolean; showSimulation?: boolean; sourceType?: 'initial_simulation' | 'live_uncertain'; history?: boolean }>(), { compact: false, showSimulation: true })
const emit = defineEmits<{ adopted: []; factsUpdated: [scope: { roomId: number; planId: number; tenantId: number }] }>()
const items = ref<AgentLearningSession[]>([])
const count = ref(0)
const total = ref(0), page = ref(1), query = ref(''), status = ref('pending')
const statusLabel = (value: string) => value === 'closed' ? '无需纠正 / 已处理' : value === 'adopted' ? '已采用纠正' : '待处理'

const expanded = ref(false)
const busy = ref('')
const error = ref('')
const notice = ref('')
const editing = ref<number | null>(null)
const feedback = ref('')
const preview = ref<AgentLearningResult | null>(null)
const panelElement = ref<HTMLElement | null>(null)
const locatedId = ref<number | null>(null)
const locating = ref(false)
const factContext = ref<AgentLearningSession | null>(null)
const factNotices = ref<Record<number, string>>({})
const suggestionErrors = ref<Record<number, string>>({})
const adoptedSuggestions = ref<Record<number, boolean>>({})
const visible = computed(() => !props.compact || count.value > 0)
const showItems = computed(() => !props.compact || expanded.value)
const displayedItems = computed(() => props.sourceType ? items.value.filter(item => item.source_type === props.sourceType) : items.value)
const displayedCount = computed(() => props.sourceType ? displayedItems.value.length : count.value)
const panelTitle = computed(() => props.history ? '纠正处理历史' : props.compact ? '互动待确认' : props.sourceType === 'initial_simulation' ? '模拟挖掘' : props.sourceType === 'live_uncertain' ? '直播互动' : '模拟挖掘与直播待确认')
let epoch = 0
let requestSequence = 0
let locationSequence = 0
let poll: ReturnType<typeof setInterval> | undefined

async function refresh(manual = false) {
  const room = props.roomId, plan = props.planId, token = epoch
  if (!room || !plan || (!manual && document.hidden)) return false
  const sequence = ++requestSequence
  try {
    const params = new URLSearchParams({ plan_id: String(plan), status: props.history ? status.value : 'pending', page: String(page.value), q: query.value })
    if (props.sourceType && !props.history) params.set('source', props.sourceType)
    const result = await request<{ items: AgentLearningSession[]; pending_count: number; total: number }>(`/api/v1/live/rooms/${room}/interaction-learning?${params}`, { suppressErrorDialog: true })
    if (token !== epoch || sequence !== requestSequence) return false
    items.value = result.items || []
    count.value = result.pending_count || 0; total.value = result.total || 0
    if (manual) error.value = ''
    if (!items.value.some(item => item.id === locatedId.value && item.status === 'editing')) locatedId.value = null
    return true
  } catch (err) {
    if (token === epoch && sequence === requestSequence && manual) error.value = err instanceof Error ? err.message : '读取待确认互动失败'
    return false
  }
}

watch(() => [props.roomId, props.planId, props.history, props.sourceType, session.bootstrap?.actor.user_id, session.bootstrap?.actor.tenant_id], () => {
  epoch++; page.value = 1; status.value = props.history ? 'all' : 'pending'; query.value = ''
  items.value = []; count.value = 0; total.value = 0; editing.value = null; preview.value = null; feedback.value = ''; notice.value = ''; error.value = ''
  locatedId.value = null
  factContext.value = null; factNotices.value = {}
  suggestionErrors.value = {}; adoptedSuggestions.value = {}
  if (busy.value.startsWith('suggestion:')) busy.value = ''
  void refresh()
}, { immediate: true })

onMounted(() => { poll = setInterval(() => { if (!busy.value && !locating.value && !factContext.value) void refresh() }, 30_000) })
onBeforeUnmount(() => { epoch++; if (poll) clearInterval(poll) })

async function locatePending(itemId: number) {
  const token = epoch
  const sequence = ++locationSequence
  locating.value = true
  expanded.value = true; page.value = 1; query.value = ''; status.value = 'pending'
  try {
    if (!await refresh(true) || token !== epoch || sequence !== locationSequence) return false
    const item = displayedItems.value.find(item => item.id === itemId && item.status === 'editing')
    if (!item) return false
    locatedId.value = item.id
    await nextTick()
    if (token !== epoch || sequence !== locationSequence) return false
    const target = panelElement.value?.querySelector<HTMLElement>(`[data-interaction-review-id="${item.id}"]`)
    if (!target) return false
    target.scrollIntoView({ behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth', block: 'start', inline: 'nearest' })
    target.focus({ preventScroll: true })
    return true
  } finally { if (sequence === locationSequence) locating.value = false }
}

function cancelLocation() { locationSequence++; locating.value = false }
defineExpose({ locatePending, cancelLocation })

async function simulate() {
  const room = props.roomId, plan = props.planId, token = epoch
  if (!room || !plan || busy.value) return
  busy.value = 'simulate'; error.value = ''; notice.value = ''
  try {
    const result = await request<{ generated: number }>(`/api/v1/live/rooms/${room}/interaction-learning/simulate`, { method: 'POST', body: JSON.stringify({ plan_id: plan }) })
    if (token !== epoch) return
    notice.value = `已生成${result.generated}条模拟建议，请查看并确认。`
    expanded.value = true
    await refresh(true)
  } catch (err) { if (token === epoch) error.value = err instanceof Error ? err.message : '模拟挖掘失败' }
  finally { busy.value = '' }
}

function begin(item: AgentLearningSession) {
  editing.value = item.id
  feedback.value = item.review?.suggested_rule || ''
  preview.value = null; error.value = ''; notice.value = ''
}

function openFactDialog(item: AgentLearningSession) {
  if (busy.value || item.status !== 'editing' || item.review?.kind !== 'missing_fact') return
  if (!props.planId || !props.roomId || item.review.plan_id !== props.planId || item.room_id !== props.roomId || !(item.tenant_id > 0)) {
    error.value = '当前问题的方案信息已变化，请刷新后重试。'
    return
  }
  factContext.value = item
}

function onFactSaved(result: { roomId: number; planId: number; tenantId: number; itemId: number; unchanged: boolean }) {
  if (props.roomId !== result.roomId || props.planId !== result.planId || factContext.value?.id !== result.itemId || factContext.value.tenant_id !== result.tenantId) return
  factContext.value = null
  factNotices.value[result.itemId] = result.unchanged
    ? '当前方案已包含这条事实。核对好后，可以点击“已处理，标记完成”。'
    : '事实依据已保存到当前方案。核对好后，可以点击“已处理，标记完成”。'
  emit('factsUpdated', result)
}

function suggestionSource(item: AgentLearningSession) {
  return `manual:interaction-learning-suggestion:room:${item.room_id}:record:${item.id}`
}

function isSuggestionAdopted(item: AgentLearningSession) {
  return adoptedSuggestions.value[item.id] || props.facts?.some(fact =>
    fact.plan_id === props.planId && fact.tenant_id === item.tenant_id && fact.status === 'active'
    && fact.source_ref === suggestionSource(item) && fact.value.trim() === (item.review?.reason || '').trim())
}

async function adoptSuggestion(item: AgentLearningSession) {
  const room = props.roomId, plan = props.planId, token = epoch
  const value = (item.review?.reason || '').trim()
  if (busy.value || factContext.value || item.status !== 'editing' || !value || isSuggestionAdopted(item)) return
  if (!room || !plan || item.review?.plan_id !== plan || item.room_id !== room || !(item.tenant_id > 0)) {
    suggestionErrors.value[item.id] = '当前问题的方案信息已变化，请刷新后重试。'
    return
  }
  const operation = `suggestion:${item.id}`
  busy.value = operation
  suggestionErrors.value[item.id] = ''
  try {
    const key = Array.from(`互动建议：${item.question || item.review.meaning || '回答条件'}`).slice(0, 255).join('')
    const result = await adoptLiveAgentPlanFacts(plan, [{
      category: 'other', key, value, status: 'pending', review_bucket: 'adoptable',
      review_reason: '用户采纳模型建议', source_quote: '',
    }], item.tenant_id, suggestionSource(item), { suppressErrorDialog: true })
    if (token !== epoch) return
    const unchanged = result.results?.[0]?.status === 'unchanged'
    if (!result.adopted && !unchanged) throw new Error(result.results?.[0]?.message || '建议未采纳，请重试')
    adoptedSuggestions.value[item.id] = true
    factNotices.value[item.id] = unchanged ? '这条建议已经在当前方案的事实依据里啦。' : '已采纳，已加入当前方案的事实依据。'
    emit('factsUpdated', { roomId: room, planId: plan, tenantId: item.tenant_id })
  } catch (cause) {
    if (token === epoch) suggestionErrors.value[item.id] = cause instanceof Error ? cause.message : '采纳建议失败，请重试'
  } finally { if (token === epoch && busy.value === operation) busy.value = '' }
}

async function generatePreview(item: AgentLearningSession) {
  const room = props.roomId, token = epoch
  if (!room || !feedback.value.trim() || busy.value) return
  busy.value = `preview:${item.id}`; error.value = ''; preview.value = null
  try {
    const out = await createAgentLearningTurn(room, item.id, feedback.value.trim())
    if (token === epoch && editing.value === item.id) preview.value = out.result
  } catch (err) { if (token === epoch) error.value = err instanceof Error ? err.message : '生成纠正预览失败' }
  finally { busy.value = '' }
}

async function adopt(item: AgentLearningSession) {
  const room = props.roomId, token = epoch
  if (!room || !preview.value || busy.value) return
  busy.value = `adopt:${item.id}`; error.value = ''
  try {
    await adoptAgentLearningSession(room, item.id)
    if (token !== epoch) return
    notice.value = '纠正已采用，后续回答使用最新规则。'
    window.dispatchEvent(new Event('interaction-review-updated'))
    editing.value = null; preview.value = null
    emit('adopted')
  } catch (err) { if (token === epoch) error.value = err instanceof Error ? err.message : '采用纠正失败' }
  finally { busy.value = ''; if (token === epoch) await refresh() }
}

async function close(item: AgentLearningSession) {
  const room = props.roomId, token = epoch
  if (!room || busy.value) return
  busy.value = `close:${item.id}`; error.value = ''
  try {
    await request(`/api/v1/live/rooms/${room}/interaction-learning/${item.id}/close`, { method: 'POST' })
    if (token === epoch) {
      editing.value = null; preview.value = null; notice.value = '记录已保留，可在交互策略的“处理历史”回看。'
      window.dispatchEvent(new Event('interaction-review-updated')); await refresh(true)
    }
  } catch (err) { if (token === epoch) error.value = err instanceof Error ? err.message : '处理记录失败' }
  finally { busy.value = '' }
}
</script>

<template>
  <section v-if="visible && roomId && planId" ref="panelElement" class="interaction-learning" aria-label="互动纠正与待确认问题">
    <header>
      <div role="status" aria-live="polite"><strong>{{ panelTitle }}</strong><span>{{ history ? `共${total}条记录` : (displayedCount ? `${displayedCount}条待处理` : '暂无待确认问题') }}</span></div>
      <div class="actions">
        <button v-if="showSimulation && !compact" :disabled="!!busy" @click="simulate">{{ busy === 'simulate' ? '模型模拟中…' : '模拟挖掘' }}</button>
        <button v-if="compact" @click="expanded = !expanded">{{ expanded ? '收起' : '查看并纠正' }}</button>
        <button v-else :disabled="!!busy" @click="refresh(true)">刷新</button>
      </div>
    </header>
    <template v-if="showItems">
      <form v-if="!compact" class="history-filters" @submit.prevent="page = 1; refresh(true)">
        <select v-if="history" v-model="status" aria-label="处理状态" @change="page = 1; refresh(true)"><option value="all">全部记录</option><option value="pending">待处理</option><option value="closed">无需纠正 / 已处理</option><option value="adopted">已采用纠正</option></select>
        <input v-model="query" maxlength="200" aria-label="检索纠正记录" placeholder="搜索问题或回答，例如：价格、包邮" />
        <button :disabled="!!busy" type="submit">检索</button>
        <button type="button" @click="query = '价格'; page = 1; refresh(true)">价格</button>
        <button type="button" @click="query = ''; page = 1; refresh(true)">全部内容</button>
      </form>
      <p class="hint">模型只提供建议，事实由你审核。你可以修改并采用纠正，或关闭无需处理的记录；这些建议不会单独阻断互动。</p>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="!displayedItems.length" class="hint">{{ sourceType === 'initial_simulation' ? '还没有模拟建议，点击“模拟挖掘”生成。' : sourceType === 'live_uncertain' ? '暂无需要处理的直播互动记录。' : '暂无需要处理的互动记录。' }}</p>
      <article v-for="item in displayedItems" :key="item.id" :data-interaction-review-id="item.id" :class="{ 'is-review-located': locatedId === item.id }" tabindex="-1">
        <div class="item-meta"><span>{{ item.source_type === 'initial_simulation' ? '模拟建议' : '直播记录' }} · {{ item.review?.kind === 'semantic' ? '问法纠正' : '缺少回答条件' }}</span><small>{{ statusLabel(item.status) }} · 记录{{ item.review?.occurrences || 1 }}次 · {{ new Date(item.updated_at).toLocaleString() }}</small></div>
        <strong class="question"><span v-if="item.status === 'editing'" class="pending-question-dot" role="img" aria-label="待处理问题"></span><span>{{ item.question }}</span></strong>
        <p v-if="item.review?.observed_context"><b>实际现场：</b>{{ item.review.observed_context }}</p>
        <p v-if="item.review?.context"><b>适用场景：</b>{{ item.review.context }}</p>
        <p v-if="item.original_reply"><b>生成的回答：</b>{{ item.original_reply }}</p>
        <p class="model-suggestion"><b>模型建议：</b><span>{{ item.review?.reason }}</span><button v-if="item.status === 'editing' && (item.review?.reason || '').trim()" type="button" class="suggestion-adopt" :disabled="!!busy || isSuggestionAdopted(item)" :aria-label="isSuggestionAdopted(item) ? '模型建议已采纳' : '采纳模型建议'" title="将这条模型建议加入当前方案的事实依据" @click="adoptSuggestion(item)">{{ isSuggestionAdopted(item) ? '已采纳' : busy === `suggestion:${item.id}` ? '采纳中…' : '采纳' }}</button></p>
        <p v-if="suggestionErrors[item.id]" class="error" role="alert">{{ suggestionErrors[item.id] }}</p>
        <p v-if="item.review?.needed_facts?.length"><b>建议补充：</b>{{ item.review.needed_facts.join('；') }}</p>
        <p v-if="item.review?.suggested_rule"><b>纠正建议：</b>{{ item.review.suggested_rule }}</p>
        <div v-if="item.status === 'editing'" class="actions">
          <button :disabled="!!busy" @click="begin(item)">填写纠正</button>
          <button v-if="item.review?.kind === 'missing_fact'" :disabled="!!busy" @click="openFactDialog(item)">补充事实依据</button>
          <button :disabled="!!busy" @click="close(item)">{{ item.review?.kind === 'missing_fact' ? '已处理，标记完成' : '无需纠正' }}</button>
        </div>
        <p v-if="factNotices[item.id]" class="notice fact-saved-notice" role="status">{{ factNotices[item.id] }}</p>
        <div v-if="editing === item.id" class="editor">
          <label :for="`review-${item.id}`">正确应该怎么理解或回答</label>
          <textarea :id="`review-${item.id}`" v-model="feedback" maxlength="5000" rows="3" placeholder="例如：食品包装场景中，抽是抽真空；不抽可以吗是在问能否选非真空包装。" @input="preview = null"></textarea>
          <button :disabled="!!busy || !feedback.trim()" @click="generatePreview(item)">{{ busy === `preview:${item.id}` ? '生成中…' : '生成纠正预览' }}</button>
          <div v-if="preview" class="preview">
            <strong>待确认的纠正</strong><p>{{ preview.result_text }}</p>
            <p v-if="preview.memory_type === 'fact'">这条包含业务事实，由你确认后采用。</p>
            <button :disabled="!!busy" @click="adopt(item)">{{ busy === `adopt:${item.id}` ? '采用中…' : '确认并采用' }}</button>
          </div>
        </div>
      </article>
      <div v-if="total > 30" class="actions" aria-label="记录翻页"><button :disabled="page <= 1 || !!busy" @click="page--; refresh(true)">上一页</button><span>第{{ page }}页 · 共{{ total }}条</span><button :disabled="page * 30 >= total || !!busy" @click="page++; refresh(true)">下一页</button></div>
    </template>
    <InteractionFactDialog v-if="factContext" :room-id="roomId" :plan-id="planId" :item="factContext" @close="factContext = null" @saved="onFactSaved" />
  </section>
</template>

<style scoped>
.history-filters{display:flex;gap:10px;flex-wrap:wrap;margin:15px 0}.history-filters input,.history-filters select{padding:9px 12px;border:1px solid #d2dbf2;border-radius:10px;font:inherit;color:inherit;background:#fff}.history-filters input{flex:1;min-width:220px}
.interaction-learning{margin:16px 0;padding:20px;border:1px solid #d9e2f8;border-radius:20px;background:#f8faff;color:#293851;font-size:16px}
.interaction-learning article.is-review-located{border-color:#e77c96;outline:3px solid #ed879e;outline-offset:3px;background:#fffafb;scroll-margin-block:24px}
.interaction-learning .question{display:flex;align-items:flex-start;gap:10px}.pending-question-dot{flex:0 0 9px;width:9px;height:9px;margin-top:8px;border-radius:50%;background:#e74d65;box-shadow:0 0 0 4px #e74d6514}
.interaction-learning .suggestion-adopt{display:inline-block;margin-left:12px;padding:4px 12px;vertical-align:middle;font-size:14px;line-height:1.5;white-space:nowrap;background:#eef0ff;border-color:#d5dafa}.interaction-learning .suggestion-adopt:disabled{cursor:not-allowed}
header,header>div,.actions,.item-meta{display:flex;align-items:center;gap:12px;flex-wrap:wrap}header{justify-content:space-between}header strong{font-size:19px}header span,.item-meta{color:#627391}.hint{color:#627391;line-height:1.7}button{padding:9px 15px;border:1px solid #d2dbf2;border-radius:10px;background:white;color:#4d54d5;cursor:pointer;font:inherit}button:disabled{opacity:.55;cursor:wait}article{margin-top:14px;padding:18px;border:1px solid #e0e6f5;border-radius:15px;background:white}.item-meta{justify-content:space-between;font-size:14px}.question{display:block;margin-top:12px;font-size:18px}article p{margin:10px 0;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.editor{margin-top:15px;padding-top:15px;border-top:1px solid #e8edf8}.editor label{display:block}textarea{box-sizing:border-box;width:100%;margin:10px 0;padding:12px;border:1px solid #cbd5ef;border-radius:10px;font:inherit;color:inherit}.preview{margin-top:12px;padding:15px;background:#f1f4ff;border-radius:12px}.error{color:#c33350}.notice{color:#137a69}
</style>
