import type { LiveAgentPlanVersion } from './types'

// Old real-time versions legitimately have no pre-generated manuscripts and
// encoded that empty list as null. Do not coerce other malformed data or edit
// any actual manuscript, voice asset, or saved configuration.
export function normalizeLiveAgentPlanVersion(version: LiveAgentPlanVersion): LiveAgentPlanVersion {
  if (version.variants == null) return { ...version, variants: [] }
  if (!Array.isArray(version.variants) || version.variants.some(item => item == null || typeof item !== 'object')) {
    throw new Error('方案版本的文稿列表格式异常，请联系后台；当前方案没有被修改')
  }
  return version
}
