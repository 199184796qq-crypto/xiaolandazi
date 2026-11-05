<script setup lang="ts">
import { computed, nextTick, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, shallowRef, watch } from 'vue'
import { appendPromptFieldsFromText, appendPromptNodesFromText, parseStructuredPrompt, removePromptField, removePromptNode, replacePromptValue, serializePromptMetadata, serializeStructuredPrompt, type StructuredPrompt } from '../structuredPrompt'

const props = withDefaults(defineProps<{ modelValue: string; metadata?: string; readonly?: boolean; inputId?: string; maxLength?: number; label?: string; resetKey?: number }>(), { readonly: false, inputId: 'plan-prompt-content', maxLength: 20000, label: '主播人设', resetKey: 0 })
const emit = defineEmits<{ change: [value: string, metadata: string]; input: [] }>()
const document = shallowRef(parseStructuredPrompt(props.modelValue, props.metadata))
const root = ref<HTMLElement | null>(null)
const addButton = ref<HTMLButtonElement | null>(null)
const rawEditor = ref<HTMLTextAreaElement | null>(null)
const textMode = ref(false)
const deleteMode = ref(false)
const rawText = ref(props.modelValue)
const compositionField = ref('')
const compositionValue = ref('')
const error = ref('')
const firstField = computed(() => document.value.nodes.flatMap(node => node.fields)[0]?.id)
interface FieldDraft { text: string; error: string; composing: boolean; blurred: boolean }
const fieldDrafts = ref<Record<string, FieldDraft>>({})
const nodeDraft = ref<FieldDraft | null>(null)
const nodeInput = ref<HTMLTextAreaElement | null>(null)
let composing: HTMLTextAreaElement | null = null
let active = true
let mounted = true
const editable = () => mounted && active && !props.readonly

function resizeValues() {
  for (const area of root.value?.querySelectorAll<HTMLTextAreaElement>('.prompt-value') || []) {
    area.style.height = 'auto'
    area.style.height = Math.max(area.scrollHeight, 60) + 'px'
  }
}

function restoreInputs() {
  rawText.value = serializeStructuredPrompt(document.value)
  for (const node of document.value.nodes) for (const field of node.fields) {
    const area = root.value?.querySelector<HTMLTextAreaElement>('[data-field-id="' + field.id + '"]')
    if (area) area.value = field.value
  }
  if (rawEditor.value) rawEditor.value.value = serializeStructuredPrompt(document.value)
  resizeValues()
}

watch(() => [props.modelValue, props.metadata, props.resetKey] as const, ([value, metadata, resetKey], previous) => {
  if (resetKey === previous[2] && value === serializeStructuredPrompt(document.value) && (metadata === undefined || metadata === serializePromptMetadata(document.value))) return
  document.value = parseStructuredPrompt(value, metadata)
  rawText.value = value
  compositionField.value = ''
  fieldDrafts.value = {}
  nodeDraft.value = null
  deleteMode.value = false
  error.value = ''
  composing = null
  void nextTick(resizeValues)
})
watch(() => props.readonly, value => {
  if (value) {
    composing = null
    compositionField.value = ''
    for (const draft of Object.values(fieldDrafts.value)) { draft.composing = false; draft.blurred = false }
    if (nodeDraft.value) { nodeDraft.value.composing = false; nodeDraft.value.blurred = false }
    deleteMode.value = false
    void nextTick(restoreInputs)
  }
})
onMounted(() => window.addEventListener('resize', resizeValues))
onActivated(() => { active = true; void nextTick(restoreInputs) })
onDeactivated(() => {
  active = false
  composing = null
  compositionField.value = ''
  for (const draft of Object.values(fieldDrafts.value)) { draft.composing = false; draft.blurred = false }
  if (nodeDraft.value) { nodeDraft.value.composing = false; nodeDraft.value.blurred = false }
  deleteMode.value = false
})
onBeforeUnmount(() => { mounted = false; window.removeEventListener('resize', resizeValues) })
void nextTick(resizeValues)

function commit(next: StructuredPrompt): boolean {
  if (!editable()) return false
  const text = serializeStructuredPrompt(next)
  if (Array.from(text).length > props.maxLength) {
    error.value = '整页内容不能超过' + props.maxLength + '字，原稿已保留。'
    return false
  }
  const metadata = serializePromptMetadata(next)
  if (new TextEncoder().encode(metadata).length > 256 * 1024) {
    error.value = '新增节点过多，原稿已保留。'
    return false
  }
  error.value = ''
  const changed = text !== serializeStructuredPrompt(document.value) || metadata !== serializePromptMetadata(document.value)
  document.value = next
  rawText.value = text
  if (changed) { emit('change', text, metadata); emit('input') }
  void nextTick(resizeValues)
  return true
}

function editValue(event: Event, fieldId: string) {
  if (!editable()) return
  const area = event.target as HTMLTextAreaElement
  if (composing === area || (event instanceof InputEvent && event.isComposing)) { compositionValue.value = area.value; return }
  const field = document.value.nodes.flatMap(node => node.fields).find(field => field.id === fieldId)
  if (!field) return
  if (area.value === field.value) return
  if (!commit(replacePromptValue(document.value, fieldId, area.value))) area.value = field.value
}

function compositionEnd(event: CompositionEvent, fieldId: string) {
  if (!editable() || composing !== event.target) return
  composing = null
  editValue(event, fieldId)
  compositionField.value = ''
}

function compositionStart(event: CompositionEvent) {
  if (!editable()) return
  composing = event.target as HTMLTextAreaElement
  compositionField.value = composing.dataset.fieldId || ''
  compositionValue.value = composing.value
}

function requestNode() {
  if (!editable()) return
  nodeDraft.value ||= { text: '', error: '', composing: false, blurred: false }
  void nextTick(() => nodeInput.value?.focus())
}

function editNodeDraft(event: Event) {
  if (!nodeDraft.value || !editable()) return
  nodeDraft.value.text = (event.target as HTMLTextAreaElement).value
  nodeDraft.value.error = ''
}

function finishNode() {
  const draft = nodeDraft.value
  if (!draft || !editable()) return
  if (draft.composing) { draft.blurred = true; return }
  if (!draft.text.trim()) { nodeDraft.value = null; return }
  try {
    if (commit(appendPromptNodesFromText(document.value, draft.text))) nodeDraft.value = null
    else draft.error = error.value
  } catch (err) { draft.error = err instanceof Error ? err.message : '未能解析，请检查节点名和键值' }
}

function nodeCompositionStart() {
  if (nodeDraft.value && editable()) nodeDraft.value.composing = true
}

function nodeCompositionEnd(event: CompositionEvent) {
  const draft = nodeDraft.value
  if (!draft || !editable() || !draft.composing) return
  draft.composing = false
  editNodeDraft(event)
  if (draft.blurred) { draft.blurred = false; finishNode() }
}

function canDelete(event: MouseEvent) {
  return editable() && deleteMode.value && !textMode.value && event.currentTarget instanceof HTMLElement && !!root.value?.contains(event.currentTarget)
}

function deleteField(fieldId: string, event: MouseEvent) {
  if (!canDelete(event)) return
  try {
    if (commit(removePromptField(document.value, fieldId))) { composing = null; compositionField.value = '' }
  } catch (err) { error.value = err instanceof Error ? err.message : '删除未完成' }
}

function deleteNode(nodeId: string, event: MouseEvent) {
  if (!canDelete(event)) return
  try {
    if (!commit(removePromptNode(document.value, nodeId))) return
    delete fieldDrafts.value[nodeId]
    composing = null
    compositionField.value = ''
  } catch (err) { error.value = err instanceof Error ? err.message : '删除未完成' }
}

function toggleDeleteMode() {
  if (editable() && !textMode.value) deleteMode.value = !deleteMode.value
}

function requestFields(nodeId: string) {
  if (!editable()) return
  fieldDrafts.value[nodeId] ||= { text: '', error: '', composing: false, blurred: false }
  void nextTick(() => root.value?.querySelector<HTMLTextAreaElement>('[data-draft-node="' + nodeId + '"]')?.focus())
}

function editDraft(event: Event, nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (!draft || !editable()) return
  draft.text = (event.target as HTMLTextAreaElement).value
  draft.error = ''
}

function finishFields(nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (!draft || !editable()) return
  if (draft.composing) { draft.blurred = true; return }
  if (!draft.text.trim()) { delete fieldDrafts.value[nodeId]; return }
  try {
    const next = appendPromptFieldsFromText(document.value, nodeId, draft.text)
    if (commit(next)) delete fieldDrafts.value[nodeId]
    else draft.error = error.value
  } catch (err) { draft.error = err instanceof Error ? err.message : '未能解析，请检查键名和内容' }
}

function draftCompositionStart(nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (draft && editable()) draft.composing = true
}

function draftCompositionEnd(event: CompositionEvent, nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (!draft || !editable() || !draft.composing) return
  draft.composing = false
  editDraft(event, nodeId)
  if (draft.blurred) { draft.blurred = false; finishFields(nodeId) }
}

function focus() {
  const area = rawEditor.value || root.value?.querySelector<HTMLTextAreaElement>('.prompt-value')
  if (area) area.focus({ preventScroll: true })
  else addButton.value?.focus({ preventScroll: true })
}

function editRaw(event: Event) {
  if (!editable()) return
  const area = event.target as HTMLTextAreaElement
  if (composing === area || (event instanceof InputEvent && event.isComposing)) { rawText.value = area.value; return }
  const original = serializeStructuredPrompt(document.value)
  if (area.value === original) return
  if (!commit(parseStructuredPrompt(area.value))) { rawText.value = original; area.value = original; return }
  fieldDrafts.value = {}
  nodeDraft.value = null
  deleteMode.value = false
}

function rawCompositionEnd(event: CompositionEvent) {
  if (!editable() || composing !== event.target) return
  composing = null
  editRaw(event)
}

async function toggleMode() {
  if (!editable()) return
  if (composing || nodeDraft.value?.composing || Object.values(fieldDrafts.value).some(draft => draft.composing)) {
    error.value = '请先完成当前输入，再切换显示方式。'
    return
  }
  if (!textMode.value) {
    for (const nodeId of Object.keys(fieldDrafts.value)) finishFields(nodeId)
    finishNode()
    if (nodeDraft.value || Object.keys(fieldDrafts.value).length) {
      error.value = '新增内容尚未添加，请修正或清空后再切换。'
      return
    }
  }
  deleteMode.value = false
  error.value = ''
  textMode.value = !textMode.value
  await nextTick()
  if (editable()) { resizeValues(); focus() }
}
defineExpose({ focus })
</script>

<template>
  <div ref="root" class="structured-prompt" :class="{ 'deletion-mode': deleteMode }" role="group" :aria-label="label + '编辑器'" :aria-disabled="readonly">
    <div class="structure-toolbar">
      <button type="button" class="edit-model" :aria-pressed="textMode" :disabled="readonly" @click="toggleMode">{{ textMode ? '结构化显示' : '编辑模型' }}</button>
      <button v-if="!textMode" ref="addButton" type="button" class="add-node" :disabled="readonly" @click="requestNode">＋ 添加节点</button>
      <button v-if="!textMode" type="button" class="delete-mode-toggle" :aria-pressed="deleteMode" :disabled="readonly" @click="toggleDeleteMode">{{ deleteMode ? '完成删除' : '删除模式' }}</button>
    </div>
    <p v-if="error" class="structure-error" role="alert">{{ error }}</p>
    <textarea v-if="textMode" :id="inputId" ref="rawEditor" class="prompt-raw" :aria-label="label + '完整内容'" :value="rawText" :readonly="readonly" :maxlength="maxLength" rows="18" spellcheck="false" @input="editRaw" @compositionstart="compositionStart" @compositionend="rawCompositionEnd"></textarea>
    <template v-else>
    <section v-for="node in document.nodes" :key="node.id" class="prompt-node" :data-node-id="node.id" :aria-label="node.name + '节点'">
      <header class="prompt-node-heading"><div class="node-title"><h4><span aria-hidden="true">▧</span> {{ node.name }}</h4></div><div class="node-actions"><button type="button" :disabled="readonly" :aria-label="'给' + node.name + '添加键值'" @click="requestFields(node.id)">＋ 添加键值</button></div><button v-if="deleteMode" type="button" class="delete-badge node-delete-badge" :disabled="readonly" :aria-label="'删除' + node.name + '节点'" :title="'删除' + node.name + '节点'" @click.stop="deleteNode(node.id, $event)"><span aria-hidden="true">×</span></button></header>
      <div v-for="field in node.fields" :key="field.id" class="prompt-field">
        <div class="prompt-key-column"><label class="prompt-key" :class="{ 'body-key': field.key === null }" :for="firstField === field.id ? inputId : inputId + '-' + field.id"><span>{{ field.key ?? '正文' }}</span></label></div>
        <button v-if="deleteMode" type="button" class="delete-badge field-delete-badge" :disabled="readonly" :aria-label="'删除' + node.name + ' / ' + (field.key === null ? '正文' : field.key + '键值')" :title="field.key === null ? '删除这段正文' : '删除' + field.key + '及其内容'" @click.stop="deleteField(field.id, $event)"><span aria-hidden="true">×</span></button>
        <textarea :id="firstField === field.id ? inputId : inputId + '-' + field.id" :data-field-id="field.id" :aria-label="node.name + ' / ' + (field.key ?? '正文') + '的值'" :value="compositionField === field.id ? compositionValue : field.value" :readonly="readonly" :maxlength="maxLength" rows="1" class="prompt-value" spellcheck="false" @input="editValue($event, field.id)" @compositionstart="compositionStart" @compositionend="compositionEnd($event, field.id)"></textarea>
      </div>
      <div v-if="fieldDrafts[node.id]" class="field-composer">
        <label :for="inputId + '-draft-' + node.id">新增键值</label>
        <textarea :id="inputId + '-draft-' + node.id" :data-draft-node="node.id" :aria-label="node.name + ' / 新增键值'" :value="fieldDrafts[node.id]!.text" :readonly="readonly" :maxlength="maxLength" :aria-invalid="!!fieldDrafts[node.id]!.error" :aria-describedby="inputId + '-draft-help-' + node.id + (fieldDrafts[node.id]!.error ? ' ' + inputId + '-draft-error-' + node.id : '')" rows="3" placeholder="例如：语速：中等偏快&#10;停顿：句尾自然停顿" spellcheck="false" @input="editDraft($event, node.id)" @blur="finishFields(node.id)" @compositionstart="draftCompositionStart(node.id)" @compositionend="draftCompositionEnd($event, node.id)"></textarea>
        <p :id="inputId + '-draft-help-' + node.id" class="draft-help">每行填写“键名：内容”，中英文冒号均可。离开输入框自动添加，名称随后锁定。多行内容的续行前加4个空格或Tab。</p>
        <p v-if="fieldDrafts[node.id]!.error" :id="inputId + '-draft-error-' + node.id" class="structure-error" role="alert">未添加，输入已保留：{{ fieldDrafts[node.id]!.error }}</p>
      </div>
      <p v-else-if="!node.fields.length" class="empty-node">点击“添加键值”，直接在这里填写。</p>
    </section>
    <div v-if="nodeDraft" class="node-composer field-composer">
      <label :for="inputId + '-new-node'">新增节点</label>
      <textarea :id="inputId + '-new-node'" ref="nodeInput" aria-label="新增节点内容" :value="nodeDraft.text" :readonly="readonly" :maxlength="maxLength" :aria-invalid="!!nodeDraft.error" :aria-describedby="inputId + '-node-help' + (nodeDraft.error ? ' ' + inputId + '-node-error' : '')" rows="4" placeholder="表达习惯&#10;语气：轻松自然&#10;停顿：句尾自然停顿" spellcheck="false" @input="editNodeDraft" @blur="finishNode" @compositionstart="nodeCompositionStart" @compositionend="nodeCompositionEnd"></textarea>
      <p :id="inputId + '-node-help'" class="draft-help">首行填写节点名，下方可同时填写“键名：内容”；也支持【节点名】。离开输入框自动添加，节点可以整组删除。</p>
      <p v-if="nodeDraft.error" :id="inputId + '-node-error'" class="structure-error" role="alert">未添加，输入已保留：{{ nodeDraft.error }}</p>
    </div>
    </template>
  </div>
</template>

<style scoped>
.structured-prompt{display:grid;gap:14px;min-width:0;padding:20px;border:1px solid #dedff1;border-radius:16px;background:#fbfcff;color:#26324d;font-size:18px;line-height:1.65}
.structure-toolbar{display:flex;flex-wrap:wrap;gap:12px;align-items:center;justify-content:flex-end}
.structured-prompt button{box-sizing:border-box;min-height:42px;border:1px solid #ddd9f6;border-radius:10px;padding:8px 13px;background:white;color:#5b48c4;font:inherit;font-size:16px;font-weight:650;cursor:pointer}.structured-prompt button:disabled{opacity:.5;cursor:default}.structured-prompt button:focus-visible{outline:3px solid #c7bfff;outline-offset:2px}.structure-toolbar .add-node{flex:none;background:#6554ec;color:white;border-color:#6554ec}
.prompt-node{overflow:hidden;min-width:0;border:1px solid #e1dcf2;border-radius:12px;background:white}.prompt-node-heading{position:relative;display:flex;align-items:center;justify-content:space-between;gap:16px;padding:12px 16px;background:#f3effd;border-bottom:1px solid #e8e2f7}.prompt-node-heading h4{min-width:0;margin:0;font-size:20px;font-weight:700;color:#6038a5;overflow-wrap:anywhere}.prompt-node-heading h4>span{margin-right:5px;color:#9274c8}.prompt-node-heading button{flex:none;min-height:36px;padding:5px 10px;font-size:16px;background:#fcfaff}
.prompt-field{position:relative;display:grid;grid-template-columns:minmax(100px,170px) minmax(0,1fr);gap:14px;padding:14px 16px;align-items:start}.prompt-field+.prompt-field{border-top:1px solid #eff1f7}.prompt-key{display:grid;gap:4px;min-width:0;padding-top:7px;color:#1d5c98;font-size:18px;font-weight:650;overflow-wrap:anywhere}.prompt-key.body-key{color:#63758d}
.field-composer{display:grid;gap:8px;padding:14px 16px;border-top:1px dashed #dcd7f3;background:#fcfbff}.field-composer>label{color:#1d5c98;font-size:16px;font-weight:650}.draft-help{margin:0;color:#78869e;font-size:16px;overflow-wrap:anywhere}
.prompt-key-column{display:grid;gap:8px;min-width:0;justify-items:start}
.node-title{min-width:0}.node-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}.node-composer{border:1px dashed #c5b6ee;border-radius:12px}
.structured-prompt textarea{box-sizing:border-box;display:block;width:100%;min-width:0;max-width:100%;padding:10px 12px;border:1px solid #e2e7f1;border-radius:9px;background:white;color:#26324d;font:inherit;font-size:20px;line-height:1.7;outline:none;resize:vertical}.structured-prompt .prompt-value{min-height:60px;overflow:hidden;resize:none}.structured-prompt textarea:focus{border-color:#9b8cf0;box-shadow:0 0 0 3px #8270ed12}.structured-prompt textarea[readonly]{background:#f7f8fc;color:#6c7890}.empty-node{margin:0;padding:17px;color:#8b95a7;font-size:16px}.structured-prompt .prompt-raw{min-height:460px;padding:18px;resize:vertical}
.structure-error{margin:0;padding:10px 12px;border-radius:9px;background:#fff2f4;color:#a43c55;font-size:16px;overflow-wrap:anywhere}
.structure-toolbar .delete-mode-toggle{color:#bd334c;border-color:#f0cdd5;background:#fff7f9}.structure-toolbar .delete-mode-toggle[aria-pressed=true]{color:white;background:#d63e59;border-color:#d63e59}
.deletion-mode .prompt-node-heading,.deletion-mode .prompt-field{padding-right:64px}
.structured-prompt .delete-badge{position:absolute;z-index:1;top:10px;right:12px;display:grid;place-items:center;width:36px;min-width:36px;height:36px;min-height:36px;padding:0;border:2px solid white;border-radius:50%;background:#e1465f;color:white;line-height:1;box-shadow:0 2px 7px #b7294540;transform-origin:50% 50%;animation:prompt-delete-wiggle .24s ease-in-out infinite alternate}
.delete-badge>span{font-size:25px;font-weight:500;line-height:1}.structured-prompt .delete-badge:hover{background:#c62e49}.structured-prompt .delete-badge:focus-visible{outline-color:#d63e59}.structured-prompt .delete-badge:disabled{animation:none}
@keyframes prompt-delete-wiggle{from{transform:rotate(-8deg)}to{transform:rotate(8deg)}}
@media(prefers-reduced-motion:reduce){.structured-prompt .delete-badge{animation:none}}

@media(max-width:600px){.structured-prompt{padding:12px;gap:12px}.structure-toolbar{flex-wrap:wrap;gap:10px}.prompt-field{grid-template-columns:1fr;gap:6px;padding:12px}.prompt-key{display:flex;flex-wrap:wrap;align-items:center;gap:10px;padding:0}.prompt-node-heading{padding:11px 12px;gap:8px}.prompt-node-heading h4{font-size:18px}}
</style>
