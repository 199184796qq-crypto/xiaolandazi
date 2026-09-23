<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  createAfterSalesRequest,
  getAfterSalesRequestEvents,
  getAfterSalesRequests,
} from '../api'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import { useFeedbackErrorRef } from '../uiFeedback'
import type { InventoryRMA, RMAEvent } from '../types'

const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const items = ref<InventoryRMA[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const selected = ref<InventoryRMA | null>(null)
const events = ref<RMAEvent[]>([])
const detailLoading = ref(false)
const showCreate = ref(false)
const showDetail = ref(false)

const actor = computed(() => session.bootstrap?.actor)
const isAgent = computed(() => actor.value?.role === 'agent_admin')
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
)

const form = reactive({
  sn: '',
  service_type: 'repair',
  customer_name: '',
  contact_phone: '',
  issue: '',
})

function statusLabel(value: string) {
  const map: Record<string, string> = {
    SUBMITTED: '待受理',
    OPEN: '已受理',
    RETURN_PENDING: '待寄回',
    RETURNING: '寄回运输中',
    RETURN_EXCEPTION: '寄回物流异常',
    RETURN_CANCELLED: '寄回已取消',
    PROCESSING: '待检 / 处理中',
    REPAIRING: '内部维修中',
    REPAIR_OUTBOUND: '外送维修运输中',
    EXTERNAL_REPAIR: '上游维修中',
    REPAIR_RETURNING: '维修返回运输中',
    REPLACEMENT_PENDING: '换机待发出',
    RETURN_TO_CUSTOMER_PENDING: '维修完成待返还',
    OUTBOUND_PENDING: '待返还',
    OUTBOUND_SHIPPING: '返还运输中',
    OUTBOUND_EXCEPTION: '返还物流异常',
    REFUND_PENDING: '退款待处理',
    REFUND_PROCESSING: '退款处理中',
    REFUNDED: '已退款',
    COMPLETED: '已完成',
    CANCELLED: '已取消',
  }
  return map[value] || value
}

function serviceLabel(value: string) {
  if (value === 'repair') return '维修'
  if (value === 'exchange') return '换机'
  if (value === 'return') return '退货'
  return value
}

function statusTone(value: string) {
  if (value === 'COMPLETED') return 'success'
  if (value === 'CANCELLED' || value === 'RETURN_CANCELLED') return 'muted'
  if (value.includes('EXCEPTION')) return 'danger'
  if (
    value.includes('RETURNING') ||
    value.includes('SHIPPING') ||
    value === 'REPAIR_OUTBOUND'
  ) return 'info'
  return 'warning'
}

function sourceLabel(value: string) {
  if (value === 'customer') return '终端发起'
  if (value === 'agent') return '代理发起'
  return '工作人员录入'
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await getAfterSalesRequests(page.value, pageSize.value)
    items.value = data.items
    total.value = data.total
    if (page.value > totalPages.value) page.value = totalPages.value
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取售后维修单失败'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  form.sn = ''
  form.service_type = 'repair'
  form.customer_name = actor.value?.display_name || actor.value?.username || ''
  form.contact_phone = actor.value?.phone || ''
  form.issue = ''
  showCreate.value = true
}

async function submit() {
  if (!form.sn.trim()) {
    error.value = '请输入设备 SN'
    return
  }
  if (!form.issue.trim()) {
    error.value = '请填写故障或问题描述'
    return
  }
  saving.value = true
  error.value = ''
  try {
    await createAfterSalesRequest({
      sn: form.sn.trim(),
      service_type: form.service_type,
      customer_name: form.customer_name.trim(),
      contact_phone: form.contact_phone.trim(),
      issue: form.issue.trim(),
    })
    showCreate.value = false
    page.value = 1
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '提交售后维修单失败'
  } finally {
    saving.value = false
  }
}

async function openDetail(item: InventoryRMA) {
  selected.value = item
  events.value = []
  showDetail.value = true
  detailLoading.value = true
  error.value = ''
  try {
    const data = await getAfterSalesRequestEvents(item.id)
    events.value = data.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取维修进度失败'
  } finally {
    detailLoading.value = false
  }
}

async function goPage(value: number) {
  page.value = Math.min(Math.max(value, 1), totalPages.value)
  await load()
}

onMounted(load)
</script>

<template>
  <div class="after-sales-portal">
    <section class="after-sales-hero">
      <div class="after-sales-hero-icon">修</div>
      <div class="after-sales-hero-copy">
        <span>AFTER-SALES SERVICE</span>
        <h1>售后维修</h1>
        <p>
          {{ isAgent
            ? '为代理自身设备或名下终端发起维修单，并实时查看受理、物流、签收、维修和返还进度。'
            : '设备出现问题时可直接用 SN 发起售后，后续受理、寄回、维修和返还状态都会在这里更新。' }}
        </p>
      </div>
      <button class="after-sales-primary" type="button" @click="openCreate">
        ＋ 发起售后
      </button>
    </section>

    <section class="after-sales-flow">
      <div><strong>1</strong><span>提交申请</span></div>
      <i>→</i>
      <div><strong>2</strong><span>售后受理</span></div>
      <i>→</i>
      <div><strong>3</strong><span>寄回 / 签收</span></div>
      <i>→</i>
      <div><strong>4</strong><span>检测 / 维修</span></div>
      <i>→</i>
      <div><strong>5</strong><span>返还设备</span></div>
      <i>→</i>
      <div><strong>6</strong><span>完成</span></div>
    </section>

    <section class="after-sales-card">
      <header>
        <div>
          <span>MY SERVICE ORDERS</span>
          <h2>{{ isAgent ? '代理售后维修单' : '我的售后维修单' }}</h2>
          <p>共 {{ total }} 张维修单，每张单都保留完整进度。</p>
        </div>
        <button type="button" class="after-sales-ghost" :disabled="loading" @click="load">
          {{ loading ? '刷新中...' : '刷新' }}
        </button>
      </header>

      <div v-if="loading" class="after-sales-empty">正在读取售后维修单...</div>
      <div v-else-if="!items.length" class="after-sales-empty">
        暂无售后维修单。设备有问题时，可点击“发起售后”提交。
      </div>
      <div v-else class="after-sales-list">
        <button
          v-for="item in items"
          :key="item.id"
          class="after-sales-order"
          type="button"
          @click="openDetail(item)"
        >
          <div class="after-sales-order-main">
            <span>{{ item.rma_no }}</span>
            <strong>{{ item.device_sn }}</strong>
            <small>
              {{ serviceLabel(item.service_type) }} · {{ sourceLabel(item.source_type) }}
              {{ item.customer_name ? ' · ' + item.customer_name : '' }}
            </small>
          </div>
          <p>{{ item.issue }}</p>
          <div class="after-sales-order-meta">
            <span :class="['after-sales-status', statusTone(item.status)]">
              {{ statusLabel(item.status) }}
            </span>
            <small>{{ new Date(item.updated_at).toLocaleString('zh-CN') }}</small>
            <b>查看进度 →</b>
          </div>
        </button>
      </div>

      <PaginationBar
        :page="page"
        :total-pages="totalPages"
        :total="total"
        :page-size="pageSize"
        @update:page="goPage"
      />
    </section>

    <div v-if="showCreate" class="after-sales-backdrop" @click.self="showCreate = false">
      <section class="after-sales-modal">
        <header>
          <div>
            <span>NEW SERVICE ORDER</span>
            <h3>发起售后维修单</h3>
          </div>
          <button type="button" @click="showCreate = false">×</button>
        </header>
        <div class="after-sales-form">
          <label>
            <span>设备 SN *</span>
            <input v-model="form.sn" type="text" placeholder="输入设备机身 SN" />
          </label>
          <label>
            <span>售后类型 *</span>
            <select v-model="form.service_type">
              <option value="repair">维修</option>
              <option value="exchange">换机</option>
              <option value="return">退货</option>
            </select>
          </label>
          <label>
            <span>联系人</span>
            <input v-model="form.customer_name" type="text" />
          </label>
          <label>
            <span>联系电话</span>
            <input v-model="form.contact_phone" type="text" />
          </label>
          <label class="wide">
            <span>故障 / 问题描述 *</span>
            <textarea
              v-model="form.issue"
              rows="5"
              placeholder="请描述故障现象、出现时间和需要售后处理的问题"
            />
          </label>
        </div>
        <footer>
          <button class="after-sales-ghost" type="button" @click="showCreate = false">取消</button>
          <button class="after-sales-primary" type="button" :disabled="saving" @click="submit">
            {{ saving ? '提交中...' : '提交售后申请' }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="showDetail && selected" class="after-sales-backdrop" @click.self="showDetail = false">
      <section class="after-sales-modal detail">
        <header>
          <div>
            <span>SERVICE TRACKING</span>
            <h3>{{ selected.rma_no }}</h3>
          </div>
          <button type="button" @click="showDetail = false">×</button>
        </header>

        <div class="after-sales-detail-summary">
          <div><span>设备 SN</span><strong>{{ selected.device_sn }}</strong></div>
          <div><span>售后类型</span><strong>{{ serviceLabel(selected.service_type) }}</strong></div>
          <div><span>当前状态</span><strong>{{ statusLabel(selected.status) }}</strong></div>
          <div><span>联系电话</span><strong>{{ selected.contact_phone || '—' }}</strong></div>
        </div>
        <div class="after-sales-issue">
          <span>问题描述</span>
          <p>{{ selected.issue || '—' }}</p>
        </div>

        <div v-if="detailLoading" class="after-sales-empty">正在读取维修进度...</div>
        <div v-else-if="events.length" class="after-sales-timeline">
          <article v-for="event in events" :key="event.id">
            <i></i>
            <div>
              <strong>{{ event.title }}</strong>
              <p>{{ event.description }}</p>
              <small>{{ new Date(event.occurred_at).toLocaleString('zh-CN') }}</small>
            </div>
          </article>
        </div>
        <div v-else class="after-sales-empty">暂无进度记录。</div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.after-sales-portal{display:grid;gap:20px;padding:24px;min-height:100%;background:#f4f6fb;color:#253044}
.after-sales-hero,.after-sales-card,.after-sales-flow{border:1px solid #e1e6ef;border-radius:24px;background:#fff;box-shadow:0 14px 38px rgba(47,57,89,.06)}
.after-sales-hero{position:relative;display:grid;grid-template-columns:92px minmax(0,1fr) auto;gap:24px;align-items:center;padding:34px 40px;overflow:hidden;background:linear-gradient(120deg,#fff 0%,#fbfbff 62%,#eff1ff 100%)}
.after-sales-hero::after{content:"";position:absolute;right:-90px;top:-110px;width:310px;height:310px;border:34px solid rgba(100,105,218,.06);border-radius:50%}
.after-sales-hero-icon{display:grid;place-items:center;width:82px;height:82px;border-radius:22px;color:#fff;background:linear-gradient(150deg,#666af0,#5055cc);font-size:34px;font-weight:900;box-shadow:0 16px 30px rgba(82,88,205,.2)}
.after-sales-hero-copy{position:relative;z-index:1}.after-sales-hero-copy>span,.after-sales-card header span,.after-sales-modal header span{color:#969fb2;font-size:12px;font-weight:800;letter-spacing:.14em}
.after-sales-hero h1{margin:7px 0 8px;font-size:34px}.after-sales-hero p,.after-sales-card header p{margin:0;color:#7c879a;line-height:1.7}
.after-sales-primary,.after-sales-ghost{position:relative;z-index:1;min-height:42px;border-radius:11px;padding:0 17px;font:inherit;font-weight:800;cursor:pointer}
.after-sales-primary{border:0;color:#fff;background:#5c61d7;box-shadow:0 9px 20px rgba(83,88,199,.18)}.after-sales-ghost{border:1px solid #dfe4ed;color:#596478;background:#fff}
.after-sales-flow{display:flex;align-items:center;justify-content:center;gap:16px;padding:18px 22px}.after-sales-flow div{display:flex;align-items:center;gap:8px;color:#697386;font-size:12px;font-weight:700}.after-sales-flow strong{display:grid;place-items:center;width:26px;height:26px;border-radius:50%;color:#5b61ce;background:#eff0ff}.after-sales-flow i{color:#c0c6d2;font-style:normal}
.after-sales-card{padding:24px}.after-sales-card>header{display:flex;justify-content:space-between;gap:20px;align-items:center;margin-bottom:18px}.after-sales-card h2{margin:5px 0;font-size:24px}
.after-sales-list{display:grid;gap:9px}.after-sales-order{display:grid;grid-template-columns:minmax(240px,1.1fr) minmax(260px,1.5fr) auto;gap:18px;align-items:center;width:100%;padding:15px 17px;border:1px solid #e6e9f0;border-radius:13px;text-align:left;background:#fff;cursor:pointer;transition:.16s ease}.after-sales-order:hover{border-color:#cbd2e5;background:#f1f4f9;transform:translateY(-1px)}
.after-sales-order-main{display:grid;gap:4px;min-width:0}.after-sales-order-main>span{color:#8b94a4;font-size:10px;font-weight:800}.after-sales-order-main strong{font-size:14px}.after-sales-order-main small{color:#858fa1;font-size:11px}.after-sales-order>p{display:-webkit-box;overflow:hidden;margin:0;color:#687386;font-size:12px;line-height:1.55;-webkit-box-orient:vertical;-webkit-line-clamp:2}.after-sales-order-meta{display:grid;justify-items:end;gap:5px}.after-sales-order-meta small{color:#9aa2b0;font-size:10px}.after-sales-order-meta b{color:#6066cf;font-size:11px}
.after-sales-status{padding:5px 9px;border-radius:999px;font-size:11px;font-weight:800}.after-sales-status.warning{color:#9a6800;background:#fff4d9}.after-sales-status.success{color:#087958;background:#e7fbf2}.after-sales-status.info{color:#3468b7;background:#eaf2ff}.after-sales-status.danger{color:#b14555;background:#fff0f2}.after-sales-status.muted{color:#777f8e;background:#eff1f4}
.after-sales-empty{padding:40px;text-align:center;color:#9199a9}
.after-sales-backdrop{position:fixed;z-index:2600;inset:0;display:grid;place-items:center;padding:22px;background:rgba(29,35,52,.32);backdrop-filter:blur(2px)}.after-sales-modal{width:min(720px,100%);max-height:calc(100vh - 44px);overflow:auto;border-radius:20px;background:#fff;box-shadow:0 25px 70px rgba(28,35,55,.24)}.after-sales-modal.detail{width:min(820px,100%)}.after-sales-modal>header{display:flex;justify-content:space-between;align-items:center;padding:21px 24px;border-bottom:1px solid #e9ecf2}.after-sales-modal h3{margin:4px 0 0;font-size:21px}.after-sales-modal>header>button{border:0;background:transparent;color:#818a9b;font-size:26px;cursor:pointer}.after-sales-modal>footer{display:flex;justify-content:flex-end;gap:10px;padding:18px 24px;border-top:1px solid #e9ecf2}
.after-sales-form{display:grid;grid-template-columns:1fr 1fr;gap:14px;padding:22px 24px}.after-sales-form label{display:grid;gap:7px}.after-sales-form label.wide{grid-column:1/-1}.after-sales-form label>span,.after-sales-issue>span{color:#687386;font-size:12px;font-weight:800}.after-sales-form input,.after-sales-form select,.after-sales-form textarea{width:100%;box-sizing:border-box;border:1px solid #dfe4ec;border-radius:10px;padding:10px 12px;color:#354052;background:#fff;font:inherit}.after-sales-form input,.after-sales-form select{min-height:43px}.after-sales-form textarea{resize:vertical}
.after-sales-detail-summary{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px;padding:22px 24px 8px}.after-sales-detail-summary>div{display:grid;gap:4px;padding:12px;border:1px solid #eaedf3;border-radius:11px;background:#fbfcfe}.after-sales-detail-summary span{color:#8b94a4;font-size:10px}.after-sales-detail-summary strong{font-size:13px}.after-sales-issue{margin:10px 24px 0;padding:14px;border-radius:11px;background:#f7f8fb}.after-sales-issue p{margin:6px 0 0;color:#596477;line-height:1.65}
.after-sales-timeline{display:grid;padding:22px 28px 30px}.after-sales-timeline article{position:relative;display:grid;grid-template-columns:18px 1fr;gap:10px;padding-bottom:18px}.after-sales-timeline article:not(:last-child)::after{content:"";position:absolute;left:5px;top:12px;bottom:0;width:1px;background:#dfe4ed}.after-sales-timeline i{position:relative;z-index:1;width:11px;height:11px;margin-top:4px;border:3px solid #eef0ff;border-radius:50%;box-sizing:border-box;background:#5f65d3}.after-sales-timeline article>div{display:grid;gap:4px}.after-sales-timeline p{margin:0;color:#697486;font-size:12px;line-height:1.6}.after-sales-timeline small{color:#9ca4b2;font-size:10px}
@media(max-width:800px){.after-sales-portal{padding:14px}.after-sales-hero{grid-template-columns:64px 1fr;padding:24px}.after-sales-hero-icon{width:58px;height:58px;border-radius:16px;font-size:25px}.after-sales-hero .after-sales-primary{grid-column:1/-1}.after-sales-flow{overflow:auto;justify-content:flex-start}.after-sales-order{grid-template-columns:1fr}.after-sales-order-meta{justify-items:start}.after-sales-form,.after-sales-detail-summary{grid-template-columns:1fr}}
</style>
