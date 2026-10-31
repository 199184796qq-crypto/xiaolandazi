export type PlanConfigurationSection = 'business' | 'facts' | 'reading' | 'voice' | 'live'
export type PlanConfigurationMode = 'business' | 'products' | 'benefits' | 'plan_prompt' | 'reading' | 'voice' | 'fullshow'
export type PlanFactMode = Extract<PlanConfigurationMode, 'products' | 'benefits'>

export const planFactModes: readonly PlanFactMode[] = ['products', 'benefits']

export function isPlanFactMode(mode: string): mode is PlanFactMode {
  return planFactModes.some(item => item === mode)
}

export function planConfigurationSection(mode: string): PlanConfigurationSection | null {
  if (mode === 'fullshow') return 'live'
  if (mode === 'business') return 'business'
  if (mode === 'reading') return 'reading'
  if (mode === 'voice') return 'voice'
  return 'facts'
}
