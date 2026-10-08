import type { AnchorAnalysisSection } from './types'
import { parseAnchorStyleDisplay } from './anchorStyleDisplay.ts'

export const anchorSampleLimit = 10000
export function limitAnchorSample(text: string) {
  const chars = Array.from(text)
  return { text: chars.slice(0, anchorSampleLimit).join(''), truncated: chars.length > anchorSampleLimit }
}
export function editableAnchorSections(text: string): AnchorAnalysisSection[] {
  return parseAnchorStyleDisplay(text).map(section => ({ title: section.title || '补充说明', points: section.points.map(point => point.text.trim()) }))
}
export function canFuseAnchorSamples(samples: Array<{ analysis_status: string }>) {
  return samples.length > 1 && samples.every(sample => sample.analysis_status === 'analyzed')
}
