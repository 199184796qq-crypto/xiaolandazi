export type PlanBusinessMode = 'ecommerce' | 'local_service'

export const planBusinessModes: { value: PlanBusinessMode; label: string; description: string }[] = [
  { value: 'ecommerce', label: '电商带货', description: '商品销售与线上下单' },
  { value: 'local_service', label: '本地生活', description: '门店服务与到店消费' },
]

export function isPlanBusinessMode(value: unknown): value is PlanBusinessMode {
  return value === 'ecommerce' || value === 'local_service'
}
