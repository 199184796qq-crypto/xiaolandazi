<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import LiveContentPolicyPanel from '../components/LiveContentPolicyPanel.vue'
import {
  createSystemDictionaryItem,
  createSystemWarehouse,
  getLivePolicyIndustries,
  getAgentPromptHistory,
  getSystemLiveStrategyCenter,
  getSystemSettingsDashboard,
  resetAgentPromptConfig,
  publishAgentPromptConfig,
  rollbackAgentPromptConfig,
  updateSystemDictionaryItem,
  updateAgentPromptConfigs,
  updateSystemSettings,
  updateMembershipRoomLimits,
  updateSystemLiveStrategyCenter,
  updateSystemWarehouse,
  upsertLivePolicyIndustry,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import PaginationBar from '../components/PaginationBar.vue'
import { financeReviewSettingKey } from '../financeReviewPolicy'
import {
  findSystemSettingsCategory,
  visibleSystemSettingsGroups,
  type SystemSettingsCategoryKey,
} from '../systemSettingsCatalog'
import type {
  InventoryWarehouse,
  AgentPromptHistory,
  LivePolicyIndustry,
  LiveAddressingOption,
  LiveStrategyCenterConfig,
  LiveStrategyCenterInput,
  LiveStrategyRule,
  SystemDictionaryItem,
  SystemDictionaryItemInput,
  SystemSettingsDashboard,
  SystemWarehouseInput,
} from '../types'

type DictionaryCategory = 'logistics_provider' | 'product_unit'

const props = withDefaults(defineProps<{ category?: SystemSettingsCategoryKey }>(), {
  category: 'general',
})

const loading = ref(false)
const saving = ref(false)
const savingRuleTypography = ref(false)
const savingFinancePolicy = ref(false)
const savingMembershipLimits = ref(false)
const savingAgentPrompts = ref(false)
const resettingPromptKey = ref('')
const publishingPromptKey = ref('')
const promptHistoryKey = ref('')
const promptHistories = reactive<Record<string, AgentPromptHistory[]>>({})
const rollingBackPromptKey = ref('')
const error = ref('')
const notice = ref('')
const dashboard = ref<SystemSettingsDashboard | null>(null)
const settingsDraft = reactive<Record<string, string>>({})
const agentPromptDrafts = reactive<Record<string, { current_value: string; enabled: boolean }>>({})
const membershipRoomLimitDrafts = reactive<Record<number, number>>({})
const dictionaryDrafts = reactive<Record<number, SystemDictionaryItemInput>>({})
const dictionaryCategory = ref<DictionaryCategory>('logistics_provider')
const dictionarySearch = ref('')
const dictionaryPage = ref(1)
const dictionaryPageSize = 12
const addingDictionary = ref(false)
const savingDictionaryId = ref<number | null>(null)
const warehouseDrafts = reactive<Record<number, SystemWarehouseInput>>({})
const warehouseSearch = ref('')
const warehousePage = ref(1)
const warehousePageSize = 10
const addingWarehouse = ref(false)
const savingWarehouseId = ref<number | null>(null)
const industries = ref<LivePolicyIndustry[]>([])
const industryDrafts = reactive<Record<string, { name: string; sort_order: number }>>({})
const addingIndustry = ref(false)
const savingIndustryCode = ref('')
const newIndustry = reactive({
  code: '',
  name: '',
  sort_order: 10,
})

const protectedWarehouseCodes = new Set([
  'HQ_MAIN',
  'AFTER_SALES_PENDING',
  'REPAIR',
  'SCRAP_HOLD',
])

const newDictionary = reactive<SystemDictionaryItemInput>({
  category: 'logistics_provider',
  code: '',
  label: '',
  description: '',
  sort_order: 10,
  enabled: true,
})

const newWarehouse = reactive<SystemWarehouseInput>({
  code: '',
  name: '',
  status: 'active',
})

const isPlatformAdmin = computed(() => session.bootstrap?.actor.role === 'platform_admin')
const agentRoutingConfigKey = 'agent.routing.live_room'
const categoryConfig = computed(() =>
  findSystemSettingsCategory(props.category) || findSystemSettingsCategory('general')!,
)
const visibleConfigGroups = computed(() =>
  visibleSystemSettingsGroups(categoryConfig.value, session.bootstrap),
)
const showGeneralSettings = computed(() => props.category === 'general')
const showIntelligenceSettings = computed(() => props.category === 'intelligence')
const showLiveSettings = computed(() => props.category === 'live')
const showCommerceSettings = computed(() => props.category === 'commerce')
const showInventorySettings = computed(() => props.category === 'inventory')

function hasPermission(code: string) {
  if (isPlatformAdmin.value) return true
  const access = session.bootstrap?.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

const canViewGlobal = computed(() => isPlatformAdmin.value)
const canManageAgentPrompts = computed(() => isPlatformAdmin.value || Boolean(session.bootstrap?.staff_access?.is_super_admin))
const agentPromptItems = computed(() =>
  (dashboard.value?.agent_prompt_configs || []).filter((item) => item.key !== agentRoutingConfigKey),
)
const canViewMembershipLimits = computed(() => hasPermission('commercial.membership.view'))
const canManageMembershipLimits = computed(() => hasPermission('system.settings.liveops.manage'))
const canViewIndustry = computed(() => hasPermission('livepolicy.view'))
const canManageIndustry = computed(() => hasPermission('livepolicy.manage_l2'))
const canViewInventorySettings = computed(() => hasPermission('inventory.view'))
const canManageInventorySettings = computed(() => hasPermission('system.settings.inventory.manage'))
const canManageStrategyCenter = computed(() => hasPermission('system.settings.liveops.manage') || canManageAgentPrompts.value)

const strategyCenter = ref<LiveStrategyCenterConfig | null>(null)
const strategySaving = ref(false)
const strategyDraft = ref<LiveStrategyCenterInput>({ rules: [], addressing_mode: 'system', addressing: [] })

function cloneStrategyCenter(value: LiveStrategyCenterConfig): LiveStrategyCenterInput {
  return {
    rules: (value.rules || []).map((item) => ({ ...item, config: item.config ? { ...item.config } : undefined })),
    addressing_mode: value.addressing_mode || 'system',
    addressing: (value.addressing || []).map((item) => ({ ...item })),
  }
}

function strategyRules(category: string) {
  return strategyDraft.value.rules.filter((item) => item.category === category)
}

const strategyCategoryMeta = [
  { key: 'interrupt', title: '打断行为策略', note: '控制回答时像真人一样怎么切入。生效时系统自动优化各策略比例，并保证启用项最低 10%。' },
  { key: 'resume', title: '回归策略', note: '安全条件先筛选，再在候选策略中按权重选择；生效时自动优化比例，并保证启用项最低 20%。' },
  { key: 'interaction', title: '直播间互动策略', note: '欢迎、点赞、关注和弹幕回复。普通互动最低 5%，回复弹幕最低 20%；Core 会按实时流速动态调权。' },
]

function toggleStrategyRule(rule: LiveStrategyRule, enabled: boolean) {
  rule.enabled = enabled
  if (!enabled) {
    rule.base_probability = 0
  } else if (Number(rule.base_probability || 0) < Number(rule.min_probability || 0)) {
    rule.base_probability = Number(rule.min_probability || 0)
  }
}

function addAdminAddressing() {
  const index = strategyDraft.value.addressing.filter((item) => !item.system_default).length + 1
  strategyDraft.value.addressing.push({
    key: `custom_${Date.now()}_${index}`,
    text: '',
    enabled: true,
    probability: 0,
    system_default: false,
  })
}

function removeAdminAddressing(option: LiveAddressingOption) {
  strategyDraft.value.addressing = strategyDraft.value.addressing.filter((item) => item !== option)
}

async function saveStrategyCenter() {
  if (strategySaving.value) return
  strategySaving.value = true
  error.value = ''
  notice.value = ''
  try {
    const saved = await updateSystemLiveStrategyCenter(strategyDraft.value)
    strategyCenter.value = saved
    strategyDraft.value = cloneStrategyCenter(saved)
    notice.value = '直播策略中心已保存并同步到 Core；下一次互动和回归立即按新概率执行。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存直播策略中心失败'
  } finally {
    strategySaving.value = false
  }
}

const categoryMeta: Record<
  DictionaryCategory,
  { title: string; description: string; codeHint: string }
> = {
  logistics_provider: {
    title: '物流公司',
    description: '仓库出库、物流单和售后寄回统一使用这里启用的承运商。',
    codeHint: '例如 SF / JD / ZTO',
  },
  product_unit: {
    title: '产品单位',
    description: '商品、设备、耗材等产品录入时统一从这里选择单位。',
    codeHint: '例如 unit / box / kilogram',
  },
}

const currentDictionaryItems = computed(() =>
  dashboard.value?.dictionaries[dictionaryCategory.value] || [],
)

const filteredDictionaryItems = computed(() => {
  const keyword = dictionarySearch.value.trim().toLowerCase()
  if (!keyword) return currentDictionaryItems.value
  return currentDictionaryItems.value.filter((item) =>
    [item.code, item.label, item.description].some((value) =>
      String(value || '').toLowerCase().includes(keyword),
    ),
  )
})

const dictionaryTotalPages = computed(() =>
  Math.max(1, Math.ceil(filteredDictionaryItems.value.length / dictionaryPageSize)),
)

const pagedDictionaryItems = computed(() => {
  const page = Math.min(dictionaryPage.value, dictionaryTotalPages.value)
  const start = (page - 1) * dictionaryPageSize
  return filteredDictionaryItems.value.slice(start, start + dictionaryPageSize)
})

const filteredWarehouses = computed(() => {
  const keyword = warehouseSearch.value.trim().toLowerCase()
  const rows = dashboard.value?.warehouses || []
  if (!keyword) return rows
  return rows.filter((item) =>
    [item.code, item.name, item.status].some((value) =>
      String(value || '').toLowerCase().includes(keyword),
    ),
  )
})

const warehouseTotalPages = computed(() =>
  Math.max(1, Math.ceil(filteredWarehouses.value.length / warehousePageSize)),
)

const pagedWarehouses = computed(() => {
  const page = Math.min(warehousePage.value, warehouseTotalPages.value)
  const start = (page - 1) * warehousePageSize
  return filteredWarehouses.value.slice(start, start + warehousePageSize)
})

function isProtectedWarehouseCode(code: string) {
  return protectedWarehouseCodes.has(String(code || '').trim().toUpperCase())
}

function syncIndustries(value: LivePolicyIndustry[]) {
  industries.value = value
  for (const item of value) {
    industryDrafts[item.code] = {
      name: item.name,
      sort_order: Number(item.sort_order || 0),
    }
  }
  newIndustry.sort_order = value.length
    ? Math.max(...value.map((item) => Number(item.sort_order || 0))) + 10
    : 10
}

function syncDashboard(value: SystemSettingsDashboard) {
  dashboard.value = value
  for (const setting of value.settings) {
    settingsDraft[setting.key] = setting.value
  }
  for (const item of value.agent_prompt_configs || []) {
    agentPromptDrafts[item.key] = {
      current_value: item.draft_value ?? item.current_value,
      enabled: item.draft_enabled ?? item.enabled,
    }
  }
  for (const items of Object.values(value.dictionaries)) {
    for (const item of items) {
      dictionaryDrafts[item.id] = {
        category: item.category,
        code: item.code,
        label: item.label,
        description: item.description,
        sort_order: item.sort_order,
        enabled: item.enabled,
      }
    }
  }
  for (const item of value.warehouses || []) {
    warehouseDrafts[item.id] = {
      code: item.code,
      name: item.name,
      status: item.status,
    }
  }
  for (const item of value.membership_room_limits || []) {
    membershipRoomLimitDrafts[item.plan_id] = Number(item.room_limit || 3)
  }
}

const shouldLoadDashboard = computed(() =>
  (showGeneralSettings.value && canViewGlobal.value) ||
  (showIntelligenceSettings.value && canManageAgentPrompts.value) ||
  (showLiveSettings.value && (canViewGlobal.value || canViewMembershipLimits.value)) ||
  (showCommerceSettings.value && canViewGlobal.value) ||
  (showInventorySettings.value && canViewInventorySettings.value),
)

function emptyDashboard(): SystemSettingsDashboard {
  return {
    settings: [],
    dictionaries: {},
    warehouses: [],
    membership_room_limits: [],
    agent_prompt_configs: [],
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [settings, industryData, liveStrategy] = await Promise.all([
      shouldLoadDashboard.value ? getSystemSettingsDashboard() : Promise.resolve(emptyDashboard()),
      showLiveSettings.value && canViewIndustry.value
        ? getLivePolicyIndustries()
        : Promise.resolve({ items: [] as LivePolicyIndustry[] }),
      showIntelligenceSettings.value && canManageStrategyCenter.value
        ? getSystemLiveStrategyCenter()
        : Promise.resolve(null),
    ])
    syncDashboard(settings)
    syncIndustries(industryData.items)
    if (liveStrategy) {
      strategyCenter.value = liveStrategy
      strategyDraft.value = cloneStrategyCenter(liveStrategy)
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取系统设置失败'
  } finally {
    loading.value = false
  }
}

const globalDisplaySettingKeys = new Set([
  'site_name',
  'auth_customer_side_label',
  'auth_customer_title_line_1',
  'auth_customer_title_line_2',
  'auth_customer_description',
  'auth_customer_status_label',
  'auth_internal_side_label',
  'auth_internal_title_line_1',
  'auth_internal_title_line_2',
  'auth_internal_description',
  'auth_internal_status_label',
  'internal_agent_name',
  'client_agent_name',
  'footer_enabled',
  'footer_copyright',
  'footer_icp_text',
  'footer_icp_url',
  'footer_police_text',
  'footer_police_url',
  'footer_report_text',
  'footer_report_url',
  'footer_extra_text',
])

const authHomepageSettingRules = [
  { key: 'auth_customer_side_label', label: '客户登录页英文眉标', max: 64 },
  { key: 'auth_customer_title_line_1', label: '客户登录页主标题第一行', max: 48 },
  { key: 'auth_customer_title_line_2', label: '客户登录页主标题第二行', max: 48 },
  { key: 'auth_customer_description', label: '客户登录页说明', max: 240 },
  { key: 'auth_customer_status_label', label: '客户登录页状态文案', max: 64 },
  { key: 'auth_internal_side_label', label: '内部登录页英文眉标', max: 64 },
  { key: 'auth_internal_title_line_1', label: '内部登录页主标题第一行', max: 48 },
  { key: 'auth_internal_title_line_2', label: '内部登录页主标题第二行', max: 48 },
  { key: 'auth_internal_description', label: '内部登录页说明', max: 240 },
  { key: 'auth_internal_status_label', label: '内部登录页状态文案', max: 64 },
]

async function saveGlobalSettings(scope: 'display' | 'commerce' = 'display') {
  if (!dashboard.value || saving.value) return
  if (scope === 'display') {
    const internalAgentName = (settingsDraft.internal_agent_name || '').trim()
    const clientAgentName = (settingsDraft.client_agent_name || '').trim()
    if (!internalAgentName || !clientAgentName) {
      error.value = '后台智能体名称和前端智能体名称都不能为空'
      return
    }
    if ([internalAgentName, clientAgentName].some((name) => [...name].length > 32)) {
      error.value = '智能体名称不能超过 32 个字符'
      return
    }
    settingsDraft.internal_agent_name = internalAgentName
    settingsDraft.client_agent_name = clientAgentName
    for (const rule of authHomepageSettingRules) {
      const value = (settingsDraft[rule.key] || '').trim()
      if (!value) {
        error.value = `${rule.label}不能为空`
        return
      }
      if ([...value].length > rule.max) {
        error.value = `${rule.label}不能超过 ${rule.max} 个字符`
        return
      }
      settingsDraft[rule.key] = value
    }
  } else {
    const holdMinutes = Number(settingsDraft.device_order_hold_minutes || 15)
    if (!Number.isInteger(holdMinutes) || holdMinutes < 1 || holdMinutes > 120) {
      error.value = '设备订单未支付锁库时间必须是 1 到 120 分钟的整数'
      return
    }
  }
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const allowedKeys = scope === 'display' ? globalDisplaySettingKeys : new Set(['device_order_hold_minutes'])
    const result = await updateSystemSettings(
      dashboard.value.settings.filter((item) => allowedKeys.has(item.key)).map((item) => ({
        key: item.key,
        value: settingsDraft[item.key] ?? '',
      })),
    )
    syncDashboard(result)
    notice.value = scope === 'display'
      ? '基础与品牌配置已保存并立即生效。'
      : '订单锁库规则已保存并立即生效。'
    window.dispatchEvent(new CustomEvent('system-config-updated'))
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存系统设置失败'
  } finally {
    saving.value = false
  }
}

async function saveAgentPrompts() {
  if (!dashboard.value || savingAgentPrompts.value) return
  savingAgentPrompts.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await updateAgentPromptConfigs(
      agentPromptItems.value.map((item) => ({
        key: item.key,
        current_value: agentPromptDrafts[item.key]?.current_value ?? item.current_value,
        enabled: agentPromptDrafts[item.key]?.enabled ?? item.enabled,
      })),
    )
    dashboard.value.agent_prompt_configs = result.items
    for (const item of result.items) {
      agentPromptDrafts[item.key] = { current_value: item.draft_value ?? item.current_value, enabled: item.draft_enabled ?? item.enabled }
    }
    notice.value = '模型指令草稿已保存。发布后新请求才会使用。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存模型与智能体配置失败'
  } finally {
    savingAgentPrompts.value = false
  }
}

async function restoreAgentPromptDefault(key: string) {
  if (!dashboard.value || resettingPromptKey.value) return
  resettingPromptKey.value = key
  error.value = ''
  notice.value = ''
  try {
    const result = await resetAgentPromptConfig(key)
    dashboard.value.agent_prompt_configs = result.items
    for (const item of result.items) {
      agentPromptDrafts[item.key] = { current_value: item.draft_value ?? item.current_value, enabled: item.draft_enabled ?? item.enabled }
    }
    notice.value = '已把系统默认内容恢复到草稿，确认后再发布。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '恢复默认模型要求失败'
  } finally {
    resettingPromptKey.value = ''
  }
}

async function publishAgentPrompt(key: string) {
  if (!dashboard.value || publishingPromptKey.value) return
  publishingPromptKey.value = key
  error.value = ''
  notice.value = ''
  try {
    await updateAgentPromptConfigs([{
      key,
      current_value: agentPromptDrafts[key]?.current_value ?? '',
      enabled: agentPromptDrafts[key]?.enabled ?? true,
    }])
    const result = await publishAgentPromptConfig(key)
    dashboard.value.agent_prompt_configs = result.items
    for (const item of result.items) {
      agentPromptDrafts[item.key] = { current_value: item.draft_value ?? item.current_value, enabled: item.draft_enabled ?? item.enabled }
    }
    notice.value = '模型指令已发布，新请求立即使用 V' + (result.items.find((item) => item.key === key)?.version || '') + '。'
    if (promptHistoryKey.value === key) {
      const history = await getAgentPromptHistory(key)
      promptHistories[key] = history.items
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '发布模型指令失败'
  } finally {
    publishingPromptKey.value = ''
  }
}

async function loadAgentPromptHistory(key: string) {
  if (promptHistoryKey.value === key) {
    promptHistoryKey.value = ''
    return
  }
  error.value = ''
  try {
    const result = await getAgentPromptHistory(key)
    promptHistories[key] = result.items
    promptHistoryKey.value = key
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取版本历史失败'
  }
}

async function rollbackAgentPrompt(key: string, version: number) {
  if (!dashboard.value || rollingBackPromptKey.value) return
  rollingBackPromptKey.value = key
  error.value = ''
  notice.value = ''
  try {
    const result = await rollbackAgentPromptConfig(key, version)
    dashboard.value.agent_prompt_configs = result.items
    for (const item of result.items) {
      agentPromptDrafts[item.key] = { current_value: item.draft_value ?? item.current_value, enabled: item.draft_enabled ?? item.enabled }
    }
    notice.value = '已回滚到 V' + version + ' 的内容，并作为新版本发布。'
    const history = await getAgentPromptHistory(key)
    promptHistories[key] = history.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '回滚模型指令失败'
  } finally {
    rollingBackPromptKey.value = ''
  }
}

async function saveFinancePolicy() {
  if (!canViewGlobal.value || !dashboard.value || savingFinancePolicy.value) return
  savingFinancePolicy.value = true
  error.value = ''
  notice.value = ''
  try {
    const value = settingsDraft[financeReviewSettingKey] === 'false' ? 'false' : 'true'
    const result = await updateSystemSettings([{ key: financeReviewSettingKey, value }])
    // Do not overwrite unrelated unsaved settings in the same page.
    const saved = result.settings.find((item) => item.key === financeReviewSettingKey)
    if (!saved) throw new Error('审核配置保存结果缺失，请重新读取核对')
    settingsDraft[financeReviewSettingKey] = saved.value
    dashboard.value.settings = dashboard.value.settings.map((item) => item.key === financeReviewSettingKey ? saved : item)
    notice.value = saved.value === 'false' ? '已保存：不强制分人，有审核权限的同一人可以办理；审核记录仍完整保留。' : '已保存：强制经办人与审核人不同。'
    window.dispatchEvent(new CustomEvent('system-config-updated'))
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存审核配置失败'
  } finally {
    savingFinancePolicy.value = false
  }
}

function draftFontSize(key: string, fallback: number) {
  const value = Number(settingsDraft[key])
  return Number.isFinite(value) ? value : fallback
}

async function saveRuleTypography() {
  if (!dashboard.value || savingRuleTypography.value) return
  const fields = [
    { key: 'live_policy_rule_title_font_size', label: '规则标题', min: 16, max: 40, fallback: 26 },
    { key: 'live_policy_rule_body_font_size', label: '规则正文', min: 14, max: 36, fallback: 24 },
    { key: 'live_policy_rule_meta_font_size', label: '规则辅助文字', min: 12, max: 28, fallback: 20 },
    { key: 'live_policy_test_title_font_size', label: '测试结果主标题', min: 18, max: 32, fallback: 22 },
    { key: 'live_policy_test_body_font_size', label: '测试结果内容', min: 16, max: 28, fallback: 18 },
    { key: 'live_policy_test_meta_font_size', label: '测试结果辅助文字', min: 14, max: 24, fallback: 16 },
  ]
  const updates = fields.map((field) => {
    const value = Number(settingsDraft[field.key] || field.fallback)
    return { ...field, value }
  })
  const invalid = updates.find(
    (item) => !Number.isInteger(item.value) || item.value < item.min || item.value > item.max,
  )
  if (invalid) {
    error.value = invalid.label + '字号必须是 ' + invalid.min + ' 到 ' + invalid.max + ' 的整数'
    return
  }

  savingRuleTypography.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await updateSystemSettings(
      updates.map((item) => ({
        key: item.key,
        value: String(item.value),
      })),
    )
    syncDashboard(result)
    notice.value = '直播规则文字大小已保存并立即生效。'
    window.dispatchEvent(new CustomEvent('system-config-updated'))
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存直播规则文字大小失败'
  } finally {
    savingRuleTypography.value = false
  }
}

async function saveMembershipLimits() {
  if (!canManageMembershipLimits.value || !dashboard.value || savingMembershipLimits.value) return
  const items = (dashboard.value.membership_room_limits || []).map((item) => ({
    plan_id: item.plan_id,
    room_limit: Number(membershipRoomLimitDrafts[item.plan_id] ?? 3),
  }))
  if (!items.length) {
    error.value = '当前还没有会员等级可配置'
    return
  }
  if (items.some((item) => !Number.isInteger(item.room_limit) || item.room_limit < 1 || item.room_limit > 10)) {
    error.value = '每个会员等级的直播间上限必须是 1 到 10 的整数'
    return
  }
  savingMembershipLimits.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await updateMembershipRoomLimits(items)
    syncDashboard(result)
    notice.value = '会员直播间上限已保存。直播运维分配客户配额时会立即受此上限约束。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存会员直播间上限失败'
  } finally {
    savingMembershipLimits.value = false
  }
}

async function saveIndustry(item: LivePolicyIndustry) {
  if (!canManageIndustry.value || savingIndustryCode.value) return
  const draft = industryDrafts[item.code]
  if (!draft?.name.trim()) {
    error.value = '行业名称不能为空。'
    return
  }
  savingIndustryCode.value = item.code
  error.value = ''
  notice.value = ''
  try {
    await upsertLivePolicyIndustry({
      code: item.code,
      name: draft.name.trim(),
      parent_code: item.parent_code || '',
      status: 'active',
      sort_order: Number(draft.sort_order || 0),
    })
    const result = await getLivePolicyIndustries()
    syncIndustries(result.items)
    notice.value = '行业目录已保存，直播策略行业目录已同步。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存行业目录失败'
  } finally {
    savingIndustryCode.value = ''
  }
}

async function addIndustry() {
  if (!canManageIndustry.value || addingIndustry.value) return
  const code = newIndustry.code.trim().toLowerCase()
  const name = newIndustry.name.trim()
  if (!/^[a-z0-9][a-z0-9_-]{1,63}$/.test(code)) {
    error.value = '行业编码需为至少 2 位小写字母、数字、下划线或短横线。'
    return
  }
  if (!name) {
    error.value = '行业名称不能为空。'
    return
  }
  addingIndustry.value = true
  error.value = ''
  notice.value = ''
  try {
    await upsertLivePolicyIndustry({
      code,
      name,
      parent_code: '',
      status: 'active',
      sort_order: Number(newIndustry.sort_order || 0),
    })
    newIndustry.code = ''
    newIndustry.name = ''
    const result = await getLivePolicyIndustries()
    syncIndustries(result.items)
    notice.value = '新行业已添加，会自动出现在直播策略行业目录中。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '新增行业失败'
  } finally {
    addingIndustry.value = false
  }
}

function selectCategory(category: DictionaryCategory) {
  dictionaryCategory.value = category
  newDictionary.category = category
  dictionarySearch.value = ''
  dictionaryPage.value = 1
  const rows = dashboard.value?.dictionaries[category] || []
  newDictionary.sort_order = rows.length
    ? Math.max(...rows.map((item) => item.sort_order)) + 10
    : 10
}

async function saveDictionaryItem(item: SystemDictionaryItem) {
  if (!canManageInventorySettings.value) return
  const draft = dictionaryDrafts[item.id]
  if (!draft || savingDictionaryId.value !== null) return
  savingDictionaryId.value = item.id
  error.value = ''
  notice.value = ''
  try {
    await updateSystemDictionaryItem(item.id, {
      ...draft,
      category: item.category,
      code: draft.code.trim(),
      label: draft.label.trim(),
      description: draft.description.trim(),
      sort_order: Number(draft.sort_order || 0),
    })
    await load()
    notice.value = '字典项已保存。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存字典项失败'
  } finally {
    savingDictionaryId.value = null
  }
}

async function addDictionaryItem() {
  if (!canManageInventorySettings.value || addingDictionary.value) return
  if (!newDictionary.code.trim() || !newDictionary.label.trim()) {
    error.value = '编码和显示名称不能为空。'
    return
  }
  addingDictionary.value = true
  error.value = ''
  notice.value = ''
  try {
    await createSystemDictionaryItem({
      ...newDictionary,
      category: dictionaryCategory.value,
      code: newDictionary.code.trim(),
      label: newDictionary.label.trim(),
      description: newDictionary.description.trim(),
      sort_order: Number(newDictionary.sort_order || 0),
    })
    newDictionary.code = ''
    newDictionary.label = ''
    newDictionary.description = ''
    await load()
    selectCategory(dictionaryCategory.value)
    notice.value = '新的系统字典项已添加。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '新增字典项失败'
  } finally {
    addingDictionary.value = false
  }
}

async function saveWarehouse(item: InventoryWarehouse) {
  if (!canManageInventorySettings.value) return
  const draft = warehouseDrafts[item.id]
  if (!draft || savingWarehouseId.value !== null) return
  if (!draft.code.trim() || !draft.name.trim()) {
    error.value = '仓库编码和仓库名称不能为空。'
    return
  }
  savingWarehouseId.value = item.id
  error.value = ''
  notice.value = ''
  try {
    const protectedWarehouse = isProtectedWarehouseCode(item.code)
    await updateSystemWarehouse(item.id, {
      code: protectedWarehouse ? item.code : draft.code.trim().toUpperCase(),
      name: draft.name.trim(),
      status: protectedWarehouse ? 'active' : draft.status,
    })
    await load()
    notice.value = '仓库配置已保存，业务下拉已同步。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存仓库失败'
  } finally {
    savingWarehouseId.value = null
  }
}

async function addWarehouse() {
  if (!canManageInventorySettings.value || addingWarehouse.value) return
  if (!newWarehouse.code.trim() || !newWarehouse.name.trim()) {
    error.value = '仓库编码和仓库名称不能为空。'
    return
  }
  addingWarehouse.value = true
  error.value = ''
  notice.value = ''
  try {
    await createSystemWarehouse({
      code: newWarehouse.code.trim().toUpperCase(),
      name: newWarehouse.name.trim(),
      status: newWarehouse.status || 'active',
    })
    newWarehouse.code = ''
    newWarehouse.name = ''
    newWarehouse.status = 'active'
    warehouseSearch.value = ''
    warehousePage.value = 1
    await load()
    notice.value = '新仓库已添加，可立即用于库存、物流和售后下拉选择。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '新增仓库失败'
  } finally {
    addingWarehouse.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page system-settings-page">
    <ModulePageNav
      :context="isPlatformAdmin ? 'workspace-admin' : 'workspace-staff'"
      :active-title="categoryConfig.title"
      section-title="系统设置"
      section-to="/system/settings"
      section-icon="设"
    />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">{{ categoryConfig.kicker }}</p>
        <h2>{{ categoryConfig.title }}</h2>
        <p>{{ categoryConfig.description }}</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '读取中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <section v-for="group in visibleConfigGroups" :key="group.key" class="settings-config-group">
      <header>
        <div>
          <h3>{{ group.title }}</h3>
          <p>{{ group.description }}</p>
        </div>
      </header>
      <div class="settings-config-entry-grid">
        <RouterLink v-for="entry in group.entries" :key="entry.key" :to="entry.to" class="settings-config-entry">
          <i>{{ entry.icon }}</i>
          <div>
            <strong>{{ entry.title }}</strong>
            <p>{{ entry.description }}</p>
          </div>
          <span v-if="entry.badge">{{ entry.badge }}</span>
          <b>›</b>
        </RouterLink>
      </div>
    </section>

    <section v-if="showCommerceSettings && canViewGlobal" id="finance-review-policy" class="system-settings-card finance-review-settings">
      <header><div><h3>财务审核规则</h3><p>适用于收款审核及充值、退款、奖励、AI 时长审批。仅控制是否必须分人，不取消权限和实际入账校验。</p></div><button type="button" class="primary-button" :disabled="loading || savingFinancePolicy || !dashboard" @click="saveFinancePolicy">{{ savingFinancePolicy ? '保存中…' : '保存审核规则' }}</button></header>
      <div class="system-settings-form">
        <label class="system-setting-wide finance-review-toggle"><input type="checkbox" :checked="settingsDraft[financeReviewSettingKey] !== 'false'" :disabled="loading || savingFinancePolicy" @change="settingsDraft[financeReviewSettingKey] = ($event.target as HTMLInputElement).checked ? 'true' : 'false'"/><span>强制经办人与审核人不同</span></label>
        <p class="system-setting-wide">{{ settingsDraft[financeReviewSettingKey] === 'false' ? '不强制：适合一人兼岗。本人提交或补件的单据也可由本人审核，前提是具备对应审核权限。' : '强制：本人提交或补件的单据，必须由另一位有审核权限的人处理。' }}经办人、审核人、时间、审核结果均保留。点击“保存审核规则”后生效。</p>
      </div>
    </section>

    <section v-if="showGeneralSettings && canViewGlobal" id="global-display" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">GLOBAL TEXT</span>
          <h3>全局文字</h3>
          <p>配置登录首页宣传文案、系统品牌名称、前后台智能体名称和页面底部文字。</p>
        </div>
        <button class="primary-button" type="button" :disabled="saving || loading" @click="saveGlobalSettings('display')">
          {{ saving ? '保存中...' : '保存全局配置' }}
        </button>
      </header>

      <div v-if="loading && !dashboard" class="panel-loading">正在读取系统设置...</div>

      <div v-else class="system-settings-form">
        <label class="system-setting-wide">
          <span>系统显示名称</span>
          <input v-model="settingsDraft.site_name" type="text" placeholder="例如 小蓝搭子" />
        </label>

        <div class="system-setting-subsection system-setting-wide">
          <strong>客户与代理登录首页</strong>
          <p>对应客户、代理登录页右侧大图区域；注册页同步复用英文眉标和两行主标题。</p>
        </div>

        <label>
          <span>英文眉标</span>
          <input v-model="settingsDraft.auth_customer_side_label" type="text" maxlength="64" placeholder="BANBO AI LIVE" />
        </label>
        <label>
          <span>底部状态文案</span>
          <input v-model="settingsDraft.auth_customer_status_label" type="text" maxlength="64" placeholder="LIVE INTELLIGENCE ONLINE" />
        </label>
        <label>
          <span>主标题第一行</span>
          <input v-model="settingsDraft.auth_customer_title_line_1" type="text" maxlength="48" placeholder="AI直播搭子，" />
        </label>
        <label>
          <span>主标题第二行</span>
          <input v-model="settingsDraft.auth_customer_title_line_2" type="text" maxlength="48" placeholder="让你直播不再冷场。" />
        </label>
        <label class="system-setting-wide">
          <span>说明文字</span>
          <textarea v-model="settingsDraft.auth_customer_description" maxlength="240" rows="3" placeholder="登录首页主标题下方的说明文字"></textarea>
        </label>

        <div class="system-setting-subsection system-setting-wide">
          <strong>内部员工登录首页</strong>
          <p>使用内部员工入口时显示，和客户侧文案分开维护。</p>
        </div>

        <label>
          <span>英文眉标</span>
          <input v-model="settingsDraft.auth_internal_side_label" type="text" maxlength="64" placeholder="AI CONTROL CENTER" />
        </label>
        <label>
          <span>底部状态文案</span>
          <input v-model="settingsDraft.auth_internal_status_label" type="text" maxlength="64" placeholder="LIVE INTELLIGENCE ONLINE" />
        </label>
        <label>
          <span>主标题第一行</span>
          <input v-model="settingsDraft.auth_internal_title_line_1" type="text" maxlength="48" placeholder="数据驱动直播运维，" />
        </label>
        <label>
          <span>主标题第二行</span>
          <input v-model="settingsDraft.auth_internal_title_line_2" type="text" maxlength="48" placeholder="全局尽在掌握。" />
        </label>
        <label class="system-setting-wide">
          <span>说明文字</span>
          <textarea v-model="settingsDraft.auth_internal_description" maxlength="240" rows="3" placeholder="内部员工登录首页主标题下方的说明文字"></textarea>
        </label>

        <div class="system-setting-subsection system-setting-wide">
          <strong>智能体与全局页脚</strong>
          <p>统一控制系统内的智能体称呼、页脚、备案与举报信息。</p>
        </div>

        <label class="system-setting-wide">
          <span>后台智能体名称</span>
          <input
            v-model="settingsDraft.internal_agent_name"
            type="text"
            maxlength="32"
            placeholder="小蓝工作搭子"
          />
          <small>内部员工、管理人员和超级系统管理员使用的统一智能体名称。</small>
        </label>

        <label class="system-setting-wide">
          <span>前端智能体名称</span>
          <input
            v-model="settingsDraft.client_agent_name"
            type="text"
            maxlength="32"
            placeholder="小蓝直播搭子"
          />
          <small>终端客户和代理账号使用的统一智能体名称。</small>
        </label>

        <label class="system-setting-toggle">
          <span>显示全局页脚</span>
          <input
            type="checkbox"
            :checked="settingsDraft.footer_enabled === 'true'"
            @change="settingsDraft.footer_enabled = ($event.target as HTMLInputElement).checked ? 'true' : 'false'"
          />
        </label>

        <label class="system-setting-wide">
          <span>版权文字</span>
          <input v-model="settingsDraft.footer_copyright" type="text" placeholder="© 2026 公司名称" />
        </label>

        <label>
          <span>ICP备案文字</span>
          <input v-model="settingsDraft.footer_icp_text" type="text" placeholder="例如 粤ICP备..." />
        </label>
        <label>
          <span>ICP备案链接</span>
          <input v-model="settingsDraft.footer_icp_url" type="url" placeholder="https://..." />
        </label>

        <label>
          <span>公安备案文字</span>
          <input v-model="settingsDraft.footer_police_text" type="text" placeholder="例如 粤公网安备..." />
        </label>
        <label>
          <span>公安备案链接</span>
          <input v-model="settingsDraft.footer_police_url" type="url" placeholder="https://..." />
        </label>

        <label>
          <span>举报中心文字</span>
          <input v-model="settingsDraft.footer_report_text" type="text" placeholder="互联网不良信息举报中心" />
        </label>
        <label>
          <span>举报中心链接</span>
          <input v-model="settingsDraft.footer_report_url" type="url" placeholder="https://..." />
        </label>

        <label class="system-setting-wide">
          <span>页脚补充文字</span>
          <input v-model="settingsDraft.footer_extra_text" type="text" placeholder="可留空" />
        </label>
      </div>

      <div class="system-footer-preview">
        <span>预览</span>
        <div>
          <span v-if="settingsDraft.footer_copyright">{{ settingsDraft.footer_copyright }}</span>
          <span v-if="settingsDraft.footer_icp_text">| {{ settingsDraft.footer_icp_text }}</span>
          <span v-if="settingsDraft.footer_police_text">| {{ settingsDraft.footer_police_text }}</span>
          <span v-if="settingsDraft.footer_report_text">| {{ settingsDraft.footer_report_text }}</span>
          <span v-if="settingsDraft.footer_extra_text">| {{ settingsDraft.footer_extra_text }}</span>
        </div>
      </div>
    </section>

    <section v-if="showIntelligenceSettings && canManageAgentPrompts" id="agent-prompts" class="system-settings-card agent-prompt-settings">
      <header>
        <div>
          <span class="section-kicker">MODEL & AGENT CONFIG</span>
          <h3>模型与智能体配置</h3>
          <p>统一维护系统与大模型之间的其他业务要求。智能体路由配置已拆到专用维护页；这里仍采用草稿、发布、版本和回滚机制。</p>
        </div>
        <button class="primary-button" type="button" :disabled="savingAgentPrompts || loading || !dashboard" @click="saveAgentPrompts">
          {{ savingAgentPrompts ? '保存中...' : '保存全部草稿' }}
        </button>
      </header>

      <div v-if="agentPromptItems.length" class="agent-prompt-list">
        <article v-for="item in agentPromptItems" :key="item.key" class="agent-prompt-item">
          <div class="agent-prompt-item-head">
            <div>
              <strong>{{ item.name }}</strong>
              <code>{{ item.key }}</code>
            </div>
            <label class="system-setting-toggle compact-toggle">
              <span>{{ agentPromptDrafts[item.key]?.enabled ? '启用' : '停用' }}</span>
              <input
                type="checkbox"
                :checked="agentPromptDrafts[item.key]?.enabled"
                @change="agentPromptDrafts[item.key].enabled = ($event.target as HTMLInputElement).checked"
              />
            </label>
          </div>
          <p>{{ item.description }}</p>
          <small>场景：{{ item.scene }} · 已发布 V{{ item.version }} · 最近发布 {{ new Date(item.updated_at).toLocaleString() }}</small>
          <small v-if="agentPromptDrafts[item.key] && (agentPromptDrafts[item.key].current_value !== item.current_value || agentPromptDrafts[item.key].enabled !== item.enabled)" class="agent-prompt-draft-badge">有未发布草稿</small>
          <textarea
            v-model="agentPromptDrafts[item.key].current_value"
            rows="7"
            spellcheck="false"
          />
          <div class="agent-prompt-actions">
            <button class="ghost-button" type="button" :disabled="resettingPromptKey === item.key" @click="restoreAgentPromptDefault(item.key)">
              {{ resettingPromptKey === item.key ? '恢复中...' : '恢复默认到草稿' }}
            </button>
            <button class="ghost-button" type="button" @click="loadAgentPromptHistory(item.key)">
              {{ promptHistoryKey === item.key ? '收起版本历史' : '版本历史' }}
            </button>
            <button class="primary-button" type="button" :disabled="publishingPromptKey === item.key" @click="publishAgentPrompt(item.key)">
              {{ publishingPromptKey === item.key ? '发布中...' : '发布' }}
            </button>
          </div>
          <div v-if="promptHistoryKey === item.key" class="agent-prompt-history">
            <article v-for="history in promptHistories[item.key] || []" :key="history.version">
              <div>
                <strong>V{{ history.version }}</strong>
                <span>{{ history.operation }}</span>
                <small>{{ new Date(history.created_at).toLocaleString() }}</small>
              </div>
              <pre>{{ history.value }}</pre>
              <button
                class="ghost-button"
                type="button"
                :disabled="rollingBackPromptKey === item.key || history.version === item.version"
                @click="rollbackAgentPrompt(item.key, history.version)"
              >{{ history.version === item.version ? '当前版本' : '回滚到此版本' }}</button>
            </article>
          </div>
        </article>
      </div>
      <div v-else class="panel-loading">暂无模型配置，刷新后重试。</div>
    </section>

    <LiveContentPolicyPanel v-if="showIntelligenceSettings && isPlatformAdmin" />
    <section v-if="showIntelligenceSettings && canManageStrategyCenter" id="strategy-center" class="system-settings-card live-strategy-center-card">
      <header>
        <div>
          <span class="section-kicker">LIVE STRATEGY CENTER</span>
          <h3>直播策略中心</h3>
          <p>配置打断行为、主线回归、直播互动和称呼。这里设置基础概率，Core 会先执行安全与语义过滤，再结合实时流速动态调整。</p>
        </div>
        <button class="primary-button" type="button" :disabled="strategySaving || !strategyCenter" @click="saveStrategyCenter">
          {{ strategySaving ? '保存同步中…' : '保存并同步 Core' }}
        </button>
      </header>

      <div v-if="strategyCenter" class="strategy-center-body">
        <section v-for="group in strategyCategoryMeta" :key="group.key" class="strategy-center-group">
          <div class="strategy-center-group-head">
            <div>
              <h4>{{ group.title }}</h4>
              <p>{{ group.note }}</p>
            </div>
          </div>

          <div class="strategy-rule-grid">
            <article v-for="rule in strategyRules(group.key)" :key="group.key + ':' + rule.key" class="strategy-rule-card" :class="{ disabled: !rule.enabled }">
              <div class="strategy-rule-title">
                <label class="strategy-enable">
                  <input type="checkbox" :checked="rule.enabled" @change="toggleStrategyRule(rule, ($event.target as HTMLInputElement).checked)" />
                  <span>{{ rule.enabled ? '启用' : '停用' }}</span>
                </label>
                <code>{{ rule.key }}</code>
              </div>
              <label class="strategy-field">
                <span>中文名称</span>
                <input v-model="rule.name" type="text" maxlength="40" />
              </label>
              <label class="strategy-field">
                <span>说明</span>
                <textarea v-model="rule.description" rows="2" maxlength="180"></textarea>
              </label>
              <label class="strategy-probability">
                <span>基础出现概率</span>
                <div>
                  <input
                    v-model.number="rule.base_probability"
                    type="number"
                    min="0"
                    max="100"
                    step="1"
                    :disabled="!rule.enabled"
                  />
                  <b>%</b>
                </div>
                <small>启用后最低 {{ rule.min_probability }}%</small>
              </label>
            </article>
          </div>
        </section>

        <section class="strategy-center-group addressing-strategy-group">
          <div class="strategy-center-group-head">
            <div>
              <h4>称呼策略</h4>
              <p>系统默认称呼会直接给终端用户查看；用户可切换自己的称呼方案。终端自定义称呼保存前必须经过大模型安全审核。</p>
            </div>
            <div class="addressing-mode-switch">
              <label><input v-model="strategyDraft.addressing_mode" type="radio" value="system" />系统称呼</label>
              <label><input v-model="strategyDraft.addressing_mode" type="radio" value="custom" />自定义称呼</label>
              <button class="ghost-button" type="button" @click="addAdminAddressing">新增称呼</button>
            </div>
          </div>
          <div class="addressing-option-grid">
            <article v-for="option in strategyDraft.addressing" :key="option.key" class="addressing-option-card">
              <label class="strategy-enable">
                <input v-model="option.enabled" type="checkbox" />
                <span>{{ option.enabled ? '启用' : '停用' }}</span>
              </label>
              <input v-model="option.text" type="text" maxlength="12" placeholder="称呼" />
              <label>
                <span>随机概率</span>
                <input v-model.number="option.probability" type="number" min="0" max="100" step="1" :disabled="!option.enabled" />
                <b>%</b>
              </label>
              <small>{{ option.system_default ? '系统默认称呼' : '自定义称呼' }}</small>
              <button v-if="!option.system_default" class="ghost-button" type="button" @click="removeAdminAddressing(option)">删除</button>
            </article>
          </div>
        </section>
      </div>
      <div v-else class="panel-loading">正在读取直播策略中心…</div>
    </section>

    <section v-if="showLiveSettings && canViewGlobal" id="live-typography" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">LIVE POLICY TYPOGRAPHY</span>
          <h3>直播规则文字大小</h3>
          <p>规则列表与测试结果分别配置，互不影响。</p>
        </div>
        <button
          class="primary-button"
          type="button"
          :disabled="savingRuleTypography || loading"
          @click="saveRuleTypography"
        >
          {{ savingRuleTypography ? '保存中...' : '保存文字大小' }}
        </button>
      </header>

      <div class="system-settings-form system-rule-font-settings">
        <label>
          <span>规则列表标题（px）</span>
          <input
            v-model="settingsDraft.live_policy_rule_title_font_size"
            type="number"
            min="16"
            max="40"
            step="1"
          />
          <small>范围 16–40，当前默认 26。</small>
        </label>
        <label>
          <span>规则列表正文（px）</span>
          <input
            v-model="settingsDraft.live_policy_rule_body_font_size"
            type="number"
            min="14"
            max="36"
            step="1"
          />
          <small>范围 14–36，当前默认 24。</small>
        </label>
        <label>
          <span>规则列表辅助文字（px）</span>
          <input
            v-model="settingsDraft.live_policy_rule_meta_font_size"
            type="number"
            min="12"
            max="28"
            step="1"
          />
          <small>范围 12–28，当前默认 20。</small>
        </label>
        <label>
          <span>测试结果主标题（px）</span>
          <input
            v-model="settingsDraft.live_policy_test_title_font_size"
            type="number"
            min="18"
            max="32"
            step="1"
          />
          <small>范围 18–32，当前默认 22。</small>
        </label>
        <label>
          <span>测试结果内容（px）</span>
          <input
            v-model="settingsDraft.live_policy_test_body_font_size"
            type="number"
            min="16"
            max="28"
            step="1"
          />
          <small>范围 16–28，当前默认 18。</small>
        </label>
        <label>
          <span>测试结果辅助文字（px）</span>
          <input
            v-model="settingsDraft.live_policy_test_meta_font_size"
            type="number"
            min="14"
            max="24"
            step="1"
          />
          <small>范围 14–24，当前默认 16。</small>
        </label>
      </div>

      <div class="system-live-policy-font-preview">
        <span>规则列表预览</span>
        <div>
          <strong :style="{ fontSize: draftFontSize('live_policy_rule_title_font_size', 26) + 'px' }">
            事实真实性
          </strong>
          <p :style="{ fontSize: draftFontSize('live_policy_rule_body_font_size', 24) + 'px' }">
            不得编造商品信息、价格、库存、优惠或商家承诺；信息无法确认时应明确说明需要核实。
          </p>
          <small :style="{ fontSize: draftFontSize('live_policy_rule_meta_font_size', 20) + 'px' }">
            规则层.事实真实性.6384c844
          </small>
        </div>
      </div>

      <div class="system-live-policy-font-preview">
        <span>测试结果预览</span>
        <div>
          <strong :style="{ fontSize: draftFontSize('live_policy_test_title_font_size', 22) + 'px' }">
            数据依据
          </strong>
          <p :style="{ fontSize: draftFontSize('live_policy_test_body_font_size', 18) + 'px' }">
            当前实时库存数量、商家承诺的发货时效及物流状态。
          </p>
          <small :style="{ fontSize: draftFontSize('live_policy_test_meta_font_size', 16) + 'px' }">
            规则层 · 事实真实性
          </small>
        </div>
      </div>
    </section>

    <section v-if="showCommerceSettings && canViewGlobal" id="device-stock-hold" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">COMMERCE STOCK RULE</span>
          <h3>交易库存规则</h3>
          <p>设备订单生成后立即锁定真实设备，未支付超时后自动取消订单并恢复销售库存。</p>
        </div>
        <button class="primary-button" type="button" :disabled="saving || loading" @click="saveGlobalSettings('commerce')">
          {{ saving ? '保存中...' : '保存库存规则' }}
        </button>
      </header>
      <div class="system-settings-form">
        <label>
          <span>未支付锁库时间（分钟）</span>
          <input
            v-model="settingsDraft.device_order_hold_minutes"
            type="number"
            min="1"
            max="120"
            step="1"
          />
          <small>默认 15 分钟。超时未支付会释放具体 SN，并把销售库存恢复。</small>
        </label>
      </div>
    </section>

    <section v-if="showLiveSettings && canViewIndustry" id="live-industries" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">LIVE INDUSTRY DIRECTORY</span>
          <h3>直播行业目录</h3>
          <p>这里配置的行业会直接同步到直播策略中的“行业目录”。普通运维可查看，直播运维管理人员可添加和修改。</p>
        </div>
      </header>

      <div v-if="canManageIndustry" class="system-industry-add">
        <input v-model="newIndustry.code" type="text" placeholder="行业编码，例如 tea_drink" />
        <input v-model="newIndustry.name" type="text" placeholder="行业名称，例如 茶饮咖啡" />
        <input v-model.number="newIndustry.sort_order" type="number" min="0" step="10" placeholder="排序" />
        <button class="primary-button" type="button" :disabled="addingIndustry" @click="addIndustry">
          {{ addingIndustry ? '添加中...' : '＋ 添加行业' }}
        </button>
      </div>

      <div class="system-dictionary-table-wrap">
        <table class="system-dictionary-table system-industry-table">
          <thead>
            <tr>
              <th>行业名称</th>
              <th>行业编码</th>
              <th>排序</th>
              <th>状态</th>
              <th v-if="canManageIndustry">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in industries" :key="item.code">
              <td>
                <input
                  v-model="industryDrafts[item.code].name"
                  type="text"
                  :disabled="!canManageIndustry"
                />
              </td>
              <td><strong>{{ item.code }}</strong></td>
              <td>
                <input
                  v-model.number="industryDrafts[item.code].sort_order"
                  type="number"
                  min="0"
                  step="10"
                  :disabled="!canManageIndustry"
                />
              </td>
              <td><span class="system-dictionary-source">{{ item.status === 'active' ? '启用' : item.status }}</span></td>
              <td v-if="canManageIndustry">
                <button
                  class="ghost-button"
                  type="button"
                  :disabled="savingIndustryCode === item.code"
                  @click="saveIndustry(item)"
                >
                  {{ savingIndustryCode === item.code ? '保存中' : '保存' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!industries.length" class="empty-state">暂无行业目录。</div>
      </div>
    </section>

    <section v-if="showLiveSettings && canViewMembershipLimits" id="membership-room-limits" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">MEMBERSHIP ROOM POLICY</span>
          <h3>会员直播间上限</h3>
          <p>会员等级决定客户可分配的直播间最高数量。无会员或未单独配置时默认 3 个，平台绝对上限 10 个。</p>
        </div>
        <button
          v-if="canManageMembershipLimits"
          class="primary-button"
          type="button"
          :disabled="savingMembershipLimits || loading"
          @click="saveMembershipLimits"
        >
          {{ savingMembershipLimits ? '保存中...' : '保存会员上限' }}
        </button>
      </header>

      <div class="membership-room-policy-summary">
        <div><span>无会员 / 未配置</span><strong>3 个</strong></div>
        <div><span>平台绝对上限</span><strong>10 个</strong></div>
        <p>直播运维可给单个客户设置更低配额，但不能超过客户当前会员等级上限。</p>
      </div>

      <div class="system-dictionary-table-wrap">
        <table class="system-dictionary-table membership-room-policy-table">
          <thead>
            <tr>
              <th>会员等级</th>
              <th>会员编码</th>
              <th>状态</th>
              <th>最大直播间数量</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in dashboard?.membership_room_limits || []" :key="item.plan_id">
              <td><strong>{{ item.plan_name }}</strong></td>
              <td>{{ item.plan_code }}</td>
              <td><span class="system-dictionary-source">{{ item.plan_status }}</span></td>
              <td>
                <div class="membership-room-limit-input">
                  <input
                    v-model.number="membershipRoomLimitDrafts[item.plan_id]"
                    type="number"
                    min="1"
                    max="10"
                    step="1"
                    :disabled="!canManageMembershipLimits"
                  />
                  <span>个</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="dashboard && !(dashboard.membership_room_limits || []).length" class="empty-state">
          暂无会员等级。先在“活动营销 → 会员方案”中建立会员方案后，这里会自动出现对应配置。
        </div>
      </div>
    </section>

    <section v-if="showInventorySettings && canViewInventorySettings" id="warehouses" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">WAREHOUSE MASTER DATA</span>
          <h3>仓库管理</h3>
          <p>统一维护真实仓库主数据。库存、出库、物流和售后只显示已启用的仓库。</p>
        </div>
      </header>

      <div class="system-dictionary-intro">
        <div>
          <strong>业务仓库</strong>
          <span>系统仓可修改显示名称，但编码和启用状态受保护；自定义仓库可停用。</span>
        </div>
        <input
          v-model="warehouseSearch"
          type="search"
          placeholder="搜索仓库编码 / 名称 / 状态"
          @input="warehousePage = 1"
        />
      </div>

      <div v-if="canManageInventorySettings" class="system-warehouse-add">
        <input v-model="newWarehouse.code" type="text" placeholder="仓库编码，例如 NC_01" />
        <input v-model="newWarehouse.name" type="text" placeholder="仓库名称" />
        <select v-model="newWarehouse.status">
          <option value="active">启用</option>
          <option value="inactive">停用</option>
        </select>
        <button class="primary-button" type="button" :disabled="addingWarehouse" @click="addWarehouse">
          {{ addingWarehouse ? '添加中...' : '＋ 添加仓库' }}
        </button>
      </div>

      <div class="system-dictionary-table-wrap">
        <table class="system-dictionary-table system-warehouse-table">
          <thead>
            <tr>
              <th>仓库编码</th>
              <th>仓库名称</th>
              <th>状态</th>
              <th>类型</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedWarehouses" :key="item.id">
              <td>
                <input
                  v-model="warehouseDrafts[item.id].code"
                  type="text"
                  :disabled="!canManageInventorySettings || isProtectedWarehouseCode(item.code)"
                  :title="isProtectedWarehouseCode(item.code) ? '系统仓库编码用于业务流程，不允许修改' : ''"
                />
              </td>
              <td><input v-model="warehouseDrafts[item.id].name" type="text" :disabled="!canManageInventorySettings" /></td>
              <td>
                <select
                  v-model="warehouseDrafts[item.id].status"
                  :disabled="!canManageInventorySettings || isProtectedWarehouseCode(item.code)"
                >
                  <option value="active">启用</option>
                  <option value="inactive">停用</option>
                </select>
              </td>
              <td>
                <span class="system-dictionary-source">
                  {{ isProtectedWarehouseCode(item.code) ? '系统仓' : '自定义' }}
                </span>
              </td>
              <td>
                <button
                  v-if="canManageInventorySettings"
                  class="ghost-button"
                  type="button"
                  :disabled="savingWarehouseId === item.id"
                  @click="saveWarehouse(item)"
                >
                  {{ savingWarehouseId === item.id ? '保存中' : '保存' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!filteredWarehouses.length" class="empty-state">暂无符合条件的仓库。</div>
      </div>

      <PaginationBar
        :page="Math.min(warehousePage, warehouseTotalPages)"
        :total-pages="warehouseTotalPages"
        :total="filteredWarehouses.length"
        :page-size="warehousePageSize"
        @update:page="warehousePage = $event"
      />
    </section>

    <section v-if="showInventorySettings && canViewInventorySettings" id="inventory-dictionaries" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">SYSTEM DICTIONARY</span>
          <h3>系统字典</h3>
          <p>这类数据由系统统一维护，业务页面只选择，不再把固定选项写死在前端。</p>
        </div>
      </header>

      <div class="system-dictionary-tabs">
        <button
          v-for="category in (['logistics_provider', 'product_unit'] as DictionaryCategory[])"
          :key="category"
          type="button"
          :class="{ active: dictionaryCategory === category }"
          @click="selectCategory(category)"
        >
          {{ categoryMeta[category].title }}
        </button>
      </div>

      <div class="system-dictionary-intro">
        <div>
          <strong>{{ categoryMeta[dictionaryCategory].title }}</strong>
          <span>{{ categoryMeta[dictionaryCategory].description }}</span>
        </div>
        <input
          v-model="dictionarySearch"
          type="search"
          placeholder="搜索编码 / 名称 / 说明"
          @input="dictionaryPage = 1"
        />
      </div>

      <div v-if="canManageInventorySettings" class="system-dictionary-add">
        <input v-model="newDictionary.code" type="text" :placeholder="categoryMeta[dictionaryCategory].codeHint" />
        <input v-model="newDictionary.label" type="text" placeholder="显示名称" />
        <input v-model="newDictionary.description" type="text" placeholder="说明（可选）" />
        <input v-model.number="newDictionary.sort_order" type="number" min="0" step="10" placeholder="排序" />
        <button class="primary-button" type="button" :disabled="addingDictionary" @click="addDictionaryItem">
          {{ addingDictionary ? '添加中...' : '＋ 添加' }}
        </button>
      </div>

      <div class="system-dictionary-table-wrap">
        <table class="system-dictionary-table">
          <thead>
            <tr>
              <th>编码</th>
              <th>显示名称</th>
              <th>说明</th>
              <th>排序</th>
              <th>启用</th>
              <th>来源</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedDictionaryItems" :key="item.id">
              <td><input v-model="dictionaryDrafts[item.id].code" type="text" :disabled="!canManageInventorySettings" /></td>
              <td><input v-model="dictionaryDrafts[item.id].label" type="text" :disabled="!canManageInventorySettings" /></td>
              <td><input v-model="dictionaryDrafts[item.id].description" type="text" :disabled="!canManageInventorySettings" /></td>
              <td><input v-model.number="dictionaryDrafts[item.id].sort_order" type="number" min="0" :disabled="!canManageInventorySettings" /></td>
              <td class="system-dictionary-enabled">
                <input v-model="dictionaryDrafts[item.id].enabled" type="checkbox" :disabled="!canManageInventorySettings" />
              </td>
              <td>
                <span class="system-dictionary-source">{{ item.system_seeded ? '系统预置' : '自定义' }}</span>
              </td>
              <td>
                <button
                  v-if="canManageInventorySettings"
                  class="ghost-button"
                  type="button"
                  :disabled="savingDictionaryId === item.id"
                  @click="saveDictionaryItem(item)"
                >
                  {{ savingDictionaryId === item.id ? '保存中' : '保存' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!filteredDictionaryItems.length" class="empty-state">暂无符合条件的字典项。</div>
      </div>

      <PaginationBar
        :page="Math.min(dictionaryPage, dictionaryTotalPages)"
        :total-pages="dictionaryTotalPages"
        :total="filteredDictionaryItems.length"
        :page-size="dictionaryPageSize"
        @update:page="dictionaryPage = $event"
      />
    </section>
  </div>
</template>

<style scoped>
.strategy-center-body{display:grid;gap:18px;padding:18px 20px}.strategy-center-group{display:grid;gap:14px;padding:16px;border:1px solid #e4e8f1;border-radius:16px;background:#fafbfe}.strategy-center-group-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.strategy-center-group-head h4,.strategy-center-group-head p{margin:0}.strategy-center-group-head h4{font-size:18px;color:#2b354b}.strategy-center-group-head p{margin-top:5px;color:#7e899d;font-size:13px;line-height:1.55}.strategy-total{padding:7px 11px;border-radius:999px;background:#eaf7ef;color:#25864d;font-size:13px;white-space:nowrap}.strategy-total.invalid{background:#fff0ed;color:#c14b3e}.strategy-rule-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.strategy-rule-card{display:grid;gap:11px;padding:14px;border:1px solid #dfe5f0;border-radius:14px;background:#fff}.strategy-rule-card.disabled{opacity:.58}.strategy-rule-title{display:flex;align-items:center;justify-content:space-between;gap:10px}.strategy-rule-title code{color:#8791a4;font-size:11px}.strategy-enable{display:flex;align-items:center;gap:7px;font-weight:800;color:#445069}.strategy-enable input{width:18px;height:18px}.strategy-field{display:grid;gap:5px}.strategy-field>span,.strategy-probability>span,.addressing-option-card label>span{color:#6b768b;font-size:12px;font-weight:800}.strategy-field input,.strategy-field textarea,.strategy-probability input,.addressing-option-card>input,.addressing-option-card label input{box-sizing:border-box;width:100%;border:1px solid #dfe5ef;border-radius:9px;padding:8px 10px;background:#fff;font:inherit;color:#344057}.strategy-field textarea{resize:vertical}.strategy-probability{display:grid;gap:6px}.strategy-probability>div,.addressing-option-card label{display:flex;align-items:center;gap:7px}.strategy-probability input{width:90px}.strategy-probability small,.addressing-option-card small{color:#8e98a8}.addressing-mode-switch{display:flex;align-items:center;flex-wrap:wrap;gap:10px}.addressing-mode-switch label{display:flex;align-items:center;gap:6px}.addressing-option-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px}.addressing-option-card{display:grid;gap:9px;padding:12px;border:1px solid #dfe5ef;border-radius:12px;background:#fff}.addressing-option-card label input[type=number]{width:76px}.addressing-option-card .ghost-button{justify-self:end}@media(max-width:1100px){.strategy-rule-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.addressing-option-grid{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(max-width:720px){.strategy-rule-grid,.addressing-option-grid{grid-template-columns:1fr}.strategy-center-group-head{flex-direction:column}.addressing-mode-switch{align-items:flex-start}}
.agent-prompt-list{display:grid;gap:14px;margin-top:16px}.agent-prompt-item{padding:16px;border:1px solid rgba(96,112,190,.16);border-radius:16px;background:rgba(250,251,255,.86)}.agent-prompt-item-head{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.agent-prompt-item-head>div{display:grid;gap:4px}.agent-prompt-item-head strong{font-size:18px;color:#26324c}.agent-prompt-item-head code{font-size:12px;color:#77819a}.agent-prompt-item p{margin:8px 0;color:#68728a;line-height:1.6}.agent-prompt-item small{display:block;margin-bottom:10px;color:#9098aa}.agent-prompt-draft-badge{display:inline-flex!important;width:max-content;padding:3px 8px;border-radius:999px;background:#fff3d9;color:#986a20;font-weight:850}.agent-prompt-item textarea{box-sizing:border-box;width:100%;min-height:150px;padding:12px 14px;border:1px solid rgba(93,107,188,.18);border-radius:12px;background:#fff;color:#2d374d;font:inherit;line-height:1.6;resize:vertical}.agent-prompt-actions{display:flex;justify-content:flex-end;flex-wrap:wrap;gap:8px;margin-top:10px}.agent-prompt-history{display:grid;gap:8px;margin-top:12px;padding-top:12px;border-top:1px solid rgba(96,112,190,.12)}.agent-prompt-history>article{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:8px;padding:10px;border:1px solid rgba(96,112,190,.12);border-radius:10px;background:#fff}.agent-prompt-history>article>div{display:flex;align-items:center;gap:8px}.agent-prompt-history>article>div small{margin:0}.agent-prompt-history pre{grid-column:1/-1;max-height:120px;margin:0;padding:8px;border-radius:8px;background:#f7f8fc;color:#5a647a;overflow:auto;white-space:pre-wrap;font:12px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace}.compact-toggle{display:flex;align-items:center;gap:8px}
.finance-review-settings h3 {font-size:20px}
.finance-review-settings p,.finance-review-settings button,.finance-review-settings label {font-size:18px;line-height:1.5}
.finance-review-settings .finance-review-toggle {display:flex;align-items:center;gap:12px;cursor:pointer}
.finance-review-settings .finance-review-toggle input {width:20px;height:20px;min-height:20px;accent-color:#326bd8}
.finance-review-settings button:hover {box-shadow:0 0 0 3px #4285ff22;border-color:#65a1ff}
.system-settings-page { display: grid; gap: 16px; }
.settings-config-group { overflow: hidden; border: 1px solid #e2e7f0; border-radius: 18px; background: #fff; box-shadow: 0 10px 28px rgba(43, 56, 91, .045); }
.settings-config-group > header { padding: 15px 18px 10px; }
.settings-config-group > header h3, .settings-config-group > header p { margin: 0; }
.settings-config-group > header h3 { color: #2b354b; font-size: 16px; }
.settings-config-group > header p { margin-top: 4px; color: #8993a6; font-size: 11px; line-height: 1.6; }
.settings-config-entry-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; padding: 0 14px 14px; }
.settings-config-entry { position: relative; display: grid; grid-template-columns: 38px minmax(0, 1fr) auto 12px; align-items: center; gap: 10px; min-height: 82px; box-sizing: border-box; padding: 12px; border: 1px solid #e6eaf2; border-radius: 13px; color: inherit; background: #fafbfe; text-decoration: none; transition: border-color .16s ease, background .16s ease, transform .16s ease; }
.settings-config-entry:hover { transform: translateY(-1px); border-color: #aebaf0; background: #f5f7ff; }
.settings-config-entry > i { display: grid; place-items: center; width: 36px; height: 36px; border-radius: 11px; color: #fff; background: linear-gradient(145deg, #7486e9, #5669d3); font-style: normal; font-weight: 900; }
.settings-config-entry > div { min-width: 0; }
.settings-config-entry strong, .settings-config-entry p { margin: 0; }
.settings-config-entry strong { color: #344057; font-size: 13px; }
.settings-config-entry p { display: -webkit-box; overflow: hidden; margin-top: 4px; color: #8490a3; font-size: 10px; line-height: 1.55; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.settings-config-entry > span { padding: 4px 7px; border-radius: 999px; color: #6573a3; background: #edf0fa; font-size: 9px; white-space: nowrap; }
.settings-config-entry > b { color: #8996bd; font-size: 18px; }
.system-settings-card { overflow: hidden; border: 1px solid #e3e8f0; border-radius: 18px; background: #fff; box-shadow: 0 12px 34px rgba(43, 56, 91, .055); }
.system-settings-card > header { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 18px 20px; border-bottom: 1px solid #edf0f5; }
.system-settings-card > header h3, .system-settings-card > header p { margin: 0; }
.system-settings-card > header h3 { margin-top: 3px; color: #242e42; font-size: 20px; }
.system-settings-card > header p { margin-top: 5px; color: #8993a6; font-size: 12px; }
.system-settings-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px 16px; padding: 18px 20px; }
.system-settings-form label { display: grid; gap: 7px; }
.system-settings-form label > span { color: #566176; font-size: 12px; font-weight: 800; }
.system-settings-form input[type="text"], .system-settings-form input[type="url"], .system-settings-form textarea { min-height: 42px; box-sizing: border-box; border: 1px solid #dfe4ed; border-radius: 10px; padding: 9px 11px; color: #354155; background: #fbfcfe; outline: none; font: inherit; resize: vertical; }
.system-settings-form input:focus { border-color: #91a4f3; box-shadow: 0 0 0 3px rgba(80, 103, 221, .08); }
.system-setting-wide { grid-column: 1 / -1; }
.system-setting-subsection { display: grid; gap: 4px; margin-top: 4px; padding: 13px 15px; border: 1px solid #e2e7f3; border-radius: 12px; background: linear-gradient(135deg, #f8faff, #f4f7ff); }
.system-setting-subsection strong { color: #273653; font-size: 15px; font-weight: 900; }
.system-setting-subsection p { margin: 0; color: #71809b; font-size: 12px; line-height: 1.6; }
.system-setting-toggle { display: flex !important; grid-column: 1 / -1; align-items: center; justify-content: space-between; padding: 10px 12px; border: 1px solid #e3e7ef; border-radius: 10px; background: #f8f9fc; }
.system-setting-toggle input { width: 18px; height: 18px; }
.system-footer-preview { display: grid; gap: 8px; margin: 0 20px 20px; padding: 14px 16px; border: 1px dashed #dce2eb; border-radius: 12px; background: #fafbfe; }
.system-footer-preview > span { color: #8d97a8; font-size: 10px; font-weight: 800; }
.system-footer-preview > div { display: flex; justify-content: center; flex-wrap: wrap; gap: 7px; color: #929cac; font-size: 10px; }
.system-industry-add { display: grid; grid-template-columns: 1fr 1.5fr 120px auto; gap: 8px; padding: 16px 18px; }
.system-industry-add input { min-width: 0; min-height: 40px; box-sizing: border-box; border: 1px solid #dfe4ed; border-radius: 9px; padding: 8px 10px; background: #fff; outline: none; font: inherit; font-size: 12px; }
.system-industry-table { min-width: 720px; }
.system-industry-table strong { color: #667187; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.system-warehouse-add { display: grid; grid-template-columns: 1fr 1.5fr 150px auto; gap: 8px; padding: 0 18px 14px; }
.system-warehouse-add input, .system-warehouse-add select { min-width: 0; min-height: 40px; box-sizing: border-box; border: 1px solid #dfe4ed; border-radius: 9px; padding: 8px 10px; color: #354155; background: #fff; outline: none; font: inherit; font-size: 12px; }
.system-dictionary-tabs { display: flex; gap: 8px; padding: 14px 18px 0; }
.system-dictionary-tabs button { min-height: 38px; border: 1px solid #dfe4ed; border-radius: 10px; padding: 0 15px; color: #667187; background: #fff; font: inherit; font-size: 12px; font-weight: 800; cursor: pointer; }
.system-dictionary-tabs button.active { border-color: #5367cf; color: #fff; background: #5367cf; box-shadow: 0 0 18px rgba(76, 99, 216, .22); }
.system-dictionary-intro { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 14px 18px; }
.system-dictionary-intro > div { display: grid; gap: 3px; }
.system-dictionary-intro strong { color: #303b50; }
.system-dictionary-intro span { color: #8993a4; font-size: 11px; }
.system-dictionary-intro input { width: min(360px, 45%); min-height: 40px; box-sizing: border-box; border: 1px solid #dfe4ed; border-radius: 10px; padding: 8px 11px; background: #fbfcfe; outline: none; font: inherit; }
.system-dictionary-add { display: grid; grid-template-columns: 1fr 1fr 1.4fr 90px auto; gap: 8px; padding: 0 18px 14px; }
.system-dictionary-add input { min-width: 0; min-height: 40px; box-sizing: border-box; border: 1px solid #dfe4ed; border-radius: 9px; padding: 8px 10px; background: #fff; outline: none; font: inherit; font-size: 12px; }
.system-dictionary-table-wrap { overflow-x: auto; border-top: 1px solid #edf0f5; }
.system-dictionary-table { width: 100%; min-width: 930px; border-collapse: collapse; }
.system-dictionary-table th { padding: 11px 12px; color: #818c9e; background: #fafbfe; text-align: left; font-size: 11px; }
.system-dictionary-table td { padding: 9px 12px; border-top: 1px solid #edf0f5; }
.system-dictionary-table tbody tr:hover { background: #edf2fb; }
.system-dictionary-table input[type="text"], .system-dictionary-table input[type="number"] { width: 100%; min-height: 36px; box-sizing: border-box; border: 1px solid #e0e5ed; border-radius: 8px; padding: 6px 8px; background: #fff; outline: none; font: inherit; font-size: 11px; }
.system-dictionary-table select { width: 100%; min-height: 36px; box-sizing: border-box; border: 1px solid #e0e5ed; border-radius: 8px; padding: 6px 8px; color: #354155; background: #fff; outline: none; font: inherit; font-size: 11px; }
.system-dictionary-table input:disabled, .system-dictionary-table select:disabled { color: #788397; background: #f1f3f7; cursor: not-allowed; }
.system-warehouse-table { min-width: 760px; }
.system-dictionary-enabled { text-align: center; }
.system-dictionary-enabled input { width: 17px; height: 17px; }
.system-dictionary-source { display: inline-flex; padding: 4px 7px; border-radius: 999px; color: #667287; background: #f0f2f6; font-size: 10px; }
.membership-room-policy-summary { display: grid; grid-template-columns: repeat(2, minmax(160px, 220px)) 1fr; gap: 12px; align-items: center; padding: 16px 20px; border-bottom: 1px solid #edf0f5; background: #f8faff; }
.membership-room-policy-summary > div { display: grid; gap: 3px; padding: 11px 13px; border: 1px solid #dfe6f6; border-radius: 12px; background: #fff; }
.membership-room-policy-summary > div span { color: #8993a6; font-size: 11px; }
.membership-room-policy-summary > div strong { color: #425bd1; font-size: 20px; }
.membership-room-policy-summary > p { margin: 0; color: #6f7c94; font-size: 12px; line-height: 1.7; }
.membership-room-policy-table { min-width: 720px; }
.membership-room-policy-table td { color: #4f5b70; }
.membership-room-limit-input { display: inline-flex; align-items: center; gap: 8px; }
.membership-room-limit-input input { width: 110px !important; }
.membership-room-limit-input span { color: #7c879a; font-size: 12px; }
@media (max-width: 820px) {
  .settings-config-entry-grid { grid-template-columns: 1fr; }
  .membership-room-policy-summary { grid-template-columns: 1fr; }
  .system-settings-form { grid-template-columns: 1fr; }
  .system-setting-wide, .system-setting-toggle { grid-column: auto; }
  .system-dictionary-intro { align-items: stretch; flex-direction: column; }
  .system-dictionary-intro input { width: 100%; }
  .system-dictionary-add { grid-template-columns: 1fr; }
  .system-warehouse-add { grid-template-columns: 1fr; }
  .system-industry-add { grid-template-columns: 1fr; }
}
</style>
