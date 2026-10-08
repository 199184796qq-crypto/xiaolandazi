export type RiskTier = 'P0' | 'P1' | 'P2'
export interface RiskHit {
  rule_id: string; severity: RiskTier; category: string; matched_text: string
  original_start: number; original_end: number; condition: string; note?: string
}
export interface RiskAnnotations {
  hits: RiskHit[]; rule_hashes: Record<RiskTier, string>; mode: 'lexical_review_only'
}
export interface RiskChunk { text: string; tier?: RiskTier; title?: string }

// Ranges use Unicode code points, like Go's []rune, not JS UTF-16 offsets.
// Text remains text nodes in Vue: never interpolate model output as HTML.
export function riskChunks(text: string, hits: RiskHit[]): RiskChunk[] {
  const runes = Array.from(text)
  const events = new Map<number, { starts: RiskHit[]; ends: RiskHit[] }>()
  const event = (at: number) => {
    if (!events.has(at)) events.set(at, { starts: [], ends: [] })
    return events.get(at)!
  }
  event(0); event(runes.length)
  for (const hit of hits) {
    const start = hit.original_start, end = hit.original_end
    if (!['P0', 'P1', 'P2'].includes(hit.severity) || !Number.isInteger(start) || !Number.isInteger(end) || start < 0 || end > runes.length || end <= start) continue
    if (runes.slice(start, end).join('') !== hit.matched_text) continue // stale/malformed ranges cannot color other text.
    event(start).starts.push(hit); event(end).ends.push(hit)
  }
  const boundaries = [...events.keys()].sort((a, b) => a - b)
  const active = new Set<RiskHit>(), chunks: RiskChunk[] = []
  for (let i = 0; i < boundaries.length - 1; i++) {
    const at = boundaries[i]!, state = events.get(at)!
    state.ends.forEach(hit => active.delete(hit)); state.starts.forEach(hit => active.add(hit))
    const top = [...active].sort((a, b) => a.severity.localeCompare(b.severity))[0]
    const chunk: RiskChunk = { text: runes.slice(at, boundaries[i + 1]).join('') }
    if (top) { chunk.tier = top.severity; chunk.title = `${top.severity} · ${top.category} · ${top.note || top.condition}` }
    const previous = chunks.at(-1)
    if (previous && previous.tier === chunk.tier && previous.title === chunk.title) previous.text += chunk.text
    else if (chunk.text) chunks.push(chunk)
  }
  return chunks
}
