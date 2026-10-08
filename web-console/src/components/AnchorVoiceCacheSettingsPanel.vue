<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getAnchorVoiceCachePolicy, updateAnchorVoiceCachePolicy } from '../api'
const hours = ref(5), loading = ref(false), saving = ref(false), error = ref(''), notice = ref('')
const loaded = ref(false)
async function load() {
  loading.value = true; error.value = ''
  try { hours.value = (await getAnchorVoiceCachePolicy()).retention_hours; loaded.value = true }
  catch (value) { error.value = value instanceof Error ? value.message : '读取声音缓存配置失败' }
  finally { loading.value = false }
}
async function save() {
  if (!Number.isInteger(hours.value) || hours.value < 1 || hours.value > 168) { error.value = '保留时间须为1～168小时的整数'; return }
  saving.value = true; error.value = ''; notice.value = ''
  try { hours.value = (await updateAnchorVoiceCachePolicy(hours.value)).retention_hours; notice.value = '已保存。之后新生成的缓存使用新时长，已存在缓存保持原到期时间。' }
  catch (value) { error.value = value instanceof Error ? value.message : '保存声音缓存配置失败' }
  finally { saving.value = false }
}
onMounted(load)
</script>

<template>
  <section id="anchor-voice-cache" class="cache-settings">
    <h2>OSS声音缓存</h2>
    <p>运维统一控制生成声音及SRT、时间轴、语义切点包的缓存生命周期。默认5小时，普通用户不能修改。</p>
    <form @submit.prevent="save">
      <label>保留时间（小时）<input v-model.number="hours" type="number" min="1" max="168" step="1" :disabled="loading || saving"></label>
      <button type="submit" :disabled="loading || saving || !loaded">{{ saving ? '保存中…' : '保存配置' }}</button>
      <button type="button" :disabled="loading || saving" @click="load">重新读取</button>
    </form>
    <p>修改后，对之后新生成的缓存生效（正在运行的生成队列也会读取新值），不追改已有缓存。命中不续期；到期立即停止复用，后台每分钟清理OSS文件，停机后恢复补清理。不会删除已有正式声音资产。</p>
    <p v-if="error" class="error">{{ error }}</p><p v-if="notice" class="success">{{ notice }}</p>
  </section>
</template>

<style scoped>
.cache-settings { padding: 24px; margin: 20px 0; background: white; border: 1px solid #dce6e8; border-radius: 16px; }
p { color: #647583; line-height: 1.6; }form { display: flex; gap: 12px; flex-wrap: wrap; align-items: end; }label { display: grid; gap: 8px; }
input,button { padding: 10px 14px; border: 1px solid #cedddd; border-radius: 8px; }button { cursor: pointer; color: #126f65; }button:disabled { opacity: .5; }
.error { color: #b32d41; }.success { color: #126f65; }
</style>
