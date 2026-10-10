import type { AgentMemoryItem } from './types'

export type AnswerStrategyCategory = 'logistics' | 'product' | 'audience' | 'children' | 'safety' | 'timing' | 'after_sales' | 'consultation' | 'language' | 'price' | 'benefits' | 'general'
export const answerStrategyCategoryLabels: Record<AnswerStrategyCategory, string> = {
  logistics: '物流相关', product: '产品相关', audience: '人群相关', children: '儿童相关',
  safety: '安全相关', timing: '时效相关', after_sales: '售后保障', consultation: '咨询相关',
  language: '术语理解', price: '价格相关', benefits: '福利活动', general: '通用策略',
}
// Visual categories only. Matching never changes the saved strategy or calls a model.
const patterns: Array<[AnswerStrategyCategory, RegExp]> = [
  ['children', /儿童|小孩|宝宝|婴幼儿|婴儿|child|baby/],
  ['after_sales', /售后|退货|退款|退换|换货|保修|赔付|售后保障|after.?sale|refund|warranty/],
  ['safety', /安全|过敏|禁忌|忌口|风险|食用禁|safety|allerg/],
  ['timing', /时效|发货时间|到货时间|指定日期|到货日期|发货日期|次日达|有效期|保质期|时限|delivery_date|timing|expiry/],
  ['audience', /人群|适合谁|适用对象|孕妇|老人|老年|消费者|audience|customer_group/],
  ['language', /术语|方言|问法|语境|term:|dialect|language|semantic:/],
  ['price', /价格|多少钱|算账|折扣|售价|报价|price|pricing/],
  ['benefits', /赠品|福利|活动|赠送|优惠券|benefit|gift|promotion/],
  ['logistics', /物流|快递|运费|包邮|发货|配送|包装|真空|shipping|delivery|logistic|packaging/],
  ['product', /产品|商品|规格|口味|切块|肉质|工艺|原料|product|cutting|flavor/],
  ['consultation', /咨询|询问|问答|客服|consult|question|inquiry/],
]
function match(text: string) { return patterns.find(([, pattern]) => pattern.test(text))?.[0] }
export function answerStrategyCategory(item: AgentMemoryItem): AnswerStrategyCategory {
  return match([item.target, item.memory_key].filter(Boolean).join(' ').toLowerCase())
    || match((item.current_version?.content_text || '').toLowerCase()) || 'general'
}
