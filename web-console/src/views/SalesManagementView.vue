<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { getAdminSalesStaff } from '../api'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type { SalesStaffSummary } from '../types'

const items = ref<SalesStaffSummary[]>([])
const loading = ref(false)
const error = useFeedbackErrorRef()

const canOpenStaff = computed(() => {
  const access = session.bootstrap?.staff_access
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes('staff.employee.view')),
  )
})

const canManageStaff = computed(() => {
  const access = session.bootstrap?.staff_access
  if (!access) return false
  return Boolean(
    access.is_super_admin ||
      access.permissions.includes('staff.employee.create') ||
      access.permissions.includes('staff.employee.disable') ||
      access.permissions.includes('staff.employee.role_assign'),
  )
})

function statusLabel(value: string) {
  if (value === 'active') return '正常'
  if (value === 'disabled') return '已停用'
  return value || '—'
}

const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('name-asc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = computed(() => [
  { label: '全部状态', value: 'all' },
  ...Array.from(new Set(items.value.map((item) => item.status).filter(Boolean))).map((value) => ({
    label: value,
    value,
  })),
])

const sortOptions = [
  { label: '姓名 A-Z', value: 'name-asc' },
  { label: '姓名 Z-A', value: 'name-desc' },
  { label: '负责终端从高到低', value: 'customers-desc' },
  { label: '负责终端从低到高', value: 'customers-asc' },
]

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [item.display_name, item.employee_code, item.username, item.phone, item.email, item.team_name]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchesStatus = statusFilter.value === 'all' || item.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'name-desc') return b.display_name.localeCompare(a.display_name, 'zh-CN')
    if (sortMode.value === 'customers-desc') return b.customer_count - a.customer_count
    if (sortMode.value === 'customers-asc') return a.customer_count - b.customer_count
    return a.display_name.localeCompare(b.display_name, 'zh-CN')
  })
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)))
const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

async function load() {
  loading.value = true
  error.value = ''
  try {
    items.value = (await getAdminSalesStaff()).items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取销售列表失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page">
    <ModulePageNav hub="sales" active-title="销售团队" />
    <section class="page-hero">
      <div>
        <p class="section-kicker">SALES BUSINESS</p>
        <h2>销售管理</h2>
        <p>
          这里查看内部销售与直营终端关系。销售员工的新增、停用、部门负责人和角色权限统一在“组织架构 → 销售部”维护。
        </p>
      </div>
      <div class="page-hero-actions">
        <button
          class="ghost-button"
          type="button"
          :disabled="loading"
          @click="load"
        >
          {{ loading ? '刷新中...' : '刷新数据' }}
        </button>
        <RouterLink
          v-if="canOpenStaff"
          class="primary-button"
          to="/staff/employees?group=sales"
        >
          {{ canManageStaff ? '管理销售部员工' : '查看销售部员工' }}
        </RouterLink>
      </div>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="settings-card">
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">SALES STAFF</span>
          <h3>销售业务账号</h3>
        </div>
        <span>{{ loading ? '加载中...' : items.length + ' 个销售账号' }}</span>
      </div>

      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="姓名 / 员工编号 / 账号 / 手机号"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="viewMode === 'card'" class="agent-list">
        <article v-for="item in pagedItems" :key="item.staff_id" class="agent-row">
          <div class="agent-main">
            <strong>{{ item.display_name }}</strong>
            <span>{{ item.employee_code }} · {{ item.username }}</span>
          </div>

          <div>
            <span class="muted-label">联系方式</span>
            <strong>{{ item.phone }}</strong>
            <span v-if="item.email">{{ item.email }}</span>
          </div>

          <div>
            <span class="muted-label">负责终端</span>
            <strong>{{ item.customer_count }}</strong>
            <span>{{ item.team_name || '销售部' }}</span>
          </div>

          <div>
            <span class="status-pill">{{ statusLabel(item.status) }}</span>
          </div>
        </article>

        <div v-if="!loading && filteredItems.length === 0" class="empty-state">
          没有符合当前搜索或筛选条件的销售员工。
        </div>
      </div>

      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>销售员工</th>
              <th>账号</th>
              <th>联系方式</th>
              <th>负责终端</th>
              <th>团队</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.staff_id">
              <td><strong>{{ item.display_name }}</strong><small>{{ item.employee_code }}</small></td>
              <td>@{{ item.username }}</td>
              <td><span>{{ item.phone }}</span><small>{{ item.email || '—' }}</small></td>
              <td>{{ item.customer_count }}</td>
              <td>{{ item.team_name || '销售部' }}</td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
            </tr>
          </tbody>
        </table>
        <div v-if="!loading && filteredItems.length === 0" class="empty-state">没有符合当前搜索或筛选条件的销售员工。</div>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>

    <section class="settings-card sales-staff-link-card">
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">STAFF SYSTEM</span>
          <h3>销售员工统一进入内部员工体系</h3>
        </div>
      </div>
      <div class="account-opening-note sales-staff-note">
        <strong>统一规则</strong>
        <span>
          销售员工属于“销售部”，可分配销售人员或销售主管角色；登录密码由系统生成，首次登录强制修改。终端归属仍保留现有销售关系，不受员工体系迁移影响。
        </span>
      </div>
    </section>
  </div>
</template>