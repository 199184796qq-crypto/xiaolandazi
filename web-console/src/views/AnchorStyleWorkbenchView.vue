<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  archiveAnchorStyleAgentStudioSession,
  compileAnchorStyleWorkbenchMeta,
  calibrateAnchorStyleScorecard,
  compileAnchorStyleScorecard,
  compileAnchorStyleSellingPointWheel,
  confirmAnchorStyleWorkbenchMeta,
  confirmAnchorStyleScorecard,
  confirmAnchorStyleSellingPointWheel,
  createAnchorStyleAgentStudioSession,
  getAnchorStyleAgentStudioCatalog,
  getAnchorStyleWorkbenchContext,
  getLiveAgentPlans,
  getRoomLiveAgentPlan,
  manuallyConfirmAnchorStyleWorkbenchMeta,
  scoreAnchorStyleSpeech,
  generateAnchorStyleRollingFeedback,
  runAnchorStyleAgentStudioTurn,
  testAnchorStyleSellingPointWheel,
} from '../api'
import type {
  AnchorStyleAgentStudioSessionResponse,
  AnchorStyleAgentStudioCatalogResponse,
  AnchorStyleAgentStudioTurnResponse,
  AnchorStyleScorecardResponse,
  AnchorStyleSpeechScoreResponse,
  AnchorStyleRollingFeedbackResponse,
  AnchorStyleWorkbenchContext,
  AnchorStyleWorkbenchMetaResponse,
  AnchorStyleSellingPointWheelResponse,
  AnchorStyleWheelTestResponse,
  LiveAgentPlan,
} from '../types'
import AnchorStyleScoreDetails from '../components/AnchorStyleScoreDetails.vue'
import AnchorCycleRunPanel from '../components/AnchorCycleRunPanel.vue'
import RiskText from '../components/RiskText.vue'
import { decodeTurns, encodeTurns, runAutoLoop, selectRememberedSlot, speechChars, wheelSlots } from '../anchorStyleAutoRun'
import type { WheelSlot } from '../anchorStyleAutoRun'

interface VisibleWheelTest {
  id: number
  response: AnchorStyleWheelTestResponse
  score?: AnchorStyleSpeechScoreResponse
  scoreError?: string
}

interface VisibleAgentStudioTurn {
  id: number
  response: AnchorStyleAgentStudioTurnResponse
  score?: AnchorStyleSpeechScoreResponse
  scoreError?: string
  rollingFeedback?: AnchorStyleRollingFeedbackResponse
  rollingFeedbackError?: string
}

const route = useRoute()
const plans = ref<LiveAgentPlan[]>([])
const selectedPlanID = ref(0)
const context = ref<AnchorStyleWorkbenchContext | null>(null)
const sampleText = ref(window.localStorage.getItem('anchor-style-workbench:sample') || '')
const analysisScene = ref('带货直播')
const compilerProfileID = ref('')
const scorecardCompilerProfileID = ref(window.localStorage.getItem('anchor-style-workbench:scorecard-compiler') || '')
const generatorProfileID = ref('')
const sellingPointProfileID = ref('')
const metaResult = ref<AnchorStyleWorkbenchMetaResponse | null>(null)
const metaPrompt = ref('')
const scorecardResult = ref<AnchorStyleScorecardResponse | null>(null)
const scorecardJSON = ref('')
const scorecardPrompt = ref('')
const scorecardPromptFileName = ref('')
const scorecardPromptMode = ref<'project' | 'manual'>('project')
const scorecardConfirmed = ref(false)
const scorecardCompiling = ref(false)
const scorecardConfirming = ref(false)
const includeLe = ref(false)
let applyingPersistedScorecard = false
const manualMetaPrompt = ref('')
const manualFileName = ref('')
const sellingPointPrompt = ref('')
const sellingPointPromptFileName = ref('')
const sellingPointPromptMode = ref<'project' | 'manual'>('project')
const sellingPointWheel = ref<AnchorStyleSellingPointWheelResponse | null>(null)
const sellingPointWheelJSON = ref('')
const sellingPointWheelConfirmed = ref(false)
const wheelPointer = ref(0)
const segmentMaxChars = ref(300)
const unlimitedChars = ref(window.localStorage.getItem('anchor-style-workbench:unlimited-chars') !== 'false')
const wheelTests = ref<VisibleWheelTest[]>([])
const testMode = ref<'agentstudio' | 'stateless'>((window.localStorage.getItem('anchor-style-workbench:test-mode') as 'agentstudio' | 'stateless') || 'agentstudio')
const agentStudioWorkspaceID = ref(window.localStorage.getItem('anchor-style-workbench:agentstudio-workspace') || '')
const agentStudioRegion = ref(window.localStorage.getItem('anchor-style-workbench:agentstudio-region') || 'cn-beijing')
const agentStudioAgentID = ref(window.localStorage.getItem('anchor-style-workbench:agentstudio-agent') || '')
const agentStudioEnvironmentID = ref(window.localStorage.getItem('anchor-style-workbench:agentstudio-environment') || '')
const agentStudioCredentialProfileID = ref(window.localStorage.getItem('anchor-style-workbench:agentstudio-credential') || '')
const agentStudioCatalog = ref<AnchorStyleAgentStudioCatalogResponse | null>(null)
const agentStudioCatalogLoading = ref(false)
const agentStudioFeedbackPrompt = ref(window.localStorage.getItem('anchor-style-workbench:agentstudio-feedback') || '')
const clearFeedbackAfterSuccess = ref(true)
const styleReviewing = ref(false)
const feedbackProfileID = ref(window.localStorage.getItem('anchor-style-workbench:rolling-feedback-profile') || '')
const rollingFeedbackPending = ref(false)
const agentStudioSession = ref<AnchorStyleAgentStudioSessionResponse | null>(null)
const agentStudioTurns = ref<VisibleAgentStudioTurn[]>([])
const agentStudioPointer = ref(0)
const agentStudioSessionStyleAssetID = ref(0)
const agentStudioSessionWheelAssetID = ref(0)
const agentStudioCreating = ref(false)
const agentStudioArchiving = ref(false)
const metaConfirmed = ref(false)
const loadingContext = ref(false)
const compiling = ref(false)
const confirming = ref(false)
const sellingPointCompiling = ref(false)
const sellingPointConfirming = ref(false)
const wheelTesting = ref(false)
const notice = ref('')
const error = ref('')
const showAdvanced = ref(false)
const runMode = ref<'cycle' | 'chars'>('cycle')
const cycleConfig = computed(() => ({ workspace_id: agentStudioWorkspaceID.value.trim(), region: agentStudioRegion.value || 'cn-beijing', agent_id: agentStudioAgentID.value, environment_id: agentStudioEnvironmentID.value, credential_profile_id: agentStudioCredentialProfileID.value, feedback_profile_id: feedbackProfileID.value }))
const autoTargetChars = ref(2000)
const autoSegmentChars = ref(300)
const autoPointGap = ref(2)
const autoCombinationGap = ref(5)
const autoMaxRounds = ref(500)
const autoRunning = ref(false)
const autoStopRequested = ref(false)
const autoStatus = ref<'idle' | 'running' | 'paused' | 'completed' | 'failed'>('idle')
const autoStartIndex = ref(0)
const autoStartedAt = ref('')
const generationInFlight = ref(false)
const generationUncertain = ref(false)
const autoPhase = ref('')
const cumulativeSpeech = computed(() => agentStudioTurns.value.slice(autoStartedAt.value ? autoStartIndex.value : 0).map(item => item.response.raw_response).join('\n\n'))
const autoChars = computed(() => agentStudioTurns.value.slice(autoStartIndex.value).reduce((sum, item) => sum + speechChars(item.response.raw_response), 0))
let applyingPersistedMeta = false
let applyingPersistedWheel = false

const selectedPlan = computed(() => plans.value.find(item => item.id === selectedPlanID.value) || null)
const enabledModels = computed(() => context.value?.models.filter(item => item.enabled) || [])
const scorecardModels = computed(() => enabledModels.value)
const canCompile = computed(() => selectedPlanID.value > 0 && sampleText.value.trim().length >= 100 && Boolean(scorecardCompilerProfileID.value) && !compiling.value && !scorecardCompiling.value && !wheelTesting.value)
const canCompileScorecard = computed(() => selectedPlanID.value > 0 && Boolean(metaResult.value?.asset_id) && Boolean(scorecardCompilerProfileID.value) && sampleText.value.trim().length >= 100 && !compiling.value && !scorecardCompiling.value && !wheelTesting.value)
const canStageManualMeta = computed(() => selectedPlanID.value > 0 && Boolean(manualMetaPrompt.value.trim()) && !confirming.value)
const canCompileSellingPoints = computed(() => selectedPlanID.value > 0 && (sellingPointPromptMode.value === 'project' || sellingPointPrompt.value.trim().length >= 100) && Boolean(context.value?.fact_package.trim()) && !sellingPointCompiling.value)
const canConfirmSellingPoints = computed(() => Boolean(sellingPointWheel.value?.asset_id) && !sellingPointWheel.value?.validation_error && sellingPointWheel.value?.status !== 'validation_failed' && Boolean(sellingPointWheelJSON.value.trim()) && !sellingPointConfirming.value)
const confirmedAssetsReady = computed(() => !loadingContext.value && context.value?.plan_id === selectedPlanID.value && metaConfirmed.value && scorecardConfirmed.value && scorecardResult.value?.meta_prompt_asset_id === metaResult.value?.asset_id && sellingPointWheelConfirmed.value)
const idleForGeneration = computed(() => !wheelTesting.value && !autoRunning.value && !generationUncertain.value && !styleReviewing.value && !rollingFeedbackPending.value)
const canTestWheel = computed(() => confirmedAssetsReady.value && idleForGeneration.value && (unlimitedChars.value || (segmentMaxChars.value >= 80 && segmentMaxChars.value <= 1000)))
const agentStudioConfigReady = computed(() => Boolean(
	agentStudioWorkspaceID.value.trim()
	&& agentStudioAgentID.value.trim()
	&& agentStudioCredentialProfileID.value
))
const selectedAgentStudioAgent = computed(() => agentStudioCatalog.value?.agents.find(item => item.id === agentStudioAgentID.value) || null)
const selectedAgentStudioAgentIsAuto = computed(() => /(^|[^a-z])auto([^a-z]|$)/i.test(selectedAgentStudioAgent.value?.model || ''))
const canLoadAgentStudioCatalog = computed(() => Boolean(agentStudioWorkspaceID.value.trim() && agentStudioCredentialProfileID.value && !agentStudioCatalogLoading.value && !agentStudioSession.value))
const agentStudioSessionStale = computed(() => Boolean(agentStudioSession.value) && (
	agentStudioSessionStyleAssetID.value !== (metaResult.value?.asset_id || 0)
	|| agentStudioSessionWheelAssetID.value !== (sellingPointWheel.value?.asset_id || 0)
))
const canCreateAgentStudioSession = computed(() => confirmedAssetsReady.value && idleForGeneration.value && agentStudioConfigReady.value && !agentStudioSession.value && !agentStudioCreating.value)
const canRunAgentStudioTurn = computed(() => canTestWheel.value && Boolean(agentStudioSession.value) && !agentStudioSessionStale.value && !agentStudioCreating.value && !agentStudioArchiving.value && !styleReviewing.value)
const canStartAuto = computed(() => confirmedAssetsReady.value && idleForGeneration.value && agentStudioConfigReady.value && Boolean(feedbackProfileID.value) && !agentStudioSessionStale.value && !agentStudioCreating.value && !agentStudioArchiving.value)
const compilerRouteLabel = computed(() => {
  const routeModel = context.value?.analysis_model || context.value?.analysis_provider || '未配置'
  return `系统风格分析路由（${routeModel}）`
})

async function loadPlans() {
  error.value = ''
  try {
    const response = await getLiveAgentPlans()
    plans.value = response.items.filter(item => item.status !== 'archived')
    const queryPlan = Number(route.query.plan || window.localStorage.getItem('anchor-style-workbench:selected-plan') || 0)
    if (queryPlan && plans.value.some(item => item.id === queryPlan)) selectedPlanID.value = queryPlan
    else if (!selectedPlanID.value && plans.value.length) {
      const roomIDs = [...new Set(plans.value.flatMap(item => item.room_ids || []).filter(id => id > 0))]
      let currentPlanID = 0
      for (const roomID of roomIDs) {
        try {
          const current = await getRoomLiveAgentPlan(roomID)
          if (current.plan && plans.value.some(item => item.id === current.plan?.id)) {
            currentPlanID = current.plan.id
            break
          }
        } catch {
          // Fall back to the active plan list if a room is temporarily unavailable.
        }
      }
      selectedPlanID.value = currentPlanID || plans.value[0].id
      if (currentPlanID) notice.value = '已自动读取直播间当前选中并发布的方案。'
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取直播方案失败'
  }
}

async function loadContext() {
  if (!selectedPlanID.value) return
  loadingContext.value = true
  error.value = ''
  try {
    context.value = await getAnchorStyleWorkbenchContext(selectedPlanID.value)
    if (!context.value.models.some(item => item.id === feedbackProfileID.value && item.enabled && item.has_api_key)) {
      feedbackProfileID.value = context.value.models.find(item => item.enabled && item.has_api_key && /flash|turbo|lite/i.test(item.model))?.id || context.value.models.find(item => item.enabled && item.has_api_key)?.id || ''
    }
    const savedScorecardCompiler = context.value.models.find(item => item.id === scorecardCompilerProfileID.value && item.enabled && item.has_api_key)
    if (!savedScorecardCompiler) scorecardCompilerProfileID.value = context.value.default_scorecard_compiler_id || context.value.models.find(item => item.enabled && item.has_api_key)?.id || ''
    const qwenMax = context.value.models.find(item => item.enabled && /qwen[\s._-]*3[.]?8.*max/i.test(`${item.name} ${item.model}`))
	compilerProfileID.value = qwenMax?.id || context.value.default_generator_id || ''
    generatorProfileID.value = qwenMax?.id || context.value.default_generator_id || ''
    sellingPointProfileID.value = qwenMax?.id || context.value.default_generator_id || ''
	const credentialExists = context.value.models.some(item => item.enabled && item.has_api_key && item.id === agentStudioCredentialProfileID.value)
	if (!credentialExists) agentStudioCredentialProfileID.value = (qwenMax?.has_api_key ? qwenMax.id : context.value.models.find(item => item.enabled && item.has_api_key)?.id) || ''
	if (agentStudioWorkspaceID.value.trim() && agentStudioCredentialProfileID.value && !agentStudioSession.value) void loadAgentStudioCatalog(true)
	applyingPersistedWheel = true
	if (context.value.saved_selling_point_wheel) {
	  sellingPointWheel.value = context.value.saved_selling_point_wheel
	  sellingPointWheelJSON.value = context.value.saved_selling_point_wheel.wheel_json
	  sellingPointWheelConfirmed.value = context.value.saved_selling_point_wheel.status === 'confirmed'
	  sellingPointPrompt.value = context.value.selling_point_prompt_md
	} else {
	  sellingPointWheel.value = null
	  sellingPointWheelJSON.value = ''
	  sellingPointWheelConfirmed.value = false
	  sellingPointPrompt.value = context.value.selling_point_prompt_md
	}
	await nextTick()
	applyingPersistedWheel = false
	sellingPointPromptMode.value = 'project'
	scorecardPromptMode.value = 'project'
	wheelPointer.value = 0
	wheelTests.value = []
    if (context.value.saved_meta_prompt) {
	  applyingPersistedMeta = true
      metaResult.value = context.value.saved_meta_prompt
      metaPrompt.value = context.value.saved_meta_prompt.meta_prompt_md
      metaConfirmed.value = context.value.saved_meta_prompt.status === 'confirmed'
	  await nextTick()
	  applyingPersistedMeta = false
      notice.value = metaConfirmed.value
        ? '已从数据库读取当前生效的原始风格基准。'
        : '已从数据库读取待确认的原始风格元提示词。'
    } else {
	  applyingPersistedMeta = true
      metaResult.value = null
      metaPrompt.value = ''
      metaConfirmed.value = false
	  await nextTick()
	  applyingPersistedMeta = false
    }
    await applyScorecard(context.value.saved_scorecard || null)
    scorecardPrompt.value = context.value.scorecard_prompt_md
    scorecardPromptFileName.value = ''
	if (!agentStudioSession.value) restoreAgentStudioState()
  } catch (value) {
    context.value = null
    error.value = value instanceof Error ? value.message : '读取调教上下文失败'
  } finally {
    loadingContext.value = false
  }
}

async function loadAgentStudioCatalog(quiet = false) {
	if (!canLoadAgentStudioCatalog.value) return
	agentStudioCatalogLoading.value = true
	if (!quiet) {
		error.value = ''
		notice.value = '正在从阿里 Agent Studio 读取智能体和环境列表…'
	}
	try {
		const result = await getAnchorStyleAgentStudioCatalog(selectedPlanID.value, {
			workspace_id: agentStudioWorkspaceID.value.trim(),
			region: agentStudioRegion.value.trim() || 'cn-beijing',
			credential_profile_id: agentStudioCredentialProfileID.value,
		})
		if (agentStudioSession.value) return
		agentStudioCatalog.value = result
		const currentAgent = result.agents.find(item => item.id === agentStudioAgentID.value)
		const preferredAgent = result.agents.find(item => /带货主播/.test(item.name) && /qwen[\s._-]*3[.]?8.*max/i.test(item.model))
			|| result.agents.find(item => /qwen[\s._-]*3[.]?8.*max/i.test(item.model))
			|| result.agents[0]
		if (!currentAgent || (/auto/i.test(currentAgent.model) && preferredAgent)) agentStudioAgentID.value = preferredAgent?.id || ''
		const currentEnvironment = result.environments.find(item => item.id === agentStudioEnvironmentID.value)
		const preferredEnvironment = result.environments.find(item => /默认|default/i.test(item.name)) || result.environments[0]
		if (!currentEnvironment) agentStudioEnvironmentID.value = preferredEnvironment?.id || ''
		if (!quiet) notice.value = `已读取 ${result.agents.length} 个智能体、${result.environments.length} 个环境；当前选择 ${preferredAgent?.name || '未找到智能体'}。`
	} catch (value) {
		agentStudioCatalog.value = null
		error.value = value instanceof Error ? value.message : '读取阿里智能体列表失败'
		if (!quiet) notice.value = ''
	} finally {
		agentStudioCatalogLoading.value = false
	}
}

async function createAgentStudioSession() {
	if (!canCreateAgentStudioSession.value) return
	agentStudioCreating.value = true
	error.value = ''
	notice.value = '正在创建阿里 Agent Studio 独立会话；风格和事实将在第一次测试时只注入一次。'
	try {
		agentStudioSession.value = await createAnchorStyleAgentStudioSession(selectedPlanID.value, {
			workspace_id: agentStudioWorkspaceID.value.trim(),
			region: agentStudioRegion.value.trim() || 'cn-beijing',
			agent_id: agentStudioAgentID.value.trim(),
			environment_id: agentStudioEnvironmentID.value.trim(),
			credential_profile_id: agentStudioCredentialProfileID.value,
		})
		agentStudioEnvironmentID.value = agentStudioSession.value.environment_id
		agentStudioTurns.value = []
		agentStudioPointer.value = 0
		agentStudioSessionStyleAssetID.value = metaResult.value?.asset_id || 0
		agentStudioSessionWheelAssetID.value = sellingPointWheel.value?.asset_id || 0
		resetAutoState()
		persistAgentStudioState()
		notice.value = `阿里连续会话 ${agentStudioSession.value.session_id} 已创建，已锁定智能体 v${agentStudioSession.value.agent_version}；点击测试将注入风格、事实和第一格。`
	} catch (value) {
		error.value = value instanceof Error ? value.message : '创建阿里连续会话失败'
		notice.value = ''
	} finally {
		agentStudioCreating.value = false
	}
}

async function runAgentStudioTurn(pointer?: number) {
	if (!canRunAgentStudioTurn.value || !agentStudioSession.value) return
	wheelTesting.value = true
	error.value = ''
	try {
		if (pointer === undefined) {
			const slot = rememberedSlot()
			pointer = Math.floor(agentStudioTurns.value.length / wheelSlots(sellingPointWheelJSON.value).length) * wheelSlots(sellingPointWheelJSON.value).length + slot.index
		}
		const turn = await performAgentStudioTurn(pointer, unlimitedChars.value ? 0 : segmentMaxChars.value)
		await scoreGeneratedSpeech(turn)
	} catch (value) {
		error.value = value instanceof Error ? value.message : '阿里连续会话生成失败'
	} finally { wheelTesting.value = false }
}

async function performAgentStudioTurn(pointer: number, maxChars: number): Promise<VisibleAgentStudioTurn> {
	if (!agentStudioSession.value) throw new Error('请先建立阿里会话')
	const firstTurn = agentStudioTurns.value.length === 0
	const feedbackForTurn = agentStudioFeedbackPrompt.value.trim()
	if ([...feedbackForTurn].length > 4000) throw new Error('回灌提示词超过4000字，请调整模板')
	generationInFlight.value = true
	try { persistAgentStudioState() } catch (value) { generationInFlight.value = false; throw value }
	try {
		const response = await runAnchorStyleAgentStudioTurn(selectedPlanID.value, agentStudioSession.value.session_id, {
			workspace_id: agentStudioWorkspaceID.value.trim(),
			region: agentStudioRegion.value.trim() || 'cn-beijing',
			credential_profile_id: agentStudioCredentialProfileID.value,
			pointer,
			max_chars: maxChars,
			unlimited_chars: maxChars === 0,
			first_turn: firstTurn,
			feedback_prompt: feedbackForTurn || undefined,
		})
		const visibleTurn: VisibleAgentStudioTurn = { id: Date.now(), response }
		agentStudioTurns.value.push(visibleTurn)
		agentStudioPointer.value = response.next_pointer
		generationInFlight.value = false
		generationUncertain.value = false
		if (feedbackForTurn && clearFeedbackAfterSuccess.value && agentStudioFeedbackPrompt.value.trim() === feedbackForTurn) agentStudioFeedbackPrompt.value = ''
		persistAgentStudioState()
		notice.value = firstTurn
			? `首轮完成：风格元提示词、事实包和 ${response.task.selling_point} × ${response.task.angle} 已一次性注入会话${response.feedback_applied ? '，并执行了本轮回灌' : ''}。`
			: `连续会话第 ${agentStudioTurns.value.length} 轮完成：本轮发送 ${response.task.selling_point} × ${response.task.angle}${response.feedback_applied ? ' + 回灌纠偏' : ''}。`
		await nextTick()
		return visibleTurn
	} catch (value) {
		if (generationInFlight.value) generationUncertain.value = true
		generationInFlight.value = false
		try { persistAgentStudioState() } catch { /* Original failure remains visible. */ }
		throw value
	}
}

async function scoreGeneratedSpeech(item: VisibleAgentStudioTurn | VisibleWheelTest) {
	if (styleReviewing.value || !item.response.raw_response.trim()) return
	styleReviewing.value = true
	item.scoreError = ''
	try {
		if (!scorecardResult.value || !scorecardConfirmed.value) throw new Error('请先确认主播专属评分基准')
		item.score = await scoreAnchorStyleSpeech(selectedPlanID.value, {
			scorecard_asset_id: scorecardResult.value.asset_id,
			meta_prompt_asset_id: 'style_asset_id' in item.response ? item.response.style_asset_id : (metaResult.value?.asset_id || 0),
			generated_text: item.response.raw_response,
			task: item.response.task,
		})
		notice.value = '本段评分完成：' + item.score.score.overall_score + '/100，已展示评分 JSON' + (item.score.feedback?.status === 'ready' ? '和单项回灌草稿；尚未发送。' : '。')
		persistAgentStudioState()
		return item.score
	} catch (value) {
		item.scoreError = value instanceof Error ? value.message : '风格评分失败'
	} finally {
		styleReviewing.value = false
	}
}

function resetAutoState() {
	autoStatus.value = 'idle'; autoStartedAt.value = ''; autoStartIndex.value = 0
	autoStopRequested.value = false; generationUncertain.value = false; generationInFlight.value = false
}

function rememberedSlot(): WheelSlot {
	return selectRememberedSlot(wheelSlots(sellingPointWheelJSON.value), agentStudioTurns.value.map(item => ({
		sellingPoint: item.response.task.selling_point, key: item.response.task.key,
	})), { pointGap: autoPointGap.value, combinationGap: autoCombinationGap.value })
}

function persistAgentStudioState(planID = selectedPlanID.value) {
	if (!agentStudioSession.value || !planID) return
	const { records, baselines } = encodeTurns(agentStudioTurns.value)
	try {
		window.localStorage.setItem('anchor-style-workbench:run:' + planID, JSON.stringify({
			version: 1, plan_id: planID, session: agentStudioSession.value,
			style_id: agentStudioSessionStyleAssetID.value, wheel_id: agentStudioSessionWheelAssetID.value,
			workspace_id: agentStudioWorkspaceID.value, region: agentStudioRegion.value,
			credential_id: agentStudioCredentialProfileID.value, agent_id: agentStudioAgentID.value,
			pointer: agentStudioPointer.value, records, baselines, feedback: agentStudioFeedbackPrompt.value,
			feedback_profile_id: feedbackProfileID.value,
			in_flight: generationInFlight.value, uncertain: generationUncertain.value,
			auto: { target: autoTargetChars.value, segment: autoSegmentChars.value, point_gap: autoPointGap.value,
				combination_gap: autoCombinationGap.value, max_rounds: autoMaxRounds.value, status: autoStatus.value,
				start_index: autoStartIndex.value, started_at: autoStartedAt.value },
		}))
	} catch { throw new Error('本地测试记录保存失败，已停止继续请求；请先导出完整测试，避免丢失原文') }
}

function restoreAgentStudioState() {
	try {
		const saved = JSON.parse(window.localStorage.getItem('anchor-style-workbench:run:' + selectedPlanID.value) || 'null')
		if (!saved || saved.version !== 1 || saved.plan_id !== selectedPlanID.value || !saved.session || !Array.isArray(saved.records)) return
		agentStudioSession.value = saved.session
		agentStudioSessionStyleAssetID.value = saved.style_id; agentStudioSessionWheelAssetID.value = saved.wheel_id
		agentStudioWorkspaceID.value = saved.workspace_id; agentStudioRegion.value = saved.region
		agentStudioCredentialProfileID.value = saved.credential_id; agentStudioAgentID.value = saved.agent_id
		agentStudioTurns.value = decodeTurns<VisibleAgentStudioTurn>(saved.records, saved.baselines || {})
		agentStudioPointer.value = saved.pointer; agentStudioFeedbackPrompt.value = saved.feedback || ''
		if (context.value?.models.some(item => item.id === saved.feedback_profile_id && item.enabled && item.has_api_key)) feedbackProfileID.value = saved.feedback_profile_id
		generationUncertain.value = Boolean(saved.in_flight || saved.uncertain)
		autoTargetChars.value = saved.auto.target; autoSegmentChars.value = saved.auto.segment
		autoPointGap.value = saved.auto.point_gap; autoCombinationGap.value = saved.auto.combination_gap
		autoMaxRounds.value = saved.auto.max_rounds; autoStartIndex.value = saved.auto.start_index
		autoStartedAt.value = saved.auto.started_at; autoStatus.value = saved.auto.status === 'running' ? 'paused' : saved.auto.status
		notice.value = '已恢复当前方案的会话、已讲记忆和累计话术；不会自动恢复请求。'
		if (generationUncertain.value) error.value = '刷新前有一轮请求尚未确认结束。为避免重复请求，已阻止续跑；请到高级设置核对或归档重建会话。'
	} catch { error.value = '本地测试记录无法恢复，请保留记录并在高级设置核对会话。' }
}

async function acceptAutoFeedback(turn: VisibleAgentStudioTurn) {
	agentStudioFeedbackPrompt.value = ''
	if (autoStopRequested.value || autoChars.value >= autoTargetChars.value) { persistAgentStudioState(); return }
	const plan = selectedPlanID.value, session = agentStudioSession.value?.session_id
	const style = metaResult.value?.asset_id, scorecard = scorecardResult.value?.asset_id, profile = feedbackProfileID.value
	if (!style || !scorecard || !profile || !session) throw new Error('缺少当前风格、基准或回灌模型')
	const index = agentStudioTurns.value.indexOf(turn)
	if (index < 0) throw new Error('回灌原文不属于当前会话')
	const start = Math.max(0, index - 4)
	const cached = turn.rollingFeedback
	if (cached && !turn.rollingFeedbackError && ['ready', 'no_issue'].includes(cached.status) && cached.profile_id === profile && cached.style_asset_id === style && cached.scorecard_asset_id === scorecard && cached.report.edges.at(-1)?.index === index + 1) {
		agentStudioFeedbackPrompt.value = cached.status === 'ready' ? cached.prompt : ''
		persistAgentStudioState(); return
	}
	rollingFeedbackPending.value = true; turn.rollingFeedbackError = ''; autoPhase.value = '滑动窗口：开头、风格、结尾回灌'
	try {
		turn.rollingFeedback = await generateAnchorStyleRollingFeedback(plan, {
			feedback_profile_id: profile, meta_prompt_asset_id: style, scorecard_asset_id: scorecard,
			recent_texts: agentStudioTurns.value.slice(start, index + 1).map(item => item.response.raw_response), start_index: start + 1,
		})
		if (selectedPlanID.value !== plan || agentStudioSession.value?.session_id !== session || metaResult.value?.asset_id !== style || scorecardResult.value?.asset_id !== scorecard || feedbackProfileID.value !== profile) throw new Error('回灌期间方案、会话或模型配置发生变化')
		if (!['ready', 'no_issue'].includes(turn.rollingFeedback.status)) throw new Error(turn.rollingFeedback.error || '快速模型回灌失败')
		agentStudioFeedbackPrompt.value = turn.rollingFeedback.status === 'ready' ? turn.rollingFeedback.prompt : ''
	} catch (value) {
		turn.rollingFeedbackError = value instanceof Error ? value.message : '滑动回灌失败'
		throw new Error(turn.rollingFeedbackError)
	} finally { rollingFeedbackPending.value = false; persistAgentStudioState() }
}

async function startAutoGeneration(resume = false) {
	if (!canStartAuto.value) return
	testMode.value = 'agentstudio'
	if (!Number.isInteger(autoTargetChars.value) || autoTargetChars.value < 1 || autoTargetChars.value > 100000) { error.value = '目标字数须为1到100000的整数'; return }
	if (!Number.isInteger(autoSegmentChars.value) || autoSegmentChars.value < 80 || autoSegmentChars.value > 1000) { error.value = '自动单段上限须为80到1000字'; return }
	if (!Number.isInteger(autoMaxRounds.value) || autoMaxRounds.value < 1 || autoMaxRounds.value > 500) { error.value = '安全轮数须为1到500的整数'; return }
	try { rememberedSlot() } catch (value) { error.value = value instanceof Error ? value.message : '调度失败'; return }
	error.value = ''; autoStopRequested.value = false
	if (!agentStudioSession.value) await createAgentStudioSession()
	if (!agentStudioSession.value || agentStudioSessionStale.value) return
	if (!resume) { autoStartIndex.value = agentStudioTurns.value.length; autoStartedAt.value = new Date().toISOString() }
	const boundPlan = selectedPlanID.value, boundSession = agentStudioSession.value.session_id
	const boundStyle = metaResult.value?.asset_id, boundWheel = sellingPointWheel.value?.asset_id, boundScore = scorecardResult.value?.asset_id
	autoRunning.value = true; autoStatus.value = 'running'
	const assertContext = () => {
		if (selectedPlanID.value !== boundPlan || agentStudioSession.value?.session_id !== boundSession || metaResult.value?.asset_id !== boundStyle || sellingPointWheel.value?.asset_id !== boundWheel || scorecardResult.value?.asset_id !== boundScore || !metaConfirmed.value || !scorecardConfirmed.value || !sellingPointWheelConfirmed.value) throw new Error('方案、风格或基准已变化，自动生成已暂停')
	}
	try {
		persistAgentStudioState()
		const last = agentStudioTurns.value.at(-1)
		if (last && (!last.score?.feedback || last.scoreError || last.score.scorecard_asset_id !== boundScore)) {
			autoPhase.value = '补做上一段评分'
			if (!await scoreGeneratedSpeech(last)) throw new Error(last.scoreError || '评分失败')
			await acceptAutoFeedback(last)
		} else if (last) await acceptAutoFeedback(last)
		autoStatus.value = await runAutoLoop({
			targetChars: autoTargetChars.value, maxRounds: autoMaxRounds.value, segmentMaxChars: autoSegmentChars.value,
			stopped: () => autoStopRequested.value, generatedChars: () => autoChars.value,
			generatedRounds: () => agentStudioTurns.value.length - autoStartIndex.value, assertContext,
			select: rememberedSlot,
			generate: async (slot, maxChars) => {
				autoPhase.value = '生成当前段'
				const size = wheelSlots(sellingPointWheelJSON.value).length
				return performAgentStudioTurn(Math.floor(agentStudioTurns.value.length / size) * size + slot.index, maxChars)
			},
			receive: () => {},
			score: async turn => {
				autoPhase.value = '评分并生成回灌'
				if (!await scoreGeneratedSpeech(turn)) throw new Error(turn.scoreError || '评分失败，已暂停')
			},
			feedback: acceptAutoFeedback,
		})
		notice.value = autoStatus.value === 'completed' ? `已达到目标，累计${autoChars.value}字；保留完整原文，没有截断。` : '已暂停，原文和记忆已保存。'
	} catch (value) {
		autoStatus.value = 'failed'; error.value = value instanceof Error ? value.message : '自动循环失败，已停止'
	} finally {
		autoRunning.value = false; autoPhase.value = ''
		try { persistAgentStudioState() } catch (value) { error.value = value instanceof Error ? value.message : '记录保存失败' }
	}
}

function pauseAutoGeneration() {
	autoStopRequested.value = true
	notice.value = '正在暂停：收完当前段并保存评分后停止，不会再发下一轮。'
}

function applyGeneratedFeedback(item: VisibleAgentStudioTurn, prompt: string) {
	if (autoRunning.value || wheelTesting.value || styleReviewing.value || !prompt.trim()) return
	if (!agentStudioSession.value || item.response.session_id !== agentStudioSession.value.session_id || item.score?.scorecard_asset_id !== scorecardResult.value?.asset_id || item.score?.meta_prompt_asset_id !== metaResult.value?.asset_id) {
		error.value = '这段评分不属于当前会话或当前基准，请使用当前会话的新评分。'
		return
	}
	agentStudioFeedbackPrompt.value = prompt
	notice.value = '回灌草稿已填入下一轮回灌框，可修改；尚未发送，点击下一轮测试才会使用。'
}


async function retryAgentStudioTurn(item: VisibleAgentStudioTurn) {
	if (wheelTesting.value) return
	const previousPointer = agentStudioPointer.value
	await runAgentStudioTurn(item.response.task.pointer)
	agentStudioPointer.value = previousPointer
}

function clearAgentStudioSession(removeSaved = true) {
	if (removeSaved) window.localStorage.removeItem('anchor-style-workbench:run:' + selectedPlanID.value)
	agentStudioSession.value = null
	agentStudioTurns.value = []
	agentStudioPointer.value = 0
	agentStudioSessionStyleAssetID.value = 0
	agentStudioSessionWheelAssetID.value = 0
	resetAutoState()
}

async function archiveAgentStudioSession(planID = selectedPlanID.value, quiet = false) {
	if (!agentStudioSession.value || agentStudioArchiving.value) {
		clearAgentStudioSession()
		return
	}
	agentStudioArchiving.value = true
	const sessionID = agentStudioSession.value.session_id
	try {
		await archiveAnchorStyleAgentStudioSession(planID, sessionID, {
			workspace_id: agentStudioWorkspaceID.value.trim(),
			region: agentStudioRegion.value.trim() || 'cn-beijing',
			credential_profile_id: agentStudioCredentialProfileID.value,
		})
		if (!quiet) notice.value = `阿里会话 ${sessionID} 已归档；下次会创建全新的上下文。`
	} catch (value) {
		if (!quiet) error.value = value instanceof Error ? value.message : '归档阿里连续会话失败'
	} finally {
		clearAgentStudioSession()
		agentStudioArchiving.value = false
	}
}

async function resetAllTests() {
	if (autoRunning.value || wheelTesting.value || styleReviewing.value) return
	resetWheelTests()
	await archiveAgentStudioSession()
}

async function loadSellingPointPromptFile(event: Event) {
	const input = event.target as HTMLInputElement
	const file = input.files?.[0]
	if (!file) return
	error.value = ''
	try {
		sellingPointPrompt.value = await file.text()
		sellingPointPromptFileName.value = file.name
		sellingPointPromptMode.value = 'manual'
		notice.value = '卖点挖掘提示词已加载；生成时会与当前正式事实包一起发送给所选模型。'
	} catch {
		error.value = '读取卖点挖掘提示词文件失败'
	} finally {
		input.value = ''
	}
}

async function restoreDefaultSellingPointPrompt() {
	if (!context.value) return
	sellingPointPromptMode.value = 'project'
	try {
	 const fresh = await getAnchorStyleWorkbenchContext(selectedPlanID.value)
	 sellingPointPrompt.value = fresh.selling_point_prompt_md
	 context.value.selling_point_prompt_source = fresh.selling_point_prompt_source
	 sellingPointPromptFileName.value = ''
	 notice.value = '已重新读取项目最新母版；生成请求时后端还会现读一次。'
	} catch (value) { error.value = value instanceof Error ? value.message : '读取最新母版失败' }
}

async function compileSellingPointWheel() {
	if (!canCompileSellingPoints.value) return
	sellingPointCompiling.value = true
	error.value = ''
	notice.value = '正在把卖点挖掘提示词和当前事实包交给所选模型…'
	try {
		const result = await compileAnchorStyleSellingPointWheel(selectedPlanID.value, {
			prompt_mode: sellingPointPromptMode.value,
			prompt_md: sellingPointPromptMode.value === 'manual' ? sellingPointPrompt.value : undefined,
			compiler_profile_id: sellingPointProfileID.value || undefined,
		})
		applyingPersistedWheel = true
		sellingPointWheel.value = result
		sellingPointWheelJSON.value = result.wheel_json
		sellingPointWheelConfirmed.value = false
		if (sellingPointPromptMode.value === 'project') sellingPointPrompt.value = result.extraction_prompt_md
		await nextTick()
		applyingPersistedWheel = false
		if (result.validation_error) { error.value = result.validation_error; notice.value = '模型原始返回已保存，字段不符合本次母版，不能确认为生效轮盘。'; return }
		notice.value = `卖点轮盘原始版已落库（资产 #${result.asset_id}，${result.selling_point_count} 个卖点、${result.combination_count} 个组合）。请检查后确认保存。`
	} catch (value) {
		error.value = value instanceof Error ? value.message : '卖点轮盘生成失败'
		notice.value = ''
	} finally {
		sellingPointCompiling.value = false
	}
}

async function confirmSellingPointWheel() {
	if (!canConfirmSellingPoints.value || !sellingPointWheel.value) return
	sellingPointConfirming.value = true
	error.value = ''
	try {
		const result = await confirmAnchorStyleSellingPointWheel(selectedPlanID.value, sellingPointWheel.value.asset_id, {
			wheel_json: sellingPointWheelJSON.value,
		})
		applyingPersistedWheel = true
		sellingPointWheel.value = result
		sellingPointWheelJSON.value = result.wheel_json
		sellingPointWheelConfirmed.value = true
		await nextTick()
		applyingPersistedWheel = false
		notice.value = `卖点轮盘资产 #${result.asset_id} 已绑定当前智能体方案；以后选择该方案会自动读取。`
	} catch (value) {
		error.value = value instanceof Error ? value.message : '保存卖点轮盘失败'
	} finally {
		sellingPointConfirming.value = false
	}
}

async function runWheelTest(pointer = wheelPointer.value) {
	if (!canTestWheel.value) return
	wheelTesting.value = true
	error.value = ''
	try {
		const response = await testAnchorStyleSellingPointWheel(selectedPlanID.value, {
			pointer,
			max_chars: unlimitedChars.value ? 0 : segmentMaxChars.value,
			unlimited_chars: unlimitedChars.value,
			generator_profile_id: generatorProfileID.value || undefined,
		})
		const visibleTest: VisibleWheelTest = { id: Date.now(), response }
		wheelTests.value.push(visibleTest)
		wheelPointer.value = response.next_pointer
		notice.value = `第 ${response.task.pointer + 1} 次单格测试完成：${response.task.selling_point} × ${response.task.angle}。`
		await nextTick()
		document.querySelector('.wheel-test-results')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
		await scoreGeneratedSpeech(visibleTest)
	} catch (value) {
		error.value = value instanceof Error ? value.message : '轮盘单格测试失败'
	} finally {
		wheelTesting.value = false
	}
}

async function retryWheelTest(item: VisibleWheelTest) {
	if (wheelTesting.value) return
	const previousPointer = wheelPointer.value
	await runWheelTest(item.response.task.pointer)
	wheelPointer.value = previousPointer
}

function resetWheelTests() {
	wheelPointer.value = 0
	wheelTests.value = []
	notice.value = '卖点轮盘测试指针已回到第一格。'
}

async function applyScorecard(result: AnchorStyleScorecardResponse | null) {
	applyingPersistedScorecard = true
	scorecardResult.value = result
	scorecardJSON.value = result?.scorecard_json || ''
	scorecardConfirmed.value = result?.status === 'confirmed' && result.calibration_version === 'qualitative-quantitative-v2'
	try { includeLe.value = Boolean(JSON.parse(result?.scorecard_json || '{}')['量化基准']?.['了_计入语气词']) } catch { includeLe.value = false }
	await nextTick()
	applyingPersistedScorecard = false
}

async function compileScorecard() {
	if (!metaResult.value?.asset_id || scorecardCompiling.value) return
	scorecardCompiling.value = true
	error.value = ''
	const selectedModel = scorecardModels.value.find(item => item.id === scorecardCompilerProfileID.value)
	notice.value = '正在把通用基准提取提示词与主播样本交给 ' + (selectedModel?.model || '选中的评分基准提取模型') + '…'
	try {
		const result = await compileAnchorStyleScorecard(selectedPlanID.value, {
			sample_text: sampleText.value,
			prompt_mode: scorecardPromptMode.value,
			prompt_md: scorecardPromptMode.value === 'manual' ? scorecardPrompt.value : undefined,
			compiler_profile_id: scorecardCompilerProfileID.value || undefined,
			meta_prompt_asset_id: metaResult.value.asset_id,
			include_le: includeLe.value,
		})
		await applyScorecard(result)
		if (scorecardPromptMode.value === 'project') scorecardPrompt.value = result.extraction_prompt_md
		if (result.calibration_error) {
			error.value = '模型原始返回已保存，但校对失败：' + result.calibration_error
			notice.value = ''
			return
		}
		notice.value = '主播专属评分基准已保存为资产 #' + result.asset_id + '；请查看基准表与原始返回，确认后启用逐段打分。'
	} catch (value) {
		error.value = value instanceof Error ? value.message : '评分基准提取失败'
		notice.value = '已有风格元提示词保留；可单独重试评分基准提取。'
	} finally {
		scorecardCompiling.value = false
	}
}

async function confirmScorecard() {
	if (!scorecardResult.value || scorecardConfirming.value || !scorecardJSON.value.trim()) return
	scorecardConfirming.value = true
	error.value = ''
	try {
		JSON.parse(scorecardJSON.value)
		const result = await confirmAnchorStyleScorecard(selectedPlanID.value, scorecardResult.value.asset_id, scorecardJSON.value, includeLe.value)
		await applyScorecard(result)
		notice.value = '评分基准已确认并保存；每段话术返回后将自动显示程序评分和逐项扣分明细。'
	} catch (value) {
		error.value = value instanceof Error ? value.message : '保存评分基准失败'
	} finally {
		scorecardConfirming.value = false
	}
}

async function recalibrateScorecard() {
	if (!scorecardResult.value || scorecardCompiling.value || wheelTesting.value) return
	scorecardCompiling.value = true
	error.value = ''
	try {
		const result = await calibrateAnchorStyleScorecard(selectedPlanID.value, scorecardResult.value.asset_id, {
			sample_text: sampleText.value || undefined,
			include_le: includeLe.value,
		})
		await applyScorecard(result)
		notice.value = '已复用保存的模型原始返回，按全文重新生成校对基准；没有调用模型。请检查后确认。'
	} catch (value) {
		error.value = value instanceof Error ? value.message : '校对失败'
	} finally { scorecardCompiling.value = false }
}

async function loadScorecardPromptFile(event: Event) {
	const input = event.target as HTMLInputElement
	const file = input.files?.[0]
	if (!file) return
	try {
		scorecardPrompt.value = await file.text()
		scorecardPromptFileName.value = file.name
		scorecardPromptMode.value = 'manual'
		notice.value = '基准提取提示词已加载；下次提取时与真实主播样本一起发送。'
	} catch {
		error.value = '读取基准提取提示词文件失败'
	} finally { input.value = '' }
}

async function restoreDefaultScorecardPrompt() {
  scorecardPromptMode.value = 'project'
  try {
    const fresh = await getAnchorStyleWorkbenchContext(selectedPlanID.value)
    scorecardPrompt.value = fresh.scorecard_prompt_md
    if (context.value) context.value.scorecard_prompt_source = fresh.scorecard_prompt_source
    scorecardPromptFileName.value = ''
    notice.value = '已重新读取评分基准最新母版；提取时后端会再次现读。'
  } catch (value) { error.value = value instanceof Error ? value.message : '读取最新母版失败' }
}

async function loadSampleFile(event: Event) {
	const input = event.target as HTMLInputElement
	const file = input.files?.[0]
	if (!file) return
	try {
		sampleText.value = await file.text()
		window.localStorage.setItem('anchor-style-workbench:sample', sampleText.value)
		notice.value = '主播样本已加载：' + file.name
	} catch {
		error.value = '读取主播样本文件失败'
	} finally { input.value = '' }
}

const scorecardPreview = computed(() => {
	try {
		const card = JSON.parse(scorecardJSON.value)
		const info = card['主播识别信息']
		const quant = card['量化基准'] || card['量化基准（供打分用）']
		const stats = card['样本统计校验']?.['实测']
		const row = (label: string, term: string, target: unknown, count: unknown) => ({
			label, term: term || '样本未定义', target: typeof target === 'number' ? target : '未定义', count: typeof count === 'number' ? count : '—',
		})
		const countOf = (terms: Array<{ term: string; count: number }> | undefined, term: string) => terms?.find(item => item.term === term)?.count
		const particles = info['语气词系统']
		const aux = particles['辅助语气词'] || []
		return {
			ratio: quant['语气词配比'] || quant['语气词配比目标'],
			ranking: quant['主次排序'] || [],
			leCount: quant['了_单列次数'] ?? stats?.le_count ?? '—',
			leIncluded: Boolean(quant['了_计入语气词']),
			lengthRange: info['句式节奏']['平均分句长度区间'] || '',
			clauseMeasured: quant['平均分句长度实测'] ?? stats?.average_clause_length,
			questionFrequency: info['句式节奏']['问句使用频率'],
			confirmations: info['句式节奏']['确认句库'] || [],
			loops: info['重复回环习惯']['高频回环短语'] || [],
			sampleChars: stats?.chars,
			countingMethod: card['样本统计校验']?.['统计口径'],
			rows: [
				row('主力语气词', particles['主力语气词'], quant['每百字_主力语气词'], countOf(stats?.particles, particles['主力语气词'])),
				...aux.map((term: string, index: number) => row('辅助语气词' + (index + 1), term, stats?.particles?.find((item: { term: string }) => item.term === term)?.per_100, countOf(stats?.particles, term))),
				row('主力称呼（含确认别名）', info['称呼体系']['主力称呼'], quant['每百字_主力称呼'], stats?.main_audience_count),
				row('主播自指', (info['自指体系']['自指词'] || []).join(' / '), quant['每百字_自指词'], stats?.self_count),
				row('问号（纯标点）', '？ / ?', quant['每百字_问号'], stats?.question_marks ?? stats?.questions),
				...(quant['每百字_问句'] !== undefined ? [row('问句（标点＋口语问句去重）', '正反问／确认问', quant['每百字_问句'], stats?.questions)] : []),
				row('感叹号', '！ / !', quant['每百字_感叹号'], stats?.exclamations),
			],
		}
	} catch { return null }
})

async function compileMeta() {
  if (!canCompile.value) return
  const includeLeForAnalysis = includeLe.value
  compiling.value = true
  error.value = ''
  const selectedCompiler = enabledModels.value.find(item => item.id === compilerProfileID.value)
  notice.value = `${selectedCompiler?.name || selectedCompiler?.model || '风格提取模型'} 正在把样本编译成一次性元提示词…`
  window.localStorage.setItem('anchor-style-workbench:sample', sampleText.value)
  try {
    metaResult.value = await compileAnchorStyleWorkbenchMeta(selectedPlanID.value, {
      sample_text: sampleText.value,
      scene: analysisScene.value,
      compiler_profile_id: compilerProfileID.value || undefined,
    })
    metaPrompt.value = metaResult.value.meta_prompt_md
    metaConfirmed.value = false
	await applyScorecard(null)
	includeLe.value = includeLeForAnalysis
	wheelPointer.value = 0
	wheelTests.value = []
    notice.value = '风格元提示词已保存；正在用同一份样本生成主播专属评分基准…'
    await compileScorecard()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '元提示词生成失败'
    notice.value = ''
  } finally {
    compiling.value = false
  }
}

function stageManualMeta() {
	const value = manualMetaPrompt.value.trim()
	if (!value) return
	applyingPersistedMeta = true
	metaResult.value = {
		asset_id: 0,
		status: 'manual_pending',
		meta_prompt_md: value,
		original_meta_prompt_md: value,
		monitor: { sample_chars: 0, terms: [], forbidden_terms: [], sentence_chars_min: 0, sentence_chars_max: 0 },
		analysis_prompt_source: 'manual-import',
		request: { system: '', user: '' },
		raw_response: value,
		provider: 'manual',
		model: 'manual-import',
		latency_ms: 0,
		input_tokens: 0,
		output_tokens: 0,
		persisted_at: '',
	}
	metaPrompt.value = value
	metaConfirmed.value = false
	void applyScorecard(null)
	void nextTick().then(() => { applyingPersistedMeta = false })
	wheelPointer.value = 0
	wheelTests.value = []
	notice.value = '已有风格提示词已载入确认区；点击“确认并保存”后才会绑定到当前智能体方案。'
}

async function loadManualMetaFile(event: Event) {
	const input = event.target as HTMLInputElement
	const file = input.files?.[0]
	if (!file) return
	error.value = ''
	try {
		manualMetaPrompt.value = await file.text()
		manualFileName.value = file.name
		stageManualMeta()
	} catch {
		error.value = '读取风格提示词文件失败'
	} finally {
		input.value = ''
	}
}

async function confirmMeta() {
  if (!metaPrompt.value.trim() || !metaResult.value || confirming.value) return
  confirming.value = true
  error.value = ''
  try {
	const confirmed = metaResult.value.asset_id > 0
	  ? await confirmAnchorStyleWorkbenchMeta(selectedPlanID.value, metaResult.value.asset_id, { meta_prompt_md: metaPrompt.value })
	  : await manuallyConfirmAnchorStyleWorkbenchMeta(selectedPlanID.value, {
		  scene: analysisScene.value,
		  meta_prompt_md: metaPrompt.value,
		})
	applyingPersistedMeta = true
	metaResult.value = confirmed
	metaPrompt.value = confirmed.meta_prompt_md
    metaConfirmed.value = true
	await nextTick()
	applyingPersistedMeta = false
	wheelPointer.value = 0
	wheelTests.value = []
    notice.value = `元提示词资产 #${metaResult.value.asset_id} 已绑定当前智能体方案。以后选择该方案会自动读取，无需重新生成。`
  } catch (value) {
    error.value = value instanceof Error ? value.message : '确认元提示词失败'
  } finally {
    confirming.value = false
  }
}

function download(name: string, content: string, type = 'text/plain;charset=utf-8') {
  const blob = new Blob([content], { type })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = name
  anchor.click()
  URL.revokeObjectURL(url)
}

function exportSession() {
  const payload = {
    exported_at: new Date().toISOString(),
    plan: selectedPlan.value,
    context: context.value,
    sample_text: sampleText.value,
    meta: metaResult.value,
    scorecard: scorecardResult.value,
    scorecard_compiler_profile_id: scorecardCompilerProfileID.value,
    confirmed_scorecard_json: scorecardJSON.value,
    confirmed_meta_prompt: metaPrompt.value,
    selling_point_prompt: sellingPointPrompt.value,
    selling_point_wheel: sellingPointWheel.value,
    confirmed_selling_point_wheel_json: sellingPointWheelJSON.value,
    generator_profile_id: generatorProfileID.value,
    wheel_pointer: wheelPointer.value,
    segment_max_chars: segmentMaxChars.value,
	unlimited_chars: unlimitedChars.value,
    wheel_tests: wheelTests.value,
	test_mode: testMode.value,
	agent_studio: {
		workspace_id: agentStudioWorkspaceID.value,
		region: agentStudioRegion.value,
		agent_id: agentStudioAgentID.value,
		environment_id: agentStudioEnvironmentID.value,
		credential_profile_id: agentStudioCredentialProfileID.value,
		pending_feedback_prompt: agentStudioFeedbackPrompt.value,
		session: agentStudioSession.value,
		wheel_pointer: agentStudioPointer.value,
		turns: agentStudioTurns.value,
		automatic_run: { target_chars: autoTargetChars.value, produced_chars: autoChars.value, status: autoStatus.value,
			start_index: autoStartIndex.value, started_at: autoStartedAt.value, point_gap: autoPointGap.value,
			combination_gap: autoCombinationGap.value, max_rounds: autoMaxRounds.value },
	},
  }
  download(`主播风格调教-${selectedPlan.value?.name || selectedPlanID.value}-${Date.now()}.json`, JSON.stringify(payload, null, 2), 'application/json;charset=utf-8')
}

watch(selectedPlanID, (next, previous) => {
	if (previous && previous !== next && agentStudioSession.value) {
		try { persistAgentStudioState(previous) } catch (value) { error.value = value instanceof Error ? value.message : '保存会话失败' }
		clearAgentStudioSession(false)
	}
	window.localStorage.setItem('anchor-style-workbench:selected-plan', String(next))
	void loadContext()
})
watch(testMode, value => window.localStorage.setItem('anchor-style-workbench:test-mode', value))
watch(agentStudioWorkspaceID, value => window.localStorage.setItem('anchor-style-workbench:agentstudio-workspace', value.trim()))
watch(agentStudioRegion, value => window.localStorage.setItem('anchor-style-workbench:agentstudio-region', value.trim()))
watch(agentStudioAgentID, value => window.localStorage.setItem('anchor-style-workbench:agentstudio-agent', value.trim()))
watch(agentStudioEnvironmentID, value => window.localStorage.setItem('anchor-style-workbench:agentstudio-environment', value.trim()))
watch(agentStudioCredentialProfileID, value => window.localStorage.setItem('anchor-style-workbench:agentstudio-credential', value))
watch(agentStudioFeedbackPrompt, value => window.localStorage.setItem('anchor-style-workbench:agentstudio-feedback', value))
watch(feedbackProfileID, value => window.localStorage.setItem('anchor-style-workbench:rolling-feedback-profile', value))
watch(unlimitedChars, value => window.localStorage.setItem('anchor-style-workbench:unlimited-chars', String(value)))
watch(scorecardJSON, () => {
	if (!applyingPersistedScorecard) scorecardConfirmed.value = false
})
watch(includeLe, () => {
	if (!applyingPersistedScorecard) scorecardConfirmed.value = false
})
watch(scorecardCompilerProfileID, value => window.localStorage.setItem('anchor-style-workbench:scorecard-compiler', value))
watch([agentStudioWorkspaceID, agentStudioRegion, agentStudioCredentialProfileID], () => {
	if (!agentStudioSession.value) agentStudioCatalog.value = null
})
watch(metaPrompt, () => {
	if (applyingPersistedMeta) return
  if (metaConfirmed.value) metaConfirmed.value = false
})
watch(sellingPointWheelJSON, () => {
	if (applyingPersistedWheel) return
	if (sellingPointWheelConfirmed.value) sellingPointWheelConfirmed.value = false
})
onMounted(() => void loadPlans())
onBeforeUnmount(() => { autoStopRequested.value = true })
</script>

<template>
  <div class="style-lab-page">
    <header class="lab-hero">
      <div>
        <p class="eyebrow">ANCHOR SPEECH CONTROL LAB</p>
        <h1>主播口播调教台</h1>
        <p>大循环按开场、中间、收口推进；每采纳一段追加正文，采纳后的1000字风格窗口轻量纠偏。</p>
      </div>
      <div class="hero-actions">
        <button type="button" class="ghost" :disabled="autoRunning" @click="showAdvanced = !showAdvanced">{{ showAdvanced ? '隐藏设置与中间状态' : '显示设置与单步调试' }}</button>
        <button v-if="runMode === 'chars'" type="button" class="ghost" :disabled="!wheelTests.length && !agentStudioTurns.length" @click="exportSession">导出完整测试</button>
        <button v-if="runMode === 'chars'" type="button" class="ghost danger" :disabled="autoRunning || wheelTesting || agentStudioArchiving" @click="resetAllTests">重置测试</button>
      </div>
    </header>

    <p v-if="error" class="message error">{{ error }}</p>
    <p v-else-if="notice && showAdvanced" class="message notice">{{ notice }}</p>

    <div class="auto-controls">
      <label>测试模式<select v-model="runMode" :disabled="autoRunning || wheelTesting"><option value="cycle">大循环：采纳后1000字控制</option><option value="chars">原字数循环（对照）</option></select></label>
      <label v-if="runMode === 'cycle'">直播间方案<select v-model.number="selectedPlanID" :disabled="autoRunning || loadingContext"><option v-for="plan in plans" :key="plan.id" :value="plan.id">{{ plan.name }}</option></select></label>
    </div>
    <AnchorCycleRunPanel v-if="runMode === 'cycle'" :plan-id="selectedPlanID" :ready="confirmedAssetsReady && agentStudioConfigReady && !!feedbackProfileID && !wheelTesting && !loadingContext" :config="cycleConfig" :room-ids="selectedPlan?.room_ids || []" :advanced="showAdvanced" @busy="autoRunning = $event" />
    <section v-else class="panel auto-generation-panel">
      <div class="auto-controls">
        <label>直播间方案
          <select v-model.number="selectedPlanID" :disabled="autoRunning || wheelTesting || loadingContext || agentStudioCreating">
            <option v-for="plan in plans" :key="plan.id" :value="plan.id">{{ plan.name }}</option>
          </select>
        </label>
        <label>目标字数<input v-model.number="autoTargetChars" type="number" min="1" max="100000" step="100" :disabled="autoRunning || agentStudioCreating"></label>
        <button type="button" class="primary" :disabled="!canStartAuto" @click="startAutoGeneration(false)">{{ agentStudioCreating ? '正在建会话…' : '开始自动生成' }}</button>
        <button v-if="autoStartedAt && ['paused', 'failed'].includes(autoStatus)" type="button" class="primary" :disabled="!canStartAuto" @click="startAutoGeneration(true)">继续本次生成</button>
        <button v-if="autoRunning" type="button" class="ghost danger" :disabled="autoStopRequested" @click="pauseAutoGeneration">{{ autoStopRequested ? '收完当前段后暂停…' : '暂停' }}</button>
        <button type="button" class="ghost" :disabled="!cumulativeSpeech" @click="download('自动生成话术-' + selectedPlanID + '-' + Date.now() + '.txt', cumulativeSpeech)">下载话术</button>
      </div>
      <p class="auto-progress">{{ loadingContext ? '正在读取方案…' : (autoRunning ? '自动生成中' : ({ idle: '等待开始', paused: '已暂停', completed: '已完成', failed: '已停止，请查看原因', running: '自动生成中' }[autoStatus])) }} · {{ autoStartedAt ? autoChars : speechChars(cumulativeSpeech) }}/{{ autoTargetChars }}字</p>
      <p v-if="!canStartAuto && !autoRunning && !loadingContext" class="source-note">需要确认风格、评分基准和卖点轮盘，并配置阿里母版与回灌模型。请展开“生成设置”或“显示设置与单步调试”检查。</p>
      <RiskText :plan-id="selectedPlanID" :text="cumulativeSpeech" box auto-scroll label="累计生成话术" />
      <details class="auto-settings">
        <summary>生成设置</summary>
        <div class="auto-controls">
          <label>单段最多字数<input v-model.number="autoSegmentChars" type="number" min="80" max="1000" :disabled="autoRunning"></label>
          <label>三项回灌模型（API）<select v-model="feedbackProfileID" :disabled="autoRunning || rollingFeedbackPending"><option v-for="item in enabledModels" :key="item.id" :value="item.id" :disabled="!item.has_api_key">{{ item.name }} · {{ item.model }}</option></select></label>
          <label>同卖点至少隔几段<input v-model.number="autoPointGap" type="number" min="0" max="100" :disabled="autoRunning"></label>
          <label>同组合至少隔几段<input v-model.number="autoCombinationGap" type="number" min="0" max="1000" :disabled="autoRunning"></label>
          <label>本次安全轮数上限<input v-model.number="autoMaxRounds" type="number" min="1" max="500" :disabled="autoRunning"></label>
        </div>
        <p class="source-note">自动模式每段默认最多300字，与单步“不限定字数”选项分开。总量达到或超过目标后停，保留完整末段；字数含标点、不含空白。记忆贯穿整个会话，刷新恢复记录但不自动续跑。生成会产生阿里运行时和模型费用。</p>
      </details>
    </section>

    <div v-show="showAdvanced" :inert="autoRunning || undefined">
    <section class="lab-grid setup-grid">
      <article class="panel sample-panel">
        <div class="panel-heading"><span class="step-number">1</span><div><h2>主播样本</h2><p>这里只提炼腔调，不继承样本商品事实。</p></div></div>
        <label>直播方案
          <select v-model.number="selectedPlanID" :disabled="loadingContext || wheelTesting || compiling || scorecardCompiling || scorecardConfirming">
            <option v-for="plan in plans" :key="plan.id" :value="plan.id">{{ plan.name }} · #{{ plan.id }}</option>
          </select>
        </label>
        <label>主播样本文本
          <textarea v-model="sampleText" rows="15" :disabled="compiling || scorecardCompiling" placeholder="粘贴主播原始口播样本，至少100字"></textarea>
        </label>
        <label class="file-picker">上传主播样本 .md / .txt
          <input type="file" accept=".md,.txt,text/markdown,text/plain" :disabled="compiling || scorecardCompiling" @change="loadSampleFile">
        </label>
        <label>生成场景
          <input v-model="analysisScene" maxlength="80" placeholder="例如：带货直播">
        </label>
        <div class="model-row">
          <label>风格元提示词分析模型
            <select v-model="compilerProfileID" :disabled="compiling || scorecardCompiling">
              <option value="">{{ compilerRouteLabel }}</option>
              <option v-for="item in enabledModels" :key="item.id" :value="item.id">{{ item.name }} · {{ item.model }}</option>
            </select>
          </label>
          <button type="button" class="primary" :disabled="!canCompile" @click="compileMeta">{{ compiling ? '正在编译…' : '开始调教' }}</button>
        </div>
        <details class="manual-loader">
          <summary>手动加载已有的风格提示词</summary>
          <p>可以粘贴已经确认的完整提示词，或选择一个 .md/.txt 文件。载入后仍需点击下方“确认并保存”。</p>
          <label>已有风格提示词
            <textarea v-model="manualMetaPrompt" rows="8" placeholder="粘贴完整的主播风格元提示词"></textarea>
          </label>
          <div class="manual-actions">
            <label class="file-picker">选择 .md / .txt
              <input type="file" accept=".md,.txt,text/markdown,text/plain" @change="loadManualMetaFile">
            </label>
            <span v-if="manualFileName">{{ manualFileName }}</span>
            <button type="button" class="ghost" :disabled="!canStageManualMeta" @click="stageManualMeta">载入确认区</button>
          </div>
        </details>
      </article>

      <article class="panel facts-panel">
        <div class="panel-heading"><span class="step-number neutral">F</span><div><h2>正式事实包</h2><p>与元提示词并列输入；继续使用当前商品卡、事实和福利。</p></div></div>
        <pre>{{ context?.fact_package || (loadingContext ? '正在读取…' : '请选择直播方案') }}</pre>
        <div v-if="context" class="fact-summary">
          <span>{{ context.selling_points.length }} 个事实候选项</span>
          <span>Logit：{{ context.logit_bias_capability }}</span>
        </div>
      </article>
    </section>

    <section class="panel wheel-panel">
      <div class="panel-heading"><span class="step-number wheel">W</span><div><h2>卖点 × 角度轮盘池</h2><p>将可替换的卖点挖掘提示词与当前正式事实包一起交给所选模型，生成可编辑、可保存的轮盘 JSON。</p></div></div>
      <div class="wheel-grid">
        <div class="wheel-prompt-column">
          <label>提示词来源<select v-model="sellingPointPromptMode" :disabled="sellingPointCompiling"><option value="project">项目最新母版（每次请求现读）</option><option value="manual">本次手动上传／编辑</option></select></label>
          <label>卖点挖掘提示词
            <textarea v-model="sellingPointPrompt" rows="18" :readonly="sellingPointPromptMode === 'project'" :disabled="sellingPointCompiling" placeholder="加载或粘贴卖点挖掘提示词"></textarea>
          </label>
          <p class="source-note">默认来源：{{ context?.selling_point_prompt_source || '正在读取…' }}</p>
          <label>卖点挖掘模型
            <select v-model="sellingPointProfileID" :disabled="sellingPointCompiling || sellingPointConfirming">
              <option v-for="item in enabledModels" :key="item.id" :value="item.id">{{ item.name }} · {{ item.model }}{{ item.is_default ? '（默认）' : '' }}</option>
            </select>
          </label>
          <div class="manual-actions wheel-actions">
            <label class="file-picker">加载 .md / .txt
              <input type="file" accept=".md,.txt,text/markdown,text/plain" :disabled="sellingPointCompiling" @change="loadSellingPointPromptFile">
            </label>
            <span v-if="sellingPointPromptFileName">{{ sellingPointPromptFileName }}</span>
            <button type="button" class="ghost" :disabled="!context" @click="restoreDefaultSellingPointPrompt">恢复默认</button>
            <button type="button" class="primary" :disabled="!canCompileSellingPoints" @click="compileSellingPointWheel">{{ sellingPointCompiling ? '正在挖掘…' : '生成卖点轮盘池' }}</button>
          </div>
        </div>

        <div class="wheel-result-column">
          <template v-if="sellingPointWheel">
            <p v-if="sellingPointWheel.validation_error" class="message error">{{ sellingPointWheel.validation_error }}</p>
            <p class="source-note">这份结果实际使用的提示词：{{ sellingPointWheel.prompt_source }}。左侧是当前母版，右侧历史结果不会因文件更新自动改写。</p>
            <div class="wheel-status">
              <strong>{{ sellingPointWheel.status === 'confirmed' && sellingPointWheelConfirmed ? '已绑定当前方案' : '等待确认' }}</strong>
              <span>资产 #{{ sellingPointWheel.asset_id }}</span>
              <span>{{ sellingPointWheel.selling_point_count }} 个卖点</span>
              <span>{{ sellingPointWheel.combination_count }} 个卖点×角度组合</span>
              <span>{{ sellingPointWheel.model }}</span>
            </div>
            <div class="wheel-traces">
              <details><summary>发送给模型的完整内容</summary><h4>System</h4><pre>{{ sellingPointWheel.request.system }}</pre><h4>User</h4><pre>{{ sellingPointWheel.request.user }}</pre></details>
              <details><summary>模型原始返回</summary><pre>{{ sellingPointWheel.raw_response }}</pre></details>
            </div>
            <label>可确认、可编辑的卖点轮盘 JSON
              <textarea v-model="sellingPointWheelJSON" rows="18" spellcheck="false"></textarea>
            </label>
            <div class="confirm-row">
              <p>{{ sellingPointWheelConfirmed ? '已保存；以后选择该智能体方案会自动显示。' : '可以修改卖点名和角度；确认时程序会校验JSON结构并保存。' }}</p>
              <button type="button" class="primary" :class="{ confirmed: sellingPointWheelConfirmed }" :disabled="!canConfirmSellingPoints" @click="confirmSellingPointWheel">{{ sellingPointConfirming ? '正在保存…' : (sellingPointWheelConfirmed ? '已确认' : '确认并保存轮盘') }}</button>
            </div>
          </template>
          <div v-else class="wheel-empty">
            <strong>尚未生成卖点轮盘</strong>
            <p>左侧提示词中的 <code v-pre>{{FACT_PACKAGE}}</code> 会替换为上方正式事实包，然后原样发送给所选模型。</p>
          </div>
        </div>
      </div>
    </section>

    <section v-if="metaResult" class="panel meta-panel">
      <div class="panel-heading"><span class="step-number">2</span><div><h2>主播风格元提示词</h2><p>{{ metaResult.provider === 'manual' ? '手动加载' : metaResult.provider + ' · ' + metaResult.model + ' · ' + metaResult.latency_ms + 'ms' }} · {{ metaResult.asset_id ? '资产 #' + metaResult.asset_id : '尚未保存' }} · {{ metaResult.status === 'confirmed' ? '已绑定当前方案' : (metaResult.status === 'manual_pending' ? '等待确认保存' : '原始版已落库') }}</p></div></div>
      <div v-if="metaResult.provider !== 'manual'" class="trace-grid">
        <details open><summary>发送给风格提取模型的完整提示词</summary><h4>System</h4><pre>{{ metaResult.request.system }}</pre><h4>User</h4><pre>{{ metaResult.request.user }}</pre></details>
        <details><summary>风格提取模型原始返回</summary><pre>{{ metaResult.raw_response }}</pre></details>
      </div>
      <label>可确认、可编辑的元提示词
        <textarea v-model="metaPrompt" rows="16"></textarea>
      </label>
      <div class="monitor-chips">
        <span v-for="term in metaResult.monitor.terms" :key="term.kind + term.text">{{ term.kind }} · {{ term.text }} · {{ term.sample_per_1000.toFixed(1) }}/千字</span>
      </div>
      <div class="confirm-row">
        <p>{{ metaConfirmed ? '已确认并绑定当前方案；以后选择该方案会自动显示。' : (metaResult.asset_id ? '原始版已保存；确认后作为当前方案的生效风格基准。' : '尚未保存；确认时将创建资产并绑定当前方案。') }}</p>
        <button type="button" class="primary" :class="{ confirmed: metaConfirmed }" :disabled="confirming" @click="confirmMeta">{{ confirming ? '正在保存…' : (metaConfirmed ? '已确认' : '确认并保存') }}</button>
      </div>
    </section>

    <section class="panel scorecard-panel">
      <div class="panel-heading"><span class="step-number">S</span><div><h2>主播专属评分基准表</h2><p>用下方独立选择的模型和原主播样本提取；确认后保存到当前方案，每段返回自动对照打分。</p></div></div>
      <label>评分基准提取模型
        <select v-model="scorecardCompilerProfileID" :disabled="compiling || scorecardCompiling">
          <option v-if="!scorecardModels.length" value="">未配置可用模型</option>
          <option v-for="item in scorecardModels" :key="item.id" :value="item.id" :disabled="!item.has_api_key">{{ item.name }} · {{ item.model }}{{ item.has_api_key ? '' : '（未配置Key）' }}</option>
        </select>
      </label>
      <label class="unlimited-option"><input v-model="includeLe" type="checkbox" :disabled="scorecardCompiling || scorecardConfirming || wheelTesting"> 将“了”计入语气词（默认关闭；修改后请重新校对）</label>
      <details class="manual-loader">
        <summary>查看／替换通用基准提取提示词</summary>
        <p>项目默认：{{ context?.scorecard_prompt_source }}</p>
        <label>提示词来源<select v-model="scorecardPromptMode" :disabled="scorecardCompiling"><option value="project">项目最新母版（每次请求现读）</option><option value="manual">本次手动上传／编辑</option></select></label>
        <textarea v-model="scorecardPrompt" rows="12" :readonly="scorecardPromptMode === 'project'" :disabled="scorecardCompiling"></textarea>
        <div class="manual-actions">
          <label class="file-picker">加载 .md / .txt<input type="file" accept=".md,.txt,text/markdown,text/plain" :disabled="scorecardCompiling" @change="loadScorecardPromptFile"></label>
          <span v-if="scorecardPromptFileName">{{ scorecardPromptFileName }}</span>
          <button type="button" class="ghost" :disabled="!context || scorecardCompiling" @click="restoreDefaultScorecardPrompt">重新读取项目母版</button>
        </div>
      </details>
      <div class="manual-actions">
        <button type="button" class="primary" :disabled="!canCompileScorecard" @click="compileScorecard">{{ scorecardCompiling ? '正在提取评分基准…' : '单独生成／重试评分基准' }}</button>
        <button type="button" class="ghost" :disabled="!scorecardResult || scorecardCompiling || scorecardConfirming || wheelTesting" @click="recalibrateScorecard">按原始返回重新校对（不调用模型）</button>
        <span v-if="scorecardResult">资产 #{{ scorecardResult.asset_id }} · 风格 #{{ scorecardResult.meta_prompt_asset_id }} · {{ scorecardResult.model }} · {{ scorecardResult.latency_ms }}ms</span>
      </div>
      <template v-if="scorecardResult">
        <p v-if="scorecardResult.calibration_error" class="message error">原始返回已保存，校对失败：{{ scorecardResult.calibration_error }}</p>
        <p v-else-if="scorecardResult.calibration_version !== 'qualitative-quantitative-v2'" class="source-note">这是旧版基准。点击“按原始返回重新校对”，再确认新表。</p>
        <div v-if="scorecardPreview" class="baseline-preview">
          <p>样本实测{{ scorecardPreview.sampleChars || '—' }}字符 · 主力配比：{{ scorecardPreview.ratio }} · 问句频率：{{ scorecardPreview.questionFrequency }}</p>
          <p>程序主次排序：{{ scorecardPreview.ranking.join(' → ') || '旧版未记录' }}。“了”实测{{ scorecardPreview.leCount }}次，{{ scorecardPreview.leIncluded ? '当前计入配比' : '只单列，不参与配比或打分' }}。</p>
          <div class="baseline-table-scroll"><table class="baseline-table"><thead><tr><th>指标</th><th>本主播词／符号</th><th>样本实际次数</th><th>每百字基准</th></tr></thead>
            <tbody><tr v-for="row in scorecardPreview.rows" :key="row.label"><td>{{ row.label }}</td><td>{{ row.term }}</td><td>{{ row.count }}</td><td>{{ row.target }}</td></tr></tbody>
          </table></div>
          <p>确认句库：{{ scorecardPreview.confirmations.join('；') || '样本未定义' }}</p>
          <p>回环短语：{{ scorecardPreview.loops.join('；') || '样本未定义' }}</p>
          <p>分句区间（模型定性）：{{ scorecardPreview.lengthRange || '未描述' }} · 平均分句长度（程序实测）：{{ scorecardPreview.clauseMeasured || '—' }}字</p>
          <p class="source-note">{{ scorecardPreview.countingMethod }}。次数、频次、配比和主次均由程序生成；“我／我们”不计入结构性自指。模型定性描述可编辑，确认时量化字段会按原始全文重新计算。</p>
        </div>
        <div class="trace-grid">
          <details><summary>实际发送给基准提取模型的提示词</summary><h4>System</h4><pre>{{ scorecardResult.request.system || '（空）' }}</pre><h4>User</h4><pre>{{ scorecardResult.request.user }}</pre></details>
          <details><summary>模型原始返回（原文保留，不随校对修改）</summary><pre>{{ scorecardResult.raw_response }}</pre><button type="button" class="ghost" @click="download('模型原始返回-' + scorecardResult.asset_id + '.txt', scorecardResult.raw_response)">下载原始返回</button></details>
        </div>
        <label>程序校对后的评分基准 JSON（可编辑定性描述）<textarea v-model="scorecardJSON" rows="18" spellcheck="false" :disabled="scorecardConfirming || wheelTesting"></textarea></label>
        <button type="button" class="ghost" :disabled="!scorecardJSON" @click="download('校对评分基准-' + scorecardResult.asset_id + '.json', scorecardJSON, 'application/json;charset=utf-8')">下载校对基准表</button>
        <div class="confirm-row">
          <p>{{ scorecardConfirmed ? '已保存；本方案每段话术都使用这份基准打分。' : '已保存原始基准草稿；确认后用于正式评分。' }}</p>
          <button type="button" class="primary" :class="{ confirmed: scorecardConfirmed }" :disabled="!scorecardJSON.trim() || scorecardConfirming || compiling || scorecardCompiling || wheelTesting" @click="confirmScorecard">{{ scorecardConfirming ? '正在保存…' : (scorecardConfirmed ? '已确认评分基准' : '确认并保存评分基准') }}</button>
        </div>
      </template>
      <p v-else>尚未建立评分基准。点击“开始调教”先生成风格元提示词，再由上方选择的模型提取基准表；已有元提示词时，可用原主播样本单独提取。</p>
    </section>

    <section v-if="metaConfirmed && sellingPointWheelConfirmed" class="panel control-panel">
      <div class="panel-heading"><span class="step-number">3</span><div><h2>卖点 × 角度单格装载测试</h2><p>推荐用阿里连续会话：首轮只注入一次风格元提示词和事实包，后续每次只发下一格任务；普通 API 保留用于 A/B 对照。</p></div></div>
      <div class="test-mode-picker">
        <button type="button" :class="{ active: testMode === 'agentstudio' }" @click="testMode = 'agentstudio'">阿里连续会话（推荐）</button>
        <button type="button" :class="{ active: testMode === 'stateless' }" @click="testMode = 'stateless'">普通无状态 API（对照）</button>
      </div>

      <template v-if="testMode === 'agentstudio'">
        <div class="agentstudio-config-grid">
          <label>Workspace ID
            <input v-model="agentStudioWorkspaceID" placeholder="ws_... 或业务空间 llm-..." :disabled="Boolean(agentStudioSession) || agentStudioCreating">
          </label>
          <label>地域
            <select v-model="agentStudioRegion" :disabled="Boolean(agentStudioSession) || agentStudioCreating">
              <option value="cn-beijing">北京 · cn-beijing</option>
            </select>
          </label>
          <label>母版 Agent
            <select v-model="agentStudioAgentID" :disabled="Boolean(agentStudioSession) || agentStudioCreating || agentStudioCatalogLoading">
              <option value="">请先读取阿里智能体列表</option>
              <option v-if="agentStudioAgentID && !agentStudioCatalog?.agents.some(item => item.id === agentStudioAgentID)" :value="agentStudioAgentID">已保存 ID · {{ agentStudioAgentID }}</option>
              <option v-for="item in agentStudioCatalog?.agents || []" :key="item.id" :value="item.id">{{ item.name }} · {{ item.model || 'Auto' }} · v{{ item.version }}</option>
            </select>
          </label>
          <label>Environment
            <select v-model="agentStudioEnvironmentID" :disabled="Boolean(agentStudioSession) || agentStudioCreating || agentStudioCatalogLoading">
              <option value="">自动选择默认环境</option>
              <option v-if="agentStudioEnvironmentID && !agentStudioCatalog?.environments.some(item => item.id === agentStudioEnvironmentID)" :value="agentStudioEnvironmentID">已保存 ID · {{ agentStudioEnvironmentID }}</option>
              <option v-for="item in agentStudioCatalog?.environments || []" :key="item.id" :value="item.id">{{ item.name || item.id }}</option>
            </select>
          </label>
          <label>API Key 来源（复用系统配置）
            <select v-model="agentStudioCredentialProfileID" :disabled="Boolean(agentStudioSession) || agentStudioCreating">
              <option value="">请选择已保存 API Key 的模型配置</option>
              <option v-for="item in enabledModels" :key="item.id" :value="item.id" :disabled="!item.has_api_key">{{ item.name }} · {{ item.model }}{{ item.has_api_key ? ' · 已有Key' : ' · 无Key' }}</option>
            </select>
          </label>
          <div class="limit-control">
            <label>单段最大字数
              <input v-model.number="segmentMaxChars" type="number" min="80" max="1000" step="20" :disabled="wheelTesting || unlimitedChars">
            </label>
            <label class="unlimited-option"><input v-model="unlimitedChars" type="checkbox" :disabled="wheelTesting"> 暂不限定字数</label>
          </div>
        </div>
        <div class="agentstudio-catalog-actions">
          <button type="button" class="ghost" :disabled="!canLoadAgentStudioCatalog" @click="loadAgentStudioCatalog()">{{ agentStudioCatalogLoading ? '正在读取阿里资源…' : '读取/刷新智能体和环境' }}</button>
          <span v-if="agentStudioCatalog">已读取 {{ agentStudioCatalog.agents.length }} 个智能体、{{ agentStudioCatalog.environments.length }} 个环境</span>
        </div>
        <p v-if="selectedAgentStudioAgentIsAuto" class="agent-warning">当前选中的是 Auto 模型。风格测试建议改选“带货主播 · Qwen3.8-Max”，然后新建会话。</p>
        <p class="agentstudio-help">页面会优先选择“带货主播 · Qwen3.8-Max”。Environment 可选“自动选择默认环境”；API Key 继续读取系统里加密保存的配置。关闭深度思考或修改母版后，需要归档并新建会话。</p>
        <details v-if="selectedAgentStudioAgent" class="agent-system-prompt">
          <summary>查看当前母版系统规则 · {{ selectedAgentStudioAgent.name }} · {{ selectedAgentStudioAgent.model }} · v{{ selectedAgentStudioAgent.version }}</summary>
          <pre>{{ selectedAgentStudioAgent.system || '该母版当前没有设置系统提示词。' }}</pre>
        </details>
        <p class="source-note">{{ scorecardConfirmed ? '每轮返回后自动展示程序评分，使用评分基准 #' + scorecardResult?.asset_id : '请先生成并确认上方主播专属评分基准，再进行话术测试。' }}</p>
        <div class="feedback-prompt-box">
          <label>下一轮回灌提示词（可空）
            <textarea v-model="agentStudioFeedbackPrompt" rows="4" maxlength="4000" :disabled="wheelTesting" placeholder="例如：上一段“哥哥姐姐们”和“我们家”偏少；下一段增加称呼和自指，但不要集中堆叠，事实内容不变。"></textarea>
          </label>
          <div>
            <span>{{ [...agentStudioFeedbackPrompt].length }}/4000字</span>
            <label class="feedback-clear-option"><input v-model="clearFeedbackAfterSuccess" type="checkbox"> 发送成功后自动清空（一次性回灌）</label>
          </div>
          <p>回灌只进入下一轮消息，不会修改已确认的风格元提示词、事实包或卖点轮盘。</p>
        </div>
        <div v-if="agentStudioSession" class="agentstudio-session-card" :class="{ stale: agentStudioSessionStale }">
          <div>
            <strong>{{ agentStudioSession.agent_name || '带货主播' }} · v{{ agentStudioSession.agent_version }}</strong>
            <span>Session：{{ agentStudioSession.session_id }}</span>
            <span>模型：{{ agentStudioSession.agent_model || '由母版锁定' }}</span>
          </div>
          <p v-if="agentStudioSessionStale">风格或轮盘资产已经变化，请归档此会话并新建，避免旧上下文继续生效。</p>
          <p v-else-if="!agentStudioTurns.length">会话已就绪。第一次点击会发送：风格元提示词 + 正式事实包 + 第一格。</p>
          <p v-else>上下文已建立。下一次默认只发送“当前卖点 × 当前角度”。</p>
        </div>
        <div class="controls agentstudio-actions">
          <button v-if="!agentStudioSession" type="button" class="primary" :disabled="!canCreateAgentStudioSession" @click="createAgentStudioSession">{{ agentStudioCreating ? '正在创建会话…' : '创建阿里独立会话' }}</button>
          <button v-else type="button" class="primary accent" :disabled="!canRunAgentStudioTurn" @click="runAgentStudioTurn()">{{ wheelTesting ? '会话生成中…' : (agentStudioTurns.length ? '装载下一格并继续' : '装载第一格并开始') }}</button>
          <button type="button" class="ghost danger" :disabled="!agentStudioSession || wheelTesting || agentStudioArchiving" @click="archiveAgentStudioSession()">{{ agentStudioArchiving ? '正在归档…' : '归档并重建会话' }}</button>
        </div>
        <div class="state-strip">
          <span>下一指针 {{ agentStudioPointer }}</span>
          <span>已完成 {{ agentStudioTurns.length }} 轮</span>
          <span>风格资产 #{{ metaResult?.asset_id }}</span>
          <span>轮盘资产 #{{ sellingPointWheel?.asset_id }}</span>
          <span>{{ unlimitedChars ? '本轮不限定字数' : `单段最多${segmentMaxChars}字` }}</span>
          <span>{{ agentStudioTurns.length ? '后续不重发风格和事实' : '首轮尚未注入上下文' }}</span>
        </div>
      </template>

      <template v-else>
        <div class="controls wheel-test-controls">
          <label>口播生成模型
            <select v-model="generatorProfileID" :disabled="wheelTesting">
              <option v-for="item in enabledModels" :key="item.id" :value="item.id">{{ item.name }} · {{ item.model }}{{ item.is_default ? '（默认）' : '' }}</option>
            </select>
          </label>
          <div class="limit-control">
            <label>单段最大字数
              <input v-model.number="segmentMaxChars" type="number" min="80" max="1000" step="20" :disabled="wheelTesting || unlimitedChars">
            </label>
            <label class="unlimited-option"><input v-model="unlimitedChars" type="checkbox" :disabled="wheelTesting"> 暂不限定字数</label>
          </div>
          <button type="button" class="primary accent" :disabled="!canTestWheel" @click="runWheelTest()">{{ wheelTesting ? '模型生成中…' : '装载下一格并测试' }}</button>
          <button type="button" class="ghost" :disabled="wheelTesting || (!wheelPointer && !wheelTests.length)" @click="resetWheelTests">指针归零</button>
        </div>
        <div class="state-strip">
          <span>下一指针 {{ wheelPointer }}</span>
          <span>每轮输入：风格元提示词 + 事实包 + 当前卖点角度</span>
          <span>{{ unlimitedChars ? '本轮不发送字数限制' : `单段最多${segmentMaxChars}字` }}</span>
          <span>无历史、无回灌、无后处理</span>
        </div>
      </template>
    </section>

    <section v-if="agentStudioTurns.length" class="wheel-test-results agentstudio-test-results">
      <article v-for="(item, index) in agentStudioTurns" :key="item.id" class="round-card wheel-test-card agentstudio-turn-card">
        <header>
          <div><strong>连续会话第 {{ index + 1 }} 轮</strong><span>{{ item.response.task.selling_point }} × {{ item.response.task.angle }}</span></div>
          <div class="round-meta">
            <span>{{ item.response.first_turn ? '首轮完整注入' : '仅卖点 × 角度' }}</span>
            <span v-if="item.response.feedback_applied" class="feedback-applied">已回灌</span>
            <span>{{ item.response.latency_ms }}ms</span>
            <span :class="{ over: item.response.exceeded_limit }">{{ item.response.unlimited_chars ? `${item.response.actual_chars}字 · 未限字数` : `${item.response.actual_chars}/${item.response.max_chars}字` }}</span>
          </div>
        </header>
        <details class="prompt-trace" :open="index < 2"><summary>本轮真正发送给母版 Agent 的消息</summary><pre>{{ item.response.request.user }}</pre></details>
        <div class="response-grid direct">
          <section :class="{ exceeded: item.response.exceeded_limit }"><h3>Agent 原始返回 <small>{{ item.response.actual_chars }}字</small></h3><RiskText :plan-id="selectedPlanID" :text="item.response.raw_response" :legend="false" /><em v-if="item.response.exceeded_limit">已超过本段最大字数，请检查母版服从性。</em></section>
        </div>
        <AnchorStyleScoreDetails :result="item.score" :error="item.scoreError" :pending="styleReviewing" :can-apply-feedback="!wheelTesting && !styleReviewing && Boolean(agentStudioSession)" @apply-feedback="applyGeneratedFeedback(item, $event)" @retry="scoreGeneratedSpeech(item)" />
        <details v-if="item.rollingFeedback || item.rollingFeedbackError"><summary>自动回灌：开头／主播风格／结尾 · {{ item.rollingFeedback?.model || '程序检查' }} · {{ item.rollingFeedback?.latency_ms || 0 }}ms</summary><p v-if="item.rollingFeedbackError">{{ item.rollingFeedbackError }}</p><pre>{{ JSON.stringify(item.rollingFeedback, null, 2) }}</pre></details>
        <div class="state-strip compact">
          <span>Session {{ item.response.session_id }}</span>
          <span>stop_reason={{ item.response.stop_reason }}</span>
          <span>风格 #{{ item.response.style_asset_id }}</span>
          <span>轮盘 #{{ item.response.wheel_asset_id }}</span>
        </div>
        <div class="test-card-actions">
          <button type="button" class="ghost" :disabled="wheelTesting" @click="retryAgentStudioTurn(item)">在同一会话重试这一格</button>
        </div>
      </article>
    </section>

    <section v-if="wheelTests.length" class="wheel-test-results">
      <article v-for="item in wheelTests" :key="item.id" class="round-card wheel-test-card">
        <header>
          <div><strong>第 {{ item.response.task.pointer + 1 }} 次测试</strong><span>{{ item.response.task.selling_point }} × {{ item.response.task.angle }}</span></div>
          <div class="round-meta">
            <span>第{{ item.response.task.cycle }}圈 · {{ item.response.task.position }}/{{ item.response.task.total }}</span>
            <span>{{ item.response.model }}</span>
            <span>{{ item.response.latency_ms }}ms</span>
            <span :class="{ over: item.response.exceeded_limit }">{{ item.response.unlimited_chars ? `${item.response.actual_chars}字 · 未限字数` : `${item.response.actual_chars}/${item.response.max_chars}字` }}</span>
          </div>
        </header>
        <details class="prompt-trace"><summary>程序发给模型的完整提示词</summary><h4>System</h4><pre>{{ item.response.request.system || '（空）' }}</pre><h4>User</h4><pre>{{ item.response.request.user }}</pre></details>
        <div class="response-grid direct">
          <section :class="{ exceeded: item.response.exceeded_limit }"><h3>模型原始返回 <small>{{ item.response.actual_chars }}字</small></h3><RiskText :plan-id="selectedPlanID" :text="item.response.raw_response" :legend="false" /><em v-if="item.response.exceeded_limit">已超过本段最大字数，请检查模型服从性。</em></section>
        </div>
        <AnchorStyleScoreDetails :result="item.score" :error="item.scoreError" :pending="styleReviewing" @retry="scoreGeneratedSpeech(item)" />
        <div class="test-card-actions">
          <button type="button" class="ghost" :disabled="wheelTesting" @click="retryWheelTest(item)">按同一格重新测试</button>
        </div>
      </article>
    </section>
    </div>
  </div>
</template>

<style scoped>
.auto-controls{display:flex;align-items:end;gap:12px;flex-wrap:wrap}.auto-controls label{margin:0;min-width:140px}.auto-controls label:first-child{min-width:230px}.auto-controls input{max-width:180px}.auto-progress{color:#607080;font-size:13px}.cumulative-speech{height:58vh;min-height:340px;font-size:15px;line-height:1.9;background:#fff}.auto-settings{margin-top:12px;color:#607080;font-size:13px}.auto-settings summary{cursor:pointer}.auto-settings .auto-controls{margin-top:12px}
.baseline-preview{margin:16px 0;padding:14px;border:1px solid #bad5cc;border-radius:12px;background:#f4faf8}.baseline-preview p{font-size:13px;line-height:1.7}.baseline-table-scroll{overflow:auto}.baseline-table{width:100%;border-collapse:collapse}.baseline-table th,.baseline-table td{padding:10px;border-bottom:1px solid #d9e7df;text-align:left;font-size:13px}.baseline-table th{background:#e6f0eb}.scorecard-panel>.manual-actions{margin-top:12px}
.style-lab-page{min-height:100vh;padding:28px;background:#f4f5f7;color:#18202a}.lab-hero,.panel,.round-card{border:1px solid #d8dde5;background:#fff;box-shadow:0 12px 35px #1c27330d}.lab-hero{display:flex;justify-content:space-between;gap:24px;padding:30px;border-radius:22px;background:linear-gradient(135deg,#101923,#223447);color:#fff}.eyebrow{margin:0 0 6px;color:#75d8c9;font-size:12px;letter-spacing:.18em}.lab-hero h1{margin:0;font-size:34px}.lab-hero p{max-width:830px;margin:10px 0 0;color:#ccd7e1;line-height:1.7}.hero-actions,.controls,.model-row,.confirm-row,.state-strip,.fact-summary,.monitor-chips,.bias-list,.feedback,.manual-actions{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.lab-grid{display:grid;gap:18px}.setup-grid{grid-template-columns:minmax(0,1.1fr) minmax(0,.9fr);margin-top:18px}.panel{margin-top:18px;padding:22px;border-radius:18px}.setup-grid .panel{margin-top:0}.panel-heading{display:flex;align-items:flex-start;gap:12px;margin-bottom:18px}.panel-heading h2{margin:0;font-size:20px}.panel-heading p{margin:4px 0 0;color:#697584}.step-number{display:grid;place-items:center;width:34px;height:34px;border-radius:11px;background:#0f8f80;color:#fff;font-weight:800}.step-number.neutral{background:#465463}label{display:grid;gap:7px;margin-top:14px;color:#465463;font-size:13px;font-weight:700}textarea,select,input{width:100%;box-sizing:border-box;border:1px solid #cbd3dc;border-radius:10px;background:#fbfcfd;color:#1d2833;font:inherit;padding:10px 12px}textarea{resize:vertical;line-height:1.6;font-family:"Microsoft YaHei",sans-serif}select:focus,textarea:focus,input:focus{outline:3px solid #20a89522;border-color:#20a895}.model-row{align-items:end}.model-row label{flex:1}.model-row button{margin-bottom:0}.manual-loader{margin-top:16px;border:1px solid #dbe1e7;border-radius:12px;padding:12px;background:#fafbfc}.manual-loader summary{cursor:pointer;font-weight:800;color:#314253}.manual-loader>p{margin:10px 0 0;color:#697584;font-size:13px;line-height:1.6}.manual-actions{margin-top:10px}.manual-actions .file-picker{display:inline-flex;width:auto;margin:0;border:1px solid #cbd3dc;border-radius:10px;padding:10px 13px;background:#fff;cursor:pointer}.manual-actions .file-picker input{display:none}.manual-actions span{max-width:230px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#607080;font-size:12px}button{border:0;border-radius:10px;padding:11px 17px;font-weight:800;cursor:pointer}button:disabled{cursor:not-allowed;opacity:.45}.primary{background:#147d70;color:#fff}.primary.accent{background:#1668c1}.primary.confirmed{background:#2f9b58}.ghost{border:1px solid #cbd3dc;background:#fff;color:#334252}.lab-hero .ghost{border-color:#ffffff44;background:#ffffff12;color:#fff}.danger{color:#bd3842!important}.warning{background:#f0a92f;color:#201400}.facts-panel pre,.trace-grid pre,.prompt-trace pre{white-space:pre-wrap;word-break:break-word;border-radius:12px;background:#121923;color:#dbe9e7;padding:15px;line-height:1.55;max-height:520px;overflow:auto}.facts-panel pre{height:490px;margin:0}.fact-summary,.state-strip{margin-top:12px;color:#647180}.fact-summary span,.state-strip span,.monitor-chips span,.bias-list span{border-radius:999px;background:#edf3f4;padding:6px 10px;font-size:12px}.trace-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.trace-grid details,.round-card details{border:1px solid #dbe1e7;border-radius:12px;padding:12px;background:#fafbfc}.trace-grid summary,.round-card summary{cursor:pointer;font-weight:800}.trace-grid h4,.prompt-trace h4{margin:14px 0 6px}.confirm-row{justify-content:space-between;margin-top:14px}.confirm-row p{margin:0;color:#596674}.controls{align-items:end}.controls label{min-width:125px;margin:0}.controls label:first-child{min-width:310px;flex:1}.switch{display:flex!important;grid-template-columns:auto 1fr;align-items:center!important;padding:10px 0}.switch input{width:auto}.message{position:sticky;top:10px;z-index:4;border-radius:12px;padding:12px 16px;box-shadow:0 8px 25px #00000014}.message.error{background:#fff0f0;color:#a82b35}.message.notice{background:#eaf8f5;color:#176e62}.timeline{display:grid;gap:16px;margin-top:18px;max-height:calc(100vh - 120px);overflow:auto;padding-right:4px}.round-card{padding:20px;border-radius:18px}.round-card>header{display:flex;justify-content:space-between;gap:16px;align-items:center}.round-card>header div:first-child{display:grid;gap:3px}.round-card>header strong{font-size:19px}.round-card>header span{color:#63707d}.round-meta{display:flex;gap:8px;flex-wrap:wrap}.round-meta span,.task-grid span{border-radius:8px;background:#edf3f4;padding:6px 9px;font-size:12px}.task-grid{display:flex;gap:8px;flex-wrap:wrap;margin:14px 0}.prompt-trace{margin-bottom:14px}.response-grid{display:grid;grid-template-columns:1fr 1fr;gap:14px}.response-grid.direct{grid-template-columns:1fr}.response-grid section{border:1px solid #dce2e8;border-radius:14px;padding:16px;background:#fff}.response-grid section.changed{border-color:#64b4a8;background:#f1fbf8}.response-grid h3,.stats-block h3{margin:0 0 10px}.response-grid small{color:#7a8692;font-weight:400}.response-grid p{white-space:pre-wrap;line-height:1.85;margin:0}.response-grid ul{margin-bottom:0;color:#33766d}.response-grid em{display:block;margin-top:12px;color:#84909b}.stats-block{margin-top:14px}.metric-table{border:1px solid #dbe1e7;border-radius:12px;overflow:hidden}.metric-row{display:grid;grid-template-columns:1.3fr 1.2fr .8fr .8fr .55fr;padding:8px 11px;border-top:1px solid #edf0f3;font-size:12px}.metric-row.header{border-top:0;background:#eef2f5;color:#52606d;font-weight:800}.metric-row.low{background:#fff8e9}.metric-row.high{background:#fff0f0}.feedback{align-items:flex-start;margin-top:12px}.feedback span{border-left:3px solid #1b9181;background:#edf8f6;padding:7px 10px;font-size:12px}.feedback strong{padding:7px 0}@media(max-width:980px){.setup-grid,.trace-grid,.response-grid{grid-template-columns:1fr}.lab-hero{display:block}.hero-actions{margin-top:18px}.facts-panel pre{height:auto}.style-lab-page{padding:14px}}
.step-number.wheel{background:#8058b6}.wheel-grid{display:grid;grid-template-columns:minmax(0,.95fr) minmax(0,1.05fr);gap:18px}.wheel-prompt-column,.wheel-result-column{min-width:0;border:1px solid #dbe1e7;border-radius:14px;padding:16px;background:#fafbfc}.wheel-prompt-column label:first-child,.wheel-result-column label{margin-top:0}.source-note{margin:8px 0 0;color:#788491;font-size:12px;word-break:break-all}.wheel-actions{align-items:center;margin-top:12px}.wheel-actions .primary{margin-left:auto}.wheel-status{display:flex;align-items:center;gap:8px;flex-wrap:wrap;margin-bottom:12px}.wheel-status strong{color:#207967}.wheel-status span{border-radius:999px;background:#edf0f5;padding:6px 9px;color:#5b6876;font-size:12px}.wheel-traces{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:12px}.wheel-traces details{min-width:0;border:1px solid #dbe1e7;border-radius:10px;padding:10px;background:#fff}.wheel-traces summary{cursor:pointer;font-weight:800}.wheel-traces h4{margin:10px 0 5px}.wheel-traces pre{max-height:300px;overflow:auto;white-space:pre-wrap;word-break:break-word;border-radius:8px;background:#121923;color:#dbe9e7;padding:10px;font-size:12px;line-height:1.5}.wheel-result-column textarea{font-family:Consolas,"Microsoft YaHei",monospace}.wheel-empty{display:grid;place-items:center;min-height:420px;text-align:center;color:#71808f}.wheel-empty strong{font-size:18px;color:#42505e}.wheel-empty p{max-width:460px;line-height:1.7}.wheel-empty code{border-radius:5px;background:#e9edf2;padding:2px 5px;color:#6b3ca2}@media(max-width:1100px){.wheel-grid,.wheel-traces{grid-template-columns:1fr}}
.wheel-test-controls label:nth-child(2){min-width:150px;max-width:190px}.wheel-test-results{display:grid;gap:16px;margin-top:18px}.wheel-test-card .prompt-trace{margin:14px 0}.wheel-test-card .round-meta .over{background:#ffe6e7;color:#b32631;font-weight:800}.wheel-test-card .response-grid section.exceeded{border-color:#d95b65;background:#fff7f7}.test-card-actions{display:flex;justify-content:flex-end;margin-top:12px}
.test-mode-picker{display:flex;gap:8px;margin-bottom:18px;border-radius:12px;background:#edf1f5;padding:5px}.test-mode-picker button{flex:1;background:transparent;color:#526171}.test-mode-picker button.active{background:#fff;color:#126b60;box-shadow:0 3px 12px #17212b18}.agentstudio-config-grid{display:grid;grid-template-columns:1.2fr .65fr 1fr 1fr 1.25fr .7fr;gap:12px;align-items:end}.agentstudio-config-grid label{min-width:0;margin:0}.agentstudio-help{margin:12px 0;color:#5d6b78;line-height:1.65}.agentstudio-session-card{display:grid;gap:8px;margin:14px 0;border:1px solid #67b6a8;border-radius:14px;background:#f0faf7;padding:14px}.agentstudio-session-card.stale{border-color:#d88b35;background:#fff8ea}.agentstudio-session-card>div{display:flex;align-items:center;gap:10px;flex-wrap:wrap}.agentstudio-session-card span{border-radius:999px;background:#fff;padding:5px 9px;color:#556674;font-size:12px}.agentstudio-session-card p{margin:0;color:#556674}.agentstudio-actions{margin-top:12px}.agentstudio-turn-card{border-color:#88bfb6}.state-strip.compact{margin-top:12px}@media(max-width:1280px){.agentstudio-config-grid{grid-template-columns:repeat(3,minmax(0,1fr))}}@media(max-width:760px){.agentstudio-config-grid{grid-template-columns:1fr}.test-mode-picker{display:grid}}
.feedback-prompt-box{margin:14px 0;border:1px solid #d7c9ef;border-radius:14px;background:#faf7ff;padding:14px}.feedback-prompt-box>label{margin:0}.feedback-prompt-box>div{display:flex;justify-content:space-between;align-items:center;gap:12px;margin-top:8px;color:#6e5b86;font-size:12px}.feedback-prompt-box>p{margin:8px 0 0;color:#786b88;font-size:12px}.feedback-clear-option{display:flex;align-items:center;gap:6px;margin:0}.feedback-clear-option input{width:auto}.round-meta .feedback-applied{background:#eee5ff;color:#7040a5;font-weight:800}@media(max-width:760px){.feedback-prompt-box>div{align-items:flex-start;flex-direction:column}}
.agentstudio-catalog-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap;margin:10px 0}.agentstudio-catalog-actions span{color:#61707e;font-size:12px}.agent-warning{margin:10px 0;border-left:4px solid #e39a30;border-radius:8px;background:#fff5df;color:#8d580c;padding:10px 12px;font-weight:700}
.agent-system-prompt{margin:12px 0;border:1px solid #cfd8e2;border-radius:12px;background:#fafbfc;padding:12px}.agent-system-prompt summary{cursor:pointer;color:#344759;font-weight:800}.agent-system-prompt pre{max-height:420px;overflow:auto;white-space:pre-wrap;word-break:break-word;border-radius:10px;background:#121923;color:#dbe9e7;padding:14px;line-height:1.6}
.limit-control{display:grid;gap:6px;min-width:170px}.limit-control>label:first-child{margin:0}.unlimited-option{display:flex;align-items:center;gap:6px;margin:0;color:#536473;font-size:12px}.unlimited-option input{width:auto}.wheel-test-controls .limit-control{max-width:210px}
.style-supervisor-controls{display:grid;grid-template-columns:auto minmax(280px,1fr) auto auto;align-items:end;gap:14px;margin:14px 0;border:1px solid #bbd9d3;border-radius:14px;background:#f2faf8;padding:14px}.style-supervisor-controls>label{margin:0}.supervisor-switch{display:flex;align-items:center;gap:7px;padding:10px 0}.supervisor-switch input{width:auto}.reviewing-indicator{color:#0f796d;font-size:12px;font-weight:800}.style-review-card{margin-top:14px;border:1px solid #9dcfc5;border-radius:14px;background:#f6fcfa;padding:16px}.style-review-card>header{display:flex;align-items:center;gap:14px}.style-review-card h3,.style-review-card h4{margin:0}.style-review-card header p{margin:4px 0 0;color:#647780;font-size:12px}.style-score{display:grid;place-items:center;min-width:72px;height:72px;border-radius:18px;background:#147d70;color:#fff}.style-score strong{font-size:28px;line-height:1}.style-score span{font-size:11px}.review-dimensions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;margin-top:14px}.review-dimension{border:1px solid #d7e7e3;border-radius:10px;background:#fff;padding:11px}.review-dimension>div{display:flex;justify-content:space-between;gap:10px}.review-dimension b{color:#147d70}.review-dimension p{margin:7px 0;color:#4f6269;line-height:1.5}.review-dimension small{color:#718188}.review-summary-grid{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-top:10px}.review-summary-grid>div{border-radius:10px;background:#fff;padding:11px}.review-summary-grid p{margin:7px 0 0;line-height:1.6}.review-feedback-output{display:flex;align-items:center;justify-content:space-between;gap:16px;margin-top:10px;border:1px solid #ceb8ec;border-radius:10px;background:#faf7ff;padding:12px}.review-feedback-output p{margin:6px 0 0;line-height:1.6}.review-feedback-output button{flex:0 0 auto}.style-review-card details{margin-top:12px}.style-review-card details pre{max-height:360px;overflow:auto;white-space:pre-wrap;word-break:break-word;border-radius:8px;background:#121923;color:#dbe9e7;padding:10px}.review-error{display:flex;align-items:center;justify-content:space-between;gap:12px;border-radius:10px;background:#fff0f0;color:#a82b35;padding:10px 12px}.review-pending{margin-top:12px;border-radius:10px;background:#edf6f4;color:#407069;padding:10px 12px}@media(max-width:1000px){.style-supervisor-controls{grid-template-columns:1fr}.review-dimensions,.review-summary-grid{grid-template-columns:1fr}.review-feedback-output{align-items:flex-start;flex-direction:column}}
</style>
