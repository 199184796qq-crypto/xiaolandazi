<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { session } from '../session'
import { useAgentEnterSend } from '../useAgentEnterSend'
import { useProductSupplementChat } from '../productSupplementChat'
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
      <article v-if="drafts.length" class="supplement-message is-agent supplement-material-review">
        <span class="supplement-speaker">待确认资料</span>
        <ul><li v-for="(fact, index) in drafts" :key="index"><strong>{{ fact.key }}</strong><p>{{ fact.value }}</p></li></ul>
        <div class="supplement-review-actions">
          <button type="button" :disabled="!!busy || !ready" @click="chat.save">{{ busy === 'save' ? '保存中…' : '确认保存资料' }}</button>
          <button type="button" :disabled="!!busy" @click="chat.discard">取消</button>
        </div>
      </article>
    </div>
    <p v-if="!ready" class="supplement-notice">请先选择一个智能体方案，再补充资料。</p>
    <p v-if="error" class="supplement-error" role="alert">{{ error }}</p>
    <p v-else-if="notice && drafts.length" class="supplement-notice" role="status">{{ notice }}</p>
    <form class="supplement-composer" @submit.prevent="send">
      <label class="supplement-input-label" for="product-supplement-input">补充商品资料</label>
      <textarea id="product-supplement-input" ref="inputBox" v-model="input" rows="4" maxlength="6000" placeholder="输入要补充的商品信息，或继续回答资料助手的问题…" :disabled="!ready || !!busy" @keydown="keydown" @compositionstart="composing = true" @compositionend="composing = false"></textarea>
      <div class="supplement-composer-bottom">
        <AgentEnterSendOption :disabled="!!busy" />
        <button type="submit" class="supplement-send" :disabled="!canSend">{{ busy === 'chat' ? '发送中…' : '发送' }}</button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.product-supplement-chat{max-width:1100px;margin:0 auto;padding:4px 0;color:#263752}
.supplement-transcript{display:flex;flex-direction:column;gap:20px;height:clamp(320px,48vh,570px);overflow-y:auto;padding:22px;border:1px solid #e8ecfa;border-radius:16px;background:#f8faff;overscroll-behavior:contain;scrollbar-width:thin}
.supplement-message{max-width:82%;width:fit-content;padding:14px 18px;border-radius:15px;background:#fff;border:1px solid #e9edf9;overflow-wrap:anywhere}
.supplement-message.is-user{align-self:flex-end;background:#efedff;border-color:#e1ddfd}
.supplement-speaker{display:block;margin-bottom:7px;color:#6f7da1;font-size:12px;font-weight:650}
.supplement-message p{margin:0;white-space:pre-wrap;line-height:1.8;font-size:15px}
.supplement-material-review{min-width:min(420px,100%);max-width:92%}.supplement-material-review ul{list-style:none;padding:0;margin:8px 0 16px}.supplement-material-review li+li{margin-top:12px;padding-top:12px;border-top:1px solid #edf0f9}.supplement-material-review strong{font-size:14px}
.supplement-review-actions{display:flex;gap:10px}.supplement-review-actions button{border:1px solid #dce1f6;border-radius:9px;background:white;color:#556383;padding:8px 13px;font:inherit;font-size:13px;cursor:pointer}.supplement-review-actions button:first-child{background:#5953ed;color:#fff;border-color:#5953ed}
.supplement-composer{margin-top:18px}.supplement-input-label{position:absolute;width:1px;height:1px;overflow:hidden;clip-path:inset(50%)}
.supplement-composer textarea{display:block;box-sizing:border-box;width:100%;min-height:105px;padding:15px 18px;border:1px solid #dfe5f5;border-radius:13px;background:#fff;color:#263752;font:inherit;font-size:15px;line-height:1.7;resize:vertical}.supplement-composer textarea::placeholder{color:#9aa5bd}
.supplement-composer textarea:focus{outline:2px solid #b4affb;outline-offset:1px}.supplement-composer-bottom{display:flex;align-items:center;justify-content:space-between;margin-top:12px}
.supplement-send{min-width:100px;padding:11px 24px;border:0;border-radius:11px;background:linear-gradient(115deg,#8072f5,#5147fa);color:#fff;font:inherit;font-size:15px;font-weight:650;cursor:pointer;box-shadow:0 5px 12px #6259e525}
.product-supplement-chat button:disabled{opacity:.5;cursor:default;box-shadow:none}.product-supplement-chat button:focus-visible{outline:3px solid #b4affb;outline-offset:3px}
.supplement-error,.supplement-notice{margin:12px 0 0;padding:10px 13px;border-radius:9px;white-space:pre-wrap;line-height:1.6;font-size:13px}.supplement-error{color:#ad4256;background:#fff2f4}.supplement-notice{color:#647596;background:#f2f5fc}.supplement-thinking{color:#8b94ad}
@media(max-width:700px){.supplement-transcript{padding:14px;gap:14px;height:45vh;min-height:280px}.supplement-message{max-width:92%;padding:12px 14px}.supplement-message p,.supplement-composer textarea{font-size:14px}.supplement-material-review{min-width:0;width:auto}.supplement-send{padding:10px 20px}}
</style>
