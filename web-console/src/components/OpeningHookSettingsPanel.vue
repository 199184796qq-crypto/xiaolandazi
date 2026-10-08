<script setup lang="ts">
import { ref, watch } from 'vue'
import { getOpeningHookSettings, saveOpeningHookSettings } from '../api'
import type { OpeningHookAsset } from '../anchorCycleRun'

const props = defineProps<{ planId: number; roomId: number; disabled: boolean }>()
const md = ref(''), mother = ref(''), source = ref(''), error = ref(''), notice = ref('')
const asset = ref<OpeningHookAsset | null>(null), loading = ref(false), saving = ref(false), opened = ref(false)
let epoch = 0
async function load() {
  if (!props.planId || saving.value) return
  const generation = ++epoch, plan = props.planId, room = props.roomId
  loading.value = true; error.value = ''; notice.value = ''
  try {
    const result = await getOpeningHookSettings(plan, room)
    if (generation !== epoch) return
    asset.value = result.asset; mother.value = result.mother_md
    md.value = result.asset?.library_md || result.mother_md
    source.value = result.asset?.source || result.mother_source
  } catch (value) { if (generation === epoch) error.value = value instanceof Error ? value.message : '读取钩子库失败' }
  finally { if (generation === epoch) loading.value = false }
}
async function save(mode: 'project' | 'manual') {
  if (!props.planId || props.disabled || loading.value || saving.value) return
  const generation = epoch, plan = props.planId, room = props.roomId
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const result = await saveOpeningHookSettings(plan, room, mode, mode === 'manual' ? md.value : '')
    if (generation !== epoch) return
    asset.value = result.asset; md.value = result.asset.library_md; source.value = result.asset.source
    notice.value = '已保存为当前方案的新版本，下次开始本场时使用；不修改母版或其他方案。'
  } catch (value) { if (generation === epoch) error.value = value instanceof Error ? value.message : '保存钩子库失败' }
  finally { if (generation === epoch) saving.value = false }
}
async function importFile(event: Event) {
  const input = event.target as HTMLInputElement, file = input.files?.[0], generation = epoch
  input.value = ''
  if (!file || props.disabled || saving.value) return
  if (file.size > 256 * 1024) { error.value = '文件不能超过256KB'; return }
  try {
    const text = await file.text()
    if (generation !== epoch) return
    md.value = text; notice.value = '文件已装载，尚未保存到方案。'; error.value = ''
  } catch { if (generation === epoch) error.value = '文件读取失败' }
}
function toggle(event: Event) {
  opened.value = (event.target as HTMLDetailsElement).open
  if (opened.value && !md.value && !loading.value) void load()
}
watch(() => [props.planId, props.roomId], () => {
  ++epoch; md.value = ''; mother.value = ''; source.value = ''; asset.value = null; error.value = ''; notice.value = ''; loading.value = false; saving.value = false
  if (opened.value) void load()
})
</script>

<template>
  <details @toggle="toggle">
    <summary>大开场 / 循环开场钩子库与选择规则</summary>
    <p class="note">{{ asset ? '当前方案已确认版本 #' + asset.id : '尚无方案版本，新开场默认读取最新项目母版' }}。仅控制两个开场，不改中间、收口或声音。已开始场次的方案版本保持不变。</p>
    <div class="actions">
      <button :disabled="loading || saving || disabled" @click="load">重新读取</button>
      <label>加载 MD<input type="file" accept=".md,text/markdown,text/plain" :disabled="loading || saving || disabled" @change="importFile"></label>
      <button :disabled="loading || saving || disabled || !mother" @click="md = mother; notice = '母版已放入编辑框，尚未保存。'">母版放入编辑框</button>
      <button :disabled="loading || saving || disabled" @click="save('project')">导入最新母版并确认</button>
      <button :disabled="loading || saving || disabled || !md" @click="save('manual')">确认并保存当前编辑版</button>
    </div>
    <p v-if="source" class="note">来源与版本指纹：{{ source }}</p>
    <textarea v-model="md" rows="15" :disabled="loading || saving || disabled" aria-label="开场钩子库母版或方案编辑版" />
    <p v-if="error" class="error">{{ error }}</p><p v-if="notice" class="note">{{ notice }}</p>
  </details>
</template>

<style scoped>
details { margin-top: 16px; } summary { cursor: pointer; font-weight: 700; }
.actions { display: flex; flex-wrap: wrap; gap: 10px; margin: 12px 0; }
textarea { width: 100%; box-sizing: border-box; font-family: monospace; }
.note { color: #647583; line-height: 1.6; overflow-wrap: anywhere; }.error { color: #b32d41; }
</style>
