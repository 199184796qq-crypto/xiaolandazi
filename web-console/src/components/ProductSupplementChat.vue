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
const canSend = computed(() => ready.value && !busy.value && !!input.value.trim())

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

async function send() { await chat.send(); await nextTick(); inputBox.value?.focus() }
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
          <strong>待确认资料</strong>
          <small>{{ drafts.length }} 条 · 按分类整理</small>
        </header>
        <div class="supplement-drafts-body">
          <div v-if="!drafts.length" class="supplement-drafts-empty">
            <p>你补充的资料会按分类整理到这里。</p>
            <small>从补全清单挑一个问题回答，或直接贴一段商品信息，确认无误后点「保存」。</small>
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
          <button type="button" class="supplement-back-list" @click="backToList">← 返回补全清单</button>
          <div ref="transcript" class="supplement-transcript" role="log" aria-label="商品资料补充对话" aria-live="polite" :aria-busy="busy === 'chat'">
            <article class="supplement-message is-agent">
              <span class="supplement-speaker">资料助手</span>
              <p>你想补充商品的哪些资料？直接告诉我，我会帮你整理；不清楚的地方，我们可以继续聊。</p>
            </article>
            <article v-for="(message, index) in messages" :key="index" class="supplement-message" :class="message.role === 'user' ? 'is-user' : 'is-agent'">
              <span class="supplement-speaker">{{ message.role === 'user' ? '我' : '资料助手' }}</span>
              <p>{{ message.text }}</p>
            </article>
            <article v-if="busy === 'chat'" class="supplement-message is-agent"><span class="supplement-speaker">资料助手</span><p class="supplement-thinking">正在整理…</p></article>
          </div>
          <p v-if="!ready" class="supplement-notice">请先选择一个智能体方案，再补充资料。</p>
          <p v-if="error" class="supplement-error" role="alert">{{ error }}</p>
          <p v-else-if="notice" class="supplement-notice" role="status">{{ notice }}</p>
          <form class="supplement-composer" @submit.prevent="send">
            <label class="supplement-input-label" for="product-supplement-input">补充商品资料</label>
            <textarea id="product-supplement-input" ref="inputBox" v-model="input" rows="4" maxlength="6000" placeholder="输入要补充的商品信息，或继续回答资料助手的问题…" :disabled="!ready || !!busy" @keydown="keydown" @compositionstart="composing = true" @compositionend="composing = false"></textarea>
            <div class="supplement-composer-bottom">
              <AgentEnterSendOption :disabled="!!busy" />
              <button type="submit" class="supplement-send" :disabled="!canSend">{{ busy === 'chat' ? '发送中…' : '发送' }}</button>
            </div>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.product-supplement-chat{max-width:1180px;margin:0 auto;padding:4px 0;color:#263752}
.supplement-layout{display:flex;gap:20px;align-items:flex-start}
.supplement-drafts-panel{flex:0 0 340px;display:flex;flex-direction:column;border:1px solid #e8ecfa;border-radius:16px;background:#fff;overflow:hidden;max-height:min(76vh,740px)}
.supplement-drafts-head{padding:16px 18px;border-bottom:1px solid #eef1fb;background:#fbfcff}
.supplement-drafts-head strong{display:block;font-size:15px;color:#263752}
.supplement-drafts-head small{display:block;margin-top:3px;color:#8490ac;font-size:12px}
.supplement-drafts-body{flex:1;overflow-y:auto;padding:16px 18px}
.supplement-drafts-empty p{margin:0;color:#556383;font-size:14px;font-weight:600}
.supplement-drafts-empty small{display:block;margin-top:8px;color:#96a1ba;font-size:12.5px;line-height:1.7}
.supplement-draft-group{margin-bottom:18px}
.supplement-draft-group ul{list-style:none;padding:0;margin:10px 0 0}
.supplement-draft-group li{margin-top:10px;padding:11px 13px;border:1px solid #edf0f9;border-radius:11px;background:#fbfcff}
.supplement-draft-group li strong{display:block;font-size:13.5px;color:#3a4563}
.supplement-draft-group li p{margin:6px 0 0;font-size:13px;color:#667294;line-height:1.7;white-space:pre-wrap}
.supplement-drafts-actions{display:flex;gap:10px;padding:14px 18px;border-top:1px solid #eef1fb;background:#fbfcff}
.supplement-drafts-actions button{border:1px solid #dce1f6;border-radius:9px;background:white;color:#556383;padding:9px 14px;font:inherit;font-size:13px;cursor:pointer}
.supplement-drafts-actions .supplement-save{background:#5953ed;color:#fff;border-color:#5953ed;flex:1}
.supplement-chat-panel{flex:1;min-width:0}
/* 问题列表 */
.supplement-gap-list{border:1px solid #e8ecfa;border-radius:16px;background:#fbfcff;overflow:hidden}
.supplement-gap-list-head{padding:18px 20px;border-bottom:1px solid #eef1fb;background:#fff}
.supplement-gap-list-head strong{display:block;font-size:16px;color:#263752}
.supplement-gap-list-head small{display:block;margin-top:4px;color:#8490ac;font-size:12.5px;line-height:1.6}
.supplement-gap-state{padding:28px 20px;color:#8490ac;font-size:14px;line-height:1.7}
.supplement-gap-state.is-error{color:#ad4256}
.supplement-gap-cards{padding:16px 18px;display:grid;gap:12px;max-height:min(62vh,640px);overflow-y:auto}
.supplement-gap-card{border:1px solid #edf0f9;border-radius:13px;background:#fff;padding:14px 16px}
.supplement-gap-card-head{display:flex;align-items:center;gap:10px}
.supplement-gap-level{flex:0 0 auto;padding:3px 9px;border-radius:8px;font-size:12px;font-weight:700}
.supplement-gap-level.is-missing{background:#fff0d8;color:#c06f0a}
.supplement-gap-level.is-extend{background:#e2f0ff;color:#2475ef}
.supplement-gap-card-head strong{font-size:15px;color:#263752}
.supplement-gap-missing{margin:9px 0 6px;font-size:14px;color:#556383;font-weight:600}
.supplement-gap-prompts{list-style:none;padding:0;margin:6px 0 12px}
.supplement-gap-prompts li{margin-top:4px;font-size:13px;color:#7b86a3;line-height:1.6;padding-left:14px;position:relative}
.supplement-gap-prompts li::before{content:'·';position:absolute;left:4px;color:#aab3cc}
.supplement-gap-actions{display:flex;gap:10px}
.supplement-gap-actions button{border:1px solid #dce1f6;border-radius:9px;background:white;color:#556383;padding:8px 15px;font:inherit;font-size:13px;cursor:pointer}
.supplement-gap-actions .supplement-gap-ask{background:#5953ed;color:#fff;border-color:#5953ed}
.supplement-gap-free{display:block;width:100%;padding:12px;border:0;border-top:1px solid #eef1fb;background:#fbfcff;color:#6a76a3;font:inherit;font-size:13px;cursor:pointer}
/* 对话视图 */
.supplement-chat-view{display:flex;flex-direction:column}
.supplement-back-list{align-self:flex-start;margin-bottom:12px;padding:8px 14px;border:1px solid #dce1f6;border-radius:9px;background:white;color:#556383;font:inherit;font-size:13px;cursor:pointer}
.supplement-transcript{display:flex;flex-direction:column;gap:20px;height:clamp(320px,48vh,570px);overflow-y:auto;padding:22px;border:1px solid #e8ecfa;border-radius:16px;background:#f8faff;overscroll-behavior:contain;scrollbar-width:thin}
.supplement-message{max-width:82%;width:fit-content;padding:14px 18px;border-radius:15px;background:#fff;border:1px solid #e9edf9;overflow-wrap:anywhere}
.supplement-message.is-user{align-self:flex-end;background:#efedff;border-color:#e1ddfd}
.supplement-speaker{display:block;margin-bottom:7px;color:#6f7da1;font-size:12px;font-weight:650}
.supplement-message p{margin:0;white-space:pre-wrap;line-height:1.8;font-size:15px}
.supplement-composer{margin-top:18px}.supplement-input-label{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}
.supplement-composer textarea{display:block;box-sizing:border-box;width:100%;min-height:105px;padding:15px 18px;border:1px solid #dfe5f5;border-radius:13px;background:#fff;color:#263752;font:inherit;font-size:15px;line-height:1.7;resize:vertical}.supplement-composer textarea::placeholder{color:#9aa5bd}
.supplement-composer textarea:focus{outline:2px solid #b4affb;outline-offset:1px}.supplement-composer-bottom{display:flex;align-items:center;justify-content:space-between;margin-top:12px}
.supplement-send{min-width:100px;padding:11px 24px;border:0;border-radius:11px;background:linear-gradient(115deg,#8072f5,#5147fa);color:#fff;font:inherit;font-size:15px;font-weight:650;cursor:pointer;box-shadow:0 5px 12px #6259e525}
.product-supplement-chat button:disabled{opacity:.5;cursor:default;box-shadow:none}.product-supplement-chat button:focus-visible{outline:3px solid #b4affb;outline-offset:3px}
.supplement-error,.supplement-notice{margin:12px 0 0;padding:10px 13px;border-radius:9px;white-space:pre-wrap;line-height:1.6;font-size:13px}.supplement-error{color:#ad4256;background:#fff2f4}.supplement-notice{color:#647596;background:#f2f5fc}.supplement-thinking{color:#8b94ad}
@media(max-width:940px){.supplement-layout{flex-direction:column}.supplement-drafts-panel{flex:0 0 auto;width:100%;max-height:none}.supplement-drafts-body{max-height:320px}}
@media(max-width:700px){.supplement-transcript{padding:14px;gap:14px;height:45vh;min-height:280px}.supplement-message{max-width:92%;padding:12px 14px}.supplement-message p,.supplement-composer textarea{font-size:14px}.supplement-send{padding:10px 20px}.supplement-gap-cards{max-height:none}}
</style>
