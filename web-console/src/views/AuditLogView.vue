<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { getAdminAuditLogs } from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type { AdminAuditLog } from '../types'

const loading = ref(false)
const error = useFeedbackErrorRef()
const items = ref<AdminAuditLog[]>([])
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('time-desc')
const page = ref(1)
const pageSize = ref(24)
const viewMode = ref<'card' | 'table'>('table')

const statusOptions = [
  { label: '全部结果', value: 'all' },
  { label: '成功', value: 'success' },
  { label: '失败', value: 'failed' },
]

const sortOptions = [
  { label: '最新操作', value: 'time-desc' },
  { label: '最早操作', value: 'time-asc' },
  { label: '经办账号 A-Z', value: 'actor-asc' },
]

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [
        item.actor_username,
        item.action,
        item.target_username,
        item.path,
        item.client_ip,
        item.result,
      ].some((value) => String(value || '').toLowerCase().includes(keyword))

    const success = item.result.startsWith('http_2') || item.result === 'success'
    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'success' && success) ||
      (statusFilter.value === 'failed' && !success)

    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'time-asc') {
      return new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime()
    }
    if (sortMode.value === 'actor-asc') {
      return a.actor_username.localeCompare(b.actor_username, 'zh-CN')
    }
    return new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime()
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

function resultLabel(value: string) {
  if (value.startsWith('http_2') || value === 'success') return '成功'
  if (value === 'started') return '处理中'
  return '失败 / ' + value
}

function actionLabel(value: string) {
  const map: Record<string, string> = {
    'staff.group.create': '新增部门',
    'staff.group.update': '修改部门',
    'staff.role.create': '新增角色',
    'staff.role.update': '修改角色权限',
    'staff.employee.create': '新增员工',
    'staff.employee.disable': '停用员工',
    'staff.employee.role_update': '调整员工角色',
    'staff.approval_policy.update': '修改审批策略',
    'commercial.membership.create': '新增会员方案',
    'commercial.membership.draft_save': '保存会员草稿',
    'commercial.membership.publish': '发布会员方案',
    'finance.recharge.create': '发起充值',
    'finance.refund.create': '发起退款',
    'finance.reward.grant': '发放奖励',
    'agent.create': '新增代理',
  }
  return map[value] || value
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getAdminAuditLogs(500)
    items.value = data.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取审计日志失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page audit-log-page">
    <ModulePageNav context="staff" active-title="权限审计" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">PERMISSION AUDIT</p>
        <h2>权限审计</h2>
        <p>集中查看内部管理操作、高权限变更、目标账号、来源 IP 与执行结果。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新日志' }}
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="经办账号 / 操作 / 目标账号 / 路径 / IP"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取审计日志...</div>

      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>经办人</th>
              <th>操作</th>
              <th>目标</th>
              <th>来源 IP</th>
              <th>结果</th>
              <th>接口路径</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.id">
              <td>{{ new Date(item.occurred_at).toLocaleString('zh-CN') }}</td>
              <td><strong>{{ item.actor_username }}</strong><small>#{{ item.actor_user_id }}</small></td>
              <td>{{ actionLabel(item.action) }}</td>
              <td>
                <span>{{ item.target_username || '—' }}</span>
                <small v-if="item.target_user_id">用户 #{{ item.target_user_id }}</small>
              </td>
              <td>{{ item.client_ip || '—' }}</td>
              <td><span class="status-pill">{{ resultLabel(item.result) }}</span></td>
              <td><small>{{ item.http_method || '' }} {{ item.path || '—' }}</small></td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredItems.length === 0" class="empty-state">暂无符合条件的审计记录。</div>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>
  </div>
</template>
