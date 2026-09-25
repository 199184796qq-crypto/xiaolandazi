<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import {
  floorToWholeYuanCents,
  formatWholeYuanMoney,
  marketingPayableCents,
  wholeYuanPerHour,
} from '../pricingRules'
import {
  createCommercialMembership,
  deleteCommercialMembership,
  getCommercialMemberships,
  publishCommercialMembership,
  saveCommercialMembershipDraft,
  setCommercialMembershipListing,
} from '../api'
import type {
  CommercialMembershipInput,
  CommercialMembershipPlan,
  CommercialMembershipVersion,
  MarketingCampaign,
  MarketingCampaignItem,
} from '../types'

type MembershipEditorMode = 'create' | 'edit'
type MembershipFocus = 'plans' | 'simulator'

const props = withDefaults(defineProps<{ focus?: MembershipFocus }>(), { focus: 'plans' })
const showPlans = computed(() => props.focus === 'plans')
const showSimulator = computed(() => props.focus === 'simulator')
const pageTitle = computed(() => props.focus === 'simulator' ? '会员规则模拟器' : '会员方案')
const navContext = computed(() => 'activityMarketing' as const)

interface MembershipFormState {
  code: string
  name: string
  description: string
  sort_order: number
  price_yuan: number
  included_hours: number
  discount_percent: number
  device_discount_percent: number
  allow_auto_renew: boolean
}

const plans = ref<CommercialMembershipPlan[]>([])
const loading = ref(true)
const error = useFeedbackErrorRef()
const editorError = ref('')
const notice = ref('')
const saving = ref(false)
const publishingId = ref<number | null>(null)
const listingId = ref<number | null>(null)
const deletingId = ref<number | null>(null)

const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('sort-asc')
const page = ref(1)
const pageSize = ref(12)

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '已上架', value: 'listed' },
  { label: '已下架', value: 'unlisted' },
  { label: '有草稿', value: 'draft' },
  { label: '已归档', value: 'archived' },
]

const sortOptions = [
  { label: '默认排序', value: 'sort-asc' },
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '价格从高到低', value: 'price-desc' },
  { label: '价格从低到高', value: 'price-asc' },
]

const filteredPlans = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = plans.value.filter((plan) => {
    const matchesKeyword =
      !keyword ||
      [plan.name, plan.code, plan.description]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'listed' && plan.status === 'active') ||
      (statusFilter.value === 'unlisted' && plan.status === 'inactive') ||
      (statusFilter.value === 'draft' && Boolean(plan.draft_version)) ||
      (statusFilter.value === 'archived' && plan.status === 'archived')
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    const priceA = preferredSimulationVersion(a)?.price_cents ?? 0
    const priceB = preferredSimulationVersion(b)?.price_cents ?? 0
    if (sortMode.value === 'name-asc') return a.name.localeCompare(b.name, 'zh-CN')
    if (sortMode.value === 'price-desc') return priceB - priceA
    if (sortMode.value === 'price-asc') return priceA - priceB
    return a.sort_order - b.sort_order
  })
})

const totalPages = computed(() => Math.max(1, Math.ceil(filteredPlans.value.length / pageSize.value)))
const pagedPlans = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredPlans.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => { page.value = 1 })
watch(totalPages, (value) => { if (page.value > value) page.value = value })
watch(
  () => simulation.target_plan_id,
  () => { simulation.cycle = 'single_month' },
)
const canManageMembership = computed(() => {
  const bootstrap = session.bootstrap
  if (bootstrap?.actor.role === 'platform_admin') return true
  const access = bootstrap?.staff_access
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes('commercial.membership.manage')),
  )
})

const editorOpen = ref(false)
const editorMode = ref<MembershipEditorMode>('create')
const editingPlanId = ref<number | null>(null)

const form = reactive<MembershipFormState>({
  code: '',
  name: '基础会员',
  description: '',
  sort_order: 10,
  price_yuan: 69,
  included_hours: 0,
  discount_percent: 10,
  device_discount_percent: 10,
  allow_auto_renew: true,
})

const simulation = reactive({
  user_name: '测试用户 001',
  wallet_yuan: 500,
  current_plan_id: 0,
  target_plan_id: 0,
  cycle: 'single_month',
  sample_card_price_yuan: 100,
  sample_device_price_yuan: 1000,
})

const selectedTargetPlan = computed(() =>
  plans.value.find((item) => item.id === simulation.target_plan_id),
)

const selectedCurrentPlan = computed(() =>
  plans.value.find((item) => item.id === simulation.current_plan_id),
)

const selectedTargetVersion = computed(() =>
  preferredSimulationVersion(selectedTargetPlan.value),
)

const simulationMarketingCampaigns = computed(() =>
  (selectedTargetPlan.value?.marketing_campaigns ?? []).filter(
    (campaign) =>
      campaign.status === 'active' &&
      Boolean(membershipCampaignItem(campaign, selectedTargetPlan.value?.id ?? 0)),
  ),
)

const simulationPlanCampaign = computed(() => {
  if (!simulation.cycle.startsWith('campaign:')) return undefined
  const id = Number(simulation.cycle.slice('campaign:'.length))
  return simulationMarketingCampaigns.value.find((campaign) => campaign.id === id)
})

const simulationPlanCampaignItem = computed(() =>
  simulationPlanCampaign.value
    ? membershipCampaignItem(
        simulationPlanCampaign.value,
        selectedTargetPlan.value?.id ?? 0,
      )
    : undefined,
)

const simulationPlanMonths = computed(
  () => Math.max(1, simulationPlanCampaignItem.value?.package_months ?? 1),
)

const simulationPlanDiscountBps = computed(
  () => normalizeDiscountBps(simulationPlanCampaignItem.value?.discount_bps ?? 10000),
)

const simulationCycleLabel = computed(
  () => simulationPlanCampaign.value?.name || '单月原价',
)

const simulationPlanListCents = computed(
  () => floorToWholeYuanCents(
    (selectedTargetVersion.value?.price_cents ?? 0) * simulationPlanMonths.value,
  ),
)
const simulationPlanPriceCents = computed(
  () => marketingPayableCents(
    simulationPlanListCents.value,
    simulationPlanDiscountBps.value,
  ),
)
const simulationPlanSavingCents = computed(
  () => Math.max(0, simulationPlanListCents.value - simulationPlanPriceCents.value),
)

const simulationIncludedSeconds = computed(
  () => (selectedTargetVersion.value?.included_seconds ?? 0) * simulationPlanMonths.value,
)

const simulationDiscountBps = computed(
  () => selectedTargetVersion.value?.default_time_card_discount_bps ?? 10000,
)
const simulationDeviceDiscountBps = computed(
  () => selectedTargetVersion.value?.default_device_discount_bps ?? 10000,
)

const simulationCardPriceCents = computed(() => {
  const originalCents = yuanToCents(simulation.sample_card_price_yuan)
  return marketingPayableCents(originalCents, simulationDiscountBps.value)
})
const simulationDevicePriceCents = computed(() => {
  const originalCents = yuanToCents(simulation.sample_device_price_yuan)
  return marketingPayableCents(originalCents, simulationDeviceDiscountBps.value)
})

const simulationWalletCents = computed(() => yuanToCents(simulation.wallet_yuan))

const simulationRemainingCents = computed(
  () => simulationWalletCents.value - simulationPlanPriceCents.value,
)

const simulationScenario = computed(() => {
  if (!selectedCurrentPlan.value) return '首次开通'
  if (selectedCurrentPlan.value.id === selectedTargetPlan.value?.id) return '会员续费'
  return '会员变更'
})

async function loadPlans() {
  loading.value = true
  error.value = ''
  try {
    const response = await getCommercialMemberships()
    plans.value = response.items
    syncSimulationSelection()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取会员方案失败'
  } finally {
    loading.value = false
  }
}

function syncSimulationSelection() {
  if (!plans.value.length) {
    simulation.current_plan_id = 0
    simulation.target_plan_id = 0
    return
  }
  if (!plans.value.some((item) => item.id === simulation.target_plan_id)) {
    simulation.target_plan_id = plans.value[0].id
  }
  if (!plans.value.some((item) => item.id === simulation.current_plan_id)) {
    simulation.current_plan_id = 0
  }
}

function makeDefaultMembershipCode() {
  const stamp = Date.now().toString(36)
  return 'membership-' + stamp
}
function openCreate() {
  editorMode.value = 'create'
  editingPlanId.value = null
  Object.assign(form, {
    code: makeDefaultMembershipCode(),
    name: '基础会员',
    description: '',
    sort_order: (plans.value.length + 1) * 10,
    price_yuan: 69,
    included_hours: 0,
    discount_percent: 10,
    device_discount_percent: 10,
    allow_auto_renew: true,
  })
  error.value = ''
  editorError.value = ''
  notice.value = ''
  editorOpen.value = true
}

function normalizeDiscountBps(value: number) {
  if (value >= 100 && value < 1000) return value * 10
  if (value > 10000) return 10000
  return Math.max(0, value || 0)
}

function discountBpsToZhe(value: number) {
  return normalizeDiscountBps(value) / 1000
}

function openEdit(plan: CommercialMembershipPlan) {
  editorMode.value = 'edit'
  editingPlanId.value = plan.id
  const version = plan.draft_version ?? plan.active_version ?? plan.latest_version

  Object.assign(form, {
    code: plan.code,
    name: plan.name,
    description: plan.description,
    sort_order: plan.sort_order,
    price_yuan: centsToYuan(version?.price_cents ?? 0),
    included_hours: secondsToHours(version?.included_seconds ?? 0),
    discount_percent: discountBpsToZhe(version?.default_time_card_discount_bps ?? 10000),
    device_discount_percent: discountBpsToZhe(version?.default_device_discount_bps ?? 10000),
    allow_auto_renew: version?.allow_auto_renew ?? true,
  })
  error.value = ''
  editorError.value = ''
  notice.value = ''
  editorOpen.value = true
}

function closeEditor() {
  if (saving.value) return
  editorOpen.value = false
}

async function saveEditor() {
  if (saving.value) return

  const payload = buildPayload()
  if (!payload) return

  saving.value = true
  error.value = ''
  editorError.value = ''
  notice.value = ''

  try {
    if (editorMode.value === 'create') {
      const created = await createCommercialMembership(payload)
      notice.value = '已创建“' + created.name + '”草稿，发布前不会影响任何终端。'
    } else if (editingPlanId.value) {
      const updated = await saveCommercialMembershipDraft(editingPlanId.value, payload)
      notice.value = '“' + updated.name + '”草稿已保存。'
    }
    editorOpen.value = false
    await loadPlans()
  } catch (value) {
    editorError.value = value instanceof Error ? value.message : '保存会员方案失败'
  } finally {
    saving.value = false
  }
}

async function publishPlan(plan: CommercialMembershipPlan) {
  if (!plan.draft_version || publishingId.value !== null) return

  const confirmed = await confirmAction({
    title: '发布会员方案',
    message: '确认发布“' + plan.name + '” V' + plan.draft_version.version_no +
      '？发布后，新购买/新周期将使用这一版本，历史订单不会被改写。',
    confirmText: '确认发布',
  })
  if (!confirmed) return

  publishingId.value = plan.id
  error.value = ''
  notice.value = ''
  try {
    const published = await publishCommercialMembership(plan.id)
    notice.value =
      '已发布“' + published.name + '” V' +
      (published.active_version?.version_no ?? '') + '。'
    await loadPlans()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '发布会员方案失败'
  } finally {
    publishingId.value = null
  }
}

async function toggleListing(plan: CommercialMembershipPlan) {
  if (!canManageMembership.value || listingId.value !== null || plan.status === 'archived' || !plan.active_version) return
  const nextStatus = plan.status === 'active' ? 'inactive' : 'active'
  if (nextStatus === 'inactive') {
    const confirmed = await confirmAction({
      title: '下架会员方案',
      message: '下架“' + plan.name + '”后将停止新销售，已有会员和历史订单不受影响。',
      confirmText: '确认下架',
    })
    if (!confirmed) return
  }
  listingId.value = plan.id
  error.value = ''
  notice.value = ''
  try {
    await setCommercialMembershipListing(plan.id, nextStatus)
    notice.value = nextStatus === 'active' ? '会员方案已上架。' : '会员方案已下架。'
    await loadPlans()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '更新会员方案上下架失败'
  } finally {
    listingId.value = null
  }
}

async function archivePlan(plan: CommercialMembershipPlan) {
  if (!canManageMembership.value || deletingId.value !== null) return
  const confirmed = await confirmAction({
    title: '删除会员方案',
    message: '确认删除“' + plan.name + '”？系统会归档并停止销售，历史会员、订单和版本记录继续保留。',
    confirmText: '确认删除',
  })
  if (!confirmed) return
  deletingId.value = plan.id
  error.value = ''
  notice.value = ''
  try {
    await deleteCommercialMembership(plan.id)
    notice.value = '会员方案已归档，不再销售。'
    await loadPlans()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '删除会员方案失败'
  } finally {
    deletingId.value = null
  }
}

function buildPayload(): CommercialMembershipInput | null {
  const code = form.code.trim().toLowerCase()
  const name = form.name.trim()
  if (code.length < 2) {
    editorError.value = '请填写至少 2 位内部编码。'
    return null
  }
  if (name.length < 2) {
    editorError.value = '请填写会员名称。'
    return null
  }
  if (form.price_yuan < 0) {
    editorError.value = '会员单月价不能小于 0。'
    return null
  }
  if (form.included_hours < 0) {
    editorError.value = '基础时长不能小于 0。'
    return null
  }
  const discountValues = [
    form.discount_percent,
    form.device_discount_percent,
  ]
  if (discountValues.some((value) => value <= 0 || value > 10)) {
    editorError.value = '所有折扣需大于 0 折且不超过 10 折。'
    return null
  }
  return {
    code,
    name,
    description: form.description.trim(),
    sort_order: Math.round(form.sort_order || 0),
    price_cents: yuanToCents(form.price_yuan),
    // Legacy package columns stay neutral. Package/term discounts now live in
    // Product & Membership -> Marketing Design.
    recurring_month_discount_bps: 10000,
    recurring_quarter_discount_bps: 10000,
    annual_discount_bps: 10000,
    included_seconds: Math.round(form.included_hours * 3600),
    default_time_card_discount_bps: Math.round(form.discount_percent * 1000),
    default_device_discount_bps: Math.round(form.device_discount_percent * 1000),
    allow_auto_renew: form.allow_auto_renew,
  }
}

function preferredSimulationVersion(
  plan?: CommercialMembershipPlan,
): CommercialMembershipVersion | undefined {
  return plan?.draft_version ?? plan?.active_version ?? plan?.latest_version
}

function formatMoney(cents: number) {
  return '¥' + Math.round(cents / 100).toLocaleString('zh-CN', {
    maximumFractionDigits: 0,
  })
}

function formatHours(seconds: number) {
  const hours = seconds / 3600
  return Number.isInteger(hours)
    ? hours.toLocaleString('zh-CN') + ' 小时'
    : hours.toFixed(1) + ' 小时'
}

function formatDailyAverageHours(seconds: number) {
  const dailyHours = Math.max(0, seconds) / 3600 / 30
  const value = dailyHours.toLocaleString('zh-CN', {
    minimumFractionDigits: 0,
    maximumFractionDigits: 1,
  })
  return value + ' 小时 / 天'
}

function formatYuanPerHour(priceCents: number, includedSeconds: number, months = 1) {
  const totalHours = Math.max(0, includedSeconds) / 3600 * Math.max(0, months)
  if (totalHours <= 0) return '暂无元/时'
  return '¥' + wholeYuanPerHour(priceCents, totalHours) + '/时'
}

function formatDiscount(bps: number) {
  const safeBps = normalizeDiscountBps(bps)
  if (safeBps <= 0) return '赠送'
  if (safeBps >= 10000) return '原价'
  const zhe = safeBps / 1000
  return zhe.toFixed(zhe % 1 === 0 ? 0 : 1) + ' 折'
}

function membershipCampaignItem(
  campaign: MarketingCampaign,
  planId: number,
): MarketingCampaignItem | undefined {
  const item = (campaign.items ?? []).find(
    (entry) => entry.target_type === 'membership' && entry.target_id === planId,
  )
  if (item) return item
  if (campaign.target_type === 'membership' && campaign.target_id === planId) {
    return {
      target_type: 'membership',
      target_id: planId,
      pricing_mode: campaign.pricing_mode || 'discount',
      package_months: campaign.package_months || 1,
      discount_bps: campaign.discount_bps ?? 10000,
      quantity: 1,
    }
  }
  return undefined
}

function marketingCampaignPrice(plan: CommercialMembershipPlan, campaign: MarketingCampaign) {
  const version = preferredSimulationVersion(plan)
  const monthlyPrice = version?.price_cents ?? 0
  const item = membershipCampaignItem(campaign, plan.id)
  if (!item) return 0
  const months = Math.max(1, item.package_months || 1)
  const safeDiscount = normalizeDiscountBps(item.discount_bps)
  return marketingPayableCents(
    floorToWholeYuanCents(monthlyPrice * months),
    safeDiscount,
  )
}

function formatMarketingMoney(cents: number) {
  return formatWholeYuanMoney(cents)
}

function activeMarketingCampaigns(plan: CommercialMembershipPlan) {
  return (plan.marketing_campaigns ?? []).filter(
    (campaign) =>
      campaign.status === 'active' &&
      Boolean(membershipCampaignItem(campaign, plan.id)),
  )
}

function campaignPackageLabel(item?: MarketingCampaignItem) {
  if (!item || item.pricing_mode !== 'package') return '普通折扣'
  if (item.package_months === 1) return '包月'
  if (item.package_months === 3) return '包季'
  if (item.package_months === 6) return '包半年'
  if (item.package_months === 12) return '包年'
  return item.package_months + '个月'
}

function versionLabel(plan: CommercialMembershipPlan) {
  if (plan.draft_version) return '草稿 V' + plan.draft_version.version_no
  if (plan.active_version) return '已发布 V' + plan.active_version.version_no
  return '暂无版本'
}

function statusLabel(plan: CommercialMembershipPlan) {
  if (plan.status === 'archived') return '已删除/归档'
  if (plan.status === 'active') return '已上架'
  if (plan.status === 'inactive') return '已下架'
  if (plan.draft_version) return '待发布'
  return '未上架'
}

function centsToYuan(cents: number) {
  return Number((cents / 100).toFixed(2))
}

function yuanToCents(yuan: number) {
  const value = Number.isFinite(Number(yuan)) ? Number(yuan) : 0
  return Math.max(0, Math.round(value * 100))
}

function secondsToHours(seconds: number) {
  return Number((seconds / 3600).toFixed(2))
}

function resetSimulation() {
  simulation.user_name = '测试用户 001'
  simulation.wallet_yuan = 500
  simulation.current_plan_id = 0
  simulation.target_plan_id = plans.value[0]?.id ?? 0
  simulation.cycle = 'single_month'
  simulation.sample_card_price_yuan = 100
  simulation.sample_device_price_yuan = 1000
}

onMounted(loadPlans)
</script>

<template>
  <div class="commercial-membership-page">
    <ModulePageNav :context="navContext" :active-title="pageTitle" :active-nav-title="pageTitle" />
    <section class="commercial-hero">
      <div>
        <p class="section-kicker">COMMERCIAL CENTER · MEMBERSHIP</p>
        <h2>{{ pageTitle }}</h2>
        <p>
          这里只定义会员基础价格、月度基础时长和会员固有权益；促销折扣统一由“营销运维 → 活动营销”管理。
        </p>
      </div>

      <div class="commercial-hero-actions">
        <button class="ghost-button" type="button" :disabled="loading" @click="loadPlans">
          {{ loading ? '刷新中...' : '刷新' }}
        </button>
        <button v-if="canManageMembership" class="primary-button" type="button" @click="openCreate">
          ＋ 新建会员方案
        </button>
      </div>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <div v-if="notice" class="settings-success">{{ notice }}</div>

    <section class="commercial-membership-layout" :class="{ 'single-pane': showPlans !== showSimulator }">
      <div v-if="showPlans" class="membership-plan-panel">
        <div class="commercial-section-head">
          <div>
            <span class="section-kicker">PLAN CATALOG</span>
            <h3>会员档位</h3>
          </div>
          <span class="commercial-help-text">修改已发布方案时会自动创建新草稿版本</span>
        </div>

        <DataListControls
          v-model:view-mode="viewMode"
          v-model:search="search"
          v-model:status="statusFilter"
          v-model:sort="sortMode"
          v-model:page-size="pageSize"
          search-placeholder="会员名称 / 编码 / 说明"
          :status-options="statusOptions"
          :sort-options="sortOptions"
        />

        <div v-if="loading" class="panel-loading">正在读取会员方案...</div>

        <div v-else-if="filteredPlans.length && viewMode === 'card'" class="membership-plan-list">
          <article
            v-for="(plan, index) in pagedPlans"
            :key="plan.id"
            class="membership-plan-card"
            :class="{ featured: index === 1 }"
          >
            <div class="membership-plan-top">
              <div>
                <div class="membership-plan-badges">
                  <span class="membership-status-badge" :class="{ active: Boolean(plan.active_version) }">
                    {{ statusLabel(plan) }}
                  </span>
                  <span class="membership-version-badge">{{ versionLabel(plan) }}</span>
                </div>
                <h4>{{ plan.name }}</h4>
                <span class="membership-code">{{ plan.code }}</span>
              </div>
              <button v-if="canManageMembership" class="plan-edit-button" type="button" @click="openEdit(plan)">
                编辑
              </button>
            </div>

            <div class="membership-price-row">
              <span>¥</span>
              <strong>
                {{
                  centsToYuan(
                    preferredSimulationVersion(plan)?.price_cents ?? 0,
                  ).toLocaleString('zh-CN')
                }}
              </strong>
              <small>/ 月</small>
            </div>

            <p class="membership-plan-description">
              {{ plan.description || '暂无会员说明，可在编辑中补充。' }}
            </p>

            <div class="membership-benefit-grid">
              <div>
                <span>每月基础时长</span>
                <strong>{{ formatHours(preferredSimulationVersion(plan)?.included_seconds ?? 0) }}</strong>
                <small>
                  日均约 {{ formatDailyAverageHours(preferredSimulationVersion(plan)?.included_seconds ?? 0) }}
                  · 按 30 天计算
                </small>
                <small class="membership-hourly-rate">
                  月费折算 {{ formatYuanPerHour(
                    preferredSimulationVersion(plan)?.price_cents ?? 0,
                    preferredSimulationVersion(plan)?.included_seconds ?? 0,
                  ) }}
                </small>
              </div>
              <div>
                <span>超额时长卡</span>
                <strong>{{ formatDiscount(preferredSimulationVersion(plan)?.default_time_card_discount_bps ?? 10000) }}</strong>
                <small>按时长卡售价结算</small>
              </div>
              <div>
                <span>购买设备</span>
                <strong>{{ formatDiscount(preferredSimulationVersion(plan)?.default_device_discount_bps ?? 10000) }}</strong>
                <small>按设备正常售价结算</small>
              </div>
              <div
                v-for="campaign in activeMarketingCampaigns(plan)"
                :key="'marketing-' + campaign.id"
                class="membership-marketing-benefit"
              >
                <span>{{ campaign.name }}</span>
                <strong>
                  {{ campaignPackageLabel(membershipCampaignItem(campaign, plan.id)) }}
                  ·
                  {{ formatDiscount(membershipCampaignItem(campaign, plan.id)?.discount_bps ?? 10000) }}
                </strong>
                <small>
                  {{ membershipCampaignItem(campaign, plan.id)?.package_months ?? 1 }}个月实付
                  {{ formatMarketingMoney(marketingCampaignPrice(plan, campaign)) }}
                </small>
                <small class="membership-hourly-rate">
                  {{ formatYuanPerHour(
                    marketingCampaignPrice(plan, campaign),
                    preferredSimulationVersion(plan)?.included_seconds ?? 0,
                    membershipCampaignItem(campaign, plan.id)?.package_months ?? 1,
                  ) }}
                </small>
              </div>
              <div v-if="!activeMarketingCampaigns(plan).length" class="membership-marketing-empty">
                <span>营销活动</span>
                <strong>暂未挂链</strong>
                <small>到“活动营销”创建营销计划并挂接该会员</small>
              </div>
            </div>

            <div class="product-marketing-links">
              <span>参与营销计划 · {{ plan.marketing_campaigns?.length || 0 }}</span>
              <div v-if="plan.marketing_campaigns?.length" class="product-marketing-link-list">
                <div
                  v-for="campaign in plan.marketing_campaigns"
                  :key="'linked-marketing-' + campaign.id"
                  class="product-marketing-link"
                  :class="{ inactive: campaign.status !== 'active' }"
                >
                  <strong>{{ campaign.name }}</strong>
                  <small>
                    {{ campaign.status === 'active' ? '生效中' : '已停用' }}
                    · {{ campaignPackageLabel(membershipCampaignItem(campaign, plan.id)) }}
                    · {{ formatDiscount(membershipCampaignItem(campaign, plan.id)?.discount_bps ?? 10000) }}
                    · 实际价值 {{ formatMarketingMoney(marketingCampaignPrice(plan, campaign)) }}
                  </small>
                </div>
              </div>
              <small v-else class="product-marketing-empty">暂未参加营销计划</small>
            </div>

            <div class="membership-plan-actions">
              <button v-if="canManageMembership && plan.status !== 'archived'" class="ghost-button" type="button" @click="openEdit(plan)">
                {{ plan.draft_version ? '继续编辑草稿' : '创建新版本' }}
              </button>
              <button
                v-if="plan.draft_version && canManageMembership && plan.status !== 'archived'"
                class="primary-button"
                type="button"
                :disabled="publishingId !== null"
                @click="publishPlan(plan)"
              >
                {{
                  publishingId === plan.id
                    ? '发布中...'
                    : '发布 V' + plan.draft_version.version_no
                }}
              </button>
              <button
                v-if="canManageMembership && plan.active_version && plan.status !== 'archived'"
                class="ghost-button"
                type="button"
                :disabled="listingId !== null"
                @click="toggleListing(plan)"
              >
                {{ listingId === plan.id ? '处理中...' : (plan.status === 'active' ? '下架' : '上架') }}
              </button>
              <button
                v-if="canManageMembership && plan.status !== 'archived'"
                class="ghost-button"
                type="button"
                :disabled="deletingId !== null"
                @click="archivePlan(plan)"
              >
                {{ deletingId === plan.id ? '删除中...' : '删除' }}
              </button>
              <span v-if="!canManageMembership" class="plan-published-note">只读</span>
            </div>
          </article>
        </div>

        <div v-else-if="filteredPlans.length && viewMode === 'table'" class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>会员方案</th>
                <th>价格</th>
                <th>基础时长</th>
                <th>时长卡折扣</th>
                <th>版本状态</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="plan in pagedPlans" :key="plan.id">
                <td><strong>{{ plan.name }}</strong><small>{{ plan.code }}</small></td>
                <td>{{ formatMoney(preferredSimulationVersion(plan)?.price_cents ?? 0) }}</td>
                <td>
                  {{ formatHours(preferredSimulationVersion(plan)?.included_seconds ?? 0) }}
                  <small>日均约 {{ formatDailyAverageHours(preferredSimulationVersion(plan)?.included_seconds ?? 0) }}</small>
                  <small>{{ formatYuanPerHour(
                    preferredSimulationVersion(plan)?.price_cents ?? 0,
                    preferredSimulationVersion(plan)?.included_seconds ?? 0,
                  ) }}</small>
                </td>
                <td>{{ formatDiscount(preferredSimulationVersion(plan)?.default_time_card_discount_bps ?? 10000) }}</td>
                <td><span class="status-pill">{{ statusLabel(plan) }} · {{ versionLabel(plan) }}</span></td>
                <td>
                  <div class="table-actions">
                    <button v-if="canManageMembership && plan.status !== 'archived'" class="text-action" type="button" @click="openEdit(plan)">编辑</button>
                    <button v-if="canManageMembership && plan.active_version && plan.status !== 'archived'" class="text-action" type="button" @click="toggleListing(plan)">{{ plan.status === 'active' ? '下架' : '上架' }}</button>
                    <button v-if="canManageMembership && plan.status !== 'archived'" class="text-action" type="button" @click="archivePlan(plan)">删除</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else class="commercial-empty-state">
          <div class="commercial-empty-icon">◇</div>
          <strong>{{ plans.length ? '没有符合筛选条件的会员方案' : '还没有会员方案' }}</strong>
          <span>
            {{ plans.length ? '可以调整搜索、状态或排序条件。' : '先建立基础会员、标准会员、高级会员等档位。新建后默认是草稿，不会立即影响终端。' }}
          </span>
          <button v-if="canManageMembership" class="primary-button" type="button" @click="openCreate">
            新建第一个会员方案
          </button>
        </div>

        <PaginationBar
          v-model:page="page"
          :total-pages="totalPages"
          :total="filteredPlans.length"
          :page-size="pageSize"
        />
      </div>

      <aside v-if="showSimulator" class="membership-simulator">
        <div class="simulator-head">
          <div>
            <span class="section-kicker">SANDBOX USER</span>
            <h3>测试用户模拟器</h3>
          </div>
          <span class="sandbox-badge">不入账</span>
        </div>

        <p class="simulator-intro">
          用临时测试用户预览会员规则。这里不会创建终端、不会扣钱包、不会生成订单。
        </p>

        <div class="simulator-form">
          <label>
            <span>测试用户名称</span>
            <input v-model="simulation.user_name" type="text" maxlength="32" />
          </label>

          <label>
            <span>模拟钱包余额</span>
            <div class="input-with-prefix">
              <span>¥</span>
              <input v-model.number="simulation.wallet_yuan" type="number" min="0" step="1" />
            </div>
          </label>

          <label>
            <span>当前会员</span>
            <select v-model.number="simulation.current_plan_id">
              <option :value="0">未开通会员</option>
              <option v-for="plan in plans" :key="plan.id" :value="plan.id">
                {{ plan.name }}
              </option>
            </select>
          </label>

          <label>
            <span>准备购买</span>
            <select v-model.number="simulation.target_plan_id">
              <option v-if="!plans.length" :value="0">暂无会员方案</option>
              <option v-for="plan in plans" :key="plan.id" :value="plan.id">
                {{ plan.name }} ·
                {{ plan.draft_version ? '测试草稿 V' + plan.draft_version.version_no : '正式版本' }}
              </option>
            </select>
          </label>

          <label>
            <span>购买方式</span>
            <select v-model="simulation.cycle">
              <option value="single_month">单月原价</option>
              <option
                v-for="campaign in simulationMarketingCampaigns"
                :key="campaign.id"
                :value="'campaign:' + campaign.id"
              >
                {{ campaign.name }}
                · {{ campaignPackageLabel(membershipCampaignItem(campaign, selectedTargetPlan?.id ?? 0)) }}
                · {{ formatDiscount(membershipCampaignItem(campaign, selectedTargetPlan?.id ?? 0)?.discount_bps ?? 10000) }}
              </option>
            </select>
          </label>

          <label>
            <span>测试时长卡原价</span>
            <div class="input-with-prefix">
              <span>¥</span>
              <input v-model.number="simulation.sample_card_price_yuan" type="number" min="0" step="1" />
            </div>
          </label>

          <label>
            <span>测试设备正常售价</span>
            <div class="input-with-prefix">
              <span>¥</span>
              <input v-model.number="simulation.sample_device_price_yuan" type="number" min="0" step="1" />
            </div>
          </label>
        </div>

        <div v-if="selectedTargetVersion" class="simulation-result-card">
          <div class="simulation-user-line">
            <div class="simulation-avatar">测</div>
            <div>
              <strong>{{ simulation.user_name || '测试用户' }}</strong>
              <span>{{ simulationScenario }} · 沙盒模式</span>
            </div>
          </div>

          <div class="simulation-purchase">
            <span>购买 {{ selectedTargetPlan?.name }} · {{ simulationCycleLabel }}</span>
            <strong>{{ formatMarketingMoney(simulationPlanPriceCents) }}</strong>
            <small>
              一次支付 · {{ simulationPlanMonths }} 个月 ·
              {{ formatDiscount(simulationPlanDiscountBps) }} ·
              V{{ selectedTargetVersion.version_no }}
              {{ selectedTargetVersion.lifecycle_status === 'draft' ? '草稿' : '正式' }}
            </small>
          </div>

          <div class="simulation-metrics">
            <div>
              <span>套餐原价合计</span>
              <strong>{{ formatMarketingMoney(simulationPlanListCents) }}</strong>
            </div>
            <div>
              <span>营销活动折扣</span>
              <strong>{{ formatDiscount(simulationPlanDiscountBps) }}</strong>
            </div>
            <div>
              <span>本次已优惠</span>
              <strong>{{ formatMarketingMoney(simulationPlanSavingCents) }}</strong>
            </div>
            <div>
              <span>获得基础时长</span>
              <strong>{{ formatHours(simulationIncludedSeconds) }}</strong>
            </div>
            <div>
              <span>时长卡 {{ formatDiscount(simulationDiscountBps) }}</span>
              <strong>{{ formatMarketingMoney(simulationCardPriceCents) }}</strong>
            </div>
            <div>
              <span>设备 {{ formatDiscount(simulationDeviceDiscountBps) }}</span>
              <strong>{{ formatMarketingMoney(simulationDevicePriceCents) }}</strong>
            </div>
            <div>
              <span>购买会员后余额</span>
              <strong :class="{ negative: simulationRemainingCents < 0 }">
                {{ formatMoney(Math.abs(simulationRemainingCents)) }}
                {{ simulationRemainingCents < 0 ? '不足' : '' }}
              </strong>
            </div>
          </div>

          <div
            class="simulation-verdict"
            :class="{ insufficient: simulationRemainingCents < 0 }"
          >
            <strong>
              {{ simulationRemainingCents >= 0 ? '余额足够，可完成购买' : '余额不足，真实购买会被拦截' }}
            </strong>
            <span>这是规则预览，不会实际扣除任何余额。</span>
          </div>
        </div>

        <div v-else class="simulator-empty">
          <strong>先建立一个会员方案</strong>
          <span>有会员草稿后，就可以在这里立即模拟用户购买效果。</span>
        </div>

        <button class="simulator-reset" type="button" @click="resetSimulation">
          重置测试用户
        </button>
      </aside>
    </section>

    <Teleport to="body">
      <div
        v-if="editorOpen && canManageMembership"
        class="modal-backdrop commercial-membership-modal-backdrop"
        @click.self="closeEditor"
      >
      <form class="modal-card commercial-editor-modal" @submit.prevent="saveEditor">
        <div class="modal-header">
          <div>
            <span class="section-kicker">
              {{ editorMode === 'create' ? 'CREATE MEMBERSHIP' : 'EDIT DRAFT' }}
            </span>
            <h3>{{ editorMode === 'create' ? '新建会员方案' : '编辑会员草稿' }}</h3>
          </div>
          <button type="button" class="close-button" @click="closeEditor">×</button>
        </div>

        <div v-if="editorError" class="inline-error commercial-editor-error">
          {{ editorError }}
        </div>

        <div class="commercial-editor-grid">
          <label>
            <span>会员名称 <b>*</b></span>
            <input v-model="form.name" type="text" maxlength="128" required placeholder="例如：标准会员" />
          </label>

          <label>
            <span>内部编码 <b>*</b></span>
            <input
              v-model="form.code"
              type="text"
              maxlength="64"
              placeholder="standard"
              :disabled="editorMode === 'edit'"
            />
            <small>小写字母、数字、下划线或短横线。创建后作为稳定标识。</small>
          </label>

          <label>
            <span>单月基础价</span>
            <div class="input-with-prefix">
              <span>¥</span>
              <input v-model.number="form.price_yuan" type="number" min="0" step="0.01" />
            </div>
            <small>这里只定义产品基础价；营销折扣到“活动营销”中配置。</small>
          </label>

          <label>
            <span>每月基础 AI 时长</span>
            <div class="input-with-suffix">
              <input v-model.number="form.included_hours" type="number" min="0" step="1" />
              <span>小时</span>
            </div>
          </label>

          <label>
            <span>超额购买时长卡折扣</span>
            <div class="input-with-suffix">
              <input v-model.number="form.discount_percent" type="number" min="0.1" max="10" step="0.1" />
              <span>折</span>
            </div>
            <small>基础 AI 时长用完后，再买时长卡按此会员折扣结算。</small>
          </label>

          <label>
            <span>购买设备折扣</span>
            <div class="input-with-suffix">
              <input v-model.number="form.device_discount_percent" type="number" min="0.1" max="10" step="0.1" />
              <span>折</span>
            </div>
            <small>设备商城以设备正常售价为基准计算该会员的实付价。</small>
          </label>

          <label>
            <span>显示排序</span>
            <input v-model.number="form.sort_order" type="number" step="1" />
          </label>

          <label class="commercial-editor-full">
            <span>会员说明</span>
            <textarea
              v-model="form.description"
              rows="3"
              maxlength="1024"
              placeholder="描述这个会员适合什么终端、主要权益等"
            ></textarea>
          </label>

          <label class="commercial-switch-row commercial-editor-full">
            <div>
              <strong>允许自动续费</strong>
              <span>这里只保存会员规则，真实支付自动续费以后再接。</span>
            </div>
            <input v-model="form.allow_auto_renew" type="checkbox" />
          </label>
        </div>

        <div class="commercial-editor-note">
          <strong>版本安全</strong>
          <span>
            保存只生成或更新草稿；点击“发布”以后才成为正式版本。历史成交订单不会被新版本覆盖。
          </span>
        </div>

        <div class="modal-actions">
          <button class="ghost-button" type="button" :disabled="saving" @click="closeEditor">
            取消
          </button>
          <button class="primary-button" type="submit" :disabled="saving">
            {{ saving ? '保存中...' : editorMode === 'create' ? '创建草稿' : '保存草稿' }}
          </button>
        </div>
      </form>
      </div>
    </Teleport>
  </div>
</template>
