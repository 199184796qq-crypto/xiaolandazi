<script setup lang="ts">
import TodoBadge from '../components/TodoBadge.vue'
import { computed, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { getSalesCustomers, getSalesFollowups } from '../api'

import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type { AdminCustomer, SalesFollowup } from '../types'

import { useFeedbackErrorRef } from '../uiFeedback'

const loading = ref(false)
const error = useFeedbackErrorRef()
const customers = ref<AdminCustomer[]>([])
const followups = ref<SalesFollowup[]>([])
const displayName = computed(
  () => session.bootstrap?.actor.display_name || session.bootstrap?.actor.username || '销售同事',
)
const recentCustomers = computed(() => customers.value.slice(0, 5))
const recentFollowups = computed(() => followups.value.slice(0, 5))

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function followupTypeLabel(value: string) {
  const labels: Record<string, string> = {
    phone: '电话',
    wechat: '微信',
    visit: '拜访',
    message: '消息',
    note: '记录',
  }
  return labels[value] || '记录'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [customerData, followupData] = await Promise.all([
      getSalesCustomers(),
      getSalesFollowups(),
    ])
    customers.value = customerData.items || []
    followups.value = followupData.items || []
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取销售工作台失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page sales-workspace-page">
    <ModulePageNav context="workspace-sales" active-title="销售工作台" />

    <section class="feature-workspace-hero sales-workspace-hero">
      <div>
        <p class="section-kicker">SALES WORKSPACE</p>
        <h2>{{ displayName }}，这是你的销售工作台</h2>
        <p>这里只处理你自己的客户、售前跟进、售后回访、方案和业绩。客户资金、奖励、退款和后台规则仍由对应部门负责。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新工作台' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="sales-action-grid">
      <RouterLink to="/sales/leads" class="sales-action-card">
        <TodoBadge to="/sales/leads" />
        <span class="sales-action-icon">意</span>
        <div><strong>意向顾客</strong><small>录入、拜访、成交转客户</small></div>
        <b>→</b>
      </RouterLink>
      <RouterLink to="/sales/customers" class="sales-action-card">
        <span class="sales-action-icon">客</span>
        <div><strong>我的客户</strong><small>查看本人负责的终端客户</small></div>
        <b>→</b>
      </RouterLink>
      <RouterLink to="/sales/followups" class="sales-action-card">
        <TodoBadge to="/sales/followups" />
        <span class="sales-action-icon">访</span>
        <div><strong>跟进与回访</strong><small>缴费前跟进，财务确认后记录售后回访</small></div>
        <b>→</b>
      </RouterLink>
      <RouterLink to="/sales/catalog" class="sales-action-card">
        <span class="sales-action-icon">价</span>
        <div><strong>产品与报价</strong><small>只读取后台已经发布的销售方案</small></div>
        <b>→</b>
      </RouterLink>
      <RouterLink to="/sales/my-performance" class="sales-action-card">
        <span class="sales-action-icon">绩</span>
        <div><strong>我的业绩</strong><small>查看本人订单、销售额和提成</small></div>
        <b>→</b>
      </RouterLink>
    </section>

    <section class="sales-workspace-columns">
      <article class="settings-card sales-workspace-panel">
        <header class="settings-card-header">
          <div><span class="section-kicker">CUSTOMERS</span><h3>最近负责客户</h3></div>
          <RouterLink to="/sales/customers" class="text-action">全部客户</RouterLink>
        </header>

        <div v-if="!recentCustomers.length" class="empty-state">
          当前还没有分配给你的客户。客户由销售管理人员在客资销售后台分配。
        </div>
        <div v-else class="sales-compact-list">
          <div v-for="item in recentCustomers" :key="item.user_id" class="sales-compact-row">
            <span class="sales-compact-avatar">{{ item.display_name.slice(0, 1) }}</span>
            <div>
              <strong>{{ item.display_name }}</strong>
              <small>{{ item.phone || '未留手机号' }} · {{ item.industry_name || '未设置行业' }}</small>
            </div>
            <span class="status-pill">{{ item.status }}</span>
          </div>
        </div>
      </article>

      <article class="settings-card sales-workspace-panel">
        <header class="settings-card-header">
          <div><span class="section-kicker">FOLLOW UPS</span><h3>最近联系记录</h3></div>
          <RouterLink to="/sales/followups" class="text-action">跟进与回访</RouterLink>
        </header>

        <div v-if="!recentFollowups.length" class="empty-state">
          暂无联系记录。缴费前记录跟进，财务确认后的沟通记录为售后回访。
        </div>
        <div v-else class="sales-compact-list">
          <div v-for="item in recentFollowups" :key="item.id" class="sales-followup-preview">
            <div>
              <strong>{{ item.customer_name }}</strong>
              <span>{{ followupTypeLabel(item.followup_type) }}</span>
            </div>
            <p>{{ item.content }}</p>
            <small>
              {{ formatDate(item.created_at) }}
              <template v-if="item.next_followup_at"> · 下次联系 {{ formatDate(item.next_followup_at) }}</template>
            </small>
          </div>
        </div>
      </article>
    </section>
  </div>
</template>

<style scoped>
.sales-workspace-hero h2 {
  margin-bottom: 8px;
}

.sales-action-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
  margin: 18px 0;
}

.sales-action-card {
  position: relative;
  display: grid;
  grid-template-columns: 46px 1fr auto;
  align-items: center;
  gap: 12px;
  padding: 18px;
  border: 1px solid rgba(110, 132, 177, 0.16);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.86);
  box-shadow: 0 12px 30px rgba(50, 69, 112, 0.06);
  transition: 160ms ease;
}

.sales-action-card:hover {
  transform: translateY(-2px);
  border-color: rgba(78, 105, 220, 0.38);
  box-shadow: 0 15px 35px rgba(61, 83, 160, 0.12);
}

.sales-action-icon {
  display: grid;
  place-items: center;
  width: 46px;
  height: 46px;
  border-radius: 15px;
  background: linear-gradient(145deg, #eef3ff, #e3eaff);
  color: #4e65cc;
  font-size: 18px;
  font-weight: 800;
}

.sales-action-card strong,
.sales-action-card small {
  display: block;
}

.sales-action-card small {
  margin-top: 5px;
  color: #8791a6;
  line-height: 1.45;
}

.sales-action-card b {
  color: #6978ad;
  font-size: 18px;
}

.sales-workspace-columns {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.sales-workspace-panel {
  min-width: 0;
}

.sales-compact-list {
  display: grid;
  gap: 9px;
}

.sales-compact-row {
  display: grid;
  grid-template-columns: 40px 1fr auto;
  align-items: center;
  gap: 11px;
  padding: 12px;
  border-radius: 14px;
  background: #f8faff;
}

.sales-compact-avatar {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  border-radius: 13px;
  background: #e8eeff;
  color: #5168cf;
  font-weight: 800;
}

.sales-compact-row strong,
.sales-compact-row small {
  display: block;
}

.sales-compact-row small {
  margin-top: 4px;
  color: #8a94a8;
}

.sales-followup-preview {
  padding: 13px 14px;
  border-radius: 14px;
  background: #f8faff;
}

.sales-followup-preview > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.sales-followup-preview span {
  color: #6070b1;
  font-size: 12px;
  font-weight: 700;
}

.sales-followup-preview p {
  margin: 8px 0 5px;
  color: #4e586d;
  line-height: 1.55;
}

.sales-followup-preview small {
  color: #929bad;
}

@media (max-width: 1080px) {
  .sales-action-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .sales-workspace-columns {
    grid-template-columns: 1fr;
  }
}
</style>
