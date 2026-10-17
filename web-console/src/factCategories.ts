import { reactive } from 'vue'
import { request } from './api'
import { session } from './session'
import { liveSupportRoomForRequest } from './liveSupportAccess'

export interface FactCategoryDefinition {
  value: string
  label: string
  description: string
  group: string
  family: string
}
export interface FactCategoryCatalog {
  schema_version: number
  version: string
  categories: FactCategoryDefinition[]
}

export const factCategoryState = reactive({ items: [] as FactCategoryDefinition[], version: '', loading: false, error: '' })
let pending: Promise<void> | undefined
let pendingScope = '', loadedScope = '', epoch = 0
function catalogScope(planId?: number) {
  const actor = session.bootstrap?.actor
  return `${actor?.user_id || 0}:${actor?.tenant_id || 0}:${liveSupportRoomForRequest('/api/v1/live-agent-fact-categories') || 0}:${planId || 0}`
}

export function factCategoryDefinition(value: string): FactCategoryDefinition {
  return factCategoryState.items.find(item => item.value === value) || {
    value, label: value || '未分类', description: '历史分类（当前配置未收录）', group: '历史分类', family: 'other',
  }
}

export function factCategoryGroups(currentValue = '') {
  const groups: Array<{ label: string; options: FactCategoryDefinition[] }> = []
  const items = [...factCategoryState.items]
  if (currentValue && !items.some(item => item.value === currentValue)) items.push(factCategoryDefinition(currentValue))
  for (const item of items) {
    let group = groups.find(candidate => candidate.label === item.group)
    if (!group) { group = { label: item.group, options: [] }; groups.push(group) }
    group.options.push(item)
  }
  return groups
}

export function loadFactCategories(options: { suppressErrorDialog?: boolean; planId?: number | null } = {}): Promise<void> {
  const planId = Number.isSafeInteger(options.planId) && Number(options.planId) > 0 ? Number(options.planId) : undefined
  const scope = catalogScope(planId)
  if (pending && pendingScope === scope) return pending
  const requestEpoch = ++epoch
  if (pendingScope !== scope || loadedScope && loadedScope !== scope) { factCategoryState.items = []; factCategoryState.version = ''; factCategoryState.error = '' }
  pendingScope = scope
  factCategoryState.loading = true
  pending = (async () => {
    try {
      const path = '/api/v1/live-agent-fact-categories' + (planId ? `?plan_id=${planId}` : '')
      const result = await request<FactCategoryCatalog>(path, { cache: 'no-store', suppressErrorDialog: options.suppressErrorDialog })
      if (requestEpoch !== epoch || catalogScope(planId) !== scope) return
      const seen = new Set<string>()
      if (result.schema_version !== 1 || !result.version || !Array.isArray(result.categories) || !result.categories.length || result.categories.some(item => {
        if (!item.value || !item.label || !item.group || !item.description || !item.family || seen.has(item.value)) return true
        seen.add(item.value); return false
      })) throw new Error('服务器事实分类配置格式错误')
      factCategoryState.items = result.categories
      factCategoryState.version = result.version
      factCategoryState.error = ''
      loadedScope = scope
    } catch (cause) {
      if (requestEpoch !== epoch || catalogScope(planId) !== scope) return
      factCategoryState.error = cause instanceof Error ? cause.message : '事实分类配置读取失败'
    } finally { if (requestEpoch === epoch) { factCategoryState.loading = false; pending = undefined } }
  })()
  return pending
}
