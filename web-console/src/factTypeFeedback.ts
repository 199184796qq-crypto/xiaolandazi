// Classification explanations are display text only. Keep provider identifiers
// and long technical details out of the table without rewriting stored facts.
export function factTypeFeedbackText(raw: string | undefined, fallback: string, categories: readonly { value: string; label: string }[] = []): string {
  const labels = new Map(categories.map(item => [item.value, item.label]))
  const text = (raw || '')
    .replace(/[（(]\s*[a-z][a-z\d_ .\/-]*\s*[)）]/gi, '')
    .replace(/\b[a-z][a-z\d_]*\b/gi, value => labels.get(value) || value)
    .replace(/\s+/g, ' ')
    .replace(/\s+([，。；：！？、）])/g, '$1')
    .trim()
  if (!text || /[a-z]{2,}/i.test(text)) return fallback
  return text.length > 500 ? text.slice(0, 500) + '…' : text
}
