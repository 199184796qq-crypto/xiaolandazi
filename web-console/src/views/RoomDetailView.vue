<script setup lang="ts">
import { confirmAction, useFeedbackErrorRef } from '../uiFeedback'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  getLiveAgentSettings,
  getLiveAgentPlans,
  createLiveAgentPlan,
  getRoomLiveAgentPlans,
  bindRoomLiveAgentPlan,
  setLiveRuntimePlan,
  chatLiveAgent,
  controlLiveDevice,
  getLiveRuntime,
  setLiveRuntimeMode,
  getLiveRuntimeEvents,
  startLiveRuntime,
  pauseLiveRuntime,
  resumeLiveRuntime,
  stopLiveRuntime,
  getRoom,
  setRoomMonitor,
  getRoomEvents,
  getRoomImportantEvents,
  getRoomSessionStats,
  getRoomReview,
  resolveRoomSessionDecision,
  getRoomBrain,
  getRoomSpeechRuntime,
  getRoomSpeechMissions,
  getRoomGeneratedSpeechHistory,
  getRoomAgentDecisions,
  enqueueRoomManualAgentDecision,
  removeRoomAgentDecision,
  getRoomBlockedUsers,
  blockRoomUser,
  restoreRoomBlockedUser,
  getTenants,
  recordLiveRuntimeEvent,
  getRoomCapture,
  startRoomAudioRecording,
  stopRoomAudioRecording,
  roomAudioRecordingFileUrl,
  getRoomSpeechAnalysis,
  startRoomSpeechAnalysis,
  uploadRoomSpeechAnalysis,
  roomSpeechAnalysisTranscriptUrl,
  roomSpeechAnalysisReportUrl,
  getRoomSpeechAnalysisReportText,
} from '../api'
import { session } from '../session'
import { coreRuntime } from '../coreRuntime'
import { getSharedAudioContext, unlockSharedAudioContext } from '../audioRuntime'
import ModulePageNav from '../components/ModulePageNav.vue'
import RoomPreferenceHub from '../components/RoomPreferenceHub.vue'
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
  SpeechMission,
  SpeechTrackRuntime,
  GeneratedSpeechHistoryItem,
  GeneratedSpeechHistoryPage,
  AgentDecisionSnapshot,
  AgentDecisionItem,
  RoomCaptureSnapshot,
  RoomSpeechAnalysisStatus,
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
const coreActionsAvailable = computed(() => coreRuntime.phase === 'online')

const room = ref<Room | null>(null)

function roomDetailCacheKey() {
  return 'livecompanion.room-detail-cache.v1:' + roomId
}

function saveRoomDetailCache(value: Room) {
  try {
    window.localStorage.setItem(roomDetailCacheKey(), JSON.stringify(value))
  } catch {
    // The cache is only used to keep the last known room visible while Core is unavailable.
  }
}

function restoreRoomDetailCache(): Room | null {
  try {
    const raw = window.localStorage.getItem(roomDetailCacheKey())
    if (!raw) return null
    const cached = JSON.parse(raw) as Room
    return cached && Number(cached.id) === roomId ? cached : null
  } catch {
    return null
  }
}

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
const PUBLIC_SCREEN_MIN_ROWS = 10
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
const liveReviewAvailable = computed(() => {
  const status = room.value?.status || ''
  return status === 'offline' || status === 'stopped'
})
const sessionDecisionBusy = ref(false)
const pendingStartAfterSessionDecision = ref(false)
const loading = ref(true)
const error = useFeedbackErrorRef()
const monitorToggleBusy = ref(false)
const streamState = ref<'connecting' | 'online' | 'offline'>('connecting')
const activeType = ref('all')
const publicScreenMode = ref<'events' | 'bucket' | 'execution' | 'preferences'>('events')
let eventSource: EventSource | null = null
let streamReconnectTimer: number | undefined
let eventFallbackPollTimer: number | undefined
let eventFallbackBusy = false
let pageUnmounted = false
type LocalAudioTask = {
  speech_task_id: string
  room_id: number
  session_id: string
  kind: string
  label: string
  audio_url: string
  duration_ms: number
  start_ms?: number
  created_at?: string
}
type LocalAudioControl = {
  room_id: number
  action: 'pause' | 'stop' | string
  speech_task_id?: string
  program_id?: string
  position_ms?: number
  occurred_at?: string
}
type RoomAudioSpeechFeedItem = {
  sequence: number
  segment_id: string
  text: string
  tone: 'normal' | 'cut' | 'interrupt' | 'resume'
  pts_ms: number
}
type RoomAudioSpeechFeedSnapshot = {
  sequence: number
  previous?: RoomAudioSpeechFeedItem
  current?: RoomAudioSpeechFeedItem
  next?: RoomAudioSpeechFeedItem
  updated_at?: string
}
type RoomAudioEngineSnapshot = {
  room_id: number
  phase: 'idle' | 'mainline' | 'preparing_interrupt' | 'armed' | 'interrupt' | 'preparing_resume' | 'resume' | 'paused' | 'error'
  active_source?: 'mainline' | 'interrupt' | ''
  sequence: number
  output_pts_ms: number
  mainline_cursor_ms: number
  current_segment_id?: string
  planned_cut_segment_id?: string
  resume_segment_id?: string
  paused_from?: string
  subscribers: number
  speech_feed?: RoomAudioSpeechFeedSnapshot
  updated_at?: string
}
let localAudioEventSource: EventSource | null = null
let localAudioContext: AudioContext | null = null
let localAudioGainNode: GainNode | null = null
let localAudioPlayer: HTMLAudioElement | null = null
let localAudioTask: LocalAudioTask | null = null
let localAudioProgressTimer: number | undefined
let localAudioPreparedSwitchTimer: number | undefined
let localAudioHeartbeatTimer: number | undefined
let localAudioReconnectTimer: number | undefined
let localAudioRegisteredRoomID = 0
let localAudioPlaybackGeneration = 0
let compositeAudioAbort: AbortController | null = null
let compositeAudioGeneration = 0
let compositeAudioNextStartTime = 0
let compositeAudioConnectPromise: Promise<void> | null = null
let compositeAudioConnectedAt = 0
let compositeAudioLastFrameAt = 0
const compositeAudioSources = new Set<AudioBufferSourceNode>()
const localAudioState = ref<'disconnected' | 'connected' | 'playing' | 'error'>('disconnected')
const localAudioError = ref('')
const localAudioMuted = ref(false)
const roomAudioEngine = ref<RoomAudioEngineSnapshot | null>(null)
const LOCAL_AUDIO_RECEIVER_KEY = 'livecompanion.web-audio-receiver.v1'
let runtimePollTimer: number | undefined
let sessionStatsPollTimer: number | undefined
let speechPollTimer: number | undefined
let roomAudioEnginePollTimer: number | undefined
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
const captureSnapshot = ref<RoomCaptureSnapshot | null>(null)
const captureBusy = ref(false)
const captureError = ref('')
const speechAnalysisStatus = ref<RoomSpeechAnalysisStatus | null>(null)
const speechAnalysisBusy = ref(false)
const speechAnalysisError = ref('')
const speechAnalysisReportOpen = ref(false)
const speechAnalysisReportLoading = ref(false)
const speechAnalysisReportText = ref('')
const speechAnalysisUploadFile = ref<File | null>(null)
const speechAnalysisUploadBusy = ref(false)
const speechAnalysisUploadLoaded = ref(0)
const speechAnalysisUploadTotal = ref(0)
const speechAnalysisUploadPercent = ref(0)
const speechAnalysisUploadStartedAt = ref(0)
let speechAnalysisUploadAbort: AbortController | null = null
const sharedVideoEl = ref<HTMLVideoElement | null>(null)
const sharedVideoCanvasEl = ref<HTMLCanvasElement | null>(null)
const sharedVideoFloatEl = ref<HTMLElement | null>(null)
const sharedVideoStream = ref<MediaStream | null>(null)
const videoShareBusy = ref(false)
const videoShareError = ref('')
const videoFloatCollapsed = ref(false)
const videoCropPanelOpen = ref(false)
const videoCropLayout = ref<'portrait' | 'landscape'>('portrait')
const videoFloatX = ref(0)
const videoFloatY = ref(118)
const videoCrop = ref({ top: 0, right: 0, bottom: 0, left: 0 })
let videoDrawFrame: number | undefined
let videoFloatDragState: {
  pointerId: number
  startX: number
  startY: number
  originX: number
  originY: number
} | null = null
const liveAgentPlans = ref<LiveAgentPlan[]>([])
const agentPlanBusy = ref(false)
const agentPlanError = ref('')
const roomBrain = ref<RoomBrainView | null>(null)
const speechRuntime = ref<SpeechRuntimeSnapshot | null>(null)
const speechMissions = ref<SpeechMission[]>([])
const speechMissionOpen = ref(false)
const selectedSpeechMissionId = ref('')
const latestSpeechMission = computed(() =>
  speechMissions.value.find((item) => item.id === selectedSpeechMissionId.value) || speechMissions.value[0] || null,
)
const speechHistoryOpen = ref(false)
const speechHistoryLoading = ref(false)
const speechHistoryError = ref('')
const speechHistoryQuery = ref('')
const speechHistoryPageSize = ref(15)
const generatedSpeechHistory = ref<GeneratedSpeechHistoryPage>({
  items: [],
  page: 1,
  page_size: speechHistoryPageSize.value,
  total: 0,
  runtime_session_id: 0,
})
let speechHistorySearchTimer: number | undefined
const speechHistoryTotalPages = computed(() =>
  Math.max(1, Math.ceil(generatedSpeechHistory.value.total / Math.max(1, generatedSpeechHistory.value.page_size))),
)
const speechHistoryPageNumbers = computed(() => {
  const total = speechHistoryTotalPages.value
  const current = generatedSpeechHistory.value.page
  const start = Math.max(1, Math.min(current - 2, total - 4))
  const end = Math.min(total, start + 4)
  const result: number[] = []
  for (let page = start; page <= end; page += 1) result.push(page)
  return result
})
const agentDecisionState = ref<AgentDecisionSnapshot | null>(null)
const questionDecisionBusy = ref<Record<number, EventDecisionAction | undefined>>({})
const bucketDecisionBusy = ref<Record<string, EventDecisionAction | undefined>>({})
const agentDecisionActionMessage = ref('')
const agentDecisionLastSuppressed = ref(false)
const answerReferencePicking = ref(false)
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
const blockedDrawerHandleEl = ref<HTMLElement | null>(null)
const blockedDrawerHandleTop = ref<number | null>(null)
const blockedBusyKey = ref('')
const moderationError = ref('')
const hoveredEventId = ref<number | null>(null)
const eventContextMenu = ref<{ x: number; y: number; event: RoomEvent } | null>(null)
const agentDecisionContextMenu = ref<{ x: number; y: number; item: AgentDecisionItem } | null>(null)
const agentDecisionRemoveBusy = ref('')
const streamPaused = computed(() => hoveredEventId.value !== null || eventContextMenu.value !== null)
const blockedDrawerHandleStyle = computed(() => (
  blockedDrawerHandleTop.value === null
    ? undefined
    : { top: blockedDrawerHandleTop.value + 'px' }
))
let blockedDrawerLayoutFrame: number | undefined

function rightEdgeHandleVisible(element: HTMLElement) {
  const style = window.getComputedStyle(element)
  if (style.display === 'none' || style.visibility === 'hidden') return false
  const rect = element.getBoundingClientRect()
  return rect.width > 0 && rect.height > 0
}

function layoutBlockedDrawerHandle() {
  blockedDrawerLayoutFrame = undefined
  const handle = blockedDrawerHandleEl.value
  if (!handle) return

  const ownRect = handle.getBoundingClientRect()
  const height = ownRect.height
  if (height <= 0) return

  const viewportHeight = Math.max(1, window.innerHeight)
  const gap = 12
  const edgePadding = 14
  const preferredCenter = viewportHeight * 0.54
  const minCenter = edgePadding + height / 2
  const maxCenter = Math.max(minCenter, viewportHeight - edgePadding - height / 2)
  const clampCenter = (value: number) => Math.min(maxCenter, Math.max(minCenter, value))

  const obstacles = Array.from(document.querySelectorAll<HTMLElement>('[data-edge-handle]'))
    .filter((element) => element !== handle && rightEdgeHandleVisible(element))
    .map((element) => element.getBoundingClientRect())
    .filter((rect) => ownRect.left < rect.right + gap && ownRect.right > rect.left - gap)

  const candidates = [clampCenter(preferredCenter)]
  for (const rect of obstacles) {
    candidates.push(clampCenter(rect.top - gap - height / 2))
    candidates.push(clampCenter(rect.bottom + gap + height / 2))
  }

  const isFree = (center: number) => {
    const top = center - height / 2
    const bottom = center + height / 2
    return obstacles.every((rect) => bottom + gap <= rect.top || top - gap >= rect.bottom)
  }

  const freeCandidates = candidates
    .filter(isFree)
    .sort((left, right) => Math.abs(left - preferredCenter) - Math.abs(right - preferredCenter))

  blockedDrawerHandleTop.value = freeCandidates[0] ?? clampCenter(preferredCenter)
}

function scheduleBlockedDrawerHandleLayout() {
  if (blockedDrawerLayoutFrame !== undefined) {
    window.cancelAnimationFrame(blockedDrawerLayoutFrame)
  }
  blockedDrawerLayoutFrame = window.requestAnimationFrame(layoutBlockedDrawerHandle)
}

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
const interruptSpeechDisplay = computed(() => {
  const text = String(interruptSpeech.value.reply_text || interruptSpeech.value.text || '').trim()
  const bridge = interruptSpeech.value.bridge_used
    ? String(interruptSpeech.value.bridge_text || '').trim()
    : ''
  if (!text || !bridge || !text.endsWith(bridge)) {
    return { body: text, bridge: '' }
  }
  return {
    body: text.slice(0, text.length - bridge.length).trimEnd(),
    bridge,
  }
})
const mainlineProgram = computed(() => speechRuntime.value?.program || null)
const mainlineProgramSegment = computed(() => {
  const program = mainlineProgram.value
  const engineSegmentID = String(roomAudioEngine.value?.current_segment_id || '').trim()
  if (program?.timeline?.length && engineSegmentID) {
    const engineSegment = program.timeline.find((segment) => segment.segment_id === engineSegmentID)
    if (engineSegment) return engineSegment
  }
  if (program?.current_segment) return program.current_segment
  const timeline = program?.timeline || []
  if (!timeline.length) return null
  const currentMS = Math.max(0, Number(program?.current_ms || 0))
  const matched = timeline.find((segment) => currentMS >= Number(segment.start_ms || 0) && currentMS < Number(segment.end_ms || 0))
  if (matched) return matched
  if (currentMS >= Number(timeline[timeline.length - 1]?.end_ms || 0)) return timeline[timeline.length - 1]
  return timeline[0]
})
const speechTrackFocus = ref<'auto' | 'mainline' | 'interrupt'>('auto')
function toggleSpeechTrackFocus(track: 'mainline' | 'interrupt') {
  speechTrackFocus.value = speechTrackFocus.value === track ? 'auto' : track
}
const mainlineProgramText = computed(() =>
  mainlineProgramSegment.value?.text ||
  mainlineProgram.value?.track_text ||
  mainlineSpeech.value.text ||
  '',
)
const mainlineProgramDurationMS = computed(() => {
  const taskDuration = Number(mainlineProgram.value?.task?.duration_ms || 0)
  if (taskDuration > 0) return taskDuration
  const timeline = mainlineProgram.value?.timeline || []
  return Number(timeline[timeline.length - 1]?.end_ms || 0)
})
const mainlineFallbackPreview = computed(() => {
  const text = String(mainlineProgramText.value || '').trim()
  if (!text) return ''
  const chars = Array.from(text)
  const limit = speechTrackFocus.value === 'mainline' ? 82 : 60
  if (chars.length <= limit) return text
  const durationMS = Math.max(1, mainlineProgramDurationMS.value)
  const currentMS = Math.max(0, Number(mainlineProgram.value?.current_ms || 0))
  const ratio = Math.min(1, Math.max(0, currentMS / durationMS))
  const center = Math.min(chars.length - 1, Math.max(0, Math.floor(ratio * chars.length)))
  const before = speechTrackFocus.value === 'mainline' ? 32 : 24
  const after = limit - before
  const start = Math.max(0, center - before)
  const end = Math.min(chars.length, center + after)
  return (start > 0 ? '…' : '') + chars.slice(start, end).join('') + (end < chars.length ? '…' : '')
})
const mainlineTransitionCutMS = computed(() => {
  const engineCutID = String(roomAudioEngine.value?.planned_cut_segment_id || '').trim()
  if (engineCutID) {
    const matched = mainlineProgram.value?.timeline?.find((segment) => segment.segment_id === engineCutID)
    if (matched) return Number(matched.end_ms || 0)
  }
  const scheduled = Number(interruptSpeech.value.switch_at_ms || 0)
  if (scheduled > 0) return scheduled
  return Number(mainlineProgram.value?.resume_offset_ms || 0)
})
const mainlineCaptionRows = computed(() => {
  const timeline = mainlineProgram.value?.timeline || []
  const current = mainlineProgramSegment.value
  if (!timeline.length || !current) return []
  const currentIndex = timeline.findIndex((item) => item.segment_id === current.segment_id)
  if (currentIndex < 0) return []
  const engineStopID = String(roomAudioEngine.value?.planned_cut_segment_id || '').trim()
  const engineResumeID = String(roomAudioEngine.value?.resume_segment_id || '').trim()
  const cutMS = mainlineTransitionCutMS.value
  const stopIndex = engineStopID
    ? timeline.findIndex((item) => item.segment_id === engineStopID)
    : cutMS > 0
      ? timeline.findIndex((item) => Number(item.end_ms || 0) === cutMS)
      : -1
  const resumeIndex = engineResumeID
    ? timeline.findIndex((item) => item.segment_id === engineResumeID)
    : stopIndex >= 0 && stopIndex + 1 < timeline.length
      ? stopIndex + 1
      : -1
  return [currentIndex - 1, currentIndex, currentIndex + 1]
    .filter((index) => index >= 0 && index < timeline.length)
    .map((index) => ({
      segment: timeline[index],
      role: index < currentIndex ? 'previous' : index > currentIndex ? 'next' : 'current',
      transition: index === stopIndex ? 'stop' : index === resumeIndex ? 'resume' : '',
    }))
})

const publicSpeechCaptionRows = computed(() => {
  const feed = roomAudioEngine.value?.speech_feed
  const current = feed?.current
  if (current) {
    const toneToTransition = (tone: RoomAudioSpeechFeedItem['tone']) => {
      if (tone === 'cut') return 'stop'
      if (tone === 'interrupt') return 'interrupt'
      if (tone === 'resume') return 'resume'
      return ''
    }
    return [
      feed?.previous ? { item: feed.previous, role: 'previous' as const } : null,
      { item: current, role: 'current' as const },
      feed?.next ? { item: feed.next, role: 'next' as const } : null,
    ]
      .filter((row): row is { item: RoomAudioSpeechFeedItem; role: 'previous' | 'current' | 'next' } => Boolean(row))
      .map(({ item, role }) => ({
        key: 'feed-' + item.segment_id + '-' + item.tone,
        text: item.text,
        role,
        transition: toneToTransition(item.tone),
      }))
  }

  return mainlineCaptionRows.value.map((row) => ({
    key: 'fallback-' + row.segment.segment_id,
    text: row.segment.text,
    role: row.role,
    transition: row.transition,
  }))
})
const speechWaveBars = [
  7, 11, 16, 22, 13, 9, 17, 27, 18, 11, 8, 14, 24, 31, 20, 12,
  9, 15, 26, 34, 23, 14, 10, 17, 29, 21, 13, 8, 12, 20, 15, 9,
]
const mainlineSafeCutRemainingMS = computed(() => {
  const program = mainlineProgram.value
  if (!program?.running || program.suspended) return 0
  const scheduled = Number(interruptSpeech.value.switch_at_ms || 0)
  const next = scheduled > 0 ? scheduled : Number(program.next_safe_cut_ms || 0)
  if (!next) return 0
  return Math.max(0, next - Number(program.current_ms || 0))
})
const mainlineSafeCutImminent = computed(() => {
  const interruptStatus = String(interruptSpeech.value.status || 'idle').toLowerCase()
  return interruptStatus === 'ready' &&
    mainlineSafeCutRemainingMS.value > 0 &&
    mainlineSafeCutRemainingMS.value <= 5000
})
const effectiveMainlineStatus = computed(() => {
  const phase = roomAudioEngine.value?.phase
  if (phase === 'paused') return 'paused'
  if (phase === 'interrupt' || phase === 'preparing_resume') return 'paused'
  if (phase === 'mainline' || phase === 'preparing_interrupt' || phase === 'armed' || phase === 'resume') {
    return 'playing'
  }
  const program = mainlineProgram.value
  if (program?.running) {
    return program.suspended ? 'paused' : 'playing'
  }
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
  if (speechTrackFocus.value === 'mainline') return 'mainline'
  if (speechTrackFocus.value === 'interrupt') return 'interrupt'
  const interruptStatus = (interruptSpeech.value.status || 'idle').toLowerCase()
  if (interruptStatus === 'playing') return 'interrupt'
  if (displayMainlineStatus.value === 'playing') return 'mainline'
  return 'balanced'
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

function generatedSpeechSourceLabel(sourceType: string) {
  if (sourceType === 'interrupt_quick') return '临时插播'
  if (sourceType === 'interrupt_answer') return '场控答疑'
  return sourceType || '场控答疑'
}

function generatedSpeechCorrectionLabel(item: GeneratedSpeechHistoryItem) {
  if (item.correction_status === 'adopted') return '已采用修正'
  if (item.correction_status === 'editing') return '纠正中'
  if (item.correction_count > 0) return '已纠正'
  return '未纠正'
}

function openGeneratedSpeechCorrection(input: {
  decisionId?: string
  question?: string
  reply: string
  sourceLabel: string
  time?: string
}) {
  const reply = input.reply.trim()
  if (!reply) return
  const decisionId = (input.decisionId || '').trim()
  window.dispatchEvent(new CustomEvent('live-answer-reference-open', {
    detail: {
      kind: 'speech',
      question: input.question?.trim() || '这次生成的话术',
      topic: input.sourceLabel,
      time: input.time || '当前',
      originalReply: reply,
      sourceRef: decisionId ? 'generated_speech:' + decisionId : '',
      sourceType: 'generated_speech_correction',
    },
  }))
}

function correctCurrentGeneratedSpeech() {
  const reply = String(interruptSpeech.value.reply_text || interruptSpeech.value.text || '').trim()
  if (!reply) return
  openGeneratedSpeechCorrection({
    decisionId: interruptSpeech.value.decision_id,
    question: interruptSpeech.value.question_text,
    reply,
    sourceLabel: interruptSpeech.value.question_text ? '场控答疑' : '临时插播',
    time: interruptSpeech.value.updated_at ? formatTime(interruptSpeech.value.updated_at) : '当前',
  })
}

function correctGeneratedSpeechHistoryItem(item: GeneratedSpeechHistoryItem) {
  openGeneratedSpeechCorrection({
    decisionId: item.decision_id,
    question: item.question_text,
    reply: item.generated_text,
    sourceLabel: generatedSpeechSourceLabel(item.source_type),
    time: formatTime(item.created_at),
  })
}

async function loadGeneratedSpeechHistory(page = generatedSpeechHistory.value.page || 1) {
  speechHistoryLoading.value = true
  speechHistoryError.value = ''
  try {
    generatedSpeechHistory.value = await getRoomGeneratedSpeechHistory(roomId, {
      query: speechHistoryQuery.value,
      page,
      pageSize: speechHistoryPageSize.value,
      sessionId: runtimeSnapshot.value?.session?.id,
    })
  } catch (err) {
    speechHistoryError.value = err instanceof Error ? err.message : '读取回答历史失败'
  } finally {
    speechHistoryLoading.value = false
  }
}

function openGeneratedSpeechHistory() {
  speechHistoryOpen.value = true
  generatedSpeechHistory.value.page = 1
  void loadGeneratedSpeechHistory(1)
}

function closeGeneratedSpeechHistory() {
  speechHistoryOpen.value = false
}

function scheduleGeneratedSpeechHistorySearch() {
  if (speechHistorySearchTimer !== undefined) window.clearTimeout(speechHistorySearchTimer)
  speechHistorySearchTimer = window.setTimeout(() => {
    speechHistorySearchTimer = undefined
    void loadGeneratedSpeechHistory(1)
  }, 280)
}

function goGeneratedSpeechHistoryPage(page: number) {
  if (speechHistoryLoading.value) return
  const nextPage = Math.min(speechHistoryTotalPages.value, Math.max(1, page))
  if (nextPage === generatedSpeechHistory.value.page) return
  void loadGeneratedSpeechHistory(nextPage)
}

function changeGeneratedSpeechHistoryPageSize() {
  generatedSpeechHistory.value.page = 1
  void loadGeneratedSpeechHistory(1)
}

const aiRuntimeStatus = computed(() => runtimeSnapshot.value?.agent_state || 'stopped')
const aiRuntimeMode = computed<'control' | 'anchor'>(() => runtimeSnapshot.value?.agent_mode || 'control')
const aiRunning = computed(() => aiRuntimeStatus.value === 'working')
const aiPaused = computed(() => aiRuntimeStatus.value === 'paused')
const aiActive = computed(() => aiRuntimeStatus.value === 'working' || aiRuntimeStatus.value === 'paused')
const boundDevice = computed(() => runtimeSnapshot.value?.device || null)
type DeviceVisualState = 'working' | 'paused' | 'offline'
const deviceControlBusy = ref(false)
const deviceControlError = ref('')

async function loadLiveAgentPlansForRoom() {
  const currentRoom = room.value
  if (!currentRoom) return
  agentPlanError.value = ''
  try {
    const result = await getRoomLiveAgentPlans(roomId)
    liveAgentPlans.value = (result.items || []).filter((item) => item.status !== 'archived')
  } catch (err) {
    liveAgentPlans.value = []
    agentPlanError.value = err instanceof Error ? err.message : '读取当前直播间已绑定方案失败'
  }
}

const selectedLiveAgentPlanId = computed(() => runtimeSnapshot.value?.agent_plan_id || 0)
const selectedLiveAgentPlanName = computed(() => runtimeSnapshot.value?.agent_plan_name || '')
const liveAgentPlanSelectValue = computed(() => {
  const selectedId = selectedLiveAgentPlanId.value
  if (!selectedId) return ''
  return liveAgentPlans.value.some((item) => item.id === selectedId) ? String(selectedId) : ''
})

async function createDefaultLiveAgentPlan() {
  const currentRoom = room.value
  if (!currentRoom) throw new Error('当前直播间不存在')
  const prefix = '智能体+' + currentRoom.name + '+方案'
  const allPlans = await getLiveAgentPlans(currentRoom.tenant_id)
  const maxNo = (allPlans.items || []).reduce((max, item) => {
    if (!item.name.startsWith(prefix)) return max
    const parsed = Number(item.name.slice(prefix.length))
    return Number.isFinite(parsed) && parsed > max ? parsed : max
  }, 0)
  const plan = await createLiveAgentPlan({
    name: prefix + String(maxNo + 1),
    description: '系统自动创建的基础智能体直播方案，可在直播策略中继续修改。',
    tenant_id: currentRoom.tenant_id,
  })
  await bindRoomLiveAgentPlan(plan.id, roomId, currentRoom.tenant_id)
  await setLiveRuntimePlan(roomId, plan.id)
  await Promise.all([refreshRuntime(), loadLiveAgentPlansForRoom()])
  return plan
}

async function changeLiveAgentPlan(event: Event) {
  if (!room.value || !coreActionsAvailable.value || agentPlanBusy.value) return
  const target = event.target as HTMLSelectElement
  const rawValue = target.value
  const previous = liveAgentPlanSelectValue.value
  agentPlanBusy.value = true
  agentPlanError.value = ''
  try {
    if (rawValue === '__create_default__') {
      const plan = await createDefaultLiveAgentPlan()
      logUserAction('LIVE_AGENT_PLAN_CREATED', {
        plan_id: plan.id,
        plan_name: plan.name,
      })
      return
    }
    const planId = Number(rawValue)
    if (!Number.isFinite(planId) || planId <= 0 || planId === selectedLiveAgentPlanId.value) return
    await setLiveRuntimePlan(roomId, planId)
    const plan = liveAgentPlans.value.find((item) => item.id === planId) || null
    await refreshRuntime()
    logUserAction('LIVE_AGENT_PLAN_SELECTED', {
      plan_id: planId,
      plan_name: plan?.name || '',
    })
  } catch (err) {
    target.value = previous
    agentPlanError.value = err instanceof Error ? err.message : '切换智能体直播方案失败'
  } finally {
    agentPlanBusy.value = false
  }
}

async function setCompanionMode(mode: 'control' | 'anchor') {
  if (!coreActionsAvailable.value || runtimeModeBusy.value || aiRuntimeMode.value === mode) return
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

function emitAnswerReferenceSelection(detail: {
  kind: 'event' | 'question' | 'bucket'
  question: string
  topic?: string
  eventId?: number
  userId?: string
  nickname?: string
  time?: string
  aggregateCount?: number
  uniqueUsers?: number
  similarQuestions?: string[]
}) {
  if (!detail.question.trim()) return
  answerReferencePicking.value = false
  window.dispatchEvent(new CustomEvent('live-answer-reference-selected', { detail }))
}

function sendQuestionToAnswerReference(
  question: RoomBrainQuestion,
  _bucket?: RoomBrainTopic,
) {
  emitAnswerReferenceSelection({
    kind: 'question',
    question: question.Content,
    eventId: question.EventID,
    userId: question.UserID || '',
    nickname: question.Nickname || question.UserID || '',
    time: formatTime(question.OccurredAt),
    aggregateCount: 1,
    uniqueUsers: 1,
    similarQuestions: [],
  })
}

function sendBucketToAnswerReference(bucket: RoomBrainTopic) {
  const sampleQuestions = Array.from(
    new Set([
      ...(bucket.Questions || []).map((item) => item.Content),
      ...(bucket.SampleQuestions || []),
    ].filter(Boolean)),
  ).slice(0, 8)
  emitAnswerReferenceSelection({
    kind: 'bucket',
    question: bucket.Topic || sampleQuestions[0] || '问题桶',
    topic: bucket.Topic,
    time: formatTime(bucket.LastSeenAt),
    aggregateCount: bucket.Count,
    uniqueUsers: bucket.UniqueUsers || bucket.Count,
    similarQuestions: sampleQuestions,
  })
}

function handleQuestionBucketClick(bucket: RoomBrainTopic) {
  if (answerReferencePicking.value) {
    sendBucketToAnswerReference(bucket)
    return
  }
  toggleQuestionBucket(bucket)
}

function handleQuestionDetailClick(question: RoomBrainQuestion, bucket: RoomBrainTopic) {
  if (answerReferencePicking.value) {
    sendQuestionToAnswerReference(question, bucket)
    return
  }
  selectQuestionDetail(question)
}

function sendPublicScreenEventToAnswerReference(event: RoomEvent) {
  if (!isDirectAnswerEvent(event)) return
  emitAnswerReferenceSelection({
    kind: 'event',
    question: event.content || '',
    eventId: event.id,
    userId: event.user_id || '',
    nickname: event.nickname || event.user_id || '',
    time: formatTime(event.occurred_at),
    aggregateCount: 1,
    uniqueUsers: 1,
    similarQuestions: [],
  })
}

function handlePublicScreenAnswerReferenceClick(event: RoomEvent) {
  if (!answerReferencePicking.value) return
  sendPublicScreenEventToAnswerReference(event)
}

function handleAnswerReferenceMode(event: Event) {
  answerReferencePicking.value = Boolean(
    (event as CustomEvent<{ active?: boolean }>).detail?.active,
  )
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
  return Boolean(bucket.Questions?.some((item) => item.EventID === question.EventID))
}

function questionBucketTTSEligible(bucket: RoomBrainTopic) {
  return Boolean((bucket.Questions?.length || 0) > 0)
}

const selectedQuestionTTSEligible = computed(() => {
  const question = selectedQuestionDetail.value
  const bucket = selectedQuestionBucket.value
  if (!question || !bucket) return false
  return questionTTSEligible(bucket, question)
})

const agentDecisionQueue = computed(() => agentDecisionState.value?.queue || [])
const agentDecisionNotes = computed(() => (agentDecisionState.value?.notes || []).slice(0, 6))
const interactionExecutionMissionByDecision = computed(() => {
  const result = new Map<string, SpeechMission>()
  for (const mission of speechMissions.value) {
    if (mission.decision_id) result.set(mission.decision_id, mission)
  }
  return result
})
const interactionExecutionPendingTotal = computed(() =>
  agentDecisionQueue.value.filter((item) => item.status !== 'CLAIMED' && Date.parse(item.expires_at) > dashboardNow.value).length,
)
const interactionExecutionQueue = computed(() => {
  const running = agentDecisionQueue.value
    .filter((item) => item.status === 'CLAIMED')
    .sort((a, b) => (Date.parse(a.claimed_at || a.created_at) || 0) - (Date.parse(b.claimed_at || b.created_at) || 0))
    .slice(0, 1)
  const pending = agentDecisionQueue.value
    .filter((item) => item.status !== 'CLAIMED' && Date.parse(item.expires_at) > dashboardNow.value)
    .sort((a, b) => (Date.parse(a.created_at) || 0) - (Date.parse(b.created_at) || 0))
    .slice(0, 10)
  return [...running, ...pending]
})
const interactionExecutionRunningCount = computed(() => interactionExecutionQueue.value.filter((item) => item.status === 'CLAIMED').length)
const interactionExecutionPendingCount = computed(() => interactionExecutionQueue.value.filter((item) => item.status !== 'CLAIMED').length)

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

function interactionExecutionTypeLabel(item: AgentDecisionItem) {
  if (item.manual_action === 'quick') return '人工抢答'
  if (item.manual_action === 'answer' || item.sources?.includes('manual')) return '人工回答'
  const kind = String(item.mission_kind || '').toLowerCase()
  const labels: Record<string, string> = {
    welcome_named: '点名欢迎',
    welcome_batch: '批量欢迎',
    reply_chat: '弹幕互动',
    reply_follow: '回应关注',
    reply_like: '回应点赞',
    reply_gift: '回应礼物',
  }
  if (labels[kind]) return labels[kind]
  const topic = String(item.topic || '').toLowerCase()
  if (topic.includes('question') || topic.includes('问题')) return '问题回答'
  return item.title || '互动任务'
}

function interactionExecutionDetail(item: AgentDecisionItem) {
  return item.summary || item.reply_hint || item.sample_questions?.[0] || item.title || '等待执行'
}

function interactionExecutionCountdown(item: AgentDecisionItem) {
  if (item.status === 'CLAIMED') return interactionExecutionStageLabel(item)
  return '放弃倒计时 ' + agentDecisionExpiryText(item.expires_at)
}

function interactionExecutionStageLabel(item: AgentDecisionItem) {
  if (item.status !== 'CLAIMED') return '准备执行'
  const mission = interactionExecutionMissionByDecision.value.get(item.id)
  const state = String(mission?.state || '').toUpperCase()
  const labels: Record<string, string> = {
    CREATED: '准备执行',
    PLANNING_INTERACTION: '互动决策',
    PLANNING_INTERRUPT: '打断决策',
    PLANNING_RESUME: '回归决策',
    PLANNING_EXPRESSION: '表达决策',
    GENERATING_TEXT: '生成话术',
    VALIDATING_TEXT: '审核话术',
    SYNTHESIZING_TTS: '生成声音',
    WAITING_CUT_POINT: '等待切点',
    DISPATCHED: '正在互动',
    RETURNING_MAINLINE: '正在回归',
    COMPLETED: '完成',
    FAILED: '失败',
  }
  if (labels[state]) return labels[state]
  if (speechRuntime.value?.interrupt?.decision_id === item.id) {
    const status = String(speechRuntime.value.interrupt.status || '').toLowerCase()
    if (status === 'returning') return '正在回归'
    if (status === 'playing' || status === 'ready') return '正在互动'
  }
  return '正在执行'
}

async function removeInteractionExecutionItem(item: AgentDecisionItem) {
  if (item.status === 'CLAIMED' || agentDecisionRemoveBusy.value) return
  agentDecisionRemoveBusy.value = item.id
  agentDecisionActionMessage.value = ''
  try {
    await removeRoomAgentDecision(roomId, item.id)
    agentDecisionActionMessage.value = '已删除这条待执行互动。'
    await refreshAgentDecisions()
  } catch (err) {
    agentDecisionActionMessage.value = err instanceof Error ? err.message : '删除互动任务失败'
    await refreshAgentDecisions()
  } finally {
    agentDecisionRemoveBusy.value = ''
  }
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
  if (!coreActionsAvailable.value || deviceControlBusy.value) return
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
        room.value.monitor_enabled = roomData.monitor_enabled
        room.value.monitor_started_at = roomData.monitor_started_at
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

const localVideoViewerActive = computed(() => Boolean(sharedVideoStream.value))
const sharedVideoFloatStyle = computed<Record<string, string>>(() => ({
  left: Math.round(videoFloatX.value) + 'px',
  top: Math.round(videoFloatY.value) + 'px',
}))
const audioRecordingActive = computed(() => captureSnapshot.value?.mode === 'audio_recording')
const audioRecordingFinalizing = computed(() => captureSnapshot.value?.mode === 'finalizing')
const captureModeLabel = computed(() => {
  switch (captureSnapshot.value?.mode) {
    case 'audio_recording': return '声音录制中'
    case 'finalizing': return '正在合并'
    default: return '空闲'
  }
})
const audioRecordingDuration = computed(() => {
  const recording = captureSnapshot.value?.recording
  if (!recording) return 0
  if (recording.status === 'recording' && recording.started_at) {
    const started = Date.parse(recording.started_at)
    if (Number.isFinite(started)) return Math.max(recording.duration_seconds || 0, Math.floor((dashboardNow.value - started) / 1000))
  }
  return recording.duration_seconds || 0
})

const currentSpeechAnalysisTask = computed(() => {
  const task = speechAnalysisStatus.value?.task
  const recordingID = captureSnapshot.value?.recording?.id
  if (!task) return undefined
  if (String(task.recording_id || '').startsWith('upload-')) return task
  if (!recordingID || task.recording_id !== recordingID) return undefined
  return task
})
const speechAnalysisRunning = computed(() => {
  const status = currentSpeechAnalysisTask.value?.status || ''
  return ['queued', 'uploading', 'transcribing', 'analyzing', 'rendering'].includes(status)
})
const speechAnalysisStatusLabel = computed(() => {
  if (speechAnalysisStatus.value && !speechAnalysisStatus.value.configured) return '未配置'
  if (speechAnalysisUploadBusy.value) return '上传中'
  const status = currentSpeechAnalysisTask.value?.status || ''
  const labels: Record<string, string> = {
    queued: '准备中',
    uploading: '上传中',
    transcribing: '文字处理中',
    analyzing: '分析中',
    rendering: '整理中',
    ready: '已完成',
    failed: '失败',
  }
  if (status) return labels[status] || '处理中'
  if (captureSnapshot.value?.recording?.status !== 'ready') return '待录音/上传'
  return '待分析'
})
const speechAnalysisCanStart = computed(() =>
  Boolean(
    speechAnalysisStatus.value?.configured &&
    captureSnapshot.value?.recording?.status === 'ready' &&
    !speechAnalysisRunning.value &&
    currentSpeechAnalysisTask.value?.status !== 'ready' &&
    !speechAnalysisBusy.value,
  ),
)
const speechAnalysisCanUpload = computed(() => Boolean(
  speechAnalysisStatus.value?.configured &&
  speechAnalysisUploadFile.value &&
  !speechAnalysisRunning.value &&
  !speechAnalysisUploadBusy.value &&
  !speechAnalysisBusy.value,
))
const speechAnalysisUploadSpeed = computed(() => {
  if (!speechAnalysisUploadBusy.value || !speechAnalysisUploadStartedAt.value) return ''
  const seconds = Math.max(0.25, (Date.now() - speechAnalysisUploadStartedAt.value) / 1000)
  return formatCaptureBytes(speechAnalysisUploadLoaded.value / seconds) + '/s'
})
const speechAnalysisSteps = ['上传录音', '分析语音', '转成文本', '分析话术', '优化话术', '整理建议', '完成']
const speechAnalysisStepIndex = computed(() => {
  if (speechAnalysisUploadBusy.value) return 0
  const task = currentSpeechAnalysisTask.value
  if (!task) return -1
  if (task.status === 'ready') return 6
  const stage = String(task.stage || '')
  if (task.status === 'failed') {
    if (stage.includes('整理')) return 5
    if (stage.includes('优化')) return 4
    if (stage.includes('话术')) return 3
    if (stage.includes('文本')) return 2
    if (stage.includes('语音')) return 1
    return 1
  }
  if (task.status === 'rendering') return 5
  if (task.status === 'analyzing') return stage.includes('优化') ? 4 : 3
  if (task.status === 'transcribing') return stage.includes('文本') ? 2 : 1
  if (task.status === 'queued' || task.status === 'uploading') return 1
  return -1
})
const speechAnalysisPublicStage = computed(() => {
  if (speechAnalysisUploadBusy.value) {
    return speechAnalysisUploadPercent.value >= 100 ? '上传完成，正在准备分析' : '正在上传录音'
  }
  const index = speechAnalysisStepIndex.value
  const task = currentSpeechAnalysisTask.value
  if (task?.status === 'failed') {
    const label = index >= 0 ? speechAnalysisSteps[index] : '处理'
    return label + '失败'
  }
  if (index >= 0) {
    return index === 6 ? '分析完成' : '正在' + speechAnalysisSteps[index]
  }
  return '录音完成后可进行智能分析'
})
const speechAnalysisFailureText = computed(() => {
  if (currentSpeechAnalysisTask.value?.status !== 'failed') return ''
  const index = speechAnalysisStepIndex.value
  const label = index >= 0 && index < 6 ? speechAnalysisSteps[index] : '处理'
  return label + '失败，请稍后重试；如持续失败请联系管理员。'
})

function formatCaptureBytes(value?: number) {
  const bytes = Math.max(0, Number(value || 0))
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / 1024 / 1024).toFixed(1) + ' MB'
  return (bytes / 1024 / 1024 / 1024).toFixed(2) + ' GB'
}

async function refreshCaptureStatus() {
  try {
    captureSnapshot.value = await getRoomCapture(roomId)
  } catch (err) {
    captureError.value = err instanceof Error ? err.message : '读取采集状态失败'
  }
}

async function refreshSpeechAnalysisStatus() {
  try {
    speechAnalysisStatus.value = await getRoomSpeechAnalysis(roomId)
    speechAnalysisError.value = ''
  } catch (err) {
    speechAnalysisError.value = err instanceof Error ? err.message : '读取智能话术分析状态失败'
  }
}

async function startSpeechAnalysis() {
  if (!speechAnalysisCanStart.value) return
  speechAnalysisBusy.value = true
  speechAnalysisError.value = ''
  speechAnalysisReportOpen.value = false
  speechAnalysisReportText.value = ''
  try {
    speechAnalysisStatus.value = await startRoomSpeechAnalysis(roomId)
  } catch (err) {
    speechAnalysisError.value = err instanceof Error ? err.message : '启动智能话术分析失败'
  } finally {
    speechAnalysisBusy.value = false
  }
}

function selectSpeechAnalysisUploadFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] || null
  speechAnalysisError.value = ''
  if (!file) {
    speechAnalysisUploadFile.value = null
    return
  }
  const allowed = /\.(wav|mp3|m4a|aac|flac|ogg|webm)$/i
  if (!allowed.test(file.name)) {
    speechAnalysisUploadFile.value = null
    input.value = ''
    speechAnalysisError.value = '仅支持 WAV、MP3、M4A、AAC、FLAC、OGG、WEBM 录音。'
    return
  }
  if (file.size <= 0 || file.size > 2 * 1024 * 1024 * 1024) {
    speechAnalysisUploadFile.value = null
    input.value = ''
    speechAnalysisError.value = '录音文件大小不合法，单个文件最大 2GB。'
    return
  }
  speechAnalysisUploadFile.value = file
}

async function uploadSpeechAnalysisRecording() {
  const file = speechAnalysisUploadFile.value
  if (!file || !speechAnalysisCanUpload.value) return
  speechAnalysisUploadBusy.value = true
  speechAnalysisUploadLoaded.value = 0
  speechAnalysisUploadTotal.value = file.size
  speechAnalysisUploadPercent.value = 0
  speechAnalysisUploadStartedAt.value = Date.now()
  speechAnalysisUploadAbort = new AbortController()
  if (speechAnalysisStatus.value) {
    speechAnalysisStatus.value = {
      configured: speechAnalysisStatus.value.configured,
      configuration_reason: speechAnalysisStatus.value.configuration_reason,
    }
  }
  speechAnalysisError.value = ''
  speechAnalysisReportOpen.value = false
  speechAnalysisReportText.value = ''
  try {
    speechAnalysisStatus.value = await uploadRoomSpeechAnalysis(
      roomId,
      file,
      (loaded, total) => {
        speechAnalysisUploadLoaded.value = loaded
        speechAnalysisUploadTotal.value = total || file.size
        speechAnalysisUploadPercent.value = Math.max(
          0,
          Math.min(100, Math.round((loaded / Math.max(1, total || file.size)) * 100)),
        )
      },
      speechAnalysisUploadAbort.signal,
    )
    speechAnalysisUploadLoaded.value = speechAnalysisUploadTotal.value || file.size
    speechAnalysisUploadPercent.value = 100
    speechAnalysisUploadFile.value = null
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      speechAnalysisError.value = '录音上传已取消。'
    } else {
      speechAnalysisError.value = err instanceof Error ? err.message : '上传录音分析失败'
    }
  } finally {
    speechAnalysisUploadBusy.value = false
    speechAnalysisUploadAbort = null
  }
}

function cancelSpeechAnalysisUpload() {
  speechAnalysisUploadAbort?.abort()
}

async function toggleSpeechAnalysisReport() {
  if (speechAnalysisReportOpen.value) {
    speechAnalysisReportOpen.value = false
    return
  }
  if (!speechAnalysisReportText.value) {
    speechAnalysisReportLoading.value = true
    speechAnalysisError.value = ''
    try {
      speechAnalysisReportText.value = await getRoomSpeechAnalysisReportText(roomId)
    } catch (err) {
      speechAnalysisError.value = err instanceof Error ? err.message : '读取智能话术分析报告失败'
      return
    } finally {
      speechAnalysisReportLoading.value = false
    }
  }
  speechAnalysisReportOpen.value = true
}

async function copySpeechAnalysisReport() {
  if (!speechAnalysisReportText.value) return
  try {
    await navigator.clipboard.writeText(speechAnalysisReportText.value)
  } catch {
    speechAnalysisError.value = '复制失败，请在报告正文中手动选择复制。'
  }
}

function openDouyinWatchPage() {
  const url = douyinLiveRoomUrl.value
  if (!url) {
    videoShareError.value = '当前直播间缺少可用的抖音直播地址。'
    return
  }
  videoShareError.value = ''
  const opened = window.open(url, '_blank')
  if (!opened) {
    videoShareError.value = '浏览器阻止了新窗口，请允许弹出窗口后重试。'
    return
  }
  try { opened.opener = null } catch { /* cross-origin window */ }
}

const LEGACY_VIDEO_CROP_STORAGE_KEY = 'livecompanion.video-crop.v2'

function videoCropStorageKey() {
  return 'livecompanion.video-crop.v3.room-' + String(roomId)
}

function clampVideoCrop(persist = true) {
  const crop = videoCrop.value
  crop.top = Math.max(0, Math.min(45, Number(crop.top || 0)))
  crop.right = Math.max(0, Math.min(45, Number(crop.right || 0)))
  crop.bottom = Math.max(0, Math.min(45, Number(crop.bottom || 0)))
  crop.left = Math.max(0, Math.min(45, Number(crop.left || 0)))
  if (crop.left + crop.right > 80) crop.right = Math.max(0, 80 - crop.left)
  if (crop.top + crop.bottom > 80) crop.bottom = Math.max(0, 80 - crop.top)
  if (persist) {
    try {
      window.localStorage.setItem(videoCropStorageKey(), JSON.stringify({
        crop: { ...crop },
        layout: videoCropLayout.value,
      }))
    } catch {
      // Browser storage can be unavailable in hardened/private sessions.
    }
  }
}

function restoreVideoCrop() {
  try {
    const roomKey = videoCropStorageKey()
    const roomRaw = window.localStorage.getItem(roomKey)
    const legacyRaw = window.localStorage.getItem(LEGACY_VIDEO_CROP_STORAGE_KEY)
    const raw = roomRaw || legacyRaw
    if (!raw) {
      applyDouyinCropPreset(false)
      return
    }
    const parsed = JSON.parse(raw) as {
      crop?: Partial<typeof videoCrop.value>
      layout?: 'portrait' | 'landscape'
      top?: number
      right?: number
      bottom?: number
      left?: number
    }
    const saved = parsed.crop || parsed
    videoCrop.value = {
      top: Number(saved.top || 0),
      right: Number(saved.right || 0),
      bottom: Number(saved.bottom || 0),
      left: Number(saved.left || 0),
    }
    videoCropLayout.value = parsed.layout || (100 - videoCrop.value.left - videoCrop.value.right < 45 ? 'portrait' : 'landscape')
    clampVideoCrop(true)
    if (!roomRaw && legacyRaw) {
      window.localStorage.removeItem(LEGACY_VIDEO_CROP_STORAGE_KEY)
    }
  } catch {
    applyDouyinCropPreset(false)
  }
}

function applyDouyinCropPreset(persist = true) {
  videoCropLayout.value = 'portrait'
  videoCrop.value = { top: 11, right: 45, bottom: 13, left: 28 }
  clampVideoCrop(persist)
}

function applyDouyinLandscapeCropPreset() {
  videoCropLayout.value = 'landscape'
  videoCrop.value = { top: 7, right: 18, bottom: 12, left: 1 }
  clampVideoCrop()
}

function resetVideoCrop() {
  videoCropLayout.value = 'landscape'
  videoCrop.value = { top: 0, right: 0, bottom: 0, left: 0 }
  clampVideoCrop()
}

function stopVideoCanvasDraw() {
  if (videoDrawFrame !== undefined) {
    window.cancelAnimationFrame(videoDrawFrame)
    videoDrawFrame = undefined
  }
}

function drawSharedVideoFrame() {
  const video = sharedVideoEl.value
  const canvas = sharedVideoCanvasEl.value
  if (video && canvas && video.videoWidth > 0 && video.videoHeight > 0) {
    const crop = videoCrop.value
    const sourceX = Math.round(video.videoWidth * crop.left / 100)
    const sourceY = Math.round(video.videoHeight * crop.top / 100)
    const sourceWidth = Math.max(1, Math.round(video.videoWidth * (100 - crop.left - crop.right) / 100))
    const sourceHeight = Math.max(1, Math.round(video.videoHeight * (100 - crop.top - crop.bottom) / 100))
    const rect = canvas.getBoundingClientRect()
    const dpr = Math.max(1, Math.min(2, window.devicePixelRatio || 1))
    const targetWidth = Math.max(2, Math.round(rect.width * dpr))
    const targetHeight = Math.max(2, Math.round(rect.height * dpr))
    if (canvas.width !== targetWidth || canvas.height !== targetHeight) {
      canvas.width = targetWidth
      canvas.height = targetHeight
    }
    const context = canvas.getContext('2d')
    if (context) {
      context.fillStyle = '#05070b'
      context.fillRect(0, 0, targetWidth, targetHeight)
      const scale = Math.max(targetWidth / sourceWidth, targetHeight / sourceHeight)
      const drawWidth = sourceWidth * scale
      const drawHeight = sourceHeight * scale
      const drawX = (targetWidth - drawWidth) / 2
      const drawY = (targetHeight - drawHeight) / 2
      context.drawImage(
        video,
        sourceX,
        sourceY,
        sourceWidth,
        sourceHeight,
        drawX,
        drawY,
        drawWidth,
        drawHeight,
      )
    }
  }
  videoDrawFrame = window.requestAnimationFrame(drawSharedVideoFrame)
}

function startVideoCanvasDraw() {
  stopVideoCanvasDraw()
  videoDrawFrame = window.requestAnimationFrame(drawSharedVideoFrame)
}

function stopBrowserVideoShare() {
  stopVideoCanvasDraw()
  const stream = sharedVideoStream.value
  sharedVideoStream.value = null
  if (stream) {
    for (const track of stream.getTracks()) track.stop()
  }
  if (sharedVideoEl.value) sharedVideoEl.value.srcObject = null
  videoShareBusy.value = false
  videoFloatCollapsed.value = false
  videoCropPanelOpen.value = false
}

async function startBrowserVideoShare() {
  if (videoShareBusy.value || sharedVideoStream.value) return
  if (!navigator.mediaDevices?.getDisplayMedia) {
    videoShareError.value = '当前浏览器不支持 Chrome 标签页共享。正式部署请使用 HTTPS + Chrome/Edge。'
    return
  }
  videoShareBusy.value = true
  videoShareError.value = ''
  try {
    const options: DisplayMediaStreamOptions & Record<string, unknown> = {
      video: { frameRate: { ideal: 20, max: 30 } },
      audio: false,
      selfBrowserSurface: 'exclude',
      surfaceSwitching: 'include',
    }
    const stream = await navigator.mediaDevices.getDisplayMedia(options)
    const videoTrack = stream.getVideoTracks()[0]
    if (!videoTrack) {
      for (const track of stream.getTracks()) track.stop()
      throw new Error('没有获得可用的视频共享轨道')
    }
    sharedVideoStream.value = stream
    restoreVideoCrop()
    await nextTick()
    if (sharedVideoEl.value) {
      sharedVideoEl.value.srcObject = stream
      await sharedVideoEl.value.play().catch(() => undefined)
    }
    const floatWidth = sharedVideoFloatEl.value?.offsetWidth || 420
    videoFloatX.value = Math.max(12, window.innerWidth - floatWidth - 18)
    videoFloatY.value = Math.max(76, Math.min(150, window.innerHeight - 260))
    videoTrack.addEventListener('ended', stopBrowserVideoShare, { once: true })
    startVideoCanvasDraw()
  } catch (err) {
    stopBrowserVideoShare()
    if (err instanceof DOMException && err.name === 'NotAllowedError') {
      videoShareError.value = '已取消共享。需要观看时再次点击“共享抖音标签页”。'
    } else {
      videoShareError.value = err instanceof Error ? err.message : '启动 Chrome 标签页共享失败'
    }
  } finally {
    videoShareBusy.value = false
  }
}

function startVideoFloatDrag(event: PointerEvent) {
  if (!sharedVideoFloatEl.value || event.button !== 0) return
  videoFloatDragState = {
    pointerId: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    originX: videoFloatX.value,
    originY: videoFloatY.value,
  }
  ;(event.currentTarget as HTMLElement)?.setPointerCapture?.(event.pointerId)
}

function moveVideoFloatDrag(event: PointerEvent) {
  const state = videoFloatDragState
  const element = sharedVideoFloatEl.value
  if (!state || !element || event.pointerId !== state.pointerId) return
  const maxX = Math.max(8, window.innerWidth - element.offsetWidth - 8)
  const maxY = Math.max(64, window.innerHeight - element.offsetHeight - 8)
  videoFloatX.value = Math.max(8, Math.min(maxX, state.originX + event.clientX - state.startX))
  videoFloatY.value = Math.max(64, Math.min(maxY, state.originY + event.clientY - state.startY))
}

function finishVideoFloatDrag(event?: PointerEvent) {
  const element = sharedVideoFloatEl.value
  if (!videoFloatDragState || !element) return
  if (event && event.pointerId !== videoFloatDragState.pointerId) return
  const rightX = Math.max(8, window.innerWidth - element.offsetWidth - 12)
  videoFloatX.value = videoFloatX.value + element.offsetWidth / 2 < window.innerWidth / 2 ? 12 : rightX
  videoFloatDragState = null
}

async function startCoreAudioRecording() {
  if (!coreActionsAvailable.value || captureBusy.value) return
  captureBusy.value = true
  captureError.value = ''
  try {
    captureSnapshot.value = await startRoomAudioRecording(roomId)
  } catch (err) {
    captureError.value = err instanceof Error ? err.message : '启动声音录制失败'
  } finally {
    captureBusy.value = false
  }
}

async function stopCoreAudioRecording() {
  if (!coreActionsAvailable.value || captureBusy.value || !audioRecordingActive.value) return
  captureBusy.value = true
  captureError.value = ''
  try {
    captureSnapshot.value = await stopRoomAudioRecording(roomId)
  } catch (err) {
    captureError.value = err instanceof Error ? err.message : '停止并合并录音失败'
    await refreshCaptureStatus()
  } finally {
    captureBusy.value = false
  }
}

function localAudioBaseURL() {
  const configured = String(import.meta.env.VITE_CORE_AUDIO_URL || '').trim().replace(/\/$/, '')
  if (configured) return configured
  const host = window.location.hostname
  if (host === '127.0.0.1' || host === 'localhost') return 'http://127.0.0.1:8081'
  // All non-local deployments expose Core audio behind the same-origin
  // /core-audio reverse proxy. Returning an empty base here makes production
  // silently skip receiver registration and composite PCM subscription while
  // the timeline UI keeps moving, which looks like "waveform but no sound".
  return '/core-audio'
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

async function ensureLocalAudioUnlocked() {
  try {
    const context = await unlockSharedAudioContext()
    if (context) {
      localAudioContext = context
      ensureLocalAudioGainNode()
    }
    localAudioError.value = ''
    return true
  } catch (err) {
    localAudioError.value = err instanceof Error
      ? '浏览器阻止了声音播放：' + err.message
      : '浏览器阻止了声音播放，请再次点击“开始”或“抢答”。'
    return false
  }
}

function ensureLocalAudioGainNode() {
  const context = localAudioContext
  if (!context || context.state === 'closed') {
    localAudioGainNode = null
    return null
  }
  if (!localAudioGainNode || localAudioGainNode.context !== context) {
    try {
      localAudioGainNode?.disconnect()
    } catch {
      // Replacing a stale gain node only.
    }
    localAudioGainNode = context.createGain()
    localAudioGainNode.gain.value = localAudioMuted.value ? 0 : 1
    localAudioGainNode.connect(context.destination)
  }
  return localAudioGainNode
}

function toggleLocalAudioMuted() {
  localAudioMuted.value = !localAudioMuted.value
  const gain = ensureLocalAudioGainNode()
  if (gain) {
    gain.gain.setValueAtTime(localAudioMuted.value ? 0 : 1, gain.context.currentTime)
  }
}

function flushCompositeAudioQueue() {
  compositeAudioNextStartTime = 0
  for (const source of compositeAudioSources) {
    try {
      source.stop()
    } catch {
      // A source that already ended does not need another stop.
    }
    try {
      source.disconnect()
    } catch {
      // Best-effort cleanup.
    }
  }
  compositeAudioSources.clear()
}

function stopCompositeAudioReceiver() {
  compositeAudioGeneration += 1
  compositeAudioAbort?.abort()
  compositeAudioAbort = null
  compositeAudioConnectedAt = 0
  compositeAudioLastFrameAt = 0
  flushCompositeAudioQueue()
}

const COMPOSITE_AUDIO_STALE_MS = 4_000

function compositeAudioStreamLooksAlive() {
  if (!compositeAudioAbort || compositeAudioAbort.signal.aborted) return false
  if (localAudioState.value !== 'connected' && localAudioState.value !== 'playing') return false
  const referenceAt = compositeAudioLastFrameAt || compositeAudioConnectedAt
  if (!referenceAt) return false
  return Date.now() - referenceAt < COMPOSITE_AUDIO_STALE_MS
}

function roomAudioPhaseNeedsPCM(phase?: RoomAudioEngineSnapshot['phase']) {
  return Boolean(phase && phase !== 'idle' && phase !== 'paused' && phase !== 'error')
}

function ensureCompositeAudioSubscriptionHealth(snapshot: RoomAudioEngineSnapshot) {
  if (pageUnmounted || !coreActionsAvailable.value || !roomAudioPhaseNeedsPCM(snapshot.phase)) return
  if (compositeAudioStreamLooksAlive()) return
  if (compositeAudioConnectPromise) return
  localAudioState.value = 'disconnected'
  stopCompositeAudioReceiver()
  scheduleLocalAudioReconnect(0)
}

function compositeAudioBufferWindow() {
  const host = window.location.hostname.toLowerCase()
  const local = host === '127.0.0.1' || host === 'localhost'
  return local
    ? { initialSeconds: 0.06, lowWaterSeconds: 0.035, refillSeconds: 0.06 }
    : { initialSeconds: 0.32, lowWaterSeconds: 0.012, refillSeconds: 0.03 }
}

function scheduleCompositePCMFrame(frame: Uint8Array) {
  const context = localAudioContext
  if (!context || context.state !== 'running' || frame.byteLength !== 960) return
  const buffer = context.createBuffer(1, 480, 24_000)
  const channel = buffer.getChannelData(0)
  const view = new DataView(frame.buffer, frame.byteOffset, frame.byteLength)
  for (let index = 0; index < 480; index += 1) {
    channel[index] = view.getInt16(index * 2, true) / 32768
  }
  const source = context.createBufferSource()
  source.buffer = buffer
  const gain = ensureLocalAudioGainNode()
  source.connect(gain || context.destination)
  const now = context.currentTime
  const bufferWindow = compositeAudioBufferWindow()
  if (compositeAudioNextStartTime <= 0) {
    compositeAudioNextStartTime = now + bufferWindow.initialSeconds
  } else if (compositeAudioNextStartTime < now + bufferWindow.lowWaterSeconds) {
    compositeAudioNextStartTime = now + bufferWindow.refillSeconds
  }
  const startAt = compositeAudioNextStartTime
  compositeAudioNextStartTime += 0.02
  compositeAudioSources.add(source)
  source.onended = () => {
    compositeAudioSources.delete(source)
    try {
      source.disconnect()
    } catch {
      // Source cleanup only.
    }
  }
  source.start(startAt)
  localAudioState.value = 'playing'
  localAudioError.value = ''
}

async function pumpCompositeAudioStream(
  stream: ReadableStream<Uint8Array>,
  generation: number,
  signal: AbortSignal,
) {
  const reader = stream.getReader()
  let pending = new Uint8Array(0)
  try {
    while (!signal.aborted && generation === compositeAudioGeneration) {
      const { done, value } = await reader.read()
      if (done) break
      if (!value?.byteLength) continue
      compositeAudioLastFrameAt = Date.now()
      const merged = new Uint8Array(pending.byteLength + value.byteLength)
      merged.set(pending, 0)
      merged.set(value, pending.byteLength)
      let offset = 0
      while (merged.byteLength - offset >= 960) {
        scheduleCompositePCMFrame(merged.subarray(offset, offset + 960))
        offset += 960
      }
      pending = offset < merged.byteLength ? merged.slice(offset) : new Uint8Array(0)
    }
  } catch (err) {
    if (!signal.aborted && generation === compositeAudioGeneration) {
      localAudioState.value = 'error'
      localAudioError.value = err instanceof Error ? err.message : '房间合成音频流读取失败'
    }
  } finally {
    try {
      reader.releaseLock()
    } catch {
      // Reader may already be released by the browser.
    }
    if (!signal.aborted && generation === compositeAudioGeneration && !pageUnmounted) {
      if (compositeAudioAbort?.signal === signal) {
        compositeAudioAbort = null
      }
      compositeAudioConnectedAt = 0
      compositeAudioLastFrameAt = 0
      localAudioState.value = 'disconnected'
      scheduleLocalAudioReconnect(250)
    }
  }
}

async function refreshRoomAudioEngine() {
  const base = localAudioBaseURL()
  if (!base || !Number.isFinite(roomId) || roomId <= 0 || !coreActionsAvailable.value) return
  try {
    const response = await fetch(base + '/v1/rooms/' + roomId + '/audio-engine', {
      cache: 'no-store',
    })
    if (!response.ok) throw new Error('HTTP ' + response.status)
    const snapshot = await response.json() as RoomAudioEngineSnapshot
    roomAudioEngine.value = snapshot
    ensureCompositeAudioSubscriptionHealth(snapshot)
  } catch {
    // Keep the last good engine state while Core reconnects.
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
  if (localAudioPreparedSwitchTimer !== undefined) {
    window.clearTimeout(localAudioPreparedSwitchTimer)
    localAudioPreparedSwitchTimer = undefined
  }
  if (localAudioProgressTimer !== undefined) {
    window.clearInterval(localAudioProgressTimer)
    localAudioProgressTimer = undefined
  }
  const previousTask = localAudioTask
  const player = localAudioPlayer
  const progressMS = player ? Math.round(player.currentTime * 1000) : 0
  localAudioPlayer = null
  localAudioTask = null
  if (player) {
    player.pause()
    player.removeAttribute('src')
    player.load()
  }
  if (reportInterrupted && previousTask) {
    void reportLocalAudioTask(previousTask, 'FAILED', progressMS, 'interrupted_by_new_task')
  }
}

function prepareLocalAudioSwitch(control: LocalAudioControl) {
  const player = localAudioPlayer
  const task = localAudioTask
  const targetMS = Math.max(0, Number(control.position_ms || 0))
  if (!player || !task || !targetMS || control.speech_task_id !== task.speech_task_id) return
  if (localAudioPreparedSwitchTimer !== undefined) {
    window.clearTimeout(localAudioPreparedSwitchTimer)
    localAudioPreparedSwitchTimer = undefined
  }
  const generation = localAudioPlaybackGeneration
  const tick = () => {
    if (
      generation !== localAudioPlaybackGeneration ||
      localAudioPlayer !== player ||
      localAudioTask?.speech_task_id !== task.speech_task_id
    ) {
      localAudioPreparedSwitchTimer = undefined
      return
    }
    const currentMS = Math.max(0, Math.round(player.currentTime * 1000))
    if (currentMS >= targetMS) {
      player.pause()
      localAudioPreparedSwitchTimer = undefined
      void reportLocalAudioTask(task, 'PROGRESS', currentMS)
      return
    }
    const remainingMS = targetMS - currentMS
    localAudioPreparedSwitchTimer = window.setTimeout(tick, Math.max(8, Math.min(40, remainingMS - 4)))
  }
  tick()
}

async function playLocalAudioTask(task: LocalAudioTask) {
  if (!task?.speech_task_id || !task.audio_url) return
  if (localAudioTask?.speech_task_id === task.speech_task_id && localAudioPlayer) return

  stopLocalAudioPlayback(Boolean(localAudioTask))
  const generation = ++localAudioPlaybackGeneration
  localAudioTask = task
  localAudioError.value = ''

  try {
    await ensureLocalAudioUnlocked()
    if (generation !== localAudioPlaybackGeneration) return

    const audio = new Audio(task.audio_url)
    audio.preload = 'auto'
    audio.autoplay = false
    audio.muted = false
    audio.volume = 1
    const sinkAwareAudio = audio as HTMLAudioElement & { setSinkId?: (sinkId: string) => Promise<void> }
    if (typeof sinkAwareAudio.setSinkId === 'function') {
      await sinkAwareAudio.setSinkId('default').catch(() => undefined)
    }
    if (generation !== localAudioPlaybackGeneration) return

    localAudioPlayer = audio
    let readySent = false
    const reportReady = () => {
      if (readySent || generation !== localAudioPlaybackGeneration) return
      readySent = true
      void reportLocalAudioTask(task, 'READY', Math.round(audio.currentTime * 1000))
    }
    audio.addEventListener('canplay', reportReady)
    audio.addEventListener('canplaythrough', reportReady)
    audio.addEventListener('playing', () => {
      if (generation !== localAudioPlaybackGeneration) return
      localAudioState.value = 'playing'
      localAudioError.value = ''
      void reportLocalAudioTask(task, 'PLAYING', Math.round(audio.currentTime * 1000))
    })
    audio.addEventListener('ended', () => {
      if (generation !== localAudioPlaybackGeneration) return
      if (localAudioProgressTimer !== undefined) {
        window.clearInterval(localAudioProgressTimer)
        localAudioProgressTimer = undefined
      }
      localAudioPlayer = null
      localAudioTask = null
      localAudioState.value = 'connected'
      void reportLocalAudioTask(task, 'COMPLETED', task.duration_ms || Math.round(audio.duration * 1000))
    })
    audio.addEventListener('error', () => {
      if (generation !== localAudioPlaybackGeneration) return
      const mediaError = audio.error
      const message = mediaError
        ? '浏览器音频播放失败（MediaError ' + mediaError.code + '）'
        : '浏览器音频加载或解码失败'
      localAudioState.value = 'error'
      localAudioError.value = message
      localAudioPlayer = null
      localAudioTask = null
      void reportLocalAudioTask(task, 'FAILED', Math.round(audio.currentTime * 1000), message)
    })

    audio.load()
    const requestedStartMS = Math.max(
      0,
      Math.min(Number(task.start_ms || 0), Math.max(0, Number(task.duration_ms || 0) - 1)),
    )
    if (requestedStartMS > 0) {
      if (audio.readyState < 1) {
        await new Promise<void>((resolve, reject) => {
          const onLoadedMetadata = () => {
            cleanup()
            resolve()
          }
          const onLoadError = () => {
            cleanup()
            reject(new Error('本机音频元数据加载失败'))
          }
          const cleanup = () => {
            audio.removeEventListener('loadedmetadata', onLoadedMetadata)
            audio.removeEventListener('error', onLoadError)
          }
          audio.addEventListener('loadedmetadata', onLoadedMetadata)
          audio.addEventListener('error', onLoadError)
        })
      }
      if (generation !== localAudioPlaybackGeneration) return
      const durationSeconds = Number.isFinite(audio.duration) ? audio.duration : 0
      const targetSeconds = requestedStartMS / 1000
      audio.currentTime = durationSeconds > 0
        ? Math.min(targetSeconds, Math.max(0, durationSeconds - 0.02))
        : targetSeconds
    }
    await audio.play()
    if (generation !== localAudioPlaybackGeneration) {
      audio.pause()
      return
    }
    reportReady()
    localAudioProgressTimer = window.setInterval(() => {
      if (generation !== localAudioPlaybackGeneration || localAudioPlayer !== audio) return
      void reportLocalAudioTask(task, 'PROGRESS', Math.round(audio.currentTime * 1000))
    }, 1500)
  } catch (err) {
    if (generation !== localAudioPlaybackGeneration) return
    const message = err instanceof Error ? err.message : '本机播放失败'
    localAudioState.value = 'error'
    localAudioError.value = message.includes('play() failed')
      ? '浏览器阻止了自动播放，请点击页面上的“开始”或“抢答”后再试。'
      : message
    if (localAudioPlayer) {
      localAudioPlayer.pause()
      localAudioPlayer.removeAttribute('src')
      localAudioPlayer.load()
    }
    localAudioPlayer = null
    localAudioTask = null
    await reportLocalAudioTask(task, 'FAILED', 0, localAudioError.value)
  }
}

// Legacy task-player helpers are retained only while the old AudioHub API still
// exists server-side. The current business page does not invoke them.
void prepareLocalAudioSwitch
void playLocalAudioTask

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
        capabilities: ['composite_pcm_s16le_24k_mono', 'room_audio_engine'],
      }),
    })
    if (!response.ok) throw new Error('Core声音注册失败 HTTP ' + response.status)
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

function scheduleLocalAudioReconnect(delayMS = 900) {
  if (pageUnmounted || !coreActionsAvailable.value || localAudioReconnectTimer !== undefined) return
  localAudioReconnectTimer = window.setTimeout(() => {
    localAudioReconnectTimer = undefined
    if (pageUnmounted || !coreActionsAvailable.value) return
    void connectLocalAudioReceiver()
  }, delayMS)
}

async function heartbeatLocalAudioReceiver() {
  if (!coreActionsAvailable.value) return
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
        localAudioRegisteredRoomID = 0
        stopCompositeAudioReceiver()
        await connectLocalAudioReceiver()
      } else {
        scheduleLocalAudioReconnect()
      }
    }
  } catch {
    localAudioState.value = 'disconnected'
    scheduleLocalAudioReconnect()
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

async function connectLocalAudioReceiverOnce() {
  if (pageUnmounted || !coreActionsAvailable.value) return
  if (localAudioReconnectTimer !== undefined) {
    window.clearTimeout(localAudioReconnectTimer)
    localAudioReconnectTimer = undefined
  }
  localAudioEventSource?.close()
  localAudioEventSource = null
  const base = localAudioBaseURL()
  if (!base || !Number.isFinite(roomId) || roomId <= 0) return
  if (!localAudioContext) {
    localAudioContext = getSharedAudioContext(true)
  }
  ensureLocalAudioGainNode()
  if (
    localAudioRegisteredRoomID === roomId &&
    compositeAudioStreamLooksAlive()
  ) {
    return
  }
  if (!(await registerLocalAudioReceiver())) return

  stopCompositeAudioReceiver()
  stopLocalAudioPlayback(false)
  const generation = ++compositeAudioGeneration
  const controller = new AbortController()
  compositeAudioAbort = controller
  compositeAudioConnectedAt = 0
  compositeAudioLastFrameAt = 0
  try {
    const response = await fetch(base + '/v1/rooms/' + roomId + '/composite.pcm', {
      cache: 'no-store',
      signal: controller.signal,
    })
    if (!response.ok || !response.body) {
      throw new Error('房间合成音频流连接失败 HTTP ' + response.status)
    }
    if (generation !== compositeAudioGeneration || controller.signal.aborted) return
    compositeAudioConnectedAt = Date.now()
    localAudioState.value = 'connected'
    localAudioError.value = ''
    void pumpCompositeAudioStream(response.body, generation, controller.signal)
  } catch (err) {
    if (controller.signal.aborted || generation !== compositeAudioGeneration) return
    localAudioState.value = 'error'
    localAudioError.value = err instanceof Error ? err.message : '房间合成音频流连接失败'
    scheduleLocalAudioReconnect()
  }
}

async function connectLocalAudioReceiver() {
  if (compositeAudioConnectPromise) {
    await compositeAudioConnectPromise
    return
  }
  const pending = connectLocalAudioReceiverOnce()
  compositeAudioConnectPromise = pending
  try {
    await pending
  } finally {
    if (compositeAudioConnectPromise === pending) {
      compositeAudioConnectPromise = null
    }
  }
}

async function startCompanionRuntime() {
  if (!coreActionsAvailable.value || runtimeControlBusy.value || aiActive.value) return
  if (!(await ensureLocalAudioUnlocked())) return
  if (aiRuntimeMode.value === 'anchor' && !selectedLiveAgentPlanId.value) {
    runtimeError.value = '主播模式需要先选择智能体直播方案'
    return
  }
  runtimeControlBusy.value = true
  runtimeError.value = ''
  try {
    await refreshSessionStats()
    if (sessionStats.value?.resume_pending) {
      pendingStartAfterSessionDecision.value = true
      return
    }
    await connectLocalAudioReceiver()
    await startLiveRuntime(roomId)
    await Promise.all([refreshRuntime(), refreshAgentDecisions()])
  } catch (err) {
    const message = err instanceof Error ? err.message : '启动直播搭子失败'
    if (message.includes('请先选择续接上一场或作为新直播')) {
      await refreshSessionStats()
      if (sessionStats.value?.resume_pending) {
        pendingStartAfterSessionDecision.value = true
        runtimeError.value = ''
        return
      }
    }
    runtimeError.value = message
  } finally {
    runtimeControlBusy.value = false
  }
}

async function pauseCompanionRuntime() {
  if (!coreActionsAvailable.value || runtimeControlBusy.value || !aiRunning.value) return
  runtimeControlBusy.value = true
  runtimeError.value = ''
  try {
    await pauseLiveRuntime(roomId)
    flushCompositeAudioQueue()
    await Promise.all([refreshRuntime(), refreshSpeechRuntime(), refreshAgentDecisions()])
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '暂停主播模式失败'
  } finally {
    runtimeControlBusy.value = false
  }
}

async function resumeCompanionRuntime() {
  if (!coreActionsAvailable.value || runtimeControlBusy.value || !aiPaused.value) return
  if (!(await ensureLocalAudioUnlocked())) return
  await connectLocalAudioReceiver()
  runtimeControlBusy.value = true
  runtimeError.value = ''
  try {
    await resumeLiveRuntime(roomId)
    await Promise.all([refreshRuntime(), refreshSpeechRuntime(), refreshAgentDecisions()])
  } catch (err) {
    runtimeError.value = err instanceof Error ? err.message : '继续主播模式失败'
  } finally {
    runtimeControlBusy.value = false
  }
}

async function stopCompanionRuntime() {
  if (!coreActionsAvailable.value || runtimeControlBusy.value || !aiActive.value) return
  runtimeControlBusy.value = true
  runtimeError.value = ''
  try {
    await stopLiveRuntime(roomId)
    stopCompositeAudioReceiver()
    if (localAudioReconnectTimer !== undefined) {
      window.clearTimeout(localAudioReconnectTimer)
      localAudioReconnectTimer = undefined
    }
    localAudioState.value = 'disconnected'
    localAudioError.value = ''
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
  const shouldStartAfterDecision = pendingStartAfterSessionDecision.value
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
    if (shouldStartAfterDecision) {
      pendingStartAfterSessionDecision.value = false
      await startCompanionRuntime()
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
    if (!coreActionsAvailable.value) return
    void refreshRuntime()
    void refreshRoomBrain()
    void refreshCaptureStatus()
    void refreshSpeechAnalysisStatus()
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
    if (!coreActionsAvailable.value) return
    void refreshSpeechRuntime()
    if (isInternalViewer.value) void refreshSpeechMissions()
  }, 1000)
}

function startRoomAudioEnginePolling() {
  if (roomAudioEnginePollTimer !== undefined) window.clearInterval(roomAudioEnginePollTimer)
  roomAudioEnginePollTimer = window.setInterval(() => {
    if (!coreActionsAvailable.value) return
    void refreshRoomAudioEngine()
  }, 250)
}

function startAgentDecisionPolling() {
  if (agentDecisionPollTimer !== undefined) window.clearInterval(agentDecisionPollTimer)
  agentDecisionPollTimer = window.setInterval(() => {
    if (!coreActionsAvailable.value) return
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
const canControlMonitoring = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'customer' || bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes('liveops.configure')))
})

const dashboardCollectorState = computed(() => {
  const currentRoom = room.value
  if (!currentRoom?.monitor_enabled) return 'stopped'
  if (streamState.value !== 'online') return 'connecting'
  if (currentRoom.status === 'live') return 'live'
  if (currentRoom.status === 'connecting' || currentRoom.status === 'pending') return 'connecting'
  if (currentRoom.status === 'error') return 'error'
  if (currentRoom.status === 'offline') return 'offline'
  return 'connecting'
})

const dashboardCollectorLabel = computed(() => {
  if (coreRuntime.phase === 'offline') return 'Core异常'
  if (coreRuntime.phase === 'recovering' || coreRuntime.phase === 'checking') return '状态同步中'
  if (monitorToggleBusy.value) return room.value?.monitor_enabled ? '停止中…' : '连接中…'
  if (!room.value?.monitor_enabled) return '连接采集'
  if (streamState.value !== 'online') return '实时流连接中'
  if (room.value.status === 'live') return '直播中'
  if (room.value.status === 'connecting') return '连接中'
  if (room.value.status === 'pending') return '等待连接'
  if (room.value.status === 'error') return '连接异常'
  if (room.value.status === 'offline') return '未开播'
  return '采集中'
})

const dashboardCollectorTitle = computed(() => {
  if (!coreActionsAvailable.value) return 'Core 服务恢复后可操作'
  if (!canControlMonitoring.value) return dashboardCollectorLabel.value
  if (monitorToggleBusy.value) return dashboardCollectorLabel.value
  return room.value?.monitor_enabled ? '点击停止 Core 公屏采集' : '点击连接 Core 公屏采集'
})

async function toggleRoomMonitoring() {
  const currentRoom = room.value
  if (!currentRoom || !canControlMonitoring.value || !coreActionsAvailable.value || monitorToggleBusy.value) return
  const enabled = Boolean(currentRoom.monitor_enabled)
  if (enabled) {
    const confirmed = await confirmAction({
      title: '停止采集',
      message: '确认停止这个直播间的公屏采集？停止后可随时在这里重新连接。',
      confirmText: '停止采集',
      danger: true,
    })
    if (!confirmed) return
  }

  monitorToggleBusy.value = true
  error.value = ''
  try {
    const updated = await setRoomMonitor(currentRoom.id, !enabled)
    room.value = updated
    if (updated.monitor_enabled) startPublicScreenTransport()
    else stopPublicScreenTransport()
  } catch (err) {
    error.value = err instanceof Error
      ? err.message
      : enabled
        ? '停止采集失败'
        : '连接采集失败'
  } finally {
    monitorToggleBusy.value = false
  }
}
function roomTitle() {
  if (!room.value) return '直播间'
  return room.value.name || '直播间 ' + room.value.external_room_id
}

const douyinLiveRoomUrl = computed(() => {
  const currentRoom = room.value
  if (!currentRoom) return ''
  const sourceURL = (currentRoom.source_url || '').trim()
  if (/^https?:\/\/live\.douyin\.com(?:\/|$)/i.test(sourceURL)) return sourceURL
  const externalRoomID = (currentRoom.external_room_id || '').trim()
  return externalRoomID ? 'https://live.douyin.com/' + encodeURIComponent(externalRoomID) : ''
})

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

async function refreshSpeechMissions() {
  if (!isInternalViewer.value) return
  try {
    const result = await getRoomSpeechMissions(roomId)
    updatePreservingPageBottom(() => {
      speechMissions.value = result.missions || []
      if (!selectedSpeechMissionId.value || !speechMissions.value.some((item) => item.id === selectedSpeechMissionId.value)) {
        selectedSpeechMissionId.value = speechMissions.value[0]?.id || ''
      }
    })
  } catch {
    // Keep the last good mission blackboard during a transient service hiccup.
  }
}

function speechMissionStateLabel(state?: string) {
  const labels: Record<string, string> = {
    CREATED: '已创建',
    PLANNING_INTERACTION: '互动决策',
    PLANNING_INTERRUPT: '打断决策',
    PLANNING_RESUME: '回归决策',
    PLANNING_EXPRESSION: '表达决策',
    GENERATING_TEXT: '生成话术',
    VALIDATING_TEXT: '审核话术',
    SYNTHESIZING_TTS: '生成声音',
    WAITING_CUT_POINT: '等待切入',
    DISPATCHED: '已下发',
    RETURNING_MAINLINE: '正在回归',
    COMPLETED: '已完成',
    FAILED: '失败',
  }
  return labels[String(state || '').toUpperCase()] || state || '暂无任务'
}

function missionMS(value?: number) {
  if (!value && value !== 0) return '—'
  return (value / 1000).toFixed(2) + 's'
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
  if (!(await ensureLocalAudioUnlocked())) return
  if (localAudioState.value === 'disconnected' || localAudioState.value === 'error') {
    await connectLocalAudioReceiver()
  }
  if (!aiRunning.value) {
    setEventDecisionRowState(event.id, { error: '请先启动直播搭子。' })
    return
  }
  if (sessionStats.value?.resume_pending) {
    setEventDecisionRowState(event.id, { error: '请先选择续接上一场或作为新直播。' })
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
  if (!questionTTSEligible(bucket, question)) return
  await ensureLocalAudioUnlocked()
  setQuestionDecisionBusy(question.EventID, action)
  agentDecisionActionMessage.value = ''
  agentDecisionLastSuppressed.value = false
  try {
    const result = await enqueueRoomManualAgentDecision(roomId, {
      question: question.Content,
      topic: question.EventID ? 'EVENT:' + question.EventID : '',
      title: '单条问题：' + String(question.Content || '').slice(0, 36),
      summary: action === 'quick'
        ? '人工对这一条观众问题发起抢答，只回答当前这一条，不合并整个问题聚类'
        : '人工对这一条观众问题发起回答，只回答当前这一条，不合并整个问题聚类',
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
  const eligibleQuestions = (bucket.Questions || [])
    .filter((question) => questionTTSEligible(bucket, question))
    .slice(0, 8)
  if (!eligibleQuestions.length) {
    agentDecisionActionMessage.value = '这个问题聚类当前没有可回答的问题。'
    return
  }

  await ensureLocalAudioUnlocked()
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
	if (events.value.some((item) => item.id === event.id)) return
	if (pendingStreamEvents.some((item) => item.id === event.id)) return
	pushFlowSample(event)
	pendingStreamEvents.push(event)
	if (pendingStreamEvents.length > 500) pendingStreamEvents = pendingStreamEvents.slice(-500)
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
  if (coreRuntime.phase === 'offline') {
    const cached = restoreRoomDetailCache()
    if (cached) {
      room.value = cached
      await loadLiveAgentPlansForRoom()
      void loadTenantDirectory()
      loading.value = false
      return
    }
  }
  try {
    const [roomData, eventData] = await Promise.all([
      getRoom(roomId),
      getRoomEvents(roomId, 300),
    ])
    room.value = roomData
    saveRoomDetailCache(roomData)
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
    if (roomData.monitor_enabled) startPublicScreenTransport()
    else stopPublicScreenTransport()
    await refreshRuntime()
    await Promise.all([refreshRoomBrain(), refreshSpeechRuntime(), refreshSpeechMissions(), refreshRoomAudioEngine(), refreshAgentDecisions(), refreshBlockedUsers(), refreshSessionStats(), refreshCaptureStatus(), refreshSpeechAnalysisStatus()])
    if (aiActive.value || roomAudioEngine.value?.phase !== 'idle') {
      if (navigator.userActivation?.hasBeenActive) {
        await ensureLocalAudioUnlocked()
      }
      void connectLocalAudioReceiver()
    }
    startRuntimePolling()
    startSessionStatsPolling()
    startSpeechRuntimePolling()
    startRoomAudioEnginePolling()
    startAgentDecisionPolling()
  } catch (err) {
    const cached = restoreRoomDetailCache()
    if (cached && coreRuntime.phase !== 'online') {
      room.value = cached
      error.value = ''
    } else {
      error.value = err instanceof Error ? err.message : '读取直播间失败'
    }
  } finally {
    loading.value = false
  }
}

function clearStreamReconnectTimer() {
	if (streamReconnectTimer === undefined) return
	window.clearTimeout(streamReconnectTimer)
	streamReconnectTimer = undefined
}

function scheduleStreamReconnect() {
	if (pageUnmounted || !coreActionsAvailable.value || !room.value?.monitor_enabled || streamReconnectTimer !== undefined) return
	streamReconnectTimer = window.setTimeout(() => {
		streamReconnectTimer = undefined
		if (!pageUnmounted && coreActionsAvailable.value && room.value?.monitor_enabled) connectStream()
	}, 2000)
}

async function pollRecentEventsFallback() {
	if (pageUnmounted || !room.value?.monitor_enabled || eventFallbackBusy) return
	eventFallbackBusy = true
	try {
		const page = await getRoomEvents(roomId, 300)
		const incoming = [...page.items].sort((a, b) => a.id - b.id)
		let newest: RoomEvent | null = null
		for (const event of incoming) {
			const alreadyVisible = events.value.some((item) => item.id === event.id)
			const alreadyPending = pendingStreamEvents.some((item) => item.id === event.id)
			if (alreadyVisible || alreadyPending) continue
			queueStreamEvent(event)
			newest = event
		}
		if (newest && room.value?.monitor_enabled) {
			room.value.status = 'live'
			room.value.last_event_at = newest.occurred_at
		}
	} catch {
		// SSE remains the primary channel; polling is only a recovery path.
	} finally {
		eventFallbackBusy = false
	}
}

function startEventFallbackPolling() {
	if (eventFallbackPollTimer !== undefined || pageUnmounted || !room.value?.monitor_enabled) return
	void pollRecentEventsFallback()
	eventFallbackPollTimer = window.setInterval(() => {
		void pollRecentEventsFallback()
	}, 3000)
}

function stopEventFallbackPolling() {
	if (eventFallbackPollTimer !== undefined) {
		window.clearInterval(eventFallbackPollTimer)
		eventFallbackPollTimer = undefined
	}
	eventFallbackBusy = false
}

function connectStream() {
	if (pageUnmounted || !room.value?.monitor_enabled) return
	clearStreamReconnectTimer()
	const previous = eventSource
	eventSource = null
	previous?.close()
	streamState.value = 'connecting'

	const source = new EventSource('/api/v1/rooms/' + roomId + '/stream', {
		withCredentials: true,
	})
	eventSource = source

	source.onopen = () => {
		if (eventSource !== source) return
		clearStreamReconnectTimer()
		streamState.value = 'online'
	}

	source.onmessage = (message) => {
		if (eventSource !== source) return
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

	source.onerror = () => {
		if (eventSource !== source) return
		streamState.value = 'offline'
		eventSource = null
		source.close()
		scheduleStreamReconnect()
	}
}

function startPublicScreenTransport() {
	if (pageUnmounted || !coreActionsAvailable.value || !room.value?.monitor_enabled) return
	startEventFallbackPolling()
	if (!eventSource || eventSource.readyState === EventSource.CLOSED) connectStream()
}

function stopPublicScreenTransport() {
	clearStreamReconnectTimer()
	stopEventFallbackPolling()
	const source = eventSource
	eventSource = null
	source?.close()
	streamState.value = 'offline'
}

function handleLocalAudioUserGesture() {
  if (pageUnmounted || !coreActionsAvailable.value) return
  if (
    localAudioContext?.state === 'running' &&
    (localAudioState.value === 'connected' || localAudioState.value === 'playing')
  ) {
    return
  }
  void ensureLocalAudioUnlocked().then((ready) => {
    if (
      ready &&
      !pageUnmounted &&
      (aiActive.value || roomAudioEngine.value?.phase !== 'idle') &&
      (localAudioState.value === 'disconnected' || localAudioState.value === 'error')
    ) {
      void connectLocalAudioReceiver()
    }
  })
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
  window.addEventListener('pointermove', moveVideoFloatDrag)
  window.addEventListener('pointerup', finishVideoFloatDrag)
  window.addEventListener('pointercancel', finishVideoFloatDrag)
  dashboardTickTimer = window.setInterval(() => {
    dashboardNow.value = Date.now()
    pruneFlowSamples()
  }, 1000)
  restoreVideoCrop()
  window.addEventListener('click', closeEventContextMenu)
  window.addEventListener('click', closeAgentDecisionContextMenu)
  window.addEventListener('live-answer-reference-mode', handleAnswerReferenceMode)
  window.addEventListener('edge-handle-layout-changed', scheduleBlockedDrawerHandleLayout)
  window.addEventListener('resize', scheduleBlockedDrawerHandleLayout)
  window.addEventListener('pointerdown', handleLocalAudioUserGesture, { passive: true })
  mascotActionTimer = window.setTimeout(runMascotAction, 1200 + Math.random() * 1400)
  void nextTick(scheduleBlockedDrawerHandleLayout)
  load()
})

watch([blockedDrawerOpen, () => blockedUsers.value.length], () => {
  void nextTick(scheduleBlockedDrawerHandleLayout)
})

watch(
  () => coreRuntime.recoverySerial,
  () => {
    if (pageUnmounted) return
    void refreshRuntime()
    void refreshSessionStats()
    void refreshCaptureStatus()
    void refreshSpeechRuntime()
    void refreshRoomAudioEngine()
    void refreshAgentDecisions()
    if (room.value?.monitor_enabled) startPublicScreenTransport()
    scheduleLocalAudioReconnect(0)
  },
)
watch(
  () => coreRuntime.phase,
  (phase) => {
    if (pageUnmounted) return
    if (phase === 'offline') {
      stopPublicScreenTransport()
      localAudioEventSource?.close()
      localAudioEventSource = null
      if (localAudioReconnectTimer !== undefined) {
        window.clearTimeout(localAudioReconnectTimer)
        localAudioReconnectTimer = undefined
      }
      if (localAudioHeartbeatTimer !== undefined) {
        window.clearInterval(localAudioHeartbeatTimer)
        localAudioHeartbeatTimer = undefined
      }
      localAudioState.value = 'disconnected'
      stopLocalAudioPlayback(false)
      stopCompositeAudioReceiver()
      return
    }
    if (phase === 'online') {
      if (room.value?.monitor_enabled) startPublicScreenTransport()
      scheduleLocalAudioReconnect(0)
    }
  },
)
onBeforeUnmount(() => {
	pageUnmounted = true
	stopBrowserVideoShare()
	stopPublicScreenTransport()
	localAudioEventSource?.close()
	localAudioEventSource = null
	if (localAudioReconnectTimer !== undefined) {
		window.clearTimeout(localAudioReconnectTimer)
		localAudioReconnectTimer = undefined
	}
	void unregisterLocalAudioReceiver()
	stopLocalAudioPlayback(false)
	stopCompositeAudioReceiver()
	// Keep the browser AudioContext alive across room navigation. Closing it here
	// forces a brand-new context on re-entry, which may be suspended by autoplay
	// policy even though the room composite stream is already producing PCM.
	localAudioContext = null
	if (runtimePollTimer !== undefined) window.clearInterval(runtimePollTimer)
	if (sessionStatsPollTimer !== undefined) window.clearInterval(sessionStatsPollTimer)
	if (speechPollTimer !== undefined) window.clearInterval(speechPollTimer)
	if (roomAudioEnginePollTimer !== undefined) window.clearInterval(roomAudioEnginePollTimer)
	if (agentDecisionPollTimer !== undefined) window.clearInterval(agentDecisionPollTimer)
	if (streamBatchTimer !== undefined) window.clearTimeout(streamBatchTimer)
	if (dashboardTickTimer !== undefined) window.clearInterval(dashboardTickTimer)
	if (speechHistorySearchTimer !== undefined) window.clearTimeout(speechHistorySearchTimer)
	if (pageBottomRestoreFrame !== undefined) window.cancelAnimationFrame(pageBottomRestoreFrame)
	stopMascotActions()
	pendingStreamEvents = []
	window.removeEventListener('click', closeEventContextMenu)
	window.removeEventListener('click', closeAgentDecisionContextMenu)
	window.removeEventListener('live-answer-reference-mode', handleAnswerReferenceMode)
	window.removeEventListener('edge-handle-layout-changed', scheduleBlockedDrawerHandleLayout)
	window.removeEventListener('resize', scheduleBlockedDrawerHandleLayout)
	window.removeEventListener('pointerdown', handleLocalAudioUserGesture)
	window.removeEventListener('pointermove', movePublicScreenResize)
	window.removeEventListener('pointerup', finishPublicScreenResize)
	window.removeEventListener('pointercancel', finishPublicScreenResize)
	window.removeEventListener('pointermove', moveVideoFloatDrag)
	window.removeEventListener('pointerup', finishVideoFloatDrag)
	window.removeEventListener('pointercancel', finishVideoFloatDrag)
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
	if (blockedDrawerLayoutFrame !== undefined) {
		window.cancelAnimationFrame(blockedDrawerLayoutFrame)
		blockedDrawerLayoutFrame = undefined
	}
})
</script>

<template>
  <div class="room-detail-page">
    <ModulePageNav
      context="live"
      active-title="直播间详情"
      active-nav-title="直播间详情"
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
                : '实时流连接中'
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
              :value="liveAgentPlanSelectValue"
              :disabled="!coreActionsAvailable || agentPlanBusy"
              @change="changeLiveAgentPlan"
            >
              <option value="" disabled>
                {{ selectedLiveAgentPlanName || (liveAgentPlans.length ? '请选择已绑定方案' : '暂无已绑定方案') }}
              </option>
              <option v-for="plan in liveAgentPlans" :key="plan.id" :value="String(plan.id)">
                {{ plan.name }}
              </option>
              <option value="__create_default__">＋ 自动创建默认方案</option>
            </select>
          </label>
          <small v-if="agentPlanError" class="companion-plan-error">{{ agentPlanError }}</small>
          <div class="companion-mode-switch" aria-label="直播搭子工作模式">
            <button
              type="button"
              :class="{ active: aiRuntimeMode === 'control' }"
              :disabled="!coreActionsAvailable || runtimeModeBusy"
              @click="setCompanionMode('control')"
            >中控模式</button>
            <button
              type="button"
              :class="{ active: aiRuntimeMode === 'anchor' }"
              :disabled="!coreActionsAvailable || runtimeModeBusy || !selectedLiveAgentPlanId"
              @click="setCompanionMode('anchor')"
            >主播模式</button>
          </div>
          <div class="companion-control-buttons" aria-label="直播搭子控制">
            <button
              v-if="!aiActive"
              type="button"
              :disabled="!coreActionsAvailable || runtimeControlBusy || (aiRuntimeMode === 'anchor' && !selectedLiveAgentPlanId)"
              @click="startCompanionRuntime"
            >{{ runtimeControlBusy ? '开始中…' : '开始' }}</button>
            <template v-else>
              <button
                v-if="aiRunning"
                type="button"
                class="pause"
                :disabled="!coreActionsAvailable || runtimeControlBusy"
                @click="pauseCompanionRuntime"
              >{{ runtimeControlBusy ? '暂停中…' : '暂停' }}</button>
              <button
                v-else-if="aiPaused"
                type="button"
                class="resume"
                :disabled="!coreActionsAvailable || runtimeControlBusy"
                @click="resumeCompanionRuntime"
              >{{ runtimeControlBusy ? '继续中…' : '继续' }}</button>
              <button
                type="button"
                class="end"
                :disabled="!coreActionsAvailable || runtimeControlBusy"
                @click="stopCompanionRuntime"
              >停止</button>
            </template>
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
            <button
              type="button"
              class="dashboard-live-state dashboard-collector-toggle"
              :class="dashboardCollectorState"
              :disabled="!coreActionsAvailable || !canControlMonitoring || monitorToggleBusy"
              :title="dashboardCollectorTitle"
              @click="toggleRoomMonitoring"
            >
              <i></i>
              {{ dashboardCollectorLabel }}
            </button>
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
            <button
              v-if="liveReviewAvailable"
              type="button"
              class="dashboard-review-button"
              :disabled="liveReviewLoading"
              @click="toggleLiveReview"
            >{{ liveReviewOpen ? '收起复盘' : '查看复盘' }}</button>
            <div class="dashboard-connection-copy">
              <span>{{ streamState === 'online' ? '公屏链路正常' : '公屏链路连接中' }}</span>
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
              :disabled="!coreActionsAvailable || deviceControlBusy"
              @click="controlBoundDevice('connect')"
            >{{ deviceControlBusy ? '连接中…' : '连接' }}</button>
            <template v-else>
              <button
                type="button"
                class="pause"
                :disabled="!coreActionsAvailable || deviceControlBusy"
                @click="controlBoundDevice(deviceVisualState === 'paused' ? 'resume' : 'pause')"
              >{{ deviceVisualState === 'paused' ? '继续' : '暂停' }}</button>
              <button
                type="button"
                class="close"
                :disabled="!coreActionsAvailable || deviceControlBusy"
                @click="controlBoundDevice('disconnect')"
              >断开</button>
            </template>
          </div>
          <small v-if="deviceControlError" class="device-control-error">{{ deviceControlError }}</small>
        </article>
      </section>

      <section v-if="liveReviewAvailable && liveReviewOpen" class="live-review-panel open review-body-only">
        <div class="live-review-body">
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
            <button
              type="button"
              class="speech-local-mute-button"
              :class="{ muted: localAudioMuted }"
              :title="localAudioMuted ? '恢复网页声音' : '静音网页声音'"
              :aria-label="localAudioMuted ? '恢复网页声音' : '静音网页声音'"
              @click="toggleLocalAudioMuted"
            >
              <svg v-if="!localAudioMuted" viewBox="0 0 24 24" aria-hidden="true">
                <path d="M4 9h4l5-4v14l-5-4H4z"></path>
                <path d="M16 8.5c1.2 1 1.8 2.1 1.8 3.5s-.6 2.5-1.8 3.5"></path>
                <path d="M18.7 6c1.8 1.7 2.8 3.7 2.8 6s-1 4.3-2.8 6"></path>
              </svg>
              <svg v-else viewBox="0 0 24 24" aria-hidden="true">
                <path d="M4 9h4l5-4v14l-5-4H4z"></path>
                <path d="M16.5 9.2l4.3 4.3"></path>
                <path d="M20.8 9.2l-4.3 4.3"></path>
              </svg>
            </button>
          </div>
        </header>

        <div v-if="!isInternalViewer" class="speech-runtime-unified">
          <div class="speech-mainline-live-caption" v-if="publicSpeechCaptionRows.length">
            <div class="speech-mainline-caption-stack">
              <div
                v-for="row in publicSpeechCaptionRows"
                :key="row.key"
                class="speech-mainline-caption-row"
                :class="['role-' + row.role, row.transition ? 'transition-' + row.transition : '']"
              >
                <span>{{ row.text }}</span>
              </div>
            </div>
          </div>
          <p v-else class="speech-runtime-unified-mainline">{{ mainlineFallbackPreview || '等待主播文案' }}</p>
          <div class="speech-runtime-unified-wave" :class="{ active: displayMainlineStatus === 'playing' || (interruptSpeech.status || '') === 'playing' }" aria-hidden="true">
            <i v-for="(height, index) in speechWaveBars" :key="index" :style="{ '--wave-height': height + 'px', '--wave-delay': (index * 37) + 'ms' }"></i>
          </div>
        </div>

        <div
          v-if="isInternalViewer"
          class="speech-runtime-track-grid"
          :class="[
            'layout-' + speechTrackLayoutState,
            {
              'is-mainline-expanded': speechTrackFocus === 'mainline',
              'is-interrupt-expanded': speechTrackFocus === 'interrupt',
            },
          ]"
        >
          <div class="speech-mainline-track-column">
            <article
              class="speech-track-card speech-mainline-card"
              :class="['status-' + displayMainlineStatus, { 'cut-imminent': mainlineSafeCutImminent }]"
            >
            <div v-if="mainlineCaptionRows.length" class="speech-mainline-live-caption">
              <div class="speech-mainline-caption-stack">
                <div
                  v-for="row in mainlineCaptionRows"
                  :key="row.segment.segment_id"
                  class="speech-mainline-caption-row"
                  :class="[
                    'role-' + row.role,
                    row.transition ? 'transition-' + row.transition : '',
                  ]"
                >
                  <span>{{ row.segment.text }}</span>
                </div>
              </div>
            </div>
            <p v-else :class="{ 'speech-empty-copy': !mainlineFallbackPreview }">{{ mainlineFallbackPreview || '等待主播文案' }}</p>
            <div
              class="speech-mainline-waveform"
              :class="{ active: displayMainlineStatus === 'playing' }"
              aria-hidden="true"
            >
              <i
                v-for="(height, index) in speechWaveBars"
                :key="index"
                :style="{ '--wave-height': height + 'px', '--wave-delay': (index * 37) + 'ms' }"
              ></i>
            </div>
            <time v-if="mainlineSpeech.updated_at">更新 {{ formatTime(mainlineSpeech.updated_at) }}</time>
            </article>
          </div>

          <article class="speech-track-card speech-interrupt-card" :class="'status-' + (interruptSpeech.status || 'idle')">
            <header>
              <span v-if="interruptSpeech.question_text" class="speech-trigger-question"><strong>触发问题：</strong>{{ interruptSpeech.question_text }}</span>
              <span v-else class="speech-interrupt-placeholder">场控答疑 / 临时插播</span>
              <div class="speech-track-head-actions">
                <button type="button" class="speech-track-size-button" @click="toggleSpeechTrackFocus('interrupt')">
                  {{ speechTrackFocus === 'interrupt' ? '还原' : '放大' }}
                </button>
                <b>{{ speechStatusLabel(interruptSpeech.status, 'interrupt') }}</b>
              </div>
            </header>
            <p class="speech-interrupt-copy">
              <span v-if="interruptSpeechDisplay.body">{{ interruptSpeechDisplay.body }}</span>
              <span v-if="interruptSpeechDisplay.bridge" class="speech-bridge-text">{{ interruptSpeechDisplay.bridge }}</span>
              <span v-if="!interruptSpeechDisplay.body && !interruptSpeechDisplay.bridge">等待临时插播…</span>
            </p>
            <div class="speech-interrupt-footer">
              <time v-if="interruptSpeech.updated_at">更新 {{ formatTime(interruptSpeech.updated_at) }}</time>
              <div class="speech-interrupt-actions">
                <button
                  type="button"
                  :disabled="!(interruptSpeech.reply_text || interruptSpeech.text)"
                  @click="correctCurrentGeneratedSpeech"
                >
                  纠正
                </button>
                <button type="button" class="history" @click="openGeneratedSpeechHistory">回答历史</button>
              </div>
            </div>
          </article>
        </div>
      </section>

      <div v-if="speechHistoryOpen" class="speech-history-mask" @click.self="closeGeneratedSpeechHistory">
        <section class="speech-history-dialog" role="dialog" aria-modal="true" aria-label="回答历史">
          <header class="speech-history-head">
            <div>
              <span>ANSWER HISTORY</span>
              <strong>回答历史</strong>
              <small>本场直播所有已生成并成功下发的话术</small>
            </div>
            <button type="button" class="speech-history-close" aria-label="关闭回答历史" @click="closeGeneratedSpeechHistory">×</button>
          </header>

          <div class="speech-history-search">
            <input
              v-model="speechHistoryQuery"
              type="search"
              placeholder="搜索问题、话术内容或来源"
              @input="scheduleGeneratedSpeechHistorySearch"
              @keyup.enter="loadGeneratedSpeechHistory(1)"
            />
            <button type="button" :disabled="speechHistoryLoading" @click="loadGeneratedSpeechHistory(1)">
              {{ speechHistoryLoading ? '查询中…' : '检索' }}
            </button>
          </div>

          <div v-if="speechHistoryError" class="speech-history-error">{{ speechHistoryError }}</div>
          <div v-else-if="speechHistoryLoading && !generatedSpeechHistory.items.length" class="speech-history-empty">正在读取本场回答历史…</div>
          <div v-else-if="!generatedSpeechHistory.items.length" class="speech-history-empty">本场还没有已生成并成功下发的话术。</div>
          <div v-else class="speech-history-list">
            <article v-for="item in generatedSpeechHistory.items" :key="item.id" class="speech-history-item">
              <div class="speech-history-item-meta">
                <span class="speech-history-source">{{ generatedSpeechSourceLabel(item.source_type) }}</span>
                <time>{{ formatTime(item.created_at) }}</time>
                <span class="speech-history-correction" :class="'status-' + item.correction_status">
                  {{ generatedSpeechCorrectionLabel(item) }}
                  <template v-if="item.correction_count"> · {{ item.correction_count }} 次</template>
                </span>
              </div>
              <p v-if="item.question_text" class="speech-history-question"><strong>关联问题：</strong>{{ item.question_text }}</p>
              <p class="speech-history-reply">{{ item.generated_text }}</p>
              <div class="speech-history-item-actions">
                <button type="button" @click="correctGeneratedSpeechHistoryItem(item)">纠正</button>
              </div>
            </article>
          </div>

          <footer class="speech-history-pagination">
            <div class="speech-history-pagination-summary">
              <span>共 {{ generatedSpeechHistory.total }} 条</span>
              <label class="speech-history-page-size">
                <span>每页</span>
                <select v-model.number="speechHistoryPageSize" :disabled="speechHistoryLoading" @change="changeGeneratedSpeechHistoryPageSize">
                  <option :value="15">15</option>
                  <option :value="30">30</option>
                  <option :value="50">50</option>
                </select>
                <span>行</span>
              </label>
            </div>
            <div class="speech-history-page-buttons">
              <button type="button" :disabled="generatedSpeechHistory.page <= 1 || speechHistoryLoading" @click="goGeneratedSpeechHistoryPage(generatedSpeechHistory.page - 1)">上一页</button>
              <button
                v-for="page in speechHistoryPageNumbers"
                :key="page"
                type="button"
                class="page-number"
                :class="{ active: page === generatedSpeechHistory.page }"
                :disabled="speechHistoryLoading"
                @click="goGeneratedSpeechHistoryPage(page)"
              >{{ page }}</button>
              <button type="button" :disabled="generatedSpeechHistory.page >= speechHistoryTotalPages || speechHistoryLoading" @click="goGeneratedSpeechHistoryPage(generatedSpeechHistory.page + 1)">下一页</button>
            </div>
          </footer>
        </section>
      </div>

      <section class="detail-layout detail-layout-no-preview">
        <div
          ref="publicScreenPanelEl"
          class="public-screen-panel"
          :class="{ 'is-resizing': publicScreenResizing }"
          :style="publicScreenPanelStyle"
        >
          <div class="panel-header">
            <div>
              <span class="section-kicker">{{ publicScreenMode === 'bucket' ? 'EVENT AGGREGATION' : publicScreenMode === 'execution' ? 'INTERACTION EXECUTION' : publicScreenMode === 'preferences' ? 'INTERACTION PREFERENCE' : 'REALTIME' }}</span>
              <h3>{{ publicScreenMode === 'bucket' ? '事件聚合' : publicScreenMode === 'execution' ? '互动执行' : publicScreenMode === 'preferences' ? '互动偏好' : '实时公屏' }}</h3>
            </div>
            <span v-if="publicScreenMode !== 'preferences'" class="event-count">{{ publicScreenMode === 'bucket' ? semanticBuckets.length + ' 组' : publicScreenMode === 'execution' ? interactionExecutionQueue.length + ' 条' : filteredEvents.length + ' 条' }}</span>
          </div>
          <div class="public-screen-mode-switch" role="tablist" aria-label="公屏视图切换">
            <button type="button" :class="{ active: publicScreenMode === 'events' }" @click="publicScreenMode = 'events'">实时公屏</button>
            <button type="button" :class="{ active: publicScreenMode === 'bucket' }" @click="publicScreenMode = 'bucket'">事件聚合</button>
            <button type="button" :class="{ active: publicScreenMode === 'execution' }" @click="publicScreenMode = 'execution'">互动执行</button>
            <button type="button" class="mobile-preferences-tab" :class="{ active: publicScreenMode === 'preferences' }" @click="publicScreenMode = 'preferences'">互动偏好</button>
          </div>

          <div v-if="publicScreenMode === 'events'" class="event-tabs">
            <button
              v-for="type in eventTypes"
              :key="type.key"
              :class="{ active: activeType === type.key }"
              @click="selectEventType(type.key)"
            >
              {{ type.label }}
            </button>
          </div>

          <div v-if="publicScreenMode === 'events'" ref="eventListEl" class="event-list" @scroll.passive="handleEventListScroll">
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
                'answer-reference-pickable': answerReferencePicking && isDirectAnswerEvent(event),
              }"
              @mouseenter="handleEventMouseEnter(event)"
              @mouseleave="handleEventMouseLeave(event)"
              @click="handlePublicScreenAnswerReferenceClick(event)"
              @contextmenu.prevent.stop="openEventContextMenu($event, event)"
            >
              <time>{{ formatTime(event.occurred_at) }}</time>
              <span class="event-type" :class="eventClass(event.event_type)">
                {{ eventLabel(event.event_type) }}
              </span>
              <div class="event-body event-stacked">
                <div
                  class="event-topline"
                  :class="{ 'actions-only': aiRunning && isDirectAnswerEvent(event) }"
                >
                  <span v-if="!(aiRunning && isDirectAnswerEvent(event))" class="event-user">【{{ event.nickname || '直播间用户' }}】：</span>
                  <div v-if="aiRunning && isDirectAnswerEvent(event)" class="event-ai-actions" @click.stop @contextmenu.stop>
                    <button
                      type="button"
                      class="reference"
                      @click="sendPublicScreenEventToAnswerReference(event)"
                    >纠正</button>
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
                  </div>
                </div>
                <div v-if="aiRunning && isDirectAnswerEvent(event)" class="event-message-line">
                  <span class="event-user">【{{ event.nickname || '直播间用户' }}】：</span>
                  <span class="event-action">{{ event.content || eventLabel(event.event_type) }}</span>
                </div>
                <span v-else class="event-action">{{ event.content || eventLabel(event.event_type) }}</span>
                <small v-if="eventDecisionRowState(event.id).message" class="event-ai-feedback ok">{{ eventDecisionRowState(event.id).message }}</small>
                <small v-else-if="eventDecisionRowState(event.id).error" class="event-ai-feedback error">{{ eventDecisionRowState(event.id).error }}</small>
              </div>
            </article>
            <div v-if="importantLoading[activeType]" class="important-history-loading">正在加载更早记录…</div>
          </div>
          <div v-if="publicScreenMode === 'bucket'" class="public-event-bucket-view">
            <div class="public-event-bucket-summary">
              <article class="tone-entry"><span>进房</span><strong>+{{ sessionStats?.entries ?? 0 }}</strong></article>
              <article class="tone-chat"><span>弹幕</span><strong>+{{ sessionStats?.chats ?? 0 }}</strong></article>
              <article class="tone-like"><span>点赞</span><strong>+{{ (sessionStats?.likes ?? 0).toLocaleString() }}</strong></article>
              <article class="tone-follow"><span>关注</span><strong>+{{ sessionStats?.follows ?? 0 }}</strong></article>
              <article class="tone-gift"><span>礼物</span><strong>+{{ sessionStats?.gifts ?? 0 }}</strong></article>
              <article class="tone-order"><span>下单信号</span><strong>+{{ sessionStats?.order_signals || 0 }}</strong></article>
            </div>
            <div class="public-question-buckets">
              <header>
                <strong>问题聚合</strong>
                <span>{{ semanticBuckets.length }} 个话题</span>
              </header>
              <div v-if="!semanticBuckets.length" class="screen-empty compact">
                <strong>暂时没有问题聚合</strong>
                <span>出现相似问题后会自动合并，方便你快速查看。</span>
              </div>
              <article
                v-for="bucket in semanticBuckets"
                :key="'public-bucket-' + bucket.Topic"
                class="public-question-bucket"
                :class="'tone-' + semanticBucketTone(bucket)"
              >
                <div class="public-question-bucket-head">
                  <button type="button" class="public-question-bucket-main" @click="handleQuestionBucketClick(bucket)">
                    <span>
                      <strong>{{ semanticBucketLabel(bucket) }}</strong>
                      <small>{{ bucket.UniqueUsers || bucket.Count }} 人提问 · 最近 {{ formatTime(bucket.LastSeenAt) }}</small>
                    </span>
                    <b>+{{ bucket.Count }}</b>
                  </button>
                  <div v-if="aiActive" class="public-question-actions" @click.stop>
                    <button type="button" class="correct" @click="sendBucketToAnswerReference(bucket)">纠正</button>
                    <button
                      type="button"
                      class="quick"
                      :disabled="Boolean(bucketDecisionBusyState(bucket.Topic)) || !aiRunning || !questionBucketTTSEligible(bucket)"
                      @click="answerQuestionBucket(bucket, 'quick')"
                    >{{ bucketDecisionBusyState(bucket.Topic) === 'quick' ? '抢答中…' : '抢答' }}</button>
                    <button
                      type="button"
                      class="answer"
                      :disabled="Boolean(bucketDecisionBusyState(bucket.Topic)) || !aiRunning || !questionBucketTTSEligible(bucket)"
                      @click="answerQuestionBucket(bucket, 'answer')"
                    >{{ bucketDecisionBusyState(bucket.Topic) === 'answer' ? '回答中…' : '回答' }}</button>
                  </div>
                </div>
                <div v-if="expandedQuestionTopic === bucket.Topic" class="public-question-list">
                  <div
                    v-for="question in bucket.Questions || []"
                    :key="question.EventID || question.Content + question.OccurredAt"
                    class="public-question-item"
                  >
                    <div class="public-question-item-copy" @click="handleQuestionDetailClick(question, bucket)">
                      <span>{{ question.Nickname || question.UserID || '直播间用户' }} · {{ formatTime(question.OccurredAt) }}</span>
                      <p>{{ question.Content }}</p>
                    </div>
                    <div v-if="aiActive" class="public-question-actions item-actions" @click.stop>
                      <button type="button" class="correct" @click="sendQuestionToAnswerReference(question, bucket)">纠正</button>
                      <button
                        type="button"
                        class="quick"
                        :disabled="Boolean(questionDecisionBusyState(question.EventID)) || !aiRunning || !questionTTSEligible(bucket, question)"
                        @click="answerQuestionDetail(bucket, question, 'quick')"
                      >{{ questionDecisionBusyState(question.EventID) === 'quick' ? '抢答中…' : '抢答' }}</button>
                      <button
                        type="button"
                        class="answer"
                        :disabled="Boolean(questionDecisionBusyState(question.EventID)) || !aiRunning || !questionTTSEligible(bucket, question)"
                        @click="answerQuestionDetail(bucket, question, 'answer')"
                      >{{ questionDecisionBusyState(question.EventID) === 'answer' ? '回答中…' : '回答' }}</button>
                    </div>
                  </div>
                </div>
                <div v-if="agentDecisionActionMessage" class="public-question-action-message">{{ agentDecisionActionMessage }}</div>
              </article>
            </div>
          </div>
          <div v-if="publicScreenMode === 'execution'" class="interaction-execution-view">
            <div class="interaction-execution-summary">
              <span><b>{{ interactionExecutionRunningCount }}</b> 正在执行</span>
              <span><b>{{ interactionExecutionPendingCount }}</b> 准备执行</span>
              <small>
                默认展示 1 条执行中 + 10 条等待 · 新任务排在下面
                <template v-if="interactionExecutionPendingTotal > 10"> · 另有 {{ interactionExecutionPendingTotal - 10 }} 条继续排队</template>
              </small>
            </div>

            <div v-if="!interactionExecutionQueue.length" class="screen-empty compact interaction-execution-empty">
              <strong>{{ aiRunning ? '暂时没有待执行互动' : '智能体还没工作' }}</strong>
              <span>欢迎、关注、点赞、问答、抢答等进入执行队列后会显示在这里。</span>
            </div>

            <div v-else class="interaction-execution-list">
              <article
                v-for="(item, index) in interactionExecutionQueue"
                :key="'interaction-execution-' + item.id"
                class="interaction-execution-item"
                :class="{
                  running: item.status === 'CLAIMED',
                  manual: item.sources?.includes('manual'),
                }"
              >
                <div class="interaction-execution-order">
                  <b>{{ item.status === 'CLAIMED' ? interactionExecutionStageLabel(item) : String(index + 1 - interactionExecutionRunningCount).padStart(2, '0') }}</b>
                </div>
                <div class="interaction-execution-copy">
                  <header>
                    <span class="interaction-execution-type">{{ interactionExecutionTypeLabel(item) }}</span>
                    <time>{{ formatTime(item.created_at) }}</time>
                  </header>
                  <strong>{{ item.title || interactionExecutionTypeLabel(item) }}</strong>
                  <p>{{ interactionExecutionDetail(item) }}</p>
                  <div class="interaction-execution-meta">
                    <span v-if="item.nicknames?.length">对象 {{ item.nicknames.slice(0, 3).join('、') }}</span>
                    <span v-if="item.merged_count > 1">融合 {{ item.merged_count }} 条</span>
                    <span :class="{ danger: item.status !== 'CLAIMED' && agentDecisionExpiryText(item.expires_at) === '00:00' }">
                      {{ interactionExecutionCountdown(item) }}
                    </span>
                  </div>
                </div>
                <button
                  type="button"
                  class="interaction-execution-delete"
                  :disabled="item.status === 'CLAIMED' || agentDecisionRemoveBusy === item.id"
                  :title="item.status === 'CLAIMED' ? '这条互动已经开始执行，不能只删除队列记录' : '删除这条待执行互动'"
                  @click="removeInteractionExecutionItem(item)"
                >
                  {{ agentDecisionRemoveBusy === item.id ? '删除中' : '删除' }}
                </button>
              </article>
            </div>

            <div v-if="agentDecisionActionMessage" class="interaction-execution-message">
              {{ agentDecisionActionMessage }}
            </div>
          </div>
          <RoomPreferenceHub
            v-if="publicScreenMode === 'preferences'"
            class="mobile-public-preference-hub"
            :room-id="roomId"
            compact
            :mobile="true"
          />
          <div
            v-if="publicScreenMode === 'events'"
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
          <RoomPreferenceHub
            class="room-middle-preference-hub"
            :room-id="roomId"
            compact
          />
          <section
            v-if="isInternalViewer"
            ref="agentDecisionPanelEl"
            class="agent-decision-panel"
            :class="{ 'is-resizing': agentPanelResizing }"
            :style="agentDecisionPanelStyle"
          >
            <div class="agent-decision-head">
              <div>
                <h3>小蓝 Agent 思考</h3>
              </div>
              <div class="agent-decision-head-actions">
                <button
                  v-if="isInternalViewer"
                  type="button"
                  class="speech-mission-toggle"
                  :class="{ active: speechMissionOpen }"
                  @click="speechMissionOpen = !speechMissionOpen"
                >策略黑板</button>
                <span
                  class="agent-decision-state"
                  :class="{ ready: agentDecisionState?.summary?.state === 'READY_TO_INTERRUPT' }"
                >
                  {{ agentDecisionStateLabel(agentDecisionState?.summary?.state) }}
                </span>
              </div>
            </div>

            <div class="agent-thinking-zone">

            <div v-if="isInternalViewer && speechMissionOpen" class="speech-mission-board">
              <template v-if="latestSpeechMission">
                <header>
                  <div class="speech-mission-title">
                    <small>MISSION {{ latestSpeechMission.id }}</small>
                    <strong>{{ speechMissionStateLabel(latestSpeechMission.state) }}</strong>
                  </div>
                  <div class="speech-mission-header-actions">
                    <select v-model="selectedSpeechMissionId" aria-label="选择口播任务">
                      <option v-for="mission in speechMissions.slice(0, 20)" :key="mission.id" :value="mission.id">
                        {{ mission.id }} · {{ speechMissionStateLabel(mission.state) }}
                      </option>
                    </select>
                    <time>{{ formatTime(latestSpeechMission.updated_at) }}</time>
                  </div>
                </header>
                <div class="speech-mission-flow">
                  <span>
                    <small>事件价值</small>
                    <b>{{ Number(latestSpeechMission.interaction.decision?.event_value || 0) > 0 ? Number(latestSpeechMission.interaction.decision.event_value).toFixed(1) : '—' }}</b>
                  </span>
                  <i>→</i>
                  <span>
                    <small>热度</small>
                    <b>{{ latestSpeechMission.interaction.decision?.heat || latestSpeechMission.human_style.state?.heat || '—' }}</b>
                  </span>
                  <i>→</i>
                  <span>
                    <small>决策</small>
                    <b>{{ latestSpeechMission.interaction.decision?.budget_allowed ? '放行' : '等待' }}</b>
                  </span>
                  <i>→</i>
                  <span>
                    <small>文本</small>
                    <b>{{ latestSpeechMission.generated_text ? '已生成' : '待生成' }}</b>
                  </span>
                  <i>→</i>
                  <span>
                    <small>TTS</small>
                    <b>{{ latestSpeechMission.tts.audio_url ? '已生成' : '待生成' }}</b>
                  </span>
                  <i>→</i>
                  <span>
                    <small>恢复段</small>
                    <b>{{ latestSpeechMission.resume.actual_resume_segment || latestSpeechMission.resume.planned_resume_segment || latestSpeechMission.resume.resume_segment_id || '—' }}</b>
                  </span>
                </div>
                <div class="speech-mission-grid">
                  <article>
                    <span>事件决策</span>
                    <b>
                      {{ latestSpeechMission.interaction.decision?.value_level || '—' }}
                      ·
                      {{ Number(latestSpeechMission.interaction.decision?.event_value || 0) > 0 ? Number(latestSpeechMission.interaction.decision.event_value).toFixed(1) : '—' }}
                    </b>
                    <small>
                      {{ latestSpeechMission.interaction.decision?.heat || '—' }}
                      · {{ latestSpeechMission.interaction.decision?.budget_level || '—' }}
                      · {{ latestSpeechMission.interaction.decision?.budget_allowed ? '已放行' : '继续等待' }}
                    </small>
                  </article>
                  <article>
                    <span>互动</span>
                    <b>{{ latestSpeechMission.interaction.goal || latestSpeechMission.event.title || '—' }}</b>
                    <small>{{ latestSpeechMission.event.event_count || 0 }} 个事件 · {{ latestSpeechMission.event.window_seconds || 0 }}s 窗口</small>
                  </article>
                  <article>
                    <span>打断</span>
                    <b>{{ latestSpeechMission.interrupt.name || latestSpeechMission.interrupt.strategy || '—' }}</b>
                    <small>实际切点 {{ missionMS(latestSpeechMission.mainline.switch_at_ms) }}</small>
                  </article>
                  <article>
                    <span>回归</span>
                    <b>{{ latestSpeechMission.resume.name || latestSpeechMission.resume.strategy || '—' }}</b>
                    <small>
                      {{ latestSpeechMission.resume.cut_after_segment || '—' }}
                      → {{ latestSpeechMission.resume.actual_resume_segment || latestSpeechMission.resume.planned_resume_segment || latestSpeechMission.resume.resume_segment_id || '—' }}
                      · 跳 {{ latestSpeechMission.resume.skip_count || 0 }} 段
                    </small>
                  </article>
                  <article>
                    <span>称呼</span>
                    <b>
                      {{ latestSpeechMission.addressing.mode || 'NONE' }}
                      ·
                      {{
                        latestSpeechMission.addressing.selected_names?.length
                          ? latestSpeechMission.addressing.selected_names.join('、')
                          : latestSpeechMission.addressing.group_label || latestSpeechMission.addressing.candidate || '不强制称呼'
                      }}
                    </b>
                    <small>最多 {{ latestSpeechMission.addressing.max_named_count || 0 }} 个昵称 · 最近点名惩罚 {{ Math.round((latestSpeechMission.addressing.recent_name_penalty || 0) * 100) }}%</small>
                  </article>
                  <article>
                    <span>真人行为</span>
                    <b>{{ latestSpeechMission.human_style.reaction?.kind || latestSpeechMission.human_style.kind || 'NONE' }}</b>
                    <small>
                      {{ latestSpeechMission.human_style.reaction?.enabled ? '本轮启用' : '本轮不触发' }}
                      · {{ latestSpeechMission.human_style.reaction?.delivery || latestSpeechMission.human_style.delivery || 'TEXT' }}
                    </small>
                  </article>
                </div>
                <div v-if="latestSpeechMission.interaction.decision?.reason" class="speech-mission-context decision-context">
                  <span>事件为什么现在处理</span>
                  <p>{{ latestSpeechMission.interaction.decision.reason }}</p>
                </div>
                <div v-if="latestSpeechMission.interaction.decision?.question_debt" class="speech-mission-meta">
                  <span>问题债务 ×{{ latestSpeechMission.interaction.decision.question_debt.repeat_count || 1 }}</span>
                  <span>独立用户 {{ latestSpeechMission.interaction.decision.question_debt.unique_users || 0 }}</span>
                  <span>等待 {{ latestSpeechMission.interaction.decision.question_debt.waiting_seconds || 0 }}s</span>
                  <span>队列优先 {{ Math.round(latestSpeechMission.interaction.decision.question_debt.current_priority || 0) }}</span>
                </div>
                <div
                  v-if="latestSpeechMission.human_style.trait?.instruction || latestSpeechMission.human_style.state?.host_state || latestSpeechMission.human_style.reaction?.rule_id"
                  class="speech-mission-meta"
                >
                  <span v-if="latestSpeechMission.human_style.trait?.instruction">长期习惯 {{ latestSpeechMission.human_style.trait.instruction }}</span>
                  <span v-if="latestSpeechMission.human_style.state?.host_state">当前状态 {{ latestSpeechMission.human_style.state.host_state }}</span>
                  <span v-if="latestSpeechMission.human_style.reaction?.rule_id">规则 {{ latestSpeechMission.human_style.reaction.rule_id }}</span>
                  <span v-if="latestSpeechMission.human_style.reaction?.source">来源 {{ latestSpeechMission.human_style.reaction.source }}</span>
                  <span v-if="latestSpeechMission.human_style.reaction?.channel">渠道 {{ latestSpeechMission.human_style.reaction.channel }}</span>
                  <span v-if="latestSpeechMission.human_style.reaction?.intensity">强度 {{ Math.round((latestSpeechMission.human_style.reaction.intensity || 0) * 100) }}%</span>
                </div>
                <div v-if="latestSpeechMission.resume.resume_preview" class="speech-mission-context">
                  <span>回归目标</span>
                  <p>{{ latestSpeechMission.resume.resume_preview }}</p>
                </div>
                <div v-if="latestSpeechMission.generated_text" class="speech-mission-context final-text">
                  <span>最终话术</span>
                  <p>{{ latestSpeechMission.generated_text }}</p>
                </div>
                <div class="speech-mission-meta">
                  <span>{{ latestSpeechMission.human_style.emotion || 'natural' }} · {{ latestSpeechMission.human_style.pace || 'normal' }}</span>
                  <span v-if="latestSpeechMission.opening.intent">开头 {{ latestSpeechMission.opening.intent }}</span>
                  <span v-if="latestSpeechMission.tts.provider">TTS {{ latestSpeechMission.tts.provider }} · {{ latestSpeechMission.tts.model || '默认模型' }}</span>
                  <span v-if="latestSpeechMission.resume.dedup_triggered">回归去重 {{ Math.round((latestSpeechMission.resume.duplicate_score || 0) * 100) }}%</span>
                </div>
                <div v-if="latestSpeechMission.trace?.length" class="speech-mission-trace">
                  <div v-for="entry in latestSpeechMission.trace.slice(-6).reverse()" :key="entry.at + entry.action">
                    <time>{{ formatTime(entry.at) }}</time>
                    <b>{{ speechMissionStateLabel(entry.state) }}</b>
                    <span>{{ entry.note || entry.action || '状态更新' }}</span>
                  </div>
                </div>
              </template>
              <div v-else class="agent-decision-empty compact">暂无本轮策略黑板</div>
            </div>
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
            v-if="isInternalViewer"
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
                <span class="section-kicker">EVENT AGGREGATION</span>
                <h3>事件聚合</h3>
              </div>
              <span class="semantic-bucket-count">本次采集</span>
            </div>

            <div class="event-bucket-section">
              <span class="event-bucket-section-title">基础事件</span>
              <div class="base-event-bucket-list">
                <article class="base-event-bucket base-member">
                  <span>进房</span><strong>+{{ sessionStats?.entries ?? 0 }}</strong>
                </article>
                <article class="base-event-bucket base-chat">
                  <span>弹幕</span><strong>+{{ sessionStats?.chats ?? 0 }}</strong>
                </article>
                <article class="base-event-bucket base-like">
                  <span>点赞</span><strong>+{{ (sessionStats?.likes ?? 0).toLocaleString() }}</strong>
                </article>
                <article class="base-event-bucket base-follow">
                  <span>关注</span><strong>+{{ sessionStats?.follows ?? 0 }}</strong>
                </article>
                <article class="base-event-bucket base-gift">
                  <span>礼物</span><strong>+{{ sessionStats?.gifts ?? 0 }}</strong>
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
                      :class="{ 'answer-reference-pickable': answerReferencePicking }"
                      @click="handleQuestionBucketClick(bucket)"
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
                        @click.stop="sendBucketToAnswerReference(bucket)"
                      >纠正</button>
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
                        :class="{
                          selected: selectedQuestionEventId === question.EventID,
                          'answer-reference-pickable': answerReferencePicking,
                        }"
                        @click.stop="handleQuestionDetailClick(question, bucket)"
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
                          @click.stop="sendQuestionToAnswerReference(question, bucket)"
                        >纠正</button>
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
                <div class="single-question-actions">
                  <button
                    type="button"
                    @click="sendQuestionToAnswerReference(selectedQuestionDetail, selectedQuestionBucket || undefined)"
                  >纠正</button>
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
              aria-label="拖动调整事件聚合高度"
              @pointerdown="startEventBucketResize"
            >
              <span class="agent-panel-resize-lines" aria-hidden="true"></span>
            </div>
          </section>

        </div>

        <aside class="capture-workspace">
          <header class="capture-workspace-head">
            <div>
              <span class="section-kicker">CAPTURE</span>
              <h3>采集工作区</h3>
            </div>
            <b :class="'mode-' + (captureSnapshot?.mode || 'idle')">{{ captureModeLabel }}</b>
          </header>

          <section class="capture-card local-video-viewer-card" :class="{ active: localVideoViewerActive }">
            <header>
              <div>
                <strong>视频观看</strong>
                <small>Chrome 标签页共享 · 仅浏览器本机处理</small>
              </div>
              <span :class="{ active: localVideoViewerActive }">
                {{ localVideoViewerActive ? '共享中' : '本机' }}
              </span>
            </header>
            <div class="browser-share-guide" :class="{ active: localVideoViewerActive }">
              <div><b>1</b><span>打开当前抖音直播页，需要登录就正常扫码登录。</span></div>
              <div><b>2</b><span>点击共享，在 Chrome 弹窗里选择刚打开的抖音标签页。</span></div>
              <div><b>3</b><span>系统右侧出现悬浮窗，可裁掉评论区和网页边缘。</span></div>
            </div>
            <div class="browser-share-actions">
              <button
                type="button"
                class="capture-primary-button viewer secondary"
                :disabled="!douyinLiveRoomUrl"
                @click="openDouyinWatchPage"
              >打开抖音页面</button>
              <button
                v-if="!localVideoViewerActive"
                type="button"
                class="capture-primary-button viewer"
                :disabled="videoShareBusy"
                @click="startBrowserVideoShare"
              >{{ videoShareBusy ? '正在请求共享…' : '共享抖音标签页' }}</button>
              <button
                v-else
                type="button"
                class="capture-stop-button"
                @click="stopBrowserVideoShare"
              >停止共享画面</button>
            </div>
            <small class="capture-lock-tip">Chrome 会要求用户主动授权共享；视频只在当前浏览器内处理，不上传服务器。</small>
            <div v-if="videoShareError" class="recording-error">{{ videoShareError }}</div>
          <p class="capture-business-note">观看窗口与直播数据采集完全独立；关掉共享不会停止公屏、智能分析或声音录制。</p>
          </section>

          <section class="capture-card audio-record-card" :class="{ recording: audioRecordingActive }">
            <header>
              <div>
                <strong>声音录制</strong>
                <small>当前直播间音轨 · 网页关闭后继续</small>
              </div>
              <span :class="{ active: audioRecordingActive }">
                {{ audioRecordingActive ? '录制中' : (audioRecordingFinalizing ? '合并中' : '后台') }}
              </span>
            </header>
            <div class="audio-record-visual" :class="{ active: audioRecordingActive }" aria-hidden="true">
              <i v-for="index in 18" :key="index" :style="{ height: (18 + ((index * 17 + audioRecordingDuration) % 64)) + '%' }"></i>
            </div>
            <div class="audio-record-metrics">
              <div><span>已录时长</span><strong>{{ formatClockSeconds(audioRecordingDuration) }}</strong></div>
              <div><span>分段</span><strong>{{ captureSnapshot?.recording?.segment_count || 0 }}</strong></div>
            </div>
            <button
              v-if="!audioRecordingActive"
              type="button"
              class="capture-primary-button audio"
              :disabled="!coreActionsAvailable || captureBusy || audioRecordingFinalizing"
              @click="startCoreAudioRecording"
            >
              {{ audioRecordingFinalizing ? '正在合并录音…' : '开始声音录制' }}
            </button>
            <button
              v-else
              type="button"
              class="capture-stop-button"
              :disabled="!coreActionsAvailable || captureBusy"
              @click="stopCoreAudioRecording"
            >{{ captureBusy ? '正在停止并合并…' : '停止并合并' }}</button>
            <div v-if="captureSnapshot?.recording?.status === 'ready'" class="recording-delivery">
              <div>
                <strong>完整录音已生成</strong>
                <small>{{ captureSnapshot.recording.final_file_name }} · {{ formatCaptureBytes(captureSnapshot.recording.final_bytes) }}</small>
              </div>
              <a :href="roomAudioRecordingFileUrl(roomId)" download>下载 WAV</a>
            </div>
            <div v-else-if="captureSnapshot?.recording?.status === 'failed'" class="recording-error">
              {{ captureSnapshot.recording.error || '录音处理失败' }}
            </div>
            <p class="capture-business-note">可用于同行直播跟踪；后续可继续转逐字稿、拆解话术并进入素材库。</p>
          </section>

          <div v-if="captureError" class="capture-workspace-error">{{ captureError }}</div>
          <section class="capture-card speech-analysis-card" :class="{ active: speechAnalysisRunning }">
            <header>
              <div>
                <strong>智能话术分析</strong>
                <small>录音转文字 · 话术分析 · 优化建议</small>
              </div>
              <span :class="{ active: currentSpeechAnalysisTask?.status === 'ready' }">{{ speechAnalysisStatusLabel }}</span>
            </header>

            <div class="speech-analysis-progress" :class="{ active: speechAnalysisRunning || speechAnalysisUploadBusy }">
              <div class="speech-analysis-progress-copy">
                <span>{{ speechAnalysisPublicStage }}</span>
                <strong>{{ speechAnalysisUploadBusy ? speechAnalysisUploadPercent : (currentSpeechAnalysisTask?.progress || 0) }}%</strong>
              </div>
              <div class="speech-analysis-progress-track" aria-hidden="true">
                <i :style="{ width: Math.max(0, Math.min(100, speechAnalysisUploadBusy ? speechAnalysisUploadPercent : (currentSpeechAnalysisTask?.progress || 0))) + '%' }"></i>
              </div>
              <div v-if="speechAnalysisUploadBusy" class="speech-analysis-upload-progress-detail">
                <span>{{ formatCaptureBytes(speechAnalysisUploadLoaded) }} / {{ formatCaptureBytes(speechAnalysisUploadTotal || speechAnalysisUploadFile?.size) }}</span>
                <span>{{ speechAnalysisUploadSpeed }}</span>
                <button type="button" @click="cancelSpeechAnalysisUpload">取消上传</button>
              </div>
              <div v-if="speechAnalysisStepIndex >= 0" class="speech-analysis-steps">
                <div
                  v-for="(step, index) in speechAnalysisSteps"
                  :key="step"
                  class="speech-analysis-step"
                  :class="{
                    done: !speechAnalysisUploadBusy && (index < speechAnalysisStepIndex || currentSpeechAnalysisTask?.status === 'ready'),
                    current: index === speechAnalysisStepIndex && (speechAnalysisUploadBusy || currentSpeechAnalysisTask?.status !== 'ready'),
                    failed: !speechAnalysisUploadBusy && index === speechAnalysisStepIndex && currentSpeechAnalysisTask?.status === 'failed',
                  }"
                >
                  <i>{{ !speechAnalysisUploadBusy && (index < speechAnalysisStepIndex || currentSpeechAnalysisTask?.status === 'ready') ? '✓' : index + 1 }}</i>
                  <span>{{ step }}</span>
                </div>
              </div>
            </div>

            <div v-if="currentSpeechAnalysisTask?.transcript_object_key" class="recording-delivery speech-analysis-transcript-delivery">
              <div>
                <strong>文字稿已生成</strong>
                <small>后续话术分析会继续进行，现在就可以先下载文字。</small>
              </div>
              <a :href="roomSpeechAnalysisTranscriptUrl(roomId)" download>下载文字</a>
            </div>

            <div class="speech-analysis-upload-box">
              <label class="speech-analysis-file-picker" :class="{ disabled: speechAnalysisRunning || speechAnalysisUploadBusy }">
                <input
                  type="file"
                  accept=".wav,.mp3,.m4a,.aac,.flac,.ogg,.webm,audio/*"
                  :disabled="speechAnalysisRunning || speechAnalysisUploadBusy"
                  @change="selectSpeechAnalysisUploadFile"
                />
                <span>{{ speechAnalysisUploadFile ? '重新选择录音' : '选择已有录音' }}</span>
              </label>
              <div class="speech-analysis-file-meta">
                <strong>{{ speechAnalysisUploadFile?.name || '支持 WAV / MP3 / M4A / AAC / FLAC / OGG / WEBM' }}</strong>
                <small v-if="speechAnalysisUploadFile">{{ formatCaptureBytes(speechAnalysisUploadFile.size) }}</small>
                <small v-else>上传后自动开始转文字、话术分析和优化建议。</small>
              </div>
              <button
                type="button"
                class="capture-primary-button speech-analysis upload"
                :disabled="!speechAnalysisCanUpload"
                @click="uploadSpeechAnalysisRecording"
              >{{ speechAnalysisUploadBusy ? ('上传 ' + speechAnalysisUploadPercent + '%') : '上传并分析' }}</button>
            </div>

            <div class="speech-analysis-or"><span>或使用本直播间刚录好的音频</span></div>

            <button
              v-if="currentSpeechAnalysisTask?.status !== 'ready'"
              type="button"
              class="capture-primary-button speech-analysis"
              :disabled="!speechAnalysisCanStart"
              @click="startSpeechAnalysis"
            >
              {{ speechAnalysisRunning ? '智能分析进行中…' : (speechAnalysisBusy ? '正在准备分析…' : '分析刚完成的直播录音') }}
            </button>

            <small v-if="speechAnalysisStatus && !speechAnalysisStatus.configured" class="capture-lock-tip">
              分析服务暂不可用，请联系管理员。
            </small>
            <small v-else-if="captureSnapshot?.recording?.status !== 'ready' && !currentSpeechAnalysisTask" class="capture-lock-tip">
              可以直接上传已有录音；也可以先完成本直播间声音录制再分析。
            </small>

            <div v-if="currentSpeechAnalysisTask?.status === 'ready'" class="recording-delivery speech-analysis-delivery">
              <div>
                <strong>本场直播分析已生成</strong>
                <small>可以直接查看，也可以下载完整分析报告。</small>
              </div>
              <div class="speech-analysis-delivery-actions">
                <button type="button" :disabled="speechAnalysisReportLoading" @click="toggleSpeechAnalysisReport">
                  {{ speechAnalysisReportLoading ? '读取中…' : (speechAnalysisReportOpen ? '收起报告' : '查看报告') }}
                </button>
                <a :href="roomSpeechAnalysisReportUrl(roomId)" download>下载报告</a>
              </div>
            </div>
            <div v-if="speechAnalysisReportOpen" class="speech-analysis-report-panel">
              <header>
                <div>
                  <strong>本场直播复盘</strong>
                  <small>优点 · 问题 · 修改建议 · 改写示例 · 风险点 · 完整逐字稿</small>
                </div>
                <button type="button" :disabled="!speechAnalysisReportText" @click="copySpeechAnalysisReport">复制报告</button>
              </header>
              <pre>{{ speechAnalysisReportText }}</pre>
            </div>
            <div v-if="currentSpeechAnalysisTask?.status === 'failed'" class="recording-error">
              {{ speechAnalysisFailureText }}
            </div>
            <div v-if="speechAnalysisError" class="recording-error">{{ speechAnalysisError }}</div>
            <p class="capture-business-note">这里只复盘这一场直播：明确做得好的、做得不好的、怎么优化，并给出下一场可执行建议。</p>
          </section>

          <div v-if="captureError" class="capture-workspace-error">{{ captureError }}</div>
        </aside>
      </section>


      <button
        ref="blockedDrawerHandleEl"
        type="button"
        class="blocked-drawer-handle"
        :class="{ open: blockedDrawerOpen }"
        :style="blockedDrawerHandleStyle"
        data-edge-handle="blocked-pool"
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

      <aside
        v-if="localVideoViewerActive"
        ref="sharedVideoFloatEl"
        class="browser-video-float"
        :class="{ collapsed: videoFloatCollapsed, cropping: videoCropPanelOpen, portrait: videoCropLayout === 'portrait' }"
        :style="sharedVideoFloatStyle"
      >
        <header class="browser-video-float-head" @pointerdown="startVideoFloatDrag">
          <div>
            <span>LOCAL VIDEO</span>
            <strong>{{ room?.name || '直播画面' }}</strong>
          </div>
          <div class="browser-video-float-actions" @pointerdown.stop>
            <button
              type="button"
              :class="{ active: videoCropPanelOpen }"
              title="调整纯净裁剪"
              @click="videoCropPanelOpen = !videoCropPanelOpen"
            >裁剪</button>
            <button
              type="button"
              :title="videoFloatCollapsed ? '展开观看窗口' : '收起观看窗口'"
              @click="videoFloatCollapsed = !videoFloatCollapsed"
            >{{ videoFloatCollapsed ? '展开' : '收起' }}</button>
            <button type="button" title="停止共享" @click="stopBrowserVideoShare">×</button>
          </div>
        </header>

        <div v-show="!videoFloatCollapsed" class="browser-video-float-body">
          <video ref="sharedVideoEl" autoplay playsinline muted aria-hidden="true"></video>
          <canvas ref="sharedVideoCanvasEl" aria-label="本机共享的直播视频画面"></canvas>
          <div v-if="videoCropPanelOpen" class="browser-video-crop-panel">
            <div class="browser-video-crop-actions">
              <button type="button" @click="applyDouyinCropPreset()">竖屏纯视频</button>
              <button type="button" @click="applyDouyinLandscapeCropPreset">横屏播放器</button>
              <button type="button" @click="resetVideoCrop">还原全页</button>
            </div>
            <label>
              <span>上 {{ videoCrop.top }}%</span>
              <input v-model.number="videoCrop.top" type="range" min="0" max="45" step="1" @input="clampVideoCrop()" />
            </label>
            <label>
              <span>右 {{ videoCrop.right }}%</span>
              <input v-model.number="videoCrop.right" type="range" min="0" max="45" step="1" @input="clampVideoCrop()" />
            </label>
            <label>
              <span>下 {{ videoCrop.bottom }}%</span>
              <input v-model.number="videoCrop.bottom" type="range" min="0" max="45" step="1" @input="clampVideoCrop()" />
            </label>
            <label>
              <span>左 {{ videoCrop.left }}%</span>
              <input v-model.number="videoCrop.left" type="range" min="0" max="45" step="1" @input="clampVideoCrop()" />
            </label>
          </div>
        </div>
      </aside>
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
.live-review-panel.review-body-only { margin-top:14px; }
.live-review-panel.review-body-only .live-review-body { padding:18px; border-top:0; }
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
  justify-content: flex-start;
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
.speech-local-mute-button {
  width:30px;
  height:30px;
  padding:0;
  margin-left:2px;
  display:inline-flex;
  align-items:center;
  justify-content:center;
  border:1px solid rgba(119,145,190,.34);
  border-radius:9px;
  color:#d6e0ef;
  background:rgba(255,255,255,.055);
  cursor:pointer;
  transition:background .18s ease,border-color .18s ease,color .18s ease,transform .18s ease;
}
.speech-local-mute-button:hover {
  color:#fff;
  border-color:rgba(112,210,190,.58);
  background:rgba(63,196,163,.12);
}
.speech-local-mute-button:active { transform:scale(.95); }
.speech-local-mute-button.muted {
  color:#ff9b9b;
  border-color:rgba(255,120,120,.36);
  background:rgba(150,48,58,.18);
}
.speech-local-mute-button svg {
  width:18px;
  height:18px;
  fill:currentColor;
  stroke:currentColor;
  stroke-width:1.8;
  stroke-linecap:round;
  stroke-linejoin:round;
}
.speech-local-mute-button svg path:first-child { stroke:none; }
.speech-runtime-track-grid {
  display: grid !important;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 12px;
  width: 100%;
  transition: grid-template-columns .28s cubic-bezier(.22, .8, .22, 1);
}

.speech-mainline-track-column {
  display:grid;
  grid-template-rows:auto;
  min-width:0;
}

.speech-runtime-track-grid > .speech-interrupt-card {
  align-self:end;
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

.speech-runtime-track-grid.is-mainline-expanded {
  grid-template-columns: minmax(0, 1.75fr) minmax(220px, .55fr);
}

.speech-runtime-track-grid.is-interrupt-expanded {
  grid-template-columns: minmax(220px, .55fr) minmax(0, 1.75fr);
}

.speech-runtime-track-grid.is-mainline-expanded .speech-mainline-card,
.speech-runtime-track-grid.is-interrupt-expanded .speech-interrupt-card {
  height: 270px;
  min-height: 270px;
  max-height: 270px;
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

.public-screen-mode-switch{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin:10px 0 12px;padding:4px;border:1px solid #dbe2ef;border-radius:12px;background:#f5f7fb}.public-screen-mode-switch button{min-height:40px;border:0;border-radius:9px;background:transparent;color:#3e4b63;font-size:15px;font-weight:900;cursor:pointer}.public-screen-mode-switch button.active{background:#fff;color:#4f60ce;box-shadow:0 5px 14px rgba(58,74,132,.12)}.public-screen-mode-switch .mobile-preferences-tab{display:none}.public-event-bucket-view{display:grid;gap:14px;min-height:420px;max-height:1100px;overflow:auto;padding:4px 2px 10px}.public-event-bucket-summary{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.public-event-bucket-summary article{display:flex;align-items:center;justify-content:space-between;gap:8px;padding:12px 13px;border:1px solid #e1e6f0;border-radius:12px;background:#fbfcff;box-shadow:0 5px 14px rgba(55,72,128,.04);transition:transform .16s ease,box-shadow .16s ease}.public-event-bucket-summary article:hover{transform:translateY(-1px);box-shadow:0 8px 18px rgba(55,72,128,.09)}.public-event-bucket-summary span{color:#34435f;font-size:15px;font-weight:850}.public-event-bucket-summary strong{font-size:18px;font-weight:950}.public-event-bucket-summary .tone-entry{border-color:#cce8df;background:linear-gradient(135deg,#f3fbf7,#edf8f4)}.public-event-bucket-summary .tone-entry:hover{border-color:#8fcfb9;background:linear-gradient(135deg,#e6f8f0,#dff4eb)}.public-event-bucket-summary .tone-entry strong{color:#2d8b6b}.public-event-bucket-summary .tone-chat{border-color:#d9def9;background:linear-gradient(135deg,#f7f7ff,#f0f2ff)}.public-event-bucket-summary .tone-chat:hover{border-color:#aeb8ef;background:linear-gradient(135deg,#ecefff,#e4e8ff)}.public-event-bucket-summary .tone-chat strong{color:#5a67d8}.public-event-bucket-summary .tone-like{border-color:#f2dfbd;background:linear-gradient(135deg,#fffaf0,#fff5df)}.public-event-bucket-summary .tone-like:hover{border-color:#e7bf79;background:linear-gradient(135deg,#fff3d8,#ffedc8)}.public-event-bucket-summary .tone-like strong{color:#c6842e}.public-event-bucket-summary .tone-follow{border-color:#cfe3f6;background:linear-gradient(135deg,#f3f9ff,#edf6ff)}.public-event-bucket-summary .tone-follow:hover{border-color:#99c7ed;background:linear-gradient(135deg,#e9f5ff,#dfefff)}.public-event-bucket-summary .tone-follow strong{color:#3f82bd}.public-event-bucket-summary .tone-gift{border-color:#edd5f2;background:linear-gradient(135deg,#fff6ff,#f9effc)}.public-event-bucket-summary .tone-gift:hover{border-color:#d7a9e2;background:linear-gradient(135deg,#fbedff,#f3e3f8)}.public-event-bucket-summary .tone-gift strong{color:#9a63b5}.public-event-bucket-summary .tone-order{border-color:#f1d1d4;background:linear-gradient(135deg,#fff6f6,#fff0f1)}.public-event-bucket-summary .tone-order:hover{border-color:#e7a2a9;background:linear-gradient(135deg,#ffecee,#ffe2e5)}.public-event-bucket-summary .tone-order strong{color:#c55461}.public-question-buckets{display:grid;gap:9px}.public-question-buckets>header{display:flex;align-items:center;justify-content:space-between;gap:12px;padding-top:2px}.public-question-buckets>header strong{color:#2d3a55;font-size:17px;font-weight:950}.public-question-buckets>header span{color:#53627c;font-size:14px;font-weight:800}.public-question-bucket{border:1px solid #dfe5f2;border-radius:12px;background:#fff;overflow:hidden;box-shadow:0 5px 14px rgba(55,72,128,.035);transform:translateY(0);transition:transform .2s ease,border-color .2s ease,box-shadow .2s ease}.public-question-bucket:hover{transform:translateY(-3px);border-color:#bfc9ee;box-shadow:0 14px 28px rgba(66,82,146,.13)}.public-question-bucket-head{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:8px}.public-question-bucket-main{display:flex;align-items:center;justify-content:space-between;gap:12px;width:100%;padding:12px 13px;border:0;background:linear-gradient(90deg,#fff,#fafbff);text-align:left;cursor:pointer;transition:background .2s ease,padding-left .2s ease}.public-question-bucket:hover .public-question-bucket-main{padding-left:17px;background:linear-gradient(90deg,#edf2ff,#fff)}.public-question-bucket-main>span{display:grid;gap:4px;min-width:0}.public-question-bucket-main strong{color:#2f3b56;font-size:15px;font-weight:950}.public-question-bucket-main small{color:#56647c;font-size:13px;font-weight:700}.public-question-bucket-main>b{flex:0 0 auto;padding:5px 8px;border-radius:999px;background:#eef1ff;color:#5967cc;font-size:13px;transform:scale(1);transition:transform .18s ease,box-shadow .18s ease}.public-question-bucket:hover .public-question-bucket-main>b{transform:scale(1.08);box-shadow:0 5px 12px rgba(83,99,201,.16)}.public-question-bucket.tone-hot{border-color:#f0cfd3}.public-question-bucket.tone-hot .public-question-bucket-main{background:linear-gradient(90deg,#fff5f6,#fff)}.public-question-bucket.tone-hot:hover{border-color:#df8f99;background:#fff3f4;box-shadow:0 14px 30px rgba(186,75,89,.16)}.public-question-bucket.tone-hot:hover .public-question-bucket-main{background:linear-gradient(90deg,#ffe6e9,#fff5f6)}.public-question-bucket.tone-hot .public-question-bucket-main>b{background:#ffe7e9;color:#c8515f}.public-question-bucket.tone-warm{border-color:#f0dfbf}.public-question-bucket.tone-warm .public-question-bucket-main{background:linear-gradient(90deg,#fff9ee,#fff)}.public-question-bucket.tone-warm:hover{border-color:#ddb66c;background:#fff9ed;box-shadow:0 14px 30px rgba(181,126,46,.15)}.public-question-bucket.tone-warm:hover .public-question-bucket-main{background:linear-gradient(90deg,#ffefcf,#fff9ee)}.public-question-bucket.tone-warm .public-question-bucket-main>b{background:#fff0d5;color:#b77a28}.public-question-bucket.tone-cool{border-color:#d6def8}.public-question-bucket.tone-cool .public-question-bucket-main{background:linear-gradient(90deg,#f6f8ff,#fff)}.public-question-bucket.tone-cool:hover{border-color:#aab8ee;background:#f4f6ff;box-shadow:0 14px 30px rgba(77,94,186,.15)}.public-question-bucket.tone-cool:hover .public-question-bucket-main{background:linear-gradient(90deg,#e8edff,#f9faff)}.public-question-bucket.tone-cool .public-question-bucket-main>b{background:#e9edff;color:#5969d2}.public-question-list{display:grid;gap:8px;padding:0 12px 12px}.public-question-item{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:8px;padding:10px 11px;border:1px solid transparent;border-radius:10px;background:#f7f9fc;transform:translateX(0);transition:transform .18s ease,background .18s ease,box-shadow .18s ease,border-color .18s ease}.public-question-item:hover{transform:translateX(5px);border-color:#cbd4ef;background:#edf2ff;box-shadow:0 8px 18px rgba(66,82,146,.11)}.public-question-item-copy{min-width:0;cursor:pointer}.public-question-list span{color:#56647a;font-size:12px;font-weight:800}.public-question-list p{margin:5px 0 0;color:#2f3a50;font-size:14px;line-height:1.55}.public-question-actions{display:flex;align-items:center;gap:6px;padding-right:10px;opacity:.18;transform:translateX(4px);transition:opacity .18s ease,transform .18s ease}.public-question-bucket:hover>.public-question-bucket-head .public-question-actions,.public-question-bucket:focus-within>.public-question-bucket-head .public-question-actions,.public-question-item:hover .public-question-actions,.public-question-item:focus-within .public-question-actions{opacity:1;transform:translateX(0)}.public-question-actions button{min-width:46px;min-height:32px;padding:5px 9px;border-radius:8px;font:inherit;font-size:12px;font-weight:900;cursor:pointer;transition:transform .15s ease,box-shadow .15s ease,filter .15s ease}.public-question-actions button:hover:not(:disabled){transform:translateY(-1px);filter:saturate(1.08)}.public-question-actions .correct{border:1px solid #c5c9ee;background:#f0f1ff;color:#5962b2}.public-question-actions .quick{border:1px solid #efc58e;background:#fff1df;color:#b76b23}.public-question-actions .answer{border:1px solid #a9d8c5;background:#e9f8f1;color:#287858}.public-question-actions .correct:hover:not(:disabled){box-shadow:0 6px 14px rgba(89,98,178,.16)}.public-question-actions .quick:hover:not(:disabled){box-shadow:0 6px 14px rgba(183,107,35,.16)}.public-question-actions .answer:hover:not(:disabled){box-shadow:0 6px 14px rgba(40,120,88,.16)}.public-question-actions button:disabled{opacity:.42;cursor:not-allowed;transform:none}.public-question-actions.item-actions{padding-right:0}.public-question-action-message{margin:0 12px 12px;padding:8px 10px;border-radius:9px;background:#edf3ff;color:#4960a8;font-size:13px;font-weight:800}.room-middle-interaction-preferences{width:100%}.mobile-public-interaction-preferences{display:none}

.interaction-execution-view {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto;
  gap: 11px;
  min-height: 0;
  max-height: 100%;
  overflow-y: auto;
  overflow-x: hidden;
  padding: 4px 2px 10px;
}
.interaction-execution-summary {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid #e2e7f1;
  border-radius: 12px;
  background: #f8faff;
}
.interaction-execution-summary > span {
  padding: 5px 9px;
  border-radius: 999px;
  color: #56627a;
  background: #eef1f8;
  font-size: 12px;
  font-weight: 850;
}
.interaction-execution-summary > span:first-child {
  color: #20765d;
  background: #e8f7f1;
}
.interaction-execution-summary b { font-size: 14px; }
.interaction-execution-summary small {
  flex: 1 1 100%;
  color: #8a94a7;
  font-size: 11px;
}
.interaction-execution-list {
  display: grid;
  align-content: start;
  gap: 9px;
  min-height: 0;
  overflow: visible;
  padding-right: 2px;
}
.interaction-execution-item {
  display: grid;
  grid-template-columns: 48px minmax(0, 1fr) auto;
  align-items: start;
  gap: 10px;
  padding: 11px 10px;
  border: 1px solid #e1e6ef;
  border-radius: 12px;
  background: #fff;
  box-shadow: 0 5px 15px rgba(52, 66, 112, .045);
}
.interaction-execution-item.running {
  border-color: #a7d9c7;
  background: linear-gradient(135deg, #f0fbf7, #fff);
  box-shadow: inset 3px 0 0 #45b58e, 0 8px 18px rgba(53, 132, 106, .08);
}
.interaction-execution-item.manual:not(.running) {
  border-color: #cfd4f5;
  background: linear-gradient(135deg, #f7f7ff, #fff);
}
.interaction-execution-order {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 34px;
  border-radius: 9px;
  color: #69758b;
  background: #f1f3f8;
  font-size: 11px;
}
.interaction-execution-item.running .interaction-execution-order {
  color: #28795f;
  background: #dff4eb;
}
.interaction-execution-copy { display: grid; gap: 5px; min-width: 0; }
.interaction-execution-copy > header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.interaction-execution-copy > header time { color: #9aa3b4; font-size: 10px; }
.interaction-execution-type {
  display: inline-flex;
  width: fit-content;
  padding: 3px 7px;
  border-radius: 999px;
  color: #5966c7;
  background: #edf0ff;
  font-size: 10px;
  font-weight: 900;
}
.interaction-execution-item.running .interaction-execution-type {
  color: #2c7b61;
  background: #e1f5ed;
}
.interaction-execution-copy > strong {
  color: #333f59;
  font-size: 14px;
  line-height: 1.4;
  overflow-wrap: anywhere;
}
.interaction-execution-copy > p {
  margin: 0;
  color: #68758c;
  font-size: 12px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}
.interaction-execution-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 10px;
  color: #9099aa;
  font-size: 10px;
  font-weight: 750;
}
.interaction-execution-meta .danger { color: #c4555d; }
.interaction-execution-delete {
  align-self: center;
  min-width: 48px;
  min-height: 30px;
  padding: 5px 8px;
  border: 1px solid #ead6d9;
  border-radius: 8px;
  color: #a9555e;
  background: #fff8f8;
  font: inherit;
  font-size: 11px;
  font-weight: 850;
  cursor: pointer;
}
.interaction-execution-delete:hover:not(:disabled) { background: #fff0f1; }
.interaction-execution-delete:disabled { opacity: .38; cursor: not-allowed; }
.interaction-execution-message {
  padding: 8px 10px;
  border-radius: 9px;
  color: #4960a8;
  background: #edf3ff;
  font-size: 12px;
  font-weight: 800;
}
.interaction-execution-empty { min-height: 250px; }
.room-detail-page .public-screen-panel {
  display: flex;
  flex-direction: column;
}
.room-detail-page .public-screen-panel > .event-list,
.room-detail-page .public-screen-panel > .public-event-bucket-view,
.room-detail-page .public-screen-panel > .interaction-execution-view {
  flex: 1 1 auto;
  min-height: 0;
}
.room-detail-page .public-screen-panel > .public-event-bucket-view {
  min-height: 0;
  max-height: none;
  overflow-y: auto;
  overflow-x: hidden;
}
.room-detail-page .public-screen-panel > .interaction-execution-view {
  overflow-y: auto;
  overflow-x: hidden;
}
.speech-runtime-unified{display:grid;gap:13px;min-height:190px;padding:18px;border:1px solid rgba(126,151,232,.22);border-radius:16px;background:linear-gradient(180deg,rgba(24,31,49,.98),rgba(18,24,39,.98));box-shadow:inset 0 1px 0 rgba(255,255,255,.03)}
.speech-runtime-unified .speech-mainline-live-caption{min-height:94px}
.speech-runtime-unified-mainline{margin:0;color:#edf2ff;font-size:19px;font-weight:850;line-height:1.7}
.speech-runtime-unified-wave{display:flex;align-items:center;justify-content:center;gap:3px;height:38px;overflow:hidden}.speech-runtime-unified-wave i{display:block;width:3px;height:var(--wave-height);max-height:32px;border-radius:999px;background:#6978dd;opacity:.45;transform:scaleY(.45);transform-origin:center;transition:.18s ease}.speech-runtime-unified-wave.active i{opacity:.9;animation:speech-wave-pulse .78s ease-in-out infinite alternate;animation-delay:var(--wave-delay)}
@media (hover:none){.public-question-actions{opacity:1;transform:none}}
@media (max-width:900px){.public-screen-mode-switch{grid-template-columns:repeat(4,minmax(0,1fr));position:sticky;top:0;z-index:2;margin-top:6px}.public-screen-mode-switch .mobile-preferences-tab{display:block}.room-middle-interaction-preferences{display:none}.mobile-public-interaction-preferences{display:grid}.public-screen-mode-switch button{min-height:44px;font-size:14px}.speech-runtime-unified{padding:14px}.speech-runtime-unified-interrupt{font-size:17px}}

.speech-track-head-actions {
  display: flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
}

.speech-track-size-button {
  height: 24px;
  padding: 0 8px;
  border: 1px solid rgba(151, 169, 220, .28);
  border-radius: 7px;
  color: inherit;
  background: rgba(255,255,255,.08);
  font: inherit;
  font-size: 9px;
  font-weight: 900;
  cursor: pointer;
  opacity: .78;
  transition: opacity .16s ease, transform .16s ease, background .16s ease;
}

.speech-track-size-button:hover {
  opacity: 1;
  transform: translateY(-1px);
}

.speech-interrupt-card .speech-track-size-button {
  border-color: #d9dfeb;
  background: #fff;
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

.speech-interrupt-card > header .speech-trigger-question {
  min-width: 0;
  color: #d83f4f;
  font-size: 15px;
  font-weight: 900;
  line-height: 1.35;
  letter-spacing: 0;
  overflow-wrap: anywhere;
}

.speech-trigger-question > strong {
  color: inherit;
  font-weight: 900;
}

.speech-interrupt-placeholder {
  color: #7f899d;
}

.room-detail-page .speech-track-card > p {
  min-height: 0;
  margin: 0;
  padding-right: 5px;
  color: inherit;
  font-size: 18px;
  font-weight: 800;
  line-height: 1.65;
  white-space: normal;
  overflow-wrap: anywhere;
  overflow-y: auto;
  overscroll-behavior: contain;
  scrollbar-width: thin;
}

.speech-interrupt-copy .speech-bridge-text {
  margin-left: .12em;
  color: #e23b4a;
  font-weight: 950;
  text-shadow: 0 0 10px rgba(226, 59, 74, .12);
}

.speech-mainline-card {
	grid-template-rows: auto minmax(0, 1fr) auto;
}

.speech-mainline-program-meta {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 12px;
	min-width: 0;
	padding: 7px 9px;
	border: 1px solid rgba(126, 151, 232, .20);
	border-radius: 9px;
	background: rgba(105, 127, 210, .10);
}

.speech-mainline-program-meta strong {
	min-width: 0;
	color: #cbd5ff;
	font-size: 10px;
	font-weight: 900;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}

.speech-mainline-program-meta span {
	flex: 0 0 auto;
	color: #8ee3c4;
	font-size: 9px;
	font-weight: 800;
}

.speech-mainline-program-meta span.warning {
  color: #ffd765;
  font-weight: 950;
}

.speech-mainline-live-caption {
  display: grid;
  align-content: center;
  gap: 8px;
  min-height: 0;
  padding: 4px 2px;
  overflow: hidden;
}

.speech-mainline-live-caption > small {
  color: #7de0bd;
  font-size: 9px;
  font-weight: 950;
  letter-spacing: .08em;
}

.speech-mainline-live-caption > p {
  margin: 0;
  color: #d6deef;
  font-size: 20px;
  font-weight: 820;
  line-height: 1.72;
  overflow: hidden;
  overflow-wrap: anywhere;
  text-wrap: pretty;
}

.speech-mainline-live-caption mark {
  padding: 1px 2px;
  border-radius: 4px;
  color: #ffffff;
  background: rgba(87, 208, 167, .28);
  box-shadow: 0 0 0 1px rgba(104, 229, 187, .18);
  font-weight: 950;
}

.speech-mainline-subtitle-progress {
  height: 3px;
  margin: 1px 0 0;
  border-radius: 999px;
  background: rgba(135, 153, 202, .18);
  overflow: hidden;
}

.speech-mainline-subtitle-progress i {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg,#6c80ff,#59d6ae);
  transition: width .25s linear;
}

.speech-mainline-segment-state {
	display: flex;
	align-items: center;
	gap: 9px;
	min-width: 0;
	color: rgba(220, 229, 250, .68);
	font-size: 9px;
}

.speech-mainline-segment-state span { font-weight: 900; }
.speech-mainline-segment-state time { font-variant-numeric: tabular-nums; }
.speech-mainline-segment-state b {
	margin-left: auto;
	color: #85ddbd;
	font-size: 9px;
	font-weight: 900;
	white-space: nowrap;
}

.speech-mainline-segment-state b.warning { color: #ffd765; }

.speech-mainline-card.cut-imminent {
  border-color: rgba(255, 205, 70, .82);
  box-shadow: 0 0 0 2px rgba(255, 205, 70, .10), 0 12px 30px rgba(155, 113, 11, .18);
}

.speech-mainline-card.cut-imminent::before {
  border-color: rgba(255, 205, 70, .66) !important;
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

.speech-interrupt-card {
  grid-template-rows: auto minmax(0, 1fr) auto;
}
.speech-interrupt-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}
.speech-interrupt-footer > time {
  color: inherit;
  opacity: .48;
  font-size: 9px;
}
.speech-interrupt-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-left: auto;
}
.speech-interrupt-actions button {
  min-width: 68px;
  height: 32px;
  padding: 0 13px;
  border: 1px solid rgba(82, 97, 196, .22);
  border-radius: 10px;
  background: #5564d9;
  color: #fff;
  font-size: 13px;
  font-weight: 850;
  cursor: pointer;
  box-shadow: 0 6px 16px rgba(78, 92, 194, .18);
}
.speech-interrupt-actions button.history {
  background: #f0f3fb;
  color: #445172;
  box-shadow: none;
}
.speech-interrupt-actions button:disabled {
  cursor: not-allowed;
  opacity: .42;
}

.speech-history-mask {
  position: fixed;
  inset: 0;
  z-index: 1300;
  display: grid;
  place-items: center;
  padding: 28px;
  background: rgba(17, 27, 48, .46);
  backdrop-filter: blur(10px);
}
.speech-history-dialog {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr) auto;
  gap: 18px;
  width: min(980px, 94vw);
  height: min(900px, 92vh);
  max-height: 92vh;
  padding: 24px;
  border: 1px solid rgba(112, 128, 196, .24);
  border-radius: 24px;
  background: #f9fbff;
  box-shadow: 0 34px 90px rgba(18, 31, 68, .28);
}
.speech-history-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 18px;
}
.speech-history-head > div {
  display: grid;
  gap: 4px;
}
.speech-history-head span {
  color: #6d79c9;
  font-size: 11px;
  font-weight: 900;
  letter-spacing: .14em;
}
.speech-history-head strong {
  color: #263353;
  font-size: 25px;
}
.speech-history-head small {
  color: #8791a8;
  font-size: 13px;
}
.speech-history-close {
  width: 38px;
  height: 38px;
  border: 0;
  border-radius: 12px;
  background: #eef1f8;
  color: #5a6785;
  font-size: 25px;
  line-height: 1;
  cursor: pointer;
}
.speech-history-search {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
}
.speech-history-search input {
  min-width: 0;
  height: 44px;
  padding: 0 14px;
  border: 1px solid rgba(119, 132, 185, .24);
  border-radius: 12px;
  outline: none;
  background: #fff;
  color: #2d3957;
  font-size: 14px;
}
.speech-history-search input:focus {
  border-color: rgba(83, 100, 217, .52);
  box-shadow: 0 0 0 3px rgba(83, 100, 217, .09);
}
.speech-history-search button,
.speech-history-pagination button,
.speech-history-item-actions button {
  border: 1px solid rgba(82, 97, 196, .2);
  border-radius: 10px;
  background: #5867db;
  color: #fff;
  font-weight: 800;
  cursor: pointer;
}
.speech-history-search button {
  min-width: 78px;
  padding: 0 18px;
}
.speech-history-list {
  display: grid;
  gap: 11px;
  min-height: 0;
  overflow-y: auto;
  padding-right: 4px;
}
.speech-history-item {
  display: grid;
  gap: 10px;
  padding: 16px;
  border: 1px solid rgba(133, 146, 194, .18);
  border-radius: 16px;
  background: #fff;
  box-shadow: 0 5px 16px rgba(27, 42, 83, .05);
}
.speech-history-item-meta {
  display: flex;
  align-items: center;
  gap: 9px;
  color: #8b94a8;
  font-size: 12px;
}
.speech-history-source {
  padding: 4px 8px;
  border-radius: 999px;
  background: #edf0ff;
  color: #5261c8;
  font-weight: 850;
}
.speech-history-correction {
  margin-left: auto;
  font-weight: 800;
}
.speech-history-correction.status-adopted { color: #17845e; }
.speech-history-correction.status-editing { color: #a66a00; }
.speech-history-question,
.speech-history-reply {
  margin: 0;
  overflow-wrap: anywhere;
}
.speech-history-question {
  color: #697590;
  font-size: 13px;
}
.speech-history-reply {
  color: #2e3954;
  font-size: 16px;
  font-weight: 700;
  line-height: 1.7;
}
.speech-history-item-actions {
  display: flex;
  justify-content: flex-end;
}
.speech-history-item-actions button {
  min-width: 72px;
  height: 34px;
}
.speech-history-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  color: #7f899f;
  font-size: 13px;
}
.speech-history-pagination-summary {
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: nowrap;
  white-space: nowrap;
}
.speech-history-page-size {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  flex: 0 0 auto;
  color: #7f899f;
  font-size: 13px;
  white-space: nowrap;
}
.speech-history-page-size > span {
  white-space: nowrap;
}
.speech-history-page-size select {
  height: 34px;
  min-width: 64px;
  padding: 0 26px 0 10px;
  border: 1px solid rgba(82, 97, 196, .2);
  border-radius: 10px;
  outline: none;
  background: #f0f3fb;
  color: #4c5877;
  font-size: 13px;
  font-weight: 800;
  cursor: pointer;
}
.speech-history-page-size select:disabled {
  cursor: not-allowed;
  opacity: .45;
}
.speech-history-page-buttons {
  display: flex;
  gap: 6px;
}
.speech-history-pagination button {
  min-width: 38px;
  height: 34px;
  padding: 0 10px;
  background: #f0f3fb;
  color: #4c5877;
}
.speech-history-pagination button.page-number.active {
  background: #5867db;
  color: #fff;
}
.speech-history-pagination button:disabled,
.speech-history-search button:disabled {
  cursor: not-allowed;
  opacity: .45;
}
.speech-history-error,
.speech-history-empty {
  display: grid;
  place-items: center;
  min-height: 180px;
  border: 1px dashed rgba(128, 142, 190, .25);
  border-radius: 16px;
  color: #8993aa;
  background: #fff;
}

.speech-track-card.status-playing { border-color: rgba(73, 201, 155, .48); }
.speech-track-card.status-playing > header b { color: #198b67; background: #dcf8ee; }
.speech-mainline-card.status-playing > header b { color: #80e4bf; background: rgba(44, 179, 132, .18); }
.speech-mainline-card {
  grid-template-rows: minmax(0, 1fr) 28px auto;
}
.speech-mainline-card.status-playing {
  border-color: rgba(205, 100, 100, .58);
  background: linear-gradient(145deg, rgba(77, 29, 33, .96), rgba(46, 23, 31, .98));
  box-shadow: inset 0 1px 0 rgba(255,255,255,.035), 0 12px 30px rgba(79, 24, 29, .16);
}
.speech-mainline-live-caption {
  align-content: start;
  gap: 5px;
  padding: 0 2px;
}
.speech-mainline-caption-stack {
  display: grid;
  grid-template-rows: minmax(0, .82fr) minmax(0, 1.22fr) minmax(0, .82fr);
  gap: 2px;
  min-height: 0;
  overflow: hidden;
}
.speech-mainline-caption-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  align-items: center;
  gap: 10px;
  min-width: 0;
  padding: 3px 7px;
  border: 1px solid transparent;
  border-radius: 8px;
  color: #cbd4e7;
  font-size: 14px;
  font-weight: 760;
  line-height: 1.35;
  opacity: .38;
  transform: translateY(-2px) scale(.98);
  transition: opacity .28s ease, transform .28s ease, color .28s ease, background .28s ease;
}
.speech-mainline-caption-row > span {
  width: 100%;
  min-width: 0;
  overflow: visible;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
  text-align: center;
}
.speech-mainline-caption-row.role-current {
  color: #f2f5fc;
  font-size: 18px;
  font-weight: 900;
  opacity: 1;
  transform: translateY(0) scale(1);
}
.speech-mainline-caption-row.role-current.transition-stop,
.speech-mainline-caption-row.role-current.transition-resume,
.speech-mainline-caption-row.role-current.transition-interrupt {
  font-size: 18px;
  line-height: 1.62;
  opacity: 1;
  transform: translateY(0) scale(1);
}
.speech-mainline-caption-row.role-previous.transition-stop,
.speech-mainline-caption-row.role-previous.transition-interrupt,
.speech-mainline-caption-row.role-previous.transition-resume,
.speech-mainline-caption-row.role-next.transition-stop,
.speech-mainline-caption-row.role-next.transition-interrupt,
.speech-mainline-caption-row.role-next.transition-resume {
  background: transparent;
  border-color: transparent;
}
.speech-mainline-caption-row.role-next {
  opacity: .24;
  transform: translateY(2px) scale(.98);
}
.speech-mainline-caption-row.transition-stop {
  border-color: transparent;
  background: transparent;
  color: #ffd86f;
}
.speech-mainline-caption-row.transition-resume {
  border-color: transparent;
  background: transparent;
  color: #7de0bd;
}
.speech-mainline-caption-row.transition-interrupt {
  border-color: transparent;
  background: transparent;
  color: #ff7f8b;
  font-weight: 950;
}
.speech-mainline-caption-row > b {
  flex: 0 0 auto;
  padding: 2px 6px;
  border-radius: 999px;
  font-size: 9px;
  font-weight: 950;
  white-space: nowrap;
}
.speech-mainline-caption-row.transition-stop > b {
  color: #6f4b00;
  background: #ffe39a;
}
.speech-mainline-caption-row.transition-resume > b {
  color: #0e6549;
  background: #a9efd2;
}
.speech-mainline-waveform {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 3px;
  min-width: 0;
  height: 26px;
  padding: 0 8px;
  overflow: hidden;
  opacity: .22;
  transition: opacity .2s ease;
}
.speech-mainline-waveform::before,
.speech-mainline-waveform::after {
  content: "";
  flex: 1 1 auto;
  height: 2px;
  min-width: 26px;
  border-radius: 999px;
  background: rgba(116, 139, 192, .22);
}
.speech-mainline-waveform i {
  display: block;
  width: 3px;
  height: 4px;
  flex: 0 0 3px;
  border-radius: 999px;
  background: linear-gradient(180deg, #74c9ff 0%, #55e2c0 100%);
  box-shadow: 0 0 7px rgba(85, 226, 192, .16);
  transform-origin: 50% 50%;
}
.speech-mainline-waveform.active {
  opacity: .9;
}
.speech-mainline-waveform.active i {
  animation: speech-wave-pulse .78s ease-in-out infinite alternate;
  animation-delay: var(--wave-delay);
}
@keyframes speech-wave-pulse {
  from { height: 4px; opacity: .52; }
  to { height: var(--wave-height); opacity: 1; }
}
.speech-mainline-card.status-playing::before {
  border-color: rgba(219, 111, 111, .34);
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
  .speech-mainline-waveform.active i {
    animation: none;
    height: min(var(--wave-height), 14px);
  }
}

@media (max-width: 900px) {
  .speech-runtime-head { align-items: flex-start; flex-direction: column; gap: 6px; }
  .speech-runtime-mainline-tools { margin-left: 0; }
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

.dashboard-collector-toggle {
  font-family: inherit;
  cursor: pointer;
  transition: border-color .18s ease, background .18s ease, color .18s ease, box-shadow .18s ease, transform .18s ease;
}

.dashboard-collector-toggle:not(:disabled):hover {
  transform: translateY(-1px);
  box-shadow: 0 7px 18px rgba(72, 88, 150, .12);
}

.dashboard-collector-toggle:disabled {
  cursor: default;
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

.dashboard-live-state.live {
  border-color: rgba(54, 185, 140, .28);
  color: #52647a;
  background: rgba(255, 255, 255, .82);
}

.dashboard-live-state.live i {
  background: #36b98c;
  box-shadow: 0 0 0 4px rgba(54, 185, 140, 0.14), 0 0 12px rgba(54, 185, 140, 0.4);
}

.dashboard-collector-toggle.live:not(:disabled):hover {
  border-color: rgba(219, 87, 98, .30);
  color: #b64855;
  background: #fff5f6;
}

.dashboard-collector-toggle.live:not(:disabled):hover i {
  background: #df5b68;
  box-shadow: 0 0 0 4px rgba(223, 91, 104, .12);
}

.dashboard-live-state.connecting {
  border-color: rgba(100, 116, 206, .22);
  color: #6370a9;
  background: #f5f6ff;
}

.dashboard-live-state.connecting i {
  background: #7180d3;
  box-shadow: 0 0 0 4px rgba(113, 128, 211, .12);
  animation: dashboard-collector-pulse 1.2s ease-in-out infinite;
}

.dashboard-live-state.stopped {
  border-color: rgba(100, 116, 206, .20);
  color: #6170b4;
  background: #f7f8ff;
}

.dashboard-live-state.stopped i {
  background: #8290d9;
  box-shadow: 0 0 0 4px rgba(130, 144, 217, .12);
}

.dashboard-live-state.error,
.dashboard-live-state.offline {
  border-color: rgba(205, 91, 103, .18);
  color: #a9525d;
  background: #fff7f8;
}

.dashboard-live-state.error i,
.dashboard-live-state.offline i {
  background: #d36a75;
  box-shadow: 0 0 0 4px rgba(211, 106, 117, .11);
}

@keyframes dashboard-collector-pulse {
  0%, 100% { opacity: .55; transform: scale(.9); }
  50% { opacity: 1; transform: scale(1.08); }
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
  grid-template-columns: 128px minmax(160px, 1fr) auto auto;
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

.dashboard-review-button {
  align-self:end;
  min-height:34px;
  padding:0 14px;
  border:1px solid rgba(82,101,225,.2);
  border-radius:10px;
  background:#f5f7ff;
  color:#5261cc;
  font:inherit;
  font-size:10px;
  font-weight:850;
  white-space:nowrap;
  cursor:pointer;
}
.dashboard-review-button:hover { background:#eef1ff; }
.dashboard-review-button:disabled { opacity:.55; cursor:wait; }

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
  align-items: start;
  cursor: context-menu;
  transition: background 0.16s ease, box-shadow 0.16s ease;
}

.room-detail-page .event-row.has-ai-actions {
  grid-template-columns: 64px 54px minmax(0, 1fr);
  align-items: start;
}

.room-detail-page .event-row > time,
.room-detail-page .event-row > .event-type {
  margin-top: 2px;
}
.room-detail-page .event-row > time { font-size: 11px; }
.room-detail-page .event-row > .event-type { font-size: 11px; }

.event-stacked {
  display: grid;
  gap: 7px;
}

.event-topline {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  min-width: 0;
}

.event-topline.actions-only {
  justify-content: flex-end;
}

.event-message-line {
  display: flex;
  align-items: baseline;
  gap: 4px;
  width: 100%;
  min-width: 0;
}

.event-message-line .event-user {
  flex: 0 0 auto;
  max-width: 42%;
  white-space: nowrap;
  overflow-wrap: normal;
  word-break: keep-all;
  overflow: hidden;
  text-overflow: ellipsis;
}

.event-message-line .event-action {
  flex: 1 1 auto;
  width: auto;
  min-width: 0;
}

.event-ai-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: flex-start;
  justify-content: flex-end;
  gap: 6px;
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
.event-ai-actions button.reference {
  color: #5965b9;
  border-color: rgba(92,105,213,.24);
  background: #f1f3ff;
}
.event-ai-actions button:disabled {
  opacity: .5;
  cursor: not-allowed;
}
.event-ai-feedback {
  display: block;
  max-width: 100%;
  font-size: 10px;
  line-height: 1.25;
}
.event-ai-feedback.ok { color: #3d8a6e; }
.event-ai-feedback.error { color: #c84d59; }

.room-detail-page .event-body {
  min-width: 0;
  max-width: 100%;
}

.room-detail-page .event-user,
.room-detail-page .event-action {
  max-width: 100%;
  white-space: normal;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.room-detail-page .event-user {
  display: block;
  min-width: 0;
  flex: 0 1 auto;
  white-space: nowrap;
  overflow-wrap: normal;
  word-break: keep-all;
  overflow: hidden;
  text-overflow: ellipsis;
}
.room-detail-page .event-user { font-size: 13px; }

.room-detail-page .event-action {
  display: block;
  width: 100%;
  line-height: 1.7;
}
.room-detail-page .event-action { font-size: 13px; }
.room-detail-page .event-ai-feedback { font-size: 9px; }

.room-detail-page .event-row.is-hovered {
  position: relative;
  z-index: 2;
  background: linear-gradient(90deg, rgba(111, 125, 255, 0.09), rgba(86, 190, 232, 0.035));
  box-shadow: inset 3px 0 0 rgba(91, 105, 235, 0.78);
}

.agent-decision-panel {
  --agent-thinking-height: 150px;
  position: relative;
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
.agent-decision-head-actions { display:flex; align-items:center; gap:6px; }
.speech-mission-toggle { border:1px solid rgba(99,112,190,.18); border-radius:999px; padding:5px 8px; background:#f3f5fb; color:#6d7894; font-size:9px; font-weight:900; cursor:pointer; }
.speech-mission-toggle.active { color:#4f58bd; background:#e9ebff; border-color:rgba(87,95,207,.28); box-shadow:0 5px 14px rgba(77,84,190,.12); }
.speech-mission-board { position:absolute; z-index:20; top:44px; left:9px; right:9px; bottom:27px; display:grid; align-content:start; gap:8px; padding:10px; overflow:auto; border:1px solid rgba(91,105,205,.22); border-radius:14px; background:rgba(249,250,255,.98); box-shadow:0 16px 38px rgba(38,48,105,.18); backdrop-filter:blur(10px); }
.speech-mission-board > header { display:flex; align-items:flex-start; justify-content:space-between; gap:8px; padding-bottom:7px; border-bottom:1px solid rgba(112,124,178,.13); }
.speech-mission-title { display:grid; gap:2px; min-width:0; }
.speech-mission-board > header small { color:#9aa3b6; font-size:8px; font-weight:800; overflow-wrap:anywhere; }
.speech-mission-board > header strong { color:#35405f; font-size:14px; }
.speech-mission-board > header time { color:#9ca6b8; font-size:8px; }
.speech-mission-header-actions { display:grid; justify-items:end; gap:4px; min-width:0; }
.speech-mission-header-actions select { width:min(190px,48vw); min-height:25px; padding:2px 22px 2px 7px; border:1px solid rgba(111,124,183,.18); border-radius:7px; outline:none; background:#fff; color:#65708b; font-size:8px; font-weight:800; }
.speech-mission-header-actions select:focus { border-color:rgba(86,100,211,.45); box-shadow:0 0 0 2px rgba(86,100,211,.08); }
.speech-mission-flow { display:flex; flex-wrap:wrap; align-items:center; gap:4px; padding:7px; border:1px solid rgba(106,121,190,.14); border-radius:10px; background:linear-gradient(135deg,#f8f9ff,#f1f5fb); }
.speech-mission-flow > span { flex:1 1 62px; min-width:0; display:grid; gap:1px; padding:4px 5px; border-radius:7px; background:rgba(255,255,255,.82); box-shadow:inset 0 0 0 1px rgba(119,132,184,.08); }
.speech-mission-flow > span small { color:#9aa4b6; font-size:7px; font-weight:850; }
.speech-mission-flow > span b { color:#4c5875; font-size:9px; line-height:1.25; overflow-wrap:anywhere; }
.speech-mission-flow > i { flex:0 0 auto; color:#a8b1c5; font-size:8px; font-style:normal; font-weight:900; }
.speech-mission-grid { display:grid; grid-template-columns:1fr 1fr; gap:6px; }
.speech-mission-grid article { min-width:0; display:grid; gap:2px; padding:7px; border:1px solid rgba(116,129,184,.13); border-radius:10px; background:white; }
.speech-mission-grid span,.speech-mission-context > span { color:#8d98ad; font-size:8px; font-weight:900; }
.speech-mission-grid b { color:#46516d; font-size:10px; line-height:1.35; overflow-wrap:anywhere; }
.speech-mission-grid small { color:#9aa4b6; font-size:8px; line-height:1.35; }
.speech-mission-context { display:grid; gap:3px; padding:7px 8px; border-radius:10px; background:#f2f5fb; }
.speech-mission-context.decision-context { background:linear-gradient(135deg,#fff8e9,#fffdf7); box-shadow:inset 3px 0 0 #e8b65b; }
.speech-mission-context.final-text { background:#edf8f3; }
.speech-mission-context p { margin:0; color:#5f6a82; font-size:9px; line-height:1.55; overflow-wrap:anywhere; }
.speech-mission-meta { display:flex; flex-wrap:wrap; gap:5px; }
.speech-mission-meta span { padding:3px 6px; border-radius:999px; background:#eef1f7; color:#7b879f; font-size:8px; font-weight:850; }
.speech-mission-trace { display:grid; gap:4px; }
.speech-mission-trace div { display:grid; grid-template-columns:auto auto minmax(0,1fr); gap:5px; align-items:start; padding:5px 6px; border-left:2px solid #aeb7e5; background:#f8f9fd; border-radius:6px; }
.speech-mission-trace time { color:#9aa4b6; font-size:8px; }
.speech-mission-trace b { color:#6874a8; font-size:8px; }
.speech-mission-trace span { color:#748096; font-size:8px; line-height:1.4; overflow-wrap:anywhere; }
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

.blocked-drawer-handle { position:fixed; top:54%; right:0; z-index:131; display:grid; gap:3px; justify-items:center; min-width:42px; padding:12px 7px; border:1px solid rgba(112,126,181,.26); border-right:0; border-radius:14px 0 0 14px; color:#5e6881; background:rgba(247,249,255,.96); box-shadow:-8px 8px 24px rgba(34,45,86,.10); backdrop-filter:blur(14px); transform:translateY(-50%); transition:top .18s ease,right .24s ease,background .18s ease; cursor:pointer; }
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
  width: 100%;
  max-width: none;
  margin-inline: 0;
  grid-template-columns: minmax(400px, 440px) minmax(460px, 520px) minmax(290px, 330px) !important;
  justify-content: space-between;
  align-items: start;
  gap: 14px;
}

.room-detail-page .public-screen-panel {
  grid-column: 1;
  grid-row: 1;
  width: 100%;
  max-width: 440px;
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
  max-width: 520px;
}

@media (min-width: 1201px) {
  .room-detail-page .room-middle-preference-hub {
    width: 100%;
    max-width: none;
    margin-left: 0;
  }
}

.room-detail-page .agent-decision-panel,
.room-detail-page .event-bucket-panel {
  width: 100%;
  max-width: 340px;
  justify-self: start;
  align-self: start;
}

.capture-workspace {
  grid-column: 3;
  grid-row: 1;
  display: grid;
  align-content: start;
  justify-self: end;
  gap: 14px;
  width: 100%;
  max-width: 330px;
}
.capture-workspace-head,
.capture-card > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.capture-workspace-head {
  padding: 2px 2px 0;
}
.capture-workspace-head h3 { margin:3px 0 0; color:#2f3d5c; font-size:22px; }
.capture-workspace-head > b {
  padding:6px 9px;
  border-radius:999px;
  color:#7f89a0;
  background:#f1f3f8;
  font-size:10px;
  white-space:nowrap;
}
.capture-workspace-head > b.mode-audio_recording { color:#c84f5d; background:#fff0f2; }
.capture-workspace-head > b.mode-finalizing { color:#a26a25; background:#fff5e8; }
.capture-card {
  display:grid;
  gap:12px;
  padding:15px;
  border:1px solid rgba(93,110,177,.16);
  border-radius:20px;
  background:rgba(255,255,255,.92);
  box-shadow:0 10px 28px rgba(43,58,111,.06);
  overflow:hidden;
}
.capture-card > header strong { display:block; color:#33415f; font-size:16px; }
.capture-card > header small { display:block; margin-top:3px; color:#8b96aa; font-size:10px; line-height:1.45; }
.capture-card > header > span {
  padding:5px 8px;
  border-radius:999px;
  color:#8791a5;
  background:#f2f4f8;
  font-size:9px;
  font-weight:900;
}
.capture-card > header > span.active { color:#2c9a80; background:#e8f8f3; }
.audio-record-card.recording > header > span.active { color:#c54b5b; background:#fff0f2; }
.browser-share-guide {
  display:grid;
  gap:7px;
  padding:10px;
  border:1px dashed rgba(99,115,184,.24);
  border-radius:13px;
  background:#f7f8fc;
}
.browser-share-guide.active { border-style:solid; border-color:rgba(74,154,128,.24); background:#f3faf7; }
.browser-share-guide > div { display:grid; grid-template-columns:22px minmax(0,1fr); align-items:start; gap:7px; }
.browser-share-guide b {
  display:grid;
  place-items:center;
  width:20px;
  height:20px;
  border-radius:50%;
  color:#6472c7;
  background:#edf0ff;
  font-size:10px;
}
.browser-share-guide span { color:#7e889d; font-size:10px; line-height:1.45; }
.browser-share-actions { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
.browser-share-actions .capture-stop-button { grid-column:2; }
.browser-video-float {
  position:fixed;
  z-index:1180;
  width:min(420px,calc(100vw - 24px));
  height:264px;
  min-width:300px;
  min-height:190px;
  border:1px solid rgba(102,118,173,.3);
  border-radius:14px;
  background:#080b11;
  box-shadow:0 20px 55px rgba(20,28,55,.28),0 4px 14px rgba(20,28,55,.18);
  overflow:hidden;
  resize:both;
}
.browser-video-float.portrait {
  width:min(330px,calc(100vw - 24px));
  height:min(590px,calc(100vh - 92px));
}
.browser-video-float.collapsed {
  width:240px;
  height:44px;
  min-width:220px;
  min-height:44px;
  resize:none;
}
.browser-video-float-head {
  height:44px;
  display:flex;
  align-items:center;
  justify-content:space-between;
  gap:8px;
  padding:0 8px 0 12px;
  color:#dfe5f3;
  background:linear-gradient(180deg,#202738,#161b27);
  cursor:grab;
  user-select:none;
}
.browser-video-float-head:active { cursor:grabbing; }
.browser-video-float-head > div:first-child { min-width:0; }
.browser-video-float-head span { display:block; color:#8490aa; font-size:8px; font-weight:900; letter-spacing:.12em; }
.browser-video-float-head strong { display:block; max-width:180px; overflow:hidden; color:#eef2fa; font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.browser-video-float-actions { display:flex; align-items:center; gap:4px; }
.browser-video-float-actions button,
.browser-video-crop-actions button {
  border:1px solid rgba(255,255,255,.1);
  border-radius:7px;
  color:#b9c2d5;
  background:rgba(255,255,255,.06);
  font:inherit;
  font-size:9px;
  font-weight:800;
  cursor:pointer;
}
.browser-video-float-actions button { min-height:26px; padding:4px 7px; }
.browser-video-float-actions button:last-child { width:26px; padding:0; color:#e3a2aa; }
.browser-video-float-actions button.active { color:#fff; background:rgba(95,112,210,.42); }
.browser-video-float-body { position:relative; height:calc(100% - 44px); background:#05070b; overflow:hidden; }
.browser-video-float-body > video {
  position:absolute;
  width:2px;
  height:2px;
  opacity:0;
  pointer-events:none;
}
.browser-video-float-body > canvas { display:block; width:100%; height:100%; background:#05070b; }
.browser-video-crop-panel {
  position:absolute;
  top:10px;
  right:10px;
  width:210px;
  display:grid;
  gap:8px;
  padding:10px;
  border:1px solid rgba(255,255,255,.12);
  border-radius:11px;
  background:rgba(15,19,29,.92);
  box-shadow:0 10px 28px rgba(0,0,0,.28);
  backdrop-filter:blur(12px);
}
.browser-video-crop-actions { display:grid; grid-template-columns:1fr 1fr; gap:6px; }
.browser-video-crop-actions button:last-child { grid-column:1 / -1; }
.browser-video-crop-actions button { min-height:28px; padding:5px 7px; }
.browser-video-crop-panel label { display:grid; grid-template-columns:48px minmax(0,1fr); align-items:center; gap:6px; color:#aeb7ca; font-size:9px; }
.browser-video-crop-panel input[type='range'] { width:100%; accent-color:#7483df; }
.capture-room-link {
  display:flex;
  align-items:center;
  justify-content:center;
  gap:5px;
  width:100%;
  min-height:34px;
  padding:7px 11px;
  border:1px solid rgba(83,98,206,.16);
  border-radius:9px;
  color:#6572bd;
  background:#fafbff;
  font-size:10px;
  font-weight:800;
  line-height:1;
  text-decoration:none;
  transition:background .18s ease, box-shadow .18s ease, transform .18s ease;
}
.capture-room-link:hover {
  background:#fff;
  box-shadow:0 6px 18px rgba(74,88,178,.12);
  transform:translateY(-1px);
}
.capture-primary-button,
.capture-stop-button {
  width:100%;
  min-height:38px;
  border-radius:11px;
  border:1px solid rgba(87,104,210,.22);
  color:#5362ce;
  background:#f1f3ff;
  font:inherit;
  font-size:12px;
  font-weight:900;
  cursor:pointer;
}
.capture-primary-button.audio { color:#3e7b8e; border-color:rgba(71,147,168,.24); background:#edf8fa; }
.capture-primary-button.viewer { color:#4e68b8; border-color:rgba(77,103,190,.23); background:#eef3ff; }
.capture-primary-button.speech-analysis { color:#5b5fc7; border-color:rgba(91,95,199,.22); background:#f2f2ff; }
.capture-stop-button { color:#b94f5b; border-color:rgba(206,83,99,.24); background:#fff2f3; }
.capture-primary-button:disabled,.capture-stop-button:disabled { opacity:.45; cursor:not-allowed; }
.capture-lock-tip { color:#a06e39; font-size:10px; line-height:1.55; }
.audio-record-visual {
  display:flex;
  align-items:center;
  justify-content:center;
  gap:3px;
  height:62px;
  padding:8px 12px;
  border-radius:14px;
  background:#f4f6fa;
  overflow:hidden;
}
.audio-record-visual i { width:4px; min-height:8%; border-radius:999px; background:#bbc4d4; transition:height .25s ease; }
.audio-record-visual.active i { background:#d16774; }
.audio-record-metrics { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
.audio-record-metrics > div { padding:9px 10px; border-radius:12px; background:#f7f8fb; }
.audio-record-metrics span { display:block; color:#929bad; font-size:9px; }
.audio-record-metrics strong { display:block; margin-top:3px; color:#3a4763; font-size:15px; }
.recording-delivery {
  display:grid;
  grid-template-columns:minmax(0,1fr) auto;
  align-items:center;
  gap:10px;
  padding:10px;
  border-radius:12px;
  background:#edf8f4;
}
.recording-delivery strong { display:block; color:#357b67; font-size:11px; }
.recording-delivery small { display:block; margin-top:3px; color:#79a091; font-size:9px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.recording-delivery a { padding:7px 9px; border-radius:9px; color:#fff; background:#4aa78d; font-size:10px; font-weight:900; text-decoration:none; white-space:nowrap; }
.speech-analysis-progress { display:grid; gap:7px; padding:10px; border-radius:12px; background:#f7f7fc; }
.speech-analysis-progress-copy { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.speech-analysis-progress-copy span { color:#7f89a0; font-size:10px; line-height:1.4; }
.speech-analysis-progress-copy strong { color:#5e67b8; font-size:11px; }
.speech-analysis-progress-track { height:6px; border-radius:999px; background:#e7e9f3; overflow:hidden; }
.speech-analysis-progress-track i { display:block; width:0; height:100%; border-radius:inherit; background:linear-gradient(90deg,#7b83df 0%,#5ba3d8 42%,#aeb5ff 58%,#5ba3d8 100%); background-size:220% 100%; transition:width .35s ease; }
.speech-analysis-progress.active .speech-analysis-progress-track i { animation:speech-analysis-flow 1.2s linear infinite; }
.speech-analysis-progress.active { background:#f4f5ff; }
.speech-analysis-upload-progress-detail { display:flex; align-items:center; gap:9px; color:#7f89a0; font-size:9px; }
.speech-analysis-upload-progress-detail span:first-child { font-weight:900; color:#5c6783; }
.speech-analysis-upload-progress-detail span:nth-child(2) { margin-left:auto; color:#7080bf; }
.speech-analysis-upload-progress-detail button { padding:4px 7px; border:1px solid rgba(184,77,91,.18); border-radius:7px; color:#b24b59; background:#fff1f3; font:inherit; font-weight:900; cursor:pointer; }
.speech-analysis-steps { display:grid; grid-template-columns:repeat(7,minmax(0,1fr)); gap:4px; margin-top:2px; }
.speech-analysis-step { position:relative; display:grid; justify-items:center; gap:4px; min-width:0; color:#a1a8b8; font-size:8px; text-align:center; }
.speech-analysis-step::before { content:''; position:absolute; top:9px; left:calc(-50% + 11px); width:calc(100% - 18px); height:2px; background:#e2e5ef; }
.speech-analysis-step:first-child::before { display:none; }
.speech-analysis-step i { position:relative; z-index:1; display:grid; place-items:center; width:18px; height:18px; border-radius:50%; color:#8f98ae; background:#e7e9f2; font-style:normal; font-size:8px; font-weight:900; }
.speech-analysis-step span { overflow:hidden; max-width:100%; white-space:nowrap; text-overflow:ellipsis; }
.speech-analysis-step.done { color:#6170b1; }
.speech-analysis-step.done::before { background:#8b96dc; }
.speech-analysis-step.done i { color:#fff; background:#7582d2; }
.speech-analysis-step.current { color:#4f5fba; font-weight:900; }
.speech-analysis-step.current::before { background:#8b96dc; }
.speech-analysis-step.current i { color:#fff; background:#6272d2; animation:speech-analysis-pulse 1.15s ease-in-out infinite; }
.speech-analysis-step.failed { color:#b54b58; }
.speech-analysis-step.failed i { color:#fff; background:#c65b69; animation:none; }
.speech-analysis-transcript-delivery { background:#eff8f4; }
.speech-analysis-transcript-delivery strong { color:#3d7b68; }
.speech-analysis-transcript-delivery small { color:#789d90; }
.speech-analysis-transcript-delivery a { background:#4c9a82; }
@keyframes speech-analysis-flow { from { background-position:100% 0; } to { background-position:-120% 0; } }
@keyframes speech-analysis-pulse { 0%,100% { box-shadow:0 0 0 0 rgba(98,114,210,.2); transform:scale(1); } 50% { box-shadow:0 0 0 6px rgba(98,114,210,0); transform:scale(1.08); } }
.speech-analysis-card.active > header > span { color:#5f68c6; background:#eff0ff; }
.speech-analysis-upload-box { display:grid; grid-template-columns:auto minmax(0,1fr) auto; align-items:center; gap:9px; padding:10px; border:1px solid rgba(96,117,201,.14); border-radius:12px; background:#f8f9ff; }
.speech-analysis-file-picker { position:relative; display:inline-flex; align-items:center; justify-content:center; min-height:34px; padding:0 10px; border:1px solid rgba(96,117,201,.18); border-radius:9px; color:#596ab2; background:#edf1ff; font-size:10px; font-weight:900; cursor:pointer; white-space:nowrap; }
.speech-analysis-file-picker.disabled { opacity:.5; cursor:not-allowed; }
.speech-analysis-file-picker input { position:absolute; width:1px; height:1px; opacity:0; pointer-events:none; }
.speech-analysis-file-meta { min-width:0; }
.speech-analysis-file-meta strong { display:block; color:#59637c; font-size:10px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.speech-analysis-file-meta small { display:block; margin-top:3px; color:#939caf; font-size:9px; line-height:1.45; }
.capture-primary-button.speech-analysis.upload { min-width:88px; }
.speech-analysis-or { display:flex; align-items:center; gap:8px; color:#a0a7b6; font-size:9px; }
.speech-analysis-or::before,.speech-analysis-or::after { content:''; flex:1; height:1px; background:#eceef4; }
.speech-analysis-or span { white-space:nowrap; }
.speech-analysis-delivery { background:#f0f5ff; }
.speech-analysis-delivery strong { color:#5064aa; }
.speech-analysis-delivery small { color:#7f8fb8; }
.speech-analysis-delivery a { background:#6075c9; }
.speech-analysis-delivery-actions { display:flex; align-items:center; gap:6px; }
.speech-analysis-delivery-actions button { padding:7px 9px; border:0; border-radius:9px; color:#5363a8; background:#e5eaff; font-size:10px; font-weight:900; cursor:pointer; white-space:nowrap; }
.speech-analysis-delivery-actions button:disabled { opacity:.55; cursor:not-allowed; }
.speech-analysis-report-panel { display:grid; gap:9px; max-height:560px; padding:11px; border:1px solid rgba(92,105,174,.15); border-radius:12px; background:#f8f9fe; overflow:hidden; }
.speech-analysis-report-panel > header { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.speech-analysis-report-panel > header strong { display:block; color:#4f5f9f; font-size:11px; }
.speech-analysis-report-panel > header small { display:block; margin-top:3px; color:#8a94ad; font-size:9px; line-height:1.45; }
.speech-analysis-report-panel > header button { flex:0 0 auto; padding:6px 8px; border:1px solid rgba(96,117,201,.18); border-radius:8px; color:#5a6cb8; background:#eef1ff; font-size:9px; font-weight:900; cursor:pointer; }
.speech-analysis-report-panel pre { margin:0; padding:12px; border-radius:10px; color:#47516a; background:#fff; font:10px/1.72 ui-monospace,SFMono-Regular,Consolas,"Liberation Mono",monospace; white-space:pre-wrap; overflow:auto; }
.recording-error,.capture-workspace-error { padding:9px 10px; border-radius:10px; color:#b54b58; background:#fff0f2; font-size:10px; line-height:1.5; }
.capture-business-note { margin:0; color:#98a1b1; font-size:9px; line-height:1.55; }

@media(max-width:1260px){
  .room-detail-page .room-control-grid {
    grid-template-columns: minmax(230px, .8fr) minmax(440px, 1.45fr) minmax(230px, .8fr);
  }

  .dashboard-flow-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media(max-width:1200px){
  .room-detail-page .detail-layout-no-preview {
    width:min(100%, 880px);
    grid-template-columns:minmax(440px,490px) minmax(330px,374px) !important;
  }
  .capture-workspace {
    grid-column:1 / -1;
    grid-row:2;
    grid-template-columns:1fr 1fr;
    max-width:880px;
  }
  .capture-workspace-head,.capture-workspace-error { grid-column:1 / -1; }
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

  .capture-workspace {
    grid-column:1;
    grid-row:auto;
    grid-template-columns:1fr;
    max-width:100%;
  }
  .capture-workspace-head,.capture-workspace-error { grid-column:1; }

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
