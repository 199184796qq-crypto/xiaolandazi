<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { createSalesFollowup, getSalesCustomers, getSalesFollowups } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import type { AdminCustomer, SalesFollowup } from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'
import { recognition, type Recognition } from '../customerBusinessApi'
import { contactRecordTitle, historicalContactTitle } from '../salesContactLabels'
import '../salesBusiness.css'

const route = useRoute()
const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')
const customers = ref<AdminCustomer[]>([])
const items = ref<SalesFollowup[]>([])
const search = ref('')
const customerFilter = ref(0)

const form = reactive({
  tenant_id: 0,
  followup_type: 'phone',
  content: '',
  next_followup_at: '',
})

// Financial qualification is read from the server, never from a URL flag.
const recognitions = ref<Record<number, Recognition>>({})
const recognitionLoading = ref(false)
const recognitionError = ref('')
let recognitionController: AbortController | undefined
let disposed = false
const selectedRecognition = computed(() => recognitions.value[form.tenant_id])
const recordTitle = computed(() => contactRecordTitle(selectedRecognition.value?.qualified))
const pageTitle = computed(() => form.tenant_id ? recordTitle.value : '跟进与回访')
const actionWord = computed(() => selectedRecognition.value?.qualified === true
  ? '回访' : selectedRecognition.value?.qualified === false ? '跟进' : '联系')
const contentHint = computed(() => selectedRecognition.value?.qualified === true
  ? '例如：客户反馈设备使用正常，需要补充直播配置指导，约定周五再次回访。'
  : '例如：客户正在评估三台设备，关注费用，约定周五再次沟通。')

async function loadRecognition() {
  recognitionController?.abort()
  const tenant = form.tenant_id
  recognitionError.value = ''
  if (!tenant || disposed) { recognitionLoading.value = false; return }
  const controller = new AbortController()
  recognitionController = controller
  recognitionLoading.value = true
  delete recognitions.value[tenant]
  try {
    const result = await recognition(tenant, controller.signal)
    if (controller.signal.aborted || disposed || form.tenant_id !== tenant) return
    if (result.tenant_id !== tenant || typeof result.qualified !== 'boolean') {
      throw new Error('客户认定信息异常，请重试')
    }
    recognitions.value[tenant] = result
  } catch (value) {
    if (!controller.signal.aborted && !disposed && form.tenant_id === tenant) {
      recognitionError.value = '暂未读到财务认定，先显示为客户联系记录。' +
        (value instanceof Error ? value.message : '请重试。')
    }
  } finally {
    if (recognitionController === controller) recognitionLoading.value = false
  }
}
function historyTitle(item: SalesFollowup) {
  const known = recognitions.value[item.tenant_id]
  return historicalContactTitle(item.created_at, known?.qualified, known?.confirmed_at)
}
function selectRouteCustomer() {
  const tenant = Number(route.query.tenant || 0)
  if (tenant && customers.value.some((item) => item.tenant_id === tenant)) {
    form.tenant_id = tenant
    customerFilter.value = tenant
  }
}
watch(() => form.tenant_id, () => { customerFilter.value = form.tenant_id; void loadRecognition() })
watch(() => route.query.tenant, selectRouteCustomer)

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return items.value.filter((item) => {
    if (customerFilter.value && item.tenant_id !== customerFilter.value) return false
    if (!keyword) return true
    return [
      item.customer_name,
      item.customer_username,
      item.customer_phone,
      item.content,
      followupTypeLabel(item.followup_type),
    ].some((value) => String(value || '').toLowerCase().includes(keyword))
  })
})

const latestByTenant = computed(() => {
  const seen = new Set<number>()
  return items.value.filter((item) => {
    if (seen.has(item.tenant_id)) return false
    seen.add(item.tenant_id)
    return true
  })
})

const pendingCustomers = computed(() =>
  latestByTenant.value
    .filter((item) => Boolean(item.next_followup_at))
    .sort((a, b) => {
      const av = a.next_followup_at ? new Date(a.next_followup_at).getTime() : Number.MAX_SAFE_INTEGER
      const bv = b.next_followup_at ? new Date(b.next_followup_at).getTime() : Number.MAX_SAFE_INTEGER
      return av - bv
    }),
)

function followupTypeLabel(value: string) {
  const labels: Record<string, string> = {
    phone: '电话',
    wechat: '微信',
    visit: '拜访',
    message: '消息',
    note: '记录',
  }
  return labels[value] || '记录'
}

function formatDate(value?: string) {
  if (!value) return '—'
  return new Date(value).toLocaleString('zh-CN', { hour12: false })
}

function isOverdue(value?: string) {
  return Boolean(value && new Date(value).getTime() < Date.now())
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [customerData, followupData] = await Promise.all([
      getSalesCustomers(),
      getSalesFollowups(),
    ])
    if (disposed) return
    customers.value = customerData.items || []
    items.value = followupData.items || []

    const previousTenant = form.tenant_id
    if (!customers.value.some((item) => item.tenant_id === previousTenant)) form.tenant_id = 0
    if (!form.tenant_id) {
      selectRouteCustomer()
      if (!form.tenant_id && customers.value.length === 1) form.tenant_id = customers.value[0]!.tenant_id
    }
    if (form.tenant_id === previousTenant) await loadRecognition()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取跟进与回访记录失败'
  } finally {
    loading.value = false
  }
}

async function submit() {
  if (saving.value || recognitionLoading.value) return
  if (!form.tenant_id) {
    error.value = '请先选择需要联系的客户'
    return
  }
  const content = form.content.trim()
  if (!content) {
    error.value = '请填写本次' + actionWord.value + '内容'
    return
  }

  saving.value = true
  error.value = ''
  notice.value = ''
  try {
    const savedTitle = recordTitle.value
    const nextFollowup = form.next_followup_at
      ? new Date(form.next_followup_at).toISOString()
      : undefined
    await createSalesFollowup({
      tenant_id: form.tenant_id,
      followup_type: form.followup_type,
      content,
      next_followup_at: nextFollowup,
    })
    form.content = ''
    form.next_followup_at = ''
    notice.value = savedTitle + '已保存。'
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存客户联系记录失败'
  } finally {
    saving.value = false
  }
}

onMounted(() => { void load(); window.addEventListener('focus', loadRecognition) })
onBeforeUnmount(() => { disposed = true; recognitionController?.abort(); window.removeEventListener('focus', loadRecognition) })
</script>

<template>
  <div class="management-page sales-business sales-followups-page">
    <ModulePageNav context="workspace-sales" :active-title="pageTitle" active-nav-title="跟进与回访" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">CUSTOMER CONTACT RECORDS</p>
        <h2>{{ pageTitle }}</h2>
        <p>{{ selectedRecognition?.qualified === true ? '记录客户使用情况、问题反馈、服务满意度和下次回访安排。' : '缴费前记录跟进；财务确认入账后的沟通记录为售后回访。' }}仅可操作本人负责的客户。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="inline-success" role="status">{{ notice }}</p>

    <section class="sales-followup-layout">
      <article class="settings-card sales-followup-form-card">
        <header class="settings-card-header">
          <div>
            <span class="section-kicker">NEW RECORD</span>
            <h3>新增{{ recordTitle }}</h3>
          </div>
        </header>

        <div v-if="!customers.length" class="empty-state">
          当前没有分配给你的客户，暂时不能新增联系记录。
        </div>

        <form v-else class="sales-followup-form" @submit.prevent="submit">
          <label>
            <span>客户</span>
            <select v-model.number="form.tenant_id" :disabled="saving">
              <option :value="0">请选择客户</option>
              <option v-for="item in customers" :key="item.tenant_id" :value="item.tenant_id">
                {{ item.display_name }} · {{ item.phone || '@' + item.username }}
              </option>
            </select>
          </label>

          <label>
            <span>{{ actionWord }}方式</span>
            <select v-model="form.followup_type">
              <option value="phone">电话</option>
              <option value="wechat">微信</option>
              <option value="visit">拜访</option>
              <option value="message">消息</option>
              <option value="note">其他记录</option>
            </select>
          </label>

          <p v-if="recognitionLoading" class="span-2" role="status">正在核对客户财务认定…</p>
          <div v-else-if="recognitionError" class="span-2 sb-error" role="alert">{{ recognitionError }} <button type="button" @click="loadRecognition">重新核对</button></div>
          <p v-else-if="selectedRecognition" class="span-2 sb-note">{{ selectedRecognition.qualified ? '财务已确认：本次沟通记为售后回访记录。' : '尚未财务确认：本次沟通仍记为跟进记录。' }}</p>
          <label class="span-2">
            <span>本次{{ actionWord }}内容</span>
            <textarea
              v-model="form.content"
              rows="5"
              maxlength="2000"
              :placeholder="contentHint"
            />
          </label>

          <label class="span-2">
            <span>下次{{ actionWord }}时间</span>
            <input v-model="form.next_followup_at" type="datetime-local" />
            <small>不需要安排下一次{{ actionWord }}时可以留空。</small>
          </label>

          <button class="primary-button span-2" type="submit" :disabled="saving || recognitionLoading">
            {{ saving ? '保存中...' : '保存' + recordTitle }}
          </button>
        </form>
      </article>

      <article class="settings-card sales-followup-task-card">
        <header class="settings-card-header">
          <div>
            <span class="section-kicker">NEXT ACTIONS</span>
            <h3>跟进与回访安排</h3>
          </div>
        </header>

        <div v-if="!pendingCustomers.length" class="empty-state">
          暂无已安排的下一次联系。
        </div>
        <div v-else class="sales-task-list">
          <article
            v-for="item in pendingCustomers.slice(0, 10)"
            :key="'task-' + item.tenant_id"
            :class="['sales-task-row', { overdue: isOverdue(item.next_followup_at) }]"
          >
            <div>
              <strong>{{ item.customer_name }}</strong>
              <small>{{ item.customer_phone || '@' + item.customer_username }}</small>
            </div>
            <div>
              <span>{{ isOverdue(item.next_followup_at) ? '已逾期' : '计划联系' }}</span>
              <strong>{{ formatDate(item.next_followup_at) }}</strong>
            </div>
          </article>
        </div>
      </article>
    </section>

    <section class="settings-card sales-followup-history">
      <header class="settings-card-header sales-followup-history-head">
        <div><span class="section-kicker">HISTORY</span><h3>跟进与售后回访历史</h3></div>
        <div class="sales-followup-filters">
          <select v-model.number="customerFilter">
            <option :value="0">全部客户</option>
            <option v-for="item in customers" :key="item.tenant_id" :value="item.tenant_id">
              {{ item.display_name }}
            </option>
          </select>
          <input v-model="search" type="search" placeholder="搜索客户、电话或联系内容" />
        </div>
      </header>

      <p class="sb-note">财务确认前的跟进历史保留，不因之后缴费而改写。当前所选客户按首次财务确认时间区分记录；尚未核对的记录不强行标记售后。</p>
      <div v-if="!loading && !filteredItems.length" class="empty-state">
        暂无符合条件的联系记录。
      </div>

      <div v-else class="sales-followup-history-list">
        <article v-for="item in filteredItems" :key="item.id" class="sales-followup-history-row">
          <div class="sales-followup-history-main">
            <div>
              <strong>{{ item.customer_name }}</strong>
              <span class="status-pill">{{ followupTypeLabel(item.followup_type) }}</span>
              <span class="status-pill contact-record-kind">{{ historyTitle(item) }}</span>
            </div>
            <p>{{ item.content }}</p>
            <small>{{ item.customer_phone || '@' + item.customer_username }} · {{ formatDate(item.created_at) }}</small>
          </div>
          <div v-if="item.next_followup_at" class="sales-followup-next">
            <span>下次联系</span>
            <strong :class="{ overdue: isOverdue(item.next_followup_at) }">
              {{ formatDate(item.next_followup_at) }}
            </strong>
          </div>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
:deep(.status-pill), .sales-followup-form small { font-size: 14px; }
.sales-followup-history-main > div { flex-wrap: wrap; }
.sales-followup-history-row:hover, .sales-task-row:hover { box-shadow: 0 0 0 2px #4285ff22, 0 5px 18px #3388ff24; }
.sales-followup-layout {
  display: grid;
  grid-template-columns: minmax(0, 1.25fr) minmax(320px, 0.75fr);
  gap: 16px;
  margin: 18px 0;
}

.sales-followup-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
}

.sales-followup-form label {
  display: grid;
  gap: 7px;
}

.sales-followup-form label > span {
  color: #657087;
  font-size: 16px;
  font-weight: 700;
}

.sales-followup-form input,
.sales-followup-form select,
.sales-followup-form textarea,
.sales-followup-filters input,
.sales-followup-filters select {
  width: 100%;
  border: 1px solid #dfe5f0;
  border-radius: 12px;
  background: #fbfcff;
  padding: 11px 12px;
  color: #253047;
  outline: none;
}

.sales-followup-form textarea {
  resize: vertical;
}

.sales-followup-form small {
  color: #919aab;
}

.span-2 {
  grid-column: 1 / -1;
}

.sales-task-list,
.sales-followup-history-list {
  display: grid;
  gap: 9px;
}

.sales-task-row {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 14px;
  padding: 13px 14px;
  border-radius: 14px;
  background: #f8faff;
  border: 1px solid transparent;
}

.sales-task-row.overdue {
  background: #fff7f4;
  border-color: #f5d7cc;
}

.sales-task-row > div {
  display: grid;
  gap: 4px;
}

.sales-task-row > div:last-child {
  text-align: right;
}

.sales-task-row small,
.sales-task-row span {
  color: #8c96a9;
  font-size: 14px;
}

.sales-followup-history-head {
  align-items: flex-end;
}

.sales-followup-filters {
  display: grid;
  grid-template-columns: 180px minmax(240px, 320px);
  gap: 8px;
}

.sales-followup-history-row {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: 20px;
  padding: 15px 16px;
  border-radius: 15px;
  background: #f8faff;
}

.sales-followup-history-main > div {
  display: flex;
  align-items: center;
  gap: 10px;
}

.sales-followup-history-main p {
  margin: 9px 0 6px;
  color: #475267;
  line-height: 1.55;
}

.sales-followup-history-main small,
.sales-followup-next span {
  color: #919aab;
}

.sales-followup-next {
  min-width: 160px;
  display: grid;
  align-content: center;
  gap: 5px;
  text-align: right;
}

.sales-followup-next strong.overdue {
  color: #cf6247;
}

@media (max-width: 980px) {
  .sales-followup-layout {
    grid-template-columns: 1fr;
  }
  .sales-followup-filters {
    grid-template-columns: 1fr;
  }
  .sales-followup-history-row {
    grid-template-columns: 1fr;
  }
  .sales-followup-next {
    text-align: left;
  }
}
</style>
