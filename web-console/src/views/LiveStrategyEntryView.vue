<script setup lang="ts">
import { computed } from 'vue'
import { session } from '../session'
import LivePolicyAdminView from './LivePolicyAdminView.vue'
import LiveStrategyView from './LiveStrategyView.vue'

const isCustomer = computed(() => session.bootstrap?.actor.role === 'customer')
</script>

<template>
  <LiveStrategyView v-if="isCustomer" />
  <LivePolicyAdminView v-else />
</template>
