<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import {
  floorToWholeYuanCents,
  formatWholeYuanMoney,
  marketingPayableCents,
  wholeYuanPerHour,
} from '../pricingRules'
import { session } from '../session'
import { confirmAction, useFeedbackErrorRef } from '../uiFeedback'
import {
  createCommercialMarketingCampaign,
  deleteCommercialMarketingCampaign,
  getCommercialDeviceProducts,
  getCommercialMarketingCampaigns,
  getCommercialMemberships,
  getCommercialTimeCards,
  updateCommercialMarketingCampaign,
} from '../api'
import type {
  CommercialDeviceProduct,
  CommercialMembershipPlan,
  CommercialTimeCardProduct,
  MarketingCampaign,
  MarketingCampaignItem,
} from '../types'

type TargetType = 'membership' | 'time_card' | 'device_product'
type MarketingDisplayLocation = 'shop' | 'membership' | 'backoffice'

const displayLocationOptions: Array<{
  code: MarketingDisplayLocation
  label: string
  description: string
}> = [
  { code: 'shop', label: '终端商城', description: '在终端商城的营销活动区域和对应商品中展示。' },
  { code: 'membership', label: '会员中心', description: '只在客户会员中心的会员周期与优惠中展示。' },
  { code: 'backoffice', label: '仅后台', description: '保留活动和价格规则，但客户端任何位置都不展示。' },
]

interface MarketingPlanView extends MarketingCampaign {
  description: string
}

interface MarketingItemForm {
  local_id: string
  target_type: TargetType
  target_id: number
  pricing_mode: 'discount' | 'package'
  package_months: number
  discount_zhe: number
  quantity: number
}

const membershipPackageOptions = [
  { months: 1, label: '包月' },
  { months: 3, label: '包季' },
  { months: 6, label: '包半年' },
  { months: 12, label: '包年' },
] as const

const campaigns = ref<MarketingCampaign[]>([])
const memberships = ref<CommercialMembershipPlan[]>([])
const timeCards = ref<CommercialTimeCardProduct[]>([])
const devices = ref<CommercialDeviceProduct[]>([])
const loading = ref(true)
const saving = ref(false)
const editorOpen = ref(false)
const editingId = ref<number | null>(null)
const error = useFeedbackErrorRef()
const notice = ref('')
const search = ref('')
const targetFilter = ref<'all' | TargetType>('all')
const placementFilter = ref<'all' | MarketingDisplayLocation>('all')
const collapsedPlanIds = ref<Set<number>>(new Set())
const expandedTargetKeys = ref<Set<string>>(new Set())

const form = reactive({
  name: '',
  description: '',
  status: 'active' as 'active' | 'inactive',
  sort_order: 10,
  starts_at: '',
  ends_at: '',
  display_locations: ['backoffice'] as MarketingDisplayLocation[],
  items: [] as MarketingItemForm[],
})

const navContext = computed(() => 'activityMarketing' as const)

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  if (bootstrap?.actor.role === 'platform_admin') return true
  const access = bootstrap?.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('commercial.marketing.manage')),
  )
})

function normalizeDiscountBps(value: number) {
  if (value >= 100 && value < 1000) return value * 10
  if (value > 10000) return 10000
  return Math.max(0, Math.round(value || 0))
}

function normalizeDisplayLocations(values?: string[]) {
  const source = Array.isArray(values) ? values : []
  const known = source.filter(
    (value): value is MarketingDisplayLocation =>
      value === 'shop' || value === 'membership' || value === 'backoffice',
  )
  if (!known.length) return ['backoffice'] as MarketingDisplayLocation[]
  const unique = [...new Set(known)]
  if (unique.length > 1) {
    return unique.filter((value) => value !== 'backoffice')
  }
  return unique
}

function displayLocationLabel(value: string) {
  return displayLocationOptions.find((item) => item.code === value)?.label || value
}

function toggleDisplayLocation(value: MarketingDisplayLocation) {
  const current = new Set(form.display_locations)
  if (value === 'backoffice') {
    form.display_locations = current.has('backoffice') ? [] : ['backoffice']
    return
  }
  current.delete('backoffice')
  if (current.has(value)) current.delete(value)
  else current.add(value)
  form.display_locations = [...current]
}

const plans = computed<MarketingPlanView[]>(() =>
  campaigns.value
    .map((item) => ({ ...item, description: String(item.description || '') }))
    .sort((a, b) => a.sort_order - b.sort_order || a.id - b.id),
)

const filteredPlans = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return plans.value.filter((plan) => {
    if (
      placementFilter.value !== 'all' &&
      !normalizeDisplayLocations(plan.display_locations).includes(placementFilter.value)
    ) return false
    if (
      targetFilter.value !== 'all' &&
      !plan.items.some((item) => item.target_type === targetFilter.value)
    ) return false
    if (!keyword) return true
    return [
      plan.name,
      plan.code,
      plan.description,
      ...plan.items.map((item) => targetName(item.target_type, item.target_id)),
    ].some((value) => String(value || '').toLowerCase().includes(keyword))
  })
})

function campaignRuntimeStatus(plan: MarketingPlanView) {
  if (plan.status !== 'active') return 'inactive'
  const now = Date.now()
  const start = plan.starts_at ? new Date(plan.starts_at).getTime() : 0
  const end = plan.ends_at ? new Date(plan.ends_at).getTime() : 0
  if (start && start > now) return 'scheduled'
  if (end && end <= now) return 'ended'
  return 'active'
}

function campaignRuntimeStatusLabel(plan: MarketingPlanView) {
  const status = campaignRuntimeStatus(plan)
  if (status === 'scheduled') return '待开始'
  if (status === 'ended') return '已结束'
  if (status === 'inactive') return '已停用'
  return '生效中'
}

function targetKey(planId: number, index: number) {
  return planId + ':' + index
}

function isPlanOpen(planId: number) {
  return !collapsedPlanIds.value.has(planId)
}

function togglePlan(planId: number) {
  const next = new Set(collapsedPlanIds.value)
  if (next.has(planId)) next.delete(planId)
  else next.add(planId)
  collapsedPlanIds.value = next
}

function isTargetOpen(planId: number, index: number) {
  return expandedTargetKeys.value.has(targetKey(planId, index))
}

function toggleTarget(planId: number, index: number) {
  const key = targetKey(planId, index)
  const next = new Set(expandedTargetKeys.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expandedTargetKeys.value = next
}

function itemCalculationText(item: MarketingCampaignItem) {
  const quantity = Math.max(1, item.quantity || 1)
  const months = item.target_type === 'membership'
    ? Math.max(1, item.package_months || 1)
    : 1
  const base = formatMoney(basePriceCents(item))
  const discount = formatDiscount(item.discount_bps)
  const payable = item.discount_bps === 0 ? '赠送' : formatMoney(payableCents(item))
  const unitNote =
    item.target_type === 'membership'
      ? months + ' 个月'
      : quantity > 1
        ? quantity + ' 份'
        : '1 份'
  return base + ' · ' + unitNote + ' · ' + discount + ' → ' + payable
}

function makeLocalId() {
  return 'mi-' + Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 7)
}

function defaultTargetId(type: TargetType) {
  if (type === 'membership') return memberships.value[0]?.id ?? 0
  if (type === 'time_card') return timeCards.value[0]?.id ?? 0
  return devices.value[0]?.id ?? 0
}

function addTarget(type: TargetType = 'membership') {
  form.items.push({
    local_id: makeLocalId(),
    target_type: type,
    target_id: defaultTargetId(type),
    pricing_mode: 'discount',
    package_months: 1,
    discount_zhe: 10,
    quantity: 1,
  })
}

function removeTarget(index: number) {
  if (form.items.length <= 1) {
    error.value = '一个营销计划至少要保留 1 个标的。'
    return
  }
  form.items.splice(index, 1)
}

function changeTargetType(item: MarketingItemForm) {
  item.target_id = defaultTargetId(item.target_type)
  if (item.target_type !== 'membership') {
    item.pricing_mode = 'discount'
    item.package_months = 1
  }
}

function setMembershipPackageMonths(item: MarketingItemForm, months: number) {
  item.pricing_mode = 'package'
  item.package_months = months
}

function targetOptions(type: TargetType) {
  if (type === 'membership') return memberships.value.map((item) => ({ id: item.id, name: item.name }))
  if (type === 'time_card') return timeCards.value.map((item) => ({ id: item.id, name: item.name }))
  return devices.value.map((item) => ({ id: item.id, name: item.name }))
}

function targetTypeLabel(type: string) {
  if (type === 'membership') return '会员方案'
  if (type === 'time_card') return '时长卡'
  return '设备商品'
}

function targetName(type: string, id: number) {
  if (type === 'membership') return memberships.value.find((item) => item.id === id)?.name || '#' + id
  if (type === 'time_card') return timeCards.value.find((item) => item.id === id)?.name || '#' + id
  return devices.value.find((item) => item.id === id)?.name || '#' + id
}

function membershipVersion(id: number) {
  const item = memberships.value.find((row) => row.id === id)
  return item?.active_version ?? item?.draft_version ?? item?.latest_version
}

function timeCardVersion(id: number) {
  const item = timeCards.value.find((row) => row.id === id)
  return item?.active_version ?? item?.draft_version ?? item?.latest_version
}

function deviceVersion(id: number) {
  const item = devices.value.find((row) => row.id === id)
  return item?.active_version ?? item?.draft_version ?? item?.latest_version
}

function itemFromForm(item: MarketingItemForm): MarketingCampaignItem {
  const packageMode = item.target_type === 'membership' && item.pricing_mode === 'package'
  return {
    target_type: item.target_type,
    target_id: item.target_id,
    pricing_mode: packageMode ? 'package' : 'discount',
    package_months: packageMode
      ? Math.max(1, Math.round(item.package_months || 1))
      : 1,
    discount_bps: Math.round(Math.max(0, Math.min(10, item.discount_zhe)) * 1000),
    quantity: Math.max(1, Math.round(item.quantity || 1)),
  }
}

function packageLabel(item: MarketingCampaignItem) {
  if (item.target_type !== 'membership' || item.pricing_mode !== 'package') {
    return item.quantity > 1 ? '购买 × ' + item.quantity : '单品'
  }
  const months = item.package_months
  const base =
    months === 1 ? '包月' :
    months === 3 ? '包季' :
    months === 6 ? '包半年' :
    months === 12 ? '包年' : months + '个月'
  return item.quantity > 1 ? base + ' × ' + item.quantity : base
}

function formatDiscount(value: number) {
  const bps = normalizeDiscountBps(value)
  if (bps <= 0) return '赠送'
  if (bps >= 10000) return '原价'
  const zhe = bps / 1000
  return zhe.toFixed(zhe % 1 === 0 ? 0 : 1) + ' 折'
}

function formatMoney(cents: number) {
  return formatWholeYuanMoney(cents)
}

function basePriceCents(item: MarketingCampaignItem) {
  const qty = Math.max(1, item.quantity || 1)
  if (item.target_type === 'membership') {
    return floorToWholeYuanCents(
      (membershipVersion(item.target_id)?.price_cents ?? 0) *
      Math.max(1, item.package_months || 1) * qty,
    )
  }
  if (item.target_type === 'time_card') {
    return floorToWholeYuanCents((timeCardVersion(item.target_id)?.price_cents ?? 0) * qty)
  }
  return floorToWholeYuanCents(
    (deviceVersion(item.target_id)?.sale_price_cents ??
      deviceVersion(item.target_id)?.list_price_cents ?? 0) * qty,
  )
}

function payableCents(item: MarketingCampaignItem) {
  return marketingPayableCents(basePriceCents(item), normalizeDiscountBps(item.discount_bps))
}

function itemHours(item: MarketingCampaignItem) {
  const qty = Math.max(1, item.quantity || 1)
  if (item.target_type === 'membership') {
    return (membershipVersion(item.target_id)?.included_seconds ?? 0) / 3600 *
      Math.max(1, item.package_months || 1) * qty
  }
  if (item.target_type === 'time_card') {
    return (timeCardVersion(item.target_id)?.duration_seconds ?? 0) / 3600 * qty
  }
  return 0
}

function hourlyPrice(item: MarketingCampaignItem) {
  const hours = itemHours(item)
  if (hours <= 0) return ''
  return '¥' + wholeYuanPerHour(payableCents(item), hours) + '/时'
}

function planBaseCents(plan: MarketingPlanView) {
  return plan.items.reduce((sum, item) => sum + basePriceCents(item), 0)
}

function planPayableCents(plan: MarketingPlanView) {
  return plan.items.reduce((sum, item) => sum + payableCents(item), 0)
}

function planSavedCents(plan: MarketingPlanView) {
  return Math.max(0, planBaseCents(plan) - planPayableCents(plan))
}

function formPreviewItems() {
  return form.items.map(itemFromForm)
}

const formBaseCents = computed(() =>
  formPreviewItems().reduce((sum, item) => sum + basePriceCents(item), 0),
)
const formPayableCents = computed(() =>
  formPreviewItems().reduce((sum, item) => sum + payableCents(item), 0),
)

function makeCode() {
  return 'marketing-' + Date.now().toString(36)
}

function toLocalDateTimeInput(value?: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  const pad = (part: number) => String(part).padStart(2, '0')
  return [
    date.getFullYear(),
    '-',
    pad(date.getMonth() + 1),
    '-',
    pad(date.getDate()),
    'T',
    pad(date.getHours()),
    ':',
    pad(date.getMinutes()),
  ].join('')
}

function toISODateTime(value: string) {
  if (!value.trim()) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function formatCampaignTime(value?: string) {
  if (!value) return '不限'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '不限'
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

function resetForm() {
  form.name = ''
  form.description = ''
  form.status = 'active'
  form.sort_order = (plans.value.length + 1) * 10
  form.starts_at = ''
  form.ends_at = ''
  form.display_locations = ['backoffice']
  form.items.splice(0)
  addTarget('membership')
}

function openCreate() {
  editingId.value = null
  resetForm()
  editorOpen.value = true
  error.value = ''
  notice.value = ''
}

function openEdit(plan: MarketingPlanView) {
  editingId.value = plan.id
  form.name = plan.name
  form.description = plan.description
  form.status = plan.status === 'active' ? 'active' : 'inactive'
  form.sort_order = plan.sort_order
  form.starts_at = toLocalDateTimeInput(plan.starts_at)
  form.ends_at = toLocalDateTimeInput(plan.ends_at)
  form.display_locations = normalizeDisplayLocations(plan.display_locations)
  form.items.splice(0)
  for (const item of plan.items) {
    form.items.push({
      local_id: makeLocalId(),
      target_type: item.target_type as TargetType,
      target_id: item.target_id,
      pricing_mode:
        item.target_type === 'membership' && item.pricing_mode === 'package'
          ? 'package'
          : 'discount',
      package_months:
        item.target_type === 'membership' && item.pricing_mode === 'package'
          ? Math.max(1, item.package_months || 1)
          : 1,
      discount_zhe: normalizeDiscountBps(item.discount_bps) / 1000,
      quantity: Math.max(1, item.quantity || 1),
    })
  }
  if (!form.items.length) addTarget('membership')
  editorOpen.value = true
  error.value = ''
  notice.value = ''
}

async function load() {
  loading.value = true
  error.value = ''
  const results = await Promise.allSettled([
    getCommercialMarketingCampaigns(),
    getCommercialMemberships(),
    getCommercialTimeCards(),
    getCommercialDeviceProducts(),
  ])
  if (results[0].status === 'fulfilled') campaigns.value = results[0].value.items
  else error.value = '读取营销计划失败'
  if (results[1].status === 'fulfilled') memberships.value = results[1].value.items
  if (results[2].status === 'fulfilled') timeCards.value = results[2].value.items
  if (results[3].status === 'fulfilled') devices.value = results[3].value.items
  loading.value = false
}

async function save() {
  if (saving.value) return
  error.value = ''
  if (form.name.trim().length < 2) {
    error.value = '请填写营销计划名称。'
    return
  }
  if (!form.display_locations.length) {
    error.value = '请选择至少一个展示场地；不需要前台展示时请选择“仅后台”。'
    return
  }
  if (!form.items.length) {
    error.value = '至少添加 1 个营销标的。'
    return
  }
  for (const item of form.items) {
    if (!item.target_id) {
      error.value = '每个营销标的都必须选择具体商品。'
      return
    }
    if (item.discount_zhe < 0 || item.discount_zhe > 10) {
      error.value = '折扣范围为 0～10 折；0 折表示赠送。'
      return
    }
    if (!Number.isFinite(item.quantity) || item.quantity < 1) {
      error.value = '数量必须大于等于 1。'
      return
    }
    if (
      item.target_type === 'membership' &&
      item.pricing_mode === 'package' &&
      (!Number.isFinite(item.package_months) || item.package_months < 1 || item.package_months > 120)
    ) {
      error.value = '会员周期套餐月数必须在 1～120 个月之间。'
      return
    }
  }

  const startsAt = toISODateTime(form.starts_at)
  const endsAt = toISODateTime(form.ends_at)
  if (form.starts_at && !startsAt) {
    error.value = '活动开始时间不正确。'
    return
  }
  if (form.ends_at && !endsAt) {
    error.value = '活动结束时间不正确。'
    return
  }
  if (startsAt && endsAt && new Date(endsAt).getTime() <= new Date(startsAt).getTime()) {
    error.value = '活动结束时间必须晚于开始时间。'
    return
  }

  saving.value = true
  const currentPlan = editingId.value
    ? plans.value.find((item) => item.id === editingId.value)
    : undefined
  const input = {
    code: currentPlan?.code || makeCode(),
    name: form.name.trim(),
    description: form.description.trim(),
    status: form.status,
    sort_order: Math.round(form.sort_order || 0),
    pricing_rule: 'floor_yuan',
    starts_at: startsAt,
    ends_at: endsAt,
    display_locations: [...form.display_locations],
    items: form.items.map(itemFromForm),
  }

  try {
    if (editingId.value) {
      await updateCommercialMarketingCampaign(editingId.value, input)
    } else {
      await createCommercialMarketingCampaign(input)
    }
    notice.value = editingId.value ? '营销计划已更新。' : '营销计划已建立，可以继续挂更多标的。'
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存营销计划失败'
  } finally {
    saving.value = false
  }
}

async function toggleStatus(plan: MarketingPlanView) {
  const next = plan.status === 'active' ? 'inactive' : 'active'
  await updateCommercialMarketingCampaign(plan.id, {
    code: plan.code,
    name: plan.name,
    description: plan.description,
    status: next,
    sort_order: plan.sort_order,
    pricing_rule: plan.pricing_rule || 'floor_yuan',
    starts_at: plan.starts_at || '',
    ends_at: plan.ends_at || '',
    display_locations: normalizeDisplayLocations(plan.display_locations),
    items: plan.items,
  })
  notice.value = next === 'active' ? '营销计划已启用。' : '营销计划已停用。'
  await load()
}

async function removePlan(plan: MarketingPlanView) {
  const confirmed = await confirmAction({
    title: '删除营销计划',
    message: '确认删除“' + plan.name + '”？计划内所有标的会同时退出该营销活动。',
    confirmText: '确认删除',
  })
  if (!confirmed) return
  await deleteCommercialMarketingCampaign(plan.id)
  notice.value = '营销计划已删除。'
  await load()
}

onMounted(load)
</script>

<template>
  <div class="management-page marketing-design-page">
    <ModulePageNav
      :context="navContext"
      active-title="营销活动"
      active-nav-title="营销活动"
    />

    <section class="marketing-hero">
      <div>
        <p class="section-kicker">ACTIVITY MARKETING · PLAN</p>
        <h2>营销活动</h2>
        <p>
          先建立营销计划，再往计划中添加多个会员、时长卡或设备商品。每个标的独立设置折扣，
          0 折就是赠送；系统自动汇总原始价值、活动实际价值和优惠金额。
        </p>
      </div>
      <div class="marketing-hero-actions">
        <button class="ghost-button" type="button" :disabled="loading" @click="load">刷新</button>
        <button v-if="canManage" class="primary-button" type="button" @click="openCreate">＋ 新建营销计划</button>
      </div>
    </section>

    <div v-if="error" class="inline-error">{{ error }}</div>
    <div v-if="notice" class="settings-success">{{ notice }}</div>

    <section class="marketing-workspace">
      <header>
        <div>
          <span class="section-kicker">MARKETING PLANS</span>
          <h3>营销计划</h3>
        </div>
        <div class="marketing-filters">
          <input v-model="search" type="search" placeholder="计划名称 / 标的名称" />
          <select v-model="targetFilter">
            <option value="all">全部标的</option>
            <option value="membership">会员方案</option>
            <option value="time_card">时长卡</option>
            <option value="device_product">设备商品</option>
          </select>
          <select v-model="placementFilter">
            <option value="all">全部展示场地</option>
            <option value="shop">终端商城</option>
            <option value="membership">会员中心</option>
            <option value="backoffice">仅后台</option>
          </select>
        </div>
      </header>

      <div v-if="loading" class="panel-loading">正在读取营销计划...</div>
      <div v-else-if="filteredPlans.length" class="marketing-card-grid">
        <article
          v-for="plan in filteredPlans"
          :key="plan.id"
          class="marketing-plan-card"
          :class="{ collapsed: !isPlanOpen(plan.id) }"
        >
          <header class="marketing-plan-head" @click="togglePlan(plan.id)">
            <div>
              <span class="marketing-plan-kicker">MARKETING PLAN · {{ plan.items.length }} 个标的</span>
              <h4>{{ plan.name }}</h4>
              <p>{{ plan.description || '暂无活动说明' }}</p>
              <div class="marketing-plan-window">
                <span>开始 {{ formatCampaignTime(plan.starts_at) }}</span>
                <span>结束 {{ formatCampaignTime(plan.ends_at) }}</span>
              </div>
              <div class="marketing-plan-placements">
                <span>展示：</span>
                <b
                  v-for="location in normalizeDisplayLocations(plan.display_locations)"
                  :key="location"
                  :class="{ backoffice: location === 'backoffice' }"
                >
                  {{ displayLocationLabel(location) }}
                </b>
              </div>
            </div>
            <div class="marketing-plan-head-side">
              <span class="status-pill" :class="{ active: campaignRuntimeStatus(plan) === 'active' }">
                {{ campaignRuntimeStatusLabel(plan) }}
              </span>
              <span class="marketing-plan-chevron" :class="{ open: isPlanOpen(plan.id) }">⌄</span>
            </div>
          </header>

          <div v-show="isPlanOpen(plan.id)" class="marketing-plan-body">
            <div class="marketing-plan-items">
              <article
                v-for="(item, index) in plan.items"
                :key="plan.id + '-' + index"
                class="marketing-plan-item"
                :class="{ expanded: isTargetOpen(plan.id, index) }"
              >
                <button
                  type="button"
                  class="marketing-item-summary"
                  @click.stop="toggleTarget(plan.id, index)"
                >
                  <div class="marketing-item-main">
                    <span class="marketing-target-type">{{ targetTypeLabel(item.target_type) }}</span>
                    <strong>{{ targetName(item.target_type, item.target_id) }}</strong>
                  </div>

                  <span class="marketing-item-chevron" :class="{ open: isTargetOpen(plan.id, index) }">⌄</span>
                </button>

                <div v-show="isTargetOpen(plan.id, index)" class="marketing-item-detail">
                  <div class="marketing-item-price-grid">
                    <div>
                      <span>正常价格</span>
                      <strong>{{ formatMoney(basePriceCents(item)) }}</strong>
                    </div>
                    <div>
                      <span>活动打折</span>
                      <strong class="discount" :class="{ gift: item.discount_bps === 0 }">
                        {{ formatDiscount(item.discount_bps) }}
                      </strong>
                    </div>
                    <div>
                      <span>折后价格</span>
                      <strong class="payable">
                        {{ item.discount_bps === 0 ? '赠送' : formatMoney(payableCents(item)) }}
                      </strong>
                    </div>
                    <div>
                      <span>优惠多少</span>
                      <strong class="saving">
                        {{ formatMoney(Math.max(0, basePriceCents(item) - payableCents(item))) }}
                      </strong>
                    </div>
                  </div>
                  <div class="marketing-item-calculation">
                    <span>价格计算</span>
                    <strong>{{ itemCalculationText(item) }}</strong>
                  </div>
                  <div class="marketing-item-detail-tags">
                    <span>数量 {{ Math.max(1, item.quantity || 1) }}</span>
                    <span v-if="item.target_type === 'membership'">周期 {{ packageLabel(item) }}</span>
                    <span v-if="itemHours(item) > 0">权益 {{ itemHours(item).toFixed(1) }} 小时</span>
                    <span v-if="hourlyPrice(item)">折后 {{ hourlyPrice(item) }}</span>
                  </div>
                </div>
              </article>
            </div>

            <div class="marketing-value-summary">
              <div>
                <span>总价值原价</span>
                <strong>{{ formatMoney(planBaseCents(plan)) }}</strong>
              </div>
              <div>
                <span>活动后价值</span>
                <strong class="actual">{{ formatMoney(planPayableCents(plan)) }}</strong>
              </div>
              <div>
                <span>总优惠</span>
                <strong class="saving">{{ formatMoney(planSavedCents(plan)) }}</strong>
              </div>
            </div>
          </div>

          <footer v-if="canManage">
            <button class="ghost-button" type="button" @click="openEdit(plan)">编辑计划</button>
            <button class="ghost-button" type="button" @click="toggleStatus(plan)">
              {{ plan.status === 'active' ? '停用' : '启用' }}
            </button>
            <button class="ghost-button danger" type="button" @click="removePlan(plan)">删除</button>
          </footer>
        </article>
      </div>

      <div v-else class="commercial-empty-state">
        <strong>还没有营销计划</strong>
        <span>建立一个计划，然后添加多个会员、时长卡或设备商品作为营销内容。</span>
      </div>
    </section>

    <Teleport to="body">
      <div v-if="editorOpen && canManage" class="modal-backdrop" @click.self="editorOpen = false">
        <form class="modal-card marketing-editor-modal" @submit.prevent="save">
          <div class="modal-header">
            <div>
              <span class="section-kicker">MARKETING PLAN</span>
              <h3>{{ editingId ? '编辑营销计划' : '新建营销计划' }}</h3>
            </div>
            <button class="close-button" type="button" @click="editorOpen = false">×</button>
          </div>

          <div class="marketing-editor-body">
            <div class="marketing-editor-grid">
              <label>
                <span>计划名称 *</span>
                <input v-model="form.name" type="text" maxlength="160" placeholder="例如：新商家开播组合套餐" />
              </label>
              <label>
                <span>计划状态</span>
                <select v-model="form.status">
                  <option value="active">启用</option>
                  <option value="inactive">停用</option>
                </select>
              </label>
              <div class="marketing-editor-full marketing-placement-field">
                <span>展示场地 *</span>
                <div class="marketing-placement-options">
                  <button
                    v-for="option in displayLocationOptions"
                    :key="option.code"
                    type="button"
                    :class="{ active: form.display_locations.includes(option.code) }"
                    @click="toggleDisplayLocation(option.code)"
                  >
                    <strong>{{ option.label }}</strong>
                    <small>{{ option.description }}</small>
                  </button>
                </div>
                <small class="marketing-placement-help">
                  终端商城和会员中心可以同时选择；“仅后台”与前台展示场地互斥。以后新增展示位置只扩展这里，不改活动价格结构。
                </small>
              </div>

              <label>
                <span>活动开始时间</span>
                <input v-model="form.starts_at" type="datetime-local" />
              </label>
              <label>
                <span>活动结束时间</span>
                <input v-model="form.ends_at" type="datetime-local" />
              </label>
              <label class="marketing-editor-full">
                <span>营销说明</span>
                <textarea v-model="form.description" rows="2" maxlength="600" placeholder="前端营销卡片会展示这段内容"></textarea>
              </label>
              <label>
                <span>显示排序</span>
                <input v-model.number="form.sort_order" type="number" step="1" />
              </label>
            </div>

            <section class="marketing-target-editor">
              <header>
                <div>
                  <strong>营销标的</strong>
                  <span>一个计划可放多个不同商品，每个标的单独定价。</span>
                </div>
                <button class="primary-button" type="button" @click="addTarget()">＋ 添加标的</button>
              </header>

              <div class="marketing-target-editor-list">
                <article v-for="(item, index) in form.items" :key="item.local_id" class="marketing-target-editor-card">
                  <div class="marketing-target-number">{{ index + 1 }}</div>
                  <div class="marketing-target-fields">
                    <label>
                      <span>标的类型</span>
                      <select v-model="item.target_type" @change="changeTargetType(item)">
                        <option value="membership">会员方案</option>
                        <option value="time_card">时长卡</option>
                        <option value="device_product">设备商品</option>
                      </select>
                    </label>

                    <label>
                      <span>具体标的</span>
                      <select v-model.number="item.target_id">
                        <option :value="0">请选择</option>
                        <option v-for="option in targetOptions(item.target_type)" :key="option.id" :value="option.id">
                          {{ option.name }}
                        </option>
                      </select>
                    </label>

                    <label v-if="item.target_type === 'membership'">
                      <span>营销方式</span>
                      <select v-model="item.pricing_mode">
                        <option value="discount">普通折扣</option>
                        <option value="package">周期套餐</option>
                      </select>
                    </label>

                    <label
                      v-if="item.target_type === 'membership' && item.pricing_mode === 'package'"
                      class="membership-package-field"
                    >
                      <span>套餐周期 *</span>
                      <div class="membership-package-input">
                        <input
                          v-model.number="item.package_months"
                          type="number"
                          min="1"
                          max="120"
                          step="1"
                        />
                        <span>个月</span>
                      </div>
                      <div class="membership-package-quick">
                        <button
                          v-for="option in membershipPackageOptions"
                          :key="option.months"
                          type="button"
                          :class="{ active: item.package_months === option.months }"
                          @click="setMembershipPackageMonths(item, option.months)"
                        >
                          {{ option.label }}
                        </button>
                      </div>
                    </label>

                    <label>
                      <span>折扣 *</span>
                      <div class="input-with-suffix">
                        <input v-model.number="item.discount_zhe" type="number" min="0" max="10" step="0.1" />
                        <span>折</span>
                      </div>
                      <small class="marketing-gift-tip">0 折 = 赠送</small>
                    </label>

                    <label>
                      <span>数量</span>
                      <input v-model.number="item.quantity" type="number" min="1" step="1" />
                    </label>
                  </div>

                  <div class="marketing-target-preview">
                    <span>原值 {{ formatMoney(basePriceCents(itemFromForm(item))) }}</span>
                    <strong>
                      {{ item.discount_zhe === 0 ? '赠送' : '实付 ' + formatMoney(payableCents(itemFromForm(item))) }}
                    </strong>
                    <small v-if="hourlyPrice(itemFromForm(item))">
                      {{ itemHours(itemFromForm(item)).toFixed(1) }} 小时 · {{ hourlyPrice(itemFromForm(item)) }}
                    </small>
                  </div>

                  <button class="marketing-remove-target" type="button" title="移除标的" @click="removeTarget(index)">×</button>
                </article>
              </div>
            </section>

            <section class="marketing-editor-total">
              <div><span>计划商品总价值</span><strong>{{ formatMoney(formBaseCents) }}</strong></div>
              <div><span>活动实际价值</span><strong>{{ formatMoney(formPayableCents) }}</strong></div>
              <div><span>优惠 / 赠送价值</span><strong>{{ formatMoney(Math.max(0, formBaseCents - formPayableCents)) }}</strong></div>
            </section>
          </div>

          <div class="modal-actions marketing-editor-actions">
            <div class="marketing-editor-actions-summary">
              <span>当前 {{ form.items.length }} 个标的</span>
              <strong>活动后价值 {{ formatMoney(formPayableCents) }}</strong>
            </div>
            <button class="ghost-button" type="button" :disabled="saving" @click="editorOpen = false">取消</button>
            <button class="primary-button" type="submit" :disabled="saving">{{ saving ? '保存中...' : '保存营销计划' }}</button>
          </div>
        </form>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.marketing-design-page { display: grid; gap: 16px; }
.marketing-hero {
  display: flex; align-items: center; justify-content: space-between; gap: 20px;
  padding: 22px 24px; border: 1px solid #dde4f2; border-radius: 18px;
  background: radial-gradient(circle at 92% 12%, rgba(91,95,220,.14), transparent 31%), #fff;
}
.marketing-hero h2 { margin: 4px 0 6px; font-size: 25px; }
.marketing-hero p:last-child { max-width: 820px; margin: 0; color: #68758a; font-size: 13px; line-height: 1.7; }
.marketing-hero-actions { display: flex; gap: 9px; }
.marketing-summary { display: grid; grid-template-columns: repeat(3,minmax(0,1fr)); gap: 12px; }
.marketing-summary article { display:grid; gap:4px; padding:16px 18px; border:1px solid #e3e8f1; border-radius:14px; background:#fff; }
.marketing-summary span,.marketing-summary small { color:#758197; font-size:12px; }
.marketing-summary strong { color:#202a3b; font-size:26px; }
.marketing-workspace { overflow:hidden; border:1px solid #e1e6ef; border-radius:18px; background:#fff; }
.marketing-workspace > header { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:16px 18px; border-bottom:1px solid #edf0f5; }
.marketing-workspace h3 { margin:3px 0 0; }
.marketing-filters { display:flex; gap:8px; }
.marketing-filters input,.marketing-filters select,.marketing-editor-grid input,.marketing-editor-grid select,.marketing-editor-grid textarea,
.marketing-target-fields input,.marketing-target-fields select {
  min-height:42px; border:1px solid #dfe5ee; border-radius:10px; padding:8px 11px; color:#303a4c; background:#fbfcfe; font:inherit;
}
.marketing-editor-grid textarea { resize: vertical; }
.marketing-filters input { width:240px; }
.marketing-card-grid {
  display:grid;
  grid-template-columns:repeat(auto-fill,minmax(350px,390px));
  align-items:start;
  justify-content:start;
  gap:16px;
  padding:18px;
}
.marketing-plan-card {
  display:grid;
  gap:12px;
  min-width:0;
  padding:16px;
  border:1px solid #d6dff0;
  border-radius:17px;
  background:
    radial-gradient(circle at 100% 0, rgba(92,108,232,.13), transparent 34%),
    linear-gradient(145deg,#f7f9ff,#eef3fb);
  box-shadow:0 8px 24px rgba(51,65,111,.07);
  transition:
    transform .2s ease,
    box-shadow .2s ease,
    border-color .2s ease;
}
.marketing-plan-card:hover {
  transform:translateY(-5px);
  border-color:#7d91ff;
  box-shadow:
    0 18px 38px rgba(56,77,175,.16),
    0 0 0 2px rgba(86,106,255,.10),
    0 0 28px rgba(74,101,255,.16);
}
.marketing-plan-card.collapsed {
  gap:7px;
}
.marketing-plan-head {
  display:flex;
  align-items:flex-start;
  justify-content:space-between;
  gap:12px;
  padding:2px 1px 3px;
  cursor:pointer;
  user-select:none;
}
.marketing-plan-head h4 {
  margin:5px 0 4px;
  color:#17233a;
  font-size:22px;
  font-weight:900;
  letter-spacing:.01em;
  text-transform:uppercase;
}
.marketing-plan-head p {
  margin:0;
  color:#66748a;
  font-size:13px;
  font-weight:600;
  line-height:1.6;
}
.marketing-plan-kicker {
  color:#5368d8;
  font-size:12px;
  font-weight:900;
  letter-spacing:.10em;
}
.marketing-plan-window {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 12px;
  margin-top: 8px;
  color: #748097;
  font-size: 11px;
  font-weight: 700;
}

.marketing-plan-window span {
  padding: 4px 7px;
  border-radius: 7px;
  background: rgba(82, 103, 203, .07);
}

.marketing-plan-placements {
  display:flex;
  align-items:center;
  flex-wrap:wrap;
  gap:6px;
  margin-top:7px;
  color:#7a8699;
  font-size:11px;
  font-weight:750;
}
.marketing-plan-placements b {
  padding:4px 8px;
  border:1px solid rgba(74,96,210,.18);
  border-radius:999px;
  color:#5065ce;
  background:rgba(92,112,224,.08);
  font-size:11px;
}
.marketing-plan-placements b.backoffice {
  color:#6e7686;
  border-color:#d8dde7;
  background:#f0f2f6;
}

.marketing-plan-head-side {
  display:grid;
  justify-items:end;
  gap:8px;
}
.marketing-plan-chevron,
.marketing-item-chevron {
  display:inline-flex;
  align-items:center;
  justify-content:center;
  color:#68758e;
  font-size:19px;
  font-weight:900;
  transition:transform .18s ease,color .18s ease;
}
.marketing-plan-chevron.open,
.marketing-item-chevron.open {
  transform:rotate(180deg);
  color:#5267d8;
}
.marketing-plan-body {
  display:grid;
  gap:12px;
}
.marketing-value-summary {
  display:grid;
  grid-template-columns:repeat(3,minmax(0,1fr));
  gap:8px;
  padding-top:3px;
}
.marketing-value-summary > div {
  display:grid;
  gap:5px;
  padding:11px 10px;
  border:1px solid #dce4f3;
  border-radius:11px;
  background:rgba(255,255,255,.72);
}
.marketing-value-summary span {
  color:#68768c;
  font-size:12px;
  font-weight:700;
}
.marketing-value-summary strong {
  color:#243047;
  font-size:17px;
  font-weight:900;
}
.marketing-value-summary .actual { color:#5a5fd3; }
.marketing-value-summary .saving { color:#dc2626; }
.marketing-plan-items {
  display:grid;
  gap:9px;
}
.marketing-plan-item {
  overflow:hidden;
  border:1px solid #dce3ef;
  border-radius:12px;
  background:rgba(255,255,255,.78);
  transition:border-color .16s ease,box-shadow .16s ease,background .16s ease;
}
.marketing-plan-item:hover,
.marketing-plan-item.expanded {
  border-color:#a8b6ff;
  background:#fff;
  box-shadow:0 7px 18px rgba(65,79,155,.08);
}
.marketing-item-summary {
  display:grid;
  grid-template-columns:minmax(0,1fr) 18px;
  gap:10px;
  width:100%;
  padding:12px;
  border:0;
  color:inherit;
  background:transparent;
  text-align:left;
  cursor:pointer;
}
.marketing-item-main {
  display:grid;
  gap:3px;
  min-width:0;
}
.marketing-item-main strong {
  overflow:hidden;
  color:#1f2b40;
  font-size:17px;
  font-weight:900;
  text-overflow:ellipsis;
  white-space:nowrap;
}
.marketing-item-main small {
  color:#65738a;
  font-size:12px;
  font-weight:700;
}
.marketing-target-type {
  color:#5267d8;
  font-size:12px;
  font-weight:900;
  letter-spacing:.05em;
}
.marketing-item-price-grid {
  grid-column:1 / -1;
  display:grid;
  grid-template-columns:repeat(2,minmax(0,1fr));
  gap:7px;
}
.marketing-item-price-grid > div {
  display:grid;
  gap:3px;
  padding:8px 9px;
  border-radius:9px;
  background:#f3f6fb;
}
.marketing-item-price-grid span {
  color:#738096;
  font-size:12px;
  font-weight:700;
}
.marketing-item-price-grid strong {
  color:#283449;
  font-size:15px;
  font-weight:900;
}
.marketing-item-price-grid strong.discount {
  color:#5359c5;
}
.marketing-item-price-grid strong.discount.gift,
.marketing-item-price-grid strong.payable,
.marketing-item-price-grid strong.saving {
  color:#dc2626;
}
.marketing-item-chevron {
  grid-column:2;
  grid-row:1;
  align-self:center;
}
.marketing-item-detail {
  display:grid;
  gap:9px;
  padding:0 12px 12px;
  border-top:1px dashed #d9e1ee;
  animation:marketing-detail-in .16s ease;
}
.marketing-item-detail > .marketing-item-price-grid {
  padding-top:10px;
}
.marketing-item-calculation {
  display:grid;
  gap:4px;
  padding-top:10px;
}
.marketing-item-calculation span {
  color:#65738a;
  font-size:12px;
  font-weight:800;
}
.marketing-item-calculation strong {
  color:#283449;
  font-size:14px;
  line-height:1.55;
}
.marketing-item-detail-tags {
  display:flex;
  flex-wrap:wrap;
  gap:6px;
}
.marketing-item-detail-tags span {
  padding:5px 8px;
  border:1px solid #dce3f7;
  border-radius:999px;
  color:#5b6780;
  background:#f5f7ff;
  font-size:12px;
  font-weight:750;
}
@keyframes marketing-detail-in {
  from { opacity:0; transform:translateY(-4px); }
  to { opacity:1; transform:translateY(0); }
}
.marketing-plan-card footer { display:flex; gap:7px; padding-top:2px; }
.marketing-plan-card footer .danger { color:#b94747; }
.marketing-editor-modal {
  display:flex;
  width:min(1080px,calc(100vw - 44px));
  height:min(880px,calc(100dvh - 44px));
  max-height:calc(100dvh - 44px);
  flex-direction:column;
  padding:0;
  overflow:hidden;
  border:1px solid #d9e1f0;
  border-radius:20px;
  background:#f8faff;
  box-shadow:
    0 30px 90px rgba(20,31,57,.28),
    0 0 0 1px rgba(255,255,255,.75) inset;
}
.marketing-editor-modal > .modal-header {
  flex:0 0 auto;
  margin:0;
  padding:17px 22px 15px;
  border-bottom:1px solid #e6ebf4;
  background:
    radial-gradient(circle at 92% 0, rgba(88,106,232,.11), transparent 33%),
    #fff;
}
.marketing-editor-modal > .modal-header h3 {
  font-size:calc(21px + var(--ui-font-delta));
}
.marketing-editor-body {
  min-height:0;
  flex:1 1 auto;
  max-height:none;
  overflow-y:auto;
  overscroll-behavior:contain;
  padding-bottom:8px;
  scrollbar-width:thin;
  scrollbar-color:rgba(91,105,143,.42) transparent;
}
.marketing-editor-grid {
  display:grid;
  grid-template-columns:repeat(2,minmax(0,1fr));
  gap:10px 14px;
  padding:15px 20px 10px;
}
.marketing-editor-grid label,.marketing-target-fields label { display:grid; gap:5px; }
.marketing-editor-grid label > span,.marketing-target-fields label > span { color:#536077; font-size:12px; font-weight:850; }
.marketing-editor-full { grid-column:1/-1; }
.marketing-placement-field {
  display:grid;
  gap:7px;
}
.marketing-placement-field > span {
  color:#536077;
  font-size:12px;
  font-weight:850;
}
.marketing-placement-options {
  display:grid;
  grid-template-columns:repeat(3,minmax(0,1fr));
  gap:9px;
}
.marketing-placement-options button {
  display:grid;
  gap:5px;
  min-height:74px;
  padding:11px 12px;
  border:1px solid #dce3ef;
  border-radius:12px;
  text-align:left;
  color:#4d596e;
  background:#f8fafd;
  cursor:pointer;
  transition:border-color .18s ease, box-shadow .18s ease, background .18s ease;
}
.marketing-placement-options button strong {
  color:#27334a;
  font-size:13px;
}
.marketing-placement-options button small {
  color:#7b8799;
  font-size:11px;
  line-height:1.5;
}
.marketing-placement-options button.active {
  border-color:#7d91ff;
  background:#f1f4ff;
  box-shadow:0 0 0 2px rgba(91,111,238,.1), 0 8px 20px rgba(70,89,180,.09);
}
.marketing-placement-options button.active strong {
  color:#405bd6;
}
.marketing-placement-help {
  color:#8791a2;
  font-size:11px;
  line-height:1.55;
}
.marketing-target-editor {
  margin:0 20px 12px;
  overflow:hidden;
  border:1px solid #dce3f0;
  border-radius:14px;
  background:#f4f7fc;
}
.marketing-target-editor > header {
  position:sticky;
  top:0;
  z-index:2;
  display:flex;
  align-items:center;
  justify-content:space-between;
  gap:12px;
  padding:10px 13px;
  border-bottom:1px solid #e1e7f1;
  background:rgba(255,255,255,.96);
  backdrop-filter:blur(10px);
}
.marketing-target-editor > header > div { display:grid; gap:2px; }
.marketing-target-editor > header strong { color:#263249; font-size:15px; }
.marketing-target-editor > header span { color:#6f7b90; font-size:12px; }
.marketing-target-editor > header .primary-button {
  min-height:38px;
  padding:0 14px;
  border-radius:10px;
  font-size:13px;
}
.marketing-target-editor-list {
  display:grid;
  gap:8px;
  padding:9px;
}
.marketing-target-editor-card {
  position:relative;
  display:grid;
  grid-template-columns:26px minmax(0,1fr) 145px 30px;
  align-items:center;
  gap:9px;
  padding:9px;
  border:1px solid #e0e6f0;
  border-radius:11px;
  background:#fff;
  box-shadow:0 3px 10px rgba(35,48,78,.025);
}
.marketing-target-number {
  display:flex;
  width:26px;
  height:26px;
  align-items:center;
  justify-content:center;
  border-radius:8px;
  color:#565bc4;
  background:#eff0ff;
  font-size:12px;
  font-weight:850;
}
.marketing-target-fields {
  display:grid;
  grid-template-columns:1fr 1.35fr .9fr .95fr .76fr .58fr;
  gap:7px;
  align-items:start;
}
.marketing-target-fields input,
.marketing-target-fields select {
  min-height:38px;
  padding:7px 9px;
  font-size:14px;
}
.marketing-target-preview {
  display:grid;
  gap:3px;
  padding:8px 9px;
  border:1px solid #e5e9f2;
  border-radius:9px;
  background:#f5f7fb;
}
.marketing-target-preview span,.marketing-target-preview small { color:#6e7a90; font-size:11px; }
.marketing-target-preview strong { color:#d92d20; font-size:13px; }
.marketing-gift-tip { color:#e02424; font-size:10px; font-weight:800; }
.membership-package-field { min-width:170px; }
.membership-package-input { display:flex; align-items:center; gap:6px; }
.membership-package-input input { min-width:0; flex:1; }
.membership-package-input > span { color:#7b8799; font-size:11px; white-space:nowrap; }
.membership-package-quick { display:flex; flex-wrap:wrap; gap:4px; }
.membership-package-quick button {
  min-height:24px;
  padding:3px 7px;
  border:1px solid #dfe4ef;
  border-radius:7px;
  background:#fff;
  color:#687389;
  font-size:10px;
  cursor:pointer;
}
.membership-package-quick button.active {
  border-color:#7788ef;
  background:#eef1ff;
  color:#465bc8;
  font-weight:850;
}
.marketing-remove-target { width:28px; height:28px; border:1px solid #ebdada; border-radius:8px; color:#b94747; background:#fffafa; cursor:pointer; }
.marketing-editor-total { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); gap:9px; margin:0 20px 10px; padding:10px 12px; border:1px solid #d9defb; border-radius:12px; background:#f0f3ff; }
.marketing-editor-total > div { display:grid; gap:3px; }
.marketing-editor-total span { color:#66738a; font-size:12px; font-weight:700; }
.marketing-editor-total strong { color:#4449b6; font-size:17px; }
.marketing-editor-actions {
  flex:0 0 auto;
  display:grid;
  grid-template-columns:minmax(0,1fr) auto auto;
  align-items:center;
  gap:10px;
  min-height:68px;
  margin:0;
  padding:11px 20px;
  border-top:1px solid #dfe6f1;
  background:rgba(255,255,255,.98);
  box-shadow:0 -10px 28px rgba(30,42,70,.07);
  backdrop-filter:blur(12px);
}
.marketing-editor-actions-summary {
  display:grid;
  gap:2px;
  min-width:0;
}
.marketing-editor-actions-summary span {
  color:#748096;
  font-size:11px;
  font-weight:700;
}
.marketing-editor-actions-summary strong {
  color:#3f4ba8;
  font-size:14px;
}
.marketing-editor-actions > .ghost-button,
.marketing-editor-actions > .primary-button {
  min-width:100px;
  min-height:42px;
  padding:0 17px;
  border-radius:10px;
  font-size:14px;
  font-weight:850;
}
@media (max-width:900px) {
  .marketing-editor-modal {
    width:min(100%,calc(100vw - 28px));
    height:calc(100dvh - 28px);
    max-height:calc(100dvh - 28px);
  }
  .marketing-target-editor-card { grid-template-columns:28px 1fr 30px; }
  .marketing-target-preview { grid-column:2; }
  .marketing-target-fields { grid-template-columns:repeat(2,minmax(0,1fr)); }
}
@media (max-width:760px) {
  .marketing-hero,.marketing-workspace > header { align-items:stretch; flex-direction:column; }
  .marketing-summary,.marketing-editor-grid,.marketing-editor-total { grid-template-columns:1fr; }
  .marketing-editor-full { grid-column:1; }
  .marketing-placement-options { grid-template-columns:1fr; }
  .marketing-filters { flex-direction:column; }
  .marketing-filters input { width:100%; }
  .marketing-card-grid { grid-template-columns:1fr; }
  .marketing-item-price-grid { grid-template-columns:1fr; }
  .marketing-value-summary { grid-template-columns:1fr; }
  .marketing-target-fields { grid-template-columns:1fr; }
  .marketing-editor-actions {
    grid-template-columns:1fr 1fr;
  }
  .marketing-editor-actions-summary {
    grid-column:1 / -1;
  }
}
</style>
