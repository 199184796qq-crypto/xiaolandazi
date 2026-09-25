<script setup lang="ts">
import { computed } from 'vue'
import { inboxCountFor, canUseWorkInbox } from '../workInbox'
import { session } from '../session'
const props=defineProps<{to?:string;count?:number}>()
const value=computed(()=>canUseWorkInbox(session.bootstrap?.actor.role)?(props.count===undefined?inboxCountFor(props.to):props.count):0)
</script>
<template><span v-if="Number.isSafeInteger(value)&&value>0" class="todo-badge" :aria-label="value+'件待办'" :title="value+'件待办'">{{value}}</span></template>
<style scoped>
.todo-badge{position:absolute;top:2px;right:3px;display:inline-flex;align-items:center;justify-content:center;box-sizing:border-box;min-width:22px;height:22px;padding:0 5px;border-radius:999px;background:#c92d3b;color:#fff!important;font-size:14px!important;font-weight:700;line-height:1;letter-spacing:0;pointer-events:none;z-index:3;box-shadow:0 0 0 2px #fff}
</style>
