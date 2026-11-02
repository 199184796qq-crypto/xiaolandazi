export type ScriptEntry = { id: string; text: string }
export type ScriptGroup = { id: string; title: string; text: string; entries?: ScriptEntry[]; audioAssetIds: number[]; pinned: boolean }

// Preview the text that Save will persist without rewriting an editor draft.
export const normalizeScriptEntryText = (text: string): string => text
  .split(/[\r\n\v\f\u0085\u2028\u2029]+/u)
  .map(line => line.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, ''))
  .join('')

export const scriptCharacters = (text: string): number => Array.from(normalizeScriptEntryText(text)).length

// Legacy separators are interpreted only when loading an old text box into
// this editor. A new entry's text is never split or joined by a delimiter.
export function scriptEntries(group: ScriptGroup): ScriptEntry[] {
  if (group.entries) return group.entries
  const parts = group.text.split(/={4,}/u).map(text => text.trim()).filter(Boolean)
  return (parts.length ? parts : ['']).map((text, i) => ({ id: `s${i + 1}`, text }))
}

export function scriptEntryErrors(entries: readonly ScriptEntry[]): string[] {
  const total = entries.reduce((sum, entry) => sum + scriptCharacters(entry.text), 0)
  return entries.map(entry => {
    const count = scriptCharacters(entry.text)
    const errors = []
    if (!normalizeScriptEntryText(entry.text)) errors.push('话术不能只包含空格或换行')
    else if (count < 400) errors.push(`当前 ${count} 字符，至少需要 400 字符`)
    if (count > 4000) errors.push(`当前 ${count} 字符，单条不能超过 4000 字符`)
    if (total > 4000) errors.push(`本组合计 ${total} 字符，所有话术合计不能超过 4000 字符`)
    return errors.join('；')
  })
}

export function scriptGroupsError(groups: readonly ScriptGroup[]): string {
  for (const group of groups) {
    const error = scriptEntryErrors(scriptEntries(group)).find(Boolean)
    if (error) return `${group.title || '未命名话术组'}：${error}`
  }
  return ''
}

export function groupedScriptForSave(groups: readonly ScriptGroup[], active: string) {
  const error = scriptGroupsError(groups)
  if (error) throw new Error(error)
  return {
    version: 2,
    tabs: groups.map((group, order) => ({
      tab_id: group.id, title: group.title.trim(), order,
      entries: scriptEntries(group).map(entry => ({ entry_id: entry.id, text: normalizeScriptEntryText(entry.text) })),
      audio_asset_ids: group.audioAssetIds, pinned: group.pinned,
    })),
    active_tab_id: active,
  }
}
