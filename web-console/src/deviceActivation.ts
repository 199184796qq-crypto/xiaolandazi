interface ActivationDevice {
  hardware_mac?: string
  quality_status: string
  lifecycle_status: string
  claim_enabled?: boolean
}

export function deviceActivationState(device: ActivationDevice) {
  if (!device.hardware_mac) return { activated: false, allowed: false, reason: '未登记 MAC，请在入库时登记设备 MAC' }
  if (device.quality_status !== 'qualified') return { activated: false, allowed: false, reason: '只有质检合格的设备才能允许激活' }
  const delivered = ['SOLD', 'CUSTOMER_BOUND', 'ACTIVE'].includes(device.lifecycle_status)
  if (!delivered && device.lifecycle_status !== 'IN_STOCK') return { activated: false, allowed: false, reason: '当前库存状态不能激活，请先完成入库或售后处理' }
  return {
    activated: delivered || Boolean(device.claim_enabled),
    allowed: true,
    reason: '已允许生成绑定码，用户仍需添加设备并绑定直播间',
  }
}
