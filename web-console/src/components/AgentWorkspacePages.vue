<script setup lang="ts">
import { ref, useId, watch } from 'vue'
import PlanConfigurationIcon from './PlanConfigurationIcon.vue'

const props = withDefaults(defineProps<{
  contextKey: string
  saving?: boolean
  saveDisabled?: boolean
  saveLabel?: string
  saveHint?: string
}>(), { saving: false, saveDisabled: false, saveLabel: '', saveHint: '' })
const emit = defineEmits<{ save: [] }>()
type Page = 'config' | 'preview'
const activePage = ref<Page>('config')
const prefix = 'agent-workspace-' + useId()
watch(() => props.contextKey, () => { activePage.value = 'config' })
function save() { if (!props.saveDisabled && !props.saving) emit('save') }
function handleTabKey(event: KeyboardEvent) {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  activePage.value = event.key === 'Home' ? 'config' : event.key === 'End' ? 'preview' : activePage.value === 'config' ? 'preview' : 'config'
  const target = (event.currentTarget as HTMLElement).querySelector<HTMLButtonElement>(`[data-page="${activePage.value}"]`)
  target?.focus()
}
</script>

<template>
  <div class="agent-workspace-pages">
    <h3 class="workspace-title">智能体工作台</h3>
    <nav class="workspace-navigation" role="tablist" aria-label="智能体工作台页面" @keydown="handleTabKey">
      <button :id="prefix + '-config-tab'" type="button" role="tab" data-page="config" :aria-selected="activePage === 'config'" :aria-controls="prefix + '-config'" :tabindex="activePage === 'config' ? 0 : -1" @click="activePage = 'config'">
        <PlanConfigurationIcon name="facts" /><span><strong>智能体配置</strong><small>内容任务 · 内容池与核对</small></span>
      </button>
      <button :id="prefix + '-preview-tab'" type="button" role="tab" data-page="preview" :aria-selected="activePage === 'preview'" :aria-controls="prefix + '-preview'" :tabindex="activePage === 'preview' ? 0 : -1" @click="activePage = 'preview'">
        <PlanConfigurationIcon name="preview" /><span><strong>生成与预览</strong><small>初始化 · 主线与试听</small></span>
      </button>
    </nav>
    <section :id="prefix + '-config'" v-show="activePage === 'config'" role="tabpanel" :aria-labelledby="prefix + '-config-tab'" class="workspace-page workspace-config-page">
      <header class="workspace-page-heading"><h3>智能体配置</h3><button class="workspace-button" type="button" @click="activePage = 'preview'">去生成与预览<PlanConfigurationIcon name="next" /></button></header>
      <div class="workspace-page-content"><slot name="config" /></div>
      <footer class="workspace-page-footer"><span>核对内容后保存当前方案</span><div class="workspace-page-actions"><button class="workspace-button primary" type="button" :disabled="saveDisabled || saving" :aria-busy="saving" :title="saveHint || undefined" @click="save"><PlanConfigurationIcon name="save" />{{ saveLabel || (saving ? '保存中…' : '保存智能体') }}</button><button class="workspace-button" type="button" @click="activePage = 'preview'">进入生成与预览<PlanConfigurationIcon name="next" /></button></div></footer>
    </section>
    <section :id="prefix + '-preview'" v-show="activePage === 'preview'" role="tabpanel" :aria-labelledby="prefix + '-preview-tab'" class="workspace-page workspace-preview-page">
      <header class="workspace-page-heading"><h3>生成与预览</h3><button class="workspace-button" type="button" @click="activePage = 'config'">返回智能体配置</button></header>
      <div class="workspace-page-content"><slot name="preview" /></div>
      <footer class="workspace-page-footer"><span>本场继续使用已保存快照</span><button class="workspace-button" type="button" @click="activePage = 'config'">返回智能体配置</button></footer>
    </section>
  </div>
</template>

<style scoped>
.agent-workspace-pages{box-sizing:border-box;display:grid;gap:18px;width:100%;min-width:0;padding:22px;border-radius:18px;background:#f5f7fe;color:#253751}
.workspace-title{margin:0;font-size:22px;font-weight:700}.workspace-navigation{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:6px;padding:5px;background:#fff;border:1px solid #e3e8f7;border-radius:14px;min-width:0}
.workspace-navigation button{display:flex;align-items:center;gap:12px;min-width:0;margin:0;padding:14px 17px;border:0;border-radius:10px;background:transparent;text-align:left;color:#344661;font:inherit;cursor:pointer}.workspace-navigation button span{display:grid;gap:4px;min-width:0}.workspace-navigation strong{font-size:16px;font-weight:700}.workspace-navigation small{font-size:12px;color:#7a8aa5}.workspace-navigation button[aria-selected=true]{background:#eef0ff;color:#5259e6}.workspace-navigation .plan-configuration-icon{width:23px;height:23px}
.workspace-page{min-width:0}.workspace-page-heading,.workspace-page-footer{display:flex;align-items:center;justify-content:space-between;gap:14px;flex-wrap:wrap}.workspace-page-heading{margin-bottom:16px}.workspace-page-heading h3{margin:0;font-size:18px;color:#26354d;font-weight:650}.workspace-page-content{display:grid;grid-template-columns:minmax(0,1fr);gap:20px;min-width:0}.workspace-page-content :deep(>*){min-width:0;max-width:100%;box-sizing:border-box}.workspace-page-content :deep(.pool-notice.is-compact){background:#fff}.workspace-page-footer{padding-top:20px;color:#7a8aa5;font-size:12px}.workspace-page-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}
.workspace-button{display:inline-flex;align-items:center;justify-content:center;gap:7px;min-height:38px;padding:8px 13px;border:1px solid #e0e6f5;border-radius:9px;background:#fff;color:#465875;font:inherit;font-size:13px;font-weight:600;cursor:pointer}.workspace-button .plan-configuration-icon{width:16px;height:16px}.workspace-button.primary{background:#5259e6;border-color:#5259e6;color:#fff}.workspace-button:disabled{opacity:.55;cursor:not-allowed}.workspace-button:focus-visible,.workspace-navigation button:focus-visible{outline:2px solid #8589f3;outline-offset:3px}
@media(max-width:700px){.agent-workspace-pages{padding:14px}.workspace-navigation button{gap:8px;padding:12px 9px}.workspace-navigation strong{font-size:14px}.workspace-navigation small{font-size:11px}.workspace-navigation .plan-configuration-icon{width:19px;height:19px}.workspace-page-heading{gap:10px}.workspace-page-footer{align-items:flex-start}.workspace-page-actions{width:100%}.workspace-button{min-height:42px}}
</style>
