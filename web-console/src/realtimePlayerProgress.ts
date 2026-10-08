export type PreparationPart = 'session' | 'text' | 'voice' | 'download'
const ranges: Record<PreparationPart, [number, number]> = { session: [0, 15], text: [15, 40], voice: [40, 95], download: [95, 99] }

// Work milestones, not a clock. Only the browser's actual playing event may
// complete initial preparation; estimates of pending text never count as audio.
export function preparationProgress(previous: number, part: PreparationPart, percent: number): number {
  if (!Number.isFinite(percent)) return previous
  const [start, end] = ranges[part]
  return Math.max(previous, Math.min(99, start + (end - start) * Math.max(0, Math.min(100, percent)) / 100))
}
export function playbackPercent(elapsedMS: number, totalMS: number): number {
  return totalMS > 0 ? Math.max(0, Math.min(100, elapsedMS / totalMS * 100)) : 0
}
export function playbackTime(ms: number): string {
  const seconds = Math.floor(Math.max(0, ms) / 1000)
  return `${Math.floor(seconds / 60).toString().padStart(2, '0')}:${(seconds % 60).toString().padStart(2, '0')}`
}
