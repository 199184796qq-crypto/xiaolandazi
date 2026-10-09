<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { request } from '../api'
import type { CycleState, CycleResponse } from '../anchorCycleRun'
import { normalizeCycleState } from '../anchorCycleRun'
import { liveSupportRoomForRequest } from '../liveSupportAccess'
import { ContinuousPreviewPCMStream, readPreviewPCM, type PreviewPCMFrame } from '../continuousPreviewPCMStream'
import RealtimeAudioPlayer from './RealtimeAudioPlayer.vue'
import { preparationProgress, playbackPercent, type PreparationPart } from '../realtimePlayerProgress'
import type { PreviewStreamEvent } from '../agent/previewStream'
import { realtimeGenerationOptionsError, validatedRealtimeOptions, type RealtimeGenerationOptions } from '../realtimeGenerationOptions'
const props = defineProps<{ planId: number; tenantId?: number; voiceSourceRoomId?: number; generationOptions: RealtimeGenerationOptions | null }>()
const run = ref<(CycleState & { preview_limit_ms?: number }) | null>(null), error = ref(''), notice = ref('')
const starting = ref(false), restoring = ref(false), playing = ref(false), stopped = ref(true), previewFinished = ref(false)
const prepared = ref(0), firstSound = ref(false), elapsedMS = ref(0), subtitle = ref(''), readyMS = ref(0)
const buffered = computed(() => ({ readyMS: readyMS.value }))
const progress = computed(() => previewFinished.value ? 100 : firstSound.value ? playbackPercent(elapsedMS.value, run.value?.preview_limit_ms || elapsedMS.value + readyMS.value) : prepared.value)
const caption = computed(() => previewFinished.value ? '本场试听已播放完成' : subtitle.value || '当前播放的实时文字将在这里显示')
let alive = true, epoch = 0, ticker: number | undefined
let controller: AbortController | null = null, streamTask: Promise<void> | null = null
let pendingControl: Promise<unknown> = Promise.resolve(), reporting = false
let otherReceiver = false
let audio = new ContinuousPreviewPCMStream(limitReached)
function url(suffix = '', plan = props.planId, tenant = props.tenantId) {
  return '/api/v1/live-agent-plans/' + plan + '/realtime-preview' + suffix + (tenant ? '?tenant_id=' + tenant : '')
}
function progressEvent(generation: number, part: PreparationPart) {
  return (event: PreviewStreamEvent) => {
    if (alive && generation === epoch && !firstSound.value && event.type === 'progress' && event.percent !== undefined) prepared.value = preparationProgress(prepared.value, part, event.percent)
  }
}
function receive(state: CycleState) {
  run.value = normalizeCycleState(state)
  audio.setPlaybackLimit(run.value.preview_limit_ms || 0)
}
async function restore(generation: number) {
  if (!props.planId) return
  const result = await request<CycleResponse>(url(), { cache: 'no-store', suppressErrorDialog: true })
  if (!alive || generation !== epoch) return
  stopped.value = true; playing.value = false
  if (!result.run) {
    run.value = null; audio.restorePlaybackOffset(0); elapsedMS.value = 0
    firstSound.value = false; previewFinished.value = false; otherReceiver = false
    error.value = ''; notice.value = ''; return
  }
  receive(result.run)
  const playback = result.playback
  const played = Math.max(0, Number(playback?.played_ms) || 0)
  audio.restorePlaybackOffset(played)
  elapsedMS.value = audio.snapshot().playedMS; firstSound.value = played > 0
  previewFinished.value = !!playback?.finished || !!run.value?.preview_limit_ms && played >= run.value.preview_limit_ms
  prepared.value = Math.max(0, Math.min(99, Number(playback?.progress) || 0))
  otherReceiver = !!playback?.connected
  error.value = playback?.error || ''
  notice.value = previewFinished.value ? '' : '已读取上次试听，点击播放继续；点击结束可重新开始。'
}
async function loadScope(prior?: readonly [number, number | undefined]) {
  const cleanup = prior ? reset(prior[0], prior[1]) : Promise.resolve()
  const generation = epoch
  restoring.value = true; error.value = ''; notice.value = ''
  try { await cleanup; if (alive && generation === epoch) await restore(generation) }
  catch (e) { if (alive && generation === epoch) error.value = e instanceof Error ? e.message : '读取试听进度失败，请点击播放重试' }
  finally { if (generation === epoch) restoring.value = false }
}
function sync() {
  const snapshot = audio.snapshot()
  elapsedMS.value = previewFinished.value && run.value?.preview_limit_ms ? run.value.preview_limit_ms : snapshot.playedMS
  playing.value = !stopped.value && snapshot.playing
  if (snapshot.playing) { firstSound.value = true; subtitle.value = snapshot.caption }
}
function control(action: string, played = audio.snapshot().playedMS) {
  const id = run.value?.run_id, target = id ? url('/' + id + '/playback') : ''
  if (!target) return Promise.resolve()
  const task = pendingControl.catch(() => undefined).then(() => request(target, { method: 'POST', body: JSON.stringify({ action, played_ms: played }), suppressErrorDialog: action === 'progress' }))
  pendingControl = task
  return task
}
function tick() {
  sync()
  if (run.value && !stopped.value && !reporting) {
    reporting = true
    void control('progress').catch(e => { if (alive && !stopped.value) fail(e) }).finally(() => { reporting = false })
  }
}
function fail(value: unknown, pauseServer = true) {
  error.value = value instanceof Error ? value.message : '声音输出暂停'
  stopped.value = true; playing.value = false
  const played = audio.pause(); elapsedMS.value = played; controller?.abort()
  if (pauseServer) void control('pause', played).catch(() => undefined)
}
function limitReached() {
  sync(); previewFinished.value = true; stopped.value = true; playing.value = false
  elapsedMS.value = run.value?.preview_limit_ms || elapsedMS.value; error.value = ''; notice.value = ''
  controller?.abort()
  // The last scheduled source ends at the exact sample. Cancel the server's
  // output and both producers; completed assets remain available until reset.
  void control('limit', elapsedMS.value).catch(e => { if (alive) error.value = e instanceof Error ? e.message : '后台停止确认失败' })
}
async function connect(generation: number) {
  if (!run.value || !alive || generation !== epoch || stopped.value) return
  const target = url('/' + run.value.run_id + '/stream'), receiver = new AbortController()
  controller = receiver
  const headers = new Headers({ Accept: 'application/x-ndjson' })
  const supportRoom = liveSupportRoomForRequest(target)
  if (supportRoom) headers.set('X-Live-Support-Room-ID', String(supportRoom))
  streamTask = (async () => {
    const response = await fetch(target, { credentials: 'include', cache: 'no-store', signal: receiver.signal, headers })
    await readPreviewPCM(response, receiver.signal, packet => {
      if (!alive || generation !== epoch || stopped.value || receiver.signal.aborted) return
      if (packet.type === 'state') {
        if (packet.run) receive(packet.run as unknown as CycleState)
        readyMS.value = Math.max(0, Number(packet.ready_ms) || 0)
        if (packet.part === 'text' || packet.part === 'voice') progressEvent(generation, packet.part)({ type: 'progress', message: '', percent: Number(packet.progress) || 0 })
      } else if (packet.type === 'audio') audio.append(packet as unknown as PreviewPCMFrame)
      else if (packet.type === 'error') throw new Error(String(packet.message || '声音输出暂停'))
    })
  })().catch(e => { if (!receiver.signal.aborted && alive && generation === epoch) fail(e) })
  if (ticker === undefined) ticker = window.setInterval(tick, 250)
}
async function start() {
  if (!props.planId || starting.value || restoring.value) return
  if (run.value && !stopped.value) { await audio.resume(); return }
  starting.value = true; notice.value = ''
  const generation = epoch
  try {
    await audio.resume() // Unlock output from the user's click, before network work.
    await pendingControl.catch(() => undefined)
    await streamTask
    if (!alive || generation !== epoch) return
    await restore(generation)
    if (!alive || generation !== epoch) return
    if (run.value && !previewFinished.value) {
      if (otherReceiver) throw new Error('本场正在另一个页面播放，请先暂停原页面，或点击结束试听。')
      if (error.value) throw new Error('上次试听已暂停：' + error.value + '。请点击结束试听后重新开始。')
      if (run.value.stream_controlled !== true) throw new Error('上次试听需要重新开启，请先点击结束试听。')
      notice.value = ''
      await control('resume'); stopped.value = false; await connect(generation); return
    }
    const options = validatedRealtimeOptions(props.generationOptions)
    if (run.value) await request(url('/' + run.value.run_id), { method: 'DELETE' })
    if (!alive || generation !== epoch) return
    audio.stop(); audio = new ContinuousPreviewPCMStream(limitReached); await audio.resume()
    run.value = null; elapsedMS.value = 0; prepared.value = 0; firstSound.value = false; subtitle.value = ''; readyMS.value = 0; previewFinished.value = false; error.value = ''
    stopped.value = false
    const createURL = url()
    const result = await request<CycleResponse>(createURL, { method: 'POST', headers: { Accept: 'text/event-stream' }, body: JSON.stringify({ stream_version: 1, voice_source_room_id: props.voiceSourceRoomId || 0, generation_options: options }), suppressErrorDialog: true }, progressEvent(generation, 'session'))
    if (!alive || generation !== epoch) {
      if (result.run) {
        const [path, query] = createURL.split('?')
        await request(path + '/' + result.run.run_id + (query ? '?' + query : ''), { method: 'DELETE' })
      }
      return
    }
    if (!result.run) throw new Error('试听场次未创建')
    receive(result.run); prepared.value = preparationProgress(prepared.value, 'session', 100)
    if (!stopped.value) await connect(generation)
  } catch (e) {
    if (alive && generation === epoch) {
      if (e instanceof Error && e.message.includes('已有本场大循环')) {
        try { await restore(generation); if (run.value) return }
        catch (restoreError) { e = restoreError }
      }
      if (alive && generation === epoch) fail(e, !otherReceiver)
    }
  }
  finally { if (generation === epoch) starting.value = false }
}
function pause() {
  stopped.value = true; playing.value = false
  const played = audio.pause(); elapsedMS.value = played
  controller?.abort()
  void control('pause', played).catch(e => { if (alive) error.value = e instanceof Error ? e.message : '暂停确认失败' })
}
async function reset(oldPlan = props.planId, oldTenant = props.tenantId) {
  stopped.value = true; playing.value = false; ++epoch; starting.value = false
  const id = run.value?.run_id, played = audio.pause()
  controller?.abort(); audio.stop(); run.value = null
  elapsedMS.value = 0; firstSound.value = false; prepared.value = 0; previewFinished.value = false; readyMS.value = 0; subtitle.value = ''
  error.value = ''; notice.value = ''; otherReceiver = false
  if (ticker !== undefined) { window.clearInterval(ticker); ticker = undefined }
  if (id && oldPlan) {
    try {
      await pendingControl.catch(() => undefined)
      await request(url('/' + id + '/playback', oldPlan, oldTenant), { method: 'POST', body: JSON.stringify({ action: 'stop', played_ms: played }) })
      await request(url('/' + id, oldPlan, oldTenant), { method: 'DELETE' })
    } catch { /* An uncertain stop is visible through the request dialog; do not repeat paid work. */ }
  }
}
watch(() => [props.planId, props.tenantId] as const, (_, prior) => { void loadScope(prior) }, { immediate: true })
onBeforeUnmount(() => { alive = false; void reset() })
</script>

<template>
  <section class="realtime-anchor-preview">
    <div class="playback-caption" aria-live="polite">{{ caption }}</div>
    <RealtimeAudioPlayer :preparing="!firstSound && (starting || !!run || !!error)" :playing="playing" :active="(starting || !!run) && !stopped" :progress="progress" :elapsed-m-s="elapsedMS" :ready-seconds="Math.floor(buffered.readyMS / 1000)" :unlimited="!!run && !run.preview_limit_ms" :failed="!!error" :start-disabled="starting || restoring || !planId || (!run && !!realtimeGenerationOptionsError(generationOptions))" :stop-disabled="!run && !starting" @play="start" @pause="pause" @stop="reset()" />
    <p v-if="notice">{{ notice }}</p><p v-if="error" class="error">{{ error }}</p>
    <slot />
  </section>
</template>
<style scoped>
.realtime-anchor-preview{display:grid;gap:16px}.playback-caption{display:flex;align-items:center;justify-content:center;text-align:center;min-height:100px;background:#661c2c;color:white;border-radius:14px;padding:24px;font-size:24px;line-height:1.7;overflow-wrap:anywhere}.error{color:#ba2542}p{margin:0;color:#62738b}
.realtime-anchor-preview :deep(.audio-player){width:50%;box-sizing:border-box;zoom:.5;justify-self:center}
</style>
