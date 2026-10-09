<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  activateLiveAgentConfigVersion,
  cloneLiveVoiceModelBinding,
  cloneLiveVoiceProfile,
  designLiveVoiceProfile,
  createLiveAgentConfigDraft,
  deleteLiveVoiceProfile,
  renameLiveVoiceProfile,
  saveLiveVoiceModelPreference,
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
import { useFeedbackErrorRef } from '../uiFeedback'

const props = defineProps<{ roomId: number | null; roomName?: string; supportSession?: boolean }>()
const emit = defineEmits<{ changed: [] }>()

const VOICE_PREVIEW_TEXT = '你好，我是你的主播，欢迎为你效劳。'
const DEFAULT_CLONE_MODEL = 'qwen-audio-3.0-tts-plus'
const MIN_SAMPLE_SECONDS = 5
const MAX_SAMPLE_SECONDS = 60
const MAX_SAMPLE_BYTES = 10 * 1024 * 1024
const cloneModels = [
  { id: 'qwen-audio-3.0-tts-plus', label: '通用模型（推荐）' },
  { id: 'qwen-audio-3.0-tts-flash', label: '经济模型 · 3.0 Flash' },
  { id: 'qwen-audio-3.1-tts-flash', label: '极速模型' },
]
const libraryModels = [
  { id: 'qwen_audio', label: 'Qwen Audio', description: '官方音色与已有专属声音' },
  { id: 'cosyvoice-v3.5-flash', label: 'CosyVoice 3.5 Flash', description: '声音复刻 / 声音设计 · 0.8 元 / 万字符' },
  { id: 'cosyvoice-v3.5-plus', label: 'CosyVoice 3.5 Plus', description: '声音复刻 / 声音设计' },
]
const allCloneModels = [...cloneModels, ...libraryModels.filter(item => item.id !== 'qwen_audio')]
const libraryModel = ref('qwen_audio')
const isQwenLibrary = computed(() => libraryModel.value === 'qwen_audio')
const libraryDetails = computed(() => libraryModels.find(item => item.id === libraryModel.value) || libraryModels[0]!)
const visibleVoiceProfiles = computed(() => voiceProfiles.value.filter(profile => {
  const target = String(asRecord(profile.config).target_model || '')
  return isQwenLibrary.value ? !target.startsWith('cosyvoice-') : target === libraryModel.value
}))
let libraryInitialized = false
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
const savingModelIds = ref<number[]>([])
const loading = ref(false)
const selecting = ref(false)
const bindingBusyKey = ref('')
const deletingProfileId = ref(0)
const editingNameId = ref(0)
const savingNameId = ref(0)
const nameDraft = ref('')
const nameError = ref('')
let nameScope = 0
const error = useFeedbackErrorRef('')
const notice = ref('')
const previewingKey = ref('')
const previewPhase = ref<'idle' | 'loading' | 'playing'>('idle')
let previewEpoch = 0
let previewAudio: HTMLAudioElement | null = null

const cloneOpen = ref(false)
const cloneMode = ref<'upload' | 'design'>('upload')
const voiceDescription = ref('')
const cloneName = ref('')
const cloneModel = ref(DEFAULT_CLONE_MODEL)
const selectedAvatarKey = ref<(typeof avatarOptions)[number]['key']>('female_young')
const sampleFile = ref<File | null>(null)
const sampleDuration = ref(0)
const sampleError = ref('')
const cloneError = ref('')
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

const currentVoiceName = computed(() => voiceProfiles.value.find(profile => profile.id === selectedProfileId.value)?.name || String(selectedVoice.value.name || '').trim() || '龙安灵心')
const selectedProfileId = computed(() => Number(selectedVoice.value.profile_id || 0))
const canSubmitClone = computed(
  () => Boolean(cloneName.value.trim() && !selecting.value && (cloneMode.value === 'design'
    ? !isQwenLibrary.value && voiceDescription.value.trim() && countVoiceCharacters(voiceDescription.value) <= 500
    : sampleFile.value && !sampleError.value && sampleDuration.value >= MIN_SAMPLE_SECONDS && sampleDuration.value <= MAX_SAMPLE_SECONDS)),
)

function countVoiceCharacters(value: string) {
  return Array.from(value).reduce((sum, char) => sum + (/\p{Script=Han}/u.test(char) ? 2 : 1), 0)
}

function storedLibrary() {
  try { return localStorage.getItem(`xiaolan.voice-library.room:${props.roomId}`) || '' } catch { return '' }
}

function changeLibrary() {
  stopPreview()
  cancelRename()
  closeCloneDialog()
  error.value = ''
  notice.value = ''
  try { localStorage.setItem(`xiaolan.voice-library.room:${props.roomId}`, libraryModel.value) } catch { /* optional UI preference */ }
}

function modelsForProfile(profile: VoiceProfile) {
  const target = String(asRecord(profile.config).target_model || '')
  return target.startsWith('cosyvoice-') ? allCloneModels.filter(item => item.id === target) : cloneModels
}

async function load() {
  if (!props.roomId) {
    officialVoices.value = []
    voiceProfiles.value = []
    voiceBindings.value = []
    versions.value = []
    return
  }
  loading.value = true
  const scope = nameScope, room = props.roomId
  error.value = ''
  try {
    const [official, profiles, bindings, configVersions, profileQuota] = await Promise.all([
      getLiveOfficialVoices(),
      getLiveVoiceProfiles(),
      getLiveVoiceModelBindings(),
      getLiveAgentConfigVersions(),
      getLiveVoiceProfileQuota(),
    ])
    if (scope !== nameScope || room !== props.roomId) return
    officialVoices.value = official.items || []
    voiceProfiles.value = (profiles || []).filter((item) => item.clone_status !== 'disabled')
    voiceBindings.value = (bindings || []).filter((item) => item.status !== 'disabled')
    versions.value = configVersions || []
    quota.value = profileQuota
    if (!libraryInitialized) {
      const saved = storedLibrary()
      const current = String(selectedVoice.value.model || '')
      libraryModel.value = libraryModels.some(item => item.id === saved) ? saved : current.startsWith('cosyvoice-') && libraryModels.some(item => item.id === current) ? current : 'qwen_audio'
      libraryInitialized = true
    }
    for (const profile of voiceProfiles.value) {
      if (!savingModelIds.value.includes(profile.id)) selectedModels.value[profile.id] = profileTargetModel(profile)
    }
  } catch (err) {
    if (scope === nameScope && room === props.roomId) error.value = err instanceof Error ? err.message : '读取声音中心失败'
  } finally {
    if (scope === nameScope && room === props.roomId) loading.value = false
  }
}

function beginRename(profile: VoiceProfile, event: MouseEvent) {
  if (props.supportSession || savingNameId.value) return
  const row = (event.currentTarget as HTMLElement).closest('.custom-copy')
  editingNameId.value = profile.id
  nameDraft.value = profile.name
  nameError.value = ''
  void nextTick(() => { const input = row?.querySelector('input'); input?.focus(); input?.select() })
}

function cancelRename() { if (savingNameId.value) return; editingNameId.value = 0; nameError.value = '' }

function nameKeydown(event: KeyboardEvent, profile: VoiceProfile) {
  if (event.isComposing) return
  if (event.key === 'Enter') { event.preventDefault(); void saveName(profile) }
  if (event.key === 'Escape') { event.preventDefault(); cancelRename() }
}

async function saveName(profile: VoiceProfile) {
  if (editingNameId.value !== profile.id || savingNameId.value) return
  const name = nameDraft.value.trim()
  if (!name || Array.from(name).length > 80) { nameError.value = '声音名称须为1～80字'; return }
  if (name === profile.name) { cancelRename(); return }
  const scope = nameScope
  savingNameId.value = profile.id
  nameError.value = ''
  try {
    const updated = await renameLiveVoiceProfile(profile.id, name, profile.name)
    if (scope !== nameScope) return
    voiceProfiles.value = voiceProfiles.value.map(item => item.id === profile.id ? { ...item, name: updated.name } : item)
    editingNameId.value = 0
    notice.value = '声音名称已保存'
  } catch (err) {
    if (scope === nameScope) nameError.value = err instanceof Error ? err.message : '名称没有保存，请重试'
  } finally { if (scope === nameScope) savingNameId.value = 0 }
}

function profileTargetModel(profile: VoiceProfile) {
  const saved = String(asRecord(asRecord(profile.config).ui_model_preferences)[String(props.roomId || 0)] || '')
  if (modelsForProfile(profile).some(item => item.id === saved)) return saved
  if (selectedProfileId.value === profile.id && selectedVoice.value.model) return String(selectedVoice.value.model)
  const configured = String(asRecord(profile.config).target_model || '').trim()
  return allCloneModels.some((item) => item.id === configured) ? configured : DEFAULT_CLONE_MODEL
}

async function rememberModel(profile: VoiceProfile) {
  if (props.supportSession || !props.roomId || savingModelIds.value.includes(profile.id)) return
  const scope = nameScope, room = props.roomId, chosen = selectedModels.value[profile.id] || DEFAULT_CLONE_MODEL
  const previous = profileTargetModel(profile)
  savingModelIds.value.push(profile.id)
  error.value = ''; notice.value = ''
  stopPreview()
  try {
    const updated = await saveLiveVoiceModelPreference(room, profile.id, chosen)
    if (scope !== nameScope || room !== props.roomId) return
    voiceProfiles.value = voiceProfiles.value.map(item => item.id === profile.id ? updated : item)
    notice.value = '模型选择已记住；试听不改变直播声音，点击“使用这个模型”后应用到当前直播间。'
  } catch (err) {
    if (scope === nameScope && room === props.roomId) { selectedModels.value[profile.id] = previous; error.value = err instanceof Error ? err.message : '模型选择保存失败' }
  } finally { if (scope === nameScope) savingModelIds.value = savingModelIds.value.filter(id => id !== profile.id) }
}

function isCustomModelSelected(profile: VoiceProfile) {
  return isCustomSelected(profile) && String(selectedVoice.value.model || profileTargetModel(profile)) === (selectedModels.value[profile.id] || profileTargetModel(profile))
}

function customPreviewKey(profile: VoiceProfile) { return `custom-${profile.id}-${selectedModels.value[profile.id] || profileTargetModel(profile)}` }

function stopPreview() {
  ++previewEpoch
  previewPhase.value = 'idle'
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
  const epoch = previewEpoch
  previewingKey.value = key
  previewPhase.value = 'loading'
  error.value = ''
  notice.value = ''
  try {
    const result = await factory()
    if (epoch !== previewEpoch) return
    const audio = new Audio(liveSupportMediaPlaybackURL(result.audio_url))
    previewAudio = audio
    audio.addEventListener('playing', () => { if (epoch === previewEpoch) previewPhase.value = 'playing' })
    audio.addEventListener('waiting', () => { if (epoch === previewEpoch) previewPhase.value = 'loading' })
    audio.addEventListener('ended', () => { if (epoch === previewEpoch) stopPreview() }, { once: true })
    audio.addEventListener('error', () => { if (epoch === previewEpoch) { stopPreview(); error.value = '试听声音加载失败，请重试' } }, { once: true })
    audio.addEventListener('pause', () => { if (epoch === previewEpoch) previewPhase.value = 'idle' })
    await audio.play()
  } catch (err) {
    if (epoch !== previewEpoch) return
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
  const scope = nameScope, room = props.roomId
  selecting.value = true
  error.value = ''
  notice.value = ''
  try {
    const draft = await createLiveAgentConfigDraft({
      speech_config: updatedSpeechConfig({ ...voice, selected_at: new Date().toISOString() }),
    })
    if (scope !== nameScope || room !== props.roomId) return false
    await activateLiveAgentConfigVersion(draft.id)
    if (scope !== nameScope || room !== props.roomId) return false
    await load()
    if (scope !== nameScope || room !== props.roomId) return false
    notice.value = `“${String(voice.name || '声音')}”已用于直播间「${props.roomName || '当前直播间'}」`
    emit('changed')
    return true
  } catch (err) {
    if (scope === nameScope) error.value = err instanceof Error ? err.message : '保存直播间声音失败'
    return false
  } finally {
    if (scope === nameScope) selecting.value = false
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
  const scope = nameScope
  bindingBusyKey.value = key
  error.value = ''
  try {
    const binding = await cloneLiveVoiceModelBinding(profile.id, { model })
    if (scope !== nameScope) return null
    voiceBindings.value = [binding, ...voiceBindings.value.filter((item) => item.id !== binding.id)]
    return binding
  } catch (err) {
    if (scope === nameScope) error.value = err instanceof Error ? err.message : '生成模型音色失败'
    return null
  } finally {
    if (scope === nameScope) bindingBusyKey.value = ''
  }
}

async function previewCustom(profile: VoiceProfile) {
  if (savingModelIds.value.includes(profile.id)) return
  const model = selectedModels.value[profile.id] || profileTargetModel(profile)
  await playURL(customPreviewKey(profile), async () => {
    const binding = await ensureBinding(profile, model)
    if (!binding) throw new Error(error.value || '声音模型尚未准备好')
    return previewLiveVoiceModelBinding(binding.id, VOICE_PREVIEW_TEXT)
  })
}

async function useCustom(profile: VoiceProfile) {
  if (savingModelIds.value.includes(profile.id) || !props.roomId) return
  const scope = nameScope, room = props.roomId
  const model = selectedModels.value[profile.id] || profileTargetModel(profile)
  const binding = await ensureBinding(profile, model)
  if (!binding || binding.status !== 'ready' || scope !== nameScope || room !== props.roomId) return
  const saved = await bindVoice({
    source: 'clone', provider: binding.provider, name: profile.name, voice_id: binding.voice_id,
    profile_id: profile.id, binding_id: binding.id, model: binding.model, rate: binding.rate || 1,
  })
  if (!saved || scope !== nameScope || room !== props.roomId) return
  selecting.value = true
  try {
    await publishLiveAgentVoiceBinding(room, binding.id)
    if (scope !== nameScope || room !== props.roomId) return
    await load()
    emit('changed')
  } catch (err) {
    if (scope === nameScope) error.value = err instanceof Error ? err.message : '声音已保存，但应用到智能体方案失败'
  } finally {
    if (scope === nameScope) selecting.value = false
  }
}

function openCloneDialog(mode: 'upload' | 'design' = 'upload') {
  error.value = ''
  notice.value = ''
  activeLibrary.value = 'custom'
  if (quota.value.limit > 0 && quota.value.used >= quota.value.limit) {
    error.value = `自定义声音已达到直播间权限上限（${quota.value.limit} 个），${props.supportSession ? '请联系客户释放声音名额。' : '请先删除一个已有声音再生成。'}`
    return
  }
  cloneOpen.value = true
  cloneMode.value = mode
  cloneModel.value = isQwenLibrary.value ? DEFAULT_CLONE_MODEL : libraryModel.value
}

function resetCloneForm() {
  cloneName.value = ''
  cloneModel.value = isQwenLibrary.value ? DEFAULT_CLONE_MODEL : libraryModel.value
  voiceDescription.value = ''
  cloneMode.value = 'upload'
  selectedAvatarKey.value = 'female_young'
  sampleFile.value = null
  sampleDuration.value = 0
  sampleError.value = ''
  cloneError.value = ''
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
  if (selecting.value) return
  const target = event.target as HTMLInputElement
  const file = target.files?.[0] || null
  sampleFile.value = null
  sampleDuration.value = 0
  sampleError.value = ''
  cloneError.value = ''
  if (!file) return
  if (file.size > MAX_SAMPLE_BYTES) {
    sampleError.value = '声音样本不能超过 10MB，请压缩后重新选择。'
    target.value = ''
    return
  }
  try {
    const duration = await readAudioDuration(file)
    sampleDuration.value = duration
    if (duration < MIN_SAMPLE_SECONDS || duration > MAX_SAMPLE_SECONDS) {
      sampleError.value = `录音需为 5–60 秒，推荐 10–20 秒；当前约 ${duration.toFixed(1)} 秒，请重新选择。`
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
  if (!canSubmitClone.value || (cloneMode.value === 'upload' && !sampleFile.value)) return
  selecting.value = true
  error.value = ''
  cloneError.value = ''
  notice.value = ''
  try {
    if (cloneMode.value === 'design') {
      cloneStage.value = 'processing'
      await designLiveVoiceProfile({ name: cloneName.value.trim(), model: libraryModel.value, description: voiceDescription.value.trim(), avatar_key: selectedAvatarKey.value })
    } else {
    cloneStage.value = 'uploading'
    const uploaded = await uploadLiveMediaAsset(sampleFile.value!, 'voice_sample', {
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
    }
    cloneOpen.value = false
    resetCloneForm()
    await load()
    notice.value = '声音已转为规范格式并生成成功，现在可以试听或用于当前直播间。'
  } catch (err) {
    cloneError.value = err instanceof Error ? err.message : '声音生成失败'
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

watch(() => props.roomId, () => { nameScope++; libraryInitialized = false; libraryModel.value = 'qwen_audio'; cloneOpen.value = false; resetCloneForm(); stopPreview(); bindingBusyKey.value = ''; selecting.value = false; selectedModels.value = {}; savingModelIds.value = []; editingNameId.value = 0; savingNameId.value = 0; nameError.value = ''; void load() })
onMounted(() => void load())
onBeforeUnmount(() => { nameScope++; stopPreview() })
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

    <div class="voice-model-bar">
      <label><span class="side-eyebrow">大模型</span><select v-model="libraryModel" aria-label="声音大模型" :disabled="loading || selecting || Boolean(bindingBusyKey) || Boolean(savingNameId) || savingModelIds.length > 0" @change="changeLibrary"><option v-for="model in libraryModels" :key="model.id" :value="model.id">{{ model.label }}</option></select></label>
      <div><strong>{{ libraryDetails.label }}</strong><p>{{ libraryDetails.description }}</p></div>
      <span class="model-scope-note">选择声音并点击使用后生效</span>
    </div>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-if="notice" class="inline-notice">{{ notice }}</p>

    <div class="voice-workspace">
      <aside class="voice-side-card">
        <div><span class="side-eyebrow">声音管理</span><h3>样本声音 · 自定义音色</h3><p>选择喜欢的声音，让你的内容更有个性</p></div>
        <nav class="voice-library-nav" aria-label="声音库">
          <button type="button" :class="{ active: activeLibrary === 'official' }" @click="activeLibrary = 'official'">
            <span class="nav-icon official-icon">★</span>
            <span><b>官方声音</b><small>{{ isQwenLibrary ? '精选官方优质音色' : '查看当前模型音色说明' }}</small></span><em>›</em>
          </button>
          <button type="button" :class="{ active: activeLibrary === 'custom' }" @click="activeLibrary = 'custom'">
            <span class="nav-icon custom-icon">●</span>
            <span><b>我的声音</b><small>{{ isQwenLibrary ? '已创建的专属音色' : '当前模型的专属音色' }}</small></span><em>›</em>
          </button>
        </nav>
        <div class="voice-side-art" aria-hidden="true"><span class="chat-bubble">•••</span><span class="microphone">♩</span><span class="sound-wave">▂▄▆▄▂</span></div>
      </aside>

      <section class="voice-list-panel">
        <header class="voice-list-head">
          <div>
            <div class="voice-title-line"><h3>{{ activeLibrary === 'official' ? '官方声音' : '我的声音' }}</h3><span v-if="activeLibrary === 'custom'" class="quota-chip">{{ quota.used }} / {{ quota.limit }} 个</span></div>
            <p v-if="activeLibrary === 'official' && isQwenLibrary">精选多种风格的官方音色，适用于不同直播场景</p>
            <p v-else-if="activeLibrary === 'official'">{{ libraryDetails.label }} 使用声音复刻和声音设计创建专属音色</p>
            <p v-else>{{ isQwenLibrary ? '使用你创建的专属音色，选择不同模型体验不同效果' : '这里仅展示为当前模型创建的声音，Flash 与 Plus 分别管理' }}</p>
          </div>
          <div v-if="activeLibrary === 'custom'" class="voice-create-actions"><button v-if="!isQwenLibrary" type="button" class="design-voice-button" :disabled="selecting" @click="openCloneDialog('design')">描述生成声音</button><button type="button" class="upload-sample-button" :disabled="selecting" @click="openCloneDialog()"><span>＋</span> 上传声音样本</button></div>
        </header>

        <div v-if="loading" class="voice-loading">正在读取声音…</div>
        <div v-else-if="activeLibrary === 'official' && !isQwenLibrary" class="voice-empty cosy-official-empty">
          <span>♫</span><strong>用 {{ libraryDetails.label }} 创建专属声音</strong><p>此模型没有预置官方音色。上传录音复刻你的声音，或用文字描述设计新声音。</p><div class="empty-create-actions"><button type="button" @click="openCloneDialog()">＋ 上传声音样本</button><button type="button" @click="openCloneDialog('design')">描述生成声音</button></div>
        </div>
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

        <div v-else-if="visibleVoiceProfiles.length" class="voice-rows custom-voice-rows">
          <article v-for="profile in visibleVoiceProfiles" :key="profile.id" class="voice-row custom-voice-row" :class="{ selected: isCustomSelected(profile) }">
            <div class="voice-avatar custom-avatar" :class="'tone-' + avatarForProfile(profile).tone"><span>{{ avatarForProfile(profile).emoji }}</span><i class="avatar-edit">✓</i></div>
            <div class="voice-row-copy custom-copy"><div>
              <input v-if="editingNameId === profile.id" v-model="nameDraft" class="voice-name-input" aria-label="声音名称" maxlength="80" :disabled="savingNameId === profile.id" @keydown.stop="nameKeydown($event, profile)" @blur="saveName(profile)" />
              <template v-else><strong>{{ profile.name }}</strong><button v-if="!props.supportSession" type="button" class="voice-name-edit" :aria-label="'修改声音名称：' + profile.name" title="修改名称" :disabled="!!savingNameId" @click.stop="beginRename(profile, $event)"><svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.8" aria-hidden="true"><path d="m15 5 4 4M4 20l4-1 12-12a2.8 2.8 0 0 0-4-4L4 15z" /></svg></button></template>
              <span v-if="isCustomSelected(profile)">当前使用</span>
            </div><small v-if="editingNameId === profile.id && nameError" class="voice-name-error" role="alert">{{ nameError }}</small><small v-else-if="savingNameId === profile.id" class="voice-name-status" role="status">正在保存…</small><p>创建于 {{ createdLabel(profile.created_at) }}</p></div>
            <label class="model-picker"><span>{{ savingModelIds.includes(profile.id) ? '正在记住选择…' : isQwenLibrary ? '选择模型' : '所属模型' }}</span><select v-model="selectedModels[profile.id]" :disabled="!isQwenLibrary || selecting || savingModelIds.includes(profile.id)" @change="rememberModel(profile)"><option v-for="model in modelsForProfile(profile)" :key="model.id" :value="model.id">{{ model.label }}</option></select><small v-if="isCustomSelected(profile) && !isCustomModelSelected(profile)">当前生效：{{ allCloneModels.find(model => model.id === selectedVoice.model)?.label || selectedVoice.model }}</small></label>
            <div class="voice-row-actions custom-actions">
              <button type="button" class="preview-button" :disabled="savingModelIds.includes(profile.id) || (Boolean(bindingBusyKey) && previewingKey !== customPreviewKey(profile))" @click="previewCustom(profile)"><span>{{ previewingKey === customPreviewKey(profile) ? 'Ⅱ' : '▶' }}</span>{{ previewingKey === customPreviewKey(profile) ? previewPhase === 'playing' ? '停止试听' : '准备中…' : '试听' }}</button>
              <button type="button" class="use-voice-button" :class="{ selected: isCustomModelSelected(profile) }" :disabled="selecting || Boolean(bindingBusyKey) || savingModelIds.includes(profile.id)" @click="useCustom(profile)">{{ isCustomModelSelected(profile) ? '当前使用' : isCustomSelected(profile) ? '使用这个模型' : '使用这个声音' }}</button>
              <button v-if="!props.supportSession" type="button" class="delete-voice-button" :disabled="deletingProfileId === profile.id" title="删除自定义声音" @click="removeCustomVoice(profile)">{{ deletingProfileId === profile.id ? '…' : '删除' }}</button>
            </div>
          </article>
        </div>

        <div v-else-if="activeLibrary === 'custom'" class="voice-empty"><span>♫</span><strong>{{ isQwenLibrary ? '还没有自定义声音' : '当前模型还没有专属声音' }}</strong><p>上传清晰录音，推荐 10–20 秒，最长 60 秒，创建你的专属音色。</p><div class="empty-create-actions"><button type="button" @click="openCloneDialog()">＋ 上传声音样本</button><button v-if="!isQwenLibrary" type="button" @click="openCloneDialog('design')">描述生成声音</button></div></div>
        <div v-if="previewingKey" class="voice-preview-wave-panel" :class="{ 'is-playing': previewPhase === 'playing' }" role="status"><span>{{ previewPhase === 'playing' ? '正在试听' : '正在准备试听声音…' }}</span><div class="voice-preview-wave" aria-hidden="true"><i v-for="bar in 27" :key="bar" :style="{ '--bar-height': (12 + Math.abs(Math.sin(bar * .77)) * 30) + 'px', '--bar-delay': (bar % 7) * -.12 + 's' }"></i></div><button type="button" @click="stopPreview">停止</button></div>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="cloneOpen" class="voice-dialog-overlay" @click.self="closeCloneDialog">
        <section class="voice-dialog" role="dialog" aria-modal="true" :aria-label="cloneMode === 'design' ? '描述生成声音' : '上传声音样本'">
          <header><div><span class="section-kicker">CUSTOM VOICE</span><h3>{{ cloneMode === 'design' ? '描述生成声音' : '上传声音样本' }}</h3><p>{{ cloneMode === 'design' ? '描述声音的性别、年龄、音调和风格，创建专属主播声音。' : '选择头像、填写名称，再上传单人清晰录音：推荐 10–20 秒，最长 60 秒。' }}</p></div><button type="button" class="voice-close" :disabled="selecting" @click="closeCloneDialog">×</button></header>
          <div class="dialog-quota"><span>自定义声音名额</span><b>已用 {{ quota.used }} 个，共 {{ quota.limit }} 个</b></div>
          <fieldset class="avatar-picker" :disabled="selecting">
            <legend>选择头像</legend>
            <button v-for="avatar in avatarOptions" :key="avatar.key" type="button" :class="['avatar-option', 'tone-' + avatar.tone, { active: selectedAvatarKey === avatar.key }]" @click="selectedAvatarKey = avatar.key"><span>{{ avatar.emoji }}</span><small>{{ avatar.label }}</small></button>
          </fieldset>
          <label class="voice-clone-field"><span>声音名字</span><input v-model="cloneName" :disabled="selecting" maxlength="60" placeholder="例如：小悠、门店主播" /></label>
          <label class="voice-clone-field"><span>声音模型</span><select v-if="isQwenLibrary" v-model="cloneModel" :disabled="selecting"><option v-for="model in cloneModels" :key="model.id" :value="model.id">{{ model.label }}</option></select><strong v-else class="dialog-model-label">{{ libraryDetails.label }}</strong></label>
          <label v-if="cloneMode === 'design'" class="voice-clone-field"><span>声音描述</span><textarea v-model="voiceDescription" :disabled="selecting" placeholder="例如：30岁左右的女性，音色温暖明亮，语气亲切，吐字清晰，适合直播讲解。"></textarea><small :class="{ 'sample-error': countVoiceCharacters(voiceDescription) > 500 }">{{ countVoiceCharacters(voiceDescription) }} / 500 字符（汉字计2个）</small></label>
          <label v-else class="voice-clone-upload" :class="{ invalid: sampleError }">
            <input ref="sampleInput" type="file" accept="audio/*" :disabled="selecting" @change="chooseSample" /><span class="upload-icon">↑</span>
            <strong>{{ sampleFile ? sampleFile.name : '选择声音文件（最长 60 秒）' }}</strong>
            <small v-if="sampleFile">时长 {{ sampleDuration.toFixed(1) }} 秒 · 将自动转为标准 WAV 格式</small><small v-else>5–60 秒、不超过 10MB；至少 5 秒清晰人声，无音乐、无噪声</small>
          </label>
          <p v-if="sampleError" class="sample-error">{{ sampleError }}</p>
          <p v-if="cloneError" class="voice-clone-error" role="alert">{{ cloneError }}</p>
          <footer class="voice-clone-actions">
            <span role="status" aria-live="polite" :class="{ 'clone-progress': selecting }"><i v-if="selecting" class="clone-spinner" aria-hidden="true"></i>{{ cloneStage === 'uploading' ? '正在上传声音样本…' : cloneStage === 'processing' ? '正在处理样本并生成声音，请稍候…' : '生成成功后会自动显示在“我的声音”中' }}</span>
            <div><button type="button" :disabled="selecting" @click="closeCloneDialog">取消</button><button type="button" class="primary-button" :disabled="!canSubmitClone" @click="submitClone">{{ selecting ? '处理中…' : '确定并生成' }}</button></div>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.voice-library-nav button.active b{color:#fff}.voice-library-nav button:not(.active) b{color:#253654}
.voice-model-bar{display:flex;align-items:center;gap:24px;padding:18px 22px;border:1px solid #dfe7f6;border-radius:20px;background:linear-gradient(120deg,#fff,#f4f7ff);box-shadow:0 10px 26px rgba(76,101,168,.05)}.voice-model-bar label{min-width:260px}.voice-model-bar select,.voice-clone-field select{width:100%;min-height:45px;padding:9px 36px 9px 13px;border:1px solid #d4ddf2;border-radius:12px;background:#fff;color:#253654;font:inherit;font-weight:800}.voice-model-bar select:focus-visible{outline:2px solid #7586f8;outline-offset:2px}.voice-model-bar select:disabled{opacity:.6}.voice-model-bar strong{font-size:16px}.voice-model-bar p{margin:5px 0 0;color:#7e8ca3;font-size:13px}.model-scope-note{margin-left:auto;color:#8a96ac;font-size:12px}.voice-create-actions,.empty-create-actions{display:flex;gap:10px;flex-wrap:wrap}.design-voice-button{min-height:43px;padding:0 17px;border:1px solid #d9e1f0;border-radius:13px;background:#f0f3ff;color:#5265d9;font-weight:900;cursor:pointer}.cosy-official-empty{min-height:380px}.voice-clone-field textarea{min-height:110px;padding:12px;border:1px solid #d2dbea;border-radius:12px;background:#fff;resize:vertical;font:inherit;line-height:1.6}.voice-clone-field small{font-size:12px;color:#7e8ca3;font-weight:500}.dialog-model-label{padding:11px 13px;border-radius:12px;background:#edf1ff;color:#5265d9;font-size:14px}@media(max-width:860px){.voice-model-bar{align-items:flex-start;flex-wrap:wrap;gap:14px}.voice-model-bar label{min-width:0;flex:1 1 250px}.model-scope-note{margin-left:0}.voice-create-actions{justify-content:flex-end}}@media(max-width:560px){.voice-model-bar{flex-direction:column}.voice-model-bar label{flex:none;width:100%}.voice-create-actions{justify-content:flex-start}}
.voice-name-edit{display:inline-grid;place-items:center;flex:none;width:28px;height:28px;padding:0;border:0;border-radius:7px;background:transparent;color:#6271ec;cursor:pointer}.voice-name-edit:hover{background:#edf0ff}.voice-name-edit:focus-visible{outline:2px solid #6373ed;outline-offset:2px}.voice-name-input{min-width:80px;width:210px;max-width:100%;padding:4px 8px;border:1px solid #8d9af6;border-radius:8px;background:#fff;color:#17233a;font:inherit;font-weight:800;outline:none}.voice-name-error{display:block;color:#b92e45;margin-top:6px}.voice-name-status{display:block;color:#7786a6;margin-top:6px}
.voice-clone-error{display:block;margin:12px 0 0;padding:11px 14px;border:1px solid #ffcfd7;border-radius:12px;background:#fff0f2;color:#b92e45;font-weight:700;overflow-wrap:anywhere}
.clone-progress{display:flex;align-items:center;gap:8px}.clone-spinner{width:16px;height:16px;flex:none;border:2px solid #dce2ff;border-top-color:#5669f5;border-radius:50%;animation:clone-spin .8s linear infinite}@keyframes clone-spin{to{transform:rotate(360deg)}}
.voice-center{display:grid;gap:14px;padding:2px 0 28px;color:#17233a}.voice-room-strip{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:16px 20px;border:1px solid #e2e8f5;border-radius:18px;background:linear-gradient(120deg,#fff,#f6f9ff);box-shadow:0 12px 30px rgba(76,101,168,.06)}.voice-room-strip>div:first-child{display:grid;gap:2px}.section-kicker{font-size:11px;font-weight:900;letter-spacing:.16em;color:#8391aa}.voice-room-strip strong{font-size:20px}.room-voice-state{display:flex;align-items:center;gap:8px;color:#8490a5;font-size:12px}.room-voice-state b{color:#253654;font-size:13px}.room-voice-state i{width:1px;height:22px;margin:0 8px;background:#dde5f2}.inline-error,.inline-notice{margin:0;padding:11px 14px;border-radius:12px;font-weight:700}.inline-error{color:#b92e45;background:#fff0f2;border:1px solid #ffcfd7}.inline-notice{color:#087a5a;background:#edfff8;border:1px solid #bdeedd}
.voice-workspace{display:grid;grid-template-columns:minmax(270px,310px) minmax(0,1fr);gap:14px;min-height:590px;padding:12px;border:1px solid #dfe7f6;border-radius:26px;background:linear-gradient(145deg,#f7f9ff,#edf3ff);box-shadow:0 24px 60px rgba(41,76,150,.08)}.voice-side-card,.voice-list-panel{position:relative;overflow:hidden;border:1px solid rgba(222,230,245,.9);border-radius:22px;background:rgba(255,255,255,.82)}.voice-side-card{display:flex;flex-direction:column;padding:28px 22px 22px;background:linear-gradient(160deg,#fff 0%,#f7f9ff 64%,#eaf0ff 100%)}.side-eyebrow{display:block;margin-bottom:7px;color:#8190a9;font-size:12px;font-weight:800}.voice-side-card h3{margin:0;font-size:25px;line-height:1.25}.voice-side-card>div>p{margin:10px 0 0;color:#75849d;font-size:14px;line-height:1.6}.voice-library-nav{display:grid;gap:12px;margin-top:34px}.voice-library-nav button{display:grid;grid-template-columns:52px 1fr auto;align-items:center;gap:13px;min-height:84px;padding:12px 14px;border:1px solid #e1e7f3;border-radius:17px;background:rgba(255,255,255,.86);color:#243450;text-align:left;cursor:pointer;transition:.22s ease}.voice-library-nav button:hover{transform:translateY(-2px);box-shadow:0 12px 26px rgba(62,91,165,.12)}.voice-library-nav button.active{border-color:#6f82ff;background:linear-gradient(135deg,#7194ff,#5764ed);color:#fff;box-shadow:0 16px 30px rgba(76,91,235,.24)}.voice-library-nav button>span:nth-child(2){display:grid;gap:3px}.voice-library-nav b{font-size:18px}.voice-library-nav small{color:#8a96aa}.voice-library-nav button.active small{color:#e6ebff}.voice-library-nav em{font-style:normal;font-size:32px;font-weight:300}.nav-icon{width:50px;height:50px;display:grid;place-items:center;border-radius:15px;background:linear-gradient(145deg,#edf1ff,#dfe6ff);color:#5368ed;font-size:22px;box-shadow:inset 0 1px #fff}.voice-library-nav button.active .nav-icon{background:linear-gradient(145deg,#3459f4,#2440cf);color:#fff}.voice-side-art{position:absolute;left:0;right:0;bottom:0;height:180px;pointer-events:none;background:radial-gradient(circle at 74% 58%,rgba(86,114,239,.16),transparent 35%),linear-gradient(160deg,transparent 38%,rgba(207,218,248,.45) 39%,rgba(183,198,239,.36) 65%,transparent 66%)}.chat-bubble{position:absolute;left:48%;top:42px;padding:16px 20px;border-radius:15px;background:linear-gradient(145deg,#e3eaff,#becdf9);color:#fff;letter-spacing:4px;font-weight:900}.microphone{position:absolute;right:42px;top:64px;width:54px;height:72px;display:grid;place-items:center;border-radius:30px;background:linear-gradient(#7aa0ff,#334ee4);color:#fff;font-size:34px;box-shadow:0 12px 30px rgba(57,82,222,.25)}.sound-wave{position:absolute;right:15px;top:95px;color:#8da4f3;letter-spacing:2px}
.voice-list-panel{padding:26px;background:rgba(255,255,255,.88)}.voice-list-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;margin-bottom:22px}.voice-list-head h3{margin:0;font-size:25px}.voice-list-head p{margin:6px 0 0;color:#75839b;font-size:14px}.voice-title-line{display:flex;align-items:center;gap:10px}.quota-chip{padding:4px 9px;border-radius:999px;background:#eef0ff;color:#5e61df;font-size:12px;font-weight:800}.upload-sample-button{min-height:43px;padding:0 17px;border:1px solid #d9e1f0;border-radius:13px;background:#fff;color:#30415f;font-weight:900;cursor:pointer;box-shadow:0 8px 18px rgba(48,74,132,.06);transition:.2s}.upload-sample-button:hover{transform:translateY(-2px);border-color:#8ea0ff;box-shadow:0 12px 24px rgba(79,94,226,.18)}.upload-sample-button span{font-size:21px;margin-right:5px}.voice-loading{padding:50px;text-align:center;color:#8090a7}.voice-rows{display:grid;gap:10px}.voice-row{display:grid;grid-template-columns:76px minmax(180px,1fr) auto;align-items:center;gap:16px;min-height:112px;padding:14px 16px;border:1px solid #e0e7f3;border-radius:18px;background:rgba(255,255,255,.92);transition:.2s}.voice-row:hover{transform:translateY(-1px);border-color:#bdc9f0;box-shadow:0 12px 28px rgba(48,74,132,.08)}.voice-row.selected{border-color:#8799ff;box-shadow:0 0 0 2px rgba(104,124,247,.08),0 12px 28px rgba(68,89,203,.11)}
.voice-avatar{position:relative;width:70px;height:70px;display:grid;place-items:center;border-radius:50%;box-shadow:inset 0 0 0 1px rgba(255,255,255,.75),0 8px 18px rgba(53,81,150,.14)}.voice-avatar>span{font-size:43px;filter:drop-shadow(0 3px 4px rgba(21,43,88,.12))}.avatar-wave{position:absolute;left:4px;right:4px;bottom:-2px;padding:1px 4px;border-radius:8px;background:#fff;color:#5070e9;font-size:9px;font-style:normal;letter-spacing:0;text-align:center}.tone-rose{background:linear-gradient(145deg,#fff1f6,#f6c8dc)}.tone-violet{background:linear-gradient(145deg,#f2edff,#c9b7f5)}.tone-amber{background:linear-gradient(145deg,#fff8dd,#f4d494)}.tone-blue{background:linear-gradient(145deg,#e9f5ff,#a9d0fa)}.tone-indigo{background:linear-gradient(145deg,#ecf0ff,#b1bdf8)}.tone-slate{background:linear-gradient(145deg,#eef3f7,#c8d2dc)}.tone-pink{background:linear-gradient(145deg,#fff0fa,#f3b8e0)}.tone-cyan{background:linear-gradient(145deg,#e8fbff,#9cddec)}.voice-row-copy{min-width:0}.voice-row-copy>div{display:flex;align-items:center;gap:9px}.voice-row-copy strong{font-size:20px}.voice-row-copy>div span{padding:4px 9px;border-radius:999px;background:#eef1ff;color:#6677ea;font-size:11px;font-weight:800}.voice-row-copy p{margin:7px 0 0;color:#718099;font-size:13px;line-height:1.5}.voice-row-actions{display:flex;align-items:center;gap:10px}.voice-row-actions button{min-height:45px;padding:0 18px;border:1px solid #dce4f2;border-radius:13px;background:#fff;color:#2f405f;font-weight:900;cursor:pointer;white-space:nowrap;transition:.2s}.voice-row-actions button:hover:not(:disabled){transform:translateY(-2px);box-shadow:0 10px 22px rgba(66,88,173,.16)}.voice-row-actions button:disabled{opacity:.58;cursor:not-allowed}.preview-button span{margin-right:7px;color:#4f6cf3}.use-voice-button{border-color:#5368ef!important;background:linear-gradient(135deg,#5778ff,#4e5ee5)!important;color:#fff!important;box-shadow:0 10px 20px rgba(70,84,219,.18)}.use-voice-button.selected{background:linear-gradient(135deg,#38b989,#21a974)!important;border-color:#21a974!important}
.custom-voice-row{grid-template-columns:76px minmax(150px,1fr) minmax(190px,230px) auto}.custom-avatar .avatar-edit{position:absolute;right:-2px;bottom:0;width:21px;height:21px;display:grid;place-items:center;border:3px solid #fff;border-radius:50%;background:#6178ef;color:#fff;font-size:10px;font-style:normal}.model-picker{display:grid;gap:5px}.model-picker>span{color:#8090a8;font-size:11px}.model-picker select{width:100%;min-height:43px;padding:0 36px 0 12px;border:1px solid #dbe3f1;border-radius:12px;background:#fff;color:#2d3e5d;font-weight:700}.delete-voice-button{min-width:56px!important;padding:0 10px!important;color:#b54a5b!important}.delete-voice-button:hover:not(:disabled){border-color:#f1b7c0!important;background:#fff3f5!important;box-shadow:none!important}.voice-empty{min-height:330px;display:grid;place-items:center;align-content:center;gap:8px;border:1px dashed #cbd6eb;border-radius:20px;background:linear-gradient(145deg,#fff,#f5f8ff);text-align:center;color:#77869e}.voice-empty>span{width:66px;height:66px;display:grid;place-items:center;border-radius:20px;background:#edf1ff;color:#5368ea;font-size:32px}.voice-empty strong{color:#253551;font-size:20px}.voice-empty p{margin:0}.voice-empty button{margin-top:8px;min-height:42px;padding:0 16px;border:0;border-radius:12px;background:#5669eb;color:#fff;font-weight:900;cursor:pointer}
.voice-dialog-overlay{position:fixed;inset:0;z-index:4400;display:grid;place-items:center;padding:22px;background:rgba(18,31,60,.52);backdrop-filter:blur(5px)}.voice-dialog{width:min(720px,95vw);max-height:92vh;overflow:auto;padding:24px;border:1px solid rgba(255,255,255,.8);border-radius:24px;background:linear-gradient(145deg,#fff,#f5f8ff);box-shadow:0 34px 90px rgba(16,31,72,.34)}.voice-dialog>header{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.voice-dialog h3{margin:4px 0 0;font-size:27px}.voice-dialog header p{margin:6px 0 0;color:#75849b}.voice-close{border:0;background:transparent;color:#7b889e;font-size:31px;cursor:pointer}.dialog-quota{display:flex;justify-content:space-between;gap:14px;margin-top:18px;padding:11px 14px;border-radius:12px;background:#edf2ff;color:#61718d;font-size:13px}.dialog-quota b{color:#445cdc}.avatar-picker{display:grid;grid-template-columns:repeat(4,1fr);gap:9px;margin:18px 0 0;padding:0;border:0}.avatar-picker legend{grid-column:1/-1;margin-bottom:8px;color:#33435f;font-weight:900}.avatar-option{display:grid;place-items:center;gap:3px;min-height:84px;padding:7px;border:2px solid transparent;border-radius:15px;cursor:pointer;transition:.18s}.avatar-option:hover{transform:translateY(-2px)}.avatar-option.active{border-color:#5b70ee;box-shadow:0 0 0 3px rgba(91,112,238,.11)}.avatar-option span{font-size:34px}.avatar-option small{color:#40506c;font-weight:800}.voice-clone-field{display:grid;gap:7px;margin-top:17px;color:#34445f;font-weight:900}.voice-clone-field input{min-height:45px;padding:9px 12px;border:1px solid #d2dbea;border-radius:12px;background:#fff;font:inherit}.voice-clone-upload{display:grid;place-items:center;gap:5px;margin-top:15px;padding:18px;border:1px dashed #aebce0;border-radius:16px;background:rgba(255,255,255,.86);cursor:pointer;text-align:center}.voice-clone-upload.invalid{border-color:#ef9cac;background:#fff6f7}.voice-clone-upload input{width:100%;font-size:12px}.voice-clone-upload .upload-icon{width:36px;height:36px;display:grid;place-items:center;border-radius:11px;background:#e9eeff;color:#536bed;font-size:22px;font-weight:900}.voice-clone-upload small{color:#7e8ca2}.sample-error{margin:7px 0 0;color:#bd3e52;font-size:13px;font-weight:700}.voice-clone-actions{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:20px;padding-top:16px;border-top:1px solid #e1e7f1}.voice-clone-actions>span{color:#75839a;font-size:12px}.voice-clone-actions>div{display:flex;gap:9px}.voice-clone-actions button{min-height:42px;padding:0 17px;border:1px solid #d7dfed;border-radius:11px;background:#fff;color:#34455f;font-weight:900;cursor:pointer}.primary-button{border-color:#5367ec!important;background:linear-gradient(135deg,#5978ff,#5360e5)!important;color:#fff!important}.voice-clone-actions button:disabled{opacity:.5;cursor:not-allowed}
@media (max-width:1180px){.voice-workspace{grid-template-columns:240px minmax(0,1fr)}.voice-side-card{padding:22px 16px}.custom-voice-row{grid-template-columns:66px 1fr 190px}.custom-actions{grid-column:2/-1;justify-content:flex-end}.voice-row-actions button{padding:0 13px}}@media (max-width:860px){.voice-room-strip{align-items:flex-start;flex-direction:column}.voice-workspace{grid-template-columns:1fr}.voice-side-card{min-height:auto}.voice-side-art{display:none}.voice-library-nav{grid-template-columns:1fr 1fr;margin-top:20px}.voice-list-panel{padding:18px}.voice-row,.custom-voice-row{grid-template-columns:64px 1fr}.voice-row-actions,.model-picker{grid-column:2}.voice-row-actions{justify-content:flex-start;flex-wrap:wrap}.voice-list-head{align-items:flex-start}.avatar-picker{grid-template-columns:repeat(2,1fr)}}@media (max-width:560px){.voice-library-nav{grid-template-columns:1fr}.voice-list-head{flex-direction:column}.voice-row-actions button{flex:1}.voice-clone-actions{align-items:flex-start;flex-direction:column}.voice-clone-actions>div{width:100%;justify-content:flex-end}}
.voice-preview-wave-panel{position:fixed;left:50%;top:50%;transform:translate(-50%,-50%);z-index:10030;box-sizing:border-box;display:grid;justify-items:center;gap:16px;width:min(380px,calc(100vw - 32px));padding:25px 28px;border:1px solid #e2dcff;border-radius:24px;background:linear-gradient(135deg,#fff,#f1edff);box-shadow:0 18px 60px #6451b34d;color:#6655bd;font-weight:800}.voice-preview-wave{height:48px;display:flex;align-items:center;justify-content:center;gap:4px}.voice-preview-wave i{width:5px;height:7px;border-radius:5px;background:linear-gradient(#69baff,#8a64ed);opacity:.65}.voice-preview-wave-panel.is-playing .voice-preview-wave i{height:var(--bar-height);opacity:1;animation:voice-audition-wave .85s ease-in-out infinite alternate;animation-delay:var(--bar-delay)}.voice-preview-wave-panel button{border:1px solid #ded7f8;border-radius:10px;background:#fff;color:#7865bf;padding:7px 25px;font:inherit;cursor:pointer}.model-picker small{color:#9a7b39;font-size:11px;line-height:1.6}@keyframes voice-audition-wave{from{transform:scaleY(.35)}to{transform:scaleY(1)}}@media(prefers-reduced-motion:reduce){.voice-preview-wave-panel.is-playing .voice-preview-wave i{animation:none}}
</style>
