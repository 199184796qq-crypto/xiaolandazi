<script setup lang="ts">
import { ref, watch } from 'vue'
import { getContentPolicy, saveContentPolicy, type ContentPolicyRecord } from '../liveContentPolicy'
const props = defineProps<{ roomId?: number }>()
const record = ref<ContentPolicyRecord | null>(null)
const busy = ref(false)
const error = ref('')
const notice = ref('')
const refreshLabels: Record<string, string> = { pending: '等待生成', running: '生成文稿与声音中', retry: '等待重试', ready: '声音已就绪，等待到期及音轨边界', completed: '已更新', cancelled: '更新已取消，保留原成品' }
let loadedMode: ContentPolicyRecord['policy']['content_mode'] | undefined
let loadNo = 0
async function load() {
  const token = ++loadNo
  record.value = null; loadedMode = undefined; error.value = ''; notice.value = ''; busy.value = true
  try { const next = await getContentPolicy(props.roomId); if (token === loadNo) { record.value = next; loadedMode = next.policy.content_mode } }
  catch (e) { if (token === loadNo) error.value = e instanceof Error ? e.message : '读取更新策略失败' }
  finally { if (token === loadNo) busy.value = false }
}
async function save(inherit = false) {
  if (!record.value || busy.value) return
  const token = loadNo; const room = props.roomId
  busy.value = true; error.value = ''; notice.value = ''
  try {
    const next = await saveContentPolicy(record.value, room, inherit, loadedMode)
    if (token === loadNo) {
      record.value = next; loadedMode = next.policy.content_mode; notice.value = inherit ? '已恢复系统默认策略' : '更新策略已保存'
      window.dispatchEvent(new CustomEvent('live-content-policy-updated', { detail: { room_id: room || 0 } }))
    }
  } catch (e) { if (token === loadNo) error.value = e instanceof Error ? e.message : '保存失败' }
  finally { if (token === loadNo) busy.value = false }
}
watch(() => props.roomId, load, { immediate: true })
const timeFields = [
  { key: 'mainline_ttl_seconds', title: '主线文稿生命周期', min: 10, max: 1440 },
  { key: 'faq_ttl_seconds', title: '常见问题缓存生命周期', min: 5, max: 1440 },
  { key: 'refresh_ahead_seconds', title: '提前生成时间', min: 1, max: 360 },
  { key: 'min_repeat_seconds', title: '相同回答最短重复间隔', min: 0, max: 1440 },
] as const
function setMinutes(key: typeof timeFields[number]['key'], event: Event) {
  if (record.value) record.value.policy[key] = Number((event.target as HTMLInputElement).value) * 60
}
</script>

<template>
  <section id="content-update-policy" class="content-policy-panel">
    <header><div><h3>{{ roomId ? '运营配置 · 内容更新' : '直播内容与更新策略' }}</h3>
      <p>{{ roomId ? '客户已授权的直播间配置，用户端不可修改更新时间。' : '配置系统默认运行模式及主线、常见问题更新周期。' }}</p></div>
      <span v-if="record">{{ record.overridden ? '直播间独立配置' : '系统默认' }}</span>
    </header>
    <p v-if="error" role="alert" class="error">{{ error }}</p>
    <p v-if="notice" role="status">{{ notice }}</p>
    <p v-if="busy && !record">正在读取配置…</p>
    <fieldset v-if="record" :disabled="busy">
      <label class="wide">直播模式<select v-model="record.policy.content_mode">
        <option value="ai_pregenerated">1 · AI话术 + 提前生成声音 + AI实时互动</option>
        <option value="user_audio">2 · 用户上传录音 + AI实时互动</option>
        <option v-if="roomId && record.dynamic_authorized" value="ai_dynamic">3 · AI话术 + 动态生成 + AI实时互动（高级）</option>
      </select></label>
      <label v-if="roomId" class="toggle wide"><input v-model="record.dynamic_authorized" type="checkbox" @change="!record.dynamic_authorized && record.policy.content_mode === 'ai_dynamic' && (record.policy.content_mode = 'ai_pregenerated')" />开通AI动态生成高级模式（仅此直播间）</label>
      <label class="toggle wide"><input v-model="record.policy.auto_refresh_enabled" type="checkbox" />允许高级模式自动更新主线</label>
      <label v-for="field in timeFields" :key="field.key">{{ field.title }}（分钟）
        <input type="number" :min="field.min" :max="field.max" step="1" :value="record.policy[field.key] / 60" @input="setMinutes(field.key, $event)" />
      </label>
      <label>每批替换比例（%）<input v-model.number="record.policy.replacement_percent" type="number" min="1" max="100" /></label>
      <label>常见问题回答版本数<input v-model.number="record.policy.faq_variant_count" type="number" min="1" max="10" /></label>
      <p class="wide">系统默认主线与常见问题生命周期均为 120 分钟，每批轮换约 25% 主线片段。常见问题到期后按需重新生成；命中缓存不延长有效期。</p>
      <p class="wide">仅已开通且选择高级模式的开播直播间自动更新主线。新文稿与声音全部通过检查后，在音轨边界切换；失败保留原成品。原始录音不会被改写，开通权限不会自动切换模式或开播。正式依据发生变化导致旧稿不再适用时，请确认并重新发布。</p>
      <div v-if="roomId" class="wide" role="status">
        <strong>最近主线更新：{{ record.refresh_status ? (refreshLabels[record.refresh_status.status] || record.refresh_status.status) : '暂无任务（开通权限不会启动更新）' }}</strong>
        <p v-if="record.refresh_status">计划更新时间：{{ new Date(record.refresh_status.publish_after).toLocaleString() }} · 尝试 {{ record.refresh_status.attempts }} 次</p>
        <p v-if="record.refresh_status?.last_error" class="error">{{ record.refresh_status.last_error }}</p>
      </div>
      <div class="wide actions"><button type="button" class="primary-button" @click="save()">{{ busy ? '保存中…' : '保存更新策略' }}</button>
        <button v-if="roomId && record.overridden" type="button" class="ghost-button" @click="save(true)">恢复系统默认</button>
        <button type="button" class="ghost-button" @click="load">刷新</button></div>
    </fieldset>
  </section>
</template>

<style scoped>
.content-policy-panel{margin:18px 0;padding:22px;border:1px solid #dce3f4;border-radius:18px;background:linear-gradient(125deg,#fff,#f4f6ff)}header{display:flex;justify-content:space-between;gap:16px}h3{margin:0;font-size:21px;color:#263550}p{color:#6e7b92;line-height:1.6}header span{color:#5767cc;white-space:nowrap}fieldset{border:0;padding:0;display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}label{display:grid;gap:8px;font-size:15px;color:#374864}input,select{width:100%;box-sizing:border-box;padding:11px;border:1px solid #dbe2f0;border-radius:10px;font:inherit;background:white}.wide{grid-column:1/-1}.toggle{display:flex;align-items:center}.toggle input{width:18px}.actions{display:flex;gap:10px;flex-wrap:wrap}.error{color:#bb324b}@media(max-width:850px){fieldset{grid-template-columns:1fr}header{flex-wrap:wrap}}
</style>
