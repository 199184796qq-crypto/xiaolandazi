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
  executeInternalAgentAction,
  executeLiveStrategyAction,
  adoptLiveAgentPlanFacts,
  adoptLiveAgentPlanBenefits,
  adoptLiveAgentPlanProductLinks,
  bindRoomLiveAgentPlan,
  deleteLiveAgentPlanFact,
  deleteLiveAgentPlanBenefit,
  deleteLiveAgentPlanProductLink,
  getClientAgentContext,
  getInternalAgentContext,
  getLiveAgentPlans,
  getLiveAgentPlanFacts,
  getLiveAgentPlanBenefits,
  getLiveAgentPlanProductLinks,
  getLiveAgentPlanScriptReferences,
  getRoomLiveAgentPlans,
  getPublicSystemConfig,
  getLiveAgentConfigVersions,
  getLiveOfficialVoices,
  getUserUIPreferences,
  getRoomAgentDecisions,
  getLiveVoiceProfiles,
  getRooms,
  interpretLiveStrategyIntent,
  previewRecognizeLiveAgentPlanImage,
  enqueueRoomManualAgentDecision,
  simulateRoomAgentDecision,
  setLiveRuntimePlan,
  testLivePolicyAdmin,
  unbindRoomLiveAgentPlan,
  updateUserUIPreferences,
  updateLiveAgentPlanFact,
  updateLiveAgentPlanBenefit,
  updateLiveAgentPlanProductLink,
  type LiveStrategyIntentResponse,
} from '../api'
import type {
  InitialCredential,
  AgentDecisionSimulationResult,
  AgentMemoryItem,
  AgentMemoryVersion,
  LiveAgentPlanFactCandidate,
  LiveAgentPlanBenefitCandidate,
  LivePolicyLearningCandidate,
  SystemAgentActionPreview,
  SystemAgentChatResponse,
  SystemAgentContextResponse,
} from '../types'
import { session } from '../session'
import { canDelegateLivePolicyL3 } from '../livePolicyAccess'
import { resolveAgentNavigationTargets } from '../navigationUi'
import { shouldRouteToSystemAgent } from '../systemAgentRouting'
import {
  buildLiveStrategyIntentClarification,
  isExplicitPendingBenefitUpdateIntent,
  isExplicitLiveBenefitCommand,
  isLiveBenefitAccept,
  isLiveBenefitCancel,
  isLiveBenefitUpdateIntent,
  isLiveImageProductAccept,
  isLiveImageProductCancel,
  isLiveProductCorrectionAccept,
  isLiveProductCorrectionCancel,
  isLiveStrategyCancelIntent,
  isLiveStrategyConfirmIntent,
  liveBenefitUpdateValues,
  liveProductField,
  liveProductImplicitSpecFromCommand,
  liveProductLinkActionFromText,
  liveProductLinkKeyFromText,
  liveProductLinkTypoCorrection,
  liveProductNameFromCommand,
  liveProductUpdateValues,
  normalizeBenefitTimeValue,
  shouldRouteLiveStrategyModuleCommand,
  type LiveStrategyIntentOption,
  type LiveStrategyMode,
} from '../agent/liveStrategyIntentRouter'

type AgentDomain = 'system' | 'live-room' | 'live-strategy' | 'live-policy-admin' | 'live-support'
type SystemTaskKey = 'create_staff_employee' | 'create_marketing_campaign'
type AgentHistoryItem = { role: 'user' | 'agent'; text: string }
type ComposerSource = 'dock' | 'drawer'
type SuggestionKind = 'department' | 'capability' | 'navigation' | 'image'

const unifiedLiveStrategyActionTypes = new Set([
  'add_live_product',
  'confirm_live_product_update',
  'confirm_live_product_disable',
  'add_live_benefit',
  'confirm_live_benefit_update',
  'confirm_live_benefit_disable',
  'add_live_fact',
  'confirm_live_fact_update',
  'confirm_live_fact_disable',
  'add_live_script_reference',
  'confirm_live_script_reference_update',
  'confirm_live_script_reference_disable',
  'confirm_live_plan_bind',
  'confirm_live_plan_unbind',
  'confirm_live_plan_switch',
])

type AgentImageAttachment = {
  id: string
  label: string
  dataUrl: string
  width: number
  height: number
  scope: string
  createdAt: number
}

type LivePolicyTestMode = {
  active: boolean
  layer: 'L1' | 'L2'
  industryCode: string
}

type CoachingKind = 'reference_answer' | 'general'

type ChatMessage = {
  role: 'user' | 'agent'
  text: string
  domain: AgentDomain
  action?: SystemAgentActionPreview
  speechChoice?: {
    question: string
    text: string
    status?: 'pending' | 'sending' | 'sent'
    selected?: 'quick' | 'answer'
  }
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
type LiveRoomIntent = LiveRoomWorkMode | 'adopt'

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
const drawerPreferenceReady = ref(false)
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
const chatNearBottom = ref(true)
const chatHasOverflow = ref(false)
const showChatJumpToBottom = computed(() => chatHasOverflow.value && !chatNearBottom.value)
const activeComposer = ref<ComposerSource | null>(null)
const suggestionIndex = ref(0)
const dismissedSuggestionInput = ref('')
const activeSystemTask = ref<SystemTaskKey | null>(null)
const systemTaskHistory = ref<AgentHistoryItem[]>([])
const agentImages = ref<AgentImageAttachment[]>([])
const imagePreview = ref<AgentImageAttachment | null>(null)
const imageAttachmentError = ref('')

function agentDrawerPreferenceCacheKey(userID: number) {
  return 'xiaolan-ui:' + String(userID) + ':agent-drawer-collapsed'
}

function restoreAgentDrawerPreferenceLocal(userID: number) {
  const raw = window.localStorage.getItem(agentDrawerPreferenceCacheKey(userID))
  if (raw === '0') drawerOpen.value = true
  if (raw === '1') drawerOpen.value = false
}

function persistAgentDrawerPreferenceLocal(userID: number) {
  window.localStorage.setItem(agentDrawerPreferenceCacheKey(userID), drawerOpen.value ? '0' : '1')
}

async function loadAgentDrawerPreference(userID: number) {
  drawerPreferenceReady.value = false
  restoreAgentDrawerPreferenceLocal(userID)
  try {
    const preferences = await getUserUIPreferences()
    drawerOpen.value = !preferences.agent_drawer_collapsed
    persistAgentDrawerPreferenceLocal(userID)
  } catch {
    // Keep the locally restored state when management-service is temporarily unavailable.
  } finally {
    drawerPreferenceReady.value = true
  }
}
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
  storedLiveStrategyMode === 'products' ||
  storedLiveStrategyMode === 'benefits' ||
  storedLiveStrategyMode === 'knowledge' ||
  storedLiveStrategyMode === 'rhythm' ||
  storedLiveStrategyMode === 'memory' ||
  storedLiveStrategyMode === 'anchor' ||
  storedLiveStrategyMode === 'script' ||
  storedLiveStrategyMode === 'voice' ||
  storedLiveStrategyMode === 'fullshow' ||
  storedLiveStrategyMode === 'plan'
    ? storedLiveStrategyMode
    : 'strategy',
)
const liveStrategyPlanId = ref(Number(window.localStorage.getItem('system-agent-live-plan-id') || 0))
const liveStrategyModuleLabel = ref(window.localStorage.getItem('system-agent-live-module-label') || '')
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

const AGENT_IMAGE_MAX_COUNT = 4
const AGENT_IMAGE_MAX_SOURCE_BYTES = 5 * 1024 * 1024
const AGENT_IMAGE_TARGET_DATA_URL_LENGTH = 1200000
const AGENT_IMAGE_HARD_DATA_URL_LENGTH = 2000000
const AGENT_IMAGE_TOTAL_DATA_URL_LENGTH = 5200000

const currentAgentImages = computed(() => {
  const scope = conversationScopeForDomain(currentDomain.value)
  return agentImages.value.filter((item) => item.scope === scope)
})

function nextAgentImageLabel(scope: string) {
  let maxIndex = 0
  for (const item of agentImages.value) {
    if (item.scope !== scope) continue
    const matched = item.label.match(/^图片(\d+)$/)
    if (matched) maxIndex = Math.max(maxIndex, Number(matched[1] || 0))
  }
  return '图片' + String(maxIndex + 1)
}

async function normalizeAgentImage(file: File) {
  if (!/^image\/(png|jpeg|webp)$/i.test(file.type)) {
    throw new Error('只支持 PNG、JPEG、WEBP 图片')
  }
  if (file.size <= 0 || file.size > AGENT_IMAGE_MAX_SOURCE_BYTES) {
    throw new Error('单张图片最大 5MB')
  }
  const bitmap = await createImageBitmap(file)
  try {
    let maxSide = 2200
    let quality = 0.92
    let dataUrl = ''
    let outputWidth = bitmap.width
    let outputHeight = bitmap.height
    for (let attempt = 0; attempt < 8; attempt += 1) {
      const scale = Math.min(1, maxSide / Math.max(bitmap.width, bitmap.height))
      outputWidth = Math.max(1, Math.round(bitmap.width * scale))
      outputHeight = Math.max(1, Math.round(bitmap.height * scale))
      const canvas = document.createElement('canvas')
      canvas.width = outputWidth
      canvas.height = outputHeight
      const context = canvas.getContext('2d')
      if (!context) throw new Error('浏览器暂时无法处理这张图片')
      context.drawImage(bitmap, 0, 0, outputWidth, outputHeight)
      dataUrl = canvas.toDataURL('image/webp', quality)
      if (dataUrl.length <= AGENT_IMAGE_TARGET_DATA_URL_LENGTH) break
      if (quality > 0.76) {
        quality -= 0.04
      } else {
        maxSide = Math.max(1100, Math.round(maxSide * 0.86))
      }
    }
    if (!dataUrl || dataUrl.length > AGENT_IMAGE_HARD_DATA_URL_LENGTH) {
      throw new Error('图片内容太大，请裁剪后再粘贴')
    }
    return { dataUrl, width: outputWidth, height: outputHeight }
  } finally {
    bitmap.close()
  }
}

async function addAgentImage(file: File) {
  imageAttachmentError.value = ''
  const scope = conversationScopeForDomain(currentDomain.value)
  const scopedImages = agentImages.value.filter((item) => item.scope === scope)
  if (scopedImages.length >= AGENT_IMAGE_MAX_COUNT) {
    imageAttachmentError.value = '当前会话最多保留 4 张图片，请先删除不用的图片。'
    return
  }
  try {
    const normalized = await normalizeAgentImage(file)
    const scopedTotal = scopedImages.reduce((sum, item) => sum + item.dataUrl.length, 0)
    if (scopedTotal + normalized.dataUrl.length > AGENT_IMAGE_TOTAL_DATA_URL_LENGTH) {
      throw new Error('当前会话图片总大小过大，请删除一张后再粘贴')
    }
    const label = nextAgentImageLabel(scope)
    agentImages.value.push({
      id: 'agent-image-' + Date.now() + '-' + Math.random().toString(36).slice(2, 8),
      label,
      dataUrl: normalized.dataUrl,
      width: normalized.width,
      height: normalized.height,
      scope,
      createdAt: Date.now(),
    })
    if (!input.value.includes('@' + label)) {
      input.value = (input.value.trimEnd() + (input.value.trim() ? ' ' : '') + '@' + label + ' ').trimStart()
    }
    dismissedSuggestionInput.value = input.value
    await nextTick(focusActiveComposer)
  } catch (error) {
    imageAttachmentError.value = error instanceof Error ? error.message : '图片粘贴失败'
  }
}

async function handleComposerPaste(event: ClipboardEvent) {
  const items = Array.from(event.clipboardData?.items || [])
  const files = items
    .filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
    .map((item) => item.getAsFile())
    .filter((item): item is File => Boolean(item))
  if (!files.length) return
  event.preventDefault()
  for (const file of files.slice(0, AGENT_IMAGE_MAX_COUNT)) {
    await addAgentImage(file)
  }
}

function removeAgentImage(image: AgentImageAttachment) {
  agentImages.value = agentImages.value.filter((item) => item.id !== image.id)
  input.value = input.value
    .replace(new RegExp('\\s*@' + image.label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '(?=\\s|$)', 'g'), ' ')
    .replace(/\s{2,}/g, ' ')
    .trimStart()
  if (imagePreview.value?.id === image.id) imagePreview.value = null
}

function insertAgentImageReference(image: AgentImageAttachment) {
  const token = '@' + image.label
  if (!input.value.includes(token)) {
    input.value = (input.value.trimEnd() + (input.value.trim() ? ' ' : '') + token + ' ').trimStart()
  }
  dismissedSuggestionInput.value = input.value
  focusActiveComposer()
}

function resolveAgentImageReferences(value: string) {
  const images = currentAgentImages.value
  const labels = Array.from(value.matchAll(/@图片\d+/g), (match) => match[0].slice(1))
  if (labels.length) {
    const uniqueLabels = Array.from(new Set(labels))
    const selected: AgentImageAttachment[] = []
    for (const label of uniqueLabels) {
      const image = images.find((item) => item.label === label)
      if (!image) return { images: [] as AgentImageAttachment[], error: '找不到“@' + label + '”，请重新选择图片。' }
      selected.push(image)
    }
    return { images: selected, error: '' }
  }
  if (images.length === 1) return { images: [images[0]], error: '' }
  if (images.length > 1) {
    return { images: [] as AgentImageAttachment[], error: '当前有多张图片，请在问题里用 @图片1、@图片2 指定要让智能体看的图片。' }
  }
  return { images: [] as AgentImageAttachment[], error: '' }
}

function agentMessageWithImageContext(value: string, images: AgentImageAttachment[]) {
  if (!images.length) return value
  const mapping = images.map((image, index) => image.label + '=第' + String(index + 1) + '张附件').join('；')
  return value +
    '\n\n【本轮图片上下文】' + mapping +
    '\n请结合被引用图片中直接可见的文字、布局、控件、商品或对象理解用户指向；看不清的内容不要猜，也不要把图片之外的信息当成事实。'
}

type RecognizedAgentImage = {
  label: string
  result: Awaited<ReturnType<typeof previewRecognizeLiveAgentPlanImage>>
}

async function agentImageAttachmentToFile(image: AgentImageAttachment) {
  const response = await fetch(image.dataUrl)
  const blob = await response.blob()
  const mimeType = blob.type || 'image/webp'
  const extension =
    mimeType === 'image/png'
      ? 'png'
      : mimeType === 'image/jpeg'
        ? 'jpg'
        : 'webp'
  return new File([blob], image.label + '.' + extension, { type: mimeType })
}

async function resolveLiveStrategyTenantID(roomId: number) {
  const response = await getRooms()
  return response.items.find((item) => item.id === roomId)?.tenant_id
}

async function recognizeLiveStrategyImages(
  planId: number,
  roomId: number,
  images: AgentImageAttachment[],
): Promise<RecognizedAgentImage[]> {
  const tenantId = await resolveLiveStrategyTenantID(roomId)
  const recognized: RecognizedAgentImage[] = []
  for (const image of images) {
    const file = await agentImageAttachmentToFile(image)
    const result = await previewRecognizeLiveAgentPlanImage(planId, file, tenantId)
    recognized.push({ label: image.label, result })
  }
  return recognized
}

function liveStrategyRecognizedImageContext(items: RecognizedAgentImage[]) {
  return items
    .map(({ label, result }) => {
      const product = result.product
      const parts = ['【' + label + '识别结果】']
      if (result.text.trim()) parts.push('图片文字：' + result.text.trim())
      if (result.visual_context.trim()) parts.push('图片可见内容：' + result.visual_context.trim())
      if (product?.product_name?.trim()) parts.push('商品名称：' + product.product_name.trim())
      if (product?.spec?.trim()) parts.push('规格：' + product.spec.trim())
      if (product?.daily_price?.trim()) parts.push('日常价：' + product.daily_price.trim())
      if (product?.quantity?.trim()) parts.push('数量：' + product.quantity.trim())
      if (product?.audience?.trim()) parts.push('适用人群：' + product.audience.trim())
      if (result.warnings?.length) parts.push('识别提示：' + result.warnings.join('；'))
      return parts.join('\n')
    })
    .join('\n\n')
}

function liveStrategyRecognizedTextPreview(items: RecognizedAgentImage[]) {
  return items
    .map(({ label, result }) => {
      const text = result.text.trim()
      const parts = ['【' + label + ' 文字识别】']
      parts.push(text || '未识别到清晰文字。')
      if (result.warnings?.length) {
        parts.push('识别提示：' + result.warnings.join('；'))
      }
      return parts.join('\n')
    })
    .join('\n\n')
}

function liveStrategyMessageWithRecognizedImages(value: string, recognized: RecognizedAgentImage[]) {
  const context = liveStrategyRecognizedImageContext(recognized)
  if (!context) return value
  return (
    value +
    '\n\n【图片识别后的结构化上下文】\n' +
    context +
    '\n\n以上只代表图片中直接可见并识别出的内容；没有识别到的字段保持为空，不得猜测。'
  )
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
      speechChoice: item.speechChoice
        ? {
            ...item.speechChoice,
            status: item.speechChoice.status === 'sending' ? 'pending' : item.speechChoice.status,
          }
        : undefined,
      action:
        item.action?.type === 'add_live_image_product' ||
        item.action?.type === 'add_live_product' ||
        item.action?.type === 'add_live_benefit' ||
        item.action?.type === 'confirm_live_product_link_correction' ||
        item.action?.type === 'clarify_live_product_update' ||
        item.action?.type === 'confirm_live_product_update' ||
        item.action?.type === 'confirm_live_product_disable' ||
        item.action?.type === 'clarify_live_benefit_target' ||
        item.action?.type === 'clarify_live_benefit_update' ||
        item.action?.type === 'confirm_live_benefit_update' ||
        item.action?.type === 'confirm_live_benefit_disable' ||
        item.action?.type === 'add_live_fact' ||
        item.action?.type === 'confirm_live_fact_update' ||
        item.action?.type === 'confirm_live_fact_disable' ||
        item.action?.type === 'add_live_script_reference' ||
        item.action?.type === 'confirm_live_script_reference_update' ||
        item.action?.type === 'confirm_live_script_reference_disable' ||
        item.action?.type === 'confirm_live_plan_bind' ||
        item.action?.type === 'confirm_live_plan_unbind' ||
        item.action?.type === 'confirm_live_plan_switch' ||
        item.action?.type === 'clarify_live_strategy_intent'
          ? item.action
          : undefined,
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
    speechChoice: item.speechChoice
      ? {
          ...item.speechChoice,
          status: item.speechChoice.status === 'sending' ? 'pending' : item.speechChoice.status,
        }
      : undefined,
    action:
      item.action?.type === 'add_live_image_product' ||
      item.action?.type === 'add_live_product' ||
      item.action?.type === 'add_live_benefit' ||
      item.action?.type === 'confirm_live_product_link_correction' ||
      item.action?.type === 'clarify_live_product_update' ||
      item.action?.type === 'confirm_live_product_update' ||
      item.action?.type === 'confirm_live_product_disable' ||
      item.action?.type === 'clarify_live_benefit_target' ||
      item.action?.type === 'clarify_live_benefit_update' ||
      item.action?.type === 'confirm_live_benefit_update' ||
      item.action?.type === 'confirm_live_benefit_disable' ||
      item.action?.type === 'add_live_fact' ||
      item.action?.type === 'confirm_live_fact_update' ||
      item.action?.type === 'confirm_live_fact_disable' ||
      item.action?.type === 'add_live_script_reference' ||
      item.action?.type === 'confirm_live_script_reference_update' ||
      item.action?.type === 'confirm_live_script_reference_disable' ||
      item.action?.type === 'confirm_live_plan_bind' ||
      item.action?.type === 'confirm_live_plan_unbind' ||
      item.action?.type === 'confirm_live_plan_switch' ||
      item.action?.type === 'clarify_live_strategy_intent'
        ? item.action
        : undefined,
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
    if (liveStrategyMode.value === 'script') return '直播策略 · 口播样稿'
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
    if (liveStrategyMode.value === 'script') return '已进入当前直播间口播样稿上下文。'
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
    if (liveStrategyMode.value === 'script') return '输入口播样稿要求……'
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
    if (liveStrategyMode.value === 'script') return ['口播样稿', '完整原文', '结构参考']
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

type AgentDisplayBlock =
  | { kind: 'heading'; text: string }
  | { kind: 'prompt'; text: string }
  | { kind: 'paragraph'; text: string }
  | { kind: 'bullet'; text: string }
  | { kind: 'numbered'; marker: string; text: string }
  | { kind: 'field'; label: string; text: string }
  | { kind: 'choices'; items: string[]; prompt?: string }
  | { kind: 'examples'; items: string[] }

function cleanAgentDisplayInline(value: string) {
  return String(value || '')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/__([^_]+)__/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/[ \t]{2,}/g, ' ')
    .trim()
}

function splitAgentDisplayItems(value: string) {
  return String(value || '')
    .split(/[、，,；;]/)
    .map((item) => cleanAgentDisplayInline(item).replace(/[。；;]$/, ''))
    .filter(Boolean)
}

function splitAgentDisplayExamples(value: string) {
  return String(value || '')
    .replace(/^[“\"]|[”\"]$/g, '')
    .split(/[”\"]\s*或\s*[“\"]|\s+或\s+|；|;/)
    .map((item) => cleanAgentDisplayInline(item).replace(/^[“\"]|[”\"]$/g, '').replace(/[。；;]$/, ''))
    .filter(Boolean)
}

function structuredAgentPromptBlocks(line: string): AgentDisplayBlock[] | null {
  const optionMarker = line.match(/(?:可选|选项)[：:]/)
  if (!optionMarker || optionMarker.index === undefined) return null

  const before = cleanAgentDisplayInline(line.slice(0, optionMarker.index))
  let rest = line.slice(optionMarker.index + optionMarker[0].length).trim()
  if (!before || !rest) return null

  const optionEnd = rest.search(/[。！？!?](?=\s|$)/)
  const optionText = cleanAgentDisplayInline(optionEnd >= 0 ? rest.slice(0, optionEnd) : rest)
  rest = optionEnd >= 0 ? rest.slice(optionEnd + 1).trim() : ''
  const choices = splitAgentDisplayItems(optionText)
  if (choices.length < 2) return null

  const blocks: AgentDisplayBlock[] = [{ kind: 'prompt', text: before }]
  blocks.push({ kind: 'choices', items: choices, prompt: before })

  if (rest) {
    const exampleMarker = rest.match(/(?:例如|比如)[：:]/)
    if (exampleMarker && exampleMarker.index !== undefined) {
      const intro = cleanAgentDisplayInline(rest.slice(0, exampleMarker.index).replace(/[，,\s]+$/, ''))
      const exampleText = cleanAgentDisplayInline(rest.slice(exampleMarker.index + exampleMarker[0].length))
      if (intro) blocks.push({ kind: 'paragraph', text: intro })
      const examples = splitAgentDisplayExamples(exampleText)
      if (examples.length) blocks.push({ kind: 'examples', items: examples })
    } else {
      blocks.push({ kind: 'paragraph', text: cleanAgentDisplayInline(rest) })
    }
  }
  return blocks
}

function fillAgentStructuredInput(value: string) {
  input.value = value
  dismissedSuggestionInput.value = ''
  expanded.value = true
  drawerOpen.value = true
  activeComposer.value = 'drawer'
  void nextTick(() => {
    const element = drawerInputEl.value
    if (!element) return
    element.focus()
    const end = element.value.length
    element.setSelectionRange(end, end)
  })
}

function chooseAgentDisplayOption(block: Extract<AgentDisplayBlock, { kind: 'choices' }>, choice: string) {
  const prompt = String(block.prompt || '')
  if (/添加哪类事实|添加.*事实/.test(prompt)) {
    const factTemplates: Record<string, string> = {
      发货物流: '添加事实 发货物流-快递方式：',
      身份产地: '添加事实 身份产地-产地：',
      产品卖点: '添加事实 产品卖点-核心优势：',
      交易售后: '添加事实 交易售后-售后规则：',
    }
    fillAgentStructuredInput(factTemplates[choice] || ('添加事实 ' + choice + '-'))
    return
  }
  fillAgentStructuredInput(choice)
}

function chooseAgentDisplayExample(example: string) {
  const normalized = cleanAgentDisplayInline(example)
  if (!normalized) return
  if (/^[^：:]{1,24}-[^：:]{1,40}[：:]/.test(normalized)) {
    fillAgentStructuredInput('添加事实 ' + normalized)
    return
  }
  fillAgentStructuredInput(normalized)
}

function formatAgentMessageBlocks(value: string): AgentDisplayBlock[] {
  let text = String(value || '').replace(/\r\n?/g, '\n')
  if (!text.trim()) return []

  text = text
    .replace(/\s*\*\*\s*([^*\n：:]{1,20}[：:])\s*\*\*\s*/g, '\n$1 ')
    .replace(/\s*\*\*\s*([^*\n]{2,24})\s*\*\*\s*/g, '\n$1\n')
    .replace(/(^|\n)\s*[＊*•·]\s+/g, '$1• ')
    .replace(/\s+[＊*•·]\s+(?=[^\n])/g, '\n• ')
    .replace(/\n{3,}/g, '\n\n')
    .trim()

  const blocks: AgentDisplayBlock[] = []
  for (const rawLine of text.split('\n')) {
    const line = cleanAgentDisplayInline(rawLine)
    if (!line) continue

    const structuredPrompt = structuredAgentPromptBlocks(line)
    if (structuredPrompt) {
      blocks.push(...structuredPrompt)
      continue
    }

    const bullet = line.match(/^•\s*(.+)$/)
    if (bullet) {
      blocks.push({ kind: 'bullet', text: cleanAgentDisplayInline(bullet[1]) })
      continue
    }

    const numbered = line.match(/^(\d{1,2}[.、）)])\s*(.+)$/)
    if (numbered) {
      blocks.push({ kind: 'numbered', marker: numbered[1], text: cleanAgentDisplayInline(numbered[2]) })
      continue
    }

    const field = line.match(/^([^：:]{1,16})[：:]\s*(.+)$/)
    const fieldLabel = field ? cleanAgentDisplayInline(field[1]) : ''
    const looksLikeFieldLabel =
      Boolean(field) &&
      fieldLabel.length <= 10 &&
      !/[。！？!?；;，,]/.test(fieldLabel) &&
      !/^(?:请问|请|如果|可以|可选|例如|比如|说明|提示)/.test(fieldLabel) &&
      !/\s/.test(fieldLabel)
    if (field && looksLikeFieldLabel) {
      blocks.push({
        kind: 'field',
        label: fieldLabel,
        text: cleanAgentDisplayInline(field[2]),
      })
      continue
    }

    const looksLikeHeading =
      line.length <= 24 &&
      !/[。！？!?；;，,]$/.test(line) &&
      /(?:配置|结果|说明|建议|提示|活动福利|商品信息|当前状态|下一步|确认|注意事项|处理方式)$/.test(line)
    if (looksLikeHeading) {
      blocks.push({ kind: 'heading', text: line })
      continue
    }

    blocks.push({ kind: 'paragraph', text: line })
  }
  return blocks
}

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
        { kind: 'capability', label: '新增样稿', description: '新增一篇完整历史口播稿作为生成参考', insertText: '新增口播样稿：' },
        { kind: 'capability', label: '完整原文', description: '保存从开场到结尾的整篇样稿原文', insertText: '这是一篇完整口播样稿：' },
        { kind: 'capability', label: '结构参考', description: '只参考结构、节奏、转场和表达，不沿用旧事实', insertText: '参考这篇样稿的结构和讲法：' },
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
    const imageSuggestions: SuggestionItem[] = currentAgentImages.value
      .filter((item) => !trigger.query || item.label.toLowerCase().includes(trigger.query))
      .map((item) => ({
        kind: 'image',
        label: item.label,
        description: '本会话图片 · ' + item.width + '×' + item.height,
        insertText: '@' + item.label + ' ',
      }))
    const departmentSuggestions: SuggestionItem[] = isInternalAgentProfile.value
      ? systemContext.value.departments
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
      : []
    return [...imageSuggestions, ...departmentSuggestions].slice(0, 12)
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
  triggerState.value?.symbol === '@'
    ? (currentAgentImages.value.length ? (isInternalAgentProfile.value ? '选择图片或部门' : '选择图片') : '选择部门')
    : '选择能力',
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

function updateChatScrollState() {
  const element = chatEl.value
  if (!element) {
    chatNearBottom.value = true
    chatHasOverflow.value = false
    return
  }
  const distanceToBottom = element.scrollHeight - element.scrollTop - element.clientHeight
  chatHasOverflow.value = element.scrollHeight > element.clientHeight + 8
  chatNearBottom.value = distanceToBottom <= 48
}

function handleChatScroll() {
  updateChatScrollState()
}

function handleDrawerWheel(event: WheelEvent) {
  event.stopPropagation()
  const element = chatEl.value
  if (!element || drawerTab.value !== 'chat') return
  const target = event.target
  if (target instanceof Node && element.contains(target)) return
  if (Math.abs(event.deltaY) <= Math.abs(event.deltaX)) return
  event.preventDefault()
  element.scrollTop += event.deltaY
  updateChatScrollState()
}

async function scrollChatToBottom(behavior: ScrollBehavior = 'auto') {
  await nextTick()
  const element = chatEl.value
  if (!element) return
  element.scrollTo({ top: element.scrollHeight, behavior })
  if (behavior === 'smooth') {
    window.setTimeout(updateChatScrollState, 180)
  } else {
    updateChatScrollState()
  }
}

function jumpChatToBottom() {
  void scrollChatToBottom('smooth')
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
}

function latestPendingLiveProductCorrection() {
  return latestCurrentPendingLiveStrategyAction(new Set(['confirm_live_product_link_correction']))
}

function latestPendingLiveImageProduct() {
  return latestCurrentPendingLiveStrategyAction(new Set(['add_live_image_product', 'add_live_product']))
}

function latestPendingLiveBenefit() {
  return latestCurrentPendingLiveStrategyAction(new Set(['add_live_benefit']))
}

function latestCurrentPendingLiveStrategyAction(types: Set<string>) {
  const roomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
  const planId = liveStrategyPlanId.value
  const scope = conversationScopeForDomain('live-strategy')
  for (let index = messages.value.length - 1; index >= 0; index -= 1) {
    const message = messages.value[index]
    if (message.domain !== 'live-strategy' || message.conversationScope !== scope) continue
    if (message.role === 'user') return null
    const action = message.action
    if (!action || !types.has(action.type)) continue
    if (Number(action.payload.plan_id || 0) !== planId) continue
    if (Number(action.payload.room_id || 0) !== roomId) continue
    return message
  }
  return null
}

function latestPendingLiveStrategyAction() {
  const supported = new Set([
    'confirm_live_product_link_correction',
    'add_live_image_product',
    'add_live_product',
    'add_live_benefit',
    'clarify_live_product_update',
    'confirm_live_product_update',
    'confirm_live_product_disable',
    'clarify_live_benefit_target',
    'clarify_live_benefit_update',
    'confirm_live_benefit_update',
    'confirm_live_benefit_disable',
    'add_live_fact',
    'confirm_live_fact_update',
    'confirm_live_fact_disable',
    'add_live_script_reference',
    'confirm_live_script_reference_update',
    'confirm_live_script_reference_disable',
    'confirm_live_plan_bind',
    'confirm_live_plan_unbind',
    'confirm_live_plan_switch',
    'clarify_live_strategy_intent',
  ])
  return latestCurrentPendingLiveStrategyAction(supported)
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

function notifyLivePlanModuleUpdated(planId: number, module: string) {
  window.dispatchEvent(
    new CustomEvent('live-agent-plan-module-updated', {
      detail: { plan_id: planId, module },
    }),
  )
}

function focusLivePlanModule(planId: number, module: LiveStrategyMode) {
  window.dispatchEvent(
    new CustomEvent('live-agent-plan-module-updated', {
      detail: { plan_id: planId, module, focus: true },
    }),
  )
}

function liveImageProductName(value: string, linkKey: string) {
  const explicit = liveProductField(value, ['商品名称', '商品名', '商品'])
  if (explicit) return explicit

  const linkNumber = linkKey.match(/^(\d+)号链接$/)?.[1] || ''
  const patterns = [
    linkNumber
      ? new RegExp(linkNumber + '\\s*号?\\s*(?:商品)?链接\\s*商品\\s*[:：]?\\s*([^，,；;\\n\\r。]+)', 'i')
      : null,
    /(?:识别为|商品为|商品是|图中商品(?:为|是))\s*[:：]?\s*([^，,；;\n\r。]+)/i,
  ].filter((item): item is RegExp => Boolean(item))

  for (const pattern of patterns) {
    const matched = value.match(pattern)
    if (matched?.[1]) return matched[1].trim()
  }
  return ''
}

function normalizeImageRecognitionText(value: string) {
  return String(value || '')
    .replace(/\*\*/g, '')
    .replace(/^\s*[•*-]\s*/gm, '')
    .trim()
}

function liveImageProductCandidate(userText: string, replyText: string) {
  const cleanReply = normalizeImageRecognitionText(replyText)
  const combined = userText + '\n' + cleanReply
  const linkKey = liveProductLinkKeyFromText(combined)
  if (!linkKey) return null

  const productName = liveImageProductName(cleanReply, linkKey)
  if (!productName) return null

  return {
    linkKey,
    productName,
    spec: liveProductField(cleanReply, ['规格/卖点', '规格卖点', '规格']),
    dailyPrice: liveProductField(cleanReply, ['价格', '日常价', '原价']),
    quantity: liveProductField(cleanReply, ['数量']),
    audience: liveProductField(cleanReply, ['适用人群', '适用对象', '适用']),
    sourceText: cleanReply,
  }
}

function liveBenefitGiftFromText(value: string) {
  const explicit = liveProductField(value, ['赠品内容', '赠品', '福利'])
  if (explicit) return explicit
  const matched = value.match(/(?:赠送|加赠|赠|送)\s*([^，,；;。\n\r]+)/)
  return matched?.[1]?.trim() || ''
}

function liveBenefitActivityPriceFromText(value: string) {
  return liveProductField(value, ['活动价', '优惠价', '秒杀价', '到手价'])
}

function liveBenefitCandidateFromText(
  userText: string,
  replyText: string,
  productName = '',
): LiveAgentPlanBenefitCandidate | null {
  const cleanReply = normalizeImageRecognitionText(replyText)
  const combined = userText + '\n' + cleanReply
  const linkKey = liveProductLinkKeyFromText(combined)
  const gift = liveBenefitGiftFromText(cleanReply) || liveBenefitGiftFromText(userText)
  const activityPrice = liveBenefitActivityPriceFromText(cleanReply) || liveBenefitActivityPriceFromText(userText)
  const activityFromReply = liveProductField(cleanReply, ['活动内容', '活动规则', '触发条件'])
  const activity = activityFromReply || ((gift || activityPrice) ? userText.trim() : '')
  if (!gift && !activityPrice && !activity) return null

  const startsAt = normalizeBenefitTimeValue(liveProductField(cleanReply, ['开始时间', '生效时间']))
  const endsAt = normalizeBenefitTimeValue(liveProductField(cleanReply, ['结束时间', '失效时间', '截止时间']))
  const hasCompleteWindow = Boolean(startsAt && endsAt)
  const keyBase = linkKey || 'room'
  return {
    key: keyBase + ':current-benefit',
    link_key: linkKey,
    product_name: productName || liveProductField(cleanReply, ['商品名称', '商品名']),
    activity_price: activityPrice,
    gift,
    activity,
    starts_at: startsAt,
    ends_at: endsAt,
    review_bucket: hasCompleteWindow ? 'adoptable' : 'discuss',
    review_reason: hasCompleteWindow
      ? '用户通过智能体自然语言明确新增活动福利。'
      : '活动内容已明确，但未提供完整开始/结束时间；采纳后只能进入活动草稿，不会进入直播生成。',
    source_quotes: [userText.trim()],
  }
}

function benefitCandidateReplyForAdd(value: string, candidate: LiveAgentPlanBenefitCandidate) {
  const hasWindow = Boolean(candidate.starts_at && candidate.ends_at)
  return String(value || '').trim() + '\n\n' + (
    hasWindow
      ? '我已经整理成活动福利候选。当前还没有写入，确认无误后点击下方“添加活动福利”。'
      : '我已经整理成活动福利候选。当前还没有写入；由于没有完整有效期，点击“添加活动福利”后会先进入活动草稿，不会立即用于直播。'
  )
}

function benefitCandidateSummary(candidate: LiveAgentPlanBenefitCandidate) {
  const lines = ['我理解你要新增一条活动福利：']
  if (candidate.link_key) lines.push('链接：' + candidate.link_key)
  if (candidate.product_name) lines.push('商品：' + candidate.product_name)
  if (candidate.activity_price) lines.push('活动价：' + candidate.activity_price)
  if (candidate.gift) lines.push('赠品：' + candidate.gift)
  if (candidate.activity) lines.push('活动内容：' + candidate.activity)
  lines.push('开始时间：' + (candidate.starts_at || '未设置'))
  lines.push('结束时间：' + (candidate.ends_at || '未设置'))
  return lines.join('\n')
}

function benefitExistingTime(value?: string) {
  const raw = String(value || '').trim()
  if (!raw) return ''
  return raw.replace('T', ' ').replace(/Z$/, '').slice(0, 16)
}

function updatePendingBenefitCandidateFromText(value: string) {
  const pending = latestPendingLiveBenefit()
  if (!pending?.action || !isLiveBenefitUpdateIntent(value)) return false
  const values = liveBenefitUpdateValues(value)
  const hasValue = Boolean(values.productName || values.activityPrice || values.gift || values.activity || values.startsAt || values.endsAt)
  if (!hasValue) return false
  if (values.productName) pending.action.payload.product_name = values.productName
  if (values.activityPrice) pending.action.payload.activity_price = values.activityPrice
  if (values.gift) pending.action.payload.gift = values.gift
  if (values.activity) pending.action.payload.activity = values.activity
  if (values.startsAt) pending.action.payload.starts_at = values.startsAt
  if (values.endsAt) pending.action.payload.ends_at = values.endsAt
  pending.action.payload.review_bucket = pending.action.payload.starts_at && pending.action.payload.ends_at ? 'adoptable' : 'discuss'
  pending.action.summary = pending.action.payload.starts_at && pending.action.payload.ends_at
    ? '候选已按你的要求修改。点击“添加活动福利”后才写入当前方案。'
    : '候选已按你的要求修改，但有效期仍不完整；点击后会写入活动草稿。'
  return true
}

async function prepareLiveBenefitUpdateAction(planId: number, roomId: number, value: string) {
  const result = await getLiveAgentPlanBenefits(planId)
  const items = (result.items || []).filter((item) => item.status !== 'disabled')
  if (!items.length) {
    return { text: '当前方案还没有正式活动福利。请先新增活动福利。' }
  }
  const linkKey = liveProductLinkKeyFromText(value)
  const matched = linkKey ? items.filter((item) => item.link_key === linkKey) : items
  if (!matched.length) {
    return { text: '当前方案里没有找到“' + linkKey + '”对应的活动福利。' }
  }
  if (matched.length > 1 && !linkKey) {
    return {
      text: '当前方案有多条活动福利，请先选择要修改哪一条：',
      action: {
        type: 'clarify_live_benefit_target',
        title: '选择要修改的活动',
        summary: '选择后我会把对应修改指令放到输入框。',
        risk_level: 'low',
        requires_confirmation: false,
        payload: {
          plan_id: planId,
          room_id: roomId,
          intent_options: matched.slice(0, 5).map((item) => ({
            id: String(item.id),
            label: item.link_key || item.product_name || '活动福利',
            description: item.activity || item.gift || item.activity_price || '活动',
            command: '修改' + (item.link_key || '活动') + '活动 ',
          })),
        },
      } as SystemAgentActionPreview,
    }
  }

  const existing = matched[0]
  const values = liveBenefitUpdateValues(value)
  const hasValue = Boolean(values.productName || values.activityPrice || values.gift || values.activity || values.startsAt || values.endsAt)
  const base = existing.link_key || existing.product_name || '当前活动'
  if (!hasValue) {
    return {
      text: '你要修改“' + base + '”的活动福利，但还没有说明改哪一项。请选择：',
      action: {
        type: 'clarify_live_benefit_update',
        title: '选择要修改的字段',
        summary: '选择字段后，在输入框补上新值并发送；确认卡通过后才会生成新版本。',
        risk_level: 'low',
        requires_confirmation: false,
        payload: {
          plan_id: planId,
          room_id: roomId,
          benefit_id: existing.id,
          benefit_key: existing.key,
          intent_options: [
            { id: 'product_name', label: '商品名称', description: existing.product_name || '未设置', command: '修改' + base + '活动 商品名称：' },
            { id: 'activity_price', label: '活动价', description: existing.activity_price || '未设置', command: '修改' + base + '活动 活动价：' },
            { id: 'gift', label: '赠品', description: existing.gift || '未设置', command: '修改' + base + '活动 赠品：' },
            { id: 'activity', label: '活动内容', description: existing.activity || '未设置', command: '修改' + base + '活动 活动内容：' },
            { id: 'starts_at', label: '开始时间', description: benefitExistingTime(existing.starts_at) || '未设置', command: '修改' + base + '活动 开始时间：' },
            { id: 'ends_at', label: '结束时间', description: benefitExistingTime(existing.ends_at) || '未设置', command: '修改' + base + '活动 结束时间：' },
          ],
        },
      } as SystemAgentActionPreview,
    }
  }

  const next = {
    productName: values.productName || existing.product_name || '',
    activityPrice: values.activityPrice || existing.activity_price || '',
    gift: values.gift || existing.gift || '',
    activity: values.activity || existing.activity || '',
    startsAt: values.startsAt || benefitExistingTime(existing.starts_at),
    endsAt: values.endsAt || benefitExistingTime(existing.ends_at),
  }
  const changed =
    next.productName !== (existing.product_name || '') ||
    next.activityPrice !== (existing.activity_price || '') ||
    next.gift !== (existing.gift || '') ||
    next.activity !== (existing.activity || '') ||
    next.startsAt !== benefitExistingTime(existing.starts_at) ||
    next.endsAt !== benefitExistingTime(existing.ends_at)
  if (!changed) return { text: '你提供的新值和当前活动福利完全一致，所以没有生成新版本。' }

  return {
    text: '我已经整理好活动福利的修改内容。请核对“修改前 → 修改后”，确认后才会写入新版本。',
    action: {
      type: 'confirm_live_benefit_update',
      title: '确认修改活动福利',
      summary: '没有提到的字段保持原值；确认后才写入正式活动福利并生成新版本。',
      risk_level: 'low',
      requires_confirmation: true,
      payload: {
        plan_id: planId,
        room_id: roomId,
        benefit_id: existing.id,
        benefit_key: existing.key,
        current_version_no: existing.version_no,
        link_key: existing.link_key || '',
        current_link_key: existing.link_key || '',
        current_product_name: existing.product_name || '',
        current_activity_price: existing.activity_price || '',
        current_gift: existing.gift || '',
        current_activity: existing.activity || '',
        current_starts_at: benefitExistingTime(existing.starts_at),
        current_ends_at: benefitExistingTime(existing.ends_at),
        product_name: next.productName,
        activity_price: next.activityPrice,
        gift: next.gift,
        activity: next.activity,
        starts_at: next.startsAt,
        ends_at: next.endsAt,
        source_text: value,
      },
    } as SystemAgentActionPreview,
  }
}

async function pushLiveBenefitUpdatePreview(domain: AgentDomain, planId: number, roomId: number, value: string) {
  const prepared = await prepareLiveBenefitUpdateAction(planId, roomId, value)
  focusLivePlanModule(planId, 'benefits')
  pushAgentMessage(domain, prepared.text, prepared.action)
}

function imageRecognitionReplyForAdd(value: string) {
  const clean = String(value || '')
    .replace(/收到[，,]?\s*已根据图片信息更新[^。！？]*[。！？]?/g, '已识别出图片中的商品信息，以下内容尚未写入。')
    .replace(/已根据图片信息更新/g, '已根据图片信息识别出')
  return clean + '\n\n确认无误后，点击下方“添加”写入当前直播智能体方案。'
}

async function prepareLiveProductUpdateAction(planId: number, roomId: number, value: string) {
  const linkKey = liveProductLinkKeyFromText(value)
  if (!linkKey) {
    return { text: '我知道你要修改商品链接，但还缺少链接编号。请例如说“修改1号链接”。' }
  }

  const current = await getLiveAgentPlanProductLinks(planId)
  const existing = (current.items || []).find((item) => item.link_key === linkKey)
  if (!existing) {
    return { text: '当前方案里没有“' + linkKey + '”。如果这是新商品，请改说“添加' + linkKey + ' 商品名”。' }
  }

  const values = liveProductUpdateValues(value)
  const hasExplicitValue = Boolean(
    values.productName || values.spec || values.dailyPrice || values.quantity || values.audience,
  )
  if (!hasExplicitValue) {
    return {
      text:
        '你要修改“' + linkKey + ' · ' + (existing.product_name || '未命名商品') + '”，但还没有说明改哪一项。请选择：',
      action: {
        type: 'clarify_live_product_update',
        title: '选择要修改的字段',
        summary: '选择字段后我会把完整修改指令放到输入框；填写新值并发送后，再给你“修改前 → 修改后”的确认。',
        risk_level: 'low',
        requires_confirmation: false,
        payload: {
          plan_id: planId,
          room_id: roomId,
          product_link_id: existing.id,
          link_key: linkKey,
          intent_options: [
            { id: 'product_name', label: '商品名称', description: existing.product_name || '未设置', command: '修改' + linkKey + ' 商品名称：' },
            { id: 'spec', label: '规格', description: existing.spec || '未设置', command: '修改' + linkKey + ' 规格：' },
            { id: 'daily_price', label: '日常价', description: existing.daily_price || '未设置', command: '修改' + linkKey + ' 日常价：' },
            { id: 'quantity', label: '数量', description: existing.quantity || '未设置', command: '修改' + linkKey + ' 数量：' },
            { id: 'audience', label: '适用人群', description: existing.audience || '未设置', command: '修改' + linkKey + ' 适用人群：' },
          ],
        },
      } as SystemAgentActionPreview,
    }
  }

  const next = {
    productName: values.productName || existing.product_name || '',
    spec: values.spec || existing.spec || '',
    dailyPrice: values.dailyPrice || existing.daily_price || '',
    quantity: values.quantity || existing.quantity || '',
    audience: values.audience || existing.audience || '',
  }
  const changed =
    next.productName !== (existing.product_name || '') ||
    next.spec !== (existing.spec || '') ||
    next.dailyPrice !== (existing.daily_price || '') ||
    next.quantity !== (existing.quantity || '') ||
    next.audience !== (existing.audience || '')
  if (!changed) {
    return { text: '你提供的新值和“' + linkKey + '”当前正式数据完全一致，所以没有生成新版本。' }
  }

  return {
    text: '我已经整理好“' + linkKey + '”的修改内容。请核对修改前后，确认后才会写入并生成新版本。',
    action: {
      type: 'confirm_live_product_update',
      title: '确认修改' + linkKey,
      summary: '未修改的字段保持原值；点击“确认修改”后才写入正式商品链接。',
      risk_level: 'low',
      requires_confirmation: true,
      payload: {
        plan_id: planId,
        room_id: roomId,
        product_link_id: existing.id,
        current_version_no: existing.version_no,
        link_key: linkKey,
        current_product_name: existing.product_name || '',
        current_spec: existing.spec || '',
        current_daily_price: existing.daily_price || '',
        current_quantity: existing.quantity || '',
        current_audience: existing.audience || '',
        product_name: next.productName,
        spec: next.spec,
        daily_price: next.dailyPrice,
        quantity: next.quantity,
        audience: next.audience,
        source_text: value,
      },
    } as SystemAgentActionPreview,
  }
}

async function pushLiveProductUpdatePreview(
  domain: AgentDomain,
  planId: number,
  roomId: number,
  value: string,
) {
  const prepared = await prepareLiveProductUpdateAction(planId, roomId, value)
  focusLivePlanModule(planId, 'products')
  pushAgentMessage(domain, prepared.text, prepared.action)
}

function intentString(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

async function prepareLiveProductIntentAction(
  planId: number,
  roomId: number,
  sourceText: string,
  result: LiveStrategyIntentResponse,
) {
  const linkKey = intentString(result.target?.link_key)
  const changes = result.changes || {}
  const current = await getLiveAgentPlanProductLinks(planId)
  const items = current.items || []
  const existing = linkKey ? items.find((item) => item.link_key === linkKey) : undefined

  if (result.intent === 'product.add') {
    const productName = intentString(changes.product_name)
    if (!linkKey || !productName) {
      return {
        text: result.reply || '我已经判断这是新增商品链接，但还缺少链接编号或商品名称。请补充后再继续。',
      }
    }
    if (existing) {
      return {
        text: '当前方案已经有“' + linkKey + ' · ' + (existing.product_name || '未命名商品') + '”。如果要改现有内容，请直接说要修改的字段。',
      }
    }
    return {
      text: '我已经理解为新增“' + linkKey + '”。下面是模型提取出的结构化商品资料，确认后才写入数据库。',
      action: {
        type: 'add_live_product',
        title: '确认添加' + linkKey,
        summary: '尚未写入。点击“确认添加”后才进入当前直播智能体方案的正式商品链接。',
        risk_level: 'low',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          link_key: linkKey,
          product_name: productName,
          spec: intentString(changes.spec),
          daily_price: intentString(changes.daily_price),
          quantity: intentString(changes.quantity),
          audience: intentString(changes.audience),
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent === 'product.disable') {
    if (!linkKey) {
      return { text: result.reply || '我知道你想停用商品链接，但还不能确定是哪一个链接。' }
    }
    if (!existing) {
      return { text: '当前方案没有“' + linkKey + '”，所以没有可停用的数据。' }
    }
    return {
      text: '我已经定位到“' + linkKey + ' · ' + (existing.product_name || '未命名商品') + '”。停用后历史版本仍保留，请确认。',
      action: {
        type: 'confirm_live_product_disable',
        title: '确认停用' + linkKey,
        summary: '确认后这条商品链接不再进入当前正式方案；历史版本与审计记录仍保留。',
        risk_level: 'medium',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          product_link_id: existing.id,
          current_version_no: existing.version_no,
          link_key: existing.link_key,
          product_name: existing.product_name || '',
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent !== 'product.update') return null
  if (!linkKey) {
    return { text: result.reply || '我判断你要修改商品链接，但还不能确定目标链接。请说清楚是几号链接。' }
  }
  if (!existing) {
    return { text: '当前方案里没有“' + linkKey + '”。如果这是新商品，请改成新增。' }
  }
  const productName = intentString(changes.product_name)
  const spec = intentString(changes.spec)
  const dailyPrice = intentString(changes.daily_price)
  const quantity = intentString(changes.quantity)
  const audience = intentString(changes.audience)
  if (!productName && !spec && !dailyPrice && !quantity && !audience) {
    return { text: result.reply || '我已经定位到“' + linkKey + '”，但还没有明确要修改哪个字段。' }
  }
  const next = {
    productName: productName || existing.product_name || '',
    spec: spec || existing.spec || '',
    dailyPrice: dailyPrice || existing.daily_price || '',
    quantity: quantity || existing.quantity || '',
    audience: audience || existing.audience || '',
  }
  const changed =
    next.productName !== (existing.product_name || '') ||
    next.spec !== (existing.spec || '') ||
    next.dailyPrice !== (existing.daily_price || '') ||
    next.quantity !== (existing.quantity || '') ||
    next.audience !== (existing.audience || '')
  if (!changed) {
    return { text: '模型理解出的新值和“' + linkKey + '”当前正式数据一致，没有生成新版本。' }
  }
  return {
    text: '我已经按你的原话理解成商品链接修改。请核对“修改前 → 修改后”，确认后才会写入。',
    action: {
      type: 'confirm_live_product_update',
      title: '确认修改' + linkKey,
      summary: '结构化意图已确定；未修改字段保持原值，确认后才生成正式新版本。',
      risk_level: 'low',
      requires_confirmation: true,
      payload: {
        plan_id: planId,
        room_id: roomId,
        product_link_id: existing.id,
        current_version_no: existing.version_no,
        link_key: linkKey,
        current_product_name: existing.product_name || '',
        current_spec: existing.spec || '',
        current_daily_price: existing.daily_price || '',
        current_quantity: existing.quantity || '',
        current_audience: existing.audience || '',
        product_name: next.productName,
        spec: next.spec,
        daily_price: next.dailyPrice,
        quantity: next.quantity,
        audience: next.audience,
        source_text: sourceText,
      },
    } as SystemAgentActionPreview,
  }
}

async function prepareLiveBenefitIntentAction(
  planId: number,
  roomId: number,
  sourceText: string,
  result: LiveStrategyIntentResponse,
) {
  const linkKey = intentString(result.target?.link_key)
  const benefitKey = intentString(result.target?.benefit_key)
  const changes = result.changes || {}
  const currentBenefits = await getLiveAgentPlanBenefits(planId)
  const formalItems = (currentBenefits.items || []).filter((item) => item.status !== 'disabled')
  let existing = benefitKey
    ? formalItems.find((item) => item.key === benefitKey)
    : undefined
  if (!existing && linkKey) {
    const matches = formalItems.filter((item) => item.link_key === linkKey)
    if (matches.length === 1) existing = matches[0]
  }

  if (result.intent === 'benefit.add') {
    const activityPrice = intentString(changes.activity_price)
    const gift = intentString(changes.gift)
    const activity = intentString(changes.activity)
    if (!activityPrice && !gift && !activity) {
      return { text: result.reply || '我已经判断这是新增活动福利，但还缺少活动价、赠品或活动内容。' }
    }
    let productName = intentString(changes.product_name)
    if (!productName && linkKey) {
      const products = await getLiveAgentPlanProductLinks(planId)
      productName = (products.items || []).find((item) => item.link_key === linkKey)?.product_name || ''
    }
    const candidate: LiveAgentPlanBenefitCandidate = {
      link_key: linkKey,
      product_name: productName,
      activity_price: activityPrice,
      gift,
      activity,
      starts_at: intentString(changes.starts_at),
      ends_at: intentString(changes.ends_at),
      review_bucket: intentString(changes.starts_at) && intentString(changes.ends_at) ? 'adoptable' : 'discuss',
      source_quotes: [sourceText],
    }
    return {
      text: '我已经理解为新增活动福利。下面是结构化候选，确认后才写入；有效期不完整时只会进入草稿。',
      action: {
        type: 'add_live_benefit',
        title: '确认添加活动福利',
        summary: candidate.starts_at && candidate.ends_at
          ? '尚未写入。确认后写入当前方案，并根据有效期决定是否立即生效。'
          : '尚未写入，且有效期不完整；确认后只写入活动草稿。',
        risk_level: 'low',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          benefit_key: '',
          link_key: candidate.link_key || '',
          product_name: candidate.product_name || '',
          activity_price: candidate.activity_price || '',
          gift: candidate.gift || '',
          activity: candidate.activity || '',
          starts_at: candidate.starts_at || '',
          ends_at: candidate.ends_at || '',
          review_bucket: candidate.review_bucket || 'discuss',
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent === 'benefit.disable') {
    if (!existing) {
      return { text: result.reply || '我知道你想停用活动福利，但还不能唯一定位到哪一条活动。' }
    }
    return {
      text: '我已经定位到“' + (existing.link_key || existing.product_name || existing.key) + '”的活动福利。停用后将不再进入直播生成，请确认。',
      action: {
        type: 'confirm_live_benefit_disable',
        title: '确认停用活动福利',
        summary: '确认后状态改为停用；历史版本和审计记录继续保留。',
        risk_level: 'medium',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          benefit_id: existing.id,
          current_version_no: existing.version_no,
          benefit_key: existing.key,
          link_key: existing.link_key || '',
          product_name: existing.product_name || '',
          activity: existing.activity || '',
          gift: existing.gift || '',
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent !== 'benefit.update') return null
  if (!existing) {
    if (linkKey) {
      return { text: '我判断你要修改“' + linkKey + '”的活动福利，但当前正式方案里没有唯一对应活动。' }
    }
    return { text: result.reply || '我判断你要修改活动福利，但还不能唯一定位到哪一条活动。' }
  }
  const productName = intentString(changes.product_name)
  const activityPrice = intentString(changes.activity_price)
  const gift = intentString(changes.gift)
  const activity = intentString(changes.activity)
  const startsAt = intentString(changes.starts_at)
  const endsAt = intentString(changes.ends_at)
  if (!productName && !activityPrice && !gift && !activity && !startsAt && !endsAt) {
    return prepareLiveBenefitUpdateAction(planId, roomId, sourceText)
  }
  const next = {
    productName: productName || existing.product_name || '',
    activityPrice: activityPrice || existing.activity_price || '',
    gift: gift || existing.gift || '',
    activity: activity || existing.activity || '',
    startsAt: startsAt || benefitExistingTime(existing.starts_at),
    endsAt: endsAt || benefitExistingTime(existing.ends_at),
  }
  const changed =
    next.productName !== (existing.product_name || '') ||
    next.activityPrice !== (existing.activity_price || '') ||
    next.gift !== (existing.gift || '') ||
    next.activity !== (existing.activity || '') ||
    next.startsAt !== benefitExistingTime(existing.starts_at) ||
    next.endsAt !== benefitExistingTime(existing.ends_at)
  if (!changed) {
    return { text: '模型理解出的新值和当前活动福利一致，没有生成新版本。' }
  }
  return {
    text: '我已经按你的原话理解成活动福利修改。请核对“修改前 → 修改后”，确认后才写入正式新版本。',
    action: {
      type: 'confirm_live_benefit_update',
      title: '确认修改活动福利',
      summary: '结构化意图已确定；没提到的字段保持原值，确认后才生成新版本。',
      risk_level: 'low',
      requires_confirmation: true,
      payload: {
        plan_id: planId,
        room_id: roomId,
        benefit_id: existing.id,
        benefit_key: existing.key,
        current_version_no: existing.version_no,
        link_key: existing.link_key || '',
        current_link_key: existing.link_key || '',
        current_product_name: existing.product_name || '',
        current_activity_price: existing.activity_price || '',
        current_gift: existing.gift || '',
        current_activity: existing.activity || '',
        current_starts_at: benefitExistingTime(existing.starts_at),
        current_ends_at: benefitExistingTime(existing.ends_at),
        product_name: next.productName,
        activity_price: next.activityPrice,
        gift: next.gift,
        activity: next.activity,
        starts_at: next.startsAt,
        ends_at: next.endsAt,
        source_text: sourceText,
      },
    } as SystemAgentActionPreview,
  }
}

async function prepareLiveFactIntentAction(
  planId: number,
  roomId: number,
  sourceText: string,
  result: LiveStrategyIntentResponse,
) {
  const category = intentString(result.target?.fact_category)
  const factKey = intentString(result.target?.fact_key)
  const factValue = intentString(result.changes?.fact_value)
  const current = await getLiveAgentPlanFacts(planId)
  const items = current.items || []
  let existing = category && factKey
    ? items.find((item) => item.category === category && item.key === factKey)
    : undefined
  if (!existing && factKey) {
    const matches = items.filter((item) => item.key === factKey)
    if (matches.length === 1) existing = matches[0]
  }

  if (result.intent === 'fact.add') {
    if (!category || !factKey || !factValue) {
      return { text: result.reply || '我已经判断这是新增事实依据，但还缺少事实分类、名称或内容。请补充后再继续。' }
    }
    if (existing) {
      return { text: '当前方案已经存在“' + existing.key + '：' + existing.value + '”。如果要调整，请直接说修改后的内容。' }
    }
    return {
      text: '我已经理解为新增事实依据。下面是结构化结果，确认后才会写入当前方案。',
      action: {
        type: 'add_live_fact',
        title: '确认添加事实依据',
        summary: '事实会进入当前方案的正式事实库；确认前不会写数据库。',
        risk_level: 'low',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          fact_category: category,
          fact_key: factKey,
          fact_value: factValue,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent === 'fact.disable') {
    if (!existing) {
      return { text: result.reply || '我知道你想停用一条事实，但还不能唯一定位到哪一条。请说清楚事实名称。' }
    }
    return {
      text: '我已经定位到事实“' + existing.key + '：' + existing.value + '”。停用后直播智能体不再引用，请确认。',
      action: {
        type: 'confirm_live_fact_disable',
        title: '确认停用事实依据',
        summary: '确认后这条事实不再进入直播生成；历史版本与审计记录仍保留。',
        risk_level: 'medium',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          fact_id: existing.id,
          current_version_no: existing.version_no,
          fact_category: existing.category,
          fact_key: existing.key,
          current_fact_value: existing.value,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent !== 'fact.update') return null
  if (!existing) {
    return { text: result.reply || '我判断你要修改事实依据，但当前正式事实中还不能唯一定位目标。请补充事实名称。' }
  }
  if (!factValue) {
    return { text: result.reply || '我已经定位到“' + existing.key + '”，但还没有明确新的事实内容。' }
  }
  if (factValue === existing.value) {
    return { text: '模型理解出的新值和当前事实“' + existing.key + '”一致，没有生成新版本。' }
  }
  return {
    text: '我已经按你的原话理解成事实依据修改。请核对修改前后，确认后才写入。',
    action: {
      type: 'confirm_live_fact_update',
      title: '确认修改事实依据',
      summary: '只修改这条事实的内容；确认后生成正式新版本。',
      risk_level: 'low',
      requires_confirmation: true,
      payload: {
        plan_id: planId,
        room_id: roomId,
        fact_id: existing.id,
        current_version_no: existing.version_no,
        fact_category: existing.category,
        fact_key: existing.key,
        current_fact_value: existing.value,
        fact_value: factValue,
        source_text: sourceText,
      },
    } as SystemAgentActionPreview,
  }
}

function liveScriptExecutionModeFromText(value: string) {
  const compact = String(value || '').replace(/\s+/g, '')
  if (/(?:100%原话|百分百原话|照原文|逐字照说|一字不改|原话锁定)/.test(compact)) {
    return 'verbatim'
  }
  return 'intent'
}

async function prepareLiveScriptIntentAction(
  planId: number,
  roomId: number,
  sourceText: string,
  result: LiveStrategyIntentResponse,
) {
  const requestedKey = intentString(result.target?.script_reference_key)
  const requestedTitle = intentString(result.target?.script_title)
  const scriptText = intentString(result.changes?.script_text)
  const current = await getLiveAgentPlanScriptReferences(planId)
  const items = current.items || []
  let existing = requestedKey
    ? items.find((item) => item.reference_key === requestedKey)
    : undefined
  if (!existing && requestedTitle) {
    const exact = items.filter((item) => item.title === requestedTitle)
    if (exact.length === 1) existing = exact[0]
  }

  if (result.intent === 'script.add') {
    const title = requestedTitle || '口播样稿'
    const referenceKey = requestedKey || title
    if (!scriptText) {
      return { text: result.reply || '我已经判断这是新增口播样稿，但还缺少具体参考内容。请把想保存的讲法告诉我。' }
    }
    if (items.some((item) => item.reference_key === referenceKey)) {
      return { text: '当前方案已经存在“' + title + '”。如果要调整，请直接说修改后的话术内容。' }
    }
    const executionMode = liveScriptExecutionModeFromText(sourceText)
    return {
      text: '我已经理解为新增正式口播样稿。它只影响“怎么说”，不会自动把里面的商品描述升级成事实依据。确认后才写入。',
      action: {
        type: 'add_live_script_reference',
        title: '确认添加口播样稿',
        summary: executionMode === 'verbatim'
          ? '确认后进入当前方案正式口播样稿，并按“100%原话”执行。'
          : '确认后进入当前方案正式口播样稿，并按“意图参考”执行，允许自然改写。',
        risk_level: 'low',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          script_reference_key: referenceKey,
          script_title: title,
          script_text: scriptText,
          execution_mode: executionMode,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent === 'script.disable') {
    if (!existing) {
      return { text: result.reply || '我知道你想停用口播样稿，但还不能唯一定位到哪一条。请说出参考名称。' }
    }
    return {
      text: '我已经定位到口播样稿“' + existing.title + '”。停用后直播生成不再参考这条内容，请确认。',
      action: {
        type: 'confirm_live_script_reference_disable',
        title: '确认停用口播样稿',
        summary: '确认后停止参与直播生成；历史版本和审计记录继续保留。',
        risk_level: 'medium',
        requires_confirmation: true,
        payload: {
          plan_id: planId,
          room_id: roomId,
          script_reference_id: existing.id,
          script_reference_key: existing.reference_key,
          script_title: existing.title,
          current_script_text: existing.content_text,
          current_version_no: existing.version_no,
          execution_mode: existing.execution_mode,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }

  if (result.intent !== 'script.update') return null
  if (!existing) {
    return { text: result.reply || '我判断你要修改口播样稿，但还不能唯一定位目标。请说出参考名称。' }
  }
  if (!scriptText) {
    return { text: result.reply || '我已经定位到“' + existing.title + '”，但还没有明确新的话术内容。' }
  }
  const explicitMode = liveScriptExecutionModeFromText(sourceText)
  const hasModeInstruction = /(?:100%原话|百分百原话|照原文|逐字照说|一字不改|原话锁定|按照这个意思|按这个意思|意图执行|自由发挥)/.test(
    String(sourceText || '').replace(/\s+/g, ''),
  )
  const executionMode = hasModeInstruction ? explicitMode : existing.execution_mode
  if (scriptText === existing.content_text && executionMode === existing.execution_mode) {
    return { text: '模型理解出的新内容和当前正式口播样稿一致，没有生成新版本。' }
  }
  return {
    text: '我已经按你的原话理解成口播样稿修改。请核对修改前后，确认后才生成正式新版本。',
    action: {
      type: 'confirm_live_script_reference_update',
      title: '确认修改口播样稿',
      summary: executionMode === 'verbatim'
        ? '确认后生成正式新版本，并按“100%原话”执行。'
        : '确认后生成正式新版本，并按“意图参考”执行。',
      risk_level: 'low',
      requires_confirmation: true,
      payload: {
        plan_id: planId,
        room_id: roomId,
        script_reference_id: existing.id,
        script_reference_key: existing.reference_key,
        script_title: existing.title,
        current_script_text: existing.content_text,
        script_text: scriptText,
        script_goal: existing.goal || '',
        script_transition: existing.transition || '',
        execution_mode: executionMode,
        current_version_no: existing.version_no,
        source_text: sourceText,
      },
    } as SystemAgentActionPreview,
  }
}

async function resolveIntentTargetPlan(roomId: number, result: LiveStrategyIntentResponse) {
  const targetID = Number(result.target?.plan_id || 0)
  const targetName = intentString(result.target?.plan_name)
  const [allResult, boundResult] = await Promise.all([
    getLiveAgentPlans(),
    getRoomLiveAgentPlans(roomId),
  ])
  const allPlans = allResult.items || []
  let target = targetID ? allPlans.find((item) => item.id === targetID) : undefined
  if (!target && targetName) {
    const exact = allPlans.filter((item) => item.name.trim().toLowerCase() === targetName.toLowerCase())
    if (exact.length === 1) target = exact[0]
  }
  if (!target && targetName) {
    const fuzzy = allPlans.filter((item) => item.name.includes(targetName) || targetName.includes(item.name))
    if (fuzzy.length === 1) target = fuzzy[0]
  }
  return {
    target,
    boundIDs: new Set((boundResult.items || []).map((item) => item.id)),
  }
}

async function prepareLivePlanIntentAction(
  currentPlanId: number,
  roomId: number,
  sourceText: string,
  result: LiveStrategyIntentResponse,
) {
  const resolved = await resolveIntentTargetPlan(roomId, result)
  const target = resolved.target
  if (!target) {
    return { text: result.reply || '我知道你要操作直播方案，但还不能唯一找到目标方案。请直接说方案名称。' }
  }
  if (target.status !== 'active') {
    return { text: '“' + target.name + '”当前不是可用状态，不能绑定或切换。' }
  }
  if (result.intent === 'plan.bind') {
    if (resolved.boundIDs.has(target.id)) {
      return { text: '“' + target.name + '”已经绑定到当前直播间，不需要重复绑定。' }
    }
    return {
      text: '我已经定位到方案“' + target.name + '”。绑定只表示当前房间可以使用，不会自动切换运行方案。请确认。',
      action: {
        type: 'confirm_live_plan_bind',
        title: '确认绑定方案',
        summary: '确认后加入当前直播间可用方案列表；不会自动切换当前运行方案。',
        risk_level: 'low',
        requires_confirmation: true,
        payload: {
          room_id: roomId,
          current_plan_id: currentPlanId || 0,
          target_plan_id: target.id,
          target_plan_name: target.name,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }
  if (result.intent === 'plan.unbind') {
    if (!resolved.boundIDs.has(target.id)) {
      return { text: '“' + target.name + '”当前没有绑定到这个直播间。' }
    }
    return {
      text: '我已经定位到已绑定方案“' + target.name + '”。解绑会移出当前直播间可用方案，请确认。',
      action: {
        type: 'confirm_live_plan_unbind',
        title: '确认解绑方案',
        summary: target.id === currentPlanId
          ? '这是当前选中的方案。解绑可能使当前方案失效，请确认后执行。'
          : '确认后从当前直播间解绑；不会删除方案本身。',
        risk_level: 'medium',
        requires_confirmation: true,
        payload: {
          room_id: roomId,
          current_plan_id: currentPlanId || 0,
          target_plan_id: target.id,
          target_plan_name: target.name,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }
  if (result.intent === 'plan.switch') {
    if (!resolved.boundIDs.has(target.id)) {
      return { text: '“' + target.name + '”还没有绑定到当前直播间。我不会把“切换”偷偷变成“绑定”；请先说“绑定' + target.name + '”。' }
    }
    if (target.id === currentPlanId) {
      return { text: '当前直播间已经在使用“' + target.name + '”，不需要重复切换。' }
    }
    return {
      text: '我已经定位到已绑定方案“' + target.name + '”。确认后会热切换当前直播运行方案。',
      action: {
        type: 'confirm_live_plan_switch',
        title: '确认切换运行方案',
        summary: '其它已绑定方案继续保留；确认后当前直播间只把运行方案切换到这个方案。',
        risk_level: 'medium',
        requires_confirmation: true,
        payload: {
          room_id: roomId,
          current_plan_id: currentPlanId || 0,
          target_plan_id: target.id,
          target_plan_name: target.name,
          source_text: sourceText,
        },
      } as SystemAgentActionPreview,
    }
  }
  return null
}

async function handleUnifiedLiveStrategyIntent(
  domain: AgentDomain,
  roomId: number,
  planId: number,
  sourceText: string,
  result: LiveStrategyIntentResponse,
) {
  if (result.kind === 'chat') {
    pushAgentMessage(domain, result.reply || '我在，继续说。')
    return true
  }
  if (result.kind === 'clarify') {
    pushAgentMessage(domain, result.reply || '我还不能唯一确定你的意思，请再补充一点。')
    return true
  }
  if (result.intent.startsWith('product.')) {
    if (!planId) {
      pushAgentMessage(domain, '我已经理解成商品链接操作，但当前还没有选中的直播智能体方案。请先选择方案。')
      return true
    }
    const prepared = await prepareLiveProductIntentAction(planId, roomId, sourceText, result)
    if (prepared) {
      focusLivePlanModule(planId, 'products')
      pushAgentMessage(domain, prepared.text, prepared.action)
      return true
    }
  }
  if (result.intent.startsWith('benefit.')) {
    if (!planId) {
      pushAgentMessage(domain, '我已经理解成活动福利操作，但当前还没有选中的直播智能体方案。请先选择方案。')
      return true
    }
    const prepared = await prepareLiveBenefitIntentAction(planId, roomId, sourceText, result)
    if (prepared) {
      focusLivePlanModule(planId, 'benefits')
      pushAgentMessage(domain, prepared.text, prepared.action)
      return true
    }
  }
  if (result.intent.startsWith('fact.')) {
    if (!planId) {
      pushAgentMessage(domain, '我已经理解成事实依据操作，但当前还没有选中的直播智能体方案。请先选择方案。')
      return true
    }
    const prepared = await prepareLiveFactIntentAction(planId, roomId, sourceText, result)
    if (prepared) {
      focusLivePlanModule(planId, 'knowledge')
      pushAgentMessage(domain, prepared.text, prepared.action)
      return true
    }
  }
  if (result.intent.startsWith('script.')) {
    if (!planId) {
      pushAgentMessage(domain, '我已经理解成口播样稿操作，但当前还没有选中的直播智能体方案。请先选择方案。')
      return true
    }
    const prepared = await prepareLiveScriptIntentAction(planId, roomId, sourceText, result)
    if (prepared) {
      focusLivePlanModule(planId, 'rhythm')
      pushAgentMessage(domain, prepared.text, prepared.action)
      return true
    }
  }
  if (result.intent.startsWith('plan.')) {
    const prepared = await prepareLivePlanIntentAction(planId, roomId, sourceText, result)
    if (prepared) {
      if (planId) focusLivePlanModule(planId, 'plan')
      pushAgentMessage(domain, prepared.text, prepared.action)
      return true
    }
  }
  return false
}

async function executeLiveProductLinkCommand(
  planId: number,
  value: string,
  sourceQuotes: string[] = [value],
  sourceRef = 'system-agent:product-links',
) {
  const linkKey = liveProductLinkKeyFromText(value)
  const action = liveProductLinkActionFromText(value)
  if (!action) return null
  if (!linkKey) {
    return '我知道你要维护商品链接，但还缺少链接编号。请直接说“添加2号链接 黑菜籽油”或“删除2号链接”。'
  }

  const current = await getLiveAgentPlanProductLinks(planId)
  const existing = (current.items || []).find((item) => item.link_key === linkKey)
  if (action === 'delete') {
    if (!existing) return '当前方案里没有“' + linkKey + '”，所以没有执行删除。'
    await deleteLiveAgentPlanProductLink(planId, existing.id)
    notifyLivePlanModuleUpdated(planId, 'products')
    return '已从当前方案停用“' + linkKey + '（' + (existing.product_name || '未命名商品') + '）”。历史版本仍保留，可审计和恢复。'
  }

  const productName = liveProductNameFromCommand(value)
  const spec = liveProductField(value, ['规格']) || liveProductImplicitSpecFromCommand(value)
  const dailyPrice = liveProductField(value, ['日常价', '原价'])
  const quantity = liveProductField(value, ['数量'])
  const audience = liveProductField(value, ['适用人群', '适用对象', '适用'])

  if (action === 'add') {
    if (existing) {
      return '当前方案已经有“' + linkKey + '”：' + (existing.product_name || '未命名商品') + '。如果要改变它，请说“修改' + linkKey + ' …”。'
    }
    if (!productName) {
      return '“' + linkKey + '”已经识别到了，但还缺商品名称。请例如说：“添加' + linkKey + ' 黑菜籽油”。'
    }
    const result = await adoptLiveAgentPlanProductLinks(
      planId,
      [{
        link_key: linkKey,
        product_name: productName,
        spec,
        daily_price: dailyPrice,
        quantity,
        audience,
        review_bucket: 'adoptable',
        source_quotes: sourceQuotes,
      }],
      undefined,
      sourceRef,
    )
    if (!result.adopted) {
      const first = result.results?.[0]
      return first?.message || '商品链接没有写入，请检查是否与当前方案已有数据冲突。'
    }
    notifyLivePlanModuleUpdated(planId, 'products')
    const saved = result.results?.find((item) => item.saved)?.saved
    return '已把“' + linkKey + ' · ' + productName + '”直接写入当前直播智能体方案的正式商品链接' +
      (saved?.version_no ? ' V' + saved.version_no : '') +
      '。这是正式数据，不需要再发布用户层草稿。'
  }

  if (!existing) {
    return '当前方案里没有“' + linkKey + '”。如果这是新商品，请改说“添加' + linkKey + ' 商品名”。'
  }
  return '修改“' + linkKey + '”需要先核对具体字段和新值，本次没有写入数据库。'
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
  if (!['anchor', 'voice'].includes(liveStrategyMode.value)) return null
  const versions = await getLiveAgentConfigVersions()
  const latest =
    versions.find((item) => item.lifecycle_status === 'draft') ||
    versions.find((item) => item.lifecycle_status === 'active') ||
    versions[0]
  const now = new Date().toISOString()
  const payload: {
    style_profile?: Record<string, unknown>
    speech_config?: Record<string, unknown>
  } = {}

  if (liveStrategyMode.value === 'anchor') {
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
  const detail = (
    event as CustomEvent<{
      room_id?: number
      mode?: string
      module_label?: string
      plan_id?: number
    }>
  ).detail
  if (detail?.room_id && detail.room_id > 0) {
    window.localStorage.setItem('system-agent-live-room-id', String(detail.room_id))
  }
  const mode = detail?.mode
  const supportedModes: LiveStrategyMode[] = [
    'basic',
    'strategy',
    'script',
    'products',
    'benefits',
    'knowledge',
    'rhythm',
    'memory',
    'anchor',
    'voice',
    'fullshow',
    'plan',
  ]
  liveStrategyMode.value = supportedModes.includes(mode as LiveStrategyMode)
    ? (mode as LiveStrategyMode)
    : 'strategy'
  liveStrategyPlanId.value = Number(detail?.plan_id || 0)
  liveStrategyModuleLabel.value = String(detail?.module_label || '')
  window.localStorage.setItem('system-agent-live-mode', liveStrategyMode.value)
  window.localStorage.setItem('system-agent-live-plan-id', String(liveStrategyPlanId.value || 0))
  window.localStorage.setItem('system-agent-live-module-label', liveStrategyModuleLabel.value)
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
    agentImages.value = []
    imagePreview.value = null
    imageAttachmentError.value = ''
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
  () => visibleMessages.value.length,
  (length, previousLength) => {
    const shouldFollow = chatNearBottom.value
    void nextTick(() => {
      if (length > previousLength && shouldFollow) {
        void scrollChatToBottom()
        return
      }
      updateChatScrollState()
    })
  },
)

watch(
  () => busy.value && busyDomain.value === currentDomain.value,
  (thinking) => {
    const shouldFollow = chatNearBottom.value
    void nextTick(() => {
      if (thinking && shouldFollow) {
        void scrollChatToBottom()
        return
      }
      updateChatScrollState()
    })
  },
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
  const userID = Number(actor.value?.user_id || 0)
  if (drawerPreferenceReady.value && userID) {
    persistAgentDrawerPreferenceLocal(userID)
    void updateUserUIPreferences({ agent_drawer_collapsed: !open }).catch(() => undefined)
  }
  void nextTick(() => window.dispatchEvent(new CustomEvent('edge-handle-layout-changed')))
  if (!open || previousOpen) return
  void scrollChatToBottom()
})

watch(
  () => Number(actor.value?.user_id || 0),
  userID => {
    if (!userID) return
    void loadAgentDrawerPreference(userID)
  },
  { immediate: true },
)

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
    '操作审计',
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

async function prepareLiveRoomSpeechChoice(rawValue: string) {
  const roomId = Number(route.params.id)
  const question = extractLiveRoomExecutionContent(rawValue)
  if (!roomId || !question) return

  liveRoomWorkMode.value = 'execution'
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = '正在审核并生成可播话术…'
  messages.value.push({
    role: 'user',
    domain: 'live-room',
    text: rawValue.trim(),
    conversationScope: conversationScopeForDomain('live-room'),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  drawerOpen.value = true
  expanded.value = true
  busy.value = true
  busyDomain.value = 'live-room'
  void scrollChatToBottom()

  try {
    const enqueued = await enqueueRoomManualAgentDecision(roomId, {
      question,
      title: '智能体上行播报预审核',
      summary: '先由 Core Agent 审核输入并在必要时优化成可播文字，等待用户选择抢答或回答；本步骤不播音',
      reply_hint: '保留用户原意；原文字已经自然、安全、事实明确时不要为了改写而改写，确有必要时再优化。',
      manual_action: 'answer',
      manual_origin: 'agent_input_preview',
      execution_mode: 'intent',
      ttl_seconds: 120,
    })
    const decisionId = enqueued.item?.id
    if (!decisionId) throw new Error('没有生成可播任务，请确认直播间 AI 已启动')

    let reply = ''
    for (let attempt = 0; attempt < 90; attempt += 1) {
      await new Promise((resolve) => window.setTimeout(resolve, 500))
      const snapshot = await getRoomAgentDecisions(roomId)
      const matched = (snapshot.simulation_results || []).find((item) => item.decision_id === decisionId)
      if (matched?.reply?.trim()) {
        reply = matched.reply.trim()
        break
      }
    }
    if (!reply) throw new Error('审核生成超时，请确认直播间 AI 正在工作')

    messages.value.push({
      role: 'agent',
      domain: 'live-room',
      text: '已完成播出前审核。下面是最终建议播出的文字，选择“抢答”或“回答”后才会真正送入 TTS。',
      conversationScope: conversationScopeForDomain('live-room'),
      speechChoice: {
        question,
        text: reply,
        status: 'pending',
      },
    })
    liveRoomExecutionStatus.value = '审核完成 · 请选择抢答或回答'
  } catch (error) {
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '审核生成失败'
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '这次可播话术没有生成成功：' + error.message : '这次可播话术没有生成成功。',
    )
  } finally {
    busy.value = false
    busyDomain.value = null
    void nextTick(focusActiveComposer)
    void scrollChatToBottom()
  }
}

async function executePreparedLiveRoomSpeech(
  message: ChatMessage,
  mode: LiveRoomAnswerMode,
) {
  const roomId = Number(route.params.id)
  const choice = message.speechChoice
  if (!roomId || !choice || choice.status === 'sending' || choice.status === 'sent') return

  choice.status = 'sending'
  choice.selected = mode
  liveRoomExecutionError.value = false
  liveRoomExecutionStatus.value = mode === 'quick' ? '正在提交抢答…' : '正在提交回答…'
  try {
    const result = await enqueueRoomManualAgentDecision(roomId, {
      question: choice.question || choice.text,
      title: mode === 'quick' ? '智能体上行抢答' : '智能体上行回答',
      summary: mode === 'quick'
        ? '用户确认抢答，使用已审核文字立即进入现有打断播报链路'
        : '用户确认回答，使用已审核文字进入现有安全切点播报链路',
      reply_hint: choice.text,
      force_reopen: mode === 'quick',
      manual_action: mode,
      manual_origin: 'agent_input',
      execution_mode: 'verbatim',
      fixed_text: choice.text,
      ttl_seconds: mode === 'quick' ? 180 : 600,
    })
    choice.status = 'sent'
    const queueText = result.merged
      ? '已融合到现有待执行任务'
      : mode === 'quick'
        ? '已进入最高优先执行区'
        : '已进入待打断队列'
    liveRoomExecutionStatus.value = (mode === 'quick' ? '抢答' : '回答') + ' · ' + queueText
    pushAgentMessage('live-room', (mode === 'quick' ? '抢答' : '回答') + '已提交，后续继续沿用现有 TTS、打断和回归逻辑。')
  } catch (error) {
    choice.status = 'pending'
    choice.selected = undefined
    liveRoomExecutionError.value = true
    liveRoomExecutionStatus.value = error instanceof Error ? error.message : '播报提交失败'
    pushAgentMessage(
      'live-room',
      error instanceof Error ? '播报提交失败：' + error.message : '播报提交失败，请稍后再试。',
    )
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

  let intent: LiveRoomIntent = 'chat'
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
    intent = routed.intent === 'chat' && isLikelyLiveRoomExecutionIntent(rawValue)
      ? 'execution'
      : routed.intent
  } catch {
    // 分流服务异常时宁可聊天，也不能把普通闲聊误写成长期记忆。
    intent = isLikelyLiveRoomExecutionIntent(rawValue) ? 'execution' : 'chat'
  }

  if (intent === 'test') {
    await sendAgentLearningPreviewTest(rawValue)
    return true
  }
  if (intent === 'adopt') {
    await adoptLatestCoachingCandidate()
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
    await prepareLiveRoomSpeechChoice(rawValue)
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

function isLikelyLiveRoomExecutionIntent(value: string) {
  const text = value.trim()
  if (!text || text.startsWith('/')) return false
  return /(?:帮我|替我|让(?:直播间|主播|智能体)?|直接(?:打断)?)(?:说|播|念|读)|(?:直播间|主播|智能体).{0,4}(?:说|播|念|读)(?:一句|一下|下)?|^(?:直接(?:打断)?)?(?:说|播|念|读)(?:一句|一下|下)?/.test(text)
}

function extractLiveRoomExecutionContent(value: string) {
  const text = value.trim()
  const direct = text.match(/^(?:(?:你)?(?:帮我|替我)?(?:直接(?:打断)?)?(?:回答|回复|抢答|说|播|念|读)(?:一句|一下|下)?(?:这条(?:弹幕|问题)?|这个(?:问题|用户)?|他|她)?|(?:让)?(?:直播间|主播|智能体)(?:直接(?:打断)?)?(?:说|播|念|读)(?:一句|一下|下)?|给(?:这个用户|他|她)回复)[：:，,\s]*(.+)$/)
  return direct?.[1]?.trim() || text
}

async function routeInactiveLiveRoomWorkModeInput(rawValue: string) {
  const roomId = Number(route.params.id)
  if (!roomId) {
    liveRoomWorkMode.value = 'chat'
    return false
  }

  let intent: LiveRoomIntent = 'chat'
  try {
    const routed = await classifyAgentLearningMessage(roomId, {
      message: rawValue,
      current_mode: liveRoomWorkMode.value,
      learning_active: false,
      test_active: liveRoomTestMode.value,
      execution_active: Boolean(liveRoomAnswerMode.value),
      history: historyPayload('live-room').slice(-10),
    })
    intent = routed.intent === 'chat' && isLikelyLiveRoomExecutionIntent(rawValue)
      ? 'execution'
      : routed.intent
  } catch {
    intent = isLikelyAgentCorrectionIntent(rawValue)
      ? 'learning'
      : isLikelyLiveRoomExecutionIntent(rawValue)
        ? 'execution'
        : 'chat'
  }

  if (intent === 'learning') {
    beginCoachingMode('当前对话纠正')
    await sendGeneralCoaching(rawValue)
    return true
  }
  if (intent === 'adopt') {
    await adoptLatestCoachingCandidate()
    return true
  }
  if (intent === 'execution') {
    await prepareLiveRoomSpeechChoice(rawValue)
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
  if (domain === 'live-strategy') {
    if (isLiveStrategyCancelIntent(rawValue)) {
      const pendingAction = latestPendingLiveStrategyAction()
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      if (pendingAction) pendingAction.action = undefined
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      pushAgentMessage(
        domain,
        pendingAction
          ? '已取消，本次操作没有写入数据库。'
          : '好，已取消。当前没有执行任何修改。',
      )
      return
    }

    const pendingConfirmedAction = latestPendingLiveStrategyAction()
    if (
      pendingConfirmedAction?.action &&
      isLiveStrategyConfirmIntent(rawValue) &&
      !pendingConfirmedAction.action.type.startsWith('clarify_')
    ) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      await executeAction(pendingConfirmedAction)
      return
    }

    const pendingCorrection = latestPendingLiveProductCorrection()
    if (pendingCorrection && isLiveProductCorrectionAccept(rawValue)) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      await executeAction(pendingCorrection)
      return
    }
    if (pendingCorrection && isLiveProductCorrectionCancel(rawValue)) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      pendingCorrection.action = undefined
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      pushAgentMessage(domain, '已取消这次文字纠正，没有写入数据库。你可以重新告诉我要怎么改。')
      return
    }

    const pendingImageProduct = latestPendingLiveImageProduct()
    if (pendingImageProduct && isLiveImageProductAccept(rawValue)) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      await executeAction(pendingImageProduct)
      return
    }
    if (pendingImageProduct && isLiveImageProductCancel(rawValue)) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      pendingImageProduct.action = undefined
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      pushAgentMessage(domain, '已取消这次图片识别结果，没有写入当前方案。你可以继续引用图片重新说明。')
      return
    }

    const pendingBenefit = latestPendingLiveBenefit()
    if (pendingBenefit && isLiveBenefitAccept(rawValue)) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      await executeAction(pendingBenefit)
      return
    }
    if (pendingBenefit && isLiveBenefitCancel(rawValue)) {
      messages.value.push({
        role: 'user',
        domain,
        text: rawValue,
        conversationScope: conversationScopeForDomain(domain),
      })
      pendingBenefit.action = undefined
      input.value = ''
      dismissedSuggestionInput.value = ''
      drawerOpen.value = true
      pushAgentMessage(domain, '已取消这次活动福利候选，没有写入数据库。你可以重新告诉我要怎么设置活动。')
      return
    }
  }
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
  const imageResolution = resolveAgentImageReferences(value)
  if (imageResolution.error) {
    pushAgentMessage(domain, imageResolution.error)
    return
  }
  const selectedImages = imageResolution.images
  const imageURLs = selectedImages.map((item) => item.dataUrl)
  const modelValue = agentMessageWithImageContext(value, selectedImages)
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
  const liveStrategyModuleOwnsCommand =
    domain === 'live-strategy' &&
    liveStrategyMode.value !== 'strategy' &&
    liveStrategyMode.value !== 'basic'
  const routeToSystemAgent = liveStrategyModuleOwnsCommand
    ? false
    : shouldRouteToSystemAgent(domain, {
        hasSystemTask: Boolean(systemTask),
        systemCapabilityIntent: isSystemCapabilityIntent(value),
        clientBoundaryIntent: isClientBoundaryIntent(value),
      })
  messages.value.push({
    role: 'user',
    domain,
    text: selectedImages.length
      ? value + '\n' + selectedImages.map((item) => '[' + item.label + ']').join(' ')
      : value,
    conversationScope: conversationScopeForDomain(domain),
  })
  input.value = ''
  dismissedSuggestionInput.value = ''
  activeComposer.value = null
  drawerOpen.value = true

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
        message: modelValue,
        history: policyHistory,
        image_urls: imageURLs,
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
        message: modelValue,
        history: systemHistory,
        current_path: route.fullPath,
        image_urls: imageURLs,
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
      if (response.state === 'permission_denied') {
        if (activeSystemTask.value) {
          activeSystemTask.value = null
          systemTaskHistory.value = []
        }
        pushAgentMessage(domain, response.reply)
        return
      }
      if (response.navigate) {
        if (activeSystemTask.value) {
          activeSystemTask.value = null
          systemTaskHistory.value = []
        }
        await router.push(response.navigate.to)
        pushAgentMessage(currentDomain.value, response.reply)
        return
      }
      pushAgentMessage(
        domain,
        response.reply,
        response.state === 'ready_to_confirm' ? response.action : undefined,
      )
      return
    }

    if (domain === 'live-room') {
      const roomId = Number(route.params.id)
      if (!roomId) {
        pushAgentMessage(domain, '当前页面没有有效直播间编号，暂时不能进入场控上下文。')
        return
      }
      const response = await chatLiveAgent(roomId, {
        message: modelValue,
        history,
        image_urls: imageURLs,
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

      if (selectedImages.length) {
        const unifiedPlanId = liveStrategyPlanId.value
        if (!unifiedPlanId) {
          pushAgentMessage(domain, '你已经引用了图片，但当前还没有选中的直播智能体方案。请先选择方案后再添加商品链接。')
          return
        }
        const recognizedImages = await recognizeLiveStrategyImages(
          unifiedPlanId,
          roomId,
          selectedImages,
        )
        pushAgentMessage(
          domain,
          liveStrategyRecognizedTextPreview(recognizedImages) +
            '\n\n我先把图片中识别到的原始文字给你看，下面再根据这些内容判断能否形成商品链接资料。',
        )
        const recognizedMessage = liveStrategyMessageWithRecognizedImages(value, recognizedImages)
        const interpreted = await interpretLiveStrategyIntent(roomId, {
          message: recognizedMessage,
          plan_id: unifiedPlanId,
          current_mode: liveStrategyMode.value,
          history,
        })
        const handledByUnifiedIntent = await handleUnifiedLiveStrategyIntent(
          domain,
          roomId,
          unifiedPlanId,
          value,
          interpreted,
        )
        if (handledByUnifiedIntent) return

        const response = await chatLiveAgent(roomId, {
          message: recognizedMessage,
          history,
        })
        const planId = unifiedPlanId
        const candidate = liveImageProductCandidate(value, response.reply)
        const clarification = !candidate && planId
          ? buildLiveStrategyIntentClarification(
              value,
              roomId,
              planId,
              liveStrategyMode.value,
              selectedImages.map((item) => item.label),
            )
          : undefined
        pushAgentMessage(
          domain,
          candidate
            ? imageRecognitionReplyForAdd(response.reply)
            : response.reply + (clarification ? '\n\n我还不能确定你想把这张图用于哪个功能，请从下面选一个。' : ''),
          candidate
            ? {
                type: 'add_live_image_product',
                title: '添加' + candidate.linkKey + '商品',
                summary: '图片识别结果尚未写入。点击“添加”后写入当前直播智能体方案，并生成可追溯版本。',
                risk_level: 'low',
                requires_confirmation: true,
                payload: {
                  plan_id: planId,
                  room_id: roomId,
                  link_key: candidate.linkKey,
                  product_name: candidate.productName,
                  spec: candidate.spec,
                  daily_price: candidate.dailyPrice,
                  quantity: candidate.quantity,
                  audience: candidate.audience,
                  source_text: candidate.sourceText,
                },
              }
            : clarification,
        )
        return
      }

      const unifiedPlanId = liveStrategyPlanId.value
      const interpreted = await interpretLiveStrategyIntent(roomId, {
        message: value,
        plan_id: unifiedPlanId || undefined,
        current_mode: liveStrategyMode.value,
        history,
      })
      const handledByUnifiedIntent = await handleUnifiedLiveStrategyIntent(
        domain,
        roomId,
        unifiedPlanId,
        value,
        interpreted,
      )
      if (handledByUnifiedIntent) return

      const typoCorrectedProductCommand = liveProductLinkTypoCorrection(value)
      if (typoCorrectedProductCommand) {
        const planId = liveStrategyPlanId.value
        if (!planId) {
          pushAgentMessage(domain, '我发现“连接”很可能是“链接”的错别字，但当前还没有选中的直播智能体方案。请先选择方案。')
          return
        }
        pushAgentMessage(
          domain,
          '我发现你这里的“连接”很可能是“链接”。我不会直接改写并入库，请你确认后再执行。',
          {
            type: 'confirm_live_product_link_correction',
            title: '确认文字纠正',
            summary: '把“连接”纠正为“链接”，确认后再写入当前直播智能体方案。',
            risk_level: 'low',
            requires_confirmation: true,
            payload: {
              plan_id: planId,
              room_id: roomId,
              original_command: value,
              corrected_command: typoCorrectedProductCommand,
            },
          },
        )
        return
      }

      const planIdForClarification = liveStrategyPlanId.value

      if (isExplicitLiveBenefitCommand(value) && isLiveBenefitUpdateIntent(value)) {
        if (!planIdForClarification) {
          pushAgentMessage(domain, '我识别到你要修改活动福利，但当前还没有选中的直播智能体方案。请先选择方案。')
          return
        }
        await pushLiveBenefitUpdatePreview(domain, planIdForClarification, roomId, value)
        return
      }

      const intentClarification = buildLiveStrategyIntentClarification(
        value,
        roomId,
        planIdForClarification,
        liveStrategyMode.value,
      )
      if (intentClarification) {
        pushAgentMessage(
          domain,
          '这句话可能对应不止一个功能，我先不执行。请选择你真正想做的事情：',
          intentClarification,
        )
        return
      }

      const explicitProductAction = liveStrategyMode.value === 'benefits'
        ? ''
        : liveProductLinkActionFromText(value)
      if (explicitProductAction) {
        const planId = liveStrategyPlanId.value
        if (!planId) {
          pushAgentMessage(domain, '我识别到你要维护商品链接，但当前还没有选中的直播智能体方案。请先选择方案，再继续这条指令。')
          return
        }
        if (explicitProductAction === 'update') {
          await pushLiveProductUpdatePreview(domain, planId, roomId, value)
          return
        }
        const productMessage = await executeLiveProductLinkCommand(planId, value)
        focusLivePlanModule(planId, 'products')
        pushAgentMessage(domain, productMessage || '已切换到商品链接模块，请继续告诉我要怎么调整。')
        return
      }

      if (
        (liveStrategyMode.value === 'products' || liveStrategyMode.value === 'benefits') &&
        !shouldRouteLiveStrategyModuleCommand(value, liveStrategyMode.value)
      ) {
        const response = await chatLiveAgent(roomId, {
          message: modelValue,
          history,
          image_urls: imageURLs,
        })
        pushAgentMessage(domain, response.reply)
        return
      }

      if (liveStrategyMode.value === 'products') {
        const planId = liveStrategyPlanId.value
        if (!planId) {
          pushAgentMessage(domain, '当前还没有选中的直播智能体方案，请先选择方案后再维护商品链接。')
          return
        }
        if (liveProductLinkActionFromText(value) === 'update') {
          await pushLiveProductUpdatePreview(domain, planId, roomId, value)
          return
        }
        const productMessage = await executeLiveProductLinkCommand(planId, value)
        if (productMessage) {
          pushAgentMessage(domain, productMessage)
          return
        }
        pushAgentMessage(
          domain,
          '你现在在“商品链接”模块。可以直接对我说：\n' +
            '“添加2号链接 黑菜籽油”\n' +
            '“修改2号链接 商品名：黑菜籽油 规格：5L”\n' +
            '“删除2号链接”',
        )
        return
      }
      if (liveStrategyMode.value === 'benefits') {
        const planId = liveStrategyPlanId.value
        if (!planId) {
          pushAgentMessage(domain, '当前还没有选中的直播智能体方案，请先选择方案后再新增活动福利。')
          return
        }

        if (isLiveBenefitUpdateIntent(value)) {
          if (isExplicitPendingBenefitUpdateIntent(value) && updatePendingBenefitCandidateFromText(value)) {
            pushAgentMessage(
              domain,
              '已修改上方活动福利候选。当前仍未写入数据库，请继续核对；确认无误后再点“添加活动福利”。',
            )
            return
          }

          const formalBenefits = await getLiveAgentPlanBenefits(planId)
          const formalItems = (formalBenefits.items || []).filter((item) => item.status !== 'disabled')
          const updateLinkKey = liveProductLinkKeyFromText(value)
          const hasMatchingFormal = updateLinkKey
            ? formalItems.some((item) => item.link_key === updateLinkKey)
            : formalItems.length > 0

          if (hasMatchingFormal) {
            await pushLiveBenefitUpdatePreview(domain, planId, roomId, value)
            return
          }

          if (updatePendingBenefitCandidateFromText(value)) {
            pushAgentMessage(
              domain,
              '当前还没有对应的正式活动，我已修改未保存的活动福利候选。确认无误后再点“添加活动福利”。',
            )
            return
          }

          await pushLiveBenefitUpdatePreview(domain, planId, roomId, value)
          return
        }

        const linkKey = liveProductLinkKeyFromText(value)
        const currentLinks = await getLiveAgentPlanProductLinks(planId)
        const linkedProduct = linkKey
          ? (currentLinks.items || []).find((item) => item.link_key === linkKey)
          : undefined
        let candidate = liveBenefitCandidateFromText(
          value,
          '',
          linkedProduct?.product_name || '',
        )
        let candidateText = candidate ? benefitCandidateSummary(candidate) : ''

        if (!candidate) {
          const extractionPrompt =
            modelValue +
            '\n\n【当前模块：活动福利候选整理】\n' +
            '这一步只整理候选，不执行保存、发布或生效，也不要说“已记录/已更新/已保存”。\n' +
            '请按以下固定字段输出；不知道就写“未设置”：\n' +
            '链接：\n商品名称：\n活动价：\n赠品：\n活动内容：\n开始时间：YYYY-MM-DD HH:mm\n结束时间：YYYY-MM-DD HH:mm'
          const response = await chatLiveAgent(roomId, {
            message: extractionPrompt,
            history,
            image_urls: imageURLs,
          })
          candidate = liveBenefitCandidateFromText(
            value,
            response.reply,
            linkedProduct?.product_name || '',
          )
          candidateText = response.reply
        }

        if (!candidate) {
          pushAgentMessage(
            domain,
            candidateText || '我知道你在维护“活动福利”，但目前还无法整理出明确的活动价、赠品或活动内容。请例如说：“1号链接买一桶送5升菜籽油，活动到今晚23:00结束”。',
          )
          return
        }

        focusLivePlanModule(planId, 'benefits')
        pushAgentMessage(
          domain,
          benefitCandidateReplyForAdd(candidateText || benefitCandidateSummary(candidate), candidate),
          {
            type: 'add_live_benefit',
            title: '添加活动福利',
            summary: candidate.starts_at && candidate.ends_at
              ? '候选尚未写入。点击“添加活动福利”后写入当前直播智能体方案。'
              : '候选尚未写入，且缺少完整有效期。点击后会写入活动草稿，不会立即进入直播生成。',
            risk_level: 'low',
            requires_confirmation: true,
            payload: {
              plan_id: planId,
              room_id: roomId,
              benefit_key: candidate.key || '',
              link_key: candidate.link_key || '',
              product_name: candidate.product_name || '',
              activity_price: candidate.activity_price || '',
              gift: candidate.gift || '',
              activity: candidate.activity || '',
              starts_at: candidate.starts_at || '',
              ends_at: candidate.ends_at || '',
              review_bucket: candidate.review_bucket || 'discuss',
              review_reason: candidate.review_reason || '',
              source_text: value,
            },
          },
        )
        return
      }
      if (liveStrategyMode.value === 'strategy') {
        const response = await chatLiveRoomPolicyAgent(roomId, {
          message: modelValue,
          history,
          image_urls: imageURLs,
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
        message: modelValue,
        history,
        image_urls: imageURLs,
      })
      const modeName =
        liveStrategyMode.value === 'anchor'
          ? '主播训练'
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
          message: modelValue,
          history,
          image_urls: imageURLs,
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
        message: modelValue,
        history,
        current_path: route.fullPath,
        image_urls: imageURLs,
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
        message: modelValue,
        history,
        image_urls: imageURLs,
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
  }
}

async function chooseLiveStrategyIntent(
  message: ChatMessage,
  option: LiveStrategyIntentOption,
) {
  const action = message.action
  if (!action || action.type !== 'clarify_live_strategy_intent' || executing.value) return

  const planId = Number(action.payload.plan_id || 0)
  const roomId = Number(action.payload.room_id || 0)
  const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
  const originalMessage = String(action.payload.original_message || '').trim()
  if (!planId || !roomId || !originalMessage) {
    message.action = undefined
    pushAgentMessage(message.domain, '这条意图确认缺少上下文，请重新告诉我你想做什么。')
    return
  }
  if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
    message.action = undefined
    pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧的意图选择已失效。请在当前方案重新说一次。')
    return
  }

  const supportedModes: LiveStrategyMode[] = [
    'basic', 'strategy', 'script', 'products', 'benefits', 'knowledge', 'rhythm', 'memory',
    'anchor', 'voice', 'fullshow', 'plan',
  ]
  const selectedMode = supportedModes.includes(option.mode as LiveStrategyMode)
    ? (option.mode as LiveStrategyMode)
    : liveStrategyMode.value
  const imageLabels = Array.isArray(action.payload.image_labels) ? action.payload.image_labels : []
  const selectedImages = imageLabels
    .map((label) => currentAgentImages.value.find((item) => item.label === label))
    .filter((item): item is AgentImageAttachment => Boolean(item))
  if (imageLabels.length && selectedImages.length !== imageLabels.length) {
    message.action = undefined
    pushAgentMessage(message.domain, '这条选择引用的图片已经不在当前会话里了，请重新粘贴图片后再操作。')
    return
  }

  message.action = undefined
  messages.value.push({
    role: 'user',
    domain: 'live-strategy',
    text: '我选择：' + option.label,
    conversationScope: conversationScopeForDomain('live-strategy'),
  })
  liveStrategyMode.value = selectedMode
  window.localStorage.setItem('system-agent-live-mode', selectedMode)
  focusLivePlanModule(planId, selectedMode)

  executing.value = true
  try {
    const confirmedMessage =
      originalMessage +
      '\n\n【已确认意图】' + option.label +
      '\n请只按这个功能继续理解，不要执行其他模块的写入；需要写数据库时仍先让我确认。'
    const history = historyPayload('live-strategy')
    const recognizedImages = selectedImages.length
      ? await recognizeLiveStrategyImages(planId, roomId, selectedImages)
      : []
    const recognizedMessage = liveStrategyMessageWithRecognizedImages(
      confirmedMessage,
      recognizedImages,
    )

    if (option.id === 'inspect-only') {
      pushAgentMessage(
        message.domain,
        (liveStrategyRecognizedImageContext(recognizedImages) || '没有识别到可可靠读取的图片内容。') +
          '\n\n本次只识别和说明，没有写入当前方案。',
      )
      return
    }

    if (option.id === 'products' && selectedImages.length) {
      const interpreted = await interpretLiveStrategyIntent(roomId, {
        message: recognizedMessage,
        plan_id: planId,
        current_mode: 'products',
        history,
      })
      const handled = await handleUnifiedLiveStrategyIntent(
        message.domain,
        roomId,
        planId,
        originalMessage,
        interpreted,
      )
      if (handled) return
      pushAgentMessage(message.domain, '图片已经识别，但还没有形成可安全写入的商品资料。请补充商品名称或链接编号。')
      return
    }

    const response = await chatLiveAgent(roomId, {
      message: recognizedMessage,
      history,
    })
    pushAgentMessage(
      message.domain,
      response.reply + '\n\n已按“' + option.label + '”理解这句话；本次没有自动写入其它模块。',
    )
  } catch (error) {
    pushAgentMessage(
      message.domain,
      error instanceof Error ? '按你选择的意图继续处理失败：' + error.message : '继续处理失败，请重试。',
    )
  } finally {
    executing.value = false
  }
}

function chooseLiveProductUpdateField(
  message: ChatMessage,
  option: LiveStrategyIntentOption,
) {
  const action = message.action
  if (!action || action.type !== 'clarify_live_product_update') return
  const command = String(option.command || '').trim()
  if (!command) return
  message.action = undefined
  input.value = command
  drawerOpen.value = true
  activeComposer.value = 'drawer'
  void nextTick(() => {
    const element = drawerInputEl.value
    if (!element) return
    element.focus()
    const end = element.value.length
    element.setSelectionRange(end, end)
  })
}

function chooseLiveBenefitUpdateField(
  message: ChatMessage,
  option: LiveStrategyIntentOption,
) {
  const action = message.action
  if (!action || !['clarify_live_benefit_target', 'clarify_live_benefit_update'].includes(action.type)) return
  const command = String(option.command || '').trim()
  if (!command) return
  message.action = undefined
  input.value = command
  drawerOpen.value = true
  activeComposer.value = 'drawer'
  void nextTick(() => {
    const element = drawerInputEl.value
    if (!element) return
    element.focus()
    const end = element.value.length
    element.setSelectionRange(end, end)
  })
}

async function executeAction(message: ChatMessage) {
  const action = message.action
  if (!action || executing.value) return

  if (unifiedLiveStrategyActionTypes.has(action.type)) {
    const roomId = Number(action.payload.room_id || 0)
    const planId = Number(action.payload.plan_id || 0)
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (!roomId || currentRoomId !== roomId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间，这条旧确认已失效，没有执行。')
      return
    }
    if (planId && liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了正在编辑的直播方案，这条旧确认已失效，没有执行。')
      return
    }

    executing.value = true
    try {
      const response = await executeLiveStrategyAction(roomId, action)
      message.action = undefined
      const data = response.data && typeof response.data === 'object'
        ? response.data as Record<string, unknown>
        : {}
      const module = String(data.module || '')
      const responsePlanId = Number(
        data.plan_id ||
        action.payload.plan_id ||
        action.payload.current_plan_id ||
        action.payload.target_plan_id ||
        0,
      )
      const selectedPlanId = Number(data.selected_plan_id || 0)
      if (response.state === 'succeeded') {
        if (selectedPlanId > 0) {
          liveStrategyPlanId.value = selectedPlanId
          window.localStorage.setItem('system-agent-live-plan-id', String(selectedPlanId))
        }
        if (module === 'products' || module === 'benefits' || module === 'knowledge' || module === 'rhythm') {
          notifyLivePlanModuleUpdated(responsePlanId, module)
          focusLivePlanModule(responsePlanId, module)
        } else if (module === 'plan') {
          notifyLivePlanModuleUpdated(responsePlanId, 'plan')
          focusLivePlanModule(responsePlanId, 'plan')
        }
      }
      pushAgentMessage(message.domain, response.reply)
    } catch (error) {
      message.action = undefined
      pushAgentMessage(
        message.domain,
        error instanceof Error ? '确认动作没有执行：' + error.message : '确认动作没有执行，请重试。',
      )
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'add_live_fact') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const category = String(action.payload.fact_category || '').trim()
    const factKey = String(action.payload.fact_key || '').trim()
    const factValue = String(action.payload.fact_value || '').trim()
    if (!planId || !roomId || !category || !factKey || !factValue) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条事实候选缺少必要信息，请重新说明。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条事实候选已失效。')
      return
    }
    executing.value = true
    try {
      const current = await getLiveAgentPlanFacts(planId)
      if ((current.items || []).some((item) => item.category === category && item.key === factKey)) {
        throw new Error('确认期间这条事实已经存在。为了避免把“新增”悄悄变成“修改”，请重新发起。')
      }
      const candidate: LiveAgentPlanFactCandidate = {
        category,
        key: factKey,
        value: factValue,
        status: 'confirmed',
        review_bucket: 'adoptable',
        source_quote: String(action.payload.source_text || '').trim(),
      }
      const result = await adoptLiveAgentPlanFacts(
        planId,
        [candidate],
        undefined,
        'system-agent:intent-fact',
      )
      const first = result.results?.[0]
      if (!result.adopted) {
        throw new Error(first?.message || '事实依据没有写入，请检查是否存在冲突。')
      }
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'knowledge')
      focusLivePlanModule(planId, 'knowledge')
      pushAgentMessage(message.domain, '已确认添加事实“' + factKey + '：' + factValue + '”。')
    } catch (error) {
      pushAgentMessage(message.domain, error instanceof Error ? '事实没有写入：' + error.message : '事实没有写入，请重试。')
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_fact_update') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const factId = Number(action.payload.fact_id || 0)
    const expectedVersion = Number(action.payload.current_version_no || 0)
    const category = String(action.payload.fact_category || '').trim()
    const factKey = String(action.payload.fact_key || '').trim()
    const factValue = String(action.payload.fact_value || '').trim()
    if (!planId || !roomId || !factId || !category || !factKey || !factValue) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条事实修改确认缺少必要信息，请重新发起。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧事实修改确认已失效。')
      return
    }
    executing.value = true
    try {
      const current = await getLiveAgentPlanFacts(planId)
      const existing = (current.items || []).find((item) => item.id === factId && item.category === category && item.key === factKey)
      if (!existing) throw new Error('当前事实已经变化或被停用，请重新发起。')
      if (expectedVersion && existing.version_no !== expectedVersion) {
        throw new Error('这条事实已经产生新版本，请重新发起修改，避免覆盖新数据。')
      }
      const updated = await updateLiveAgentPlanFact(planId, factId, {
        category,
        key: factKey,
        value: factValue,
      })
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'knowledge')
      focusLivePlanModule(planId, 'knowledge')
      pushAgentMessage(message.domain, '已确认修改事实“' + factKey + '”，正式版本更新为 V' + updated.version_no + '。')
    } catch (error) {
      pushAgentMessage(message.domain, error instanceof Error ? '事实修改没有写入：' + error.message : '事实修改没有写入，请重试。')
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_fact_disable') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const factId = Number(action.payload.fact_id || 0)
    const expectedVersion = Number(action.payload.current_version_no || 0)
    const factKey = String(action.payload.fact_key || '').trim()
    if (!planId || !roomId || !factId || !factKey) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条事实停用确认缺少必要信息，请重新发起。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧事实停用确认已失效。')
      return
    }
    executing.value = true
    try {
      const current = await getLiveAgentPlanFacts(planId)
      const existing = (current.items || []).find((item) => item.id === factId)
      if (!existing) throw new Error('当前事实已经变化或被停用，请重新发起。')
      if (expectedVersion && existing.version_no !== expectedVersion) {
        throw new Error('这条事实已经产生新版本，请重新发起停用。')
      }
      await deleteLiveAgentPlanFact(planId, factId)
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'knowledge')
      focusLivePlanModule(planId, 'knowledge')
      pushAgentMessage(message.domain, '已确认停用事实“' + factKey + '”。历史版本和审计记录仍保留。')
    } catch (error) {
      pushAgentMessage(message.domain, error instanceof Error ? '事实停用没有执行：' + error.message : '事实停用没有执行，请重试。')
    } finally {
      executing.value = false
    }
    return
  }

  if (
    action.type === 'confirm_live_plan_bind' ||
    action.type === 'confirm_live_plan_unbind' ||
    action.type === 'confirm_live_plan_switch'
  ) {
    const roomId = Number(action.payload.room_id || 0)
    const targetPlanId = Number(action.payload.target_plan_id || 0)
    const targetPlanName = String(action.payload.target_plan_name || '').trim()
    if (!roomId || !targetPlanId || !targetPlanName) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条方案确认缺少目标方案信息，请重新发起。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间，这条旧方案确认已失效。')
      return
    }
    executing.value = true
    try {
      if (action.type === 'confirm_live_plan_bind') {
        const bound = await getRoomLiveAgentPlans(roomId)
        if ((bound.items || []).some((item) => item.id === targetPlanId)) {
          throw new Error('这个方案已经绑定到当前直播间。')
        }
        await bindRoomLiveAgentPlan(targetPlanId, roomId)
        message.action = undefined
        focusLivePlanModule(liveStrategyPlanId.value || targetPlanId, 'plan')
        pushAgentMessage(message.domain, '已把“' + targetPlanName + '”绑定到当前直播间。当前运行方案没有自动切换。')
      } else if (action.type === 'confirm_live_plan_unbind') {
        const bound = await getRoomLiveAgentPlans(roomId)
        if (!(bound.items || []).some((item) => item.id === targetPlanId)) {
          throw new Error('这个方案已经不在当前直播间的绑定列表里。')
        }
        await unbindRoomLiveAgentPlan(targetPlanId, roomId)
        message.action = undefined
        focusLivePlanModule(liveStrategyPlanId.value || targetPlanId, 'plan')
        pushAgentMessage(message.domain, '已从当前直播间解绑“' + targetPlanName + '”。方案本身没有删除。')
      } else {
        const bound = await getRoomLiveAgentPlans(roomId)
        if (!(bound.items || []).some((item) => item.id === targetPlanId)) {
          throw new Error('目标方案已经不再绑定到当前直播间，请先重新绑定。')
        }
        await setLiveRuntimePlan(roomId, targetPlanId)
        liveStrategyPlanId.value = targetPlanId
        window.localStorage.setItem('system-agent-live-plan-id', String(targetPlanId))
        message.action = undefined
        focusLivePlanModule(targetPlanId, 'plan')
        pushAgentMessage(message.domain, '已把当前直播间运行方案热切换到“' + targetPlanName + '”。其它已绑定方案继续保留。')
      }
    } catch (error) {
      pushAgentMessage(message.domain, error instanceof Error ? '方案操作没有执行：' + error.message : '方案操作没有执行，请重试。')
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_product_disable') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const productLinkId = Number(action.payload.product_link_id || 0)
    const expectedVersion = Number(action.payload.current_version_no || 0)
    const linkKey = String(action.payload.link_key || '').trim()
    if (!planId || !roomId || !productLinkId || !linkKey) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条停用确认缺少必要信息，请重新发起。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧停用确认已失效。')
      return
    }
    executing.value = true
    try {
      const current = await getLiveAgentPlanProductLinks(planId)
      const existing = (current.items || []).find((item) => item.id === productLinkId && item.link_key === linkKey)
      if (!existing) throw new Error('当前商品链接已经变化或已停用，请重新发起。')
      if (expectedVersion && existing.version_no !== expectedVersion) {
        throw new Error('这条商品链接已经产生新版本，请重新发起停用，避免操作旧版本。')
      }
      await deleteLiveAgentPlanProductLink(planId, productLinkId)
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'products')
      focusLivePlanModule(planId, 'products')
      pushAgentMessage(message.domain, '已确认停用“' + linkKey + '”。历史版本和审计记录仍保留。')
    } catch (error) {
      pushAgentMessage(message.domain, error instanceof Error ? '停用没有执行：' + error.message : '停用没有执行，请重试。')
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_benefit_disable') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const benefitId = Number(action.payload.benefit_id || 0)
    const expectedVersion = Number(action.payload.current_version_no || 0)
    const benefitKey = String(action.payload.benefit_key || '').trim()
    if (!planId || !roomId || !benefitId || !benefitKey) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条活动停用确认缺少必要信息，请重新发起。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧活动停用确认已失效。')
      return
    }
    executing.value = true
    try {
      const current = await getLiveAgentPlanBenefits(planId)
      const existing = (current.items || []).find((item) => item.id === benefitId && item.key === benefitKey)
      if (!existing) throw new Error('当前活动福利已经变化或已停用，请重新发起。')
      if (expectedVersion && existing.version_no !== expectedVersion) {
        throw new Error('这条活动福利已经产生新版本，请重新发起停用，避免操作旧版本。')
      }
      await deleteLiveAgentPlanBenefit(planId, benefitId)
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'benefits')
      focusLivePlanModule(planId, 'benefits')
      pushAgentMessage(message.domain, '已确认停用这条活动福利。它不会再进入直播生成，历史版本和审计记录仍保留。')
    } catch (error) {
      pushAgentMessage(message.domain, error instanceof Error ? '活动停用没有执行：' + error.message : '活动停用没有执行，请重试。')
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_product_update') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const productLinkId = Number(action.payload.product_link_id || 0)
    const expectedVersion = Number(action.payload.current_version_no || 0)
    const linkKey = String(action.payload.link_key || '').trim()
    if (!planId || !roomId || !productLinkId || !linkKey) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条修改确认缺少必要信息，请重新输入修改指令。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧修改确认已失效，没有写入数据库。')
      return
    }

    executing.value = true
    try {
      const current = await getLiveAgentPlanProductLinks(planId)
      const existing = (current.items || []).find((item) => item.id === productLinkId && item.link_key === linkKey)
      if (!existing) throw new Error('当前正式商品链接已经变化，请重新发起修改。')
      if (expectedVersion && existing.version_no !== expectedVersion) {
        throw new Error('这条链接已经产生了更新版本，请重新发起修改，避免覆盖新数据。')
      }

      const next = {
        link_key: linkKey,
        product_name: String(action.payload.product_name || ''),
        spec: String(action.payload.spec || ''),
        daily_price: String(action.payload.daily_price || ''),
        quantity: String(action.payload.quantity || ''),
        audience: String(action.payload.audience || ''),
      }
      const changed =
        next.product_name !== (existing.product_name || '') ||
        next.spec !== (existing.spec || '') ||
        next.daily_price !== (existing.daily_price || '') ||
        next.quantity !== (existing.quantity || '') ||
        next.audience !== (existing.audience || '')
      if (!changed) {
        message.action = undefined
        pushAgentMessage(message.domain, '新值和当前正式数据一致，没有生成新版本。')
        return
      }

      const updated = await updateLiveAgentPlanProductLink(planId, existing.id, next)
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'products')
      focusLivePlanModule(planId, 'products')
      pushAgentMessage(message.domain, '已确认修改“' + linkKey + '”，正式版本更新为 V' + updated.version_no + '。')
    } catch (error) {
      pushAgentMessage(
        message.domain,
        error instanceof Error ? '修改没有写入：' + error.message : '修改没有写入，请重试。',
      )
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_benefit_update') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const benefitId = Number(action.payload.benefit_id || 0)
    const expectedVersion = Number(action.payload.current_version_no || 0)
    const benefitKey = String(action.payload.benefit_key || '').trim()
    if (!planId || !roomId || !benefitId || !benefitKey) {
      message.action = undefined
      pushAgentMessage(message.domain, '这条活动修改确认缺少必要信息，请重新发起修改。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧活动修改确认已失效，没有写入数据库。')
      return
    }

    executing.value = true
    try {
      const current = await getLiveAgentPlanBenefits(planId)
      const existing = (current.items || []).find((item) => item.id === benefitId && item.key === benefitKey)
      if (!existing) throw new Error('当前活动福利已经变化，请重新发起修改。')
      if (expectedVersion && existing.version_no !== expectedVersion) {
        throw new Error('这条活动已经产生了新版本，请重新发起修改，避免覆盖新数据。')
      }

      const next = {
        expected_version_no: existing.version_no,
        key: benefitKey,
        link_key: String(action.payload.link_key || ''),
        product_name: String(action.payload.product_name || ''),
        activity_price: String(action.payload.activity_price || ''),
        gift: String(action.payload.gift || ''),
        activity: String(action.payload.activity || ''),
        starts_at: String(action.payload.starts_at || ''),
        ends_at: String(action.payload.ends_at || ''),
      }
      const changed =
        next.link_key !== (existing.link_key || '') ||
        next.product_name !== (existing.product_name || '') ||
        next.activity_price !== (existing.activity_price || '') ||
        next.gift !== (existing.gift || '') ||
        next.activity !== (existing.activity || '') ||
        next.starts_at !== benefitExistingTime(existing.starts_at) ||
        next.ends_at !== benefitExistingTime(existing.ends_at)
      if (!changed) {
        message.action = undefined
        pushAgentMessage(message.domain, '新值和当前活动福利一致，没有生成新版本。')
        return
      }

      const updated = await updateLiveAgentPlanBenefit(planId, benefitId, next)
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'benefits')
      focusLivePlanModule(planId, 'benefits')
      const statusText = updated.status === 'active'
        ? '当前已生效'
        : updated.status === 'expired'
          ? '当前已过期'
          : '当前为草稿'
      pushAgentMessage(
        message.domain,
        '已确认修改活动福利，正式版本更新为 V' + updated.version_no + '，' + statusText + '。',
      )
    } catch (error) {
      pushAgentMessage(
        message.domain,
        error instanceof Error ? '活动修改没有写入：' + error.message : '活动修改没有写入，请重试。',
      )
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'confirm_live_product_link_correction') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const correctedCommand = String(action.payload.corrected_command || '').trim()
    const originalCommand = String(action.payload.original_command || '').trim()
    if (!planId || !roomId || !correctedCommand) {
      pushAgentMessage(message.domain, '这条纠错确认缺少必要信息，请重新输入原指令。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧的纠错确认已失效，没有写入数据库。请在当前方案重新输入。')
      return
    }
    executing.value = true
    try {
      if (liveProductLinkActionFromText(correctedCommand) === 'update') {
        message.action = undefined
        executing.value = false
        await pushLiveProductUpdatePreview(message.domain, planId, roomId, correctedCommand)
        return
      }
      const result = await executeLiveProductLinkCommand(
        planId,
        correctedCommand,
        [
          '原输入：' + originalCommand,
          '纠正后：' + correctedCommand,
          '用户确认：采纳',
        ],
        'system-agent:product-links:typo-confirmed',
      )
      message.action = undefined
      focusLivePlanModule(planId, 'products')
      pushAgentMessage(
        message.domain,
        '已采纳文字纠正：\n原输入：' + originalCommand + '\n纠正后：' + correctedCommand + '\n' +
          (result || '已按纠正后的内容处理。'),
      )
    } catch (error) {
      pushAgentMessage(
        message.domain,
        error instanceof Error ? '采纳后写入失败：' + error.message : '采纳后写入失败，请重试。',
      )
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'add_live_image_product' || action.type === 'add_live_product') {
    const isImageProduct = action.type === 'add_live_image_product'
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    const linkKey = String(action.payload.link_key || '').trim()
    const productName = String(action.payload.product_name || '').trim()
    if (!planId || !roomId || !linkKey || !productName) {
      pushAgentMessage(message.domain, isImageProduct ? '这条图片识别结果缺少必要的商品信息，请重新识别后再添加。' : '这条商品候选缺少链接编号或商品名称，请重新说明。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, isImageProduct ? '你已经切换了直播间或直播方案，这条图片识别结果已失效，没有写入数据库。请在当前方案重新识别。' : '你已经切换了直播间或直播方案，这条商品候选已失效，没有写入数据库。')
      return
    }

    executing.value = true
    try {
      const current = await getLiveAgentPlanProductLinks(planId)
      const existing = (current.items || []).find((item) => item.link_key === linkKey)
      let resultText = ''
      if (existing) {
        if (!isImageProduct) {
          throw new Error('确认期间“' + linkKey + '”已经存在。为避免把“新增”悄悄变成“修改”，请重新发起操作。')
        }
        const updated = await updateLiveAgentPlanProductLink(planId, existing.id, {
          link_key: linkKey,
          product_name: productName || existing.product_name || '',
          spec: String(action.payload.spec || '').trim() || existing.spec || '',
          daily_price: String(action.payload.daily_price || '').trim() || existing.daily_price || '',
          quantity: String(action.payload.quantity || '').trim() || existing.quantity || '',
          audience: String(action.payload.audience || '').trim() || existing.audience || '',
        })
        resultText = isImageProduct
          ? '已把图片识别结果添加到“' + linkKey + '”，当前正式版本为 V' + updated.version_no + '。未识别到的字段保持原值。'
          : '已确认更新“' + linkKey + '”，当前正式版本为 V' + updated.version_no + '。'
      } else {
        const adopted = await adoptLiveAgentPlanProductLinks(
          planId,
          [{
            link_key: linkKey,
            product_name: productName,
            spec: String(action.payload.spec || '').trim(),
            daily_price: String(action.payload.daily_price || '').trim(),
            quantity: String(action.payload.quantity || '').trim(),
            audience: String(action.payload.audience || '').trim(),
            review_bucket: 'adoptable',
            source_quotes: [
              String(action.payload.source_text || '').trim(),
              isImageProduct ? '用户确认：添加图片识别结果' : '用户确认：添加商品链接',
            ].filter(Boolean),
          }],
          undefined,
          isImageProduct ? 'system-agent:image-product' : 'system-agent:intent-product',
        )
        const saved = adopted.results?.find((item) => item.saved)?.saved
        if (!adopted.adopted) {
          throw new Error(adopted.results?.[0]?.message || '商品信息没有写入，请检查当前方案是否存在冲突。')
        }
        resultText = (isImageProduct ? '已把图片识别结果添加为“' : '已确认添加“') + linkKey + ' · ' + productName + '”' +
          (saved?.version_no ? '，当前正式版本 V' + saved.version_no : '') + '。'
      }
      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'products')
      focusLivePlanModule(planId, 'products')
      pushAgentMessage(message.domain, resultText)
    } catch (error) {
      pushAgentMessage(
        message.domain,
        error instanceof Error ? '添加失败：' + error.message : '添加失败，请重试。',
      )
    } finally {
      executing.value = false
    }
    return
  }

  if (action.type === 'add_live_benefit') {
    const planId = Number(action.payload.plan_id || 0)
    const roomId = Number(action.payload.room_id || 0)
    if (!planId || !roomId) {
      pushAgentMessage(message.domain, '这条活动福利候选缺少当前直播间或方案信息，请重新输入。')
      return
    }
    const currentRoomId = Number(window.localStorage.getItem('system-agent-live-room-id') || 0)
    if (currentRoomId !== roomId || liveStrategyPlanId.value !== planId) {
      message.action = undefined
      pushAgentMessage(message.domain, '你已经切换了直播间或直播方案，这条旧的活动福利候选已失效，没有写入数据库。请在当前方案重新输入。')
      return
    }

    const candidate: LiveAgentPlanBenefitCandidate = {
      key: String(action.payload.benefit_key || '').trim(),
      link_key: String(action.payload.link_key || '').trim(),
      product_name: String(action.payload.product_name || '').trim(),
      activity_price: String(action.payload.activity_price || '').trim(),
      gift: String(action.payload.gift || '').trim(),
      activity: String(action.payload.activity || '').trim(),
      starts_at: String(action.payload.starts_at || '').trim(),
      ends_at: String(action.payload.ends_at || '').trim(),
      review_bucket: String(action.payload.review_bucket || 'discuss').trim(),
      review_reason: String(action.payload.review_reason || '').trim(),
      source_quotes: [
        String(action.payload.source_text || '').trim(),
        '用户确认：添加活动福利',
      ].filter(Boolean),
    }
    if (!candidate.activity_price && !candidate.gift && !candidate.activity) {
      pushAgentMessage(message.domain, '这条候选没有可写入的活动价、赠品或活动内容，请重新说明活动福利。')
      return
    }

    executing.value = true
    try {
      const result = await adoptLiveAgentPlanBenefits(
        planId,
        [candidate],
        undefined,
        'system-agent:benefits:natural-language',
      )
      const first = result.results?.[0]
      if (result.blocked) {
        throw new Error(first?.message || '活动福利被规则阻止，尚未写入。')
      }
      if (result.conflicts && first?.existing) {
        const existing = first.existing
        const nextProductName = candidate.product_name || existing.product_name || ''
        const nextActivityPrice = candidate.activity_price || existing.activity_price || ''
        const nextGift = candidate.gift || existing.gift || ''
        const nextActivity = candidate.activity || existing.activity || ''
        const nextStartsAt = candidate.starts_at || benefitExistingTime(existing.starts_at)
        const nextEndsAt = candidate.ends_at || benefitExistingTime(existing.ends_at)
        message.action = undefined
        pushAgentMessage(
          message.domain,
          '当前方案已经有同一活动位。我没有重复新增，而是把你的内容整理成“修改现有活动”。请核对修改前后：',
          {
            type: 'confirm_live_benefit_update',
            title: '确认修改活动福利',
            summary: '点击确认后才会覆盖当前活动内容并生成新版本；未提供的字段保持原值。',
            risk_level: 'low',
            requires_confirmation: true,
            payload: {
              plan_id: planId,
              room_id: roomId,
              benefit_id: existing.id,
              benefit_key: existing.key,
              current_version_no: existing.version_no,
              link_key: candidate.link_key || existing.link_key || '',
              current_link_key: existing.link_key || '',
              current_product_name: existing.product_name || '',
              current_activity_price: existing.activity_price || '',
              current_gift: existing.gift || '',
              current_activity: existing.activity || '',
              current_starts_at: benefitExistingTime(existing.starts_at),
              current_ends_at: benefitExistingTime(existing.ends_at),
              product_name: nextProductName,
              activity_price: nextActivityPrice,
              gift: nextGift,
              activity: nextActivity,
              starts_at: nextStartsAt,
              ends_at: nextEndsAt,
              source_text: String(action.payload.source_text || '').trim(),
            },
          },
        )
        return
      }
      if (!result.adopted && !result.drafted && !result.skipped) {
        throw new Error(first?.message || '活动福利没有发生写入。')
      }

      message.action = undefined
      notifyLivePlanModuleUpdated(planId, 'benefits')
      focusLivePlanModule(planId, 'benefits')
      if (result.adopted) {
        const version = first?.saved?.version_no
        pushAgentMessage(
          message.domain,
          '已真正写入活动福利并在有效期内生效' + (version ? '，当前版本 V' + version : '') + '。左侧活动福利列表已刷新。',
        )
      } else if (result.drafted) {
        const version = first?.saved?.version_no
        pushAgentMessage(
          message.domain,
          '已真正写入活动福利草稿' + (version ? '，当前版本 V' + version : '') + '。因为没有完整有效期或尚未到开始时间，所以暂时不会进入直播生成；左侧列表已刷新。',
        )
      } else {
        pushAgentMessage(message.domain, first?.message || '当前方案已经存在相同活动福利，没有重复写入。')
      }
    } catch (error) {
      pushAgentMessage(
        message.domain,
        error instanceof Error ? '添加活动福利失败：' + error.message : '添加活动福利失败，请重试。',
      )
    } finally {
      executing.value = false
    }
    return
  }

  if (!isInternalAgentProfile.value) {
    pushAgentMessage(
      message.domain,
      '当前账号不能执行内部管理动作。',
    )
    return
  }

  executing.value = true
  try {
    const response = await executeInternalAgentAction(action)
    latestSystemResponse.value = response
    systemContext.value.capabilities = response.capabilities
    message.action = undefined

    if (response.credential) {
      messages.value.push({
        role: 'agent',
        domain: message.domain,
        text: response.reply,
        credential: response.credential,
        conversationScope: conversationScopeForDomain(message.domain),
      })
      void scrollChatToBottom()
    } else {
      pushAgentMessage(message.domain, response.reply)
    }

    if (response.state === 'succeeded' || response.state === 'permission_denied') {
      activeSystemTask.value = null
      systemTaskHistory.value = []
    }
    return
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
            <div v-if="currentAgentImages.length" class="agent-image-strip">
              <div v-for="image in currentAgentImages" :key="image.id" class="agent-image-chip">
                <button type="button" class="agent-image-thumb" :title="'打开' + image.label" @click.stop="imagePreview = image">
                  <img :src="image.dataUrl" :alt="image.label" />
                  <span>{{ image.label }}</span>
                </button>
                <button type="button" class="agent-image-reference" :title="'引用@' + image.label" @click.stop="insertAgentImageReference(image)">@</button>
                <button type="button" class="agent-image-remove" :aria-label="'删除' + image.label" @click.stop="removeAgentImage(image)">×</button>
              </div>
            </div>
            <div v-if="imageAttachmentError" class="agent-image-error">{{ imageAttachmentError }}</div>
            <textarea
              ref="inputEl"
              v-model="input"
              rows="2"
              :placeholder="inputPlaceholder"
              @focus="composerFocus('dock')"
              @input="composerInput('dock')"
              @paste="handleComposerPaste"
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
                <i>{{ item.kind === 'image' ? '图' : item.kind === 'department' ? '@' : '/' }}</i>
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

    <button
      v-if="actor && !drawerOpen"
      class="system-agent-drawer-edge-handle collapsed"
      data-edge-handle="system-agent-drawer"
      type="button"
      aria-label="展开智能体抽屉"
      title="展开智能体抽屉"
      @click="openDrawer"
    >‹</button>

    <div
      v-if="drawerOpen && actor"
      class="system-agent-backdrop"
    >
      <aside
        class="system-agent-drawer"
        :class="{'without-work-inbox': !showInbox, 'terminal-agent-drawer': isTerminalCustomer}"
        @wheel="handleDrawerWheel"
      >
        <button
          class="system-agent-drawer-edge-handle expanded"
          data-edge-handle="system-agent-drawer"
          type="button"
          aria-label="折叠智能体抽屉"
          title="折叠智能体抽屉"
          @click="drawerOpen = false"
        >›</button>
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
        <div v-show="!showInbox || drawerTab==='chat'" class="system-agent-chat-shell">
        <section ref="chatEl" class="system-agent-chat" @scroll="handleChatScroll" @wheel.stop>
          <div v-if="!isTerminalCustomer && visibleMessages.length === 0" class="system-agent-context-empty">
            <strong>{{ contextLabel }}</strong>
            <p>{{ contextDescription }}</p>
          </div>

          <article
            v-for="(message, index) in visibleMessages"
            :key="index"
            :class="['system-agent-message', message.role]"
          >
            <div
              v-if="!message.answerReference && message.role === 'agent'"
              class="agent-message-formatted"
            >
              <template v-for="(block, blockIndex) in formatAgentMessageBlocks(message.text)" :key="blockIndex">
                <h4 v-if="block.kind === 'heading'">{{ block.text }}</h4>
                <div v-else-if="block.kind === 'prompt'" class="agent-message-prompt">{{ block.text }}</div>
                <div v-else-if="block.kind === 'field'" class="agent-message-field">
                  <strong>{{ block.label }}</strong>
                  <span>{{ block.text }}</span>
                </div>
                <div v-else-if="block.kind === 'choices'" class="agent-message-choices">
                  <button
                    v-for="choice in block.items"
                    :key="choice"
                    type="button"
                    @click="chooseAgentDisplayOption(block, choice)"
                  >{{ choice }}</button>
                </div>
                <div v-else-if="block.kind === 'examples'" class="agent-message-examples">
                  <strong>示例</strong>
                  <button
                    v-for="example in block.items"
                    :key="example"
                    type="button"
                    @click="chooseAgentDisplayExample(example)"
                  >{{ example }}</button>
                </div>
                <div v-else-if="block.kind === 'bullet'" class="agent-message-bullet">
                  <i></i>
                  <span>{{ block.text }}</span>
                </div>
                <div v-else-if="block.kind === 'numbered'" class="agent-message-numbered">
                  <b>{{ block.marker }}</b>
                  <span>{{ block.text }}</span>
                </div>
                <p v-else>{{ block.text }}</p>
              </template>
            </div>
            <p v-else-if="!message.answerReference">{{ message.text }}</p>

            <div v-if="message.speechChoice" class="live-speech-choice-card">
              <span>播出前审核结果</span>
              <p>{{ message.speechChoice.text }}</p>
              <div class="live-speech-choice-actions">
                <button
                  type="button"
                  :disabled="message.speechChoice.status === 'sending' || message.speechChoice.status === 'sent'"
                  @click="executePreparedLiveRoomSpeech(message, 'quick')"
                >
                  {{ message.speechChoice.status === 'sending' && message.speechChoice.selected === 'quick' ? '提交中…' : '抢答' }}
                </button>
                <button
                  type="button"
                  :disabled="message.speechChoice.status === 'sending' || message.speechChoice.status === 'sent'"
                  @click="executePreparedLiveRoomSpeech(message, 'answer')"
                >
                  {{ message.speechChoice.status === 'sending' && message.speechChoice.selected === 'answer' ? '提交中…' : '回答' }}
                </button>
                <small v-if="message.speechChoice.status === 'sent'">
                  已按{{ message.speechChoice.selected === 'quick' ? '抢答' : '回答' }}提交
                </small>
              </div>
            </div>

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
                <span>{{ message.action.type === 'add_live_image_product'
                  ? '图片识别结果'
                  : message.action.type === 'add_live_product'
                    ? '商品链接候选'
                  : message.action.type === 'add_live_benefit'
                    ? '活动福利候选'
                  : message.action.type === 'clarify_live_product_update'
                    ? '选择修改字段'
                  : message.action.type === 'confirm_live_product_update'
                    ? '商品修改确认'
                  : message.action.type === 'confirm_live_product_disable'
                    ? '商品停用确认'
                  : message.action.type === 'clarify_live_benefit_target'
                    ? '选择活动'
                  : message.action.type === 'clarify_live_benefit_update'
                    ? '选择活动字段'
                  : message.action.type === 'confirm_live_benefit_update'
                    ? '活动修改确认'
                  : message.action.type === 'confirm_live_benefit_disable'
                    ? '活动停用确认'
                  : message.action.type === 'add_live_fact'
                    ? '事实依据候选'
                  : message.action.type === 'confirm_live_fact_update'
                    ? '事实修改确认'
                  : message.action.type === 'confirm_live_fact_disable'
                    ? '事实停用确认'
                  : message.action.type === 'add_live_script_reference'
                    ? '口播样稿候选'
                  : message.action.type === 'confirm_live_script_reference_update'
                    ? '口播样稿修改确认'
                  : message.action.type === 'confirm_live_script_reference_disable'
                    ? '口播样稿停用确认'
                  : message.action.type === 'confirm_live_plan_bind'
                    ? '方案绑定确认'
                  : message.action.type === 'confirm_live_plan_unbind'
                    ? '方案解绑确认'
                  : message.action.type === 'confirm_live_plan_switch'
                    ? '方案切换确认'
                  : message.action.type === 'clarify_live_strategy_intent'
                    ? '确认意图'
                    : '待确认动作' }}</span>
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
              <dl v-else-if="message.action.type === 'confirm_live_product_link_correction'">
                <div>
                  <dt>原输入</dt>
                  <dd>{{ message.action.payload.original_command || '-' }}</dd>
                </div>
                <div>
                  <dt>建议纠正</dt>
                  <dd>{{ message.action.payload.corrected_command || '-' }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'add_live_image_product' || message.action.type === 'add_live_product'">
                <div>
                  <dt>链接</dt>
                  <dd>{{ message.action.payload.link_key || '-' }}</dd>
                </div>
                <div>
                  <dt>商品名称</dt>
                  <dd>{{ message.action.payload.product_name || '-' }}</dd>
                </div>
                <div v-if="message.action.payload.daily_price">
                  <dt>价格</dt>
                  <dd>{{ message.action.payload.daily_price }}</dd>
                </div>
                <div v-if="message.action.payload.spec">
                  <dt>规格/卖点</dt>
                  <dd>{{ message.action.payload.spec }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'add_live_benefit'">
                <div>
                  <dt>链接</dt>
                  <dd>{{ message.action.payload.link_key || '全直播间' }}</dd>
                </div>
                <div v-if="message.action.payload.product_name">
                  <dt>商品</dt>
                  <dd>{{ message.action.payload.product_name }}</dd>
                </div>
                <div v-if="message.action.payload.activity_price">
                  <dt>活动价</dt>
                  <dd>{{ message.action.payload.activity_price }}</dd>
                </div>
                <div v-if="message.action.payload.gift">
                  <dt>赠品</dt>
                  <dd>{{ message.action.payload.gift }}</dd>
                </div>
                <div v-if="message.action.payload.activity">
                  <dt>活动内容</dt>
                  <dd>{{ message.action.payload.activity }}</dd>
                </div>
                <div>
                  <dt>有效期</dt>
                  <dd>{{ message.action.payload.starts_at || '未设置' }} → {{ message.action.payload.ends_at || '未设置' }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'add_live_fact' || message.action.type === 'confirm_live_fact_update' || message.action.type === 'confirm_live_fact_disable'">
                <div>
                  <dt>事实分类</dt>
                  <dd>{{ message.action.payload.fact_category || '-' }}</dd>
                </div>
                <div>
                  <dt>事实名称</dt>
                  <dd>{{ message.action.payload.fact_key || '-' }}</dd>
                </div>
                <div v-if="message.action.type === 'confirm_live_fact_update'">
                  <dt>内容</dt>
                  <dd>{{ message.action.payload.current_fact_value || '未设置' }} → {{ message.action.payload.fact_value || '未设置' }}</dd>
                </div>
                <div v-else>
                  <dt>内容</dt>
                  <dd>{{ message.action.payload.fact_value || message.action.payload.current_fact_value || '-' }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'add_live_script_reference' || message.action.type === 'confirm_live_script_reference_update' || message.action.type === 'confirm_live_script_reference_disable'">
                <div>
                  <dt>参考名称</dt>
                  <dd>{{ message.action.payload.script_title || message.action.payload.script_reference_key || '-' }}</dd>
                </div>
                <div>
                  <dt>执行方式</dt>
                  <dd>{{ message.action.payload.execution_mode === 'verbatim' ? '100%原话' : '意图参考' }}</dd>
                </div>
                <div v-if="message.action.type === 'confirm_live_script_reference_update'">
                  <dt>话术内容</dt>
                  <dd>{{ message.action.payload.current_script_text || '未设置' }} → {{ message.action.payload.script_text || '未设置' }}</dd>
                </div>
                <div v-else>
                  <dt>话术内容</dt>
                  <dd>{{ message.action.payload.script_text || message.action.payload.current_script_text || '-' }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'confirm_live_plan_bind' || message.action.type === 'confirm_live_plan_unbind' || message.action.type === 'confirm_live_plan_switch'">
                <div>
                  <dt>目标方案</dt>
                  <dd>{{ message.action.payload.target_plan_name || '-' }}</dd>
                </div>
                <div>
                  <dt>操作</dt>
                  <dd>{{ message.action.type === 'confirm_live_plan_bind' ? '绑定到当前直播间' : message.action.type === 'confirm_live_plan_unbind' ? '从当前直播间解绑' : '切换为当前运行方案' }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'confirm_live_product_update'">
                <div v-if="message.action.payload.current_product_name !== message.action.payload.product_name">
                  <dt>商品名称</dt>
                  <dd>{{ message.action.payload.current_product_name || '未设置' }} → {{ message.action.payload.product_name || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_spec !== message.action.payload.spec">
                  <dt>规格</dt>
                  <dd>{{ message.action.payload.current_spec || '未设置' }} → {{ message.action.payload.spec || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_daily_price !== message.action.payload.daily_price">
                  <dt>日常价</dt>
                  <dd>{{ message.action.payload.current_daily_price || '未设置' }} → {{ message.action.payload.daily_price || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_quantity !== message.action.payload.quantity">
                  <dt>数量</dt>
                  <dd>{{ message.action.payload.current_quantity || '未设置' }} → {{ message.action.payload.quantity || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_audience !== message.action.payload.audience">
                  <dt>适用人群</dt>
                  <dd>{{ message.action.payload.current_audience || '未设置' }} → {{ message.action.payload.audience || '未设置' }}</dd>
                </div>
              </dl>
              <dl v-else-if="message.action.type === 'confirm_live_benefit_update'">
                <div v-if="message.action.payload.current_product_name !== message.action.payload.product_name">
                  <dt>商品名称</dt>
                  <dd>{{ message.action.payload.current_product_name || '未设置' }} → {{ message.action.payload.product_name || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_activity_price !== message.action.payload.activity_price">
                  <dt>活动价</dt>
                  <dd>{{ message.action.payload.current_activity_price || '未设置' }} → {{ message.action.payload.activity_price || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_gift !== message.action.payload.gift">
                  <dt>赠品</dt>
                  <dd>{{ message.action.payload.current_gift || '未设置' }} → {{ message.action.payload.gift || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_activity !== message.action.payload.activity">
                  <dt>活动内容</dt>
                  <dd>{{ message.action.payload.current_activity || '未设置' }} → {{ message.action.payload.activity || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_starts_at !== message.action.payload.starts_at">
                  <dt>开始时间</dt>
                  <dd>{{ message.action.payload.current_starts_at || '未设置' }} → {{ message.action.payload.starts_at || '未设置' }}</dd>
                </div>
                <div v-if="message.action.payload.current_ends_at !== message.action.payload.ends_at">
                  <dt>结束时间</dt>
                  <dd>{{ message.action.payload.current_ends_at || '未设置' }} → {{ message.action.payload.ends_at || '未设置' }}</dd>
                </div>
              </dl>
              <div
                v-else-if="message.action.type === 'clarify_live_strategy_intent'"
                class="system-agent-intent-options"
              >
                <button
                  v-for="option in message.action.payload.intent_options || []"
                  :key="option.id"
                  type="button"
                  :disabled="executing"
                  @click="chooseLiveStrategyIntent(message, option)"
                >
                  <strong>{{ option.label }}</strong>
                  <small v-if="option.description">{{ option.description }}</small>
                </button>
              </div>
              <div
                v-else-if="message.action.type === 'clarify_live_product_update'"
                class="system-agent-intent-options"
              >
                <button
                  v-for="option in message.action.payload.intent_options || []"
                  :key="option.id"
                  type="button"
                  :disabled="executing"
                  @click="chooseLiveProductUpdateField(message, option)"
                >
                  <strong>{{ option.label }}</strong>
                  <small v-if="option.description">当前：{{ option.description }}</small>
                </button>
              </div>
              <div
                v-else-if="message.action.type === 'clarify_live_benefit_target' || message.action.type === 'clarify_live_benefit_update'"
                class="system-agent-intent-options"
              >
                <button
                  v-for="option in message.action.payload.intent_options || []"
                  :key="option.id"
                  type="button"
                  :disabled="executing"
                  @click="chooseLiveBenefitUpdateField(message, option)"
                >
                  <strong>{{ option.label }}</strong>
                  <small v-if="option.description">当前：{{ option.description }}</small>
                </button>
              </div>
              <button
                v-if="message.action.type !== 'clarify_live_strategy_intent' && message.action.type !== 'clarify_live_product_update' && message.action.type !== 'clarify_live_benefit_target' && message.action.type !== 'clarify_live_benefit_update'"
                class="primary-button"
                type="button"
                :disabled="executing"
                @click="executeAction(message)"
              >
                {{ executing
                  ? '处理中…'
                  : message.action.type === 'add_live_image_product'
                    ? '添加'
                    : message.action.type === 'add_live_product'
                      ? '确认添加'
                    : message.action.type === 'add_live_benefit'
                      ? '添加活动福利'
                    : message.action.type === 'confirm_live_product_link_correction'
                      ? '采纳'
                    : message.action.type === 'confirm_live_product_update'
                      ? '确认修改'
                    : message.action.type === 'confirm_live_product_disable'
                      ? '确认停用'
                    : message.action.type === 'confirm_live_benefit_update'
                      ? '确认修改活动'
                    : message.action.type === 'confirm_live_benefit_disable'
                      ? '确认停用活动'
                    : message.action.type === 'add_live_fact'
                      ? '确认添加事实'
                    : message.action.type === 'confirm_live_fact_update'
                      ? '确认修改事实'
                    : message.action.type === 'confirm_live_fact_disable'
                      ? '确认停用事实'
                    : message.action.type === 'add_live_script_reference'
                      ? '确认添加话术'
                    : message.action.type === 'confirm_live_script_reference_update'
                      ? '确认修改话术'
                    : message.action.type === 'confirm_live_script_reference_disable'
                      ? '确认停用话术'
                    : message.action.type === 'confirm_live_plan_bind'
                      ? '确认绑定'
                    : message.action.type === 'confirm_live_plan_unbind'
                      ? '确认解绑'
                    : message.action.type === 'confirm_live_plan_switch'
                      ? '确认切换'
                      : '确认执行' }}
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
            <div class="system-agent-thinking-row">
              <span class="system-agent-thinking-dots" aria-label="智能体正在思考">
                <i></i><i></i><i></i>
              </span>
              <small>{{ isTerminalCustomer ? '正在思考…' : '正在理解你的要求，并检查当前工作域与权限…' }}</small>
            </div>
          </article>
        </section>
        <button
          v-if="showChatJumpToBottom"
          class="system-agent-jump-bottom"
          type="button"
          title="快速到底"
          aria-label="快速滚动到最新消息"
          @click="jumpChatToBottom"
        >⌄</button>
        </div>

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
            <div v-if="currentAgentImages.length" class="agent-image-strip">
              <div v-for="image in currentAgentImages" :key="image.id" class="agent-image-chip">
                <button type="button" class="agent-image-thumb" :title="'打开' + image.label" @click.stop="imagePreview = image">
                  <img :src="image.dataUrl" :alt="image.label" />
                  <span>{{ image.label }}</span>
                </button>
                <button type="button" class="agent-image-reference" :title="'引用@' + image.label" @click.stop="insertAgentImageReference(image)">@</button>
                <button type="button" class="agent-image-remove" :aria-label="'删除' + image.label" @click.stop="removeAgentImage(image)">×</button>
              </div>
            </div>
            <div v-if="imageAttachmentError" class="agent-image-error">{{ imageAttachmentError }}</div>
            <textarea
              ref="drawerInputEl"
              v-model="input"
              rows="3"
              :placeholder="inputPlaceholder"
              @focus="composerFocus('drawer')"
              @input="composerInput('drawer')"
              @paste="handleComposerPaste"
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
                <i>{{ item.kind === 'image' ? '图' : item.kind === 'department' ? '@' : '/' }}</i>
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
    <div
      v-if="imagePreview"
      class="agent-image-preview-backdrop"
      @click.self="imagePreview = null"
    >
      <section class="agent-image-preview-card">
        <header>
          <div>
            <strong>{{ imagePreview.label }}</strong>
            <small>{{ imagePreview.width }} × {{ imagePreview.height }}</small>
          </div>
          <button type="button" aria-label="关闭图片预览" @click="imagePreview = null">×</button>
        </header>
        <img :src="imagePreview.dataUrl" :alt="imagePreview.label" />
      </section>
    </div>
  </Teleport>
</template>
<style scoped>
.agent-image-strip{display:flex;align-items:flex-start;gap:8px;max-width:100%;padding:2px 2px 7px;overflow-x:auto;scrollbar-width:thin}.agent-image-chip{position:relative;flex:0 0 72px;width:72px;height:68px;border:1px solid rgba(91,107,207,.2);border-radius:10px;background:#f8f9ff;box-shadow:0 3px 10px rgba(71,86,169,.07);overflow:hidden}.agent-image-thumb{display:grid!important;grid-template-rows:45px 17px!important;width:100%!important;height:100%!important;min-width:0!important;min-height:0!important;margin:0!important;padding:0!important;border:0!important;border-radius:0!important;background:transparent!important;color:#5a6480!important;cursor:pointer!important;font-size:10px!important;line-height:1.1!important;box-shadow:none!important}.agent-image-thumb img{display:block;width:100%;height:45px;object-fit:cover;background:#eef1f8}.agent-image-thumb span{display:block;padding:3px 18px 0 4px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;text-align:left;font-size:10px!important;font-weight:800;line-height:1.1}.agent-image-reference,.agent-image-remove{position:absolute;z-index:2;display:grid!important;place-items:center;width:18px!important;height:18px!important;min-width:18px!important;min-height:18px!important;padding:0!important;border:0!important;border-radius:6px!important;background:rgba(42,51,79,.78)!important;color:#fff!important;font-size:11px!important;font-weight:900!important;line-height:1!important;cursor:pointer!important;box-shadow:none!important}.agent-image-remove{top:3px;right:3px}.agent-image-reference{right:3px;bottom:3px;background:rgba(82,99,213,.9)!important}.agent-image-error{margin:0 2px 6px;padding:5px 8px;border-radius:7px;background:#fff1f3;color:#c24655;font-size:11px;line-height:1.35}.agent-image-preview-backdrop{position:fixed;inset:0;z-index:20050;display:grid;place-items:center;padding:28px;background:rgba(18,23,40,.62);backdrop-filter:blur(3px)}.agent-image-preview-card{display:grid;grid-template-rows:auto minmax(0,1fr);width:min(980px,88vw);max-height:88vh;border:1px solid rgba(255,255,255,.55);border-radius:16px;background:#fff;box-shadow:0 24px 80px rgba(12,18,44,.32);overflow:hidden}.agent-image-preview-card>header{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:11px 14px;border-bottom:1px solid #e5e8f2;background:#fafbff}.agent-image-preview-card>header>div{display:grid;gap:2px}.agent-image-preview-card>header strong{color:#35405a;font-size:14px}.agent-image-preview-card>header small{color:#929aac;font-size:10px}.agent-image-preview-card>header button{display:grid;place-items:center;width:30px;height:30px;padding:0;border:1px solid #dfe3ee;border-radius:8px;background:#fff;color:#59647a;font-size:20px;cursor:pointer}.agent-image-preview-card>img{display:block;max-width:100%;max-height:calc(88vh - 55px);margin:auto;object-fit:contain;background:#f3f5fa}
.live-room-answer-state{display:flex;align-items:center;gap:8px;min-height:24px;margin:0 0 5px;padding:3px 8px;border:1px solid rgba(104,118,220,.18);border-radius:8px;background:rgba(244,246,255,.9);color:#66708c;font-size:12px;line-height:1.35}.live-room-answer-state strong{flex:0 0 auto;color:#5666d8;font-size:12px;font-weight:900}.live-room-answer-state span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.live-room-answer-state.error{border-color:rgba(216,63,79,.22);background:rgba(255,244,246,.94);color:#cf4050}.live-room-answer-state.error strong{color:#cf4050}
.live-room-test-toggle{display:flex;align-items:center;gap:8px}.live-room-test-toggle button{height:26px;padding:0 10px;border:1px solid rgba(84,104,214,.22);border-radius:999px;background:rgba(255,255,255,.88);color:#6672b8;font:inherit;font-size:11px;font-weight:900;cursor:pointer;box-shadow:0 2px 8px rgba(72,88,170,.06)}.live-room-test-toggle button.active{border-color:rgba(84,104,214,.5);background:linear-gradient(135deg,rgba(96,111,230,.16),rgba(120,134,243,.10));color:#4f5fd0;box-shadow:0 0 0 2px rgba(84,104,214,.07),0 4px 12px rgba(72,88,170,.10)}.live-room-test-toggle span{color:#8a93a8;font-size:10px;line-height:1.3}.dock-live-room-test-toggle{flex:0 0 100%;width:100%;box-sizing:border-box;justify-content:flex-start;margin:2px 0 0;padding:7px 10px 0 0;border-top:1px solid rgba(105,121,190,.12)}.drawer-live-room-test-toggle{grid-column:1 / -1;margin:0;padding-top:7px;border-top:1px solid rgba(105,121,190,.12)}
.live-speech-choice-card{display:grid;gap:10px;margin-top:4px;padding:12px;border:1px solid rgba(85,103,214,.22);border-radius:12px;background:linear-gradient(145deg,#f9faff,#f1f4ff)}.live-speech-choice-card>span{color:#5a67c8;font-size:12px;font-weight:900}.live-speech-choice-card>p{margin:0!important;padding:10px 12px;border-radius:9px;background:#fff;color:#2f3a52!important;font-size:15px!important;line-height:1.65!important;white-space:pre-wrap}.live-speech-choice-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.live-speech-choice-actions>button{min-width:86px;min-height:38px;padding:7px 14px;border:1px solid rgba(82,99,211,.30);border-radius:10px;background:#fff;color:#4c5dcc;font:inherit;font-size:13px;font-weight:900;cursor:pointer}.live-speech-choice-actions>button:first-child{background:linear-gradient(135deg,#6475ea,#5264da);color:#fff}.live-speech-choice-actions>button:disabled{opacity:.55;cursor:default}.live-speech-choice-actions>small{color:#7d88a4;font-size:11px;font-weight:800}
.answer-reference-context{display:flex;align-items:center;justify-content:space-between;gap:10px;margin:0 0 6px;padding:7px 9px;border:1px solid rgba(84,104,214,.2);border-radius:10px;background:linear-gradient(135deg,rgba(241,244,255,.96),rgba(250,251,255,.96));color:#5f6985;line-height:1.35}.answer-reference-context>div{min-width:0;display:grid;gap:2px}.answer-reference-context strong{color:#5362cf;font-size:12px;font-weight:900}.answer-reference-context span{max-width:440px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}.answer-reference-context small{color:#929bb0;font-size:10px}.answer-reference-context button{flex:0 0 auto;padding:4px 8px;border:1px solid rgba(84,104,214,.16);border-radius:8px;background:#fff;color:#6874b8;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.answer-reference-context.picking{border-style:dashed;background:rgba(244,246,255,.96)}.coaching-context{border-color:rgba(112,91,220,.22);background:linear-gradient(135deg,rgba(244,241,255,.97),rgba(251,250,255,.97))}.drawer-answer-reference-context{grid-column:1 / -1;margin:0}.answer-reference-result-card{align-items:stretch;gap:14px;flex-wrap:wrap}.answer-reference-result-copy{display:grid;gap:8px}.answer-reference-result-target{color:#7f8aa0;font-size:12px;line-height:1.45}.answer-reference-result-text{margin:0!important;padding:12px 14px;border:1px solid rgba(92,111,210,.14);border-radius:10px;background:#f8f9ff;color:#293349!important;font-size:17px!important;font-weight:750;line-height:1.7;white-space:pre-wrap}.answer-reference-result-reference{color:#8d96a8;font-size:11px;line-height:1.5}.coaching-result-actions{display:flex;align-items:center;justify-content:flex-end;gap:8px;flex-wrap:wrap}.coaching-result-actions .ghost-button{min-height:36px;padding:7px 12px}
.agent-memory-list-card{align-items:stretch}.agent-memory-list-head{display:flex;align-items:center;justify-content:space-between;gap:10px}.agent-memory-list-head>span{color:#5666d8!important;font-size:13px!important;font-weight:900}.agent-memory-list-head>small{color:#929bb0;font-size:11px}.agent-memory-empty{padding:14px;border-radius:10px;background:#f8f9fc;color:#8a93a6;text-align:center}.agent-memory-item{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:start;gap:12px;padding:12px;border:1px solid #e5e8f2;border-radius:11px;background:#fafbff}.agent-memory-item>div:first-child{display:grid;min-width:0;gap:4px}.agent-memory-item>div:first-child>span{width:max-content;padding:2px 7px;border-radius:999px;background:#eef0ff;color:#5965c8;font-size:10px;font-weight:850}.agent-memory-item strong{color:#30394a;font-size:14px}.agent-memory-item p{margin:0!important;color:#596579!important;line-height:1.55;white-space:pre-wrap}.agent-memory-item small{color:#9aa2b0;font-size:10px}.agent-memory-actions{display:flex;align-items:center;justify-content:flex-end;gap:6px;flex-wrap:wrap}.agent-memory-actions button{min-height:32px;padding:5px 9px;border:1px solid #d9deeb;border-radius:8px;background:#fff;color:#5f6a7e;font:inherit;font-size:11px;font-weight:800;cursor:pointer}.agent-memory-actions button:hover{border-color:#aeb7df;background:#f4f6ff;color:#4f5fc4}@media(max-width:720px){.agent-memory-item{grid-template-columns:1fr}.agent-memory-actions{justify-content:flex-start}}
.system-agent-drawer{position:relative;grid-template-rows:auto auto minmax(0,1fr) auto;overflow:visible}
.system-agent-drawer>header,.system-agent-drawer>.agent-inbox-tabs,.system-agent-drawer>.system-agent-chat,.system-agent-drawer>footer,.system-agent-drawer>:deep(.work-inbox-panel.compact){overflow:hidden}
.system-agent-drawer-edge-handle{position:fixed;top:50%;z-index:1385;display:grid;width:28px;height:74px;place-items:center;padding:0;border:1px solid rgba(87,112,207,.30);background:rgba(239,245,255,.97);color:#5368bd;box-shadow:-5px 0 16px rgba(63,81,143,.13);font:inherit;font-size:23px;font-weight:900;line-height:1;cursor:pointer;transform:translateY(-50%);transition:background .18s ease,color .18s ease,box-shadow .18s ease}.system-agent-drawer-edge-handle:hover{background:#fff;color:#3f59d2;box-shadow:-7px 0 21px rgba(63,81,143,.18)}.system-agent-drawer-edge-handle.collapsed{right:0;border-radius:14px 0 0 14px}.system-agent-drawer .system-agent-drawer-edge-handle.expanded{position:absolute;left:-28px;right:auto;border-radius:14px 0 0 14px;pointer-events:auto}
.system-agent-drawer.without-work-inbox{grid-template-rows:auto minmax(0,1fr) auto}
.system-agent-drawer.terminal-agent-drawer{grid-template-rows:auto minmax(0,1fr) auto}
.terminal-agent-drawer > header{padding:18px 20px;align-items:center}
.terminal-agent-drawer > header h3{font-size:18px!important;line-height:1.5;margin:0}
.terminal-agent-drawer > header > div{min-width:0}
.terminal-agent-drawer .terminal-agent-subtitle{display:block;margin-top:4px;color:#65748e;font-size:14px;line-height:1.4;font-weight:500;letter-spacing:.06em;overflow-wrap:anywhere}
.terminal-agent-drawer > footer{grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:12px}
.terminal-agent-drawer > footer .system-agent-composer-field > textarea{display:block;margin:0}
.terminal-agent-drawer > footer > .primary-button{align-self:center;justify-self:end;min-width:76px;height:48px;min-height:48px;margin:0;padding:0 18px;border-radius:12px;white-space:nowrap;transform:none}
.terminal-agent-drawer .system-agent-message.user>p,.terminal-agent-drawer textarea,.terminal-agent-drawer > footer button{font-size:18px!important;line-height:1.6}
.agent-message-formatted{display:grid;gap:9px;min-width:0;color:#3b465b;font-size:15px;line-height:1.72}.terminal-agent-drawer .agent-message-formatted{font-size:16px}.agent-message-formatted h4{margin:2px 0 1px;color:#34415a;font-size:1em;font-weight:900;line-height:1.5}.agent-message-formatted p{margin:0!important;color:inherit!important;font-size:1em!important;line-height:1.72!important;white-space:pre-wrap}.agent-message-prompt{padding:1px 0 2px;color:#35415a;font-size:1.02em;font-weight:900;line-height:1.55}.agent-message-choices{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:7px}.agent-message-choices>span{display:flex;align-items:center;min-height:38px;padding:7px 10px;border:1px solid rgba(94,109,205,.18);border-radius:9px;background:linear-gradient(135deg,#fafbff,#f3f5ff);color:#4858bd;font-size:.9em;font-weight:850;line-height:1.4;box-sizing:border-box}.agent-message-examples{display:grid;gap:6px;padding:9px 10px;border:1px solid rgba(214,220,238,.9);border-radius:10px;background:#fafbfe}.agent-message-examples>strong{color:#7b8599;font-size:.78em;font-weight:900;letter-spacing:.04em}.agent-message-examples>code{display:block;padding:7px 9px;border-radius:7px;background:#fff;color:#4a5570;font-family:inherit;font-size:.88em;line-height:1.55;white-space:normal;overflow-wrap:anywhere;box-shadow:0 1px 4px rgba(53,68,122,.045)}.agent-message-field{display:grid;grid-template-columns:minmax(70px,112px) minmax(0,1fr);align-items:start;gap:10px;padding:8px 10px;border-left:3px solid rgba(91,105,205,.38);border-radius:0 8px 8px 0;background:rgba(247,249,255,.72)}.agent-message-field>strong{min-width:0;color:#5362c7;font-size:.92em;font-weight:900;line-height:1.55;overflow-wrap:anywhere}.agent-message-field>span{min-width:0;color:#465268;line-height:1.62;overflow-wrap:anywhere}.agent-message-bullet,.agent-message-numbered{display:grid;grid-template-columns:14px minmax(0,1fr);align-items:start;gap:7px}.agent-message-bullet>i{display:block;width:6px;height:6px;margin:10px 0 0 3px;border-radius:50%;background:#7180dd}.agent-message-bullet>span,.agent-message-numbered>span{min-width:0;overflow-wrap:anywhere}.agent-message-numbered{grid-template-columns:24px minmax(0,1fr)}.agent-message-numbered>b{color:#5968ca;font-size:.9em;font-weight:900;line-height:1.8}.system-agent-message.agent .system-agent-action-card p{font-size:14px!important;line-height:1.55!important}@media(max-width:720px){.agent-message-choices{grid-template-columns:1fr}}
.agent-message-choices>button{display:flex;align-items:center;min-height:38px;padding:7px 10px;border:1px solid rgba(94,109,205,.18);border-radius:9px;background:linear-gradient(135deg,#fafbff,#f3f5ff);color:#4858bd;font:inherit;font-size:.9em;font-weight:850;line-height:1.4;text-align:left;box-sizing:border-box;cursor:pointer;transition:border-color .16s ease,box-shadow .16s ease,transform .16s ease}.agent-message-choices>button:hover{border-color:rgba(83,100,211,.42);box-shadow:0 5px 14px rgba(74,91,181,.12);transform:translateY(-1px)}.agent-message-examples>button{display:block;width:100%;padding:7px 9px;border:0;border-radius:7px;background:#fff;color:#4a5570;font:inherit;font-size:.88em;line-height:1.55;text-align:left;white-space:normal;overflow-wrap:anywhere;box-shadow:0 1px 4px rgba(53,68,122,.045);cursor:pointer;transition:box-shadow .16s ease,transform .16s ease}.agent-message-examples>button:hover{box-shadow:0 5px 14px rgba(53,68,122,.10);transform:translateY(-1px)}
.system-agent-drawer :deep(.work-inbox-panel.compact){min-height:0;max-height:none;overflow:auto}
.system-agent-intent-options{display:grid;gap:8px;width:100%}.system-agent-intent-options>button{display:grid!important;grid-template-columns:1fr!important;justify-items:start!important;gap:3px!important;width:100%!important;min-height:54px!important;padding:10px 12px!important;border:1px solid rgba(86,103,207,.20)!important;border-radius:10px!important;background:linear-gradient(135deg,#fbfcff,#f4f6ff)!important;color:#34415d!important;text-align:left!important;cursor:pointer!important;box-shadow:0 3px 10px rgba(67,83,163,.05)!important}.system-agent-intent-options>button:hover{border-color:rgba(86,103,207,.48)!important;background:#eef1ff!important;box-shadow:0 6px 16px rgba(67,83,163,.10)!important}.system-agent-intent-options>button:disabled{cursor:default!important;opacity:.55}.system-agent-intent-options strong{font-size:13px;font-weight:900;color:#4356c9}.system-agent-intent-options small{font-size:11px;line-height:1.4;color:#7f8aa2}
.system-agent-chat-shell{position:relative;min-height:0;height:100%;overflow:hidden}
.system-agent-chat-shell>.system-agent-chat{height:100%;min-height:0;box-sizing:border-box;overflow-y:scroll!important;overflow-x:hidden;overscroll-behavior-y:contain;scrollbar-gutter:stable;scrollbar-width:thin;scrollbar-color:rgba(102,119,194,.58) rgba(226,233,248,.72)}
.system-agent-chat-shell>.system-agent-chat::-webkit-scrollbar{width:10px}.system-agent-chat-shell>.system-agent-chat::-webkit-scrollbar-track{background:rgba(226,233,248,.72);border-radius:999px}.system-agent-chat-shell>.system-agent-chat::-webkit-scrollbar-thumb{border:2px solid rgba(226,233,248,.72);border-radius:999px;background:rgba(102,119,194,.58)}.system-agent-chat-shell>.system-agent-chat::-webkit-scrollbar-thumb:hover{background:rgba(81,99,184,.78)}
.system-agent-jump-bottom{position:absolute;right:22px;bottom:16px;z-index:12;display:grid;width:42px;height:42px;place-items:center;padding:0;border:1px solid rgba(88,105,202,.28);border-radius:50%;background:rgba(255,255,255,.96);color:#5667cb;box-shadow:0 8px 24px rgba(54,71,145,.18);font:inherit;font-size:27px;font-weight:900;line-height:1;cursor:pointer;backdrop-filter:blur(10px);transition:transform .16s ease,box-shadow .16s ease,background .16s ease}.system-agent-jump-bottom:hover{transform:translateY(-2px);background:#fff;box-shadow:0 11px 28px rgba(54,71,145,.25)}
.system-agent-chat .system-agent-message{position:relative;display:grid;gap:7px;box-sizing:border-box;max-width:82%;margin-left:48px;padding:13px 15px 14px;border:1px solid rgba(211,220,239,.82);border-radius:6px 18px 18px 18px;background:linear-gradient(145deg,rgba(255,255,255,.98),rgba(247,250,255,.97));color:#39465d;box-shadow:0 7px 20px rgba(63,78,130,.075)}
.system-agent-chat .system-agent-message::before{content:'蓝';position:absolute;left:-48px;top:0;display:grid;width:36px;height:36px;place-items:center;border:1px solid rgba(255,255,255,.72);border-radius:12px;background:linear-gradient(145deg,#72a1ff,#405bf1);color:#fff;box-shadow:0 6px 16px rgba(56,83,198,.22);font-size:16px;font-weight:950;line-height:1}
.system-agent-chat .system-agent-message::after{content:'';position:absolute;left:-7px;top:14px;width:12px;height:12px;border-left:1px solid rgba(211,220,239,.82);border-bottom:1px solid rgba(211,220,239,.82);background:#fbfdff;transform:rotate(45deg)}
.system-agent-chat .system-agent-message>strong{position:relative;z-index:1;color:#5262c9;font-size:12px;font-weight:900;line-height:1.35}
.system-agent-chat .system-agent-message.user{justify-self:end;max-width:78%;margin-right:48px;margin-left:0;border-color:rgba(199,207,250,.88);border-radius:18px 6px 18px 18px;background:linear-gradient(145deg,#f4f5ff,#eef1ff);box-shadow:0 7px 20px rgba(70,78,168,.075)}
.system-agent-chat .system-agent-message.user::before{content:'我';right:-48px;left:auto;border-radius:50%;background:linear-gradient(145deg,#8f9cff,#6e74ec);box-shadow:0 6px 16px rgba(91,90,196,.18);font-size:14px}
.system-agent-chat .system-agent-message.user::after{right:-7px;left:auto;border:0;border-top:1px solid rgba(199,207,250,.88);border-right:1px solid rgba(199,207,250,.88);background:#f1f3ff}
.system-agent-chat .system-agent-message.user>strong{color:#5d59c7;text-align:right}
.system-agent-chat .system-agent-message.user>p{color:#3e4960}
.system-agent-chat .system-agent-message .system-agent-action-card{position:relative;z-index:1;border-color:rgba(211,220,241,.9);box-shadow:0 4px 13px rgba(59,72,124,.055)}
.system-agent-chat .system-agent-message.system-agent-thinking{min-width:210px;max-width:70%;padding-block:12px;background:linear-gradient(145deg,#fff,#f8faff)}
@media(max-width:720px){.system-agent-chat .system-agent-message{max-width:84%;margin-left:42px}.system-agent-chat .system-agent-message::before{left:-42px;width:32px;height:32px;border-radius:10px;font-size:14px}.system-agent-chat .system-agent-message.user{max-width:82%;margin-right:42px}.system-agent-chat .system-agent-message.user::before{right:-42px;left:auto}}
.agent-inbox-tabs{display:flex;gap:10px;padding:0 18px 12px}.agent-inbox-tabs button{position:relative;padding:8px 32px 8px 14px;min-height:40px;font-size:18px;border:1px solid #cfdbef;border-radius:8px;background:#fff;color:#315b94;cursor:pointer}.agent-inbox-tabs button.active{background:#eaf3ff;border-color:#75a4ea}.agent-inbox-tabs button:hover,.agent-inbox-tabs button:focus-visible{outline:none;box-shadow:0 0 0 3px #4285ff22;border-color:#5e96ed}.system-agent-orb{position:relative}
</style>
