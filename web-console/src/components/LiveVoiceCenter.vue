<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  activateLiveAgentConfigVersion,
  cloneLiveVoiceModelBinding,
  cloneLiveVoiceProfile,
  createLiveAgentConfigDraft,
  deleteLiveVoiceProfile,
  getLiveAgentConfigVersions,
  getLiveOfficialVoices,
  getLiveVoiceModelBindings,
  getLiveVoiceProfileQuota,
  getLiveVoiceProfiles,
  previewLiveOfficialVoice,
  previewLiveVoiceModelBinding,
  publishLiveAgentVoiceBinding,
  uploadLiveMediaAsset,
} from '../api'
import type { LiveAgentConfigVersion, OfficialVoice, VoiceModelBinding, VoiceProfile } from '../types'
import { liveSupportMediaPlaybackURL } from '../liveSupportAccess'

const props = defineProps<{ roomId: number | null; roomName?: string; supportSession?: boolean }>()
const emit = defineEmits<{ changed: [] }>()

const VOICE_PREVIEW_TEXT = '你好，我是你的主播，欢迎为你效劳。'
const DEFAULT_CLONE_MODEL = 'qwen-audio-3.0-tts-plus'
const cloneModels = [
  { id: 'qwen-audio-3.0-tts-plus', label: '通用模型（推荐）' },
  { id: 'qwen-audio-3.1-tts-flash', label: '极速模型' },
]
const avatarOptions = [
  { key: 'female_young', label: '女年轻', emoji: '👩🏻', tone: 'rose' },
  { key: 'female_middle', label: '女中年', emoji: '👩', tone: 'violet' },
  { key: 'female_senior', label: '女老年', emoji: '👵🏻', tone: 'amber' },
  { key: 'male_young', label: '男青年', emoji: '👨🏻', tone: 'blue' },
  { key: 'male_middle', label: '男中年', emoji: '👨', tone: 'indigo' },
  { key: 'male_senior', label: '男老年', emoji: '👴🏻', tone: 'slate' },
  { key: 'cartoon_female', label: '可爱卡通女', emoji: '👧🏻', tone: 'pink' },
  { key: 'cartoon_male', label: '可爱卡通男', emoji: '👦🏻', tone: 'cyan' },
] as const

const activeLibrary = ref<'official' | 'custom'>('official')
const officialVoices = ref<OfficialVoice[]>([])
const voiceProfiles = ref<VoiceProfile[]>([])
const voiceBindings = ref<VoiceModelBinding[]>([])
const versions = ref<LiveAgentConfigVersion[]>([])
const quota = ref({ limit: 0, used: 0, remaining: 0 })
const selectedModels = ref<Record<number, string>>({})
const loading = ref(false)
const selecting = ref(false)
const bindingBusyKey = ref('')
const deletingProfileId = ref(0)
const error = ref('')
const notice = ref('')
const previewingKey = ref('')
let previewAudio: HTMLAudioElement | null = null

const cloneOpen = ref(false)
const cloneName = ref('')
const cloneModel = ref(DEFAULT_CLONE_MODEL)
const selectedAvatarKey = ref<(typeof avatarOptions)[number]['key']>('female_young')
const sampleFile = ref<File | null>(null)
const sampleDuration = ref(0)
const sampleError = ref('')
const sampleInput = ref<HTMLInputElement | null>(null)
const cloneStage = ref<'idle' | 'uploading' | 'processing'>('idle')

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return value as Record<string, unknown>
}

const activeVersion = computed(
  () => versions.value.find((item) => item.lifecycle_status === 'active') || versions.value[0] || null,
)

const selectedVoice = computed(() => {
  const speech = asRecord(activeVersion.value?.speech_config)
  const rooms = asRecord(speech.rooms)
  const room = asRecord(rooms[String(props.roomId || 0)])
  return asRecord(room.selected_voice)
})

const currentVoiceName = computed(() => String(selectedVoice.value.name || '').trim() || '龙安灵心')
const selectedProfileId = computed(() => Number(selectedVoice.value.profile_id || 0))
const canSubmitClone = computed(
  () => Boolean(cloneName.value.trim() && sampleFile.value && !sampleError.value && sampleDuration.value >= 5 && sampleDuration.value <= 10 && !selecting.value),
)

async function load() {
  if (!props.roomId) {
    officialVoices.value = []
    voiceProfiles.value = []
    voiceBindings.value = []
    versions.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const [official, profiles, bindings, configVersions, profileQuota] = await Promise.all([
      getLiveOfficialVoices(),
      getLiveVoiceProfiles(),
      getLiveVoiceModelBindings(),
      getLiveAgentConfigVersions(),
      getLiveVoiceProfileQuota(),
    ])
    officialVoices.value = official.items || []
    voiceProfiles.value = (profiles || []).filter((item) => item.clone_status !== 'disabled')
    voiceBindings.value = (bindings || []).filter((item) => item.status !== 'disabled')
    versions.value = configVersions || []
    quota.value = profileQuota
    for (const profile of voiceProfiles.value) {
      if (!selectedModels.value[profile.id]) selectedModels.value[profile.id] = profileTargetModel(profile)
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取声音中心失败'
  } finally {
    loading.value = false
  }
}

function profileTargetModel(profile: VoiceProfile) {
  if (selectedProfileId.value === profile.id && selectedVoice.value.model) return String(selectedVoice.value.model)
  const configured = String(asRecord(profile.config).target_model || '').trim()
  return cloneModels.some((item) => item.id === configured) ? configured : DEFAULT_CLONE_MODEL
}

function stopPreview() {
  if (previewAudio) {
    previewAudio.pause()
    previewAudio.src = ''
    previewAudio = null
  }
  previewingKey.value = ''
}

async function playURL(key: string, factory: () => Promise<{ audio_url: string }>) {
  if (previewingKey.value === key) {
    stopPreview()
    return
  }
  stopPreview()
  previewingKey.value = key
  error.value = ''
  notice.value = ''
  try {
    const result = await factory()
    const audio = new Audio(liveSupportMediaPlaybackURL(result.audio_url))
    previewAudio = audio
    audio.addEventListener('ended', stopPreview, { once: true })
    audio.addEventListener('error', stopPreview, { once: true })
    await audio.play()
  } catch (err) {
    stopPreview()
    error.value = err instanceof Error ? err.message : '声音试听失败'
  }
}

function updatedSpeechConfig(voice: Record<string, unknown>) {
  const speech = { ...asRecord(activeVersion.value?.speech_config) }
  const rooms = { ...asRecord(speech.rooms) }
  const key = String(props.roomId || 0)
  const room = { ...asRecord(rooms[key]) }
  room.selected_voice = voice
  rooms[key] = room
  speech.rooms = rooms
  return speech
}

async function bindVoice(voice: Record<string, unknown>): Promise<boolean> {
  if (!props.roomId || selecting.value) return false
  selecting.value = true
  error.value = ''
  notice.value = ''
  try {
    const draft = await createLiveAgentConfigDraft({
      speech_config: updatedSpeechConfig({ ...voice, selected_at: new Date().toISOString() }),
    })
    await activateLiveAgentConfigVersion(draft.id)
    await load()
    notice.value = `“${String(voice.name || '声音')}”已用于直播间「${props.roomName || '当前直播间'}」`
    emit('changed')
    return true
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存直播间声音失败'
    return false
  } finally {
    selecting.value = false
  }
}

async function chooseOfficial(item: OfficialVoice) {
  await bindVoice({ source: 'official', provider: 'aliyun_qwen', name: item.name, voice_id: item.id, model: item.model })
}

function bindingsForProfile(profileId: number) {
  return voiceBindings.value.filter((item) => item.profile_id === profileId)
}

function latestBinding(profileId: number, model: string) {
  return bindingsForProfile(profileId).find(
    (item) => item.model === model && item.status === 'ready' && Boolean(item.voice_id),
  ) || null
}

async function ensureBinding(profile: VoiceProfile, model: string): Promise<VoiceModelBinding | null> {
  const existing = latestBinding(profile.id, model)
  if (existing) return existing
  const key = profile.id + ':' + model
  if (!profile.sample_asset_id || bindingBusyKey.value) return null
  bindingBusyKey.value = key
  error.value = ''
  try {
    const binding = await cloneLiveVoiceModelBinding(profile.id, { model })
    voiceBindings.value = [binding, ...voiceBindings.value.filter((item) => item.id !== binding.id)]
    return binding
  } catch (err) {
    error.value = err instanceof Error ? err.message : '生成模型音色失败'
    return null
  } finally {
    bindingBusyKey.value = ''
  }
}

async function previewCustom(profile: VoiceProfile) {
  const model = selectedModels.value[profile.id] || profileTargetModel(profile)
  const binding = await ensureBinding(profile, model)
  if (!binding) return
  await playURL('binding-' + binding.id, () => previewLiveVoiceModelBinding(binding.id, VOICE_PREVIEW_TEXT))
}

async function useCustom(profile: VoiceProfile) {
  const model = selectedModels.value[profile.id] || profileTargetModel(profile)
  const binding = await ensureBinding(profile, model)
  if (!binding || binding.status !== 'ready' || !props.roomId) return
  const saved = await bindVoice({
    source: 'clone', provider: binding.provider, name: profile.name, voice_id: binding.voice_id,
    profile_id: profile.id, binding_id: binding.id, model: binding.model, rate: binding.rate || 1,
  })
  if (!saved) return
  selecting.value = true
  try {
    await publishLiveAgentVoiceBinding(props.roomId, binding.id)
    await load()
    emit('changed')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '声音已保存，但发布到直播间失败'
  } finally {
    selecting.value = false
  }
}

function openCloneDialog() {
  error.value = ''
  notice.value = ''
  activeLibrary.value = 'custom'
  if (quota.value.limit > 0 && quota.value.used >= quota.value.limit) {
    error.value = `自定义声音已达到直播间权限上限（${quota.value.limit} 个），${props.supportSession ? '请联系客户释放声音名额。' : '请先删除一个已有声音再生成。'}`
    return
  }
  cloneOpen.value = true
}

function resetCloneForm() {
  cloneName.value = ''
  cloneModel.value = DEFAULT_CLONE_MODEL
  selectedAvatarKey.value = 'female_young'
  sampleFile.value = null
  sampleDuration.value = 0
  sampleError.value = ''
  cloneStage.value = 'idle'
  if (sampleInput.value) sampleInput.value.value = ''
}

function closeCloneDialog() {
  if (selecting.value) return
  cloneOpen.value = false
  resetCloneForm()
}

async function readAudioDuration(file: File) {
  return await new Promise<number>((resolve, reject) => {
    const url = URL.createObjectURL(file)
    const audio = new Audio()
    const cleanup = () => URL.revokeObjectURL(url)
    audio.preload = 'metadata'
    audio.addEventListener('loadedmetadata', () => {
      const duration = Number(audio.duration)
      cleanup()
      if (!Number.isFinite(duration) || duration <= 0) reject(new Error('无法识别录音时长'))
      else resolve(duration)
    }, { once: true })
    audio.addEventListener('error', () => {
      cleanup()
      reject(new Error('无法读取这个声音文件'))
    }, { once: true })
    audio.src = url
  })
}

async function chooseSample(event: Event) {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0] || null
  sampleFile.value = null
  sampleDuration.value = 0
  sampleError.value = ''
  if (!file) return
  try {
    const duration = await readAudioDuration(file)
    sampleDuration.value = duration
    if (duration < 5 || duration > 10) {
      sampleError.value = `录音必须为 5–10 秒，当前约 ${duration.toFixed(1)} 秒，请重新选择。`
      target.value = ''
      return
    }
    sampleFile.value = file
  } catch (err) {
    sampleError.value = err instanceof Error ? err.message : '无法读取声音文件'
    target.value = ''
  }
}

async function submitClone() {
  if (!canSubmitClone.value || !sampleFile.value) return
  selecting.value = true
  error.value = ''
  notice.value = ''
  try {
    cloneStage.value = 'uploading'
    const uploaded = await uploadLiveMediaAsset(sampleFile.value, 'voice_sample', {
      durationMs: Math.round(sampleDuration.value * 1000),
      metadata: {
        source: 'voice_center_clone', avatar_key: selectedAvatarKey.value,
        expected_duration_seconds: Number(sampleDuration.value.toFixed(2)),
      },
    })
    cloneStage.value = 'processing'
    await cloneLiveVoiceProfile({
      name: cloneName.value.trim(), sample_asset_id: uploaded.asset.id,
      model: cloneModel.value, avatar_key: selectedAvatarKey.value,
    })
    cloneOpen.value = false
    resetCloneForm()
    await load()
    notice.value = '声音已转为规范格式并生成成功，现在可以试听或用于当前直播间。'
  } catch (err) {
    error.value = err instanceof Error ? err.message : '声音生成失败'
    cloneStage.value = 'idle'
  } finally {
    selecting.value = false
  }
}

async function removeCustomVoice(profile: VoiceProfile) {
  if (props.supportSession || deletingProfileId.value || selecting.value) return
  if (!window.confirm(`确认删除自定义声音“${profile.name}”吗？删除后才能重新占用这个声音名额。`)) return
  deletingProfileId.value = profile.id
  error.value = ''
  notice.value = ''
  const wasSelected = selectedProfileId.value === profile.id
  try {
    await deleteLiveVoiceProfile(profile.id)
    await load()
    if (wasSelected && officialVoices.value[0]) await chooseOfficial(officialVoices.value[0])
    notice.value = `已删除“${profile.name}”，声音名额已释放。`
  } catch (err) {
    error.value = err instanceof Error ? err.message : '删除自定义声音失败'
  } finally {
    deletingProfileId.value = 0
  }
}

function avatarForProfile(profile: VoiceProfile) {
  const key = String(asRecord(profile.config).avatar_key || 'female_young')
  return avatarOptions.find((item) => item.key === key) || avatarOptions[0]
}

function officialAvatar(item: OfficialVoice, index: number) {
  const candidates = item.gender.includes('女')
    ? [avatarOptions[0], avatarOptions[6], avatarOptions[1]]
    : [avatarOptions[3], avatarOptions[7], avatarOptions[4]]
  return candidates[index % candidates.length]
}

function officialTag(item: OfficialVoice) { return item.tags?.[0] || item.gender || '官方声音' }
function isOfficialSelected(item: OfficialVoice) {
  return selectedVoice.value.source === 'official' && String(selectedVoice.value.voice_id || '') === item.id
}
function isCustomSelected(profile: VoiceProfile) {
  return selectedVoice.value.source === 'clone' && selectedProfileId.value === profile.id
}
function createdLabel(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', {
    hour12: false, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  })
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
onBeforeUnmount(stopPreview)
</script>

<template>
  <section class="voice-center">
    <div class="voice-room-strip">
      <div><span class="section-kicker">VOICE MANAGEMENT</span><strong>样本声音 · 自定义音色</strong></div>
      <div class="room-voice-state">
        <span>指定直播间</span><b>{{ roomName || '请选择直播间' }}</b><i></i>
        <span>当前声音</span><b>{{ currentVoiceName }}</b>
      </div>
    </div>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-if="notice" class="inline-notice">{{ notice }}</p>

    <div class="voice-workspace">
      <aside class="voice-side-card">
        <div><span class="side-eyebrow">声音管理</span><h3>样本声音 · 自定义音色</h3><p>选择喜欢的声音，让你的内容更有个性</p></div>
        <nav class="voice-library-nav" aria-label="声音库">
          <button type="button" :class="{ active: activeLibrary === 'official' }" @click="activeLibrary = 'official'">
            <span class="nav-icon official-icon">★</span>
            <span><b>官方声音</b><small>精选官方优质音色</small></span><em>›</em>
          </button>
          <button type="button" :class="{ active: activeLibrary === 'custom' }" @click="activeLibrary = 'custom'">
            <span class="nav-icon custom-icon">●</span>
            <span><b>我的声音</b><small>已创建的专属音色</small></span><em>›</em>
          </button>
        </nav>
        <div class="voice-side-art" aria-hidden="true"><span class="chat-bubble">•••</span><span class="microphone">♩</span><span class="sound-wave">▂▄▆▄▂</span></div>
      </aside>

      <section class="voice-list-panel">
        <header class="voice-list-head">
          <div>
            <div class="voice-title-line"><h3>{{ activeLibrary === 'official' ? '官方声音' : '我的声音' }}</h3><span v-if="activeLibrary === 'custom'" class="quota-chip">{{ quota.used }} / {{ quota.limit }} 个</span></div>
            <p v-if="activeLibrary === 'official'">精选多种风格的官方音色，适用于不同直播场景</p>
            <p v-else>使用你创建的专属音色，选择不同模型体验不同效果</p>
          </div>
          <button v-if="activeLibrary === 'custom'" type="button" class="upload-sample-button" @click="openCloneDialog"><span>＋</span> 上传声音样本</button>
        </header>

        <div v-if="loading" class="voice-loading">正在读取声音…</div>
        <div v-else-if="activeLibrary === 'official'" class="voice-rows">
          <article v-for="(item, index) in officialVoices" :key="item.id" class="voice-row" :class="{ selected: isOfficialSelected(item) }">
            <div class="voice-avatar" :class="'tone-' + officialAvatar(item, index).tone"><span>{{ officialAvatar(item, index).emoji }}</span><i class="avatar-wave">▂▅▃▆▂</i></div>
            <div class="voice-row-copy"><div><strong>{{ item.name }}</strong><span>{{ officialTag(item) }}</span></div><p>{{ item.description }}，适合直播讲解、互动和内容播报</p></div>
            <div class="voice-row-actions">
              <button type="button" class="preview-button" @click="playURL('official-' + item.id, () => previewLiveOfficialVoice(item.id, VOICE_PREVIEW_TEXT))"><span>{{ previewingKey === 'official-' + item.id ? 'Ⅱ' : '▶' }}</span>{{ previewingKey === 'official-' + item.id ? '正在试听' : '试听' }}</button>
              <button type="button" class="use-voice-button" :class="{ selected: isOfficialSelected(item) }" :disabled="selecting" @click="chooseOfficial(item)">{{ isOfficialSelected(item) ? '当前使用' : '使用这个声音' }}</button>
            </div>
          </article>
        </div>

        <div v-else-if="voiceProfiles.length" class="voice-rows custom-voice-rows">
          <article v-for="profile in voiceProfiles" :key="profile.id" class="voice-row custom-voice-row" :class="{ selected: isCustomSelected(profile) }">
            <div class="voice-avatar custom-avatar" :class="'tone-' + avatarForProfile(profile).tone"><span>{{ avatarForProfile(profile).emoji }}</span><i class="avatar-edit">✓</i></div>
            <div class="voice-row-copy custom-copy"><div><strong>{{ profile.name }}</strong><span v-if="isCustomSelected(profile)">当前使用</span></div><p>创建于 {{ createdLabel(profile.created_at) }}</p></div>
            <label class="model-picker"><span>选择模型</span><select v-model="selectedModels[profile.id]"><option v-for="model in cloneModels" :key="model.id" :value="model.id">{{ model.label }}</option></select></label>
            <div class="voice-row-actions custom-actions">
              <button type="button" class="preview-button" :disabled="Boolean(bindingBusyKey)" @click="previewCustom(profile)"><span>▶</span>{{ bindingBusyKey.startsWith(profile.id + ':') ? '生成中…' : '试听' }}</button>
              <button type="button" class="use-voice-button" :class="{ selected: isCustomSelected(profile) }" :disabled="selecting || Boolean(bindingBusyKey)" @click="useCustom(profile)">{{ isCustomSelected(profile) ? '当前使用' : '使用这个声音' }}</button>
              <button v-if="!props.supportSession" type="button" class="delete-voice-button" :disabled="deletingProfileId === profile.id" title="删除自定义声音" @click="removeCustomVoice(profile)">{{ deletingProfileId === profile.id ? '…' : '删除' }}</button>
            </div>
          </article>
        </div>

        <div v-else-if="activeLibrary === 'custom'" class="voice-empty"><span>♫</span><strong>还没有自定义声音</strong><p>上传一段 5–10 秒的清晰录音，创建你的专属音色。</p><button type="button" @click="openCloneDialog">＋ 上传声音样本</button></div>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="cloneOpen" class="voice-dialog-overlay" @click.self="closeCloneDialog">
        <section class="voice-dialog" role="dialog" aria-modal="true" aria-label="上传声音样本">
          <header><div><span class="section-kicker">CUSTOM VOICE</span><h3>上传声音样本</h3><p>选择头像、填写名称，再上传 5–10 秒的单人清晰录音。</p></div><button type="button" class="voice-close" :disabled="selecting" @click="closeCloneDialog">×</button></header>
          <div class="dialog-quota"><span>自定义声音名额</span><b>已用 {{ quota.used }} 个，共 {{ quota.limit }} 个</b></div>
          <fieldset class="avatar-picker">
            <legend>选择头像</legend>
            <button v-for="avatar in avatarOptions" :key="avatar.key" type="button" :class="['avatar-option', 'tone-' + avatar.tone, { active: selectedAvatarKey === avatar.key }]" @click="selectedAvatarKey = avatar.key"><span>{{ avatar.emoji }}</span><small>{{ avatar.label }}</small></button>
          </fieldset>
          <label class="voice-clone-field"><span>声音名字</span><input v-model="cloneName" maxlength="60" placeholder="例如：小悠、门店主播" /></label>
          <label class="voice-clone-upload" :class="{ invalid: sampleError }">
            <input ref="sampleInput" type="file" accept="audio/*" @change="chooseSample" /><span class="upload-icon">↑</span>
            <strong>{{ sampleFile ? sampleFile.name : '选择 5–10 秒声音文件' }}</strong>
            <small v-if="sampleFile">时长 {{ sampleDuration.toFixed(1) }} 秒 · 将自动转为标准 WAV 格式</small><small v-else>支持常见音频格式，要求单人、无音乐、无噪声</small>
          </label>
          <p v-if="sampleError" class="sample-error">{{ sampleError }}</p>
          <footer class="voice-clone-actions">
            <span v-if="cloneStage === 'uploading'">正在上传声音样本…</span><span v-else-if="cloneStage === 'processing'">正在转为规范格式并生成声音 ID…</span><span v-else>生成成功后会自动显示在“我的声音”中</span>
            <div><button type="button" :disabled="selecting" @click="closeCloneDialog">取消</button><button type="button" class="primary-button" :disabled="!canSubmitClone" @click="submitClone">{{ selecting ? '处理中…' : '确定并生成' }}</button></div>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.voice-center{display:grid;gap:14px;padding:2px 0 28px;color:#17233a}.voice-room-strip{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:16px 20px;border:1px solid #e2e8f5;border-radius:18px;background:linear-gradient(120deg,#fff,#f6f9ff);box-shadow:0 12px 30px rgba(76,101,168,.06)}.voice-room-strip>div:first-child{display:grid;gap:2px}.section-kicker{font-size:11px;font-weight:900;letter-spacing:.16em;color:#8391aa}.voice-room-strip strong{font-size:20px}.room-voice-state{display:flex;align-items:center;gap:8px;color:#8490a5;font-size:12px}.room-voice-state b{color:#253654;font-size:13px}.room-voice-state i{width:1px;height:22px;margin:0 8px;background:#dde5f2}.inline-error,.inline-notice{margin:0;padding:11px 14px;border-radius:12px;font-weight:700}.inline-error{color:#b92e45;background:#fff0f2;border:1px solid #ffcfd7}.inline-notice{color:#087a5a;background:#edfff8;border:1px solid #bdeedd}
.voice-workspace{display:grid;grid-template-columns:minmax(270px,310px) minmax(0,1fr);gap:14px;min-height:590px;padding:12px;border:1px solid #dfe7f6;border-radius:26px;background:linear-gradient(145deg,#f7f9ff,#edf3ff);box-shadow:0 24px 60px rgba(41,76,150,.08)}.voice-side-card,.voice-list-panel{position:relative;overflow:hidden;border:1px solid rgba(222,230,245,.9);border-radius:22px;background:rgba(255,255,255,.82)}.voice-side-card{display:flex;flex-direction:column;padding:28px 22px 22px;background:linear-gradient(160deg,#fff 0%,#f7f9ff 64%,#eaf0ff 100%)}.side-eyebrow{display:block;margin-bottom:7px;color:#8190a9;font-size:12px;font-weight:800}.voice-side-card h3{margin:0;font-size:25px;line-height:1.25}.voice-side-card>div>p{margin:10px 0 0;color:#75849d;font-size:14px;line-height:1.6}.voice-library-nav{display:grid;gap:12px;margin-top:34px}.voice-library-nav button{display:grid;grid-template-columns:52px 1fr auto;align-items:center;gap:13px;min-height:84px;padding:12px 14px;border:1px solid #e1e7f3;border-radius:17px;background:rgba(255,255,255,.86);color:#243450;text-align:left;cursor:pointer;transition:.22s ease}.voice-library-nav button:hover{transform:translateY(-2px);box-shadow:0 12px 26px rgba(62,91,165,.12)}.voice-library-nav button.active{border-color:#6f82ff;background:linear-gradient(135deg,#7194ff,#5764ed);color:#fff;box-shadow:0 16px 30px rgba(76,91,235,.24)}.voice-library-nav button>span:nth-child(2){display:grid;gap:3px}.voice-library-nav b{font-size:18px}.voice-library-nav small{color:#8a96aa}.voice-library-nav button.active small{color:#e6ebff}.voice-library-nav em{font-style:normal;font-size:32px;font-weight:300}.nav-icon{width:50px;height:50px;display:grid;place-items:center;border-radius:15px;background:linear-gradient(145deg,#edf1ff,#dfe6ff);color:#5368ed;font-size:22px;box-shadow:inset 0 1px #fff}.voice-library-nav button.active .nav-icon{background:linear-gradient(145deg,#3459f4,#2440cf);color:#fff}.voice-side-art{position:absolute;left:0;right:0;bottom:0;height:180px;pointer-events:none;background:radial-gradient(circle at 74% 58%,rgba(86,114,239,.16),transparent 35%),linear-gradient(160deg,transparent 38%,rgba(207,218,248,.45) 39%,rgba(183,198,239,.36) 65%,transparent 66%)}.chat-bubble{position:absolute;left:48%;top:42px;padding:16px 20px;border-radius:15px;background:linear-gradient(145deg,#e3eaff,#becdf9);color:#fff;letter-spacing:4px;font-weight:900}.microphone{position:absolute;right:42px;top:64px;width:54px;height:72px;display:grid;place-items:center;border-radius:30px;background:linear-gradient(#7aa0ff,#334ee4);color:#fff;font-size:34px;box-shadow:0 12px 30px rgba(57,82,222,.25)}.sound-wave{position:absolute;right:15px;top:95px;color:#8da4f3;letter-spacing:2px}
.voice-list-panel{padding:26px;background:rgba(255,255,255,.88)}.voice-list-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:22px}.voice-list-head h3{margin:0;font-size:25px}.voice-list-head p{margin:6px 0 0;color:#75839b;font-size:14px}.voice-title-line{display:flex;align-items:center;gap:10px}.quota-chip{padding:4px 9px;border-radius:999px;background:#eef0ff;color:#5e61df;font-size:12px;font-weight:800}.upload-sample-button{min-height:43px;padding:0 17px;border:1px solid #d9e1f0;border-radius:13px;background:#fff;color:#30415f;font-weight:900;cursor:pointer;box-shadow:0 8px 18px rgba(48,74,132,.06);transition:.2s}.upload-sample-button:hover{transform:translateY(-2px);border-color:#8ea0ff;box-shadow:0 12px 24px rgba(79,94,226,.18)}.upload-sample-button span{font-size:21px;margin-right:5px}.voice-loading{padding:50px;text-align:center;color:#8090a7}.voice-rows{display:grid;gap:10px}.voice-row{display:grid;grid-template-columns:76px minmax(180px,1fr) auto;align-items:center;gap:16px;min-height:112px;padding:14px 16px;border:1px solid #e0e7f3;border-radius:18px;background:rgba(255,255,255,.92);transition:.2s}.voice-row:hover{transform:translateY(-1px);border-color:#bdc9f0;box-shadow:0 12px 28px rgba(48,74,132,.08)}.voice-row.selected{border-color:#8799ff;box-shadow:0 0 0 2px rgba(104,124,247,.08),0 12px 28px rgba(68,89,203,.11)}
.voice-avatar{position:relative;width:70px;height:70px;display:grid;place-items:center;border-radius:50%;box-shadow:inset 0 0 0 1px rgba(255,255,255,.75),0 8px 18px rgba(53,81,150,.14)}.voice-avatar>span{font-size:43px;filter:drop-shadow(0 3px 4px rgba(21,43,88,.12))}.avatar-wave{position:absolute;left:4px;right:4px;bottom:-2px;padding:1px 4px;border-radius:8px;background:#fff;color:#5070e9;font-size:9px;font-style:normal;letter-spacing:0;text-align:center}.tone-rose{background:linear-gradient(145deg,#fff1f6,#f6c8dc)}.tone-violet{background:linear-gradient(145deg,#f2edff,#c9b7f5)}.tone-amber{background:linear-gradient(145deg,#fff8dd,#f4d494)}.tone-blue{background:linear-gradient(145deg,#e9f5ff,#a9d0fa)}.tone-indigo{background:linear-gradient(145deg,#ecf0ff,#b1bdf8)}.tone-slate{background:linear-gradient(145deg,#eef3f7,#c8d2dc)}.tone-pink{background:linear-gradient(145deg,#fff0fa,#f3b8e0)}.tone-cyan{background:linear-gradient(145deg,#e8fbff,#9cddec)}.voice-row-copy{min-width:0}.voice-row-copy>div{display:flex;align-items:center;gap:9px}.voice-row-copy strong{font-size:20px}.voice-row-copy>div span{padding:4px 9px;border-radius:999px;background:#eef1ff;color:#6677ea;font-size:11px;font-weight:800}.voice-row-copy p{margin:7px 0 0;color:#718099;font-size:13px;line-height:1.5}.voice-row-actions{display:flex;align-items:center;gap:10px}.voice-row-actions button{min-height:45px;padding:0 18px;border:1px solid #dce4f2;border-radius:13px;background:#fff;color:#2f405f;font-weight:900;cursor:pointer;white-space:nowrap;transition:.2s}.voice-row-actions button:hover:not(:disabled){transform:translateY(-2px);box-shadow:0 10px 22px rgba(66,88,173,.16)}.voice-row-actions button:disabled{opacity:.58;cursor:not-allowed}.preview-button span{margin-right:7px;color:#4f6cf3}.use-voice-button{border-color:#5368ef!important;background:linear-gradient(135deg,#5778ff,#4e5ee5)!important;color:#fff!important;box-shadow:0 10px 20px rgba(70,84,219,.18)}.use-voice-button.selected{background:linear-gradient(135deg,#38b989,#21a974)!important;border-color:#21a974!important}
.custom-voice-row{grid-template-columns:76px minmax(150px,1fr) minmax(190px,230px) auto}.custom-avatar .avatar-edit{position:absolute;right:-2px;bottom:0;width:21px;height:21px;display:grid;place-items:center;border:3px solid #fff;border-radius:50%;background:#6178ef;color:#fff;font-size:10px;font-style:normal}.model-picker{display:grid;gap:5px}.model-picker>span{color:#8090a8;font-size:11px}.model-picker select{width:100%;min-height:43px;padding:0 36px 0 12px;border:1px solid #dbe3f1;border-radius:12px;background:#fff;color:#2d3e5d;font-weight:700}.delete-voice-button{min-width:56px!important;padding:0 10px!important;color:#b54a5b!important}.delete-voice-button:hover:not(:disabled){border-color:#f1b7c0!important;background:#fff3f5!important;box-shadow:none!important}.voice-empty{min-height:330px;display:grid;place-items:center;align-content:center;gap:8px;border:1px dashed #cbd6eb;border-radius:20px;background:linear-gradient(145deg,#fff,#f5f8ff);text-align:center;color:#77869e}.voice-empty>span{width:66px;height:66px;display:grid;place-items:center;border-radius:20px;background:#edf1ff;color:#5368ea;font-size:32px}.voice-empty strong{color:#253551;font-size:20px}.voice-empty p{margin:0}.voice-empty button{margin-top:8px;min-height:42px;padding:0 16px;border:0;border-radius:12px;background:#5669eb;color:#fff;font-weight:900;cursor:pointer}
.voice-dialog-overlay{position:fixed;inset:0;z-index:4400;display:grid;place-items:center;padding:22px;background:rgba(18,31,60,.52);backdrop-filter:blur(5px)}.voice-dialog{width:min(720px,95vw);max-height:92vh;overflow:auto;padding:24px;border:1px solid rgba(255,255,255,.8);border-radius:24px;background:linear-gradient(145deg,#fff,#f5f8ff);box-shadow:0 34px 90px rgba(16,31,72,.34)}.voice-dialog>header{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.voice-dialog h3{margin:4px 0 0;font-size:27px}.voice-dialog header p{margin:6px 0 0;color:#75849b}.voice-close{border:0;background:transparent;color:#7b889e;font-size:31px;cursor:pointer}.dialog-quota{display:flex;justify-content:space-between;gap:14px;margin-top:18px;padding:11px 14px;border-radius:12px;background:#edf2ff;color:#61718d;font-size:13px}.dialog-quota b{color:#445cdc}.avatar-picker{display:grid;grid-template-columns:repeat(4,1fr);gap:9px;margin:18px 0 0;padding:0;border:0}.avatar-picker legend{grid-column:1/-1;margin-bottom:8px;color:#33435f;font-weight:900}.avatar-option{display:grid;place-items:center;gap:3px;min-height:84px;padding:7px;border:2px solid transparent;border-radius:15px;cursor:pointer;transition:.18s}.avatar-option:hover{transform:translateY(-2px)}.avatar-option.active{border-color:#5b70ee;box-shadow:0 0 0 3px rgba(91,112,238,.11)}.avatar-option span{font-size:34px}.avatar-option small{color:#40506c;font-weight:800}.voice-clone-field{display:grid;gap:7px;margin-top:17px;color:#34445f;font-weight:900}.voice-clone-field input{min-height:45px;padding:9px 12px;border:1px solid #d2dbea;border-radius:12px;background:#fff;font:inherit}.voice-clone-upload{display:grid;place-items:center;gap:5px;margin-top:15px;padding:18px;border:1px dashed #aebce0;border-radius:16px;background:rgba(255,255,255,.86);cursor:pointer;text-align:center}.voice-clone-upload.invalid{border-color:#ef9cac;background:#fff6f7}.voice-clone-upload input{width:100%;font-size:12px}.voice-clone-upload .upload-icon{width:36px;height:36px;display:grid;place-items:center;border-radius:11px;background:#e9eeff;color:#536bed;font-size:22px;font-weight:900}.voice-clone-upload small{color:#7e8ca2}.sample-error{margin:7px 0 0;color:#bd3e52;font-size:13px;font-weight:700}.voice-clone-actions{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:20px;padding-top:16px;border-top:1px solid #e1e7f1}.voice-clone-actions>span{color:#75839a;font-size:12px}.voice-clone-actions>div{display:flex;gap:9px}.voice-clone-actions button{min-height:42px;padding:0 17px;border:1px solid #d7dfed;border-radius:11px;background:#fff;color:#34455f;font-weight:900;cursor:pointer}.primary-button{border-color:#5367ec!important;background:linear-gradient(135deg,#5978ff,#5360e5)!important;color:#fff!important}.voice-clone-actions button:disabled{opacity:.5;cursor:not-allowed}
@media (max-width:1180px){.voice-workspace{grid-template-columns:240px minmax(0,1fr)}.voice-side-card{padding:22px 16px}.custom-voice-row{grid-template-columns:66px 1fr 190px}.custom-actions{grid-column:2/-1;justify-content:flex-end}.voice-row-actions button{padding:0 13px}}@media (max-width:860px){.voice-room-strip{align-items:flex-start;flex-direction:column}.voice-workspace{grid-template-columns:1fr}.voice-side-card{min-height:auto}.voice-side-art{display:none}.voice-library-nav{grid-template-columns:1fr 1fr;margin-top:20px}.voice-list-panel{padding:18px}.voice-row,.custom-voice-row{grid-template-columns:64px 1fr}.voice-row-actions,.model-picker{grid-column:2}.voice-row-actions{justify-content:flex-start;flex-wrap:wrap}.voice-list-head{align-items:flex-start}.avatar-picker{grid-template-columns:repeat(2,1fr)}}@media (max-width:560px){.voice-library-nav{grid-template-columns:1fr}.voice-list-head{flex-direction:column}.voice-row-actions button{flex:1}.voice-clone-actions{align-items:flex-start;flex-direction:column}.voice-clone-actions>div{width:100%;justify-content:flex-end}}
</style>
