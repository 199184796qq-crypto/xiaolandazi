<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import {
  finalizeAdminAgentExit,
  getAdminAgentExitCheck,
  getAdminAgentExitHistory,
  getAdminAgents,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type { AgentExitCheck, AgentExitRecord } from '../types'

interface AgentOption {
  organization_id: number
  name: string
  code: string
  status: string
  customer_count: number
}

const loading = ref(false)
const checking = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const agents = ref<AgentOption[]>([])
const selectedOrganizationId = ref<number | null>(null)
const check = ref<AgentExitCheck | null>(null)
const history = ref<AgentExitRecord[]>([])
const agentSearch = ref('')
const historyPage = ref(1)
const historyPageSize = 20

const selectedAgent = computed(() =>
  agents.value.find((item) => item.organization_id === selectedOrganizationId.value) || null,
)
const filteredAgents = computed(() => {
  const keyword = agentSearch.value.trim().toLowerCase()
  if (!keyword) return agents.value
  return agents.value.filter((item) =>
    [item.name, item.code, item.status, String(item.organization_id)]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const visibleAgents = computed(() => {
  const list = filteredAgents.value.slice(0, 50)
  const selected = selectedAgent.value
  if (
    selected &&
    !list.some((item) => item.organization_id === selected.organization_id)
  ) {
    return [selected, ...list.slice(0, 49)]
  }
  return list
})

const canFinalize = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes('agent.exit.finalize')),
  )
})

const historyPageCount = computed(() =>
  Math.max(1, Math.ceil(history.value.length / historyPageSize)),
)
const currentHistory = computed(() => {
  const page = Math.min(historyPage.value, historyPageCount.value)
  const start = (page - 1) * historyPageSize
  return history.value.slice(start, start + historyPageSize)
})

watch(selectedOrganizationId, async (value) => {
  check.value = null
  history.value = []
  historyPage.value = 1
  if (!value) return
  await Promise.all([loadCheck(), loadHistory()])
})

function money(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}
async function loadHistory() {
  if (!selectedOrganizationId.value) return
  const data = await getAdminAgentExitHistory(selectedOrganizationId.value)
  history.value = data.items
}

async function loadCheck() {
  if (!selectedOrganizationId.value) return
  checking.value = true
  error.value = ''
  try {
    check.value = await getAdminAgentExitCheck(selectedOrganizationId.value)
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取代理清算状态失败'
  } finally {
    checking.value = false
  }
}

async function finalizeExitRecord() {
  if (!selectedAgent.value || !check.value?.can_exit) return
  const customerCount = check.value.active_customer_count || 0
  const message =
    '确认完成代理“' +
    selectedAgent.value.name +
    '”退出？\n\n系统将停用代理组织、代理管理员和邀请码。' +
    (customerCount
      ? '\n该代理当前 ' + customerCount + ' 个在服终端将自动归还平台继续服务。'
      : '') +
    '\n此操作会写入永久退出清算记录。'
  if (!(await confirmAction({
    title: '完成代理退出并停用',
    message,
    confirmText: '确认完成退出',
    danger: true,
  }))) return

  saving.value = true
  error.value = ''
  try {
    await finalizeAdminAgentExit(selectedAgent.value.organization_id)
    const [agentData, historyData] = await Promise.all([
      getAdminAgents(),
      getAdminAgentExitHistory(selectedAgent.value.organization_id),
    ])
    agents.value = agentData.items
    history.value = historyData.items
    check.value = null
    const next = agents.value.find(
      (item) => item.organization_id === selectedOrganizationId.value,
    )
    if (next?.status === 'active') {
      await loadCheck()
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '完成代理退出失败'
    await loadCheck()
  } finally {
    saving.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const agentData = await getAdminAgents()
    agents.value = agentData.items
    if (!selectedOrganizationId.value && agents.value.length) {
      selectedOrganizationId.value = agents.value[0].organization_id
    } else if (selectedOrganizationId.value) {
      await Promise.all([loadCheck(), loadHistory()])
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取代理清算数据失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page agent-exit-page">
    <ModulePageNav context="agents" active-title="退出清算" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">AGENT EXIT CLEARING</p>
        <h2>退出清算</h2>
        <p>
          系统自动检查设备、售后、资源余额、未结收益和结算批次。通过后将停用代理组织与账号，并把仍在服务的终端安全归还平台。
        </p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading || checking" @click="loadCheck">
        {{ checking ? '检查中...' : '重新检查' }}
      </button>
    </section>

    <section class="settings-card agent-exit-selector">
      <label>
        <span>搜索代理</span>
        <input v-model="agentSearch" type="search" placeholder="代理名称 / 编码" />
      </label>
      <label>
        <span>选择代理（最多展示 50 个匹配项）</span>
        <select v-model.number="selectedOrganizationId">
          <option
            v-for="agent in visibleAgents"
            :key="agent.organization_id"
            :value="agent.organization_id"
          >
            {{ agent.name }}（{{ agent.code }}）
          </option>
        </select>
      </label>
      <div v-if="selectedAgent">
        <strong>{{ selectedAgent.name }}</strong>
        <span>{{ selectedAgent.status }} · {{ selectedAgent.customer_count }} 个终端</span>
      </div>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <div v-if="loading || checking" class="panel-loading">正在检查退出条件...</div>

    <template v-else-if="check">
      <section class="module-hub-metrics-v2">
        <article class="module-hub-metric-v2" :class="check.active_device_count ? 'tone-warning' : 'tone-success'">
          <span>未清设备</span>
          <strong>{{ check.active_device_count }}</strong>
          <small>代理名下非报废设备</small>
        </article>
        <article class="module-hub-metric-v2" :class="check.open_rma_count ? 'tone-warning' : 'tone-success'">
          <span>未结售后</span>
          <strong>{{ check.open_rma_count }}</strong>
          <small>处理中售后单</small>
        </article>
        <article class="module-hub-metric-v2" :class="check.unsettled_earning_count ? 'tone-warning' : 'tone-success'">
          <span>未结收益</span>
          <strong>{{ money(check.unsettled_earning_amount_cents) }}</strong>
          <small>{{ check.unsettled_earning_count }} 笔待处理</small>
        </article>
        <article class="module-hub-metric-v2" :class="check.open_settlement_batch_count ? 'tone-warning' : 'tone-success'">
          <span>未完结批次</span>
          <strong>{{ check.open_settlement_batch_count }}</strong>
          <small>待审核或待支付</small>
        </article>
        <article class="module-hub-metric-v2" :class="check.nonzero_resource_account_count ? 'tone-warning' : 'tone-success'">
          <span>未清资源账户</span>
          <strong>{{ check.nonzero_resource_account_count }}</strong>
          <small>余额或预留量需先清零</small>
        </article>
        <article class="module-hub-metric-v2 tone-neutral">
          <span>在服终端</span>
          <strong>{{ check.active_customer_count }}</strong>
          <small>退出完成时自动归还平台</small>
        </article>
      </section>

      <section
        class="agent-exit-result"
        :class="{ clear: check.can_exit, blocked: !check.can_exit }"
      >
        <div class="agent-exit-result-icon">{{ check.can_exit ? '✓' : '!' }}</div>
        <div>
          <strong>{{ check.can_exit ? '当前具备退出清算条件' : '当前不能完成退出清算' }}</strong>
          <p v-if="check.can_exit">
            自动检查项均已通过。完成退出后将立即停用代理组织、代理管理员和邀请码，并关闭当前代理服务关系；在服终端将归还平台继续服务。
          </p>
          <ul v-else>
            <li v-for="item in check.blockers" :key="item">{{ item }}</li>
          </ul>
        </div>
        <button
          v-if="check.can_exit && canFinalize"
          class="primary-button"
          type="button"
          :disabled="saving"
          @click="finalizeExitRecord"
        >
          {{ saving ? '处理中...' : '完成退出并停用代理' }}
        </button>
      </section>

      <section class="settings-card agent-exit-history">
        <header>
          <div>
            <strong>历史清算记录</strong>
            <span>{{ currentHistory.length }} 条</span>
          </div>
        </header>
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr><th>记录</th><th>状态</th><th>完成时间</th><th>经办人</th><th>设备</th><th>售后</th><th>未结收益</th><th>结算批次</th><th>回收终端</th></tr>
            </thead>
            <tbody>
              <tr v-for="item in currentHistory" :key="item.id">
                <td><strong>{{ item.agent_name }}退出清算</strong><small>{{ item.record_key }}</small></td>
                <td><span class="status-pill status-success">已完成</span></td>
                <td>{{ new Date(item.finalized_at).toLocaleString('zh-CN') }}</td>
                <td>{{ item.finalized_by_name || (item.finalized_by_user_id ? '用户 #' + item.finalized_by_user_id : '—') }}</td>
                <td>{{ item.active_device_count }}</td>
                <td>{{ item.open_rma_count }}</td>
                <td>{{ money(item.unsettled_earning_amount_cents) }}</td>
                <td>{{ item.open_settlement_batch_count }}</td>
                <td>{{ item.transferred_customer_count }}</td>
              </tr>
            </tbody>
          </table>
          <div v-if="currentHistory.length === 0" class="empty-state">当前代理暂无退出清算完成记录。</div>
        </div>
        <PaginationBar
          :page="Math.min(historyPage, historyPageCount)"
          :total-pages="historyPageCount"
          :total="history.length"
          :page-size="historyPageSize"
          @update:page="historyPage = $event"
        />
      </section>
    </template>
  </div>
</template>
