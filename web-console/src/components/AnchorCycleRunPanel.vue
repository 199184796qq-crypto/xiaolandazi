<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { advanceAnchorCycle, endAnchorCycle, getAnchorCycle, startAnchorCycle, synthesizeAnchorCycleVoice, getAnchorCycleVoice, getLiveOfficialVoices, getLiveVoiceProfiles } from '../api'
import { cycleProgress, cycleSpeech, runCycleLoop } from '../anchorCycleRun'
import type { CycleConfig, CycleState, CycleVoiceAsset } from '../anchorCycleRun'
import { pendingVoiceSegment, productionBuffer, mergeVoiceResponse, runVoiceQueue } from '../anchorCycleAudio'
import type { BufferedVoice } from '../anchorCycleAudio'
import type { OfficialVoice, VoiceProfile } from '../types'
import OpeningHookSettingsPanel from './OpeningHookSettingsPanel.vue'
import RiskText from './RiskText.vue'
import { liveSupportMediaPlaybackURL } from '../liveSupportAccess'

const props = defineProps<{ planId: number; ready: boolean; config: Omit<CycleConfig, 'threshold' | 'max_retries' | 'max_chars' | 'point_gap' | 'repair_punctuation' | 'rolling' | 'body_segments' | 'combination_gap' | 'max_cycles'>; roomIds: number[]; advanced: boolean }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const run = ref<CycleState | null>(null)
const roomId = ref(0)
const threshold = ref(40), retries = ref(3), maxChars = ref(300), taskLimit = ref(0)
const repairPunctuation = ref(true)
const pointGap = ref(2)
const rolling = ref(true), bodyMin = ref(2), bodyMax = ref(6), combinationGap = ref(2), maxCycles = ref(2)
const ttsEnabled = ref(false), selectedVoice = ref(''), voiceRate = ref(1), bufferSeconds = ref(120)
const officialVoices = ref<OfficialVoice[]>([]), clonedVoices = ref<VoiceProfile[]>([]), voiceLoading = ref(false), voiceError = ref('')
const voicePlayer = ref<HTMLAudioElement | null>(null), voiceQueue = ref<BufferedVoice[]>([]), playedSeconds = ref(0)
const synthesizing = ref(false), bufferWaiting = ref(false), playbackBlocked = ref(false)
const voiceErrorMessage = ref(''), voicePaused = ref(true)
const consumedVoices = ref(new Set<string>())
const cachedVoices = new Map<string, CycleVoiceAsset>()
const audioBuffer = computed(() => productionBuffer(run.value, consumedVoices.value, voiceQueue.value[0]?.id || '', playedSeconds.value))
const pendingAudioSeconds = computed(() => Math.ceil(audioBuffer.value.totalMS / 1000))
const activeBufferSeconds = computed(() => run.value?.config.tts?.buffer_seconds || bufferSeconds.value)
const seenVoices = new Set<string>()
const showTTS = computed(() => !!run.value?.config.tts || ttsEnabled.value)
const latestVoice = computed(() => run.value?.accepted.filter(item => item.voice?.status === 'ready').at(-1)?.voice)
const running = ref(false), creating = ref(false), ending = ref(false), loading = ref(false), stopped = ref(false)
const error = ref(''), notice = ref(''), retainedText = ref('')
const speech = computed(() => run.value ? cycleSpeech(run.value) : retainedText.value)
const progress = computed(() => run.value ? cycleProgress(run.value) : null)
const riskDrafts = computed(() => {
  const state = run.value
  if (!state) return []
  const drafts = [...state.accepted.flatMap(segment => segment.attempts), ...state.attempts, ...(state.p0_skipped || []).flatMap(skip => skip.attempts), ...(state.p0_audit_pending ? [state.p0_audit_pending] : [])]
  return [...new Map(drafts.filter(draft => draft.p0_report && !draft.p0_report.passed).map(draft => [draft.id, draft])).values()].slice(-20)
})
const requestStartedAt = ref(0), currentTime = ref(Date.now())
const requestWaitSeconds = computed(() => requestStartedAt.value ? Math.max(0, Math.floor((currentTime.value - requestStartedAt.value) / 1000)) : 0)
const timer = setInterval(() => { if (requestStartedAt.value) currentTime.value = Date.now() }, 1000)
const stages = [{ phase: 'opening', title: '开场' }, { phase: 'body', title: '中间讲解' }, { phase: 'closing', title: '收口' }] as const
const phases = { opening: '开场', body: '中间', closing: '收口', ended: '本轮完成' }
let active = true, epoch = 0
async function loadVoices() {
  voiceLoading.value = true; voiceError.value = ''
  try {
    const result = await Promise.allSettled([getLiveOfficialVoices(), getLiveVoiceProfiles()])
    if (result[0].status === 'fulfilled') officialVoices.value = result[0].value.items
    if (result[1].status === 'fulfilled') clonedVoices.value = result[1].value.filter(item => item.clone_status === 'ready' && item.voice_id)
    if (result.every(item => item.status === 'rejected')) voiceError.value = '音色加载失败，请重试。'
    if (!selectedVoice.value && officialVoices.value[0]) selectedVoice.value = 'official:' + officialVoices.value[0].id
  } finally { voiceLoading.value = false }
}
function clearAudio() {
  voicePaused.value = true; voicePlayer.value?.pause(); voiceQueue.value = []; playedSeconds.value = 0; seenVoices.clear(); consumedVoices.value = new Set(); cachedVoices.clear(); playbackBlocked.value = false; voiceErrorMessage.value = ''
  if (voicePlayer.value) { voicePlayer.value.removeAttribute('src'); voicePlayer.value.load() }
}
async function playBufferedVoice() {
  const item = voiceQueue.value[0], player = voicePlayer.value
  if (!item || !player) return
  if (player.getAttribute('src') !== item.url) { player.src = item.url; playedSeconds.value = 0 }
  try { await player.play(); playbackBlocked.value = false }
  catch { playbackBlocked.value = true; notice.value = '浏览器阻止自动播放。可先点击播放缓存声音；文字预生成仍受缓存总上限约束。' }
}
function audioEnded() {
  const item = voiceQueue.value.shift(); if (item) consumedVoices.value.add(item.id); playedSeconds.value = 0
  if (voiceQueue.value.length) void playBufferedVoice()
}
function audioFailed() {
  playbackBlocked.value = true; notice.value = '声音播放失败，请核对音频；文字只会预生成到缓存上限，不会无限积累。'
}
function enqueueReadyVoices() {
  if (!run.value) return
  const wasEmpty = voiceQueue.value.length === 0
  for (const segment of run.value.accepted) {
    if (consumedVoices.value.has(segment.selected.id)) continue
    if (segment.voice?.status !== 'ready') break // Never skip an unresolved earlier clip.
    if (seenVoices.has(segment.selected.id)) continue
    seenVoices.add(segment.selected.id)
    voiceQueue.value.push({ id: segment.selected.id, url: liveSupportMediaPlaybackURL(segment.voice.bundle.audio_url), durationMS: segment.voice.bundle.audio_duration_ms })
  }
  if (wasEmpty && voiceQueue.value.length) void nextTick().then(playBufferedVoice)
}
let voiceWorkerEpoch: number | null = null
function startVoiceWorker(retry = false) {
  if (!run.value?.config.tts || !active || ending.value || voiceWorkerEpoch !== null || voicePaused.value || (voiceErrorMessage.value && !retry)) return
  const generation = epoch, plan = props.planId, room = roomId.value
  voiceWorkerEpoch = generation; synthesizing.value = true
  if (retry) voiceErrorMessage.value = ''
  let allowRetry = retry
  void runVoiceQueue({
    state: () => run.value,
    stopped: () => !active || generation !== epoch || voicePaused.value,
    synthesize: async (state, id) => { const retryThis = allowRetry; allowRetry = false; return synthesizeAnchorCycleVoice(plan, room, state.run_id, id, retryThis) },
    inspect: (state, id) => getAnchorCycleVoice(plan, room, state.run_id, id),
    wait: () => new Promise(resolve => setTimeout(resolve, 2000)),
    receive: result => {
      if (!active || generation !== epoch || result.run_id !== run.value?.run_id) return
      if (result.voice) cachedVoices.set(result.candidate_id, result.voice)
      if (run.value) run.value = mergeVoiceResponse(run.value, result)
      enqueueReadyVoices()
    },
  }).catch(value => {
    if (active && generation === epoch) { voiceErrorMessage.value = value instanceof Error ? value.message : 'TTS失败'; voicePaused.value = true }
  }).finally(() => {
    if (voiceWorkerEpoch === generation) { voiceWorkerEpoch = null; synthesizing.value = false }
    if (active && voiceWorkerEpoch === null && !voicePaused.value && !voiceErrorMessage.value && run.value && pendingVoiceSegment(run.value)) startVoiceWorker()
  })
}
function retryVoiceQueue() {
  voicePaused.value = false
  startVoiceWorker(true)
}
function pauseProduction() { stopped.value = true; voicePaused.value = true }
async function waitForAudioCapacity(generation: number) {
  if (!run.value?.config.tts) return
  bufferWaiting.value = true
  try {
    while (active && generation === epoch && !stopped.value && audioBuffer.value.totalMS >= activeBufferSeconds.value * 1000) {
      await new Promise(resolve => setTimeout(resolve, 200))
    }
  } finally { bufferWaiting.value = false }
}
function update(value: CycleState | null) {
  if (value) value = { ...value, accepted: value.accepted.map(segment => {
    const cached = cachedVoices.get(segment.selected.id)
    if (cached && cached.status === 'ready') return { ...segment, voice: cached }
    if (segment.voice) cachedVoices.set(segment.selected.id, segment.voice)
    return segment
  }) }
  run.value = value
  if (value) retainedText.value = cycleSpeech(value)
}
async function restore() {
  const generation = ++epoch, plan = props.planId, room = roomId.value
  if (!plan) return
  loading.value = true
  error.value = ''
  try {
    const result = await getAnchorCycle(plan, room)
    if (active && generation === epoch && props.planId === plan && roomId.value === room) {
      update(result.run)
      // Browser playback is not room playback. Reload never replays old voices
      // automatically or assumes a persisted core/playback position.
      for (const item of result.run?.accepted || []) if (item.voice?.status === 'ready' && !seenVoices.has(item.selected.id)) consumedVoices.value.add(item.selected.id)
      notice.value = result.run ? '已从Redis读取本场状态；点击继续，不会自动请求。' : '暂无本场状态。服务器重启后从开场重新开始。'
    }
  } catch (value) { if (generation === epoch) error.value = value instanceof Error ? value.message : '读取大循环失败' }
  finally { if (generation === epoch) loading.value = false }
}
async function drive() {
  if (!run.value || running.value || ending.value) return
  running.value = true; stopped.value = false; error.value = ''; emit('busy', true)
  const generation = epoch, plan = props.planId, room = roomId.value
  try {
    if (!voiceErrorMessage.value) voicePaused.value = false
    startVoiceWorker()
    const outcome = await runCycleLoop({ state: () => run.value, stopped: () => stopped.value || !active || generation !== epoch,
      beforeAdvance: async () => { await waitForAudioCapacity(generation) },
      advance: async state => {
        requestStartedAt.value = Date.now(); currentTime.value = requestStartedAt.value
        try { return await advanceAnchorCycle(plan, room, state.run_id, state.revision) }
        finally { requestStartedAt.value = 0 }
      },
      receive: state => { if (active && generation === epoch) { update(state); startVoiceWorker() } },
    })
    if (generation === epoch && !playbackBlocked.value) notice.value = outcome === 'completed' ? '本轮文字已完成。缓存声音继续按顺序播放；所有资产与评分可在调试记录查看。' : '已暂停生成，当前状态保存在Redis。'
  } catch (value) {
    if (generation === epoch) {
      error.value = value instanceof Error ? value.message : '大循环已停止'
      // Read rather than retry after a lost response: a draft or adoption may
      // already have been committed. Never automatically repeat generation.
      try { const snapshot = await getAnchorCycle(plan, room); if (active && generation === epoch) update(snapshot.run) } catch { }
    }
  } finally { running.value = false; if (active) emit('busy', false) }
}
async function begin() {
  if (!props.ready || run.value || creating.value || ending.value) return
  creating.value = true; error.value = ''; emit('busy', true)
  notice.value = ''; clearAudio()
  const generation = epoch
  try {
    if (ttsEnabled.value && !selectedVoice.value) throw new Error('请先选择系统音色')
    const [source, id] = selectedVoice.value.split(':')
    const result = await startAnchorCycle(props.planId, roomId.value, { ...props.config, threshold: threshold.value, max_retries: retries.value, max_chars: maxChars.value, point_gap: pointGap.value, repair_punctuation: repairPunctuation.value, task_limit: taskLimit.value, rolling: rolling.value, body_segments: 0, body_min: rolling.value ? bodyMin.value : 0, body_max: rolling.value ? bodyMax.value : 0, combination_gap: combinationGap.value, max_cycles: maxCycles.value, tts: ttsEnabled.value ? { source: source || 'official', voice_id: source === 'official' ? id || '' : '', profile_id: source === 'clone' ? Number(id) : undefined, rate: voiceRate.value, buffer_seconds: bufferSeconds.value } : undefined })
    if (active && generation === epoch) { retainedText.value = ''; update(result.run) }
  } catch (value) { error.value = value instanceof Error ? value.message : '建立大循环失败' }
  finally { creating.value = false; emit('busy', false) }
  if (active && generation === epoch && run.value) await drive()
}
async function finish() {
  if (!run.value || ending.value) return
  const id = run.value.run_id
  stopped.value = true; ending.value = true; ++epoch
  clearAudio()
  try {
    const result = await endAnchorCycle(props.planId, roomId.value, id)
    update(null); error.value = ''; notice.value = result.warning || '本场Redis状态已清除，未完成请求已取消。采纳稿可在当前页下载。'
  } catch (value) { error.value = value instanceof Error ? value.message : '结束本场失败，请重试' }
  finally { ending.value = false }
}
function download(name: string, value: string, type = 'text/plain;charset=utf-8') {
  const url = URL.createObjectURL(new Blob([value], { type }))
  const link = document.createElement('a'); link.href = url; link.download = name; link.click(); URL.revokeObjectURL(url)
}
watch(ttsEnabled, enabled => { if (enabled && !officialVoices.value.length) void loadVoices() })
watch(() => props.planId, () => { stopped.value = true; clearAudio(); update(null); retainedText.value = ''; error.value = ''; void restore() }, { immediate: true })
watch(roomId, () => { stopped.value = true; clearAudio(); update(null); retainedText.value = ''; error.value = ''; void restore() })
onBeforeUnmount(() => { active = false; stopped.value = true; ++epoch; clearAudio(); clearInterval(timer); emit('busy', false) })
</script>

<template>
  <section class="cycle-panel">
    <div class="controls">
      <button :disabled="!ready || !!run || creating || loading || ending" @click="begin">{{ creating ? '正在建会话…' : '开始大循环' }}</button>
      <button v-if="run && (run.phase !== 'ended' || (run.config.tts && pendingVoiceSegment(run)))" :disabled="running || creating || ending || run.busy || run.uncertain || run.status === 'spacing_blocked' || run.status === 'content_blocked' || run.status === 'compliance_blocked'" @click="drive">{{ run.phase === 'ended' ? '补齐最后声音' : '继续本场' }}</button>
      <button v-if="running || synthesizing" :disabled="stopped || ending" @click="pauseProduction">{{ stopped ? '收完在途请求后暂停…' : '暂停生产' }}</button>
      <button v-if="run" :disabled="ending" @click="finish">{{ ending ? '正在清理…' : '结束本场并清理' }}</button>
      <button :disabled="running || creating || loading || ending" @click="restore">刷新当前进度</button>
      <button :disabled="!speech" @click="download('大循环采纳话术-' + planId + '.txt', speech)">下载话术</button>
    </div>
    <div class="tts-controls">
      <label><input v-model="ttsEnabled" type="checkbox" :disabled="!!run || creating">开启TTS声音试听（仅测试，不进入房间）</label>
      <div v-if="ttsEnabled && !run" class="controls">
        <label>系统音色<select v-model="selectedVoice" :disabled="voiceLoading || creating"><option value="">请选择音色</option><optgroup label="官方音色"><option v-for="voice in officialVoices" :key="voice.id" :value="'official:' + voice.id">{{ voice.name }}</option></optgroup><optgroup label="已有克隆音色"><option v-for="voice in clonedVoices" :key="voice.id" :value="'clone:' + voice.id">{{ voice.name }}</option></optgroup></select></label>
        <label>语速<input v-model.number="voiceRate" type="number" min="0.5" max="2" step="0.05" :disabled="creating"></label>
        <label>缓存上限（秒）<input v-model.number="bufferSeconds" type="number" min="20" max="600" :disabled="creating"></label>
        <button :disabled="voiceLoading || creating" @click="loadVoices">{{ voiceLoading ? '加载音色…' : '刷新音色' }}</button>
      </div>
      <p v-if="voiceError" class="error">{{ voiceError }}</p>
      <div v-if="showTTS">
        <p class="note">总缓存 {{ pendingAudioSeconds }}/{{ activeBufferSeconds }}秒（待合成文字预计{{ Math.ceil(audioBuffer.pendingMS / 1000) }}秒＋声音实际剩余{{ Math.ceil(audioBuffer.readyMS / 1000) }}秒） · 待合成{{ audioBuffer.pendingCount }}段</p>
        <p class="note">文字：{{ bufferWaiting ? '缓存已满，等待播放消耗' : running ? '持续预生成' : '已暂停／完成' }} · TTS：{{ voiceErrorMessage ? '失败，队列暂停' : synthesizing ? '后台生成声音、SRT与语义切点' : voicePaused ? '已暂停' : '等待新采纳稿' }} · 播放：{{ playbackBlocked ? '等待手动播放或核对声音' : '按采纳顺序试听' }}</p>
        <p v-if="voiceErrorMessage" class="error">{{ voiceErrorMessage }}。文字只会预生成到缓存上限，后面的声音不会跳过失败段。</p>
        <button v-if="voiceErrorMessage || (voicePaused && run?.config.tts && pendingVoiceSegment(run))" :disabled="synthesizing || ending" @click="retryVoiceQueue">继续／重试声音队列</button>
        <audio ref="voicePlayer" controls preload="auto" @timeupdate="playedSeconds = voicePlayer?.currentTime || 0" @ended="audioEnded" @error="audioFailed" @play="playbackBlocked = false"></audio>
        <button :disabled="!voiceQueue.length" @click="playBufferedVoice">播放缓存声音</button>
        <button v-if="latestVoice" @click="download('最新采纳段.srt', latestVoice.bundle.srt)">下载最新SRT</button>
        <button v-if="latestVoice" @click="download('最新声音资产包.json', JSON.stringify(latestVoice.bundle, null, 2), 'application/json;charset=utf-8')">下载时间轴与语义资产清单</button>
        <p v-if="latestVoice?.cache_id" class="note">最新声音：{{ latestVoice.cache_hit ? '命中OSS缓存，未重新合成' : '已存OSS缓存' }} · 到期 {{ latestVoice.cache_expires_at ? new Date(latestVoice.cache_expires_at).toLocaleString() : '' }}。命中不续期；缓存过期不再复用，后台删除文件。</p>
        <p class="note">OSS声音缓存保留时间由后台运维统一设置，默认5小时；与这里的播放缓存上限（秒）是两项不同配置。</p>
        <p class="note">文字和TTS异步并行。待合成文字先估算时长，声音完成后改用实际时长；总缓存满了才等，不逐段等TTS。一次在途段可能略超上限。声音失败只停声音队列，文字仍受总缓存上限约束。刷新不自动重播历史声音。</p>
      </div>
    </div>
    <p v-if="error || run?.error" class="error">{{ error || run?.error }}</p>
    <div v-if="run && progress" class="cycle-progress" aria-label="大循环进度">
      <div class="progress-heading">
        <strong>大循环 · 第{{ progress.round }}{{ run.config.rolling && run.config.max_cycles ? '/' + run.config.max_cycles : '' }}轮 · {{ phases[run.phase] }}{{ run.config.rolling && !run.config.max_cycles ? '（持续滚动）' : '' }}</strong>
        <span>{{ ending ? '正在结束本场' : run.uncertain ? '结果待核对' : run.status === 'compliance_blocked' ? '内容检查未通过，已暂停' : run.status === 'content_blocked' ? '塑价钩子未兑现，已暂停' : run.status === 'spacing_blocked' ? '避重间隔不足，已暂停' : bufferWaiting ? '总缓存已满，等待播放' : running ? (stopped ? '收好在途请求后暂停' : '文字自动推进中' + (synthesizing ? '，TTS并行合成' : '')) : run.busy ? '服务端正在处理，请核对进度' : run.phase === 'ended' ? '文字完成' + (synthesizing ? '，TTS仍在合成' : '') : '已暂停／待继续' }}</span>
      </div>
      <div class="stage-track">
        <div v-for="stage in stages" :key="stage.phase" :class="['stage', { current: run.phase === stage.phase, done: stage.phase === 'opening' ? progress.openingDone : stage.phase === 'body' ? run.phase === 'closing' || run.phase === 'ended' : progress.closingDone }]" :aria-current="run.phase === stage.phase ? 'step' : undefined">
          <strong>{{ stage.title }}{{ stage.phase === 'opening' ? ' · ' + progress.openingVersion : '' }}</strong>
          <span v-if="stage.phase === 'opening'">{{ progress.openingDone ? '已采纳' : run.queue.length ? '待采纳' : '无卖点，跳过' }}</span>
          <span v-else-if="stage.phase === 'body'">{{ progress.bodyDone }}/{{ progress.bodyTotal }}段已采纳</span>
          <span v-else>{{ progress.closingDone ? '已采纳' : '待采纳' }}</span>
        </div>
      </div>
      <div class="progress-bar" role="progressbar" aria-label="本轮已采纳任务进度" :aria-valuenow="progress.percent" :aria-valuemin="0" :aria-valuemax="100"><span :style="{ width: progress.percent + '%' }"></span></div>
      <p class="current-task">{{ run.phase === 'ended' ? '完成情况' : '当前任务' }}：{{ progress.task }}</p>
      <p class="progress-metrics">本轮已采纳{{ progress.done }}/{{ progress.total }}段 · 卖点×角度已覆盖{{ progress.covered }}/{{ run.queue.length }}个 · 原稿达到{{ run.config.threshold }}分即采纳 · 1000字窗口{{ run.macro_report?.stats?.chars || 0 }}/1000字</p>
      <p v-if="run.phase !== 'ended'" class="progress-metrics">{{ requestStartedAt ? '正在请求' : '下次请求' }}第{{ progress.nextAttempt }}/{{ progress.maxAttempts }}版{{ progress.nextAttempt > 1 ? '（同任务重写）' : '（首次生成）' }}<span v-if="requestStartedAt"> · 已等待{{ requestWaitSeconds }}秒（含生成、评分及必要回灌）</span></p>
      <p v-if="progress.lastScore !== null" class="progress-metrics">上一采纳段：{{ progress.lastScore }}分 · 选中稿模型耗时{{ progress.lastGenerationSeconds.toFixed(2) }}秒 · 本段所有候选模型累计{{ progress.lastTaskGenerationSeconds.toFixed(2) }}秒</p>
      <p v-if="run.config.body_min" class="progress-metrics">中间讲解允许{{ run.config.body_min }}～{{ run.config.body_max }}段，本轮计划{{ progress.bodyTotal }}段；{{ run.pending_price_hook ? '下一段必须兑现：' + run.pending_price_hook.promise : '当前没有待兑现塑价钩子' }}</p>
    </div>
    <p v-else class="progress">{{ creating ? '正在建立大循环会话…' : '等待开始大循环' }}</p>
    <p class="note">{{ notice || '首场完整版开场，后续收口后接新版开场；卖点池优先未讲组合，耗尽后隔开复用、换讲法。重写不推进任务，1000字窗口跨轮保留，只统计采纳原稿。' }}</p>
    <RiskText :plan-id="planId" :text="speech" box auto-scroll label="大循环采纳稿" />
    <details v-if="riskDrafts.length">
      <summary>查看被拦截的风险稿（不进入正文或声音）</summary>
      <section v-for="draft in riskDrafts" :key="draft.id" class="risk-draft">
        <RiskText :plan-id="planId" :text="draft.text" :legend="false" />
      </section>
    </details>
    <details>
      <summary>大循环设置</summary>
      <div class="controls">
        <label>运行房间<select v-model.number="roomId" :disabled="!!run || running || creating"><option :value="0">独立测试（不绑定房间）</option><option v-for="id in roomIds" :key="id" :value="id">直播间 #{{ id }}</option></select></label>
        <label>达标分数<input v-model.number="threshold" type="number" min="0" max="100" :disabled="!!run || creating"></label>
        <label>补齐明确句末标点<input v-model="repairPunctuation" type="checkbox" :disabled="!!run || creating"></label>
        <label>最多重试<input v-model.number="retries" type="number" min="0" max="5" :disabled="!!run || creating"></label>
        <label>每段最多字符<input v-model.number="maxChars" type="number" min="80" max="1000" :disabled="!!run || creating"></label>
        <label>同卖点至少隔几段<input v-model.number="pointGap" type="number" min="0" max="100" :disabled="!!run || creating"></label>
        <label>收口后继续滚动<input v-model="rolling" type="checkbox" :disabled="!!run || creating"></label>
        <label v-if="rolling">中间最少段数<input v-model.number="bodyMin" type="number" min="2" max="6" :disabled="!!run || creating"></label>
        <label v-if="rolling">中间最多段数<input v-model.number="bodyMax" type="number" min="2" max="6" :disabled="!!run || creating"></label>
        <label v-if="rolling">同组合至少隔几段<input v-model.number="combinationGap" type="number" min="0" max="500" :disabled="!!run || creating"></label>
        <label v-if="rolling">测试轮数（0为持续）<input v-model.number="maxCycles" type="number" min="0" max="1000" :disabled="!!run || creating"></label>
        <label>测试组合数（0为全部）<input v-model.number="taskLimit" type="number" min="0" max="500" :disabled="!!run || creating"></label>
      </div>
      <p class="note">默认达到40分即采纳，低于阈值才重写；首次生成＋最多{{ retries }}次重试。默认测试2轮，每轮开场1段＋中间2～6段＋收口1段；每轮计划一次，重写不改计划。塑价默认本段完成，偶尔留钩时下一段先兑现，不连续吊着；必要承接可延长但不超过6段。测试轮数设0才持续。避重间隔无法满足时暂停，不偷偷放宽。补标点不改原始评分。本场状态只放Redis，结束清除，重启重新开始；生成的声音资产包另存媒体库。页面关闭后不发新请求。模型和TTS会产生费用。</p>
    </details>
    <details v-if="advanced && run">
      <summary>候选稿、逐段评分、采纳结果和1000字回灌 JSON</summary>
      <button @click="download('大循环完整记录-' + run.run_id + '.json', JSON.stringify(run, null, 2), 'application/json;charset=utf-8')">导出完整记录</button>
      <pre>{{ JSON.stringify(run, null, 2) }}</pre>
    </details>
    <OpeningHookSettingsPanel :plan-id="planId" :room-id="roomId" :disabled="!!run || creating || running || ending" />
  </section>
</template>

<style scoped>
.cycle-panel { background: white; border: 1px solid #dce6e8; padding: 24px; border-radius: 20px; }
.controls { display: flex; flex-wrap: wrap; gap: 12px; align-items: end; }
button { cursor: pointer; border: 1px solid #cedddd; border-radius: 10px; padding: 12px 18px; background: #f7fbfa; color: #126f65; font-weight: 700; }
button:disabled { opacity: .45; cursor: not-allowed; }
.controls > button:first-child { background: #117e72; color: white; }
label { display: grid; gap: 8px; color: #415463; font-weight: 600; }
input, select { padding: 10px; border: 1px solid #cdd9df; border-radius: 8px; max-width: 240px; }
input { width: 150px; }
textarea { width: 100%; box-sizing: border-box; min-height: 440px; padding: 20px; border: 1px solid #cdd9df; border-radius: 12px; font-size: 19px; line-height: 1.8; resize: vertical; }
.progress { color: #415463; }.note { color: #6b7c8d; line-height: 1.6; }.error { color: #b32d41; }
.cycle-progress { margin-top: 18px; padding: 18px; border: 1px solid #d5e6e1; border-radius: 14px; background: #f5faf8; }
.progress-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 10px; color: #126f65; }
.progress-heading > span { color: #6b7c8d; font-size: 14px; }
.stage-track { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin: 14px 0; }
.stage { display: grid; gap: 6px; padding: 12px; background: white; border: 1px solid #dce6e8; border-radius: 10px; color: #647583; }
.stage > span { font-size: 13px; }.stage.done { color: #126f65; background: #e5f3ed; }.stage.current { border: 2px solid #117e72; padding: 11px; color: #126f65; }
.progress-bar { height: 7px; border-radius: 5px; overflow: hidden; background: #dce9e3; }.progress-bar > span { display: block; height: 100%; background: #117e72; transition: width .2s; }
.current-task { margin: 12px 0 8px; color: #314b49; overflow-wrap: anywhere; }.progress-metrics { margin: 6px 0; color: #647583; font-size: 14px; line-height: 1.6; }
details { margin-top: 18px; } summary { cursor: pointer; font-weight: 700; color: #415463; }
pre { max-height: 650px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; background: #13202b; color: #e9f4f4; padding: 18px; border-radius: 10px; }
.tts-controls { margin-top: 16px; padding: 14px; border: 1px solid #d5e6e1; border-radius: 10px; }.tts-controls > label { display: flex; align-items: center; }.tts-controls input[type=checkbox] { width: auto; }audio { display: block; width: 100%; max-width: 650px; margin: 8px 0; }
.risk-draft { margin-top: 12px; padding: 14px; border: 1px solid #fecdd3; border-radius: 10px; background: #fffafa; }
</style>
