export interface SpeechModelProfile {
  id: string; name: string; protocol: string; base_url: string; model: string
  enabled: boolean; timeout_ms: number; max_tokens: number; system_prompt: string
  has_api_key: boolean; api_key?: string; chat_token_field?: string
  copy_from_id?: string
}

export function copySpeechModel(source: SpeechModelProfile, profiles: readonly SpeechModelProfile[], id: string): SpeechModelProfile {
  if (profiles.length >= 30) throw new Error('最多保存 30 个模型配置')
  if (!id || profiles.some(p => p.id === id)) throw new Error('复制必须使用新的模型配置 ID')
  const names = new Set(profiles.map(p => p.name.trim()))
  const base = Array.from(source.name.trim() || '新模型')
  let suffix = 2, name = ''
  do {
    const tail = String(suffix++).padStart(2, '0')
    name = base.slice(0, 80 - tail.length).join('') + tail
  } while (names.has(name))
  const copied = { ...source, id, name }
  // Reuse only a server-saved source. Typed keys remain editable draft inputs;
  // a saved secret is never fetched into the browser to perform a copy.
  if (source.api_key?.trim()) delete copied.copy_from_id
  else if (source.has_api_key) copied.copy_from_id = source.copy_from_id || source.id
  else delete copied.copy_from_id
  return copied
}
