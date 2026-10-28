<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { classifyQuestionType, deleteQuestionLibrary, getPlanPurposes, listQuestionLibrary, saveQuestionLibrary, getPlanQuestionPurposes, listPlanQuestionLibrary, savePlanQuestionLibrary, deletePlanQuestionLibrary, classifyPlanQuestionType } from '../api'
import type { QuestionLibraryEntry } from '../types'

const props = defineProps<{ roomId?: number | null; planId?: number | null; tenantId?: number }>()
const assetId = computed(() => props.planId || props.roomId || null)
const purposesForAsset = (id: number, query?: string, init?: Parameters<typeof getPlanPurposes>[2]) => props.planId ? getPlanQuestionPurposes(id, props.tenantId, query, init) : getPlanPurposes(id, query, init)
const listForAsset = (id: number) => props.planId ? listPlanQuestionLibrary(id, props.tenantId) : listQuestionLibrary(id)
const saveForAsset = (id: number, payload: Parameters<typeof saveQuestionLibrary>[1], rowId?: number) => props.planId ? savePlanQuestionLibrary(id, payload, rowId, props.tenantId) : saveQuestionLibrary(id, payload, rowId)
const deleteForAsset = (id: number, rowId: number) => props.planId ? deletePlanQuestionLibrary(id, rowId, props.tenantId) : deleteQuestionLibrary(id, rowId)
const classifyForAsset = (id: number, question: string) => props.planId ? classifyPlanQuestionType(id, question, props.tenantId) : classifyQuestionType(id, question)


type Row = {
  clientId: number
  id: number
  question_type: string
  question: string
  answer: string
  sense_key: string
  sense_name: string
  excludeText: string
  excludeEdited: boolean
  suggestions: Suggestion[]
  showAll: boolean
  suggestionQuery: string
  suggestionVersion: number
  saving?: boolean
  deleting?: boolean
}

type PurposeOption = { key: string; name: string }
type Suggestion = PurposeOption & { score: number; suggested_excludes: string[] }

const typeOptions = [
  { value: 'pool_in_out', label: '池内/池外' },
  { value: 'fact', label: '单值事实' },
  { value: 'yes_no', label: '是非/条件' },
  { value: 'number', label: '数值/区间' },
  { value: 'list', label: '枚举' },
  { value: 'process', label: '流程' },
  { value: 'compare', label: '对比' },
  { value: 'unknown', label: '无事实' },
  { value: 'social', label: '社交' },
]

const rows = ref<Row[]>([])
const purposes = ref<PurposeOption[]>([])
const loading = ref(false)
const error = ref('')
const notice = ref('')
const suggestionTimers = new Map<number, ReturnType<typeof setTimeout>>()
let nextClientId = 0
let loadVersion = 0

function blankRow(): Row {
  return {
    clientId: ++nextClientId, id: 0, question_type: 'fact', question: '', answer: '',
    sense_key: '', sense_name: '', excludeText: '', excludeEdited: false,
    suggestions: [], showAll: false, suggestionQuery: '', suggestionVersion: 0,
  }
}

function cancelSuggestion(row: Row) {
  clearTimeout(suggestionTimers.get(row.clientId))
  suggestionTimers.delete(row.clientId)
  row.suggestionVersion += 1
}

function invalidateRequests() {
  loadVersion += 1
  suggestionTimers.forEach(timer => clearTimeout(timer))
  suggestionTimers.clear()
}

function currentRow(row: Row, roomId: number | null, version: number) {
  return assetId.value === roomId && loadVersion === version && rows.value.includes(row)
}

function senseOptions(row: Row): PurposeOption[] {
  if (row.sense_key && !purposes.value.some(item => item.key === row.sense_key)) {
    return [{ key: row.sense_key, name: row.sense_name || row.sense_key }, ...purposes.value]
  }
  return purposes.value
}

function applySense(row: Row, purpose?: PurposeOption) {
  row.sense_key = purpose?.key || ''
  row.sense_name = purpose?.name || ''
  if (!row.excludeEdited) {
    row.excludeText = row.suggestions.find(item => item.key === purpose?.key)?.suggested_excludes.join('、') || ''
  }
}

function pickSense(row: Row, key: string) {
  cancelSuggestion(row)
  applySense(row, row.suggestions.find(item => item.key === key) || senseOptions(row).find(item => item.key === key))
}

function changeSense(row: Row, event: Event) {
  if (event.target instanceof HTMLSelectElement) pickSense(row, event.target.value)
}

function clearSense(row: Row) {
  cancelSuggestion(row)
  row.sense_key = ''
  row.sense_name = ''
  row.excludeText = ''
  row.excludeEdited = false
  // A late response or unchanged blur must not undo an explicit clear.
  row.suggestionQuery = row.question.trim()
}

function questionChanged(row: Row) {
  cancelSuggestion(row)
  row.suggestions = []
  row.suggestionQuery = ''
  row.showAll = false
}

function suggest(row: Row) {
  const roomId = assetId.value
  const query = row.question.trim()
  if (!roomId || !query || query === row.suggestionQuery || row.saving) return
  cancelSuggestion(row)
  const version = loadVersion
  const suggestionVersion = row.suggestionVersion
  const isCurrent = () => currentRow(row, roomId, version) && row.suggestionVersion === suggestionVersion && row.question.trim() === query
  suggestionTimers.set(row.clientId, setTimeout(async () => {
    suggestionTimers.delete(row.clientId)
    if (!isCurrent()) return
    try {
      const result = await purposesForAsset(roomId, query, { suppressErrorDialog: true })
      if (!isCurrent()) return
      row.suggestions = (Array.isArray(result.suggestions) ? result.suggestions : [])
        .filter(item => item && typeof item.key === 'string' && typeof item.name === 'string')
        .map(item => ({ ...item, score: Number(item.score) || 0, suggested_excludes: Array.isArray(item.suggested_excludes) ? item.suggested_excludes.filter(word => typeof word === 'string') : [] }))
        .sort((a, b) => b.score - a.score).slice(0, 3)
      row.suggestionQuery = query
      applySense(row, row.suggestions[0])
    } catch {
      if (!isCurrent()) return
      row.suggestions = []
      row.showAll = true
    }
  }, 350))
}

async function load() {
  invalidateRequests()
  const roomId = assetId.value
  const version = loadVersion
  const isCurrent = () => version === loadVersion && assetId.value === roomId
  error.value = ''
  notice.value = ''
  if (!roomId) {
    rows.value = []
    purposes.value = []
    loading.value = false
    return
  }
  loading.value = true
  // Purpose hints are optional; they must not block the saved question library.
  void purposesForAsset(roomId, undefined, { suppressErrorDialog: true }).then(result => {
    if (isCurrent()) purposes.value = result.items || []
  }).catch(() => {
    if (isCurrent()) purposes.value = []
  })
  try {
    const result = await listForAsset(roomId)
    if (!isCurrent()) return
    rows.value = (result.items || []).map((item: QuestionLibraryEntry) => ({
      ...blankRow(),
      id: item.id,
      question_type: item.question_type || 'fact',
      question: item.question || '',
      answer: item.answer || '',
      sense_key: item.sense_key || '',
      sense_name: item.sense_name || '',
      excludeText: (item.exclude_words || []).join('、'),
      excludeEdited: Boolean(item.exclude_words?.length),
    }))
  } catch (e) {
    if (isCurrent()) error.value = e instanceof Error ? e.message : '读取问题库失败'
  } finally {
    if (isCurrent()) loading.value = false
  }
}

function addRow() {
  rows.value.push(blankRow())
}

async function saveRow(row: Row) {
  if (row.saving) return
  const roomId = assetId.value
  const version = loadVersion
  if (!roomId) {
    error.value = '请先选择方案'
    return
  }
  cancelSuggestion(row)
  if (!row.question.trim()) {
    error.value = '问题要填'
    return
  }
  row.saving = true
  error.value = ''
  notice.value = ''
  try {
    const saved = await saveForAsset(roomId, {
      question_type: row.question_type,
      question: row.question,
      answer: row.answer,
      sense_key: row.sense_key,
      sense_name: row.sense_name,
      exclude_words: row.excludeText.split(/[、,，\s]+/).filter(Boolean),
    }, row.id || undefined)
    if (!currentRow(row, roomId, version)) return
    row.id = saved.id
    row.question_type = saved.question_type
    notice.value = '已保存，回答时优先使用问题库'
  } catch (e) {
    if (currentRow(row, roomId, version)) error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    row.saving = false
  }
}

async function removeRow(row: Row) {
  if (row.saving || row.deleting) return
  const roomId = assetId.value
  const version = loadVersion
  if (!roomId || !row.id) {
    cancelSuggestion(row)
    rows.value = rows.value.filter(item => item !== row)
    return
  }
  if (!window.confirm('删除这条问题库？')) return
  cancelSuggestion(row)
  row.deleting = true
  error.value = ''
  try {
    await deleteForAsset(roomId, row.id)
    if (!currentRow(row, roomId, version)) return
    rows.value = rows.value.filter(item => item !== row)
  } catch (e) {
    if (currentRow(row, roomId, version)) error.value = e instanceof Error ? e.message : '删除失败'
  } finally {
    row.deleting = false
  }
}

async function classify(row: Row) {
  const roomId = assetId.value
  const version = loadVersion
  const question = row.question
  const previousType = row.question_type
  if (!roomId || !question.trim()) {
    error.value = '先填问题再判断类型'
    return
  }
  error.value = ''
  notice.value = ''
  try {
    const result = await classifyForAsset(roomId, question)
    if (!currentRow(row, roomId, version) || row.question !== question || row.question_type !== previousType || row.saving) return
    row.question_type = result.type
    notice.value = '模型判断类型：' + (typeOptions.find(item => item.value === result.type)?.label || result.type)
  } catch (e) {
    if (currentRow(row, roomId, version)) error.value = e instanceof Error ? e.message : '类型判断失败'
  }
}

watch(() => [props.planId, props.tenantId, props.roomId], () => {
  rows.value = []
  purposes.value = []
  void load()
}, { immediate: true })
onBeforeUnmount(invalidateRequests)
</script>

<template>
  <section class="question-library">
    <header class="ql-head">
      <div>
        <h4>问题库</h4>
        <p>按「问题类型 / 问题 / 回答策略」维护；回答时优先查这里，再查事实依据。问题里可用顿号写多个关键词，如「上海、发货」。</p>
      </div>
      <div class="ql-actions">
        <button type="button" class="ghost" :disabled="loading" @click="load">刷新</button>
        <button type="button" class="primary" :disabled="loading || !assetId" @click="addRow">添加一行</button>
      </div>
    </header>
    <table class="ql-table">
      <thead>
        <tr>
          <th style="width: 150px">问题类型</th>
          <th>问题</th>
          <th>回答策略</th>
          <th style="width: 150px">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="row in rows" :key="row.clientId" :class="{ unanswered: !row.answer.trim() }">
          <td>
            <select v-model="row.question_type" :disabled="row.saving">
              <option v-for="option in typeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </td>
          <td>
            <input
              v-model="row.question"
              type="text"
              placeholder="如：上海、发货（右键让模型判断类型）"
              :disabled="row.saving"
              @contextmenu.prevent="classify(row)"
              @input="questionChanged(row)"
              @blur="suggest(row)"
            />
          </td>
          <td>
            <div class="ql-sense">
              <div class="ql-chips" role="group" aria-label="问题含义推荐">
                <span v-if="row.sense_key && !row.suggestions.some(item => item.key === row.sense_key)" class="ql-chip-btn active">{{ row.sense_name || row.sense_key }} ✓</span>
                <button v-for="suggestion in row.suggestions" :key="suggestion.key" type="button" class="ql-chip-btn" :class="{ active: row.sense_key === suggestion.key }" :aria-pressed="row.sense_key === suggestion.key" :disabled="row.saving" @click="pickSense(row, suggestion.key)">
                  {{ suggestion.name }}<span v-if="row.sense_key === suggestion.key"> ✓</span>
                </button>
                <button type="button" class="ql-link" :aria-expanded="row.showAll" :disabled="row.saving" @click="row.showAll = !row.showAll">{{ row.showAll ? '收起' : '更多' }}</button>
                <button type="button" class="ql-link" :disabled="row.saving" @click="clearSense(row)">不指定</button>
              </div>
              <select v-if="row.showAll" :value="row.sense_key" :disabled="row.saving" aria-label="全部问题含义" @change="changeSense(row, $event)">
                <option value="">指的是（可选）</option>
                <option v-for="purpose in senseOptions(row)" :key="purpose.key" :value="purpose.key">{{ purpose.name }}</option>
              </select>
              <label class="ql-excludes">排除词：<input v-model="row.excludeText" type="text" :disabled="row.saving" placeholder="自动填好，可修改" @input="row.excludeEdited = true" /></label>
            </div>
            <span v-if="!row.answer.trim()" class="ql-chip">待回答</span>
            <textarea v-model="row.answer" rows="2" :disabled="row.saving" placeholder="直接写要说的话；留空则标红，系统自动追加的问题先空着" />
          </td>
          <td class="ql-row-actions">
            <button type="button" class="primary" :disabled="row.saving" @click="saveRow(row)">{{ row.saving ? '保存中…' : '保存' }}</button>
            <button type="button" class="danger" :disabled="row.deleting || row.saving" @click="removeRow(row)">{{ row.deleting ? '删除中…' : '删除' }}</button>
          </td>
        </tr>
        <tr v-if="!rows.length && loading">
          <td colspan="4" class="ql-empty">正在读取问题库…</td>
        </tr>
        <tr v-else-if="!rows.length && error">
          <td colspan="4" class="ql-empty">问题库暂时无法读取，请点击“刷新”重试。</td>
        </tr>
        <tr v-else-if="!rows.length">
          <td colspan="4" class="ql-empty">还没有条目，点「添加一行」开始。</td>
        </tr>
      </tbody>
    </table>
    <p v-if="notice" class="ql-ok">{{ notice }}</p>
    <p v-if="error" class="ql-error">{{ error }}</p>
  </section>
</template>

<style scoped>
.question-library{display:grid;gap:12px;padding:16px;border:1px solid #e1e6ef;border-radius:12px;background:#fff;font-size:16px;min-width:0;overflow-x:auto}
.ql-head{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}
.ql-head h4{margin:0 0 6px;font-size:20px}
.ql-head p{margin:0;color:#5b6472;font-size:16px;line-height:1.6}
.ql-actions{display:flex;gap:8px}
.ql-table{width:100%;border-collapse:collapse;font-size:16px}
.ql-table tr.unanswered td{background:#fff5f5}
.ql-chip{display:inline-block;margin-bottom:4px;padding:1px 8px;border-radius:10px;background:#fde2e1;color:#c62828;font-size:13px}
.ql-table th,.ql-table td{border-bottom:1px solid #eef1f6;padding:8px 6px;text-align:left;vertical-align:top}
.ql-table th{color:#6b7484;font-weight:600;font-size:20px;padding-bottom:10px}
.ql-table input,.ql-table select,.ql-table textarea{width:100%;box-sizing:border-box;padding:6px 8px;border:1px solid #d9e0ea;border-radius:6px;font:inherit;background:#fff}
.ql-table textarea{resize:vertical}
.ql-row-actions{display:flex;gap:6px}
.ql-sense{display:grid;gap:8px;margin-bottom:6px}
.ql-chips{display:flex;gap:6px;align-items:center;flex-wrap:wrap}
.ql-chip-btn{display:inline-block;padding:5px 10px;border-radius:999px;border:1px solid #d9e0ea;background:#fff;color:#414a57;font:inherit;cursor:pointer}
.ql-chip-btn.active{border-color:#6b5cff;background:#efeaff;color:#4a3fd6;font-weight:600}
.ql-link{padding:5px 6px;border:0;background:transparent;color:#6253db;font:inherit;cursor:pointer;white-space:nowrap}
.ql-excludes{display:flex;align-items:center;gap:6px;color:#5b6472;white-space:nowrap}
.ql-excludes input{flex:1;min-width:0}
button.primary,button.ghost,button.danger{padding:6px 12px;border-radius:6px;border:1px solid transparent;font:inherit;cursor:pointer}
button.primary{background:#6b5cff;color:#fff}
button.ghost{background:#fff;border-color:#d9e0ea;color:#414a57}
button.danger{background:#fff;border-color:#f0c7c7;color:#c62828}
button:disabled{opacity:.6;cursor:default}
.ql-empty{color:#8a93a3;text-align:center}
.ql-ok{color:#1a7f37;font-size:13px;margin:0}
.ql-error{color:#c62828;font-size:13px;margin:0}
</style>
