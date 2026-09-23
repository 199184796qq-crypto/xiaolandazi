<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import {
  getAdminAgents,
  getAdminCustomers,
  getAdminSalesStaff,
  getRooms,
  getStaffDashboard,
} from '../api'
import type {
  AdminCustomer,
  AgentSummary,
  Room,
  SalesStaffSummary,
  StaffDashboard,
} from '../types'

const loading = ref(false)
const error = useFeedbackErrorRef()
const agents = ref<AgentSummary[]>([])
const customers = ref<AdminCustomer[]>([])
const sales = ref<SalesStaffSummary[]>([])
const rooms = ref<Room[]>([])
const staff = ref<StaffDashboard | null>(null)

const activeAgents = computed(
  () => agents.value.filter((item) => item.status === 'active').length,
)
const activeCustomers = computed(
  () => customers.value.filter((item) => item.status === 'active').length,
)
const liveRooms = computed(
  () => rooms.value.filter((item) => item.status === 'live').length,
)
const directCustomers = computed(
  () =>
    customers.value.filter((item) => item.parent_org_type === 'platform')
      .length,
)
const agentCustomers = computed(
  () =>
    customers.value.filter((item) => item.parent_org_type === 'agent').length,
)
const salesCustomers = computed(
  () => customers.value.filter((item) => item.sales_staff_id > 0).length,
)
const referralCustomers = computed(
  () => customers.value.filter((item) => item.inviter_user_id > 0).length,
)

function hasPermission(code: string) {
  const access = staff.value?.access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes(code)),
  )
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [agentData, customerData, salesData, roomData, staffData] =
      await Promise.all([
        getAdminAgents(),
        getAdminCustomers(),
        getAdminSalesStaff(),
        getRooms(),
        getStaffDashboard(),
      ])

    agents.value = agentData.items
    customers.value = customerData.items
    sales.value = salesData.items
    rooms.value = roomData.items
    staff.value = staffData
  } catch (value) {
    error.value =
      value instanceof Error ? value.message : '读取系统总览失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page admin-control-page">
    <section class="page-hero admin-control-hero">
      <div>
        <p class="section-kicker">SYSTEM</p>
        <h2>系统</h2>
        <p>
          串联内部员工、渠道、终端、直播运维、商业方案和资源账户。超级系统管理员负责系统规则；管理部可以查看整体架构，但不能修改部门和权限定义。
        </p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="admin-kpi-grid">
      <RouterLink class="admin-kpi-card" to="/staff">
        <span>内部员工</span>
        <strong>{{ staff?.employees.length || 0 }}</strong>
        <small>{{ staff?.groups.length || 0 }} 个部门</small>
      </RouterLink>

      <RouterLink class="admin-kpi-card" to="/customers">
        <span>终端总数</span>
        <strong>{{ customers.length }}</strong>
        <small>正常 {{ activeCustomers }}</small>
      </RouterLink>

      <RouterLink class="admin-kpi-card" to="/agents">
        <span>代理</span>
        <strong>{{ agents.length }}</strong>
        <small>正常 {{ activeAgents }}</small>
      </RouterLink>

      <RouterLink class="admin-kpi-card" to="/">
        <span>直播间</span>
        <strong>{{ rooms.length }}</strong>
        <small>直播中 {{ liveRooms }}</small>
      </RouterLink>
    </section>

    <section class="settings-card admin-flow-card">
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">SYSTEM FLOW</span>
          <h3>系统业务链路</h3>
        </div>
        <span>企业组织与外部业务分层管理</span>
      </div>

      <div class="admin-business-flow">
        <RouterLink class="admin-flow-step" to="/staff">
          <span class="admin-flow-index">01</span>
          <strong>组织架构</strong>
          <small>部门 · 员工 · 角色 · 权限 · 数据范围</small>
          <em>{{ staff?.employees.length || 0 }} 名可见员工</em>
        </RouterLink>

        <span class="admin-flow-arrow">→</span>

        <RouterLink class="admin-flow-step" to="/sales">
          <span class="admin-flow-index">02</span>
          <strong>获客渠道</strong>
          <small>内部销售 · 外部代理 · 推荐关系</small>
          <em>{{ sales.length }} 销售 / {{ agents.length }} 代理</em>
        </RouterLink>

        <span class="admin-flow-arrow">→</span>

        <RouterLink class="admin-flow-step" to="/customers">
          <span class="admin-flow-index">03</span>
          <strong>终端归属</strong>
          <small>直营 · 销售 · 代理 · 终端推荐</small>
          <em>{{ customers.length }} 个终端</em>
        </RouterLink>

        <span class="admin-flow-arrow">→</span>

        <RouterLink class="admin-flow-step" to="/">
          <span class="admin-flow-index">04</span>
          <strong>直播运维</strong>
          <small>直播间 · 实时监控 · 后续设备</small>
          <em>{{ liveRooms }} 个直播中</em>
        </RouterLink>

        <span class="admin-flow-arrow">→</span>

        <RouterLink
          v-if="hasPermission('commercial.membership.view')"
          class="admin-flow-step"
          to="/commercial/memberships"
        >
          <span class="admin-flow-index">05</span>
          <strong>商业与资源</strong>
          <small>会员方案 · 时长 · 资源账户 · 调整流水</small>
          <em>进入商业管理</em>
        </RouterLink>
        <div v-else class="admin-flow-step admin-flow-step-static">
          <span class="admin-flow-index">05</span>
          <strong>商业与资源</strong>
          <small>当前角色仅查看系统架构，无商业管理权限</small>
          <em>权限由角色定义</em>
        </div>
      </div>
    </section>

    <section class="admin-domain-grid">
      <article class="settings-card admin-domain-card">
        <div class="admin-domain-heading">
          <span class="admin-domain-icon">♜</span>
          <div>
            <span class="section-kicker">INTERNAL STAFF</span>
            <h3>组织架构</h3>
          </div>
        </div>

        <div class="admin-domain-links">
          <RouterLink to="/staff">员工与部门</RouterLink>
        </div>

        <p class="admin-domain-note">
          销售、财务、管理及未来运营/客服/技术人员统一进入内部员工体系；代理和终端保持外部组织体系。
        </p>
      </article>

      <article class="settings-card admin-domain-card">
        <div class="admin-domain-heading">
          <span class="admin-domain-icon">◎</span>
          <div>
            <span class="section-kicker">OPERATIONS</span>
            <h3>业务运营</h3>
          </div>
        </div>

        <div class="admin-domain-links">
          <RouterLink to="/">直播间</RouterLink>
          <RouterLink to="/customers">终端管理</RouterLink>
          <RouterLink to="/agents">代理管理</RouterLink>
          <RouterLink to="/sales">销售管理</RouterLink>
        </div>
      </article>

      <article class="settings-card admin-domain-card">
        <div class="admin-domain-heading">
          <span class="admin-domain-icon">◆</span>
          <div>
            <span class="section-kicker">COMMERCIAL</span>
            <h3>商业管理</h3>
          </div>
        </div>

        <div class="admin-domain-links">
          <RouterLink
            v-if="hasPermission('commercial.membership.view')"
            to="/commercial/memberships"
          >
            会员方案
          </RouterLink>
          <RouterLink
            v-if="
              hasPermission('resources.view') ||
              hasPermission('finance.resource.adjust')
            "
            to="/resources"
          >
            资源中心
          </RouterLink>
        </div>

        <p class="admin-domain-note">
          财务角色的充值、退款、奖励及审批能力统一从角色权限和审批策略进入，不再给所有内部员工同一套管理权限。
        </p>
      </article>
    </section>

    <section class="admin-bottom-grid">
      <article class="settings-card admin-structure-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">CUSTOMER STRUCTURE</span>
            <h3>终端结构</h3>
          </div>
        </div>

        <div class="admin-structure-list">
          <div>
            <span>平台直营</span>
            <strong>{{ directCustomers }}</strong>
          </div>
          <div>
            <span>代理归属</span>
            <strong>{{ agentCustomers }}</strong>
          </div>
          <div>
            <span>销售绑定</span>
            <strong>{{ salesCustomers }}</strong>
          </div>
          <div>
            <span>推荐关系</span>
            <strong>{{ referralCustomers }}</strong>
          </div>
        </div>
      </article>

      <article class="settings-card admin-structure-card">
        <div class="settings-card-header">
          <div>
            <span class="section-kicker">STAFF STRUCTURE</span>
            <h3>组织架构</h3>
          </div>
          <RouterLink class="text-action" to="/staff">管理</RouterLink>
        </div>

        <div class="admin-structure-list">
          <div v-for="group in staff?.groups || []" :key="group.id">
            <span>{{ group.name }}</span>
            <strong>{{ group.member_count }}</strong>
          </div>
        </div>
      </article>
    </section>
  </div>
</template>