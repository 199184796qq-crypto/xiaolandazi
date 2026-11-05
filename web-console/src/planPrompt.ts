export interface PlanPromptSections {
  persona_md: string
  facts_md: string
  red_lines_md: string
  speech_requirements_md: string
  interaction_requirements_md: string
  // Editor-only provenance; never compose this value into model prompts.
  persona_editor_json?: string
}

export const planPromptTabs = [
  { key: 'persona_md', label: '主播人设' },
  { key: 'facts_md', label: '事实依据' },
  { key: 'red_lines_md', label: '红线禁止' },
  { key: 'speech_requirements_md', label: '直播话术要求' },
  { key: 'interaction_requirements_md', label: '互动回答要求' },
] as const

export function emptyPlanPromptSections(): PlanPromptSections {
  return { persona_md: '', facts_md: '', red_lines_md: '', speech_requirements_md: '', interaction_requirements_md: '' }
}

export function readPlanPromptSections(value: unknown): PlanPromptSections {
  if (!value || typeof value !== 'object' || !planPromptTabs.every(tab => typeof (value as Record<string, unknown>)[tab.key] === 'string')) {
    throw new Error('服务端未返回完整的分区提示词，当前内容已保留，请更新服务后重试')
  }
  const source = value as PlanPromptSections
  const sections: PlanPromptSections = { persona_md: source.persona_md, facts_md: source.facts_md, red_lines_md: source.red_lines_md, speech_requirements_md: source.speech_requirements_md, interaction_requirements_md: source.interaction_requirements_md }
  if (typeof source.persona_editor_json === 'string' && source.persona_editor_json) sections.persona_editor_json = source.persona_editor_json
  return sections
}

export function readPlanRewritePlanning(value: unknown): string {
  if (typeof value !== 'string') throw new Error('服务端未返回话术规划原文，当前内容已保留，请更新服务后重试')
  return value
}

export function cleanPlanPromptSections(value: PlanPromptSections): PlanPromptSections {
  const out = emptyPlanPromptSections()
  for (const { key } of planPromptTabs) {
    out[key] = value[key].replace(/\r\n/g, '\n').split('\n').filter(line => !line.trim().startsWith('〔提示〕')).join('\n').trim()
  }
  return out
}

export function compilePlanPrompts(value: PlanPromptSections) {
  const s = cleanPlanPromptSections(value)
  const combine = (keys: (typeof planPromptTabs[number]['key'])[]) => keys.map(key => {
    const title = planPromptTabs.find(tab => tab.key === key)!.label
    return s[key] ? `【${title}】\n${s[key]}` : ''
  }).filter(Boolean).join('\n\n')
  return {
    interaction_prompt_md: combine(['persona_md', 'facts_md', 'red_lines_md', 'interaction_requirements_md']),
    speech_prompt_md: combine(['persona_md', 'facts_md', 'red_lines_md', 'speech_requirements_md']),
  }
}

export function compilePlanRewritePrompt(value: PlanPromptSections, planning: string) {
  const s = cleanPlanPromptSections(value)
  return '你是直播话术改写器。你的工作不是润色，是"换一套说法把同一件事重新讲一遍"。'
    + `\n\n【主播人设】\n${s.persona_md}\n\n【事实依据】\n${s.facts_md}\n\n【红线禁止】\n${s.red_lines_md}`
    + `\n\n【样本话术】\n{{样本话术}}\n\n${planning}`
}

export function planPromptValidation(value: PlanPromptSections, planning = '') {
  if (Array.from(planning).length > 20000) return '话术规划不能超过20000字，请缩短内容'
  const compiled = compilePlanPrompts(value)
  if (Object.values(compiled).some(text => Array.from(text).length > 20000)) {
    return '合成后的每份元提示词不能超过20000字，请缩短对应文本框内容'
  }
  return Array.from(compilePlanRewritePrompt(value, planning)).length > 20000
    ? '合成后的话术修改元提示词不能超过20000字，请缩短话术规划或方案资料' : ''
}
