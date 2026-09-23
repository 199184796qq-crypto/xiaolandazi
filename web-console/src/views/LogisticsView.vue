<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  createLogisticsShipment,
  getInventoryAgents,
  getInventoryDevices,
  getInventoryWarehouses,
  getLogisticsShipments,
  updateLogisticsShipmentStatus,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  CreateLogisticsShipmentInput,
  AgentSummary,
  InventoryDevice,
  InventoryWarehouse,
  LogisticsShipment,
  UpdateLogisticsShipmentStatusInput,
} from '../types'

const route = useRoute()

const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const shipments = ref<LogisticsShipment[]>([])
const devices = ref<InventoryDevice[]>([])
const warehouses = ref<InventoryWarehouse[]>([])
const agents = ref<AgentSummary[]>([])

const viewMode = ref<'card' | 'table'>('table')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('updated-desc')
const page = ref(1)
const pageSize = ref(12)

const modal = ref<'create' | 'status' | 'detail' | ''>('')
const selectedShipment = ref<LogisticsShipment | null>(null)
const selectedDeviceIDs = ref<number[]>([])

const createForm = reactive({
  shipment_type: 'outbound',
  business_type: 'manual',
  business_id: '',
  business_no: '',
  from_warehouse_id: '',
  to_warehouse_id: '',
  recipient_customer_id: '',
  recipient_org_id: '',
  recipient_type: 'individual',
  delivery_method: 'courier',
  recipient_name: '',
  recipient_phone: '',
  recipient_address: '',
  carrier_code: '',
  carrier_name: '',
  tracking_no: '',
  logistics_fee_yuan: '',
  note: '',
})

const statusForm = reactive({
  status: '',
  location: '',
  description: '',
})

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('logistics.manage')),
  )
})

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '待处理', value: 'pending' },
  { label: '待出库', value: 'ready_to_ship' },
  { label: '已发货', value: 'shipped' },
  { label: '运输中', value: 'in_transit' },
  { label: '已签收', value: 'delivered' },
  { label: '物流异常', value: 'exception' },
  { label: '已退回', value: 'returned' },
  { label: '已取消', value: 'cancelled' },
]

const sortOptions = [
  { label: '最近更新', value: 'updated-desc' },
  { label: '最早创建', value: 'created-asc' },
  { label: '物流单号 A-Z', value: 'no-asc' },
]

const filteredShipments = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const items = shipments.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [
        item.shipment_no,
        item.tracking_no,
        item.business_no,
        item.recipient_name,
        item.recipient_phone,
        item.recipient_address,
        item.carrier_name,
        ...item.items.map((row) => row.sn),
      ].some((value) => String(value || '').toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' || item.status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...items].sort((a, b) => {
    if (sortMode.value === 'created-asc') {
      return new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    }
    if (sortMode.value === 'no-asc') {
      return a.shipment_no.localeCompare(b.shipment_no, 'zh-CN')
    }
    return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredShipments.value.length / pageSize.value)),
)

const pagedShipments = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredShipments.value.slice(start, start + pageSize.value)
})

const pendingCount = computed(() =>
  shipments.value.filter((item) =>
    ['pending', 'ready_to_ship'].includes(item.status),
  ).length,
)

const transitCount = computed(() =>
  shipments.value.filter((item) =>
    ['shipped', 'in_transit'].includes(item.status),
  ).length,
)

const exceptionCount = computed(() =>
  shipments.value.filter((item) => item.status === 'exception').length,
)

const deliveredCount = computed(() =>
  shipments.value.filter((item) => item.status === 'delivered').length,
)

const eligibleDevices = computed(() => {
  const sourceWarehouseID = Number(createForm.from_warehouse_id) || 0
  const type = createForm.shipment_type

  return devices.value.filter((item) => {
    if (
      sourceWarehouseID > 0 &&
      item.custody_warehouse_id &&
      item.custody_warehouse_id !== sourceWarehouseID
    ) {
      return false
    }

    if (type === 'return' || type === 'rma_return') {
      return ['ACTIVE', 'CUSTOMER_BOUND', 'SOLD'].includes(item.lifecycle_status)
    }
    if (type === 'repair_outbound') {
      return item.lifecycle_status === 'REPAIRING'
    }
    if (type === 'repair_return') {
      return item.lifecycle_status === 'EXTERNAL_REPAIR'
    }

    return ['IN_STOCK', 'RESERVED', 'AGENT_STOCK'].includes(item.lifecycle_status)
  })
})

const nextStatusOptions = computed(() => {
  const current = selectedShipment.value?.status || ''
  const map: Record<string, Array<{ value: string; label: string }>> = {
    pending: [
      { value: 'ready_to_ship', label: '备货完成 / 待出库' },
      { value: 'cancelled', label: '取消物流单' },
    ],
    ready_to_ship:
      selectedShipment.value?.delivery_method === 'pickup'
        ? [
            { value: 'delivered', label: '确认已直接领取' },
            { value: 'cancelled', label: '取消交接单' },
          ]
        : [
            { value: 'shipped', label: '确认出库 / 已交快递' },
            { value: 'cancelled', label: '取消物流单' },
          ],
    shipped: [
      { value: 'in_transit', label: '运输中' },
      { value: 'delivered', label: '已签收' },
      { value: 'exception', label: '物流异常' },
      { value: 'returned', label: '退回' },
    ],
    in_transit: [
      { value: 'delivered', label: '已签收' },
      { value: 'exception', label: '物流异常' },
      { value: 'returned', label: '退回' },
    ],
    exception: [
      { value: 'in_transit', label: '恢复运输' },
      { value: 'delivered', label: '已签收' },
      { value: 'returned', label: '退回' },
    ],
  }
  return map[current] || []
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})

watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

watch(
  () => createForm.shipment_type,
  () => {
    selectedDeviceIDs.value = []
    applyShipmentTypeDefaults()
  },
)

watch(
  () => createForm.from_warehouse_id,
  () => {
    selectedDeviceIDs.value = selectedDeviceIDs.value.filter((id) =>
      eligibleDevices.value.some((item) => item.id === id),
    )
  },
)

function optionalNumber(value: string) {
  const n = Number(value)
  return value.trim() && Number.isFinite(n) && n > 0 ? n : undefined
}

function warehouseName(id?: number) {
  if (!id) return '—'
  return warehouses.value.find((item) => item.id === id)?.name || '仓库 #' + id
}

function warehouseByCode(code: string) {
  return warehouses.value.find((item) => item.code === code) || null
}

function applyShipmentTypeDefaults() {
  const type = createForm.shipment_type
  if (type === 'rma_return' || type === 'return') {
    const target = warehouseByCode('AFTER_SALES_PENDING')
    createForm.from_warehouse_id = ''
    createForm.to_warehouse_id = target?.id ? String(target.id) : ''
    createForm.recipient_type = 'individual'
    return
  }
  if (type === 'repair_outbound') {
    const repair = warehouseByCode('REPAIR')
    createForm.from_warehouse_id = repair?.id ? String(repair.id) : ''
    createForm.to_warehouse_id = ''
    createForm.recipient_type = 'individual'
    return
  }
  if (type === 'repair_return') {
    const repair = warehouseByCode('REPAIR')
    createForm.from_warehouse_id = ''
    createForm.to_warehouse_id = repair?.id ? String(repair.id) : ''
    createForm.recipient_type = 'individual'
    return
  }
  if (type === 'outbound' || type === 'exchange' || type === 'resend') {
    const main = warehouseByCode('HQ_MAIN')
    if (!createForm.from_warehouse_id && main?.id) {
      createForm.from_warehouse_id = String(main.id)
    }
  }
}

function applyRouteIntent() {
  const shipmentType = String(route.query.shipment_type || '')
  if (!shipmentType) return

  resetCreateForm()
  createForm.shipment_type = shipmentType
  createForm.business_type = String(route.query.business_type || 'manual')
  createForm.business_id = String(route.query.business_id || '')
  createForm.business_no = String(route.query.business_no || '')
  createForm.recipient_name = String(route.query.recipient_name || '')
  applyShipmentTypeDefaults()

  const deviceID = Number(route.query.device_id || 0)
  if (deviceID > 0) {
    const device = devices.value.find((item) => item.id === deviceID)
    if (device?.custody_warehouse_id && ['exchange', 'resend', 'repair_outbound'].includes(shipmentType)) {
      createForm.from_warehouse_id = String(device.custody_warehouse_id)
    }
    selectedDeviceIDs.value = [deviceID]
  }
  error.value = ''
  modal.value = 'create'
}

function shipmentTypeLabel(value: string) {
  const map: Record<string, string> = {
    outbound: '设备出库',
    transfer: '仓库调拨',
    return: '终端退回',
    exchange: '换货发出',
    resend: '补发',
    rma_return: '售后退回',
    repair_outbound: '外送维修',
    repair_return: '维修返回',
  }
  return map[value] || value
}

function businessTypeLabel(value: string) {
  const map: Record<string, string> = {
    manual: '手工业务',
    order: '订单',
    rma: '售后单',
    transfer: '调拨单',
  }
  return map[value] || value
}

function statusLabel(value: string) {
  const map: Record<string, string> = {
    pending: '待处理',
    ready_to_ship: '待出库',
    shipped: '已发货',
    in_transit: '运输中',
    delivered: '已签收',
    exception: '物流异常',
    returned: '已退回',
    cancelled: '已取消',
  }
  return map[value] || value
}

function statusTone(value: string) {
  if (value === 'delivered') return 'status-success'
  if (value === 'exception') return 'status-error'
  if (value === 'returned' || value === 'cancelled') return 'status-muted'
  if (value === 'in_transit' || value === 'shipped') return 'status-info'
  return 'status-warning'
}

function defaultStatusDescription(status: string) {
  const map: Record<string, string> = {
    ready_to_ship: '仓库已完成备货，等待发出',
    shipped: '仓库已出库并交付承运商',
    in_transit: '运输途中',
    delivered: '已完成签收或现场领取',
    exception: '物流异常',
    returned: '物流退回',
    cancelled: '取消物流单',
  }
  return map[status] || ''
}

function resetCreateForm() {
  createForm.shipment_type = 'outbound'
  createForm.business_type = 'manual'
  createForm.business_id = ''
  createForm.business_no = ''
  createForm.from_warehouse_id = warehouses.value[0]?.id
    ? String(warehouses.value[0].id)
    : ''
  createForm.to_warehouse_id = ''
  createForm.recipient_customer_id = ''
  createForm.recipient_org_id = ''
  createForm.recipient_type = 'individual'
  createForm.delivery_method = 'courier'
  createForm.recipient_name = ''
  createForm.recipient_phone = ''
  createForm.recipient_address = ''
  createForm.carrier_code = ''
  createForm.carrier_name = ''
  createForm.tracking_no = ''
  createForm.logistics_fee_yuan = ''
  createForm.note = ''
  selectedDeviceIDs.value = []
}

function openCreate() {
  resetCreateForm()
  applyShipmentTypeDefaults()
  error.value = ''
  modal.value = 'create'
}

function onAgentChange() {
  const agent = agents.value.find(
    (item) => item.organization_id === Number(createForm.recipient_org_id),
  )
  if (!agent) return
  createForm.recipient_name = agent.name || agent.display_name || agent.username
  createForm.recipient_phone = agent.phone || ''
}

function openDetail(item: LogisticsShipment) {
  selectedShipment.value = item
  modal.value = 'detail'
}

function openStatus(item: LogisticsShipment) {
  selectedShipment.value = item
  statusForm.status = ''
  statusForm.location = ''
  statusForm.description = ''
  error.value = ''
  modal.value = 'status'
}

function toggleDevice(id: number) {
  if (selectedDeviceIDs.value.includes(id)) {
    selectedDeviceIDs.value = selectedDeviceIDs.value.filter((item) => item !== id)
  } else {
    selectedDeviceIDs.value = [...selectedDeviceIDs.value, id]
  }
}

async function submitCreate() {
  if (
    createForm.delivery_method === 'courier' &&
    String(createForm.logistics_fee_yuan).trim() === ''
  ) {
    error.value = '请填写物流费用，无费用请填 0'
    return
  }

  const payload: CreateLogisticsShipmentInput = {
    shipment_type: createForm.shipment_type,
    business_type: createForm.business_type,
    business_id: optionalNumber(createForm.business_id),
    business_no: createForm.business_no.trim(),
    from_warehouse_id: optionalNumber(createForm.from_warehouse_id),
    to_warehouse_id: optionalNumber(createForm.to_warehouse_id),
    recipient_customer_id: optionalNumber(createForm.recipient_customer_id),
    recipient_org_id: optionalNumber(createForm.recipient_org_id),
    recipient_type: createForm.recipient_type,
    delivery_method: createForm.delivery_method,
    logistics_fee_cents:
      createForm.delivery_method === 'pickup'
        ? 0
        : Math.round(Number(createForm.logistics_fee_yuan || 0) * 100),
    recipient_name: createForm.recipient_name.trim(),
    recipient_phone: createForm.recipient_phone.trim(),
    recipient_address: createForm.recipient_address.trim(),
    carrier_code: createForm.carrier_code.trim(),
    carrier_name: createForm.carrier_name.trim(),
    tracking_no: createForm.tracking_no.trim(),
    device_ids: [...selectedDeviceIDs.value],
    note: createForm.note.trim(),
  }

  saving.value = true
  error.value = ''
  try {
    await createLogisticsShipment(payload)
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '创建物流单失败'
  } finally {
    saving.value = false
  }
}

async function submitStatus() {
  if (!selectedShipment.value || !statusForm.status) return
  const payload: UpdateLogisticsShipmentStatusInput = {
    status: statusForm.status,
    location: statusForm.location.trim(),
    description:
      statusForm.description.trim() ||
      defaultStatusDescription(statusForm.status),
  }

  saving.value = true
  error.value = ''
  try {
    const item = await updateLogisticsShipmentStatus(
      selectedShipment.value.id,
      payload,
    )
    selectedShipment.value = item
    modal.value = 'detail'
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '更新物流状态失败'
  } finally {
    saving.value = false
  }
}

async function loadAll() {
  loading.value = true
  error.value = ''
  try {
    const [shipmentData, deviceData, warehouseData, agentData] = await Promise.all([
      getLogisticsShipments(),
      getInventoryDevices(),
      getInventoryWarehouses(),
      getInventoryAgents(),
    ])
    shipments.value = shipmentData.items
    devices.value = deviceData.items
    warehouses.value = warehouseData.items
    agents.value = agentData.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取物流数据失败'
  } finally {
    loading.value = false
  }
}

onMounted(async () => {
  await loadAll()
  applyRouteIntent()
})

watch(
  () => route.fullPath,
  () => {
    if (route.path === '/resources/logistics') applyRouteIntent()
  },
)
</script>

<template>
  <div class="management-page logistics-page">
    <ModulePageNav context="resources" active-title="物流管理" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">LOGISTICS MANAGEMENT</p>
        <h2>物流管理</h2>
        <p>
          管理设备出库、快递、自提、退回、外送维修和维修返回。每个物流单都关联具体设备 SN，并同步库存与设备生命周期。
        </p>
      </div>

      <div class="inventory-hero-actions">
        <button class="ghost-button" type="button" :disabled="loading" @click="loadAll">
          {{ loading ? '刷新中...' : '刷新数据' }}
        </button>
        <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
          ＋ 新建物流 / 交接单
        </button>
      </div>
    </section>

    <section class="module-hub-metrics-v2">
      <article class="module-hub-metric-v2 tone-primary">
        <span>物流单</span><strong>{{ shipments.length }}</strong><small>累计物流单</small>
      </article>
      <article class="module-hub-metric-v2 tone-warning">
        <span>待出库</span><strong>{{ pendingCount }}</strong><small>待处理 / 待发货</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>运输中</span><strong>{{ transitCount }}</strong><small>已发货 / 运输途中</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>已签收 / 异常</span><strong>{{ deliveredCount }} / {{ exceptionCount }}</strong><small>签收完成 / 待处理异常</small>
      </article>
    </section>

    <section class="settings-card logistics-list-panel">
      <header class="inventory-section-head">
        <div>
          <strong>物流与交接记录</strong>
          <span>快递保留真实运单号；直接领取无需运单号，但同样记录设备、领取人、时间和经办人。</span>
        </div>
      </header>

      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="物流单 / 运单号 / 收件人 / 手机号 / SN"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <p v-if="error && !modal" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取物流数据...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table logistics-table">
          <thead>
            <tr>
              <th>物流单</th>
              <th>类型</th>
              <th>关联业务</th>
              <th>设备</th>
              <th>承运商 / 运单</th>
              <th>收件信息</th>
              <th>状态</th>
              <th>更新时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedShipments" :key="item.id">
              <td>
                <strong>{{ item.shipment_no }}</strong>
                <small>{{ warehouseName(item.from_warehouse_id) }} → {{ warehouseName(item.to_warehouse_id) }}</small>
              </td>
              <td>{{ shipmentTypeLabel(item.shipment_type) }}</td>
              <td>
                <strong>{{ businessTypeLabel(item.business_type) }}</strong>
                <small>{{ item.business_no || (item.business_id ? '#' + item.business_id : '手工业务') }}</small>
              </td>
              <td>
                <strong>{{ item.items.length }} 台</strong>
                <small>{{ item.items.map((row) => row.sn).slice(0, 2).join('、') }}{{ item.items.length > 2 ? '…' : '' }}</small>
              </td>
              <td>
                <strong>{{ item.delivery_method === 'pickup' ? '直接领取' : (item.carrier_name || '—') }}</strong>
                <small>{{ item.delivery_method === 'pickup' ? '无需运单号' : (item.tracking_no || '—') }}</small>
              </td>
              <td>
                <strong>{{ item.recipient_name || '—' }}</strong>
                <small>{{ item.recipient_phone || item.recipient_address || '—' }}</small>
              </td>
              <td>
                <span class="status-pill" :class="statusTone(item.status)">
                  {{ statusLabel(item.status) }}
                </span>
              </td>
              <td>{{ new Date(item.updated_at).toLocaleString('zh-CN') }}</td>
              <td>
                <div class="table-actions">
                  <button class="text-action" type="button" @click="openDetail(item)">轨迹</button>
                  <button
                    v-if="canManage && !['delivered','returned','cancelled'].includes(item.status)"
                    class="text-action"
                    type="button"
                    @click="openStatus(item)"
                  >
                    推进
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredShipments.length === 0" class="empty-state">暂无符合条件的物流单。</div>
      </div>

      <div v-else class="logistics-card-grid">
        <article v-for="item in pagedShipments" :key="item.id" class="logistics-card">
          <header>
            <div>
              <span>{{ shipmentTypeLabel(item.shipment_type) }}</span>
              <strong>{{ item.shipment_no }}</strong>
            </div>
            <span class="status-pill" :class="statusTone(item.status)">
              {{ statusLabel(item.status) }}
            </span>
          </header>
          <dl>
            <div><dt>交付</dt><dd>{{ item.delivery_method === 'pickup' ? '直接领取' : ((item.carrier_name || '—') + ' · ' + (item.tracking_no || '—')) }}</dd></div>
            <div><dt>设备</dt><dd>{{ item.items.map((row) => row.sn).join('、') }}</dd></div>
            <div><dt>收件人</dt><dd>{{ item.recipient_name || '—' }} {{ item.recipient_phone }}</dd></div>
            <div><dt>地址</dt><dd>{{ item.recipient_address || '—' }}</dd></div>
          </dl>
          <footer>
            <button class="ghost-button" type="button" @click="openDetail(item)">查看轨迹</button>
            <button
              v-if="canManage && !['delivered','returned','cancelled'].includes(item.status)"
              class="primary-button"
              type="button"
              @click="openStatus(item)"
            >
              推进状态
            </button>
          </footer>
        </article>
        <div v-if="filteredShipments.length === 0" class="empty-state">暂无符合条件的物流单。</div>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredShipments.length"
        :page-size="pageSize"
      />
    </section>

    <div v-if="modal" class="feature-editor-backdrop" @click.self="modal = ''">
      <section class="feature-editor-panel logistics-editor-panel">
        <header>
          <div>
            <span class="section-kicker">LOGISTICS OPERATION</span>
            <h3>
              {{ modal === 'create' ? '新建物流 / 交接单' : modal === 'status' ? '推进物流状态' : '物流轨迹' }}
            </h3>
          </div>
          <button class="icon-button" type="button" @click="modal = ''">×</button>
        </header>

        <div v-if="modal === 'create'" class="feature-editor-grid">
          <label>
            <span>物流类型 *</span>
            <select v-model="createForm.shipment_type">
              <option value="outbound">设备出库</option>
              <option value="transfer">仓库调拨</option>
              <option value="return">终端退回</option>
              <option value="exchange">换货发出</option>
              <option value="resend">补发</option>
              <option value="rma_return">售后退回</option>
              <option value="repair_outbound">外送维修</option>
              <option value="repair_return">维修返回</option>
            </select>
          </label>
          <label>
            <span>关联业务</span>
            <select v-model="createForm.business_type">
              <option value="manual">手工业务</option>
              <option value="order">订单</option>
              <option value="rma">售后单</option>
              <option value="transfer">调拨单</option>
            </select>
          </label>
          <label><span>业务 ID</span><input v-model="createForm.business_id" type="number" /></label>
          <label><span>业务单号</span><input v-model="createForm.business_no" type="text" placeholder="订单号 / RMA 号" /></label>
          <label>
            <span>出库仓库</span>
            <select v-model="createForm.from_warehouse_id">
              <option value="">无 / 终端侧</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">
                {{ warehouse.name }}
              </option>
            </select>
          </label>
          <label>
            <span>目标仓库</span>
            <select v-model="createForm.to_warehouse_id">
              <option value="">无</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">
                {{ warehouse.name }}
              </option>
            </select>
          </label>
          <label v-if="['outbound','exchange','resend'].includes(createForm.shipment_type)">
            <span>出库给谁</span>
            <select
              v-model="createForm.recipient_type"
              @change="createForm.recipient_org_id = ''; createForm.recipient_name = ''; createForm.recipient_phone = ''; createForm.recipient_address = ''"
            >
              <option value="agent">代理</option>
              <option value="individual">个人</option>
              <option value="customer">系统终端</option>
            </select>
          </label>
          <label v-if="createForm.recipient_type === 'agent' && ['outbound','exchange','resend'].includes(createForm.shipment_type)">
            <span>选择代理 *</span>
            <select v-model="createForm.recipient_org_id" @change="onAgentChange">
              <option value="">请选择代理</option>
              <option v-for="agent in agents" :key="agent.organization_id" :value="String(agent.organization_id)">
                {{ agent.name }} · {{ agent.username }}
              </option>
            </select>
          </label>
          <label v-if="createForm.recipient_type === 'customer' && ['outbound','exchange','resend'].includes(createForm.shipment_type)">
            <span>终端 ID</span>
            <input v-model="createForm.recipient_customer_id" type="number" />
          </label>
          <label v-if="createForm.shipment_type !== 'transfer'">
            <span>{{ createForm.delivery_method === 'pickup' ? '领取人' : '收件人' }}</span>
            <input v-model="createForm.recipient_name" type="text" />
          </label>
          <label v-if="createForm.shipment_type !== 'transfer'">
            <span>联系电话</span>
            <input v-model="createForm.recipient_phone" type="text" />
          </label>
          <label v-if="createForm.shipment_type !== 'transfer'">
            <span>交付方式 *</span>
            <select v-model="createForm.delivery_method">
              <option value="courier">快递</option>
              <option value="pickup">直接领取 / 自提</option>
            </select>
          </label>
          <label v-if="createForm.shipment_type !== 'transfer' && createForm.delivery_method === 'courier'" class="feature-editor-wide">
            <span>收货地址 *</span>
            <input v-model="createForm.recipient_address" type="text" />
          </label>
          <label v-if="createForm.shipment_type !== 'transfer' && createForm.delivery_method === 'courier'">
            <span>快递公司 *</span>
            <input v-model="createForm.carrier_name" type="text" placeholder="例如 顺丰" />
          </label>
          <label v-if="createForm.shipment_type !== 'transfer' && createForm.delivery_method === 'courier'">
            <span>运单号 *</span>
            <input v-model="createForm.tracking_no" type="text" placeholder="真实运单号" />
          </label>
          <label v-if="createForm.shipment_type !== 'transfer' && createForm.delivery_method === 'courier'">
            <span>物流费用（元） *</span>
            <input v-model="createForm.logistics_fee_yuan" type="number" min="0" step="0.01" placeholder="无费用填 0" />
          </label>
          <div v-if="createForm.shipment_type !== 'transfer' && createForm.delivery_method === 'pickup'" class="feature-editor-wide inventory-v2-pickup-tip">
            直接领取不填写快递单号，后续状态直接推进到“已领取”。
          </div>

          <div class="feature-editor-wide logistics-device-picker">
            <div class="logistics-picker-head">
              <div>
                <strong>选择设备 SN *</strong>
                <span>{{ selectedDeviceIDs.length }} 台已选 · {{ eligibleDevices.length }} 台可选</span>
              </div>
            </div>
            <div class="logistics-device-list">
              <button
                v-for="device in eligibleDevices"
                :key="device.id"
                type="button"
                class="logistics-device-option"
                :class="{ selected: selectedDeviceIDs.includes(device.id) }"
                @click="toggleDevice(device.id)"
              >
                <span class="logistics-check">{{ selectedDeviceIDs.includes(device.id) ? '✓' : '' }}</span>
                <div>
                  <strong>{{ device.sn }}</strong>
                  <small>{{ device.sku_code }} · {{ device.lifecycle_status }} · {{ device.custody_warehouse || '无仓库' }}</small>
                </div>
              </button>
            </div>
          </div>

          <label class="feature-editor-wide"><span>备注</span><textarea v-model="createForm.note" rows="3" /></label>
        </div>

        <div v-else-if="modal === 'status' && selectedShipment" class="feature-editor-grid">
          <div class="inventory-current-state feature-editor-wide">
            <strong>{{ selectedShipment.shipment_no }}</strong>
            <span>{{ statusLabel(selectedShipment.status) }} · {{ selectedShipment.delivery_method === 'pickup' ? '直接领取' : ((selectedShipment.carrier_name || '—') + ' · ' + (selectedShipment.tracking_no || '—')) }}</span>
          </div>
          <label>
            <span>下一状态 *</span>
            <select v-model="statusForm.status" @change="statusForm.description = defaultStatusDescription(statusForm.status)">
              <option value="">请选择</option>
              <option v-for="option in nextStatusOptions" :key="option.value" :value="option.value">
                {{ option.label }}
              </option>
            </select>
          </label>
          <label><span>当前位置</span><input v-model="statusForm.location" type="text" placeholder="例如：南充市顺庆区" /></label>
          <label class="feature-editor-wide"><span>轨迹说明</span><textarea v-model="statusForm.description" rows="4" /></label>
        </div>

        <div v-else-if="modal === 'detail' && selectedShipment" class="logistics-detail">
          <section class="logistics-detail-summary">
            <div><span>物流单</span><strong>{{ selectedShipment.shipment_no }}</strong></div>
            <div><span>交付方式</span><strong>{{ selectedShipment.delivery_method === 'pickup' ? '直接领取' : '快递' }}</strong></div>
            <div><span>运单</span><strong>{{ selectedShipment.tracking_no || '—' }}</strong></div>
            <div><span>物流费用</span><strong>¥{{ (selectedShipment.logistics_fee_cents / 100).toFixed(2) }}</strong></div>
            <div><span>状态</span><strong>{{ statusLabel(selectedShipment.status) }}</strong></div>
            <div><span>设备</span><strong>{{ selectedShipment.items.length }} 台</strong></div>
          </section>

          <section class="logistics-detail-devices">
            <strong>关联设备</strong>
            <div>
              <span v-for="item in selectedShipment.items" :key="item.id">{{ item.sn }} · {{ item.sku_code }}</span>
            </div>
          </section>

          <section class="logistics-timeline">
            <article v-for="event in selectedShipment.events" :key="event.id">
              <i></i>
              <div>
                <header>
                  <strong>{{ statusLabel(event.status) }}</strong>
                  <time>{{ new Date(event.occurred_at).toLocaleString('zh-CN') }}</time>
                </header>
                <p>{{ event.description || '状态更新' }}</p>
                <span v-if="event.location">{{ event.location }}</span>
              </div>
            </article>
          </section>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>

        <footer v-if="modal !== 'detail'">
          <button class="ghost-button" type="button" @click="modal = ''">取消</button>
          <button
            class="primary-button"
            type="button"
            :disabled="saving"
            @click="modal === 'create' ? submitCreate() : submitStatus()"
          >
            {{ saving ? '处理中...' : modal === 'create' ? '创建物流单' : '确认推进' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>
