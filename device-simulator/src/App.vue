<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

interface SpeechTask {
  speech_task_id: string
  room_id: number
  session_id: string
  kind: string
  label: string
  audio_url: string
  mime_type: string
  duration_ms: number
  start_ms?: number
  program_id?: string
  sequence?: number
  slot?: string
  started_at?: string
  created_at: string
}

interface RoomProgramSnapshot {
  program_id?: string
  room_id: number
  running: boolean
  suspended?: boolean
  resume_offset_ms?: number
  sequence?: number
  slot?: string
  task?: SpeechTask
  started_at?: string
  server_time: string
}

interface CoreTaskState {
  speech_task_id: string
  status: string
  event?: { receiver_id: string; status: string; progress_ms?: number; occurred_at?: string }
}

interface BrainTopic {
  Topic: string
  Count: number
  UniqueUsers: number
  LastSeenAt: string
  LastAnsweredAt?: string
  SampleQuestions?: string[]
}

interface BrainDebt {
  Value: number
  LastRaised?: string
  LastSpent?: string
  CooldownUntil?: string
}

interface BrainPin {
  ID: string
  At: string
  Kind: string
  Strategy?: string
  Topic?: string
  Key?: string
  MainlineUnit?: string
}

interface BrainView {
  RoomID: number
  GeneratedAt: string
  Intelligence: {
    Heat: string
    OnlineCount: number
    Entries30s: number
    Entries60s: number
    Chats30s: number
    Likes30s: number
    Follows30s: number
    Orders30s: number
    UniqueChatters30s: number
    QuestionCount30s: number
    NegativeFeedback30s: number
    QuestionPressure: number
    AudienceTurnover5m: number
    PreferAggregateQNA: boolean
    PreferOneToOneQNA: boolean
    TopTopics: BrainTopic[]
  }
  Timeline: {
    StartedAt: string
    HotWindowSeconds: number
    HotPins: BrainPin[]
    Debts: Record<string, BrainDebt>
    Summary: Record<string, unknown>
  }
  Director: {
    Progress: string
    Atmosphere: string
    TargetSeconds: number
    HumorLevel: number
    HumorInstruction?: string
    PromptDirectives?: string[]
    StrategyTrace: string[]
    Humanization: {
      Strategy: string
      Enabled: boolean
      Kind: string
      Delivery: string
      Instruction?: string
      Reason?: string
    }
    Resume: {
      Strategy: string
      Mode: string
      BridgeText?: string
      ResumeUnit?: string
      SkipUnits?: string[]
      AbandonCurrentPlan: boolean
      ReAnchor: boolean
      Reason?: string
    }
  }
}

interface RealTTSResult {
  text: string
  entry_mode: string
  entry_lead: string
  reply_core: string
  resume_tail: string
  final_text: string
  covered_topics: string[]
  covered_fact_ids: string[]
  skip_units: string[]
  resume_unit: string
  resume_mode: string
  text_digest: string
  bridge_digest: string
  target_seconds: number
  estimated_seconds: number
  agent_provider: string
  agent_model: string
  agent_latency_ms: number
  review_provider?: string
  review_model?: string
  review_latency_ms?: number
  tts_provider: string
  tts_model: string
  voice_id: string
  tts_rate: number
  audio_url?: string
  tts_latency_ms?: number
  continuity_quality?: {
    score: number
    level: string
    fatal?: boolean
    fatal_reason?: string
    summary: string
    issues: string[]
  }
  quality_attempts?: number
  actual_duration_ms?: number
  task_id?: string
}

interface MainlineSentence {
  id: string
  start_ms: number
  end_ms: number
  play_start_ms?: number
  play_end_ms?: number
  text: string
  topics?: string[]
}

interface MainlineSafePoint {
  id: string
  cut_ms: number
  score: number
  grade: string
  kind: string
  sentence_id: string
  left_preview: string
  next_preview: string
}

interface MainlineMap {
  duration_ms: number
  sentences: MainlineSentence[]
  safe_points: MainlineSafePoint[]
}

interface InteractionRecord {
  task_id: string
  room_id: number
  session_id: string
  created_at: string
  started_at?: string
  completed_at?: string
  status: string
  question: string
  strategy_chain: string[]
  director_progress: string
  director_atmosphere: string
  humanization: string
  entry_mode: string
  entry_lead: string
  resume_mode: string
  resume_unit: string
  resume_offset_ms?: number
  stop_ms: number
  stop_safe_point_id: string
  stop_grade: string
  stop_kind: string
  stop_text: string
  resume_text: string
  reply_core: string
  resume_tail: string
  final_text: string
  target_seconds: number
  tts_rate: number
  actual_duration_ms: number
  quality_score?: number
  quality_level?: string
  quality_summary?: string
  quality_issues?: string[]
  quality_attempts?: number
}

interface PendingQuestionItem {
  id: string
  room_id: number
  question: string
  topic?: string
  user_id?: string
  count: number
  status: 'PENDING' | 'CLAIMED'
  created_at: string
  last_seen_at: string
  expires_at: string
  available_at: string
  claimed_at?: string
  claim_until?: string
}


interface LearningCandidate {
  id: number
  evidence_type: string
  source_layer: string
  industry_code?: string
  tenant_id?: number
  room_id?: number
  question: string
  observed_reply?: string
  final_reply?: string
  feedback?: string
  recommended_layer: string
  recommendation_reason: string
  absorb_recommended: boolean
  confidence: number
  rule_title: string
  rule_text: string
  execution_mode: string
  status: string
  model_provider?: string
  model?: string
  latency_ms?: number
  learning_meta?: {
    summary?: string
    promotion_level?: string
    regression_cases?: string[]
  }
  adopted_version_id?: number
}

interface AgentModelOption {
  provider: string
  id: string
}

interface ManagementActor {
  user_id: number
  username: string
  display_name?: string
  role: string
  tenant_id?: number
}

interface ManagementTenant {
  id: number
  name: string
}

interface LiveAgentPlanTermVariant {
  id: number
  variant_text: string
  source: string
  confirmation_count: number
  last_confirmed_at: string
}

interface LiveAgentPlanTerm {
  id: number
  plan_id: number
  canonical_text: string
  term_type: string
  note?: string
  status: string
  variants: LiveAgentPlanTermVariant[]
}

interface LiveAgentPlan {
  id: number
  tenant_id: number
  name: string
  description?: string
  status: string
  room_count: number
  term_count: number
  room_ids?: number[]
  terms?: LiveAgentPlanTerm[]
  created_at: string
  updated_at: string
}

interface NormalizePlanTextResult {
  original_text: string
  normalized_text: string
  hot_terms: string[]
  applied: Array<{
    observed_text: string
    canonical_text: string
    term_id: number
  }>
}

const audioServiceURL = ref(localStorage.getItem('xl-audio-service-url') || 'http://127.0.0.1:8082')
const roomID = ref(Number(localStorage.getItem('xl-audio-room-id') || '1001'))
const receiverID = 'web-' + crypto.randomUUID().slice(0, 12)

const connected = ref(false)
const connecting = ref(false)
const playbackStatus = ref('未接听')
const coreStatus = ref('—')
const progressMS = ref(0)
const currentTask = ref<SpeechTask | null>(null)
const programRunning = ref(false)
const programSequence = ref(0)
const programSlot = ref('—')
const error = ref('')
const events = ref<string[]>([])
const brain = ref<BrainView | null>(null)
const brainError = ref('')
const brainBusy = ref(false)
const ttsQuestion = ref('这个鸡怎么吃？')
const ttsTargetSeconds = ref(10)
const agentProvider = ref('qwen')
const agentModel = ref('qwen3.8-flash')

const reviewerProvider = ref('qwen')
const reviewerModel = ref('deepseek-v3.2')
const agentModelOptions = ref<AgentModelOption[]>([])
const modelCatalogError = ref('')

const managementActor = ref<ManagementActor | null>(null)
const managementTenants = ref<ManagementTenant[]>([])
const managementUsername = ref('Admin')
const managementPassword = ref('')
const managementCaptcha = ref('')
const managementCaptchaURL = ref('')
const managementAuthBusy = ref(false)
const managementAuthError = ref('')
const planTenantID = ref(Number(localStorage.getItem('xl-plan-tenant-id') || '0'))
const liveAgentPlans = ref<LiveAgentPlan[]>([])
const selectedPlanID = ref(0)
const selectedPlan = ref<LiveAgentPlan | null>(null)
const planNameInput = ref('童子鸡直播智能体方案')
const planDescriptionInput = ref('同一套商品与主播逻辑，可由多个直播间共同使用。')
const planRoomID = ref(roomID.value)
const correctionObserved = ref('丑都养鸭子')
const correctionCanonical = ref('绸都杨鸭子')
const correctionType = ref('proper_noun')
const correctionNote = ref('人工确认的品牌/商家专有名词')
const normalizationInput = ref('欢迎来到丑都养鸭子直播间，今天给大家讲一下童子鸡。')
const normalizationResult = ref<NormalizePlanTextResult | null>(null)
const planLabBusy = ref(false)
const planLabError = ref('')
const planLabNotice = ref('')

const learningEvidenceType = ref('screenshot_review')
const learningSourceLayer = ref('L3')
const learningRoomID = ref(roomID.value)
const learningQuestion = ref('娃娃能吃吗？')
const learningObservedReply = ref('鸡是拿来吃的，不是拿来玩的娃娃。这童子鸡皮薄肉紧，处理干净就能下锅。')
const learningCorrectedReply = ref('')
const learningFeedback = ref('这里的“娃娃”在四川口语里是小孩，原回答把问题理解错了；意图理解错误不应该被评审高分放行。不要把“娃娃=小孩”写死，要结合上下文判断。')
const learningProvider = ref('qwen')
const learningModel = ref('deepseek-v3.2')
const learningBusy = ref(false)
const learningError = ref('')
const learningSummary = ref('')
const learningCandidates = ref<LearningCandidate[]>([])
const testLearnedDirectives = ref<string[]>([])

const quickTestQuestions = [
  '娃娃能吃吗？',
  '这个娃儿可以吃不？',
  '老人吃得动不？',
  '外地到不得哦？',
  '这个是不是现杀的？',
  '这个娃娃玩具多少钱？',
]
const ttsProvider = ref('qwen_audio')
const ttsModel = ref('qwen-audio-3.0-tts-plus')
const voiceID = ref('qwen-audio-3.0-tts-plus-yangduck-d4988b957aff42a89c44ec3ddb34d052')
const ttsRate = ref(Number(localStorage.getItem('xl-tts-rate') || '1'))
const ttsBusy = ref(false)
const ttsError = ref('')
const ttsResult = ref<RealTTSResult | null>(null)
const mainlineMap = ref<MainlineMap | null>(null)
const mainlineMapError = ref('')
const plannedSafePoint = ref<MainlineSafePoint | null>(null)
const interactionHistory = ref<InteractionRecord[]>([])
const latestRecord = ref<InteractionRecord | null>(null)
const pendingQuestions = ref<PendingQuestionItem[]>([])
const queueNotice = ref('')
const queueProcessing = ref(false)

let stream: EventSource | null = null
let player: HTMLAudioElement | null = null
let lastProgressSentAt = 0
let readySentForTask = ''
let corePollTimer: number | undefined
let roomSyncTimer: number | undefined
let brainPollTimer: number | undefined
let historyPollTimer: number | undefined
let questionQueueTimer: number | undefined
let reportQueue: Promise<void> = Promise.resolve()
let syncInFlight = false

const progressPercent = computed(() => {
  if (!currentTask.value?.duration_ms) return 0
  return Math.min(100, Math.max(0, (progressMS.value / currentTask.value.duration_ms) * 100))
})

const mainlineClockMS = computed(() => {
  if (currentTask.value?.kind === 'interaction_tts') {
    const stop = Number(plannedSafePoint.value?.cut_ms || 0)
    if (stop > 0) return Math.max(0, stop - 1)
  }
  return progressMS.value
})

const mainlineProgressPercent = computed(() => {
  const duration = mainlineMap.value?.duration_ms || currentTask.value?.duration_ms || 0
  if (!duration) return 0
  return Math.min(100, Math.max(0, (mainlineClockMS.value / duration) * 100))
})

const currentMainlineSentence = computed(() => {
  const items = mainlineMap.value?.sentences || []
  if (!items.length) return null
  const at = mainlineClockMS.value
  return items.find((item) => at >= item.start_ms && at < item.end_ms)
    || items.reduce((best, item) => Math.abs(item.start_ms - at) < Math.abs(best.start_ms - at) ? item : best, items[0])
})

function mainlineUnitsAfter(cutMS: number) {
  const items = mainlineMap.value?.sentences || []
  return items
    .filter((item) => Number(item.play_start_ms ?? item.start_ms) >= cutMS)
    .slice(0, 8)
    .map((item) => ({
      id: item.id,
      text: item.text,
      topics: item.topics || [],
      start_ms: Number(item.play_start_ms ?? item.start_ms),
    }))
}

const activeRecord = computed(() => interactionHistory.value[0] || latestRecord.value)

const monitorStage = computed<'MAINLINE' | 'INTERACTION' | 'RESUME'>(() => {
  if (currentTask.value?.kind === 'interaction_tts') return 'INTERACTION'
  const record = activeRecord.value
  if (
    record?.status === 'COMPLETED' &&
    currentTask.value?.kind === 'test_wav_program' &&
    record.resume_unit
  ) {
    const sentence = (mainlineMap.value?.sentences || []).find((item) => item.id === record.resume_unit)
    const resumeStart = Number(record.resume_offset_ms || sentence?.play_start_ms || sentence?.start_ms || 0)
    const resumeEnd = Number(sentence?.play_end_ms || sentence?.end_ms || (resumeStart + 6000))
    if (resumeStart > 0 && progressMS.value >= resumeStart && progressMS.value < resumeEnd) {
      return 'RESUME'
    }
  }
  return 'MAINLINE'
})

const monitorMainlineText = computed(() => {
  return currentMainlineSentence.value?.text || '等待主线开始播放'
})

const visiblePlannedStop = computed(() => {
  if (!ttsBusy.value && currentTask.value?.kind !== 'interaction_tts') return null
  return plannedSafePoint.value
})

const mainlineApproachingStop = computed(() => {
  const point = visiblePlannedStop.value
  const sentence = currentMainlineSentence.value
  if (!point || !sentence) return false
  if (point.sentence_id && sentence.id === point.sentence_id) return true
  return mainlineClockMS.value >= Math.max(0, point.cut_ms - 1800) && mainlineClockMS.value < point.cut_ms
})

const monitorInteractionText = computed(() => {
  if (ttsBusy.value && !ttsResult.value?.final_text) return '场控智能体正在生成临时插播话术…'
  return activeRecord.value?.final_text || ttsResult.value?.final_text || '等待临时插播'
})

const monitorResumeText = computed(() => {
  return activeRecord.value?.resume_text
    || plannedSafePoint.value?.next_preview
    || '等待确定回归主线位置'
})

const debtEntries = computed(() => {
  const items = Object.entries(brain.value?.Timeline?.Debts || {})
  const order = ['INTERACTION', 'QUESTION', 'LIKE_CTA', 'FOLLOW_CTA', 'CONVERSION']
  return items.sort(([a], [b]) => order.indexOf(a) - order.indexOf(b))
})

const heatLabel = computed(() => {
  const value = brain.value?.Intelligence?.Heat || '—'
  const labels: Record<string, string> = {
    COLD: '冷场',
    WARM: '正常',
    BUSY: '活跃',
    HOT: '高热',
    OVERHEATED: '爆量',
  }
  return labels[value] || value
})

function percentage(value: number | undefined) {
  return Math.max(0, Math.min(100, Math.round((value || 0) * 100)))
}

function decimal(value: number | undefined, digits = 2) {
  return Number(value || 0).toFixed(digits)
}

function formatMS(value: number | undefined) {
  const ms = Math.max(0, Number(value || 0))
  return (ms / 1000).toFixed(2) + ' 秒'
}

function strategyLabel(value: string) {
  const labels: Record<string, string> = {
    'progress.aggregate_questions': '聚合问题',
    'progress.question_hold': '优先答疑',
    'progress.conversion_momentum': '成交推进',
    'progress.continue_story': '继续主线',
    'atmosphere.low_traffic_interaction': '低流量互动',
    'atmosphere.cold_room_warm_up': '冷场拉互动',
    'atmosphere.order_momentum': '成交升温',
    'atmosphere.complaint_calm': '投诉收稳',
    'atmosphere.neutral': '保持节奏',
    'style.anchor_profile': '主播风格',
    'builtin.direct': '直接回主线',
    'builtin.bridge': '桥接后回主线',
    'builtin.fusion_skip': '融合后跳过重复',
    'builtin.cross_resume': '跨段回接',
    'builtin.re_anchor': '重新锚定主线',
    'builtin.switch_plan': '切换主线计划',
    'humanization.pause': '自然停顿',
    'humanization.filler': '自然口头衔接',
    'humanization.repeat_fragment': '轻微重复',
    'humanization.self_correction': '自然自我修正',
    'humanization.inversion': '口语倒装',
    'humanization.rehook': '重新抓回注意力',
    'humanization.throat_clear': '轻清嗓',
    'humanization.cough': '轻咳',
    'humanization.none': '不加仿真人动作',
  }
  return labels[value] || value.replace(/^progress\.|^atmosphere\.|^style\.|^humanization\./, '')
}

function resumeModeLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    DIRECT: '直接回到主线',
    BRIDGE: '桥接后回到主线',
    FUSION_SKIP: '融合回答并跳过重复内容',
    CROSS_RESUME: '跨到后面的合适位置继续',
    RE_ANCHOR: '重新交代上下文后继续',
    SWITCH_PLAN: '放弃原位置并切换主线计划',
  }
  if (!value) return '—'
  return labels[value] || value
}

function entryModeLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    DIRECT: '顺接',
    SOFT: '软接',
    HARD: '硬接',
  }
  if (!value) return '—'
  return labels[value] || value
}

function qualityLevelLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    HIGH: '高',
    MEDIUM_HIGH: '中高',
    MEDIUM: '中',
    LOW: '低',
  }
  if (!value) return '—'
  return labels[value] || value
}

function playbackStatusLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    DISPATCHED: '已下发',
    READY: '音频已就绪',
    PLAYING: '播放中',
    PROGRESS: '播放中',
    COMPLETED: '播放完成',
    FAILED: '播放失败',
    QUEUED: '等待播放',
  }
  if (!value) return '—'
  return labels[value] || value
}

function progressStrategyLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    HOLD_FOR_QNA: '暂停推进，优先答疑',
    CONTINUE_MAINLINE: '继续主线',
    CONVERSION_PUSH: '推进成交',
    RECOVER_MAINLINE: '恢复主线',
    SWITCH_TOPIC: '切换主题',
  }
  if (!value) return '—'
  return labels[value] || value
}

function atmosphereLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    NEUTRAL: '保持当前节奏',
    WARM_UP: '拉升互动',
    CELEBRATE: '轻庆祝',
    LIGHT_HUMOR: '轻松一点',
    CALM_AND_FOCUS: '收稳并聚焦',
  }
  if (!value) return '—'
  return labels[value] || value
}

function humanizationLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    NONE: '不加仿真人动作',
    PAUSE: '自然停顿',
    FILLER: '自然口头衔接',
    REPEAT_FRAGMENT: '轻微重复',
    SELF_CORRECTION: '自然自我修正',
    INVERSION: '口语倒装',
    REHOOK: '重新抓回注意力',
    THROAT_CLEAR: '轻清嗓',
    COUGH: '轻咳',
  }
  if (!value) return '—'
  return labels[value] || value
}

function safePointIDLabel(value: string | undefined) {
  if (!value) return '待判断'
  return value.startsWith('SP') ? '安全点 ' + value.slice(2) : value
}

function topicLabel(value: string | undefined) {
  const labels: Record<string, string> = {
    PRICE: '价格',
    SHIPPING: '发货',
    HOW_TO_EAT: '吃法',
    SPEC: '规格',
    ORIGIN: '产地',
    STORAGE: '保存',
    AFTER_SALE: '售后',
    QUALITY: '品质',
    COUPON: '优惠',
    ORDERING: '下单',
    GENERAL_CHAT: '普通互动',
    NEGATIVE: '负面反馈',
    DOUBT: '质疑',
  }
  if (!value) return '—'
  return labels[value] || value
}

function debtLabel(value: string) {
  const labels: Record<string, string> = {
    INTERACTION: '互动需求',
    QUESTION: '问题回答需求',
    LIKE_CTA: '点赞引导需求',
    FOLLOW_CTA: '关注引导需求',
    CONVERSION: '成交推进需求',
  }
  return labels[value] || value
}

function pinKindLabel(value: string) {
  const labels: Record<string, string> = {
    STRATEGY: '策略',
    QUESTION: '问题',
    ANSWER: '回答',
    CTA: '引导动作',
    HUMOR: '轻松表达',
    HUMANIZATION: '仿真人动作',
    BRIDGE: '桥接',
    RESUME: '回归主线',
    NODE_JUMP: '主线跳转',
    MAINLINE_TOPIC: '主线主题',
  }
  return labels[value] || value
}

async function loadMainlineMap() {
  try {
    const response = await fetch('/core/internal/v1/dev/audio/mainline-map', { cache: 'no-store' })
    const body = await response.json() as MainlineMap & { error?: string }
    if (!response.ok) throw new Error(body.error || 'HTTP ' + response.status)
    mainlineMap.value = body
    mainlineMapError.value = ''
  } catch (value) {
    mainlineMapError.value = value instanceof Error ? value.message : String(value)
  }
}

function mainlineContextBefore(cutMS: number) {
  const items = mainlineMap.value?.sentences || []
  const previous = items
    .filter((item) => Number(item.play_start_ms ?? item.start_ms) < cutMS)
    .slice(-2)
    .map((item) => item.text)
  return previous.join(' ')
}

function chooseSafePointWindow(now: number, minLead: number, maxLead: number, targetLead: number) {
  const map = mainlineMap.value
  if (!map?.safe_points?.length) return null
  const future = map.safe_points.filter((point) => point.cut_ms >= now + minLead && point.cut_ms <= now + maxLead)
  for (const grade of ['A', 'B', 'C']) {
    const candidates = future
      .filter((point) => point.grade === grade)
      .sort((a, b) => Math.abs((a.cut_ms - now) - targetLead) - Math.abs((b.cut_ms - now) - targetLead))
    if (candidates[0]) return candidates[0]
  }
  return future.sort((a, b) => Math.abs((a.cut_ms - now) - targetLead) - Math.abs((b.cut_ms - now) - targetLead))[0] || null
}

function chooseNextSafePoint() {
  const now = mainlineClockMS.value
  return chooseSafePointWindow(now, 30000, 55000, 42000)
    || chooseSafePointWindow(now, 24000, 50000, 36000)
}

function chooseSafePointAfterGeneration(preferred: MainlineSafePoint | null) {
  const now = progressMS.value
  if (preferred) {
    const lead = preferred.cut_ms - now
    if (lead >= 4000 && lead <= 33000) return preferred
  }
  return chooseSafePointWindow(now, 5000, 30000, 12000)
    || chooseSafePointWindow(now, 3000, 33000, 10000)
}

async function pollInteractionHistory() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) return
  try {
    const response = await fetch('/core/internal/v1/dev/audio/interactions?room_id=' + room, { cache: 'no-store' })
    if (!response.ok) return
    const body = await response.json() as { items?: InteractionRecord[] }
    interactionHistory.value = body.items || []
    if (interactionHistory.value[0]) latestRecord.value = interactionHistory.value[0]
  } catch {
    // Keep the last visible record during local service restarts.
  }
}

function startInteractionHistoryPolling() {
  if (historyPollTimer !== undefined) window.clearInterval(historyPollTimer)
  void pollInteractionHistory()
  historyPollTimer = window.setInterval(() => void pollInteractionHistory(), 900)
}

async function clearInteractionHistory() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) return
  await fetch('/core/internal/v1/dev/audio/interactions?room_id=' + room, { method: 'DELETE' })
  interactionHistory.value = []
  latestRecord.value = null
}

function scenarioLabel(value: string) {
  const labels: Record<string, string> = {
    cold: '冷场',
    warm: '普通',
    hot: '高热',
    complaint: '投诉',
  }
  return labels[value] || value
}

function addEvent(message: string) {
  events.value.unshift(new Date().toLocaleTimeString('zh-CN', { hour12: false }) + '  ' + message)
  events.value = events.value.slice(0, 12)
}

function baseURL() {
  return audioServiceURL.value.trim().replace(/\/$/, '')
}

function report(status: string, task = currentTask.value, message = '') {
  if (!task) return Promise.resolve()
  const taskID = task.speech_task_id
  const payload = {
    receiver_id: receiverID,
    status,
    progress_ms: Math.max(0, Math.round(progressMS.value)),
    error: message,
    occurred_at: new Date().toISOString(),
  }
  reportQueue = reportQueue.then(async () => {
    try {
      const response = await fetch(baseURL() + '/v1/tasks/' + encodeURIComponent(taskID) + '/events', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })
      if (!response.ok) throw new Error('HTTP ' + response.status)
    } catch (value) {
      error.value = '播放状态回传失败：' + (value instanceof Error ? value.message : String(value))
    }
  })
  return reportQueue
}

function stopPlayer() {
  if (!player) return
  player.pause()
  player.src = ''
  player.load()
  player = null
}

async function playTask(task: SpeechTask) {
	if (currentTask.value?.speech_task_id === task.speech_task_id && player) {
		return
	}
  stopPlayer()
  currentTask.value = task
  progressMS.value = Math.max(0, Math.min(task.start_ms || 0, task.duration_ms))
  if (task.program_id) {
    programRunning.value = true
    programSequence.value = task.sequence || 0
    programSlot.value = task.slot || '—'
  }
  readySentForTask = ''
  coreStatus.value = 'DISPATCHED'
  playbackStatus.value = '准备播放'
  error.value = ''
  addEvent('收到任务 ' + task.speech_task_id + ' · ' + task.label)

  const audio = new Audio(task.audio_url)
  audio.preload = 'auto'
  player = audio

  audio.addEventListener('canplaythrough', () => {
    if (readySentForTask === task.speech_task_id) return
    readySentForTask = task.speech_task_id
    playbackStatus.value = 'READY'
    addEvent('音频已就绪')
    void report('READY', task)
  })
  audio.addEventListener('playing', () => {
    playbackStatus.value = 'PLAYING'
    addEvent('开始播放')
    void report('PLAYING', task)
  })
  audio.addEventListener('timeupdate', () => {
    progressMS.value = Math.round(audio.currentTime * 1000)
    const now = performance.now()
    if (now - lastProgressSentAt >= 650) {
      lastProgressSentAt = now
      void report('PROGRESS', task)
    }
  })
  audio.addEventListener('ended', () => {
    progressMS.value = task.duration_ms
    playbackStatus.value = task.program_id ? '等待下一分段' : '播放完成'
    addEvent(task.program_id ? '当前分段播放完成，等待无缝切换' : '播放完成')
    void report('COMPLETED', task)
    if (!task.program_id) beginCorePolling(task.speech_task_id)
  })
  audio.addEventListener('error', () => {
    const message = '浏览器音频解码或加载失败'
    playbackStatus.value = 'FAILED'
    error.value = message
    addEvent('播放失败')
    void report('FAILED', task, message)
  })

  audio.load()
  if ((task.start_ms || 0) > 0) {
    if (audio.readyState < HTMLMediaElement.HAVE_METADATA) {
      await new Promise<void>((resolve) => {
        audio.addEventListener('loadedmetadata', () => resolve(), { once: true })
        audio.addEventListener('error', () => resolve(), { once: true })
      })
    }
    if (audio.duration > 0 && Number.isFinite(audio.duration)) {
      const seekSeconds = Math.min((task.start_ms || 0) / 1000, Math.max(0, audio.duration - 0.05))
      audio.currentTime = seekSeconds
      progressMS.value = Math.round(seekSeconds * 1000)
      addEvent('接入房间当前进度 ' + (seekSeconds).toFixed(1) + 's')
    }
  }
  try {
    await audio.play()
  } catch {
    playbackStatus.value = 'READY · 等待手动播放'
    error.value = '浏览器阻止了自动播放，请点击“播放当前任务”。'
    addEvent('等待手动播放')
  }
  beginCorePolling(task.speech_task_id)
}

async function resumeCurrent() {
  if (!player || !currentTask.value) return
  error.value = ''
  try {
    await player.play()
  } catch (value) {
    error.value = value instanceof Error ? value.message : String(value)
  }
}

async function syncRoomClock() {
  if (!connected.value || syncInFlight) return
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) return
  syncInFlight = true
  try {
    const response = await fetch(baseURL() + '/v1/rooms/' + room + '/sync', { cache: 'no-store' })
    if (!response.ok) return
    const data = await response.json() as RoomProgramSnapshot
    programRunning.value = Boolean(data.running)
    programSequence.value = data.sequence || 0
    programSlot.value = data.slot || '—'

    if (!data.running || !data.task) {
      if (currentTask.value?.program_id) {
        stopPlayer()
        currentTask.value = null
        progressMS.value = 0
        playbackStatus.value = '等待节目'
      }
      return
    }

    const task = data.task
    if (currentTask.value?.speech_task_id !== task.speech_task_id || !player) {
      await playTask(task)
      return
    }

    const expectedMS = Math.max(0, Math.min(task.start_ms || 0, Math.max(0, task.duration_ms - 50)))
    const localMS = Math.round(player.currentTime * 1000)
    const driftMS = expectedMS - localMS
    if (Math.abs(driftMS) >= 220 && player.readyState >= HTMLMediaElement.HAVE_METADATA) {
      player.currentTime = expectedMS / 1000
      progressMS.value = expectedMS
      addEvent('房间时钟校准 ' + (driftMS > 0 ? '+' : '') + driftMS + 'ms')
    }
  } catch {
    // SSE keeps the room usable during a short sync request failure.
  } finally {
    syncInFlight = false
  }
}

function startRoomSync() {
  if (roomSyncTimer !== undefined) window.clearInterval(roomSyncTimer)
  void syncRoomClock()
  roomSyncTimer = window.setInterval(() => void syncRoomClock(), 650)
}

async function startContinuousProgram() {
  const room = Number(roomID.value)
  if (!connected.value || !Number.isInteger(room) || room <= 0) return
  error.value = ''
  try {
    const response = await fetch('/core/internal/v1/dev/audio/program/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ room_id: room }),
    })
    const data = await response.json() as RoomProgramSnapshot & { error?: string }
    if (!response.ok) throw new Error(data.error || 'HTTP ' + response.status)
    programRunning.value = data.running
    programSequence.value = data.sequence || 0
    programSlot.value = data.slot || '—'
    addEvent('Core 已启动房间连续节目')
    await syncRoomClock()
  } catch (value) {
    error.value = '启动连续节目失败：' + (value instanceof Error ? value.message : String(value))
  }
}

async function stopContinuousProgram() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) return
  error.value = ''
  try {
    const response = await fetch('/core/internal/v1/dev/audio/program/stop', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ room_id: room }),
    })
    const data = await response.json() as RoomProgramSnapshot & { error?: string }
    if (!response.ok) throw new Error(data.error || 'HTTP ' + response.status)
    programRunning.value = false
    programSequence.value = data.sequence || 0
    programSlot.value = data.slot || '—'
    if (currentTask.value?.program_id) {
      stopPlayer()
      currentTask.value = null
      progressMS.value = 0
      playbackStatus.value = '等待节目'
    }
    addEvent('Core 已停止房间连续节目')
  } catch (value) {
    error.value = '停止连续节目失败：' + (value instanceof Error ? value.message : String(value))
  }
}


async function pollBrain() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0 || brainBusy.value) return
  brainBusy.value = true
  try {
    const response = await fetch('/core/internal/v1/rooms/' + room + '/brain', { cache: 'no-store' })
    if (!response.ok) throw new Error('HTTP ' + response.status)
    brain.value = await response.json() as BrainView
    brainError.value = ''
  } catch (value) {
    brainError.value = value instanceof Error ? value.message : String(value)
  } finally {
    brainBusy.value = false
  }
}

function startBrainPolling() {
  if (brainPollTimer !== undefined) window.clearInterval(brainPollTimer)
  void pollBrain()
  brainPollTimer = window.setInterval(() => void pollBrain(), 1000)
}

async function simulateBrainScenario(scenario: string) {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0 || brainBusy.value) return
  brainBusy.value = true
  try {
    const response = await fetch('/core/internal/v1/dev/rooms/' + room + '/brain/scenario', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ Scenario: scenario }),
    })
    const body = await response.json()
    if (!response.ok) throw new Error(body.error || 'HTTP ' + response.status)
    brain.value = body as BrainView
    brainError.value = ''
    addEvent('房间大脑切换到模拟场景：' + scenarioLabel(scenario))
  } catch (value) {
    brainError.value = '模拟场景失败：' + (value instanceof Error ? value.message : String(value))
  } finally {
    brainBusy.value = false
  }
}

async function resetBrain() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0 || brainBusy.value) return
  brainBusy.value = true
  try {
    const response = await fetch('/core/internal/v1/dev/rooms/' + room + '/brain/reset', { method: 'POST' })
    const body = await response.json()
    if (!response.ok) throw new Error(body.error || 'HTTP ' + response.status)
    brain.value = body as BrainView
    brainError.value = ''
    addEvent('已清空当前房间大脑状态')
  } catch (value) {
    brainError.value = '重置房间大脑失败：' + (value instanceof Error ? value.message : String(value))
  } finally {
    brainBusy.value = false
  }
}


function queueRemainingSeconds(item: PendingQuestionItem) {
  return Math.max(0, Math.ceil((Date.parse(item.expires_at) - Date.now()) / 1000))
}

async function loadQuestionQueue() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) return
  try {
    const response = await fetch('/core/internal/v1/rooms/' + room + '/questions', { cache: 'no-store' })
    if (!response.ok) return
    const body = await response.json() as { items?: PendingQuestionItem[] }
    pendingQuestions.value = body.items || []
  } catch {
    // Keep the last queue view during a short Core restart.
  }
}

async function enqueueQuestion(question: string, topic = '') {
  const room = Number(roomID.value)
  const value = question.trim()
  if (!Number.isInteger(room) || room <= 0 || !value) return null
  const response = await fetch('/core/internal/v1/rooms/' + room + '/questions', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ question: value, topic }),
  })
  const body = await response.json() as { item?: PendingQuestionItem; merged?: boolean; error?: string }
  if (!response.ok || !body.item) throw new Error(body.error || '问题进入待回答队列失败')
  await loadQuestionQueue()
  queueNotice.value = body.merged
    ? '同类问题已合并到待回答队列，按最新提问重新计算 2 分钟时效。'
    : '问题已进入待回答队列，场控会自己寻找后面的合适回答位置。'
  addEvent(body.merged ? '同类问题已合并到待回答队列' : '问题已进入待回答队列')
  return body.item
}

async function claimNextQuestion() {
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) return null
  try {
    const response = await fetch('/core/internal/v1/rooms/' + room + '/questions/claim', { method: 'POST' })
    if (!response.ok) return null
    const body = await response.json() as { item?: PendingQuestionItem | null }
    await loadQuestionQueue()
    return body.item || null
  } catch {
    return null
  }
}

async function completeQuestion(item: PendingQuestionItem) {
  try {
    await fetch(
      '/core/internal/v1/rooms/' + item.room_id + '/questions/' + encodeURIComponent(item.id) + '/complete',
      { method: 'POST' },
    )
  } catch {
    // The interaction already entered audio dispatch; a short Core restart must not undo it.
  } finally {
    await loadQuestionQueue()
  }
}

async function dropQuestion(item: PendingQuestionItem) {
  try {
    await fetch(
      '/core/internal/v1/rooms/' + item.room_id + '/questions/' + encodeURIComponent(item.id),
      { method: 'DELETE' },
    )
  } catch {
    // Core TTL cleanup is authoritative; the next list will prune stale data.
  } finally {
    await loadQuestionQueue()
  }
}

async function releaseQuestion(item: PendingQuestionItem, retryAfterSeconds = 15) {
  try {
    await fetch(
      '/core/internal/v1/rooms/' + item.room_id + '/questions/' + encodeURIComponent(item.id) + '/release',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ retry_after_seconds: retryAfterSeconds }),
      },
    )
  } catch {
    // Claim lease returns the item to PENDING automatically if Core briefly becomes unavailable.
  } finally {
    await loadQuestionQueue()
  }
}

async function submitQuestion() {
  const question = ttsQuestion.value.trim()
  if (!question) {
    ttsError.value = '请输入要测试的问题。'
    return
  }
  if (!connected.value) {
    ttsError.value = '请先点击“开始接听”。'
    return
  }
  try {
    ttsError.value = ''
    await enqueueQuestion(question)
    if (!programRunning.value) await startContinuousProgram()
    void processQuestionQueue()
  } catch (value) {
    ttsError.value = value instanceof Error ? value.message : String(value)
  }
}

async function processQuestionQueue() {
  if (
    queueProcessing.value ||
    ttsBusy.value ||
    !connected.value ||
    !programRunning.value ||
    currentTask.value?.kind !== 'test_wav_program'
  ) {
    return
  }
  queueProcessing.value = true
  try {
    await syncRoomClock()
    if (currentTask.value?.kind !== 'test_wav_program') return
    const safePoint = chooseNextSafePoint()
    if (!safePoint) {
      await loadQuestionQueue()
      return
    }
    const item = await claimNextQuestion()
    if (!item) return
    await generateRealInteraction(item)
  } finally {
    queueProcessing.value = false
  }
}

function startQuestionQueuePolling() {
  if (questionQueueTimer !== undefined) window.clearInterval(questionQueueTimer)
  void loadQuestionQueue()
  questionQueueTimer = window.setInterval(() => {
    void loadQuestionQueue()
    void processQuestionQueue()
  }, 1200)
}

async function generateRealInteraction(queueItem: PendingQuestionItem) {
  const room = Number(roomID.value)
  const question = queueItem.question.trim()
  if (!Number.isInteger(room) || room <= 0) {
    ttsError.value = '房间编号不正确。'
    await releaseQuestion(queueItem, 15)
    return
  }
  if (!question) {
    await dropQuestion(queueItem)
    return
  }
  if (ttsBusy.value) return

  ttsBusy.value = true
  ttsError.value = ''
  ttsResult.value = null

  try {
    if (!programRunning.value) {
      await startContinuousProgram()
      if (!programRunning.value) {
        throw new Error('主声音没有成功启动')
      }
    }

    localStorage.setItem('xl-tts-rate', String(ttsRate.value))
    await syncRoomClock()
    if (currentTask.value?.kind !== 'test_wav_program') {
      await releaseQuestion(queueItem, 10)
      queueNotice.value = '当前正在插播，这个问题继续留在队列里等待下一次回答窗口。'
      return
    }
    const safePoint = chooseNextSafePoint()
    if (!safePoint) {
      await releaseQuestion(queueItem, 12)
      queueNotice.value = '当前没有足够的插入空间，问题继续排队，场控会自动寻找后面的回答位置。'
      return
    }
    plannedSafePoint.value = safePoint

    const brainSnapshot = brain.value
    const mainlineText = mainlineContextBefore(safePoint.cut_ms) || safePoint.left_preview || currentMainlineSentence.value?.text || currentTask.value?.label || ''
    const nextUnits = mainlineUnitsAfter(safePoint.cut_ms)
    const managementResponse = await fetch('/management/internal/v1/dev/runtime/answer-tts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        question,
        target_seconds: Number(ttsTargetSeconds.value),
        agent_provider: agentProvider.value.trim(),
        agent_model: agentModel.value.trim(),
        review_provider: reviewerProvider.value.trim(),
        review_model: reviewerModel.value.trim(),
        tts_provider: ttsProvider.value.trim(),
        tts_model: ttsModel.value.trim(),
        voice_id: voiceID.value.trim(),
        tts_rate: Number(ttsRate.value),
        director_progress: brainSnapshot?.Director?.Progress || '',
        director_atmosphere: brainSnapshot?.Director?.Atmosphere || '',
        humanization_kind: brainSnapshot?.Director?.Humanization?.Enabled
          ? brainSnapshot.Director.Humanization.Kind
          : 'NONE',
        resume_mode_hint: brainSnapshot?.Director?.Resume?.Mode || '',
        current_mainline: mainlineText,
        next_mainline_units: nextUnits,
        top_topics: (brainSnapshot?.Intelligence?.TopTopics || []).slice(0, 5).map((item) => item.Topic),
        prompt_directives: [...(brainSnapshot?.Director?.PromptDirectives || []), ...testLearnedDirectives.value].slice(0, 24),
      }),
    })
    const generated = await managementResponse.json() as RealTTSResult & { error?: string }
    ttsResult.value = generated
    if (!managementResponse.ok) {
      throw new Error(generated.error || 'Agent/TTS HTTP ' + managementResponse.status)
    }
    if (!generated.audio_url) {
      throw new Error('语音合成没有返回音频地址')
    }

    if (Date.now() >= Date.parse(queueItem.expires_at)) {
      await dropQuestion(queueItem)
      queueNotice.value = '这个问题已经等待超过 2 分钟，已自动丢弃，不再回头回答。'
      addEvent('待回答问题超过 2 分钟，已自动丢弃')
      return
    }

    await syncRoomClock()
    if (currentTask.value?.kind !== 'test_wav_program' || !programRunning.value) {
      await releaseQuestion(queueItem, 10)
      queueNotice.value = '生成完成时主线暂时不可插入，问题已回到队列继续等待。'
      return
    }
    const actualSafePoint = chooseSafePointAfterGeneration(safePoint)
    if (!actualSafePoint) {
      await releaseQuestion(queueItem, 12)
      queueNotice.value = '本轮主线已经没有合适空间，问题已回到队列，下一轮继续寻找位置。'
      return
    }
    if (actualSafePoint.id !== safePoint.id) {
      addEvent(
        '生成耗时较长，插入点已自动顺延：' +
        safePointIDLabel(safePoint.id) +
        ' → ' +
        safePointIDLabel(actualSafePoint.id),
      )
    }
    plannedSafePoint.value = actualSafePoint

    const topic = generated.covered_topics?.[0]
      || brainSnapshot?.Intelligence?.TopTopics?.[0]?.Topic
      || ''
    const coreResponse = await fetch('/core/internal/v1/dev/audio/interaction', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        room_id: room,
        session_id: currentTask.value?.session_id || ('dev-room-' + room + '-continuous'),
        label: '真实互动语音 · ' + ttsTargetSeconds.value + '秒',
        audio_url: generated.audio_url,
        topic,
        resume_mode: generated.resume_mode,
        resume_unit: generated.resume_unit,
        covered_topics: generated.covered_topics || [],
        skip_units: generated.skip_units || [],
        text_digest: generated.text_digest,
        bridge_digest: generated.bridge_digest,
        switch_at_ms: actualSafePoint.cut_ms,
        question,
        strategy_chain: brainSnapshot?.Director?.StrategyTrace || [],
        director_progress: brainSnapshot?.Director?.Progress || '',
        director_atmosphere: brainSnapshot?.Director?.Atmosphere || '',
        humanization: brainSnapshot?.Director?.Humanization?.Enabled
          ? brainSnapshot.Director.Humanization.Kind
          : 'NONE',
        entry_mode: generated.entry_mode,
        entry_lead: generated.entry_lead,
        reply_core: generated.reply_core,
        resume_tail: generated.resume_tail,
        final_text: generated.final_text,
        target_seconds: generated.target_seconds,
        tts_rate: generated.tts_rate,
        quality_score: generated.continuity_quality?.score || 0,
        quality_level: generated.continuity_quality?.level || '',
        quality_summary: generated.continuity_quality?.summary || '',
        quality_issues: generated.continuity_quality?.issues || [],
        quality_attempts: generated.quality_attempts || 1,
      }),
    })
    const coreBody = await coreResponse.json() as { task?: SpeechTask; record?: InteractionRecord; error?: string }
    if (!coreResponse.ok || !coreBody.task) {
      throw new Error(coreBody.error || '核心服务插入互动音频失败')
    }

    ttsResult.value = {
      ...generated,
      actual_duration_ms: coreBody.task.duration_ms,
      task_id: coreBody.task.speech_task_id,
    }
    if (coreBody.record) {
      latestRecord.value = coreBody.record
    }
    await completeQuestion(queueItem)
    queueNotice.value = '队列问题已经安排并进入播音。'
    await pollInteractionHistory()
    addEvent(
      '真实语音已进入房间 · ' +
      resumeModeLabel(coreBody.record?.resume_mode || generated.resume_mode) +
      ' · 实际 ' +
      (coreBody.task.duration_ms / 1000).toFixed(1) +
      's',
    )
    window.setTimeout(() => void pollBrain(), 350)
  } catch (value) {
    ttsError.value = value instanceof Error ? value.message : String(value)
    await releaseQuestion(queueItem, 20)
    addEvent('本次回答未成功，问题继续留在队列')
  } finally {
    ttsBusy.value = false
  }
}

function disconnect() {
  stream?.close()
  stream = null
  connected.value = false
  connecting.value = false
  playbackStatus.value = '未接听'
  if (corePollTimer !== undefined) {
    window.clearInterval(corePollTimer)
    corePollTimer = undefined
  }
  if (roomSyncTimer !== undefined) {
    window.clearInterval(roomSyncTimer)
    roomSyncTimer = undefined
  }
  stopPlayer()
  addEvent('已停止接听')
}

function connect() {
  disconnect()
  const room = Number(roomID.value)
  if (!Number.isInteger(room) || room <= 0) {
    error.value = '请输入正确的房间编号。'
    return
  }
  localStorage.setItem('xl-audio-service-url', baseURL())
  localStorage.setItem('xl-audio-room-id', String(room))
  error.value = ''
  connecting.value = true
  playbackStatus.value = '连接中'

  const source = new EventSource(baseURL() + '/v1/rooms/' + room + '/stream?receiver_id=' + encodeURIComponent(receiverID))
  stream = source
  source.addEventListener('connected', () => {
    connected.value = true
    connecting.value = false
    playbackStatus.value = '等待节目'
    addEvent('接听通道已连接 · room ' + room)
    startRoomSync()
  })
  source.addEventListener('task', (event) => {
    try {
      const task = JSON.parse((event as MessageEvent).data) as SpeechTask
      void playTask(task)
    } catch {
      error.value = '收到无法识别的播音任务。'
    }
  })
  source.onerror = () => {
    connected.value = false
    connecting.value = false
    playbackStatus.value = '连接中断，等待重连'
  }
}

async function sendTestSound() {
  const room = Number(roomID.value)
  if (!connected.value || !Number.isInteger(room) || room <= 0) return
  error.value = ''
  try {
    const response = await fetch('/core/internal/v1/dev/audio/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ room_id: room, label: '小蓝播音通道第一阶段测试音', duration_ms: 2600 }),
    })
    const body = await response.json()
    if (!response.ok) throw new Error(body.error || 'HTTP ' + response.status)
    const task = body.task as SpeechTask
    coreStatus.value = body.core_status || 'DISPATCHED'
    addEvent('Core 已提交测试任务 ' + task.speech_task_id)
    beginCorePolling(task.speech_task_id)
  } catch (value) {
    error.value = 'Core 提交测试声音失败：' + (value instanceof Error ? value.message : String(value))
  }
}

function beginCorePolling(taskID: string) {
  if (corePollTimer !== undefined) window.clearInterval(corePollTimer)
  const poll = async () => {
    try {
      const response = await fetch('/core/internal/v1/dev/audio/tasks/' + encodeURIComponent(taskID))
      if (!response.ok) return
      const data = await response.json() as CoreTaskState
      coreStatus.value = data.status || 'DISPATCHED'
      if (data.status === 'COMPLETED' || data.status === 'FAILED') {
        if (corePollTimer !== undefined) {
          window.clearInterval(corePollTimer)
          corePollTimer = undefined
        }
      }
    } catch {
      // Keep the last Core state during a local service restart.
    }
  }
  void poll()
  corePollTimer = window.setInterval(poll, 550)
}


async function loadAgentModelCatalog() {
  modelCatalogError.value = ''
  try {
    const response = await fetch('/management/internal/v1/dev/runtime/models?provider=' + encodeURIComponent(agentProvider.value || 'qwen'), {
      cache: 'no-store',
    })
    const body = await response.json() as { items?: AgentModelOption[]; error?: string }
    if (!response.ok) throw new Error(body.error || 'HTTP ' + response.status)
    agentModelOptions.value = body.items || []
    if (agentModelOptions.value.some((item) => item.id === 'deepseek-v3.2')) {
      if (!reviewerModel.value) reviewerModel.value = 'deepseek-v3.2'
    }
  } catch (value) {
    modelCatalogError.value = value instanceof Error ? value.message : String(value)
  }
}

function revokeCaptchaURL() {
  if (managementCaptchaURL.value.startsWith('blob:')) {
    URL.revokeObjectURL(managementCaptchaURL.value)
  }
  managementCaptchaURL.value = ''
}

async function refreshManagementCaptcha() {
  revokeCaptchaURL()
  managementCaptcha.value = ''
  try {
    const response = await fetch('/management/api/v1/auth/captcha?ts=' + Date.now(), { cache: 'no-store' })
    if (!response.ok) throw new Error('验证码加载失败')
    managementCaptchaURL.value = URL.createObjectURL(await response.blob())
  } catch (value) {
    managementAuthError.value = value instanceof Error ? value.message : String(value)
  }
}

async function loadManagementTenants() {
  if (managementActor.value?.role !== 'platform_admin') return
  try {
    const response = await fetch('/management/api/v1/tenants', { cache: 'no-store' })
    if (!response.ok) return
    const body = await response.json() as { items?: ManagementTenant[] }
    managementTenants.value = body.items || []
    if (!planTenantID.value && managementTenants.value.length) {
      planTenantID.value = managementTenants.value[0].id
    }
  } catch {
    // The tenant ID can still be entered manually in the development workbench.
  }
}

async function checkManagementSession() {
  managementAuthError.value = ''
  try {
    const response = await fetch('/management/api/v1/bootstrap', { cache: 'no-store' })
    if (!response.ok) {
      managementActor.value = null
      await refreshManagementCaptcha()
      return
    }
    const body = await response.json() as { actor?: ManagementActor }
    managementActor.value = body.actor || null
    if (managementActor.value?.tenant_id) {
      planTenantID.value = managementActor.value.tenant_id
    }
    await loadManagementTenants()
    await loadLiveAgentPlans()
  } catch {
    managementActor.value = null
    await refreshManagementCaptcha()
  }
}

async function loginManagement() {
  if (!managementUsername.value.trim() || !managementPassword.value || !managementCaptcha.value.trim()) {
    managementAuthError.value = '请输入账号、密码和图形验证码。'
    return
  }
  managementAuthBusy.value = true
  managementAuthError.value = ''
  try {
    const response = await fetch('/management/api/v1/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        method: 'password',
        identifier: managementUsername.value.trim(),
        credential: managementPassword.value,
        captcha: managementCaptcha.value.trim(),
      }),
    })
    const body = await response.json() as { actor?: ManagementActor; error?: string }
    if (!response.ok || !body.actor) throw new Error(body.error || '登录失败')
    managementActor.value = body.actor
    managementPassword.value = ''
    managementCaptcha.value = ''
    if (body.actor.tenant_id) planTenantID.value = body.actor.tenant_id
    await loadManagementTenants()
    await loadLiveAgentPlans()
    planLabNotice.value = '管理身份已接入测试工作台。'
  } catch (value) {
    managementAuthError.value = value instanceof Error ? value.message : String(value)
    await refreshManagementCaptcha()
  } finally {
    managementAuthBusy.value = false
  }
}

async function logoutManagement() {
  try {
    await fetch('/management/api/v1/auth/logout', { method: 'POST' })
  } finally {
    managementActor.value = null
    liveAgentPlans.value = []
    selectedPlan.value = null
    selectedPlanID.value = 0
    await refreshManagementCaptcha()
  }
}

function planTenantQuery() {
  const tenant = Number(planTenantID.value)
  return Number.isInteger(tenant) && tenant > 0 ? '?tenant_id=' + tenant : ''
}

async function loadLiveAgentPlans() {
  if (!managementActor.value) return
  const tenant = Number(planTenantID.value)
  if (!Number.isInteger(tenant) || tenant <= 0) {
    planLabError.value = '请选择或填写 tenant_id。'
    return
  }
  localStorage.setItem('xl-plan-tenant-id', String(tenant))
  planLabBusy.value = true
  planLabError.value = ''
  try {
    const response = await fetch('/management/api/v1/live-agent-plans' + planTenantQuery(), { cache: 'no-store' })
    const body = await response.json() as { items?: LiveAgentPlan[]; error?: string }
    if (!response.ok) throw new Error(body.error || '读取直播智能体方案失败')
    liveAgentPlans.value = body.items || []
    if (selectedPlanID.value) {
      const exists = liveAgentPlans.value.some((item) => item.id === selectedPlanID.value)
      if (exists) await selectLiveAgentPlan(selectedPlanID.value)
      else {
        selectedPlanID.value = 0
        selectedPlan.value = null
      }
    }
  } catch (value) {
    planLabError.value = value instanceof Error ? value.message : String(value)
  } finally {
    planLabBusy.value = false
  }
}

async function createLiveAgentPlan() {
  const tenant = Number(planTenantID.value)
  if (!managementActor.value || !tenant) {
    planLabError.value = '请先登录并选择终端。'
    return
  }
  if (!planNameInput.value.trim()) {
    planLabError.value = '请输入直播智能体方案名称。'
    return
  }
  planLabBusy.value = true
  planLabError.value = ''
  try {
    const response = await fetch('/management/api/v1/live-agent-plans', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        tenant_id: tenant,
        name: planNameInput.value.trim(),
        description: planDescriptionInput.value.trim(),
      }),
    })
    const item = await response.json() as LiveAgentPlan & { error?: string }
    if (!response.ok) throw new Error(item.error || '创建直播智能体方案失败')
    selectedPlanID.value = item.id
    selectedPlan.value = item
    planLabNotice.value = '直播智能体方案已创建。业务知识以后跟方案走，不跟设备走。'
    await loadLiveAgentPlans()
  } catch (value) {
    planLabError.value = value instanceof Error ? value.message : String(value)
  } finally {
    planLabBusy.value = false
  }
}

async function selectLiveAgentPlan(planID: number) {
  if (!managementActor.value || !planID) return
  selectedPlanID.value = planID
  planLabError.value = ''
  try {
    const response = await fetch(
      '/management/api/v1/live-agent-plans/' + planID + planTenantQuery(),
      { cache: 'no-store' },
    )
    const item = await response.json() as LiveAgentPlan & { error?: string }
    if (!response.ok) throw new Error(item.error || '读取方案详情失败')
    selectedPlan.value = item
  } catch (value) {
    planLabError.value = value instanceof Error ? value.message : String(value)
  }
}

async function bindSelectedPlanToRoom() {
  if (!selectedPlanID.value) {
    planLabError.value = '请先选择一个直播智能体方案。'
    return
  }
  const targetRoom = Number(planRoomID.value)
  if (!Number.isInteger(targetRoom) || targetRoom <= 0) {
    planLabError.value = '请输入正确的直播间 ID。'
    return
  }
  planLabBusy.value = true
  planLabError.value = ''
  try {
    const response = await fetch(
      '/management/api/v1/live-agent-plans/' + selectedPlanID.value + '/room-bindings',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tenant_id: Number(planTenantID.value), room_id: targetRoom }),
      },
    )
    const item = await response.json() as LiveAgentPlan & { error?: string }
    if (!response.ok) throw new Error(item.error || '绑定直播间失败')
    selectedPlan.value = item
    planLabNotice.value = '直播间 #' + targetRoom + ' 已使用这套直播智能体方案。'
    await loadLiveAgentPlans()
  } catch (value) {
    planLabError.value = value instanceof Error ? value.message : String(value)
  } finally {
    planLabBusy.value = false
  }
}

async function savePlanCorrection() {
  if (!selectedPlanID.value) {
    planLabError.value = '请先选择一个直播智能体方案。'
    return
  }
  if (!correctionCanonical.value.trim()) {
    planLabError.value = '请输入人工确认后的正确术语。'
    return
  }
  planLabBusy.value = true
  planLabError.value = ''
  try {
    const response = await fetch(
      '/management/api/v1/live-agent-plans/' + selectedPlanID.value + '/terms',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tenant_id: Number(planTenantID.value),
          canonical_text: correctionCanonical.value.trim(),
          observed_text: correctionObserved.value.trim(),
          term_type: correctionType.value,
          note: correctionNote.value.trim(),
          source: 'manual_correction',
        }),
      },
    )
    const body = await response.json() as LiveAgentPlanTerm & { error?: string }
    if (!response.ok) throw new Error(body.error || '保存人工纠错失败')
    planLabNotice.value = '已记住：' + correctionObserved.value.trim() + ' → ' + correctionCanonical.value.trim() + '。同方案直播间共享。'
    await selectLiveAgentPlan(selectedPlanID.value)
  } catch (value) {
    planLabError.value = value instanceof Error ? value.message : String(value)
  } finally {
    planLabBusy.value = false
  }
}

async function normalizePlanTextPreview() {
  if (!selectedPlanID.value) {
    planLabError.value = '请先选择一个直播智能体方案。'
    return
  }
  planLabBusy.value = true
  planLabError.value = ''
  normalizationResult.value = null
  try {
    const response = await fetch(
      '/management/api/v1/live-agent-plans/' + selectedPlanID.value + '/normalize-text',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tenant_id: Number(planTenantID.value),
          text: normalizationInput.value,
        }),
      },
    )
    const body = await response.json() as NormalizePlanTextResult & { error?: string }
    if (!response.ok) throw new Error(body.error || '术语纠错预览失败')
    normalizationResult.value = body
  } catch (value) {
    planLabError.value = value instanceof Error ? value.message : String(value)
  } finally {
    planLabBusy.value = false
  }
}

function learningLayerLabel(value: string) {
  const labels: Record<string, string> = {
    L1: '规则层',
    L2: '行业层',
    L3: '用户层',
  }
  return labels[value] || value || '待判断'
}

function loadRecentInteractionIntoLearning() {
  const record = activeRecord.value
  if (!record) {
    learningError.value = '当前还没有可载入的互动记录。'
    return
  }
  learningRoomID.value = record.room_id || roomID.value
  learningQuestion.value = record.question || ''
  learningObservedReply.value = record.final_text || record.reply_core || ''
  learningCorrectedReply.value = ''
  learningFeedback.value = ''
  learningEvidenceType.value = 'bad_answer'
  learningError.value = ''
}

async function submitLearningEvidence() {
  if (!managementActor.value) {
    learningError.value = '请先在上面的直播智能体方案区域接入测试管理身份。'
    return
  }
  if (
    !learningQuestion.value.trim() &&
    !learningObservedReply.value.trim() &&
    !learningCorrectedReply.value.trim() &&
    !learningFeedback.value.trim()
  ) {
    learningError.value = '请至少填写一项学习证据。'
    return
  }
  learningBusy.value = true
  learningError.value = ''
  learningSummary.value = ''
  learningCandidates.value = []
  try {
    const response = await fetch('/management/api/v1/live/policy-learning/evidence', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        evidence_type: learningEvidenceType.value,
        source_ref: selectedPlanID.value ? 'live-agent-plan:' + selectedPlanID.value : 'dev-workbench',
        source_layer: learningSourceLayer.value,
        industry_code: 'general',
        room_id: Number(learningRoomID.value) || 0,
        question: learningQuestion.value.trim(),
        observed_reply: learningObservedReply.value.trim(),
        corrected_reply: learningCorrectedReply.value.trim(),
        feedback: learningFeedback.value.trim(),
        learning_provider: learningProvider.value.trim(),
        learning_model: learningModel.value.trim(),
      }),
    })
    const body = await response.json() as {
      summary?: string
      items?: LearningCandidate[]
      model?: string
      latency_ms?: number
      error?: string
    }
    if (!response.ok) throw new Error(body.error || '学习 Agent 分析失败')
    learningSummary.value = body.summary || '学习 Agent 已完成分析。'
    learningCandidates.value = body.items || []
    if (!learningCandidates.value.length) {
      learningSummary.value += ' 本次没有生成可沉淀的语言策略候选。'
    }
  } catch (value) {
    learningError.value = value instanceof Error ? value.message : String(value)
  } finally {
    learningBusy.value = false
  }
}

function loadLearningCandidateIntoTest(candidate: LearningCandidate) {
  const rule = candidate.rule_text.trim()
  if (!rule) return
  if (!testLearnedDirectives.value.includes(rule)) {
    testLearnedDirectives.value = [...testLearnedDirectives.value, rule].slice(-12)
  }
  learningSummary.value = '候选 #' + candidate.id + ' 已临时加载到研发测试上下文。只影响本页后续测试，不会写入正式策略。'
}

function removeTestLearnedDirective(rule: string) {
  testLearnedDirectives.value = testLearnedDirectives.value.filter((item) => item !== rule)
}

async function adoptLearningCandidate(candidate: LearningCandidate) {
  if (!managementActor.value) return
  learningBusy.value = true
  learningError.value = ''
  try {
    const response = await fetch(
      '/management/api/v1/live/policy-learning/candidates/' + candidate.id + '/adopt',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          target_layer: candidate.recommended_layer,
          industry_code: candidate.industry_code || 'general',
          room_id: Number(learningRoomID.value) || candidate.room_id || 0,
          review_note: '研发测试工作台人工采纳；仅进入策略草稿，未自动发布。',
        }),
      },
    )
    const body = await response.json() as {
      candidate?: LearningCandidate
      draft?: { id?: number }
      error?: string
    }
    if (!response.ok || !body.candidate) {
      throw new Error(body.error || '采纳学习候选失败')
    }
    learningCandidates.value = learningCandidates.value.map((item) =>
      item.id === candidate.id ? body.candidate as LearningCandidate : item,
    )
    learningSummary.value = '候选 #' + candidate.id + ' 已进入' + learningLayerLabel(candidate.recommended_layer) +
      '草稿。当前仍未发布，不会直接改变正式直播行为。'
  } catch (value) {
    learningError.value = value instanceof Error ? value.message : String(value)
  } finally {
    learningBusy.value = false
  }
}

async function rejectLearningCandidate(candidate: LearningCandidate) {
  learningBusy.value = true
  learningError.value = ''
  try {
    const response = await fetch(
      '/management/api/v1/live/policy-learning/candidates/' + candidate.id + '/reject',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ review_note: '研发测试工作台人工放弃。' }),
      },
    )
    const body = await response.json() as LearningCandidate & { error?: string }
    if (!response.ok) throw new Error(body.error || '放弃学习候选失败')
    learningCandidates.value = learningCandidates.value.map((item) =>
      item.id === candidate.id ? body : item,
    )
  } catch (value) {
    learningError.value = value instanceof Error ? value.message : String(value)
  } finally {
    learningBusy.value = false
  }
}

onMounted(() => {
  startBrainPolling()
  startInteractionHistoryPolling()
  startQuestionQueuePolling()
  void loadMainlineMap()
  void loadAgentModelCatalog()
  void checkManagementSession()
})
onBeforeUnmount(() => {
  if (brainPollTimer !== undefined) window.clearInterval(brainPollTimer)
  if (historyPollTimer !== undefined) window.clearInterval(historyPollTimer)
  if (questionQueueTimer !== undefined) window.clearInterval(questionQueueTimer)
  revokeCaptchaURL()
  disconnect()
})
</script>

<template>
  <main class="receiver-shell">
    <header class="receiver-header">
      <div>
        <p class="eyebrow">研发测试工作台 · 仅开发环境</p>
        <h1>小蓝直播智能体研发工作台</h1>
        <p class="subtitle">测试端允许透明观察底层机制；正式交付端只暴露业务能力，不下发内部调度、评分、Prompt 与诊断链。</p>
      </div>
      <div class="connection-pill" :class="{ online: connected }"><span></span>{{ connected ? '已接听' : connecting ? '连接中' : '未接听' }}</div>
    </header>

    <section class="control-card">
      <div class="field-grid">
        <label><span>播音分发层</span><input v-model="audioServiceURL" :disabled="connected" /></label>
        <label><span>测试房间</span><input v-model.number="roomID" type="number" min="1" :disabled="connected" /></label>
        <label><span>本机接收端</span><input :value="receiverID" disabled /></label>
      </div>
      <div class="actions">
        <button v-if="!connected" class="primary" type="button" :disabled="connecting" @click="connect">{{ connecting ? '连接中…' : '开始接听' }}</button>
        <button v-else class="danger" type="button" @click="disconnect">停止接听</button>
        <button v-if="!programRunning" class="test-button" type="button" :disabled="!connected" @click="startContinuousProgram">开始长时间连续播放</button>
        <button v-else class="danger" type="button" :disabled="!connected" @click="stopContinuousProgram">停止长时间连续播放</button>
        <button class="test-button" type="button" :disabled="!connected" @click="sendTestSound">发送测试声音</button>
        <button v-if="currentTask && playbackStatus.includes('等待手动')" class="secondary" type="button" @click="resumeCurrent">播放当前任务</button>
      </div>
    </section>

    <p v-if="error" class="error-banner">{{ error }}</p>

    <section class="plan-lab-card">
      <div class="plan-lab-head">
        <div>
          <p class="eyebrow">研发测试工作台 · 业务资产实验</p>
          <h2>直播智能体方案</h2>
          <p>方案承载业务知识、术语纠错和后续学习；设备只负责播放，直播间只负责独立运行。</p>
        </div>
        <div class="plan-lab-badges">
          <span>方案可复用</span>
          <span>多直播间共享</span>
          <span>交付端隐藏底层机制</span>
        </div>
      </div>

      <div v-if="!managementActor" class="plan-auth-panel">
        <div class="plan-auth-copy">
          <strong>接入测试管理身份</strong>
          <span>这里使用正式权限接口，不在测试页绕过权限。账号、密码只用于当前本机测试会话。</span>
        </div>
        <div class="plan-auth-form">
          <label><span>账号</span><input v-model="managementUsername" autocomplete="username" /></label>
          <label><span>密码</span><input v-model="managementPassword" type="password" autocomplete="current-password" /></label>
          <label class="captcha-field">
            <span>图形验证码</span>
            <div>
              <input v-model="managementCaptcha" maxlength="5" @keyup.enter="loginManagement" />
              <button type="button" class="captcha-image-button" @click="refreshManagementCaptcha">
                <img v-if="managementCaptchaURL" :src="managementCaptchaURL" alt="验证码" />
                <b v-else>刷新验证码</b>
              </button>
            </div>
          </label>
          <button class="primary" type="button" :disabled="managementAuthBusy" @click="loginManagement">
            {{ managementAuthBusy ? '登录中…' : '接入测试身份' }}
          </button>
        </div>
        <p v-if="managementAuthError" class="brain-error">{{ managementAuthError }}</p>
      </div>

      <template v-else>
        <div class="plan-session-bar">
          <div>
            <span>当前身份</span>
            <strong>{{ managementActor.display_name || managementActor.username }}</strong>
            <small>{{ managementActor.role }} · user #{{ managementActor.user_id }}</small>
          </div>
          <label v-if="managementActor.role === 'platform_admin' && managementTenants.length">
            <span>测试终端</span>
            <select v-model.number="planTenantID" @change="loadLiveAgentPlans">
              <option v-for="tenant in managementTenants" :key="tenant.id" :value="tenant.id">
                #{{ tenant.id }} · {{ tenant.name }}
              </option>
            </select>
          </label>
          <label v-else>
            <span>tenant_id</span>
            <input v-model.number="planTenantID" type="number" min="1" :disabled="!!managementActor.tenant_id" @change="loadLiveAgentPlans" />
          </label>
          <button class="secondary compact" type="button" @click="loadLiveAgentPlans">刷新方案</button>
          <button class="secondary compact" type="button" @click="logoutManagement">退出身份</button>
        </div>

        <p v-if="planLabError" class="brain-error">{{ planLabError }}</p>
        <p v-if="planLabNotice" class="plan-lab-notice">{{ planLabNotice }}</p>

        <div class="plan-lab-grid">
          <article class="plan-lab-panel plan-list-panel">
            <div class="panel-title">
              <div>
                <span>业务知识资产</span>
                <strong>方案列表</strong>
              </div>
              <b>{{ liveAgentPlans.length }} 套</b>
            </div>

            <div class="plan-create-box">
              <input v-model="planNameInput" placeholder="方案名称，例如：童子鸡直播智能体方案" />
              <textarea v-model="planDescriptionInput" rows="2" placeholder="这套方案卖什么、适合哪些直播间"></textarea>
              <button class="primary" type="button" :disabled="planLabBusy" @click="createLiveAgentPlan">新建直播智能体方案</button>
            </div>

            <div v-if="liveAgentPlans.length" class="plan-list">
              <button
                v-for="plan in liveAgentPlans"
                :key="plan.id"
                type="button"
                class="plan-list-item"
                :class="{ active: selectedPlanID === plan.id }"
                @click="selectLiveAgentPlan(plan.id)"
              >
                <div>
                  <strong>{{ plan.name }}</strong>
                  <span>{{ plan.description || '暂无说明' }}</span>
                </div>
                <small>{{ plan.room_count }} 个直播间 · {{ plan.term_count }} 个术语</small>
              </button>
            </div>
            <p v-else class="empty compact-empty">当前终端还没有直播智能体方案。</p>
          </article>

          <article class="plan-lab-panel plan-detail-panel">
            <template v-if="selectedPlan">
              <div class="panel-title">
                <div>
                  <span>当前选择</span>
                  <strong>{{ selectedPlan.name }}</strong>
                </div>
                <b>方案 #{{ selectedPlan.id }}</b>
              </div>

              <div class="plan-principle-strip">
                <span>换设备：知识不丢</span>
                <span>换直播间：方案继续用</span>
                <span>同方案：纠错共享</span>
              </div>

              <div class="plan-subsection">
                <div class="plan-subsection-head">
                  <div><small>运行关系</small><strong>绑定直播间</strong></div>
                  <span>一个方案可服务多个直播间</span>
                </div>
                <div class="plan-inline-form">
                  <input v-model.number="planRoomID" type="number" min="1" placeholder="直播间 ID" />
                  <button type="button" class="test-button" :disabled="planLabBusy" @click="bindSelectedPlanToRoom">绑定</button>
                </div>
                <div v-if="selectedPlan.room_ids?.length" class="room-chip-row">
                  <span v-for="id in selectedPlan.room_ids" :key="id">直播间 #{{ id }}</span>
                </div>
                <p v-else class="empty compact-empty">还没有绑定直播间。</p>
              </div>

              <div class="plan-subsection">
                <div class="plan-subsection-head">
                  <div><small>人工纠错记忆</small><strong>术语学习</strong></div>
                  <span>只属于这套直播智能体方案</span>
                </div>
                <div class="correction-grid">
                  <label><span>原始误识别</span><input v-model="correctionObserved" placeholder="丑都养鸭子" /></label>
                  <label><span>人工确认正确词</span><input v-model="correctionCanonical" placeholder="绸都杨鸭子" /></label>
                  <label>
                    <span>术语类型</span>
                    <select v-model="correctionType">
                      <option value="proper_noun">专有名词</option>
                      <option value="brand">品牌/商家名</option>
                      <option value="product">商品名</option>
                      <option value="person">人物/主播名</option>
                      <option value="regional">地域词</option>
                    </select>
                  </label>
                  <label><span>备注</span><input v-model="correctionNote" /></label>
                </div>
                <button class="primary correction-save" type="button" :disabled="planLabBusy" @click="savePlanCorrection">
                  人工确认并让方案记住
                </button>

                <div v-if="selectedPlan.terms?.length" class="term-memory-list">
                  <div v-for="term in selectedPlan.terms" :key="term.id" class="term-memory-item">
                    <strong>{{ term.canonical_text }}</strong>
                    <span>{{ term.term_type }}</span>
                    <div>
                      <em v-for="variant in term.variants" :key="variant.id">
                        {{ variant.variant_text }} → {{ term.canonical_text }} · 已确认 {{ variant.confirmation_count }} 次
                      </em>
                    </div>
                  </div>
                </div>
              </div>

              <div class="plan-subsection">
                <div class="plan-subsection-head">
                  <div><small>未来 ASR 接入前先验证</small><strong>转写纠错体验</strong></div>
                  <span>原始文字 → 方案纠错 → 最终文字</span>
                </div>
                <textarea v-model="normalizationInput" rows="3"></textarea>
                <button class="test-button" type="button" :disabled="planLabBusy" @click="normalizePlanTextPreview">模拟方案纠错</button>
                <div v-if="normalizationResult" class="normalization-result">
                  <div><span>原始转写</span><p>{{ normalizationResult.original_text }}</p></div>
                  <div class="normalized"><span>方案纠正后</span><p>{{ normalizationResult.normalized_text }}</p></div>
                  <small>当前热词：{{ normalizationResult.hot_terms.join(' / ') || '—' }}</small>
                </div>
              </div>
            </template>
            <div v-else class="plan-empty-state">
              <strong>先选择一套直播智能体方案</strong>
              <span>然后体验多直播间绑定、人工术语纠错和持久记忆。</span>
            </div>
          </article>
        </div>
      </template>
    </section>


    <section class="learning-lab-card">
      <div class="learning-lab-head">
        <div>
          <p class="eyebrow">自我学习 V1 · 人工可控</p>
          <h2>让系统从一次错误里学到可复用规律</h2>
          <p>原始证据可以是错误回答、截图复盘、人工纠正或优质样本。学习 Agent 只生成候选；没有人工采纳和后续发布，不会直接改正式直播。</p>
        </div>
        <div class="learning-guard">
          <strong>证据 → 候选 → 人工采纳 → 草稿 → 发布</strong>
          <span>禁止 AI 自己直接改正式规则</span>
        </div>
      </div>

      <div class="learning-toolbar">
        <label>
          <span>证据类型</span>
          <select v-model="learningEvidenceType">
            <option value="screenshot_review">截图复盘</option>
            <option value="bad_answer">错误回答</option>
            <option value="corrected_answer">人工纠正</option>
            <option value="gold_sample">优秀样本</option>
            <option value="post_live_review">直播复盘</option>
            <option value="manual_feedback">人工反馈</option>
          </select>
        </label>
        <label>
          <span>来源层</span>
          <select v-model="learningSourceLayer">
            <option value="L1">规则层</option>
            <option value="L2">行业层</option>
            <option value="L3">用户层</option>
          </select>
        </label>
        <label><span>直播间</span><input v-model.number="learningRoomID" type="number" min="1" /></label>
        <label><span>学习模型通道</span><input v-model="learningProvider" /></label>
        <label>
          <span>学习 Agent 模型</span>
          <select v-model="learningModel">
            <option v-if="!agentModelOptions.some((item) => item.id === learningModel)" :value="learningModel">{{ learningModel }}</option>
            <option v-for="item in agentModelOptions" :key="'learn-' + item.id" :value="item.id">{{ item.id }}</option>
          </select>
        </label>
        <button class="secondary" type="button" @click="loadRecentInteractionIntoLearning">载入最近互动</button>
      </div>

      <div class="learning-evidence-grid">
        <label>
          <span>原始问题 / 场景</span>
          <textarea v-model="learningQuestion" rows="3"></textarea>
        </label>
        <label>
          <span>当时真实回答（可以是错的）</span>
          <textarea v-model="learningObservedReply" rows="3"></textarea>
        </label>
        <label>
          <span>人工修正后的回复（可空）</span>
          <textarea v-model="learningCorrectedReply" rows="3" placeholder="没有完整正确答案也可以，只写你的反馈即可。"></textarea>
        </label>
        <label>
          <span>你的判断 / 为什么错</span>
          <textarea v-model="learningFeedback" rows="3"></textarea>
        </label>
      </div>

      <div class="learning-actions">
        <button class="primary" type="button" :disabled="learningBusy || !managementActor" @click="submitLearningEvidence">
          {{ learningBusy ? '学习 Agent 分析中…' : '生成学习候选' }}
        </button>
        <span v-if="!managementActor">先在“直播智能体方案”区域接入测试管理身份。</span>
        <span v-else>当前只生成候选，不会自动生效。</span>
      </div>

      <p v-if="learningError" class="brain-error">{{ learningError }}</p>
      <p v-if="learningSummary" class="learning-summary">{{ learningSummary }}</p>

      <div v-if="learningCandidates.length" class="learning-candidate-list">
        <article v-for="candidate in learningCandidates" :key="candidate.id" class="learning-candidate">
          <div class="learning-candidate-head">
            <div>
              <span>候选 #{{ candidate.id }} · {{ learningLayerLabel(candidate.recommended_layer) }}</span>
              <strong>{{ candidate.rule_title }}</strong>
            </div>
            <div class="learning-score">
              <b>{{ candidate.confidence }}%</b>
              <small>{{ candidate.learning_meta?.promotion_level || 'candidate' }}</small>
            </div>
          </div>

          <p class="learning-rule-text">{{ candidate.rule_text }}</p>
          <p class="learning-reason">{{ candidate.recommendation_reason }}</p>

          <div v-if="candidate.learning_meta?.regression_cases?.length" class="learning-regression">
            <span>自动回归题：</span>
            <em v-for="item in candidate.learning_meta.regression_cases" :key="item">{{ item }}</em>
          </div>

          <div class="learning-candidate-footer">
            <div>
              <span>模型：{{ candidate.model || learningModel }}</span>
              <span>耗时：{{ candidate.latency_ms || 0 }} ms</span>
              <span>状态：{{ candidate.status === 'pending' ? '待人工决定' : candidate.status === 'adopted' ? '已采纳到草稿' : '已放弃' }}</span>
            </div>
            <div>
              <button class="secondary compact" type="button" @click="loadLearningCandidateIntoTest(candidate)">
                临时加载到测试
              </button>
              <template v-if="candidate.status === 'pending'">
                <button class="primary compact" type="button" :disabled="learningBusy" @click="adoptLearningCandidate(candidate)">
                  采纳到草稿
                </button>
                <button class="secondary compact" type="button" :disabled="learningBusy" @click="rejectLearningCandidate(candidate)">
                  放弃
                </button>
              </template>
              <strong v-else-if="candidate.status === 'adopted'">尚未发布 · 正式行为未自动改变</strong>
            </div>
          </div>
        </article>
      </div>
    </section>

    <section class="continuity-workbench">
      <div class="wb-head">
        <div>
          <p class="eyebrow">主线接续测试</p>
          <h2>主线接续测试</h2>
        </div>
        <div class="wb-state">
          <span :class="{ active: programRunning }">{{ programRunning ? '主线运行中' : '主线未启动' }}</span>
          <span>房间 #{{ roomID }}</span>
        </div>
      </div>

      <p v-if="mainlineMapError" class="brain-error">主线时间轴：{{ mainlineMapError }}</p>

      <div class="wb-mainline">
        <div class="wb-section-title">
          <strong>实时主监控</strong>
          <span>{{ monitorStage === 'INTERACTION' ? '临时插播中' : monitorStage === 'RESUME' ? '正在接回主线' : playbackStatusLabel(playbackStatus) }}</span>
        </div>
        <div class="wb-mainline-progress">
          <i :style="{ width: mainlineProgressPercent + '%' }"></i>
        </div>
        <div class="wb-mainline-meta">
          <span>{{ formatMS(mainlineClockMS) }}</span>
          <span>{{ formatMS(mainlineMap?.duration_ms || currentTask?.duration_ms) }}</span>
        </div>

        <div class="wb-monitor-flow">
          <article class="wb-monitor-card mainline" :class="{ active: monitorStage !== 'INTERACTION', frozen: monitorStage === 'INTERACTION' }">
            <div class="wb-monitor-card-head">
              <span>主线</span>
              <b>{{ monitorStage === 'INTERACTION' ? '主线暂停' : '字幕跟随中' }}</b>
            </div>

            <div class="wb-mainline-live">
              <small>当前播放文字</small>
              <p :class="{ 'stop-coming': mainlineApproachingStop }">{{ monitorMainlineText }}</p>
            </div>

            <div v-if="visiblePlannedStop" class="wb-planned-stop">
              <div>
                <small>计划停止位置</small>
                <b>{{ safePointIDLabel(visiblePlannedStop.id) }} · {{ formatMS(visiblePlannedStop.cut_ms) }}</b>
              </div>
              <p>{{ visiblePlannedStop.left_preview }}</p>
            </div>
          </article>

          <div class="wb-monitor-arrow" :class="{ active: monitorStage === 'INTERACTION' }">→</div>

          <article class="wb-monitor-card interaction" :class="{ active: monitorStage === 'INTERACTION' }">
            <div class="wb-monitor-card-head">
              <span>临时插播</span>
              <b>{{ ttsBusy ? '生成中' : currentTask?.kind === 'interaction_tts' ? '正在读' : '等待' }}</b>
            </div>
            <p>{{ monitorInteractionText }}</p>
            <small v-if="activeRecord?.entry_mode || ttsResult?.entry_mode">
              {{ entryModeLabel(activeRecord?.entry_mode || ttsResult?.entry_mode) }}
              <template v-if="activeRecord?.entry_lead || ttsResult?.entry_lead">
                · {{ activeRecord?.entry_lead || ttsResult?.entry_lead }}
              </template>
            </small>
          </article>

          <div class="wb-monitor-arrow" :class="{ active: monitorStage === 'RESUME' }">→</div>

          <article class="wb-monitor-card resume" :class="{ active: monitorStage === 'RESUME' }">
            <div class="wb-monitor-card-head">
              <span>接回主线</span>
              <b>{{ monitorStage === 'RESUME' ? '正在接话' : '等待' }}</b>
            </div>
            <p>{{ monitorResumeText }}</p>
            <small v-if="activeRecord?.resume_mode || ttsResult?.resume_mode">
              {{ resumeModeLabel(activeRecord?.resume_mode || ttsResult?.resume_mode) }}
              <template v-if="activeRecord?.resume_unit"> · {{ activeRecord.resume_unit }}</template>
            </small>
          </article>
        </div>

        <div class="wb-monitor-cycle">
          <span>正常播放：左侧持续更新</span>
          <b>插播时：左 → 中 → 右 → 左</b>
        </div>

        <div class="wb-question-queue" :class="{ waiting: pendingQuestions.length > 0 }">
          <div class="wb-question-queue-head">
            <strong>待回答队列 · {{ pendingQuestions.length }}</strong>
            <span>超过 2 分钟自动丢弃，不再回头回答</span>
          </div>
          <div v-if="pendingQuestions.length" class="wb-question-queue-list">
            <div v-for="item in pendingQuestions.slice(0, 4)" :key="item.id" class="wb-question-queue-item">
              <p>{{ item.question }}</p>
              <div>
                <span>{{ item.status === 'CLAIMED' ? '已安排回答窗口' : '等待合适插入位置' }}</span>
                <b>剩余 {{ queueRemainingSeconds(item) }} 秒</b>
                <em v-if="item.count > 1">同类 ×{{ item.count }}</em>
              </div>
            </div>
          </div>
          <small v-else>当前没有积压问题。</small>
        </div>
      </div>

      <div class="wb-process">
        <div class="wb-step">
          <span class="wb-step-label">场控智能体使用策略</span>
          <div v-if="activeRecord?.strategy_chain?.length" class="wb-strategy-chain">
            <template v-for="(item, index) in activeRecord.strategy_chain" :key="item + index">
              <b>{{ strategyLabel(item) }}</b>
              <em v-if="index < activeRecord.strategy_chain.length - 1">→</em>
            </template>
          </div>
          <p v-else class="wb-placeholder">{{ ttsBusy ? '场控智能体正在生成策略和话术…' : '等待插入指令' }}</p>
        </div>

        <div class="wb-step wb-resume-strategy">
          <div>
            <span class="wb-step-label">切入策略</span>
            <strong>{{ entryModeLabel(activeRecord?.entry_mode || ttsResult?.entry_mode) }}</strong>
            <small v-if="activeRecord?.entry_lead || ttsResult?.entry_lead">
              进入话术：{{ activeRecord?.entry_lead || ttsResult?.entry_lead }}
            </small>
          </div>
          <div>
            <span class="wb-step-label">回归策略</span>
            <strong>{{ resumeModeLabel(activeRecord?.resume_mode || ttsResult?.resume_mode) }}</strong>
            <small v-if="activeRecord?.resume_unit">回到语义段：{{ activeRecord.resume_unit }}</small>
          </div>
        </div>

        <div class="wb-step">
          <span class="wb-step-label">最终送入语音合成的文案</span>
          <p class="wb-final-copy">{{ activeRecord?.final_text || ttsResult?.final_text || '生成后显示完整语音合成文案' }}</p>
          <small v-if="activeRecord">
            目标 {{ activeRecord.target_seconds }} 秒 · 语速 {{ decimal(activeRecord.tts_rate, 1) }} 倍 · 实际 {{ formatMS(activeRecord.actual_duration_ms) }}
          </small>
        </div>

        <div
          v-if="ttsResult?.continuity_quality"
          class="wb-quality-step"
          :class="{ pass: ttsResult.continuity_quality.score >= 80, fail: ttsResult.continuity_quality.score < 80 }"
        >
          <div class="wb-quality-head">
            <span class="wb-step-label">接续质量闸门</span>
            <strong>
              {{ qualityLevelLabel(ttsResult.continuity_quality.level) }}
              · {{ ttsResult.continuity_quality.score }} 分
            </strong>
          </div>
          <p>{{ ttsResult.continuity_quality.summary }}</p>
          <p v-if="ttsResult.continuity_quality.fatal" class="wb-quality-fatal">
            一票拦截：{{ ttsResult.continuity_quality.fatal_reason || '问题意图或事实正确性不合格' }}
          </p>
          <div v-if="ttsResult.continuity_quality.issues?.length" class="wb-quality-issues">
            <span v-for="issue in ttsResult.continuity_quality.issues" :key="issue">{{ issue }}</span>
          </div>
          <small>
            共评判 {{ ttsResult.quality_attempts || 1 }} 次 ·
            {{ !ttsResult.continuity_quality.fatal && ttsResult.continuity_quality.score >= 80 ? '达到中高质量，允许进入语音合成' : '未达到中高质量，已拦截语音合成' }}
          </small>
        </div>

      </div>

      <div class="wb-command">
        <label class="wb-question">
          <span>问题 / 插入指令</span>
          <textarea v-model="ttsQuestion" rows="3" placeholder="例如：这个鸡怎么吃？"></textarea>
        </label>

        <div class="wb-command-options">
          <div>
            <span>目标回答总时长</span>
            <div class="duration-buttons">
              <button type="button" :class="{ active: ttsTargetSeconds === 10 }" @click="ttsTargetSeconds = 10">10 秒</button>
              <button type="button" :class="{ active: ttsTargetSeconds === 20 }" @click="ttsTargetSeconds = 20">20 秒</button>
            </div>
          </div>
          <label>
            <span>语音合成语速</span>
            <select v-model.number="ttsRate">
              <option :value="0.8">0.8 倍</option>
              <option :value="0.9">0.9 倍</option>
              <option :value="1">1.0 倍</option>
              <option :value="1.1">1.1 倍</option>
              <option :value="1.2">1.2 倍</option>
              <option :value="1.3">1.3 倍</option>
              <option :value="1.5">1.5 倍</option>
            </select>
          </label>
          <button
            class="wb-insert-button"
            type="button"
            :disabled="!connected"
            @click="submitQuestion"
          >
            提交问题
          </button>
        </div>
      </div>

      <p v-if="ttsError" class="brain-error">{{ ttsError }}</p>
      <p v-if="queueNotice" class="wb-queue-notice">{{ queueNotice }}</p>

      <details class="wb-advanced">
        <summary>高级测试设置</summary>
        <div class="wb-advanced-grid">
          <label><span>大模型服务商</span><input v-model="agentProvider" /></label>
          <label><span>大模型型号</span><input v-model="agentModel" /></label>
          <label><span>语音合成服务商</span><input v-model="ttsProvider" /></label>
          <label><span>语音合成模型</span><input v-model="ttsModel" /></label>
          <label class="voice-field"><span>克隆音色编号</span><input v-model="voiceID" /></label>
        </div>
        <div class="scenario-row">
          <span>模拟房间：</span>
          <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('cold')">冷场</button>
          <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('warm')">普通</button>
          <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('hot')">高热</button>
          <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('complaint')">投诉</button>
        </div>
      </details>
    </section>

    <section class="continuity-history">
      <div class="wb-head compact-head">
        <div>
          <p class="eyebrow">接续历史</p>
          <h2>接续记录</h2>
        </div>
        <button class="secondary compact" type="button" :disabled="!interactionHistory.length" @click="clearInteractionHistory">清空本页记录</button>
      </div>
      <p v-if="!interactionHistory.length" class="wb-placeholder history-empty">完成一次插播后，这里会保留主线停止文字、插入文字和回归文字，方便逐条评判。</p>
      <article v-for="(record, index) in interactionHistory.slice(0, 12)" :key="record.task_id" class="history-record">
        <div class="history-record-head">
          <strong>#{{ interactionHistory.length - index }} · {{ record.question }}</strong>
          <span>{{ new Date(record.created_at).toLocaleTimeString('zh-CN', { hour12: false }) }} · {{ playbackStatusLabel(record.status) }}</span>
        </div>
        <div class="history-flow">
          <div class="history-mainline">
            <small>主线停止 · {{ safePointIDLabel(record.stop_safe_point_id) }} · {{ formatMS(record.stop_ms) }}</small>
            <p>{{ record.stop_text || '—' }}</p>
          </div>
          <div class="history-insert">
            <small>插入 · {{ entryModeLabel(record.entry_mode) }} → {{ resumeModeLabel(record.resume_mode) }}</small>
            <p>{{ record.final_text }}</p>
          </div>
          <div class="history-mainline resume">
            <small>回归主线</small>
            <p>{{ record.resume_text || '—' }}</p>
          </div>
        </div>

        <div v-if="record.quality_score" class="history-quality">
          <strong>系统接续自评：{{ qualityLevelLabel(record.quality_level) }} · {{ record.quality_score }} 分</strong>
          <span>{{ record.quality_summary || '—' }}</span>
          <small v-if="record.quality_attempts">共评判 {{ record.quality_attempts }} 次</small>
        </div>
        <div v-if="record.strategy_chain?.length" class="history-strategies">
          <span v-for="item in record.strategy_chain" :key="item">{{ strategyLabel(item) }}</span>
        </div>
      </article>
    </section>

    <section class="status-grid">
      <article><span>本机播放器</span><strong>{{ playbackStatusLabel(playbackStatus) }}</strong></article>
      <article><span>核心服务确认状态</span><strong>{{ playbackStatusLabel(coreStatus) }}</strong></article>
      <article><span>当前房间</span><strong>#{{ roomID }}</strong></article>
      <article><span>房间连续节目</span><strong>{{ programRunning ? '分段 #' + programSequence + ' · 槽 ' + programSlot : '未运行' }}</strong></article>
    </section>


    <section class="brain-card">
      <div class="brain-head">
        <div>
          <p class="eyebrow">直播间大脑 · 影子模式</p>
          <h2>直播间大脑</h2>
          <p class="segment-line">先观察、不自动发声：公屏聚类 → 房间状态 → 策略需求积累 → 场控决策。</p>
        </div>
        <div class="brain-head-actions">
          <span class="heat-badge" :class="'heat-' + (brain?.Intelligence?.Heat || 'unknown').toLowerCase()">{{ heatLabel }}</span>
          <button class="secondary compact" type="button" :disabled="brainBusy" @click="pollBrain">刷新</button>
        </div>
      </div>

      <p v-if="brainError" class="brain-error">{{ brainError }}</p>

      <div class="scenario-row">
        <span>快速模拟：</span>
        <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('cold')">冷场</button>
        <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('warm')">普通</button>
        <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('hot')">高热</button>
        <button type="button" :disabled="brainBusy" @click="simulateBrainScenario('complaint')">投诉</button>
        <button class="danger-soft" type="button" :disabled="brainBusy" @click="resetBrain">清空</button>
      </div>

      <div v-if="brain" class="brain-kpi-grid">
        <article><span>当前在线</span><strong>{{ brain.Intelligence.OnlineCount }}</strong><small>30秒进房 {{ brain.Intelligence.Entries30s }}</small></article>
        <article><span>30秒弹幕</span><strong>{{ brain.Intelligence.Chats30s }}</strong><small>问题 {{ brain.Intelligence.QuestionCount30s }}</small></article>
        <article><span>问题压力</span><strong>{{ decimal(brain.Intelligence.QuestionPressure) }}</strong><small>{{ brain.Intelligence.PreferAggregateQNA ? '聚合回答' : brain.Intelligence.PreferOneToOneQNA ? '一对一互动' : '保持主线' }}</small></article>
        <article><span>5分钟换血率</span><strong>{{ decimal(brain.Intelligence.AudienceTurnover5m) }}×</strong><small>热记忆 {{ Math.round(brain.Timeline.HotWindowSeconds / 60) }} 分钟</small></article>
        <article><span>点赞 / 关注</span><strong>{{ brain.Intelligence.Likes30s }} / {{ brain.Intelligence.Follows30s }}</strong><small>近30秒</small></article>
        <article><span>订单 / 负反馈</span><strong>{{ brain.Intelligence.Orders30s }} / {{ brain.Intelligence.NegativeFeedback30s }}</strong><small>近30秒</small></article>
      </div>

      <div v-if="brain" class="brain-columns">
        <article class="brain-panel">
          <div class="panel-title"><span>公屏语义分类</span><strong>公屏语义桶</strong></div>
          <div v-if="brain.Intelligence.TopTopics?.length" class="topic-list">
            <div v-for="topic in brain.Intelligence.TopTopics" :key="topic.Topic" class="topic-row">
              <div><strong>{{ topicLabel(topic.Topic) }}</strong><span>{{ topic.SampleQuestions?.[0] || '—' }}</span></div>
              <div class="topic-count"><b>{{ topic.Count }}</b><small>{{ topic.UniqueUsers }}人</small></div>
            </div>
          </div>
          <p v-else class="empty compact-empty">还没有可聚合的问题。</p>
        </article>

        <article class="brain-panel director-panel">
          <div class="panel-title"><span>实时场控判断</span><strong>当前导演判断</strong></div>
          <div class="director-grid">
            <div><span>直播进度</span><strong>{{ progressStrategyLabel(brain.Director.Progress) }}</strong></div>
            <div><span>氛围策略</span><strong>{{ atmosphereLabel(brain.Director.Atmosphere) }}</strong></div>
            <div><span>目标口播</span><strong>{{ brain.Director.TargetSeconds }} 秒</strong></div>
            <div><span>回接策略</span><strong>{{ resumeModeLabel(brain.Director.Resume?.Mode) }}</strong></div>
            <div><span>仿真人行为</span><strong>{{ brain.Director.Humanization?.Enabled ? humanizationLabel(brain.Director.Humanization.Kind) : '不加仿真人动作' }}</strong></div>
            <div><span>幽默等级</span><strong>{{ brain.Director.HumorLevel }}</strong></div>
          </div>
          <div v-if="brain.Director.PromptDirectives?.length" class="directive-list">
            <p v-for="item in brain.Director.PromptDirectives" :key="item">{{ item }}</p>
          </div>
          <p v-if="brain.Director.Humanization?.Reason" class="decision-note">仿真人：{{ brain.Director.Humanization.Reason }}</p>
          <p v-if="brain.Director.Resume?.Reason" class="decision-note">回接：{{ brain.Director.Resume.Reason }}</p>
        </article>
      </div>

      <div v-if="brain" class="brain-columns lower">
        <article class="brain-panel">
          <div class="panel-title"><span>策略需求积累</span><strong>当前需求强度</strong></div>
          <div class="debt-list">
            <div v-for="[name, debt] in debtEntries" :key="name" class="debt-row">
              <div><span>{{ debtLabel(name) }}</span><b>{{ percentage(debt.Value) }}%</b></div>
              <div class="debt-track"><i :style="{ width: percentage(debt.Value) + '%' }"></i></div>
            </div>
          </div>
        </article>

        <article class="brain-panel">
          <div class="panel-title"><span>最近策略钉</span><strong>最近时间钉</strong></div>
          <div v-if="brain.Timeline.HotPins?.length" class="pin-list">
            <p v-for="pin in [...brain.Timeline.HotPins].reverse().slice(0, 8)" :key="pin.ID">
              <b>{{ pinKindLabel(pin.Kind) }}</b>
              <span>{{ pin.Strategy ? strategyLabel(pin.Strategy) : pin.Topic ? topicLabel(pin.Topic) : pin.Key || '—' }}</span>
              <small>{{ new Date(pin.At).toLocaleTimeString('zh-CN', { hour12: false }) }}</small>
            </p>
          </div>
          <p v-else class="empty compact-empty">当前热记忆窗口内还没有策略钉。</p>
        </article>
      </div>
    </section>


    <section class="tts-lab-card">
      <div class="brain-head">
        <div>
          <p class="eyebrow">智能体 + 语音合成 + 调度器</p>
          <h2>真实互动语音实验台</h2>
          <p class="segment-line">场控智能体一次生成回答正文 + 桥接尾巴 → 语音合成 → 核心服务 → 房间统一切入 → 服务端自动恢复主线。</p>
        </div>
        <span class="tts-live-badge">{{ programRunning ? '主线运行中' : '主线未启动' }}</span>
      </div>

      <div class="tts-question-row">
        <label>
          <span>你来发问</span>
          <textarea v-model="ttsQuestion" rows="3" placeholder="例如：这个鸡怎么吃？"></textarea>
        </label>
        <div class="duration-box">
          <span>目标回答总时长</span>
          <div class="duration-buttons">
            <button type="button" :class="{ active: ttsTargetSeconds === 10 }" @click="ttsTargetSeconds = 10">10 秒</button>
            <button type="button" :class="{ active: ttsTargetSeconds === 20 }" @click="ttsTargetSeconds = 20">20 秒</button>
          </div>
          <input v-model.number="ttsTargetSeconds" type="number" min="3" max="60" />
        </div>
      </div>

      <div v-if="testLearnedDirectives.length" class="test-learning-context">
        <div>
          <strong>本页临时学习上下文</strong>
          <span>仅用于研发 A/B；刷新后可重新选择，正式策略不受影响。</span>
        </div>
        <div>
          <button v-for="rule in testLearnedDirectives" :key="rule" type="button" @click="removeTestLearnedDirective(rule)">
            {{ rule }} ×
          </button>
        </div>
      </div>

      <div class="quick-question-bank">
        <span>口语/歧义快速测试：</span>
        <button v-for="item in quickTestQuestions" :key="item" type="button" @click="ttsQuestion = item">
          {{ item }}
        </button>
      </div>

      <div class="agent-pair-grid">
        <article>
          <div class="agent-pair-head">
            <span>场控 Agent</span>
            <strong>负责理解问题与生成回答</strong>
          </div>
          <label><span>服务商</span><input v-model="agentProvider" @change="loadAgentModelCatalog" /></label>
          <label>
            <span>模型</span>
            <select v-model="agentModel">
              <option v-if="!agentModelOptions.some((item) => item.id === agentModel)" :value="agentModel">{{ agentModel }}</option>
              <option v-for="item in agentModelOptions" :key="'director-' + item.id" :value="item.id">{{ item.id }}</option>
            </select>
          </label>
        </article>
        <article>
          <div class="agent-pair-head">
            <span>评审 Agent</span>
            <strong>独立检查意图、事实与接续质量</strong>
          </div>
          <label><span>服务商</span><input v-model="reviewerProvider" /></label>
          <label>
            <span>模型</span>
            <select v-model="reviewerModel">
              <option v-if="!agentModelOptions.some((item) => item.id === reviewerModel)" :value="reviewerModel">{{ reviewerModel }}</option>
              <option v-for="item in agentModelOptions" :key="'review-' + item.id" :value="item.id">{{ item.id }}</option>
            </select>
          </label>
        </article>
      </div>
      <p v-if="modelCatalogError" class="model-catalog-note">模型目录读取失败：{{ modelCatalogError }}；仍可手工填写后端支持的模型。</p>

      <div class="tts-config-grid compact-config">
        <label><span>语音合成服务商</span><input v-model="ttsProvider" /></label>
        <label><span>语音合成模型</span><input v-model="ttsModel" /></label>
        <label>
          <span>语音合成语速</span>
          <select v-model.number="ttsRate">
            <option :value="0.8">0.8 倍 · 偏慢</option>
            <option :value="0.9">0.9 倍 · 稍慢</option>
            <option :value="1">1.0 倍 · 正常</option>
            <option :value="1.1">1.1 倍 · 稍快</option>
            <option :value="1.2">1.2 倍 · 较快</option>
            <option :value="1.3">1.3 倍 · 快</option>
            <option :value="1.5">1.5 倍 · 很快</option>
          </select>
        </label>
        <label class="voice-field"><span>克隆音色编号</span><input v-model="voiceID" /></label>
      </div>

      <div class="tts-actions">
        <button
          class="primary"
          type="button"
          :disabled="!connected"
          @click="submitQuestion"
        >
          提交到待回答队列
        </button>
        <span>场控与评审模型已解耦；模型目录从当前百炼账户动态读取，不在前端写死。</span>
      </div>

      <p v-if="ttsError" class="brain-error">{{ ttsError }}</p>

      <div v-if="ttsResult" class="tts-result-grid">
        <article class="speech-result reply">
          <div class="panel-title"><span>回答正文</span><strong>回答正文</strong></div>
          <p>{{ ttsResult.reply_core }}</p>
        </article>
        <article class="speech-result bridge">
          <div class="panel-title"><span>回归桥接</span><strong>提前生成的桥接</strong></div>
          <p>{{ ttsResult.resume_tail || '直接回到主线：本次无需额外桥接尾巴' }}</p>
        </article>
        <article class="speech-result meta">
          <div class="tts-meta-grid">
            <div><span>回接建议</span><strong>{{ resumeModeLabel(ttsResult.resume_mode) }}</strong></div>
            <div><span>目标 / 估算 / 实际</span><strong>{{ ttsResult.target_seconds }} 秒 / {{ decimal(ttsResult.estimated_seconds, 1) }} 秒 / {{ decimal((ttsResult.actual_duration_ms || 0) / 1000, 1) }} 秒</strong></div>
            <div><span>智能体延迟</span><strong>{{ ttsResult.agent_latency_ms }} 毫秒</strong></div>
            <div><span>评审模型</span><strong>{{ ttsResult.review_model || reviewerModel }} · {{ ttsResult.review_latency_ms || 0 }} 毫秒</strong></div>
            <div><span>语音合成延迟</span><strong>{{ ttsResult.tts_latency_ms }} 毫秒</strong></div>
            <div><span>语音合成语速</span><strong>{{ decimal(ttsResult.tts_rate, 1) }} 倍</strong></div>
            <div><span>覆盖主题</span><strong>{{ ttsResult.covered_topics?.join(' / ') || '—' }}</strong></div>
            <div><span>跳过 / 回接语义段</span><strong>{{ ttsResult.skip_units?.join(', ') || '—' }} → {{ ttsResult.resume_unit || '待程序判定' }}</strong></div>
          </div>
          <p class="final-speech"><b>最终送入语音合成：</b>{{ ttsResult.final_text }}</p>
        </article>
      </div>
    </section>

    <section class="program-card">
      <div class="program-head">
        <div>
          <p class="eyebrow">房间节目</p>
          <h2>{{ currentTask?.label || '等待核心服务下发节目' }}</h2>
          <p v-if="currentTask?.program_id" class="segment-line">内部正在播放分段 #{{ currentTask.sequence }} · 槽 {{ currentTask.slot }}，外部仍是一条连续房间声音</p>
        </div>
        <code>{{ currentTask?.speech_task_id || '暂无任务' }}</code>
      </div>
      <div class="progress-track"><span :style="{ width: progressPercent + '%' }"></span></div>
      <div class="program-meta"><span>{{ Math.round(progressMS / 100) / 10 }} 秒</span><span>{{ currentTask ? Math.round(currentTask.duration_ms / 100) / 10 + ' 秒' : '—' }}</span></div>
    </section>

    <section class="event-card">
      <div class="event-head">
        <div><p class="eyebrow">播放状态</p><h2>播放回执</h2></div>
        <span>音频就绪 / 播放中 / 进度回传 / 播放完成 / 播放失败</span>
      </div>
      <div v-if="events.length" class="event-list"><p v-for="item in events" :key="item">{{ item }}</p></div>
      <p v-else class="empty">开始接听后，这里会显示真实播放状态。</p>
    </section>
  </main>
</template>
