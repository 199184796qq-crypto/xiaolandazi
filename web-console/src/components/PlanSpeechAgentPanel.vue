<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { adoptLiveAgentPlanFacts, adoptPlanTestAnswer, request } from '../api'
import type { AgentDecisionSimulationResult, Room } from '../types'
import { session } from '../session'
import AgentEnterSendOption from './AgentEnterSendOption.vue'
import NaturalContentCorrection from './NaturalContentCorrection.vue'
import { useAgentEnterSend } from '../useAgentEnterSend'

const props = defineProps<{ roomId: number; planId: number }>()
const emit = defineEmits<{ busy: [value: boolean]; saved: [] }>()
type Answer = { id: number; question: string; reply: string; correction: string; editing: boolean; savedAs?: 'fact' | 'strategy'; notice: string; error: string }
const answers = ref<Answer[]>([]), question = ref(''), error = ref(''), pending = ref('')
const transcript = ref<HTMLElement | null>(null)
const enterSend = useAgentEnterSend(), composing = ref(false)
let epoch = 0, nextId = 0
watch(() => [props.roomId, props.planId, session.bootstrap?.actor.user_id, session.bootstrap?.actor.tenant_id], () => {
  epoch++; answers.value = []; question.value = ''; error.value = ''; pending.value = ''; composing.value = false; emit('busy', false)
}, { immediate: true })
onBeforeUnmount(() => { epoch++; emit('busy', false) })

function handleQuestionKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' || event.shiftKey || event.isComposing || composing.value || event.keyCode === 229 || !enterSend.value) return
  event.preventDefault()
  if (!event.repeat && !pending.value) void test()
}

async function test(source?: Answer) {
  const text = (source?.question || question.value).trim(), room = props.roomId, plan = props.planId, token = epoch
  if (!text || !room || !plan || pending.value) return
  const correction = source?.correction.trim() || ''
  if (source && !correction) { source.error = '先填写你认为正确的回答或回答要求哦～'; return }
  pending.value = source ? `correct:${source.id}` : 'test'; emit('busy', true)
  error.value = ''; if (source) source.error = ''
  try {
    const recent = answers.value.slice(-8).map(answer => ({ question: answer.question, reply: Array.from(answer.reply).slice(0, 1000).join('') }))
    const result = await request<AgentDecisionSimulationResult>(`/api/v1/rooms/${room}/agent-decisions/simulate`, {
      method: 'POST', body: JSON.stringify({ question: text, plan_id: plan, preview_instruction: correction || undefined, recent_answers: recent.length ? recent : undefined }), suppressErrorDialog: true,
    })
    if (token !== epoch) return
    if (result.plan_id !== plan || !result.reply?.trim()) throw new Error('测试返回的方案或回答不完整，请重试')
    answers.value.push({ id: ++nextId, question: text, reply: result.reply.trim(), correction, editing: false, notice: correction ? '已按你的纠正重新测试，尚未保存。' : '', error: '' })
    if (source) source.editing = false
    else question.value = ''
    await nextTick(); transcript.value?.scrollTo({ top: transcript.value.scrollHeight, behavior: 'smooth' })
  } catch (cause) {
    if (token === epoch) {
      const message = cause instanceof Error ? cause.message : '测试失败，请重试'
      if (source) source.error = message; else error.value = message
    }
  } finally { if (token === epoch) { pending.value = ''; emit('busy', false) } }
}

async function adopt(answer: Answer, kind: 'fact' | 'strategy') {
  const room = props.roomId, plan = props.planId, token = epoch
  if (pending.value || answer.savedAs || !room || !plan) return
  pending.value = `save:${answer.id}`; emit('busy', true); answer.error = ''
  try {
    if (kind === 'fact') {
      const detail = await request<Room>(`/api/v1/rooms/${room}`, { suppressErrorDialog: true })
      if (token !== epoch) return
      const result = await adoptLiveAgentPlanFacts(plan, [{
        category: 'other', key: Array.from(`测试确认：${answer.question}`).slice(0, 255).join(''), value: answer.reply,
        status: 'pending', review_bucket: 'adoptable', source_quote: '', review_reason: '用户明确确认方案测试回答',
      }], detail.tenant_id, `manual:plan-speech-test:room:${room}`, { suppressErrorDialog: true })
      if (token !== epoch) return
      if (!result.adopted && result.results?.[0]?.status !== 'unchanged') throw new Error(result.results?.[0]?.message || '事实未保存，请核对后重试')
    } else {
      await adoptPlanTestAnswer(room, { plan_id: plan, question: answer.question, answer: answer.reply })
      if (token !== epoch) return
    }
    answer.savedAs = kind; answer.editing = false
    answer.notice = kind === 'fact' ? '已采纳到当前方案的事实依据，可以继续测试啦。' : '已采纳到当前方案的互动策略，可以继续测试啦。'
    window.dispatchEvent(new CustomEvent('live-strategy-assets-updated', { detail: { plan_id: plan, room_id: room } }))
    window.dispatchEvent(new Event('agent-memory-updated')); window.dispatchEvent(new Event('interaction-review-updated'))
    emit('saved')
  } catch (cause) { if (token === epoch) answer.error = cause instanceof Error ? cause.message : '采纳失败，请重试' }
  finally { if (token === epoch) { pending.value = ''; emit('busy', false) } }
}
</script>

<template>
  <section class="plan-speech-agent" aria-label="话术智能体互动测试">
    <header><strong>话术智能体 · 互动测试</strong><p>方案 #{{ planId }} · 共用互动智能体的业务核心</p><small>模拟观众提问，不叠加主播风格或直播打断、回归。确认后可采纳到事实依据或回答策略。</small></header>
    <div ref="transcript" class="plan-test-transcript" aria-live="polite">
      <div v-if="!answers.length" class="plan-test-empty"><strong>来问一句，看看它怎么答</strong><p>例如：发什么快递？骨头多不多？一斤多少钱？</p></div>
      <article v-for="answer in answers" :key="answer.id" :data-plan-test-answer="answer.id">
        <p class="test-question"><b>模拟观众：</b>{{ answer.question }}</p>
        <p class="test-reply"><b>智能体回答：</b>{{ answer.reply }}</p>
        <NaturalContentCorrection :plan-id="planId" :room-id="roomId" :selected-text="answer.reply" :disabled="!!pending" @busy="emit('busy',$event)" @saved="emit('saved')" />
        <div class="answer-actions">
          <button type="button" :disabled="!!pending || !!answer.savedAs" @click="answer.editing = !answer.editing">纠正回答</button>
          <button type="button" :disabled="!!pending || !!answer.savedAs" @click="adopt(answer, 'fact')">{{ answer.savedAs === 'fact' ? '事实已采纳' : '采纳到事实依据' }}</button>
          <button type="button" :disabled="!!pending || !!answer.savedAs" @click="adopt(answer, 'strategy')">{{ answer.savedAs === 'strategy' ? '策略已采纳' : '采纳到互动策略' }}</button>
        </div>
        <form v-if="answer.editing" class="answer-correction" @submit.prevent="test(answer)">
          <label :for="`plan-correction-${answer.id}`">正确回答或回答要求</label>
          <textarea :id="`plan-correction-${answer.id}`" v-model="answer.correction" rows="3" maxlength="4000" :disabled="!!pending" placeholder="填写哪里不对、真实情况或应该如何回答" />
          <button type="submit" :disabled="!!pending || !answer.correction.trim()">{{ pending === `correct:${answer.id}` ? '重新测试中…' : '应用纠正并重测' }}</button>
        </form>
        <p v-if="answer.notice" class="test-notice" role="status">{{ answer.notice }}</p><p v-if="answer.error" class="test-error" role="alert">{{ answer.error }}</p>
      </article>
      <p v-if="pending" class="test-working" role="status">{{ pending.startsWith('save:') ? '正在采纳到当前方案…' : '正在模拟观众提问，生成回答…' }}</p>
    </div>
    <form class="plan-test-composer" @submit.prevent="test()">
      <p v-if="error" class="test-error" role="alert">{{ error }}</p>
      <label for="plan-test-question">模拟用户提问</label><textarea id="plan-test-question" v-model="question" rows="2" maxlength="1000" :disabled="!!pending" placeholder="输入观众问题，测试当前方案的回答" @keydown="handleQuestionKeydown" @compositionstart="composing = true" @compositionend="composing = false" @blur="composing = false" />
      <div class="plan-test-send-actions">
        <AgentEnterSendOption :disabled="!!pending" />
        <button class="test-primary" type="submit" :disabled="!!pending || !question.trim()">{{ pending === 'test' ? '测试中…' : '发送' }}</button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.plan-speech-agent{display:grid;grid-template-rows:auto minmax(0,1fr) auto;box-sizing:border-box;min-height:0;height:100%;font-size:14px;color:#344360;background:#f8faff}.plan-speech-agent>header{padding:16px 18px;border-bottom:1px solid #e2e7f4}.plan-speech-agent>header strong{font-size:18px;color:#374ba1}.plan-speech-agent>header p{margin:6px 0;color:#7485a6}.plan-speech-agent>header small{color:#8291ab;line-height:1.7}.plan-test-transcript{overflow:auto;min-height:0;padding:14px 16px;overscroll-behavior:contain}.plan-test-empty{padding:30px 8px;color:#8997b1;line-height:1.8}.plan-test-empty strong{color:#53688e;font-size:17px}.plan-test-transcript article{padding:15px;margin-bottom:14px;border:1px solid #e2e7f8;border-radius:15px;background:#fff}.test-question{margin:0 0 12px;color:#647699}.test-reply{margin:0;padding:12px;border-radius:10px;background:#f1f4ff;line-height:1.8;white-space:pre-wrap;overflow-wrap:anywhere}.answer-actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:12px}.plan-speech-agent button{padding:8px 12px;border:1px solid #d7def4;border-radius:9px;background:white;color:#5a65cc;font:inherit;cursor:pointer}.plan-speech-agent button:disabled{opacity:.55;cursor:not-allowed}.plan-speech-agent textarea{box-sizing:border-box;width:100%;padding:11px;border:1px solid #d9e0f4;border-radius:10px;color:inherit;background:white;font:inherit;line-height:1.6;resize:vertical}.plan-speech-agent label{display:block;margin-bottom:8px;font-weight:700}.answer-correction{margin-top:15px}.answer-correction button{margin-top:8px}.test-error{color:#b73b54;line-height:1.7}.test-notice{color:#26816a;line-height:1.7}.test-working{color:#7683af;padding:8px}.plan-test-composer{padding:14px 16px;border-top:1px solid #e2e7f4;background:#fff}.plan-test-composer button{display:block;margin:9px 0 0 auto}.plan-speech-agent .test-primary{background:linear-gradient(110deg,#7489ee,#605bd9);color:#fff;font-weight:700}.plan-speech-agent button:focus-visible,.plan-speech-agent textarea:focus-visible{outline:2px solid #788cf0;outline-offset:2px}
.plan-test-send-actions{display:flex;justify-content:flex-end;align-items:center;gap:14px;margin-top:9px}.plan-test-send-actions button{display:inline-block;margin:0}
</style>
