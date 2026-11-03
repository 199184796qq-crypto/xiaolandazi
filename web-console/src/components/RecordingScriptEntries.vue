<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { annotatePlanText, type PlanTextRiskHit } from '../api'
import { scriptCharacters, scriptEntryErrors, type ScriptEntry } from '../recordingScriptGroups'

const props = defineProps<{ entries: readonly ScriptEntry[]; disabled?: boolean; planId?: number | null }>()
const emit = defineEmits<{ update: [entries: ScriptEntry[]]; violations: [count: number] }>()
const root = ref<HTMLElement | null>(null)
const addError = ref('')
const pendingDeleteID = ref<string | null>(null)
const cancelledDeleteTarget = ref<HTMLElement | null>(null)
const total = computed(() => props.entries.reduce((sum, entry) => sum + scriptCharacters(entry.text), 0))
const errors = computed(() => scriptEntryErrors(props.entries))
function cancelPendingDelete() {
  pendingDeleteID.value = null
}
// 按当前 P0 词表检查话术片。只作字面命中提示，不会改写正文或阻止保存。
const riskyHits = ref<Record<string, PlanTextRiskHit[]>>({})
const checking = ref<Record<string, boolean>>({})
const violationCount = computed(() => Object.values(riskyHits.value).reduce((sum, hits) => sum + hits.length, 0))
watch(violationCount, count => emit('violations', count), { immediate: true })
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

async function checkEntry(entry: ScriptEntry) {
  const planId = props.planId
  const text = entry.text.trim()
  if (!planId) {
    checkError.value = { ...checkError.value, [entry.id]: '未取到方案 ID，无法检测违规' }
    return
  }
  if (!text) {
    const next = { ...riskyHits.value }; delete next[entry.id]; riskyHits.value = next
    return
  }
  checking.value = { ...checking.value, [entry.id]: true }
  const nextError = { ...checkError.value }; delete nextError[entry.id]; checkError.value = nextError
  try {
    const result = await annotatePlanText(planId, text)
    const next = { ...riskyHits.value }
    const hits = mapHits(entry.text, text, result.hits ?? [])
    if (hits.length) next[entry.id] = hits
    else delete next[entry.id]
    riskyHits.value = next
  } catch (err) {
    checkError.value = { ...checkError.value, [entry.id]: err instanceof Error ? err.message : '违规检测失败' }
  } finally {
    checking.value = { ...checking.value, [entry.id]: false }
  }
}
const checkError = ref<Record<string, string>>({})
function mapHits(original: string, submitted: string, hits: PlanTextRiskHit[]) {
  const chars = Array.from(original)
  const trimmed = Array.from(submitted)
  const out: PlanTextRiskHit[] = []
  for (const hit of hits) {
    const matched = (hit.matched_text || '').trim()
    if (!matched) continue
    const candidate = trimmed.slice(hit.original_start, hit.original_end).join('')
    const offset = chars.length - trimmed.length
    if (candidate === hit.matched_text) {
      out.push({ ...hit, original_start: hit.original_start + offset, original_end: hit.original_end + offset })
      continue
    }
    const start = chars.join('').indexOf(matched)
    if (start < 0) continue
    out.push({ ...hit, original_start: start, original_end: start + Array.from(matched).length })
  }
  return out
}

// autosize 让文本框高度跟着正文走，不出现内部滚动条。
function autosize(el: HTMLTextAreaElement | null | undefined) {
  if (!el) return
  const border = el.offsetHeight - el.clientHeight
  const height = el.scrollHeight + border
  if (Math.abs(height - el.offsetHeight) <= 1) return
  el.style.height = `${height}px`
}
function autosizeAll() {
  const editors = root.value?.querySelectorAll<HTMLTextAreaElement>('textarea')
  editors?.forEach(autosize)
}
onMounted(() => nextTick(autosizeAll))
watch(() => props.entries, () => nextTick(autosizeAll), { deep: true })
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
  const entry = props.entries[index]
  if (entry && riskyHits.value[entry.id]) {
    const next = { ...riskyHits.value }; delete next[entry.id]; riskyHits.value = next
  }
  emit('update', props.entries.map((entry, i) => i === index ? { ...entry, text: (event.target as HTMLTextAreaElement).value } : entry))
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
onBeforeUnmount(() => document.removeEventListener('pointerdown', cancelDeleteFromOutside, true))
</script>

<template>
  <div ref="root" class="recording-script-entries" @keydown.esc="cancelPendingDelete">
    <p class="recording-script-group-total">本组合计 {{ total }} / 4000 字符 · 每条至少 400 字符</p>
    <section v-for="(entry, index) in entries" :key="entry.id" class="recording-script-entry">
      <header>
        <label :for="`script-entry-${entry.id}`">话术 {{ index + 1 }}</label>
        <span>{{ scriptCharacters(entry.text) }} 字符</span>
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
      <ul v-if="riskyHits[entry.id]?.length" class="recording-script-entry-violations" role="alert">
        <li v-for="(hit, hitIndex) in riskyHits[entry.id]" :key="`${entry.id}-${hitIndex}`">
          命中「{{ hit.matched_text }}」· {{ hit.category || hit.rule_id }}<template v-if="hit.note">：{{ hit.note }}</template>
        </li>
      </ul>
      <p v-if="checking[entry.id]" class="recording-script-entry-checking">正在检测违规…</p>
      <p v-if="checkError[entry.id]" class="recording-script-entry-error" role="alert">违规检测失败：{{ checkError[entry.id] }}</p>
      <p v-if="addError && index === entries.length - 1" class="recording-script-entry-error" role="alert">{{ addError }}</p>
      <div class="recording-script-entry-field" :class="{ 'is-risky': riskyHits[entry.id]?.length }">
        <div v-if="segments(entry)" :data-mirror="entry.id" class="recording-script-entry-mirror" aria-hidden="true">
          <template v-for="(part, partIndex) in segments(entry)!" :key="partIndex">
            <mark v-if="part.risky">{{ part.text }}</mark><span v-else>{{ part.text }}</span>
          </template>
        </div>
        <textarea
          :id="`script-entry-${entry.id}`" :value="entry.text" :disabled="disabled"
          :aria-invalid="Boolean(errors[index] || riskyHits[entry.id]?.length)" :aria-describedby="errors[index] ? `script-error-${entry.id}` : undefined"
          :aria-label="`话术 ${index + 1} 正文`" rows="1" spellcheck="false"
          placeholder="输入本条独立话术，至少 400 字符；本组所有话术合计不超过 4000 字符。"
          @input="input(index, $event)"
          @blur="checkEntry(entry)"
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
.recording-script-entry header button { margin-left: auto; }
.recording-script-entry-violations { margin: 0 0 12px; padding: 12px 16px 12px 34px; border-radius: 12px; background: #fff1f2; color: #b42318; }
.recording-script-entry-violations li { list-style: disc; line-height: 1.6; }
.recording-script-entry-checking { margin: 0 0 10px; color: #65748b; }
.recording-script-entry-field { position: relative; background: #fff; border-radius: 14px; }
.recording-script-entry-field.is-risky textarea { border-color: #e5484d; box-shadow: 0 0 0 3px rgba(229,72,77,.12); }
.recording-script-entry-mirror { position: absolute; inset: 0; overflow: hidden; pointer-events: none; white-space: pre-wrap; overflow-wrap: anywhere; color: transparent; padding: 18px 20px; line-height: 1.8; border: 1px solid transparent; border-radius: 14px; }
.recording-script-entry-mirror mark { background: rgba(229,72,77,.22); color: transparent; border-radius: 3px; }
.recording-script-entry textarea { display: block; width: 100%; resize: none; overflow: hidden; min-height: 220px; padding: 18px 20px; border: 1px solid #dce3f5; border-radius: 14px; line-height: 1.8; color: #344563; background: white; overflow-wrap: anywhere; }
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
.recording-script-entry .recording-script-entry-field > .recording-script-entry-mirror { position: absolute; inset: 0; z-index: 0; overflow: hidden; pointer-events: none; white-space: pre-wrap; overflow-wrap: anywhere; color: transparent; padding: 18px 20px; line-height: 1.8; border: 1px solid transparent; border-radius: 14px; }
.recording-script-entry .recording-script-entry-mirror mark { background: rgba(229,72,77,.28); color: transparent; border-radius: 3px; }
@media(max-width: 600px) { .recording-script-entry textarea { padding: 12px; } }
</style>
