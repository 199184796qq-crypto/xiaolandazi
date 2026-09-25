<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  getLiveAgentSettings,
  getLiveAgentPlans,
  getRoomLiveAgentPlan,
  bindRoomLiveAgentPlan,
  chatLiveAgent,
  controlLiveDevice,
  getLiveRuntime,
  setLiveRuntimeMode,
  getLiveRuntimeEvents,
  startLiveRuntime,
  stopLiveRuntime,
  getRoom,
  getRoomEvents,
  getRoomImportantEvents,
  getRoomSessionStats,
  getRoomReview,
  resolveRoomSessionDecision,
  getRoomBrain,
  getRoomSpeechRuntime,
  getRoomAgentDecisions,
  enqueueRoomManualAgentDecision,
  removeRoomAgentDecision,
  getRoomBlockedUsers,
  blockRoomUser,
  restoreRoomBlockedUser,
  getTenants,
  recordLiveRuntimeEvent,
} from '../api'
import { session } from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import type {
  LiveAgentSettings,
  LiveAgentPlan,
  LiveRuntimeSnapshot,
  Room,
  RoomBlockedUser,
  RoomBrainTopic,
  RoomBrainQuestion,
  RoomBrainView,
  RoomEvent,
  RoomSessionStats,
  LiveReviewResponse,
  SpeechRuntimeSnapshot,
  SpeechTrackRuntime,
  AgentDecisionSnapshot,
  AgentDecisionItem,
  Tenant,
} from '../types'

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
type EventDecisionAction = 'quick' | 'answer'
type EventDecisionRowState = {
  busy?: EventDecisionAction
  submitted?: EventDecisionAction
  message?: string
  error?: string
}
const eventDecisionRows = ref<Record<number, EventDecisionRowState>>({})
const importantHistory = ref<Record<string, RoomEvent[]>>({})
const importantCursor = ref<Record<string, number>>({})
const importantHasMore = ref<Record<string, boolean>>({})
const importantLoading = ref<Record<string, boolean>>({})
const eventListEl = ref<HTMLElement | null>(null)
const publicScreenPanelEl = ref<HTMLElement | null>(null)
const publicScreenHeight = ref<number | null>(null)
const publicScreenResizing = ref(false)
const PUBLIC_SCREEN_MIN_ROWS = 20
const PUBLIC_SCREEN_BASE_ROW_HEIGHT = 62
const PUBLIC_SCREEN_CHROME_HEIGHT = 180
const PUBLIC_SCREEN_MIN_HEIGHT = PUBLIC_SCREEN_MIN_ROWS * PUBLIC_SCREEN_BASE_ROW_HEIGHT + PUBLIC_SCREEN_CHROME_HEIGHT
const PUBLIC_SCREEN_MAX_HEIGHT = 3600
const PUBLIC_SCREEN_HEIGHT_STORAGE_KEY = 'livecompanion.public-screen-height.v1'
let publicScreenResizeState: { startY: number; startHeight: number; pointerId: number } | null = null
const sessionStats = ref<RoomSessionStats | null>(null)
const liveReview = ref<LiveReviewResponse | null>(null)
const liveReviewOpen = ref(false)
const liveReviewLoading = ref(false)
const liveReviewError = ref('')
const sessionDecisionBusy = ref(false)
const loading = ref(true)
const error = useFeedbackErrorRef()
const streamState = ref<'connecting' | 'online' | 'offline'>('connecting')
const activeType = ref('all')
let eventSource: EventSource | null = null
type LocalAudioTask = {
  speech_task_id: string
  room_id: number
  session_id: string
  kind: string
  label: string
  audio_url: string
  duration_ms: number
  created_at?: string
}
let localAudioEventSource: EventSource | null = null
let localAudioContext: AudioContext | null = null
let localAudioSource: AudioBufferSourceNode | null = null
let localAudioTask: LocalAudioTask | null = null
let localAudioProgressTimer: number | undefined
let localAudioHeartbeatTimer: number | undefined
let localAudioRegisteredRoomID = 0
let localAudioPlaybackGeneration = 0
const localAudioState = ref<'disconnected' | 'connected' | 'playing' | 'error'>('disconnected')
const localAudioError = ref('')
const LOCAL_AUDIO_RECEIVER_KEY = 'livecompanion.web-audio-receiver.v1'
let runtimePollTimer: number | undefined
let sessionStatsPollTimer: number | undefined
let speechPollTimer: number | undefined
let agentDecisionPollTimer: number | undefined
let streamBatchTimer: number | undefined
let pendingStreamEvents: RoomEvent[] = []
let dashboardTickTimer: number | undefined
let pageBottomRestoreFrame: number | undefined
const PAGE_BOTTOM_PIN_THRESHOLD = 80

function pageIsPinnedToBottom() {
  const root = document.documentElement
  const scrollHeight = Math.max(root.scrollHeight, document.body?.scrollHeight || 0)
  return scrollHeight - (window.scrollY + window.innerHeight) <= PAGE_BOTTOM_PIN_THRESHOLD
}

function restorePageBottomAfterRender(wasPinned: boolean) {
  if (!wasPinned) return
  void nextTick(() => {
    if (pageBottomRestoreFrame !== undefined) {
      window.cancelAnimationFrame(pageBottomRestoreFrame)
    }
    pageBottomRestoreFrame = window.requestAnimationFrame(() => {
      pageBottomRestoreFrame = undefined
      const root = document.documentElement
      const scrollHeight = Math.max(root.scrollHeight, document.body?.scrollHeight || 0)
      window.scrollTo({
        top: Math.max(0, scrollHeight - window.innerHeight),
        left: window.scrollX,
        behavior: 'auto',
      })
    })
  })
}

function updatePreservingPageBottom(update: () => void) {
  const wasPinned = pageIsPinnedToBottom()
  update()
  restorePageBottomAfterRender(wasPinned)
}
const dashboardNow = ref(Date.now())
type FlowSample = { at: number; type: string; weight: number }
const flowSamples = ref<FlowSample[]>([])
const runtimeSnapshot = ref<LiveRuntimeSnapshot | null>(null)
const runtimeError = ref('')
const runtimeControlBusy = ref(false)
const runtimeModeBusy = ref(false)
const liveAgentPlans = ref<LiveAgentPlan[]>([])
const selectedLiveAgentPlanId = ref(0)
const agentPlanBusy = ref(false)
const agentPlanError = ref('')
const roomBrain = ref<RoomBrainView | null>(null)
const speechRuntime = ref<SpeechRuntimeSnapshot | null>(null)
const agentDecisionState = ref<AgentDecisionSnapshot | null>(null)
const questionDecisionBusy = ref<Record<number, EventDecisionAction | undefined>>({})
const bucketDecisionBusy = ref<Record<string, EventDecisionAction | undefined>>({})
const agentDecisionActionMessage = ref('')
const agentDecisionLastSuppressed = ref(false)
const agentDecisionPanelEl = ref<HTMLElement | null>(null)
const agentPanelHeight = ref<number | null>(null)
const agentThinkingHeight = ref<number | null>(null)
const agentPanelResizing = ref<'split' | 'panel' | null>(null)
const AGENT_PANEL_DEFAULT_HEIGHT = 520
const AGENT_PANEL_MIN_HEIGHT = 360
const AGENT_PANEL_MAX_HEIGHT = 1200
const AGENT_THINKING_DEFAULT_HEIGHT = 150
const AGENT_THINKING_MIN_HEIGHT = 80
const AGENT_INTERRUPT_MIN_HEIGHT = 145
const AGENT_PANEL_HEIGHT_STORAGE_KEY = 'livecompanion.agent-panel-height.v1'
const AGENT_THINKING_HEIGHT_STORAGE_KEY = 'livecompanion.agent-thinking-height.v1'
let agentPanelResizeState:
  | {
      mode: 'split' | 'panel'
      pointerId: number
      startY: number
      startPanelHeight: number
      startThinkingHeight: number
      startInterruptHeight: number
      fixedHeight: number
      ratio: number
    }
  | null = null
const agentDecisionPanelStyle = computed<Record<string, string>>(() => ({
  height: (agentPanelHeight.value || AGENT_PANEL_DEFAULT_HEIGHT) + 'px',
  minHeight: AGENT_PANEL_MIN_HEIGHT + 'px',
  '--agent-thinking-height': (agentThinkingHeight.value || AGENT_THINKING_DEFAULT_HEIGHT) + 'px',
}))

function clampAgentPanelHeight(value: number) {
  return Math.min(AGENT_PANEL_MAX_HEIGHT, Math.max(AGENT_PANEL_MIN_HEIGHT, Math.round(value)))
}

function restoreAgentPanelLayout() {
  const savedPanel = Number(window.localStorage.getItem(AGENT_PANEL_HEIGHT_STORAGE_KEY) || '')
  const savedThinking = Number(window.localStorage.getItem(AGENT_THINKING_HEIGHT_STORAGE_KEY) || '')
  agentPanelHeight.value = clampAgentPanelHeight(
    Number.isFinite(savedPanel) && savedPanel > 0 ? savedPanel : AGENT_PANEL_DEFAULT_HEIGHT,
  )
  agentThinkingHeight.value = Math.max(
    AGENT_THINKING_MIN_HEIGHT,
    Number.isFinite(savedThinking) && savedThinking > 0 ? Math.round(savedThinking) : AGENT_THINKING_DEFAULT_HEIGHT,
  )
}

function beginAgentResize(event: PointerEvent, mode: 'split' | 'panel') {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  const panel = agentDecisionPanelEl.value
  if (!panel) return
  const thinking = panel.querySelector<HTMLElement>('.agent-thinking-zone')
  const interrupt = panel.querySelector<HTMLElement>('.agent-interrupt-zone')
  if (!thinking || !interrupt) return
  const panelHeight = panel.getBoundingClientRect().height
  const thinkingHeight = thinking.getBoundingClientRect().height
  const interruptHeight = interrupt.getBoundingClientRect().height
  const flexibleHeight = Math.max(1, thinkingHeight + interruptHeight)
  agentPanelResizeState = {
    mode,
    pointerId: event.pointerId,
    startY: event.clientY,
    startPanelHeight: panelHeight,
    startThinkingHeight: thinkingHeight,
    startInterruptHeight: interruptHeight,
    fixedHeight: Math.max(0, panelHeight - flexibleHeight),
    ratio: thinkingHeight / flexibleHeight,
  }
  agentPanelResizing.value = mode
  document.body.style.userSelect = 'none'
  document.body.style.cursor = 'ns-resize'
}

function startAgentSplitResize(event: PointerEvent) {
  beginAgentResize(event, 'split')
}

function startAgentPanelResize(event: PointerEvent) {
  beginAgentResize(event, 'panel')
}

function moveAgentPanelResize(event: PointerEvent) {
  const state = agentPanelResizeState
  if (!state || event.pointerId !== state.pointerId) return
  const delta = event.clientY - state.startY
  if (state.mode === 'split') {
    const maxThinking = Math.max(
      AGENT_THINKING_MIN_HEIGHT,
      state.startPanelHeight - state.fixedHeight - AGENT_INTERRUPT_MIN_HEIGHT,
    )
    agentThinkingHeight.value = Math.min(
      maxThinking,
      Math.max(AGENT_THINKING_MIN_HEIGHT, Math.round(state.startThinkingHeight + delta)),
    )
    return
  }

  const nextPanelHeight = clampAgentPanelHeight(state.startPanelHeight + delta)
  const available = Math.max(
    AGENT_THINKING_MIN_HEIGHT + AGENT_INTERRUPT_MIN_HEIGHT,
    nextPanelHeight - state.fixedHeight,
  )
  const maxThinking = Math.max(AGENT_THINKING_MIN_HEIGHT, available - AGENT_INTERRUPT_MIN_HEIGHT)
  agentPanelHeight.value = nextPanelHeight
  agentThinkingHeight.value = Math.min(
    maxThinking,
    Math.max(AGENT_THINKING_MIN_HEIGHT, Math.round(available * state.ratio)),
  )
}

function finishAgentPanelResize(event?: PointerEvent) {
  const state = agentPanelResizeState
  if (!state) return
  if (event && event.pointerId !== state.pointerId) return
  agentPanelResizeState = null
  agentPanelResizing.value = null
  document.body.style.userSelect = ''
  document.body.style.cursor = ''
  if (agentPanelHeight.value) {
    window.localStorage.setItem(AGENT_PANEL_HEIGHT_STORAGE_KEY, String(agentPanelHeight.value))
  }
  if (agentThinkingHeight.value) {
    window.localStorage.setItem(AGENT_THINKING_HEIGHT_STORAGE_KEY, String(agentThinkingHeight.value))
  }
}

const eventBucketPanelEl = ref<HTMLElement | null>(null)
const eventBucketHeight = ref<number | null>(null)
const eventBucketResizing = ref(false)
const EVENT_BUCKET_MIN_HEIGHT = 420
const EVENT_BUCKET_MAX_HEIGHT = 1800
const EVENT_BUCKET_HEIGHT_STORAGE_KEY = 'livecompanion.event-bucket-height.v1'
let eventBucketResizeState: { startY: number; startHeight: number; pointerId: number } | null = null

const eventBucketPanelStyle = computed<Record<string, string> | undefined>(() => {
  if (eventBucketHeight.value === null) return undefined
  return {
    height: eventBucketHeight.value + 'px',
    minHeight: EVENT_BUCKET_MIN_HEIGHT + 'px',
  }
})

function clampEventBucketHeight(value: number) {
  return Math.min(EVENT_BUCKET_MAX_HEIGHT, Math.max(EVENT_BUCKET_MIN_HEIGHT, Math.round(value)))
}

function restoreEventBucketHeight() {
  const saved = Number(window.localStorage.getItem(EVENT_BUCKET_HEIGHT_STORAGE_KEY) || '')
  if (Number.isFinite(saved) && saved > 0) {
    eventBucketHeight.value = clampEventBucketHeight(saved)
  }
}

function startEventBucketResize(event: PointerEvent) {
  if (event.button !== 0) return
  event.preventDefault()
  event.stopPropagation()
  const panel = eventBucketPanelEl.value
  if (!panel) return
  const startHeight = panel.getBoundingClientRect().height
  eventBucketHeight.value = clampEventBucketHeight(startHeight)
  eventBucketResizeState = {
    startY: event.clientY,
    startHeight,
    pointerId: event.pointerId,
  }
  eventBucketResizing.value = true
  document.body.style.userSelect = 'none'
  document.body.style.cursor = 'ns-resize'
}

function moveEventBucketResize(event: PointerEvent) {
  if (!eventBucketResizeState || event.pointerId !== eventBucketResizeState.pointerId) return
  eventBucketHeight.value = clampEventBucketHeight(
    eventBucketResizeState.startHeight + event.clientY - eventBucketResizeState.startY,
  )
}

function finishEventBucketResize(event?: PointerEvent) {
  if (!eventBucketResizeState) return
  if (event && event.pointerId !== eventBucketResizeState.pointerId) return
  eventBucketResizeState = null
  eventBucketResizing.value = false
  document.body.style.userSelect = ''
  document.body.style.cursor = ''
  if (eventBucketHeight.value !== null) {
    window.localStorage.setItem(EVENT_BUCKET_HEIGHT_STORAGE_KEY, String(eventBucketHeight.value))
  }
}

const blockedUsers = ref<RoomBlockedUser[]>([])
const blockedDrawerOpen = ref(false)
const blockedBusyKey = ref('')
const moderationError = ref('')
const hoveredEventId = ref<number | null>(null)
const eventContextMenu = ref<{ x: number; y: number; event: RoomEvent } | null>(null)
const agentDecisionContextMenu = ref<{ x: number; y: number; item: AgentDecisionItem } | null>(null)
const agentDecisionRemoveBusy = ref('')
const streamPaused = computed(() => hoveredEventId.value !== null || eventContextMenu.value !== null)
const publicScreenPanelStyle = computed(() => ({
  height: (publicScreenHeight.value || PUBLIC_SCREEN_MIN_HEIGHT) + 'px',
  minHeight: PUBLIC_SCREEN_MIN_HEIGHT + 'px',
}))

function clampPublicScreenHeight(value: number) {
  return Math.min(PUBLIC_SCREEN_MAX_HEIGHT, Math.max(PUBLIC_SCREEN_MIN_HEIGHT, Math.round(value)))
}

function restorePublicScreenHeight() {
  const saved = Number(window.localStorage.getItem(PUBLIC_SCREEN_HEIGHT_STORAGE_KEY) || '')
  publicScreenHeight.value = clampPublicScreenHeight(Number.isFinite(saved) && saved > 0 ? saved : PUBLIC_SCREEN_MIN_HEIGHT)
}

function startPublicScreenResize(event: PointerEvent) {
  if (event.button !== 0) return
  event.preventDefault()
  const panel = publicScreenPanelEl.value
  if (!panel) return
  publicScreenResizeState = {
    startY: event.clientY,
    startHeight: panel.getBoundingClientRect().height,
    pointerId: event.pointerId,
  }
  publicScreenResizing.value = true
  document.body.style.userSelect = 'none'
  document.body.style.cursor = 'ns-resize'
}

function movePublicScreenResize(event: PointerEvent) {
  if (!publicScreenResizeState || event.pointerId !== publicScreenResizeState.pointerId) return
  publicScreenHeight.value = clampPublicScreenHeight(
    publicScreenResizeState.startHeight + event.clientY - publicScreenResizeState.startY,
  )
}

function finishPublicScreenResize(event?: PointerEvent) {
  if (!publicScreenResizeState) return
  if (event && event.pointerId !== publicScreenResizeState.pointerId) return
  publicScreenResizeState = null
  publicScreenResizing.value = false
  document.body.style.userSelect = ''
  document.body.style.cursor = ''
  if (publicScreenHeight.value) {
    window.localStorage.setItem(PUBLIC_SCREEN_HEIGHT_STORAGE_KEY, String(publicScreenHeight.value))
  }
}
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
  { time: '09:48:30', kind: '观察', text: '等待直播间事件，问题会立即进入问题聚类，相似提问自动合并。' },
  { time: '09:48:30', kind: '计划', text: '保持当前产品主线，不主动打断主播。' },
])
const mainlineSpeech = computed<SpeechTrackRuntime>(() =>
  speechRuntime.value?.mainline || { status: 'idle' },
)
const interruptSpeech = computed<SpeechTrackRuntime>(() =>
  speechRuntime.value?.interrupt || { status: 'idle' },
)
const effectiveMainlineStatus = computed(() => {
  const mainlineStatus = (mainlineSpeech.value.status || 'idle').toLowerCase()
  const interruptStatus = (interruptSpeech.value.status || 'idle').toLowerCase()
  if (interruptStatus === 'playing' && mainlineStatus === 'playing') return 'paused'
  return mainlineStatus
})
const displayMainlineStatus = computed(() => {
  const mode = runtimeSnapshot.value?.agent_mode || 'control'
  if (mode === 'control' && ['playing', 'paused', 'ready'].includes(effectiveMainlineStatus.value)) {
    return 'idle'
  }
  return effectiveMainlineStatus.value
})
const speechTrackLayoutState = computed<'balanced' | 'mainline' | 'interrupt'>(() => {
  const interruptStatus = (interruptSpeech.value.status || 'idle').toLowerCase()
  if (interruptStatus === 'playing') return 'interrupt'
  if (displayMainlineStatus.value === 'playing') return 'mainline'
  return 'balanced'
})
const speechRuntimeSummary = computed(() => {
  const interruptStatus = (interruptSpeech.value.status || 'idle').toLowerCase()
  if (interruptStatus === 'playing') return '临时插播中，主线等待恢复'
  if (displayMainlineStatus.value === 'playing') return '主线口播中'
  if (displayMainlineStatus.value === 'paused') return '主线已暂停，等待恢复'
  if (displayMainlineStatus.value === 'ready') return '主线已就绪，等待播放'
  return '等待口播任务…'
})

function speechStatusLabel(status: string, track: 'mainline' | 'interrupt') {
  switch ((status || 'idle').toLowerCase()) {
    case 'ready': return track === 'interrupt' ? '待插播' : '待播放'
    case 'playing': return track === 'interrupt' ? '插播中' : '播放中'
    case 'paused': return '已暂停'
    case 'completed': return '已完成'
    case 'failed': return '异常'
    default: return '等待'
  }
}

const aiRuntimeStatus = computed(() => runtimeSnapshot.value?.agent_state || 'stopped')
const aiRuntimeMode = computed<'control' | 'anchor'>(() => runtimeSnapshot.value?.agent_mode || 'control')
const aiRunning = computed(() => aiRuntimeStatus.value === 'working')
const aiPaused = computed(() => aiRuntimeStatus.value === 'paused')
const aiActive = computed(() => aiRuntimeStatus.value === 'working' || aiRuntimeStatus.value === 'paused')
const aiSessionActive = computed(() => {
  const status = runtimeSnapshot.value?.session?.status || 'stopped'
  return status === 'running' || status === 'paused'
})
const boundDevice = computed(() => runtimeSnapshot.value?.device || null)
type DeviceVisualState = 'working' | 'paused' | 'offline'
const deviceControlBusy = ref(false)
const deviceControlError = ref('')

async function loadLiveAgentPlansForRoom() {
  const currentRoom = room.value
  if (!currentRoom) return
  agentPlanError.value = ''
  try {
    const [listResult, currentResult] = await Promise.all([
      getLiveAgentPlans(currentRoom.tenant_id),
      getRoomLiveAgentPlan(roomId),
    ])
    liveAgentPlans.value = (listResult.items || []).filter((item) => item.status === 'active')
    selectedLiveAgentPlanId.value = currentResult.plan?.id || 0
  } catch (err) {
    liveAgentPlans.value = []
    selectedLiveAgentPlanId.value = 0
    agentPlanError.value = err instanceof Error ? err.message : '读取智能体直播方案失败'
  }
}

async function changeLiveAgentPlan(event: Event) {
  const currentRoom = room.value
  if (!currentRoom || agentPlanBusy.value) return
  const target = event.target as HTMLSelectElement
  const planId = Number(target.value)
  if (!Number.isFinite(planId) || planId <= 0 || planId === selectedLiveAgentPlanId.value) return
  const previous = selectedLiveAgentPlanId.value
  agentPlanBusy.value = true
  agentPlanError.value = ''
  try {
    await bindRoomLiveAgentPlan(planId, roomId, currentRoom.tenant_id)
    selectedLiveAgentPlanId.value = planId
    const plan = liveAgentPlans.value.find((item) => item.id === planId)
    logUserAction('LIVE_AGENT_PLAN_SELECTED', {
      plan_id: planId,
      plan_name: plan?.name || '',
    })
  } catch (err) {
    selectedLiveAgentPlanId.value = previous
    target.value = String(previous || '')
    agentPlanError.value = err instanceof Error ? err.message : '切换智能体直播方案失败'
  } finally {
    agentPlanBusy.value = false
  }
}

async function setCompanionMode(mode: 'control' | 'anchor') {
  if (runtimeModeBusy.value || aiRuntimeMode.value === mode) return
  if (mode === 'anchor' && !selectedLiveAgentPlanId.value) {
    runtimeError.value = '主播模式需要先选择智能体直播方案'
    return
  }
  runtimeModeBusy.value = true
  runtimeError.value = ''
  try {
    await setLiveRuntimeMode(roomId, mode)
    await Promise.all([refreshRuntime(), refreshAgentDecisions()])
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '切换直播搭子模式失败'
  } finally {
    runtimeModeBusy.value = false
  }
}

const deviceVisualState = computed<DeviceVisualState>(() => {
  const device = boundDevice.value
  if (!device || device.connection_status !== 'online') return 'offline'
  if (device.work_status === 'paused') return 'paused'
  return 'working'
})

const deviceStatusText = computed(() => {
  const prefix = boundDevice.value?.sn || '小蓝盒子'
  if (deviceVisualState.value === 'working') return prefix + ' · 已连接，工作中'
  if (deviceVisualState.value === 'paused') return prefix + ' · 已连接，已暂停'
  return prefix + ' · 离线'
})

const deviceStatusLabel = computed(() => {
  if (deviceVisualState.value === 'working') return '已连接'
  if (deviceVisualState.value === 'paused') return '已暂停'
  return '离线'
})

const expandedQuestionTopic = ref('')
const questionClusterCollapsed = ref(false)
const selectedQuestionEventId = ref<number | null>(null)
const questionClusterHovered = ref(false)
const frozenSemanticBuckets = ref<RoomBrainTopic[] | null>(null)
const questionClusterFrozen = computed(
  () => questionClusterHovered.value || Boolean(expandedQuestionTopic.value),
)

function cloneQuestionBucket(bucket: RoomBrainTopic): RoomBrainTopic {
  return {
    ...bucket,
    SampleQuestions: bucket.SampleQuestions ? [...bucket.SampleQuestions] : bucket.SampleQuestions,
    Questions: bucket.Questions?.map((question) => ({ ...question })),
    TTSQuestions: (bucket.TTSQuestions || []).map((question) => ({ ...question })),
  }
}

const liveSemanticBuckets = computed(() =>
  [...(roomBrain.value?.Intelligence?.TopTopics || [])]
    .filter((bucket) => bucket.Count > 0)
    .sort((a, b) => {
      if (a.Count !== b.Count) return b.Count - a.Count
      return Date.parse(b.LastSeenAt || '') - Date.parse(a.LastSeenAt || '')
    }),
)

const semanticBuckets = computed(() => frozenSemanticBuckets.value || liveSemanticBuckets.value)

function ensureQuestionClusterSnapshot() {
  if (!frozenSemanticBuckets.value) {
    frozenSemanticBuckets.value = liveSemanticBuckets.value.map(cloneQuestionBucket)
  }
}

function freezeQuestionCluster() {
  questionClusterHovered.value = true
  ensureQuestionClusterSnapshot()
}

function unfreezeQuestionCluster() {
  questionClusterHovered.value = false
  if (!expandedQuestionTopic.value) frozenSemanticBuckets.value = null
}

function toggleQuestionClusterCollapsed() {
  questionClusterCollapsed.value = !questionClusterCollapsed.value
  if (!questionClusterCollapsed.value) return
  expandedQuestionTopic.value = ''
  selectedQuestionEventId.value = null
  if (!questionClusterHovered.value) frozenSemanticBuckets.value = null
}

function toggleQuestionBucket(bucket: RoomBrainTopic) {
  if (expandedQuestionTopic.value === bucket.Topic) {
    expandedQuestionTopic.value = ''
    selectedQuestionEventId.value = null
    if (!questionClusterHovered.value) frozenSemanticBuckets.value = null
    return
  }
  ensureQuestionClusterSnapshot()
  expandedQuestionTopic.value = bucket.Topic
  selectedQuestionEventId.value = null
}

function selectQuestionDetail(question: RoomBrainQuestion) {
  selectedQuestionEventId.value = question.EventID || null
  agentDecisionActionMessage.value = ''
  agentDecisionLastSuppressed.value = false
}

const selectedQuestionDetail = computed<RoomBrainQuestion | null>(() => {
  if (!expandedQuestionTopic.value || !selectedQuestionEventId.value) return null
  const bucket = semanticBuckets.value.find((item) => item.Topic === expandedQuestionTopic.value)
  return bucket?.Questions?.find((item) => item.EventID === selectedQuestionEventId.value) || null
})

const selectedQuestionBucket = computed(() =>
  semanticBuckets.value.find((item) => item.Topic === expandedQuestionTopic.value) || null,
)

function questionTTSEligible(bucket: RoomBrainTopic, question: RoomBrainQuestion) {
  const occurredAt = Date.parse(question.OccurredAt || '')
  if (!Number.isFinite(occurredAt) || dashboardNow.value - occurredAt > 30 * 60 * 1000) return false
  return Boolean(bucket.TTSQuestions?.some((item) => item.EventID === question.EventID))
}

function questionBucketTTSEligible(bucket: RoomBrainTopic) {
  return Boolean((bucket.TTSEligibleCount || 0) > 0 && (bucket.TTSQuestions?.length || 0) > 0)
}

const selectedQuestionTTSEligible = computed(() => {
  const question = selectedQuestionDetail.value
  const bucket = selectedQuestionBucket.value
  if (!question || !bucket) return false
  return questionTTSEligible(bucket, question)
})

const agentDecisionQueue = computed(() => agentDecisionState.value?.queue || [])
const agentDecisionNotes = computed(() => (agentDecisionState.value?.notes || []).slice(0, 6))

function agentDecisionSourceLabel(item: AgentDecisionItem) {
  return item.sources?.includes('manual') ? '人工' : 'Agent'
}

function agentDecisionPriorityLabel(item: AgentDecisionItem) {
  if (item.manual_action === 'quick') return '抢答优先'
  if (item.sources?.includes('manual')) return '人工回答'
  if (item.priority >= 80) return '高优先'
  if (item.priority >= 55) return '中优先'
  return '自然排队'
}

function agentDecisionStateLabel(state?: string) {
  if (!aiRunning.value) return '未工作'
  if (state === 'READY_TO_INTERRUPT') return '准备打断'
  return '扫描中'
}

function agentDecisionNoteLabel(kind: string) {
  const labels: Record<string, string> = {
    manual_enqueue: '人工回答',
    manual_promote: '人工融合',
    manual_quick: '人工抢答',
    agent_enqueue: 'Agent发现',
    merge: '自动融合',
    cooldown: '回答冷却',
    answered: '已回答',
    expired: '自动抛出',
    capacity_drop: '容量淘汰',
    claim: '准备执行',
  }
  return labels[kind] || '判断'
}

function agentDecisionExpiryText(expiresAt: string) {
  const remain = Math.max(0, Math.ceil((Date.parse(expiresAt) - dashboardNow.value) / 1000))
  const minutes = Math.floor(remain / 60)
  const seconds = remain % 60
  return String(minutes).padStart(2, '0') + ':' + String(seconds).padStart(2, '0')
}

function openAgentDecisionContextMenu(mouseEvent: MouseEvent, item: AgentDecisionItem) {
  agentDecisionContextMenu.value = {
    x: Math.max(10, Math.min(mouseEvent.clientX, window.innerWidth - 220)),
    y: Math.max(10, Math.min(mouseEvent.clientY, window.innerHeight - 120)),
    item,
  }
}

function closeAgentDecisionContextMenu() {
  agentDecisionContextMenu.value = null
}

async function removeAgentDecisionFromQueue() {
  const item = agentDecisionContextMenu.value?.item
  if (!item || agentDecisionRemoveBusy.value) return
  agentDecisionRemoveBusy.value = item.id
  agentDecisionActionMessage.value = ''
  try {
    await removeRoomAgentDecision(roomId, item.id)
    agentDecisionActionMessage.value = '已从待打断队列移除。'
    closeAgentDecisionContextMenu()
    await refreshAgentDecisions()
  } catch (err) {
    agentDecisionActionMessage.value = err instanceof Error ? err.message : '移除待打断任务失败'
    await refreshAgentDecisions()
  } finally {
    agentDecisionRemoveBusy.value = ''
  }
}

const semanticBucketTones = ['violet', 'rose', 'blue', 'orange', 'cyan', 'green', 'indigo', 'red', 'teal', 'purple']

type MascotMouthState = 'smile' | 'flat' | 'open'
type MascotBrowState = 'neutral' | 'left-up' | 'right-up' | 'both-up'
type MascotMotionState = 'idle' | 'hop' | 'tilt-left' | 'tilt-right'
const mascotBlinkLeft = ref(false)
const mascotBlinkRight = ref(false)
const mascotMouthState = ref<MascotMouthState>('smile')
const mascotBrowState = ref<MascotBrowState>('neutral')
const mascotMotionState = ref<MascotMotionState>('idle')
let mascotActionTimer: number | undefined
let mascotResetTimers: number[] = []

function mascotResetLater(callback: () => void, delay: number) {
  const timer = window.setTimeout(() => {
    mascotResetTimers = mascotResetTimers.filter((item) => item !== timer)
    callback()
  }, delay)
  mascotResetTimers.push(timer)
}

function runMascotAction() {
  const action = Math.floor(Math.random() * 6)
  if (action === 0) {
    mascotBlinkLeft.value = true
    mascotBlinkRight.value = true
    mascotResetLater(() => {
      mascotBlinkLeft.value = false
      mascotBlinkRight.value = false
    }, 150)
  } else if (action === 1) {
    const left = Math.random() > 0.5
    mascotBlinkLeft.value = left
    mascotBlinkRight.value = !left
    mascotResetLater(() => {
      mascotBlinkLeft.value = false
      mascotBlinkRight.value = false
    }, 135)
  } else if (action === 2) {
    const mouths: MascotMouthState[] = ['smile', 'flat', 'open']
    mascotMouthState.value = mouths[Math.floor(Math.random() * mouths.length)]
  } else if (action === 3) {
    const brows: MascotBrowState[] = ['left-up', 'right-up', 'both-up']
    mascotBrowState.value = brows[Math.floor(Math.random() * brows.length)]
    mascotResetLater(() => { mascotBrowState.value = 'neutral' }, 700)
  } else {
    const motions: MascotMotionState[] = ['hop', 'tilt-left', 'tilt-right']
    mascotMotionState.value = motions[Math.floor(Math.random() * motions.length)]
    mascotResetLater(() => { mascotMotionState.value = 'idle' }, 760)
  }
  mascotActionTimer = window.setTimeout(runMascotAction, 1800 + Math.random() * 2800)
}

function stopMascotActions() {
  if (mascotActionTimer !== undefined) window.clearTimeout(mascotActionTimer)
  mascotResetTimers.forEach((timer) => window.clearTimeout(timer))
  mascotResetTimers = []
}

function semanticBucketLabel(bucket: RoomBrainTopic) {
  if (bucket.Topic.startsWith('FAMILY:')) return bucket.Topic.slice('FAMILY:'.length)
  return bucket.SampleQuestions?.[0] || bucket.Topic
}

function semanticBucketTone(bucket: RoomBrainTopic) {
  const source = semanticBucketLabel(bucket)
  let hash = 0
  for (let index = 0; index < source.length; index += 1) hash = ((hash * 31) + source.charCodeAt(index)) >>> 0
  return semanticBucketTones[hash % semanticBucketTones.length]
}

const FLOW_WINDOW_MS = 60_000
const IMPORTANT_EVENT_TYPES = new Set(['chat', 'like', 'follow', 'gift'])
const IMPORTANT_PAGE_SIZE = 300

function formatClockSeconds(value?: number) {
  const totalSeconds = Math.max(0, Math.floor(value || 0))
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  return [hours, minutes, seconds].map((part) => String(part).padStart(2, '0')).join(':')
}

function formatClockMinutes(value?: number) {
  const totalMinutes = Math.max(0, Math.floor((value || 0) / 60))
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return [hours, minutes].map((part) => String(part).padStart(2, '0')).join(':')
}

const sessionDurationText = computed(() => formatClockMinutes(sessionStats.value?.live_seconds))

function eventWeight(event: RoomEvent) {
  if (event.event_type !== 'like') return 1
  const content = event.content || ''
  const match = content.match(/[×xX*]\s*(\d+)/)
  return match ? Math.max(1, Number(match[1]) || 1) : 1
}

function pushFlowSample(event: RoomEvent) {
  if (event.event_type === 'room') return
  const parsed = Date.parse(event.occurred_at)
  flowSamples.value.push({
    at: Number.isFinite(parsed) ? parsed : Date.now(),
    type: event.event_type,
    weight: eventWeight(event),
  })
  pruneFlowSamples()
}

function pruneFlowSamples() {
  const cutoff = dashboardNow.value - FLOW_WINDOW_MS
  if (!flowSamples.value.length || flowSamples.value[0].at >= cutoff) return
  flowSamples.value = flowSamples.value.filter((sample) => sample.at >= cutoff)
}

const flowStats = computed(() => {
  const cutoff = dashboardNow.value - FLOW_WINDOW_MS
  let member = 0
  let chat = 0
  let like = 0
  let follow = 0
  let gift = 0
  let total = 0
  for (const sample of flowSamples.value) {
    if (sample.at < cutoff) continue
    total += 1
    if (sample.type === 'member') member += sample.weight
    if (sample.type === 'chat') chat += sample.weight
    if (sample.type === 'like') like += sample.weight
    if (sample.type === 'follow') follow += sample.weight
    if (sample.type === 'gift') gift += sample.weight
  }
  return { member, chat, like, follow, gift, total }
})

const roomHeatLabel = computed(() => {
  const rate = flowStats.value.total
  if (rate >= 180) return '高热流量'
  if (rate >= 60) return '活跃流量'
  if (rate >= 15) return '稳定流量'
  if (rate > 0) return '低速流量'
  return '等待事件'
})

const roomHeatLevel = computed(() => {
  const rate = flowStats.value.total
  if (rate >= 180) return 'hot'
  if (rate >= 60) return 'active'
  if (rate >= 15) return 'steady'
  return 'calm'
})

function logUserAction(eventCode: string, detail?: Record<string, unknown>) {
  recordLiveRuntimeEvent(roomId, eventCode, detail).catch(() => {})
}

async function controlBoundDevice(action: 'connect' | 'pause' | 'resume' | 'disconnect') {
  const device = boundDevice.value
  if (!device) {
    deviceControlError.value = '当前直播间还没有绑定小蓝盒子'
    return
  }
  if (deviceControlBusy.value) return
  deviceControlBusy.value = true
  deviceControlError.value = ''
  try {
    const updated = await controlLiveDevice(device.id, roomId, action)
    if (runtimeSnapshot.value) {
      runtimeSnapshot.value = { ...runtimeSnapshot.value, device: updated }
    } else {
      await refreshRuntime()
    }
  } catch (err) {
    deviceControlError.value = err instanceof Error ? err.message : '设备控制失败'
  } finally {
    deviceControlBusy.value = false
  }
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
      anchor_transcript: String(mainlineSpeech.value.text || ''),
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
  // Closed orb always returns to the bottom-center base CSS position.
  // Keep the dragged position in memory so expanded behavior stays unchanged.
  if (!agentDockExpanded.value || !agentDockPosition.value) return undefined
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
    const [snapshot, runtimeEvents, roomData] = await Promise.all([
      getLiveRuntime(roomId),
      getLiveRuntimeEvents(roomId, 30),
      getRoom(roomId),
    ])
    updatePreservingPageBottom(() => {
      runtimeSnapshot.value = snapshot
      if (room.value) {
        room.value.online_count = roomData.online_count
        room.value.status = roomData.status
        room.value.last_event_at = roomData.last_event_at
      }
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
    })
  } catch (err) {
    updatePreservingPageBottom(() => {
      runtimeError.value = err instanceof Error ? err.message : '读取AI运行状态失败'
    })
  }
}

function localAudioBaseURL() {
  const configured = String(import.meta.env.VITE_AUDIO_SERVICE_URL || '').trim().replace(/\/$/, '')
  if (configured) return configured
  const host = window.location.hostname
  if (host === '127.0.0.1' || host === 'localhost') return 'http://127.0.0.1:8082'
  return ''
}

function localAudioReceiverID() {
  const stored = window.localStorage.getItem(LOCAL_AUDIO_RECEIVER_KEY)
  if (stored) return stored
  const suffix = typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID()
    : Math.random().toString(36).slice(2) + Date.now().toString(36)
  const id = 'web-console-' + suffix
  window.localStorage.setItem(LOCAL_AUDIO_RECEIVER_KEY, id)
  return id
}

function ensureLocalAudioUnlocked() {
  const AudioContextCtor = window.AudioContext
  if (!AudioContextCtor) {
    localAudioError.value = '当前浏览器不支持本机音频播放。'
    return
  }
  if (!localAudioContext) localAudioContext = new AudioContextCtor()
  if (localAudioContext.state === 'suspended') {
    void localAudioContext.resume().catch(() => {
      localAudioError.value = '浏览器阻止了自动播放，请再次点击抢答或开始。'
    })
  }
}

async function reportLocalAudioTask(task: LocalAudioTask, status: string, progressMS = 0, message = '') {
  const base = localAudioBaseURL()
  if (!base || !task.speech_task_id) return
  try {
    await fetch(base + '/v1/tasks/' + encodeURIComponent(task.speech_task_id) + '/events', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        receiver_id: localAudioReceiverID(),
        status,
        progress_ms: Math.max(0, Math.round(progressMS)),
        error: message,
        occurred_at: new Date().toISOString(),
      }),
    })
  } catch {
    // Playback itself remains primary during local testing; feedback can retry on the next event.
  }
}

function stopLocalAudioPlayback(reportInterrupted = false) {
  localAudioPlaybackGeneration += 1
  if (localAudioProgressTimer !== undefined) {
    window.clearInterval(localAudioProgressTimer)
    localAudioProgressTimer = undefined
  }
  const previousTask = localAudioTask
  const source = localAudioSource
  localAudioSource = null
  localAudioTask = null
  if (source) {
    try { source.stop() } catch { /* already stopped */ }
  }
  if (reportInterrupted && previousTask) {
    void reportLocalAudioTask(previousTask, 'FAILED', 0, 'interrupted_by_new_task')
  }
}

async function playLocalAudioTask(task: LocalAudioTask) {
  if (!task?.speech_task_id || !task.audio_url) return
  if (localAudioTask?.speech_task_id === task.speech_task_id && localAudioSource) return
  ensureLocalAudioUnlocked()
  const context = localAudioContext
  if (!context || context.state !== 'running') {
    localAudioError.value = '本机声音未解锁，请点击“开始”或“抢答”后再试。'
    return
  }

  stopLocalAudioPlayback(Boolean(localAudioTask))
  const generation = ++localAudioPlaybackGeneration
  localAudioTask = task
  localAudioError.value = ''
  try {
    const response = await fetch(task.audio_url, { cache: 'no-store' })
    if (!response.ok) throw new Error('音频 HTTP ' + response.status)
    const raw = await response.arrayBuffer()
    const buffer = await context.decodeAudioData(raw.slice(0))
    if (generation !== localAudioPlaybackGeneration) return

    const source = context.createBufferSource()
    source.buffer = buffer
    source.connect(context.destination)
    localAudioSource = source
    localAudioState.value = 'playing'
    const startedAt = performance.now()
    await reportLocalAudioTask(task, 'READY', 0)
    await reportLocalAudioTask(task, 'PLAYING', 0)

    localAudioProgressTimer = window.setInterval(() => {
      if (generation !== localAudioPlaybackGeneration) return
      const elapsed = Math.min(task.duration_ms || buffer.duration * 1000, performance.now() - startedAt)
      void reportLocalAudioTask(task, 'PROGRESS', elapsed)
    }, 1500)

    source.onended = () => {
      if (generation !== localAudioPlaybackGeneration) return
      if (localAudioProgressTimer !== undefined) {
        window.clearInterval(localAudioProgressTimer)
        localAudioProgressTimer = undefined
      }
      localAudioSource = null
      localAudioTask = null
      localAudioState.value = 'connected'
      void reportLocalAudioTask(task, 'COMPLETED', task.duration_ms || Math.round(buffer.duration * 1000))
    }
    source.start()
  } catch (err) {
    if (generation !== localAudioPlaybackGeneration) return
    localAudioState.value = 'error'
    localAudioError.value = err instanceof Error ? err.message : '本机播放失败'
    localAudioSource = null
    localAudioTask = null
    await reportLocalAudioTask(task, 'FAILED', 0, localAudioError.value)
  }
}

async function registerLocalAudioReceiver() {
  const base = localAudioBaseURL()
  if (!base || !Number.isFinite(roomId) || roomId <= 0) return false
  try {
    const response = await fetch(base + '/v1/receivers/register', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        receiver_id: localAudioReceiverID(),
        room_id: roomId,
        terminal_type: 'web_console',
        name: '直播详情本机测试端',
        capabilities: ['audio/wav', 'interaction_tts', 'mainline'],
      }),
    })
    if (!response.ok) throw new Error('声音网关注册失败 HTTP ' + response.status)
    localAudioRegisteredRoomID = roomId
    if (localAudioHeartbeatTimer !== undefined) window.clearInterval(localAudioHeartbeatTimer)
    localAudioHeartbeatTimer = window.setInterval(() => {
      void heartbeatLocalAudioReceiver()
    }, 15_000)
    return true
  } catch (err) {
    localAudioState.value = 'error'
    localAudioError.value = err instanceof Error ? err.message : '声音终端注册失败'
    return false
  }
}

async function heartbeatLocalAudioReceiver() {
  const base = localAudioBaseURL()
  if (!base || localAudioRegisteredRoomID <= 0) return
  try {
    const response = await fetch(
      base + '/v1/receivers/' + encodeURIComponent(localAudioReceiverID()) + '/heartbeat',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: localAudioRegisteredRoomID }),
      },
    )
    if (!response.ok) {
      localAudioState.value = 'disconnected'
      if (response.status === 409) {
        await registerLocalAudioReceiver()
      }
    }
  } catch {
    localAudioState.value = 'disconnected'
  }
}

async function unregisterLocalAudioReceiver() {
  const base = localAudioBaseURL()
  const registeredRoomID = localAudioRegisteredRoomID
  localAudioRegisteredRoomID = 0
  if (localAudioHeartbeatTimer !== undefined) {
    window.clearInterval(localAudioHeartbeatTimer)
    localAudioHeartbeatTimer = undefined
  }
  if (!base || registeredRoomID <= 0) return
  try {
    await fetch(
      base + '/v1/receivers/' + encodeURIComponent(localAudioReceiverID()) + '/unregister',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ room_id: registeredRoomID }),
      },
    )
  } catch {
    // Page teardown should not be blocked by a best-effort unregister.
  }
}

async function connectLocalAudioReceiver() {
  localAudioEventSource?.close()
  localAudioEventSource = null
  const base = localAudioBaseURL()
  if (!base || !Number.isFinite(roomId) || roomId <= 0) return
  if (!(await registerLocalAudioReceiver())) return

  const source = new EventSource(
    base + '/v1/rooms/' + roomId + '/stream?receiver_id=' + encodeURIComponent(localAudioReceiverID()),
  )
  localAudioEventSource = source
  source.addEventListener('connected', () => {
    localAudioState.value = 'connected'
    localAudioError.value = ''
  })
  source.addEventListener('task', (rawEvent) => {
    try {
      const task = JSON.parse((rawEvent as MessageEvent).data) as LocalAudioTask
      void playLocalAudioTask(task)
    } catch {
      localAudioState.value = 'error'
      localAudioError.value = '收到无法识别的本机播音任务。'
    }
  })
  source.addEventListener('unregistered', () => {
    localAudioState.value = 'disconnected'
    void connectLocalAudioReceiver()
  })
  source.onerror = () => {
    localAudioState.value = 'disconnected'
  }
}

async function startCompanionRuntime() {
  if (runtimeControlBusy.value) return
  ensureLocalAudioUnlocked()
  if (aiRuntimeMode.value === 'anchor' && !selectedLiveAgentPlanId.value) {
    runtimeError.value = '主播模式需要先选择智能体直播方案'
    return
  }
  runtimeControlBusy.value = true
  runtimeError.value = ''
  try {
    await startLiveRuntime(roomId)
    await Promise.all([refreshRuntime(), refreshAgentDecisions()])
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '启动直播搭子失败'
  } finally {
    runtimeControlBusy.value = false
  }
}

async function stopCompanionRuntime() {
  if (runtimeControlBusy.value || (!aiActive.value && !aiSessionActive.value)) return
  runtimeControlBusy.value = true
  runtimeError.value = ''
  try {
    await stopLiveRuntime(roomId)
    await Promise.all([refreshRuntime(), refreshAgentDecisions()])
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '结束直播搭子失败'
  } finally {
    runtimeControlBusy.value = false
  }
}

async function refreshSessionStats() {
  try {
    const nextStats = await getRoomSessionStats(roomId)
    updatePreservingPageBottom(() => {
      sessionStats.value = nextStats
    })
  } catch {
    // Keep the last good session stats during a transient Core/Redis hiccup.
  }
}

async function loadLiveReview() {
  if (liveReviewLoading.value) return
  liveReviewLoading.value = true
  liveReviewError.value = ''
  try {
    liveReview.value = await getRoomReview(roomId)
  } catch (err) {
    liveReview.value = null
    liveReviewError.value = err instanceof Error ? err.message : '读取直播复盘失败'
  } finally {
    liveReviewLoading.value = false
  }
}

function toggleLiveReview() {
  liveReviewOpen.value = !liveReviewOpen.value
  if (liveReviewOpen.value) void loadLiveReview()
}

async function chooseSessionContinuation(action: 'merge' | 'fresh') {
  if (sessionDecisionBusy.value) return
  sessionDecisionBusy.value = true
  runtimeError.value = ''
  try {
    const nextStats = await resolveRoomSessionDecision(roomId, action)
    sessionStats.value = nextStats
    liveReview.value = null
    if (action === 'fresh') {
      events.value = []
      importantHistory.value = {}
      importantCursor.value = {}
      importantHasMore.value = {}
      expandedQuestionTopic.value = ''
      selectedQuestionEventId.value = null
      frozenSemanticBuckets.value = null
    }
    await Promise.all([
      refreshRuntime(),
      refreshSessionStats(),
      refreshRoomBrain(),
      refreshSpeechRuntime(),
      refreshAgentDecisions(),
    ])
    if (action === 'fresh') {
      const page = await getRoomEvents(roomId, 500)
      events.value = page.items
    }
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '处理直播续接失败'
  } finally {
    sessionDecisionBusy.value = false
  }
}

function startRuntimePolling() {
  if (runtimePollTimer !== undefined) window.clearInterval(runtimePollTimer)
  runtimePollTimer = window.setInterval(() => {
    void refreshRuntime()
    void refreshRoomBrain()
  }, 5000)
}

function startSessionStatsPolling() {
  if (sessionStatsPollTimer !== undefined) window.clearInterval(sessionStatsPollTimer)
  sessionStatsPollTimer = window.setInterval(() => {
    void refreshSessionStats()
  }, 60_000)
}

function startSpeechRuntimePolling() {
  if (speechPollTimer !== undefined) window.clearInterval(speechPollTimer)
  speechPollTimer = window.setInterval(() => {
    void refreshSpeechRuntime()
  }, 1000)
}

function startAgentDecisionPolling() {
  if (agentDecisionPollTimer !== undefined) window.clearInterval(agentDecisionPollTimer)
  agentDecisionPollTimer = window.setInterval(() => {
    void refreshAgentDecisions()
  }, 1500)
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
  events.value.filter((event) => !['room', 'order_signal', 'session_start', 'session_end'].includes(event.event_type)),
)

function mergeEventHistory(...groups: RoomEvent[][]) {
  const seen = new Map<number, RoomEvent>()
  for (const group of groups) {
    for (const event of group) seen.set(event.id, event)
  }
  return [...seen.values()].sort((a, b) => b.id - a.id)
}

function eventMatchesTab(event: RoomEvent, eventType: string) {
  if (eventType === 'chat') return event.event_type === 'chat' || event.event_type === 'comment'
  return event.event_type === eventType
}

function isDirectAnswerEvent(event: RoomEvent) {
  const eventType = String(event.event_type || '').toLowerCase()
  return (eventType === 'chat' || eventType === 'comment') && Boolean(String(event.content || '').trim())
}

function eventDecisionRowState(eventID: number) {
  return eventDecisionRows.value[eventID] || {}
}

function setEventDecisionRowState(eventID: number, patch: Partial<EventDecisionRowState>) {
  eventDecisionRows.value = {
    ...eventDecisionRows.value,
    [eventID]: {
      ...(eventDecisionRows.value[eventID] || {}),
      ...patch,
    },
  }
}

function eventDecisionDisabled(event: RoomEvent, action: EventDecisionAction) {
  const state = eventDecisionRowState(event.id)
  if (state.busy) return true
  if (state.submitted === 'quick') return true
  if (action === 'answer' && state.submitted === 'answer') return true
  return false
}

function eventDecisionLabel(event: RoomEvent, action: EventDecisionAction) {
  const state = eventDecisionRowState(event.id)
  if (state.busy === action) return action === 'quick' ? '抢答中…' : '提交中…'
  if (state.submitted === 'quick' && action === 'quick') return '已抢答'
  if (state.submitted === 'answer' && action === 'answer') return '已回答'
  return action === 'quick' ? '抢答' : '回答'
}

const filteredEvents = computed(() => {
  if (activeType.value === 'all') return visibleEvents.value
  const realtime = visibleEvents.value.filter((event) => eventMatchesTab(event, activeType.value))
  if (IMPORTANT_EVENT_TYPES.has(activeType.value)) {
    return mergeEventHistory(importantHistory.value[activeType.value] || [], realtime)
  }
  return realtime
})

async function loadImportantHistory(eventType: string, reset = false) {
  if (!IMPORTANT_EVENT_TYPES.has(eventType) || importantLoading.value[eventType]) return
  if (reset) {
    importantHistory.value[eventType] = []
    importantCursor.value[eventType] = 0
    importantHasMore.value[eventType] = true
  }
  if (importantHasMore.value[eventType] === false) return
  importantLoading.value[eventType] = true
  try {
    const page = await getRoomImportantEvents(roomId, eventType, IMPORTANT_PAGE_SIZE, importantCursor.value[eventType] || 0)
    const typedItems = page.items.filter((event) => eventMatchesTab(event, eventType))
    importantHistory.value[eventType] = mergeEventHistory(importantHistory.value[eventType] || [], typedItems)
    importantCursor.value[eventType] = page.next_before_id || (page.items.length ? page.items[page.items.length - 1].id : 0)
    importantHasMore.value[eventType] = Boolean(page.has_more && page.items.length)
  } finally {
    importantLoading.value[eventType] = false
  }
}

function selectEventType(eventType: string) {
  activeType.value = eventType
  if (IMPORTANT_EVENT_TYPES.has(eventType) && !(importantHistory.value[eventType]?.length)) {
    void loadImportantHistory(eventType, true)
  }
}

function handleEventListScroll() {
  const element = eventListEl.value
  if (!element || !IMPORTANT_EVENT_TYPES.has(activeType.value)) return
  if (element.scrollTop + element.clientHeight >= element.scrollHeight - 80) {
    void loadImportantHistory(activeType.value)
  }
}

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

function normalizeSubjectPart(value?: string) {
  return (value || '').trim().replace(/\s+/g, ' ').toLowerCase()
}

function matchesSubject(event: RoomEvent, userId?: string, nickname?: string) {
  const targetUser = normalizeSubjectPart(userId)
  const targetNickname = normalizeSubjectPart(nickname)
  const eventUser = normalizeSubjectPart(event.user_id)
  const eventNickname = normalizeSubjectPart(event.nickname)
  if (targetUser && eventUser && targetUser === eventUser) return true
  return Boolean(targetNickname && eventNickname && targetNickname === eventNickname)
}

function rebuildFlowSamples() {
  dashboardNow.value = Date.now()
  flowSamples.value = []
  for (const event of [...events.value].reverse()) pushFlowSample(event)
}

async function refreshRoomBrain() {
  try {
    const nextBrain = await getRoomBrain(roomId)
    updatePreservingPageBottom(() => {
      roomBrain.value = nextBrain
    })
  } catch {
    // Semantic buckets are an enhancement; realtime public screen remains usable.
  }
}

async function refreshSpeechRuntime() {
  try {
    const nextSpeechRuntime = await getRoomSpeechRuntime(roomId)
    updatePreservingPageBottom(() => {
      speechRuntime.value = nextSpeechRuntime
    })
  } catch {
    // Keep the last good speech state during a transient Core/management hiccup.
  }
}

async function refreshAgentDecisions() {
  try {
    const nextAgentDecisions = await getRoomAgentDecisions(roomId)
    updatePreservingPageBottom(() => {
      agentDecisionState.value = nextAgentDecisions
    })
  } catch {
    // Keep the last good Core decision snapshot during a transient service hiccup.
  }
}

async function answerPublicScreenEvent(event: RoomEvent, action: EventDecisionAction) {
  if (!isDirectAnswerEvent(event) || eventDecisionRowState(event.id).busy) return
  ensureLocalAudioUnlocked()
  if (!aiRunning.value) {
    setEventDecisionRowState(event.id, { error: '请先启动直播搭子。' })
    return
  }
  if (sessionStats.value?.resume_pending) {
    setEventDecisionRowState(event.id, { error: '请先选择续接上一场或作为新直播。' })
    return
  }
  const occurredAt = Date.parse(event.occurred_at || '')
  if (!Number.isFinite(occurredAt) || Date.now() - occurredAt > 30 * 60 * 1000) {
    setEventDecisionRowState(event.id, { error: '已超过30分钟实时回答窗口。' })
    return
  }

  setEventDecisionRowState(event.id, { busy: action, error: '', message: '' })
  try {
    const result = await enqueueRoomManualAgentDecision(roomId, {
      question: String(event.content || '').trim(),
      title: '单条弹幕',
      summary: action === 'quick'
        ? '人工从实时公屏发起抢答，立即生成并播出'
        : '人工从实时公屏加入待打断队列，由监控Agent安排回答时机',
      event_id: event.id,
      user_id: event.user_id,
      force_reopen: action === 'quick',
      manual_action: action,
    })
    const submitted = action === 'quick' ? 'quick' : (eventDecisionRowState(event.id).submitted || 'answer')
    let message = action === 'quick' ? '已提交抢答' : '已进入待打断队列'
    if (result.merged && action === 'quick') message = '已提升为抢答优先'
    else if (result.merged) message = '已融合到待回答任务'
    else if (result.suppressed) message = '同类问题刚回答过，可点抢答强制执行'
    setEventDecisionRowState(event.id, { busy: undefined, submitted, message, error: '' })
    await refreshAgentDecisions()
  } catch (err) {
    setEventDecisionRowState(event.id, {
      busy: undefined,
      error: err instanceof Error ? err.message : '提交回答失败',
    })
  }
}

function questionDecisionBusyState(eventID: number) {
  return questionDecisionBusy.value[eventID]
}

function setQuestionDecisionBusy(eventID: number, action?: EventDecisionAction) {
  questionDecisionBusy.value = {
    ...questionDecisionBusy.value,
    [eventID]: action,
  }
}

function bucketDecisionBusyState(topic: string) {
  return bucketDecisionBusy.value[topic]
}

function setBucketDecisionBusy(topic: string, action?: EventDecisionAction) {
  bucketDecisionBusy.value = {
    ...bucketDecisionBusy.value,
    [topic]: action,
  }
}

async function submitQuestionDecision(
  bucket: RoomBrainTopic,
  question: RoomBrainQuestion,
  action: EventDecisionAction,
) {
  if (questionDecisionBusyState(question.EventID)) return
  if (!aiRunning.value) {
    agentDecisionActionMessage.value = '请先启动直播搭子。'
    return
  }
  if (sessionStats.value?.resume_pending) {
    agentDecisionActionMessage.value = '请先选择续接上一场或作为新直播。'
    return
  }
  if (!questionTTSEligible(bucket, question)) {
    agentDecisionActionMessage.value = '这个问题已超过30分钟实时回答窗口，仅保留用于复盘。'
    return
  }
  ensureLocalAudioUnlocked()
  setQuestionDecisionBusy(question.EventID, action)
  agentDecisionActionMessage.value = ''
  agentDecisionLastSuppressed.value = false
  try {
    const result = await enqueueRoomManualAgentDecision(roomId, {
      question: question.Content,
      topic: bucket.Topic,
      title: semanticBucketLabel(bucket),
      summary: action === 'quick'
        ? '人工抢答，进入最高优先级硬打断流程'
        : '人工回答，等待当前播音窗口后优先插入',
      event_id: question.EventID,
      user_id: question.UserID,
      force_reopen: action === 'quick',
      manual_action: action,
    })
    if (result.suppressed) {
      agentDecisionLastSuppressed.value = true
      agentDecisionActionMessage.value = '同类问题刚回答过，已累计；如需立即说请点“抢答”。'
    } else if (action === 'quick') {
      agentDecisionActionMessage.value = result.merged
        ? '已融合并提升为抢答最高优先级。'
        : '已进入抢答最高优先级，等待执行层立即接管。'
    } else if (result.merged && result.promoted) {
      agentDecisionActionMessage.value = '已融合到同类任务并提升为人工回答优先。'
    } else if (result.merged) {
      agentDecisionActionMessage.value = '已融合到现有同类回答任务。'
    } else {
      agentDecisionActionMessage.value = '已加入待打断队列，当前播音结束后优先回答。'
    }
    await refreshAgentDecisions()
  } catch (err) {
    agentDecisionActionMessage.value = err instanceof Error ? err.message : '人工回答加入队列失败'
  } finally {
    setQuestionDecisionBusy(question.EventID)
  }
}

async function answerQuestionDetail(
  bucket: RoomBrainTopic,
  question: RoomBrainQuestion,
  action: EventDecisionAction,
) {
  selectQuestionDetail(question)
  await submitQuestionDecision(bucket, question, action)
}

async function answerSelectedQuestion(action: EventDecisionAction) {
  const question = selectedQuestionDetail.value
  const bucket = selectedQuestionBucket.value
  if (!question || !bucket) return
  await submitQuestionDecision(bucket, question, action)
}

async function answerQuestionBucket(bucket: RoomBrainTopic, action: EventDecisionAction) {
  if (bucketDecisionBusyState(bucket.Topic)) return
  if (!aiRunning.value) {
    agentDecisionActionMessage.value = '请先启动直播搭子。'
    return
  }
  if (sessionStats.value?.resume_pending) {
    agentDecisionActionMessage.value = '请先选择续接上一场或作为新直播。'
    return
  }
  const eligibleQuestions = (bucket.TTSQuestions || [])
    .filter((question) => questionTTSEligible(bucket, question))
    .slice(0, 8)
  if (!eligibleQuestions.length) {
    agentDecisionActionMessage.value = '这个问题聚类已超过30分钟实时回答窗口，仅保留用于复盘。'
    return
  }

  ensureLocalAudioUnlocked()
  setBucketDecisionBusy(bucket.Topic, action)
  agentDecisionActionMessage.value = ''
  agentDecisionLastSuppressed.value = false
  try {
    const combinedQuestion = eligibleQuestions
      .map((question) => String(question.Content || '').trim())
      .filter(Boolean)
      .join('；')
    const result = await enqueueRoomManualAgentDecision(roomId, {
      question: combinedQuestion,
      topic: bucket.Topic,
      title: semanticBucketLabel(bucket),
      summary: action === 'quick'
        ? '人工对整个问题聚类发起抢答，请综合本桶当前有效提问生成统一口播并立即播出'
        : '人工对整个问题聚类发起回答，请综合本桶当前有效提问生成统一口播并等待合适打断时机',
      reply_hint: '请综合回答本问题聚类的共同诉求，不逐条机械复述。',
      force_reopen: action === 'quick',
      manual_action: action,
      manual_origin: 'question_cluster',
    })
    if (result.suppressed) {
      agentDecisionLastSuppressed.value = true
      agentDecisionActionMessage.value = '这个聚类刚回答过，已累计；如需立即说请点“抢答”。'
    } else if (action === 'quick') {
      agentDecisionActionMessage.value = result.merged
        ? '聚类任务已融合并提升为抢答最高优先级。'
        : '整个问题聚类已进入抢答最高优先级。'
    } else if (result.merged) {
      agentDecisionActionMessage.value = '整个问题聚类已融合到待回答任务。'
    } else {
      agentDecisionActionMessage.value = '整个问题聚类已进入待打断队列。'
    }
    await refreshAgentDecisions()
  } catch (err) {
    agentDecisionActionMessage.value = err instanceof Error ? err.message : '问题聚类加入队列失败'
  } finally {
    setBucketDecisionBusy(bucket.Topic)
  }
}

async function refreshBlockedUsers() {
  try {
    const response = await getRoomBlockedUsers(roomId)
    blockedUsers.value = response.items
    moderationError.value = ''
  } catch (err) {
    moderationError.value = err instanceof Error ? err.message : '读取屏蔽池失败'
  }
}

function handleEventMouseEnter(event: RoomEvent) {
  hoveredEventId.value = event.id
}

function handleEventMouseLeave(event: RoomEvent) {
  if (hoveredEventId.value === event.id) hoveredEventId.value = null
  if (!eventContextMenu.value) flushStreamEvents()
}

function openEventContextMenu(mouseEvent: MouseEvent, event: RoomEvent) {
  if (!event.user_id && !event.nickname) return
  hoveredEventId.value = event.id
  eventContextMenu.value = {
    x: Math.max(10, Math.min(mouseEvent.clientX, window.innerWidth - 210)),
    y: Math.max(10, Math.min(mouseEvent.clientY, window.innerHeight - 110)),
    event,
  }
}

function closeEventContextMenu() {
  if (!eventContextMenu.value && hoveredEventId.value === null) return
  eventContextMenu.value = null
  hoveredEventId.value = null
  flushStreamEvents()
}

async function blockContextUser() {
  const target = eventContextMenu.value?.event
  if (!target || (!target.user_id && !target.nickname)) return
  const busyKey = normalizeSubjectPart(target.user_id) || normalizeSubjectPart(target.nickname)
  blockedBusyKey.value = busyKey
  moderationError.value = ''
  try {
    const blocked = await blockRoomUser(roomId, {
      user_id: target.user_id,
      nickname: target.nickname,
      reason: '公屏右键人工屏蔽',
    })
    blockedUsers.value = [
      blocked,
      ...blockedUsers.value.filter((item) => item.subject_key !== blocked.subject_key),
    ]
    events.value = events.value.filter(
      (event) => !matchesSubject(event, blocked.user_id, blocked.nickname),
    )
    pendingStreamEvents = pendingStreamEvents.filter(
      (event) => !matchesSubject(event, blocked.user_id, blocked.nickname),
    )
    rebuildFlowSamples()
    blockedDrawerOpen.value = true
    logUserAction('PUBLIC_USER_BLOCKED', {
      user_id: blocked.user_id || '',
      nickname: blocked.nickname || '',
      subject_key: blocked.subject_key,
    })
    closeEventContextMenu()
    void refreshRoomBrain()
  } catch (err) {
    moderationError.value = err instanceof Error ? err.message : '屏蔽用户失败'
  } finally {
    blockedBusyKey.value = ''
  }
}

async function restoreBlockedUser(item: RoomBlockedUser) {
  if (blockedBusyKey.value) return
  blockedBusyKey.value = item.subject_key
  moderationError.value = ''
  try {
    await restoreRoomBlockedUser(roomId, {
      user_id: item.user_id,
      nickname: item.nickname,
    })
    blockedUsers.value = blockedUsers.value.filter(
      (blocked) => blocked.subject_key !== item.subject_key,
    )
    logUserAction('PUBLIC_USER_RESTORED', {
      user_id: item.user_id || '',
      nickname: item.nickname || '',
      subject_key: item.subject_key,
    })
  } catch (err) {
    moderationError.value = err instanceof Error ? err.message : '恢复用户失败'
  } finally {
    blockedBusyKey.value = ''
  }
}

function flushStreamEvents() {
	streamBatchTimer = undefined
	if (streamPaused.value) return
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
	updatePreservingPageBottom(() => {
		events.value = [...additions.reverse(), ...events.value].slice(0, 500)
	})
}

function queueStreamEvent(event: RoomEvent) {
	pushFlowSample(event)
	pendingStreamEvents.push(event)
	if (streamPaused.value) return
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
      getRoomEvents(roomId, 300),
    ])
    room.value = roomData
    await loadLiveAgentPlansForRoom()
    events.value = [...eventData.items].sort((a, b) => b.id - a.id)
    dashboardNow.value = Date.now()
    flowSamples.value = []
    for (const event of [...eventData.items].reverse()) pushFlowSample(event)
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
    void connectLocalAudioReceiver()
    await refreshRuntime()
    await Promise.all([refreshRoomBrain(), refreshSpeechRuntime(), refreshAgentDecisions(), refreshBlockedUsers(), refreshSessionStats()])
    startRuntimePolling()
    startSessionStatsPolling()
    startSpeechRuntimePolling()
    startAgentDecisionPolling()
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

onMounted(() => {
  restorePublicScreenHeight()
  restoreAgentPanelLayout()
  restoreEventBucketHeight()
  window.addEventListener('pointermove', moveAgentPanelResize)
  window.addEventListener('pointerup', finishAgentPanelResize)
  window.addEventListener('pointercancel', finishAgentPanelResize)
  window.addEventListener('pointermove', moveEventBucketResize)
  window.addEventListener('pointerup', finishEventBucketResize)
  window.addEventListener('pointercancel', finishEventBucketResize)
  window.addEventListener('pointermove', movePublicScreenResize)
  window.addEventListener('pointerup', finishPublicScreenResize)
  window.addEventListener('pointercancel', finishPublicScreenResize)
  dashboardTickTimer = window.setInterval(() => {
    dashboardNow.value = Date.now()
    pruneFlowSamples()
  }, 1000)
  window.addEventListener('click', closeEventContextMenu)
  window.addEventListener('click', closeAgentDecisionContextMenu)
  mascotActionTimer = window.setTimeout(runMascotAction, 1200 + Math.random() * 1400)
  load()
})
onBeforeUnmount(() => {
	eventSource?.close()
	localAudioEventSource?.close()
	localAudioEventSource = null
	void unregisterLocalAudioReceiver()
	stopLocalAudioPlayback(false)
	if (localAudioContext) {
		void localAudioContext.close().catch(() => undefined)
		localAudioContext = null
	}
	if (runtimePollTimer !== undefined) window.clearInterval(runtimePollTimer)
	if (sessionStatsPollTimer !== undefined) window.clearInterval(sessionStatsPollTimer)
	if (speechPollTimer !== undefined) window.clearInterval(speechPollTimer)
	if (agentDecisionPollTimer !== undefined) window.clearInterval(agentDecisionPollTimer)
	if (streamBatchTimer !== undefined) window.clearTimeout(streamBatchTimer)
	if (dashboardTickTimer !== undefined) window.clearInterval(dashboardTickTimer)
	if (pageBottomRestoreFrame !== undefined) window.cancelAnimationFrame(pageBottomRestoreFrame)
	stopMascotActions()
	pendingStreamEvents = []
	window.removeEventListener('click', closeEventContextMenu)
	window.removeEventListener('click', closeAgentDecisionContextMenu)
	window.removeEventListener('pointermove', movePublicScreenResize)
	window.removeEventListener('pointerup', finishPublicScreenResize)
	window.removeEventListener('pointercancel', finishPublicScreenResize)
	finishPublicScreenResize()
	window.removeEventListener('pointermove', moveEventBucketResize)
	window.removeEventListener('pointerup', finishEventBucketResize)
	window.removeEventListener('pointercancel', finishEventBucketResize)
	finishEventBucketResize()
	window.removeEventListener('pointermove', moveAgentPanelResize)
	window.removeEventListener('pointerup', finishAgentPanelResize)
	window.removeEventListener('pointercancel', finishAgentPanelResize)
	finishAgentPanelResize()
	eventContextMenu.value = null
	agentDecisionContextMenu.value = null
	hoveredEventId.value = null
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
      <div v-if="localAudioError" class="inline-error">本机播音：{{ localAudioError }}</div>

      <div v-if="sessionStats?.resume_pending" class="session-resume-mask">
        <section class="session-resume-dialog" role="dialog" aria-modal="true" aria-label="直播重新开播处理">
          <span class="session-resume-kicker">直播已重新开播</span>
          <strong>要接着上一场继续，还是作为一场新直播？</strong>
          <p>
            续接上一场会保留原开始时间，把这次当作直播中断恢复；新开一场会清空上一场的实时工作上下文，
            但原始记录仍留给后续复盘。
          </p>
          <div class="session-resume-actions">
            <button type="button" :disabled="sessionDecisionBusy" @click="chooseSessionContinuation('merge')">
              {{ sessionDecisionBusy ? '处理中…' : '续接上一场' }}
            </button>
            <button type="button" class="fresh" :disabled="sessionDecisionBusy" @click="chooseSessionContinuation('fresh')">
              作为新直播
            </button>
          </div>
        </section>
      </div>

      <section class="detail-stat-grid room-control-grid" aria-label="直播运行控制">
        <article class="runtime-stat-card companion-control-card">
          <div
            class="companion-mascot"
            :class="['brow-' + mascotBrowState, 'motion-' + mascotMotionState]"
            aria-hidden="true"
          >
            <span class="companion-mascot-glow"></span>
            <i class="companion-mascot-brow brow-left"></i>
            <i class="companion-mascot-brow brow-right"></i>
            <span class="companion-mascot-face">
              <i class="companion-mascot-eye eye-left" :class="{ blink: mascotBlinkLeft }"></i>
              <i class="companion-mascot-eye eye-right" :class="{ blink: mascotBlinkRight }"></i>
              <i class="companion-mascot-mouth" :class="'mouth-' + mascotMouthState"></i>
            </span>
          </div>
          <span>直播搭子</span>
          <label class="companion-plan-select" aria-label="智能体直播方案">
            <select
              :value="selectedLiveAgentPlanId || ''"
              :disabled="agentPlanBusy"
              @change="changeLiveAgentPlan"
            >
              <option value="" disabled>
                {{ liveAgentPlans.length ? '未选方案 · 仅中控模式' : '无直播方案 · 仅中控模式' }}
              </option>
              <option v-for="plan in liveAgentPlans" :key="plan.id" :value="plan.id">
                {{ plan.name }}
              </option>
            </select>
          </label>
          <small v-if="agentPlanError" class="companion-plan-error">{{ agentPlanError }}</small>
          <div class="companion-mode-switch" aria-label="直播搭子工作模式">
            <button
              type="button"
              :class="{ active: aiRuntimeMode === 'control' }"
              :disabled="runtimeModeBusy"
              @click="setCompanionMode('control')"
            >中控模式</button>
            <button
              type="button"
              :class="{ active: aiRuntimeMode === 'anchor' }"
              :disabled="runtimeModeBusy || !selectedLiveAgentPlanId"
              @click="setCompanionMode('anchor')"
            >主播模式</button>
          </div>
          <div class="companion-control-buttons" aria-label="直播搭子控制">
            <button
              v-if="!aiRunning"
              type="button"
              :disabled="runtimeControlBusy || (aiRuntimeMode === 'anchor' && !selectedLiveAgentPlanId)"
              @click="startCompanionRuntime"
            >{{ runtimeControlBusy ? (aiPaused ? '恢复中…' : '启动中…') : (aiPaused ? '继续' : '开始') }}</button>
            <button
              v-if="aiActive || aiSessionActive"
              type="button"
              class="end"
              :disabled="runtimeControlBusy"
              @click="stopCompanionRuntime"
            >结束</button>
          </div>
        </article>
        <article class="live-room-dashboard" :class="'heat-' + roomHeatLevel">
          <div class="live-room-dashboard-head">
            <div>
              <span class="dashboard-kicker">LIVE ROOM PULSE</span>
              <strong>直播间实时大屏</strong>
              <div class="dashboard-session-meta">
                <span>采集时间 <b>{{ sessionStats?.started_at ? formatTime(sessionStats.started_at) : '--:--:--' }}</b></span>
                <span>直播时长 <b>{{ sessionDurationText }}</b></span>
              </div>
            </div>
            <div class="dashboard-live-state" :class="streamState">
              <i></i>
              {{ room.status === 'live' ? '直播中' : statusText(room.status) }}
            </div>
          </div>

          <div class="dashboard-main-metric">
            <span>实时在线</span>
            <div class="dashboard-online-value">
              <strong>{{ (room.online_count || 0).toLocaleString() }}</strong>
              <small>人</small>
            </div>
          </div>

          <div class="dashboard-flow-grid">
            <div class="dashboard-session-metric dashboard-order-signal">
              <span>下单信号</span>
              <strong>{{ (sessionStats?.order_signals || 0).toLocaleString() }}</strong>
              <small>用户自报 / 本场</small>
            </div>
            <div>
              <span>进房流速</span>
              <strong>{{ flowStats.member }}</strong>
              <small>人 / 分钟</small>
            </div>
            <div>
              <span>弹幕流速</span>
              <strong>{{ flowStats.chat }}</strong>
              <small>条 / 分钟</small>
            </div>
            <div>
              <span>点赞流速</span>
              <strong>{{ flowStats.like.toLocaleString() }}</strong>
              <small>赞 / 分钟</small>
            </div>
          </div>

          <div class="dashboard-pulse-bar">
            <div class="dashboard-pulse-copy">
              <span>当前流量状态</span>
              <strong>{{ roomHeatLabel }}</strong>
            </div>
            <div class="dashboard-pulse-track" aria-hidden="true">
              <i v-for="index in 12" :key="index" :style="{ height: Math.max(18, Math.min(100, 22 + ((flowStats.total + index * 11) % 78))) + '%' }"></i>
            </div>
            <div class="dashboard-connection-copy">
              <span>{{ streamState === 'online' ? '公屏链路正常' : '公屏链路重连' }}</span>
              <small>最近 60 秒滚动统计</small>
            </div>
          </div>
        </article>
        <article class="device-runtime-card xiaozhi-device-card">
          <span>小蓝盒子</span>
          <div class="device-runtime-status" :class="'state-' + deviceVisualState">
            <i></i>
            <strong>{{ deviceStatusLabel }}</strong>
          </div>
          <small>{{ deviceStatusText }}</small>
          <div class="device-sim-controls" :class="{ 'is-offline': deviceVisualState === 'offline' }">
            <button
              v-if="deviceVisualState === 'offline'"
              type="button"
              class="connect"
              :disabled="deviceControlBusy"
              @click="controlBoundDevice('connect')"
            >{{ deviceControlBusy ? '连接中…' : '连接' }}</button>
            <template v-else>
              <button
                type="button"
                class="pause"
                :disabled="deviceControlBusy"
                @click="controlBoundDevice(deviceVisualState === 'paused' ? 'resume' : 'pause')"
              >{{ deviceVisualState === 'paused' ? '继续' : '暂停' }}</button>
              <button
                type="button"
                class="close"
                :disabled="deviceControlBusy"
                @click="controlBoundDevice('disconnect')"
              >断开</button>
            </template>
          </div>
          <small v-if="deviceControlError" class="device-control-error">{{ deviceControlError }}</small>
        </article>
      </section>

      <section class="live-review-panel" :class="{ open: liveReviewOpen }">
        <header class="live-review-head">
          <div>
            <span class="section-kicker">LIVE REVIEW</span>
            <h3>直播复盘</h3>
            <p>从本直播间的归档数据重新梳理，不读取实时问题桶。</p>
          </div>
          <button type="button" :disabled="liveReviewLoading" @click="toggleLiveReview">
            {{ liveReviewOpen ? '收起复盘' : '查看复盘' }}
          </button>
        </header>

        <div v-if="liveReviewOpen" class="live-review-body">
          <div v-if="liveReviewLoading" class="live-review-empty">正在从归档重建本场复盘…</div>
          <div v-else-if="liveReviewError" class="live-review-error">{{ liveReviewError }}</div>
          <template v-else-if="liveReview">
            <div class="live-review-metrics">
              <article><span>有效直播时长</span><strong>{{ formatClockSeconds(liveReview.summary.active_seconds) }}</strong></article>
              <article><span>进房</span><strong>{{ liveReview.summary.entries.toLocaleString() }}</strong></article>
              <article><span>弹幕</span><strong>{{ liveReview.summary.chats.toLocaleString() }}</strong></article>
              <article><span>点赞</span><strong>{{ liveReview.summary.likes.toLocaleString() }}</strong></article>
              <article><span>关注</span><strong>{{ liveReview.summary.follows.toLocaleString() }}</strong></article>
              <article><span>下单信号</span><strong>{{ liveReview.summary.order_signals.toLocaleString() }}</strong></article>
            </div>
            <div class="live-review-questions">
              <header>
                <strong>归档问题重建</strong>
                <span>{{ liveReview.summary.question_groups.length }} 组</span>
              </header>
              <div v-if="!liveReview.summary.question_groups.length" class="live-review-empty">本场归档中暂未识别到问题。</div>
              <article v-for="item in liveReview.summary.question_groups.slice(0, 20)" :key="item.text + item.last_at">
                <div>
                  <strong>{{ item.text }}</strong>
                  <small>
                    最近 {{ formatTime(item.last_at) }}
                    <template v-if="item.nicknames?.length"> · {{ item.nicknames.join('、') }}</template>
                  </small>
                </div>
                <b>+{{ item.count }}</b>
              </article>
            </div>
          </template>
        </div>
      </section>

      <section class="anchor-transcript-strip speech-runtime-panel">
        <header class="speech-runtime-head">
          <div class="speech-runtime-brand">
            <span class="anchor-live-dot"></span>
            <strong>主播实时口播</strong>
            <small>双轨文字</small>
          </div>
          <p>{{ speechRuntimeSummary }}</p>
        </header>

        <div
          class="speech-runtime-track-grid"
          :class="'layout-' + speechTrackLayoutState"
        >
          <article class="speech-track-card speech-mainline-card" :class="'status-' + displayMainlineStatus">
            <header>
              <span>主播内容</span>
              <b>{{ speechStatusLabel(displayMainlineStatus, 'mainline') }}</b>
            </header>
            <p :class="{ 'speech-empty-copy': !mainlineSpeech.text }">{{ mainlineSpeech.text || '等待主播文案' }}</p>
            <time v-if="mainlineSpeech.updated_at">更新 {{ formatTime(mainlineSpeech.updated_at) }}</time>
          </article>

          <article class="speech-track-card speech-interrupt-card" :class="'status-' + (interruptSpeech.status || 'idle')">
            <header>
              <span>临时打断</span>
              <b>{{ speechStatusLabel(interruptSpeech.status, 'interrupt') }}</b>
            </header>
            <small v-if="interruptSpeech.question_text" class="speech-trigger-question"><span>触发问题：</span>{{ interruptSpeech.question_text }}</small>
            <small v-else>场控答疑 / 临时插播</small>
            <p>{{ interruptSpeech.reply_text || interruptSpeech.text || '等待临时插播…' }}</p>
            <time v-if="interruptSpeech.updated_at">更新 {{ formatTime(interruptSpeech.updated_at) }}</time>
          </article>
        </div>
      </section>

      <section class="detail-layout detail-layout-no-preview">
        <div
          ref="publicScreenPanelEl"
          class="public-screen-panel"
          :class="{ 'is-resizing': publicScreenResizing }"
          :style="publicScreenPanelStyle"
        >
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
              @click="selectEventType(type.key)"
            >
              {{ type.label }}
            </button>
          </div>

          <div ref="eventListEl" class="event-list" @scroll.passive="handleEventListScroll">
          <div
            class="public-screen-resize-handle is-middle"
            role="separator"
            aria-orientation="horizontal"
            aria-label="拖动调整实时公屏高度"
            @pointerdown="startPublicScreenResize"
          >
            <span class="resize-grip-lines" aria-hidden="true"></span>
          </div>

            <div v-if="!filteredEvents.length" class="screen-empty">
              <div class="screen-empty-icon">⌁</div>
              <strong>等待直播间事件</strong>
              <span>真实采集器接入后，弹幕、进房、点赞等会实时出现在这里。</span>
            </div>

            <article
              v-for="event in filteredEvents"
              :key="event.id"
              class="event-row"
              :class="{
                'is-hovered': hoveredEventId === event.id,
                'has-ai-actions': aiRunning && isDirectAnswerEvent(event),
              }"
              @mouseenter="handleEventMouseEnter(event)"
              @mouseleave="handleEventMouseLeave(event)"
              @contextmenu.prevent.stop="openEventContextMenu($event, event)"
            >
              <time>{{ formatTime(event.occurred_at) }}</time>
              <span class="event-type" :class="eventClass(event.event_type)">
                {{ eventLabel(event.event_type) }}
              </span>
              <div class="event-body event-inline">
                <span class="event-user">【{{ event.nickname || '直播间用户' }}】：</span>
                <span class="event-action">{{ event.content || eventLabel(event.event_type) }}</span>
              </div>
              <div v-if="aiRunning && isDirectAnswerEvent(event)" class="event-ai-actions" @click.stop @contextmenu.stop>
                <button
                  type="button"
                  class="quick"
                  :disabled="eventDecisionDisabled(event, 'quick')"
                  @click="answerPublicScreenEvent(event, 'quick')"
                >{{ eventDecisionLabel(event, 'quick') }}</button>
                <button
                  type="button"
                  class="answer"
                  :disabled="eventDecisionDisabled(event, 'answer')"
                  @click="answerPublicScreenEvent(event, 'answer')"
                >{{ eventDecisionLabel(event, 'answer') }}</button>
                <small v-if="eventDecisionRowState(event.id).message" class="ok">{{ eventDecisionRowState(event.id).message }}</small>
                <small v-else-if="eventDecisionRowState(event.id).error" class="error">{{ eventDecisionRowState(event.id).error }}</small>
              </div>
            </article>
            <div v-if="importantLoading[activeType]" class="important-history-loading">正在加载更早记录…</div>
          </div>
          <div
            class="public-screen-resize-handle"
            role="separator"
            aria-orientation="horizontal"
            aria-label="拖动调整实时公屏高度"
            @pointerdown="startPublicScreenResize"
          >
            <span class="resize-grip-lines" aria-hidden="true"></span>
          </div>
        </div>

        <div class="live-control-stack">
          <section
            ref="agentDecisionPanelEl"
            class="agent-decision-panel"
            :class="{ 'is-resizing': agentPanelResizing }"
            :style="agentDecisionPanelStyle"
          >
            <div class="agent-decision-head">
              <div>
                <h3>小蓝 Agent 思考</h3>
              </div>
              <span
                class="agent-decision-state"
                :class="{ ready: agentDecisionState?.summary?.state === 'READY_TO_INTERRUPT' }"
              >
                {{ agentDecisionStateLabel(agentDecisionState?.summary?.state) }}
              </span>
            </div>

            <div class="agent-thinking-zone">
              <div class="agent-scan-watermark" :class="{ scanning: aiRunning }" aria-hidden="true">
                <strong>{{ aiRunning ? '扫描直播间' : '智能体还没工作' }}</strong>
              </div>
              <div v-if="aiRunning && agentDecisionNotes.length" class="agent-decision-note-list">
                <article v-for="note in agentDecisionNotes" :key="note.kind + note.created_at + note.message">
                  <header>
                    <b>{{ agentDecisionNoteLabel(note.kind) }}</b>
                    <time>{{ formatTime(note.created_at) }}</time>
                  </header>
                  <p>{{ note.message }}</p>
                </article>
              </div>
            </div>

            <div
              class="agent-panel-resize-handle is-split"
              role="separator"
              aria-orientation="horizontal"
              aria-label="拖动调整上下窗口高度"
              @pointerdown="startAgentSplitResize"
            >
              <span class="agent-panel-resize-lines" aria-hidden="true"></span>
            </div>

            <div class="agent-interrupt-zone">
              <div class="agent-zone-title">
                <strong>待打断队列</strong>
              </div>
              <div v-if="!agentDecisionQueue.length" class="agent-decision-empty">
                {{ aiRunning ? '暂无待打断内容' : '智能体还没工作' }}
              </div>
              <div v-else class="agent-interrupt-list">
                <article
                  v-for="(item, index) in agentDecisionQueue"
                  :key="item.id"
                  class="agent-interrupt-item"
                  :class="{
                    manual: item.sources?.includes('manual'),
                    high: item.priority >= 80,
                    claimed: item.status === 'CLAIMED',
                  }"
                  @contextmenu.prevent.stop="openAgentDecisionContextMenu($event, item)"
                >
                  <header>
                    <span class="agent-source-badge">{{ agentDecisionSourceLabel(item) }}</span>
                    <b>#{{ index + 1 }} · {{ agentDecisionPriorityLabel(item) }}</b>
                  </header>
                  <strong>{{ item.title }}</strong>
                  <p>{{ item.summary || item.sample_questions?.[0] || '等待生成打断内容' }}</p>
                  <footer>
                    <span v-if="item.merged_count > 1">已融合 {{ item.merged_count }} 条</span>
                    <span v-else>单条候选</span>
                    <time>{{ agentDecisionExpiryText(item.expires_at) }}</time>
                  </footer>
                </article>
              </div>
              <div v-if="agentDecisionState?.recently_answered?.length" class="agent-cooldown-strip">
                <span>刚回答</span>
                <b v-for="item in agentDecisionState.recently_answered.slice(0, 3)" :key="item.topic">
                  {{ item.title }}<em v-if="item.accumulated"> +{{ item.accumulated }}</em>
                </b>
              </div>
            </div>
            <div
              class="agent-panel-resize-handle is-bottom"
              role="separator"
              aria-orientation="horizontal"
              aria-label="拖动调整小蓝Agent模块高度"
              @pointerdown="startAgentPanelResize"
            >
              <span class="agent-panel-resize-lines" aria-hidden="true"></span>
            </div>
          </section>
          <section
            ref="eventBucketPanelEl"
            class="semantic-bucket-panel event-bucket-panel"
            :class="{
              'is-resizing': eventBucketResizing,
              'is-sized': eventBucketHeight !== null && !questionClusterCollapsed,
            }"
            :style="questionClusterCollapsed ? undefined : eventBucketPanelStyle"
          >
            <div class="panel-header semantic-bucket-head">
              <div>
                <span class="section-kicker">EVENT BUCKET</span>
                <h3>事件桶</h3>
              </div>
              <span class="semantic-bucket-count">本次采集</span>
            </div>

            <div class="event-bucket-section">
              <span class="event-bucket-section-title">基础事件</span>
              <div class="base-event-bucket-list">
                <article class="base-event-bucket base-member">
                  <span>进房</span><strong>+{{ sessionStats?.entries ?? roomBrain?.Intelligence?.SessionEntries ?? flowStats.member }}</strong>
                </article>
                <article class="base-event-bucket base-chat">
                  <span>弹幕</span><strong>+{{ sessionStats?.chats ?? roomBrain?.Intelligence?.SessionChats ?? flowStats.chat }}</strong>
                </article>
                <article class="base-event-bucket base-like">
                  <span>点赞</span><strong>+{{ (sessionStats?.likes ?? roomBrain?.Intelligence?.SessionLikes ?? flowStats.like).toLocaleString() }}</strong>
                </article>
                <article class="base-event-bucket base-follow">
                  <span>关注</span><strong>+{{ sessionStats?.follows ?? roomBrain?.Intelligence?.SessionFollows ?? flowStats.follow }}</strong>
                </article>
                <article class="base-event-bucket base-gift">
                  <span>礼物</span><strong>+{{ sessionStats?.gifts ?? roomBrain?.Intelligence?.SessionGifts ?? flowStats.gift }}</strong>
                </article>
                <article class="base-event-bucket base-order-signal">
                  <span>下单信号</span><strong>+{{ sessionStats?.order_signals || 0 }}</strong>
                </article>
              </div>
            </div>

            <div
              class="event-bucket-section event-bucket-smart-section"
              :class="{
                'is-collapsed': questionClusterCollapsed,
                'is-frozen': questionClusterFrozen,
              }"
              @mouseenter="freezeQuestionCluster"
              @mouseleave="unfreezeQuestionCluster"
            >
              <button type="button" class="question-cluster-heading" @click="toggleQuestionClusterCollapsed">
                <div>
                  <span class="event-bucket-section-title">问题聚类</span>
                  <small>粗意图聚合 · 点击问题桶可查看本场具体提问</small>
                </div>
                <span class="question-cluster-heading-actions">
                  <em v-if="questionClusterFrozen" class="question-cluster-freeze-badge">暂停刷新</em>
                  <b>{{ semanticBuckets.length }} 桶</b>
                  <i
                    class="question-cluster-toggle-icon"
                    :class="{ collapsed: questionClusterCollapsed }"
                    :title="questionClusterCollapsed ? '展开问题聚类' : '收起问题聚类'"
                    aria-hidden="true"
                  >
                    <svg viewBox="0 0 20 20" focusable="false" aria-hidden="true">
                      <path d="M5.5 12.5 10 8l4.5 4.5" />
                    </svg>
                  </i>
                </span>
              </button>
              <div v-if="!questionClusterCollapsed && !semanticBuckets.length" class="semantic-bucket-empty">
                本场出现问题后会立即形成问题桶；后续相似提问自动合并并累计数量。
              </div>
              <div
                v-else-if="!questionClusterCollapsed"
                class="semantic-bucket-list"
                :class="{ 'is-scrollable': semanticBuckets.length > 15 }"
              >
                <article
                  v-for="bucket in semanticBuckets"
                  :key="bucket.Topic"
                  class="semantic-bucket-card"
                  :class="[
                    'tone-' + semanticBucketTone(bucket),
                    { expanded: expandedQuestionTopic === bucket.Topic },
                  ]"
                >
                  <div class="semantic-bucket-summary-row">
                    <button
                      type="button"
                      class="semantic-bucket-summary"
                      @click="toggleQuestionBucket(bucket)"
                    >
                      <span class="semantic-bucket-copy">
                        <strong>{{ semanticBucketLabel(bucket) }}</strong>
                        <small>{{ bucket.UniqueUsers || bucket.Count }} 人提问 · 最近 {{ formatTime(bucket.LastSeenAt) }}</small>
                      </span>
                      <span class="semantic-bucket-number">+{{ bucket.Count }}</span>
                      <i class="semantic-bucket-chevron">⌄</i>
                    </button>
                    <div v-if="aiActive" class="semantic-bucket-actions">
                      <button
                        type="button"
                        class="force"
                        :disabled="Boolean(bucketDecisionBusyState(bucket.Topic)) || !aiRunning || !questionBucketTTSEligible(bucket)"
                        @click.stop="answerQuestionBucket(bucket, 'quick')"
                      >{{ bucketDecisionBusyState(bucket.Topic) === 'quick' ? '抢答中…' : '抢答' }}</button>
                      <button
                        type="button"
                        :disabled="Boolean(bucketDecisionBusyState(bucket.Topic)) || !aiRunning || !questionBucketTTSEligible(bucket)"
                        @click.stop="answerQuestionBucket(bucket, 'answer')"
                      >{{ bucketDecisionBusyState(bucket.Topic) === 'answer' ? '提交中…' : '回答' }}</button>
                    </div>
                  </div>

                  <div v-if="expandedQuestionTopic === bucket.Topic" class="question-detail-list">
                    <div
                      v-for="question in bucket.Questions || []"
                      :key="question.EventID || question.Content + question.OccurredAt"
                      class="question-detail-item"
                    >
                      <button
                        type="button"
                        class="question-detail-row"
                        :class="{ selected: selectedQuestionEventId === question.EventID }"
                        @click.stop="selectQuestionDetail(question)"
                      >
                        <span>
                          <b>{{ question.Nickname || question.UserID || '匿名用户' }}</b>
                          <time>{{ formatTime(question.OccurredAt) }}</time>
                        </span>
                        <p>{{ question.Content }}</p>
                      </button>
                      <div v-if="aiActive" class="question-detail-actions">
                        <button
                          type="button"
                          class="force"
                          :disabled="Boolean(questionDecisionBusyState(question.EventID)) || !aiRunning || !questionTTSEligible(bucket, question)"
                          @click.stop="answerQuestionDetail(bucket, question, 'quick')"
                        >{{ questionDecisionBusyState(question.EventID) === 'quick' ? '抢答中…' : '抢答' }}</button>
                        <button
                          type="button"
                          :disabled="Boolean(questionDecisionBusyState(question.EventID)) || !aiRunning || !questionTTSEligible(bucket, question)"
                          @click.stop="answerQuestionDetail(bucket, question, 'answer')"
                        >{{ questionDecisionBusyState(question.EventID) === 'answer' ? '提交中…' : '回答' }}</button>
                      </div>
                    </div>
                    <small v-if="bucket.Count > (bucket.Questions?.length || 0)" class="question-detail-more">
                      当前展开最近 {{ bucket.Questions?.length || 0 }} 条，本桶共 {{ bucket.Count }} 条。
                    </small>
                  </div>
                </article>
              </div>

              <div v-if="selectedQuestionDetail" class="single-question-focus">
                <span>单条问题</span>
                <strong>{{ selectedQuestionDetail.Content }}</strong>
                <small>
                  {{ selectedQuestionDetail.Nickname || selectedQuestionDetail.UserID || '匿名用户' }}
                  · {{ formatTime(selectedQuestionDetail.OccurredAt) }}
                </small>
                <small v-if="!selectedQuestionTTSEligible" class="single-question-expired">
                  已超过30分钟实时回答窗口 · 仅用于复盘
                </small>
                <div class="single-question-actions">
                  <button type="button" :disabled="Boolean(questionDecisionBusyState(selectedQuestionDetail.EventID)) || !aiRunning || !selectedQuestionTTSEligible" @click="answerSelectedQuestion('answer')">
                    {{ questionDecisionBusyState(selectedQuestionDetail.EventID) === 'answer' ? '提交中…' : '回答' }}
                  </button>
                  <button
                    type="button"
                    class="force"
                    :disabled="Boolean(questionDecisionBusyState(selectedQuestionDetail.EventID)) || !aiRunning || !selectedQuestionTTSEligible"
                    @click="answerSelectedQuestion('quick')"
                  >{{ questionDecisionBusyState(selectedQuestionDetail.EventID) === 'quick' ? '抢答中…' : '抢答' }}</button>
                </div>
                <small v-if="agentDecisionActionMessage" class="single-question-action-message">
                  {{ agentDecisionActionMessage }}
                </small>
              </div>
            </div>
            <div
              class="agent-panel-resize-handle is-bottom event-bucket-resize-handle"
              role="separator"
              aria-orientation="horizontal"
              aria-label="拖动调整事件桶高度"
              @pointerdown="startEventBucketResize"
            >
              <span class="agent-panel-resize-lines" aria-hidden="true"></span>
            </div>
          </section>

        </div>

      </section>

      <button
        type="button"
        class="blocked-drawer-handle"
        :class="{ open: blockedDrawerOpen }"
        @click.stop="blockedDrawerOpen = !blockedDrawerOpen"
      >
        <span>屏蔽池</span>
        <b>{{ blockedUsers.length }}</b>
      </button>

      <aside class="blocked-users-drawer" :class="{ open: blockedDrawerOpen }" @click.stop>
        <header>
          <div>
            <span class="section-kicker">BLOCK POOL</span>
            <h3>屏蔽池</h3>
            <p>系统已过滤这些用户，仍保留命中记录。</p>
          </div>
          <button type="button" aria-label="关闭屏蔽池" @click="blockedDrawerOpen = false">×</button>
        </header>
        <div v-if="moderationError" class="blocked-drawer-error">{{ moderationError }}</div>
        <div v-if="!blockedUsers.length" class="blocked-drawer-empty">暂无屏蔽用户</div>
        <div v-else class="blocked-user-list">
          <button
            v-for="item in blockedUsers"
            :key="item.subject_key"
            type="button"
            class="blocked-user-row"
            :disabled="blockedBusyKey === item.subject_key"
            @click="restoreBlockedUser(item)"
          >
            <span>
              <strong>{{ item.nickname || item.user_id || '未知用户' }}</strong>
              <small>{{ formatTime(item.blocked_at) }} · {{ item.reason || '人工屏蔽' }}</small>
            </span>
            <em>{{ blockedBusyKey === item.subject_key ? '恢复中…' : '点击恢复' }}</em>
          </button>
        </div>
      </aside>

      <div
        v-if="eventContextMenu"
        class="event-context-menu"
        :style="{ left: eventContextMenu.x + 'px', top: eventContextMenu.y + 'px' }"
        @click.stop
      >
        <span>{{ eventContextMenu.event.nickname || '直播间用户' }}</span>
        <button type="button" :disabled="Boolean(blockedBusyKey)" @click="blockContextUser">
          屏蔽该用户
        </button>
      </div>

      <div
        v-if="agentDecisionContextMenu"
        class="event-context-menu agent-queue-context-menu"
        :style="{ left: agentDecisionContextMenu.x + 'px', top: agentDecisionContextMenu.y + 'px' }"
        @click.stop
      >
        <span>{{ agentDecisionContextMenu.item.title || '待打断任务' }}</span>
        <button
          type="button"
          :disabled="agentDecisionRemoveBusy === agentDecisionContextMenu.item.id"
          @click="removeAgentDecisionFromQueue"
        >
          {{ agentDecisionRemoveBusy === agentDecisionContextMenu.item.id ? '正在移除…' : '移除' }}
        </button>
      </div>

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
.room-detail-page { overflow-anchor:none; }
.room-detail-page .room-control-grid {
  grid-template-columns: minmax(250px, 0.72fr) minmax(520px, 1.7fr) minmax(250px, 0.72fr);
  align-items: end;
  gap: 14px;
}

.room-detail-page .room-control-grid > article {
  padding: 16px 18px;
}

.live-review-panel {
  margin: 14px 0 16px;
  border: 1px solid rgba(91, 111, 163, .16);
  border-radius: 18px;
  background: rgba(255, 255, 255, .9);
  box-shadow: 0 10px 26px rgba(38, 52, 96, .06);
  overflow: hidden;
}
.live-review-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  padding: 15px 18px;
}
.live-review-head h3 { margin: 2px 0 0; color:#2f3c5a; font-size:18px; }
.live-review-head p { margin:3px 0 0; color:#8a95aa; font-size:12px; }
.live-review-head button {
  flex:0 0 auto;
  min-height:38px;
  padding:0 15px;
  border:1px solid rgba(82,101,225,.18);
  border-radius:11px;
  background:#f5f7ff;
  color:#5261cc;
  font-weight:850;
  cursor:pointer;
}
.live-review-body { padding:0 18px 18px; border-top:1px solid rgba(91,111,163,.09); }
.live-review-metrics { display:grid; grid-template-columns:repeat(6,minmax(0,1fr)); gap:10px; padding:16px 0; }
.live-review-metrics article { display:grid; gap:5px; min-width:0; padding:12px 13px; border-radius:13px; background:#f7f9fd; }
.live-review-metrics span { color:#8b95aa; font-size:11px; }
.live-review-metrics strong { color:#35435f; font-size:18px; }
.live-review-questions { display:grid; gap:8px; }
.live-review-questions > header { display:flex; align-items:center; justify-content:space-between; padding:4px 2px 7px; color:#53617e; }
.live-review-questions > header span { color:#8e98ab; font-size:12px; }
.live-review-questions > article { display:flex; align-items:center; justify-content:space-between; gap:15px; padding:11px 13px; border:1px solid rgba(91,111,163,.10); border-radius:12px; background:#fff; }
.live-review-questions > article > div { display:grid; gap:4px; min-width:0; }
.live-review-questions > article strong { color:#42506c; font-size:13px; line-height:1.5; }
.live-review-questions > article small { color:#929bad; font-size:10px; }
.live-review-questions > article b { flex:0 0 auto; color:#5969d7; font-size:16px; }
.live-review-empty,.live-review-error { padding:20px 0; text-align:center; color:#8d97aa; }
.live-review-error { color:#c84a52; }
@media (max-width: 1100px) { .live-review-metrics { grid-template-columns:repeat(3,minmax(0,1fr)); } }

.room-detail-page .anchor-transcript-strip.speech-runtime-panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: stretch;
  gap: 12px;
  min-height: 190px;
  margin-bottom: 16px;
  padding: 16px 18px 18px;
  border: 1px solid rgba(91, 111, 163, .18);
  border-radius: 18px;
  background: linear-gradient(135deg, #0f1828 0%, #121e32 68%, #17243a 100%);
  box-shadow: 0 12px 30px rgba(24, 35, 62, .13);
  color: #fff;
}

.speech-runtime-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  min-width: 0;
}

.speech-runtime-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 0 0 auto;
  white-space: nowrap;
}

.speech-runtime-brand strong { font-size: 16px; }
.speech-runtime-brand small { color: #8fa3bf; font-size: 12px; }
.speech-runtime-head > p {
  min-width: 0;
  margin: 0;
  color: #cbd5e5;
  font-size: 12px;
  font-weight: 700;
  text-align: right;
  overflow-wrap: anywhere;
}

.speech-runtime-track-grid {
  display: grid !important;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
  width: 100%;
  transition: grid-template-columns .28s cubic-bezier(.22, .8, .22, 1);
}

.speech-runtime-track-grid.layout-mainline {
  grid-template-columns: minmax(0, 1.2fr) minmax(0, 1fr);
}

.speech-runtime-track-grid.layout-interrupt {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
}

.speech-runtime-track-grid.layout-balanced {
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
}

.speech-track-card {
  position: relative;
  isolation: isolate;
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr) auto;
  align-content: stretch;
  gap: 7px;
  min-width: 0;
  height: 184px;
  min-height: 184px;
  max-height: 184px;
  padding: 13px 15px 14px;
  border: 1px solid rgba(139, 157, 208, .18);
  border-radius: 15px;
  overflow: visible;
}

.speech-track-card > * {
  position: relative;
  z-index: 1;
}

.speech-track-card.status-playing::before {
  content: "";
  position: absolute;
  z-index: -1;
  inset: 0;
  border: 1px solid rgba(73, 201, 155, .34);
  border-radius: inherit;
  background: inherit;
  pointer-events: none;
  transform-origin: 50% 50%;
  animation: speech-card-breathe 1.55s ease-in-out infinite;
}

@keyframes speech-card-breathe {
  0%, 100% {
    transform: scale(1);
    opacity: .78;
    box-shadow: 0 8px 20px rgba(54, 80, 150, .10);
  }
  50% {
    transform: scale(1.018, 1.045);
    opacity: 1;
    box-shadow: 0 14px 34px rgba(54, 80, 150, .20);
  }
}

.speech-runtime-track-grid.layout-mainline .speech-mainline-card,
.speech-runtime-track-grid.layout-interrupt .speech-interrupt-card {
  box-shadow: 0 12px 28px rgba(34, 52, 106, .16);
}

.speech-track-card > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.speech-track-card > header span {
  font-size: 13px;
  font-weight: 900;
  letter-spacing: .03em;
}

.speech-track-card > header b {
  flex: 0 0 auto;
  padding: 4px 8px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 900;
}

.speech-track-card > small {
  min-width: 0;
  color: inherit;
  opacity: .62;
  font-size: 10px;
  line-height: 1.45;
  overflow-wrap: anywhere;
}

.speech-track-card > small.speech-trigger-question {
  color: #d83f4f;
  opacity: .92;
  max-height: 2.9em;
  overflow: hidden;
}

.speech-trigger-question > span {
  color: inherit;
  font-weight: 900;
}

.room-detail-page .speech-track-card > p {
  min-height: 0;
  margin: 0;
  padding-right: 5px;
  color: inherit;
  font-size: 14px;
  font-weight: 800;
  line-height: 1.65;
  white-space: normal;
  overflow-wrap: anywhere;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: thin;
}

.speech-mainline-card {
  grid-template-rows: auto minmax(0, 1fr) auto;
}

.room-detail-page .speech-mainline-card > p.speech-empty-copy {
  display: grid;
  place-items: center;
  width: 100%;
  height: 100%;
  padding: 0;
  color: rgba(221, 228, 244, .38);
  font-size: 20px;
  font-weight: 800;
  line-height: 1.4;
  letter-spacing: .04em;
  text-align: center;
  overflow: hidden;
}

.speech-track-card > time {
  align-self: end;
  color: inherit;
  opacity: .48;
  font-size: 9px;
}

.speech-mainline-card {
  color: #f8fbff;
  border-color: rgba(96, 135, 255, .36);
  background: linear-gradient(145deg, rgba(31, 48, 79, .96), rgba(20, 32, 54, .98));
  box-shadow: inset 0 1px 0 rgba(255,255,255,.04);
}
.speech-mainline-card > header b { color: #aebcff; background: rgba(89, 107, 210, .20); }

.speech-interrupt-card {
  color: #303a52;
  border-color: rgba(208, 217, 234, .92);
  background: linear-gradient(145deg, #ffffff 0%, #f5f7fb 100%);
  box-shadow: 0 7px 18px rgba(8, 17, 37, .08);
}
.speech-interrupt-card > header b { color: #76829b; background: #e9edf4; }

.speech-track-card.status-playing { border-color: rgba(73, 201, 155, .48); }
.speech-track-card.status-playing > header b { color: #198b67; background: #dcf8ee; }
.speech-mainline-card.status-playing > header b { color: #80e4bf; background: rgba(44, 179, 132, .18); }
.speech-mainline-card.status-playing::before {
  border-color: rgba(87, 145, 255, .34);
}
.speech-interrupt-card.status-playing::before {
  border-color: rgba(73, 201, 155, .42);
}
.speech-track-card.status-paused > header b { color: #a46a00; background: #fff0c9; }
.speech-track-card.status-failed > header b { color: #b23b47; background: #ffe2e5; }

@media (prefers-reduced-motion: reduce) {
  .speech-track-card.status-playing::before {
    animation: none;
  }
}

@media (max-width: 900px) {
  .speech-runtime-head { align-items: flex-start; flex-direction: column; gap: 6px; }
  .speech-runtime-head > p { text-align: left; }
  .speech-runtime-track-grid,
  .speech-runtime-track-grid.layout-mainline,
  .speech-runtime-track-grid.layout-interrupt,
  .speech-runtime-track-grid.layout-balanced { grid-template-columns: minmax(0, 1fr); }
}

.room-detail-page .live-room-dashboard {
  min-height: 158px;
}

.room-detail-page .companion-control-card,
.room-detail-page .xiaozhi-device-card {
  position: relative;
  overflow: hidden;
  min-height: 0;
  padding: 11px 14px;
  row-gap: 3px;
  align-self: end;
  border: 1px solid rgba(118, 137, 204, 0.28);
  border-radius: 18px;
  background:
    radial-gradient(circle at 100% 0%, rgba(121, 143, 255, 0.11), transparent 42%),
    linear-gradient(180deg, rgba(251, 252, 255, 0.98) 0%, rgba(241, 245, 253, 0.98) 100%);
  box-shadow:
    0 12px 30px rgba(46, 60, 118, 0.09),
    inset 0 1px 0 rgba(255, 255, 255, 0.96);
}

.room-detail-page .companion-control-card::before,
.room-detail-page .xiaozhi-device-card::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  pointer-events: none;
  box-shadow: inset 0 0 0 1px rgba(136, 155, 230, 0.08);
}

.room-detail-page .companion-control-card {
  isolation: isolate;
}

.room-detail-page .companion-control-card > span,
.room-detail-page .companion-control-card > strong,
.room-detail-page .companion-control-card > small {
  position: relative;
  z-index: 2;
  margin-left: 78px;
}

.companion-mascot {
  position: absolute;
  z-index: 1;
  top: 5px;
  left: 9px;
  width: 64px;
  height: 66px;
  pointer-events: none;
  transform-origin: 50% 75%;
}

.companion-mascot-glow {
  position: absolute;
  left: 2px;
  top: 7px;
  width: 60px;
  height: 58px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(100, 116, 255, .22) 0%, rgba(100, 116, 255, .08) 50%, transparent 72%);
  animation: companionMascotGlow 3.2s ease-in-out infinite;
}

.companion-mascot-face {
  position: absolute;
  left: 7px;
  top: 10px;
  width: 50px;
  height: 50px;
  border: 2px solid rgba(107, 120, 255, .62);
  border-radius: 50%;
  background: linear-gradient(145deg, #ffffff 0%, #eef1ff 66%, #e4e8ff 100%);
  box-shadow: 0 8px 20px rgba(71, 84, 190, .18), inset 0 -4px 9px rgba(91, 103, 202, .08);
}

.companion-mascot-eye {
  position: absolute;
  top: 16px;
  width: 11px;
  height: 14px;
  border-radius: 50%;
  background: linear-gradient(180deg, #46517b, #313a61);
  box-shadow: inset 0 1px 1px rgba(255,255,255,.18);
  transform-origin: center;
  transition: transform .08s ease, height .08s ease, top .08s ease;
}

.companion-mascot-eye::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 3px;
  height: 3px;
  border-radius: 50%;
  background: rgba(255,255,255,.92);
}

.companion-mascot-eye.eye-left { left: 9px; }
.companion-mascot-eye.eye-right { right: 9px; }
.companion-mascot-eye.blink { top: 22px; height: 2px; transform: scaleY(.7); }
.companion-mascot-eye.blink::after { display: none; }

.companion-mascot-brow {
  position: absolute;
  z-index: 2;
  top: 5px;
  width: 15px;
  height: 3px;
  border-radius: 999px;
  background: #59648f;
  transition: transform .22s ease, top .22s ease;
}

.companion-mascot-brow.brow-left { left: 12px; transform: rotate(8deg); }
.companion-mascot-brow.brow-right { right: 12px; transform: rotate(-8deg); }
.companion-mascot.brow-left-up .brow-left { top: 1px; transform: rotate(-12deg); }
.companion-mascot.brow-right-up .brow-right { top: 1px; transform: rotate(12deg); }
.companion-mascot.brow-both-up .companion-mascot-brow { top: 1px; }

.companion-mascot-mouth {
  position: absolute;
  left: 50%;
  top: 34px;
  display: block;
  transform: translateX(-50%);
  transition: width .2s ease, height .2s ease, border .2s ease, border-radius .2s ease;
}

.companion-mascot-mouth.mouth-smile {
  width: 17px;
  height: 7px;
  border-bottom: 2px solid #59658f;
  border-radius: 0 0 14px 14px;
}

.companion-mascot-mouth.mouth-flat {
  width: 15px;
  height: 1px;
  border-bottom: 2px solid #59658f;
  border-radius: 999px;
}

.companion-mascot-mouth.mouth-open {
  top: 32px;
  width: 9px;
  height: 9px;
  border: 2px solid #59658f;
  border-radius: 50%;
}

.companion-mascot.motion-hop { animation: companionMascotHop .72s ease; }
.companion-mascot.motion-tilt-left { animation: companionMascotTiltLeft .72s ease; }
.companion-mascot.motion-tilt-right { animation: companionMascotTiltRight .72s ease; }

@keyframes companionMascotGlow {
  0%, 100% { opacity: .55; transform: scale(.96); }
  50% { opacity: .9; transform: scale(1.04); }
}

@keyframes companionMascotHop {
  0%, 100% { transform: translateY(0) scale(1); }
  45% { transform: translateY(-7px) scale(1.03); }
}

@keyframes companionMascotTiltLeft {
  0%, 100% { transform: rotate(0deg); }
  50% { transform: rotate(-8deg) translateY(-2px); }
}

@keyframes companionMascotTiltRight {
  0%, 100% { transform: rotate(0deg); }
  50% { transform: rotate(8deg) translateY(-2px); }
}

@media (prefers-reduced-motion: reduce) {
  .companion-mascot,
  .companion-mascot-glow,
  .companion-mascot-eye,
  .companion-mascot-brow,
  .companion-mascot-mouth {
    animation: none !important;
    transition: none !important;
  }
}

.live-room-dashboard {
  position: relative;
  overflow: hidden;
  border: 1px solid rgba(121, 143, 255, 0.24);
  border-radius: 18px;
  background:
    radial-gradient(circle at 52% -30%, rgba(105, 125, 255, 0.18), transparent 50%),
    linear-gradient(135deg, #fbfcff 0%, #f7f8ff 48%, #fbfdff 100%);
  box-shadow: 0 16px 42px rgba(41, 55, 112, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.92);
  display: grid;
  grid-template-columns: 0.88fr 1.45fr;
  grid-template-rows: auto 1fr auto;
  column-gap: 22px;
}

.live-room-dashboard::before {
  content: '';
  position: absolute;
  inset: 0 auto 0 0;
  width: 4px;
  background: linear-gradient(180deg, #6f7dff, #42b8ff);
  opacity: 0.72;
}

.live-room-dashboard.heat-hot::before {
  background: linear-gradient(180deg, #ff7b7b, #ffb648);
}

.live-room-dashboard.heat-active::before {
  background: linear-gradient(180deg, #6f7dff, #45c7af);
}

.live-room-dashboard-head {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(139, 151, 193, 0.15);
}

.live-room-dashboard-head > div:first-child {
  display: grid;
  gap: 2px;
}

.dashboard-kicker {
  font-size: 10px;
  font-weight: 900;
  letter-spacing: 0.18em;
  color: #7784b8;
}

.live-room-dashboard-head strong {
  font-size: 16px;
  color: #1e2745;
}

.dashboard-live-state {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: 30px;
  padding: 0 11px;
  border: 1px solid #dbe3ef;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.72);
  color: #68738c;
  font-size: 12px;
  font-weight: 800;
}

.dashboard-session-meta {
  display: flex;
  align-items: center;
  gap: 22px;
  margin-top: 8px;
  color: #7f8aa0;
  font-size: 20px;
  line-height: 1.2;
  font-weight: 800;
  white-space: nowrap;
}

.dashboard-session-meta b {
  margin-left: 6px;
  color: #4e5b76;
  font-size: 20px;
  font-weight: 900;
}

.dashboard-live-state i {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #a7b0c1;
  box-shadow: 0 0 0 4px rgba(167, 176, 193, 0.14);
}

.dashboard-live-state.online i {
  background: #36b98c;
  box-shadow: 0 0 0 4px rgba(54, 185, 140, 0.14), 0 0 12px rgba(54, 185, 140, 0.4);
}

.dashboard-main-metric {
  display: grid;
  align-content: center;
  justify-items: start;
  min-width: 150px;
  padding: 10px 0 4px 4px;
}

.dashboard-online-value {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
  margin-top: 2px;
  white-space: nowrap;
}

.dashboard-online-value strong,
.dashboard-online-value small {
  margin-top: 0;
}

.dashboard-main-metric span {
  color: #7b879f;
  font-size: 12px;
  font-weight: 800;
}

.dashboard-main-metric strong {
  margin-top: 2px;
  color: #202a50;
  font-size: clamp(38px, 3vw, 56px);
  line-height: 1;
  letter-spacing: -0.04em;
}

.dashboard-main-metric small {
  margin-top: 3px;
  color: #8b95aa;
  font-size: 11px;
}

.dashboard-flow-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  align-items: stretch;
  gap: 8px;
  padding: 10px 0 4px;
}

.dashboard-flow-grid > div {
  min-width: 0;
  padding: 10px 11px;
  border: 1px solid rgba(134, 149, 202, 0.14);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.7);
  display: grid;
  align-content: center;
  gap: 2px;
}

.dashboard-flow-grid span,
.dashboard-pulse-copy span,
.dashboard-connection-copy span {
  color: #7a859c;
  font-size: 10px;
  font-weight: 800;
}

.dashboard-flow-grid strong {
  color: #2d385e;
  font-size: 22px;
  line-height: 1.1;
}

.dashboard-flow-grid small,
.dashboard-connection-copy small {
  color: #9aa3b5;
  font-size: 9px;
}

.dashboard-flow-grid .dashboard-order-signal {
  border-color: rgba(224, 154, 79, 0.34);
  background: linear-gradient(145deg, rgba(255, 247, 232, 0.96), rgba(255, 252, 245, 0.88));
  box-shadow: 0 6px 18px rgba(184, 104, 31, 0.08);
}

.dashboard-flow-grid .dashboard-order-signal strong { color: #b7631f; }

.dashboard-pulse-bar {
  grid-column: 1 / -1;
  display: grid;
  grid-template-columns: 128px minmax(160px, 1fr) auto;
  align-items: end;
  gap: 14px;
  min-height: 48px;
  padding-top: 8px;
}

.dashboard-pulse-copy,
.dashboard-connection-copy {
  display: grid;
  gap: 2px;
}

.dashboard-pulse-copy strong {
  color: #384469;
  font-size: 13px;
}

.dashboard-connection-copy {
  text-align: right;
}

.dashboard-pulse-track {
  height: 34px;
  display: flex;
  align-items: end;
  gap: 4px;
}

.dashboard-pulse-track i {
  flex: 1;
  min-width: 3px;
  max-width: 10px;
  border-radius: 4px 4px 2px 2px;
  background: linear-gradient(180deg, rgba(92, 113, 255, 0.88), rgba(78, 185, 233, 0.46));
  box-shadow: 0 0 10px rgba(95, 117, 255, 0.1);
  transition: height 0.35s ease;
}

.companion-control-card,
.xiaozhi-device-card {
  align-content: start;
}

.companion-control-card > span,
.xiaozhi-device-card > span {
  font-size: 11px;
  line-height: 1.2;
}

.companion-control-card > strong,
.xiaozhi-device-card .device-runtime-status strong {
  font-size: 17px;
  line-height: 1.15;
}

.companion-control-card > small,
.xiaozhi-device-card > small {
  min-height: 0;
  font-size: 10px;
  line-height: 1.25;
}

.xiaozhi-device-card .device-runtime-status {
  margin-top: 1px;
  gap: 6px;
}

.xiaozhi-device-card .device-runtime-status.state-working i {
  background: #22c55e;
  box-shadow: 0 0 0 5px rgba(34, 197, 94, .12);
}
.xiaozhi-device-card .device-runtime-status.state-working strong { color: #169447; }
.xiaozhi-device-card .device-runtime-status.state-paused i {
  background: #f59e0b;
  box-shadow: 0 0 0 5px rgba(245, 158, 11, .13);
}
.xiaozhi-device-card .device-runtime-status.state-paused strong { color: #b7790b; }
.xiaozhi-device-card .device-runtime-status.state-offline i {
  background: #ef4444;
  box-shadow: 0 0 0 5px rgba(239, 68, 68, .11);
}
.xiaozhi-device-card .device-runtime-status.state-offline strong { color: #d83a3a; }

.companion-mode-switch {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 7px;
  width: 100%;
  margin-top: 10px;
  padding: 4px;
  border: 1px solid rgba(111, 123, 165, .16);
  border-radius: 13px;
  background: rgba(244, 246, 251, .86);
}
.companion-mode-switch button {
  min-height: 34px;
  border: 0;
  border-radius: 9px;
  color: #7b8599;
  background: transparent;
  font: inherit;
  font-size: 12px;
  font-weight: 850;
  cursor: pointer;
}
.companion-mode-switch button.active {
  color: #4d5bc7;
  background: #fff;
  box-shadow: 0 4px 12px rgba(63, 76, 131, .12);
}
.companion-mode-switch button:disabled { cursor: wait; opacity: .62; }
.companion-plan-select {
  display: block;
  width: min(190px, calc(100% - 28px));
  min-width: 150px;
  margin: 2px 6px 0 auto;
  justify-self: end;
  align-self: start;
}
.companion-plan-select select {
  width: 100%;
  box-sizing: border-box;
  text-align: left;
  min-height: 38px;
  padding: 0 34px 0 12px;
  border: 1px solid rgba(103, 116, 172, .18);
  border-radius: 12px;
  color: #4f5d7b;
  background: rgba(255, 255, 255, .9);
  font: inherit;
  font-size: 12px;
  font-weight: 800;
  outline: none;
  cursor: pointer;
}
.companion-plan-select select:focus {
  border-color: rgba(82, 101, 225, .44);
  box-shadow: 0 0 0 3px rgba(82, 101, 225, .09);
}
.companion-plan-select select:disabled { cursor: wait; opacity: .64; }
.companion-plan-error {
  display: block;
  width: 100%;
  color: #c84a52 !important;
  font-size: 10px !important;
  line-height: 1.35 !important;
}

.companion-control-buttons,
.xiaozhi-device-card .device-sim-controls {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  width: 100%;
  margin-top: 12px;
}

.xiaozhi-device-card .device-sim-controls {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}
.xiaozhi-device-card .device-sim-controls.is-offline {
  grid-template-columns: minmax(0, 1fr);
}
.xiaozhi-device-card .device-control-error {
  color: #c84a52;
  font-size: 10px;
  line-height: 1.35;
}

.companion-control-buttons button,
.xiaozhi-device-card .device-sim-controls button {
  width: 100%;
  min-width: 0;
  height: 46px;
  padding: 0 10px;
  border: 1px solid #d3dbea;
  border-radius: 13px;
  color: #5c687e;
  background: linear-gradient(180deg, #ffffff 0%, #f7f9fc 100%);
  box-shadow: 0 5px 14px rgba(47, 61, 99, 0.08), inset 0 1px 0 rgba(255, 255, 255, 0.92);
  font: inherit;
  font-size: 14px;
  font-weight: 850;
  letter-spacing: .02em;
  transition: transform .16s ease, box-shadow .16s ease, border-color .16s ease, filter .16s ease;
}

.companion-control-buttons button:first-child {
  border-color: rgba(93, 94, 238, 0.42);
  color: #ffffff;
  background: linear-gradient(135deg, #6b6ff5 0%, #5558dc 100%);
  box-shadow: 0 7px 18px rgba(82, 85, 220, 0.24), inset 0 1px 0 rgba(255, 255, 255, 0.24);
}

.companion-control-buttons button.pause {
  border-color: #cbd4e3;
  color: #4f5d73;
  background: linear-gradient(180deg, #fbfcfe 0%, #eef2f7 100%);
  box-shadow: 0 5px 14px rgba(73, 88, 116, 0.11), inset 0 1px 0 rgba(255, 255, 255, 0.96);
}

.companion-control-buttons button.end {
  border-color: rgba(224, 118, 128, 0.42);
  color: #b24d58;
  background: linear-gradient(180deg, #fffafb 0%, #fff0f2 100%);
  box-shadow: 0 5px 14px rgba(190, 77, 91, 0.12), inset 0 1px 0 rgba(255, 255, 255, 0.9);
}

.companion-control-buttons button:not(:disabled):hover {
  transform: translateY(-2px);
  filter: brightness(1.03);
  box-shadow: 0 10px 22px rgba(68, 81, 133, 0.18);
}

.companion-control-buttons button:not(:disabled):active {
  transform: translateY(0);
}

.companion-control-buttons button:disabled {
  opacity: .48;
  cursor: not-allowed;
  box-shadow: none;
  filter: saturate(.72);
}

.room-detail-page .event-row {
  grid-template-columns: 64px 54px minmax(0, 1fr);
  cursor: context-menu;
  transition: background 0.16s ease, box-shadow 0.16s ease;
}

.room-detail-page .event-row.has-ai-actions {
  grid-template-columns: 64px 54px minmax(0, 1fr) auto;
  align-items: center;
}

.event-ai-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  min-width: 158px;
}
.event-ai-actions button {
  min-width: 54px;
  height: 30px;
  padding: 0 10px;
  border-radius: 9px;
  border: 1px solid rgba(83, 98, 190, .18);
  font-size: 12px;
  font-weight: 850;
  cursor: pointer;
  white-space: nowrap;
}
.event-ai-actions button.quick {
  color: #b84b36;
  border-color: rgba(217, 103, 75, .26);
  background: #fff4ef;
}
.event-ai-actions button.answer {
  color: #4f5fd0;
  background: #f4f6ff;
}
.event-ai-actions button:disabled {
  opacity: .5;
  cursor: not-allowed;
}
.event-ai-actions small {
  max-width: 128px;
  font-size: 10px;
  line-height: 1.25;
}
.event-ai-actions small.ok { color: #3d8a6e; }
.event-ai-actions small.error { color: #c84d59; }

.room-detail-page .event-body,
.room-detail-page .event-inline {
  min-width: 0;
  max-width: 100%;
}

.room-detail-page .event-inline {
  display: block;
  white-space: normal;
}

.room-detail-page .event-user,
.room-detail-page .event-action {
  display: inline;
  max-width: 100%;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.room-detail-page .event-row.is-hovered {
  position: relative;
  z-index: 2;
  background: linear-gradient(90deg, rgba(111, 125, 255, 0.09), rgba(86, 190, 232, 0.035));
  box-shadow: inset 3px 0 0 rgba(91, 105, 235, 0.78);
}

.agent-decision-panel {
  --agent-thinking-height: 150px;
  box-sizing: border-box;
  min-width: 0;
  width: 100%;
  max-width: 340px;
  display: grid;
  grid-template-rows: auto var(--agent-thinking-height) 18px minmax(0, 1fr) 18px;
  gap: 5px;
  padding: 11px;
  border: 1px solid rgba(110, 126, 205, .28);
  border-radius: 18px;
  background:
    radial-gradient(circle at 100% 0%, rgba(106, 119, 255, .16), transparent 38%),
    linear-gradient(180deg, rgba(251,252,255,.99), rgba(244,247,255,.99));
  box-shadow: 0 14px 34px rgba(42, 55, 105, .09);
  overflow: hidden;
}
.agent-decision-panel.is-resizing { user-select:none; box-shadow:0 16px 38px rgba(42,55,105,.13), inset 0 0 0 1px rgba(90,104,220,.12); }
.agent-decision-head { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.agent-decision-head h3 { margin:0; color:#29324d; font-size:18px; line-height:1.2; }
.agent-decision-state { flex:0 0 auto; padding:5px 8px; border-radius:999px; color:#77829a; background:#eef1f7; font-size:10px; font-weight:900; }
.agent-decision-state.ready { color:#a45d14; background:#fff0d7; box-shadow:inset 0 0 0 1px rgba(218,151,66,.18); }
.agent-thinking-zone { position:relative; min-height:0; height:100%; display:grid; align-content:start; gap:4px; padding:3px 4px; overflow:hidden; }
.agent-interrupt-zone { min-height:0; height:100%; display:grid; grid-template-rows:auto minmax(0,1fr) auto; align-content:stretch; gap:6px; padding:8px; border:1px solid rgba(128,142,194,.14); border-radius:14px; background:rgba(255,255,255,.76); overflow:hidden; }
.agent-zone-title { display:flex; align-items:center; justify-content:space-between; gap:8px; }
.agent-zone-title strong { color:#536079; font-size:16px; font-weight:900; line-height:1.2; letter-spacing:.02em; }
.agent-zone-title small { color:#9aa4b6; font-size:9px; }
.agent-scan-watermark { position:absolute; z-index:0; inset:0; display:grid; place-items:start center; padding-top:5px; pointer-events:none; user-select:none; }
.agent-scan-watermark strong { color:#b7bfce; font-size:14px; font-weight:850; letter-spacing:.06em; opacity:.66; }
.agent-scan-watermark.scanning strong { color:transparent; background:linear-gradient(90deg,#b8c0cf 0%,#b8c0cf 35%,#7f92ee 47%,#9bd8f1 50%,#7f92ee 53%,#b8c0cf 65%,#b8c0cf 100%); background-size:260% 100%; background-clip:text; -webkit-background-clip:text; animation:agentScanTextSweep 1.8s linear infinite; }
@keyframes agentScanTextSweep { from { background-position:120% 0; } to { background-position:-120% 0; } }
.agent-decision-note-list { position:relative; z-index:1; min-height:0; height:calc(100% - 22px); max-height:none; display:grid; align-content:start; gap:4px; margin-top:22px; overflow-y:auto; padding-right:3px; overscroll-behavior:contain; scrollbar-width:thin; }
.agent-decision-note-list article { display:grid; gap:2px; padding:5px 7px; border-left:3px solid #aeb8e8; border-radius:7px; background:#f8f9fd; }
.agent-decision-note-list header { display:flex; align-items:center; justify-content:space-between; gap:8px; }
.agent-decision-note-list b { color:#6874a8; font-size:9px; }
.agent-decision-note-list time { color:#a0a9ba; font-size:8px; }
.agent-decision-note-list p { margin:0; color:#69748a; font-size:9px; line-height:1.45; overflow-wrap:anywhere; }
.agent-decision-empty { display:grid; place-items:center; min-height:62px; padding:10px; border:1px dashed rgba(126,140,190,.22); border-radius:11px; color:#9aa4b5; font-size:10px; line-height:1.6; text-align:center; }
.agent-decision-empty.compact { min-height:34px; padding:7px; }
.agent-interrupt-list { min-height:0; height:100%; max-height:none; display:grid; align-content:start; gap:7px; overflow-y:auto; padding:2px 5px 2px 0; overscroll-behavior:contain; scrollbar-width:thin; }
.agent-interrupt-item { display:grid; gap:5px; padding:9px 10px; border:1px solid rgba(130,143,184,.18); border-radius:11px; background:#fbfcff; box-shadow:0 5px 14px rgba(50,61,104,.05); }
.agent-interrupt-item.manual { border-color:rgba(102,111,226,.38); background:linear-gradient(135deg,#f1f1ff,#f8f8ff); box-shadow:0 8px 18px rgba(86,91,202,.10); }
.agent-interrupt-item.high:not(.manual) { border-color:rgba(226,151,61,.34); background:linear-gradient(135deg,#fff7ea,#fffbf4); }
.agent-interrupt-item.claimed { box-shadow:inset 3px 0 0 #47b894, 0 8px 18px rgba(53,132,106,.08); }
.agent-interrupt-item > header,.agent-interrupt-item > footer { display:flex; align-items:center; justify-content:space-between; gap:8px; }
.agent-source-badge { padding:3px 6px; border-radius:999px; color:#69758d; background:#eef1f6; font-size:8px; font-weight:900; }
.agent-interrupt-item.manual .agent-source-badge { color:#5559bd; background:#e8e9ff; }
.agent-interrupt-item > header b { color:#8b95a8; font-size:8px; }
.agent-interrupt-item > strong { color:#36415b; font-size:11px; line-height:1.35; overflow-wrap:anywhere; }
.agent-interrupt-item > p { margin:0; color:#7c879b; font-size:9px; line-height:1.45; overflow-wrap:anywhere; }
.agent-interrupt-item > footer { color:#98a1b2; font-size:8px; }
.agent-cooldown-strip { display:flex; flex-wrap:wrap; align-items:center; gap:5px; padding-top:7px; border-top:1px solid rgba(128,141,183,.12); }
.agent-cooldown-strip > span { color:#9aa4b5; font-size:8px; font-weight:850; }
.agent-cooldown-strip b { padding:3px 6px; border-radius:999px; color:#8d6c35; background:#fff3d9; font-size:8px; font-weight:850; }
.agent-cooldown-strip em { font-style:normal; }
.agent-panel-resize-handle { position:relative; display:grid; place-items:center; width:100%; height:18px; cursor:ns-resize; touch-action:none; user-select:none; }
.agent-panel-resize-handle::before { content:""; position:absolute; left:8px; right:8px; top:50%; height:1px; background:rgba(126,136,167,.18); transform:translateY(-50%); }
.agent-panel-resize-handle.is-split { align-self:center; }
.agent-panel-resize-handle.is-bottom { align-self:end; }
.agent-panel-resize-lines { position:relative; z-index:1; display:block; width:34px; height:1px; border-radius:999px; background:rgba(104,115,150,.48); box-shadow:0 5px 0 rgba(104,115,150,.34); opacity:.9; transform:translateY(-2px); transition:width .16s ease,background .16s ease,box-shadow .16s ease,opacity .16s ease; }
.agent-panel-resize-handle:hover .agent-panel-resize-lines,
.agent-decision-panel.is-resizing .agent-panel-resize-lines,
.event-bucket-panel.is-resizing .agent-panel-resize-lines { width:44px; background:rgba(85,96,207,.68); box-shadow:0 5px 0 rgba(85,96,207,.46); opacity:1; }

.semantic-bucket-panel {
  min-width: 0;
  max-width: 360px;
  padding: 14px;
  border: 1px solid rgba(129, 145, 204, 0.22);
  border-radius: 18px;
  background: radial-gradient(circle at 100% 0%, rgba(112, 129, 255, 0.10), transparent 42%), linear-gradient(180deg, rgba(253,253,255,.98), rgba(246,248,253,.98));
  box-shadow: 0 10px 28px rgba(42, 55, 98, 0.06);
}
.event-bucket-panel.is-sized { display:flex; flex-direction:column; overflow:hidden; }
.event-bucket-panel.is-sized .event-bucket-smart-section:not(.is-collapsed) { flex:1 1 auto; height:auto; min-height:0; }
.event-bucket-panel.is-sized .event-bucket-smart-section.is-collapsed { flex:0 0 auto; }
.event-bucket-resize-handle { flex:0 0 18px; margin-top:5px; }
.semantic-bucket-head { margin-bottom: 10px; }
.semantic-bucket-head h3 { margin: 3px 0 0; }
.semantic-bucket-count { padding: 5px 8px; border-radius: 999px; color: #73809a; font-size: 11px; font-weight: 800; background: rgba(238,241,248,.86); }
.event-bucket-section { display: grid; gap: 8px; }
.event-bucket-smart-section { margin-top:12px; padding-top:12px; border-top:1px solid rgba(132,145,187,.14); }
.event-bucket-smart-section:not(.is-collapsed) { display:flex; flex-direction:column; height:clamp(420px,68vh,760px); min-height:420px; overflow:hidden; }
.event-bucket-section-title { color: #8b95a7; font-size: 10px; font-weight: 850; letter-spacing: .08em; }
.base-event-bucket-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; }
.base-event-bucket { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-width: 0; padding: 8px 10px; border: 1px solid transparent; border-radius: 11px; font-size: 12px; }
.base-event-bucket span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.base-event-bucket strong { flex: 0 0 auto; font-size: 12px; font-weight: 900; }
.base-event-bucket.base-member { color:#2d806b; border-color:#cce9df; background:#eef9f5; }
.base-event-bucket.base-chat { color:#5555bd; border-color:#d8d8fb; background:#f1f1ff; }
.base-event-bucket.base-like { color:#b84e65; border-color:#f1ced7; background:#fff1f4; }
.base-event-bucket.base-follow { color:#95671f; border-color:#f0dfbd; background:#fff8e9; }
.base-event-bucket.base-gift { color:#8750aa; border-color:#e5d3f0; background:#f9f0ff; }
.base-event-bucket.base-order-signal { grid-column:1 / -1; color:#b45b18; border-color:#f0c58f; background:linear-gradient(90deg,#fff6e7,#fffaf1); box-shadow:0 5px 14px rgba(188,105,30,.08); }
.important-history-loading { padding:10px; color:#8a94a7; font-size:11px; text-align:center; }
.question-cluster-heading { width:100%; display:flex; align-items:flex-start; justify-content:space-between; gap:12px; margin-bottom:2px; padding:0; border:0; color:inherit; background:transparent; font:inherit; text-align:left; cursor:pointer; }
.question-cluster-heading > div { display:grid; gap:4px; }
.question-cluster-heading .event-bucket-section-title { color:#4f5b79; font-size:14px; font-weight:900; letter-spacing:.035em; }
.question-cluster-heading small { color:#9aa4b6; font-size:10px; line-height:1.4; }
.question-cluster-heading-actions { display:flex; align-items:center; gap:7px; flex:0 0 auto; }
.question-cluster-freeze-badge { padding:4px 7px; border-radius:999px; color:#7d879b; background:#f1f3f8; font-size:9px; font-style:normal; font-weight:850; white-space:nowrap; }
.question-cluster-heading-actions b { padding:5px 8px; border-radius:999px; color:#5c68b4; background:#eef0ff; font-size:10px; font-weight:900; }
.question-cluster-toggle-icon { width:28px; height:28px; display:grid; place-items:center; border:1px solid rgba(111,126,190,.18); border-radius:50%; color:#74809b; background:rgba(247,249,255,.92); box-shadow:0 4px 12px rgba(67,80,132,.07); font-style:normal; transition:transform .18s ease, color .18s ease, border-color .18s ease, background .18s ease, box-shadow .18s ease; }
.question-cluster-toggle-icon svg { width:15px; height:15px; fill:none; stroke:currentColor; stroke-width:2; stroke-linecap:round; stroke-linejoin:round; transition:transform .18s ease; }
.question-cluster-toggle-icon.collapsed svg { transform:rotate(180deg); }
.question-cluster-heading:hover .question-cluster-toggle-icon { color:#5968c7; border-color:rgba(93,108,199,.30); background:#f1f3ff; box-shadow:0 6px 16px rgba(67,80,132,.11); }
.semantic-bucket-empty { padding: 16px 12px; border:1px dashed rgba(128,143,198,.28); border-radius:13px; color:#8e99ad; background:rgba(248,250,255,.8); font-size:11px; line-height:1.65; }
.semantic-bucket-list { display:grid; gap:9px; min-height:0; }
.event-bucket-smart-section:not(.is-collapsed) .semantic-bucket-list { flex:1 1 auto; overflow-y:auto; padding:2px 8px 4px 0; overscroll-behavior:contain; overflow-anchor:none; scrollbar-gutter:stable; scrollbar-width:thin; }
.semantic-bucket-list.is-scrollable { overflow-y:auto; }
.semantic-bucket-card { --bucket-accent:#5c68b4; --bucket-hover:#f2f4ff; min-width:0; overflow:visible; border:1px solid transparent; border-radius:14px; box-shadow:0 6px 16px rgba(45,57,101,.055); transition:box-shadow .18s ease, border-color .18s ease, filter .18s ease; }
.semantic-bucket-card:hover { border-color:rgba(93,108,199,.24); box-shadow:0 10px 24px rgba(45,57,101,.10); filter:saturate(1.03) brightness(1.01); }
.semantic-bucket-card.expanded { box-shadow:0 12px 28px rgba(45,57,101,.11); }
.semantic-bucket-summary-row { display:grid; grid-template-columns:minmax(0,1fr) auto; align-items:center; gap:7px; min-width:0; padding-right:10px; }
.semantic-bucket-summary { width:100%; min-width:0; display:grid; grid-template-columns:minmax(0,1fr) auto 16px; align-items:center; gap:9px; padding:11px 0 11px 12px; border:0; color:inherit; background:transparent; font:inherit; text-align:left; cursor:pointer; }
.semantic-bucket-copy { display:grid; min-width:0; gap:3px; }
.semantic-bucket-copy > strong { overflow:hidden; color:currentColor; font-size:13px; font-weight:900; line-height:1.35; text-overflow:ellipsis; white-space:nowrap; }
.semantic-bucket-copy > small { overflow:hidden; color:#8c96aa; font-size:9px; line-height:1.35; text-overflow:ellipsis; white-space:nowrap; }
.semantic-bucket-number { min-width:34px; padding:5px 8px; border-radius:999px; color:currentColor; background:rgba(255,255,255,.72); box-shadow:inset 0 0 0 1px rgba(255,255,255,.9); font-size:12px; font-weight:950; text-align:center; }
.semantic-bucket-chevron { color:#929bad; font-style:normal; font-size:16px; line-height:1; transition:transform .18s ease; }
.semantic-bucket-card.expanded .semantic-bucket-chevron { transform:rotate(180deg); }
.semantic-bucket-actions { display:flex; align-items:center; justify-content:flex-end; gap:5px; padding:0; flex:0 0 auto; }
.semantic-bucket-actions button,
.question-detail-actions button { min-width:38px; padding:5px 7px; border:1px solid rgba(92,105,213,.24); border-radius:8px; color:#5965b9; background:rgba(238,240,255,.92); font:inherit; font-size:9px; font-weight:900; line-height:1.2; cursor:pointer; white-space:nowrap; }
.semantic-bucket-actions button.force,
.question-detail-actions button.force { color:#b45a3d; border-color:rgba(236,153,126,.52); background:#fff2ec; }
.semantic-bucket-actions button:disabled,
.question-detail-actions button:disabled { opacity:.42; cursor:not-allowed; }
.question-detail-list { display:grid; height:auto; max-height:none; gap:6px; padding:8px; border-top:1px solid rgba(120,135,183,.13); background:rgba(255,255,255,.68); overflow:visible; }
.question-detail-item { display:grid; gap:5px; min-width:0; }
.question-detail-actions { display:flex; justify-content:flex-end; gap:5px; padding:0 3px 2px; }
.question-detail-row { width:100%; display:grid; gap:5px; padding:8px 9px; border:1px solid rgba(130,143,184,.15); border-radius:10px; color:#4b566e; background:rgba(255,255,255,.82); font:inherit; text-align:left; cursor:pointer; transform:translateX(0); transition:transform .18s ease, border-color .18s ease, background .18s ease, box-shadow .18s ease, color .18s ease; will-change:transform; }
.question-detail-row:hover { transform:translateX(-6px); border-color:var(--bucket-accent); color:var(--bucket-accent); background:var(--bucket-hover); box-shadow:0 8px 18px rgba(56,68,119,.11); }
.question-detail-row.selected { border-color:var(--bucket-accent); color:var(--bucket-accent); background:var(--bucket-hover); box-shadow:0 6px 16px rgba(71,84,178,.10); }
.question-detail-row.selected:hover { transform:translateX(-6px); box-shadow:0 9px 20px rgba(56,68,119,.14); }
.question-detail-row > span { display:flex; align-items:center; justify-content:space-between; gap:8px; color:#8c96a9; font-size:9px; transition:color .18s ease; }
.question-detail-row > span b { overflow:hidden; color:#667087; font-weight:850; text-overflow:ellipsis; white-space:nowrap; transition:color .18s ease; }
.question-detail-row p { margin:0; color:#333d55; font-size:11px; font-weight:700; line-height:1.5; overflow-wrap:anywhere; transition:color .18s ease; }
.question-detail-row:hover > span,
.question-detail-row:hover > span b,
.question-detail-row:hover p,
.question-detail-row.selected > span,
.question-detail-row.selected > span b,
.question-detail-row.selected p { color:var(--bucket-accent); }
.question-detail-more { padding:3px 5px 1px; color:#9aa4b5; font-size:9px; text-align:center; }
.single-question-focus { display:grid; gap:5px; margin-top:2px; padding:11px 12px; border:1px solid rgba(97,113,224,.25); border-radius:13px; background:linear-gradient(135deg,#f4f5ff,#f8fbff); box-shadow:0 8px 20px rgba(69,83,177,.07); }
.single-question-focus > span { color:#6a76c5; font-size:9px; font-weight:900; letter-spacing:.08em; }
.single-question-focus > strong { color:#29334d; font-size:13px; line-height:1.5; overflow-wrap:anywhere; }
.single-question-focus > small { color:#8e98aa; font-size:9px; }
.single-question-actions { display:flex; gap:7px; margin-top:4px; }
.single-question-actions button { min-width:0; padding:7px 10px; border:1px solid rgba(92,105,213,.26); border-radius:9px; color:#5965b9; background:#eef0ff; font:inherit; font-size:9px; font-weight:900; cursor:pointer; }
.single-question-actions button.force { color:#a25c1f; border-color:#f0c893; background:#fff5e5; }
.single-question-actions button:disabled { opacity:.55; cursor:wait; }
.single-question-action-message { color:#66728c !important; line-height:1.45; }
.single-question-expired { color:#b06b2a !important; font-weight:750; line-height:1.45; }
.semantic-bucket-card.tone-violet { color:#6957b8; border-color:#ddd5fa; background:linear-gradient(135deg,#f8f6ff,#f1efff); }
.semantic-bucket-card.tone-rose { color:#a14f68; border-color:#f3d1dc; background:linear-gradient(135deg,#fff7f9,#fff0f5); }
.semantic-bucket-card.tone-blue { color:#426ca8; border-color:#d1e0f6; background:linear-gradient(135deg,#f6faff,#edf5ff); }
.semantic-bucket-card.tone-orange { color:#9a642c; border-color:#f2dcc2; background:linear-gradient(135deg,#fffaf3,#fff4e5); }
.semantic-bucket-card.tone-cyan { color:#35768b; border-color:#cae7ee; background:linear-gradient(135deg,#f5fcfe,#ebf9fd); }
.semantic-bucket-card.tone-green { color:#3d7a62; border-color:#cce8db; background:linear-gradient(135deg,#f6fcf9,#ecf8f3); }
.semantic-bucket-card.tone-indigo { color:#5360a4; border-color:#d5daf5; background:linear-gradient(135deg,#f7f8ff,#eef1ff); }
.semantic-bucket-card.tone-red { color:#a64b4b; border-color:#f1cccc; background:linear-gradient(135deg,#fff8f8,#fff0f0); }
.semantic-bucket-card.tone-teal { color:#327c78; border-color:#c7e8e4; background:linear-gradient(135deg,#f5fcfb,#eaf8f6); }
.semantic-bucket-card.tone-purple { color:#8159a5; border-color:#e3d4f0; background:linear-gradient(135deg,#fbf7fd,#f6effb); }
.semantic-bucket-card.tone-gray { color:#667085; border-color:#dfe3ea; background:linear-gradient(135deg,#fafbfc,#f4f6f8); }
.semantic-bucket-card.tone-violet { --bucket-accent:#6957b8; --bucket-hover:#f3f0ff; }
.semantic-bucket-card.tone-rose { --bucket-accent:#a14f68; --bucket-hover:#fff0f5; }
.semantic-bucket-card.tone-blue { --bucket-accent:#426ca8; --bucket-hover:#edf5ff; }
.semantic-bucket-card.tone-orange { --bucket-accent:#9a642c; --bucket-hover:#fff4e5; }
.semantic-bucket-card.tone-cyan { --bucket-accent:#35768b; --bucket-hover:#ebf9fd; }
.semantic-bucket-card.tone-green { --bucket-accent:#3d7a62; --bucket-hover:#ecf8f3; }
.semantic-bucket-card.tone-indigo { --bucket-accent:#5360a4; --bucket-hover:#eef1ff; }
.semantic-bucket-card.tone-red { --bucket-accent:#a64b4b; --bucket-hover:#fff0f0; }
.semantic-bucket-card.tone-teal { --bucket-accent:#327c78; --bucket-hover:#eaf8f6; }
.semantic-bucket-card.tone-purple { --bucket-accent:#8159a5; --bucket-hover:#f6effb; }
.semantic-bucket-card.tone-gray { --bucket-accent:#667085; --bucket-hover:#f3f5f8; }

.event-context-menu { position:fixed; z-index:140; display:grid; min-width:190px; max-width:260px; gap:7px; padding:8px; border:1px solid rgba(120,132,166,.23); border-radius:12px; background:rgba(255,255,255,.98); box-shadow:0 16px 42px rgba(25,34,68,.18); backdrop-filter:blur(14px); }
.event-context-menu > span { overflow:hidden; padding:5px 7px 2px; color:#667085; font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.event-context-menu button { width:100%; border:1px solid #f1cfd2; border-radius:9px; padding:8px 10px; color:#a44249; background:#fff5f5; font:inherit; font-size:12px; font-weight:800; text-align:left; cursor:pointer; }
.event-context-menu button:hover { border-color:#e9afb5; background:#ffecec; }
.event-context-menu button:disabled { opacity:.55; cursor:wait; }
.agent-interrupt-item { cursor:context-menu; }
.agent-queue-context-menu button:disabled { cursor:not-allowed; }

.blocked-drawer-handle { position:fixed; top:54%; right:0; z-index:131; display:grid; gap:3px; justify-items:center; min-width:42px; padding:12px 7px; border:1px solid rgba(112,126,181,.26); border-right:0; border-radius:14px 0 0 14px; color:#5e6881; background:rgba(247,249,255,.96); box-shadow:-8px 8px 24px rgba(34,45,86,.10); backdrop-filter:blur(14px); transform:translateY(-50%); transition:right .24s ease, background .18s ease; cursor:pointer; }
.blocked-drawer-handle span { writing-mode:vertical-rl; font-size:11px; font-weight:800; letter-spacing:.08em; }
.blocked-drawer-handle b { min-width:20px; height:20px; border-radius:999px; display:grid; place-items:center; color:#fff; background:#6977cf; font-size:10px; }
.blocked-drawer-handle.open { right:min(360px,88vw); }
.blocked-users-drawer { position:fixed; top:0; right:0; z-index:130; width:min(360px,88vw); height:100dvh; padding:18px; border-left:1px solid rgba(118,132,184,.20); background:radial-gradient(circle at 100% 0%,rgba(111,126,255,.12),transparent 34%),rgba(248,250,255,.985); box-shadow:-22px 0 52px rgba(25,35,73,.15); backdrop-filter:blur(18px); transform:translateX(102%); transition:transform .24s ease; overflow-y:auto; }
.blocked-users-drawer.open { transform:translateX(0); }
.blocked-users-drawer > header { display:flex; justify-content:space-between; gap:14px; padding-bottom:14px; border-bottom:1px solid rgba(132,145,187,.15); }
.blocked-users-drawer h3 { margin:4px 0 3px; }
.blocked-users-drawer header p { margin:0; color:#8b95a8; font-size:11px; line-height:1.5; }
.blocked-users-drawer header button { align-self:flex-start; width:32px; height:32px; border:1px solid #dfe4ee; border-radius:10px; color:#758097; background:rgba(255,255,255,.85); font-size:20px; cursor:pointer; }
.blocked-drawer-error { margin-top:12px; padding:9px 10px; border-radius:10px; color:#a44249; background:#fff0f1; font-size:11px; }
.blocked-drawer-empty { padding:44px 8px; color:#9aa4b5; text-align:center; font-size:12px; }
.blocked-user-list { display:grid; gap:9px; margin-top:14px; }
.blocked-user-row { display:grid; grid-template-columns:minmax(0,1fr) auto; align-items:center; gap:10px; width:100%; padding:11px 12px; border:1px solid rgba(133,147,193,.18); border-radius:12px; color:#333c52; background:rgba(255,255,255,.80); font:inherit; text-align:left; cursor:pointer; }
.blocked-user-row:hover { border-color:#c7d0f0; background:#fff; }
.blocked-user-row > span { display:grid; min-width:0; gap:4px; }
.blocked-user-row strong,.blocked-user-row small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.blocked-user-row small { color:#8a94a7; font-size:10px; }
.blocked-user-row em { color:#6874bb; font-size:10px; font-style:normal; font-weight:800; }
.blocked-user-row:disabled { opacity:.6; cursor:wait; }

.room-detail-page .detail-layout-no-preview {
  width: min(100%, 880px);
  margin-inline: 0;
  grid-template-columns: minmax(460px, 520px) minmax(280px, 340px) !important;
  justify-content: start;
  gap: 16px;
}

.room-detail-page .public-screen-panel {
  grid-column: 1;
  grid-row: 1;
  width: 100%;
  max-width: 520px;
  justify-self: start;
  align-self: stretch;
  overflow: hidden;
}

.room-detail-page .live-control-stack {
  grid-column: 2;
  grid-row: 1;
  display: grid;
  align-self: start;
  align-content: start;
  gap: 16px;
  width: 100%;
  max-width: 340px;
}

.room-detail-page .agent-decision-panel,
.room-detail-page .event-bucket-panel {
  width: 100%;
  max-width: 340px;
  justify-self: start;
  align-self: start;
}

@media(max-width:1260px){
  .room-detail-page .room-control-grid {
    grid-template-columns: minmax(230px, .8fr) minmax(440px, 1.45fr) minmax(230px, .8fr);
  }

  .dashboard-flow-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media(max-width:1050px){
  .room-detail-page .room-control-grid {
    grid-template-columns: repeat(2, minmax(260px, 1fr));
  }

  .live-room-dashboard {
    grid-column: 1 / -1;
    order: -1;
  }
}

@media(max-width:900px){
  .room-detail-page .detail-layout-no-preview {
    width: min(100%, 520px);
    grid-template-columns: minmax(0, 1fr) !important;
  }

  .room-detail-page .public-screen-panel {
    grid-column: 1;
    grid-row: auto;
    max-width: 100%;
    justify-self: stretch;
  }

  .room-detail-page .live-control-stack {
    display: grid;
    grid-column: 1;
    grid-row: auto;
    gap: 16px;
    width: 100%;
    max-width: 100%;
  }

  .room-detail-page .agent-decision-panel,
  .room-detail-page .event-bucket-panel {
    max-width: 100%;
    justify-self: stretch;
  }
}


.session-resume-mask {
  position: fixed;
  inset: 0;
  z-index: 1200;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(244, 246, 252, .72);
  backdrop-filter: blur(10px);
}
.session-resume-dialog {
  width: min(620px, 94vw);
  padding: 30px;
  border: 1px solid rgba(96, 110, 214, .22);
  border-radius: 28px;
  background: rgba(255, 255, 255, .96);
  box-shadow: 0 28px 80px rgba(46, 58, 116, .18);
}
.session-resume-kicker {
  display: block;
  margin-bottom: 10px;
  color: #5261cc;
  font-size: 14px;
  font-weight: 800;
  letter-spacing: .16em;
}
.session-resume-dialog strong {
  display: block;
  color: #263353;
  font-size: 28px;
  line-height: 1.35;
}
.session-resume-dialog p {
  margin: 14px 0 0;
  color: #78849e;
  font-size: 17px;
  line-height: 1.8;
}
.session-resume-actions {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
  margin-top: 24px;
}
.session-resume-actions button {
  min-height: 56px;
  border: 0;
  border-radius: 18px;
  background: #5364d9;
  color: #fff;
  font-size: 18px;
  font-weight: 800;
  cursor: pointer;
}
.session-resume-actions button.fresh {
  border: 1px solid rgba(92, 105, 158, .22);
  background: #f5f7fc;
  color: #425070;
}
.session-resume-actions button:disabled {
  cursor: wait;
  opacity: .58;
}

@media(max-width:720px){
  .live-room-dashboard {
    grid-template-columns: 1fr;
  }

  .live-room-dashboard-head,
  .dashboard-pulse-bar {
    grid-column: 1;
  }

  .dashboard-flow-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .dashboard-pulse-bar {
    grid-template-columns: 1fr;
  }

  .dashboard-connection-copy {
    text-align: left;
  }
}

@media(max-width:800px){
  .room-detail-page .room-control-grid {
    grid-template-columns: 1fr;
  }
}
</style>
