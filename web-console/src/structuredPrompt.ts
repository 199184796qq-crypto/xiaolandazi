export interface PromptField {
  id: string
  origin: 'model' | 'user'
  key: string | null
  prefix: string
  value: string
  rawValue: string
  suffix: string
}

export interface PromptNode {
  id: string
  name: string
  heading: string
  fields: PromptField[]
  origin: 'model' | 'user'
}

export interface StructuredPrompt {
  nodes: PromptNode[]
  newline: string
}

function headingName(line: string): string | null {
  const bracket = /^ {0,3}【([^【】\r\n]+)】[ \t]*$/.exec(line)
  if (bracket?.[1]?.trim()) return bracket[1].trim()
  const markdown = /^ {0,3}#{1,6}[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$/.exec(line)
  return markdown?.[1]?.trim() || null
}

function numberedItem(line: string): boolean {
  // A list item remains a whole sentence even when it contains a colon.
  // Four-space/tab continuations belong to an existing value, not a new item.
  return /^ {0,3}(?:\*\*)?(?:\d+[.．](?!\d)|\d+[、)）]|[（(]\d+[)）]|[一二三四五六七八九十百零〇]+[、)）])/.test(line)
}

function fieldStart(line: string): { key: string; prefix: string; rest: string } | null {
  if (/^ {4}|^\t/.test(line)) return null
  if (numberedItem(line)) return null
  const match = /^( {0,3}(?:[-*+][ \t]+)?(?:\*\*([^*\r\n]{1,40})\*\*|([^:：\r\n]{1,40}?))[ \t]*[:：][ \t]*)(.*)$/.exec(line)
  if (!match) return null
  const key = (match[2] || match[3] || '').trim()
  if (!key || /[，。！？；【】<>`]/.test(key) || /^(?:https?|ftp|\d+)$/i.test(key)) return null
  return { key, prefix: match[1]!, rest: match[4]! }
}

function decodedValue(raw: string, keyed: boolean): string {
  const lines = raw.replace(/\r\n|\r/g, '\n').split('\n')
  return lines.map((line, index) => (!keyed || index > 0) && line.startsWith('    ') ? line.slice(4) : line).join('\n')
}

// Recognised labels are separate immutable data. Everything else remains body text.
// Merely reading/entering the editor must round-trip the source byte for byte.
export function parseStructuredPrompt(text: string, metadata = ''): StructuredPrompt {
  const newline = text.match(/\r\n|\r|\n/)?.[0] || '\n'
  let sequence = 0
  const nodes: PromptNode[] = []
  let node: PromptNode = { id: 'n' + sequence++, name: '未分组正文', heading: '', fields: [], origin: 'model' }
  let pending: { key: string | null; prefix: string; text: string } | null = null
  const flush = () => {
    if (!pending) return
    const suffix = pending.text.match(/(?:(?:\r\n|\r|\n)[ \t]*)+$/)?.[0] || ''
    const rawValue = pending.text.slice(0, pending.text.length - suffix.length)
    node.fields.push({ id: 'f' + sequence++, origin: 'model', key: pending.key, prefix: pending.prefix, value: decodedValue(rawValue, pending.key !== null), rawValue, suffix })
    pending = null
  }
  for (const match of text.matchAll(/[^\r\n]*(?:\r\n|\r|\n|$)/g)) {
    const raw = match[0]
    if (!raw) continue
    const line = raw.replace(/(?:\r\n|\r|\n)$/, '')
    const name = headingName(line)
    if (name !== null) {
      flush()
      if (node.heading || node.fields.length) nodes.push(node)
      node = { id: 'n' + sequence++, name, heading: raw, fields: [], origin: 'model' }
    } else {
      const field = fieldStart(line)
      if (field) {
        flush()
        pending = { key: field.key, prefix: field.prefix, text: raw.slice(field.prefix.length) }
      } else if (pending && pending.key !== null && numberedItem(line)) {
        flush()
        pending = { key: null, prefix: '', text: raw }
      } else if (pending) pending.text += raw
      else pending = { key: null, prefix: '', text: raw }
    }
  }
  flush()
  if (!node.heading && !node.fields.length) node.fields.push({ id: 'f' + sequence++, origin: 'model', key: null, prefix: '', value: '', rawValue: '', suffix: '' })
  nodes.push(node)
  const document: StructuredPrompt = { nodes, newline }
  // Missing/invalid/stale provenance always locks every node. Metadata is kept
  // separately from prompt text and only applies to this exact saved source.
  try {
    const saved = JSON.parse(metadata)
    if (saved?.version !== 1 || saved.source !== text || !Array.isArray(saved.user_nodes)) return document
    const indexes = new Set<number>()
    for (const entry of saved.user_nodes) {
      if (!Number.isInteger(entry?.index) || entry.index < 0 || !nodes[entry.index]?.heading || nodes[entry.index]?.name !== entry.name || indexes.has(entry.index)) return document
      indexes.add(entry.index)
    }
    const fields = new Set<string>()
    // v1 documents saved before field provenance only know entire custom nodes.
    // Never infer ownership of an unknown field in an original/model node.
    if (saved.user_fields !== undefined) {
      if (!Array.isArray(saved.user_fields)) return document
      for (const entry of saved.user_fields) {
        if (!Number.isInteger(entry?.node_index) || entry.node_index < 0 || !Number.isInteger(entry?.index) || entry.index < 0) return document
        const field = nodes[entry.node_index]?.fields[entry.index]
        const identity = entry.node_index + ':' + entry.index
        if (!field || field.key === null || field.key !== entry.key || fields.has(identity)) return document
        fields.add(identity)
      }
    }
    return { ...document, nodes: nodes.map((node, index) => ({ ...node,
      origin: indexes.has(index) ? 'user' : 'model',
      fields: node.fields.map((field, fieldIndex) => ({ ...field, origin: field.key !== null && (indexes.has(index) || fields.has(index + ':' + fieldIndex)) ? 'user' : 'model' })),
    })) }
  } catch { return document }
}

export function serializeStructuredPrompt(document: StructuredPrompt): string {
  return document.nodes.map(node => node.heading + node.fields.map(field => field.prefix + field.rawValue + field.suffix).join('')).join('')
}

export function serializePromptMetadata(document: StructuredPrompt): string {
  const user_nodes = document.nodes.flatMap((node, index) => node.origin === 'user' ? [{ index, name: node.name }] : [])
  const user_fields = document.nodes.flatMap((node, node_index) => node.fields.flatMap((field, index) => field.origin === 'user' && field.key !== null ? [{ node_index, index, key: field.key }] : []))
  return user_nodes.length || user_fields.length ? JSON.stringify({ version: 1, source: serializeStructuredPrompt(document), user_nodes, user_fields }) : ''
}

export function removePromptField(document: StructuredPrompt, fieldId: string): StructuredPrompt {
  const field = document.nodes.flatMap(node => node.fields).find(field => field.id === fieldId)
  if (!field) throw new Error('当前键值或正文已变化，请重新选择')
  return { ...document, nodes: document.nodes.map(node => ({ ...node, fields: node.fields.filter(field => field.id !== fieldId) })) }
}

export function removePromptNode(document: StructuredPrompt, nodeId: string): StructuredPrompt {
  const node = document.nodes.find(node => node.id === nodeId)
  if (!node) throw new Error('当前节点已变化，请重新选择')
  return { ...document, nodes: document.nodes.filter(node => node.id !== nodeId) }
}

function encodedValue(value: string, keyed: boolean, newline: string): string {
  // Continuations are indented so pasted headings/keys stay inside their value.
  return value.replace(/\r\n|\r/g, '\n').split('\n').map((line, index) => {
    if (index > 0) return '    ' + line
    if (!keyed && (headingName(line) || fieldStart(line))) return '    ' + line
    return line
  }).join(newline)
}

export function replacePromptValue(document: StructuredPrompt, fieldId: string, value: string): StructuredPrompt {
  return { ...document, nodes: document.nodes.map(node => ({ ...node, fields: node.fields.map(field => field.id === fieldId
    ? { ...field, value: value.replace(/\r\n|\r/g, '\n'), rawValue: encodedValue(value, field.key !== null, document.newline) }
    : field) })) }
}

function normalizedName(value: string): string {
  return value.trim().normalize('NFKC').toLocaleLowerCase()
}

export function promptNameError(value: string, kind: 'node' | 'key', existing: string[]): string {
  const name = kind === 'node' ? '节点名' : '键名'
  if (!value.trim()) return '请填写' + name
  if (Array.from(value.trim()).length > 40) return name + '最多40字'
  if (/[\r\n\u0000-\u001f【】<>`#*]/.test(value) || (kind === 'key' && /[:：]/.test(value))) return name + '不能包含换行或格式分隔符'
  if (kind === 'key' && fieldStart(value.trim() + '：')?.key !== value.trim()) return '键名请使用简短文字，避免句子标点或时间、网址格式'
  if (existing.some(item => normalizedName(item) === normalizedName(value))) return name + '已存在，请换一个名称'
  return ''
}

function nextId(document: StructuredPrompt, prefix: string): string {
  const ids = new Set(document.nodes.flatMap(node => [node.id, ...node.fields.map(field => field.id)]))
  let next = 1
  while (ids.has(prefix + next)) next++
  return prefix + next
}

export function appendPromptNode(document: StructuredPrompt, name: string): StructuredPrompt {
  const error = promptNameError(name, 'node', document.nodes.map(node => node.name))
  if (error) throw new Error(error)
  const raw = serializeStructuredPrompt(document)
  const gap = !raw || /(?:\r\n|\r|\n)$/.test(raw) ? '' : document.newline
  const existing = raw ? document.nodes : document.nodes.filter(node => node.heading)
  return { ...document, nodes: [...existing, { id: nextId(document, 'n'), name: name.trim(), heading: gap + '【' + name.trim() + '】' + document.newline, fields: [], origin: 'user' }] }
}

// One inline box accepts a bare node name or a headed block with key/value
// lines. Validate the entire addition before emitting any changes.
export function appendPromptNodesFromText(document: StructuredPrompt, text: string): StructuredPrompt {
  const lines = text.replace(/\r\n|\r/g, '\n').split('\n')
  const first = lines.findIndex(line => !!line.trim())
  if (first < 0) return document
  let next = document
  let name = headingName(lines[first]!) ?? lines[first]!.trim()
  let body: string[] = []
  const finish = () => {
    next = appendPromptNode(next, name)
    next = appendPromptFieldsFromText(next, next.nodes.at(-1)!.id, body.join('\n'))
  }
  for (const line of lines.slice(first + 1)) {
    const heading = headingName(line)
    if (heading) { finish(); name = heading; body = [] }
    else body.push(line)
  }
  finish()
  return next
}

export function appendPromptField(document: StructuredPrompt, nodeId: string, key: string, value: string): StructuredPrompt {
  const target = document.nodes.find(node => node.id === nodeId)
  if (!target) throw new Error('当前节点已变化，请重新添加')
  const error = promptNameError(key, 'key', target.fields.flatMap(field => field.key === null ? [] : [field.key]))
  if (error) throw new Error(error)
  const raw = target.heading + target.fields.map(field => field.prefix + field.rawValue + field.suffix).join('')
  const gap = !raw || /(?:\r\n|\r|\n)$/.test(raw) ? '' : document.newline
  const field: PromptField = { id: nextId(document, 'f'), origin: 'user', key: key.trim(), prefix: gap + key.trim() + '：', value: value.replace(/\r\n|\r/g, '\n'), rawValue: encodedValue(value, true, document.newline), suffix: document.newline }
  return { ...document, nodes: document.nodes.map(node => node.id === nodeId ? { ...node, fields: [...node.fields, field] } : node) }
}

// Parse user additions atomically. Unindented lines start key/value pairs;
// indented continuations are values, even when they contain structural syntax.
export function appendPromptFieldsFromText(document: StructuredPrompt, nodeId: string, text: string): StructuredPrompt {
  if (!text.trim()) return document
  const fields: { key: string; value: string }[] = []
  const lines = text.replace(/\r\n|\r/g, '\n').split('\n')
  for (const [index, line] of lines.entries()) {
    const previous = fields.at(-1)
    if (/^(?: {4}|\t)/.test(line)) {
      if (!previous) throw new Error('第' + (index + 1) + '行请先填写“键名：内容”')
      previous.value += '\n' + line.replace(/^(?: {4}|\t)/, '')
      continue
    }
    if (!line.trim()) continue
    const match = /^([^:：]+?)[:：][ \t]*(.*)$/.exec(line)
    if (!match) throw new Error('第' + (index + 1) + '行请使用“键名：内容”；多行内容请在续行前加4个空格或Tab')
    fields.push({ key: match[1]!.trim(), value: match[2]! })
  }
  let next = document
  for (const field of fields) next = appendPromptField(next, nodeId, field.key, field.value)
  return next
}
