import { computed, onScopeDispose, ref, watch } from 'vue'
import { adoptLiveAgentPlanFacts, chatProductSupplement } from './api'
import type { SpeechMinerFactDraft, SpeechMinerMessage } from './types'

export type ProductSupplementScope = { planId: number; tenantId?: number; roomId?: number; actorKey: string }
type Services = { chat: typeof chatProductSupplement; adopt: typeof adoptLiveAgentPlanFacts }
const identity = (fact: SpeechMinerFactDraft) => JSON.stringify([fact.category, fact.key])
const sameFact = (a: SpeechMinerFactDraft, b: SpeechMinerFactDraft) => identity(a) === identity(b) && a.value === b.value && a.source_quote === b.source_quote

export function useProductSupplementChat(
  scope: () => ProductSupplementScope,
  updated: (scope: ProductSupplementScope) => void,
  services: Services = { chat: chatProductSupplement, adopt: adoptLiveAgentPlanFacts },
) {
  const messages = ref<SpeechMinerMessage[]>([])
  const input = ref('')
  const drafts = ref<SpeechMinerFactDraft[]>([])
  const busy = ref<'chat' | 'save' | ''>('')
  const error = ref('')
  const notice = ref('')
  const ready = computed(() => scope().planId > 0 && !!scope().actorKey)
  let epoch = 0
  let controller: AbortController | undefined
  let sessionId = ''
  watch(() => JSON.stringify(scope()), () => {
    epoch++
    controller?.abort()
    messages.value = []; drafts.value = []; input.value = ''; busy.value = ''; error.value = ''; notice.value = ''; sessionId = ''
  }, { flush: 'sync' })
  onScopeDispose(() => { epoch++; controller?.abort() })

  async function deliver(message: string, focusJson = '', preserveInput = false) {
    if (!ready.value || busy.value || !message) return
    if ([...message].length > 3000) { error.value = '每次最多发送3000字，请分段补充。'; return }
    if (messages.value.length > 56) { error.value = '本轮对话已较长，请先保存或取消待确认资料，再开始新一轮。'; return }
    if (drafts.value.length >= 12) { error.value = '请先保存或取消待确认资料，再继续补充。'; return }
    const capturedScope = { ...scope() }, token = epoch
    const current = () => token === epoch && JSON.stringify(capturedScope) === JSON.stringify(scope())
    const history = messages.value.map(item => ({ ...item })).slice(-16)
    const userMessage: SpeechMinerMessage = { role: 'user', text: message }
    messages.value.push(userMessage)
    if (!preserveInput) input.value = ''
    error.value = ''; notice.value = ''; busy.value = 'chat'
    controller = new AbortController()
    try {
      const result = await services.chat(capturedScope.planId, {
        tenant_id: capturedScope.tenantId, message, history, draft_facts: drafts.value.map(item => ({ ...item })),
        focus_gap: focusJson || undefined,
      }, controller.signal)
      if (!current()) return
      if (!result.reply?.trim()) throw new Error('智能体未返回回复，请重试。')
      const next = [...drafts.value]
      for (const fact of result.captured_facts || []) {
        // Preserve only material traced to this user's message; IDs belong to the program.
        if (!fact.category || !fact.key?.trim() || !fact.value?.trim() || !fact.source_quote?.trim() || !message.includes(fact.source_quote)) continue
        const index = next.findIndex(item => identity(item) === identity(fact))
        if (index >= 0) next[index] = { ...fact }
        else next.push({ ...fact })
      }
      drafts.value = next
      messages.value.push({ role: 'agent', text: result.reply })
      if (next.length > 12) notice.value = '已保留全部资料，请分批确认保存。'
    } catch (cause) {
      if (!current()) return
      messages.value.pop()
      // Keep the original text available for retry; do not overwrite new typing.
      if (!input.value) input.value = message
      error.value = cause instanceof Error ? cause.message : '智能体暂时没有响应，请重试。'
    } finally { if (current()) busy.value = '' }
  }

  async function send() {
    await deliver(input.value.trim())
  }

  async function askGap(focusJson: string, triggerText: string) {
    await deliver(triggerText, focusJson, true)
  }

  function discard() {
    if (busy.value) return
    drafts.value = []
    sessionId = ''
    messages.value.push({ role: 'agent', text: '待确认资料已取消，尚未保存。你可以继续补充。' })
    notice.value = ''
    error.value = ''
    messages.value = messages.value.slice(-16)
  }

  async function save() {
    if (!ready.value || busy.value || !drafts.value.length) return
    const capturedScope = { ...scope() }, token = epoch
    const current = () => token === epoch && JSON.stringify(capturedScope) === JSON.stringify(scope())
    const batch = drafts.value.slice(0, 12).map(item => ({ ...item }))
    if (!sessionId) sessionId = crypto.randomUUID()
    busy.value = 'save'; error.value = ''; notice.value = ''
    try {
      const result = await services.adopt(capturedScope.planId, batch.map(item => ({
        ...item, status: 'confirmed', review_bucket: 'adoptable', note: '用户确认的商品资料补充',
      })), capturedScope.tenantId, 'product-supplement:' + sessionId, { suppressErrorDialog: true })
      if (!current()) return
      const saved = (result.results || []).filter(item => item.status === 'adopted' || item.status === 'unchanged')
      // The API returns one result per input in order; normalization may rename it.
      // Do not match a normalized value against the original draft and save it twice.
      const completeResults = result.results?.length === batch.length
      const savedInputs = completeResults ? batch.filter((_, index) => ['adopted', 'unchanged'].includes(result.results[index]!.status)) : []
      drafts.value = drafts.value.filter(item => !savedInputs.some(saved => sameFact(item, saved)))
      const unresolved = (result.results || []).filter(item => item.status !== 'adopted' && item.status !== 'unchanged')
      if (unresolved.length) error.value = unresolved.map(item => `${item.candidate.key}：${item.message || '本条尚未保存'}`).join('\n')
      if (saved.length) {
        const text = `已保存${result.adopted || 0}条补充资料${result.skipped ? `，${result.skipped}条已存在` : ''}。可在“事实依据”查看，并点击顶部“保存方案”。`
        messages.value.push({ role: 'agent', text })
        notice.value = text
      } else if (!unresolved.length) error.value = '未收到保存结果，请重试或到“事实依据”核对。'
      if (!completeResults) error.value = '保存结果不完整，请到“事实依据”核对后再试。'
      updated(capturedScope)
      if (!drafts.value.length) { sessionId = ''; messages.value = messages.value.slice(-16) }
    } catch (cause) {
      if (!current()) return
      error.value = cause instanceof Error ? cause.message : '保存资料未完成，请重试。'
      // A failed response may follow a partial save. Refresh actual assets before retry.
      updated(capturedScope)
    } finally { if (current()) busy.value = '' }
  }

  return { messages, input, drafts, busy, ready, error, notice, send, askGap, save, discard }
}
