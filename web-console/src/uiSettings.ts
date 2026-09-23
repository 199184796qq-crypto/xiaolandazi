import { reactive } from 'vue'

const storageKey = 'live-companion.ui-settings.v2'

export type TableDensity = 'compact' | 'standard' | 'comfortable'

export interface UISettings {
  uiTextSize: number
  publicScreenFontSize: number
  tableTextSize: number
  tableDensity: TableDensity
}

const defaults: UISettings = {
  uiTextSize: 15,
  publicScreenFontSize: 18,
  tableTextSize: 14,
  tableDensity: 'standard',
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function normalizeDensity(value: unknown): TableDensity {
  return value === 'compact' || value === 'comfortable' ? value : 'standard'
}

function loadSettings(): UISettings {
  try {
    const raw = window.localStorage.getItem(storageKey) || window.localStorage.getItem('live-companion.ui-settings.v1')
    if (!raw) return { ...defaults }
    const saved = JSON.parse(raw) as Partial<UISettings>
    return {
      uiTextSize: clamp(Number(saved.uiTextSize) || defaults.uiTextSize, 13, 20),
      publicScreenFontSize: clamp(Number(saved.publicScreenFontSize) || defaults.publicScreenFontSize, 16, 30),
      tableTextSize: clamp(Number(saved.tableTextSize) || defaults.tableTextSize, 13, 18),
      tableDensity: normalizeDensity(saved.tableDensity),
    }
  } catch {
    return { ...defaults }
  }
}

export const uiSettings = reactive<UISettings>(loadSettings())

export function applyUISettings() {
  const root = document.documentElement
  const delta = uiSettings.uiTextSize - 14
  const densityPadding = uiSettings.tableDensity === 'compact' ? 9 : uiSettings.tableDensity === 'comfortable' ? 17 : 13

  root.style.setProperty('--ui-font-delta', String(delta) + 'px')
  root.style.setProperty('--public-screen-font-size', String(uiSettings.publicScreenFontSize) + 'px')
  root.style.setProperty('--table-font-size', String(uiSettings.tableTextSize) + 'px')
  root.style.setProperty('--table-cell-padding-y', String(densityPadding) + 'px')
}

function persist() {
  window.localStorage.setItem(storageKey, JSON.stringify(uiSettings))
  applyUISettings()
}

export function setUITextSize(value: number) {
  uiSettings.uiTextSize = clamp(value, 13, 20)
  persist()
}

export function setPublicScreenFontSize(value: number) {
  uiSettings.publicScreenFontSize = clamp(value, 16, 30)
  persist()
}

export function setTableTextSize(value: number) {
  uiSettings.tableTextSize = clamp(value, 13, 18)
  persist()
}

export function setTableDensity(value: TableDensity) {
  uiSettings.tableDensity = normalizeDensity(value)
  persist()
}

export function resetUISettings() {
  Object.assign(uiSettings, defaults)
  persist()
}

applyUISettings()