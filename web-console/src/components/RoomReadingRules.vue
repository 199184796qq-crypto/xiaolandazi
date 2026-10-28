<script setup lang="ts">
import { ref, watch } from 'vue'
import { getRoomReadingRules, updateRoomReadingRules } from '../api'

const props = defineProps<{ roomId: number | null }>()

const rulesText = ref('')
const loading = ref(false)
const saving = ref(false)
const message = ref('')
const error = ref('')

const sample = `南充特产绸都杨鸭子 => 南充特产|绸都杨鸭子
绸都 => chou2 du1`

async function load() {
  if (!props.roomId) {
    rulesText.value = ''
    return
  }
  loading.value = true
  error.value = ''
  message.value = ''
  try {
    const item = await getRoomReadingRules(props.roomId)
    rulesText.value = item.rules_text || ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : '读取读写规则失败'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!props.roomId) {
    error.value = '请先选择直播间'
    return
  }
  saving.value = true
  error.value = ''
  message.value = ''
  try {
    const item = await updateRoomReadingRules(props.roomId, rulesText.value)
    rulesText.value = item.rules_text || ''
    message.value = '已保存，开播后送 TTS 前生效'
  } catch (e) {
    error.value = e instanceof Error ? e.message : '保存读写规则失败'
  } finally {
    saving.value = false
  }
}

watch(() => props.roomId, load, { immediate: true })
</script>

<template>
  <section class="reading-rules">
    <header>
      <div>
        <h3>读写规则</h3>
        <p>一行一条，格式：<code>原文 =&gt; 断点/读法</code>。断点用 <code>|</code> 标出（停顿 200ms），读法用拼音（如 <code>chou2 du1</code>）。</p>
      </div>
      <button type="button" class="primary" :disabled="saving || loading" @click="save">{{ saving ? '保存中…' : '保存' }}</button>
    </header>
    <textarea
      v-model="rulesText"
      rows="10"
      spellcheck="false"
      placeholder="南充特产绸都杨鸭子 => 南充特产|绸都杨鸭子&#10;绸都 => chou2 du1"
      :disabled="loading"
    />
    <p class="hint">示例：</p>
    <pre class="sample">{{ sample }}</pre>
    <p v-if="message" class="ok">{{ message }}</p>
    <p v-if="error" class="error">{{ error }}</p>
  </section>
</template>

<style scoped>
.reading-rules{display:grid;gap:12px;padding:16px;border:1px solid #e1e6ef;border-radius:12px;background:#fff}
.reading-rules header{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}
.reading-rules h3{margin:0 0 4px;font-size:16px}
.reading-rules p{margin:0;color:#5b6472;font-size:13px;line-height:1.6}
.reading-rules code{background:#f2f5fa;border-radius:4px;padding:0 4px}
.reading-rules textarea{width:100%;box-sizing:border-box;padding:10px 12px;border:1px solid #d9e0ea;border-radius:8px;font-family:inherit;font-size:14px;line-height:1.7;resize:vertical}
.reading-rules .primary{padding:8px 18px;border:0;border-radius:8px;background:#6b5cff;color:#fff;font-weight:600;cursor:pointer}
.reading-rules .primary:disabled{opacity:.6;cursor:default}
.reading-rules .hint{font-size:12px;color:#8a93a3}
.reading-rules .sample{margin:0;padding:10px 12px;background:#f7f9fc;border-radius:8px;font-size:13px;color:#414a57;white-space:pre-wrap}
.reading-rules .ok{color:#1a7f37;font-size:13px}
.reading-rules .error{color:#c62828;font-size:13px}
</style>
