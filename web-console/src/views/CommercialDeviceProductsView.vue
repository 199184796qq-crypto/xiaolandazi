<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createCommercialDeviceProduct,
  deleteCommercialDeviceProduct,
  getCommercialDeviceProducts,
  getCommercialDeviceSKUTypes,
  getSystemDictionaryItems,
  publishCommercialDeviceProduct,
  saveCommercialDeviceProductDraft,
  setCommercialDeviceProductListing,
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
  CommercialDeviceInput,
  CommercialDeviceProduct,
  CommercialDeviceVersion,
  InventoryDeviceSKUType,
  MarketingCampaign,
  MarketingCampaignItem,
  SystemDictionaryItem,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const publishingId = ref<number | null>(null)
const listingId = ref<number | null>(null)
const deletingId = ref<number | null>(null)
const error = useFeedbackErrorRef()
const items = ref<CommercialDeviceProduct[]>([])
const productUnits = ref<SystemDictionaryItem[]>([])
const skuTypes = ref<InventoryDeviceSKUType[]>([])
const skuPickerOpen = ref(false)
const skuSearch = ref('')
const viewMode = ref<'card' | 'table'>('card')
const search = ref('')
const navContext = computed(() => 'activityMarketing' as const)
const statusFilter = ref('listed')
const sortMode = ref('sort-asc')
const page = ref(1)
const pageSize = ref(12)
const editorOpen = ref(false)
const editing = ref<CommercialDeviceProduct | null>(null)

const form = reactive({
  code: '',
  sku_code: '',
  name: '',
  description: '',
  image_url: '',
  unit_code: 'unit',
  sort_order: 0,
  cost_price_yuan: 0,
  sale_price_yuan: 0,
  sales_stock: 0,
  participates_referral: true,
  participates_sales_commission: true,
  participates_agent_settlement: true,
})

function hasPermission(code: string) {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(access && (access.is_super_admin || access.permissions.includes(code)))
}

const canEditCatalog = computed(() => hasPermission('inventory.manage'))
const canManageListing = computed(() => hasPermission('commercial.device.listing.manage'))

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '已上架', value: 'listed' },
  { label: '已下架', value: 'unlisted' },
  { label: '有草稿', value: 'draft' },
  { label: '已归档', value: 'archived' },
  { label: '有库存', value: 'stock' },
  { label: '缺货', value: 'out-of-stock' },
]

const sortOptions = [
  { label: '默认排序', value: 'sort-asc' },
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '售价从高到低', value: 'price-desc' },
  { label: '库存从高到低', value: 'stock-desc' },
]

function displayVersion(item: CommercialDeviceProduct) {
  return item.draft_version || item.active_version || item.latest_version
}

function marketingTargetItem(
  campaign: MarketingCampaign,
  productId: number,
): MarketingCampaignItem | undefined {
  const item = (campaign.items ?? []).find(
    (entry) => entry.target_type === 'device_product' && entry.target_id === productId,
  )
  if (item) return item
  if (campaign.target_type === 'device_product' && campaign.target_id === productId) {
    return {
      target_type: 'device_product',
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

function campaignActualValue(item: CommercialDeviceProduct, campaign: MarketingCampaign) {
  const target = marketingTargetItem(campaign, item.id)
  if (!target) return 0
  const version = displayVersion(item)
  const price = version?.sale_price_cents || version?.list_price_cents || 0
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
    const matchesKeyword =
      !keyword ||
      [item.code, item.sku_code, item.name, item.description].some((value) =>
        String(value || '').toLowerCase().includes(keyword),
      )
    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'listed' && item.status === 'active') ||
      (statusFilter.value === 'unlisted' && item.status === 'inactive') ||
      (statusFilter.value === 'draft' && Boolean(item.draft_version)) ||
      (statusFilter.value === 'archived' && item.status === 'archived') ||
      (statusFilter.value === 'stock' && item.available_stock > 0) ||
      (statusFilter.value === 'out-of-stock' && item.available_stock <= 0)
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    const va = displayVersion(a)
    const vb = displayVersion(b)
    if (sortMode.value === 'name-asc') return a.name.localeCompare(b.name, 'zh-CN')
    if (sortMode.value === 'price-desc') {
      return (vb?.sale_price_cents || 0) - (va?.sale_price_cents || 0)
    }
    if (sortMode.value === 'stock-desc') return b.available_stock - a.available_stock
    return a.sort_order - b.sort_order || a.id - b.id
  })
})

const filteredSkuTypes = computed(() => {
  const keyword = skuSearch.value.trim().toLowerCase()
  if (!keyword) return skuTypes.value
  return skuTypes.value.filter((item) =>
    [item.sku_code, item.sample_sn, item.sample_batch_no, item.bound_product_name]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})

const selectedSkuType = computed(() =>
  skuTypes.value.find((item) => item.sku_code === form.sku_code),
)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredItems.value.length / pageSize.value)),
)

const pagedItems = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredItems.value.slice(start, start + pageSize.value)
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toFixed(2)
}

function versionLabel(item: CommercialDeviceProduct) {
  if (item.status === 'archived') return '已删除/归档'
  const listing = item.status === 'active' ? '已上架' : item.status === 'inactive' ? '已下架' : '未上架'
  if (item.draft_version && item.active_version) {
    return listing + ' · V' + item.active_version.version_no + ' · 草稿 V' + item.draft_version.version_no
  }
  if (item.draft_version) return listing + ' · 草稿 V' + item.draft_version.version_no
  if (item.active_version) return listing + ' · V' + item.active_version.version_no
  return listing
}

function grossProfitLabel(version?: CommercialDeviceVersion) {
  if (!version) return '—'
  const profitCents = version.sale_price_cents - version.cost_price_cents
  if (version.sale_price_cents <= 0) return formatMoney(profitCents)
  const margin = profitCents / version.sale_price_cents * 100
  return `${formatMoney(profitCents)} · ${margin.toFixed(1)}%`
}

function resetForm() {
  form.code = ''
  form.sku_code = ''
  form.name = ''
  form.description = ''
  form.image_url = ''
  form.unit_code = productUnits.value.find((item) => item.code === 'unit')?.code || productUnits.value[0]?.code || 'unit'
  form.sort_order = 0
  form.cost_price_yuan = 0
  form.sale_price_yuan = 0
  form.sales_stock = 0
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

function openEdit(item: CommercialDeviceProduct) {
  editing.value = item
  const version = displayVersion(item)
  form.code = item.code
  form.sku_code = item.sku_code
  form.name = item.name
  form.description = item.description
  form.image_url = item.image_url || ''
  form.unit_code = item.unit_code || 'unit'
  form.sort_order = item.sort_order
  form.cost_price_yuan = (version?.cost_price_cents || 0) / 100
  form.sale_price_yuan = ((version?.sale_price_cents || 0) > 0
    ? (version?.sale_price_cents || 0)
    : (version?.list_price_cents || 0)) / 100
  form.sales_stock = Number(item.sales_stock || 0)
  form.participates_referral = version?.participates_referral ?? true
  form.participates_sales_commission = version?.participates_sales_commission ?? true
  form.participates_agent_settlement = version?.participates_agent_settlement ?? true
  editorOpen.value = true
  error.value = ''
}

function openSkuPicker() {
  skuSearch.value = ''
  skuPickerOpen.value = true
}

function skuTypeSelectable(item: InventoryDeviceSKUType) {
  return item.bound_product_id <= 0 || item.bound_product_id === editing.value?.id
}

function selectSkuType(item: InventoryDeviceSKUType) {
  if (!skuTypeSelectable(item)) return
  const changed = form.sku_code !== item.sku_code
  form.sku_code = item.sku_code
  if (changed) {
    form.sales_stock = Math.min(Math.max(Number(form.sales_stock || 0), 0), item.in_stock_quantity)
  }
  skuPickerOpen.value = false
}

function inputPayload(): CommercialDeviceInput {
  return {
    code: editing.value?.code || '',
    sku_code: form.sku_code.trim(),
    name: form.name.trim(),
    description: form.description.trim(),
    image_url: form.image_url.trim(),
    unit_code: form.unit_code,
    sort_order: Number(form.sort_order || 0),
    sales_stock: Number(form.sales_stock || 0),
    cost_price_cents: Math.round(Number(form.cost_price_yuan || 0) * 100),
    list_price_cents: Math.round(Number(form.sale_price_yuan || 0) * 100),
    sale_price_cents: Math.round(Number(form.sale_price_yuan || 0) * 100),
    participates_referral: form.participates_referral,
    participates_sales_commission: form.participates_sales_commission,
    participates_agent_settlement: form.participates_agent_settlement,
  }
}

async function save() {
  if (!canEditCatalog.value) return
  saving.value = true
  error.value = ''
  try {
    const payload = inputPayload()
    const realStock = selectedSkuType.value?.in_stock_quantity ?? editing.value?.real_stock ?? 0
    if (!form.sku_code) throw new Error('请先选择库存设备类型')
    if (!Number.isInteger(Number(form.sales_stock)) || Number(form.sales_stock) < 0) {
      throw new Error('销售库存必须是大于等于 0 的整数')
    }
    if (Number(form.sales_stock) > Number(realStock)) {
      throw new Error(`销售库存不能超过真实可用库存 ${realStock} 台`)
    }
    if (editing.value) {
      await saveCommercialDeviceProductDraft(editing.value.id, payload)
    } else {
      await createCommercialDeviceProduct(payload)
    }
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function publish(item: CommercialDeviceProduct) {
  if (!canManageListing.value || !item.draft_version) return
  if (!(await confirmAction({
    title: '发布设备商品',
    message: '确认发布“' + item.name + '”的当前草稿？发布后终端商城会读取该价格和库存 SKU。',
    confirmText: '确认发布',
  }))) return
  publishingId.value = item.id
  error.value = ''
  try {
    await publishCommercialDeviceProduct(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '发布失败'
  } finally {
    publishingId.value = null
  }
}

async function toggleListing(item: CommercialDeviceProduct) {
  if (!canManageListing.value || listingId.value !== null || item.status === 'archived' || !item.active_version) return
  const nextStatus = item.status === 'active' ? 'inactive' : 'active'
  if (nextStatus === 'inactive') {
    const confirmed = await confirmAction({
      title: '下架设备商品',
      message: '下架“' + item.name + '”后终端商城将停止销售，但仓库库存和历史订单不会删除。',
      confirmText: '确认下架',
    })
    if (!confirmed) return
  }
  listingId.value = item.id
  error.value = ''
  try {
    await setCommercialDeviceProductListing(item.id, nextStatus)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '更新设备商品上下架失败'
  } finally {
    listingId.value = null
  }
}

async function archiveItem(item: CommercialDeviceProduct) {
  if (!canEditCatalog.value || deletingId.value !== null) return
  const confirmed = await confirmAction({
    title: '删除设备商品资料',
    message: '确认删除“' + item.name + '”？系统会归档商品资料并停止销售；设备 SN、库存流水和历史订单继续保留。',
    confirmText: '确认删除',
  })
  if (!confirmed) return
  deletingId.value = item.id
  error.value = ''
  try {
    await deleteCommercialDeviceProduct(item.id)
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '删除设备商品失败'
  } finally {
    deletingId.value = null
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [data, unitData] = await Promise.all([
      getCommercialDeviceProducts(),
      getSystemDictionaryItems('product_unit'),
    ])
    items.value = data.items
    productUnits.value = unitData.items
    if (canEditCatalog.value) {
      const skuData = await getCommercialDeviceSKUTypes()
      skuTypes.value = skuData.items
    } else {
      skuTypes.value = []
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取设备商品失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page commercial-device-product-page">
    <ModulePageNav :context="navContext" active-title="设备商城运营" active-nav-title="设备商城运营" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">DEVICE CATALOG</p>
        <h2>设备商城运营</h2>

      </div>
      <button v-if="canEditCatalog" class="primary-button" type="button" @click="openCreate">
        ＋ 新建设备商品
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="商品编码 / SKU / 名称"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <p v-if="error && !editorOpen" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取设备商品...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>设备商品</th>
              <th>SKU</th>
              <th>版本</th>
              <th>成本价</th>
              <th>销售价</th>
              <th>毛利</th>
              <th>库存</th>
              <th>参与规则</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.id">
              <td>
                <strong>{{ item.name }}</strong>
                <small>{{ item.code }}</small>
              </td>
              <td><strong>{{ item.sku_code }}</strong></td>
              <td>{{ versionLabel(item) }}</td>
              <td>{{ formatMoney(displayVersion(item)?.cost_price_cents || 0) }}</td>
              <td><strong>{{ formatMoney(displayVersion(item)?.sale_price_cents || 0) }}</strong></td>
              <td>{{ grossProfitLabel(displayVersion(item)) }}</td>
              <td>
                <div class="device-stock-breakdown">
                  <span class="status-pill" :class="item.available_stock > 0 ? 'status-success' : 'status-error'">
                    可售 {{ item.available_stock }} {{ item.unit_label || '台' }}
                  </span>
                  <small>销售 {{ item.sales_stock }} · 真实 {{ item.real_stock }}</small>
                </div>
              </td>
              <td>
                <small>
                  推荐 {{ displayVersion(item)?.participates_referral ? '✓' : '—' }}
                  · 销售 {{ displayVersion(item)?.participates_sales_commission ? '✓' : '—' }}
                  · 代理 {{ displayVersion(item)?.participates_agent_settlement ? '✓' : '—' }}
                </small>
              </td>
              <td>
                <div class="table-actions">
                  <button v-if="canEditCatalog && item.status !== 'archived'" class="text-action" type="button" @click="openEdit(item)">
                    编辑资料
                  </button>
                  <button
                    v-if="canManageListing && item.draft_version && item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="publishingId === item.id"
                    @click="publish(item)"
                  >
                    {{ publishingId === item.id ? '发布中...' : '发布新版本' }}
                  </button>
                  <button
                    v-if="canManageListing && item.active_version && item.status !== 'archived'"
                    class="text-action"
                    type="button"
                    :disabled="listingId === item.id"
                    @click="toggleListing(item)"
                  >
                    {{ listingId === item.id ? '处理中...' : (item.status === 'active' ? '下架' : '上架') }}
                  </button>
                  <button
                    v-if="canEditCatalog && item.status !== 'archived'"
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
        <div v-if="filteredItems.length === 0" class="empty-state">暂无符合条件的设备商品。</div>
      </div>

      <div v-else class="commercial-product-card-grid commerce-goods-grid">
        <article v-for="item in pagedItems" :key="item.id" class="commercial-product-card commerce-goods-card">
          <header class="commerce-goods-head">
            <div class="commerce-goods-title">
              <strong>{{ item.name }}</strong>
              <span>SKU · {{ item.sku_code }}</span>
            </div>
            <div class="commerce-goods-stock">
              <span class="commerce-stock-pill" :class="{ empty: item.available_stock <= 0 }">
                {{ item.available_stock }} {{ item.unit_label || '台' }}可售
              </span>
              <small class="commerce-stock-detail">
                销售库存 {{ item.sales_stock }} · 真实库存 {{ item.real_stock }}
              </small>
            </div>
          </header>

          <div class="commerce-goods-image">
            <img v-if="item.image_url" :src="item.image_url" :alt="item.name" />
            <div v-else class="commerce-goods-image-placeholder">
              <span>DEVICE</span>
              <strong>▣</strong>
              <small>暂未配置商品图片</small>
            </div>
          </div>

          <div class="commerce-goods-price-row">
            <div class="commerce-goods-sale-price">
              <span>销售价</span>
              <strong>{{ formatMoney(displayVersion(item)?.sale_price_cents || 0) }}</strong>
            </div>
            <div class="commerce-goods-price-metrics">
              <div class="commerce-goods-cost-price">
                <span>成本价</span>
                <strong>{{ formatMoney(displayVersion(item)?.cost_price_cents || 0) }}</strong>
              </div>
              <div class="commerce-goods-profit">
                <span>毛利</span>
                <strong>{{ grossProfitLabel(displayVersion(item)) }}</strong>
              </div>
            </div>
          </div>

          <p class="commerce-goods-description">{{ item.description || '暂无商品说明' }}</p>

          <div class="commerce-goods-meta">
            <span>{{ versionLabel(item) }}</span>
            <span>
              推荐 {{ displayVersion(item)?.participates_referral ? '✓' : '—' }} ·
              销售 {{ displayVersion(item)?.participates_sales_commission ? '✓' : '—' }} ·
              代理 {{ displayVersion(item)?.participates_agent_settlement ? '✓' : '—' }}
            </span>
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

          <footer v-if="canEditCatalog || canManageListing" class="commerce-goods-actions">
            <button v-if="canEditCatalog && item.status !== 'archived'" class="commerce-goods-edit" type="button" @click="openEdit(item)">编辑资料</button>
            <button
              v-if="canManageListing && item.draft_version && item.status !== 'archived'"
              class="commerce-goods-publish"
              type="button"
              :disabled="publishingId === item.id"
              @click="publish(item)"
            >
              {{ publishingId === item.id ? '发布中...' : '发布新版本' }}
            </button>
            <button
              v-if="canManageListing && item.active_version && item.status !== 'archived'"
              class="commerce-goods-edit"
              type="button"
              :disabled="listingId === item.id"
              @click="toggleListing(item)"
            >
              {{ listingId === item.id ? '处理中...' : (item.status === 'active' ? '下架' : '上架') }}
            </button>
            <button v-if="canEditCatalog && item.status !== 'archived'" class="commerce-goods-edit" type="button" @click="archiveItem(item)">删除</button>
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

    <div v-if="editorOpen" class="feature-editor-backdrop" @click.self="editorOpen = false">
      <section class="feature-editor-panel device-product-editor-panel">
        <header>
          <div>
            <span class="section-kicker">DEVICE PRODUCT</span>
            <h3>{{ editing ? '编辑设备商品资料' : '新建设备商品' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label>
            <span>内部编码</span>
            <input
              :value="editing?.code || '保存后由系统自动生成'"
              type="text"
              disabled
              class="system-generated-field"
            />
            <small class="field-help">系统唯一编码，创建后固定，不需要人工填写。</small>
          </label>
          <label>
            <span>库存设备类型 *</span>
            <button class="device-sku-picker-trigger" type="button" @click="openSkuPicker">
              <span v-if="form.sku_code">
                <strong>{{ form.sku_code }}</strong>
                <small v-if="selectedSkuType">
                  可售 {{ selectedSkuType.in_stock_quantity }} 台 · 共 {{ selectedSkuType.total_quantity }} 台档案
                </small>
              </span>
              <span v-else class="device-sku-picker-placeholder">
                点击从库存设备类型中选择
              </span>
              <b>选择 ›</b>
            </button>
            <small class="field-help">这里绑定设备类型（SKU），不是具体 SN；发货时系统再分配具体设备。</small>
          </label>
          <label class="feature-editor-wide">
            <span>商品名称 *</span>
            <input v-model="form.name" type="text" />
          </label>
          <label class="feature-editor-wide">
            <span>商品图片 URL</span>
            <input v-model="form.image_url" type="url" placeholder="https://...（后续可接媒体库 / OSS 选择器）" />
          </label>
          <label>
            <span>产品单位 *</span>
            <select v-model="form.unit_code">
              <option
                v-for="unit in productUnits"
                :key="unit.id"
                :value="unit.code"
              >
                {{ unit.label }}
              </option>
            </select>
          </label>
          <label>
            <span>成本价（元） *</span>
            <input v-model.number="form.cost_price_yuan" type="number" min="0.01" step="0.01" />
            <small class="field-help">内部财务成本，不会展示给终端客户。</small>
          </label>
          <label>
            <span>销售价（元） *</span>
            <input v-model.number="form.sale_price_yuan" type="number" min="0.01" step="0.01" />
            <small class="field-help">终端商城的设备正常售价；会员折扣在此价格基础上计算。</small>
          </label>
          <label class="device-sales-stock-field">
            <span>销售库存 *</span>
            <input
              v-model.number="form.sales_stock"
              type="number"
              min="0"
              :max="selectedSkuType?.in_stock_quantity ?? editing?.real_stock ?? 0"
              step="1"
            />
            <small class="real-stock-warning">
              真实可用库存：{{ selectedSkuType?.in_stock_quantity ?? editing?.real_stock ?? 0 }} 台
            </small>
            <small class="field-help">销售库存不得超过真实库存；用户下单后会立即锁库并减少。</small>
          </label>
          <label>
            <span>排序</span>
            <input v-model.number="form.sort_order" type="number" />
          </label>
          <label class="feature-editor-wide">
            <span>商品说明</span>
            <textarea v-model="form.description" rows="3" />
          </label>

          <div class="feature-editor-wide feature-rule-switches">
            <label class="feature-checkbox device-product-rule-option"><input v-model="form.participates_referral" type="checkbox" /><span>参与推荐奖励</span></label>
            <label class="feature-checkbox device-product-rule-option"><input v-model="form.participates_sales_commission" type="checkbox" /><span>参与销售提成</span></label>
            <label class="feature-checkbox device-product-rule-option"><input v-model="form.participates_agent_settlement" type="checkbox" /><span>参与代理结算</span></label>
          </div>
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

    <div v-if="skuPickerOpen" class="device-sku-picker-backdrop" @click.self="skuPickerOpen = false">
      <section class="device-sku-picker-panel">
        <header>
          <div>
            <span class="section-kicker">INVENTORY DEVICE TYPE</span>
            <h3>选择库存设备类型</h3>
            <p>按 SKU 类型选择。具体 SN 设备由销售订单发货时再从库存中分配。</p>
          </div>
          <button class="icon-button" type="button" @click="skuPickerOpen = false">×</button>
        </header>

        <div class="device-sku-picker-search">
          <input v-model="skuSearch" type="search" placeholder="搜索 SKU / 示例 SN / 批次 / 已绑定商品" />
        </div>

        <div class="device-sku-type-list">
          <button
            v-for="item in filteredSkuTypes"
            :key="item.sku_code"
            type="button"
            class="device-sku-type-option"
            :class="{ selected: form.sku_code === item.sku_code, blocked: !skuTypeSelectable(item) }"
            :disabled="!skuTypeSelectable(item)"
            @click="selectSkuType(item)"
          >
            <div class="device-sku-type-main">
              <strong>{{ item.sku_code }}</strong>
              <span>
                可售 <b>{{ item.in_stock_quantity }}</b> 台 · 总档案 {{ item.total_quantity }} 台 · {{ item.warehouse_count }} 个有货仓库
              </span>
              <small>
                示例 SN：{{ item.sample_sn || '—' }}
                <template v-if="item.sample_batch_no"> · 批次：{{ item.sample_batch_no }}</template>
              </small>
            </div>
            <em v-if="item.bound_product_id && item.bound_product_id !== editing?.id">
              已绑定 {{ item.bound_product_name || ('商品 #' + item.bound_product_id) }}
            </em>
            <em v-else-if="form.sku_code === item.sku_code" class="selected-label">已选择</em>
            <em v-else>选择此类型</em>
          </button>
          <div v-if="filteredSkuTypes.length === 0" class="device-sku-picker-empty">
            没有找到符合条件的库存设备类型。
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.system-generated-field:disabled {
  color: #6f788a;
  background: #f3f5f9;
  cursor: not-allowed;
}

.field-help {
  display: block;
  margin-top: 6px;
  color: #9aa3b2;
  font-size: 11px;
  line-height: 1.45;
}

.device-sku-picker-trigger {
  display: flex;
  width: 100%;
  min-height: 62px;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  padding: 10px 14px;
  border: 1px solid #dbe2ee;
  border-radius: 12px;
  color: #354055;
  text-align: left;
  background: #fff;
  transition: .18s ease;
}

.device-sku-picker-trigger:hover {
  border-color: #91a5e8;
  box-shadow: 0 0 18px rgba(82, 109, 205, .10);
}

.device-sku-picker-trigger > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.device-sku-picker-trigger strong {
  font-size: 15px;
}

.device-sku-picker-trigger small {
  color: #8791a3;
  font-size: 11px;
}

.device-sku-picker-placeholder {
  color: #8d96a7;
  font-size: 14px;
}

.device-sku-picker-trigger > b {
  flex: 0 0 auto;
  color: #536bc6;
  font-size: 12px;
}

.device-stock-breakdown {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.device-stock-breakdown small,
.commerce-stock-detail {
  color: #8b94a5;
  font-size: 10px;
  font-weight: 600;
}

.commerce-stock-detail {
  display: block;
  margin-top: 4px;
}

.real-stock-warning {
  display: block;
  margin-top: 7px;
  color: #d7363f;
  font-size: 13px;
  font-weight: 800;
}

.device-sales-stock-field input:invalid {
  border-color: #dc6570;
}

.commerce-goods-card {
  width: 360px !important;
  min-height: 560px !important;
  gap: 16px !important;
  padding: 18px !important;
  border-radius: 22px !important;
}

.commerce-goods-head {
  display: grid !important;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start !important;
  gap: 12px 18px !important;
  min-height: 66px !important;
}

.commerce-goods-title {
  display: grid;
  min-width: 0;
  gap: 6px;
}

.commerce-goods-title > strong {
  color: #26344d !important;
  font-size: 21px !important;
  font-weight: 900 !important;
  line-height: 1.2;
}

.commerce-goods-title > span {
  color: #76839a !important;
  font-size: 13px !important;
  font-weight: 700;
}

.commerce-goods-stock {
  display: grid;
  justify-items: end;
  gap: 7px;
}

.commerce-stock-pill {
  padding: 8px 13px !important;
  font-size: 13px !important;
}

.commerce-stock-detail {
  margin-top: 0 !important;
  color: #7e8aa0 !important;
  font-size: 12px !important;
  font-weight: 750 !important;
  white-space: nowrap;
}

.commerce-goods-image {
  border-radius: 18px !important;
}

.commerce-goods-image-placeholder > span {
  font-size: 12px !important;
}

.commerce-goods-image-placeholder > small {
  font-size: 13px !important;
}

.commerce-goods-price-row {
  display: grid !important;
  grid-template-columns: minmax(0, 1.12fr) minmax(0, .88fr);
  align-items: stretch !important;
  gap: 14px !important;
}

.commerce-goods-sale-price {
  display: grid !important;
  align-content: center;
  gap: 6px !important;
  min-width: 0;
  padding: 14px 0;
}

.commerce-goods-sale-price > span {
  color: #65728a !important;
  font-size: 14px !important;
  font-weight: 850 !important;
}

.commerce-goods-sale-price > strong {
  color: #df3f46 !important;
  font-size: 36px !important;
  font-weight: 950 !important;
  line-height: 1 !important;
  white-space: nowrap;
}

.commerce-goods-price-metrics {
  display: grid;
  gap: 8px;
}

.commerce-goods-cost-price,
.commerce-goods-profit {
  display: grid;
  grid-template-columns: auto 1fr;
  align-items: center;
  gap: 6px 10px;
  min-width: 0;
  padding: 9px 11px;
  border-radius: 11px;
  background: #f7f9fc;
}

.commerce-goods-cost-price > span,
.commerce-goods-profit > span {
  color: #7d899d;
  font-size: 12px;
  font-weight: 800;
  white-space: nowrap;
}

.commerce-goods-cost-price > strong {
  color: #68758b;
  font-size: 15px;
  font-weight: 850;
  white-space: nowrap;
}

.commerce-goods-profit {
  background: #fff3e9;
}

.commerce-goods-profit > strong {
  color: #d86125;
  font-size: 13px;
  font-weight: 900;
  line-height: 1.25;
}

.commerce-goods-description {
  min-height: 42px !important;
  color: #5f6d83 !important;
  font-size: 14px !important;
  line-height: 1.55 !important;
}

.commerce-goods-meta {
  gap: 7px !important;
  padding-top: 12px !important;
  color: #7d899c !important;
  font-size: 13px !important;
  line-height: 1.5;
}

.commerce-goods-actions {
  gap: 10px !important;
  padding-top: 8px !important;
}

.commerce-goods-edit,
.commerce-goods-publish {
  min-height: 42px !important;
  padding: 0 16px !important;
  border-radius: 11px !important;
  font-size: 14px !important;
  font-weight: 850 !important;
}

@media (max-width: 760px) {
  .commerce-goods-card {
    width: 100% !important;
  }

  .commerce-goods-price-row {
    grid-template-columns: 1fr;
  }

  .commerce-goods-stock {
    justify-items: start;
  }

  .commerce-goods-head {
    grid-template-columns: 1fr;
  }
}

.device-sku-picker-backdrop {
  position: fixed;
  z-index: 3200;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 28px;
  background: rgba(32, 41, 61, .42);
  backdrop-filter: blur(7px);
}

.device-sku-picker-panel {
  width: min(820px, 94vw);
  max-height: 82vh;
  overflow: hidden;
  border: 1px solid #dce3ef;
  border-radius: 20px;
  background: #f8faff;
  box-shadow: 0 28px 70px rgba(36, 48, 78, .24), 0 0 24px rgba(80, 108, 204, .10);
}

.device-sku-picker-panel > header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  padding: 22px 24px 18px;
  border-bottom: 1px solid #e5eaf2;
  background: #fff;
}

.device-sku-picker-panel h3 {
  margin: 5px 0 5px;
  font-size: 21px;
}

.device-sku-picker-panel header p {
  margin: 0;
  color: #8c95a5;
  font-size: 12px;
}

.device-sku-picker-search {
  padding: 16px 20px 12px;
}

.device-sku-picker-search input {
  width: 100%;
  min-height: 46px;
  padding: 0 14px;
  border: 1px solid #dbe2ee;
  border-radius: 12px;
  background: #fff;
}

.device-sku-type-list {
  display: grid;
  gap: 9px;
  max-height: calc(82vh - 170px);
  padding: 0 20px 20px;
  overflow-y: auto;
}

.device-sku-type-option {
  display: flex;
  width: 100%;
  min-height: 94px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  padding: 15px 16px;
  border: 1px solid #e0e6f0;
  border-radius: 14px;
  color: #344055;
  text-align: left;
  background: #fff;
  transition: .18s ease;
}

.device-sku-type-option:hover:not(:disabled),
.device-sku-type-option.selected {
  border-color: #8599df;
  background: #f4f7ff;
  box-shadow: 0 8px 20px rgba(67, 88, 159, .08), 0 0 14px rgba(80, 109, 205, .08);
}

.device-sku-type-option.blocked {
  opacity: .55;
  cursor: not-allowed;
}

.device-sku-type-main {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 6px;
}

.device-sku-type-main > strong {
  color: #2d3d68;
  font-size: 16px;
}

.device-sku-type-main > span {
  color: #626e82;
  font-size: 12px;
}

.device-sku-type-main > span b {
  color: #385dc0;
}

.device-sku-type-main > small {
  color: #9aa3b1;
  font-size: 10px;
}

.device-sku-type-option > em {
  flex: 0 0 auto;
  padding: 6px 9px;
  border-radius: 9px;
  color: #6171a8;
  font-size: 10px;
  font-style: normal;
  background: #eef2ff;
}

.device-sku-type-option.blocked > em {
  color: #9d6570;
  background: #fff0f2;
}

.device-sku-type-option > .selected-label {
  color: #307154;
  background: #e9f8f0;
}

.device-sku-picker-empty {
  padding: 50px 20px;
  color: #9099aa;
  text-align: center;
}
</style>
