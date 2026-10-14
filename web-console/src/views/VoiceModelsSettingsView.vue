<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { request } from '../api'

interface VoiceModelConfig { custom_model: string; official_library: string; revision: number }
interface VoiceSettingsResponse { config: VoiceModelConfig; credentials: { tencent: boolean; aliyun: boolean } }
const config = ref<VoiceModelConfig>({ custom_model: 'cosyvoice-v3.5-flash', official_library: 'tencent-premium', revision: 0 })
const credentials = ref({ tencent: false, aliyun: false })
const loaded = ref(false), loading = ref(false), saving = ref(false)
const error = ref(''), notice = ref('')
const customModels = [
  { id: 'cosyvoice-v3.5-flash', name: 'CosyVoice 3.5 Flash' },
  { id: 'cosyvoice-v3.5-plus', name: 'CosyVoice 3.5 Plus' },
  { id: 'qwen-audio-3.0-tts-plus', name: 'Qwen Audio 3.0 Plus' },
  { id: 'qwen-audio-3.0-tts-flash', name: 'Qwen Audio 3.0 Flash' },
  { id: 'qwen-audio-3.1-tts-flash', name: 'Qwen Audio 3.1 Flash' },
]
async function load() {
  if (saving.value) return
  loading.value = true; error.value = ''; notice.value = ''
  try {
    const result = await request<VoiceSettingsResponse>('/api/v1/system/voice-models', { suppressErrorDialog: true })
    config.value = result.config; credentials.value = result.credentials; loaded.value = true
  } catch (e) { error.value = e instanceof Error ? e.message : '读取声音配置失败' }
  finally { loading.value = false }
}
async function save() {
  if (!loaded.value || loading.value || saving.value) return
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const result = await request<VoiceSettingsResponse>('/api/v1/system/voice-models', { method: 'PUT', body: JSON.stringify(config.value), suppressErrorDialog: true })
    config.value = result.config; credentials.value = result.credentials
    notice.value = '声音配置已保存。用户刷新声音页后采用新配置；已有声音和直播间当前选择保留。'
  } catch (e) { error.value = e instanceof Error ? e.message : '保存声音配置失败' }
  finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <section class="voice-settings">
    <header><div><RouterLink to="/system/settings/intelligence">← 返回 AI 与智能体</RouterLink><h1>声音模型配置</h1><p>统一设置官方声音库和自定义声音模型，用户端只展示声音。</p></div><button :disabled="loading || saving" @click="load">{{ loading ? '读取中…' : '重新读取' }}</button></header>
    <p v-if="error" class="settings-error" role="alert">{{ error }}</p>
    <p v-if="notice" class="settings-notice" role="status">{{ notice }}</p>
    <fieldset :disabled="!loaded || loading || saving">
      <article><span class="card-icon">♩</span><h2>自定义声音</h2><p>用户上传录音或描述声音时，统一使用这里指定的模型。</p><label>声音模型<select v-model="config.custom_model" aria-label="自定义声音模型"><option v-for="item in customModels" :key="item.id" :value="item.id">{{ item.name }}</option></select></label><small>{{ config.custom_model.startsWith('cosyvoice-') ? '支持录音复刻和描述生成声音' : '支持录音复刻；描述生成入口自动隐藏' }}</small></article>
      <article><span class="card-icon">★</span><h2>官方声音</h2><p>仅提供成年中文男声和女声，不提供外语或童声音色。</p><label>官方声音库<select v-model="config.official_library" aria-label="官方声音库"><option value="tencent-premium">腾讯实时精品音色</option><option value="qwen_audio">阿里云官方音色</option></select></label><small>声音的供应商、模型与音色编号由服务端统一绑定。</small></article>
    </fieldset>
    <aside><h2>服务连接</h2><div><span>阿里云声音服务</span><b :class="{ ready: credentials.aliyun }">{{ credentials.aliyun ? '凭据已配置' : '尚未配置' }}</b></div><div><span>腾讯声音服务</span><b :class="{ ready: credentials.tencent }">{{ credentials.tencent ? '凭据已配置' : '尚未配置' }}</b></div><p>密钥由服务器安全配置管理。此处显示配置状态，实际可用性以试听结果为准。</p></aside>
    <footer><p>已有录音、音色和当前房间选择继续保留。配置变更后，需要转换的旧声音会从原样本生成新音色，不混用不同模型的音色编号。</p><button class="save" :disabled="!loaded || loading || saving" @click="save">{{ saving ? '保存中…' : '保存声音配置' }}</button></footer>
  </section>
</template>

<style scoped>
.voice-settings{width:100%;max-width:1180px;margin:auto;padding:24px;box-sizing:border-box;color:#243454}.voice-settings header{display:flex;justify-content:space-between;align-items:center;gap:16px}.voice-settings h1{font-size:28px;margin:18px 0 8px}.voice-settings h2{font-size:20px;margin:12px 0}.voice-settings p{color:#7c8aa3;line-height:1.7}.voice-settings a{color:#6457eb;text-decoration:none}.voice-settings button{padding:12px 20px;border:1px solid #dce2f1;border-radius:12px;background:#fff;color:#4c5d80;font:inherit;font-weight:700;cursor:pointer}.voice-settings button:disabled{opacity:.5;cursor:default}.voice-settings fieldset{display:grid;grid-template-columns:1fr 1fr;gap:20px;margin:26px 0;padding:0;border:0;min-width:0}.voice-settings article,.voice-settings aside{padding:24px;border:1px solid #e0e6f6;border-radius:20px;background:#fff;min-width:0}.card-icon{display:grid;place-items:center;width:48px;height:48px;background:#eeecff;color:#6857ee;border-radius:14px;font-size:24px}.voice-settings label{display:grid;gap:10px;font-weight:700;margin:22px 0 12px}.voice-settings select{width:100%;max-width:100%;padding:13px;border:1px solid #d8e0f2;border-radius:12px;background:#f9faff;color:#283c60;font:inherit}.voice-settings small{color:#7d8da9;line-height:1.6}.voice-settings aside>div{display:flex;justify-content:space-between;gap:20px;max-width:420px;padding:10px 0}.voice-settings aside b{color:#b68331}.voice-settings aside b.ready{color:#21a37a}.voice-settings footer{display:flex;align-items:center;justify-content:space-between;gap:24px;margin-top:20px}.voice-settings footer p{max-width:780px}.voice-settings .save{flex:none;background:linear-gradient(120deg,#8a75ff,#5144ff);border-color:transparent;color:#fff}.settings-error,.settings-notice{padding:14px;border-radius:12px}.voice-settings .settings-error{background:#fff0f2;color:#bc4055}.voice-settings .settings-notice{background:#ebfff6;color:#1a8861}@media(max-width:700px){.voice-settings{padding:16px}.voice-settings header{align-items:flex-start}.voice-settings fieldset{grid-template-columns:1fr}.voice-settings footer{flex-direction:column;align-items:stretch}.voice-settings h1{font-size:24px}}
</style>
