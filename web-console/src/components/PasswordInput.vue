<script setup lang="ts">
import { ref, useAttrs } from 'vue'

defineOptions({ inheritAttrs: false })

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ (event: 'update:modelValue', value: string): void }>()
const attrs = useAttrs()
const visible = ref(false)

function onInput(event: Event) {
  emit('update:modelValue', (event.target as HTMLInputElement).value)
}
</script>

<template>
  <div class="password-input-shell">
    <input
      v-bind="attrs"
      :type="visible ? 'text' : 'password'"
      :value="modelValue"
      @input="onInput"
    />
    <button
      class="password-eye-button"
      type="button"
      :aria-label="visible ? '隐藏密码' : '显示密码'"
      :title="visible ? '隐藏密码' : '显示密码'"
      @click="visible = !visible"
    >
      <span aria-hidden="true">{{ visible ? '◉' : '◎' }}</span>
    </button>
  </div>
</template>