<script setup lang="ts">
import { ref, watch } from 'vue'
import { classifyQuestionType, deleteQuestionLibrary, listQuestionLibrary, saveQuestionLibrary } from '../api'
import type { QuestionLibraryEntry } from '../types'

const props = defineProps<{ roomId: number | null }>()

type Row = {
  id: number
  question_type: string
  question: string
  answer: string
  saving?: boolean
  deleting?: boolean
}

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
const loading = ref(false)
const error = ref('')
const notice = ref('')

function blankRow(): Row {
  return { id: 0, question_type: 'fact', question: '', answer: '' }
}

async function load() {
  if (!props.roomId) {
    rows.value = []
    return
  }
  loading.value = true
  error.value = ''
  try {
    const result = await listQuestionLibrary(props.roomId)
    rows.value = (result.items || []).map((item: QuestionLibraryEntry) => ({
      id: item.id,
      question_type: item.question_type || 'fact',
      question: item.question || '',
      answer: item.answer || '',
    }))
  } catch (e) {
    error.value = e instanceof Error ? e.message : '读取问题库失败'
  } finally {
    loading.value = false
  }
}

function addRow() {
  rows.value.push(blankRow())
}

async function saveRow(row: Row) {
  if (!props.roomId) {
    error.value = '请先选择直播间'
    return
  }
  if (!row.question.trim()) {
    error.value = '问题要填'
    return
  }
  row.saving = true
  error.value = ''
  notice.value = ''
  try {
    const saved = await saveQuestionLibrary(props.roomId, {
      question_type: row.question_type,
      question: row.question,
      answer: row.answer,
    }, row.id || undefined)
    row.id = saved.id
    row.question_type = saved.question_type
    notice.value = '已保存，回答时优先使用问题库'
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存失败'
  } finally {
    row.saving = false
  }
}

async function removeRow(row: Row) {
  if (!props.roomId || !row.id) {
    rows.value = rows.value.filter(item => item !== row)
    return
  }
  if (!window.confirm('删除这条问题库？')) return
  row.deleting = true
  error.value = ''
  try {
    await deleteQuestionLibrary(props.roomId, row.id)
    rows.value = rows.value.filter(item => item !== row)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '删除失败'
  } finally {
    row.deleting = false
  }
}

async function classify(row: Row) {
  if (!props.roomId || !row.question.trim()) {
    error.value = '先填问题再判断类型'
    return
  }
  error.value = ''
  notice.value = ''
  try {
    const result = await classifyQuestionType(props.roomId, row.question)
    row.question_type = result.type
    notice.value = '模型判断类型：' + (typeOptions.find(item => item.value === result.type)?.label || result.type)
  } catch (e) {
    error.value = e instanceof Error ? e.message : '类型判断失败'
  }
}

watch(() => props.roomId, load, { immediate: true })
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
        <button type="button" class="primary" @click="addRow">添加一行</button>
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
        <tr v-for="row in rows" :key="row.id || row.question" :class="{ unanswered: !row.answer.trim() }">
          <td>
            <select v-model="row.question_type">
              <option v-for="option in typeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
          </td>
          <td>
            <input
              v-model="row.question"
              type="text"
              placeholder="如：上海、发货（右键让模型判断类型）"
              @contextmenu.prevent="classify(row)"
            />
          </td>
          <td>
            <span v-if="!row.answer.trim()" class="ql-chip">待回答</span>
            <textarea v-model="row.answer" rows="2" placeholder="直接写要说的话；留空则标红，系统自动追加的问题先空着" />
          </td>
          <td class="ql-row-actions">
            <button type="button" class="primary" :disabled="row.saving" @click="saveRow(row)">{{ row.saving ? '保存中…' : '保存' }}</button>
            <button type="button" class="danger" :disabled="row.deleting" @click="removeRow(row)">{{ row.deleting ? '删除中…' : '删除' }}</button>
          </td>
        </tr>
        <tr v-if="!rows.length && !loading">
          <td colspan="4" class="ql-empty">还没有条目，点「添加一行」开始。</td>
        </tr>
      </tbody>
    </table>
    <p v-if="notice" class="ql-ok">{{ notice }}</p>
    <p v-if="error" class="ql-error">{{ error }}</p>
  </section>
</template>

<style scoped>
.question-library{display:grid;gap:12px;padding:16px;border:1px solid #e1e6ef;border-radius:12px;background:#fff}
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
button.primary,button.ghost,button.danger{padding:6px 12px;border-radius:6px;border:1px solid transparent;font:inherit;cursor:pointer}
button.primary{background:#6b5cff;color:#fff}
button.ghost{background:#fff;border-color:#d9e0ea;color:#414a57}
button.danger{background:#fff;border-color:#f0c7c7;color:#c62828}
button:disabled{opacity:.6;cursor:default}
.ql-empty{color:#8a93a3;text-align:center}
.ql-ok{color:#1a7f37;font-size:13px;margin:0}
.ql-error{color:#c62828;font-size:13px;margin:0}
</style>
