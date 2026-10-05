<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'

import SupportAssistantPicker from '../components/SupportAssistantPicker.vue'
import LiveVoiceCenter from '../components/LiveVoiceCenter.vue'
import LiveProductLinkCards from '../components/LiveProductLinkCards.vue'
import RoomInteractionPreferences from '../components/RoomInteractionPreferences.vue'
import {
  activateLiveAgentConfigVersion,
  adoptAgentLearningSession,
  adoptLiveAgentPlanBenefits,
  adoptLiveAgentPlanFacts,
  adoptLiveAgentPlanProductLinks,
  archiveLiveAgentPlan,
  bindRoomLiveAgentPlan,
  cloneLiveVoiceProfile,
  unbindRoomLiveAgentPlan,
  createAgentLearningSession,
  createAgentLearningTurn,
  createLiveAgentPlanVersion,
  createLiveAgentPlanScript,
  createLiveAgentPlan,
	createLiveAgentPlanProductAttribute,
  getLiveAgentPlanWorkspace,
  getLiveAgentPlanVersions,
  getLiveAgentPlans,
  getLiveAgentPlanBenefits,
  getLiveAgentPlanFacts,
  getLiveAgentPlanProductLinks,
  getLiveAgentPlanScripts,
  getAgentMemories,
  getRoomLiveAgentPlans,
  getLiveRuntime,
  setLiveRuntimePlan,
  createLiveAgentConfigDraft,
  getLiveAgentConfigVersions,
  getLiveAddressingStrategy,
  getLiveAgentSettings,
  getUserUIPreferences,
  getLiveRoomPolicyContext,
  getRooms,
  getRoom,
  deactivateAgentMemory,
  deleteLiveAgentPlanFact,
  deleteLiveAgentPlanProductLink,
	deleteLiveAgentPlanProductAttribute,
  publishLiveRoomPolicyVersion,
  auditLiveAgentFullShowPreview,
  regenerateLiveAgentFullShowVariant,
  generateLiveAgentFullShowVariantVoice,
  rebuildLiveAgentFullShowVariantSubtitles,
  rebuildLiveAgentCustomMainline,
  confirmLiveAgentPlanScriptAnalysis,
  getLiveAgentPlanStyleOverlays,
  getAnchorStylePluginCatalog,
  activateLiveAgentPlanStylePlugin,
  interpretLiveAgentPlanStyleOverlay,
  learnLiveAgentPlanStyleOverlay,
  saveLiveAgentPlanStyleOverlays,
  testLiveAgentAnchorStyle,
  previewAnalyzeLiveAgentAnchorStyle,
  listLiveAnchorStyles,
  createLiveAnchorStyle,
  getLiveAnchorStyle,
  updateLiveAnchorStyle,
  deleteLiveAnchorStyle,
  listLiveAnchorStyleSamples,
  createLiveAnchorStyleSample,
  deleteLiveAnchorStyleSample,
  analyzeLiveAnchorStyleSample,
  upsertLiveAnchorStylePlugin,
  deleteLiveAnchorStylePlugin,
  bindLiveAnchorStyleToPlan,
  previewAnalyzeLiveAgentPlanScript,
  previewGenerateLiveAgentFullShow,
  previewRecognizeLiveAgentPlanImage,
  publishLiveAgentPlanVersion,
  updateLiveAgentPlan,
  updateLiveAgentPlanFact,
  updateLiveAgentPlanProductLink,
	updateLiveAgentPlanProductAttribute,
  updateLiveAgentPlanScript,
  updateLiveAgentSettings,
  updateLiveAddressingStrategy,
  updateUserUIPreferences,
  uploadLiveAgentCustomMainline,
  uploadLiveMediaAsset,
} from '../api'
import { session } from '../session'
import '../styles/workflow-guidance.css'
import { beginLiveSupportScope, liveSupportMediaPlaybackURL } from '../liveSupportAccess'
import { getLiveContentMode, setLiveContentMode } from '../liveContentMode'
import { canSelectLiveContentMode, type LiveContentMode, type LiveContentModeAccess } from '../liveContentModeOptions'
import type {
  AgentMemoryItem,
  LiveAgentConfigInput,
  LiveAgentPlan,
  LiveAgentPlanBenefit,
  LiveAgentPlanBenefitCandidate,
  LiveAgentPlanFact,
  LiveAgentPlanFactCandidate,
  LiveAgentPlanProductLink,
  LiveAgentPlanProductLinkCandidate,
	LiveAgentPlanProductAttribute,
  LiveAgentPlanScript,
  LiveAgentPlanScriptAnalysis,
  LiveAgentPlanAnchorStyleProfile,
  LiveAnchorStyle,
  LiveAnchorStyleSample,
  LiveAnchorStyleAnalysisQC,
  LiveAgentPlanStyleOverlayProfile,
  LiveAnchorStyleOverlayItem,
  AnchorStylePluginCatalog,
  AnchorStylePluginCatalogItem,
  LiveAgentFullShowGenerationContext,
  LiveAgentFullShowPreviewResponse,
  LiveAgentFullShowVariant,
  LiveAgentCustomMainlineResponse,
  LiveAgentPlanVersion,
  LiveAgentPlanTimelineSegment,
  LiveAgentPlanSafePoint,
  LiveAgentVoiceIdentity,
  LiveAgentConfigVersion,
  LiveAgentSettings,
  LiveAgentSettingsInput,
  LiveAddressingOption,
  LiveAddressingStrategy,
  LiveRuntimeSnapshot,
  LiveRoomPolicyContext,
  Room,
} from '../types'

const props = withDefaults(defineProps<{
  supportSession?: boolean
  supportTenantId?: number
  supportRoomId?: number
}>(), {
  supportSession: false,
  supportTenantId: 0,
  supportRoomId: 0,
})

const rooms = ref<Room[]>([])
const activeRoomId = ref<number | null>(props.supportRoomId || null)
const endSupportScope = props.supportSession ? beginLiveSupportScope(props.supportRoomId) : null

function liveStrategyPreferenceKey(name: string) {
  const userID = Number(session.bootstrap?.actor.user_id || 0)
  return 'xiaolan-ui:' + String(userID || 'guest') + ':live-strategy:' + name
}

function restoreLiveStrategyPreferencesLocal() {
  if (props.supportSession) return
  const roomRaw = window.localStorage.getItem(liveStrategyPreferenceKey('selected-room'))
  const parsedRoom = Number(roomRaw || 0)
  if (parsedRoom > 0) activeRoomId.value = parsedRoom
}

function persistSelectedRoomLocal(roomID: number | null) {
  if (props.supportSession) return
  const key = liveStrategyPreferenceKey('selected-room')
  if (roomID) window.localStorage.setItem(key, String(roomID))
  else window.localStorage.removeItem(key)
}

function selectLiveStrategyRoom(roomID: number) {
  if (props.supportSession && roomID !== props.supportRoomId) return
  if (activeRoomId.value === roomID) return
  activeRoomId.value = roomID
  persistSelectedRoomLocal(roomID)
  if (!props.supportSession) {
    void updateUserUIPreferences({ selected_live_room_id: roomID }).catch(() => undefined)
  }
}

type StrategyMode =
  | 'script'
  | 'products'
  | 'benefits'
  | 'knowledge'
  | 'memory'
  | 'addressing'
  | 'anchor'
  | 'voice'
  | 'fullshow'
  | 'plan'
  | 'basic'
  | 'strategy'
// “直播素材”是旧版资料入口，保留内部兼容状态但不再作为前台页面；进入策略页默认落在主播风格。
const activeMode = ref<StrategyMode>('anchor')
const defaultSettings: LiveAgentSettings = {
  tenant_id: 0,
  display_name: '小伴直播教练',
  role_name: '直播策略与场控 Agent',
  self_introduction: '我是小伴直播教练，是你的直播策略与场控 Agent。',
  mission: '我负责直播策略调教、主播训练、固定话术、声音配置和现场场控协作。终端用户的策略调整只作用于当前直播间用户层，不修改规则层和行业层。',
  greeting: '你好，我是小伴直播教练。你可以直接告诉我想调整主播表达、固定话术、声音或直播策略。',
}
const settings = ref<LiveAgentSettings>({ ...defaultSettings })
const settingsDraft = ref<LiveAgentSettingsInput>({
  display_name: defaultSettings.display_name,
  role_name: defaultSettings.role_name,
  self_introduction: defaultSettings.self_introduction,
  mission: defaultSettings.mission,
  greeting: defaultSettings.greeting,
})
const settingsSaving = ref(false)
const settingsError = ref('')
const addressingStrategy = ref<LiveAddressingStrategy>({ addressing_mode: 'system', addressing: [] })
const addressingSaving = ref(false)
const addressingError = ref('')
const addressingNotice = ref('')
const configVersions = ref<LiveAgentConfigVersion[]>([])
const livePlans = ref<LiveAgentPlan[]>([])
const boundRoomPlans = ref<LiveAgentPlan[]>([])
const publishedRoomPlans = ref<LiveAgentPlan[]>([])
const roomRuntimeSnapshot = ref<LiveRuntimeSnapshot | null>(null)
const currentRoomPlanId = ref<number | null>(null)
const planBusy = ref(false)
const newPlanName = ref('')
const newPlanDescription = ref('')
const showPlanManager = ref(false)
const planManagerMode = ref<'create' | 'edit'>('edit')
const deleteConfirmPlanId = ref<number | null>(null)
const roomPolicyContext = ref<LiveRoomPolicyContext | null>(null)
const selectedVersionId = ref<number | null>(null)
const uploading = ref(false)
const documentInput = ref<HTMLInputElement | null>(null)
const audioInput = ref<HTMLInputElement | null>(null)
const scriptFileInput = ref<HTMLInputElement | null>(null)
const planScripts = ref<LiveAgentPlanScript[]>([])
const selectedScriptId = ref<number | null>(null)
const scriptTitle = ref('')
const scriptRawText = ref('')
const scriptReadableText = ref('')
const scriptSourceFile = ref<File | null>(null)
const scriptSourceType = ref<'paste' | 'upload'>('paste')
const scriptBusy = ref(false)
const scriptRecognizing = ref(false)
const scriptAnalyzing = ref(false)
const anchorStyleApplying = ref(false)
const scriptNotice = ref('')
const scriptAnalysisProgress = ref(0)
const scriptAnalysisStage = ref('')
const analysisDraft = ref<LiveAgentPlanScriptAnalysis | null>(null)
const knowledgeDraftText = ref('')
const rhythmDraftText = ref('')
const formalFacts = ref<LiveAgentPlanFact[]>([])
const formalBenefits = ref<LiveAgentPlanBenefit[]>([])
const formalProductLinks = ref<LiveAgentPlanProductLink[]>([])
const expandedFormalBenefitIds = ref<Set<number>>(new Set())
const benefitClock = ref(Date.now())
const selectedFactKeys = ref<string[]>([])
const editingFormalFactId = ref<number | null>(null)
const formalFactEditDraft = ref({ category: 'other', key: '', value: '', forbidden_wording: '', safe_rewrite: '' })
const creatingFormalFact = ref(false)
const formalFactCreateDraft = ref({ category: 'other', key: '', value: '', forbidden_wording: '', safe_rewrite: '' })
const mutatingFormalFactId = ref<number | null>(null)
const adoptingBenefitKey = ref('')
const adoptingProductLinkKey = ref('')
let scriptAnalysisProgressTimer: number | null = null
const memories = ref<AgentMemoryItem[]>([])
const interactionMemories = computed(() => memories.value.filter((item) => {
	const type = String(item.memory_type || '').trim().toLowerCase()
	return type === 'semantic' || type === 'interaction'
}))
const editingMemoryId = ref<number | null>(null)
const memoryEditSessionId = ref<number | null>(null)
const memoryEditFeedback = ref('')
const memoryEditPreview = ref('')
const memoryEditError = ref('')
const memoryMutatingId = ref<number | null>(null)
const memoryDeleteConfirmId = ref<number | null>(null)
const messages = ref<Array<{ role: 'agent' | 'user'; text: string }>>([
  { role: 'agent', text: defaultSettings.greeting },
])
const activeRoom = computed(() => rooms.value.find((item) => item.id === activeRoomId.value) || rooms.value[0])
const currentPlan = computed(
  () => livePlans.value.find((item) => item.id === currentRoomPlanId.value) || null,
)
const runtimePlanId = computed(() => roomRuntimeSnapshot.value?.agent_plan_id || 0)
const boundPlanIdSet = computed(() => new Set(boundRoomPlans.value.map((item) => item.id)))
const publishedPlanIdSet = computed(() => new Set(publishedRoomPlans.value.map((item) => item.id)))
const currentPlanIsBound = computed(() => !!currentPlan.value && boundPlanIdSet.value.has(currentPlan.value.id))
const currentPlanIsPublished = computed(
  () => !!currentPlan.value && publishedPlanIdSet.value.has(currentPlan.value.id),
)
const orderedRoomPlans = computed(() => [...livePlans.value].sort((left, right) => {
  const rank = (plan: LiveAgentPlan) => {
    if (runtimePlanId.value === plan.id) return 0
    if (publishedPlanIdSet.value.has(plan.id)) return 1
    if (boundPlanIdSet.value.has(plan.id)) return 2
    return 3
  }
  return rank(left) - rank(right) || left.name.localeCompare(right.name, 'zh-CN')
}))

function planRoomRelationLabel(plan: LiveAgentPlan) {
  if (runtimePlanId.value === plan.id && publishedPlanIdSet.value.has(plan.id)) return '正在使用 · 已发布'
  if (publishedPlanIdSet.value.has(plan.id)) return '已发布'
  if (boundPlanIdSet.value.has(plan.id)) return '已绑定 · 未发布'
  return '未绑定'
}
const activeScript = computed(
  () => planScripts.value.find((item) => item.id === selectedScriptId.value) || null,
)
const currentAnalysis = computed(() => analysisDraft.value || activeScript.value?.analysis || null)
const productLinks = computed<LiveAgentPlanProductLinkCandidate[]>(() => currentAnalysis.value?.product_links || [])
const activeFormalBenefits = computed(() => {
  const now = benefitClock.value
  const liveLinkKeys = new Set(formalProductLinks.value.filter((item) => item.status === 'active').map((item) => item.link_key))
  return formalBenefits.value.filter((item) => {
    if (item.status !== 'active') return false
    if (item.starts_at && new Date(item.starts_at).getTime() > now) return false
    if (item.ends_at && new Date(item.ends_at).getTime() < now) return false
    return !item.link_key || liveLinkKeys.has(item.link_key)
  })
})
const activityBenefits = computed<LiveAgentPlanBenefitCandidate[]>(() => productLinks.value
  .filter((item) => item.activity_price || item.gift || item.activity)
  .map((item) => ({
    key: `${item.link_key || 'room'}:current-benefit`,
    link_key: item.link_key || '未识别链接',
    product_name: item.product_name || '商品名称待确认',
    activity_price: item.activity_price || '',
    gift: item.gift || '',
    activity: item.activity || '',
    review_bucket: item.review_bucket || 'discuss',
    review_reason: item.review_reason || '',
    source_quotes: item.source_quotes || [],
  })))

function toggleFormalBenefit(id: number) {
	const next = new Set(expandedFormalBenefitIds.value)
	if (next.has(id)) next.delete(id)
	else next.add(id)
	expandedFormalBenefitIds.value = next
}
const savedStyleScript = computed(() => planScripts.value.find((item) => item.analysis_status === 'analyzed' && item.analysis?.anchor_style?.dimensions?.length))
const reusableAnchorStyles = ref<LiveAnchorStyle[]>([])
const selectedReusableAnchorStyleId = ref<number | null>(null)
const selectedReusableAnchorStyle = ref<LiveAnchorStyle | null>(null)
const reusableAnchorStyleSamples = ref<LiveAnchorStyleSample[]>([])
const reusableAnchorStyleTab = ref<'samples' | 'analysis' | 'testing' | 'personalization'>('samples')
const reusableAnchorStyleBusy = ref(false)
const reusableAnchorStyleNotice = ref('')
const reusableAnchorStyleError = ref('')
const reusableAnchorStyleDraft = ref({ name: '', description: '' })
const reusableAnchorStyleSampleDraft = ref({ title: '', raw_text: '' })
const reusableAnchorStyleAnalysisStage = ref('')
const reusableAnchorStyleLongTestRunning = ref(false)
const reusableAnchorStyleLongTestText = ref('')
const reusableAnchorStyleLongTestSegments = ref(0)
const reusableAnchorStyleLongTestMinutes = ref(30)
const reusableAnchorStyleProfile = computed(() => selectedReusableAnchorStyle.value?.profile || null)
const anchorStyleProfile = computed(() => reusableAnchorStyleProfile.value?.dimensions?.length
  ? reusableAnchorStyleProfile.value
  : currentAnalysis.value?.anchor_style?.dimensions?.length
    ? currentAnalysis.value.anchor_style
    : savedStyleScript.value?.analysis.anchor_style || null)
const anchorStyleSourceText = ref('')
const anchorStyleSampleExpanded = ref(true)
const anchorStyleTestBusy = ref(false)
const anchorStyleTestTopic = ref('本轮主线介绍当前商品的核心价值，并从一个正式事实自然展开')
const anchorStyleTestTargetChars = ref(500)
const anchorStyleTestResult = ref<Awaited<ReturnType<typeof testLiveAgentAnchorStyle>> | null>(null)
const anchorStyleTestError = ref('')
const anchorStyleAnalysisQC = ref<LiveAnchorStyleAnalysisQC | null>(null)
const anchorStyleCorrectionFeedback = ref('')
const anchorStyleCorrectionCandidates = ref<LiveAnchorStyleOverlayItem[]>([])
const anchorStyleCorrectionHistory = ref<Array<{ feedback: string; diagnosis: string; model: string }>>([])
const anchorStyleCorrectionBusy = ref(false)
const anchorStyleCorrectionError = ref('')
const styleOverlayProfile = ref<LiveAgentPlanStyleOverlayProfile | null>(null)
const styleOverlaySource = ref('')
const styleOverlayExplanation = ref('')
const styleOverlayStrength = ref(50)
const styleOverlayBusy = ref(false)
const styleOverlayError = ref('')
const styleOverlayNotice = ref('')
const styleOverlayPending = ref<LiveAnchorStyleOverlayItem | null>(null)
const styleOverlayMemoryMatches = ref(0)
const stylePluginCatalog = ref<AnchorStylePluginCatalog | null>(null)
const stylePluginBusyID = ref('')
const anchorDelivery = computed(() => anchorStyleProfile.value?.delivery_spec)
const anchorRulebook = computed(() => anchorDelivery.value?.version === 'anchor-delivery/v1' ? anchorDelivery.value.rulebook || '' : '')
const enabledStyleOverlayCount = computed(() => styleOverlayProfile.value?.items.filter((item) => item.enabled).length || 0)
const activeStylePluginIDs = computed(() => new Set((styleOverlayProfile.value?.items || []).flatMap((item) => item.plugin?.plugin_id ? [item.plugin.plugin_id] : [])))
const canRunAnchorStyleTest = computed(() => Boolean(anchorRulebook.value) || enabledStyleOverlayCount.value > 0)
const anchorCopyNotice = ref('')
async function copyAnchorRulebook() {
  try {
    await navigator.clipboard.writeText(anchorRulebook.value)
    anchorCopyNotice.value = '口播规范已复制'
  } catch { anchorCopyNotice.value = '复制失败，请展开规范后手动选择复制' }
}

function styleOverlayStrengthLabel(value: number) {
  if (value <= 35) return '轻微'
  if (value <= 70) return '适中'
  return '明显'
}

function styleOverlayApplicationLabel(value: string) {
  if (value === 'always') return '持续习惯'
  if (value === 'occasional') return '偶尔出现'
  return '按场景出现'
}

async function loadStyleOverlays() {
  const planId = currentRoomPlanId.value
  const tenantId = activeRoom.value?.tenant_id
  styleOverlayProfile.value = null
  stylePluginCatalog.value = null
  styleOverlayPending.value = null
  styleOverlayError.value = ''
  styleOverlayNotice.value = ''
  if (!planId) return
  try {
    const [result, catalog] = await Promise.all([
      getLiveAgentPlanStyleOverlays(planId, tenantId),
      getAnchorStylePluginCatalog(),
    ])
    if (planId === currentRoomPlanId.value) {
      styleOverlayProfile.value = result
      stylePluginCatalog.value = catalog
    }
  } catch (err) {
    if (planId === currentRoomPlanId.value) styleOverlayError.value = err instanceof Error ? err.message : '读取叠加风格失败'
  }
}

async function loadReusableAnchorStyleWorkspace(preferredID?: number | null) {
  const tenantID = activeRoom.value?.tenant_id
  try {
    const result = await listLiveAnchorStyles(tenantID)
    reusableAnchorStyles.value = result.items || []
    const nextID = preferredID || selectedReusableAnchorStyleId.value || result.items?.[0]?.id || null
    if (!nextID) {
      selectedReusableAnchorStyleId.value = null
      selectedReusableAnchorStyle.value = null
      reusableAnchorStyleSamples.value = []
      return
    }
    await selectReusableAnchorStyle(nextID, false)
  } catch (err) {
    reusableAnchorStyleError.value = err instanceof Error ? err.message : '读取主播风格失败'
  }
}

async function selectReusableAnchorStyle(styleID: number, showNotice = true) {
  reusableAnchorStyleBusy.value = true
  reusableAnchorStyleError.value = ''
  try {
    const tenantID = activeRoom.value?.tenant_id
    const [style, samples] = await Promise.all([
      getLiveAnchorStyle(styleID, tenantID),
      listLiveAnchorStyleSamples(styleID, tenantID),
    ])
    if (!stylePluginCatalog.value) {
      try { stylePluginCatalog.value = await getAnchorStylePluginCatalog() } catch { /* 插件目录不可用时仍可查看和训练主播 */ }
    }
    selectedReusableAnchorStyleId.value = styleID
    selectedReusableAnchorStyle.value = style
    reusableAnchorStyleSamples.value = samples.items || []
    reusableAnchorStyleDraft.value = { name: style.name, description: style.description || '' }
    reusableAnchorStyleTab.value = reusableAnchorStyleTab.value || 'samples'
    if (showNotice) reusableAnchorStyleNotice.value = `已切换到主播“${style.name}”，后续测试和方案绑定都会使用这套风格。`
  } catch (err) {
    reusableAnchorStyleError.value = err instanceof Error ? err.message : '读取主播风格详情失败'
  } finally {
    reusableAnchorStyleBusy.value = false
  }
}

async function createReusableAnchorStyle() {
  reusableAnchorStyleBusy.value = true
  reusableAnchorStyleError.value = ''
  try {
    const style = await createLiveAnchorStyle({ name: '新主播', description: '请添加这个主播的表达样本和风格说明。', tenant_id: activeRoom.value?.tenant_id })
    reusableAnchorStyles.value = [style, ...reusableAnchorStyles.value]
    await selectReusableAnchorStyle(style.id)
    reusableAnchorStyleTab.value = 'samples'
  } catch (err) {
    reusableAnchorStyleError.value = err instanceof Error ? err.message : '新建主播失败'
  } finally { reusableAnchorStyleBusy.value = false }
}

async function saveReusableAnchorStyleInfo() {
  const style = selectedReusableAnchorStyle.value
  if (!style || !reusableAnchorStyleDraft.value.name.trim()) return
  reusableAnchorStyleBusy.value = true
  try {
    const updated = await updateLiveAnchorStyle(style.id, {
      name: reusableAnchorStyleDraft.value.name.trim(),
      description: reusableAnchorStyleDraft.value.description.trim(),
      profile: style.profile,
    }, activeRoom.value?.tenant_id)
    selectedReusableAnchorStyle.value = updated
    reusableAnchorStyles.value = reusableAnchorStyles.value.map((item) => item.id === updated.id ? updated : item)
    reusableAnchorStyleNotice.value = '主播名称和介绍已保存。'
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '保存主播风格失败' }
  finally { reusableAnchorStyleBusy.value = false }
}

async function deleteReusableAnchorStyle() {
  const style = selectedReusableAnchorStyle.value
  if (!style) return
  if (!window.confirm(`确定删除主播“${style.name}”吗？删除后该主播的样本、训练记录和绑定关系都会移除。`)) return
  if (!window.confirm('请再次确认：这不是移出列表，而是永久删除这名主播的风格资料。')) return
  try {
    await deleteLiveAnchorStyle(style.id, activeRoom.value?.tenant_id)
    reusableAnchorStyles.value = reusableAnchorStyles.value.filter((item) => item.id !== style.id)
    selectedReusableAnchorStyleId.value = null
    selectedReusableAnchorStyle.value = null
    await loadReusableAnchorStyleWorkspace()
    reusableAnchorStyleNotice.value = '主播风格已删除。'
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '删除主播风格失败' }
}

async function addReusableAnchorStyleSample() {
  const style = selectedReusableAnchorStyle.value
  const text = reusableAnchorStyleSampleDraft.value.raw_text.trim()
  if (!style || !text) { reusableAnchorStyleError.value = '请先粘贴一份主播真实话术。'; return }
  reusableAnchorStyleBusy.value = true
  reusableAnchorStyleError.value = ''
  try {
    const sample = await createLiveAnchorStyleSample(style.id, {
      title: reusableAnchorStyleSampleDraft.value.title.trim() || `样本${reusableAnchorStyleSamples.value.length + 1}`,
      source_type: 'paste', raw_text: text, readable_text: text,
    }, activeRoom.value?.tenant_id)
    reusableAnchorStyleSamples.value = [sample, ...reusableAnchorStyleSamples.value]
    reusableAnchorStyleSampleDraft.value = { title: '', raw_text: '' }
    reusableAnchorStyleNotice.value = '样本已保存。请点击“分析这份样本”，系统只提取主播怎么说。'
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '保存主播样本失败' }
  finally { reusableAnchorStyleBusy.value = false }
}

async function importReusableAnchorStyleSamples(event: Event) {
  const style = selectedReusableAnchorStyle.value
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!style || !files.length) return
  reusableAnchorStyleBusy.value = true
  reusableAnchorStyleError.value = ''
  try {
    for (const file of files) {
      const text = (await file.text()).trim()
      if (!text) continue
      const sample = await createLiveAnchorStyleSample(style.id, {
        title: file.name.replace(/\.(txt|md)$/i, '') || '导入样本', source_type: 'upload', original_name: file.name,
        raw_text: text, readable_text: text,
      }, activeRoom.value?.tenant_id)
      reusableAnchorStyleSamples.value.unshift(sample)
    }
    reusableAnchorStyleNotice.value = `已导入 ${files.length} 份样本，请逐份点击分析。`
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '导入主播样本失败' }
  finally { reusableAnchorStyleBusy.value = false }
}

async function analyzeReusableAnchorStyleSample(sample: LiveAnchorStyleSample) {
  const style = selectedReusableAnchorStyle.value
  if (!style || reusableAnchorStyleBusy.value) return
  reusableAnchorStyleBusy.value = true
  reusableAnchorStyleError.value = ''
  reusableAnchorStyleAnalysisStage.value = '正在读取样本，观察称呼、口头禅和句子节奏…'
  sample.analysis_status = 'analyzing'
  try {
    const result = await analyzeLiveAnchorStyleSample(style.id, sample.id, activeRoom.value?.tenant_id)
    selectedReusableAnchorStyle.value = result.style
    reusableAnchorStyleSamples.value = reusableAnchorStyleSamples.value.map((item) => item.id === result.sample.id ? result.sample : item)
    reusableAnchorStyleTab.value = 'analysis'
    reusableAnchorStyleNotice.value = `“${sample.title}”分析完成${result.style_qc?.available ? (result.style_qc.passed ? '，同步质检通过。' : '，同步质检提示了风格边界问题，请查看结果。') : '。'}`
  } catch (err) {
    reusableAnchorStyleError.value = err instanceof Error ? err.message : '主播风格分析失败，请检查样本后重试'
    window.alert(reusableAnchorStyleError.value)
    sample.analysis_status = 'failed'
  } finally { reusableAnchorStyleBusy.value = false; reusableAnchorStyleAnalysisStage.value = '' }
}

async function deleteReusableAnchorStyleSample(sample: LiveAnchorStyleSample) {
  const style = selectedReusableAnchorStyle.value
  if (!style || !window.confirm(`删除样本“${sample.title}”吗？`)) return
  try {
    await deleteLiveAnchorStyleSample(style.id, sample.id, activeRoom.value?.tenant_id)
    reusableAnchorStyleSamples.value = reusableAnchorStyleSamples.value.filter((item) => item.id !== sample.id)
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '删除主播样本失败' }
}

async function bindReusableAnchorStyle() {
  const style = selectedReusableAnchorStyle.value
  const planID = currentRoomPlanId.value
  if (!style || !planID) { reusableAnchorStyleError.value = '请先选择智能体方案和主播风格。'; return }
  try {
    await bindLiveAnchorStyleToPlan(style.id, planID, activeRoom.value?.tenant_id)
    reusableAnchorStyleNotice.value = `主播“${style.name}”已作为当前智能体方案的主播风格。`
    await loadReusableAnchorStyleWorkspace(style.id)
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '绑定主播风格失败' }
}

async function runReusableLongTest() {
  if (reusableAnchorStyleLongTestRunning.value || !selectedReusableAnchorStyle.value || !currentRoomPlanId.value || !activeRoomId.value) return
  const style = selectedReusableAnchorStyle.value.profile
  const sampleText = reusableAnchorStyleSamples.value.find((item) => item.analysis_status === 'analyzed')?.readable_text || currentAnchorStyleSampleText(style)
  if (!style?.dimensions?.length || !sampleText) { reusableAnchorStyleError.value = '请先分析至少一份主播样本，再进行长时间测试。'; return }
  reusableAnchorStyleLongTestRunning.value = true
  reusableAnchorStyleLongTestText.value = ''
  reusableAnchorStyleLongTestSegments.value = 0
  reusableAnchorStyleError.value = ''
  try {
    const totalSegments = Math.max(1, Math.ceil(Math.max(5, reusableAnchorStyleLongTestMinutes.value) / 5))
    for (let index = 0; index < totalSegments && reusableAnchorStyleLongTestRunning.value; index += 1) {
      const previousTail = reusableAnchorStyleLongTestText.value.slice(-120)
      const continuityTopic = `${anchorStyleTestTopic.value}。这是连续主线测试的第${index + 1}段，请承接上一段但换一个表达角度，不引入互动问答。${previousTail ? `上一段最后几句仅供衔接参考：${previousTail}` : ''}`.slice(0, 280)
      const result = await testLiveAgentAnchorStyle(currentRoomPlanId.value, {
        room_id: activeRoomId.value, topic: continuityTopic,
        target_chars: Math.min(1800, Math.max(300, anchorStyleTestTargetChars.value)),
        expansion_freedom: fullShowExpansionFreedom.value, source_text: sampleText, anchor_style: style,
        selected_facts: selectedFactKeys.value,
        transient_overlays: anchorStyleCorrectionCandidates.value,
      })
      reusableAnchorStyleLongTestText.value += (reusableAnchorStyleLongTestText.value ? '\n\n' : '') + result.text
      reusableAnchorStyleLongTestSegments.value += 1
    }
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '长时间主播测试失败' }
  finally { reusableAnchorStyleLongTestRunning.value = false }
}

function stopReusableLongTest() { reusableAnchorStyleLongTestRunning.value = false }

async function toggleReusableAnchorPlugin(item: AnchorStylePluginCatalogItem) {
  const style = selectedReusableAnchorStyle.value
  if (!style) return
  const existing = style.plugin_settings?.find((entry) => entry.plugin_id === item.manifest.id)
  try {
    if (existing?.enabled) {
      await deleteLiveAnchorStylePlugin(style.id, item.manifest.id, activeRoom.value?.tenant_id)
      style.plugin_settings = style.plugin_settings.filter((entry) => entry.plugin_id !== item.manifest.id)
    } else {
      const result = await upsertLiveAnchorStylePlugin(style.id, {
        plugin_id: item.manifest.id,
        plugin_version: item.manifest.version,
        enabled: true,
        parameters: existing?.parameters || stylePluginDefaultParameters(item),
      }, activeRoom.value?.tenant_id)
      style.plugin_settings = result.items
    }
    if (currentRoomPlanId.value && styleOverlayProfile.value && item.activation_supported) {
      if (!existing?.enabled) {
        await activateStylePlugin(item)
      } else {
        const remaining = styleOverlayProfile.value.items.filter((entry) => entry.plugin?.plugin_id !== item.manifest.id)
        await persistStyleOverlayItems(remaining, `已停用“${item.manifest.name}”，当前方案不会再叠加这项能力。`)
      }
    }
    reusableAnchorStyleNotice.value = `${item.manifest.name}已${existing?.enabled ? '停用' : '启用'}，测试生成时会读取这项设置。`
  } catch (err) { reusableAnchorStyleError.value = err instanceof Error ? err.message : '保存个性插件失败' }
}

function stylePluginDefaultParameters(item: AnchorStylePluginCatalogItem) {
  return Object.fromEntries(Object.entries(item.manifest.parameters || {}).flatMap(([name, definition]) => (
    definition.default === undefined ? [] : [[name, definition.default]]
  )))
}

async function activateStylePlugin(item: AnchorStylePluginCatalogItem) {
  const planId = currentRoomPlanId.value
  const profile = styleOverlayProfile.value
  if (!planId || !profile || !item.activation_supported || stylePluginBusyID.value) return
  stylePluginBusyID.value = item.manifest.id
  styleOverlayError.value = ''
  styleOverlayNotice.value = ''
  try {
    const result = await activateLiveAgentPlanStylePlugin(planId, {
      expected_revision: profile.revision,
      plugin_id: item.manifest.id,
      plugin_version: item.manifest.version,
      strength: 50,
      parameters: stylePluginDefaultParameters(item),
    }, activeRoom.value?.tenant_id)
    if (planId !== currentRoomPlanId.value) return
    styleOverlayProfile.value = result.profile
    styleOverlayNotice.value = `插件“${item.manifest.name}”已加入主播并热生效。`
  } catch (err) {
    styleOverlayError.value = err instanceof Error ? err.message : '启用主播风格插件失败'
  } finally {
    stylePluginBusyID.value = ''
  }
}

async function interpretStyleOverlay() {
  const planId = currentRoomPlanId.value
  const source = styleOverlaySource.value.trim()
  if (styleOverlayBusy.value) return
  if (!planId) {
    styleOverlayError.value = '请先选择直播方案'
    return
  }
  if (!styleOverlayProfile.value) {
    styleOverlayError.value = '主播风格正在加载，请稍后再试'
    return
  }
  if (!source) {
    styleOverlayError.value = '请先在“希望主播增加什么习惯”中输入内容；灰色示例文字不是已输入内容。'
    return
  }
  styleOverlayBusy.value = true
  styleOverlayError.value = ''
  styleOverlayNotice.value = ''
  styleOverlayPending.value = null
  try {
    const result = await interpretLiveAgentPlanStyleOverlay(planId, {
      source_text: source,
      explanation_text: styleOverlayExplanation.value.trim() || undefined,
      strength: styleOverlayStrength.value,
    }, activeRoom.value?.tenant_id)
    if (planId !== currentRoomPlanId.value) return
    styleOverlayPending.value = result.item
    styleOverlayMemoryMatches.value = result.memory_matches
    styleOverlayNotice.value = `已由 ${result.model} 完成理解，请确认系统理解后再加入主播。`
  } catch (err) {
    styleOverlayError.value = err instanceof Error ? err.message : '叠加风格理解失败'
  } finally {
    styleOverlayBusy.value = false
  }
}

async function persistStyleOverlayItems(items: LiveAnchorStyleOverlayItem[], success: string) {
  const planId = currentRoomPlanId.value
  const profile = styleOverlayProfile.value
  if (!planId || !profile || styleOverlayBusy.value) return false
  styleOverlayBusy.value = true
  styleOverlayError.value = ''
  styleOverlayNotice.value = ''
  try {
    const result = await saveLiveAgentPlanStyleOverlays(planId, { ...profile, items }, activeRoom.value?.tenant_id)
    if (planId !== currentRoomPlanId.value) return false
    styleOverlayProfile.value = result.profile
    styleOverlayNotice.value = success + (result.semantic_indexed ? '；已存入当前客户的风格理解记忆。' : '；向量服务不可用时不影响规则执行。')
    return true
  } catch (err) {
    styleOverlayError.value = err instanceof Error ? err.message : '保存叠加风格失败'
    return false
  } finally {
    styleOverlayBusy.value = false
  }
}

async function confirmStyleOverlay() {
  if (!styleOverlayPending.value || !styleOverlayProfile.value) return
  const saved = await persistStyleOverlayItems([...styleOverlayProfile.value.items, styleOverlayPending.value], '叠加风格已保存并热生效')
  if (!saved) return
  styleOverlayPending.value = null
  styleOverlaySource.value = ''
  styleOverlayExplanation.value = ''
}

async function toggleStyleOverlay(item: LiveAnchorStyleOverlayItem) {
  if (!styleOverlayProfile.value) return
  await persistStyleOverlayItems(styleOverlayProfile.value.items.map((current) => current.id === item.id ? { ...current, enabled: !current.enabled } : current), item.enabled ? '叠加风格已暂停' : '叠加风格已启用')
}

async function removeStyleOverlay(item: LiveAnchorStyleOverlayItem) {
  if (!styleOverlayProfile.value) return
  await persistStyleOverlayItems(styleOverlayProfile.value.items.filter((current) => current.id !== item.id), '叠加风格已移除')
}
const fullShowDuration = ref(90)
const fullShowDurationProgress = computed(() => ((fullShowDuration.value - 30) / 90) * 100)
const fullShowDurationLabel = computed(() => {
  const hours = fullShowDuration.value / 60
  return Number.isInteger(hours)
    ? `${fullShowDuration.value} 分钟（${hours} 小时）`
    : `${fullShowDuration.value} 分钟（${hours.toFixed(1)} 小时）`
})
const fullShowRoundMinutes = ref(7)
const fullShowVariantCount = ref(5)
const fullShowExpansionFreedom = ref(65)
const fullShowExpansionLabel = computed(() => {
  if (fullShowExpansionFreedom.value <= 25) return '保守扩展'
  if (fullShowExpansionFreedom.value <= 55) return '平衡扩展'
  if (fullShowExpansionFreedom.value <= 80) return '开放扩展'
  return '边缘合规'
})
const fullShowUseAnchorStyle = ref(true)
const fullShowUseDynamicFacts = ref(true)
const fullShowGenerateTTS = ref(true)
const fullShowAvoidRecent = ref(true)
const fullShowPreviewReady = ref(false)
const fullShowNotice = ref('')
const fullShowGenerating = ref(false)
const fullShowError = ref('')
const fullShowGenerationProgress = ref({
  visible: false,
  mode: '文字预览',
  percent: 0,
  stage: '',
  detail: '',
  failed: false,
})
let fullShowGenerationProgressTimer: number | null = null
let fullShowGenerationStartedAt = 0
const fullShowResult = ref<LiveAgentFullShowPreviewResponse | null>(null)
const selectedFullShowVariantKey = ref('A')
const fullShowEditingVariantKey = ref('')
const fullShowEditingText = ref('')
const fullShowEditSaving = ref(false)
const fullShowEditError = ref('')
const fullShowRegeneratingVariantKey = ref('')
const fullShowRegenerateError = ref('')
const fullShowManualEditedKeys = ref<string[]>([])
const fullShowFormalVariantKeys = ref<string[]>([])
const fullShowFactContextMenu = ref({
  visible: false,
  x: 0,
  y: 0,
  text: '',
})
const fullShowFactDialogOpen = ref(false)
const fullShowFactSaving = ref(false)
const fullShowFactError = ref('')
const fullShowFactDraft = ref({
  category: 'other',
  key: '',
  value: '',
})
type FullShowVoiceState = {
  status: 'idle' | 'generating' | 'ready' | 'candidate' | 'failed'
  progress: number
  audio_url: string
  audio_asset_id: number
  duration_ms: number
  timeline: LiveAgentPlanTimelineSegment[]
  srt: string
  safe_points: LiveAgentPlanSafePoint[]
  asset_manifest: Record<string, unknown>
  candidate_url: string
  candidate_asset_id: number
  candidate_duration_ms: number
  candidate_timeline: LiveAgentPlanTimelineSegment[]
  candidate_srt: string
  candidate_safe_points: LiveAgentPlanSafePoint[]
  candidate_asset_manifest: Record<string, unknown>
  candidate_voice_identity: string
  candidate_voice_identity_version: string
  candidate_voice_identity_key: string
  error: string
  generation_no: number
  voice_identity: string
  voice_identity_version: string
  voice_identity_key: string
}
const fullShowVoiceStates = ref<Record<string, FullShowVoiceState>>({})
const fullShowVoicePlaybackMS = ref<Record<string, number>>({})
const fullShowVoiceDetail = ref<{ key: string; mode: '' | 'srt' | 'safe' }>({ key: '', mode: '' })
const fullShowOutputMode = ref<'generated' | 'custom'>('generated')
const contentModeAccess = ref<LiveContentModeAccess | null>(null)
const contentModeLoading = ref(false)
const contentModeSaving = ref(false)
const contentModeError = ref('')
const contentModeNotice = ref('')
const isUserAudioContentMode = computed(() => contentModeAccess.value?.content_mode === 'user_audio')
const contentModeReady = computed(() => Boolean(contentModeAccess.value && canSelectLiveContentMode(contentModeAccess.value, contentModeAccess.value.content_mode)))
let contentModeLoadNo = 0
let contentModeRefreshTimer: number | undefined
let contentModeDisposed = false

async function refreshContentMode(reset = false) {
  if (contentModeDisposed) return
  const roomId = activeRoomId.value
  const token = ++contentModeLoadNo
  if (reset) contentModeAccess.value = null
  contentModeError.value = ''
  if (!roomId) { contentModeLoading.value = false; return }
  contentModeLoading.value = true
  try {
    const value = await getLiveContentMode(roomId)
    if (token !== contentModeLoadNo || activeRoomId.value !== roomId) return
    const previousMode = contentModeAccess.value?.content_mode
    contentModeAccess.value = value
    if (previousMode !== value.content_mode) {
      fullShowOutputMode.value = value.content_mode === 'user_audio' ? 'custom' : 'generated'
    }
  } catch (err) {
    if (token !== contentModeLoadNo) return
    contentModeAccess.value = null
    contentModeError.value = err instanceof Error ? err.message : '读取直播模式失败'
  } finally {
    if (token === contentModeLoadNo) contentModeLoading.value = false
  }
}

async function chooseContentMode(mode: LiveContentMode) {
  const roomId = activeRoomId.value
  if (!roomId || contentModeLoading.value || contentModeSaving.value || fullShowGenerating.value) return
  if (!canSelectLiveContentMode(contentModeAccess.value, mode)) return
  if (contentModeAccess.value?.content_mode === mode) return
  const token = ++contentModeLoadNo
  contentModeSaving.value = true
  contentModeError.value = ''
  contentModeNotice.value = ''
  try {
    const value = await setLiveContentMode(roomId, mode)
    if (token !== contentModeLoadNo || roomId !== activeRoomId.value) return
    contentModeAccess.value = value
    selectFullShowOutputMode(value.content_mode === 'user_audio' ? 'custom' : 'generated')
    contentModeNotice.value = '已保存当前直播间的内容模式；文案和声音仍需确认并发布后使用。'
  } catch (err) {
    if (token !== contentModeLoadNo) return
    contentModeError.value = err instanceof Error ? err.message : '切换直播模式失败'
    contentModeAccess.value = null
  } finally {
    if (token === contentModeLoadNo) contentModeSaving.value = false
  }
}

function handleContentPolicyUpdated(event: Event) {
  if (contentModeSaving.value) return
  const roomId = Number((event as CustomEvent<{ room_id?: number }>).detail?.room_id || 0)
  if (!roomId || roomId === activeRoomId.value) void refreshContentMode()
}
const fullShowSubtitleDrafts = ref<Record<string, LiveAgentPlanTimelineSegment[]>>({})
const fullShowSubtitleSavingKey = ref('')
const fullShowSubtitleError = ref('')
const customMainlineFile = ref<File | null>(null)
const customMainlineVoiceSample = ref<File | null>(null)
const customMainlineDurationMS = ref(0)
const customMainlineUploadProgress = ref(0)
const customMainlineStage = ref<'idle' | 'uploading' | 'analyzing' | 'ready' | 'saving' | 'failed'>('idle')
const customMainlineDraft = ref<LiveAgentCustomMainlineResponse | null>(null)
const customMainlineTimeline = ref<LiveAgentPlanTimelineSegment[]>([])
const customMainlineTranscript = ref('')
const customMainlineSaveNotice = ref('')
const customMainlineError = ref('')
const customMainlineCloneBusy = ref(false)
const originalAiCandidateKey = ref('')
const originalAiSrtProgress = ref<{
  status: 'idle' | 'generating' | 'done' | 'failed'
  percent: number
  stage: string
  error: string
}>({ status: 'idle', percent: 0, stage: '', error: '' })
let customMainlineAbort: AbortController | null = null
const fullShowVoiceTimers = new Map<string, number>()
const fullShowSetListening = ref(false)
const fullShowSetListeningKey = ref('')
let fullShowSetAudio: HTMLAudioElement | null = null
let fullShowSetResolve: (() => void) | null = null
const fullShowSavedVersion = ref<LiveAgentPlanVersion | null>(null)
const fullShowVersionHistory = ref<LiveAgentPlanVersion[]>([])
const fullShowVersionSaving = ref(false)
const fullShowVersionPublishing = ref(false)
const fullShowVersionError = ref('')
const fullShowWorkspaceLoading = ref(false)
const fullShowWorkspaceHydrating = ref(false)
const fullShowWorkspaceBaseVersion = ref<LiveAgentPlanVersion | null>(null)
const fullShowWorkspaceSource = ref<'draft' | 'published' | ''>('')
const fullShowWorkspaceInherited = ref(false)
const fullShowWorkspaceVoiceIdentity = ref<LiveAgentVoiceIdentity | null>(null)
const fullShowWorkspaceLegacyPartial = ref(false)
const fullShowWorkspaceDirty = computed(
  () =>
    Boolean(fullShowResult.value && fullShowWorkspaceBaseVersion.value) &&
    !fullShowSavedVersion.value,
)
const fullShowRoundCount = computed(() => Math.max(1, Math.ceil(fullShowDuration.value / fullShowRoundMinutes.value)))
const selectedFullShowVariant = computed<LiveAgentFullShowVariant | null>(() => {
  const variants = (fullShowResult.value?.variants || []).filter(
    (item) => fullShowOutputMode.value === 'custom' || item.variant_key !== 'CUSTOM',
  )
  return variants.find((item) => item.variant_key === selectedFullShowVariantKey.value) || variants[0] || null
})
const aiFullShowVariants = computed(() =>
  (fullShowResult.value?.variants || []).filter((item) => item.variant_key !== 'CUSTOM'),
)
const fullShowEditing = computed(
  () => Boolean(selectedFullShowVariant.value && fullShowEditingVariantKey.value === selectedFullShowVariant.value.variant_key),
)
const fullShowAuditPassedCount = computed(() => (fullShowResult.value?.variants || []).filter((item) => item.audit?.passed).length)
const fullShowFormalSelectedCount = computed(() => fullShowFormalVariantKeys.value.length)
const fullShowVoiceReadyCount = computed(() =>
  fullShowFormalVariantKeys.value.filter((key) => {
    const voice = fullShowVoiceState(key)
    return Boolean(
      voice.audio_url &&
        voice.audio_asset_id > 0 &&
        voice.duration_ms > 0 &&
        voice.timeline.length > 0 &&
        voice.srt.trim() &&
        voice.safe_points.length > 0 &&
        Object.keys(voice.asset_manifest).length > 0,
    )
  }).length,
)
const fullShowAllFormalVoicesReady = computed(
  () =>
    fullShowFormalSelectedCount.value > 0 &&
    fullShowVoiceReadyCount.value === fullShowFormalSelectedCount.value,
)
const systemDefaultVoice = {
  source: 'official',
  provider: 'aliyun_qwen',
  name: '龙安灵心',
  voice_id: 'longanlingxin',
  profile_id: 0,
  model: 'qwen-audio-3.0-tts-plus',
  system_default: true,
}
const currentRoomSelectedVoice = computed(() => {
  const speech = asRecord(activeConfig.value?.speech_config)
  const rooms = asRecord(speech.rooms)
  const room = asRecord(rooms[String(activeRoomId.value || 0)])
  const selected = asRecord(room.selected_voice)
  return String(selected.voice_id || '').trim() ? selected : systemDefaultVoice
})
const currentRoomSelectedVoiceName = computed(
  () => {
    const name = String(currentRoomSelectedVoice.value.name || '').trim() || systemDefaultVoice.name
    return currentRoomSelectedVoice.value.system_default ? '系统默认 · ' + name : name
  },
)
const roomVoiceIdentity = computed<LiveAgentVoiceIdentity>(() => {
  const voice = asRecord(currentRoomSelectedVoice.value)
  return {
    name: String(voice.identity_name || voice.name || systemDefaultVoice.name).trim(),
    version: String(voice.version || 'V1').trim(),
    source: String(voice.source || systemDefaultVoice.source).trim(),
    provider: String(voice.provider || systemDefaultVoice.provider).trim(),
    voice_id: String(voice.voice_id || systemDefaultVoice.voice_id).trim(),
    profile_id: Number(voice.profile_id || 0) || undefined,
    binding_id: Number(voice.binding_id || 0) || undefined,
    model: String(voice.model || systemDefaultVoice.model).trim(),
    rate: Number(voice.rate || 1) || 1,
    emotion: String(voice.emotion || '').trim() || undefined,
    style: asRecord(voice.style),
  }
})
const currentVoiceIdentity = computed<LiveAgentVoiceIdentity>(
  () => fullShowWorkspaceVoiceIdentity.value || roomVoiceIdentity.value,
)
const currentFullShowVoiceName = computed(() =>
  fullShowWorkspaceVoiceIdentity.value
    ? '版本恢复 · ' + currentVoiceIdentity.value.name
    : currentRoomSelectedVoiceName.value,
)
function fullShowVoiceIdentityKey(identity: LiveAgentVoiceIdentity) {
  return [
    identity.source,
    identity.provider,
    identity.voice_id,
    String(identity.profile_id || 0),
    String(identity.binding_id || 0),
    identity.model,
    identity.version,
  ].join('|')
}
const currentVoiceIdentityKey = computed(() => fullShowVoiceIdentityKey(currentVoiceIdentity.value))
const fullShowVoiceIdentityConsistent = computed(() =>
  fullShowFormalVariantKeys.value.every((key) => {
    const state = fullShowVoiceState(key)
    // Uploaded mainlines retain their recorded voice independently of the
    // selected interactive voice. The server verifies the actual asset purpose.
    return Boolean(state.audio_url) && (isUserAudioContentMode.value || state.voice_identity_key === currentVoiceIdentityKey.value)
  }),
)
const fullShowGuideCurrentStep = computed(() => {
  if (fullShowGenerating.value) return 2
  if (fullShowAllFormalVoicesReady.value && fullShowVoiceIdentityConsistent.value) return 5
  if (fullShowFormalSelectedCount.value > 0) return 4
  if (fullShowResult.value) return 3
  if (fullShowPreviewReady.value) return 2
  return 1
})
const fullShowGuideSteps = computed(() => [
  {
    index: 1,
    title: '准备生成依据',
    description: '确认商品、活动、事实依据和主播风格',
    status: fullShowGuideCurrentStep.value > 1 ? 'done' : 'active',
  },
  {
    index: 2,
    title: '生成并复核文稿',
    description: '生成 A/B/C/D/E 变化稿，并自动检查事实与数字',
    status:
      fullShowGuideCurrentStep.value > 2
        ? 'done'
        : fullShowGuideCurrentStep.value === 2
          ? 'active'
          : 'pending',
  },
  {
    index: 3,
    title: '选择正式稿件',
    description: '逐套查看复核结果，决定哪些稿件进入声音生成',
    status:
      fullShowGuideCurrentStep.value > 3
        ? 'done'
        : fullShowGuideCurrentStep.value === 3
          ? 'active'
          : 'pending',
  },
  {
    index: 4,
    title: '生成并试听声音',
    description: '按选中稿件生成真实声音，逐条试听，不满意可单独重做',
    status:
      fullShowGuideCurrentStep.value > 4
        ? 'done'
        : fullShowGuideCurrentStep.value === 4
          ? 'active'
          : 'pending',
  },
  {
    index: 5,
    title: '保存版本并发布',
    description: '文字与版本关系进 MySQL，音频进 OSS，确认后再发布给 Core',
    status: fullShowGuideCurrentStep.value === 5 ? 'active' : 'pending',
  },
])
const fullShowGuideNextAction = computed(() => {
  if (fullShowGenerating.value) return '当前：等待文稿生成与自动复核完成'
  if (fullShowAllFormalVoicesReady.value) {
    return `已完成 ${fullShowVoiceReadyCount.value}/${fullShowFormalSelectedCount.value} 套正式稿声音；下一步：保存正式版本并发布`
  }
  if (fullShowFormalSelectedCount.value > 0) {
    return `已选 ${fullShowFormalSelectedCount.value} 套正式稿；声音完成 ${fullShowVoiceReadyCount.value}/${fullShowFormalSelectedCount.value}，继续逐稿生成并试听`
  }
  if (fullShowResult.value) return '下一步：查看 A/B/C/D/E 稿件，确定哪些稿件进入声音生成'
  if (fullShowPreviewReady.value) return '下一步：点击“生成直播智能体预览”生成正式文字预览'
  return '下一步：先检查本次生成依据，再整理生成结构'
})
const fullShowSourceSummary = computed(() => {
  const snapshot = fullShowResult.value?.context
  if (snapshot) {
    return {
      facts: snapshot.formal_facts?.length || 0,
      products: snapshot.product_links?.length || 0,
      benefits: snapshot.benefits?.length || 0,
      style: snapshot.anchor_style?.dimensions?.length || 0,
    }
  }
  return {
    facts: formalFacts.value.length,
    products: formalProductLinks.value.length,
    benefits: activeFormalBenefits.value.length,
    style: anchorStyleProfile.value?.dimensions?.length || 0,
  }
})
const fullShowRoundPreview = computed(() => {
  const variants = ['A稿', 'B稿', 'C稿', 'D稿', 'E稿']
  const openings = ['新人留人切入', '生活场景切入', '用户疑问切入', '产品价值切入', '老客信任切入']
  return Array.from({ length: Math.min(fullShowRoundCount.value, 12) }, (_, index) => ({
    index: index + 1,
    variant: variants[index % variants.length],
    opening: openings[index % openings.length],
    startMinute: index * fullShowRoundMinutes.value,
  }))
})

function formatKnowledgeDraft(analysis: LiveAgentPlanScriptAnalysis) {
  if (!analysis.facts?.length) return ''
  return analysis.facts.map((fact, index) => {
    const lines = [
      `${index + 1}. [${factCategoryLabel(fact.category)}] ${fact.key}：${fact.value}`,
    ]
    if (fact.source_quote) lines.push(`   原文依据：${fact.source_quote}`)
    if (fact.note) lines.push(`   备注：${fact.note}`)
    return lines.join('\n')
  }).join('\n\n')
}

function formatRhythmDraft(analysis: LiveAgentPlanScriptAnalysis) {
  if (!analysis.rhythm_nodes?.length) return ''
  return analysis.rhythm_nodes.map((node) => {
    const lines = [`${String(node.order).padStart(2, '0')}. ${node.title}`]
    if (node.goal) lines.push(`目标：${node.goal}`)
    if (node.must_cover?.length) lines.push(`必讲：${node.must_cover.join('；')}`)
    if (node.transition) lines.push(`转场：${node.transition}`)
    lines.push(`生成方式：${node.execution_mode === 'verbatim' ? '100%原话' : '按意思生成'}`)
    if (node.duration_seconds) lines.push(`建议时长：${node.duration_seconds}秒`)
    return lines.join('\n')
  }).join('\n\n')
}

function benefitStatusLabel(status: string) {
  if (status === 'active') return '生效中'
  if (status === 'draft') return '草稿'
  if (status === 'expired') return '已过期'
  if (status === 'disabled') return '已停用'
  return status || '未知'
}

function formatBenefitTime(value?: string) {
  if (!value) return '未设置'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function stopScriptAnalysisProgressTimer() {
  if (scriptAnalysisProgressTimer !== null) {
    window.clearInterval(scriptAnalysisProgressTimer)
    scriptAnalysisProgressTimer = null
  }
}

function startScriptAnalysisProgressTimer() {
  stopScriptAnalysisProgressTimer()
  scriptAnalysisProgressTimer = window.setInterval(() => {
    if (!scriptAnalyzing.value) return
    if (scriptAnalysisProgress.value < 88) {
      const step = scriptAnalysisProgress.value < 60 ? 4 : 2
      scriptAnalysisProgress.value = Math.min(88, scriptAnalysisProgress.value + step)
    }
  }, 800)
}
const activeConfig = computed(
  () => configVersions.value.find((item) => item.lifecycle_status === 'active') || null,
)
const latestConfig = computed(
  () =>
    configVersions.value.find((item) => item.lifecycle_status === 'draft') ||
    activeConfig.value ||
    configVersions.value[0] ||
    null,
)
const roomPolicyDraft = computed(
  () => roomPolicyContext.value?.l3_versions.find((item) => item.lifecycle_status === 'draft') || null,
)
const roomPolicyActive = computed(
  () => roomPolicyContext.value?.l3_versions.find((item) => item.lifecycle_status === 'active') || null,
)

const activeModeLabel = computed(() => {
  if (activeMode.value === 'products') return '商品链接'
  if (activeMode.value === 'benefits') return '活动福利'
  if (activeMode.value === 'knowledge') return '事实依据'
  if (activeMode.value === 'memory') return '互动策略'
  if (activeMode.value === 'addressing') return '称呼策略'
  if (activeMode.value === 'plan') return '兼容方案列表'
  if (activeMode.value === 'basic') return '基础设置'
  if (activeMode.value === 'strategy') return '用户层策略'
  if (activeMode.value === 'anchor') return '口播风格'
  if (activeMode.value === 'fullshow') return '直播智能体生成'
  return '主播声音'
})

function previewFullShowPlan() {
  fullShowPreviewReady.value = true
  fullShowNotice.value = `已整理 ${fullShowDuration.value} 分钟整场结构，预计 ${fullShowRoundCount.value} 个轮次；当前只做页面结构预览，不会保存也不会调用正式生成。`
}

function startFullShowVariantEdit() {
  if (isUserAudioContentMode.value) return
  const variant = selectedFullShowVariant.value
  if (!variant) return
  fullShowEditingVariantKey.value = variant.variant_key
  fullShowEditingText.value = variant.text
  fullShowEditError.value = ''
}

function cancelFullShowVariantEdit() {
  fullShowEditingVariantKey.value = ''
  fullShowEditingText.value = ''
  fullShowEditError.value = ''
  fullShowFactContextMenu.value.visible = false
  fullShowFactDialogOpen.value = false
}

function handleFullShowEditorContextMenu(event: MouseEvent) {
  const target = event.currentTarget as HTMLTextAreaElement | null
  if (!target) return
  const start = target.selectionStart ?? 0
  const end = target.selectionEnd ?? 0
  const selected = target.value.slice(Math.min(start, end), Math.max(start, end)).trim()
  if (!selected) {
    fullShowFactContextMenu.value.visible = false
    return
  }
  event.preventDefault()
  fullShowFactContextMenu.value = {
    visible: true,
    x: Math.min(event.clientX, Math.max(12, window.innerWidth - 190)),
    y: Math.min(event.clientY, Math.max(12, window.innerHeight - 84)),
    text: selected,
  }
}

function closeFullShowFactContextMenu() {
  fullShowFactContextMenu.value.visible = false
}

function openFullShowFactDialog() {
  const text = fullShowFactContextMenu.value.text.trim()
  if (!text) return
  const oneLine = text.replace(/\s+/g, ' ').trim()
  fullShowFactDraft.value = {
    category: 'other',
    key: oneLine.length > 28 ? oneLine.slice(0, 28) + '…' : oneLine,
    value: text,
  }
  fullShowFactError.value = ''
  fullShowFactContextMenu.value.visible = false
  fullShowFactDialogOpen.value = true
}

function closeFullShowFactDialog() {
  if (fullShowFactSaving.value) return
  fullShowFactDialogOpen.value = false
  fullShowFactError.value = ''
}

async function saveSelectedTextAsFormalFact() {
  const planId = currentRoomPlanId.value
  const variant = selectedFullShowVariant.value
  const key = fullShowFactDraft.value.key.trim()
  const value = fullShowFactDraft.value.value.trim()
  if (!planId || !variant || fullShowFactSaving.value) return
  if (!key || !value) {
    fullShowFactError.value = '事实名称和事实内容不能为空'
    return
  }
  fullShowFactSaving.value = true
  fullShowFactError.value = ''
  try {
    const candidate: LiveAgentPlanFactCandidate = {
      category: fullShowFactDraft.value.category,
      key,
      value,
      status: 'confirmed',
      review_bucket: 'adoptable',
      source_quote: fullShowFactContextMenu.value.text || value,
      confidence: 'high',
      note: '从完整主线稿人工选中文字添加',
    }
    const result = await adoptLiveAgentPlanFacts(
      planId,
      [candidate],
      activeRoom.value?.tenant_id,
      'fullshow:' + variant.variant_key + ':manual-selection',
    )
    await refreshFormalFacts()
    if (result.adopted > 0) {
      fullShowNotice.value = '已把选中文字添加为正式事实，后续重新生成或复核时会使用这条事实。'
      fullShowFactDialogOpen.value = false
      return
    }
    const first = result.results?.[0]
    fullShowFactError.value =
      first?.message ||
      (result.conflicts ? '这条事实与现有事实存在冲突，请先处理冲突。' : '这条事实已经存在，没有重复添加。')
  } catch (err) {
    fullShowFactError.value = err instanceof Error ? err.message : '添加正式事实失败'
  } finally {
    fullShowFactSaving.value = false
  }
}

function isFullShowFormalVariant(key: string) {
  return fullShowFormalVariantKeys.value.includes(key)
}

function toggleFullShowFormalVariant(variant: LiveAgentFullShowVariant) {
  if (!variant.audit?.passed) {
    fullShowNotice.value = `${variant.variant_key}稿还没有复核通过，请先修改并复核通过后再选为正式稿。`
    return
  }
  if (isFullShowFormalVariant(variant.variant_key)) {
    fullShowFormalVariantKeys.value = fullShowFormalVariantKeys.value.filter((key) => key !== variant.variant_key)
    fullShowSavedVersion.value = null
    fullShowNotice.value = `已取消 ${variant.variant_key}稿的正式稿选择；当前已选 ${fullShowFormalVariantKeys.value.length} 套。`
    return
  }
  fullShowFormalVariantKeys.value = [...fullShowFormalVariantKeys.value, variant.variant_key]
  fullShowSavedVersion.value = null
  fullShowNotice.value = `已将 ${variant.variant_key}稿选为正式稿；当前已选 ${fullShowFormalVariantKeys.value.length} 套，后续声音只按这些稿件生成。`
}

function defaultFullShowVoiceState(): FullShowVoiceState {
  return {
    status: 'idle',
    progress: 0,
    audio_url: '',
    audio_asset_id: 0,
    duration_ms: 0,
    timeline: [],
    srt: '',
    safe_points: [],
    asset_manifest: {},
    candidate_url: '',
    candidate_asset_id: 0,
    candidate_duration_ms: 0,
    candidate_timeline: [],
    candidate_srt: '',
    candidate_safe_points: [],
    candidate_asset_manifest: {},
    candidate_voice_identity: '',
    candidate_voice_identity_version: '',
    candidate_voice_identity_key: '',
    error: '',
    generation_no: 0,
    voice_identity: currentVoiceIdentity.value.name,
    voice_identity_version: currentVoiceIdentity.value.version,
    voice_identity_key: currentVoiceIdentityKey.value,
  }
}

function fullShowVoiceState(key: string) {
  return fullShowVoiceStates.value[key] || defaultFullShowVoiceState()
}

function updateFullShowVoiceState(key: string, patch: Partial<FullShowVoiceState>) {
  fullShowVoiceStates.value = {
    ...fullShowVoiceStates.value,
    [key]: {
      ...defaultFullShowVoiceState(),
      ...fullShowVoiceStates.value[key],
      ...patch,
    },
  }
}

function formatFullShowTimelineMS(value: number) {
  const totalSeconds = Math.max(0, Math.floor(Number(value || 0) / 1000))
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  return String(minutes).padStart(2, '0') + ':' + String(seconds).padStart(2, '0')
}

function updateFullShowVoicePlayback(key: string, event: Event) {
  const audio = event.currentTarget as HTMLAudioElement | null
  if (!audio) return
  fullShowVoicePlaybackMS.value = {
    ...fullShowVoicePlaybackMS.value,
    [key]: Math.max(0, Math.round(audio.currentTime * 1000)),
  }
}

function isFullShowTimelineSegmentActive(key: string, segment: LiveAgentPlanTimelineSegment) {
  const current = Number(fullShowVoicePlaybackMS.value[key] || 0)
  return current >= segment.start_ms && current < segment.end_ms
}

function toggleFullShowVoiceDetail(key: string, mode: 'srt' | 'safe') {
  if (mode === 'srt' && !(fullShowSubtitleDrafts.value[key]?.length)) {
    fullShowSubtitleDrafts.value = {
      ...fullShowSubtitleDrafts.value,
      [key]: fullShowVoiceState(key).timeline.map((item) => ({ ...item })),
    }
  }
  fullShowSubtitleError.value = ''
  fullShowVoiceDetail.value =
    fullShowVoiceDetail.value.key === key && fullShowVoiceDetail.value.mode === mode
      ? { key: '', mode: '' }
      : { key, mode }
}

function selectFullShowOutputMode(mode: 'generated' | 'custom') {
  if (
    mode === 'generated' &&
    (customMainlineStage.value === 'uploading' || customMainlineStage.value === 'analyzing')
  ) {
    customMainlineAbort?.abort()
  }
  fullShowOutputMode.value = mode
  if (mode === 'generated' && selectedFullShowVariantKey.value === 'CUSTOM') {
    selectedFullShowVariantKey.value = aiFullShowVariants.value[0]?.variant_key || 'A'
  }
  if (mode === 'generated' && isUserAudioContentMode.value && !contentModeSaving.value) {
    void chooseContentMode('ai_pregenerated')
  }
  customMainlineError.value = ''
}

const customMainlineCanApply = computed(() =>
  Boolean(
    customMainlineDraft.value?.audio_asset_id &&
      customMainlineDraft.value?.srt &&
      customMainlineDraft.value?.timeline?.length &&
      customMainlineDraft.value?.safe_points?.length &&
      customMainlineDraft.value?.voice_identity?.voice_id,
  ),
)

function openCustomMainline() {
  selectFullShowOutputMode('custom')
}

async function saveFullShowSubtitleText(key: string) {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const state = fullShowVoiceState(key)
  const draft = fullShowSubtitleDrafts.value[key]
  if (!planId || !room || !state.audio_asset_id || !draft?.length) return
  fullShowSubtitleSavingKey.value = key
  fullShowSubtitleError.value = ''
  try {
    const input = {
      room_id: room.id,
      audio_asset_id: state.audio_asset_id,
      timeline: draft.map((item) => ({ ...item })),
    }
    const response = isUserAudioContentMode.value
      ? await rebuildLiveAgentCustomMainline(planId, input)
      : await rebuildLiveAgentFullShowVariantSubtitles(planId, key, { ...input, tenant_id: room.tenant_id })
    updateFullShowVoiceState(key, {
      audio_url: response.audio_url,
      audio_asset_id: response.audio_asset_id,
      duration_ms: response.duration_ms,
      timeline: response.timeline.map((item) => ({ ...item })),
      srt: response.srt,
      safe_points: response.safe_points.map((item) => ({ ...item })),
      asset_manifest: { ...response.asset_manifest },
      progress: 100,
      status: 'ready',
      error: '',
    })
    fullShowSubtitleDrafts.value = {
      ...fullShowSubtitleDrafts.value,
      [key]: response.timeline.map((item) => ({ ...item })),
    }
    if (fullShowResult.value) {
      const nextText = response.timeline.map((item) => item.text.trim()).filter(Boolean).join('\n')
      fullShowResult.value = {
        ...fullShowResult.value,
        variants: fullShowResult.value.variants.map((item) =>
          item.variant_key === key ? { ...item, text: nextText } : item,
        ),
      }
    }
    if (!fullShowManualEditedKeys.value.includes(key)) {
      fullShowManualEditedKeys.value = [...fullShowManualEditedKeys.value, key]
    }
    fullShowSavedVersion.value = null
    fullShowVersionError.value = ''
    fullShowNotice.value = isUserAudioContentMode.value
      ? key + '稿 SRT 文字已保存；原始录音和时间位置保持不变。'
      : key + '稿 SRT 文字已保存；时间位置保持不变，声音资产已生成新副本。'
  } catch (err) {
    fullShowSubtitleError.value = err instanceof Error ? err.message : '保存 SRT 文字失败'
  } finally {
    fullShowSubtitleSavingKey.value = ''
  }
}

function readCustomMainlineDuration(file: File) {
  return new Promise<number>((resolve) => {
    const objectURL = URL.createObjectURL(file)
    const audio = document.createElement('audio')
    const finish = (value: number) => {
      URL.revokeObjectURL(objectURL)
      audio.removeAttribute('src')
      audio.load()
      resolve(value)
    }
    audio.preload = 'metadata'
    audio.onloadedmetadata = () => {
      const duration = Number(audio.duration || 0)
      finish(Number.isFinite(duration) && duration > 0 ? Math.round(duration * 1000) : 0)
    }
    audio.onerror = () => finish(0)
    audio.src = objectURL
  })
}

function encodePCM16WAV(buffer: AudioBuffer) {
  const channel = buffer.getChannelData(0)
  const bytes = new ArrayBuffer(44 + channel.length * 2)
  const view = new DataView(bytes)
  const writeASCII = (offset: number, value: string) => {
    for (let index = 0; index < value.length; index += 1) view.setUint8(offset + index, value.charCodeAt(index))
  }
  writeASCII(0, 'RIFF')
  view.setUint32(4, 36 + channel.length * 2, true)
  writeASCII(8, 'WAVE')
  writeASCII(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, buffer.sampleRate, true)
  view.setUint32(28, buffer.sampleRate * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  writeASCII(36, 'data')
  view.setUint32(40, channel.length * 2, true)
  let offset = 44
  for (let index = 0; index < channel.length; index += 1) {
    const value = Math.max(-1, Math.min(1, channel[index] || 0))
    view.setInt16(offset, value < 0 ? value * 0x8000 : value * 0x7fff, true)
    offset += 2
  }
  return new Blob([bytes], { type: 'audio/wav' })
}

async function createCustomMainlineVoiceSample(file: File) {
  try {
    const context = new AudioContext()
    try {
      const decoded = await context.decodeAudioData(await file.arrayBuffer())
      const duration = Math.min(Math.max(decoded.duration, 0), 12)
      if (duration <= 0.5) return null
      const sampleRate = 16000
      const offline = new OfflineAudioContext(1, Math.ceil(duration * sampleRate), sampleRate)
      const source = offline.createBufferSource()
      source.buffer = decoded
      source.connect(offline.destination)
      source.start(0, 0, duration)
      const rendered = await offline.startRendering()
      const blob = encodePCM16WAV(rendered)
      const stem = file.name.replace(/\.[^.]+$/, '') || 'custom-mainline'
      return new File([blob], stem + '-voice-sample.wav', { type: 'audio/wav' })
    } finally {
      await context.close().catch(() => undefined)
    }
  } catch {
    return null
  }
}

async function handleCustomMainlineFile(event: Event) {
  const input = event.currentTarget as HTMLInputElement
  const file = input.files?.[0] || null
  customMainlineError.value = ''
  customMainlineDraft.value = null
  customMainlineTimeline.value = []
  customMainlineVoiceSample.value = null
  customMainlineUploadProgress.value = 0
  customMainlineStage.value = 'idle'
  if (!file) {
    customMainlineFile.value = null
    customMainlineVoiceSample.value = null
    customMainlineDurationMS.value = 0
    return
  }
  if (file.size > 200 * 1024 * 1024) {
    customMainlineFile.value = null
    customMainlineVoiceSample.value = null
    customMainlineDurationMS.value = 0
    customMainlineError.value = '自定义音稿最大支持 200MB。'
    input.value = ''
    return
  }
  customMainlineFile.value = file
  const [durationMS, voiceSample] = await Promise.all([
    readCustomMainlineDuration(file),
    createCustomMainlineVoiceSample(file),
  ])
  customMainlineDurationMS.value = durationMS
  customMainlineVoiceSample.value = voiceSample
}

async function uploadCustomMainline() {
  if (contentModeAccess.value?.content_mode !== 'user_audio') await chooseContentMode('user_audio')
  selectFullShowOutputMode('custom')
  if (!contentModeReady.value || !isUserAudioContentMode.value) {
    customMainlineError.value = '当前直播间暂不能使用原始音，请刷新页面后重试。'
    return
  }
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const file = customMainlineFile.value
  if (!planId || !room || !file) {
    customMainlineError.value = '请选择当前方案、直播间和要上传的音频。'
    return
  }
  customMainlineError.value = ''
  customMainlineUploadProgress.value = 0
  customMainlineStage.value = 'uploading'
  customMainlineAbort?.abort()
  customMainlineAbort = new AbortController()
  try {
    const response = await uploadLiveAgentCustomMainline(
      planId,
      room.id,
      file,
      customMainlineDurationMS.value,
      customMainlineVoiceSample.value,
      (loaded, total) => {
        const percent = total > 0 ? Math.min(100, Math.round((loaded / total) * 100)) : 0
        customMainlineUploadProgress.value = percent
        if (percent >= 100) customMainlineStage.value = 'analyzing'
      },
      customMainlineAbort.signal,
    )
    customMainlineDraft.value = response
    customMainlineTimeline.value = (response.timeline || []).map((item) => ({ ...item }))
    customMainlineTranscript.value = String(
      response.transcript || (response.timeline || []).map((item) => item.text).filter(Boolean).join('\n'),
    ).trim()
    customMainlineSaveNotice.value = ''
    customMainlineUploadProgress.value = 100
    customMainlineStage.value = 'ready'
    if (response.clone_error) {
      customMainlineError.value = '音稿与 SRT 已完成，但自动生成自定义音色失败：' + response.clone_error
    }
  } catch (err) {
    if (err instanceof DOMException && err.name === 'AbortError') {
      customMainlineStage.value = 'idle'
      return
    }
    customMainlineStage.value = 'failed'
    customMainlineError.value = err instanceof Error ? err.message : '自定义音稿处理失败'
  } finally {
    customMainlineAbort = null
  }
}

function buildOriginalTimeline(
  transcript: string,
  timeline: LiveAgentPlanTimelineSegment[],
  durationMS: number,
) {
  const text = String(transcript || '').trim()
  const source = (timeline || []).map((item, index) => ({
    ...item,
    segment_id: String(item.segment_id || 'ORIGINAL-' + String(index + 1)),
    index: Number(item.index || index + 1),
  }))

  if (!text) return []
  if (!source.length) {
    return [{ segment_id: 'ORIGINAL-1', index: 1, start_ms: 0, end_ms: Number(durationMS || 0), text, safe_cut: true }]
  }
  const paragraphs = text.split(/\n+/).map((item) => item.trim()).filter(Boolean)
  if (paragraphs.length === source.length) return source.map((item, index) => ({ ...item, text: paragraphs[index] }))
  const characters = text.replace(/\s+/g, ' ').trim().match(/\S\s*/gu) || []
  const count = Math.min(source.length, characters.length)
  const selected = source.slice(0, count)
  const weights = selected.map((item) => Math.max(1, Array.from(String(item.text || '')).length))
  const totalWeight = weights.reduce((sum, value) => sum + value, 0)
  let usedWeight = 0
  let cursor = 0
  return selected.map((item, index) => {
    usedWeight += weights[index]
    const remaining = count - index - 1
    const minimumEnd = cursor + 1
    const maximumEnd = characters.length - remaining
    let end = index === count - 1 ? characters.length : Math.round((characters.length * usedWeight) / totalWeight)
    end = Math.max(minimumEnd, Math.min(maximumEnd, end))
    if (index < count - 1) {
      for (let candidate = end; candidate < Math.min(maximumEnd, end + 32); candidate += 1) {
        if (/[。！？!?；;]/.test(characters[candidate] || '')) { end = candidate + 1; break }
      }
    }
    const segmentText = characters.slice(cursor, end).join('').trim()
    cursor = end
    return {
      ...item,
      index: index + 1,
      end_ms: index === count - 1 ? Number(durationMS || source[source.length - 1]?.end_ms || item.end_ms || 0) : Number(item.end_ms || 0),
      text: segmentText,
    }
  })
}

async function rebuildCustomMainline() {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const draft = customMainlineDraft.value
  if (!planId || !room || !draft) return
  const transcript = customMainlineTranscript.value.trim()
  if (!transcript) {
    customMainlineError.value = '原始话术不能为空。'
    customMainlineSaveNotice.value = ''
    return
  }
  customMainlineStage.value = 'saving'
  customMainlineError.value = ''
  try {
    const response = await rebuildLiveAgentCustomMainline(planId, {
      room_id: room.id,
      audio_asset_id: draft.audio_asset_id,
      timeline: buildOriginalTimeline(transcript, customMainlineTimeline.value, draft.duration_ms),
    })
    customMainlineDraft.value = {
      ...response,
      voice_profile: response.voice_profile || draft.voice_profile,
      voice_identity: response.voice_identity || draft.voice_identity,
      clone_error: response.clone_error || draft.clone_error,
    }
    customMainlineTimeline.value = response.timeline.map((item) => ({ ...item }))
    customMainlineTranscript.value = String(
      response.transcript || response.timeline.map((item) => item.text).filter(Boolean).join('\n'),
    ).trim()
    if (fullShowResult.value) {
      fullShowResult.value = {
        ...fullShowResult.value,
        variants: fullShowResult.value.variants.map((variant) =>
          variant.variant_key === 'CUSTOM'
            ? { ...variant, text: customMainlineTranscript.value }
            : variant,
        ),
      }
    }
    if (fullShowVoiceStates.value.CUSTOM) {
      updateFullShowVoiceState('CUSTOM', {
        timeline: response.timeline.map((item) => ({ ...item })),
        srt: response.srt,
        safe_points: response.safe_points.map((item) => ({ ...item })),
        asset_manifest: { ...response.asset_manifest },
      })
    }
    fullShowSavedVersion.value = null
    customMainlineStage.value = 'ready'
    customMainlineSaveNotice.value = '原始话术已保存，SRT 已按原声音时间同步更新。'
  } catch (err) {
    customMainlineStage.value = 'failed'
    customMainlineError.value = err instanceof Error ? err.message : '保存校对并重建 SRT 失败'
    customMainlineSaveNotice.value = ''
  }
}

async function retryCustomMainlineClone() {
  const draft = customMainlineDraft.value
  if (!draft?.audio_asset_id || customMainlineCloneBusy.value) return
  customMainlineCloneBusy.value = true
  customMainlineError.value = ''
  try {
    const profile = await cloneLiveVoiceProfile({
      name: (customMainlineFile.value?.name || '自定义主线') + ' · 自定义音色',
      sample_asset_id: draft.audio_asset_id,
    })
    const model = String(profile.config?.target_model || 'qwen-audio-3.0-tts-plus')
    customMainlineDraft.value = {
      ...draft,
      voice_profile: profile,
      voice_identity: {
        name: profile.name,
        version: 'V1',
        source: 'clone',
        provider: profile.provider,
        voice_id: profile.voice_id,
        profile_id: profile.id,
        binding_id: Number(profile.config?.binding_id || 0) || undefined,
        model,
        rate: 1,
      },
      clone_error: '',
    }
  } catch (err) {
    customMainlineError.value = err instanceof Error ? err.message : '重新生成自定义音色失败'
  } finally {
    customMainlineCloneBusy.value = false
  }
}

async function applyCustomMainlineAsFormal() {
  if (contentModeAccess.value?.content_mode !== 'user_audio') await chooseContentMode('user_audio')
  selectFullShowOutputMode('custom')
  if (!contentModeReady.value || !isUserAudioContentMode.value) return
  const draft = customMainlineDraft.value
  const identity = draft?.voice_identity
  if (!draft || !identity || !currentPlan.value || !activeRoom.value || !customMainlineCanApply.value) {
    customMainlineError.value = '自定义音稿、SRT 和自定义音色都准备完成后才能替换主线。'
    return
  }
  // Uploaded recordings can form a complete workspace without first buying an
  // unrelated AI-generated script preview.
  const result: LiveAgentFullShowPreviewResponse = fullShowResult.value || {
    variants: [], persisted: false, preview: true,
    context: {
      plan_id: currentPlan.value.id,
      plan_name: currentPlan.value.name,
      plan_description: currentPlan.value.description,
      room_id: activeRoom.value.id,
      policy_rule_count: 0,
      formal_facts: formalFacts.value.map((item) => ({ category: item.category, key: item.key, value: item.value, forbidden_wording: item.forbidden_wording, safe_rewrite: item.safe_rewrite, version: item.version_no })),
      benefits: activeFormalBenefits.value,
      product_links: formalProductLinks.value,
      script_references: [],
      rhythm_nodes: [],
      anchor_style: anchorStyleProfile.value || { summary: '', dimensions: [], reusable_rules: [], candidate_patterns: [], excluded_from_style: [] },
      style_overlay_prompt: '',
      style_overlay_count: 0,
      duration_minutes: fullShowDuration.value,
      round_minutes: fullShowRoundMinutes.value,
      round_count: fullShowRoundCount.value,
      variant_count: 1,
      use_anchor_style: true,
      use_dynamic_facts: true,
      generate_tts_hints: false,
      avoid_recent: true,
      draft_product_source: false,
      draft_rhythm_source: false,
      draft_style_source: false,
    },
  }
  const variants = result.variants.filter((item) => item.variant_key !== 'CUSTOM')
  const customVariant: LiveAgentFullShowVariant = {
    index: variants.length + 1,
    variant_key: 'CUSTOM',
    title: '原始音话术',
    opening_angle: '上传原始声音',
    text: customMainlineTranscript.value.trim() || draft.transcript,
    estimated_minutes: Math.max(1, Math.ceil(draft.duration_ms / 60000)),
    covered_fact_keys: [],
    covered_link_keys: [],
    tts_hints: [],
    audit: {
      passed: true,
      issues: [],
      fact_coverage_pct: 0,
      link_coverage_pct: 0,
      similarity_pct: 0,
    },
  }
  fullShowResult.value = {
    ...result,
    variants: [...variants, customVariant],
  }
  const identityKey = fullShowVoiceIdentityKey(identity)
  fullShowVoiceStates.value = {
    ...fullShowVoiceStates.value,
    CUSTOM: {
      ...defaultFullShowVoiceState(),
      status: 'ready',
      progress: 100,
      audio_url: draft.audio_url,
      audio_asset_id: draft.audio_asset_id,
      duration_ms: draft.duration_ms,
      timeline: draft.timeline.map((item) => ({ ...item })),
      srt: draft.srt,
      safe_points: draft.safe_points.map((item) => ({ ...item })),
      asset_manifest: { ...draft.asset_manifest },
      generation_no: 1,
      voice_identity: identity.name,
      voice_identity_version: identity.version,
      voice_identity_key: identityKey,
    },
  }
  fullShowFormalVariantKeys.value = ['CUSTOM']
  selectedFullShowVariantKey.value = 'CUSTOM'
  fullShowWorkspaceVoiceIdentity.value = { ...identity }
  fullShowSavedVersion.value = null
  fullShowVersionError.value = ''
  fullShowPreviewReady.value = true
  originalAiSrtProgress.value = { status: 'idle', percent: 0, stage: '', error: '' }
  fullShowNotice.value = `已采用上传的原始声音；原始音频会持续保留，也可以使用当前选中的声音生成 AI 版本。`
  fullShowOutputMode.value = 'custom'
}

function originalSourceManifest() {
  const draft = customMainlineDraft.value
  if (!draft) return null
  return {
    audio_url: draft.audio_url,
    audio_asset_id: Number(draft.audio_asset_id || 0),
    duration_ms: Number(draft.duration_ms || 0),
    transcript: customMainlineTranscript.value.trim() || draft.transcript,
    timeline: draft.timeline.map((item) => ({ ...item })),
    srt: draft.srt,
    safe_points: draft.safe_points.map((item) => ({ ...item })),
    asset_manifest: JSON.parse(JSON.stringify(draft.asset_manifest || {})),
    voice_identity: draft.voice_identity ? JSON.parse(JSON.stringify(draft.voice_identity)) : {},
  }
}

const liveAgentVersionRequestLimitBytes = 64 * 1024
const liveAgentVersionSafePayloadBytes = 60 * 1024

function bytesToBase64(bytes: Uint8Array) {
  let binary = ''
  const chunkSize = 0x8000
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize))
  }
  return window.btoa(binary)
}

function base64ToBytes(value: string) {
  const binary = window.atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
  return bytes
}

async function compressVersionJSON(value: Record<string, unknown>) {
  if (typeof CompressionStream !== 'function') return ''
  const stream = new Blob([JSON.stringify(value)]).stream().pipeThrough(new CompressionStream('gzip'))
  const compressed = new Uint8Array(await new Response(stream).arrayBuffer())
  return bytesToBase64(compressed)
}

async function decompressVersionJSON(value: string) {
  if (!value || typeof DecompressionStream !== 'function') return null
  const bytes = base64ToBytes(value)
  const buffer = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
  const stream = new Blob([buffer]).stream().pipeThrough(new DecompressionStream('gzip'))
  return JSON.parse(await new Response(stream).text()) as Record<string, unknown>
}

async function compactOriginalSourceManifest() {
  const source = originalSourceManifest()
  if (!source) return null
  const { transcript, timeline, srt, safe_points, ...reference } = source
  try {
    const data = await compressVersionJSON({ transcript, timeline, srt, safe_points })
    if (!data) return source
    return {
      ...reference,
      compact_bundle: {
        version: 'original-source-v1',
        encoding: 'gzip-base64',
        data,
      },
    }
  } catch {
    return source
  }
}

async function expandOriginalSourceManifest(source: Record<string, unknown>) {
  const bundle = asRecord(source.compact_bundle)
  if (bundle.encoding !== 'gzip-base64' || typeof bundle.data !== 'string') return source
  try {
    const expanded = await decompressVersionJSON(bundle.data)
    return expanded ? { ...source, ...expanded } : source
  } catch {
    return source
  }
}

async function generateOriginalAiVoice() {
  const draft = customMainlineDraft.value
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const text = customMainlineTranscript.value.trim() || draft?.transcript || ''
  if (!draft || !planId || !room || !text) {
    customMainlineError.value = '请先完成原始声音上传和话术校对。'
    return
  }
  if (!fullShowResult.value) await applyCustomMainlineAsFormal()
  if (!fullShowResult.value) return
  if (contentModeAccess.value?.content_mode === 'user_audio') await chooseContentMode('ai_pregenerated')
  selectFullShowOutputMode('custom')
  if (contentModeAccess.value?.content_mode === 'user_audio') {
    customMainlineError.value = '当前直播间暂不能切换为 AI 声音模式。'
    return
  }
  const identity = roomVoiceIdentity.value
  const targetKey = aiFullShowVariants.value.find((item) => item.variant_key === 'A')?.variant_key || 'A'
  const existing = fullShowVoiceState(targetKey)
  const sourceVariant = aiFullShowVariants.value.find((item) => item.variant_key === targetKey)
  const variant: LiveAgentFullShowVariant = {
    index: sourceVariant?.index || 1,
    variant_key: targetKey,
    title: '原始话术 AI 声音版',
    opening_angle: '原始话术',
    text,
    estimated_minutes: Math.max(1, Math.ceil(draft.duration_ms / 60000)),
    covered_fact_keys: sourceVariant?.covered_fact_keys || [],
    covered_link_keys: sourceVariant?.covered_link_keys || [],
    tts_hints: sourceVariant?.tts_hints || [],
    audit: sourceVariant?.audit || { passed: true, issues: [], fact_coverage_pct: 0, link_coverage_pct: 0, similarity_pct: 0 },
  }
  const base = fullShowResult.value
  if (base) {
    fullShowResult.value = {
      ...base,
      variants: [...base.variants.filter((item) => item.variant_key !== targetKey), variant]
        .sort((left, right) => left.index - right.index),
    }
  }
  originalAiCandidateKey.value = targetKey
  originalAiSrtProgress.value = { status: 'idle', percent: 0, stage: '', error: '' }
  customMainlineError.value = ''
  startFullShowVoiceProgress(targetKey)
  try {
    const response = await generateLiveAgentFullShowVariantVoice(planId, targetKey, {
      tenant_id: room.tenant_id,
      room_id: room.id,
      text,
      source: String(identity.source || 'official'),
      voice_id: String(identity.voice_id || ''),
      profile_id: Number(identity.profile_id || 0) || undefined,
      rate: identity.rate || 1,
    })
    stopFullShowVoiceTimer(targetKey)
    updateFullShowVoiceState(targetKey, {
      ...existing,
      status: 'candidate',
      progress: 100,
      candidate_url: response.audio_url,
      candidate_asset_id: Number(response.audio_asset_id || 0),
      candidate_duration_ms: Number(response.duration_ms || 0),
      candidate_timeline: response.timeline || [],
      candidate_srt: response.srt || '',
      candidate_safe_points: response.safe_points || [],
      candidate_asset_manifest: response.asset_manifest || {},
      candidate_voice_identity: identity.name,
      candidate_voice_identity_version: identity.version,
      candidate_voice_identity_key: fullShowVoiceIdentityKey(identity),
      error: '',
    })
    fullShowNotice.value = 'AI 候选声音已生成，请试听后点击“采用 AI 声音”。原始声音仍完整保留。'
  } catch (err) {
    stopFullShowVoiceTimer(targetKey)
    const message = err instanceof Error ? err.message : 'AI 声音生成失败'
    updateFullShowVoiceState(targetKey, { status: 'failed', error: message })
    customMainlineError.value = message
  }
}

async function acceptOriginalAiVoice() {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const key = originalAiCandidateKey.value
  const state = key ? fullShowVoiceState(key) : null
  if (!planId || !room || !key || !state?.candidate_asset_id || !state.candidate_timeline.length) {
    customMainlineError.value = 'AI 候选声音还没有完整时间轴，暂时不能采用。'
    return
  }
  originalAiSrtProgress.value = { status: 'generating', percent: 12, stage: '正在读取 AI 声音时间轴', error: '' }
  const timer = window.setInterval(() => {
    const current = originalAiSrtProgress.value
    if (current.status !== 'generating') return
    const percent = Math.min(88, current.percent + 6)
    originalAiSrtProgress.value = {
      ...current,
      percent,
      stage: percent < 42 ? '正在读取 AI 声音时间轴' : percent < 76 ? '正在生成对应 SRT' : '正在校验字幕时间',
    }
  }, 180)
  try {
    const response = await rebuildLiveAgentFullShowVariantSubtitles(planId, key, {
      tenant_id: room.tenant_id,
      room_id: room.id,
      audio_asset_id: state.candidate_asset_id,
      timeline: state.candidate_timeline.map((item) => ({ ...item })),
    })
    updateFullShowVoiceState(key, {
      candidate_timeline: response.timeline || state.candidate_timeline,
      candidate_srt: response.srt || state.candidate_srt,
      candidate_safe_points: response.safe_points || state.candidate_safe_points,
      candidate_asset_manifest: {
        ...(response.asset_manifest || state.candidate_asset_manifest),
        original_source: originalSourceManifest(),
      },
    })
    adoptFullShowVoiceCandidate(key)
    fullShowFormalVariantKeys.value = [key]
    selectedFullShowVariantKey.value = key
    fullShowWorkspaceVoiceIdentity.value = { ...roomVoiceIdentity.value }
    fullShowSavedVersion.value = null
    originalAiSrtProgress.value = { status: 'done', percent: 100, stage: 'SRT 已生成，可以展开查看', error: '' }
    fullShowNotice.value = '已采用 AI 声音并完成对应 SRT；上传的原始声音仍完整保留，可随时切回。'
  } catch (err) {
    const message = err instanceof Error ? err.message : '生成对应 SRT 失败'
    originalAiSrtProgress.value = { status: 'failed', percent: 0, stage: 'SRT 生成失败', error: message }
    customMainlineError.value = message
  } finally {
    window.clearInterval(timer)
  }
}

function discardOriginalAiVoice() {
  if (originalAiCandidateKey.value) discardFullShowVoiceCandidate(originalAiCandidateKey.value)
  originalAiCandidateKey.value = ''
  originalAiSrtProgress.value = { status: 'idle', percent: 0, stage: '', error: '' }
  fullShowNotice.value = '已放弃本次 AI 声音，原始声音保持不变。'
}

function stopFullShowVoiceTimer(key: string) {
  const timer = fullShowVoiceTimers.get(key)
  if (timer !== undefined) {
    window.clearInterval(timer)
    fullShowVoiceTimers.delete(key)
  }
}

function invalidateFullShowVariantArtifacts(key: string) {
  const normalized = String(key || '').trim()
  if (!normalized) return
  if (fullShowSetListening.value) stopFormalVoiceSet()
  fullShowFormalVariantKeys.value = fullShowFormalVariantKeys.value.filter((item) => item !== normalized)
  stopFullShowVoiceTimer(normalized)
  const next = { ...fullShowVoiceStates.value }
  delete next[normalized]
  fullShowVoiceStates.value = next
  const playback = { ...fullShowVoicePlaybackMS.value }
  delete playback[normalized]
  fullShowVoicePlaybackMS.value = playback
  if (fullShowVoiceDetail.value.key === normalized) fullShowVoiceDetail.value = { key: '', mode: '' }
  fullShowSavedVersion.value = null
  fullShowVersionError.value = ''
}

function startFullShowVoiceProgress(key: string) {
  stopFullShowVoiceTimer(key)
  updateFullShowVoiceState(key, { status: 'generating', progress: 8, error: '' })
  const timer = window.setInterval(() => {
    const current = fullShowVoiceState(key)
    if (current.status !== 'generating') {
      stopFullShowVoiceTimer(key)
      return
    }
    const next = current.progress < 45
      ? current.progress + 5
      : current.progress < 75
        ? current.progress + 3
        : current.progress < 92
          ? current.progress + 1
          : current.progress
    updateFullShowVoiceState(key, { progress: Math.min(92, next) })
  }, 1000)
  fullShowVoiceTimers.set(key, timer)
}

async function generateFullShowVariantVoice(variant: LiveAgentFullShowVariant, regenerate = false) {
  if (!contentModeReady.value || isUserAudioContentMode.value) {
    fullShowNotice.value = '原始录音主线保留上传的声音；需要更换时请重新上传录音。'
    return
  }
  if (!isFullShowFormalVariant(variant.variant_key)) {
    fullShowNotice.value = '请先把这篇稿件选为正式稿，再生成声音。'
    return
  }
  if (!variant.audit?.passed) {
    fullShowNotice.value = '这篇稿件还没有复核通过，请先修改并复核。'
    return
  }
  const generatedIdentity = currentVoiceIdentity.value
  const source = String(generatedIdentity.source || '').trim()
  const voiceID = String(generatedIdentity.voice_id || '').trim()
  const profileID = Number(generatedIdentity.profile_id || 0)
  const existing = fullShowVoiceState(variant.variant_key)
  if (existing.status === 'generating') return
  fullShowSavedVersion.value = null
  startFullShowVoiceProgress(variant.variant_key)
  const generatedIdentityKey = currentVoiceIdentityKey.value
  try {
    const planId = currentRoomPlanId.value
    const room = activeRoom.value
    if (!planId || !room) throw new Error('当前直播间或直播方案不存在')
    const response = await generateLiveAgentFullShowVariantVoice(planId, variant.variant_key, {
      tenant_id: room.tenant_id,
      room_id: room.id,
      text: variant.text,
      source: source || 'official',
      voice_id: voiceID,
      profile_id: profileID || undefined,
      rate: generatedIdentity.rate || 1,
    })
    stopFullShowVoiceTimer(variant.variant_key)
    if (regenerate && existing.audio_url) {
      updateFullShowVoiceState(variant.variant_key, {
        status: 'candidate',
        progress: 100,
        candidate_url: response.audio_url,
        candidate_asset_id: Number(response.audio_asset_id || 0),
        candidate_duration_ms: Number(response.duration_ms || 0),
        candidate_timeline: response.timeline || [],
        candidate_srt: response.srt || '',
        candidate_safe_points: response.safe_points || [],
        candidate_asset_manifest: response.asset_manifest || {},
        candidate_voice_identity: generatedIdentity.name,
        candidate_voice_identity_version: generatedIdentity.version,
        candidate_voice_identity_key: generatedIdentityKey,
        error: '',
        generation_no: existing.generation_no + 1,
      })
      fullShowNotice.value = `${variant.variant_key}稿的新声音已经生成，请试听新旧声音后决定是否替换。`
      return
    }
    updateFullShowVoiceState(variant.variant_key, {
      status: 'ready',
      progress: 100,
      audio_url: response.audio_url,
      audio_asset_id: Number(response.audio_asset_id || 0),
      duration_ms: Number(response.duration_ms || 0),
      timeline: response.timeline || [],
      srt: response.srt || '',
      safe_points: response.safe_points || [],
      asset_manifest: response.asset_manifest || {},
      candidate_url: '',
      candidate_asset_id: 0,
      candidate_duration_ms: 0,
      candidate_timeline: [],
      candidate_srt: '',
      candidate_safe_points: [],
      candidate_asset_manifest: {},
      candidate_voice_identity: '',
      candidate_voice_identity_version: '',
      candidate_voice_identity_key: '',
      error: '',
      generation_no: Math.max(1, existing.generation_no + 1),
      voice_identity: generatedIdentity.name,
      voice_identity_version: generatedIdentity.version,
      voice_identity_key: generatedIdentityKey,
    })
    fullShowNotice.value = `${variant.variant_key}稿声音已生成，可以直接试听；当前仍是声音预览，尚未发布到 Core。`
  } catch (err) {
    stopFullShowVoiceTimer(variant.variant_key)
    updateFullShowVoiceState(variant.variant_key, {
      status: 'failed',
      error: err instanceof Error ? err.message : '声音生成失败',
    })
  }
}

function adoptFullShowVoiceCandidate(key: string) {
  if (!contentModeReady.value || isUserAudioContentMode.value) return
  const state = fullShowVoiceState(key)
  if (!state.candidate_url) return
  updateFullShowVoiceState(key, {
    status: 'ready',
    audio_url: state.candidate_url,
    audio_asset_id: state.candidate_asset_id,
    duration_ms: state.candidate_duration_ms,
    timeline: state.candidate_timeline,
    srt: state.candidate_srt,
    safe_points: state.candidate_safe_points,
    asset_manifest: state.candidate_asset_manifest,
    candidate_url: '',
    candidate_asset_id: 0,
    candidate_duration_ms: 0,
    candidate_timeline: [],
    candidate_srt: '',
    candidate_safe_points: [],
    candidate_asset_manifest: {},
    candidate_voice_identity: '',
    candidate_voice_identity_version: '',
    candidate_voice_identity_key: '',
    progress: 100,
    error: '',
    voice_identity: state.candidate_voice_identity || state.voice_identity,
    voice_identity_version: state.candidate_voice_identity_version || state.voice_identity_version,
    voice_identity_key: state.candidate_voice_identity_key || state.voice_identity_key,
  })
  fullShowSavedVersion.value = null
  fullShowNotice.value = key + '稿已采用新声音；当前只更新页面中的声音预览，正式版本将在最后发布步骤保存。'
}

function discardFullShowVoiceCandidate(key: string) {
  const state = fullShowVoiceState(key)
  updateFullShowVoiceState(key, {
    status: state.audio_url ? 'ready' : 'idle',
    candidate_url: '',
    candidate_asset_id: 0,
    candidate_duration_ms: 0,
    candidate_timeline: [],
    candidate_srt: '',
    candidate_safe_points: [],
    candidate_asset_manifest: {},
    candidate_voice_identity: '',
    candidate_voice_identity_version: '',
    candidate_voice_identity_key: '',
    progress: state.audio_url ? 100 : 0,
    error: '',
  })
}

function stopFormalVoiceSet() {
  fullShowSetListening.value = false
  fullShowSetListeningKey.value = ''
  if (fullShowSetAudio) {
    fullShowSetAudio.pause()
    fullShowSetAudio.removeAttribute('src')
    fullShowSetAudio.load()
    fullShowSetAudio = null
  }
  const resolve = fullShowSetResolve
  fullShowSetResolve = null
  resolve?.()
}

async function previewFormalVoiceSet() {
  if (fullShowSetListening.value) {
    stopFormalVoiceSet()
    fullShowNotice.value = '已停止整套声音试听。'
    return
  }
  const items = fullShowFormalVariantKeys.value
    .map((key) => ({ key, url: fullShowVoiceState(key).audio_url }))
    .filter((item) => Boolean(item.url))
  if (!items.length) {
    fullShowNotice.value = '请先生成正式稿声音，再试听整套声音。'
    return
  }
  const audio = new Audio()
  fullShowSetAudio = audio
  fullShowSetListening.value = true
  for (let index = 0; index < items.length && fullShowSetListening.value; index += 1) {
    const item = items[index]
    fullShowSetListeningKey.value = item.key
    selectedFullShowVariantKey.value = item.key
    fullShowNotice.value = `整套试听 ${index + 1}/${items.length}：正在播放 ${item.key}稿声音。`
    await new Promise<void>((resolve) => {
      let finished = false
      const done = () => {
        if (finished) return
        finished = true
        audio.onended = null
        audio.onerror = null
        if (fullShowSetResolve === done) fullShowSetResolve = null
        resolve()
      }
      fullShowSetResolve = done
      audio.onended = done
      audio.onerror = done
      audio.src = liveSupportMediaPlaybackURL(item.url)
      audio.load()
      void audio.play().catch(done)
    })
  }
  const completed = fullShowSetListening.value
  if (fullShowSetAudio === audio) fullShowSetAudio = null
  fullShowSetResolve = null
  fullShowSetListening.value = false
  fullShowSetListeningKey.value = ''
  if (completed) {
    fullShowNotice.value = `整套试听完成：已连续播放 ${items.length} 条正式稿声音。`
  }
}

async function refreshFullShowVersionHistory() {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  if (!planId || !room) {
    fullShowVersionHistory.value = []
    return
  }
  try {
    const result = await getLiveAgentPlanVersions(planId, room.id, room.tenant_id)
    fullShowVersionHistory.value = result.items || []
  } catch {
    fullShowVersionHistory.value = []
  }
}

let fullShowWorkspaceLoadToken = 0

function resetFullShowWorkspaceState() {
  stopFormalVoiceSet()
  for (const key of fullShowVoiceTimers.keys()) stopFullShowVoiceTimer(key)
  fullShowResult.value = null
  fullShowPreviewReady.value = false
  selectedFullShowVariantKey.value = 'A'
  fullShowEditingVariantKey.value = ''
  fullShowEditingText.value = ''
  fullShowEditError.value = ''
  fullShowRegenerateError.value = ''
  fullShowManualEditedKeys.value = []
  fullShowFormalVariantKeys.value = []
  fullShowVoiceStates.value = {}
  fullShowVoicePlaybackMS.value = {}
  fullShowVoiceDetail.value = { key: '', mode: '' }
  fullShowOutputMode.value = isUserAudioContentMode.value ? 'custom' : 'generated'
  fullShowSubtitleDrafts.value = {}
  fullShowSubtitleSavingKey.value = ''
  fullShowSubtitleError.value = ''
  customMainlineFile.value = null
  customMainlineVoiceSample.value = null
  customMainlineDurationMS.value = 0
  customMainlineUploadProgress.value = 0
  customMainlineStage.value = 'idle'
  customMainlineDraft.value = null
  customMainlineTimeline.value = []
  customMainlineTranscript.value = ''
  customMainlineSaveNotice.value = ''
  customMainlineError.value = ''
  originalAiCandidateKey.value = ''
  originalAiSrtProgress.value = { status: 'idle', percent: 0, stage: '', error: '' }
  fullShowSavedVersion.value = null
  fullShowWorkspaceBaseVersion.value = null
  fullShowWorkspaceSource.value = ''
  fullShowWorkspaceVoiceIdentity.value = null
  fullShowWorkspaceLegacyPartial.value = false
  fullShowWorkspaceInherited.value = false
  fullShowVersionError.value = ''
  fullShowNotice.value = ''
  fullShowError.value = ''
  fullShowDuration.value = 90
  fullShowRoundMinutes.value = 7
  fullShowVariantCount.value = 5
  fullShowExpansionFreedom.value = 65
  fullShowUseAnchorStyle.value = true
  fullShowUseDynamicFacts.value = true
  fullShowGenerateTTS.value = true
  fullShowAvoidRecent.value = true
}

async function restoreFullShowWorkspaceVersion(version: LiveAgentPlanVersion, source: 'draft' | 'published') {
  fullShowWorkspaceHydrating.value = true
  try {
    const rawContext = version.generation_context as unknown as LiveAgentFullShowGenerationContext
    const context = {
      ...rawContext,
      plan_id: Number(rawContext?.plan_id || version.plan_id),
      plan_name: String(rawContext?.plan_name || currentPlan.value?.name || ''),
      plan_description: String(rawContext?.plan_description || currentPlan.value?.description || ''),
      room_id: Number(rawContext?.room_id || version.room_id),
      policy_rule_count: Number(rawContext?.policy_rule_count || 0),
      style_overlay_prompt: String(rawContext?.style_overlay_prompt || ''),
      style_overlay_count: Number(rawContext?.style_overlay_count || 0),
      fact_expansion: rawContext?.fact_expansion,
      formal_facts: Array.isArray(rawContext?.formal_facts) ? rawContext.formal_facts : [],
      product_links: Array.isArray(rawContext?.product_links) ? rawContext.product_links : [],
      benefits: Array.isArray(rawContext?.benefits) ? rawContext.benefits : [],
      script_references: Array.isArray(rawContext?.script_references) ? rawContext.script_references : [],
      rhythm_nodes: Array.isArray(rawContext?.rhythm_nodes) ? rawContext.rhythm_nodes : [],
      anchor_style: {
        summary: String(rawContext?.anchor_style?.summary || ''),
        dimensions: Array.isArray(rawContext?.anchor_style?.dimensions)
          ? rawContext.anchor_style.dimensions
          : [],
        reusable_rules: Array.isArray(rawContext?.anchor_style?.reusable_rules)
          ? rawContext.anchor_style.reusable_rules
          : [],
        candidate_patterns: Array.isArray(rawContext?.anchor_style?.candidate_patterns)
          ? rawContext.anchor_style.candidate_patterns
          : [],
        excluded_from_style: Array.isArray(rawContext?.anchor_style?.excluded_from_style)
          ? rawContext.anchor_style.excluded_from_style
          : [],
      },
      duration_minutes: version.duration_minutes,
      round_minutes: version.round_minutes,
      round_count: Math.max(1, Math.ceil(version.duration_minutes / Math.max(1, version.round_minutes))),
      variant_count: version.variants.length,
      use_anchor_style:
        typeof rawContext?.use_anchor_style === 'boolean'
          ? rawContext.use_anchor_style
          : Boolean(rawContext?.draft_style_source),
      use_dynamic_facts:
        typeof rawContext?.use_dynamic_facts === 'boolean' ? rawContext.use_dynamic_facts : true,
      generate_tts_hints:
        typeof rawContext?.generate_tts_hints === 'boolean' ? rawContext.generate_tts_hints : true,
      avoid_recent: typeof rawContext?.avoid_recent === 'boolean' ? rawContext.avoid_recent : true,
    } as LiveAgentFullShowGenerationContext

    const variants: LiveAgentFullShowVariant[] = version.variants.map((item, index) => ({
      index: Number(item.index || index + 1),
      variant_key: String(item.variant_key || String.fromCharCode(65 + index)).trim(),
      title: String(item.title || item.variant_key + '稿').trim(),
      opening_angle: String(item.opening_angle || '版本恢复').trim(),
      text: String(item.text || '').trim(),
      estimated_minutes: Number(item.estimated_minutes || version.round_minutes),
      covered_fact_keys: item.covered_fact_keys || [],
      covered_link_keys: item.covered_link_keys || [],
      tts_hints: item.tts_hints || [],
      audit: item.audit || {
        passed: true,
        issues: [],
        fact_coverage_pct: 0,
        link_coverage_pct: 0,
        similarity_pct: 0,
      },
    }))

    fullShowDuration.value = version.duration_minutes
    fullShowRoundMinutes.value = version.round_minutes
    fullShowVariantCount.value = Math.min(5, Math.max(3, variants.length))
    fullShowExpansionFreedom.value = Math.min(100, Math.max(0, Number(context.fact_expansion?.freedom ?? 65)))
    fullShowWorkspaceLegacyPartial.value =
      Number(rawContext?.variant_count || version.variants.length) > version.variants.length
    fullShowUseAnchorStyle.value = context.use_anchor_style
    fullShowUseDynamicFacts.value = context.use_dynamic_facts
    fullShowGenerateTTS.value = context.generate_tts_hints
    fullShowAvoidRecent.value = context.avoid_recent
    fullShowResult.value = {
      context,
      variants,
      persisted: true,
      preview: false,
    }
    selectedFullShowVariantKey.value = variants[0]?.variant_key || 'A'
    fullShowFormalVariantKeys.value = version.variants.filter((item) => item.is_formal).map((item) => item.variant_key)
    fullShowWorkspaceVoiceIdentity.value = { ...version.voice_identity }
    const restoredIdentityKey = fullShowVoiceIdentityKey(version.voice_identity)
    const restoredStates: Record<string, FullShowVoiceState> = {}
    for (const item of version.variants) {
      if (!item.is_formal || !item.audio_asset_id || !item.audio_url) continue
      restoredStates[item.variant_key] = {
        ...defaultFullShowVoiceState(),
        status: 'ready',
        progress: 100,
        audio_url: item.audio_url,
        audio_asset_id: Number(item.audio_asset_id || 0),
        duration_ms: Number(item.audio_duration_ms || 0),
        timeline: item.timeline || [],
        srt: item.srt || '',
        safe_points: item.safe_points || [],
        asset_manifest: item.asset_manifest || {},
        generation_no: Number(item.generation_no || 1),
        voice_identity: version.voice_identity.name,
        voice_identity_version: version.voice_identity.version,
        voice_identity_key: item.voice_identity_key || restoredIdentityKey,
      }
    }
    fullShowVoiceStates.value = restoredStates
    const originalOwner = version.variants.find((item) =>
      Object.keys(asRecord(asRecord(item.asset_manifest).original_source)).length > 0,
    ) || version.variants.find((item) => item.variant_key === 'CUSTOM')
    const archivedOriginal = await expandOriginalSourceManifest(
      asRecord(asRecord(originalOwner?.asset_manifest).original_source),
    )
    const original = Object.keys(archivedOriginal).length ? archivedOriginal : asRecord(originalOwner)
    const originalAssetID = Number(original.audio_asset_id || 0)
    const originalAudioURL = String(original.audio_url || '')
    if (originalAssetID > 0 && originalAudioURL) {
      const originalTimeline = Array.isArray(original.timeline)
        ? (original.timeline as LiveAgentPlanTimelineSegment[]).map((item) => ({ ...item }))
        : []
      const originalSafePoints = Array.isArray(original.safe_points)
        ? (original.safe_points as LiveAgentPlanSafePoint[]).map((item) => ({ ...item }))
        : []
      const archivedIdentity = asRecord(original.voice_identity)
      customMainlineDraft.value = {
        audio_asset_id: originalAssetID,
        audio_url: originalAudioURL,
        duration_ms: Number(original.duration_ms || original.audio_duration_ms || 0),
        transcript: String(original.transcript || originalOwner?.text || ''),
        timeline: originalTimeline,
        srt: String(original.srt || ''),
        safe_points: originalSafePoints,
        asset_manifest: asRecord(original.asset_manifest),
        voice_identity: Object.keys(archivedIdentity).length
          ? (archivedIdentity as unknown as LiveAgentVoiceIdentity)
          : { ...version.voice_identity },
      }
      customMainlineTimeline.value = originalTimeline
      customMainlineTranscript.value = String(
        original.transcript || originalOwner?.text || originalTimeline.map((item) => item.text).filter(Boolean).join('\n'),
      ).trim()
      customMainlineStage.value = 'ready'
    }
    fullShowWorkspaceBaseVersion.value = version
    fullShowWorkspaceSource.value = source
    fullShowSavedVersion.value = version
    fullShowPreviewReady.value = true
    fullShowNotice.value =
      source === 'draft'
        ? `已恢复待发布 V${version.version_no} 的编辑工作区，可继续修改、替换声音后保存新版本。`
        : `已基于当前已发布 V${version.version_no} 恢复编辑副本；这里的修改不会影响正在使用的版本，保存后会生成新版本。`
  } finally {
    fullShowWorkspaceHydrating.value = false
  }
}

async function loadFullShowWorkspace() {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const token = ++fullShowWorkspaceLoadToken
  resetFullShowWorkspaceState()
  fullShowWorkspaceLoading.value = false
  if (!planId || !room) return
  fullShowWorkspaceLoading.value = true
  try {
    const workspace = await getLiveAgentPlanWorkspace(planId, room.id, room.tenant_id)
    if (token !== fullShowWorkspaceLoadToken || currentRoomPlanId.value !== planId || activeRoom.value?.id !== room.id) return
    if (!workspace.version || !workspace.source) {
      fullShowNotice.value = '当前方案还没有保存过智能体版本，可以从现有方案依据开始生成第一版。'
      return
    }
    await restoreFullShowWorkspaceVersion(workspace.version, workspace.source)
    if (workspace.inherited) {
      fullShowWorkspaceInherited.value = true
      fullShowSavedVersion.value = null
      fullShowNotice.value =
        `已从同一智能体方案已有的 V${workspace.version.version_no} 恢复内容作为当前直播间编辑副本；保存后会生成“${room.name}”自己的新版本，不会改动其它直播间正在使用的版本。`
    }
  } catch (err) {
    if (token !== fullShowWorkspaceLoadToken) return
    fullShowError.value = err instanceof Error ? err.message : '恢复直播智能体编辑工作区失败'
  } finally {
    if (token === fullShowWorkspaceLoadToken) fullShowWorkspaceLoading.value = false
  }
}

function cloneVersionJSON<T>(value: T, fallback: T): T {
  try { return JSON.parse(JSON.stringify(value ?? fallback)) as T } catch { return fallback }
}

function cleanVersionVoiceIdentity(identity: LiveAgentVoiceIdentity) {
  const rate = Number(identity.rate)
  return {
    name: String(identity.name || ''),
    version: String(identity.version || ''),
    source: String(identity.source || ''),
    provider: String(identity.provider || ''),
    voice_id: String(identity.voice_id || ''),
    profile_id: Number(identity.profile_id || 0) || undefined,
    binding_id: Number(identity.binding_id || 0) || undefined,
    model: String(identity.model || ''),
    rate: Number.isFinite(rate) && rate > 0 ? rate : 1,
  }
}

function cleanVersionTimeline(timeline: LiveAgentPlanTimelineSegment[]) {
  return (timeline || []).map((item, index) => ({
    segment_id: String(item.segment_id || ''),
    index: Number(item.index ?? index),
    start_ms: Number(item.start_ms || 0),
    end_ms: Number(item.end_ms || 0),
    text: String(item.text || ''),
    safe_cut: Boolean(item.safe_cut),
  }))
}

function cleanVersionSafePoints(points: LiveAgentPlanSafePoint[]) {
  return (points || []).map((item) => ({
    id: String(item.id || ''),
    cut_ms: Number(item.cut_ms || 0),
    score: Number(item.score || 0),
    grade: String(item.grade || ''),
    kind: String(item.kind || ''),
    sentence_id: String(item.sentence_id || ''),
    left_preview: String(item.left_preview || ''),
    next_preview: String(item.next_preview || ''),
    topics: Array.isArray(item.topics) ? item.topics.map((topic) => String(topic)) : [],
  }))
}

async function saveFullShowVersion(): Promise<LiveAgentPlanVersion | null> {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const result = fullShowResult.value
  if (!planId || !room || !result || fullShowVersionSaving.value) return null
  fullShowVersionError.value = ''
  if (!contentModeReady.value || contentModeLoading.value || contentModeSaving.value) {
    fullShowVersionError.value = '请先确认当前直播间的可用内容模式。'
    return null
  }
  if (!fullShowFormalVariantKeys.value.length) {
    fullShowVersionError.value = '请先选择至少一套正式稿。'
    return null
  }
  if (!fullShowAllFormalVoicesReady.value) {
    fullShowVersionError.value = '还有正式稿没有生成声音，不能保存版本。'
    return null
  }
  if (!fullShowVoiceIdentityConsistent.value) {
    fullShowVersionError.value = '正式稿声音身份不一致，请用当前声音身份重新生成不一致的稿件。'
    return null
  }
  const originalSource = await compactOriginalSourceManifest()
  const originalSourceOwnerKey = fullShowFormalVariantKeys.value[0] || ''
  const variants = result.variants.filter((variant) =>
    fullShowFormalVariantKeys.value.includes(variant.variant_key),
  ).map((variant) => {
    const isFormal = fullShowFormalVariantKeys.value.includes(variant.variant_key)
    const voice = fullShowVoiceState(variant.variant_key)
    const assetManifest = isFormal
      ? {
          ...cloneVersionJSON(voice.asset_manifest || {}, {}),
          ...(originalSource && variant.variant_key === originalSourceOwnerKey
            ? { original_source: originalSource }
            : {}),
        }
      : {}
    return {
      index: Math.max(1, Math.round(Number(variant.index || 1))),
      variant_key: variant.variant_key,
      is_formal: isFormal,
      title: variant.title,
      opening_angle: variant.opening_angle,
      text: variant.text,
      estimated_minutes: Math.max(1, Math.round(Number(variant.estimated_minutes || 1))),
      covered_fact_keys: variant.covered_fact_keys || [],
      covered_link_keys: variant.covered_link_keys || [],
      tts_hints: (variant.tts_hints || []).map((item) => ({
        segment: String(item.segment || ''),
        instruction: String(item.instruction || ''),
        rate: Number.isFinite(Number(item.rate)) ? Number(item.rate) : undefined,
      })),
      audit: {
        passed: Boolean(variant.audit?.passed),
        issues: (variant.audit?.issues || []).map((item) => ({
          severity: String(item.severity || ''),
          code: String(item.code || ''),
          message: String(item.message || ''),
        })),
        fact_coverage_pct: Math.round(Number(variant.audit?.fact_coverage_pct || 0)),
        link_coverage_pct: Math.round(Number(variant.audit?.link_coverage_pct || 0)),
        similarity_pct: Math.round(Number(variant.audit?.similarity_pct || 0)),
      },
      audio_url: isFormal ? voice.audio_url : '',
      audio_asset_id: isFormal ? Math.round(Number(voice.audio_asset_id || 0)) || undefined : undefined,
      audio_duration_ms: isFormal ? Math.round(Number(voice.duration_ms || 0)) || undefined : undefined,
      timeline: isFormal ? cleanVersionTimeline(voice.timeline) : [],
      srt: isFormal ? voice.srt : '',
      safe_points: isFormal ? cleanVersionSafePoints(voice.safe_points) : [],
      asset_manifest: assetManifest,
      generation_no: isFormal ? Math.max(1, Math.round(Number(voice.generation_no || 1))) : 0,
      voice_identity_key: isFormal ? voice.voice_identity_key : '',
    }
  })
  fullShowVersionSaving.value = true
  try {
    const payload = {
      tenant_id: Math.round(Number(room.tenant_id || 0)) || undefined,
      room_id: Math.round(Number(room.id || 0)),
      duration_minutes: Math.round(Number(fullShowDuration.value || 0)),
      round_minutes: Math.round(Number(fullShowRoundMinutes.value || 0)),
      voice_identity: cleanVersionVoiceIdentity(currentVoiceIdentity.value),
      variants,
      generation_context: {
        content_mode: contentModeAccess.value?.content_mode,
        duration_minutes: Math.round(Number(fullShowDuration.value || 0)),
        round_minutes: Math.round(Number(fullShowRoundMinutes.value || 0)),
        round_count: Math.max(1, Math.ceil(fullShowDuration.value / Math.max(1, fullShowRoundMinutes.value))),
        variant_count: variants.length,
        expansion_freedom: fullShowExpansionFreedom.value,
        use_anchor_style: fullShowUseAnchorStyle.value,
        use_dynamic_facts: fullShowUseDynamicFacts.value,
        generate_tts_hints: fullShowGenerateTTS.value,
        avoid_recent: fullShowAvoidRecent.value,
      } as Record<string, unknown>,
    }
    const payloadBytes = new TextEncoder().encode(JSON.stringify(payload)).byteLength
    if (payloadBytes > liveAgentVersionSafePayloadBytes) {
      throw new Error(
        `当前版本保存数据为 ${Math.ceil(payloadBytes / 1024)}KB，超过接口可安全接收的 60KB（上限 64KB）。请减少正式话术数量或缩短话术后再保存；原始声音不会被删除。`,
      )
    }
    const saved = await createLiveAgentPlanVersion(planId, payload)
    fullShowSavedVersion.value = saved
    fullShowWorkspaceBaseVersion.value = saved
    fullShowWorkspaceSource.value = 'draft'
    fullShowWorkspaceInherited.value = false
    fullShowNotice.value = `已保存“${room.name}”直播智能体 V${saved.version_no}，当前还是待发布版本，不会影响直播间正在运行的版本。`
    await refreshFullShowVersionHistory()
    return saved
  } catch (err) {
    const message = err instanceof Error ? err.message : '保存直播智能体版本失败'
    fullShowVersionError.value = message.includes('保存智能体版本格式错误')
      ? `保存请求未通过接口格式校验。这不是声音、SRT 或语义分析失败；接口最多接收 ${Math.round(liveAgentVersionRequestLimitBytes / 1024)}KB，请刷新页面后重新保存。`
      : message
    return null
  } finally {
    fullShowVersionSaving.value = false
  }
}

async function publishFullShowVersion() {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const result = fullShowResult.value
  if (!planId || !room || !result || fullShowVersionPublishing.value || fullShowVersionSaving.value) return
  fullShowVersionError.value = ''
  const version = fullShowSavedVersion.value
  if (!version || version.lifecycle_status === 'published') {
    fullShowVersionError.value = version?.lifecycle_status === 'published'
      ? '当前版本已经发布；有修改时请先点击 S5 保存新版本，再点击 S6 发布。'
      : '请先点击 S5 保存新版本；保存成功后再点击 S6 发布到直播间。'
    return
  }
  if (!fullShowFormalVariantKeys.value.length) {
    fullShowVersionError.value = '请先选择至少一套正式稿。'
    return
  }
  if (!fullShowAllFormalVoicesReady.value) {
    fullShowVersionError.value = '还有正式稿没有生成声音，不能发布。'
    return
  }
  if (!fullShowVoiceIdentityConsistent.value) {
    fullShowVersionError.value = '当前声音身份与已生成正式稿不一致，不能发布。'
    return
  }
  fullShowVersionPublishing.value = true
  try {
    const published = await publishLiveAgentPlanVersion(planId, version.id, {
      room_id: room.id,
      tenant_id: room.tenant_id,
    })
    fullShowSavedVersion.value = published
    fullShowWorkspaceBaseVersion.value = published
    fullShowWorkspaceSource.value = 'published'
    fullShowWorkspaceInherited.value = false
    fullShowNotice.value = `已将直播智能体 V${published.version_no} 发布到“${room.name}”。发布不会自动开播，回直播间点击开始后再运行这套版本。`
    await Promise.all([refreshFullShowVersionHistory(), refreshCurrentRoomPlan()])
  } catch (err) {
    fullShowVersionError.value = err instanceof Error ? err.message : '发布直播智能体版本失败'
  } finally {
    fullShowVersionPublishing.value = false
  }
}

async function saveFullShowVariantEdit() {
  if (isUserAudioContentMode.value) return
  const result = fullShowResult.value
  const variant = selectedFullShowVariant.value
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const text = fullShowEditingText.value.trim()
  if (!result || !variant || !planId || !room || fullShowEditSaving.value) return
  if (!text) {
    fullShowEditError.value = '主线稿不能为空'
    return
  }
  const contentChanged = text !== variant.text.trim()
  fullShowEditSaving.value = true
  fullShowEditError.value = ''
  try {
    const variants = result.variants.map((item) => ({
      ...item,
      text: item.variant_key === variant.variant_key ? text : item.text,
    }))
    const audited = await auditLiveAgentFullShowPreview(planId, {
      tenant_id: room.tenant_id,
      room_id: room.id,
      round_minutes: fullShowRoundMinutes.value,
      use_dynamic_facts: fullShowUseDynamicFacts.value,
      avoid_recent: fullShowAvoidRecent.value,
      variants,
    })
    fullShowResult.value = {
      ...result,
      variants: audited.variants,
    }
    if (contentChanged) {
      invalidateFullShowVariantArtifacts(variant.variant_key)
    }
    if (!fullShowManualEditedKeys.value.includes(variant.variant_key)) {
      fullShowManualEditedKeys.value = [...fullShowManualEditedKeys.value, variant.variant_key]
    }
    fullShowEditingVariantKey.value = ''
    fullShowEditingText.value = ''
    const updated = audited.variants.find((item) => item.variant_key === variant.variant_key)
    if (!contentChanged && updated && !updated.audit?.passed && isFullShowFormalVariant(variant.variant_key)) {
      fullShowFormalVariantKeys.value = fullShowFormalVariantKeys.value.filter((key) => key !== variant.variant_key)
    }
    fullShowNotice.value = updated?.audit?.passed
      ? contentChanged
        ? `${variant.variant_key}稿已修改并重新复核通过；旧正式稿状态和旧声音已清空，请重新确认正式稿并生成声音。`
        : `${variant.variant_key}稿已重新复核通过。当前仍是预览，不会自动保存或发布。`
      : `${variant.variant_key}稿已修改并重新复核，但仍有需要处理的问题，请按红色提示继续修改。`
  } catch (err) {
    fullShowEditError.value = err instanceof Error ? err.message : '保存修改并重新复核失败'
  } finally {
    fullShowEditSaving.value = false
  }
}

async function regenerateFullShowVariant(variant: LiveAgentFullShowVariant) {
  if (!contentModeReady.value || isUserAudioContentMode.value) return
  const result = fullShowResult.value
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  if (!result || !planId || !room || fullShowRegeneratingVariantKey.value) return
  const analysis = currentAnalysis.value
  fullShowRegeneratingVariantKey.value = variant.variant_key
  fullShowRegenerateError.value = ''
  fullShowNotice.value = `正在只重新生成 ${variant.variant_key}稿，其它稿件保持不变。`
  try {
    const recentTexts = fullShowAvoidRecent.value
      ? result.variants.slice(0, 5).map((item) => item.text)
      : []
    const regenerated = await regenerateLiveAgentFullShowVariant(planId, variant.variant_key, {
      tenant_id: room.tenant_id,
      room_id: room.id,
      duration_minutes: fullShowDuration.value,
      round_minutes: fullShowRoundMinutes.value,
      expansion_freedom: fullShowExpansionFreedom.value,
      use_anchor_style: fullShowUseAnchorStyle.value,
      use_dynamic_facts: fullShowUseDynamicFacts.value,
      generate_tts_hints: fullShowGenerateTTS.value,
      avoid_recent: fullShowAvoidRecent.value,
      product_links: analysis?.product_links || [],
      rhythm_nodes: analysis?.rhythm_nodes || [],
      anchor_style: anchorStyleProfile.value || {
        summary: '',
        dimensions: [],
        reusable_rules: [],
        candidate_patterns: [],
        excluded_from_style: [],
      },
      variants: result.variants,
      recent_texts: recentTexts,
    })
    invalidateFullShowVariantArtifacts(variant.variant_key)
    fullShowResult.value = {
      ...result,
      variants: regenerated.variants,
      provider: regenerated.provider || result.provider,
      model: regenerated.model || result.model,
      latency_ms: regenerated.latency_ms ?? result.latency_ms,
    }
    fullShowEditingVariantKey.value = ''
    fullShowEditingText.value = ''
    fullShowEditError.value = ''
    if (!fullShowManualEditedKeys.value.includes(variant.variant_key)) {
      fullShowManualEditedKeys.value = [...fullShowManualEditedKeys.value, variant.variant_key]
    }
    fullShowNotice.value = regenerated.variant.audit?.passed
      ? `${variant.variant_key}稿已单独重新生成并复核通过；其它稿件未改变。旧正式稿状态和旧声音已清空，请重新确认后生成声音。`
      : `${variant.variant_key}稿已单独重新生成，其它稿件未改变；自动复核仍发现问题，请继续修改或再次重新生成本稿。`
  } catch (err) {
    fullShowRegenerateError.value = err instanceof Error ? err.message : `${variant.variant_key}稿重新生成失败`
    fullShowNotice.value = ''
  } finally {
    fullShowRegeneratingVariantKey.value = ''
  }
}

function stopFullShowGenerationProgressTimer() {
  if (fullShowGenerationProgressTimer !== null) {
    window.clearInterval(fullShowGenerationProgressTimer)
    fullShowGenerationProgressTimer = null
  }
}

function startFullShowGenerationProgress(mode = '文字预览') {
  stopFullShowGenerationProgressTimer()
  fullShowGenerationStartedAt = Date.now()
  fullShowGenerationProgress.value = {
    visible: true,
    mode,
    percent: 6,
    stage: '准备生成',
    detail: '正在检查当前方案、正式商品、有效活动、事实依据和生成参数',
    failed: false,
  }
  fullShowGenerationProgressTimer = window.setInterval(() => {
    const current = fullShowGenerationProgress.value.percent
    const elapsedSeconds = Math.max(0, Math.floor((Date.now() - fullShowGenerationStartedAt) / 1000))
    let next = current
    if (current < 86) {
      next = Math.min(86, current + (current < 30 ? 4 : current < 64 ? 2 : 1))
    } else {
      next = Math.max(
        current,
        Math.min(96, 86 + Math.max(0, Math.floor((elapsedSeconds - 40) / 15))),
      )
    }
    let stage = '编译生成依据'
    let detail = '正在汇总规则层、行业层、事实依据、商品链接、活动福利和主播风格'
    if (next >= 30 && next < 72) {
      stage = '生成变化稿'
      detail = `正在生成 ${fullShowVariantCount.value} 套可播变化稿，并控制事实边界与近期重复`
    } else if (next >= 72 && next < 86) {
      stage = '复核生成结果'
      detail = '正在检查数字、价格、规格、库存、链接和重复表达；有问题会自动尝试修复'
    } else if (next >= 86) {
      stage = '等待模型最终结果'
      detail =
        `正在完成自动复核与必要修复，已等待 ${elapsedSeconds} 秒；模型返回后会立即跳到 100%`
    }
    fullShowGenerationProgress.value = {
      ...fullShowGenerationProgress.value,
      percent: next,
      stage,
      detail,
    }
  }, 1000)
}

function completeFullShowGenerationProgress(detail: string) {
  stopFullShowGenerationProgressTimer()
  fullShowGenerationProgress.value = {
    ...fullShowGenerationProgress.value,
    visible: true,
    percent: 100,
    stage: '生成完成',
    detail,
    failed: false,
  }
}

function failFullShowGenerationProgress(detail: string) {
  stopFullShowGenerationProgressTimer()
  fullShowGenerationProgress.value = {
    ...fullShowGenerationProgress.value,
    visible: true,
    stage: '生成失败',
    detail,
    failed: true,
  }
}

async function generateFullShowPreview() {
  if (fullShowGenerating.value) return
  if (contentModeAccess.value?.content_mode === 'user_audio') await chooseContentMode('ai_pregenerated')
  selectFullShowOutputMode('generated')
  if (!contentModeReady.value || contentModeLoading.value || contentModeSaving.value || isUserAudioContentMode.value) {
    fullShowError.value = '当前直播间暂不能切换到 AI 话术模式，请刷新后重试。'
    return
  }
  const planId = currentRoomPlanId.value
  if (!planId) {
    fullShowError.value = '当前直播间还没有绑定直播方案'
    return
  }
  const analysis = currentAnalysis.value
  fullShowSavedVersion.value = null
  fullShowVersionError.value = ''
  fullShowGenerating.value = true
  fullShowError.value = ''
  fullShowNotice.value = '正在编译当前直播方案并生成变化稿，本次只生成预览，不会保存或发送给 Core。'
  startFullShowGenerationProgress('文字预览')
  try {
    const recentTexts = fullShowAvoidRecent.value
      ? (fullShowResult.value?.variants || []).slice(0, 5).map((item) => item.text)
      : []
    const result = await previewGenerateLiveAgentFullShow(planId, {
      tenant_id: activeRoom.value?.tenant_id,
      room_id: activeRoom.value?.id,
      duration_minutes: fullShowDuration.value,
      round_minutes: fullShowRoundMinutes.value,
      variant_count: fullShowVariantCount.value,
      expansion_freedom: fullShowExpansionFreedom.value,
      use_anchor_style: fullShowUseAnchorStyle.value,
      use_dynamic_facts: fullShowUseDynamicFacts.value,
      generate_tts_hints: fullShowGenerateTTS.value,
      avoid_recent: fullShowAvoidRecent.value,
      product_links: analysis?.product_links || [],
      rhythm_nodes: analysis?.rhythm_nodes || [],
      anchor_style: anchorStyleProfile.value || {
        summary: '',
        dimensions: [],
        reusable_rules: [],
        candidate_patterns: [],
        excluded_from_style: [],
      },
      recent_texts: recentTexts,
    })
    fullShowResult.value = result
    selectedFullShowVariantKey.value = result.variants[0]?.variant_key || 'A'
    fullShowEditingVariantKey.value = ''
    fullShowEditingText.value = ''
    fullShowEditError.value = ''
    fullShowManualEditedKeys.value = []
    fullShowFormalVariantKeys.value = []
    for (const key of fullShowVoiceTimers.keys()) stopFullShowVoiceTimer(key)
    fullShowVoiceStates.value = {}
    fullShowPreviewReady.value = true
    const passed = result.variants.filter((item) => item.audit?.passed).length
    fullShowNotice.value = `整场话术预览已生成：${result.variants.length} 套变化稿，自动复核通过 ${passed} 套。当前结果未保存、未发布、未进入 Core。`
    completeFullShowGenerationProgress(
      `已完成 ${result.variants.length} 套文字变化稿，自动复核通过 ${passed} 套；当前仅为预览。`,
    )
  } catch (err) {
    fullShowError.value = err instanceof Error ? err.message : '整场话术生成失败'
    fullShowNotice.value = ''
    failFullShowGenerationProgress(fullShowError.value)
  } finally {
    fullShowGenerating.value = false
  }
}

const versionLabel = computed(() => {
  if (activeMode.value === 'strategy') {
    if (roomPolicyDraft.value) return '用户层草稿 V' + roomPolicyDraft.value.version_no
    if (roomPolicyActive.value) return '用户层已发布 V' + roomPolicyActive.value.version_no
    return '用户层未版本化'
  }
  const latest = latestConfig.value
  if (!latest) return '未版本化'
  if (latest.lifecycle_status === 'draft') return '草稿 V' + latest.version_no
  if (latest.lifecycle_status === 'active') return '已发布 V' + latest.version_no
  return 'V' + latest.version_no + ' · ' + latest.lifecycle_status
})

function settingsToInput(value: LiveAgentSettings): LiveAgentSettingsInput {
  return {
    display_name: value.display_name,
    role_name: value.role_name,
    self_introduction: value.self_introduction,
    mission: value.mission,
    greeting: value.greeting,
  }
}

async function refreshRoomPolicy() {
  const roomId = activeRoomId.value
  if (!roomId) {
    roomPolicyContext.value = null
    return
  }
  try {
    roomPolicyContext.value = await getLiveRoomPolicyContext(roomId)
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取直播间用户层策略失败'
    }
  }
}

async function refreshLivePlans() {
  try {
    const result = await getLiveAgentPlans(props.supportSession ? props.supportTenantId : undefined)
    livePlans.value = (result.items || []).filter((item) => item.status === 'active')
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取智能体直播方案失败'
    }
  }
}

async function refreshCurrentRoomPlan(preserveEditing = true) {
  const roomId = activeRoomId.value
  if (!roomId) {
    boundRoomPlans.value = []
    publishedRoomPlans.value = []
    roomRuntimeSnapshot.value = null
    currentRoomPlanId.value = null
    return
  }
  try {
    const [boundResult, publishedResult, runtime] = await Promise.all([
      getRoomLiveAgentPlans(roomId),
      getRoomLiveAgentPlans(roomId, true),
      getLiveRuntime(roomId),
    ])
    boundRoomPlans.value = boundResult.items || []
    publishedRoomPlans.value = publishedResult.items || []
    roomRuntimeSnapshot.value = runtime
    const availableIds = new Set(livePlans.value.map((item) => item.id))
    const runtimeId = runtime.agent_plan_id || 0
    if (preserveEditing && currentRoomPlanId.value && availableIds.has(currentRoomPlanId.value)) {
      return
    } else if (runtimeId && availableIds.has(runtimeId)) {
      currentRoomPlanId.value = runtimeId
    } else {
      currentRoomPlanId.value =
        publishedRoomPlans.value[0]?.id || boundRoomPlans.value[0]?.id || livePlans.value[0]?.id || null
    }
  } catch (err) {
    boundRoomPlans.value = []
    publishedRoomPlans.value = []
    roomRuntimeSnapshot.value = null
    currentRoomPlanId.value = null
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取直播间方案关系失败'
    }
  }
}

async function refreshPlanScripts() {
  const planId = currentRoomPlanId.value
  if (!planId) {
    planScripts.value = []
    selectedScriptId.value = null
    formalFacts.value = []
    formalBenefits.value = []
    formalProductLinks.value = []
    return
  }
  try {
    const result = await getLiveAgentPlanScripts(planId, activeRoom.value?.tenant_id)
    if (planId !== currentRoomPlanId.value) return
    planScripts.value = result.items || []
    if (!selectedScriptId.value || !planScripts.value.some((item) => item.id === selectedScriptId.value)) {
      const first = planScripts.value[0] || null
      if (first) {
        selectPlanScript(first)
      }
    }
    await Promise.all([
      refreshFormalFacts(),
      refreshFormalBenefits(),
      refreshFormalProductLinks(),
    ])
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '读取直播话术失败'
  }
}

async function refreshFormalFacts() {
  const planId = currentRoomPlanId.value
  if (!planId) {
    formalFacts.value = []
    return
  }
  try {
    const result = await getLiveAgentPlanFacts(planId, activeRoom.value?.tenant_id)
    formalFacts.value = result.items || []
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取正式事实依据失败'
    }
  }
}

const formalFactCategoryOptions = [
  { value: 'product', label: '商品事实' },
  { value: 'link', label: '链接事实' },
  { value: 'trade', label: '交易 / 售后' },
  { value: 'fulfillment', label: '履约 / 物流' },
  { value: 'identity_location', label: '身份 / 地点' },
  { value: 'other', label: '其它事实' },
]

function startFormalFactEdit(item: LiveAgentPlanFact) {
	creatingFormalFact.value = false
  editingFormalFactId.value = item.id
  formalFactEditDraft.value = {
    category: item.category || 'other',
    key: item.key,
    value: item.value,
		forbidden_wording: item.forbidden_wording || '',
		safe_rewrite: item.safe_rewrite || '',
  }
}

function cancelFormalFactEdit() {
  editingFormalFactId.value = null
  formalFactEditDraft.value = { category: 'other', key: '', value: '', forbidden_wording: '', safe_rewrite: '' }
}

function beginCreateFormalFact() {
	cancelFormalFactEdit()
	creatingFormalFact.value = true
	formalFactCreateDraft.value = { category: 'other', key: '', value: '', forbidden_wording: '', safe_rewrite: '' }
}

function cancelCreateFormalFact() {
	creatingFormalFact.value = false
	formalFactCreateDraft.value = { category: 'other', key: '', value: '', forbidden_wording: '', safe_rewrite: '' }
}

async function createFormalFact() {
	const planId = currentRoomPlanId.value
	if (!planId || mutatingFormalFactId.value) return
	const draft = formalFactCreateDraft.value
	const key = draft.key.trim()
	const value = draft.value.trim()
	if (!key || !value) {
		settingsError.value = '事实名称和事实内容不能为空'
		return
	}
	mutatingFormalFactId.value = -1
	settingsError.value = ''
	try {
		const result = await adoptLiveAgentPlanFacts(
			planId,
			[{
				category: draft.category,
				key,
				value,
				forbidden_wording: draft.forbidden_wording.trim(),
				safe_rewrite: draft.safe_rewrite.trim(),
				status: 'pending',
				review_bucket: 'adoptable',
				source_quote: '',
			}],
			activeRoom.value?.tenant_id,
			'manual:fact-editor',
		)
		if (!result.adopted && result.conflicts) {
			throw new Error(result.results?.[0]?.message || '同分类下已经存在相同事实名称')
		}
		await refreshFormalFacts()
		cancelCreateFormalFact()
		scriptNotice.value = '事实依据已保存，主线口播与互动回答会共同使用。'
	} catch (err) {
		settingsError.value = err instanceof Error ? err.message : '新增事实依据失败'
	} finally {
		mutatingFormalFactId.value = null
	}
}

async function saveFormalFactEdit(item: LiveAgentPlanFact) {
  const planId = currentRoomPlanId.value
  if (!planId || mutatingFormalFactId.value) return
  const key = formalFactEditDraft.value.key.trim()
  const value = formalFactEditDraft.value.value.trim()
  if (!key || !value) {
    settingsError.value = '事实名称和事实内容不能为空'
    return
  }
  mutatingFormalFactId.value = item.id
  settingsError.value = ''
  try {
    await updateLiveAgentPlanFact(
      planId,
      item.id,
      {
			expected_version_no: item.version_no,
        category: formalFactEditDraft.value.category,
        key,
        value,
			forbidden_wording: formalFactEditDraft.value.forbidden_wording.trim(),
			safe_rewrite: formalFactEditDraft.value.safe_rewrite.trim(),
      },
      activeRoom.value?.tenant_id,
    )
    await refreshFormalFacts()
    cancelFormalFactEdit()
    scriptNotice.value = '正式事实已修改并生成新版本。'
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '修改正式事实失败'
  } finally {
    mutatingFormalFactId.value = null
  }
}

async function deleteFormalFact(item: LiveAgentPlanFact) {
  const planId = currentRoomPlanId.value
  if (!planId || mutatingFormalFactId.value) return
  if (!window.confirm('确定删除事实“' + item.key + '”吗？删除后直播智能体不会再使用它，历史版本仍保留。')) {
    return
  }
  mutatingFormalFactId.value = item.id
  settingsError.value = ''
  try {
    await deleteLiveAgentPlanFact(planId, item.id, activeRoom.value?.tenant_id)
    if (editingFormalFactId.value === item.id) cancelFormalFactEdit()
    await refreshFormalFacts()
    scriptNotice.value = '正式事实已删除；历史版本仍保留用于审计。'
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '删除正式事实失败'
  } finally {
    mutatingFormalFactId.value = null
  }
}

async function refreshFormalBenefits() {
  const planId = currentRoomPlanId.value
  if (!planId) {
    formalBenefits.value = []
    return
  }
  try {
    const result = await getLiveAgentPlanBenefits(planId, activeRoom.value?.tenant_id)
    formalBenefits.value = result.items || []
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取正式活动福利失败'
    }
  }
}

async function refreshFormalProductLinks() {
  const planId = currentRoomPlanId.value
  if (!planId) {
    formalProductLinks.value = []
    return
  }
  try {
    const result = await getLiveAgentPlanProductLinks(planId, activeRoom.value?.tenant_id)
    formalProductLinks.value = result.items || []
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取正式商品链接失败'
    }
  }
}

async function saveProductLinkField(
  item: LiveAgentPlanProductLink,
  field: 'link_key' | 'product_name' | 'spec' | 'daily_price' | 'quantity' | 'audience',
  value: string,
) {
  const planId = currentRoomPlanId.value
  if (!planId || item.plan_id !== planId) throw new Error('当前方案已切换，请重新编辑')
  const updated = await updateLiveAgentPlanProductLink(planId, item.id, {
    expected_version_no: item.version_no,
    link_key: item.link_key,
    product_name: item.product_name || '',
    spec: item.spec || '',
    daily_price: item.daily_price || '',
    quantity: item.quantity || '',
    audience: item.audience || '',
    [field]: value,
  }, item.tenant_id)
  if (currentRoomPlanId.value === planId) {
    formalProductLinks.value = formalProductLinks.value.map((link) => link.id === item.id ? updated : link)
  }
}

async function removeProductLink(item: LiveAgentPlanProductLink) {
  const planId = currentRoomPlanId.value
  if (!planId || item.plan_id !== planId) throw new Error('当前方案已切换，请重新操作')
  await deleteLiveAgentPlanProductLink(planId, item.id, item.tenant_id)
  if (currentRoomPlanId.value === planId) {
    formalProductLinks.value = formalProductLinks.value.filter((link) => link.id !== item.id)
  }
}

type ProductAttributeDraft = {
	code: string
	label: string
	value: string
	unit?: string
	display_type?: string
	display_priority?: number
}

function replaceProductAttribute(productLinkId: number, attribute: LiveAgentPlanProductAttribute) {
	formalProductLinks.value = formalProductLinks.value.map((link) => {
		if (link.id !== productLinkId) return link
		const attributes = [...(link.attributes || []).filter((item) => item.id !== attribute.id), attribute]
		attributes.sort((left, right) => left.display_priority - right.display_priority || left.id - right.id)
		return { ...link, attributes }
	})
}

async function addProductAttribute(item: LiveAgentPlanProductLink, draft: ProductAttributeDraft) {
	const planId = currentRoomPlanId.value
	if (!planId || item.plan_id !== planId) throw new Error('当前方案已切换，请重新操作')
	const created = await createLiveAgentPlanProductAttribute(planId, item.id, draft, item.tenant_id)
	if (currentRoomPlanId.value === planId) replaceProductAttribute(item.id, created)
}

async function saveProductAttribute(item: LiveAgentPlanProductLink, attribute: LiveAgentPlanProductAttribute, draft: ProductAttributeDraft) {
	const planId = currentRoomPlanId.value
	if (!planId || item.plan_id !== planId) throw new Error('当前方案已切换，请重新操作')
	const updated = await updateLiveAgentPlanProductAttribute(planId, item.id, attribute.id, {
		...draft,
		expected_version_no: attribute.version_no,
	}, item.tenant_id)
	if (currentRoomPlanId.value === planId) replaceProductAttribute(item.id, updated)
}

async function removeProductAttribute(item: LiveAgentPlanProductLink, attribute: LiveAgentPlanProductAttribute) {
	const planId = currentRoomPlanId.value
	if (!planId || item.plan_id !== planId) throw new Error('当前方案已切换，请重新操作')
	await deleteLiveAgentPlanProductAttribute(planId, item.id, attribute.id, item.tenant_id)
	if (currentRoomPlanId.value === planId) {
		formalProductLinks.value = formalProductLinks.value.map((link) => link.id === item.id
			? { ...link, attributes: (link.attributes || []).filter((entry) => entry.id !== attribute.id) }
			: link)
	}
}

async function adoptProductLinkCandidate(candidate: LiveAgentPlanProductLinkCandidate) {
  const planId = currentRoomPlanId.value
  if (!planId || adoptingProductLinkKey.value) return
  adoptingProductLinkKey.value = candidate.link_key || 'product-link'
  settingsError.value = ''
  try {
    const sourceRef = selectedScriptId.value
      ? 'script:' + selectedScriptId.value + ':analysis'
      : 'script:draft:analysis'
    const result = await adoptLiveAgentPlanProductLinks(
      planId,
      [candidate],
      activeRoom.value?.tenant_id,
      sourceRef,
    )
    await refreshFormalProductLinks()
    const parts: string[] = []
    if (result.adopted) parts.push('已采纳 ' + result.adopted + ' 条')
    if (result.skipped) parts.push('已存在 ' + result.skipped + ' 条')
    if (result.conflicts) parts.push('冲突 ' + result.conflicts + ' 条')
    if (result.blocked) parts.push('阻止 ' + result.blocked + ' 条')
    scriptNotice.value = (parts.length ? parts.join('，') : '商品链接未发生变更') + '。活动价、赠品和临时活动仍由“活动福利”单独管理。'
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '采纳商品链接失败'
  } finally {
    adoptingProductLinkKey.value = ''
  }
}

async function adoptBenefitCandidate(candidate: LiveAgentPlanBenefitCandidate) {
  const planId = currentRoomPlanId.value
  if (!planId || adoptingBenefitKey.value) return
  const key = candidate.key || candidate.link_key || 'benefit'
  adoptingBenefitKey.value = key
  settingsError.value = ''
  try {
    const sourceRef = selectedScriptId.value
      ? 'script:' + selectedScriptId.value + ':analysis'
      : 'script:draft:analysis'
    const result = await adoptLiveAgentPlanBenefits(
      planId,
      [candidate],
      activeRoom.value?.tenant_id,
      sourceRef,
    )
    await refreshFormalBenefits()
    const parts: string[] = []
    if (result.adopted) parts.push('已生效 ' + result.adopted + ' 条')
    if (result.drafted) parts.push('已进入活动草稿 ' + result.drafted + ' 条')
    if (result.skipped) parts.push('已存在 ' + result.skipped + ' 条')
    if (result.conflicts) parts.push('冲突 ' + result.conflicts + ' 条')
    if (result.blocked) parts.push('阻止 ' + result.blocked + ' 条')
    scriptNotice.value = (parts.length ? parts.join('，') : '活动福利未发生变更') + '。'
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '采纳活动福利失败'
  } finally {
    adoptingBenefitKey.value = ''
  }
}

async function refreshMemories() {
  const roomId = activeRoomId.value
  if (!roomId) {
    memories.value = []
    return
  }
  try {
    const result = await getAgentMemories(roomId)
    memories.value = (result.items || []).filter((item) => item.status === 'active')
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取学习记忆失败'
    }
  }
}

function resetMemoryEdit() {
  editingMemoryId.value = null
  memoryEditSessionId.value = null
  memoryEditFeedback.value = ''
  memoryEditPreview.value = ''
  memoryEditError.value = ''
}

function beginMemoryEdit(item: AgentMemoryItem) {
  memoryDeleteConfirmId.value = null
  editingMemoryId.value = item.id
  memoryEditSessionId.value = null
  memoryEditFeedback.value = ''
  memoryEditPreview.value = ''
  memoryEditError.value = ''
}

async function previewMemoryEdit(item: AgentMemoryItem) {
  const roomId = activeRoomId.value
  const feedback = memoryEditFeedback.value.trim()
  if (!roomId || !feedback || memoryMutatingId.value) return
  memoryMutatingId.value = item.id
  memoryEditError.value = ''
  try {
    let sessionId = memoryEditSessionId.value
    if (!sessionId) {
      const created = await createAgentLearningSession(roomId, {
        source_type: 'memory_correction',
        source_ref: 'agent_memory:' + item.id,
        original_reply: item.current_version?.content_text || '',
        target: item.target,
      })
      sessionId = created.id
      memoryEditSessionId.value = sessionId
    }
    const output = await createAgentLearningTurn(roomId, sessionId, feedback)
    memoryEditPreview.value = output.result.result_text || ''
  } catch (err) {
    memoryEditError.value = err instanceof Error ? err.message : '生成修改预览失败'
  } finally {
    memoryMutatingId.value = null
  }
}

async function adoptMemoryEdit(item: AgentMemoryItem) {
  const roomId = activeRoomId.value
  const sessionId = memoryEditSessionId.value
  if (!roomId || !sessionId || !memoryEditPreview.value || memoryMutatingId.value) return
  memoryMutatingId.value = item.id
  memoryEditError.value = ''
  try {
    await adoptAgentLearningSession(roomId, sessionId)
    await refreshMemories()
    resetMemoryEdit()
  } catch (err) {
    memoryEditError.value = err instanceof Error ? err.message : '采用修改失败'
  } finally {
    memoryMutatingId.value = null
  }
}

async function deleteMemory(item: AgentMemoryItem) {
  const roomId = activeRoomId.value
  if (!roomId || memoryMutatingId.value) return
  if (memoryDeleteConfirmId.value !== item.id) {
    memoryDeleteConfirmId.value = item.id
    return
  }
  memoryMutatingId.value = item.id
  try {
    await deactivateAgentMemory(roomId, item.id)
    if (editingMemoryId.value === item.id) resetMemoryEdit()
    memoryDeleteConfirmId.value = null
    await refreshMemories()
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '删除互动策略失败'
  } finally {
    memoryMutatingId.value = null
  }
}

async function handlePlanSwitch(event: Event) {
  const id = Number((event.target as HTMLSelectElement).value || 0)
  const plan = livePlans.value.find((item) => item.id === id)
  if (!plan || plan.id === currentRoomPlanId.value) return
  deleteConfirmPlanId.value = null
  currentRoomPlanId.value = plan.id
  await refreshPlanScripts()
  if (showPlanManager.value && planManagerMode.value === 'edit') {
    loadCurrentPlanIntoManager()
  }
}

async function openCurrentPlanPublishing() {
  const plan = currentPlan.value
  if (!plan || !currentPlanIsBound.value) return
  activeMode.value = 'fullshow'
  if (!fullShowResult.value) await loadFullShowWorkspace()
  if (!fullShowResult.value) {
    fullShowVersionError.value = '这个方案还没有可发布的智能体版本，请先完成文稿确认和声音生成。'
    return
  }
  await publishFullShowVersion()
}

function loadCurrentPlanIntoManager() {
  newPlanName.value = currentPlan.value?.name || ''
  newPlanDescription.value = currentPlan.value?.description || ''
}

function nextPlanSequence(roomName: string) {
  const prefix = `${roomName}直播间 智能体方案`
  const used = new Set<number>()
  for (const plan of livePlans.value) {
    const name = String(plan.name || '').trim()
    if (!name.startsWith(prefix)) continue
    const suffix = name.slice(prefix.length).trim()
    if (!/^\d+$/.test(suffix)) continue
    const sequence = Number(suffix)
    if (Number.isFinite(sequence) && sequence > 0) used.add(sequence)
  }
  let sequence = 1
  while (used.has(sequence)) sequence += 1
  return String(sequence).padStart(2, '0')
}

function openCreatePlanManager() {
  deleteConfirmPlanId.value = null
  if (showPlanManager.value && planManagerMode.value === 'create') {
    showPlanManager.value = false
    return
  }
  const roomName = String(activeRoom.value?.name || '直播间').trim() || '直播间'
  const sequence = nextPlanSequence(roomName)
  planManagerMode.value = 'create'
  newPlanName.value = `${roomName}直播间 智能体方案${sequence}`
  newPlanDescription.value = `${roomName}话术 + 事实依据 + 链接`
  showPlanManager.value = true
}

function toggleEditPlanManager() {
  deleteConfirmPlanId.value = null
  if (showPlanManager.value && planManagerMode.value === 'edit') {
    showPlanManager.value = false
    return
  }
  planManagerMode.value = 'edit'
  loadCurrentPlanIntoManager()
  showPlanManager.value = true
}

function normalizeReadableScript(value: string) {
  const lines = value.replace(/\r\n?/g, '\n').split('\n')
  const output: string[] = []
  let blank = false
  for (const raw of lines) {
    const line = raw.trim()
    if (!line) {
      if (!blank && output.length) output.push('')
      blank = true
      continue
    }
    blank = false
    output.push(line)
  }
  return output.join('\n').trim()
}

function startNewPlanScript() {
  anchorStyleSourceText.value = ''
  selectedScriptId.value = null
  scriptTitle.value = ''
  scriptRawText.value = ''
  scriptReadableText.value = ''
  scriptSourceFile.value = null
  scriptSourceType.value = 'paste'
  scriptNotice.value = ''
  analysisDraft.value = null
  selectedFactKeys.value = []
  knowledgeDraftText.value = ''
  rhythmDraftText.value = ''
}

function selectPlanScript(item: LiveAgentPlanScript) {
  anchorStyleSourceText.value = item.readable_text.trim()
  selectedScriptId.value = item.id
  scriptTitle.value = item.title
  scriptRawText.value = item.raw_text
  scriptReadableText.value = item.readable_text
  scriptSourceFile.value = null
  scriptSourceType.value = item.source_type === 'upload' ? 'upload' : 'paste'
  scriptNotice.value = ''
  analysisDraft.value = null
  selectedFactKeys.value = []
  knowledgeDraftText.value = item.analysis_status === 'analyzed' ? formatKnowledgeDraft(item.analysis) : ''
  rhythmDraftText.value = item.analysis_status === 'analyzed' ? formatRhythmDraft(item.analysis) : ''
}

function syncSimpleScriptText() {
  if (!selectedScriptId.value && scriptSourceType.value === 'paste' && !scriptSourceFile.value) {
    scriptRawText.value = scriptReadableText.value
  }
}

async function handlePlanScriptFile(event: Event) {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  if (!file) return
  const lower = file.name.toLowerCase()
  const isText = lower.endsWith('.txt') || lower.endsWith('.md')
  const isImage = file.type.startsWith('image/') || /\.(png|jpe?g|webp)$/i.test(lower)
  if (!isText && !isImage) {
    settingsError.value = '直播素材目前支持 TXT、MD、JPG、PNG、WEBP'
    return
  }
  const planId = currentRoomPlanId.value
  if (!planId) {
    settingsError.value = '请先选择直播方案'
    return
  }
  settingsError.value = ''
  try {
    startNewPlanScript()
    scriptSourceType.value = 'upload'
    scriptTitle.value = file.name.replace(/\.(txt|md|png|jpe?g|webp)$/i, '')
    if (isText) {
      const text = await file.text()
      scriptSourceFile.value = file
      scriptRawText.value = text
      scriptReadableText.value = normalizeReadableScript(text)
      scriptNotice.value = '已读取“' + file.name + '”，当前只是页面内容，点击“保存素材”后才会写入直播方案。'
      return
    }

    scriptRecognizing.value = true
    scriptNotice.value = '正在识别“' + file.name + '”中的文字和可见内容，本次识别不会自动保存。'
    const result = await previewRecognizeLiveAgentPlanImage(planId, file, activeRoom.value?.tenant_id)
    const extractedText = result.text.trim()
    const visualContext = result.visual_context.trim()
    const readableParts: string[] = []
    const rawParts: string[] = []
    if (extractedText) {
      rawParts.push('【图片文字】\n' + extractedText)
      readableParts.push(extractedText)
    }
    if (visualContext) readableParts.push('【图片可见内容】\n' + visualContext)
    if (visualContext) rawParts.push('【图片可见内容】\n' + visualContext)
    scriptSourceFile.value = file
    scriptRawText.value = rawParts.join('\n\n')
    scriptReadableText.value = readableParts.join('\n\n')
    const warningText = result.warnings?.length ? '；识别提示：' + result.warnings.join('；') : ''
    scriptNotice.value = '已识别“' + file.name + '”' + warningText + '。当前结果未保存，可先核对文字，再点击“保存素材”。'
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '读取直播素材失败'
  } finally {
    scriptRecognizing.value = false
  }
}

async function savePlanScript(showNotice = true) {
  const planId = currentRoomPlanId.value
  if (!planId || scriptBusy.value) return null
  if (!scriptRawText.value.trim() || !scriptReadableText.value.trim()) {
    settingsError.value = '请先粘贴或上传直播话术，并确认整理后的内容'
    return null
  }
  scriptBusy.value = true
  settingsError.value = ''
  try {
    let sourceAssetId = activeScript.value?.source_asset_id
    let originalName = activeScript.value?.original_name || ''
    if (!selectedScriptId.value && scriptSourceFile.value) {
      const assetType = scriptSourceFile.value.type.startsWith('image/') ? 'image' : 'document'
      const asset = await uploadLiveMediaAsset(scriptSourceFile.value, assetType, {
        metadata: {
          room_id: activeRoomId.value || 0,
          plan_id: planId,
          source: 'live_plan_script',
        },
      })
      sourceAssetId = asset.asset.id
      originalName = scriptSourceFile.value.name
    }
    const payload = {
      tenant_id: activeRoom.value?.tenant_id,
      title: scriptTitle.value.trim(),
      source_type: scriptSourceType.value,
      source_asset_id: sourceAssetId,
      original_name: originalName,
      raw_text: scriptRawText.value,
      readable_text: scriptReadableText.value,
    }
    const item = selectedScriptId.value
      ? await updateLiveAgentPlanScript(planId, selectedScriptId.value, payload)
      : await createLiveAgentPlanScript(planId, payload)
    const index = planScripts.value.findIndex((entry) => entry.id === item.id)
    if (index >= 0) {
      planScripts.value.splice(index, 1, item)
    } else {
      planScripts.value.unshift(item)
    }
    selectPlanScript(item)
    if (showNotice) scriptNotice.value = '直播话术已保存到当前方案。'
    return item
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '保存直播话术失败'
    return null
  } finally {
    scriptBusy.value = false
  }
}

async function analyzeCurrentPlanScript() {
  if (scriptAnalyzing.value) return
  const returnMode = activeMode.value
	const styleOnly = returnMode === 'anchor'
  const planId = currentRoomPlanId.value
  const text = scriptReadableText.value.trim()
  if (!planId || !currentPlan.value) {
    settingsError.value = '请先选择直播方案'
    return
  }
  if (!text) {
    settingsError.value = '请先上传 TXT / MD 或在文本框中输入直播话术'
    return
  }

  scriptAnalyzing.value = true
  settingsError.value = ''
  scriptNotice.value = ''
	if (styleOnly) anchorStyleAnalysisQC.value = null
  scriptAnalysisProgress.value = 8
  scriptAnalysisStage.value = '正在读取当前文本框内容'
  try {
    scriptAnalysisProgress.value = 22
		scriptAnalysisStage.value = styleOnly
			? '只提取称呼、原词、句式、停顿与局部表达推进'
			: '正在识别商品链接、活动福利、事实、口播结构与主播风格'
    startScriptAnalysisProgressTimer()
		const result = styleOnly
			? await previewAnalyzeLiveAgentAnchorStyle(planId, text, activeRoom.value?.tenant_id)
			: await previewAnalyzeLiveAgentPlanScript(planId, text, activeRoom.value?.tenant_id)
    if (planId !== currentRoomPlanId.value || text !== scriptReadableText.value.trim()) {
      throw new Error('方案或素材已变化，已忽略旧素材的分析结果，请重新分析。')
    }
    stopScriptAnalysisProgressTimer()
    scriptAnalysisProgress.value = 95
    scriptAnalysisStage.value = '正在整理分析结果'
    analysisDraft.value = result.analysis
    anchorStyleSourceText.value = text
    anchorStyleTestResult.value = null
		if (!styleOnly) {
			selectedFactKeys.value = []
			knowledgeDraftText.value = formatKnowledgeDraft(result.analysis)
			rhythmDraftText.value = formatRhythmDraft(result.analysis)
		}
		if (styleOnly) anchorStyleAnalysisQC.value = result.style_qc || null
		if (!(result.analysis.product_links || []).length
			&& (styleOnly || !knowledgeDraftText.value.trim())
			&& (styleOnly || !rhythmDraftText.value.trim())
			&& !(result.analysis.anchor_style?.dimensions || []).length) {
      throw new Error('智能分析返回了空结果，请重新分析')
    }
    scriptAnalysisProgress.value = 100
    scriptAnalysisStage.value = '分析完成'
    const linkCount = (result.analysis.product_links || []).length
    const benefitCount = (result.analysis.product_links || []).filter(
      (item) => item.activity_price || item.gift || item.activity,
    ).length
    const linkCoverage = result.analysis.completeness?.link_coverage_pct ?? 100
    const styleDimensionCount = result.analysis.anchor_style?.dimensions?.length || 0
		if (styleOnly) {
			const seconds = result.latency_ms ? (result.latency_ms / 1000).toFixed(1) : '—'
			const qcLabel = result.style_qc?.available
				? (result.style_qc.passed ? 'Qwen同步质检通过' : 'Qwen同步质检未通过')
				: 'Qwen同步质检暂不可用'
			scriptNotice.value = `主播风格独立分析完成：${styleDimensionCount} 个固定维度、${result.analysis.anchor_style?.delivery_spec?.literal_habits?.length || 0} 个有原文证据的习惯；${qcLabel}；总耗时 ${seconds} 秒。未分析商品事实、销售步骤、互动打断或回归策略。`
			activeMode.value = 'anchor'
		} else {
			scriptNotice.value = '素材归位完成：商品链接 ' + linkCount + ' 个、活动福利 ' + benefitCount
				+ ' 个、事实依据 ' + result.analysis.facts.length + ' 条'
				+ '、主播风格 ' + styleDimensionCount + ' 个维度；链接覆盖 ' + linkCoverage
				+ '%。本次仅生成页面草稿，没有保存任何内容。'
			activeMode.value = linkCount ? 'products' : (result.analysis.facts.length ? 'knowledge' : 'anchor')
		}
    window.setTimeout(() => {
      if (!scriptAnalyzing.value && scriptAnalysisProgress.value === 100) {
        scriptAnalysisProgress.value = 0
        scriptAnalysisStage.value = ''
      }
    }, 1800)
  } catch (err) {
    stopScriptAnalysisProgressTimer()
    scriptAnalysisProgress.value = 0
    scriptAnalysisStage.value = ''
    settingsError.value = err instanceof Error ? err.message : '智能分析直播素材失败'
  } finally {
    stopScriptAnalysisProgressTimer()
    scriptAnalyzing.value = false
  }
}

async function applyAnchorStyleHot() {
  if (anchorStyleApplying.value || scriptAnalyzing.value) return
  const planId = currentRoomPlanId.value
  if (!planId || !currentPlan.value) {
    settingsError.value = '请先选择直播方案'
    return
  }
  if (!scriptReadableText.value.trim()) {
    settingsError.value = '请先准备主播素材'
    return
  }
  anchorStyleApplying.value = true
  settingsError.value = ''
  scriptNotice.value = ''
  try {
    if (anchorStyleSourceText.value && anchorStyleSourceText.value !== scriptReadableText.value.trim()) {
      throw new Error('素材已修改，请重新分析和测试后再保存风格。')
    }
    const confirmedAnalysis = currentAnalysis.value
    if (!confirmedAnalysis?.anchor_style?.dimensions?.length) {
      throw new Error('请先分析当前素材，检查规则并测试文案后再保存。')
    }
    const savedScript = await savePlanScript(false)
    if (!savedScript) return
    const updated = await confirmLiveAgentPlanScriptAnalysis(planId, savedScript.id, scriptReadableText.value, confirmedAnalysis, activeRoom.value?.tenant_id)
    const index = planScripts.value.findIndex((entry) => entry.id === updated.id)
    if (index >= 0) planScripts.value.splice(index, 1, updated)
    else planScripts.value.unshift(updated)
    selectedScriptId.value = updated.id
    analysisDraft.value = null
    scriptNotice.value = '主播风格已保存并热生效；当前主线声音继续播放，不需要重新生成。'
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '保存主播风格失败'
  } finally {
    anchorStyleApplying.value = false
  }
}

function factCategoryLabel(category: string) {
  if (category === 'product') return '商品事实'
  if (category === 'link') return '商品 / 链接'
  if (category === 'trade') return '交易事实'
  if (category === 'fulfillment') return '履约事实'
  if (category === 'identity_location') return '地点 / 身份'
  return '其它事实'
}

function reviewBucketLabel(bucket?: string) {
  if (bucket === 'adoptable') return '可采纳'
  if (bucket === 'conflict') return '矛盾'
  if (bucket === 'violation') return '严重违规'
  return '待商量'
}


function memoryTypeLabel(type: string) {
  if (type === 'fact') return '事实纠正'
  if (type === 'semantic') return '回答策略'
  if (type === 'wording') return '用词纠正'
  if (type === 'style') return '主播表达'
  return type || '其它记忆'
}

async function createPlanForCurrentRoom() {
  const name = newPlanName.value.trim()
  const room = activeRoom.value
  if (!name || planBusy.value) return
  planBusy.value = true
  settingsError.value = ''
  try {
    const created = await createLiveAgentPlan({
      name,
      description: newPlanDescription.value.trim(),
      tenant_id: room?.tenant_id,
    })
    if (room) {
      await bindRoomLiveAgentPlan(created.id, room.id, room.tenant_id)
      currentRoomPlanId.value = created.id
    }
    newPlanName.value = ''
    newPlanDescription.value = ''
    await Promise.all([refreshLivePlans(), refreshCurrentRoomPlan()])
    currentRoomPlanId.value = created.id
    await refreshPlanScripts()
    messages.value.push({
      role: 'agent',
      text: `智能体直播方案“${created.name}”已创建${room ? '，并绑定到当前直播间' : ''}。当前状态为“已绑定 · 未发布”，发布后才会出现在直播间大屏的方案列表中。`,
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '创建智能体直播方案失败'
  } finally {
    planBusy.value = false
  }
}

async function saveCurrentPlan() {
  const plan = currentPlan.value
  const room = activeRoom.value
  const name = newPlanName.value.trim()
  if (!plan || !name || planBusy.value) return
  planBusy.value = true
  settingsError.value = ''
  try {
    const updated = await updateLiveAgentPlan(plan.id, {
      name,
      description: newPlanDescription.value.trim(),
      tenant_id: room?.tenant_id,
    })
    const index = livePlans.value.findIndex((item) => item.id === updated.id)
    if (index >= 0) livePlans.value.splice(index, 1, updated)
    await refreshLivePlans()
    loadCurrentPlanIntoManager()
    messages.value.push({ role: 'agent', text: `直播方案已保存为“${updated.name}”。` })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '保存直播方案失败'
  } finally {
    planBusy.value = false
  }
}

async function deleteCurrentPlan() {
  const plan = currentPlan.value
  if (!plan || planBusy.value) return
  if (deleteConfirmPlanId.value !== plan.id) {
    deleteConfirmPlanId.value = plan.id
    window.setTimeout(() => {
      if (deleteConfirmPlanId.value === plan.id) {
        deleteConfirmPlanId.value = null
      }
    }, 5000)
    return
  }

  planBusy.value = true
  deleteConfirmPlanId.value = null
  settingsError.value = ''
  try {
    await archiveLiveAgentPlan(plan.id, activeRoom.value?.tenant_id)
    currentRoomPlanId.value = null
    showPlanManager.value = false
    newPlanName.value = ''
    newPlanDescription.value = ''
    await refreshLivePlans()
    await refreshCurrentRoomPlan()
    await refreshPlanScripts()
    messages.value.push({ role: 'agent', text: `直播方案“${plan.name}”已删除。` })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '删除直播方案失败'
  } finally {
    planBusy.value = false
  }
}

async function submitPlanManager() {
  if (planManagerMode.value === 'create') {
    await createPlanForCurrentRoom()
    planManagerMode.value = 'edit'
    loadCurrentPlanIntoManager()
    return
  }
  await saveCurrentPlan()
}

async function usePlanForCurrentRoom(plan: LiveAgentPlan) {
  const room = activeRoom.value
  if (!room || planBusy.value || runtimePlanId.value === plan.id) return
  if (!boundPlanIdSet.value.has(plan.id)) {
    settingsError.value = '请先把这个方案绑定到当前直播间，再切换使用。'
    return
  }
  planBusy.value = true
  settingsError.value = ''
  try {
    await setLiveRuntimePlan(room.id, plan.id)
    currentRoomPlanId.value = plan.id
    await refreshCurrentRoomPlan()
    currentRoomPlanId.value = plan.id
    await refreshPlanScripts()
    messages.value.push({ role: 'agent', text: `当前直播间运行方案已热切换到“${plan.name}”。其它已绑定方案仍然保留。` })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '切换智能体直播方案失败'
  } finally {
    planBusy.value = false
  }
}

async function bindPlanToCurrentRoom(plan: LiveAgentPlan) {
  const room = activeRoom.value
  if (!room || planBusy.value || boundPlanIdSet.value.has(plan.id)) return
  planBusy.value = true
  settingsError.value = ''
  try {
    await bindRoomLiveAgentPlan(plan.id, room.id, room.tenant_id)
    await Promise.all([refreshLivePlans(), refreshCurrentRoomPlan()])
    if (!currentRoomPlanId.value) currentRoomPlanId.value = plan.id
    await refreshPlanScripts()
    messages.value.push({ role: 'agent', text: `已把“${plan.name}”绑定到“${room.name}”。现在这个房间可以使用该方案，但不会自动切换直播运行方案。` })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '绑定直播方案失败'
  } finally {
    planBusy.value = false
  }
}

async function unbindPlanFromCurrentRoom(plan: LiveAgentPlan) {
  const room = activeRoom.value
  if (!room || planBusy.value || !boundPlanIdSet.value.has(plan.id)) return
  planBusy.value = true
  settingsError.value = ''
  try {
    await unbindRoomLiveAgentPlan(plan.id, room.id, room.tenant_id)
    await Promise.all([refreshLivePlans(), refreshCurrentRoomPlan()])
    await refreshPlanScripts()
    messages.value.push({ role: 'agent', text: `已取消“${plan.name}”与“${room.name}”的绑定。方案本身没有删除，仍可绑定给其它直播间。` })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '取消方案绑定失败'
  } finally {
    planBusy.value = false
  }
}

async function loadAgentSettings() {
  settingsError.value = ''

  restoreLiveStrategyPreferencesLocal()

  try {
    const [roomData, uiPreferences] = await Promise.all([
      props.supportSession
        ? getRoom(props.supportRoomId).then((room) => ({ items: [room] }))
        : getRooms(),
      props.supportSession ? Promise.resolve(null) : getUserUIPreferences().catch(() => null),
    ])
    rooms.value = roomData.items
    if (uiPreferences) {
      if (
        uiPreferences.selected_live_room_id &&
        rooms.value.some(room => room.id === uiPreferences.selected_live_room_id)
      ) {
        activeRoomId.value = uiPreferences.selected_live_room_id
      }
    }
    if (
      props.supportSession &&
      props.supportRoomId &&
      rooms.value.some(room => room.id === props.supportRoomId)
    ) {
      activeRoomId.value = props.supportRoomId
    } else if (!activeRoomId.value || !rooms.value.some(room => room.id === activeRoomId.value)) {
      activeRoomId.value = rooms.value[0]?.id || null
      if (activeRoomId.value && !props.supportSession) {
        void updateUserUIPreferences({ selected_live_room_id: activeRoomId.value }).catch(() => undefined)
      }
    }
    persistSelectedRoomLocal(activeRoomId.value)
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '读取直播间失败'
    if (props.supportSession) {
      rooms.value = []
      activeRoomId.value = null
      return
    }
  }

  if (!props.supportSession) {
    try {
      const [value, addressing] = await Promise.all([
        getLiveAgentSettings(),
        getLiveAddressingStrategy(),
      ])
      settings.value = value
      settingsDraft.value = settingsToInput(value)
      addressingStrategy.value = {
        addressing_mode: addressing.addressing_mode || 'system',
        addressing: (addressing.addressing || []).map((item) => ({ ...item })),
      }
      messages.value = [{ role: 'agent', text: value.greeting }]
    } catch (err) {
      if (!settingsError.value) {
        settingsError.value = err instanceof Error ? err.message : '读取基础设置失败'
      }
    }
  } else {
    messages.value = [{ role: 'agent', text: '已进入客户授权协助模式，仅可维护当前已授权直播间的智能体方案和绑定关系。' }]
  }

  await refreshRoomPolicy()
  await refreshAgentVersions()
  await refreshLivePlans()
  await refreshCurrentRoomPlan()
  await Promise.all([refreshPlanScripts(), refreshMemories()])
}

async function executeAnchorStyleTest(options?: { lockedSampleText?: string; learnedRegeneration?: boolean }) {
  const planId = currentRoomPlanId.value
  const room = activeRoom.value
  const sampledStyle = anchorRulebook.value ? anchorStyleProfile.value : null
  if (!planId) {
    anchorStyleTestError.value = '当前直播方案已变化，请刷新后重新测试。'
    return false
  }
  if (!room) {
    anchorStyleTestError.value = '当前直播间已变化，请刷新后重新测试。'
    return false
  }
  if (!sampledStyle && enabledStyleOverlayCount.value === 0) {
    anchorStyleTestError.value = '当前没有可用于测试的主播样本或叠加风格。'
    return false
  }
  if (anchorStyleTestBusy.value) {
    anchorStyleTestError.value = '上一轮测试仍在生成，请完成后再试。'
    return false
  }
  if (!options?.learnedRegeneration && sampledStyle && !selectedReusableAnchorStyle.value && anchorStyleSourceText.value && anchorStyleSourceText.value !== scriptReadableText.value.trim()) {
    anchorStyleTestError.value = '素材已修改，请先重新分析。'
    return false
  }
  const style: LiveAgentPlanAnchorStyleProfile = sampledStyle || {
    dimensions: [],
    reusable_rules: [],
    candidate_patterns: [],
    excluded_from_style: [],
  }
  const scope = `${room.id}:${planId}`
  anchorStyleTestBusy.value = true
  anchorStyleTestError.value = ''
  anchorStyleTestResult.value = null
  try {
    // A feedback-learning request can take several seconds. During that time the
    // editable source state may be normalized or refreshed by another watcher.
    // Regenerate against the exact sample snapshot that produced the learned
    // candidate instead of silently failing the ordinary "source changed" guard.
    const sourceText = options?.lockedSampleText ?? currentAnchorStyleSampleText(sampledStyle)
    const result = await testLiveAgentAnchorStyle(planId, {
      tenant_id: room.tenant_id,
      room_id: room.id,
      topic: anchorStyleTestTopic.value,
      target_chars: Math.max(100, Math.min(3000, Math.round(Number(anchorStyleTestTargetChars.value) || 500))),
      expansion_freedom: fullShowExpansionFreedom.value,
      source_text: sourceText,
      anchor_style: style,
      selected_facts: selectedFactKeys.value,
      transient_overlays: anchorStyleCorrectionCandidates.value,
    })
    if (`${activeRoomId.value}:${currentRoomPlanId.value}` === scope) anchorStyleTestResult.value = result
    return true
  } catch (err) {
    if (`${activeRoomId.value}:${currentRoomPlanId.value}` === scope) anchorStyleTestError.value = err instanceof Error ? err.message : '风格测试失败'
    return false
  } finally { anchorStyleTestBusy.value = false }
}

async function runAnchorStyleTest() {
  await executeAnchorStyleTest()
}

function currentAnchorStyleSampleText(sampledStyle: LiveAgentPlanAnchorStyleProfile | null = anchorRulebook.value ? anchorStyleProfile.value : null) {
  if (!sampledStyle) return ''
  if (selectedReusableAnchorStyle.value) {
    return reusableAnchorStyleSamples.value.find((item) => item.analysis_status === 'analyzed')?.readable_text || ''
  }
  return currentAnalysis.value?.anchor_style?.dimensions?.length
    ? (anchorStyleSourceText.value || scriptReadableText.value)
    : savedStyleScript.value?.readable_text || ''
}

async function learnAndRegenerateAnchorStyle() {
  const planId = currentRoomPlanId.value
  const result = anchorStyleTestResult.value
  const feedback = anchorStyleCorrectionFeedback.value.trim()
  const sampleText = currentAnchorStyleSampleText()
  if (!planId || !result || !feedback || anchorStyleCorrectionBusy.value) return
  if (!sampleText) {
    anchorStyleCorrectionError.value = '要做“哪里不像”的对比学习，需要先粘贴并分析一份真实主播样本。'
    return
  }
  if (anchorStyleCorrectionCandidates.value.length >= 6) {
    anchorStyleCorrectionError.value = '本轮已累计6条调整。请先保存或放弃，再开始下一轮。'
    return
  }
  anchorStyleCorrectionBusy.value = true
  anchorStyleCorrectionError.value = ''
  try {
    const learned = await learnLiveAgentPlanStyleOverlay(planId, {
      sample_text: sampleText,
      generated_text: result.text,
      feedback_text: feedback,
      strength: 50,
    }, activeRoom.value?.tenant_id)
    anchorStyleCorrectionCandidates.value = [...anchorStyleCorrectionCandidates.value, learned.item]
    anchorStyleCorrectionHistory.value = [...anchorStyleCorrectionHistory.value, {
      feedback,
      diagnosis: learned.diagnosis,
      model: learned.model,
    }]
    anchorStyleCorrectionFeedback.value = ''
    const regenerated = await executeAnchorStyleTest({
      lockedSampleText: sampleText,
      learnedRegeneration: true,
    })
    if (!regenerated) {
      throw new Error(anchorStyleTestError.value || '反馈已经学习，但自动重新生成未启动，请重试。')
    }
  } catch (err) {
    anchorStyleCorrectionError.value = err instanceof Error ? err.message : '学习并重新生成失败'
  } finally {
    anchorStyleCorrectionBusy.value = false
  }
}

async function saveAnchorStyleCorrections() {
  const profile = styleOverlayProfile.value
  if (!profile || !anchorStyleCorrectionCandidates.value.length || anchorStyleCorrectionBusy.value) return
  anchorStyleCorrectionBusy.value = true
  anchorStyleCorrectionError.value = ''
  try {
    const saved = await persistStyleOverlayItems(
      [...profile.items, ...anchorStyleCorrectionCandidates.value],
      '本轮风格调整已保存并热生效',
    )
    if (!saved) return
    anchorStyleCorrectionCandidates.value = []
    anchorStyleCorrectionHistory.value = []
    anchorStyleCorrectionFeedback.value = ''
  } finally {
    anchorStyleCorrectionBusy.value = false
  }
}

function discardAnchorStyleCorrections() {
  anchorStyleCorrectionCandidates.value = []
  anchorStyleCorrectionHistory.value = []
  anchorStyleCorrectionFeedback.value = ''
  anchorStyleCorrectionError.value = ''
  anchorStyleTestResult.value = null
  styleOverlayNotice.value = '本轮未保存的风格调整已放弃，已保存的主播风格没有改变。'
}

const addressingSystemOptions = computed(() => addressingStrategy.value.addressing.filter((item) => item.system_default))
const addressingCustomOptions = computed(() => addressingStrategy.value.addressing.filter((item) => !item.system_default))

function setAddressingMode(mode: 'system' | 'custom') {
  addressingStrategy.value = { ...addressingStrategy.value, addressing_mode: mode }
  addressingError.value = ''
  addressingNotice.value = ''
}

function addAddressingOption() {
  addressingStrategy.value.addressing.push({
    key: `custom_${Date.now()}`,
    text: '',
    enabled: true,
    probability: addressingCustomOptions.value.length ? 0 : 100,
    system_default: false,
  })
}

function removeAddressingOption(option: LiveAddressingOption) {
  addressingStrategy.value.addressing = addressingStrategy.value.addressing.filter((item) => item !== option)
}

async function saveAddressingStrategy() {
  if (props.supportSession || addressingSaving.value) return
  addressingError.value = ''
  addressingNotice.value = ''
  if (
    addressingStrategy.value.addressing_mode === 'custom' &&
    !addressingCustomOptions.value.some((item) => item.enabled && item.text.trim())
  ) {
    addressingError.value = '请至少添加一个自己的称呼。'
    return
  }
  addressingSaving.value = true
  try {
    const saved = await updateLiveAddressingStrategy({
      addressing_mode: addressingStrategy.value.addressing_mode,
      addressing: addressingStrategy.value.addressing.map((item) => ({ ...item, text: item.text.trim() })),
    })
    addressingStrategy.value = {
      addressing_mode: saved.addressing_mode,
      addressing: (saved.addressing || []).map((item) => ({ ...item })),
    }
    addressingNotice.value = saved.addressing_mode === 'custom'
      ? '我的称呼已通过审核并生效。'
      : '已切换为系统默认称呼。'
  } catch (err) {
    addressingError.value = err instanceof Error ? err.message : '保存称呼策略失败'
  } finally {
    addressingSaving.value = false
  }
}

async function refreshAgentVersions() {
  try {
    configVersions.value = await getLiveAgentConfigVersions()
    const selectedStillExists = configVersions.value.some((item) => item.id === selectedVersionId.value)
    if (!selectedStillExists) {
      selectedVersionId.value =
        configVersions.value.find((item) => item.lifecycle_status === 'active')?.id ||
        configVersions.value[0]?.id ||
        null
    }
  } catch (err) {
    if (!settingsError.value) {
      settingsError.value = err instanceof Error ? err.message : '读取配置版本失败'
    }
  }
}

async function handleLiveVoiceChanged() {
  fullShowWorkspaceVoiceIdentity.value = null
  if (fullShowResult.value) {
    fullShowSavedVersion.value = null
    fullShowVersionError.value = ''
    fullShowNotice.value = isUserAudioContentMode.value
      ? '互动音色已变更；原始录音主线保持不变，无需重新生成录音，可保存发布。'
      : '直播间声音身份已变更；原有正式声音仍保留供对比，但需要按新声音身份重新生成后才能保存发布。'
  }
  await refreshAgentVersions()
}

async function saveAgentSettings() {
  settingsSaving.value = true
  settingsError.value = ''
  try {
    const value = await updateLiveAgentSettings(settingsDraft.value)
    settings.value = value
    settingsDraft.value = settingsToInput(value)
    await refreshAgentVersions()
    messages.value.push({
      role: 'agent',
      text: '基础设置已保存。以后问我“你是谁”“你叫什么”“你能做什么”，我都会按这套身份回答。',
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '保存基础设置失败'
  } finally {
    settingsSaving.value = false
  }
}

function roomStatusLabel(status: string) {
  if (status === 'live') return '直播中'
  if (status === 'connecting') return '连接中'
  if (status === 'pending') return '等待连接'
  if (status === 'offline') return '未开播'
  if (status === 'error') return '连接异常'
  return status || '未知'
}

function asRecord(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return {}
  return { ...(value as Record<string, unknown>) }
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? [...value] : []
}

function roomScopedRecord(
  source: Record<string, unknown> | undefined,
  roomId: number,
  key: string,
  entry: Record<string, unknown>,
) {
  const root = asRecord(source)
  const roomMap = asRecord(root.rooms)
  const roomKey = String(roomId)
  const roomConfig = asRecord(roomMap[roomKey])
  return {
    ...root,
    rooms: {
      ...roomMap,
      [roomKey]: {
        ...roomConfig,
        [key]: [...asArray(roomConfig[key]), entry],
      },
    },
  }
}

async function publishLatestDraft() {
  if (activeMode.value === 'strategy') {
    const draft = roomPolicyDraft.value
    const roomId = activeRoomId.value
    if (!draft || !roomId || settingsSaving.value || (draft.conflicts?.length ?? 0)) return
    settingsSaving.value = true
    settingsError.value = ''
    try {
      await publishLiveRoomPolicyVersion(roomId, draft.id)
      await refreshRoomPolicy()
      messages.value.push({
        role: 'agent',
        text: '用户层草稿 V' + draft.version_no + ' 已发布，当前直播间后续运行会按新的有效策略加载。',
      })
    } catch (err) {
      settingsError.value = err instanceof Error ? err.message : '发布用户层策略失败'
    } finally {
      settingsSaving.value = false
    }
    return
  }

  const draft = configVersions.value.find((item) => item.lifecycle_status === 'draft')
  if (!draft || settingsSaving.value) return
  settingsSaving.value = true
  settingsError.value = ''
  try {
    await activateLiveAgentConfigVersion(draft.id)
    await refreshAgentVersions()
    messages.value.push({
      role: 'agent',
      text: '配置 V' + draft.version_no + ' 已发布，当前直播运行可按这个版本加载。',
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '发布配置失败'
  } finally {
    settingsSaving.value = false
  }
}

async function activateSelectedVersion() {
  if (!selectedVersionId.value || selectedVersionId.value === activeConfig.value?.id) return
  settingsSaving.value = true
  settingsError.value = ''
  try {
    await activateLiveAgentConfigVersion(selectedVersionId.value)
    await refreshAgentVersions()
    const value = await getLiveAgentSettings()
    settings.value = value
    settingsDraft.value = settingsToInput(value)
    const active = activeConfig.value
    messages.value.push({
      role: 'agent',
      text: active ? `已切换到配置 V${active.version_no}。` : '配置版本已切换。',
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '切换配置版本失败'
  } finally {
    settingsSaving.value = false
  }
}

async function handleMediaUpload(event: Event, assetType: 'document' | 'audio' | 'voice_sample') {
  const target = event.target as HTMLInputElement
  const file = target.files?.[0]
  target.value = ''
  const roomId = activeRoomId.value
  if (!file || !roomId || uploading.value) return

  uploading.value = true
  settingsError.value = ''
  try {
    const result = await uploadLiveMediaAsset(file, assetType, {
      metadata: { room_id: roomId, source: 'live_strategy_agent' },
    })
    const entry = {
      asset_id: result.asset.id,
      name: result.asset.original_name,
      asset_type: result.asset.asset_type,
      created_at: result.asset.created_at,
    }
    const latest = latestConfig.value
    const payload: LiveAgentConfigInput =
      assetType === 'document'
        ? { layer3: roomScopedRecord(latest?.layer3, roomId, 'attachments', entry) }
        : { speech_config: roomScopedRecord(latest?.speech_config, roomId, 'audio_assets', entry) }
    const draft = await createLiveAgentConfigDraft(payload)
    configVersions.value = [draft, ...configVersions.value.filter((item) => item.id !== draft.id)]
    selectedVersionId.value = draft.id
    messages.value.push({
      role: 'agent',
      text: `“${file.name}”已保存为媒体资产，并关联到草稿 V${draft.version_no}。`,
    })
  } catch (err) {
    settingsError.value = err instanceof Error ? err.message : '上传媒体文件失败'
  } finally {
    uploading.value = false
  }
}

function notifySystemAgentContext() {
  window.dispatchEvent(
    new CustomEvent('system-agent-live-strategy-context', {
      detail: {
        room_id: activeRoomId.value || 0,
        mode: activeMode.value,
        module_label: activeModeLabel.value,
        plan_id: currentRoomPlanId.value || 0,
        interaction_model: activeMode.value === 'script' ? 'material_source' : 'agent_managed_versioned_module',
      },
    }),
  )
}

function handlePlanModuleUpdated(event: Event) {
  const detail = (event as CustomEvent<{ plan_id?: number; module?: string; focus?: boolean }>).detail
  if (detail?.module === 'plan') {
    if (detail.focus) activeMode.value = 'plan'
    void Promise.all([refreshLivePlans(), refreshCurrentRoomPlan()])
    return
  }
  if (!detail?.plan_id || detail.plan_id !== currentRoomPlanId.value) return
  if (detail.focus) {
    const module = detail.module
    if (
      module === 'script' ||
      module === 'products' ||
      module === 'benefits' ||
      module === 'knowledge' ||
      module === 'memory' ||
      module === 'anchor' ||
      module === 'voice' ||
      module === 'fullshow' ||
      module === 'plan' ||
      module === 'basic' ||
      module === 'strategy'
    ) {
      activeMode.value = module
    }
  }
  if (detail.module === 'products') {
    void refreshFormalProductLinks()
    return
  }
  if (detail.module === 'knowledge') {
    void refreshFormalFacts()
    return
  }
  if (detail.module === 'benefits') {
    void refreshFormalBenefits()
    return
  }
}

watch(activeRoomId, async () => {
  contentModeSaving.value = false
  contentModeNotice.value = ''
  void refreshContentMode(true)
  currentRoomPlanId.value = null
  selectedScriptId.value = null
  await Promise.all([refreshRoomPolicy(), refreshCurrentRoomPlan(false), refreshMemories()])
  await refreshPlanScripts()
  await Promise.all([loadFullShowWorkspace(), refreshFullShowVersionHistory()])
})

watch(
  activeRoomId,
  (roomId) => {
    if (roomId) window.localStorage.setItem('system-agent-live-room-id', String(roomId))
    notifySystemAgentContext()
  },
  { immediate: true },
)

watch(
  activeMode,
  (mode) => {
    if (mode === 'fullshow' && !contentModeSaving.value) void refreshContentMode(true)
    if (mode === 'anchor') void loadReusableAnchorStyleWorkspace()
    window.localStorage.setItem('system-agent-live-mode', mode)
    notifySystemAgentContext()
  },
  { immediate: true },
)

watch([activeRoomId, currentRoomPlanId], () => {
  analysisDraft.value = null
  planScripts.value = []
  selectedScriptId.value = null
  scriptReadableText.value = ''
  scriptRawText.value = ''
  anchorStyleTestResult.value = null
  anchorStyleTestError.value = ''
	anchorStyleAnalysisQC.value = null
  anchorStyleCorrectionFeedback.value = ''
  anchorStyleCorrectionCandidates.value = []
  anchorStyleCorrectionHistory.value = []
  anchorStyleCorrectionError.value = ''
  anchorStyleSourceText.value = ''
  void loadStyleOverlays()
  void loadReusableAnchorStyleWorkspace()
  void loadFullShowWorkspace()
  void refreshFullShowVersionHistory()
  notifySystemAgentContext()
})

watch(
  [
    fullShowDuration,
    fullShowRoundMinutes,
    fullShowVariantCount,
    fullShowExpansionFreedom,
    fullShowUseAnchorStyle,
    fullShowUseDynamicFacts,
    fullShowGenerateTTS,
    fullShowAvoidRecent,
  ],
  () => {
    if (fullShowWorkspaceHydrating.value || !fullShowResult.value) return
    fullShowSavedVersion.value = null
    fullShowVersionError.value = ''
  },
)

onMounted(async () => {
  window.addEventListener('live-agent-plan-module-updated', handlePlanModuleUpdated)
  window.addEventListener('live-content-policy-updated', handleContentPolicyUpdated)
  await loadAgentSettings()
  if (contentModeDisposed) return
  await refreshContentMode(true)
  if (contentModeDisposed) return
  contentModeRefreshTimer = window.setInterval(() => {
    benefitClock.value = Date.now()
    if (activeMode.value === 'fullshow' && !contentModeSaving.value && !contentModeLoading.value) void refreshContentMode()
  }, 30000)
})

onBeforeUnmount(() => {
  contentModeDisposed = true
  contentModeLoadNo++
  if (contentModeRefreshTimer !== undefined) window.clearInterval(contentModeRefreshTimer)
  window.removeEventListener('live-content-policy-updated', handleContentPolicyUpdated)
  endSupportScope?.()
  window.removeEventListener('live-agent-plan-module-updated', handlePlanModuleUpdated)
  stopFullShowGenerationProgressTimer()
  stopFormalVoiceSet()
  for (const key of fullShowVoiceTimers.keys()) stopFullShowVoiceTimer(key)
})
</script>

<template>
  <div class="live-strategy-page">


    <section class="live-strategy-shell">
      <aside class="live-strategy-rooms">
        <div class="strategy-panel-title">
          <span class="section-kicker">{{ props.supportSession ? 'CUSTOMER ROOMS' : 'MY ROOMS' }}</span>
          <h2>{{ props.supportSession ? '客户直播间' : '我的直播间' }}</h2>
        </div>
        <button
          v-for="room in rooms"
          :key="room.id"
          class="strategy-room-card"
          :class="{ active: activeRoomId === room.id }"
          type="button"
          @click="selectLiveStrategyRoom(room.id)"
        >
          <span class="strategy-room-icon">播</span>
          <span>
            <strong>{{ room.name }}</strong>
            <small>{{ room.platform }} · {{ roomStatusLabel(room.status) }}</small>
          </span>
        </button>
        <section v-if="!props.supportSession" class="live-support-authorize-panel">
          <SupportAssistantPicker :room-id="activeRoomId" />
        </section>
      </aside>

      <main class="live-strategy-agent">
        <header class="strategy-agent-head strategy-plan-container-head">
          <div>
            <div class="strategy-plan-selector-row">
              <select
                class="strategy-plan-title-switch"
                :value="currentRoomPlanId || ''"
                :disabled="planBusy || !livePlans.length"
                @change="handlePlanSwitch"
              >
                <option value="" disabled>选择智能体直播方案</option>
                <option v-for="plan in orderedRoomPlans" :key="plan.id" :value="plan.id">
                  {{ plan.name }}{{ plan.id === currentRoomPlanId ? '' : ' · ' + planRoomRelationLabel(plan) }}
                </option>
              </select>
              <button
                v-if="currentPlan && currentPlanIsBound"
                class="strategy-plan-relation-button danger"
                type="button"
                :disabled="planBusy"
                @click="unbindPlanFromCurrentRoom(currentPlan)"
              >解除绑定</button>
              <button
                v-else-if="currentPlan"
                class="strategy-plan-relation-button"
                type="button"
                :disabled="planBusy || !activeRoom"
                @click="bindPlanToCurrentRoom(currentPlan)"
              >绑定直播间</button>
              <button
                v-if="currentPlan && currentPlanIsBound && !currentPlanIsPublished"
                class="strategy-plan-relation-button publish"
                type="button"
                :disabled="planBusy || fullShowVersionPublishing || fullShowVersionSaving"
                @click="openCurrentPlanPublishing"
              >{{ fullShowVersionPublishing || fullShowVersionSaving ? '保存并发布中…' : '发布到直播间' }}</button>
            </div>
            <p class="strategy-plan-current-line">
              <template v-if="currentPlan">
                当前编辑方案：{{ currentPlan.name }}
                · 已用于 {{ currentPlan.room_count || 0 }} 个直播间
                · {{ planRoomRelationLabel(currentPlan) }}
              </template>
              <template v-else>
                当前还没有可编辑的直播方案
              </template>
            </p>
            <div class="strategy-plan-toolbar">
              <button
                class="strategy-version-button primary strategy-new-plan-button"
                :class="{ expanded: showPlanManager && planManagerMode === 'create' }"
                type="button"
                :aria-expanded="showPlanManager && planManagerMode === 'create'"
                @click="openCreatePlanManager"
              >
                <span class="strategy-plan-action-label">＋ 新建直播方案</span>
                <span
                  class="strategy-plan-expand-arrow"
                  :class="{ expanded: showPlanManager && planManagerMode === 'create' }"
                  aria-hidden="true"
                >⌄</span>
              </button>
              <button
                class="strategy-version-button strategy-manage-plan-button"
                :class="{ expanded: showPlanManager && planManagerMode === 'edit' }"
                type="button"
                :aria-expanded="showPlanManager && planManagerMode === 'edit'"
                @click="toggleEditPlanManager"
              >
                <span class="strategy-plan-action-label">方案管理</span>
                <span
                  class="strategy-plan-expand-arrow"
                  :class="{ expanded: showPlanManager && planManagerMode === 'edit' }"
                  aria-hidden="true"
                >⌄</span>
              </button>
              <button
                class="strategy-version-button strategy-delete-plan-button"
                :class="{ confirming: deleteConfirmPlanId === currentPlan?.id }"
                type="button"
                :disabled="!currentPlan || planBusy"
                @click="deleteCurrentPlan"
              >
                {{
                  planBusy
                    ? '删除中…'
                    : (deleteConfirmPlanId === currentPlan?.id ? '再次确认删除' : '删除方案')
                }}
              </button>
            </div>
          </div>
        </header>

        <section v-if="showPlanManager" class="strategy-plan-manager-inline">
          <form class="strategy-plan-create" @submit.prevent="submitPlanManager">
            <label>
              <span>方案名称</span>
              <input v-model="newPlanName" maxlength="160" placeholder="例如：菜籽油主推方案" />
            </label>
            <label>
              <span>方案说明</span>
              <input v-model="newPlanDescription" maxlength="2000" placeholder="例如：菜籽油主线 + 答疑 + 物流事实" />
            </label>
            <button class="primary-button" type="submit" :disabled="planBusy || !newPlanName.trim()">
              {{
                planBusy
                  ? (planManagerMode === 'edit' ? '保存中…' : '创建中…')
                  : (planManagerMode === 'edit' ? '保存' : '创建并绑定当前直播间')
              }}
            </button>
          </form>
        </section>

        <div class="strategy-workflow-layout" :class="'workflow-mode-' + activeMode">
          <nav class="strategy-mode-tabs strategy-mode-tabs-v2 strategy-workflow-nav" aria-label="直播方案流程">
            <div class="strategy-workflow-inputs">
            <button class="strategy-workflow-step workflow-step-products" :class="{ active: activeMode === 'products' }" @click="activeMode = 'products'">
              <span class="strategy-workflow-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M10 13a5 5 0 0 0 7.1.1l2-2a5 5 0 0 0-7.1-7.1l-1.2 1.2"/><path d="M14 11a5 5 0 0 0-7.1-.1l-2 2A5 5 0 0 0 12 20l1.2-1.2"/></svg>
              </span>
              <span class="strategy-workflow-label">商品链接</span>
            </button>
            <button class="strategy-workflow-step workflow-step-benefits" :class="{ active: activeMode === 'benefits' }" @click="activeMode = 'benefits'">
              <span class="strategy-workflow-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><rect x="3" y="8" width="18" height="13" rx="2"/><path d="M12 8v13M3 12h18M7.5 8C5.5 8 5 6.8 5 5.8 5 4.8 5.8 4 6.8 4c2.2 0 5.2 4 5.2 4M16.5 8C18.5 8 19 6.8 19 5.8 19 4.8 18.2 4 17.2 4 15 4 12 8 12 8"/></svg>
              </span>
              <span class="strategy-workflow-label">福利活动</span>
            </button>
            <button class="strategy-workflow-step workflow-step-knowledge" :class="{ active: activeMode === 'knowledge' }" @click="activeMode = 'knowledge'">
              <span class="strategy-workflow-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><path d="M6 3h9l3 3v15H6z"/><path d="M15 3v4h4M9 11h6M9 15h6M9 19h4"/></svg>
              </span>
              <span class="strategy-workflow-label">事实依据</span>
            </button>
            <button class="strategy-workflow-step workflow-step-anchor" :class="{ active: activeMode === 'anchor' }" @click="activeMode = 'anchor'">
              <span class="strategy-workflow-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><circle cx="10" cy="8" r="3"/><path d="M4 20c0-4 2.5-6 6-6s6 2 6 6"/><path d="m18 4 .6 1.4L20 6l-1.4.6L18 8l-.6-1.4L16 6l1.4-.6z"/></svg>
              </span>
              <span class="strategy-workflow-label">主播风格</span>
            </button>
            <button class="strategy-workflow-step workflow-step-voice" :class="{ active: activeMode === 'voice' }" @click="activeMode = 'voice'">
              <span class="strategy-workflow-icon" aria-hidden="true">
                <svg viewBox="0 0 24 24"><rect x="8" y="3" width="8" height="12" rx="4"/><path d="M5 11a7 7 0 0 0 14 0M12 18v3M9 21h6"/></svg>
              </span>
              <span class="strategy-workflow-label">声音</span>
            </button>
              <button
                class="strategy-workflow-output workflow-step-fullshow"
                :class="{ active: activeMode === 'fullshow' }"
                @click="activeMode = 'fullshow'"
              >
                <span class="strategy-workflow-output-mark">✦</span>
                <span class="strategy-workflow-output-copy">
                  <strong>生成智能体</strong>
                </span>
              </button>
            </div>
          </nav>

          <div class="strategy-workflow-content">

        <div
          v-if="activeMode === 'plan' || activeMode === 'basic' || activeMode === 'strategy' || activeMode === 'anchor'"
          class="strategy-legacy-version-bar strategy-legacy-version-bar-hidden"
        >
          <div>
            <span>兼容配置</span>
            <strong>{{ versionLabel }}</strong>
          </div>
          <select
            v-if="activeMode !== 'strategy' && configVersions.length"
            v-model.number="selectedVersionId"
            class="strategy-version-select"
            :disabled="settingsSaving"
          >
            <option
              v-for="version in configVersions"
              :key="version.id"
              :value="version.id"
            >
              V{{ version.version_no }} · {{ version.lifecycle_status }}
            </option>
          </select>
          <button
            v-if="activeMode !== 'strategy' && selectedVersionId && selectedVersionId !== activeConfig?.id"
            class="strategy-version-button"
            type="button"
            :disabled="settingsSaving"
            @click="activateSelectedVersion"
          >
            切换旧配置版本
          </button>
          <button
            v-if="activeMode === 'strategy' ? !!roomPolicyDraft : latestConfig?.lifecycle_status === 'draft'"
            class="strategy-version-button primary"
            type="button"
            :disabled="settingsSaving || (activeMode === 'strategy' && !!roomPolicyDraft?.conflicts?.length)"
            @click="publishLatestDraft"
          >
            {{ activeMode === 'strategy' && roomPolicyDraft?.conflicts?.length ? '存在冲突' : '发布兼容草稿' }}
          </button>
        </div>

    <section v-if="activeMode === 'script'" class="strategy-v2-workspace strategy-script-workspace">
          <div class="strategy-v2-section-head">
            <div>
              <h3>直播素材</h3>
              <p class="strategy-script-intro">这是当前直播方案的原始资料入口。素材本身不等于正式事实；智能分析只负责识别内容并把候选信息分别归位到商品链接、活动福利、事实依据和主播风格。</p>
            </div>
          </div>
          <div class="strategy-script-simple-upload">
            <input
              ref="scriptFileInput"
              type="file"
              hidden
              accept=".txt,.md,.jpg,.jpeg,.png,.webp,text/plain,text/markdown,image/jpeg,image/png,image/webp"
              @change="handlePlanScriptFile"
            />
            <button class="strategy-version-button" type="button" :disabled="!currentPlan || scriptRecognizing" @click="scriptFileInput?.click()">
              {{ scriptRecognizing ? '图片识别中…' : '上传文字 / 图片素材' }}
            </button>
            <button
              class="strategy-version-button"
              type="button"
              :disabled="!currentPlan || scriptBusy || scriptRecognizing || !scriptReadableText.trim()"
              @click="savePlanScript()"
            >
              {{ scriptBusy ? '保存中…' : '保存素材' }}
            </button>
            <button
              class="primary-button"
              type="button"
              :disabled="!currentPlan || scriptBusy || scriptRecognizing || scriptAnalyzing || !scriptReadableText.trim()"
              @click="analyzeCurrentPlanScript"
            >
              {{ scriptAnalyzing ? '智能分析中…' : '智能归位素材' }}
            </button>
          </div>

          <div v-if="settingsError" class="inline-error">{{ settingsError }}</div>
          <div v-if="scriptNotice" class="notice-banner">{{ scriptNotice }}</div>
          <div
            v-if="scriptAnalyzing || scriptAnalysisProgress > 0"
            class="strategy-analysis-progress"
            role="status"
            aria-live="polite"
          >
            <div class="strategy-analysis-progress-head">
              <strong>{{ scriptAnalysisStage }}</strong>
              <span>{{ scriptAnalysisProgress }}%</span>
            </div>
            <div class="strategy-analysis-progress-track">
              <span :style="{ width: scriptAnalysisProgress + '%' }"></span>
            </div>
          </div>
          <div v-if="scriptNotice && !scriptAnalyzing" class="strategy-script-notice">{{ scriptNotice }}</div>

          <template v-if="currentPlan">
            <section class="strategy-script-editor strategy-script-editor-single">
              <textarea
                v-model="scriptReadableText"
                rows="22"
                placeholder="粘贴或上传当前直播素材。当前已接通 TXT / MD 文本素材；图片、截图、PDF、Word、Excel 等会继续接入同一素材入口。"
                @input="syncSimpleScriptText"
              ></textarea>
            </section>

            <div v-if="currentAnalysis" class="strategy-analysis-summary">
              <div>
                <span class="section-kicker">AI ANALYSIS</span>
                <h4>{{ analysisDraft ? '智能分析草稿（未保存）' : '已保存的分析结果' }}</h4>
                <p>
                  {{ currentAnalysis.summary || '已完成事实候选和直播节奏提取。' }}
                </p>
              </div>
              <div class="strategy-analysis-metrics">
                <button type="button" @click="activeMode = 'products'">
                  <strong>{{ currentAnalysis.product_links?.length || 0 }}</strong>
                  <span>商品链接</span>
                </button>
                <button type="button" @click="activeMode = 'benefits'">
                  <strong>{{ activityBenefits.length }}</strong>
                  <span>活动福利</span>
                </button>
                <button type="button" @click="activeMode = 'knowledge'">
                  <strong>{{ currentAnalysis.facts.length }}</strong>
                  <span>事实依据</span>
                </button>
                <button type="button" @click="activeMode = 'anchor'">
                  <strong>{{ currentAnalysis.anchor_style?.dimensions?.length || 0 }}</strong>
                  <span>主播风格</span>
                </button>
              </div>
            </div>
          </template>
        </section>

        <section v-else-if="activeMode === 'products'" class="strategy-v2-workspace strategy-products-workspace">
          <section class="strategy-product-formal">
            <header>
              <div>
                <strong>已采纳商品链接</strong>
                <span>当前正式 {{ formalProductLinks.length }} 个</span>
              </div>
            </header>
            <div v-if="!formalProductLinks.length" class="strategy-v2-empty strategy-product-formal-empty">
              <strong>还没有正式商品链接</strong>
              <span>可以向右侧智能体提供商品信息，添加需要使用的商品链接。</span>
            </div>
            <LiveProductLinkCards v-else :key="currentRoomPlanId || 0" :items="formalProductLinks" :benefits="activeFormalBenefits" :save-field="saveProductLinkField" :remove-item="removeProductLink" :add-attribute="addProductAttribute" :save-attribute="saveProductAttribute" :remove-attribute="removeProductAttribute" />
          </section>

          <div class="strategy-product-link-grid">
            <article
              v-for="link in productLinks"
              :key="link.link_key"
              class="strategy-product-link-card"
              :class="'is-' + (link.review_bucket || 'discuss')"
            >
              <header>
                <div>
                  <span>{{ link.link_key }}</span>
                  <strong>{{ link.product_name || '商品名称待确认' }}</strong>
                </div>
                <em>{{ reviewBucketLabel(link.review_bucket) }}</em>
              </header>
              <dl>
                <div><dt>规格</dt><dd>{{ link.spec || '—' }}</dd></div>
                <div><dt>日常价</dt><dd>{{ link.daily_price || '—' }}</dd></div>
                <div><dt>活动价</dt><dd>{{ link.activity_price || '—' }}</dd></div>
                <div><dt>数量 / 组合</dt><dd>{{ link.quantity || '—' }}</dd></div>
                <div><dt>赠品</dt><dd>{{ link.gift || '—' }}</dd></div>
                <div><dt>活动</dt><dd>{{ link.activity || '—' }}</dd></div>
                <div><dt>适用</dt><dd>{{ link.audience || '—' }}</dd></div>
                <div><dt>判断说明</dt><dd>{{ link.review_reason || '—' }}</dd></div>
              </dl>
              <details v-if="link.attributes?.length" class="strategy-product-link-attributes">
                <summary>个性属性（{{ link.attributes.length }} 项）<span>展开反推结果</span></summary>
                <dl>
                  <div v-for="attribute in link.attributes" :key="attribute.code"><dt>{{ attribute.label }}</dt><dd>{{ attribute.value }}{{ attribute.unit || '' }}</dd></div>
                </dl>
              </details>
              <section class="strategy-product-link-evidence">
                <b>原文依据</b>
                <p v-if="!link.source_quotes?.length">—</p>
                <p v-for="(quote, index) in link.source_quotes || []" :key="link.link_key + ':quote:' + index">{{ quote }}</p>
              </section>
              <footer class="strategy-product-link-footer">
                <span>采纳商品链接只写入稳定信息；活动价、赠品、活动内容会留给“活动福利”。</span>
                <button
                  class="primary-button"
                  type="button"
                  :disabled="link.review_bucket === 'violation' || link.review_bucket === 'conflict' || !!adoptingProductLinkKey"
                  @click="adoptProductLinkCandidate(link)"
                >
                  {{ adoptingProductLinkKey === link.link_key ? '采纳中…' : '采纳候选' }}
                </button>
              </footer>
            </article>
          </div>
        </section>

        <section v-else-if="activeMode === 'benefits'" class="strategy-v2-workspace">
          <div class="strategy-v2-section-head">
            <div>
              <h3>活动福利</h3>
              <p>这里专门承接活动价、赠品、满减、限时权益等有时效的内容。活动福利和稳定事实分开管理，过期后不能继续被直播智能体引用。</p>
            </div>
            <button class="strategy-version-button" type="button" @click="activeMode = 'anchor'">返回主播风格</button>
          </div>

          <section class="strategy-benefit-formal">
            <header>
              <div>
                <strong>已采纳活动福利</strong>
                <span>正式活动 {{ formalBenefits.length }} 条</span>
              </div>
            </header>
            <div v-if="!formalBenefits.length" class="strategy-v2-empty strategy-benefit-formal-empty">
              <strong>还没有正式活动福利</strong>
              <span>从下方素材候选采纳，或后续直接对系统智能体说“新增活动 / 修改活动 / 停用活动”。</span>
            </div>
            <div v-else class="strategy-benefit-formal-list">
              <article v-for="item in formalBenefits" :key="item.id" class="strategy-benefit-formal-item" :class="{ expanded: expandedFormalBenefitIds.has(item.id) }">
                <header class="strategy-benefit-formal-card-head">
                  <div class="strategy-benefit-formal-icon strategy-benefit-formal-link-icon" aria-hidden="true">
                    <svg viewBox="0 0 24 24"><path d="M10.5 13.5 13.5 10.5M7.7 16.3l-1.4 1.4a4 4 0 0 1-5.7-5.7l3.7-3.7a4 4 0 0 1 5.7 0l.7.7M16.3 7.7l1.4-1.4a4 4 0 0 1 5.7 5.7l-3.7 3.7a4 4 0 0 1-5.7 0l-.7-.7" /></svg>
                  </div>
                  <div class="strategy-benefit-formal-title">
                    <span>{{ item.link_key || '全直播间' }}</span>
                    <strong>{{ item.product_name || item.key }}</strong>
                    <small>V{{ item.version_no }} · {{ item.key }}</small>
                    <em :class="'is-' + item.status"><i></i>{{ benefitStatusLabel(item.status) }}</em>
                  </div>
                </header>

                <div class="strategy-benefit-formal-rule" aria-hidden="true"></div>

                <div class="strategy-benefit-formal-detail">
                  <div class="strategy-benefit-formal-detail-item">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m3 11 8-8h9v9l-8 8a2 2 0 0 1-2.8 0l-6.2-6.2A2 2 0 0 1 3 11Z" /><circle cx="15.5" cy="7.5" r="1.25" /></svg>
                    <span>活动价：</span>
                    <strong>{{ item.activity_price || '未设置' }}</strong>
                  </div>
                  <div class="strategy-benefit-formal-detail-item">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 7v14M4 4h16v5H4zM3 9h18v4H3zM12 4c0-2-3-3-4.5-1.5S8.5 6 12 6M12 4c0-2 3-3 4.5-1.5S15.5 6 12 6" /></svg>
                    <span>福利：</span>
                    <strong>{{ item.gift || '未设置' }}</strong>
                  </div>
                  <div class="strategy-benefit-formal-detail-item strategy-benefit-formal-detail-wide">
                    <svg viewBox="0 0 24 24" aria-hidden="true"><path d="m3 11 12-5v12L3 13zM15 9.5 21 7v10l-6-2.5M6 14l1.5 6h3L9 14" /></svg>
                    <span>活动：</span>
                    <strong>{{ item.activity || '未设置' }}</strong>
                  </div>
                </div>

                <div class="strategy-benefit-formal-rule" aria-hidden="true"></div>

                <div v-if="expandedFormalBenefitIds.has(item.id)" class="strategy-benefit-formal-window">
                  <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8.5" /><path d="M12 7v5l3.5 2" /></svg>
                  <div>
                    <span>有效期</span>
                    <strong>{{ formatBenefitTime(item.starts_at) }} <i>→</i> {{ formatBenefitTime(item.ends_at) }}</strong>
                    <small v-if="item.source_quote">依据：{{ item.source_quote }}</small>
                    <small v-else-if="item.source_review_reason">判断：{{ item.source_review_reason }}</small>
                  </div>
                </div>
                <button type="button" class="strategy-benefit-formal-expand" :aria-expanded="expandedFormalBenefitIds.has(item.id)" @click="toggleFormalBenefit(item.id)"><span>{{ expandedFormalBenefitIds.has(item.id) ? '收起条件与有效期' : '展开条件与有效期' }}</span><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg></button>
              </article>
            </div>
          </section>

          <div class="strategy-benefit-grid">
            <article
              v-for="benefit in activityBenefits"
              :key="benefit.key"
              class="strategy-benefit-card"
              :class="'is-' + benefit.review_bucket"
            >
              <header>
                <div>
                  <span>{{ benefit.link_key }}</span>
                  <strong>{{ benefit.product_name }}</strong>
                </div>
                <em>素材候选 · {{ reviewBucketLabel(benefit.review_bucket) }}</em>
              </header>
              <dl>
                <div><dt>活动价</dt><dd>{{ benefit.activity_price || '—' }}</dd></div>
                <div><dt>赠品 / 权益</dt><dd>{{ benefit.gift || '—' }}</dd></div>
                <div><dt>活动口径</dt><dd>{{ benefit.activity || '—' }}</dd></div>
                <div><dt>判断说明</dt><dd>{{ benefit.review_reason || '当前由素材分析得到，尚未进入正式活动福利版本' }}</dd></div>
              </dl>
              <section class="strategy-product-link-evidence">
                <b>素材依据</b>
                <p v-if="!benefit.source_quotes?.length">—</p>
                <p v-for="(quote, index) in benefit.source_quotes || []" :key="benefit.key + ':quote:' + index">{{ quote }}</p>
              </section>
              <footer>
                <span>没有有效期时只会采纳成草稿，不会进入直播智能体。</span>
                <button
                  class="primary-button"
                  type="button"
                  :disabled="benefit.review_bucket === 'violation' || benefit.review_bucket === 'conflict' || !!adoptingBenefitKey"
                  @click="adoptBenefitCandidate(benefit)"
                >
                  {{ adoptingBenefitKey === benefit.key ? '采纳中…' : '采纳候选' }}
                </button>
              </footer>
            </article>
          </div>
        </section>

        <section v-else-if="activeMode === 'knowledge'" class="strategy-v2-workspace">
          <div class="strategy-v2-section-head">
            <div>
              <h3>事实依据</h3>
            </div>
            <div class="strategy-fact-head-actions">
              <button class="primary-button" type="button" :disabled="!currentPlan" @click="beginCreateFormalFact">＋ 新增事实依据</button>
            </div>
          </div>

          <section class="strategy-formal-facts">
            <header class="strategy-formal-facts-head">
              <div>
                <span class="section-kicker">CURRENT PLAN FACTS</span>
                <strong>当前方案已采纳事实</strong>
                <small>{{ currentPlan?.name || '未选择方案' }} · {{ formalFacts.length }} 条</small>
              </div>
              <span>以下内容同时供主线口播和互动回答使用；修改、删除都会记录版本历史。</span>
            </header>

            <div v-if="creatingFormalFact" class="strategy-formal-fact-create">
              <label><span>事实分类</span><select v-model="formalFactCreateDraft.category"><option v-for="option in formalFactCategoryOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
              <label><span>事实名称</span><input v-model="formalFactCreateDraft.key" maxlength="255" placeholder="例如：适用人群" /></label>
              <label class="is-wide"><span>事实内容（可以说）</span><textarea v-model="formalFactCreateDraft.value" rows="3" maxlength="4000" placeholder="例如：本品为非转基因菜籽油"></textarea></label>
              <label><span>不要这样说（选填）</span><textarea v-model="formalFactCreateDraft.forbidden_wording" rows="3" maxlength="4000" placeholder="一行一句，例如：老人小孩吃了更好"></textarea></label>
              <label><span>改成这样说（选填）</span><textarea v-model="formalFactCreateDraft.safe_rewrite" rows="3" maxlength="4000" placeholder="例如：家里日常做饭可以按页面信息选择"></textarea></label>
              <div class="strategy-formal-fact-create-actions"><button class="primary-button" type="button" :disabled="mutatingFormalFactId === -1" @click="createFormalFact">{{ mutatingFormalFactId === -1 ? '保存中…' : '保存事实依据' }}</button><button type="button" :disabled="mutatingFormalFactId === -1" @click="cancelCreateFormalFact">取消</button></div>
            </div>

            <div v-if="!formalFacts.length" class="strategy-v2-empty strategy-formal-facts-empty">
              <strong>当前方案还没有正式事实</strong>
              <span>可以从上面的素材候选采纳，也可以点击“新增事实依据”直接录入。</span>
            </div>

            <div v-else class="strategy-formal-facts-table-wrap">
              <table class="strategy-formal-facts-table">
                <thead>
                  <tr>
                    <th>分类</th>
                    <th>事实名称</th>
                    <th>事实内容（可以说）</th>
                    <th>不要这样说</th>
                    <th>改成这样说</th>
                    <th>版本</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="item in formalFacts" :key="item.id">
                    <template v-if="editingFormalFactId === item.id">
                      <td>
                        <select v-model="formalFactEditDraft.category">
                          <option
                            v-for="option in formalFactCategoryOptions"
                            :key="option.value"
                            :value="option.value"
                          >
                            {{ option.label }}
                          </option>
                        </select>
                      </td>
                      <td>
                        <input v-model="formalFactEditDraft.key" maxlength="255" />
                      </td>
                      <td>
                        <textarea v-model="formalFactEditDraft.value" rows="3" maxlength="4000"></textarea>
                      </td>
                      <td><textarea v-model="formalFactEditDraft.forbidden_wording" rows="3" maxlength="4000" placeholder="选填，一行一句"></textarea></td>
                      <td><textarea v-model="formalFactEditDraft.safe_rewrite" rows="3" maxlength="4000" placeholder="选填，写建议说法"></textarea></td>
                      <td class="strategy-formal-fact-version">
                        V{{ item.version_no }}
                        <small>保存后升级版本</small>
                      </td>
                      <td>
                        <div class="strategy-formal-fact-actions">
                          <button
                            class="primary-button"
                            type="button"
                            :disabled="mutatingFormalFactId === item.id"
                            @click="saveFormalFactEdit(item)"
                          >
                            {{ mutatingFormalFactId === item.id ? '保存中…' : '保存' }}
                          </button>
                          <button type="button" :disabled="mutatingFormalFactId === item.id" @click="cancelFormalFactEdit">
                            取消
                          </button>
                        </div>
                      </td>
                    </template>
                    <template v-else>
                      <td><span class="strategy-formal-fact-category">{{ factCategoryLabel(item.category) }}</span></td>
                      <td><strong>{{ item.key }}</strong></td>
                      <td class="strategy-formal-fact-value">{{ item.value }}</td>
                      <td class="strategy-formal-fact-boundary">{{ item.forbidden_wording || '—' }}</td>
                      <td class="strategy-formal-fact-rewrite">{{ item.safe_rewrite || '—' }}</td>
                      <td class="strategy-formal-fact-version">
                        V{{ item.version_no }}
                        <small>{{ item.source_type === 'manual_create' ? '人工新增' : (item.source_type === 'manual_edit' ? '人工修改' : '采纳生成') }}</small>
                      </td>
                      <td>
                        <div class="strategy-formal-fact-actions">
                          <button
                            type="button"
                            :disabled="!!mutatingFormalFactId"
                            @click="startFormalFactEdit(item)"
                          >
                            修改
                          </button>
                          <button
                            class="is-danger"
                            type="button"
                            :disabled="!!mutatingFormalFactId"
                            @click="deleteFormalFact(item)"
                          >
                            删除
                          </button>
                        </div>
                      </td>
                    </template>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </section>

        <section v-else-if="activeMode === 'memory'" class="strategy-v2-workspace interaction-strategy-workspace">
          <div class="strategy-v2-section-head interaction-strategy-head">
            <div>
              <h3>互动策略</h3>
              <p>这里只规定遇到什么情况要回应、优先级、频率和怎么组织回答；商品、履约、售后等回答内容只能来自“事实依据”。</p>
            </div>
          </div>
          <RoomInteractionPreferences v-if="activeRoomId" :room-id="activeRoomId" />
          <div v-else class="strategy-v2-empty">
            <strong>先选择一个直播间</strong>
            <span>互动偏好按直播间保存，选择直播间后即可调整。</span>
          </div>
          <div class="strategy-v2-section-head interaction-rules-head">
            <div>
              <h3>已采用的互动规则</h3>
              <p>这里保留互动规范，例如“人多时不要逐个点名”“售后问题必须优先回答”。它不能新增商品事实，也不能覆盖事实依据。</p>
            </div>
            <button class="strategy-version-button" type="button" @click="refreshMemories">刷新规则</button>
          </div>
          <div v-if="!interactionMemories.length" class="strategy-v2-empty">
            <strong>当前直播间还没有已采用的互动策略</strong>
            <span>在直播公屏或智能体对话中确认的互动规则会进入这里；其它类型纠正会自动路由到对应模块。</span>
          </div>
          <div v-else class="strategy-memory-list">
            <article v-for="item in interactionMemories" :key="item.id" :class="{ editing: editingMemoryId === item.id }">
              <span>{{ memoryTypeLabel(item.memory_type) }}</span>
              <div>
                <strong>{{ item.target || item.memory_key }}</strong>
                <p>{{ item.current_version?.content_text || '当前版本暂无文字内容' }}</p>
                <small>记忆键：{{ item.memory_key }} · V{{ item.current_version?.version_no || 1 }}</small>
              </div>
              <div class="strategy-memory-card-actions">
                <button
                  type="button"
                  :disabled="memoryMutatingId === item.id"
                  @click="editingMemoryId === item.id ? resetMemoryEdit() : beginMemoryEdit(item)"
                >{{ editingMemoryId === item.id ? '收起修改' : '修改' }}</button>
                <button
                  type="button"
                  class="danger"
                  :class="{ confirming: memoryDeleteConfirmId === item.id }"
                  :disabled="memoryMutatingId === item.id"
                  @click="deleteMemory(item)"
                >{{ memoryMutatingId === item.id ? '处理中…' : memoryDeleteConfirmId === item.id ? '确认删除' : '删除' }}</button>
              </div>
              <div v-if="editingMemoryId === item.id" class="strategy-memory-edit-panel">
                <label>
                  <span>修改要求</span>
                  <textarea
                    v-model="memoryEditFeedback"
                    rows="3"
                    placeholder="直接说明互动规范，例如：售后问题必须优先回答，但答案内容仍以事实依据为准。"
                  ></textarea>
                </label>
                <div class="strategy-memory-edit-actions">
                  <button
                    type="button"
                    :disabled="memoryMutatingId === item.id || !memoryEditFeedback.trim()"
                    @click="previewMemoryEdit(item)"
                  >{{ memoryMutatingId === item.id ? '处理中…' : memoryEditPreview ? '重新生成预览' : '生成修改预览' }}</button>
                  <button type="button" @click="resetMemoryEdit">取消</button>
                </div>
                <div v-if="memoryEditPreview" class="strategy-memory-edit-preview">
                  <strong>修改后预览</strong>
                  <p>{{ memoryEditPreview }}</p>
                  <button
                    class="primary"
                    type="button"
                    :disabled="memoryMutatingId === item.id"
                    @click="adoptMemoryEdit(item)"
                  >{{ memoryMutatingId === item.id ? '采用中…' : '采用修改' }}</button>
                </div>
                <small v-if="memoryEditError" class="strategy-memory-edit-error">{{ memoryEditError }}</small>
              </div>
            </article>
          </div>
        </section>

        <section v-else-if="activeMode === 'addressing' && !props.supportSession" class="strategy-v2-workspace addressing-workspace">
          <div class="strategy-v2-section-head">
            <div>
              <h3>称呼策略</h3>
              <p>系统默认称呼由后台统一维护；你也可以切换成“我的称呼”。自定义称呼保存前会经过大模型安全审核，多个称呼按设置概率随机使用。</p>
            </div>
            <button
              class="primary-button"
              type="button"
              :disabled="addressingSaving"
              @click="saveAddressingStrategy"
            >{{ addressingSaving ? '审核保存中…' : '保存称呼策略' }}</button>
          </div>

          <div v-if="addressingError" class="inline-error">{{ addressingError }}</div>
          <div v-if="addressingNotice" class="addressing-notice">{{ addressingNotice }}</div>

          <div class="addressing-mode-grid">
            <button
              type="button"
              :class="{ active: addressingStrategy.addressing_mode !== 'custom' }"
              @click="setAddressingMode('system')"
            >
              <strong>系统默认称呼</strong>
              <span>直接跟随后台维护的安全称呼和默认随机概率。</span>
            </button>
            <button
              type="button"
              :class="{ active: addressingStrategy.addressing_mode === 'custom' }"
              @click="setAddressingMode('custom')"
            >
              <strong>我的称呼</strong>
              <span>自己添加和分配概率，审核通过后只作用于你的账号。</span>
            </button>
          </div>

          <section class="addressing-system-card">
            <header>
              <div>
                <span>SYSTEM DEFAULT</span>
                <strong>系统默认称呼</strong>
              </div>
              <b v-if="addressingStrategy.addressing_mode !== 'custom'">正在使用</b>
            </header>
            <div class="addressing-chip-list">
              <div v-for="option in addressingSystemOptions" :key="option.key" class="addressing-chip">
                <strong>{{ option.text }}</strong>
                <span>{{ option.enabled ? option.probability + '%' : '停用' }}</span>
              </div>
            </div>
          </section>

          <section class="addressing-custom-card" :class="{ inactive: addressingStrategy.addressing_mode !== 'custom' }">
            <header>
              <div>
                <span>USER CUSTOM</span>
                <strong>我的称呼</strong>
              </div>
              <div class="addressing-custom-actions">
                <button type="button" class="strategy-version-button" @click="addAddressingOption">＋ 新增称呼</button>
                <button
                  type="button"
                  class="primary-button addressing-save-button"
                  :disabled="addressingSaving"
                  @click="saveAddressingStrategy"
                >{{ addressingSaving ? '审核保存中…' : '保存并生效' }}</button>
              </div>
            </header>

            <div v-if="!addressingCustomOptions.length" class="strategy-v2-empty addressing-empty">
              <strong>还没有自定义称呼</strong>
              <span>点击“新增称呼”，例如填写“老哥”“朋友”“老板”，然后给每个称呼分配随机概率。</span>
            </div>
            <div v-else class="addressing-custom-list">
              <article v-for="option in addressingCustomOptions" :key="option.key">
                <label class="addressing-enable">
                  <input v-model="option.enabled" type="checkbox" />
                  <span>{{ option.enabled ? '启用' : '停用' }}</span>
                </label>
                <label class="addressing-name-field">
                  <span>称呼</span>
                  <input v-model="option.text" maxlength="12" placeholder="例如：老哥" />
                </label>
                <label class="addressing-probability-field">
                  <span>随机概率</span>
                  <div><input v-model.number="option.probability" type="number" min="0" max="100" step="1" :disabled="!option.enabled" /><b>%</b></div>
                </label>
                <button type="button" class="addressing-remove" @click="removeAddressingOption(option)">删除</button>
              </article>
            </div>

            <p class="addressing-review-tip">保存生效时系统会自动优化当前称呼的实际比例。自定义称呼审核不通过时不会发布到直播间。</p>
          </section>
        </section>

        <section v-else-if="activeMode === 'plan'" class="strategy-plan-settings">
          <div class="strategy-basic-settings-head">
            <div>
              <span class="section-kicker">LIVE AGENT PLANS</span>
              <h3>智能体直播方案</h3>
              <p>一个方案可以给当前客户的多个直播间共用；每个直播间同时选择一个当前运行方案。</p>
            </div>
          </div>

          <div v-if="settingsError" class="inline-error">{{ settingsError }}</div>

          <form class="strategy-plan-create" @submit.prevent="createPlanForCurrentRoom">
            <input v-model="newPlanName" maxlength="160" placeholder="方案名称，例如：跑山鸡中控方案" />
            <input v-model="newPlanDescription" maxlength="2000" placeholder="方案说明，例如：中控答疑、物流/价格/吃法优先" />
            <button class="primary-button" type="submit" :disabled="planBusy || !newPlanName.trim()">
              {{ planBusy ? '处理中…' : '＋ 新建方案' }}
            </button>
          </form>

          <div v-if="!livePlans.length" class="strategy-plan-empty">还没有直播方案，先在上面创建一个。</div>
          <div v-else class="strategy-plan-grid">
            <article
              v-for="plan in livePlans"
              :key="plan.id"
              class="strategy-plan-card"
              :class="{ active: currentRoomPlanId === plan.id, bound: boundPlanIdSet.has(plan.id), runtime: runtimePlanId === plan.id }"
            >
              <div>
                <span>
                  {{ runtimePlanId === plan.id
                    ? '当前直播使用中 · 已发布'
                    : publishedPlanIdSet.has(plan.id)
                      ? '已发布当前直播间'
                      : boundPlanIdSet.has(plan.id)
                        ? '已绑定 · 未发布'
                        : '未绑定当前直播间' }}
                </span>
                <strong>{{ plan.name }}</strong>
                <p>{{ plan.description || '暂无方案说明' }}</p>
                <small>已用于 {{ plan.room_count || 0 }} 个直播间 · 专用词 {{ plan.term_count || 0 }} 条</small>
              </div>
              <div class="strategy-plan-card-actions">
                <button
                  v-if="boundPlanIdSet.has(plan.id)"
                  type="button"
                  :disabled="planBusy || currentRoomPlanId === plan.id"
                  @click="currentRoomPlanId = plan.id; refreshPlanScripts()"
                >{{ currentRoomPlanId === plan.id ? '正在编辑' : '编辑方案' }}</button>
                <button
                  v-if="publishedPlanIdSet.has(plan.id)"
                  type="button"
                  class="primary"
                  :disabled="planBusy || runtimePlanId === plan.id"
                  @click="usePlanForCurrentRoom(plan)"
                >{{ runtimePlanId === plan.id ? '直播使用中' : '切换使用' }}</button>
                <button
                  v-if="boundPlanIdSet.has(plan.id)"
                  type="button"
                  class="danger"
                  :disabled="planBusy"
                  @click="unbindPlanFromCurrentRoom(plan)"
                >取消绑定</button>
                <button
                  v-else
                  type="button"
                  class="primary"
                  :disabled="planBusy || !activeRoom"
                  @click="bindPlanToCurrentRoom(plan)"
                >绑定当前房间</button>
              </div>
            </article>
          </div>
        </section>

        <section v-else-if="activeMode === 'basic'" class="strategy-basic-settings">
          <div class="strategy-basic-settings-head">
            <div>
              <span class="section-kicker">AGENT IDENTITY</span>
              <h3>基础设置</h3>
              <p>定义这个 Agent 叫什么、是谁、负责什么。这里是当前客户共用设置，所有直播间统一使用。</p>
            </div>
            <button
              class="primary-button"
              type="button"
              :disabled="settingsSaving"
              @click="saveAgentSettings"
            >
              {{ settingsSaving ? '保存中…' : '保存基础设置' }}
            </button>
          </div>

          <div v-if="settingsError" class="inline-error">{{ settingsError }}</div>

          <div class="strategy-basic-grid">
            <label>
              <span>Agent 名称</span>
              <input
                v-model="settingsDraft.display_name"
                maxlength="128"
                placeholder="例如：小伴直播教练"
              />
            </label>
            <label>
              <span>身份定位</span>
              <input
                v-model="settingsDraft.role_name"
                maxlength="160"
                placeholder="例如：直播策略与场控 Agent"
              />
            </label>
            <label class="wide">
              <span>自我介绍</span>
              <textarea
                v-model="settingsDraft.self_introduction"
                rows="2"
                maxlength="600"
                placeholder="用户问“你是谁”时，优先按这里回答。"
              ></textarea>
            </label>
            <label class="wide">
              <span>主要职责与边界</span>
              <textarea
                v-model="settingsDraft.mission"
                rows="3"
                maxlength="1200"
                placeholder="说明能做什么、负责什么，以及不能越过哪些系统边界。"
              ></textarea>
            </label>
            <label class="wide">
              <span>首次问候语</span>
              <textarea
                v-model="settingsDraft.greeting"
                rows="2"
                maxlength="600"
                placeholder="进入直播策略 Agent 后第一句话。"
              ></textarea>
            </label>
          </div>

          <div class="strategy-identity-preview">
            <span>身份预览</span>
            <strong>{{ settingsDraft.display_name || '未命名 Agent' }}</strong>
            <p>{{ settingsDraft.self_introduction || '还没有填写自我介绍。' }}</p>
            <small>{{ settingsDraft.mission }}</small>
          </div>
        </section>

        <section v-else-if="activeMode === 'anchor'" class="strategy-v2-workspace strategy-anchor-style-workspace">
          <div class="reusable-anchor-style-workspace">
            <aside class="reusable-anchor-style-sidebar">
              <div class="reusable-anchor-style-sidebar-head">
                <div><span>主播库</span><strong>已保存的主播风格</strong></div>
                <button type="button" class="primary-button" :disabled="reusableAnchorStyleBusy" @click="createReusableAnchorStyle">＋新建主播</button>
              </div>
              <p class="reusable-anchor-style-help">选中后，测试文案和当前智能体方案都会按这名主播的说话方式生成。</p>
              <div v-if="!reusableAnchorStyles.length" class="reusable-anchor-style-empty">还没有保存主播。先新建主播，再导入一份或多份真实话术。</div>
              <button
                v-for="style in reusableAnchorStyles"
                :key="style.id"
                type="button"
                class="reusable-anchor-style-card"
                :class="{ selected: selectedReusableAnchorStyleId === style.id }"
                @click="selectReusableAnchorStyle(style.id)"
              >
                <span class="reusable-anchor-style-card-radio">{{ selectedReusableAnchorStyleId === style.id ? '✓' : '' }}</span>
                <span><strong>{{ style.name }}</strong><small>{{ style.description || '还没有填写风格介绍' }}</small><em>{{ style.sample_count }} 份样本 · {{ style.training_count }} 次训练</em></span>
              </button>
              <button v-if="selectedReusableAnchorStyle" type="button" class="reusable-anchor-style-bind" :disabled="!currentRoomPlanId" @click="bindReusableAnchorStyle">
                {{ currentRoomPlanId ? '设为当前智能体主播' : '先选择智能体方案' }}
              </button>
              <button v-if="selectedReusableAnchorStyle" type="button" class="reusable-anchor-style-settings-link" @click="reusableAnchorStyleTab = 'personalization'">⚙ 当前系统的个性插件</button>
            </aside>

            <div class="reusable-anchor-style-main">
              <div class="reusable-anchor-style-main-head">
                <div>
                  <span>REUSABLE PRESENTER PROFILE</span>
                  <h3>{{ selectedReusableAnchorStyle?.name || '主播风格' }}</h3>
                  <p>{{ selectedReusableAnchorStyle?.description || '把不同商品话术中的“怎么说”沉淀为可复用主播。' }}</p>
                </div>
                <div class="reusable-anchor-style-main-actions" v-if="selectedReusableAnchorStyle">
                  <button type="button" class="strategy-version-button" @click="saveReusableAnchorStyleInfo">保存名称与介绍</button>
                  <button type="button" class="danger-button" @click="deleteReusableAnchorStyle">删除主播</button>
                </div>
              </div>
              <div v-if="reusableAnchorStyleError" class="inline-error">{{ reusableAnchorStyleError }}</div>
              <div v-if="reusableAnchorStyleNotice" class="notice-banner">{{ reusableAnchorStyleNotice }}</div>
              <div v-if="selectedReusableAnchorStyle" class="reusable-anchor-style-edit-row">
                <input v-model="reusableAnchorStyleDraft.name" maxlength="80" placeholder="主播名称，例如：四川老乡主播" />
                <input v-model="reusableAnchorStyleDraft.description" maxlength="500" placeholder="一句话介绍这个主播的说话感觉" />
              </div>
              <nav class="reusable-anchor-style-tabs" v-if="selectedReusableAnchorStyle">
                <button type="button" :class="{ active: reusableAnchorStyleTab === 'samples' }" @click="reusableAnchorStyleTab = 'samples'">主播风格样本话术</button>
                <button type="button" :class="{ active: reusableAnchorStyleTab === 'analysis' }" @click="reusableAnchorStyleTab = 'analysis'">风格分析结果</button>
                <button type="button" :class="{ active: reusableAnchorStyleTab === 'testing' }" @click="reusableAnchorStyleTab = 'testing'">测试与训练</button>
                <button type="button" :class="{ active: reusableAnchorStyleTab === 'personalization' }" @click="reusableAnchorStyleTab = 'personalization'">个性化挂载</button>
              </nav>

              <div v-if="selectedReusableAnchorStyle && reusableAnchorStyleTab === 'samples'" class="reusable-anchor-style-tab-panel">
                <div class="reusable-anchor-style-panel-title"><div><strong>导入这个主播不同场景的话术</strong><small>可以分别放菜籽油、衣服、闲聊等样本。系统只学习口语、称呼、口头禅和局部推进，不学习商品事实。</small></div></div>
                <div class="reusable-anchor-style-sample-compose">
                  <input v-model="reusableAnchorStyleSampleDraft.title" maxlength="120" placeholder="样本标题，例如：菜籽油主线" />
                  <textarea v-model="reusableAnchorStyleSampleDraft.raw_text" rows="7" maxlength="30000" placeholder="粘贴主播真实说过的话。多段话可以一起粘贴。"></textarea>
                  <div class="reusable-anchor-style-sample-compose-actions"><button type="button" class="primary-button" :disabled="reusableAnchorStyleBusy || !reusableAnchorStyleSampleDraft.raw_text.trim()" @click="addReusableAnchorStyleSample">保存这份样本</button><label class="strategy-version-button reusable-anchor-style-file-button">导入 TXT/MD<input type="file" accept=".txt,.md,text/plain,text/markdown" multiple @change="importReusableAnchorStyleSamples" /></label></div>
                </div>
                <div v-if="reusableAnchorStyleAnalysisStage" class="reusable-anchor-style-thinking"><span class="thinking-dot"></span><strong>{{ reusableAnchorStyleAnalysisStage }}</strong><small>正在调用风格分析与同步质检，请不要重复点击。</small></div>
                <div class="reusable-anchor-style-sample-list">
                  <article v-for="sample in reusableAnchorStyleSamples" :key="sample.id">
                    <header><div><strong>{{ sample.title }}</strong><small>{{ sample.readable_text.length }} 字 · {{ sample.analysis_status === 'analyzed' ? '已分析' : sample.analysis_status === 'analyzing' ? '分析中' : sample.analysis_status === 'failed' ? '分析失败' : '待分析' }}</small></div><div><button type="button" class="primary-button" :disabled="reusableAnchorStyleBusy || sample.analysis_status === 'analyzing'" @click="analyzeReusableAnchorStyleSample(sample)">{{ sample.analysis_status === 'analyzed' ? '重新分析' : '分析这份样本' }}</button><button type="button" class="strategy-version-button" @click="deleteReusableAnchorStyleSample(sample)">删除</button></div></header>
                    <details><summary>查看原始样本</summary><pre>{{ sample.readable_text }}</pre></details>
                    <p v-if="sample.error_message" class="inline-error">{{ sample.error_message }}</p>
                  </article>
                </div>
              </div>

              <div v-else-if="selectedReusableAnchorStyle && reusableAnchorStyleTab === 'analysis'" class="reusable-anchor-style-tab-panel">
                <div class="reusable-anchor-style-panel-title"><div><strong>风格分析结果</strong><small>下面的内容是跨商品可复用的“怎么说”，事实、价格、链接和互动策略不在这里。</small></div><span>{{ selectedReusableAnchorStyle.sample_count }} 份样本已纳入</span></div>
                <div v-if="!selectedReusableAnchorStyle.profile.dimensions?.length" class="reusable-anchor-style-empty">还没有分析结果。请回到“主播风格样本话术”分析至少一份样本。</div>
                <template v-else>
                  <section class="reusable-anchor-style-rulebook reusable-anchor-style-meta-prompt-only">
                    <header><strong>主播元提示词</strong><small>这是给生成模型使用的可执行风格规范；商品事实、价格、链接和互动策略不写入这里。</small></header>
                    <pre>{{ selectedReusableAnchorStyle.profile.delivery_spec?.rulebook || selectedReusableAnchorStyle.profile.reusable_rules.join('\n') }}</pre>
                  </section>
                </template>
              </div>

              <div v-else-if="selectedReusableAnchorStyle && reusableAnchorStyleTab === 'testing'" class="reusable-anchor-style-tab-panel">
                <div class="reusable-anchor-style-panel-title"><div><strong>测试与训练</strong><small>每次先生成一小段；不满意可写差异继续校正。测试不会发布，也不会自动备份版本或改当前直播。</small></div></div>
                <div class="strategy-anchor-test-settings reusable-anchor-style-test-settings">
                  <label>这次主线要说什么<input v-model="anchorStyleTestTopic" maxlength="300" placeholder="例如：从衣服面料和尺码开始讲一轮主线" /></label>
                  <label>目标字数<input v-model.number="anchorStyleTestTargetChars" type="number" min="100" max="3000" step="50" /></label>
                  <label>热度/扩展授权：{{ fullShowExpansionFreedom }}/100<input v-model.number="fullShowExpansionFreedom" type="range" min="0" max="100" step="5" /></label>
                </div>
                <div class="reusable-anchor-style-fact-picker"><span>可选事实（只决定说哪些事实，不改变主播风格）</span><button v-for="fact in formalFacts.slice(0, 12)" :key="fact.key" type="button" :class="{ selected: selectedFactKeys.includes(fact.key) }" @click="selectedFactKeys = selectedFactKeys.includes(fact.key) ? selectedFactKeys.filter((key) => key !== fact.key) : [...selectedFactKeys, fact.key]">{{ fact.key }}</button></div>
                <div class="reusable-anchor-style-test-actions"><button class="primary-button" type="button" :disabled="anchorStyleTestBusy || !selectedReusableAnchorStyle.profile.dimensions?.length || !currentPlan" @click="runAnchorStyleTest">{{ anchorStyleTestBusy ? '生成中…' : '生成一小段测试文案' }}</button><button type="button" class="strategy-version-button" :disabled="reusableAnchorStyleLongTestRunning" @click="runReusableLongTest">{{ reusableAnchorStyleLongTestRunning ? '长时间测试中…' : '长时间主播测试' }}</button><button v-if="reusableAnchorStyleLongTestRunning" type="button" class="strategy-version-button" @click="stopReusableLongTest">停止</button><label v-if="!reusableAnchorStyleLongTestRunning">测试时长（分钟）<input v-model.number="reusableAnchorStyleLongTestMinutes" type="number" min="5" max="240" step="5" /></label></div>
                <div v-if="anchorStyleTestError" class="inline-error">{{ anchorStyleTestError }}</div>
                <pre v-if="anchorStyleTestResult" class="reusable-anchor-style-generated-text">{{ anchorStyleTestResult.text }}</pre>
                <div v-if="anchorStyleTestResult" class="reusable-anchor-style-score-row"><span>客观风格评分：{{ anchorStyleTestResult.runtime_evaluation?.style_score ?? '—' }}</span><span>用词：{{ anchorStyleTestResult.runtime_evaluation?.lexical_score ?? '—' }}</span><span>节奏：{{ anchorStyleTestResult.runtime_evaluation?.rhythm_score ?? '—' }}</span><span>本次测试仅供校对，不自动备份版本。</span></div>
                <section v-if="anchorStyleTestResult" class="strategy-style-correction-loop reusable-anchor-style-training-loop">
                  <header><div><span>ITERATIVE CALIBRATION</span><strong>哪里不像，就继续告诉系统</strong></div><em>{{ anchorStyleCorrectionCandidates.length }} 条未保存调整</em></header>
                  <p>直接写这版和真实主播哪里不同，也可以粘贴其他 AI 的差异分析。系统只提取可复用的说话方式，重新生成一小段给你比较；满意前不会改变正式主播风格。</p>
                  <div v-if="anchorStyleCorrectionHistory.length" class="strategy-style-correction-history">
                    <article v-for="(entry, index) in anchorStyleCorrectionHistory" :key="`${index}:${entry.feedback}`">
                      <span>第 {{ index + 1 }} 轮 · {{ entry.model }}</span>
                      <p><b>你的反馈：</b>{{ entry.feedback }}</p>
                      <p><b>系统归纳：</b>{{ entry.diagnosis }}</p>
                    </article>
                  </div>
                  <label><span>这版哪里还不像</span><textarea v-model="anchorStyleCorrectionFeedback" rows="3" maxlength="300" placeholder="例如：说得太完整太顺了，原主播更喜欢短句，偶尔回头补一句。也可以粘贴其他 AI 的分析。"></textarea></label>
                  <div class="strategy-style-correction-actions">
                    <button class="primary-button" type="button" :disabled="!anchorStyleCorrectionFeedback.trim() || !anchorRulebook || anchorStyleCorrectionBusy || anchorStyleTestBusy" @click="learnAndRegenerateAnchorStyle">{{ anchorStyleCorrectionBusy ? '正在学习并重生成…' : '学习这条反馈并重新生成' }}</button>
                    <button v-if="anchorStyleCorrectionCandidates.length" class="primary-button" type="button" :disabled="anchorStyleCorrectionBusy || styleOverlayBusy" @click="saveAnchorStyleCorrections">满意，保存这些调整</button>
                    <button v-if="anchorStyleCorrectionCandidates.length" class="strategy-version-button" type="button" :disabled="anchorStyleCorrectionBusy" @click="discardAnchorStyleCorrections">放弃本轮调整</button>
                  </div>
                  <small v-if="!anchorRulebook">需要先分析真实主播样本，系统才能判断“哪里不像”。</small>
                  <div v-if="anchorStyleCorrectionError" class="inline-error">{{ anchorStyleCorrectionError }}</div>
                </section>
                <div v-if="reusableAnchorStyleLongTestText" class="reusable-anchor-style-long-test"><header><strong>长时间测试结果</strong><span>已生成 {{ reusableAnchorStyleLongTestSegments }} 段</span></header><pre>{{ reusableAnchorStyleLongTestText }}</pre></div>
              </div>

              <div v-else-if="selectedReusableAnchorStyle && reusableAnchorStyleTab === 'personalization'" class="reusable-anchor-style-tab-panel">
                <div class="reusable-anchor-style-panel-title"><div><strong>个性化挂载</strong><small>插件是主播的表达叠加能力，例如偶尔幽默、偶尔自我纠正、轻微口结。启用后参与测试生成，不改变事实库和互动策略。</small></div></div>
                <div v-if="stylePluginCatalog?.items.length" class="reusable-anchor-style-plugin-grid"><article v-for="pluginItem in stylePluginCatalog.items" :key="pluginItem.manifest.id"><div><strong>{{ pluginItem.manifest.name }}</strong><small>{{ pluginItem.manifest.description }}</small></div><button type="button" :disabled="!pluginItem.activation_supported" @click="toggleReusableAnchorPlugin(pluginItem)">{{ selectedReusableAnchorStyle.plugin_settings?.some((setting) => setting.plugin_id === pluginItem.manifest.id && setting.enabled) ? '已启用 · 点击卸载' : '启用这个插件' }}</button></article></div>
                <div v-else class="reusable-anchor-style-empty">当前方案还没有读取插件目录，请先选择一个直播方案。</div>
              </div>
            </div>
          </div>
          <details class="reusable-anchor-style-legacy"><summary>兼容旧版：当前直播方案素材分析与叠加风格</summary>
          <div class="strategy-v2-section-head">
            <div>
              <h3>主播风格</h3>
              <p>从直播素材和已采纳参考中只学习“怎么说”。商品、价格、规格、链接、产地、活动福利和履约事实不会进入主播风格规则。</p>
            </div>
            <div class="strategy-anchor-style-actions">
              <button
                class="strategy-version-button"
                type="button"
                :disabled="!currentPlan || scriptAnalyzing || anchorStyleApplying"
                @click="analyzeCurrentPlanScript"
              >
                {{ scriptAnalyzing ? '分析中…' : '重新分析当前素材' }}
              </button>
              <button
                class="primary-button"
                type="button"
                :disabled="!currentPlan || scriptAnalyzing || anchorStyleApplying || !scriptReadableText.trim() || !anchorRulebook"
                @click="applyAnchorStyleHot"
              >
                {{ anchorStyleApplying ? '保存生效中…' : '保存口播规范并生效' }}
              </button>
            </div>
          </div>

          <section class="strategy-anchor-core-panel">
            <header><strong>主流程：粘贴主播原文 → 分析 → 生成测试文案 → 反馈校正</strong><button class="strategy-version-button" @click="anchorStyleSampleExpanded = !anchorStyleSampleExpanded">{{ anchorStyleSampleExpanded ? '收起主播原文' : '展开并粘贴主播原文' }}</button></header>
            <p>这里才是主播样本入口。下面的“额外习惯”不是样本分析；它只用于你主动追加一句长期要求。</p>
            <template v-if="anchorStyleSampleExpanded">
              <label>主播真实口播原文（必填）<textarea v-model="scriptReadableText" rows="8" maxlength="60000" placeholder="请在这里粘贴一份或多份主播真实说过的话。粘贴后，下方会显示实际字数。" @input="syncSimpleScriptText"></textarea></label>
              <small>当前已输入 {{ scriptReadableText.trim().length }} 字；灰色提示文字不计入输入。</small>
              <button class="primary-button" :disabled="scriptAnalyzing" @click="analyzeCurrentPlanScript">{{ scriptAnalyzing ? '分析规则中…' : '开始分析主播样本' }}</button>
              <div v-if="settingsError" class="inline-error">{{ settingsError }}</div>
            </template>
            <small v-if="savedStyleScript && !currentAnalysis?.anchor_style?.dimensions?.length">当前显示当前方案已保存素材「{{ savedStyleScript.title || '主播素材' }}」的风格；不会因新建未分析素材而隐藏。</small>
          </section>

          <section class="strategy-style-overlay-panel">
            <header>
              <div>
                <span>PERSONA LAYERS</span>
                <strong>可选：额外习惯</strong>
                <p>这不是主播原文分析。只有想主动增加“偶尔幽默”“先说重点”等长期要求时才填写。</p>
              </div>
              <em>{{ styleOverlayProfile?.items.filter((item) => item.enabled).length || 0 }} 条生效</em>
            </header>

            <details class="strategy-style-plugin-library">
              <summary>
                <div><span>ADVANCED</span><strong>高级：主播能力插件</strong><p>一般不需要操作；只有希望直接叠加预制能力时再展开。</p></div>
                <em>{{ stylePluginCatalog?.items.length || 0 }} 个能力</em>
              </summary>
              <div v-if="stylePluginCatalog?.items.length" class="strategy-style-plugin-grid">
                <article v-for="pluginItem in stylePluginCatalog.items" :key="`${pluginItem.manifest.id}@${pluginItem.manifest.version}`" :class="{ locked: !pluginItem.activation_supported }">
                  <div class="strategy-style-plugin-title"><div><span>{{ pluginItem.manifest.mode === 'declarative' ? '声明式' : '可信执行器' }} · v{{ pluginItem.manifest.version }}</span><strong>{{ pluginItem.manifest.name }}</strong></div><code>{{ pluginItem.manifest.id }}</code></div>
                  <p>{{ pluginItem.manifest.description }}</p>
                  <small>场景：{{ pluginItem.manifest.scenes.join('、') }} · 参数 {{ Object.keys(pluginItem.manifest.parameters || {}).length }} 项</small>
                  <small v-if="pluginItem.activation_reason" class="strategy-style-plugin-reason">{{ pluginItem.activation_reason }}</small>
                  <button
                    type="button"
                    :disabled="!pluginItem.activation_supported || activeStylePluginIDs.has(pluginItem.manifest.id) || Boolean(stylePluginBusyID) || !styleOverlayProfile"
                    @click="activateStylePlugin(pluginItem)"
                  >
                    {{ activeStylePluginIDs.has(pluginItem.manifest.id) ? '已加入主播' : stylePluginBusyID === pluginItem.manifest.id ? '启用中…' : pluginItem.activation_supported ? '加入当前主播' : '执行链待接入' }}
                  </button>
                </article>
              </div>
              <div v-else class="strategy-style-overlay-empty">服务器尚未加载主播风格插件目录。</div>
            </details>

            <div v-if="styleOverlayProfile?.items.length" class="strategy-style-overlay-list">
              <article v-for="item in styleOverlayProfile.items" :key="item.id" :class="{ disabled: !item.enabled }">
                <div class="strategy-style-overlay-card-head">
                  <div>
                    <span>{{ item.plugin ? `插件 ${item.plugin.plugin_id} · ` : '' }}{{ styleOverlayApplicationLabel(item.rule.application) }} · {{ styleOverlayStrengthLabel(item.rule.strength) }}</span>
                    <strong>{{ item.rule.label }}</strong>
                  </div>
                  <div>
                    <button type="button" :disabled="styleOverlayBusy" @click="toggleStyleOverlay(item)">{{ item.enabled ? '暂停' : '启用' }}</button>
                    <button type="button" :disabled="styleOverlayBusy" @click="removeStyleOverlay(item)">删除</button>
                  </div>
                </div>
                <p>{{ item.rule.mainline_instruction }}</p>
                <small>短互动：{{ item.rule.interaction_instruction }}</small>
                <small>严肃场景：{{ item.rule.serious_instruction }}</small>
                <small v-if="item.rule.mainline_max_per_1000_chars">频率：每1000字 {{ item.rule.mainline_min_per_1000_chars || 0 }}～{{ item.rule.mainline_max_per_1000_chars }} 次；短答最多 {{ item.rule.interaction_max_occurrences || 0 }} 次</small>
                <details>
                  <summary>查看原始表达与理解依据</summary>
                  <p>原话：{{ item.source_text }}</p>
                  <p v-if="item.explanation_text">补充解释：{{ item.explanation_text }}</p>
                  <p>避免：{{ item.rule.avoid.join('、') || '无额外项' }} · 理解置信度 {{ item.rule.confidence }}/100</p>
                </details>
              </article>
            </div>
            <div v-else class="strategy-style-overlay-empty">还没有叠加风格。可以输入“喜欢时不时幽默一下”“先说重点再解释”等自然语言。</div>

            <div class="strategy-style-overlay-compose">
              <label>
                <span>希望主播增加什么习惯</span>
                <textarea v-model="styleOverlaySource" rows="2" maxlength="300" placeholder="请填写一条额外习惯，例如：偶尔幽默一下，但不要每段都讲笑话。"></textarea>
              </label>
              <label>
                <span>补充我的意思 <small>选填；系统理解不准时再填写</small></span>
                <textarea v-model="styleOverlayExplanation" rows="2" maxlength="600" placeholder="例如：一千字两三次，短回答不强求，只做自然接话或轻微自嘲。"></textarea>
              </label>
              <label class="strategy-style-overlay-strength">
                <span>叠加强度：{{ styleOverlayStrengthLabel(styleOverlayStrength) }}</span>
                <input v-model.number="styleOverlayStrength" type="range" min="10" max="90" step="5" />
              </label>
              <button class="primary-button" type="button" :disabled="!currentPlan || !styleOverlayProfile || styleOverlayBusy" @click="interpretStyleOverlay">
                {{ styleOverlayBusy ? '正在理解…' : '智能理解' }}
              </button>
            </div>

            <div v-if="styleOverlayPending" class="strategy-style-overlay-preview">
              <header><div><span>系统理解</span><strong>{{ styleOverlayPending.rule.label }}</strong></div><em>置信度 {{ styleOverlayPending.rule.confidence }}/100</em></header>
              <p><b>长主线：</b>{{ styleOverlayPending.rule.mainline_instruction }}</p>
              <p><b>短互动：</b>{{ styleOverlayPending.rule.interaction_instruction }}</p>
              <p><b>严肃场景：</b>{{ styleOverlayPending.rule.serious_instruction }}</p>
              <p v-if="styleOverlayPending.rule.mainline_max_per_1000_chars"><b>次数预算：</b>每1000字 {{ styleOverlayPending.rule.mainline_min_per_1000_chars || 0 }}～{{ styleOverlayPending.rule.mainline_max_per_1000_chars }} 次，短答最多 {{ styleOverlayPending.rule.interaction_max_occurrences || 0 }} 次。</p>
              <small>参考了 {{ styleOverlayMemoryMatches }} 条当前客户已确认的相似理解；用户补充解释始终优先。</small>
              <div><button type="button" class="strategy-version-button" @click="styleOverlayPending = null">重新描述</button><button type="button" class="primary-button" :disabled="styleOverlayBusy" @click="confirmStyleOverlay">确认加入主播</button></div>
            </div>
            <div v-if="styleOverlayError" class="inline-error">{{ styleOverlayError }}</div>
            <div v-if="styleOverlayNotice" class="notice-banner">{{ styleOverlayNotice }}</div>
            <small>叠加风格保存到当前直播方案；直播间“主播状态”只保留本场临时变化，不再维护另一套长期习惯。</small>
          </section>
          <div v-if="scriptNotice" class="notice-banner">{{ scriptNotice }}</div>
			<div v-if="anchorStyleAnalysisQC" class="notice-banner" :class="{ 'inline-error': !anchorStyleAnalysisQC.passed }">
				<strong>Qwen 同步质检：{{ anchorStyleAnalysisQC.passed ? '通过' : '未通过' }}</strong>
				<span v-if="anchorStyleAnalysisQC.available">证据覆盖 {{ anchorStyleAnalysisQC.coverage_score }}/100 · 风格纯度 {{ anchorStyleAnalysisQC.purity_score }}/100 · {{ anchorStyleAnalysisQC.summary }}</span>
				<span v-else>{{ anchorStyleAnalysisQC.error || '质检服务暂不可用；当前分析仅供查看。' }}</span>
			</div>

          <div v-if="settingsError" class="inline-error">{{ settingsError }}</div>
          <div v-if="scriptAnalyzing" class="strategy-analysis-progress" role="status" aria-live="polite">
            <div class="strategy-analysis-progress-head">
              <strong>{{ scriptAnalysisStage || '正在分析主播风格' }}</strong>
              <span>等待模型返回并校验规范</span>
            </div>
            <div class="strategy-analysis-progress-track">
              <span class="anchor-analysis-indeterminate"></span>
            </div>
          </div>

          <div v-if="!anchorStyleProfile?.dimensions?.length" class="strategy-v2-empty">
            <strong>还没有主播口播规范</strong>
            <span v-if="enabledStyleOverlayCount">已启用 {{ enabledStyleOverlayCount }} 条叠加风格，可以先直接生成测试文案；以后仍可粘贴真人样本作为底层风格。</span>
            <span v-else>粘贴主播真实口播，系统提取本人的表达习惯、核对原词并生成可执行规范；也可以先添加叠加风格，从零搭建。</span>
          </div>
          <template v-else>
            <section class="strategy-anchor-style-summary">
              <div>
                <span>分析结果统计</span>
                <strong>{{ anchorStyleProfile.summary || '已完成样本分析' }}</strong>
                <small>统计由程序依据本次样本计算。规范只约束怎么说，不复制商品事实；单篇样本不能代表主播的所有场景。</small>
              </div>
              <div class="strategy-anchor-style-metrics">
                <div><strong>{{ anchorDelivery?.sample_chars || '—' }}</strong><span>样本字数</span></div>
                <div><strong>{{ anchorDelivery?.sentence_count || '—' }}</strong><span>标点分句</span></div>
                <div><strong>{{ anchorDelivery?.literal_habits?.length ?? '—' }}</strong><span>原词习惯</span></div>
                <div><strong>{{ anchorDelivery?.instructions?.length || '—' }}</strong><span>执行规则</span></div>
              </div>
            </section>
            <details v-if="anchorRulebook" class="strategy-anchor-rulebook">
              <summary><strong>主播口播规范</strong><span>展开查看 · 可复制 · 测试、主线与互动共用</span></summary>
              <div class="strategy-anchor-rulebook-body">
                <button class="strategy-version-button" @click="copyAnchorRulebook">复制口播规范</button>
                <small role="status">{{ anchorCopyNotice }}</small>
                <pre>{{ anchorRulebook }}</pre>
              </div>
            </details>
            <div v-else class="notice-banner">这是旧版分析结果。请点击“重新分析当前素材”，生成新版可执行的主播口播规范；旧内容不会自动改写。</div>
          </template>
          <section v-if="canRunAnchorStyleTest" class="strategy-anchor-core-panel">
            <header><strong>用当前风格生成测试文案</strong><button class="primary-button" :disabled="!canRunAnchorStyleTest || anchorStyleTestBusy || scriptAnalyzing" @click="runAnchorStyleTest">{{ anchorStyleTestBusy ? '生成并自动校验中…' : '生成测试文案' }}</button></header>
            <div class="strategy-anchor-test-settings">
              <label>测试主题<input v-model="anchorStyleTestTopic" maxlength="300" placeholder="例如：欢迎新观众并介绍当前商品"></label>
              <label>目标字数<input v-model.number="anchorStyleTestTargetChars" type="number" min="100" max="3000" step="50"></label>
              <label>内容扩展授权（{{ fullShowExpansionLabel }} {{ fullShowExpansionFreedom }}/100）<input v-model.number="fullShowExpansionFreedom" type="range" min="0" max="100" step="5"></label>
            </div>
            <p>{{ anchorRulebook ? '使用样本口播规范与已启用的叠加风格共同测试；' : '本次没有真人样本，只测试已启用的叠加风格；' }}系统按时间单元逐段生成，前段长短会自动结转，临近结束按剩余字数收束。测试不发布、不生成声音，也不改变当前直播。</p>
            <div v-if="anchorStyleTestError" class="inline-error">{{ anchorStyleTestError }}</div>
            <template v-if="anchorStyleTestResult">
              <p class="strategy-anchor-test-text">{{ anchorStyleTestResult.text }}</p>
              <small>长度：实际 {{ anchorStyleTestResult.actual_chars }} 字；目标 {{ anchorStyleTestResult.target_chars }} 字，合格范围 {{ anchorStyleTestResult.min_chars }}–{{ anchorStyleTestResult.max_chars }} 字。</small>
              <small v-if="anchorStyleTestResult.generation_mode === 'time_driven_segments'">生成方式：按 {{ anchorStyleTestResult.segment_count || '多个' }} 个虚拟时间单元连续生成；只对出错小段补正，不整篇重写。</small>
              <small v-if="anchorStyleTestResult.style_coverage_warnings?.length" class="notice-banner">本次仍已生成，但样本里有 {{ anchorStyleTestResult.style_coverage_warnings.length }} 处说话习惯没有完全学到：{{ anchorStyleTestResult.style_coverage_warnings.join('；') }}。你可以先比较文案，再用“哪里还不像”补充。</small>
              <small :class="{ 'inline-error': !anchorStyleTestResult.style_check.passed }">样本规范原词软检查：{{ anchorStyleTestResult.style_check.passed ? '通过' : `有 ${anchorStyleTestResult.style_check.missing.length} 项偏差` }}；事实、数字与链接硬边界检查通过{{ anchorStyleTestResult.repair_attempted ? '（有小段经过补正）' : '' }}。风格偏差只提示，不会让整篇文案消失。</small>
              <small v-if="anchorStyleTestResult.style_purity">纯风格边界检查：{{ anchorStyleTestResult.style_purity.passed ? '通过' : `发现 ${anchorStyleTestResult.style_purity.issues.length} 项内容策略混入` }}。</small>
              <small v-if="anchorRulebook && anchorStyleTestResult.runtime_evaluation">影子评测：客观风格 {{ anchorStyleTestResult.runtime_evaluation.style_score }}/100（用词 {{ anchorStyleTestResult.runtime_evaluation.lexical_score }}、节奏 {{ anchorStyleTestResult.runtime_evaluation.rhythm_score }}），与原样本12字片段覆盖 {{ anchorStyleTestResult.runtime_evaluation.copy_containment_pct }}%，最长连续复用 {{ anchorStyleTestResult.runtime_evaluation.longest_shared_runes }} 字。当前只记录和展示，不自动放行或拦截。</small>
              <small v-else>当前是纯叠加风格测试，没有真人样本，因此不伪造样本相似度分数。</small>
              <small v-if="anchorStyleTestResult.style_vector_evaluation?.available">风格向量影子分 {{ anchorStyleTestResult.style_vector_evaluation.score }}/100（{{ anchorStyleTestResult.style_vector_evaluation.model }}）；当前只用于校准，不参与放行。</small>
              <small v-if="anchorStyleTestResult.overlay_qc?.available" :class="{ 'inline-error': !anchorStyleTestResult.overlay_qc.passed }">Qwen 同步质检：{{ anchorStyleTestResult.overlay_qc.passed ? '通过' : '发现风格偏差' }} · 执行度 {{ anchorStyleTestResult.overlay_qc.adherence_score }}/100 · 过量风险 {{ anchorStyleTestResult.overlay_qc.overuse_risk }}/100。质检只观察和提示，不会擅自重写整篇。{{ anchorStyleTestResult.overlay_qc.summary }}</small>
              <small v-else-if="enabledStyleOverlayCount" class="inline-error">Qwen 同步质检暂不可用：{{ anchorStyleTestResult.overlay_qc?.error || '未返回质检结果' }}。本次文案仅供查看，不应据此确认风格。</small>
              <section class="strategy-style-correction-loop">
                <header><div><span>ITERATIVE CALIBRATION</span><strong>哪里不像，就继续告诉系统</strong></div><em>{{ anchorStyleCorrectionCandidates.length }} 条未保存调整</em></header>
                <p>直接写你的感觉，也可以粘贴其他 AI 给出的差异分析。系统只提取可复用的说话方式，重新生成给你看；满意前不会改变正式主播。</p>
                <div v-if="anchorStyleCorrectionHistory.length" class="strategy-style-correction-history">
                  <article v-for="(entry, index) in anchorStyleCorrectionHistory" :key="`${index}:${entry.feedback}`">
                    <span>第 {{ index + 1 }} 轮 · {{ entry.model }}</span>
                    <p><b>你的反馈：</b>{{ entry.feedback }}</p>
                    <p><b>系统归纳：</b>{{ entry.diagnosis }}</p>
                  </article>
                </div>
                <label>
                  <span>这版哪里还不像</span>
                  <textarea v-model="anchorStyleCorrectionFeedback" rows="3" maxlength="300" placeholder="例如：说得太完整太顺了，原主播更喜欢短句，偶尔回头补一句。也可以粘贴其他 AI 的分析。"></textarea>
                </label>
                <div class="strategy-style-correction-actions">
                  <button class="primary-button" type="button" :disabled="!anchorStyleCorrectionFeedback.trim() || !anchorRulebook || anchorStyleCorrectionBusy || anchorStyleTestBusy" @click="learnAndRegenerateAnchorStyle">
                    {{ anchorStyleCorrectionBusy ? '正在学习并重生成…' : '学习这条反馈并重新生成' }}
                  </button>
                  <button v-if="anchorStyleCorrectionCandidates.length" class="primary-button" type="button" :disabled="anchorStyleCorrectionBusy || styleOverlayBusy" @click="saveAnchorStyleCorrections">满意，保存这些调整</button>
                  <button v-if="anchorStyleCorrectionCandidates.length" class="strategy-version-button" type="button" :disabled="anchorStyleCorrectionBusy" @click="discardAnchorStyleCorrections">放弃本轮调整</button>
                </div>
                <small v-if="!anchorRulebook">需要先粘贴并分析真实主播样本，系统才能判断“哪里不像”。</small>
                <div v-if="anchorStyleCorrectionError" class="inline-error">{{ anchorStyleCorrectionError }}</div>
              </section>
            </template>
          </section>
          </details>
        </section>

        <LiveVoiceCenter
          v-else-if="activeMode === 'voice'"
          :room-id="activeRoomId"
          :room-name="activeRoom?.name"
          :support-session="props.supportSession"
          @changed="handleLiveVoiceChanged"
        />

        <section
          v-else-if="activeMode === 'fullshow'"
          :key="'fullshow:' + String(activeRoomId || 0) + ':' + String(currentRoomPlanId || 0)"
          class="strategy-v2-workspace strategy-fullshow-workspace"
        >
          <div class="strategy-v2-section-head strategy-fullshow-head strategy-fullshow-generation-hero">
            <div class="strategy-fullshow-title-block">
              <h3>直播智能体生成</h3>
              <div class="strategy-fullshow-title-action">
                <button class="primary-button" type="button" :disabled="fullShowGenerating || !currentPlan || !contentModeReady || contentModeLoading || contentModeSaving" @click="generateFullShowPreview">
                  {{ fullShowGenerating ? 'S1 生成中…' : 'S1 生成直播智能体预览' }}
                </button>
              </div>
              <p>已有方案会优先恢复最近待发布草稿，没有草稿时恢复当前已发布版本作为编辑副本；修改后保存为新版本，不覆盖历史版本。</p>
            </div>
          </div>

          <div v-if="contentModeError" class="inline-error strategy-content-mode-inline-error">
            {{ contentModeError }} <button type="button" @click="refreshContentMode(true)">重新读取</button>
          </div>

          <section v-if="fullShowResult" class="strategy-fullshow-main-actions">
            <div class="strategy-fullshow-main-actions-copy">
              <span>版本操作</span>
              <strong>{{ activeRoom?.name || '当前直播间' }}</strong>
              <small>
                S5 只保存待发布新版本；保存成功后，再用 S6 把这个版本发布到当前直播间。
              </small>
            </div>
            <div class="strategy-fullshow-main-actions-buttons">
              <button
                type="button"
                :disabled="
                  fullShowVersionSaving ||
                  fullShowVersionPublishing
                "
                @click="saveFullShowVersion"
              >
                <span>{{ fullShowVersionSaving ? 'S5 保存中…' : 'S5 保存新版本' }}</span>
                <small>{{ fullShowSavedVersion?.lifecycle_status === 'draft' ? ('已保存 V' + fullShowSavedVersion.version_no + '，可以发布') : '只保存，不发布直播间' }}</small>
              </button>
              <button
                class="primary"
                type="button"
                :disabled="
                  fullShowVersionSaving ||
                  fullShowVersionPublishing
                "
                @click="publishFullShowVersion"
              >
                <span>
                  {{
                    fullShowVersionPublishing
                      ? 'S6 发布中…'
                      : fullShowSavedVersion?.lifecycle_status === 'published'
                        ? '已发布到直播间'
                        : 'S6 发布到直播间'
                  }}
                </span>
                <small>{{ fullShowSavedVersion?.lifecycle_status === 'draft' ? ('发布已保存的 V' + fullShowSavedVersion.version_no) : '请先完成 S5 保存' }}</small>
              </button>
            </div>
          </section>
          <div v-if="fullShowVersionError" class="inline-error strategy-fullshow-action-error" role="alert">
            {{ fullShowVersionError }}
          </div>

          <section v-if="fullShowWorkspaceLoading" class="strategy-fullshow-restore-banner is-loading">
            <div>
              <strong>正在恢复智能体工作区…</strong>
              <span>正在读取文案、生成参数、正式声音和声音时间轴</span>
            </div>
          </section>
          <section v-else-if="fullShowWorkspaceBaseVersion" class="strategy-fullshow-restore-banner">
            <div>
              <strong>
                {{ fullShowWorkspaceInherited ? '从同方案 V' : '基于 V' }}{{ fullShowWorkspaceBaseVersion.version_no }} 恢复编辑
              </strong>
              <span>
                {{
                  fullShowWorkspaceInherited
                    ? '恢复的是该智能体方案在其它绑定直播间已有的版本副本'
                    : fullShowWorkspaceSource === 'draft'
                    ? '恢复的是待发布草稿'
                    : '恢复的是当前已发布版本副本'
                }}
                · 文案、时长、正式稿、声音和时间轴已恢复
              </span>
              <span v-if="fullShowWorkspaceInherited" class="strategy-fullshow-restore-legacy">
                当前是继承编辑副本；请先点击 S5 保存为当前直播间的新版本，再点击 S6 发布到这个直播间。
              </span>
              <span v-if="fullShowWorkspaceLegacyPartial" class="strategy-fullshow-restore-legacy">
                这是旧版历史数据：当时只保存了正式稿，未入选草稿没有历史快照；本次之后的新版本会保存全部稿件。
              </span>
            </div>
            <div class="strategy-fullshow-restore-status">
              <b v-if="fullShowWorkspaceDirty" class="dirty">有未保存修改</b>
              <b v-else>{{ fullShowWorkspaceSource === 'draft' ? '可直接继续编辑或发布' : '当前未修改' }}</b>
              <em>这里的修改不会直接影响当前直播</em>
            </div>
          </section>

          <details v-if="!isUserAudioContentMode" class="strategy-fullshow-guide" open>
            <summary class="strategy-fullshow-guide-title">
              <div>
                <strong>生成指导步骤</strong>
                <span>{{ fullShowGuideNextAction }}</span>
              </div>
              <em>当前第 {{ fullShowGuideCurrentStep }} 步 / 5</em>
            </summary>
            <div class="strategy-fullshow-guide-steps">
              <template v-for="step in fullShowGuideSteps" :key="step.index">
                <article :class="['is-' + step.status]">
                  <i>{{ step.index }}</i>
                  <div>
                    <strong>{{ step.title }}</strong>
                    <span>{{ step.description }}</span>
                  </div>
                  <b v-if="step.status === 'done'">完成</b>
                  <b v-else-if="step.status === 'active'">当前</b>
                  <b v-else>待进行</b>
                </article>
                <span v-if="step.index < fullShowGuideSteps.length" class="strategy-fullshow-guide-arrow">→</span>
              </template>
            </div>
          </details>

          <section
            v-if="fullShowGenerationProgress.visible"
            class="strategy-fullshow-progress"
            :class="{ failed: fullShowGenerationProgress.failed, done: fullShowGenerationProgress.percent >= 100 && !fullShowGenerationProgress.failed }"
          >
            <div class="strategy-fullshow-progress-head">
              <div>
                <strong>{{ fullShowGenerationProgress.mode }}生成进度</strong>
                <span>{{ fullShowGenerationProgress.stage }}</span>
              </div>
              <em>{{ fullShowGenerationProgress.percent }}%</em>
            </div>
            <div
              class="strategy-fullshow-progress-track"
              role="progressbar"
              :aria-valuenow="fullShowGenerationProgress.percent"
              aria-valuemin="0"
              aria-valuemax="100"
            >
              <i :style="{ width: fullShowGenerationProgress.percent + '%' }"></i>
            </div>
            <div class="strategy-fullshow-progress-foot">
              <span>{{ fullShowGenerationProgress.detail }}</span>
              <small v-if="fullShowGenerating">预计进度 · 后端当前为一次性生成接口</small>
              <small v-else-if="fullShowGenerationProgress.percent >= 100">已完成</small>
            </div>
          </section>

          <div v-if="fullShowNotice" class="strategy-script-notice">{{ fullShowNotice }}</div>
          <div v-if="fullShowError" class="inline-error strategy-inline-error">{{ fullShowError }}</div>

          <div v-if="!isUserAudioContentMode" class="strategy-fullshow-grid">
            <section class="strategy-fullshow-panel strategy-fullshow-config">
              <header>
                <div>
                  <span>01</span>
                  <strong>整场参数</strong>
                </div>
                <div class="strategy-fullshow-panel-head-actions">
                  <button
                    class="strategy-fullshow-structure-button"
                    type="button"
                    @click="previewFullShowPlan"
                  >
                    S2 生成轮次安排
                  </button>
                </div>
              </header>

              <div class="strategy-fullshow-field">
                <div class="strategy-fullshow-field-title">
                  <span>直播时长</span>
                  <strong>{{ fullShowDurationLabel }}</strong>
                </div>
                <div class="strategy-fullshow-duration-slider">
                  <input
                    v-model.number="fullShowDuration"
                    type="range"
                    min="30"
                    max="120"
                    step="30"
                    :style="{ '--duration-progress': fullShowDurationProgress + '%' }"
                  />
                  <div class="strategy-fullshow-duration-ticks">
                    <span>30分钟</span>
                    <span>60分钟</span>
                    <span>90分钟</span>
                    <span>120分钟</span>
                  </div>
                </div>
              </div>

              <div class="strategy-fullshow-field">
                <div class="strategy-fullshow-field-title">
                  <span>内容扩展授权</span>
                  <strong>{{ fullShowExpansionLabel }} · {{ fullShowExpansionFreedom }}/100</strong>
                </div>
                <div class="strategy-fullshow-duration-slider">
                  <input
                    v-model.number="fullShowExpansionFreedom"
                    type="range"
                    min="0"
                    max="100"
                    step="5"
                    :style="{ '--duration-progress': fullShowExpansionFreedom + '%' }"
                  />
                  <div class="strategy-fullshow-duration-ticks">
                    <span>保守</span>
                    <span>平衡</span>
                    <span>开放</span>
                    <span>边缘合规</span>
                  </div>
                </div>
                <small>法律、平台规则和你明确锁定的内容始终不动；数值越高，越允许场景、类比、故事化、常识推演和更强促单。触碰审核边缘时优先换成合规说法，不直接删掉。</small>
              </div>

              <div class="strategy-fullshow-inline-fields">
                <label>
                  <span>单轮目标时长</span>
                  <div><input v-model.number="fullShowRoundMinutes" type="number" min="5" max="12" /><em>分钟</em></div>
                </label>
                <label>
                  <span>首批变化稿</span>
                  <select v-model.number="fullShowVariantCount">
                    <option :value="3">3 套</option>
                    <option :value="4">4 套</option>
                    <option :value="5">5 套</option>
                  </select>
                </label>
              </div>

              <div class="strategy-fullshow-options">
                <label><input v-model="fullShowUseAnchorStyle" type="checkbox" /><span><strong>使用主播风格</strong><small>按当前主播画像控制措辞、转场和情绪曲线</small></span></label>
                <label><input v-model="fullShowUseDynamicFacts" type="checkbox" /><span><strong>保留动态事实插槽</strong><small>库存、在线状态等运行时读取，不写死在整场稿里</small></span></label>
                <label><input v-model="fullShowGenerateTTS" type="checkbox" /><span><strong>同步生成声音指令</strong><small>为开场、讲品、算账、逼单分别生成语气和语速建议</small></span></label>
                <label><input v-model="fullShowAvoidRecent" type="checkbox" /><span><strong>避免近期重复</strong><small>生成时避开最近主线的开场、结构和高重复句式</small></span></label>
              </div>
            </section>

            <section class="strategy-fullshow-panel strategy-fullshow-sources">
              <header>
                <div>
                  <span>02</span>
                  <strong>本次生成依据</strong>
                </div>
                <small>{{ currentPlan?.name || '当前直播间还没有绑定直播方案' }}</small>
              </header>

              <div class="strategy-fullshow-source-cards">
                <button type="button" @click="activeMode = 'knowledge'">
                  <span>正式事实</span><strong>{{ fullShowSourceSummary.facts }}</strong><small>只使用已明确采纳的事实</small>
                </button>
                <button type="button" @click="activeMode = 'products'">
                  <span>商品链接</span><strong>{{ fullShowSourceSummary.products }}</strong><small>链接与商品信息不允许串号</small>
                </button>
                <button type="button" @click="activeMode = 'benefits'">
                  <span>活动福利</span><strong>{{ fullShowSourceSummary.benefits }}</strong><small>只统计已经正式生效的活动</small>
                </button>
                <button type="button" @click="activeMode = 'anchor'">
                  <span>主播风格</span><strong>{{ fullShowSourceSummary.style }}</strong><small>控制怎么说，不携带商品事实</small>
                </button>
              </div>

              <div class="strategy-fullshow-guardrails">
                <div><span>固定不变</span><strong>正式事实 · 链接对应 · 当前有效活动条件</strong></div>
                <div><span>每轮可变</span><strong>开场角度 · 讲解顺序 · 例子 · 转场 · CTA 位置</strong></div>
                <div><span>运行时插入</span><strong>库存 · 弹幕答疑 · 欢迎 · 临场纠正</strong></div>
              </div>
            </section>
          </div>

          <details v-if="!isUserAudioContentMode" class="strategy-fullshow-panel strategy-fullshow-timeline">
            <summary>
              <div>
                <span>03</span>
                <strong>整场轮次安排</strong>
              </div>
              <small>{{ fullShowDuration }} 分钟 · 约 {{ fullShowRoundCount }} 轮 · 每轮约 {{ fullShowRoundMinutes }} 分钟</small>
            </summary>

            <div v-if="!fullShowPreviewReady" class="strategy-fullshow-empty">
              <strong>先整理整场结构</strong>
              <span>系统会按直播时长拆分轮次，并把 A/B/C/D/E 不同表达稿交叉安排，避免整场反复念同一篇。</span>
            </div>
            <div v-else class="strategy-fullshow-rounds">
              <article v-for="round in fullShowRoundPreview" :key="round.index">
                <div class="strategy-fullshow-round-index">{{ String(round.index).padStart(2, '0') }}</div>
                <div>
                  <span>{{ round.variant }}</span>
                  <strong>{{ round.opening }}</strong>
                  <small>{{ round.startMinute }}–{{ Math.min(fullShowDuration, round.startMinute + fullShowRoundMinutes) }} 分钟</small>
                </div>
                <div class="strategy-fullshow-round-flow">
                  <span>留人</span><i>→</i><span>讲品</span><i>→</i><span>价值</span><i>→</i><span>链接</span><i>→</i><span>CTA</span>
                </div>
                <em>{{ fullShowGenerateTTS ? '含声音语气' : '纯文字' }}</em>
              </article>
              <div v-if="fullShowRoundCount > fullShowRoundPreview.length" class="strategy-fullshow-more-rounds">
                还有 {{ fullShowRoundCount - fullShowRoundPreview.length }} 轮将按同样规则继续变化生成
              </div>
            </div>
          </details>

          <div class="strategy-fullshow-grid strategy-fullshow-output-grid">
            <section class="strategy-fullshow-panel strategy-fullshow-output">
              <header>
                <div class="strategy-fullshow-output-title">
                  <span>S3</span>
                  <strong class="strategy-fullshow-output-section-name">话术处理区</strong>
                  <button
                    type="button"
                    class="strategy-fullshow-output-mode"
                    :class="{ active: fullShowOutputMode === 'generated' }"
                    @click="selectFullShowOutputMode('generated')"
                  >
                    AI话术
                  </button>
                  <button
                    type="button"
                    class="strategy-fullshow-output-mode"
                    :class="{ active: fullShowOutputMode === 'custom' }"
                    @click="openCustomMainline"
                  >
                    原始音
                  </button>
                </div>
                <div
                  class="strategy-fullshow-status"
                  :class="{ ready: fullShowOutputMode === 'custom' ? Boolean(customMainlineDraft) : Boolean(fullShowResult) }"
                >
                  <i></i>
                  {{
                    fullShowOutputMode === 'custom'
                      ? (customMainlineDraft ? '已准备' : '等待上传')
                      : (fullShowResult ? '已生成预览' : '等待生成')
                  }}
                </div>
              </header>
              <section v-if="fullShowOutputMode === 'custom'" class="strategy-custom-mainline-inline">
                <div class="strategy-custom-mainline-upload">
                  <label>
                    <input
                      type="file"
                      accept=".wav,.mp3,.m4a,.aac,.flac,.ogg,.webm,audio/*"
                      @change="handleCustomMainlineFile"
                    />
                    <strong>{{ customMainlineFile ? customMainlineFile.name : (customMainlineDraft ? '重新上传原始声音' : '选择原始声音上传') }}</strong>
                    <span>单文件最大 200MB · WAV / MP3 / M4A / AAC / FLAC / OGG / WEBM</span>
                    <span>上传后自动转换为直播播放格式（24kHz · 单声道 · 16bit PCM），无需手工转换。</span>
                  </label>
                  <div v-if="customMainlineFile" class="strategy-custom-mainline-file-meta">
                    <span>{{ (customMainlineFile.size / 1024 / 1024).toFixed(2) }} MB</span>
                    <span v-if="customMainlineDurationMS">{{ formatFullShowTimelineMS(customMainlineDurationMS) }}</span>
                    <button
                      type="button"
                      :disabled="customMainlineStage === 'uploading' || customMainlineStage === 'analyzing'"
                      @click="uploadCustomMainline"
                    >
                      {{
                        customMainlineStage === 'uploading'
                          ? '上传中…'
                          : customMainlineStage === 'analyzing'
                            ? '识别与建模中…'
                            : '上传并自动处理'
                      }}
                    </button>
                  </div>
                  <div
                    v-if="customMainlineStage === 'uploading' || customMainlineStage === 'analyzing'"
                    class="strategy-custom-mainline-progress"
                  >
                    <div><i :style="{ width: customMainlineUploadProgress + '%' }"></i></div>
                    <span>
                      {{
                        customMainlineStage === 'uploading'
                          ? ('网页直传 ' + customMainlineUploadProgress + '%')
                          : '上传完成，正在识别文字、生成 SRT 并自动生成自定义音色…'
                      }}
                    </span>
                  </div>
                </div>

                <div v-if="customMainlineError" class="inline-error strategy-custom-mainline-error">
                  {{ customMainlineError }}
                </div>

                <template v-if="customMainlineDraft">
                  <div class="strategy-custom-mainline-summary">
                    <div>
                      <strong>原始声音</strong>
                      <span>{{ formatFullShowTimelineMS(customMainlineDraft.duration_ms) }}</span>
                    </div>
                    <div :class="{ ready: Boolean(customMainlineDraft.voice_identity?.voice_id) }">
                      <strong>原始声音身份</strong>
                      <span v-if="customMainlineDraft.voice_identity">
                        {{ customMainlineDraft.voice_identity.name }} · 已自动生成 · 回答/抢答跟随此音色
                      </span>
                      <span v-else>尚未生成</span>
                      <button
                        v-if="!customMainlineDraft.voice_identity"
                        type="button"
                        :disabled="customMainlineCloneBusy"
                        @click="retryCustomMainlineClone"
                      >
                        {{ customMainlineCloneBusy ? '生成中…' : '重新生成自定义音色' }}
                      </button>
                    </div>
                  </div>

                  <audio controls preload="metadata" :src="liveSupportMediaPlaybackURL(customMainlineDraft.audio_url)"></audio>

                  <section class="strategy-custom-mainline-segments">
                    <header>
                      <div>
                        <strong>原始话术</strong>
                        <span>这里始终显示上传原音识别出的完整话术；可直接编辑，保存后同步重建原始 SRT。</span>
                      </div>
                      <button
                        type="button"
                        :disabled="customMainlineStage === 'saving' || !customMainlineTranscript.trim()"
                        @click="rebuildCustomMainline"
                      >
                        {{ customMainlineStage === 'saving' ? '正在保存…' : '保存原始话术' }}
                      </button>
                    </header>
                    <textarea class="strategy-original-transcript-editor" v-model="customMainlineTranscript" rows="16" spellcheck="false"></textarea>
                    <p v-if="customMainlineSaveNotice" class="strategy-script-notice">{{ customMainlineSaveNotice }}</p>
                  </section>

                  <details class="strategy-custom-mainline-srt">
                    <summary>查看 SRT</summary>
                    <pre>{{ customMainlineDraft.srt }}</pre>
                  </details>

                  <footer class="strategy-custom-mainline-footer">
                    <div>
                      <span v-if="customMainlineCanApply">✓ 原始声音、原始话术与 SRT 已保留</span>
                      <span v-else>请先完成原始声音处理。</span>
                    </div>
                    <button
                      type="button"
                      :disabled="!customMainlineCanApply"
                      @click="applyCustomMainlineAsFormal"
                    >
                      使用原始音
                    </button>
                    <button
                      class="primary"
                      type="button"
                      :disabled="!customMainlineCanApply || Boolean(originalAiCandidateKey && fullShowVoiceState(originalAiCandidateKey).status === 'generating')"
                      @click="generateOriginalAiVoice"
                    >
                      {{ originalAiCandidateKey && fullShowVoiceState(originalAiCandidateKey).status === 'generating' ? 'AI 声音生成中…' : 'AI 生成声音' }}
                    </button>
                  </footer>

                  <div
                    v-if="originalAiCandidateKey && fullShowVoiceState(originalAiCandidateKey).status === 'generating'"
                    class="strategy-fullshow-voice-progress strategy-original-ai-progress"
                  >
                    <div>
                      <span>正在使用当前选中的声音生成 AI 版本</span>
                      <strong>{{ fullShowVoiceState(originalAiCandidateKey).progress }}%</strong>
                    </div>
                    <div class="strategy-fullshow-voice-progress-track">
                      <i :style="{ width: fullShowVoiceState(originalAiCandidateKey).progress + '%' }"></i>
                    </div>
                    <small>原始声音和原始话术不会删除。</small>
                  </div>

                  <div
                    v-if="originalAiCandidateKey && fullShowVoiceState(originalAiCandidateKey).candidate_url"
                    class="strategy-fullshow-voice-candidate strategy-original-ai-candidate"
                  >
                    <div>
                      <strong>AI 候选声音</strong>
                      <span>先试听，采用后系统会按这段声音的真实时间轴生成对应 SRT；原始声音仍会保留。</span>
                    </div>
                    <audio controls preload="metadata" :src="liveSupportMediaPlaybackURL(fullShowVoiceState(originalAiCandidateKey).candidate_url)"></audio>
                    <footer>
                      <button type="button" :disabled="originalAiSrtProgress.status === 'generating'" @click="discardOriginalAiVoice">放弃本次生成</button>
                      <button class="primary" type="button" :disabled="originalAiSrtProgress.status === 'generating'" @click="acceptOriginalAiVoice">
                        {{ originalAiSrtProgress.status === 'generating' ? '正在生成 SRT…' : '采用 AI 声音' }}
                      </button>
                    </footer>
                  </div>

                  <section
                    v-if="originalAiSrtProgress.status !== 'idle'"
                    class="strategy-original-srt-progress"
                    :class="originalAiSrtProgress.status"
                    aria-live="polite"
                  >
                    <div>
                      <strong>{{ originalAiSrtProgress.stage }}</strong>
                      <span>{{ originalAiSrtProgress.percent }}%</span>
                    </div>
                    <div class="strategy-original-srt-progress-track">
                      <i :style="{ width: originalAiSrtProgress.percent + '%' }"></i>
                    </div>
                    <p v-if="originalAiSrtProgress.error">{{ originalAiSrtProgress.error }}</p>
                    <details
                      v-if="originalAiSrtProgress.status === 'done' && originalAiCandidateKey && fullShowVoiceState(originalAiCandidateKey).srt"
                      class="strategy-original-srt-result"
                    >
                      <summary>打开 AI 声音对应的 SRT</summary>
                      <pre>{{ fullShowVoiceState(originalAiCandidateKey).srt }}</pre>
                    </details>
                  </section>
                </template>
              </section>
              <div v-else-if="!fullShowResult" class="strategy-fullshow-output-empty">
                <strong>完整话术会在这里按轮次展示</strong>
                <span>每轮都是完整可播口语，不是关键词提纲；A/B/C/D/E 保持同一事实，但开场、句式、顺序、例子和转场会变化。</span>
              </div>
              <template v-else>
                <div class="strategy-fullshow-formal-summary" :class="{ ready: fullShowFormalSelectedCount > 0 }">
                  <div>
                    <strong>正式稿选择</strong>
                    <span v-if="fullShowFormalSelectedCount">
                      已选 {{ fullShowFormalSelectedCount }} 套：
                      {{ fullShowFormalVariantKeys.map((key) => key + '稿').join('、') }}
                    </span>
                    <span v-else>请从复核通过的 A/B/C/D/E 稿中选择至少一套，后续只为正式稿生成声音。</span>
                  </div>
                  <em>{{ fullShowFormalSelectedCount ? '第3步已完成' : '等待选择' }}</em>
                </div>
                <div class="strategy-fullshow-variant-tabs">
                  <button
                    v-for="variant in aiFullShowVariants"
                    :key="variant.variant_key"
                    type="button"
                    :class="{
                      active: selectedFullShowVariant?.variant_key === variant.variant_key,
                      failed: !variant.audit?.passed,
                      formal: isFullShowFormalVariant(variant.variant_key),
                    }"
                    @click="selectedFullShowVariantKey = variant.variant_key"
                  >
                    <strong>{{ variant.variant_key }}稿</strong>
                    <b
                      v-if="selectedFullShowVariant?.variant_key === variant.variant_key"
                      class="strategy-fullshow-current-badge"
                    >
                      当前查看
                    </b>
                    <span>{{ variant.opening_angle || variant.title }}</span>
                    <em>
                      {{ isFullShowFormalVariant(variant.variant_key) ? '正式稿 · ' : '' }}
                      {{ fullShowManualEditedKeys.includes(variant.variant_key) ? '已修改 · ' : '' }}
                      {{ variant.audit?.passed ? '复核通过' : '需检查' }}
                    </em>
                  </button>
                </div>

                <article v-if="selectedFullShowVariant" class="strategy-fullshow-script-card">
                  <header>
                    <div>
                      <span>{{ selectedFullShowVariant.variant_key }}稿 · {{ selectedFullShowVariant.opening_angle }}</span>
                      <strong>{{ selectedFullShowVariant.title }}</strong>
                    </div>
                    <div class="strategy-fullshow-script-side">
                      <div class="strategy-fullshow-script-metrics">
                        <span>约 {{ selectedFullShowVariant.estimated_minutes }} 分钟</span>
                        <span>事实覆盖 {{ selectedFullShowVariant.audit.fact_coverage_pct }}%</span>
                        <span>链接覆盖 {{ selectedFullShowVariant.audit.link_coverage_pct }}%</span>
                        <span>相似度 {{ selectedFullShowVariant.audit.similarity_pct }}%</span>
                      </div>
                      <div class="strategy-fullshow-script-actions">
                        <button
                          v-if="!isUserAudioContentMode && !fullShowEditing"
                          type="button"
                          :disabled="Boolean(fullShowRegeneratingVariantKey)"
                          @click="regenerateFullShowVariant(selectedFullShowVariant)"
                        >
                          {{
                            fullShowRegeneratingVariantKey === selectedFullShowVariant.variant_key
                              ? '正在重新生成…'
                              : '重新生成话术'
                          }}
                        </button>
                        <button v-if="!isUserAudioContentMode && !fullShowEditing" type="button" @click="startFullShowVariantEdit">修改话术</button>
                        <button
                          v-if="!fullShowEditing"
                          class="formal"
                          type="button"
                          :class="{ selected: isFullShowFormalVariant(selectedFullShowVariant.variant_key) }"
                          :disabled="!selectedFullShowVariant.audit?.passed"
                          :title="selectedFullShowVariant.audit?.passed ? '决定这套话术是否进入声音生成' : '请先修复并复核通过'"
                          @click="toggleFullShowFormalVariant(selectedFullShowVariant)"
                        >
                          {{ isFullShowFormalVariant(selectedFullShowVariant.variant_key) ? '取消正式话术' : '选为正式话术' }}
                        </button>
                        <template v-if="!isUserAudioContentMode && fullShowEditing">
                          <button type="button" :disabled="fullShowEditSaving" @click="cancelFullShowVariantEdit">取消</button>
                          <button class="primary" type="button" :disabled="fullShowEditSaving" @click="saveFullShowVariantEdit">
                            {{ fullShowEditSaving ? '复核中…' : '保存修改并复核' }}
                          </button>
                        </template>
                      </div>
                    </div>
                  </header>

                  <div
                    class="strategy-fullshow-audit"
                    :class="{ passed: selectedFullShowVariant.audit.passed }"
                  >
                    <strong>{{ selectedFullShowVariant.audit.passed ? '自动复核通过' : '自动复核发现问题' }}</strong>
                    <span v-if="!selectedFullShowVariant.audit.issues?.length">未发现新增数字、串链接、写死库存或高相似问题。</span>
                    <ul v-else>
                      <li v-for="(issue, index) in selectedFullShowVariant.audit.issues" :key="issue.code + ':' + index" :class="'is-' + issue.severity">
                        {{ issue.message }}
                      </li>
                    </ul>
                  </div>

                  <div v-if="!isUserAudioContentMode && fullShowEditing" class="strategy-fullshow-script-editor">
                    <div class="strategy-fullshow-script-editor-tip">
                      <strong>正在修改 {{ selectedFullShowVariant.variant_key }}稿</strong>
                      <span>这里只改当前稿；选中文字后可右键“添加为事实”。保存后系统会立即重新检查价格、规格、链接、库存数字、篇幅和与其它稿件的相似度。</span>
                    </div>
                    <textarea
                      v-model="fullShowEditingText"
                      rows="22"
                      spellcheck="false"
                      @contextmenu="handleFullShowEditorContextMenu"
                    ></textarea>
                    <div v-if="fullShowEditError" class="inline-error">{{ fullShowEditError }}</div>
                  </div>
                  <div v-else class="strategy-fullshow-script-text">{{ selectedFullShowVariant.text }}</div>
                  <div
                    v-if="fullShowRegenerateError && selectedFullShowVariant.variant_key === selectedFullShowVariantKey"
                    class="inline-error"
                  >
                    {{ fullShowRegenerateError }}
                  </div>

                  <Teleport to="body">
                    <div
                      v-if="fullShowFactContextMenu.visible"
                      class="strategy-fullshow-fact-menu-layer"
                      @mousedown.self="closeFullShowFactContextMenu"
                      @contextmenu.prevent
                    >
                      <div
                        class="strategy-fullshow-fact-menu"
                        :style="{ left: fullShowFactContextMenu.x + 'px', top: fullShowFactContextMenu.y + 'px' }"
                        @mousedown.stop
                      >
                        <button type="button" @click="openFullShowFactDialog">
                          <span>＋</span>
                          <strong>添加为事实</strong>
                        </button>
                        <small>{{ fullShowFactContextMenu.text }}</small>
                      </div>
                    </div>

                    <div
                      v-if="fullShowFactDialogOpen"
                      class="strategy-fullshow-fact-dialog-overlay"
                      @mousedown.self="closeFullShowFactDialog"
                    >
                      <section class="strategy-fullshow-fact-dialog" role="dialog" aria-modal="true" aria-label="添加正式事实">
                        <header>
                          <div>
                            <span>从主线稿添加</span>
                            <strong>添加正式事实</strong>
                          </div>
                          <button type="button" :disabled="fullShowFactSaving" @click="closeFullShowFactDialog">×</button>
                        </header>
                        <div class="strategy-fullshow-fact-form">
                          <label>
                            <span>事实分类</span>
                            <select v-model="fullShowFactDraft.category">
                              <option v-for="option in formalFactCategoryOptions" :key="option.value" :value="option.value">
                                {{ option.label }}
                              </option>
                            </select>
                          </label>
                          <label>
                            <span>事实名称</span>
                            <input v-model="fullShowFactDraft.key" maxlength="255" />
                          </label>
                          <label class="wide">
                            <span>事实内容</span>
                            <textarea v-model="fullShowFactDraft.value" rows="5" maxlength="4000"></textarea>
                          </label>
                        </div>
                        <div v-if="fullShowFactError" class="inline-error">{{ fullShowFactError }}</div>
                        <footer>
                          <button type="button" :disabled="fullShowFactSaving" @click="closeFullShowFactDialog">取消</button>
                          <button class="primary" type="button" :disabled="fullShowFactSaving" @click="saveSelectedTextAsFormalFact">
                            {{ fullShowFactSaving ? '添加中…' : '添加事实' }}
                          </button>
                        </footer>
                      </section>
                    </div>
                  </Teleport>

                  <section
                    v-if="isFullShowFormalVariant(selectedFullShowVariant.variant_key)"
                    class="strategy-fullshow-voice"
                  >
                    <header>
                      <div>
                          <strong>S4 {{ isUserAudioContentMode ? '原始声音' : '生成 / 试听声音' }}</strong>
                        <span v-if="isUserAudioContentMode">主线保留上传原声 · 互动音色：{{ currentVoiceIdentity.name }} {{ currentVoiceIdentity.version }}</span>
                        <span v-else>声音身份：{{ currentVoiceIdentity.name }} {{ currentVoiceIdentity.version }} · {{ currentFullShowVoiceName }}</span>
                      </div>
                      <button
                        type="button"
                        class="strategy-fullshow-voice-set-preview"
                        :class="{ active: fullShowSetListening }"
                        @click="previewFormalVoiceSet"
                      >
                        {{
                          fullShowSetListening
                            ? ('停止试听 · ' + (fullShowSetListeningKey ? fullShowSetListeningKey + '稿' : '准备中'))
                            : '整套试听'
                        }}
                      </button>
                      <em>
                        {{
                          fullShowVoiceState(selectedFullShowVariant.variant_key).status === 'generating'
                            ? '生成中'
                            : fullShowVoiceState(selectedFullShowVariant.variant_key).status === 'candidate'
                              ? '有新声音待选择'
                              : fullShowVoiceState(selectedFullShowVariant.variant_key).audio_url
                                ? '已生成'
                                : '未生成'
                        }}
                      </em>
                    </header>

                    <div
                      v-if="fullShowVoiceState(selectedFullShowVariant.variant_key).status === 'generating'"
                      class="strategy-fullshow-voice-progress"
                    >
                      <div>
                        <span>正在生成 {{ selectedFullShowVariant.variant_key }}稿声音</span>
                        <strong>{{ fullShowVoiceState(selectedFullShowVariant.variant_key).progress }}%</strong>
                      </div>
                      <div class="strategy-fullshow-voice-progress-track">
                        <i :style="{ width: fullShowVoiceState(selectedFullShowVariant.variant_key).progress + '%' }"></i>
                      </div>
                      <small>每一稿单独生成；这一稿不满意，只重新生成这一稿。</small>
                    </div>

                    <div
                      v-else-if="!fullShowVoiceState(selectedFullShowVariant.variant_key).audio_url"
                      class="strategy-fullshow-voice-empty"
                    >
                      <div>
                        <strong>{{ isUserAudioContentMode ? '还没有可用的原始录音' : '还没有生成这篇稿子的声音' }}</strong>
                        <span v-if="isUserAudioContentMode">上传原始录音并设为正式主线，不会通过 AI 替换录音声音。</span>
                        <span v-else>点击后只生成 {{ selectedFullShowVariant.variant_key }}稿，不会批量生成其它正式稿。</span>
                      </div>
                      <button v-if="isUserAudioContentMode" type="button" @click="openCustomMainline">上传原始录音</button>
                      <button
                        v-else-if="String(currentRoomSelectedVoice.source || '')"
                        type="button"
                        @click="generateFullShowVariantVoice(selectedFullShowVariant)"
                      >
                        生成声音
                      </button>
                      <button v-else type="button" @click="activeMode = 'voice'">先选择声音</button>
                    </div>

                    <div
                      v-if="fullShowVoiceState(selectedFullShowVariant.variant_key).audio_url"
                      class="strategy-fullshow-voice-version"
                    >
                      <div class="strategy-fullshow-voice-version-head">
                        <div>
                          <strong>当前声音 V{{ Math.max(1, fullShowVoiceState(selectedFullShowVariant.variant_key).generation_no) }}</strong>
                          <span>当前页面试听版本 · 正式发布前不会发送给 Core</span>
                        </div>
                        <button
                          v-if="!isUserAudioContentMode"
                          type="button"
                          :disabled="fullShowVoiceState(selectedFullShowVariant.variant_key).status === 'generating'"
                          @click="generateFullShowVariantVoice(selectedFullShowVariant, true)"
                        >
                          重新生成
                        </button>
                      </div>
                      <audio
                        controls
                        preload="metadata"
                        :src="liveSupportMediaPlaybackURL(fullShowVoiceState(selectedFullShowVariant.variant_key).audio_url)"
                        @timeupdate="updateFullShowVoicePlayback(selectedFullShowVariant.variant_key, $event)"
                        @seeked="updateFullShowVoicePlayback(selectedFullShowVariant.variant_key, $event)"
                      ></audio>
                      <div class="strategy-fullshow-asset-bundle">
                        <div>
                          <strong>声音资产</strong>
                          <span>WAV ✓</span>
                          <span :class="{ ok: Boolean(fullShowVoiceState(selectedFullShowVariant.variant_key).srt) }">SRT {{ fullShowVoiceState(selectedFullShowVariant.variant_key).srt ? '✓' : '—' }}</span>
                          <span :class="{ ok: fullShowVoiceState(selectedFullShowVariant.variant_key).timeline.length > 0 }">时间轴 {{ fullShowVoiceState(selectedFullShowVariant.variant_key).timeline.length ? '✓' : '—' }}</span>
                        </div>
                        <div>
                          <button
                            type="button"
                            :disabled="!fullShowVoiceState(selectedFullShowVariant.variant_key).srt"
                            @click="toggleFullShowVoiceDetail(selectedFullShowVariant.variant_key, 'srt')"
                          >
                            {{ fullShowVoiceDetail.key === selectedFullShowVariant.variant_key && fullShowVoiceDetail.mode === 'srt' ? '收起 SRT' : '查看 / 修改 SRT' }}
                          </button>
                        </div>
                      </div>
                      <div
                        v-if="fullShowVoiceDetail.key === selectedFullShowVariant.variant_key && fullShowVoiceDetail.mode === 'srt'"
                        class="strategy-fullshow-srt-view"
                      >
                        <header>
                          <div>
                            <strong>SRT 字幕</strong>
                            <span>时间位置与当前声音保持不变，只修改文字。</span>
                          </div>
                          <button
                            type="button"
                            :disabled="fullShowSubtitleSavingKey === selectedFullShowVariant.variant_key"
                            @click="saveFullShowSubtitleText(selectedFullShowVariant.variant_key)"
                          >
                            {{ fullShowSubtitleSavingKey === selectedFullShowVariant.variant_key ? '保存中…' : '保存修改' }}
                          </button>
                        </header>
                        <div class="strategy-fullshow-srt-edit-list">
                          <article
                            v-for="segment in fullShowSubtitleDrafts[selectedFullShowVariant.variant_key] || []"
                            :key="segment.segment_id"
                          >
                            <div>
                              <b>{{ segment.index }}</b>
                              <time>
                                {{ formatFullShowTimelineMS(segment.start_ms) }}–{{ formatFullShowTimelineMS(segment.end_ms) }}
                              </time>
                            </div>
                            <textarea v-model="segment.text" rows="2"></textarea>
                          </article>
                        </div>
                        <div v-if="fullShowSubtitleError" class="inline-error">{{ fullShowSubtitleError }}</div>
                      </div>
                      <div
                        v-if="fullShowVoiceState(selectedFullShowVariant.variant_key).timeline.length"
                        class="strategy-fullshow-timeline"
                      >
                        <header>
                          <div>
                            <strong>声音时间轴</strong>
                            <span>{{ formatFullShowTimelineMS(fullShowVoiceState(selectedFullShowVariant.variant_key).duration_ms) }}</span>
                          </div>
                          <em>蓝色为当前正在说的文字</em>
                        </header>
                        <div class="strategy-fullshow-timeline-list">
                          <article
                            v-for="segment in fullShowVoiceState(selectedFullShowVariant.variant_key).timeline"
                            :key="segment.segment_id"
                            :class="{ active: isFullShowTimelineSegmentActive(selectedFullShowVariant.variant_key, segment) }"
                          >
                            <time>
                              {{ formatFullShowTimelineMS(segment.start_ms) }}–{{ formatFullShowTimelineMS(segment.end_ms) }}
                            </time>
                            <span>{{ segment.text }}</span>
                          </article>
                        </div>
                      </div>
                    </div>

                    <div
                      v-if="!isUserAudioContentMode && fullShowVoiceState(selectedFullShowVariant.variant_key).candidate_url"
                      class="strategy-fullshow-voice-candidate"
                    >
                      <div>
                        <strong>新生成候选声音</strong>
                        <span>先试听；满意再替换当前声音，不满意直接放弃。</span>
                      </div>
                      <audio controls preload="metadata" :src="liveSupportMediaPlaybackURL(fullShowVoiceState(selectedFullShowVariant.variant_key).candidate_url)"></audio>
                      <small v-if="fullShowVoiceState(selectedFullShowVariant.variant_key).candidate_timeline.length">
                        新声音已建立精确声音时间轴；采用后会替换当前时间轴。
                      </small>
                      <footer>
                        <button type="button" @click="discardFullShowVoiceCandidate(selectedFullShowVariant.variant_key)">放弃本次生成</button>
                        <button class="primary" type="button" @click="adoptFullShowVoiceCandidate(selectedFullShowVariant.variant_key)">采用新声音</button>
                      </footer>
                    </div>

                    <div
                      v-if="fullShowVoiceState(selectedFullShowVariant.variant_key).status === 'failed'"
                      class="inline-error"
                    >
                      {{ fullShowVoiceState(selectedFullShowVariant.variant_key).error || '声音生成失败' }}
                    </div>
                  </section>

                  <div v-if="selectedFullShowVariant.tts_hints?.length" class="strategy-fullshow-tts-hints">
                    <header><strong>声音语气建议</strong><span>只做声音表现，不改变商品事实</span></header>
                    <div>
                      <article v-for="(hint, index) in selectedFullShowVariant.tts_hints" :key="hint.segment + ':' + index">
                        <strong>{{ hint.segment }}</strong>
                        <span>{{ hint.instruction }}</span>
                        <em v-if="hint.rate">语速 {{ hint.rate }}</em>
                      </article>
                    </div>
                  </div>
                </article>
              </template>
            </section>

            <section class="strategy-fullshow-panel strategy-fullshow-runtime">
              <header><div><span>✓</span><strong>生成检查</strong></div><small>{{ fullShowResult ? (fullShowAuditPassedCount + '/' + fullShowResult.variants.length + ' 通过') : '等待生成' }}</small></header>
              <div v-if="fullShowResult" class="strategy-fullshow-compile-summary">
                <div><span>规则 / 行业</span><strong>{{ fullShowResult.context.policy_rule_count }}</strong></div>
                <div><span>统一可生成事实</span><strong>{{ fullShowResult.context.authorized_facts?.length || 0 }}</strong></div>
                <div><span>正式事实</span><strong>{{ fullShowResult.context.formal_facts.length }}</strong></div>
                <div><span>有效活动</span><strong>{{ fullShowResult.context.benefits.length }}</strong></div>
                <div><span>可用商品</span><strong>{{ fullShowResult.context.product_links.length }}</strong></div>
                <div><span>风格规则</span><strong>{{ fullShowResult.context.anchor_style.dimensions.length }}</strong></div>
              </div>
              <div class="strategy-fullshow-runtime-list">
                <div><b>主线</b><span>完整稿连续播放，按安全语义点切入声音插播，不破坏上下文。</span></div>
                <div><b>插播</b><span>欢迎、答疑、逼单可以中断主线，结束后用桥接句回到原位置。</span></div>
                <div><b>动态纠正</b><span>直播中确认的新事实立即生效，并按现有规则回写当前直播方案。</span></div>
                <div><b>去重复</b><span>下一批主线避开近期高相似结构，不只是替换几个同义词。</span></div>
              </div>
            </section>
          </div>

          <section v-if="fullShowResult" class="strategy-fullshow-panel strategy-fullshow-publish">
            <header>
                <div><span>状态</span><strong>版本与发布状态</strong></div>
              <small>
                {{
                  fullShowSavedVersion
                    ? ('V' + fullShowSavedVersion.version_no + ' · ' + (fullShowSavedVersion.lifecycle_status === 'published' ? '已发布' : '待发布'))
                    : '尚未保存版本'
                }}
              </small>
            </header>

            <div class="strategy-fullshow-publish-checks">
              <div :class="{ ok: fullShowFormalSelectedCount > 0 }">
                <span>正式稿</span>
                <strong>{{ fullShowFormalSelectedCount }} 套</strong>
              </div>
              <div :class="{ ok: fullShowAllFormalVoicesReady }">
                <span>正式声音</span>
                <strong>{{ fullShowVoiceReadyCount }}/{{ fullShowFormalSelectedCount }}</strong>
              </div>
              <div :class="{ ok: fullShowVoiceIdentityConsistent }">
                <span>{{ isUserAudioContentMode ? '录音主线' : '声音身份' }}</span>
                <strong>{{ isUserAudioContentMode ? (fullShowVoiceIdentityConsistent ? '原声保留' : '待上传录音') : (fullShowVoiceIdentityConsistent ? '一致' : '需重新生成') }}</strong>
              </div>
              <div :class="{ ok: Boolean(currentVoiceIdentity.voice_id) }">
                <span>{{ isUserAudioContentMode ? '互动音色' : '当前身份' }}</span>
                <strong>{{ currentVoiceIdentity.name }} {{ currentVoiceIdentity.version }}</strong>
              </div>
            </div>

            <div class="strategy-fullshow-publish-note">
              <strong>发布不会自动开播</strong>
              <span>保存先生成不可覆盖的新版本；发布后只把该版本设为当前直播间待运行版本，回直播间点击“开始”后再由 Core 加载运行。</span>
            </div>

            <div v-if="fullShowSavedVersion" class="strategy-fullshow-saved-version">
              <div>
                <span>当前保存版本</span>
                <strong>V{{ fullShowSavedVersion.version_no }}</strong>
              </div>
              <div>
                <span>声音身份</span>
                <strong>{{ fullShowSavedVersion.voice_identity.name }} {{ fullShowSavedVersion.voice_identity.version }}</strong>
              </div>
              <div>
                <span>正式稿</span>
                <strong>{{ fullShowSavedVersion.variants.filter((item) => item.is_formal).map((item) => item.variant_key + '稿').join('、') }}</strong>
              </div>
              <div>
                <span>状态</span>
                <strong>{{ fullShowSavedVersion.lifecycle_status === 'published' ? '已发布' : '待发布' }}</strong>
              </div>
            </div>

            <div v-if="fullShowVersionHistory.length" class="strategy-fullshow-version-history">
              <header><strong>最近版本</strong><span>旧版本保留，不覆盖</span></header>
              <div>
                <article v-for="version in fullShowVersionHistory.slice(0, 5)" :key="version.id">
                  <strong>V{{ version.version_no }}</strong>
                  <span>
                    正式：
                    {{ version.variants.filter((item) => item.is_formal).map((item) => item.variant_key + '稿').join('、') }}
                    · 共 {{ version.variants.length }} 稿
                  </span>
                  <em :class="{ published: version.lifecycle_status === 'published' }">
                    {{
                      version.lifecycle_status === 'published'
                        ? '已发布'
                        : version.lifecycle_status === 'superseded'
                          ? '历史版本'
                          : '待发布'
                    }}
                  </em>
                </article>
              </div>
            </div>
          </section>
        </section>

        <section v-else class="strategy-agent-workspace">
          <div v-if="settingsError" class="inline-error strategy-inline-error">{{ settingsError }}</div>
          <div class="strategy-agent-context-card">
            <span class="section-kicker">CURRENT CONTEXT</span>
            <strong>{{ activeModeLabel }}</strong>
            <small>使用页面底部小蓝输入框进行调整</small>
          </div>
          <div class="strategy-upload-row">
            <input
              ref="documentInput"
              type="file"
              hidden
              accept=".txt,.md,.doc,.docx,.pdf,text/plain,text/markdown,application/pdf"
              @change="handleMediaUpload($event, 'document')"
            />
            <input
              ref="audioInput"
              type="file"
              hidden
              accept="audio/*"
              @change="handleMediaUpload($event, 'audio')"
            />
            <button type="button" :disabled="uploading" @click="documentInput?.click()">＋ 文案</button>
            <button type="button" :disabled="uploading" @click="audioInput?.click()">＋ 录音</button>
            <span v-if="uploading">素材上传中…</span>
          </div>
        </section>
          </div>
        </div>
      </main>
    </section>
  </div>
</template>

<style scoped>
.strategy-content-mode-panel{display:grid;gap:14px;padding:18px 20px;border:1px solid #dce3f4;border-radius:18px;background:linear-gradient(125deg,#fff,#f6f8ff)}
.strategy-content-mode-panel>header{display:flex;align-items:center;justify-content:space-between;gap:14px}.strategy-content-mode-panel>header strong{font-size:19px;color:#283750}.strategy-content-mode-panel>header span{font-size:13px;color:#8090a7}
.strategy-content-mode-options{display:grid;grid-template-columns:repeat(auto-fit,minmax(240px,1fr));gap:12px}.strategy-content-mode-options>button{position:relative;display:grid;gap:8px;align-content:start;min-height:100px;padding:16px;border:1px solid #dce2ef;border-radius:14px;background:#fff;text-align:left;font:inherit;cursor:pointer}.strategy-content-mode-options>button strong{color:#364764;font-size:16px;line-height:1.6}.strategy-content-mode-options>button span{color:#7c89a0;font-size:14px;line-height:1.6}.strategy-content-mode-options>button.active{border-color:#6879df;background:#f0f3ff;box-shadow:0 4px 16px #5666c215}.strategy-content-mode-options>button.active strong{color:#485bcc}.strategy-content-mode-options>button:disabled{cursor:default;opacity:.65}.strategy-content-mode-options>button em{color:#2c855d;font-style:normal;font-size:12px;font-weight:700}.strategy-content-mode-help{margin:0;color:#66778f;font-size:14px;line-height:1.8}.strategy-content-mode-help button,.strategy-content-mode-panel .inline-error button{margin-left:10px;border:0;background:transparent;color:#5366ce;font:inherit;cursor:pointer}
.live-strategy-page {
  min-width:0;
  max-width:100%;
  overflow-x:hidden;
}
.live-strategy-shell {
  min-width:0;
  max-width:100%;
}
.live-strategy-agent {
  min-width:0;
  width:100%;
  max-width:100%;
  grid-template-rows:max-content max-content max-content;
  align-content:start;
  overflow:visible;
}
.live-strategy-agent > * {
  min-width:0;
  max-width:100%;
  box-sizing:border-box;
}
.strategy-plan-settings { display:grid; gap:18px; }
.strategy-plan-create { display:grid; grid-template-columns:minmax(180px,.75fr) minmax(260px,1.4fr) auto; gap:10px; align-items:center; }
.strategy-plan-create input { min-height:44px; border:1px solid rgba(100,113,166,.18); border-radius:12px; padding:0 13px; background:#fff; color:#33415f; font:inherit; }
.strategy-plan-grid { display:grid; gap:12px; }
.strategy-plan-card { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:18px; align-items:center; padding:18px; border:1px solid rgba(100,113,166,.16); border-radius:18px; background:rgba(255,255,255,.82); }
.strategy-plan-card.active { border-color:rgba(82,101,225,.38); box-shadow:0 0 0 3px rgba(82,101,225,.07); }
.strategy-plan-card.bound { background:linear-gradient(135deg,rgba(248,250,255,.96),rgba(244,247,255,.9)); }
.strategy-plan-card.runtime { border-color:rgba(39,173,116,.34); box-shadow:0 0 0 3px rgba(39,173,116,.07); }
.strategy-plan-card div { display:grid; gap:6px; }
.strategy-plan-card span { color:#6876cf; font-size:12px; font-weight:850; }
.strategy-plan-card strong { color:#293756; font-size:18px; }
.strategy-plan-card p { margin:0; color:#77839a; line-height:1.55; }
.strategy-plan-card small { color:#919bb0; }
.strategy-plan-card button { min-width:132px; min-height:40px; border:1px solid rgba(82,101,225,.18); border-radius:11px; background:#f4f6ff; color:#5261cc; font-weight:800; cursor:pointer; }
.strategy-plan-card-actions { display:flex!important; align-items:center; justify-content:flex-end; gap:8px!important; flex-wrap:wrap; }
.strategy-plan-card-actions button { min-width:104px; }
.strategy-plan-card-actions button.primary { background:#eef1ff; border-color:#c7d0ff; color:#4658cf; }
.strategy-plan-card-actions button.danger { background:#fff6f6; border-color:#ffd6d9; color:#c95862; }
.strategy-plan-card button:disabled { cursor:default; opacity:.58; }
.strategy-plan-empty { padding:28px; border:1px dashed rgba(100,113,166,.22); border-radius:16px; text-align:center; color:#8a95aa; }
.strategy-plan-container-head { align-items:center; padding:10px 22px 16px; background:linear-gradient(135deg,#fbfcff 0%,#f7f9ff 100%); }
.strategy-plan-selector-row { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.strategy-plan-title-switch {
  display:block;
  width:min(660px,58vw);
  min-height:58px;
  margin-top:0;
  appearance:none;
  border:1px solid rgba(99,114,208,.24);
  border-radius:17px;
  padding:0 58px 0 20px;
  background:
    linear-gradient(45deg,transparent 50%,#596bc9 50%) calc(100% - 25px) 25px/7px 7px no-repeat,
    linear-gradient(135deg,#ffffff 0%,#f3f6ff 60%,#e9eeff 100%);
  color:#202c43;
  font:inherit;
  font-size:24px;
  font-weight:900;
  line-height:1.2;
  cursor:pointer;
  box-shadow:0 10px 24px rgba(70,84,157,.11),inset 0 1px 0 rgba(255,255,255,.95);
  transition:border-color .18s ease,box-shadow .18s ease,transform .18s ease,background .18s ease;
}
.strategy-plan-current-line { margin:5px 0 0!important; color:#68758f!important; font-size:16px; font-weight:750; line-height:1.5!important; }
.strategy-plan-title-switch:hover { border-color:rgba(86,102,205,.46); background-color:#f4f6ff; box-shadow:0 13px 30px rgba(70,84,157,.16),inset 0 1px 0 rgba(255,255,255,.98); transform:translateY(-1px); }
.strategy-plan-title-switch:focus { outline:none; border-color:#7c8be3; box-shadow:0 0 0 4px rgba(91,108,211,.12),0 13px 30px rgba(70,84,157,.15); }
.strategy-plan-title-switch:disabled { cursor:default; opacity:.72; }
.strategy-plan-relation-button { min-height:38px; padding:0 15px; border:1px solid #cbd4ff; border-radius:11px; background:#eef1ff; color:#4b5dcc; font-size:14px; font-weight:900; cursor:pointer; white-space:nowrap; }
.strategy-plan-relation-button.publish { border-color:#6576dd; background:linear-gradient(135deg,#5a6fd7,#6d79de); color:#fff; box-shadow:0 8px 18px rgba(78,94,199,.2); }
.strategy-plan-relation-button.danger { border-color:#efcbd1; background:#fff3f5; color:#bb5360; }
.strategy-plan-relation-button:disabled { cursor:default; opacity:.55; }
.strategy-plan-container-note { max-width:920px; margin-top:5px!important; color:#8b95a8!important; font-size:12px; line-height:1.55!important; }
.strategy-plan-toolbar { display:flex; align-items:center; justify-content:flex-start; gap:9px; flex-wrap:wrap; margin-top:12px; }
.strategy-plan-active-pill { padding:7px 11px; border-radius:999px; background:#eaf9f0; color:#16875a; font-size:12px; font-weight:900; }
.strategy-plan-active-pill:not(.runtime) { background:#eef1ff; color:#5362ce; }
.strategy-room-plan-relations { margin:0 0 2px; padding:11px 16px 12px; border:0; border-bottom:1px solid rgba(102,116,177,.13); border-radius:0; background:linear-gradient(135deg,rgba(250,251,255,.98),rgba(246,248,255,.92)); }
.strategy-room-plan-relations.collapsed { padding-block:10px; }
.strategy-room-plan-relations-head { display:flex; align-items:flex-end; justify-content:space-between; gap:16px; }
.strategy-room-plan-relations-head>div:first-child { display:grid; gap:4px; }
.strategy-room-plan-relations-head strong { color:#2f3d5a; font-size:15px; }
.strategy-room-plan-relations-head small { color:#8c96aa; font-size:11px; }
.strategy-room-plan-relations-controls { display:flex; align-items:center; justify-content:flex-end; gap:9px; }
.strategy-room-plan-collapse-button { display:grid; width:38px; height:38px; flex:0 0 38px; place-items:center; padding:0; border:1px solid #d2d9f2; border-radius:11px; background:#fff; color:#5362c9; font:inherit; font-size:18px; font-weight:900; cursor:pointer; box-shadow:0 5px 14px rgba(75,91,170,.07); }
.strategy-room-plan-collapse-button:hover { border-color:#aebaff; background:#f8f9ff; box-shadow:0 7px 18px rgba(75,91,170,.12); }
.strategy-room-plan-bind-box { display:flex; align-items:center; gap:8px; }
.strategy-room-plan-bind-box select { min-width:210px; min-height:38px; padding:0 34px 0 10px; border:1px solid #d8def2; border-radius:10px; background:#fff; color:#43506a; font:inherit; font-size:12px; }
.strategy-room-plan-bind-box button,.strategy-room-plan-binding-actions button { min-height:36px; padding:0 12px; border:1px solid #ced6ff; border-radius:9px; background:#eef1ff; color:#4f60ce; font-size:12px; font-weight:850; cursor:pointer; }
.strategy-room-plan-binding-list { display:grid; gap:8px; margin-top:11px; }
.strategy-room-plan-binding-list article { display:flex; align-items:center; justify-content:space-between; gap:14px; padding:10px 11px; border:1px solid rgba(101,115,180,.13); border-radius:12px; background:#fff; }
.strategy-room-plan-binding-list article.runtime { border-color:rgba(35,171,113,.25); background:#f4fcf8; }
.strategy-room-plan-binding-list article>div:first-child { display:grid; gap:2px; min-width:0; }
.strategy-room-plan-binding-list article strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; color:#33405d; font-size:13px; }
.strategy-room-plan-binding-list article span { color:#8994a8; font-size:11px; }
.strategy-room-plan-binding-list article.runtime span { color:#19875d; font-weight:800; }
.strategy-room-plan-binding-actions { display:flex; align-items:center; gap:6px; flex-wrap:wrap; justify-content:flex-end; }
.strategy-room-plan-binding-actions button.primary { background:#e9edff; color:#4053cf; }
.strategy-room-plan-binding-actions button.danger { border-color:#ffd4d8; background:#fff5f6; color:#c65a64; }
.strategy-room-plan-bind-box button:disabled,.strategy-room-plan-binding-actions button:disabled { cursor:default; opacity:.55; }
.strategy-room-plan-empty-binding { margin-top:11px; padding:12px; border:1px dashed #d5dced; border-radius:11px; color:#8994a8; font-size:12px; text-align:center; }
.strategy-new-plan-button,
.strategy-manage-plan-button,
.strategy-delete-plan-button { min-width:156px; min-height:44px; padding-inline:18px; border-radius:12px; font-size:15px; font-weight:900; }
.strategy-new-plan-button,
.strategy-manage-plan-button { display:inline-flex; align-items:center; justify-content:center; gap:11px; }
.strategy-plan-action-label { white-space:nowrap; }
.strategy-plan-expand-arrow { display:grid; width:24px; height:24px; flex:0 0 24px; place-items:center; border:1px solid rgba(78,91,202,.18); border-radius:50%; background:rgba(255,255,255,.68); color:currentColor; font-size:17px; font-weight:900; line-height:1; transition:transform .18s ease,background .18s ease,border-color .18s ease,box-shadow .18s ease; }
.strategy-plan-expand-arrow.expanded { transform:rotate(180deg); background:#fff; border-color:rgba(78,91,202,.34); box-shadow:0 4px 10px rgba(67,82,190,.12); }
.strategy-new-plan-button.expanded { box-shadow:0 10px 24px rgba(82,92,220,.16); transform:translateY(-1px); }
.strategy-manage-plan-button { border:1px solid #cfd6ff; background:#eef1ff; color:#4f5fd1; box-shadow:0 8px 18px rgba(84,97,202,.08); }
.strategy-manage-plan-button:hover { border-color:#aeb9ff; background:#e5e9ff; color:#4556ca; box-shadow:0 10px 22px rgba(84,97,202,.12); }
.strategy-manage-plan-button.expanded { border-color:#aab6ff; background:#e5e9ff; color:#4455c8; box-shadow:0 10px 24px rgba(84,97,202,.15); transform:translateY(-1px); }
.strategy-delete-plan-button { border:1px solid #efcfd4; background:#fff4f5; color:#bd5b66; box-shadow:0 8px 18px rgba(176,73,85,.06); }
.strategy-delete-plan-button:hover:not(:disabled) { border-color:#e4abb3; background:#ffe9ec; color:#aa4652; box-shadow:0 10px 22px rgba(176,73,85,.1); }
.strategy-delete-plan-button.confirming { border-color:#dc7d88; background:#df6673; color:#fff; box-shadow:0 10px 22px rgba(176,73,85,.18); }
.strategy-delete-plan-button.confirming:hover:not(:disabled) { border-color:#cb6572; background:#cf5966; color:#fff; }
.strategy-delete-plan-button:disabled { border-color:#e2e5eb; background:#f2f3f6; color:#adb4bf; box-shadow:none; cursor:not-allowed; opacity:1; }
.strategy-plan-manager-inline {
  position:relative;
  display:grid;
  gap:14px;
  margin:14px 18px 20px;
  padding:20px 22px 22px;
  border:1px solid #cbd5f2;
  border-radius:16px;
  background:linear-gradient(135deg,#fbfcff 0%,#f3f6ff 100%);
  box-shadow:0 14px 32px rgba(67,82,158,.12),inset 0 1px 0 rgba(255,255,255,.92);
  overflow:hidden;
}
.strategy-plan-manager-inline::before {
  content:"";
  position:absolute;
  left:0;
  top:0;
  bottom:0;
  width:4px;
  background:linear-gradient(180deg,#6074dd,#8f9af1);
}
.strategy-plan-create label { display:grid; gap:7px; min-width:0; }
.strategy-plan-create label>span { color:#586783; font-size:12px; font-weight:900; }
.strategy-plan-create input {
  border-color:#cbd4e9;
  background:#fff;
  box-shadow:inset 0 1px 2px rgba(52,66,115,.04);
}
.strategy-plan-create input:focus {
  border-color:#8796dc;
  outline:none;
  box-shadow:0 0 0 3px rgba(91,107,207,.09),inset 0 1px 2px rgba(52,66,115,.04);
}
.strategy-plan-create>button { align-self:end; min-height:44px; height:44px; box-shadow:0 8px 18px rgba(74,88,190,.2); }
.strategy-new-plan-button.expanded,
.strategy-manage-plan-button.expanded {
  border-color:#96a5f0;
  background:#dfe5ff;
  color:#4052c4;
  box-shadow:0 11px 26px rgba(77,93,190,.2);
  transform:translateY(-1px);
}
.strategy-plan-mini-list { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:9px; }
.strategy-plan-mini-list button { display:grid; gap:4px; min-width:0; padding:11px 13px; border:1px solid #e1e6f1; border-radius:12px; background:#fff; text-align:left; cursor:pointer; }
.strategy-plan-mini-list button.active { border-color:#8e99ef; background:#f0f2ff; box-shadow:0 0 0 2px rgba(91,96,221,.08); }
.strategy-plan-mini-list strong { overflow:hidden; color:#34405c; text-overflow:ellipsis; white-space:nowrap; }
.strategy-plan-mini-list small { color:#8b95a7; }
.strategy-mode-tabs-v2 { background:#fff; }
.strategy-workflow-nav {
  display:grid!important;
  width:100%;
  max-width:100%;
  min-width:0;
  box-sizing:border-box;
  grid-template-columns:minmax(0,1fr);
  align-items:center;
  gap:0;
  position:relative;
  overflow:hidden;
  padding:14px 14px;
  border-bottom:1px solid #e5eaf4;
  background:
    radial-gradient(circle at 86% 50%,rgba(86,103,210,.065),transparent 22%),
    linear-gradient(180deg,#fff 0%,#fbfcff 100%);
}
.strategy-workflow-nav::before {
  content:"";
  position:absolute;
  left:-6%;
  right:12%;
  bottom:-18px;
  height:48px;
  border-radius:50%;
  background:linear-gradient(90deg,rgba(102,116,223,.02),rgba(102,116,223,.08),rgba(102,116,223,.02));
  filter:blur(7px);
  pointer-events:none;
}
.strategy-workflow-inputs {
  display:grid;
  grid-template-columns:repeat(8,minmax(0,1fr)) minmax(142px,1.15fr);
  align-items:center;
  gap:6px;
  width:100%;
  min-width:0;
  overflow:hidden;
  position:relative;
  z-index:1;
  padding:6px 2px 8px;
}
.strategy-workflow-inputs::before {
  content:"";
  position:absolute;
  left:28px;
  right:28px;
  top:50%;
  height:2px;
  z-index:0;
  background:linear-gradient(90deg,#dfe5f1 0%,#c5cee6 52%,#aeb9df 100%);
  transform:translateY(-50%);
}
.strategy-workflow-step {
  display:flex;
  flex-direction:row;
  align-items:center;
  justify-content:center;
  gap:7px;
  position:relative;
  z-index:1;
  width:100%;
  min-width:0;
  min-height:56px;
  padding:7px 6px!important;
  border:1px solid #dfe5ef;
  border-radius:16px;
  background:rgba(255,255,255,.97);
  color:#526078;
  font:inherit;
  font-size:12px!important;
  font-weight:900;
  cursor:pointer;
  box-shadow:0 6px 16px rgba(43,58,104,.045);
  transition:transform .18s ease,border-color .18s ease,box-shadow .18s ease,background .18s ease,color .18s ease;
}
.strategy-workflow-label {
  display:block;
  min-width:0;
  color:inherit;
  text-align:center;
  line-height:1;
  white-space:nowrap;
  overflow:hidden;
  text-overflow:ellipsis;
}
.strategy-workflow-step:hover {
  transform:translateY(-2px);
  border-color:#b9c4df;
  background:#fbfcff;
  color:#4659b8;
  box-shadow:0 9px 20px rgba(67,82,176,.08);
}
.strategy-workflow-step.active {
  border-color:#aeb8e8;
  background:linear-gradient(135deg,#f0f3ff 0%,#f8f9ff 100%);
  color:#4d5ec6;
  box-shadow:0 0 0 2px rgba(82,99,198,.07),0 9px 20px rgba(67,82,176,.08);
}
.strategy-workflow-step:not(:last-child)::after {
  content:"";
  position:absolute;
  right:-9px;
  top:50%;
  width:8px;
  height:8px;
  border-top:2px solid #9ba8d4;
  border-right:2px solid #9ba8d4;
  transform:translateY(-50%) rotate(45deg);
  pointer-events:none;
}
.strategy-workflow-icon {
  display:grid;
  place-items:center;
  width:29px;
  height:29px;
  flex:0 0 29px;
  border-radius:9px;
  background:#f1f4fa;
  color:#6678b8;
  transition:.18s ease;
}
.strategy-workflow-icon svg {
  width:16px;
  height:16px;
  fill:none;
  stroke:currentColor;
  stroke-width:1.8;
  stroke-linecap:round;
  stroke-linejoin:round;
}
.strategy-workflow-step:nth-child(1) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(2) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(3) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(4) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(5) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(6) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(7) .strategy-workflow-icon,
.strategy-workflow-step:nth-child(8) .strategy-workflow-icon {
  background:#f0f3fb;
  color:#6678b8;
}
.strategy-workflow-step.active .strategy-workflow-icon {
  background:#5a69ca;
  color:#fff;
  box-shadow:0 4px 10px rgba(83,97,200,.16);
}
.strategy-workflow-output {
  display:flex;
  align-items:center;
  justify-content:center;
  gap:10px;
  position:relative;
  z-index:2;
  width:100%;
  min-width:0;
  min-height:56px;
  overflow:hidden;
  padding:9px 11px!important;
  border:1px solid #9eabe0;
  border-radius:18px;
  background:linear-gradient(135deg,#5b68c8 0%,#536fcf 54%,#4f80d1 100%);
  color:#fff;
  cursor:pointer;
  box-shadow:
    0 0 0 1px rgba(84,103,190,.10),
    0 12px 26px rgba(67,82,164,.20),
    0 0 20px rgba(75,101,190,.10);
  transition:transform .2s ease,box-shadow .2s ease,filter .2s ease;
}
.strategy-workflow-output::before {
  content:"";
  position:absolute;
  width:120px;
  height:120px;
  right:-42px;
  top:-55px;
  border-radius:50%;
  background:rgba(255,255,255,.10);
}
.strategy-workflow-output::after {
  content:"";
  position:absolute;
  left:-55%;
  top:-30%;
  width:38%;
  height:160%;
  background:linear-gradient(90deg,transparent,rgba(255,255,255,.24),transparent);
  transform:rotate(18deg);
  transition:left .45s ease;
}
.strategy-workflow-output:hover {
  transform:translateY(-2px) scale(1.015);
  filter:saturate(1.06);
  box-shadow:
    0 0 0 1px rgba(84,103,190,.14),
    0 16px 32px rgba(67,82,164,.24),
    0 0 26px rgba(75,101,190,.13);
}
.strategy-workflow-output:hover::after { left:120%; }
.strategy-workflow-output.active {
  box-shadow:
    0 0 0 3px rgba(83,100,190,.11),
    0 17px 34px rgba(67,82,164,.27),
    0 0 30px rgba(75,101,190,.16);
}
.strategy-workflow-output-mark {
  display:grid;
  place-items:center;
  width:28px;
  height:28px;
  flex:0 0 28px;
  border-radius:11px;
  background:rgba(255,255,255,.16);
  color:#fff;
  font-size:17px;
  text-shadow:0 0 14px rgba(255,255,255,.75);
}
.strategy-workflow-output-copy {
  display:grid;
  gap:2px;
  position:relative;
  z-index:1;
  text-align:left;
}
.strategy-workflow-output-copy small {
  color:rgba(255,255,255,.72);
  font-size:8px;
  font-weight:850;
  letter-spacing:.16em;
}
.strategy-workflow-output-copy strong {
  color:#fff;
  font-size:13px;
  font-weight:950;
  white-space:nowrap;
}
.strategy-legacy-select { margin-left:auto; min-height:38px; border:1px solid #e2e7ef; border-radius:10px; padding:7px 30px 7px 10px; background:#fff; color:#7b8598; font:inherit; font-size:12px; font-weight:800; }
.strategy-legacy-version-bar { display:flex; align-items:center; justify-content:flex-end; gap:8px; padding:10px 18px; border-bottom:1px solid #e8ecf3; background:#fafbfe; }
.strategy-legacy-version-bar-hidden { display:none !important; }
.strategy-legacy-version-bar>div { display:grid; gap:1px; margin-right:auto; }
.strategy-legacy-version-bar>div span { color:#9aa3b3; font-size:10px; font-weight:850; }
.strategy-legacy-version-bar>div strong { color:#69748a; font-size:12px; }

.strategy-v2-workspace { display:grid; gap:18px; align-content:start; min-height:520px; padding:12px 22px 22px; background:linear-gradient(180deg,#fbfcff 0%,#f7f9fe 100%); }
.strategy-script-workspace { padding-top:12px; }
.strategy-v2-section-head { display:flex; align-items:flex-start; justify-content:space-between; gap:18px; }
.strategy-v2-section-head h3 { margin:0 0 5px; color:#202c43; font-size:23px; }
.strategy-v2-section-head p { max-width:860px; margin:0; color:#7c879b; font-size:14px; line-height:1.55; }
.strategy-script-intro { font-size:14px; line-height:1.55!important; }
.strategy-v2-head-actions { display:flex; gap:8px; flex-wrap:wrap; justify-content:flex-end; }
.strategy-v2-empty { display:grid; gap:7px; min-height:150px; place-content:center; padding:28px; border:1px dashed #ccd4e5; border-radius:18px; background:#fff; color:#7d879a; text-align:center; }
.strategy-v2-empty strong { color:#394660; font-size:18px; }
.strategy-v2-empty span { max-width:620px; line-height:1.6; }
.strategy-script-notice { padding:11px 14px; border:1px solid #d8e5fb; border-radius:12px; background:#f1f6ff; color:#52648f; font-size:13px; }
.strategy-analysis-progress { display:grid; gap:10px; padding:14px 16px; border:1px solid #d7e1fb; border-radius:14px; background:linear-gradient(135deg,#f4f7ff,#eef3ff); box-shadow:0 8px 22px rgba(74,91,189,.06); }
.strategy-analysis-progress-head { display:flex; align-items:center; justify-content:space-between; gap:14px; }
.strategy-analysis-progress-head strong { color:#52628d; font-size:14px; }
.strategy-analysis-progress-head span { color:#5868d6; font-size:13px; font-weight:900; }
.strategy-analysis-progress-track { height:9px; overflow:hidden; border-radius:999px; background:#dfe5f5; }
.strategy-analysis-progress-track span { display:block; height:100%; border-radius:999px; background:linear-gradient(90deg,#7b88e8,#5868d6); transition:width .35s ease; box-shadow:0 0 12px rgba(88,104,214,.22); }
.strategy-script-simple-upload { display:flex; align-items:center; justify-content:flex-start; gap:12px; flex-wrap:nowrap; }
.strategy-script-simple-upload .strategy-version-button,
.strategy-script-simple-upload .primary-button { min-width:148px; min-height:44px; padding-inline:18px; font-size:14px; font-weight:850; }
.strategy-script-editor-single { display:block; width:100%; }
.strategy-script-editor-single textarea { min-height:520px; }

.strategy-script-library { display:flex; gap:9px; padding-bottom:2px; overflow-x:auto; }
.strategy-script-library button { display:grid; flex:0 0 230px; gap:5px; min-height:86px; padding:12px 14px; border:1px solid #e0e6f1; border-radius:14px; background:#fff; color:#45516b; text-align:left; cursor:pointer; }
.strategy-script-library button.active { border-color:#8794ed; background:#f1f3ff; box-shadow:0 0 0 2px rgba(83,99,218,.08); }
.strategy-script-library button.empty { flex-basis:100%; place-content:center; border-style:dashed; color:#8b95a8; text-align:center; }
.strategy-script-library strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.strategy-script-library span { color:#6574cf; font-size:12px; font-weight:850; }
.strategy-script-library small { color:#8d97aa; }

.strategy-script-meta { display:grid; grid-template-columns:minmax(0,1fr) minmax(220px,.35fr); gap:12px; }
.strategy-script-meta label { display:grid; gap:7px; }
.strategy-script-meta label>span { color:#647087; font-size:12px; font-weight:850; }
.strategy-script-meta input { min-height:44px; border:1px solid #dfe5ef; border-radius:12px; padding:0 13px; background:#fff; color:#34405b; font:inherit; }
.strategy-script-source-state { display:grid; gap:3px; align-content:center; padding:9px 13px; border:1px solid #e0e6f1; border-radius:12px; background:#fff; }
.strategy-script-source-state>span,.strategy-script-source-state small { color:#929caf; font-size:11px; }
.strategy-script-source-state strong { color:#47536e; }
.strategy-script-editor-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px; }
.strategy-script-editor { display:grid; grid-template-rows:auto 1fr; min-width:0; border:1px solid #dee5f1; border-radius:17px; overflow:hidden; background:#fff; }
.strategy-script-editor.readable { border-color:#ccd4ff; box-shadow:0 0 0 2px rgba(91,96,221,.035); }
.strategy-script-editor header { display:flex; align-items:center; justify-content:space-between; gap:12px; min-height:58px; padding:11px 14px; border-bottom:1px solid #e8ecf3; background:#fafbfe; }
.strategy-script-editor header>div { display:grid; gap:2px; }
.strategy-script-editor header span { color:#38445f; font-weight:900; }
.strategy-script-editor header small { color:#909aae; font-size:11px; }
.strategy-script-editor header button { border:1px solid #dce3f0; border-radius:9px; padding:6px 9px; background:#fff; color:#5b67c6; font-weight:800; cursor:pointer; }
.strategy-script-editor header button:disabled { opacity:.4; cursor:default; }
.strategy-script-lock { flex:0 0 auto; padding:5px 9px; border-radius:999px; background:#eef2f8; color:#8490a6!important; font-size:11px; }
.strategy-script-editor textarea { width:100%; min-height:440px; resize:vertical; border:0; outline:0; padding:16px; background:#fff; color:#38435c; font:inherit; font-size:14px; line-height:1.75; box-sizing:border-box; }
.strategy-script-editor textarea[readonly] { background:#f8f9fc; color:#6f798c; }
.strategy-analysis-draft-editor textarea { min-height:520px; }
.strategy-script-reference-formal { display:grid; gap:12px; padding:16px; border:1px solid #dde4f0; border-radius:16px; background:#fff; }
.strategy-script-reference-formal>header { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; padding-bottom:10px; border-bottom:1px solid #edf0f5; }
.strategy-script-reference-formal>header>div { display:flex; align-items:baseline; gap:10px; }
.strategy-script-reference-formal>header strong { color:#34405b; font-size:17px; }
.strategy-script-reference-formal>header span,.strategy-script-reference-formal>header small { color:#8a94a7; font-size:12px; line-height:1.5; }
.strategy-script-reference-empty { min-height:110px; }
.strategy-script-reference-list { display:grid; gap:10px; }
.strategy-script-reference-list>article { position:relative; display:grid; gap:10px; padding:14px 16px 14px 18px; border:1px solid #d9e1f4; border-radius:13px; background:linear-gradient(135deg,#f7f9ff 0%,#eef3ff 100%); box-shadow:0 6px 16px rgba(67,82,158,.055); overflow:hidden; }
.strategy-script-reference-list>article::before { content:''; position:absolute; inset:0 auto 0 0; width:4px; background:#7183e8; }
.strategy-script-reference-list>article:nth-child(even) { border-color:#dedcf4; background:linear-gradient(135deg,#faf8ff 0%,#f1eefb 100%); }
.strategy-script-reference-list>article:nth-child(even)::before { background:#9a78d7; }
.strategy-script-reference-list>article>header { display:flex; align-items:flex-start; justify-content:space-between; gap:14px; }
.strategy-script-reference-list>article>header>div { display:grid; gap:2px; }
.strategy-script-reference-list>article>header span { color:#5366d2; font-size:12px; font-weight:900; }
.strategy-script-reference-list>article>header strong { color:#34405a; font-size:15px; }
.strategy-script-reference-list>article>header em { padding:5px 9px; border-radius:999px; background:rgba(255,255,255,.8); color:#6370c8; font-size:11px; font-style:normal; font-weight:850; }
.strategy-script-reference-list>article>p { margin:0; color:#4c5870; font-size:14px; line-height:1.75; white-space:pre-wrap; word-break:break-word; }
.strategy-script-reference-list>article>footer { display:flex; gap:8px; flex-wrap:wrap; }
.strategy-script-reference-list>article>footer span { padding:4px 7px; border:1px solid rgba(200,209,232,.75); border-radius:7px; background:rgba(255,255,255,.72); color:#7a8498; font-size:10px; }
.strategy-script-reference-candidate-head { display:flex; align-items:flex-start; justify-content:space-between; gap:14px; padding:12px 14px; border-bottom:1px solid #e7ebf3; background:#fafbfe; }
.strategy-script-reference-candidate-head>div { display:flex; align-items:baseline; gap:9px; }
.strategy-script-reference-candidate-head strong { color:#45506a; font-size:15px; }
.strategy-script-reference-candidate-head span { color:#a07a2f; font-size:11px; font-weight:850; }
.strategy-script-reference-candidate-head small { color:#8a94a7; font-size:11px; line-height:1.5; }
.strategy-hidden-file-input { display:none; }
.strategy-oral-sample-workspace { gap:16px; }
.strategy-oral-sample-head-actions { display:flex; align-items:center; gap:10px; flex-wrap:wrap; }
.strategy-oral-sample-head-actions button { min-height:40px; padding:8px 15px; border-radius:11px; font-weight:900; }
.strategy-oral-sample-rule { display:grid; grid-template-columns:auto minmax(0,1fr); gap:8px 14px; align-items:start; padding:14px 16px; border:1px solid #dce5fa; border-radius:14px; background:linear-gradient(135deg,#f5f8ff,#fbfcff); }
.strategy-oral-sample-rule strong { color:#5365c9; font-size:13px; }
.strategy-oral-sample-rule span { color:#69758c; font-size:13px; line-height:1.65; }
.strategy-oral-sample-layout { display:grid; grid-template-columns:320px minmax(0,1fr); min-height:660px; border:1px solid #dfe5f1; border-radius:18px; background:#fff; overflow:hidden; box-shadow:0 10px 28px rgba(52,68,124,.055); }
.strategy-oral-sample-sidebar { display:grid; align-content:start; gap:10px; padding:16px; border-right:1px solid #e5e9f2; background:linear-gradient(180deg,#f7f9ff 0%,#f3f5fa 100%); overflow:auto; }
.strategy-oral-sample-sidebar>header { display:grid; gap:4px; padding:2px 2px 10px; border-bottom:1px solid #e4e8f2; }
.strategy-oral-sample-sidebar>header>div { display:flex; align-items:baseline; justify-content:space-between; gap:10px; }
.strategy-oral-sample-sidebar>header strong { color:#34415c; font-size:18px; }
.strategy-oral-sample-sidebar>header span { color:#5c6dd0; font-size:12px; font-weight:900; }
.strategy-oral-sample-sidebar>header small { color:#909aae; font-size:11px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.strategy-oral-sample-card { display:grid; gap:7px; width:100%; padding:13px 14px; border:1px solid #dfe4ef; border-radius:13px; background:#fff; color:inherit; text-align:left; cursor:pointer; box-shadow:0 4px 12px rgba(48,62,105,.035); transition:.16s ease; }
.strategy-oral-sample-card:hover { transform:translateY(-1px); border-color:#c3cceb; box-shadow:0 8px 18px rgba(48,62,105,.07); }
.strategy-oral-sample-card.active { border-color:#7e8fe7; background:linear-gradient(135deg,#eef2ff,#f7f8ff); box-shadow:0 0 0 2px rgba(93,109,214,.09),0 10px 22px rgba(62,79,163,.09); }
.strategy-oral-sample-card-title { color:#35425e; font-size:14px; font-weight:900; line-height:1.45; }
.strategy-oral-sample-card-preview { display:-webkit-box; overflow:hidden; color:#69758b; font-size:12px; line-height:1.55; -webkit-box-orient:vertical; -webkit-line-clamp:2; }
.strategy-oral-sample-card-meta { display:flex; gap:6px; flex-wrap:wrap; }
.strategy-oral-sample-card-meta b,.strategy-oral-sample-card-meta i { padding:3px 6px; border-radius:6px; background:#f1f3f8; color:#7c879a; font-size:10px; font-style:normal; font-weight:800; }
.strategy-oral-sample-card.active .strategy-oral-sample-card-meta b { background:#dfe5ff; color:#5668ca; }
.strategy-oral-sample-card>small { color:#9ba4b4; font-size:10px; }
.strategy-oral-sample-sidebar-empty { display:grid; gap:6px; padding:22px 14px; border:1px dashed #cfd7e8; border-radius:13px; background:rgba(255,255,255,.7); text-align:center; }
.strategy-oral-sample-sidebar-empty strong { color:#5b667d; }
.strategy-oral-sample-sidebar-empty span { color:#929caf; font-size:12px; line-height:1.55; }
.strategy-oral-sample-editor { min-width:0; display:grid; align-content:start; gap:14px; padding:18px 20px 22px; background:#fff; }
.strategy-oral-sample-editor-head { display:flex; align-items:flex-start; justify-content:space-between; gap:18px; padding-bottom:13px; border-bottom:1px solid #e9edf4; }
.strategy-oral-sample-editor-head>div:first-child { display:grid; gap:2px; }
.strategy-oral-sample-editor-head>div:first-child span { color:#7080dc; font-size:10px; font-weight:950; letter-spacing:.12em; }
.strategy-oral-sample-editor-head>div:first-child strong { color:#34405a; font-size:20px; }
.strategy-oral-sample-editor-head>div:last-child { display:flex; gap:8px; flex-wrap:wrap; justify-content:flex-end; }
.strategy-oral-sample-editor-head button { min-height:36px; padding:7px 12px; border:1px solid #d7deed; border-radius:9px; background:#fff; color:#56627a; font:inherit; font-size:12px; font-weight:900; cursor:pointer; }
.strategy-oral-sample-editor-head button.primary-button { border-color:#5c6fd7; background:#5c6fd7; color:#fff; }
.strategy-oral-sample-editor-head button.is-danger { border-color:#efd0d4; background:#fff7f8; color:#b75460; }
.strategy-oral-sample-editor-head button:disabled { opacity:.52; cursor:default; }
.strategy-oral-sample-title-field { display:grid; gap:6px; }
.strategy-oral-sample-title-field>span { color:#657188; font-size:12px; font-weight:900; }
.strategy-oral-sample-title-field input { width:100%; min-height:42px; padding:9px 12px; border:1px solid #d9dfeb; border-radius:10px; background:#fbfcff; color:#34405a; font:inherit; font-size:15px; font-weight:850; outline:none; box-sizing:border-box; }
.strategy-oral-sample-title-field input:focus { border-color:#8796df; box-shadow:0 0 0 3px rgba(91,106,207,.08); background:#fff; }
.strategy-oral-sample-document { overflow:hidden; border:1px solid #dfe4ed; border-radius:14px; background:#fff; }
.strategy-oral-sample-document>header { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:10px 13px; border-bottom:1px solid #e8ecf3; background:#fafbfe; }
.strategy-oral-sample-document>header>div { display:flex; align-items:baseline; gap:9px; }
.strategy-oral-sample-document>header strong { color:#48546d; font-size:14px; }
.strategy-oral-sample-document>header span,.strategy-oral-sample-document>header small { color:#8c96a8; font-size:11px; }
.strategy-oral-sample-document textarea { display:block; width:100%; min-height:430px; resize:vertical; padding:18px 20px; border:0; outline:0; background:#fff; color:#39455e; font:inherit; font-size:15px; line-height:1.9; box-sizing:border-box; }
.strategy-oral-sample-document textarea::placeholder { color:#a7afbd; }
.strategy-oral-sample-notice { margin:0; padding:9px 12px; border-radius:9px; background:#f2f5ff; color:#6070c9; font-size:12px; line-height:1.55; }
.strategy-oral-sample-analysis { display:grid; gap:12px; padding:15px; border:1px solid #e0e5f0; border-radius:15px; background:#fafbfe; }
.strategy-oral-sample-analysis>header { display:flex; align-items:flex-start; justify-content:space-between; gap:14px; }
.strategy-oral-sample-analysis>header>div { display:grid; gap:2px; }
.strategy-oral-sample-analysis>header span { color:#7382d8; font-size:10px; font-weight:950; letter-spacing:.1em; }
.strategy-oral-sample-analysis>header strong { color:#3f4b65; font-size:16px; }
.strategy-oral-sample-analysis>header small { max-width:420px; color:#929bad; font-size:11px; line-height:1.55; text-align:right; }
.strategy-oral-sample-analysis-empty { display:grid; gap:5px; padding:18px; border:1px dashed #d5dbea; border-radius:11px; background:#fff; text-align:center; }
.strategy-oral-sample-analysis-empty strong { color:#5d687e; }
.strategy-oral-sample-analysis-empty span { color:#949daf; font-size:12px; line-height:1.55; }
.strategy-oral-sample-analysis-summary { margin:0; padding:11px 13px; border-radius:10px; background:#fff; color:#5d6980; font-size:13px; line-height:1.65; }
.strategy-oral-sample-structure-list { display:grid; gap:8px; }
.strategy-oral-sample-structure-list article { display:grid; grid-template-columns:38px minmax(0,1fr); gap:11px; align-items:start; padding:11px 12px; border:1px solid #e2e6ef; border-radius:11px; background:#fff; }
.strategy-oral-sample-structure-list article>span { display:grid; width:32px; height:32px; place-items:center; border-radius:50%; background:#eef1ff; color:#5d6dd0; font-size:11px; font-weight:950; }
.strategy-oral-sample-structure-list article>div { display:grid; gap:4px; }
.strategy-oral-sample-structure-list article strong { color:#3f4b64; font-size:13px; }
.strategy-oral-sample-structure-list article p { margin:0; color:#69758a; font-size:12px; line-height:1.55; }
.strategy-oral-sample-structure-list article small { color:#8b95a8; font-size:11px; line-height:1.45; }
.strategy-oral-sample-editor-empty { display:grid; place-items:center; align-content:center; gap:8px; min-height:560px; padding:30px; text-align:center; }
.strategy-oral-sample-editor-empty strong { color:#4f5b73; font-size:18px; }
.strategy-oral-sample-editor-empty span { max-width:420px; color:#929caf; font-size:13px; line-height:1.65; }
@media (max-width:1180px) {
  .strategy-oral-sample-layout { grid-template-columns:1fr; }
  .strategy-oral-sample-sidebar { border-right:0; border-bottom:1px solid #e5e9f2; max-height:360px; }
}
.strategy-link-coverage { display:grid; grid-template-columns:repeat(5,minmax(0,1fr)); gap:12px; }
.strategy-link-coverage>div { display:grid; grid-template-columns:1fr auto; gap:4px 10px; align-items:end; min-height:86px; padding:16px 18px; border:1px solid #dfe5f0; border-radius:16px; background:#fff; }
.strategy-link-coverage span { color:#748096; font-size:14px; font-weight:850; }
.strategy-link-coverage strong { color:#5868d6; font-size:28px; line-height:1; }
.strategy-link-coverage small { grid-column:1/-1; color:#949dae; font-size:13px; }
.strategy-link-coverage .is-warning { border-color:#e4b778; background:#fff9ef; }
.strategy-link-coverage .is-warning strong { color:#b36b18; }
.strategy-link-coverage .is-warning small { color:#a06a24; }
.strategy-product-link-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }
.strategy-product-formal { display:grid; gap:16px; min-width:0; padding:18px; border:1px solid #e0e5fb; border-radius:18px; background:rgba(255,255,255,.95); box-shadow:0 14px 36px rgba(78,91,171,.06); }
.strategy-product-formal>header { display:flex; align-items:flex-start; justify-content:space-between; gap:12px; padding-bottom:12px; border-bottom:1px solid #e9ecf7; }
.strategy-product-formal>header>div { display:flex; align-items:baseline; gap:14px; flex-wrap:wrap; }
.strategy-product-formal>header strong { color:#172440; font-size:18px; font-weight:900; }
.strategy-product-formal>header span,.strategy-product-formal>header small { color:#8a94a7; font-size:15px; line-height:1.5; }
.strategy-product-formal-empty { min-height:110px; }
.strategy-product-intro { font-size:14px!important; line-height:1.55!important; }
@media(max-width:600px) { .strategy-product-formal { padding:12px; } }
.strategy-product-link-card { display:grid; gap:14px; padding:18px; border:1px solid #dfe5f0; border-radius:18px; background:#fff; box-shadow:0 8px 22px rgba(48,63,104,.045); }
.strategy-product-link-card>header { display:flex; align-items:flex-start; justify-content:space-between; gap:14px; padding-bottom:12px; border-bottom:1px solid #e9edf4; }
.strategy-product-link-card>header>div { display:grid; gap:4px; }
.strategy-product-link-card>header span { color:#6472d0; font-size:15px; font-weight:900; }
.strategy-product-link-card>header strong { color:#2f3b56; font-size:21px; }
.strategy-product-link-card>header em { padding:5px 9px; border-radius:999px; background:#f3f5f9; color:#6f7b91; font-size:13px; font-style:normal; font-weight:850; }
.strategy-product-link-card.is-adoptable>header em { background:#eaf8f0; color:#2f7d5a; }
.strategy-product-link-card.is-conflict>header em { background:#fff4e6; color:#b36b18; }
.strategy-product-link-card.is-violation>header em { background:#fff0f0; color:#b54848; }
.strategy-product-link-card dl { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px 14px; margin:0; }
.strategy-product-link-card dl>div { display:grid; gap:3px; padding:10px 12px; border-radius:11px; background:#f8f9fc; }
.strategy-product-link-card dt { color:#8a94a7; font-size:13px; font-weight:800; }
.strategy-product-link-card dd { margin:0; color:#34405a; font-size:17px; line-height:1.5; word-break:break-word; }
.strategy-product-link-attributes { border:1px solid #e4e8f3; border-radius:11px; background:#fbfcff; overflow:hidden; }
.strategy-product-link-attributes summary { display:flex; align-items:center; justify-content:space-between; gap:8px; padding:9px 11px; color:#5667ca; font-size:12px; font-weight:850; cursor:pointer; }
.strategy-product-link-attributes summary span { color:#929bad; font-size:10px; font-weight:500; }
.strategy-product-link-attributes>dl { padding:0 10px 10px; }
.strategy-product-link-attributes>dl>div { padding:8px 10px; background:#f2f4fc; }
.strategy-product-link-attributes>dl dt { font-size:11px; }
.strategy-product-link-attributes>dl dd { font-size:13px; }
.strategy-product-link-footer { display:flex; align-items:center; justify-content:space-between; gap:12px; padding-top:2px; color:#778296; font-size:12px; line-height:1.55; }
.strategy-product-link-footer button { min-width:108px; min-height:36px; padding:7px 12px; }
.strategy-product-link-evidence { display:grid; gap:7px; padding-top:4px; }
.strategy-product-link-evidence b { color:#68748a; font-size:14px; }
.strategy-product-link-evidence p { margin:0; padding:9px 11px; border-left:3px solid #d8deeb; background:#fafbfc; color:#59657b; font-size:15px; line-height:1.6; white-space:pre-wrap; }
.strategy-benefit-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }
.strategy-benefit-formal { display:grid; gap:12px; padding:16px; border:1px solid #dde4f0; border-radius:16px; background:#fff; }
.strategy-benefit-formal>header { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; padding-bottom:10px; border-bottom:1px solid #edf0f5; }
.strategy-benefit-formal>header>div { display:flex; align-items:baseline; gap:10px; }
.strategy-benefit-formal>header strong { color:#34405b; font-size:17px; }
.strategy-benefit-formal>header span,.strategy-benefit-formal>header small { color:#8a94a7; font-size:12px; line-height:1.5; }
.strategy-benefit-formal-empty { min-height:110px; }
.strategy-benefit-formal-list { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; }
.strategy-benefit-formal-list>article { display:grid; width:auto; justify-self:stretch; box-sizing:border-box; gap:12px; padding:16px; border:1px solid #dfe7f5; border-radius:14px; background:linear-gradient(145deg,#fff 0%,#fcfdff 100%); box-shadow:0 8px 22px rgba(74,92,155,.07); }
.strategy-benefit-formal-card-head { display:grid; gap:12px; align-items:start; }
.strategy-benefit-formal-icon { display:grid; width:76px; height:76px; place-items:center; border-radius:18px; background:linear-gradient(145deg,#eef4ff,#e6edff); color:#3e63df; }
.strategy-benefit-formal-icon svg { width:44px; height:44px; fill:none; stroke:currentColor; stroke-width:1.65; stroke-linecap:round; stroke-linejoin:round; }
.strategy-benefit-formal-title { display:grid; gap:7px; min-width:0; }
.strategy-benefit-formal-title>span { color:#3e63df; font-size:16px; font-weight:900; line-height:1.25; }
.strategy-benefit-formal-title>strong { color:#172847; font-size:24px; line-height:1.35; font-weight:900; overflow-wrap:anywhere; }
.strategy-benefit-formal-title>small { color:#8b9ab5; font-size:13px; line-height:1.45; overflow-wrap:anywhere; }
.strategy-benefit-formal-title>em { display:inline-flex; align-items:center; gap:6px; width:max-content; margin-top:3px; padding:5px 9px; border-radius:999px; font-size:13px; font-style:normal; font-weight:900; white-space:nowrap; }
.strategy-benefit-formal-title>em i { width:7px; height:7px; border-radius:50%; background:currentColor; }
.strategy-benefit-formal-title>em.is-active { background:#e5f8ed; color:#18865d; }
.strategy-benefit-formal-title>em.is-draft { background:#eef2ff; color:#5f6cc9; }
.strategy-benefit-formal-title>em.is-expired { background:#f2f3f5; color:#858d9b; }
.strategy-benefit-formal-title>em.is-disabled { background:#fff0f0; color:#b54848; }
.strategy-benefit-formal-rule { height:1px; background:#e5ebf5; }
.strategy-benefit-formal-detail { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px; }
.strategy-benefit-formal-detail-item { display:grid; grid-template-columns:30px minmax(0,1fr); grid-template-rows:auto auto; column-gap:8px; align-items:end; min-width:0; min-height:72px; padding:10px 11px; border-radius:11px; background:linear-gradient(145deg,#f5f8ff,#f0f4fc); }
.strategy-benefit-formal-detail-item>svg { grid-row:1 / span 2; width:26px; height:26px; align-self:center; fill:none; stroke:#4969de; stroke-width:1.7; stroke-linecap:round; stroke-linejoin:round; }
.strategy-benefit-formal-detail-item>span { color:#8290a9; font-size:12px; line-height:1.35; }
.strategy-benefit-formal-detail-item>strong { color:#192d51; font-size:14px; line-height:1.4; font-weight:650; overflow-wrap:anywhere; }
.strategy-benefit-formal-detail-wide { grid-column:1/-1; }
.strategy-benefit-formal-window { display:grid; grid-template-columns:32px minmax(0,1fr); gap:9px; align-items:start; color:#6f7b90; }
.strategy-benefit-formal-window>svg { width:28px; height:28px; fill:none; stroke:#7b8baa; stroke-width:1.8; stroke-linecap:round; stroke-linejoin:round; }
.strategy-benefit-formal-window>div { display:grid; gap:4px; min-width:0; }
.strategy-benefit-formal-window span { color:#7e8da8; font-size:13px; line-height:1.35; }
.strategy-benefit-formal-window strong { color:#1c3156; font-size:14px; line-height:1.45; font-weight:650; overflow-wrap:anywhere; }
.strategy-benefit-formal-window i { color:#7d8eac; font-style:normal; padding-inline:5px; }
.strategy-benefit-formal-window small { color:#8d9bb3; font-size:11px; line-height:1.55; overflow-wrap:anywhere; }
.strategy-benefit-formal-expand { display:inline-flex; align-items:center; justify-content:center; gap:5px; justify-self:center; padding:3px 6px; border:0; background:transparent; color:#4264df; font:inherit; font-size:14px; font-weight:750; cursor:pointer; }
.strategy-benefit-formal-expand svg { width:17px; height:17px; fill:none; stroke:currentColor; stroke-width:2.2; transition:transform .18s ease; }
.strategy-benefit-formal-item.expanded .strategy-benefit-formal-expand svg { transform:rotate(180deg); }
.strategy-benefit-card { display:grid; gap:14px; padding:18px; border:1px solid #e2dfef; border-radius:18px; background:#fff; box-shadow:0 8px 22px rgba(48,63,104,.045); }
.strategy-benefit-card>header { display:flex; align-items:flex-start; justify-content:space-between; gap:14px; padding-bottom:12px; border-bottom:1px solid #ece8f2; }
.strategy-benefit-card>header>div { display:grid; gap:4px; }
.strategy-benefit-card>header span { color:#7b68cf; font-size:15px; font-weight:900; }
.strategy-benefit-card>header strong { color:#2f3b56; font-size:20px; }
.strategy-benefit-card>header em { padding:5px 9px; border-radius:999px; background:#f5f2fb; color:#7665b6; font-size:12px; font-style:normal; font-weight:850; }
.strategy-benefit-card.is-adoptable>header em { background:#eaf8f0; color:#2f7d5a; }
.strategy-benefit-card.is-conflict>header em { background:#fff4e6; color:#b36b18; }
.strategy-benefit-card.is-violation>header em { background:#fff0f0; color:#b54848; }
.strategy-benefit-card dl { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px 14px; margin:0; }
.strategy-benefit-card dl>div { display:grid; gap:3px; padding:10px 12px; border-radius:11px; background:#f8f9fc; }
.strategy-benefit-card dt { color:#8a94a7; font-size:13px; font-weight:800; }
.strategy-benefit-card dd { margin:0; color:#34405a; font-size:16px; line-height:1.5; word-break:break-word; }
.strategy-benefit-card footer { display:flex; align-items:center; justify-content:space-between; gap:12px; padding-top:2px; color:#8b728d; font-size:12px; line-height:1.55; }
.strategy-benefit-card footer button { min-width:108px; min-height:36px; padding:7px 12px; }
.strategy-fact-review-cards { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:14px; }
.strategy-fact-review-card { display:grid; grid-template-columns:1fr auto auto; align-items:center; gap:8px; min-height:88px; padding:18px 20px; border:1px solid #dfe5f0; border-radius:18px; background:#fff; box-shadow:0 8px 22px rgba(48,63,104,.05); cursor:pointer; text-align:left; transition:.18s ease; }
.strategy-fact-review-card:hover { transform:translateY(-1px); box-shadow:0 12px 26px rgba(48,63,104,.09); }
.strategy-fact-review-card span { color:#4c5871; font-size:18px; font-weight:900; }
.strategy-fact-review-card strong { font-size:28px; line-height:1; }
.strategy-fact-review-card small { color:#8791a4; font-size:14px; font-weight:800; }
.strategy-fact-review-card.active { box-shadow:0 0 0 2px rgba(91,96,221,.10),0 12px 26px rgba(48,63,104,.08); }
.strategy-fact-review-card.is-adoptable.active { border-color:#82c5a8; background:#f1fbf6; }
.strategy-fact-review-card.is-adoptable strong { color:#2f7d5a; }
.strategy-fact-review-card.is-conflict.active { border-color:#e3b36e; background:#fff8ee; }
.strategy-fact-review-card.is-conflict strong { color:#b36b18; }
.strategy-fact-review-card.is-discuss.active { border-color:#aaa2ef; background:#f6f4ff; }
.strategy-fact-review-card.is-discuss strong { color:#6a62b5; }
.strategy-fact-review-card.is-violation.active { border-color:#e5a1a1; background:#fff3f3; }
.strategy-fact-review-card.is-violation strong { color:#b54848; }
.strategy-fact-review-detail { display:grid; gap:12px; }
.strategy-fact-review-detail-head { display:flex; align-items:center; justify-content:space-between; padding:0 4px; }
.strategy-fact-review-detail-head>div { display:flex; align-items:baseline; gap:10px; }
.strategy-fact-review-detail-head strong { color:#34405a; font-size:22px; font-weight:900; }
.strategy-fact-review-detail-head span { color:#8a95a9; font-size:15px; font-weight:800; }
.strategy-fact-adopt-actions { display:flex!important; align-items:center!important; justify-content:flex-end; gap:12px!important; flex-wrap:wrap; }
.strategy-fact-adopt-actions label { display:flex; align-items:center; gap:7px; color:#5b667c; font-size:15px; font-weight:800; cursor:pointer; }
.strategy-fact-adopt-actions input,
.strategy-fact-select-cell input { width:18px; height:18px; accent-color:#5e6bd8; cursor:pointer; }
.strategy-fact-adopt-actions input:disabled,
.strategy-fact-select-cell input:disabled { cursor:not-allowed; opacity:.5; }
.strategy-fact-adopt-actions .primary-button { min-width:96px; min-height:40px; padding:8px 18px; font-size:16px; }
.strategy-fact-review-detail.is-adoptable .strategy-fact-review-detail-head strong { color:#2f7d5a; }
.strategy-fact-review-detail.is-conflict .strategy-fact-review-detail-head strong { color:#b36b18; }
.strategy-fact-review-detail.is-discuss .strategy-fact-review-detail-head strong { color:#6a62b5; }
.strategy-fact-review-detail.is-violation .strategy-fact-review-detail-head strong { color:#b54848; }
@media (max-width:1100px) { .strategy-fact-review-cards { grid-template-columns:repeat(2,minmax(0,1fr)); } }
.strategy-fact-table-wrap { overflow:auto; border:1px solid #dfe5f0; border-radius:16px; background:#fff; }
.strategy-fact-table { width:100%; min-width:1080px; border-collapse:collapse; table-layout:fixed; }
.strategy-fact-table th,
.strategy-fact-table td { padding:16px 18px; border-bottom:1px solid #e7ebf2; color:#34405a; font-size:18px; line-height:1.6; text-align:left; vertical-align:top; word-break:break-word; }
.strategy-fact-table th { background:#eef2fa; color:#47536b; font-weight:900; }
.strategy-fact-table tbody tr:nth-child(odd) td { background:#fff; }
.strategy-fact-table tbody tr:nth-child(even) td { background:#f6f8fc; }
.strategy-fact-table tbody tr:last-child td { border-bottom:0; }
.strategy-fact-table th:nth-child(1),
.strategy-fact-table td:nth-child(1) { width:7%; text-align:center; }
.strategy-fact-table th:nth-child(2),
.strategy-fact-table td:nth-child(2) { width:11%; }
.strategy-fact-table th:nth-child(3),
.strategy-fact-table td:nth-child(3) { width:15%; }
.strategy-fact-table th:nth-child(4),
.strategy-fact-table td:nth-child(4) { width:23%; }
.strategy-fact-table th:nth-child(5),
.strategy-fact-table td:nth-child(5) { width:26%; }
.strategy-fact-table th:nth-child(6),
.strategy-fact-table td:nth-child(6) { width:18%; }
.strategy-fact-select-cell { vertical-align:middle!important; }
.strategy-fact-select-cell small { display:block; margin-top:5px; color:#2f7d5a; font-size:12px; font-weight:850; white-space:nowrap; }
.strategy-fact-select-cell small.is-danger { color:#b54848; }
.strategy-fact-empty { padding:52px 20px !important; color:#8a95a9 !important; text-align:center !important; }
.strategy-formal-facts { display:grid; gap:14px; margin-top:8px; padding:18px; border:1px solid #dce3ef; border-radius:18px; background:#fff; box-shadow:0 8px 22px rgba(48,63,104,.04); }
.strategy-formal-facts-head { display:flex; align-items:flex-start; justify-content:space-between; gap:20px; padding:0 2px 12px; border-bottom:1px solid #edf0f5; }
.strategy-formal-facts-head>div { display:grid; gap:3px; }
.strategy-formal-facts-head strong { color:#2f3b56; font-size:20px; font-weight:950; }
.strategy-formal-facts-head small,.strategy-formal-facts-head>span { color:#8a95a8; font-size:12px; line-height:1.55; }
.strategy-formal-facts-head>span { max-width:520px; text-align:right; }
.strategy-fact-head-actions { display:flex; align-items:center; gap:9px; flex-wrap:wrap; }
.strategy-fact-boundary-guide { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:10px; margin:0 0 14px; }
.strategy-fact-boundary-guide>div { display:grid; gap:4px; padding:12px 14px; border:1px solid #e0e6f2; border-radius:12px; background:#f8faff; }
.strategy-fact-boundary-guide strong { color:#42518f; font-size:13px; }
.strategy-fact-boundary-guide span { color:#7b879b; font-size:12px; line-height:1.55; }
.strategy-formal-fact-create { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:12px; padding:14px; border:1px solid #dce4f2; border-radius:14px; background:#f8faff; }
.strategy-formal-fact-create label { display:grid; gap:5px; }
.strategy-formal-fact-create label.is-wide { grid-column:1 / -1; }
.strategy-formal-fact-create label>span { color:#59667d; font-size:12px; font-weight:850; }
.strategy-formal-fact-create input,.strategy-formal-fact-create select,.strategy-formal-fact-create textarea { width:100%; border:1px solid #ccd5e5; border-radius:9px; background:#fff; color:#34405a; font:inherit; outline:none; padding:8px 9px; }
.strategy-formal-fact-create textarea { resize:vertical; }
.strategy-formal-fact-create-actions { display:flex; gap:8px; align-items:center; grid-column:1 / -1; }
.strategy-formal-facts-empty { min-height:120px; }
.strategy-formal-facts-table-wrap { overflow-x:auto; border:1px solid #e3e8f1; border-radius:14px; }
.strategy-formal-facts-table { width:100%; min-width:1280px; border-collapse:collapse; table-layout:fixed; }
.strategy-formal-facts-table th,.strategy-formal-facts-table td { padding:13px 14px; border-bottom:1px solid #edf0f5; color:#3d4961; font-size:14px; line-height:1.55; vertical-align:middle; text-align:left; word-break:break-word; }
.strategy-formal-facts-table th { background:#f3f6fb; color:#647188; font-size:13px; font-weight:900; }
.strategy-formal-facts-table tr:last-child td { border-bottom:0; }
.strategy-formal-facts-table th:nth-child(1),.strategy-formal-facts-table td:nth-child(1) { width:10%; }
.strategy-formal-facts-table th:nth-child(2),.strategy-formal-facts-table td:nth-child(2) { width:14%; }
.strategy-formal-facts-table th:nth-child(3),.strategy-formal-facts-table td:nth-child(3) { width:27%; }
.strategy-formal-facts-table th:nth-child(4),.strategy-formal-facts-table td:nth-child(4) { width:17%; }
.strategy-formal-facts-table th:nth-child(5),.strategy-formal-facts-table td:nth-child(5) { width:17%; }
.strategy-formal-facts-table th:nth-child(6),.strategy-formal-facts-table td:nth-child(6) { width:7%; }
.strategy-formal-facts-table th:nth-child(7),.strategy-formal-facts-table td:nth-child(7) { width:8%; }
.strategy-formal-fact-category { display:inline-flex; padding:5px 8px; border-radius:8px; background:#eef2fb; color:#5c6dbb; font-size:12px; font-weight:900; }
.strategy-formal-fact-value { color:#34405a!important; }
.strategy-formal-fact-boundary { color:#a25a5a!important; background:#fffafa; }
.strategy-formal-fact-rewrite { color:#4b6d63!important; background:#fafffc; }
.strategy-formal-fact-version { color:#596985!important; font-weight:900; white-space:nowrap; }
.strategy-formal-fact-version small { display:block; margin-top:2px; color:#99a2b2; font-size:10px; font-weight:700; }
.strategy-formal-facts-table input,.strategy-formal-facts-table select,.strategy-formal-facts-table textarea { width:100%; border:1px solid #ccd5e5; border-radius:9px; background:#fff; color:#34405a; font:inherit; outline:none; }
.strategy-formal-facts-table input,.strategy-formal-facts-table select { min-height:38px; padding:7px 9px; }
.strategy-formal-facts-table textarea { min-height:76px; padding:8px 9px; resize:vertical; }
.strategy-formal-facts-table input:focus,.strategy-formal-facts-table select:focus,.strategy-formal-facts-table textarea:focus { border-color:#8e9ddd; box-shadow:0 0 0 3px rgba(92,107,202,.08); }
.strategy-formal-fact-actions { display:flex; align-items:center; gap:7px; flex-wrap:wrap; }
.strategy-formal-fact-actions button { min-height:34px; padding:6px 11px; border:1px solid #d7deea; border-radius:9px; background:#fff; color:#59667d; font:inherit; font-size:12px; font-weight:900; cursor:pointer; }
.strategy-formal-fact-actions button:hover { border-color:#aeb9d4; color:#4658b8; }
.strategy-formal-fact-actions button.primary-button { border-color:transparent; background:#5968cb; color:#fff; }
.strategy-formal-fact-actions button.is-danger { border-color:#f0caca; background:#fff7f7; color:#bd5b5b; }
.strategy-formal-fact-actions button:disabled { cursor:not-allowed; opacity:.55; }
.strategy-script-actions { display:flex; align-items:center; justify-content:flex-end; gap:9px; padding:14px 16px; border:1px solid #e0e6f1; border-radius:15px; background:#fff; }
.strategy-script-actions>div { display:grid; gap:3px; margin-right:auto; }
.strategy-script-actions strong { color:#37435e; }
.strategy-script-actions small { color:#8993a6; }

.strategy-analysis-summary { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:18px; align-items:center; padding:18px; border:1px solid #dce2f0; border-radius:17px; background:linear-gradient(135deg,#fff,#f3f5ff); }
.strategy-analysis-summary h4 { margin:4px 0 6px; color:#2c3855; font-size:18px; }
.strategy-analysis-summary p { margin:0; color:#78849a; line-height:1.6; }
.strategy-analysis-metrics { display:flex; gap:9px; flex-wrap:wrap; }
.strategy-analysis-metrics button { display:grid; min-width:110px; gap:3px; place-items:center; padding:12px; border:1px solid #d9e0ef; border-radius:13px; background:#fff; cursor:pointer; }
.strategy-analysis-metrics strong { color:#5868d6; font-size:24px; }
.strategy-analysis-metrics span { color:#778299; font-size:12px; font-weight:800; }

.strategy-knowledge-groups { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px; }
.strategy-knowledge-group { display:grid; align-content:start; border:1px solid #e0e6f1; border-radius:16px; overflow:hidden; background:#fff; }
.strategy-knowledge-group>header { display:flex; align-items:center; justify-content:space-between; padding:13px 15px; border-bottom:1px solid #e8ecf3; background:#fafbfe; }
.strategy-knowledge-group>header strong { color:#36425d; }
.strategy-knowledge-group>header span { color:#737fd2; font-size:12px; font-weight:850; }
.strategy-knowledge-group article { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:5px 10px; padding:13px 15px; border-top:1px solid #f0f2f6; }
.strategy-knowledge-group article:first-of-type { border-top:0; }
.strategy-knowledge-group article>div { display:flex; gap:8px; align-items:baseline; min-width:0; }
.strategy-knowledge-group article>div strong { flex:0 0 auto; color:#657087; font-size:12px; }
.strategy-knowledge-group article>div span { color:#303c57; line-height:1.5; }
.strategy-knowledge-group article small { grid-column:1/-1; color:#969fb1; line-height:1.45; }
.strategy-pending-pill { align-self:start; padding:4px 8px; border-radius:999px; background:#fff6dd; color:#a67513; font-size:11px; font-weight:850; }

.strategy-rhythm-timeline { display:grid; gap:0; }
.strategy-rhythm-timeline article { position:relative; display:grid; grid-template-columns:42px minmax(0,1fr); gap:13px; padding:0 0 20px; }
.strategy-rhythm-timeline article:not(:last-child)::before { content:""; position:absolute; left:20px; top:41px; bottom:0; width:2px; background:#dfe4f3; }
.strategy-rhythm-order { z-index:1; display:grid; width:42px; height:42px; place-items:center; border-radius:50%; background:#6674df; color:#fff; font-weight:900; box-shadow:0 5px 16px rgba(76,92,199,.18); }
.strategy-rhythm-timeline article>div { display:grid; gap:8px; padding:14px 16px; border:1px solid #e0e6f1; border-radius:15px; background:#fff; }
.strategy-rhythm-timeline header { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.strategy-rhythm-timeline header strong { color:#303c58; font-size:17px; }
.strategy-rhythm-timeline header span { padding:4px 8px; border-radius:999px; background:#eef1ff; color:#5a68ce; font-size:11px; font-weight:850; }
.strategy-rhythm-timeline header small { margin-left:auto; color:#8e98aa; }
.strategy-rhythm-timeline p { margin:0; color:#667289; line-height:1.55; }
.strategy-rhythm-timeline article>div>small { color:#8994a8; }
.strategy-rhythm-points { display:flex; align-items:center; gap:6px; flex-wrap:wrap; }
.strategy-rhythm-points b { color:#8a94a7; font-size:11px; }
.strategy-rhythm-points span { padding:5px 8px; border-radius:8px; background:#f4f6fa; color:#59657b; font-size:12px; }

.strategy-memory-list { display:grid; gap:10px; }
.strategy-memory-list article { display:grid; grid-template-columns:92px minmax(0,1fr); gap:14px; padding:15px 16px; border:1px solid #e0e6f1; border-radius:15px; background:#fff; }
.strategy-memory-list article.editing { border-color:#c8d2f4; box-shadow:0 8px 22px rgba(67,82,158,.07); }
.strategy-memory-list article>span { align-self:start; padding:6px 9px; border-radius:9px; background:#eef1ff; color:#5867ce; font-size:12px; font-weight:850; text-align:center; }
.strategy-memory-list article>div { display:grid; gap:5px; min-width:0; }
.strategy-memory-list strong { color:#34405b; }
.strategy-memory-list p { margin:0; color:#616d83; line-height:1.6; white-space:pre-wrap; }
.strategy-memory-list small { color:#929caf; }
.strategy-memory-card-actions { grid-column:2; display:flex!important; grid-auto-flow:column; justify-content:end; gap:8px!important; margin-top:3px; }
.strategy-memory-card-actions button,.strategy-memory-edit-actions button,.strategy-memory-edit-preview button { min-height:34px; padding:6px 12px; border:1px solid #d6ddec; border-radius:9px; background:#fff; color:#53617a; font:inherit; font-size:12px; font-weight:850; cursor:pointer; }
.strategy-memory-card-actions button:hover,.strategy-memory-edit-actions button:hover { border-color:#aeb9e5; color:#4658bd; background:#f7f8ff; }
.strategy-memory-card-actions button.danger { border-color:#f0cccc; color:#b6555f; background:#fff8f8; }
.strategy-memory-card-actions button.danger.confirming { border-color:#d96a76; background:#d96a76; color:#fff; }
.strategy-memory-card-actions button:disabled,.strategy-memory-edit-actions button:disabled,.strategy-memory-edit-preview button:disabled { cursor:default; opacity:.55; }
.strategy-memory-edit-panel { grid-column:2; display:grid!important; gap:10px!important; padding-top:12px; border-top:1px solid #edf0f6; }
.strategy-memory-edit-panel label { display:grid; gap:6px; }
.strategy-memory-edit-panel label>span { color:#657088; font-size:12px; font-weight:850; }
.strategy-memory-edit-panel textarea { width:100%; min-height:88px; resize:vertical; padding:10px 12px; border:1px solid #d8deeb; border-radius:10px; background:#fbfcff; color:#39455e; font:inherit; line-height:1.55; outline:none; box-sizing:border-box; }
.strategy-memory-edit-panel textarea:focus { border-color:#8998db; box-shadow:0 0 0 3px rgba(83,101,202,.08); }
.strategy-memory-edit-actions { display:flex!important; grid-auto-flow:column; justify-content:start; gap:8px!important; }
.strategy-memory-edit-preview { display:grid!important; gap:8px!important; padding:11px 12px; border:1px solid #d8e1fb; border-radius:11px; background:#f5f8ff; }
.strategy-memory-edit-preview strong { color:#4e60c8; font-size:13px; }
.strategy-memory-edit-preview p { color:#46536c; font-size:13px; line-height:1.6; }
.strategy-memory-edit-preview button.primary { justify-self:start; border-color:#596bd1; background:#596bd1; color:#fff; }
.strategy-memory-edit-error { color:#bd4f5a!important; font-size:12px!important; }

.strategy-anchor-style-workspace { gap:20px; }
.reusable-anchor-style-workspace{display:grid;grid-template-columns:285px minmax(0,1fr);gap:18px;align-items:start}.reusable-anchor-style-sidebar{display:grid;gap:12px;position:sticky;top:16px;padding:16px;border:1px solid #dbe2f1;border-radius:18px;background:#f8faff}.reusable-anchor-style-sidebar-head{display:flex;align-items:center;justify-content:space-between;gap:8px}.reusable-anchor-style-sidebar-head>div{display:grid;gap:3px}.reusable-anchor-style-sidebar-head span{color:#6877d4;font-size:11px;font-weight:900;letter-spacing:.1em}.reusable-anchor-style-sidebar-head strong{color:#324362;font-size:17px}.reusable-anchor-style-sidebar-head .primary-button{padding:8px 10px;font-size:12px}.reusable-anchor-style-help{margin:0;color:#77839a;font-size:12px;line-height:1.6}.reusable-anchor-style-card{display:grid;grid-template-columns:25px minmax(0,1fr);gap:8px;text-align:left;padding:11px 10px;border:1px solid #dfe5f1;border-radius:12px;background:#fff;color:#43516d;cursor:pointer}.reusable-anchor-style-card.selected{border-color:#6575dc;background:#f0f2ff;box-shadow:0 5px 15px rgba(75,89,190,.12)}.reusable-anchor-style-card-radio{display:grid;place-items:center;width:21px;height:21px;border:1px solid #c6cee1;border-radius:50%;color:#fff;background:#fff;font-weight:900}.reusable-anchor-style-card.selected .reusable-anchor-style-card-radio{border-color:#6172d6;background:#6172d6}.reusable-anchor-style-card>span:last-child{display:grid;gap:3px;min-width:0}.reusable-anchor-style-card strong{font-size:14px}.reusable-anchor-style-card small{overflow:hidden;color:#7c879a;font-size:11px;line-height:1.4;text-overflow:ellipsis;white-space:nowrap}.reusable-anchor-style-card em{color:#9aa3b4;font-size:10px;font-style:normal}.reusable-anchor-style-bind{padding:10px;border:1px solid #6474d8;border-radius:10px;background:#6474d8;color:#fff;font-weight:900;cursor:pointer}.reusable-anchor-style-bind:disabled{opacity:.5;cursor:not-allowed}.reusable-anchor-style-empty{padding:24px 14px;border:1px dashed #cfd8ea;border-radius:12px;background:#fff;color:#8994a9;font-size:13px;line-height:1.65;text-align:center}.reusable-anchor-style-main{display:grid;gap:14px;min-width:0}.reusable-anchor-style-main-head{display:flex;justify-content:space-between;gap:18px;align-items:flex-start;padding:2px 0}.reusable-anchor-style-main-head>div:first-child{display:grid;gap:5px}.reusable-anchor-style-main-head span{color:#6577d8;font-size:11px;font-weight:950;letter-spacing:.12em}.reusable-anchor-style-main-head h3{margin:0;color:#293a5c;font-size:26px}.reusable-anchor-style-main-head p{margin:0;color:#79859b;font-size:14px}.reusable-anchor-style-main-actions{display:flex;gap:8px;flex-wrap:wrap}.danger-button{padding:9px 13px;border:1px solid #efc6c6;border-radius:9px;background:#fff7f7;color:#b54d55;font-weight:850;cursor:pointer}.reusable-anchor-style-edit-row{display:grid;grid-template-columns:220px minmax(0,1fr);gap:10px}.reusable-anchor-style-edit-row input,.reusable-anchor-style-sample-compose input,.reusable-anchor-style-sample-compose textarea{width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid #d4dceb;border-radius:10px;background:#fff;color:#35425e;font:inherit;outline:none}.reusable-anchor-style-sample-compose textarea{resize:vertical;line-height:1.6}.reusable-anchor-style-tabs{display:flex;gap:6px;flex-wrap:wrap;padding:5px;border-bottom:1px solid #dfe5f1}.reusable-anchor-style-tabs button{padding:10px 14px;border:0;border-radius:9px;background:transparent;color:#6c7892;font:inherit;font-size:14px;font-weight:850;cursor:pointer}.reusable-anchor-style-tabs button.active{background:#e9ecff;color:#5164c9}.reusable-anchor-style-tab-panel{display:grid;gap:14px;padding:18px;border:1px solid #dbe2f1;border-radius:16px;background:linear-gradient(135deg,#fff,#f7f8ff)}.reusable-anchor-style-panel-title{display:flex;justify-content:space-between;gap:14px;align-items:flex-start}.reusable-anchor-style-panel-title>div{display:grid;gap:4px}.reusable-anchor-style-panel-title strong{color:#344362;font-size:19px}.reusable-anchor-style-panel-title small{color:#7b879c;line-height:1.6}.reusable-anchor-style-panel-title>span{color:#6674c9;font-size:12px;font-weight:850}.reusable-anchor-style-sample-compose{display:grid;gap:10px;padding:14px;border:1px solid #e1e6f1;border-radius:13px;background:#fbfcff}.reusable-anchor-style-sample-compose .primary-button{justify-self:start}.reusable-anchor-style-sample-list{display:grid;gap:10px}.reusable-anchor-style-sample-list article{display:grid;gap:9px;padding:13px;border:1px solid #e1e6f1;border-radius:13px;background:#fff}.reusable-anchor-style-sample-list header{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}.reusable-anchor-style-sample-list header>div:first-child{display:grid;gap:3px}.reusable-anchor-style-sample-list header small{color:#8a95a9;font-size:11px}.reusable-anchor-style-sample-list header>div:last-child{display:flex;gap:6px;flex-wrap:wrap}.reusable-anchor-style-sample-list button{padding:7px 10px;font-size:11px}.reusable-anchor-style-sample-list details{border-top:1px dashed #e0e5ef;padding-top:8px}.reusable-anchor-style-sample-list summary{color:#6574be;font-size:12px;font-weight:850;cursor:pointer}.reusable-anchor-style-sample-list pre,.reusable-anchor-style-generated-text,.reusable-anchor-style-long-test pre{max-height:360px;margin:8px 0 0;padding:14px;overflow:auto;white-space:pre-wrap;word-break:break-word;border-radius:10px;background:#f7f8fc;color:#4b5871;font:inherit;line-height:1.65}.reusable-anchor-style-thinking{display:flex;align-items:center;gap:10px;padding:13px;border-radius:11px;background:#eef1ff;color:#5a6bd0}.thinking-dot{width:10px;height:10px;border-radius:50%;background:#6475dc;box-shadow:0 0 0 0 rgba(100,117,220,.45);animation:thinking-pulse 1.2s infinite}@keyframes thinking-pulse{70%{box-shadow:0 0 0 10px rgba(100,117,220,0)}}.reusable-anchor-style-thinking small{margin-left:auto;color:#7d89a1}.reusable-anchor-style-analysis-summary{display:grid;gap:7px;padding:15px;border-radius:12px;background:#eef1ff;color:#4659be}.reusable-anchor-style-analysis-summary span{color:#77839a;font-size:12px}.reusable-anchor-style-dimension-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.reusable-anchor-style-dimension-grid article{display:grid;gap:5px;padding:12px;border:1px solid #e0e6f1;border-radius:11px;background:#fff}.reusable-anchor-style-dimension-grid article span{color:#6474c7;font-size:12px;font-weight:900}.reusable-anchor-style-dimension-grid article strong{color:#3c4965;font-size:13px;line-height:1.55}.reusable-anchor-style-dimension-grid article small{color:#8993a6;font-size:11px;line-height:1.5}.reusable-anchor-style-rulebook{border:1px solid #dfe5f1;border-radius:11px;background:#fff}.reusable-anchor-style-rulebook summary{padding:11px;color:#5d6dbe;font-weight:850;cursor:pointer}.reusable-anchor-style-rulebook pre{margin:0;padding:13px;white-space:pre-wrap;color:#59667c;line-height:1.65}.reusable-anchor-style-test-settings{grid-template-columns:minmax(0,1fr) 160px minmax(180px,260px)}.reusable-anchor-style-test-settings label{display:grid;gap:6px;color:#5d6981;font-size:12px;font-weight:850}.reusable-anchor-style-test-settings input{padding:9px;border:1px solid #d4dceb;border-radius:9px;background:#fff;color:#35425e;font:inherit}.reusable-anchor-style-fact-picker{display:flex;gap:7px;align-items:center;flex-wrap:wrap;color:#76839a;font-size:12px}.reusable-anchor-style-fact-picker button{padding:6px 9px;border:1px solid #d7deed;border-radius:8px;background:#fff;color:#62708b;font-size:11px;cursor:pointer}.reusable-anchor-style-fact-picker button.selected{border-color:#6475d8;background:#eef0ff;color:#5063c6}.reusable-anchor-style-test-actions{display:flex;align-items:center;gap:9px;flex-wrap:wrap}.reusable-anchor-style-test-actions label{display:flex;align-items:center;gap:6px;color:#7a869b;font-size:12px}.reusable-anchor-style-test-actions input{width:80px;padding:6px;border:1px solid #d4dceb;border-radius:7px}.reusable-anchor-style-generated-text{max-height:480px;background:#fff;border:1px solid #dfe5f1;color:#35415d;font-size:15px}.reusable-anchor-style-score-row{display:flex;gap:12px;align-items:center;flex-wrap:wrap;color:#61708b;font-size:12px}.reusable-anchor-style-score-row button{margin-left:auto}.reusable-anchor-style-long-test{display:grid;gap:7px}.reusable-anchor-style-long-test header,.reusable-anchor-style-training-list header{display:flex;justify-content:space-between;gap:10px;align-items:center}.reusable-anchor-style-long-test header span,.reusable-anchor-style-training-list header small{color:#8a95a9;font-size:11px}.reusable-anchor-style-training-list{display:grid;gap:9px}.reusable-anchor-style-training-list article{display:flex;justify-content:space-between;gap:10px;align-items:center;padding:10px 12px;border:1px solid #e0e5ef;border-radius:10px;background:#fff}.reusable-anchor-style-training-list article>div:first-child{display:grid;gap:3px}.reusable-anchor-style-training-list article small{color:#8a95a9;font-size:11px}.reusable-anchor-style-training-list article>div:last-child{display:flex;gap:6px}.reusable-anchor-style-training-list button{padding:6px 9px;font-size:11px}.reusable-anchor-style-plugin-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:11px}.reusable-anchor-style-plugin-grid article{display:grid;gap:8px;padding:13px;border:1px solid #e0e6f1;border-radius:12px;background:#fff}.reusable-anchor-style-plugin-grid article>div{display:grid;gap:4px}.reusable-anchor-style-plugin-grid article strong{color:#3a4865}.reusable-anchor-style-plugin-grid article small{color:#7a869b;line-height:1.5}.reusable-anchor-style-plugin-grid button{justify-self:start;padding:7px 10px;border:1px solid #cfd8eb;border-radius:8px;background:#fff;color:#596ac5;font-weight:850;cursor:pointer}.reusable-anchor-style-legacy{margin-top:2px;padding-top:10px;border-top:1px dashed #d8deeb}.reusable-anchor-style-legacy>summary{color:#8691a6;font-size:12px;font-weight:850;cursor:pointer}.reusable-anchor-style-legacy[open]{display:grid;gap:18px}@media(max-width:980px){.reusable-anchor-style-workspace{grid-template-columns:1fr}.reusable-anchor-style-sidebar{position:static}.reusable-anchor-style-test-settings,.reusable-anchor-style-edit-row{grid-template-columns:1fr}.reusable-anchor-style-dimension-grid,.reusable-anchor-style-plugin-grid{grid-template-columns:1fr}}
.strategy-anchor-rulebook { border:1px solid #dce2f0; border-radius:18px; background:linear-gradient(135deg,#fff,#f4f6ff); overflow:hidden; }
.strategy-anchor-rulebook summary { display:flex; flex-wrap:wrap; align-items:center; justify-content:space-between; gap:12px; padding:20px; cursor:pointer; color:#39456b; }
.strategy-anchor-rulebook summary strong { font-size:21px; }
.strategy-anchor-rulebook summary span { color:#6675b5; font-size:15px; }
.strategy-anchor-rulebook-body { padding:0 20px 20px; }
.strategy-anchor-rulebook-body small { margin-left:12px; color:#6675b5; }
.strategy-anchor-rulebook pre { white-space:pre-wrap; overflow-wrap:anywhere; font-family:inherit; line-height:1.8; font-size:16px; color:#39456b; padding:18px; background:#fff; border-radius:12px; border:1px solid #e1e6f2; }
.anchor-analysis-indeterminate { width:45%; animation:anchor-analyzing 1.5s ease-in-out infinite alternate; }
@keyframes anchor-analyzing { from { transform:translateX(0); } to { transform:translateX(100%); } }
@media (prefers-reduced-motion:reduce) { .anchor-analysis-indeterminate { animation:none; } }
.strategy-anchor-core-panel { display:grid; gap:14px; padding:22px; border:1px solid #dce2f6; border-radius:18px; background:linear-gradient(135deg,#fff,#f2f5ff); }
.strategy-anchor-core-panel header { display:flex; align-items:center; justify-content:space-between; gap:16px; flex-wrap:wrap; }
.strategy-anchor-core-panel header strong { font-size:21px; color:#33436c; }
.strategy-anchor-core-panel p { margin:0; font-size:17px; line-height:1.8; color:#65728c; }
.strategy-anchor-core-panel small { font-size:15px; line-height:1.7; color:#71809c; }
.strategy-anchor-core-panel label { display:grid; gap:10px; font-size:17px; font-weight:700; color:#415174; }
.strategy-anchor-test-settings { display:grid; grid-template-columns:minmax(0,1fr) minmax(150px,220px); gap:14px; align-items:end; }
.strategy-anchor-core-panel textarea,.strategy-anchor-core-panel input { width:100%; box-sizing:border-box; padding:14px; border:1px solid #d3dcf2; border-radius:12px; font:inherit; line-height:1.8; background:#fff; color:#2e3a54; }
.strategy-anchor-core-panel .strategy-anchor-test-text { white-space:pre-wrap; color:#273756; padding:18px; border-radius:12px; background:#fff; border:1px solid #e0e6f7; font-size:19px; }
@media(max-width:760px){.strategy-anchor-test-settings{grid-template-columns:1fr}}
.strategy-style-correction-loop{display:grid;gap:12px;margin-top:8px;padding:18px;border:1px solid #bfc9ee;border-radius:15px;background:linear-gradient(135deg,#fff 0%,#f5f3ff 100%)}.strategy-style-correction-loop>header{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.strategy-style-correction-loop>header>div{display:grid;gap:3px}.strategy-style-correction-loop>header span{color:#6675d7;font-size:10px;font-weight:950;letter-spacing:.12em}.strategy-anchor-core-panel .strategy-style-correction-loop>header strong{font-size:18px;color:#30436c}.strategy-style-correction-loop>header em{padding:5px 8px;border-radius:999px;background:#eceffd;color:#6070c8;font-size:11px;font-style:normal;font-weight:900;white-space:nowrap}.strategy-anchor-core-panel .strategy-style-correction-loop>p{font-size:13px;line-height:1.65;color:#6c7890}.strategy-style-correction-loop textarea{min-height:92px}.strategy-style-correction-actions{display:flex;align-items:center;gap:8px;flex-wrap:wrap}.strategy-style-correction-history{display:grid;gap:8px}.strategy-style-correction-history article{display:grid;gap:4px;padding:10px 12px;border-left:3px solid #8998e7;border-radius:8px;background:#fff}.strategy-style-correction-history span{color:#7280c5;font-size:10px;font-weight:900}.strategy-anchor-core-panel .strategy-style-correction-history p{font-size:12px;line-height:1.55;color:#68758d}
.strategy-style-overlay-panel{display:grid;gap:16px;padding:22px;border:1px solid #d8dff4;border-radius:20px;background:linear-gradient(140deg,#fff 0%,#f4f5ff 62%,#f7f2ff 100%)}
.strategy-style-overlay-panel>header{display:flex;align-items:flex-start;justify-content:space-between;gap:18px}.strategy-style-overlay-panel>header>div{display:grid;gap:5px}.strategy-style-overlay-panel>header span{color:#6e71d8;font-size:12px;font-weight:950;letter-spacing:.12em}.strategy-style-overlay-panel>header strong{color:#2c3d66;font-size:24px}.strategy-style-overlay-panel>header p{margin:0;color:#6b7892;line-height:1.65}.strategy-style-overlay-panel>header em{padding:7px 11px;border-radius:999px;background:#eaf8f2;color:#2f8a64;font-size:12px;font-style:normal;font-weight:900;white-space:nowrap}
.strategy-style-plugin-library{display:grid;gap:12px;padding:14px 16px;border:1px solid #dbe2f2;border-radius:15px;background:rgba(255,255,255,.72)}.strategy-style-plugin-library>summary{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;cursor:pointer;list-style:none}.strategy-style-plugin-library>summary::-webkit-details-marker{display:none}.strategy-style-plugin-library>summary>div{display:grid;gap:3px}.strategy-style-plugin-library>summary span{color:#596dd2;font-size:10px;font-weight:950;letter-spacing:.12em}.strategy-style-plugin-library>summary strong{color:#314466;font-size:15px}.strategy-style-plugin-library>summary p{margin:0;color:#78849a;font-size:12px;line-height:1.55}.strategy-style-plugin-library>summary em{color:#66738d;font-size:11px;font-style:normal;font-weight:850;white-space:nowrap}.strategy-style-plugin-library[open] .strategy-style-plugin-grid,.strategy-style-plugin-library[open] .strategy-style-overlay-empty{margin-top:12px}.strategy-style-plugin-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}.strategy-style-plugin-grid article{display:grid;align-content:start;gap:7px;padding:13px;border:1px solid #dce3ef;border-radius:12px;background:#fff}.strategy-style-plugin-grid article.locked{background:#f7f8fb}.strategy-style-plugin-title{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.strategy-style-plugin-title>div{display:grid;gap:2px}.strategy-style-plugin-title span{color:#6978cb;font-size:10px;font-weight:900}.strategy-style-plugin-title strong{color:#354662;font-size:15px}.strategy-style-plugin-title code{padding:3px 6px;border-radius:6px;background:#f1f3fa;color:#75809a;font-size:10px}.strategy-style-plugin-grid p{margin:0;color:#59677e;font-size:12px;line-height:1.55}.strategy-style-plugin-grid small{color:#8791a5;line-height:1.4}.strategy-style-plugin-grid .strategy-style-plugin-reason{color:#a27842}.strategy-style-plugin-grid button{justify-self:start;padding:7px 10px;border:1px solid #cfd8eb;border-radius:8px;background:#fff;color:#5366bd;font-size:11px;font-weight:900;cursor:pointer}.strategy-style-plugin-grid button:disabled{cursor:not-allowed;color:#99a2b5;background:#f4f5f8}
.strategy-style-overlay-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.strategy-style-overlay-list article{display:grid;align-content:start;gap:7px;padding:16px;border:1px solid #dde3f1;border-radius:15px;background:#fff;box-shadow:0 8px 20px rgba(49,63,108,.05)}.strategy-style-overlay-list article.disabled{opacity:.58;background:#f7f8fb}.strategy-style-overlay-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.strategy-style-overlay-card-head>div:first-child{display:grid;gap:3px}.strategy-style-overlay-card-head span{color:#6b74cf;font-size:11px;font-weight:900}.strategy-style-overlay-card-head strong{color:#2f405f;font-size:18px}.strategy-style-overlay-card-head>div:last-child{display:flex;gap:6px}.strategy-style-overlay-card-head button{padding:5px 8px;border:1px solid #dce2ee;border-radius:8px;background:#fff;color:#67738a;font-size:11px;font-weight:850;cursor:pointer}.strategy-style-overlay-list p{margin:0;color:#46546e;line-height:1.6}.strategy-style-overlay-list small{color:#7b879d;line-height:1.5}.strategy-style-overlay-list details{margin-top:4px;padding-top:7px;border-top:1px dashed #e0e5ee}.strategy-style-overlay-list summary{color:#6b75a8;font-size:12px;font-weight:850;cursor:pointer}.strategy-style-overlay-list details p{margin-top:6px;font-size:12px;color:#778298}
.strategy-style-overlay-empty{padding:18px;border:1px dashed #ccd5e8;border-radius:13px;background:rgba(255,255,255,.65);color:#8792a6;text-align:center;line-height:1.6}.strategy-style-overlay-compose{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;align-items:end}.strategy-style-overlay-compose label{display:grid;gap:7px;color:#405170;font-weight:850}.strategy-style-overlay-compose label span small{color:#929bac;font-weight:650}.strategy-style-overlay-compose textarea{width:100%;box-sizing:border-box;padding:12px;border:1px solid #d8dfed;border-radius:11px;background:#fff;color:#34425d;font:inherit;line-height:1.55;resize:vertical}.strategy-style-overlay-strength{grid-column:1/-1}.strategy-style-overlay-strength input{width:100%;accent-color:#6679ea}.strategy-style-overlay-compose>.primary-button{justify-self:start}
.strategy-style-overlay-preview{display:grid;gap:9px;padding:17px;border:1px solid #9eaaed;border-radius:15px;background:#fff;box-shadow:0 10px 25px rgba(75,88,176,.09)}.strategy-style-overlay-preview header{display:flex;justify-content:space-between;gap:12px}.strategy-style-overlay-preview header>div{display:grid;gap:3px}.strategy-style-overlay-preview header span{color:#6976dc;font-size:11px;font-weight:950;letter-spacing:.1em}.strategy-style-overlay-preview header strong{color:#293e69;font-size:19px}.strategy-style-overlay-preview header em{color:#5a6991;font-size:12px;font-style:normal;font-weight:850}.strategy-style-overlay-preview p{margin:0;color:#53627c;line-height:1.55}.strategy-style-overlay-preview small{color:#818ba0}.strategy-style-overlay-preview>div:last-child{display:flex;justify-content:flex-end;gap:8px}
@media(max-width:760px){.strategy-style-overlay-panel{padding:16px}.strategy-style-overlay-panel>header,.strategy-style-plugin-library>summary,.strategy-style-overlay-card-head,.strategy-style-overlay-preview header{align-items:stretch;flex-direction:column}.strategy-style-plugin-grid,.strategy-style-overlay-list,.strategy-style-overlay-compose{grid-template-columns:1fr}.strategy-style-overlay-strength{grid-column:auto}.strategy-style-overlay-card-head>div:last-child{justify-content:flex-start}}
.strategy-anchor-style-summary { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:20px; align-items:center; padding:20px; border:1px solid #dce2f0; border-radius:18px; background:linear-gradient(135deg,#fff,#f4f6ff); }
.strategy-anchor-style-summary>div:first-child { display:grid; gap:7px; min-width:0; }
.strategy-anchor-style-summary>div:first-child>span { color:#6775d3; font-size:13px; font-weight:900; }
.strategy-anchor-style-summary>div:first-child>strong { color:#2e3a56; font-size:19px; line-height:1.65; }
.strategy-anchor-style-summary>div:first-child>small { color:#8791a4; line-height:1.55; }
.strategy-anchor-style-metrics { display:grid; grid-template-columns:repeat(4,minmax(96px,1fr)); gap:10px; }
.strategy-anchor-style-metrics>div { display:grid; place-items:center; gap:4px; min-height:76px; padding:10px 12px; border:1px solid #e1e6f2; border-radius:13px; background:#fff; }
.strategy-anchor-style-metrics strong { color:#5868d6; font-size:25px; line-height:1; }
.strategy-anchor-style-metrics span { color:#7d879b; font-size:12px; font-weight:850; white-space:nowrap; }
.strategy-anchor-style-group-cards { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:14px; }
.strategy-anchor-style-group-cards button { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:7px 10px; min-height:104px; padding:16px 18px; border:1px solid #dfe5f0; border-radius:17px; background:#fff; color:#44506a; text-align:left; cursor:pointer; box-shadow:0 8px 22px rgba(48,63,104,.045); transition:.18s ease; }
.strategy-anchor-style-group-cards button:hover { transform:translateY(-1px); box-shadow:0 12px 26px rgba(48,63,104,.08); }
.strategy-anchor-style-group-cards button.active { border-color:#8995ee; background:#f2f4ff; box-shadow:0 0 0 2px rgba(91,96,221,.08); }
.strategy-anchor-style-group-cards span { color:#3d4962; font-size:18px; font-weight:900; }
.strategy-anchor-style-group-cards strong { color:#5d6ad6; font-size:25px; line-height:1; }
.strategy-anchor-style-group-cards small { grid-column:1/-1; color:#8a94a7; font-size:13px; line-height:1.45; }
.strategy-anchor-style-detail { display:grid; gap:12px; }
.strategy-anchor-style-detail>header { display:flex; align-items:flex-end; justify-content:space-between; gap:14px; padding:0 4px; }
.strategy-anchor-style-detail>header>div { display:grid; gap:4px; }
.strategy-anchor-style-detail>header strong { color:#34405a; font-size:22px; }
.strategy-anchor-style-detail>header span,.strategy-anchor-style-detail>header small { color:#8a95a9; font-size:14px; }
.strategy-anchor-style-table-wrap { overflow:auto; border:1px solid #dfe5f0; border-radius:16px; background:#fff; }
.strategy-anchor-style-table { width:100%; min-width:1220px; border-collapse:collapse; table-layout:fixed; }
.strategy-anchor-style-table th,.strategy-anchor-style-table td { padding:15px 16px; border-bottom:1px solid #e7ebf2; color:#34405a; font-size:16px; line-height:1.6; text-align:left; vertical-align:top; word-break:break-word; }
.strategy-anchor-style-table th { background:#eef2fa; color:#47536b; font-weight:900; }
.strategy-anchor-style-table tbody tr:nth-child(odd) td { background:#fff; }
.strategy-anchor-style-table tbody tr:nth-child(even) td { background:#f7f8fb; }
.strategy-anchor-style-table th:nth-child(1),.strategy-anchor-style-table td:nth-child(1) { width:12%; }
.strategy-anchor-style-table th:nth-child(2),.strategy-anchor-style-table td:nth-child(2) { width:9%; }
.strategy-anchor-style-table th:nth-child(3),.strategy-anchor-style-table td:nth-child(3) { width:31%; }
.strategy-anchor-style-table th:nth-child(4),.strategy-anchor-style-table td:nth-child(4) { width:28%; }
.strategy-anchor-style-table th:nth-child(5),.strategy-anchor-style-table td:nth-child(5) { width:10%; }
.strategy-anchor-style-table th:nth-child(6),.strategy-anchor-style-table td:nth-child(6) { width:10%; }
.strategy-anchor-style-evidence { display:grid; gap:6px; }
.strategy-anchor-style-evidence span { display:block; padding:7px 9px; border-left:3px solid #d9deeb; background:#fafbfc; color:#5d687d; font-size:14px; line-height:1.55; }
.strategy-anchor-confidence,.strategy-anchor-candidate-pill { display:inline-flex; align-items:center; justify-content:center; min-width:68px; padding:5px 8px; border-radius:999px; font-size:12px; font-weight:900; white-space:nowrap; }
.strategy-anchor-confidence.is-high { background:#eaf8f0; color:#2f7d5a; }
.strategy-anchor-confidence.is-medium { background:#fff5e8; color:#a76a1c; }
.strategy-anchor-confidence.is-low { background:#f1f3f7; color:#7b8598; }
.strategy-anchor-candidate-pill { background:#eef1ff; color:#5b68cd; }
.strategy-anchor-style-rule-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:14px; }
.strategy-anchor-style-rule-grid>section { display:grid; align-content:start; gap:9px; padding:17px; border:1px solid #e0e5ef; border-radius:16px; background:#fff; }
.strategy-anchor-style-rule-grid header,.strategy-anchor-style-excluded header { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.strategy-anchor-style-rule-grid header strong,.strategy-anchor-style-excluded header strong { color:#34405b; font-size:17px; }
.strategy-anchor-style-rule-grid header span,.strategy-anchor-style-excluded header span { color:#8c96a9; font-size:12px; font-weight:800; }
.strategy-anchor-style-rule-grid p { margin:0; padding:9px 11px; border-radius:10px; background:#f7f8fc; color:#566278; line-height:1.55; }
.strategy-anchor-style-empty-line { padding:16px 10px; color:#929cad; text-align:center; }
.strategy-anchor-style-excluded { display:grid; gap:10px; padding:17px; border:1px solid #eadfcb; border-radius:16px; background:#fffaf2; }
.strategy-anchor-style-excluded>div { display:flex; flex-wrap:wrap; gap:8px; }
.strategy-anchor-style-excluded>div span { padding:6px 9px; border-radius:9px; background:#fff; color:#876b3e; font-size:13px; line-height:1.45; }

.strategy-fullshow-workspace {
  grid-template-columns:minmax(0,1fr);
  gap:20px;
  min-width:0;
  max-width:100%;
}
.strategy-fullshow-workspace>* {
  min-width:0;
  max-width:100%;
  box-sizing:border-box;
}
.strategy-fullshow-head {
  position:relative;
  align-items:flex-start;
  overflow:hidden;
  padding:24px 26px 26px;
  border:1px solid #dce3f4;
  border-radius:22px;
  background:
    radial-gradient(circle at 88% 0%,rgba(118,132,244,.16),transparent 34%),
    linear-gradient(180deg,#fbfcff 0%,#f3f6ff 100%);
  box-shadow:0 14px 34px rgba(54,72,145,.08);
}
.strategy-fullshow-head::before {
  content:"";
  position:absolute;
  top:0;
  left:0;
  width:5px;
  height:100%;
  background:linear-gradient(180deg,#7d8bf2 0%,#5366db 100%);
  opacity:.9;
}
.strategy-fullshow-main-actions {
  display:grid;
  grid-template-columns:minmax(220px,1fr) minmax(0,520px);
  gap:18px;
  align-items:center;
  width:100%;
  min-width:0;
  box-sizing:border-box;
  padding:15px 16px;
  border:1px solid #d7def2;
  border-radius:16px;
  background:
    linear-gradient(135deg,rgba(255,255,255,.96),rgba(243,246,255,.96)),
    #fff;
  box-shadow:0 9px 24px rgba(67,82,155,.07);
}
.strategy-fullshow-main-actions-copy {
  display:grid;
  gap:4px;
  min-width:0;
}
.strategy-fullshow-main-actions-copy>span {
  color:#7a86a5;
  font-size:10px;
  font-weight:950;
  letter-spacing:.11em;
}
.strategy-fullshow-main-actions-copy>strong {
  overflow:hidden;
  color:#35425e;
  font-size:16px;
  font-weight:950;
  text-overflow:ellipsis;
  white-space:nowrap;
}
.strategy-fullshow-main-actions-copy>small {
  color:#8994aa;
  font-size:11px;
  line-height:1.5;
}
.strategy-fullshow-main-actions-buttons {
  display:grid;
  grid-template-columns:repeat(2,minmax(0,1fr));
  gap:10px;
  justify-self:end;
  width:100%;
  min-width:0;
  max-width:520px;
}
.strategy-fullshow-main-actions-buttons button {
  display:grid;
  align-content:center;
  justify-items:start;
  width:100%;
  min-width:0;
  box-sizing:border-box;
  min-height:60px;
  padding:10px 16px;
  border:1px solid #cbd4ee;
  border-radius:14px;
  background:linear-gradient(180deg,#fff,#f7f9ff);
  color:#5363c9;
  text-align:left;
  cursor:pointer;
  box-shadow:0 6px 15px rgba(71,87,170,.06);
  transition:transform .16s ease,border-color .16s ease,box-shadow .16s ease;
}
.strategy-fullshow-main-actions-buttons button:hover:not(:disabled) {
  transform:translateY(-1px);
  border-color:#9eaaf0;
  box-shadow:0 10px 22px rgba(71,87,170,.12);
}
.strategy-fullshow-main-actions-buttons button>span {
  font-size:13px;
  font-weight:950;
}
.strategy-fullshow-main-actions-buttons button>small {
  margin-top:3px;
  color:#929bb1;
  font-size:10px;
  font-weight:750;
}
.strategy-fullshow-main-actions-buttons button.primary {
  border-color:#6374e0;
  background:linear-gradient(135deg,#7081ee 0%,#5265d8 100%);
  color:#fff;
  box-shadow:0 10px 24px rgba(72,88,198,.22);
}
.strategy-fullshow-main-actions-buttons button.primary>small {
  color:rgba(255,255,255,.76);
}
.strategy-fullshow-main-actions-buttons button:disabled {
  cursor:not-allowed;
  opacity:.5;
  transform:none;
  box-shadow:none;
}
.strategy-fullshow-restore-banner { display:flex; align-items:center; justify-content:space-between; gap:18px; padding:13px 16px; border:1px solid #cfd7ff; border-radius:14px; background:linear-gradient(135deg,#f5f7ff 0%,#eef2ff 100%); box-shadow:0 7px 20px rgba(77,92,190,.08); }
.strategy-fullshow-restore-banner>div:first-child { display:grid; gap:4px; min-width:0; }
.strategy-fullshow-restore-banner strong { color:#4656bd; font-size:13px; font-weight:950; }
.strategy-fullshow-restore-banner span { color:#707d96; font-size:11px; line-height:1.5; }
.strategy-fullshow-restore-banner .strategy-fullshow-restore-legacy { color:#a36f18; }
.strategy-fullshow-restore-banner.is-loading { border-style:dashed; opacity:.84; }
.strategy-fullshow-restore-status { display:grid; justify-items:end; gap:4px; flex:0 0 auto; }
.strategy-fullshow-restore-status b { padding:5px 8px; border-radius:999px; background:#e5f4eb; color:#347a58; font-size:10px; font-weight:950; }
.strategy-fullshow-restore-status b.dirty { background:#fff1d8; color:#a66b04; }
.strategy-fullshow-restore-status em { color:#8b95a8; font-size:10px; font-style:normal; white-space:nowrap; }
.strategy-fullshow-title-block { display:grid; gap:15px; width:100%; min-width:0; }
.strategy-fullshow-title-block h3,.strategy-fullshow-title-block p { margin:0; }
.strategy-fullshow-title-block h3 {
  position:relative;
  width:max-content;
  max-width:100%;
  padding-bottom:12px;
  color:#26344f;
  font-size:34px;
  font-weight:950;
  line-height:1.15;
  letter-spacing:.015em;
}
.strategy-fullshow-title-block h3::after {
  content:"";
  position:absolute;
  left:0;
  bottom:0;
  width:92px;
  height:4px;
  border-radius:999px;
  background:linear-gradient(90deg,#6676e6 0%,#9ca8ff 100%);
  box-shadow:0 3px 10px rgba(90,104,222,.24);
}
.strategy-fullshow-title-block p {
  max-width:1080px;
  color:#7b879d;
  font-size:15px;
  line-height:1.8;
}
.strategy-fullshow-title-action { display:flex; align-items:center; justify-content:flex-start; }
.strategy-fullshow-title-action .primary-button {
  position:relative;
  min-width:320px;
  min-height:58px;
  padding:0 30px;
  overflow:hidden;
  border:1px solid rgba(91,106,220,.75)!important;
  border-radius:20px;
  background:linear-gradient(135deg,#7381f2 0%,#5f6fe2 52%,#4e61d3 100%)!important;
  box-shadow:
    0 15px 30px rgba(75,91,202,.28),
    inset 0 1px 0 rgba(255,255,255,.26);
  color:#fff!important;
  font-size:18px;
  font-weight:950;
  letter-spacing:.02em;
  transition:transform .18s ease,box-shadow .18s ease,filter .18s ease;
}
.strategy-fullshow-title-action .primary-button::before {
  content:"";
  position:absolute;
  top:5px;
  left:9%;
  right:9%;
  height:42%;
  border-radius:999px;
  background:linear-gradient(180deg,rgba(255,255,255,.22),rgba(255,255,255,.02));
  pointer-events:none;
}
.strategy-fullshow-title-action .primary-button::after {
  content:"";
  position:absolute;
  inset:0;
  background:linear-gradient(110deg,transparent 16%,rgba(255,255,255,.28) 43%,transparent 70%);
  transform:translateX(-125%);
  transition:transform .55s ease;
  pointer-events:none;
}
.strategy-fullshow-title-action .primary-button:hover:not(:disabled) {
  transform:translateY(-2px);
  filter:brightness(1.04);
  box-shadow:
    0 19px 38px rgba(75,91,202,.34),
    0 0 0 4px rgba(96,111,226,.10),
    inset 0 1px 0 rgba(255,255,255,.28);
}
.strategy-fullshow-title-action .primary-button:hover:not(:disabled)::after { transform:translateX(125%); }
.strategy-fullshow-title-action .primary-button:active:not(:disabled) { transform:translateY(0); }
.strategy-fullshow-title-action .primary-button:disabled { opacity:.58; box-shadow:none; cursor:not-allowed; }
.strategy-fullshow-guide { display:grid; grid-template-columns:minmax(0,1fr); gap:12px; min-width:0; padding:16px 18px; border:1px solid #dfe4f5; border-radius:16px; background:linear-gradient(180deg,#fbfcff 0%,#f7f9ff 100%); }
.strategy-fullshow-guide-title { display:flex; align-items:center; justify-content:space-between; gap:18px; min-width:0; max-width:100%; box-sizing:border-box; }
.strategy-fullshow-guide-title>div { display:grid; gap:4px; min-width:0; }
.strategy-fullshow-guide-title strong { color:#3d4862; font-size:15px; }
.strategy-fullshow-guide-title span { color:#657189; font-size:12px; line-height:1.5; }
.strategy-fullshow-guide-title em { flex:0 0 auto; padding:5px 9px; border-radius:999px; background:#eef1ff; color:#5c69d4; font-size:11px; font-style:normal; font-weight:900; }
.strategy-fullshow-guide-steps { display:flex; align-items:stretch; gap:8px; width:100%; min-width:0; max-width:100%; overflow-x:auto; box-sizing:border-box; padding-bottom:2px; }
.strategy-fullshow-guide-steps article { display:grid; grid-template-columns:34px minmax(150px,1fr) auto; gap:10px; align-items:center; min-width:220px; flex:1 1 0; padding:11px 12px; border:1px solid #e3e7f0; border-radius:13px; background:#fff; }
.strategy-fullshow-guide-steps article>i { display:grid; width:32px; height:32px; place-items:center; border-radius:10px; background:#f0f2f7; color:#7f899a; font-size:12px; font-style:normal; font-weight:950; }
.strategy-fullshow-guide-steps article>div { display:grid; gap:3px; min-width:0; }
.strategy-fullshow-guide-steps article strong { color:#46516a; font-size:13px; }
.strategy-fullshow-guide-steps article span { color:#8b95a8; font-size:11px; line-height:1.4; }
.strategy-fullshow-guide-steps article>b { align-self:start; padding:4px 6px; border-radius:999px; background:#f2f4f8; color:#8e97a8; font-size:10px; white-space:nowrap; }
.strategy-fullshow-guide-steps article.is-active { border-color:#9aa5f0; background:#f3f5ff; box-shadow:0 0 0 2px rgba(95,110,217,.07); }
.strategy-fullshow-guide-steps article.is-active>i { background:#6674df; color:#fff; }
.strategy-fullshow-guide-steps article.is-active>b { background:#e3e7ff; color:#5361c9; }
.strategy-fullshow-guide-steps article.is-done { border-color:#cde5d7; background:#f7fcf9; }
.strategy-fullshow-guide-steps article.is-done>i { background:#4aa476; color:#fff; }
.strategy-fullshow-guide-steps article.is-done>b { background:#e5f5eb; color:#357b58; }
.strategy-fullshow-guide-arrow { display:grid; flex:0 0 18px; place-items:center; align-self:center; color:#a2abc0; font-size:17px; font-weight:900; }
.strategy-fullshow-grid { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:16px; }
.strategy-fullshow-output-grid { grid-template-columns:minmax(0,1.25fr) minmax(320px,.75fr); }
.strategy-fullshow-panel { display:grid; align-content:start; gap:16px; min-width:0; padding:18px; border:1px solid #dfe5f0; border-radius:18px; background:#fff; box-shadow:0 8px 22px rgba(48,63,104,.045); }
.strategy-fullshow-panel>header { display:flex; align-items:center; justify-content:space-between; gap:14px; padding-bottom:12px; border-bottom:1px solid #e9edf4; }
.strategy-fullshow-panel>header>div { display:flex; align-items:center; gap:9px; }
.strategy-fullshow-panel>header>div>span { display:grid; width:30px; height:30px; place-items:center; border-radius:9px; background:#eef1ff; color:#5d6ad6; font-size:12px; font-weight:900; }
.strategy-fullshow-panel>header strong { color:#34405a; font-size:18px; }
.strategy-fullshow-panel>header small { color:#8b95a8; font-size:12px; text-align:right; }
.strategy-fullshow-panel-head-actions { display:flex!important; align-items:center!important; justify-content:flex-end; gap:10px!important; margin-left:auto; }
.strategy-fullshow-structure-button {
  position:relative;
  min-height:46px;
  padding:0 22px;
  overflow:hidden;
  border:1px solid rgba(105,118,233,.65);
  border-radius:14px;
  background:linear-gradient(135deg,#6f7df0 0%,#5365dd 55%,#4457d0 100%);
  box-shadow:0 10px 24px rgba(74,89,202,.24), inset 0 1px 0 rgba(255,255,255,.22);
  color:#fff;
  font-size:16px;
  font-weight:950;
  letter-spacing:.02em;
  white-space:nowrap;
  cursor:pointer;
  transition:transform .18s ease, box-shadow .18s ease, filter .18s ease;
}
.strategy-fullshow-structure-button::after {
  content:"";
  position:absolute;
  inset:0;
  background:linear-gradient(110deg,transparent 15%,rgba(255,255,255,.26) 42%,transparent 68%);
  transform:translateX(-120%);
  transition:transform .45s ease;
  pointer-events:none;
}
.strategy-fullshow-structure-button:hover {
  transform:translateY(-2px);
  filter:brightness(1.04);
  box-shadow:0 14px 30px rgba(74,89,202,.32), 0 0 0 3px rgba(96,111,226,.10), inset 0 1px 0 rgba(255,255,255,.28);
}
.strategy-fullshow-structure-button:hover::after { transform:translateX(120%); }
.strategy-fullshow-structure-button:active { transform:translateY(0); box-shadow:0 7px 16px rgba(74,89,202,.22); }
.strategy-fullshow-field { display:grid; gap:9px; }
.strategy-fullshow-field>span,.strategy-fullshow-inline-fields label>span { color:#657189; font-size:13px; font-weight:900; }
.strategy-fullshow-field-title { display:flex; align-items:center; justify-content:space-between; gap:16px; }
.strategy-fullshow-field-title>span { color:#657189; font-size:13px; font-weight:900; }
.strategy-fullshow-field-title>strong { color:#5362cf; font-size:15px; font-weight:950; }
.strategy-fullshow-duration-slider { display:grid; gap:10px; padding:10px 4px 0; }
.strategy-fullshow-duration-slider input[type="range"] {
  width:100%;
  height:10px;
  margin:0;
  appearance:none;
  border-radius:999px;
  outline:none;
  background:linear-gradient(
    90deg,
    #6674df 0%,
    #6674df var(--duration-progress),
    #e7eaf4 var(--duration-progress),
    #e7eaf4 100%
  );
  cursor:pointer;
}
.strategy-fullshow-duration-slider input[type="range"]::-webkit-slider-thumb {
  width:26px;
  height:26px;
  appearance:none;
  border:4px solid #fff;
  border-radius:50%;
  background:#5d6bdb;
  box-shadow:0 4px 12px rgba(73,88,196,.30),0 0 0 2px rgba(93,107,219,.12);
}
.strategy-fullshow-duration-slider input[type="range"]::-moz-range-thumb {
  width:18px;
  height:18px;
  border:4px solid #fff;
  border-radius:50%;
  background:#5d6bdb;
  box-shadow:0 4px 12px rgba(73,88,196,.30),0 0 0 2px rgba(93,107,219,.12);
}
.strategy-fullshow-duration-ticks { display:grid; grid-template-columns:repeat(4,1fr); color:#8b95a7; font-size:11px; font-weight:800; }
.strategy-fullshow-duration-ticks span:nth-child(1) { text-align:left; }
.strategy-fullshow-duration-ticks span:nth-child(2),.strategy-fullshow-duration-ticks span:nth-child(3) { text-align:center; }
.strategy-fullshow-duration-ticks span:nth-child(4) { text-align:right; }
.strategy-fullshow-inline-fields { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px; }
.strategy-fullshow-inline-fields label { display:grid; gap:7px; }
.strategy-fullshow-inline-fields label>div { display:grid; grid-template-columns:1fr auto; align-items:center; overflow:hidden; min-height:44px; border:1px solid #dfe5ef; border-radius:11px; background:#fff; }
.strategy-fullshow-inline-fields input,.strategy-fullshow-inline-fields select { width:100%; min-height:42px; border:0; outline:0; padding:0 12px; background:transparent; color:#34405a; font:inherit; font-weight:800; }
.strategy-fullshow-inline-fields label>div em { padding-right:12px; color:#929bad; font-size:12px; font-style:normal; }
.strategy-fullshow-inline-fields select { border:1px solid #dfe5ef; border-radius:11px; }
.strategy-fullshow-options { display:grid; gap:8px; }
.strategy-fullshow-options label { display:grid; grid-template-columns:auto minmax(0,1fr); gap:10px; align-items:start; padding:11px 12px; border:1px solid #edf0f5; border-radius:12px; background:#fafbfe; cursor:pointer; }
.strategy-fullshow-options input { width:18px; height:18px; margin-top:2px; accent-color:#5f6ed9; }
.strategy-fullshow-options label>span { display:grid; gap:2px; }
.strategy-fullshow-options strong { color:#46516a; font-size:14px; }
.strategy-fullshow-options small { color:#9099ab; font-size:12px; line-height:1.45; }
.strategy-fullshow-source-cards { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:10px; }
.strategy-fullshow-source-cards button { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:4px 10px; min-height:90px; padding:14px; border:1px solid #e1e6ef; border-radius:14px; background:#fafbfe; text-align:left; cursor:pointer; }
.strategy-fullshow-source-cards button:hover { border-color:#c9d0ff; background:#f5f6ff; }
.strategy-fullshow-source-cards span { color:#536078; font-size:14px; font-weight:900; }
.strategy-fullshow-source-cards strong { color:#5e6bd8; font-size:25px; line-height:1; }
.strategy-fullshow-source-cards small { grid-column:1/-1; color:#9099aa; font-size:12px; line-height:1.45; }
.strategy-fullshow-guardrails { display:grid; gap:8px; }
.strategy-fullshow-guardrails>div { display:grid; grid-template-columns:82px minmax(0,1fr); gap:10px; align-items:start; padding:10px 12px; border-radius:11px; background:#f7f8fb; }
.strategy-fullshow-guardrails span { color:#7d879a; font-size:12px; font-weight:900; }
.strategy-fullshow-guardrails strong { color:#455169; font-size:13px; line-height:1.55; }
.strategy-fullshow-empty,.strategy-fullshow-output-empty { display:grid; gap:7px; min-height:150px; place-content:center; padding:26px; border:1px dashed #ccd4e5; border-radius:15px; background:#fafbfe; color:#8a94a7; text-align:center; }
.strategy-fullshow-empty strong,.strategy-fullshow-output-empty strong { color:#3e4962; font-size:17px; }
.strategy-fullshow-empty span,.strategy-fullshow-output-empty span { max-width:700px; line-height:1.6; }
.strategy-fullshow-rounds { display:grid; gap:9px; }
.strategy-fullshow-rounds article { display:grid; grid-template-columns:46px minmax(180px,.7fr) minmax(330px,1.4fr) auto; gap:12px; align-items:center; padding:12px 14px; border:1px solid #e5e9f1; border-radius:13px; background:#fbfcfe; }
.strategy-fullshow-round-index { display:grid; width:38px; height:38px; place-items:center; border-radius:11px; background:#6674df; color:#fff; font-size:13px; font-weight:900; }
.strategy-fullshow-rounds article>div:nth-child(2) { display:grid; gap:2px; }
.strategy-fullshow-rounds article>div:nth-child(2)>span { color:#6674d5; font-size:12px; font-weight:900; }
.strategy-fullshow-rounds article>div:nth-child(2)>strong { color:#3e4961; font-size:15px; }
.strategy-fullshow-rounds article>div:nth-child(2)>small { color:#949daf; font-size:11px; }
.strategy-fullshow-round-flow { display:flex; align-items:center; gap:6px; flex-wrap:wrap; }
.strategy-fullshow-round-flow span { padding:5px 7px; border-radius:8px; background:#f0f2f7; color:#5d687d; font-size:12px; font-weight:800; }
.strategy-fullshow-round-flow i { color:#a4acbc; font-style:normal; }
.strategy-fullshow-rounds article>em { padding:5px 8px; border-radius:999px; background:#eef7f2; color:#3d8160; font-size:11px; font-style:normal; font-weight:900; white-space:nowrap; }
.strategy-fullshow-more-rounds { padding:10px 12px; border-radius:11px; background:#f4f6fb; color:#7d879a; font-size:12px; text-align:center; }
.strategy-fullshow-status { display:flex!important; align-items:center!important; gap:6px!important; color:#8993a5; font-size:12px; font-weight:850; }
.strategy-fullshow-status i { width:8px; height:8px; border-radius:50%; background:#b7bfcc; }
.strategy-fullshow-status.ready { color:#2f7d5a; }
.strategy-fullshow-status.ready i { background:#47a978; box-shadow:0 0 0 4px rgba(71,169,120,.10); }
.strategy-fullshow-progress { display:grid; gap:10px; padding:14px 18px; border:1px solid #dbe2f8; border-radius:14px; background:#f8faff; box-shadow:0 10px 24px rgba(77,92,178,.06); }
.strategy-fullshow-progress-head,.strategy-fullshow-progress-foot { display:flex; align-items:center; justify-content:space-between; gap:18px; }
.strategy-fullshow-progress-head>div { display:flex; align-items:center; gap:12px; min-width:0; }
.strategy-fullshow-progress-head strong { color:#3f4d6b; font-size:14px; }
.strategy-fullshow-progress-head span { color:#6673ce; font-size:13px; font-weight:850; }
.strategy-fullshow-progress-head em { color:#5e6bd8; font-size:18px; font-style:normal; font-weight:950; }
.strategy-fullshow-progress-track { position:relative; height:9px; overflow:hidden; border-radius:999px; background:#e9edfb; }
.strategy-fullshow-progress-track i { display:block; height:100%; border-radius:inherit; background:linear-gradient(90deg,#7d88eb 0%,#5f6fdf 100%); transition:width .45s ease; }
.strategy-fullshow-progress-foot span { color:#6e7890; font-size:12px; line-height:1.55; }
.strategy-fullshow-progress-foot small { color:#9aa3b5; font-size:11px; white-space:nowrap; }
.strategy-fullshow-progress.done { border-color:#c8e5d5; background:#f6fcf8; }
.strategy-fullshow-progress.done .strategy-fullshow-progress-head span,.strategy-fullshow-progress.done .strategy-fullshow-progress-head em { color:#31805c; }
.strategy-fullshow-progress.done .strategy-fullshow-progress-track i { background:linear-gradient(90deg,#62b88a 0%,#3d996d 100%); }
.strategy-fullshow-progress.failed { border-color:#f0c9c9; background:#fff8f8; }
.strategy-fullshow-progress.failed .strategy-fullshow-progress-head span,.strategy-fullshow-progress.failed .strategy-fullshow-progress-head em { color:#b95555; }
.strategy-fullshow-progress.failed .strategy-fullshow-progress-track i { background:linear-gradient(90deg,#db8c8c 0%,#c85f5f 100%); }
.strategy-fullshow-formal-summary { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:12px 14px; border:1px solid #e1e6f0; border-radius:12px; background:#fafbfe; }
.strategy-fullshow-formal-summary>div { display:grid; gap:3px; min-width:0; }
.strategy-fullshow-formal-summary strong { color:#46516a; font-size:13px; }
.strategy-fullshow-formal-summary span { color:#7d879b; font-size:12px; line-height:1.5; }
.strategy-fullshow-formal-summary em { flex:0 0 auto; padding:5px 8px; border-radius:999px; background:#f0f2f7; color:#8a94a6; font-size:10px; font-style:normal; font-weight:900; }
.strategy-fullshow-formal-summary.ready { border-color:#bfe0cc; background:#f4fbf7; }
.strategy-fullshow-formal-summary.ready strong { color:#327b58; }
.strategy-fullshow-formal-summary.ready em { background:#e1f3e8; color:#327b58; }
.strategy-fullshow-variant-tabs { display:grid; grid-template-columns:repeat(5,minmax(0,1fr)); gap:8px; }
.strategy-fullshow-variant-tabs button {
  position:relative;
  display:grid;
  gap:4px;
  min-width:0;
  min-height:78px;
  padding:11px 12px;
  border:1px solid #e1e6ef;
  border-radius:12px;
  background:linear-gradient(180deg,#fcfdff 0%,#f7f9fc 100%);
  color:#4d5870;
  text-align:left;
  cursor:pointer;
  box-shadow:0 4px 10px rgba(46,61,101,.035);
  transition:transform .16s ease,border-color .16s ease,box-shadow .16s ease,background .16s ease;
}
.strategy-fullshow-variant-tabs button strong { color:#40506c; font-size:15px; }
.strategy-fullshow-variant-tabs button span { overflow:hidden; color:#818ca0; font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.strategy-fullshow-variant-tabs button em { color:#2f7d5a; font-size:10px; font-style:normal; font-weight:900; }
.strategy-fullshow-variant-tabs button.failed em { color:#b36b18; }
.strategy-fullshow-variant-tabs button:hover {
  transform:translateY(-3px);
  border-color:#9ea9e9;
  background:linear-gradient(180deg,#f8f9ff 0%,#eef2ff 100%);
  box-shadow:0 10px 22px rgba(69,84,169,.14),0 0 0 2px rgba(95,110,217,.06);
}
.strategy-fullshow-variant-tabs button:hover strong { color:#4e60c9; }
.strategy-fullshow-variant-tabs button.active {
  transform:translateY(-2px);
  border:2px solid #6676df;
  background:linear-gradient(180deg,#eef1ff 0%,#e4e9ff 100%);
  box-shadow:
    0 12px 26px rgba(77,94,195,.20),
    0 0 0 4px rgba(99,116,223,.10),
    inset 0 1px 0 rgba(255,255,255,.88);
}
.strategy-fullshow-variant-tabs button.active strong { color:#4055bf; font-weight:950; }
.strategy-fullshow-variant-tabs button.formal {
  border-color:#72b58e;
  background:linear-gradient(180deg,#f3fbf6 0%,#eaf7ef 100%);
  box-shadow:0 0 0 2px rgba(74,164,118,.08);
}
.strategy-fullshow-variant-tabs button.formal strong,.strategy-fullshow-variant-tabs button.formal em { color:#347c59; }
.strategy-fullshow-variant-tabs button.formal:hover {
  border-color:#59a97b;
  background:linear-gradient(180deg,#eefaf3 0%,#e2f4e9 100%);
  box-shadow:0 10px 22px rgba(65,142,99,.15),0 0 0 2px rgba(74,164,118,.08);
}
.strategy-fullshow-variant-tabs button.formal.active {
  border:2px solid #4aa476;
  background:
    radial-gradient(circle at 85% 12%,rgba(255,255,255,.72),transparent 30%),
    linear-gradient(180deg,#ecf9f1 0%,#dff2e7 100%);
  box-shadow:
    0 12px 26px rgba(56,131,87,.20),
    0 0 0 4px rgba(74,164,118,.11),
    inset 0 1px 0 rgba(255,255,255,.90);
}
.strategy-fullshow-current-badge {
  position:absolute;
  top:8px;
  right:8px;
  padding:3px 6px;
  border-radius:999px;
  background:#6674df;
  color:#fff;
  font-size:9px;
  font-weight:950;
  line-height:1.2;
  box-shadow:0 4px 10px rgba(72,89,190,.20);
}
.strategy-fullshow-variant-tabs button.formal.active .strategy-fullshow-current-badge { background:#4aa476; }
.strategy-fullshow-script-card { display:grid; gap:14px; min-width:0; }
.strategy-fullshow-script-card>header { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:16px; align-items:end; }
.strategy-fullshow-script-card>header>div:first-child { display:grid; gap:3px; }
.strategy-fullshow-script-card>header>div:first-child span { color:#6573ce; font-size:12px; font-weight:900; }
.strategy-fullshow-script-card>header>div:first-child strong { color:#34405b; font-size:19px; }
.strategy-fullshow-script-side { display:grid; gap:8px; justify-items:end; }
.strategy-fullshow-script-metrics { display:flex; gap:7px; flex-wrap:wrap; justify-content:flex-end; }
.strategy-fullshow-script-metrics span { padding:5px 8px; border-radius:999px; background:#f2f4f8; color:#6f7a8e; font-size:11px; font-weight:800; }
.strategy-fullshow-script-actions { display:flex; justify-content:flex-end; gap:7px; }
.strategy-fullshow-script-actions button { min-height:34px; padding:0 12px; border:1px solid #dce2ee; border-radius:9px; background:#fff; color:#58647b; font-size:12px; font-weight:850; cursor:pointer; }
.strategy-fullshow-script-actions button:hover { border-color:#aeb8ef; background:#f7f8ff; color:#5360c7; }
.strategy-fullshow-script-actions button.primary { border-color:#6875df; background:#6674df; color:#fff; }
.strategy-fullshow-script-actions button.formal { border-color:#77b993; color:#347c59; }
.strategy-fullshow-script-actions button.formal.selected { border-color:#4aa476; background:#4aa476; color:#fff; }
.strategy-fullshow-script-actions button:disabled { cursor:not-allowed; opacity:.58; }
.strategy-fullshow-audit { display:grid; gap:7px; padding:12px 14px; border:1px solid #ebc98d; border-radius:12px; background:#fff8ee; }
.strategy-fullshow-audit.passed { border-color:#b9e0c9; background:#f2fbf6; }
.strategy-fullshow-audit strong { color:#9a681d; font-size:13px; }
.strategy-fullshow-audit.passed strong { color:#2f7d5a; }
.strategy-fullshow-audit>span,.strategy-fullshow-audit li { color:#6c7688; font-size:12px; line-height:1.55; }
.strategy-fullshow-audit ul { display:grid; gap:4px; margin:0; padding-left:18px; }
.strategy-fullshow-audit li.is-error { color:#b54b4b; }
.strategy-fullshow-audit li.is-warning { color:#a76a1c; }
.strategy-fullshow-script-text { max-height:680px; overflow:auto; padding:18px; border:1px solid #e3e7ef; border-radius:14px; background:#fbfcfe; color:#354158; font-size:16px; line-height:1.95; white-space:pre-wrap; word-break:break-word; }
.strategy-fullshow-script-editor { display:grid; gap:10px; }
.strategy-fullshow-script-editor-tip { display:grid; gap:3px; padding:11px 13px; border:1px solid #dce2f6; border-radius:11px; background:#f7f9ff; }
.strategy-fullshow-script-editor-tip strong { color:#5360c7; font-size:13px; }
.strategy-fullshow-script-editor-tip span { color:#7c879c; font-size:12px; line-height:1.55; }
.strategy-fullshow-script-editor textarea { width:100%; min-height:440px; resize:vertical; box-sizing:border-box; padding:18px; border:1px solid #aeb8ee; border-radius:14px; outline:0; background:#fff; color:#354158; font:inherit; font-size:16px; line-height:1.95; box-shadow:0 0 0 3px rgba(102,116,223,.06); }
.strategy-fullshow-script-editor textarea:focus { border-color:#6976df; box-shadow:0 0 0 3px rgba(102,116,223,.10); }
.strategy-fullshow-fact-menu-layer { position:fixed; inset:0; z-index:6200; }
.strategy-fullshow-fact-menu { position:fixed; display:grid; gap:7px; width:180px; padding:8px; border:1px solid #d9e0f2; border-radius:12px; background:rgba(255,255,255,.98); box-shadow:0 16px 36px rgba(38,52,101,.20),0 4px 10px rgba(38,52,101,.10); backdrop-filter:blur(12px); }
.strategy-fullshow-fact-menu button { display:flex; align-items:center; gap:8px; min-height:38px; padding:0 10px; border:0; border-radius:9px; background:linear-gradient(135deg,#6f7df0,#5365d8); color:#fff; font-weight:900; cursor:pointer; }
.strategy-fullshow-fact-menu button span { font-size:18px; line-height:1; }
.strategy-fullshow-fact-menu small { display:block; max-height:44px; overflow:hidden; padding:0 5px 3px; color:#7d8799; font-size:11px; line-height:1.45; word-break:break-word; }
.strategy-fullshow-fact-dialog-overlay { position:fixed; inset:0; z-index:6300; display:grid; place-items:center; padding:24px; background:rgba(25,38,70,.34); backdrop-filter:blur(4px); }
.strategy-fullshow-fact-dialog { width:min(620px,94vw); display:grid; gap:14px; padding:20px; border:1px solid rgba(218,225,244,.92); border-radius:20px; background:#fbfcff; box-shadow:0 28px 80px rgba(27,42,89,.28); }
.strategy-fullshow-fact-dialog>header { display:flex; align-items:center; justify-content:space-between; gap:16px; }
.strategy-fullshow-fact-dialog>header>div { display:grid; gap:3px; }
.strategy-fullshow-fact-dialog>header span { color:#7d88a0; font-size:11px; font-weight:850; }
.strategy-fullshow-fact-dialog>header strong { color:#2f3c58; font-size:22px; }
.strategy-fullshow-fact-dialog>header button { width:34px; height:34px; border:0; border-radius:10px; background:#eef1f8; color:#7d8799; font-size:22px; cursor:pointer; }
.strategy-fullshow-fact-form { display:grid; grid-template-columns:180px minmax(0,1fr); gap:12px; }
.strategy-fullshow-fact-form label { display:grid; gap:6px; color:#5f6a80; font-size:12px; font-weight:900; }
.strategy-fullshow-fact-form label.wide { grid-column:1/-1; }
.strategy-fullshow-fact-form input,.strategy-fullshow-fact-form select,.strategy-fullshow-fact-form textarea { width:100%; box-sizing:border-box; border:1px solid #d6ddeb; border-radius:10px; outline:0; background:#fff; color:#34405a; font:inherit; }
.strategy-fullshow-fact-form input,.strategy-fullshow-fact-form select { min-height:42px; padding:0 11px; }
.strategy-fullshow-fact-form textarea { min-height:118px; padding:11px 12px; resize:vertical; line-height:1.6; }
.strategy-fullshow-fact-form input:focus,.strategy-fullshow-fact-form select:focus,.strategy-fullshow-fact-form textarea:focus { border-color:#7584e8; box-shadow:0 0 0 3px rgba(102,116,223,.08); }
.strategy-fullshow-fact-dialog>footer { display:flex; justify-content:flex-end; gap:9px; }
.strategy-fullshow-fact-dialog>footer button { min-height:40px; padding:0 15px; border:1px solid #d7deec; border-radius:10px; background:#fff; color:#59657a; font-weight:850; cursor:pointer; }
.strategy-fullshow-fact-dialog>footer button.primary { border-color:#6372dc; background:#6372dc; color:#fff; }
.strategy-fullshow-fact-dialog>footer button:disabled { opacity:.55; cursor:not-allowed; }
.strategy-fullshow-output-title { display:flex; align-items:center; gap:9px; min-width:0; }
.strategy-fullshow-output-mode {
  min-height:40px;
  padding:0 12px;
  border:1px solid transparent;
  border-radius:11px;
  background:transparent;
  color:#34405a;
  font:inherit;
  font-size:18px;
  font-weight:900;
  cursor:pointer;
  transition:.16s ease;
}
.strategy-fullshow-output-mode:hover { background:#f4f6ff; color:#5362c9; }
.strategy-fullshow-output-mode.active {
  border-color:#8795ed;
  background:linear-gradient(180deg,#f2f4ff 0%,#e6eaff 100%);
  color:#4e5fd0;
  box-shadow:0 7px 17px rgba(76,91,190,.13),0 0 0 2px rgba(99,114,220,.06);
}
.strategy-custom-mainline-inline { display:grid; gap:14px; min-width:0; padding-top:2px; }
.strategy-custom-mainline-inline>audio { width:100%; min-height:42px; }
.strategy-fullshow-custom-mainline-entry {
  min-height:30px; padding:0 11px; border:1px solid #8f9bed; border-radius:9px;
  background:linear-gradient(180deg,#f7f8ff 0%,#e8ecff 100%); color:#5060ca;
  font-size:11px; font-weight:950; cursor:pointer; box-shadow:0 5px 12px rgba(74,89,184,.10);
}
.strategy-fullshow-custom-mainline-entry:hover { transform:translateY(-1px); border-color:#6474df; box-shadow:0 8px 18px rgba(74,89,184,.16); }
.strategy-custom-mainline-upload { display:grid; gap:10px; padding:15px; border:1px solid #dce2f1; border-radius:15px; background:#f7f9fd; }
.strategy-custom-mainline-upload>label {
  display:grid; place-items:center; gap:6px; min-height:112px; padding:16px;
  border:1px dashed #9ba7e5; border-radius:13px; background:#fff; cursor:pointer; text-align:center;
}
.strategy-custom-mainline-upload>label input { position:absolute; width:1px; height:1px; opacity:0; pointer-events:none; }
.strategy-custom-mainline-upload>label strong { color:#4d5cc4; font-size:16px; }
.strategy-custom-mainline-upload>label span { color:#8893a8; font-size:11px; }
.strategy-custom-mainline-file-meta { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.strategy-custom-mainline-file-meta span { padding:5px 8px; border-radius:999px; background:#edf1f8; color:#6d788f; font-size:10px; font-weight:850; }
.strategy-custom-mainline-file-meta button,
.strategy-custom-mainline-segments>header button,
.strategy-custom-mainline-summary button {
  margin-left:auto; min-height:34px; padding:0 12px; border:1px solid #6877df; border-radius:9px;
  background:#6674df; color:#fff; font-size:11px; font-weight:900; cursor:pointer;
}
.strategy-custom-mainline-file-meta button:disabled,
.strategy-custom-mainline-segments>header button:disabled,
.strategy-custom-mainline-summary button:disabled { cursor:not-allowed; opacity:.55; }
.strategy-custom-mainline-progress { display:grid; gap:6px; }
.strategy-custom-mainline-progress>div { height:7px; overflow:hidden; border-radius:999px; background:#e5e9f3; }
.strategy-custom-mainline-progress>div i { display:block; height:100%; border-radius:inherit; background:linear-gradient(90deg,#6674df,#7b8cf3); transition:width .18s ease; }
.strategy-custom-mainline-progress>span { color:#78849a; font-size:11px; line-height:1.55; }
.strategy-custom-mainline-error { margin:0; }
.strategy-custom-mainline-summary { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:10px; }
.strategy-custom-mainline-summary>div {
  display:flex; align-items:center; gap:8px; flex-wrap:wrap; min-height:54px; padding:11px 12px;
  border:1px solid #dfe4ef; border-radius:12px; background:#fff;
}
.strategy-custom-mainline-summary>div.ready { border-color:#b9e0c9; background:#f1fbf5; }
.strategy-custom-mainline-summary strong { color:#46536d; font-size:12px; }
.strategy-custom-mainline-summary span { color:#77839a; font-size:10px; font-weight:800; }
.strategy-custom-mainline-segments { display:grid; gap:10px; padding:13px; border:1px solid #dfe4ef; border-radius:14px; background:#fff; }
.strategy-custom-mainline-segments>header { display:flex; align-items:center; justify-content:space-between; gap:14px; }
.strategy-custom-mainline-segments>header>div { display:grid; gap:3px; }
.strategy-custom-mainline-segments>header strong { color:#3e4b67; font-size:14px; }
.strategy-custom-mainline-segments>header span { color:#8993a5; font-size:10px; line-height:1.5; }
.strategy-custom-mainline-segment-list { display:grid; gap:8px; max-height:420px; overflow:auto; padding-right:4px; }
.strategy-custom-mainline-segment-list article {
  display:grid; grid-template-columns:150px minmax(0,1fr); gap:10px; align-items:stretch;
  padding:9px; border:1px solid #e5e9f2; border-radius:11px; background:#f9fafd;
}
.strategy-custom-mainline-segment-list article>div { display:grid; align-content:start; gap:5px; padding:5px 4px; }
.strategy-custom-mainline-segment-list b { color:#5361c9; font-size:11px; }
.strategy-custom-mainline-segment-list time { color:#6f7b91; font-size:10px; font-variant-numeric:tabular-nums; }
.strategy-custom-mainline-segment-list span { color:#97a0b0; font-size:9px; font-weight:800; }
.strategy-custom-mainline-segment-list textarea {
  width:100%; min-height:68px; box-sizing:border-box; resize:vertical; padding:9px 10px;
  border:1px solid #d8deeb; border-radius:9px; outline:0; background:#fff; color:#34415c; font:inherit; line-height:1.6;
}
.strategy-custom-mainline-segment-list textarea:focus { border-color:#7482e5; box-shadow:0 0 0 3px rgba(102,116,223,.08); }
.strategy-custom-mainline-srt { padding:11px 12px; border:1px solid #dfe4ef; border-radius:12px; background:#fff; }
.strategy-custom-mainline-srt summary { color:#5361c9; font-size:11px; font-weight:900; cursor:pointer; }
.strategy-custom-mainline-srt pre {
  max-height:300px; margin:10px 0 0; overflow:auto; padding:10px; border-radius:9px;
  background:#f6f8fc; color:#536078; font:11px/1.65 ui-monospace,SFMono-Regular,Consolas,monospace; white-space:pre-wrap;
}
.strategy-custom-mainline-footer { display:flex; align-items:center; justify-content:flex-end; gap:9px; padding-top:2px; }
.strategy-custom-mainline-footer>div { margin-right:auto; min-width:0; }
.strategy-custom-mainline-footer span { color:#758098; font-size:12px; line-height:1.5; }
.strategy-custom-mainline-footer button {
  min-height:40px;
  padding:0 15px;
  border:1px solid #d6ddeb;
  border-radius:10px;
  background:#fff;
  color:#59657b;
  font-size:13px;
  font-weight:900;
  cursor:pointer;
}
.strategy-custom-mainline-footer button.primary { border-color:#6574df; background:#6574df; color:#fff; }
.strategy-custom-mainline-footer button:disabled { cursor:not-allowed; opacity:.5; }
.strategy-fullshow-voice { display:grid; gap:12px; padding:15px; border:1px solid #d9e0f2; border-radius:14px; background:linear-gradient(180deg,#fbfcff 0%,#f7f9ff 100%); }
.strategy-fullshow-voice>header { display:flex; align-items:center; justify-content:space-between; gap:14px; }
.strategy-fullshow-voice>header>div { display:grid; gap:2px; }
.strategy-fullshow-voice>header strong { color:#3f4c67; font-size:17px; }
.strategy-fullshow-voice>header span { color:#7d889d; font-size:14px; }
.strategy-fullshow-voice>header em { padding:5px 8px; border-radius:999px; background:#eef1ff; color:#5b68cf; font-size:12px; font-style:normal; font-weight:900; }
.strategy-fullshow-voice-empty { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:13px 14px; border:1px dashed #cfd7eb; border-radius:12px; background:#fff; }
.strategy-fullshow-voice-empty>div { display:grid; gap:3px; }
.strategy-fullshow-voice-empty strong { color:#46526a; font-size:15px; }
.strategy-fullshow-voice-empty span { color:#8791a5; font-size:14px; line-height:1.5; }
.strategy-fullshow-voice-empty button,.strategy-fullshow-voice-version button,.strategy-fullshow-voice-candidate button { min-height:38px; padding:0 13px; border:1px solid #ccd4eb; border-radius:9px; background:#fff; color:#5361c9; font-size:14px; font-weight:850; cursor:pointer; white-space:nowrap; }
.strategy-fullshow-voice-progress { display:grid; gap:8px; padding:13px 14px; border-radius:12px; background:#fff; }
.strategy-fullshow-voice-progress>div:first-child { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.strategy-fullshow-voice-progress span { color:#56627a; font-size:14px; font-weight:850; }
.strategy-fullshow-voice-progress strong { color:#5e6bd8; font-size:16px; }
.strategy-fullshow-voice-progress small { color:#9099aa; font-size:13px; }
.strategy-fullshow-voice-progress-track { height:8px; overflow:hidden; border-radius:999px; background:#e9edfb; }
.strategy-fullshow-voice-progress-track i { display:block; height:100%; border-radius:inherit; background:linear-gradient(90deg,#7d88eb,#5f6fdf); transition:width .45s ease; }
.strategy-fullshow-voice-version,.strategy-fullshow-voice-candidate { display:grid; gap:10px; padding:13px 14px; border:1px solid #e1e6f0; border-radius:12px; background:#fff; }
.strategy-fullshow-voice-set-preview { min-height:38px; padding:0 14px; border:1px solid #6674df; border-radius:10px; background:#eef1ff; color:#5665d5; font-size:14px; font-weight:900; cursor:pointer; }
.strategy-fullshow-voice-set-preview.active { border-color:#5364d9; background:linear-gradient(135deg,#6575e7,#5265d8); color:#fff; box-shadow:0 8px 18px rgba(73,91,195,.22),inset 0 1px 0 rgba(255,255,255,.28); }
.strategy-fullshow-voice-version-head { display:flex; align-items:center; justify-content:space-between; gap:14px; }
.strategy-fullshow-voice-version-head>div,.strategy-fullshow-voice-candidate>div:first-child { display:grid; gap:3px; }
.strategy-fullshow-voice-version strong,.strategy-fullshow-voice-candidate strong { color:#46516a; font-size:15px; }
.strategy-fullshow-voice-version span,.strategy-fullshow-voice-candidate span { color:#8a94a7; font-size:13px; line-height:1.5; }
.strategy-fullshow-voice audio { width:100%; min-height:38px; }
.strategy-fullshow-asset-bundle { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:10px 11px; border:1px solid #dfe5f3; border-radius:11px; background:#f8faff; }
.strategy-fullshow-asset-bundle>div { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.strategy-fullshow-asset-bundle strong { color:#48546d; font-size:14px; }
.strategy-fullshow-asset-bundle span { padding:4px 7px; border-radius:999px; background:#eef1f7; color:#7b869a; font-size:11px; font-weight:850; }
.strategy-fullshow-asset-bundle span.ok { background:#e6f5ec; color:#347d5a; }
.strategy-fullshow-asset-bundle button { min-height:34px; padding:0 10px; border:1px solid #cbd4f2; border-radius:8px; background:#fff; color:#5362c8; font-size:12px; font-weight:900; cursor:pointer; }
.strategy-fullshow-asset-bundle button:disabled { cursor:not-allowed; opacity:.45; }
.strategy-fullshow-srt-view { display:grid; gap:9px; padding:11px; border:1px solid #dfe4f2; border-radius:11px; background:#fff; }
.strategy-fullshow-srt-view>header { display:flex; align-items:center; justify-content:space-between; gap:10px; }
.strategy-fullshow-srt-view>header>div { display:grid; gap:3px; min-width:0; }
.strategy-fullshow-srt-view>header strong { color:#46536d; font-size:14px; }
.strategy-fullshow-srt-view>header span { color:#8d97aa; font-size:12px; }
.strategy-fullshow-srt-view>header button { min-height:34px; padding:0 11px; border:1px solid #cbd4f2; border-radius:8px; background:#fff; color:#5362c8; font-size:12px; font-weight:900; cursor:pointer; white-space:nowrap; }
.strategy-fullshow-srt-view>header button:disabled { cursor:not-allowed; opacity:.5; }
.strategy-fullshow-srt-edit-list { display:grid; gap:7px; max-height:360px; overflow:auto; padding-right:3px; }
.strategy-fullshow-srt-edit-list article { display:grid; grid-template-columns:118px minmax(0,1fr); gap:10px; padding:9px; border:1px solid #e4e8f1; border-radius:10px; background:#f8f9fc; }
.strategy-fullshow-srt-edit-list article>div { display:grid; align-content:start; gap:5px; padding:5px 3px; }
.strategy-fullshow-srt-edit-list b { color:#5664cb; font-size:12px; }
.strategy-fullshow-srt-edit-list time { color:#7b869b; font-size:12px; font-variant-numeric:tabular-nums; }
.strategy-fullshow-srt-edit-list textarea { width:100%; min-height:70px; box-sizing:border-box; resize:vertical; padding:9px 10px; border:1px solid #d6ddeb; border-radius:9px; outline:0; background:#fff; color:#48556d; font-size:14px; line-height:1.65; font-family:inherit; }
.strategy-fullshow-srt-edit-list textarea:focus { border-color:#7381e2; box-shadow:0 0 0 3px rgba(102,116,223,.08); }
.strategy-fullshow-timeline { display:grid; gap:9px; padding:11px; border:1px solid #e3e7f3; border-radius:11px; background:#f8f9fd; }
.strategy-fullshow-timeline>header { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.strategy-fullshow-timeline>header>div { display:flex; align-items:baseline; gap:9px; min-width:0; }
.strategy-fullshow-timeline>header strong { color:#46536f; font-size:14px; }
.strategy-fullshow-timeline>header span,.strategy-fullshow-timeline>header em { color:#8e98aa; font-size:12px; font-style:normal; }
.strategy-fullshow-timeline-list { display:grid; gap:5px; max-height:260px; overflow:auto; padding-right:3px; }
.strategy-fullshow-timeline-list article { display:grid; grid-template-columns:96px minmax(0,1fr); gap:10px; align-items:start; padding:9px 10px; border:1px solid transparent; border-radius:9px; background:#fff; transition:.16s ease; }
.strategy-fullshow-timeline-list article.active { border-color:#8794ee; background:#eef1ff; box-shadow:0 5px 13px rgba(77,92,190,.10); }
.strategy-fullshow-timeline-list time { color:#77839a; font-size:12px; font-variant-numeric:tabular-nums; white-space:nowrap; }
.strategy-fullshow-timeline-list span { color:#59657b; font-size:14px; line-height:1.6; }
.strategy-fullshow-timeline-list article.active span { color:#4456bf; font-weight:850; }
.strategy-fullshow-timeline-list b { color:#5f6dd2; font-size:9px; white-space:nowrap; }
.strategy-fullshow-voice-candidate { border-color:#c9d2ff; background:#f8f9ff; }
.strategy-fullshow-voice-candidate footer { display:flex; justify-content:flex-end; gap:8px; }
.strategy-fullshow-voice-candidate button.primary { border-color:#6674df; background:#6674df; color:#fff; }
.strategy-fullshow-tts-hints { display:grid; gap:9px; }
.strategy-fullshow-tts-hints>header { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.strategy-fullshow-tts-hints>header strong { color:#47536c; font-size:14px; }
.strategy-fullshow-tts-hints>header span { color:#939cad; font-size:11px; }
.strategy-fullshow-tts-hints>div { display:grid; gap:7px; }
.strategy-fullshow-tts-hints article { display:grid; grid-template-columns:90px minmax(0,1fr) auto; gap:10px; align-items:center; padding:9px 11px; border-radius:10px; background:#f7f8fb; }
.strategy-fullshow-tts-hints article strong { color:#5966c5; font-size:12px; }
.strategy-fullshow-tts-hints article span { color:#5e697d; font-size:12px; line-height:1.5; }
.strategy-fullshow-tts-hints article em { color:#7f899b; font-size:11px; font-style:normal; white-space:nowrap; }
.strategy-fullshow-compile-summary { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:8px; }
.strategy-fullshow-compile-summary>div { display:grid; grid-template-columns:1fr auto; gap:8px; align-items:center; padding:10px 11px; border-radius:10px; background:#f7f8fb; }
.strategy-fullshow-compile-summary span { color:#7d879a; font-size:12px; font-weight:800; }
.strategy-fullshow-compile-summary strong { color:#5d69cf; font-size:18px; }
.strategy-fullshow-runtime-list { display:grid; gap:9px; }
.strategy-fullshow-runtime-list>div { display:grid; grid-template-columns:64px minmax(0,1fr); gap:10px; padding:11px 12px; border-radius:11px; background:#f8f9fc; }
.strategy-fullshow-runtime-list b { color:#5b67c9; font-size:13px; }
.strategy-fullshow-runtime-list span { color:#59657a; font-size:13px; line-height:1.55; }
.strategy-fullshow-publish { margin-top:14px; border-color:#d9e1f7; background:linear-gradient(180deg,#fbfcff 0%,#f7f9ff 100%); }
.strategy-fullshow-publish-checks { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:9px; }
.strategy-fullshow-publish-checks>div { display:grid; gap:4px; min-width:0; padding:12px 13px; border:1px solid #e2e7f0; border-radius:12px; background:#fff; }
.strategy-fullshow-publish-checks>div.ok { border-color:#c6e4d3; background:#f5fbf7; }
.strategy-fullshow-publish-checks span { color:#8590a4; font-size:11px; font-weight:850; }
.strategy-fullshow-publish-checks strong { overflow:hidden; color:#45516a; font-size:13px; text-overflow:ellipsis; white-space:nowrap; }
.strategy-fullshow-publish-checks>div.ok strong { color:#347c59; }
.strategy-fullshow-publish-note { display:grid; gap:3px; padding:12px 14px; border:1px solid #dfe5f5; border-radius:12px; background:#f5f7fd; }
.strategy-fullshow-publish-note strong { color:#5362c9; font-size:13px; }
.strategy-fullshow-publish-note span { color:#7d879b; font-size:12px; line-height:1.55; }
.strategy-fullshow-publish-actions { display:flex; justify-content:flex-end; gap:9px; }
.strategy-fullshow-publish-actions button { min-height:44px; padding:0 18px; border:1px solid #ccd4eb; border-radius:12px; background:#fff; color:#5261c9; font-size:13px; font-weight:900; cursor:pointer; }
.strategy-fullshow-publish-actions button.primary { border-color:#6372dc; background:linear-gradient(135deg,#7180ed,#5668da); color:#fff; box-shadow:0 10px 22px rgba(78,94,203,.20); }
.strategy-fullshow-publish-actions button:disabled { cursor:not-allowed; opacity:.52; box-shadow:none; }
.strategy-fullshow-saved-version { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:8px; }
.strategy-fullshow-saved-version>div { display:grid; gap:3px; padding:10px 12px; border-radius:11px; background:#fff; border:1px solid #e4e8f1; }
.strategy-fullshow-saved-version span { color:#8a94a6; font-size:10px; font-weight:800; }
.strategy-fullshow-saved-version strong { color:#43506a; font-size:12px; }
.strategy-fullshow-version-history { display:grid; gap:8px; }
.strategy-fullshow-version-history>header { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.strategy-fullshow-version-history>header strong { color:#48556f; font-size:13px; }
.strategy-fullshow-version-history>header span { color:#969fb0; font-size:11px; }
.strategy-fullshow-version-history>div { display:grid; gap:6px; }
.strategy-fullshow-version-history article { display:grid; grid-template-columns:70px minmax(0,1fr) auto; gap:10px; align-items:center; padding:9px 11px; border:1px solid #e5e9f1; border-radius:10px; background:#fff; }
.strategy-fullshow-version-history article>strong { color:#5362c9; font-size:12px; }
.strategy-fullshow-version-history article>span { overflow:hidden; color:#667187; font-size:11px; text-overflow:ellipsis; white-space:nowrap; }
.strategy-fullshow-version-history article>em { padding:4px 7px; border-radius:999px; background:#f0f2f7; color:#7c879a; font-size:10px; font-style:normal; font-weight:900; }
.strategy-fullshow-version-history article>em.published { background:#e6f5ec; color:#2f7d5a; }

@media (max-width: 1180px) {
  .strategy-workflow-nav { grid-template-columns:minmax(0,1fr); padding-inline:10px; }
  .strategy-workflow-inputs { gap:5px; }
  .strategy-workflow-step { min-height:54px; padding:7px 6px!important; font-size:12px!important; }
  .strategy-workflow-icon { width:26px; height:26px; flex-basis:26px; }
  .strategy-workflow-icon svg { width:15px; height:15px; }
  .strategy-workflow-step:not(:last-child)::after { right:-6px; width:6px; height:6px; }
  .strategy-workflow-output { min-width:0; padding-inline:9px!important; }
  .strategy-plan-mini-list { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .strategy-product-link-grid { grid-template-columns:1fr; }
  .strategy-benefit-grid { grid-template-columns:1fr; }
  .strategy-benefit-formal-list>article { width:min(100%,760px); }
  .strategy-anchor-style-summary { grid-template-columns:1fr; }
  .strategy-anchor-style-group-cards { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .strategy-fullshow-grid,.strategy-fullshow-output-grid { grid-template-columns:1fr; }
  .strategy-fullshow-rounds article { grid-template-columns:46px minmax(160px,.7fr) minmax(0,1.3fr); }
  .strategy-fullshow-rounds article>em { grid-column:2/-1; justify-self:start; }
  .strategy-script-editor-grid,.strategy-knowledge-groups { grid-template-columns:1fr; }
}
@media (max-width: 900px) {
  .strategy-workflow-nav { grid-template-columns:1fr; gap:10px; overflow:visible; }
  .strategy-workflow-inputs {
    display:grid;
    grid-template-columns:repeat(2,minmax(0,1fr));
    width:100%;
    gap:8px;
    overflow:visible;
  }
  .strategy-workflow-inputs::before { display:none; }
  .strategy-workflow-step { width:100%; justify-content:center; }
  .strategy-workflow-step::after { display:none!important; }
  .strategy-workflow-output { width:100%; min-width:0; min-height:68px; grid-column:1/-1; }
  .strategy-workflow-output-copy { text-align:center; }
  .strategy-plan-create,.strategy-script-meta { grid-template-columns:1fr; }
  .strategy-plan-card { grid-template-columns:1fr; }
  .strategy-benefit-formal>header { flex-direction:column; }
  .strategy-benefit-formal-list { grid-template-columns:1fr; }
  .strategy-benefit-formal-list>article { width:100%; padding:24px 20px 20px; border-radius:24px; }
  .strategy-benefit-formal-detail { grid-template-columns:1fr; }
  .strategy-benefit-formal-detail-wide { grid-column:auto; }
  .strategy-benefit-card footer { align-items:flex-start; flex-direction:column; }
  .strategy-plan-toolbar,.strategy-v2-section-head,.strategy-script-actions { align-items:flex-start; flex-direction:column; }
  .strategy-plan-toolbar { justify-content:flex-start; }
  .strategy-plan-title-switch { width:100%; font-size:22px; }
  .strategy-plan-mini-list { grid-template-columns:1fr; }
  .strategy-legacy-select { margin-left:0; }
  .strategy-analysis-summary { grid-template-columns:1fr; }
  .strategy-analysis-metrics { width:100%; }
  .strategy-analysis-metrics button { flex:1; }
  .strategy-link-coverage { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .strategy-anchor-style-metrics { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .strategy-anchor-style-rule-grid { grid-template-columns:1fr; }
  .strategy-fullshow-head { padding:20px 18px 22px; border-radius:18px; }
  .strategy-fullshow-title-block h3 { font-size:28px; }
  .strategy-fullshow-title-block p { font-size:14px; line-height:1.7; }
  .strategy-fullshow-title-action { width:100%; }
  .strategy-fullshow-title-action .primary-button { width:100%; max-width:360px; min-width:0; }
  .strategy-fullshow-main-actions { grid-template-columns:minmax(0,1fr); }
  .strategy-fullshow-main-actions-buttons { justify-self:stretch; width:100%; max-width:none; }
  .strategy-fullshow-source-cards,.strategy-fullshow-inline-fields { grid-template-columns:1fr; }
  .strategy-fullshow-duration { grid-template-columns:1fr; }
  .strategy-fullshow-variant-tabs { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .strategy-fullshow-script-card>header { grid-template-columns:1fr; }
  .strategy-fullshow-script-metrics { justify-content:flex-start; }
  .strategy-fullshow-script-side { justify-items:start; }
  .strategy-fullshow-script-actions { justify-content:flex-start; }
  .strategy-fullshow-tts-hints article { grid-template-columns:1fr; }
  .strategy-fullshow-rounds article { grid-template-columns:42px minmax(0,1fr); }
  .strategy-fullshow-round-flow,.strategy-fullshow-rounds article>em { grid-column:2; }
  .strategy-script-actions>div { margin-right:0; }
  .strategy-legacy-version-bar { align-items:flex-start; flex-wrap:wrap; justify-content:flex-start; }
  .strategy-legacy-version-bar>div { width:100%; }
}

/* Final workflow layout: horizontal embedded rail + content */
.strategy-workflow-layout {
  display:grid;
  grid-template-columns:minmax(0,1fr);
  grid-template-rows:max-content max-content;
  align-items:start;
  align-content:start;
  align-self:start;
  min-width:0;
  width:100%;
  --workflow-accent-rgb:79,101,202;
  --workflow-surface:#eef2ff;
  --workflow-surface-deep:#e5ebff;
  background:var(--workflow-surface);
}
.strategy-workflow-content {
  align-self:start;
  min-width:0;
  width:100%;
  border-left:0;
  border-top:0;
  background:var(--workflow-surface);
  box-shadow:none;
  transition:background .18s ease;
}
.strategy-workflow-nav {
  display:block!important;
  width:100%!important;
  min-width:0!important;
  max-width:100%!important;
  padding:16px 18px 0!important;
  border:0!important;
  border-bottom:0!important;
  background:var(--workflow-surface)!important;
  overflow:hidden!important;
  box-sizing:border-box;
  align-self:start;
}
.strategy-workflow-nav::before,
.strategy-workflow-inputs::before {
  display:none!important;
}
.strategy-workflow-inputs {
  display:flex!important;
  flex-direction:row!important;
  align-items:stretch!important;
  gap:0!important;
  width:100%!important;
  padding:0!important;
  overflow:hidden!important;
}
.strategy-workflow-step,
.strategy-workflow-output {
  position:relative!important;
  z-index:1;
  display:flex!important;
  flex-direction:row!important;
  align-items:center!important;
  justify-content:flex-start!important;
  gap:7px!important;
  width:auto!important;
  flex:1 1 0!important;
  min-width:0!important;
  min-height:60px!important;
  margin:0 0 0 -10px!important;
  padding:10px 17px 10px 20px!important;
  border:0!important;
  border-radius:0!important;
  background:linear-gradient(180deg,#ffffff 0%,#f3f6fc 100%)!important;
  color:#4d5a73!important;
  clip-path:polygon(0 0,calc(100% - 13px) 0,100% 50%,calc(100% - 13px) 100%,0 100%,13px 50%);
  box-shadow:none!important;
  filter:drop-shadow(0 5px 9px rgba(67,82,130,.08));
  transform:none!important;
  box-sizing:border-box;
  cursor:pointer;
  transition:filter .18s ease,color .18s ease,background .18s ease;
}
.strategy-workflow-step:first-child {
  margin-left:0!important;
  padding-left:13px!important;
  clip-path:polygon(0 0,calc(100% - 13px) 0,100% 50%,calc(100% - 13px) 100%,0 100%);
}
.strategy-workflow-step::after,
.strategy-workflow-output::after,
.strategy-workflow-output::before {
  display:none!important;
}
.strategy-workflow-step:hover {
  z-index:3;
  color:#4459c1!important;
  background:linear-gradient(180deg,#f7f8ff 0%,#e9edff 100%)!important;
  filter:drop-shadow(0 8px 13px rgba(75,92,177,.16));
  transform:none!important;
}
.strategy-workflow-step.active {
  z-index:4;
  color:#455bc7!important;
  background:var(--workflow-surface)!important;
  filter:drop-shadow(0 -4px 9px rgba(76,94,183,.10));
}
.strategy-workflow-icon {
  width:31px!important;
  height:31px!important;
  flex:0 0 31px!important;
  border-radius:9px!important;
  background:rgba(93,112,192,.09)!important;
  color:#6275bb!important;
}
.strategy-workflow-step.active .strategy-workflow-icon {
  background:#5b70d5!important;
  color:#fff!important;
  box-shadow:none!important;
}
.strategy-workflow-label {
  min-width:0!important;
  overflow:hidden!important;
  color:inherit!important;
  font-size:13px!important;
  font-weight:900!important;
  line-height:1.25!important;
  text-align:left!important;
  text-overflow:ellipsis!important;
  white-space:nowrap!important;
}
.strategy-workflow-output {
  z-index:2!important;
  flex:1.22 1 0!important;
  margin-left:-10px!important;
  color:#fff!important;
  background:linear-gradient(145deg,#4258bf 0%,#536fd0 58%,#4a83cd 100%)!important;
  filter:drop-shadow(0 10px 16px rgba(60,80,170,.24));
}
.strategy-workflow-output:hover,
.strategy-workflow-output.active {
  z-index:5!important;
  background:linear-gradient(145deg,#374eb9 0%,#4965ca 56%,#427cc8 100%)!important;
  filter:drop-shadow(0 12px 19px rgba(58,77,168,.3));
  transform:none!important;
}

.workflow-mode-script,
.workflow-mode-products,
.workflow-mode-benefits,
.workflow-mode-knowledge,
.workflow-mode-rhythm,
.workflow-mode-memory,
.workflow-mode-anchor,
.workflow-mode-voice,
.workflow-mode-fullshow {
  --workflow-accent-rgb:79,101,202;
}
.strategy-workflow-output-mark {
  width:31px!important;
  height:31px!important;
  flex:0 0 31px!important;
  border-radius:9px!important;
  background:rgba(255,255,255,.17)!important;
  font-size:17px!important;
}
.strategy-workflow-output-copy {
  min-width:0;
  display:block!important;
  text-align:left!important;
}
.strategy-workflow-output-copy strong {
  display:block;
  color:#fff!important;
  font-size:13px!important;
  font-weight:950!important;
  line-height:1.25!important;
  white-space:nowrap!important;
  overflow:hidden!important;
  text-overflow:ellipsis!important;
}

@media (max-width: 1180px) {
  .strategy-workflow-layout { grid-template-columns:minmax(0,1fr); }
  .strategy-workflow-nav { width:100%!important; padding:13px 10px 15px!important; }
  .strategy-workflow-step,.strategy-workflow-output { min-height:56px!important; padding:9px 14px 9px 17px!important; gap:5px!important; }
  .strategy-workflow-icon,.strategy-workflow-output-mark { width:27px!important; height:27px!important; flex-basis:27px!important; }
  .strategy-workflow-label,.strategy-workflow-output-copy strong { font-size:12px!important; }
}
@media (max-width: 760px) {
  .strategy-workflow-nav { padding:10px 7px 12px!important; overflow-x:auto!important; }
  .strategy-workflow-inputs { min-width:900px!important; overflow:visible!important; }
  .strategy-workflow-step,.strategy-workflow-output { min-height:54px!important; }
}

/* Active workflow tab and visible module share one continuous surface */
.strategy-workflow-layout {
  --workflow-tab-surface:#dce4ff;
  --workflow-surface:#e8edff;
  --workflow-surface-soft:#f4f6ff;
}
.strategy-workflow-nav {
  background:#f7f9ff!important;
}
.strategy-workflow-step.active,
.strategy-workflow-output.active {
  background:linear-gradient(180deg,#e4eaff 0%,var(--workflow-tab-surface) 100%)!important;
  color:#334aa8!important;
  box-shadow:inset 0 1px 0 rgba(255,255,255,.92),inset 0 -1px 0 rgba(76,96,190,.16)!important;
  filter:drop-shadow(0 8px 12px rgba(65,82,170,.18))!important;
}
.strategy-workflow-content,
.strategy-workflow-content > section,
.strategy-workflow-content > .strategy-v2-workspace,
.strategy-workflow-content > .strategy-script-workspace,
.strategy-workflow-content > .strategy-agent-workspace,
.strategy-workflow-content > .strategy-plan-settings {
  background:linear-gradient(180deg,var(--workflow-surface) 0%,#edf1ff 180px,var(--workflow-surface-soft) 420px)!important;
}
.strategy-workflow-content {
  border-top:0!important;
  box-shadow:inset 0 18px 28px rgba(73,91,176,.065),inset 0 1px 0 rgba(255,255,255,.72)!important;
}
.strategy-workflow-step.active .strategy-workflow-icon {
  background:linear-gradient(145deg,#6579df,#4f65c9)!important;
  color:#fff!important;
  box-shadow:0 6px 12px rgba(64,83,178,.22),inset 0 1px 0 rgba(255,255,255,.2)!important;
}
.strategy-workflow-output.active {
  color:#334aa8!important;
}
.strategy-workflow-output.active .strategy-workflow-output-mark {
  background:linear-gradient(145deg,#6579df,#4f65c9)!important;
  color:#fff!important;
  box-shadow:0 6px 12px rgba(64,83,178,.22),inset 0 1px 0 rgba(255,255,255,.2)!important;
}
.strategy-workflow-output.active .strategy-workflow-output-copy strong {
  color:#334aa8!important;
}

/* Final workflow output: premium destination treatment */
.strategy-workflow-output {
  isolation:isolate;
  overflow:hidden!important;
  padding-right:22px!important;
  border:1px solid rgba(116,135,238,.72)!important;
  background:
    linear-gradient(180deg,#8190f4 0%,#6578e7 22%,#566bd8 68%,#4054bd 100%)!important;
  box-shadow:
    inset 0 3px 0 rgba(255,255,255,.34),
    inset 0 -7px 0 rgba(43,61,157,.24),
    inset 0 -14px 22px rgba(37,53,143,.16),
    0 3px 0 rgba(45,61,149,.25)!important;
  filter:
    drop-shadow(0 14px 20px rgba(54,73,168,.32))
    drop-shadow(0 4px 6px rgba(55,71,135,.16))!important;
  transform:translateY(-3px)!important;
}
.strategy-workflow-output::before {
  content:""!important;
  display:block!important;
  position:absolute!important;
  z-index:-1!important;
  width:130px!important;
  height:130px!important;
  right:-42px!important;
  top:-68px!important;
  border-radius:50%!important;
  background:radial-gradient(circle,rgba(255,255,255,.30) 0%,rgba(255,255,255,.08) 48%,transparent 72%)!important;
  pointer-events:none!important;
}
.strategy-workflow-output::after {
  content:""!important;
  display:block!important;
  position:absolute!important;
  z-index:0!important;
  left:-55%!important;
  top:-55%!important;
  width:42%!important;
  height:210%!important;
  background:linear-gradient(90deg,transparent,rgba(255,255,255,.28),transparent)!important;
  transform:rotate(18deg)!important;
  transition:left .5s ease!important;
  pointer-events:none!important;
}
.strategy-workflow-output:hover::after { left:125%!important; }
.strategy-workflow-output:hover {
  background:
    linear-gradient(180deg,#8b99f8 0%,#6d80eb 22%,#5a70dc 68%,#4258c2 100%)!important;
  box-shadow:
    inset 0 3px 0 rgba(255,255,255,.40),
    inset 0 -8px 0 rgba(43,61,157,.26),
    inset 0 -16px 24px rgba(37,53,143,.18),
    0 4px 0 rgba(45,61,149,.28)!important;
  filter:
    drop-shadow(0 18px 28px rgba(54,73,168,.38))
    drop-shadow(0 5px 8px rgba(55,71,135,.18))!important;
  transform:translateY(-5px)!important;
}
.strategy-workflow-output-mark {
  position:relative!important;
  z-index:2!important;
  width:38px!important;
  height:38px!important;
  flex:0 0 38px!important;
  border:1px solid rgba(255,255,255,.26)!important;
  border-radius:13px!important;
  background:
    linear-gradient(180deg,rgba(255,255,255,.38) 0%,rgba(255,255,255,.17) 62%,rgba(42,61,156,.12) 100%)!important;
  box-shadow:
    0 9px 0 rgba(36,52,139,.16),
    0 12px 18px rgba(42,56,139,.24),
    inset 0 2px 0 rgba(255,255,255,.56),
    inset 0 -4px 0 rgba(36,52,139,.10)!important;
  transform:translateY(-2px)!important;
  color:#fff!important;
  font-size:20px!important;
  text-shadow:0 0 14px rgba(255,255,255,.8)!important;
}
.strategy-workflow-output-copy {
  position:relative!important;
  z-index:2!important;
}
.strategy-workflow-output-copy strong {
  font-size:13px!important;
  letter-spacing:.01em!important;
  text-shadow:0 1px 2px rgba(25,40,118,.12)!important;
}
.strategy-workflow-output.active {
  background:
    radial-gradient(circle at 78% 10%,rgba(255,255,255,.72),transparent 30%),
    linear-gradient(180deg,#f2f5ff 0%,#e5ebff 22%,#d6e0ff 70%,#bdcbfa 100%)!important;
  color:#2f49aa!important;
  filter:
    drop-shadow(0 16px 24px rgba(62,80,175,.24))
    drop-shadow(0 4px 6px rgba(66,83,169,.12))
    drop-shadow(0 -2px 6px rgba(255,255,255,.82))!important;
  box-shadow:
    inset 0 3px 0 rgba(255,255,255,.98),
    inset 0 -7px 0 rgba(77,96,190,.16),
    inset 0 -14px 20px rgba(70,91,187,.10),
    0 3px 0 rgba(76,95,183,.16)!important;
  transform:translateY(-3px)!important;
}
.strategy-workflow-output.active::before {
  background:radial-gradient(circle,rgba(95,117,224,.17) 0%,rgba(95,117,224,.05) 48%,transparent 72%)!important;
}
.strategy-workflow-output.active .strategy-workflow-output-mark {
  background:linear-gradient(180deg,#8797f4 0%,#6f82e8 42%,#4d63c8 100%)!important;
  border-color:rgba(255,255,255,.42)!important;
  color:#fff!important;
  box-shadow:
    0 7px 0 rgba(47,65,154,.18),
    0 12px 20px rgba(61,80,177,.30),
    inset 0 2px 0 rgba(255,255,255,.45),
    inset 0 -5px 0 rgba(46,63,151,.14)!important;
  transform:translateY(-2px)!important;
}
.strategy-workflow-output.active .strategy-workflow-output-copy strong {
  color:#2944a5!important;
  text-shadow:none!important;
}
.addressing-workspace{display:grid;gap:16px}.addressing-notice{padding:11px 13px;border-radius:12px;background:#edf9f4;color:#347a61;font-size:12px;font-weight:800}.addressing-mode-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.addressing-mode-grid>button{display:grid;gap:6px;padding:16px 18px;border:1px solid rgba(95,111,174,.16);border-radius:16px;background:#fafbfe;color:#556078;text-align:left;transition:.18s ease}.addressing-mode-grid>button strong{font-size:15px;color:#38435a}.addressing-mode-grid>button span{font-size:11px;color:#8c96a8;line-height:1.55}.addressing-mode-grid>button.active{border-color:rgba(91,111,211,.46);background:linear-gradient(145deg,#f2f5ff,#e8edff);box-shadow:0 0 0 3px rgba(91,111,211,.07),0 10px 24px rgba(71,90,177,.08)}.addressing-system-card,.addressing-custom-card{padding:16px;border:1px solid rgba(94,109,169,.13);border-radius:16px;background:rgba(255,255,255,.9)}.addressing-system-card>header,.addressing-custom-card>header{display:flex;align-items:center;justify-content:space-between;gap:12px}.addressing-system-card header>div,.addressing-custom-card header>div{display:grid;gap:3px}.addressing-system-card header span,.addressing-custom-card header span{color:#8d97aa;font-size:9px;font-weight:900;letter-spacing:.1em}.addressing-system-card header strong,.addressing-custom-card header strong{color:#38435a;font-size:15px}.addressing-system-card header>b{padding:5px 9px;border-radius:999px;background:#e7f7ef;color:#2f8a65;font-size:10px}.addressing-chip-list{display:flex;flex-wrap:wrap;gap:9px;margin-top:14px}.addressing-chip{display:flex;align-items:center;gap:10px;padding:9px 12px;border:1px solid #e1e5ee;border-radius:12px;background:#f8f9fc}.addressing-chip strong{color:#455168;font-size:12px}.addressing-chip span{color:#7180a2;font-size:10px;font-weight:900}.addressing-custom-card.inactive{opacity:.72}.addressing-custom-list{display:grid;gap:9px;margin-top:14px}.addressing-custom-list>article{display:grid;grid-template-columns:90px minmax(180px,1fr) 150px auto;align-items:end;gap:10px;padding:12px;border:1px solid #e3e7f0;border-radius:13px;background:#fafbfe}.addressing-enable,.addressing-name-field,.addressing-probability-field{display:grid;gap:6px}.addressing-enable{grid-template-columns:auto 1fr;align-items:center;align-self:center}.addressing-enable input{width:18px;height:18px}.addressing-name-field>span,.addressing-probability-field>span,.addressing-enable>span{color:#6d7890;font-size:10px;font-weight:850}.addressing-name-field input,.addressing-probability-field input{box-sizing:border-box;width:100%;min-height:38px;padding:8px 10px;border:1px solid #dce2ec;border-radius:10px;background:#fff;color:#39445a;font:inherit}.addressing-probability-field>div{display:flex;align-items:center;gap:6px}.addressing-probability-field input{width:92px}.addressing-probability-field b{color:#74809a;font-size:11px}.addressing-remove{min-height:38px;padding:0 11px;border:0;border-radius:10px;background:#fff0f1;color:#bd5662;font-size:11px;font-weight:850}.addressing-total{display:flex;align-items:center;justify-content:space-between;margin-top:14px;padding-top:12px;border-top:1px solid #edf0f4;color:#5b667d}.addressing-total strong{font-size:16px;color:#3f8a69}.addressing-total.invalid strong{color:#c3505b}.addressing-review-tip{margin:8px 0 0;color:#9099aa;font-size:10px;line-height:1.6}.addressing-empty{margin-top:12px}@media(max-width:900px){.addressing-mode-grid{grid-template-columns:1fr}.addressing-custom-list>article{grid-template-columns:80px 1fr 130px}.addressing-remove{grid-column:2/-1;justify-self:end}}
.addressing-custom-actions{display:flex!important;align-items:center;justify-content:flex-end;gap:8px}.addressing-save-button{min-width:112px}.addressing-custom-actions .strategy-version-button,.addressing-custom-actions .primary-button{white-space:nowrap}
.strategy-anchor-style-actions{display:flex;align-items:center;justify-content:flex-end;flex-wrap:wrap;gap:8px}.strategy-anchor-style-actions button{white-space:nowrap}
.strategy-workflow-nav .strategy-workflow-inputs { display:flex!important; }
.strategy-workflow-nav .strategy-workflow-step,.strategy-workflow-nav .strategy-workflow-output { min-height:72px!important; }
.strategy-workflow-nav .strategy-workflow-label,.strategy-workflow-nav .strategy-workflow-output-copy strong { font-size:16px!important; font-weight:900!important; }
@media(max-width:1200px) { .strategy-workflow-nav .strategy-workflow-label,.strategy-workflow-nav .strategy-workflow-output-copy strong { font-size:15px!important; } }
.reusable-anchor-style-settings-link{padding:8px;border:0;background:transparent;color:#6574be;font-size:12px;font-weight:850;text-align:left;cursor:pointer}
.reusable-anchor-style-sample-compose-actions{display:flex;gap:8px;align-items:center;flex-wrap:wrap}.reusable-anchor-style-file-button{display:inline-flex;align-items:center;cursor:pointer}.reusable-anchor-style-file-button input{display:none}
</style>
