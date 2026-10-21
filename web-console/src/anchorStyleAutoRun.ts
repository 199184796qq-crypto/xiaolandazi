export interface WheelSlot { index: number; sellingPoint: string; angle: string; key: string }
export interface SpokenSlot { sellingPoint: string; key: string }
export interface WheelSpacing { pointGap: number; combinationGap: number }

// Must match flattenAnchorStyleWheelTasks: interleave points by angle index.
export function wheelSlots(raw: string): WheelSlot[] {
  const root = JSON.parse(raw) as { 卖点列表?: Array<{ 卖点名: string; 角度: string[] }> }
  if (!Array.isArray(root.卖点列表)) throw new Error('卖点轮盘格式无效')
  const items = root.卖点列表
  const slots: WheelSlot[] = []
  const max = Math.max(0, ...items.map(item => item.角度.length))
  for (let angleIndex = 0; angleIndex < max; angleIndex++) {
    for (const item of items) {
      const point = item.卖点名.trim()
      const angle = item.角度[angleIndex]?.trim()
      if (point && angle) slots.push({ index: slots.length, sellingPoint: point, angle, key: point + '×' + angle })
    }
  }
  return slots
}

export function speechChars(text: string): number { return [...text.replace(/\s/gu, '')].length }

export function selectRememberedSlot(slots: WheelSlot[], history: SpokenSlot[], spacing: WheelSpacing): WheelSlot {
  if (!slots.length) throw new Error('卖点轮盘为空')
  if (!Number.isInteger(spacing.pointGap) || !Number.isInteger(spacing.combinationGap) || spacing.pointGap < 0 || spacing.combinationGap < 0) throw new Error('记忆间隔须为非负整数')
  const recentPoints = new Set(history.slice(Math.max(0, history.length - spacing.pointGap)).map(item => item.sellingPoint))
  const recentKeys = new Set(history.slice(Math.max(0, history.length - spacing.combinationGap)).map(item => item.key))
  const uses = new Map<string, number>(), pointUses = new Map<string, number>()
  const lastPoint = new Map<string, number>(), lastKey = new Map<string, number>()
  history.forEach((item, index) => {
    uses.set(item.key, (uses.get(item.key) || 0) + 1)
    pointUses.set(item.sellingPoint, (pointUses.get(item.sellingPoint) || 0) + 1)
    lastPoint.set(item.sellingPoint, index); lastKey.set(item.key, index)
  })
  const eligible = slots.filter(slot => !recentPoints.has(slot.sellingPoint) && !recentKeys.has(slot.key))
  if (!eligible.length) throw new Error('当前卖点角度池无法满足记忆间隔，已暂停；请扩充池或降低间隔，不会强行重复')
  eligible.sort((a, b) => (uses.get(a.key) || 0) - (uses.get(b.key) || 0)
    || (pointUses.get(a.sellingPoint) || 0) - (pointUses.get(b.sellingPoint) || 0)
    || (lastPoint.get(a.sellingPoint) ?? -1) - (lastPoint.get(b.sellingPoint) ?? -1)
    || (lastKey.get(a.key) ?? -1) - (lastKey.get(b.key) ?? -1) || a.index - b.index)
  return eligible[0]!
}

export interface AutoLoopOptions<T> {
  targetChars: number
  maxRounds: number
  segmentMaxChars: number
  stopped: () => boolean
  generatedChars: () => number
  generatedRounds: () => number
  select: () => WheelSlot
  assertContext: () => void
  generate: (slot: WheelSlot, maxChars: number) => Promise<T>
  receive: (turn: T) => void
  score: (turn: T) => Promise<void>
}

// One outstanding generation only. A received turn is committed before scoring;
// score failure must not make the caller regenerate the same turn on resume.
export async function runAutoLoop<T>(options: AutoLoopOptions<T>): Promise<'completed' | 'paused'> {
  if (!Number.isInteger(options.targetChars) || options.targetChars < 1 || options.targetChars > 100000) throw new Error('目标字数须为1到100000的整数')
  if (!Number.isInteger(options.segmentMaxChars) || options.segmentMaxChars < 80 || options.segmentMaxChars > 1000) throw new Error('自动生成单段上限须为80到1000字')
  if (!Number.isInteger(options.maxRounds) || options.maxRounds < 1 || options.maxRounds > 500) throw new Error('安全轮数上限须为1到500')
  while (options.generatedChars() < options.targetChars) {
    if (options.stopped()) return 'paused'
    options.assertContext()
    if (options.generatedRounds() >= options.maxRounds) throw new Error('已达到安全轮数上限，停止自动请求')
    const slot = options.select()
    // Keep full returned text; last segment may exceed a small remaining budget.
    const maxChars = Math.max(80, Math.min(options.segmentMaxChars, options.targetChars - options.generatedChars()))
    const previousChars = options.generatedChars()
    const turn = await options.generate(slot, maxChars)
    options.receive(turn)
    if (options.generatedChars() <= previousChars) throw new Error('本轮没有有效话术，已停止，避免空返回持续消耗请求')
    await options.score(turn)
  }
  return 'completed'
}

export function encodeTurns<T extends { score?: { baseline_json: string; scorecard_asset_id: number } }>(turns: T[]) {
  const baselines: Record<string, string> = {}
  const records = turns.map(turn => {
    if (!turn.score) return turn
    baselines[String(turn.score.scorecard_asset_id)] = turn.score.baseline_json
    return { ...turn, score: { ...turn.score, baseline_json: '' } }
  })
  return { records, baselines }
}

export function decodeTurns<T extends { score?: { baseline_json: string; scorecard_asset_id: number } }>(records: T[], baselines: Record<string, string>): T[] {
  return records.map(turn => turn.score ? { ...turn, score: { ...turn.score, baseline_json: baselines[String(turn.score.scorecard_asset_id)] || turn.score.baseline_json } } : turn)
}
