// Presentation only. Stored financial evidence and business events are unchanged.
export function historyDisplayNote(event: { action: string; note: string }): string {
  if (event.action !== 'review_policy') return event.note
  const note = event.note.replace(/^审核规则：不强制分人，按审核权限办理[；;]?\s*/, '')
  return note || '按当时审核配置办理'
}
