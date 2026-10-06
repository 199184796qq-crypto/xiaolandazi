import { showPermissionToast } from './uiFeedback'
import { readPreviewStream, type PreviewStreamEvent } from './agent/previewStream'
import { liveSupportRoomForRequest } from './liveSupportAccess'
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
  RechargeRecord,
  WechatCashRefund,
  WechatRefundWallet,
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
  BeanWalletDashboard,
  BeanPurchaseOrder,
  BeanChargeQuote,
  BeanCommercialDashboard,
  BeanCommerceSettingsInput,
  BeanPricingRule,
  BeanPricingRuleInput,
  BeanFinanceDashboard,
  BeanConversionRequest,
  BeneficiaryWalletDashboard,
  WithdrawalRequest,
  Room,
  CoreRuntimeStatus,
  RoomEvent,
  RoomEventPage,
  RoomSessionStats,
  RoomStrategyStageStats,
  RoomInteractionPreferences,
  RoomInteractionPreferencesInput,
  RoomAddressingPreferences,
  RoomAddressingPreferencesInput,
  RoomHumanBehaviorProfile,
  RoomHumanBehaviorProfileInput,
  LiveReviewResponse,
  RoomBlockedUser,
  RoomBrainView,
  SpeechRuntimeSnapshot,
  SpeechMission,
  SpeechMissionListResponse,
  GeneratedSpeechHistoryPage,
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
  LiveTimeCardPage,
  LiveRuntimeEvent,
  LiveAgentSettings,
  LiveAgentSettingsInput,
  LiveAddressingStrategy,
  LiveStrategyCenterConfig,
  LiveStrategyCenterInput,
  LiveAgentPlan,
  LiveAgentPlanStyleOverlayProfile,
  LiveAnchorStyleOverlayItem,
  LiveAnchorAppliedTraining,
  AnchorStylePluginCatalog,
  LiveAgentPlanFact,
  LiveAgentPlanFactCandidate,
  AdoptLiveAgentPlanFactsOutput,
  LiveAgentPlanBenefit,
  LiveAgentPlanBenefitCandidate,
  AdoptLiveAgentPlanBenefitsOutput,
  LiveAgentPlanProductLink,
  LiveAgentPlanProductLinkCandidate,
	LiveAgentPlanProductAttribute,
  AdoptLiveAgentPlanProductLinksOutput,
  LiveAgentPlanScript,
  LiveAgentPlanScriptAnalysis,
  LiveAgentFullShowVariant,
  LiveAgentPlanScriptReference,
  LiveAgentPlanScriptAnalysisPreviewResponse,
  LiveAnchorStyle,
  LiveAnchorStyleSample,
  LiveAnchorStyleTraining,
  LiveAnchorStylePluginSetting,
  LiveAnchorStyleAnalysisQC,
  LiveAgentPlanImageRecognitionPreviewResponse,
  LiveAgentFullShowPreviewInput,
  LiveAgentFullShowPreviewResponse,
  LiveAgentFullShowAuditPreviewInput,
  LiveAgentFullShowAuditPreviewResponse,
  LiveAgentFullShowRegenerateInput,
  LiveAgentFullShowRegenerateResponse,
  LiveAgentFullShowVoiceResponse,
  LiveAgentCustomMainlineResponse,
  LiveAgentPlanTimelineSegment,
  CreateLiveAgentPlanVersionInput,
  LiveAgentPlanVersion,
  LiveAgentPlanWorkspaceResponse,
  SaveLiveAgentPlanScriptInput,
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
  AgentLearningSession,
  AgentLearningSessionDetail,
  AgentLearningTurnOutput,
  AgentMemoryItem,
  AgentMemoryVersion,
  AdoptAgentLearningOutput,
  LivePolicyVersion,
  LiveRoomPolicyContext,
  LiveSupportAuthorization,
  LiveSupportCapability,
  LiveSupportRequest,
  LiveSupportStaff,
  LiveSupportTrainingDraft,
  MediaAsset,
  VoiceProfile,
  VoiceModelBinding,
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
  SystemAgentActionPreview,
  SystemAgentChatResponse,
  SystemAgentContextResponse,
  AgentPromptConfig,
  AgentPromptConfigUpdate,
  AgentPromptHistory,
  AgentRoutingAssistResponse,
  AgentRoutingMaintenanceResponse,
  AgentUnderstandingEffectivePolicy,
  AgentUnderstandingModelDescriptor,
  AgentUnderstandingPolicy,
  AgentUnderstandingPolicyInput,
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

export async function request<T>(url: string, init?: RequestInit, onPreviewEvent?: (event: PreviewStreamEvent) => void): Promise<T> {
  const headers = new Headers(init?.headers)
  const supportRoomId = liveSupportRoomForRequest(url)
  if (supportRoomId) headers.set('X-Live-Support-Room-ID', String(supportRoomId))
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

  if (onPreviewEvent && response.headers.get('Content-Type')?.includes('text/event-stream')) {
    return readPreviewStream<T>(response, onPreviewEvent)
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

export function getAgentRoutingConfig() {
  return request<AgentRoutingMaintenanceResponse>('/api/v1/system/agent-routing')
}

export function saveAgentRoutingDraft(value: string) {
  return request<AgentRoutingMaintenanceResponse>('/api/v1/system/agent-routing/draft', {
    method: 'PUT',
    body: JSON.stringify({ value }),
  })
}

export function publishAgentRoutingConfig() {
  return request<AgentRoutingMaintenanceResponse>('/api/v1/system/agent-routing/publish', {
    method: 'POST',
  })
}

export function getAgentRoutingHistory() {
  return request<{ items: AgentPromptHistory[] }>('/api/v1/system/agent-routing/history')
}

export function rollbackAgentRoutingConfig(version: number) {
  return request<AgentRoutingMaintenanceResponse>('/api/v1/system/agent-routing/rollback', {
    method: 'POST',
    body: JSON.stringify({ version }),
  })
}

export function assistAgentRoutingConfig(instruction: string, currentJson: string) {
  return request<AgentRoutingAssistResponse>('/api/v1/system/agent-routing/assist', {
    method: 'POST',
    body: JSON.stringify({ instruction, current_json: currentJson }),
  })
}

export function getAgentUnderstandingPolicies() {
  return request<{ items: AgentUnderstandingPolicy[] }>('/api/v1/system/agent-understanding')
}

export function getEffectiveAgentUnderstandingPolicy(tenantId = 0) {
  const query = tenantId > 0 ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ policy: AgentUnderstandingEffectivePolicy }>(
    '/api/v1/system/agent-understanding/effective' + query,
  )
}

export function saveAgentUnderstandingPolicy(policy: AgentUnderstandingPolicyInput) {
  return request<{ policy: AgentUnderstandingPolicy }>('/api/v1/system/agent-understanding/policy', {
    method: 'PUT',
    body: JSON.stringify({ policy }),
  })
}

export function deleteAgentUnderstandingPolicy(scopeType: string, scopeId: number) {
  return request<{ deleted: boolean }>(
    '/api/v1/system/agent-understanding/policy/' +
      encodeURIComponent(scopeType) +
      '/' +
      encodeURIComponent(String(scopeId)),
    { method: 'DELETE' },
  )
}

export function getAgentUnderstandingModels(provider = 'qwen') {
  return request<{ provider: string; items: AgentUnderstandingModelDescriptor[] }>(
    '/api/v1/system/agent-understanding/models?provider=' + encodeURIComponent(provider),
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

export function getCoreRuntimeStatus() {
  return request<CoreRuntimeStatus>('/api/v1/runtime/core-status')
}

export function createRoom(payload: CreateRoomPayload) {
  return request<Room>('/api/v1/rooms', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function deleteRoom(roomId: number) {
  return request<void | { status: 'deletion_pending'; room_id: number; message: string }>('/api/v1/rooms/' + roomId, {
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

export function getRoomStrategyStats(roomId: number) {
  return request<RoomStrategyStageStats>('/api/v1/rooms/' + roomId + '/strategy-stats')
}

export function getRoomInteractionPreferences(roomId: number) {
  return request<RoomInteractionPreferences>('/api/v1/rooms/' + roomId + '/interaction-preferences')
}

export function updateRoomInteractionPreferences(roomId: number, payload: RoomInteractionPreferencesInput) {
  return request<RoomInteractionPreferences>('/api/v1/rooms/' + roomId + '/interaction-preferences', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function getRoomAddressingPreferences(roomId: number) {
  return request<RoomAddressingPreferences>('/api/v1/rooms/' + roomId + '/addressing-preferences')
}

export function updateRoomAddressingPreferences(roomId: number, payload: RoomAddressingPreferencesInput) {
  return request<RoomAddressingPreferences>('/api/v1/rooms/' + roomId + '/addressing-preferences', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function getRoomHumanBehaviorProfile(roomId: number) {
  return request<RoomHumanBehaviorProfile>('/api/v1/rooms/' + roomId + '/human-behavior-profile')
}

export function updateRoomHumanBehaviorProfile(roomId: number, payload: RoomHumanBehaviorProfileInput) {
  return request<RoomHumanBehaviorProfile>('/api/v1/rooms/' + roomId + '/human-behavior-profile', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function updateRoomStrategyWeight(
  roomId: number,
  payload: { category: string; key: string; value: number },
) {
  return request<{
    room_id: number
    tenant_id: number
    category: string
    key: string
    requested_value: number
    config: LiveStrategyCenterConfig
  }>('/api/v1/rooms/' + roomId + '/strategy-weight', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
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

export function getRoomSpeechMissions(roomId: number) {
  return request<SpeechMissionListResponse>('/api/v1/live/rooms/' + roomId + '/speech-missions')
}

export function getRoomSpeechMission(roomId: number, missionId: string) {
  return request<SpeechMission>(
    '/api/v1/live/rooms/' + roomId + '/speech-missions/' + encodeURIComponent(missionId),
  )
}

export function getRoomGeneratedSpeechHistory(
  roomId: number,
  options: { query?: string; page?: number; pageSize?: number; sessionId?: number } = {},
) {
  const params = new URLSearchParams()
  if (options.query?.trim()) params.set('q', options.query.trim())
  params.set('page', String(options.page || 1))
  params.set('page_size', String(options.pageSize || 10))
  if (options.sessionId) params.set('session_id', String(options.sessionId))
  return request<GeneratedSpeechHistoryPage>(
    '/api/v1/rooms/' + roomId + '/generated-speeches?' + params.toString(),
  )
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
    manual_origin?: 'agent_input' | 'agent_input_preview' | 'question_cluster' | 'test_simulation'
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

export function updateLiveAgentPlan(
  planId: number,
  payload: { name: string; description?: string; tenant_id?: number },
) {
  return request<LiveAgentPlan>('/api/v1/live-agent-plans/' + planId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function archiveLiveAgentPlan(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ ok: boolean }>('/api/v1/live-agent-plans/' + planId + '/archive' + query, {
    method: 'POST',
  })
}

export function getRoomLiveAgentPlan(roomId: number) {
  return request<{ plan?: LiveAgentPlan | null }>('/api/v1/rooms/' + roomId + '/live-agent-plan')
}

export function getRoomLiveAgentPlans(roomId: number, publishedOnly = false) {
  const query = publishedOnly ? '?published_only=1' : ''
  return request<{ items: LiveAgentPlan[] }>('/api/v1/rooms/' + roomId + '/live-agent-plans' + query)
}

export const getRoomCustomerContact = (roomId: number) => request<{ tenant_id: number; name: string; phone: string }>(`/api/v1/rooms/${roomId}/customer-contact`, { cache: 'no-store' })

export function bindRoomLiveAgentPlan(planId: number, roomId: number, tenantId?: number) {
  return request<LiveAgentPlan>('/api/v1/live-agent-plans/' + planId + '/room-bindings', {
    method: 'POST',
    body: JSON.stringify({ room_id: roomId, tenant_id: tenantId }),
  })
}

export function unbindRoomLiveAgentPlan(planId: number, roomId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ ok: boolean }>(
    '/api/v1/live-agent-plans/' + planId + '/room-bindings/' + roomId + query,
    { method: 'DELETE' },
  )
}

export function getLiveAgentPlanFacts(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAgentPlanFact[] }>('/api/v1/live-agent-plans/' + planId + '/facts' + query)
}

export function adoptLiveAgentPlanFacts(
  planId: number,
  facts: LiveAgentPlanFactCandidate[],
  tenantId?: number,
  sourceRef?: string,
) {
  return request<AdoptLiveAgentPlanFactsOutput>('/api/v1/live-agent-plans/' + planId + '/facts/adopt', {
    method: 'POST',
    body: JSON.stringify({
      tenant_id: tenantId,
      source_ref: sourceRef,
      facts,
    }),
  })
}

export function updateLiveAgentPlanFact(
  planId: number,
  factId: number,
  input: {
    expected_version_no?: number
    category: string
    key: string
    value: string
    forbidden_wording?: string
    safe_rewrite?: string
  },
  tenantId?: number,
) {
  return request<LiveAgentPlanFact>('/api/v1/live-agent-plans/' + planId + '/facts/' + factId, {
    method: 'PATCH',
    body: JSON.stringify({
      tenant_id: tenantId,
      expected_version_no: input.expected_version_no,
      category: input.category,
      key: input.key,
      value: input.value,
      forbidden_wording: input.forbidden_wording,
      safe_rewrite: input.safe_rewrite,
    }),
  })
}

export function deleteLiveAgentPlanFact(planId: number, factId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<void>('/api/v1/live-agent-plans/' + planId + '/facts/' + factId + query, {
    method: 'DELETE',
  })
}

export function getLiveAgentPlanBenefits(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAgentPlanBenefit[] }>('/api/v1/live-agent-plans/' + planId + '/benefits' + query)
}

export function adoptLiveAgentPlanBenefits(
  planId: number,
  benefits: LiveAgentPlanBenefitCandidate[],
  tenantId?: number,
  sourceRef?: string,
) {
  return request<AdoptLiveAgentPlanBenefitsOutput>('/api/v1/live-agent-plans/' + planId + '/benefits/adopt', {
    method: 'POST',
    body: JSON.stringify({
      tenant_id: tenantId,
      source_ref: sourceRef,
      benefits,
    }),
  })
}

export function updateLiveAgentPlanBenefit(
  planId: number,
  benefitId: number,
  input: {
    expected_version_no?: number
    key: string
    link_key?: string
    product_name?: string
    activity_price?: string
    gift?: string
    activity?: string
    starts_at?: string
    ends_at?: string
  },
  tenantId?: number,
) {
  return request<LiveAgentPlanBenefit>('/api/v1/live-agent-plans/' + planId + '/benefits/' + benefitId, {
    method: 'PATCH',
    body: JSON.stringify({ tenant_id: tenantId, ...input }),
  })
}

export function deleteLiveAgentPlanBenefit(
  planId: number,
  benefitId: number,
  tenantId?: number,
) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<void>('/api/v1/live-agent-plans/' + planId + '/benefits/' + benefitId + query, {
    method: 'DELETE',
  })
}

export function getLiveAgentPlanProductLinks(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAgentPlanProductLink[] }>('/api/v1/live-agent-plans/' + planId + '/product-links' + query)
}

export function adoptLiveAgentPlanProductLinks(
  planId: number,
  links: LiveAgentPlanProductLinkCandidate[],
  tenantId?: number,
  sourceRef?: string,
) {
  return request<AdoptLiveAgentPlanProductLinksOutput>('/api/v1/live-agent-plans/' + planId + '/product-links/adopt', {
    method: 'POST',
    body: JSON.stringify({
      tenant_id: tenantId,
      source_ref: sourceRef,
      links,
    }),
  })
}

export function updateLiveAgentPlanProductLink(
  planId: number,
  productLinkId: number,
  input: {
    expected_version_no?: number
    link_key: string
    product_name: string
    room_roles?: string[]
    spec?: string
    daily_price?: string
    quantity?: string
    audience?: string
  },
  tenantId?: number,
) {
  return request<LiveAgentPlanProductLink>(
    '/api/v1/live-agent-plans/' + planId + '/product-links/' + productLinkId,
    {
      method: 'PATCH',
      body: JSON.stringify({
        tenant_id: tenantId,
        ...input,
      }),
    },
  )
}

export function deleteLiveAgentPlanProductLink(
  planId: number,
  productLinkId: number,
  tenantId?: number,
) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<void>(
    '/api/v1/live-agent-plans/' + planId + '/product-links/' + productLinkId + query,
    { method: 'DELETE' },
)
}

export function createLiveAgentPlanProductAttribute(
	planId: number,
	productLinkId: number,
	input: {
		code: string
		label: string
		value: string
		unit?: string
		display_type?: string
		display_priority?: number
		source_quote?: string
	},
	tenantId?: number,
) {
	return request<LiveAgentPlanProductAttribute>(
		'/api/v1/live-agent-plans/' + planId + '/product-links/' + productLinkId + '/attributes',
		{ method: 'POST', body: JSON.stringify({ tenant_id: tenantId, ...input }) },
	)
}

export function updateLiveAgentPlanProductAttribute(
	planId: number,
	productLinkId: number,
	attributeId: number,
	input: {
		expected_version_no?: number
		code: string
		label: string
		value: string
		unit?: string
		display_type?: string
		display_priority?: number
	},
	tenantId?: number,
) {
	return request<LiveAgentPlanProductAttribute>(
		'/api/v1/live-agent-plans/' + planId + '/product-links/' + productLinkId + '/attributes/' + attributeId,
		{ method: 'PATCH', body: JSON.stringify({ tenant_id: tenantId, ...input }) },
	)
}

export function deleteLiveAgentPlanProductAttribute(
	planId: number,
	productLinkId: number,
	attributeId: number,
	tenantId?: number,
) {
	const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
	return request<void>(
		'/api/v1/live-agent-plans/' + planId + '/product-links/' + productLinkId + '/attributes/' + attributeId + query,
		{ method: 'DELETE' },
	)
}

export function getLiveAgentPlanScripts(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAgentPlanScript[] }>('/api/v1/live-agent-plans/' + planId + '/scripts' + query)
}

export function createLiveAgentPlanScript(planId: number, payload: SaveLiveAgentPlanScriptInput) {
  return request<LiveAgentPlanScript>('/api/v1/live-agent-plans/' + planId + '/scripts', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateLiveAgentPlanScript(
  planId: number,
  scriptId: number,
  payload: SaveLiveAgentPlanScriptInput,
) {
  return request<LiveAgentPlanScript>(
    '/api/v1/live-agent-plans/' + planId + '/scripts/' + scriptId,
    {
      method: 'PUT',
      body: JSON.stringify(payload),
    },
  )
}

export function getLiveAgentPlanScriptReferences(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAgentPlanScriptReference[] }>(
    '/api/v1/live-agent-plans/' + planId + '/script-references' + query,
  )
}

export function createLiveAgentPlanScriptReference(
  planId: number,
  input: {
    reference_key: string
    title: string
    content_text: string
    goal?: string
    transition?: string
    execution_mode?: string
    source_quote?: string
    source_type?: string
    source_ref?: string
  },
  tenantId?: number,
) {
  return request<LiveAgentPlanScriptReference>('/api/v1/live-agent-plans/' + planId + '/script-references', {
    method: 'POST',
    body: JSON.stringify({ tenant_id: tenantId, ...input }),
  })
}

export function updateLiveAgentPlanScriptReference(
  planId: number,
  referenceId: number,
  input: {
    expected_version_no?: number
    reference_key: string
    title: string
    content_text: string
    goal?: string
    transition?: string
    execution_mode?: string
  },
  tenantId?: number,
) {
  return request<LiveAgentPlanScriptReference>(
    '/api/v1/live-agent-plans/' + planId + '/script-references/' + referenceId,
    {
      method: 'PATCH',
      body: JSON.stringify({ tenant_id: tenantId, ...input }),
    },
  )
}

export function deleteLiveAgentPlanScriptReference(
  planId: number,
  referenceId: number,
  expectedVersionNo?: number,
  tenantId?: number,
) {
  const params = new URLSearchParams()
  if (tenantId) params.set('tenant_id', String(tenantId))
  if (expectedVersionNo) params.set('expected_version_no', String(expectedVersionNo))
  const query = params.toString() ? '?' + params.toString() : ''
  return request<void>(
    '/api/v1/live-agent-plans/' + planId + '/script-references/' + referenceId + query,
    { method: 'DELETE' },
  )
}

export function previewAnalyzeLiveAgentPlanScript(planId: number, text: string, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAgentPlanScriptAnalysisPreviewResponse>(
    '/api/v1/live-agent-plans/' + planId + '/scripts/analyze-preview' + query,
    {
      method: 'POST',
      body: JSON.stringify({ text }),
    },
  )
}

export function analyzeLiveAgentPlanScript(planId: number, scriptId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAgentPlanScript>(
    '/api/v1/live-agent-plans/' + planId + '/scripts/' + scriptId + '/analyze' + query,
    { method: 'POST' },
  )
}

export function previewRecognizeLiveAgentPlanImage(
  planId: number,
  file: File,
  tenantId?: number,
) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  const form = new FormData()
  form.append('file', file)
  return request<LiveAgentPlanImageRecognitionPreviewResponse>(
    '/api/v1/live-agent-plans/' + planId + '/scripts/recognize-image-preview' + query,
    {
      method: 'POST',
      body: form,
    },
  )
}

export function previewGenerateLiveAgentFullShow(planId: number, payload: LiveAgentFullShowPreviewInput) {
  return request<LiveAgentFullShowPreviewResponse>(
    '/api/v1/live-agent-plans/' + planId + '/full-show/preview',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function auditLiveAgentFullShowPreview(planId: number, payload: LiveAgentFullShowAuditPreviewInput) {
  return request<LiveAgentFullShowAuditPreviewResponse>(
    '/api/v1/live-agent-plans/' + planId + '/full-show/audit-preview',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function regenerateLiveAgentFullShowVariant(
  planId: number,
  variantKey: string,
  payload: LiveAgentFullShowRegenerateInput,
) {
  return request<LiveAgentFullShowRegenerateResponse>(
    '/api/v1/live-agent-plans/' +
      planId +
      '/full-show/variants/' +
      encodeURIComponent(variantKey) +
      '/regenerate-preview',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function generateLiveAgentFullShowVariantVoice(
  planId: number,
  variantKey: string,
  payload: {
    tenant_id?: number
    room_id: number
    text: string
    source: string
    voice_id?: string
    profile_id?: number
    rate?: number
  },
) {
  return request<LiveAgentFullShowVoiceResponse>(
    '/api/v1/live-agent-plans/' +
      planId +
      '/full-show/variants/' +
      encodeURIComponent(variantKey) +
      '/voice',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function rebuildLiveAgentFullShowVariantSubtitles(
  planId: number,
  variantKey: string,
  payload: {
    tenant_id?: number
    room_id: number
    audio_asset_id: number
    timeline: LiveAgentPlanTimelineSegment[]
  },
) {
  return request<LiveAgentFullShowVoiceResponse>(
    '/api/v1/live-agent-plans/' +
      planId +
      '/full-show/variants/' +
      encodeURIComponent(variantKey) +
      '/voice/subtitles',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function uploadLiveAgentCustomMainline(
  planId: number,
  roomId: number,
  file: File,
  durationMs: number,
  voiceSample?: File | null,
  onProgress?: (loaded: number, total: number) => void,
  signal?: AbortSignal,
) {
  const form = new FormData()
  form.append('file', file)
  if (voiceSample) form.append('voice_sample', voiceSample)
  form.append('room_id', String(roomId))
  if (durationMs > 0) form.append('duration_ms', String(Math.round(durationMs)))
  return new Promise<LiveAgentCustomMainlineResponse>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', '/api/v1/live-agent-plans/' + planId + '/custom-mainline/upload')
    xhr.withCredentials = true
    const supportRoomId = liveSupportRoomForRequest('/api/v1/live-agent-plans/' + planId + '/custom-mainline/upload')
    if (supportRoomId) xhr.setRequestHeader('X-Live-Support-Room-ID', String(supportRoomId))
    xhr.upload.addEventListener('progress', (event) => {
      if (!event.lengthComputable) return
      onProgress?.(event.loaded, event.total)
    })
    xhr.addEventListener('load', () => {
      let payload: LiveAgentCustomMainlineResponse | { error?: string } | null = null
      try {
        payload = xhr.responseText ? JSON.parse(xhr.responseText) : null
      } catch {
        payload = null
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve(payload as LiveAgentCustomMainlineResponse)
        return
      }
      const message = payload && 'error' in payload && payload.error
        ? payload.error
        : (xhr.status >= 500 ? '后台服务暂不可用，请稍后重试' : '上传自定义音稿失败')
      if (xhr.status === 403) showPermissionToast(message)
      reject(new Error(message))
    })
    xhr.addEventListener('error', () => reject(new Error('上传自定义音稿失败，请检查网络后重试')))
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

export function rebuildLiveAgentCustomMainline(
  planId: number,
  payload: {
    room_id: number
    audio_asset_id: number
    timeline: LiveAgentPlanTimelineSegment[]
  },
) {
  return request<LiveAgentCustomMainlineResponse>(
    '/api/v1/live-agent-plans/' + planId + '/custom-mainline/rebuild',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function createLiveAgentPlanVersion(planId: number, payload: CreateLiveAgentPlanVersionInput) {
  return request<LiveAgentPlanVersion>('/api/v1/live-agent-plans/' + planId + '/versions', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getLiveAgentPlanVersions(planId: number, roomId: number, tenantId?: number) {
  const params = new URLSearchParams({ room_id: String(roomId) })
  if (tenantId) params.set('tenant_id', String(tenantId))
  return request<{ items: LiveAgentPlanVersion[] }>(
    '/api/v1/live-agent-plans/' + planId + '/versions?' + params.toString(),
  )
}

export function getLiveAgentPlanWorkspace(planId: number, roomId: number, tenantId?: number) {
  const params = new URLSearchParams({ room_id: String(roomId) })
  if (tenantId) params.set('tenant_id', String(tenantId))
  return request<LiveAgentPlanWorkspaceResponse>(
    '/api/v1/live-agent-plans/' + planId + '/workspace?' + params.toString(),
  )
}

export function publishLiveAgentPlanVersion(
  planId: number,
  versionId: number,
  payload: { room_id: number; tenant_id?: number },
) {
  return request<LiveAgentPlanVersion>(
    '/api/v1/live-agent-plans/' + planId + '/versions/' + versionId + '/publish',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function getRoomPublishedLiveAgentPlanVersion(roomId: number) {
  return request<{ version?: LiveAgentPlanVersion | null }>(
    '/api/v1/rooms/' + roomId + '/live-agent-plan/published-version',
  )
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

export function getLiveAddressingStrategy() {
	return request<LiveAddressingStrategy>('/api/v1/live/addressing-strategy')
}

export function updateLiveAddressingStrategy(payload: LiveAddressingStrategy) {
	return request<LiveAddressingStrategy>('/api/v1/live/addressing-strategy', {
		method: 'PUT',
		body: JSON.stringify(payload),
	})
}

export function getSystemLiveStrategyCenter() {
	return request<LiveStrategyCenterConfig>('/api/v1/system/live-strategy-center')
}

export function updateSystemLiveStrategyCenter(payload: LiveStrategyCenterInput) {
	return request<LiveStrategyCenterConfig>('/api/v1/system/live-strategy-center', {
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

export function previewAnalyzeLiveAgentAnchorStyle(planId: number, text: string, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAgentPlanScriptAnalysisPreviewResponse>(
    '/api/v1/live-agent-plans/' + planId + '/anchor-style/analyze-preview' + query,
    {
      method: 'POST',
      body: JSON.stringify({ text }),
    },
  )
}

export function confirmLiveAgentPlanScriptAnalysis(planId: number, scriptId: number, sourceText: string, analysis: LiveAgentPlanScriptAnalysis, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAgentPlanScript>('/api/v1/live-agent-plans/' + planId + '/scripts/' + scriptId + '/analysis-confirm' + query, {
    method: 'POST', body: JSON.stringify({ source_text: sourceText, analysis }),
  })
}

export function testLiveAgentAnchorStyle(planId: number, input: { tenant_id?: number; room_id: number; topic: string; target_chars: number; expansion_freedom: number; conversion_intensity?: number; source_text: string; anchor_style: LiveAgentPlanScriptAnalysis['anchor_style']; selected_facts?: string[]; transient_overlays?: LiveAnchorStyleOverlayItem[]; continuation?: { completed_units: number; finish?: boolean; recent_units: Array<{ primary_fact_id?: string; support_fact_ids?: string[]; content_role?: string; fact_keys?: string[]; speech_act?: string; expression_move?: string; expression_signature?: string; text_tail?: string }> }; advisory_overrides?: string[] }, onPreviewEvent?: (event: PreviewStreamEvent) => void) {
  return request<{
    text: string
    target_chars: number
    min_chars: number
    max_chars: number
    actual_chars: number
    persisted: false
    audit: LiveAgentFullShowVariant['audit']
    style_check: { passed: boolean; checked: number; missing: string[] }
    style_coverage_warnings?: string[]
    runtime_budget?: { version: string; heat: number; target_chars: number; sentence_chars_min: number; sentence_chars_max: number; total_habit_max: number }
    runtime_evaluation?: { passed: boolean; style_score: number; lexical_score: number; rhythm_score: number; copy_containment_pct: number; longest_shared_runes: number; issues: Array<{ severity: string; code: string; message: string }> }
    style_purity?: { passed: boolean; issues: Array<{ location: string; category: string; text: string; reason: string }> }
    style_vector_evaluation?: { available: boolean; shadow_only: true; model?: string; similarity?: number; score?: number; error?: string }
    overlay_qc?: { available: boolean; passed: boolean; adherence_score: number; overuse_risk: number; issue_codes: string[]; summary: string; model?: string; latency_ms?: number; error?: string; repair_attempted: boolean }
    content_strategy?: { version: string; live_type: string; industry_code: string; plan_goal?: string; conversion_intensity: number; expansion_freedom: number; role_sequence: string[]; scheduling_mode?: string; actual_steps?: Array<{ index: number; role: string; primary_fact_id?: string; support_fact_ids?: string[]; source: string; reason: string }>; product_plan?: Array<{ link_key: string; product_name?: string; room_roles: string[]; emphasis: string; revisit: string; primary_angles?: string[]; transitions?: Array<{ target_link_key: string; reason: string }>; position_source: string; reason: string }>; layers: Array<{ layer: string; source: string; effects: string[] }> }
    advisories?: Array<{ code: string; level: string; title: string; message: string; suggestion: string; user_decidable: boolean; decision_target?: string; hard_boundary: boolean }>
    continuation?: { completed_units: number; finish?: boolean; recent_units: Array<{ primary_fact_id?: string; support_fact_ids?: string[]; content_role?: string; fact_keys?: string[]; speech_act?: string; expression_move?: string; expression_signature?: string; text_tail?: string }> }
    speech_text_only?: boolean
    applied_trainings?: LiveAnchorAppliedTraining[]
    protocol: string
    repair_attempted: boolean
    generation_mode?: 'time_driven_segments' | string
    segment_count?: number
    model?: string
    latency_ms?: number
    transient_overlay_count?: number
  }>(
    '/api/v1/live-agent-plans/' + planId + '/anchor-style/test', { method: 'POST', body: JSON.stringify(input), headers: onPreviewEvent ? { Accept: 'text/event-stream' } : undefined }, onPreviewEvent,
  )
}

export function getLiveAgentPlanStyleOverlays(planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAgentPlanStyleOverlayProfile>('/api/v1/live-agent-plans/' + planId + '/style-overlays' + query, { cache: 'no-store' })
}

export function interpretLiveAgentPlanStyleOverlay(planId: number, input: { source_text: string; explanation_text?: string; strength: number }, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ item: LiveAnchorStyleOverlayItem; provider: string; model: string; latency_ms: number; memory_matches: number }>(
    '/api/v1/live-agent-plans/' + planId + '/style-overlays/interpret' + query,
    { method: 'POST', body: JSON.stringify(input) },
  )
}

export function learnLiveAgentPlanStyleOverlay(
  planId: number,
  input: { sample_text: string; generated_text: string; feedback_text: string; strength: number },
  tenantId?: number,
) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ item: LiveAnchorStyleOverlayItem; diagnosis: string; provider: string; model: string; latency_ms: number; requires_test: true; auto_saved: false }>(
    '/api/v1/live-agent-plans/' + planId + '/style-overlays/learn' + query,
    { method: 'POST', body: JSON.stringify(input) },
  )
}

export function saveLiveAgentPlanStyleOverlays(planId: number, profile: LiveAgentPlanStyleOverlayProfile, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ profile: LiveAgentPlanStyleOverlayProfile; semantic_indexed: boolean }>(
    '/api/v1/live-agent-plans/' + planId + '/style-overlays' + query,
    { method: 'PUT', body: JSON.stringify({ expected_revision: profile.revision, items: profile.items }) },
  )
}

export function getAnchorStylePluginCatalog() {
  return request<AnchorStylePluginCatalog>('/api/v1/live/style-plugins', { cache: 'no-store' })
}

export function listLiveAnchorStyles(tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAnchorStyle[] }>('/api/v1/live-anchor-styles' + query, { cache: 'no-store' })
}

export function createLiveAnchorStyle(input: { name: string; description?: string; tenant_id?: number }) {
  return request<LiveAnchorStyle>('/api/v1/live-anchor-styles', { method: 'POST', body: JSON.stringify(input) })
}

export function getLiveAnchorStyle(styleId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAnchorStyle>('/api/v1/live-anchor-styles/' + styleId + query, { cache: 'no-store' })
}

export function updateLiveAnchorStyle(styleId: number, input: { name: string; description?: string; profile?: LiveAgentPlanScriptAnalysis['anchor_style'] }, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAnchorStyle>('/api/v1/live-anchor-styles/' + styleId + query, { method: 'PUT', body: JSON.stringify(input) })
}

export function deleteLiveAnchorStyle(styleId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ deleted: boolean }>('/api/v1/live-anchor-styles/' + styleId + query, { method: 'DELETE' })
}

export function listLiveAnchorStyleSamples(styleId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAnchorStyleSample[] }>('/api/v1/live-anchor-styles/' + styleId + '/samples' + query, { cache: 'no-store' })
}

export function createLiveAnchorStyleSample(styleId: number, input: { title: string; source_type?: string; original_name?: string; raw_text: string; readable_text?: string }, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAnchorStyleSample>('/api/v1/live-anchor-styles/' + styleId + '/samples' + query, { method: 'POST', body: JSON.stringify(input) })
}

export function deleteLiveAnchorStyleSample(styleId: number, sampleId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ deleted: boolean }>('/api/v1/live-anchor-styles/' + styleId + '/samples/' + sampleId + query, { method: 'DELETE' })
}

export function analyzeLiveAnchorStyleSample(styleId: number, sampleId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ sample: LiveAnchorStyleSample; style: LiveAnchorStyle; analysis: LiveAgentPlanScriptAnalysis['anchor_style']; style_qc?: LiveAnchorStyleAnalysisQC; provider?: string; model?: string; latency_ms?: number }>(
    '/api/v1/live-anchor-styles/' + styleId + '/samples/' + sampleId + '/analyze' + query,
    { method: 'POST' },
  )
}

export function listLiveAnchorStyleTrainings(styleId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAnchorStyleTraining[] }>('/api/v1/live-anchor-styles/' + styleId + '/trainings' + query, { cache: 'no-store' })
}

export function saveLiveAnchorStyleTraining(styleId: number, input: Partial<LiveAnchorStyleTraining> & { request_text: string }, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAnchorStyleTraining>('/api/v1/live-anchor-styles/' + styleId + '/trainings' + query, { method: 'POST', body: JSON.stringify(input) })
}

export function updateLiveAnchorStyleTraining(styleId: number, trainingId: number, input: Partial<LiveAnchorStyleTraining> & { request_text: string }, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<LiveAnchorStyleTraining>('/api/v1/live-anchor-styles/' + styleId + '/trainings/' + trainingId + query, { method: 'PUT', body: JSON.stringify(input) })
}

export function deleteLiveAnchorStyleTraining(styleId: number, trainingId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ deleted: boolean }>('/api/v1/live-anchor-styles/' + styleId + '/trainings/' + trainingId + query, { method: 'DELETE' })
}

export function upsertLiveAnchorStylePlugin(styleId: number, input: LiveAnchorStylePluginSetting, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ items: LiveAnchorStylePluginSetting[] }>('/api/v1/live-anchor-styles/' + styleId + '/plugins' + query, { method: 'PUT', body: JSON.stringify(input) })
}

export function deleteLiveAnchorStylePlugin(styleId: number, pluginId: string, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ deleted: boolean }>('/api/v1/live-anchor-styles/' + styleId + '/plugins/' + encodeURIComponent(pluginId) + query, { method: 'DELETE' })
}

export function bindLiveAnchorStyleToPlan(styleId: number, planId: number, tenantId?: number) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ bound: boolean; style_id: number; plan_id: number }>('/api/v1/live-anchor-styles/' + styleId + '/bind-plan' + query, { method: 'POST', body: JSON.stringify({ plan_id: planId }) })
}

export function activateLiveAgentPlanStylePlugin(
  planId: number,
  input: { expected_revision: number; plugin_id: string; plugin_version: string; strength: number; parameters?: Record<string, unknown> },
  tenantId?: number,
) {
  const query = tenantId ? '?tenant_id=' + encodeURIComponent(String(tenantId)) : ''
  return request<{ profile: LiveAgentPlanStyleOverlayProfile; semantic_indexed: boolean; item: LiveAnchorStyleOverlayItem }>(
    '/api/v1/live-agent-plans/' + planId + '/style-plugins/activate' + query,
    { method: 'POST', body: JSON.stringify(input) },
  )
}

export function getLiveVoiceProfileQuota() {
  return request<{ limit: number; used: number; remaining: number }>('/api/v1/live/voice-profiles/quota')
}

export function deleteLiveVoiceProfile(profileId: number) {
  return request<void>('/api/v1/live/voice-profiles/' + profileId, { method: 'DELETE' })
}

export function getLiveVoiceModelBindings(profileId?: number) {
  const query = profileId ? '?profile_id=' + encodeURIComponent(String(profileId)) : ''
  return request<VoiceModelBinding[]>('/api/v1/live/voice-model-bindings' + query)
}

export function cloneLiveVoiceModelBinding(profileId: number, payload: { model: string }) {
  return request<VoiceModelBinding>('/api/v1/live/voice-profiles/' + profileId + '/bindings/clone', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function previewLiveVoiceModelBinding(bindingId: number, text = '') {
  return request<VoicePreviewResponse>('/api/v1/live/voice-model-bindings/' + bindingId + '/preview', {
    method: 'POST',
    body: JSON.stringify({ text }),
  })
}

export function publishLiveAgentVoiceBinding(roomId: number, bindingId: number) {
  return request<LiveAgentPlanVersion>(
    '/api/v1/rooms/' + roomId + '/live-agent-plan/published-version/voice-binding',
    {
      method: 'POST',
      body: JSON.stringify({ binding_id: bindingId }),
    },
  )
}

export function publishLiveAgentVoiceRate(roomId: number, rate: number) {
  return request<LiveAgentPlanVersion>(
    '/api/v1/rooms/' + roomId + '/live-agent-plan/published-version/voice-rate',
    {
      method: 'POST',
      body: JSON.stringify({ rate }),
    },
  )
}

export function publishLiveAgentEmotion(roomId: number, enabled: boolean) {
  return request<LiveAgentPlanVersion>(
    '/api/v1/rooms/' + roomId + '/live-agent-plan/published-version/emotion',
    {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    },
  )
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

export function previewLiveOfficialVoice(voiceId: string, text = '', tenantId?: number) {
  return request<VoicePreviewResponse>(
    '/api/v1/live/official-voices/' + encodeURIComponent(voiceId) + '/preview',
    { method: 'POST', body: JSON.stringify({ text, tenant_id: tenantId }) },
  )
}

export function cloneLiveVoiceProfile(payload: {
  name: string
  sample_asset_id: number
  model?: string
  avatar_key?: string
}) {
  return request<VoiceProfile>('/api/v1/live/voice-profiles/clone', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function previewLiveVoiceProfile(profileId: number, text = '', tenantId?: number) {
  return request<VoicePreviewResponse>(
    '/api/v1/live/voice-profiles/' + profileId + '/preview',
    { method: 'POST', body: JSON.stringify({ text, tenant_id: tenantId }) },
  )
}

export function chatLiveAgent(
  roomId: number,
  payload: {
    message: string
    anchor_transcript?: string
    history?: Array<{ role: 'user' | 'agent'; text: string }>
    image_urls?: string[]
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

export interface LiveStrategyIntentResponse {
  protocol_version?: string
  state?: 'responded' | 'permission_denied'
  code?: string
  required_permission?: string
  kind: 'chat' | 'command' | 'clarify'
  intent:
    | 'chat'
    | 'product.add'
    | 'product.update'
    | 'product.disable'
	  | 'product_attribute.add'
	  | 'product_attribute.update'
	  | 'product_attribute.disable'
    | 'benefit.add'
    | 'benefit.update'
    | 'benefit.disable'
    | 'fact.add'
    | 'fact.update'
    | 'fact.disable'
    | 'script.add'
    | 'script.update'
    | 'script.disable'
    | 'plan.bind'
    | 'plan.unbind'
    | 'plan.switch'
    | 'unknown'
  reply?: string
  target?: {
    link_key?: string
	product_attribute_id?: number
	attribute_code?: string
	attribute_label?: string
    benefit_key?: string
    fact_category?: string
    fact_key?: string
    script_reference_key?: string
    script_title?: string
    plan_id?: number
    plan_name?: string
  }
  changes?: {
    product_name?: string
    room_roles?: string[]
    spec?: string
    daily_price?: string
    quantity?: string
    audience?: string
	attribute_code?: string
	attribute_label?: string
	attribute_value?: string
	attribute_unit?: string
	attribute_display_type?: string
	attribute_display_priority?: number
    activity_price?: string
    gift?: string
    activity?: string
    starts_at?: string
    ends_at?: string
    fact_value?: string
    script_text?: string
  }
  missing?: string[]
  confidence?: number
  provider?: string
  model?: string
  latency_ms?: number
  engine?: 'program' | 'model' | 'program_fallback' | string
  policy_source?: string
}

export function interpretLiveStrategyIntent(
  roomId: number,
  payload: {
    message: string
    plan_id?: number
    current_mode?: string
    history?: Array<{ role: 'user' | 'agent'; text: string }>
    image_urls?: string[]
  },
) {
  return request<LiveStrategyIntentResponse>('/api/v1/live/rooms/' + roomId + '/agent/interpret', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function executeLiveStrategyAction(
  roomId: number,
  action: SystemAgentActionPreview,
) {
  return request<SystemAgentChatResponse>('/api/v1/live/rooms/' + roomId + '/agent/actions/execute', {
    method: 'POST',
    body: JSON.stringify({
      action: {
        type: action.type,
        payload: action.payload,
      },
    }),
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
  image_urls?: string[]
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
    image_urls?: string[]
  },
) {
  return request<LivePolicyAgentResponse>('/api/v1/live/rooms/' + roomId + '/policy-agent/chat', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function createAgentLearningSession(
  roomId: number,
  payload: {
    source_type?: string
    source_ref?: string
    question?: string
    original_reply?: string
    target?: string
  },
) {
  return request<AgentLearningSession>('/api/v1/live/rooms/' + roomId + '/agent-learning/sessions', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function getAgentLearningSessions(roomId: number) {
  return request<{ items: AgentLearningSession[] }>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/sessions',
  )
}

export function getAgentLearningSession(roomId: number, sessionId: number) {
  return request<AgentLearningSessionDetail>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/sessions/' + sessionId,
  )
}

export function createAgentLearningTurn(roomId: number, sessionId: number, feedback: string) {
  return request<AgentLearningTurnOutput>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/sessions/' + sessionId + '/turns',
    {
      method: 'POST',
      body: JSON.stringify({ feedback }),
    },
  )
}

export function testAgentLearningSession(roomId: number, sessionId: number, question = '') {
  return request<AgentDecisionSimulationResult>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/sessions/' + sessionId + '/test',
    {
      method: 'POST',
      body: JSON.stringify(question.trim() ? { question: question.trim() } : {}),
    },
  )
}

export function classifyAgentLearningMessage(
  roomId: number,
  payload: {
    message: string
    session_id?: number
    target?: string
    latest_candidate?: string
    current_mode?: 'chat' | 'learning' | 'test' | 'execution'
    learning_active?: boolean
    test_active?: boolean
    execution_active?: boolean
    history?: Array<{ role: 'user' | 'agent'; text: string }>
  },
) {
  return request<{ intent: 'chat' | 'learning' | 'test' | 'execution' | 'adopt'; confidence?: string; reason?: string }>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/intent',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function chatAgentLearningCompanion(
  roomId: number,
  payload: {
    message: string
    session_id?: number
    target?: string
    latest_candidate?: string
    history?: Array<{ role: 'user' | 'agent'; text: string }>
  },
) {
  return request<{ reply: string; kind: string; model?: string; latency_ms?: number }>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/chat',
    {
      method: 'POST',
      body: JSON.stringify(payload),
    },
  )
}

export function adoptAgentLearningSession(roomId: number, sessionId: number) {
  return request<AdoptAgentLearningOutput>(
    '/api/v1/live/rooms/' + roomId + '/agent-learning/sessions/' + sessionId + '/adopt',
    { method: 'POST' },
  )
}

export function getAgentMemories(roomId: number) {
  return request<{ items: AgentMemoryItem[] }>(
    '/api/v1/live/rooms/' + roomId + '/agent-memories',
  )
}

export function deactivateAgentMemory(roomId: number, memoryId: number) {
  return request<AgentMemoryItem>(
    '/api/v1/live/rooms/' + roomId + '/agent-memories/' + memoryId + '/deactivate',
    { method: 'POST' },
  )
}

export function getAgentMemoryVersions(roomId: number, memoryId: number) {
  return request<{ items: AgentMemoryVersion[] }>(
    '/api/v1/live/rooms/' + roomId + '/agent-memories/' + memoryId + '/versions',
  )
}

export function rollbackAgentMemoryVersion(roomId: number, memoryId: number, versionId: number) {
  return request<AgentMemoryItem>(
    '/api/v1/live/rooms/' + roomId + '/agent-memories/' + memoryId + '/versions/' + versionId + '/rollback',
    { method: 'POST' },
  )
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

export function getLiveTimeCards(page = 1, pageSize = 5) {
  const params = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
  })
  return request<LiveTimeCardPage>('/api/v1/live/time-cards?' + params.toString())
}

export function activateLiveTimeCard(assetId: number) {
  return request<{ asset_id: number; quota: LiveQuotaSummary }>(
    '/api/v1/live/time-cards/' + assetId + '/activate',
    { method: 'POST' },
  )
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

export interface UserUIPreferences {
  user_id: number
  selected_live_room_id?: number | null
  sidebar_collapsed: boolean
  live_plan_panel_collapsed: boolean
  agent_drawer_collapsed: boolean
}

export function getUserUIPreferences() {
  return request<UserUIPreferences>('/api/v1/account/ui-preferences')
}

export function updateUserUIPreferences(payload: Partial<Pick<
  UserUIPreferences,
  'selected_live_room_id' | 'sidebar_collapsed' | 'live_plan_panel_collapsed' | 'agent_drawer_collapsed'
>>) {
  return request<UserUIPreferences>('/api/v1/account/ui-preferences', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
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

export function getCustomerBeanWallet() {
  return request<BeanWalletDashboard>('/api/v1/finance/beans')
}

export function purchaseCustomerBeans(amountCents: number, idempotencyKey: string) {
  return request<BeanPurchaseOrder>('/api/v1/finance/beans/purchases', {
    method: 'POST',
    body: JSON.stringify({ amount_cents: amountCents, idempotency_key: idempotencyKey }),
  })
}

export function quoteCustomerBeans(actionCode: string, units: number) {
  return request<BeanChargeQuote>('/api/v1/finance/beans/quote', {
    method: 'POST',
    body: JSON.stringify({ action_code: actionCode, units }),
  })
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
  source?: string
  object_type?: string
  room_id?: number
  tenant_id?: number
  from?: string
  to?: string
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
  if (params.source) query.set('source', params.source)
  if (params.object_type) query.set('object_type', params.object_type)
  if (params.room_id) query.set('room_id', String(params.room_id))
  if (params.tenant_id) query.set('tenant_id', String(params.tenant_id))
  if (params.from) query.set('from', params.from)
  if (params.to) query.set('to', params.to)
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
  image_urls?: string[]
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

export function executeInternalAgentAction(action: SystemAgentActionPreview) {
  return request<SystemAgentChatResponse>('/api/v1/internal-agent/actions/execute', {
    method: 'POST',
    body: JSON.stringify({ action }),
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

export const getWechatRefundWallet = () => request<WechatRefundWallet>('/api/v1/wallet/wechat-refunds')
export const getFinanceWechatRefunds = () => request<{ items: WechatCashRefund[] }>('/api/v1/finance/wechat-refunds')
export const queryFinanceWechatRefund = (id: number) => request<WechatCashRefund>(`/api/v1/finance/wechat-refunds/${id}/query`, { method: 'POST', body: '{}' })
export const createWechatCashRefund = (amountCents: number, idempotencyKey: string) => request<WechatCashRefund>('/api/v1/wallet/wechat-refunds', { method: 'POST', body: JSON.stringify({ amount_cents: amountCents, idempotency_key: idempotencyKey }) })
export const queryWechatCashRefund = (id: number) => request<WechatCashRefund>(`/api/v1/wallet/wechat-refunds/${id}/query`, { method: 'POST', body: '{}' })
export const createWechatRecharge = (amountCents: number, idempotencyKey: string) =>
  request<RechargeRecord>('/api/v1/wallet/recharges', { method: 'POST', body: JSON.stringify({ amount_cents: amountCents, idempotency_key: idempotencyKey, payment_method: 'wechat_native' }) })
export const getWechatRecharge = (id: number) => request<RechargeRecord>(`/api/v1/wallet/recharges/${id}`)
export const prepayWechatNativeRecharge = (id: number) => request<{ recharge: RechargeRecord; payment_no?: string; code_url?: string; qr_code_data_url?: string; expires_at?: string }>(`/api/v1/wallet/recharges/${id}/wechat-native-prepay`, { method: 'POST', body: '{}' })
export const queryWechatRecharge = (id: number) =>
  request<{ recharge: RechargeRecord; trade_state: string }>(`/api/v1/wallet/recharges/${id}/wechat-query`, { method: 'POST', body: '{}' })

export function getCommercialBeans() {
  return request<BeanCommercialDashboard>('/api/v1/commercial/beans')
}

export function updateCommercialBeanSettings(payload: BeanCommerceSettingsInput) {
  return request<BeanCommercialDashboard['settings']>('/api/v1/commercial/beans/settings', {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function createCommercialBeanRule(payload: BeanPricingRuleInput) {
  return request<BeanPricingRule>('/api/v1/commercial/beans/rules', {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

export function updateCommercialBeanRule(ruleId: number, payload: BeanPricingRuleInput) {
  return request<BeanPricingRule>('/api/v1/commercial/beans/rules/' + ruleId, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

export function getStaffBeanWallet() {
  return request<BeanWalletDashboard>('/api/v1/staff/beans/me')
}

export function createStaffBeanConversion(beanAmount: number) {
  return request<BeanConversionRequest>('/api/v1/staff/beans/conversions', {
    method: 'POST',
    body: JSON.stringify({ bean_amount: beanAmount }),
  })
}

export function getFinanceBeans() {
  return request<BeanFinanceDashboard>('/api/v1/finance/beans/overview')
}

export function transitionBeanConversion(
  conversionId: number,
  action: 'approve' | 'reject' | 'pay',
  reason = '',
) {
  return request<BeanConversionRequest>(
    '/api/v1/finance/beans/conversions/' + conversionId + '/' + action,
    { method: 'POST', body: JSON.stringify({ reason }) },
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

export function walletPayCustomerTimeCardOrder(orderId: number, idempotencyKey: string) {
  return request<{ order: CustomerShopOrder; payment_method: string }>(
    '/api/v1/shop/orders/' + orderId + '/wallet-pay',
    {
      method: 'POST',
      body: JSON.stringify({ idempotency_key: idempotencyKey }),
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

export interface InventoryStockProduct {
  product_id: number; name: string; sku_code: string
  total: number; available: number; transit: number; repair: number; scrap: number; delivered: number
}
export interface InventoryStockPage<T> { items: T[]; total: number; page: number; page_size: number }
export function getInventoryStockProducts(q = '', page = 1, page_size = 12) {
  return request<InventoryStockPage<InventoryStockProduct>>('/api/v1/inventory/stock-products?' + new URLSearchParams({ q, page: String(page), page_size: String(page_size) }))
}
export function getInventoryStockDevices(sku_code: string, q = '', status = 'all', page = 1, page_size = 20) {
  return request<InventoryStockPage<InventoryDevice>>('/api/v1/inventory/stock-devices?' + new URLSearchParams({ sku_code, q, status, page: String(page), page_size: String(page_size) }))
}
export function checkInventoryBatch(sku_code: string, batch_no: string) {
  return request<{ exists: boolean; batch_no: string }>('/api/v1/inventory/batches/check?' + new URLSearchParams({ sku_code, batch_no }))
}

export const getDefaultDeviceName = () => request<{ device_name: string }>('/api/v1/live/devices/default-name')
export const claimLiveDevice = (binding_code: string, device_name: string) => request<LiveDevice>('/api/v1/live/devices/claim', { method: 'POST', body: JSON.stringify({ binding_code, device_name }) })
export const renameLiveDevice = (id: number, device_name: string) => request<LiveDevice>('/api/v1/live/devices/' + id, { method: 'PATCH', body: JSON.stringify({ device_name }) })
export const unbindLiveDevice = (id: number) => request<LiveDevice>('/api/v1/live/devices/' + id + '/bind', { method: 'DELETE' })
export type DeviceAddressingMode = 'auto' | 'female' | 'male' | 'child' | 'neutral'
export const getDeviceAddressing = (id: number) => request<{ mode: DeviceAddressingMode }>('/api/v1/live/devices/' + id + '/addressing')
export const saveDeviceAddressing = (id: number, mode: DeviceAddressingMode) => request<{ mode: DeviceAddressingMode }>('/api/v1/live/devices/' + id + '/addressing', { method: 'PUT', body: JSON.stringify({ mode }) })
export const configureDeviceHardware = (id: number, hardware_mac: string, claim_enabled: boolean, reason: string) => request<{ ok: boolean }>('/api/v1/inventory/devices/' + id + '/hardware', { method: 'PUT', body: JSON.stringify({ hardware_mac, claim_enabled, reason }) })
export const releaseDeviceOwnership = (id: number, reason: string) => request<{ ok: boolean }>('/api/v1/inventory/devices/' + id + '/release-ownership', { method: 'POST', body: JSON.stringify({ reason }) })

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
