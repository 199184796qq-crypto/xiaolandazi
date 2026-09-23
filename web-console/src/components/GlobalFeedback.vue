<script setup lang="ts">
import {
  dismissAlert,
  dismissToast,
  feedbackState,
  resolveConfirm,
} from '../uiFeedback'
</script>

<template>
  <Teleport to="body">
    <div
      v-if="feedbackState.alertOpen"
      class="modal-backdrop system-confirm-backdrop system-alert-backdrop"
      role="alertdialog"
      aria-modal="true"
      @click.self="dismissAlert"
    >
      <section class="system-confirm-card system-alert-card">
        <div class="system-confirm-icon danger">!</div>
        <div class="system-confirm-copy">
          <h3>{{ feedbackState.alertTitle }}</h3>
          <p>{{ feedbackState.alertMessage }}</p>
        </div>
        <div class="system-confirm-actions">
          <button class="primary-button" type="button" @click="dismissAlert">
            知道了
          </button>
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
