<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import {
  getInvitationDashboard,
  updateAdminInviteCodePolicy,
  updateOwnInviteCodeStatus,
} from '../api'
import { session } from '../session'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import type {
  InvitationDashboard,
  InvitationRecord,
  InviteCodeSummary,
} from '../types'

const dashboard = ref<InvitationDashboard | null>(null)
const loading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')
const copied = ref('')
const updatingOwn = ref(false)

const policyTarget = ref<InviteCodeSummary | null>(null)
const policyStatus = ref<'active' | 'disabled'>('active')
const policyMaxUses = ref<number | null>(0)
const policyExpiresAt = ref('')
const policySaving = ref(false)

const codePage = ref(1)
const recordPage = ref(1)
const codePageSize = 10
const recordPageSize = 10

const actor = computed(() => session.bootstrap?.actor)
const isAdmin = computed(() => actor.value?.role === 'platform_admin')
const isAgent = computed(() => actor.value?.role === 'agent_admin')
const isSales = computed(() => actor.value?.role === 'sales_staff')
const isInvitationManager = computed(() => {
  if (isAdmin.value) return true
  const access = session.bootstrap?.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('invitations.view_all')),
  )
})
const navContext = computed(() =>
  isInvitationManager.value ? 'activityMarketing' : 'workspace-auto',
)

const registrationUrl = computed(() => {
  const code = dashboard.value?.my_code.code || ''
  return window.location.origin + '/register?invite=' + encodeURIComponent(code)
})

const ownReferralCount = computed(() => dashboard.value?.own_referral_count || 0)

const codeTotalPages = computed(() =>
  Math.max(1, Math.ceil((dashboard.value?.codes_total || 0) / codePageSize)),
)
const recordTotalPages = computed(() =>
  Math.max(1, Math.ceil((dashboard.value?.records_total || 0) / recordPageSize)),
)

async function setCodePage(page: number) {
  codePage.value = Math.min(Math.max(page, 1), codeTotalPages.value)
  await load()
}

async function setRecordPage(page: number) {
  recordPage.value = Math.min(Math.max(page, 1), recordTotalPages.value)
  await load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    dashboard.value = await getInvitationDashboard({
      code_page: codePage.value,
      code_page_size: codePageSize,
      record_page: recordPage.value,
      record_page_size: recordPageSize,
    })
    codePage.value = Math.min(
      codePage.value,
      Math.max(1, Math.ceil(dashboard.value.codes_total / codePageSize)),
    )
    recordPage.value = Math.min(
      recordPage.value,
      Math.max(1, Math.ceil(dashboard.value.records_total / recordPageSize)),
    )
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取邀请与推荐数据失败'
  } finally {
    loading.value = false
  }
}

async function copyText(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value)
    copied.value = label
    window.setTimeout(() => {
      if (copied.value === label) copied.value = ''
    }, 1600)
  } catch {
    copied.value = ''
  }
}

async function toggleOwnCode() {
  if (!dashboard.value || updatingOwn.value) return
  updatingOwn.value = true
  error.value = ''
  notice.value = ''
  try {
    const next =
      dashboard.value.my_code.status === 'active' ? 'disabled' : 'active'
    await updateOwnInviteCodeStatus(next)
    notice.value = next === 'active' ? '我的邀请码已启用。' : '我的邀请码已停用。'
    await load()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '更新邀请码状态失败'
  } finally {
    updatingOwn.value = false
  }
}

function openPolicy(item: InviteCodeSummary) {
  policyTarget.value = item
  policyStatus.value = item.status === 'active' ? 'active' : 'disabled'
  policyMaxUses.value = item.max_uses || 0
  policyExpiresAt.value = item.expires_at
    ? new Date(item.expires_at).toISOString().slice(0, 16)
    : ''
  error.value = ''
  notice.value = ''
}

function closePolicy() {
  if (policySaving.value) return
  policyTarget.value = null
  policyExpiresAt.value = ''
  policyMaxUses.value = 0
}

async function savePolicy() {
  if (!policyTarget.value || policySaving.value) return
  policySaving.value = true
  error.value = ''
  notice.value = ''

  try {
    await updateAdminInviteCodePolicy(policyTarget.value.id, {
      status: policyStatus.value,
      max_uses: Math.max(0, Number(policyMaxUses.value || 0)),
      expires_at: policyExpiresAt.value
        ? new Date(policyExpiresAt.value).toISOString()
        : '',
    })
    notice.value = '邀请码策略已保存。'
    closePolicy()
    await load()
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '保存邀请码策略失败'
  } finally {
    policySaving.value = false
  }
}

function roleLabel(role: string) {
  if (role === 'platform_admin') return '系统'
  if (role === 'agent_admin') return '代理'
  if (role === 'sales_staff') return '销售'
  if (role === 'customer') return '终端'
  return role
}

function sourceLabel(source: string) {
  if (source === 'platform_invite') return '平台邀请'
  if (source === 'sales_invite') return '销售邀请'
  if (source === 'agent_invite') return '代理邀请'
  if (source === 'referral') return '终端推荐'
  return source
}

function formatDate(value?: string) {
  if (!value) return '不限'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function recordScopeText(item: InvitationRecord) {
  if (item.source_type === 'referral') {
    return '推荐人：' + item.inviter_display_name
  }
  return '归属：' + item.parent_org_name
}

const visibleScopeTitle = computed(() => {
  if (isAdmin.value) return '全平台注册记录'
  if (isAgent.value) return '当前代理归属注册'
  if (isSales.value) return '我的销售邀请注册'
  return '我的推荐记录'
})

onMounted(load)
</script>

<template>
  <div class="management-page invitation-page">
    <ModulePageNav :context="navContext" active-title="邀请与推荐" />
    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">INVITATION & REFERRAL</p>
        <h2>邀请与推荐</h2>
      </div>
      <button
        class="ghost-button"
        type="button"
        :disabled="loading"
        @click="load"
      >
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="settings-success">{{ notice }}</p>

    <template v-if="dashboard">
      <section class="invitation-summary-grid">
        <article class="settings-card invite-code-card">
          <div class="settings-card-header">
            <div>
              <span class="section-kicker">MY INVITE CODE</span>
              <h3>我的邀请码</h3>
            </div>
            <span
              class="status-pill"
              :class="{ inactive: dashboard.my_code.status !== 'active' }"
            >
              {{ dashboard.my_code.status === 'active' ? '可用' : '已停用' }}
            </span>
          </div>

          <div class="invite-code-display">
            <strong>{{ dashboard.my_code.code }}</strong>
            <span>已注册 {{ dashboard.my_code.used_count }} 个账号</span>
          </div>

          <div class="invite-policy-summary">
            <span>
              使用上限：
              {{
                dashboard.my_code.max_uses
                  ? dashboard.my_code.max_uses + ' 次'
                  : '不限'
              }}
            </span>
            <span>
              有效期：
              {{ formatDate(dashboard.my_code.expires_at) }}
            </span>
          </div>

          <div class="invite-copy-actions invite-copy-actions-wrap">
            <button
              class="ghost-button"
              type="button"
              @click="copyText(dashboard.my_code.code, 'code')"
            >
              {{ copied === 'code' ? '已复制' : '复制邀请码' }}
            </button>
            <button
              class="primary-button"
              type="button"
              :disabled="dashboard.my_code.status !== 'active'"
              @click="copyText(registrationUrl, 'link')"
            >
              {{ copied === 'link' ? '已复制链接' : '复制邀请链接' }}
            </button>
            <button
              class="ghost-button"
              type="button"
              :disabled="updatingOwn"
              @click="toggleOwnCode"
            >
              {{
                updatingOwn
                  ? '处理中...'
                  : dashboard.my_code.status === 'active'
                    ? '停用我的邀请码'
                    : '启用我的邀请码'
              }}
            </button>
          </div>

          <p class="invite-card-note">
            注册入口：{{ registrationUrl }}
          </p>
        </article>

        <article class="settings-card invitation-stat-card">
          <span class="section-kicker">ATTRIBUTION</span>
          <h3>我的邀请成果</h3>
          <div class="invitation-stat-value">
            {{ ownReferralCount }}
          </div>
          <p>由当前账号邀请码直接带来的注册数量。</p>
          <div class="invitation-rule-note">
            终端推荐只绑定推荐关系，不改变其平台/代理归属。
          </div>
        </article>

        <article class="settings-card invitation-stat-card">
          <span class="section-kicker">VISIBLE RECORDS</span>
          <h3>{{ visibleScopeTitle }}</h3>
          <div class="invitation-stat-value">
            {{ dashboard.records_total }}
          </div>
          <p>当前权限范围内可查看的邀请注册关系。</p>
        </article>
      </section>

      <section v-if="isAdmin" class="settings-card invite-admin-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">INVITE CODE CONTROL</span>
            <h3>全系统邀请码</h3>
          </div>
          <span>{{ dashboard.codes_total }} 个账号邀请码</span>
        </div>

        <div class="invite-code-table-wrap">
          <table class="invite-code-table">
            <thead>
              <tr>
                <th>账号</th>
                <th>角色</th>
                <th>邀请码</th>
                <th>使用情况</th>
                <th>有效期</th>
                <th>状态</th>
                <th>策略</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in dashboard.codes" :key="item.id">
                <td>
                  <strong>{{ item.owner_name }}</strong>
                  <span>@{{ item.owner_username }}</span>
                </td>
                <td>{{ roleLabel(item.owner_role) }}</td>
                <td class="invite-code-mono">{{ item.code }}</td>
                <td>
                  {{ item.used_count }}
                  /
                  {{ item.max_uses || '不限' }}
                </td>
                <td>{{ formatDate(item.expires_at) }}</td>
                <td>
                  <span
                    class="status-pill"
                    :class="{ inactive: item.status !== 'active' }"
                  >
                    {{ item.status === 'active' ? '可用' : '停用' }}
                  </span>
                </td>
                <td>
                  <button
                    class="text-action"
                    type="button"
                    @click="openPolicy(item)"
                  >
                    配置
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <PaginationBar
          :page="codePage"
          :total-pages="codeTotalPages"
          :total="dashboard.codes_total"
          :page-size="codePageSize"
          @update:page="setCodePage"
        />
      </section>

      <section class="settings-card invitation-record-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">REGISTRATION ATTRIBUTION</span>
            <h3>邀请注册与推荐记录</h3>
          </div>
          <span>{{ dashboard.records_total }} 条</span>
        </div>

        <div v-if="dashboard.records_total === 0" class="empty-state">
          暂无邀请注册记录。
        </div>

        <div v-else class="invitation-record-list">
          <article
            v-for="item in dashboard.records"
            :key="item.id"
            class="invitation-record-row"
          >
            <div class="invitation-record-person">
              <span class="invitation-avatar">
                {{ item.referred_display_name.slice(0, 1) }}
              </span>
              <div>
                <strong>{{ item.referred_display_name }}</strong>
                <span>@{{ item.referred_username }}</span>
              </div>
            </div>

            <div>
              <span class="muted-label">来源</span>
              <strong>{{ sourceLabel(item.source_type) }}</strong>
            </div>

            <div>
              <span class="muted-label">邀请人</span>
              <strong>{{ item.inviter_display_name }}</strong>
              <span>@{{ item.inviter_username }}</span>
            </div>

            <div>
              <span class="muted-label">归属关系</span>
              <strong>{{ recordScopeText(item) }}</strong>
              <span>{{ item.parent_org_name }}</span>
            </div>

            <div>
              <span class="muted-label">注册时间</span>
              <strong>{{ formatDate(item.bound_at) }}</strong>
            </div>
          </article>
        </div>
        <PaginationBar
          :page="recordPage"
          :total-pages="recordTotalPages"
          :total="dashboard.records_total"
          :page-size="recordPageSize"
          @update:page="setRecordPage"
        />
      </section>
    </template>

    <div
      v-if="policyTarget"
      class="modal-backdrop"
      @click.self="closePolicy"
    >
      <form class="modal-card invite-policy-modal" @submit.prevent="savePolicy">
        <div class="modal-header">
          <div>
            <p class="section-kicker">INVITE POLICY</p>
            <h3>配置邀请码</h3>
          </div>
          <button
            class="icon-button"
            type="button"
            @click="closePolicy"
          >
            ×
          </button>
        </div>

        <div class="credential-card invite-policy-target">
          <div>
            <span>账号</span>
            <strong>{{ policyTarget.owner_name }}</strong>
          </div>
          <div>
            <span>邀请码</span>
            <strong>{{ policyTarget.code }}</strong>
          </div>
        </div>

        <div class="form-grid">
          <label>
            <span>状态</span>
            <select v-model="policyStatus" class="text-input">
              <option value="active">启用</option>
              <option value="disabled">停用</option>
            </select>
          </label>

          <label>
            <span>最大使用次数</span>
            <input
              v-model.number="policyMaxUses"
              class="text-input"
              type="number"
              min="0"
              step="1"
              placeholder="0 表示不限"
            />
          </label>

          <label class="form-span-2">
            <span>过期时间</span>
            <input
              v-model="policyExpiresAt"
              class="text-input"
              type="datetime-local"
            />
          </label>
        </div>

        <div class="account-opening-note">
          <strong>策略说明</strong>
          <span>
            最大使用次数填 0 表示不限；过期时间留空表示长期有效。注册请求最终以服务端策略为准。
          </span>
        </div>

        <div class="modal-actions">
          <button class="ghost-button" type="button" @click="closePolicy">
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="policySaving">
            {{ policySaving ? '保存中...' : '保存策略' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>