<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createCommercialTimeCard,
  getCommercialTimeCards,
  publishCommercialTimeCard,
  saveCommercialTimeCardDraft,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  CommercialTimeCardInput,
  CommercialTimeCardProduct,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const publishingId = ref<number | null>(null)
const error = useFeedbackErrorRef()
const items = ref<CommercialTimeCardProduct[]>([])
const viewMode = ref<'card' | 'table'>('table')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('sort-asc')
const page = ref(1)
const pageSize = ref(12)
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
]

const sortOptions = [
  { label: '默认排序', value: 'sort-asc' },
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '价格从高到低', value: 'price-desc' },
  { label: '时长从高到低', value: 'duration-desc' },
]

function displayVersion(item: CommercialTimeCardProduct) {
  return item.draft_version || item.active_version || item.latest_version
}

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const version = displayVersion(item)
    const matchesKeyword =
      !keyword ||
      [item.code, item.name, item.description]
        .some((value) => String(value || '').toLowerCase().includes(keyword))

    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'published' && Boolean(item.active_version)) ||
      (statusFilter.value === 'draft' && Boolean(item.draft_version)) ||
      (statusFilter.value === 'unpublished' && !item.active_version)

    return matchesKeyword && matchesStatus && Boolean(version)
  })

  return [...result].sort((a, b) => {
    const va = displayVersion(a)
    const vb = displayVersion(b)
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

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toFixed(2)
}

function formatHours(seconds: number) {
  return (seconds / 3600).toLocaleString('zh-CN') + ' 小时'
}

function versionLabel(item: CommercialTimeCardProduct) {
  if (item.draft_version && item.active_version) {
    return '已发布 V' + item.active_version.version_no + ' · 草稿 V' + item.draft_version.version_no
  }
  if (item.draft_version) return '草稿 V' + item.draft_version.version_no
  if (item.active_version) return '已发布 V' + item.active_version.version_no
  return '未配置'
}

function resetForm() {
  form.code = ''
  form.name = ''
  form.description = ''
  form.sort_order = 0
  form.price_yuan = 0
  form.duration_hours = 10
  form.validity_days = 365
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
    <ModulePageNav context="commercial" active-title="时长卡" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">TIME CARD CATALOG</p>
        <h2>时长卡</h2>
        <p>配置终端可购买的时长、原价、有效期和参与奖励/提成/代理结算规则。发布后终端商城自动读取。</p>
      </div>
      <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
        ＋ 新建时长卡
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="时长卡名称 / 编码 / 说明"
        :status-options="statusOptions"
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
              <th>有效期</th>
              <th>版本状态</th>
              <th>参与规则</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.id">
              <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
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
                  <button class="text-action" type="button" @click="openEdit(item)">编辑</button>
                  <button
                    v-if="item.draft_version"
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
        <div v-if="filteredItems.length === 0" class="empty-state">暂无时长卡。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in pagedItems" :key="item.id" class="feature-record-card">
          <header>
            <div><span>{{ item.code }}</span><h3>{{ item.name }}</h3></div>
            <span class="status-pill">{{ versionLabel(item) }}</span>
          </header>
          <dl>
            <div><dt>价格</dt><dd>{{ formatMoney(displayVersion(item)?.price_cents || 0) }}</dd></div>
            <div><dt>时长</dt><dd>{{ formatHours(displayVersion(item)?.duration_seconds || 0) }}</dd></div>
            <div><dt>有效期</dt><dd>{{ displayVersion(item)?.validity_days || 0 }} 天</dd></div>
            <div><dt>说明</dt><dd>{{ item.description || '—' }}</dd></div>
          </dl>
          <footer v-if="canManage">
            <button class="text-action" type="button" @click="openEdit(item)">编辑</button>
            <button v-if="item.draft_version" class="text-action" type="button" @click="publish(item)">发布</button>
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
          <label><span>有效期（天）</span><input v-model.number="form.validity_days" type="number" min="1" /></label>
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
