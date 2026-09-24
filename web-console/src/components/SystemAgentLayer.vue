<script setup lang="ts">
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
  chatLiveAgent,
  chatLivePolicyAdminAgent,
  chatClientAgent,
  chatInternalAgent,
  chatLiveRoomPolicyAgent,
  createLiveOpsAnchorTraining,
  createCommercialMarketingCampaign,
  createStaffEmployee,
  getClientAgentContext,
  getInternalAgentContext,
  getPublicSystemConfig,
  getRooms,
  testLivePolicyAdmin,
} from '../api'
import type {
  InitialCredential,
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

type ChatMessage = {
  role: 'user' | 'agent'
  text: string
  domain: AgentDomain
  action?: SystemAgentActionPreview
  credential?: InitialCredential
}

type SuggestionItem = {
  kind: SuggestionKind
  label: string
  description: string
  insertText: string
}

const route = useRoute()
const router = useRouter()
const expanded = ref(false)
const drawerOpen = ref(false)
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

const dockEl = ref<HTMLElement | null>(null)
const dockPosition = ref<{ left: number; top: number } | null>(null)
const dockDragging = ref(false)
const dockStyle = computed(() => {
  if (!dockPosition.value) return undefined
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
const internalLiveStrategyMode = ref<'policy' | 'support'>(
  window.localStorage.getItem('system-agent-live-strategy-internal-mode') === 'support'
    ? 'support'
    : 'policy',
)
const liveSupportMode = ref<'strategy' | 'anchor' | 'voice'>(
  window.localStorage.getItem('system-agent-live-support-mode') === 'anchor'
    ? 'anchor'
    : window.localStorage.getItem('system-agent-live-support-mode') === 'voice'
      ? 'voice'
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

const messages = ref<ChatMessage[]>([
  {
    role: 'agent',
    domain: 'system',
    text: `我是${assistantName.value}。你在系统里走到哪里，我就切换到那个业务工作域；所有动作仍受当前账号权限和原有审批规则约束。`,
  },
])

const contextLabel = computed(() => {
  if (livePolicyTestMode.value.active && currentDomain.value === 'live-policy-admin') {
    return '直播策略 · 规则测试'
  }
  if (currentDomain.value === 'live-room') return '直播场控'
  if (currentDomain.value === 'live-strategy') return '直播策略 · 当前直播间 L3'
  if (currentDomain.value === 'live-policy-admin') return '直播策略 · 系统/行业规则'
  if (currentDomain.value === 'live-support') return '直播策略 · 客户授权协助'
  return isInternalAgentProfile.value ? '系统管理' : '终端助手'
})

const contextDescription = computed(() => {
  if (currentDomain.value === 'live-room') {
    return '已进入当前直播间场控上下文，直接处理现场问题、话术和场控协作。'
  }
  if (currentDomain.value === 'live-strategy') {
    return '已进入终端直播策略上下文，默认围绕当前选中直播间的 L3 策略工作。'
  }
  if (currentDomain.value === 'live-policy-admin') {
    return '已进入管理端直播策略上下文，自动跟随当前 L1/L2 与行业选择。'
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      return canDelegateLivePolicyL3(session.bootstrap)
        ? '已进入客户授权的 L3 代维护上下文，只会作用于当前授权直播间。'
        : '当前账号不能代维护客户 L3。L1 配置人员不可代维护 L3；其他员工需要 L2 配置能力及客户授权。'
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
  if (livePolicyTestMode.value.active && currentDomain.value === 'live-policy-admin') {
    return livePolicyTestHistory.value.length
      ? '继续打磨：例如“再自然一点”“销售感强一点”“保留意思再简短些”……'
      : '输入一个直播问题，之后可以连续反馈直到满意……'
  }
  if (currentDomain.value === 'live-room') {
    return '输入内容，或用 / 呼出场控能力……'
  }
  if (currentDomain.value === 'live-strategy') {
    return '输入策略要求，或用 / 呼出直播策略能力……'
  }
  if (currentDomain.value === 'live-policy-admin') {
    return '输入规则要求，或用 / 呼出 L1/L2 策略能力……'
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      return canDelegateLivePolicyL3(session.bootstrap)
        ? '输入客户 L3 调整要求，或用 / 呼出授权协助能力……'
        : '当前岗位不可代维护客户 L3，请选择本岗位可用功能……'
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
    return ['当前直播间 L3', '策略调教', '固定话术', '生成策略草稿']
  }
  if (currentDomain.value === 'live-policy-admin') {
    return ['L1/L2 策略', '行业规则', '规则调教', '生成策略草稿']
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      return canDelegateLivePolicyL3(session.bootstrap)
        ? ['客户授权 L3', '策略调教', '生成 L3 草稿']
        : ['当前岗位不可代维护 L3']
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

const visibleMessages = computed(() =>
  messages.value.filter((item) => item.domain === currentDomain.value),
)

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
      { kind: 'capability', label: '处理现场问题', description: '结合当前直播间上下文处理观众问题', insertText: '处理现场问题 ' },
      { kind: 'capability', label: '生成话术', description: '根据当前场景生成主播可说的话术', insertText: '生成话术 ' },
      { kind: 'capability', label: '场控建议', description: '结合直播状态给出现场操作建议', insertText: '场控建议 ' },
    ]
  }
  if (currentDomain.value === 'live-strategy') {
    return [
      { kind: 'capability', label: '调整策略', description: '调整当前直播间 L3 策略并生成草稿', insertText: '调整策略 ' },
      { kind: 'capability', label: '固定话术', description: '新增或调整当前直播间固定话术', insertText: '固定话术 ' },
      { kind: 'capability', label: '主播风格', description: '调整主播表达、风格或训练要求', insertText: '主播风格 ' },
    ]
  }
  if (currentDomain.value === 'live-policy-admin') {
    return [
      { kind: 'capability', label: '修改规则', description: '修改当前选择的 L1 或 L2 规则并生成草稿', insertText: '修改规则 ' },
      { kind: 'capability', label: '查看规则', description: '围绕当前系统/行业规则进行说明和检查', insertText: '查看规则 ' },
      { kind: 'capability', label: '生成草稿', description: '按自然语言要求生成策略草稿，不直接发布', insertText: '生成草稿 ' },
    ]
  }
  if (currentDomain.value === 'live-support') {
    if (liveSupportMode.value === 'strategy') {
      if (!canDelegateLivePolicyL3(session.bootstrap)) return []
      return [
        { kind: 'capability', label: '调整客户L3', description: '按客户授权调整当前直播间 L3 并生成草稿', insertText: '调整当前客户L3 ' },
        { kind: 'capability', label: '查看客户L3', description: '查看和讨论当前授权直播间的 L3', insertText: '查看当前客户L3 ' },
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
  void nextTick(() => drawerInputEl.value?.focus())
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
  return messages.value
    .filter((item) => item.domain === domain)
    .slice(-10)
    .map((item) => ({ role: item.role, text: item.text }))
}

async function scrollChatToBottom() {
  await nextTick()
  if (chatEl.value) {
    chatEl.value.scrollTop = chatEl.value.scrollHeight
  }
}

function pushAgentMessage(
  domain: AgentDomain,
  text: string,
  action?: SystemAgentActionPreview,
) {
  messages.value.push({ role: 'agent', domain, text, action })
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
    /(?:第一层|最底层|第?底层|底层规则|系统全局规则)/.test(compact)
  const explicitL2 =
    /只(?:修改|配置|处理)?L2/.test(upper) ||
    /当前L2/.test(upper) ||
    /L2(?:行业|规则|草稿)/.test(upper) ||
    /(?:第二层|行业默认规则)/.test(compact)

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
  internalLiveStrategyMode.value = mode === 'support' ? 'support' : 'policy'
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
        detail: { question: value, result },
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
  window.addEventListener('system-agent-live-support-context', handleLiveSupportContextEvent)
  window.addEventListener('live-policy-test-mode', handleLivePolicyTestModeEvent)
  window.addEventListener('resize', handleDockViewportResize)
  void loadAgentBranding()
  void loadSystemAgentContext()
})

watch(expanded, () => {
  if (!dockPosition.value) return
  void nextTick(keepDockInsideViewport)
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

onBeforeUnmount(() => {
  stopDockDrag()
  window.removeEventListener('system-agent:prefill', handleExternalPrefill)
  window.removeEventListener('system-config-updated', loadAgentBranding)
  window.removeEventListener('system-agent-live-strategy-mode', handleLiveStrategyModeEvent)
  window.removeEventListener('system-agent-live-support-context', handleLiveSupportContextEvent)
  window.removeEventListener('live-policy-test-mode', handleLivePolicyTestModeEvent)
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

async function send() {
  const rawValue = input.value.trim()
  if (!rawValue || busy.value) return

  const value = unescapeAgentTriggerText(rawValue.replace(/^\/+/, '').trim()).trim()
  if (!value) return

  const domain = currentDomain.value
  if (livePolicyTestMode.value.active && domain === 'live-policy-admin') {
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
      const response = await chatLiveRoomPolicyAgent(roomId, {
        message: value,
        history,
      })
      pushAgentMessage(
        domain,
        response.reply +
          (response.draft
            ? '\n\n已生成当前直播间 L3 草稿 V' +
              response.draft.version_no +
              '，仍需在直播策略工作台发布后才正式生效。'
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
          pushAgentMessage(domain, '当前账号不能代维护客户 L3。具备 L1 配置能力的账号即使获得客户授权也不能操作 L3；请由仅具备 L2 配置能力的运维员工在授权后处理。')
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
              ? '\n\n已生成当前授权直播间 L3 草稿 V' +
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
          <small>{{ assistantName }} · {{ contextLabel }} · {{ latestAgentMessage }}</small>
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
          发送
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
      </div>
    </div>

    <div
      v-if="drawerOpen && actor"
      class="system-agent-backdrop"
    >
      <aside class="system-agent-drawer">
        <header>
          <div>
            <span class="section-kicker">AI COPILOT · {{ contextLabel }}</span>
            <h3>{{ assistantName }}</h3>
            <p>{{ contextDescription }}</p>
          </div>
          <button class="icon-button" type="button" @click="drawerOpen = false">×</button>
        </header>

        <div class="system-agent-capabilities">
          <span>当前工作域：</span>
          <b>{{ contextLabel }}</b>
          <b v-for="item in capabilities" :key="item">{{ item }}</b>
        </div>

        <section ref="chatEl" class="system-agent-chat">
          <div v-if="visibleMessages.length === 0" class="system-agent-context-empty">
            <strong>{{ contextLabel }}</strong>
            <p>{{ contextDescription }}</p>
          </div>

          <article
            v-for="(message, index) in visibleMessages"
            :key="index"
            :class="['system-agent-message', message.role]"
          >
            <strong>{{ message.role === 'agent' ? assistantName : '我' }}</strong>
            <p>{{ message.text }}</p>

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
              <small>正在理解你的要求，并检查当前工作域与权限…</small>
            </div>
          </article>
        </section>

        <footer>
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
            发送
          </button>
        </footer>
      </aside>
    </div>
  </Teleport>
</template>

