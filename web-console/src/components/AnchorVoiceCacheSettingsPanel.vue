<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getAnchorVoiceCachePolicy, updateAnchorVoiceCachePolicy } from '../api'
const hours = ref(5), loading = ref(false), saving = ref(false), error = ref(''), notice = ref('')
const loaded = ref(false)
const mainlineReuse = ref(true), interactionReuse = ref(true)
function apply(value: Awaited<ReturnType<typeof getAnchorVoiceCachePolicy>>) {
  hours.value = value.retention_hours
  mainlineReuse.value = value.mainline_cache_reuse_enabled
  interactionReuse.value = value.interaction_cache_reuse_enabled
}
async function load() {
  loading.value = true; error.value = ''
  try { apply(await getAnchorVoiceCachePolicy()); loaded.value = true }
  catch (value) { error.value = value instanceof Error ? value.message : '读取声音缓存配置失败' }
  finally { loading.value = false }
}
async function save() {
  if (!Number.isInteger(hours.value) || hours.value < 1 || hours.value > 168) { error.value = '保留时间须为1～168小时的整数'; return }
  saving.value = true; error.value = ''; notice.value = ''
  try {
    apply(await updateAnchorVoiceCachePolicy(hours.value, { mainline_cache_reuse_enabled: mainlineReuse.value, interaction_cache_reuse_enabled: interactionReuse.value }))
    notice.value = '已保存。新请求按开关决定是否复用成品；关闭时实时生成文字与声音。已有缓存保留原到期时间。'
  }
  catch (value) { error.value = value instanceof Error ? value.message : '保存声音缓存配置失败' }
  finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <section id="anchor-voice-cache" class="cache-settings">
    <h2>话术与声音成品缓存</h2>
    <p>关闭复用后，新请求实时生成文字与声音，可用于测试模型和TTS实际费用。已有推演方向及事实资料保留。</p>
    <form @submit.prevent="save">
      <label>保留时间（小时）<input v-model.number="hours" type="number" min="1" max="168" step="1" :disabled="loading || saving"></label>
      <label class="reuse-toggle"><input v-model="mainlineReuse" type="checkbox" :disabled="loading || saving || !loaded">复用主线话术与声音成品</label>
      <label class="reuse-toggle"><input v-model="interactionReuse" type="checkbox" :disabled="loading || saving || !loaded">复用互动回答与声音成品</label>
      <button type="submit" :disabled="loading || saving || !loaded">{{ saving ? '保存中…' : '保存配置' }}</button>
      <button type="button" :disabled="loading || saving" @click="load">重新读取</button>
    </form>
    <p>复用开关对保存后的新请求生效；已生成或正在处理的内容保持原结果。保留时间用于新生成的声音、SRT和时间轴文件，命中不续期。模型服务商的输入缓存折扣仍按实际账单记录。</p>
    <p v-if="error" class="error">{{ error }}</p><p v-if="notice" class="success">{{ notice }}</p>
  </section>
</template>

<style scoped>
.cache-settings { padding: 24px; margin: 20px 0; background: white; border: 1px solid #dce6e8; border-radius: 16px; }
p { color: #647583; line-height: 1.6; }form { display: flex; gap: 12px; flex-wrap: wrap; align-items: end; }label { display: grid; gap: 8px; }
input,button { padding: 10px 14px; border: 1px solid #cedddd; border-radius: 8px; }button { cursor: pointer; color: #126f65; }button:disabled { opacity: .5; }
.reuse-toggle { display: flex; align-items: center; gap: 8px; padding-bottom: 10px; }
.error { color: #b32d41; }.success { color: #126f65; }
</style>
