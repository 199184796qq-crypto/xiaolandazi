<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { session } from '../session'
import { useAgentEnterSend } from '../useAgentEnterSend'
import { useProductSupplementChat } from '../productSupplementChat'
import { factCategoryDefinition, loadFactCategories } from '../factCategories'
import { factGapCheck, type FactGapItem } from '../api'
import type { SpeechMinerFactDraft } from '../types'
import FactCategoryBadge from './FactCategoryBadge.vue'
import AgentEnterSendOption from './AgentEnterSendOption.vue'
import documentsIllustration from '../assets/product-supplement-documents.svg'
import assistantAvatar from '../assets/product-supplement-assistant.svg'

const props = defineProps<{ planId: number; tenantId?: number; roomId?: number }>()
const emit = defineEmits<{ updated: [scope: { planId: number; tenantId: number; roomId: number }] }>()
const chat = useProductSupplementChat(() => ({
  planId: props.planId, tenantId: props.tenantId, roomId: props.roomId,
  actorKey: session.bootstrap?.actor ? `${session.bootstrap.actor.tenant_id}:${session.bootstrap.actor.user_id}` : '',
}), scope => emit('updated', { planId: scope.planId, tenantId: scope.tenantId || 0, roomId: scope.roomId || 0 }))
const { messages, input, drafts, busy, ready, error, notice } = chat
const enterSend = useAgentEnterSend()
const composing = ref(false)
const transcript = ref<HTMLElement | null>(null)
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

onMounted(() => { void loadFactCategories({ suppressErrorDialog: true }) })

// 补全清单（问题列表）
const view = ref<'list' | 'chat'>('list')
const gaps = ref<FactGapItem[]>([])
const gapsLoading = ref(false)
const gapsError = ref('')
const skipped = ref<string[]>([])
const visibleGaps = computed(() => gaps.value.filter(gap => !skipped.value.includes(gap['维度'])))

watch(() => [props.planId, ready.value], async () => {
  if (!props.planId || !ready.value) return
  if (gaps.value.length || gapsLoading.value) return
  gapsLoading.value = true
  gapsError.value = ''
  try {
    const res = await factGapCheck(props.planId, { tenant_id: props.tenantId })
    gaps.value = res.gaps || []
  } catch (cause) {
    gapsError.value = cause instanceof Error ? cause.message : '读取补全清单失败'
  } finally {
    gapsLoading.value = false
  }
}, { immediate: true })

function openGap(gap: FactGapItem) {
  view.value = 'chat'
  const trigger = `我想补充「${gap['待补事实']}」这项资料`
  void chat.askGap(JSON.stringify(gap), trigger)
}
function skipGap(gap: FactGapItem) {
  if (!skipped.value.includes(gap['维度'])) skipped.value.push(gap['维度'])
}
function backToList() { view.value = 'list' }

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
      <aside class="supplement-drafts-panel" aria-label="待确认资料">
        <header class="supplement-drafts-head">
          <span class="supplement-title-icon" aria-hidden="true"><svg viewBox="0 0 32 32"><path d="M9 4h10l6 6v16a2 2 0 0 1-2 2H9a3 3 0 0 1-3-3V7a3 3 0 0 1 3-3Z"/><path d="M19 4v6h6M11 15h10M11 20h10M11 25h6"/></svg></span>
          <div class="supplement-title-copy"><strong>待确认资料</strong><small>{{ drafts.length }} 条 · 按分类整理</small></div>
          <span class="supplement-organize-badge"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m12 2 2.6 6.9L22 12l-7.4 3.1L12 22l-2.6-6.9L2 12l7.4-3.1L12 2Z"/></svg>整理更高效</span>
        </header>
        <div class="supplement-drafts-body">
          <div v-if="!drafts.length" class="supplement-drafts-empty">
            <img class="supplement-documents-art" :src="documentsIllustration" alt="" />
            <p>你补充的资料会按分类整理到这里。</p>
            <small>从补全清单挑一个问题回答，或直接贴一段商品信息，确认无误后点「保存」。</small>
            <div class="supplement-feature-list">
              <div class="supplement-feature"><span class="supplement-feature-icon is-purple" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m13 2-9 12h7l-1 8 10-13h-8l1-7Z"/></svg></span><div><strong>自动分类</strong><span>帮你按类型整理资料</span></div></div>
              <div class="supplement-feature"><span class="supplement-feature-icon is-blue" aria-hidden="true"><svg viewBox="0 0 24 24"><rect x="3" y="3" width="18" height="18" rx="4"/><path d="m7 12 3 3 7-7"/></svg></span><div><strong>简单高效</strong><span>粘贴或输入即可</span></div></div>
              <div class="supplement-feature"><span class="supplement-feature-icon is-green" aria-hidden="true"><svg viewBox="0 0 24 24"><path d="m12 2 8 3v7c0 5-8 10-8 10S4 17 4 12V5l8-3Z"/><path d="M12 8v8M8 12h8"/></svg></span><div><strong>确认后保存</strong><span>核对资料后，保存到当前方案</span></div></div>
            </div>
          </div>
          <div v-else class="supplement-draft-groups">
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
        </div>
        <div v-if="!drafts.length" class="supplement-sidebar-note" aria-hidden="true">补充越完整，直播越轻松<svg viewBox="0 0 260 20"><path d="M3 17C76 0 162 1 257 5"/></svg></div>
        <footer v-if="drafts.length" class="supplement-drafts-actions">
          <button type="button" class="supplement-save" :disabled="!!busy || !ready" @click="chat.save">{{ busy === 'save' ? '保存中…' : '确认保存资料' }}</button>
          <button type="button" class="supplement-discard" :disabled="!!busy" @click="chat.discard">取消</button>
        </footer>
      </aside>

      <div class="supplement-chat-panel">
        <!-- 问题列表视图 -->
        <div v-if="view === 'list'" class="supplement-gap-list">
          <header class="supplement-gap-list-head">
            <div><strong>补全清单</strong><small>按必查维度整理，点一条开始问；已有的不重复问，只补缺的或在已有基础上深挖一层</small></div>
          </header>
          <div v-if="gapsLoading" class="supplement-gap-state">正在核对商品资料…</div>
          <div v-else-if="gapsError" class="supplement-gap-state is-error" role="alert">{{ gapsError }}</div>
          <div v-else-if="!visibleGaps.length" class="supplement-gap-state">商品资料已覆盖必查维度，可直接自由补充。</div>
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
                <button type="button" class="supplement-gap-ask" :disabled="!!busy" @click="openGap(gap)">问这个</button>
                <button type="button" class="supplement-gap-skip" :disabled="!!busy" @click="skipGap(gap)">跳过</button>
              </div>
            </section>
          </div>
          <button v-if="!gapsLoading" type="button" class="supplement-gap-free" @click="view = 'chat'">或直接自由补充（进入对话）</button>
        </div>

        <!-- 对话视图 -->
        <div v-else class="supplement-chat-view">
          <button type="button" class="supplement-back-list" @click="backToList"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m10 5-7 7 7 7M3 12h18"/></svg>返回补全清单</button>
          <div ref="transcript" class="supplement-transcript" role="log" aria-label="商品资料补充对话" aria-live="polite" :aria-busy="busy === 'chat'">
            <article class="supplement-message is-agent">
              <img class="supplement-assistant-avatar" :src="assistantAvatar" alt="" />
              <div class="supplement-message-content"><span class="supplement-speaker">资料助手</span><p>你想补充商品的哪些资料？直接告诉我，我会帮你整理；不清楚的地方，我们可以继续聊。</p></div>
            </article>
            <div v-if="!messages.length && !busy" class="supplement-chat-doodle" aria-hidden="true">有什么需要补充的？<br />我来帮你整理～<svg viewBox="0 0 220 20"><path d="M3 17C62 1 146 0 217 5"/></svg></div>
            <article v-for="(message, index) in messages" :key="index" class="supplement-message" :class="message.role === 'user' ? 'is-user' : 'is-agent'">
              <img v-if="message.role !== 'user'" class="supplement-assistant-avatar" :src="assistantAvatar" alt="" />
              <div class="supplement-message-content"><span class="supplement-speaker">{{ message.role === 'user' ? '我' : '资料助手' }}</span><p>{{ message.text }}</p></div>
            </article>
            <article v-if="busy === 'chat'" class="supplement-message is-agent"><img class="supplement-assistant-avatar" :src="assistantAvatar" alt="" /><div class="supplement-message-content"><span class="supplement-speaker">资料助手</span><p class="supplement-thinking">正在整理<span class="supplement-thinking-dots" aria-hidden="true"><i></i><i></i><i></i></span></p></div></article>
          </div>
          <p v-if="!ready" class="supplement-notice">请先选择一个智能体方案，再补充资料。</p>
          <p v-if="error" class="supplement-error" role="alert">{{ error }}</p>
          <p v-else-if="notice" class="supplement-notice" role="status">{{ notice }}</p>
          <p v-if="fileError" class="supplement-error" role="alert">{{ fileError }}</p>
          <p v-else-if="fileNotice" class="supplement-notice" role="status">{{ fileNotice }}</p>
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
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Definite width prevents shrink-to-fit inside the strategy grid with contained children. */
.product-supplement-chat{--supplement-chat-height:clamp(380px,55vh,620px);box-sizing:border-box;width:100%;min-width:0;max-width:1560px;justify-self:center;container-type:inline-size;container-name:supplement-workspace;margin:0 auto;padding:8px 0;color:#253451}
.supplement-layout{display:grid;grid-template-columns:minmax(310px,.36fr) minmax(0,.64fr);gap:28px;align-items:stretch}
.supplement-drafts-panel{container-type:inline-size;container-name:supplement-drafts;position:relative;display:flex;flex-direction:column;min-width:0;min-height:calc(var(--supplement-chat-height) + 216px);border:1px solid #edf0ff;border-radius:24px;background:linear-gradient(145deg,#f9fbff 0%,#f5f7ff 52%,#f9f9ff 100%);overflow:hidden;isolation:isolate}
.supplement-drafts-panel::after{content:'';position:absolute;z-index:-1;left:-45px;bottom:-90px;width:210px;height:210px;border-radius:50%;background:linear-gradient(135deg,#e8edff66,#eef0ff22);pointer-events:none}
.supplement-drafts-head{display:flex;align-items:center;gap:16px;padding:28px 26px 16px}
.supplement-title-icon{display:grid;place-items:center;flex:0 0 70px;width:70px;height:72px;border-radius:17px;background:linear-gradient(135deg,#c0c0ff,#8478ff 62%,#6352fc);box-shadow:0 7px 14px #8c81ea22,inset 1px 2px 3px #ffffff66}
.supplement-title-icon svg{width:34px;height:34px;fill:#fff;fill-opacity:.88;stroke:#fff;stroke-width:2;stroke-linecap:round;stroke-linejoin:round}.supplement-title-icon svg path+path{fill:none;stroke:#a294fc}
.supplement-title-copy{min-width:0;flex:1}.supplement-title-copy strong{display:block;font-size:clamp(24px,1.9vw,30px);font-weight:750;letter-spacing:-.5px;color:#182949;white-space:nowrap}.supplement-title-copy small{display:block;margin-top:8px;color:#8693b3;font-size:17px;line-height:1.5;white-space:nowrap}
.supplement-organize-badge{display:flex;align-items:center;gap:5px;flex:0 0 auto;padding:7px 10px;border-radius:20px;background:#eaeaff;color:#7161ef;font-size:13px;font-weight:650;white-space:nowrap}.supplement-organize-badge svg{width:19px;height:19px;fill:#8577f9;stroke:#fff;stroke-width:.7}
.supplement-drafts-body{flex:1;min-height:0;padding:0 26px 20px;overflow-y:auto;scrollbar-width:thin}
.supplement-drafts-empty{text-align:center}.supplement-documents-art{display:block;width:min(100%,300px);height:auto;margin:8px auto 8px;pointer-events:none}
.supplement-drafts-empty>p{margin:0;font-size:17px;color:#253654;font-weight:700;line-height:1.7}.supplement-drafts-empty>small{display:block;margin:14px auto 0;max-width:400px;font-size:15px;line-height:1.9;color:#8794b2}
.supplement-feature-list{display:grid;gap:16px;margin-top:24px;padding:20px 18px;border:1px solid #e7ecff;border-radius:18px;background:#ffffff80;text-align:left}
.supplement-feature{display:flex;align-items:center;gap:16px}.supplement-feature-icon{display:grid;place-items:center;flex:0 0 52px;width:52px;height:52px;border-radius:50%}.supplement-feature-icon svg{width:26px;height:26px;stroke-width:2.1;stroke-linecap:round;stroke-linejoin:round}
.supplement-feature-icon.is-purple{background:#e9e6ff;color:#7158f8}.is-purple svg{fill:currentColor;stroke:none}.supplement-feature-icon.is-blue{background:#e0eeff;color:#359df4}.is-blue svg{fill:currentColor;stroke:none}.is-blue svg path{stroke:#fff;fill:none}.supplement-feature-icon.is-green{background:#dff5ef;color:#25bc97}.is-green svg{fill:none;stroke:currentColor}
.supplement-feature strong{display:block;color:#233451;font-size:18px;font-weight:700;line-height:1.5}.supplement-feature div>span{display:block;margin-top:3px;color:#8492b0;font-size:15px;line-height:1.6}
.supplement-sidebar-note{align-self:center;margin:14px 18px 32px;color:#a09aff;font-family:KaiTi,STKaiti,serif;font-size:21px;font-style:italic;letter-spacing:2px;transform:rotate(-5deg);pointer-events:none}.supplement-sidebar-note svg{display:block;width:95%;height:20px;margin:3px auto 0;fill:none;stroke:#aba2ff;stroke-width:2.5;stroke-linecap:round}
.supplement-draft-group{margin-bottom:20px}.supplement-draft-group:first-child{margin-top:20px}.supplement-draft-group ul{list-style:none;padding:0;margin:12px 0 0}.supplement-draft-group li{margin-top:10px;padding:14px 15px;border:1px solid #e5eafb;border-radius:13px;background:#ffffffd9}.supplement-draft-group li strong{display:block;font-size:15px;color:#344363}.supplement-draft-group li p{margin:7px 0 0;color:#677797;font-size:14px;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}
.supplement-draft-groups{max-height:calc(var(--supplement-chat-height) + 80px);overflow-y:auto;scrollbar-width:thin}.supplement-drafts-actions{display:flex;gap:10px;padding:18px 24px;border-top:1px solid #e7ecfa;background:#ffffffb3}.supplement-drafts-actions button{border:1px solid #dce1f6;border-radius:11px;background:#fff;color:#667594;padding:11px 16px;font:inherit;font-size:14px;cursor:pointer}.supplement-drafts-actions .supplement-save{flex:1;border-color:transparent;background:linear-gradient(115deg,#8675fa,#6351f8);color:#fff}
.supplement-chat-panel{min-width:0;container-type:inline-size;container-name:supplement-chat}.supplement-chat-view{display:flex;flex-direction:column;min-width:0}
.supplement-back-list{display:inline-flex;align-items:center;gap:10px;align-self:flex-start;margin:0 0 18px;padding:12px 18px;border:1px solid #e2e7ff;border-radius:13px;background:#ffffffd9;color:#7182a7;font:inherit;font-size:16px;line-height:1.4;cursor:pointer;box-shadow:0 2px 4px #9ba9d108}.supplement-back-list svg{width:17px;height:17px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.supplement-transcript{position:relative;display:flex;flex-direction:column;gap:24px;box-sizing:border-box;height:var(--supplement-chat-height);overflow-y:auto;padding:26px 22px;border:1px solid #e5eaff;border-radius:22px;background:linear-gradient(135deg,#f7f9ff,#f5f7ff 58%,#f1f4ff);overscroll-behavior:contain;scrollbar-width:thin}
.supplement-message{position:relative;z-index:1;display:flex;align-items:flex-start;gap:13px;width:fit-content;max-width:88%;min-width:0;overflow-wrap:anywhere}.supplement-assistant-avatar{flex:0 0 62px;width:62px;height:62px;margin-top:-5px;pointer-events:none}.supplement-message-content{min-width:0}.supplement-speaker{display:block;margin:0 0 9px;color:#7b6cf0;font-size:17px;font-weight:700;line-height:1.5}.supplement-message p{margin:0;padding:15px 20px;border:1px solid #e6eaff;border-radius:17px;background:#ffffffed;color:#314463;font-size:18px;white-space:pre-wrap;line-height:1.85;box-shadow:0 2px 5px #9da8d505}
.supplement-message.is-user{align-self:flex-end;max-width:80%}.supplement-message.is-user .supplement-speaker{text-align:right;color:#8980b8}.supplement-message.is-user p{background:#efebff;border-color:#e3dcff;color:#51456f}
.supplement-chat-doodle{position:absolute;right:25px;top:58px;color:#b4adff;font-family:KaiTi,STKaiti,serif;font-size:20px;font-style:italic;line-height:1.7;letter-spacing:1px;transform:rotate(-12deg);text-align:center;pointer-events:none}.supplement-chat-doodle::before{content:'';position:absolute;width:30px;height:30px;border-radius:50%;left:6px;top:-27px;background:#e6eaff;z-index:-1}.supplement-chat-doodle svg{display:block;width:200px;height:20px;fill:none;stroke:#c0b8ff;stroke-width:2.5;stroke-linecap:round}
.supplement-transcript:has(.supplement-chat-doodle)>.supplement-message{max-width:72%}
.supplement-composer{position:relative;box-sizing:border-box;margin-top:20px;padding:3px 6px 6px;border:1px solid #e0dfff;border-radius:19px;background:#fff;box-shadow:0 4px 12px #8074dc08}.supplement-composer:focus-within{border-color:#ada4fa;box-shadow:0 0 0 3px #edeaff}
.supplement-input-label,.supplement-file-input{position:absolute;width:1px;height:1px;padding:0;overflow:hidden;clip-path:inset(50%);white-space:nowrap}
.supplement-composer textarea{display:block;box-sizing:border-box;width:100%;min-height:110px;max-height:280px;padding:14px 16px;border:0;border-radius:15px;background:transparent;color:#314463;font:inherit;font-size:16px;line-height:1.8;resize:vertical}.supplement-composer textarea::placeholder{color:#9aa6c0}.supplement-composer textarea:focus{outline:none}.supplement-composer textarea:disabled{background:transparent}
.supplement-composer-bottom{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:12px;padding:8px 10px 7px}.supplement-composer-tools{display:flex;flex-wrap:wrap;align-items:center;gap:22px}.supplement-tool{display:inline-flex;align-items:center;gap:8px;padding:4px 0;border:0;background:transparent;color:#8795b7;font:inherit;font-size:14px;line-height:1.5;cursor:pointer}.supplement-tool svg{width:19px;height:19px;fill:none;stroke:#63769b;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}.supplement-tool:hover{color:#7160ee}.supplement-composer-tools :deep(.agent-enter-send-option){font-size:14px;color:#8795b7}.supplement-composer-tools :deep(.agent-enter-send-option input){width:17px;height:17px;accent-color:#7460f8}
.supplement-send{display:inline-flex;justify-content:center;align-items:center;gap:9px;min-width:144px;padding:13px 22px;border:0;border-radius:12px;background:linear-gradient(110deg,#8978fa,#6652f7);color:#fff;font:inherit;font-size:16px;font-weight:650;cursor:pointer;box-shadow:0 4px 10px #8174e21c}.supplement-send svg{width:19px;height:19px;fill:#fff;stroke:#fff;stroke-width:1.4;stroke-linecap:round;stroke-linejoin:round}.supplement-send svg path+path{fill:none;stroke:#9181f8}
.product-supplement-chat button:disabled{opacity:.5;cursor:default;box-shadow:none}.product-supplement-chat button:focus-visible{outline:3px solid #b4affb;outline-offset:3px}.product-supplement-chat button:not(:disabled){transition:background .15s,box-shadow .15s}.supplement-send:not(:disabled):hover{box-shadow:0 5px 14px #8174e24d}
.supplement-error,.supplement-notice{margin:12px 0 0;padding:10px 13px;border-radius:10px;white-space:pre-wrap;line-height:1.6;font-size:13px}.supplement-error{color:#ad4256;background:#fff2f4}.supplement-notice{color:#647596;background:#f2f5fc}.supplement-message .supplement-thinking{display:flex;align-items:center;gap:10px;color:#8b94ad}.supplement-thinking-dots{display:inline-flex;gap:4px}.supplement-thinking-dots i{width:4px;height:4px;border-radius:50%;background:#a699f6;animation:supplement-dot 1.4s infinite}.supplement-thinking-dots i:nth-child(2){animation-delay:.2s}.supplement-thinking-dots i:nth-child(3){animation-delay:.4s}@keyframes supplement-dot{0%,70%,100%{opacity:.35}35%{opacity:1}}
.supplement-examples{position:absolute;z-index:5;bottom:64px;left:16px;box-sizing:border-box;width:min(360px,calc(100% - 32px));padding:16px;border:1px solid #e1e2fc;border-radius:16px;background:#fff;box-shadow:0 12px 36px #4f44871c}.supplement-examples>strong{display:block;font-size:15px;color:#354369}.supplement-examples>small{display:block;margin:5px 0 12px;font-size:12px;color:#929db6}.supplement-examples button{display:block;width:100%;margin-top:8px;padding:10px 12px;border:1px solid #eaeafb;border-radius:10px;background:#f9f9ff;text-align:left;color:#5b528b;font:inherit;cursor:pointer}.supplement-examples button span{display:block;font-size:13px;font-weight:650}.supplement-examples button small{display:block;margin-top:4px;font-size:12px;color:#8490aa;line-height:1.6}.supplement-examples .supplement-examples-close{border:0;background:none;text-align:center;padding:4px;font-size:12px;color:#8c94ab}
.supplement-gap-list{min-height:calc(var(--supplement-chat-height) + 214px);border:1px solid #e5eaff;border-radius:22px;background:#f8faff;overflow:hidden}.supplement-gap-list-head{padding:24px;border-bottom:1px solid #e9edfc;background:#fff}.supplement-gap-list-head strong{display:block;font-size:21px;color:#263752}.supplement-gap-list-head small{display:block;margin-top:9px;color:#8490ac;font-size:14px;line-height:1.8}.supplement-gap-state{padding:28px 24px;color:#8490ac;font-size:14px;line-height:1.8}.supplement-gap-state.is-error{color:#ad4256}.supplement-gap-cards{padding:20px;display:grid;gap:14px;max-height:var(--supplement-chat-height);overflow-y:auto}.supplement-gap-card{border:1px solid #e8ecfa;border-radius:15px;background:#fff;padding:17px 18px}.supplement-gap-card-head{display:flex;align-items:center;gap:10px}.supplement-gap-level{flex:0 0 auto;padding:4px 9px;border-radius:7px;font-size:12px;font-weight:700}.supplement-gap-level.is-missing{background:#fff0d8;color:#c06f0a}.supplement-gap-level.is-extend{background:#e2f0ff;color:#2475ef}.supplement-gap-card-head strong{font-size:16px;color:#263752}.supplement-gap-missing{margin:12px 0 6px;font-size:14px;color:#556383;font-weight:600;line-height:1.7}.supplement-gap-prompts{list-style:none;padding:0;margin:6px 0 14px}.supplement-gap-prompts li{position:relative;margin-top:4px;padding-left:14px;font-size:13px;color:#7b86a3;line-height:1.7}.supplement-gap-prompts li::before{content:'·';position:absolute;left:4px;color:#aab3cc}.supplement-gap-actions{display:flex;gap:10px}.supplement-gap-actions button{border:1px solid #dce1f6;border-radius:9px;background:#fff;color:#6c7d9f;padding:8px 16px;font:inherit;font-size:13px;cursor:pointer}.supplement-gap-actions .supplement-gap-ask{background:#7b67f6;color:#fff;border-color:#7b67f6}.supplement-gap-free{display:block;width:100%;padding:16px;border:0;border-top:1px solid #e9edfc;background:#fff;color:#7768da;font:inherit;font-size:14px;cursor:pointer}
@container supplement-chat (max-width:730px){.supplement-chat-doodle{display:none}.supplement-transcript:has(.supplement-chat-doodle)>.supplement-message{max-width:92%}.supplement-composer-tools{gap:15px}.supplement-tool,.supplement-composer-tools :deep(.agent-enter-send-option){font-size:13px}.supplement-send{min-width:95px}}
@container supplement-workspace (max-width:1200px){.supplement-layout{grid-template-columns:minmax(290px,.36fr) minmax(0,.64fr);gap:20px}.supplement-drafts-head{padding:24px 20px 16px;gap:12px}.supplement-title-icon{width:52px;height:54px;flex-basis:52px;border-radius:14px}.supplement-title-copy strong{font-size:23px}.supplement-title-copy small{font-size:12px}.supplement-organize-badge{font-size:10px;padding:6px}.supplement-drafts-body{padding:0 20px 18px}.supplement-feature-list{padding:18px 14px}.supplement-feature{gap:12px}.supplement-feature-icon{width:44px;height:44px;flex-basis:44px}.supplement-feature strong{font-size:15px}.supplement-feature div>span{font-size:13px}.supplement-drafts-empty>p{font-size:15px}}
@container supplement-workspace (max-width:940px){.supplement-layout{grid-template-columns:minmax(0,1fr);gap:20px}.supplement-drafts-panel{min-height:0}.supplement-drafts-head{padding:20px}.supplement-drafts-body{padding:0 20px 20px}.supplement-documents-art{width:180px;margin:0 auto}.supplement-feature-list{grid-template-columns:repeat(3,minmax(0,1fr));gap:14px;max-width:650px;margin:20px auto 0;padding:15px}.supplement-feature{gap:9px}.supplement-feature-icon{flex-basis:36px;width:36px;height:36px}.supplement-feature-icon svg{width:21px;height:21px}.supplement-feature strong{font-size:14px}.supplement-feature div>span{font-size:12px}.supplement-sidebar-note{margin:0 16px 20px;font-size:18px}.supplement-draft-groups{max-height:320px}.supplement-gap-list{min-height:0}.supplement-transcript{height:380px}.supplement-gap-cards{max-height:none}}
@container supplement-workspace (max-width:600px){.supplement-layout{gap:16px}.supplement-drafts-head{padding:18px 16px;gap:10px}.supplement-title-copy strong{font-size:22px}.supplement-organize-badge{font-size:10px}.supplement-drafts-body{padding:0 16px 16px}.supplement-documents-art{width:165px}.supplement-drafts-empty>p{font-size:14px}.supplement-drafts-empty>small{font-size:13px;margin-top:9px}.supplement-feature-list{grid-template-columns:1fr;gap:12px;padding:14px 16px}.supplement-feature-icon{width:38px;height:38px;flex-basis:38px}.supplement-feature strong{font-size:14px}.supplement-feature div>span{font-size:12px}.supplement-transcript{padding:18px 12px;gap:18px;height:350px;border-radius:18px}.supplement-assistant-avatar{width:43px;height:43px;flex-basis:43px;margin-top:-3px}.supplement-message{gap:7px;max-width:96%}.supplement-speaker{font-size:14px;margin-bottom:6px}.supplement-message p{font-size:14px;padding:12px 14px;line-height:1.8}.supplement-composer{margin-top:16px;border-radius:16px}.supplement-composer textarea{font-size:14px;min-height:105px;padding:12px}.supplement-composer-bottom{padding:4px 6px 5px;gap:12px}.supplement-composer-tools{width:100%;justify-content:space-between;gap:8px}.supplement-send{margin-left:auto;min-width:104px;padding:10px 18px;font-size:14px}.supplement-back-list{font-size:14px;padding:10px 14px;margin-bottom:14px}.supplement-sidebar-note{font-size:17px}.supplement-gap-list-head{padding:18px}.supplement-gap-list-head strong{font-size:18px}.supplement-gap-cards{padding:14px}.supplement-drafts-actions{padding:15px 16px}.supplement-examples{bottom:98px}}
@container supplement-drafts (max-width:480px){.supplement-organize-badge{display:none}}
@media(prefers-reduced-motion:reduce){.supplement-thinking-dots i{animation:none}.product-supplement-chat button:not(:disabled){transition:none}}
</style>
