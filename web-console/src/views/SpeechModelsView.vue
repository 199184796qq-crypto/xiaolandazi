<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { request } from '../api'
import ModelPickerDialog from '../components/ModelPickerDialog.vue'

interface Profile {
  id: string; name: string; protocol: string; base_url: string; model: string
  enabled: boolean; timeout_ms: number; max_tokens: number; system_prompt: string
  has_api_key: boolean; api_key?: string
  chat_token_field?: string
}
interface Config { profiles: Profile[]; default_id: string; revision: number }
interface TestResult { text: string; model: string; protocol: string; latency_ms: number; input_tokens: number; output_tokens: number; total_tokens: number }
interface AvailableModel { id: string; name: string }
interface ModelList { models: AvailableModel[]; truncated: boolean }
const config = ref<Config>({ profiles: [], default_id: '', revision: 0 })
const selectedId = ref('')
const selected = computed(() => config.value.profiles.find(p => p.id === selectedId.value))
const loading = ref(false), saving = ref(false), testing = ref(false)
const listing = ref(false), pickerOpen = ref(false), listError = ref('')
const modelList = ref<ModelList | null>(null), listedContext = ref('')
const connectionContext = computed(() => JSON.stringify([selected.value?.id, selected.value?.protocol, selected.value?.base_url, selected.value?.api_key || '']))
const currentModelList = computed(() => listedContext.value === connectionContext.value ? modelList.value : null)
const currentListError = computed(() => listedContext.value === connectionContext.value ? listError.value : '')
const keyStorageReady = ref(false)
const message = ref(''), error = ref(''), result = ref<TestResult | null>(null)
const mode = ref('prompt')
const question = ref('请根据上面的基础提示词，回答：这个产品买回家以后应该怎么保存？')
const protocols = [
  { id: 'openai_chat', name: 'OpenAI 兼容 · Chat Completions', hint: '填写 API Base URL（例如以 /v1 结尾），也支持完整 /chat/completions 地址。适用于采用该协议的模型服务。' },
  { id: 'openai_responses', name: 'OpenAI · Responses', hint: '填写 API Base URL，也支持完整 /responses 地址。' },
  { id: 'gemini', name: 'Gemini 原生 · generateContent', hint: '填写版本根地址，例如 https://generativelanguage.googleapis.com/v1beta；模型 ID 单独填写。' },
  { id: 'anthropic', name: 'Anthropic 原生 · Messages', hint: '填写 https://api.anthropic.com/v1 或服务商提供的同协议地址。' },
]
const hint = computed(() => protocols.find(p => p.id === selected.value?.protocol)?.hint)

async function load() {
  modelList.value = null; listError.value = ''; listedContext.value = ''; pickerOpen.value = false
  loading.value = true; error.value = ''; message.value = ''
  try {
    const loaded = await request<{ config: Config; key_storage_ready: boolean }>('/api/v1/system/speech-models')
    config.value = loaded.config; keyStorageReady.value = loaded.key_storage_ready
    for (const p of config.value.profiles) p.chat_token_field ||= 'max_tokens'
    if (!config.value.profiles.some(p => p.id === selectedId.value)) selectedId.value = config.value.profiles[0]?.id || ''
    result.value = null
  } catch (e) { error.value = e instanceof Error ? e.message : '读取失败' }
  finally { loading.value = false }
}
function add() {
  const p: Profile = { id: crypto.randomUUID(), name: '新模型', protocol: 'openai_chat', base_url: '', model: '', enabled: true, timeout_ms: 120000, max_tokens: 16000, chat_token_field: 'max_tokens', system_prompt: '', has_api_key: false, api_key: '' }
  config.value.profiles.push(p); selectedId.value = p.id; result.value = null
}
function remove() {
  const p = selected.value; if (!p) return
  if (!window.confirm(`删除“${p.name}”配置？保存后生效，已有稿件和音频不会删除。`)) return
  if (config.value.default_id === p.id) config.value.default_id = ''
  config.value.profiles = config.value.profiles.filter(item => item.id !== p.id)
  selectedId.value = config.value.profiles[0]?.id || ''; result.value = null
}
async function save() {
  saving.value = true; error.value = ''; message.value = ''
  try {
    const saved = await request<{ config: Config }>('/api/v1/system/speech-models', { method: 'PUT', body: JSON.stringify(config.value) })
    config.value = saved.config
    message.value = '已保存。后续话术与实时回答使用所选模型；不会替换已生成音频，也不会修改智能体模型。'
    result.value = null
  } catch (e) { error.value = e instanceof Error ? e.message : '保存失败' }
  finally { saving.value = false }
}
async function test(connectivity = false) {
  if (!selected.value) return
  testing.value = true; error.value = ''; message.value = ''; result.value = null
  try {
    result.value = await request<TestResult>('/api/v1/system/speech-models/test', { method: 'POST', body: JSON.stringify({ profile: selected.value, mode: connectivity ? 'connectivity' : mode.value, question: question.value }) })
    message.value = connectivity ? '模型已成功返回文字。测试没有改变默认模型。' : '测试完成，没有发布或改变直播内容。'
  } catch (e) { error.value = e instanceof Error ? e.message : '测试失败' }
  finally { testing.value = false }
}
async function fetchModels() {
  if (!selected.value || listing.value) return
  const context = connectionContext.value
  listing.value = true; listError.value = ''; modelList.value = null; listedContext.value = context
  try {
    const list = await request<ModelList>('/api/v1/system/speech-models/models', { method: 'POST', body: JSON.stringify({ profile: selected.value }) })
    if (context === connectionContext.value) modelList.value = list
  } catch (e) {
    if (context === connectionContext.value) listError.value = e instanceof Error ? e.message : '获取失败，仍可手动填写模型 ID'
  } finally { listing.value = false }
}
function openModelPicker() {
  pickerOpen.value = true
  void fetchModels()
}
function selectModel(id: string) {
  if (listing.value || !selected.value || !currentModelList.value?.models.some(m => m.id === id)) return
  selected.value.model = id
  result.value = null
  pickerOpen.value = false
}
onMounted(load)
</script>

<template>
  <div class="speech-model-page">
    <header class="model-hero">
      <div><small>ANCHOR MODEL CONNECTIONS</small><h2>主播话术与实时互动模型</h2><p>同一个默认模型用于主线话术、风格测试、动态更新和实时回答。智能体理解、策略决策与风格分析模型保持原配置。</p></div>
      <div class="actions"><button class="ghost-button" :disabled="loading || saving || testing || listing" @click="load">刷新</button><button class="primary-button" :disabled="loading || saving || testing || listing || (!keyStorageReady && config.profiles.length > 0)" @click="save">{{ saving ? '保存中…' : '保存全部配置' }}</button></div>
    </header>
    <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="message" class="notice" role="status">{{ message }}</p>
    <p v-if="!loading && !keyStorageReady" class="notice">服务器尚未配置模型密钥加密主密钥，暂不能保存带密钥的配置。请由服务器管理员设置 MODEL_CONFIG_ENCRYPTION_KEY（至少 32 字符）；仍可填写密钥进行临时测试。</p>
    <section class="default-model">
      <label><strong>话术与实时互动的默认模型</strong><select v-model="config.default_id" :disabled="loading || saving || testing || listing"><option value="">保留原有服务器配置</option><option v-for="p in config.profiles" :key="p.id" :value="p.id" :disabled="!p.enabled">{{ p.name }} · {{ p.model || '未填写模型 ID' }}{{ p.enabled ? '' : '（停用）' }}</option></select></label>
      <p>保存后对后续请求生效。正在处理的请求和已有成品音频不变；模型失败不会自动切换到其它服务商。</p>
    </section>
    <div class="model-layout">
      <aside class="model-list"><div class="list-head"><h3>模型配置</h3><button class="ghost-button" :disabled="loading || saving || testing || listing || config.profiles.length >= 30" @click="add">＋ 添加</button></div>
        <button v-for="p in config.profiles" :key="p.id" class="model-item" :class="{ active: p.id === selectedId }" :disabled="loading || saving || testing || listing" @click="selectedId = p.id; result = null"><strong>{{ p.name }}</strong><span>{{ p.model || '未设置模型' }}</span><small>{{ config.default_id === p.id ? '默认模型 · ' : '' }}{{ p.enabled ? '启用' : '停用' }} · {{ p.has_api_key ? '密钥已保存' : '待配置密钥' }}</small></button>
        <p v-if="!config.profiles.length">还没有自定义模型。添加配置不会自动切换现有模型。</p>
      </aside>
      <fieldset v-if="selected" class="model-editor" :disabled="saving || testing || loading">
        <div class="editor-head"><h3>{{ selected.name }}</h3><button class="ghost-button danger" @click="remove">删除配置</button></div>
        <div class="fields">
          <label><span>配置名称</span><input v-model.trim="selected.name" maxlength="80" placeholder="例如 Gemini 主播模型" /></label>
          <label><span>API 协议</span><select v-model="selected.protocol"><option v-for="p in protocols" :key="p.id" :value="p.id">{{ p.name }}</option></select></label>
          <label class="wide"><span>API 地址</span><input v-model.trim="selected.base_url" type="url" placeholder="https://服务商地址/v1" /><small>{{ hint }}仅允许公网 HTTPS。</small></label>
          <div class="model-discovery">
            <div class="model-id-heading"><label for="speech-model-id">模型 ID</label><button class="ghost-button" type="button" :disabled="listing || !selected.base_url.trim() || (!selected.api_key?.trim() && !selected.has_api_key)" @click="openModelPicker">{{ listing ? '正在获取…' : '刷新模型列表' }}</button></div>
            <input id="speech-model-id" v-model.trim="selected.model" placeholder="选择模型，或手动填写模型 ID" maxlength="160" />
            <small>填写 API 地址和密钥后，可打开列表搜索并选择模型。</small>
          </div>
          <label><span>API 密钥</span><input v-model.trim="selected.api_key" type="password" autocomplete="new-password" :placeholder="selected.has_api_key ? '已保存，留空保留原密钥' : '请输入 API Key'" /><small>密钥加密保存，不回显。更换地址或协议须重新输入。</small></label>
          <label><span>请求超时（毫秒）</span><input v-model.number="selected.timeout_ms" type="number" min="1000" max="180000" step="1000" /><small>实际超时还受业务请求上限约束。</small></label>
          <label><span>最大输出 Token</span><input v-model.number="selected.max_tokens" type="number" min="64" max="32000" /><small>这是上限；实时回答使用更小的业务额度。</small></label>
          <label v-if="selected.protocol === 'openai_chat'"><span>输出额度参数</span><select v-model="selected.chat_token_field"><option value="max_tokens">max_tokens（兼容服务常用）</option><option value="max_completion_tokens">max_completion_tokens</option></select><small>按服务商文档选择；不支持旧参数的模型使用后一项。</small></label>
          <label class="enabled"><input v-model="selected.enabled" type="checkbox" /><span>启用此配置</span></label>
          <label class="wide"><span>基础／元提示词（可选）</span><textarea v-model="selected.system_prompt" rows="8" maxlength="20000" placeholder="填写通用生成约束。每位主播的风格规范和正式事实仍由业务请求携带，不要把某位主播或某个商品固定在这里。" /><small>作为额外系统提示词，不替代原有业务指令、主播规范或事实依据。</small></label>
        </div>
      </fieldset>
      <div v-else class="empty">选择或添加一组模型配置。</div>
    </div>
    <section v-if="selected" class="model-test">
      <div class="editor-head"><div><h3>连接与文字测试</h3><p>使用当前编辑配置，可在保存前测试。测试会调用服务商 API，可能产生费用；不会生成声音、发布内容或切换默认模型。</p></div><button class="ghost-button" :disabled="testing || saving || listing || loading" @click="test(true)">{{ testing ? '测试中…' : '测试连通性' }}</button></div>
      <label><span>测试方式</span><select v-model="mode" :disabled="testing"><option value="direct">直接提问（不携带基础提示词）</option><option value="prompt">基础提示词＋测试问题／完整测试提示词</option></select></label>
      <textarea v-model="question" :disabled="testing" rows="7" maxlength="30000" placeholder="粘贴测试问题、主播规则和测试设定。这里只发送你填写的内容，不自动读取客户直播间数据。" />
      <button class="primary-button" :disabled="testing || saving || listing || loading || !question.trim()" @click="test(false)">{{ testing ? '等待模型返回…' : '生成测试文字' }}</button>
      <div v-if="result" class="test-result"><div class="metrics"><span>{{ result.model }}</span><span>{{ result.latency_ms }} ms</span><span>输入 {{ result.input_tokens }} Token</span><span>输出 {{ result.output_tokens }} Token</span></div><pre>{{ result.text }}</pre></div>
    </section>
    <ModelPickerDialog v-if="pickerOpen && selected" :models="currentModelList?.models || []" :loading="listing" :error="currentListError" :truncated="currentModelList?.truncated || false" :initial-id="selected.model" @close="pickerOpen = false" @refresh="fetchModels" @select="selectModel" />
  </div>
</template>

<style scoped>
.model-discovery{display:grid;gap:8px;align-content:start}.model-id-heading{display:flex;align-items:center;gap:14px;min-height:26px}.model-id-heading label{font-weight:700;white-space:nowrap}.model-id-heading button{padding:3px 10px;border-radius:8px;font-size:13px;line-height:18px}.model-discovery small{color:#7a8499;line-height:1.6}.fields>label{align-content:start}
.speech-model-page{display:grid;gap:18px;color:#28334d}.model-hero,.default-model,.model-list,.model-editor,.model-test,.empty{padding:22px;border:1px solid #dfe5f2;border-radius:20px;background:#fff;box-shadow:0 10px 28px #4254800a}.model-hero{display:flex;justify-content:space-between;align-items:center;gap:20px;background:linear-gradient(125deg,#fff,#eff2ff)}.model-hero h2{margin:6px 0;font-size:28px}.model-hero small{color:#6978cc;letter-spacing:.1em}.model-hero p,.default-model p,.model-test p,.model-list p{color:#748097;line-height:1.7;margin:8px 0 0}.actions,.editor-head,.list-head{display:flex;justify-content:space-between;align-items:center;gap:12px}.actions{flex-shrink:0}.default-model label,.fields label,.model-test>label{display:grid;gap:8px}.model-layout{display:grid;grid-template-columns:260px minmax(0,1fr);gap:18px}.model-editor{margin:0;min-width:0}.fields{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:18px;margin-top:15px}.wide{grid-column:1/-1}.fields span,.model-test>label span{font-weight:700}.fields small{color:#7a8499;line-height:1.6}.fields .enabled{display:flex;align-items:center}.enabled input{width:auto}.model-item{display:grid;gap:6px;text-align:left;width:100%;border:1px solid #e1e6f1;border-radius:14px;margin-top:12px;padding:15px;background:#f8f9fd;color:inherit;cursor:pointer}.model-item.active{border-color:#9199ec;background:#eef0ff;box-shadow:0 5px 15px #636dcc1a}.model-item span,.model-item small{color:#7b849b;overflow-wrap:anywhere}.speech-model-page input,.speech-model-page select,.speech-model-page textarea{box-sizing:border-box;width:100%;padding:11px 13px;border:1px solid #d8dfed;border-radius:11px;background:white;color:inherit;font:inherit}.speech-model-page input[type=checkbox]{width:auto}.speech-model-page textarea{resize:vertical;line-height:1.7}.model-test{display:grid;gap:16px}.model-test>label{max-width:540px}.model-test>.primary-button{justify-self:start}.danger{color:#c0526a}.error,.notice{padding:14px;border-radius:12px;margin:0}.error{background:#fff0f2;color:#b34359}.notice{background:#eff9f3;color:#2c7a58}.metrics{display:flex;gap:15px;flex-wrap:wrap;color:#6978a5}.test-result pre{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;line-height:1.9;padding:18px;border-radius:12px;background:#f5f7fc}.empty{color:#8791a6}@media(max-width:900px){.model-layout{grid-template-columns:1fr}.model-hero{align-items:flex-start;flex-direction:column}.fields{grid-template-columns:1fr}.editor-head{flex-wrap:wrap}}
</style>
