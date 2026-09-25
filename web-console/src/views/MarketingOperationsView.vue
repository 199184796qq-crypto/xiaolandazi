<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import {
  getCommercialMarketingCampaigns,
  getIncentivePrograms,
  getInvitationDashboard,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import { session } from '../session'
import type {
  IncentiveProgram,
  InvitationDashboard,
  MarketingCampaign,
} from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'

type MarketingOperationsMode = 'tools' | 'channels' | 'analytics'

const props = withDefaults(
  defineProps<{ mode?: MarketingOperationsMode }>(),
  { mode: 'tools' },
)

const loading = ref(false)
const error = useFeedbackErrorRef()
const warnings = ref<string[]>([])
const campaigns = ref<MarketingCampaign[]>([])
const invitations = ref<InvitationDashboard | null>(null)
const rewardPrograms = ref<IncentiveProgram[]>([])

const pageConfig = computed(() => {
  if (props.mode === 'channels') {
    return {
      title: '渠道活动',
      kicker: 'CHANNEL CAMPAIGNS',
      description: '按真实邀请关系查看终端老带新、销售邀请、代理邀请等渠道来源；奖励条件回到奖励管理配置，不在渠道页面写死金额和比例。',
    }
  }
  if (props.mode === 'analytics') {
    return {
      title: '营销数据',
      kicker: 'MARKETING ANALYTICS',
      description: '把营销活动、邀请推荐和奖励规则放到同一个数据视角，便于检查活动到推荐、成交和奖励的业务链路。',
    }
  }
  return {
    title: '优惠工具',
    kicker: 'PROMOTION TOOLS',
    description: '统一查看系统已经具备的折扣、赠送、组合和限时工具；具体数值仍保存在营销活动规则中，不把优惠参数写死在页面代码里。',
  }
})

const pageTitle = computed(() => pageConfig.value.title)

function hasStaffPermission(code: string) {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

function campaignRuntimeStatus(item: MarketingCampaign) {
  if (item.status === 'draft') return '草稿'
  if (item.status !== 'active') return '已停用'
  const now = Date.now()
  const startsAt = item.starts_at ? new Date(item.starts_at).getTime() : 0
  const endsAt = item.ends_at ? new Date(item.ends_at).getTime() : 0
  if (startsAt && startsAt > now) return '待开始'
  if (endsAt && endsAt <= now) return '已结束'
  return '进行中'
}

function toolTags(item: MarketingCampaign) {
  const tags: string[] = []
  if (item.items.some((rule) => rule.discount_bps > 0 && rule.discount_bps < 10000)) tags.push('折扣')
  if (item.items.some((rule) => rule.discount_bps === 0)) tags.push('赠送')
  if (item.items.some((rule) => (rule.package_months || 1) > 1 || (rule.quantity || 1) > 1)) tags.push('组合')
  if (item.starts_at || item.ends_at) tags.push('限时')
  return tags.length ? tags : ['标准定价']
}

const loadedChannelCounts = computed(() => {
  const counts: Record<string, number> = {}
  for (const item of invitations.value?.records || []) {
    counts[item.source_type] = (counts[item.source_type] || 0) + 1
  }
  return counts
})

const channelRows = computed(() => [
  {
    name: '终端老带新',
    source: 'referral',
    count: loadedChannelCounts.value.referral || 0,
    note: '终端客户推荐新客户，关系进入推荐链路。',
    to: '/invitations',
  },
  {
    name: '销售邀请',
    source: 'sales_invite',
    count: loadedChannelCounts.value.sales_invite || 0,
    note: '通过销售邀请码注册，保留销售来源与归属。',
    to: '/invitations',
  },
  {
    name: '代理邀请',
    source: 'agent_invite',
    count: loadedChannelCounts.value.agent_invite || 0,
    note: '通过代理邀请码注册，保留代理来源与组织归属。',
    to: '/invitations',
  },
  {
    name: '平台邀请',
    source: 'platform_invite',
    count: loadedChannelCounts.value.platform_invite || 0,
    note: '由平台发起的邀请注册，用于直营或运营活动。',
    to: '/invitations',
  },
])

function formatDate(value?: string) {
  if (!value) return '长期'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

async function load() {
  loading.value = true
  error.value = ''
  warnings.value = []
  campaigns.value = []
  invitations.value = null
  rewardPrograms.value = []

  if (props.mode !== 'channels') {
    try {
      campaigns.value = (await getCommercialMarketingCampaigns()).items
    } catch (value) {
      error.value = value instanceof Error ? value.message : '读取营销活动失败'
    }
  }

  if (props.mode === 'channels' || props.mode === 'analytics') {
    if (props.mode === 'channels' && hasStaffPermission('invitations.view_all')) {
      try {
        invitations.value = await getInvitationDashboard({
          code_page: 1,
          code_page_size: 100,
          record_page: 1,
          record_page_size: 100,
        })
      } catch (value) {
        warnings.value.push(value instanceof Error ? value.message : '邀请数据暂时无法读取')
      }
    } else if (props.mode === 'channels') {
      warnings.value.push('当前账号没有全平台邀请关系查看权限，渠道数据仅显示可访问部分。')
    }

    if (props.mode === 'analytics' && hasStaffPermission('commercial.referral.view')) {
      try {
        rewardPrograms.value = (await getIncentivePrograms('referral')).items
      } catch (value) {
        warnings.value.push(value instanceof Error ? value.message : '奖励规则暂时无法读取')
      }
    }
  }

  loading.value = false
}

onMounted(() => {
  void load()
})

watch(
  () => props.mode,
  () => {
    void load()
  },
)
</script>

<template>
  <div class="management-page marketing-operations-page">
    <ModulePageNav
      context="activityMarketing"
      :active-title="pageTitle"
      :active-nav-title="pageTitle"
    />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">{{ pageConfig.kicker }}</p>
        <h2>{{ pageConfig.title }}</h2>
        <p>{{ pageConfig.description }}</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新数据' }}
      </button>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-for="item in warnings" :key="item" class="settings-help">{{ item }}</p>

    <section v-if="props.mode === 'tools'" class="settings-card feature-workspace-panel">
      <div class="module-section-title">
        <div>
          <span class="section-kicker">TOOL LIBRARY</span>
          <h3>已接入优惠能力</h3>
        </div>
        <RouterLink class="text-action" to="/commercial/marketing">进入营销活动配置 →</RouterLink>
      </div>

      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>营销活动</th>
              <th>优惠工具</th>
              <th>标的数</th>
              <th>运行状态</th>
              <th>活动时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in campaigns" :key="item.id">
              <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
              <td>{{ toolTags(item).join(' / ') }}</td>
              <td>{{ item.items.length }}</td>
              <td><span class="status-pill">{{ campaignRuntimeStatus(item) }}</span></td>
              <td>{{ formatDate(item.starts_at) }} ～ {{ formatDate(item.ends_at) }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="!loading && campaigns.length === 0" class="empty-state">暂无营销活动。</div>
      </div>
    </section>

    <section v-else-if="props.mode === 'channels'" class="settings-card feature-workspace-panel">
      <div class="module-section-title">
        <div>
          <span class="section-kicker">CHANNEL SOURCES</span>
          <h3>渠道来源</h3>
        </div>
        <div class="table-actions">
          <RouterLink class="text-action" to="/invitations">邀请与推荐 →</RouterLink>
          <RouterLink class="text-action" to="/commercial/referrals">奖励管理 →</RouterLink>
        </div>
      </div>

      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>渠道</th>
              <th>来源标识</th>
              <th>当前载入记录</th>
              <th>业务说明</th>
              <th>配置入口</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in channelRows" :key="item.source">
              <td><strong>{{ item.name }}</strong></td>
              <td>{{ item.source }}</td>
              <td>{{ item.count }}</td>
              <td>{{ item.note }}</td>
              <td><RouterLink class="text-action" :to="item.to">查看关系</RouterLink></td>
            </tr>
          </tbody>
        </table>
      </div>

      <p class="settings-help">
        门店帮卖、异业合作等后续渠道不单独写死提成比例；接入时先生成明确的渠道来源/邀请关系，再由奖励规则版本计算，订单继续保存关系快照。
      </p>
    </section>

    <template v-else>
      <section class="settings-card feature-workspace-panel">
        <div class="module-section-title">
          <div>
            <span class="section-kicker">CAMPAIGNS</span>
            <h3>活动运行概览</h3>
          </div>
          <RouterLink class="text-action" to="/commercial/marketing">管理营销活动 →</RouterLink>
        </div>
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>活动</th>
                <th>状态</th>
                <th>标的</th>
                <th>优惠能力</th>
                <th>开始</th>
                <th>结束</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in campaigns" :key="item.id">
                <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
                <td><span class="status-pill">{{ campaignRuntimeStatus(item) }}</span></td>
                <td>{{ item.items.length }}</td>
                <td>{{ toolTags(item).join(' / ') }}</td>
                <td>{{ formatDate(item.starts_at) }}</td>
                <td>{{ formatDate(item.ends_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="settings-card feature-workspace-panel">
        <div class="module-section-title">
          <div>
            <span class="section-kicker">REWARD RULES</span>
            <h3>奖励规则版本</h3>
          </div>
          <RouterLink class="text-action" to="/commercial/referrals">进入奖励管理 →</RouterLink>
        </div>
        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>规则</th>
                <th>已发布版本</th>
                <th>草稿版本</th>
                <th>冻结期</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in rewardPrograms" :key="item.id">
                <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
                <td>{{ item.active_version ? 'V' + item.active_version.version_no : '—' }}</td>
                <td>{{ item.draft_version ? 'V' + item.draft_version.version_no : '—' }}</td>
                <td>{{ (item.draft_version || item.active_version)?.pending_days || 0 }} 天</td>
              </tr>
            </tbody>
          </table>
          <div v-if="!loading && rewardPrograms.length === 0" class="empty-state">
            当前账号暂无可查看的推荐奖励规则。
          </div>
        </div>
      </section>
    </template>
  </div>
</template>
