// Display terminology only. Financial qualification and write permissions stay server-owned.
export function contactRecordTitle(qualified?: boolean | null): string {
  if (qualified === true) return '售后回访记录'
  if (qualified === false) return '跟进记录'
  return '客户联系记录'
}

export function contactActionLabel(qualified?: boolean | null): string {
  if (qualified === true) return '售后回访记录'
  if (qualified === false) return '记录跟进'
  return '联系记录'
}

// Historical pre-payment communication must not become after-sales history just
// because the customer paid later. Missing evidence remains unclassified.
export function historicalContactTitle(
  createdAt: string,
  qualified?: boolean | null,
  confirmedAt?: string,
): string {
  if (qualified === false) return '跟进记录'
  const created = Date.parse(createdAt)
  const confirmed = confirmedAt ? Date.parse(confirmedAt) : NaN
  if (qualified !== true || !Number.isFinite(created) || !Number.isFinite(confirmed)) {
    return '客户联系记录'
  }
  return contactRecordTitle(created >= confirmed)
}
