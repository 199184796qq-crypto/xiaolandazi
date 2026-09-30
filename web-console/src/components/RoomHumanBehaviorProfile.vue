<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getRoomHumanBehaviorProfile, updateRoomHumanBehaviorProfile } from '../api'
import type { RoomHumanBehaviorProfile } from '../types'

const props = defineProps<{ roomId: number; compact?: boolean; mobile?: boolean; embedded?: boolean }>()

const value = ref<RoomHumanBehaviorProfile>({
  tenant_id: 0,
  room_id: props.roomId,
  trait_text: '',
  state_text: '',
})
const loading = ref(false)
const saving = ref(false)
const notice = ref('')
const error = ref('')
const stateDuration = ref<'session' | '30m' | '2h' | 'today'>('session')
type BehaviorPickerTarget = 'trait' | 'state'
const pickerOpen = ref(false)
const pickerTarget = ref<BehaviorPickerTarget>('trait')
const pickerSelection = ref<string[]>([])
const pickerCustomItems = ref<string[]>([])
const pickerError = ref('')

const defaultTraitItems = [
  '喜欢短句',
  '说话偏慢',
  '说话偏快',
  '喜欢先重复一下观众的问题',
  '回答前会先确认一下问题',
  '转场前喜欢先总结一句',
  '喜欢先说重点再补充',
  '喜欢先解释原因再给结论',
  '少用长句',
  '少用夸张语气',
  '语气自然一点',
  '语气热情一点',
  '喜欢偶尔说“你看哈”',
  '喜欢偶尔说“对吧”',
  '喜欢偶尔说“是不是”',
  '回答完喜欢自然接回主线',
]

const defaultStateItems = [
  '今天嗓子有点不舒服',
  '今天有点咳嗽',
  '今天有点感冒',
  '今天有点累',
  '今天有点困',
  '今天精神一般',
  '今天状态很好',
  '今天比较兴奋',
  '今天有点紧张',
  '今天声音想轻一点',
  '今天长句少一点',
  '今天想说慢一点',
]

const pickerTitle = computed(() => pickerTarget.value === 'trait' ? '选择主播长期习惯' : '选择今天的状态')
const pickerOptions = computed(() => pickerTarget.value === 'trait' ? defaultTraitItems : defaultStateItems)
const pickerHint = computed(() => pickerTarget.value === 'trait'
  ? '勾选后点确定，会自动填入主播习惯；你自己写的内容会保留。'
  : '这些只是后台控制参数，不会直接作为直播台词说出来。')

function behaviorItems(raw: string) {
  const seen = new Set<string>()
  return raw
    .split(/[；;\n]+/)
    .map((item) => item.trim())
    .filter((item) => {
      if (!item || seen.has(item)) return false
      seen.add(item)
      return true
    })
    .slice(0, 12)
}

function openPicker(target: BehaviorPickerTarget) {
  pickerTarget.value = target
  pickerError.value = ''
  const raw = target === 'trait' ? value.value.trait_text : value.value.state_text
  const current = behaviorItems(raw)
  const defaults = new Set(pickerOptions.value)
  pickerSelection.value = current.filter((item) => defaults.has(item))
  pickerCustomItems.value = current.filter((item) => !defaults.has(item))
  pickerOpen.value = true
}

function closePicker() {
  pickerOpen.value = false
  pickerError.value = ''
}

function isPickerSelected(item: string) {
  return pickerSelection.value.includes(item)
}

function togglePickerItem(item: string) {
  const index = pickerSelection.value.indexOf(item)
  if (index >= 0) {
    pickerSelection.value.splice(index, 1)
    pickerError.value = ''
    return
  }
  if (pickerCustomItems.value.length + pickerSelection.value.length >= 12) {
    pickerError.value = '每栏最多 12 项'
    return
  }
  pickerSelection.value.push(item)
  pickerError.value = ''
}

function confirmPicker() {
  const merged = behaviorItems([...pickerCustomItems.value, ...pickerSelection.value].join('；'))
  if (pickerTarget.value === 'trait') {
    value.value.trait_text = merged.join('；')
  } else {
    value.value.state_text = merged.join('；')
  }
  closePicker()
}

function expiryISO() {
  if (!value.value.state_text.trim()) return undefined
  const now = new Date()
  if (stateDuration.value === '30m') return new Date(now.getTime() + 30 * 60 * 1000).toISOString()
  if (stateDuration.value === 'today') {
    const end = new Date(now)
    end.setHours(23, 59, 59, 999)
    return end.toISOString()
  }
  // 当前直播方案最长按 2 小时执行；“当前场”因此使用同一上限并自动失效。
  return new Date(now.getTime() + 2 * 60 * 60 * 1000).toISOString()
}

async function load() {
  if (!props.roomId) return
  loading.value = true
  error.value = ''
  try {
    value.value = await getRoomHumanBehaviorProfile(props.roomId)
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取主播行为配置失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!props.roomId || saving.value) return
  saving.value = true
  notice.value = ''
  error.value = ''
  try {
    value.value = await updateRoomHumanBehaviorProfile(props.roomId, {
      trait_text: value.value.trait_text.trim(),
      state_text: value.value.state_text.trim(),
      state_expires_at: expiryISO(),
    })
    notice.value = '已生效，新口播任务立即使用'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存主播行为配置失败'
  } finally {
    saving.value = false
  }
}

function clearState() {
  value.value.state_text = ''
  void save()
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
</script>

<template>
  <section class="human-behavior-profile" :class="{ compact, mobile, embedded }">
    <header v-if="!embedded">
      <div>
        <strong>主播状态</strong>
        <span>用自然语言告诉小蓝你平时怎么说、今天是什么状态。</span>
      </div>
      <em>{{ loading ? '读取中' : saving ? '保存中' : notice || '热生效' }}</em>
    </header>

    <div v-if="error" class="profile-error">{{ error }}</div>

    <label class="profile-field">
      <span>
        <b>主播习惯</b>
        <small>长期保留</small>
      </span>
      <div class="behavior-input-wrap">
        <textarea
          v-model="value.trait_text"
          :disabled="loading || saving"
          maxlength="1000"
          placeholder="例如：我说话偏慢，喜欢短句，偶尔说“你看哈”，转场前喜欢先总结一句。"
        />
        <button
          type="button"
          class="behavior-picker-trigger"
          title="从默认主播习惯中选择"
          aria-label="选择主播习惯"
          :disabled="loading || saving"
          @click="openPicker('trait')"
        ><span></span><span></span><span></span></button>
      </div>
    </label>

    <label class="profile-field">
      <span>
        <b>今天的状态</b>
        <small>到期自动失效</small>
      </span>
      <div class="behavior-input-wrap">
        <textarea
          v-model="value.state_text"
          :disabled="loading || saving"
          maxlength="600"
          placeholder="例如：今天嗓子有点不舒服，声音轻一点、长句少一点。"
        />
        <button
          type="button"
          class="behavior-picker-trigger"
          title="从默认状态中选择"
          aria-label="选择今天的状态"
          :disabled="loading || saving"
          @click="openPicker('state')"
        ><span></span><span></span><span></span></button>
      </div>
    </label>

    <div class="state-expiry">
      <span>状态有效期</span>
      <div>
        <button type="button" :class="{ active: stateDuration === 'session' }" @click="stateDuration = 'session'">当前场</button>
        <button type="button" :class="{ active: stateDuration === '30m' }" @click="stateDuration = '30m'">30 分钟</button>
        <button type="button" :class="{ active: stateDuration === '2h' }" @click="stateDuration = '2h'">2 小时</button>
        <button type="button" :class="{ active: stateDuration === 'today' }" @click="stateDuration = 'today'">今天</button>
      </div>
    </div>

    <footer class="human-main-footer">
      <button v-if="value.state_text" type="button" class="ghost" :disabled="saving" @click="clearState">恢复正常状态</button>
      <button type="button" class="save" :disabled="loading || saving" @click="save">
        {{ saving ? '正在生效…' : '保存并立即生效' }}
      </button>
    </footer>

    <Teleport to="body">
      <div v-if="pickerOpen" class="behavior-picker-backdrop" @click.self="closePicker">
        <section class="behavior-picker-dialog" role="dialog" aria-modal="true" :aria-label="pickerTitle">
          <header class="behavior-picker-header">
            <div>
              <strong>{{ pickerTitle }}</strong>
              <span>{{ pickerHint }}</span>
            </div>
            <button type="button" class="behavior-picker-close" aria-label="关闭" @click="closePicker">×</button>
          </header>

          <div class="behavior-picker-meta">
            <span>已选 {{ pickerSelection.length + pickerCustomItems.length }} / 12</span>
            <em v-if="pickerCustomItems.length">已保留 {{ pickerCustomItems.length }} 条自定义内容</em>
          </div>

          <div class="behavior-picker-grid">
            <button
              v-for="item in pickerOptions"
              :key="item"
              type="button"
              class="behavior-picker-option"
              :class="{ selected: isPickerSelected(item) }"
              @click="togglePickerItem(item)"
            >
              <i aria-hidden="true">{{ isPickerSelected(item) ? '✓' : '' }}</i>
              <span>{{ item }}</span>
            </button>
          </div>

          <div v-if="pickerTarget === 'state'" class="behavior-picker-note">
            状态只控制语速、句长、停顿、声音力度和情绪，不会直接念成“我咳嗽了”“我不舒服”等台词。
          </div>

          <div v-if="pickerError" class="behavior-picker-error">{{ pickerError }}</div>

          <footer class="behavior-picker-actions">
            <button type="button" class="picker-cancel" @click="closePicker">取消</button>
            <button type="button" class="picker-confirm" @click="confirmPicker">确定</button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.human-behavior-profile{display:grid;gap:12px;padding:16px;border:1px solid #e1e7f2;border-radius:18px;background:#fff;box-shadow:0 12px 30px rgba(55,75,126,.06)}
.human-behavior-profile.embedded{padding:0;border:0;border-radius:0;background:transparent;box-shadow:none}
header{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:start}header>div{display:grid;gap:4px}header strong{color:#172b53;font-size:20px;font-weight:950}header span{color:#697792;font-size:12px;font-weight:700;line-height:1.5}header em{padding:5px 9px;border-radius:999px;background:#edf8f2;color:#2e8b62;font-size:11px;font-style:normal;font-weight:900}
.profile-error{padding:8px 10px;border-radius:10px;background:#fff1f2;color:#b74652;font-size:12px;font-weight:800}
.profile-field{display:grid;gap:7px}.profile-field>span{display:flex;align-items:center;gap:8px}.profile-field b{color:#20355d;font-size:14px}.profile-field small{color:#8793a8;font-size:10px}
textarea{width:100%;min-height:72px;resize:vertical;box-sizing:border-box;padding:10px 12px;border:1px solid #dfe5f0;border-radius:11px;background:#fafbfe;color:#25395e;font:inherit;font-size:13px;line-height:1.55;outline:none}textarea:focus{border-color:#8797ef;box-shadow:0 0 0 3px rgba(99,119,232,.1)}
.behavior-input-wrap{position:relative;min-width:0}
.behavior-input-wrap textarea{padding-right:52px}
.behavior-picker-trigger{position:absolute;right:8px;top:9px;display:flex;align-items:center;justify-content:center;gap:3px;width:34px;height:30px;padding:0;border:1px solid #dce3f0;border-radius:9px;background:#fff;color:#687792;cursor:pointer;box-shadow:0 3px 8px rgba(52,72,118,.06)}
.behavior-picker-trigger:hover{border-color:#9aa9ef;background:#f4f6ff}
.behavior-picker-trigger span{display:block;width:4px;height:4px;border-radius:50%;background:currentColor}
.behavior-picker-trigger:disabled{opacity:.45;cursor:not-allowed}
.behavior-picker-backdrop{position:fixed;inset:0;z-index:9999;display:grid;place-items:center;padding:24px;background:rgba(25,38,66,.28);backdrop-filter:blur(3px)}
.behavior-picker-dialog{width:min(760px,calc(100vw - 40px));max-height:min(760px,calc(100vh - 48px));overflow:auto;padding:22px;border:1px solid #dce4f2;border-radius:22px;background:#fff;box-shadow:0 28px 80px rgba(27,42,77,.24)}
.behavior-picker-header{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px;align-items:start}
.behavior-picker-header>div{display:grid;gap:5px}
.behavior-picker-header strong{color:#172b53;font-size:22px;font-weight:950}
.behavior-picker-header span{color:#75829a;font-size:12px;font-weight:700;line-height:1.5}
.behavior-picker-close{width:34px;height:34px;border:1px solid #e0e5ee;border-radius:10px;background:#f8f9fc;color:#63708a;font-size:24px;line-height:1;cursor:pointer}
.behavior-picker-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:16px;padding:9px 11px;border-radius:10px;background:#f7f9fd;color:#60708b;font-size:11px;font-weight:800}
.behavior-picker-meta em{color:#7c67d9;font-style:normal}
.behavior-picker-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:9px;margin-top:14px}
.behavior-picker-option{display:grid;grid-template-columns:22px minmax(0,1fr);align-items:center;gap:8px;min-height:46px;padding:8px 10px;border:1px solid #e0e5ef;border-radius:11px;background:#fff;color:#33496f;text-align:left;font-size:12px;font-weight:850;cursor:pointer;transition:.18s ease}
.behavior-picker-option:hover{border-color:#aab7ef;background:#f8f9ff;transform:translateY(-1px)}
.behavior-picker-option i{display:grid;place-items:center;width:20px;height:20px;border:1.5px solid #cfd7e6;border-radius:6px;background:#fff;color:#fff;font-size:12px;font-style:normal}
.behavior-picker-option.selected{border-color:#7b8cf0;background:#f2f4ff;color:#253e78;box-shadow:0 6px 14px rgba(85,103,220,.1)}
.behavior-picker-option.selected i{border-color:#6377ea;background:#6377ea}
.behavior-picker-note{margin-top:14px;padding:10px 12px;border:1px solid #dcebe4;border-radius:10px;background:#f1faf5;color:#507260;font-size:11.5px;font-weight:750;line-height:1.55}
.behavior-picker-error{margin-top:12px;padding:8px 10px;border-radius:9px;background:#fff0f1;color:#b84750;font-size:12px;font-weight:800}
.behavior-picker-actions{display:flex;justify-content:flex-end;gap:9px;margin-top:18px}
.behavior-picker-actions button{min-width:86px;height:38px;border-radius:10px;font-size:12px;font-weight:900;cursor:pointer}
.picker-cancel{border:1px solid #dce2ed;background:#fff;color:#60708c}
.picker-confirm{border:1px solid #5d71e8;background:#6075eb;color:#fff;box-shadow:0 7px 16px rgba(77,96,222,.2)}
.state-expiry{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:10px}.state-expiry>span{color:#60708c;font-size:12px;font-weight:850}.state-expiry>div{display:flex;flex-wrap:wrap;gap:5px}.state-expiry button,.ghost,.save{border:1px solid #dfe5f0;border-radius:9px;background:#fff;color:#405171;font-size:11px;font-weight:850;cursor:pointer}.state-expiry button{padding:6px 9px}.state-expiry button.active{border-color:#6579ea;background:#eef1ff;color:#5266da}
.human-main-footer{display:flex;justify-content:flex-end;gap:8px}.ghost,.save{min-height:34px;padding:0 12px}.save{border-color:#586de5;background:#5c71ea;color:#fff}.ghost{color:#6f7c93}
button:disabled,textarea:disabled{opacity:.55;cursor:not-allowed}
.compact{gap:9px;padding:12px;border-radius:14px}.compact header strong{font-size:16px}.compact textarea{min-height:58px}.mobile{box-shadow:none}
@media(max-width:760px){header{grid-template-columns:1fr}.state-expiry{grid-template-columns:1fr}.human-main-footer{justify-content:stretch}.human-main-footer button{flex:1}.behavior-picker-dialog{width:calc(100vw - 24px);padding:16px;border-radius:16px}.behavior-picker-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.behavior-picker-meta{align-items:flex-start;flex-direction:column}}
</style>
