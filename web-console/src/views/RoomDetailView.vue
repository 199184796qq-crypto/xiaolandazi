<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  getLiveAgentSettings,
  chatLiveAgent,
  getLiveRuntime,
  getLiveRuntimeEvents,
  getRoom,
  getRoomEvents,
  getTenants,
  recordLiveRuntimeEvent,
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
let eventSource: EventSource | null = null
let runtimePollTimer: number | undefined
let streamBatchTimer: number | undefined
let pendingStreamEvents: RoomEvent[] = []
const runtimeSnapshot = ref<LiveRuntimeSnapshot | null>(null)
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
type SimDeviceState = 'working' | 'connected' | 'offline'
const simulatedDeviceState = ref<SimDeviceState>(
  (localStorage.getItem('live-sim-device-' + roomId) as SimDeviceState) || 'offline',
)

const aiStatusText = computed(() => {
  if (aiRunning.value) return '工作中'
  return '等待开始'
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

function flushStreamEvents() {
	streamBatchTimer = undefined
	if (!pendingStreamEvents.length) return
	const incoming = pendingStreamEvents
	pendingStreamEvents = []
	const seen = new Set(events.value.map((item) => item.id))
	const additions: RoomEvent[] = []
	for (const event of incoming) {
		if (seen.has(event.id)) continue
		seen.add(event.id)
		additions.push(event)
	}
	if (!additions.length) return
	events.value = [...additions.reverse(), ...events.value].slice(0, 500)
}

function queueStreamEvent(event: RoomEvent) {
	pendingStreamEvents.push(event)
	if (streamBatchTimer === undefined) {
		streamBatchTimer = window.setTimeout(flushStreamEvents, 100)
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
    await refreshRuntime()
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
			queueStreamEvent(event)
			if (room.value) {
				room.value.status = 'live'
        room.value.last_event_at = event.occurred_at
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
	if (streamBatchTimer !== undefined) window.clearTimeout(streamBatchTimer)
	pendingStreamEvents = []
	finishAgentDockDrag()
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

      <section class="detail-stat-grid room-control-grid" aria-label="直播运行控制">
        <article class="runtime-stat-card companion-control-card">
          <span>直播搭子</span>
          <strong :class="aiRunning ? 'live-agent-status' : 'muted-value'">{{ aiStatusText }}</strong>
          <small>按当前直播智能体方案工作</small>
          <div class="companion-control-buttons" aria-label="直播搭子控制（设计阶段）">
            <button type="button" disabled>开始</button>
            <button type="button" class="pause" disabled>暂停</button>
            <button type="button" class="end" disabled>结束</button>
          </div>
        </article>
        <article class="device-runtime-card xiaozhi-device-card">
          <span>小智盒子</span>
          <div class="device-runtime-status" :class="'state-' + deviceVisualState">
            <i></i>
            <strong>{{ deviceStatusLabel }}</strong>
          </div>
          <small>{{ deviceStatusText }}</small>
          <div class="device-sim-controls">
            <button
              type="button"
              :class="{ active: deviceVisualState === 'connected' }"
              @click="setSimulatedDeviceState('connected')"
            >连接</button>
            <button
              type="button"
              :class="{ active: deviceVisualState === 'working' }"
              @click="setSimulatedDeviceState('working')"
            >工作</button>
            <button
              type="button"
              class="close"
              :class="{ active: deviceVisualState === 'offline' }"
              @click="setSimulatedDeviceState('offline')"
            >断开</button>
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

      <section class="detail-layout detail-layout-no-preview">
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

<style scoped>
.room-detail-page .room-control-grid {
  grid-template-columns: repeat(2, minmax(280px, 430px));
  justify-content: start;
  gap: 12px;
}

.room-detail-page .room-control-grid > article {
  min-height: 138px;
  padding: 16px 18px;
}

.companion-control-card,
.xiaozhi-device-card {
  align-content: start;
}

.companion-control-card > strong,
.xiaozhi-device-card .device-runtime-status strong {
  font-size: 20px;
  line-height: 1.25;
}

.companion-control-card > small,
.xiaozhi-device-card > small {
  min-height: 18px;
}

.companion-control-buttons {
  display: flex;
  gap: 8px;
  margin-top: 14px;
}

.companion-control-buttons button {
  min-width: 68px;
  height: 34px;
  border: 1px solid #d9e0ec;
  border-radius: 9px;
  color: #657186;
  background: #fff;
  font: inherit;
  font-size: 13px;
  font-weight: 800;
}

.companion-control-buttons button:first-child {
  border-color: #cbd5ff;
  color: #4f46e5;
  background: #f3f4ff;
}

.companion-control-buttons button.end {
  color: #8b5a60;
  background: #fff7f7;
}

.companion-control-buttons button:disabled {
  opacity: .78;
}

.room-detail-page .detail-layout-no-preview {
  grid-template-columns: minmax(0, 1.18fr) minmax(360px, .82fr) !important;
}

@media(max-width:1100px){
  .room-detail-page .room-control-grid {
    grid-template-columns: repeat(2, minmax(260px, 1fr));
  }
}

@media(max-width:900px){
  .room-detail-page .detail-layout-no-preview {
    grid-template-columns: 1fr !important;
  }
}

@media(max-width:800px){
  .room-detail-page .room-control-grid {
    grid-template-columns: 1fr;
  }
}
</style>
