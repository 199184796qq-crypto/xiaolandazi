import { reactive } from 'vue'

const storageKey = 'live-companion.ui-settings.v1'

export interface UISettings {
  uiTextSize: number
  publicScreenFontSize: number
}

const defaults: UISettings = {
  uiTextSize: 15,
  publicScreenFontSize: 18,
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}

function loadSettings(): UISettings {
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return { ...defaults }

    const saved = JSON.parse(raw) as Partial<UISettings>
    return {
      uiTextSize: clamp(Number(saved.uiTextSize) || defaults.uiTextSize, 13, 20),
      publicScreenFontSize: clamp(
        Number(saved.publicScreenFontSize) || defaults.publicScreenFontSize,
        16,
        30,
      ),
    }
  } catch {
    return { ...defaults }
  }
}

export const uiSettings = reactive<UISettings>(loadSettings())

export function applyUISettings() {
  const root = document.documentElement
  const delta = uiSettings.uiTextSize - 14

  root.style.setProperty('--ui-font-delta', String(delta) + 'px')
  root.style.setProperty('--public-screen-font-size', String(uiSettings.publicScreenFontSize) + 'px')
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

export function resetUISettings() {
  uiSettings.uiTextSize = defaults.uiTextSize
  uiSettings.publicScreenFontSize = defaults.publicScreenFontSize
  persist()
}

applyUISettings()