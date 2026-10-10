import type { AgentLearningSession } from './types'

export type InteractionSuggestionTopic = 'price' | 'shipping' | 'after_sales' | 'gift' | 'other'
export const interactionTopicFilters = [
  { key: 'price', label: '价格' }, { key: 'shipping', label: '物流' },
  { key: 'after_sales', label: '售后' }, { key: 'gift', label: '赠品' }, { key: 'all', label: '全部内容' },
] as const
export const interactionTopicLabels: Record<InteractionSuggestionTopic, string> = {
  price: '价格相关', shipping: '物流相关', after_sales: '售后相关', gift: '赠品相关', other: '其他问题',
}
// Presentation tags only: questions take priority over supporting model text.
const patterns: Array<[InteractionSuggestionTopic, RegExp]> = [
  ['after_sales', /售后|退货|退款|退换|换货|不满意|保修|质量问题|破损|赔付/],
  ['gift', /赠品|赠送|赠礼|赠什么|送什么|送啥|有送|附送/],
  ['price', /价格|多少钱|多少块|几块钱|售价|报价|折扣|优惠价|贵不贵|便宜/],
  ['shipping', /物流|配送|快递|发货|到货|多久到|几天到|包邮|运费|邮费|送达|截单|包装|礼盒|盒子|包裹|打包|真空|抽气/],
]
function matchTopic(text: string): InteractionSuggestionTopic | undefined {
  return patterns.find(([, pattern]) => pattern.test(text))?.[0]
}
export function interactionSuggestionTopic(item: AgentLearningSession): InteractionSuggestionTopic {
  return matchTopic(item.question || '') || matchTopic([
    item.review?.meaning, item.review?.context, ...(item.review?.needed_facts || []), item.review?.reason,
  ].filter(Boolean).join('；')) || 'other'
}
