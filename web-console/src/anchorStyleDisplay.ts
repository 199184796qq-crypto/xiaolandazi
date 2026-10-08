// Read-only presentation of the returned prose. Do not infer, score, rewrite,
// or persist anything here; the original string remains the generation asset.
export interface StyleDisplayPoint {
  marker: string
  text: string
}
export interface StyleDisplaySection {
  title: string
  points: StyleDisplayPoint[]
}
export interface StyleInlineToken {
  kind: 'text' | 'strong' | 'em' | 'code'
  text: string
}

export function parseAnchorStyleDisplay(text: string): StyleDisplaySection[] {
  const sections: StyleDisplaySection[] = []
  let current: StyleDisplaySection | undefined
  let point: StyleDisplayPoint | undefined
  for (const line of text.replace(/\r\n?/g, '\n').split('\n')) {
    const heading = /^\s*(?:#{1,6}\s*)?【([^】\n]+)】(.*)$/.exec(line)
    if (heading) {
      current = { title: heading[1]!.trim(), points: [] }
      sections.push(current)
      point = undefined
      if (heading[2]!.trim()) {
        point = { marker: '', text: heading[2]!.trim() }
        current.points.push(point)
      }
      continue
    }
    if (!line.trim()) {
      if (point) point.text += '\n'
      continue
    }
    if (!current) {
      current = { title: '', points: [] }
      sections.push(current)
    }
    // Decimal prices inside prose are not list numbering. Also support Chinese
    // list punctuation and Markdown bullets without imposing any fixed topics.
    const numbered = /^\s*(\d{1,3})([.．、])(?:\s+|(?=[^\d\s]))(.*)$/.exec(line)
    const bullet = /^\s*([-*+])\s+(.*)$/.exec(line)
    if (numbered || bullet) {
      point = { marker: numbered ? numbered[1]! + numbered[2]! : bullet![1]!, text: numbered ? numbered[3]! : bullet![2]! }
      current.points.push(point)
    } else if (point) {
      point.text += '\n' + line
    } else {
      point = { marker: '', text: line }
      current.points.push(point)
    }
  }
  return sections
}

export function styleDisplayLabel(text: string): { label: string; separator: string; body: string } {
  const match = /^([^\n：:]{1,40})([：:])([\s\S]*)$/.exec(text)
  if (!match || Array.from(match[1]!.replace(/[*_`]/g, '')).length > 24) return { label: '', separator: '', body: text }
  return { label: match[1]!, separator: match[2]!, body: match[3]! }
}

export function styleInlineTokens(text: string): StyleInlineToken[] {
  const tokens: StyleInlineToken[] = []
  const pattern = /\*\*([^*\n]+)\*\*|__([^_\n]+)__|\*([^*\n]+)\*|_([^_\n]+)_|`([^`\n]+)`/g
  let offset = 0
  for (const match of text.matchAll(pattern)) {
    const index = match.index!
    if (index > offset) tokens.push({ kind: 'text', text: text.slice(offset, index) })
    tokens.push({ kind: match[1] !== undefined || match[2] !== undefined ? 'strong' : match[5] !== undefined ? 'code' : 'em', text: match[1] ?? match[2] ?? match[3] ?? match[4] ?? match[5]! })
    offset = index + match[0].length
  }
  if (offset < text.length) tokens.push({ kind: 'text', text: text.slice(offset) })
  return tokens
}
