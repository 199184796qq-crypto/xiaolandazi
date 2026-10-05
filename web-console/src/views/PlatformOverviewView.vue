<script setup lang="ts">
import TodoBadge from '../components/TodoBadge.vue'
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, ref } from 'vue'
import { getStaffDashboard } from '../api'

import type { StaffDashboard } from '../types'

import { moduleEntries, type HubKey, type ModuleEntry } from '../moduleUi'

const loading = ref(false)
const error = useFeedbackErrorRef()
const staff = ref<StaffDashboard | null>(null)

function hasPermission(code: string) {
  const access = staff.value?.access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes(code)),
  )
}

const canDrillAcrossDepartments = computed(() =>
  Boolean(staff.value?.access?.is_super_admin),
)

function canShowSubfunction(hub: HubKey, entry: ModuleEntry) {
  const to = entry.to || ''
  if (!to) return false

  if (hub === 'live' && to.includes('/room-quotas')) {
    return hasPermission('liveops.room_quota.view')
  }
  if (hub === 'staff' && to.includes('/staff/audit')) {
    return hasPermission('audit.view')
  }
  if (hub === 'staff' && to.includes('/staff/approvals')) {
    return hasPermission('finance.dashboard.view')
  }
  if (hub === 'customers' && to.includes('focus=security')) {
    return hasPermission('customer.password_reset')
  }
  if (hub === 'customers' && to.includes('focus=audit')) {
    return hasPermission('audit.view')
  }
  if (hub === 'commercial') {
    return hasPermission('commercial.membership.view')
  }
  if (hub === 'activityMarketing' && to.includes('/commercial/marketing/channels')) {
    return hasPermission('invitations.view_all')
  }
  if (hub === 'activityMarketing' && to.includes('/commercial/marketing')) {
    return hasPermission('commercial.marketing.view')
  }
  if (
    hub === 'activityMarketing' &&
    (
      to.includes('/commercial/memberships/plans') ||
      to.includes('/commercial/memberships/simulator') ||
      to.includes('/commercial/referrals') ||
      to.includes('/commercial/settlement')
    )
  ) {
    return hasPermission('commercial.membership.view')
  }
  if (hub === 'activityMarketing' && to.includes('/invitations')) {
    return hasPermission('invitations.view_all')
  }
  if (hub === 'activityMarketing' && to.includes('/commercial/time-cards')) {
    return hasPermission('commercial.time_card.view')
  }
  if (hub === 'activityMarketing' && to.includes('/commercial/device-products')) {
    return hasPermission('commercial.device.view')
  }
  if (hub === 'resources' && to.includes('/inventory')) {
    return hasPermission('inventory.view') || hasPermission('resources.view')
  }
  if (hub === 'resources' && to.includes('/logistics')) {
    return hasPermission('logistics.view') || hasPermission('resources.view')
  }
  if (hub === 'finance' && to.includes('/ai-time')) {
    return hasPermission('finance.resource.view')
  }
  if (hub === 'finance' && to.includes('/operating')) {
    return hasPermission('finance.operating.view')
  }
  if (hub === 'finance') {
    return hasPermission('finance.dashboard.view')
  }
  return true
}

function collectSubfunctions(hubs: HubKey[]) {
  const seen = new Set<string>()
  const result: ModuleEntry[] = []
  for (const hub of hubs) {
    for (const entry of moduleEntries(hub)) {
      if (!entry.to || !canShowSubfunction(hub, entry)) continue
      if (seen.has(entry.to)) continue
      seen.add(entry.to)
      result.push(entry)
    }
  }
  return result
}

const overviewSubfunctions = computed(() => ({
  staff: collectSubfunctions(['staff']),
  live: collectSubfunctions(['live', 'activityMarketing']),
  customers: collectSubfunctions(['customers', 'sales']),
  agents: collectSubfunctions(['agents']),
  products: collectSubfunctions(['commercial', 'resources']),
  finance: collectSubfunctions(['finance']),
}))

async function load() {
  loading.value = true
  error.value = ''
  try {
    staff.value = await getStaffDashboard()
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

    <p v-if="error" class="auth-error">{{ error }}</p>

    <section class="settings-card admin-architecture-card">
      <div class="settings-card-header">
        <div>
          <span class="section-kicker">SYSTEM ARCHITECTURE</span>
          <h3>系统业务架构</h3>
        </div>
      </div>

      <div class="admin-architecture-grid">
        <article class="admin-architecture-domain">
          <div class="admin-architecture-domain-head">
            <span class="admin-domain-icon">系</span>
            <div>
              <span class="section-kicker">SYSTEM MANAGEMENT</span>
              <h3>系统管理</h3>
            </div>
          </div>
          <p>系统总览、组织、员工、角色、权限和审批规则。</p>
          <div class="admin-domain-links">
            <RouterLink to="/overview">系统总览</RouterLink>
            <RouterLink v-if="canDrillAcrossDepartments" to="/system/settings">系统设置</RouterLink>
            <RouterLink to="/staff">组织架构</RouterLink>
          </div>
          <div class="admin-domain-subfunctions">
            <span>二级功能</span>
            <div>
              <RouterLink
                v-for="entry in overviewSubfunctions.staff"
                :key="entry.to"
                :to="entry.to || '/staff'"
              >
                <b>{{ entry.icon }}</b>{{ entry.title }}<TodoBadge :to="entry.to"/>
              </RouterLink>
            </div>
          </div>
        </article>

        <article class="admin-architecture-domain">
          <div class="admin-architecture-domain-head">
            <span class="admin-domain-icon">播</span>
            <div>
              <span class="section-kicker">MARKETING OPERATIONS</span>
              <h3>营销运维</h3>
            </div>
          </div>
          <p>围绕直播间运行、策略、监控和现场设备开展日常业务。</p>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-links">
            <RouterLink to="/operations/live">直播运维</RouterLink>
            <RouterLink to="/operations/live/marketing">活动营销</RouterLink>
          </div>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-subfunctions">
            <span>二级功能</span>
            <div>
              <RouterLink
                v-for="entry in overviewSubfunctions.live"
                :key="entry.to"
                :to="entry.to || '/operations/live'"
              >
                <b>{{ entry.icon }}</b>{{ entry.title }}<TodoBadge :to="entry.to"/>
              </RouterLink>
            </div>
          </div>
          <p v-else>观察模式：只在系统总览查看运行概况，不进入营销运维工作台。</p>
        </article>

        <article class="admin-architecture-domain">
          <div class="admin-architecture-domain-head">
            <span class="admin-domain-icon">客</span>
            <div>
              <span class="section-kicker">CUSTOMER & SALES</span>
              <h3>客资销售</h3>
            </div>
          </div>
          <p>客户来源、销售归属、推荐关系和持续经营统一管理。</p>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-links">
            <RouterLink to="/customers">客户资源</RouterLink>
            <RouterLink to="/sales">销售体系</RouterLink>
            <RouterLink to="/invitations">邀请与推荐</RouterLink>
          </div>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-subfunctions">
            <span>二级功能</span>
            <div>
              <RouterLink
                v-for="entry in overviewSubfunctions.customers"
                :key="entry.to"
                :to="entry.to || '/customers'"
              >
                <b>{{ entry.icon }}</b>{{ entry.title }}<TodoBadge :to="entry.to"/>
              </RouterLink>
            </div>
          </div>
          <p v-else>观察模式：只看客资与销售概况，不进入客资销售工作台。</p>
        </article>

        <article class="admin-architecture-domain">
          <div class="admin-architecture-domain-head">
            <span class="admin-domain-icon">仓</span>
            <div>
              <span class="section-kicker">WAREHOUSE & AFTER-SALES</span>
              <h3>仓储与售后</h3>
            </div>
          </div>
          <p>实体设备、库存仓储、物流交付、维修退换和售后闭环。</p>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-links">
            <RouterLink
              v-if="
                hasPermission('resources.view') ||
                hasPermission('inventory.view') ||
                hasPermission('logistics.view')
              "
              to="/resources"
            >
              设备与仓储
            </RouterLink>
            <RouterLink
              v-if="hasPermission('inventory.after_sales.view')"
              to="/staff/after-sales"
            >
              物流与售后
            </RouterLink>
          </div>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-subfunctions">
            <span>二级功能</span>
            <div>
              <RouterLink
                v-for="entry in overviewSubfunctions.products"
                :key="entry.to"
                :to="entry.to || '/resources'"
              >
                <b>{{ entry.icon }}</b>{{ entry.title }}<TodoBadge :to="entry.to"/>
              </RouterLink>
              <RouterLink
                v-if="hasPermission('inventory.after_sales.view')"
                to="/staff/after-sales"
              >
                <b>修</b>售后维修
              </RouterLink>
            </div>
          </div>
          <p v-else>观察模式：只看仓储售后概况，不进入仓储售后工作台。</p>
          <em>按商品、设备 SN 和售后单据全链追溯</em>
        </article>

        <article class="admin-architecture-domain">
          <div class="admin-architecture-domain-head">
            <span class="admin-domain-icon">财</span>
            <div>
              <span class="section-kicker">FINANCE MANAGEMENT</span>
              <h3>财务管理</h3>
            </div>
          </div>
          <p>资金账户、审批、流水、收益结算和全链路财务追溯。</p>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-links">
            <RouterLink
              v-if="hasPermission('finance.dashboard.view')"
              to="/staff/finance"
            >
              财务与结算
            </RouterLink>
          </div>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-subfunctions">
            <span>二级功能</span>
            <div>
              <RouterLink
                v-for="entry in overviewSubfunctions.finance"
                :key="entry.to"
                :to="entry.to || '/staff/finance'"
              >
                <b>{{ entry.icon }}</b>{{ entry.title }}<TodoBadge :to="entry.to"/>
              </RouterLink>
            </div>
          </div>
          <p v-else>观察模式：只看财务概况，不进入财务工作台。</p>
          <em>资金可进、可退、可抵、可提、可核对</em>
        </article>

        <article class="admin-architecture-domain">
          <div class="admin-architecture-domain-head">
            <span class="admin-domain-icon">渠</span>
            <div>
              <span class="section-kicker">CHANNEL PARTNERS</span>
              <h3>渠道合作</h3>
            </div>
          </div>
          <p>管理代理准入、等级政策、合作合同以及退出清算。</p>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-links">
            <RouterLink to="/agents">代理合作</RouterLink>
          </div>
          <div v-if="canDrillAcrossDepartments" class="admin-domain-subfunctions">
            <span>二级功能</span>
            <div>
              <RouterLink
                v-for="entry in overviewSubfunctions.agents"
                :key="entry.to"
                :to="entry.to || '/agents'"
              >
                <b>{{ entry.icon }}</b>{{ entry.title }}<TodoBadge :to="entry.to"/>
              </RouterLink>
            </div>
          </div>
          <p v-else>观察模式：只看渠道合作概况，不进入代理业务页面。</p>
        </article>
      </div>
    </section>

  </div>
</template>
<style scoped>.admin-domain-subfunctions a{position:relative;padding-right:30px}</style>
