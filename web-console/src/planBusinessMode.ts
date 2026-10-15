export type PlanBusinessMode = 'ecommerce' | 'local_service'
export type PlanBusinessIndustryOption = { code: string; label: string }
export type PlanBusinessSettings = {
  biz_domain: PlanBusinessMode
  industries: Record<PlanBusinessMode, string>
  industry_options: Record<PlanBusinessMode, PlanBusinessIndustryOption[]>
}

export const planBusinessModes: { value: PlanBusinessMode; label: string; description: string }[] = [
  { value: 'ecommerce', label: '电商带货', description: '商品销售与线上下单' },
  { value: 'local_service', label: '本地生活', description: '门店服务与到店消费' },
]

export function isPlanBusinessMode(value: unknown): value is PlanBusinessMode {
  return value === 'ecommerce' || value === 'local_service'
}

export function validatePlanBusinessSettings(settings: PlanBusinessSettings): void {
  if (!isPlanBusinessMode(settings.biz_domain)) throw new Error('业务模式数据异常，请重新读取')
  for (const mode of ['ecommerce', 'local_service'] as const) {
    const options = settings.industry_options?.[mode]
    if (!Array.isArray(options) || !options.length || options.some(item => typeof item.code !== 'string' || !item.code || typeof item.label !== 'string' || !item.label) || !options.some(item => item.code === settings.industries?.[mode])) {
      throw new Error('行业数据异常，请重新读取')
    }
  }
}
