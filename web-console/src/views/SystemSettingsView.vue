<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  createSystemDictionaryItem,
  createSystemWarehouse,
  getLivePolicyIndustries,
  getSystemSettingsDashboard,
  updateSystemDictionaryItem,
  updateSystemSettings,
  updateMembershipRoomLimits,
  updateSystemWarehouse,
  upsertLivePolicyIndustry,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import PaginationBar from '../components/PaginationBar.vue'
import { financeReviewSettingKey } from '../financeReviewPolicy'
import type {
  InventoryWarehouse,
  LivePolicyIndustry,
  SystemDictionaryItem,
  SystemDictionaryItemInput,
  SystemSettingsDashboard,
  SystemWarehouseInput,
} from '../types'

type DictionaryCategory = 'logistics_provider' | 'product_unit'

const loading = ref(false)
const saving = ref(false)
const savingRuleTypography = ref(false)
const savingFinancePolicy = ref(false)
const savingMembershipLimits = ref(false)
const error = ref('')
const notice = ref('')
const dashboard = ref<SystemSettingsDashboard | null>(null)
const settingsDraft = reactive<Record<string, string>>({})
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

function hasPermission(code: string) {
  if (isPlatformAdmin.value) return true
  const access = session.bootstrap?.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

const canViewGlobal = computed(() => isPlatformAdmin.value)
const canViewMembershipLimits = computed(() => hasPermission('commercial.membership.view'))
const canManageMembershipLimits = computed(() => hasPermission('system.settings.liveops.manage'))
const canViewIndustry = computed(() => hasPermission('livepolicy.view'))
const canManageIndustry = computed(() => hasPermission('livepolicy.manage_l2'))
const canViewInventorySettings = computed(() => hasPermission('inventory.view'))
const canManageInventorySettings = computed(() => hasPermission('system.settings.inventory.manage'))

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

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [settings, industryData] = await Promise.all([
      getSystemSettingsDashboard(),
      canViewIndustry.value ? getLivePolicyIndustries() : Promise.resolve({ items: [] as LivePolicyIndustry[] }),
    ])
    syncDashboard(settings)
    syncIndustries(industryData.items)
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取系统设定失败'
  } finally {
    loading.value = false
  }
}

async function saveGlobalSettings() {
  if (!dashboard.value || saving.value) return
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
  const holdMinutes = Number(settingsDraft.device_order_hold_minutes || 15)
  if (!Number.isInteger(holdMinutes) || holdMinutes < 1 || holdMinutes > 120) {
    error.value = '设备订单未支付锁库时间必须是 1 到 120 分钟的整数'
    return
  }
  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const result = await updateSystemSettings(
      dashboard.value.settings.filter((item) => item.key !== financeReviewSettingKey).map((item) => ({
        key: item.key,
        value: settingsDraft[item.key] ?? '',
      })),
    )
    syncDashboard(result)
    notice.value = '全局系统文字已保存并立即生效。'
    window.dispatchEvent(new CustomEvent('system-config-updated'))
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存系统设置失败'
  } finally {
    saving.value = false
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
    <ModulePageNav :context="isPlatformAdmin ? 'workspace-admin' : 'workspace-staff'" active-title="系统设定" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">SYSTEM SETTINGS</p>
        <h2>系统设定</h2>
        <p>这里是统一配置入口；每个部门只看到自己有权限查看或维护的配置。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '读取中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <section v-if="canViewGlobal" id="finance-review-policy" class="system-settings-card finance-review-settings">
      <header><div><h3>财务审核规则</h3><p>适用于收款审核及充值、退款、奖励、AI 时长审批。仅控制是否必须分人，不取消权限和实际入账校验。</p></div><button type="button" class="primary-button" :disabled="loading || savingFinancePolicy || !dashboard" @click="saveFinancePolicy">{{ savingFinancePolicy ? '保存中…' : '保存审核规则' }}</button></header>
      <div class="system-settings-form">
        <label class="system-setting-wide finance-review-toggle"><input type="checkbox" :checked="settingsDraft[financeReviewSettingKey] !== 'false'" :disabled="loading || savingFinancePolicy" @change="settingsDraft[financeReviewSettingKey] = ($event.target as HTMLInputElement).checked ? 'true' : 'false'"/><span>强制经办人与审核人不同</span></label>
        <p class="system-setting-wide">{{ settingsDraft[financeReviewSettingKey] === 'false' ? '不强制：适合一人兼岗。本人提交或补件的单据也可由本人审核，前提是具备对应审核权限。' : '强制：本人提交或补件的单据，必须由另一位有审核权限的人处理。' }}经办人、审核人、时间、审核结果均保留。点击“保存审核规则”后生效。</p>
      </div>
    </section>

    <section v-if="canViewGlobal" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">GLOBAL TEXT</span>
          <h3>全局文字</h3>
          <p>配置整个系统统一显示的品牌名称、前后台智能体名称和页面底部文字。</p>
        </div>
        <button class="primary-button" type="button" :disabled="saving || loading" @click="saveGlobalSettings">
          {{ saving ? '保存中...' : '保存全局配置' }}
        </button>
      </header>

      <div v-if="loading && !dashboard" class="panel-loading">正在读取系统设定...</div>

      <div v-else class="system-settings-form">
        <label class="system-setting-wide">
          <span>系统显示名称</span>
          <input v-model="settingsDraft.site_name" type="text" placeholder="例如 小蓝搭子" />
        </label>

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

    <section v-if="canViewGlobal" class="system-settings-card">
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

    <section v-if="canViewGlobal" class="system-settings-card">
      <header>
        <div>
          <span class="section-kicker">COMMERCE STOCK RULE</span>
          <h3>交易库存规则</h3>
          <p>设备订单生成后立即锁定真实设备，未支付超时后自动取消订单并恢复销售库存。</p>
        </div>
        <button class="primary-button" type="button" :disabled="saving || loading" @click="saveGlobalSettings">
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

    <section v-if="canViewIndustry" class="system-settings-card">
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

    <section v-if="canViewMembershipLimits" class="system-settings-card">
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

    <section v-if="canViewInventorySettings" class="system-settings-card">
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

    <section v-if="canViewInventorySettings" class="system-settings-card">
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
.finance-review-settings h3 {font-size:20px}
.finance-review-settings p,.finance-review-settings button,.finance-review-settings label {font-size:18px;line-height:1.5}
.finance-review-settings .finance-review-toggle {display:flex;align-items:center;gap:12px;cursor:pointer}
.finance-review-settings .finance-review-toggle input {width:20px;height:20px;min-height:20px;accent-color:#326bd8}
.finance-review-settings button:hover {box-shadow:0 0 0 3px #4285ff22;border-color:#65a1ff}
.system-settings-page { display: grid; gap: 16px; }
.system-settings-card { overflow: hidden; border: 1px solid #e3e8f0; border-radius: 18px; background: #fff; box-shadow: 0 12px 34px rgba(43, 56, 91, .055); }
.system-settings-card > header { display: flex; align-items: center; justify-content: space-between; gap: 18px; padding: 18px 20px; border-bottom: 1px solid #edf0f5; }
.system-settings-card > header h3, .system-settings-card > header p { margin: 0; }
.system-settings-card > header h3 { margin-top: 3px; color: #242e42; font-size: 20px; }
.system-settings-card > header p { margin-top: 5px; color: #8993a6; font-size: 12px; }
.system-settings-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px 16px; padding: 18px 20px; }
.system-settings-form label { display: grid; gap: 7px; }
.system-settings-form label > span { color: #566176; font-size: 12px; font-weight: 800; }
.system-settings-form input[type="text"], .system-settings-form input[type="url"] { min-height: 42px; box-sizing: border-box; border: 1px solid #dfe4ed; border-radius: 10px; padding: 9px 11px; color: #354155; background: #fbfcfe; outline: none; font: inherit; }
.system-settings-form input:focus { border-color: #91a4f3; box-shadow: 0 0 0 3px rgba(80, 103, 221, .08); }
.system-setting-wide { grid-column: 1 / -1; }
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
