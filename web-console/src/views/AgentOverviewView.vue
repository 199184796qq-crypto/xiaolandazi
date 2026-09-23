<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import {
  getAgentCustomers,
  getCurrentResources,
  getInvitationDashboard,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import type {
  AdminCustomer,
  InvitationDashboard,
  ResourceDashboard,
} from '../types'

const loading = ref(false)
const error = useFeedbackErrorRef()
const customers = ref<AdminCustomer[]>([])
const resources = ref<ResourceDashboard | null>(null)
const invitations = ref<InvitationDashboard | null>(null)

const activeCustomers = computed(
  () => customers.value.filter((item) => item.status === 'active').length,
)

function resourceBalance(type: string) {
  const account = resources.value?.accounts.find(
    (item) => item.resource_type === type,
  )
  const balance = account?.balance || 0
  if (type === 'ai_seconds') {
    return new Intl.NumberFormat('zh-CN', {
      maximumFractionDigits: 2,
    }).format(balance / 3600)
  }
  return new Intl.NumberFormat('zh-CN').format(balance)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [customerData, resourceData, invitationData] = await Promise.all([
      getAgentCustomers(),
      getCurrentResources(),
      getInvitationDashboard(),
    ])
    customers.value = customerData.items
    resources.value = resourceData
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
    <section class="page-hero">
      <div>
        <p class="section-kicker">AGENT OVERVIEW</p>
        <h2>代理总览</h2>
        <p>
          查看当前代理的终端规模、资源余额和邀请发展情况。
        </p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="overview-metric-grid">
      <RouterLink class="overview-metric-card" to="/agent/customers">
        <span>终端总数</span>
        <strong>{{ customers.length }}</strong>
        <small>正常 {{ activeCustomers }}</small>
      </RouterLink>
      <RouterLink class="overview-metric-card" to="/resources">
        <span>AI 时长</span>
        <strong>{{ resourceBalance('ai_seconds') }}</strong>
        <small>小时</small>
      </RouterLink>
      <RouterLink class="overview-metric-card" to="/resources">
        <span>设备</span>
        <strong>{{ resourceBalance('device_slots') }}</strong>
        <small>台</small>
      </RouterLink>
      <RouterLink class="overview-metric-card" to="/invitations">
        <span>邀请注册</span>
        <strong>{{ invitations?.records.length || 0 }}</strong>
        <small>当前代理归属范围</small>
      </RouterLink>
      <RouterLink class="overview-metric-card" to="/account">
        <span>账户中心</span>
        <strong>→</strong>
        <small>资料与安全</small>
      </RouterLink>
    </section>

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
