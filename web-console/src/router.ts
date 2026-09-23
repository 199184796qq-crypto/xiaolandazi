import { createRouter, createWebHistory } from 'vue-router'
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
import StaffOrganizationView from './views/StaffOrganizationView.vue'
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
import LiveOperationsView from './views/LiveOperationsView.vue'
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
import { loadSession, session } from './session'

function isInternalRole(role?: string) {
  return role === 'platform_admin' || role === 'staff' || role === 'sales_staff'
}

function hasStaffPermission(code: string) {
  const access = session.bootstrap?.staff_access
  if (!access) return false
  return access.is_super_admin || access.permissions.includes(code)
}

function defaultAuthenticatedRoute() {
  const role = session.bootstrap?.actor.role

  if (role === 'platform_admin') {
    return { name: 'platform-overview' }
  }
  if (role === 'agent_admin') {
    return { name: 'agent-overview' }
  }
  if (role === 'sales_staff') {
    return { name: 'sales-customers' }
  }
  if (role === 'staff') {
    if (hasStaffPermission('system.architecture.view')) {
      return { name: 'platform-overview' }
    }
    if (hasStaffPermission('finance.dashboard.view')) {
      return { name: 'staff-finance-hub' }
    }
    if (
      hasStaffPermission('inventory.view') ||
      hasStaffPermission('logistics.view') ||
      hasStaffPermission('inventory.after_sales.view')
    ) {
      return { name: 'resource-inventory' }
    }
    if (hasStaffPermission('customer.view_all')) {
      return { name: 'customers-hub' }
    }
    if (hasStaffPermission('agent.view_all')) {
      return { name: 'agents-hub' }
    }
    if (hasStaffPermission('sales.view_all')) {
      return { name: 'sales-hub' }
    }
    if (hasStaffPermission('commercial.membership.view')) {
      return { name: 'commercial-hub' }
    }
    if (hasStaffPermission('invitations.view_all')) {
      return { name: 'invitations' }
    }
    return { name: 'staff-hub' }
  }
  return { name: 'rooms' }
}

export const router = createRouter({
  history: createWebHistory(),
  routes: [
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
      path: '/staff',
      name: 'staff-hub',
      component: DomainHubView,
      props: { hub: 'staff' },
      meta: { internalStaffOnly: true },
    },
    {
      path: '/staff/groups',
      name: 'staff-groups',
      component: StaffOrganizationView,
      props: { initialTab: 'employees', groupsOnly: true },
      meta: { internalStaffOnly: true },
    },
    {
      path: '/staff/employees',
      name: 'staff-employees',
      component: StaffOrganizationView,
      props: { initialTab: 'employees' },
      meta: { internalStaffOnly: true },
    },
    {
      path: '/staff/roles',
      name: 'staff-roles',
      component: StaffOrganizationView,
      props: { initialTab: 'roles' },
      meta: { internalStaffOnly: true },
    },
    {
      path: '/staff/approvals',
      name: 'staff-approvals',
      component: StaffOrganizationView,
      props: { initialTab: 'approvals' },
      meta: { internalStaffOnly: true },
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
      component: ResourcesView,
      meta: { staffPermission: 'finance.resource.view' },
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
      meta: { staffPermission: 'system.architecture.view' },
    },
    {
      path: '/operations/live/monitor',
      name: 'live-monitor',
      component: LiveOperationsView,
      props: { focus: 'monitor' },
      meta: { staffPermission: 'system.architecture.view' },
    },
    {
      path: '/operations/live/events',
      name: 'live-events',
      component: LiveOperationsView,
      props: { focus: 'events' },
      meta: { staffPermission: 'system.architecture.view' },
    },
    {
      path: '/',
      name: 'rooms',
      component: RoomsView,
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
      path: '/sales/customers',
      name: 'sales-customers',
      component: SalesCustomersView,
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
      component: DomainHubView,
      props: { hub: 'commercial' },
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
      meta: { staffPermission: 'commercial.membership.view' },
    },
    {
      path: '/commercial/device-products',
      name: 'commercial-device-products',
      component: CommercialDeviceProductsView,
      meta: { staffPermission: 'commercial.membership.view' },
    },
    {
      path: '/commercial/referrals',
      name: 'commercial-referrals',
      component: IncentiveProgramsView,
      props: { mode: 'referral' },
      meta: { staffPermission: 'commercial.membership.view' },
    },
    {
      path: '/commercial/settlement',
      name: 'commercial-settlement',
      component: IncentiveProgramsView,
      props: { mode: 'settlement' },
      meta: { staffPermission: 'commercial.membership.view' },
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
    if (session.bootstrap) {
      return defaultAuthenticatedRoute()
    }
    return true
  }

  if (!session.bootstrap) {
    return {
      name: 'login',
      query: { redirect: to.fullPath },
    }
  }

  const role = session.bootstrap.actor.role
  const primaryGroupCode = session.bootstrap.staff_access?.primary_group_code

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
    to.name === 'resources-workspace'
  ) {
    return { name: 'staff-finance-ai-time' }
  }

  const staffPermission = to.meta.staffPermission
  if (
    typeof staffPermission === 'string' &&
    !hasStaffPermission(staffPermission)
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

  if (
    (role === 'staff' || role === 'sales_staff') &&
    (to.name === 'rooms' || to.name === 'rooms-list' || to.name === 'room-detail') &&
    !hasStaffPermission('system.architecture.view')
  ) {
    return defaultAuthenticatedRoute()
  }

  if (role === 'staff' && to.name === 'invitations') {
    if (!hasStaffPermission('invitations.view_all')) {
      return defaultAuthenticatedRoute()
    }
  }

  return true
})
