<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { getPlanPromptTemplateCatalog, savePlanPromptTemplateCatalog, type PlanPromptPage } from '../api'
import { catalogError, emptyTemplateCatalog, templateListError, validTemplateRevision, type PromptTemplateCatalogDocument } from '../planPromptTemplateCatalog'
import { planPromptTabs } from '../planPrompt'
import PromptPlaceholderTextarea from '../components/PromptPlaceholderTextarea.vue'
import { promptPlaceholdersForDocument } from '../planPromptPlaceholders'

const tabs = [...planPromptTabs, { key: 'rewrite_planning_md', label: '话术规划' }] as const
const draft = ref(emptyTemplateCatalog())
const original = ref('')
const revision = ref('')
const selected = ref<PlanPromptPage>('persona_md')
const templateID = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const confirmation = ref<HTMLDialogElement | null>(null)
const deletePending = ref('')
const saveFeedback = ref<HTMLParagraphElement | null>(null)
const busy = computed(() => loading.value || saving.value)
const dirty = computed(() => JSON.stringify(draft.value) !== original.value)
const validation = computed(() => {
  for (const tab of tabs) {
    const issue = templateListError(draft.value.pages[tab.key])
    if (issue) return tab.label + '：' + issue
  }
  return catalogError(draft.value)
})
const activeLabel = computed(() => tabs.find(tab => tab.key === selected.value)!.label)
const items = computed(() => draft.value.pages[selected.value])
const active = computed(() => items.value.find(item => item.id === templateID.value))
const availablePlaceholders = computed(() => promptPlaceholdersForDocument(undefined).filter(item => item.templates.includes(selected.value)))
let mounted = true
onBeforeUnmount(() => { mounted = false; confirmation.value?.close() })
function accept(value: PromptTemplateCatalogDocument) {
  const invalid = catalogError(value.catalog)
  if (!validTemplateRevision(value.revision) || invalid) throw new Error(invalid || '模板版本无效')
  draft.value = structuredClone(value.catalog)
  revision.value = value.revision
  original.value = JSON.stringify(value.catalog)
  if (!items.value.some(item => item.id === templateID.value)) templateID.value = items.value[0]?.id || ''
  deletePending.value = ''
}
function selectPage(page: PlanPromptPage) { selected.value = page; templateID.value = items.value[0]?.id || ''; deletePending.value = ''; notice.value = '' }
function addTemplate() {
  if (busy.value || !revision.value || items.value.length >= 20) return
  const item = { id: crypto.randomUUID(), name: '新模板 ' + (items.value.length + 1), content: '' }
  items.value.push(item); templateID.value = item.id; deletePending.value = ''; notice.value = ''
}
function removeTemplate() {
  if (busy.value || !active.value) return
  if (deletePending.value !== active.value.id) { deletePending.value = active.value.id; return }
  draft.value.pages[selected.value] = items.value.filter(item => item.id !== active.value!.id)
  templateID.value = items.value[0]?.id || ''; deletePending.value = ''; notice.value = ''
}
async function load() {
  if (busy.value) return
  loading.value = true; error.value = ''; notice.value = ''
  try { const value = await getPlanPromptTemplateCatalog(); if (mounted) accept(value) }
  catch (err) { if (mounted) error.value = err instanceof Error ? err.message : '读取模板目录失败' }
  finally { if (mounted) loading.value = false }
}
function reload() { if (busy.value) return; if (dirty.value && revision.value) { confirmation.value?.showModal(); return }; void load() }
function confirmReload() { confirmation.value?.close(); void load() }
async function save() {
  if (busy.value || !revision.value) return
  if (validation.value) {
    error.value = validation.value; notice.value = ''
    await nextTick()
    saveFeedback.value?.focus({ preventScroll: true })
    saveFeedback.value?.scrollIntoView({ block: 'nearest' })
    return
  }
  const snapshot = JSON.parse(JSON.stringify(draft.value))
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const value = await savePlanPromptTemplateCatalog(snapshot, revision.value)
    if (!mounted) return
    accept(value); notice.value = '模板目录已保存。前台可通过“模板选择”使用，已有方案保持原稿。'
  } catch (err) { if (mounted) error.value = err instanceof Error ? err.message : '保存失败，草稿已保留' }
  finally { if (mounted) saving.value = false }
}
onMounted(load)
</script>

<template>
  <section class="prompt-templates" :aria-busy="busy">
    <header>
      <div><h1>提示词维护</h1><p>按标签管理多份命名模板，前台选择模板后写入当前页。</p></div>
      <div class="actions"><button type="button" :disabled="busy" @click="reload">重新读取</button><button type="button" class="primary" :disabled="busy || !revision" @click="save">{{ saving ? '保存中…' : '保存模板' }}</button></div>
    </header>
    <p v-if="error || validation" ref="saveFeedback" class="error" role="alert" tabindex="-1">{{ error || validation }}<span> 当前草稿已保留。</span></p>
    <p class="hint">每个标签最多20份、合计60000字节（含格式）；前台选用时填充可用占位符，仍需点击“保存方案”采用。</p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在读取模板目录…</p>
    <nav class="tabs" aria-label="六个模板标签"><button v-for="tab in tabs" :key="tab.key" type="button" :class="{ active: selected === tab.key }" :aria-pressed="selected === tab.key" @click="selectPage(tab.key)">{{ tab.label }}</button></nav>
    <div class="template-toolbar">
      <label>当前模板<select v-model="templateID" :disabled="busy || !items.length" @change="deletePending = ''"><option v-if="!items.length" value="">暂无模板</option><option v-for="item in items" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
      <button type="button" class="primary" :disabled="busy || !revision || items.length >= 20" @click="addTemplate">添加模板</button>
      <button v-if="active" type="button" class="danger" :disabled="busy" @click="removeTemplate">{{ deletePending === active.id ? '确认删除模板' : '删除模板' }}</button>
      <button v-if="deletePending" type="button" :disabled="busy" @click="deletePending = ''">取消删除</button>
    </div>
    <template v-if="active">
      <label class="template-name">模板名称<input v-model="active.name" :readonly="busy" maxlength="60" aria-label="模板名称" @input="notice = ''; error = ''" /></label>
      <div class="editor-heading"><label for="prompt-template-content">{{ activeLabel }} · 模板内容</label><span>{{ Array.from(active.content).length }} 字</span></div>
    <p class="placeholder-help">输入 @ 可插入内置占位符；模板原文保留。前台选用时自动填充可用值，未知或缺值的保留原样，不调用模型。</p>
      <PromptPlaceholderTextarea :key="active.id" id="prompt-template-content" v-model="active.content" :label="activeLabel + '模板内容'" :placeholders="availablePlaceholders" :readonly="busy || !revision" @input="notice = ''; error = ''" />
    </template>
    <p v-else-if="!loading" class="hint">「{{ activeLabel }}」暂无模板，点击“添加模板”开始维护。</p>
    <dialog ref="confirmation" aria-labelledby="template-reload-title">
      <h2 id="template-reload-title">重新读取模板？</h2><p>六个标签未保存的修改将被替换。</p><div class="actions"><button type="button" autofocus @click="confirmation?.close()">取消</button><button type="button" class="primary" @click="confirmReload">确认读取</button></div>
    </dialog>
  </section>
</template>

<style scoped>
.prompt-templates{box-sizing:border-box;width:100%;min-width:0;display:grid;gap:18px;padding:28px;background:#fff;border:1px solid #e2e6f3;border-radius:22px;color:#26324d;font-size:20px;line-height:1.65}.prompt-templates header{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.prompt-templates h1{margin:0;font-size:28px}.prompt-templates p{margin:8px 0;overflow-wrap:anywhere}.prompt-templates header>div:first-child{max-width:900px;min-width:0}.hint{color:#71819d}.actions,.tabs{display:flex;flex-wrap:wrap;gap:12px}.actions{flex-shrink:0}.prompt-templates button{min-height:46px;padding:10px 18px;border:1px solid #dfe3f4;border-radius:12px;background:#f5f6ff;color:#596682;font:inherit;cursor:pointer}.prompt-templates button.primary,.tabs button.active{background:#6554ec;color:#fff;border-color:#6554ec}.prompt-templates button:disabled{opacity:.55;cursor:wait}.prompt-templates button:focus-visible{outline:3px solid #b7aaff;outline-offset:3px}.editor-heading{display:flex;justify-content:space-between;gap:16px}.editor-heading label{font-weight:650}.editor-heading span{color:#71819d}.placeholder-help{color:#71819d;font-size:16px}.error,.notice{padding:14px;border-radius:12px;border:1px solid #f1cbd2;background:#fff5f6;color:#a64256}.notice{border-color:#d6e8e2;background:#f0faf6;color:#307c69}.prompt-templates dialog{box-sizing:border-box;width:min(560px,calc(100vw - 32px));padding:24px;border:1px solid #dfe3f4;border-radius:18px;font:inherit;color:inherit}.prompt-templates dialog::backdrop{background:#26324d66}.prompt-templates dialog h2{margin-top:0;font-size:24px}.prompt-templates dialog .actions{justify-content:flex-end}@media(max-width:900px){.prompt-templates{padding:18px}.prompt-templates header{flex-direction:column}.actions{flex-shrink:1}.prompt-templates h1{font-size:26px}}
.template-toolbar{display:flex;flex-wrap:wrap;align-items:end;gap:12px}.template-toolbar label,.template-name{display:grid;gap:8px;min-width:0}.template-toolbar label{flex:1;min-width:min(100%,220px)}.prompt-templates select,.prompt-templates input{box-sizing:border-box;min-width:0;width:100%;max-width:100%;padding:12px;border:1px solid #dfe3f4;border-radius:12px;font:inherit;color:inherit;background:white}.prompt-templates .danger{color:#ba344f;border-color:#f1cbd2;background:#fff5f6}</style>
