<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { getRoomInteractionPreferences, updateRoomInteractionPreferences } from '../api'
import type { InteractionControlKind, RoomInteractionPreferences, RoomInteractionPreferencesInput } from '../types'
const props = withDefaults(defineProps<{ roomId: number; compact?: boolean; mobile?: boolean; embedded?: boolean }>(), { compact: false, mobile: false, embedded: false })
const emit = defineEmits<{ (event: 'updated', value: RoomInteractionPreferences): void }>()
const rows: { key: InteractionControlKind; label: string; description: string; initial: number; hint: string }[] = [
  { key: 'welcome', label: '欢迎新人', description: '按进房与弹幕热度自适应，设置值作为下限。', initial: 0, hint: '0 = 自适应' },
  { key: 'like', label: '点赞感谢', description: '合并最近 30 秒不同观众的点赞。', initial: 30, hint: '人气高时自动延长' },
  { key: 'follow', label: '关注感谢', description: '合并最近 30 秒不同观众的关注。', initial: 20, hint: '人气高时自动延长' },
  { key: 'order', label: '下单播报', description: '播报已确认下单，未播成的补报沿用原配额。', initial: 5, hint: '补报仍按原规则重试' },
  { key: 'guide', label: '互动暖场', description: '距最后一次互动结束至少 180 秒才可暖场。', initial: 180, hint: '系统下限为 180 秒' },
  { key: 'semantic', label: '弹幕问答', description: '保留原问答筛选与配额，也控制“扣 1”等主动互动。', initial: 0, hint: '0 = 保留原节奏' },
  { key: 'urgent', label: '紧急投诉', description: '控制紧急投诉通道，关闭后不再自动播报。', initial: 0, hint: '间隔不可调' },
  { key: 'time_report', label: '报时', description: '在原报时计划上额外延后设置的秒数。', initial: 0, hint: '0 = 保留原节奏' },
  { key: 'dialogue', label: '追问澄清', description: '控制追问与后续对话，不改变弹幕筛选窗口。', initial: 0, hint: '0 = 保留原节奏' },
]
const loading = ref(true), saving = ref(false), error = ref(''), notice = ref('')
const value = ref<RoomInteractionPreferences>(), active = ref(true)
const switches = ref<Partial<Record<InteractionControlKind, boolean>>>({})
const seconds = ref<Partial<Record<InteractionControlKind, number | string>>>({})
let generation = 0
const busy = computed(() => loading.value || saving.value || !value.value)
const allOff = computed(() => !active.value || rows.every(row => switches.value[row.key] === false))
function hydrate(item: RoomInteractionPreferences) {
  value.value = item; active.value = item.interaction_active ?? item.overall_interaction > 0
  const legacy: Partial<Record<InteractionControlKind, boolean>> = { welcome: item.welcome_preference > 0, like: item.engagement_preference > 0, follow: item.engagement_preference > 0, order: item.conversion_preference > 0, semantic: item.question_preference > 0 || item.chat_preference > 0 }
  if (item.interaction_active === undefined && item.schedule) {
    const allowed = (scene: string) => { const rule = item.schedule?.events[scene]; return !rule || (rule.enabled && rule.weight > 0) }
    legacy.welcome = legacy.welcome && allowed('welcome'); legacy.like = legacy.like && allowed('like_follow'); legacy.follow = legacy.follow && allowed('like_follow'); legacy.order = legacy.order && allowed('order')
    legacy.semantic = (item.question_preference > 0 && allowed('product_question')) || (item.chat_preference > 0 && allowed('smalltalk'))
    legacy.urgent = allowed('complaint'); legacy.dialogue = allowed('product_question'); legacy.guide = allowed('warmup')
  }
  switches.value = Object.fromEntries(rows.map(row => [row.key, item.interaction_switches?.[row.key] ?? (item.interaction_active === undefined ? legacy[row.key] ?? true : true)]))
  seconds.value = Object.fromEntries(rows.filter(row => row.key !== 'urgent').map(row => [row.key, item.interaction_cooldowns?.[row.key] ?? row.initial]))
}
async function load(keepError = false) {
  const id = props.roomId, token = ++generation
  loading.value = true; saving.value = false; value.value = undefined
  if (!keepError) { error.value = ''; notice.value = '' }
  try { if (id) { const item = await getRoomInteractionPreferences(id); if (token === generation && id === props.roomId) hydrate(item) } }
  catch (err) { if (token === generation) error.value = err instanceof Error ? err.message : '读取互动设置失败' }
  finally { if (token === generation) loading.value = false }
}
function invalid(key: InteractionControlKind) { const n = seconds.value[key]; return n === '' || n === undefined || !Number.isInteger(Number(n)) || Number(n) < 0 || Number(n) > 3600 }
function belowDefault(key: InteractionControlKind, initial: number) { return !invalid(key) && Number(seconds.value[key]) < initial }
async function persist() {
  if (busy.value || !value.value) return
  const bad = rows.find(row => row.key !== 'urgent' && invalid(row.key))
  if (bad) { error.value = `${bad.label}的冷却时间必须为 0–3600 秒的整数`; return }
  const item = value.value, id = props.roomId, token = generation
  const payload: RoomInteractionPreferencesInput = {
    overall_interaction: item.overall_interaction, question_preference: item.question_preference, welcome_preference: item.welcome_preference,
    engagement_preference: item.engagement_preference, chat_preference: item.chat_preference, conversion_preference: item.conversion_preference, auto_heat: item.auto_heat,
    interaction_active: active.value, interaction_switches: { ...switches.value },
    interaction_cooldowns: Object.fromEntries(rows.filter(row => row.key !== 'urgent').map(row => [row.key, Number(seconds.value[row.key])])), interaction_controls_revision: item.interaction_controls_revision ?? 0,
  }
  // These room controls must not resave the immutable plan schedule.
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const saved = await updateRoomInteractionPreferences(id, payload)
    if (token !== generation || id !== props.roomId) return
    hydrate(saved); notice.value = '已保存并热生效。关闭时撤销未播任务，正在播出的互动正常完成。'; emit('updated', saved)
  } catch (err) {
    if (token !== generation || id !== props.roomId) return
    error.value = err instanceof Error ? err.message : '保存互动设置失败'
    // A failed hot-sync can have persisted. Reload the saved version for retry.
    await load(true)
  } finally { if (token === generation) saving.value = false }
}
function toggle(key?: InteractionControlKind) {
  if (busy.value || (key && !active.value)) return
  let restored = false
  for (const row of rows) { if (row.key !== 'urgent' && invalid(row.key)) { seconds.value[row.key] = value.value?.interaction_cooldowns?.[row.key] ?? row.initial; restored = true } }
  if (key) switches.value[key] = !switches.value[key]; else active.value = !active.value
  void persist().then(() => { if (restored && !error.value) notice.value += ' 无效的冷却时间未保存。' })
}
watch(() => props.roomId, () => void load(), { immediate: true })
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <section class="interaction-preferences" :class="{ compact, mobile, embedded }" aria-label="互动开关与冷却">
    <header v-if="!embedded"><strong>互动开关与冷却</strong><span>按房间保存，修改后立即生效。</span></header>
    <div v-if="error" class="message error" role="alert">{{ error }} <button type="button" :disabled="busy" @click="persist()">重试保存</button> <button type="button" :disabled="busy" @click="load()">重新读取</button></div>
    <div v-if="notice" class="message success" role="status">{{ notice }}</div>
    <div class="master"><div><strong>互动总开关</strong><span>{{ loading ? '正在读取设置…' : saving ? '正在保存…' : allOff ? '互动已关闭，主线继续播放' : '自动互动已开启' }}</span></div><button type="button" class="switch" role="switch" aria-label="互动总开关" :aria-checked="active" :disabled="busy" @click="toggle()"><i></i><span>{{ active ? '开' : '关' }}</span></button></div>
    <div class="list" :class="{ 'master-off': !active }">
      <article v-for="row in rows" :key="row.key" class="row" :class="{ off: switches[row.key] === false }">
        <div class="copy"><strong>{{ row.label }}</strong><span>{{ row.description }}</span></div>
        <div class="actions"><button type="button" class="switch" role="switch" :aria-label="`${row.label}开关`" :aria-checked="switches[row.key] !== false" :disabled="busy || !active" @click="toggle(row.key)"><i></i><span>{{ switches[row.key] !== false ? '开' : '关' }}</span></button>
          <label v-if="row.key !== 'urgent'" class="interval"><span>{{ row.key === 'time_report' ? '额外冷却' : '冷却时间' }}</span><input v-model="seconds[row.key]" type="number" min="0" max="3600" step="1" inputmode="numeric" :aria-label="`${row.label}冷却时间`" :aria-invalid="invalid(row.key)" :disabled="busy || !active || switches[row.key] === false" @change="persist()" @keydown.enter.prevent="persist()"><span>秒</span></label><span v-else class="fixed">间隔不可调</span>
        </div>
        <p v-if="row.key !== 'urgent'" class="hint" :class="{ warning: belowDefault(row.key, row.initial) }">{{ row.hint }}<template v-if="belowDefault(row.key, row.initial)"> · 低于默认值，仍受系统下限及自适应限制。</template></p>
      </article>
    </div>
    <footer>一般冷却从实际播报开始计算；暖场从上次互动结束计算，报时是在原计划上额外延后。填 0 不会关闭互动，请使用开关。</footer>
  </section>
</template>

<style scoped>
.interaction-preferences{display:grid;gap:14px;min-width:0;color:#26354f;font-size:16px;line-height:1.5}header,.master>div,.copy{display:grid;gap:4px;min-width:0}header strong{font-size:20px}header span,footer{color:#64748b}.master{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:18px;border:1px solid #dce2ff;border-radius:16px;background:linear-gradient(115deg,#f1f3ff,#fafbff)}.master strong{font-size:18px}.master>div span,.copy span{color:#61718b}.list{display:grid;gap:10px}.row{display:grid;grid-template-columns:minmax(150px,1fr) auto;gap:10px 16px;align-items:center;padding:16px;border:1px solid #e3e8f2;border-radius:14px;background:white}.copy strong{font-size:17px}.actions{display:flex;align-items:center;gap:18px}.interval{display:flex;align-items:center;gap:8px;white-space:nowrap;color:#52627d}.interval input{width:88px;min-height:42px;box-sizing:border-box;border:1px solid #cad3e5;border-radius:9px;padding:8px;color:#28364e;background:#fff;font:inherit;text-align:center}.interval input:focus{outline:2px solid #8a81ff;outline-offset:2px}.interval input[aria-invalid=true]:not(:disabled){border-color:#d56565}.switch{display:flex;align-items:center;gap:7px;flex:none;min-height:42px;min-width:76px;border:0;border-radius:24px;padding:7px 10px;background:#dbe2ec;color:#44536a;font:inherit;font-weight:700;cursor:pointer}.switch i{display:block;width:24px;height:24px;border-radius:50%;background:#fff;box-shadow:0 1px 4px #0002}.switch[aria-checked=true]{background:#6554ef;color:white;flex-direction:row-reverse}.switch:focus-visible{outline:3px solid #a49aff;outline-offset:3px}.switch:disabled{cursor:not-allowed;opacity:.55}.master-off .row,.row.off{background:#f6f7fa;color:#788399}.master-off .copy,.off .copy{opacity:.65}.interval input:disabled{background:#eaeef4;color:#8a95a7}.hint{grid-column:1/-1;margin:0;color:#657695}.hint.warning{color:#9a6520}.fixed{min-width:206px;text-align:right;color:#79859b}.message{padding:12px 14px;border-radius:10px;overflow-wrap:anywhere}.error{background:#fff0f0;color:#a23944}.success{background:#edf9f4;color:#27785b}.message button{padding:5px 9px;border:1px solid currentColor;border-radius:7px;background:transparent;color:inherit;font:inherit;cursor:pointer}footer{padding:0 3px}.compact .row{grid-template-columns:1fr;padding:14px}.compact .actions{justify-content:space-between}.compact .master{padding:14px}@media(max-width:680px){.row{grid-template-columns:1fr}.actions{justify-content:space-between;gap:10px}.interval{gap:6px}.interval input{width:78px}.master{padding:14px}.fixed{min-width:0}}
</style>
