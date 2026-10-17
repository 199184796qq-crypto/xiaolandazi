<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { session } from '../session'
import { useAgentEnterSend } from '../useAgentEnterSend'
import { useProductSupplementChat } from '../productSupplementChat'
import { factCategoryDefinition, loadFactCategories } from '../factCategories'
import { type FactGapItem } from '../api'
import { useProductSupplementGaps } from '../productSupplementGaps'
import { type SpeechMinerFactDraft, type LiveAgentPlanProductLink } from '../types'
import FactCategoryBadge from './FactCategoryBadge.vue'
import AgentEnterSendOption from './AgentEnterSendOption.vue'
import ChatMessageContent from './ChatMessageContent.vue'
import documentsIllustration from '../assets/product-supplement-documents.svg'
import assistantAvatar from '../assets/product-supplement-assistant.svg'

const props = defineProps<{ planId: number; tenantId?: number; roomId?: number; productLinks?: LiveAgentPlanProductLink[]; businessLabel?: string; industryName?: string }>()
const emit = defineEmits<{ updated: [scope: { planId: number; tenantId: number; roomId: number }] }>()
const currentScope = () => ({
  planId: props.planId, tenantId: props.tenantId, roomId: props.roomId,
  actorKey: session.bootstrap?.actor ? `${session.bootstrap.actor.tenant_id}:${session.bootstrap.actor.user_id}` : '',
})
const selectedProductLink = ref('')
const selectedProductLinkId = () => Number(selectedProductLink.value) || undefined
const chat = useProductSupplementChat(currentScope, scope => emit('updated', { planId: scope.planId, tenantId: scope.tenantId || 0, roomId: scope.roomId || 0 }), undefined, selectedProductLinkId)
const { messages, input, drafts, busy, ready, error, notice, skip, save } = chat
const enterSend = useAgentEnterSend()
const composing = ref(false)
const transcript = ref<HTMLElement | null>(null)
const confirmation = ref<HTMLElement | null>(null)
const inputBox = ref<HTMLTextAreaElement | null>(null)
const filePicker = ref<HTMLInputElement | null>(null)
const importingFile = ref(false)
const fileError = ref('')
const fileNotice = ref('')
const examplesOpen = ref(false)
const canSend = computed(() => ready.value && !busy.value && !importingFile.value && !!input.value.trim())
const quickExamples = [
  { title: '原料与工艺', text: '我想补充商品的原料和制作工艺，你可以先问我。' },
  { title: '配送与保存', text: '我想补充配送和保存要求，请帮我逐项整理。' },
  { title: '品牌与产地', text: '我有一段品牌和产地资料，想整理到当前方案里。' },
]
let fileEpoch = 0
watch(() => [props.planId, props.tenantId, props.roomId, session.bootstrap?.actor?.tenant_id, session.bootstrap?.actor?.user_id], () => {
  fileEpoch++; importingFile.value = false; fileError.value = ''; fileNotice.value = ''; examplesOpen.value = false
})

async function importFile(event: Event) {
  const picker = event.target as HTMLInputElement
  const file = picker.files?.[0]
  picker.value = ''
  if (!file || !ready.value || busy.value || importingFile.value) return
  fileError.value = ''; fileNotice.value = ''
  if (!/\.(txt|md)$/i.test(file.name)) { fileError.value = '请选择 TXT 或 Markdown 文本文件。'; return }
  if (file.size > 1024 * 1024) { fileError.value = '文件过大，请将资料分成小段补充。'; return }
  const token = ++fileEpoch
  importingFile.value = true
  try {
    const buffer = await file.arrayBuffer()
    if (token !== fileEpoch) return
    let text: string
    try { text = new TextDecoder('utf-8', { fatal: true }).decode(buffer).trim() }
    catch { text = new TextDecoder('gb18030', { fatal: true }).decode(buffer).trim() }
    if (!text || text.includes('\0')) throw new Error('文件没有可用的文字内容，请检查后重新选择。')
    const combined = [input.value.trim(), text].filter(Boolean).join('\n\n')
    if ([...combined].length > 3000) throw new Error('每次最多发送3000字，请将文件内容分段补充。')
    input.value = combined
    fileNotice.value = `已读入“${file.name}”，确认内容后点击发送。`
    await nextTick(); inputBox.value?.focus()
  } catch (cause) {
    if (token === fileEpoch) fileError.value = cause instanceof Error ? cause.message : '读取文件失败，请重试。'
  } finally { if (token === fileEpoch) importingFile.value = false }
}

async function chooseExample(text: string) {
  if (!ready.value || busy.value || importingFile.value) return
  input.value = [input.value.trim(), text].filter(Boolean).join('\n\n')
  examplesOpen.value = false
  await nextTick(); inputBox.value?.focus()
}

watch(() => [props.planId, props.tenantId, props.roomId, session.bootstrap?.actor?.user_id], () => {
  void loadFactCategories({ planId: props.planId, suppressErrorDialog: true })
}, { immediate: true })

// The checklist and conversation are independent, always-visible panels.
const checklist = useProductSupplementGaps(() => ({ ...currentScope(), productLinkId: selectedProductLinkId() }))
const { gaps, loading: gapsLoading, error: gapsError, skipped } = checklist
const visibleGaps = computed(() => gaps.value.filter(gap => !skipped.value.includes(gap['维度'])))
const availableProductLinks = computed(() => (props.productLinks || []).filter(link => link.plan_id === props.planId && (!props.tenantId || link.tenant_id === props.tenantId)))
watch(() => [props.planId, props.tenantId, props.roomId, session.bootstrap?.actor?.tenant_id, session.bootstrap?.actor?.user_id], () => { selectedProductLink.value = '' }, { flush: 'sync' })
watch(availableProductLinks, links => {
  if (selectedProductLink.value && !links.some(link => String(link.id) === selectedProductLink.value)) selectedProductLink.value = ''
})
function productLinkLabel(link: LiveAgentPlanProductLink) {
  const key = link.link_key.trim()
  return /(?:号链接|链接)$/.test(key) ? key : `${key}号链接`
}


// 所属行业与方案「业务模式」联动：这里只读展示业务模式 + 已选行业（红字），选择与修改都在业务模式面板完成。
const industryText = computed(() => {
  const business = (props.businessLabel || '').trim()
  const industry = (props.industryName || '').trim()
  if (business && industry) return `${business} · ${industry}`
  if (industry) return industry
  return '未选择业务模式或行业'
})

// 卖点分级确认 UI 暂时下线，后续改到右侧智能体对话框里重新设计。


async function openGap(gap: FactGapItem) {
  if (!ready.value || busy.value || importingFile.value) return
  examplesOpen.value = false
  const trigger = `我想补充「${gap['待补事实']}」这项资料`
  await chat.askGap(JSON.stringify(gap), trigger)
  await nextTick(); inputBox.value?.focus()
}
function skipGap(gap: FactGapItem) {
  if (!skipped.value.includes(gap['维度'])) skipped.value.push(gap['维度'])
}
async function focusComposer() { await nextTick(); inputBox.value?.focus() }
async function showConfirmation() { await nextTick(); confirmation.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }

interface DraftGroup { category: string; items: SpeechMinerFactDraft[] }
const groupedDrafts = computed<DraftGroup[]>(() => {
  const groups: DraftGroup[] = []
  for (const fact of drafts.value) {
    let group = groups.find(item => item.category === fact.category)
    if (!group) { group = { category: fact.category, items: [] }; groups.push(group) }
    group.items.push(fact)
  }
  return groups
})
function categoryLabel(category: string) { return factCategoryDefinition(category).label }
function categoryFamily(category: string) { return factCategoryDefinition(category).family }

async function send() {
  if (importingFile.value) return
  examplesOpen.value = false; fileNotice.value = ''; fileError.value = ''
  await chat.send(); await nextTick(); inputBox.value?.focus()
}
function keydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || !enterSend.value || event.shiftKey || event.ctrlKey || event.altKey || event.metaKey || event.isComposing || composing.value || event.keyCode === 229) return
  event.preventDefault()
  if (!event.repeat && canSend.value) void send()
}
watch(() => [messages.value.length, drafts.value.length, busy.value], async () => {
  await nextTick()
  if (transcript.value) transcript.value.scrollTop = transcript.value.scrollHeight
})
</script>

<template>
  <div class="product-supplement-chat">
    <div class="supplement-layout">
      <aside class="supplement-gap-list" aria-label="补全清单">
        <header class="supplement-gap-list-head">
          <div class="supplement-category">
            <label>所属行业</label>
            <span class="supplement-industry-text">{{ industryText }}</span>
          </div>
          <div class="supplement-link-toolbar">
            <div class="supplement-link-field">
              <label for="supplement-product-link">链接选择</label>
              <select id="supplement-product-link" v-model="selectedProductLink" :disabled="!ready || !!busy">
                <option value="">全部</option>
                <option v-for="link in availableProductLinks" :key="link.id" :value="String(link.id)">{{ productLinkLabel(link) }}</option>
              </select>
            </div>
            <button type="button" class="supplement-gap-refresh" :disabled="!ready || gapsLoading || !!busy" :aria-busy="gapsLoading" @click="checklist.reload"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 0 0-14-5L3 9M3 3v6h6M4 13a8 8 0 0 0 14 5l3-3M21 21v-6h-6" /></svg>{{ gapsLoading ? '正在换清单…' : '换下一批清单' }}</button>
          </div>
          <small class="supplement-gap-description">{{ visibleGaps.length }} 项 · 按必查维度整理，点一条开始问；已有的不重复问，只补缺的或在已有基础上深挖一层</small>
        </header>
        <div v-if="gapsLoading" class="supplement-gap-state">正在核对商品资料…</div>
        <div v-else-if="gapsError" class="supplement-gap-state is-error" role="alert">{{ gapsError }}</div>
        <div v-else-if="!visibleGaps.length" class="supplement-gap-state supplement-gap-empty"><img class="supplement-documents-art" :src="documentsIllustration" alt="" /><p>{{ gaps.length ? '本轮问题已跳过，可换下一批清单或在右侧自由补充。' : '目前没有待补问题，可以在右侧自由补充商品资料。' }}</p></div>
        <div v-else class="supplement-gap-cards">
          <section v-for="gap in visibleGaps" :key="gap['维度']" class="supplement-gap-card">
            <div class="supplement-gap-card-head">
              <span class="supplement-gap-level" :class="'is-' + (gap['层级'] || 'missing')">{{ gap['层级'] === 'extend' ? '可拓展' : '缺失' }}</span>
              <strong>{{ gap['维度'] }}</strong>
            </div>
            <p class="supplement-gap-missing">{{ gap['待补事实'] }}</p>
            <ul v-if="gap['填写提示']?.length" class="supplement-gap-prompts">
              <li v-for="(prompt, index) in gap['填写提示']" :key="index">{{ prompt }}</li>
            </ul>
            <div class="supplement-gap-actions">
              <button type="button" class="supplement-gap-ask" :disabled="!ready || !!busy || importingFile" @click="openGap(gap)">回答这个</button>
              <button type="button" class="supplement-gap-skip" :disabled="!!busy" @click="skipGap(gap)">跳过</button>
            </div>
          </section>
        </div>
        <button type="button" class="supplement-gap-free" :disabled="!ready || !!busy || importingFile" @click="focusComposer">在右侧直接补充资料 →</button>
      </aside>

      <div class="supplement-chat-panel">
        <!-- Conversation stays mounted while the checklist is used. -->
        <div class="supplement-chat-view">
          <header class="supplement-chat-head"><strong>资料助手</strong><small>选择左侧问题开始问，或直接输入商品资料。</small></header>
          <div ref="transcript" class="supplement-transcript" role="log" aria-label="商品资料补充对话" aria-live="polite" :aria-busy="busy === 'chat'">
            <article class="supplement-message is-agent">
              <img class="supplement-assistant-avatar" :src="assistantAvatar" alt="" />
              <div class="supplement-message-content"><span class="supplement-speaker">资料助手</span><p>你想补充商品的哪些资料？直接告诉我，我会帮你整理；不清楚的地方，我们可以继续聊。</p></div>
            </article>
            <div v-if="!messages.length && !busy" class="supplement-chat-doodle" aria-hidden="true">有什么需要补充的？<br />我来帮你整理～<svg viewBox="0 0 220 20"><path d="M3 17C62 1 146 0 217 5"/></svg></div>
            <article v-for="(message, index) in messages" :key="index" class="supplement-message" :class="message.role === 'user' ? 'is-user' : 'is-agent'">
              <img v-if="message.role !== 'user'" class="supplement-assistant-avatar" :src="assistantAvatar" alt="" />
              <div class="supplement-message-content">
                <span class="supplement-speaker">{{ message.role === 'user' ? '我' : '资料助手' }}</span>
                <p v-if="message.role === 'user'">{{ message.text }}</p>
                <div v-else class="supplement-message-body"><ChatMessageContent :text="message.text" /></div>
                <div v-if="message.role === 'agent' && message.kind === 'reply' && !busy" class="supplement-message-actions">
                  <button v-if="message.captured && drafts.length" type="button" class="supplement-action-confirm" @click="save">确定，保存这 {{ message.captured }} 条资料</button>
                  <button type="button" class="supplement-action-skip" :disabled="index !== messages.length - 1" @click="skip">跳过</button>
                </div>
              </div>
            </article>
            <article v-if="busy === 'chat'" class="supplement-message is-agent"><img class="supplement-assistant-avatar" :src="assistantAvatar" alt="" /><div class="supplement-message-content"><span class="supplement-speaker">资料助手</span><p class="supplement-thinking">正在整理<span class="supplement-thinking-dots" aria-hidden="true"><i></i><i></i><i></i></span></p></div></article>
          </div>
          <p v-if="!ready" class="supplement-notice">请先选择一个智能体方案，再补充资料。</p>
          <p v-if="error" class="supplement-error" role="alert">{{ error }}</p>
          <p v-else-if="notice" class="supplement-notice" role="status">{{ notice }}</p>
          <p v-if="fileError" class="supplement-error" role="alert">{{ fileError }}</p>
          <p v-else-if="fileNotice" class="supplement-notice" role="status">{{ fileNotice }}</p>
          <button v-if="drafts.length" type="button" class="supplement-confirmation-jump" @click="showConfirmation">已整理 {{ drafts.length }} 条待确认资料，核对并保存 ↓</button>
          <form class="supplement-composer" @submit.prevent="send" @keydown.esc="examplesOpen = false">
            <label class="supplement-input-label" for="product-supplement-input">补充商品资料</label>
            <textarea id="product-supplement-input" ref="inputBox" v-model="input" rows="4" maxlength="6000" placeholder="输入要补充的商品信息，或继续回答资料助手的问题…" :disabled="!ready || !!busy || importingFile" @keydown="keydown" @compositionstart="composing = true" @compositionend="composing = false"></textarea>
            <div class="supplement-composer-bottom">
              <div class="supplement-composer-tools">
                <input ref="filePicker" class="supplement-file-input" type="file" tabindex="-1" accept=".txt,.md,text/plain,text/markdown" aria-label="选择商品资料文本文件" :disabled="!ready || !!busy || importingFile" @change="importFile" />
                <button type="button" class="supplement-tool" :disabled="!ready || !!busy || importingFile" title="支持 TXT / Markdown，确认内容后发送" @click="filePicker?.click()"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m8 13 7-7a3 3 0 0 1 4 4l-9 9a5 5 0 0 1-7-7L13 2a4 4 0 0 1 6 6l-9 9a2 2 0 0 1-3-3l8-8"/></svg>{{ importingFile ? '读取中…' : '上传文件' }}</button>
                <button type="button" class="supplement-tool" :disabled="!ready || !!busy || importingFile" :aria-expanded="examplesOpen" aria-controls="supplement-quick-examples" @click="examplesOpen = !examplesOpen"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 20h6M10 23h4M8 16c-2-2-3-4-3-7a7 7 0 0 1 14 0c0 3-1 5-3 7l-1 2H9l-1-2Z"/></svg>快速示例</button>
                <AgentEnterSendOption label="回车发送" :disabled="!!busy || importingFile" />
              </div>
              <button type="submit" class="supplement-send" :disabled="!canSend"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m22 2-7 20-4-9L2 9l20-7Z"/><path d="m11 13 6-6"/></svg>{{ busy === 'chat' ? '发送中…' : '发送' }}</button>
            </div>
            <div v-if="examplesOpen" id="supplement-quick-examples" class="supplement-examples" aria-label="快速示例">
              <strong>选一个话题开始补充</strong><small>填入输入框，确认后再发送。</small>
              <button v-for="example in quickExamples" :key="example.title" type="button" @click="chooseExample(example.text)"><span>{{ example.title }}</span><small>{{ example.text }}</small></button>
              <button type="button" class="supplement-examples-close" @click="examplesOpen = false">收起示例</button>
            </div>
          </form>
          <section v-if="drafts.length" ref="confirmation" class="supplement-confirmation" aria-label="待确认资料">
            <header><strong>待确认资料 <span>{{ drafts.length }} 条</span></strong><small>核对以下内容后确认保存，再点击顶部“保存方案”。</small></header>
            <div class="supplement-draft-groups">
              <section v-for="group in groupedDrafts" :key="group.category" class="supplement-draft-group">
                <FactCategoryBadge :category="group.category" :label="categoryLabel(group.category)" :family="categoryFamily(group.category)" />
                <ul>
                  <li v-for="(fact, index) in group.items" :key="group.category + ':' + index">
                    <strong>{{ fact.key }}</strong>
                    <p>{{ fact.value }}</p>
                  </li>
                </ul>
            </section>
          </div>

        <footer class="supplement-drafts-actions">
          <button type="button" class="supplement-save" :disabled="!!busy || !ready" @click="chat.save">{{ busy === 'save' ? '保存中…' : '确认保存资料' }}</button>
          <button type="button" class="supplement-discard" :disabled="!!busy" @click="chat.discard">取消</button>
        </footer>
          </section>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Definite width prevents shrink-to-fit inside the strategy grid with contained children. */
.product-supplement-chat{--supplement-chat-height:clamp(380px,55vh,620px);box-sizing:border-box;width:100%;min-width:0;max-width:1560px;justify-self:center;container-type:inline-size;container-name:supplement-workspace;margin:0 auto;padding:8px 0;color:#253451}
.supplement-layout{display:grid;grid-template-columns:minmax(310px,.36fr) minmax(0,.64fr);gap:28px;align-items:start}
.supplement-documents-art{display:block;width:min(100%,180px);height:auto;margin:0 auto 16px;pointer-events:none}
.supplement-draft-group{margin-bottom:20px}.supplement-draft-group:first-child{margin-top:20px}.supplement-draft-group ul{list-style:none;padding:0;margin:12px 0 0}.supplement-draft-group li{margin-top:10px;padding:14px 15px;border:1px solid #e5eafb;border-radius:13px;background:#ffffffd9}.supplement-draft-group li strong{display:block;font-size:16px;color:#344363}.supplement-draft-group li p{margin:7px 0 0;color:#677797;font-size:16px;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}
.supplement-draft-groups{max-height:calc(var(--supplement-chat-height) + 80px);overflow-y:auto;scrollbar-width:thin}.supplement-drafts-actions{display:flex;gap:10px;padding:18px 24px;border-top:1px solid #e7ecfa;background:#ffffffb3}.supplement-drafts-actions button{border:1px solid #dce1f6;border-radius:11px;background:#fff;color:#667594;padding:11px 16px;font:inherit;font-size:16px;cursor:pointer}.supplement-drafts-actions .supplement-save{flex:1;border-color:transparent;background:linear-gradient(115deg,#8675fa,#6351f8);color:#fff}
.supplement-chat-panel{min-width:0;container-type:inline-size;container-name:supplement-chat}.supplement-chat-view{display:flex;flex-direction:column;min-width:0}
.supplement-transcript{position:relative;display:flex;flex-direction:column;gap:24px;box-sizing:border-box;height:var(--supplement-chat-height);overflow-y:auto;padding:26px 22px;border:1px solid #e5eaff;border-radius:22px;background:linear-gradient(135deg,#f7f9ff,#f5f7ff 58%,#f1f4ff);overscroll-behavior:contain;scrollbar-width:thin}
.supplement-message{position:relative;z-index:1;display:flex;align-items:flex-start;gap:13px;width:fit-content;max-width:88%;min-width:0;overflow-wrap:anywhere}.supplement-assistant-avatar{flex:0 0 62px;width:62px;height:62px;margin-top:-5px;pointer-events:none}.supplement-message-content{min-width:0}.supplement-speaker{display:block;margin:0 0 9px;color:#7b6cf0;font-size:16px;font-weight:700;line-height:1.5}.supplement-message-content>p,.supplement-message-body{margin:0;padding:15px 20px;border:1px solid #e6eaff;border-radius:17px;background:#ffffffed;color:#314463;font-size:16px;white-space:pre-wrap;line-height:1.85;box-shadow:0 2px 5px #9da8d505}
.supplement-message.is-user{align-self:flex-end;max-width:80%}.supplement-message.is-user .supplement-speaker{text-align:right;color:#8980b8}.supplement-message.is-user p{background:#efebff;border-color:#e3dcff;color:#51456f}
.supplement-message-actions{display:flex;gap:10px;margin-top:10px}.supplement-message-actions button{border:1px solid #d8ddf3;border-radius:10px;background:#fff;color:#66759c;padding:8px 16px;font:inherit;font-size:16px;line-height:1.5;cursor:pointer}.supplement-message-actions .supplement-action-confirm{border-color:transparent;background:linear-gradient(115deg,#8675fa,#6351f8);color:#fff}.supplement-message-actions button:not(:disabled):hover{box-shadow:0 4px 12px #8174e22e}.supplement-message-actions button:disabled{opacity:.45;cursor:default}
.supplement-chat-doodle{position:absolute;right:25px;top:58px;color:#b4adff;font-family:KaiTi,STKaiti,serif;font-size:16px;font-style:italic;line-height:1.7;letter-spacing:1px;transform:rotate(-12deg);text-align:center;pointer-events:none}.supplement-chat-doodle::before{content:'';position:absolute;width:30px;height:30px;border-radius:50%;left:6px;top:-27px;background:#e6eaff;z-index:-1}.supplement-chat-doodle svg{display:block;width:200px;height:20px;fill:none;stroke:#c0b8ff;stroke-width:2.5;stroke-linecap:round}
.supplement-transcript:has(.supplement-chat-doodle)>.supplement-message{max-width:72%}
.supplement-composer{position:relative;box-sizing:border-box;margin-top:20px;padding:3px 6px 6px;border:1px solid #e0dfff;border-radius:19px;background:#fff;box-shadow:0 4px 12px #8074dc08}.supplement-composer:focus-within{border-color:#ada4fa;box-shadow:0 0 0 3px #edeaff}
.supplement-input-label,.supplement-file-input{position:absolute;width:1px;height:1px;padding:0;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.supplement-composer textarea{display:block;box-sizing:border-box;width:100%;min-height:110px;max-height:280px;padding:14px 16px;border:0;border-radius:15px;background:transparent;color:#314463;font:inherit;font-size:16px;line-height:1.8;resize:vertical}.supplement-composer textarea::placeholder{color:#9aa6c0}.supplement-composer textarea:focus{outline:none}.supplement-composer textarea:disabled{background:transparent}
.supplement-composer-bottom{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px;padding:8px 10px 7px}.supplement-composer-tools{display:flex;flex-wrap:wrap;align-items:center;gap:22px}.supplement-tool{display:inline-flex;align-items:center;gap:8px;padding:4px 0;border:0;background:transparent;color:#8795b7;font:inherit;font-size:16px;line-height:1.5;cursor:pointer}.supplement-tool svg{width:19px;height:19px;fill:none;stroke:#63769b;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}.supplement-tool:hover{color:#7160ee}.supplement-composer-tools :deep(.agent-enter-send-option){font-size:16px;color:#8795b7}.supplement-composer-tools :deep(.agent-enter-send-option input){width:17px;height:17px;accent-color:#7460f8}
.supplement-send{display:inline-flex;justify-content:center;align-items:center;gap:9px;min-width:144px;padding:13px 22px;border:0;border-radius:12px;background:linear-gradient(110deg,#8978fa,#6652f7);color:#fff;font:inherit;font-size:16px;font-weight:650;cursor:pointer;box-shadow:0 4px 10px #8174e21c}.supplement-send svg{width:19px;height:19px;fill:#fff;stroke:#fff;stroke-width:1.4;stroke-linecap:round;stroke-linejoin:round}.supplement-send svg path+path{fill:none;stroke:#9181f8}
.product-supplement-chat button:disabled{opacity:.5;cursor:default;box-shadow:none}.product-supplement-chat button:focus-visible{outline:3px solid #b4affb;outline-offset:3px}.product-supplement-chat button:not(:disabled){transition:background .15s,box-shadow .15s}.supplement-send:not(:disabled):hover{box-shadow:0 5px 14px #8174e24d}
.supplement-error,.supplement-notice{margin:12px 0 0;padding:10px 13px;border-radius:10px;white-space:pre-wrap;line-height:1.6;font-size:16px}.supplement-error{color:#ad4256;background:#fff2f4}.supplement-notice{color:#647596;background:#f2f5fc}.supplement-message .supplement-thinking{display:flex;align-items:center;gap:10px;color:#8b94ad}.supplement-thinking-dots{display:inline-flex;gap:4px}.supplement-thinking-dots i{width:4px;height:4px;border-radius:50%;background:#a699f6;animation:supplement-dot 1.4s infinite}.supplement-thinking-dots i:nth-child(2){animation-delay:.2s}.supplement-thinking-dots i:nth-child(3){animation-delay:.4s}@keyframes supplement-dot{0%,70%,100%{opacity:.35}35%{opacity:1}}
.supplement-examples{position:absolute;z-index:5;bottom:64px;left:16px;box-sizing:border-box;width:min(360px,calc(100% - 32px));padding:16px;border:1px solid #e1e2fc;border-radius:16px;background:#fff;box-shadow:0 12px 36px #4f44871c}.supplement-examples>strong{display:block;font-size:16px;color:#354369}.supplement-examples>small{display:block;margin:5px 0 12px;font-size:16px;color:#929db6}.supplement-examples button{display:block;width:100%;margin-top:8px;padding:10px 12px;border:1px solid #eaeafb;border-radius:10px;background:#f9f9ff;text-align:left;color:#5b528b;font:inherit;cursor:pointer}.supplement-examples button span{display:block;font-size:16px;font-weight:650}.supplement-examples button small{display:block;margin-top:4px;font-size:16px;color:#8490aa;line-height:1.6}.supplement-examples .supplement-examples-close{border:0;background:none;text-align:center;padding:4px;font-size:16px;color:#8c94ab}
/* Independent checklist and confirmation cards do not replace the conversation. */
.supplement-chat-head{margin:0 0 18px;padding:0 4px}.supplement-chat-head strong{display:block;color:#263752;font-size:16px;line-height:1.5}.supplement-chat-head small{display:block;margin-top:6px;font-size:16px;line-height:1.7;color:#8490ac}
.supplement-confirmation-jump{align-self:flex-start;margin-top:14px;border:0;background:#f1efff;color:#7460de;border-radius:9px;padding:9px 12px;font:inherit;font-size:16px;cursor:pointer}
.supplement-confirmation{margin-top:20px;border:1px solid #e4e8fb;border-radius:19px;background:#f8faff;overflow:hidden}.supplement-confirmation>header{padding:20px 24px 0}.supplement-confirmation>header strong{display:block;color:#273953;font-size:16px}.supplement-confirmation>header strong span{margin-left:8px;color:#7c6ae5;font-size:16px}.supplement-confirmation>header small{display:block;margin-top:7px;color:#8490ac;font-size:16px;line-height:1.7}.supplement-confirmation .supplement-draft-groups{padding:0 24px;max-height:320px}
.supplement-gap-list{container-type:inline-size;container-name:supplement-checklist;display:flex;flex-direction:column;min-width:0;min-height:calc(var(--supplement-chat-height) + 230px);border:1px solid #e5eaff;border-radius:22px;background:#f8faff;overflow:hidden}
.supplement-gap-list-head{display:grid;gap:16px;padding:22px;border-bottom:1px solid #e9edfc;background:#fff}
.supplement-category,.supplement-link-field{display:flex;align-items:center;gap:10px;min-width:0}
.supplement-category label,.supplement-link-field label{flex:0 0 auto;font-size:16px;font-weight:800;color:#4a5678}
.supplement-category select,.supplement-link-field select{flex:1;box-sizing:border-box;min-width:0;width:100%;min-height:44px;padding:8px 10px;border:1px solid #d8dff0;border-radius:9px;background:#fff;color:#263752;font:inherit;font-size:16px;cursor:pointer}
.supplement-category select:disabled,.supplement-link-field select:disabled{opacity:.6;cursor:wait}
.supplement-industry-text{flex:1;min-width:0;align-self:center;color:#e02f2f;font-size:17px;font-weight:750;line-height:1.5;overflow-wrap:anywhere}
.supplement-category-state{margin:0;font-size:16px;line-height:1.6;color:#7768da;overflow-wrap:anywhere}
.supplement-link-toolbar{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:12px}
.supplement-gap-description{color:#8490ac;font-size:16px;line-height:1.8}
.supplement-gap-refresh{display:inline-flex;align-items:center;justify-content:center;gap:7px;align-self:center;min-height:44px;min-width:0;padding:10px 12px;border:1px solid transparent;border-radius:10px;color:#fff;background:linear-gradient(115deg,#7f6ffb,#5e49e9);box-shadow:0 5px 14px #7463ed2e;font:inherit;font-size:16px;font-weight:700;line-height:1.4;white-space:nowrap;cursor:pointer}.supplement-gap-refresh svg{flex:0 0 17px;width:17px;height:17px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}.supplement-gap-refresh:not(:disabled):hover{background:linear-gradient(115deg,#7461f4,#553fe0);box-shadow:0 6px 16px #7463ed40}
.supplement-gap-state{flex:1;padding:28px 24px;color:#8490ac;font-size:16px;line-height:1.8}.supplement-gap-state.is-error{color:#ad4256}.supplement-gap-empty{display:grid;align-content:center;text-align:center}.supplement-gap-empty p{margin:0}
.supplement-gap-cards{flex:1;min-height:0;padding:18px;display:grid;align-content:start;gap:14px;max-height:calc(var(--supplement-chat-height) + 66px);overflow-y:auto;scrollbar-width:thin;overscroll-behavior:contain}.supplement-gap-card{min-width:0;border:1px solid #e8ecfa;border-radius:15px;background:#fff;padding:17px 18px;overflow-wrap:anywhere}.supplement-gap-card-head{display:flex;align-items:center;gap:10px}.supplement-gap-level{flex:0 0 auto;padding:4px 9px;border-radius:7px;font-size:16px;font-weight:700}.supplement-gap-level.is-missing{background:#fff0d8;color:#c06f0a}.supplement-gap-level.is-extend{background:#e2f0ff;color:#2475ef}.supplement-gap-card-head strong{font-size:16px;color:#263752}.supplement-gap-missing{margin:12px 0 6px;font-size:16px;color:#556383;font-weight:600;line-height:1.7}.supplement-gap-prompts{list-style:none;padding:0;margin:6px 0 14px}.supplement-gap-prompts li{position:relative;margin-top:4px;padding-left:14px;font-size:16px;color:#7b86a3;line-height:1.7}.supplement-gap-prompts li::before{content:'·';position:absolute;left:4px;color:#aab3cc}.supplement-gap-actions{display:flex;gap:10px}.supplement-gap-actions button{border:1px solid #dce1f6;border-radius:9px;background:#fff;color:#6c7d9f;padding:8px 16px;font:inherit;font-size:16px;cursor:pointer}.supplement-gap-actions .supplement-gap-ask{background:#7b67f6;color:#fff;border-color:#7b67f6}.supplement-gap-free{display:block;width:100%;margin-top:auto;padding:16px;border:0;border-top:1px solid #e9edfc;background:#fff;color:#7768da;font:inherit;font-size:16px;cursor:pointer}
@container supplement-chat (max-width:730px){.supplement-chat-doodle{display:none}.supplement-transcript:has(.supplement-chat-doodle)>.supplement-message{max-width:92%}.supplement-composer-tools{gap:15px}.supplement-tool,.supplement-composer-tools :deep(.agent-enter-send-option){font-size:16px}.supplement-send{min-width:95px}}
@container supplement-workspace (max-width:1200px){.supplement-layout{grid-template-columns:minmax(290px,.36fr) minmax(0,.64fr);gap:20px}.supplement-gap-list-head{padding:20px}.supplement-gap-cards{padding:14px}.supplement-gap-card{padding:15px}}
@container supplement-workspace (max-width:940px){.supplement-layout{grid-template-columns:minmax(0,1fr);gap:20px}.supplement-gap-list{min-height:0}.supplement-gap-cards{max-height:360px}.supplement-transcript{height:380px}.supplement-gap-empty{padding:20px}.supplement-chat-head{margin-bottom:14px}}
@container supplement-workspace (max-width:600px){.supplement-layout{gap:16px}.supplement-gap-list-head{padding:18px}.supplement-gap-cards{max-height:300px;padding:12px}.supplement-gap-card{padding:14px}.supplement-transcript{padding:18px 12px;gap:18px;height:350px;border-radius:18px}.supplement-assistant-avatar{width:43px;height:43px;flex-basis:43px;margin-top:-3px}.supplement-message{gap:7px;max-width:96%}.supplement-speaker{font-size:16px;margin-bottom:6px}.supplement-message-content>p,.supplement-message-body{font-size:16px;padding:12px 14px;line-height:1.8}.supplement-composer{margin-top:16px;border-radius:16px}.supplement-composer textarea{font-size:16px;min-height:105px;padding:12px}.supplement-composer-bottom{padding:4px 6px 5px;gap:12px}.supplement-composer-tools{width:100%;justify-content:space-between;gap:8px}.supplement-send{margin-left:auto;min-width:104px;padding:10px 18px;font-size:16px}.supplement-drafts-actions{padding:15px 16px}.supplement-examples{bottom:98px}.supplement-confirmation>header{padding:18px 16px 0}.supplement-confirmation .supplement-draft-groups{padding:0 16px}.supplement-chat-head strong{font-size:16px}}
@container supplement-checklist (max-width:360px){.supplement-link-toolbar{grid-template-columns:minmax(0,1fr)}.supplement-gap-refresh{justify-self:stretch}}
@media(prefers-reduced-motion:reduce){.supplement-thinking-dots i{animation:none}.product-supplement-chat button:not(:disabled){transition:none}}
.product-supplement-chat{font-size:16px}.product-supplement-chat :deep(.fact-category-badge),.product-supplement-chat :deep(.chat-message-content code){font-size:16px}
</style>
