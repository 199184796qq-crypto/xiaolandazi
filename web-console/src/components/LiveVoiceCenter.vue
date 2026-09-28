<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  activateLiveAgentConfigVersion,
  cloneLiveVoiceProfile,
  createLiveAgentConfigDraft,
  getLiveAgentConfigVersions,
  getLiveOfficialVoices,
  getLiveVoiceProfiles,
  previewLiveOfficialVoice,
  previewLiveVoiceProfile,
  uploadLiveMediaAsset,
} from '../api'
import type { LiveAgentConfigVersion, OfficialVoice, VoiceProfile } from '../types'

const props = defineProps<{ roomId: number | null; roomName?: string }>()
const emit = defineEmits<{ changed: [] }>()

const officialVoices = ref<OfficialVoice[]>([])
const voiceProfiles = ref<VoiceProfile[]>([])
const versions = ref<LiveAgentConfigVersion[]>([])
const error = ref('')
const loading = ref(false)
const selecting = ref(false)
const catalogOpen = ref(false)
const cloneOpen = ref(false)
const cloneName = ref('')
const sampleFile = ref<File | null>(null)
const sampleInput = ref<HTMLInputElement | null>(null)
const previewingKey = ref('')
let previewAudio: HTMLAudioElement | null = null

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

function selectedName() {
  return String(selectedVoice.value.name || '').trim() || '龙安灵心'
}

function selectedSource() {
  if (selectedVoice.value.source === 'clone') return '我的声音'
  if (selectedVoice.value.source === 'official') return '阿里官方声音'
  return '系统默认官方声音'
}

async function load() {
  if (!props.roomId) {
    officialVoices.value = []
    voiceProfiles.value = []
    versions.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const [official, profiles, configVersions] = await Promise.all([
      getLiveOfficialVoices(),
      getLiveVoiceProfiles(),
      getLiveAgentConfigVersions(),
    ])
    officialVoices.value = official.items || []
    voiceProfiles.value = profiles || []
    versions.value = configVersions || []
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取声音中心失败'
  } finally {
    loading.value = false
  }
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
  try {
    const result = await factory()
    const audio = new Audio(result.audio_url)
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

async function bindVoice(voice: Record<string, unknown>) {
  if (!props.roomId || selecting.value) return
  selecting.value = true
  error.value = ''
  try {
    const draft = await createLiveAgentConfigDraft({
      speech_config: updatedSpeechConfig({
        ...voice,
        selected_at: new Date().toISOString(),
      }),
    })
    await activateLiveAgentConfigVersion(draft.id)
    catalogOpen.value = false
    await load()
    emit('changed')
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存直播间声音失败'
  } finally {
    selecting.value = false
  }
}

async function chooseOfficial(item: OfficialVoice) {
  await bindVoice({
    source: 'official',
    provider: 'aliyun_qwen',
    name: item.name,
    voice_id: item.id,
    model: item.model,
  })
}

async function chooseProfile(item: VoiceProfile) {
  if (item.clone_status !== 'ready') return
  await bindVoice({
    source: 'clone',
    provider: item.provider,
    name: item.name,
    voice_id: item.voice_id,
    profile_id: item.id,
    model: String(item.config?.target_model || 'qwen3-tts-vc-2026-01-22'),
  })
}

function chooseSample(event: Event) {
  const target = event.target as HTMLInputElement
  sampleFile.value = target.files?.[0] || null
}

async function submitClone() {
  if (!cloneName.value.trim() || !sampleFile.value || selecting.value) return
  selecting.value = true
  error.value = ''
  try {
    const uploaded = await uploadLiveMediaAsset(sampleFile.value, 'voice_sample', {
      metadata: { source: 'voice_center_clone' },
    })
    const profile = await cloneLiveVoiceProfile({
      name: cloneName.value.trim(),
      sample_asset_id: uploaded.asset.id,
    })
    voiceProfiles.value = [profile, ...voiceProfiles.value.filter((item) => item.id !== profile.id)]
    cloneName.value = ''
    sampleFile.value = null
    if (sampleInput.value) sampleInput.value.value = ''
    cloneOpen.value = false
  } catch (err) {
    error.value = err instanceof Error ? err.message : '声音复刻失败'
  } finally {
    selecting.value = false
  }
}

function voiceStatus(item: VoiceProfile) {
  if (item.clone_status === 'ready') return '可使用'
  if (item.clone_status === 'failed') return '克隆失败'
  if (item.clone_status === 'training') return '克隆中'
  return '处理中'
}

watch(() => props.roomId, () => void load())
onMounted(() => void load())
onBeforeUnmount(stopPreview)
</script>

<template>
  <section class="voice-center">
    <div class="voice-current-card">
      <div>
        <small>{{ roomName || '当前直播间' }}</small>
        <h3>{{ selectedName() }}</h3>
        <p>{{ selectedSource() }}</p>
      </div>
      <div class="voice-current-actions">
        <button type="button" class="primary-button" @click="catalogOpen = true">选择官方声音</button>
        <button type="button" @click="cloneOpen = true">＋ 克隆新声音</button>
      </div>
    </div>

    <p v-if="error" class="inline-error">{{ error }}</p>

    <section class="voice-library-section">
      <div class="voice-section-head">
        <div>
          <h3>我的声音</h3>
        </div>
        <button type="button" @click="cloneOpen = true">＋ 克隆新声音</button>
      </div>

      <div v-if="voiceProfiles.length" class="voice-card-grid">
        <article v-for="item in voiceProfiles" :key="item.id" class="voice-card">
          <div class="voice-card-icon">声</div>
          <div class="voice-card-copy">
            <strong>{{ item.name }}</strong>
            <small>{{ voiceStatus(item) }} · 可供所有直播间使用</small>
          </div>
          <div class="voice-card-actions">
            <button
              type="button"
              :disabled="item.clone_status !== 'ready'"
              @click="playURL('profile-' + item.id, () => previewLiveVoiceProfile(item.id))"
            >
              {{ previewingKey === 'profile-' + item.id ? '停止' : '试听' }}
            </button>
            <button
              type="button"
              class="primary-button"
              :disabled="item.clone_status !== 'ready' || selecting"
              @click="chooseProfile(item)"
            >
              用于当前直播间
            </button>
          </div>
        </article>
      </div>
      <div v-else-if="!loading" class="voice-empty">还没有克隆声音</div>
    </section>

    <Teleport to="body">
      <div v-if="catalogOpen" class="voice-dialog-overlay" @click.self="catalogOpen = false">
        <section class="voice-dialog" role="dialog" aria-modal="true" aria-label="选择官方声音">
          <header>
            <div>
              <span class="section-kicker">ALIYUN QWEN · 声音</span>
              <h3>选择官方声音</h3>
            </div>
            <button type="button" class="voice-close" @click="catalogOpen = false">×</button>
          </header>

          <div class="official-voice-grid">
            <article v-for="item in officialVoices" :key="item.id" class="official-voice-card">
              <div class="official-voice-title">
                <div class="voice-card-icon">{{ item.gender === '男声' ? '男' : '女' }}</div>
                <div>
                  <strong>{{ item.name }}</strong>
                  <small>{{ item.gender }} · {{ item.id }}</small>
                </div>
              </div>
              <p>{{ item.description }}</p>
              <div class="voice-tags">
                <span v-for="tag in item.tags" :key="tag">{{ tag }}</span>
              </div>
              <footer>
                <button type="button" @click="playURL('official-' + item.id, () => previewLiveOfficialVoice(item.id))">
                  {{ previewingKey === 'official-' + item.id ? '停止试听' : '试听' }}
                </button>
                <button type="button" class="primary-button" :disabled="selecting" @click="chooseOfficial(item)">
                  选择这个声音
                </button>
              </footer>
            </article>
          </div>
        </section>
      </div>

      <div v-if="cloneOpen" class="voice-dialog-overlay" @click.self="cloneOpen = false">
        <section class="voice-dialog voice-clone-dialog" role="dialog" aria-modal="true" aria-label="克隆新声音">
          <header>
            <div>
              <span class="section-kicker">VOICE CLONE</span>
              <h3>克隆新声音</h3>
            </div>
            <button type="button" class="voice-close" @click="cloneOpen = false">×</button>
          </header>

          <label class="voice-clone-field">
            <span>声音名称</span>
            <input v-model="cloneName" maxlength="60" placeholder="例如：小张、门店主播" />
          </label>
          <label class="voice-clone-upload">
            <input ref="sampleInput" type="file" accept="audio/*" @change="chooseSample" />
            <strong>{{ sampleFile ? sampleFile.name : '选择声音样本' }}</strong>
            <small>建议使用清晰、单人、无背景音乐的录音。</small>
          </label>
          <footer class="voice-clone-actions">
            <button type="button" @click="cloneOpen = false">取消</button>
            <button
              type="button"
              class="primary-button"
              :disabled="!cloneName.trim() || !sampleFile || selecting"
              @click="submitClone"
            >
              {{ selecting ? '克隆中…' : '开始克隆' }}
            </button>
          </footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.voice-center{display:grid;gap:18px;padding:4px 0 28px}
.voice-current-card{display:flex;align-items:center;justify-content:space-between;gap:20px;padding:22px 24px;border:1px solid #dfe5f4;border-radius:20px;background:linear-gradient(135deg,#f7f9ff,#fff)}
.voice-current-card>div:first-child{display:grid;gap:2px}
.voice-current-card small{color:#7f8ba1}
.voice-current-card h3{margin:4px 0 0;font-size:28px;color:#18243b}
.voice-current-card p{margin:0;color:#68758d}
.voice-current-actions,.voice-card-actions{display:flex;gap:9px;flex-wrap:wrap}
.voice-current-actions button,.voice-section-head button,.voice-card-actions button,.official-voice-card footer button,.voice-clone-actions button{min-height:40px;border:1px solid #d5ddef;border-radius:11px;padding:8px 13px;background:#fff;color:#3f506d;font-weight:800;cursor:pointer}
.primary-button{background:#5666d9!important;color:#fff!important;border-color:#5666d9!important}
.voice-library-section{padding:20px;border:1px solid #e2e7f1;border-radius:20px;background:#fff}
.voice-section-head{display:flex;align-items:center;justify-content:space-between;gap:12px;margin-bottom:14px}
.voice-section-head h3{margin:2px 0 0;font-size:22px}
.voice-card-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(310px,1fr));gap:12px}
.voice-card{display:grid;grid-template-columns:auto 1fr;gap:12px;padding:15px;border:1px solid #e1e6f0;border-radius:16px;background:#fafbff}
.voice-card-icon{width:46px;height:46px;border-radius:14px;display:grid;place-items:center;background:#edf0ff;color:#5263d7;font-weight:900}
.voice-card-copy{display:grid;align-content:start}
.voice-card-copy strong{font-size:17px;color:#1e2a42}
.voice-card-copy small{color:#7f8a9d;font-size:12px}
.voice-card-actions{grid-column:1/-1;justify-content:flex-end}
.voice-empty{padding:24px;text-align:center;color:#8a95a8;background:#fafbfe;border-radius:14px}
.voice-dialog-overlay{position:fixed;inset:0;z-index:4400;background:#18243c66;display:grid;place-items:center;padding:24px}
.voice-dialog{width:min(1000px,96vw);max-height:90vh;overflow:auto;border-radius:24px;background:#f8faff;padding:24px;box-shadow:0 30px 90px #14223c4d}
.voice-dialog>header{display:flex;align-items:center;justify-content:space-between;gap:16px}
.voice-dialog h3{margin:2px 0 0;font-size:27px;color:#1c2941}
.voice-close{border:0!important;background:transparent!important;font-size:30px!important;color:#7c879b!important;cursor:pointer}
.official-voice-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(270px,1fr));gap:14px;margin-top:20px}
.official-voice-card{display:grid;gap:12px;padding:17px;border:1px solid #dfe5f1;border-radius:18px;background:#fff}
.official-voice-title{display:flex;align-items:center;gap:12px}
.official-voice-title>div:last-child{display:grid}
.official-voice-title strong{font-size:19px}
.official-voice-title small{color:#8a95a8}
.official-voice-card p{margin:0;color:#5f6d84}
.voice-tags{display:flex;gap:6px;flex-wrap:wrap}
.voice-tags span{padding:3px 8px;border-radius:999px;background:#f0f3ff;color:#5262cb;font-size:11px;font-weight:700}
.official-voice-card footer{display:flex;justify-content:flex-end;gap:8px}
.voice-clone-dialog{width:min(560px,94vw)}
.voice-clone-field{display:grid;gap:7px;margin-top:22px;font-weight:800;color:#34425b}
.voice-clone-field input{min-height:44px;border:1px solid #cfd8ea;border-radius:11px;padding:9px 12px;font:inherit}
.voice-clone-upload{display:grid;gap:6px;margin-top:16px;padding:20px;border:1px dashed #bfcbe3;border-radius:16px;background:#fff;cursor:pointer}
.voice-clone-upload input{max-width:100%}
.voice-clone-upload small{color:#8792a5}
.voice-clone-actions{display:flex;justify-content:flex-end;gap:8px;margin-top:18px}
@media(max-width:720px){.voice-current-card{align-items:flex-start;flex-direction:column}.voice-card-grid,.official-voice-grid{grid-template-columns:1fr}}
</style>
