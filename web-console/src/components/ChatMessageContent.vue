<script setup lang="ts">
import { computed } from 'vue'
import { chatDisplayBlocks, chatInlineText } from '../chatMessageDisplay'
import StyleDisplayText from './StyleDisplayText.vue'
const props = defineProps<{ text: string }>()
const blocks = computed(() => chatDisplayBlocks(props.text))
</script>

<template>
  <div class="chat-message-content">
    <template v-for="(block, index) in blocks" :key="index">
      <h4 v-if="block.kind === 'heading'"><StyleDisplayText :text="chatInlineText(block.text)" /></h4>
      <blockquote v-else-if="block.kind === 'quote'"><StyleDisplayText :text="chatInlineText(block.text)" /></blockquote>
      <pre v-else-if="block.kind === 'code'"><code>{{ block.text }}</code></pre>
      <component :is="block.ordered ? 'ol' : 'ul'" v-else-if="block.kind === 'list'">
        <li v-for="(item, itemIndex) in block.items" :key="itemIndex" :value="item.number"><StyleDisplayText :text="chatInlineText(item.text)" /></li>
      </component>
      <p v-else><StyleDisplayText :text="chatInlineText(block.text)" /></p>
    </template>
  </div>
</template>

<style scoped>
.chat-message-content{display:grid;gap:.65em;min-width:0;overflow-wrap:anywhere;white-space:normal;font-size:inherit;line-height:inherit}
.chat-message-content p,.chat-message-content h4,.chat-message-content blockquote,.chat-message-content pre,.chat-message-content ul,.chat-message-content ol{margin:0;font-size:inherit;line-height:inherit;color:inherit}
.chat-message-content p,.chat-message-content li,.chat-message-content blockquote{white-space:pre-wrap}
.chat-message-content h4{font-weight:750}.chat-message-content ul,.chat-message-content ol{padding-left:1.5em;display:grid;gap:.3em}
.chat-message-content blockquote{padding-left:.8em;border-left:3px solid #ddd7fc;color:#647390}
.chat-message-content pre{min-width:0;max-width:100%;padding:.65em .8em;border-radius:9px;background:#f3f4fb;white-space:pre-wrap;overflow-wrap:anywhere}
.chat-message-content :deep(strong){display:inline;font-size:inherit;font-weight:750;line-height:inherit}
.chat-message-content :deep(em){font-size:inherit;line-height:inherit}.chat-message-content :deep(code){font-size:.94em;font-family:inherit;background:#f3f4fb;border-radius:4px;padding:0 .15em}
</style>
