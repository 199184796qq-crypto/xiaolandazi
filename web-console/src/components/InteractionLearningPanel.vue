<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { request, createAgentLearningTurn, adoptAgentLearningSession, adoptLiveAgentPlanFacts } from '../api'
import type { AgentLearningSession, AgentLearningResult, LiveAgentPlanFact } from '../types'
import { session } from '../session'
import InteractionFactDialog from './InteractionFactDialog.vue'
import SimulationMiningProgress from './SimulationMiningProgress.vue'
import InteractionTopicIcon from './InteractionTopicIcon.vue'
import { interactionSuggestionTopic, interactionTopicFilters, interactionTopicLabels, type InteractionSuggestionTopic } from '../interactionSuggestionTopics'

const props = withDefaults(defineProps<{ roomId: number | null; planId: number | null; facts?: LiveAgentPlanFact[]; compact?: boolean; showSimulation?: boolean; sourceType?: 'initial_simulation' | 'live_uncertain'; history?: boolean }>(), { compact: false, showSimulation: true })
const emit = defineEmits<{ adopted: []; factsUpdated: [scope: { roomId: number; planId: number; tenantId: number }] }>()
const items = ref<AgentLearningSession[]>([])
const count = ref(0)
const total = ref(0), page = ref(1), query = ref(''), status = ref('pending')
const topicFilter = ref<InteractionSuggestionTopic | 'all'>('all')
const isSimulationView = computed(() => props.sourceType === 'initial_simulation' && !props.history && !props.compact)
const statusLabel = (value: string) => value === 'closed' ? '无需纠正 / 已处理' : value === 'adopted' ? '已采用纠正' : '待处理'

const expanded = ref(false)
const busy = ref('')
const error = ref('')
const notice = ref('')
const simulationPhase = ref<'running' | 'loading' | 'complete' | 'failed' | ''>('')
const simulationGenerated = ref(0)
let simulationRun = 0
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
const scopedItems = computed(() => props.sourceType ? items.value.filter(item => item.source_type === props.sourceType) : items.value)
const filteredItems = computed(() => isSimulationView.value && topicFilter.value !== 'all' ? scopedItems.value.filter(item => interactionSuggestionTopic(item) === topicFilter.value) : scopedItems.value)
const displayedItems = computed(() => isSimulationView.value && topicFilter.value !== 'all' ? filteredItems.value.slice((page.value - 1) * 30, page.value * 30) : filteredItems.value)
const displayedCount = computed(() => props.sourceType ? displayedItems.value.length : count.value)
const panelTitle = computed(() => props.history ? '纠正处理历史' : props.compact ? '互动待确认' : props.sourceType === 'initial_simulation' ? '模拟挖掘' : props.sourceType === 'live_uncertain' ? '直播互动' : '模拟挖掘与直播待确认')
let epoch = 0
let requestSequence = 0
let locationSequence = 0
let poll: ReturnType<typeof setInterval> | undefined

watch(() => [props.roomId, props.planId, session.bootstrap?.actor.user_id, session.bootstrap?.actor.tenant_id], () => {
  simulationRun++
  simulationPhase.value = ''
  simulationGenerated.value = 0
  if (busy.value === 'simulate') busy.value = ''
}, { flush: 'sync' })

async function refresh(manual = false) {
  const room = props.roomId, plan = props.planId, token = epoch
  if (!room || !plan || (!manual && document.hidden)) return false
  const sequence = ++requestSequence
  const selectedTopic = isSimulationView.value ? topicFilter.value : 'all'
  try {
    const params = new URLSearchParams({ plan_id: String(plan), status: props.history ? status.value : 'pending', page: String(selectedTopic === 'all' ? page.value : 1), q: query.value })
    if (props.sourceType && !props.history) params.set('source', props.sourceType)
    const result = await request<{ items: AgentLearningSession[]; pending_count: number; total: number }>(`/api/v1/live/rooms/${room}/interaction-learning?${params}`, { suppressErrorDialog: true })
    if (token !== epoch || sequence !== requestSequence) return false
    const records = [...(result.items || [])]
    // Topic filters include subsequent pages rather than hiding matching older records.
    if (selectedTopic !== 'all') {
      const pages = Math.ceil((result.total || 0) / 30)
      for (let nextPage = 2; nextPage <= pages; nextPage++) {
        params.set('page', String(nextPage))
        const next = await request<{ items: AgentLearningSession[] }>(`/api/v1/live/rooms/${room}/interaction-learning?${params}`, { suppressErrorDialog: true })
        if (token !== epoch || sequence !== requestSequence) return false
        records.push(...(next.items || []))
      }
    }
    items.value = [...new Map(records.map(item => [item.id, item])).values()]
    count.value = result.pending_count || 0
    total.value = selectedTopic === 'all' ? result.total || 0 : filteredItems.value.length
    if (manual) error.value = ''
    if (!items.value.some(item => item.id === locatedId.value && item.status === 'editing')) locatedId.value = null
    return true
  } catch (err) {
    if (token === epoch && sequence === requestSequence && manual) error.value = err instanceof Error ? err.message : '读取待确认互动失败'
    return false
  }
}

watch(() => [props.roomId, props.planId, props.history, props.sourceType, session.bootstrap?.actor.user_id, session.bootstrap?.actor.tenant_id], () => {
  epoch++; page.value = 1; status.value = props.history ? 'all' : 'pending'; query.value = ''; topicFilter.value = 'all'
  items.value = []; count.value = 0; total.value = 0; editing.value = null; preview.value = null; feedback.value = ''; notice.value = ''; error.value = ''
  locatedId.value = null
  factContext.value = null; factNotices.value = {}
  suggestionErrors.value = {}; adoptedSuggestions.value = {}
  if (busy.value.startsWith('suggestion:')) busy.value = ''
  void refresh()
}, { immediate: true })

onMounted(() => { poll = setInterval(() => { if (!busy.value && !locating.value && !factContext.value) void refresh() }, 30_000) })
onBeforeUnmount(() => { epoch++; simulationRun++; if (poll) clearInterval(poll) })

async function locatePending(itemId: number) {
  const token = epoch
  const sequence = ++locationSequence
  locating.value = true
  expanded.value = true; page.value = 1; query.value = ''; topicFilter.value = 'all'; status.value = 'pending'
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
  const room = props.roomId, plan = props.planId
  if (!room || !plan || busy.value) return
  const run = ++simulationRun
  const current = () => run === simulationRun && room === props.roomId && plan === props.planId
  busy.value = 'simulate'; error.value = ''; notice.value = ''
  simulationPhase.value = 'running'; simulationGenerated.value = 0
  query.value = ''; page.value = 1; topicFilter.value = 'all'; requestSequence++
  try {
    const result = await request<{ generated: number }>(`/api/v1/live/rooms/${room}/interaction-learning/simulate`, { method: 'POST', body: JSON.stringify({ plan_id: plan }), suppressErrorDialog: true })
    if (!current()) return
    if (!Number.isInteger(result.generated) || result.generated < 0) throw new Error('模拟结果未完整返回，请刷新后重试。')
    simulationGenerated.value = result.generated
    simulationPhase.value = 'loading'
    expanded.value = true
    const refreshed = await refresh(true)
    if (!current()) return
    simulationPhase.value = 'complete'
    if (!refreshed) notice.value = '模拟已完成，建议列表暂未刷新；点击右上角“刷新”查看。'
    window.dispatchEvent(new Event('interaction-review-updated'))
  } catch (err) { if (current()) { simulationPhase.value = 'failed'; error.value = err instanceof Error ? err.message : '模拟挖掘失败' } }
  finally { if (current() && busy.value === 'simulate') busy.value = '' }
}

function begin(item: AgentLearningSession) {
  editing.value = item.id
  feedback.value = item.review?.suggested_rule || ''
  preview.value = null; error.value = ''; notice.value = ''
}

function selectTopic(topic: InteractionSuggestionTopic | 'all') {
  if (busy.value) return
  topicFilter.value = topic; query.value = ''; page.value = 1
  void refresh(true)
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
  <section v-if="visible && roomId && planId" ref="panelElement" class="interaction-learning" :class="{ 'is-simulation-view': isSimulationView }" aria-label="互动纠正与待确认问题">
    <div class="learning-toolbar">
    <header>
      <div class="learning-heading">
        <i v-if="isSimulationView" class="simulation-brand-icon" aria-hidden="true"><svg viewBox="0 0 32 32"><path d="M7 3h12l6 6v10M19 3v7h6M7 3v25h10M11 14h8M11 19h5" /><circle cx="23" cy="23" r="5" /><path d="m27 27 3 3" /></svg></i>
        <div>
        <button v-if="showSimulation && !compact && !history" type="button" class="simulation-start" aria-label="模拟挖掘" :aria-busy="busy === 'simulate'" :disabled="!!busy" title="点击开始模拟挖掘" @click="simulate"><strong>{{ panelTitle }}</strong><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 10 7-10 7z" /></svg></button>
        <strong v-else>{{ panelTitle }}</strong>
        <p v-if="isSimulationView" class="simulation-subtitle">基于用户可能提出的问题，生成优化建议，帮助智能体更好地回答。</p>
        <span v-else role="status" aria-live="polite">{{ history ? `共${total}条记录` : (displayedCount ? `${displayedCount}条待处理` : '暂无待确认问题') }}</span>
        </div>
      </div>
      <div class="actions">
        <span v-if="isSimulationView" class="simulation-generation-status" role="status" aria-live="polite" :data-simulation-phase="simulationPhase"><i aria-hidden="true"></i>{{ topicFilter !== 'all' || query.trim() ? `筛选到${total}条模拟建议` : simulationPhase === 'complete' ? `已生成${simulationGenerated}条模拟建议` : `共${total}条模拟建议` }}</span>
        <button v-if="compact" @click="expanded = !expanded">{{ expanded ? '收起' : '查看并纠正' }}</button>
        <button v-else :disabled="!!busy" @click="refresh(true)"><svg v-if="isSimulationView" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 7v5h-5M4 17v-5h5M5 8a8 8 0 0 1 13-3l2 2M4 17l2 2a8 8 0 0 0 13-3" /></svg>刷新</button>
      </div>
    </header>
      <form v-if="showItems && !compact" class="history-filters" @submit.prevent="page = 1; refresh(true)">
        <select v-if="history" v-model="status" aria-label="处理状态" :disabled="!!busy" @change="page = 1; refresh(true)"><option value="all">全部记录</option><option value="pending">待处理</option><option value="closed">无需纠正 / 已处理</option><option value="adopted">已采用纠正</option></select>
        <div class="learning-search-box"><svg v-if="isSimulationView" viewBox="0 0 24 24" aria-hidden="true"><circle cx="10" cy="10" r="7" /><path d="m15 15 6 6" /></svg><input v-model="query" maxlength="200" aria-label="检索纠正记录" :disabled="busy === 'simulate'" :placeholder="isSimulationView ? '搜索问题或回答，例如：价格、包邮、发货时间、退换货…' : '搜索问题或回答，例如：价格、包邮'" /><button :disabled="!!busy" type="submit">{{ isSimulationView ? '搜索' : '检索' }}</button></div>
        <div v-if="isSimulationView" class="simulation-quick-filters" aria-label="模拟建议快捷筛选"><span>快捷筛选：</span><button v-for="filter in interactionTopicFilters" :key="filter.key" type="button" :class="'is-' + filter.key" :aria-pressed="topicFilter === filter.key" :disabled="!!busy" @click="selectTopic(filter.key)"><InteractionTopicIcon :topic="filter.key" />{{ filter.label }}</button></div>
        <template v-else><button type="button" :disabled="!!busy" @click="query = '价格'; page = 1; refresh(true)">价格</button><button type="button" :disabled="!!busy" @click="query = ''; page = 1; refresh(true)">全部内容</button></template>
      </form>
    </div>
    <SimulationMiningProgress v-if="simulationPhase && showSimulation && !compact && !history && (!isSimulationView || simulationPhase !== 'complete')" :phase="simulationPhase" :generated="simulationGenerated" />
    <template v-if="showItems">
      <p v-if="!isSimulationView" class="hint">模型只提供建议，事实由你审核。你可以修改并采用纠正，或关闭无需处理的记录；这些建议不会单独阻断互动。</p>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="!displayedItems.length && busy !== 'simulate'" class="hint">{{ isSimulationView && (topicFilter !== 'all' || query.trim()) ? '没有找到匹配的模拟建议，换个关键词或点击“全部内容”看看吧。' : sourceType === 'initial_simulation' ? '还没有模拟建议，点击左上方“模拟挖掘”开始。' : sourceType === 'live_uncertain' ? '暂无需要处理的直播互动记录。' : '暂无需要处理的互动记录。' }}</p>
      <article v-for="item in displayedItems" :key="item.id" :data-interaction-review-id="item.id" class="simulation-result-card" :class="[{ 'is-review-located': locatedId === item.id }, 'topic-' + interactionSuggestionTopic(item)]" tabindex="-1">
        <aside v-if="isSimulationView" class="simulation-topic-rail"><i><InteractionTopicIcon :topic="interactionSuggestionTopic(item)" /></i><strong>{{ interactionTopicLabels[interactionSuggestionTopic(item)] }}</strong></aside>
        <div class="review-card-content">
        <div class="item-meta"><span>{{ item.source_type === 'initial_simulation' ? '模拟建议' : '直播记录' }} · {{ item.review?.kind === 'semantic' ? '问法纠正' : '缺少回答条件' }}</span><small>{{ statusLabel(item.status) }} · 记录{{ item.review?.occurrences || 1 }}次 · {{ new Date(item.updated_at).toLocaleString() }}</small></div>
        <strong class="question"><span v-if="item.status === 'editing'" class="pending-question-dot" role="img" aria-label="待处理问题"></span><span>{{ item.question }}</span></strong>
        <div class="review-card-body"><div class="review-card-info">
        <p v-if="item.review?.observed_context"><b>实际现场：</b><span>{{ item.review.observed_context }}</span></p>
        <p v-if="item.review?.context"><b>适用场景：</b><span>{{ item.review.context }}</span></p>
        <p v-if="item.original_reply"><b>生成的回答：</b><span>{{ item.original_reply }}</span></p>
        <p class="model-suggestion"><b>模型建议：</b><span>{{ item.review?.reason }}</span><button v-if="item.status === 'editing' && (item.review?.reason || '').trim()" type="button" class="suggestion-adopt" :disabled="!!busy || isSuggestionAdopted(item)" :aria-label="isSuggestionAdopted(item) ? '模型建议已采纳' : '采纳模型建议'" title="将这条模型建议加入当前方案的事实依据" @click="adoptSuggestion(item)">{{ isSuggestionAdopted(item) ? '已采纳' : busy === `suggestion:${item.id}` ? '采纳中…' : '采纳' }}</button></p>
        <p v-if="suggestionErrors[item.id]" class="error" role="alert">{{ suggestionErrors[item.id] }}</p>
        <p v-if="item.review?.needed_facts?.length"><b>建议补充：</b><span>{{ item.review.needed_facts.join('；') }}</span></p>
        <p v-if="item.review?.suggested_rule"><b>纠正建议：</b><span>{{ item.review.suggested_rule }}</span></p>
        </div>
        <div v-if="item.status === 'editing'" class="actions review-card-actions">
          <button :disabled="!!busy" @click="begin(item)"><svg v-if="isSimulationView" viewBox="0 0 24 24" aria-hidden="true"><path d="m4 16-.8 5 5-.8L20 8l-4-4zM14 6l4 4" /></svg>填写纠正</button>
          <button v-if="item.review?.kind === 'missing_fact'" :disabled="!!busy" @click="openFactDialog(item)"><svg v-if="isSimulationView" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 3h9l4 4v14H6zM14 3v5h5M9 12h7M9 16h5" /></svg>补充事实依据</button>
          <button class="is-complete-action" :disabled="!!busy" @click="close(item)"><svg v-if="isSimulationView" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{{ item.review?.kind === 'missing_fact' ? '已处理，标记完成' : '无需纠正' }}</button>
          <details v-if="isSimulationView" class="review-record-details"><summary aria-label="查看记录详情" title="查看记录详情">•••</summary><div><strong>记录详情</strong><dl><dt>问题含义</dt><dd>{{ item.review?.meaning || '暂无说明' }}</dd><dt>记录次数</dt><dd>{{ item.review?.occurrences || 1 }}次</dd><dt>记录时间</dt><dd>{{ new Date(item.updated_at).toLocaleString() }}</dd></dl></div></details>
        </div>
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
        </div>
      </article>
      <div v-if="total > 30" class="actions" aria-label="记录翻页"><button :disabled="page <= 1 || !!busy" @click="page--; refresh(true)">上一页</button><span>第{{ page }}页 · 共{{ total }}条</span><button :disabled="page * 30 >= total || !!busy" @click="page++; refresh(true)">下一页</button></div>
    </template>
    <InteractionFactDialog v-if="factContext" :room-id="roomId" :plan-id="planId" :item="factContext" @close="factContext = null" @saved="onFactSaved" />
  </section>
</template>

<style scoped>
.history-filters{display:flex;gap:10px;flex-wrap:wrap;margin:15px 0}.history-filters input,.history-filters select{padding:9px 12px;border:1px solid #d2dbf2;border-radius:10px;font:inherit;color:inherit;background:#fff}.history-filters input{flex:1;min-width:220px}
.learning-search-box{display:flex;align-items:center;gap:10px;flex:1;min-width:0}.learning-search-box input{min-width:0;width:100%}
.interaction-learning{margin:16px 0;padding:20px;border:1px solid #d9e2f8;border-radius:20px;background:#f8faff;color:#293851;font-size:16px}
.interaction-learning .simulation-start{display:inline-flex;align-items:center;gap:9px;padding:6px 8px;margin-left:-8px;border:1px solid transparent;border-radius:10px;background:transparent;color:#293851}.interaction-learning .simulation-start svg{width:19px;height:19px;fill:#7364e8}.interaction-learning .simulation-start:hover:not(:disabled){border-color:#d7ddff;background:#eeefff;color:#5754bd}.interaction-learning .simulation-start:focus-visible{outline:2px solid #786ee6;outline-offset:3px}
.interaction-learning article.is-review-located{border-color:#e77c96;outline:3px solid #ed879e;outline-offset:3px;background:#fffafb;scroll-margin-block:24px}
.interaction-learning .question{display:flex;align-items:flex-start;gap:10px}.pending-question-dot{flex:0 0 9px;width:9px;height:9px;margin-top:8px;border-radius:50%;background:#e74d65;box-shadow:0 0 0 4px #e74d6514}
.interaction-learning .suggestion-adopt{display:inline-block;margin-left:12px;padding:4px 12px;vertical-align:middle;font-size:14px;line-height:1.5;white-space:nowrap;background:#eef0ff;border-color:#d5dafa}.interaction-learning .suggestion-adopt:disabled{cursor:not-allowed}
header,header>div,.actions,.item-meta{display:flex;align-items:center;gap:12px;flex-wrap:wrap}header{justify-content:space-between}header strong{font-size:19px}header span,.item-meta{color:#627391}.hint{color:#627391;line-height:1.7}button{padding:9px 15px;border:1px solid #d2dbf2;border-radius:10px;background:white;color:#4d54d5;cursor:pointer;font:inherit}button:disabled{opacity:.55;cursor:wait}article{margin-top:14px;padding:18px;border:1px solid #e0e6f5;border-radius:15px;background:white}.item-meta{justify-content:space-between;font-size:14px}.question{display:block;margin-top:12px;font-size:18px}article p{margin:10px 0;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.editor{margin-top:15px;padding-top:15px;border-top:1px solid #e8edf8}.editor label{display:block}textarea{box-sizing:border-box;width:100%;margin:10px 0;padding:12px;border:1px solid #cbd5ef;border-radius:10px;font:inherit;color:inherit}.preview{margin-top:12px;padding:15px;background:#f1f4ff;border-radius:12px}.error{color:#c33350}.notice{color:#137a69}
.interaction-learning.is-simulation-view{padding:0;border:0;background:transparent;margin:16px 0}
.is-simulation-view .learning-toolbar{padding:20px 24px 17px;border:1px solid #dde6fb;border-radius:20px;background:linear-gradient(125deg,#fff,#f5f8ff);box-shadow:0 8px 24px #5890d80b}
.is-simulation-view .learning-heading{gap:17px;min-width:0;flex-wrap:nowrap}
.simulation-brand-icon{display:grid;place-items:center;flex:0 0 60px;width:60px;height:60px;border-radius:18px;background:linear-gradient(140deg,#e5e1ff,#d4d6ff);color:#7567e9}
.simulation-brand-icon svg{width:37px;height:37px;fill:none;stroke:currentColor;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round}
.is-simulation-view .simulation-start{padding:0 4px;margin-left:-4px}.is-simulation-view .simulation-start strong{font-size:25px;color:#19315b}
.simulation-subtitle{margin:5px 0 0;color:#7085ad;font-size:14px;line-height:1.65}
.simulation-generation-status{display:inline-flex;align-items:center;gap:9px;padding:10px 16px;border-radius:999px;background:#fff;color:#5b74a0;font-size:13px;white-space:nowrap}
.simulation-generation-status i{width:8px;height:8px;border-radius:50%;background:#20b786}
.is-simulation-view header>.actions button{display:inline-flex;align-items:center;justify-content:center;gap:10px;min-width:108px;height:46px;font-weight:750;background:linear-gradient(120deg,#fff,#f7faff)}
.is-simulation-view header>.actions svg{width:22px;height:22px;fill:none;stroke:currentColor;stroke-width:1.9;stroke-linecap:round;stroke-linejoin:round}
.is-simulation-view .history-filters{display:flex;align-items:center;gap:20px;margin:17px 0 0;flex-wrap:wrap}
.is-simulation-view .learning-search-box{position:relative;gap:0;flex:1 1 450px;min-width:300px;height:48px;border:1px solid #cfd9f4;border-radius:12px;background:#fff;box-shadow:0 2px 6px #667dba08}
.is-simulation-view .learning-search-box>svg{flex:0 0 21px;width:21px;height:21px;margin-left:14px;fill:none;stroke:#7188b3;stroke-width:1.8;stroke-linecap:round}
.is-simulation-view .learning-search-box input{min-width:0;height:100%;padding:10px 12px;border:0;background:transparent;color:#3f547b;font-size:14px;outline:none}
.is-simulation-view .learning-search-box:focus-within{outline:2px solid #a3affb;outline-offset:2px}
.is-simulation-view .learning-search-box button{flex:0 0 100px;align-self:stretch;border:0;border-radius:11px;background:linear-gradient(130deg,#8784fb,#5b56e8);color:#fff;font-size:15px;font-weight:750;box-shadow:0 3px 10px #7775f22b}
.simulation-quick-filters{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.simulation-quick-filters>span{color:#7c8caa;font-size:13px}
.simulation-quick-filters button{--filter-color:#7364e5;--filter-soft:#f5f2ff;display:inline-flex;align-items:center;justify-content:center;gap:8px;height:44px;padding:8px 12px;border:1px solid #e3deff;background:var(--filter-soft);color:var(--filter-color);font-size:14px;font-weight:750;white-space:nowrap}
.simulation-quick-filters .is-price{--filter-color:#f09b36;--filter-soft:#fff9ef;border-color:#ffe9c9}.simulation-quick-filters .is-shipping{--filter-color:#3485f1;--filter-soft:#f0f7ff;border-color:#d4e6ff}.simulation-quick-filters .is-after_sales{--filter-color:#18af7c;--filter-soft:#effcf6;border-color:#cef2e2}.simulation-quick-filters .is-gift{--filter-color:#ec5c88;--filter-soft:#fff3f7;border-color:#ffdaE6}
.simulation-quick-filters button[aria-pressed="true"]{box-shadow:inset 0 0 0 1px var(--filter-color),0 3px 9px #788db615}
.is-simulation-view .simulation-result-card{--topic-color:#7d66dc;--topic-soft:#f3efff;--topic-strong:#a489ee;position:relative;display:grid;grid-template-columns:110px minmax(0,1fr);gap:20px;min-height:166px;margin-top:12px;padding:8px 20px 8px 8px;border:1px solid #e4ebfa;border-radius:17px;background:#fff;box-shadow:0 5px 18px #6194d908}
.is-simulation-view .topic-price{--topic-color:#ee941f;--topic-soft:#fff9ed;--topic-strong:#ffc869}.is-simulation-view .topic-shipping{--topic-color:#228afb;--topic-soft:#eef7ff;--topic-strong:#77c3ff}.is-simulation-view .topic-after_sales{--topic-color:#13b77b;--topic-soft:#edfcf4;--topic-strong:#83e4b9}.is-simulation-view .topic-gift{--topic-color:#ef5b84;--topic-soft:#fff1f5;--topic-strong:#ffa0ba}
.is-simulation-view .simulation-result-card::before{content:'';position:absolute;left:7px;top:8px;bottom:8px;width:3px;border-radius:3px;background:var(--topic-strong)}
.simulation-topic-rail{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:13px;min-width:0;padding:16px 9px;border-radius:13px;background:linear-gradient(140deg,var(--topic-soft),#fff);color:var(--topic-color)}
.simulation-topic-rail>i{display:grid;place-items:center;width:52px;height:52px;border:5px solid var(--topic-soft);border-radius:50%;background:linear-gradient(140deg,var(--topic-strong),var(--topic-color));color:#fff;box-shadow:0 5px 14px var(--topic-soft)}
.simulation-topic-rail svg{width:27px;height:27px}.simulation-topic-rail strong{font-size:14px;white-space:nowrap}
.review-card-content{min-width:0}.is-simulation-view .review-card-content{padding:6px 0}
.is-simulation-view .item-meta{gap:8px;font-size:12px;line-height:1.6;color:#8293b3}.is-simulation-view .item-meta small{font-size:12px;font-weight:400}
.is-simulation-view .question{margin:6px 0 7px;font-size:19px;color:#16305a;line-height:1.5}.is-simulation-view .pending-question-dot{width:7px;height:7px;flex-basis:7px;margin-top:10px;box-shadow:0 0 0 3px #e74d6512}
.is-simulation-view .review-card-body{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:18px;align-items:end}
.review-card-info{min-width:0}.is-simulation-view .review-card-info p{display:flex;align-items:flex-start;gap:9px;margin:4px 0;font-size:14px;line-height:1.65;color:#4d6083;white-space:pre-wrap}
.is-simulation-view .review-card-info p>b{flex:0 0 73px;color:#6d82a7;font-weight:750;white-space:nowrap}.is-simulation-view .review-card-info p>span{min-width:0;flex:1}
.is-simulation-view .review-card-info .model-suggestion>span{flex:0 1 auto}
.is-simulation-view .model-suggestion .suggestion-adopt{flex:0 0 auto;margin:0 0 0 3px;padding:4px 11px;border:0;border-radius:9px;background:#eeebff;color:#7163d6;font-size:13px;line-height:1.5}
.is-simulation-view .review-card-actions{gap:9px;justify-content:flex-end;align-self:end;padding:0 0 3px;flex-wrap:wrap;max-width:560px}
.is-simulation-view .review-card-actions>button{display:inline-flex;align-items:center;justify-content:center;gap:8px;min-height:39px;padding:8px 12px;border-radius:10px;font-size:13px;font-weight:750;white-space:nowrap}
.is-simulation-view .review-card-actions svg{width:18px;height:18px;flex:0 0 18px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.is-simulation-view .review-card-actions .is-complete-action{border-color:#bcecd8;background:linear-gradient(120deg,#f0fff7,#e9fbf3);color:#17ad79}
.review-record-details{position:relative}.review-record-details summary{display:grid;place-items:center;width:40px;min-height:39px;border:1px solid #dce5f9;border-radius:10px;background:#fff;color:#687bab;font-size:14px;letter-spacing:2px;cursor:pointer;list-style:none}.review-record-details summary::-webkit-details-marker{display:none}.review-record-details summary:focus-visible{outline:2px solid #8c8bde;outline-offset:2px}
.review-record-details>div{position:absolute;right:0;bottom:calc(100% + 9px);z-index:3;width:270px;max-width:calc(100vw - 48px);padding:16px;border:1px solid #dce5f9;border-radius:13px;background:#fff;box-shadow:0 10px 32px #314b7026;color:#3e557a;font-size:13px}
.review-record-details dl{display:grid;grid-template-columns:64px minmax(0,1fr);gap:9px;margin:12px 0 0}.review-record-details dt{color:#8493b3}.review-record-details dd{margin:0;overflow-wrap:anywhere;line-height:1.6}
.is-simulation-view .simulation-result-card.is-review-located{border-color:#e77c96;outline:3px solid #ed879e;background:#fffafb}
.is-simulation-view .fact-saved-notice{padding:8px 10px;margin:8px 0 0;border-radius:8px;background:#ecfbf3;font-size:13px}
@media(max-width:1400px){.is-simulation-view .review-card-body{grid-template-columns:minmax(0,1fr)}.is-simulation-view .review-card-actions{max-width:none}.is-simulation-view .history-filters{gap:14px}.is-simulation-view .learning-search-box{flex-basis:100%}.is-simulation-view .review-card-info p{font-size:14px}}
@media(max-width:700px){.is-simulation-view .learning-toolbar{padding:17px 14px}.simulation-brand-icon{width:47px;height:47px;flex-basis:47px;border-radius:14px}.simulation-brand-icon svg{width:29px;height:29px}.is-simulation-view .learning-heading{gap:12px;align-items:flex-start}.is-simulation-view .simulation-start strong{font-size:22px}.simulation-subtitle{font-size:12px}.is-simulation-view header>.actions{justify-content:space-between;width:100%;gap:6px}.simulation-generation-status{padding:8px 10px;font-size:12px}.is-simulation-view header>.actions button{min-width:86px;height:40px}.is-simulation-view .learning-search-box{min-width:0;height:44px}.is-simulation-view .learning-search-box button{flex-basis:76px;padding-inline:9px;font-size:13px}.is-simulation-view .learning-search-box>svg{margin-left:10px;width:18px;height:18px;flex-basis:18px}.is-simulation-view .learning-search-box input{padding-inline:8px;font-size:12px}.simulation-quick-filters{gap:8px}.simulation-quick-filters>span{flex-basis:100%;font-size:12px}.simulation-quick-filters button{height:38px;font-size:12px;padding:7px 10px;gap:6px}.is-simulation-view .simulation-result-card{grid-template-columns:minmax(0,1fr);gap:9px;padding:10px 14px}.simulation-topic-rail{flex-direction:row;justify-content:flex-start;gap:9px;padding:8px 11px}.simulation-topic-rail>i{width:36px;height:36px;border-width:3px}.simulation-topic-rail svg{width:20px;height:20px}.simulation-topic-rail strong{font-size:13px}.is-simulation-view .item-meta{font-size:11px}.is-simulation-view .item-meta small{font-size:11px}.is-simulation-view .question{font-size:17px}.is-simulation-view .review-card-info p{font-size:13px;gap:7px}.is-simulation-view .review-card-info p>b{flex-basis:65px}.is-simulation-view .review-card-actions{justify-content:flex-start;gap:8px}.is-simulation-view .review-card-actions>button{font-size:12px;padding:7px 10px}.review-record-details{position:static}.review-record-details>div{left:14px;right:14px;width:auto}}
</style>
