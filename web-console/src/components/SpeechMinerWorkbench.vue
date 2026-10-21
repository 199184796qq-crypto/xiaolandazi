<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import '../styles/speech-miner-card-tones.css'
import { confirmAnchorStyleSellingPointWheel, confirmSpeechMiner, guideSpeechMiner, request } from '../api'
import { contentPoolURL, type PoolStatus } from '../contentPools'
import { minerPoolPoints, minerPointEditTasks, type MinerPoolPoint } from '../minerContentPools'
import SourceRiskDetails from './SourceRiskDetails.vue'
import { session } from '../session'
import { riskSources, type RiskReview } from '../riskSources'
import type {
  AnchorStyleSellingPointWheelDocument,
  AnchorStyleSellingPointWheelItem,
  AnchorStyleSellingPointWheelResponse,
  SpeechMinerFactDraft,
  SpeechMinerMessage,
  SpeechMinerOption,
} from '../types'

type MinerRiskReview = RiskReview

const props = withDefaults(defineProps<{
  planId: number
  tenantId?: number
  roomId?: number
  poolStatus?: PoolStatus | null
  poolError?: string
  wheel?: AnchorStyleSellingPointWheelResponse | null
  wheelLoading?: boolean
  riskReviews?: MinerRiskReview[]
  riskLoading?: boolean
}>(), { tenantId: 0, roomId: 0, poolStatus: null, poolError: '', wheel: null, wheelLoading: false, riskReviews: () => [], riskLoading: false })

const emit = defineEmits<{ updated: []; poolUpdated: [value: PoolStatus]; confirmRisk: [value: MinerRiskReview]; appealRisk: [value: MinerRiskReview, reason: string] }>()

const history = ref<SpeechMinerMessage[]>([])
const options = ref<SpeechMinerOption[]>([])
const selectionMode = ref<'single' | 'multiple'>('single')
const selectedOptionValues = ref<string[]>([])
const draftFacts = ref<SpeechMinerFactDraft[]>([])
const input = ref('')
const enterToSend = ref(false)
const composingMessage = ref(false)
const busy = ref(false)
const applying = ref(false)
const started = ref(false)
const miningMode = ref<'fact_gaps' | 'new_selling_point' | 'audience_reverse'>('fact_gaps')
const sessionId = ref('')
const reviewOpen = ref(false)
const readyToConfirm = ref(false)
const factsSaved = ref(false)
const saveProgress = ref(0)
const saveStage = ref('')
let scopeEpoch = 0
const error = ref('')
const notice = ref('')
const chatEnd = ref<HTMLElement | null>(null)
const minerGrid = ref<HTMLElement | null>(null)
const splitPercent = ref(40)
const resizing = ref(false)
const pointListHeight = ref(580)
const pointListResizeStart = ref({ y: 0, height: 580 })
const resizingPointList = ref(false)
const chatMessageHeight = ref(460)
const chatResizeStart = ref({ y: 0, height: 460 })
const resizingChat = ref(false)
const heightPreferenceKey = computed(() => {
  const actor = session.bootstrap?.actor
  if (!actor?.user_id) return ''
  return `speech-miner-heights:v1:${actor.user_id}:${props.tenantId || actor.tenant_id || 0}`
})
let activeHeightPreferenceKey = ''
const expandedPointKeys = ref<string[]>([])
const collapsedPointKeys = ref<string[]>([])
const locatedRiskSource = ref('')
const selectedPoint = ref<AnchorStyleSellingPointWheelItem | null>(null)
const selectedPointNumber = ref(0)
const selectedScope = ref('common')
const legacyMode = computed(() => selectedScope.value === 'legacy')
type SellingPointEditDraft = {
  index: number
  name: string
  painPoint: string
  targetAudience: string
  angles: string[]
  angleTaskIds: string[]
  originalTaskIds: string[]
  poolKey: string
  setId: number
  angleEditingIndex: number | null
  pendingDeleteAngleIndex: number | null
  pendingDeletePoint: boolean
}
const pointEdit = ref<SellingPointEditDraft | null>(null)
const editingPointsSnapshot = ref<AnchorStyleSellingPointWheelItem[] | null>(null)
const pointEditSaving = ref(false)
const pointEditError = ref('')

const splitStyle = computed(() => ({ '--miner-left-width': `${splitPercent.value}%` }))

const wheelDocument = computed<AnchorStyleSellingPointWheelDocument | null>(() => {
  const raw = props.wheel?.wheel_json?.trim()
  if (!raw) return null
  try {
    const parsed = JSON.parse(raw) as AnchorStyleSellingPointWheelDocument
    return Array.isArray(parsed.卖点列表) ? parsed : null
  } catch {
    return null
  }
})
const points = computed(() => pointEdit.value && editingPointsSnapshot.value ? editingPointsSnapshot.value : legacyMode.value ? wheelDocument.value?.卖点列表 || [] : minerPoolPoints(props.poolStatus, selectedScope.value))
const scopeOptions = computed(() => [
  { value: 'common', label: '公共内容' },
  ...(props.poolStatus?.products || []).map(product => ({ value: `product:${product.id}`, label: `${product.link}号 · ${product.name}` })),
  ...(wheelDocument.value ? [{ value: 'legacy', label: '原始挖掘素材' }] : []),
])
const scopeLabel = computed(() => scopeOptions.value.find(option => option.value === selectedScope.value)?.label || '公共内容')
const poolEditable = computed(() => !!props.roomId && !!props.poolStatus?.set_id && ['ready', 'synced'].includes(props.poolStatus.state))
const resultsLoading = computed(() => legacyMode.value ? props.wheelLoading : !props.poolStatus && !props.poolError)
const resultNotice = computed(() => {
  if (legacyMode.value) return '原始挖掘素材供内容池更新时参考；这里的修改还需更新内容池并保存智能体。'
  if (props.poolError) return props.poolError
  if (props.poolStatus?.state === 'updating') return '内容池正在更新，当前展示上一版内容，完成后可编辑。'
  if (props.poolStatus && !poolEditable.value) return '资料已变化，请先点击上方“更新受影响内容”，完成后编辑并保存智能体。'
  if (props.poolStatus?.set_id !== props.poolStatus?.saved_set_id) return '这些内容尚未保存到当前房间智能体；确认后点击“保存智能体方案”。'
  return ''
})
const angleCount = computed(() => points.value.reduce((total, item) => total + (item.角度?.length || 0), 0))
const collapsedPointCount = computed(() => points.value.reduce(
  (total, point, index) => total + (collapsedPointKeys.value.includes(pointKey(point, index)) ? 1 : 0),
  0,
))

function mergeDraftFacts(incoming: SpeechMinerFactDraft[]) {
  const next = [...draftFacts.value]
  for (const fact of incoming || []) {
    const index = next.findIndex((item) => item.category === fact.category && item.key === fact.key)
    if (index >= 0) next[index] = fact
    else next.push(fact)
  }
  draftFacts.value = next
  if (next.length > 12) notice.value = '本轮已超过12条，请在核对时移除部分事实后分批保存，内容没有被静默丢弃。'
}

async function scrollChat() {
  await nextTick()
  chatEnd.value?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
}

function updateSplit(clientX: number) {
  const bounds = minerGrid.value?.getBoundingClientRect()
  if (!bounds?.width) return
  const next = ((clientX - bounds.left) / bounds.width) * 100
  splitPercent.value = Math.round(Math.min(68, Math.max(28, next)) * 10) / 10
}

function stopResize() {
  if (!resizing.value) return
  resizing.value = false
  window.removeEventListener('pointermove', handleResizeMove)
  window.removeEventListener('pointerup', stopResize)
  window.removeEventListener('pointercancel', stopResize)
  document.body.classList.remove('speech-miner-is-resizing')
}

function handleResizeMove(event: PointerEvent) {
  if (!resizing.value) return
  updateSplit(event.clientX)
}

function startResize(event: PointerEvent) {
  event.preventDefault()
  resizing.value = true
  updateSplit(event.clientX)
  window.addEventListener('pointermove', handleResizeMove)
  window.addEventListener('pointerup', stopResize)
  window.addEventListener('pointercancel', stopResize)
  document.body.classList.add('speech-miner-is-resizing')
}

function resizeByKeyboard(event: KeyboardEvent) {
  if (event.key === 'ArrowLeft') splitPercent.value = Math.max(28, splitPercent.value - 2)
  else if (event.key === 'ArrowRight') splitPercent.value = Math.min(68, splitPercent.value + 2)
  else if (event.key === 'Home') splitPercent.value = 28
  else if (event.key === 'End') splitPercent.value = 68
  else return
  event.preventDefault()
}

function resetSplit() {
  splitPercent.value = 40
}

function pointKey(point: AnchorStyleSellingPointWheelItem, index: number) {
  return `${index}:${point.卖点名}`
}

function visiblePointAngles(point: AnchorStyleSellingPointWheelItem, index: number) {
  return expandedPointKeys.value.includes(pointKey(point, index)) ? point.角度 || [] : (point.角度 || []).slice(0, 4)
}

function riskForAngle(pointIndex: number, angleIndex: number) {
  if (!legacyMode.value) return undefined
  const sourceKey = `wheel/${pointIndex}/angle/${angleIndex}`
  return props.riskReviews.find(review => review.status !== 'confirmed' && (review.source_key === sourceKey || (review.sources || []).some(source => source.source_key === sourceKey)))
}

function headerRisks(pointIndex: number) {
  if (!legacyMode.value) return []
  return props.riskReviews.filter(review => review.status !== 'confirmed' && riskSources(review).some(source =>
    ['name', 'audience', 'pain'].some(field => source.source_key === `wheel/${pointIndex}/${field}`)))
}

function locatedHeaderRisk(pointIndex: number) {
  return headerRisks(pointIndex).find(review => riskSources(review).some(source => source.source_key === locatedRiskSource.value))?.claim_sha256
}

async function locateRisk(claimHash: string) {
  const review = props.riskReviews.find(item => item.claim_sha256 === claimHash && item.status !== 'confirmed')
  if (!review) return false
  if (pointEdit.value || pointEditSaving.value) return false
  selectedScope.value = 'legacy'
  await nextTick()
  const source = [review.source_key, ...(review.sources || []).map(item => item.source_key)]
    .find(key => {
      const match = /^wheel\/(\d+)\/(?:name|audience|pain|angle\/(\d+))$/.exec(key)
      const point = match ? points.value[Number(match[1])] : undefined
      return !!point && (match?.[2] === undefined || Number(match[2]) < (point.角度?.length || 0))
    })
  if (!source) return false
  const pointIndex = Number(source.split('/')[1])
  const key = pointKey(points.value[pointIndex]!, pointIndex)
  collapsedPointKeys.value = collapsedPointKeys.value.filter(item => item !== key)
  if (!expandedPointKeys.value.includes(key)) expandedPointKeys.value.push(key)
  locatedRiskSource.value = source
  await nextTick()
  const card = minerGrid.value?.querySelector<HTMLElement>(`[data-wheel-point-index="${pointIndex}"]`)
  const target = card?.querySelector<HTMLElement>(`[data-wheel-source-key="${source}"]`) || card
  target?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' })
  target?.focus({ preventScroll: true })
  return !!target
}

watch(() => props.riskReviews, reviews => {
  if (!reviews.some(review => review.status !== 'confirmed' &&
    (review.source_key === locatedRiskSource.value || review.sources?.some(source => source.source_key === locatedRiskSource.value)))) {
    locatedRiskSource.value = ''
  }
}, { deep: true })

async function locatePoolTask(poolKey: string, taskID: string, setID: number) {
  if (pointEdit.value || pointEditSaving.value || !poolEditable.value || props.poolStatus?.set_id !== setID) return false
  const pool = props.poolStatus.pools.find(pool => pool.key === poolKey && pool.tasks?.some(task => task.id === taskID))
  if (!pool) return false
  const epoch = scopeEpoch
  selectedScope.value = pool.owner ? `product:${pool.owner}` : 'common'
  await nextTick()
  if (epoch !== scopeEpoch || props.poolStatus?.set_id !== setID) return false
  const pointIndex = points.value.findIndex(point => (point as MinerPoolPoint).poolKey === poolKey && (point as MinerPoolPoint).taskIds.includes(taskID))
  const point = points.value[pointIndex] as MinerPoolPoint | undefined
  if (!point) return false
  const angleIndex = point.taskIds.indexOf(taskID)
  beginAngleEdit(point, pointIndex, angleIndex)
  if (!expandedPointKeys.value.includes(pointKey(point, pointIndex))) expandedPointKeys.value.push(pointKey(point, pointIndex))
  await nextTick()
  const target = minerGrid.value?.querySelector<HTMLElement>(`[data-angle-editor="${pointIndex}-${angleIndex}"]`)
  target?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  target?.focus({ preventScroll: true })
  return !!target
}
async function locateOriginalWheel() {
  if (pointEdit.value || pointEditSaving.value || !wheelDocument.value) return false
  selectedScope.value = 'legacy'
  await nextTick()
  minerGrid.value?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  return !!minerGrid.value
}
defineExpose({ locateRisk, locatePoolTask, locateOriginalWheel })

function riskCanConfirm(review?: MinerRiskReview) {
  if (!review) return false
  return review.severity === 'P1' || review.severity === 'P2' || (review.severity === 'P0' && review.semantic_decision !== 'hard')
}

function riskLabel(review?: MinerRiskReview) {
  if (!review) return ''
  if (riskCanConfirm(review)) return '待确认'
  return '请修改'
}

function confirmAngleRisk(pointIndex: number, angleIndex: number) {
  const review = riskForAngle(pointIndex, angleIndex)
  if (review && riskCanConfirm(review)) emit('confirmRisk', review)
}

function togglePointAngles(point: AnchorStyleSellingPointWheelItem, index: number) {
  const key = pointKey(point, index)
  expandedPointKeys.value = expandedPointKeys.value.includes(key)
    ? expandedPointKeys.value.filter((item) => item !== key)
    : [...expandedPointKeys.value, key]
}

function isPointCollapsed(point: AnchorStyleSellingPointWheelItem, index: number) {
  return collapsedPointKeys.value.includes(pointKey(point, index))
}

function togglePointCollapsed(point: AnchorStyleSellingPointWheelItem, index: number) {
  const key = pointKey(point, index)
  collapsedPointKeys.value = collapsedPointKeys.value.includes(key)
    ? collapsedPointKeys.value.filter((item) => item !== key)
    : [...collapsedPointKeys.value, key]
}

function expandAllPoints() {
  collapsedPointKeys.value = []
}

function collapseAllPoints() {
  collapsedPointKeys.value = points.value.map((point, index) => pointKey(point, index))
}

function openPointAngles(point: AnchorStyleSellingPointWheelItem, index: number) {
  selectedPoint.value = point
  selectedPointNumber.value = index + 1
}

function closePointAngles() {
  selectedPoint.value = null
  selectedPointNumber.value = 0
}

function isPointEditing(index: number) {
  return pointEdit.value?.index === index
}

function beginPointEdit(point: AnchorStyleSellingPointWheelItem, index: number, angleIndex: number | null = null) {
  if (pointEditSaving.value) return
  if (!legacyMode.value && !poolEditable.value) { notice.value = resultNotice.value || '请先更新内容池。'; return }
  closePointAngles()
  const key = pointKey(point, index)
  collapsedPointKeys.value = collapsedPointKeys.value.filter(item => item !== key)
  editingPointsSnapshot.value = JSON.parse(JSON.stringify(points.value)) as AnchorStyleSellingPointWheelItem[]
  pointEdit.value = {
    index,
    name: point.卖点名 || '',
    painPoint: point.痛点 || '',
    targetAudience: point.目标人群 || '',
    angles: [...(point.角度 || [])],
    angleTaskIds: [...((point as MinerPoolPoint).taskIds || [])],
    originalTaskIds: [...((point as MinerPoolPoint).taskIds || [])],
    poolKey: (point as MinerPoolPoint).poolKey || '',
    setId: props.poolStatus?.set_id || 0,
    angleEditingIndex: angleIndex,
    pendingDeleteAngleIndex: null,
    pendingDeletePoint: false,
  }
  pointEditError.value = ''
}

function cancelPointEdit() {
  if (pointEditSaving.value) return
  pointEdit.value = null
  pointEditError.value = ''
}

function beginAngleEdit(point: AnchorStyleSellingPointWheelItem, pointIndex: number, angleIndex: number) {
  if (!isPointEditing(pointIndex)) beginPointEdit(point, pointIndex, angleIndex)
  else if (pointEdit.value) pointEdit.value.angleEditingIndex = angleIndex
  pointEditError.value = ''
  void nextTick(() => {
    const input = document.querySelector<HTMLInputElement>(`[data-angle-editor="${pointIndex}-${angleIndex}"]`)
    input?.focus()
    input?.select()
  })
}

function addPointAngle() {
  if (!pointEdit.value || pointEditSaving.value) return
  pointEdit.value.angles.push('')
  pointEdit.value.angleTaskIds.push('')
  const angleIndex = pointEdit.value.angles.length - 1
  pointEdit.value.angleEditingIndex = angleIndex
  pointEdit.value.pendingDeleteAngleIndex = null
  pointEditError.value = ''
  void nextTick(() => document.querySelector<HTMLInputElement>(`[data-angle-editor="${pointEdit.value?.index}-${angleIndex}"]`)?.focus())
}

function requestDeleteAngle(index: number) {
  if (!pointEdit.value || pointEditSaving.value) return
  pointEdit.value.pendingDeleteAngleIndex = index
  pointEdit.value.angleEditingIndex = null
  pointEditError.value = ''
}

function cancelDeleteAngle() {
  if (pointEdit.value) pointEdit.value.pendingDeleteAngleIndex = null
}

function confirmDeleteAngle(index: number) {
  if (!pointEdit.value || pointEditSaving.value) return
  if (pointEdit.value.angles.length <= 1) {
    pointEditError.value = '每个卖点至少保留一条内容，不能删除最后一条。'
    pointEdit.value.pendingDeleteAngleIndex = null
    return
  }
  pointEdit.value.angles.splice(index, 1)
  pointEdit.value.angleTaskIds.splice(index, 1)
  pointEdit.value.pendingDeleteAngleIndex = null
  pointEdit.value.angleEditingIndex = null
}

function pointEditValidationError() {
  const draft = pointEdit.value
  if (!draft) return '没有正在编辑的卖点。'
  if (!draft.name.trim()) return '卖点名称不能为空。'
  if (legacyMode.value && !draft.painPoint.trim()) return '卖点说明不能为空。'
  if (legacyMode.value && !draft.targetAudience.trim()) return '适用人群不能为空。'
  if (!draft.angles.length) return '至少需要保留一条内容。'
  if (draft.angles.some(angle => !angle.trim())) return '内容不能为空，请填写或删除空白内容。'
  const normalizedAngles = draft.angles.map(angle => angle.trim())
  if (new Set(normalizedAngles).size !== normalizedAngles.length) return '同一个卖点下不能保存重复内容。'
  const duplicateName = legacyMode.value && points.value.some((point, index) => index !== draft.index && point.卖点名.trim() === draft.name.trim())
  if (duplicateName) return '卖点名称不能与其他卖点重复。'
  return ''
}

function buildEditedWheel(removePoint = false) {
  if (!wheelDocument.value || !pointEdit.value) return null
  const draft = pointEdit.value
  const sellingPoints = wheelDocument.value.卖点列表.map(point => ({
    ...point,
    角度: [...(point.角度 || [])],
  }))
  if (removePoint) sellingPoints.splice(draft.index, 1)
  else sellingPoints[draft.index] = {
    ...sellingPoints[draft.index],
    卖点名: draft.name.trim(),
    痛点: draft.painPoint.trim(),
    目标人群: draft.targetAudience.trim(),
    角度: draft.angles.map(angle => angle.trim()),
  }
  return { ...wheelDocument.value, 卖点列表: sellingPoints }
}

async function persistEditedWheel(removePoint = false) {
  if (!pointEdit.value || pointEditSaving.value || !props.planId) return
  if (legacyMode.value && !props.wheel?.asset_id) return
  if (legacyMode.value && removePoint && points.value.length <= 1) {
    pointEditError.value = '至少需要保留一个卖点，不能删除最后一个。'
    pointEdit.value.pendingDeletePoint = false
    return
  }
  if (!removePoint) {
    const validationError = pointEditValidationError()
    if (validationError) {
      pointEditError.value = validationError
      return
    }
  }
  const nextWheel = legacyMode.value ? buildEditedWheel(removePoint) : null
  if (legacyMode.value && !nextWheel) {
    pointEditError.value = '当前挖掘数据不可用，请刷新后重试。'
    return
  }
  pointEditSaving.value = true
  pointEditError.value = ''
  const owner = { plan: props.planId, room: props.roomId, tenant: props.tenantId, scope: selectedScope.value, epoch: scopeEpoch }
  const current = () => owner.plan === props.planId && owner.room === props.roomId && owner.tenant === props.tenantId && owner.scope === selectedScope.value && owner.epoch === scopeEpoch
  try {
    if (legacyMode.value) {
      await confirmAnchorStyleSellingPointWheel(owner.plan, props.wheel!.asset_id, { tenant_id: owner.tenant || undefined, wheel_json: JSON.stringify(nextWheel) })
    } else {
      const draft = pointEdit.value
      const url = contentPoolURL(owner.plan, owner.room, owner.tenant).replace('/content-pools?', '/content-pools/edit?')
      const status = await request<PoolStatus>(url, { method: 'POST', body: JSON.stringify({ expected_set_id: draft.setId, pool_key: draft.poolKey, task_ids: draft.originalTaskIds, tasks: removePoint ? [] : minerPointEditTasks(draft) }), suppressErrorDialog: true })
      if (!current()) return
      emit('poolUpdated', status)
    }
    if (!current()) return
    notice.value = legacyMode.value ? '原始挖掘素材已保存；更新内容池后，再保存智能体方案。' : `${scopeLabel.value}的修改已保存。点击“保存智能体方案”后，下次开播采用。`
    pointEdit.value = null
    emit('updated')
  } catch (cause) {
    if (current()) pointEditError.value = cause instanceof Error ? cause.message : '卖点修改保存失败'
  } finally {
    if (current()) pointEditSaving.value = false
  }
}

function requestDeletePoint() {
  if (!pointEdit.value || pointEditSaving.value) return
  pointEdit.value.pendingDeletePoint = true
  pointEdit.value.pendingDeleteAngleIndex = null
  pointEditError.value = ''
}

function handleWindowKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') closePointAngles()
}

function updatePointListHeight(event: PointerEvent) {
  if (!resizingPointList.value) return
  const next = pointListResizeStart.value.height + event.clientY - pointListResizeStart.value.y
  pointListHeight.value = Math.round(Math.min(1000, Math.max(360, next)))
}

function stopPointListResize() {
  if (!resizingPointList.value) return
  resizingPointList.value = false
  window.removeEventListener('pointermove', updatePointListHeight)
  window.removeEventListener('pointerup', stopPointListResize)
  window.removeEventListener('pointercancel', stopPointListResize)
  document.body.classList.remove('speech-miner-is-resizing-vertical')
  saveHeightPreferences()
}

function startPointListResize(event: PointerEvent) {
  event.preventDefault()
  pointListResizeStart.value = { y: event.clientY, height: pointListHeight.value }
  resizingPointList.value = true
  window.addEventListener('pointermove', updatePointListHeight)
  window.addEventListener('pointerup', stopPointListResize)
  window.addEventListener('pointercancel', stopPointListResize)
  document.body.classList.add('speech-miner-is-resizing-vertical')
}

function resizePointListByKeyboard(event: KeyboardEvent) {
  if (event.key === 'ArrowUp') pointListHeight.value = Math.max(360, pointListHeight.value - 40)
  else if (event.key === 'ArrowDown') pointListHeight.value = Math.min(1000, pointListHeight.value + 40)
  else if (event.key === 'Home') pointListHeight.value = 360
  else if (event.key === 'End') pointListHeight.value = 1000
  else return
  event.preventDefault()
  saveHeightPreferences()
}

function updateChatMessageHeight(event: PointerEvent) {
  if (!resizingChat.value) return
  const next = chatResizeStart.value.height + event.clientY - chatResizeStart.value.y
  chatMessageHeight.value = Math.round(Math.min(900, Math.max(280, next)))
}

function stopChatResize() {
  if (!resizingChat.value) return
  resizingChat.value = false
  window.removeEventListener('pointermove', updateChatMessageHeight)
  window.removeEventListener('pointerup', stopChatResize)
  window.removeEventListener('pointercancel', stopChatResize)
  document.body.classList.remove('speech-miner-is-resizing-chat')
  saveHeightPreferences()
}

function startChatResize(event: PointerEvent) {
  event.preventDefault()
  chatResizeStart.value = { y: event.clientY, height: chatMessageHeight.value }
  resizingChat.value = true
  window.addEventListener('pointermove', updateChatMessageHeight)
  window.addEventListener('pointerup', stopChatResize)
  window.addEventListener('pointercancel', stopChatResize)
  document.body.classList.add('speech-miner-is-resizing-chat')
}

function resizeChatByKeyboard(event: KeyboardEvent) {
  if (event.key === 'ArrowUp') chatMessageHeight.value = Math.max(280, chatMessageHeight.value - 40)
  else if (event.key === 'ArrowDown') chatMessageHeight.value = Math.min(900, chatMessageHeight.value + 40)
  else if (event.key === 'Home') chatMessageHeight.value = 280
  else if (event.key === 'End') chatMessageHeight.value = 900
  else return
  event.preventDefault()
  saveHeightPreferences()
}

function resetChatHeight() {
  chatMessageHeight.value = 460
  saveHeightPreferences()
}

function saveHeightPreferences() {
  if (!activeHeightPreferenceKey) return
  try {
    window.localStorage.setItem(activeHeightPreferenceKey, JSON.stringify({
      listHeight: pointListHeight.value,
      chatHeight: chatMessageHeight.value,
    }))
  } catch {
    // Resizing remains usable when browser storage is unavailable.
  }
}

function restoreHeightPreferences(key: string) {
  // Finish an in-progress drag under its original account before changing scope.
  stopPointListResize()
  stopChatResize()
  activeHeightPreferenceKey = key
  pointListHeight.value = 580
  chatMessageHeight.value = 460
  if (!key) return
  try {
    const saved = JSON.parse(window.localStorage.getItem(key) || 'null')
    if (typeof saved?.listHeight === 'number' && Number.isFinite(saved.listHeight)) {
      pointListHeight.value = Math.round(Math.min(1000, Math.max(360, saved.listHeight)))
    }
    if (typeof saved?.chatHeight === 'number' && Number.isFinite(saved.chatHeight)) {
      chatMessageHeight.value = Math.round(Math.min(900, Math.max(280, saved.chatHeight)))
    }
  } catch {
    // Missing or invalid preferences use the normal default heights.
  }
}

function optionAction(option: SpeechMinerOption) {
  return option.action || 'answer'
}

function isOptionSelected(option: SpeechMinerOption) {
  return selectedOptionValues.value.includes(option.value)
}

function handleOptionClick(option: SpeechMinerOption) {
  if (selectionMode.value !== 'multiple' || optionAction(option) !== 'answer') {
    void sendMessage(option.value, optionAction(option))
    return
  }
  selectedOptionValues.value = isOptionSelected(option)
    ? selectedOptionValues.value.filter((value) => value !== option.value)
    : [...selectedOptionValues.value, option.value]
}

function submitSelectedOptions() {
  const values = options.value
    .filter((option) => optionAction(option) === 'answer' && isOptionSelected(option))
    .map((option) => option.value)
  if (!values.length) return
  void sendMessage(values.join('；'))
}

function handleComposerKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || !enterToSend.value || event.shiftKey || event.ctrlKey || event.altKey || event.metaKey || event.isComposing || composingMessage.value || event.keyCode === 229) return
  event.preventDefault()
  if (!event.repeat) void sendMessage()
}

async function sendMessage(value?: string, action: 'answer' | 'skip' | 'complete' = 'answer') {
  const message = (value ?? input.value).trim()
  if (!message || busy.value || applying.value || !props.planId) return
  if (factsSaved.value) { notice.value = '本轮事实已保存，请先重试保存或开启新一轮，不修改已确认的内容。'; return }
  if (history.value.length > 57) { notice.value = '本轮对话已较长，请先核对并保存事实，再开始下一轮。'; return }
  started.value = true
  input.value = ''
  error.value = ''
  notice.value = ''
  options.value = []
  selectedOptionValues.value = []
  const priorHistory = history.value.slice(-16)
  const plan = props.planId, tenant = props.tenantId, epoch = scopeEpoch
  const current = () => plan === props.planId && tenant === props.tenantId && epoch === scopeEpoch
  if (!sessionId.value) sessionId.value = crypto.randomUUID()
  history.value.push({ role: 'user', text: message })
  busy.value = true
  await scrollChat()
  if (!current()) return
  try {
    const result = await guideSpeechMiner(plan, {
      tenant_id: tenant || undefined,
      message,
      history: priorHistory,
      draft_facts: draftFacts.value,
      mode: miningMode.value,
      action,
    })
    if (!current()) return
    mergeDraftFacts(result.captured_facts || [])
    history.value.push({ role: 'agent', text: result.reply })
    options.value = result.options || []
    selectionMode.value = result.selection_mode === 'multiple' ? 'multiple' : 'single'
    selectedOptionValues.value = []
    await scrollChat()
    if (!current()) return
    if (result.completed) {
      readyToConfirm.value = true
      notice.value = draftFacts.value.length ? '本轮提问完成，请核对待保存事实；确认后才会保存挖掘数据，不会发布到直播间。' : '本轮没有新的事实，现有结果保持不变。'
    }
  } catch (cause) {
    if (current()) error.value = cause instanceof Error ? cause.message : '话术挖掘机暂时没有响应'
  } finally {
    if (current()) busy.value = false
  }
}

async function startMining(mode: 'fact_gaps' | 'new_selling_point' | 'audience_reverse' = 'fact_gaps') {
  if (busy.value || applying.value || !props.planId) return
  if (draftFacts.value.length && !window.confirm('本轮有尚未保存的事实，重新开始会放弃这些临时内容。继续吗？')) return
  history.value = []
  input.value = ''
  options.value = []
  selectionMode.value = 'single'
  selectedOptionValues.value = []
  draftFacts.value = []
  started.value = false
  notice.value = ''
  error.value = ''
  miningMode.value = mode
  sessionId.value = crypto.randomUUID()
  readyToConfirm.value = false
  factsSaved.value = false
  reviewOpen.value = false
  saveProgress.value = 0
  saveStage.value = ''
  const plan = props.planId, tenant = props.tenantId, epoch = ++scopeEpoch
  const current = () => plan === props.planId && tenant === props.tenantId && epoch === scopeEpoch
  busy.value = true
  started.value = true
  const opening = {
    new_selling_point: '我们先挖掘新的卖点，从你已经提供的真实内容里找亮点。',
    fact_gaps: '我们先补充商品的真实信息，我会一次问你一个问题。',
    audience_reverse: '我们先从目标人群出发，看看他们关心什么，再找有事实支持的沟通内容。',
  }[mode]
  const startText = {
    new_selling_point: '开始挖掘卖点',
    fact_gaps: '开始挖掘内容',
    audience_reverse: '开始人群反推',
  }[mode]
  history.value.push({ role: 'user', text: startText }, { role: 'agent', text: opening })
  await scrollChat()
  if (!current()) return
  try {
    const result = await guideSpeechMiner(plan, { tenant_id: tenant || undefined, message: '', mode, start: true, history: [], draft_facts: [] })
    if (!current()) return
    history.value[1] = { role: 'agent', text: result.reply || opening }
    mergeDraftFacts(result.captured_facts || [])
    options.value = result.options || []
    selectionMode.value = result.selection_mode === 'multiple' ? 'multiple' : 'single'
    readyToConfirm.value = !!result.completed
    await scrollChat()
  } catch (cause) {
    if (current()) {
      error.value = cause instanceof Error ? cause.message : '挖掘助手暂时没有响应'
      started.value = false
    }
  }
  finally { if (current()) busy.value = false }
}

function isMiningModeActive(mode: typeof miningMode.value) {
  return started.value && miningMode.value === mode
}

function resetMiningSession() {
  scopeEpoch++
  busy.value = false
  applying.value = false
  started.value = false
  sessionId.value = ''
  history.value = []
  options.value = []
  selectedOptionValues.value = []
  selectionMode.value = 'single'
  input.value = ''
  draftFacts.value = []
  reviewOpen.value = false
  readyToConfirm.value = false
  factsSaved.value = false
  saveProgress.value = 0
  saveStage.value = ''
}

async function saveAndExitMining() {
  if (busy.value || applying.value || !props.planId) return
  const plan = props.planId, tenant = props.tenantId, epoch = scopeEpoch
  const unsent = [
    ...options.value.filter(option => optionAction(option) === 'answer' && isOptionSelected(option)).map(option => option.value),
    input.value.trim(),
  ].filter(Boolean).join('；')
  if (unsent && !factsSaved.value) {
    await sendMessage(unsent)
    if (plan !== props.planId || tenant !== props.tenantId || epoch !== scopeEpoch) return
    if (error.value) { input.value = unsent; selectedOptionValues.value = []; return }
  }
  if (!draftFacts.value.length) {
    resetMiningSession()
    error.value = ''
    notice.value = '本轮没有新增信息，已退出。已有挖掘结果保持不变。'
    await scrollChat()
    return
  }
  await persistMining()
}

async function handleMiningModeClick(mode: typeof miningMode.value) {
  if (isMiningModeActive(mode)) await saveAndExitMining()
  else if (!started.value) await startMining(mode)
}

function removeDraftFact(index: number) {
  if (applying.value || factsSaved.value) return
  draftFacts.value = draftFacts.value.filter((_, itemIndex) => itemIndex !== index)
}

function openReview() {
  if (!draftFacts.value.length || busy.value || applying.value) return
  reviewOpen.value = true
}

async function finishMining() {
  if (applying.value || busy.value || !props.planId || !draftFacts.value.length) return
  if (!reviewOpen.value) { openReview(); return }
  await persistMining()
}

async function persistMining() {
  if (applying.value || busy.value || !props.planId || !draftFacts.value.length) return
  const plan = props.planId, tenant = props.tenantId, epoch = scopeEpoch
  const current = () => plan === props.planId && tenant === props.tenantId && epoch === scopeEpoch
  const facts = draftFacts.value.map(fact => ({ ...fact }))
  if (facts.length > 12 || facts.some(fact => !fact.key.trim() || !fact.value.trim())) { error.value = '请核对完整的事实名称与内容，单轮最多12条。'; return }
  applying.value = true
  error.value = ''
  notice.value = ''
  saveProgress.value = 0
  saveStage.value = '正在准备保存确认事实…'
  try {
    const result = await confirmSpeechMiner(plan, {
      tenant_id: tenant || undefined, confirmed: true, session_id: sessionId.value,
      facts, history: history.value.map(message => ({ ...message })),
    }, event => {
      if (!current() || event.type !== 'progress') return
      saveProgress.value = event.percent ?? saveProgress.value
      saveStage.value = event.message
    })
    if (!current()) return
    factsSaved.value = result.facts_saved
    notice.value = result.notice
    error.value = result.error || ''
    options.value = []
    selectedOptionValues.value = []
    if (result.wheel_ready) {
      resetMiningSession()
    } else {
      saveStage.value = '事实已保存，挖掘数据整理未完成；可以重试保存'
      // Keep the confirmed facts and same session ID for an idempotent retry.
    }
    emit('updated')
    await scrollChat()
  } catch (cause) {
    if (current()) { error.value = cause instanceof Error ? cause.message : '保存挖掘数据失败'; saveStage.value = '本次未完成，请核对提示后重试' }
  } finally { if (current()) applying.value = false }
}

watch([() => props.planId, () => props.tenantId, () => props.roomId], () => {
  scopeEpoch++
  busy.value = false
  applying.value = false
  sessionId.value = ''
  reviewOpen.value = false
  readyToConfirm.value = false
  factsSaved.value = false
  saveProgress.value = 0
  saveStage.value = ''
  history.value = []
  options.value = []
  selectionMode.value = 'single'
  selectedOptionValues.value = []
  draftFacts.value = []
  input.value = ''
  started.value = false
  error.value = ''
  notice.value = ''
  expandedPointKeys.value = []
  collapsedPointKeys.value = []
  pointEdit.value = null
  locatedRiskSource.value = ''
  selectedScope.value = 'common'
  pointEditSaving.value = false
  pointEditError.value = ''
  closePointAngles()
})

watch(selectedScope, () => {
  expandedPointKeys.value = []
  collapsedPointKeys.value = []
  pointEdit.value = null
  pointEditError.value = ''
  locatedRiskSource.value = ''
  notice.value = ''
  closePointAngles()
})
watch(scopeOptions, options => {
  if (!options.some(option => option.value === selectedScope.value) && !pointEdit.value) selectedScope.value = 'common'
})

watch(heightPreferenceKey, restoreHeightPreferences, { immediate: true })

onMounted(() => window.addEventListener('keydown', handleWindowKeydown))
onBeforeUnmount(() => {
  stopResize()
  stopPointListResize()
  stopChatResize()
  window.removeEventListener('keydown', handleWindowKeydown)
})
</script>

<template>
  <section class="speech-miner" aria-label="话术挖掘机">
    <header class="speech-miner-head">
      <div class="speech-miner-brand">
        <div class="speech-miner-brand-icon" aria-hidden="true"><span>•••</span><b>✦</b></div>
        <div><h3>话术挖掘机</h3><span>GUIDED SPEECH MINER</span><p>左边看挖掘结果，右边跟着智能体一次回答一个问题。选择按钮或直接输入都可以。</p></div>
      </div>
      <div class="speech-miner-head-actions">
        <span>好话术，卖得更好！</span>
      </div>
    </header>

    <div ref="minerGrid" class="speech-miner-grid" :class="{ 'is-resizing': resizing }" :style="splitStyle">
      <section class="speech-miner-results">
        <header>
          <div class="speech-miner-results-title"><strong>已经挖掘出来的</strong><small>{{ points.length }} 个卖点 · {{ angleCount }} 条内容</small></div>
          <div class="speech-miner-results-toolbar">
            <label class="speech-miner-pool-select"><span class="sr-only">内容范围</span><select v-model="selectedScope" aria-label="内容范围" :disabled="!!pointEdit || pointEditSaving"><option v-for="option in scopeOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label>
            <div v-if="points.length" class="speech-miner-results-actions">
              <button type="button" :disabled="!collapsedPointCount" @click="expandAllPoints">全部展开</button>
              <button type="button" :disabled="collapsedPointCount === points.length" @click="collapseAllPoints">全部收缩</button>
            </div>
            <em v-if="legacyMode ? wheel?.fact_package_stale : poolStatus && !poolEditable"><i aria-hidden="true">↻</i>{{ poolStatus?.state === 'updating' && !legacyMode ? '更新中' : '资料已变化' }}</em>
          </div>
        </header>
        <p v-if="resultNotice" class="speech-miner-pool-notice" role="status">{{ resultNotice }}</p>
        <div v-if="resultsLoading" class="speech-miner-empty">正在读取挖掘结果…</div>
        <div v-else-if="!points.length" class="speech-miner-empty">{{ legacyMode ? '还没有原始挖掘素材，可以在右侧开始挖掘。' : poolStatus?.set_id ? '这个范围暂时没有卖点内容。' : '这个范围尚未生成内容池。请先点击上方“更新受影响内容”。' }}</div>
        <div v-else class="speech-miner-point-list" :style="{ height: pointListHeight + 'px' }">
          <article v-for="(point, pointIndex) in points" :key="pointIndex + ':' + point.卖点名" :data-wheel-point-index="pointIndex" tabindex="-1" :class="{ 'is-collapsed': isPointCollapsed(point, pointIndex), 'is-editing': isPointEditing(pointIndex), 'is-risk-located': locatedRiskSource.startsWith('wheel/' + pointIndex + '/') }">
            <span class="speech-miner-point-index">{{ String(pointIndex + 1).padStart(2, '0') }}</span>
            <div class="speech-miner-point-content">
              <div class="speech-miner-point-overview">
                <div class="speech-miner-selling-summary">
                  <i aria-hidden="true"><svg viewBox="0 0 28 28"><path d="M6 4h11l5 5v15H6z"/><path d="M17 4v6h5M10 12h8M10 16h8M10 20h5"/><path d="m19 17 5 5-4 4-5-5z"/></svg></i>
                  <div v-if="isPointEditing(pointIndex) && pointEdit" class="speech-miner-inline-fields">
                    <label><span>卖点名称</span><input v-model="pointEdit.name" maxlength="40" :disabled="pointEditSaving" /></label>
                    <label><span>卖点说明</span><input v-model="pointEdit.painPoint" maxlength="200" :disabled="pointEditSaving" /></label>
                  </div>
                  <div v-else @dblclick="beginPointEdit(point, pointIndex)"><strong :data-wheel-source-key="'wheel/' + pointIndex + '/name'" tabindex="-1" :class="{ 'is-risk-located': locatedRiskSource === 'wheel/' + pointIndex + '/name' }">{{ point.卖点名 }}</strong><p :data-wheel-source-key="'wheel/' + pointIndex + '/pain'" tabindex="-1" :class="{ 'is-risk-located': locatedRiskSource === 'wheel/' + pointIndex + '/pain' }">{{ point.痛点 || '围绕该卖点提供清晰、易理解的选购说明' }}</p></div>
                </div>
                <div class="speech-miner-audience">
                  <i aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="9" cy="8" r="3.5"/><circle cx="16.5" cy="9.5" r="2.8"/><path d="M2.5 20c.5-4.4 2.7-6.5 6.5-6.5s6 2.1 6.5 6.5M14 14c4.2-.5 6.5 1.4 7 5"/></svg></i>
                  <div v-if="isPointEditing(pointIndex) && pointEdit" class="speech-miner-inline-fields"><label><span>适用人群</span><input v-model="pointEdit.targetAudience" maxlength="200" :disabled="pointEditSaving" /></label></div>
                  <div v-else :data-wheel-source-key="'wheel/' + pointIndex + '/audience'" tabindex="-1" :class="{ 'is-risk-located': locatedRiskSource === 'wheel/' + pointIndex + '/audience' }" @dblclick="beginPointEdit(point, pointIndex)"><b>适用人群</b><span>{{ point.目标人群 || '适合关注该卖点的消费者' }}</span></div>
                </div>
              </div>
              <div v-if="!isPointCollapsed(point, pointIndex) || isPointEditing(pointIndex)" class="speech-miner-angle-area">
                <SourceRiskDetails :reviews="headerRisks(pointIndex)" :loading="riskLoading" :can-confirm="session.bootstrap?.actor.role === 'customer'" :located-hash="locatedHeaderRisk(pointIndex)" @confirm="emit('confirmRisk', $event)" @edit="beginPointEdit(point, pointIndex)" @appeal="(review, reason) => emit('appealRisk', review, reason)" />
                <ul v-if="isPointEditing(pointIndex) && pointEdit" class="speech-miner-angle-editor-list">
                  <li
                    v-for="(angle, angleIndex) in pointEdit.angles"
                    :key="angleIndex"
                    :data-wheel-source-key="'wheel/' + pointIndex + '/angle/' + angleIndex"
                    tabindex="-1"
                    :class="['angle-tone-' + ((angleIndex % 6) + 1), { 'is-angle-editing': pointEdit.angleEditingIndex === angleIndex, 'is-delete-pending': pointEdit.pendingDeleteAngleIndex === angleIndex }]"
                    @dblclick="beginAngleEdit(point, pointIndex, angleIndex)"
                  >
                    <template v-if="pointEdit.pendingDeleteAngleIndex === angleIndex">
                      <span>确定删除这条内容？</span>
                      <button type="button" class="danger" :disabled="pointEditSaving" @click="confirmDeleteAngle(angleIndex)">确认删除</button>
                      <button type="button" :disabled="pointEditSaving" @click="cancelDeleteAngle">取消</button>
                    </template>
                    <template v-else-if="pointEdit.angleEditingIndex === angleIndex">
                      <input
                        v-model="pointEdit.angles[angleIndex]"
                        :data-angle-editor="pointIndex + '-' + angleIndex"
                        maxlength="1000"
                        :disabled="pointEditSaving"
                        @keydown.enter.prevent="pointEdit.angleEditingIndex = null"
                        @keydown.esc.prevent="pointEdit.angleEditingIndex = null"
                        @blur="pointEdit.angleEditingIndex = null"
                      />
                      <button type="button" aria-label="完成内容编辑" title="完成" @mousedown.prevent @click="pointEdit.angleEditingIndex = null">✓</button>
                    </template>
                    <template v-else>
                      <span>{{ angle || '空白内容，双击填写' }}</span>
                      <button type="button" aria-label="编辑这条内容" title="编辑" @click="beginAngleEdit(point, pointIndex, angleIndex)">✎</button>
                      <button type="button" class="danger-icon" aria-label="删除这条内容" title="删除" @click="requestDeleteAngle(angleIndex)">×</button>
                    </template>
                  </li>
                  <li class="speech-miner-add-angle"><button type="button" :disabled="pointEditSaving || pointEdit.angles.length >= 20" @click="addPointAngle">＋ 添加内容</button><small v-if="!legacyMode">新增内容沿用本卖点的事实依据</small></li>
                </ul>
                <ul v-else>
                  <li
                    v-for="(angle, angleIndex) in visiblePointAngles(point, pointIndex)"
                    :key="angleIndex + ':' + angle"
                    :data-wheel-source-key="'wheel/' + pointIndex + '/angle/' + angleIndex"
                    tabindex="-1"
                    :class="['angle-tone-' + ((angleIndex % 6) + 1), { 'has-risk': !!riskForAngle(pointIndex, angleIndex), 'is-risk-located': locatedRiskSource === 'wheel/' + pointIndex + '/angle/' + angleIndex }]"
                    title="双击修改"
                    @dblclick="beginAngleEdit(point, pointIndex, angleIndex)"
                  >
                    <span>{{ angle }}</span>
                    <small v-if="riskForAngle(pointIndex, angleIndex)" class="speech-miner-angle-risk">{{ riskLabel(riskForAngle(pointIndex, angleIndex)) }}</small>
                    <button v-if="riskCanConfirm(riskForAngle(pointIndex, angleIndex))" type="button" class="speech-miner-angle-confirm" :disabled="riskLoading" @click.stop="confirmAngleRisk(pointIndex, angleIndex)">我已知晓并确认</button>
                  </li>
                </ul>
                <button
                  v-if="!isPointEditing(pointIndex) && (point.角度?.length || 0) > 4"
                  type="button"
                  class="speech-miner-angle-expand"
                  :aria-expanded="expandedPointKeys.includes(pointKey(point, pointIndex))"
                  @click="togglePointAngles(point, pointIndex)"
                >
                  <span>{{ expandedPointKeys.includes(pointKey(point, pointIndex)) ? '收起部分内容' : '展开其余 ' + ((point.角度?.length || 0) - 4) + ' 条内容' }}</span>
                  <i :class="{ expanded: expandedPointKeys.includes(pointKey(point, pointIndex)) }" aria-hidden="true">⌄</i>
                </button>
                <div v-if="isPointEditing(pointIndex) && pointEdit" class="speech-miner-edit-footer">
                  <p v-if="pointEditError || pointEditValidationError()" role="alert">{{ pointEditError || pointEditValidationError() }}</p>
                  <div v-if="pointEdit.pendingDeletePoint" class="speech-miner-delete-confirm">
                    <span>确定删除整个“{{ pointEdit.name || '未命名卖点' }}”及其全部内容？</span>
                    <button type="button" class="danger" :disabled="pointEditSaving" @click="persistEditedWheel(true)">确认删除</button>
                    <button type="button" :disabled="pointEditSaving" @click="pointEdit.pendingDeletePoint = false">取消</button>
                  </div>
                  <div v-else class="speech-miner-edit-actions">
                    <button type="button" class="danger-link" :disabled="pointEditSaving" @click="requestDeletePoint">删除卖点</button>
                    <span></span>
                    <button type="button" :disabled="pointEditSaving" @click="cancelPointEdit">取消</button>
                    <button type="button" class="primary" :disabled="pointEditSaving || !!pointEditValidationError()" @click="persistEditedWheel(false)">{{ pointEditSaving ? '保存中…' : '保存修改' }}</button>
                  </div>
                </div>
              </div>
            </div>
            <div class="speech-miner-point-actions">
              <button
                v-if="!isPointEditing(pointIndex)"
                type="button"
                class="speech-miner-point-more"
                :class="{ collapsed: isPointCollapsed(point, pointIndex) }"
                :aria-label="isPointCollapsed(point, pointIndex) ? '展开该卖点卡片' : '收起该卖点卡片'"
                :title="isPointCollapsed(point, pointIndex) ? '展开卡片' : '收起卡片'"
                @click="togglePointCollapsed(point, pointIndex)"
              ><i></i><i></i><i></i></button>
              <button
                v-if="!isPointEditing(pointIndex)"
                type="button"
                class="speech-miner-point-edit"
                aria-label="编辑该卖点"
                title="编辑卖点"
                :disabled="!legacyMode && !poolEditable"
                @click="beginPointEdit(point, pointIndex)"
              ><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m4 16.8-.8 4 4-.8L18.4 8.8l-3.2-3.2zM13.8 7l3.2 3.2M3.2 20.8h17.6"/></svg></button>
              <button v-if="!isPointEditing(pointIndex) && !isPointCollapsed(point, pointIndex)" type="button" class="speech-miner-point-chevron" :aria-label="'查看“' + point.卖点名 + '”的全部内容'" @click="openPointAngles(point, pointIndex)">›</button>
            </div>
          </article>
        </div>
        <div
          v-if="points.length"
          class="speech-miner-list-resizer"
          role="separator"
          aria-label="调整卖点列表高度"
          aria-orientation="horizontal"
          :aria-valuenow="pointListHeight"
          aria-valuemin="360"
          aria-valuemax="1000"
          tabindex="0"
          title="向下拖动可显示更多卖点卡片"
          @pointerdown="startPointListResize"
          @keydown="resizePointListByKeyboard"
        ><span aria-hidden="true"></span><small>拖动调整列表高度</small></div>
        <section v-if="draftFacts.length" class="speech-miner-drafts">
          <header><strong>{{ factsSaved ? '本轮已保存事实' : '本轮待核对事实' }}</strong><span>用户确认后才保存，不自动发布</span></header>
          <article v-for="(fact, index) in draftFacts" :key="fact.category + ':' + fact.key">
            <div><strong>{{ fact.key }}</strong><p>{{ fact.value }}</p></div>
            <button type="button" :disabled="applying || factsSaved" @click="removeDraftFact(index)">移除</button>
          </article>
        </section>
      </section>

      <div
        class="speech-miner-resizer"
        role="separator"
        aria-label="调整挖掘结果和助手区域宽度"
        aria-orientation="vertical"
        :aria-valuenow="splitPercent"
        aria-valuemin="28"
        aria-valuemax="68"
        tabindex="0"
        title="拖动调整宽度，双击恢复默认"
        @pointerdown="startResize"
        @keydown="resizeByKeyboard"
        @dblclick="resetSplit"
      ><span aria-hidden="true"><i></i><i></i><i></i></span></div>

      <section class="speech-miner-chat">
        <header><div><i></i><strong>挖掘助手</strong></div><span>一次问一个</span></header>
        <div class="speech-miner-messages" :style="{ height: chatMessageHeight + 'px' }" aria-live="polite">
          <div v-if="!history.length" class="speech-miner-welcome-row">
            <div class="speech-miner-bot" aria-hidden="true"><span></span><i></i><i></i></div>
            <div class="speech-miner-welcome"><strong>不用填复杂表格</strong><p>我会从当前结果里找最缺的一项。你可以点选答案，也可以直接把真实情况完整告诉我。</p></div>
          </div>
          <article v-for="(message, index) in history" :key="index" :class="'is-' + message.role"><p>{{ message.text }}</p></article>
          <div v-if="applying" class="speech-miner-typing is-saving"><i aria-hidden="true"></i><span>正在保存挖掘数据，请稍候…</span></div>
          <div v-else-if="busy" class="speech-miner-typing"><i aria-hidden="true"></i><span>正在整理下一个问题…</span></div>
          <p v-if="notice" class="speech-miner-notice">{{ notice }}</p>
          <p v-if="error" class="speech-miner-error" role="alert">{{ error }}</p>
          <div ref="chatEnd"></div>
        </div>
        <div
          class="speech-miner-chat-resizer"
          role="separator"
          aria-label="调整对话记录区高度"
          aria-orientation="horizontal"
          :aria-valuenow="chatMessageHeight"
          aria-valuemin="280"
          aria-valuemax="900"
          tabindex="0"
          title="上下拖动调整对话高度，双击恢复默认"
          @pointerdown="startChatResize"
          @keydown="resizeChatByKeyboard"
          @dblclick="resetChatHeight"
        ><span aria-hidden="true"></span><small>拖动调整对话高度</small></div>
        <div v-if="options.length" class="speech-miner-options" :class="{ 'is-multiple': selectionMode === 'multiple' }">
          <div v-if="selectionMode === 'multiple'" class="speech-miner-multi-hint"><strong>可多选</strong><span>选好后统一提交，也可以直接输入</span></div>
          <button
            v-for="(option, optionIndex) in options"
            :key="option.value"
            type="button"
            class="speech-miner-option"
            :class="{ 'is-selected': isOptionSelected(option), 'is-control': optionAction(option) !== 'answer' }"
            :aria-pressed="selectionMode === 'multiple' && optionAction(option) === 'answer' ? isOptionSelected(option) : undefined"
            :disabled="busy || applying"
            @click="handleOptionClick(option)"
          >
            <i aria-hidden="true">{{ selectionMode === 'multiple' && optionAction(option) === 'answer' ? (isOptionSelected(option) ? '✓' : '') : optionIndex + 1 }}</i>
            <span>{{ option.label }}</span>
            <b aria-hidden="true">{{ selectionMode === 'multiple' && optionAction(option) === 'answer' ? (isOptionSelected(option) ? '已选' : '可选') : '›' }}</b>
          </button>
          <div v-if="selectionMode === 'multiple'" class="speech-miner-multi-submit">
            <span>已选择 {{ selectedOptionValues.length }} 项</span>
            <button type="button" :disabled="busy || applying || !selectedOptionValues.length" @click="submitSelectedOptions">提交所选</button>
          </div>
        </div>
        <div class="speech-miner-chat-start">
          <button type="button" class="speech-miner-new-point" :class="{ 'is-active-mode': isMiningModeActive('new_selling_point') }" :aria-label="isMiningModeActive('new_selling_point') ? '保存退出：挖掘卖点' : '挖掘卖点'" :aria-pressed="isMiningModeActive('new_selling_point')" :disabled="busy || applying || !planId || (started && miningMode !== 'new_selling_point')" @click="handleMiningModeClick('new_selling_point')">
            <i aria-hidden="true"><svg class="speech-miner-excavator is-lift" viewBox="0 0 32 32"><path d="M5 18h14l-2.5-7H9v7M10 11V7h4.5l2 4M17 12l5-6 3 2-3 7M22 15l4-2 2.5 4-5.5 2zM5 21h16a2.5 2.5 0 0 1 0 5H5a2.5 2.5 0 0 1 0-5Z"/><path class="miner-accent" d="m3 9 2-2m-2-2 1-1m23 5 2-1"/></svg></i>
            <span class="speech-miner-entry-copy"><strong>{{ isMiningModeActive('new_selling_point') ? '保存退出' : '挖掘卖点' }}</strong><small>{{ isMiningModeActive('new_selling_point') ? '保存本轮真实信息' : '从内容中提炼亮点' }}</small></span>
          </button>
          <button type="button" class="speech-miner-fact-gaps" :class="{ 'is-active-mode': isMiningModeActive('fact_gaps') }" :aria-label="isMiningModeActive('fact_gaps') ? '保存退出：挖掘内容' : '挖掘内容'" :aria-pressed="isMiningModeActive('fact_gaps')" :disabled="busy || applying || !planId || (started && miningMode !== 'fact_gaps')" @click="handleMiningModeClick('fact_gaps')">
            <i aria-hidden="true"><svg class="speech-miner-excavator is-dig" viewBox="0 0 32 32"><path d="M4 18h15l-2.5-7H9v7M10 11V7h4.5l2 4M17 12l5 2 3 7M24.5 20l5 2-2 4-5.5-2zM4 21h16a2.5 2.5 0 0 1 0 5H4a2.5 2.5 0 0 1 0-5Z"/><path class="miner-accent" d="M19 27c2-1 4-1 6 0m3 0h2"/></svg></i>
            <span class="speech-miner-entry-copy"><strong>{{ isMiningModeActive('fact_gaps') ? '保存退出' : '挖掘内容' }}</strong><small>{{ isMiningModeActive('fact_gaps') ? '保存本轮真实信息' : '补充真实信息' }}</small></span>
          </button>
          <button type="button" class="speech-miner-audience-reverse" :class="{ 'is-active-mode': isMiningModeActive('audience_reverse') }" :aria-label="isMiningModeActive('audience_reverse') ? '保存退出：人群反推' : '按目标人群反推卖点'" :aria-pressed="isMiningModeActive('audience_reverse')" :disabled="busy || applying || !planId || (started && miningMode !== 'audience_reverse')" @click="handleMiningModeClick('audience_reverse')">
            <i aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="8" cy="8" r="3"/><circle cx="17" cy="9" r="2.5"/><path d="M2.5 20c.5-4.5 2.3-6.5 5.5-6.5s5 2 5.5 6.5M13 20c.3-3.4 1.7-5 4.2-5 2.4 0 3.8 1.6 4.3 5" fill="none" stroke="currentColor" stroke-width="1.8"/></svg></i>
            <span class="speech-miner-entry-copy"><strong>{{ isMiningModeActive('audience_reverse') ? '保存退出' : '人群反推' }}</strong><small>{{ isMiningModeActive('audience_reverse') ? '保存本轮真实信息' : '从人群推沟通内容' }}</small></span>
          </button>
          <button type="button" class="speech-miner-save" :disabled="busy || applying || !draftFacts.length" @click="openReview">
            <i aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m5 12 4.5 4.5L19 7" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round"/></svg></i>
            <span class="speech-miner-entry-copy"><strong>{{ applying ? '保存中…' : factsSaved ? '重试保存' : '核对并保存' }}</strong><small>确认后保存挖掘数据</small></span>
          </button>
        </div>
        <div v-if="readyToConfirm && draftFacts.length" class="speech-miner-review-hint" role="status">提问已结束，请核对本轮事实后确认生成。新内容尚未发布。</div>
        <div v-if="saveStage" class="speech-miner-save-progress" role="progressbar" aria-label="挖掘数据保存进度" :aria-valuenow="saveProgress" aria-valuemin="0" aria-valuemax="100"><i :style="{ width: saveProgress + '%' }"></i><span>{{ saveStage }} · {{ saveProgress }}%</span></div>
        <form class="speech-miner-composer" @submit.prevent="sendMessage()">
          <div class="speech-miner-textarea-wrap"><textarea v-model="input" rows="3" maxlength="3000" :disabled="busy || applying || factsSaved || !planId" placeholder="直接输入真实情况，复杂一点也没关系…" @keydown="handleComposerKeydown" @compositionstart="composingMessage = true" @compositionend="composingMessage = false"></textarea><small>{{ input.length }}/3000</small></div>
          <div class="speech-miner-send-actions"><label class="speech-miner-enter-send" title="勾选后回车发送，Shift+回车仍可换行；不勾选时回车换行"><input v-model="enterToSend" type="checkbox" />回车即发送</label><button class="primary" type="submit" :disabled="busy || applying || factsSaved || !input.trim()"><i aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m2 10 20-8-8 20-4-8-8-4z" fill="currentColor"/><path d="m10 14 7-7" stroke="#7760ef" stroke-width="1.8" stroke-linecap="round"/></svg></i>发送</button></div>
        </form>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="reviewOpen" class="speech-miner-modal-backdrop" @click.self="!applying && (reviewOpen = false)">
        <section class="speech-miner-confirm-modal" role="dialog" aria-modal="true" aria-label="核对事实依据" @keydown.esc="!applying && (reviewOpen = false)">
          <header><h3>核对事实依据</h3><button type="button" :disabled="applying" aria-label="关闭事实核对" @click="reviewOpen = false">×</button></header>
          <p>请确认以下内容真实、范围准确。确认后会保存事实依据和挖掘结果，不重新分析主播，也不会自动发布。</p>
          <article v-for="(fact, index) in draftFacts" :key="index"><label>事实名称<input v-model="fact.key" maxlength="255" :disabled="applying || factsSaved" /></label><label>事实内容<textarea v-model="fact.value" rows="3" maxlength="4000" :disabled="applying || factsSaved" /></label><small>用户原话：{{ fact.source_quote }}</small><button v-if="!factsSaved" type="button" :disabled="applying" @click="removeDraftFact(index)">移除</button></article>
          <p v-if="error" class="speech-miner-error" role="alert">{{ error }}</p><p v-if="notice" class="speech-miner-notice">{{ notice }}</p>
          <div v-if="applying" class="speech-miner-save-progress" role="progressbar" aria-label="确认保存进度" :aria-valuenow="saveProgress" aria-valuemin="0" aria-valuemax="100"><i :style="{ width: saveProgress + '%' }"></i><span>{{ saveStage }} · {{ saveProgress }}%</span></div>
          <footer><button type="button" :disabled="applying" @click="reviewOpen = false">返回继续核对</button><button type="button" :disabled="busy || applying || !draftFacts.length" @click="finishMining">{{ applying ? '正在保存…' : factsSaved ? '重试保存' : '确认并保存挖掘数据' }}</button></footer>
        </section>
      </div>
      <div v-if="selectedPoint" class="speech-miner-modal-backdrop" @click.self="closePointAngles">
        <section class="speech-miner-angle-modal" role="dialog" aria-modal="true" aria-labelledby="speech-miner-angle-modal-title">
          <header>
            <div>
              <span>{{ String(selectedPointNumber).padStart(2, '0') }}</span>
              <div><small>卖点全部内容</small><h3 id="speech-miner-angle-modal-title"><em>卖点：</em>{{ selectedPoint.卖点名 }}</h3></div>
            </div>
            <button type="button" aria-label="关闭" @click="closePointAngles">×</button>
          </header>
          <p class="speech-miner-modal-audience"><i aria-hidden="true"><svg viewBox="0 0 24 24"><circle cx="12" cy="8" r="4"/><path d="M4.5 20c.6-4.2 3-6.4 7.5-6.4s6.9 2.2 7.5 6.4"/></svg></i><b>人群：</b>{{ selectedPoint.目标人群 || selectedPoint.痛点 || '适合关注该卖点的消费者' }}</p>
          <div class="speech-miner-modal-angles">
            <article v-for="(angle, angleIndex) in selectedPoint.角度 || []" :key="angle" :class="'angle-tone-' + ((angleIndex % 6) + 1)"><span>{{ angleIndex + 1 }}</span><p>{{ angle }}</p></article>
          </div>
          <footer><span>共 {{ selectedPoint.角度?.length || 0 }} 条内容</span><button type="button" @click="closePointAngles">关闭</button></footer>
        </section>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.speech-miner {
  display: grid;
  gap: 20px;
  color: #172544;
  font-size: 16px;
}

.speech-miner-head {
  position: relative;
  display: flex;
  min-height: 126px;
  align-items: center;
  justify-content: space-between;
  gap: 28px;
  overflow: hidden;
  padding: 22px 30px;
  border: 1px solid #dce5fb;
  border-radius: 22px;
  background:
    radial-gradient(circle at 68% 120%, rgba(150, 176, 255, .18) 0 24%, transparent 25%),
    radial-gradient(circle at 50% -50%, rgba(255, 255, 255, .96) 0 34%, transparent 35%),
    linear-gradient(120deg, #edf3ff 0%, #f9fbff 48%, #eef3ff 100%);
  box-shadow: 0 10px 30px rgba(77, 103, 178, .08);
}

.speech-miner-brand { display: flex; align-items: center; gap: 20px; min-width: 0; }
.speech-miner-brand > div:last-child { display: grid; gap: 4px; }
.speech-miner-brand-icon {
  position: relative;
  display: grid;
  width: 92px;
  height: 78px;
  flex: 0 0 auto;
  place-items: center;
  border: 8px solid #fff;
  border-radius: 30px;
  background: linear-gradient(145deg, #222c67, #0b164a);
  color: #50e5ff;
  box-shadow: 0 10px 25px rgba(63, 100, 232, .24), inset 0 0 18px rgba(49, 210, 255, .34);
  font-size: 30px;
}
.speech-miner-brand-icon::before,
.speech-miner-brand-icon::after { content: ''; position: absolute; top: 32px; width: 18px; height: 18px; border-radius: 50%; background: #74a8ff; }
.speech-miner-brand-icon::before { left: 16px; }
.speech-miner-brand-icon::after { right: 16px; }
.speech-miner-brand-icon span { position: absolute; top: -24px; right: -22px; padding: 4px 8px; border: 4px solid #eef3ff; border-radius: 12px; background: #6d5cf5; color: #fff; font-size: 15px; letter-spacing: 2px; line-height: 1; }
.speech-miner-brand-icon b { transform: translateY(30px); color: #5daaff; font-size: 17px; }
.speech-miner-brand h3 { margin: 0; color: #10203f; font-size: 32px; line-height: 1.15; letter-spacing: -.02em; }
.speech-miner-brand span { color: #7f91bb; font-size: 12px; font-weight: 900; letter-spacing: .14em; }
.speech-miner-brand p { margin: 2px 0 0; color: #647596; font-size: 15px; line-height: 1.55; }

.speech-miner-head-actions { position: relative; display: flex; flex: 0 0 auto; align-items: center; gap: 26px; }
.speech-miner-head-actions > span { transform: rotate(-5deg); color: #6381ef; font-size: 18px; font-weight: 800; letter-spacing: .08em; }
.speech-miner-chat-start{display:flex;gap:12px;flex-wrap:wrap}
.speech-miner-chat-start > button {
  position: relative;
  display: flex;
  align-items: center;
  gap: 12px;
  width: 180px;
  min-height: 64px;
  overflow: hidden;
  box-sizing: border-box;
  border: 1px solid #83c7ff;
  border-radius: 16px;
  padding: 10px 14px;
  background: linear-gradient(125deg, #66b7ff, #3783f7);
  box-shadow: 0 5px 13px rgba(75, 126, 224, .14), inset 0 1px 0 rgba(255,255,255,.6);
  color: #fff;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.speech-miner-chat-start > button::after { content: ''; position: absolute; top: -15px; right: -20px; width: 65px; height: 110px; transform: rotate(30deg); border-radius: 45%; background: rgba(255,255,255,.12); pointer-events: none; }
.speech-miner-chat-start > button.speech-miner-new-point { border-color: #ece5ff; background: linear-gradient(125deg,#f2edff,#dcd0ff); color: #5537b8; box-shadow: 0 5px 13px rgba(114,91,198,.12), inset 0 1px 0 #fff; }
.speech-miner-chat-start > button.speech-miner-audience-reverse { border-color:#d9eee9; background:linear-gradient(125deg,#eafaf4,#ccefe3); color:#23775e; box-shadow:0 5px 13px rgba(45,143,111,.12),inset 0 1px 0 #fff; }
.speech-miner-chat-start > button.speech-miner-save { border-color:#f0e1bd; background:linear-gradient(125deg,#fff9e9,#f7e6b8); color:#89651c; box-shadow:0 5px 13px rgba(168,126,36,.12),inset 0 1px 0 #fff; }
.speech-miner-chat-start > button i { display: grid; position: relative; z-index: 1; width: 38px; height: 38px; flex: 0 0 38px; place-items: center; border-radius: 50%; background: rgba(255,255,255,.25); color: #fff; box-shadow: inset 0 1px 0 rgba(255,255,255,.4); font-style: normal; }
.speech-miner-chat-start > button.speech-miner-new-point i { background: linear-gradient(135deg,#b397ff,#9b77ef); box-shadow: 0 3px 8px rgba(132,97,217,.18), inset 0 1px 0 rgba(255,255,255,.4); }
.speech-miner-chat-start > button.speech-miner-audience-reverse i{background:linear-gradient(135deg,#54c69e,#2c9c78)}
.speech-miner-chat-start > button.speech-miner-save i{background:linear-gradient(135deg,#e5b54b,#c38a1d)}
.speech-miner-chat-start > button i svg { width: 23px; height: 23px; fill: currentColor; }
.speech-miner-chat-start .speech-miner-excavator { width: 27px; height: 27px; overflow: visible; fill: none; stroke: currentColor; stroke-width: 1.75; stroke-linecap: round; stroke-linejoin: round; }
.speech-miner-chat-start .speech-miner-excavator .miner-accent { stroke-width: 1.45; opacity: .72; }
.speech-miner-chat-start .speech-miner-excavator.is-lift { transform: translateY(-1px); }
.speech-miner-chat-start .speech-miner-excavator.is-dig { transform: translate(1px,1px); }
.speech-miner-entry-copy { position: relative; z-index: 1; display: grid; gap: 4px; min-width: 0; }
.speech-miner-entry-copy strong { font-size: 16px; font-weight: 850; line-height: 1.3; white-space: nowrap; }
.speech-miner-entry-copy small { color: inherit; opacity: .72; font-size: 11px; font-weight: 500; line-height: 1.3; white-space: nowrap; }
.speech-miner-chat-start > button:disabled { opacity: .55; cursor: not-allowed; }
.speech-miner-chat-start > button.is-active-mode { outline: 2px solid currentColor; outline-offset: 2px; }

.speech-miner-grid {
  --miner-left-width: 40%;
  display: grid;
  grid-template-columns: minmax(320px, var(--miner-left-width)) 18px minmax(0, 1fr);
  min-height: 720px;
  align-items: start;
}
.speech-miner-results,
.speech-miner-chat { min-width: 0; border: 1px solid #dfe6f4; border-radius: 22px; background: rgba(255,255,255,.96); overflow: hidden; box-shadow: 0 12px 30px rgba(50, 73, 135, .06); }
.speech-miner-results { display: flex; flex-direction: column; gap: 18px; padding: 22px; container-name: speech-results; container-type: inline-size; }
.speech-miner-results > header,
.speech-miner-chat > header,
.speech-miner-drafts > header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.speech-miner-results > header { flex-wrap: wrap; }
.speech-miner-results-title { display: flex; align-items: center; gap: 14px; min-width: 0; }
.speech-miner-results-toolbar { display: flex; align-items: center; gap: 10px; width: 100%; flex-wrap: wrap; }
.speech-miner-pool-select { flex: 1 1 180px; min-width: 0; max-width: 330px; }
.speech-miner-pool-select select { width: 100%; min-height: 40px; padding: 8px 30px 8px 12px; border: 1px solid #ccd7f2; border-radius: 10px; background: #f7f9ff; color: #425b96; font: inherit; font-size: 14px; font-weight: 750; text-overflow: ellipsis; }
.speech-miner-pool-select select:focus-visible { outline: 3px solid #a4b8ff; outline-offset: 2px; }
.speech-miner-pool-select select:disabled { opacity: .65; }
.speech-miner-pool-select .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip: rect(0,0,0,0); white-space: nowrap; }
.speech-miner-pool-notice { margin: 0; color: #7b83a1; font-size: 13px; line-height: 1.6; }
.speech-miner-point-edit:disabled { opacity: .4; cursor: default; }
.speech-miner-results > header strong { color: #152747; font-size: 22px; white-space: nowrap; }
.speech-miner-results > header small { padding: 7px 11px; border-radius: 999px; background: #f0f4ff; color: #6c7da7; font-size: 14px; white-space: nowrap; }
.speech-miner-results-actions { display: flex!important; align-items: center; justify-content: flex-end; gap: 8px!important; margin-left: auto; }
.speech-miner-results-actions button { min-height: 34px; border: 1px solid #ced8ee; border-radius: 10px; padding: 6px 12px; background: #fff; color: #586c9e; font: inherit; font-size: 13px; font-weight: 800; cursor: pointer; }
.speech-miner-results-actions button:last-child { border-color: #ddd4f5; background: #f8f5ff; color: #715ba8; }
.speech-miner-results-actions button:disabled { cursor: default; opacity: .42; }
.speech-miner-results-toolbar em { display: flex; align-items: center; gap: 6px; padding: 8px 11px; border: 1px solid #ffe0aa; border-radius: 999px; background: #fff8eb; color: #df8615; font-size: 13px; font-style: normal; white-space: nowrap; }
.speech-miner-results-toolbar em i { font-size: 18px; font-style: normal; }
.speech-miner-empty { display: grid; min-height: 300px; place-items: center; padding: 28px; border: 1px dashed #d8e1f2; border-radius: 16px; background: #f7f9fd; color: #748198; font-size: 16px; line-height: 1.7; text-align: center; }

.speech-miner-point-list { display: grid; align-content: start; gap: 13px; min-height: 360px; overflow: auto; padding: 1px 8px 8px 1px; scrollbar-color: #aeb6c4 transparent; scrollbar-width: thin; }
.speech-miner-point-list article { display: grid; grid-template-columns: 66px minmax(0,1fr) 42px; align-items: stretch; gap: 16px; padding: 18px; border: 1px solid #dfe6f3; border-radius: 20px; background: linear-gradient(120deg,#fbfdff,#fff); transition: border-color .18s, box-shadow .18s, transform .18s; }
.speech-miner-point-list article:first-child { border-color: #c5d4ff; box-shadow: 0 8px 20px rgba(80, 111, 205, .08); }
.speech-miner-point-list article:hover { transform: translateY(-1px); border-color: #aebfff; box-shadow: 0 9px 22px rgba(69, 95, 172, .1); }
.speech-miner-point-list article.is-collapsed { align-items: stretch; }
.speech-miner-point-list article.is-editing { border-color: #91a9f6; background: var(--miner-card-tone, #fbfcff); box-shadow: 0 12px 28px rgba(69, 95, 172, .13); transform: none; }
.speech-miner-point-list article.is-risk-located { outline: 2px solid #df6379; outline-offset: -2px; }
.speech-miner-point-content li.is-risk-located { outline: 3px solid #df6379; outline-offset: 2px; scroll-margin: 20px; }
.speech-miner-point-overview .is-risk-located { outline: 3px solid #df6379; outline-offset: 3px; border-radius: 6px; }
.speech-miner-point-index { display: grid; min-height: 100%; place-items: start center; padding-top: 18px; border-radius: 17px; background: linear-gradient(180deg,#edf3ff,#f8faff); color: #4e7cf1; font-size: 17px; font-weight: 950; box-sizing: border-box; }
.speech-miner-point-content { min-width: 0; }
.speech-miner-point-overview { display: grid; grid-template-columns: minmax(0,1.25fr) minmax(150px,.85fr); align-items: center; gap: 18px; padding: 2px 0 15px; border-radius: 16px; background: linear-gradient(120deg,#f5f7ff,#f9fbff); }
.speech-miner-selling-summary { display: grid; grid-template-columns: 56px minmax(0,1fr); align-items: center; gap: 13px; min-width: 0; padding: 13px 0 13px 14px; }
.speech-miner-selling-summary > i { display: grid; width: 54px; height: 54px; place-items: center; border-radius: 15px; background: linear-gradient(145deg,#ebe5ff,#f5efff); color: #7258df; box-shadow: inset 0 1px 0 rgba(255,255,255,.85); }
.speech-miner-selling-summary svg { width: 31px; height: 31px; fill: none; stroke: currentColor; stroke-linecap: round; stroke-linejoin: round; stroke-width: 1.8; }
.speech-miner-selling-summary strong { display: block; overflow: hidden; color: #172c51; font-size: 20px; line-height: 1.35; text-overflow: ellipsis; white-space: nowrap; }
.speech-miner-selling-summary p { margin: 7px 0 0; overflow: hidden; color: #6f7f9e; font-size: 13px; line-height: 1.45; text-overflow: ellipsis; white-space: nowrap; }
.speech-miner-audience { display: grid; grid-template-columns: 48px minmax(0,1fr); align-items: center; gap: 12px; min-width: 0; padding: 13px 14px 13px 18px; border-left: 1px solid #dbe3f5; color: #52678f; }
.speech-miner-audience > i { display: grid; width: 46px; height: 46px; place-items: center; border-radius: 50%; background: linear-gradient(145deg,#eee8ff,#e6e5ff); color: #7258df; }
.speech-miner-audience svg { width: 25px; height: 25px; fill: none; stroke: currentColor; stroke-linecap: round; stroke-linejoin: round; stroke-width: 1.8; }
.speech-miner-audience > div { min-width: 0; }
.speech-miner-audience b { display: block; color: #5b38bd; font-size: 15px; }
.speech-miner-audience span { display: block; margin-top: 6px; overflow: hidden; color: #66789b; font-size: 13px; line-height: 1.45; text-overflow: ellipsis; white-space: nowrap; }
.speech-miner-inline-fields { display: grid; min-width: 0; gap: 8px; }
.speech-miner-inline-fields label { display: grid; min-width: 0; gap: 4px; color: #65769a; font-size: 11px; font-weight: 850; }
.speech-miner-inline-fields input { width: 100%; min-width: 0; border: 1px solid #c9d5ef; border-radius: 9px; padding: 8px 10px; outline: 0; background: #fff; color: #1d3156; font: inherit; font-size: 13px; box-sizing: border-box; }
.speech-miner-inline-fields input:focus { border-color: #718df0; box-shadow: 0 0 0 3px rgba(91,119,226,.11); }
.speech-miner-inline-fields input:disabled { background: #f4f6fa; color: #8b96aa; }
.speech-miner-angle-area { padding-top: 15px; border-top: 1px solid #e0e6f3; }
.speech-miner-point-content ul { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 7px; margin: 0; padding: 0; list-style: none; }
.speech-miner-point-content li { display: inline-flex; width: fit-content; max-width: 100%; align-items: center; padding: 8px 13px; border: 1px solid; border-radius: 999px; font-size: 14px; line-height: 1.35; white-space: normal; word-break: break-word; }
.speech-miner-point-content li.angle-tone-1,
.speech-miner-modal-angles article.angle-tone-1 { border-color: #bed0fa; background: #f1f5ff; color: #4867af; }
.speech-miner-point-content li.angle-tone-2,
.speech-miner-modal-angles article.angle-tone-2 { border-color: #bce7d0; background: #eefaf4; color: #2f7b55; }
.speech-miner-point-content li.angle-tone-3,
.speech-miner-modal-angles article.angle-tone-3 { border-color: #f3d4a7; background: #fff8e9; color: #996727; }
.speech-miner-point-content li.angle-tone-4,
.speech-miner-modal-angles article.angle-tone-4 { border-color: #dcc8fa; background: #f8f2ff; color: #7852a8; }
.speech-miner-point-content li.angle-tone-5,
.speech-miner-modal-angles article.angle-tone-5 { border-color: #f3c5d1; background: #fff2f5; color: #9c5265; }
.speech-miner-point-content li.angle-tone-6,
.speech-miner-modal-angles article.angle-tone-6 { border-color: #b9e2eb; background: #effafd; color: #317887; }
.speech-miner-point-content li.has-risk { gap: 7px; border-color: #e7b0b9; background: #fff3f5; color: #9d3f4e; }
.speech-miner-angle-risk { padding: 2px 6px; border-radius: 999px; background: #ffdce2; color: #b3354b; font-size: 10px; font-weight: 850; white-space: nowrap; }
.speech-miner-point-content li > button.speech-miner-angle-confirm { border: 0; border-radius: 999px; padding: 4px 8px; background: #627dec; color: #fff; font: inherit; font-size: 10px; font-weight: 850; cursor: pointer; white-space: nowrap; }
.speech-miner-point-content li > button.speech-miner-angle-confirm:disabled { cursor: not-allowed; opacity: .5; }
.speech-miner-angle-editor-list li { gap: 7px; padding-right: 8px; }
.speech-miner-angle-editor-list li > span { min-width: 0; }
.speech-miner-angle-editor-list li > button { display: grid; min-width: 26px; height: 26px; place-items: center; border: 0; border-radius: 999px; padding: 0 7px; background: rgba(255,255,255,.72); color: inherit; font: inherit; font-size: 12px; font-weight: 850; cursor: pointer; }
.speech-miner-angle-editor-list li > button:hover { background: #fff; box-shadow: 0 2px 7px rgba(48,64,105,.13); }
.speech-miner-angle-editor-list li > button.danger-icon { color: #c85460; font-size: 18px; }
.speech-miner-angle-editor-list li > button.danger { width: auto; color: #bb3949; }
.speech-miner-angle-editor-list li.is-angle-editing { width: min(100%, 620px); flex: 1 1 300px; }
.speech-miner-angle-editor-list li.is-delete-pending { border-color: #f1b8be; background: #fff5f6; color: #93434c; }
.speech-miner-angle-editor-list input { min-width: 180px; flex: 1 1 260px; border: 0; border-bottom: 1px solid currentColor; padding: 3px 4px; outline: 0; background: transparent; color: inherit; font: inherit; }
.speech-miner-angle-editor-list li.speech-miner-add-angle { border-style: dashed; border-color: #b9c7e6; background: #f8faff; }
.speech-miner-angle-editor-list li.speech-miner-add-angle > button { width: auto; background: transparent; color: #5871b7; }
.speech-miner-edit-footer { display: grid; gap: 9px; margin-top: 14px; padding-top: 13px; border-top: 1px dashed #d7dfef; }
.speech-miner-edit-footer > p { margin: 0; padding: 8px 10px; border-radius: 9px; background: #fff1f2; color: #b44350; font-size: 12px; line-height: 1.45; }
.speech-miner-edit-actions { display: grid; grid-template-columns: auto 1fr auto auto; align-items: center; gap: 8px; }
.speech-miner-edit-actions button,
.speech-miner-delete-confirm button { border: 1px solid #ced8ec; border-radius: 9px; padding: 7px 11px; background: #fff; color: #566b99; font: inherit; font-size: 12px; font-weight: 850; cursor: pointer; }
.speech-miner-edit-actions button.primary { border-color: #627dec; background: #627dec; color: #fff; }
.speech-miner-edit-actions button.danger-link { border-color: transparent; padding-left: 0; background: transparent; color: #c4515d; }
.speech-miner-edit-actions button:disabled,
.speech-miner-delete-confirm button:disabled { cursor: not-allowed; opacity: .45; }
.speech-miner-delete-confirm { display: flex; align-items: center; gap: 8px; padding: 10px 12px; border: 1px solid #f2c5ca; border-radius: 11px; background: #fff6f7; color: #8f414a; font-size: 12px; }
.speech-miner-delete-confirm span { min-width: 0; flex: 1; line-height: 1.5; }
.speech-miner-delete-confirm button.danger { border-color: #d55e69; background: #d55e69; color: #fff; }
.speech-miner-angle-expand { display: flex; width: fit-content; align-items: center; gap: 6px; margin-top: 9px; border: 0; padding: 4px 2px; background: transparent; color: #6479b4; font: inherit; font-size: 12px; font-weight: 750; cursor: pointer; }
.speech-miner-angle-expand i { display: inline-block; font-size: 18px; font-style: normal; line-height: .8; transition: transform .18s; }
.speech-miner-angle-expand i.expanded { transform: rotate(180deg); }
.speech-miner-point-actions { display: flex; min-height: 100%; flex-direction: column; align-items: center; justify-content: space-between; gap: 18px; padding: 1px 0 8px; }
.speech-miner-point-edit { display: grid; width: 36px; height: 34px; place-items: center; border: 1px solid #d9e1f2; border-radius: 11px; background: #fff; color: #5f75bd; cursor: pointer; transition: border-color .18s, background .18s, transform .18s; }
.speech-miner-point-edit svg { width: 18px; height: 18px; fill: none; stroke: currentColor; stroke-linecap: round; stroke-linejoin: round; stroke-width: 1.8; }
.speech-miner-point-edit:hover { transform: translateY(-1px); border-color: #9eb1ed; background: #f4f7ff; }
.speech-miner-point-more { display: flex; width: 36px; height: 30px; align-items: center; justify-content: center; gap: 3px; border: 0; border-radius: 999px; background: #f2f5fc; cursor: pointer; }
.speech-miner-point-more i { width: 4px; height: 4px; border-radius: 50%; background: #4867af; }
.speech-miner-point-more.collapsed { background: #e7edff; box-shadow: 0 0 0 2px rgba(90,111,205,.08); }
.speech-miner-point-more:hover { background: #e8edfb; }
.speech-miner-point-chevron { display: grid; width: 40px; height: 40px; place-items: center; border: 1px solid #d6e0f2; border-radius: 50%; background: #fff; color: #7391cf; font: inherit; font-size: 30px; line-height: 1; cursor: pointer; transition: border-color .18s, background .18s, transform .18s; }
.speech-miner-point-chevron:hover { transform: translateX(2px); border-color: #9cb3eb; background: #f4f7ff; }
.speech-miner-list-resizer { display: grid; min-height: 24px; grid-template-columns: 1fr auto 1fr; align-items: center; gap: 10px; outline: none; color: #93a0b7; cursor: row-resize; touch-action: none; user-select: none; }
.speech-miner-list-resizer::before,
.speech-miner-list-resizer::after { content: ''; height: 1px; background: linear-gradient(90deg,transparent,#d5deee); }
.speech-miner-list-resizer::after { background: linear-gradient(90deg,#d5deee,transparent); }
.speech-miner-list-resizer > span { display: none; }
.speech-miner-list-resizer small { padding: 4px 10px; border-radius: 999px; background: #f4f7fc; font-size: 11px; }
.speech-miner-list-resizer:hover small,
.speech-miner-list-resizer:focus-visible small { background: #eaf0ff; color: #6379b4; }

.speech-miner-drafts { display: grid; gap: 9px; padding-top: 15px; border-top: 1px solid #e4e9f2; }
.speech-miner-drafts > header strong { font-size: 16px; }
.speech-miner-drafts > header span { color: #8290a4; font-size: 13px; }
.speech-miner-drafts article { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; padding: 12px 14px; border-radius: 12px; background: #eff8f1; }
.speech-miner-drafts article strong { color: #2b6843; font-size: 14px; }
.speech-miner-drafts article p { margin: 4px 0 0; color: #3e5f4b; font-size: 14px; line-height: 1.5; }
.speech-miner-drafts article button { border: 0; background: transparent; color: #84938a; font-size: 13px; cursor: pointer; }

.speech-miner-resizer { position: relative; display: grid; width: 18px; align-self: stretch; place-items: center; outline: none; cursor: col-resize; touch-action: none; user-select: none; }
.speech-miner-resizer::before { content: ''; position: absolute; inset: 0 7px; border-radius: 999px; background: transparent; transition: background .18s, box-shadow .18s; }
.speech-miner-resizer:hover::before,
.speech-miner-resizer:focus-visible::before,
.speech-miner-grid.is-resizing .speech-miner-resizer::before { background: #8fa7f5; box-shadow: 0 0 0 4px rgba(105, 132, 231, .12); }
.speech-miner-resizer > span { position: relative; display: grid; z-index: 1; gap: 4px; padding: 9px 5px; border: 1px solid #d8e1f3; border-radius: 999px; background: #fff; box-shadow: 0 4px 12px rgba(57, 75, 132, .12); }
.speech-miner-resizer > span i { width: 3px; height: 3px; border-radius: 50%; background: #7891d8; }

.speech-miner-chat { display: grid; height: fit-content; align-self: start; align-content: start; grid-template-rows: auto auto auto auto auto; }
.speech-miner-chat > header { padding: 20px 23px; border-bottom: 1px solid #e4e9f2; }
.speech-miner-chat > header > div { display: flex; align-items: center; gap: 11px; }
.speech-miner-chat > header strong { color: #182b4c; font-size: 21px; }
.speech-miner-chat > header i { width: 11px; height: 11px; border-radius: 50%; background: #32cf7b; box-shadow: 0 0 0 5px #e2f9ec; }
.speech-miner-chat > header span { color: #8290ad; font-size: 14px; }
.speech-miner-messages { display: flex; min-height: 280px; max-height: 900px; box-sizing: border-box; flex-direction: column; gap: 14px; overflow: auto; padding: 22px; background: linear-gradient(180deg,#fafcff,#f7f9fd); }
.speech-miner-chat-resizer { display: flex; min-height: 28px; align-items: center; justify-content: center; gap: 9px; border-top: 1px solid #e4e9f2; background: #f9fbff; color: #8392af; cursor: row-resize; touch-action: none; user-select: none; }
.speech-miner-chat-resizer > span { width: 36px; height: 5px; border-radius: 999px; background: #bcc9e4; }
.speech-miner-chat-resizer small { font-size: 11px; }
.speech-miner-chat-resizer:hover, .speech-miner-chat-resizer:focus-visible { background: #edf2ff; color: #5874bc; outline: 2px solid #a9bdf1; outline-offset: -2px; }
.speech-miner-chat-resizer:hover > span, .speech-miner-chat-resizer:focus-visible > span { background: #7895dd; }
.speech-miner-welcome-row { display: flex; align-items: flex-start; gap: 16px; }
.speech-miner-bot { position: relative; display: grid; width: 74px; height: 62px; flex: 0 0 auto; place-items: center; border: 7px solid #fff; border-radius: 24px; background: #132459; box-shadow: 0 8px 20px rgba(61, 94, 200, .16); }
.speech-miner-bot::before { content: ''; position: absolute; top: -14px; width: 4px; height: 12px; border-radius: 4px; background: #86afff; }
.speech-miner-bot span { position: absolute; top: 22px; width: 9px; height: 14px; border-radius: 8px; background: #46dfff; box-shadow: 28px 0 #46dfff; transform: translateX(-14px); }
.speech-miner-bot i { position: absolute; bottom: -17px; width: 13px; height: 18px; border-radius: 10px; background: #80aaff; }
.speech-miner-bot i:first-of-type { left: 14px; transform: rotate(16deg); }
.speech-miner-bot i:last-of-type { right: 14px; transform: rotate(-16deg); }
.speech-miner-welcome { max-width: 640px; padding: 18px 20px; border-radius: 18px; background: linear-gradient(135deg,#eef3ff,#f3f6fd); color: #455b8a; }
.speech-miner-welcome strong { color: #21375e; font-size: 19px; }
.speech-miner-welcome p { margin: 7px 0 0; font-size: 15px; line-height: 1.75; }
.speech-miner-messages article { display: flex; }
.speech-miner-messages article p { max-width: 88%; margin: 0; overflow-wrap: anywhere; padding: 13px 16px; border-radius: 15px; font-size: 16px; line-height: 1.7; white-space: pre-wrap; }
.speech-miner-messages .is-agent p { border: 1px solid #e0e6f0; border-bottom-left-radius: 5px; background: #fff; color: #33425d; }
.speech-miner-messages .is-user { justify-content: flex-end; }
.speech-miner-messages .is-user p { border-bottom-right-radius: 5px; background: linear-gradient(135deg,#667be8,#536fdf); color: #fff; }
.speech-miner-typing { display: flex; align-items: center; gap: 9px; width: fit-content; padding: 10px 13px; border: 1px solid #dde5f3; border-radius: 13px; background: #fff; color: #6c7b96; font-size: 14px; }
.speech-miner-typing i { width: 9px; height: 9px; border: 2px solid #b2bfda; border-top-color: #617ae0; border-radius: 50%; animation: speech-miner-spin .8s linear infinite; }
.speech-miner-typing.is-saving { border-color: #cce8d8; background: #f2fbf6; color: #347450; }
.speech-miner-typing.is-saving i { border-color: #b7dbc5; border-top-color: #35a766; }
@keyframes speech-miner-spin { to { transform: rotate(360deg); } }

.speech-miner-options { display: grid; gap: 10px; padding: 12px 22px 2px; }
.speech-miner-multi-hint { display: flex; align-items: center; gap: 10px; color: #73809a; font-size: 13px; }
.speech-miner-multi-hint strong { padding: 4px 9px; border-radius: 999px; background: #eeeaff; color: #6658d9; font-size: 13px; }
.speech-miner-option { display: grid; grid-template-columns: 36px minmax(0,1fr) auto; align-items: center; gap: 12px; width: 100%; border: 1px solid #dce4f2; border-radius: 16px; padding: 12px 15px; background: #fff; color: #30486f; font: inherit; font-size: 15px; text-align: left; cursor: pointer; transition: border-color .18s, background .18s, transform .18s, box-shadow .18s; }
.speech-miner-option:hover { transform: translateY(-1px); border-color: #9fb4ef; background: #f8faff; }
.speech-miner-option > i { display: grid; width: 34px; height: 34px; place-items: center; border-radius: 50%; background: #f0edff; color: #7265df; font-size: 13px; font-style: normal; font-weight: 900; }
.speech-miner-options.is-multiple .speech-miner-option:not(.is-control) > i { border: 2px solid #c8c0fb; border-radius: 10px; background: #fff; box-sizing: border-box; }
.speech-miner-option > b { min-width: 25px; color: #91a1bf; font-size: 25px; font-weight: 400; text-align: right; }
.speech-miner-options.is-multiple .speech-miner-option:not(.is-control) > b { color: #7e8ba4; font-size: 13px; font-weight: 700; }
.speech-miner-option.is-selected { border-color: #7667e8; background: #f4f2ff; box-shadow: 0 0 0 3px rgba(111,91,226,.09); }
.speech-miner-options.is-multiple .speech-miner-option.is-selected > i { border-color: #7667e8; background: #7667e8; color: #fff; }
.speech-miner-option.is-selected > b { color: #6657d8 !important; }
.speech-miner-option.is-control { background: #fafbfe; color: #66738a; }
.speech-miner-option:disabled { opacity: .55; cursor: not-allowed; }
.speech-miner-multi-submit { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding-top: 3px; }
.speech-miner-multi-submit > span { color: #7b879d; font-size: 13px; }
.speech-miner-multi-submit > button { border: 0; border-radius: 11px; padding: 10px 18px; background: linear-gradient(135deg,#7664f4,#5a56ec); color: #fff; font: inherit; font-size: 14px; font-weight: 800; cursor: pointer; }
.speech-miner-multi-submit > button:disabled { opacity: .45; cursor: not-allowed; }

.speech-miner-chat-start { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); justify-content:flex-start; padding:14px 22px 0; border-top:1px solid #e7ecf4; background:#fff; }
.speech-miner-chat-start > button { width:auto; min-width:0; }

.speech-miner-composer { display: grid; gap: 13px; padding: 14px 22px 20px; }
.speech-miner-textarea-wrap { position: relative; }
.speech-miner-composer textarea { width: 100%; min-height: 118px; resize: vertical; border: 1px solid #becff1; border-radius: 16px; padding: 16px 18px 34px; background: #fff; color: #243756; font: inherit; font-size: 16px; line-height: 1.6; box-sizing: border-box; outline: none; }
.speech-miner-composer textarea:focus { border-color: #718de6; box-shadow: 0 0 0 3px rgba(91,117,222,.11); }
.speech-miner-textarea-wrap small { position: absolute; right: 16px; bottom: 12px; color: #94a1ba; font-size: 13px; }
.speech-miner-send-actions { gap:16px; flex-wrap:wrap; }
.speech-miner-enter-send { display:inline-flex; align-items:center; gap:7px; color:#71809f; font-size:14px; cursor:pointer; }
.speech-miner-enter-send input { width:16px; height:16px; margin:0; accent-color:#8061e9; cursor:pointer; }
.speech-miner-composer > div:last-child { display: flex; align-items: center; justify-content: flex-end; gap: 11px; }
.speech-miner-composer button { display: flex; align-items: center; justify-content: center; gap: 7px; min-width: 112px; height: 40px; box-sizing: border-box; border: 1px solid #dce3f2; border-radius: 12px; padding: 8px 13px; background: linear-gradient(135deg,#fff,#f9fbff); color: #586b91; font: inherit; font-size: 14px; font-weight: 800; cursor: pointer; }
.speech-miner-composer button i { font-style: normal; }
.speech-miner-composer button i svg { display: block; width: 18px; height: 18px; }
.speech-miner-composer button.primary { min-width: 112px; border-color: #9480ff; background: linear-gradient(125deg,#b99aff,#6c59f6); box-shadow: 0 5px 13px rgba(109,88,224,.16); color: #fff; }
.speech-miner-composer button:disabled { opacity: .5; cursor: not-allowed; }
.speech-miner-notice,
.speech-miner-error { width: fit-content; max-width: 88%; margin: 0; padding: 12px 14px; border-radius: 11px; font-size: 14px; line-height: 1.5; }
.speech-miner-notice { background: #edf8f1; color: #36714d; }
.speech-miner-error { background: #fff0f2; color: #a43a49; }

.speech-miner-modal-backdrop { position: fixed; z-index: 10020; inset: 0; display: grid; place-items: center; padding: 26px; background: rgba(19,31,58,.42); backdrop-filter: blur(5px); }
.speech-miner-angle-modal { display: grid; width: min(760px, calc(100vw - 40px)); max-height: min(820px, calc(100vh - 52px)); overflow: hidden; border: 1px solid #dbe4f5; border-radius: 24px; background: #fff; box-shadow: 0 28px 80px rgba(25,42,84,.28); }
.speech-miner-angle-modal > header { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 21px 23px; border-bottom: 1px solid #e7ebf4; background: linear-gradient(130deg,#f3f6ff,#fff); }
.speech-miner-angle-modal > header > div { display: flex; align-items: center; gap: 13px; min-width: 0; }
.speech-miner-angle-modal > header > div > span { display: grid; width: 46px; height: 46px; flex: 0 0 auto; place-items: center; border-radius: 14px; background: #e9f0ff; color: #5579df; font-weight: 900; }
.speech-miner-angle-modal > header small { color: #8592ad; font-size: 12px; font-weight: 800; }
.speech-miner-angle-modal > header h3 { margin: 3px 0 0; overflow: hidden; color: #1b2e50; font-size: 22px; line-height: 1.3; text-overflow: ellipsis; white-space: nowrap; }
.speech-miner-angle-modal > header h3 em { margin-right: 5px; color: #7181a3; font-size: 14px; font-style: normal; }
.speech-miner-angle-modal > header > button { display: grid; width: 38px; height: 38px; flex: 0 0 auto; place-items: center; border: 0; border-radius: 50%; background: #eef2f9; color: #61708c; font: inherit; font-size: 26px; cursor: pointer; }
.speech-miner-modal-audience { display: flex; align-items: center; gap: 7px; margin: 18px 23px 0; padding: 10px 12px; border-radius: 13px; background: #f5f0ff; color: #745aa0; font-size: 14px; line-height: 1.5; }
.speech-miner-modal-audience i { display: grid; width: 24px; height: 24px; flex: 0 0 auto; place-items: center; border-radius: 8px; background: #e7dcff; color: #775bc0; }
.speech-miner-modal-audience svg { width: 16px; height: 16px; fill: none; stroke: currentColor; stroke-linecap: round; stroke-width: 1.8; }
.speech-miner-modal-audience b { color: #66479a; }
.speech-miner-modal-angles { display: grid; gap: 10px; overflow: auto; margin: 15px 23px; padding-right: 6px; scrollbar-color: #aeb6c4 transparent; scrollbar-width: thin; }
.speech-miner-modal-angles article { display: grid; grid-template-columns: 32px minmax(0,1fr); align-items: flex-start; gap: 11px; border: 1px solid; border-radius: 15px; padding: 12px 14px; }
.speech-miner-modal-angles article > span { display: grid; width: 28px; height: 28px; place-items: center; border-radius: 9px; background: rgba(255,255,255,.72); font-size: 12px; font-weight: 900; }
.speech-miner-modal-angles article > p { margin: 2px 0 0; font-size: 15px; line-height: 1.55; word-break: break-word; }
.speech-miner-angle-modal > footer { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 16px 23px 20px; border-top: 1px solid #e9edf5; }
.speech-miner-angle-modal > footer span { color: #8793aa; font-size: 13px; }
.speech-miner-angle-modal > footer button { border: 0; border-radius: 11px; padding: 10px 24px; background: linear-gradient(135deg,#7664f4,#5a56ec); color: #fff; font: inherit; font-weight: 800; cursor: pointer; }

:global(body.speech-miner-is-resizing) { cursor: col-resize !important; user-select: none !important; }
:global(body.speech-miner-is-resizing-vertical) { cursor: row-resize !important; user-select: none !important; }
:global(body.speech-miner-is-resizing-chat) { cursor: row-resize !important; user-select: none !important; }

@container speech-results (max-width: 620px) {
  .speech-miner-point-list article { grid-template-columns: 52px minmax(0,1fr) 38px; gap: 11px; padding: 14px; }
  .speech-miner-point-overview { grid-template-columns: 1fr; gap: 0; }
  .speech-miner-audience { border-top: 1px solid #dbe3f5; border-left: 0; }
  .speech-miner-selling-summary strong { font-size: 18px; }
}

@media (max-width: 1180px) {
  .speech-miner-head-actions > span { display: none; }
  .speech-miner-grid { grid-template-columns: 1fr; gap: 16px; }
  .speech-miner-resizer { display: none; }
  .speech-miner-point-list { height: min(620px, 68vh) !important; }
  .speech-miner-list-resizer { display: none; }
  .speech-miner-chat { min-height: 0; }
}
@media (max-width: 720px) {
  .speech-miner { font-size: 15px; }
  .speech-miner-head { display: grid; padding: 20px; }
  .speech-miner-brand-icon { width: 70px; height: 60px; border-radius: 23px; }
  .speech-miner-brand h3 { font-size: 27px; }
  .speech-miner-head-actions { justify-content: flex-end; }
  .speech-miner-chat-start { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .speech-miner-chat-start > button { width:auto; min-width:0; gap:10px; padding:10px 11px; }
  .speech-miner-grid { min-height: 0; }
  .speech-miner-results { padding: 16px; }
  .speech-miner-results-title { display: grid; gap: 5px; }
  .speech-miner-point-list article { grid-template-columns: 42px minmax(0,1fr); padding: 12px; position: relative; }
  .speech-miner-point-index { min-height: 60px; padding-top: 13px; border-radius: 13px; font-size: 13px; }
  .speech-miner-point-actions { position: absolute; top: 12px; right: 12px; min-height: auto; }
  .speech-miner-point-more { display: none; }
  .speech-miner-selling-summary { grid-template-columns: 44px minmax(0,1fr); padding-right: 44px; }
  .speech-miner-selling-summary > i { width: 42px; height: 42px; border-radius: 12px; }
  .speech-miner-selling-summary svg { width: 25px; height: 25px; }
  .speech-miner-selling-summary strong { font-size: 16px; }
  .speech-miner-audience { grid-template-columns: 38px minmax(0,1fr); padding: 11px; }
  .speech-miner-audience > i { width: 36px; height: 36px; }
  .speech-miner-point-content li { padding: 6px 10px; font-size: 13px; }
  .speech-miner-point-chevron { width: 32px; height: 32px; font-size: 23px; }
  .speech-miner-angle-modal { width: calc(100vw - 24px); max-height: calc(100vh - 24px); border-radius: 18px; }
  .speech-miner-angle-modal > header { padding: 16px; }
  .speech-miner-modal-audience { margin: 14px 16px 0; }
  .speech-miner-modal-angles { margin: 13px 16px; }
  .speech-miner-angle-modal > footer { padding: 14px 16px 16px; }
  .speech-miner-welcome-row { display: grid; }
  .speech-miner-bot { width: 62px; height: 52px; }
  .speech-miner-messages { padding: 16px; }
  .speech-miner-options { padding-inline: 16px; }
  .speech-miner-composer { padding: 16px; }
}
</style>

<style scoped>
.speech-miner-review-hint{padding:12px 16px;color:#766231;background:#fff9e8;font-size:13px}
.speech-miner-save-progress{position:relative;display:grid;place-items:center;overflow:hidden;min-height:42px;margin:8px 16px;border:1px solid #dedff7;border-radius:10px;background:#f2f3fd}
.speech-miner-save-progress>i{position:absolute;inset:0 auto 0 0;background:#c9c1fb;transition:width .25s}.speech-miner-save-progress>span{position:relative;padding:8px 12px;font-size:12px;text-align:center;color:#57517d}
.speech-miner-confirm-modal{width:min(720px,95vw);max-height:88vh;overflow:auto;padding:22px;background:#fff;border-radius:20px;box-shadow:0 24px 80px #18244040;color:#334561}
.speech-miner-confirm-modal>header,.speech-miner-confirm-modal>footer{display:flex;align-items:center;justify-content:space-between;gap:10px}
.speech-miner-confirm-modal h3{margin:0}.speech-miner-confirm-modal p{font-size:13px;line-height:1.8}.speech-miner-confirm-modal article{display:grid;gap:8px;margin:12px 0;padding:14px;border:1px solid #dde5f4;border-radius:12px}
.speech-miner-confirm-modal label{display:grid;gap:6px;font-size:13px}.speech-miner-confirm-modal input,.speech-miner-confirm-modal textarea{padding:9px;border:1px solid #d4dcee;border-radius:8px;font:inherit;color:#334561;box-sizing:border-box;width:100%}
.speech-miner-confirm-modal small{color:#929caf}.speech-miner-confirm-modal button{padding:8px 12px;border:1px solid #d2d9ef;border-radius:8px;background:#f6f7ff;color:#6b61bc;font:inherit;font-size:13px;cursor:pointer}.speech-miner-confirm-modal button:disabled{opacity:.5;cursor:default}
</style>
