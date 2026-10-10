<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { getLiveAgentPlanProductLinks } from '../api'
import { getLectures, ingestLecture, previewLecture, setLecturePolicy, updateLecture, type LectureIssue, type LectureLibrary, type LecturePolicy, type LecturePreview, type LectureTask } from '../lectureTasks'
import AgentEnterSendOption from './AgentEnterSendOption.vue'
import { useAgentEnterSend } from '../useAgentEnterSend'

const props = defineProps<{ planId: number; tenantId?: number; roomId?: number; savedFingerprint?: string; compact?: boolean; saveDisabled?: boolean; saveHint?: string }>()
const emit = defineEmits<{ updated: []; busy: [value: boolean]; save: [] }>()
const library = ref<LectureLibrary | null>(null)
const source = ref('')
const scope = ref('')
const products = ref<{ link_key: string; product_name?: string }[]>([])
const loading = ref(false)
const pending = ref('')
const error = ref('')
const notice = ref('')
const issues = ref<LectureIssue[]>([])
const previews = ref<Record<number, LecturePreview>>({})
const selectedAngles = ref<Record<number, number>>({})
const deletePending = ref(0)
const search = ref('')
const enterSend = useAgentEnterSend()
let epoch = 0
let loadNo = 0
const busy = computed(() => !!pending.value)
const tasks = computed(() => (library.value?.tasks || []).filter(t => !search.value.trim() || `${t.title} ${t.purpose} ${t.angles.map(a => a.title).join(' ')}`.includes(search.value.trim())))
const activeCount = computed(() => library.value?.tasks.filter(t => t.enabled && t.valid).length || 0)
const cadence = computed(() => {
  let cycles = activeCount.value >= 8 ? 2 : activeCount.value >= 3 ? 3 : 4
  let minutes = activeCount.value >= 8 ? 6 : activeCount.value >= 3 ? 8 : 10
  if (library.value?.policy.frequency === 'less') { cycles += 2; minutes += 4 }
  if (library.value?.policy.frequency === 'more') { cycles = Math.max(2, cycles - 1); minutes = Math.max(6, minutes - 2) }
  return `约${cycles}—${cycles + 2}轮，至少间隔${minutes}分钟`
})
const saved = computed(() => !!props.savedFingerprint && props.savedFingerprint === library.value?.fingerprint)
watch(busy, value => emit('busy', value))
function problem(cause: unknown) { return cause instanceof Error ? cause.message : '暂时没有完成，请稍后再试' }
async function refresh() {
  const attempt = ++loadNo, current = epoch
  if (!props.planId) return
  loading.value = true
  try { const result = await getLectures(props.planId, props.tenantId); if (current === epoch && attempt === loadNo) library.value = result }
  catch (cause) { if (current === epoch && attempt === loadNo) error.value = problem(cause) }
  finally { if (current === epoch && attempt === loadNo) loading.value = false }
}
function changed(factsChanged = false) {
  emit('updated')
  window.dispatchEvent(new CustomEvent('live-lecture-tasks-updated', { detail: { plan_id: props.planId, room_id: props.roomId, facts_changed: factsChanged } }))
}
async function send() {
  if (busy.value || !source.value.trim() || !props.planId) return
  const current = epoch, text = source.value.trim()
  pending.value = 'ingest'; error.value = ''; notice.value = ''; issues.value = []
  try {
    const result = await ingestLecture(props.planId, props.tenantId, text, scope.value ? [scope.value] : [])
    if (current !== epoch) return
    if (!result.saved) { issues.value = result.issues || []; notice.value = '这段素材还有几处需要调整，改好后再发给我吧～'; return }
    notice.value = `${result.title} 已收好啦：新增 ${result.facts_added || 0} 条事实，整理出 ${result.angles?.length || 0} 个讲解角度。`
    if (source.value.trim() === text) source.value = ''
    changed(!!result.facts_added); await refresh()
  } catch (cause) { if (current === epoch) error.value = problem(cause) }
  finally { if (current === epoch) pending.value = '' }
}
function keydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || !enterSend.value || event.shiftKey || event.isComposing || event.keyCode === 229) return
  event.preventDefault(); if (!event.repeat) void send()
}
async function mutate(task: LectureTask, action: string) {
  if (busy.value) return
  const current = epoch; pending.value = `${action}:${task.id}`; error.value = ''
  try { await updateLecture(props.planId, props.tenantId, task, action); if (current !== epoch) return; deletePending.value = 0; changed(); await refresh() }
  catch (cause) { if (current === epoch) error.value = problem(cause) }
  finally { if (current === epoch) pending.value = '' }
}
async function policy(next: LecturePolicy) {
  if (busy.value) return
  const current = epoch; pending.value = 'policy'; error.value = ''
  try { await setLecturePolicy(props.planId, props.tenantId, next); if (current !== epoch) return; changed(); await refresh() }
  catch (cause) { if (current === epoch) error.value = problem(cause) }
  finally { if (current === epoch) pending.value = '' }
}
async function preview(task: LectureTask) {
  if (busy.value) return
  const current = epoch, angle = selectedAngles.value[task.id] || 0; pending.value = `preview:${task.id}`; error.value = ''
  delete previews.value[task.id]
  try { const result = await previewLecture(props.planId, props.tenantId, task, angle); if (current === epoch && (selectedAngles.value[task.id] || 0) === angle) previews.value[task.id] = result }
  catch (cause) { if (current === epoch) error.value = problem(cause) }
  finally { if (current === epoch) pending.value = '' }
}
function updateEvent(event: Event) {
  const detail = (event as CustomEvent<{ plan_id: number; room_id?: number }>).detail
  if (detail?.plan_id === props.planId && (!detail.room_id || detail.room_id === props.roomId)) void refresh()
}
watch(() => [props.planId, props.tenantId, props.roomId], async () => {
  const current = ++epoch; source.value = ''; library.value = null; products.value = []; scope.value = ''; issues.value = []; previews.value = {}; selectedAngles.value = {}; error.value = ''; notice.value = ''; pending.value = ''; deletePending.value = 0
  void refresh()
  try { const result = await getLiveAgentPlanProductLinks(props.planId, props.tenantId); if (current === epoch) products.value = result.items.filter(p => p.status === 'active') }
  catch { /* The public scope remains available; the server validates any selected product. */ }
}, { immediate: true })
onMounted(() => window.addEventListener('live-lecture-tasks-updated', updateEvent))
onBeforeUnmount(() => { ++epoch; window.removeEventListener('live-lecture-tasks-updated', updateEvent); emit('busy', false) })
</script>

<template>
  <section class="lecture-panel" :class="{ compact }" aria-label="任务讲解">
    <header class="lecture-hero">
      <span class="lecture-hero-icon" aria-hidden="true"><svg viewBox="0 0 32 32"><path d="M5 5h9c3 0 4 2 4 4v19c-1-2-3-3-6-3H5zM27 5h-9v23c1-2 3-3 6-3h3zM9 11h5M9 16h5M22 11h2M22 16h2"/></svg></span>
      <div><h3>任务讲解 <small>讲解任务库</small></h3><p>把故事交给智能体，整理成事实与讲解角度，直播时适时讲给观众听。</p></div>
      <span class="lecture-count">{{ activeCount }} 个可用主题</span>
    </header>
    <div class="lecture-compose">
      <div class="lecture-compose-head"><strong>把故事、产地背景或制作介绍放这里</strong><label>适用范围 <select v-model="scope" :disabled="busy"><option value="">公共讲解</option><option v-for="p in products" :key="p.link_key" :value="p.link_key">{{ p.link_key }}号 · {{ p.product_name }}</option></select></label></div>
      <textarea v-model="source" rows="3" maxlength="8000" :disabled="pending === 'ingest'" placeholder="例如：这款鸭子用的土麻鸭产自四川南充……我来帮你拆分事实、核对规则、整理讲解目的和角度。" aria-label="故事素材" @keydown="keydown" />
      <div class="lecture-compose-foot"><small>{{ source.length }}/8000 · 全部通过后自动归档</small><AgentEnterSendOption :disabled="busy"/><button class="primary" type="button" :disabled="busy || !source.trim()" @click="send">{{ pending === 'ingest' ? '正在审核并整理…' : '发送' }}</button></div>
      <div v-if="pending === 'ingest'" class="lecture-working" role="status"><i></i>正在读取素材、核对规则并建立讲解角度，请稍等…</div>
      <p v-if="notice" class="lecture-notice" role="status">{{ notice }}</p>
      <div v-if="issues.length" class="lecture-issues" role="alert"><article v-for="(issue, index) in issues" :key="index"><blockquote>{{ issue.quote }}</blockquote><strong>{{ issue.rule }}</strong><p>{{ issue.reason }}</p><small v-if="issue.suggestion">修改参考：{{ issue.suggestion }}</small></article><p>直接修改上面的素材后重新发送，当前内容尚未入库。</p></div>
    </div>
    <p v-if="error" class="lecture-error" role="alert">{{ error }}</p>
    <div v-if="library" class="lecture-toolbar">
      <label class="lecture-switch"><input type="checkbox" :checked="library.policy.enabled" :disabled="busy" @change="policy({ ...library.policy, enabled: !library.policy.enabled })"/>自动讲解</label>
      <label>频率 <select :value="library.policy.frequency" :disabled="busy" @change="policy({ ...library.policy, frequency: ($event.target as HTMLSelectElement).value as LecturePolicy['frequency'] })"><option value="less">少讲</option><option value="normal">适中</option><option value="more">多讲</option></select></label>
      <input v-model="search" aria-label="搜索讲解任务" placeholder="搜索主题或角度…"/>
      <button type="button" :disabled="loading || busy" @click="refresh">刷新</button>
    </div>
    <div class="lecture-schedule"><span aria-hidden="true">◷</span><p><strong>{{ library?.policy.enabled === false ? '自动讲解已关闭' : '在完整商品小循环后，适时插入一段讲解' }}</strong><small>{{ cadence }}（按当前可用主题估算） · 每段约30—60秒 · 总时长不超过10% · 同主题充分间隔</small></p></div>
    <div v-if="library && (library.tasks.length || savedFingerprint)" class="lecture-save-state"><span>{{ saved ? '已随当前方案保存，下次开播按需使用。' : '讲解库有待保存内容；保存智能体方案后，下次开播生效。' }}</span><button v-if="!saved && !compact" class="primary" :disabled="saveDisabled || busy" :title="saveHint" @click="emit('save')">保存智能体</button></div>
    <p v-if="loading && !library" class="lecture-empty">正在打开讲解库…</p>
    <div v-else-if="!tasks.length" class="lecture-empty"><span aria-hidden="true">✦</span><strong>{{ search ? '没有找到这个主题' : '讲解库等着你的第一个故事' }}</strong><p>素材整理后会出现在这里，不需要逐条分类，也不用跳转页面。</p></div>
    <div class="lecture-task-list">
      <article v-for="(task, index) in tasks" :key="task.id" class="lecture-task" :class="['tone-' + index % 4, { disabled: !task.enabled || !task.valid }]">
        <div class="lecture-task-number"><b>{{ String(index + 1).padStart(2, '0') }}</b><span>{{ task.links.length ? task.links.join('、') + '号适用' : '公共讲解' }}</span></div>
        <div class="lecture-task-content">
          <header><h4>{{ task.title }}</h4><span class="lecture-task-status">{{ !task.valid ? '事实有变化，需重新整理' : task.enabled ? '已启用' : '已停用' }}</span></header>
          <p><strong>讲解目的：</strong>{{ task.purpose }}</p>
          <div class="lecture-angles"><button v-for="(angle, i) in task.angles" :key="i" type="button" :aria-pressed="(selectedAngles[task.id] || 0) === i" @click="selectedAngles[task.id] = i; delete previews[task.id]">{{ angle.title }}</button></div>
          <p class="lecture-angle-goal">{{ task.angles[selectedAngles[task.id] || 0]?.goal }}</p>
          <details><summary>事实依据 {{ task.facts.length }} 条 · 查看原始素材</summary><ul><li v-for="fact in task.facts" :key="fact.id"><strong>{{ fact.name }}</strong>：{{ fact.text }}</li></ul><blockquote>{{ task.source }}</blockquote><button type="button" :disabled="busy" @click="source = task.source || ''; scope = task.links.length === 1 ? task.links[0]! : ''">复制素材到输入框</button></details>
          <div v-if="previews[task.id]" class="lecture-preview" :class="{ 'preview-rejected': !previews[task.id]!.approved }">
            <strong>{{ previews[task.id]!.approved ? '这个角度可以这样讲' : '已生成正文 · 未通过审核或审核尚未完成' }}</strong><p>{{ previews[task.id]!.text }}</p>
            <ul v-if="previews[task.id]!.issues?.length" class="preview-issues"><li v-for="(issue, i) in previews[task.id]!.issues" :key="i"><b>{{ issue.kind === 'review_error' ? '审核未完成' : issue.kind === 'fact_boundary' ? '事实边界' : issue.kind === 'output_rule' ? '输出规则' : 'P0规则' }}：</b><q v-if="issue.quote">{{ issue.quote }}</q> {{ issue.reason }}</li></ul>
            <details v-if="previews[task.id]!.review_note"><summary>审核说明</summary><p>{{ previews[task.id]!.review_note }}</p></details>
            <details v-if="previews[task.id]!.raw_text && previews[task.id]!.raw_text !== previews[task.id]!.text"><summary>查看模型原始返回</summary><p>{{ previews[task.id]!.raw_text }}</p></details>
            <small>{{ previews[task.id]!.notice }}</small>
          </div>
        </div>
        <div class="lecture-task-actions"><button type="button" :disabled="busy || !task.valid" @click="preview(task)">{{ pending === 'preview:' + task.id ? '生成中…' : '预览讲解' }}</button><button type="button" :disabled="busy || (!task.enabled && !task.valid)" @click="mutate(task, task.enabled ? 'disable' : 'enable')">{{ task.enabled ? '停用' : '启用' }}</button><button v-if="deletePending !== task.id" class="danger" type="button" :disabled="busy" @click="deletePending = task.id">删除</button><template v-else><button class="danger" :disabled="busy" @click="mutate(task, 'delete')">确认删除</button><button :disabled="busy" @click="deletePending = 0">取消</button><small>只删除讲解任务，保留事实依据。</small></template></div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.lecture-preview.preview-rejected{border:1px solid #efc6cf;background:#fff7f8}.preview-issues{margin:12px 0;padding-left:20px;color:#ac435c;line-height:1.7}.preview-issues li+li{margin-top:7px}.lecture-preview details{margin:10px 0;font-size:13px}.lecture-preview details summary{cursor:pointer}
.lecture-panel{--ink:#243354;--accent:#675bec;color:var(--ink);margin-top:24px;min-width:0}.lecture-panel *{box-sizing:border-box}.lecture-hero{display:flex;align-items:center;gap:18px;background:linear-gradient(115deg,#eeebff,#f5faff 60%,#e9e6ff);border:1px solid #dfdfff;padding:24px 28px;border-radius:24px;position:relative;overflow:hidden}.lecture-hero-icon{display:grid;place-items:center;background:linear-gradient(140deg,#a799ff,#675bed);color:white;width:66px;height:66px;border-radius:20px;flex-shrink:0;box-shadow:0 9px 22px #7364e22a}.lecture-hero-icon svg{width:38px;fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}.lecture-hero h3{margin:0;font-size:24px;color:#162953}.lecture-hero h3 small{font-size:12px;color:#7b73b5;margin-left:8px}.lecture-hero p{margin:8px 0 0;color:#7382a3;font-size:14px}.lecture-count{margin-left:auto;flex-shrink:0;background:#ffffffb8;color:#6d61c3;padding:10px 15px;border-radius:20px;font-size:13px}.lecture-compose{margin-top:16px;background:linear-gradient(120deg,#fff,#f7f8ff);border:1px solid #e0e5ff;padding:20px 24px;border-radius:22px}.lecture-compose-head{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-bottom:12px}.lecture-compose-head strong{font-size:16px}.lecture-panel label{display:flex;align-items:center;gap:8px;font-size:13px;color:#7885a5;white-space:nowrap}.lecture-panel select,.lecture-toolbar input{background:#fff;border:1px solid #dce2fd;border-radius:10px;color:#526386;padding:8px 10px;max-width:220px}.lecture-compose textarea{width:100%;padding:15px 17px;border:1px solid #d8dfff;border-radius:14px;min-height:104px;color:var(--ink);background:#fff;line-height:1.65;font:inherit;font-size:14px;resize:vertical}.lecture-compose textarea:focus{outline:2px solid #a599ff;outline-offset:1px}.lecture-compose-foot{display:flex;align-items:center;gap:16px;margin-top:12px}.lecture-compose-foot small{margin-right:auto;color:#92a0ba;font-size:12px}.lecture-panel button{padding:10px 16px;border:1px solid #dce1ff;border-radius:12px;background:#fff;color:#665cce;font-weight:600;font-size:13px;cursor:pointer;white-space:nowrap}.lecture-panel button:disabled{opacity:.48;cursor:default}.lecture-panel button.primary{background:linear-gradient(115deg,#7990ff,#6555ea);border-color:transparent;color:#fff;box-shadow:0 5px 14px #705ae42b}.lecture-panel button.danger{border-color:#ffdae3;background:#fff6f8;color:#e76584}.lecture-working{display:flex;align-items:center;gap:10px;color:#6b62d2;font-size:13px;margin-top:16px}.lecture-working i{width:16px;height:16px;border:2px solid #ded8ff;border-top-color:#7964ed;border-radius:50%;animation:lecture-spin 1s linear infinite}.lecture-notice{color:#179777;font-size:14px;margin:14px 0 0}.lecture-error{padding:12px 16px;background:#fff1f4;border:1px solid #ffdde5;color:#bc526b;border-radius:12px;white-space:pre-wrap}.lecture-issues{margin-top:15px;background:#fff5f7;border:1px solid #ffdae3;padding:15px;border-radius:14px;font-size:13px}.lecture-issues article+article{margin-top:16px}.lecture-issues blockquote{margin:0 0 8px;border-left:3px solid #f38aa2;padding-left:10px;color:#d65376}.lecture-issues p{margin:5px 0}.lecture-issues small{color:#8f758b}.lecture-toolbar{display:flex;align-items:center;gap:18px;padding:20px 6px 12px;flex-wrap:wrap}.lecture-switch input{accent-color:#7163de;width:17px;height:17px}.lecture-toolbar>input{margin-left:auto;min-width:180px;max-width:320px;flex:1}.lecture-schedule{display:flex;align-items:center;gap:12px;color:#7b86a6;background:#f4f6ff;padding:14px 18px;border-radius:14px;font-size:13px}.lecture-schedule>span{font-size:26px;color:#8e81e8}.lecture-schedule p{margin:0}.lecture-schedule strong{display:block;color:#65769a;font-size:13px}.lecture-schedule small{display:block;margin-top:5px}.lecture-save-state{display:flex;align-items:center;justify-content:space-between;gap:15px;padding:14px 6px;color:#838eaa;font-size:13px}.lecture-task-list{display:grid;gap:14px;margin-top:16px}.lecture-task{--tone:#8264e6;--soft:#f0ebff;display:grid;grid-template-columns:105px minmax(0,1fr) auto;gap:22px;padding:20px;background:linear-gradient(110deg,#fff,#fcfdff);border:1px solid #e1e6ff;border-radius:22px;box-shadow:0 7px 20px #819dd10a;align-items:start}.lecture-task.tone-1{--tone:#dc852e;--soft:#fff0df}.lecture-task.tone-2{--tone:#0ca87a;--soft:#e4faf1}.lecture-task.tone-3{--tone:#4193ec;--soft:#e9f4ff}.lecture-task-number{display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:114px;border-radius:16px;background:var(--soft);color:var(--tone);gap:8px}.lecture-task-number b{font-size:36px;line-height:1.2}.lecture-task-number span{font-size:12px;text-align:center;padding:0 6px}.lecture-task-content{min-width:0}.lecture-task-content header{display:flex;align-items:start;gap:15px;justify-content:space-between}.lecture-task-content h4{margin:0;font-size:19px;color:#26325a}.lecture-task-status{font-size:11px;flex-shrink:0;color:#20a984;background:#eafbf4;padding:5px 9px;border-radius:20px}.lecture-task.disabled .lecture-task-status{color:#8b90aa;background:#f0f2f9}.lecture-task-content p{font-size:14px;color:#7684a4;line-height:1.65;margin:8px 0;overflow-wrap:anywhere}.lecture-task-content p strong{color:#55678e}.lecture-angles{display:flex;flex-wrap:wrap;gap:8px;margin-top:12px}.lecture-angles button{background:var(--soft);border-color:transparent;color:var(--tone);font-size:12px;padding:7px 12px}.lecture-angles button[aria-pressed=true]{border-color:var(--tone)}.lecture-task-content .lecture-angle-goal{font-size:12px}.lecture-task-content details{color:#8b96b2;font-size:12px;margin-top:12px}.lecture-task-content summary{cursor:pointer;color:var(--tone)}.lecture-task-content details li{line-height:1.7;overflow-wrap:anywhere}.lecture-task-content details blockquote{margin:10px 0;white-space:pre-wrap;background:#f7f8fe;border-radius:10px;padding:12px;line-height:1.7;overflow-wrap:anywhere}.lecture-task-actions{display:flex;flex-direction:column;gap:8px;min-width:106px}.lecture-task-actions small{max-width:110px;white-space:normal;color:#a0a8be;font-size:11px;line-height:1.5}.lecture-preview{border-radius:14px;padding:14px 18px;background:var(--soft);margin-top:15px;line-height:1.7}.lecture-preview>strong{color:var(--tone);font-size:13px}.lecture-preview p{color:#536387}.lecture-preview small{font-size:11px;color:#8995b0}.lecture-empty{text-align:center;padding:34px 20px;color:#929ab4;font-size:14px}.lecture-empty>span{display:block;color:#a895fa;font-size:30px;margin-bottom:10px}.lecture-empty>strong{font-size:16px;color:#7c82aa}.lecture-empty p{font-size:13px;line-height:1.7}.compact{margin:0;padding:18px;overflow:auto;flex:1;min-height:0}.compact .lecture-hero{padding:16px;gap:12px}.compact .lecture-hero h3{font-size:20px}.compact .lecture-hero h3 small,.compact .lecture-count{display:none}.compact .lecture-hero-icon{width:48px;height:48px;border-radius:15px}.compact .lecture-hero-icon svg{width:28px}.compact .lecture-compose{padding:16px}.compact .lecture-compose-head{align-items:start;flex-direction:column}.compact .lecture-task{grid-template-columns:60px minmax(0,1fr);gap:12px;padding:15px}.compact .lecture-task-number{min-height:76px}.compact .lecture-task-number b{font-size:25px}.compact .lecture-task-actions{grid-column:2;flex-direction:row;flex-wrap:wrap}.compact .lecture-task-content header{flex-wrap:wrap;gap:8px}.compact .lecture-toolbar{gap:10px}.compact .lecture-toolbar>input{min-width:100px}.compact .lecture-compose-foot{gap:10px;flex-wrap:wrap}.compact .lecture-compose-foot small{width:100%}@keyframes lecture-spin{to{transform:rotate(360deg)}}@media(max-width:850px){.lecture-hero{padding:20px;gap:12px}.lecture-count{display:none}.lecture-task{grid-template-columns:75px minmax(0,1fr);gap:14px}.lecture-task-actions{grid-column:2;flex-direction:row;flex-wrap:wrap}.lecture-task-content header{flex-wrap:wrap;gap:8px}.lecture-compose-head{align-items:start;flex-direction:column}.lecture-compose{padding:18px}.lecture-compose-foot{flex-wrap:wrap;gap:12px}.lecture-toolbar{gap:12px}.lecture-toolbar>input{min-width:120px}.lecture-save-state{flex-wrap:wrap}}@media(prefers-reduced-motion:reduce){.lecture-working i{animation:none}}
</style>
