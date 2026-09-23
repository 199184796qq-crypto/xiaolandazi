import type { HubKey } from './moduleUi'

export type FeatureFieldType =
  | 'text'
  | 'number'
  | 'date'
  | 'checkbox'
  | 'select'
  | 'textarea'

export interface FeatureFieldOption {
  label: string
  value: string
}

export interface FeatureField {
  key: string
  label: string
  type: FeatureFieldType
  placeholder?: string
  unit?: string
  options?: FeatureFieldOption[]
  required?: boolean
}

export interface FeatureWorkspaceConfig {
  featureKey: string
  hub: HubKey
  title: string
  kicker: string
  description: string
  recordLabel: string
  codeLabel: string
  managePermission: string
  fields: FeatureField[]
}

export const featureWorkspaceConfigs: Record<string, FeatureWorkspaceConfig> = {
  'agent-levels': {
    featureKey: 'agent-levels',
    hub: 'agents',
    title: '代理等级',
    kicker: 'AGENT LEVELS',
    description: '配置代理准入门槛、设备政策、返佣比例和准备金要求。',
    recordLabel: '等级名称',
    codeLabel: '等级编码',
    managePermission: 'system.architecture.view',
    fields: [
      { key: 'threshold_yuan', label: '准入门槛', type: 'number', unit: '元', required: true },
      { key: 'included_devices', label: '首批设备', type: 'number', unit: '台' },
      { key: 'device_discount_percent', label: '设备拿货折扣', type: 'number', unit: '%' },
      { key: 'settlement_rate_percent', label: '销售资金返还比例', type: 'number', unit: '%' },
      { key: 'deposit_rate_percent', label: '准备金比例', type: 'number', unit: '%' },
      { key: 'note', label: '等级说明', type: 'textarea', placeholder: '适用范围、特殊约定等' },
    ],
  },
  'agent-contracts': {
    featureKey: 'agent-contracts',
    hub: 'agents',
    title: '代理合同',
    kicker: 'AGENT CONTRACTS',
    description: '管理代理签约、续签、补充协议和合同周期。',
    recordLabel: '合同名称',
    codeLabel: '合同编号',
    managePermission: 'system.architecture.view',
    fields: [
      { key: 'agent_name', label: '代理名称', type: 'text', required: true },
      { key: 'level_name', label: '代理等级', type: 'text' },
      { key: 'starts_on', label: '生效日期', type: 'date', required: true },
      { key: 'ends_on', label: '到期日期', type: 'date' },
      { key: 'contract_amount_yuan', label: '合同金额', type: 'number', unit: '元' },
      { key: 'note', label: '合同备注', type: 'textarea' },
    ],
  },
  'agent-exit': {
    featureKey: 'agent-exit',
    hub: 'agents',
    title: '退出清算',
    kicker: 'AGENT EXIT',
    description: '自动检查库存、在途、售后、收益结算和资金余额，满足退出条件后关闭代理关系。',
    recordLabel: '退出清算单',
    codeLabel: '清算编号',
    managePermission: 'system.architecture.view',
    fields: [
      { key: 'agent_name', label: '代理名称', type: 'text', required: true },
      { key: 'note', label: '清算说明', type: 'textarea' },
    ],
  },
}
