<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createCommercialTimeCard,
  deleteCommercialTimeCard,
  getCommercialTimeCards,
  publishCommercialTimeCard,
  saveCommercialTimeCardDraft,
  setCommercialTimeCardListing,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import {
  floorToWholeYuanCents,
  formatWholeYuanMoney,
  marketingPayableCents,
} from '../pricingRules'
import { session } from '../session'
import type {
  CommercialTimeCardInput,
  CommercialTimeCardProduct,
  MarketingCampaign,
  MarketingCampaignItem,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const publishingId = ref<number | null>(null)
const listingId = ref<number | null>(null)
const deletingId = ref<number | null>(null)
const error = useFeedbackErrorRef()
const items = ref<CommercialTimeCardProduct[]>([])
const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const navContext = computed(() => 'activityMarketing' as const)
const sortMode = ref('sort-asc')
const page = ref(1)
const pageSize = ref(12)
const secondaryViewMode = ref<'card' | 'table'>('card')
const secondarySearch = ref('')
const secondaryStatusFilter = ref<'unlisted' | 'draft' | 'archived'>('unlisted')
const secondarySortMode = ref('sort-asc')
const secondaryPage = ref(1)
const secondaryPageSize = ref(12)
const editorOpen = ref(false)
const editing = ref<CommercialTimeCardProduct | null>(null)

const form = reactive({
  code: '',
  name: '',
  description: '',
  sort_order: 0,
  price_yuan: 0,
  duration_hours: 10,
  validity_days: 365,
  activation_mode: 'first_use',
  activation_deadline_days: 0,
  participates_referral: true,
  participates_sales_commission: true,
  participates_agent_settlement: true,
})

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes('commercial.time_card.manage')),
  )
})

const secondaryStatusOptions = [
  { label: '已下架', value: 'unlisted' },
  { label: '有草稿', value: 'draft' },
  { label: '已归档', value: 'archived' },
]

const sortOptions = [
  { label: '默认排序', value: 'sort-asc' },
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '价格从高到低', value: 'price-desc' },
  { label: '时长从高到低', value: 'duration-desc' },
]

function displayVersion(item: CommercialTimeCardProduct, preferActive = false) {
  if (preferActive) {
    return item.active_version || item.latest_version || item.draft_version
  }
  return item.draft_version || item.active_version || item.latest_version
}

function marketingTargetItem(
  campaign: MarketingCampaign,
  productId: number,
): MarketingCampaignItem | undefined {
  const item = (campaign.items ?? []).find(
    (entry) => entry.target_type === 'time_card' && entry.target_id === productId,
  )
  if (item) return item
  if (campaign.target_type === 'time_card' && campaign.target_id === productId) {
    return {
      target_type: 'time_card',
      target_id: productId,
      pricing_mode: campaign.pricing_mode || 'discount',
      package_months: 1,
      discount_bps: campaign.discount_bps ?? 10000,
      quantity: 1,
    }
  }
  return undefined
}

function campaignDiscountLabel(campaign: MarketingCampaign, productId: number) {
  const bps = marketingTargetItem(campaign, productId)?.discount_bps ?? 10000
  if (bps <= 0) return '赠送'
  if (bps >= 10000) return '原价'
  const zhe = bps / 1000
  return zhe.toFixed(zhe % 1 === 0 ? 0 : 1) + ' 折'
}

function campaignActualValue(item: CommercialTimeCardProduct, campaign: MarketingCampaign) {
  const target = marketingTargetItem(campaign, item.id)
  if (!target) return 0
  const price = displayVersion(item)?.price_cents ?? 0
  return marketingPayableCents(
    floorToWholeYuanCents(price * Math.max(1, target.quantity || 1)),
    Math.max(0, target.discount_bps),
  )
}

function formatMarketingMoney(cents: number) {
  return formatWholeYuanMoney(cents)
}

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const version = displayVersion(item, true)
    const matchesKeyword =
      !keyword ||
      [item.code, item.name, item.description]
        .some((value) => String(value || '').toLowerCase().includes(keyword))
    return matchesKeyword && item.status === 'active' && Boolean(version)
  })

  return [...result].sort((a, b) => {
    const va = displayVersion(a, true)
    const vb = displayVersion(b, true)
    if (sortMode.value === 'name-asc') return a.name.localeCompare(b.name, 'zh-CN')
    if (sortMode.value === 'price-desc') return (vb?.price_cents || 0) - (va?.price_cents || 0)
    if (sortMode.value === 'duration-desc') return (vb?.duration_seconds || 0) - (va?.duration_seconds || 0)
    return a.sort_order - b.sort_order || a.id - b.id
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

const secondaryFilteredItems = computed(() => {
  const keyword = secondarySearch.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const version = displayVersion(item)
    const matchesKeyword =
      !keyword ||
      [item.code, item.name, item.description]
        .some((value) => String(value || '').toLowerCase().includes(keyword))
    const matchesStatus =
      (secondaryStatusFilter.value === 'unlisted' && item.status === 'inactive') ||
      (
        secondaryStatusFilter.value === 'draft' &&
        item.status !== 'archived' &&
        Boolean(item.draft_version)
      ) ||
      (secondaryStatusFilter.value === 'archived' && item.status === 'archived')
    return matchesKeyword && matchesStatus && Boolean(version)
  })

  return [...result].sort((a, b) => {
    const va = displayVersion(a)
    const vb = displayVersion(b)
    if (secondarySortMode.value === 'name-asc') return a.name.localeCompare(b.name, 'zh-CN')
    if (secondarySortMode.value === 'price-desc') return (vb?.price_cents || 0) - (va?.price_cents || 0)
    if (secondarySortMode.value === 'duration-desc') return (vb?.duration_seconds || 0) - (va?.duration_seconds || 0)
    return a.sort_order - b.sort_order || a.id - b.id
  })
})

const secondaryTotalPages = computed(() =>
  Math.max(1, Math.ceil(secondaryFilteredItems.value.length / secondaryPageSize.value)),
)

const secondaryPagedItems = computed(() => {
  const start = (secondaryPage.value - 1) * secondaryPageSize.value
  return secondaryFilteredItems.value.slice(start, start + secondaryPageSize.value)
})

const secondaryStatusLabel = computed(() =>
  secondaryStatusOptions.find((option) => option.value === secondaryStatusFilter.value)?.label || '其他状态',
)

watch([search, sortMode, pageSize], () => {
  page.value = 1
})

watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

watch([secondarySearch, secondaryStatusFilter, secondarySortMode, secondaryPageSize], () => {
  secondaryPage.value = 1
})

watch(secondaryTotalPages, (value) => {
  if (secondaryPage.value > value) secondaryPage.value = value
})

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toFixed(2)
}

function formatHours(seconds: number) {
  return (seconds / 3600).toLocaleString('zh-CN') + ' 小时'
}

function durationValue(seconds: number) {
  return (seconds / 3600).toLocaleString('zh-CN', { maximumFractionDigits: 2 })
}

function ruleSummary(item: CommercialTimeCardProduct, preferActive = false) {
  const version = displayVersion(item, preferActive)
  const labels: string[] = []
  if (version?.participates_referral) labels.push('推荐奖励')
  if (version?.participates_sales_commission) labels.push('销售提成')
  if (version?.participates_agent_settlement) labels.push('代理结算')
  return labels.length ? labels.join(' · ') : '不参与奖励 / 提成 / 代理结算'
}

function versionLabel(item: CommercialTimeCardProduct) {
  if (item.status === 'archived') return '已删除/归档'
  const listing = item.status === 'active' ? '已上架' : item.status === 'inactive' ? '已下架' : '未上架'
  if (item.draft_version && item.active_version) {
    return listing + ' · V' + item.active_version.version_no + ' · 草稿 V' + item.draft_version.version_no
  }
  if (item.draft_version) return listing + ' · 草稿 V' + item.draft_version.version_no
  if (item.active_version) return listing + ' · V' + item.active_version.version_no
  return listing
}

function resetForm() {
  form.code = ''
  form.name = ''
  form.description = ''
  form.sort_order = 0
  form.price_yuan = 0
  form.duration_hours = 10
  form.validity_days = 365
  form.activation_mode = 'first_use'
  form.activation_deadline_days = 0
  form.participates_referral = true
  form.participates_sales_commission = true
  form.participates_agent_settlement = true
}

function openCreate() {
  editing.value = null
  resetForm()
  editorOpen.value = true
  error.value = ''
}

function openEdit(item: CommercialTimeCardProduct) {
  editing.value = item
  const version = displayVersion(item)
  form.code = item.code
  form.name = item.name
  form.description = item.description
  form.sort_order = item.sort_order
  form.price_yuan = (version?.price_cents || 0) / 100
  form.duration_hours = (version?.duration_seconds || 36000) / 3600
  form.validity_days = version?.validity_days || 365
  form.activation_mode = version?.activation_mode || 'first_use'
  form.activation_deadline_days = version?.activation_deadline_days || 0
  form.participates_referral = version?.participates_referral ?? true
  form.participates_sales_commission = version?.participates_sales_commission ?? true
  form.participates_agent_settlement = version?.participates_agent_settlement ?? true
  editorOpen.value = true
  error.value = ''
}

function inputPayload(): CommercialTimeCardInput {
  return {
    code: form.code.trim(),
    name: form.name.trim(),
    description: form.description.trim(),
    sort_order: Number(form.sort_order || 0),
    price_cents: Math.round(Number(form.price_yuan || 0) * 100),
    duration_seconds: Math.round(Number(form.duration_hours || 0) * 3600),
    validity_days: Math.round(Number(form.validity_days || 0)),
    activation_mode: form.activation_mode,
    activation_deadline_days: Math.round(Number(form.activation_deadline_days || 0)),
    participates_referral: form.participates_referral,
    participates_sales_commission: form.participates_sales_commission,
    participates_agent_settlement: form.participates_agent_settlement,
  }
}

async function save() {
  saving.value = true
  error.value = ''
  try {
    const payload = inputPayload()
    if (editing.value) {
      await saveCommercialTimeCardDraft(editing.value.id, payload)
    } else {
      await createCommercialTimeCard(payload)
    }
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function publish(item: CommercialTimeCardProduct) {
  if (!item.draft_version) return
  if (!(await confirmAction({ title: '发布时长卡', message: '确认发布“' + item.name + '”的当前草稿？发布后终端商城会读取新版本。', confirmText: '确认发布' }))) return
  publishingId.value = item.id
  error.value = ''
  try {
    await publishCommercialTimeCard(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '发布失败'
  } finally {
    publishingId.value = null
  }
}

async function toggleListing(item: CommercialTimeCardProduct) {
  if (!canManage.value || listingId.value !== null || item.status === 'archived' || !item.active_version) return
  const nextStatus = item.status === 'active' ? 'inactive' : 'active'
  if (nextStatus === 'inactive') {
    const confirmed = await confirmAction({
      title: '下架时长卡',
      message: '下架“' + item.name + '”后终端商城将停止销售，历史订单和已到账时长不受影响。',
      confirmText: '确认下架',
    })
    if (!confirmed) return
  }
  listingId.value = item.id
  error.value = ''
  try {
    await setCommercialTimeCardListing(item.id, nextStatus)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '更新上下架状态失败'
  } finally {
    listingId.value = null
  }
}

async function archiveItem(item: CommercialTimeCardProduct) {
  if (!canManage.value || deletingId.value !== null) return
  const confirmed = await confirmAction({
    title: '删除时长卡',
    message: '确认删除“' + item.name + '”？系统会归档并停止销售，历史订单和版本记录继续保留。',
    confirmText: '确认删除',
  })
  if (!confirmed) return
  deletingId.value = item.id
  error.value = ''
  try {
    await deleteCommercialTimeCard(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '删除时长卡失败'
  } finally {
    deletingId.value = null
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getCommercialTimeCards()
    items.value = data.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取时长卡失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page commercial-time-card-page">
    <ModulePageNav :context="navContext" active-title="时长卡运营" active-nav-title="时长卡运营" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">TIME CARD CATALOG</p>
        <h2>时长卡运营</h2>
        <p>由营销运维部维护终端可购买的时长卡、价格、有效期和上下架；终端商城只读取已发布商品。</p>
      </div>
      <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
        ＋ 新建时长卡
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="时长卡名称 / 编码 / 说明"
        :sort-options="sortOptions"
      />

      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取时长卡...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时长卡</th>
              <th>价格</th>
              <th>时长</th>
              <th>激活后有效期</th>
              <th>版本状态</th>
              <th>参与规则</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.id">
              <td><strong>{{ item.name }}</strong></td>
              <td>{{ formatMoney(displayVersion(item, true)?.price_cents || 0) }}</td>
              <td>{{ formatHours(displayVersion(item, true)?.duration_seconds || 0) }}</td>
              <td>{{ displayVersion(item, true)?.validity_days || 0 }} 天</td>
              <td><span class="status-pill">{{ versionLabel(item) }}</span></td>
              <td>
                <small>
                  推荐 {{ displayVersion(item, true)?.participates_referral ? '✓' : '—' }}
                  · 销售 {{ displayVersion(item, true)?.participates_sales_commission ? '✓' : '—' }}
                  · 代理 {{ displayVersion(item, true)?.participates_agent_settlement ? '✓' : '—' }}
                </small>
              </td>
              <td v-if="canManage">
                <div class="table-actions">
                  <button class="text-action" type="button" @click="openEdit(item)">编辑</button>
                  <button
                    v-if="item.draft_version && item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="publishingId === item.id"
                    @click="publish(item)"
                  >
                    {{ publishingId === item.id ? '发布中...' : '发布新版本' }}
                  </button>
                  <button
                    v-if="item.active_version && item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="listingId === item.id"
                    @click="toggleListing(item)"
                  >
                    {{ listingId === item.id ? '处理中...' : (item.status === 'active' ? '下架' : '上架') }}
                  </button>
                  <button
                    v-if="item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="deletingId === item.id"
                    @click="archiveItem(item)"
                  >
                    {{ deletingId === item.id ? '删除中...' : '删除' }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredItems.length === 0" class="empty-state">暂无已上架时长卡。</div>
      </div>

      <div v-else class="commercial-time-card-grid">
        <article v-for="item in pagedItems" :key="item.id" class="commercial-time-card">
          <header class="commercial-time-card-head">
            <div>
              <span>激活后时效</span>
              <strong>{{ displayVersion(item, true)?.validity_days || 0 }} 天</strong>
            </div>
            <span class="commercial-time-card-status">{{ versionLabel(item) }}</span>
          </header>

          <div class="commercial-time-card-duration">
            <strong>{{ durationValue(displayVersion(item, true)?.duration_seconds || 0) }}</strong>
            <span>时</span>
          </div>

          <div class="commercial-time-card-name">{{ item.name }}</div>

          <div class="commercial-time-card-price">
            <span>价格</span>
            <strong>{{ formatMoney(displayVersion(item, true)?.price_cents || 0) }}</strong>
          </div>

          <div class="commercial-time-card-rules">
            <span>规则说明</span>
            <strong>{{ ruleSummary(item, true) }}</strong>
            <p>{{ item.description || '暂无额外说明' }}</p>
          </div>

          <div class="product-marketing-links">
            <span>参与营销计划 · {{ item.marketing_campaigns?.length || 0 }}</span>
            <div v-if="item.marketing_campaigns?.length" class="product-marketing-link-list">
              <div
                v-for="campaign in item.marketing_campaigns"
                :key="campaign.id"
                class="product-marketing-link"
                :class="{ inactive: campaign.status !== 'active' }"
              >
                <strong>{{ campaign.name }}</strong>
                <small>
                  {{ campaign.status === 'active' ? '生效中' : '已停用' }}
                  · {{ campaignDiscountLabel(campaign, item.id) }}
                  · 实际价值 {{ formatMarketingMoney(campaignActualValue(item, campaign)) }}
                </small>
              </div>
            </div>
            <small v-else class="product-marketing-empty">暂未参加营销计划</small>
          </div>

          <footer v-if="canManage" class="commercial-time-card-actions">
            <button v-if="item.status !== 'archived'" class="commercial-time-card-edit" type="button" @click="openEdit(item)">编辑</button>
            <button
              v-if="item.draft_version && item.status !== 'archived'"
              class="commercial-time-card-publish"
              type="button"
              :disabled="publishingId === item.id"
              @click="publish(item)"
            >
              {{ publishingId === item.id ? '发布中...' : '发布新版本' }}
            </button>
            <button
              v-if="item.active_version && item.status !== 'archived'"
              class="commercial-time-card-edit"
              type="button"
              :disabled="listingId === item.id"
              @click="toggleListing(item)"
            >
              {{ listingId === item.id ? '处理中...' : (item.status === 'active' ? '下架' : '上架') }}
            </button>
            <button v-if="item.status !== 'archived'" class="commercial-time-card-edit" type="button" @click="archiveItem(item)">删除</button>
          </footer>
        </article>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredItems.length"
        :page-size="pageSize"
      />
    </section>

    <section class="settings-card feature-workspace-panel time-card-secondary-panel">
      <div class="time-card-secondary-head">
        <div>
          <p class="section-kicker">OFF-SHELF & VERSION QUEUE</p>
          <h3>下架 / 草稿 / 归档</h3>
        </div>
        <span>这里集中处理未在终端商城销售的时长卡和待发布版本。</span>
      </div>

      <DataListControls
        v-model:view-mode="secondaryViewMode"
        v-model:search="secondarySearch"
        v-model:status="secondaryStatusFilter"
        v-model:sort="secondarySortMode"
        v-model:page-size="secondaryPageSize"
        search-placeholder="时长卡名称 / 编码 / 说明"
        :status-options="secondaryStatusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="loading" class="panel-loading">正在读取时长卡...</div>

      <div v-else-if="secondaryViewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时长卡</th>
              <th>价格</th>
              <th>时长</th>
              <th>激活后有效期</th>
              <th>版本状态</th>
              <th>参与规则</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in secondaryPagedItems" :key="item.id">
              <td><strong>{{ item.name }}</strong></td>
              <td>{{ formatMoney(displayVersion(item)?.price_cents || 0) }}</td>
              <td>{{ formatHours(displayVersion(item)?.duration_seconds || 0) }}</td>
              <td>{{ displayVersion(item)?.validity_days || 0 }} 天</td>
              <td><span class="status-pill">{{ versionLabel(item) }}</span></td>
              <td>
                <small>
                  推荐 {{ displayVersion(item)?.participates_referral ? '✓' : '—' }}
                  · 销售 {{ displayVersion(item)?.participates_sales_commission ? '✓' : '—' }}
                  · 代理 {{ displayVersion(item)?.participates_agent_settlement ? '✓' : '—' }}
                </small>
              </td>
              <td v-if="canManage">
                <div class="table-actions">
                  <button
                    v-if="item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    @click="openEdit(item)"
                  >
                    编辑
                  </button>
                  <button
                    v-if="item.draft_version && item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="publishingId === item.id"
                    @click="publish(item)"
                  >
                    {{ publishingId === item.id ? '发布中...' : '发布新版本' }}
                  </button>
                  <button
                    v-if="item.active_version && item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="listingId === item.id"
                    @click="toggleListing(item)"
                  >
                    {{ listingId === item.id ? '处理中...' : (item.status === 'active' ? '下架' : '上架') }}
                  </button>
                  <button
                    v-if="item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="deletingId === item.id"
                    @click="archiveItem(item)"
                  >
                    {{ deletingId === item.id ? '删除中...' : '删除' }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="secondaryFilteredItems.length === 0" class="empty-state">
          暂无{{ secondaryStatusLabel }}时长卡。
        </div>
      </div>

      <div v-else class="commercial-time-card-grid">
        <article v-for="item in secondaryPagedItems" :key="item.id" class="commercial-time-card">
          <header class="commercial-time-card-head">
            <div>
              <span>时效</span>
              <strong>{{ displayVersion(item)?.validity_days || 0 }} 天</strong>
            </div>
            <span class="commercial-time-card-status">{{ versionLabel(item) }}</span>
          </header>

          <div class="commercial-time-card-duration">
            <strong>{{ durationValue(displayVersion(item)?.duration_seconds || 0) }}</strong>
            <span>时</span>
          </div>

          <div class="commercial-time-card-name">{{ item.name }}</div>

          <div class="commercial-time-card-price">
            <span>价格</span>
            <strong>{{ formatMoney(displayVersion(item)?.price_cents || 0) }}</strong>
          </div>

          <div class="commercial-time-card-rules">
            <span>规则说明</span>
            <strong>{{ ruleSummary(item) }}</strong>
            <p>{{ item.description || '暂无额外说明' }}</p>
          </div>

          <div class="product-marketing-links">
            <span>参与营销计划 · {{ item.marketing_campaigns?.length || 0 }}</span>
            <div v-if="item.marketing_campaigns?.length" class="product-marketing-link-list">
              <div
                v-for="campaign in item.marketing_campaigns"
                :key="campaign.id"
                class="product-marketing-link"
                :class="{ inactive: campaign.status !== 'active' }"
              >
                <strong>{{ campaign.name }}</strong>
                <small>
                  {{ campaign.status === 'active' ? '生效中' : '已停用' }}
                  · {{ campaignDiscountLabel(campaign, item.id) }}
                  · 实际价值 {{ formatMarketingMoney(campaignActualValue(item, campaign)) }}
                </small>
              </div>
            </div>
            <small v-else class="product-marketing-empty">暂未参加营销计划</small>
          </div>

          <footer v-if="canManage" class="commercial-time-card-actions">
            <button
              v-if="item.status !== 'archived'"
              class="commercial-time-card-edit"
              type="button"
              @click="openEdit(item)"
            >
              编辑
            </button>
            <button
              v-if="item.draft_version && item.status !== 'archived'"
              class="commercial-time-card-publish"
              type="button"
              :disabled="publishingId === item.id"
              @click="publish(item)"
            >
              {{ publishingId === item.id ? '发布中...' : '发布新版本' }}
            </button>
            <button
              v-if="item.active_version && item.status !== 'archived'"
              class="commercial-time-card-edit"
              type="button"
              :disabled="listingId === item.id"
              @click="toggleListing(item)"
            >
              {{ listingId === item.id ? '处理中...' : (item.status === 'active' ? '下架' : '上架') }}
            </button>
            <button
              v-if="item.status !== 'archived'"
              class="commercial-time-card-edit"
              type="button"
              :disabled="deletingId === item.id"
              @click="archiveItem(item)"
            >
              {{ deletingId === item.id ? '删除中...' : '删除' }}
            </button>
          </footer>
        </article>
      </div>

      <PaginationBar
        v-model:page="secondaryPage"
        :total-pages="secondaryTotalPages"
        :total="secondaryFilteredItems.length"
        :page-size="secondaryPageSize"
      />
    </section>

    <div v-if="editorOpen" class="feature-editor-backdrop" @click.self="editorOpen = false">
      <section class="feature-editor-panel">
        <header>
          <div>
            <span class="section-kicker">{{ editing ? 'EDIT DRAFT' : 'CREATE' }}</span>
            <h3>{{ editing ? '编辑时长卡草稿' : '新建时长卡' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label><span>内部编码</span><input v-model="form.code" type="text" /></label>
          <label><span>名称</span><input v-model="form.name" type="text" /></label>
          <label><span>售价（元）</span><input v-model.number="form.price_yuan" type="number" min="0" step="0.01" /></label>
          <label><span>时长（小时）</span><input v-model.number="form.duration_hours" type="number" min="1" /></label>
          <label><span>激活后有效期（天）</span><input v-model.number="form.validity_days" type="number" min="1" /></label>
          <label><span>购买后最晚激活（天）</span><input v-model.number="form.activation_deadline_days" type="number" min="0" /><small>0 表示不限；首次真正使用时才开始计算有效期。</small></label>
          <label><span>激活方式</span><input value="首次使用自动激活" disabled /></label>
          <label><span>排序</span><input v-model.number="form.sort_order" type="number" /></label>

          <label class="feature-editor-wide">
            <span>说明</span>
            <textarea v-model="form.description" rows="3" />
          </label>

          <label class="feature-checkbox"><input v-model="form.participates_referral" type="checkbox" /><span>参与推荐奖励</span></label>
          <label class="feature-checkbox"><input v-model="form.participates_sales_commission" type="checkbox" /><span>参与销售提成</span></label>
          <label class="feature-checkbox"><input v-model="form.participates_agent_settlement" type="checkbox" /><span>参与代理结算</span></label>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>

        <footer>
          <button class="ghost-button" type="button" @click="editorOpen = false">取消</button>
          <button class="primary-button" type="button" :disabled="saving" @click="save">
            {{ saving ? '保存中...' : '保存草稿' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>
