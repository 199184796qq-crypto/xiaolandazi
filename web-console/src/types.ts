export interface StaffFinanceCustomerSummary {
  tenant_id: number
  user_id: number
  username: string
  display_name: string
  phone: string
  parent_org_name: string
  parent_org_type: string
  cash_balance_cents: number
  reward_balance_cents: number
}

export interface StaffFinanceTaskSummary {
  id: number
  policy_id?: number
  operation_code: string
  requester_user_id: number
  requester_name: string
  approver_user_id?: number
  approver_name?: string
  tenant_id: number
  customer_name: string
  target_type: string
  amount_yuan: number
  resource_seconds?: number
  status: string
  reason: string
  approver_role_code: string
  created_at: string
  decided_at?: string
}

export interface StaffFinanceOperationResult {
  task: StaffFinanceTaskSummary
  requires_approval: boolean
  applied: boolean
}

export interface StaffFinanceOverview {
  customers: StaffFinanceCustomerSummary[]
  tasks: StaffFinanceTaskSummary[]
}
export interface StaffAccessContext {
  is_super_admin: boolean
  employee_id?: number
  primary_group_id?: number
  primary_group_code?: string
  primary_group_name?: string
  role_codes: string[]
  permissions: string[]
  permission_scopes: Record<string, string>
  permission_group_ids: Record<string, number[]>
  group_ids: number[]
  managed_group_ids: number[]
}

export interface StaffGroupSummary {
  id: number
  code: string
  name: string
  description: string
  status: string
  sort_order: number
  system_managed: boolean
  member_count: number
  manager_count: number
  created_at: string
}

export interface StaffPermissionSummary {
  id: number
  code: string
  module: string
  action: string
  description: string
}

export interface StaffRoleSummary {
  id: number
  group_id: number
  group_code: string
  group_name: string
  code: string
  name: string
  description: string
  is_group_manager: boolean
  default_scope_type: string
  status: string
  permissions: StaffPermissionSummary[]
  permission_codes: string[]
  created_at: string
}

export interface StaffEmployeeRoleSummary {
  role_id: number
  group_id: number
  group_code: string
  group_name: string
  code: string
  name: string
  scope_type: string
  is_group_manager: boolean
}

export interface StaffEmployeeGroupSummary {
  group_id: number
  group_code: string
  group_name: string
  is_primary: boolean
}

export interface StaffEmployeeSummary {
  id: number
  user_id: number
  employee_no: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  primary_group_id: number
  primary_group_code: string
  primary_group_name: string
  employment_status: string
  user_status: string
  roles: StaffEmployeeRoleSummary[]
  groups: StaffEmployeeGroupSummary[]
  created_at: string
}

export interface StaffApprovalPolicySummary {
  id: number
  code: string
  name: string
  operation_code: string
  mode: string
  threshold_amount: number
  approver_role_code: string
  status: string
}

export interface StaffDashboard {
  access: StaffAccessContext
  groups: StaffGroupSummary[]
  roles: StaffRoleSummary[]
  permissions: StaffPermissionSummary[]
  employees: StaffEmployeeSummary[]
  approval_policies: StaffApprovalPolicySummary[]
}

export interface StaffPermissionCenterDashboard {
  access: StaffAccessContext
  groups: StaffGroupSummary[]
  roles: StaffRoleSummary[]
  permissions: StaffPermissionSummary[]
  employees: StaffEmployeeSummary[]
}
export interface InitialCredential {
  initial_password: string
  login_url: string
  delivery_method: 'copy' | 'email' | string
  email?: string
  email_sent: boolean
  email_error?: string
}

export interface SystemAgentCreateEmployeePayload {
  employee_no: string
  primary_group_id: number
  primary_group_name: string
  role_ids: number[]
  role_names: string[]
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  delivery_method: 'copy' | 'email' | string
}

export interface SystemAgentActionPayload {
  employee_no?: string
  primary_group_id?: number
  primary_group_name?: string
  role_ids?: number[]
  role_names?: string[]
  username?: string
  display_name?: string
  phone?: string
  email?: string
  province?: string
  city?: string
  district?: string
  delivery_method?: 'copy' | 'email' | string

  code?: string
  name?: string
  description?: string
  status?: 'active' | 'inactive' | 'draft' | string
  sort_order?: number
  pricing_rule?: 'floor_yuan' | string
  starts_at?: string
  ends_at?: string
  items?: MarketingCampaignItem[]
  display_locations?: string[]
}

export interface SystemAgentActionPreview {
  type: string
  title: string
  summary: string
  risk_level: string
  requires_confirmation: boolean
  payload: SystemAgentActionPayload
}

export interface SystemAgentDepartment {
  name: string
  code: string
}

export interface SystemAgentContextResponse {
  capabilities: string[]
  departments: SystemAgentDepartment[]
}

export interface SystemAgentNavigateTarget {
  title: string
  to: string
  section?: string
}

export interface SystemAgentChatResponse {
  reply: string
  action?: SystemAgentActionPreview
  navigate?: SystemAgentNavigateTarget
  capabilities: string[]
  model?: string
  latency_ms?: number
}
export interface Actor {
  user_id: number
  username: string
  role: string
  must_change_password: boolean
  phone: string
  province: string
  city: string
  district: string
  tenant_id?: number
  display_name: string
  avatar_url?: string
}
export interface Tenant {
  id: number
  code: string
  name: string
  status: string
  created_at: string
}

export interface Bootstrap {
  actor: Actor
  staff_access?: StaffAccessContext | null
  tenants: Tenant[]
  environment: string
}

export interface SystemSetting {
  key: string
  group: string
  label: string
  value: string
  input_type: 'text' | 'url' | 'boolean' | string
  sort_order: number
  updated_by_user_id: number
  updated_at: string
}

export interface SystemSettingUpdate {
  key: string
  value: string
}

export interface SystemDictionaryItem {
  id: number
  category: string
  code: string
  label: string
  description: string
  sort_order: number
  enabled: boolean
  system_seeded: boolean
  created_at: string
  updated_at: string
}

export interface SystemDictionaryItemInput {
  category: string
  code: string
  label: string
  description: string
  sort_order: number
  enabled: boolean
}

export interface SystemWarehouseInput {
  code: string
  name: string
  status: 'active' | 'inactive' | string
}

export interface MembershipRoomLimitSetting {
  plan_id: number
  plan_code: string
  plan_name: string
  plan_status: string
  room_limit: number
}

export interface MembershipRoomLimitUpdate {
  plan_id: number
  room_limit: number
}

export interface AgentPromptConfig {
  key: string
  name: string
  description: string
  scene: string
  default_value: string
  current_value: string
  draft_value: string
  enabled: boolean
  draft_enabled: boolean
  version: number
  updated_by_user_id: number
  updated_at: string
}

export interface AgentPromptConfigUpdate {
  key: string
  current_value: string
  enabled: boolean
}

export interface AgentPromptHistory {
  key: string
  version: number
  value: string
  enabled: boolean
  operation: string
  updated_by_user_id: number
  created_at: string
}

export interface SystemSettingsDashboard {
  settings: SystemSetting[]
  dictionaries: Record<string, SystemDictionaryItem[]>
  warehouses: InventoryWarehouse[]
  membership_room_limits: MembershipRoomLimitSetting[]
  agent_prompt_configs: AgentPromptConfig[]
}

export interface PublicSystemConfig {
  site_name: string
  internal_agent_name: string
  client_agent_name: string
  live_policy_rule_title_font_size: number
  live_policy_rule_body_font_size: number
  live_policy_rule_meta_font_size: number
  live_policy_test_title_font_size: number
  live_policy_test_body_font_size: number
  live_policy_test_meta_font_size: number
  footer_enabled: boolean
  footer_copyright: string
  footer_icp_text: string
  footer_icp_url: string
  footer_police_text: string
  footer_police_url: string
  footer_report_text: string
  footer_report_url: string
  footer_extra_text: string
}

export interface Room {
  id: number
  tenant_id: number
  platform: string
  external_room_id: string
  source_url?: string
  name: string
  status: 'pending' | 'connecting' | 'live' | 'offline' | 'error' | string
  cooperation_status?: 'cooperating' | 'non_cooperating' | string
  cooperation_note?: string
  cooperation_marked_at?: string
  cooperation_marked_by_user_id?: number
  last_recharge_at?: string
  recharge_dormant_90_days?: boolean
  collector_mode: string
  monitor_enabled?: boolean
  monitor_started_at?: string
  device_online?: boolean
  online_count: number
  last_event_at?: string
  created_at: string
  updated_at: string
}

export interface RoomEvent {
  id: number
  tenant_id: number
  room_id: number
  event_type: string
  user_id?: string
  nickname?: string
  content?: string
  occurred_at: string
  payload?: unknown
}

export interface RoomEventPage {
  items: RoomEvent[]
  has_more: boolean
  next_before_id: number
}

export interface RoomSessionStats {
  started_at?: string
  ended_at?: string
  resume_pending: boolean
  reopened_at?: string
  interrupted_seconds: number
  live_seconds: number
  event_count: number
  entries: number
  chats: number
  likes: number
  follows: number
  gifts: number
  order_signals: number
}

export interface LiveReviewQuestionGroup {
  text: string
  count: number
  last_at: string
  nicknames?: string[]
}

export interface LiveReviewSummary {
  room_id: number
  started_at: string
  ended_at?: string
  active_seconds: number
  interrupted_seconds: number
  event_count: number
  entries: number
  chats: number
  likes: number
  follows: number
  gifts: number
  order_signals: number
  question_groups: LiveReviewQuestionGroup[]
}

export interface LiveReviewEvent {
  id: number
  event_id: number
  tenant_id: number
  room_id: number
  event_type: string
  user_id?: string
  nickname?: string
  content?: string
  payload_json?: string
  occurred_at: string
}

export interface LiveReviewResponse {
  summary: LiveReviewSummary
  events: LiveReviewEvent[]
}

export interface RoomBlockedUser {
  id: number
  tenant_id: number
  room_id: number
  subject_key: string
  user_id?: string
  nickname?: string
  reason?: string
  blocked_at: string
}

export interface RoomBrainQuestion {
  EventID: number
  UserID: string
  Nickname: string
  Content: string
  OccurredAt: string
}

export interface RoomBrainTopic {
  Topic: string
  Count: number
  UniqueUsers: number
  LastSeenAt: string
  LastAnsweredAt: string
  SampleQuestions: string[]
  Questions: RoomBrainQuestion[]
  TTSQuestions: RoomBrainQuestion[]
  TTSEligibleCount: number
  ArchivedQuestionCount: number
}

export interface RoomBrainIntelligence {
  Heat: string
  OnlineCount: number
  Entries30s: number
  Entries60s: number
  Chats30s: number
  Likes30s: number
  Follows30s: number
  Orders30s: number
  OrderSignals30s: number
  OrderSignals60s: number
  OrderSignalSamples: string[]
  SessionEntries: number
  SessionChats: number
  SessionLikes: number
  SessionFollows: number
  SessionGifts: number
  UniqueChatters30s: number
  QuestionCount30s: number
  NegativeFeedback30s: number
  QuestionPressure: number
  AudienceTurnover5m: number
  PreferAggregateQNA: boolean
  PreferOneToOneQNA: boolean
  TopTopics: RoomBrainTopic[]
}

export interface RoomBrainView {
  RoomID: number
  GeneratedAt: string
  Intelligence: RoomBrainIntelligence
}

export interface SpeechTrackRuntime {
  status: string
  text?: string
  question_text?: string
  reply_text?: string
  source?: string
  audio_url?: string
  decision_id?: string
  speech_task_id?: string
  started_at?: string
  updated_at?: string
}

export interface SpeechRuntimeSnapshot {
  room_id: number
  revision: number
  mainline: SpeechTrackRuntime
  interrupt: SpeechTrackRuntime
  updated_at?: string
}

export interface GeneratedSpeechHistoryItem {
  id: number
  tenant_id: number
  room_id: number
  runtime_session_id: number
  runtime_external_id: string
  decision_id: string
  source_type: string
  question_text?: string
  generated_text: string
  correction_count: number
  adopted_correction_count: number
  editing_correction_count: number
  correction_status: 'none' | 'editing' | 'corrected' | 'adopted' | string
  created_at: string
}

export interface GeneratedSpeechHistoryPage {
  items: GeneratedSpeechHistoryItem[]
  page: number
  page_size: number
  total: number
  runtime_session_id: number
}

export type AgentDecisionSource = 'agent' | 'manual'

export interface AgentDecisionItem {
  id: string
  room_id: number
  topic: string
  title: string
  summary?: string
  reply_hint?: string
  priority: number
  status: string
  sources: AgentDecisionSource[]
  merged_count: number
  sample_questions?: string[]
  linked_event_ids?: number[]
  user_ids?: string[]
  created_at: string
  last_seen_at: string
  expires_at: string
  claimed_at?: string
  manual_promoted: boolean
  manual_action?: 'answer' | 'quick'
}

export interface AgentDecisionRecentAnswer {
  topic: string
  title: string
  answered_at: string
  cooldown_until: string
  accumulated: number
  sample_questions?: string[]
}

export interface AgentDecisionNote {
  kind: string
  message: string
  created_at: string
}

export interface AgentDecisionSummary {
  state: string
  focus?: string
  reason?: string
  queue_length: number
  manual_waiting: number
  agent_waiting: number
  cooling_topics: number
}

export interface AgentDecisionSimulationResult {
  decision_id: string
  question: string
  reply: string
  execution_mode: 'intent' | 'verbatim' | string
  plan_name?: string
  user_layer_version?: number
  created_at: string
}

export interface AgentDecisionSnapshot {
  room_id: number
  generated_at: string
  summary: AgentDecisionSummary
  queue: AgentDecisionItem[]
  recently_answered: AgentDecisionRecentAnswer[]
  notes: AgentDecisionNote[]
  simulation_results?: AgentDecisionSimulationResult[]
  capacity: number
  ttl_seconds: number
  cooldown_seconds: number
}

export interface AgentDecisionEnqueueResult {
  item?: AgentDecisionItem
  merged: boolean
  promoted: boolean
  suppressed: boolean
  recently_answered?: AgentDecisionRecentAnswer
  dropped?: AgentDecisionItem
}

export interface RoomAudioRecordingStatus {
  id: string
  status: 'recording' | 'finalizing' | 'ready' | 'failed' | string
  started_at: string
  finished_at?: string
  duration_seconds: number
  segment_count: number
  final_file_name?: string
  final_bytes?: number
  source_protocol?: string
  error?: string
}

export interface RoomCaptureSnapshot {
  room_id: number
  mode: 'idle' | 'audio_recording' | 'finalizing' | string
  recording?: RoomAudioRecordingStatus
}

export interface RoomSpeechAnalysisTask {
  id: number
  tenant_id: number
  room_id: number
  recording_id: string
  recording_started_at: string
  status: 'queued' | 'uploading' | 'transcribing' | 'analyzing' | 'rendering' | 'ready' | 'failed' | string
  stage: string
  progress: number
  error_message?: string
  asr_task_id?: string
  audio_object_key?: string
  transcript_object_key?: string
  report_object_key?: string
  report_file_name?: string
  created_by_user_id: number
  created_at: string
  updated_at: string
  finished_at?: string
}

export interface RoomSpeechAnalysisStatus {
  configured: boolean
  configuration_reason?: string
  task?: RoomSpeechAnalysisTask
}

export interface SpeechAnalysisProfile {
  id: number
  version: number
  name: string
  description: string
  provider: string
  model: string
  segment_system_prompt: string
  segment_prompt_template: string
  summary_system_prompt: string
  summary_prompt_template: string
  status: 'active' | 'draft' | 'inactive' | string
  updated_by_user_id?: number
  updated_by_display_name: string
  created_at: string
  updated_at: string
}

export interface SpeechAnalysisProfileInput {
  name: string
  description: string
  provider: string
  model: string
  segment_system_prompt: string
  segment_prompt_template: string
  summary_system_prompt: string
  summary_prompt_template: string
  activate: boolean
}

export interface CreateRoomPayload {
  tenant_id?: number
  platform: string
  external_room_id: string
  name: string
  collector_mode: string
}

export interface LiveDevice {
  id: number
  sn: string
  sku_code: string
  lifecycle_status: string
  tenant_id: number
  room_id?: number
  binding_role: string
  connection_status: string
  work_status: string
  stop_reason: string
  last_heartbeat_at?: string
}

export interface LiveOpsRoomQuotaSummary {
  tenant_id: number
  user_id: number
  username: string
  display_name: string
  phone: string
  status: string
  parent_org_name: string
  membership_plan_id: number
  membership_name: string
  membership_room_limit: number
  current_room_count: number
  room_limit: number
  remaining_slots: number
  last_reason: string
  last_operator_name: string
  last_adjusted_at?: string
}

export interface LiveOpsRoomQuotaAdjustInput {
  room_limit: number
  reason: string
}

export interface LiveRuntimeSession {
  id: number
  external_id: string
  tenant_id: number
  room_id: number
  device_id?: number
  device_sn?: string
  status: string
  stop_reason: string
  started_by_user_id?: number
  stopped_by_user_id?: number
  started_at: string
  last_billed_at: string
  ended_at?: string
  total_billed_seconds: number
  version: number
}

export interface LiveQuotaSourceSummary {
  source_type: string
  source_id?: number
  source_label: string
  asset_no?: string
  remaining_seconds: number
  expires_at?: string
}

export interface LiveTimeCardSummary {
  id: number
  asset_no: string
  product_name: string
  status: string
  original_seconds: number
  remaining_seconds: number
  activation_mode: string
  validity_days: number
  activation_deadline_at?: string
  activated_at?: string
  expires_at?: string
  purchased_at: string
}

export interface LiveTimeCardPage {
  items: LiveTimeCardSummary[]
  page: number
  page_size: number
  total: number
}

export interface LiveBillingRoomSummary {
  room_id: number
  room_name: string
  session_id: number
  billed_seconds: number
  started_at: string
}

export interface LiveQuotaSummary {
  active_seconds: number
  active_time_card_seconds: number
  reserve_time_card_seconds: number
  reserve_time_card_count: number
  current?: LiveQuotaSourceSummary
  sources: LiveQuotaSourceSummary[]
  time_cards: LiveTimeCardSummary[]
  active_billing_rooms: LiveBillingRoomSummary[]
}

export interface LiveRuntimeSnapshot {
  session?: LiveRuntimeSession
  agent_state: 'stopped' | 'working' | 'paused'
  agent_mode: 'control' | 'anchor'
  agent_plan_id?: number
  agent_plan_name?: string
  agent_working_seconds: number
  quota_remaining_seconds: number
  reserve_time_card_seconds: number
  reserve_time_card_count: number
  current_quota?: LiveQuotaSourceSummary
  time_cards: LiveTimeCardSummary[]
  room_live: boolean
  device?: LiveDevice
}

export interface LiveAgentPlanTermVariant {
  id: number
  variant_text: string
  source: string
  confirmation_count: number
  last_confirmed_at: string
}

export interface LiveAgentPlanTerm {
  id: number
  plan_id: number
  canonical_text: string
  term_type: string
  note?: string
  status: string
  variants: LiveAgentPlanTermVariant[]
  created_at: string
  updated_at: string
}

export interface LiveAgentPlan {
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

export interface LiveAgentSettings {
  tenant_id: number
  display_name: string
  role_name: string
  self_introduction: string
  mission: string
  greeting: string
  updated_by_user_id?: number
  updated_at?: string
}

export interface LiveAgentSettingsInput {
  display_name: string
  role_name: string
  self_introduction: string
  mission: string
  greeting: string
}

export interface LiveAgentConfigVersion {
  id: number
  agent_id: number
  version_no: number
  layer1: Record<string, unknown>
  layer2: Record<string, unknown>
  layer3: Record<string, unknown>
  persona: Record<string, unknown>
  model_config: Record<string, unknown>
  speech_config: Record<string, unknown>
  style_profile: Record<string, unknown>
  safety_config: Record<string, unknown>
  lifecycle_status: 'draft' | 'active' | 'archived' | string
  created_at: string
  published_at?: string
}

export interface LiveAgentConfigInput {
  layer1?: Record<string, unknown>
  layer2?: Record<string, unknown>
  layer3?: Record<string, unknown>
  persona?: Record<string, unknown>
  model_config?: Record<string, unknown>
  speech_config?: Record<string, unknown>
  style_profile?: Record<string, unknown>
  safety_config?: Record<string, unknown>
}

export interface LivePolicyIndustry {
  code: string
  name: string
  parent_code?: string
  status: string
  sort_order: number
  created_at: string
  updated_at: string
}

export interface LivePolicyRule {
  key: string
  title?: string
  text: string
  execution_mode: 'intent' | 'verbatim' | string
  fixed_text?: string
  enabled: boolean
  metadata?: Record<string, unknown>
}

export interface LivePolicyOverride {
  key: string
  operation: 'add' | 'replace' | 'disable' | string
  title?: string
  text?: string
  execution_mode?: 'intent' | 'verbatim' | string
  fixed_text?: string
  metadata?: Record<string, unknown>
}

export interface LivePolicyConflict {
  code: string
  key?: string
  message: string
}

export interface LivePolicyVersion {
  id: number
  policy_id: number
  version_no: number
  lifecycle_status: 'draft' | 'active' | 'archived' | string
  source_text: string
  rules: LivePolicyRule[]
  overrides: LivePolicyOverride[]
  conflicts: LivePolicyConflict[]
  note?: string
  source_version_id?: number
  created_by_user_id?: number
  published_by_user_id?: number
  created_at: string
  published_at?: string
}

export interface LivePolicyScope {
  id: number
  layer: 'L1' | 'L2' | 'L3' | string
  scope_key: string
  industry_code?: string
  tenant_id?: number
  room_id?: number
  name: string
  status: string
  current_version_id?: number
  created_at: string
  updated_at: string
}

export interface LivePolicyContext {
  scope: LivePolicyScope
  versions: LivePolicyVersion[]
  active?: LivePolicyVersion
  industry?: LivePolicyIndustry
}

export interface LivePolicyAgentResponse {
  reply: string
  target?: string
  action: 'EXPLAIN' | 'DRAFT' | string
  draft?: LivePolicyVersion
  conflicts?: LivePolicyConflict[]
  model?: string
  latency_ms?: number
}

export interface LiveEffectivePolicyRule {
  key: string
  title?: string
  text: string
  execution_mode: 'intent' | 'verbatim' | string
  fixed_text?: string
  source_layer: 'L1' | 'L2' | 'L3' | string
  source_version_id?: number
  metadata?: Record<string, unknown>
}

export interface LiveEffectivePolicy {
  industry_code: string
  l1?: LivePolicyVersion
  l2?: LivePolicyVersion
  l3?: LivePolicyVersion
  rules: LiveEffectivePolicyRule[]
  conflicts: LivePolicyConflict[]
  prompt_text: string
}

export interface LivePolicyTestMatchedRule {
  key: string
  title?: string
  source_layer: 'L1' | 'L2' | 'L3' | string
  execution_mode: string
}

export interface LivePolicyTestVersionSource {
  layer: 'L1' | 'L2' | 'L3' | string
  version_id: number
  version_no: number
  lifecycle_status: string
  is_test_target: boolean
}

export interface LivePolicyTestResult {
  sandbox: boolean
  reply: string
  blocked: boolean
  block_reason?: string
  matched_rules: LivePolicyTestMatchedRule[]
  data_sources: string[]
  missing_data: string[]
  effective: {
    industry_code: string
    rule_count: number
    conflict_count: number
    sources: LivePolicyTestVersionSource[]
  }
  model?: string
  latency_ms?: number
}

export type AgentMemoryType = 'semantic' | 'fact' | 'wording' | 'style' | string

export interface AgentLearningSession {
  id: number
  tenant_id: number
  room_id: number
  source_type: string
  source_ref?: string
  question?: string
  original_reply?: string
  target?: string
  status: 'editing' | 'adopted' | 'closed' | string
  memory_type?: AgentMemoryType
  adopted_memory_item_id?: number
  created_by_user_id: number
  created_at: string
  updated_at: string
  adopted_at?: string
}

export interface AgentLearningEvidence {
  id: number
  session_id: number
  turn_no: number
  feedback: string
  created_at: string
}

export interface AgentLearningResult {
  id: number
  session_id: number
  evidence_id: number
  turn_no: number
  memory_type: AgentMemoryType
  target: string
  memory_key: string
  matched_memory_item_id?: number
  result_text: string
  structured?: Record<string, unknown>
  model_provider?: string
  model_name?: string
  latency_ms?: number
  created_at: string
}

export interface AgentLearningTimelineItem {
  evidence: AgentLearningEvidence
  result: AgentLearningResult
}

export interface AgentLearningSessionDetail {
  session: AgentLearningSession
  timeline: AgentLearningTimelineItem[]
  latest?: AgentLearningResult
}

export interface AgentMemoryVersion {
  id: number
  memory_item_id: number
  version_no: number
  status: 'active' | 'superseded' | 'rolled_back' | string
  content_text: string
  structured?: Record<string, unknown>
  source_session_id: number
  source_result_id: number
  created_by_user_id: number
  created_at: string
}

export interface AgentMemoryItem {
  id: number
  tenant_id: number
  room_id: number
  memory_type: AgentMemoryType
  memory_key: string
  target: string
  status: 'active' | 'inactive' | string
  current_version_id?: number
  created_by_user_id: number
  created_at: string
  updated_at: string
  current_version?: AgentMemoryVersion
}

export interface AgentLearningTurnOutput {
  session: AgentLearningSession
  result: AgentLearningResult
}

export interface AdoptAgentLearningOutput {
  session: AgentLearningSession
  memory: AgentMemoryItem
  version: AgentMemoryVersion
}

export type LivePolicyLearningStatus = 'pending' | 'adopted' | 'rejected' | string

export interface LivePolicyLearningHistoryItem {
  role: 'user' | 'agent' | string
  text: string
}

export interface LivePolicyLearningCandidate {
  id: number
  source_layer: 'L1' | 'L2' | 'L3' | string
  industry_code?: string
  tenant_id?: number
  room_id?: number
  question: string
  final_reply: string
  feedback?: string
  history: LivePolicyLearningHistoryItem[]
  recommended_layer: 'L1' | 'L2' | 'L3' | string
  recommendation_reason: string
  absorb_recommended: boolean
  confidence: number
  rule_title: string
  rule_text: string
  execution_mode: 'intent' | 'verbatim' | string
  status: LivePolicyLearningStatus
  model?: string
  latency_ms?: number
  adopted_version_id?: number
  created_by_user_id: number
  reviewed_by_user_id?: number
  review_note?: string
  created_at: string
  updated_at: string
  reviewed_at?: string
}

export interface CreateLivePolicyLearningCandidateInput {
  source_layer: 'L1' | 'L2' | 'L3'
  source_ref?: string
  industry_code?: string
  room_id?: number
  question: string
  final_reply: string
  feedback?: string
  history?: LivePolicyLearningHistoryItem[]
}

export interface AdoptLivePolicyLearningCandidateInput {
  target_layer?: 'L1' | 'L2' | 'L3'
  industry_code?: string
  room_id?: number
  review_note?: string
}

export interface LivePolicyLearningAdoptResult {
  candidate: LivePolicyLearningCandidate
  draft: LivePolicyVersion
  version?: LivePolicyVersion
  published?: boolean
}

export interface LiveRoomPolicyContext {
  industry_code: string
  effective: LiveEffectivePolicy
  l3_versions: LivePolicyVersion[]
  l3_scope?: LivePolicyScope
}

export type LiveSupportCapability = 'l3_policy' | 'anchor_training' | 'voice_clone'

export interface LiveSupportStaff {
  user_id: number
  username: string
  display_name: string
  avatar_url?: string
  specialty_industries?: string[]
  allowed_capabilities?: LiveSupportCapability[]
  l3_restriction_reason?: string
}

export interface LiveSupportAuthorization {
  id: number
  tenant_id: number
  room_id: number
  staff_user_id: number
  staff_username?: string
  staff_display_name?: string
  capability: LiveSupportCapability | string
  status: string
  granted_by_user_id: number
  granted_at: string
  revoked_by_user_id?: number
  revoked_at?: string
  updated_at: string
}

export interface LiveSupportRequest {
  id: number
  tenant_id: number
  room_id: number
  room_name?: string
  staff_user_id: number
  staff_username?: string
  staff_display_name?: string
  requested_by_user_id: number
  capabilities: LiveSupportCapability[]
  status: 'pending' | 'accepted' | 'rejected' | 'cancelled' | string
  decided_by_user_id?: number
  decision_note: string
  requested_at: string
  decided_at?: string
  updated_at: string
}

export interface LiveSupportTrainingDraft {
  version_id: number
  version_no: number
  status: string
}

export interface MediaAsset {
  id: number
  tenant_id: number
  agent_id?: number
  asset_type: string
  original_name: string
  storage_driver: string
  storage_bucket?: string
  object_key: string
  mime_type: string
  size_bytes: number
  duration_ms?: number
  checksum_sha256: string
  status: string
  metadata?: Record<string, unknown>
  created_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface VoiceProfile {
  id: number
  tenant_id: number
  agent_id?: number
  name: string
  provider: string
  voice_id: string
  sample_asset_id?: number
  clone_status: 'pending' | 'training' | 'ready' | 'failed' | 'disabled' | string
  config?: Record<string, unknown>
  is_default: boolean
  created_at: string
  updated_at: string
}

export interface OfficialVoice {
  id: string
  name: string
  gender: string
  description: string
  tags: string[]
  model: string
}

export interface VoicePreviewResponse {
  audio_url: string
}

export interface LiveRuntimeEvent {
  id: number
  tenant_id: number
  room_id?: number
  device_id?: number
  session_id?: number
  actor_type: string
  actor_user_id?: number
  event_code: string
  title: string
  detail?: Record<string, unknown>
  occurred_at: string
}

export interface StaffBusinessScope {
  mode: 'all' | 'groups' | 'self' | string
  actor_user_id?: number
  group_ids?: number[]
  manager_view: boolean
}

export interface CustomerScopeSummary {
  total_count: number
  active_count: number
  agent_count: number
  referral_count: number
}

export interface AdminCustomer {
  user_id: number
  tenant_id: number
  username: string
  display_name: string
  avatar_url?: string
  phone: string
  email: string
  status: string
  source_type: string
  cooperation_status: 'cooperating' | 'non_cooperating' | string
  cooperation_note: string
  cooperation_marked_at?: string
  cooperation_marked_by_user_id?: number
  last_recharge_at?: string
  recharge_dormant_90_days: boolean
  parent_org_id: number
  parent_org_name: string
  parent_org_type: string
  inviter_user_id: number
  inviter_username: string
  inviter_display_name: string
  sales_staff_id: number
  sales_user_id: number
  sales_employee_code: string
  sales_username: string
  sales_display_name: string
  industry_code: string
  industry_name: string
  created_at: string
}

export interface CustomerCooperationInfo {
  tenant_id: number
  cooperation_status: 'cooperating' | 'non_cooperating' | string
  cooperation_note: string
  cooperation_marked_at?: string
  cooperation_marked_by_user_id?: number
  last_recharge_at?: string
  recharge_dormant_90_days: boolean
}

export interface AdminCustomerPage {
  items: AdminCustomer[]
  total: number
  page: number
  page_size: number
  total_pages: number
  summary: CustomerScopeSummary
  scope: StaffBusinessScope
}
export interface AdminAuditLog {
  id: string
  occurred_at: string
  actor_user_id: number
  actor_username: string
  action: string
  target_user_id?: number
  target_username?: string
  target_tenant_id?: number
  http_method?: string
  path?: string
  client_ip?: string
  result: string
}

export interface AdminAuditPage {
  items: AdminAuditLog[]
  page_size: number
  next_cursor: number
  has_more: boolean
  scope: StaffBusinessScope
}
export interface AccountProfile {
  user_id: number
  tenant_id?: number
  username: string
  display_name: string
  avatar_url: string
  role: string
  phone: string
  email: string
  qq: string
  wechat: string
  province: string
  city: string
  district: string
  address: string
  status: string
  created_at: string
}
export interface AuthSessionSummary {
  id: number
  client_ip: string
  user_agent: string
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}
export interface MembershipSummary {
  id: number
  plan_id: number
  plan_version_id: number
  plan_name: string
  status: string
  cycle_start_at: string
  cycle_end_at: string
  included_seconds: number
  auto_renew: boolean
}

export interface QuotaSummary {
  membership_seconds: number
  purchased_seconds: number
  reward_seconds: number
  total_seconds: number
}

export interface AccountDashboard {
  profile: AccountProfile
  membership?: MembershipSummary
  quota: QuotaSummary
}

export interface WalletLedgerRecord {
  id: number
  direction: string
  amount_cents: number
  balance_before_cents: number
  balance_after_cents: number
  business_type: string
  order_no?: string
  reason?: string
  occurred_at: string
}

export interface RechargeRecord {
  id: number
  recharge_no: string
  requested_amount_cents: number
  credited_amount_cents: number
  payment_method: string
  status: string
  paid_at?: string
  created_at: string
}

export interface PurchaseRecord {
  id: number
  order_no: string
  order_type: string
  status: string
  list_amount_cents: number
  discount_amount_cents: number
  paid_amount_cents: number
  refunded_amount_cents: number
  paid_at?: string
  created_at: string
}

export interface RefundRecord {
  id: number
  refund_no: string
  source_type: string
  source_id: number
  refund_amount_cents: number
  refund_method: string
  status: string
  reason: string
  processed_at?: string
  created_at: string
}

export interface FinanceDashboard {
  cash_balance_cents: number
  reward_balance_cents: number
  commission_balance_cents: number
  commission_frozen_cents: number
  total_balance_cents: number
  month_spent_cents: number
  available_seconds: number
  membership_name?: string
  ledger: WalletLedgerRecord[]
  recharges: RechargeRecord[]
  purchases: PurchaseRecord[]
  refunds: RefundRecord[]
  payments: SandboxPaymentRecord[]
}

export interface BeneficiaryWallet {
  id: number
  beneficiary_type: string
  beneficiary_id: number
  currency: string
  available_balance_cents: number
  frozen_balance_cents: number
  version: number
  created_at: string
  updated_at: string
}

export interface BeneficiaryWalletLedger {
  id: number
  wallet_id: number
  external_id: string
  business_type: string
  reference_type: string
  reference_id?: number
  available_delta_cents: number
  frozen_delta_cents: number
  available_before_cents: number
  available_after_cents: number
  frozen_before_cents: number
  frozen_after_cents: number
  operator_user_id?: number
  reason: string
  created_at: string
}

export interface WithdrawalRequest {
  id: number
  withdrawal_no: string
  wallet_id: number
  beneficiary_type: string
  beneficiary_id: number
  amount_cents: number
  status: string
  requested_by_user_id: number
  approved_by_user_id?: number
  paid_by_user_id?: number
  reject_reason: string
  requested_at: string
  approved_at?: string
  paid_at?: string
  updated_at: string
}

export interface BeneficiaryWalletDashboard {
  wallet: BeneficiaryWallet
  ledger: BeneficiaryWalletLedger[]
  withdrawals: WithdrawalRequest[]
}
export interface CommercialMembershipVersion {
  id: number
  plan_id: number
  version_no: number
  lifecycle_status: string
  currency: string
  price_cents: number
  recurring_month_discount_bps: number
  recurring_quarter_discount_bps: number
  annual_discount_bps: number
  billing_period_unit: string
  billing_period_count: number
  included_seconds: number
  default_time_card_discount_bps: number
  default_device_discount_bps: number
  allow_auto_renew: boolean
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
}

export interface MarketingCampaignItem {
  id?: number
  campaign_id?: number
  target_type: 'membership' | 'time_card' | 'device_product' | string
  target_id: number
  pricing_mode: 'discount' | 'package' | string
  package_months: number
  discount_bps: number
  quantity: number
  sort_order?: number
}

export interface MarketingCampaign {
  id: number
  code: string
  name: string
  description?: string
  status: string
  sort_order: number
  pricing_rule?: 'floor_yuan' | string
  items: MarketingCampaignItem[]
  display_locations: string[]
  target_type?: 'membership' | 'time_card' | 'device_product' | string
  target_id?: number
  pricing_mode?: 'discount' | 'package' | string
  package_months?: number
  discount_bps?: number
  starts_at?: string
  ends_at?: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface MarketingCampaignInput {
  code: string
  name: string
  description: string
  status: 'active' | 'inactive' | 'draft' | string
  sort_order: number
  pricing_rule: 'floor_yuan' | string
  starts_at?: string
  ends_at?: string
  items: MarketingCampaignItem[]
  display_locations: string[]
}

export interface CommercialMembershipPlan {
  id: number
  code: string
  name: string
  description: string
  status: string
  sort_order: number
  created_at: string
  updated_at: string
  latest_version?: CommercialMembershipVersion
  active_version?: CommercialMembershipVersion
  draft_version?: CommercialMembershipVersion
  marketing_campaigns: MarketingCampaign[]
}

export interface CommercialMembershipInput {
  code: string
  name: string
  description: string
  sort_order: number
  price_cents: number
  recurring_month_discount_bps: number
  recurring_quarter_discount_bps: number
  annual_discount_bps: number
  included_seconds: number
  default_time_card_discount_bps: number
  default_device_discount_bps: number
  allow_auto_renew: boolean
}

export interface CustomerMembershipOffer {
  id: number
  code: string
  name: string
  description: string
  monthly_price_cents: number
  recurring_month_discount_bps: number
  recurring_quarter_discount_bps: number
  annual_discount_bps: number
  included_seconds: number
  time_card_discount_bps: number
  device_discount_bps: number
  allow_auto_renew: boolean
  version_no: number
  marketing_campaigns: MarketingCampaign[]
}
export interface AgentSummary {
  organization_id: number
  code: string
  name: string
  status: string
  created_at: string
  admin_user_id: number
  username: string
  display_name: string
  phone: string
  email: string
  user_status: string
  customer_count: number
}
export interface InviteCodeSummary {
  id: number
  code: string
  owner_user_id: number
  owner_tenant_id?: number
  owner_role: string
  owner_username: string
  owner_name: string
  status: string
  max_uses: number
  used_count: number
  expires_at?: string
  created_at: string
}

export interface InvitePreview {
  code: string
  inviter_name: string
  inviter_role: string
  source_type: string
}

export interface InvitationRecord {
  id: number
  invite_code_id: number
  invite_code: string
  inviter_user_id: number
  inviter_tenant_id?: number
  inviter_username: string
  inviter_display_name: string
  referred_user_id: number
  referred_tenant_id: number
  referred_username: string
  referred_display_name: string
  source_type: string
  parent_org_id: number
  parent_org_name: string
  bound_at: string
}

export interface InvitationDashboard {
  my_code: InviteCodeSummary
  codes: InviteCodeSummary[]
  codes_total: number
  records: InvitationRecord[]
  records_total: number
  own_referral_count: number
}
export interface ResourceAccount {
  id: number
  organization_id: number
  resource_type: string
  unit: string
  balance: number
  reserved: number
  status: string
  updated_at: string
}

export interface ResourceLedgerEntry {
  id: number
  organization_id: number
  counterparty_org_id?: number
  resource_type: string
  change_quantity: number
  balance_before: number
  balance_after: number
  business_type: string
  operator_user_id?: number
  reason: string
  created_at: string
}

export interface ResourceDashboard {
  organization_id: number
  organization: string
  org_type: string
  accounts: ResourceAccount[]
  ledger: ResourceLedgerEntry[]
}
export interface SalesStaffSummary {
  staff_id: number
  user_id: number
  employee_code: string
  username: string
  display_name: string
  phone: string
  email: string
  province: string
  city: string
  district: string
  status: string
  team_id?: number
  team_name: string
  customer_count: number
  created_at: string
}

export interface SalesStaffPage {
  items: SalesStaffSummary[]
  total: number
  page: number
  page_size: number
  total_pages: number
  scope: StaffBusinessScope
}


export interface CommercialTimeCardVersion {
  id: number
  product_id: number
  version_no: number
  lifecycle_status: string
  currency: string
  price_cents: number
  duration_seconds: number
  validity_days: number
  participates_referral: boolean
  activation_mode: string
  activation_deadline_days: number

  participates_sales_commission: boolean
  participates_agent_settlement: boolean
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
}

export interface CommercialTimeCardProduct {
  id: number
  code: string
  name: string
  description: string
  status: string
  sort_order: number
  created_at: string
  updated_at: string
  latest_version?: CommercialTimeCardVersion
  active_version?: CommercialTimeCardVersion
  draft_version?: CommercialTimeCardVersion
  marketing_campaigns: MarketingCampaign[]
}

export interface CommercialTimeCardInput {
  code: string
  name: string
  description: string
  sort_order: number
  price_cents: number
  duration_seconds: number
  validity_days: number
  participates_referral: boolean
  activation_mode: string
  activation_deadline_days: number

  participates_sales_commission: boolean
  participates_agent_settlement: boolean
}

export interface CustomerTimeCardOffer {
  id: number
  code: string
  name: string
  description: string
  duration_seconds: number
  validity_days: number
  original_price_cents: number
  activation_mode: string
  activation_deadline_days: number

  discount_bps: number
  sale_price_cents: number
  version_no: number
}

export interface CommercialDeviceVersion {
  id: number
  product_id: number
  version_no: number
  lifecycle_status: string
  currency: string
  cost_price_cents: number
  list_price_cents: number
  sale_price_cents: number
  participates_referral: boolean
  participates_sales_commission: boolean
  participates_agent_settlement: boolean
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
}

export interface CommercialDeviceProduct {
  id: number
  code: string
  sku_code: string
  name: string
  description: string
  image_url: string
  unit_code: string
  unit_label: string
  status: string
  sort_order: number
  available_stock: number
  sales_stock: number
  real_stock: number
  created_at: string
  updated_at: string
  latest_version?: CommercialDeviceVersion
  active_version?: CommercialDeviceVersion
  draft_version?: CommercialDeviceVersion
  marketing_campaigns: MarketingCampaign[]
}

export interface CommercialDeviceInput {
  code: string
  sku_code: string
  name: string
  description: string
  image_url: string
  unit_code: string
  sort_order: number
  list_price_cents: number
  sales_stock: number
  cost_price_cents: number
  sale_price_cents: number
  participates_referral: boolean
  participates_sales_commission: boolean
  participates_agent_settlement: boolean
}

export interface CustomerDeviceOffer {
  id: number
  code: string
  sku_code: string
  name: string
  description: string
  image_url: string
  unit_code: string
  unit_label: string
  original_price_cents: number
  base_sale_price_cents: number
  sale_price_cents: number
  membership_discount_bps: number
  discount_bps: number
  version_no: number
  available_stock: number
}

export interface FeatureRecord {
  id: number
  feature_key: string
  record_key: string
  title: string
  status: string
  sort_order: number
  payload_json: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface FeatureRecordInput {
  record_key: string
  title: string
  status: string
  sort_order: number
  payload_json: string
}


export interface InventoryWarehouse {
  id: number
  code: string
  name: string
  organization_id?: number
  status: string
  created_at: string
  updated_at: string
}

export interface InventoryDeviceProduct {
  id: number
  code: string
  sku_code: string
  name: string
  status: string
}

export interface InventoryDeviceSKUType {
  sku_code: string
  total_quantity: number
  in_stock_quantity: number
  warehouse_count: number
  sample_sn: string
  sample_batch_no: string
  bound_product_id: number
  bound_product_name: string
}

export interface InventoryBatchInboundInput {
  product_id: number
  batch_no: string
  purchase_no: string
  warehouse_id: number
  expected_quantity: number
  purchase_amount_cents: number
  supplier_name: string
  payment_method: string
  sns: string[]
  quality_status: string
  reason: string
}

export interface InventoryBatchInboundResult {
  document_id: number
  document_no: string
  product_id: number
  product_name: string
  sku_code: string
  batch_no: string
  purchase_no: string
  expected_quantity: number
  actual_quantity: number
  purchase_amount_cents: number
  supplier_name: string
  payment_method: string
  device_ids: number[]
  sns: string[]
}

export interface InventoryDevice {
  id: number
  sn: string
  sku_code: string
  batch_no: string
  owner_org_id?: number
  custody_warehouse_id?: number
  custody_warehouse: string
  current_customer_id?: number
  lifecycle_status: string
  quality_status: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface InventoryDeviceLedgerEntry {
  id: number
  device_id: number
  document_id: number
  document_no: string
  action: string
  from_status: string
  to_status: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  from_owner_org_id?: number
  to_owner_org_id?: number
  from_customer_id?: number
  to_customer_id?: number
  operator_user_id?: number
  reason: string
  created_at: string
}

export interface InventoryStockDocument {
  id: number
  document_no: string
  document_type: string
  status: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  counterparty_org_id?: number
  reference_no: string
  product_id?: number
  product_name: string
  expected_quantity: number
  actual_quantity: number
  business_amount_cents: number
  counterparty_name: string
  payment_method: string
  reason: string
  operator_user_id?: number
  approved_by_user_id?: number
  effective_at?: string
  created_at: string
  item_count: number
}

export interface InventorySummary {
  warehouse_id: number
  warehouse_name: string
  lifecycle_status: string
  quantity: number
}

export interface InventoryRMA {
  id: number
  rma_no: string
  device_id: number
  device_sn: string
  service_type: string
  status: string
  source_type: string
  source_user_id?: number
  source_tenant_id?: number
  source_agent_org_id?: number
  customer_name: string
  contact_phone: string
  issue: string
  resolution: string
  replacement_device_id?: number
  source_order_id?: number
  source_order_no: string
  source_shipment_id?: number
  source_shipment_no: string
  return_shipment_id?: number
  return_shipment_no: string
  outbound_shipment_id?: number
  outbound_shipment_no: string
  repair_outbound_shipment_id?: number
  repair_outbound_shipment_no: string
  repair_return_shipment_id?: number
  repair_return_shipment_no: string
  operator_user_id?: number
  accepted_by_user_id?: number
  accepted_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export interface InventoryRMAPage {
  items: InventoryRMA[]
  total: number
  open_total: number
  page: number
  page_size: number
}

export interface AfterSalesRequestPage {
  items: InventoryRMA[]
  total: number
  page: number
  page_size: number
}

export interface RMAEvent {
  id: number
  rma_id: number
  event_code: string
  status: string
  title: string
  description: string
  customer_visible: boolean
  operator_user_id?: number
  occurred_at: string
}

export interface RMACost {
  id: number
  cost_no: string
  rma_id: number
  cost_type: string
  amount_cents: number
  counterparty_name: string
  payment_method: string
  note: string
  operator_user_id?: number
  occurred_at: string
  created_at: string
}

export interface CreateRMACostInput {
  cost_type: string
  amount_cents: number
  counterparty_name: string
  payment_method: string
  note: string
}

export interface AfterSalesRequestInput {
  sn: string
  service_type: string
  customer_name: string
  contact_phone: string
  issue: string
}

export interface InventoryCreateDeviceInput {
  sn: string
  sku_code: string
  batch_no: string
  owner_org_id?: number
  warehouse_id: number
  quality_status: string
  reason: string
}

export interface InventoryDeviceTransitionInput {
  to_status: string
  to_warehouse_id?: number
  to_owner_org_id?: number
  to_customer_id?: number
  reason: string
  reference_no: string
}

export interface InventoryCreateRMAInput {
  device_id: number
  service_type: string
  customer_name: string
  contact_phone: string
  issue: string
}

export interface InventoryCompleteRMAInput {
  resolution: string
  to_status: string
  to_warehouse_id?: number
  replacement_device_id?: number
}


export interface SalesPerformanceSummary {
  sales_staff_id: number
  user_id: number
  employee_code: string
  display_name: string
  team_name: string
  paid_order_count: number
  customer_count: number
  paid_amount_cents: number
  refunded_amount_cents: number
  net_revenue_cents: number
  earning_amount_cents: number
  pending_earning_cents: number
  settled_earning_cents: number
}

export interface SalesPerformanceTotals {
  paid_order_count: number
  customer_count: number
  paid_amount_cents: number
  refunded_amount_cents: number
  net_revenue_cents: number
  earning_amount_cents: number
  pending_earning_cents: number
  settled_earning_cents: number
}

export interface SalesPerformanceResponse {
  period: string
  period_start: string
  period_end: string
  items: SalesPerformanceSummary[]
  total: number
  page: number
  page_size: number
  total_pages: number
  totals: SalesPerformanceTotals
  scope: StaffBusinessScope
}

export interface SalesFollowup {
  id: number
  sales_staff_id: number
  tenant_id: number
  customer_user_id: number
  customer_username: string
  customer_name: string
  customer_phone: string
  followup_type: string
  content: string
  next_followup_at?: string
  created_at: string
}

export interface SalesFollowupSummary {
  total_count: number
  due_count: number
  overdue_count: number
}

export interface SalesFollowupResponse {
  items: SalesFollowup[]
  summary: SalesFollowupSummary
}

export interface SalesCatalogMembership {
  id: number
  code: string
  name: string
  description: string
  price_cents: number
  included_seconds: number
  billing_period_unit: string
  billing_period_count: number
  allow_auto_renew: boolean
  version_no: number
}

export interface SalesCatalogTimeCard {
  id: number
  code: string
  name: string
  description: string
  price_cents: number
  duration_seconds: number
  validity_days: number
  activation_mode: string
  activation_deadline_days: number
  version_no: number
}

export interface SalesCatalogDevice {
  id: number
  code: string
  sku_code: string
  name: string
  description: string
  image_url: string
  unit_label: string
  list_price_cents: number
  sale_price_cents: number
  available_stock: number
  version_no: number
}

export interface SalesCatalog {
  memberships: SalesCatalogMembership[]
  time_cards: SalesCatalogTimeCard[]
  devices: SalesCatalogDevice[]
}


export interface IncentiveRule {
  id: number
  program_version_id: number
  priority: number
  event_type: string
  conditions_json: string
  action_type: string
  action_config_json: string
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface IncentiveProgramVersion {
  id: number
  program_id: number
  version_no: number
  lifecycle_status: string
  pending_days: number
  effective_from?: string
  effective_to?: string
  published_at?: string
  created_at: string
  rules: IncentiveRule[]
}

export interface IncentiveProgram {
  id: number
  code: string
  name: string
  program_type: string
  status: string
  description: string
  created_at: string
  updated_at: string
  active_version?: IncentiveProgramVersion
  draft_version?: IncentiveProgramVersion
  latest_version?: IncentiveProgramVersion
}

export interface IncentiveRuleInput {
  priority: number
  event_type: string
  conditions_json: string
  action_type: string
  action_config_json: string
  enabled: boolean
}

export interface IncentiveProgramInput {
  code: string
  name: string
  program_type: string
  description: string
  pending_days: number
  rules: IncentiveRuleInput[]
}

export interface IncentiveEarning {
  id: number
  external_id: string
  beneficiary_type: string
  beneficiary_id: number
  earning_type: string
  source_order_id?: number
  source_refund_id?: number
  program_version_id?: number
  rule_id?: number
  currency: string
  amount_cents: number
  quota_seconds: number
  status: string
  available_at?: string
  created_at: string
  updated_at: string
}

export interface SettlementBatch {
  id: number
  batch_no: string
  beneficiary_type: string
  beneficiary_id: number
  currency: string
  period_start_at: string
  period_end_at: string
  gross_amount_cents: number
  adjustment_amount_cents: number
  settlement_amount_cents: number
  status: string
  created_by_user_id?: number
  approved_by_user_id?: number
  approved_at?: string
  paid_at?: string
  created_at: string
  updated_at: string
  item_count: number
}

export interface SettlementDashboard {
  earnings: IncentiveEarning[]
  batches: SettlementBatch[]
}

export interface CreateSettlementBatchInput {
  beneficiary_type: string
  beneficiary_id: number
  period_start_at: string
  period_end_at: string
}


export interface AgentExitCheck {
  organization_id: number
  active_device_count: number
  open_rma_count: number
  unsettled_earning_count: number
  unsettled_earning_amount_cents: number
  open_settlement_batch_count: number
  nonzero_resource_account_count: number
  active_customer_count: number
  can_exit: boolean
  blockers: string[]
}

export interface AgentExitRecord {
  id: number
  record_key: string
  organization_id: number
  agent_code: string
  agent_name: string
  status: string
  transferred_customer_count: number
  active_device_count: number
  open_rma_count: number
  unsettled_earning_count: number
  unsettled_earning_amount_cents: number
  open_settlement_batch_count: number
  nonzero_resource_account_count: number
  finalized_by_user_id?: number
  finalized_by_name: string
  finalized_at: string
  created_at: string
}


export interface AgentLevel {
  id: number
  code: string
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
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
}

export interface AgentLevelHistory {
  id: number
  agent_tenant_id: number
  agent_name: string
  level_id: number
  level_code: string
  level_name: string
  previous_level_id?: number
  previous_level_name?: string
  status: string
  approved_by_user_id?: number
  approved_at: string
  effective_at: string
  ended_at?: string
  reason: string
  created_at: string
}

export interface AgentContractAttachment {
  id: number
  contract_id: number
  file_name: string
  file_url: string
  content_type: string
  size_bytes: number
  page_order: number
  created_by_user_id?: number
  created_at: string
}
export interface AgentContract {
  id: number
  contract_no: string
  external_contract_no: string
  agent_tenant_id: number
  agent_name: string
  parent_contract_id?: number
  contract_type: string
  status: string
  level_id?: number
  level_name?: string
  level_snapshot_json: string
  starts_on: string
  ends_on?: string
  contract_amount_cents: number
  note: string
  signed_at?: string
  created_by_user_id?: number
  updated_by_user_id?: number
  created_at: string
  updated_at: string
  attachments: AgentContractAttachment[]
}


export interface LogisticsShipmentItem {
  id: number
  shipment_id: number
  device_id: number
  sn: string
  sku_code: string
  created_at: string
}

export interface LogisticsEvent {
  id: number
  shipment_id: number
  event_code: string
  status: string
  location: string
  description: string
  operator_user_id?: number
  occurred_at: string
  created_at: string
}

export interface LogisticsShipment {
  id: number
  shipment_no: string
  shipment_type: string
  business_type: string
  business_id?: number
  business_no: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  recipient_customer_id?: number
  recipient_org_id?: number
  recipient_type: string
  delivery_method: string
  logistics_fee_cents: number
  recipient_name: string
  recipient_phone: string
  recipient_address: string
  carrier_code: string
  carrier_name: string
  tracking_no: string
  status: string
  note: string
  operator_user_id?: number
  shipped_at?: string
  delivered_at?: string
  created_at: string
  updated_at: string
  items: LogisticsShipmentItem[]
  events: LogisticsEvent[]
}

export interface CreateLogisticsShipmentInput {
  shipment_type: string
  business_type: string
  business_id?: number
  business_no: string
  from_warehouse_id?: number
  to_warehouse_id?: number
  recipient_customer_id?: number
  recipient_org_id?: number
  recipient_type: string
  delivery_method: string
  logistics_fee_cents?: number
  recipient_name: string
  recipient_phone: string
  recipient_address: string
  carrier_code: string
  carrier_name: string
  tracking_no: string
  device_ids: number[]
  note: string
}

export interface UpdateLogisticsShipmentStatusInput {
  status: string
  location: string
  description: string
}

export interface ScrapDisposalInput {
  amount_cents: number
  buyer_name: string
  payment_method: string
  note: string
}

export interface ScrapDisposal {
  id: number
  disposal_no: string
  device_id: number
  device_sn: string
  amount_cents: number
  buyer_name: string
  payment_method: string
  note: string
  operator_user_id?: number
  disposed_at: string
  created_at: string
}

export interface OperatingFinanceEntry {
  id: number
  entry_no: string
  direction: 'income' | 'expense' | string
  category: string
  amount_cents: number
  currency: string
  business_type: string
  business_id?: number
  business_no: string
  counterparty_name: string
  payment_method: string
  description: string
  operator_user_id?: number
  operator_name: string
  occurred_at: string
  created_at: string
}

export interface TokenPurchase {
  id: number
  purchase_no: string
  provider_name: string
  model_scope: string
  token_quantity: number
  amount_cents: number
  payment_method: string
  invoice_no: string
  purchased_at: string
  note: string
  operator_user_id?: number
  operator_name: string
  created_at: string
}

export interface TokenPurchaseInput {
  provider_name: string
  model_scope: string
  token_quantity: number
  amount_cents: number
  payment_method: string
  invoice_no: string
  purchased_at?: string
  note: string
}

export interface OperatingFinanceOverview {
  total_income_cents: number
  total_expense_cents: number
  month_income_cents: number
  month_expense_cents: number
  entries: OperatingFinanceEntry[]
  token_purchases: TokenPurchase[]
}

export interface CustomerShopOrderItem {
  id: number
  order_id: number
  product_type: string
  product_id: number
  product_version_id: number
  product_name: string
  quantity: number
  duration_seconds: number
  validity_days: number
  unit_list_price_cents: number
  unit_paid_price_cents: number
  discount_bps: number
  created_at: string
}

export interface SandboxPaymentRecord {
  id: number
  payment_no: string
  tenant_id: number
  order_id: number
  order_no: string
  channel: string
  payment_method: string
  currency: string
  expected_amount_cents: number
  input_amount_cents: number
  paid_amount_cents: number
  status: string
  failure_reason: string
  external_trade_no: string
  operator_user_id?: number
  idempotency_key: string
  paid_at?: string
  created_at: string
  updated_at: string
}

export interface CustomerOrderShipping {
  recipient_name: string
  recipient_phone: string
  province: string
  city: string
  district: string
  address: string
  full_address: string
}

export interface CustomerOrderDevice {
  id: number
  order_id: number
  order_item_id: number
  device_id: number
  sn: string
  sku_code: string
  status: string
  shipment_id?: number
  reserved_at: string
  shipped_at?: string
  delivered_at?: string
  returned_at?: string
}

export interface CustomerShopOrder {
  id: number
  order_no: string
  tenant_id: number
  order_type: string
  status: string
  payment_status: string
  fulfillment_status: string
  currency: string
  list_amount_cents: number
  discount_amount_cents: number
  payable_amount_cents: number
  paid_amount_cents: number
  refunded_amount_cents: number
  refundable_amount_cents: number
  refundable_seconds: number
  paid_at?: string
  cancelled_at?: string
  created_at: string
  updated_at: string
  items: CustomerShopOrderItem[]
  payments: SandboxPaymentRecord[]
  shipping?: CustomerOrderShipping
  devices: CustomerOrderDevice[]
  shipments: LogisticsShipment[]
}

export interface CreateCustomerShopOrderInput {
  product_type: string
  product_id: number
  quantity: number
  membership_cycle?: 'single_month' | 'recurring_month' | 'quarter' | 'half_year' | 'annual' | string
  marketing_campaign_id?: number
  marketing_placement?: 'shop' | 'membership' | string
  idempotency_key: string
  recipient_name?: string
  recipient_phone?: string
  province?: string
  city?: string
  district?: string
  address?: string
}

export interface SandboxPayOrderInput {
  amount_cents: number
  simulate_result: 'success' | 'failure'
  idempotency_key: string
}

export interface SandboxRefundOrderInput {
  amount_cents: number
  reason: string
  idempotency_key: string
}
