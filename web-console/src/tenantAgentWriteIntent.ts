const WRITE_VERBS = [
  '加一条',
  '加上',
  '加进',
  '新增',
  '添加',
  '修改',
  '改',
  '改成',
  '删除',
  '删掉',
  '停用',
  '保存',
  '换方案',
  '切换方案',
  '切换',
  '绑定',
  '解绑',
]

const WRITE_OBJECTS = [
  '事实',
  '依据',
  '福利',
  '活动',
  '优惠',
  '商品',
  '链接',
  '话术',
  '方案',
  '卖点',
  '包邮',
  '价格',
  '地址',
  '赠品',
  '库存',
]

/**
 * 判断一条租户消息是否值得进入“维护写”链路。
 * 只做便宜的关键词预判；真正的意图和字段仍由后端 intent 解析。
 */
export function isTenantMaintenanceWriteIntent(value: string): boolean {
  const text = value.replace(/\s+/g, '')
  if (!text) return false
  return (
    WRITE_VERBS.some((verb) => text.includes(verb)) &&
    WRITE_OBJECTS.some((object) => text.includes(object))
  )
}
