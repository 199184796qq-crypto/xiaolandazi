export type HubKey =
  | 'live'
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
      title: '直播业务',
      description: '直播间日常管理、运行状态和异常事件。',
      entries: [
        { title: '直播间列表', description: '查看全部直播间、状态、平台和连接信息。', icon: '播', to: '/rooms/list', badge: '已上线', status: 'ready' },
        { title: '运行监控', description: '集中查看在线、连接中和异常直播间。', icon: '监', to: '/operations/live/monitor', badge: '监控', status: 'ready' },
        { title: '事件记录', description: '按直播间查看采集、连接和异常事件。', icon: '事', to: '/operations/live/events', badge: '事件', status: 'ready' },
      ],
    }],
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
    title: '终端资源',
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
          { title: '终端归属', description: '查看直营、代理、销售来源与当前销售归属。', icon: '归', to: '/customers/list?focus=attribution', badge: '归属', status: 'ready' },
        ],
      },
      {
        title: '账号与审计',
        description: '高风险账号操作与历史留痕。',
        entries: [
          { title: '密码重置', description: '帮助终端重置密码，管理员不能读取原密码。', icon: '密', to: '/customers/list?focus=security', badge: '安全', status: 'ready' },
          { title: '管理审计', description: '查看终端管理操作、经办人和执行结果。', icon: '审', to: '/customers/list?focus=audit', badge: '审计', status: 'ready' },
        ],
      },
    ],
  },
  agents: {
    kicker: 'AGENT NETWORK',
    title: '代理体系',
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
    description: '统一查看销售团队、终端归属、业绩和提成关系。',
    hubTo: '/sales',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '销',
    groups: [{
      title: '销售经营',
      description: '销售人员、终端归属和业绩提成。',
      entries: [
        { title: '销售团队', description: '查看销售业务账号并进入内部员工体系维护。', icon: '销', to: '/sales/team', badge: '已上线', status: 'ready' },
        { title: '终端归属', description: '查看销售与终端的当前关系和历史来源。', icon: '归', to: '/customers/list?focus=attribution', badge: '归属', status: 'ready' },
        { title: '业绩与提成', description: '按订单和规则版本追踪业绩、提成与冲回。', icon: '绩', to: '/sales/performance', badge: '提成', status: 'ready' },
      ],
    }],
  },
  commercial: {
    kicker: 'COMMERCIAL CENTER',
    title: '商品与会员',
    description: '会员、时长卡、实体设备、推荐奖励和结算规则分开配置并全部版本化。',
    hubTo: '/commercial/memberships',
    parentTitle: '系统',
    parentTo: '/overview',
    heroIcon: '会',
    groups: [
      {
        title: '商品配置',
        description: '定义终端可购买的标准化商品与权益。',
        entries: [
          { title: '会员方案', description: '维护会员价格、权益、有效期和版本发布。', icon: '会', to: '/commercial/memberships/plans', badge: '已上线', status: 'ready' },
          { title: '规则模拟器', description: '用测试用户预览购买和时长规则，不真实入账。', icon: '测', to: '/commercial/memberships/simulator', badge: '沙盒', status: 'ready' },
          { title: '时长卡', description: '独立管理算力/时长卡价格、时长和渠道策略。', icon: '时', to: '/commercial/time-cards', badge: '商品', status: 'ready' },
          { title: '设备商品', description: '配置实体设备 SKU、原价、售价和库存映射，发布后进入终端商城。', icon: '设', to: '/commercial/device-products', badge: '实物', status: 'ready' },
        ],
      },
      {
        title: '奖励与结算',
        description: '奖励和佣金规则独立版本化。',
        entries: [
          { title: '一级推荐奖励', description: '配置触发点、奖励金额和退款冲回规则。', icon: '奖', to: '/commercial/referrals', badge: '奖励', status: 'ready' },
          { title: '结算规则', description: '配置代理返佣、销售提成和阶梯结算。', icon: '结', to: '/commercial/settlement', badge: '结算', status: 'ready' },
        ],
      },
    ],
  },
  finance: {
    kicker: 'FINANCE CENTER',
    title: '财务结算',
    description: '客户资金、经营收支、AI 时长、审批、账务流水和追溯职责分离。',
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
          { title: 'AI 时长', description: '查看并按权限调整代理和终端 AI 时长，全部变动保留流水。', icon: '时', to: '/staff/finance/ai-time', badge: '时长', status: 'ready' },
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
          { title: '收益结算', description: '查看收益明细并完成结算批次生成、审核和支付。', icon: '结', to: '/staff/finance/settlements', badge: '结算', status: 'ready' },
        ],
      },
    ],
  },
  resources: {
    kicker: 'WAREHOUSE & STOCK',
    title: '设备库存',
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
          { title: '物流管理', description: '管理快递和直接领取，关联具体出库对象、运单和设备 SN。', icon: '物', to: '/resources/logistics', badge: '物流', status: 'ready' },
        ],
      },
    ],
  },
}

export function moduleEntries(hub: HubKey) {
  return moduleUiMap[hub].groups.flatMap((group) => group.entries)
}
