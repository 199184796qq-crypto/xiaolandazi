<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { getPlanPromptTemplates, savePlanPromptTemplates, type PlanPromptTemplates, type PlanPromptTemplatesDocument } from '../api'
import { emptyPlanPromptSections, planPromptTabs, planPromptValidation } from '../planPrompt'

const tabs = [...planPromptTabs, { key: 'rewrite_planning_md', label: '话术规划' }] as const
const draft = ref<PlanPromptTemplates>({ ...emptyPlanPromptSections(), rewrite_planning_md: '' })
const original = ref('')
const revision = ref('')
const selected = ref<keyof PlanPromptTemplates>('persona_md')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
const confirmation = ref<HTMLDialogElement | null>(null)
const busy = computed(() => loading.value || saving.value)
const dirty = computed(() => JSON.stringify(draft.value) !== original.value)
const validation = computed(() => planPromptValidation(draft.value, draft.value.rewrite_planning_md))
const activeLabel = computed(() => tabs.find(tab => tab.key === selected.value)!.label)
let mounted = true
onBeforeUnmount(() => { mounted = false; confirmation.value?.close() })

function accept(value: PlanPromptTemplatesDocument) {
  if (typeof value.revision !== 'string' || value.revision.length !== 64 ||
      tabs.some(tab => typeof value.templates?.[tab.key] !== 'string')) throw new Error('未返回完整的六份母版，当前内容已保留')
  const next: PlanPromptTemplates = { ...emptyPlanPromptSections(), rewrite_planning_md: '' }
  for (const tab of tabs) next[tab.key] = value.templates[tab.key]
  draft.value = next
  revision.value = value.revision
  original.value = JSON.stringify(draft.value)
}
async function load() {
  if (busy.value) return
  loading.value = true; error.value = ''; notice.value = ''
  try { const value = await getPlanPromptTemplates(); if (mounted) accept(value) }
  catch (err) { if (mounted) error.value = err instanceof Error ? err.message : '读取提示词母版失败' }
  finally { if (mounted) loading.value = false }
}
function reload() {
  if (busy.value) return
  if (dirty.value && revision.value) { confirmation.value?.showModal(); return }
  void load()
}
function confirmReload() { confirmation.value?.close(); void load() }
async function save() {
  if (busy.value || !revision.value || validation.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const value = await savePlanPromptTemplates({ ...draft.value }, revision.value)
    if (!mounted) return
    accept(value); notice.value = '六份母版已保存。方案页面下次点击“拉取提示词”时使用新内容。'
  } catch (err) { if (mounted) error.value = err instanceof Error ? err.message : '保存失败，草稿已保留' }
  finally { if (mounted) saving.value = false }
}
onMounted(load)
</script>

<template>
  <section class="prompt-templates" :aria-busy="busy">
    <header>
      <div><h1>提示词维护</h1><p>维护方案提示词的六份母版。事实依据在拉取时按方案填充商品卡、活动福利和正式事实。</p></div>
      <div class="actions"><button type="button" :disabled="busy" @click="reload">重新读取</button><button type="button" class="primary" :disabled="busy || !revision || !!validation" @click="save">{{ saving ? '保存中…' : '保存母版' }}</button></div>
    </header>
    <p class="hint">保存母版后，由方案页面点击“拉取提示词”并“保存方案”采用。</p>
    <p v-if="error" class="error" role="alert">{{ error }}<span> 当前草稿已保留。</span></p>
    <p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <p v-if="loading" role="status">正在读取提示词母版…</p>
    <nav class="tabs" aria-label="六份提示词母版"><button v-for="tab in tabs" :key="tab.key" type="button" :class="{ active: selected === tab.key }" :aria-pressed="selected === tab.key" @click="selected = tab.key">{{ tab.label }}</button></nav>
    <div class="editor-heading"><label for="prompt-template-content">{{ activeLabel }}母版</label><span>{{ Array.from(draft[selected]).length }} 字</span></div>
    <textarea id="prompt-template-content" v-model="draft[selected]" :aria-label="activeLabel + '母版'" :readonly="busy || !revision" maxlength="20000" rows="20" spellcheck="false" @input="notice = ''"></textarea>
    <p v-if="validation" class="error" role="alert">{{ validation }}</p>
    <dialog ref="confirmation" aria-labelledby="template-reload-title">
      <h2 id="template-reload-title">重新读取母版？</h2><p>当前未保存的修改将被替换。</p><div class="actions"><button type="button" autofocus @click="confirmation?.close()">取消</button><button type="button" class="primary" @click="confirmReload">确认读取</button></div>
    </dialog>
  </section>
</template>

<style scoped>
.prompt-templates{box-sizing:border-box;width:100%;min-width:0;display:grid;gap:18px;padding:28px;background:#fff;border:1px solid #e2e6f3;border-radius:22px;color:#26324d;font-size:20px;line-height:1.65}.prompt-templates header{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.prompt-templates h1{margin:0;font-size:28px}.prompt-templates p{margin:8px 0;overflow-wrap:anywhere}.prompt-templates header>div:first-child{max-width:900px;min-width:0}.hint{color:#71819d}.actions,.tabs{display:flex;flex-wrap:wrap;gap:12px}.actions{flex-shrink:0}.prompt-templates button{min-height:46px;padding:10px 18px;border:1px solid #dfe3f4;border-radius:12px;background:#f5f6ff;color:#596682;font:inherit;cursor:pointer}.prompt-templates button.primary,.tabs button.active{background:#6554ec;color:#fff;border-color:#6554ec}.prompt-templates button:disabled{opacity:.55;cursor:wait}.prompt-templates button:focus-visible{outline:3px solid #b7aaff;outline-offset:3px}.editor-heading{display:flex;justify-content:space-between;gap:16px}.editor-heading label{font-weight:650}.editor-heading span{color:#71819d}.prompt-templates textarea{box-sizing:border-box;display:block;width:100%;max-width:100%;min-width:0;min-height:540px;padding:20px;border:1px solid #dfe3f4;border-radius:14px;background:#fff;color:#26324d;font:inherit;font-size:20px;line-height:1.8;resize:vertical;outline:none}.prompt-templates textarea:focus{border-color:#8273f6;box-shadow:0 0 0 3px #6554ec15}.prompt-templates textarea[readonly]{background:#f8f9fd}.error,.notice{padding:14px;border-radius:12px;border:1px solid #f1cbd2;background:#fff5f6;color:#a64256}.notice{border-color:#d6e8e2;background:#f0faf6;color:#307c69}.prompt-templates dialog{box-sizing:border-box;width:min(560px,calc(100vw - 32px));padding:24px;border:1px solid #dfe3f4;border-radius:18px;font:inherit;color:inherit}.prompt-templates dialog::backdrop{background:#26324d66}.prompt-templates dialog h2{margin-top:0;font-size:24px}.prompt-templates dialog .actions{justify-content:flex-end}@media(max-width:900px){.prompt-templates{padding:18px}.prompt-templates header{flex-direction:column}.actions{flex-shrink:1}.prompt-templates textarea{padding:16px}.prompt-templates h1{font-size:26px}}
</style>
