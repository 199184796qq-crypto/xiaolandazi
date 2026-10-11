<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { request } from '../api'
const props = defineProps<{ planId: number; roomId: number; tenantId?: number; factId: number; versionNo: number; selectedText: string; disabled?: boolean }>()
const emit = defineEmits<{ busy: [value: boolean]; adopted: [constraint: string] }>()
type Draft = { state: 'draft' | 'clarify'; draft_id?: string; understanding: string; constraint?: string; question?: string; scope?: string[] }
const opened = ref(false), feedback = ref(''), draft = ref<Draft | null>(null), pending = ref(''), error = ref(''), notice = ref(''), adopted = ref(false)
let epoch = 0, controller: AbortController | null = null
function resetRequest() { controller?.abort(); ++epoch; pending.value = ''; emit('busy', false); draft.value = null; error.value = ''; notice.value = ''; adopted.value = false }
watch(() => [props.factId, props.versionNo, props.planId, props.roomId, props.tenantId], () => { resetRequest(); opened.value = false; feedback.value = '' })
watch(() => props.selectedText, resetRequest)
watch(feedback, () => { draft.value = null; adopted.value = false; notice.value = ''; error.value = '' })
onBeforeUnmount(() => { controller?.abort(); ++epoch })
async function perform(action: 'prepare' | 'adopt') {
  if (pending.value || props.disabled || !feedback.value.trim() || action === 'adopt' && !draft.value?.draft_id) return
  const owner = ++epoch; controller?.abort(); controller = new AbortController()
  pending.value = action; emit('busy', true); error.value = ''; notice.value = ''
  const body = { room_id: props.roomId, tenant_id: props.tenantId, version_no: props.versionNo,
    ...(action === 'prepare' ? { selected_text: props.selectedText, feedback: feedback.value.trim() } : { draft_id: draft.value?.draft_id }) }
  try {
    const result = await request<Draft & { notice?: string }>(`/api/v1/live-agent-plans/${props.planId}/facts/${props.factId}/speech-constraints/${action}`, { method: 'POST', body: JSON.stringify(body), signal: controller.signal, suppressErrorDialog: true })
    if (owner !== epoch) return
    if (action === 'prepare') { draft.value = result; adopted.value = false }
    else { adopted.value = true; notice.value = result.notice || '已采用并记录，后续新生成的话术会遵守这条约束。'; emit('adopted', result.constraint || draft.value?.constraint || '') }
  } catch (e) { if (owner === epoch && !(e instanceof Error && e.name === 'AbortError')) error.value = e instanceof Error ? e.message : '约束未保存，请重试' }
  finally { if (owner === epoch) { pending.value = ''; emit('busy', false) } }
}
</script>
<template>
  <section class="speech-constraint-editor" aria-label="补充表达约束">
    <button type="button" :disabled="disabled || !!pending" @click="opened = !opened">补充表达约束</button>
    <form v-if="opened" @submit.prevent="perform('prepare')">
      <label>你希望以后怎么讲？<textarea v-model="feedback" rows="3" maxlength="5000" :disabled="!!pending" placeholder="例如：不要拿早餐举例，也不要用早上赶时间暗示早餐。直接说你的要求就好。" /></label>
      <small>后台会结合当前事实整理约束，采用后才记录并使用。</small>
      <button type="submit" :disabled="disabled || !!pending || !feedback.trim()">{{ pending === 'prepare' ? '正在整理…' : '生成约束' }}</button>
      <div v-if="draft" class="constraint-draft">
        <strong>{{ draft.state === 'clarify' ? '需要补充的信息' : adopted ? '已采用的约束' : '待采用约束' }}</strong>
        <p class="understanding">{{ draft.understanding }}</p>
        <p v-if="draft.question" role="status">{{ draft.question }} 请补充后重新生成。</p>
        <template v-else><p class="constraint-text">{{ draft.constraint }}</p><small v-if="draft.scope?.length">适用当前方案：{{ draft.scope.join('、') }}</small><button type="button" :disabled="disabled || !!pending || adopted" @click="perform('adopt')">{{ pending === 'adopt' ? '正在记录…' : adopted ? '已采用' : '采用约束' }}</button></template>
      </div>
      <p v-if="notice" class="success" role="status">{{ notice }}</p><p v-if="error" class="error" role="alert">{{ error }}</p>
    </form>
  </section>
</template>
<style scoped>
.speech-constraint-editor{margin-top:16px}button{padding:9px 15px;border:1px solid #d4dcf6;border-radius:10px;color:#5b60bd;background:#fff;font:inherit;font-weight:600;cursor:pointer}button:disabled{opacity:.55;cursor:default}form{margin-top:12px;padding:17px;border:1px solid #dfdafa;border-radius:13px;background:#f8f6ff}label{display:block;font-weight:700}textarea{box-sizing:border-box;display:block;width:100%;margin:9px 0;padding:12px;border:1px solid #d9def4;border-radius:9px;background:#fff;color:#34486b;font:inherit;line-height:1.6;resize:vertical}small{display:block;margin:8px 0;color:#7c89a3;line-height:1.6}.constraint-draft{margin-top:14px;padding:14px;border-radius:10px;background:#fff;border:1px solid #e0e5fa}.understanding{color:#7b89a5;font-size:13px}.constraint-text{color:#34486b;font-size:16px;line-height:1.8;white-space:pre-wrap}.success{color:#218269;line-height:1.6}.error{color:#b04762;line-height:1.6}
</style>
