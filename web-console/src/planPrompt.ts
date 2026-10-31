export interface PlanPromptSections {
  persona_md: string
  facts_md: string
  speech_requirements_md: string
  interaction_requirements_md: string
}

export const planPromptTabs = [
  { key: 'persona_md', label: '主播人设' },
  { key: 'facts_md', label: '事实依据' },
  { key: 'speech_requirements_md', label: '直播话术要求' },
  { key: 'interaction_requirements_md', label: '互动回答要求' },
] as const

export function emptyPlanPromptSections(): PlanPromptSections {
  return { persona_md: '', facts_md: '', speech_requirements_md: '', interaction_requirements_md: '' }
}

export function readPlanPromptSections(value: unknown): PlanPromptSections {
  if (!value || typeof value !== 'object' || !planPromptTabs.every(tab => typeof (value as Record<string, unknown>)[tab.key] === 'string')) {
    throw new Error('服务端未返回完整的四分区提示词，当前内容已保留，请更新服务后重试')
  }
  return { ...(value as PlanPromptSections) }
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
  const combine = (keys: (keyof PlanPromptSections)[]) => keys.map(key => {
    const title = planPromptTabs.find(tab => tab.key === key)!.label
    return s[key] ? `【${title}】\n${s[key]}` : ''
  }).filter(Boolean).join('\n\n')
  return {
    interaction_prompt_md: combine(['persona_md', 'facts_md', 'interaction_requirements_md']),
    speech_prompt_md: combine(['persona_md', 'facts_md', 'speech_requirements_md']),
  }
}

export function planPromptValidation(value: PlanPromptSections) {
  const compiled = compilePlanPrompts(value)
  return Object.values(compiled).some(text => Array.from(text).length > 20000)
    ? '合成后的每份元提示词不能超过20000字，请缩短对应文本框内容' : ''
}
