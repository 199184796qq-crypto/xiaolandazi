<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getSpeechLibraryPolicy, saveSpeechLibraryPolicy, type SpeechLibraryPolicy } from '../api'
const policy = ref<SpeechLibraryPolicy>({ capacity: 3, one_rate: 30, two_rate: 60, full_rate: 70, cooldown_seconds: 180 })
const loaded = ref(false), busy = ref(false), error = ref(''), notice = ref(''), revision = ref('')
const stats = ref({ Texts: 0, Audio: 0, PlayedReuses: 0 })
async function load() {
  busy.value = true; error.value = ''; loaded.value = false
  try { const v = await getSpeechLibraryPolicy(); policy.value = v.policy; revision.value = v.revision; stats.value = v.stats; loaded.value = true }
  catch (e) { error.value = e instanceof Error ? e.message : '读取话术库设置失败' }
  finally { busy.value = false }
}
async function save() {
  const p = policy.value
  if (![p.capacity, p.one_rate, p.two_rate, p.full_rate, p.cooldown_seconds].every(Number.isInteger) || p.capacity < 1 || p.capacity > 20 || p.one_rate < 0 || p.one_rate > p.two_rate || p.two_rate > p.full_rate || p.full_rate > 100 || p.cooldown_seconds < 0 || p.cooldown_seconds > 86400) { error.value = '请填写有效容量、递增的复用率和重复间隔'; return }
  busy.value = true; error.value = ''; notice.value = ''
  try { const v = await saveSpeechLibraryPolicy(p, revision.value); policy.value = v.policy; revision.value = v.revision; stats.value = v.stats; notice.value = '话术库设置已保存，后续请求生效。' }
  catch (e) { error.value = e instanceof Error ? e.message : '保存话术库设置失败' }
  finally { busy.value = false }
}
onMounted(load)
</script>
<template>
  <section class="library-settings">
    <header><div><h3>主线跨场话术库</h3><p>只复用同一房间、同一方案且内容未变化的正文。开场、讲解和个性化互动沿用原有生成流程。</p></div><button type="button" :disabled="busy" @click="load">刷新话术库设置</button></header>
    <p>卖点、角度、事实、价格、福利、风格或规则变化，旧文字和声音退出复用；仅音色、声音模型或语速变化时，保留文字并重新合成声音。库内资产长期保留，淘汰后等待播放引用释放再清理。</p>
    <p v-if="error" role="alert" class="error">{{ error }}</p><p v-if="notice" role="status" class="notice">{{ notice }}</p>
    <form @submit.prevent="save">
      <label>每个卖点与角度保留条数<input v-model.number="policy.capacity" type="number" min="1" max="20" step="1" :disabled="!loaded || busy"></label>
      <label>已有一条时复用率（%）<input v-model.number="policy.one_rate" type="number" min="0" max="100" step="1" :disabled="!loaded || busy"></label>
      <label>已有两条时复用率（%）<input v-model.number="policy.two_rate" type="number" min="0" max="100" step="1" :disabled="!loaded || busy"></label>
      <label>已有三条及以上复用率（%）<input v-model.number="policy.full_rate" type="number" min="0" max="100" step="1" :disabled="!loaded || busy"></label>
      <label>同条话术最短重复间隔（秒）<input v-model.number="policy.cooldown_seconds" type="number" min="0" max="86400" step="1" :disabled="!loaded || busy"></label>
      <button type="submit" :disabled="!loaded || busy">{{ busy ? '处理中…' : '保存话术库设置' }}</button>
    </form>
    <p v-if="loaded">当前有效文字 {{ stats.Texts }} 条 · 有效声音 {{ stats.Audio }} 份 · 最近24小时已实际起播的复用 {{ stats.PlayedReuses }} 次</p>
    <p>沿用主线复用开关和用户单独设置。声音保留时间用于临时缓存，不改变长期话术库或直播下载的保留规则。</p>
  </section>
</template>
<style scoped>
.library-settings{padding:24px;background:#f8f9ff;border:1px solid #e0e5f5;border-radius:16px;margin:24px 0;color:#253658}header{align-items:flex-start;display:flex;justify-content:space-between;gap:16px;flex-wrap:wrap}h3{margin:0;font-size:21px}p{line-height:1.7;color:#627394}form{display:grid;grid-template-columns:repeat(auto-fit,minmax(210px,1fr));gap:16px;align-items:end}label{display:flex;flex-direction:column;gap:9px;font-weight:600}input{width:100%;box-sizing:border-box;border:1px solid #d7def0;border-radius:8px;padding:10px;color:#243454;font-size:16px}button{background:#6155f5;border:0;border-radius:9px;padding:12px 18px;color:white;cursor:pointer;font-weight:600}button:disabled{opacity:.5;cursor:default}.error{color:#b33232}.notice{color:#18865c}@media(max-width:480px){.library-settings{padding:16px}form{grid-template-columns:1fr}}
</style>
