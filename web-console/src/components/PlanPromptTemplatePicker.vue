<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { getLiveAgentPlanPromptTemplates, type PlanPromptPage } from '../api'
import { templateListError, validTemplateRevision, type PromptTextTemplate, type SelectedPromptTemplate } from '../planPromptTemplateCatalog'

const props = defineProps<{ planId: number; page: PlanPromptPage; label: string }>()
const emit = defineEmits<{ cancel: []; apply: [template: PromptTextTemplate] }>()
const dialog = ref<HTMLDialogElement | null>(null)
const items = ref<SelectedPromptTemplate[]>([])
const selected = ref('')
const loading = ref(false)
const error = ref('')
const active = computed(() => items.value.find(item => item.id === selected.value))
const preview = computed(() => active.value?.rendered_content ?? active.value?.content ?? '')
let mounted = true
let sequence = 0
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = ++sequence
  controller = new AbortController()
  loading.value = true; error.value = ''; selected.value = ''; items.value = []
  try {
    const value = await getLiveAgentPlanPromptTemplates(props.planId, props.page, controller.signal)
    if (!mounted || request !== sequence) return
    if (value.page !== props.page || !validTemplateRevision(value.revision)) throw new Error('模板目录回执无效，请重新读取')
    // Catalog capacity counts stored originals, never the extra preview bytes.
    const originals = Array.isArray(value.templates) ? value.templates.map(item => item && ({ id: item.id, name: item.name, content: item.content })) : value.templates
    const invalid = templateListError(originals)
    if (invalid) throw new Error(invalid)
    if (value.templates.some(item => (item.rendered_content !== undefined && typeof item.rendered_content !== 'string') || (item.render_error !== undefined && typeof item.render_error !== 'string'))) throw new Error('模板填充回执无效，请重新读取')
    items.value = value.templates.map(item => ({ ...item }))
  } catch (err) {
    if (mounted && request === sequence) error.value = err instanceof Error ? err.message : '读取模板失败'
  } finally { if (mounted && request === sequence) loading.value = false }
}
function backdrop(event: MouseEvent) {
  if (!dialog.value || event.target !== dialog.value) return
  const box = dialog.value.getBoundingClientRect()
  if (event.clientX < box.left || event.clientX > box.right || event.clientY < box.top || event.clientY > box.bottom) emit('cancel')
}
onMounted(async () => { await nextTick(); dialog.value?.showModal(); void load() })
onBeforeUnmount(() => { mounted = false; sequence++; controller?.abort(); dialog.value?.close() })
</script>

<template>
  <Teleport to="body">
    <dialog ref="dialog" class="prompt-template-picker" aria-labelledby="prompt-template-picker-title" :aria-busy="loading" @cancel.prevent="emit('cancel')" @click="backdrop">
      <h2 id="prompt-template-picker-title">模板选择 · {{ label }}</h2>
      <p>按已保存的方案资料填充占位符，无法填充的保留原样。确认后写入当前页，检查后点击顶部“保存方案”。</p>
      <p v-if="loading" role="status">正在读取模板…</p>
      <div v-else-if="error" class="picker-error" role="alert"><p>{{ error }}</p><button type="button" @click="load">重新读取</button></div>
      <p v-else-if="!items.length" role="status">当前标签暂无模板，请先在后台“提示词维护”中添加并保存。</p>
      <div v-else class="picker-layout">
        <div class="picker-list" role="radiogroup" aria-label="可用模板">
          <label v-for="item in items" :key="item.id" :class="{ selected: selected === item.id }"><input v-model="selected" type="radio" name="prompt-template" :value="item.id" />{{ item.name }}</label>
        </div>
        <div class="picker-preview"><strong>模板预览</strong><p v-if="active?.render_error" class="picker-error" role="alert">{{ active.render_error }}</p><pre v-if="active">{{ preview || '（空模板：确认后将清空当前页）' }}</pre><p v-else>请选择一份模板查看内容。</p></div>
      </div>
      <footer><button type="button" autofocus @click="emit('cancel')">取消</button><button type="button" class="primary" :disabled="loading || !active || !!active.render_error" @click="active && !active.render_error && emit('apply', { id: active.id, name: active.name, content: preview })">确认使用</button></footer>
    </dialog>
  </Teleport>
</template>

<style scoped>
.prompt-template-picker{box-sizing:border-box;width:min(960px,calc(100vw - 32px));max-height:calc(100dvh - 32px);margin:auto;padding:26px;border:1px solid #dfe3f4;border-radius:22px;background:#fff;color:#26324d;font:inherit;font-size:18px;line-height:1.7;overflow:auto;box-shadow:0 24px 80px #26324d33}.prompt-template-picker::backdrop{background:#26324d66;backdrop-filter:blur(4px)}h2{margin:0;font-size:24px}p{color:#71819d;overflow-wrap:anywhere}.picker-layout{display:grid;grid-template-columns:minmax(170px,1fr) minmax(0,3fr);gap:20px;min-width:0}.picker-list{display:grid;gap:10px;align-content:start}.picker-list label{display:flex;align-items:start;gap:10px;padding:12px;border:1px solid #dfe3f4;border-radius:12px;cursor:pointer;overflow-wrap:anywhere}.picker-list label.selected{background:#f3f0ff;border-color:#8273f6;color:#5741ca}.picker-list input{box-sizing:border-box;flex:none;width:20px;height:20px;min-height:20px;max-width:20px;margin:5px 0 0;padding:0;appearance:auto;box-shadow:none;accent-color:#6554ec}.picker-preview{min-width:0;padding:16px;border:1px solid #dfe3f4;border-radius:12px;background:#fafbff}.picker-preview pre{max-height:50vh;overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;font-size:20px}.picker-error{padding:14px;background:#fff5f6;color:#a64256;border-radius:12px}footer{display:flex;justify-content:flex-end;gap:12px;margin-top:22px}button{box-sizing:border-box;min-height:44px;padding:10px 18px;border:1px solid #dfe3f4;border-radius:12px;background:#fff;color:#596682;font:inherit;cursor:pointer}button.primary{background:#6554ec;color:white;border-color:#6554ec}button:disabled{opacity:.5;cursor:default}button:focus-visible,label:focus-within{outline:3px solid #c5bfff;outline-offset:2px}@media(max-width:600px){.prompt-template-picker{padding:18px}.picker-layout{grid-template-columns:minmax(0,1fr)}.picker-preview pre{max-height:35vh}}
</style>
