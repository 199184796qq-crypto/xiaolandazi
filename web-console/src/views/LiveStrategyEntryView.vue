<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import LivePolicyAdminView from './LivePolicyAdminView.vue'
import LiveStrategyView from './LiveStrategyView.vue'
import LiveSupportView from './LiveSupportView.vue'

const isCustomer = computed(() => session.bootstrap?.actor.role === 'customer')
const storedMode = window.localStorage.getItem('system-agent-live-strategy-internal-mode')
const internalMode = ref<'policy' | 'support'>(storedMode === 'support' ? 'support' : 'policy')

watch(
  internalMode,
  (mode) => {
    window.localStorage.setItem('system-agent-live-strategy-internal-mode', mode)
    window.dispatchEvent(
      new CustomEvent('system-agent-live-strategy-mode', { detail: { mode } }),
    )
  },
  { immediate: true },
)
</script>

<template>
  <LiveStrategyView v-if="isCustomer" />
  <div v-else class="live-strategy-entry-page">
    <ModulePageNav context="live" active-title="直播策略" active-nav-title="直播运维" />
    <div class="live-strategy-entry-tabs">
      <button
        type="button"
        :class="{ active: internalMode === 'policy' }"
        @click="internalMode = 'policy'"
      >
        系统 / 行业规则
      </button>
      <button
        type="button"
        :class="{ active: internalMode === 'support' }"
        @click="internalMode = 'support'"
      >
        客户授权协助
      </button>
    </div>
    <LivePolicyAdminView v-if="internalMode === 'policy'" embedded />
    <LiveSupportView v-else embedded />
  </div>
</template>
