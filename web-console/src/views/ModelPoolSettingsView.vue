<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { request } from '../api'

interface Profile { id: string; name: string; protocol: string; base_url: string; model: string; enabled: boolean; timeout_ms: number; max_tokens: number; system_prompt: string; api_key?: string }
interface Entry { id: string; account_name: string; profile: Profile; max_rooms: number; enabled: boolean; revision: number; used_rooms: number; available_rooms: number; has_api_key: boolean }
interface Lease { lease_id: string; entry_id: string; account_name: string; model: string; tenant_id: number; room_id: number; runtime_id: number; tenant_name: string; room_name: string; ai_status: string; acquired_at: string }
interface Pool { entries: Entry[]; leases: Lease[] }
interface Draft { id: string; account_name: string; profile: Profile; max_rooms: number | ''; enabled: boolean; revision: number }
const entries = ref<Entry[]>([]), leases = ref<Lease[]>([])
const loading = ref(false), saving = ref(false), listing = ref(false), editor = ref(false)
const error = ref(''), notice = ref(''), deleteTarget = ref<Entry | null>(null), occupiedEntry = ref('')
const models = ref<string[]>([]), manualModel = ref(false)
function emptyDraft(): Draft { return { id: '', account_name: '', profile: { id: '', name: '', protocol: 'openai_chat', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: '', enabled: true, timeout_ms: 120000, max_tokens: 6000, system_prompt: '', api_key: '' }, max_rooms: '', enabled: true, revision: 0 } }
const draft = ref<Draft>(emptyDraft())
const total = computed(() => entries.value.reduce((a, e) => a + (e.enabled ? e.max_rooms : 0), 0))
const available = computed(() => entries.value.reduce((a, e) => a + (e.enabled ? e.available_rooms : 0), 0))
const visibleLeases = computed(() => leases.value.filter(l => l.entry_id === occupiedEntry.value))
const occupiedAccount = computed(() => entries.value.find(e => e.id === occupiedEntry.value))
const occupancyPage = ref(1), occupancyPageSize = ref(10)
const occupancyPageCount = computed(() => Math.max(1, Math.ceil(visibleLeases.value.length / occupancyPageSize.value)))
const pageLeases = computed(() => visibleLeases.value.slice((occupancyPage.value - 1) * occupancyPageSize.value, occupancyPage.value * occupancyPageSize.value))
const occupancyDialog = ref<HTMLElement | null>(null), occupancyClose = ref<HTMLButtonElement | null>(null)
let occupancyOpener: HTMLElement | null = null, previousBodyOverflow = ''
watch(occupancyPageSize, () => { occupancyPage.value = 1 })
watch(occupancyPageCount, count => { occupancyPage.value = Math.min(occupancyPage.value, count) })
async function openOccupancy(entry: Entry, event: MouseEvent) {
  if (busy.value) return
  occupancyOpener = event.currentTarget as HTMLElement
  previousBodyOverflow = document.body.style.overflow
  document.body.style.overflow = 'hidden'
  occupiedEntry.value = entry.id; occupancyPage.value = 1
  await nextTick(); occupancyClose.value?.focus()
  await load()
}
function closeOccupancy() {
  if (!occupiedEntry.value) return
  occupiedEntry.value = ''; document.body.style.overflow = previousBodyOverflow
  if (occupancyOpener?.isConnected) occupancyOpener.focus()
  occupancyOpener = null
}
function occupancyKeydown(event: KeyboardEvent) {
  if (!occupiedEntry.value) return
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeOccupancy(); return }
  if (event.key !== 'Tab' || !occupancyDialog.value) return
  const controls = [...occupancyDialog.value.querySelectorAll<HTMLElement>('button:not(:disabled), select:not(:disabled), [tabindex="0"]')]
  const first = controls[0], last = controls.at(-1)
  if (!first || !last) return
  if (!occupancyDialog.value.contains(document.activeElement)) { event.preventDefault(); first.focus() }
  else if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
}
onMounted(() => document.addEventListener('keydown', occupancyKeydown, true))
onUnmounted(() => { document.removeEventListener('keydown', occupancyKeydown, true); closeOccupancy() })
const busy = computed(() => loading.value || saving.value || listing.value)
const canSave = computed(() => !!draft.value.account_name.trim() && !!draft.value.profile.model.trim() && draft.value.max_rooms !== '' && Number.isInteger(Number(draft.value.max_rooms)) && Number(draft.value.max_rooms) >= (draft.value.enabled ? 1 : 0) && (!!draft.value.id || !!draft.value.profile.api_key?.trim()))
function fail(e: unknown, fallback: string) { error.value = e instanceof Error ? e.message : fallback }
async function load() {
  if (saving.value || listing.value) return
  loading.value = true; error.value = ''
  try { const data = await request<Pool>('/api/v1/system/model-pool', { suppressErrorDialog: true }); entries.value = data.entries; leases.value = data.leases }
  catch (e) { fail(e, '读取模型池失败') }
  finally { loading.value = false }
}
function edit(e?: Entry) {
  if (busy.value) return
  draft.value = e ? { id: e.id, account_name: e.account_name, profile: { ...e.profile, api_key: '' }, max_rooms: e.max_rooms, enabled: e.enabled, revision: e.revision } : emptyDraft()
  models.value = e ? [e.profile.model] : []; manualModel.value = false; editor.value = true; error.value = ''; notice.value = ''
}
function closeEditor() { if (!saving.value && !listing.value) { editor.value = false; draft.value = emptyDraft(); models.value = [] } }
async function listModels() {
  if (busy.value) return
  listing.value = true; error.value = ''
  try {
    const result = await request<{ models: { id: string }[] }>('/api/v1/system/model-pool/models', { method: 'POST', body: JSON.stringify({ id: draft.value.id, profile: draft.value.profile }), suppressErrorDialog: true })
    models.value = [...new Set(result.models.map(m => m.id))].sort()
    if (draft.value.profile.model && !models.value.includes(draft.value.profile.model)) models.value.unshift(draft.value.profile.model)
    notice.value = `已读取 ${result.models.length} 个可选模型，请选择口播与审核使用的文字模型。`
  } catch (e) { fail(e, '读取模型列表失败') }
  finally { listing.value = false }
}
async function save() {
  if (!canSave.value || busy.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    await request<Entry>('/api/v1/system/model-pool/entries', { method: 'POST', body: JSON.stringify({ ...draft.value, max_rooms: Number(draft.value.max_rooms) }), suppressErrorDialog: true })
    draft.value.profile.api_key = ''; editor.value = false; draft.value = emptyDraft(); notice.value = '配置已保存。新的AI启动按此分配；已占用的房间保留原账号和模型。'
  } catch (e) { fail(e, '保存模型池失败') }
  finally { saving.value = false }
  if (!editor.value) await load()
}
async function importCurrent() {
  if (busy.value || editor.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const entry = await request<Entry>('/api/v1/system/model-pool/import-current', { method: 'POST', body: '{}', suppressErrorDialog: true })
    saving.value = false; await load(); edit(entry); draft.value.max_rooms = ''; draft.value.enabled = true
    notice.value = '账号已安全导入。请手动填写直播间名额后启用，无需再次输入原Key。'
  } catch (e) { fail(e, '导入账号失败') }
  finally { saving.value = false }
}
async function remove() {
  const e = deleteTarget.value
  if (!e || busy.value) return
  saving.value = true; error.value = ''
  try { await request(`/api/v1/system/model-pool/entries/${encodeURIComponent(e.id)}?revision=${e.revision}`, { method: 'DELETE', suppressErrorDialog: true }); deleteTarget.value = null; notice.value = '账号模型配置已删除。' }
  catch (err) { fail(err, '删除失败') }
  finally { saving.value = false }
  if (!deleteTarget.value) await load()
}
function timeLabel(raw: string) { return new Date(raw).toLocaleString('zh-CN', { hour12: false }) }
onMounted(load)
</script>

<template>
  <section class="model-pool-page">
    <header><div><RouterLink to="/system/settings/intelligence">← 返回 AI 与智能体</RouterLink><h1>模型池配置</h1><p>按账号分配模型与直播间名额，AI停止后归还；暂停保留当前分配。</p></div><button :disabled="busy" @click="load">{{ loading ? '读取中…' : '刷新状态' }}</button></header>
    <p v-if="error" class="message error" role="alert">{{ error }}</p><p v-if="notice" class="message success" role="status">{{ notice }}</p>
    <div class="summary"><article><span>账号模型配置</span><strong>{{ entries.length }} <small>条</small></strong></article><article><span>已分配直播间</span><strong>{{ leases.length }} <small>个</small></strong></article><article><span>可分配名额</span><strong>{{ available }} <small>/ 启用名额 {{ total }}</small></strong></article></div>
    <div class="list-toolbar"><div><h2>账号池列表</h2><p>同一账号、同一模型共用额度。更换这个账号的Key请编辑已有记录。</p></div><div class="actions"><button :disabled="busy || editor" @click="importCurrent">导入当前口播账号</button><button class="primary" :disabled="busy" @click="edit()">＋ 添加账号模型</button></div></div>
    <div class="table-scroll"><table><thead><tr><th>账号名称</th><th>模型</th><th>Key</th><th>直播间名额</th><th>已分配 / 剩余</th><th>分配状态</th><th>操作</th></tr></thead><tbody><tr v-for="e in entries" :key="e.id"><td>{{ e.account_name }}</td><td class="model-name">{{ e.profile.model }}</td><td><span class="tag">{{ e.has_api_key ? '已配置' : '待配置' }}</span></td><td>{{ e.max_rooms || '待设置' }}</td><td>{{ e.used_rooms }} / {{ e.available_rooms }}</td><td><span class="tag" :class="{ enabled: e.enabled }">{{ e.enabled ? '已启用' : '暂停分配' }}</span></td><td><div class="row-actions"><button :disabled="busy" @click="edit(e)">编辑</button><button :disabled="busy" @click="openOccupancy(e, $event)">查看占用</button><button class="danger" :disabled="busy || e.used_rooms > 0" @click="deleteTarget = e">删除</button></div></td></tr><tr v-if="!entries.length"><td colspan="7" class="empty">还没有账号模型。添加账号，或导入当前口播账号，再填写直播间名额。</td></tr></tbody></table></div>
    <aside>每次AI启动领取一次账号和模型，口播、审核及该房间运行中的规划使用同一分配。名额满时提示等待，不超额分配。Key加密保存，页面不回显完整Key。</aside>
    <div v-if="occupiedEntry" class="overlay occupancy-overlay" @click.self="closeOccupancy">
      <section ref="occupancyDialog" class="occupancy-dialog" role="dialog" aria-modal="true" aria-labelledby="occupancy-title">
        <header><div><h2 id="occupancy-title">占用详情</h2><p>{{ occupiedAccount?.account_name }} · {{ occupiedAccount?.profile.model }}</p></div><button ref="occupancyClose" aria-label="关闭占用详情" @click="closeOccupancy">×</button></header>
        <div class="occupancy-toolbar"><span>共 {{ visibleLeases.length }} 个房间 · 账号名额 {{ occupiedAccount?.max_rooms ?? 0 }}</span><button :disabled="busy" @click="load">{{ loading ? '读取中…' : '刷新状态' }}</button></div>
        <p v-if="error" class="message error" role="alert">{{ error }}</p>
        <div class="table-scroll occupancy-table"><table><thead><tr><th>客户</th><th>直播间</th><th>AI状态</th><th>分配时间</th></tr></thead><tbody><tr v-for="l in pageLeases" :key="l.lease_id"><td data-label="客户"><div>{{ l.tenant_name || `客户 ${l.tenant_id}` }}</div></td><td data-label="直播间"><div><span>{{ l.room_name || `房间 ${l.room_id}` }}</span><small>房间 {{ l.room_id }}</small></div></td><td data-label="AI状态"><div><span class="tag" :class="{ enabled: l.ai_status === 'running' }">{{ l.ai_status === 'paused' ? '已暂停 · 保留名额' : '运行中' }}</span></div></td><td data-label="分配时间"><div>{{ timeLabel(l.acquired_at) }}</div></td></tr><tr v-if="!pageLeases.length"><td colspan="4" class="empty">{{ loading ? '读取占用详情中…' : '这个账号当前没有占用房间。' }}</td></tr></tbody></table></div>
        <footer class="occupancy-pagination"><label>每页显示<select v-model.number="occupancyPageSize" aria-label="每页显示房间数"><option :value="10">10 个</option><option :value="20">20 个</option><option :value="50">50 个</option></select></label><nav aria-label="占用详情分页"><button :disabled="loading || occupancyPage <= 1" @click="occupancyPage--">上一页</button><span aria-live="polite">第 {{ occupancyPage }} / {{ occupancyPageCount }} 页</span><button :disabled="loading || occupancyPage >= occupancyPageCount" @click="occupancyPage++">下一页</button></nav></footer>
      </section>
    </div>
    <div v-if="editor" class="overlay" @click.self="closeEditor"><form class="editor" @submit.prevent="save"><header><h2>{{ draft.id ? '编辑账号模型' : '添加账号模型' }}</h2><button type="button" :disabled="saving || listing" aria-label="关闭编辑" @click="closeEditor">×</button></header><p v-if="error" class="message error" role="alert">{{ error }}</p><p v-if="notice" class="message success">{{ notice }}</p><fieldset :disabled="saving || listing"><label>账号名称<input v-model="draft.account_name" maxlength="80" required placeholder="例如：阿里云账号A，同一账号保持同一名称"></label><label>账号 API Key<input v-model="draft.profile.api_key" type="password" autocomplete="new-password" :required="!draft.id" :placeholder="draft.id ? '已配置，留空保留原Key' : '输入这个账号的API Key'"></label><div class="form-grid"><label>接口协议<select v-model="draft.profile.protocol"><option value="openai_chat">OpenAI兼容对话接口</option><option value="openai_responses">OpenAI Responses接口</option><option value="anthropic">Anthropic接口</option><option value="gemini">Gemini接口</option></select></label><label>接口地址<input v-model="draft.profile.base_url" type="url" required placeholder="HTTPS API地址"></label></div><div class="model-picker"><label>使用模型<select v-if="!manualModel" v-model="draft.profile.model" required><option value="" disabled>读取模型列表后选择</option><option v-for="m in models" :key="m" :value="m">{{ m }}</option></select><input v-else v-model="draft.profile.model" required placeholder="例如 qwen3.7-flash"></label><button type="button" :disabled="busy" @click="listModels">{{ listing ? '读取中…' : '读取可选模型' }}</button></div><label class="checkbox"><input v-model="manualModel" type="checkbox">手动填写模型编号</label><div class="form-grid"><label>最多分配直播间数<input v-model.number="draft.max_rooms" type="number" :min="draft.enabled ? 1 : 0" max="10000" step="1" required placeholder="手动填写 X"></label><label>分配状态<select v-model="draft.enabled"><option :value="true">启用新分配</option><option :value="false">暂停新分配</option></select></label></div><p class="hint">暂停新分配不会收回正在使用的名额。更换Key只影响之后新启动的AI。</p></fieldset><footer><button type="button" :disabled="saving || listing" @click="closeEditor">取消</button><button class="primary" type="submit" :disabled="!canSave || busy">{{ saving ? '保存中…' : '保存配置' }}</button></footer></form></div>
    <div v-if="deleteTarget" class="overlay" @click.self="!saving && (deleteTarget = null)"><section class="delete-dialog" role="alertdialog" aria-modal="true"><h2>删除账号模型配置？</h2><p>{{ deleteTarget.account_name }} · {{ deleteTarget.profile.model }}</p><p>仅可删除没有AI占用的记录。</p><footer><button :disabled="saving" @click="deleteTarget = null">取消</button><button class="danger" :disabled="saving" @click="remove">确认删除</button></footer></section></div>
  </section>
</template>

<style scoped>
.model-pool-page{width:100%;max-width:1460px;min-width:0;margin:auto;padding:24px;box-sizing:border-box;color:#243454}.model-pool-page h1{font-size:28px;margin:16px 0 8px}.model-pool-page h2{font-size:20px;margin:0 0 8px}.model-pool-page p,.model-pool-page aside{color:#7c8aa3;line-height:1.7}.model-pool-page a{color:#6554eb;text-decoration:none}.model-pool-page header,.list-toolbar,.actions,.row-actions,.model-picker,.model-pool-page footer{display:flex;align-items:center;justify-content:space-between;gap:12px}.model-pool-page button{border:1px solid #dce3f4;border-radius:10px;background:#fff;color:#4e6082;font:inherit;font-weight:700;padding:11px 16px;cursor:pointer;white-space:nowrap}.model-pool-page button:disabled{opacity:.45;cursor:default}.model-pool-page .primary{background:linear-gradient(120deg,#8472ff,#5143ff);border-color:transparent;color:#fff}.model-pool-page .danger{color:#d84d66}.summary{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:18px;margin:22px 0 30px}.summary article{display:grid;gap:14px;padding:22px;border:1px solid #e3e8f8;border-radius:16px;background:#fff}.summary span{color:#7c8aa3}.summary strong{font-size:28px}.summary small{font-size:14px;font-weight:400;color:#8a96af}.list-toolbar{margin:24px 0 16px;flex-wrap:wrap}.list-toolbar p{margin:0}.table-scroll{overflow-x:auto;border:1px solid #e1e7f7;border-radius:16px;background:#fff}.model-pool-page table{width:100%;border-collapse:collapse;min-width:800px;text-align:left}.model-pool-page th{background:#f6f7fd;color:#7a89a6;font-weight:600}.model-pool-page th,.model-pool-page td{padding:16px;vertical-align:middle;border-bottom:1px solid #eef1fa}.model-name{overflow-wrap:anywhere}.model-pool-page td small{display:block;color:#8996ae;margin-top:5px}.model-pool-page tr:last-child td{border-bottom:0}.row-actions{justify-content:flex-start;gap:6px}.row-actions button{padding:8px 10px}.tag{padding:5px 10px;border-radius:8px;background:#f0efff;color:#7d6dec;font-size:13px;white-space:nowrap}.tag.enabled{background:#e8fbf4;color:#22a377}.empty{text-align:center;color:#8b98b2;height:100px}.model-pool-page aside{padding:18px 22px;background:#f0f3ff;border-radius:14px;margin-top:22px;font-size:14px}.message{padding:12px 16px;border-radius:10px}.model-pool-page .error{background:#fff0f2;color:#bd4054}.model-pool-page .success{background:#eafff5;color:#258864}.overlay{position:fixed;inset:0;z-index:1000;background:#20325266;display:flex;align-items:center;justify-content:center;padding:20px;box-sizing:border-box}.editor,.delete-dialog{width:min(760px,100%);max-height:calc(100dvh - 40px);overflow-y:auto;background:white;padding:26px;border-radius:20px;box-sizing:border-box;box-shadow:0 20px 70px #18294635}.editor fieldset{border:0;padding:0;margin:18px 0;min-width:0}.editor label{display:grid;gap:8px;font-weight:600;min-width:0;margin:14px 0}.editor input,.editor select{width:100%;min-width:0;box-sizing:border-box;font:inherit;border:1px solid #d9e1f2;border-radius:10px;background:#fafbff;color:#293c61;padding:12px}.form-grid{display:grid;grid-template-columns:1fr 1fr;gap:18px}.model-picker{align-items:end}.model-picker label{flex:1}.model-picker button{margin-bottom:14px}.editor .checkbox{display:flex;align-items:center;font-weight:400;font-size:14px;color:#8290ab}.checkbox input{width:17px}.editor .hint{font-size:13px}.editor footer,.delete-dialog footer{justify-content:flex-end}@media(max-width:700px){.model-pool-page{padding:16px}.model-pool-page h1{font-size:24px}.summary{gap:8px}.summary article{padding:14px}.summary strong{font-size:22px}.summary small{display:block;font-size:12px}.actions{width:100%;flex-wrap:wrap}.form-grid{grid-template-columns:1fr;gap:0}.overlay{padding:10px}.editor{padding:18px;max-height:calc(100dvh - 20px)}.model-picker{flex-wrap:wrap}.model-picker label{flex-basis:100%}}
</style>

<style scoped>
.occupancy-dialog{display:flex;flex-direction:column;width:min(1040px,100%);min-width:0;max-height:calc(100dvh - 40px);padding:26px;background:#fff;border-radius:20px;box-shadow:0 20px 70px #18294635;box-sizing:border-box;gap:18px}.occupancy-dialog header{align-items:flex-start}.occupancy-dialog header>div{min-width:0}.occupancy-dialog header p{margin:0;overflow-wrap:anywhere}.occupancy-toolbar{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap;color:#7a89a6;font-size:14px}.occupancy-table{min-height:0;overflow:auto}.occupancy-table table{min-width:700px}.occupancy-dialog .message{margin:0}.occupancy-pagination{flex-wrap:wrap;flex-shrink:0}.occupancy-pagination label,.occupancy-pagination nav{display:flex;align-items:center;gap:12px;font-size:14px;color:#6e7e9b}.occupancy-pagination select{font:inherit;color:#354b73;background:#fafbff;border:1px solid #dce3f4;border-radius:8px;padding:9px}.occupancy-pagination nav span{white-space:nowrap}.occupancy-pagination button{padding:9px 14px}@media(max-width:700px){.occupancy-dialog{max-height:calc(100dvh - 20px);padding:18px;gap:14px}.occupancy-table table{min-width:0}.occupancy-table thead{display:none}.occupancy-table tbody,.occupancy-table tr{display:block}.occupancy-table tr{padding:10px 14px;border-bottom:1px solid #eef1fa}.occupancy-table td{display:flex;padding:8px 0;border:0;gap:10px;overflow-wrap:anywhere}.occupancy-table td:before{content:attr(data-label);flex:0 0 72px;color:#8996ae;font-size:13px}.occupancy-table td>div{min-width:0;flex:1}.occupancy-table .empty{display:block;height:auto;padding:24px 0}.occupancy-table .empty:before{display:none}.occupancy-pagination{gap:14px}.occupancy-pagination nav{justify-content:space-between;width:100%;gap:8px}}
</style>
