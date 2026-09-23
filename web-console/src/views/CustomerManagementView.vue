<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import CredentialResultModal from '../components/CredentialResultModal.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import { useRoute } from 'vue-router'
import { session } from '../session'
import {
  adminResetCustomerPassword,
  getAdminAuditLogs,
  getAdminCustomers,
} from '../api'
import type { AdminAuditLog, AdminCustomer, InitialCredential } from '../types'

const route = useRoute()
const customerFocus = computed(() => String(route.query.focus || ''))
const customerPageTitle = computed(() => {
  if (customerFocus.value === 'attribution') return '终端归属'
  if (customerFocus.value === 'security') return '终端密码重置'
  if (customerFocus.value === 'audit') return '终端管理审计'
  return '终端列表'
})
const customerPageDescription = computed(() => {
  if (customerFocus.value === 'attribution') return '只读查看终端来源、代理归属、销售归属与推荐关系。'
  if (customerFocus.value === 'security') return '仅授权账号管理员可为终端生成新的系统初始密码。'
  if (customerFocus.value === 'audit') return '查看终端管理操作、经办人、目标对象、执行结果与发生时间。审计记录不可修改或删除。'
  return '{{ customerPageDescription }}'
})

const customers = ref<AdminCustomer[]>([])
const auditLogs = ref<AdminAuditLog[]>([])
const loading = ref(false)
const auditLoading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')
const search = ref('')
const viewMode = ref<'card' | 'table'>('table')
const statusFilter = ref('all')
const sortMode = ref('created-desc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '正常', value: 'active' },
  { label: '其他状态', value: 'other' },
]

const sortOptions = [
  { label: '注册时间从新到旧', value: 'created-desc' },
  { label: '注册时间从旧到新', value: 'created-asc' },
  { label: '终端名称 A-Z', value: 'name-asc' },
  { label: '终端名称 Z-A', value: 'name-desc' },
]

const resetTarget = ref<AdminCustomer | null>(null)
const resetDeliveryMethod = ref<'copy' | 'email'>('copy')
const resetting = ref(false)
const resetError = ref('')
const resetCredentialOpen = ref(false)
const resetCredential = ref<InitialCredential | null>(null)
const resetCredentialName = ref('')
const resetCredentialUsername = ref('')

const staffAccess = computed(() => session.bootstrap?.staff_access ?? null)
const canViewAudit = computed(
  () =>
    staffAccess.value?.is_super_admin === true ||
    staffAccess.value?.permissions.includes('audit.view') === true,
)
const canResetCustomer = computed(
  () => staffAccess.value?.is_super_admin === true,
)
const showCustomerRecords = computed(
  () =>
    customerFocus.value !== 'audit' &&
    (customerFocus.value !== 'security' || canResetCustomer.value),
)

const filteredCustomers = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = customers.value.filter((customer) => {
    const matchesKeyword =
      !keyword ||
      [
        customer.display_name,
        customer.username,
        customer.phone,
        customer.parent_org_name,
        customer.inviter_display_name,
        customer.inviter_username,
        sourceLabel(customer.source_type),
        String(customer.tenant_id),
        String(customer.user_id),
      ].some((value) => String(value || '').toLowerCase().includes(keyword))

    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'active' && customer.status === 'active') ||
      (statusFilter.value === 'other' && customer.status !== 'active')

    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'name-asc') return a.display_name.localeCompare(b.display_name, 'zh-CN')
    if (sortMode.value === 'name-desc') return b.display_name.localeCompare(a.display_name, 'zh-CN')
    const timeA = new Date(a.created_at).getTime() || 0
    const timeB = new Date(b.created_at).getTime() || 0
    return sortMode.value === 'created-asc' ? timeA - timeB : timeB - timeA
  })
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredCustomers.value.length / pageSize.value)))
const pagedCustomers = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredCustomers.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

const activeCount = computed(
  () => customers.value.filter((item) => item.status === 'active').length,
)

const agentCustomerCount = computed(
  () =>
    customers.value.filter((item) => item.parent_org_type === 'agent').length,
)

const referralCount = computed(
  () => customers.value.filter((item) => item.source_type === 'referral').length,
)

async function loadCustomers() {
  loading.value = true
  error.value = ''
  try {
    const response = await getAdminCustomers()
    customers.value = response.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取终端列表失败'
  } finally {
    loading.value = false
  }
}

async function loadAuditLogs() {
  if (!canViewAudit.value) {
    auditLogs.value = []
    return
  }

  auditLoading.value = true
  try {
    const response = await getAdminAuditLogs(50)
    auditLogs.value = response.items
  } catch (value) {
    if (!error.value) {
      error.value =
        value instanceof Error ? value.message : '读取审计日志失败'
    }
  } finally {
    auditLoading.value = false
  }
}

async function loadAll() {
  await Promise.all([loadCustomers(), loadAuditLogs()])
}

function openReset(customer: AdminCustomer) {
  resetTarget.value = customer
  resetDeliveryMethod.value = customer.email ? 'email' : 'copy'
  resetError.value = ''
  notice.value = ''
}

function closeReset() {
  if (resetting.value) return
  resetTarget.value = null
  resetError.value = ''
}

function closeResetCredential() {
  resetCredentialOpen.value = false
  resetCredential.value = null
  resetCredentialName.value = ''
  resetCredentialUsername.value = ''
}

async function submitReset() {
  if (!resetTarget.value || resetting.value) return

  resetError.value = ''
  const customer = resetTarget.value

  if (resetDeliveryMethod.value === 'email' && !customer.email) {
    resetError.value = '该终端没有邮箱，请选择复制方式交付'
    return
  }

  resetting.value = true
  try {
    const result = await adminResetCustomerPassword(customer.user_id, {
      delivery_method: resetDeliveryMethod.value,
      email: customer.email || '',
    })

    resetCredentialName.value = result.customer.display_name
    resetCredentialUsername.value = result.customer.username
    resetCredential.value = result.credential

    resetTarget.value = null
    resetCredentialOpen.value = true
    notice.value =
      '已为 ' +
      customer.display_name +
      ' 生成新的系统初始密码。终端下次登录必须先修改密码。'
    await loadAuditLogs()
  } catch (value) {
    resetError.value =
      value instanceof Error ? value.message : '重置终端密码失败'
  } finally {
    resetting.value = false
  }
}
function sourceLabel(source: string) {
  if (source === 'agent') return '代理直接开户'
  if (source === 'agent_invite') return '代理邀请码'
  if (source === 'platform_invite') return '平台邀请码'
  if (source === 'referral') return '终端推荐'
  if (source === 'direct') return '历史直营'
  return source || '未知来源'
}

function parentLabel(customer: AdminCustomer) {
  if (customer.parent_org_type === 'agent') {
    return '代理 · ' + (customer.parent_org_name || '#' + customer.parent_org_id)
  }
  if (customer.parent_org_type === 'platform') {
    return '平台直营'
  }
  return customer.parent_org_name || '未识别'
}

function formatDate(value: string) {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function auditActionLabel(action: string) {
  const labels: Record<string, string> = {
    'admin.login': '管理员登录',
    'admin.logout': '管理员退出',
    'admin.change_password': '管理员修改密码',
    'customer.list_view': '查看终端列表',
    'customer.password_reset': '重置终端密码',
    'customer.delete': '删除终端',
    'audit.list_view': '查看审计日志',
    'room.create': '创建直播间',
    'room.delete': '删除直播间',
    'agent.create': '创建代理',
    'agent.customer.create': '代理开通终端',
    'invitation.status_update': '修改邀请码状态',
    'agent.resource_adjust': '调整代理资源',
    'customer.resource_adjust': '调整终端资源',
    'agent.customer.resource_allocate': '代理分配终端资源',
  }
  return labels[action] ?? action
}

function auditResultLabel(result: string) {
  if (result === 'started') return '执行中'
  if (result.startsWith('http_2')) return '成功'
  if (result.startsWith('http_4')) return '已拒绝'
  if (result.startsWith('http_5')) return '失败'
  return result
}

onMounted(loadAll)
</script>

<template>
  <div class="customer-management-page">
    <ModulePageNav hub="customers" :active-title="customerPageTitle" />
    <section class="page-hero customer-admin-hero">
      <div>
        <p class="section-kicker">CUSTOMER MANAGEMENT</p>
        <h2>{{ customerPageTitle }}</h2>
        <p>
          {{ customerPageDescription }}
        </p>
      </div>

      <button
        class="ghost-button"
        type="button"
        :disabled="loading || auditLoading"
        @click="loadAll"
      >
        {{ loading || auditLoading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <div v-if="notice" class="settings-success">{{ notice }}</div>

    <section v-if="showCustomerRecords" class="customer-admin-stats customer-admin-stats-wide">
      <article>
        <span>终端总数</span>
        <strong>{{ customers.length }}</strong>
      </article>
      <article>
        <span>正常账号</span>
        <strong>{{ activeCount }}</strong>
      </article>
      <article>
        <span>代理归属终端</span>
        <strong>{{ agentCustomerCount }}</strong>
      </article>
      <article>
        <span>终端推荐注册</span>
        <strong>{{ referralCount }}</strong>
      </article>
    </section>

    <section v-if="showCustomerRecords" class="customer-admin-card">
      <div class="customer-admin-card-head">
        <div>
          <span class="section-kicker">CUSTOMER ATTRIBUTION</span>
          <h3>终端账号与归属</h3>
        </div>

      </div>

      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="终端 / 电话 / 代理 / 推荐人 / 来源"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="loading && !customers.length" class="customer-admin-loading">
        正在读取终端账号...
      </div>

      <div v-else-if="!filteredCustomers.length" class="customer-admin-empty">
        <strong>没有匹配的终端</strong>
        <span>邀请码注册或代理开户后会自动出现在这里。</span>
      </div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="customer-table customer-attribution-table">
          <thead>
            <tr>
              <th>终端</th>
              <th>联系电话</th>
              <th>来源</th>
              <th>归属</th>
              <th>邀请 / 推荐人</th>
              <th>状态</th>
              <th>注册时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="customer in pagedCustomers" :key="customer.user_id">
              <td>
                <div class="customer-identity">
                  <span class="customer-avatar">
                    {{ customer.display_name.slice(0, 1) }}
                  </span>
                  <div>
                    <strong>{{ customer.display_name }}</strong>
                    <span>@{{ customer.username }} · #{{ customer.tenant_id }}</span>
                  </div>
                </div>
              </td>
              <td class="customer-account">
                {{ customer.phone || '未完善' }}
              </td>
              <td>
                <span class="source-badge">{{ sourceLabel(customer.source_type) }}</span>
              </td>
              <td>{{ parentLabel(customer) }}</td>
              <td>
                <template v-if="customer.inviter_display_name">
                  <strong>{{ customer.inviter_display_name }}</strong>
                  <span class="table-subtext">@{{ customer.inviter_username }}</span>
                </template>
                <span v-else>—</span>
              </td>
              <td>
                <span
                  class="customer-status"
                  :class="{ active: customer.status === 'active' }"
                >
                  {{ customer.status === 'active' ? '正常' : customer.status }}
                </span>
              </td>
              <td>{{ formatDate(customer.created_at) }}</td>
              <td>
                <div class="customer-actions">
                  <button v-if="canResetCustomer && customerFocus === 'security'" type="button" @click="openReset(customer)">
                    重置密码
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else class="customer-card-grid">
        <article
          v-for="customer in pagedCustomers"
          :key="'mobile-' + customer.user_id"
          class="customer-mobile-card"
        >
          <div class="customer-mobile-head">
            <div class="customer-identity">
              <span class="customer-avatar">
                {{ customer.display_name.slice(0, 1) }}
              </span>
              <div>
                <strong>{{ customer.display_name }}</strong>
                <span>{{ customer.username }}</span>
              </div>
            </div>
            <span
              class="customer-status"
              :class="{ active: customer.status === 'active' }"
            >
              {{ customer.status === 'active' ? '正常' : customer.status }}
            </span>
          </div>

          <dl class="customer-mobile-meta customer-source-meta">
            <div>
              <dt>联系电话</dt>
              <dd>{{ customer.phone || '未完善' }}</dd>
            </div>
            <div>
              <dt>来源</dt>
              <dd>{{ sourceLabel(customer.source_type) }}</dd>
            </div>
            <div>
              <dt>归属</dt>
              <dd>{{ parentLabel(customer) }}</dd>
            </div>
            <div>
              <dt>邀请 / 推荐人</dt>
              <dd>{{ customer.inviter_display_name || '—' }}</dd>
            </div>
          </dl>

          <button
            v-if="canResetCustomer"
            class="ghost-button customer-mobile-reset"
            type="button"
            @click="openReset(customer)"
          >
            重置密码
          </button>
        </article>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredCustomers.length"
        :page-size="pageSize"
      />
    </section>

        <section v-if="customerFocus === 'audit' && !canViewAudit" class="customer-admin-card">
      <div class="customer-admin-empty">
        <strong>无审计查看权限</strong>
        <span>当前角色不能查看终端管理审计记录。</span>
      </div>
    </section>

    <section v-if="customerFocus === 'security' && !canResetCustomer" class="customer-admin-card">
      <div class="customer-admin-empty">
        <strong>无密码重置权限</strong>
        <span>当前角色只能查看终端资料，不能重置终端密码。</span>
      </div>
    </section>
<section v-if="canViewAudit && customerFocus === 'audit'" class="customer-admin-card audit-card">
      <div class="customer-admin-card-head">
        <div>
          <span class="section-kicker">AUDIT LOG</span>
          <h3>近期管理操作</h3>
        </div>
        <span>{{ auditLogs.length }} 条</span>
      </div>

      <div v-if="auditLoading && !auditLogs.length" class="customer-admin-loading">
        正在读取审计日志...
      </div>

      <div v-else-if="!auditLogs.length" class="customer-admin-empty">
        <strong>暂无审计记录</strong>
      </div>

      <div v-else class="audit-list">
        <article v-for="item in auditLogs" :key="item.id" class="audit-row">
          <div>
            <strong>{{ auditActionLabel(item.action) }}</strong>
            <span>{{ item.actor_username }}</span>
          </div>
          <div>
            <span>目标</span>
            <strong>{{ item.target_username || '—' }}</strong>
          </div>
          <div>
            <span>结果</span>
            <strong>{{ auditResultLabel(item.result) }}</strong>
          </div>
          <time>{{ formatDate(item.occurred_at) }}</time>
        </article>
      </div>
    </section>

    <div
      v-if="resetTarget"
      class="modal-backdrop"
      @click.self="closeReset"
    >
      <form class="modal-card" @submit.prevent="submitReset">
        <div class="modal-header">
          <div>
            <p class="section-kicker">RESET PASSWORD</p>
            <h3>重置终端密码</h3>
          </div>
          <button class="icon-button" type="button" @click="closeReset">×</button>
        </div>

        <p class="modal-helper">
          系统将为 {{ resetTarget.display_name }}（{{ resetTarget.username }}）
          随机生成新的初始密码。终端下次登录后必须立即修改。
        </p>

        <div class="form-grid">
          <label class="form-span-2">
            <span>初始凭证交付方式</span>
            <select v-model="resetDeliveryMethod" class="text-input">
              <option value="copy">生成后复制登录信息</option>
              <option value="email" :disabled="!resetTarget.email">
                发送到终端邮箱
              </option>
            </select>
          </label>

          <div class="form-span-2 account-opening-note">
            <strong>终端邮箱</strong>
            <span>
              {{ resetTarget.email || '未填写邮箱，只能使用复制方式交付' }}
            </span>
          </div>
        </div>

        <p v-if="resetError" class="auth-error">{{ resetError }}</p>

        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="closeReset">
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="resetting">
            {{ resetting ? '生成中...' : '生成新初始密码' }}
          </button>
        </div>
      </form>
    </div>

    <CredentialResultModal
      :open="resetCredentialOpen"
      title="终端初始密码已重置"
      :display-name="resetCredentialName"
      :username="resetCredentialUsername"
      :credential="resetCredential"
      @close="closeResetCredential"
    />
  </div>
</template>