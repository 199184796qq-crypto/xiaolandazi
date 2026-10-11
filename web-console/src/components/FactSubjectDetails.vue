<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { editLectureDetail, getLectures, type LectureDetail, type LectureTask } from '../lectureTasks'
const props = defineProps<{ planId: number; tenantId?: number; factId: number; editRequested?: boolean }>()
const opened = ref(false), loading = ref(false), error = ref(''), notice = ref(''), tasks = ref<LectureTask[]>([])
const panel = ref<HTMLElement | null>(null), editing = ref(''), pending = ref(false), draft = ref({ name: '', text: '' })
let epoch = 0, loadNo = 0
const key = (task: LectureTask, fact: LectureDetail) => `${task.id}/${fact.storage || 'plan'}/${fact.id}`
watch(() => [props.planId, props.tenantId, props.factId], () => { epoch++; opened.value = false; tasks.value = []; error.value = ''; notice.value = ''; loading.value = false; editing.value = ''; pending.value = false })
async function load() {
 const owner = epoch, attempt = ++loadNo; loading.value = true; error.value = ''
 try { const lib = await getLectures(props.planId, props.tenantId); if (owner === epoch && attempt === loadNo) tasks.value = lib.tasks.filter(t => t.root_fact?.id === props.factId) }
 catch (e) { if (owner === epoch && attempt === loadNo) error.value = e instanceof Error ? e.message : '读取二级资料失败' }
 finally { if (owner === epoch && attempt === loadNo) loading.value = false }
}
async function open() { opened.value = true; await load(); await nextTick(); panel.value?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }) }
watch(() => props.editRequested, value => { if (value) void open() }, { immediate: true })
function edit(task: LectureTask, fact: LectureDetail) { if (pending.value) return; editing.value = key(task, fact); draft.value = { name: fact.name, text: fact.text }; error.value = ''; notice.value = '' }
async function save(task: LectureTask, fact: LectureDetail) {
 if (pending.value || !draft.value.name.trim() || !draft.value.text.trim()) return
 const owner = epoch; pending.value = true; error.value = ''
 try {
  const result = await editLectureDetail(props.planId, props.tenantId, task, fact, draft.value.name, draft.value.text)
  if (owner !== epoch) return
  notice.value = result.notice; editing.value = ''
  window.dispatchEvent(new CustomEvent('live-lecture-tasks-updated', { detail: { plan_id: props.planId, facts_changed: true } }))
  await load()
 } catch (e) { if (owner === epoch) error.value = e instanceof Error ? e.message : '保存未完成，输入已保留' }
 finally { if (owner === epoch) pending.value = false }
}
onBeforeUnmount(() => { epoch++ })
</script>
<template>
 <div ref="panel" class="fact-subject-details">
  <button class="toggle" type="button" :aria-expanded="opened" :disabled="pending" @click="opened ? opened = false : open()">{{ opened ? '收起二级资料' : '查看与修改二级资料' }}</button>
  <section v-if="opened" class="subject-content" aria-label="二级事实资料面板">
   <header><div><strong>二级事实 · 对象资料</strong><p>按资料对象管理，用于任务讲解。各条资料可以单独修改。</p></div></header>
   <p v-if="loading" role="status">正在读取…</p><p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
   <section v-for="task in tasks" :key="task.id" class="subject">
    <div class="subject-heading"><strong>{{ task.subject || task.title }}</strong><span>{{ task.facts.length }} 条资料 · {{ task.links.length ? task.links.join('、') + '号商品' : '当前方案公共资料' }}</span></div>
    <p class="relation"><b>与商品的关系：</b>{{ task.root_fact?.text }}</p><small v-if="!task.valid" class="error">资料关联已有变化，请核对关联后再讲解。</small>
    <div class="detail-grid"><article v-for="f in task.facts" :key="key(task, f)" class="detail" :class="{ editing: editing === key(task, f) }">
     <template v-if="editing === key(task, f)"><label>资料名称<input v-model="draft.name" maxlength="100" :disabled="pending"/></label><label>已确认的对象事实<textarea v-model="draft.text" rows="4" maxlength="4000" :disabled="pending"/></label><small>{{ draft.text.length }}/4000 · 保留对象和适用条件</small><div class="actions"><button class="primary" type="button" :disabled="pending || !draft.name.trim() || !draft.text.trim()" @click="save(task, f)">{{ pending ? '整理并复核中…' : '保存资料' }}</button><button type="button" :disabled="pending" @click="editing = ''">取消</button></div></template>
     <template v-else><header><strong>{{ f.name }}</strong><button type="button" :aria-label="'修改二级资料：' + f.name" :disabled="pending" @click="edit(task, f)">修改</button></header><p>{{ f.text }}</p><details v-if="f.quote && f.quote !== f.text"><summary>原始素材</summary><p>{{ f.quote }}</p></details></template>
    </article></div>
   </section>
   <p v-if="!loading && !error && !tasks.length">暂无已关联的二级资料，请在任务讲解板块整理关联。</p>
  </section>
 </div>
</template>
<style scoped>
.fact-subject-details{font-size:14px;color:#344d76;white-space:normal}.fact-subject-details button{padding:8px 13px;border:1px solid #d8dff6;border-radius:9px;color:#655ac9;background:#fff;font:inherit;cursor:pointer}.fact-subject-details button:disabled{opacity:.55;cursor:wait}.toggle{margin:6px 0}.subject-content{margin-top:8px;padding:22px;border:1px solid #dfdef7;border-radius:17px;background:linear-gradient(125deg,#fbfaff,#f3f7ff);line-height:1.6}.subject-content>header,.detail>header,.subject-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:14px}.subject-content>header strong{font-size:18px;color:#252d62}.subject-content>header p{margin:5px 0 14px;color:#7884a1}.subject+.subject{margin-top:22px}.subject-heading{margin-top:10px}.subject-heading strong{font-size:17px}.subject-heading span,.relation{color:#7382a2;font-size:13px}.detail-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin-top:14px}.detail{padding:16px 18px;border:1px solid #e1e6f5;background:white;border-radius:13px;min-width:0}.detail header button{flex-shrink:0}.detail p{margin:10px 0 0;white-space:pre-wrap;overflow-wrap:anywhere}.detail label{display:grid;gap:6px;margin-bottom:12px;color:#52688d}.detail input,.detail textarea{box-sizing:border-box;width:100%;padding:10px;border:1px solid #cdd9f6;border-radius:9px;font:inherit;color:#334c76;line-height:1.65;background:white}.detail textarea{resize:vertical}.detail small,.detail details{color:#8390a8;font-size:12px}.detail details{margin-top:10px}.actions{display:flex;gap:10px;justify-content:flex-end;margin-top:12px}.fact-subject-details .primary{background:#6b63df;color:white;border-color:transparent}.editing{border-color:#a8a1f2;box-shadow:0 0 0 2px #e8e6ff}.notice{color:#24826b}.error{color:#ad4761}@media(max-width:800px){.detail-grid{grid-template-columns:1fr}.subject-content{padding:16px}.subject-content>header{flex-direction:column}}
</style>
