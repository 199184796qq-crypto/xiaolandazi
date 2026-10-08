<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AnchorAnalysisSection } from '../types'
import { editableAnchorSections } from '../anchorAnalysis'
import { parseAnchorStyleDisplay, styleDisplayLabel } from '../anchorStyleDisplay'
import StyleDisplayText from './StyleDisplayText.vue'

const props = defineProps<{ text: string; title?: string; editable?: boolean; busy?: boolean }>()
const emit = defineEmits<{ save: [sections: AnchorAnalysisSection[]] }>()
const editing = ref('')
const draft = ref('')
const pendingDelete = ref('')
const editError = ref('')
watch(() => props.text, () => { editing.value = ''; draft.value = ''; pendingDelete.value = ''; editError.value = '' })
function editPoint(section: number, point: number, text: string) { if (props.busy) return; pendingDelete.value = ''; editError.value = ''; editing.value = `${section}:${point}`; draft.value = text }
function editTitle(section: number, text: string) { if (props.busy) return; pendingDelete.value = ''; editError.value = ''; editing.value = `title:${section}`; draft.value = text }
function saveTitle(section: number) {
  if (props.busy || !draft.value.trim()) return
  if (Array.from(draft.value.trim()).length > 60 || /[\r\n【】]/.test(draft.value)) { editError.value = '标题最多60字，不能包含换行或【】'; return }
  const document = editableAnchorSections(props.text)
  document[section]!.title = draft.value.trim()
  emit('save', document)
}
function savePoint(section: number, point: number) {
  if (props.busy || !draft.value.trim()) return
  const document = editableAnchorSections(props.text)
  document[section]!.points[point] = draft.value.trim()
  emit('save', document)
}
function removeItem(section: number, point?: number) {
  if (props.busy) return
  const key = point === undefined ? `section:${section}` : `${section}:${point}`
  if (pendingDelete.value !== key) { editing.value = ''; editError.value = ''; pendingDelete.value = key; return }
  const document = editableAnchorSections(props.text)
  if (point === undefined) document.splice(section, 1)
  else document[section]!.points.splice(point, 1)
  if (!document.some(item => item.points.length)) { editError.value = '请至少保留一个分析小项。'; return }
  emit('save', document)
}
const sections = computed(() => {
  let number = 0
  return parseAnchorStyleDisplay(props.text).map(section => ({
    ...section,
    number: section.title ? ++number : 0,
    points: section.points.map(point => ({ ...point, ...styleDisplayLabel(point.text) })),
  }))
})
</script>

<template>
  <section class="style-analysis-display" aria-label="主播风格分析结果">
    <header class="analysis-hero">
      <span class="analysis-icon" aria-hidden="true"><svg viewBox="0 0 32 32" fill="none"><path d="M5 14v4M10 10v12M16 5v22M22 10v12M27 14v4" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" /></svg></span>
      <div class="analysis-heading"><h3>{{ title || '风格分析结果' }}</h3><p>跨商品复用。程序校对的评分基准已自动关联保存，不需要另外确认。</p></div>
      <slot name="versions"><span class="analysis-badge">口播风格解析</span></slot>
    </header>
    <div v-if="text.trim()" class="analysis-sections">
      <details v-for="(section, index) in sections" :key="index" open class="analysis-section" :class="section.number ? `tone-${(section.number - 1) % 5}` : 'analysis-preface'">
        <summary class="section-heading">
          <span class="section-number" aria-hidden="true">{{ section.number || '·' }}</span>
          <div v-if="editing === `title:${index}`" class="section-title-editor" @click.stop>
            <input v-model="draft" maxlength="60" aria-label="修改分析大项标题" :disabled="busy" />
            <button type="button" :disabled="busy || !draft.trim()" @click.stop.prevent="saveTitle(index)">保存</button>
            <button type="button" :disabled="busy" @click.stop.prevent="editing = ''">取消</button>
          </div>
          <h4 v-else><span class="section-title-text"><StyleDisplayText :text="section.title || '补充说明'" /></span><span class="fold-triangle" aria-hidden="true"></span></h4>
          <span v-if="editable" class="point-actions section-actions">
            <template v-if="pendingDelete === `section:${index}`"><span>删除整个大项？</span><button type="button" class="analysis-delete" :disabled="busy" @click.stop.prevent="removeItem(index)">确认删除</button><button type="button" :disabled="busy" @click.stop.prevent="pendingDelete = ''; editError = ''">取消</button></template>
            <template v-else><button type="button" :disabled="busy" aria-label="修改分析大项" title="修改大项标题" @click.stop.prevent="editTitle(index, section.title || '补充说明')">✎</button><button type="button" class="analysis-delete" :disabled="busy" aria-label="删除大项" title="删除大项" @click.stop.prevent="removeItem(index)">删除</button></template>
          </span>
        </summary>
        <div class="section-points">
          <div v-for="(point, pointIndex) in section.points" :key="pointIndex" class="analysis-point" :class="{ 'has-label': point.label }">
            <span class="point-marker" aria-hidden="true">{{ point.marker || '•' }}</span>
            <div v-if="point.label" class="point-label"><StyleDisplayText :text="point.label" /><span>{{ point.separator }}</span></div>
            <div class="point-body">
              <template v-if="editing === `${index}:${pointIndex}`"><textarea v-model="draft" rows="4" aria-label="修改分析小项" :disabled="busy" /><div class="point-actions"><button type="button" :disabled="busy || !draft.trim()" @click="savePoint(index, pointIndex)">保存</button><button type="button" :disabled="busy" @click="editing = ''">取消</button></div></template>
              <template v-else><StyleDisplayText :text="point.body" /><span v-if="editable" class="point-actions"><template v-if="pendingDelete === `${index}:${pointIndex}`"><span>删除这项？</span><button type="button" class="analysis-delete" :disabled="busy" @click="removeItem(index, pointIndex)">确认删除</button><button type="button" :disabled="busy" @click="pendingDelete = ''; editError = ''">取消</button></template><template v-else><button type="button" :disabled="busy" aria-label="修改分析小项" title="修改小项" @click="editPoint(index, pointIndex, point.text)">✎</button><button type="button" class="analysis-delete" :disabled="busy" aria-label="删除分析小项" @click="removeItem(index, pointIndex)">删除</button></template></span></template>
            </div>
          </div>
          <p v-if="!section.points.length" class="section-empty">此板块未返回正文。</p>
        </div>
      </details>
    </div>
    <p v-if="editError" class="analysis-edit-error" role="alert">{{ editError }}</p>
    <p v-if="!text.trim()" class="analysis-empty">还没有总风格。单份样本分析后自动进入；多份样本请勾选“多样本分析”后点击“开始分析”。</p>
  </section>
</template>

<style scoped>
.style-analysis-display{min-width:0;border:1px solid #e0e5f3;border-radius:20px;background:#fff;overflow:hidden;box-shadow:0 8px 28px #5864a008;color:#1d2943}
.analysis-hero{display:flex;gap:18px;align-items:center;padding:24px;background:linear-gradient(120deg,#fff,#f2f0ff)}
.analysis-icon{display:grid;place-items:center;width:64px;height:64px;flex:0 0 64px;color:#fff;border-radius:17px;background:linear-gradient(145deg,#bba8ff,#6e57ff);box-shadow:0 8px 18px #8b6afa24}.analysis-icon svg{width:36px;height:36px}
.analysis-heading{min-width:0;flex:1}.analysis-heading h3{margin:0 0 8px;font-size:22px;line-height:1.4;font-weight:850;overflow-wrap:anywhere}.analysis-heading p{margin:0;color:#68778f;font-size:14px;line-height:1.7}.analysis-badge{flex-shrink:0;color:#fff;background:linear-gradient(110deg,#ab8dff,#9c80f0);border-radius:24px;padding:9px 15px;font-size:12px;font-weight:700}
.analysis-sections{display:grid;gap:14px;padding:16px}.analysis-section{--accent:#3985ff;--tint:#f0f6ff;--border:#e4eeff;padding:19px 20px;border:1px solid var(--border);border-radius:16px;background:linear-gradient(110deg,var(--tint),#fff 150%)}
.tone-1{--accent:#0c9c7a;--tint:#effaf6;--border:#def2e9}.tone-2{--accent:#ef7626;--tint:#fff6ef;--border:#ffeade}.tone-3{--accent:#8a5aff;--tint:#f5f0ff;--border:#ebe3ff}.tone-4{--accent:#da5c92;--tint:#fff2f8;--border:#fae4ef}.analysis-preface{--accent:#7b8ca7;--tint:#f7f9fc;--border:#e8edf4}
.section-heading{display:flex;align-items:center;gap:14px;margin-bottom:16px}.section-number{display:grid;place-items:center;width:38px;height:38px;flex-shrink:0;border-radius:50%;background:var(--accent);color:#fff;font-size:20px;font-weight:750;box-shadow:inset 0 2px 5px #ffffff38}.section-heading h4{margin:0;font-size:20px;line-height:1.5;font-weight:800}
.section-points{display:grid;gap:13px}.analysis-point{display:grid;grid-template-columns:24px minmax(0,1fr);column-gap:10px;align-items:baseline}.analysis-point.has-label{grid-template-columns:24px minmax(92px,130px) minmax(0,1fr)}.point-marker{color:var(--accent);font-size:14px;font-weight:800;line-height:1.85}.point-label{font-weight:750;color:#293b55;font-size:15px;line-height:1.85;overflow-wrap:anywhere}.point-body{font-size:15px;line-height:1.9;color:#65758c;white-space:pre-wrap;overflow-wrap:anywhere}.point-body :deep(strong){color:#354a67;font-weight:750}.point-body :deep(code){padding:2px 6px;border-radius:5px;background:#ffffffa0;color:var(--accent);font:inherit}.section-empty,.analysis-empty{margin:0;color:#8190a6;font-size:14px;line-height:1.8}.analysis-empty{padding:28px}
.section-heading{cursor:pointer;list-style:none;margin-bottom:0}.section-heading::-webkit-details-marker{display:none}.analysis-section[open] .section-points{margin-top:16px}.fold-triangle{display:inline-block;margin-left:8px;font-size:12px;line-height:1;color:#64728c;vertical-align:middle}.fold-triangle::before{content:"▼"}.analysis-section[open] .fold-triangle::before{content:"▶"}.point-actions{display:inline-flex;gap:6px;margin-left:12px;vertical-align:middle}.point-actions button,.analysis-delete{border:1px solid #dce2f4;border-radius:7px;background:#fff;padding:4px 8px;color:#6a61c9;cursor:pointer;font:inherit;font-size:12px}.analysis-delete{color:#c3516b}.point-body textarea{box-sizing:border-box;width:100%;min-height:96px;padding:10px;border:1px solid #a69bf5;border-radius:9px;font:inherit;line-height:1.6;color:#354a67}.point-actions button:disabled,.analysis-delete:disabled{opacity:.45;cursor:default}.section-heading h4{flex:1}
.analysis-original{border-top:1px solid #e5eaf3;padding:16px 6px 6px;color:#76839a;font-size:13px}.analysis-original summary{cursor:pointer;font-weight:650}.analysis-original pre{margin:14px 0 0;padding:18px;border-radius:12px;background:#f7f9fc;color:#617188;font:inherit;line-height:1.9;white-space:pre-wrap;overflow-wrap:anywhere}
.section-actions{flex-wrap:wrap;justify-content:flex-end;white-space:normal}.section-title-editor{display:flex;flex:1;min-width:0;gap:7px;flex-wrap:wrap}.section-title-editor input{box-sizing:border-box;min-width:0;flex:1;width:150px;padding:8px;border:1px solid #a69bf5;border-radius:8px;font:inherit}.section-title-editor button{border:1px solid #dce2f4;border-radius:7px;background:#fff;color:#6a61c9;padding:4px 8px;cursor:pointer}.analysis-edit-error{color:#bd4966;padding:0 20px 16px;margin:0}.section-heading{flex-wrap:wrap}.point-actions{white-space:normal}
@media(max-width:780px){.analysis-hero{padding:18px;gap:12px;flex-wrap:wrap}.analysis-icon{width:48px;height:48px;flex-basis:48px;border-radius:13px}.analysis-icon svg{width:29px;height:29px}.analysis-heading h3{font-size:18px}.analysis-heading p{font-size:12px}.analysis-badge{margin-left:60px}.analysis-sections{padding:12px;gap:12px}.analysis-section{padding:16px 14px}.section-heading h4{font-size:18px}.analysis-point.has-label{grid-template-columns:22px minmax(0,1fr)}.point-label{grid-column:2}.has-label .point-body{grid-column:2}.point-body,.point-label{font-size:14px}}
</style>

<style scoped>
@media(max-width:780px){.analysis-heading{flex:1 1 calc(100% - 72px)}.analysis-badge{margin-left:auto}.analysis-hero :deep(.anchor-analysis-version-picker){width:100%;justify-content:flex-start}}
</style>
