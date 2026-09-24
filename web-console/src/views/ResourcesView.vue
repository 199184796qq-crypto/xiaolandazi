<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  adjustAdminAgentResource,
  adjustAdminCustomerResource,
  allocateAgentCustomerResource,
  createCommercialAITimeGrantRequest,
  getAdminAgentResources,
  getAdminAgents,
  getAdminCustomerResources,
  getAdminCustomers,
  getAgentCustomerResources,
  getAgentCustomers,
  getCurrentResources,
} from '../api'
import { session } from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type {
  AdminCustomer,
  AgentSummary,
  ResourceAccount,
  ResourceDashboard,
} from '../types'

const route = useRoute()
const isFinanceTimePage = computed(() =>
  route.path.startsWith('/staff/finance/ai-time'),
)
const isMarketingTimePage = computed(() =>
  route.path.startsWith('/operations/live/marketing/ai-time'),
)
const resourcePageTitle = computed(() => {
  if (isFinanceTimePage.value) return 'AI 时长'
  const focus = String(route.query.focus || '')
  if (focus === 'ledger') return 'AI 时长流水'
  if (focus === 'adjustment') return 'AI 时长调整'
  return 'AI 时长'
})

const actor = computed(() => session.bootstrap?.actor)
const staffAccess = computed(() => session.bootstrap?.staff_access ?? null)

function hasStaffPermission(code: string) {
  return Boolean(
    staffAccess.value &&
      (staffAccess.value.is_super_admin ||
        staffAccess.value.permissions.includes(code)),
  )
}

const isAdmin = computed(
  () => actor.value?.role === 'platform_admin' || actor.value?.role === 'staff',
)
const isAgent = computed(() => actor.value?.role === 'agent_admin')
const isCustomer = computed(() => actor.value?.role === 'customer')
const canRequestAITime = computed(() => hasStaffPermission('commercial.ai_time.request'))
const canAdjustSystemResource = computed(() =>
  isMarketingTimePage.value
    ? canRequestAITime.value
    : hasStaffPermission('finance.resource.adjust'),
)

const loading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')

const agents = ref<AgentSummary[]>([])
const customers = ref<AdminCustomer[]>([])

const adminScope = ref<'agent' | 'customer'>('agent')
const selectedAgentID = ref<number | null>(null)
const selectedCustomerID = ref<number | null>(null)
const showAgentPicker = ref(false)
const agentSearch = ref('')
const agentPage = ref(1)
const agentPageSize = 20
const showCustomerPicker = ref(false)
const customerSearch = ref('')
const customerPage = ref(1)
const customerPageSize = 20
const resourceLedgerSearch = ref('')
const resourceLedgerPage = ref(1)
const resourceLedgerPageSize = 20

const ownResources = ref<ResourceDashboard | null>(null)
const targetResources = ref<ResourceDashboard | null>(null)

const showAdjust = ref(false)
const adjustResourceType = ref('ai_seconds')
const adjustDelta = ref<number | null>(null)
const adjustReason = ref('')
const adjusting = ref(false)

const showAllocate = ref(false)
const allocateQuantity = ref<number | null>(null)
const allocateReason = ref('')
const allocating = ref(false)

const resourceOrder = ['ai_seconds']
const customerResourceTypes = ['ai_seconds']

const currentDashboard = computed(() =>
  isAdmin.value ? targetResources.value : ownResources.value,
)

const currentLedger = computed(() => {
  const dashboard = currentDashboard.value
  if (!dashboard) return []
  const allowed = new Set(['ai_seconds'])
  return dashboard.ledger.filter((item) => allowed.has(item.resource_type))
})
const filteredCurrentLedger = computed(() => {
  const keyword = resourceLedgerSearch.value.trim().toLowerCase()
  if (!keyword) return currentLedger.value
  return currentLedger.value.filter((item) =>
    [businessLabel(item.business_type), item.reason, item.business_type, String(item.id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const resourceLedgerPageCount = computed(() =>
  Math.max(1, Math.ceil(filteredCurrentLedger.value.length / resourceLedgerPageSize)),
)
const pagedCurrentLedger = computed(() => {
  const page = Math.min(resourceLedgerPage.value, resourceLedgerPageCount.value)
  const start = (page - 1) * resourceLedgerPageSize
  return filteredCurrentLedger.value.slice(start, start + resourceLedgerPageSize)
})

const selectedAgent = computed(
  () =>
    agents.value.find(
      (item) => item.organization_id === selectedAgentID.value,
    ) || null,
)

const filteredAgents = computed(() => {
  const keyword = agentSearch.value.trim().toLowerCase()
  if (!keyword) return agents.value
  return agents.value.filter((item) =>
    [item.name, item.code, item.username, item.display_name, item.phone, item.email]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(keyword)),
  )
})

const agentPageCount = computed(() => Math.max(1, Math.ceil(filteredAgents.value.length / agentPageSize)))
const pagedAgents = computed(() => {
  const page = Math.min(agentPage.value, agentPageCount.value)
  const start = (page - 1) * agentPageSize
  return filteredAgents.value.slice(start, start + agentPageSize)
})

const selectedCustomer = computed(
  () =>
    customers.value.find(
      (item) => item.tenant_id === selectedCustomerID.value,
    ) || null,
)
const filteredCustomers = computed(() => {
  const keyword = customerSearch.value.trim().toLowerCase()
  if (!keyword) return customers.value
  return customers.value.filter((item) =>
    [item.display_name, item.username, item.phone, item.parent_org_name, String(item.tenant_id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const customerPageCount = computed(() => Math.max(1, Math.ceil(filteredCustomers.value.length / customerPageSize)))
const pagedCustomers = computed(() => {
  const page = Math.min(customerPage.value, customerPageCount.value)
  const start = (page - 1) * customerPageSize
  return filteredCustomers.value.slice(start, start + customerPageSize)
})

function resourceLabel(type: string) {
  const labels: Record<string, string> = {
    ai_seconds: 'AI 时长',
  }
  return labels[type] || type
}

function unitLabel(unit: string) {
  const labels: Record<string, string> = {
    seconds: '小时',
  }
  return labels[unit] || unit
}

function formatQuantity(type: string, value: number) {
  if (type === 'ai_seconds') {
    return (value / 3600).toFixed(2)
  }
  return new Intl.NumberFormat('zh-CN').format(value)
}

function businessLabel(type: string) {
  const labels: Record<string, string> = {
    platform_adjust: '平台调整',
    marketing_ai_time_grant: '营销运维申请 · 财务已审核',
    agent_allocate: '代理分配',
    customer_slot_consume: '开户占用终端名额',
  }
  return labels[type] || type
}

function formatDate(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function orderedAccounts(
  accounts: ResourceAccount[],
  orgType: 'agent' | 'customer' = 'agent',
) {
  const allowed =
    orgType === 'customer' ? customerResourceTypes : resourceOrder
  return accounts
    .filter((item) => allowed.includes(item.resource_type))
    .sort(
      (a, b) =>
        resourceOrder.indexOf(a.resource_type) -
        resourceOrder.indexOf(b.resource_type),
    )
}

async function loadAdminTarget() {
  if (adminScope.value === 'agent') {
    if (!selectedAgentID.value) {
      targetResources.value = null
      return
    }
    targetResources.value = await getAdminAgentResources(
      selectedAgentID.value,
    )
    return
  }

  if (!selectedCustomerID.value) {
    targetResources.value = null
    return
  }
  targetResources.value = await getAdminCustomerResources(
    selectedCustomerID.value,
  )
}

async function loadAdminData() {
  const [agentList, customerList] = await Promise.all([
    getAdminAgents(),
    getAdminCustomers(),
  ])

  agents.value = agentList.items
  customers.value = customerList.items

  if (!selectedAgentID.value && agents.value.length) {
    selectedAgentID.value = agents.value[0].organization_id
  }
  if (!selectedCustomerID.value && customers.value.length) {
    selectedCustomerID.value = customers.value[0].tenant_id
  }

  await loadAdminTarget()
}

async function loadAgentData() {
  const [own, customerList] = await Promise.all([
    getCurrentResources(),
    getAgentCustomers(),
  ])

  ownResources.value = own
  customers.value = customerList.items

  if (!selectedCustomerID.value && customers.value.length) {
    selectedCustomerID.value = customers.value[0].tenant_id
  }

  if (selectedCustomerID.value) {
    targetResources.value = await getAgentCustomerResources(
      selectedCustomerID.value,
    )
  } else {
    targetResources.value = null
  }
}

async function load() {
  loading.value = true
  error.value = ''
  notice.value = ''

  try {
    if (isAdmin.value) {
      await loadAdminData()
    } else if (isAgent.value) {
      await loadAgentData()
    } else if (isCustomer.value) {
      ownResources.value = await getCurrentResources()
    }
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取资源数据失败'
  } finally {
    loading.value = false
  }
}

async function changeAdminScope(scope: 'agent' | 'customer') {
  adminScope.value = scope
  if (scope === 'customer') {
    adjustResourceType.value = 'ai_seconds'
  }
  loading.value = true
  error.value = ''
  notice.value = ''

  try {
    await loadAdminTarget()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取资源账户失败'
  } finally {
    loading.value = false
  }
}

function openAgentPicker() {
  agentSearch.value = ''
  agentPage.value = 1
  showAgentPicker.value = true
}

async function chooseAgent(item: AgentSummary) {
  const changed = selectedAgentID.value !== item.organization_id
  selectedAgentID.value = item.organization_id
  showAgentPicker.value = false
  agentSearch.value = ''
  if (changed || !targetResources.value) {
    await selectAgent()
  }
}

function openCustomerPicker() {
  customerSearch.value = ''
  customerPage.value = 1
  showCustomerPicker.value = true
}

async function chooseCustomer(item: AdminCustomer) {
  const changed = selectedCustomerID.value !== item.tenant_id
  selectedCustomerID.value = item.tenant_id
  showCustomerPicker.value = false
  customerSearch.value = ''
  if (changed || !targetResources.value) {
    await selectCustomer()
  }
}

async function selectAgent() {
  if (!selectedAgentID.value) return
  loading.value = true
  error.value = ''

  try {
    await loadAdminTarget()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取代理资源失败'
  } finally {
    loading.value = false
  }
}

async function selectCustomer() {
  if (!selectedCustomerID.value) return
  loading.value = true
  error.value = ''

  try {
    targetResources.value = isAdmin.value
      ? await getAdminCustomerResources(selectedCustomerID.value)
      : await getAgentCustomerResources(selectedCustomerID.value)
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取客户资源失败'
  } finally {
    loading.value = false
  }
}

function openAdjust(account?: ResourceAccount) {
  const requested = account?.resource_type || 'ai_seconds'

  adjustResourceType.value =
    adminScope.value === 'customer'
      ? 'ai_seconds'
      : resourceOrder.includes(requested)
        ? requested
        : 'ai_seconds'
  adjustDelta.value = null
  adjustReason.value = ''
  error.value = ''
  notice.value = ''
  showAdjust.value = true
}

async function submitAdjust() {
  if (adjusting.value) return
  if (!adjustDelta.value) {
    error.value = isMarketingTimePage.value ? '增加时长必须大于 0' : '调整数量不能为 0'
    return
  }
  if (isMarketingTimePage.value && adjustDelta.value <= 0) {
    error.value = '运营申请只允许增加 AI 时长，不能扣减'
    return
  }
  if (!adjustReason.value.trim()) {
    error.value = isMarketingTimePage.value ? '请填写申请原因' : '请填写调整原因'
    return
  }

  adjusting.value = true
  error.value = ''

  try {
    if (
      adjustResourceType.value === 'device_slots' &&
      !Number.isInteger(adjustDelta.value)
    ) {
      error.value = '设备数量必须是整数'
      adjusting.value = false
      return
    }

    const resourceType =
      adminScope.value === 'customer' ? 'ai_seconds' : adjustResourceType.value

    const payload = {
      resource_type: resourceType,
      delta:
        resourceType === 'ai_seconds'
          ? Math.round(adjustDelta.value * 3600)
          : adjustDelta.value,
      reason: adjustReason.value.trim(),
    }

    if (isMarketingTimePage.value) {
      const organizationID = adminScope.value === 'agent'
        ? selectedAgentID.value
        : selectedCustomerID.value
      if (!organizationID) return
      await createCommercialAITimeGrantRequest({
        organization_id: organizationID,
        resource_seconds: payload.delta,
        reason: payload.reason,
      })
      showAdjust.value = false
      notice.value = 'AI 时长增加申请已提交财务审核，审批通过前余额不会变动。'
    } else {
      if (adminScope.value === 'agent') {
        if (!selectedAgentID.value) return
        await adjustAdminAgentResource(selectedAgentID.value, payload)
      } else {
        if (!selectedCustomerID.value) return
        await adjustAdminCustomerResource(
          selectedCustomerID.value,
          payload,
        )
      }

      showAdjust.value = false
      notice.value = '资源已调整，并写入资源流水。'
      await loadAdminTarget()
    }
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '调整资源失败'
  } finally {
    adjusting.value = false
  }
}

function openAllocate(_account?: ResourceAccount) {
  allocateQuantity.value = null
  allocateReason.value = ''
  error.value = ''
  notice.value = ''
  showAllocate.value = true
}

async function submitAllocate() {
  if (!selectedCustomerID.value || allocating.value) return
  if (!allocateQuantity.value || allocateQuantity.value <= 0) {
    error.value = '分配数量必须大于 0'
    return
  }
  if (!allocateReason.value.trim()) {
    error.value = '请填写分配原因'
    return
  }

  allocating.value = true
  error.value = ''

  try {
    await allocateAgentCustomerResource(selectedCustomerID.value, {
      resource_type: 'ai_seconds',
      quantity: Math.round(allocateQuantity.value * 3600),
      reason: allocateReason.value.trim(),
    })

    showAllocate.value = false
    notice.value =
      '资源已从代理账户划拨到终端，并生成双方资源流水。'
    await loadAgentData()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '分配资源失败'
  } finally {
    allocating.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page resource-page">
    <ModulePageNav
      v-if="isMarketingTimePage"
      context="activityMarketing"
      active-title="AI 时长"
      active-nav-title="AI 时长"
    />
    <ModulePageNav
      v-else-if="isFinanceTimePage"
      context="finance"
      active-title="AI 时长"
    />
    <ModulePageNav
      v-else
      :context="isAgent ? 'workspace-agent' : 'workspace-customer'"
      active-title="AI 时长"
      active-nav-title="AI 时长"
    />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">AI TIME CONTROL</p>
        <h2>AI 时长</h2>
        <p v-if="isAdmin && isMarketingTimePage">
          营销运维统一查看代理和终端 AI 时长；增加时长必须提交财务审核，审批通过后才实际入账。
        </p>
        <p v-else-if="isAdmin">
          查看代理、终端 AI 时长与完整资源流水。
        </p>
        <p v-else-if="isAgent">
          查看当前代理 AI 时长余额，并把可分配时长划拨给自己名下的终端。
        </p>
        <p v-else>
          查看当前终端可用 AI 时长和完整变动流水。
        </p>
      </div>
      <button
        class="ghost-button"
        type="button"
        :disabled="loading"
        @click="load"
      >
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <template v-if="isAdmin">
      <section class="settings-card resource-scope-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">AI TIME CONTROL</span>
            <h3>AI 时长对象</h3>
          </div>
          <button
            v-if="canAdjustSystemResource"
            class="primary-button"
            type="button"
            :disabled="
              adminScope === 'agent'
                ? !selectedAgentID
                : !selectedCustomerID
            "
            @click="openAdjust()"
          >
            {{ isMarketingTimePage ? '申请增加时长' : '调整时长' }}
          </button>
        </div>

        <div class="resource-scope-tabs">
          <button
            type="button"
            :class="{ active: adminScope === 'agent' }"
            @click="changeAdminScope('agent')"
          >
            代理 AI 时长
          </button>
          <button
            type="button"
            :class="{ active: adminScope === 'customer' }"
            @click="changeAdminScope('customer')"
          >
            终端 AI 时长
          </button>
        </div>

        <div class="resource-scope-select">
          <div v-if="adminScope === 'agent'" class="resource-picker-field">
            <span>选择代理</span>
            <button
              class="resource-picker-trigger"
              type="button"
              :disabled="!agents.length"
              @click="openAgentPicker"
            >
              <span class="resource-picker-trigger-main">
                <strong>{{ selectedAgent?.name || '请选择代理' }}</strong>
                <small v-if="selectedAgent">
                  {{ selectedAgent.username }} · {{ selectedAgent.code }}
                </small>
              </span>
              <span class="resource-picker-trigger-action">搜索切换</span>
            </button>
          </div>

          <div v-else class="resource-picker-field">
            <span>选择终端</span>
            <button class="resource-picker-trigger" type="button" :disabled="!customers.length" @click="openCustomerPicker">
              <span class="resource-picker-trigger-main">
                <strong>{{ selectedCustomer?.display_name || '请选择终端' }}</strong>
                <small v-if="selectedCustomer">{{ selectedCustomer.username }} · {{ selectedCustomer.phone || '无电话' }}</small>
              </span>
              <span class="resource-picker-trigger-action">搜索切换</span>
            </button>
          </div>

          <div
            v-if="adminScope === 'agent' && selectedAgent"
            class="resource-scope-meta"
          >
            <strong>{{ selectedAgent.name }}</strong>
            <span>{{ selectedAgent.phone || '联系电话未完善' }}</span>
            <span>{{ selectedAgent.customer_count }} 个终端</span>
          </div>

          <div
            v-else-if="adminScope === 'customer' && selectedCustomer"
            class="resource-scope-meta"
          >
            <strong>{{ selectedCustomer.display_name }}</strong>
            <span>{{ selectedCustomer.phone || '联系电话未完善' }}</span>
            <span>
              {{
                selectedCustomer.parent_org_type === 'agent'
                  ? '代理归属 · ' + selectedCustomer.parent_org_name
                  : '平台直营'
              }}
            </span>
          </div>
        </div>
      </section>
    </template>

    <template v-else-if="isAgent">
      <section class="settings-card resource-scope-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">MY RESOURCE ACCOUNT</span>
            <h3>我的代理资源</h3>
          </div>
          <span>{{ ownResources?.organization || '当前代理' }}</span>
        </div>

        <div
          v-if="ownResources"
          class="resource-account-grid compact"
        >
          <article
            v-for="item in orderedAccounts(ownResources.accounts, 'agent')"
            :key="'own-' + item.resource_type"
            class="resource-account-card"
          >
            <span>{{ resourceLabel(item.resource_type) }}</span>
            <strong>
              {{ formatQuantity(item.resource_type, item.balance) }}
            </strong>
            <em>{{ unitLabel(item.unit) }}</em>
          </article>
        </div>
      </section>

      <section class="settings-card resource-scope-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">CUSTOMER ALLOCATION</span>
            <h3>客户资源分配</h3>
          </div>
          <button
            v-if="canAdjustSystemResource && resourcePageTitle === '资源调整'"
            class="primary-button"
            type="button"
            :disabled="!selectedCustomerID"
            @click="openAllocate()"
          >
            给终端分配资源
          </button>
        </div>

        <div class="resource-scope-select">
          <div class="resource-picker-field">
            <span>选择终端</span>
            <button class="resource-picker-trigger" type="button" :disabled="!customers.length" @click="openCustomerPicker">
              <span class="resource-picker-trigger-main">
                <strong>{{ selectedCustomer?.display_name || '请选择终端' }}</strong>
                <small v-if="selectedCustomer">{{ selectedCustomer.username }} · {{ selectedCustomer.phone || '无电话' }}</small>
              </span>
              <span class="resource-picker-trigger-action">搜索切换</span>
            </button>
          </div>

          <div v-if="selectedCustomer" class="resource-scope-meta">
            <strong>{{ selectedCustomer.display_name }}</strong>
            <span>{{ selectedCustomer.phone || '联系电话未完善' }}</span>
            <span>{{ selectedCustomer.username }}</span>
          </div>
        </div>
      </section>
    </template>

    <section
      v-if="currentDashboard"
      class="resource-account-section"
    >
      <div class="resource-section-heading">
        <div>
          <span class="section-kicker">BALANCE</span>
          <h3>{{ currentDashboard.organization }} · 资源余额</h3>
        </div>
      </div>

      <div class="resource-account-grid">
        <article
          v-for="item in orderedAccounts(currentDashboard.accounts, currentDashboard.org_type)"
          :key="item.resource_type"
          class="resource-account-card"
        >
          <div>
            <span>{{ resourceLabel(item.resource_type) }}</span>
            <em>{{ item.status === 'active' ? '可用' : item.status }}</em>
          </div>
          <strong>
            {{ formatQuantity(item.resource_type, item.balance) }}
          </strong>
          <small>{{ unitLabel(item.unit) }}</small>
          <button
            v-if="isAdmin && canAdjustSystemResource"
            class="text-action"
            type="button"
            @click="openAdjust(item)"
          >
            {{ isMarketingTimePage ? '申请增加' : '调整' }}
          </button>
        </article>
      </div>
    </section>

    <section
      v-if="isAgent && targetResources"
      class="resource-account-section customer-resource-section"
    >
      <div class="resource-section-heading">
        <div>
          <span class="section-kicker">CUSTOMER BALANCE</span>
          <h3>{{ targetResources.organization }} · 客户资源</h3>
        </div>
      </div>

      <div class="resource-account-grid">
        <article
          v-for="item in orderedAccounts(targetResources.accounts, 'customer')"
          :key="'customer-' + item.resource_type"
          class="resource-account-card"
        >
          <div>
            <span>{{ resourceLabel(item.resource_type) }}</span>
          </div>
          <strong>
            {{ formatQuantity(item.resource_type, item.balance) }}
          </strong>
          <small>{{ unitLabel(item.unit) }}</small>
          <button
            class="text-action"
            type="button"
            @click="openAllocate(item)"
          >
            分配
          </button>
        </article>
      </div>
    </section>

    <section
      v-if="currentDashboard"
      class="settings-card resource-ledger-card"
    >
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">RESOURCE LEDGER</span>
          <h3>资源流水</h3>
        </div>
        <span>{{ filteredCurrentLedger.length }} 条</span>
      </div>

      <div class="resource-ledger-toolbar">
        <input
          v-model="resourceLedgerSearch"
          type="search"
          placeholder="搜索业务 / 原因 / 流水编号"
          @input="resourceLedgerPage = 1"
        />
      </div>

      <div
        v-if="!filteredCurrentLedger.length"
        class="empty-state"
      >
        暂无资源流水。平台调整或代理分配后会自动记录。
      </div>

      <div v-else class="resource-ledger-list">
        <article
          v-for="item in pagedCurrentLedger"
          :key="item.id"
          class="resource-ledger-row"
        >
          <div>
            <strong>{{ resourceLabel(item.resource_type) }}</strong>
            <span>{{ businessLabel(item.business_type) }}</span>
          </div>
          <div
            class="resource-ledger-change"
            :class="{ negative: item.change_quantity < 0 }"
          >
            {{ item.change_quantity > 0 ? '+' : '' }}{{
              formatQuantity(
                item.resource_type,
                item.change_quantity,
              )
            }} {{ item.resource_type === 'ai_seconds' ? '小时' : '台' }}
          </div>
          <div>
            <span class="muted-label">余额变化</span>
            <strong>
              {{
                formatQuantity(
                  item.resource_type,
                  item.balance_before,
                )
              }} {{ item.resource_type === 'ai_seconds' ? '小时' : '台' }}
              →
              {{
                formatQuantity(
                  item.resource_type,
                  item.balance_after,
                )
              }} {{ item.resource_type === 'ai_seconds' ? '小时' : '台' }}
            </strong>
          </div>
          <div>
            <span class="muted-label">原因</span>
            <strong>{{ item.reason || '—' }}</strong>
          </div>
          <time>{{ formatDate(item.created_at) }}</time>
        </article>
      </div>
      <PaginationBar
        :page="Math.min(resourceLedgerPage, resourceLedgerPageCount)"
        :total-pages="resourceLedgerPageCount"
        :total="filteredCurrentLedger.length"
        :page-size="resourceLedgerPageSize"
        @update:page="resourceLedgerPage = $event"
      />
    </section>

    <div
      v-if="showAgentPicker"
      class="modal-backdrop resource-agent-picker-backdrop"
      @click.self="showAgentPicker = false"
    >
      <section class="modal-card resource-agent-picker-modal">
        <div class="modal-header">
          <div>
            <p class="section-kicker">AGENT SELECTOR</p>
            <h3>选择代理</h3>
          </div>
          <button class="icon-button" type="button" @click="showAgentPicker = false">×</button>
        </div>

        <label class="resource-agent-picker-search">
          <span>搜索代理</span>
          <input
            v-model.trim="agentSearch"
            type="search"
            autofocus
            placeholder="输入代理名称、账号、编码、联系人或手机号"
            @input="agentPage = 1"
          />
        </label>

        <div class="resource-agent-picker-summary">
          <span>全部 {{ agents.length }} 个代理</span>
          <span v-if="agentSearch">匹配 {{ filteredAgents.length }} 个</span>
        </div>

        <div class="resource-agent-picker-list">
          <button
            v-for="item in pagedAgents"
            :key="item.organization_id"
            class="resource-agent-picker-row"
            :class="{ active: item.organization_id === selectedAgentID }"
            type="button"
            @click="chooseAgent(item)"
          >
            <span class="resource-agent-picker-avatar">{{ (item.name || item.username || '代').slice(0, 1) }}</span>
            <span class="resource-agent-picker-copy">
              <strong>{{ item.name }}</strong>
              <small>{{ item.username }} · {{ item.code }}</small>
              <em>{{ item.display_name || '负责人未设置' }} · {{ item.phone || '电话未设置' }}</em>
            </span>
            <span class="resource-agent-picker-stats">
              <strong>{{ item.customer_count }}</strong>
              <small>终端</small>
            </span>
            <span class="resource-agent-picker-status">{{ item.status === 'active' ? '正常' : item.status }}</span>
          </button>

          <div v-if="!pagedAgents.length" class="resource-agent-picker-empty">
            没有找到匹配的代理，请更换关键词。
          </div>
        </div>

        <PaginationBar
          :page="Math.min(agentPage, agentPageCount)"
          :total-pages="agentPageCount"
          :total="filteredAgents.length"
          :page-size="agentPageSize"
          @update:page="agentPage = $event"
        />
      </section>
    </div>

    <div
      v-if="showCustomerPicker"
      class="modal-backdrop resource-agent-picker-backdrop"
      @click.self="showCustomerPicker = false"
    >
      <section class="modal-card resource-agent-picker-modal">
        <div class="modal-header">
          <div>
            <p class="section-kicker">CUSTOMER SELECTOR</p>
            <h3>选择终端</h3>
          </div>
          <button class="icon-button" type="button" @click="showCustomerPicker = false">×</button>
        </div>

        <label class="resource-agent-picker-search">
          <span>搜索终端</span>
          <input
            v-model.trim="customerSearch"
            type="search"
            autofocus
            placeholder="输入终端名称、账号、手机号或归属"
            @input="customerPage = 1"
          />
        </label>

        <div class="resource-agent-picker-summary">
          <span>全部 {{ customers.length }} 个终端</span>
          <span v-if="customerSearch">匹配 {{ filteredCustomers.length }} 个</span>
        </div>

        <div class="resource-agent-picker-list">
          <button
            v-for="item in pagedCustomers"
            :key="item.tenant_id"
            class="resource-agent-picker-row"
            :class="{ active: item.tenant_id === selectedCustomerID }"
            type="button"
            @click="chooseCustomer(item)"
          >
            <span class="resource-agent-picker-avatar">{{ (item.display_name || item.username || '终').slice(0, 1) }}</span>
            <span class="resource-agent-picker-copy">
              <strong>{{ item.display_name }}</strong>
              <small>{{ item.username }} · {{ item.phone || '电话未设置' }}</small>
              <em>{{ item.parent_org_name || '平台直营' }}</em>
            </span>
            <span class="resource-agent-picker-status">终端</span>
          </button>

          <div v-if="!pagedCustomers.length" class="resource-agent-picker-empty">
            没有找到匹配的终端，请更换关键词。
          </div>
        </div>

        <PaginationBar
          :page="Math.min(customerPage, customerPageCount)"
          :total-pages="customerPageCount"
          :total="filteredCustomers.length"
          :page-size="customerPageSize"
          @update:page="customerPage = $event"
        />
      </section>
    </div>

    <div
      v-if="showAdjust"
      class="modal-backdrop"
      @click.self="showAdjust = false"
    >
      <form class="modal-card" @submit.prevent="submitAdjust">
        <div class="modal-header">
          <div>
            <p class="section-kicker">{{ isMarketingTimePage ? 'AI TIME REQUEST' : 'SYSTEM ADJUSTMENT' }}</p>
            <h3>
              {{
                isMarketingTimePage
                  ? (adminScope === 'agent' ? '申请增加代理 AI 时长' : '申请增加终端 AI 时长')
                  : (adminScope === 'agent' ? '调整代理资源' : '调整客户资源')
              }}
            </h3>
          </div>
          <button
            class="icon-button"
            type="button"
            @click="showAdjust = false"
          >
            ×
          </button>
        </div>

        <p class="modal-helper">
          {{ isMarketingTimePage
            ? '运营只允许申请增加时长。提交后进入财务待审核，审批通过前余额不会变化。'
            : '正数为增加，负数为扣减。扣减后的资源余额不能小于 0。' }}
        </p>

        <div class="form-grid">
          <label>
            <span>资源类型</span>
            <select v-if="adminScope === 'agent'" v-model="adjustResourceType">
              <option
                v-for="type in resourceOrder"
                :key="type"
                :value="type"
              >
                {{ resourceLabel(type) }}
              </option>
            </select>
            <input v-else value="AI 时长（小时）" disabled />
          </label>

          <label>
            <span>{{ isMarketingTimePage ? '增加小时数' : (adjustResourceType === 'ai_seconds' ? '调整小时数' : '调整设备数量') }}</span>
            <input
              v-model.number="adjustDelta"
              type="number"
              :min="isMarketingTimePage ? 0.1 : undefined"
              :step="adjustResourceType === 'ai_seconds' ? 0.1 : 1"
              required
              :placeholder="isMarketingTimePage ? '例如 10（小时）' : (adjustResourceType === 'ai_seconds' ? '例如 10 或 -5（小时）' : '例如 10 或 -5（台）')"
            />
          </label>

          <label class="form-span-2">
            <span>{{ isMarketingTimePage ? '申请原因' : '调整原因' }}</span>
            <input
              v-model="adjustReason"
              maxlength="512"
              required
               :placeholder="isMarketingTimePage ? '例如：客户活动赠送 / 售后补偿 / 运营补时' : '例如：首批资源包 / 终端增购'"
            />
          </label>
        </div>

        <div class="modal-actions">
          <button
            class="ghost-button"
            type="button"
            @click="showAdjust = false"
          >
            取消
          </button>
          <button
            class="primary-button"
            type="submit"
            :disabled="adjusting"
          >
            {{ adjusting ? '提交中...' : (isMarketingTimePage ? '提交财务审核' : '确认调整') }}
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="showAllocate"
      class="modal-backdrop"
      @click.self="showAllocate = false"
    >
      <form class="modal-card" @submit.prevent="submitAllocate">
        <div class="modal-header">
          <div>
            <p class="section-kicker">RESOURCE ALLOCATION</p>
            <h3>给终端分配资源</h3>
          </div>
          <button
            class="icon-button"
            type="button"
            @click="showAllocate = false"
          >
            ×
          </button>
        </div>

        <p class="modal-helper">
          分配会同时扣减代理余额并增加终端余额，双方各生成一条资源流水。
        </p>

        <div class="form-grid">
          <label>
            <span>资源类型</span>
            <input value="AI 时长（小时）" disabled />
          </label>

          <label>
            <span>分配小时数</span>
            <input
              v-model.number="allocateQuantity"
              type="number"
              min="0.1"
              step="0.1"
              required
              placeholder="请输入小时数"
            />
          </label>

          <label class="form-span-2">
            <span>分配原因</span>
            <input
              v-model="allocateReason"
              maxlength="512"
              required
              placeholder="例如：终端首期 AI 时长"
            />
          </label>
        </div>

        <div class="modal-actions">
          <button
            class="ghost-button"
            type="button"
            @click="showAllocate = false"
          >
            取消
          </button>
          <button
            class="primary-button"
            type="submit"
            :disabled="allocating"
          >
            {{ allocating ? '分配中...' : '确认分配' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>
