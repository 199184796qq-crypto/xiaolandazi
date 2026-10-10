<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { request } from '../api'
interface Profile { id: string; name: string; model: string; enabled: boolean; has_api_key: boolean }
interface Settings {
	mainline_buffer_seconds: number
  interaction_profile_id?: string; style_profile_id: string; scorecard_profile_id: string; selling_point_profile_id: string; safety_profile_id?: string
  cycle: { text_profile_id?: string; workspace_id: string; region: string; agent_id: string; environment_id: string; credential_profile_id: string; feedback_profile_id: string; threshold: number; max_retries: number; max_chars: number; point_gap: number; combination_gap: number; body_min: number; body_max: number; rolling: boolean; repair_punctuation: boolean; max_cycles: number }
}
const config = ref<Settings | null>(null), profiles = ref<Profile[]>([]), busy = ref(false), error = ref(''), notice = ref('')
const modelNames = ref({ system: '', speech: '', review: '' })
async function load() {
  busy.value = true; error.value = ''
  try {
    const [settings, models] = await Promise.all([request<Settings>('/api/v1/system/anchor-generation-settings'), request<{ config: { profiles: Profile[]; default_id: string; system_default_id?: string; review_default_id?: string } }>('/api/v1/system/speech-models')])
    config.value = settings; profiles.value = models.config.profiles.filter(p => p.enabled && p.has_api_key)
    const label = (id?: string) => { const p = models.config.profiles.find(p => p.id === id); return p ? `${p.name} · ${p.model}` : '请在模型连接设置中选择' }
    modelNames.value = { system: label(models.config.system_default_id), speech: label(models.config.default_id), review: label(models.config.review_default_id || models.config.default_id) }
  } catch (e) { error.value = e instanceof Error ? e.message : '读取生成配置失败' }
  finally { busy.value = false }
}
async function save() {
  busy.value = true; error.value = ''; notice.value = ''
  if (config.value?.cycle.text_profile_id) config.value.cycle.feedback_profile_id = config.value.cycle.text_profile_id
  try { config.value = await request<Settings>('/api/v1/system/anchor-generation-settings', { method: 'PUT', body: JSON.stringify(config.value) }); notice.value = '已保存，新调教/初始化/新场采用此配置；正在运行的场次保持原配置快照。' }
  catch (e) { error.value = e instanceof Error ? e.message : '保存失败' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section id="anchor-generation" class="generation-settings">
    <h2>文字生成调度</h2>
    <p>系统智能体：{{ modelNames.system }}。口播生成：{{ modelNames.speech }}。审核：{{ modelNames.review }}。在“系统智能体、口播生成与审核模型”页面分别切换。</p>
    <form v-if="config" @submit.prevent="save">
      <div class="grid">
        <label>采纳分数<input v-model.number="config.cycle.threshold" type="number" min="0" max="100"></label>
        <label>评分最多重写次数<input v-model.number="config.cycle.max_retries" type="number" min="0" max="3"></label>
        <label>卖点最短间隔<input v-model.number="config.cycle.point_gap" type="number" min="0" max="100"></label>
        <label>同组合最短间隔<input v-model.number="config.cycle.combination_gap" type="number" min="0" max="100"></label>
      </div>
      <p>缓存时长、中间讲解段数、单段字数已移至用户方案的“实时声音”页面，随方案版本独立保存。安全改写只处理派生事实包与轮盘，不修改原始资料。</p>
      <button :disabled="busy">{{ busy ? '处理中…' : '保存生成配置' }}</button><button type="button" :disabled="busy" @click="load">重新读取</button>
    </form>
    <p v-if="error" class="error">{{ error }}</p><p v-if="notice" class="notice">{{ notice }}</p>
  </section>
</template>
<style scoped>
.generation-settings{background:white;border:1px solid #dbe3ec;border-radius:16px;padding:24px;margin:20px 0}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:16px}label{display:grid;gap:6px}input,select,button{padding:10px;border:1px solid #cdd7e0;border-radius:8px}button{margin-right:12px;color:#137f72}p{line-height:1.7;color:#657385}.error{color:#b92a42}.notice{color:#137f72}
</style>
