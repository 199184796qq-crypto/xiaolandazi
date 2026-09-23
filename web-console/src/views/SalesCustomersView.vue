<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import { getSalesCustomers } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import type { AdminCustomer } from '../types'

const items = ref<AdminCustomer[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()
const search = ref('')

const filtered = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return items.value
  return items.value.filter((item) =>
    [
      item.display_name,
      item.username,
      item.phone,
      item.source_type,
      item.inviter_display_name,
    ].some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = (await getSalesCustomers()).items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取我的终端失败'
  } finally {
    loading.value = false
  }
}

function sourceLabel(source: string) {
  if (source === 'sales_invite') return '我的邀请码注册'
  if (source === 'platform_invite') return '平台邀请码'
  if (source === 'referral') return '终端推荐'
  if (source === 'direct') return '平台直营'
  return source || '未知来源'
}

function formatDate(value: string) {
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

onMounted(load)
</script>

<template>
  <div class="management-page">
    <ModulePageNav context="workspace-sales" active-title="我的终端" />
    <section class="page-hero">
      <div>
        <p class="section-kicker">MY SALES CUSTOMERS</p>
        <h2>我的终端</h2>
        <p>
          这里只显示当前分配给您的平台直营终端。该页面为只读，终端资料、密码、资源和奖励均由平台管理。
        </p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="settings-card">
      <div class="settings-card-header sales-customer-head">
        <div>
          <span class="section-kicker">READ ONLY</span>
          <h3>负责终端</h3>
        </div>
        <label class="customer-search">
          <span>⌕</span>
          <input
            v-model="search"
            type="search"
            placeholder="搜索终端、电话、来源"
          />
        </label>
      </div>

      <div v-if="!loading && !filtered.length" class="empty-state">
        当前没有分配给您的终端。
      </div>

      <div v-else class="sales-customer-list">
        <article
          v-for="item in filtered"
          :key="item.user_id"
          class="sales-customer-row"
        >
          <div class="sales-staff-main">
            <span class="sales-avatar">
              {{ item.display_name.slice(0, 1) }}
            </span>
            <div>
              <strong>{{ item.display_name }}</strong>
              <span>@{{ item.username }} · #{{ item.tenant_id }}</span>
            </div>
          </div>
          <div>
            <span class="muted-label">联系电话</span>
            <strong>{{ item.phone || '未完善' }}</strong>
          </div>
          <div>
            <span class="muted-label">终端来源</span>
            <strong>{{ sourceLabel(item.source_type) }}</strong>
            <span v-if="item.inviter_display_name">
              推荐人：{{ item.inviter_display_name }}
            </span>
          </div>
          <div>
            <span class="muted-label">注册时间</span>
            <strong>{{ formatDate(item.created_at) }}</strong>
          </div>
          <div>
            <span class="status-pill">{{ item.status }}</span>
          </div>
        </article>
      </div>
    </section>
  </div>
</template>