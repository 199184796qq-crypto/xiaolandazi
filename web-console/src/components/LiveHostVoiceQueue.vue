<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { getLiveHostVoice, setLiveHostVoice, type LiveHostVoiceSnapshot } from '../api'

const props = defineProps<{ roomId: number; sessionId: number; canChange: boolean }>()
const snapshot = ref<LiveHostVoiceSnapshot | null>(null)
const error = ref('')
const busy = ref(false)
let generation = 0
let disposed = false
let timer: ReturnType<typeof setTimeout> | undefined
const key = (v: { model: string; voice_id: string }) => JSON.stringify([v.model, v.voice_id])
const selected = computed(() => snapshot.value ? key(snapshot.value.state.requested || snapshot.value.state.pending || snapshot.value.state.active) : '')
const currentName = computed(() => snapshot.value?.voices.find(v => key(v) === key(snapshot.value!.state.active))?.name || '准备中')
const status = computed(() => snapshot.value?.state.pending ? '待当前话术结束后上线，互动同步切换' : snapshot.value?.state.requested ? '正在准备所选主播声音，继续使用当前主播' : '')

async function load(epoch: number) {
  try {
    const value = await getLiveHostVoice(props.roomId)
    if (disposed || epoch !== generation || value.state.session_id !== props.sessionId) return
    if (!snapshot.value || value.state.revision >= snapshot.value.state.revision) snapshot.value = value
    error.value = ''
  } catch (e) {
    if (!disposed && epoch === generation) error.value = e instanceof Error ? e.message : '读取主播声音失败'
  } finally {
    if (!disposed && epoch === generation) timer = setTimeout(() => void load(epoch), 3000)
  }
}
async function change(event: Event) {
  const voice = snapshot.value?.voices.find(v => key(v) === (event.target as HTMLSelectElement).value)
  if (!voice || !snapshot.value || busy.value || !props.canChange) return
  const epoch = generation
  busy.value = true
  try {
    const value = await setLiveHostVoice(props.roomId, { session_id: props.sessionId, version_id: snapshot.value.state.version_id, voice_id: voice.voice_id, model: voice.model })
    if (!disposed && epoch === generation && value.state.session_id === props.sessionId) {
      if (!snapshot.value || value.state.revision >= snapshot.value.state.revision) snapshot.value = value
      error.value = ''
    }
  } catch (e) {
    if (!disposed && epoch === generation) error.value = e instanceof Error ? e.message : '切换主播声音失败'
  } finally { if (epoch === generation) busy.value = false }
}
watch(() => [props.roomId, props.sessionId], () => {
  generation++; clearTimeout(timer); snapshot.value = null; error.value = ''; busy.value = false
  if (props.sessionId > 0) void load(generation)
}, { immediate: true })
onBeforeUnmount(() => { disposed = true; generation++; clearTimeout(timer) })
</script>

<template>
  <div class="live-host-voice-queue">
    <span>当前上线主播音色：<strong>{{ currentName }}</strong></span>
    <label v-if="snapshot && snapshot.voices.length > 1">
      <span>主播音色队列</span>
      <select aria-label="切换上线主播音色" :value="selected" :disabled="busy || !canChange" @change="change">
        <option v-for="voice in snapshot.voices" :key="key(voice)" :value="key(voice)">{{ voice.name }}</option>
      </select>
    </label>
    <small role="status">{{ busy ? '提交切换中…' : status }}</small>
    <small v-if="error" class="error" role="alert">{{ error }}</small>
  </div>
</template>

<style scoped>
.live-host-voice-queue { display:flex; align-items:center; flex-wrap:wrap; gap:10px; font-size:16px; }
label { display:flex; align-items:center; gap:8px; }
select { max-width:220px; padding:8px 12px; border:1px solid #dce0f5; border-radius:10px; background:white; color:#38445e; font-size:16px; }
small { color:#6c7692; }
.error { color:#d93348; }
</style>
