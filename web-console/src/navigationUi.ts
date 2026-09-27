import { moduleEntries, moduleUiMap, type HubKey } from './moduleUi'
import type { Bootstrap } from './types'

export type WorkspaceNavKey =
  | 'workspace-auto'
  | 'workspace-admin'
  | 'workspace-staff'
  | 'workspace-sales'
  | 'workspace-agent'
  | 'workspace-customer'
  | 'personal-auto'

export type NavigationContextKey = HubKey | WorkspaceNavKey | 'staff-after-sales'

export interface NavigationLink {
  title: string
  to: string
  icon: string
}

export interface AgentNavigationTarget extends NavigationLink {
  section: string
}


function moduleEntryVisible(key: HubKey, to: string, bootstrap: Bootstrap | null | undefined) {
  if (to === '/staff/finance/invitations') return hasStaffPermission(bootstrap, 'finance.dashboard.view')
  if (to === '/operations/live/customer-handoffs') {
    return hasStaffPermission(bootstrap, 'liveops.configure') || hasStaffPermission(bootstrap, 'liveops.ticket.manage')
  }
  if (to === '/sales/handovers') return hasStaffPermission(bootstrap, 'sales.assignment.manage')
  if (key === 'staff' && to.startsWith('/staff/permissions')) {
    return hasStaffPermission(bootstrap, 'staff.role.manage')
  }
  if (key === 'staff' && to.startsWith('/staff/approvals')) {
    return hasStaffPermission(bootstrap, 'finance.dashboard.view')
  }
  if (key === 'staff' && to.startsWith('/staff/audit')) {
    return hasStaffPermission(bootstrap, 'audit.view')
  }
  if (key === 'customers' && to.includes('focus=security')) {
    return Boolean(
      bootstrap?.actor.role === 'platform_admin' || bootstrap?.staff_access?.is_super_admin,
    )
  }
  if (key === 'customers' && to.includes('focus=audit')) {
    return hasStaffPermission(bootstrap, 'audit.view')
  }
  if (key === 'resources' && to.includes('focus=adjust')) {
    return hasStaffPermission(bootstrap, 'finance.resource.adjust')
  }
  if (key === 'live' && to.startsWith('/operations/live/room-quotas')) {
    return hasStaffPermission(bootstrap, 'liveops.room_quota.view')
  }
  if (key === 'live' && to.startsWith('/operations/live/analysis-settings')) {
    return hasStaffPermission(bootstrap, 'liveanalysis.view')
  }
  if (key === 'live' && to.startsWith('/operations/live/marketing')) {
    return (
      hasStaffPermission(bootstrap, 'commercial.marketing.view') ||
      hasStaffPermission(bootstrap, 'commercial.time_card.view') ||
      hasStaffPermission(bootstrap, 'commercial.device.view')
    )
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/marketing/channels')) {
    return hasStaffPermission(bootstrap, 'invitations.view_all')
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/marketing')) {
    return hasStaffPermission(bootstrap, 'commercial.marketing.view')
  }
  if (key === 'activityMarketing' && to.startsWith('/operations/live/marketing/ai-time')) {
    return hasStaffPermission(bootstrap, 'commercial.ai_time.view')
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/memberships/plans')) {
    return hasStaffPermission(bootstrap, 'commercial.membership.view')
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/memberships/simulator')) {
    return hasStaffPermission(bootstrap, 'commercial.membership.view')
  }
  if (key === 'activityMarketing' && to.startsWith('/invitations')) {
    return hasStaffPermission(bootstrap, 'invitations.view_all')
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/referrals')) {
    return hasStaffPermission(bootstrap, 'commercial.referral.view')
  }
  if (key === 'finance' && to.startsWith('/commercial/settlement')) {
    return hasStaffPermission(bootstrap, 'finance.settlement_rules.view')
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/time-cards')) {
    return hasStaffPermission(bootstrap, 'commercial.time_card.view')
  }
  if (key === 'activityMarketing' && to.startsWith('/commercial/device-products')) {
    return hasStaffPermission(bootstrap, 'commercial.device.view')
  }
  return true
}
export interface NavigationContext {
  rootTitle: string
  rootTo: string
  sectionTitle?: string
  sectionTo?: string
  sectionIcon?: string
  kicker: string
  entries: NavigationLink[]
}

function hasStaffPermission(bootstrap: Bootstrap | null | undefined, code: string) {
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

function adminEntries(): NavigationLink[] {
  return [
    { title: '系统总览', to: '/overview', icon: '总' },
    { title: '系统设定', to: '/system/settings', icon: '设' },
    { title: '组织架构', to: '/staff', icon: '部' },
    { title: '直播运维', to: '/operations/live', icon: '播' },
    { title: '活动营销', to: '/operations/live/marketing', icon: '营' },
    { title: '客户资源', to: '/customers', icon: '客' },
    { title: '代理合作', to: '/agents', icon: '代' },
    { title: '销售体系', to: '/sales', icon: '销' },
    { title: '财务与结算', to: '/staff/finance', icon: '财' },
    { title: '设备与仓储', to: '/resources', icon: '库' },
    { title: '物流与售后', to: '/staff/after-sales', icon: '修' },
  ]
}

function customerEntries(): NavigationLink[] {
  return [
    { title: '直播运维', to: '/', icon: '播' },
    { title: '运维协助', to: '/support', icon: '助' },
    { title: 'AI 时长', to: '/resources/workspace', icon: '时' },
    { title: '终端商城', to: '/shop', icon: '商' },
    { title: '售后维修', to: '/after-sales', icon: '修' },
    { title: '邀请与推荐', to: '/invitations', icon: '邀' },
    { title: '我的钱包', to: '/finance', icon: '财' },
    { title: '资金记录', to: '/finance/records', icon: '流' },
  ]
}

function agentEntries(): NavigationLink[] {
  return [
    { title: '代理总览', to: '/agent/overview', icon: '总' },
    { title: '客户资源', to: '/agent/customers', icon: '客' },
    { title: 'AI 时长', to: '/resources/workspace', icon: '时' },
    { title: '邀请与推荐', to: '/invitations', icon: '邀' },
  ]
}

function salesEntries(): NavigationLink[] {
  return [
    { title: '销售工作台', to: '/sales/workspace', icon: '工' },
    { title: '意向顾客', to: '/sales/leads', icon: '意' },
    { title: '我的客户', to: '/sales/customers', icon: '客' },
    { title: '收款进度', to: '/sales/receipts', icon: '款' },
    { title: '运维协助', to: '/sales/support', icon: '助' },
    { title: '跟进与回访', to: '/sales/followups', icon: '访' },
    { title: '产品与报价', to: '/sales/catalog', icon: '价' },
    { title: '我的业绩', to: '/sales/my-performance', icon: '绩' },
    { title: '邀请与推荐', to: '/invitations', icon: '邀' },
  ]
}

function staffEntries(bootstrap: Bootstrap | null | undefined): NavigationLink[] {
  const entries: NavigationLink[] = []
  const access = bootstrap?.staff_access
  const groupCode = access?.primary_group_code || ''
  const roleCodes = new Set(access?.role_codes || [])
  const isPrimaryGroupManager =
    roleCodes.has(groupCode + '_manager') ||
    Boolean(
      access?.primary_group_id &&
        access.managed_group_ids?.includes(access.primary_group_id),
    )

  if (groupCode === 'management') {
    return [
      { title: '系统总览', to: '/overview', icon: '总' },
      { title: '组织架构', to: '/staff', icon: '部' },
    ]
  }

  if (groupCode === 'finance') {
    if (hasStaffPermission(bootstrap, 'finance.dashboard.view')) {
      entries.push({ title: '客户收款确认', to: '/staff/finance/receipts', icon: '款' })
    }
    entries.push({ title: '财务与结算', to: '/staff/finance', icon: '财' })
    if (isPrimaryGroupManager) entries.push({ title: '组织架构', to: '/staff', icon: '部' })
    return entries
  }

  if (groupCode === 'sales') {
    if (hasStaffPermission(bootstrap, 'customer.view_all')) {
      entries.push({ title: '客户资源', to: '/customers', icon: '客' })
    }
    if (hasStaffPermission(bootstrap, 'sales.view_all')) {
      entries.push({ title: '销售体系', to: '/sales', icon: '销' })
    }
    if (isPrimaryGroupManager) entries.push({ title: '组织架构', to: '/staff', icon: '部' })
    return entries
  }

  if (groupCode === 'live_operations') {
    entries.push(
      { title: '直播运维', to: '/operations/live', icon: '播' },
      { title: '活动营销', to: '/operations/live/marketing', icon: '营' },
    )
    if (isPrimaryGroupManager) entries.push({ title: '组织架构', to: '/staff', icon: '部' })
    return entries
  }

  if (groupCode === 'warehouse_after_sales') {
    entries.push(
      { title: '设备与仓储', to: '/resources', icon: '库' },
      { title: '物流与售后', to: '/staff/after-sales', icon: '修' },
    )
    if (isPrimaryGroupManager) entries.push({ title: '组织架构', to: '/staff', icon: '部' })
    return entries
  }

  if (hasStaffPermission(bootstrap, 'system.architecture.view')) {
    entries.push({ title: '系统总览', to: '/overview', icon: '总' })
  }
  if (
    hasStaffPermission(bootstrap, 'liveops.configure') ||
    hasStaffPermission(bootstrap, 'liveops.view_all')
  ) {
    entries.push({ title: '直播运维', to: '/operations/live', icon: '播' })
  }
  if (hasStaffPermission(bootstrap, 'finance.dashboard.view')) {
    entries.push({ title: '财务与结算', to: '/staff/finance', icon: '财' })
  }
  if (hasStaffPermission(bootstrap, 'inventory.view') || hasStaffPermission(bootstrap, 'logistics.view')) {
    entries.push({ title: '设备与仓储', to: '/resources', icon: '库' })
  }
  if (
    hasStaffPermission(bootstrap, 'staff.group.view') ||
    hasStaffPermission(bootstrap, 'staff.employee.view') ||
    hasStaffPermission(bootstrap, 'staff.role.view')
    || hasStaffPermission(bootstrap, 'staff.role.manage')
  ) {
    entries.push({ title: '组织架构', to: '/staff', icon: '部' })
  }

  return entries
}

function workspaceForRole(bootstrap: Bootstrap | null | undefined): WorkspaceNavKey {
  const role = bootstrap?.actor.role
  if (role === 'platform_admin') return 'workspace-admin'
  if (role === 'agent_admin') return 'workspace-agent'
  if (role === 'sales_staff') return 'workspace-sales'
  if (role === 'staff') return 'workspace-staff'
  return 'workspace-customer'
}

function rootForWorkspace(
  key: Exclude<WorkspaceNavKey, 'workspace-auto' | 'personal-auto'>,
  bootstrap: Bootstrap | null | undefined,
) {
  if (key === 'workspace-admin') {
    return {
      title: '系统',
      to: '/overview',
      kicker: 'SYSTEM',
      entries: adminEntries(),
    }
  }
  if (key === 'workspace-agent') {
    return {
      title: '代理工作台',
      to: '/agent/overview',
      kicker: 'AGENT CONSOLE',
      entries: agentEntries(),
    }
  }
  if (key === 'workspace-sales') {
    return {
      title: '销售工作台',
      to: '/sales/workspace',
      kicker: 'SALES CONSOLE',
      entries: salesEntries(),
    }
  }
  if (key === 'workspace-staff') {
    return {
      title: (bootstrap?.staff_access?.primary_group_name || '内部员工') + '工作台',
      to: '/staff',
      kicker: 'STAFF CONSOLE',
      entries: staffEntries(bootstrap),
    }
  }
  return {
    title: '终端工作台',
    to: '/',
    kicker: 'CUSTOMER CONSOLE',
    entries: customerEntries(),
  }
}

function moduleRoot(
  hub: HubKey,
  bootstrap: Bootstrap | null | undefined,
) {
  const role = bootstrap?.actor.role

  if (role === 'agent_admin') {
    return rootForWorkspace('workspace-agent', bootstrap)
  }
  if (role === 'sales_staff') {
    return rootForWorkspace('workspace-sales', bootstrap)
  }
  if (role === 'customer' && hub === 'live') {
    return rootForWorkspace('workspace-customer', bootstrap)
  }
  if (role === 'staff' && !hasStaffPermission(bootstrap, 'system.architecture.view')) {
    return rootForWorkspace('workspace-staff', bootstrap)
  }

  const config = moduleUiMap[hub]
  return {
    title: config.parentTitle,
    to: config.parentTo,
    kicker: config.kicker,
    entries: [],
  }
}

export function resolveAgentNavigationTargets(
  bootstrap: Bootstrap | null | undefined,
): AgentNavigationTarget[] {
  if (!bootstrap) return []

  const targets: AgentNavigationTarget[] = []
  const seen = new Set<string>()
  const add = (title: string, to: string, icon: string, section: string) => {
    const normalizedTitle = title.trim()
    const normalizedTo = to.trim()
    if (!normalizedTitle || !normalizedTo) return
    const key = normalizedTitle + '|' + normalizedTo
    if (seen.has(key)) return
    seen.add(key)
    targets.push({
      title: normalizedTitle,
      to: normalizedTo,
      icon,
      section,
    })
  }

  if (['platform_admin','staff','sales_staff'].includes(bootstrap.actor.role) && hasStaffPermission(bootstrap,'finance.dashboard.view')) {
    add('邀请与推荐（财务）', '/staff/finance/invitations', '邀', '财务与结算')
  }

  const workspace = resolveNavigationContext('workspace-auto', bootstrap)
  add(workspace.rootTitle, workspace.rootTo, '总', '工作台')
  for (const entry of workspace.entries) {
    add(entry.title, entry.to, entry.icon, '工作台')
  }

  const expandable = new Map<string, HubKey>([
    ['/operations/live', 'live'],
    ['/operations/live/marketing', 'activityMarketing'],
    ['/staff', 'staff'],
    ['/customers', 'customers'],
    ['/agents', 'agents'],
    ['/sales', 'sales'],
    ['/staff/finance', 'finance'],
    ['/resources', 'resources'],
  ])

  const visibleWorkspacePaths = new Set(workspace.entries.map((entry) => entry.to))
  if (bootstrap.actor.role === 'customer') {
    visibleWorkspacePaths.add('/operations/live')
  }

  for (const [rootPath, hub] of expandable) {
    if (!visibleWorkspacePaths.has(rootPath)) continue
    const context = resolveNavigationContext(hub, bootstrap)
    if (context.sectionTitle && context.sectionTo) {
      add(context.sectionTitle, context.sectionTo, context.sectionIcon || '入', context.sectionTitle)
    }
    for (const entry of context.entries) {
      add(entry.title, entry.to, entry.icon, context.sectionTitle || context.rootTitle)
    }
  }

  const personal = resolveNavigationContext('personal-auto', bootstrap)
  for (const entry of personal.entries) {
    add(entry.title, entry.to, entry.icon, '个人中心')
  }

  return targets
}

export function resolveNavigationContext(
  key: NavigationContextKey,
  bootstrap: Bootstrap | null | undefined,
): NavigationContext {
  if (key === 'staff-after-sales') {
    const workspace = rootForWorkspace(
      workspaceForRole(bootstrap) as Exclude<WorkspaceNavKey, 'workspace-auto' | 'personal-auto'>,
      bootstrap,
    )
    return {
      rootTitle: workspace.title,
      rootTo: workspace.to,
      sectionTitle: '售后维修',
      sectionTo: '/staff/after-sales',
      sectionIcon: '修',
      kicker: 'AFTER-SALES SERVICE',
      entries: [{ title: '售后维修', to: '/staff/after-sales', icon: '修' }],
    }
  }

  if (key === 'personal-auto') {
    const workspace = rootForWorkspace(
      workspaceForRole(bootstrap) as Exclude<WorkspaceNavKey, 'workspace-auto' | 'personal-auto'>,
      bootstrap,
    )
    return {
      rootTitle: workspace.title,
      rootTo: workspace.to,
      sectionTitle: '个人中心',
      sectionTo: '/personal',
      sectionIcon: '我',
      kicker: 'PERSONAL CENTER',
      entries: [
        { title: '账户与安全', to: '/account', icon: '安' },
        { title: '个人设置', to: '/settings', icon: '设' },
      ],
    }
  }

  if (key.startsWith('workspace-')) {
    const resolvedKey =
      key === 'workspace-auto'
        ? workspaceForRole(bootstrap)
        : key

    const workspace = rootForWorkspace(
      resolvedKey as Exclude<WorkspaceNavKey, 'workspace-auto' | 'personal-auto'>,
      bootstrap,
    )

    return {
      rootTitle: workspace.title,
      rootTo: workspace.to,
      kicker: workspace.kicker,
      entries: workspace.entries,
    }
  }

  const config = moduleUiMap[key as HubKey]
  const root = moduleRoot(key as HubKey, bootstrap)

  return {
    rootTitle: root.title,
    rootTo: root.to,
    sectionTitle: config.title,
    sectionTo:
      bootstrap?.actor.role === 'customer' && key === 'live'
        ? '/'
        : config.hubTo,
    sectionIcon: config.heroIcon,
    kicker: config.kicker,
    entries: moduleEntries(key as HubKey)
      .filter((entry): entry is typeof entry & { to: string } => {
        if (!entry.to) return false
        if (!moduleEntryVisible(key as HubKey, entry.to, bootstrap)) return false
        if (
          key === 'resources' &&
          bootstrap?.actor.role === 'agent_admin'
        ) {
          return entry.to.startsWith('/resources/workspace')
        }
        return true
      })
      .map((entry) => ({
        title: entry.title,
        to: entry.to,
        icon: entry.icon,
      })),
  }
}
