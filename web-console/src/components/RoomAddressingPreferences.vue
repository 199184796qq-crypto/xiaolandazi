<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getRoomAddressingPreferences, updateRoomAddressingPreferences } from '../api'
import type { AddressingPreferenceLevel, RoomAddressingPreferences } from '../types'

const props = defineProps<{ roomId: number; compact?: boolean; mobile?: boolean; embedded?: boolean }>()

const value = ref<RoomAddressingPreferences>({
  tenant_id: 0,
  room_id: props.roomId,
  naming_preference: 'natural',
  preferred_terms: [],
  blocked_terms: [],
})
const preferredInput = ref('')
const blockedInput = ref('')
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const notice = ref('')
type PickerTarget = 'preferred' | 'blocked'
const pickerOpen = ref(false)
const pickerTarget = ref<PickerTarget>('preferred')
const pickerSelection = ref<string[]>([])
const pickerCustomTerms = ref<string[]>([])
const pickerError = ref('')

const defaultPreferredTerms = [
  '朋友','老哥','老妹','兄弟','姐妹','大哥','大姐','小哥',
  '美女','帅哥','叔叔','阿姨','老板','朋友们','大家','各位朋友',
  '新来的朋友','直播间的朋友','老朋友','新朋友','哥们','姐们',
  '老铁','家人们','宝子','宝贝','亲','姐姐','妹妹','哥哥',
]
const defaultBlockedTerms = [
  '宝子','家人们','老铁','亲','宝贝','宝宝','姐妹们','兄弟们',
  '宝宝们','家人','亲爱的','老板','美女','帅哥','大哥','大姐',
  '哥哥','姐姐','妹妹','老哥','老妹','朋友们','各位','铁子',
]

const pickerTitle = computed(() => pickerTarget.value === 'preferred' ? '选择常用观众称谓' : '选择禁用观众称谓')
const pickerOptions = computed(() => pickerTarget.value === 'preferred' ? defaultPreferredTerms : defaultBlockedTerms)

const options: Array<{ value: AddressingPreferenceLevel; label: string }> = [
  { value: 'less', label: '少点名' },
  { value: 'natural', label: '自然' },
  { value: 'more', label: '多点名' },
]

function terms(raw: string) {
  const seen = new Set<string>()
  return raw
    .split(/[，,、/\n]+/)
    .map((item) => item.trim())
    .filter((item) => {
      if (!item || seen.has(item)) return false
      seen.add(item)
      return true
    })
    .slice(0, 12)
}

function syncInputs() {
  preferredInput.value = (value.value.preferred_terms || []).join('、')
  blockedInput.value = (value.value.blocked_terms || []).join('、')
}

function openPicker(target: PickerTarget) {
  pickerTarget.value = target
  pickerError.value = ''
  const current = terms(target === 'preferred' ? preferredInput.value : blockedInput.value)
  const defaults = new Set(pickerOptions.value)
  pickerSelection.value = current.filter((item) => defaults.has(item))
  pickerCustomTerms.value = current.filter((item) => !defaults.has(item))
  pickerOpen.value = true
}

function closePicker() {
  pickerOpen.value = false
  pickerError.value = ''
}

function isPickerSelected(term: string) {
  return pickerSelection.value.includes(term)
}

function togglePickerTerm(term: string) {
  const index = pickerSelection.value.indexOf(term)
  if (index >= 0) {
    pickerSelection.value.splice(index, 1)
    pickerError.value = ''
    return
  }
  if (pickerCustomTerms.value.length + pickerSelection.value.length >= 12) {
    pickerError.value = '每栏最多 12 个称谓'
    return
  }
  pickerSelection.value.push(term)
  pickerError.value = ''
}

function confirmPicker() {
  const merged = terms([...pickerCustomTerms.value, ...pickerSelection.value].join('、'))
  if (pickerTarget.value === 'preferred') {
    preferredInput.value = merged.join('、')
  } else {
    blockedInput.value = merged.join('、')
  }
  closePicker()
}

async function load() {
  if (!props.roomId) return
  loading.value = true
  error.value = ''
  try {
    value.value = await getRoomAddressingPreferences(props.roomId)
    syncInputs()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取称呼习惯失败'
  } finally {
    loading.value = false
  }
}

async function save(nextPreference?: AddressingPreferenceLevel) {
  if (!props.roomId || saving.value) return
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    value.value = await updateRoomAddressingPreferences(props.roomId, {
      naming_preference: nextPreference || value.value.naming_preference,
      preferred_terms: terms(preferredInput.value),
      blocked_terms: terms(blockedInput.value),
    })
    syncInputs()
    notice.value = '已生效，新口播任务立即使用'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存称呼习惯失败'
    await load()
  } finally {
    saving.value = false
  }
}

function choose(next: AddressingPreferenceLevel) {
  if (value.value.naming_preference === next || saving.value) return
  value.value.naming_preference = next
  void save(next)
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
</script>

<template>
  <section class="addressing-preferences" :class="{ compact, mobile, embedded }">
    <header v-if="!embedded">
      <div>
        <strong>称呼习惯</strong>
        <span>控制点名频率，并告诉小蓝哪些称呼更像你、哪些不要说。</span>
      </div>
      <em>{{ loading ? '读取中' : saving ? '保存中' : notice || '热生效' }}</em>
    </header>

    <div v-if="error" class="addressing-error">{{ error }}</div>

    <div class="naming-level">
      <span>点名频率</span>
      <div>
        <button
          v-for="option in options"
          :key="option.value"
          type="button"
          :class="{ active: value.naming_preference === option.value }"
          :disabled="loading || saving"
          @click="choose(option.value)"
        >{{ option.label }}</button>
      </div>
    </div>

    <label>
      <span><b>称呼观众时常用</b><small>可选，只用于叫对方，不是主播自称</small></span>
      <div class="term-input-wrap">
        <input
          v-model="preferredInput"
          :disabled="loading || saving"
          maxlength="240"
          placeholder="朋友、老哥、大家"
          @keyup.enter="save()"
        />
        <button
          type="button"
          class="term-picker-trigger"
          title="从默认称谓中选择"
          aria-label="选择常用观众称谓"
          :disabled="loading || saving"
          @click="openPicker('preferred')"
        ><span></span><span></span><span></span></button>
      </div>
    </label>

    <label>
      <span><b>禁用观众称谓</b><small>优先级最高，正文禁止使用</small></span>
      <div class="term-input-wrap">
        <input
          v-model="blockedInput"
          :disabled="loading || saving"
          maxlength="240"
          placeholder="例如：宝子、家人们"
          @keyup.enter="save()"
        />
        <button
          type="button"
          class="term-picker-trigger"
          title="从默认称谓中选择"
          aria-label="选择禁用观众称谓"
          :disabled="loading || saving"
          @click="openPicker('blocked')"
        ><span></span><span></span><span></span></button>
      </div>
    </label>

    <footer class="addressing-main-footer">
      <span>昵称仍会自动过滤广告、符号、难读名称和最近刚点过的人。</span>
      <button type="button" :disabled="loading || saving" @click="save()">{{ saving ? '正在生效…' : '保存称呼习惯' }}</button>
    </footer>

    <Teleport to="body">
      <div v-if="pickerOpen" class="term-picker-backdrop" @click.self="closePicker">
        <section class="term-picker-dialog" role="dialog" aria-modal="true" :aria-label="pickerTitle">
          <header class="term-picker-header">
            <div>
              <strong>{{ pickerTitle }}</strong>
              <span>勾选后点确定，会自动填入当前输入框；自定义内容会保留。</span>
            </div>
            <button type="button" class="term-picker-close" aria-label="关闭" @click="closePicker">×</button>
          </header>

          <div class="term-picker-meta">
            <span>已选 {{ pickerSelection.length + pickerCustomTerms.length }} / 12</span>
            <em v-if="pickerCustomTerms.length">已保留 {{ pickerCustomTerms.length }} 个自定义称谓</em>
          </div>

          <div class="term-picker-grid">
            <button
              v-for="term in pickerOptions"
              :key="term"
              type="button"
              class="term-picker-option"
              :class="{ selected: isPickerSelected(term) }"
              @click="togglePickerTerm(term)"
            >
              <i aria-hidden="true">{{ isPickerSelected(term) ? '✓' : '' }}</i>
              <span>{{ term }}</span>
            </button>
          </div>

          <div v-if="pickerError" class="term-picker-error">{{ pickerError }}</div>

          <footer class="term-picker-actions">
            <button type="button" class="picker-cancel" @click="closePicker">取消</button>
            <button type="button" class="picker-confirm" @click="confirmPicker">确定</button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.addressing-preferences{display:grid;gap:11px;padding:16px;border:1px solid #e1e7f2;border-radius:18px;background:#fff;box-shadow:0 12px 30px rgba(55,75,126,.06)}
.addressing-preferences.embedded{padding:0;border:0;border-radius:0;background:transparent;box-shadow:none}
header{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;align-items:start}header>div{display:grid;gap:4px}header strong{color:#172b53;font-size:20px;font-weight:950}header span{color:#697792;font-size:12px;font-weight:700;line-height:1.5}header em{padding:5px 9px;border-radius:999px;background:#edf8f2;color:#2e8b62;font-size:11px;font-style:normal;font-weight:900}
.addressing-error{padding:8px 10px;border-radius:10px;background:#fff1f2;color:#b74652;font-size:12px;font-weight:800}
.naming-level{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:10px}.naming-level>span{color:#60708c;font-size:12px;font-weight:850}.naming-level>div{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:5px;padding:3px;border:1px solid #e3e8f3;border-radius:10px;background:#f8faff}.naming-level button{min-height:32px;border:1px solid transparent;border-radius:7px;background:#fff;color:#405171;font-size:11.5px;font-weight:900;cursor:pointer}.naming-level button.active{border-color:#6477eb;background:#6074e9;color:#fff;box-shadow:0 5px 12px rgba(75,94,221,.2)}
label{display:grid;gap:6px}label>span{display:flex;align-items:center;gap:8px}label b{color:#20355d;font-size:13px}label small{color:#8793a8;font-size:10px}input{width:100%;height:36px;box-sizing:border-box;padding:0 11px;border:1px solid #dfe5f0;border-radius:9px;background:#fafbfe;color:#25395e;font:inherit;font-size:12px;outline:none}input:focus{border-color:#8797ef;box-shadow:0 0 0 3px rgba(99,119,232,.08)}
.term-input-wrap{position:relative;min-width:0}
.term-input-wrap input{padding-right:48px}
.term-picker-trigger{position:absolute;right:7px;top:50%;display:flex;align-items:center;justify-content:center;gap:3px;width:32px;height:28px;padding:0;transform:translateY(-50%);border:1px solid #dce3f0;border-radius:8px;background:#fff;color:#687792;cursor:pointer;box-shadow:0 3px 8px rgba(52,72,118,.06)}
.term-picker-trigger:hover{border-color:#9aa9ef;background:#f4f6ff}
.term-picker-trigger span{display:block;width:4px;height:4px;border-radius:50%;background:currentColor}
.term-picker-trigger:disabled{opacity:.45;cursor:not-allowed}
.term-picker-backdrop{position:fixed;inset:0;z-index:9999;display:grid;place-items:center;padding:24px;background:rgba(25,38,66,.28);backdrop-filter:blur(3px)}
.term-picker-dialog{width:min(680px,calc(100vw - 40px));max-height:min(720px,calc(100vh - 48px));overflow:auto;padding:22px;border:1px solid #dce4f2;border-radius:22px;background:#fff;box-shadow:0 28px 80px rgba(27,42,77,.24)}
.term-picker-header{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:14px;align-items:start}
.term-picker-header>div{display:grid;gap:5px}
.term-picker-header strong{color:#172b53;font-size:22px;font-weight:950}
.term-picker-header span{color:#75829a;font-size:12px;font-weight:700;line-height:1.5}
.term-picker-close{width:34px;height:34px;border:1px solid #e0e5ee;border-radius:10px;background:#f8f9fc;color:#63708a;font-size:24px;line-height:1;cursor:pointer}
.term-picker-meta{display:flex;align-items:center;justify-content:space-between;gap:10px;margin-top:16px;padding:9px 11px;border-radius:10px;background:#f7f9fd;color:#60708b;font-size:11px;font-weight:800}
.term-picker-meta em{color:#7c67d9;font-style:normal}
.term-picker-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:9px;margin-top:14px}
.term-picker-option{display:grid;grid-template-columns:22px minmax(0,1fr);align-items:center;gap:8px;min-height:42px;padding:7px 10px;border:1px solid #e0e5ef;border-radius:11px;background:#fff;color:#33496f;text-align:left;font-size:12px;font-weight:850;cursor:pointer;transition:.18s ease}
.term-picker-option:hover{border-color:#aab7ef;background:#f8f9ff;transform:translateY(-1px)}
.term-picker-option i{display:grid;place-items:center;width:20px;height:20px;border:1.5px solid #cfd7e6;border-radius:6px;background:#fff;color:#fff;font-size:12px;font-style:normal}
.term-picker-option.selected{border-color:#7b8cf0;background:#f2f4ff;color:#253e78;box-shadow:0 6px 14px rgba(85,103,220,.1)}
.term-picker-option.selected i{border-color:#6377ea;background:#6377ea}
.term-picker-error{margin-top:12px;padding:8px 10px;border-radius:9px;background:#fff0f1;color:#b84750;font-size:12px;font-weight:800}
.term-picker-actions{display:flex;justify-content:flex-end;gap:9px;margin-top:18px}
.term-picker-actions button{min-width:86px;height:38px;border-radius:10px;font-size:12px;font-weight:900;cursor:pointer}
.picker-cancel{border:1px solid #dce2ed;background:#fff;color:#60708c}
.picker-confirm{border:1px solid #5d71e8;background:#6075eb;color:#fff;box-shadow:0 7px 16px rgba(77,96,222,.2)}
.addressing-main-footer{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:10px}.addressing-main-footer span{color:#7b879c;font-size:10.5px;line-height:1.45}.addressing-main-footer button{min-height:34px;padding:0 12px;border:1px solid #586de5;border-radius:9px;background:#5c71ea;color:#fff;font-size:11px;font-weight:900;cursor:pointer}
button:disabled,input:disabled{opacity:.55;cursor:not-allowed}.compact{gap:8px;padding:12px;border-radius:14px}.compact header strong{font-size:16px}.compact .addressing-main-footer{grid-template-columns:1fr}.compact .addressing-main-footer button{justify-self:end}.mobile{box-shadow:none}
@media(max-width:760px){header,.naming-level,.addressing-main-footer{grid-template-columns:1fr}.compact .addressing-main-footer button,.addressing-main-footer button{width:100%}.term-picker-dialog{width:calc(100vw - 24px);padding:16px;border-radius:16px}.term-picker-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.term-picker-meta{align-items:flex-start;flex-direction:column}}
</style>
