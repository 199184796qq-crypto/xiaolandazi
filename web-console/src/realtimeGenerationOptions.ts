export interface RealtimeGenerationOptions {
  buffer_seconds: number
  body_min: number
  body_max: number
  max_chars: number
}

export function realtimeGenerationOptionsError(options: RealtimeGenerationOptions | null): string {
  if (!options) return '正在读取实时声音参数，请稍后再试'
  if (!Number.isInteger(options.buffer_seconds) || options.buffer_seconds < 20 || options.buffer_seconds > 600) return '正式主线缓存须为20～600秒的整数'
  if (!Number.isInteger(options.body_min) || !Number.isInteger(options.body_max) || options.body_min < 2 || options.body_max > 6 || options.body_max < options.body_min) return '中间讲解最少/最多段数须为2～6的整数，且最少不能超过最多'
  if (!Number.isInteger(options.max_chars) || options.max_chars < 80 || options.max_chars > 1000) return '单段最大字数须为80～1000的整数'
  return ''
}

// The versioned server snapshot is authoritative on restore. Older versions
// already contain these values, even before an editable options object existed.
export function realtimeOptionsFromVersion(context: Record<string, unknown> | undefined): RealtimeGenerationOptions | null {
  const scheme = context?.anchor_generation as { buffer_seconds?: number; config?: { body_min?: number; body_max?: number; max_chars?: number } } | undefined
  if (!scheme) return null
  const options = { buffer_seconds: scheme.buffer_seconds!, body_min: scheme.config?.body_min!, body_max: scheme.config?.body_max!, max_chars: scheme.config?.max_chars! }
  return realtimeGenerationOptionsError(options) ? null : options
}

export function validatedRealtimeOptions(options: RealtimeGenerationOptions | null): RealtimeGenerationOptions {
  const error = realtimeGenerationOptionsError(options)
  if (error) throw new Error(error)
  return { buffer_seconds: options!.buffer_seconds, body_min: options!.body_min, body_max: options!.body_max, max_chars: options!.max_chars }
}
