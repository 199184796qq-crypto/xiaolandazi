<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createCommercialDeviceProduct,
  getCommercialDeviceProducts,
  publishCommercialDeviceProduct,
  saveCommercialDeviceProductDraft,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  CommercialDeviceInput,
  CommercialDeviceProduct,
  CommercialDeviceVersion,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const publishingId = ref<number | null>(null)
const error = useFeedbackErrorRef()
const items = ref<CommercialDeviceProduct[]>([])
const viewMode = ref<'card' | 'table'>('table')
const search = ref('')
const statusFilter = ref('all')
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
  sort_order: 0,
  list_price_yuan: 0,
  sale_price_yuan: 0,
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
        access.permissions.includes('commercial.membership.manage')),
  )
})

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '已发布', value: 'published' },
  { label: '有草稿', value: 'draft' },
  { label: '未发布', value: 'unpublished' },
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
      (statusFilter.value === 'published' && Boolean(item.active_version)) ||
      (statusFilter.value === 'draft' && Boolean(item.draft_version)) ||
      (statusFilter.value === 'unpublished' && !item.active_version) ||
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
  if (item.draft_version && item.active_version) {
    return '已发布 V' + item.active_version.version_no + ' · 草稿 V' + item.draft_version.version_no
  }
  if (item.draft_version) return '草稿 V' + item.draft_version.version_no
  if (item.active_version) return '已发布 V' + item.active_version.version_no
  return '未配置'
}

function discountLabel(version?: CommercialDeviceVersion) {
  if (!version || version.list_price_cents <= 0) return '—'
  if (version.sale_price_cents >= version.list_price_cents) return '原价'
  const ratio = (version.sale_price_cents / version.list_price_cents) * 10
  return ratio.toFixed(1).replace(/\.0$/, '') + ' 折'
}

function resetForm() {
  form.code = ''
  form.sku_code = ''
  form.name = ''
  form.description = ''
  form.sort_order = 0
  form.list_price_yuan = 0
  form.sale_price_yuan = 0
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
  form.sort_order = item.sort_order
  form.list_price_yuan = (version?.list_price_cents || 0) / 100
  form.sale_price_yuan = (version?.sale_price_cents || 0) / 100
  form.participates_referral = version?.participates_referral ?? true
  form.participates_sales_commission = version?.participates_sales_commission ?? true
  form.participates_agent_settlement = version?.participates_agent_settlement ?? true
  editorOpen.value = true
  error.value = ''
}

function inputPayload(): CommercialDeviceInput {
  return {
    code: form.code.trim(),
    sku_code: form.sku_code.trim(),
    name: form.name.trim(),
    description: form.description.trim(),
    sort_order: Number(form.sort_order || 0),
    list_price_cents: Math.round(Number(form.list_price_yuan || 0) * 100),
    sale_price_cents: Math.round(Number(form.sale_price_yuan || 0) * 100),
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
  if (!item.draft_version) return
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

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getCommercialDeviceProducts()
    items.value = data.items
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
    <ModulePageNav context="commercial" active-title="设备商品" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">DEVICE CATALOG</p>
        <h2>设备商品</h2>
        <p>
          配置终端终端可购买的实体设备。SKU 必须与库存设备 SKU 一致，发布后商城会显示实时可售 SN 数量；支付成功后系统按 SKU 自动锁定设备并生成物流单。
        </p>
      </div>
      <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
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
              <th>原价</th>
              <th>售价</th>
              <th>折扣</th>
              <th>可售库存</th>
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
              <td>{{ formatMoney(displayVersion(item)?.list_price_cents || 0) }}</td>
              <td><strong>{{ formatMoney(displayVersion(item)?.sale_price_cents || 0) }}</strong></td>
              <td>{{ discountLabel(displayVersion(item)) }}</td>
              <td>
                <span class="status-pill" :class="item.available_stock > 0 ? 'status-success' : 'status-error'">
                  {{ item.available_stock }} 台
                </span>
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
                  <button v-if="canManage" class="text-action" type="button" @click="openEdit(item)">
                    编辑草稿
                  </button>
                  <button
                    v-if="canManage && item.draft_version"
                    class="text-action"
                    type="button"
                    :disabled="publishingId === item.id"
                    @click="publish(item)"
                  >
                    {{ publishingId === item.id ? '发布中...' : '发布' }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredItems.length === 0" class="empty-state">暂无符合条件的设备商品。</div>
      </div>

      <div v-else class="commercial-product-card-grid">
        <article v-for="item in pagedItems" :key="item.id" class="commercial-product-card">
          <header>
            <div>
              <span>{{ item.sku_code }}</span>
              <strong>{{ item.name }}</strong>
            </div>
            <span class="status-pill" :class="item.available_stock > 0 ? 'status-success' : 'status-error'">
              {{ item.available_stock }} 台可售
            </span>
          </header>
          <p>{{ item.description || '暂无说明' }}</p>
          <dl>
            <div><dt>版本</dt><dd>{{ versionLabel(item) }}</dd></div>
            <div><dt>原价</dt><dd>{{ formatMoney(displayVersion(item)?.list_price_cents || 0) }}</dd></div>
            <div><dt>售价</dt><dd>{{ formatMoney(displayVersion(item)?.sale_price_cents || 0) }}</dd></div>
            <div><dt>折扣</dt><dd>{{ discountLabel(displayVersion(item)) }}</dd></div>
          </dl>
          <footer v-if="canManage">
            <button class="ghost-button" type="button" @click="openEdit(item)">编辑草稿</button>
            <button
              v-if="item.draft_version"
              class="primary-button"
              type="button"
              :disabled="publishingId === item.id"
              @click="publish(item)"
            >
              {{ publishingId === item.id ? '发布中...' : '发布到商城' }}
            </button>
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
      <section class="feature-editor-panel">
        <header>
          <div>
            <span class="section-kicker">DEVICE PRODUCT</span>
            <h3>{{ editing ? '编辑设备商品草稿' : '新建设备商品' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label>
            <span>内部编码 *</span>
            <input v-model="form.code" type="text" placeholder="例如 device_box_138" />
          </label>
          <label>
            <span>库存 SKU *</span>
            <input v-model="form.sku_code" type="text" placeholder="必须与设备档案 SKU 一致" />
          </label>
          <label class="feature-editor-wide">
            <span>商品名称 *</span>
            <input v-model="form.name" type="text" />
          </label>
          <label>
            <span>原价（元） *</span>
            <input v-model.number="form.list_price_yuan" type="number" min="0.01" step="0.01" />
          </label>
          <label>
            <span>当前售价（元） *</span>
            <input v-model.number="form.sale_price_yuan" type="number" min="0.01" step="0.01" />
          </label>
          <label>
            <span>排序</span>
            <input v-model.number="form.sort_order" type="number" />
          </label>
          <label class="feature-editor-wide">
            <span>商品说明</span>
            <textarea v-model="form.description" rows="4" />
          </label>

          <div class="feature-editor-wide feature-rule-switches">
            <label><input v-model="form.participates_referral" type="checkbox" /> 参与推荐奖励</label>
            <label><input v-model="form.participates_sales_commission" type="checkbox" /> 参与销售提成</label>
            <label><input v-model="form.participates_agent_settlement" type="checkbox" /> 参与代理结算</label>
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
  </div>
</template>
