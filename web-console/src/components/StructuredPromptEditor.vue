<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { appendPromptFieldsFromText, appendPromptNodesFromText, parseStructuredPrompt, removePromptField, removePromptNode, replacePromptValue, serializePromptMetadata, serializeStructuredPrompt, type StructuredPrompt } from '../structuredPrompt'

const props = withDefaults(defineProps<{ modelValue: string; metadata?: string; readonly?: boolean; inputId?: string; maxLength?: number }>(), { metadata: '', readonly: false, inputId: 'plan-prompt-content', maxLength: 20000 })
const emit = defineEmits<{ change: [value: string, metadata: string]; input: [] }>()
const document = shallowRef(parseStructuredPrompt(props.modelValue, props.metadata))
const root = ref<HTMLElement | null>(null)
const addButton = ref<HTMLButtonElement | null>(null)
const error = ref('')
const firstField = computed(() => document.value.nodes.flatMap(node => node.fields)[0]?.id)
interface FieldDraft { text: string; error: string; composing: boolean; blurred: boolean }
const fieldDrafts = ref<Record<string, FieldDraft>>({})
const nodeDraft = ref<FieldDraft | null>(null)
const nodeInput = ref<HTMLTextAreaElement | null>(null)
const deletingNode = ref('')
const deletingField = ref('')
let composing: HTMLTextAreaElement | null = null

function resizeValues() {
  for (const area of root.value?.querySelectorAll<HTMLTextAreaElement>('.prompt-value') || []) {
    area.style.height = 'auto'
    area.style.height = Math.max(area.scrollHeight, 60) + 'px'
  }
}

watch(() => [props.modelValue, props.metadata] as const, ([value, metadata]) => {
  if (value === serializeStructuredPrompt(document.value) && metadata === serializePromptMetadata(document.value)) return
  document.value = parseStructuredPrompt(value, metadata)
  fieldDrafts.value = {}
  nodeDraft.value = null
  deletingNode.value = ''
  deletingField.value = ''
  error.value = ''
  composing = null
  void nextTick(resizeValues)
})
watch(() => props.readonly, value => {
  if (value) {
    composing = null
    for (const draft of Object.values(fieldDrafts.value)) { draft.composing = false; draft.blurred = false }
    if (nodeDraft.value) { nodeDraft.value.composing = false; nodeDraft.value.blurred = false }
    deletingNode.value = ''
    deletingField.value = ''
    void nextTick(() => {
      for (const node of document.value.nodes) for (const field of node.fields) {
        const area = root.value?.querySelector<HTMLTextAreaElement>('[data-field-id="' + field.id + '"]')
        if (area) area.value = field.value
      }
      resizeValues()
    })
  }
})
onMounted(() => window.addEventListener('resize', resizeValues))
onBeforeUnmount(() => window.removeEventListener('resize', resizeValues))
void nextTick(resizeValues)

function commit(next: StructuredPrompt): boolean {
  if (props.readonly) return false
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
  document.value = next
  if (text !== props.modelValue || metadata !== props.metadata) { emit('change', text, metadata); emit('input') }
  void nextTick(resizeValues)
  return true
}

function editValue(event: Event, fieldId: string) {
  const area = event.target as HTMLTextAreaElement
  if (composing === area || (event instanceof InputEvent && event.isComposing)) return
  const field = document.value.nodes.flatMap(node => node.fields).find(field => field.id === fieldId)
  if (!field) return
  if (area.value === field.value) return
  if (!commit(replacePromptValue(document.value, fieldId, area.value))) area.value = field.value
}

function compositionEnd(event: CompositionEvent, fieldId: string) {
  composing = null
  editValue(event, fieldId)
}

function compositionStart(event: CompositionEvent) {
  composing = event.target as HTMLTextAreaElement
}

function requestNode() {
  if (props.readonly) return
  nodeDraft.value ||= { text: '', error: '', composing: false, blurred: false }
  void nextTick(() => nodeInput.value?.focus())
}

function editNodeDraft(event: Event) {
  if (!nodeDraft.value || props.readonly) return
  nodeDraft.value.text = (event.target as HTMLTextAreaElement).value
  nodeDraft.value.error = ''
}

function finishNode() {
  const draft = nodeDraft.value
  if (!draft || props.readonly) return
  if (draft.composing) { draft.blurred = true; return }
  if (!draft.text.trim()) { nodeDraft.value = null; return }
  try {
    if (commit(appendPromptNodesFromText(document.value, draft.text))) nodeDraft.value = null
    else draft.error = error.value
  } catch (err) { draft.error = err instanceof Error ? err.message : '未能解析，请检查节点名和键值' }
}

function nodeCompositionStart() {
  if (nodeDraft.value && !props.readonly) nodeDraft.value.composing = true
}

function nodeCompositionEnd(event: CompositionEvent) {
  const draft = nodeDraft.value
  if (!draft || props.readonly) return
  draft.composing = false
  editNodeDraft(event)
  if (draft.blurred) { draft.blurred = false; finishNode() }
}

function requestDelete(nodeId: string) {
  if (!props.readonly && document.value.nodes.some(node => node.id === nodeId)) { deletingField.value = ''; deletingNode.value = nodeId }
}

function requestDeleteField(fieldId: string) {
  const field = document.value.nodes.flatMap(node => node.fields).find(field => field.id === fieldId)
  if (!props.readonly && field) { deletingNode.value = ''; deletingField.value = fieldId }
}

function deleteField(fieldId: string) {
  if (props.readonly) return
  try {
    if (commit(removePromptField(document.value, fieldId))) deletingField.value = ''
  } catch (err) { error.value = err instanceof Error ? err.message : '删除未完成' }
}

function deleteNode(nodeId: string) {
  if (props.readonly) return
  try {
    if (!commit(removePromptNode(document.value, nodeId))) return
    delete fieldDrafts.value[nodeId]
    deletingNode.value = ''
  } catch (err) { error.value = err instanceof Error ? err.message : '删除未完成' }
}

function requestFields(nodeId: string) {
  if (props.readonly) return
  fieldDrafts.value[nodeId] ||= { text: '', error: '', composing: false, blurred: false }
  void nextTick(() => root.value?.querySelector<HTMLTextAreaElement>('[data-draft-node="' + nodeId + '"]')?.focus())
}

function editDraft(event: Event, nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (!draft || props.readonly) return
  draft.text = (event.target as HTMLTextAreaElement).value
  draft.error = ''
}

function finishFields(nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (!draft || props.readonly) return
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
  if (draft && !props.readonly) draft.composing = true
}

function draftCompositionEnd(event: CompositionEvent, nodeId: string) {
  const draft = fieldDrafts.value[nodeId]
  if (!draft || props.readonly) return
  draft.composing = false
  editDraft(event, nodeId)
  if (draft.blurred) { draft.blurred = false; finishFields(nodeId) }
}

function focus() {
  const area = root.value?.querySelector<HTMLTextAreaElement>('.prompt-value')
  if (area) area.focus({ preventScroll: true })
  else addButton.value?.focus({ preventScroll: true })
}
defineExpose({ focus })
</script>

<template>
  <div ref="root" class="structured-prompt" role="group" aria-label="主播人设结构化编辑（测试）" :aria-disabled="readonly">
    <div class="structure-toolbar">
      <div class="structure-guide"><span class="structure-test">结构化编辑 · 测试</span><p>节点和键值均可删除，名称仍锁定，内容可修改或清空。新增内容离开输入框后自动识别。</p></div>
      <button ref="addButton" type="button" class="add-node" :disabled="readonly" @click="requestNode">＋ 添加节点</button>
    </div>
    <div class="structure-legend" aria-label="颜色说明"><span class="node-legend">节点名称</span><span class="key-legend">键名</span><span>值可编辑</span></div>
    <p v-if="error" class="structure-error" role="alert">{{ error }}</p>
    <section v-for="node in document.nodes" :key="node.id" class="prompt-node" :data-node-id="node.id" :aria-label="node.name + '节点'">
      <header class="prompt-node-heading"><div class="node-title"><h4><span aria-hidden="true">▧</span> {{ node.name }}</h4><small>{{ node.origin === 'user' ? '自己添加' : '原始节点 · 名称锁定' }}</small></div><div class="node-actions"><button type="button" :disabled="readonly" :aria-label="'给' + node.name + '添加键值'" @click="requestFields(node.id)">＋ 添加键值</button><button type="button" class="delete-node" :disabled="readonly" :aria-label="'删除' + node.name + '节点'" @click="requestDelete(node.id)">删除节点</button></div></header>
      <div v-if="deletingNode === node.id" class="node-delete-confirm" role="group" :aria-label="'确认删除' + node.name"><span>删除「{{ node.name }}」及其中全部键值？</span><div><button type="button" :disabled="readonly" @click="deletingNode = ''">取消删除</button><button type="button" class="delete-node" :disabled="readonly" @click="deleteNode(node.id)">确认删除节点</button></div></div>
      <div v-for="field in node.fields" :key="field.id" class="prompt-field">
        <div class="prompt-key-column"><label class="prompt-key" :class="{ 'body-key': field.key === null }" :for="firstField === field.id ? inputId : inputId + '-' + field.id"><span>{{ field.key ?? '正文' }}</span><small>{{ field.key === null ? '正文可编辑' : field.origin === 'user' ? '自己添加 · 键名锁定' : '键名锁定' }}</small></label><button type="button" class="delete-field" :disabled="readonly" :aria-label="'删除' + node.name + ' / ' + (field.key === null ? '正文' : field.key + '键值')" @click="requestDeleteField(field.id)">{{ field.key === null ? '删除正文' : '删除键值' }}</button></div>
        <textarea :id="firstField === field.id ? inputId : inputId + '-' + field.id" :data-field-id="field.id" :aria-label="node.name + ' / ' + (field.key ?? '正文') + '的值'" :value="field.value" :readonly="readonly" :maxlength="maxLength" rows="1" class="prompt-value" spellcheck="false" @input="editValue($event, field.id)" @compositionstart="compositionStart" @compositionend="compositionEnd($event, field.id)"></textarea>
        <div v-if="deletingField === field.id" class="field-delete-confirm" role="group" :aria-label="'确认删除' + node.name + ' / ' + (field.key ?? '正文')"><span>{{ field.key === null ? '删除这段正文？' : '删除「' + field.key + '」及其内容？' }}</span><div><button type="button" :disabled="readonly" @click="deletingField = ''">{{ field.key === null ? '取消删除正文' : '取消删除键值' }}</button><button type="button" class="delete-field" :disabled="readonly" @click="deleteField(field.id)">{{ field.key === null ? '确认删除正文' : '确认删除键值' }}</button></div></div>
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
    <details class="structure-original"><summary>查看完整原文（只读）</summary><textarea :value="modelValue" readonly rows="8" aria-label="主播人设完整原文（只读）"></textarea></details>
  </div>
</template>

<style scoped>
.structured-prompt{display:grid;gap:14px;min-width:0;padding:20px;border:1px solid #dedff1;border-radius:16px;background:#fbfcff;color:#26324d;font-size:18px;line-height:1.65}
.structure-toolbar{display:flex;gap:16px;align-items:flex-start;justify-content:space-between}.structure-guide{min-width:0}.structure-test{font-size:16px;font-weight:700;color:#6250b6}.structure-guide p{margin:5px 0 0;color:#738097;font-size:16px}.structure-legend{display:flex;flex-wrap:wrap;gap:10px;font-size:16px;color:#69768d}.structure-legend>span{padding:3px 10px;border:1px solid #e2e7f2;border-radius:20px;background:white}.structure-legend .node-legend{color:#633ca9;background:#f2ecff;border-color:#e2d6fb}.structure-legend .key-legend{color:#1e5e9b;background:#eef6ff;border-color:#d9e9f8}
.structured-prompt button,.structure-dialog button{box-sizing:border-box;min-height:42px;border:1px solid #ddd9f6;border-radius:10px;padding:8px 13px;background:white;color:#5b48c4;font:inherit;font-size:16px;font-weight:650;cursor:pointer}.structured-prompt button:disabled,.structure-dialog button:disabled{opacity:.5;cursor:default}.structured-prompt button:focus-visible,.structure-dialog button:focus-visible{outline:3px solid #c7bfff;outline-offset:2px}.structure-toolbar .add-node{flex:none;background:#6554ec;color:white;border-color:#6554ec}
.prompt-node{overflow:hidden;min-width:0;border:1px solid #e1dcf2;border-radius:12px;background:white}.prompt-node-heading{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:12px 16px;background:#f3effd;border-bottom:1px solid #e8e2f7}.prompt-node-heading h4{min-width:0;margin:0;font-size:20px;font-weight:700;color:#6038a5;overflow-wrap:anywhere}.prompt-node-heading h4>span{margin-right:5px;color:#9274c8}.prompt-node-heading button{flex:none;min-height:36px;padding:5px 10px;font-size:16px;background:#fcfaff}
.prompt-field{display:grid;grid-template-columns:minmax(100px,170px) minmax(0,1fr);gap:14px;padding:14px 16px;align-items:start}.prompt-field+.prompt-field{border-top:1px solid #eff1f7}.prompt-key{display:grid;gap:4px;min-width:0;padding-top:7px;color:#1d5c98;font-size:18px;font-weight:650;overflow-wrap:anywhere}.prompt-key small{font-size:16px;font-weight:400;color:#8c98aa}.prompt-key.body-key{color:#63758d}
.field-composer{display:grid;gap:8px;padding:14px 16px;border-top:1px dashed #dcd7f3;background:#fcfbff}.field-composer>label{color:#1d5c98;font-size:16px;font-weight:650}.draft-help{margin:0;color:#78869e;font-size:16px;overflow-wrap:anywhere}
.prompt-key-column{display:grid;gap:8px;min-width:0;justify-items:start}.prompt-key-column .prompt-key small{font-size:16px}.structured-prompt .delete-field{color:#ac3c57;border-color:#f0d1d9;background:#fff8fa;font-size:16px}.field-delete-confirm{grid-column:1/-1;display:flex;flex-wrap:wrap;gap:10px;align-items:center;justify-content:space-between;min-width:0;padding:10px 12px;border-radius:9px;background:#fff5f7;color:#a43c55;font-size:16px;overflow-wrap:anywhere}.field-delete-confirm>div{display:flex;gap:8px;flex-wrap:wrap}
.node-title{min-width:0}.node-title>small{color:#8b79ab;font-size:16px}.node-actions{display:flex;gap:8px;flex-wrap:wrap;justify-content:flex-end}.node-actions .delete-node,.node-delete-confirm .delete-node{color:#ac3c57;border-color:#f0d1d9;background:#fff8fa}.node-composer{border:1px dashed #c5b6ee;border-radius:12px}.node-delete-confirm{display:flex;flex-wrap:wrap;gap:10px;align-items:center;justify-content:space-between;padding:12px 16px;background:#fff5f7;color:#a43c55;font-size:16px}.node-delete-confirm>div{display:flex;gap:8px;flex-wrap:wrap}
.structured-prompt textarea,.structure-dialog textarea{box-sizing:border-box;display:block;width:100%;min-width:0;max-width:100%;padding:10px 12px;border:1px solid #e2e7f1;border-radius:9px;background:white;color:#26324d;font:inherit;font-size:20px;line-height:1.7;outline:none;resize:vertical}.structured-prompt .prompt-value{min-height:60px;overflow:hidden;resize:none}.structured-prompt textarea:focus,.structure-dialog input:focus,.structure-dialog textarea:focus{border-color:#9b8cf0;box-shadow:0 0 0 3px #8270ed12}.structured-prompt textarea[readonly]{background:#f7f8fc;color:#6c7890}.empty-node{margin:0;padding:17px;color:#8b95a7;font-size:16px}.structure-original{min-width:0;padding-top:2px}.structure-original summary{color:#7a87a1;font-size:16px;cursor:pointer}.structure-original textarea{min-height:180px;margin-top:12px;font-size:16px}
.structure-error{margin:0;padding:10px 12px;border-radius:9px;background:#fff2f4;color:#a43c55;font-size:16px;overflow-wrap:anywhere}.structure-dialog{box-sizing:border-box;width:min(520px,calc(100vw - 32px));max-height:calc(100dvh - 32px);padding:26px;border:1px solid #e2dff4;border-radius:18px;color:#26324d;font-size:18px;line-height:1.65;box-shadow:0 24px 80px #26324d33;overflow:auto}.structure-dialog::backdrop{background:#26324d60;backdrop-filter:blur(3px)}.structure-dialog h3{margin:0 0 18px;font-size:24px}.structure-dialog label{display:block;margin:14px 0 7px;font-size:16px;font-weight:650}.structure-dialog input{box-sizing:border-box;width:100%;min-height:46px;padding:10px 12px;border:1px solid #e2e7f1;border-radius:9px;outline:none;color:#26324d;font:inherit;font-size:18px}.dialog-target,.name-help{color:#78869e;font-size:16px;margin:8px 0}.structure-dialog footer{display:flex;justify-content:flex-end;gap:10px;margin-top:22px}.structure-dialog footer button{background:#6554ec;color:white;border-color:#6554ec}.structure-dialog footer .cancel{background:white;color:#68758f;border-color:#dfe3ee}
@media(max-width:600px){.structured-prompt{padding:12px;gap:12px}.structure-toolbar{flex-wrap:wrap;gap:10px}.prompt-field{grid-template-columns:1fr;gap:6px;padding:12px}.prompt-key{display:flex;flex-wrap:wrap;align-items:center;gap:10px;padding:0}.prompt-node-heading{padding:11px 12px;gap:8px}.prompt-node-heading h4{font-size:18px}.structure-dialog{padding:20px}}
</style>
