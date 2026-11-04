<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { getUserVoiceCachePolicies, type UserVoiceCachePolicy } from '../api'
import { clearVoiceCleanup, listCleanupPlans, listCleanupVoices, scanVoicePolicy, listVoiceCleanupJobs, previewVoiceCleanup, voiceCleanupRequestID, type VoiceCleanupJob, type VoiceCleanupPreview, type VoiceCleanupScope } from '../voiceAssetCleanup'

const users = ref<UserVoiceCachePolicy[]>([]), tenant = ref(0), plan = ref(0)
const plans = ref<{ id: number; name: string }[]>([]), category = ref<'mainline' | 'interaction'>('mainline')
const voices=ref<{key:string;label:string}[]>([]), voice=ref(''), mode=ref<'history'|'local_only'>('history'), scan=ref<Record<string,number>|null>(null)
const preview = ref<VoiceCleanupPreview | null>(null), jobs = ref<VoiceCleanupJob[]>([])
const error = ref(''), notice = ref(''), busy = ref(false), confirming = ref(false), requestID = ref('')
const scope = computed<VoiceCleanupScope>(() => ({ tenant_id: tenant.value, plan_id: plan.value, category: category.value, voice_key:voice.value }))
let revision = 0, planLoadSequence = 0, voiceLoadSequence=0, disposed = false
const stateName: Record<string, string> = { dry_run:'自动回收候选 · 未执行删除', scanning: '扫描旧资产', processing: '清理中', completed: '清理完成', pending: '待删除', waiting_refs: '等待引用释放', retry: '失败待重试', deleting: '删除中', deleted: '已处理资产', shared_retained: '其它有效引用保留', review_required: '归属待核对' }
const label = (v: string) => stateName[v] || '等待核对'
function bytes(n = 0) { return (n / 1024 / 1024).toFixed(2) + ' MB' }
async function refreshJobs() {
  const n = revision, selected = { ...scope.value }
  if (!selected.tenant_id || !selected.plan_id) return
  try { const r = await listVoiceCleanupJobs(selected); if (!disposed && n === revision) jobs.value = r.items || [] }
  catch (e) { if (!disposed && n === revision) error.value = e instanceof Error ? e.message : '读取清理进度失败' }
}
watch([tenant, plan, category, voice, mode], () => { revision++; scan.value=null; preview.value = null; confirming.value = false; requestID.value = ''; notice.value = ''; error.value = ''; jobs.value = []; void refreshJobs() })
watch([tenant,plan,category],async()=>{
  voices.value=[]; voice.value=''; const n=++voiceLoadSequence, selected={...scope.value}
  if (!selected.tenant_id||!selected.plan_id)return
  try {const r=await listCleanupVoices(selected);if(!disposed&&n===voiceLoadSequence)voices.value=r.items||[]}
  catch(e){if(!disposed&&n===voiceLoadSequence)error.value=e instanceof Error?e.message:'读取音色失败'}
})
async function inspectPolicy(){
  if(busy.value||!tenant.value||!plan.value)return
  busy.value=true;error.value='';const n=revision
  try{const r=await scanVoicePolicy({...scope.value});if(!disposed&&n===revision)scan.value=r.counts}
  catch(e){if(!disposed&&n===revision)error.value=e instanceof Error?e.message:'盘点失败，未执行删除'}
  finally{if(!disposed)busy.value=false}
}
watch(tenant, async value => {
  plan.value = 0; plans.value = []; const n = ++planLoadSequence
  if (!value) return
  try { const r = await listCleanupPlans(value); if (!disposed && n === planLoadSequence && tenant.value === value) plans.value = r.items || [] }
  catch (e) { if (!disposed && n === planLoadSequence) error.value = e instanceof Error ? e.message : '读取方案失败' }
})
async function inspect() {
  if (busy.value || !tenant.value || !plan.value) return
  busy.value = true; error.value = ''; notice.value = ''; preview.value=null; requestID.value=''; const n = revision
  try { const r = await previewVoiceCleanup({ ...scope.value },mode.value); if (!disposed && n === revision) { preview.value = r; requestID.value = voiceCleanupRequestID(); confirming.value = false } }
  catch (e) { if (!disposed && n === revision) error.value = e instanceof Error ? e.message : '读取清理清单失败' }
  finally { if (!disposed) busy.value = false }
}
async function clear() {
  if (busy.value || !preview.value || !confirming.value) return
  const selected = preview.value, id = requestID.value, n = revision
  busy.value = true; error.value = ''
  try {
    await clearVoiceCleanup(selected, id, mode.value)
    if (!disposed && n === revision) { confirming.value = false; preview.value = null; notice.value = mode.value==='local_only'?'本地副本清理已提交；有效OSS归档仍可恢复，请查看进度。':'旧声音已禁止新使用。后台将在播放和共享引用释放后删除文件，请查看下方进度。'; await refreshJobs() }
  } catch (e) { if (!disposed && n === revision) error.value = e instanceof Error ? e.message : '提交失败；请使用本页同一操作重试' }
  finally { if (!disposed) busy.value = false }
}
onMounted(async () => { try { const r = await getUserVoiceCachePolicies(); if (!disposed) users.value = r.items || [] } catch (e) { if (!disposed) error.value = e instanceof Error ? e.message : '读取用户失败' } })
onBeforeUnmount(() => { disposed = true; revision++ })
</script>

<template>
  <section class="voice-cleanup" aria-labelledby="voice-cleanup-title">
    <header><h2 id="voice-cleanup-title">方案历史声音清理</h2><p>清除后禁止从本地或OSS恢复旧声音，再按实际引用情况回收文件。</p></header>
    <div class="scope-fields">
      <label>用户<select v-model.number="tenant" :disabled="busy"><option :value="0">请选择用户</option><option v-for="u in users" :key="u.tenant_id" :value="u.tenant_id">{{ u.display_name || u.username }}</option></select></label>
      <label>方案<select v-model.number="plan" :disabled="busy || !tenant"><option :value="0">请选择方案</option><option v-for="p in plans" :key="p.id" :value="p.id">{{ p.name }}</option></select></label>
      <label>声音类型<select v-model="category" :disabled="busy"><option value="mainline">主线原文及改写</option><option value="interaction">互动成品</option></select></label>
      <label>音色范围<select v-model="voice" :disabled="busy"><option value="">全部音色（含旧记录）</option><option v-for="v in voices" :key="v.key" :value="v.key">{{ v.label }}</option></select></label>
      <label>清理方式<select v-model="mode" :disabled="busy"><option value="history">清除历史声音（本地＋OSS）</option><option value="local_only">仅清理本地副本，允许从OSS恢复</option></select></label>
      <button type="button" :disabled="busy || !tenant || !plan" @click="inspect">{{ busy ? '处理中…' : '预览清理范围' }}</button>
    </div>
    <p class="note">上传原始录音、克隆样本和直播录音保留。已经下发或正在播放的声音会等待实际引用释放。清除后重新生成声音可能产生正常合成费用。</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <div v-if="preview" class="preview">
      <h3>{{ preview.plan_name }} · {{ category === 'mainline' ? '主线原文及改写' : '互动成品' }}</h3>
      <p>涉及直播间：{{ preview.room_ids?.length ? preview.room_ids.join('、') : '暂无绑定直播间' }}。范围：{{ voice ? (voices.find(v=>v.key===voice)?.label || '选定完整音色') : '全部音色' }}。</p>
      <p>候选资产 {{ preview.counts.candidate_records || 0 }} 条；归属待核对 {{ preview.counts.review_required || 0 }} 条。</p>
      <p>去重登记对象 {{ preview.counts.object_count_unverified || 0 }} 个，登记容量 {{ bytes(preview.counts.expected_bytes_unverified) }}。</p>
      <p v-if="preview.physical_inventory_complete">完整分页核实当前存在 {{ preview.counts.confirmed_objects||0 }} 个，当前容量 {{ bytes(preview.counts.confirmed_current_bytes) }}；确认缺失 {{ preview.counts.confirmed_missing_objects||0 }} 个。桶版本状态：{{ preview.bucket_version_state==='disabled'?'未启用':preview.bucket_version_state==='enabled'?'已启用':'待核对' }}。历史版本与独立备份未计为已释放。</p>
      <p v-else>仅清理本地副本，OSS内容保持有效。</p>
      <p v-if="category === 'interaction'">供应商持有的源文件和已归档直播录音不会被此操作删除；本系统的互动复用资格会失效。</p>
      <button v-if="!confirming" type="button" class="danger" :disabled="busy" @click="confirming = true">{{ mode==='local_only'?'清理本地副本':'清除这些历史声音' }}</button>
      <div v-else class="confirmation"><p>{{ mode==='local_only'?'确认仅清理此范围的本地副本？有效OSS归档可以恢复。':'确认清除上述固定范围的历史声音？删除后不能继续复用旧文件。' }}</p><button type="button" :disabled="busy" @click="confirming = false">取消</button><button type="button" class="danger" :disabled="busy" @click="clear">{{ busy ? '提交中…' : '确认清除' }}</button></div>
    </div>
    <div v-if="tenant && plan" class="progress"><h3>自动回收盘点</h3><p>退役改写观察7天；旧音色或正文连续30天无真实使用后再观察7天；上传孤儿需超过24小时且两次独立扫描。当前只生成清单，不自动删除。使用时间未知、当前池与共享引用保留核对。</p><button type="button" :disabled="busy" @click="inspectPolicy">生成自动回收清单</button><p v-if="scan">闲置候选 {{ scan.idle_candidate||0 }}；观察中 {{ scan.idle_observation||0 }}；上传孤儿候选 {{ scan.orphan_upload_candidates||0 }}；引用保护 {{ scan.protected_reference||0 }}；使用未知 {{ scan.usage_unknown_review||0 }}。</p></div>
    <div v-if="tenant && plan" class="progress"><header><h3>清理进度</h3><button type="button" :disabled="busy" @click="refreshJobs">刷新进度</button></header>
      <p v-if="!jobs.length" class="note">当前方案暂无清理记录。</p>
      <article v-for="job in jobs" :key="job.id"><p><strong>{{ label(job.state) }}</strong> · {{ job.mode==='local_only'?'仅本地副本':'本地与OSS' }} · {{ job.scope.category === 'mainline' ? '主线' : '互动' }} · {{ new Date(job.created_at).toLocaleString('zh-CN') }}</p><p>已处理资产 {{ job.counts?.deleted || 0 }}；等待引用 {{ job.counts?.waiting_refs || 0 }}；失败待重试 {{ job.counts?.retry || 0 }}；其它引用保留 {{ job.counts?.shared_retained || 0 }}；待核对 {{ job.counts?.review_required || 0 }}。</p><p>确认删除的当前对象登记容量：{{ bytes(job.counts?.confirmed_bytes) }}。桶历史版本及独立备份另行核对。</p></article>
    </div>
  </section>
</template>

<style scoped>
.voice-cleanup{background:#fff;border:1px solid #d9dee7;border-radius:12px;padding:24px;margin-top:24px;font-size:16px;min-width:0}.voice-cleanup h2{margin:0 0 10px;font-size:22px}.voice-cleanup h3{font-size:18px}.scope-fields{display:flex;flex-wrap:wrap;gap:16px;align-items:end;margin:20px 0}.scope-fields label{display:grid;gap:8px;min-width:180px;flex:1}.voice-cleanup select,.voice-cleanup button{font:inherit;min-height:42px;border:1px solid #cbd2dc;border-radius:6px;padding:8px 12px}.voice-cleanup select{width:100%;background:#fff}.voice-cleanup button{cursor:pointer;background:#f8fafc}.voice-cleanup button:disabled{opacity:.55;cursor:default}.voice-cleanup .danger{background:#a52e28;color:#fff;border-color:#a52e28}.note{color:#596579;line-height:1.65}.error{color:#a52e28}.notice{color:#176647}.preview,.progress{border-top:1px solid #e2e6ec;margin-top:20px;padding-top:12px}.confirmation{background:#fff6ee;padding:14px}.confirmation button+button{margin-left:12px}.progress header{display:flex;justify-content:space-between;align-items:center;gap:12px}.progress article{padding:12px 0;border-bottom:1px solid #edf0f4;overflow-wrap:anywhere}@media(max-width:600px){.voice-cleanup{padding:16px}.scope-fields{display:grid}.scope-fields label{min-width:0}}
</style>
