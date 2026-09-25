<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { onMounted, ref } from 'vue'

import { getAgentCustomers, getInvitationDashboard } from '../api'

import ModulePageNav from '../components/ModulePageNav.vue'
import type { AdminCustomer, InvitationDashboard } from '../types'


const loading = ref(false)
const error = useFeedbackErrorRef()
const customers = ref<AdminCustomer[]>([])
const invitations = ref<InvitationDashboard | null>(null)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [customerData, invitationData] = await Promise.all([
      getAgentCustomers(),
      getInvitationDashboard(),
    ])
    customers.value = customerData.items
    invitations.value = invitationData
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取代理总览失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page overview-page">
    <ModulePageNav context="workspace-agent" active-title="代理总览" />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">AGENT OVERVIEW</p>
        <h2>代理总览</h2>
        <p>
          管理负责客户和邀请业务，查看具体业务记录。
        </p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="overview-grid-two">
      <article class="settings-card overview-breakdown-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">RECENT CUSTOMERS</span>
            <h3>最近终端</h3>
          </div>
        </div>
        <div v-if="!customers.length" class="empty-state">
          暂无终端。
        </div>
        <div v-else class="overview-rank-list">
          <div
            v-for="item in customers.slice(0, 8)"
            :key="item.user_id"
            class="overview-rank-row"
          >
            <div>
              <strong>{{ item.display_name }}</strong>
              <span>@{{ item.username }} · {{ item.phone }}</span>
            </div>
            <span class="status-pill">{{ item.status }}</span>
          </div>
        </div>
      </article>

      <article class="settings-card overview-breakdown-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">INVITATION</span>
            <h3>我的邀请码</h3>
          </div>
        </div>
        <template v-if="invitations">
          <div class="overview-invite-code">
            <strong>{{ invitations.my_code.code }}</strong>
            <span>
              已使用 {{ invitations.my_code.used_count }} 次 ·
              {{ invitations.my_code.status === 'active' ? '可用' : '已停用' }}
            </span>
          </div>
          <RouterLink class="primary-button overview-link-button" to="/invitations">
            管理邀请码
          </RouterLink>
        </template>
      </article>
    </section>
  </div>
</template>
