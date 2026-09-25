<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import ModulePageNav from '../components/ModulePageNav.vue'
import DataListControls from '../components/DataListControls.vue'
import PaginationBar from '../components/PaginationBar.vue'
import SalesCustomerActions from '../components/SalesCustomerActions.vue'
import { contactActionLabel } from '../salesContactLabels'
import {receiptLabels} from '../customerBusinessApi'
import {
  getSalesCustomerPage, customerSourceLabels,
  customerSourceLabel as sourceLabel, customerStatusLabel as statusLabel,
} from '../salesCustomersApi'
import type { SalesCustomer } from '../salesCustomersApi'
import '../salesBusiness.css'

const items = ref<SalesCustomer[]>([])
const loading = ref(false), error = ref('')
const search = ref(''), status = ref('all'), source = ref('all'), sort = ref('created-desc')
const page = ref(1), pageSize = ref(12), total = ref(0), pages = ref(1)
const view = ref<'table' | 'card'>('table')
const qualification = ref('all'), collection = ref('all')
const collectionOptions = Object.entries(receiptLabels).filter(([v]) => ['awaiting_payment','pending','needs_info','rejected','posting_failed','posted'].includes(v))
const filtered = computed(() => Boolean(search.value.trim() || status.value !== 'all' || source.value !== 'all' || qualification.value !== 'all' || collection.value !== 'all'))
const statusOptions = [
  { value: 'all', label: '全部账号状态' }, { value: 'active', label: '启用' },
  { value: 'disabled', label: '已停用' },
]
const sourceOptions = Object.entries(customerSourceLabels).map(([value, label]) => ({ value, label }))
const sortOptions = [
  { value: 'created-desc', label: '最新开户优先' }, { value: 'created-asc', label: '最早开户优先' },
  { value: 'name-asc', label: '客户名称正序' }, { value: 'name-desc', label: '客户名称倒序' },
]
let controller: AbortController | undefined
let searchTimer: ReturnType<typeof setTimeout> | undefined
let sequence = 0

async function load() {
  controller?.abort()
  const current = new AbortController(), version = ++sequence
  controller = current
  loading.value = true
  error.value = ''
  try {
    const result = await getSalesCustomerPage({
      search: search.value.trim(), status: status.value, source: source.value,
      sort: sort.value, page: page.value, page_size: pageSize.value,
      qualification: qualification.value, collection: collection.value,
    }, current.signal)
    if (current.signal.aborted || version !== sequence) return
    if (!Array.isArray(result.items) || !Number.isFinite(result.total)) {
      throw new Error('客户列表返回格式异常，请刷新重试')
    }
    pages.value = Math.max(1, result.total_pages)
    total.value = result.total
    if (page.value > pages.value) {
      page.value = pages.value
      await load()
      return
    }
    items.value = result.items
  } catch (value) {
    if (!current.signal.aborted && version === sequence) {
      items.value = []
      error.value = value instanceof Error ? value.message : '读取客户失败，请重试'
    }
  } finally {
    if (version === sequence) loading.value = false
  }
}
function reloadFirstPage() {
  clearTimeout(searchTimer)
  page.value = 1
  void load()
}
function changeSearch(value: string) {
  search.value = value
  controller?.abort()
  clearTimeout(searchTimer)
  searchTimer = setTimeout(reloadFirstPage, 300)
}
function changeStatus(value: string) { status.value = value; reloadFirstPage() }
function changeSort(value: string) { sort.value = value; reloadFirstPage() }
function changePageSize(value: number) { pageSize.value = value; reloadFirstPage() }
function changePage(value: number) { page.value = value; void load() }
function clearFilters() { search.value = ''; status.value = 'all'; source.value = 'all'; qualification.value = 'all'; collection.value = 'all'; reloadFirstPage() }
function displayName(item: SalesCustomer) { return item.display_name?.trim() || item.username || '未填写名称' }
function initials(item: SalesCustomer) { return Array.from(displayName(item)).slice(0, 1).join('') }
function region(item: SalesCustomer) { return [item.province, item.city, item.district].filter(Boolean).join(' / ') || '未填写地区' }
function formatDate(value: string) {
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date.toLocaleString('zh-CN', { hour12: false }) : '未记录'
}
const target = ref<SalesCustomer | null>(null)
const dialog = ref<HTMLElement | null>(null)
let returnFocus: HTMLElement | null = null
async function detail(item: SalesCustomer) {
  returnFocus = document.activeElement as HTMLElement | null
  target.value = item
  await nextTick()
  dialog.value?.focus()
}
async function closeDetail() {
  target.value = null
  await nextTick()
  if (returnFocus?.isConnected) returnFocus.focus()
}
function dialogKey(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); void closeDetail(); return }
  if (event.key !== 'Tab') return
  const buttons = Array.from(dialog.value?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href]') || [])
  const first = buttons[0], last = buttons[buttons.length - 1]
  if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog.value)) {
    event.preventDefault(); last?.focus()
  } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog.value)) {
    event.preventDefault(); first?.focus()
  }
}
let refreshTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => { void load(); refreshTimer=setInterval(() => {if (!document.hidden && !loading.value && !target.value) void load()},15000) })
onBeforeUnmount(() => { controller?.abort(); clearTimeout(searchTimer); clearInterval(refreshTimer); sequence++ })
</script>

<template>
  <div class="management-page sales-business sales-customers-page">
    <ModulePageNav context="workspace-sales" active-title="我的客户" />
    <header class="sb-toolbar sc-heading">
      <div><h2>我的客户</h2><p>查看当前负责的已开户客户；只有财务审核入账后才认定真实客户。</p></div>
      <button type="button" :disabled="loading" @click="load">{{ loading ? '刷新中…' : '刷新客户' }}</button>
    </header>
    <section class="sb-panel sc-panel" :aria-busy="loading">
      <div class="sb-toolbar sc-panel-heading"><div><h3>负责客户</h3><small>客户归属由后台管理，销售仅查看本人范围。</small></div><span v-if="!error && !loading" class="sc-count">{{ filtered ? '筛选结果' : '当前共' }} {{ total }} 位</span></div>
      <DataListControls
        :view-mode="view" :search="search" :status="status" :sort="sort" :page-size="pageSize"
        :status-options="statusOptions" :sort-options="sortOptions"
        search-placeholder="搜索客户名称、账号、电话或地区"
        @update:view-mode="view = $event" @update:search="changeSearch" @update:status="changeStatus"
        @update:sort="changeSort" @update:page-size="changePageSize"
      />
      <div class="sc-extra-filters"><label>客户来源 <select v-model="source" @change="reloadFirstPage"><option value="all">全部来源</option><option v-for="option in sourceOptions" :key="option.value" :value="option.value">{{ option.label }}</option></select></label><label>客户认定 <select v-model="qualification" @change="reloadFirstPage"><option value="all">全部认定</option><option value="unconfirmed">待财务确认</option><option value="qualified">真实客户</option></select></label><label>收款进度 <select v-model="collection" @change="reloadFirstPage"><option value="all">全部收款进度</option><option v-for="[value,label] in collectionOptions" :key="value" :value="value">{{label}}</option></select></label><button v-if="filtered" type="button" @click="clearFilters">清空筛选</button></div>
      <div v-if="error" class="sb-error" role="alert">{{ error }} <button type="button" @click="load">重试</button></div>
      <div v-else-if="loading" class="sb-empty" role="status">正在读取客户，请稍候…</div>
      <div v-else-if="!items.length" class="sb-empty">
        <h3>{{ filtered ? '没有符合条件的客户' : '还没有负责的客户' }}</h3>
        <p>{{ filtered ? '试试其他关键词，或清空筛选查看全部负责客户。' : '意向顾客预开户后可转入这里，也可由主管分配；是否真实客户要看财务认定。' }}</p>
        <button v-if="filtered" type="button" @click="clearFilters">清空筛选</button>
        <RouterLink v-else class="sc-link-button" to="/sales/leads">进入意向顾客</RouterLink>
      </div>
      <div v-else-if="view === 'table'" class="sb-table sc-table">
        <table aria-label="本人负责客户"><thead><tr><th>客户信息</th><th>联系电话</th><th>所在地区</th><th>客户来源</th><th>客户认定</th><th>收款进度</th><th>账号状态</th><th>开户时间</th><th>操作</th></tr></thead>
          <tbody><tr v-for="item in items" :key="item.user_id">
            <td><div class="sc-identity"><span class="sc-avatar" aria-hidden="true">{{ initials(item) }}</span><div><strong class="sc-name" :title="displayName(item)">{{ displayName(item) }}</strong><small>@{{ item.username }} · 客户 #{{ item.tenant_id }}</small></div></div></td>
            <td><span class="sc-phone">{{ item.phone || '未填写电话' }}</span></td>
            <td><span class="sc-clamp" :title="region(item)">{{ region(item) }}</span></td>
            <td><span class="sc-source">{{ sourceLabel(item.source_type) }}</span></td>
            <td><span class="sb-badge" :class="{'is-qualified':item.qualified}">{{item.qualified ? '真实客户' : '待财务确认'}}</span><small v-if="item.confirmed_at">确认：{{formatDate(item.confirmed_at)}}</small></td>
            <td><span class="sb-badge">{{receiptLabels[item.collection_status] || '待核实'}}</span></td>
            <td><span class="sb-badge sc-status" :class="{ 'is-active': item.status === 'active', 'is-disabled': item.status === 'disabled' }">{{ statusLabel(item.status) }}</span></td>
            <td><time :datetime="item.created_at">{{ formatDate(item.created_at) }}</time></td>
            <td><SalesCustomerActions :tenant-id="item.tenant_id" :qualified="item.qualified" @detail="detail(item)"/></td>
          </tr></tbody>
        </table>
      </div>
      <div v-else class="sb-grid sc-card-grid">
        <article v-for="item in items" :key="item.user_id" class="sb-record sc-card">
          <header><div class="sc-identity"><span class="sc-avatar" aria-hidden="true">{{ initials(item) }}</span><div><h3 class="sc-name" :title="displayName(item)">{{ displayName(item) }}</h3><small>@{{ item.username }} · 客户 #{{ item.tenant_id }}</small></div></div><span class="sb-badge sc-status" :class="{ 'is-active': item.status === 'active', 'is-disabled': item.status === 'disabled' }">{{ statusLabel(item.status) }}</span></header>
          <dl class="sc-card-facts"><div><dt>客户认定</dt><dd><span class="sb-badge">{{item.qualified?'真实客户':'待财务确认'}}</span></dd></div><div><dt>收款进度</dt><dd>{{receiptLabels[item.collection_status]||'待核实'}}</dd></div><div><dt>联系电话</dt><dd class="sc-phone">{{ item.phone || '未填写电话' }}</dd></div><div><dt>客户来源</dt><dd>{{ sourceLabel(item.source_type) }}</dd></div><div><dt>所在地区</dt><dd class="sc-clamp" :title="region(item)">{{ region(item) }}</dd></div><div><dt>开户时间</dt><dd>{{ formatDate(item.created_at) }}</dd></div></dl>
          <footer><SalesCustomerActions :tenant-id="item.tenant_id" :qualified="item.qualified" @detail="detail(item)"/></footer>
        </article>
      </div>
      <PaginationBar v-if="!error && !loading" :page="page" :total-pages="pages" :total="total" :page-size="pageSize" @update:page="changePage" />
    </section>
    <Teleport to="body"><div v-if="target" class="sb-overlay" @click.self="closeDetail"><section ref="dialog" tabindex="-1" class="sb-dialog sales-business sc-details" role="dialog" aria-modal="true" aria-labelledby="customer-detail-title" @keydown="dialogKey">
      <header class="sb-toolbar"><div><h2 id="customer-detail-title">客户资料</h2><small>客户 #{{ target.tenant_id }} · 只读查看</small></div><button type="button" @click="closeDetail">关闭</button></header>
      <div class="sc-identity"><span class="sc-avatar" aria-hidden="true">{{ initials(target) }}</span><div><h3>{{ displayName(target) }}</h3><small>@{{ target.username }}</small></div></div>
      <dl class="sc-detail-facts"><div><dt>客户认定</dt><dd>{{target.qualified?'真实客户 · 财务已入账':'已开户 · 待财务确认'}}</dd></div><div><dt>收款进度</dt><dd>{{receiptLabels[target.collection_status]||'待核实'}}</dd></div><div><dt>首次财务确认</dt><dd>{{target.confirmed_at?formatDate(target.confirmed_at):'尚无确认记录'}}</dd></div><div><dt>联系电话</dt><dd>{{ target.phone || '未填写' }}</dd></div><div><dt>客户邮箱</dt><dd>{{ target.email || '未填写' }}</dd></div><div><dt>客户来源</dt><dd>{{ sourceLabel(target.source_type) }}</dd></div><div><dt>账号状态</dt><dd>{{ statusLabel(target.status) }}</dd></div><div><dt>所在地区</dt><dd>{{ region(target) }}</dd></div><div><dt>详细地址</dt><dd>{{ target.address || '未填写' }}</dd></div><div><dt>开户时间</dt><dd>{{ formatDate(target.created_at) }}</dd></div></dl>
      <p class="sb-note">此处不修改客户账号、资金或销售归属；财务确认前记录跟进，确认后的沟通记为售后回访记录。</p>
      <div class="sb-actions"><RouterLink class="sc-link-button" :to="'/sales/customers/'+target.tenant_id+'/money'" @click="closeDetail">资金记录</RouterLink><RouterLink class="sc-link-button" :to="{path:'/sales/receipts',query:{tenant:target.tenant_id}}" @click="closeDetail">收款进度</RouterLink><RouterLink class="sc-link-button" :to="{path:'/sales/support',query:{tenant:target.tenant_id}}" @click="closeDetail">申请运维协助</RouterLink></div>
      <RouterLink class="sc-link-button" :to="{ path: '/sales/followups', query: { tenant: target.tenant_id } }" @click="closeDetail">{{ contactActionLabel(target.qualified) }}</RouterLink>
    </section></div></Teleport>
  </div>
</template>

<style scoped>
.sales-customers-page { min-width: 0; }
.sc-heading p { color: #6d7d95; margin-bottom: 0; }
.sc-metrics { grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); }
.sc-table .is-qualified {color:#217451;background:#eef9f2;border-color:#c0e5d0}
.sc-metrics strong small { font-weight: 500; }
.sc-panel-heading { padding-bottom: 18px; border-bottom: 1px solid #e7edf7; }
.sc-panel-heading small { display: block; margin-top: 4px; }
.sc-count { color: #607695; white-space: nowrap; font-size: 15px; }
.sc-extra-filters { display: flex; align-items: center; gap: 12px; margin: 0 0 20px; flex-wrap: wrap; }
.sc-extra-filters label { display: flex; align-items: center; gap: 10px; color: #64758e; }
.sc-extra-filters select { min-height: 42px; padding: 7px 12px; border: 1px solid #d5e1f5; border-radius: 10px; background: #fbfdff; color: #344e76; }
.sc-table table { min-width: 1510px; }
.sc-table td:last-child {min-width:320px}
.sc-table th, .sc-table td { white-space: normal; vertical-align: middle; }
.sc-table th:first-child { min-width: 260px; }
.sc-table th:nth-child(3) { min-width: 140px; }
.sc-table th:last-child { min-width: 230px; }
.sc-table time { display: block; min-width: 150px; font-variant-numeric: tabular-nums; }
.sc-identity { display: flex; gap: 13px; align-items: center; min-width: 0; }
.sc-identity > div { min-width: 0; }
.sc-avatar { display: grid; place-items: center; flex: 0 0 46px; width: 46px; height: 46px; border: 1px solid #d4e2ff; border-radius: 14px; color: #3d6bc3; background: linear-gradient(145deg, #eef5ff, #dfeaff); font-size: 21px; font-weight: 750; }
.sc-name { color: #263c61; line-height: 1.5; overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.sc-identity small { display: block; margin-top: 4px; overflow-wrap: anywhere; }
.sc-phone { font-variant-numeric: tabular-nums; font-weight: 650; color: #3b5272; }
.sc-clamp { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; overflow-wrap: anywhere; }
.sc-source { white-space: nowrap; color: #536e97; }
.sc-status.is-active { color: #247957; background: #eef9f3; border-color: #ccebdd; }
.sc-status.is-disabled { color: #805f58; background: #faf3f1; border-color: #ecdbd6; }
.sc-actions { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; }
.sc-link-button { display: inline-flex; align-items: center; justify-content: center; min-height: 42px; border: 1px solid #aacbff; padding: 8px 14px; border-radius: 10px; background: #eef5ff; color: #2b60b5; font-size: 16px; font-weight: 600; text-decoration: none; transition: box-shadow .16s, border-color .16s; white-space: nowrap; }
.sc-link-button:hover, .sc-link-button:focus-visible { outline: none; border-color: #66a1ff; box-shadow: 0 0 0 3px #4285ff22, 0 5px 18px #3388ff2b; }
.sc-card-grid { grid-template-columns: repeat(auto-fill, minmax(min(320px, 100%), 1fr)); align-items: stretch; }
.sc-card { display: flex; flex-direction: column; gap: 18px; }
.sc-card header { align-items: flex-start; gap: 10px; }
.sc-card header h3 { font-size: 19px; }
.sc-card-facts { display: grid; gap: 13px; margin: 0; }
.sc-card-facts > div { display: grid; grid-template-columns: 80px minmax(0,1fr); gap: 12px; }
.sc-card-facts dt, .sc-detail-facts dt { color: #75839a; }
.sc-card-facts dd, .sc-detail-facts dd { margin: 0; overflow-wrap: anywhere; }
.sc-card footer { padding-top: 16px; margin-top: auto; border-top: 1px solid #e4ebf6; }
.sc-details { max-width: 820px; outline: none; }
.sc-detail-facts { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 18px 24px; margin: 24px 0; }
.sc-detail-facts > div { display: grid; gap: 4px; }
@media(max-width:700px) {
  .sc-metrics { grid-template-columns: 1fr; }
  .sc-detail-facts { grid-template-columns: 1fr; }
}
</style>