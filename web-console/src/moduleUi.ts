export type HubKey =
  | 'live'
  | 'activityMarketing'
  | 'staff'
  | 'customers'
  | 'agents'
  | 'sales'
  | 'commercial'
  | 'finance'
  | 'resources'

export interface ModuleEntry {
  title: string
  description: string
  icon: string
  to?: string
  badge?: string
  status?: 'ready' | 'planned'
}

export interface ModuleGroup {
  title: string
  description: string
  entries: ModuleEntry[]
}

export interface ModuleConfig {
  kicker: string
  title: string
  description: string
  hubTo: string
  parentTitle: string
  parentTo: string
  heroIcon: string
  groups: ModuleGroup[]
}

export const moduleUiMap: Record<HubKey, ModuleConfig> = {
  live: {
    kicker: 'LIVE OPERATIONS',
    title: '直播运维',
    description: '集中查看直播业务运行状态，再进入直播间、监控和事件处理具体工作。',
    hubTo: '/operations/live',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '播',
    groups: [{
      title: '功能入口',
      description: '围绕直播间、策略、会员价格和商品运营组织直播运维能力。',
      entries: [
        { title: '直播间', description: '管理直播间、查看实时公屏、直播画面和现场状态。', icon: '播', to: '/rooms/list', badge: '现场', status: 'ready' },
        { title: '直播间数量', description: '查看终端已开直播间、额度和剩余可开数量，由营销运维部统一管理。', icon: '额', to: '/operations/live/room-quotas', badge: '额度', status: 'ready' },
        { title: '直播策略', description: '通过策略 Agent 调教当前直播间的主播、话术、声音和第 3 层策略。', icon: '策', to: '/operations/live/strategy', badge: 'Agent', status: 'ready' },
        { title: '设备绑定', description: '查看名下设备状态，并管理设备与直播间的绑定关系。', icon: '设', to: '/operations/live/devices', badge: '设备', status: 'ready' },
      ],
    }],
  },
  activityMarketing: {
    kicker: 'ACTIVITY MARKETING',
    title: '活动营销',
    description: '统一管理营销活动、邀请推荐、优惠工具、奖励、渠道活动和营销数据；商品与资源配置作为营销执行底座继续保留。',
    hubTo: '/operations/live/marketing',
    parentTitle: '营销运维',
    parentTo: '/overview',
    heroIcon: '营',
    groups: [
      {
        title: '营销业务',
        description: '围绕获客、转介绍、优惠、奖励、渠道和效果数据形成完整营销闭环。',
        entries: [
          { title: '营销活动', description: '建立营销计划，为会员、时长卡和设备商品配置活动折扣、赠送、组合标的和生效时间。', icon: '营', to: '/commercial/marketing', badge: '活动', status: 'ready' },
          { title: '邀请与推荐', description: '统一管理全平台邀请码、邀请注册归属、推荐记录和邀请码策略；终端客户端仍保留一级入口。', icon: '邀', to: '/invitations', badge: '推荐', status: 'ready' },
          { title: '优惠工具', description: '汇总折扣、赠送、组合和限时等现有营销工具，统一回到营销活动配置，不把优惠参数写死在代码里。', icon: '惠', to: '/commercial/marketing/tools', badge: '工具', status: 'ready' },
          { title: '奖励管理', description: '配置推荐奖励触发条件、冻结期和退款冲回规则；AI 时长奖励继续经过真实审批与流水入账。', icon: '奖', to: '/commercial/referrals', badge: '奖励', status: 'ready' },
          { title: '渠道活动', description: '查看终端老带新、销售邀请、代理邀请等渠道来源，并从邀请关系和奖励规则继续配置。', icon: '渠', to: '/commercial/marketing/channels', badge: '渠道', status: 'ready' },
          { title: '营销数据', description: '汇总营销计划、邀请推荐、奖励规则的关键数据，检查活动到成交与奖励的链路状态。', icon: '数', to: '/commercial/marketing/analytics', badge: '数据', status: 'ready' },
        ],
      },
      {
        title: '营销商品与资源',
        description: '会员、时长卡、设备商品、AI 时长与规则模拟继续作为营销业务的执行底座。',
        entries: [
          { title: '会员方案', description: '维护会员基础价格、每月基础 AI 时长和会员固有权益。', icon: '会', to: '/commercial/memberships/plans', badge: '会员', status: 'ready' },
          { title: '规则模拟器', description: '使用测试用户预览会员购买、时长与营销规则，不真实入账。', icon: '测', to: '/commercial/memberships/simulator', badge: '沙盒', status: 'ready' },
          { title: '时长卡运营', description: '维护时长卡商品、价格、时长和上下架状态，终端只读取已发布价格。', icon: '时', to: '/commercial/time-cards', badge: '商品', status: 'ready' },
          { title: '设备商城运营', description: '管理设备商品展示、正常售价和上下架，营销价格由活动计划统一计算。', icon: '商', to: '/commercial/device-products', badge: '商城', status: 'ready' },
          { title: 'AI 时长', description: '查看代理与终端 AI 时长；营销运维发起增加申请，财务审核通过后才实际入账。', icon: '时', to: '/operations/live/marketing/ai-time', badge: '审批', status: 'ready' },
        ],
      },
    ],
  },
  staff: {
    kicker: 'INTERNAL ORGANIZATION',
    title: '组织架构',
    description: '统一管理内部部门、员工账号、角色权限和高风险审批规则。',
    hubTo: '/staff',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '组',
    groups: [
      {
        title: '组织与人员',
        description: '管理企业内部部门、员工账号、负责人和归属关系。',
        entries: [
          { title: '部门', description: '查看系统内置部门和自定义部门，并维护部门负责人。', icon: '部', to: '/staff/groups', badge: '组织', status: 'ready' },
          { title: '员工账号', description: '新增、停用员工，查看员工状态与所在组。', icon: '人', to: '/staff/employees', badge: '账号', status: 'ready' },
        ],
      },
      {
        title: '权限与风控',
        description: '角色、数据范围和高风险审批分开管理。',
        entries: [
          { title: '角色权限', description: '配置角色可以做什么以及默认数据范围。', icon: '权', to: '/staff/roles', badge: '权限', status: 'ready' },
          { title: '审批策略', description: '配置充值、退款、奖励等业务的审核规则。', icon: '审', to: '/staff/approvals', badge: '审批', status: 'ready' },
          { title: '权限审计', description: '查看高权限角色变化、授权与撤权记录。', icon: '迹', to: '/staff/audit', badge: '审计', status: 'ready' },
        ],
      },
    ],
  },
  customers: {
    kicker: 'CUSTOMER RESOURCE',
    title: '客户资源',
    description: '统一查看终端主档、来源归属、账号安全和管理审计。',
    hubTo: '/customers',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '客',
    groups: [
      {
        title: '终端主数据',
        description: '终端主档与商业归属分离管理。',
        entries: [
          { title: '终端列表', description: '搜索、筛选和查看终端主档与当前状态。', icon: '客', to: '/customers/list', badge: '主档', status: 'ready' },
          { title: '客户资源', description: '查看直营、代理、销售来源与当前销售归属。', icon: '客', to: '/customers/list?focus=attribution', badge: '归属', status: 'ready' },
        ],
      },
      {
        title: '账号与审计',
        description: '高风险账号操作与历史留痕。',
        entries: [
          { title: '密码重置', description: '帮助终端重置密码，管理员不能读取原密码。', icon: '密', to: '/customers/list?focus=security', badge: '安全', status: 'ready' },
          { title: '管理审计', description: '查看客户资源管理操作、经办人和执行结果。', icon: '审', to: '/customers/list?focus=audit', badge: '审计', status: 'ready' },
        ],
      },
    ],
  },
  agents: {
    kicker: 'AGENT NETWORK',
    title: '代理合作',
    description: '统一管理代理档案、等级政策、合作合同和退出清算。',
    hubTo: '/agents',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '代',
    groups: [{
      title: '代理经营',
      description: '代理档案、等级、合同和退出全周期管理。',
      entries: [
        { title: '代理列表', description: '查看、搜索和维护代理账号与组织档案。', icon: '代', to: '/agents/list', badge: '已上线', status: 'ready' },
        { title: '代理等级', description: '配置准入门槛、采购折扣、返佣和准备金。', icon: '级', to: '/agents/levels', badge: '等级', status: 'ready' },
        { title: '代理合同', description: '管理签约、续签、补充协议和规则快照。', icon: '约', to: '/agents/contracts', badge: '合同', status: 'ready' },
        { title: '退出清算', description: '清理库存、在途、售后、结算和负余额。', icon: '退', to: '/agents/exit', badge: '清算', status: 'ready' },
      ],
    }],
  },
  sales: {
    kicker: 'SALES SYSTEM',
    title: '销售体系',
    description: '统一查看销售团队、客户资源、业绩和提成关系。',
    hubTo: '/sales',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '销',
    groups: [{
      title: '销售经营',
      description: '销售人员、客户资源和业绩提成。',
      entries: [
        { title: '销售团队', description: '查看销售业务账号并进入内部员工体系维护。', icon: '销', to: '/sales/team', badge: '已上线', status: 'ready' },
        { title: '客户资源', description: '查看销售与客户的当前关系和历史来源。', icon: '客', to: '/customers/list?focus=attribution', badge: '归属', status: 'ready' },
        { title: '业绩与提成', description: '按订单和规则版本追踪业绩、提成与冲回。', icon: '绩', to: '/sales/performance', badge: '提成', status: 'ready' },
      ],
    }],
  },
  commercial: {
    kicker: 'ACTIVITY MARKETING',
    title: '活动营销',
    description: '旧商业中心入口已归并到活动营销。',
    hubTo: '/operations/live/marketing',
    parentTitle: '营销运维',
    parentTo: '/overview',
    heroIcon: '营',
    groups: [],
  },
  finance: {
    kicker: 'FINANCE CENTER',
    title: '财务与结算',
    description: '客户资金、经营收支、结算规则、审批、账务流水和追溯职责分离。',
    hubTo: '/staff/finance',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '财',
    groups: [
      {
        title: '资金业务',
        description: '终端资金账户和审批职责分离。',
        entries: [
          { title: '终端账户', description: '查看余额并发起授权范围内的资金操作。', icon: '账', to: '/staff/finance/accounts', badge: '账户', status: 'ready' },
          { title: '待审核', description: '集中处理充值、退款、奖励等待审核任务。', icon: '审', to: '/staff/finance/approvals', badge: '审批', status: 'ready' },
          { title: '经营收支', description: '统一查看设备采购、物流成本、报废收入和 Token 采购成本。', icon: '营', to: '/staff/finance/operating', badge: '成本', status: 'ready' },
        ],
      },
      {
        title: '账务与追溯',
        description: '通过流水和操作历史解释每一笔资金变化。',
        entries: [
          { title: '钱包流水', description: '按终端查看不可变资金流水和余额变化。', icon: '流', to: '/staff/finance/ledger', badge: '流水', status: 'ready' },
          { title: '操作记录', description: '查看经办、审核结果和处理时间。', icon: '录', to: '/staff/finance/history', badge: '审计', status: 'ready' },
          { title: '全链路追溯', description: '从任意资金穿透到订单、结算、提现和责任人。', icon: '追', to: '/staff/finance/trace', badge: '追溯', status: 'ready' },
          { title: '结算规则', description: '配置销售提成、代理返佣、最低订单金额、冻结期和冲回规则。', icon: '规', to: '/commercial/settlement', badge: '规则', status: 'ready' },
          { title: '收益结算', description: '查看收益明细并完成结算批次生成、审核和支付。', icon: '结', to: '/staff/finance/settlements', badge: '结算', status: 'ready' },
        ],
      },
    ],
  },
  resources: {
    kicker: 'WAREHOUSE & STOCK',
    title: '设备与仓储',
    description: '按设备名称管理采购批量入库、批量出库和物流交付；每一台实物都保留独立 SN、内部 ID 和完整生命周期记录。',
    hubTo: '/resources',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '库',
    groups: [
      {
        title: '设备库存业务',
        description: '设备出入库与物流交付统一按 SN 串联追溯；售后维修作为独立业务项目单独管理。',
        entries: [
          { title: '设备出入库', description: '按设备名称管理采购批量入库、SN 实物库存和批量出库。', icon: '库', to: '/resources/inventory', badge: '出入库', status: 'ready' },
          { title: '设备商品资料', description: '仓库维护设备商品/SKU 主资料；商品上下架由营销运维部负责。', icon: '设', to: '/commercial/device-products', badge: '主数据', status: 'ready' },
          { title: '物流管理', description: '管理快递和直接领取，关联具体出库对象、运单和设备 SN。', icon: '物', to: '/resources/logistics', badge: '物流', status: 'ready' },
        ],
      },
    ],
  },
}

export function moduleEntries(hub: HubKey) {
  return moduleUiMap[hub].groups.flatMap((group) => group.entries)
}
