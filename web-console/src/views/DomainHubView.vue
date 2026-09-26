<script setup lang="ts">
import { computed } from 'vue'
import TodoBadge from '../components/TodoBadge.vue'
import { RouterLink } from 'vue-router'
import { moduleEntries, moduleUiMap, type HubKey } from '../moduleUi'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'

const props = defineProps<{ hub: HubKey }>()

const config = computed(() => moduleUiMap[props.hub])

function hasStaffPermission(code: string) {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

function canShowEntry(to?: string) {
  if (to === '/operations/live/customer-handoffs') return hasStaffPermission('liveops.configure') || hasStaffPermission('liveops.ticket.manage')
  if (to === '/sales/handovers') return hasStaffPermission('sales.assignment.manage')
  if (!to) return true
  if (props.hub === 'staff' && to.startsWith('/staff/permissions')) {
    return hasStaffPermission('staff.role.manage')
  }
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
  if (props.hub === 'live' && to.startsWith('/operations/live/room-quotas')) {
    return hasStaffPermission('liveops.room_quota.view')
  }
  if (props.hub === 'live' && to.startsWith('/commercial/memberships')) {
    return hasStaffPermission('commercial.membership.view')
  }
  if (props.hub === 'live' && to.startsWith('/commercial/time-cards')) {
    return hasStaffPermission('commercial.time_card.view')
  }
  if (props.hub === 'live' && to.startsWith('/commercial/device-products')) {
    return hasStaffPermission('commercial.device.view')
  }
  if (props.hub === 'live' && to.startsWith('/operations/live/marketing')) {
    return (
      hasStaffPermission('commercial.marketing.view') ||
      hasStaffPermission('commercial.time_card.view') ||
      hasStaffPermission('commercial.device.view')
    )
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/marketing/channels')) {
    return hasStaffPermission('invitations.view_all')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/marketing')) {
    return hasStaffPermission('commercial.marketing.view')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/operations/live/marketing/ai-time')) {
    return hasStaffPermission('commercial.ai_time.view')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/memberships/plans')) {
    return hasStaffPermission('commercial.membership.view')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/memberships/simulator')) {
    return hasStaffPermission('commercial.membership.view')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/invitations')) {
    return hasStaffPermission('invitations.view_all')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/referrals')) {
    return hasStaffPermission('commercial.referral.view')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/time-cards')) {
    return hasStaffPermission('commercial.time_card.view')
  }
  if (props.hub === 'activityMarketing' && to.startsWith('/commercial/device-products')) {
    return hasStaffPermission('commercial.device.view')
  }
  if (props.hub === 'finance' && to.startsWith('/staff/finance')) {
    return hasStaffPermission('finance.dashboard.view')
  }
  if (props.hub === 'finance' && to.startsWith('/commercial/settlement')) {
    return hasStaffPermission('finance.settlement_rules.view')
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
const entryEnglishTitles: Record<string, string> = {
  '直播间': 'LIVE ROOMS',
  '直播间数量': 'ROOM QUOTAS',
  '直播策略': 'LIVE STRATEGY',
  '设备绑定': 'DEVICE BINDING',
  '活动营销': 'ACTIVITY MARKETING',
  '营销活动': 'MARKETING CAMPAIGNS',
  '营销设计': 'MARKETING DESIGN',
  '邀请与推荐': 'INVITATION & REFERRAL',
  '优惠工具': 'PROMOTION TOOLS',
  '奖励管理': 'REWARD MANAGEMENT',
  '渠道活动': 'CHANNEL CAMPAIGNS',
  '营销数据': 'MARKETING ANALYTICS',
  '会员与折扣': 'MEMBERSHIP PRICING',
  '时长卡运营': 'AI TIME CARD OPERATIONS',
  '设备商城运营': 'DEVICE SHOP OPERATIONS',
  '部门': 'DEPARTMENTS',
  '员工账号': 'EMPLOYEE ACCOUNTS',
  '角色权限': 'ROLES & PERMISSIONS',
  '权限中心': 'PERMISSION CENTER',
  '审批策略': 'APPROVAL POLICIES',
  '权限审计': 'ACCESS AUDIT',
  '终端列表': 'CUSTOMER LIST',
  '客户资源': 'CUSTOMER RESOURCES',
  '密码重置': 'PASSWORD RESET',
  '管理审计': 'MANAGEMENT AUDIT',
  '代理合作': 'AGENT PARTNERSHIP',
  '代理列表': 'AGENT LIST',
  '代理等级': 'AGENT LEVELS',
  '代理合同': 'AGENT CONTRACTS',
  '退出清算': 'EXIT SETTLEMENT',
  '销售团队': 'SALES TEAM',
  '业绩与提成': 'PERFORMANCE & COMMISSION',
  '会员方案': 'MEMBERSHIP PLANS',
  '规则模拟器': 'RULE SIMULATOR',
  '时长卡': 'AI TIME CARDS',
  '设备商品': 'DEVICE PRODUCTS',
  '一级推荐奖励': 'REFERRAL REWARDS',
  '结算规则': 'SETTLEMENT RULES',
  '终端账户': 'CUSTOMER ACCOUNTS',
  '客户收款确认': 'CUSTOMER RECEIPTS',
  '资金与权益审批': 'OPERATION APPROVALS',
  'AI 时长': 'AI TIME',
  '经营收支': 'OPERATING FINANCE',
  '钱包流水': 'WALLET LEDGER',
  '操作记录': 'OPERATION HISTORY',
  '全链路追溯': 'FULL TRACE',
  '收益结算': 'EARNINGS SETTLEMENT',
  '设备出入库': 'DEVICE INVENTORY',
  '物流管理': 'LOGISTICS',
}

function entryEnglishTitle(title: string) {
  return entryEnglishTitles[title] || 'FUNCTION'
}

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

      </div>

    </section>

    <section class="module-quick-menu">
      <div class="module-section-title">
        <div>
          <span class="section-kicker">FUNCTIONS</span>
          <h3>功能入口</h3>
        </div>

      </div>

      <div class="module-quick-grid">
        <component
          :is="entry.to ? RouterLink : 'article'"
          v-for="entry in quickEntries"
          :key="entry.title"
          :to="entry.to"
          class="module-quick-card"
          :class="{ 'finance-review-entry': props.hub === 'finance' && entry.scopeHint }"
        >
          <div class="module-quick-card-icon">{{ entry.icon }}</div>
          <TodoBadge :to="entry.to" />
          <div class="module-quick-card-copy">
            <div>
              <span class="module-quick-card-en">{{ entryEnglishTitle(entry.title) }}</span>
              <strong>{{ entry.title }}</strong>
              <span v-if="entry.scopeHint" class="module-entry-scope">{{ entry.scopeHint }}</span>
            </div>
          </div>
          <b>{{ entry.to ? '→' : '·' }}</b>
        </component>
      </div>
    </section>

  </div>
</template>

<style scoped>
.module-quick-card {position:relative}
.hub-finance .module-quick-card.finance-review-entry .module-quick-card-copy {min-width:0}
.hub-finance .module-quick-card.finance-review-entry .module-quick-card-copy strong {font-size:18px;line-height:1.4}
.hub-finance .module-quick-card.finance-review-entry .module-quick-card-en {font-size:14px;line-height:1.4;letter-spacing:.05em}
.hub-finance .module-quick-card.finance-review-entry .module-entry-scope {display:block;margin-top:5px;color:#526783;font-size:16px;line-height:1.45;font-weight:400;white-space:normal;overflow-wrap:anywhere}
</style>
