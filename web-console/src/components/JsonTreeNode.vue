<script setup lang="ts">
import { computed, ref, watch } from 'vue'

const props = withDefaults(
  defineProps<{
    name?: string
    value: unknown
    depth?: number
    expandToken?: number
    expandAll?: boolean
    path?: string
    notes?: Record<string, string>
  }>(),
  {
    name: '',
    depth: 0,
    expandToken: 0,
    expandAll: false,
    path: '',
    notes: () => ({}),
  },
)

const open = ref(props.depth < 1)
const isArray = computed(() => Array.isArray(props.value))
const isObject = computed(
  () => props.value !== null && typeof props.value === 'object' && !Array.isArray(props.value),
)
const isBranch = computed(() => isArray.value || isObject.value)

const entries = computed<[string, unknown][]>(() => {
  if (Array.isArray(props.value)) {
    return props.value.map((value, index) => [String(index), value])
  }
  if (isObject.value) {
    return Object.entries(props.value as Record<string, unknown>)
  }
  return []
})

const typeLabel = computed(() => {
  if (isArray.value) return `数组 · ${entries.value.length} 项`
  if (isObject.value) return `对象 · ${entries.value.length} 项`
  if (props.value === null) return 'null'
  if (typeof props.value === 'string') return '字符串'
  if (typeof props.value === 'number') return '数字'
  if (typeof props.value === 'boolean') return '布尔'
  return typeof props.value
})

const scalarText = computed(() => {
  if (typeof props.value === 'string') return props.value
  if (props.value === null) return 'null'
  return String(props.value)
})

const currentNote = computed(() => props.notes[props.path] || '')

function childPath(entryName: string) {
  const segment = isArray.value ? `[${entryName}]` : entryName
  if (!props.path) return segment
  return isArray.value ? props.path + segment : props.path + '.' + segment
}

watch(
  () => props.expandToken,
  () => {
    if (isBranch.value) open.value = props.expandAll
  },
)
</script>

<template>
  <div class="json-tree-node">
    <button
      v-if="isBranch"
      class="json-tree-row json-tree-branch-row"
      type="button"
      :style="{ paddingLeft: depth * 20 + 8 + 'px' }"
      @click="open = !open"
    >
      <span class="json-tree-arrow" :class="{ open }">›</span>
      <strong v-if="name" class="json-tree-key">{{ name }}</strong>
      <span class="json-tree-type">{{ typeLabel }}</span>
      <span class="json-tree-bracket">{{ isArray ? '[ ]' : '{ }' }}</span>
      <span v-if="currentNote" class="json-tree-note">{{ currentNote }}</span>
    </button>

    <div
      v-else
      class="json-tree-row json-tree-value-row"
      :style="{ paddingLeft: depth * 20 + 34 + 'px' }"
    >
      <strong v-if="name" class="json-tree-key">{{ name }}</strong>
      <span class="json-tree-type">{{ typeLabel }}</span>
      <span
        class="json-tree-value"
        :class="{
          string: typeof value === 'string',
          number: typeof value === 'number',
          boolean: typeof value === 'boolean',
          null: value === null,
        }"
      >{{ scalarText }}</span>
      <span v-if="currentNote" class="json-tree-note">{{ currentNote }}</span>
    </div>

    <div v-if="isBranch && open" class="json-tree-children">
      <JsonTreeNode
        v-for="[entryName, entryValue] in entries"
        :key="entryName"
        :name="isArray ? '[' + entryName + ']' : entryName"
        :value="entryValue"
        :depth="depth + 1"
        :expand-token="expandToken"
        :expand-all="expandAll"
        :path="childPath(entryName)"
        :notes="notes"
      />
      <div
        v-if="entries.length === 0"
        class="json-tree-empty"
        :style="{ paddingLeft: (depth + 1) * 20 + 34 + 'px' }"
      >
        空{{ isArray ? '数组' : '对象' }}
      </div>
    </div>
  </div>
</template>

<style scoped>
.json-tree-node { min-width: max-content; }
.json-tree-row {
  width: 100%;
  min-height: 36px;
  display: flex;
  align-items: center;
  gap: 9px;
  border: 0;
  border-bottom: 1px solid rgba(91, 111, 175, 0.08);
  background: transparent;
  color: #2d3650;
  text-align: left;
  box-sizing: border-box;
}
.json-tree-branch-row { cursor: pointer; }
.json-tree-branch-row:hover,
.json-tree-value-row:hover { background: rgba(91, 104, 218, 0.055); }
.json-tree-arrow {
  width: 16px;
  flex: 0 0 16px;
  color: #6570d9;
  font-size: 22px;
  line-height: 1;
  transform: rotate(0deg);
  transition: transform 0.16s ease;
}
.json-tree-arrow.open { transform: rotate(90deg); }
.json-tree-key {
  color: #3d4b72;
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 16px;
}
.json-tree-type {
  color: #8a93aa;
  font-size: 14px;
  white-space: nowrap;
}
.json-tree-bracket {
  color: #a1a8bb;
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 15px;
}
.json-tree-value {
  max-width: 900px;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  font-family: "Cascadia Code", "Consolas", monospace;
  font-size: 15px;
}
.json-tree-value.string { color: #2d6b52; }
.json-tree-value.number { color: #8c5b1e; }
.json-tree-value.boolean { color: #5b55c7; }
.json-tree-value.null { color: #9a6074; }
.json-tree-note {
  max-width: 620px;
  margin-left: 8px;
  color: #8a91a5;
  font-size: 14px;
  line-height: 1.45;
  white-space: normal;
}
.json-tree-empty {
  min-height: 34px;
  display: flex;
  align-items: center;
  color: #9aa2b5;
  font-size: 15px;
}
</style>
