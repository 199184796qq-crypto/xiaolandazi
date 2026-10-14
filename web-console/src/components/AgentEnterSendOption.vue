<script setup lang="ts">
import { useAgentEnterSend } from '../useAgentEnterSend'
withDefaults(defineProps<{ disabled?: boolean; label?: string }>(), { label: '回车' })
const enterSend = useAgentEnterSend()
</script>

<template>
  <label class="agent-enter-send-option" title="勾选后回车发送，Shift+回车换行">
    <input v-model="enterSend" type="checkbox" :disabled="disabled" />
    <span>{{ label }}</span>
  </label>
</template>

<style scoped>
.agent-enter-send-option{display:inline-flex!important;align-items:center;gap:7px;margin:0!important;color:#7482a1;font-size:13px;font-weight:500!important;line-height:1.5;white-space:nowrap;cursor:pointer}
.agent-enter-send-option input{width:16px;height:16px;min-height:0;margin:0;padding:0;accent-color:#625bda;cursor:pointer}
.agent-enter-send-option:has(input:disabled){opacity:.6;cursor:default}
.agent-enter-send-option input:focus-visible{outline:2px solid #788cf0;outline-offset:3px}
</style>
