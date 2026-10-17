<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { factCategoryState } from '../factCategories'

type FactDraft = { category: string; key: string; value: string; forbidden_wording: string; safe_rewrite: string }
const draft = defineModel<FactDraft>({ required: true })
const props = defineProps<{ number: number; saving: boolean; nameLoading: boolean; nameError: string; error: string; guidance: string }>()
const emit = defineEmits<{ save: []; close: []; changed: []; nameEdited: [] }>()
const row = ref<HTMLTableRowElement | null>(null)
const input = ref<HTMLTextAreaElement | null>(null)
const canSave = computed(() => !props.saving && !factCategoryState.loading && !factCategoryState.error && factCategoryState.items.some(item => item.value === draft.value.category) && !!draft.value.value.trim())
const composing = ref(false)
const showName = ref(false)
watch(() => props.nameError, value => { if (value) showName.value = true }, { immediate: true })
let blurredDuringComposition = false
function save() { if (!composing.value && canSave.value) emit('save') }
function blur(event: FocusEvent) {
  if (event.relatedTarget instanceof Node && row.value?.contains(event.relatedTarget)) return
  if (composing.value) { blurredDuringComposition = true; return }
  save()
}
function compositionEnd() {
  composing.value = false
  if (blurredDuringComposition) { blurredDuringComposition = false; save() }
}
onMounted(async () => {
  await nextTick()
  input.value?.focus({ preventScroll: true })
  input.value?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
})
</script>

<template>
  <tr ref="row" class="inline-formal-fact-create-row" data-formal-fact-draft :aria-busy="saving" @focusout="blur">
    <td class="draft-number"><span>{{ number }}</span></td>
    <td><span class="draft-scope">保存后选择归属</span></td>
    <td class="draft-content">
      <p v-if="guidance" class="draft-guidance">{{ guidance }}</p>
      <textarea ref="input" v-model="draft.value" aria-label="新增事实内容" aria-describedby="inline-create-status" :aria-invalid="!!error" :disabled="saving" rows="3" maxlength="4000" placeholder="请输入真实的事实内容" @input="emit('changed')" @compositionstart="composing = true" @compositionend="compositionEnd" @keydown.ctrl.enter.prevent="save" @keydown.meta.enter.prevent="save"></textarea>
      <label v-if="showName" class="draft-name">事实名称<input v-model="draft.key" aria-label="新增事实名称" maxlength="255" :disabled="saving" placeholder="自动提炼失败，可手动填写名称" @input="emit('nameEdited')" @compositionstart="composing = true" @compositionend="compositionEnd" /></label>
      <p id="inline-create-status" class="draft-status" role="status">{{ saving ? '正在保存并整理四层内容…' : nameLoading ? '正在提炼事实名称…' : '输入完成后移开焦点自动保存' }}</p>
      <p v-if="error || nameError" class="draft-error" role="alert">{{ error || nameError }}</p>
    </td>
    <td class="draft-progress">{{ saving ? '整理中' : '待保存' }}</td>
    <td><div class="draft-actions"><button type="button" :disabled="!canSave" @click="save">{{ saving ? '保存中…' : '保存' }}</button><button type="button" class="draft-cancel" :disabled="saving" @click="emit('close')">取消</button></div></td>
  </tr>
</template>

<style scoped>
.inline-formal-fact-create-row>td { padding:12px; border-top:1px solid #ccd9fb; background:#f4f7ff; vertical-align:top; font-size:16px; line-height:1.6; }
.draft-number { text-align:center; font-variant-numeric:tabular-nums; }
.draft-number>span { display:inline-grid; place-items:center; width:38px; height:38px; border-radius:12px; background:#e5eaff; color:#5272f5; font-size:16px; }
.draft-progress { color:#687ca4; font-size:16px; }
.draft-scope,.draft-status { color:#637795; font-size:16px; }
.draft-content { min-width:0; }
.draft-content textarea,.draft-content input { box-sizing:border-box; display:block; width:100%; padding:10px 12px; border:1px solid #9cb1e7; border-radius:9px; background:#fff; color:#111; font:inherit; font-size:16px; line-height:1.7; }
.draft-content textarea { min-height:108px; resize:vertical; }
.draft-content textarea:focus,.draft-content input:focus { outline:2px solid #d2dcff; outline-offset:1px; }
.draft-content textarea:disabled,.draft-content input:disabled { opacity:.8; }
.draft-status,.draft-error,.draft-guidance { margin:8px 0 0; font-size:16px; overflow-wrap:anywhere; }
.draft-guidance { margin:0 0 8px; color:#435b88; }
.draft-error { color:#bf3150; }
.draft-name { display:grid; gap:6px; margin-top:8px; font-size:16px; }
.draft-actions { display:flex; align-items:center; gap:8px; flex-wrap:wrap; }
.draft-actions button { min-height:40px; padding:7px 16px; border:1px solid #5f69ed; border-radius:9px; background:#575be9; color:#fff; font:inherit; font-size:16px; cursor:pointer; }
.draft-actions .draft-cancel { border-color:#c8d7f8; background:#fff; color:#48618a; }
.draft-actions button:disabled { cursor:default; opacity:.55; }
</style>
