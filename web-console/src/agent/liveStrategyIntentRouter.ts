import type { SystemAgentActionPreview } from '../types'

export type LiveStrategyMode =
  | 'basic'
  | 'strategy'
  | 'script'
  | 'products'
  | 'benefits'
  | 'knowledge'
  | 'rhythm'
  | 'memory'
  | 'anchor'
  | 'voice'
  | 'fullshow'
  | 'plan'

export type LiveProductLinkAction = 'add' | 'update' | 'delete' | ''

export type LiveStrategyIntentOption = NonNullable<
  SystemAgentActionPreview['payload']['intent_options']
>[number]

function compactText(value: string) {
  return String(value || '').replace(/[\s，,。.!！?？]/g, '')
}

export function liveProductLinkKeyFromText(value: string) {
  const patterns = [
    /(\d+)\s*号?\s*(?:商品)?链接/,
    /(?:商品)?链接\s*(\d+)\s*号?/,
    /(\d+)\s*#\s*(?:商品)?链接/,
  ]
  for (const pattern of patterns) {
    const matched = value.match(pattern)
    if (matched?.[1]) return matched[1] + '号链接'
  }
  return ''
}

export function liveProductField(value: string, labels: string[]) {
  const labelPattern = labels
    .map((label) => label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    .join('|')
  const matched = value.match(
    new RegExp('(?:^|[\\n\\r\\s])(?:' + labelPattern + ')\\s*[:：]?\\s*([^，,；;\\n\\r]+)', 'im'),
  )
  return matched?.[1]?.trim() || ''
}

export function liveProductNameFromCommand(value: string) {
  const explicit = value.match(/商品(?:名称|名)\s*[:：]?\s*([^，,；;\n]+)/)
  if (explicit?.[1]) return explicit[1].trim()
  const linkMatch = value.match(/\d+\s*号?\s*(?:商品)?链接/)
  if (!linkMatch?.index && linkMatch?.index !== 0) return ''
  let remainder = value.slice((linkMatch.index || 0) + linkMatch[0].length).trim()
  remainder = remainder.replace(/^(?:改成|改为|修改为|设置为|设为)\s*/, '')
  const fieldIndex = remainder.search(
    /(?:规格|日常价|原价|价格|数量|适用人群|适用对象|适用)\s*[:：]/,
  )
  if (fieldIndex >= 0) remainder = remainder.slice(0, fieldIndex).trim()
  remainder = remainder.replace(
    /(?:^|[\s，,、])\d+(?:\.\d+)?\s*(?:ml|mL|ML|l|L|毫升|升|kg|Kg|KG|g|G|千克|公斤|克|斤)\s*$/,
    '',
  )
  return remainder.replace(/^[：:，,、\s]+|[。；;，,\s]+$/g, '').trim()
}

export function liveProductImplicitSpecFromCommand(value: string) {
  const linkMatch = value.match(/\d+\s*号?\s*(?:商品)?链接/)
  if (!linkMatch?.index && linkMatch?.index !== 0) return ''
  const remainder = value.slice((linkMatch.index || 0) + linkMatch[0].length).trim()
  const matched = remainder.match(
    /(?:^|[\s，,、])(\d+(?:\.\d+)?\s*(?:ml|mL|ML|l|L|毫升|升|kg|Kg|KG|g|G|千克|公斤|克|斤))\s*$/,
  )
  return matched?.[1]?.trim() || ''
}

export function liveProductUpdateValues(value: string) {
  return {
    productName: liveProductNameFromCommand(value),
    spec: liveProductField(value, ['规格']) || liveProductImplicitSpecFromCommand(value),
    dailyPrice: liveProductField(value, ['日常价', '原价']),
    quantity: liveProductField(value, ['数量']),
    audience: liveProductField(value, ['适用人群', '适用对象', '适用']),
  }
}

export function liveProductLinkActionFromText(value: string): LiveProductLinkAction {
  const compact = String(value || '').replace(/\s+/g, '')
  if (/(?:删除|移除|停用|下掉|取消).*(?:链接)|(?:链接).*(?:删除|移除|停用|下掉|取消)/.test(compact)) {
    return 'delete'
  }
  if (/(?:修改|调整|更新|改成|改为|设为).*(?:链接)|(?:链接).*(?:修改|调整|更新|改成|改为|设为)/.test(compact)) {
    return 'update'
  }
  if (/(?:添加|新增|增加|新建|录入).*(?:链接)|(?:链接).*(?:添加|新增|增加|新建|录入)/.test(compact)) {
    return 'add'
  }
  return ''
}

export function liveProductLinkTypoCorrection(value: string) {
  if (!/(?:商品)?连接/.test(value)) return ''
  const corrected = value.replace(/商品连接/g, '商品链接').replace(/连接/g, '链接')
  if (!liveProductLinkActionFromText(corrected) || !liveProductLinkKeyFromText(corrected)) return ''
  return corrected
}

function formatLocalDateTime(value: Date) {
  const pad = (part: number) => String(part).padStart(2, '0')
  return value.getFullYear() + '-' +
    pad(value.getMonth() + 1) + '-' +
    pad(value.getDate()) + ' ' +
    pad(value.getHours()) + ':' +
    pad(value.getMinutes())
}

export function normalizeBenefitTimeValue(value: string, now = new Date()) {
  const trimmed = String(value || '').trim()
  if (!trimmed || /^(?:无|未设置|留空|待确认|暂无|-)$/.test(trimmed)) return ''
  if (/^(?:现在|立即|马上|即刻|当前时间|此刻)$/.test(trimmed)) {
    return formatLocalDateTime(now)
  }
  const direct = trimmed.match(/\d{4}-\d{1,2}-\d{1,2}(?:[ T]\d{1,2}:\d{2}(?::\d{2})?)?/)
  if (!direct?.[0]) return ''
  const normalized = direct[0].replace('T', ' ')
  const parts = normalized.split(' ')
  const date = parts[0].split('-').map((item) => item.padStart(2, '0'))
  if (date.length !== 3) return ''
  const time = parts[1] || ''
  return date[0] + '-' + date[1] + '-' + date[2] + (time ? ' ' + time : '')
}

export function liveBenefitUpdateValues(value: string) {
  const read = (labels: string[]) => {
    const labelPattern = labels.map((item) => item.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|')
    const matched = value.match(
      new RegExp('(?:' + labelPattern + ')\\s*(?:(?:改成|改为|修改为|设置为|设为|调整为|改到|调整到)\\s*|[:：]\\s*|\\s+)([^，,；;\\n\\r]+)', 'i'),
    )
    return matched?.[1]?.trim() || ''
  }
  const productName = read(['商品名称', '商品名'])
  const activityPrice = read(['活动价', '优惠价', '秒杀价', '到手价'])
  const gift = read(['赠品内容', '赠品', '福利'])
  const activity = read(['活动内容', '活动规则'])
  let startsAtRaw = read(['开始时间', '生效时间'])
  const endsAtRaw = read(['结束时间', '失效时间', '截止时间'])
  if (!startsAtRaw && /(?:从)?(?:现在|立即|马上|即刻)(?:开始|起)?|立即生效|马上生效/.test(value)) {
    startsAtRaw = '现在'
  }
  return {
    productName,
    activityPrice,
    gift,
    activity,
    startsAt: normalizeBenefitTimeValue(startsAtRaw),
    endsAt: normalizeBenefitTimeValue(endsAtRaw),
  }
}

export function isLiveBenefitUpdateIntent(value: string) {
  return /(?:修改|改成|改为|调整|更新|变更)/.test(value)
}

export function isExplicitLiveBenefitCommand(value: string) {
  const raw = String(value || '').trim()
  const compact = raw.replace(/\s+/g, '')
  if (!raw) return false

  const hasBenefitContext = /活动|福利|优惠|赠品|满减|秒杀|折扣|券/.test(compact)
  const hasBenefitField = /(?:活动价|优惠价|秒杀价|到手价|赠品|活动内容|活动规则|开始时间|生效时间|结束时间|失效时间|截止时间)/.test(compact)
  const hasRelativeTime = /(?:现在|立即|马上|即刻|当前时间|今晚|今天|明天|后天|月底|月末|立即生效|马上生效)/.test(compact)

  if (hasBenefitContext && (hasBenefitField || hasRelativeTime)) return true
  if (liveProductLinkKeyFromText(raw) && hasBenefitField) return true
  return false
}

export function isExplicitPendingBenefitUpdateIntent(value: string) {
  const compact = String(value || '').replace(/\s+/g, '')
  return /(?:上方|刚才|这个|当前)?(?:活动福利)?候选|未保存(?:的)?活动|还没保存(?:的)?活动|这张候选卡|当前候选/.test(compact)
}

export function isLiveStrategyCancelIntent(value: string) {
  const compact = compactText(value)
  return /^(?:取消|不用了|不用|算了|先算了|不弄了|不改了|先不改|先不弄|不要了|停一下|先这样|到此为止)$/.test(compact)
}

export function isLiveStrategyConfirmIntent(value: string) {
  const compact = compactText(value)
  return /^(?:确认|确认执行|确定|执行|可以|好的|好|对|是的|就这样|就按这个|按这个|采用|采纳)$/.test(compact)
}

export function isLiveProductCorrectionAccept(value: string) {
  const compact = compactText(value)
  return /^(?:采纳|采用|确认|确定|可以|好的|好|对|是的|就按这个|按这个|这样改|改吧)$/.test(compact)
}

export function isLiveProductCorrectionCancel(value: string) {
  const compact = compactText(value)
  return /^(?:取消|不采纳|不采用|不确认|不对|不是|不要|别改|不要这样改|先不改)$/.test(compact)
}

export function isLiveImageProductAccept(value: string) {
  const compact = compactText(value)
  return /^(?:添加|加入|写入|录入|保存|采纳|采用|确认|确定|可以|好的|好|对|是的|用这个|就这个|就这样)$/.test(compact)
}

export function isLiveImageProductCancel(value: string) {
  const compact = compactText(value)
  return /^(?:取消|不添加|别添加|不要添加|不写入|不保存|不用这个|不是这个|先不加|先不添加)$/.test(compact)
}

export function isLiveBenefitAccept(value: string) {
  const compact = compactText(value)
  return /^(?:添加|保存|写入|录入|采纳|采用|确认|确定|可以|好的|好|对|是的|就这个|就这样|用这个)$/.test(compact)
}

export function isLiveBenefitCancel(value: string) {
  const compact = compactText(value)
  return /^(?:取消|不添加|别添加|不要添加|不保存|别保存|不写入|不用这个|先不加|先不保存)$/.test(compact)
}

export function shouldRouteLiveStrategyModuleCommand(
  value: string,
  currentMode: LiveStrategyMode,
) {
  const raw = String(value || '').trim()
  const compact = raw.replace(/\s+/g, '')
  if (!raw) return false

  if (currentMode === 'products') {
    if (liveProductLinkActionFromText(raw)) return true
    if (/(?:商品名称|商品名|规格|日常价|原价|数量|适用人群|适用对象)\s*[:：]/.test(raw)) {
      return true
    }
    if (liveProductLinkKeyFromText(raw) && /(?:商品|规格|价格|日常价|原价|数量|适用)/.test(compact)) {
      return true
    }
    return false
  }

  if (currentMode === 'benefits') {
    if (isLiveBenefitUpdateIntent(raw)) return true
    if (/(?:添加|新增|增加|新建|录入|设置|保存|写入).*(?:活动|福利|优惠|赠品)/.test(compact)) {
      return true
    }
    if (/(?:活动价|优惠价|秒杀价|到手价|赠品|活动内容|活动规则|开始时间|生效时间|结束时间|失效时间|截止时间)\s*[:：]/.test(raw)) {
      return true
    }
    if (
      liveProductLinkKeyFromText(raw) &&
      /(?:买.+送|赠送|赠品|活动价|优惠|福利|满减|秒杀|截止|结束|到\d{4}-\d{1,2}-\d{1,2})/.test(compact)
    ) {
      return true
    }
    return false
  }

  return true
}

function liveStrategyIntentOption(
  id: string,
  label: string,
  description: string,
  mode: LiveStrategyMode,
  command = '',
): LiveStrategyIntentOption {
  return { id, label, description, mode, command }
}

export function liveStrategyIntentClarificationOptions(
  value: string,
  hasImages: boolean,
  currentMode: LiveStrategyMode,
) {
  const compact = String(value || '').replace(/\s+/g, '')
  const explicitBenefitContext = /活动|优惠|福利|折扣|赠品|满减|券|秒杀/.test(compact)
  if (isExplicitLiveBenefitCommand(value) && !hasImages) return []
  if (currentMode === 'benefits' && explicitBenefitContext && !hasImages) return []

  const options: LiveStrategyIntentOption[] = []
  const push = (option: LiveStrategyIntentOption) => {
    if (!options.some((item) => item.id === option.id)) options.push(option)
  }

  if (/(?:商品)?链接|商品|规格|价格|几号链接|链接号/.test(compact)) {
    push(liveStrategyIntentOption('products', '商品链接', '按商品链接资料处理；真正写入前仍会再次确认。', 'products'))
  }
  if (/事实|依据|资料|信息|产地|发货|快递|物流|保质期|资质|库存/.test(compact)) {
    push(liveStrategyIntentOption('knowledge', '事实依据', '作为可核对的事实资料整理，不自动当成话术。', 'knowledge'))
  }
  if (/话术|口播|怎么说|如何说|介绍|开场|逼单|参考说法|话术参考/.test(compact)) {
    push(liveStrategyIntentOption('script', '话术参考', '作为主播怎么说的参考，不自动升级为商品事实。', 'script'))
  }
  if (explicitBenefitContext) {
    push(liveStrategyIntentOption('benefits', '活动福利', '作为活动/优惠信息处理，仍需核对时效和适用范围。', 'benefits'))
  }
  if (/方案|绑定|解绑|取消绑定|切换方案|使用方案|当前方案/.test(compact)) {
    push(liveStrategyIntentOption('plan', '方案绑定 / 切换', '处理直播间与方案的绑定关系或当前运行方案。', 'plan'))
  }

  if (hasImages) {
    if (!options.some((item) => item.id === 'products')) {
      push(liveStrategyIntentOption('products', '商品链接', '把图片内容用于商品链接资料。', 'products'))
    }
    if (!options.some((item) => item.id === 'knowledge')) {
      push(liveStrategyIntentOption('knowledge', '事实依据', '把图片中可见信息作为事实候选。', 'knowledge'))
    }
    if (!options.some((item) => item.id === 'script')) {
      push(liveStrategyIntentOption('script', '话术参考', '只用于生成或调整主播话术参考。', 'script'))
    }
    push(liveStrategyIntentOption('inspect-only', '只识别图片，不写入', '只告诉你图片里有什么，不修改当前方案。', currentMode))
    return options.slice(0, 5)
  }

  if (options.length >= 2) return options.slice(0, 5)
  if (options.length === 1) return []

  const looksLikeGenericMutation = /(?:添加|加进去|放进去|放到|改一下|修改|调整|更新|删除|去掉|用这个|采用这个|保存|录入|处理一下)/.test(compact)
  if (!looksLikeGenericMutation || currentMode !== 'plan') return []

  return [
    liveStrategyIntentOption('products', '商品链接', '维护几号链接、商品名、规格或价格。', 'products'),
    liveStrategyIntentOption('knowledge', '事实依据', '保存可核对的商品/直播事实。', 'knowledge'),
    liveStrategyIntentOption('benefits', '活动福利', '维护优惠、赠品、限时活动等。', 'benefits'),
    liveStrategyIntentOption('script', '话术参考', '作为主播表达和参考说法。', 'script'),
    liveStrategyIntentOption('plan', '方案绑定 / 切换', '调整直播间和方案关系。', 'plan'),
  ]
}

export function buildLiveStrategyIntentClarification(
  value: string,
  roomId: number,
  planId: number,
  currentMode: LiveStrategyMode,
  imageLabels: string[] = [],
): SystemAgentActionPreview | undefined {
  if (!planId || !roomId || value.includes('【已确认意图】')) return undefined
  const options = liveStrategyIntentClarificationOptions(value, imageLabels.length > 0, currentMode)
  if (options.length < 2) return undefined
  return {
    type: 'clarify_live_strategy_intent',
    title: '确认你想做什么',
    summary: '这句话可能对应多个功能。先选一个真实意图，我再继续；选择前不会写入数据库。',
    risk_level: 'low',
    requires_confirmation: true,
    payload: {
      plan_id: planId,
      room_id: roomId,
      original_message: value,
      image_labels: imageLabels,
      intent_options: options,
    },
  }
}

