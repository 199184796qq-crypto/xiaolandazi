<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getRoomInteractionPreferences, updateRoomInteractionPreferences } from '../api'
import PreferenceSlider from './ui/PreferenceSlider.vue'
import type {
  RoomInteractionPreferences,
  RoomInteractionPreferencesInput,
} from '../types'

const props = withDefaults(defineProps<{
  roomId: number
  compact?: boolean
  mobile?: boolean
  embedded?: boolean
}>(), {
  compact: false,
  mobile: false,
  embedded: false,
})

const emit = defineEmits<{
  (event: 'updated', value: RoomInteractionPreferences): void
}>()

const loading = ref(false)
const savingKey = ref('')
const error = ref('')
const notice = ref('')
const preferenceCommand = ref('')
const value = ref<RoomInteractionPreferences>({
  tenant_id: 0,
  room_id: props.roomId,
  overall_interaction: 50,
  question_preference: 50,
  welcome_preference: 50,
  engagement_preference: 50,
  chat_preference: 50,
  conversion_preference: 50,
  auto_heat: true,
})

type PreferenceKey = keyof Pick<
  RoomInteractionPreferencesInput,
  'overall_interaction'
  | 'question_preference'
  | 'welcome_preference'
  | 'engagement_preference'
  | 'chat_preference'
  | 'conversion_preference'
>

const rows = computed(() => [
  { key: 'question_preference' as PreferenceKey, label: '回答问题', desc: '优先回复用户问题', icon: '▣', tone: 'violet' },
  { key: 'welcome_preference' as PreferenceKey, label: '欢迎新人', desc: '新人进入时的互动', icon: '+', tone: 'amber' },
  { key: 'engagement_preference' as PreferenceKey, label: '点赞关注', desc: '感谢点赞和关注', icon: '♥', tone: 'rose' },
  { key: 'chat_preference' as PreferenceKey, label: '聊天互动', desc: '普通聊天和非问题弹幕', icon: '•••', tone: 'sky' },
  { key: 'conversion_preference' as PreferenceKey, label: '成交互动', desc: '价格 / 库存 / 购买信号', icon: '▰', tone: 'mint' },
])

function predictionLevel(score: number) {
  if (score === 0) return '关闭'
  if (score <= 30) return '偏少'
  if (score <= 70) return '自然'
  return '偏多'
}

const predictionItems = computed(() => [
  { label: '问题互动', value: predictionLevel(value.value.question_preference), tone: 'violet' },
  { label: '欢迎新人', value: predictionLevel(value.value.welcome_preference), tone: 'mint' },
  { label: '成交互动', value: predictionLevel(value.value.conversion_preference), tone: 'purple' },
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

function finishSlider(key: PreferenceKey) {
  void persist(key)
}

function toggleAutoHeat() {
  value.value.auto_heat = !value.value.auto_heat
  void persist('auto_heat')
}

function applyNaturalPreferenceCommand() {
  const text = preferenceCommand.value.trim()
  if (!text) return
  let matched = false
  const has = (...terms: string[]) => terms.some((term) => text.includes(term))
  const set = (key: PreferenceKey, next: number) => {
    value.value[key] = next
    matched = true
  }

  if (has('完全不互动', '关闭互动', '不要互动')) set('overall_interaction', 0)
  else if (has('安静一点', '少互动', '互动少一点')) set('overall_interaction', 25)
  if (has('热情一点', '多互动', '积极互动')) set('overall_interaction', 75)

  if (has('不要回答问题', '关闭回答问题', '问题不互动')) set('question_preference', 0)
  else if (has('多回答', '多答问题', '优先回答')) set('question_preference', 75)
  else if (has('少回答', '少答问题')) set('question_preference', 25)

  if (has('不要欢迎新人', '关闭欢迎新人', '新人不欢迎')) set('welcome_preference', 0)
  else if (has('多欢迎', '多迎新', '新人多欢迎')) set('welcome_preference', 75)
  else if (has('少欢迎', '别总欢迎', '不要一直欢迎')) set('welcome_preference', 25)

  if (has('点赞不用', '不用感谢点赞', '关闭点赞互动')) set('engagement_preference', 0)
  else if (has('少感谢点赞', '点赞少一点')) set('engagement_preference', 25)
  else if (has('多感谢点赞', '多感谢关注', '点赞多互动')) set('engagement_preference', 75)

  if (has('不要聊天', '关闭聊天', '不闲聊')) set('chat_preference', 0)
  else if (has('多聊天', '多聊两句')) set('chat_preference', 75)
  else if (has('少聊天', '别闲聊')) set('chat_preference', 25)

  if (has('不促单', '关闭成交互动', '成交不互动')) set('conversion_preference', 0)
  else if (has('成交积极', '多促单', '多逼单')) set('conversion_preference', 75)
  else if (has('成交稳一点', '少逼单', '别一直促单')) set('conversion_preference', 25)
  if (has('人少', '人多', '热度', '人气', '自动调整')) {
    value.value.auto_heat = true
    matched = true
  }
  if (!matched) {
    error.value = '没有识别到明确偏好，可以说“多回答问题、少感谢点赞、人少多欢迎”。'
    return
  }
  error.value = ''
  preferenceCommand.value = ''
  void persist('natural_language')
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
</script>

<template>
  <section class="interaction-preferences" :class="{ compact, mobile, embedded }">
    <header v-if="!embedded" class="preference-header">
      <div class="preference-heading">
        <strong>互动偏好</strong>
        <span>你决定更想回应什么；滑到最左侧 0 时，该项绝对不自动互动。</span>
      </div>
      <div class="smart-badge" :class="{ active: value.auto_heat }">
        <i></i>
        {{ loading ? '正在读取' : savingKey ? '正在生效' : value.auto_heat ? '智能随人气调整' : '固定偏好' }}
      </div>
    </header>

    <div v-if="error" class="interaction-pref-error">{{ error }}</div>

    <div class="preference-list">
      <article class="preference-row overall-row">
        <div class="preference-row-main">
          <span class="preference-icon tone-blue" aria-hidden="true">≋</span>
          <div class="interaction-pref-copy">
            <strong>整体互动</strong>
            <span>控制整体互动频率</span>
          </div>
        </div>
        <PreferenceSlider
          v-model="value.overall_interaction"
          :compact="compact"
          :disabled="loading || Boolean(savingKey)"
          aria-label="整体互动频率"
          @commit="finishSlider('overall_interaction')"
        />
      </article>

      <article v-for="row in rows" :key="row.key" class="preference-row">
        <div class="preference-row-main">
          <span class="preference-icon" :class="'tone-' + row.tone" aria-hidden="true">{{ row.icon }}</span>
          <div class="interaction-pref-copy">
            <strong>{{ row.label }}</strong>
            <span>{{ row.desc }}</span>
          </div>
        </div>
        <PreferenceSlider
          v-model="value[row.key]"
          :compact="compact"
          :disabled="loading || Boolean(savingKey)"
          :aria-label="row.label"
          @commit="finishSlider(row.key)"
        />
      </article>
    </div>

    <section class="ai-prediction-strip">
      <div class="ai-prediction-title">
        <span class="ai-spark" aria-hidden="true">✦</span>
        <div>
          <strong>AI 预测</strong>
          <small>基于当前互动偏好与直播节奏</small>
        </div>
      </div>
      <div class="prediction-items">
        <div v-for="item in predictionItems" :key="item.label" class="prediction-item" :class="'tone-' + item.tone">
          <i></i>
          <span>{{ item.label }}</span>
          <strong>{{ item.value }}</strong>
        </div>
      </div>
    </section>

    <section class="natural-pref-command">
      <span class="ai-spark small" aria-hidden="true">✦</span>
      <input
        v-model="preferenceCommand"
        type="text"
        :disabled="loading || Boolean(savingKey)"
        placeholder="直接告诉小蓝：人少多欢迎，多回答问题，点赞不用太频繁感谢"
        @keyup.enter="applyNaturalPreferenceCommand"
      />
      <button type="button" :disabled="loading || Boolean(savingKey) || !preferenceCommand.trim()" @click="applyNaturalPreferenceCommand">应用</button>
    </section>

    <section class="auto-adjust-row">
      <div class="auto-adjust-copy">
        <span class="ai-spark small" aria-hidden="true">✦</span>
        <div>
          <strong>AI 自动调节</strong>
          <span>根据在线人数、互动密度和成交阶段，智能优化上述设置。</span>
          <small v-if="notice && !savingKey">{{ notice }}</small>
        </div>
      </div>
      <button
        type="button"
        class="auto-toggle"
        :class="{ active: value.auto_heat }"
        :aria-pressed="value.auto_heat"
        :disabled="loading || Boolean(savingKey)"
        @click="toggleAutoHeat"
      >
        <i></i>
      </button>
    </section>
  </section>
</template>

<style scoped>
.interaction-preferences{display:grid;gap:12px;padding:18px;border:1px solid #dce4f4;border-radius:20px;background:linear-gradient(180deg,#fff 0%,#fbfcff 100%);box-shadow:0 14px 34px rgba(65,83,140,.08)}
.preference-header{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:start;gap:8px 16px}
.preference-heading{display:grid;gap:6px;min-width:0}
.preference-heading strong{color:#172a52;font-size:26px;font-weight:950;letter-spacing:-.5px}
.preference-heading span{color:#53627d;font-size:14px;font-weight:750;line-height:1.55}
.smart-badge{display:inline-flex;align-items:center;gap:8px;min-height:38px;padding:0 13px;border:1px solid #d8dfeb;border-radius:999px;background:#f7f9fc;color:#637087;font-size:13px;font-weight:900;white-space:nowrap}
.smart-badge i{width:9px;height:9px;border-radius:50%;background:#a7b1c2}
.smart-badge.active{border-color:#bfe9d7;background:linear-gradient(180deg,#effbf5,#e9f8f1);color:#277653;box-shadow:inset 0 0 0 1px rgba(79,185,136,.04)}
.smart-badge.active i{background:#37b87c;box-shadow:0 0 0 5px rgba(55,184,124,.12)}
.interaction-pref-error{padding:10px 12px;border:1px solid #f3ced3;border-radius:11px;background:#fff2f3;color:#b34450;font-size:13px;font-weight:850}
.preference-list{display:grid;gap:8px}
.preference-row{display:grid;grid-template-columns:minmax(210px,1fr) minmax(300px,460px);align-items:center;gap:18px;padding:9px 12px;border:1px solid #e9edf7;border-radius:13px;background:linear-gradient(90deg,#f7f9fe 0%,#fbfcff 56%,#fff 100%);transition:border-color .18s ease,box-shadow .18s ease,transform .18s ease}
.preference-row:hover{border-color:#d4dcf3;box-shadow:0 7px 18px rgba(79,99,164,.07);transform:translateY(-1px)}
.preference-row-main{display:grid;grid-template-columns:46px minmax(0,1fr);align-items:center;gap:12px;min-width:0}
.preference-icon{display:grid;place-items:center;width:46px;height:46px;border-radius:12px;font-size:19px;font-weight:950;letter-spacing:-2px}
.preference-icon.tone-blue{background:linear-gradient(145deg,#edf5ff,#dceaff);color:#4384ee}.preference-icon.tone-violet{background:linear-gradient(145deg,#f1eaff,#e6dbff);color:#7750db}.preference-icon.tone-amber{background:linear-gradient(145deg,#fff7e6,#ffebbf);color:#e99a10}.preference-icon.tone-rose{background:linear-gradient(145deg,#fff0f4,#ffdce7);color:#ef4f79}.preference-icon.tone-sky{background:linear-gradient(145deg,#eaf8ff,#d8efff);color:#3294dc}.preference-icon.tone-mint{background:linear-gradient(145deg,#eafbf4,#d7f5e7);color:#26a66d}
.interaction-pref-copy{display:grid;gap:3px;min-width:0}
.interaction-pref-copy strong{color:#172b53;font-size:17px;font-weight:950;line-height:1.25}
.interaction-pref-copy span{color:#62718d;font-size:13px;font-weight:750;line-height:1.35}
.ai-prediction-strip{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:14px;padding:10px 12px;border:1px solid #dfe5f5;border-radius:13px;background:linear-gradient(100deg,#fbfcff,#f7f9ff)}
.ai-prediction-title{display:flex;align-items:center;gap:10px;padding-right:14px;border-right:1px solid #dce3f2}
.ai-prediction-title>div{display:grid;gap:2px}
.ai-prediction-title strong{color:#5668df;font-size:15px;font-weight:950;white-space:nowrap}
.ai-prediction-title small{color:#8490a8;font-size:10.5px;font-weight:750;white-space:nowrap}
.ai-spark{display:grid;place-items:center;width:28px;height:28px;color:#5268ef;font-size:26px;line-height:1;text-shadow:0 5px 12px rgba(82,104,239,.2)}
.ai-spark.small{width:25px;height:25px;font-size:22px}
.prediction-items{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px}
.prediction-item{display:flex;align-items:center;justify-content:center;gap:5px;min-width:0;color:#4e5d79;font-size:11px;font-weight:800;white-space:nowrap}
.prediction-item i{width:9px;height:9px;border-radius:3px;background:#8d9aae}.prediction-item strong{font-size:11px;font-weight:950}
.prediction-item.tone-violet i{background:#d853cb}.prediction-item.tone-violet strong{color:#ac43a8}.prediction-item.tone-mint i{background:#57c899}.prediction-item.tone-mint strong{color:#2c956d}.prediction-item.tone-purple i{background:#7a63de}.prediction-item.tone-purple strong{color:#6955c9}
.natural-pref-command{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:8px;padding:8px 10px;border:1px solid #e1e6f4;border-radius:12px;background:#f9faff}
.natural-pref-command input{min-width:0;height:34px;padding:0 10px;border:1px solid #dfe5f0;border-radius:9px;background:#fff;color:#2d4168;font:inherit;font-size:11.5px;font-weight:700;outline:none}
.natural-pref-command input:focus{border-color:#8797ef;box-shadow:0 0 0 3px rgba(99,119,232,.08)}
.natural-pref-command button{height:34px;padding:0 12px;border:1px solid #6477eb;border-radius:9px;background:#6074e9;color:#fff;font-size:11.5px;font-weight:900;cursor:pointer}
.natural-pref-command button:disabled,.natural-pref-command input:disabled{opacity:.55;cursor:not-allowed}
.auto-adjust-row{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:14px;padding:11px 13px;border:1px solid #dfe5f4;border-radius:13px;background:#fff}
.auto-adjust-copy{display:flex;align-items:flex-start;gap:10px;min-width:0}
.auto-adjust-copy>div{display:grid;gap:2px;min-width:0}
.auto-adjust-copy strong{color:#5567df;font-size:15px;font-weight:950}
.auto-adjust-copy span{color:#6b7890;font-size:11.5px;font-weight:750;line-height:1.4}
.auto-adjust-copy small{color:#30a16e;font-size:10.5px;font-weight:900}
.auto-toggle{position:relative;width:48px;height:28px;padding:0;border:0;border-radius:999px;background:#c9d1df;cursor:pointer;transition:.18s ease;box-shadow:inset 0 1px 3px rgba(38,53,89,.12)}
.auto-toggle i{position:absolute;top:4px;left:4px;width:20px;height:20px;border-radius:50%;background:#fff;box-shadow:0 2px 7px rgba(38,53,89,.2);transition:.18s ease}
.auto-toggle.active{background:linear-gradient(90deg,#5570ef,#657cff)}.auto-toggle.active i{left:24px}.auto-toggle:disabled{cursor:not-allowed;opacity:.55}
.interaction-preferences.embedded{padding:0;border:0;border-radius:0;background:transparent;box-shadow:none}
.interaction-preferences.compact{gap:9px;padding:14px 12px;border-radius:17px}
.interaction-preferences.compact.embedded{padding:0}
.interaction-preferences.compact .preference-heading strong{font-size:21px}
.interaction-preferences.compact .preference-heading span{font-size:12.5px;line-height:1.4}
.interaction-preferences.compact .smart-badge{min-height:32px;padding:0 9px;font-size:11px}
.interaction-preferences.compact .smart-badge i{width:8px;height:8px}
.interaction-preferences.compact .preference-list{gap:6px}
.interaction-preferences.compact .preference-row{grid-template-columns:minmax(132px,42%) minmax(0,1fr);gap:10px;padding:8px 9px;border-radius:11px}
.interaction-preferences.compact .preference-row-main{grid-template-columns:36px minmax(0,1fr);gap:8px}
.interaction-preferences.compact .preference-icon{width:36px;height:36px;border-radius:10px;font-size:15px}
.interaction-preferences.compact .interaction-pref-copy{gap:1px}
.interaction-preferences.compact .interaction-pref-copy strong{font-size:14px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.interaction-preferences.compact .interaction-pref-copy span{font-size:10.5px;line-height:1.25;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.interaction-preferences.compact .ai-prediction-strip{grid-template-columns:1fr;gap:8px;padding:9px 10px}
.interaction-preferences.compact .ai-prediction-title{padding-right:0;padding-bottom:7px;border-right:0;border-bottom:1px solid #e4e8f3}
.interaction-preferences.compact .ai-prediction-title small{white-space:normal}
.interaction-preferences.compact .prediction-items{gap:4px}
.interaction-preferences.compact .prediction-item{display:grid;grid-template-columns:8px 1fr;justify-content:start;gap:1px 5px;font-size:9.5px;white-space:normal}
.interaction-preferences.compact .prediction-item i{grid-row:1 / span 2;align-self:center;width:7px;height:7px}
.interaction-preferences.compact .prediction-item strong{grid-column:2;font-size:10px}
.interaction-preferences.compact .auto-adjust-row{padding:9px 10px}
.interaction-preferences.compact .auto-adjust-copy{gap:7px}
.interaction-preferences.compact .auto-adjust-copy strong{font-size:13px}
.interaction-preferences.compact .auto-adjust-copy span{font-size:10.5px}
.interaction-preferences.compact .auto-toggle{width:44px;height:26px}.interaction-preferences.compact .auto-toggle i{width:18px;height:18px}.interaction-preferences.compact .auto-toggle.active i{left:22px}
@media (max-width:760px){
  .interaction-preferences{padding:14px;border-radius:16px}
  .preference-header{grid-template-columns:1fr}.smart-badge{justify-self:start}
  .preference-row{grid-template-columns:1fr;gap:9px}
  .ai-prediction-strip{grid-template-columns:1fr}.ai-prediction-title{padding:0 0 8px;border-right:0;border-bottom:1px solid #e2e7f2}
  .prediction-items{grid-template-columns:1fr}
}
</style>
