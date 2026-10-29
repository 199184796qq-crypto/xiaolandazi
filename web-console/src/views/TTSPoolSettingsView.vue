<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { request } from '../api'

interface Account { id: string; name: string; account_uid: string; credential_source: string; configured: boolean; enabled: boolean; rpm: number; rps: number; revision: number; used_minute: number; used_second: number; in_flight: number; voices: Record<string, string> }
interface Queued { id: string; model: string; room_id: number; session_id: number; queued_at: string; expires_at: string }
interface Pool { accounts: Account[]; queued: number; requests: Queued[]; observed_at: string }
interface Mapping { model: string; source: string; target: string }
interface Draft { id: string; name: string; account_uid: string; enabled: boolean; rpm: number; rps: number; revision: number; new_api_key: string; mappings: Mapping[] }
const pool = ref<Pool>({ accounts: [], queued: 0, requests: [], observed_at: '' })
const loading = ref(false), saving = ref(false), editor = ref(false), error = ref(''), notice = ref('')
const empty = (): Draft => ({ id: '', name: '', account_uid: '', enabled: true, rpm: 180, rps: 3, revision: 0, new_api_key: '', mappings: [] })
const draft = ref<Draft>(empty())
const isCurrent = computed(() => draft.value.id === 'current-aliyun-tts')
const canSave = computed(() => draft.value.name.trim() && draft.value.account_uid.trim() && Number.isInteger(draft.value.rpm) && draft.value.rpm >= 1 && draft.value.rpm <= 180 && Number.isInteger(draft.value.rps) && draft.value.rps >= 1 && draft.value.rps <= 3 && (draft.value.id || draft.value.new_api_key.trim()) && (isCurrent.value || !draft.value.enabled || draft.value.mappings.length > 0) && draft.value.mappings.every(m => m.model.trim() && m.source.trim() && m.target.trim()))
let timer: ReturnType<typeof setInterval> | undefined, disposed = false
async function load(manual = false) {
  if (loading.value || saving.value) return
  loading.value = true
  if (manual) error.value = ''
  try {
    const data = await request<Pool>('/api/v1/system/tts-pool', { suppressErrorDialog: true })
    if (!disposed) { pool.value = data; if (!editor.value) error.value = '' }
  } catch (e) { if (!disposed && (manual || !editor.value)) error.value = e instanceof Error ? e.message : '读取TTS账户池失败' }
  finally { loading.value = false }
}
function edit(a?: Account) {
  if (saving.value) return
  draft.value = a ? { id: a.id, name: a.name, account_uid: a.account_uid, enabled: a.enabled, rpm: a.rpm, rps: a.rps, revision: a.revision, new_api_key: '', mappings: Object.entries(a.voices || {}).map(([k, target]) => { const at = k.indexOf('/'); return { model: k.slice(0, at), source: k.slice(at + 1), target } }) } : empty()
  editor.value = true; error.value = ''; notice.value = ''
}
function close() { if (!saving.value) { editor.value = false; draft.value = empty() } }
async function save() {
  if (!canSave.value || saving.value) return
  const voices: Record<string, string> = {}
  for (const row of draft.value.mappings) {
    const key = `${row.model.trim()}/${row.source.trim()}`
    if (key in voices) { error.value = '同一模型和原音色不能重复配置'; return }
    voices[key] = row.target.trim()
  }
  saving.value = true; error.value = ''; notice.value = ''
  const { mappings: _, ...fields } = draft.value
  try {
    await request('/api/v1/system/tts-pool', { method: 'PUT', body: JSON.stringify({ ...fields, voices }), suppressErrorDialog: true })
    if (!disposed) { editor.value = false; draft.value = empty(); notice.value = '已保存，后续合成按新配置分配。排队期间主线使用可用缓存，互动暂缓。' }
  } catch (e) { if (!disposed) { draft.value.new_api_key = ''; error.value = e instanceof Error ? e.message : '保存失败，请重试' } }
  finally { saving.value = false }
  if (!editor.value && !disposed) await load()
}
onMounted(() => { void load(true); timer = setInterval(() => { void load() }, 5000) })
onUnmounted(() => { disposed = true; if (timer) clearInterval(timer); draft.value.new_api_key = '' })
</script>

<template>
  <main class="tts-pool">
    <header><div><p class="kicker">TTS ACCOUNT POOL</p><h1>TTS账户池与调度</h1><p>每个账号分别限制每分钟180次、每秒3次。当前账号满额就切换下一个可用账号，全部满额才排队。</p></div><div class="actions"><button :disabled="loading || saving" @click="load(true)">刷新用量</button><button class="primary" :disabled="saving" @click="edit()">添加账号</button></div></header>
    <p class="policy">排队不停止直播：主线使用当前方案的可用缓存；互动声音未就绪时暂缓互动。无有效缓存时等待补料，不播放其他方案的声音。</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <section class="panel"><h2>账号与用量</h2><p>同一阿里云主账号下的多个Key共用额度，只能登记一个账号。备用账号需先配置可用的音色对应关系。</p>
      <div class="table-wrap"><table><thead><tr><th>账号</th><th>状态</th><th>近60秒</th><th>近1秒</th><th>正在合成</th><th>操作</th></tr></thead><tbody><tr v-for="a in pool.accounts" :key="a.id"><td><strong>{{ a.name }}</strong><small>主账号UID：{{ a.account_uid === 'current' ? '待补充' : a.account_uid }}</small></td><td>{{ !a.configured ? '未配置凭据' : a.enabled ? '已启用' : '已停用' }}<small>{{ a.credential_source === 'current' ? '使用当前服务器凭据' : '凭据已加密保存' }}</small></td><td>{{ a.used_minute }} / {{ a.rpm }}</td><td>{{ a.used_second }} / {{ a.rps }}</td><td>{{ a.in_flight }}</td><td><button :disabled="saving" @click="edit(a)">编辑</button></td></tr><tr v-if="!pool.accounts.length"><td colspan="6">{{ loading ? '正在读取…' : '暂无账号' }}</td></tr></tbody></table></div>
    </section>
    <section v-if="editor" class="panel editor" aria-label="编辑TTS账号"><h2>{{ draft.id ? '编辑账号' : '添加阿里TTS账号' }}</h2>
      <form @submit.prevent="save"><fieldset :disabled="saving"><div class="fields"><label>账号名称<input v-model="draft.name" required maxlength="80" /></label><label>阿里云主账号UID<input v-model="draft.account_uid" aria-label="阿里云主账号UID" required maxlength="80" /><small>填写主账号UID，不能填写RAM用户ID或Key名称。</small></label><label>每分钟请求上限<input v-model.number="draft.rpm" type="number" min="1" max="180" step="1" required /></label><label>每秒请求上限<input v-model.number="draft.rps" type="number" min="1" max="3" step="1" required /></label><label v-if="!isCurrent">API Key<input v-model="draft.new_api_key" aria-label="API Key" type="password" autocomplete="new-password" :required="!draft.id" placeholder="已有凭据留空保留" /><small>仅提交新凭据，保存后不会回显。</small></label><label class="check"><input v-model="draft.enabled" type="checkbox" />启用账号</label></div>
        <div v-if="!isCurrent" class="mappings"><h3>同一声音在该账号下的音色</h3><p>使用该账号已创建并确认一致的音色。不会自动克隆或调用付费测试。</p><div v-for="(m, i) in draft.mappings" :key="i" class="mapping"><label>模型<input v-model="m.model" required placeholder="qwen-audio-3.1-tts-flash" /></label><label>方案原音色ID<input v-model="m.source" required /></label><label>本账号对应音色ID<input v-model="m.target" required /></label><button type="button" @click="draft.mappings.splice(i, 1)">移除</button></div><button type="button" @click="draft.mappings.push({ model: 'qwen-audio-3.1-tts-flash', source: '', target: '' })">添加音色对应</button></div>
        <div class="actions"><button type="button" @click="close">取消</button><button class="primary" :disabled="!canSave || saving">{{ saving ? '保存中…' : '保存账号' }}</button></div>
      </fieldset></form>
    </section>
    <section class="panel"><h2>等待合成 <span class="badge">{{ pool.queued }}</span></h2><p>显示最早的50条等待请求。账号有额度后继续分配；过期或取消的请求不再发送。</p><div class="table-wrap"><table><thead><tr><th>直播间</th><th>场次</th><th>模型</th><th>进入队列</th></tr></thead><tbody><tr v-for="q in pool.requests" :key="q.id"><td>{{ q.room_id || '后台任务' }}</td><td>{{ q.session_id || '—' }}</td><td>{{ q.model }}</td><td>{{ new Date(q.queued_at).toLocaleTimeString() }}</td></tr><tr v-if="!pool.requests.length"><td colspan="4">暂无等待请求</td></tr></tbody></table></div></section>
  </main>
</template>

<style scoped>
.tts-pool{max-width:1400px;margin:0 auto;padding:24px;color:#273750;font-size:16px}.tts-pool header,.actions{display:flex;align-items:center;justify-content:space-between;gap:12px}.tts-pool header{align-items:flex-start;gap:24px}.kicker{font-size:12px;letter-spacing:.16em;color:#7974db}h1{font-size:28px;margin:6px 0 12px}h2{font-size:21px;margin:0 0 12px}h3{font-size:18px}p{line-height:1.7;color:#6c7890}.panel{margin:20px 0;padding:24px;background:white;border:1px solid #e2e6f3;border-radius:18px}.policy{padding:16px 20px;background:#eff2ff;border-radius:12px;color:#4d5e95}.error{color:#ba2741}.notice{color:#16805c}.table-wrap{overflow:auto}table{border-collapse:collapse;width:100%;text-align:left;white-space:nowrap}th,td{padding:14px;border-bottom:1px solid #ecedf5}small{display:block;color:#77849b;font-size:13px;margin-top:6px;line-height:1.5}button,input{font:inherit}button{padding:10px 16px;border:1px solid #dce1f3;border-radius:10px;background:white;color:#596a91;cursor:pointer;white-space:nowrap}button.primary{background:#6657eb;color:white;border-color:#6657eb}button:disabled{opacity:.5;cursor:default}.badge{display:inline-block;padding:2px 10px;background:#eeecff;color:#6756dc;border-radius:12px;font-size:16px}.fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px}label{display:flex;flex-direction:column;gap:8px}input{width:100%;box-sizing:border-box;padding:12px;border:1px solid #d9dff0;border-radius:10px;min-width:0}.check{flex-direction:row;align-items:center}.check input{width:auto}fieldset{border:0;padding:0;margin:0;min-width:0}.mapping{display:grid;grid-template-columns:1.3fr 1fr 1fr auto;align-items:end;gap:10px;margin:14px 0}.editor .actions{justify-content:flex-end;margin-top:24px}.mappings{margin-top:24px}.actions{flex-wrap:wrap}@media(max-width:700px){.tts-pool{padding:12px}.tts-pool header{display:block}.fields,.mapping{grid-template-columns:1fr}.panel{padding:16px}h1{font-size:24px}}
</style>
