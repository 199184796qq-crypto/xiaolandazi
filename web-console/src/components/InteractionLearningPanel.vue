<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { request, createAgentLearningTurn, adoptAgentLearningSession } from '../api'
import type { AgentLearningSession, AgentLearningResult } from '../types'

const props = withDefaults(defineProps<{ roomId: number | null; planId: number | null; compact?: boolean; showSimulation?: boolean; sourceType?: 'initial_simulation' | 'live_uncertain' }>(), { compact: false, showSimulation: true })
const emit = defineEmits<{ adopted: []; openFacts: [] }>()
const items = ref<AgentLearningSession[]>([])
const count = ref(0)
const expanded = ref(false)
const busy = ref('')
const error = ref('')
const notice = ref('')
const editing = ref<number | null>(null)
const feedback = ref('')
const preview = ref<AgentLearningResult | null>(null)
const visible = computed(() => !props.compact || count.value > 0)
const showItems = computed(() => !props.compact || expanded.value)
const displayedItems = computed(() => props.sourceType ? items.value.filter(item => item.source_type === props.sourceType) : items.value)
const displayedCount = computed(() => props.sourceType ? displayedItems.value.length : count.value)
const panelTitle = computed(() => props.compact ? '互动待确认' : props.sourceType === 'initial_simulation' ? '模拟挖掘' : props.sourceType === 'live_uncertain' ? '直播互动' : '模拟挖掘与直播待确认')
let epoch = 0
let poll: ReturnType<typeof setInterval> | undefined

async function refresh(manual = false) {
  const room = props.roomId, plan = props.planId, token = epoch
  if (!room || !plan || document.hidden) return
  try {
    const result = await request<{ items: AgentLearningSession[]; pending_count: number }>(`/api/v1/live/rooms/${room}/interaction-learning?plan_id=${plan}`, { suppressErrorDialog: true })
    if (token !== epoch) return
    items.value = result.items || []
    count.value = result.pending_count || 0
    if (manual) error.value = ''
  } catch (err) {
    if (token === epoch && manual) error.value = err instanceof Error ? err.message : '读取待确认互动失败'
  }
}

watch(() => [props.roomId, props.planId], () => {
  epoch++
  items.value = []; count.value = 0; editing.value = null; preview.value = null; feedback.value = ''; notice.value = ''; error.value = ''
  void refresh()
}, { immediate: true })

onMounted(() => { poll = setInterval(() => { if (!busy.value) void refresh() }, 30_000) })
onBeforeUnmount(() => { epoch++; if (poll) clearInterval(poll) })

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
    if (token === epoch) { editing.value = null; preview.value = null; await refresh(true) }
  } catch (err) { if (token === epoch) error.value = err instanceof Error ? err.message : '处理记录失败' }
  finally { busy.value = '' }
}
</script>

<template>
  <section v-if="visible && roomId && planId" class="interaction-learning" aria-label="互动纠正与待确认问题">
    <header>
      <div role="status" aria-live="polite"><strong>{{ panelTitle }}</strong><span>{{ displayedCount ? `${displayedCount}条待处理` : '暂无待确认问题' }}</span></div>
      <div class="actions">
        <button v-if="showSimulation && !compact" :disabled="!!busy" @click="simulate">{{ busy === 'simulate' ? '模型模拟中…' : '模拟挖掘' }}</button>
        <button v-if="compact" @click="expanded = !expanded">{{ expanded ? '收起' : '查看并纠正' }}</button>
        <button v-else :disabled="!!busy" @click="refresh(true)">刷新</button>
      </div>
    </header>
    <template v-if="showItems">
      <p class="hint">模型只提供建议，事实由你审核。你可以修改并采用纠正，或关闭无需处理的记录；这些建议不会单独阻断互动。</p>
      <p v-if="notice" class="notice" role="status">{{ notice }}</p>
      <p v-if="error" class="error" role="alert">{{ error }}</p>
      <p v-if="!displayedItems.length" class="hint">{{ sourceType === 'initial_simulation' ? '还没有模拟建议，点击“模拟挖掘”生成。' : sourceType === 'live_uncertain' ? '暂无需要处理的直播互动记录。' : '暂无需要处理的互动记录。' }}</p>
      <article v-for="item in displayedItems" :key="item.id">
        <div class="item-meta"><span>{{ item.source_type === 'initial_simulation' ? '模拟建议' : '直播记录' }} · {{ item.review?.kind === 'semantic' ? '问法纠正' : '缺少回答条件' }}</span><small>记录{{ item.review?.occurrences || 1 }}次</small></div>
        <strong class="question">{{ item.question }}</strong>
        <p v-if="item.review?.observed_context"><b>实际现场：</b>{{ item.review.observed_context }}</p>
        <p v-if="item.review?.context"><b>适用场景：</b>{{ item.review.context }}</p>
        <p v-if="item.original_reply"><b>生成的回答：</b>{{ item.original_reply }}</p>
        <p><b>模型建议：</b>{{ item.review?.reason }}</p>
        <p v-if="item.review?.needed_facts?.length"><b>建议补充：</b>{{ item.review.needed_facts.join('；') }}</p>
        <p v-if="item.review?.suggested_rule"><b>纠正建议：</b>{{ item.review.suggested_rule }}</p>
        <div class="actions">
          <button :disabled="!!busy" @click="begin(item)">填写纠正</button>
          <button v-if="item.review?.kind === 'missing_fact'" @click="emit('openFacts')">补充事实依据</button>
          <button :disabled="!!busy" @click="close(item)">{{ item.review?.kind === 'missing_fact' ? '已处理，标记完成' : '无需纠正' }}</button>
        </div>
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
      <p v-if="count > items.length" class="hint">当前显示最近{{ items.length }}条，处理后可继续查看其余记录。</p>
    </template>
  </section>
</template>

<style scoped>
.interaction-learning{margin:16px 0;padding:20px;border:1px solid #d9e2f8;border-radius:20px;background:#f8faff;color:#293851;font-size:16px}
header,header>div,.actions,.item-meta{display:flex;align-items:center;gap:12px;flex-wrap:wrap}header{justify-content:space-between}header strong{font-size:19px}header span,.item-meta{color:#627391}.hint{color:#627391;line-height:1.7}button{padding:9px 15px;border:1px solid #d2dbf2;border-radius:10px;background:white;color:#4d54d5;cursor:pointer;font:inherit}button:disabled{opacity:.55;cursor:wait}article{margin-top:14px;padding:18px;border:1px solid #e0e6f5;border-radius:15px;background:white}.item-meta{justify-content:space-between;font-size:14px}.question{display:block;margin-top:12px;font-size:18px}article p{margin:10px 0;line-height:1.7;white-space:pre-wrap;overflow-wrap:anywhere}.editor{margin-top:15px;padding-top:15px;border-top:1px solid #e8edf8}.editor label{display:block}textarea{box-sizing:border-box;width:100%;margin:10px 0;padding:12px;border:1px solid #cbd5ef;border-radius:10px;font:inherit;color:inherit}.preview{margin-top:12px;padding:15px;background:#f1f4ff;border-radius:12px}.error{color:#c33350}.notice{color:#137a69}
</style>
