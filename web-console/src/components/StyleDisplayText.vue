<script setup lang="ts">
import { computed } from 'vue'
import { styleInlineTokens } from '../anchorStyleDisplay'
const props = defineProps<{ text: string }>()
const tokens = computed(() => styleInlineTokens(props.text))
</script>

<template>
  <template v-for="(token, index) in tokens" :key="index">
    <strong v-if="token.kind === 'strong'">{{ token.text }}</strong>
    <em v-else-if="token.kind === 'em'">{{ token.text }}</em>
    <code v-else-if="token.kind === 'code'">{{ token.text }}</code>
    <span v-else>{{ token.text }}</span>
  </template>
</template>
