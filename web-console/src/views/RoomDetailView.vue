<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import Hls from 'hls.js'
import {
  getLiveAgentSettings,
  chatLiveAgent,
  getLiveRuntime,
  getLiveRuntimeEvents,
  getRoom,
  getRoomEvents,
  getTenants,
  recordLiveRuntimeEvent,
  startLiveRuntime,
  stopLiveRuntime,
} from '../api'
import { session } from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import type { LiveAgentSettings, LiveRuntimeSnapshot, Room, RoomEvent, Tenant } from '../types'

const route = useRoute()
const legacyAgentSurfaceEnabled = false
const roomId = Number(route.params.id)

const isInternalViewer = computed(() =>
  ['platform_admin', 'staff', 'sales_staff'].includes(
    session.bootstrap?.actor.role || '',
  ),
)

const room = ref<Room | null>(null)
const tenantDirectory = ref<Tenant[]>([])
const events = ref<RoomEvent[]>([])
const loading = ref(true)
const error = useFeedbackErrorRef()
const streamState = ref<'connecting' | 'online' | 'offline'>('connecting')
const activeType = ref('all')
const liveVideo = ref<HTMLVideoElement | null>(null)
const videoState = ref<'idle' | 'connecting' | 'live' | 'error'>('idle')
const previewPaused = ref(false)
const videoAspectRatio = ref(16 / 9)
const videoOrientation = ref<'portrait' | 'landscape'>('landscape')
let eventSource: EventSource | null = null
let hls: Hls | null = null
let videoRetryTimer: number | undefined
let runtimePollTimer: number | undefined
const runtimeSnapshot = ref<LiveRuntimeSnapshot | null>(null)
const runtimeBusy = ref(false)
const runtimeError = ref('')

const questionIds = ref<number[]>([])
const selectedQuestionIds = ref<number[]>([])
const agentDrawerOpen = ref(false)
const agentDockExpanded = ref(false)
const agentDockEl = ref<HTMLElement | null>(null)
const agentDockInputEl = ref<HTMLInputElement | null>(null)
const agentInput = ref('')
const agentChatBusy = ref(false)
const agentDockPosition = ref<{ x: number; y: number } | null>(null)
const agentDockDragging = ref(false)
const agentDockDragged = ref(false)
let agentDockDragState:
  | {
      pointerId: number
      startX: number
      startY: number
      originX: number
      originY: number
      halfWidth: number
      halfHeight: number
    }
  | null = null
const defaultAgentSettings: LiveAgentSettings = {
  tenant_id: 0,
  display_name: '小伴直播教练',
  role_name: '直播策略与场控 Agent',
  self_introduction: '我是小伴直播教练，是你的直播策略与场控 Agent。',
  mission: '我负责直播策略调教、主播训练、固定话术、声音配置和现场场控协作。',
  greeting: '你好，我是小伴直播教练。你可以直接告诉我现在直播间要怎么处理。',
}
const agentSettings = ref<LiveAgentSettings>({ ...defaultAgentSettings })

const agentMessages = ref<Array<{ role: 'agent' | 'user'; text: string }>>([
  { role: 'agent', text: defaultAgentSettings.greeting },
])
const latestAgentMessage = computed(() => {
  for (let index = agentMessages.value.length - 1; index >= 0; index -= 1) {
    if (agentMessages.value[index].role === 'agent') return agentMessages.value[index].text
  }
  return agentSettings.value.display_name + ' 已就绪'
})
const agentTimeline = ref([
  { time: '09:48:30', kind: '观察', text: '等待直播间事件，出现购买决策问题后会进入待处理队列。' },
  { time: '09:48:30', kind: '计划', text: '保持当前产品主线，不主动打断主播。' },
])
const anchorTranscript = ref('等待主播实时语音转写…')

const aiRunning = computed(() => runtimeSnapshot.value?.session?.status === 'running')
const boundDevice = computed(() => runtimeSnapshot.value?.device || null)
const activeQuotaSeconds = computed(() => runtimeSnapshot.value?.quota_remaining_seconds || 0)
const reserveCardCount = computed(() => runtimeSnapshot.value?.reserve_time_card_count || 0)
const reserveCardSeconds = computed(() => runtimeSnapshot.value?.reserve_time_card_seconds || 0)
const hasAnyAIQuota = computed(() => activeQuotaSeconds.value > 0 || reserveCardCount.value > 0)
type SimDeviceState = 'working' | 'connected' | 'offline'
const simulatedDeviceState = ref<SimDeviceState>(
  (localStorage.getItem('live-sim-device-' + roomId) as SimDeviceState) || 'offline',
)
const canStartAI = computed(() =>
  room.value?.status === 'live' &&
  !aiRunning.value &&
  hasAnyAIQuota.value &&
  boundDevice.value?.connection_status === 'online',
)

const aiStatusText = computed(() => {
  if (aiRunning.value) return '运行中'
  if (!hasAnyAIQuota.value) return '时长已用完'
  if (activeQuotaSeconds.value <= 0 && reserveCardCount.value > 0) return '有储备卡，启动后自动激活'
  if (room.value?.status !== 'live') return '等待开播'
  if (!boundDevice.value) return '未绑定设备'
  if (boundDevice.value.connection_status !== 'online') return '设备离线'
  return '已停止'
})

const deviceVisualState = computed<SimDeviceState>(() => {
  const device = boundDevice.value
  if (!device) return simulatedDeviceState.value
  if (device.connection_status !== 'online') return 'offline'
  if (device.work_status === 'working') return 'working'
  return 'connected'
})

const deviceStatusText = computed(() => {
  const prefix = boundDevice.value?.sn || '模拟设备'
  if (deviceVisualState.value === 'working') return prefix + ' · 工作中'
  if (deviceVisualState.value === 'connected') return prefix + ' · 已连接，未工作'
  return prefix + ' · 离线'
})

const deviceStatusLabel = computed(() => {
  if (deviceVisualState.value === 'working') return '工作'
  if (deviceVisualState.value === 'connected') return '已连接未工作'
  return '离线'
})

const pendingQuestions = computed(() =>
  events.value.filter((event) => questionIds.value.includes(event.id)),
)

function logUserAction(eventCode: string, detail?: Record<string, unknown>) {
  recordLiveRuntimeEvent(roomId, eventCode, detail).catch(() => {})
}

function setSimulatedDeviceState(state: SimDeviceState) {
  simulatedDeviceState.value = state
  localStorage.setItem('live-sim-device-' + roomId, state)
  const eventCode =
    state === 'working'
      ? 'DEVICE_SIM_STARTED'
      : state === 'connected'
        ? 'DEVICE_SIM_PAUSED'
        : 'DEVICE_SIM_CLOSED'
  logUserAction(eventCode, { simulated: true, state })
}

function addQuestion(event: RoomEvent) {
  if (questionIds.value.includes(event.id)) return
  questionIds.value.unshift(event.id)
  logUserAction('QUESTION_ADDED', {
    public_event_id: event.id,
    nickname: event.nickname || '',
    content: event.content || '',
  })
}

function toggleQuestion(event: RoomEvent) {
  selectedQuestionIds.value = selectedQuestionIds.value.includes(event.id)
    ? selectedQuestionIds.value.filter((id) => id !== event.id)
    : [...selectedQuestionIds.value, event.id]
}

function mentionSelected() {
  const selected = pendingQuestions.value.filter((event) => selectedQuestionIds.value.includes(event.id))
  if (!selected.length) return
  const quote = selected.map((event) => '@' + (event.nickname || '直播间用户') + '「' + (event.content || '未填写内容') + '」').join('\n')
  window.dispatchEvent(
    new CustomEvent('system-agent:prefill', {
      detail: {
        text: quote + '\n请告诉我怎么处理这些问题。',
        open: true,
      },
    }),
  )
  logUserAction('QUESTIONS_MENTIONED', {
    public_event_ids: selected.map((event) => event.id),
    count: selected.length,
  })
}

function handleAgentInputKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.isComposing) return
  if (!event.shiftKey) {
    event.preventDefault()
    sendAgentCommand()
  }
}

async function sendAgentCommand() {
  const value = agentInput.value.trim()
  if (!value || agentChatBusy.value) return
  if (!agentDrawerOpen.value) {
    agentDrawerOpen.value = true
    logUserAction('AGENT_DRAWER_AUTO_OPENED', { source: 'send' })
  }

  const history = agentMessages.value.slice(-8).map((item) => ({
    role: item.role,
    text: item.text,
  }))
  agentMessages.value.push({ role: 'user', text: value })
  agentInput.value = ''
  agentChatBusy.value = true
  logUserAction('AGENT_COMMAND_SUBMITTED', { message: value.slice(0, 2000) })

  try {
    const result = await chatLiveAgent(roomId, {
      message: value,
      anchor_transcript: anchorTranscript.value,
      history,
    })
    agentMessages.value.push({ role: 'agent', text: result.reply })
    logUserAction('AGENT_REPLY_RECEIVED', {
      kind: result.kind,
      model: result.model || '',
      latency_ms: result.latency_ms || 0,
    })
  } catch (err) {
    const message = err instanceof Error ? err.message : '场控 Agent 暂时无法回答'
    agentMessages.value.push({
      role: 'agent',
      text: '我这次没有连上回答服务：' + message,
    })
  } finally {
    agentChatBusy.value = false
  }
}

function expandAgentDock() {
  agentDockExpanded.value = true
  if (!agentDrawerOpen.value) {
    agentDrawerOpen.value = true
    logUserAction('AGENT_DRAWER_AUTO_OPENED', { source: 'dock' })
  }
  nextTick(() => agentDockInputEl.value?.focus())
}

function collapseAgentDock() {
  agentDockExpanded.value = false
  agentDrawerOpen.value = false
}

const agentDockStyle = computed(() => {
  if (!agentDockPosition.value) return undefined
  return {
    left: agentDockPosition.value.x + 'px',
    top: agentDockPosition.value.y + 'px',
    bottom: 'auto',
    transform: 'translate(-50%, -50%)',
  }
})

function moveAgentDock(event: PointerEvent) {
  const state = agentDockDragState
  if (!state || event.pointerId !== state.pointerId) return
  const dx = event.clientX - state.startX
  const dy = event.clientY - state.startY
  if (Math.abs(dx) + Math.abs(dy) > 4) {
    agentDockDragged.value = true
  }
  const margin = 10
  const minX = state.halfWidth + margin
  const maxX = Math.max(minX, window.innerWidth - state.halfWidth - margin)
  const minY = state.halfHeight + margin
  const maxY = Math.max(minY, window.innerHeight - state.halfHeight - margin)
  agentDockPosition.value = {
    x: Math.min(maxX, Math.max(minX, state.originX + dx)),
    y: Math.min(maxY, Math.max(minY, state.originY + dy)),
  }
}

function finishAgentDockDrag(event?: PointerEvent) {
  if (
    event &&
    agentDockDragState &&
    event.pointerId !== agentDockDragState.pointerId
  ) {
    return
  }
  window.removeEventListener('pointermove', moveAgentDock)
  window.removeEventListener('pointerup', finishAgentDockDrag)
  window.removeEventListener('pointercancel', finishAgentDockDrag)
  if (agentDockDragging.value && agentDockDragged.value) {
    logUserAction('AGENT_DOCK_MOVED', agentDockPosition.value || undefined)
  }
  agentDockDragging.value = false
  agentDockDragState = null
  document.body.style.userSelect = ''
}

function startAgentDockDrag(event: PointerEvent) {
  if (!agentDockEl.value || event.button !== 0) return
  const rect = agentDockEl.value.getBoundingClientRect()
  agentDockDragging.value = true
  agentDockDragged.value = false
  agentDockDragState = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    originX: rect.left + rect.width / 2,
    originY: rect.top + rect.height / 2,
    halfWidth: rect.width / 2,
    halfHeight: rect.height / 2,
  }
  document.body.style.userSelect = 'none'
  window.addEventListener('pointermove', moveAgentDock)
  window.addEventListener('pointerup', finishAgentDockDrag)
  window.addEventListener('pointercancel', finishAgentDockDrag)
}

function handleAgentOrbClick() {
  if (agentDockDragged.value) {
    agentDockDragged.value = false
    return
  }
  collapseAgentDock()
}

function formatDuration(seconds?: number) {
  const total = Math.max(0, Math.floor(seconds || 0))
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const secs = total % 60
  if (hours > 0) return hours + '小时' + minutes + '分'
  if (minutes > 0) return minutes + '分' + secs + '秒'
  return secs + '秒'
}

function formatQuotaExpiry(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function timeCardStatusLabel(status: string) {
  if (status === 'active') return '使用中'
  if (status === 'unactivated') return '未激活'
  return status
}

function runtimeEventKind(code: string) {
  if (code.includes('QUOTA')) return '计费'
  if (code.includes('DEVICE')) return '设备'
  if (code.includes('STOP') || code.includes('OFFLINE')) return '停止'
  if (code.includes('START')) return '启动'
  if (code.includes('QUESTION') || code.includes('AGENT')) return '人工'
  return '记录'
}

async function refreshRuntime() {
  try {
    const [snapshot, runtimeEvents] = await Promise.all([
      getLiveRuntime(roomId),
      getLiveRuntimeEvents(roomId, 30),
    ])
    runtimeSnapshot.value = snapshot
    runtimeError.value = ''
    if (room.value && room.value.status === 'live' && !snapshot.room_live) {
      room.value.status = 'offline'
    }
    if (runtimeEvents.length) {
      agentTimeline.value = runtimeEvents.slice(0, 8).map((event) => ({
        time: formatTime(event.occurred_at),
        kind: runtimeEventKind(event.event_code),
        text: event.title,
      }))
    }
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '读取AI运行状态失败'
  }
}

function startRuntimePolling() {
  if (runtimePollTimer !== undefined) window.clearInterval(runtimePollTimer)
  runtimePollTimer = window.setInterval(() => {
    refreshRuntime()
  }, 5000)
}

async function handleStartAI() {
  if (!canStartAI.value || runtimeBusy.value) return
  runtimeBusy.value = true
  runtimeError.value = ''
  try {
    await startLiveRuntime(roomId, boundDevice.value?.id)
    await refreshRuntime()
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '启动AI伴播失败'
  } finally {
    runtimeBusy.value = false
  }
}

async function handleStopAI() {
  if (!aiRunning.value || runtimeBusy.value) return
  runtimeBusy.value = true
  runtimeError.value = ''
  try {
    await stopLiveRuntime(roomId, 'manual_stop')
    await refreshRuntime()
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '停止AI伴播失败'
  } finally {
    runtimeBusy.value = false
  }
}


const eventTypes = [
  { key: 'all', label: '全部' },
  { key: 'chat', label: '弹幕' },
  { key: 'member', label: '进房' },
  { key: 'like', label: '点赞' },
  { key: 'follow', label: '关注' },
  { key: 'gift', label: '礼物' },
]

const visibleEvents = computed(() =>
  events.value.filter((event) => event.event_type !== 'room'),
)

const filteredEvents = computed(() => {
  if (activeType.value === 'all') return visibleEvents.value
  return visibleEvents.value.filter((event) => event.event_type === activeType.value)
})

const isAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const livePreviewUrl = computed(() => {
  if (!room.value) return ''
  if (room.value.source_url) return room.value.source_url
  if (!room.value.external_room_id) return ''
  return 'https://live.douyin.com/' + room.value.external_room_id + '?from=web_code_link'
})

function clearVideoRetry() {
  if (videoRetryTimer !== undefined) {
    window.clearTimeout(videoRetryTimer)
    videoRetryTimer = undefined
  }
}

function destroyLivePlayer() {
  clearVideoRetry()

  if (hls) {
    hls.destroy()
    hls = null
  }

  const video = liveVideo.value
  if (video) {
    video.pause()
    video.removeAttribute('src')
    video.load()
  }
}

function scheduleVideoRetry() {
  clearVideoRetry()
  if (previewPaused.value) return
  videoRetryTimer = window.setTimeout(() => {
    if (!previewPaused.value) startLivePlayer()
  }, 3000)
}

async function startLivePlayer() {
  if (previewPaused.value) {
    videoState.value = 'idle'
    return
  }
  if (!room.value || room.value.status !== 'live') {
    videoState.value = 'idle'
    return
  }

  await nextTick()
  const video = liveVideo.value
  if (!video) return

  clearVideoRetry()
  if (hls) {
    hls.destroy()
    hls = null
  }

  videoState.value = 'connecting'
  videoAspectRatio.value = 16 / 9
  videoOrientation.value = 'landscape'
  const source =
    '/api/v1/rooms/' + roomId + '/live/index.m3u8?t=' + Date.now()

  if (Hls.isSupported()) {
    const instance = new Hls({
      lowLatencyMode: true,
      liveSyncDurationCount: 2,
      liveMaxLatencyDurationCount: 6,
      maxBufferLength: 8,
      backBufferLength: 0,
    })

    hls = instance
    instance.attachMedia(video)

    instance.on(Hls.Events.MEDIA_ATTACHED, () => {
      instance.loadSource(source)
    })

    instance.on(Hls.Events.MANIFEST_PARSED, () => {
      video.play().catch(() => {})
    })

    instance.on(Hls.Events.ERROR, (_event, data) => {
      if (!data.fatal) return
      videoState.value = 'error'
      instance.destroy()
      if (hls === instance) hls = null
      scheduleVideoRetry()
    })
    return
  }

  if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = source
    video.play().catch(() => {})
    return
  }

  videoState.value = 'error'
}

function handleVideoMetadata() {
  const video = liveVideo.value
  if (!video || video.videoWidth <= 0 || video.videoHeight <= 0) {
    return
  }

  videoAspectRatio.value = video.videoWidth / video.videoHeight
  videoOrientation.value =
    video.videoHeight > video.videoWidth ? 'portrait' : 'landscape'
}
function handleVideoPlaying() {
  if (previewPaused.value) return
  videoState.value = 'live'
}

function handleVideoError() {
  if (previewPaused.value) return
  videoState.value = 'error'
  scheduleVideoRetry()
}
function pauseLivePreview() {
  if (previewPaused.value) return
  previewPaused.value = true
  destroyLivePlayer()
  videoState.value = 'idle'
  logUserAction('LIVE_PREVIEW_PAUSED', { source: 'room_detail' })
}

function resumeLivePreview() {
  if (!previewPaused.value) return
  previewPaused.value = false
  videoState.value = room.value?.status === 'live' ? 'connecting' : 'idle'
  logUserAction('LIVE_PREVIEW_RESUMED', { source: 'room_detail' })
  void startLivePlayer()
}

function toggleLivePreview() {
  if (previewPaused.value) {
    resumeLivePreview()
    return
  }
  pauseLivePreview()
}
function roomTitle() {
  if (!room.value) return '直播间'
  return room.value.name || '直播间 ' + room.value.external_room_id
}

function tenantName() {
  if (!room.value) return ''
  const tenants = tenantDirectory.value.length
    ? tenantDirectory.value
    : (session.bootstrap?.tenants ?? [])
  return tenants.find((tenant) => tenant.id === room.value?.tenant_id)?.name || ''
}

async function loadTenantDirectory() {
  const bootstrap = session.bootstrap
  tenantDirectory.value = bootstrap?.tenants ?? []
  if (bootstrap?.actor.role !== 'platform_admin') return

  try {
    const response = await getTenants()
    tenantDirectory.value = response.items
  } catch {
    // The room detail remains usable even if the optional tenant label fails.
  }
}

function statusText(status?: string) {
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'pending') return '等待连接'
  if (status === 'offline') return '未开播'
  if (status === 'error') return '连接异常'
  return status || '未知'
}

function eventLabel(type: string) {
  const labels: Record<string, string> = {
    chat: '弹幕',
    member: '进房',
    like: '点赞',
    follow: '关注',
    gift: '礼物',
    room: '房间',
  }
  return labels[type] || type
}

function eventClass(type: string) {
  return 'event-' + type
}

function formatTime(value: string) {
  const date = new Date(value)
  return date.toLocaleTimeString('zh-CN', {
    hour12: false,
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function pushEvent(event: RoomEvent) {
  if (events.value.some((item) => item.id === event.id)) return
  events.value.unshift(event)
  if (events.value.length > 500) {
    events.value = events.value.slice(0, 500)
  }
}

async function load() {
  if (!Number.isFinite(roomId) || roomId <= 0) {
    error.value = '无效的直播间'
    loading.value = false
    return
  }

  loading.value = true
  error.value = ''
  try {
    const [roomData, eventData] = await Promise.all([
      getRoom(roomId),
      getRoomEvents(roomId, 200),
    ])
    room.value = roomData
    events.value = [...eventData.items].sort((a, b) => b.id - a.id)
    void loadTenantDirectory()

    if (!isInternalViewer.value) {
      try {
        const agentSettingsData = await getLiveAgentSettings()
        agentSettings.value = agentSettingsData
        if (agentMessages.value.length === 1 && agentMessages.value[0].role === 'agent') {
          agentMessages.value = [{ role: 'agent', text: agentSettingsData.greeting }]
        }
      } catch {
        agentSettings.value = { ...defaultAgentSettings }
      }
    }
    connectStream()
    await Promise.all([startLivePlayer(), refreshRuntime()])
    startRuntimePolling()
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取直播间失败'
  } finally {
    loading.value = false
  }
}

function connectStream() {
  eventSource?.close()
  streamState.value = 'connecting'

  eventSource = new EventSource('/api/v1/rooms/' + roomId + '/stream', {
    withCredentials: true,
  })

  eventSource.onopen = () => {
    streamState.value = 'online'
  }

  eventSource.onmessage = (message) => {
    try {
      const event = JSON.parse(message.data) as RoomEvent
      pushEvent(event)
      if (room.value) {
        room.value.status = 'live'
        room.value.last_event_at = event.occurred_at
      }
      if (
        !previewPaused.value &&
        (videoState.value === 'idle' || videoState.value === 'error')
      ) {
        startLivePlayer()
      }
    } catch {
      // Ignore malformed development events.
    }
  }

  eventSource.onerror = () => {
    streamState.value = 'offline'
  }
}

onMounted(load)
onBeforeUnmount(() => {
  eventSource?.close()
  if (runtimePollTimer !== undefined) window.clearInterval(runtimePollTimer)
  finishAgentDockDrag()
  destroyLivePlayer()
})
</script>

<template>
  <div class="room-detail-page">
    <ModulePageNav
      context="live"
      active-title="直播间详情"
      :active-nav-title="isInternalViewer ? '直播间列表' : '直播间'"
    />

    <div v-if="loading" class="detail-loading">正在读取直播间…</div>
    <div v-else-if="error && !room" class="inline-error">{{ error }}</div>

    <template v-else-if="room">
      <section class="detail-header">
        <div class="detail-title-row">
          <div class="platform-icon large">抖</div>
          <div>
            <div class="detail-title-line">
              <h2>{{ roomTitle() }}</h2>
              <span class="status-pill" :class="'status-' + room.status">
                <i></i>
                {{ statusText(room.status) }}
              </span>
            </div>
            <p>
              抖音 · 房间号 {{ room.external_room_id }}
              <span v-if="isAdmin && tenantName()"> · {{ tenantName() }}</span>
            </p>
          </div>
        </div>

        <div class="stream-indicator" :class="streamState">
          <i></i>
          {{
            streamState === 'online'
              ? '公屏实时连接'
              : streamState === 'connecting'
                ? '正在连接公屏'
                : '实时流重连中'
          }}
        </div>
      </section>

      <div v-if="error" class="inline-error">{{ error }}</div>
      <div v-if="runtimeError" class="inline-error">{{ runtimeError }}</div>

      <section class="detail-stat-grid">
        <article>
          <span>在线人数</span>
          <strong>{{ room.online_count.toLocaleString() }}</strong>
        </article>
        <article>
          <span>公屏事件</span>
          <strong>{{ events.length }}</strong>
        </article>
        <article class="runtime-stat-card">
          <span>AI 伴播</span>
          <strong :class="aiRunning ? 'live-agent-status' : 'muted-value'">{{ aiStatusText }}</strong>
          <div class="runtime-quota-brief">
            <div>
              <span>当前已激活</span>
              <b>{{ formatDuration(activeQuotaSeconds) }}</b>
            </div>
            <small v-if="runtimeSnapshot?.current_quota">
              {{ runtimeSnapshot.current_quota.source_label }}
              <template v-if="runtimeSnapshot.current_quota.asset_no"> · {{ runtimeSnapshot.current_quota.asset_no }}</template>
              <template v-if="runtimeSnapshot.current_quota.expires_at"> · {{ formatQuotaExpiry(runtimeSnapshot.current_quota.expires_at) }} 到期</template>
            </small>
            <small v-else-if="reserveCardCount > 0">当前没有已激活额度，启动 AI 时自动激活 1 张储备卡。</small>
            <small v-if="reserveCardCount > 0" class="runtime-quota-reserve">
              储备 {{ reserveCardCount }} 张未激活 · {{ formatDuration(reserveCardSeconds) }}
            </small>
          </div>
          <details v-if="runtimeSnapshot?.time_cards?.length" class="runtime-time-card-details">
            <summary>查看时长卡（{{ runtimeSnapshot.time_cards.length }}）</summary>
            <div class="runtime-time-card-list">
              <div
                v-for="card in runtimeSnapshot.time_cards"
                :key="card.asset_no"
                class="runtime-time-card-item"
                :class="{ active: card.status === 'active' }"
              >
                <div>
                  <strong>{{ card.product_name }}</strong>
                  <span>{{ card.asset_no }} · {{ timeCardStatusLabel(card.status) }}</span>
                </div>
                <b>{{ formatDuration(card.remaining_seconds) }}</b>
                <small v-if="card.status === 'active' && card.expires_at">
                  {{ formatQuotaExpiry(card.expires_at) }} 到期
                </small>
                <small v-else-if="card.activation_deadline_at">
                  {{ formatQuotaExpiry(card.activation_deadline_at) }} 前需激活
                </small>
                <small v-else>首次实际使用时自动激活</small>
              </div>
            </div>
          </details>
          <button
            v-if="!aiRunning"
            type="button"
            class="runtime-control-button"
            :disabled="!canStartAI || runtimeBusy"
            @click="handleStartAI"
          >
            {{ runtimeBusy ? '处理中…' : '启动 AI' }}
          </button>
          <button
            v-else
            type="button"
            class="runtime-control-button danger"
            :disabled="runtimeBusy"
            @click="handleStopAI"
          >
            {{ runtimeBusy ? '处理中…' : '停止 AI' }}
          </button>
        </article>
        <article class="device-runtime-card">
          <span>工作设备</span>
          <div class="device-runtime-status" :class="'state-' + deviceVisualState">
            <i></i>
            <strong>{{ deviceStatusLabel }}</strong>
          </div>
          <small>{{ deviceStatusText }}</small>
          <div class="device-sim-controls">
            <button
              type="button"
              :class="{ active: deviceVisualState === 'working' }"
              @click="setSimulatedDeviceState('working')"
            >开始</button>
            <button
              type="button"
              class="pause"
              :class="{ active: deviceVisualState === 'connected' }"
              @click="setSimulatedDeviceState('connected')"
            >暂停</button>
            <button
              type="button"
              class="close"
              :class="{ active: deviceVisualState === 'offline' }"
              @click="setSimulatedDeviceState('offline')"
            >关闭</button>
          </div>
        </article>
      </section>

      <section class="anchor-transcript-strip">
        <div>
          <span class="anchor-live-dot"></span>
          <strong>主播实时口播</strong>
          <small>实时转写</small>
        </div>
        <p>{{ anchorTranscript }}</p>
      </section>

      <section class="detail-layout">
        <div class="public-screen-panel">
          <div class="panel-header">
            <div>
              <span class="section-kicker">REALTIME</span>
              <h3>实时公屏</h3>
            </div>
            <span class="event-count">{{ filteredEvents.length }} 条</span>
          </div>

          <div class="event-tabs">
            <button
              v-for="type in eventTypes"
              :key="type.key"
              :class="{ active: activeType === type.key }"
              @click="activeType = type.key"
            >
              {{ type.label }}
            </button>
          </div>

          <div class="event-list">
            <div v-if="!filteredEvents.length" class="screen-empty">
              <div class="screen-empty-icon">⌁</div>
              <strong>等待直播间事件</strong>
              <span>真实采集器接入后，弹幕、进房、点赞等会实时出现在这里。</span>
            </div>

            <article
              v-for="event in filteredEvents"
              :key="event.id"
              class="event-row"
            >
              <time>{{ formatTime(event.occurred_at) }}</time>
              <span class="event-type" :class="eventClass(event.event_type)">
                {{ eventLabel(event.event_type) }}
              </span>
              <div class="event-body event-inline">
                <span class="event-user">【{{ event.nickname || '直播间用户' }}】</span>
                <span class="event-action">{ {{ event.content || eventLabel(event.event_type) }} }</span>
              </div>
              <button
                v-if="event.event_type === 'chat'"
                type="button"
                class="event-question-button"
                @click="addQuestion(event)"
              >
                ＋ 待处理
              </button>
            </article>
          </div>
        </div>

        <div class="live-control-stack">
          <section class="question-pool-panel">
            <div class="panel-header">
              <div>
                <span class="section-kicker">QUESTION POOL</span>
                <h3>待处理问题</h3>
              </div>
              <div class="question-pool-actions">
                <span>{{ pendingQuestions.length }} 条</span>
                <button type="button" :disabled="!selectedQuestionIds.length" @click="mentionSelected">@ 到场控</button>
              </div>
            </div>
            <div v-if="!pendingQuestions.length" class="question-pool-empty">从实时公屏把需要人工关注的问题收进这里。</div>
            <label v-for="event in pendingQuestions" :key="event.id" class="question-pool-row">
              <input
                type="checkbox"
                :checked="selectedQuestionIds.includes(event.id)"
                @change="toggleQuestion(event)"
              />
              <span>
                <strong>@{{ event.nickname || '直播间用户' }}</strong>
                <small>{{ event.content || '未填写内容' }}</small>
              </span>
              <time>{{ formatTime(event.occurred_at) }}</time>
            </label>
          </section>

          <section class="agent-timeline-panel">
            <div class="panel-header">
              <div>
                <span class="section-kicker">FLOOR AGENT</span>
                <h3>{{ agentSettings.display_name }}</h3>
              </div>
              <span class="agent-running-pill" :class="{ offline: !aiRunning }">
                {{ aiRunning ? '● 运行中' : '○ ' + aiStatusText }}
              </span>
            </div>
            <div class="agent-timeline-list">
              <article v-for="item in agentTimeline" :key="item.time + item.kind">
                <time>{{ item.time }}</time>
                <div>
                  <strong>{{ item.kind }}</strong>
                  <p>{{ item.text }}</p>
                </div>
              </article>
            </div>
            <div class="agent-now-card">
              <span>当前执行</span>
              <strong>{{ aiRunning ? 'AI 正在按直播策略工作' : 'AI 当前未执行直播任务' }}</strong>
              <small v-if="runtimeSnapshot?.session">
                会话 {{ runtimeSnapshot.session.external_id }} · 已计费 {{ formatDuration(runtimeSnapshot.session.total_billed_seconds) }}
              </small>
              <small v-else>只展示现场可读决策与执行结果；内部 JSON 与底层调用不在前台展示。</small>
            </div>
          </section>
        </div>

        <aside class="room-side-panel">
          <section class="live-preview-card">
            <div class="live-preview-head">
              <div>
                <span class="section-kicker">LIVE PREVIEW</span>
                <h3>直播画面</h3>
              </div>
              <span class="preview-live-state" :class="{ live: room.status === 'live' }">
                <i></i>
                {{ statusText(room.status) }}
              </span>
            </div>

            <div
              class="live-preview-stage"
              :class="'is-' + videoOrientation"
              :style="{ aspectRatio: String(videoAspectRatio) }"
            >
              <video
                ref="liveVideo"
                class="live-preview-frame"
                autoplay
                playsinline
                controls
                @loadedmetadata="handleVideoMetadata"
                @playing="handleVideoPlaying"
                @error="handleVideoError"
              ></video>

              <div v-if="videoState !== 'live'" class="live-preview-placeholder">
                <strong>
                  {{
                    previewPaused
                      ? '直播画面采集已暂停'
                      : videoState === 'error'
                        ? '直播画面连接失败，正在重试'
                        : room.status === 'live'
                          ? '正在连接直播视频流'
                          : '直播画面暂不可用'
                  }}
                </strong>
                <span>
                  {{
                    previewPaused
                      ? '点击“继续采集”后恢复画面；弹幕与直播状态仍保持连接'
                      : room.status === 'live'
                        ? '正在将抖音直播流转换为浏览器可播放画面'
                        : '房间进入直播状态后会自动显示画面'
                  }}
                </span>
              </div>


            </div>

            <div class="live-preview-actions">
              <button
                class="ghost-button live-preview-capture-toggle"
                :class="{ paused: previewPaused }"
                type="button"
                :disabled="room.status !== 'live' && !previewPaused"
                @click="toggleLivePreview"
              >
                <span class="live-preview-capture-icon">{{ previewPaused ? '▶' : 'Ⅱ' }}</span>
                {{ previewPaused ? '继续采集' : '暂停采集' }}
              </button>
              <a
                class="primary-button"
                :href="livePreviewUrl"
                target="_blank"
                rel="noreferrer"
              >
                打开原直播间 ↗
              </a>
            </div>

            <p class="live-preview-note">
              画面来自实时直播流，按需启动低延迟转码；暂停采集会停止本页视频拉流，继续后自动恢复。
            </p>
          </section>
        </aside>
      </section>

      <div
        v-if="legacyAgentSurfaceEnabled"
        ref="agentDockEl"
        class="floor-agent-dock"
        :class="{ expanded: agentDockExpanded, dragging: agentDockDragging }"
        :style="agentDockStyle"
      >
        <button
          v-if="!agentDockExpanded"
          class="floor-agent-orb"
          type="button"
          aria-label="打开场控 Agent 快捷对话"
          @click="expandAgentDock"
        >
          <span class="agent-orb-core">✦</span>
          <i class="agent-orb-ring ring-one"></i>
          <i class="agent-orb-ring ring-two"></i>
        </button>

        <div v-else class="floor-agent-inline">
          <button
            class="floor-agent-orb mini"
            type="button"
            aria-label="拖动或收起场控 Agent"
            @pointerdown="startAgentDockDrag"
            @click="handleAgentOrbClick"
          >
            <span class="agent-orb-core">✦</span>
            <i class="agent-orb-ring ring-one"></i>
            <i class="agent-orb-ring ring-two"></i>
          </button>
          <div class="floor-agent-inline-main">
            <div
              class="floor-agent-inline-preview"
              @pointerdown="startAgentDockDrag"
            >
              <strong>{{ agentSettings.display_name }}</strong>
              <span>{{ latestAgentMessage }}</span>
            </div>
            <input
              ref="agentDockInputEl"
              v-model="agentInput"
              type="text"
              :placeholder="'告诉' + agentSettings.display_name + '现在要做什么…'"
              @keydown="handleAgentInputKeydown"
            />
          </div>
          <button class="agent-inline-send" type="button" :disabled="!agentInput.trim() || agentChatBusy" @click="sendAgentCommand">{{ agentChatBusy ? '思考中…' : '发送' }}</button>
          <button class="agent-inline-close" type="button" aria-label="收起快捷对话" @click="collapseAgentDock">×</button>
        </div>
      </div>

      <div v-if="legacyAgentSurfaceEnabled && agentDrawerOpen" class="agent-drawer-backdrop" @click.self="agentDrawerOpen = false">
        <aside class="agent-drawer">
          <header>
            <div>
              <span class="section-kicker">LIVE COPILOT</span>
              <h3>{{ agentSettings.display_name }} · 场控协作</h3>
              <p>先沟通清楚，再进入直播执行决策。</p>
            </div>
            <button type="button" class="close-button" @click="agentDrawerOpen = false">×</button>
          </header>
          <section class="agent-drawer-chat">
            <article v-for="(message, index) in agentMessages" :key="index" :class="['agent-chat-message', message.role]">
              <strong>{{ message.role === 'agent' ? agentSettings.display_name : '我' }}</strong>
              <p>{{ message.text }}</p>
            </article>
          </section>
          <footer class="agent-drawer-composer">
            <textarea
              v-model="agentInput"
              rows="4"
              :placeholder="'继续告诉' + agentSettings.display_name + '你的现场要求……'"
              @keydown="handleAgentInputKeydown"
            ></textarea>
            <div>
              <span>Enter 发送 · Shift + Enter 换行</span>
              <button
                class="primary-button"
                type="button"
                :disabled="!agentInput.trim() || agentChatBusy"
                @click="sendAgentCommand"
              >
                {{ agentChatBusy ? '思考中…' : '发送' }}
              </button>
            </div>
          </footer>
        </aside>
      </div>
    </template>
  </div>
</template>
