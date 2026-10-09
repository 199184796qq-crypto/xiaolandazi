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
const roles = [ ['interaction_profile_id', '互动文字生成'], ['style_profile_id', '主播风格提取'], ['scorecard_profile_id', '评分基准提取'], ['selling_point_profile_id', '卖点与角度挖掘'], ['safety_profile_id', '派生事实/卖点安全改写'] ] as const
async function load() {
  busy.value = true; error.value = ''
  try {
    const [settings, models] = await Promise.all([request<Settings>('/api/v1/system/anchor-generation-settings'), request<{ config: { profiles: Profile[] } }>('/api/v1/system/speech-models')])
    config.value = settings; profiles.value = models.config.profiles.filter(p => p.enabled && p.has_api_key)
  } catch (e) { error.value = e instanceof Error ? e.message : '读取生成配置失败' }
  finally { busy.value = false }
}
async function save() {
  busy.value = true; error.value = ''; notice.value = ''
  try { config.value = await request<Settings>('/api/v1/system/anchor-generation-settings', { method: 'PUT', body: JSON.stringify(config.value) }); notice.value = '已保存，新调教/初始化/新场采用此配置；正在运行的场次保持原配置快照。' }
  catch (e) { error.value = e instanceof Error ? e.message : '保存失败' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section id="anchor-generation" class="generation-settings">
    <h2>主播文字生成模型</h2>
    <p>各角色使用后台所选模型，自动加载对应提示词配置并关闭思考。主线由程序维护近期上下文，切换模型不改变声音设置。</p>
    <form v-if="config" @submit.prevent="save">
      <div class="grid">
        <label v-for="[key,label] in roles" :key="key">{{ label }}<select v-model="config[key]" :disabled="busy"><option value="">{{ key === 'safety_profile_id' ? '复用卖点挖掘模型' : key === 'interaction_profile_id' ? '使用默认文字模型' : '请选择' }}</option><option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }} · {{ p.model }}</option></select></label>
        <label>主线文字生成<select v-model="config.cycle.text_profile_id"><option value="">使用原 Agent 会话</option><option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }} · {{ p.model }}</option></select></label>
        <label v-if="!config.cycle.text_profile_id">阿里凭据配置<select v-model="config.cycle.credential_profile_id"><option value="">请选择</option><option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option></select></label>
        <label>回灌辅助模型<select v-model="config.cycle.feedback_profile_id"><option value="">请选择</option><option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }} · {{ p.model }}</option></select></label>
        <label v-if="!config.cycle.text_profile_id">Workspace ID<input v-model="config.cycle.workspace_id"></label>
        <label v-if="!config.cycle.text_profile_id">地域<input v-model="config.cycle.region" placeholder="cn-beijing"></label>
        <label v-if="!config.cycle.text_profile_id">母版 Agent ID<input v-model="config.cycle.agent_id"></label>
        <label v-if="!config.cycle.text_profile_id">Environment ID<input v-model="config.cycle.environment_id" placeholder="留空则查询可用环境"></label>
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
