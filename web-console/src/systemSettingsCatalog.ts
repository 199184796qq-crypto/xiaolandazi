import type { Bootstrap } from './types'

export type SystemSettingsCategoryKey =
  | 'general'
  | 'intelligence'
  | 'live'
  | 'commerce'
  | 'inventory'
  | 'access'

export interface SystemSettingsEntry {
  key: string
  title: string
  description: string
  icon: string
  to: string
  badge?: string
  viewAny?: string[]
  platformOnly?: boolean
  superAdminOnly?: boolean
}

export interface SystemSettingsGroup {
  key: string
  title: string
  description: string
  entries: SystemSettingsEntry[]
}

export interface SystemSettingsCategory {
  key: SystemSettingsCategoryKey
  title: string
  kicker: string
  description: string
  icon: string
  to: string
  groups: SystemSettingsGroup[]
}

export const systemSettingsCategories: SystemSettingsCategory[] = [
  {
    key: 'general',
    title: '基础与品牌',
    kicker: 'GENERAL & BRAND',
    description: '统一系统名称、智能体称呼、页脚、备案和品牌展示。',
    icon: '基',
    to: '/system/settings/general',
    groups: [
      {
        key: 'brand-display',
        title: '品牌与公共展示',
        description: '影响全站公共展示，修改后立即作用于登录页、导航和页脚。',
        entries: [
          {
            key: 'global-display',
            title: '系统名称与公共文字',
            description: '设置系统名称、登录首页宣传文案、智能体名称、备案链接和页脚内容。',
            icon: '名',
            to: '/system/settings/general#global-display',
            badge: '平台级',
            platformOnly: true,
          },
        ],
      },
    ],
  },
  {
    key: 'intelligence',
    title: 'AI 与智能体',
    kicker: 'AI & AGENT',
    description: '集中管理理解路由、模型指令、直播策略和录音分析。',
    icon: '智',
    to: '/system/settings/intelligence',
    groups: [
      {
        key: 'understanding',
        title: '理解与模型',
        description: '决定请求由程序还是大模型处理，以及模型收到的系统级业务约束。',
        entries: [
          {
            key: 'agent-routing',
            title: '智能体理解路由',
            description: '配置程序理解、大模型理解、预算、上下文、降级和版本发布。',
            icon: '路',
            to: '/system/settings/agent-routing',
            badge: '版本化',
            viewAny: ['system.settings.agent_routing.manage'],
          },
          {
            key: 'agent-prompts',
            title: '模型与智能体指令',
            description: '维护系统发给模型的业务指令，使用草稿、发布、回滚机制。',
            icon: '模',
            to: '/system/settings/intelligence#agent-prompts',
            badge: '高风险',
            superAdminOnly: true,
          },
          {
            key: 'speech-models',
            title: '主播话术与互动模型',
            description: '配置多个模型 API、密钥和基础提示词，选择话术与实时回答的默认模型，并测试返回文字。独立于智能体模型。',
            icon: '播',
            to: '/system/settings/intelligence/speech-models',
            badge: '独立模型',
            superAdminOnly: true,
          },
          {
            key: 'live-analysis',
            title: '直播录音分析',
            description: '配置录音分析使用的模型、提示词版本和启用状态。',
            icon: '析',
            to: '/system/settings/intelligence/live-analysis',
            badge: '直播',
            viewAny: ['liveanalysis.view'],
          },
        ],
      },
      {
        key: 'agent-behavior',
        title: '智能体行为',
        description: '配置系统级互动、打断、主线回归和称呼行为。',
        entries: [
          {
            key: 'strategy-center',
            title: '直播策略中心',
            description: '维护基础出现概率、打断、回归、互动与称呼策略并同步 Core。',
            icon: '策',
            to: '/system/settings/intelligence#strategy-center',
            badge: '系统级',
            viewAny: ['system.settings.liveops.manage'],
          },
        ],
      },
    ],
  },
  {
    key: 'live',
    title: '直播业务',
    kicker: 'LIVE BUSINESS',
    description: '管理直播行业、规则层、会员房间上限和客户容量。',
    icon: '播',
    to: '/system/settings/live',
    groups: [
      {
        key: 'policy',
        title: '行业与规则',
        description: '统一直播行业目录、系统/行业规则和规则页面显示规格。',
        entries: [
          {
            key: 'live-industries',
            title: '直播行业目录',
            description: '维护行业编码、名称和排序，作为行业层策略的统一入口。',
            icon: '行',
            to: '/system/settings/live#live-industries',
            badge: '目录',
            viewAny: ['livepolicy.view'],
          },
          {
            key: 'live-policy',
            title: '系统与行业规则',
            description: '维护规则层、行业层规则、测试样例、版本发布和学习候选。',
            icon: '规',
            to: '/system/settings/live/policy',
            badge: '策略',
            viewAny: ['livepolicy.view'],
          },
          {
            key: 'live-typography',
            title: '规则显示规格',
            description: '设置规则列表和测试结果的标题、正文与辅助文字大小。',
            icon: '字',
            to: '/system/settings/live#live-typography',
            badge: '平台级',
            platformOnly: true,
          },
        ],
      },
      {
        key: 'capacity',
        title: '会员与容量',
        description: '系统上限与客户实际额度分开管理。',
        entries: [
          {
            key: 'membership-room-limits',
            title: '会员直播间上限',
            description: '配置各会员等级允许创建的最大直播间数量。',
            icon: '会',
            to: '/system/settings/live#membership-room-limits',
            badge: '会员',
            viewAny: ['commercial.membership.view'],
          },
          {
            key: 'customer-room-quotas',
            title: '客户直播间额度',
            description: '查看和调整单个客户的实际直播间额度，不得超过会员上限。',
            icon: '额',
            to: '/system/settings/live/room-quotas',
            badge: '客户级',
            viewAny: ['liveops.room_quota.view'],
          },
        ],
      },
    ],
  },
  {
    key: 'commerce',
    title: '商品、计费与财务',
    kicker: 'COMMERCE & BILLING',
    description: '集中商品价格、营销、虚拟豆、奖励、结算和财务风控配置。',
    icon: '商',
    to: '/system/settings/commerce',
    groups: [
      {
        key: 'catalog',
        title: '商品与价格',
        description: '配置客户可购买的会员、时长卡和设备商品。',
        entries: [
          {
            key: 'membership-plans',
            title: '会员方案',
            description: '维护会员价格、基础时长、权益和发布版本。',
            icon: '会',
            to: '/system/settings/commerce/memberships',
            badge: '商品',
            viewAny: ['commercial.membership.view'],
          },
          {
            key: 'time-cards',
            title: '时长卡',
            description: '维护时长、售价、有效期、上下架和结算参与规则。',
            icon: '时',
            to: '/system/settings/commerce/time-cards',
            badge: '商品',
            viewAny: ['commercial.time_card.view'],
          },
          {
            key: 'device-products',
            title: '设备商城商品',
            description: '维护设备商品展示、售价和上下架。',
            icon: '设',
            to: '/system/settings/commerce/device-products',
            badge: '商品',
            viewAny: ['commercial.device.view'],
          },
          {
            key: 'device-stock-hold',
            title: '订单锁库规则',
            description: '设置设备订单未支付时占用具体 SN 的最长时间。',
            icon: '锁',
            to: '/system/settings/commerce#device-stock-hold',
            badge: '平台级',
            platformOnly: true,
          },
        ],
      },
      {
        key: 'marketing-billing',
        title: '营销与计费',
        description: '配置活动价格、虚拟豆扣费和推荐奖励。',
        entries: [
          {
            key: 'marketing-campaigns',
            title: '营销活动',
            description: '维护活动、优惠、赠送、挂链标的和生效时间。',
            icon: '营',
            to: '/system/settings/commerce/marketing',
            badge: '活动',
            viewAny: ['commercial.marketing.view'],
          },
          {
            key: 'marketing-tools',
            title: '优惠工具',
            description: '集中维护折扣、赠送、组合和限时等营销工具。',
            icon: '惠',
            to: '/system/settings/commerce/marketing-tools',
            badge: '营销',
            viewAny: ['commercial.marketing.view'],
          },
          {
            key: 'bean-economy',
            title: '虚拟豆计费',
            description: '维护购豆比例、行为扣豆、员工分成和计价边界。',
            icon: '豆',
            to: '/system/settings/commerce/beans',
            badge: '计费',
            viewAny: ['commercial.beans.view'],
          },
          {
            key: 'referral-rules',
            title: '推荐奖励',
            description: '配置奖励条件、冻结期、退款冲回和版本。',
            icon: '奖',
            to: '/system/settings/commerce/referrals',
            badge: '奖励',
            viewAny: ['commercial.referral.view'],
          },
        ],
      },
      {
        key: 'finance-control',
        title: '财务与结算',
        description: '配置审批隔离、提成、返佣和结算规则。',
        entries: [
          {
            key: 'finance-review-policy',
            title: '财务审核规则',
            description: '设置经办人与审核人是否必须分离，审计记录始终保留。',
            icon: '审',
            to: '/system/settings/commerce#finance-review-policy',
            badge: '平台级',
            platformOnly: true,
          },
          {
            key: 'settlement-rules',
            title: '提成与返佣规则',
            description: '维护销售提成、代理返佣、冻结期和冲回规则。',
            icon: '结',
            to: '/system/settings/commerce/settlement',
            badge: '财务',
            viewAny: ['finance.settlement_rules.view'],
          },
          {
            key: 'agent-levels',
            title: '代理等级政策',
            description: '维护代理准入门槛、采购折扣、返佣和准备金政策。',
            icon: '代',
            to: '/system/settings/commerce/agent-levels',
            badge: '渠道',
            viewAny: ['agent.view_all'],
          },
        ],
      },
    ],
  },
  {
    key: 'inventory',
    title: '设备与仓储',
    kicker: 'DEVICE & INVENTORY',
    description: '管理仓库、物流商、单位和设备商品基础资料。',
    icon: '库',
    to: '/system/settings/inventory',
    groups: [
      {
        key: 'warehouse',
        title: '仓库基础资料',
        description: '业务页面只引用统一主数据，不再各自维护固定选项。',
        entries: [
          {
            key: 'warehouses',
            title: '仓库管理',
            description: '维护系统仓和自定义仓库的名称、编码与状态。',
            icon: '仓',
            to: '/system/settings/inventory#warehouses',
            badge: '主数据',
            viewAny: ['inventory.view'],
          },
          {
            key: 'inventory-dictionaries',
            title: '物流商与产品单位',
            description: '统一维护物流服务商、设备计量单位和业务显示名称。',
            icon: '典',
            to: '/system/settings/inventory#inventory-dictionaries',
            badge: '字典',
            viewAny: ['inventory.view'],
          },
        ],
      },
      {
        key: 'device-master',
        title: '设备商品主数据',
        description: '商品主档与库存实物分开维护。',
        entries: [
          {
            key: 'device-master-data',
            title: '设备商品资料',
            description: '维护设备名称、SKU、规格和仓库识别信息。',
            icon: '设',
            to: '/system/settings/inventory/device-products',
            badge: '商品主档',
            viewAny: ['commercial.device.view'],
          },
        ],
      },
    ],
  },
  {
    key: 'access',
    title: '权限、流程与审计',
    kicker: 'ACCESS & GOVERNANCE',
    description: '管理组织角色、权限来源、审批规则和关键操作追溯。',
    icon: '权',
    to: '/system/settings/access',
    groups: [
      {
        key: 'organization-access',
        title: '组织与权限',
        description: '岗位角色、可操作能力和数据范围分开配置。',
        entries: [
          {
            key: 'departments',
            title: '部门结构',
            description: '维护内部部门、负责人和组织归属。',
            icon: '部',
            to: '/system/settings/access/departments',
            badge: '组织',
            viewAny: ['staff.group.view'],
          },
          {
            key: 'roles',
            title: '角色权限',
            description: '维护角色、权限组合和默认数据范围。',
            icon: '角',
            to: '/system/settings/access/roles',
            badge: 'RBAC',
            viewAny: ['staff.role.view'],
          },
          {
            key: 'permission-center',
            title: '权限中心',
            description: '从业务能力反查角色、员工和权限来源。',
            icon: '核',
            to: '/system/settings/access/permissions',
            badge: '诊断',
            viewAny: ['staff.role.manage'],
          },
          {
            key: 'employees',
            title: '员工与岗位',
            description: '维护员工账号、部门归属和岗位角色。',
            icon: '人',
            to: '/system/settings/access/employees',
            badge: '组织',
            viewAny: ['staff.employee.view'],
          },
        ],
      },
      {
        key: 'workflow-audit',
        title: '审批与审计',
        description: '高风险操作经过审批，并保留完整操作记录。',
        entries: [
          {
            key: 'approval-policies',
            title: '审批策略',
            description: '查看和处理充值、退款、奖励等业务审批规则。',
            icon: '审',
            to: '/system/settings/access/approvals',
            badge: '流程',
            viewAny: ['finance.dashboard.view'],
          },
          {
            key: 'audit-log',
            title: '操作审计',
            description: '查询关键操作的经办人、来源、原因、前后值和结果。',
            icon: '迹',
            to: '/system/settings/access/audit',
            badge: '追溯',
            viewAny: ['audit.view'],
          },
        ],
      },
    ],
  },
]

export function hasSystemSettingsPermission(bootstrap: Bootstrap | null | undefined, code: string) {
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

export function canViewSystemSettingsEntry(
  entry: SystemSettingsEntry,
  bootstrap: Bootstrap | null | undefined,
) {
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  if (bootstrap.actor.role !== 'staff') return false
  const access = bootstrap.staff_access
  if (!access) return false
  if (entry.platformOnly) return false
  if (entry.superAdminOnly) return access.is_super_admin
  if (access.is_super_admin) return true
  if (!entry.viewAny?.length) return access.permissions.includes('system.settings.view')
  return entry.viewAny.some((permission) => access.permissions.includes(permission))
}

export function visibleSystemSettingsGroups(
  category: SystemSettingsCategory,
  bootstrap: Bootstrap | null | undefined,
) {
  return category.groups
    .map((group) => ({
      ...group,
      entries: group.entries.filter((entry) => canViewSystemSettingsEntry(entry, bootstrap)),
    }))
    .filter((group) => group.entries.length > 0)
}

export function visibleSystemSettingsCategories(bootstrap: Bootstrap | null | undefined) {
  return systemSettingsCategories.filter(
    (category) => visibleSystemSettingsGroups(category, bootstrap).length > 0,
  )
}

export function findSystemSettingsCategory(key: string | undefined) {
  return systemSettingsCategories.find((category) => category.key === key)
}

export function canAccessSystemSettings(bootstrap: Bootstrap | null | undefined) {
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  if (bootstrap.actor.role !== 'staff') return false
  if (!hasSystemSettingsPermission(bootstrap, 'system.settings.view')) return false
  return visibleSystemSettingsCategories(bootstrap).length > 0
}
