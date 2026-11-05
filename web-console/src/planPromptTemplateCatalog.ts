import type { PlanPromptPage } from './api'

export interface PromptTextTemplate { id: string; name: string; content: string }
export interface PromptTemplateCatalog { version: 1; pages: Record<PlanPromptPage, PromptTextTemplate[]> }
export interface PromptTemplateCatalogDocument { catalog: PromptTemplateCatalog; revision: string }
export interface SelectedPromptTemplate extends PromptTextTemplate { rendered_content?: string; render_error?: string }
export interface PromptTemplateSelection { page: PlanPromptPage; templates: SelectedPromptTemplate[]; revision: string }
export const templatePages: PlanPromptPage[] = ['persona_md', 'facts_md', 'red_lines_md', 'speech_requirements_md', 'interaction_requirements_md', 'rewrite_planning_md']
export const emptyTemplateCatalog = (): PromptTemplateCatalog => ({ version: 1, pages: Object.fromEntries(templatePages.map(page => [page, [] as PromptTextTemplate[]])) as PromptTemplateCatalog['pages'] })
export function templateListError(items: unknown): string {
  if (!Array.isArray(items) || items.length > 20) return '每个标签最多20份模板'
  const ids = new Set<string>()
  for (const item of items) {
    if (!item || typeof item.id !== 'string' || !/^[\w-]{1,80}$/.test(item.id) || ids.has(item.id)) return '模板标识无效或重复'
    ids.add(item.id)
    if (typeof item.name !== 'string' || !item.name.trim() || Array.from(item.name).length > 60) return '模板名称须为1至60字'
    if (typeof item.content !== 'string' || Array.from(item.content).length > 20000 || new TextEncoder().encode(item.content).length > 60000) return '每份模板内容最多20000字、60000字节'
  }
  // Match Go's JSON escaping and the existing TEXT setting column capacity.
  const serialized = JSON.stringify(items).replace(/[<>&\u2028\u2029]/g, c => '\\u' + c.charCodeAt(0).toString(16).padStart(4, '0'))
  if (new TextEncoder().encode(serialized).length > 60000) return '每个标签的模板合计最多60000字节（含格式），请精简内容'
  return ''
}
export function catalogError(catalog: PromptTemplateCatalog): string {
  if (!catalog || catalog.version !== 1 || !catalog.pages || Object.keys(catalog.pages).length !== 6) return '未返回完整的六个标签模板目录'
  const ids = new Set<string>()
  for (const page of templatePages) {
    const error = templateListError(catalog.pages[page])
    if (error) return error
    for (const item of catalog.pages[page]) {
      if (ids.has(item.id)) return '模板标识重复'
      ids.add(item.id)
    }
  }
  return ''
}
export function validTemplateRevision(value: unknown): value is string { return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value) }
