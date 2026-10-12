export type PlanConfigurationSection = 'facts' | 'interaction' | 'style' | 'voice'
export type PlanConfigurationMode = 'products' | 'supplement' | 'benefits' | 'knowledge' | 'miner' | 'memory' | 'anchor' | 'voice'
export type PlanFactMode = Extract<PlanConfigurationMode, 'products' | 'supplement' | 'benefits' | 'knowledge' | 'miner'>

export const planFactModes: readonly PlanFactMode[] = ['products', 'supplement', 'benefits', 'knowledge', 'miner']

export function isPlanFactMode(mode: string): mode is PlanFactMode {
  return planFactModes.some(item => item === mode)
}

export function planConfigurationSection(mode: string): PlanConfigurationSection | null {
  if (mode === 'fullshow') return null
  if (mode === 'memory' || mode === 'addressing' || mode === 'strategy') return 'interaction'
  if (mode === 'anchor') return 'style'
  if (mode === 'voice') return 'voice'
  return 'facts'
}
