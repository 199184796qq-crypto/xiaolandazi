import type { PoolStatus, PoolTask } from './contentPools'
import type { AnchorStyleSellingPointWheelItem } from './types'

export interface MinerPoolPoint extends AnchorStyleSellingPointWheelItem {
  poolKey: string
  poolLabel: string
  taskIds: string[]
}

export function minerPoolPoints(status: PoolStatus | null | undefined, scope: string): MinerPoolPoint[] {
  const result: MinerPoolPoint[] = []
  for (const pool of status?.pools || []) {
    if (scope === 'common' ? pool.owner !== 0 : scope !== `product:${pool.owner}` || pool.owner <= 0) continue
    const groups = new Map<string, MinerPoolPoint>()
    for (const task of pool.tasks || []) {
      const key = JSON.stringify([task.selling_point, task.pain_point || '', task.target_audience || ''])
      let point = groups.get(key)
      if (!point) {
        point = { 卖点名: task.selling_point, 痛点: task.pain_point || '', 目标人群: task.target_audience || '', 角度: [], poolKey: pool.key, poolLabel: pool.label, taskIds: [] }
        groups.set(key, point)
        result.push(point)
      }
      point.角度.push(task.angle)
      point.taskIds.push(task.id)
    }
  }
  return result
}

export function minerPointEditTasks(point: { name: string; painPoint: string; targetAudience: string; angles: string[]; angleTaskIds: string[] }): Pick<PoolTask, 'id' | 'selling_point' | 'angle' | 'pain_point' | 'target_audience'>[] {
  return point.angles.map((angle, index) => ({ id: point.angleTaskIds[index] || '', selling_point: point.name.trim(), angle: angle.trim(), pain_point: point.painPoint.trim(), target_audience: point.targetAudience.trim() }))
}
