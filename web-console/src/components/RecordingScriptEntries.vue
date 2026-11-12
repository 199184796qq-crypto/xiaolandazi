<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { annotatePlanText, autoGenerateLiveAgentPlanScript, type PlanTextRiskHit } from '../api'
import { scriptCharacters, scriptEntryErrors, type ScriptEntry } from '../recordingScriptGroups'

const props = defineProps<{ entries: readonly ScriptEntry[]; disabled?: boolean; planId?: number | null }>()
const emit = defineEmits<{ update: [entries: ScriptEntry[]]; violations: [count: number] }>()
const root = ref<HTMLElement | null>(null)
const addError = ref('')
const generating = ref<Record<string, boolean>>({})
const generateError = ref<Record<string, string>>({})
const generateWarning = ref<Record<string, string>>({})
const pendingDeleteID = ref<string | null>(null)
const cancelledDeleteTarget = ref<HTMLElement | null>(null)
const total = computed(() => props.entries.reduce((sum, entry) => sum + scriptCharacters(entry.text), 0))
const errors = computed(() => scriptEntryErrors(props.entries))
// Keep the editor preview aligned with the mainline's 320-rune/minute estimate.
// This is only a normal-speed preview; synthesized audio supplies actual duration.
function normalSpeechDuration(text: string): string {
  const seconds = Math.ceil(scriptCharacters(text) * 60 / 320)
  const minutes = Math.floor(seconds / 60)
  const remainder = seconds % 60
  return minutes > 0 ? `${minutes} 分 ${remainder} 秒` : `${seconds} 秒`
}
function cancelPendingDelete() {
  pendingDeleteID.value = null
}
// 按当前 P0 词表检查话术片。只作字面命中提示，不会改写正文或阻止保存。
const riskyHits = ref<Record<string, PlanTextRiskHit[]>>({})
const checking = ref<Record<string, boolean>>({})
const violationCount = computed(() => Object.values(riskyHits.value).reduce((sum, hits) => sum + hits.length, 0))
watch(violationCount, count => emit('violations', count), { immediate: true })
const checkError = ref<Record<string, string>>({})
const reviewTimers = new Map<string, ReturnType<typeof setTimeout>>()
const reviewControllers = new Map<string, AbortController>()
const reviewVersions = new Map<string, number>()
const queuedTexts = new Map<string, string>()
let queuedPlanID: number | null = null
let disposed = false

function setEntryState<T>(state: { value: Record<string, T> }, id: string, value?: T) {
  const next = { ...state.value }
  if (value === undefined) delete next[id]
  else next[id] = value
  state.value = next
}
function invalidateReview(id: string) {
  const timer = reviewTimers.get(id)
  if (timer) clearTimeout(timer)
  reviewTimers.delete(id)
  reviewControllers.get(id)?.abort()
  reviewControllers.delete(id)
  reviewVersions.set(id, (reviewVersions.get(id) ?? 0) + 1)
}
function clearReview(id: string) {
  invalidateReview(id)
  setEntryState(riskyHits, id)
  setEntryState(checkError, id)
  setEntryState(checking, id)
}

function segments(entry: ScriptEntry) {
  const hits = riskyHits.value[entry.id] ?? []
  if (!hits.length) return null
  const chars = Array.from(entry.text)
  const ranges = hits
    .map(hit => [Math.max(0, hit.original_start), Math.min(chars.length, hit.original_end)] as const)
    .filter(([start, end]) => end > start)
    .sort((a, b) => a[0] - b[0])
  if (!ranges.length) return null
  const merged: Array<[number, number]> = []
  for (const range of ranges) {
    const last = merged[merged.length - 1]
    if (last && range[0] <= last[1]) last[1] = Math.max(last[1], range[1])
    else merged.push([range[0], range[1]])
  }
  const parts: Array<{ text: string; risky: boolean }> = []
  let cursor = 0
  for (const [start, end] of merged) {
    if (start > cursor) parts.push({ text: chars.slice(cursor, start).join(''), risky: false })
    parts.push({ text: chars.slice(start, end).join(''), risky: true })
    cursor = end
  }
  if (cursor < chars.length) parts.push({ text: chars.slice(cursor).join(''), risky: false })
  return parts
}

function isCurrentReview(id: string, version: number, text: string, planId: number) {
  return reviewVersions.get(id) === version
    && props.planId === planId
    && props.entries.find(entry => entry.id === id)?.text === text
}
async function checkEntry(id: string, text: string, planId: number, version: number) {
  const controller = new AbortController()
  reviewControllers.set(id, controller)
  try {
    const result = await annotatePlanText(planId, text, controller.signal)
    if (!isCurrentReview(id, version, text, planId)) return
    const verified = verifyHits(text, result.hits ?? [])
    setEntryState(riskyHits, id, verified.length ? verified : undefined)
  } catch (err) {
    if (controller.signal.aborted || !isCurrentReview(id, version, text, planId)) return
    // 检测接口失败要显示出来，不能静默当作“没问题”。
    setEntryState(checkError, id, err instanceof Error ? err.message : 'P0 检测失败')
  } finally {
    if (isCurrentReview(id, version, text, planId)) setEntryState(checking, id)
    if (reviewControllers.get(id) === controller) reviewControllers.delete(id)
  }
}
function queueRiskCheck(entry: ScriptEntry) {
  const id = entry.id
  invalidateReview(id)
  setEntryState(riskyHits, id)
  setEntryState(checkError, id)
  const planId = props.planId ?? 0
  const text = entry.text
  if (!planId || !text.trim()) {
    setEntryState(checking, id)
    return
  }
  const version = reviewVersions.get(id) ?? 0
  setEntryState(checking, id, true)
  reviewTimers.set(id, setTimeout(() => {
    reviewTimers.delete(id)
    void checkEntry(id, text, planId, version)
  }, 320))
}
function syncRiskChecks() {
  const ids = new Set(props.entries.map(entry => entry.id))
  for (const id of [...queuedTexts.keys()]) {
    if (!ids.has(id)) {
      queuedTexts.delete(id)
      clearReview(id)
    }
  }
  const planChanged = queuedPlanID !== (props.planId ?? null)
  queuedPlanID = props.planId ?? null
  for (const entry of props.entries) {
    if (planChanged || queuedTexts.get(entry.id) !== entry.text) {
      queuedTexts.set(entry.id, entry.text)
      queueRiskCheck(entry)
    }
  }
}

// 服务端偏移以 Unicode code point 计数。只接受与本次原文严格对得上的 P0 命中，
// 迟到响应、P1/P2 与编辑后的旧偏移都不能染红当前文本。
function verifyHits(text: string, hits: PlanTextRiskHit[]) {
  const chars = Array.from(text)
  const out: PlanTextRiskHit[] = []
  for (const hit of hits) {
    if (hit.severity !== 'P0') continue
    const start = Math.max(0, hit.original_start)
    const end = Math.min(chars.length, hit.original_end)
    if (end <= start || chars.slice(start, end).join('') !== hit.matched_text) continue
    out.push({ ...hit, original_start: start, original_end: end })
  }
  return out
}

// autosize 让文本框高度跟着正文走，不出现内部滚动条。
function autosize(el: HTMLTextAreaElement | null | undefined) {
  if (!el) return
  // 先清掉旧显式高度，删字后也能收回高度；同一帧内完成，不会闪到最小高度。
  el.style.height = '0px'
  const border = el.offsetHeight - el.clientHeight
  el.style.height = `${el.scrollHeight + border}px`
}
function autosizeAll() {
  const editors = root.value?.querySelectorAll<HTMLTextAreaElement>('textarea')
  editors?.forEach(autosize)
}
onMounted(() => nextTick(autosizeAll))
watch(() => props.entries, () => nextTick(autosizeAll), { deep: true })
watch(() => [props.planId, ...props.entries.map(entry => `${entry.id}\u0000${entry.text}`)], syncRiskChecks, { immediate: true })
function syncScroll(entry: ScriptEntry, event: Event) {
  const mirror = root.value?.querySelector<HTMLElement>(`[data-mirror="${entry.id}"]`)
  const area = event.target as HTMLTextAreaElement
  if (mirror) mirror.scrollTop = area.scrollTop
}

watch(() => props.entries, () => {
  addError.value = ''
  if (pendingDeleteID.value && !props.entries.some(entry => entry.id === pendingDeleteID.value)) cancelPendingDelete()
}, { deep: true })
watch(() => props.disabled, disabled => {
  if (disabled) cancelPendingDelete()
})
function input(index: number, event: Event) {
  if (props.disabled) return
  autosize(event.target as HTMLTextAreaElement)
  setEntryState(generateWarning, props.entries[index].id)
  emit('update', props.entries.map((entry, i) => i === index ? { ...entry, text: (event.target as HTMLTextAreaElement).value } : entry))
}
async function autoGenerate(entry: ScriptEntry) {
  const planId = props.planId ?? 0
  const sourceText = entry.text
  if (props.disabled || generating.value[entry.id]) return
  if (!planId) {
    setEntryState(generateError, entry.id, '当前方案未保存，无法生成话术')
    return
  }
  setEntryState(generateError, entry.id)
  setEntryState(generateWarning, entry.id)
  setEntryState(generating, entry.id, true)
  try {
    const recentTexts = props.entries.filter(item => item.id !== entry.id && item.text.trim()).map(item => item.text).slice(-6)
    const result = await autoGenerateLiveAgentPlanScript(planId, entry.text, undefined, recentTexts)
    if (disposed || props.planId !== planId || props.disabled) return
    const current = props.entries.find(item => item.id === entry.id)
    if (!current || current.text !== sourceText) {
      setEntryState(generateError, entry.id, '话术已修改，本次生成结果未覆盖当前内容')
      return
    }
    const text = String(result.text || '').trim()
    if (!text) throw new Error('模型没有返回话术')
    emit('update', props.entries.map(item => item.id === entry.id ? { ...item, text } : item))
    if (result.p0_pending) setEntryState(generateWarning, entry.id, '生成稿已保留，仍有 P0 句子需要手动修改；消除红色标注后才能保存。')
    await nextTick()
    const textarea = root.value?.querySelector<HTMLTextAreaElement>(`#script-entry-${entry.id}`)
    if (textarea) {
      autosize(textarea)
      textarea.focus()
    }
  } catch (err) {
    if (!disposed && props.planId === planId) setEntryState(generateError, entry.id, err instanceof Error ? err.message : '自动生成失败')
  } finally {
    if (!disposed) setEntryState(generating, entry.id)
  }
}
async function add() {
  if (props.disabled) return
  if (errors.value.some(Boolean)) {
    addError.value = '请先修改上面的话术，使每条至少 400 字符且本组合计不超过 4000 字符。'
    return
  }
  if (4000 - total.value < 400) {
    addError.value = `本组只剩 ${4000 - total.value} 字符额度，不足新增一条至少 400 字符的话术。`
    return
  }
  let n = 1
  while (props.entries.some(entry => entry.id === `s${n}`)) n++
  emit('update', [...props.entries, { id: `s${n}`, text: '' }])
  await nextTick()
  const editors = root.value?.querySelectorAll('textarea')
  editors?.[editors.length - 1]?.focus()
}
function remove(entry: ScriptEntry, index: number, event: MouseEvent) {
  if (props.disabled || props.entries.length <= 1) return
  if (cancelledDeleteTarget.value === event.currentTarget) {
    cancelledDeleteTarget.value = null
    return
  }
  if (pendingDeleteID.value === entry.id) {
    cancelPendingDelete()
    emit('update', props.entries.filter((_, i) => i !== index))
    return
  }
  pendingDeleteID.value = entry.id
}
function cancelDeleteFromOutside(event: PointerEvent) {
  if (!pendingDeleteID.value || !(event.target instanceof Element)) return
  const confirmation = root.value?.querySelector<HTMLElement>('[data-delete-confirmation="true"]')
  if (confirmation?.contains(event.target)) return
  cancelPendingDelete()
  cancelledDeleteTarget.value = event.target.closest<HTMLElement>('[data-delete-entry]')
}
onMounted(() => document.addEventListener('pointerdown', cancelDeleteFromOutside, true))
onBeforeUnmount(() => {
  disposed = true
  document.removeEventListener('pointerdown', cancelDeleteFromOutside, true)
  for (const id of reviewVersions.keys()) invalidateReview(id)
})
</script>

<template>
  <div ref="root" class="recording-script-entries" @keydown.esc="cancelPendingDelete">
    <p class="recording-script-group-total">本组合计 {{ total }} / 4000 字符 · 每条至少 400 字符</p>
    <section v-for="(entry, index) in entries" :key="entry.id" class="recording-script-entry">
      <header>
        <label :for="`script-entry-${entry.id}`">话术 {{ index + 1 }}</label>
        <span>{{ scriptCharacters(entry.text) }} 字符</span>
        <span class="recording-script-entry-duration" title="按正常语速约 320 字/分钟估算，实际时长以合成音频为准。">· 正常语速约 {{ normalSpeechDuration(entry.text) }}</span>
        <button
          type="button"
          class="recording-script-entry-auto"
          :disabled="disabled || generating[entry.id]"
          :aria-label="`自动生成话术 ${index + 1}`"
          :title="generating[entry.id] ? '正在生成…' : '按方案人设、红线、话术规划和语义库自动生成这一条'"
          @click.stop="autoGenerate(entry)"
        >{{ generating[entry.id] ? '生成中…' : '自动话术生成' }}</button>
        <button
          v-if="entries.length > 1"
          type="button"
          class="recording-script-entry-remove"
          :class="{ 'is-confirming': pendingDeleteID === entry.id }"
          :data-delete-entry="entry.id"
          :data-delete-confirmation="pendingDeleteID === entry.id ? 'true' : undefined"
          :disabled="disabled"
          :aria-label="pendingDeleteID === entry.id ? `确认删除话术 ${index + 1}` : `删除话术 ${index + 1}`"
          :title="pendingDeleteID === entry.id ? '再次点击确认删除；点击其它位置取消' : `删除话术 ${index + 1}`"
          @click.stop="remove(entry, index, $event)"
        >{{ pendingDeleteID === entry.id ? '确认删除' : '删除话术' }}</button>
      </header>
      <p v-if="errors[index]" :id="`script-error-${entry.id}`" class="recording-script-entry-error" role="alert">{{ errors[index] }}</p>
      <ul v-if="riskyHits[entry.id]?.length" :id="`script-p0-${entry.id}`" class="recording-script-entry-violations" role="status">
        <li v-for="(hit, hitIndex) in riskyHits[entry.id]" :key="`${entry.id}-${hitIndex}`">
          P0 命中「{{ hit.matched_text }}」· {{ hit.category || hit.rule_id }}<template v-if="hit.note">：{{ hit.note }}</template>
        </li>
      </ul>
      <p v-if="checking[entry.id]" class="recording-script-entry-checking">正在检查 P0 词…</p>
      <p v-if="checkError[entry.id]" class="recording-script-entry-error" role="alert">P0 检查失败：{{ checkError[entry.id] }}</p>
      <p v-if="generateError[entry.id]" class="recording-script-entry-error" role="alert">自动生成失败：{{ generateError[entry.id] }}</p>
      <p v-if="generateWarning[entry.id]" class="recording-script-entry-error" role="status">{{ generateWarning[entry.id] }}</p>
      <p v-if="addError && index === entries.length - 1" class="recording-script-entry-error" role="alert">{{ addError }}</p>
      <div class="recording-script-entry-field" :class="{ 'is-risky': riskyHits[entry.id]?.length }">
        <div v-if="segments(entry)" :data-mirror="entry.id" class="recording-script-entry-mirror" aria-hidden="true">
          <template v-for="(part, partIndex) in segments(entry)!" :key="partIndex">
            <mark v-if="part.risky">{{ part.text }}</mark><span v-else>{{ part.text }}</span>
          </template>
        </div>
        <textarea
          :id="`script-entry-${entry.id}`" :value="entry.text" :disabled="disabled"
          :aria-invalid="Boolean(errors[index] || riskyHits[entry.id]?.length)" :aria-describedby="[errors[index] ? `script-error-${entry.id}` : '', riskyHits[entry.id]?.length ? `script-p0-${entry.id}` : ''].filter(Boolean).join(' ') || undefined"
          :aria-label="`话术 ${index + 1} 正文`" rows="1" spellcheck="false"
          placeholder="输入本条独立话术，至少 400 字符；本组所有话术合计不超过 4000 字符。"
          @input="input(index, $event)"
          @scroll="syncScroll(entry, $event)"
        ></textarea>
      </div>
    </section>
    <p class="recording-script-group-total">本组合计 {{ total }} / 4000 字符 · 每条至少 400 字符</p>
    <button type="button" class="recording-script-entry-add" :disabled="disabled" @click="add">＋ 添加话术</button>
  </div>
</template>

<style scoped>
.recording-script-entries, .recording-script-entries * { font-size: 20px; box-sizing: border-box; }
.recording-script-entry { margin-top: 20px; min-width: 0; }
.recording-script-entry header { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin-bottom: 10px; }
.recording-script-entry label { font-weight: 700; color: #26344c; }
.recording-script-entry header span { color: #65748b; }
.recording-script-entry header button { margin-left: 0; }
.recording-script-entry header .recording-script-entry-auto { margin-left: auto; border: 1px solid #c7d2fe; background: #eef2ff; color: #3730a3; border-radius: 10px; padding: 6px 12px; }
.recording-script-entry header .recording-script-entry-auto:hover { background: #e0e7ff; }
.recording-script-entry header .recording-script-entry-auto:disabled { background: #f4f5fb; color: #8a93a8; }
.recording-script-entry-violations { margin: 0 0 12px; padding: 12px 16px 12px 34px; border-radius: 12px; background: #fff1f2; color: #b42318; }
.recording-script-entry-violations li { list-style: disc; line-height: 1.6; }
.recording-script-entry-checking { margin: 0 0 10px; color: #65748b; }
.recording-script-entry-field { position: relative; background: #fff; border-radius: 14px; }
.recording-script-entry-field.is-risky textarea { border-color: #e5484d; box-shadow: 0 0 0 3px rgba(229,72,77,.12); }
.recording-script-entry-mirror { position: absolute; inset: 0; z-index: 2; overflow: hidden; pointer-events: none; white-space: pre-wrap; overflow-wrap: anywhere; color: #344563; padding: 18px 20px; line-height: 1.8; border: 1px solid transparent; border-radius: 14px; }
.recording-script-entry-mirror mark { background: rgba(229,72,77,.22); color: #c42c3b; border-radius: 3px; }
.recording-script-entry textarea { display: block; width: 100%; resize: none; overflow: hidden; min-height: 220px; padding: 18px 20px; border: 1px solid #dce3f5; border-radius: 14px; line-height: 1.8; color: #344563; background: white; overflow-wrap: anywhere; }
.recording-script-entry-field.is-risky textarea { color: transparent; caret-color: #344563; }
.recording-script-entry-field.is-risky textarea::selection { color: transparent; background: rgba(136,129,255,.3); }
.recording-script-entry textarea:focus { outline: 2px solid #8881ff; outline-offset: 2px; }
.recording-script-entry textarea[aria-invalid="true"] { border-color: #d4424e; }
.recording-script-entry-error { margin: 8px 0; color: #c42c3b; line-height: 1.6; overflow-wrap: anywhere; }
.recording-script-group-total { margin: 18px 0 12px; line-height: 1.6; color: #65748b; }
.recording-script-entries button { min-height: 44px; padding: 10px 18px; border: 1px solid #dddff4; border-radius: 12px; background: #f6f5ff; color: #4e45d7; cursor: pointer; }
.recording-script-entry header .recording-script-entry-remove.is-confirming { border-color: #d93842; background: #d93842; color: #fff; box-shadow: 0 5px 14px #d9384233; }
.recording-script-entry header .recording-script-entry-remove.is-confirming:hover { background: #c72f39; }
.recording-script-entries button:disabled { opacity: .55; cursor: default; }
.recording-script-entry-add { width: 100%; }
.recording-script-entry .recording-script-entry-field { position: relative; background: #fff; border-radius: 14px; }
.recording-script-entry .recording-script-entry-field > textarea { position: relative; z-index: 1; background: transparent; }
@media(max-width: 600px) { .recording-script-entry textarea { padding: 12px; } }
</style>
