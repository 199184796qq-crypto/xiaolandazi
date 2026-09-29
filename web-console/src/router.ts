import { createRouter, createWebHistory } from 'vue-router'
import WorkInboxView from './views/WorkInboxView.vue'
import { canOpenWorkInboxRoute, canUseWorkInbox } from './workInboxRoutes'
import LoginView from './views/LoginView.vue'
import RegisterView from './views/RegisterView.vue'
import RoomsView from './views/RoomsView.vue'
import PlatformOverviewView from './views/PlatformOverviewView.vue'
import AgentOverviewView from './views/AgentOverviewView.vue'
import RoomDetailView from './views/RoomDetailView.vue'
import CustomerManagementView from './views/CustomerManagementView.vue'
import AgentsView from './views/AgentsView.vue'
import AgentCustomersView from './views/AgentCustomersView.vue'
import InvitationsView from './views/InvitationsView.vue'
import ResourcesView from './views/ResourcesView.vue'
import SalesManagementView from './views/SalesManagementView.vue'
import SalesCustomersView from './views/SalesCustomersView.vue'
import SalesWorkspaceView from './views/SalesWorkspaceView.vue'
import SalesFollowupsView from './views/SalesFollowupsView.vue'
import SalesCatalogView from './views/SalesCatalogView.vue'
import StaffOrganizationView from './views/StaffOrganizationView.vue'
import StaffPermissionCenterView from './views/StaffPermissionCenterView.vue'
import StaffFinanceView from './views/StaffFinanceView.vue'
import CommercialMembershipsView from './views/CommercialMembershipsView.vue'
import FinanceView from './views/FinanceView.vue'
import ShopView from './views/ShopView.vue'
import AccountView from './views/AccountView.vue'
import SettingsView from './views/SettingsView.vue'
import PersonalCenterView from './views/PersonalCenterView.vue'
import DomainHubView from './views/DomainHubView.vue'
import AgentLevelsView from './views/AgentLevelsView.vue'
import AgentContractsView from './views/AgentContractsView.vue'
import CommercialTimeCardsView from './views/CommercialTimeCardsView.vue'
import CommercialDeviceProductsView from './views/CommercialDeviceProductsView.vue'
import MarketingDesignView from './views/MarketingDesignView.vue'
import MarketingOperationsView from './views/MarketingOperationsView.vue'
import LiveOperationsView from './views/LiveOperationsView.vue'
import LiveRoomQuotaView from './views/LiveRoomQuotaView.vue'
import LiveStrategyEntryView from './views/LiveStrategyEntryView.vue'
import LiveAnalysisSettingsView from './views/LiveAnalysisSettingsView.vue'
import DeviceBindingView from './views/DeviceBindingView.vue'
import AuditLogView from './views/AuditLogView.vue'
import FinanceTraceView from './views/FinanceTraceView.vue'
import InventoryLifecycleView from './views/InventoryLifecycleView.vue'
import LogisticsView from './views/LogisticsView.vue'
import SalesPerformanceView from './views/SalesPerformanceView.vue'
import AgentExitView from './views/AgentExitView.vue'
import IncentiveProgramsView from './views/IncentiveProgramsView.vue'
import SettlementBatchesView from './views/SettlementBatchesView.vue'
import OperatingFinanceView from './views/OperatingFinanceView.vue'
import AfterSalesPortalView from './views/AfterSalesPortalView.vue'
import SystemSettingsView from './views/SystemSettingsView.vue'
import { loadSession, session } from './session'

function isInternalRole(role?: string) {
  return role === 'platform_admin' || role === 'staff' || role === 'sales_staff'
}

function hasAuthenticatedActor() {
  return Boolean(session.bootstrap?.actor?.user_id && session.bootstrap?.actor?.role)
}

function hasStaffPermission(code: string) {
  const bootstrap = session.bootstrap
  if (bootstrap?.actor.role === 'platform_admin') return true
  const access = bootstrap?.staff_access
  if (!access) return false
  return access.is_super_admin || access.permissions.includes(code)
}

function defaultAuthenticatedRoute() {
  const role = session.bootstrap?.actor.role
  const primaryGroupCode = session.bootstrap?.staff_access?.primary_group_code

  if (role === 'platform_admin') {
    return { name: 'platform-overview' }
  }
  if (role === 'agent_admin') {
    return { name: 'agent-overview' }
  }
  if (role === 'sales_staff') {
    return { name: 'sales-workspace' }
  }
  if (role === 'staff') {
    if (primaryGroupCode === 'management') {
      return { name: 'platform-overview' }
    }
    if (primaryGroupCode === 'finance') {
      return { name: 'staff-finance-hub' }
    }
    if (primaryGroupCode === 'sales') {
      return { name: 'sales-hub' }
    }
    if (primaryGroupCode === 'live_operations') {
      return { name: 'live-hub' }
    }
    if (primaryGroupCode === 'warehouse_after_sales') {
      return { name: 'resource-inventory' }
    }

    if (hasStaffPermission('system.architecture.view')) {
      return { name: 'platform-overview' }
    }
    if (hasStaffPermission('finance.dashboard.view')) {
      return { name: 'staff-finance-hub' }
    }
    if (
      hasStaffPermission('liveops.configure') ||
      hasStaffPermission('liveops.view_all') ||
      hasStaffPermission('liveops.room_quota.view')
    ) {
      return { name: 'live-hub' }
    }
    if (
      hasStaffPermission('inventory.view') ||
      hasStaffPermission('logistics.view') ||
      hasStaffPermission('inventory.after_sales.view')
    ) {
      return { name: 'resource-inventory' }
    }
    return { name: 'staff-hub' }
  }
  return { name: 'rooms' }
}

const departmentRouteOwners: Record<string, string> = {
  'staff-finance-invitations': 'finance',
  'staff-finance-receipts': 'finance',
  'staff-finance-customer-money': 'finance',
  'sales-receipts': 'sales',
  'sales-customer-money': 'sales',
  'sales-support': 'sales',
  'operations-support': 'live_operations',
  'platform-overview': 'management',

  'staff-finance-hub': 'finance',
  'staff-finance-accounts': 'finance',
  'staff-finance-approvals': 'finance',
  'staff-finance-ledger': 'finance',
  'staff-finance-history': 'finance',
  'staff-finance-trace': 'finance',
  'staff-finance-settlements': 'finance',
  'staff-finance-operating': 'finance',
  'commercial-settlement': 'finance',

  'customers-hub': 'sales',
  'customers-list': 'sales',
  'sales-hub': 'sales',
  'sales-team': 'sales',
  'sales-performance': 'sales',
  'sales-workspace': 'sales',
  'sales-leads': 'sales',
  'sales-handovers': 'sales',
  'sales-followups': 'sales',
  'sales-catalog': 'sales',
  'sales-my-performance': 'sales',
  'sales-customers': 'sales',
  invitations: 'sales',

  'live-hub': 'live_operations',
  'live-customer-handoffs': 'live_operations',
  'live-activity-marketing': 'live_operations',
  'commercial-ai-time': 'live_operations',
  'live-room-quotas': 'live_operations',
  'live-monitor': 'live_operations',
  'live-events': 'live_operations',
  'live-strategy': 'live_operations',
  'live-analysis-settings': 'live_operations',
  'live-devices': 'live_operations',
  rooms: 'live_operations',
  'rooms-list': 'live_operations',
  'room-detail': 'live_operations',
  'commercial-hub': 'live_operations',
  'commercial-membership-plans': 'live_operations',
  'commercial-membership-simulator': 'live_operations',
  'commercial-time-cards': 'live_operations',
  'commercial-device-products': 'live_operations',
  'commercial-marketing': 'live_operations',
  'commercial-marketing-tools': 'live_operations',
  'commercial-marketing-channels': 'live_operations',
  'commercial-marketing-analytics': 'live_operations',
  'commercial-referrals': 'live_operations',

  'resources-hub': 'warehouse_after_sales',
  'resource-devices': 'warehouse_after_sales',
  'resource-inventory': 'warehouse_after_sales',
  'resource-device-products': 'warehouse_after_sales',
  'resource-logistics': 'warehouse_after_sales',
  'staff-after-sales': 'warehouse_after_sales',
}

function isCrossDepartmentRoute(routeName: string, primaryGroupCode?: string) {
  const owner = departmentRouteOwners[routeName]
  return Boolean(owner && primaryGroupCode && owner !== primaryGroupCode)
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/work/inbox', name: 'work-inbox', component: WorkInboxView },
    {path:'/staff/finance/invitations',name:'staff-finance-invitations',component:()=>import('./views/FinanceInvitationsView.vue'),meta:{internalStaffOnly:true,staffPermission:'finance.dashboard.view'}},
    {path:'/staff/finance/receipts',name:'staff-finance-receipts',component:()=>import('./views/CustomerReceiptsView.vue'),meta:{staffPermission:'finance.dashboard.view'}},
    {path:'/staff/finance/customers/:tenantID/money',name:'staff-finance-customer-money',component:()=>import('./views/CustomerMoneyView.vue'),meta:{staffPermission:'finance.dashboard.view'}},
    {path:'/sales/receipts',name:'sales-receipts',component:()=>import('./views/CustomerReceiptsView.vue'),meta:{salesOnly:true}},
    {path:'/sales/customers/:tenantID/money',name:'sales-customer-money',component:()=>import('./views/CustomerMoneyView.vue'),meta:{salesOnly:true}},
    {path:'/sales/support',name:'sales-support',component:()=>import('./views/ServiceTicketsView.vue'),meta:{salesOnly:true}},
    {path:'/finance/receipts',name:'customer-receipts',component:()=>import('./views/CustomerReceiptsView.vue'),meta:{customerOnly:true}},
    {path:'/finance/records',name:'customer-money',component:()=>import('./views/CustomerMoneyView.vue'),meta:{customerOnly:true}},
    {path:'/support',name:'customer-support',component:()=>import('./views/ServiceTicketsView.vue'),meta:{customerOnly:true}},
    {path:'/operations/support',name:'operations-support',component:()=>import('./views/LiveSupportView.vue'),meta:{staffPermission:'liveops.configure'}},
    {
      path: '/login',
      name: 'login',
      component: LoginView,
      meta: { public: true },
    },
    {
      path: '/register',
      name: 'register',
      component: RegisterView,
      meta: { public: true },
    },
    {
      path: '/overview',
      name: 'platform-overview',
      component: PlatformOverviewView,
      meta: { staffPermission: 'system.architecture.view' },
    },
    {
      path: '/system/settings',
      name: 'system-settings',
      component: SystemSettingsView,
      meta: { platformAdminOnly: true },
    },
    {
      path: '/system/settings/agent-routing',
      name: 'system-agent-routing',
      component: () => import('./views/AgentRoutingSettingsView.vue'),
      meta: { staffPermission: 'system.settings.agent_routing.manage' },
    },
    {
      path: '/staff',
      name: 'staff-hub',
      component: DomainHubView,
      props: { hub: 'staff' },
      meta: {
        staffPermissionsAny: [
          'system.architecture.view',
          'staff.group.view',
          'staff.employee.view',
          'staff.role.view',
          'staff.role.manage',
        ],
      },
    },
    {
      path: '/staff/groups',
      name: 'staff-groups',
      component: StaffOrganizationView,
      props: { initialTab: 'employees', groupsOnly: true },
      meta: { staffPermission: 'staff.group.view' },
    },
    {
      path: '/staff/employees',
      name: 'staff-employees',
      component: StaffOrganizationView,
      props: { initialTab: 'employees' },
      meta: { staffPermission: 'staff.employee.view' },
    },
    {
      path: '/staff/roles',
      name: 'staff-roles',
      component: StaffOrganizationView,
      props: { initialTab: 'roles' },
      meta: { staffPermission: 'staff.role.view' },
    },
    {
      path: '/staff/permissions',
      name: 'staff-permissions',
      component: StaffPermissionCenterView,
      meta: { staffPermission: 'staff.role.manage' },
    },
    {
      path: '/staff/approvals',
      name: 'staff-approvals',
      component: StaffOrganizationView,
      props: { initialTab: 'approvals' },
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/audit',
      name: 'staff-audit',
      component: AuditLogView,
      meta: { staffPermission: 'audit.view' },
    },
    {
      path: '/staff/finance',
      name: 'staff-finance-hub',
      component: DomainHubView,
      props: { hub: 'finance' },
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/accounts',
      name: 'staff-finance-accounts',
      component: StaffFinanceView,
      props: { focus: 'accounts' },
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/approvals',
      name: 'staff-finance-approvals',
      component: StaffFinanceView,
      props: { focus: 'approvals' },
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/ledger',
      name: 'staff-finance-ledger',
      component: StaffFinanceView,
      props: { focus: 'ledger' },
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/history',
      name: 'staff-finance-history',
      component: StaffFinanceView,
      props: { focus: 'history' },
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/trace',
      name: 'staff-finance-trace',
      component: FinanceTraceView,
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/settlements',
      name: 'staff-finance-settlements',
      component: SettlementBatchesView,
      meta: { staffPermission: 'finance.dashboard.view' },
    },
    {
      path: '/staff/finance/ai-time',
      name: 'staff-finance-ai-time',
      redirect: '/staff/finance/approvals?focus=ai-time',
    },
    {
      path: '/staff/finance/operating',
      name: 'staff-finance-operating',
      component: OperatingFinanceView,
      meta: { staffPermission: 'finance.operating.view' },
    },
    {
      path: '/agent/overview',
      name: 'agent-overview',
      component: AgentOverviewView,
      meta: { agentOnly: true },
    },
    {
      path: '/operations/live',
      name: 'live-hub',
      component: DomainHubView,
      props: { hub: 'live' },
      meta: { staffPermission: 'liveops.configure' },
    },
    {
      path: '/operations/live/marketing',
      name: 'live-activity-marketing',
      component: DomainHubView,
      props: { hub: 'activityMarketing' },
      meta: {
        staffPermissionsAny: [
          'commercial.marketing.view',
          'commercial.membership.view',
          'commercial.ai_time.view',
          'commercial.time_card.view',
          'commercial.device.view',
          'invitations.view_all',
        ],
      },
    },
    {
      path: '/operations/live/marketing/ai-time',
      name: 'commercial-ai-time',
      component: ResourcesView,
      meta: { staffPermission: 'commercial.ai_time.view' },
    },
    {
      path: '/operations/live/room-quotas',
      name: 'live-room-quotas',
      component: LiveRoomQuotaView,
      meta: { staffPermission: 'liveops.room_quota.view' },
    },
    {
      path: '/operations/live/monitor',
      name: 'live-monitor',
      component: LiveOperationsView,
      props: { focus: 'monitor' },
      meta: { staffPermissionsAny: ['liveops.view_all', 'liveops.configure'] },
    },
    {
      path: '/operations/live/events',
      name: 'live-events',
      component: LiveOperationsView,
      props: { focus: 'events' },
      meta: { staffPermissionsAny: ['liveops.view_all', 'liveops.configure'] },
    },
    {
      path: '/operations/live/strategy',
      name: 'live-strategy',
      component: LiveStrategyEntryView,
    },
    {
      path: '/operations/live/analysis-settings',
      name: 'live-analysis-settings',
      component: LiveAnalysisSettingsView,
      meta: { staffPermission: 'liveanalysis.view' },
    },
    {
      path: '/operations/live/devices',
      name: 'live-devices',
      component: DeviceBindingView,
    },
    {
      path: '/',
      name: 'rooms',
      component: DomainHubView,
      props: { hub: 'live' },
    },
    {
      path: '/rooms/list',
      name: 'rooms-list',
      component: RoomsView,
    },
    {
      path: '/rooms/:id',
      name: 'room-detail',
      component: RoomDetailView,
    },
    {
      path: '/customers',
      name: 'customers-hub',
      component: DomainHubView,
      props: { hub: 'customers' },
      meta: { staffPermission: 'customer.view_all' },
    },
    {
      path: '/customers/list',
      name: 'customers-list',
      component: CustomerManagementView,
      meta: { staffPermission: 'customer.view_all' },
    },
    {
      path: '/agents',
      name: 'agents-hub',
      component: DomainHubView,
      props: { hub: 'agents' },
      meta: { staffPermission: 'agent.view_all' },
    },
    {
      path: '/agents/list',
      name: 'agents-list',
      component: AgentsView,
      meta: { staffPermission: 'agent.view_all' },
    },
    {
      path: '/agents/levels',
      name: 'agent-levels',
      component: AgentLevelsView,
      meta: { staffPermission: 'agent.view_all' },
    },
    {
      path: '/agents/contracts',
      name: 'agent-contracts',
      component: AgentContractsView,
      meta: { staffPermission: 'agent.view_all' },
    },
    {
      path: '/agents/exit',
      name: 'agent-exit',
      component: AgentExitView,
      meta: { staffPermission: 'agent.view_all' },
    },
    {
      path: '/sales',
      name: 'sales-hub',
      component: DomainHubView,
      props: { hub: 'sales' },
      meta: { staffPermission: 'sales.view_all' },
    },
    {
      path: '/sales/team',
      name: 'sales-team',
      component: SalesManagementView,
      meta: { staffPermission: 'sales.view_all' },
    },
    {
      path: '/sales/performance',
      name: 'sales-performance',
      component: SalesPerformanceView,
      meta: { staffPermission: 'sales.view_all' },
    },
    {
      path: '/sales/leads',
      name: 'sales-leads',
      component: () => import('./views/SalesLeadsView.vue'),
      meta: { salesOnly: true },
    },
    {
      path: '/sales/handovers',
      name: 'sales-handovers',
      component: () => import('./views/SalesHandoverView.vue'),
      meta: { staffPermission: 'sales.assignment.manage' },
    },
    {
      path: '/operations/live/customer-handoffs',
      name: 'live-customer-handoffs',
      component: () => import('./views/CustomerHandoffsView.vue'),
      meta: { staffPermissionsAny: ['liveops.configure', 'liveops.ticket.manage'] },
    },
    {
      path: '/sales/workspace',
      name: 'sales-workspace',
      component: SalesWorkspaceView,
      meta: { salesOnly: true },
    },
    {
      path: '/sales/customers',
      name: 'sales-customers',
      component: SalesCustomersView,
      meta: { salesOnly: true },
    },
    {
      path: '/sales/followups',
      name: 'sales-followups',
      component: SalesFollowupsView,
      meta: { salesOnly: true },
    },
    {
      path: '/sales/catalog',
      name: 'sales-catalog',
      component: SalesCatalogView,
      meta: { salesOnly: true },
    },
    {
      path: '/sales/my-performance',
      name: 'sales-my-performance',
      component: SalesPerformanceView,
      meta: { salesOnly: true },
    },
    {
      path: '/agent/customers',
      name: 'agent-customers',
      component: AgentCustomersView,
      meta: { agentOnly: true },
    },
    {
      path: '/commercial/memberships',
      name: 'commercial-hub',
      redirect: '/operations/live/marketing',
      meta: { staffPermission: 'commercial.membership.view' },
    },
    {
      path: '/commercial/memberships/plans',
      name: 'commercial-membership-plans',
      component: CommercialMembershipsView,
      props: { focus: 'plans' },
      meta: { staffPermission: 'commercial.membership.view' },
    },
    {
      path: '/commercial/memberships/simulator',
      name: 'commercial-membership-simulator',
      component: CommercialMembershipsView,
      props: { focus: 'simulator' },
      meta: { staffPermission: 'commercial.membership.view' },
    },
    {
      path: '/commercial/time-cards',
      name: 'commercial-time-cards',
      component: CommercialTimeCardsView,
      meta: { staffPermission: 'commercial.time_card.view' },
    },
    {
      path: '/commercial/device-products',
      name: 'commercial-device-products',
      component: CommercialDeviceProductsView,
      meta: { staffPermission: 'commercial.device.view' },
    },
    {
      path: '/commercial/marketing',
      name: 'commercial-marketing',
      component: MarketingDesignView,
      meta: { staffPermission: 'commercial.marketing.view' },
    },
    {
      path: '/commercial/marketing/tools',
      name: 'commercial-marketing-tools',
      component: MarketingOperationsView,
      props: { mode: 'tools' },
      meta: { staffPermission: 'commercial.marketing.view' },
    },
    {
      path: '/commercial/marketing/channels',
      name: 'commercial-marketing-channels',
      component: MarketingOperationsView,
      props: { mode: 'channels' },
      meta: { staffPermission: 'invitations.view_all' },
    },
    {
      path: '/commercial/marketing/analytics',
      name: 'commercial-marketing-analytics',
      component: MarketingOperationsView,
      props: { mode: 'analytics' },
      meta: { staffPermission: 'commercial.marketing.view' },
    },
    {
      path: '/commercial/referrals',
      name: 'commercial-referrals',
      component: IncentiveProgramsView,
      props: { mode: 'referral' },
      meta: { staffPermission: 'commercial.referral.view' },
    },
    {
      path: '/commercial/settlement',
      name: 'commercial-settlement',
      component: IncentiveProgramsView,
      props: { mode: 'settlement' },
      meta: { staffPermission: 'finance.settlement_rules.view' },
    },
    {
      path: '/shop',
      name: 'shop',
      component: ShopView,
      meta: { customerOnly: true },
    },
    {
      path: '/finance',
      name: 'finance',
      component: FinanceView,
      meta: { customerOnly: true },
    },
    {
      path: '/invitations',
      name: 'invitations',
      component: InvitationsView,
    },
    {
      path: '/after-sales',
      name: 'after-sales-portal',
      component: AfterSalesPortalView,
      meta: { afterSalesPortal: true },
    },
    {
      path: '/resources',
      name: 'resources-hub',
      component: DomainHubView,
      props: { hub: 'resources' },
      meta: { staffPermission: 'inventory.view' },
    },
    {
      path: '/resources/workspace',
      name: 'resources-workspace',
      component: ResourcesView,
      meta: { resourceAccess: true },
    },
    {
      path: '/resources/devices',
      name: 'resource-devices',
      component: InventoryLifecycleView,
      props: { focus: 'devices' },
      meta: { staffPermission: 'inventory.view' },
    },
    {
      path: '/resources/inventory',
      name: 'resource-inventory',
      component: InventoryLifecycleView,
      props: { focus: 'inventory' },
      meta: { staffPermission: 'inventory.view' },
    },
    {
      path: '/resources/device-products',
      name: 'resource-device-products',
      component: CommercialDeviceProductsView,
      props: {
        navigationContext: 'resources',
        pageTitle: '设备商品资料',
      },
      meta: { staffPermission: 'commercial.device.view' },
    },
    {
      path: '/staff/after-sales',
      name: 'staff-after-sales',
      component: InventoryLifecycleView,
      props: { focus: 'after-sales' },
      meta: { staffPermission: 'inventory.after_sales.view' },
    },
    {
      path: '/resources/after-sales',
      redirect: '/staff/after-sales',
    },
    {
      path: '/resources/logistics',
      name: 'resource-logistics',
      component: LogisticsView,
      meta: { staffPermission: 'logistics.view' },
    },
    {
      path: '/personal',
      name: 'personal-center',
      component: PersonalCenterView,
    },
    {
      path: '/account',
      name: 'account',
      component: AccountView,
    },
    {
      path: '/settings',
      name: 'settings',
      component: SettingsView,
    },
  ],
})

router.beforeEach(async (to) => {
  if (!session.initialized) {
    try {
      await loadSession()
    } catch {
      // An unauthenticated response is expected before login.
    }
  }

  if (to.meta.public === true) {
    if (hasAuthenticatedActor()) {
      return defaultAuthenticatedRoute()
    }
    return true
  }

  if (!hasAuthenticatedActor()) {
    return {
      name: 'login',
      query: {
        redirect: to.fullPath,
        portal:
          to.meta.platformAdminOnly === true ||
          to.meta.internalStaffOnly === true ||
          typeof to.meta.staffPermission === 'string' ||
          Array.isArray(to.meta.staffPermissionsAny)
            ? 'internal'
            : 'user',
      },
    }
  }

  const bootstrap = session.bootstrap!
  const role = bootstrap.actor.role
  if (to.name === 'work-inbox' && !canUseWorkInbox(role)) {
    return { path: '/personal', replace: true }
  }

  const primaryGroupCode = bootstrap.staff_access?.primary_group_code

  if (
    role !== 'platform_admin' &&
    (role === 'staff' || role === 'sales_staff') &&
    isCrossDepartmentRoute(String(to.name || ''), primaryGroupCode) &&
    !canOpenWorkInboxRoute(to.path, role, hasStaffPermission) &&
    !(String(to.name || '').startsWith('staff-finance-') && hasStaffPermission('finance.dashboard.view'))
  ) {
    return defaultAuthenticatedRoute()
  }

  if (to.meta.platformAdminOnly === true && role !== 'platform_admin') {
    return defaultAuthenticatedRoute()
  }

  if (
    to.meta.internalStaffOnly === true &&
    !isInternalRole(role)
  ) {
    return defaultAuthenticatedRoute()
  }

  if (
    role === 'staff' &&
    to.name === 'staff-hub' &&
    !hasStaffPermission('system.architecture.view') &&
    !hasStaffPermission('staff.group.view') &&
    !hasStaffPermission('staff.employee.view') &&
    !hasStaffPermission('staff.role.view')
  ) {
    const target = defaultAuthenticatedRoute()
    if (target.name !== 'staff-hub') {
      return target
    }
  }

  if (
    role === 'staff' &&
    primaryGroupCode === 'warehouse_after_sales' &&
    to.name === 'resources-hub'
  ) {
    return { name: 'resource-inventory' }
  }

  if (
    (role === 'staff' || role === 'platform_admin') &&
    to.name === 'resources-workspace' &&
    hasStaffPermission('commercial.ai_time.view')
  ) {
    return { name: 'commercial-ai-time' }
  }

  const staffPermission = to.meta.staffPermission
  if (
    typeof staffPermission === 'string' &&
    !hasStaffPermission(staffPermission)
  ) {
    return defaultAuthenticatedRoute()
  }

  const staffPermissionsAny = to.meta.staffPermissionsAny
  if (
    Array.isArray(staffPermissionsAny) &&
    staffPermissionsAny.length > 0 &&
    !staffPermissionsAny.some(
      (permission) => typeof permission === 'string' && hasStaffPermission(permission),
    )
  ) {
    return defaultAuthenticatedRoute()
  }

  if (
    to.meta.customerOnly === true &&
    role !== 'customer'
  ) {
    return defaultAuthenticatedRoute()
  }

  if (
    to.meta.agentOnly === true &&
    role !== 'agent_admin'
  ) {
    return defaultAuthenticatedRoute()
  }

  if (
    to.meta.afterSalesPortal === true &&
    role !== 'customer' &&
    role !== 'agent_admin'
  ) {
    return defaultAuthenticatedRoute()
  }

  if (
    to.meta.salesOnly === true &&
    role !== 'sales_staff'
  ) {
    return defaultAuthenticatedRoute()
  }

  if (to.meta.resourceAccess === true) {
    const allowed =
      role === 'agent_admin' ||
      role === 'customer' ||
      hasStaffPermission('finance.resource.view') ||
      hasStaffPermission('finance.resource.adjust')

    if (!allowed) {
      return defaultAuthenticatedRoute()
    }
  }

  if (
    role === 'agent_admin' &&
    (to.name === 'rooms' || to.name === 'rooms-list' || to.name === 'room-detail')
  ) {
    return { name: 'agent-overview' }
  }

  if (to.name === 'rooms' || to.name === 'rooms-list' || to.name === 'room-detail') {
    const canAccessRooms =
      role === 'customer' ||
      role === 'platform_admin' ||
      (role === 'staff' &&
        (
          hasStaffPermission('system.architecture.view') ||
          hasStaffPermission('liveops.view_all') ||
          hasStaffPermission('liveops.configure')
        ))
    if (!canAccessRooms) {
      return defaultAuthenticatedRoute()
    }
  }

  if (role === 'staff' && to.name === 'invitations') {
    if (!hasStaffPermission('invitations.view_all')) {
      return defaultAuthenticatedRoute()
    }
  }

  return true
})
