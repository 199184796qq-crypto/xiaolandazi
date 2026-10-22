<script setup lang="ts">
import { computed } from 'vue'
import type { LiveAgentCustomMainlineResponse } from '../types'

const props = defineProps<{
  draft: LiveAgentCustomMainlineResponse
  audioSrc: string
  durationLabel: string
  busy?: boolean
  cloneBusy?: boolean
}>()

const emit = defineEmits<{
  remove: []
  retryClone: []
}>()

const hasVoiceIdentity = computed(() => Boolean(props.draft.voice_identity?.voice_id))
const disabled = computed(() => Boolean(props.busy || props.cloneBusy))
</script>

<template>
  <section class="recording-original-media" aria-label="原始声音与字幕">
    <header class="recording-original-media__header">
      <strong>原始声音</strong>
      <button
        class="recording-original-media__remove"
        type="button"
        title="移除原音和 SRT"
        aria-label="移除原音和 SRT"
        :disabled="disabled"
        @click="emit('remove')"
      >
        <svg aria-hidden="true" viewBox="0 0 24 24" fill="none">
          <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13M10 11v5M14 11v5" />
        </svg>
      </button>
    </header>

    <div class="recording-original-media__summary">
      <div class="recording-original-media__item">
        <span class="recording-original-media__icon" aria-hidden="true">≋</span>
        <div>
          <strong>原始声音</strong>
          <span>{{ durationLabel }}</span>
        </div>
      </div>
      <div class="recording-original-media__item" :class="{ ready: hasVoiceIdentity }">
        <span class="recording-original-media__icon identity" aria-hidden="true">●</span>
        <div>
          <strong>原始声音身份</strong>
          <span v-if="hasVoiceIdentity">
            {{ draft.voice_identity?.name || '已生成' }} · 已自动生成
          </span>
          <span v-else>尚未生成</span>
        </div>
        <button
          v-if="!hasVoiceIdentity"
          class="recording-original-media__retry"
          type="button"
          :disabled="disabled"
          @click="emit('retryClone')"
        >
          {{ cloneBusy ? '生成中…' : '重新生成自定义音色' }}
        </button>
      </div>
    </div>

    <audio :key="draft.audio_asset_id" controls preload="metadata" :src="audioSrc"></audio>

    <details v-if="draft.srt.trim()" class="recording-original-media__srt">
      <summary><span aria-hidden="true">▤</span> 查看 SRT</summary>
      <pre>{{ draft.srt }}</pre>
    </details>
  </section>
</template>

<style scoped>
.recording-original-media{box-sizing:border-box;display:grid;gap:16px;width:100%;min-width:0;padding:18px;border:1px solid #dfe6f2;border-radius:18px;background:#fff;color:#344560}
.recording-original-media__header{display:flex;align-items:center;justify-content:space-between;gap:12px;min-width:0}
.recording-original-media__header>strong{font-size:18px;font-weight:850}
.recording-original-media button{box-sizing:border-box;flex-shrink:0;font:inherit;font-size:16px;font-weight:750;cursor:pointer;transition:background .16s ease,border-color .16s ease,color .16s ease,box-shadow .16s ease}
.recording-original-media button:focus-visible,.recording-original-media summary:focus-visible{outline:3px solid rgba(102,100,239,.28);outline-offset:3px}
.recording-original-media button:disabled{opacity:.45;cursor:not-allowed}
.recording-original-media__remove{display:grid;place-items:center;width:36px;height:36px;padding:9px;border:1px solid #dfe4f0;border-radius:11px;background:#f8f9fe;color:#7b84a0}
.recording-original-media__remove svg{display:block;width:16px;height:16px;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}
.recording-original-media__remove:not(:disabled):hover{border-color:#efbdc6;background:#fff4f6;color:#cd455a}
.recording-original-media__summary{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:12px;min-width:0}
.recording-original-media__item{box-sizing:border-box;display:flex;align-items:center;gap:11px;flex-wrap:wrap;min-width:0;min-height:74px;padding:14px;border:1px solid #e0e7f3;border-radius:14px;background:#fbfcff}
.recording-original-media__icon{display:grid;flex:0 0 34px;place-items:center;width:34px;height:34px;border-radius:10px;background:#edf0ff;color:#6b68ed;font-size:26px;line-height:1}
.recording-original-media__icon.identity{background:#edf6f3;color:#43aa7f;font-size:14px}
.recording-original-media__item>div{display:grid;flex:1 1 120px;gap:5px;min-width:0}
.recording-original-media__item strong{color:#425574;font-size:16px;font-weight:800}
.recording-original-media__item>div>span{color:#7c89a0;font-size:16px;line-height:1.6;overflow-wrap:anywhere}
.recording-original-media__item.ready{border-color:#c1e5d5;background:#f2faf6}
.recording-original-media__retry{max-width:100%;min-height:38px;margin-left:auto;padding:8px 12px;border:1px solid #d7dafa;border-radius:10px;background:#f2f1ff;color:#635bda;white-space:normal;overflow-wrap:anywhere}
.recording-original-media__retry:not(:disabled):hover{border-color:#b6b1ef;background:#eae7ff}
.recording-original-media>audio{display:block;box-sizing:border-box;width:100%;min-width:0;max-width:100%;height:48px;border-radius:999px}
.recording-original-media__srt{min-width:0;padding:13px 15px;border:1px solid #dfe6f3;border-radius:12px;background:#fbfcff}
.recording-original-media__srt summary{color:#625ed7;font-size:16px;font-weight:800;cursor:pointer}
.recording-original-media__srt summary>span{margin-right:5px}
.recording-original-media__srt pre{box-sizing:border-box;max-width:100%;max-height:300px;margin:12px 0 0;padding:12px;overflow:auto;border-radius:9px;background:#f1f4fa;color:#53617a;font:16px/1.7 ui-monospace,SFMono-Regular,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere}
@media(max-width:640px){.recording-original-media{gap:14px;padding:14px}.recording-original-media__summary{grid-template-columns:minmax(0,1fr)}.recording-original-media__item{padding:12px}.recording-original-media__srt{padding:12px}}
@media(prefers-reduced-motion:reduce){.recording-original-media button{transition:none}}
</style>
