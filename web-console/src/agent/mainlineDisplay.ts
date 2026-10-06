// Display wording only: keep the scheduler's machine-readable keys unchanged.
const liveTypeLabels: Record<string, string> = {
  commerce: '电商带货', ecommerce: '电商带货', education: '知识讲解',
  performance: '才艺表演', conversation: '聊天交流', chat: '聊天交流',
}
const industryLabels: Record<string, string> = {
  general: '通用行业', apparel: '服装鞋帽', food: '食品生鲜', durable_goods: '家电数码',
}
const roleLabels: Record<string, string> = {
  orient: '引入话题', value: '讲清价值', scenario: '展开使用场景',
  evidence: '提供可信依据', difference: '解释差异', decision: '帮助选择',
  action: '引导下单', recap_bridge: '回顾重点并自然过渡',
}
const productRoomRoleLabels: Record<string, string> = {
  main: '主推', traffic: '引流', benefit: '福利', profit: '利润', bundle: '搭配', ordinary: '普通',
}
const productEmphasisLabels: Record<string, string> = { high: '重点讲', normal: '正常讲', light: '适时带到' }
const productRevisitLabels: Record<string, string> = { frequent: '经常返场', frequent_short: '短频返场', timely: '按活动时机提醒', related: '相关场景带到', adaptive: '动态安排' }

function displayLabel(value: string, labels: Record<string, string>, fallback: string): string {
  const key = value.trim().toLowerCase()
  // User-defined Chinese names remain readable; never expose an unknown code.
  return labels[key] || (/^[\p{Script=Han}\s、与和]+$/u.test(value.trim()) ? value.trim() : fallback)
}

export function mainlineLiveTypeLabel(value: string): string {
  return displayLabel(value, liveTypeLabels, '自定义直播')
}

export function mainlineIndustryLabel(value: string): string {
  return displayLabel(value, industryLabels, '自定义行业')
}

export function mainlineRoleSequenceLabel(roles: string[], liveType: string): string {
  const isCommerce = ['commerce', 'ecommerce'].includes(liveType.trim().toLowerCase())
  return roles.map((role) => {
    const key = role.trim().toLowerCase()
    if (!isCommerce) {
      if (key === 'action') return '引导参与'
      if (key === 'scenario') return '展开具体情境'
      if (key === 'decision') return '帮助判断'
    }
    return displayLabel(role, roleLabels, '继续展开话题')
  }).join(' → ')
}

export function productRoomRolesLabel(roles: string[]): string {
  return roles.length ? roles.map((role) => productRoomRoleLabels[role] || role).join('＋') : '未设置'
}

export function productPlanDirectionLabel(emphasis: string, revisit: string): string {
  return (productEmphasisLabels[emphasis] || '动态讲解') + ' · ' + (productRevisitLabels[revisit] || '动态安排')
}
