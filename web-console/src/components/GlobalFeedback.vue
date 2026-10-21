<script setup lang="ts">
import { ref, watch } from 'vue'
import {
  clearErrorHistory,
  dismissAlert,
  dismissToast,
  feedbackState,
  resolveConfirm,
} from '../uiFeedback'

const historyOpen = ref(false)
const copyStatus = ref('')
watch(() => feedbackState.alertMessage, () => { copyStatus.value = '' })
async function copyDiagnostic(message: string) {
  try {
    await navigator.clipboard.writeText(message)
    copyStatus.value = '已复制'
  } catch {
    copyStatus.value = '复制不可用，请选中文字手动复制'
  }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="feedbackState.alertOpen"
      class="modal-backdrop system-confirm-backdrop system-alert-backdrop"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby="system-error-title"
      aria-describedby="system-error-message"
      @click.self="dismissAlert"
    >
      <section class="system-confirm-card system-alert-card">
        <div class="system-confirm-icon danger">!</div>
        <div class="system-confirm-copy">
          <h3 id="system-error-title">{{ feedbackState.alertTitle }}</h3>
          <p id="system-error-message">{{ feedbackState.alertMessage }}</p>
        </div>
        <div class="system-confirm-actions">
          <span class="diagnostic-copy-status" role="status">{{ copyStatus }}</span>
          <button class="ghost-button" type="button" @click="copyDiagnostic(feedbackState.alertMessage)">复制错误详情</button>
          <button class="primary-button" type="button" @click="dismissAlert">
            知道了
          </button>
        </div>
      </section>
    </div>

    <button v-if="feedbackState.errors.length && !feedbackState.alertOpen && !feedbackState.confirmOpen && !historyOpen" class="error-history-toggle" type="button" @click="historyOpen = true">错误记录（{{ feedbackState.errors.length }}）</button>
    <div v-if="historyOpen" class="modal-backdrop system-confirm-backdrop" role="dialog" aria-modal="true" aria-labelledby="error-history-title" @click.self="historyOpen = false" @keydown.esc="historyOpen = false">
      <section class="system-confirm-card error-history-card">
        <h3 id="error-history-title">本页错误记录</h3>
        <p>保留最近20条，可复制中文原因、处理建议和场次信息。</p>
        <ol class="error-history-list">
          <li v-for="item in feedbackState.errors" :key="item.id">
            <strong>{{ item.title }}</strong><time>{{ item.occurredAt }}</time>
            <p>{{ item.message }}</p>
            <button class="ghost-button" type="button" @click="copyDiagnostic(item.message)">复制详情</button>
          </li>
        </ol>
        <span role="status">{{ copyStatus }}</span>
        <div class="system-confirm-actions">
          <button class="ghost-button" type="button" @click="clearErrorHistory">清空记录</button>
          <button class="primary-button" type="button" @click="historyOpen = false">关闭</button>
        </div>
      </section>
    </div>

    <div class="system-toast-stack" aria-live="polite">
      <article
        v-for="item in feedbackState.toasts"
        :key="item.id"
        class="system-toast"
        :class="'tone-' + item.tone"
      >
        <span class="system-toast-icon" aria-hidden="true">
          {{ item.tone === 'success' ? '✓' : item.tone === 'error' ? '!' : item.tone === 'warning' ? '!' : 'i' }}
        </span>
        <div>
          <strong>{{ item.title }}</strong>
          <p v-if="item.message">{{ item.message }}</p>
        </div>
        <button type="button" aria-label="关闭提示" @click="dismissToast(item.id)">×</button>
      </article>
    </div>

    <div
      v-if="feedbackState.confirmOpen"
      class="modal-backdrop system-confirm-backdrop"
      role="dialog"
      aria-modal="true"
      @click.self="resolveConfirm(false)"
    >
      <section class="system-confirm-card">
        <div class="system-confirm-icon" :class="{ danger: feedbackState.confirmOptions.danger }">
          {{ feedbackState.confirmOptions.danger ? '!' : '?' }}
        </div>
        <div class="system-confirm-copy">
          <h3>{{ feedbackState.confirmOptions.title }}</h3>
          <p>{{ feedbackState.confirmOptions.message }}</p>
        </div>
        <div class="system-confirm-actions">
          <button class="ghost-button" type="button" @click="resolveConfirm(false)">
            {{ feedbackState.confirmOptions.cancelText || '取消' }}
          </button>
          <button
            :class="feedbackState.confirmOptions.danger ? 'danger-button' : 'primary-button'"
            type="button"
            @click="resolveConfirm(true)"
          >
            {{ feedbackState.confirmOptions.confirmText || '确认' }}
          </button>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.system-alert-card, .error-history-card { max-height: 85dvh; overflow: auto; }
#system-error-message, .system-toast p, .error-history-list p { white-space: pre-wrap; overflow-wrap: anywhere; }
#system-error-message, .error-history-card p, .error-history-list strong { font-size: 16px; }
.diagnostic-copy-status { align-self: center; font-size: 16px; }
.system-alert-card, .error-history-card, .error-history-toggle { font-size: 16px; }
.system-alert-card button, .error-history-card button { font-size: 16px; }
.system-toast p, .system-toast strong { font-size: 16px; }
.error-history-toggle { position: fixed; left: 16px; bottom: 16px; z-index: 65; border: 1px solid #e6b4ba; border-radius: 12px; padding: 8px 14px; background: #fff6f7; color: #a73446; box-shadow: 0 4px 18px #1b25451a; }
.error-history-list { padding-left: 22px; text-align: left; }
.error-history-list li { padding: 12px 0; border-bottom: 1px solid #e8eaf1; }
.error-history-list time { display: block; color: #6f7890; font-size: 16px; margin-top: 4px; }
.error-history-list p { margin: 8px 0; }
@media (max-width: 520px) { .system-confirm-actions { flex-wrap: wrap; } }
</style>
