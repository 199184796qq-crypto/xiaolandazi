<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { scriptCharacters, scriptEntryErrors, type ScriptEntry } from '../recordingScriptGroups'

const props = defineProps<{ entries: readonly ScriptEntry[]; disabled?: boolean }>()
const emit = defineEmits<{ update: [entries: ScriptEntry[]] }>()
const root = ref<HTMLElement | null>(null)
const addError = ref('')
const pendingDeleteID = ref<string | null>(null)
const cancelledDeleteTarget = ref<HTMLElement | null>(null)
const total = computed(() => props.entries.reduce((sum, entry) => sum + scriptCharacters(entry.text), 0))
const errors = computed(() => scriptEntryErrors(props.entries))
function cancelPendingDelete() {
  pendingDeleteID.value = null
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
      <p v-if="addError && index === entries.length - 1" class="recording-script-entry-error" role="alert">{{ addError }}</p>
      <textarea
        :id="`script-entry-${entry.id}`" :value="entry.text" :disabled="disabled"
        :aria-invalid="Boolean(errors[index])" :aria-describedby="errors[index] ? `script-error-${entry.id}` : undefined"
        :aria-label="`话术 ${index + 1} 正文`" rows="9" spellcheck="false"
        placeholder="输入本条独立话术，至少 400 字符；本组所有话术合计不超过 4000 字符。"
        @input="input(index, $event)"
      ></textarea>
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
.recording-script-entry textarea { display: block; width: 100%; resize: vertical; padding: 18px 20px; border: 1px solid #dce3f5; border-radius: 14px; line-height: 1.8; color: #344563; background: white; overflow-wrap: anywhere; }
.recording-script-entry textarea:focus { outline: 2px solid #8881ff; outline-offset: 2px; }
.recording-script-entry textarea[aria-invalid="true"] { border-color: #d4424e; }
.recording-script-entry-error { margin: 8px 0; color: #c42c3b; line-height: 1.6; overflow-wrap: anywhere; }
.recording-script-group-total { margin: 18px 0 12px; line-height: 1.6; color: #65748b; }
.recording-script-entries button { min-height: 44px; padding: 10px 18px; border: 1px solid #dddff4; border-radius: 12px; background: #f6f5ff; color: #4e45d7; cursor: pointer; }
.recording-script-entry header .recording-script-entry-remove.is-confirming { border-color: #d93842; background: #d93842; color: #fff; box-shadow: 0 5px 14px #d9384233; }
.recording-script-entry header .recording-script-entry-remove.is-confirming:hover { background: #c72f39; }
.recording-script-entries button:disabled { opacity: .55; cursor: default; }
.recording-script-entry-add { width: 100%; }
@media(max-width: 600px) { .recording-script-entry textarea { padding: 12px; } }
</style>
