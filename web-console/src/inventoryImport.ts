export interface InventoryCodes { sns: string[]; macs: string[] }

export function splitInventoryCodes(text: string): string[] {
  return text.split(/[\s,，;；]+/).map(value => value.trim()).filter(Boolean)
}

export function normalizeInventoryMAC(raw: string): string {
  const value = raw.trim().toLowerCase()
  let hex: string
  if (/^[0-9a-f]{12}$/.test(value)) hex = value
  else if (/^([0-9a-f]{2}:){5}[0-9a-f]{2}$/.test(value)) hex = value.replaceAll(':', '')
  else if (/^([0-9a-f]{2}-){5}[0-9a-f]{2}$/.test(value)) hex = value.replaceAll('-', '')
  else if (/^([0-9a-f]{4}\.){2}[0-9a-f]{4}$/.test(value)) hex = value.replaceAll('.', '')
  else throw new Error(`MAC 格式不正确：${raw}，例如 1c:29:04:31:0e:b8`)
  return hex.match(/.{2}/g)!.join(':')
}

export function validateInventoryCodes(sns: string[], macs: string[]): InventoryCodes {
  if (!sns.length) throw new Error('请录入至少一个 SN')
  const snSet = new Set<string>()
  for (const sn of sns) {
    const key = sn.toLowerCase()
    if (sn.length > 96 || /[\s,，;；]/.test(sn)) throw new Error(`SN 格式不正确：${sn}，不能包含空格或分隔符`)
    if (snSet.has(key)) throw new Error(`SN 重复：${sn}，请删除重复项，不会自动去重`)
    snSet.add(key)
  }
  if (macs.length && macs.length !== sns.length) throw new Error(`已录 ${sns.length} 个 SN、${macs.length} 个 MAC，数量必须相同并按顺序对应`)
  const normalized = macs.map(normalizeInventoryMAC)
  const macSet = new Set<string>()
  for (const mac of normalized) {
    if (macSet.has(mac)) throw new Error(`MAC 重复：${mac}，请检查设备对应关系`)
    macSet.add(mac)
  }
  return { sns, macs: normalized }
}

// Quoted CSV fields, escaped quotes, UTF-8 BOM, CRLF and Excel's tab/semicolon exports.
function parseDelimited(text: string, delimiter: string): string[][] {
  const rows: string[][] = []
  let row: string[] = [], value = '', quoted = false
  for (let i = 0; i < text.length; i++) {
    const c = text[i]!
    if (quoted) {
      if (c === '"' && text[i + 1] === '"') { value += '"'; i++ }
      else if (c === '"') quoted = false
      else value += c
    } else if (c === '"' && !value.trim()) quoted = true
    else if (c === delimiter) { row.push(value.trim()); value = '' }
    else if (c === '\r' || c === '\n') {
      if (c === '\r' && text[i + 1] === '\n') i++
      row.push(value.trim()); rows.push(row); row = []; value = ''
    } else value += c
  }
  if (quoted) throw new Error('CSV 引号未闭合，请检查文件内容')
  row.push(value.trim()); rows.push(row)
  return rows.filter(cells => cells.some(Boolean))
}

export function parseInventoryImport(content: string, filename = 'devices.csv'): InventoryCodes {
  const text = content.replace(/^\uFEFF/, '').trim()
  if (!text) throw new Error('文件为空，请先填写设备 SN 和 MAC')
  const first = text.split(/\r?\n/)[0] || ''
  const delimiter = first.includes('\t') ? '\t' : first.includes(';') && !first.includes(',') ? ';' : ','
  const rows = parseDelimited(text, delimiter)
  const headerKey = (s: string) => s.toLowerCase().replace(/[\s_\-]/g, '')
  const headers = rows[0]!.map(headerKey)
  const snHeaders = ['sn', '设备sn', 'sn码', '序列号', '设备序列号']
  const macHeaders = ['mac', '设备mac', 'mac地址', '设备mac地址']
  const snIndex = headers.findIndex(value => snHeaders.includes(value))
  const macIndex = headers.findIndex(value => macHeaders.includes(value))
  const hasHeader = snIndex >= 0 || macIndex >= 0
  if (hasHeader && snIndex < 0) throw new Error('模板缺少 SN 列，请使用下载的模板')
  if (hasHeader && headers.some(value => !snHeaders.includes(value) && !macHeaders.includes(value))) throw new Error('模板表头必须是 SN、MAC 两列，或单独的 SN 列')
  if (hasHeader && (headers.filter(value => snHeaders.includes(value)).length > 1 || headers.filter(value => macHeaders.includes(value)).length > 1)) throw new Error('模板表头不能重复')
  // Legacy TXT remains a plain SN list; paired imports always use a header or CSV/TSV.
  if (!hasHeader && filename.toLowerCase().endsWith('.txt') && !first.includes('\t')) return validateInventoryCodes(splitInventoryCodes(text), [])
  const data = hasHeader ? rows.slice(1) : rows
  if (!data.length) throw new Error('模板只有表头，请在第 2 行开始填写设备，一行一台')
  const snColumn = hasHeader ? snIndex : 0
  const macColumn = hasHeader ? macIndex : data.some(row => row.length > 1) ? 1 : -1
  const sns: string[] = [], macs: string[] = []
  for (let i = 0; i < data.length; i++) {
    const row = data[i]!
    const line = i + (hasHeader ? 2 : 1)
    if (row.length > (hasHeader ? headers.length : 2)) throw new Error(`第 ${line} 行列数不正确，请按模板填写 SN、MAC 两列`)
    const sn = row[snColumn] || ''
    if (!sn) throw new Error(`第 ${line} 行缺少 SN，不会跳过该行`)
    sns.push(sn); if (macColumn >= 0) macs.push(row[macColumn] || '')
  }
  if (macs.some(Boolean) && macs.some(value => !value)) throw new Error('部分设备缺少 MAC，请补齐每一行，避免 SN 和 MAC 错位')
  return validateInventoryCodes(sns, macs.some(Boolean) ? macs : [])
}

export function mergeInventoryCodes(existing: InventoryCodes, incoming: InventoryCodes): InventoryCodes {
  if (existing.sns.length && Boolean(existing.macs.length) !== Boolean(incoming.macs.length)) throw new Error('已有数据和导入文件的 MAC 填写方式不同，请补齐 MAC 或清空已有编码再导入')
  return validateInventoryCodes([...existing.sns, ...incoming.sns], [...existing.macs, ...incoming.macs])
}
