export type PlanConfigurationSection = 'business' | 'facts' | 'interaction' | 'reading' | 'style' | 'voice' | 'live'
export type PlanConfigurationMode = 'business' | 'products' | 'supplement' | 'benefits' | 'knowledge' | 'plan_prompt' | 'memory' | 'reading' | 'anchor' | 'voice' | 'fullshow'
export type PlanFactMode = Extract<PlanConfigurationMode, 'products' | 'supplement' | 'benefits' | 'knowledge'>

export const planFactModes: readonly PlanFactMode[] = ['products', 'benefits', 'supplement', 'knowledge']

export function isPlanFactMode(mode: string): mode is PlanFactMode {
  return planFactModes.some(item => item === mode)
}

export function planConfigurationSection(mode: string): PlanConfigurationSection | null {
  if (mode === 'fullshow') return 'live'
  if (mode === 'business') return 'business'
  if (mode === 'memory' || mode === 'addressing' || mode === 'strategy') return 'interaction'
  if (mode === 'anchor') return 'style'
  if (mode === 'reading') return 'reading'
  if (mode === 'voice') return 'voice'
  return 'facts'
}
