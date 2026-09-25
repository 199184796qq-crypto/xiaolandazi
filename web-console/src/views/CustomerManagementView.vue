<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import CredentialResultModal from '../components/CredentialResultModal.vue'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import SalesScopeSelector from '../components/SalesScopeSelector.vue'
import { session } from '../session'
import { useFeedbackErrorRef } from '../uiFeedback'
import {
  adminResetCustomerPassword,
  bindTenantLivePolicyIndustry,
  getAdminAuditLogs,
  getAdminCustomers,
  getAdminSalesStaff,
  getLivePolicyIndustries,
  updateAdminCustomerCooperation,
} from '../api'
import type {
  AdminAuditLog,
  AdminCustomer,
  InitialCredential,
  LivePolicyIndustry,
  SalesStaffSummary,
  StaffBusinessScope,
} from '../types'

const route = useRoute()
const customerFocus = computed(() => String(route.query.focus || ''))

const customerPageTitle = computed(() => {
  if (customerFocus.value === 'attribution') return '客户资源'
  if (customerFocus.value === 'security') return '终端密码重置'
  if (customerFocus.value === 'audit') return '客户资源审计'
  return '终端账号与归属'
})

const customerPageDescription = computed(() => {
  if (customerFocus.value === 'attribution') return '按当前员工权限范围查看客户来源、销售归属、代理归属与行业。'
  if (customerFocus.value === 'security') return '只能查看并操作当前权限范围内的终端账号。'
  if (customerFocus.value === 'audit') return '普通员工只看自己的操作记录；部门负责人查看管理范围内员工的操作记录。'
  return '普通销售只看自己开发的客户；部门负责人及以上可按管理范围选择销售人员。'
})

const staffAccess = computed(() => session.bootstrap?.staff_access ?? null)
const canViewAudit = computed(
  () =>
    staffAccess.value?.is_super_admin === true ||
    staffAccess.value?.permissions.includes('*') === true ||
    staffAccess.value?.permissions.includes('audit.view') === true,
)
const canResetCustomer = computed(
  () =>
    staffAccess.value?.is_super_admin === true ||
    staffAccess.value?.permissions.includes('*') === true ||
    staffAccess.value?.permissions.includes('customer.password_reset') === true,
)
const canManageCooperation = computed(
  () =>
    session.bootstrap?.actor.role === 'platform_admin' ||
    staffAccess.value?.is_super_admin === true ||
    staffAccess.value?.permissions.includes('*') === true ||
    staffAccess.value?.permissions.includes('customer.cooperation.manage') === true,
)
const canManageL2 = computed(
  () =>
    session.bootstrap?.actor.role === 'platform_admin' ||
    staffAccess.value?.is_super_admin === true ||
    staffAccess.value?.permissions.includes('*') === true ||
    staffAccess.value?.permissions.includes('livepolicy.manage_l2') === true,
)

const customers = ref<AdminCustomer[]>([])
const customerScope = ref<StaffBusinessScope | null>(null)
const customerTotal = ref(0)
const customerTotalPages = ref(1)
const loading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')

const search = ref('')
const viewMode = ref<'card' | 'table'>('table')
const statusFilter = ref('all')
const sortMode = ref('created-desc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '正常', value: 'active' },
  { label: '其他状态', value: 'other' },
]
const sortOptions = [
  { label: '注册时间从新到旧', value: 'created-desc' },
  { label: '注册时间从旧到新', value: 'created-asc' },
  { label: '终端名称 A-Z', value: 'name-asc' },
  { label: '终端名称 Z-A', value: 'name-desc' },
]

const salesItems = ref<SalesStaffSummary[]>([])
const salesSearch = ref('')
const salesPage = ref(1)
const salesTotal = ref(0)
const salesTotalPages = ref(1)
const salesLoading = ref(false)
const selectedSalesStaffId = ref(0)
const selectedSalesName = ref('')

const industries = ref<LivePolicyIndustry[]>([])
const bindingTenantID = ref<number | null>(null)

const auditLogs = ref<AdminAuditLog[]>([])
const auditLoading = ref(false)
const auditSearch = ref('')
const auditAction = ref('all')
const auditResult = ref('all')
const auditPageSize = ref(20)
const auditPageIndex = ref(1)
const auditCursorStack = ref<number[]>([0])
const auditNextCursor = ref(0)
const auditHasMore = ref(false)

const resetTarget = ref<AdminCustomer | null>(null)
const resetDeliveryMethod = ref<'copy' | 'email'>('copy')
const resetting = ref(false)
const resetError = ref('')
const resetCredentialOpen = ref(false)
const resetCredential = ref<InitialCredential | null>(null)
const resetCredentialName = ref('')
const resetCredentialUsername = ref('')

const cooperationTarget = ref<AdminCustomer | null>(null)
const cooperationStatus = ref<'cooperating' | 'non_cooperating'>('cooperating')
const cooperationNote = ref('')
const cooperationSaving = ref(false)
const cooperationError = ref('')

const showCustomerRecords = computed(() => customerFocus.value !== 'audit')
const managerView = computed(() => customerScope.value?.manager_view === true)
const scopeCaption = computed(() => {
  if (!managerView.value) return '我的客户'
  if (selectedSalesStaffId.value > 0) return selectedSalesName.value + ' · 名下客户'
  return '当前管理范围 · 全部客户'
})

let customerSearchTimer: ReturnType<typeof setTimeout> | undefined
let salesSearchTimer: ReturnType<typeof setTimeout> | undefined
let auditSearchTimer: ReturnType<typeof setTimeout> | undefined

async function loadCustomers() {
  if (!showCustomerRecords.value) return
  loading.value = true
  error.value = ''
  try {
    const response = await getAdminCustomers({
      search: search.value.trim(),
      status: statusFilter.value,
      sort: sortMode.value,
      page: page.value,
      page_size: pageSize.value,
      sales_staff_id: selectedSalesStaffId.value || undefined,
    })
    customers.value = response.items
    customerScope.value = response.scope
    customerTotal.value = response.total
    customerTotalPages.value = response.total_pages
    if (page.value > response.total_pages) page.value = response.total_pages

    if (response.scope.manager_view && !salesItems.value.length && !salesLoading.value) {
      void loadSalesStaff()
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取终端列表失败'
  } finally {
    loading.value = false
  }
}

async function loadSalesStaff() {
  if (!managerView.value && customerScope.value !== null) return
  salesLoading.value = true
  try {
    const response = await getAdminSalesStaff({
      search: salesSearch.value.trim(),
      page: salesPage.value,
      page_size: 10,
    })
    salesItems.value = response.items
    salesTotal.value = response.total
    salesTotalPages.value = response.total_pages
    if (salesPage.value > response.total_pages) salesPage.value = response.total_pages
  } catch (value) {
    if (!error.value) {
      error.value = value instanceof Error ? value.message : '读取销售人员失败'
    }
  } finally {
    salesLoading.value = false
  }
}

async function loadIndustries() {
  if (!canManageL2.value) {
    industries.value = []
    return
  }
  try {
    const response = await getLivePolicyIndustries()
    industries.value = response.items
  } catch (value) {
    if (!error.value) {
      error.value = value instanceof Error ? value.message : '读取行业目录失败'
    }
  }
}

function currentAuditCursor() {
  return auditCursorStack.value[Math.max(0, auditPageIndex.value - 1)] || 0
}

async function loadAuditLogs() {
  if (!canViewAudit.value || customerFocus.value !== 'audit') {
    auditLogs.value = []
    return
  }
  auditLoading.value = true
  error.value = ''
  try {
    const response = await getAdminAuditLogs({
      search: auditSearch.value.trim(),
      action: auditAction.value,
      result: auditResult.value,
      page_size: auditPageSize.value,
      before_id: currentAuditCursor() || undefined,
    })
    auditLogs.value = response.items
    auditNextCursor.value = response.next_cursor
    auditHasMore.value = response.has_more
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取审计日志失败'
  } finally {
    auditLoading.value = false
  }
}

async function loadAll() {
  notice.value = ''
  if (customerFocus.value === 'audit') {
    await loadAuditLogs()
    return
  }
  await Promise.all([loadCustomers(), loadIndustries()])
}

function selectSalesStaff(staffId: number) {
  selectedSalesStaffId.value = staffId
  const item = salesItems.value.find((entry) => entry.staff_id === staffId)
  selectedSalesName.value = item?.display_name || ''
  page.value = 1
  void loadCustomers()
}

function setSalesPage(value: number) {
  salesPage.value = Math.min(Math.max(1, value), salesTotalPages.value)
  void loadSalesStaff()
}

function resetAuditPaging() {
  auditPageIndex.value = 1
  auditCursorStack.value = [0]
  auditNextCursor.value = 0
  auditHasMore.value = false
}

function nextAuditPage() {
  if (!auditHasMore.value || !auditNextCursor.value) return
  auditCursorStack.value = [
    ...auditCursorStack.value.slice(0, auditPageIndex.value),
    auditNextCursor.value,
  ]
  auditPageIndex.value += 1
  void loadAuditLogs()
}

function previousAuditPage() {
  if (auditPageIndex.value <= 1) return
  auditPageIndex.value -= 1
  void loadAuditLogs()
}

async function changeIndustry(customer: AdminCustomer, industryCode: string) {
  if (!canManageL2.value || bindingTenantID.value) return
  const previousCode = customer.industry_code
  const previousName = customer.industry_name
  bindingTenantID.value = customer.tenant_id
  error.value = ''
  notice.value = ''
  try {
    await bindTenantLivePolicyIndustry(customer.tenant_id, industryCode)
    const industry = industries.value.find((item) => item.code === industryCode)
    customer.industry_code = industryCode
    customer.industry_name = industry?.name || industryCode
    notice.value = '已将 ' + customer.display_name + ' 绑定到“' + customer.industry_name + '”行业。'
  } catch (value) {
    customer.industry_code = previousCode
    customer.industry_name = previousName
    error.value = value instanceof Error ? value.message : '绑定终端行业失败'
  } finally {
    bindingTenantID.value = null
  }
}

function changeIndustryFromEvent(customer: AdminCustomer, event: Event) {
  const target = event.target as HTMLSelectElement | null
  if (!target) return
  void changeIndustry(customer, target.value)
}

function openReset(customer: AdminCustomer) {
  if (!canResetCustomer.value) return
  resetTarget.value = customer
  resetDeliveryMethod.value = customer.email ? 'email' : 'copy'
  resetError.value = ''
  notice.value = ''
}

function closeReset() {
  if (resetting.value) return
  resetTarget.value = null
  resetError.value = ''
}

function closeResetCredential() {
  resetCredentialOpen.value = false
  resetCredential.value = null
  resetCredentialName.value = ''
  resetCredentialUsername.value = ''
}

async function submitReset() {
  if (!resetTarget.value || resetting.value) return
  resetError.value = ''
  const customer = resetTarget.value
  if (resetDeliveryMethod.value === 'email' && !customer.email) {
    resetError.value = '该终端没有邮箱，请选择复制方式交付'
    return
  }
  resetting.value = true
  try {
    const result = await adminResetCustomerPassword(customer.user_id, {
      delivery_method: resetDeliveryMethod.value,
      email: customer.email || '',
    })
    resetCredentialName.value = result.customer.display_name
    resetCredentialUsername.value = result.customer.username
    resetCredential.value = result.credential
    resetTarget.value = null
    resetCredentialOpen.value = true
    notice.value = '已为 ' + customer.display_name + ' 生成新的系统初始密码。'
  } catch (value) {
    resetError.value = value instanceof Error ? value.message : '重置终端密码失败'
  } finally {
    resetting.value = false
  }
}

function cooperationLabel(customer: AdminCustomer) {
  return customer.cooperation_status === 'non_cooperating' ? '不合作' : '合作中'
}

function rechargeLabel(customer: AdminCustomer) {
  if (customer.last_recharge_at) {
    return '最近充值：' + formatDate(customer.last_recharge_at)
  }
  return customer.recharge_dormant_90_days ? '90天+无成功充值' : '暂无成功充值'
}

function openCooperation(customer: AdminCustomer) {
  if (!canManageCooperation.value) return
  cooperationTarget.value = customer
  cooperationStatus.value =
    customer.cooperation_status === 'non_cooperating'
      ? 'cooperating'
      : 'non_cooperating'
  cooperationNote.value =
    cooperationStatus.value === 'non_cooperating'
      ? customer.cooperation_note || ''
      : ''
  cooperationError.value = ''
}

function closeCooperation() {
  if (cooperationSaving.value) return
  cooperationTarget.value = null
  cooperationError.value = ''
}

async function submitCooperation() {
  const customer = cooperationTarget.value
  if (!customer || cooperationSaving.value) return
  if (cooperationStatus.value === 'non_cooperating' && !cooperationNote.value.trim()) {
    cooperationError.value = '标记不合作时请填写原因'
    return
  }

  cooperationSaving.value = true
  cooperationError.value = ''
  try {
    const result = await updateAdminCustomerCooperation(customer.tenant_id, {
      status: cooperationStatus.value,
      note: cooperationNote.value.trim(),
    })
    customer.cooperation_status = result.cooperation_status
    customer.cooperation_note = result.cooperation_note
    customer.cooperation_marked_at = result.cooperation_marked_at
    customer.cooperation_marked_by_user_id = result.cooperation_marked_by_user_id
    customer.last_recharge_at = result.last_recharge_at
    customer.recharge_dormant_90_days = result.recharge_dormant_90_days
    notice.value =
      customer.display_name +
      (result.cooperation_status === 'non_cooperating'
        ? ' 已标记为不合作，直播运维可清理其直播间。'
        : ' 已恢复为合作中。')
    cooperationTarget.value = null
  } catch (value) {
    cooperationError.value =
      value instanceof Error ? value.message : '更新合作状态失败'
  } finally {
    cooperationSaving.value = false
  }
}

function sourceLabel(source: string) {
  if (source === 'agent') return '代理直接开户'
  if (source === 'agent_invite') return '代理邀请码'
  if (source === 'platform_invite') return '平台邀请码'
  if (source === 'referral') return '终端推荐'
  if (source === 'direct') return '历史直营'
  return source || '未知来源'
}

function parentLabel(customer: AdminCustomer) {
  if (customer.parent_org_type === 'agent') {
    return '代理 · ' + (customer.parent_org_name || '#' + customer.parent_org_id)
  }
  if (customer.parent_org_type === 'platform') return '平台直营'
  return customer.parent_org_name || '未识别'
}

function formatDate(value: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function auditActionLabel(action: string) {
  const labels: Record<string, string> = {
    'admin.login': '管理员登录',
    'admin.logout': '管理员退出',
    'admin.change_password': '管理员修改密码',
    'customer.list_view': '查看终端列表',
    'customer.password_reset': '重置终端密码',
    'customer.delete': '删除终端',
    'audit.list_view': '查看审计日志',
    'room.create': '创建直播间',
    'room.delete': '删除直播间',
    'agent.create': '创建代理',
    'agent.customer.create': '代理开通终端',
    'invitation.status_update': '修改邀请码状态',
    'agent.resource_adjust': '调整代理资源',
    'customer.resource_adjust': '调整客户资源',
    'agent.customer.resource_allocate': '代理分配客户资源',
  }
  return labels[action] ?? action
}

function auditResultLabel(result: string) {
  if (result === 'started') return '执行中'
  if (result.startsWith('http_2')) return '成功'
  if (result.startsWith('http_4')) return '已拒绝'
  if (result.startsWith('http_5')) return '失败'
  return result
}

watch(search, () => {
  if (!showCustomerRecords.value) return
  if (customerSearchTimer) clearTimeout(customerSearchTimer)
  customerSearchTimer = setTimeout(() => {
    page.value = 1
    void loadCustomers()
  }, 280)
})

watch([statusFilter, sortMode, pageSize], () => {
  if (!showCustomerRecords.value) return
  page.value = 1
  void loadCustomers()
})

watch(page, () => {
  if (showCustomerRecords.value) void loadCustomers()
})

watch(salesSearch, () => {
  if (salesSearchTimer) clearTimeout(salesSearchTimer)
  salesSearchTimer = setTimeout(() => {
    salesPage.value = 1
    void loadSalesStaff()
  }, 280)
})

watch([auditSearch, auditAction, auditResult, auditPageSize], () => {
  if (customerFocus.value !== 'audit') return
  if (auditSearchTimer) clearTimeout(auditSearchTimer)
  auditSearchTimer = setTimeout(() => {
    resetAuditPaging()
    void loadAuditLogs()
  }, 280)
})

watch(customerFocus, () => {
  search.value = ''
  page.value = 1
  selectedSalesStaffId.value = 0
  selectedSalesName.value = ''
  salesItems.value = []
  salesPage.value = 1
  resetAuditPaging()
  void loadAll()
})

onMounted(loadAll)
</script>

<template>
  <div class="customer-management-page">
    <ModulePageNav hub="customers" :active-title="customerPageTitle" />

    <section class="feature-workspace-hero customer-admin-hero">
      <div>
        <p class="section-kicker">CUSTOMER MANAGEMENT</p>
        <h2>{{ customerPageTitle }}</h2>
        <p>{{ customerPageDescription }}</p>
      </div>
      <button
        class="ghost-button"
        type="button"
        :disabled="loading || auditLoading"
        @click="loadAll"
      >
        {{ loading || auditLoading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <div v-if="notice" class="settings-success">{{ notice }}</div>

    <template v-if="showCustomerRecords">

      <section class="customer-scope-layout">
        <SalesScopeSelector
          v-if="managerView"
          :items="salesItems"
          :total="salesTotal"
          :page="salesPage"
          :total-pages="salesTotalPages"
          :search="salesSearch"
          :selected-staff-id="selectedSalesStaffId"
          :loading="salesLoading"
          @update:search="salesSearch = $event"
          @update:page="setSalesPage"
          @select="selectSalesStaff"
        />

        <section class="customer-admin-card customer-scoped-results">
          <div class="customer-admin-card-head customer-scope-result-head">
            <div>
              <span class="section-kicker">CUSTOMER ATTRIBUTION</span>
              <h3>{{ scopeCaption }}</h3>
            </div>
            <span>{{ customerTotal }} 条</span>
          </div>

          <DataListControls
            v-model:view-mode="viewMode"
            v-model:search="search"
            v-model:status="statusFilter"
            v-model:sort="sortMode"
            v-model:page-size="pageSize"
            search-placeholder="终端 / 电话 / 代理 / 推荐人 / 来源 / 行业"
            :status-options="statusOptions"
            :sort-options="sortOptions"
          />

          <div v-if="loading" class="customer-admin-loading">正在读取终端...</div>

          <div v-else-if="viewMode === 'table'" class="customer-table-wrap">
            <table class="customer-table">
              <thead>
                <tr>
                  <th>终端</th>
                  <th>联系电话</th>
                  <th>来源</th>
                  <th>归属</th>
                  <th>行业 / 行业层</th>
                  <th>合作状态</th>
                  <th>邀请 / 推荐人</th>
                  <th>状态</th>
                  <th>注册时间</th>
                  <th v-if="customerFocus === 'security'">操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="customer in customers" :key="customer.user_id">
                  <td>
                    <div class="customer-identity">
                      <span class="customer-avatar">{{ customer.display_name.slice(0, 1) }}</span>
                      <div>
                        <strong>{{ customer.display_name }}</strong>
                        <span>@{{ customer.username }} · #{{ customer.tenant_id }}</span>
                      </div>
                    </div>
                  </td>
                  <td>{{ customer.phone || '未完善' }}</td>
                  <td><span class="source-badge">{{ sourceLabel(customer.source_type) }}</span></td>
                  <td>
                    <strong>{{ parentLabel(customer) }}</strong>
                    <span class="table-subtext">
                      销售：{{ customer.sales_display_name || '未分配' }}
                    </span>
                  </td>
                  <td>
                    <select
                      v-if="canManageL2"
                      class="customer-industry-select"
                      :value="customer.industry_code || 'general'"
                      :disabled="bindingTenantID === customer.tenant_id"
                      @change="changeIndustryFromEvent(customer, $event)"
                    >
                      <option
                        v-for="industry in industries"
                        :key="industry.code"
                        :value="industry.code"
                      >
                        {{ industry.name }}
                      </option>
                    </select>
                    <span v-else class="customer-industry-badge">
                      {{ customer.industry_name || '通用' }}
                    </span>
                  </td>
                  <td>
                    <span
                      class="customer-status"
                      :class="{
                        active: customer.cooperation_status !== 'non_cooperating',
                        'non-cooperating': customer.cooperation_status === 'non_cooperating',
                      }"
                    >
                      {{ cooperationLabel(customer) }}
                    </span>
                    <span
                      v-if="customer.recharge_dormant_90_days"
                      class="cooperation-recharge-warning"
                    >
                      90天+未充值
                    </span>
                    <span class="table-subtext">{{ rechargeLabel(customer) }}</span>
                    <button
                      v-if="canManageCooperation"
                      class="table-action-button cooperation-action-button"
                      type="button"
                      @click="openCooperation(customer)"
                    >
                      {{ customer.cooperation_status === 'non_cooperating' ? '恢复合作' : '标记不合作' }}
                    </button>
                  </td>
                  <td>
                    <template v-if="customer.inviter_display_name">
                      <strong>{{ customer.inviter_display_name }}</strong>
                      <span class="table-subtext">@{{ customer.inviter_username }}</span>
                    </template>
                    <span v-else>—</span>
                  </td>
                  <td>
                    <span class="customer-status" :class="{ active: customer.status === 'active' }">
                      {{ customer.status === 'active' ? '正常' : customer.status }}
                    </span>
                  </td>
                  <td>{{ formatDate(customer.created_at) }}</td>
                  <td v-if="customerFocus === 'security'">
                    <button
                      v-if="canResetCustomer"
                      class="table-action-button"
                      type="button"
                      @click="openReset(customer)"
                    >
                      重置密码
                    </button>
                    <span v-else>—</span>
                  </td>
                </tr>
              </tbody>
            </table>
            <div v-if="!customers.length" class="empty-state">当前范围内暂无符合条件的客户。</div>
          </div>

          <div v-else class="customer-card-grid">
            <article
              v-for="customer in customers"
              :key="'mobile-' + customer.user_id"
              class="customer-mobile-card"
            >
              <div class="customer-mobile-head">
                <div class="customer-identity">
                  <span class="customer-avatar">{{ customer.display_name.slice(0, 1) }}</span>
                  <div>
                    <strong>{{ customer.display_name }}</strong>
                    <span>{{ customer.username }}</span>
                  </div>
                </div>
                <span class="customer-status" :class="{ active: customer.status === 'active' }">
                  {{ customer.status === 'active' ? '正常' : customer.status }}
                </span>
              </div>
              <dl class="customer-mobile-meta customer-source-meta">
                <div><dt>联系电话</dt><dd>{{ customer.phone || '未完善' }}</dd></div>
                <div><dt>来源</dt><dd>{{ sourceLabel(customer.source_type) }}</dd></div>
                <div><dt>归属</dt><dd>{{ parentLabel(customer) }}</dd></div>
                <div><dt>销售</dt><dd>{{ customer.sales_display_name || '未分配' }}</dd></div>
                <div><dt>行业 / 行业层</dt><dd>{{ customer.industry_name || '通用' }}</dd></div>
                <div>
                  <dt>合作状态</dt>
                  <dd>
                    <span
                      class="customer-status"
                      :class="{
                        active: customer.cooperation_status !== 'non_cooperating',
                        'non-cooperating': customer.cooperation_status === 'non_cooperating',
                      }"
                    >
                      {{ cooperationLabel(customer) }}
                    </span>
                    <span v-if="customer.recharge_dormant_90_days" class="cooperation-recharge-warning">
                      90天+未充值
                    </span>
                  </dd>
                </div>
                <div><dt>充值参考</dt><dd>{{ rechargeLabel(customer) }}</dd></div>
                <div><dt>邀请 / 推荐人</dt><dd>{{ customer.inviter_display_name || '—' }}</dd></div>
              </dl>
              <button
                v-if="canManageCooperation"
                class="ghost-button customer-mobile-reset"
                type="button"
                @click="openCooperation(customer)"
              >
                {{ customer.cooperation_status === 'non_cooperating' ? '恢复合作' : '标记不合作' }}
              </button>
              <button
                v-if="customerFocus === 'security' && canResetCustomer"
                class="ghost-button customer-mobile-reset"
                type="button"
                @click="openReset(customer)"
              >
                重置密码
              </button>
            </article>
          </div>

          <PaginationBar
            v-model:page="page"
            :total-pages="customerTotalPages"
            :total="customerTotal"
            :page-size="pageSize"
          />
        </section>
      </section>
    </template>

    <section
      v-else-if="customerFocus === 'audit' && canViewAudit"
      class="customer-admin-card audit-card customer-audit-table-card"
    >
      <div class="customer-admin-card-head">
        <div>
          <span class="section-kicker">AUDIT LOG</span>
          <h3>客户资源操作审计</h3>
        </div>
        <span>第 {{ auditPageIndex }} 页</span>
      </div>

      <div class="customer-audit-filters">
        <label>
          <span>查询</span>
          <input v-model="auditSearch" type="search" placeholder="经办人 / 操作 / 目标 / 路径 / IP" />
        </label>
        <label>
          <span>操作</span>
          <select v-model="auditAction">
            <option value="all">全部操作</option>
            <option value="customer.list_view">查看终端列表</option>
            <option value="customer.password_reset">重置终端密码</option>
            <option value="audit.list_view">查看审计日志</option>
          </select>
        </label>
        <label>
          <span>结果</span>
          <select v-model="auditResult">
            <option value="all">全部结果</option>
            <option value="success">成功</option>
            <option value="denied">已拒绝</option>
            <option value="failed">失败</option>
          </select>
        </label>
        <label>
          <span>每页</span>
          <select v-model.number="auditPageSize">
            <option :value="20">20</option>
            <option :value="50">50</option>
            <option :value="100">100</option>
          </select>
        </label>
      </div>

      <div v-if="auditLoading" class="customer-admin-loading">正在读取审计日志...</div>
      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>经办人</th>
              <th>操作</th>
              <th>目标</th>
              <th>来源 IP</th>
              <th>结果</th>
              <th>接口</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in auditLogs" :key="item.id">
              <td>{{ formatDate(item.occurred_at) }}</td>
              <td><strong>{{ item.actor_username }}</strong><small>#{{ item.actor_user_id }}</small></td>
              <td>{{ auditActionLabel(item.action) }}</td>
              <td>{{ item.target_username || '—' }}</td>
              <td>{{ item.client_ip || '—' }}</td>
              <td><span class="status-pill">{{ auditResultLabel(item.result) }}</span></td>
              <td><small>{{ item.http_method || '' }} {{ item.path || '—' }}</small></td>
            </tr>
          </tbody>
        </table>
        <div v-if="!auditLogs.length" class="empty-state">暂无符合条件的审计记录。</div>
      </div>

      <div class="audit-cursor-pagination">
        <button
          type="button"
          :disabled="auditLoading || auditPageIndex <= 1"
          @click="previousAuditPage"
        >
          上一页
        </button>
        <span>第 {{ auditPageIndex }} 页 · 游标分页</span>
        <button
          type="button"
          :disabled="auditLoading || !auditHasMore"
          @click="nextAuditPage"
        >
          下一页
        </button>
      </div>
    </section>

    <section v-else-if="customerFocus === 'audit'" class="customer-admin-card">
      <div class="customer-admin-empty">
        <strong>无审计查看权限</strong>
        <span>当前角色不能查看客户资源审计记录。</span>
      </div>
    </section>

    <div v-if="resetTarget" class="modal-backdrop" @click.self="closeReset">
      <form class="modal-card" @submit.prevent="submitReset">
        <div class="modal-header">
          <div>
            <p class="section-kicker">RESET PASSWORD</p>
            <h3>重置终端密码</h3>
          </div>
          <button class="icon-button" type="button" @click="closeReset">×</button>
        </div>
        <p class="modal-helper">
          系统将为 {{ resetTarget.display_name }}（{{ resetTarget.username }}）随机生成新的初始密码。
        </p>
        <div class="form-grid">
          <label class="form-span-2">
            <span>初始凭证交付方式</span>
            <select v-model="resetDeliveryMethod" class="text-input">
              <option value="copy">生成后复制登录信息</option>
              <option value="email" :disabled="!resetTarget.email">发送到终端邮箱</option>
            </select>
          </label>
          <div class="form-span-2 account-opening-note">
            <strong>终端邮箱</strong>
            <span>{{ resetTarget.email || '未填写邮箱，只能使用复制方式交付' }}</span>
          </div>
        </div>
        <p v-if="resetError" class="auth-error">{{ resetError }}</p>
        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="closeReset">取消</button>
          <button class="primary-button" type="submit" :disabled="resetting">
            {{ resetting ? '生成中...' : '生成新初始密码' }}
          </button>
        </div>
      </form>
    </div>

    <div v-if="cooperationTarget" class="modal-backdrop" @click.self="closeCooperation">
      <form class="modal-card" @submit.prevent="submitCooperation">
        <div class="modal-header">
          <div>
            <p class="section-kicker">COOPERATION STATUS</p>
            <h3>客户合作状态</h3>
          </div>
          <button class="icon-button" type="button" @click="closeCooperation">×</button>
        </div>
        <p class="modal-helper">
          {{ cooperationTarget.display_name }} · {{ rechargeLabel(cooperationTarget) }}
        </p>
        <div class="form-grid">
          <label class="form-span-2">
            <span>状态</span>
            <select v-model="cooperationStatus" class="text-input">
              <option value="cooperating">合作中</option>
              <option value="non_cooperating">不合作</option>
            </select>
          </label>
          <label class="form-span-2">
            <span>原因 / 备注</span>
            <textarea
              v-model="cooperationNote"
              class="text-input"
              rows="4"
              :placeholder="cooperationStatus === 'non_cooperating' ? '必填，例如：合同终止、客户主动停止合作等' : '可填写恢复合作说明'"
            />
          </label>
          <div
            v-if="cooperationStatus === 'non_cooperating'"
            class="form-span-2 account-opening-note"
          >
            <strong>影响</strong>
            <span>标记后，该商户名下直播间会显示红色“不合作”角标，直播运维人员可以删除这些直播间。</span>
          </div>
        </div>
        <p v-if="cooperationError" class="auth-error">{{ cooperationError }}</p>
        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="closeCooperation">取消</button>
          <button class="primary-button" type="submit" :disabled="cooperationSaving">
            {{ cooperationSaving ? '保存中...' : '确认保存' }}
          </button>
        </div>
      </form>
    </div>

    <CredentialResultModal
      :open="resetCredentialOpen"
      title="终端初始密码已重置"
      :display-name="resetCredentialName"
      :username="resetCredentialUsername"
      :credential="resetCredential"
      @close="closeResetCredential"
    />
  </div>
</template>
