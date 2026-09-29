<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getRoomInteractionPreferences, updateRoomInteractionPreferences } from '../api'
import type {
  ConversionInteractionLevel,
  InteractionPreferenceLevel,
  OverallInteractionLevel,
  RoomInteractionPreferences,
  RoomInteractionPreferencesInput,
} from '../types'

const props = withDefaults(defineProps<{
  roomId: number
  compact?: boolean
  mobile?: boolean
}>(), {
  compact: false,
  mobile: false,
})

const emit = defineEmits<{
  (event: 'updated', value: RoomInteractionPreferences): void
}>()

const loading = ref(false)
const savingKey = ref('')
const error = ref('')
const notice = ref('')
const value = ref<RoomInteractionPreferences>({
  tenant_id: 0,
  room_id: props.roomId,
  overall_interaction: 'natural',
  question_preference: 'natural',
  welcome_preference: 'natural',
  engagement_preference: 'natural',
  chat_preference: 'natural',
  conversion_preference: 'natural',
  auto_heat: true,
})

const commonOptions: Array<{ value: InteractionPreferenceLevel; label: string }> = [
  { value: 'less', label: '少一些' },
  { value: 'natural', label: '自然' },
  { value: 'more', label: '多一些' },
]

const overallOptions: Array<{ value: OverallInteractionLevel; label: string }> = [
  { value: 'quiet', label: '安静一点' },
  { value: 'natural', label: '自然互动' },
  { value: 'active', label: '热情一点' },
]

const conversionOptions: Array<{ value: ConversionInteractionLevel; label: string }> = [
  { value: 'steady', label: '稳一点' },
  { value: 'natural', label: '自然' },
  { value: 'active', label: '积极' },
]

const rows = computed(() => [
  { key: 'question_preference', label: '回答问题', desc: '问题、追问和重复问题更优先', options: commonOptions },
  { key: 'welcome_preference', label: '欢迎新人', desc: '人少时可更积极，人多自动聚合', options: commonOptions },
  { key: 'engagement_preference', label: '点赞关注', desc: '感谢点赞和关注，但不会逐条打断', options: commonOptions },
  { key: 'chat_preference', label: '聊天互动', desc: '普通聊天和非问题弹幕的回应倾向', options: commonOptions },
  { key: 'conversion_preference', label: '成交互动', desc: '价格、规格、库存、物流等购买信号', options: conversionOptions },
])

function inputPayload(): RoomInteractionPreferencesInput {
  return {
    overall_interaction: value.value.overall_interaction,
    question_preference: value.value.question_preference,
    welcome_preference: value.value.welcome_preference,
    engagement_preference: value.value.engagement_preference,
    chat_preference: value.value.chat_preference,
    conversion_preference: value.value.conversion_preference,
    auto_heat: value.value.auto_heat,
  }
}

async function load() {
  if (!props.roomId) return
  loading.value = true
  error.value = ''
  try {
    value.value = await getRoomInteractionPreferences(props.roomId)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取互动偏好失败'
  } finally {
    loading.value = false
  }
}

async function persist(key: string) {
  if (!props.roomId || savingKey.value) return
  savingKey.value = key
  error.value = ''
  notice.value = ''
  try {
    value.value = await updateRoomInteractionPreferences(props.roomId, inputPayload())
    notice.value = '已生效'
    emit('updated', value.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存互动偏好失败'
    await load()
  } finally {
    savingKey.value = ''
  }
}

function chooseOverall(next: OverallInteractionLevel) {
  if (value.value.overall_interaction === next) return
  value.value.overall_interaction = next
  void persist('overall_interaction')
}

function chooseRow(key: string, next: string) {
  if ((value.value as unknown as Record<string, unknown>)[key] === next) return
  ;(value.value as unknown as Record<string, unknown>)[key] = next
  void persist(key)
}

function toggleAutoHeat() {
  value.value.auto_heat = !value.value.auto_heat
  void persist('auto_heat')
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
</script>

<template>
  <section class="interaction-preferences" :class="{ compact, mobile }">
    <header>
      <div>
        <strong>互动偏好</strong>
        <span>你决定更想回应什么，小蓝会按直播间热度自动调整节奏。</span>
      </div>
      <button type="button" class="heat-switch" :class="{ active: value.auto_heat }" :disabled="loading || Boolean(savingKey)" @click="toggleAutoHeat">
        <i></i>
        {{ value.auto_heat ? '智能随人气调整' : '固定偏好' }}
      </button>
    </header>

    <div v-if="error" class="interaction-pref-error">{{ error }}</div>

    <div class="interaction-overall">
      <div>
        <strong>整体互动</strong>
        <span>控制整场互动的总体积极程度</span>
      </div>
      <div class="interaction-segments">
        <button
          v-for="option in overallOptions"
          :key="option.value"
          type="button"
          :class="{ active: value.overall_interaction === option.value }"
          :disabled="loading || Boolean(savingKey)"
          @click="chooseOverall(option.value)"
        >{{ option.label }}</button>
      </div>
    </div>

    <div class="interaction-pref-grid">
      <article v-for="row in rows" :key="row.key">
        <div class="interaction-pref-copy">
          <strong>{{ row.label }}</strong>
          <span>{{ row.desc }}</span>
        </div>
        <div class="interaction-segments small">
          <button
            v-for="option in row.options"
            :key="option.value"
            type="button"
            :class="{ active: (value as any)[row.key] === option.value }"
            :disabled="loading || Boolean(savingKey)"
            @click="chooseRow(row.key, option.value)"
          >{{ option.label }}</button>
        </div>
      </article>
    </div>

    <footer>
      <span v-if="loading">正在读取…</span>
      <span v-else-if="savingKey">正在生效…</span>
      <span v-else-if="notice">{{ notice }}</span>
      <span v-else>人少时会更积极互动，人多时自动聚合并优先重要问题。</span>
    </footer>
  </section>
</template>

<style scoped>
.interaction-preferences{display:grid;gap:16px;padding:18px;border:1px solid #d7def0;border-radius:18px;background:#fff;box-shadow:0 10px 28px rgba(55,72,128,.08)}
.interaction-preferences>header{display:flex;align-items:center;justify-content:space-between;gap:18px}
.interaction-preferences>header>div{display:grid;gap:5px}
.interaction-preferences header strong{color:#26344f;font-size:22px;font-weight:950}
.interaction-preferences header span,.interaction-pref-copy span,.interaction-overall span,.interaction-preferences footer{color:#4e5c74;font-size:14px;font-weight:700;line-height:1.55}
.heat-switch{display:flex;align-items:center;gap:8px;min-height:38px;padding:0 13px;border:1px solid #cad3e7;border-radius:999px;background:#f6f8fc;color:#43516b;font-size:13px;font-weight:900;cursor:pointer}
.heat-switch i{width:9px;height:9px;border-radius:50%;background:#9da8b9}
.heat-switch.active{border-color:#b9dfce;background:#eff9f4;color:#2f7758}.heat-switch.active i{background:#48a779;box-shadow:0 0 0 4px rgba(72,167,121,.11)}
.interaction-overall,.interaction-pref-grid article{display:grid;grid-template-columns:minmax(150px,.7fr) minmax(280px,1.3fr);align-items:center;gap:18px;padding:14px 16px;border:1px solid #e3e8f2;border-radius:14px;background:#fbfcff}
.interaction-overall>div:first-child,.interaction-pref-copy{display:grid;gap:4px}
.interaction-overall strong,.interaction-pref-copy strong{color:#293751;font-size:17px;font-weight:950}
.interaction-pref-grid{display:grid;gap:10px}
.interaction-segments{display:grid;grid-template-columns:repeat(3,1fr);gap:8px}
.interaction-segments button{min-height:42px;padding:0 12px;border:1px solid #ccd5e7;border-radius:11px;background:#fff;color:#35435d;font-size:15px;font-weight:900;cursor:pointer;transition:.16s ease}
.interaction-segments button:hover:not(:disabled){border-color:#8794e5;transform:translateY(-1px)}
.interaction-segments button.active{border-color:#6877df;background:#5f6fd8;color:#fff;box-shadow:0 7px 16px rgba(75,91,202,.18)}
.interaction-segments button:disabled{cursor:not-allowed;opacity:.58}
.interaction-pref-error{padding:10px 12px;border-radius:10px;background:#fff0f1;color:#b34450;font-size:14px;font-weight:800}
.interaction-preferences footer{padding-top:2px}
.interaction-preferences.compact{gap:10px;padding:12px 10px}
.interaction-preferences.compact>header{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:start;gap:6px 12px}
.interaction-preferences.compact>header>div{display:contents}
.interaction-preferences.compact header strong{grid-column:1;grid-row:1;font-size:20px;line-height:1.25}
.interaction-preferences.compact header span{grid-column:1 / -1;grid-row:2;max-width:none;font-size:14px;line-height:1.45}
.interaction-preferences.compact .heat-switch{grid-column:2;grid-row:1;min-height:34px;padding:0 11px;font-size:12.5px;white-space:nowrap}
.interaction-preferences.compact .interaction-overall,.interaction-preferences.compact .interaction-pref-grid article{grid-template-columns:1fr;gap:8px;padding:10px 10px;border-radius:12px}
.interaction-preferences.compact .interaction-overall>div:first-child,.interaction-preferences.compact .interaction-pref-copy{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:baseline;gap:0 10px}
.interaction-preferences.compact .interaction-overall strong,.interaction-preferences.compact .interaction-pref-copy strong{font-size:16px;line-height:1.35;white-space:nowrap}
.interaction-preferences.compact .interaction-pref-copy span,.interaction-preferences.compact .interaction-overall span{min-width:0;font-size:13.5px;line-height:1.4}
.interaction-preferences.compact .interaction-pref-grid{gap:8px}
.interaction-preferences.compact .interaction-segments{gap:7px}
.interaction-preferences.compact .interaction-segments button{min-height:38px;padding:0 8px;font-size:14.5px;border-radius:10px}
.interaction-preferences.compact .interaction-pref-error{padding:8px 10px;font-size:13px}
.interaction-preferences.compact footer{font-size:12.5px;line-height:1.4}
@media (max-width: 760px){
  .interaction-preferences{padding:14px;border-radius:15px}
  .interaction-preferences>header{align-items:flex-start;flex-direction:column}
  .interaction-preferences header strong{font-size:20px}
  .interaction-overall,.interaction-pref-grid article{grid-template-columns:1fr;gap:10px}
  .interaction-segments button{min-height:44px;font-size:14px}
  .heat-switch{width:100%;justify-content:center}
}
</style>
