<script setup lang="ts">
import PasswordInput from './components/PasswordInput.vue'
import GlobalFeedback from './components/GlobalFeedback.vue'
import RegionSelect from './components/RegionSelect.vue'
import SystemFooter from './components/SystemFooter.vue'
import CustomerMembershipCenter from './components/CustomerMembershipCenter.vue'
import SystemAgentLayer from './components/SystemAgentLayer.vue'
import TodoBadge from './components/TodoBadge.vue'
import { startInbox, stopInbox, canUseWorkInbox } from './workInbox'
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  changePassword,
  getAccountDashboard,
  getLiveQuotaSummary,
  getUserUIPreferences,
  logout,
  updateUserUIPreferences,
  updateAccountProfile,
} from './api'
import { clearSession, loadSession, session } from './session'
import {
  coreRuntime,
  startCoreRuntimeWatch,
  stopCoreRuntimeWatch,
} from './coreRuntime'

interface NavItem {
  label: string
  to: string
  icon: string
  routeNames: string[]
}

interface NavSection {
  label: string
  items: NavItem[]
}

const route = useRoute()
const router = useRouter()

const loggingOut = ref(false)
const sidebarCollapsed = ref(true)

function sidebarPreferenceCacheKey(userID: number) {
  return 'xiaolan-ui:' + String(userID) + ':sidebar-collapsed'
}

function restoreSidebarPreferenceFromLocal(userID: number) {
  const raw = window.localStorage.getItem(sidebarPreferenceCacheKey(userID))
  if (raw === '0') sidebarCollapsed.value = false
  if (raw === '1') sidebarCollapsed.value = true
}

function persistSidebarPreferenceLocal(userID: number) {
  window.localStorage.setItem(sidebarPreferenceCacheKey(userID), sidebarCollapsed.value ? '1' : '0')
}

async function loadSidebarPreference(userID: number) {
  restoreSidebarPreferenceFromLocal(userID)
  try {
    const preferences = await getUserUIPreferences()
    sidebarCollapsed.value = preferences.sidebar_collapsed
    persistSidebarPreferenceLocal(userID)
  } catch {
    // Local cache keeps the shell stable while management-service is restarting.
  }
}

function toggleSidebarCollapsed() {
  const userID = Number(actor.value?.user_id || 0)
  sidebarCollapsed.value = !sidebarCollapsed.value
  if (userID) persistSidebarPreferenceLocal(userID)
  void updateUserUIPreferences({ sidebar_collapsed: sidebarCollapsed.value }).catch(() => undefined)
}

const isAuthPage = computed(() => {
  const routeName = String(route.name || '')
  return (
    routeName === 'login' ||
    routeName === 'register' ||
    route.path === '/login' ||
    route.path === '/register' ||
    route.meta.public === true
  )
})
const actor = computed(() => session.bootstrap?.actor)
const showAuthenticatedShell = computed(
  () => !isAuthPage.value && Boolean(session.bootstrap?.actor),
)
const staffAccess = computed(() => session.bootstrap?.staff_access ?? null)

const customerLiveRouteNames = new Set([
  'rooms',
  'rooms-list',
  'room-detail',
  'live-strategy',
  'live-devices',
])
const showCoreRuntimeNotice = computed(
  () =>
    showAuthenticatedShell.value &&
    customerLiveRouteNames.has(String(route.name || '')) &&
    (coreRuntime.phase === 'offline' || coreRuntime.phase === 'recovering'),
)
const coreRuntimeNoticeTitle = computed(() =>
  coreRuntime.phase === 'recovering'
    ? 'Core 已恢复，正在同步实时状态'
    : 'Core 服务异常，实时状态未知',
)
const coreRuntimeNoticeText = computed(() =>
  coreRuntime.phase === 'recovering'
    ? '正在重新获取直播间真实状态并恢复基础采集；付费 Agent / 声音服务不会自动重新启动。'
    : '系统正在自动重连。直播间实时状态、采集、Agent、录制和设备实时控制暂不可用；策略、历史记录、财务等功能仍可使用。',
)
const showCustomerLiveQuota = computed(
  () =>
    actor.value?.role === 'customer' &&
    String(route.name || '') !== 'live-strategy' &&
    customerLiveRouteNames.has(String(route.name || '')),
)
const customerLiveQuota = ref<Awaited<ReturnType<typeof getLiveQuotaSummary>> | null>(null)
const customerLiveQuotaExpanded = ref(false)
let customerLiveQuotaTimer: number | undefined

function formatCustomerAIQuota(seconds = 0) {
  const totalMinutes = Math.max(0, Math.ceil(Number(seconds || 0) / 60))
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return hours + '小时' + minutes + '分'
}

function formatCustomerAIUsage(seconds = 0) {
  const totalMinutes = Math.max(0, Math.floor(Number(seconds || 0) / 60))
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  return [hours, minutes].map(value => String(value).padStart(2, '0')).join(':')
}

async function refreshCustomerLiveQuota() {
  if (!showCustomerLiveQuota.value) return
  try {
    customerLiveQuota.value = await getLiveQuotaSummary()
  } catch {
    // Keep the last visible value during transient development-service restarts.
  }
}

function stopCustomerLiveQuotaPolling() {
  if (customerLiveQuotaTimer !== undefined) {
    window.clearInterval(customerLiveQuotaTimer)
    customerLiveQuotaTimer = undefined
  }
}

watch(
  showCustomerLiveQuota,
  (visible) => {
    stopCustomerLiveQuotaPolling()
    if (!visible) {
      customerLiveQuota.value = null
      customerLiveQuotaExpanded.value = false
      return
    }
    void refreshCustomerLiveQuota()
    customerLiveQuotaTimer = window.setInterval(() => {
      void refreshCustomerLiveQuota()
    }, 15000)
  },
  { immediate: true },
)

onBeforeUnmount(stopCustomerLiveQuotaPolling)

watch(
  () => showAuthenticatedShell.value ? String(actor.value?.user_id || '') : '',
  (key) => {
    if (key) startCoreRuntimeWatch(key)
    else stopCoreRuntimeWatch()
  },
  { immediate: true },
)
onBeforeUnmount(stopCoreRuntimeWatch)

watch(
  () => showAuthenticatedShell.value ? Number(actor.value?.user_id || 0) : 0,
  userID => {
    if (!userID) return
    void loadSidebarPreference(userID)
  },
  { immediate: true },
)

const isAdmin = computed(() => actor.value?.role === 'platform_admin')
const isAgent = computed(() => actor.value?.role === 'agent_admin')
const isSales = computed(() => actor.value?.role === 'sales_staff')
const isStaff = computed(() => actor.value?.role === 'staff')
const isInternalStaff = computed(
  () => isAdmin.value || isSales.value || isStaff.value,
)

function hasStaffPermission(code: string) {
  const access = staffAccess.value
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes(code)),
  )
}

function navItem(
  label: string,
  to: string,
  icon: string,
  routeNames: string[],
): NavItem {
  return { label, to, icon, routeNames }
}

function navSectionIcon(section: NavSection) {
  const label = section.label
  if (label.includes('系统')) return '⌂'
  if (label.includes('业务') || label.includes('直播')) return '▣'
  if (label.includes('客户') || label.includes('销售')) return '客'
  if (label.includes('渠道') || label.includes('代理')) return '合'
  if (label.includes('产品') || label.includes('交付')) return '◆'
  if (label.includes('财务')) return '¥'
  if (label.includes('终端')) return '终'
  if (label.includes('工作台')) return '台'
  return '组'
}

function navSectionTone(section: NavSection) {
  const label = section.label
  if (label.includes('客户') || label.includes('销售')) return 'nav-tone-cyan'
  if (label.includes('渠道') || label.includes('代理')) return 'nav-tone-orange'
  if (label.includes('产品') || label.includes('交付')) return 'nav-tone-purple'
  if (label.includes('财务')) return 'nav-tone-green'
  if (label.includes('业务') || label.includes('直播')) return 'nav-tone-sky'
  return 'nav-tone-blue'
}

const navSections = computed<NavSection[]>(() => {
  if (isAdmin.value) {
    return [
      {
        label: '系统管理',
        items: [
          navItem('系统总览', '/overview', '⌂', ['platform-overview']),
          navItem('系统设定', '/system/settings', '设', ['system-settings', 'system-agent-routing']),
          navItem('组织架构', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-permissions', 'staff-approvals', 'staff-audit']),
        ],
      },
      {
        label: '营销运维',
        items: [
          navItem('直播运维', '/operations/live', '▣', ['live-hub', 'live-monitor', 'live-events', 'live-room-quotas', 'live-strategy', 'live-analysis-settings', 'live-devices', 'rooms', 'rooms-list', 'room-detail']),
          navItem('活动营销', '/operations/live/marketing', '营', ['live-activity-marketing', 'commercial-membership-plans', 'commercial-membership-simulator', 'commercial-ai-time', 'commercial-marketing', 'commercial-marketing-tools', 'commercial-marketing-channels', 'commercial-marketing-analytics', 'commercial-time-cards', 'commercial-device-products', 'commercial-referrals', 'invitations']),
        ],
      },
      {
        label: '客资销售',
        items: [
          navItem('客户资源', '/customers', '◎', ['customers-hub', 'customers-list']),
          navItem('销售体系', '/sales', '◈', ['sales-hub', 'sales-team', 'sales-performance']),
        ],
      },
      {
        label: '仓储与售后',
        items: [
          navItem('设备与仓储', '/resources', '◌', ['resources-hub', 'resource-devices', 'resource-inventory', 'resource-device-products', 'resource-logistics']),
          navItem('物流与售后', '/staff/after-sales', '修', ['staff-after-sales']),
        ],
      },
      {
        label: '财务管理',
        items: [
          navItem('邀请与推荐', '/staff/finance/invitations', '邀', ['staff-finance-invitations']),          navItem('客户收款确认', '/staff/finance/receipts', '款', ['staff-finance-receipts', 'staff-finance-customer-money']),
          navItem('财务与结算', '/staff/finance', '¥', ['staff-finance-hub', 'staff-finance-accounts', 'staff-finance-approvals', 'staff-finance-ledger', 'staff-finance-history', 'staff-finance-trace', 'commercial-settlement', 'staff-finance-settlements']),
        ],
      },
      {
        label: '渠道合作',
        items: [
          navItem('代理合作', '/agents', '◇', ['agents-hub', 'agents-list', 'agent-levels', 'agent-contracts', 'agent-exit']),
        ],
      },
    ]
  }

  if (isAgent.value) {
    return [
      {
        label: '代理工作台',
        items: [
          navItem('代理总览', '/agent/overview', '⌂', ['agent-overview']),
          navItem('客户资源', '/agent/customers', '◎', ['agent-customers']),
          navItem('AI 时长', '/resources/workspace', '时', ['resources-workspace']),
          navItem('售后维修', '/after-sales', '修', ['after-sales-portal']),
          navItem('邀请与推荐', '/invitations', '↗', ['invitations']),
        ],
      },
    ]
  }

  if (isInternalStaff.value) {
    const workItems: NavItem[] = []
    const access = staffAccess.value
    const groupCode = access?.primary_group_code || ''
    const roleCodes = new Set(access?.role_codes || [])
    const isPrimaryGroupManager =
      roleCodes.has(groupCode + '_manager') ||
      Boolean(
        access?.primary_group_id &&
          access.managed_group_ids?.includes(access.primary_group_id),
      )

    const addOrganization = () => {
      workItems.push(
        navItem(
          '组织架构',
          '/staff',
          '♜',
          ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit'],
        ),
      )
    }

    if (groupCode === 'management') {
      workItems.push(navItem('系统总览', '/overview', '⌂', ['platform-overview']))
      addOrganization()
    } else if (groupCode === 'finance') {
      workItems.push(
        navItem('客户收款确认', '/staff/finance/receipts', '款', ['staff-finance-receipts','staff-finance-customer-money']),
        navItem('财务与结算', '/staff/finance', '¥', ['staff-finance-hub', 'staff-finance-accounts', 'staff-finance-approvals', 'staff-finance-ledger', 'staff-finance-history', 'staff-finance-trace', 'commercial-settlement', 'staff-finance-settlements']),
      )
      if (isPrimaryGroupManager) addOrganization()
    } else if (groupCode === 'sales') {
      if (isSales.value) {
        workItems.push(
          navItem('销售工作台', '/sales/workspace', '工', ['sales-workspace']),
          navItem('意向顾客', '/sales/leads', '意', ['sales-leads']),
          navItem('我的客户', '/sales/customers', '客', ['sales-customers']),
          navItem('收款进度', '/sales/receipts', '款', ['sales-receipts','sales-customer-money']),
          navItem('运维协助', '/sales/support', '助', ['sales-support']),
          navItem('跟进与回访', '/sales/followups', '访', ['sales-followups']),
          navItem('产品与报价', '/sales/catalog', '价', ['sales-catalog']),
          navItem('我的业绩', '/sales/my-performance', '绩', ['sales-my-performance']),
          navItem('邀请与推荐', '/invitations', '邀', ['invitations']),
        )
      } else {
        if (hasStaffPermission('customer.view_all')) {
          workItems.push(navItem('客户资源', '/customers', '◎', ['customers-hub', 'customers-list']))
        }
        if (hasStaffPermission('sales.view_all')) {
          workItems.push(navItem('销售体系', '/sales', '◈', ['sales-hub', 'sales-team', 'sales-performance']))
        }
        if (isPrimaryGroupManager) addOrganization()
      }
    } else if (groupCode === 'live_operations') {
      workItems.push(
        navItem('运维协助', '/operations/support', '助', ['operations-support']),
        navItem('直播运维', '/operations/live', '▣', ['live-hub', 'live-monitor', 'live-events', 'live-room-quotas', 'live-strategy', 'live-devices', 'rooms', 'rooms-list', 'room-detail']),
        navItem(
          '活动营销',
          '/operations/live/marketing',
          '营',
          [
            'live-activity-marketing',
            'commercial-membership-plans',
            'commercial-membership-simulator',
            'commercial-ai-time',
            'commercial-marketing',
            'commercial-marketing-tools',
            'commercial-marketing-channels',
            'commercial-marketing-analytics',
            'commercial-time-cards',
            'commercial-device-products',
            'commercial-referrals',
            ...(hasStaffPermission('invitations.view_all') ? ['invitations'] : []),
          ],
        ),
      )
      if (isPrimaryGroupManager) addOrganization()
    } else if (groupCode === 'warehouse_after_sales') {
      workItems.push(
        navItem('设备与仓储', '/resources', '◌', ['resources-hub', 'resource-devices', 'resource-inventory', 'resource-logistics']),
        navItem('物流与售后', '/staff/after-sales', '修', ['staff-after-sales']),
      )
      if (isPrimaryGroupManager) addOrganization()
    } else {
      if (hasStaffPermission('system.architecture.view')) {
        workItems.push(navItem('系统总览', '/overview', '⌂', ['platform-overview']))
      }
      if (
        hasStaffPermission('liveops.configure') ||
        hasStaffPermission('liveops.view_all') ||
        hasStaffPermission('liveops.room_quota.view')
      ) {
        workItems.push(navItem('直播运维', '/operations/live', '▣', ['live-hub', 'live-monitor', 'live-events', 'live-room-quotas', 'live-strategy', 'live-devices', 'rooms', 'rooms-list', 'room-detail']))
      }
      if (hasStaffPermission('finance.dashboard.view')) {
        workItems.push(navItem('财务与结算', '/staff/finance', '¥', ['staff-finance-hub']))
      }
      if (hasStaffPermission('inventory.view') || hasStaffPermission('logistics.view')) {
        workItems.push(navItem('设备与仓储', '/resources', '◌', ['resources-hub', 'resource-devices', 'resource-inventory', 'resource-device-products', 'resource-logistics']))
      }
      if (
        hasStaffPermission('staff.group.view') ||
        hasStaffPermission('staff.employee.view') ||
        hasStaffPermission('staff.role.view')
      ) {
        addOrganization()
      }
    }

    if (
      hasStaffPermission('system.settings.agent_routing.manage') &&
      !workItems.some((item) => item.to === '/system/settings/agent-routing')
    ) {
      workItems.push(navItem('智能体理解配置', '/system/settings/agent-routing', '智', ['system-agent-routing']))
    }

    if (hasStaffPermission('finance.dashboard.view') && !workItems.some((item) => item.to === '/staff/finance/receipts')) {
      workItems.push(navItem('客户收款确认', '/staff/finance/receipts', '款', ['staff-finance-receipts', 'staff-finance-customer-money']))
    }
    if (hasStaffPermission('finance.dashboard.view')) {
      workItems.push(navItem(workItems.some(item => item.to === '/invitations') ? '邀请与推荐（财务）' : '邀请与推荐', '/staff/finance/invitations', '邀', ['staff-finance-invitations']))
    }

    return [
      {
        label: access?.primary_group_name || '内部员工',
        items: workItems,
      },
    ]
  }

  return [
    {
      label: '终端工作台',
      items: [
        navItem('直播运维', '/', '▣', ['rooms', 'rooms-list', 'room-detail', 'live-strategy', 'live-devices']),
        navItem('运维协助', '/support', '助', ['customer-support']),
        navItem('AI 时长', '/resources/workspace', '时', ['resources-workspace']),
        navItem('终端商城', '/shop', '▤', ['shop']),
        navItem('售后维修', '/after-sales', '修', ['after-sales-portal']),
        navItem('邀请与推荐', '/invitations', '↗', ['invitations']),
        navItem('我的钱包', '/finance', '¥', ['finance']),
        navItem('资金记录', '/finance/records', '流', ['customer-money']),
      ],
    },
  ]
})

function navActive(item: NavItem) {
  return item.routeNames.includes(String(route.name || ''))
}

const collapsedNavSections = ref<Record<string, boolean>>({})

function sectionHasActive(section: NavSection) {
  return section.items.some((item) => navActive(item))
}

function sectionCollapsed(section: NavSection) {
  return collapsedNavSections.value[section.label] === true
}

async function openNavSection(section: NavSection) {
  collapsedNavSections.value = {
    ...collapsedNavSections.value,
    [section.label]: false,
  }
  const target = section.items[0]?.to
  if (!target) return
  if (route.path === target) return
  await router.push(target)
}


watch(
  () => String(route.name || ''),
  () => {
    const activeSection = navSections.value.find((section) => sectionHasActive(section))
    if (activeSection) {
      collapsedNavSections.value = {
        ...collapsedNavSections.value,
        [activeSection.label]: false,
      }
    }

  },
  { immediate: true },
)

const mobileNavItems = computed<NavItem[]>(() => {
  if (isAdmin.value) {
    return [
      navItem('系统总览', '/overview', '⌂', ['platform-overview']),
      navItem('设定', '/system/settings', '设', ['system-settings']),
      navItem('组织', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit']),
      navItem('客户', '/customers', '◎', ['customers-hub', 'customers-list']),
      navItem('营销', '/operations/live/marketing', '营', [
        'live-activity-marketing',
        'commercial-membership-plans',
        'commercial-membership-simulator',
        'commercial-ai-time',
        'commercial-marketing',
        'commercial-time-cards',
        'commercial-device-products',
        'commercial-referrals',
      ]),
      navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
    ]
  }

  if (isAgent.value) {
    return [
      navItem('总览', '/agent/overview', '⌂', ['agent-overview']),
      navItem('终端', '/agent/customers', '◎', ['agent-customers']),
      navItem('资源', '/resources/workspace', '时', ['resources-workspace']),
      navItem('售后', '/after-sales', '修', ['after-sales-portal']),
      navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
    ]
  }

  if (isSales.value) {
    return [
      navItem('工作台', '/sales/workspace', '工', ['sales-workspace']),
      navItem('意向', '/sales/leads', '意', ['sales-leads']),
      navItem('客户', '/sales/customers', '客', ['sales-customers']),
      navItem('跟进与回访', '/sales/followups', '访', ['sales-followups']),
      navItem('报价', '/sales/catalog', '价', ['sales-catalog']),
      navItem('业绩', '/sales/my-performance', '绩', ['sales-my-performance']),
      navItem('邀请', '/invitations', '邀', ['invitations']),
      navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
    ]
  }

  if (isStaff.value) {
    const items: NavItem[] = [
      navItem('组织', '/staff', '♜', ['staff-hub', 'staff-groups', 'staff-employees', 'staff-roles', 'staff-approvals', 'staff-audit']),
    ]
    if (hasStaffPermission('system.architecture.view')) {
      items.unshift(navItem('系统', '/overview', '⌂', ['platform-overview']))
    }
    if (hasStaffPermission('system.settings.view')) {
      items.push(navItem('设定', '/system/settings', '设', ['system-settings']))
    }
    if (
      hasStaffPermission('liveops.configure') ||
      hasStaffPermission('liveops.view_all') ||
      hasStaffPermission('liveops.room_quota.view')
    ) {
      items.push(
        navItem('直播运维', '/operations/live', '▣', ['live-hub', 'live-monitor', 'live-events', 'live-room-quotas', 'live-strategy', 'live-devices', 'rooms', 'rooms-list', 'room-detail']),
      )
    }
    if (
      hasStaffPermission('finance.dashboard.view') ||
      hasStaffPermission('finance.settlement_rules.view')
    ) {
      items.push(
        navItem('财务', '/staff/finance', '¥', ['staff-finance-hub', 'staff-finance-accounts', 'staff-finance-approvals', 'staff-finance-ledger', 'staff-finance-history', 'staff-finance-trace', 'commercial-settlement', 'staff-finance-settlements']),
      )
    }
    if (hasStaffPermission('customer.view_all')) {
      items.push(navItem('客户', '/customers', '◎', ['customers-hub', 'customers-list']))
    }
    if (
      hasStaffPermission('commercial.marketing.view') ||
      hasStaffPermission('commercial.referral.view')
    ) {
      items.push(
        navItem(
          '营销',
          '/operations/live/marketing',
          '营',
          ['live-activity-marketing', 'commercial-membership-plans', 'commercial-membership-simulator', 'commercial-ai-time', 'commercial-marketing', 'commercial-time-cards', 'commercial-device-products', 'commercial-referrals'],
        ),
      )
    }
    if (
      hasStaffPermission('resources.view') ||
      hasStaffPermission('finance.resource.adjust')
    ) {
      items.push(navItem('资源', '/resources', '◌', ['resources-hub', 'resources-workspace', 'resource-devices', 'resource-inventory', 'resource-logistics']))
    }
    if (hasStaffPermission('inventory.after_sales.view')) {
      items.push(navItem('售后', '/staff/after-sales', '修', ['staff-after-sales']))
    }
    items.push(navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']))
    return items.slice(0, 5)
  }

  return [
    navItem('直播运维', '/', '▣', ['rooms', 'rooms-list', 'room-detail', 'live-strategy', 'live-devices']),
    navItem('终端商城', '/shop', '▤', ['shop']),
    navItem('财务', '/finance', '¥', ['finance']),
    navItem('售后', '/after-sales', '修', ['after-sales-portal']),
    navItem('我的', '/personal', '♙', ['personal-center', 'account', 'settings']),
  ]
})

const topbarEyebrow = computed(() => {
  if (isAdmin.value) return 'SYSTEM'
  if (isInternalStaff.value) return 'STAFF CONSOLE'
  if (isAgent.value) return 'AGENT CONSOLE'
  return 'BANBO AI'
})

const topbarTitle = computed(() => {
  if (isAdmin.value) return '系统'
  if (isAgent.value) return '代理工作台'
  if (isSales.value) return '销售工作台'
  if (isStaff.value) {
    return (staffAccess.value?.primary_group_name || '内部员工') + '工作台'
  }
  return '终端工作台'
})

const accountRoleLabel = computed(() => {
  if (isAdmin.value) return '超级系统管理员'
  if (isAgent.value) return '代理账号'
  if (isSales.value) {
    return staffAccess.value?.primary_group_name || '销售部'
  }
  if (isStaff.value) {
    return staffAccess.value?.primary_group_name || '内部员工'
  }
  return '终端账号'
})

const mustChangePassword = computed(
  () => actor.value?.must_change_password === true,
)

const mustCompleteContact = computed(() => {
  if (!actor.value) return false
  return !(
    actor.value.phone?.trim() &&
    actor.value.province?.trim() &&
    actor.value.city?.trim() &&
    actor.value.district?.trim()
  )
})

const forcedCurrentPassword = ref('')
const forcedNewPassword = ref('')
const forcedConfirmPassword = ref('')
const forcedPasswordSubmitting = ref(false)
const forcedPasswordError = ref('')

const requiredPhone = ref('')
const requiredProvince = ref('')
const requiredCity = ref('')
const requiredDistrict = ref('')
const requiredPhoneSubmitting = ref(false)
const requiredPhoneError = ref('')

watch(
  mustCompleteContact,
  (required) => {
    if (!required || !actor.value) return
    requiredPhone.value = actor.value.phone?.trim() || ''
    requiredProvince.value = actor.value.province?.trim() || ''
    requiredCity.value = actor.value.city?.trim() || ''
    requiredDistrict.value = actor.value.district?.trim() || ''
  },
  { immediate: true },
)

async function submitRequiredPhone() {
  requiredPhoneError.value = ''
  const phoneValue = requiredPhone.value.trim()
  const provinceValue = requiredProvince.value.trim()
  const cityValue = requiredCity.value.trim()
  const districtValue = requiredDistrict.value.trim()

  if (!phoneValue) {
    requiredPhoneError.value = '联系电话不能为空'
    return
  }
  if (!provinceValue || !cityValue || !districtValue) {
    requiredPhoneError.value = '省、市、区/县不能为空'
    return
  }

  requiredPhoneSubmitting.value = true
  try {
    const dashboard = await getAccountDashboard()
    const profile = dashboard.profile

    await updateAccountProfile({
      display_name: profile.display_name,
      phone: phoneValue,
      email: profile.email || '',
      qq: profile.qq || '',
      wechat: profile.wechat || '',
      province: provinceValue,
      city: cityValue,
      district: districtValue,
      address: profile.address || '',
    })

    requiredPhone.value = ''
    requiredProvince.value = ''
    requiredCity.value = ''
    requiredDistrict.value = ''
    requiredPhoneError.value = ''
    await loadSession()
  } catch (error) {
    requiredPhoneError.value =
      error instanceof Error ? error.message : '保存基础资料失败'
  } finally {
    requiredPhoneSubmitting.value = false
  }
}

async function submitForcedPassword() {
  forcedPasswordError.value = ''

  if (
    !forcedCurrentPassword.value ||
    !forcedNewPassword.value ||
    !forcedConfirmPassword.value
  ) {
    forcedPasswordError.value = '请完整填写当前密码和新密码。'
    return
  }

  if (forcedNewPassword.value !== forcedConfirmPassword.value) {
    forcedPasswordError.value = '两次输入的新密码不一致。'
    return
  }

  if (forcedNewPassword.value.length < 8) {
    forcedPasswordError.value = '新密码至少需要 8 位。'
    return
  }

  forcedPasswordSubmitting.value = true
  try {
    await changePassword({
      current_password: forcedCurrentPassword.value,
      new_password: forcedNewPassword.value,
      confirm_password: forcedConfirmPassword.value,
    })

    await loadSession()

    forcedCurrentPassword.value = ''
    forcedNewPassword.value = ''
    forcedConfirmPassword.value = ''
    forcedPasswordError.value = ''
  } catch (error) {
    forcedPasswordError.value =
      error instanceof Error ? error.message : '修改密码失败'
  } finally {
    forcedPasswordSubmitting.value = false
  }
}

async function signOut() {
  if (loggingOut.value) return

  loggingOut.value = true
  try {
    await logout()
  } catch {
    // Clear local state even if the server session has already expired.
  } finally {
    clearSession()
    loggingOut.value = false
    await router.replace('/login')
  }
}

watch(() => showAuthenticatedShell.value && canUseWorkInbox(actor.value?.role) && !mustChangePassword.value && !mustCompleteContact.value
  ? JSON.stringify([actor.value?.user_id, actor.value?.role, actor.value?.tenant_id, staffAccess.value]) : '',
  key => { if (key) startInbox(key); else stopInbox() }, { immediate: true })
onBeforeUnmount(stopInbox)

</script>

<template>
  <RouterView v-if="isAuthPage" :key="route.fullPath" />

  <div v-else-if="showAuthenticatedShell" class="app-shell" :class="{ 'sidebar-collapsed': sidebarCollapsed }">
    <div
      v-if="showCustomerLiveQuota"
      :class="[
        'customer-ai-time-global-topbar',
        {
          'is-expanded': customerLiveQuotaExpanded,
          'is-collapsed': !customerLiveQuotaExpanded,
          'has-active-billing-rooms': customerLiveQuotaExpanded && customerLiveQuota?.active_billing_rooms?.length,
        },
      ]"
      :title="customerLiveQuota?.reserve_time_card_count
        ? '当前可扣费总剩余 ' + formatCustomerAIQuota(customerLiveQuota.active_seconds) + '；另有 ' + customerLiveQuota.reserve_time_card_count + ' 张未启用卡'
        : '当前可扣费总剩余 ' + formatCustomerAIQuota(customerLiveQuota?.active_seconds || 0)"
    >
      <button
        class="customer-ai-time-toggle"
        type="button"
        :aria-expanded="customerLiveQuotaExpanded"
        :aria-label="customerLiveQuotaExpanded ? '折叠时长卡详情' : '展开时长卡详情'"
        :title="customerLiveQuotaExpanded ? '折叠详情' : '展开详情'"
        @click.stop="customerLiveQuotaExpanded = !customerLiveQuotaExpanded"
      >
        <span aria-hidden="true">⌄</span>
      </button>
      <span v-if="customerLiveQuotaExpanded" class="customer-ai-time-title">时长卡剩余</span>
      <strong class="customer-ai-time-value">{{ customerLiveQuota ? formatCustomerAIQuota(customerLiveQuota.active_seconds) : '读取中…' }}</strong>
      <div
        v-if="customerLiveQuotaExpanded && customerLiveQuota?.active_billing_rooms?.length"
        class="customer-ai-time-billing-rooms"
      >
        <div class="customer-ai-time-billing-heading">
          <span>正在扣费</span>
          <em>{{ customerLiveQuota.active_billing_rooms.length }} 个房间</em>
        </div>
        <div
          v-for="room in customerLiveQuota.active_billing_rooms"
          :key="room.session_id"
          class="customer-ai-time-billing-room"
        >
          <div class="customer-ai-time-billing-room-name">
            <i></i>
            <b>{{ room.room_name }}</b>
          </div>
          <span class="customer-ai-time-billing-usage">已扣 {{ formatCustomerAIUsage(room.billed_seconds) }}</span>
        </div>
      </div>
      <small v-if="customerLiveQuotaExpanded && customerLiveQuota?.reserve_time_card_count" class="customer-ai-time-reserve">
        卡包 {{ customerLiveQuota.reserve_time_card_count }} 张未启用
      </small>
    </div>
    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <button
        class="sidebar-collapse-handle"
        type="button"
        :aria-label="sidebarCollapsed ? '展开全局导航' : '折叠全局导航'"
        :title="sidebarCollapsed ? '展开全局导航' : '折叠全局导航'"
        @click="toggleSidebarCollapsed"
      >{{ sidebarCollapsed ? '›' : '‹' }}</button>
      <div class="brand">
        <div class="brand-mark">{{ isInternalStaff ? '蓝' : '蓝' }}</div>
        <div>
          <strong>{{ isInternalStaff ? '小蓝搭子管理系统' : '小蓝搭子' }}</strong>
          <span>{{ isInternalStaff ? 'BANBO AI SYSTEM' : 'BANBO AI SYSTEM' }}</span>
        </div>
      </div>

      <nav class="nav-list">
        <section
          v-for="section in navSections"
          :key="section.label"
          class="nav-section-card"
          :class="[
            navSectionTone(section),
            { collapsed: sectionCollapsed(section), 'has-active': sectionHasActive(section) },
          ]"
        >
          <button
            class="nav-section-heading"
            type="button"
            :aria-expanded="!sectionCollapsed(section)"
            @click="openNavSection(section)"
          >
            <span class="nav-section-leading">
              <span class="nav-section-group-icon">{{ navSectionIcon(section) }}</span>
              <span class="nav-section-title-wrap">
                <span class="nav-section-label">{{ section.label }}</span>
              </span>
            </span>
            <span class="nav-section-heading-actions">
              <span class="nav-section-count">{{ section.items.length }}</span>
              <span class="nav-section-chevron">⌄</span>
            </span>
          </button>

          <div v-show="!sectionCollapsed(section)" class="nav-section-items">
            <RouterLink
              v-for="item in section.items"
              :key="item.to + item.label"
              class="nav-item"
              :class="{ active: navActive(item) }"
              :to="item.to"
            >
              <span class="nav-icon-shell">
                <span class="nav-icon">{{ item.icon }}</span>
              </span>
              <span class="nav-item-label">{{ item.label }}</span>
              <TodoBadge :to="item.to" />
              <span class="nav-item-arrow">›</span>
            </RouterLink>
          </div>
        </section>
      </nav>

      <RouterLink v-if="canUseWorkInbox(actor?.role)" to="/work/inbox" class="nav-item work-inbox-entry"><span class="nav-icon">办</span><span class="nav-item-label">我的待办</span><TodoBadge to="/work/inbox" /></RouterLink>

      <section class="sidebar-personal-section">
        <RouterLink
          class="sidebar-personal-button"
          :class="{
            active:
              route.name === 'personal-center' ||
              route.name === 'account' ||
              route.name === 'settings',
          }"
          to="/personal"
        >
          <span class="sidebar-personal-button-icon">
            <img
              v-if="actor?.avatar_url"
              :src="actor.avatar_url"
              alt="个人头像"
            />
            <span v-else>我</span>
          </span>
          <span class="sidebar-personal-button-copy">
            <strong>个人中心</strong>
            <small>账户、安全与个人偏好</small>
          </span>
          <span class="sidebar-personal-button-arrow">›</span>
        </RouterLink>
      </section>

      <div class="sidebar-footer">
        <div class="service-dot-row">
          <span class="service-dot"></span>
          <span>管理服务已连接</span>
        </div>
        <span class="version">V1 本地开发版</span>
      </div>
    </aside>

    <main class="main-area" :class="{'work-inbox-main': route.name === 'work-inbox'}">
      <header class="topbar global-topbar">
        <div id="app-global-breadcrumbs" class="global-topbar-navigation">
          <div class="topbar-fallback">
            <p class="eyebrow">{{ topbarEyebrow }}</p>
            <h1>{{ topbarTitle }}</h1>
          </div>
        </div>

        <div class="topbar-actions">
          <div id="app-global-status" class="global-topbar-status"></div>
          <CustomerMembershipCenter v-if="actor?.role === 'customer'" />
          <RouterLink class="account-chip account-chip-link" to="/personal">
            <div class="avatar">
              <img
                v-if="actor?.avatar_url"
                :src="actor.avatar_url"
                alt="账户头像"
              />
              <span v-else>{{ actor?.display_name?.slice(0, 1) || '账' }}</span>
            </div>
            <div>
              <strong>{{ actor?.display_name || '账户' }}</strong>
              <span>{{ accountRoleLabel }} · {{ actor?.username || '' }}</span>
            </div>
          </RouterLink>

          <button
            class="logout-button"
            type="button"
            :disabled="loggingOut"
            @click="signOut"
          >
            {{ loggingOut ? '退出中...' : '退出' }}
          </button>
        </div>
      </header>

      <div id="app-global-switches" class="global-topbar-switches"></div>

      <div
        v-if="showCoreRuntimeNotice"
        class="core-runtime-banner"
        :class="'phase-' + coreRuntime.phase"
        role="status"
        aria-live="polite"
      >
        <span class="core-runtime-banner-dot" aria-hidden="true"></span>
        <div>
          <strong>{{ coreRuntimeNoticeTitle }}</strong>
          <span>{{ coreRuntimeNoticeText }}</span>
        </div>
        <small v-if="coreRuntime.bootId">Boot {{ coreRuntime.bootId }}</small>
      </div>

      <section class="page-content">
        <RouterView :key="route.fullPath" />
      </section>
      <SystemFooter />
    </main>

    <nav class="mobile-nav" aria-label="移动端导航">
      <RouterLink
        v-for="item in mobileNavItems"
        :key="'mobile-' + item.to + item.label"
        class="mobile-nav-item"
        :class="{ active: navActive(item) }"
        :to="item.to"
      >
        <span class="mobile-nav-icon">{{ item.icon }}</span>
        <TodoBadge :to="item.to" />
        <span>{{ item.label }}</span>
      </RouterLink>
    </nav>
  </div>

  <GlobalFeedback />

  <SystemAgentLayer v-if="!isAuthPage && !mustChangePassword && !mustCompleteContact" />

  <Teleport to="body">
    <div
      v-if="mustChangePassword"
      class="forced-password-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="forced-password-title"
    >
      <form
        class="forced-password-card"
        @submit.prevent="submitForcedPassword"
      >
        <div class="forced-password-brand">
          <div class="brand-mark">{{ isInternalStaff ? '蓝' : '蓝' }}</div>
          <div>
            <strong>{{ isInternalStaff ? '小蓝搭子管理系统' : '小蓝搭子' }}</strong>
            <span>ACCOUNT SECURITY</span>
          </div>
        </div>

        <div class="forced-password-heading">
          <span class="section-kicker">PASSWORD REQUIRED</span>
          <h2 id="forced-password-title">首次登录请修改密码</h2>
          <p>
            当前账号使用的是系统生成的初始密码。修改完成前不能继续使用其他功能。
          </p>
        </div>

        <div class="forced-password-fields">
          <label>
            <span>当前初始密码</span>
            <PasswordInput
              v-model="forcedCurrentPassword"
              autocomplete="current-password"
              placeholder="输入当前初始密码"
              required
            />
          </label>

          <label>
            <span>新密码</span>
            <PasswordInput
              v-model="forcedNewPassword"
              autocomplete="new-password"
              placeholder="至少 8 位"
              required
            />
          </label>

          <label>
            <span>确认新密码</span>
            <PasswordInput
              v-model="forcedConfirmPassword"
              autocomplete="new-password"
              placeholder="再次输入新密码"
              required
            />
          </label>
        </div>

        <p v-if="forcedPasswordError" class="forced-password-error">
          {{ forcedPasswordError }}
        </p>

        <button
          class="forced-password-submit"
          type="submit"
          :disabled="forcedPasswordSubmitting"
        >
          {{ forcedPasswordSubmitting ? '正在修改...' : '修改密码并继续' }}
        </button>

        <p class="forced-password-note">
          修改成功后，其他登录会话会自动失效。
        </p>
      </form>
    </div>
  </Teleport>

  <Teleport to="body">
    <div
      v-if="mustCompleteContact && !mustChangePassword"
      class="forced-password-backdrop required-contact-backdrop"
      role="dialog"
      aria-modal="true"
      aria-labelledby="required-contact-title"
    >
      <form
        class="forced-password-card required-contact-card"
        @submit.prevent="submitRequiredPhone"
      >
        <div class="forced-password-brand">
          <div class="brand-mark">{{ isInternalStaff ? '蓝' : '蓝' }}</div>
          <div>
            <strong>{{ isInternalStaff ? '小蓝搭子管理系统' : '小蓝搭子' }}</strong>
            <span>ACCOUNT PROFILE</span>
          </div>
        </div>

        <div class="forced-password-heading">
          <span class="section-kicker">PROFILE REQUIRED</span>
          <h2 id="required-contact-title">首次进入，请补全地区资料</h2>
          <p>
            注册已经完成。首次进入系统请补全省、市、区/县，并确认联系电话，保存后即可继续使用。
          </p>
        </div>

        <div class="forced-password-fields">
          <label>
            <span>联系电话 <em class="required-mark">*</em></span>
            <input
              v-model="requiredPhone"
              type="text"
              autocomplete="tel"
              placeholder="请输入联系电话"
              required
            />
          </label>

          <RegionSelect
            v-model:province="requiredProvince"
            v-model:city="requiredCity"
            v-model:district="requiredDistrict"
          />
        </div>

        <p v-if="requiredPhoneError" class="forced-password-error">
          {{ requiredPhoneError }}
        </p>

        <button
          class="forced-password-submit"
          type="submit"
          :disabled="requiredPhoneSubmitting"
        >
          {{ requiredPhoneSubmitting ? '正在保存...' : '保存资料并继续' }}
        </button>

        <p class="forced-password-note">
          邮箱、QQ、微信和详细地址可在“账户管理”中继续完善。
        </p>
      </form>
    </div>
  </Teleport>
</template>
<style scoped>
/* One shared, in-flow pinned header for customers and every staff department.
   Sticky preserves its actual height (including wrapped breadcrumbs) without
   scroll listeners, duplicated spacers, or role-specific fixed offsets. */
.main-area { min-width: 0; }
.main-area > .global-topbar {
  position: sticky;
  top: 0;
  z-index: 80;
  flex-shrink: 0;
  background: #fbfcff;
}
/* The sidebar is hidden at this breakpoint; don't retain its desktop gutter. */
@media (max-width: 680px) {
  .main-area { width: 100%; margin-left: 0; min-width: 0; }
  .main-area > .global-topbar { flex-wrap: wrap; gap: 10px; }
  .global-topbar-navigation { flex-basis: 100%; }
  .topbar-actions { max-width: 100%; margin-left: auto; flex-wrap: wrap; }
}
</style>
