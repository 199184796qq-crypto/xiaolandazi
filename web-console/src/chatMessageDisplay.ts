// Presentation only: never rewrite the stored message or conversation history.
export type ChatDisplayBlock =
  | { kind: 'paragraph' | 'heading' | 'quote' | 'code'; text: string }
  | { kind: 'list'; ordered: boolean; items: { text: string; number?: number }[] }

export function chatDisplayBlocks(text: string): ChatDisplayBlock[] {
  const blocks: ChatDisplayBlock[] = []
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  let paragraph: string[] = []
  const flush = () => {
    if (paragraph.length) blocks.push({ kind: 'paragraph', text: paragraph.join('\n') })
    paragraph = []
  }
  for (let index = 0; index < lines.length; index++) {
    const line = lines[index]!
    if (!line.trim()) { flush(); continue }
    const fence = /^\s*(`{3,}|~{3,})[^`~]*$/.exec(line)
    if (fence) {
      flush()
      const content: string[] = []
      const closing = new RegExp('^\\s*' + fence[1]![0] + '{' + fence[1]!.length + ',}\\s*$')
      while (++index < lines.length && !closing.test(lines[index]!)) content.push(lines[index]!)
      blocks.push({ kind: 'code', text: content.join('\n') })
      continue
    }
    const heading = /^\s{0,3}#{1,6}\s+(.+?)(?:\s+#+\s*)?$/.exec(line)
    if (heading) { flush(); blocks.push({ kind: 'heading', text: heading[1]! }); continue }
    const quote = /^\s*>\s?(.*)$/.exec(line)
    if (quote) { flush(); blocks.push({ kind: 'quote', text: quote[1]! }); continue }
    const bullet = /^\s*[-*+•]\s+(.+)$/.exec(line)
    const numbered = /^\s*(\d{1,3})[.)、](?:\s+|(?=[^\d\s]))(.+)$/.exec(line)
    if (bullet || numbered) {
      flush()
      const ordered = !!numbered
      const previous = blocks.at(-1)
      const item = numbered ? { text: numbered[2]!, number: Number(numbered[1]) } : { text: bullet![1]! }
      if (previous?.kind === 'list' && previous.ordered === ordered && index > 0 && lines[index - 1]!.trim()) previous.items.push(item)
      else blocks.push({ kind: 'list', ordered, items: [item] })
      continue
    }
    paragraph.push(line)
  }
  flush()
  return blocks
}

// Links and image captions remain readable text. Model-supplied URLs and HTML
// never become executable markup, network-loaded images or clickable actions.
export function chatInlineText(text: string): string {
  return text.replace(/!?\[([^\]\n]*)\]\(([^\s)]+)(?:\s+"[^"\n]*")?\)/g, (_match, label: string, url: string) => label ? label + '（' + url + '）' : url)
}
