<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { request } from '../api'

interface Display { voice_id: string; name: string; gender: string; revision: number }
const items = ref<Display[]>([])
const loading = ref(false), saving = ref(false)
const error = ref(''), notice = ref('')
let epoch = 0, disposed = false
async function load() {
  if (saving.value) return
  const token = ++epoch
  loading.value = true; error.value = ''; notice.value = ''
  try {
    const response = await request<{ items: Display[] }>('/api/v1/system/tts-voice-display')
    if (!disposed && token === epoch) items.value = response.items.map(item => ({ ...item }))
  } catch (e) {
    if (!disposed && token === epoch) error.value = e instanceof Error ? e.message : '读取失败'
  } finally { if (!disposed && token === epoch) loading.value = false }
}
async function save(item: Display) {
  if (loading.value || saving.value) return
  const token = ++epoch
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const saved = await request<Display>('/api/v1/system/tts-voice-display', { method: 'PUT', body: JSON.stringify({ ...item }) })
    if (!disposed && token === epoch) {
      Object.assign(item, saved)
      notice.value = `“${saved.name}”展示资料已保存，用户重新进入声音页面后可见。`
    }
  } catch (e) {
    if (!disposed && token === epoch) error.value = e instanceof Error ? e.message : '保存失败'
  } finally { if (!disposed && token === epoch) saving.value = false }
}
onMounted(load)
onBeforeUnmount(() => { disposed = true; ++epoch })
</script>
<template>
  <section class="voice-display-panel" aria-label="高仿真声音展示资料">
    <div class="heading"><h2>高仿真声音展示资料</h2><button type="button" :disabled="loading || saving" @click="load">重新读取音色</button></div>
    <p>为账号池内的复刻音色设置用户看到的名称及头像。请按控制台名称填写；女声、男声使用对应头像，未知使用麦克风图标。</p>
    <p v-if="loading" role="status">正在读取…</p>
    <p v-if="error" class="error" role="alert">{{ error }}</p><p v-if="notice" class="notice" role="status">{{ notice }}</p>
    <p v-if="!loading && !error && !items.length">暂无可用音色，请先保存账号及音色映射，再重新读取。</p>
    <form v-for="item in items" :key="item.voice_id" @submit.prevent="save(item)">
      <fieldset :disabled="loading || saving">
        <legend>{{ item.voice_id }}</legend>
        <label>声音名称<input v-model="item.name" required maxlength="80" placeholder="填写控制台音色名称" /></label>
        <label>声音头像<select v-model="item.gender"><option value="">🎙️ 未知</option><option value="女声">👩 女声</option><option value="男声">👨 男声</option></select></label>
        <button class="primary" :disabled="!item.name.trim()">{{ saving ? '保存中…' : '保存展示资料' }}</button>
      </fieldset>
    </form>
  </section>
</template>
<style scoped>
.voice-display-panel{margin:20px 0;padding:24px;background:white;border:1px solid #e2e6f3;border-radius:18px;color:#273750;font-size:16px}.heading{display:flex;justify-content:space-between;gap:16px;align-items:center;flex-wrap:wrap}h2{font-size:21px;margin:0}p{color:#6c7890;line-height:1.7}fieldset{display:grid;grid-template-columns:minmax(0,1fr) minmax(160px,.4fr) auto;gap:16px;align-items:end;border:1px solid #e2e6f3;border-radius:12px;margin:20px 0 0;padding:16px;min-width:0}legend{overflow-wrap:anywhere;max-width:100%}label{display:grid;gap:8px}input,select,button{font:inherit;min-width:0;padding:12px;border:1px solid #d9dff0;border-radius:10px;box-sizing:border-box;background:white}input,select{width:100%}button{cursor:pointer;color:#596a91}button.primary{background:#6657eb;color:white}button:disabled{opacity:.5;cursor:default}.error{color:#ba2741}.notice{color:#16805c}@media(max-width:700px){.voice-display-panel{padding:16px}fieldset{grid-template-columns:minmax(0,1fr)}}
</style>
