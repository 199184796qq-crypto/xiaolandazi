import type { planPromptTabs } from './planPrompt'
import compatibilityCatalog from './planPromptPlaceholderCatalog.json'

export type PlanPromptTemplateKey = typeof planPromptTabs[number]['key'] | 'rewrite_planning_md'
export interface PlanPromptPlaceholder {
  key: string
  label: string
  insert_text: string
  description: string
  group: string
  templates: PlanPromptTemplateKey[]
}

const keys = new Set(['persona_md', 'facts_md', 'red_lines_md', 'speech_requirements_md', 'interaction_requirements_md', 'rewrite_planning_md'])

export function readPromptPlaceholders(value: unknown): PlanPromptPlaceholder[] {
  if (!Array.isArray(value)) return []
  const seen = new Set<string>()
  return value.filter((item): item is PlanPromptPlaceholder => {
    if (!item || typeof item !== 'object') return false
    const valid = ['key', 'label', 'insert_text', 'description', 'group'].every(key => typeof item[key] === 'string')
      && item.key.length > 0 && item.insert_text.length > 0 && item.insert_text.length <= 1000
      && Array.isArray(item.templates) && item.templates.length > 0 && item.templates.every((key: unknown) => typeof key === 'string' && keys.has(key))
    if (!valid || seen.has(item.key)) return false
    seen.add(item.key)
    return true
  })
}

// Versioned compatibility data generated from the backend catalog's renderer test.
// A server-provided catalog, including an explicit empty catalog, takes precedence.
export function promptPlaceholdersForDocument(value: unknown): PlanPromptPlaceholder[] {
  return readPromptPlaceholders(value === undefined ? compatibilityCatalog : value)
}
