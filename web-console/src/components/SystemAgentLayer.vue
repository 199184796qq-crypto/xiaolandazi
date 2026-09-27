<script setup lang="ts">
import WorkInboxPanel from './WorkInboxPanel.vue'
import TodoBadge from './TodoBadge.vue'
import { isInboxIntent, refreshInbox, canUseWorkInbox } from '../workInbox'
import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  ref,
  watch,
} from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  activateLiveAgentConfigVersion,
  chatLiveAgent,
  chatLivePolicyAdminAgent,
  chatClientAgent,
  chatInternalAgent,
  chatLiveRoomPolicyAgent,
  createAgentLearningSession,
  createAgentLearningTurn,
  testAgentLearningSession,
  classifyAgentLearningMessage,
  chatAgentLearningCompanion,
  adoptAgentLearningSession,
  getAgentMemories,
  deactivateAgentMemory,
  getAgentMemoryVersions,
  rollbackAgentMemoryVersion,
  createLiveOpsAnchorTraining,
  createLiveAgentConfigDraft,
  createLivePolicyLearningCandidate,
  createCommercialMarketingCampaign,
  createStaffEmployee,
  getClientAgentContext,
  getInternalAgentContext,
  getPublicSystemConfig,
  getLiveAgentConfigVersions,
  getLiveOfficialVoices,
  getRoomAgentDecisions,
  getLiveVoiceProfiles,
  getRooms,
  enqueueRoomManualAgentDecision,
  simulateRoomAgentDecision,
  testLivePolicyAdmin,
} from '../api'
import type {
  InitialCredential,
  AgentDecisionSimulationResult,
  AgentMemoryItem,
  AgentMemoryVersion,
  LivePolicyLearningCandidate,
  SystemAgentActionPreview,
  SystemAgentChatResponse,
  SystemAgentContextResponse,
} from '../types'
import { session } from '../session'
import { canDelegateLivePolicyL3 } from '../livePolicyAccess'
import { resolveAgentNavigationTargets } from '../navigationUi'
import { shouldRouteToSystemAgent } from '../systemAgentRouting'

type AgentDomain = 'system' | 'live-room' | 'live-strategy' | 'live-policy-admin' | 'live-support'
type SystemTaskKey = 'create_staff_employee' | 'create_marketing_campaign'
type AgentHistoryItem = { role: 'user' | 'agent'; text: string }
type ComposerSource = 'dock' | 'drawer'
type SuggestionKind = 'department' | 'capability' | 'navigation'

type LivePolicyTestMode = {
  active: boolean
  layer: 'L1' | 'L2'
  industryCode: string
}

type LiveStrategyMode = 'basic' | 'strategy' | 'anchor' | 'script' | 'voice'
type CoachingKind = 'reference_answer' | 'general'

type ChatMessage = {
  role: 'user' | 'agent'
  text: string
  domain: AgentDomain
  action?: SystemAgentActionPreview
  credential?: InitialCredential
  introduction?: boolean
  conversationScope?: string
  answerReference?: {
    learningSessionId?: number
    question: string
    strategy: string
    reference?: string
    feedback?: string
    history?: AgentHistoryItem[]
    canAdopt?: boolean
    saving?: boolean
    accepted?: boolean
    coachingKind?: CoachingKind
    target?: string
    memoryType?: string
    matchedMemoryItemId?: number
    adoptedVersionNo?: number
  }
  memoryList?: AgentMemoryItem[]
  memoryVersions?: {
    memory: AgentMemoryItem
    versions: AgentMemoryVersion[]
  }
}

type LiveRoomAnswerMode = 'quick' | 'answer'
type LiveRoomWorkMode = 'chat' | 'learning' | 'test' | 'execution'

type SuggestionItem = {
  kind: SuggestionKind
  label: string
  description: string
  insertText: string
  liveAction?: LiveRoomAnswerMode
  answerReferenceAction?: boolean
}

type AnswerReferenceContext = {
  kind?: 'event' | 'question' | 'bucket' | 'speech'
  question: string
  topic?: string
  eventId?: number
  userId?: string
  nickname?: string
  time?: string
  aggregateCount?: number
  uniqueUsers?: number
  similarQuestions?: string[]
  originalReply?: string
  sourceRef?: string
  sourceType?: string
}

type AnswerReferenceSession = {
  context: AnswerReferenceContext
  history: AgentHistoryItem[]
  initialReference?: string
  latestReply?: string
}

type CoachingSession = {
  active: boolean
  kind: CoachingKind
  target: string
  scope: string
  context?: AnswerReferenceContext
  history: AgentHistoryItem[]
  initialReference?: string
  latestReply?: string
  backendSessionId?: number
}

const route = useRoute()
const router = useRouter()
const expanded = ref(false)
const drawerOpen = ref(false)
const drawerTab = ref<'chat' | 'inbox'>('chat')
const inboxRequest = ref('')
const input = ref('')
const busy = ref(false)
const busyDomain = ref<AgentDomain | null>(null)
const executing = ref(false)
const latestSystemResponse = ref<SystemAgentChatResponse | null>(null)
const systemContext = ref<SystemAgentContextResponse>({
  capabilities: [],
  departments: [],
})
const inputEl = ref<HTMLTextAreaElement | null>(null)
const drawerInputEl = ref<HTMLTextAreaElement | null>(null)
const chatEl = ref<HTMLElement | null>(null)
const activeComposer = ref<ComposerSource | null>(null)
const suggestionIndex = ref(0)
const dismissedSuggestionInput = ref('')
const activeSystemTask = ref<SystemTaskKey | null>(null)
const systemTaskHistory = ref<AgentHistoryItem[]>([])
const livePolicyTestMode = ref<LivePolicyTestMode>({
  active: false,
  layer: 'L1',
  industryCode: '',
})
const livePolicyTestHistory = ref<AgentHistoryItem[]>([])
const liveRoomAnswerMode = ref<LiveRoomAnswerMode | null>(null)
const liveRoomExecutionStatus = ref('')
const liveRoomExecutionError = ref(false)
const liveRoomTestMode = ref(false)
const liveRoomWorkMode = ref<LiveRoomWorkMode>('chat')
const answerReferencePicking = ref(false)
const answerReferencePending = ref<AnswerReferenceContext | null>(null)
const answerReferenceSession = ref<AnswerReferenceSession | null>(null)
const coachingSession = ref<CoachingSession | null>(null)

function toggleLiveRoomTestMode() {
  liveRoomTestMode.value = !liveRoomTestMode.value
  liveRoomWorkMode.value = liveRoomTestMode.value ? 'test' : 'chat'
  liveRoomAnswerMode.value = null
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = liveRoomTestMode.value
    ? '测试模式 · 输入内容将模拟观众提问，走真实 Agent 链路，仅返回文字不播音'
    : ''
  input.value = ''
  dismissedSuggestionInput.value = ''
  void nextTick(focusActiveComposer)
}

function setAnswerReferencePicking(active: boolean) {
  answerReferencePicking.value = active
  window.dispatchEvent(
    new CustomEvent('live-answer-reference-mode', { detail: { active } }),
  )
}

function beginAnswerReferencePicking() {
  liveRoomWorkMode.value = 'learning'
  liveRoomAnswerMode.value = null
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = '纠正智能体 · 请点击公屏问题、问题聚类或聚类内单条问题'
  answerReferencePending.value = null
  answerReferenceSession.value = null
  coachingSession.value = null
  input.value = ''
  dismissedSuggestionInput.value = ''
  expanded.value = true
  drawerOpen.value = false
  activeComposer.value = 'dock'
  setAnswerReferencePicking(true)
  void nextTick(() => inputEl.value?.focus())
}

function openAnswerReference(payload: AnswerReferenceContext) {
  const question = String(payload.question || '').trim()
  if (!question) return
  const context: AnswerReferenceContext = {
    ...payload,
    question,
    similarQuestions: (payload.similarQuestions || []).filter(Boolean).slice(0, 8),
  }
  setAnswerReferencePicking(false)
  answerReferencePending.value = context
  answerReferenceSession.value = {
    context,
    history: [],
    latestReply: context.originalReply,
  }
  coachingSession.value = {
    active: true,
    kind: 'reference_answer',
    target: context.topic || context.question,
    scope: conversationScopeForDomain('live-room'),
    context,
    history: [],
    latestReply: context.originalReply,
  }
  liveRoomWorkMode.value = 'learning'
  liveRoomAnswerMode.value = null
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = ''
  input.value = ''
  dismissedSuggestionInput.value = ''
  expanded.value = true
  drawerOpen.value = true
  drawerTab.value = 'chat'
  activeComposer.value = 'drawer'
  if (context.originalReply) {
    pushAgentMessage(
      'live-room',
      '正在纠正这次生成的话术：\n' + context.originalReply + '\n\n直接告诉我哪里不对，或者正确应该怎么说。',
    )
  }
  void nextTick(() => drawerInputEl.value?.focus())
}

async function ensureAgentLearningBackendSession(sessionState: CoachingSession) {
  if (sessionState.backendSessionId) return sessionState.backendSessionId
  const roomId = Number(route.params.id)
  if (!roomId) throw new Error('当前没有有效直播间')
  const context = sessionState.context
  const created = await createAgentLearningSession(roomId, {
    source_type: context?.sourceType || (context?.question ? 'question_correction' : 'direct_correction'),
    source_ref: context?.sourceRef || (context?.eventId ? 'room_event:' + context.eventId : ''),
    question: context?.question || '',
    original_reply: context?.originalReply || sessionState.latestReply || '',
    target: sessionState.target,
  })
  sessionState.backendSessionId = created.id
  return created.id
}

function cancelAnswerReference() {
  setAnswerReferencePicking(false)
  answerReferencePending.value = null
  answerReferenceSession.value = null
  if (coachingSession.value?.kind === 'reference_answer') coachingSession.value = null
  liveRoomExecutionStatus.value = ''
  liveRoomExecutionError.value = false
  input.value = ''
  dismissedSuggestionInput.value = ''
  void nextTick(focusActiveComposer)
}

function beginCoachingMode(targetOverride = '') {
  if (currentDomain.value !== 'live-room') return
  const existingReference = answerReferencePending.value || answerReferenceSession.value?.context
  if (existingReference?.question) {
    coachingSession.value = {
      active: true,
      kind: 'reference_answer',
      target: existingReference.topic || existingReference.question,
      scope: conversationScopeForDomain('live-room'),
      context: existingReference,
      history: answerReferenceSession.value?.history.slice(-16) || [],
      initialReference: answerReferenceSession.value?.initialReference,
      latestReply: answerReferenceSession.value?.latestReply,
    }
    liveRoomWorkMode.value = 'learning'
    liveRoomExecutionStatus.value = '智能体学习 · ' + coachingSession.value.target
    return
  }
  const target = targetOverride.trim() || '当前直播间需要纠正的内容'
  coachingSession.value = {
    active: true,
    kind: 'general',
    target,
    scope: conversationScopeForDomain('live-room'),
    history: [],
  }
  liveRoomWorkMode.value = 'learning'
  setAnswerReferencePicking(false)
  answerReferencePending.value = null
  answerReferenceSession.value = null
  liveRoomAnswerMode.value = null
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = '智能体学习 · ' + target
  input.value = ''
  dismissedSuggestionInput.value = ''
  expanded.value = true
  void nextTick(focusActiveComposer)
}

function endCoachingMode(announce = true) {
  const wasActive = Boolean(coachingSession.value?.active || answerReferencePending.value || answerReferenceSession.value)
  setAnswerReferencePicking(false)
  answerReferencePending.value = null
  answerReferenceSession.value = null
  coachingSession.value = null
  liveRoomWorkMode.value = 'chat'
  liveRoomExecutionStatus.value = ''
  liveRoomExecutionError.value = false
  input.value = ''
  dismissedSuggestionInput.value = ''
  if (announce && wasActive) pushAgentMessage('live-room', '已退出当前学习界面，未采用内容仍保留在对话记录中。')
  void nextTick(focusActiveComposer)
}

function handleAnswerReferenceSelected(event: Event) {
  const detail = (event as CustomEvent<AnswerReferenceContext>).detail
  if (!detail?.question) return
  openAnswerReference(detail)
}

async function adoptAnswerReference(message: NonNullable<ChatMessage['answerReference']>) {
  if (message.accepted || message.saving || message.canAdopt === false) return
  const roomId = Number(route.params.id)
  if (!roomId) {
    pushAgentMessage('live-room', '当前没有有效直播间，暂时不能采用这条修正结果。')
    return
  }
  const sessionId = message.learningSessionId || coachingSession.value?.backendSessionId
  if (!sessionId) {
    pushAgentMessage('live-room', '这条修正结果还没有学习会话，请继续输入一次纠正后再采用。')
    return
  }

  message.saving = true
  try {
    const adopted = await adoptAgentLearningSession(roomId, sessionId)
    disablePriorLearningCandidates(sessionId)
    message.accepted = true
    message.canAdopt = false
    message.memoryType = adopted.memory.memory_type
    message.adoptedVersionNo = adopted.version.version_no
    setAnswerReferencePicking(false)
    answerReferencePending.value = null
    answerReferenceSession.value = null
    coachingSession.value = null
    liveRoomWorkMode.value = 'chat'
    liveRoomExecutionError.value = false
    liveRoomExecutionStatus.value = ''
    window.dispatchEvent(new CustomEvent('agent-memory-updated', { detail: adopted.memory }))
    pushAgentMessage(
      'live-room',
      '已采用并立即生效。V' + adopted.version.version_no + '。当前直播间从下一次回答开始按这个意思处理。',
    )
  } catch (error) {
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '采用失败：' + error.message : '采用失败，请稍后重试。',
    )
  } finally {
    message.saving = false
    void scrollChatToBottom()
  }
}

function disablePriorLearningCandidates(sessionId: number) {
  if (!sessionId) return
  for (const item of messages.value) {
    if (item.answerReference?.learningSessionId !== sessionId) continue
    item.answerReference.canAdopt = false
  }
}

async function showAgentMemories() {
  const roomId = Number(route.params.id)
  if (!roomId) return
  busy.value = true
  busyDomain.value = 'live-room'
  drawerOpen.value = true
  expanded.value = true
  drawerTab.value = 'chat'
  try {
    const response = await getAgentMemories(roomId)
    messages.value.push({
      role: 'agent',
      domain: 'live-room',
      text: response.items.length ? '当前直播间正在生效的智能体记忆：' : '当前直播间还没有已采用的智能体记忆。',
      conversationScope: conversationScopeForDomain('live-room'),
      memoryList: response.items,
    })
  } catch (error) {
    pushAgentMessage('live-room', error instanceof Error ? error.message : '读取智能体记忆失败')
  } finally {
    busy.value = false
    busyDomain.value = null
    void scrollChatToBottom()
  }
}

async function correctAgentMemory(memory: AgentMemoryItem) {
  const roomId = Number(route.params.id)
  if (!roomId) return
  const created = await createAgentLearningSession(roomId, {
    source_type: 'memory_correction',
    source_ref: 'agent_memory:' + memory.id,
    original_reply: memory.current_version?.content_text || '',
    target: memory.target,
  })
  coachingSession.value = {
    active: true,
    kind: 'general',
    target: memory.target,
    scope: conversationScopeForDomain('live-room'),
    history: [],
    latestReply: memory.current_version?.content_text,
    backendSessionId: created.id,
  }
  drawerOpen.value = true
  expanded.value = true
  liveRoomExecutionStatus.value = '智能体学习 · 正在纠正：' + memory.target
  pushAgentMessage(
    'live-room',
    '正在纠正“' + memory.target + '”当前记忆（V' +
      (memory.current_version?.version_no || 1) +
      '）：\n' + (memory.current_version?.content_text || '') +
      '\n\n直接告诉我哪里不对，或者正确应该是什么。',
  )
  activeComposer.value = 'drawer'
  void nextTick(focusActiveComposer)
}

async function stopAgentMemory(memory: AgentMemoryItem) {
  const roomId = Number(route.params.id)
  if (!roomId) return
  try {
    await deactivateAgentMemory(roomId, memory.id)
    pushAgentMessage('live-room', '已停用“' + memory.target + '”。从下一次回答开始不再使用这条记忆，历史版本仍保留。')
    await showAgentMemories()
  } catch (error) {
    pushAgentMessage('live-room', error instanceof Error ? error.message : '停用智能体记忆失败')
  }
}

async function showAgentMemoryVersions(memory: AgentMemoryItem) {
  const roomId = Number(route.params.id)
  if (!roomId) return
  try {
    const response = await getAgentMemoryVersions(roomId, memory.id)
    messages.value.push({
      role: 'agent',
      domain: 'live-room',
      text: '“' + memory.target + '”版本记录',
      conversationScope: conversationScopeForDomain('live-room'),
      memoryVersions: { memory, versions: response.items },
    })
    drawerOpen.value = true
    void scrollChatToBottom()
  } catch (error) {
    pushAgentMessage('live-room', error instanceof Error ? error.message : '读取记忆版本失败')
  }
}

async function rollbackAgentMemory(memory: AgentMemoryItem, version: AgentMemoryVersion) {
  const roomId = Number(route.params.id)
  if (!roomId) return
  try {
    const updated = await rollbackAgentMemoryVersion(roomId, memory.id, version.id)
    memory.status = updated.status
    memory.current_version_id = updated.current_version_id
    memory.current_version = updated.current_version
    pushAgentMessage('live-room', '已恢复“' + memory.target + '”到 V' + version.version_no + '，立即对当前直播间生效。')
    window.dispatchEvent(new CustomEvent('agent-memory-updated', { detail: updated }))
  } catch (error) {
    pushAgentMessage('live-room', error instanceof Error ? error.message : '回滚记忆版本失败')
  }
}

const liveRoomAnswerModeLabel = computed(() =>
  liveRoomAnswerMode.value === 'quick'
    ? '抢答模式'
    : liveRoomAnswerMode.value === 'answer'
      ? '回答模式'
      : '',
)

const dockEl = ref<HTMLElement | null>(null)
const dockPosition = ref<{ left: number; top: number } | null>(null)
const dockDragging = ref(false)
const dockStyle = computed(() => {
  // Collapsed orb is always anchored at the global bottom-center CSS position.
  // Preserve any dragged coordinates so expanding restores the existing dock logic.
  if (!expanded.value || !dockPosition.value) return undefined
  return {
    left: dockPosition.value.left + 'px',
    top: dockPosition.value.top + 'px',
    right: 'auto',
    bottom: 'auto',
    transform: 'none',
  }
})

const DOCK_VIEWPORT_MARGIN = 8
let dockDragPointerID: number | null = null
let dockDragStartX = 0
let dockDragStartY = 0
let dockDragOriginLeft = 0
let dockDragOriginTop = 0
let dockDragWidth = 0
let dockDragHeight = 0
let dockDragMoved = false

const actor = computed(() => session.bootstrap?.actor)
const isTerminalCustomer = computed(() => actor.value?.role === 'customer')
const showInbox = computed(() => canUseWorkInbox(actor.value?.role))
watch(showInbox, enabled => {
  if (!enabled) { drawerTab.value = 'chat'; inboxRequest.value = '' }
}, { immediate: true })

const storedInternalLiveStrategyMode = window.localStorage.getItem(
  'system-agent-live-strategy-internal-mode',
)
const internalLiveStrategyMode = ref<'policy' | 'support' | 'learning'>(
  storedInternalLiveStrategyMode === 'support' || storedInternalLiveStrategyMode === 'learning'
    ? storedInternalLiveStrategyMode
    : 'policy',
)
const liveSupportMode = ref<'strategy' | 'anchor' | 'voice'>(
  window.localStorage.getItem('system-agent-live-support-mode') === 'anchor'
    ? 'anchor'
    : window.localStorage.getItem('system-agent-live-support-mode') === 'voice'
      ? 'voice'
      : 'strategy',
)
const storedLiveStrategyMode = window.localStorage.getItem('system-agent-live-mode')
const liveStrategyMode = ref<LiveStrategyMode>(
  storedLiveStrategyMode === 'basic' ||
  storedLiveStrategyMode === 'anchor' ||
  storedLiveStrategyMode === 'script' ||
  storedLiveStrategyMode === 'voice'
    ? storedLiveStrategyMode
    : 'strategy',
)
const isInternalAgentProfile = computed(() =>
  ['platform_admin', 'staff', 'sales_staff'].includes(actor.value?.role || ''),
)
const navigationTargets = computed(() => resolveAgentNavigationTargets(session.bootstrap))
const currentDomain = computed<AgentDomain>(() => {
  if (route.name === 'room-detail') return 'live-room'
  if (route.name === 'live-strategy') {
    if (actor.value?.role === 'customer') return 'live-strategy'
    if (isInternalAgentProfile.value) {
      return internalLiveStrategyMode.value === 'support'
        ? 'live-support'
        : 'live-policy-admin'
    }
  }
  return 'system'
})

const internalAgentName = ref('小蓝工作搭子')
const clientAgentName = ref('小蓝直播搭子')
const assistantName = computed(() =>
  isInternalAgentProfile.value ? internalAgentName.value : clientAgentName.value,
)

const AGENT_CHAT_HISTORY_PREFIX = 'system-agent-chat-history:v2:'
const AGENT_CHAT_HISTORY_LIMIT = 500

function conversationScopeForDomain(domain: AgentDomain) {
  if (domain === 'live-room') {
    const roomId = Number(route.params.id || 0)
    return roomId > 0 ? 'live-room:room:' + roomId : 'live-room:room:none'
  }
  if (domain === 'live-strategy') {
    const roomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    return roomId > 0 ? 'live-strategy:room:' + roomId : 'live-strategy:room:none'
  }
  if (domain === 'live-support') {
    const roomId = Number(window.localStorage.getItem('system-agent-live-support-room-id') || 0)
    return roomId > 0 ? 'live-support:room:' + roomId : 'live-support:room:none'
  }
  return domain
}

function agentHistoryStorageKey(userId: number) {
  return AGENT_CHAT_HISTORY_PREFIX + String(userId)
}

function restoreAgentChatHistory(userId: number) {
  if (!userId) return
  try {
    const raw = window.localStorage.getItem(agentHistoryStorageKey(userId))
    if (!raw) return
    const parsed = JSON.parse(raw) as { messages?: ChatMessage[]; coachingSession?: CoachingSession | null }
    const restored = Array.isArray(parsed.messages)
      ? parsed.messages.filter((item) => item && (item.role === 'user' || item.role === 'agent') && typeof item.text === 'string')
      : []
    if (!restored.length) return
    messages.value = restored.slice(-AGENT_CHAT_HISTORY_LIMIT).map((item) => ({
      role: item.role,
      text: item.text,
      domain: item.domain,
      introduction: item.introduction,
      conversationScope: item.conversationScope,
      answerReference: item.answerReference
        ? { ...item.answerReference, saving: false }
        : undefined,
    })) as ChatMessage[]
    const savedCoaching = parsed.coachingSession
    if (savedCoaching?.active && savedCoaching.scope === conversationScopeForDomain('live-room')) {
      coachingSession.value = {
        ...savedCoaching,
        history: Array.isArray(savedCoaching.history) ? savedCoaching.history.slice(-16) : [],
      }
      liveRoomExecutionStatus.value = '智能体学习 · ' + savedCoaching.target
      if (savedCoaching.kind === 'reference_answer' && savedCoaching.context?.question) {
        answerReferencePending.value = savedCoaching.context
        answerReferenceSession.value = {
          context: savedCoaching.context,
          history: Array.isArray(savedCoaching.history) ? savedCoaching.history.slice(-16) : [],
          initialReference: savedCoaching.initialReference,
          latestReply: savedCoaching.latestReply,
        }
      }
    }
  } catch {
    window.localStorage.removeItem(agentHistoryStorageKey(userId))
  }
}

function persistAgentChatHistory(userId: number) {
  if (!userId) return
  const normalized = messages.value.slice(-AGENT_CHAT_HISTORY_LIMIT).map((item) => ({
    role: item.role,
    text: item.text,
    domain: item.domain,
    introduction: item.introduction,
    conversationScope: item.conversationScope || conversationScopeForDomain(item.domain),
    answerReference: item.answerReference
      ? { ...item.answerReference, saving: false }
      : undefined,
  }))
  window.localStorage.setItem(
    agentHistoryStorageKey(userId),
    JSON.stringify({
      savedAt: new Date().toISOString(),
      messages: normalized,
      coachingSession: coachingSession.value?.active ? coachingSession.value : null,
    }),
  )
}

const messages = ref<ChatMessage[]>([
  {
    role: 'agent',
    domain: 'system',
    introduction: true,
    text: `我是${assistantName.value}。你在系统里走到哪里，我就切换到那个业务工作域；所有动作仍受当前账号权限和原有审批规则约束。`,
  },
])

const contextLabel = computed(() => {
  if (livePolicyTestMode.value.active && currentDomain.value === 'live-policy-admin') {
    return '直播策略 · 规则测试'
  }
  if (currentDomain.value === 'live-room') return '直播场控'
  if (currentDomain.value === 'live-strategy') {
    if (liveStrategyMode.value === 'anchor') return '直播策略 · 主播训练'
    if (liveStrategyMode.value === 'script') return '直播策略 · 固定话术'
    if (liveStrategyMode.value === 'voice') return '直播策略 · 声音配置'
    if (liveStrategyMode.value === 'basic') return '直播策略 · 基础设置'
    return '直播策略 · 当前直播间用户层'
  }
  if (currentDomain.value === 'live-policy-admin') return '直播策略 · 系统/行业规则'
  if (currentDomain.value === 'live-support') return '直播策略 · 客户授权协助'
  return isInternalAgentProfile.value ? '系统管理' : '终端助手'
})

const contextDescription = computed(() => {
  if (currentDomain.value === 'live-room') {
    return '已进入当前直播间场控上下文，直接处理现场问题、话术和场控协作。'
  }
  if (currentDomain.value === 'live-strategy') {
    if (liveStrategyMode.value === 'anchor') return '已进入当前直播间主播训练上下文。'
    if (liveStrategyMode.value === 'script') return '已进入当前直播间固定话术上下文。'
    if (liveStrategyMode.value === 'voice') return '已进入当前直播间声音配置上下文。'
    if (liveStrategyMode.value === 'basic') return '已进入当前客户直播助手基础设置上下文。'
    return '已进入当前直播间用户层策略上下文。'
  }
  if (currentDomain.value === 'live-policy-admin') {
    if (internalLiveStrategyMode.value === 'learning') {
      return '已进入调教学习中心；满意的对话可提交吸收，智能体会判断应沉淀到规则层、行业层还是用户层。'
    }
    return '已进入管理端直播策略上下文，自动跟随当前规则层 / 行业层与行业选择。'
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      return canDelegateLivePolicyL3(session.bootstrap)
        ? '已进入客户授权的用户层代维护上下文，只会作用于当前授权直播间。'
        : '当前账号没有用户层授权协助权限；运维人员需要具备行业层配置能力，并获得客户对当前直播间的授权。'
    }
    if (liveSupportMode.value === 'anchor') {
      return '已进入客户授权的主播训练上下文；明确要求训练/学习时生成草稿，发布仍在工作台确认。'
    }
    return '已进入客户授权的声音复刻上下文；声音样本上传和复刻档案仍需在工作台完成。'
  }
  return isInternalAgentProfile.value
    ? '按照当前账号权限查询和执行后台事务；写入动作先预览再确认。'
    : '只处理当前账号自己的终端业务，不接触内部后台数据和管理工具。'
})

const inputPlaceholder = computed(() => {
  if (currentDomain.value === 'live-room' && answerReferencePicking.value) {
    return '纠正智能体：请点击公屏问题、问题聚类或聚类内单条问题…'
  }
  if (currentDomain.value === 'live-room' && answerReferencePending.value) {
    return answerReferenceSession.value?.history.length
      ? '继续告诉智能体哪里不对，或者正确应该是什么……'
      : '告诉智能体哪里不对，或者正确应该是什么……'
  }
  if (currentDomain.value === 'live-room' && coachingSession.value?.active && coachingSession.value.kind === 'general') {
    if (liveRoomWorkMode.value === 'chat') return '正常聊就行；要继续纠正时直接说哪里需要改……'
    if (liveRoomWorkMode.value === 'test') return '正在测试当前候选；也可以继续纠正或正常聊天……'
    if (liveRoomWorkMode.value === 'execution') return '正式回答模式；也可以随时继续纠正当前候选……'
    return '智能体学习：继续输入修改意见；满意后点“采用”……'
  }
  if (currentDomain.value === 'live-room' && liveRoomTestMode.value) {
    return '测试模式：输入模拟观众问题，例如“哪年的菜籽？”……'
  }
  if (isTerminalCustomer.value) return '输入你想说的话…'
  if (livePolicyTestMode.value.active && currentDomain.value === 'live-policy-admin') {
    return livePolicyTestHistory.value.length
      ? '继续打磨；满意后直接说“吸收这次调教”……'
      : '输入一个直播问题，之后可以连续反馈直到满意……'
  }
  if (currentDomain.value === 'live-room') {
    if (liveRoomAnswerMode.value === 'quick') return '抢答模式：输入内容后立即生成并播出……'
    if (liveRoomAnswerMode.value === 'answer') return '回答模式：输入内容后进入待打断队列……'
    return '输入内容，或用 / 选择抢答 / 回答模式……'
  }
  if (currentDomain.value === 'live-strategy') {
    if (liveStrategyMode.value === 'anchor') return '输入主播训练要求……'
    if (liveStrategyMode.value === 'script') return '输入固定话术要求……'
    if (liveStrategyMode.value === 'voice') return '输入声音配置要求……'
    if (liveStrategyMode.value === 'basic') return '输入直播助手基础设置问题……'
    return '输入用户层策略要求……'
  }
  if (currentDomain.value === 'live-policy-admin') {
    if (internalLiveStrategyMode.value === 'learning') {
      return '查看学习建议，或说“吸收这次调教”沉淀满意对话……'
    }
    return '输入规则要求，或用 / 呼出规则层 / 行业层策略能力……'
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      return canDelegateLivePolicyL3(session.bootstrap)
        ? '输入客户用户层调整要求，或用 / 呼出授权协助能力……'
        : '当前岗位不可代维护客户用户层，请选择本岗位可用功能……'
    }
    if (liveSupportMode.value === 'anchor') {
      return '输入主播训练要求；素材请在当前工作台上传……'
    }
    return '输入声音复刻相关问题；声音样本请在当前工作台上传……'
  }
  return isInternalAgentProfile.value
    ? '输入要做的事；@ 呼出部门，/ 呼出能力……'
    : '输入要做的事；/ 呼出当前账号可用能力……'
})

const capabilities = computed(() => {
  if (currentDomain.value === 'live-room') {
    return ['当前直播间场控', '现场问题处理', '话术协作', '场控建议']
  }
  if (currentDomain.value === 'live-strategy') {
    if (liveStrategyMode.value === 'anchor') return ['主播训练', '主播风格', '训练草稿']
    if (liveStrategyMode.value === 'script') return ['固定话术', '原话锁定', '意图执行']
    if (liveStrategyMode.value === 'voice') return ['声音配置', '官方声音', '我的声音']
    if (liveStrategyMode.value === 'basic') return ['基础设置']
    return ['当前直播间用户层', '策略调教', '生成策略草稿']
  }
  if (currentDomain.value === 'live-policy-admin') {
    return ['规则层 / 行业层策略', '行业规则', '规则调教', '生成策略草稿']
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      return canDelegateLivePolicyL3(session.bootstrap)
        ? ['客户授权用户层', '策略调教', '生成用户层草稿']
        : ['当前岗位不可代维护用户层']
    }
    if (liveSupportMode.value === 'anchor') {
      return ['客户授权主播训练', '训练要求', '生成训练草稿']
    }
    return ['客户授权声音复刻', '复刻说明', '声音样本协助']
  }
  return (
    latestSystemResponse.value?.capabilities ||
    systemContext.value.capabilities ||
    ['权限范围说明']
  )
})

const visibleMessages = computed(() => {
  const scope = conversationScopeForDomain(currentDomain.value)
  return messages.value.filter((item) =>
    item.domain === currentDomain.value &&
    (item.conversationScope || conversationScopeForDomain(item.domain)) === scope &&
    (!isTerminalCustomer.value || !item.introduction),
  )
})

const latestAgentMessage = computed(() => {
  for (let index = messages.value.length - 1; index >= 0; index -= 1) {
    const item = messages.value[index]
    if (item?.domain === currentDomain.value && item.role === 'agent') {
      return item.text
    }
  }
  return contextDescription.value
})

function isEscapedAgentTrigger(value: string, index: number) {
  let slashCount = 0
  for (let cursor = index - 1; cursor >= 0 && value[cursor] === '\\'; cursor -= 1) {
    slashCount += 1
  }
  return slashCount % 2 === 1
}

function unescapeAgentTriggerText(value: string) {
  return value.replace(/\\([@/])/g, '$1')
}

const triggerState = computed(() => {
  if (!input.value || input.value === dismissedSuggestionInput.value) return null

  const trailingTokenStart = input.value.search(/\S*$/)
  for (let index = input.value.length - 1; index >= trailingTokenStart; index -= 1) {
    const symbol = input.value[index]
    if (symbol !== '@' && symbol !== '/') continue
    if (isEscapedAgentTrigger(input.value, index)) continue

    const full = input.value.slice(index)
    return {
      full,
      symbol: symbol as '@' | '/',
      query: unescapeAgentTriggerText(full.slice(1)).trim().toLowerCase(),
      index,
    }
  }
  return null
})

function systemCapabilityCommand(label: string) {
  const map: Record<string, { command: string; description: string }> = {
    查询员工: { command: '查询员工 ', description: '按姓名、部门或岗位查询当前权限范围内员工' },
    新增员工: { command: '新增员工 ', description: '收集员工信息并生成新增员工执行预览' },
    识别可分配岗位: { command: '查询岗位 ', description: '查看当前权限范围内可以识别和分配的岗位' },
    查询部门: { command: '查询部门 ', description: '查询当前账号有权查看的内部部门' },
    权限范围说明: { command: '权限说明 ', description: '说明当前账号可以由智能体处理的事务范围' },
    当前账号信息咨询: { command: '账号信息 ', description: '咨询当前账号和可使用的智能体能力' },
    创建营销活动: { command: '创建营销活动 ', description: '通过多轮对话补齐活动名称、商品、折扣和时间，再生成确认草稿' },
  }
  return map[label] || {
    command: label + ' ',
    description: '使用“' + label + '”能力',
  }
}

const capabilitySuggestions = computed<SuggestionItem[]>(() => {
  if (currentDomain.value === 'live-room') {
    return [
      {
        kind: 'capability',
        label: '抢答',
        description: '输入内容后立即交给监控 Agent 生成话术并打断当前播音',
        insertText: '/抢答 ',
        liveAction: 'quick',
      },
      {
        kind: 'capability',
        label: '回答',
        description: '输入内容后进入待打断队列，由监控 Agent 协调合适时间播出',
        insertText: '/回答 ',
        liveAction: 'answer',
      },
      {
        kind: 'capability',
        label: '纠正问题',
        description: '点击公屏问题或问题聚类后，直接告诉智能体哪里不对、正确应该是什么',
        insertText: '/纠正智能体 ',
        answerReferenceAction: true,
      },
      {
        kind: 'capability',
        label: '纠正智能体',
        description: '直接进入智能体学习，后续继续聊天就是继续修改，采用后立即生效',
        insertText: '/纠正智能体 ',
      },
      {
        kind: 'capability',
        label: '采用',
        description: '采用当前最新修正结果，立即写入当前直播间智能体记忆并生效',
        insertText: '/采用',
      },
      {
        kind: 'capability',
        label: '智能体记忆',
        description: '查看当前直播间已生效的语义、事实、用词和主播风格记忆',
        insertText: '/智能体记忆',
      },
      { kind: 'capability', label: '处理现场问题', description: '结合当前直播间上下文处理观众问题', insertText: '/处理现场问题 ' },
      { kind: 'capability', label: '生成话术', description: '根据当前场景生成主播可说的话术', insertText: '/生成话术 ' },
      { kind: 'capability', label: '场控建议', description: '结合直播状态给出现场操作建议', insertText: '/场控建议 ' },
    ]
  }
  if (currentDomain.value === 'live-strategy') {
    if (liveStrategyMode.value === 'anchor') {
      return [
        { kind: 'capability', label: '主播训练', description: '调整当前直播间主播表达、语气和节奏', insertText: '训练主播：' },
        { kind: 'capability', label: '主播风格', description: '沉淀当前直播间主播风格要求', insertText: '主播风格：' },
      ]
    }
    if (liveStrategyMode.value === 'script') {
      return [
        { kind: 'capability', label: '固定话术', description: '新增或调整当前直播间固定话术', insertText: '固定话术：' },
        { kind: 'capability', label: '原话锁定', description: '要求固定话术逐字执行', insertText: '100%原话：' },
        { kind: 'capability', label: '意图执行', description: '保留核心意思但允许自然变化', insertText: '按照这个意思来：' },
      ]
    }
    if (liveStrategyMode.value === 'voice') {
      return [
        { kind: 'capability', label: '声音配置', description: '讨论当前直播间声音选择和使用', insertText: '声音配置：' },
        { kind: 'capability', label: '官方声音', description: '询问官方声音选择建议', insertText: '推荐官方声音：' },
        { kind: 'capability', label: '我的声音', description: '使用已克隆的个人声音', insertText: '使用我的声音：' },
      ]
    }
    if (liveStrategyMode.value === 'basic') {
      return [{ kind: 'capability', label: '基础设置', description: '咨询直播助手基础身份设置', insertText: '基础设置：' }]
    }
    return [
      { kind: 'capability', label: '调整策略', description: '调整当前直播间用户层策略并生成草稿', insertText: '调整策略 ' },
      { kind: 'capability', label: '生成草稿', description: '按当前直播间用户层要求生成策略草稿', insertText: '生成策略草稿：' },
    ]
  }
  if (currentDomain.value === 'live-policy-admin') {
    return [
      { kind: 'capability', label: '修改规则', description: '修改当前选择的规则层或行业层规则并生成草稿', insertText: '修改规则 ' },
      { kind: 'capability', label: '查看规则', description: '围绕当前系统/行业规则进行说明和检查', insertText: '查看规则 ' },
      { kind: 'capability', label: '生成草稿', description: '按自然语言要求生成策略草稿，不直接发布', insertText: '生成草稿 ' },
    ]
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      if (!canDelegateLivePolicyL3(session.bootstrap)) return []
      return [
        { kind: 'capability', label: '调整用户层', description: '按客户授权调整当前直播间用户层并生成草稿', insertText: '调整当前客户用户层 ' },
        { kind: 'capability', label: '查看用户层', description: '查看和讨论当前授权直播间的用户层', insertText: '查看当前客户用户层 ' },
      ]
    }
    if (liveSupportMode.value === 'anchor') {
      return [
        { kind: 'capability', label: '主播训练', description: '明确训练要求并生成授权主播训练草稿', insertText: '训练当前主播：' },
        { kind: 'capability', label: '训练建议', description: '先讨论主播训练方案，不直接生成草稿', insertText: '先给我主播训练建议，不要生成草稿：' },
      ]
    }
    return [
      { kind: 'capability', label: '声音复刻说明', description: '说明当前授权声音复刻的操作方法', insertText: '说明声音复刻步骤 ' },
    ]
  }
  return capabilities.value.map((label) => {
    const value = systemCapabilityCommand(label)
    return {
      kind: 'capability',
      label,
      description: value.description,
      insertText: value.command,
    }
  })
})

const suggestions = computed<SuggestionItem[]>(() => {
  const trigger = triggerState.value
  if (!trigger) return []

  if (trigger.symbol === '@') {
    if (!isInternalAgentProfile.value) return []
    return systemContext.value.departments
      .filter((item) => {
        if (!trigger.query) return true
        return (
          item.name.toLowerCase().includes(trigger.query) ||
          item.code.toLowerCase().includes(trigger.query)
        )
      })
      .slice(0, 12)
      .map((item) => ({
        kind: 'department',
        label: item.name,
        description: item.code,
        insertText: '@' + item.name + ' ',
      }))
  }

  const navigationSuggestions: SuggestionItem[] = navigationTargets.value.map((item) => ({
    kind: 'navigation',
    label: '打开' + item.title,
    description: item.section + ' · 跳转到对应页面',
    insertText: '打开' + item.title + ' ',
  }))

  return [...capabilitySuggestions.value, ...navigationSuggestions]
    .filter((item) => {
      if (!trigger.query) return true
      return (
        item.label.toLowerCase().includes(trigger.query) ||
        item.insertText.toLowerCase().includes(trigger.query) ||
        item.description.toLowerCase().includes(trigger.query)
      )
    })
    .slice(0, 16)
})

const suggestionTitle = computed(() =>
  triggerState.value?.symbol === '@' ? '选择部门' : '选择能力',
)

function showSuggestions(source: ComposerSource) {
  return activeComposer.value === source && suggestions.value.length > 0
}

function composerFocus(source: ComposerSource) {
  activeComposer.value = source
  dismissedSuggestionInput.value = ''
  suggestionIndex.value = 0
}

function composerInput(source: ComposerSource) {
  activeComposer.value = source
  dismissedSuggestionInput.value = ''
  suggestionIndex.value = 0
}

function focusActiveComposer() {
  void nextTick(() => {
    if (activeComposer.value === 'drawer') {
      drawerInputEl.value?.focus()
      return
    }
    inputEl.value?.focus()
  })
}

function selectSuggestion(item: SuggestionItem) {
  const trigger = triggerState.value
  if (!trigger) return

  if (currentDomain.value === 'live-room' && item.answerReferenceAction) {
    beginAnswerReferencePicking()
    return
  }

  if (currentDomain.value === 'live-room' && item.liveAction) {
    liveRoomAnswerMode.value = item.liveAction
    liveRoomExecutionError.value = false
    liveRoomExecutionStatus.value = item.liveAction === 'quick'
      ? '已进入抢答模式 · 输入后立即生成并播出'
      : '已进入回答模式 · 输入后进入待打断队列'
    input.value = input.value.slice(0, trigger.index) + item.insertText
    dismissedSuggestionInput.value = input.value
    suggestionIndex.value = 0
    focusActiveComposer()
    return
  }

  if (currentDomain.value === 'live-room') {
    liveRoomAnswerMode.value = null
    liveRoomExecutionStatus.value = ''
    liveRoomExecutionError.value = false
  }
  input.value = input.value.slice(0, trigger.index) + item.insertText
  dismissedSuggestionInput.value = input.value
  suggestionIndex.value = 0
  focusActiveComposer()
}

function closeSuggestions() {
  dismissedSuggestionInput.value = input.value
  suggestionIndex.value = 0
}

function handleComposerKeydown(event: KeyboardEvent, source: ComposerSource) {
  activeComposer.value = source

  if (showSuggestions(source)) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      suggestionIndex.value =
        (suggestionIndex.value + 1) % suggestions.value.length
      return
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault()
      suggestionIndex.value =
        (suggestionIndex.value - 1 + suggestions.value.length) %
        suggestions.value.length
      return
    }
    if (event.key === 'Enter' && !event.isComposing) {
      event.preventDefault()
      const item = suggestions.value[suggestionIndex.value]
      if (item) selectSuggestion(item)
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      closeSuggestions()
      return
    }
  }

  if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return
  event.preventDefault()
  void send()
}

function expand() {
  expanded.value = true
  activeComposer.value = 'dock'
  void nextTick(() => inputEl.value?.focus())
}

function collapse() {
  expanded.value = false
  drawerOpen.value = false
  activeComposer.value = null
}

function openDrawer() {
  drawerOpen.value = true
  expanded.value = true
  activeComposer.value = 'drawer'
  void nextTick(async () => {
    await scrollChatToBottom()
    drawerInputEl.value?.focus()
  })
}

function toggleDrawer() {
  if (drawerOpen.value) {
    drawerOpen.value = false
    activeComposer.value = 'dock'
    void nextTick(() => inputEl.value?.focus())
    return
  }
  openDrawer()
}

function clampDockCoordinates(
  left: number,
  top: number,
  width: number,
  height: number,
) {
  const maxLeft = Math.max(
    DOCK_VIEWPORT_MARGIN,
    window.innerWidth - width - DOCK_VIEWPORT_MARGIN,
  )
  const maxTop = Math.max(
    DOCK_VIEWPORT_MARGIN,
    window.innerHeight - height - DOCK_VIEWPORT_MARGIN,
  )
  return {
    left: Math.min(Math.max(DOCK_VIEWPORT_MARGIN, left), maxLeft),
    top: Math.min(Math.max(DOCK_VIEWPORT_MARGIN, top), maxTop),
  }
}

function keepDockInsideViewport() {
  if (!dockPosition.value || !dockEl.value) return
  const rect = dockEl.value.getBoundingClientRect()
  dockPosition.value = clampDockCoordinates(
    dockPosition.value.left,
    dockPosition.value.top,
    rect.width,
    rect.height,
  )
}

function moveDockDrag(event: PointerEvent) {
  if (!dockDragging.value || dockDragPointerID !== event.pointerId) return

  const deltaX = event.clientX - dockDragStartX
  const deltaY = event.clientY - dockDragStartY
  if (!dockDragMoved && Math.hypot(deltaX, deltaY) >= 4) {
    dockDragMoved = true
  }

  dockPosition.value = clampDockCoordinates(
    dockDragOriginLeft + deltaX,
    dockDragOriginTop + deltaY,
    dockDragWidth,
    dockDragHeight,
  )
}

function stopDockDrag(event?: PointerEvent) {
  if (
    event &&
    dockDragPointerID !== null &&
    event.pointerId !== dockDragPointerID
  ) {
    return
  }
  dockDragging.value = false
  dockDragPointerID = null
  window.removeEventListener('pointermove', moveDockDrag)
  window.removeEventListener('pointerup', stopDockDrag)
  window.removeEventListener('pointercancel', stopDockDrag)
}

function startDockDrag(event: PointerEvent) {
  if (!expanded.value || drawerOpen.value || !dockEl.value) return
  if (event.pointerType === 'mouse' && event.button !== 0) return

  const target = event.target as HTMLElement | null
  const orbHandle = Boolean(target?.closest('.system-agent-orb.mini'))
  if (
    !orbHandle &&
    target?.closest(
      'input, textarea, select, a, button, .system-agent-suggestion-menu',
    )
  ) {
    return
  }

  const rect = dockEl.value.getBoundingClientRect()
  dockDragPointerID = event.pointerId
  dockDragStartX = event.clientX
  dockDragStartY = event.clientY
  dockDragOriginLeft = rect.left
  dockDragOriginTop = rect.top
  dockDragWidth = rect.width
  dockDragHeight = rect.height
  dockDragMoved = false
  dockDragging.value = true
  dockPosition.value = { left: rect.left, top: rect.top }

  event.preventDefault()
  window.addEventListener('pointermove', moveDockDrag)
  window.addEventListener('pointerup', stopDockDrag)
  window.addEventListener('pointercancel', stopDockDrag)
}

function handleDockMiniOrbClick(event: MouseEvent) {
  if (dockDragMoved) {
    event.preventDefault()
    event.stopPropagation()
    dockDragMoved = false
    return
  }
  collapse()
}

function handleDockViewportResize() {
  if (!dockPosition.value) return
  void nextTick(keepDockInsideViewport)
}

function historyPayload(domain: AgentDomain) {
  const scope = conversationScopeForDomain(domain)
  return messages.value
    .filter((item) => item.domain === domain && (item.conversationScope || conversationScopeForDomain(item.domain)) === scope)
    .slice(-10)
    .map((item) => ({ role: item.role, text: item.text }))
}

async function scrollChatToBottom() {
  await nextTick()
  if (chatEl.value) {
    chatEl.value.scrollTop = chatEl.value.scrollHeight
  }
}

function sanitizeTerminalAgentText(value: string) {
  if (!isTerminalCustomer.value) return value
  let text = String(value || '')
  const replacements: Array<[RegExp, string]> = [
    [/\bL[123]\b/gi, ''],
    [/overrides?/gi, ''],
    [/用户层(?:规则|配置|策略)?/g, ''],
    [/行业层(?:规则|配置|策略)?/g, ''],
    [/规则层(?:规则|配置|策略)?/g, ''],
    [/策略合并(?:逻辑|结果)?/g, ''],
    [/内部版本(?:结构|号|信息)?/g, ''],
    [/模型(?:调用)?链路/g, ''],
    [/底层(?:业务)?逻辑/g, ''],
  ]
  for (const [pattern, replacement] of replacements) text = text.replace(pattern, replacement)
  text = text
    .replace(/不(?:会|再)?写入\s*(?:或|、|\/|，|,|\s)*/g, '不会修改其他设置。')
    .replace(/(?:内部配置|系统配置)\s*(?:或|、|\/|，|,)+\s*(?:内部配置|系统配置)/g, '其他设置')
    .replace(/\s{2,}/g, ' ')
    .replace(/\n\s*\n\s*\n+/g, '\n\n')
    .trim()
  if (!text) return '已根据你的要求完成处理。'
  return text
}

function pushAgentMessage(
  domain: AgentDomain,
  text: string,
  action?: SystemAgentActionPreview,
) {
  messages.value.push({ role: 'agent', domain, text: sanitizeTerminalAgentText(text), action, conversationScope: conversationScopeForDomain(domain) })
  void scrollChatToBottom()
}

function readAdminPolicyContext() {
  let layer: 'L1' | 'L2' = 'L1'
  let industryCode = ''
  const raw = window.localStorage.getItem('system-agent-live-policy-context')
  if (!raw) return { layer, industryCode }

  try {
    const value = JSON.parse(raw) as {
      layer?: unknown
      industry_code?: unknown
    }
    if (value.layer === 'L2') layer = 'L2'
    if (typeof value.industry_code === 'string') {
      industryCode = value.industry_code.trim()
    }
  } catch {
    window.localStorage.removeItem('system-agent-live-policy-context')
  }
  return { layer, industryCode }
}

function resolveExplicitAdminPolicyIntent(value: string) {
  if (!isInternalAgentProfile.value) return null
  const compact = value.replace(/\s+/g, '')
  if (
    !/(配置|新增|添加|修改|调整|生成|创建|建立|删除|移除|保存|草稿|发布|回滚)/.test(compact)
  ) {
    return null
  }

  const upper = compact.toUpperCase()
  const explicitL1 =
    /只(?:修改|配置|处理)?L1/.test(upper) ||
    /当前L1/.test(upper) ||
    /L1(?:系统|全局|底层|规则|草稿)/.test(upper) ||
    /(?:规则层|第一层|最底层|第?底层|底层规则|系统全局规则)/.test(compact)
  const explicitL2 =
    /只(?:修改|配置|处理)?L2/.test(upper) ||
    /当前L2/.test(upper) ||
    /L2(?:行业|规则|草稿)/.test(upper) ||
    /(?:行业层|第二层|行业默认规则)/.test(compact)

  let layer: 'L1' | 'L2' | null = null
  if (explicitL1 && !/只(?:修改|配置|处理)?L2/.test(upper)) {
    layer = 'L1'
  } else if (explicitL2 && !/只(?:修改|配置|处理)?L1/.test(upper)) {
    layer = 'L2'
  } else if (upper.includes('L1') && !upper.includes('L2')) {
    layer = 'L1'
  } else if (upper.includes('L2') && !upper.includes('L1')) {
    layer = 'L2'
  }
  if (!layer) return null

  const current = readAdminPolicyContext()
  return {
    layer,
    industryCode:
      layer === 'L2' && current.layer === 'L2'
        ? current.industryCode || 'general'
        : layer === 'L2'
          ? 'general'
          : '',
  }
}

function persistAdminPolicyContext(layer: 'L1' | 'L2', industryCode = '') {
  window.localStorage.setItem(
    'system-agent-live-policy-context',
    JSON.stringify({
      layer,
      industry_code: layer === 'L2' ? industryCode || 'general' : '',
    }),
  )
  window.localStorage.setItem('system-agent-live-strategy-internal-mode', 'policy')
  internalLiveStrategyMode.value = 'policy'
}

function notifyAdminPolicyUpdated(layer: 'L1' | 'L2', industryCode = '') {
  window.dispatchEvent(
    new CustomEvent('live-policy-admin-updated', {
      detail: {
        layer,
        industry_code: layer === 'L2' ? industryCode || 'general' : '',
      },
    }),
  )
}

async function resolveLiveStrategyRoomID() {
  const stored = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
  if (stored > 0) return stored

  const response = await getRooms()
  const roomId = response.items[0]?.id || 0
  if (roomId > 0) {
    window.localStorage.setItem('system-agent-live-room-id', String(roomId))
  }
  return roomId
}

function agentConfigRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return { ...(value as Record<string, unknown>) }
}

function agentConfigArray(value: unknown): unknown[] {
  return Array.isArray(value) ? [...value] : []
}

function agentRoomScopedRecord(
  source: Record<string, unknown> | undefined,
  roomId: number,
  key: string,
  entry: Record<string, unknown>,
) {
  const root = agentConfigRecord(source)
  const roomMap = agentConfigRecord(root.rooms)
  const roomKey = String(roomId)
  const roomConfig = agentConfigRecord(roomMap[roomKey])
  return {
    ...root,
    rooms: {
      ...roomMap,
      [roomKey]: {
        ...roomConfig,
        [key]: [...agentConfigArray(roomConfig[key]), entry],
      },
    },
  }
}

async function persistLiveStrategyModeDraft(roomId: number, value: string) {
  if (liveStrategyMode.value === 'basic' || liveStrategyMode.value === 'strategy') return null
  const versions = await getLiveAgentConfigVersions()
  const latest =
    versions.find((item) => item.lifecycle_status === 'draft') ||
    versions.find((item) => item.lifecycle_status === 'active') ||
    versions[0]
  const now = new Date().toISOString()
  const payload: {
    layer3?: Record<string, unknown>
    style_profile?: Record<string, unknown>
    speech_config?: Record<string, unknown>
  } = {}

  if (liveStrategyMode.value === 'script') {
    const compact = value.replace(/\s/g, '')
    const executionMode =
      compact.includes('100%原话') || compact.includes('照原文') || compact.includes('原话锁定')
        ? 'exact'
        : 'intent'
    payload.layer3 = agentRoomScopedRecord(latest?.layer3, roomId, 'fixed_scripts', {
      text: value,
      execution_mode: executionMode,
      created_at: now,
    })
  } else if (liveStrategyMode.value === 'anchor') {
    payload.style_profile = agentRoomScopedRecord(latest?.style_profile, roomId, 'training_entries', {
      text: value,
      created_at: now,
    })
  } else if (liveStrategyMode.value === 'voice') {
    payload.speech_config = agentRoomScopedRecord(latest?.speech_config, roomId, 'instruction_entries', {
      text: value,
      created_at: now,
    })
  }

  return createLiveAgentConfigDraft(payload)
}

function agentRoomSelectedVoice(
  source: Record<string, unknown> | undefined,
  roomId: number,
  voice: Record<string, unknown>,
) {
  const root = agentConfigRecord(source)
  const roomMap = agentConfigRecord(root.rooms)
  const roomKey = String(roomId)
  const roomConfig = agentConfigRecord(roomMap[roomKey])
  return {
    ...root,
    rooms: {
      ...roomMap,
      [roomKey]: {
        ...roomConfig,
        selected_voice: {
          ...voice,
          selected_at: new Date().toISOString(),
        },
      },
    },
  }
}

async function tryBindRequestedVoice(roomId: number, value: string) {
  const compact = value.replace(/\s+/g, '').toLowerCase()
  if (!/(使用|用|换成|切换|设为|选择|改成)/.test(compact)) return null

  const [officialResponse, profiles, versions] = await Promise.all([
    getLiveOfficialVoices(),
    getLiveVoiceProfiles(),
    getLiveAgentConfigVersions(),
  ])
  const latest =
    versions.find((item) => item.lifecycle_status === 'draft') ||
    versions.find((item) => item.lifecycle_status === 'active') ||
    versions[0]

  const official = [...(officialResponse.items || [])]
    .sort((a, b) => Math.max(b.name.length, b.id.length) - Math.max(a.name.length, a.id.length))
    .find((item) => compact.includes(item.name.toLowerCase()) || compact.includes(item.id.toLowerCase()))
  if (official) {
    const draft = await createLiveAgentConfigDraft({
      speech_config: agentRoomSelectedVoice(latest?.speech_config, roomId, {
        source: 'official',
        provider: 'aliyun_qwen',
        name: official.name,
        voice_id: official.id,
        model: official.model,
      }),
    })
    await activateLiveAgentConfigVersion(draft.id)
    return '已将当前直播间声音切换为“' + official.name + '”。后续该直播间会使用这个声音。'
  }

  const profile = [...profiles]
    .filter((item) => item.clone_status === 'ready' && item.voice_id)
    .sort((a, b) => b.name.length - a.name.length)
    .find((item) => compact.includes(item.name.toLowerCase()))
  if (profile) {
    const draft = await createLiveAgentConfigDraft({
      speech_config: agentRoomSelectedVoice(latest?.speech_config, roomId, {
        source: 'clone',
        provider: profile.provider,
        name: profile.name,
        voice_id: profile.voice_id,
        profile_id: profile.id,
        model: String(profile.config?.target_model || 'qwen3-tts-vc-2026-01-22'),
      }),
    })
    await activateLiveAgentConfigVersion(draft.id)
    return '已将当前直播间声音切换为“' + profile.name + '”。这个克隆声音仍可在你的其它直播间继续使用。'
  }

  return null
}

function resolveLiveSupportRoomID() {
  const stored = Number(
    window.localStorage.getItem('system-agent-live-support-room-id') || 0,
  )
  return stored > 0 ? stored : 0
}

function isExplicitAnchorTrainingIntent(value: string) {
  const compact = value.replace(/\s+/g, '')
  if (/[?？]$/.test(compact)) return false
  if (
    /(怎么|如何|为什么|是什么|说明|建议|分析|先不要|不要生成|不要保存)/.test(compact)
  ) {
    return false
  }
  return /(训练|学习|新增|添加|保存|生成).*(主播|风格|语气|节奏|表达)|(?:主播|风格|语气|节奏|表达).*(训练|学习|新增|添加|保存|生成)/.test(compact)
}

function handleLiveStrategyModeEvent(event: Event) {
  const mode = (event as CustomEvent<{ mode?: string }>).detail?.mode
  internalLiveStrategyMode.value =
    mode === 'support' || mode === 'learning' ? mode : 'policy'
}

function handleLiveStrategyContextEvent(event: Event) {
  const detail = (event as CustomEvent<{ room_id?: number; mode?: string }>).detail
  if (detail?.room_id && detail.room_id > 0) {
    window.localStorage.setItem('system-agent-live-room-id', String(detail.room_id))
  }
  const mode = detail?.mode
  liveStrategyMode.value =
    mode === 'basic' || mode === 'anchor' || mode === 'script' || mode === 'voice'
      ? mode
      : 'strategy'
  window.localStorage.setItem('system-agent-live-mode', liveStrategyMode.value)
}

function handleLiveSupportContextEvent(event: Event) {
  const detail = (event as CustomEvent<{ mode?: string }>).detail
  if (detail?.mode === 'anchor' || detail?.mode === 'voice') {
    liveSupportMode.value = detail.mode
    return
  }
  liveSupportMode.value = 'strategy'
}

function handleExternalPrefill(event: Event) {
  const detail = (event as CustomEvent<{ text?: string; open?: boolean }>).detail
  const text = detail?.text?.trim()
  if (!text) return
  input.value = text
  expanded.value = true
  if (detail.open !== false) {
    drawerOpen.value = true
    activeComposer.value = 'drawer'
  } else {
    activeComposer.value = 'dock'
  }
  dismissedSuggestionInput.value = ''
  focusActiveComposer()
}

function handleLivePolicyTestModeEvent(event: Event) {
  const detail = (
    event as CustomEvent<{
      active?: boolean
      layer?: 'L1' | 'L2'
      industry_code?: string
    }>
  ).detail

  if (!detail?.active) {
    livePolicyTestMode.value = {
      ...livePolicyTestMode.value,
      active: false,
    }
    livePolicyTestHistory.value = []
    return
  }

  const nextLayer: 'L1' | 'L2' = detail.layer === 'L2' ? 'L2' : 'L1'
  const nextIndustryCode =
    nextLayer === 'L2'
      ? String(detail.industry_code || 'general').trim() || 'general'
      : ''
  const contextChanged =
    !livePolicyTestMode.value.active ||
    livePolicyTestMode.value.layer !== nextLayer ||
    livePolicyTestMode.value.industryCode !== nextIndustryCode

  livePolicyTestMode.value = {
    active: true,
    layer: nextLayer,
    industryCode: nextIndustryCode,
  }
  if (contextChanged) {
    livePolicyTestHistory.value = []
  }
  expanded.value = true
  drawerOpen.value = false
  activeComposer.value = 'dock'
  void nextTick(focusActiveComposer)
}

async function sendLivePolicyTest(value: string) {
  input.value = ''
  dismissedSuggestionInput.value = ''
  activeComposer.value = null
  busy.value = true
  busyDomain.value = 'live-policy-admin'
  window.dispatchEvent(
    new CustomEvent('live-policy-test-loading', { detail: { loading: true } }),
  )

  try {
    const result = await testLivePolicyAdmin({

      layer: livePolicyTestMode.value.layer,
      industry_code:
        livePolicyTestMode.value.layer === 'L2'
          ? livePolicyTestMode.value.industryCode || 'general'
          : undefined,
      message: value,
      history: livePolicyTestHistory.value.slice(-12),
    })
    livePolicyTestHistory.value.push(
      { role: 'user', text: value },
      { role: 'agent', text: result.reply },
    )
    livePolicyTestHistory.value = livePolicyTestHistory.value.slice(-12)
    window.dispatchEvent(
      new CustomEvent('live-policy-test-result', {
        detail: {
          question: value,
          result,
          history: livePolicyTestHistory.value.slice(-12),
        },
      }),
    )
  } catch (error) {
    window.dispatchEvent(
      new CustomEvent('live-policy-test-error', {
        detail: {
          question: value,
          message: error instanceof Error ? error.message : '规则测试失败',
        },
      }),
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    window.dispatchEvent(
      new CustomEvent('live-policy-test-loading', { detail: { loading: false } }),
    )
    activeComposer.value = 'dock'
    void nextTick(focusActiveComposer)
  }
}

function isPolicyLearningAbsorbIntent(value: string) {
  const compact = value.replace(/s+/g, '')
  if (/(?:不要|先不|暂不|别)(?:吸收|沉淀|学习|保存)/.test(compact)) return false
  if (
    [
      '吸收这次调教',
      '吸收本次调教',
      '沉淀这次调教',
      '沉淀本次调教',
      '把这次调教吸收',
      '把这次调教沉淀',
      '记住这次调教',
      '保存这次学习',
    ].some((item) => compact.includes(item))
  ) {
    return true
  }
  return /(?:吸收|沉淀|记住|学习).*(?:这次|本次|当前).*(?:调教|回复|经验)|(?:这次|本次|当前).*(?:调教|回复|经验).*(?:吸收|沉淀|记住|学习)/.test(
    compact,
  )
}

function extractPolicyLearningConversation(history: AgentHistoryItem[]) {
  const usable = history.filter((item) => item.text.trim())
  const firstUser = usable.find((item) => item.role === 'user')
  const lastAgent = [...usable].reverse().find((item) => item.role === 'agent')
  if (!firstUser || !lastAgent) return null
  const userTurns = usable.filter((item) => item.role === 'user')
  return {
    question: firstUser.text.trim(),
    finalReply: lastAgent.text.trim(),
    feedback:
      userTurns.length > 1
        ? userTurns[userTurns.length - 1].text.trim()
        : '人工确认当前回复满意，提交吸收。',
  }
}

function policyLearningCandidateMessage(candidate: LivePolicyLearningCandidate) {
  const recommendation = candidate.absorb_recommended ? '建议吸收' : '建议人工判断'
  return (
    '已提交调教学习。\n\n' +
    '智能体判断：' +
    recommendation +
    ' → ' +
    candidate.recommended_layer +
    '（' +
    candidate.confidence +
    '%）\n' +
    candidate.recommendation_reason +
    '\n\n准备沉淀的规则：' +
    candidate.rule_title +
    '\n' +
    candidate.rule_text +
    '\n\n当前只是“待吸收”，还没有自动发布到正式规则。'
  )
}

async function createPolicyLearningFromHistory(
  sourceLayer: 'L1' | 'L2' | 'L3',
  history: AgentHistoryItem[],
  options: {
    industryCode?: string
    roomId?: number
    feedback?: string
  } = {},
) {
  const conversation = extractPolicyLearningConversation(history)
  if (!conversation) {
    throw new Error('当前还没有完整的“问题 → 满意回复”对话，先完成至少一轮调教再吸收。')
  }
  return createLivePolicyLearningCandidate({
    source_layer: sourceLayer,
    industry_code: options.industryCode,
    room_id: options.roomId,
    question: conversation.question,
    final_reply: conversation.finalReply,
    feedback: options.feedback?.trim() || conversation.feedback,
    history: history.slice(-16),
  })
}

async function absorbLivePolicyTest(value: string) {
  const history = livePolicyTestHistory.value.slice(-16)
  input.value = ''
  dismissedSuggestionInput.value = ''
  activeComposer.value = null
  busy.value = true
  busyDomain.value = 'live-policy-admin'
  try {
    const candidate = await createPolicyLearningFromHistory(
      livePolicyTestMode.value.layer,
      history,
      {
        industryCode:
          livePolicyTestMode.value.layer === 'L2'
            ? livePolicyTestMode.value.industryCode || 'general'
            : undefined,
        feedback: value,
      },
    )
    window.dispatchEvent(
      new CustomEvent('live-policy-learning-created', {
        detail: { candidate },
      }),
    )
    pushAgentMessage('live-policy-admin', policyLearningCandidateMessage(candidate))
  } catch (error) {
    pushAgentMessage(
      'live-policy-admin',
      error instanceof Error ? '吸收失败：' + error.message : '这次调教暂时无法吸收。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    activeComposer.value = 'dock'
    void nextTick(focusActiveComposer)
  }
}

async function createPolicyLearningForDomain(
  domain: AgentDomain,
  history: AgentHistoryItem[],
  feedback: string,
) {
  if (domain === 'live-policy-admin') {
    const policyContext = readAdminPolicyContext()
    return createPolicyLearningFromHistory(policyContext.layer, history, {
      industryCode:
        policyContext.layer === 'L2' ? policyContext.industryCode || 'general' : undefined,
      feedback,
    })
  }
  if (domain === 'live-strategy') {
    const roomId = await resolveLiveStrategyRoomID()
    if (!roomId) throw new Error('当前没有可用直播间，不能沉淀为直播间学习。')
    return createPolicyLearningFromHistory('L3', history, {
      roomId,
      feedback,
    })
  }
  if (domain === 'live-support' && liveSupportMode.value === 'strategy') {
    const roomId = resolveLiveSupportRoomID()
    if (!roomId) throw new Error('当前没有选中的客户授权直播间。')
    return createPolicyLearningFromHistory('L3', history, {
      roomId,
      feedback,
    })
  }
  return null
}

function syncAgentWelcomeMessage() {
  const firstSystemMessage = messages.value.find(
    (item) => item.domain === 'system' && item.role === 'agent',
  )
  if (!firstSystemMessage) return
  firstSystemMessage.text = `我是${assistantName.value}。你在系统里走到哪里，我就切换到那个业务工作域；所有动作仍受当前账号权限和原有审批规则约束。`
}

async function loadAgentBranding() {
  try {
    const config = await getPublicSystemConfig()
    internalAgentName.value = config.internal_agent_name?.trim() || '小蓝工作搭子'
    clientAgentName.value = config.client_agent_name?.trim() || '小蓝直播搭子'
  } catch {
    internalAgentName.value = internalAgentName.value.trim() || '小蓝工作搭子'
    clientAgentName.value = clientAgentName.value.trim() || '小蓝直播搭子'
  }
  syncAgentWelcomeMessage()
}

async function loadSystemAgentContext() {
  if (!actor.value) return
  try {
    systemContext.value = isInternalAgentProfile.value
      ? await getInternalAgentContext()
      : await getClientAgentContext()
  } catch {
    systemContext.value = {
      capabilities: [],
      departments: [],
    }
  }
}

onMounted(() => {
  window.addEventListener('system-agent:prefill', handleExternalPrefill)
  window.addEventListener('system-config-updated', loadAgentBranding)
  window.addEventListener('system-agent-live-strategy-mode', handleLiveStrategyModeEvent)
  window.addEventListener('system-agent-live-strategy-context', handleLiveStrategyContextEvent)
  window.addEventListener('system-agent-live-support-context', handleLiveSupportContextEvent)
  window.addEventListener('live-policy-test-mode', handleLivePolicyTestModeEvent)
  window.addEventListener('live-answer-reference-open', handleAnswerReferenceSelected)
  window.addEventListener('live-answer-reference-selected', handleAnswerReferenceSelected)
  window.addEventListener('resize', handleDockViewportResize)
  void loadAgentBranding()
  void loadSystemAgentContext()
})

watch(
  () => actor.value?.user_id || 0,
  (userId, previousUserId) => {
    if (!userId || userId === previousUserId) return
    restoreAgentChatHistory(userId)
  },
  { immediate: true },
)

watch(
  messages,
  () => {
    const userId = actor.value?.user_id || 0
    if (!userId) return
    for (const item of messages.value) {
      if (!item.conversationScope) item.conversationScope = conversationScopeForDomain(item.domain)
    }
    persistAgentChatHistory(userId)
  },
  { deep: true },
)

watch(
  coachingSession,
  () => {
    const userId = actor.value?.user_id || 0
    if (userId) persistAgentChatHistory(userId)
  },
  { deep: true },
)

watch(expanded, () => {
  if (!dockPosition.value) return
  void nextTick(keepDockInsideViewport)
})

watch(drawerOpen, (open, previousOpen) => {
  if (!open || previousOpen) return
  void scrollChatToBottom()
})

watch(
  () => actor.value?.role,
  (role, previousRole) => {
    if (!role || role === previousRole) return
    latestSystemResponse.value = null
    systemContext.value = { capabilities: [], departments: [] }
    void loadSystemAgentContext()
  },
)

watch(currentDomain, (domain) => {
  if (domain === 'live-room') return
  liveRoomAnswerMode.value = null
  liveRoomExecutionStatus.value = ''
  liveRoomExecutionError.value = false
  answerReferencePending.value = null
  answerReferenceSession.value = null
  setAnswerReferencePicking(false)
})

onBeforeUnmount(() => {
  if (actor.value?.user_id) persistAgentChatHistory(actor.value.user_id)
  stopDockDrag()
  window.removeEventListener('system-agent:prefill', handleExternalPrefill)
  window.removeEventListener('system-config-updated', loadAgentBranding)
  window.removeEventListener('system-agent-live-strategy-mode', handleLiveStrategyModeEvent)
  window.removeEventListener('system-agent-live-strategy-context', handleLiveStrategyContextEvent)
  window.removeEventListener('system-agent-live-support-context', handleLiveSupportContextEvent)
  window.removeEventListener('live-policy-test-mode', handleLivePolicyTestModeEvent)
  window.removeEventListener('live-answer-reference-open', handleAnswerReferenceSelected)
  window.removeEventListener('live-answer-reference-selected', handleAnswerReferenceSelected)
  setAnswerReferencePicking(false)
  window.removeEventListener('resize', handleDockViewportResize)
})

function resolveNavigationIntent(value: string) {
  const compact = value.replace(/\s+/g, '')
  const hasNavigationVerb = ['打开', '进入', '跳转', '导航到', '带我到', '去到'].some((verb) =>
    compact.includes(verb),
  )
  if (!hasNavigationVerb) return null

  return [...navigationTargets.value]
    .sort((a, b) => b.title.length - a.title.length)
    .find((item) => compact.includes(item.title.replace(/\s+/g, ''))) || null
}

function detectSystemTaskIntent(value: string): SystemTaskKey | null {
  if (!isInternalAgentProfile.value) return null
  const compact = value.replace(/\s+/g, '')
  if (
    /(?:新增|添加|创建|录入|招入).*(?:员工|人员)|(?:员工|人员).*(?:新增|添加|创建|录入)/.test(compact)
  ) {
    return 'create_staff_employee'
  }
  if (
    /(?:新增|创建|建立|做一个|配置).*(?:营销活动|活动营销)|(?:营销活动|活动营销).*(?:新增|创建|建立|配置)/.test(compact)
  ) {
    return 'create_marketing_campaign'
  }
  return null
}

function isClientBoundaryIntent(value: string) {
  if (isInternalAgentProfile.value) return false
  const compact = value.replace(/\s+/g, '').toLowerCase()
  return [
    '新增员工',
    '添加员工',
    '创建员工',
    '员工名单',
    '所有部门',
    '内部部门',
    '组织架构',
    '角色权限',
    '系统提示',
    'systemprompt',
    '隐藏工具',
    '内部工具',
    '系统设定',
    '财务与结算',
    '后台财务',
    '权限审计',
  ].some((keyword) => compact.includes(keyword))
}

function isSystemCapabilityIntent(value: string) {
  const compact = value.replace(/\s+/g, '')
  if (
    /(?:查询|查|看看|查看).*(?:员工|部门|岗位)|(?:员工|部门|岗位).*(?:查询|查|查看)/.test(compact)
  ) {
    return true
  }
  if (compact.includes('权限说明') || compact.includes('账号信息')) {
    return true
  }
  if (
    systemContext.value.capabilities.some((item) =>
      compact.includes(item.replace(/\s+/g, '')),
    )
  ) {
    return true
  }
  return navigationTargets.value.some((item) =>
    compact.includes(item.title.replace(/\s+/g, '')),
  )
}

function cancelActiveSystemTask(value: string) {
  if (!activeSystemTask.value) return false
  const compact = value.replace(/\s+/g, '')
  if (!['取消', '算了', '不用了', '结束任务', '取消这个任务'].some((item) => compact.includes(item))) {
    return false
  }
  activeSystemTask.value = null
  systemTaskHistory.value = []
  return true
}

function startOrContinueSystemTask(value: string) {
  const detected = detectSystemTaskIntent(value)
  if (!detected) return activeSystemTask.value
  if (activeSystemTask.value !== detected) {
    activeSystemTask.value = detected
    systemTaskHistory.value = []
  }
  return detected
}

function resolveLiveRoomExecution(value: string) {
  const compact = value.replace(/\s+/g, '').toLowerCase()
  const verbatim = /100%|百分百|一字不改|一个字不要改|原封不动|照原话|按原话|必须原文|逐字|严格.*(?:原话|照说|照着说|按照)/.test(compact)
  if (!verbatim) {
    return { executionMode: 'intent' as const, fixedText: '' }
  }

  let fixedText = value.trim()
  const colon = fixedText.search(/[:：]/)
  if (colon >= 0 && colon < Math.min(fixedText.length - 1, 40)) {
    const candidate = fixedText.slice(colon + 1).trim()
    if (candidate) fixedText = candidate
  } else {
    fixedText = fixedText
      .replace(/^(?:请)?\s*(?:严格\s*)?(?:100%|百分百)?\s*(?:按照|按|照)?\s*(?:这个|以下|下面)?\s*(?:原话|文字|内容|回答|话术)?\s*(?:来说|说|播|回答)?\s*[，,。.!！]?\s*/i, '')
      .trim() || value.trim()
  }
  return { executionMode: 'verbatim' as const, fixedText }
}

async function sendLiveRoomAnswer(value: string, mode: LiveRoomAnswerMode) {
  liveRoomWorkMode.value = 'execution'
  const roomId = Number(route.params.id)
  if (!roomId) {
    pushAgentMessage('live-room', '当前页面没有有效直播间编号，不能提交现场回答。')
    return
  }

  const execution = resolveLiveRoomExecution(value)
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = mode === 'quick' ? '正在提交抢答…' : '正在提交回答…'
  input.value = mode === 'quick' ? '/抢答 ' : '/回答 '
  dismissedSuggestionInput.value = input.value
  activeComposer.value = 'dock'
  busy.value = true
  busyDomain.value = 'live-room'
  try {
    const result = await enqueueRoomManualAgentDecision(roomId, {
      question: value,
      title: mode === 'quick' ? '智能体输入抢答' : '智能体输入回答',
      summary: mode === 'quick'
        ? '直播操作者通过智能体输入框发起抢答，要求立即生成并播出'
        : '直播操作者通过智能体输入框提交回答，由监控 Agent 协调待打断时机',
      reply_hint: value,
      force_reopen: mode === 'quick',
      manual_action: mode,
      manual_origin: 'agent_input',
      execution_mode: execution.executionMode,
      fixed_text: execution.fixedText || undefined,
      ttl_seconds: mode === 'quick' ? 180 : 600,
    })
    const modeText = mode === 'quick' ? '抢答' : '回答'
    const executionText = execution.executionMode === 'verbatim' ? ' · 100%原话' : ''
    const queueText = result.merged
      ? '已融合到现有待执行任务'
      : mode === 'quick'
        ? '已进入最高优先执行区'
        : '已进入待打断队列'
    pushAgentMessage('live-room', modeText + executionText + '：' + queueText + '。')
    liveRoomExecutionError.value = false
    liveRoomExecutionStatus.value = modeText + executionText + ' · ' + queueText
  } catch (error) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '现场回答提交失败'
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '现场回答提交失败：' + error.message : '现场回答提交失败。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    void nextTick(focusActiveComposer)
  }
}

async function sendLiveRoomSimulation(value: string) {
  liveRoomWorkMode.value = 'test'
  const roomId = Number(route.params.id)
  if (!roomId) {
    pushAgentMessage('live-room', '当前页面没有有效直播间编号，不能进行真实链路测试。')
    return
  }
  const question = value.trim()
  if (!question) return

  messages.value.push({
    role: 'user',
    domain: 'live-room',
    text: '【测试模拟观众】' + question,
    conversationScope: conversationScopeForDomain('live-room'),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  activeComposer.value = null
  drawerOpen.value = true
  busy.value = true
  busyDomain.value = 'live-room'
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = '测试模式 · Core Agent 正在按真实链路处理…'
  void scrollChatToBottom()

  try {
    let matched: AgentDecisionSimulationResult | null = null
    try {
      const enqueued = await enqueueRoomManualAgentDecision(roomId, {
        question,
        title: '测试模拟观众提问',
        summary: '测试模式模拟真实观众问题，只生成最终回答，不进入真实公屏、不播音',
        manual_action: 'answer',
        manual_origin: 'test_simulation',
        execution_mode: 'intent',
        ttl_seconds: 120,
      })
      const decisionId = enqueued.item?.id
      if (decisionId) {
        for (let attempt = 0; attempt < 40; attempt += 1) {
          await new Promise((resolve) => window.setTimeout(resolve, 500))
          const snapshot = await getRoomAgentDecisions(roomId)
          matched = (snapshot.simulation_results || []).find((item) => item.decision_id === decisionId) || null
          if (matched) break
        }
      }
    } catch {
      // 直播搭子未开始/刚重启时，仍用同一套真实回答生成与规则终审链路做离线测试。
    }
    if (!matched) {
      matched = await simulateRoomAgentDecision(roomId, { question })
    }

    const planText = matched.plan_name || '未绑定方案'
    const versionText = matched.user_layer_version ? 'V' + matched.user_layer_version : '无生效用户层版本'
    const modeText = matched.execution_mode || 'intent'
    pushAgentMessage(
      'live-room',
      '测试结果｜模拟观众问题\n' +
        '问题：' + matched.question + '\n' +
        '当前方案：' + planText + '\n' +
        '用户层：' + versionText + '\n' +
        '执行模式：' + modeText + '\n' +
        '最终回复：' + matched.reply + '\n' +
        '状态：仅测试，未播音',
    )
    liveRoomExecutionStatus.value = '测试完成 · 真实 Agent 链路已返回，未播音'
  } catch (error) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '测试失败'
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '测试失败：' + error.message : '测试失败，请稍后再试。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    void nextTick(focusActiveComposer)
    void scrollChatToBottom()
  }
}

async function sendAnswerReference(value: string) {
  liveRoomWorkMode.value = 'learning'
  const roomId = Number(route.params.id)
  const sessionState = answerReferenceSession.value
  const learningState = coachingSession.value
  const context = answerReferencePending.value || sessionState?.context
  if (!roomId || !sessionState || !learningState?.active || !context) return

  const userValue = value.trim()
  if (!userValue) return
  const firstTurn = sessionState.history.length === 0

  messages.value.push({
    role: 'user',
    domain: 'live-room',
    text: userValue,
    conversationScope: conversationScopeForDomain('live-room'),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  drawerTab.value = 'chat'
  drawerOpen.value = true
  expanded.value = true
  activeComposer.value = 'drawer'
  busy.value = true
  busyDomain.value = 'live-room'
  void scrollChatToBottom()

  try {
    const backendSessionId = await ensureAgentLearningBackendSession(learningState)
    const output = await createAgentLearningTurn(roomId, backendSessionId, userValue)
    const result = output.result
    disablePriorLearningCandidates(backendSessionId)
    const visibleReply = sanitizeTerminalAgentText(result.result_text)
    if (firstTurn) sessionState.initialReference = userValue
    sessionState.history.push(
      { role: 'user', text: userValue },
      { role: 'agent', text: visibleReply },
    )
    sessionState.history = sessionState.history.slice(-16)
    sessionState.latestReply = visibleReply
    learningState.backendSessionId = backendSessionId
    learningState.target = result.target || learningState.target
    learningState.history = sessionState.history.slice(-16)
    learningState.initialReference = sessionState.initialReference
    learningState.latestReply = visibleReply

    messages.value.push({
      role: 'agent',
      domain: 'live-room',
      text: visibleReply,
      conversationScope: conversationScopeForDomain('live-room'),
      answerReference: {
        learningSessionId: backendSessionId,
        question: context.question,
        strategy: visibleReply,
        reference: sessionState.initialReference,
        feedback: userValue,
        history: sessionState.history.slice(-16),
        canAdopt: true,
        coachingKind: 'reference_answer',
        target: result.target || context.topic || context.question,
        memoryType: result.memory_type,
        matchedMemoryItemId: result.matched_memory_item_id,
      },
    })
    liveRoomExecutionError.value = false
    liveRoomExecutionStatus.value = '智能体学习 · 继续输入就是继续修改，满意后点击“采用”'
  } catch (error) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '生成修正结果失败'
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '生成修正结果失败：' + error.message : '生成修正结果失败，请稍后再试。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    activeComposer.value = 'drawer'
    void nextTick(focusActiveComposer)
    void scrollChatToBottom()
  }
}

async function sendGeneralCoaching(value: string) {
  liveRoomWorkMode.value = 'learning'
  const sessionState = coachingSession.value
  const roomId = Number(route.params.id)
  if (!sessionState?.active || sessionState.kind !== 'general' || !roomId) return

  const userValue = value.trim()
  if (!userValue) return

  messages.value.push({
    role: 'user',
    domain: 'live-room',
    text: userValue,
    conversationScope: conversationScopeForDomain('live-room'),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  drawerTab.value = 'chat'
  drawerOpen.value = true
  expanded.value = true
  activeComposer.value = 'drawer'
  busy.value = true
  busyDomain.value = 'live-room'
  void scrollChatToBottom()

  try {
    const backendSessionId = await ensureAgentLearningBackendSession(sessionState)
    const output = await createAgentLearningTurn(roomId, backendSessionId, userValue)
    const result = output.result
    disablePriorLearningCandidates(backendSessionId)
    const visibleReply = sanitizeTerminalAgentText(result.result_text)
    sessionState.backendSessionId = backendSessionId
    sessionState.target = result.target || sessionState.target
    sessionState.history.push(
      { role: 'user', text: userValue },
      { role: 'agent', text: visibleReply },
    )
    sessionState.history = sessionState.history.slice(-16)
    sessionState.latestReply = visibleReply
    messages.value.push({
      role: 'agent',
      domain: 'live-room',
      text: visibleReply,
      conversationScope: conversationScopeForDomain('live-room'),
      answerReference: {
        learningSessionId: backendSessionId,
        question: sessionState.target,
        strategy: visibleReply,
        feedback: userValue,
        history: sessionState.history.slice(-16),
        canAdopt: true,
        coachingKind: 'general',
        target: result.target || sessionState.target,
        memoryType: result.memory_type,
        matchedMemoryItemId: result.matched_memory_item_id,
      },
    })
    liveRoomExecutionError.value = false
    liveRoomExecutionStatus.value = '智能体学习 · 继续输入就是继续修改，满意后点击“采用”'
  } catch (error) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '生成修正结果失败'
    pushAgentMessage('live-room', error instanceof Error ? '生成修正结果失败：' + error.message : '生成修正结果失败，请稍后再试。')
  } finally {
    busy.value = false
    busyDomain.value = null
    activeComposer.value = 'drawer'
    void nextTick(focusActiveComposer)
    void scrollChatToBottom()
  }
}

function parseAgentLearningPreviewTestIntent(value: string) {
  const text = value.trim()
  if (!text || text.startsWith('/')) return null
  const matched = text.match(/^(?:你)?(?:帮我)?(?:试试看|试试|测试|验证|测)(?:一下|下|一遍|看看|看下)?(?:[：:，,\s]*(.+?))?[。！!？?]*$/)
  if (!matched) return null
  return { question: (matched[1] || '').trim() }
}

async function sendAgentLearningPreviewTest(rawValue: string, question = '') {
  liveRoomWorkMode.value = 'test'
  const activeSession = coachingSession.value
  const roomId = Number(route.params.id)
  if (!activeSession?.active || !roomId) return

  messages.value.push({
    role: 'user',
    domain: 'live-room',
    text: rawValue.trim(),
    conversationScope: conversationScopeForDomain('live-room'),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  drawerTab.value = 'chat'
  drawerOpen.value = true
  expanded.value = true
  activeComposer.value = 'drawer'
  busy.value = true
  busyDomain.value = 'live-room'
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = '正在测试当前修正结果…'
  void scrollChatToBottom()

  try {
    const backendSessionId = await ensureAgentLearningBackendSession(activeSession)
    const result = await testAgentLearningSession(roomId, backendSessionId, question)
    pushAgentMessage(
      'live-room',
      '候选测试｜尚未采用\n' +
        '模拟观众：' + result.question + '\n' +
        '最终回复：' + result.reply + '\n' +
        '状态：仅测试当前修正结果，未采用、未播音',
    )
    liveRoomExecutionStatus.value = '候选测试完成 · 当前修正结果尚未采用'
  } catch (error) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '测试当前修正结果失败'
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '测试当前修正结果失败：' + error.message : '测试当前修正结果失败，请稍后再试。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    activeComposer.value = 'drawer'
    void nextTick(focusActiveComposer)
    void scrollChatToBottom()
  }
}

async function sendAgentLearningCompanionChat(rawValue: string) {
  liveRoomWorkMode.value = 'chat'
  const activeSession = coachingSession.value
  const roomId = Number(route.params.id)
  if (!activeSession?.active || !roomId) return

  const userValue = rawValue.trim()
  if (!userValue) return
  const history = historyPayload('live-room')
  messages.value.push({
    role: 'user',
    domain: 'live-room',
    text: userValue,
    conversationScope: conversationScopeForDomain('live-room'),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  drawerTab.value = 'chat'
  drawerOpen.value = true
  expanded.value = true
  activeComposer.value = 'drawer'
  busy.value = true
  busyDomain.value = 'live-room'
  void scrollChatToBottom()

  try {
    const response = await chatAgentLearningCompanion(roomId, {
      message: userValue,
      session_id: activeSession.backendSessionId,
      target: activeSession.target,
      latest_candidate: activeSession.latestReply || '',
      history,
    })
    pushAgentMessage('live-room', sanitizeTerminalAgentText(response.reply))
    liveRoomExecutionError.value = false
  } catch (error) {
    liveRoomExecutionError.value = true
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '这句话刚才没接上：' + error.message : '这句话刚才没接上，再说一次就好。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    activeComposer.value = 'drawer'
    void nextTick(focusActiveComposer)
    void scrollChatToBottom()
  }
}

async function routeActiveAgentLearningInput(rawValue: string) {
  const activeSession = coachingSession.value
  const roomId = Number(route.params.id)
  if (!activeSession?.active || !roomId) return false

  const previewTest = parseAgentLearningPreviewTestIntent(rawValue)
  if (previewTest) {
    await sendAgentLearningPreviewTest(rawValue, previewTest.question)
    return true
  }

  let intent: LiveRoomWorkMode = 'chat'
  try {
    const routed = await classifyAgentLearningMessage(roomId, {
      message: rawValue,
      session_id: activeSession.backendSessionId,
      target: activeSession.target,
      latest_candidate: activeSession.latestReply || '',
      current_mode: liveRoomWorkMode.value,
      learning_active: true,
      test_active: liveRoomTestMode.value,
      execution_active: Boolean(liveRoomAnswerMode.value),
      history: historyPayload('live-room').slice(-10),
    })
    intent = routed.intent
  } catch {
    // 分流服务异常时宁可聊天，也不能把普通闲聊误写成长期记忆。
    intent = 'chat'
  }

  if (intent === 'test') {
    await sendAgentLearningPreviewTest(rawValue)
    return true
  }
  if (intent === 'learning') {
    if (activeSession.kind === 'reference_answer' && (answerReferencePending.value || answerReferenceSession.value)) {
      await sendAnswerReference(rawValue)
    } else {
      await sendGeneralCoaching(rawValue)
    }
    return true
  }
  if (intent === 'execution') {
    await sendLiveRoomAnswer(extractLiveRoomExecutionContent(rawValue), 'answer')
    input.value = ''
    dismissedSuggestionInput.value = ''
    return true
  }

  await sendAgentLearningCompanionChat(rawValue)
  return true
}

function isLikelyAgentCorrectionIntent(value: string) {
  const text = value.trim()
  if (!text || text.startsWith('/')) return false
  return /(?:我|之前|刚才).{0,8}(?:说错|写错|打错|教错)|(?:说错了|写错了|打错了|教错了).{0,12}(?:应该|实际|正确)|^不是.{1,30}[，,、 ]?(?:是|应该是)/.test(text)
}

function extractLiveRoomExecutionContent(value: string) {
  const text = value.trim()
  const direct = text.match(/^(?:(?:你)?(?:帮我|替我)?(?:直接)?(?:回答|回复|抢答)(?:一下|下)?(?:这条(?:弹幕|问题)?|这个(?:问题|用户)?|他|她)?|给(?:这个用户|他|她)回复)[：:，,\s]*(.+)$/)
  return direct?.[1]?.trim() || text
}

function isPotentialLiveRoomWorkModeSwitch(value: string) {
  const text = value.trim()
  if (!text || text.startsWith('/')) return false
  if (isLikelyAgentCorrectionIntent(text)) return true
  return /(?:这个|刚才|之前).{0,10}(?:不对|有问题|生硬|太官方|不自然)|(?:改一下|改成|改为|换成|纠正|修正|再短一点|再简短一点|再自然一点|怎么回答|应该怎么说|测试下|测试一下|试试看|验证一下|帮我回答这条|回答这条弹幕|回复这条弹幕|给他回复|给她回复|抢答这条)/.test(text)
}

async function routeInactiveLiveRoomWorkModeInput(rawValue: string) {
  const roomId = Number(route.params.id)
  if (!roomId || !isPotentialLiveRoomWorkModeSwitch(rawValue)) {
    liveRoomWorkMode.value = 'chat'
    return false
  }

  let intent: LiveRoomWorkMode = 'chat'
  try {
    const routed = await classifyAgentLearningMessage(roomId, {
      message: rawValue,
      current_mode: liveRoomWorkMode.value,
      learning_active: false,
      test_active: liveRoomTestMode.value,
      execution_active: Boolean(liveRoomAnswerMode.value),
      history: historyPayload('live-room').slice(-10),
    })
    intent = routed.intent
  } catch {
    intent = isLikelyAgentCorrectionIntent(rawValue) ? 'learning' : 'chat'
  }

  if (intent === 'learning') {
    beginCoachingMode('当前对话纠正')
    await sendGeneralCoaching(rawValue)
    return true
  }
  if (intent === 'execution') {
    await sendLiveRoomAnswer(extractLiveRoomExecutionContent(rawValue), 'answer')
    input.value = ''
    dismissedSuggestionInput.value = ''
    return true
  }
  if (intent === 'test') {
    liveRoomWorkMode.value = 'test'
    liveRoomExecutionStatus.value = '当前没有待测试的学习候选；要测试直播回答，请先开启“测试模式”并输入模拟观众问题。'
    pushAgentMessage('live-room', '当前没有待测试的修正结果。要测试真实直播回答链，可以开启“测试模式”后直接输入一个模拟观众问题。')
    return true
  }

  liveRoomWorkMode.value = 'chat'
  return false
}

async function adoptLatestCoachingCandidate() {
  const activeSession = coachingSession.value
  if (!activeSession?.active) {
    pushAgentMessage('live-room', '当前没有可采用的修正结果，请先输入 /纠正智能体，或从公屏问题旁点击“纠正”。')
    return
  }
  const scope = conversationScopeForDomain('live-room')
  const activeTarget = activeSession.target
  let latest: NonNullable<ChatMessage['answerReference']> | null = null
  for (let index = messages.value.length - 1; index >= 0; index -= 1) {
    const item = messages.value[index]
    if (item.domain !== 'live-room' || !item.answerReference) continue
    if ((item.conversationScope || conversationScopeForDomain(item.domain)) !== scope) continue
    const candidateTarget = item.answerReference.target || item.answerReference.question
    if (candidateTarget !== activeTarget) continue
    latest = item.answerReference
    break
  }
  if (!latest) {
    pushAgentMessage('live-room', '当前调教还没有可采用的成果，请先让智能体完成一轮优化。')
    return
  }
  if (latest.accepted) {
    pushAgentMessage('live-room', '最新这条成果已经采用。')
    return
  }
  if (latest.saving) {
    pushAgentMessage('live-room', '最新这条成果正在采用中。')
    return
  }
  if (latest.canAdopt === false) {
    pushAgentMessage('live-room', '最新这条修正结果当前不可采用，请继续输入纠正内容后再试。')
    return
  }
  await adoptAnswerReference(latest)
}

async function send() {
  const rawValue = input.value.trim()
  if (!rawValue || busy.value) return

  const domain = currentDomain.value
  if (domain === 'live-room' && /^\/结束调教\s*$/.test(rawValue)) {
    endCoachingMode(true)
    return
  }
  if (domain === 'live-room' && /^\/采用\s*$/.test(rawValue)) {
    input.value = ''
    dismissedSuggestionInput.value = ''
    await adoptLatestCoachingCandidate()
    return
  }
  if (domain === 'live-room' && /^\/智能体记忆\s*$/.test(rawValue)) {
    input.value = ''
    dismissedSuggestionInput.value = ''
    await showAgentMemories()
    return
  }
  if (domain === 'live-room') {
    const coachingCommand = rawValue.match(/^\/(?:纠正智能体|调教)(?:\s+|$)/)
    if (coachingCommand) {
      const coachingFeedback = rawValue.slice(coachingCommand[0].length).trim()
      beginCoachingMode()
      input.value = ''
      dismissedSuggestionInput.value = ''
      if (coachingFeedback) await sendGeneralCoaching(coachingFeedback)
      return
    }
  }
  if (domain === 'live-room' && /^\/(?:纠正问题|参考回答|回答参考)\s*$/.test(rawValue)) {
    beginAnswerReferencePicking()
    return
  }
  if (domain === 'live-room' && answerReferencePicking.value) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = '纠正智能体 · 先点击要处理的问题'
    return
  }
  if (domain === 'live-room' && coachingSession.value?.active) {
    const liveExecutionCommand = /^\/(抢答|回答)(?:\s+|$)/.test(rawValue)
    if (!liveExecutionCommand && !rawValue.startsWith('/')) {
      await routeActiveAgentLearningInput(rawValue)
      return
    }
  }
  if (domain === 'live-room' && liveRoomTestMode.value && !rawValue.startsWith('/')) {
    await sendLiveRoomSimulation(rawValue)
    return
  }
  if (domain === 'live-room' && !coachingSession.value?.active && !rawValue.startsWith('/')) {
    const handledByModeSwitch = await routeInactiveLiveRoomWorkModeInput(rawValue)
    if (handledByModeSwitch) return
  }

  if (domain === 'live-room') {
    const command = rawValue.match(/^\/(抢答|回答)(?:\s+|$)/)
    const commandMode: LiveRoomAnswerMode | null = command?.[1] === '抢答'
      ? 'quick'
      : command?.[1] === '回答'
        ? 'answer'
        : null
    const otherSlashCommand = rawValue.startsWith('/') && !command
    if (otherSlashCommand) {
      liveRoomAnswerMode.value = null
      liveRoomExecutionStatus.value = ''
      liveRoomExecutionError.value = false
    }
    const mode = commandMode || (otherSlashCommand ? null : liveRoomAnswerMode.value)
    if (mode) {
      liveRoomAnswerMode.value = mode
      const commandText = command ? rawValue.slice(command[0].length) : rawValue
      const liveValue = unescapeAgentTriggerText(commandText).trim()
      if (!liveValue) {
        liveRoomExecutionError.value = true
        liveRoomExecutionStatus.value = '请在功能触发词后输入要执行的内容'
        return
      }
      await sendLiveRoomAnswer(liveValue, mode)
      return
    }
  }

  const value = unescapeAgentTriggerText(rawValue.replace(/^\/+/, '').trim()).trim()
  if (!value) return
  if (showInbox.value && isInboxIntent(value)) {
    inboxRequest.value = value
    drawerOpen.value = true
    expanded.value = true
    drawerTab.value = 'inbox'
    input.value = ''
    await refreshInbox()
    return
  }
  drawerTab.value = 'chat'
  if (livePolicyTestMode.value.active && domain === 'live-policy-admin') {
    if (isPolicyLearningAbsorbIntent(value)) {
      await absorbLivePolicyTest(value)
      return
    }
    await sendLivePolicyTest(value)
    return
  }
  const history = historyPayload(domain)
  const navigationTarget = resolveNavigationIntent(value)
  const systemTask = startOrContinueSystemTask(value)
  const adminPolicyIntent = resolveExplicitAdminPolicyIntent(value)
  const routeToSystemAgent = shouldRouteToSystemAgent(domain, {
    hasSystemTask: Boolean(systemTask),
    systemCapabilityIntent: isSystemCapabilityIntent(value),
    clientBoundaryIntent: isClientBoundaryIntent(value),
  })
  messages.value.push({ role: 'user', domain, text: value })
  input.value = ''
  dismissedSuggestionInput.value = ''
  activeComposer.value = null
  drawerOpen.value = true
  void scrollChatToBottom()

  if (navigationTarget && isInternalAgentProfile.value && !adminPolicyIntent) {
    await router.push(navigationTarget.to)
    pushAgentMessage(
      currentDomain.value,
      '已打开“' + navigationTarget.title + '”。你可以继续告诉我下一步要做什么。',
    )
    return
  }

  if (cancelActiveSystemTask(value)) {
    pushAgentMessage(domain, '已取消当前办理中的任务。你可以直接告诉我下一件要做的事。')
    return
  }

  busy.value = true
  busyDomain.value = domain

  try {
    if (
      isPolicyLearningAbsorbIntent(value) &&
      (domain === 'live-policy-admin' ||
        domain === 'live-strategy' ||
        (domain === 'live-support' && liveSupportMode.value === 'strategy'))
    ) {
      const candidate = await createPolicyLearningForDomain(domain, history, value)
      if (candidate) {
        window.dispatchEvent(
          new CustomEvent('live-policy-learning-created', {
            detail: { candidate },
          }),
        )
        pushAgentMessage(domain, policyLearningCandidateMessage(candidate))
        return
      }
    }

    if (adminPolicyIntent) {
      const policyHistory = historyPayload('live-policy-admin')
      const response = await chatLivePolicyAdminAgent({
        layer: adminPolicyIntent.layer,
        industry_code:
          adminPolicyIntent.layer === 'L2'
            ? adminPolicyIntent.industryCode || 'general'
            : undefined,
        message: value,
        history: policyHistory,
      })

      persistAdminPolicyContext(
        adminPolicyIntent.layer,
        adminPolicyIntent.industryCode,
      )
      if (domain !== 'live-policy-admin') {
        messages.value.push({
          role: 'user',
          domain: 'live-policy-admin',
          text: value,
        })
      }
      if (route.name !== 'live-strategy') {
        await router.push('/operations/live/strategy')
      }
      if (response.draft) {
        notifyAdminPolicyUpdated(
          adminPolicyIntent.layer,
          adminPolicyIntent.industryCode,
        )
      }
      pushAgentMessage(
        'live-policy-admin',
        response.reply +
          (response.draft
            ? '\n\n已生成' +
              adminPolicyIntent.layer +
              '草稿 V' +
              response.draft.version_no +
              '，已经打开直播策略页面供你核对；仍需手动发布后才正式生效。'
            : ''),
      )
      return
    }

    if (routeToSystemAgent) {
      const systemHistory = activeSystemTask.value
        ? systemTaskHistory.value.slice(-10)
        : history
      if (activeSystemTask.value) {
        systemTaskHistory.value.push({ role: 'user', text: value })
      }
      const agentPayload = {
        message: value,
        history: systemHistory,
        current_path: route.fullPath,
        navigation: navigationTargets.value.map((item) => ({
          title: item.title,
          to: item.to,
          section: item.section,
        })),
      }
      const response = isInternalAgentProfile.value
        ? await chatInternalAgent(agentPayload)
        : await chatClientAgent(agentPayload)
      if (activeSystemTask.value) {
        systemTaskHistory.value.push({ role: 'agent', text: response.reply })
      }
      latestSystemResponse.value = response
      systemContext.value.capabilities = response.capabilities
      if (response.navigate) {
        if (activeSystemTask.value) {
          activeSystemTask.value = null
          systemTaskHistory.value = []
        }
        await router.push(response.navigate.to)
        pushAgentMessage(currentDomain.value, response.reply)
        return
      }
      pushAgentMessage(domain, response.reply, response.action)
      return
    }

    if (domain === 'live-room') {
      const roomId = Number(route.params.id)
      if (!roomId) {
        pushAgentMessage(domain, '当前页面没有有效直播间编号，暂时不能进入场控上下文。')
        return
      }
      const response = await chatLiveAgent(roomId, {
        message: value,
        history,
      })
      pushAgentMessage(domain, response.reply)
      return
    }

    if (domain === 'live-strategy') {
      const roomId = await resolveLiveStrategyRoomID()
      if (!roomId) {
        pushAgentMessage(domain, '你当前还没有可用直播间，请先创建或选择直播间。')
        return
      }
      if (liveStrategyMode.value === 'strategy') {
        const response = await chatLiveRoomPolicyAgent(roomId, {
          message: value,
          history,
        })
        pushAgentMessage(
          domain,
          response.reply +
            (response.draft
              ? '\n\n已生成当前直播间用户层草稿 V' +
                response.draft.version_no +
                '，仍需在直播策略工作台发布后才正式生效。'
              : ''),
        )
        return
      }

      if (liveStrategyMode.value === 'voice') {
        const voiceBoundMessage = await tryBindRequestedVoice(roomId, value)
        if (voiceBoundMessage) {
          pushAgentMessage(domain, voiceBoundMessage)
          return
        }
      }

      const draft = await persistLiveStrategyModeDraft(roomId, value)
      const response = await chatLiveAgent(roomId, {
        message: value,
        history,
      })
      const modeName =
        liveStrategyMode.value === 'anchor'
          ? '主播训练'
          : liveStrategyMode.value === 'script'
            ? '固定话术'
            : liveStrategyMode.value === 'voice'
              ? '声音配置'
              : '基础设置'
      pushAgentMessage(
        domain,
        response.reply +
          (draft
            ? '\n\n已保存为当前直播间' + modeName + '草稿 V' + draft.version_no + '。'
            : ''),
      )
      return
    }

    if (domain === 'live-support') {
      const roomId = resolveLiveSupportRoomID()
      if (!roomId) {
        pushAgentMessage(domain, '当前没有选中的客户授权直播间，请先在“客户授权协助”里选择直播间。')
        return
      }
      if (liveSupportMode.value === 'strategy') {
        if (!canDelegateLivePolicyL3(session.bootstrap)) {
          pushAgentMessage(domain, '当前账号没有用户层授权协助权限，或尚未获得客户对当前直播间的授权。')
          return
        }
        const response = await chatLiveRoomPolicyAgent(roomId, {
          message: value,
          history,
        })
        pushAgentMessage(
          domain,
          response.reply +
            (response.draft
              ? '\n\n已生成当前授权直播间用户层草稿 V' +
                response.draft.version_no +
                '，仍需在客户授权协助工作台发布后才正式生效。'
              : ''),
        )
        return
      }
      if (liveSupportMode.value === 'anchor' && isExplicitAnchorTrainingIntent(value)) {
        const draft = await createLiveOpsAnchorTraining(roomId, { text: value })
        pushAgentMessage(
          domain,
          '已根据你的要求生成当前授权直播间主播训练草稿 V' +
            draft.version_no +
            '。请在客户授权协助工作台确认并发布；需要录音或文档样本时，请从工作台上传。',
        )
        return
      }

      const response = await chatInternalAgent({
        message: value,
        history,
        current_path: route.fullPath,
        navigation: navigationTargets.value.map((item) => ({
          title: item.title,
          to: item.to,
          section: item.section,
        })),
      })
      latestSystemResponse.value = response
      pushAgentMessage(domain, response.reply)
      return
    }

    if (domain === 'live-policy-admin') {
      const policyContext = readAdminPolicyContext()
      const response = await chatLivePolicyAdminAgent({
        layer: policyContext.layer,
        industry_code:
          policyContext.layer === 'L2' ? policyContext.industryCode || 'general' : undefined,
        message: value,
        history,
      })
      if (response.draft) {
        notifyAdminPolicyUpdated(policyContext.layer, policyContext.industryCode)
      }
      pushAgentMessage(
        domain,
        response.reply +
          (response.draft
            ? '\n\n已生成' +
              policyContext.layer +
              '草稿 V' +
              response.draft.version_no +
              '，仍需在直播策略工作台发布后才正式生效。'
            : ''),
      )
      return
    }

  } catch (error) {
    pushAgentMessage(
      domain,
      error instanceof Error
        ? '处理失败：' + error.message
        : assistantName.value + '暂时无法处理这条指令。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    void scrollChatToBottom()
  }
}

async function executeAction(message: ChatMessage) {
  const action = message.action
  if (!action || executing.value) return

  if (!isInternalAgentProfile.value) {
    pushAgentMessage(
      message.domain,
      '当前账号不能执行内部管理动作。',
    )
    return
  }

  executing.value = true
  try {
    const payload = action.payload

    if (action.type === 'create_staff_employee') {
      if (
        !payload.employee_no ||
        !payload.primary_group_id ||
        !payload.role_ids?.length ||
        !payload.username ||
        !payload.display_name ||
        !payload.phone ||
        !payload.province ||
        !payload.city ||
        !payload.district
      ) {
        throw new Error('员工执行参数不完整，请重新让智能体整理一次。')
      }

      const result = await createStaffEmployee({
        employee_no: payload.employee_no,
        primary_group_id: payload.primary_group_id,
        role_ids: payload.role_ids,
        username: payload.username,
        display_name: payload.display_name,
        phone: payload.phone,
        email: payload.email || '',
        province: payload.province,
        city: payload.city,
        district: payload.district,
        delivery_method: payload.delivery_method === 'email' ? 'email' : 'copy',
      })
      message.action = undefined
      message.credential = result.credential
      messages.value.push({
        role: 'agent',
        domain: message.domain,
        text:
          '已通过正式员工创建接口完成：' +
          result.item.display_name +
          '（' +
          result.item.employee_no +
          '），登录账号：' +
          result.item.username +
          '。首次登录需要修改初始密码。',
        credential: result.credential,
      })
      void scrollChatToBottom()
      activeSystemTask.value = null
      systemTaskHistory.value = []
      return
    }

    if (action.type === 'create_marketing_campaign') {
      if (!payload.code || !payload.name || !payload.items?.length) {
        throw new Error('营销活动执行参数不完整，请继续补充后再确认。')
      }
      const result = await createCommercialMarketingCampaign({
        code: payload.code,
        name: payload.name,
        description: payload.description || '',
        status: 'draft',
        sort_order: payload.sort_order || 10,
        pricing_rule: payload.pricing_rule || 'floor_yuan',
        starts_at: payload.starts_at || '',
        ends_at: payload.ends_at || '',
        items: payload.items,
        display_locations: payload.display_locations?.length
          ? payload.display_locations
          : ['backoffice'],
      })
      message.action = undefined
      pushAgentMessage(
        message.domain,
        '已创建营销活动草稿“' +
          result.name +
          '”。已默认设为“仅后台”，不会出现在终端商城或会员中心；你可以打开营销活动页面选择展示场地后再启用。',
      )
      activeSystemTask.value = null
      systemTaskHistory.value = []
      return
    }

    pushAgentMessage(
      message.domain,
      '这个动作当前还没有接入正式执行工具，我不会绕过系统直接修改数据。',
    )
  } catch (error) {
    pushAgentMessage(
      message.domain,
      error instanceof Error
        ? '执行失败：' + error.message
        : '执行失败，请检查权限和输入数据。',
    )
  } finally {
    executing.value = false
  }
}

function formatAgentDateTime(value?: string) {
  if (!value) return '不限'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function agentMarketingTargetTypeLabel(value?: string) {
  if (value === 'membership') return '会员方案'
  if (value === 'time_card') return '时长卡'
  if (value === 'device_product') return '设备商品'
  return value || '商品'
}

function agentMarketingDiscountLabel(value: number) {
  if (value <= 0) return '赠送'
  if (value >= 10000) return '原价'
  const zhe = value / 1000
  return zhe.toFixed(zhe % 1 === 0 ? 0 : 1) + '折'
}

async function copyCredential(credential?: InitialCredential) {
  if (!credential?.initial_password) return
  const text =
    '登录地址：' +
    credential.login_url +
    '\n初始密码：' +
    credential.initial_password
  await navigator.clipboard.writeText(text)
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="actor"
      ref="dockEl"
      class="system-agent-dock"
      :class="{
        dragging: dockDragging,
        'policy-test-active': livePolicyTestMode.active,
      }"
      :style="dockStyle"
    >
      <button
        v-if="!expanded"
        class="system-agent-orb"
        type="button"
        :aria-label="'打开' + assistantName"
        @click="expand"
      >
        <span>✦</span>
        <TodoBadge v-if="showInbox" to="/work/inbox" />
        <i class="ring-one"></i>
        <i class="ring-two"></i>
      </button>

      <div
        v-else
        class="system-agent-inline"
        title="按住空白区域可拖动"
        @pointerdown="startDockDrag"
      >
        <button
          class="system-agent-orb mini"
          type="button"
          title="拖动移动；单击收起"
          @pointerdown.stop="startDockDrag"
          @click="handleDockMiniOrbClick"
        >
          <span>✦</span>
        </button>
        <div class="system-agent-inline-main">
          <small v-if="!isTerminalCustomer">{{ assistantName }} · {{ contextLabel }} · {{ latestAgentMessage }}</small>
          <small v-else>{{ assistantName }}</small>
          <div
            v-if="currentDomain === 'live-room' && (answerReferencePicking || answerReferencePending)"
            class="answer-reference-context"
            :class="{ picking: answerReferencePicking }"
          >
            <div>
              <strong>{{ answerReferencePicking ? '纠正智能体 · 选择问题' : '智能体学习' }}</strong>
              <span v-if="answerReferencePicking">点击公屏问题、问题桶或桶内问题</span>
              <template v-else-if="answerReferencePending">
                <span>{{ answerReferencePending.topic || answerReferencePending.question }}</span>
                <small>
                  <template v-if="answerReferencePending.uniqueUsers">{{ answerReferencePending.uniqueUsers }} 人 · </template>
                  <template v-if="answerReferencePending.aggregateCount">{{ answerReferencePending.aggregateCount }} 条 · </template>
                  {{ answerReferencePending.time || '当前' }}
                </small>
              </template>
            </div>
            <button v-if="answerReferencePicking" type="button" @click.stop="cancelAnswerReference()">取消</button>
            <button v-else-if="coachingSession?.active && coachingSession.latestReply" type="button" @click.stop="adoptLatestCoachingCandidate">采用当前修正</button>
          </div>
          <div
            v-if="currentDomain === 'live-room' && coachingSession?.active && coachingSession.kind === 'general'"
            class="answer-reference-context coaching-context"
          >
            <div>
              <strong>智能体学习</strong>
              <span>{{ coachingSession.target }}</span>
              <small>继续输入就是继续修改；满意后点击“采用”立即生效</small>
            </div>
            <button v-if="coachingSession.latestReply" type="button" @click.stop="adoptLatestCoachingCandidate">采用当前修正</button>
          </div>
          <div
            v-if="currentDomain === 'live-room' && (liveRoomAnswerMode || liveRoomExecutionStatus)"
            class="live-room-answer-state"
            :class="{ error: liveRoomExecutionError }"
          >
            <strong v-if="liveRoomAnswerMode">{{ liveRoomAnswerModeLabel }}</strong>
            <span v-if="liveRoomExecutionStatus">{{ liveRoomExecutionStatus }}</span>
          </div>
          <div class="system-agent-composer-field">
            <textarea
              ref="inputEl"
              v-model="input"
              rows="2"
              :placeholder="inputPlaceholder"
              @focus="composerFocus('dock')"
              @input="composerInput('dock')"
              @keydown="handleComposerKeydown($event, 'dock')"
            ></textarea>
            <div
              v-if="showSuggestions('dock')"
              class="system-agent-suggestion-menu"
            >
              <div class="system-agent-suggestion-head">
                <strong>{{ suggestionTitle }}</strong>
                <span>↑↓ 选择 · Enter 确认 · Esc 关闭 · \@ / \/ 按普通字符输入</span>
              </div>
              <button
                v-for="(item, index) in suggestions"
                :key="item.kind + '-' + item.insertText"
                type="button"
                :class="{ active: suggestionIndex === index }"
                @mousedown.prevent="selectSuggestion(item)"
                @mouseenter="suggestionIndex = index"
              >
                <i>{{ item.kind === 'department' ? '@' : '/' }}</i>
                <span>
                  <strong>{{ item.label }}</strong>
                  <small>{{ item.description }}</small>
                </span>
              </button>
            </div>
          </div>
        </div>
        <button
          class="system-agent-send"
          type="button"
          :disabled="busy || !input.trim()"
          @click="send"
        >
          {{ currentDomain === 'live-room' && answerReferencePending
            ? '发送'
            : currentDomain === 'live-room' && coachingSession?.active && coachingSession.kind === 'general'
              ? '发送'
              : currentDomain === 'live-room' && liveRoomAnswerMode
                ? (liveRoomAnswerMode === 'quick' ? '抢答' : '回答')
                : '发送' }}
        </button>
        <button
          class="system-agent-open"
          type="button"
          :aria-expanded="drawerOpen"
          @click="toggleDrawer"
        >
          {{ drawerOpen ? '关闭' : '展开' }}
        </button>
        <button
          class="system-agent-close"
          type="button"
          :aria-label="'收起' + assistantName"
          @click="collapse"
        >
          ×
        </button>
        <div v-if="currentDomain === 'live-room'" class="live-room-test-toggle dock-live-room-test-toggle">
          <button
            type="button"
            :class="{ active: liveRoomTestMode }"
            @click.stop="toggleLiveRoomTestMode"
          >
            {{ liveRoomTestMode ? '退出测试' : '测试模式' }}
          </button>
          <span>{{ liveRoomTestMode ? '模拟观众提问 · 真实 Agent 处理 · 不播音' : '开启后模拟观众发问，不影响真实直播数据' }}</span>
        </div>
      </div>
    </div>

    <div
      v-if="drawerOpen && actor"
      class="system-agent-backdrop"
    >
      <aside class="system-agent-drawer" :class="{'without-work-inbox': !showInbox, 'terminal-agent-drawer': isTerminalCustomer}">
        <header>
          <div>
            <span v-if="!isTerminalCustomer" class="section-kicker">AI COPILOT · {{ contextLabel }}</span>
            <h3>{{ assistantName }}</h3>
            <small v-if="isTerminalCustomer" class="terminal-agent-subtitle" lang="en">XIAOLAN LIVE COMPANION</small>
          </div>
          <button class="icon-button" type="button" @click="drawerOpen = false">×</button>
        </header>

        <nav v-if="showInbox" class="agent-inbox-tabs" aria-label="智能体工作面板"><button type="button" :class="{active:drawerTab==='chat'}" @click="drawerTab='chat'">对话</button><button type="button" :class="{active:drawerTab==='inbox'}" @click="drawerTab='inbox';inboxRequest=''">我的待办<TodoBadge to="/work/inbox"/></button></nav>
        <WorkInboxPanel v-if="showInbox && drawerTab==='inbox'" :request="inboxRequest" compact />
        <section v-show="!showInbox || drawerTab==='chat'" ref="chatEl" class="system-agent-chat">
          <div v-if="!isTerminalCustomer && visibleMessages.length === 0" class="system-agent-context-empty">
            <strong>{{ contextLabel }}</strong>
            <p>{{ contextDescription }}</p>
          </div>

          <article
            v-for="(message, index) in visibleMessages"
            :key="index"
            :class="['system-agent-message', message.role]"
          >
            <strong>{{ message.role === 'agent' ? assistantName : '我' }}</strong>
            <p v-if="!message.answerReference">{{ message.text }}</p>

            <div
              v-if="message.answerReference"
              class="system-agent-action-card answer-reference-result-card"
            >
              <div class="answer-reference-result-copy">
                <span>以后这样处理</span>
                <small class="answer-reference-result-target">
                  {{ message.answerReference.target || message.answerReference.question }}
                </small>
                <p class="answer-reference-result-text">{{ message.answerReference.strategy }}</p>
                <small v-if="message.answerReference.accepted" class="answer-reference-result-reference">已采用 · V{{ message.answerReference.adoptedVersionNo || 1 }} · 当前直播间立即生效</small>
              </div>
              <div class="coaching-result-actions">
                <button
                  v-if="message.answerReference.canAdopt || message.answerReference.accepted || message.answerReference.saving"
                  class="primary-button"
                  type="button"
                  :disabled="message.answerReference.accepted || message.answerReference.saving"
                  @click="adoptAnswerReference(message.answerReference)"
                >
                  {{ message.answerReference.saving ? '采用中…' : message.answerReference.accepted ? '已采用' : '采用' }}
                </button>
              </div>
            </div>

            <div v-if="message.memoryList" class="system-agent-action-card agent-memory-list-card">
              <div class="agent-memory-list-head">
                <span>智能体记忆</span>
                <small>当前直播间 · 已生效</small>
              </div>
              <div v-if="!message.memoryList.length" class="agent-memory-empty">暂无已采用记忆。</div>
              <article v-for="memory in message.memoryList" :key="memory.id" class="agent-memory-item">
                <div>
                  <strong>{{ memory.target }}</strong>
                  <p>{{ memory.current_version?.content_text || '暂无内容' }}</p>
                  <small>V{{ memory.current_version?.version_no || 1 }}</small>
                </div>
                <div class="agent-memory-actions">
                  <button type="button" @click="correctAgentMemory(memory)">纠正</button>
                  <button type="button" @click="showAgentMemoryVersions(memory)">历史</button>
                  <button type="button" @click="stopAgentMemory(memory)">停用</button>
                </div>
              </article>
            </div>

            <div v-if="message.memoryVersions" class="system-agent-action-card agent-memory-list-card">
              <div class="agent-memory-list-head">
                <span>{{ message.memoryVersions.memory.target }} · 版本记录</span>
                <small>当前 V{{ message.memoryVersions.memory.current_version?.version_no || 1 }}</small>
              </div>
              <article v-for="version in message.memoryVersions.versions" :key="version.id" class="agent-memory-item">
                <div>
                  <strong>V{{ version.version_no }}</strong>
                  <p>{{ version.content_text }}</p>
                  <small>{{ version.status }}</small>
                </div>
                <div class="agent-memory-actions">
                  <button
                    v-if="version.id !== message.memoryVersions.memory.current_version_id"
                    type="button"
                    @click="rollbackAgentMemory(message.memoryVersions.memory, version)"
                  >恢复此版本</button>
                </div>
              </article>
            </div>

            <div v-if="message.action" class="system-agent-action-card">
              <div>
                <span>待确认动作</span>
                <h4>{{ message.action.title }}</h4>
                <p>{{ message.action.summary }}</p>
              </div>
              <dl v-if="message.action.type === 'create_staff_employee'">
                <div>
                  <dt>部门</dt>
                  <dd>{{ message.action.payload.primary_group_name || '-' }}</dd>
                </div>
                <div>
                  <dt>岗位</dt>
                  <dd>{{ message.action.payload.role_names?.join('、') || '-' }}</dd>
                </div>
                <div>
                  <dt>员工编号</dt>
                  <dd>{{ message.action.payload.employee_no || '-' }}</dd>
                </div>
                <div>
                  <dt>登录账号</dt>
                  <dd>{{ message.action.payload.username || '-' }}</dd>
                </div>
                <div>
                  <dt>手机号</dt>
                  <dd>{{ message.action.payload.phone || '-' }}</dd>
                </div>
                <div>
                  <dt>地区</dt>
                  <dd>
                    {{ message.action.payload.province || '' }}
                    {{ message.action.payload.city || '' }}
                    {{ message.action.payload.district || '' }}
                  </dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'create_marketing_campaign'">
                <div>
                  <dt>活动名称</dt>
                  <dd>{{ message.action.payload.name || '-' }}</dd>
                </div>
                <div>
                  <dt>创建状态</dt>
                  <dd>草稿</dd>
                </div>
                <div>
                  <dt>开始时间</dt>
                  <dd>{{ formatAgentDateTime(message.action.payload.starts_at) }}</dd>
                </div>
                <div>
                  <dt>结束时间</dt>
                  <dd>{{ formatAgentDateTime(message.action.payload.ends_at) }}</dd>
                </div>
                <div
                  v-for="(item, itemIndex) in message.action.payload.items || []"
                  :key="itemIndex"
                >
                  <dt>{{ agentMarketingTargetTypeLabel(item.target_type) }}</dt>
                  <dd>
                    #{{ item.target_id }} · ×{{ item.quantity || 1 }} ·
                    {{ agentMarketingDiscountLabel(item.discount_bps || 0) }}
                  </dd>
                </div>
              </dl>
              <button
                class="primary-button"
                type="button"
                :disabled="executing"
                @click="executeAction(message)"
              >
                {{ executing ? '执行中…' : '确认执行' }}
              </button>
            </div>

            <div
              v-if="message.credential?.initial_password"
              class="system-agent-credential"
            >
              <span>初始凭证</span>
              <code>{{ message.credential.initial_password }}</code>
              <button
                type="button"
                class="text-action"
                @click="copyCredential(message.credential)"
              >
                复制登录信息
              </button>
            </div>
          </article>

          <article
            v-if="busy && busyDomain === currentDomain"
            class="system-agent-message agent system-agent-thinking"
          >
            <strong>{{ assistantName }}</strong>
            <div class="system-agent-thinking-row">
              <span class="system-agent-thinking-dots" aria-label="智能体正在思考">
                <i></i><i></i><i></i>
              </span>
              <small>{{ isTerminalCustomer ? '正在思考…' : '正在理解你的要求，并检查当前工作域与权限…' }}</small>
            </div>
          </article>
        </section>

        <footer>
          <div
            v-if="currentDomain === 'live-room' && answerReferencePending"
            class="answer-reference-context drawer-answer-reference-context"
          >
            <div>
              <strong>智能体学习</strong>
              <span>{{ answerReferencePending.topic || answerReferencePending.question }}</span>
              <small>
                <template v-if="answerReferencePending.uniqueUsers">{{ answerReferencePending.uniqueUsers }} 人 · </template>
                <template v-if="answerReferencePending.aggregateCount">{{ answerReferencePending.aggregateCount }} 条 · </template>
                {{ answerReferencePending.time || '当前' }}
              </small>
            </div>
            <button v-if="coachingSession?.active && coachingSession.latestReply" type="button" @click.stop="adoptLatestCoachingCandidate">采用当前修正</button>
          </div>
          <div
            v-if="currentDomain === 'live-room' && coachingSession?.active && coachingSession.kind === 'general'"
            class="answer-reference-context drawer-answer-reference-context coaching-context"
          >
            <div>
              <strong>智能体学习</strong>
              <span>{{ coachingSession.target }}</span>
              <small>继续输入就是继续修改；满意后点击“采用”</small>
            </div>
            <button v-if="coachingSession.latestReply" type="button" @click.stop="adoptLatestCoachingCandidate">采用当前修正</button>
          </div>
          <div class="system-agent-composer-field">
            <textarea
              ref="drawerInputEl"
              v-model="input"
              rows="3"
              :placeholder="inputPlaceholder"
              @focus="composerFocus('drawer')"
              @input="composerInput('drawer')"
              @keydown="handleComposerKeydown($event, 'drawer')"
            ></textarea>
            <div
              v-if="showSuggestions('drawer')"
              class="system-agent-suggestion-menu"
            >
              <div class="system-agent-suggestion-head">
                <strong>{{ suggestionTitle }}</strong>
                <span>↑↓ 选择 · Enter 确认 · Esc 关闭 · \@ / \/ 按普通字符输入</span>
              </div>
              <button
                v-for="(item, index) in suggestions"
                :key="item.kind + '-' + item.insertText"
                type="button"
                :class="{ active: suggestionIndex === index }"
                @mousedown.prevent="selectSuggestion(item)"
                @mouseenter="suggestionIndex = index"
              >
                <i>{{ item.kind === 'department' ? '@' : '/' }}</i>
                <span>
                  <strong>{{ item.label }}</strong>
                  <small>{{ item.description }}</small>
                </span>
              </button>
            </div>
          </div>
          <button
            class="primary-button"
            type="button"
            :disabled="busy || !input.trim()"
            @click="send"
          >
            {{ currentDomain === 'live-room' && answerReferencePending
              ? '发送'
              : currentDomain === 'live-room' && coachingSession?.active && coachingSession.kind === 'general'
                ? '发送'
                : '发送' }}
          </button>
          <div v-if="currentDomain === 'live-room'" class="live-room-test-toggle drawer-live-room-test-toggle">
            <button
              type="button"
              :class="{ active: liveRoomTestMode }"
              @click="toggleLiveRoomTestMode"
            >
              {{ liveRoomTestMode ? '退出测试' : '测试模式' }}
            </button>
            <span>{{ liveRoomTestMode ? '模拟观众提问 · 真实 Agent 处理 · 不播音' : '开启后模拟观众发问，不影响真实直播数据' }}</span>
          </div>
        </footer>
      </aside>
    </div>
  </Teleport>
</template>
<style scoped>
.live-room-answer-state{display:flex;align-items:center;gap:8px;min-height:24px;margin:0 0 5px;padding:3px 8px;border:1px solid rgba(104,118,220,.18);border-radius:8px;background:rgba(244,246,255,.9);color:#66708c;font-size:12px;line-height:1.35}.live-room-answer-state strong{flex:0 0 auto;color:#5666d8;font-size:12px;font-weight:900}.live-room-answer-state span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.live-room-answer-state.error{border-color:rgba(216,63,79,.22);background:rgba(255,244,246,.94);color:#cf4050}.live-room-answer-state.error strong{color:#cf4050}
.live-room-test-toggle{display:flex;align-items:center;gap:8px}.live-room-test-toggle button{height:26px;padding:0 10px;border:1px solid rgba(84,104,214,.22);border-radius:999px;background:rgba(255,255,255,.88);color:#6672b8;font:inherit;font-size:11px;font-weight:900;cursor:pointer;box-shadow:0 2px 8px rgba(72,88,170,.06)}.live-room-test-toggle button.active{border-color:rgba(84,104,214,.5);background:linear-gradient(135deg,rgba(96,111,230,.16),rgba(120,134,243,.10));color:#4f5fd0;box-shadow:0 0 0 2px rgba(84,104,214,.07),0 4px 12px rgba(72,88,170,.10)}.live-room-test-toggle span{color:#8a93a8;font-size:10px;line-height:1.3}.dock-live-room-test-toggle{flex:0 0 100%;width:100%;box-sizing:border-box;justify-content:flex-start;margin:2px 0 0;padding:7px 10px 0 0;border-top:1px solid rgba(105,121,190,.12)}.drawer-live-room-test-toggle{grid-column:1 / -1;margin:0;padding-top:7px;border-top:1px solid rgba(105,121,190,.12)}
.answer-reference-context{display:flex;align-items:center;justify-content:space-between;gap:10px;margin:0 0 6px;padding:7px 9px;border:1px solid rgba(84,104,214,.2);border-radius:10px;background:linear-gradient(135deg,rgba(241,244,255,.96),rgba(250,251,255,.96));color:#5f6985;line-height:1.35}.answer-reference-context>div{min-width:0;display:grid;gap:2px}.answer-reference-context strong{color:#5362cf;font-size:12px;font-weight:900}.answer-reference-context span{max-width:440px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}.answer-reference-context small{color:#929bb0;font-size:10px}.answer-reference-context button{flex:0 0 auto;padding:4px 8px;border:1px solid rgba(84,104,214,.16);border-radius:8px;background:#fff;color:#6874b8;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.answer-reference-context.picking{border-style:dashed;background:rgba(244,246,255,.96)}.coaching-context{border-color:rgba(112,91,220,.22);background:linear-gradient(135deg,rgba(244,241,255,.97),rgba(251,250,255,.97))}.drawer-answer-reference-context{grid-column:1 / -1;margin:0}.answer-reference-result-card{align-items:stretch;gap:14px;flex-wrap:wrap}.answer-reference-result-copy{display:grid;gap:8px}.answer-reference-result-target{color:#7f8aa0;font-size:12px;line-height:1.45}.answer-reference-result-text{margin:0!important;padding:12px 14px;border:1px solid rgba(92,111,210,.14);border-radius:10px;background:#f8f9ff;color:#293349!important;font-size:17px!important;font-weight:750;line-height:1.7;white-space:pre-wrap}.answer-reference-result-reference{color:#8d96a8;font-size:11px;line-height:1.5}.coaching-result-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}.coaching-result-actions .ghost-button{min-height:36px;padding:7px 12px}
.agent-memory-list-card{align-items:stretch}.agent-memory-list-head{display:flex;align-items:center;justify-content:space-between;gap:10px}.agent-memory-list-head>span{color:#5666d8!important;font-size:13px!important;font-weight:900}.agent-memory-list-head>small{color:#929bb0;font-size:11px}.agent-memory-empty{padding:14px;border-radius:10px;background:#f8f9fc;color:#8a93a6;text-align:center}.agent-memory-item{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:start;gap:12px;padding:12px;border:1px solid #e5e8f2;border-radius:11px;background:#fafbff}.agent-memory-item>div:first-child{display:grid;min-width:0;gap:4px}.agent-memory-item>div:first-child>span{width:max-content;padding:2px 7px;border-radius:999px;background:#eef0ff;color:#5965c8;font-size:10px;font-weight:850}.agent-memory-item strong{color:#30394a;font-size:14px}.agent-memory-item p{margin:0!important;color:#596579!important;line-height:1.55;white-space:pre-wrap}.agent-memory-item small{color:#9aa2b0;font-size:10px}.agent-memory-actions{display:flex;align-items:center;justify-content:flex-end;gap:6px;flex-wrap:wrap}.agent-memory-actions button{min-height:32px;padding:5px 9px;border:1px solid #d9deeb;border-radius:8px;background:#fff;color:#5f6a7e;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.agent-memory-actions button:hover{border-color:#aeb7df;background:#f4f6ff;color:#4f5fc4}@media(max-width:720px){.agent-memory-item{grid-template-columns:1fr}.agent-memory-actions{justify-content:flex-start}}
.system-agent-drawer{grid-template-rows:auto auto minmax(0,1fr) auto;overflow:hidden}
.system-agent-drawer.without-work-inbox{grid-template-rows:auto minmax(0,1fr) auto}
.system-agent-drawer.terminal-agent-drawer{grid-template-rows:auto minmax(0,1fr) auto}
.terminal-agent-drawer > header{padding:18px 20px;align-items:center}
.terminal-agent-drawer > header h3{font-size:18px!important;line-height:1.5;margin:0}
.terminal-agent-drawer > header > div{min-width:0}
.terminal-agent-drawer .terminal-agent-subtitle{display:block;margin-top:4px;color:#65748e;font-size:14px;line-height:1.4;font-weight:500;letter-spacing:.06em;overflow-wrap:anywhere}
.terminal-agent-drawer > footer{grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:12px}
.terminal-agent-drawer > footer .system-agent-composer-field > textarea{display:block;margin:0}
.terminal-agent-drawer > footer > .primary-button{align-self:center;justify-self:end;min-width:76px;height:48px;min-height:48px;margin:0;padding:0 18px;border-radius:12px;white-space:nowrap;transform:none}
.terminal-agent-drawer .system-agent-message p,.terminal-agent-drawer textarea,.terminal-agent-drawer > footer button{font-size:18px!important;line-height:1.6}
.system-agent-drawer :deep(.work-inbox-panel.compact){min-height:0;max-height:none;overflow:auto}
.system-agent-drawer > .system-agent-chat{min-height:0}
.agent-inbox-tabs{display:flex;gap:10px;padding:0 18px 12px}.agent-inbox-tabs button{position:relative;padding:8px 32px 8px 14px;min-height:40px;font-size:18px;border:1px solid #cfdbef;border-radius:8px;background:#fff;color:#315b94;cursor:pointer}.agent-inbox-tabs button.active{background:#eaf3ff;border-color:#75a4ea}.agent-inbox-tabs button:hover,.agent-inbox-tabs button:focus-visible{outline:none;box-shadow:0 0 0 3px #4285ff22;border-color:#5e96ed}.system-agent-orb{position:relative}
</style>
