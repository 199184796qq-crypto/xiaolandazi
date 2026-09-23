<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import {
  getAdminAgents,
  getAdminCustomers,
  getAdminSalesStaff,
  getCommercialMemberships,
  getInventoryDevices,
  getRooms,
  getStaffDashboard,
  getStaffFinanceOverview,
} from '../api'
import { moduleEntries, moduleUiMap, type HubKey } from '../moduleUi'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'

interface Metric {
  label: string
  value: string | number
  hint: string
  tone: 'primary' | 'success' | 'warning' | 'neutral'
}

const props = defineProps<{ hub: HubKey }>()
const loading = ref(false)
const error = useFeedbackErrorRef()
const metrics = ref<Metric[]>([])

const config = computed(() => moduleUiMap[props.hub])

function hasStaffPermission(code: string) {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

function canShowEntry(to?: string) {
  if (!to) return true
  if (props.hub === 'staff' && to.startsWith('/staff/approvals')) {
    return hasStaffPermission('finance.dashboard.view')
  }
  if (props.hub === 'staff' && to.startsWith('/staff/audit')) {
    return hasStaffPermission('audit.view')
  }
  if (props.hub === 'customers' && to.includes('focus=security')) {
    return Boolean(
      session.bootstrap?.actor.role === 'platform_admin' || session.bootstrap?.staff_access?.is_super_admin,
    )
  }
  if (props.hub === 'customers' && to.includes('focus=audit')) {
    return hasStaffPermission('audit.view')
  }
  if (props.hub === 'resources' && to.includes('focus=adjust')) {
    return hasStaffPermission('resources.adjust') || hasStaffPermission('finance.resource.adjust')
  }
  return true
}
const quickEntries = computed(() => {
  const entries = moduleEntries(props.hub)
  if (
    props.hub === 'resources' &&
    session.bootstrap?.actor.role === 'agent_admin'
  ) {
    return entries.filter((entry) =>
      Boolean(entry.to && entry.to.startsWith('/resources/workspace')),
    )
  }
  return entries.filter((entry) => canShowEntry(entry.to))
})
const readyCount = computed(() => quickEntries.value.filter((item) => item.to).length)
function metric(label: string, value: string | number, hint: string, tone: Metric['tone'] = 'neutral'): Metric {
  return { label, value, hint, tone }
}

async function loadMetrics() {
  loading.value = true
  error.value = ''

  try {
    if (props.hub === 'staff') {
      const data = await getStaffDashboard()
      metrics.value = [
        metric('部门', data.groups.length, '当前可见部门', 'primary'),
        metric('员工', data.employees.length, '在册内部员工', 'success'),
        metric('角色', data.roles.length, '已配置角色模板', 'neutral'),
        metric('审批策略', data.approval_policies.length, '资金与高风险业务规则', 'warning'),
      ]
      return
    }

    if (props.hub === 'customers') {
      const items = (await getAdminCustomers()).items
      metrics.value = [
        metric('终端总数', items.length, '当前系统终端主档', 'primary'),
        metric('代理终端', items.filter((item) => item.parent_org_type === 'agent').length, '归属代理组织', 'success'),
        metric('推荐注册', items.filter((item) => item.source_type === 'referral').length, '通过推荐关系注册', 'warning'),
        metric('直营终端', items.filter((item) => item.parent_org_type !== 'agent').length, '总部直营或自然注册', 'neutral'),
      ]
      return
    }

    if (props.hub === 'agents') {
      const items = (await getAdminAgents()).items
      metrics.value = [
        metric('代理总数', items.length, '当前代理组织', 'primary'),
        metric('正常合作', items.filter((item) => item.status === 'active').length, '当前可正常开展业务', 'success'),
        metric('其他状态', items.filter((item) => item.status !== 'active').length, '待审、停用或其他状态', 'warning'),
        metric('渠道模型', '分级', '等级和结算规则可独立扩展', 'neutral'),
      ]
      return
    }

    if (props.hub === 'sales') {
      const items = (await getAdminSalesStaff()).items
      metrics.value = [
        metric('销售账号', items.length, '当前销售业务账号', 'primary'),
        metric('员工体系', '统一', '销售纳入内部员工体系', 'success'),
        metric('终端范围', '隔离', '默认只看本人或团队终端', 'neutral'),
        metric('资金权限', '只读', '销售不能发奖励或改钱包', 'warning'),
      ]
      return
    }

    if (props.hub === 'commercial') {
      const items = (await getCommercialMemberships()).items
      metrics.value = [
        metric('会员方案', items.length, '当前稳定方案数量', 'primary'),
        metric('已发布', items.filter((item) => Boolean(item.active_version)).length, '当前正式生效版本', 'success'),
        metric('草稿', items.filter((item) => Boolean(item.draft_version)).length, '等待发布的新版本', 'warning'),
        metric('规则模型', '版本化', '历史订单不被新规则覆盖', 'neutral'),
      ]
      return
    }

    if (props.hub === 'finance') {
      const data = await getStaffFinanceOverview()
      metrics.value = [
        metric('终端账户', data.customers.length, '当前可管理资金账户', 'primary'),
        metric('待审核', data.tasks.filter((item) => item.status === 'pending').length, '需要有权人员处理', 'warning'),
        metric('财务任务', data.tasks.length, '当前操作记录', 'neutral'),
        metric('职责分离', '启用', '经办人不能审核本人单据', 'success'),
      ]
      return
    }

    if (props.hub === 'live') {
      const items = (await getRooms()).items
      metrics.value = [
        metric('直播间', items.length, '当前可见直播间', 'primary'),
        metric('直播中', items.filter((item) => item.status === 'live').length, '正在运行', 'success'),
        metric('连接中', items.filter((item) => item.status === 'connecting').length, '正在建立连接', 'neutral'),
        metric('异常', items.filter((item) => item.status === 'error').length, '需要尽快处理', 'warning'),
      ]
      return
    }

    if (props.hub === 'resources') {
      const items = (await getInventoryDevices()).items
      const lockedOrTransit = items.filter((item) =>
        [
          'RESERVED',
          'IN_TRANSIT',
          'RMA_TRANSIT',
          'REPAIR_TRANSIT',
          'REPAIR_RETURN_TRANSIT',
        ].includes(item.lifecycle_status),
      ).length
      const afterSalesOrScrap = items.filter((item) =>
        [
          'AFTER_SALES',
          'REPAIRING',
          'EXTERNAL_REPAIR',
          'SCRAP_PENDING',
          'SCRAPPED',
        ].includes(item.lifecycle_status),
      ).length
      metrics.value = [
        metric('设备总数', items.length, '所有 SN 档案', 'primary'),
        metric(
          '在库可用',
          items.filter((item) => item.lifecycle_status === 'IN_STOCK').length,
          'IN_STOCK',
          'success',
        ),
        metric('锁定 / 在途', lockedOrTransit, '锁定、运输与维修往返中的设备', 'neutral'),
        metric('售后 / 报废', afterSalesOrScrap, '售后、维修和报废设备', 'warning'),
      ]
      return
    }

    metrics.value = [
      metric('资源模型', '分账', '总部、代理、终端分别核算', 'primary'),
      metric('资源流水', '可追溯', '每次变化保留来源和去向', 'success'),
      metric('设备标识', 'SN', '按序列号追踪全生命周期', 'neutral'),
      metric('库存模型', '可重算', '库存余额可由流水重建', 'warning'),
    ]
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取总览数据失败'
  } finally {
    loading.value = false
  }
}

watch(() => props.hub, loadMetrics)
onMounted(loadMetrics)
</script>

<template>
  <div
    class="management-page module-hub-page module-hub-page-v2"
    :class="'hub-' + props.hub"
  >
    <ModulePageNav :context="props.hub" :active-title="config.title" variant="hub" />

    <section class="module-hub-hero-v2">
      <div class="module-hub-hero-icon">{{ config.heroIcon }}</div>

      <div class="module-hub-hero-copy">
        <p class="section-kicker">{{ config.kicker }}</p>
        <h2>{{ config.title }}</h2>
        <p>{{ config.description }}</p>

        <div class="module-hub-hero-tags">
          <span>{{ readyCount }} 个可用功能</span>
        </div>
      </div>

      <button class="ghost-button module-hub-refresh" type="button" :disabled="loading" @click="loadMetrics">
        {{ loading ? '刷新中...' : '刷新总览' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="module-hub-metrics-v2">
      <article
        v-for="item in metrics"
        :key="item.label"
        class="module-hub-metric-v2"
        :class="'tone-' + item.tone"
      >
        <div class="module-metric-dot"></div>
        <span>{{ item.label }}</span>
        <strong>{{ item.value }}</strong>
        <small>{{ item.hint }}</small>
      </article>
    </section>

    <section class="module-quick-menu">
      <div class="module-section-title">
        <div>
          <span class="section-kicker">FUNCTIONS</span>
          <h3>功能入口</h3>
        </div>
        <p>功能较多时在入口区域直接分组展示，不再重复第二套菜单。</p>
      </div>

      <div class="module-quick-grid">
        <component
          :is="entry.to ? RouterLink : 'article'"
          v-for="entry in quickEntries"
          :key="entry.title"
          :to="entry.to"
          class="module-quick-card"
        >
          <div class="module-quick-card-icon">{{ entry.icon }}</div>
          <div class="module-quick-card-copy">
            <div>
              <strong>{{ entry.title }}</strong>
              <span>{{ entry.badge || '进入功能' }}</span>
            </div>
            <p>{{ entry.description }}</p>
          </div>
          <b>{{ entry.to ? '→' : '·' }}</b>
        </component>
      </div>
    </section>

  </div>
</template>
