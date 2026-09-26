import { showPermissionToast } from './uiFeedback'
import type {
  AccountDashboard,
  AccountProfile,
  AuthSessionSummary,
  AdminAuditPage,
  AdminCustomer,
  AdminCustomerPage,
  CustomerCooperationInfo,
  AgentSummary,
  Bootstrap,
  CommercialMembershipInput,
  CommercialMembershipPlan,
  CommercialTimeCardProduct,
  CommercialTimeCardInput,
  CustomerTimeCardOffer,
  CommercialDeviceProduct,
  CommercialDeviceInput,
  CustomerDeviceOffer,
  CustomerMembershipOffer,
  CustomerShopOrder,
  CreateCustomerShopOrderInput,
  SandboxPayOrderInput,
  SandboxRefundOrderInput,
  RefundRecord,
  FeatureRecord,
  FeatureRecordInput,
  MarketingCampaign,
  MarketingCampaignInput,
  InventoryWarehouse,
  InventoryDeviceProduct,
  InventoryDeviceSKUType,
  InventoryBatchInboundInput,
  InventoryBatchInboundResult,
  InventoryDevice,
  InventoryDeviceLedgerEntry,
  InventoryStockDocument,
  InventorySummary,
  InventoryRMA,
  InventoryRMAPage,
  InventoryCreateDeviceInput,
  InventoryDeviceTransitionInput,
  InventoryCreateRMAInput,
  InventoryCompleteRMAInput,
  RMAEvent,
  RMACost,
  CreateRMACostInput,
  AfterSalesRequestPage,
  AfterSalesRequestInput,
  LogisticsShipment,
  CreateLogisticsShipmentInput,
  UpdateLogisticsShipmentStatusInput,
  ScrapDisposalInput,
  ScrapDisposal,
  OperatingFinanceOverview,
  TokenPurchase,
  TokenPurchaseInput,
  SalesPerformanceResponse,
  SalesFollowup,
  SalesFollowupResponse,
  SalesCatalog,
  IncentiveProgram,
  IncentiveProgramInput,
  SettlementDashboard,
  SettlementBatch,
  CreateSettlementBatchInput,
  AgentExitCheck,
  AgentExitRecord,
  AgentLevel,
  AgentLevelHistory,
  AgentContract,
  CreateRoomPayload,
  FinanceDashboard,
  BeneficiaryWalletDashboard,
  WithdrawalRequest,
  Room,
  RoomEvent,
  RoomEventPage,
  RoomSessionStats,
  LiveReviewResponse,
  RoomBlockedUser,
  RoomBrainView,
  SpeechRuntimeSnapshot,
  AgentDecisionSnapshot,
  AgentDecisionSimulationResult,
  AgentDecisionEnqueueResult,
  RoomCaptureSnapshot,
  RoomSpeechAnalysisStatus,
  SpeechAnalysisProfile,
  SpeechAnalysisProfileInput,
  LiveDevice,
  LiveOpsRoomQuotaSummary,
  LiveOpsRoomQuotaAdjustInput,
  MembershipRoomLimitUpdate,
  LiveRuntimeSession,
  LiveRuntimeSnapshot,
  LiveQuotaSummary,
  LiveRuntimeEvent,
  LiveAgentSettings,
  LiveAgentSettingsInput,
  LiveAgentPlan,
  LiveAgentConfigVersion,
  LiveAgentConfigInput,
  LivePolicyIndustry,
  LivePolicyContext,
  LivePolicyRule,
  LivePolicyAgentResponse,
  LivePolicyTestResult,
  LivePolicyLearningCandidate,
  CreateLivePolicyLearningCandidateInput,
  AdoptLivePolicyLearningCandidateInput,
  LivePolicyLearningAdoptResult,
  LivePolicyVersion,
  LiveRoomPolicyContext,
  LiveSupportAuthorization,
  LiveSupportCapability,
  LiveSupportRequest,
  LiveSupportStaff,
  LiveSupportTrainingDraft,
  MediaAsset,
  VoiceProfile,
  OfficialVoice,
  VoicePreviewResponse,
  InvitationDashboard,
  InvitePreview,
  InitialCredential,
  ResourceDashboard,
  SalesStaffSummary,
  SalesStaffPage,
  StaffRoleSummary,
  StaffGroupSummary,
  StaffEmployeeSummary,
  StaffDashboard,
  StaffPermissionCenterDashboard,
  StaffFinanceOverview,
  StaffFinanceOperationResult,
  SystemAgentChatResponse,
  SystemAgentContextResponse,
  AgentPromptConfig,
  AgentPromptConfigUpdate,
  AgentPromptHistory,
  SystemDictionaryItem,
  SystemDictionaryItemInput,
  SystemSettingUpdate,
  SystemWarehouseInput,
  SystemSettingsDashboard,
  PublicSystemConfig,
  Tenant,
} from './types'

interface ListResponse<T> {
  items: T[]
}

export async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (init?.body !== undefined && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  let response: Response
  try {
    response = await fetch(url, {
      credentials: 'include',
      ...init,
      headers,
    })
  } catch {
    throw new Error('后台服务暂不可用，请稍后重试')
  }

  if (!response.ok) {
    let message =
      response.status >= 500
        ? '后台服务暂不可用，请稍后重试'
        : '请求失败'
    try {
      const body = await response.json() as { error?: string }
      if (body.error) message = body.error
    } catch {
      // Keep the fallback message.
    }
    if (response.status === 403) {
      showPermissionToast(message)
    }
    throw new Error(message)
  }

  if (response.status === 204) {
    return undefined as T
  }

  return response.json() as Promise<T>
}

export function getBootstrap() {
  return request<Bootstrap>('/api/v1/bootstrap')
}

export function getPublicSystemConfig() {
  return request<PublicSystemConfig>('/api/v1/system/public-config')
}

export function getSystemSettingsDashboard() {
  return request<SystemSettingsDashboard>('/api/v1/system/settings')
}

export function updateSystemSettings(settings: SystemSettingUpdate[]) {
  return request<SystemSettingsDashboard>('/api/v1/system/settings', {
    method: 'PUT',
    body: JSON.stringify({ settings }),
  })
}

export function updateAgentPromptConfigs(items: AgentPromptConfigUpdate[]) {
  return request<{ items: AgentPromptConfig[] }>('/api/v1/system/agent-prompts', {
    method: 'PUT',
    body: JSON.stringify({ items }),
  })
}

export function publishAgentPromptConfig(key: string) {
  return request<{ items: AgentPromptConfig[] }>(
    '/api/v1/system/agent-prompts/' + encodeURIComponent(key) + '/publish',
    { method: 'POST' },
  )
}

export function getAgentPromptHistory(key: string) {
  return request<{ items: AgentPromptHistory[] }>(
    '/api/v1/system/agent-prompts/' + encodeURIComponent(key) + '/history',
  )
}

export function rollbackAgentPromptConfig(key: string, version: number) {
  return request<{ items: AgentPromptConfig[] }>(
    '/api/v1/system/agent-prompts/' + encodeURIComponent(key) + '/rollback',
    { method: 'POST', body: JSON.stringify({ version }) },
  )
}

export function resetAgentPromptConfig(key: string) {
  return request<{ items: AgentPromptConfig[] }>(
    '/api/v1/system/agent-prompts/' + encodeURIComponent(key) + '/reset',
    { method: 'POST' },
  )
}

export function updateMembershipRoomLimits(items: MembershipRoomLimitUpdate[]) {
  return request<SystemSettingsDashboard>('/api/v1/system/membership-room-limits', {
    method: 'PUT',
    body: JSON.stringify({ items }),
  })
}

export function getSystemDictionaryItems(
  category: string,
  includeDisabled = false,
) {
  const query = includeDisabled ? '?include_disabled=1' : ''
  return request<ListResponse<SystemDictionaryItem>>(
    '/api/v1/system/dictionaries/' + encodeURIComponent(category) + query,
  )
}

export function createSystemDictionaryItem(payload: SystemDictionaryItemInput) {
  return request<SystemDictionaryItem>('/api/v1/system/dictionaries', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateSystemDictionaryItem(
  itemId: number,
  payload: SystemDictionaryItemInput,
) {
  return request<SystemDictionaryItem>(
    '/api/v1/system/dictionaries/' + encodeURIComponent(String(itemId)),
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function createSystemWarehouse(payload: SystemWarehouseInput) {
  return request<InventoryWarehouse>('/api/v1/system/warehouses', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateSystemWarehouse(
  warehouseId: number,
  payload: SystemWarehouseInput,
) {
  return request<InventoryWarehouse>(
    '/api/v1/system/warehouses/' + encodeURIComponent(String(warehouseId)),
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function getTenants() {
  return request<ListResponse<Tenant>>('/api/v1/tenants')
}

export function getRooms(tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<ListResponse<Room>>('/api/v1/rooms' + query)
}

export function createRoom(payload: CreateRoomPayload) {
  return request<Room>('/api/v1/rooms', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteRoom(roomId: number) {
  return request<void>('/api/v1/rooms/' + roomId, {
    method: 'DELETE',
  })
}

export function getRoom(roomId: number) {
  return request<Room>('/api/v1/rooms/' + roomId)
}

export function setRoomMonitor(roomId: number, enabled: boolean) {
  return request<Room>('/api/v1/rooms/' + roomId + '/monitor', {
    method: 'PATCH',
    body: JSON.stringify({ enabled }),
  })
}

export function getRoomEvents(roomId: number, limit = 200) {
  return request<ListResponse<RoomEvent>>(
    '/api/v1/rooms/' + roomId + '/events?limit=' + limit,
  )
}

export function getRoomImportantEvents(roomId: number, eventType: string, limit = 300, beforeId = 0) {
  const query = new URLSearchParams({
    channel: 'important',
    type: eventType,
    limit: String(limit),
  })
  if (beforeId > 0) query.set('before_id', String(beforeId))
  return request<RoomEventPage>('/api/v1/rooms/' + roomId + '/events?' + query.toString())
}

export function getRoomSessionStats(roomId: number) {
  return request<RoomSessionStats>('/api/v1/rooms/' + roomId + '/session-stats')
}

export function getRoomReview(roomId: number, limit = 5000) {
  return request<LiveReviewResponse>(
    '/api/v1/rooms/' + roomId + '/review?limit=' + encodeURIComponent(String(limit)),
  )
}

export function resolveRoomSessionDecision(roomId: number, action: 'merge' | 'fresh') {
  return request<RoomSessionStats>('/api/v1/rooms/' + roomId + '/session-decision', {
    method: 'POST',
    body: JSON.stringify({ action }),
  })
}

export function getRoomBrain(roomId: number) {
  return request<RoomBrainView>('/api/v1/rooms/' + roomId + '/brain')
}

export function getRoomSpeechRuntime(roomId: number) {
  return request<SpeechRuntimeSnapshot>('/api/v1/rooms/' + roomId + '/speech-runtime')
}

export function getRoomAgentDecisions(roomId: number) {
  return request<AgentDecisionSnapshot>('/api/v1/rooms/' + roomId + '/agent-decisions')
}

export function simulateRoomAgentDecision(roomId: number, payload: { question: string }) {
  return request<AgentDecisionSimulationResult>('/api/v1/rooms/' + roomId + '/agent-decisions/simulate', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function enqueueRoomManualAgentDecision(
  roomId: number,
  payload: {
    question?: string
    topic?: string
    title?: string
    summary?: string
    reply_hint?: string
    event_id?: number
    user_id?: string
    force_reopen?: boolean
    manual_action?: 'answer' | 'quick'
    manual_origin?: 'agent_input' | 'question_cluster' | 'test_simulation'
    execution_mode?: 'intent' | 'verbatim'
    fixed_text?: string
    ttl_seconds?: number
  },
) {
  return request<AgentDecisionEnqueueResult>('/api/v1/rooms/' + roomId + '/agent-decisions/manual', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function completeRoomAgentDecision(roomId: number, decisionId: string) {
  return request<{ ok: boolean }>('/api/v1/rooms/' + roomId + '/agent-decisions/' + encodeURIComponent(decisionId) + '/complete', {
    method: 'POST',
  })
}

export function removeRoomAgentDecision(roomId: number, decisionId: string) {
  return request<{ ok: boolean }>('/api/v1/rooms/' + roomId + '/agent-decisions/' + encodeURIComponent(decisionId), {
    method: 'DELETE',
  })
}

export function getRoomBlockedUsers(roomId: number) {
  return request<ListResponse<RoomBlockedUser>>('/api/v1/rooms/' + roomId + '/blocked-users')
}

export function blockRoomUser(
  roomId: number,
  payload: { user_id?: string; nickname?: string; reason?: string },
) {
  return request<RoomBlockedUser>('/api/v1/rooms/' + roomId + '/blocked-users', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function restoreRoomBlockedUser(
  roomId: number,
  payload: { user_id?: string; nickname?: string },
) {
  return request<{ ok: boolean }>('/api/v1/rooms/' + roomId + '/blocked-users/restore', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getRoomCapture(roomId: number) {
  return request<RoomCaptureSnapshot>('/api/v1/rooms/' + roomId + '/capture')
}

export function startRoomAudioRecording(roomId: number) {
  return request<RoomCaptureSnapshot>('/api/v1/rooms/' + roomId + '/capture/audio/start', {
    method: 'POST',
  })
}

export function stopRoomAudioRecording(roomId: number) {
  return request<RoomCaptureSnapshot>('/api/v1/rooms/' + roomId + '/capture/audio/stop', {
    method: 'POST',
  })
}

export function roomAudioRecordingFileUrl(roomId: number) {
  return '/api/v1/rooms/' + roomId + '/capture/audio/file'
}

export function getRoomSpeechAnalysis(roomId: number) {
  return request<RoomSpeechAnalysisStatus>('/api/v1/rooms/' + roomId + '/speech-analysis')
}

export function startRoomSpeechAnalysis(roomId: number) {
  return request<RoomSpeechAnalysisStatus>('/api/v1/rooms/' + roomId + '/speech-analysis', {
    method: 'POST',
  })
}

export function uploadRoomSpeechAnalysis(
  roomId: number,
  file: File,
  onProgress?: (loaded: number, total: number) => void,
  signal?: AbortSignal,
) {
  const form = new FormData()
  form.append('file', file)
  return new Promise<RoomSpeechAnalysisStatus>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/v1/rooms/' + roomId + '/speech-analysis/upload')
    xhr.withCredentials = true
    xhr.upload.addEventListener('progress', (event) => {
      if (!event.lengthComputable) return
      onProgress?.(event.loaded, event.total)
    })
    xhr.addEventListener('load', () => {
      let payload: RoomSpeechAnalysisStatus | { error?: string } | null = null
      try {
        payload = xhr.responseText ? JSON.parse(xhr.responseText) : null
      } catch {
        payload = null
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(payload as RoomSpeechAnalysisStatus)
        return
      }
      const message = payload && 'error' in payload && payload.error
        ? payload.error
        : (xhr.status >= 500 ? '后台服务暂不可用，请稍后重试' : '上传录音失败')
      if (xhr.status === 403) showPermissionToast(message)
      reject(new Error(message))
    })
    xhr.addEventListener('error', () => reject(new Error('上传录音失败，请检查网络后重试')))
    xhr.addEventListener('abort', () => reject(new DOMException('上传已取消', 'AbortError')))
    if (signal) {
      if (signal.aborted) {
        xhr.abort()
        return
      }
      signal.addEventListener('abort', () => xhr.abort(), { once: true })
    }
    xhr.send(form)
  })
}

export function roomSpeechAnalysisReportUrl(roomId: number) {
  return '/api/v1/rooms/' + roomId + '/speech-analysis/report'
}

export function roomSpeechAnalysisTranscriptUrl(roomId: number) {
  return '/api/v1/rooms/' + roomId + '/speech-analysis/transcript'
}

export async function getRoomSpeechAnalysisReportText(roomId: number) {
  const response = await fetch(roomSpeechAnalysisReportUrl(roomId), {
    credentials: 'include',
    cache: 'no-store',
  })
  if (!response.ok) {
    let message = '读取智能话术分析报告失败'
    try {
      const payload = await response.json() as { error?: string }
      if (payload?.error) message = payload.error
    } catch {
      // Ignore non-JSON error bodies.
    }
    throw new Error(message)
  }
  return response.text()
}

export function getSpeechAnalysisProfiles() {
  return request<{ items: SpeechAnalysisProfile[] }>('/api/v1/live-analysis/profiles')
}

export function createSpeechAnalysisProfile(payload: SpeechAnalysisProfileInput) {
  return request<SpeechAnalysisProfile>('/api/v1/live-analysis/profiles', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function activateSpeechAnalysisProfile(profileId: number) {
  return request<SpeechAnalysisProfile>('/api/v1/live-analysis/profiles/' + profileId + '/activate', {
    method: 'POST',
  })
}

export function getLiveAgentPlans(tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAgentPlan[] }>('/api/v1/live-agent-plans' + query)
}

export function createLiveAgentPlan(payload: { name: string; description?: string; tenant_id?: number }) {
  return request<LiveAgentPlan>('/api/v1/live-agent-plans', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getRoomLiveAgentPlan(roomId: number) {
  return request<{ plan?: LiveAgentPlan | null }>('/api/v1/rooms/' + roomId + '/live-agent-plan')
}

export function bindRoomLiveAgentPlan(planId: number, roomId: number, tenantId?: number) {
  return request<LiveAgentPlan>('/api/v1/live-agent-plans/' + planId + '/room-bindings', {
    method: 'POST',
    body: JSON.stringify({ room_id: roomId, tenant_id: tenantId }),
  })
}

export function getLiveAgentSettings() {
  return request<LiveAgentSettings>('/api/v1/live/agent/settings')
}

export function updateLiveAgentSettings(payload: LiveAgentSettingsInput) {
  return request<LiveAgentSettings>('/api/v1/live/agent/settings', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function getLiveAgentConfigVersions() {
  return request<LiveAgentConfigVersion[]>('/api/v1/live/agent/config-versions')
}

export function createLiveAgentConfigDraft(payload: LiveAgentConfigInput) {
  return request<LiveAgentConfigVersion>('/api/v1/live/agent/config-versions', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function activateLiveAgentConfigVersion(versionId: number) {
  return request<{ active_version_id: number }>(
    '/api/v1/live/agent/config-versions/' + versionId + '/activate',
    { method: 'POST' },
  )
}

export function getLiveMediaAssets(assetType?: string) {
  const query = assetType ? '?asset_type=' + encodeURIComponent(assetType) : ''
  return request<MediaAsset[]>('/api/v1/live/media-assets' + query)
}

export function uploadLiveMediaAsset(
  file: File,
  assetType: string,
  options?: {
    agentId?: number
    durationMs?: number
    metadata?: Record<string, unknown>
  },
) {
  const form = new FormData()
  form.append('file', file)
  form.append('asset_type', assetType)
  if (options?.agentId) form.append('agent_id', String(options.agentId))
  if (options?.durationMs !== undefined) form.append('duration_ms', String(options.durationMs))
  if (options?.metadata) form.append('metadata_json', JSON.stringify(options.metadata))
  return request<{ asset: MediaAsset; content_url: string }>('/api/v1/live/media-assets', {
    method: 'POST',
    body: form,
  })
}

export function deleteLiveMediaAsset(assetId: number) {
  return request<void>('/api/v1/live/media-assets/' + assetId, {
    method: 'DELETE',
  })
}

export function getLiveVoiceProfiles() {
  return request<VoiceProfile[]>('/api/v1/live/voice-profiles')
}

export function createLiveVoiceProfile(payload: {
  agent_id?: number
  name: string
  provider: string
  voice_id?: string
  sample_asset_id?: number
  clone_status?: string
  config?: Record<string, unknown>
  is_default?: boolean
}) {
  return request<VoiceProfile>('/api/v1/live/voice-profiles', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getLiveOfficialVoices() {
  return request<{ items: OfficialVoice[] }>('/api/v1/live/official-voices')
}

export function previewLiveOfficialVoice(voiceId: string, text = '') {
  return request<VoicePreviewResponse>(
    '/api/v1/live/official-voices/' + encodeURIComponent(voiceId) + '/preview',
    { method: 'POST', body: JSON.stringify({ text }) },
  )
}

export function cloneLiveVoiceProfile(payload: { name: string; sample_asset_id: number }) {
  return request<VoiceProfile>('/api/v1/live/voice-profiles/clone', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function previewLiveVoiceProfile(profileId: number, text = '') {
  return request<VoicePreviewResponse>(
    '/api/v1/live/voice-profiles/' + profileId + '/preview',
    { method: 'POST', body: JSON.stringify({ text }) },
  )
}

export function chatLiveAgent(
  roomId: number,
  payload: {
    message: string
    anchor_transcript?: string
    history?: Array<{ role: 'user' | 'agent'; text: string }>
  },
) {
  return request<{
    reply: string
    kind: 'local' | 'model'
    model?: string
    latency_ms?: number
  }>('/api/v1/live/rooms/' + roomId + '/agent/chat', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getLivePolicyIndustries() {
  return request<ListResponse<LivePolicyIndustry>>('/api/v1/live/policies/industries')
}

export function upsertLivePolicyIndustry(payload: Omit<LivePolicyIndustry, 'created_at' | 'updated_at'>) {
  return request<LivePolicyIndustry>('/api/v1/live/policies/industries', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function bindTenantLivePolicyIndustry(tenantId: number, industryCode: string) {
  return request<{ tenant_id: number; industry_code: string }>(
    '/api/v1/live/policies/tenants/' + tenantId + '/industry',
    {
      method: 'PUT',
      body: JSON.stringify({ industry_code: industryCode }),
    },
  )
}

export function getLivePolicyAdminContext(layer: 'L1' | 'L2', industryCode?: string) {
  const params = new URLSearchParams({ layer })
  params.set('_ts', String(Date.now()))
  if (industryCode) params.set('industry_code', industryCode)
  return request<LivePolicyContext>('/api/v1/live/policies/admin/context?' + params.toString())
}

export function createLivePolicyAdminDraft(payload: {
  layer: 'L1' | 'L2'
  industry_code?: string
  source_text: string
  rules: LivePolicyRule[]
  note?: string
}) {
  return request<LivePolicyVersion>('/api/v1/live/policies/admin/drafts', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function chatLivePolicyAdminAgent(payload: {
  layer: 'L1' | 'L2'
  industry_code?: string
  message: string
  history?: Array<{ role: 'user' | 'agent'; text: string }>
}) {
  return request<LivePolicyAgentResponse>('/api/v1/live/policies/admin/agent/chat', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function testLivePolicyAdmin(payload: {
  layer: 'L1' | 'L2'
  industry_code?: string
  room_id?: number
  message: string
  history?: Array<{ role: 'user' | 'agent'; text: string }>
}) {
  return request<LivePolicyTestResult>('/api/v1/live/policies/admin/test', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function createLivePolicyLearningCandidate(
  payload: CreateLivePolicyLearningCandidateInput,
) {
  return request<LivePolicyLearningCandidate>('/api/v1/live/policy-learning/candidates', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getLivePolicyLearningCandidates(status = 'pending') {
  const params = new URLSearchParams({ status })
  return request<{ items: LivePolicyLearningCandidate[] }>(
    '/api/v1/live/policy-learning/candidates?' + params.toString(),
  )
}

export function adoptLivePolicyLearningCandidate(
  candidateId: number,
  payload: AdoptLivePolicyLearningCandidateInput = {},
) {
  return request<LivePolicyLearningAdoptResult>(
    '/api/v1/live/policy-learning/candidates/' + candidateId + '/adopt',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function rejectLivePolicyLearningCandidate(
  candidateId: number,
  reviewNote = '',
) {
  return request<LivePolicyLearningCandidate>(
    '/api/v1/live/policy-learning/candidates/' + candidateId + '/reject',
    {
      method: 'POST',
      body: JSON.stringify({ review_note: reviewNote }),
    },
  )
}

export function publishLivePolicyAdminVersion(versionId: number) {
  return request<LivePolicyVersion>(
    '/api/v1/live/policies/admin/versions/' + versionId + '/publish',
    { method: 'POST' },
  )
}

export function rollbackLivePolicyAdminVersion(versionId: number) {
  return request<LivePolicyVersion>(
    '/api/v1/live/policies/admin/versions/' + versionId + '/rollback',
    { method: 'POST' },
  )
}

export function getLiveRoomPolicyContext(roomId: number) {
  return request<LiveRoomPolicyContext>('/api/v1/live/rooms/' + roomId + '/policy')
}

export function chatLiveRoomPolicyAgent(
  roomId: number,
  payload: {
    message: string
    history?: Array<{ role: 'user' | 'agent'; text: string }>
    scene?: 'reference_answer' | string
  },
) {
  return request<LivePolicyAgentResponse>('/api/v1/live/rooms/' + roomId + '/policy-agent/chat', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function publishLiveRoomPolicyVersion(roomId: number, versionId: number) {
  return request<LivePolicyVersion>(
    '/api/v1/live/rooms/' + roomId + '/policy/versions/' + versionId + '/publish',
    { method: 'POST' },
  )
}

export function rollbackLiveRoomPolicyVersion(roomId: number, versionId: number) {
  return request<LivePolicyVersion>(
    '/api/v1/live/rooms/' + roomId + '/policy/versions/' + versionId + '/rollback',
    { method: 'POST' },
  )
}

export function getLiveSupportStaff() {
  return request<{ items: LiveSupportStaff[] }>('/api/v1/live/support/staff')
}

export function getLiveRoomSupportAuthorizations(roomId: number) {
  return request<{ items: LiveSupportAuthorization[] }>(
    '/api/v1/live/rooms/' + roomId + '/support-authorizations',
  )
}

export function getLiveRoomSupportRequests(roomId: number) {
  return request<{ items: LiveSupportRequest[] }>(
    '/api/v1/live/rooms/' + roomId + '/support-requests',
  )
}

export function createLiveRoomSupportRequest(
  roomId: number,
  staffUserId: number,
  capabilities: LiveSupportCapability[],
) {
  return request<LiveSupportRequest>(
    '/api/v1/live/rooms/' + roomId + '/support-requests/' + staffUserId,
    {
      method: 'POST',
      body: JSON.stringify({ capabilities }),
    },
  )
}

export function updateLiveRoomSupportAuthorizations(
  roomId: number,
  staffUserId: number,
  capabilities: LiveSupportCapability[],
) {
  return request<{ items: LiveSupportAuthorization[] }>(
    '/api/v1/live/rooms/' + roomId + '/support-authorizations/' + staffUserId,
    {
      method: 'PUT',
      body: JSON.stringify({ capabilities }),
    },
  )
}

export function getLiveOpsSupportAuthorizations() {
  return request<{ items: LiveSupportAuthorization[] }>(
    '/api/v1/liveops/support-authorizations',
  )
}

export function getLiveOpsSupportRequests() {
  return request<{ items: LiveSupportRequest[] }>('/api/v1/liveops/support-requests')
}

export function acceptLiveOpsSupportRequest(requestId: number, note = '') {
  return request<LiveSupportRequest>(
    '/api/v1/liveops/support-requests/' + requestId + '/accept',
    { method: 'POST', body: JSON.stringify({ note }) },
  )
}

export function rejectLiveOpsSupportRequest(requestId: number, note = '') {
  return request<LiveSupportRequest>(
    '/api/v1/liveops/support-requests/' + requestId + '/reject',
    { method: 'POST', body: JSON.stringify({ note }) },
  )
}

export function createLiveOpsAnchorTraining(
  roomId: number,
  payload: { text: string; asset_ids?: number[] },
) {
  return request<LiveSupportTrainingDraft>(
    '/api/v1/liveops/support/rooms/' + roomId + '/anchor-training',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function activateLiveOpsSupportConfigVersion(roomId: number, versionId: number) {
  return request<{ status: string; version_id: number }>(
    '/api/v1/liveops/support/rooms/' + roomId + '/config-versions/' + versionId + '/activate',
    { method: 'POST' },
  )
}

export function uploadLiveOpsSupportMediaAsset(
  roomId: number,
  file: File,
  assetType: 'document' | 'audio' | 'voice_sample',
) {
  const form = new FormData()
  form.append('file', file)
  form.append('asset_type', assetType)
  return request<{ asset: MediaAsset }>(
    '/api/v1/liveops/support/rooms/' + roomId + '/media-assets',
    {
      method: 'POST',
      body: form,
    },
  )
}

export function getLiveOpsSupportVoiceProfiles(roomId: number) {
  return request<{ items: VoiceProfile[] }>(
    '/api/v1/liveops/support/rooms/' + roomId + '/voice-profiles',
  )
}

export function createLiveOpsSupportVoiceProfile(
  roomId: number,
  payload: {
    name: string
    provider: string
    voice_id?: string
    sample_asset_id?: number
    clone_status?: string
    config?: Record<string, unknown>
    is_default?: boolean
  },
) {
  return request<VoiceProfile>(
    '/api/v1/liveops/support/rooms/' + roomId + '/voice-profiles',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getLiveDevices() {
  return request<LiveDevice[]>('/api/v1/live/devices')
}

export function getLiveOpsRoomQuotas() {
  return request<ListResponse<LiveOpsRoomQuotaSummary>>('/api/v1/liveops/room-quotas')
}

export function adjustLiveOpsRoomQuota(
  tenantId: number,
  payload: LiveOpsRoomQuotaAdjustInput,
) {
  return request<LiveOpsRoomQuotaSummary>(
    '/api/v1/liveops/room-quotas/' + tenantId + '/adjust',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function bindLiveDevice(deviceId: number, roomId: number, bindingRole = 'primary') {
  return request<LiveDevice>('/api/v1/live/devices/' + deviceId + '/bind', {
    method: 'POST',
    body: JSON.stringify({ room_id: roomId, binding_role: bindingRole }),
  })
}

export function heartbeatLiveDevice(deviceId: number, roomId?: number) {
  return request<LiveDevice>('/api/v1/live/devices/' + deviceId + '/heartbeat', {
    method: 'POST',
    body: JSON.stringify(roomId ? { room_id: roomId } : {}),
  })
}

export function controlLiveDevice(
  deviceId: number,
  roomId: number,
  action: 'connect' | 'pause' | 'resume' | 'disconnect',
) {
  return request<LiveDevice>('/api/v1/live/devices/' + deviceId + '/control', {
    method: 'POST',
    body: JSON.stringify({ room_id: roomId, action }),
  })
}

export function getLiveQuotaSummary() {
  return request<LiveQuotaSummary>('/api/v1/live/quota-summary')
}

export function activateLiveQuotaCards(count = 1) {
  return request<{ activated_count: number; quota: LiveQuotaSummary }>(
    '/api/v1/live/quota/activate',
    {
      method: 'POST',
      body: JSON.stringify({ count }),
    },
  )
}

export function getLiveRuntime(roomId: number) {
  return request<LiveRuntimeSnapshot>('/api/v1/rooms/' + roomId + '/runtime')
}

export function setLiveRuntimeMode(roomId: number, mode: 'control' | 'anchor') {
  return request<{ room_id: number; state: string; mode: 'control' | 'anchor'; working_seconds: number }>(
    '/api/v1/rooms/' + roomId + '/runtime/mode',
    {
      method: 'POST',
      body: JSON.stringify({ mode }),
    },
  )
}

export function setLiveRuntimePlan(roomId: number, planId: number) {
  return request<{ room_id: number; state: string; mode: 'control' | 'anchor'; plan_id: number; plan_name: string; working_seconds: number }>(
    '/api/v1/rooms/' + roomId + '/runtime/plan',
    {
      method: 'POST',
      body: JSON.stringify({ plan_id: planId }),
    },
  )
}

export function startLiveRuntime(roomId: number, deviceId?: number) {
  return request<LiveRuntimeSession>('/api/v1/rooms/' + roomId + '/runtime/start', {
    method: 'POST',
    body: JSON.stringify(deviceId ? { device_id: deviceId } : {}),
  })
}

export function pauseLiveRuntime(roomId: number) {
  return request<LiveRuntimeSession>('/api/v1/rooms/' + roomId + '/runtime/pause', {
    method: 'POST',
  })
}

export function resumeLiveRuntime(roomId: number) {
  return request<LiveRuntimeSession>('/api/v1/rooms/' + roomId + '/runtime/resume', {
    method: 'POST',
  })
}

export function stopLiveRuntime(roomId: number, reason = 'manual_stop') {
  return request<LiveRuntimeSession>('/api/v1/rooms/' + roomId + '/runtime/stop', {
    method: 'POST',
    body: JSON.stringify({ reason }),
  })
}

export function getLiveRuntimeEvents(roomId: number, limit = 100) {
  return request<LiveRuntimeEvent[]>(
    '/api/v1/rooms/' + roomId + '/runtime/events?limit=' + limit,
  )
}

export function recordLiveRuntimeEvent(
  roomId: number,
  eventCode: string,
  detail?: Record<string, unknown>,
  title?: string,
) {
  return request<LiveRuntimeEvent>('/api/v1/rooms/' + roomId + '/runtime/events', {
    method: 'POST',
    body: JSON.stringify({ event_code: eventCode, title, detail }),
  })
}

export function login(payload: {
  username: string
  password: string
  captcha: string
}) {
  return request<Bootstrap>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({
      method: 'password',
      identifier: payload.username,
      credential: payload.password,
      captcha: payload.captcha,
    }),
  })
}

export function sendSMSLoginCode(phone: string) {
  return request<{
    phone: string
    retry_after_seconds: number
    debug_code?: string
  }>('/api/v1/auth/challenges/sms', {
    method: 'POST',
    body: JSON.stringify({ phone }),
  })
}

export function loginWithSMS(payload: { phone: string; code: string }) {
  return request<Bootstrap>('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({
      method: 'sms',
      identifier: payload.phone,
      credential: payload.code,
    }),
  })
}

export function register(payload: {
  username: string
  phone: string
  password: string
  confirm_password: string
  invite_code: string
  captcha: string
}) {
  return request<Bootstrap>('/api/v1/auth/register', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function logout() {
  return request<void>('/api/v1/auth/logout', {
    method: 'POST',
  })
}

export function changePassword(payload: {
  current_password: string
  new_password: string
  confirm_password: string
}) {
  return request<{ ok: boolean }>('/api/v1/auth/change-password', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAuthSessions() {
  return request<ListResponse<AuthSessionSummary>>('/api/v1/auth/sessions')
}

export function logoutOtherAuthSessions() {
  return request<void>('/api/v1/auth/sessions/logout-others', {
    method: 'POST',
  })
}
export function getAccountDashboard() {
  return request<AccountDashboard>('/api/v1/account')
}

export function updateAccountProfile(payload: {
  display_name: string
  phone: string
  email: string
  qq: string
  wechat: string
  province: string
  city: string
  district: string
  address: string
}) {
  return request<AccountProfile>('/api/v1/account/profile', {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function uploadAccountAvatar(file: File) {
  const body = new FormData()
  body.append('avatar', file)
  return request<AccountProfile>('/api/v1/account/avatar', {
    method: 'POST',
    body,
  })
}

export function getFinanceDashboard(limit = 50) {
  return request<FinanceDashboard>(
    '/api/v1/finance/dashboard?limit=' + encodeURIComponent(String(limit)),
  )
}

export function getReferralWallet(limit = 100) {
  return request<BeneficiaryWalletDashboard>(
    '/api/v1/finance/referral-wallet?limit=' + encodeURIComponent(String(limit)),
  )
}

export function createReferralWithdrawal(amountCents: number) {
  return request<WithdrawalRequest>('/api/v1/finance/referral-withdrawals', {
    method: 'POST',
    body: JSON.stringify({ amount_cents: amountCents }),
  })
}

export function getCustomerWithdrawals(accountType = 'all') {
  return request<{ items: WithdrawalRequest[] }>(
    '/api/v1/finance/withdrawals?account_type=' + encodeURIComponent(accountType),
  )
}

export function createCustomerWalletWithdrawal(
  accountType: 'cash' | 'reward',
  amountCents: number,
) {
  return request<WithdrawalRequest>('/api/v1/finance/withdrawals', {
    method: 'POST',
    body: JSON.stringify({ account_type: accountType, amount_cents: amountCents }),
  })
}

export function getFinanceCustomerWithdrawals(status = 'all') {
  return request<{ items: WithdrawalRequest[] }>(
    '/api/v1/finance/customer-withdrawals?status=' + encodeURIComponent(status),
  )
}

export function approveCustomerWalletWithdrawal(withdrawalId: number) {
  return request<WithdrawalRequest>(
    '/api/v1/finance/customer-withdrawals/' + withdrawalId + '/approve',
    { method: 'POST' },
  )
}

export function rejectCustomerWalletWithdrawal(withdrawalId: number, reason: string) {
  return request<WithdrawalRequest>(
    '/api/v1/finance/customer-withdrawals/' + withdrawalId + '/reject',
    { method: 'POST', body: JSON.stringify({ reason }) },
  )
}

export function payCustomerWalletWithdrawal(withdrawalId: number) {
  return request<WithdrawalRequest>(
    '/api/v1/finance/customer-withdrawals/' + withdrawalId + '/pay',
    { method: 'POST' },
  )
}

export function getReferralWithdrawals(status = 'all') {
  return request<{ items: WithdrawalRequest[] }>(
    '/api/v1/finance/referral-withdrawals?status=' + encodeURIComponent(status),
  )
}

export function approveReferralWithdrawal(withdrawalId: number) {
  return request<WithdrawalRequest>(
    '/api/v1/finance/referral-withdrawals/' + withdrawalId + '/approve',
    { method: 'POST' },
  )
}

export function rejectReferralWithdrawal(withdrawalId: number, reason: string) {
  return request<WithdrawalRequest>(
    '/api/v1/finance/referral-withdrawals/' + withdrawalId + '/reject',
    { method: 'POST', body: JSON.stringify({ reason }) },
  )
}

export function payReferralWithdrawal(withdrawalId: number) {
  return request<WithdrawalRequest>(
    '/api/v1/finance/referral-withdrawals/' + withdrawalId + '/pay',
    { method: 'POST' },
  )
}

export function createCustomerRechargeRequest(payload: {
  amount_cents: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>('/api/v1/finance/recharge-request', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAdminCustomers(params: {
  search?: string
  status?: string
  sort?: string
  page?: number
  page_size?: number
  sales_staff_id?: number
} = {}) {
  const query = new URLSearchParams()
  if (params.search) query.set('search', params.search)
  if (params.status) query.set('status', params.status)
  if (params.sort) query.set('sort', params.sort)
  if (params.page) query.set('page', String(params.page))
  if (params.page_size) query.set('page_size', String(params.page_size))
  if (params.sales_staff_id) query.set('sales_staff_id', String(params.sales_staff_id))
  const suffix = query.toString() ? '?' + query.toString() : ''
  return request<AdminCustomerPage>('/api/v1/admin/customers' + suffix)
}

export function updateAdminCustomerCooperation(
  tenantId: number,
  payload: { status: 'cooperating' | 'non_cooperating'; note: string },
) {
  return request<CustomerCooperationInfo>(
    '/api/v1/admin/customers/' + tenantId + '/cooperation',
    {
      method: 'PATCH',
      body: JSON.stringify(payload),
    },
  )
}

export function adminResetCustomerPassword(
  userId: number,
  payload: {
    delivery_method: 'copy' | 'email'
    email?: string
  },
) {
  return request<{
    ok: boolean
    customer: AdminCustomer
    credential: InitialCredential
  }>(
    '/api/v1/admin/customers/' + userId + '/reset-password',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}
export function adminDeleteCustomer(userId: number) {
  return request<void>('/api/v1/admin/customers/' + userId, {
    method: 'DELETE',
  })
}

export function getAdminAuditLogs(params: number | {
  search?: string
  action?: string
  result?: string
  page_size?: number
  before_id?: number
} = {}) {
  if (typeof params === 'number') {
    return request<AdminAuditPage>(
      '/api/v1/admin/audit-logs?page_size=' + encodeURIComponent(String(params)),
    )
  }
  const query = new URLSearchParams()
  if (params.search) query.set('search', params.search)
  if (params.action) query.set('action', params.action)
  if (params.result) query.set('result', params.result)
  if (params.page_size) query.set('page_size', String(params.page_size))
  if (params.before_id) query.set('before_id', String(params.before_id))
  const suffix = query.toString() ? '?' + query.toString() : ''
  return request<AdminAuditPage>('/api/v1/admin/audit-logs' + suffix)
}
export function getCommercialMemberships() {
  return request<ListResponse<CommercialMembershipPlan>>(
    '/api/v1/commercial/memberships',
  )
}

export function createCommercialMembership(payload: CommercialMembershipInput) {
  return request<CommercialMembershipPlan>('/api/v1/commercial/memberships', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveCommercialMembershipDraft(
  planId: number,
  payload: CommercialMembershipInput,
) {
  return request<CommercialMembershipPlan>(
    '/api/v1/commercial/memberships/' + planId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishCommercialMembership(planId: number) {
  return request<CommercialMembershipPlan>(
    '/api/v1/commercial/memberships/' + planId + '/publish',
    {
      method: 'POST',
    },
  )
}

export function setCommercialMembershipListing(planId: number, status: 'active' | 'inactive') {
  return request<CommercialMembershipPlan>('/api/v1/commercial/memberships/' + planId + '/listing', {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}

export function deleteCommercialMembership(planId: number) {
  return request<CommercialMembershipPlan>('/api/v1/commercial/memberships/' + planId, {
    method: 'DELETE',
  })
}
export function getAdminAgents() {
  return request<ListResponse<AgentSummary>>('/api/v1/admin/agents')
}

export function createAdminAgent(payload: {
  code: string
  name: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    item: AgentSummary
    credential: InitialCredential
  }>('/api/v1/admin/agents', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
export function getAgentCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/agent/customers')
}

export function createAgentCustomer(payload: {
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    user_id: number
    tenant_id?: number
    username: string
    display_name: string
    email: string
    status: string
    credential: InitialCredential
  }>('/api/v1/agent/customers', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
export function getInvitePreview(code: string) {
  return request<InvitePreview>(
    '/api/v1/auth/invite/' + encodeURIComponent(code.trim()),
  )
}

export function getInvitationDashboard(params: {
  code_page?: number
  code_page_size?: number
  record_page?: number
  record_page_size?: number
} = {}) {
  const search = new URLSearchParams()
  if (params.code_page) search.set('code_page', String(params.code_page))
  if (params.code_page_size) search.set('code_page_size', String(params.code_page_size))
  if (params.record_page) search.set('record_page', String(params.record_page))
  if (params.record_page_size) search.set('record_page_size', String(params.record_page_size))
  const query = search.toString()
  return request<InvitationDashboard>(
    '/api/v1/invitations/dashboard' + (query ? '?' + query : ''),
  )
}

export function updateOwnInviteCodeStatus(status: 'active' | 'disabled') {
  return request<void>('/api/v1/invitations/mine', {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}

export function updateAdminInviteCodePolicy(
  codeId: number,
  payload: {
    status: 'active' | 'disabled'
    max_uses?: number
    expires_at?: string
  },
) {
  return request<void>('/api/v1/admin/invitations/' + codeId, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}
export function updateAdminInviteCodeStatus(
  codeId: number,
  status: 'active' | 'disabled',
) {
  return request<void>('/api/v1/admin/invitations/' + codeId, {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}
export function getCurrentResources() {
  return request<ResourceDashboard>('/api/v1/resources')
}

export function getAdminAgentResources(orgId: number) {
  return request<ResourceDashboard>(
    '/api/v1/admin/agents/' + orgId + '/resources',
  )
}

export function adjustAdminAgentResource(
  orgId: number,
  payload: {
    resource_type: string
    delta: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/admin/agents/' + orgId + '/resources/adjust',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getAgentCustomerResources(tenantId: number) {
  return request<ResourceDashboard>(
    '/api/v1/agent/customers/' + tenantId + '/resources',
  )
}

export function allocateAgentCustomerResource(
  tenantId: number,
  payload: {
    resource_type: string
    quantity: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/agent/customers/' + tenantId + '/resources/allocate',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}
export function getAdminCustomerResources(tenantId: number) {
  return request<ResourceDashboard>(
    '/api/v1/admin/customers/' + tenantId + '/resources',
  )
}

export function adjustAdminCustomerResource(
  tenantId: number,
  payload: {
    resource_type: string
    delta: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/admin/customers/' + tenantId + '/resources/adjust',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}
export function getAdminSalesStaff(params: {
  search?: string
  page?: number
  page_size?: number
} = {}) {
  const query = new URLSearchParams()
  if (params.search) query.set('search', params.search)
  if (params.page) query.set('page', String(params.page))
  if (params.page_size) query.set('page_size', String(params.page_size))
  const suffix = query.toString() ? '?' + query.toString() : ''
  return request<SalesStaffPage>('/api/v1/admin/sales' + suffix)
}

export function createAdminSalesStaff(payload: {
  employee_code: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    item: SalesStaffSummary
    credential: InitialCredential
  }>('/api/v1/admin/sales', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}
export function assignAdminCustomerSales(
  tenantId: number,
  payload: {
    sales_staff_id: number
    reason: string
  },
) {
  return request<void>(
    '/api/v1/admin/customers/' + tenantId + '/sales-assignment',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function getSalesCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/sales/customers')
}

export function getMySalesPerformance(period: string) {
  const query = new URLSearchParams({ period })
  return request<SalesPerformanceResponse>('/api/v1/sales/performance?' + query.toString())
}

export function getSalesFollowups() {
  return request<SalesFollowupResponse>('/api/v1/sales/followups')
}

export function createSalesFollowup(payload: {
  tenant_id: number
  followup_type: string
  content: string
  next_followup_at?: string
}) {
  return request<SalesFollowup>('/api/v1/sales/followups', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getSalesCatalog() {
  return request<SalesCatalog>('/api/v1/sales/catalog')
}

export interface AgentChatPayload {
  message: string
  history?: Array<{ role: 'user' | 'agent'; text: string }>
  current_path?: string
  navigation?: Array<{ title: string; to: string; section?: string }>
}

export function getClientAgentContext() {
  return request<SystemAgentContextResponse>('/api/v1/client-agent/context')
}

export function chatClientAgent(payload: AgentChatPayload) {
  return request<SystemAgentChatResponse>('/api/v1/client-agent/chat', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getInternalAgentContext() {
  return request<SystemAgentContextResponse>('/api/v1/internal-agent/context')
}

export function chatInternalAgent(payload: AgentChatPayload) {
  return request<SystemAgentChatResponse>('/api/v1/internal-agent/chat', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// Compatibility aliases. The legacy backend route is internal-only.
export function getSystemAgentContext() {
  return getInternalAgentContext()
}

export function chatSystemAgent(payload: AgentChatPayload) {
  return chatInternalAgent(payload)
}

export function getStaffDashboard() {
  return request<StaffDashboard>('/api/v1/staff/dashboard')
}

export function getStaffPermissionCenter() {
  return request<StaffPermissionCenterDashboard>('/api/v1/staff/permission-center')
}

export function updateStaffPermissionCenterRole(roleId: number, permissionIds: number[]) {
  return request<void>('/api/v1/staff/permission-center/roles/' + roleId, {
    method: 'PUT',
    body: JSON.stringify({ permission_ids: permissionIds }),
  })
}

export function createStaffGroup(payload: {
  code: string
  name: string
  description: string
}) {
  return request<StaffGroupSummary>('/api/v1/staff/groups', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateStaffGroup(
  groupId: number,
  payload: {
    name: string
    description: string
    status: 'active' | 'disabled'
  },
) {
  return request<void>('/api/v1/staff/groups/' + groupId, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}

export function createStaffRole(payload: {
  group_id: number
  code: string
  name: string
  description: string
  is_group_manager: boolean
  default_scope_type: string
  permission_ids: number[]
}) {
  return request<StaffRoleSummary>('/api/v1/staff/roles', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateStaffRole(
  roleId: number,
  payload: {
    name: string
    description: string
    is_group_manager: boolean
    default_scope_type: string
    status: 'active' | 'disabled'
    permission_ids: number[]
  },
) {
  return request<void>('/api/v1/staff/roles/' + roleId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function createStaffEmployee(payload: {
  employee_no: string
  primary_group_id: number
  role_ids: number[]
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email'
}) {
  return request<{
    item: StaffEmployeeSummary
    credential: InitialCredential
  }>('/api/v1/staff/employees', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function disableStaffEmployee(employeeId: number) {
  return request<void>('/api/v1/staff/employees/' + employeeId + '/disable', {
    method: 'POST',
  })
}

export function resetStaffEmployeePassword(employeeId: number) {
  return request<{
    item: StaffEmployeeSummary
    credential: InitialCredential
  }>('/api/v1/staff/employees/' + employeeId + '/reset-password', {
    method: 'POST',
  })
}

export function replaceStaffEmployeeRoles(
  employeeId: number,
  roleIds: number[],
) {
  return request<void>('/api/v1/staff/employees/' + employeeId + '/roles', {
    method: 'PUT',
    body: JSON.stringify({ role_ids: roleIds }),
  })
}

export function updateStaffApprovalPolicy(
  policyId: number,
  payload: {
    mode: string
    threshold_amount: number
    approver_role_code: string
    status: 'active' | 'disabled'
  },
) {
  return request<void>('/api/v1/staff/approval-policies/' + policyId, {
    method: 'PATCH',
    body: JSON.stringify(payload),
  })
}
export function getStaffFinanceOverview() {
  return request<StaffFinanceOverview>('/api/v1/staff/finance')
}

export function getStaffFinanceCustomer(tenantId: number) {
  return request<FinanceDashboard>(
    '/api/v1/staff/finance/customers/' + tenantId,
  )
}

export function createStaffFinanceRecharge(payload: {
  tenant_id: number
  amount_cents: number
  payment_method: string
  reason: string
}) {
  return request<StaffFinanceOperationResult>(
    '/api/v1/staff/finance/recharge',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function createStaffFinanceRefund(payload: {
  tenant_id: number
  amount_cents: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>(
    '/api/v1/staff/finance/refund',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function createStaffFinanceReward(payload: {
  tenant_id: number
  amount_cents: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>(
    '/api/v1/staff/finance/reward',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getCommercialAITimeAgents() {
  return request<ListResponse<AgentSummary>>('/api/v1/commercial/ai-time/agents')
}

export function getCommercialAITimeCustomers() {
  return request<ListResponse<AdminCustomer>>('/api/v1/commercial/ai-time/customers')
}

export function createCommercialAITimeGrantRequest(payload: {
  organization_id: number
  resource_seconds: number
  reason: string
}) {
  return request<StaffFinanceOperationResult>('/api/v1/commercial/ai-time/requests', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function approveStaffFinanceTask(taskId: number) {
  return request<void>(
    '/api/v1/staff/finance/approvals/' + taskId + '/approve',
    { method: 'POST' },
  )
}

export function rejectStaffFinanceTask(taskId: number) {
  return request<void>(
    '/api/v1/staff/finance/approvals/' + taskId + '/reject',
    { method: 'POST' },
  )
}


export function getCommercialTimeCards() {
  return request<ListResponse<CommercialTimeCardProduct>>('/api/v1/commercial/time-cards')
}

export function createCommercialTimeCard(payload: CommercialTimeCardInput) {
  return request<CommercialTimeCardProduct>('/api/v1/commercial/time-cards', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveCommercialTimeCardDraft(productId: number, payload: CommercialTimeCardInput) {
  return request<CommercialTimeCardProduct>(
    '/api/v1/commercial/time-cards/' + productId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishCommercialTimeCard(productId: number) {
  return request<CommercialTimeCardProduct>(
    '/api/v1/commercial/time-cards/' + productId + '/publish',
    {
      method: 'POST',
    },
  )
}

export function setCommercialTimeCardListing(productId: number, status: 'active' | 'inactive') {
  return request<CommercialTimeCardProduct>('/api/v1/commercial/time-cards/' + productId + '/listing', {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}

export function deleteCommercialTimeCard(productId: number) {
  return request<CommercialTimeCardProduct>('/api/v1/commercial/time-cards/' + productId, {
    method: 'DELETE',
  })
}

export function getCommercialDeviceProducts() {
  return request<ListResponse<CommercialDeviceProduct>>('/api/v1/commercial/device-products')
}

export function getCommercialDeviceSKUTypes() {
  return request<ListResponse<InventoryDeviceSKUType>>('/api/v1/commercial/device-sku-types')
}

export function createCommercialDeviceProduct(payload: CommercialDeviceInput) {
  return request<CommercialDeviceProduct>('/api/v1/commercial/device-products', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveCommercialDeviceProductDraft(
  productId: number,
  payload: CommercialDeviceInput,
) {
  return request<CommercialDeviceProduct>(
    '/api/v1/commercial/device-products/' + productId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishCommercialDeviceProduct(productId: number) {
  return request<CommercialDeviceProduct>(
    '/api/v1/commercial/device-products/' + productId + '/publish',
    { method: 'POST' },
  )
}

export function setCommercialDeviceProductListing(productId: number, status: 'active' | 'inactive') {
  return request<CommercialDeviceProduct>('/api/v1/commercial/device-products/' + productId + '/listing', {
    method: 'PATCH',
    body: JSON.stringify({ status }),
  })
}

export function deleteCommercialDeviceProduct(productId: number) {
  return request<CommercialDeviceProduct>('/api/v1/commercial/device-products/' + productId, {
    method: 'DELETE',
  })
}

export function getCustomerMembershipOffers() {
  return request<ListResponse<CustomerMembershipOffer>>('/api/v1/shop/memberships')
}

export function getCustomerDeviceOffers() {
  return request<ListResponse<CustomerDeviceOffer>>('/api/v1/shop/devices')
}

export function getCustomerTimeCardOffers() {
  return request<ListResponse<CustomerTimeCardOffer>>('/api/v1/shop/time-cards')
}

export function getCommercialMarketingCampaigns() {
  return request<ListResponse<MarketingCampaign>>('/api/v1/commercial/marketing-campaigns')
}

export function createCommercialMarketingCampaign(payload: MarketingCampaignInput) {
  return request<MarketingCampaign>('/api/v1/commercial/marketing-campaigns', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateCommercialMarketingCampaign(
  campaignId: number,
  payload: MarketingCampaignInput,
) {
  return request<MarketingCampaign>('/api/v1/commercial/marketing-campaigns/' + campaignId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function deleteCommercialMarketingCampaign(campaignId: number) {
  return request<void>('/api/v1/commercial/marketing-campaigns/' + campaignId, {
    method: 'DELETE',
  })
}

export function getCustomerMarketingCampaigns() {
  return request<ListResponse<MarketingCampaign>>('/api/v1/shop/marketing-campaigns')
}

export function getCustomerShopOrders() {
  return request<ListResponse<CustomerShopOrder>>('/api/v1/shop/orders')
}

export function getCustomerShopOrder(orderId: number) {
  return request<CustomerShopOrder>('/api/v1/shop/orders/' + orderId)
}

export function createCustomerShopOrder(payload: CreateCustomerShopOrderInput) {
  return request<CustomerShopOrder>('/api/v1/shop/orders', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function sandboxPayCustomerShopOrder(orderId: number, payload: SandboxPayOrderInput) {
  return request<{ order: CustomerShopOrder; sandbox: boolean }>(
    '/api/v1/shop/orders/' + orderId + '/sandbox-pay',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function sandboxRefundCustomerShopOrder(orderId: number, payload: SandboxRefundOrderInput) {
  return request<{ order: CustomerShopOrder; refund: RefundRecord; sandbox: boolean }>(
    '/api/v1/shop/orders/' + orderId + '/sandbox-refund',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function cancelCustomerShopOrder(orderId: number) {
  return request<CustomerShopOrder>('/api/v1/shop/orders/' + orderId + '/cancel', {
    method: 'POST',
  })
}

export function getFeatureRecords(featureKey: string) {
  return request<ListResponse<FeatureRecord>>(
    '/api/v1/admin/features/' + encodeURIComponent(featureKey),
  )
}

export function createFeatureRecord(featureKey: string, payload: FeatureRecordInput) {
  return request<FeatureRecord>(
    '/api/v1/admin/features/' + encodeURIComponent(featureKey),
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function updateFeatureRecord(
  featureKey: string,
  recordId: number,
  payload: FeatureRecordInput,
) {
  return request<FeatureRecord>(
    '/api/v1/admin/features/' +
      encodeURIComponent(featureKey) +
      '/' +
      encodeURIComponent(String(recordId)),
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function deleteFeatureRecord(featureKey: string, recordId: number) {
  return request<void>(
    '/api/v1/admin/features/' +
      encodeURIComponent(featureKey) +
      '/' +
      encodeURIComponent(String(recordId)),
    {
      method: 'DELETE',
    },
  )
}


export function getInventoryWarehouses() {
  return request<ListResponse<InventoryWarehouse>>('/api/v1/inventory/warehouses')
}

export function getInventoryDeviceProducts() {
  return request<ListResponse<InventoryDeviceProduct>>('/api/v1/inventory/device-products')
}

export function getInventoryAgents() {
  return request<ListResponse<AgentSummary>>('/api/v1/inventory/agents')
}

export function createInventoryBatchInbound(payload: InventoryBatchInboundInput) {
  return request<InventoryBatchInboundResult>('/api/v1/inventory/inbounds', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getInventoryDevices() {
  return request<ListResponse<InventoryDevice>>('/api/v1/inventory/devices')
}

export function createInventoryDevice(payload: InventoryCreateDeviceInput) {
  return request<InventoryDevice>('/api/v1/inventory/devices', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function transitionInventoryDevice(
  deviceId: number,
  payload: InventoryDeviceTransitionInput,
) {
  return request<InventoryDevice>(
    '/api/v1/inventory/devices/' + deviceId + '/transition',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function disposeInventoryScrapDevice(
  deviceId: number,
  payload: ScrapDisposalInput,
) {
  return request<ScrapDisposal>(
    '/api/v1/inventory/devices/' + deviceId + '/scrap-dispose',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getInventoryDeviceLedger(deviceId: number) {
  return request<ListResponse<InventoryDeviceLedgerEntry>>(
    '/api/v1/inventory/devices/' + deviceId + '/ledger',
  )
}

export function getInventoryLedger() {
  return request<ListResponse<InventoryDeviceLedgerEntry>>('/api/v1/inventory/ledger')
}

export function getInventorySummary() {
  return request<ListResponse<InventorySummary>>('/api/v1/inventory/summary')
}

export function getInventoryDocuments() {
  return request<ListResponse<InventoryStockDocument>>('/api/v1/inventory/documents')
}

export function getInventoryRMAs(page = 1, pageSize = 20) {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  return request<InventoryRMAPage>(
    '/api/v1/inventory/rmas?' + params.toString(),
  )
}

export function createInventoryRMA(payload: InventoryCreateRMAInput) {
  return request<InventoryRMA>('/api/v1/inventory/rmas', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function acceptInventoryRMA(rmaId: number) {
  return request<InventoryRMA>('/api/v1/inventory/rmas/' + rmaId + '/accept', {
    method: 'POST',
  })
}

export function startInventoryRMARepair(rmaId: number) {
  return request<InventoryRMA>('/api/v1/inventory/rmas/' + rmaId + '/start-repair', {
    method: 'POST',
  })
}

export function getInventoryRMAEvents(rmaId: number) {
  return request<ListResponse<RMAEvent>>('/api/v1/inventory/rmas/' + rmaId + '/events')
}

export function getInventoryRMACosts(rmaId: number) {
  return request<ListResponse<RMACost>>('/api/v1/inventory/rmas/' + rmaId + '/costs')
}

export function createInventoryRMACost(rmaId: number, payload: CreateRMACostInput) {
  return request<RMACost>('/api/v1/inventory/rmas/' + rmaId + '/costs', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAfterSalesRequests(page = 1, pageSize = 20) {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  return request<AfterSalesRequestPage>('/api/v1/after-sales/requests?' + params.toString())
}

export function createAfterSalesRequest(payload: AfterSalesRequestInput) {
  return request<InventoryRMA>('/api/v1/after-sales/requests', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAfterSalesRequestEvents(rmaId: number) {
  return request<ListResponse<RMAEvent>>('/api/v1/after-sales/requests/' + rmaId + '/events')
}

export function completeInventoryRMA(
  rmaId: number,
  payload: InventoryCompleteRMAInput,
) {
  return request<InventoryRMA>(
    '/api/v1/inventory/rmas/' + rmaId + '/complete',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getLogisticsShipments() {
  return request<ListResponse<LogisticsShipment>>('/api/v1/logistics/shipments')
}

export function createLogisticsShipment(payload: CreateLogisticsShipmentInput) {
  return request<LogisticsShipment>('/api/v1/logistics/shipments', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateLogisticsShipmentStatus(
  shipmentId: number,
  payload: UpdateLogisticsShipmentStatusInput,
) {
  return request<LogisticsShipment>(
    '/api/v1/logistics/shipments/' + shipmentId + '/status',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}


export function getAdminSalesPerformance(
  period: string,
  params: {
    search?: string
    sort?: string
    page?: number
    page_size?: number
  } = {},
) {
  const query = new URLSearchParams({ period })
  if (params.search) query.set('search', params.search)
  if (params.sort) query.set('sort', params.sort)
  if (params.page) query.set('page', String(params.page))
  if (params.page_size) query.set('page_size', String(params.page_size))
  return request<SalesPerformanceResponse>(
    '/api/v1/admin/sales/performance?' + query.toString(),
  )
}


export function getIncentivePrograms(programType?: string) {
  const suffix = programType ? '?type=' + encodeURIComponent(programType) : ''
  return request<ListResponse<IncentiveProgram>>('/api/v1/commercial/incentives' + suffix)
}

export function createIncentiveProgram(payload: IncentiveProgramInput) {
  return request<IncentiveProgram>('/api/v1/commercial/incentives', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function saveIncentiveProgramDraft(
  programId: number,
  payload: IncentiveProgramInput,
) {
  return request<IncentiveProgram>(
    '/api/v1/commercial/incentives/' + programId + '/draft',
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function publishIncentiveProgram(programId: number) {
  return request<IncentiveProgram>(
    '/api/v1/commercial/incentives/' + programId + '/publish',
    { method: 'POST' },
  )
}

export function getOperatingFinance() {
  return request<OperatingFinanceOverview>('/api/v1/finance/operating')
}

export function createTokenPurchase(payload: TokenPurchaseInput) {
  return request<TokenPurchase>('/api/v1/finance/token-purchases', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getFinanceSettlementDashboard() {
  return request<SettlementDashboard>('/api/v1/finance/settlements')
}

export function createSettlementBatch(payload: CreateSettlementBatchInput) {
  return request<SettlementBatch>('/api/v1/finance/settlements/batches', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function approveSettlementBatch(batchId: number) {
  return request<SettlementBatch>(
    '/api/v1/finance/settlements/batches/' + batchId + '/approve',
    { method: 'POST' },
  )
}

export function rejectSettlementBatch(batchId: number) {
  return request<SettlementBatch>(
    '/api/v1/finance/settlements/batches/' + batchId + '/reject',
    { method: 'POST' },
  )
}

export function paySettlementBatch(batchId: number) {
  return request<SettlementBatch>(
    '/api/v1/finance/settlements/batches/' + batchId + '/pay',
    { method: 'POST' },
  )
}


export function getAdminAgentExitCheck(organizationId: number) {
  return request<AgentExitCheck>(
    '/api/v1/admin/agents/' + organizationId + '/exit-check',
  )
}

export function getAdminAgentExitHistory(organizationId: number) {
  return request<ListResponse<AgentExitRecord>>(
    '/api/v1/admin/agents/' + organizationId + '/exit-history',
  )
}

export function finalizeAdminAgentExit(organizationId: number) {
  return request<{ ok: boolean; check: AgentExitCheck }>(
    '/api/v1/admin/agents/' + organizationId + '/finalize-exit',
    { method: 'POST' },
  )
}


export function getAdminAgentLevels() {
  return request<{ levels: AgentLevel[]; history: AgentLevelHistory[] }>(
    '/api/v1/admin/agent-levels',
  )
}

export function createAdminAgentLevel(payload: {
  name: string
  status: string
  entry_fee_cents: number
  included_devices: number
  device_discount_bps: number
  consumer_share_bps: number
  reserve_bps: number
  settlement_cycle: string
  hold_days: number
  oem_enabled: boolean
  note: string
}) {
  return request<AgentLevel>('/api/v1/admin/agent-levels', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateAdminAgentLevel(
  levelId: number,
  payload: {
    name: string
    status: string
    entry_fee_cents: number
    included_devices: number
    device_discount_bps: number
    consumer_share_bps: number
    reserve_bps: number
    settlement_cycle: string
    hold_days: number
    oem_enabled: boolean
    note: string
  },
) {
  return request<AgentLevel>('/api/v1/admin/agent-levels/' + levelId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function assignAdminAgentLevel(
  organizationId: number,
  payload: {
    level_id: number
    effective_at: string
    reason: string
  },
) {
  return request<AgentLevelHistory>(
    '/api/v1/admin/agents/' + organizationId + '/level-assignments',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getAdminAgentContracts() {
  return request<{ items: AgentContract[] }>('/api/v1/admin/agent-contracts')
}

export function createAdminAgentContract(payload: {
  external_contract_no: string
  agent_tenant_id: number
  parent_contract_id?: number
  contract_type: string
  level_id?: number
  starts_on: string
  ends_on?: string
  contract_amount_cents: number
  note: string
}) {
  return request<AgentContract>('/api/v1/admin/agent-contracts', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateAdminAgentContract(
  contractId: number,
  payload: {
    external_contract_no: string
    agent_tenant_id: number
    parent_contract_id?: number
    contract_type: string
    level_id?: number
    starts_on: string
    ends_on?: string
    contract_amount_cents: number
    note: string
  },
) {
  return request<AgentContract>('/api/v1/admin/agent-contracts/' + contractId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function transitionAdminAgentContract(
  contractId: number,
  status: string,
) {
  return request<AgentContract>(
    '/api/v1/admin/agent-contracts/' + contractId + '/transition',
    {
      method: 'POST',
      body: JSON.stringify({ status }),
    },
  )
}

export function uploadAdminAgentContractAttachments(contractId: number, files: File[]) {
  const body = new FormData()
  files.forEach((file) => body.append('files', file))
  return request<{ items: import('./types').AgentContractAttachment[] }>(
    '/api/v1/admin/agent-contracts/' + contractId + '/attachments',
    { method: 'POST', body },
  )
}

export function deleteAdminAgentContractAttachment(contractId: number, attachmentId: number) {
  return request<void>(
    '/api/v1/admin/agent-contracts/' + contractId + '/attachments/' + attachmentId,
    { method: 'DELETE' },
  )
}
