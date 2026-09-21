<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  adminDeleteCustomer,
  adminResetCustomerPassword,
  getAdminAuditLogs,
  getAdminCustomers,
} from '../api'
import type { AdminAuditLog, AdminCustomer } from '../types'

const customers = ref<AdminCustomer[]>([])
const auditLogs = ref<AdminAuditLog[]>([])
const loading = ref(false)
const auditLoading = ref(false)
const error = ref('')
const notice = ref('')
const search = ref('')

const resetTarget = ref<AdminCustomer | null>(null)
const newPassword = ref('')
const confirmPassword = ref('')
const resetting = ref(false)
const resetError = ref('')

const deleteTarget = ref<AdminCustomer | null>(null)
const deleteConfirm = ref('')
const deleting = ref(false)
const deleteError = ref('')

const filteredCustomers = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return customers.value

  return customers.value.filter((customer) =>
    [
      customer.display_name,
      customer.username,
      String(customer.tenant_id),
      String(customer.user_id),
    ].some((value) => value.toLowerCase().includes(keyword)),
  )
})

const activeCount = computed(
  () => customers.value.filter((item) => item.status === 'active').length,
)

async function loadCustomers() {
  loading.value = true
  error.value = ''
  try {
    const response = await getAdminCustomers()
    customers.value = response.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取客户列表失败'
  } finally {
    loading.value = false
  }
}

async function loadAuditLogs() {
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
  newPassword.value = ''
  confirmPassword.value = ''
  resetError.value = ''
  notice.value = ''
}

function closeReset() {
  if (resetting.value) return
  resetTarget.value = null
  newPassword.value = ''
  confirmPassword.value = ''
  resetError.value = ''
}

async function submitReset() {
  if (!resetTarget.value || resetting.value) return

  resetError.value = ''
  if (newPassword.value.length < 8) {
    resetError.value = '新密码至少 8 位'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    resetError.value = '两次输入的新密码不一致'
    return
  }

  resetting.value = true
  const customer = resetTarget.value
  try {
    await adminResetCustomerPassword(customer.user_id, {
      new_password: newPassword.value,
      confirm_password: confirmPassword.value,
    })

    resetting.value = false
    closeReset()
    notice.value =
      '已为 ' + customer.display_name + ' 重置密码，并注销该客户的旧登录会话。'
    await loadAuditLogs()
  } catch (value) {
    resetError.value =
      value instanceof Error ? value.message : '重置客户密码失败'
  } finally {
    resetting.value = false
  }
}

function openDelete(customer: AdminCustomer) {
  deleteTarget.value = customer
  deleteConfirm.value = ''
  deleteError.value = ''
  notice.value = ''
}

function closeDelete() {
  if (deleting.value) return
  deleteTarget.value = null
  deleteConfirm.value = ''
  deleteError.value = ''
}

async function submitDelete() {
  if (!deleteTarget.value || deleting.value) return

  if (deleteConfirm.value.trim() !== deleteTarget.value.username) {
    deleteError.value = '请输入完整登录账号确认删除'
    return
  }

  deleting.value = true
  deleteError.value = ''
  const customer = deleteTarget.value

  try {
    await adminDeleteCustomer(customer.user_id)
    deleting.value = false
    closeDelete()
    notice.value =
      '客户 ' + customer.display_name + ' 已删除，关联直播间也已清理。'
    await loadAll()
  } catch (value) {
    deleteError.value =
      value instanceof Error ? value.message : '删除客户失败'
  } finally {
    deleting.value = false
  }
}

function formatDate(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', {
    hour12: false,
  })
}

function auditActionLabel(action: string) {
  const labels: Record<string, string> = {
    'admin.login': '管理员登录',
    'admin.logout': '管理员退出',
    'admin.change_password': '管理员修改密码',
    'customer.list_view': '查看客户列表',
    'customer.password_reset': '重置客户密码',
    'customer.delete': '删除客户',
    'audit.list_view': '查看审计日志',
    'room.create': '创建直播间',
    'room.delete': '删除直播间',
  }
  return labels[action] ?? action
}

function auditResultLabel(result: string) {
  if (result === 'started') return '执行中/结果未知'
  if (result.startsWith('http_2')) return '成功'
  if (result.startsWith('http_4')) return '已拒绝'
  if (result.startsWith('http_5')) return '失败'
  return result
}

onMounted(loadAll)
</script>

<template>
  <div class="customer-management-page">
    <section class="page-hero customer-admin-hero">
      <div>
        <p class="section-kicker">CUSTOMER MANAGEMENT</p>
        <h2>客户管理</h2>
        <p>管理客户账号、重置密码和客户数据范围。</p>
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

    <section class="customer-security-banner">
      <div class="security-shield">✓</div>
      <div>
        <strong>管理员永远无法查看客户密码</strong>
        <p>
          数据库只保存 bcrypt 密码哈希。管理员只能设置一个新的密码，
          系统不会返回原密码、密码哈希，也不会把新密码写入审计日志。
        </p>
      </div>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <div v-if="notice" class="settings-success">{{ notice }}</div>

    <section class="customer-admin-stats">
      <article>
        <span>客户账号</span>
        <strong>{{ customers.length }}</strong>
      </article>
      <article>
        <span>正常账号</span>
        <strong>{{ activeCount }}</strong>
      </article>
      <article>
        <span>审计记录</span>
        <strong>{{ auditLogs.length }}</strong>
      </article>
    </section>

    <section class="customer-admin-card">
      <div class="customer-admin-card-head">
        <div>
          <span class="section-kicker">ACCOUNTS</span>
          <h3>客户账号</h3>
        </div>

        <label class="customer-search">
          <span>⌕</span>
          <input
            v-model="search"
            type="search"
            placeholder="搜索客户名称、账号或租户 ID"
          />
        </label>
      </div>

      <div v-if="loading && !customers.length" class="customer-admin-loading">
        正在读取客户账号…
      </div>

      <div v-else-if="!filteredCustomers.length" class="customer-admin-empty">
        <strong>没有匹配的客户</strong>
        <span>客户注册后会自动出现在这里。</span>
      </div>

      <div v-else class="customer-table-wrap">
        <table class="customer-table">
          <thead>
            <tr>
              <th>客户</th>
              <th>登录账号</th>
              <th>租户 ID</th>
              <th>状态</th>
              <th>注册时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="customer in filteredCustomers" :key="customer.user_id">
              <td>
                <div class="customer-identity">
                  <span class="customer-avatar">
                    {{ customer.display_name.slice(0, 1) }}
                  </span>
                  <div>
                    <strong>{{ customer.display_name }}</strong>
                    <span>User #{{ customer.user_id }}</span>
                  </div>
                </div>
              </td>
              <td class="customer-account">{{ customer.username }}</td>
              <td>#{{ customer.tenant_id }}</td>
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
                  <button type="button" @click="openReset(customer)">
                    重置密码
                  </button>
                  <button
                    class="danger"
                    type="button"
                    @click="openDelete(customer)"
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="filteredCustomers.length" class="customer-mobile-list">
        <article
          v-for="customer in filteredCustomers"
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

          <dl>
            <div>
              <dt>用户 ID</dt>
              <dd>#{{ customer.user_id }}</dd>
            </div>
            <div>
              <dt>租户 ID</dt>
              <dd>#{{ customer.tenant_id }}</dd>
            </div>
            <div>
              <dt>注册时间</dt>
              <dd>{{ formatDate(customer.created_at) }}</dd>
            </div>
          </dl>

          <div class="customer-mobile-actions">
            <button type="button" @click="openReset(customer)">
              重置密码
            </button>
            <button
              class="danger"
              type="button"
              @click="openDelete(customer)"
            >
              删除客户
            </button>
          </div>
        </article>
      </div>
    </section>

    <section class="customer-admin-card audit-card">
      <div class="customer-admin-card-head">
        <div>
          <span class="section-kicker">REDIS AUDIT</span>
          <h3>管理员操作日志</h3>
        </div>
        <span class="audit-storage-badge">Redis Stream</span>
      </div>

      <div v-if="auditLoading && !auditLogs.length" class="customer-admin-loading">
        正在读取审计日志…
      </div>

      <div v-else-if="!auditLogs.length" class="customer-admin-empty">
        <strong>暂无管理员操作日志</strong>
      </div>

      <div v-else class="audit-list">
        <article v-for="item in auditLogs" :key="item.id" class="audit-row">
          <time>{{ formatDate(item.occurred_at) }}</time>
          <div class="audit-main">
            <div>
              <strong>{{ auditActionLabel(item.action) }}</strong>
              <span class="audit-result">{{ auditResultLabel(item.result) }}</span>
            </div>
            <p>
              管理员 {{ item.actor_username }}
              <template v-if="item.target_username">
                · 客户 {{ item.target_username }}
              </template>
              <template v-if="item.target_tenant_id">
                · Tenant #{{ item.target_tenant_id }}
              </template>
            </p>
          </div>
          <span class="audit-ip">{{ item.client_ip || '-' }}</span>
        </article>
      </div>
    </section>

    <div
      v-if="resetTarget"
      class="modal-backdrop"
      @click.self="closeReset"
    >
      <form class="modal-card customer-action-modal" @submit.prevent="submitReset">
        <div class="modal-header">
          <div>
            <span class="section-kicker">RESET PASSWORD</span>
            <h3>为客户重置密码</h3>
          </div>
          <button type="button" class="close-button" @click="closeReset">×</button>
        </div>

        <div class="customer-modal-target">
          <strong>{{ resetTarget.display_name }}</strong>
          <span>{{ resetTarget.username }}</span>
        </div>

        <p class="customer-modal-note">
          原密码无法查看。提交后只会保存新的 bcrypt 哈希，
          同时注销该客户当前所有登录会话。
        </p>

        <div class="form-stack">
          <label>
            <span>新密码</span>
            <input
              v-model="newPassword"
              type="password"
              autocomplete="new-password"
              maxlength="72"
              placeholder="8-72 位"
              required
            />
          </label>
          <label>
            <span>确认新密码</span>
            <input
              v-model="confirmPassword"
              type="password"
              autocomplete="new-password"
              maxlength="72"
              placeholder="再次输入新密码"
              required
            />
          </label>
        </div>

        <p v-if="resetError" class="auth-error">{{ resetError }}</p>

        <div class="modal-actions">
          <button type="button" class="ghost-button" @click="closeReset">
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="resetting">
            {{ resetting ? '正在重置...' : '确认重置密码' }}
          </button>
        </div>
      </form>
    </div>

    <div
      v-if="deleteTarget"
      class="modal-backdrop"
      @click.self="closeDelete"
    >
      <form class="modal-card customer-action-modal" @submit.prevent="submitDelete">
        <div class="modal-header">
          <div>
            <span class="section-kicker danger-kicker">DELETE CUSTOMER</span>
            <h3>删除客户</h3>
          </div>
          <button type="button" class="close-button" @click="closeDelete">×</button>
        </div>

        <div class="customer-delete-warning">
          <strong>这个操作不可撤销</strong>
          <p>
            将删除 {{ deleteTarget.display_name }} 的客户账号、登录会话，
            并清理该客户在 Core 中的全部直播间。
          </p>
        </div>

        <label class="delete-confirm-field">
          <span>
            输入登录账号
            <strong>{{ deleteTarget.username }}</strong>
            确认删除
          </span>
          <input
            v-model="deleteConfirm"
            type="text"
            autocomplete="off"
            :placeholder="deleteTarget.username"
            required
          />
        </label>

        <p v-if="deleteError" class="auth-error">{{ deleteError }}</p>

        <div class="modal-actions">
          <button type="button" class="ghost-button" @click="closeDelete">
            取消
          </button>
          <button
            class="danger-button"
            type="submit"
            :disabled="deleting || deleteConfirm.trim() !== deleteTarget.username"
          >
            {{ deleting ? '正在删除...' : '永久删除客户' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>