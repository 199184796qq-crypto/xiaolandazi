<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  acceptInventoryRMA,
  completeInventoryRMA,
  createInventoryBatchInbound,
  createInventoryDevice,
  createInventoryRMA,
  createInventoryRMACost,
  createLogisticsShipment,
  disposeInventoryScrapDevice,
  getInventoryAgents,
  getInventoryDeviceLedger,
  getInventoryDeviceProducts,
  getInventoryDevices,
  getInventoryDocuments,
  getInventoryLedger,
  getInventoryRMAEvents,
  getInventoryRMACosts,
  getInventoryRMAs,
  getInventoryWarehouses,
  getSystemDictionaryItems,
  startInventoryRMARepair,
  transitionInventoryDevice,
  updateLogisticsShipmentStatus,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  AgentSummary,
  CreateLogisticsShipmentInput,
  InventoryCompleteRMAInput,
  InventoryCreateDeviceInput,
  InventoryCreateRMAInput,
  InventoryDevice,
  InventoryDeviceLedgerEntry,
  InventoryDeviceProduct,
  InventoryDeviceTransitionInput,
  InventoryRMA,
  RMACost,
  RMAEvent,
  InventoryStockDocument,
  InventoryWarehouse,
  SystemDictionaryItem,
} from '../types'

const props = withDefaults(
  defineProps<{ focus?: 'devices' | 'inventory' | 'after-sales' }>(),
  { focus: 'devices' },
)
const router = useRouter()

const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const warehouses = ref<InventoryWarehouse[]>([])
const logisticsProviders = ref<SystemDictionaryItem[]>([])
const products = ref<InventoryDeviceProduct[]>([])
const agents = ref<AgentSummary[]>([])
const devices = ref<InventoryDevice[]>([])
const documents = ref<InventoryStockDocument[]>([])
const ledger = ref<InventoryDeviceLedgerEntry[]>([])
const rmas = ref<InventoryRMA[]>([])
const selectedRMAEvents = ref<RMAEvent[]>([])
const selectedRMACosts = ref<RMACost[]>([])
const rmaDetailLoading = ref(false)
const rmaPage = ref(1)
const rmaPageSize = ref(20)
const rmaTotal = ref(0)
const rmaOpenTotal = ref(0)
const rmaLoading = ref(false)
const selectedDeviceLedger = ref<InventoryDeviceLedgerEntry[]>([])

const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('updated-desc')
const page = ref(1)
const pageSize = ref(12)
const viewMode = ref<'card' | 'table'>('table')
const inventorySelectedDeviceID = ref<number | null>(null)
const inventoryRecordTab = ref<'flow' | 'status'>('flow')
const modal = ref<
  | 'batch-inbound'
  | 'outbound'
  | 'create-device'
  | 'transition'
  | 'ledger'
  | 'create-rma'
  | 'rma-detail'
  | 'complete-rma'
  | 'scrap-dispose'
  | ''
>('')
const selectedDevice = ref<InventoryDevice | null>(null)
const selectedRMA = ref<InventoryRMA | null>(null)
const selectedInventoryProductID = ref<number | null>(null)
const selectedOutboundDeviceIDs = ref<number[]>([])
const outboundSelectionMode = ref<'manual' | 'quantity' | 'sn'>('manual')
const outboundBatchFilter = ref('')
const outboundManualSearch = ref('')
const outboundQuantity = ref('')
const outboundSNText = ref('')

const batchInboundForm = reactive({
  product_id: '',
  purchase_no: '',
  supplier_name: '',
  purchase_amount_yuan: '',
  payment_method: 'bank_transfer',
  expected_quantity: 0,
  batch_no: '',
  warehouse_id: '',
  quality_status: 'qualified',
  sns_text: '',
  reason: '采购入库',
})

const outboundForm = reactive({
  product_id: '',
  from_warehouse_id: '',
  recipient_type: 'agent',
  recipient_org_id: '',
  recipient_name: '',
  recipient_phone: '',
  recipient_address: '',
  delivery_method: 'courier',
  carrier_code: '',
  tracking_no: '',
  logistics_fee_yuan: '',
  note: '',
})

const createForm = reactive({
  sn: '',
  sku_code: '',
  batch_no: '',
  owner_org_id: '',
  warehouse_id: '',
  quality_status: 'qualified',
  reason: '采购/生产入库',
})

const transitionForm = reactive({
  to_status: '',
  to_warehouse_id: '',
  to_owner_org_id: '',
  to_customer_id: '',
  reason: '',
  reference_no: '',
})

const rmaForm = reactive({
  device_id: '',
  service_type: 'repair',
  customer_name: '',
  contact_phone: '',
  issue: '',
})

const rmaCostForm = reactive({
  cost_type: 'parts',
  amount_yuan: '',
  counterparty_name: '',
  payment_method: 'bank_transfer',
  note: '',
})

const completeRMAForm = reactive({
  resolution: '',
  to_status: 'ACTIVE',
  to_warehouse_id: '',
  replacement_device_id: '',
})

const scrapDisposalForm = reactive({
  amount_yuan: '',
  buyer_name: '',
  payment_method: 'bank_transfer',
  note: '',
})

const pageTitle = computed(() => {
  if (props.focus === 'inventory') return '设备出入库'
  if (props.focus === 'after-sales') return '售后维修单中心'
  return '设备档案'
})

const canManageInventory = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('inventory.manage')),
  )
})

const canManageAfterSales = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('inventory.after_sales.manage')),
  )
})

const canManageLogistics = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin || access.permissions.includes('logistics.manage')),
  )
})

const statusOptions = computed(() => [
  { label: '全部状态', value: 'all' },
  ...Array.from(new Set(devices.value.map((item) => item.lifecycle_status))).map((value) => ({
    label: deviceStatusLabel(value),
    value,
  })),
])

const sortOptions = [
  { label: '最近更新', value: 'updated-desc' },
  { label: 'SN A-Z', value: 'sn-asc' },
  { label: 'SKU A-Z', value: 'sku-asc' },
]

const filteredDevices = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = devices.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [
        item.sn,
        item.sku_code,
        item.batch_no,
        item.custody_warehouse,
        String(item.owner_org_id || ''),
        String(item.current_customer_id || ''),
      ].some((value) => String(value || '').toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' || item.lifecycle_status === statusFilter.value
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'sn-asc') return a.sn.localeCompare(b.sn, 'zh-CN')
    if (sortMode.value === 'sku-asc') return a.sku_code.localeCompare(b.sku_code, 'zh-CN')
    return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredDevices.value.length / pageSize.value)),
)

const pagedDevices = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredDevices.value.slice(start, start + pageSize.value)
})

const inventorySelectedDevice = computed(
  () =>
    devices.value.find((item) => item.id === inventorySelectedDeviceID.value) ||
    null,
)

const inventoryDeviceLedger = computed(() => {
  if (!inventorySelectedDeviceID.value) return []
  return ledger.value.filter(
    (item) => item.device_id === inventorySelectedDeviceID.value,
  )
})

const inventoryDeviceDocuments = computed(() => {
  const documentIDs = new Set(
    inventoryDeviceLedger.value.map((item) => item.document_id),
  )
  return documents.value.filter((item) => documentIDs.has(item.id))
})

const inventoryProductGroups = computed(() => {
  const bySKU = new Map<string, {
    product_id?: number
    name: string
    sku_code: string
    total: number
    available: number
    transit: number
    repair: number
    scrap: number
  }>()

  for (const product of products.value) {
    bySKU.set(product.sku_code, {
      product_id: product.id,
      name: product.name,
      sku_code: product.sku_code,
      total: 0,
      available: 0,
      transit: 0,
      repair: 0,
      scrap: 0,
    })
  }

  for (const device of devices.value) {
    let group = bySKU.get(device.sku_code)
    if (!group) {
      group = {
        name: device.sku_code,
        sku_code: device.sku_code,
        total: 0,
        available: 0,
        transit: 0,
        repair: 0,
        scrap: 0,
      }
      bySKU.set(device.sku_code, group)
    }
    group.total += 1
    if (device.lifecycle_status === 'IN_STOCK') group.available += 1
    if (['RESERVED', 'IN_TRANSIT', 'RMA_TRANSIT', 'REPAIR_TRANSIT', 'REPAIR_RETURN_TRANSIT'].includes(device.lifecycle_status)) group.transit += 1
    if (['AFTER_SALES', 'REPAIRING', 'EXTERNAL_REPAIR'].includes(device.lifecycle_status)) group.repair += 1
    if (['SCRAP_PENDING', 'SCRAPPED'].includes(device.lifecycle_status)) group.scrap += 1
  }

  const keyword = search.value.trim().toLowerCase()
  return Array.from(bySKU.values()).filter((group) =>
    !keyword ||
    group.name.toLowerCase().includes(keyword) ||
    group.sku_code.toLowerCase().includes(keyword) ||
    devices.value.some((device) =>
      device.sku_code === group.sku_code && device.sn.toLowerCase().includes(keyword),
    ),
  )
})

const selectedInventoryProduct = computed(() =>
  inventoryProductGroups.value.find(
    (item) => item.product_id === selectedInventoryProductID.value,
  ) || null,
)

const selectedInventoryProductDevices = computed(() => {
  if (!selectedInventoryProduct.value) return []
  return devices.value.filter(
    (item) => item.sku_code === selectedInventoryProduct.value?.sku_code,
  )
})

const selectedInventoryProductFilteredDevices = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return selectedInventoryProductDevices.value.filter((item) =>
    !keyword ||
    [item.sn, item.batch_no, item.custody_warehouse, item.lifecycle_status]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})

const selectedInventoryProductTotalPages = computed(() =>
  Math.max(
    1,
    Math.ceil(selectedInventoryProductFilteredDevices.value.length / pageSize.value),
  ),
)

const pagedInventoryProductDevices = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return selectedInventoryProductFilteredDevices.value.slice(
    start,
    start + pageSize.value,
  )
})

const parsedBatchSNs = computed(() => {
  const raw = batchInboundForm.sns_text
    .split(/[\s,，;；]+/)
    .map((item) => item.trim())
    .filter(Boolean)
  return Array.from(new Set(raw))
})

const batchDuplicateCount = computed(() => {
  const raw = batchInboundForm.sns_text
    .split(/[\s,，;；]+/)
    .map((item) => item.trim())
    .filter(Boolean)
  return Math.max(0, raw.length - parsedBatchSNs.value.length)
})

const outboundProduct = computed(() =>
  products.value.find((item) => item.id === Number(outboundForm.product_id)) || null,
)

const outboundEligibleDevices = computed(() => {
  const sku = outboundProduct.value?.sku_code || ''
  const warehouseID = Number(outboundForm.from_warehouse_id) || 0
  return devices.value.filter(
    (item) =>
      item.lifecycle_status === 'IN_STOCK' &&
      (!sku || item.sku_code === sku) &&
      (!warehouseID || item.custody_warehouse_id === warehouseID),
  )
})

const outboundBatchOptions = computed(() =>
  Array.from(
    new Set(
      outboundEligibleDevices.value
        .map((item) => String(item.batch_no || '').trim())
        .filter(Boolean),
    ),
  ).sort((a, b) => a.localeCompare(b, 'zh-CN')),
)

const outboundBatchDevices = computed(() =>
  outboundEligibleDevices.value.filter(
    (item) =>
      !outboundBatchFilter.value ||
      String(item.batch_no || '') === outboundBatchFilter.value,
  ),
)

const outboundManualMatchingDevices = computed(() => {
  const keyword = outboundManualSearch.value.trim().toLowerCase()
  return outboundBatchDevices.value.filter(
    (item) =>
      !keyword ||
      [item.sn, item.sku_code, item.batch_no].some((value) =>
        String(value || '').toLowerCase().includes(keyword),
      ),
  )
})

const outboundManualVisibleDevices = computed(() =>
  outboundManualMatchingDevices.value.slice(0, 80),
)

const parsedOutboundSNs = computed(() =>
  Array.from(
    new Set(
      outboundSNText.value
        .split(/[\s,，;；]+/)
        .map((item) => item.trim())
        .filter(Boolean),
    ),
  ),
)

const outboundSNMatchedDevices = computed(() => {
  const bySN = new Map(
    outboundEligibleDevices.value.map((item) => [
      item.sn.trim().toLowerCase(),
      item,
    ]),
  )
  return parsedOutboundSNs.value
    .map((sn) => bySN.get(sn.toLowerCase()))
    .filter((item): item is InventoryDevice => Boolean(item))
})

const outboundSNMissing = computed(() => {
  const available = new Set(
    outboundEligibleDevices.value.map((item) => item.sn.trim().toLowerCase()),
  )
  return parsedOutboundSNs.value.filter(
    (sn) => !available.has(sn.toLowerCase()),
  )
})

const availableCount = computed(() =>
  devices.value.filter((item) => item.lifecycle_status === 'IN_STOCK').length,
)
const reservedCount = computed(() =>
  devices.value.filter((item) => item.lifecycle_status === 'RESERVED').length,
)
const transitCount = computed(() =>
  devices.value.filter((item) => item.lifecycle_status === 'IN_TRANSIT').length,
)
const afterSalesCount = computed(() =>
  devices.value.filter((item) =>
    ['RMA_TRANSIT', 'AFTER_SALES', 'REPAIRING', 'REPAIR_TRANSIT', 'EXTERNAL_REPAIR', 'REPAIR_RETURN_TRANSIT', 'SCRAP_PENDING'].includes(item.lifecycle_status),
  ).length,
)
const scrappedCount = computed(() =>
  devices.value.filter((item) => item.lifecycle_status === 'SCRAPPED').length,
)

const openRMA = computed(() => rmaOpenTotal.value)
const rmaTotalPages = computed(() =>
  Math.max(1, Math.ceil(rmaTotal.value / rmaPageSize.value)),
)

const allowedTransitions = computed(() => {
  if (!selectedDevice.value) return []
  const map: Record<string, string[]> = {
    INBOUND_PENDING: ['IN_STOCK'],
    IN_STOCK: ['RESERVED', 'IN_TRANSIT', 'AFTER_SALES', 'SCRAP_PENDING'],
    RESERVED: ['IN_STOCK', 'IN_TRANSIT'],
    IN_TRANSIT: ['AGENT_STOCK', 'SOLD', 'IN_STOCK'],
    AGENT_STOCK: ['SOLD', 'IN_TRANSIT', 'AFTER_SALES'],
    SOLD: ['CUSTOMER_BOUND', 'AFTER_SALES'],
    CUSTOMER_BOUND: ['ACTIVE', 'AFTER_SALES'],
    ACTIVE: ['RMA_TRANSIT', 'AFTER_SALES'],
    RMA_TRANSIT: ['AFTER_SALES'],
    AFTER_SALES: ['REPAIRING', 'REPLACED', 'SCRAP_PENDING', 'IN_STOCK', 'ACTIVE'],
    REPAIRING: ['ACTIVE', 'IN_STOCK', 'SCRAP_PENDING', 'REPAIR_TRANSIT', 'EXTERNAL_REPAIR'],
    REPAIR_TRANSIT: ['EXTERNAL_REPAIR', 'REPAIRING'],
    EXTERNAL_REPAIR: ['REPAIR_RETURN_TRANSIT', 'REPAIRING'],
    REPAIR_RETURN_TRANSIT: ['REPAIRING'],
    SCRAP_PENDING: ['SCRAPPED'],
    REPLACED: ['SCRAP_PENDING', 'AFTER_SALES'],
  }
  return map[selectedDevice.value.lifecycle_status] || []
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})
watch(rmaTotalPages, (value) => {
  if (rmaPage.value > value) rmaPage.value = value
})
watch(() => props.focus, () => {
  search.value = ''
  statusFilter.value = 'all'
  page.value = 1
  rmaPage.value = 1
  loadAll()
})

function deviceStatusLabel(value: string) {
  const map: Record<string, string> = {
    INBOUND_PENDING: '待入库',
    IN_STOCK: '在库可用',
    RESERVED: '已锁定',
    IN_TRANSIT: '运输中',
    AGENT_STOCK: '代理库存',
    SOLD: '已销售',
    CUSTOMER_BOUND: '已绑定终端',
    ACTIVE: '正常使用',
    RMA_TRANSIT: '售后运输中',
    AFTER_SALES: '售后处理中',
    REPAIRING: '维修中',
    REPAIR_TRANSIT: '送修运输中',
    EXTERNAL_REPAIR: '外部维修中',
    REPAIR_RETURN_TRANSIT: '维修返回运输中',
    REPLACED: '已换机',
    SCRAP_PENDING: '待报废',
    SCRAPPED: '已报废',
  }
  return map[value] || value
}

function actionLabel(value: string) {
  const map: Record<string, string> = {
    inbound: '入库',
    reserve: '锁库',
    unreserve: '释放锁库',
    outbound: '出库',
    agent_inbound: '代理入库',
    sale: '销售',
    customer_bind: '终端绑定',
    activate: '激活',
    after_sales: '售后',
    rma_open: '发起售后',
    rma_complete: '售后完成',
    scrap: '报废',
    adjust: '调整',
  }
  return map[value] || value
}

function documentTypeLabel(value: string) {
  const map: Record<string, string> = {
    inbound: '采购/生产入库单',
    reserve: '锁库单',
    unreserve: '释放锁库单',
    outbound: '销售/调拨出库单',
    agent_inbound: '代理入库单',
    sale: '销售单',
    customer_bind: '终端绑定单',
    activate: '激活单',
    after_sales: '售后流转单',
    rma_open: '售后发起单',
    rma_complete: '售后完成单',
    scrap: '报废单',
    adjust: '调整单',
  }
  return map[value] || value
}

function serviceTypeLabel(value: string) {
  const map: Record<string, string> = {
    return: '退货',
    exchange: '换货',
    repair: '维修',
    refurbish: '翻新',
    scrap: '报废',
  }
  return map[value] || value
}

function rmaStatusLabel(value: string) {
  const map: Record<string, string> = {
    SUBMITTED: '待受理',
    OPEN: '已受理',
    RETURN_PENDING: '待寄回',
    RETURNING: '退回运输中',
    RETURN_EXCEPTION: '退回物流异常',
    RETURN_CANCELLED: '退回物流已取消',
    PROCESSING: '待检 / 处理中',
    REPAIRING: '内部维修中',
    REPAIR_OUTBOUND: '外送维修运输中',
    EXTERNAL_REPAIR: '外部维修中',
    REPAIR_RETURNING: '维修返回运输中',
    REPLACEMENT_PENDING: '换机待发出',
    RETURN_TO_CUSTOMER_PENDING: '维修完成待寄回',
    OUTBOUND_PENDING: '待出库寄回',
    OUTBOUND_SHIPPING: '寄回运输中',
    OUTBOUND_EXCEPTION: '寄回物流异常',
    REFUND_PENDING: '退款待处理',
    REFUND_PROCESSING: '退款处理中',
    REFUNDED: '已退款',
    COMPLETED: '已完成',
    CANCELLED: '已取消',
  }
  return map[value] || value
}

function rmaStatusTone(value: string) {
  if (value === 'COMPLETED') return 'status-success'
  if (value.includes('EXCEPTION')) return 'status-error'
  if (value === 'CANCELLED' || value === 'RETURN_CANCELLED') return 'status-muted'
  if (
    value.includes('SHIPPING') ||
    value.includes('RETURNING') ||
    value === 'RETURNING' ||
    value === 'REPAIR_OUTBOUND'
  ) {
    return 'status-info'
  }
  return 'status-warning'
}

function canCompleteRMA(item: InventoryRMA) {
  return ['OPEN', 'PROCESSING', 'REPAIRING'].includes(item.status)
}

function deviceSN(id?: number) {
  if (!id) return '—'
  return devices.value.find((item) => item.id === id)?.sn || ('设备 #' + id)
}

function warehouseName(id?: number) {
  if (!id) return '—'
  return warehouses.value.find((item) => item.id === id)?.name || '仓库 #' + id
}

function resetCreateForm() {
  createForm.sn = ''
  createForm.sku_code = ''
  createForm.batch_no = ''
  createForm.owner_org_id = ''
  createForm.warehouse_id = warehouses.value[0]?.id ? String(warehouses.value[0].id) : ''
  createForm.quality_status = 'qualified'
  createForm.reason = '采购/生产入库'
}

function openCreateDevice() {
  resetCreateForm()
  error.value = ''
  modal.value = 'create-device'
}

function openBatchInbound(productID?: number) {
  const mainWarehouse =
    warehouses.value.find((item) => item.code === 'HQ_MAIN') || warehouses.value[0]
  batchInboundForm.product_id = productID
    ? String(productID)
    : products.value[0]?.id
      ? String(products.value[0].id)
      : ''
  batchInboundForm.purchase_no = ''
  batchInboundForm.supplier_name = ''
  batchInboundForm.purchase_amount_yuan = ''
  batchInboundForm.payment_method = 'bank_transfer'
  batchInboundForm.expected_quantity = 0
  batchInboundForm.batch_no = ''
  batchInboundForm.warehouse_id = mainWarehouse?.id ? String(mainWarehouse.id) : ''
  batchInboundForm.quality_status = 'qualified'
  batchInboundForm.sns_text = ''
  batchInboundForm.reason = '采购入库'
  error.value = ''
  modal.value = 'batch-inbound'
}

function openOutbound(productID?: number) {
  const mainWarehouse =
    warehouses.value.find((item) => item.code === 'HQ_MAIN') || warehouses.value[0]
  outboundForm.product_id = productID
    ? String(productID)
    : products.value[0]?.id
      ? String(products.value[0].id)
      : ''
  outboundForm.from_warehouse_id = mainWarehouse?.id ? String(mainWarehouse.id) : ''
  outboundForm.recipient_type = 'agent'
  outboundForm.recipient_org_id = ''
  outboundForm.recipient_name = ''
  outboundForm.recipient_phone = ''
  outboundForm.recipient_address = ''
  outboundForm.delivery_method = 'courier'
  outboundForm.carrier_code = ''
  outboundForm.tracking_no = ''
  outboundForm.logistics_fee_yuan = ''
  outboundForm.note = ''
  selectedOutboundDeviceIDs.value = []
  outboundSelectionMode.value = 'manual'
  outboundBatchFilter.value = ''
  outboundManualSearch.value = ''
  outboundQuantity.value = ''
  outboundSNText.value = ''
  error.value = ''
  modal.value = 'outbound'
}

function selectInventoryProduct(productID?: number) {
  if (!productID) return
  selectedInventoryProductID.value = productID
  page.value = 1
}

function toggleOutboundDevice(deviceID: number) {
  if (selectedOutboundDeviceIDs.value.includes(deviceID)) {
    selectedOutboundDeviceIDs.value = selectedOutboundDeviceIDs.value.filter(
      (item) => item !== deviceID,
    )
  } else {
    selectedOutboundDeviceIDs.value = [...selectedOutboundDeviceIDs.value, deviceID]
  }
}

function toggleAllOutboundDevices() {
  const candidates = outboundManualMatchingDevices.value
  if (
    candidates.length > 0 &&
    candidates.every((item) =>
      selectedOutboundDeviceIDs.value.includes(item.id),
    )
  ) {
    const candidateIDs = new Set(candidates.map((item) => item.id))
    selectedOutboundDeviceIDs.value = selectedOutboundDeviceIDs.value.filter(
      (id) => !candidateIDs.has(id),
    )
    return
  }

  selectedOutboundDeviceIDs.value = Array.from(
    new Set([
      ...selectedOutboundDeviceIDs.value,
      ...candidates.map((item) => item.id),
    ]),
  )
}

function resetOutboundSelection() {
  selectedOutboundDeviceIDs.value = []
  outboundBatchFilter.value = ''
  outboundManualSearch.value = ''
  outboundQuantity.value = ''
  outboundSNText.value = ''
}

function setOutboundSelectionMode(mode: 'manual' | 'quantity' | 'sn') {
  outboundSelectionMode.value = mode
  resetOutboundSelection()
  error.value = ''
}

function applyOutboundQuantitySelection() {
  const quantity = Math.floor(Number(outboundQuantity.value))
  if (!Number.isFinite(quantity) || quantity <= 0) {
    error.value = '请输入大于 0 的出库数量'
    return
  }
  if (quantity > outboundBatchDevices.value.length) {
    error.value =
      '当前条件仅有 ' +
      outboundBatchDevices.value.length +
      ' 台在库可出，不能选择 ' +
      quantity +
      ' 台'
    return
  }

  selectedOutboundDeviceIDs.value = [...outboundBatchDevices.value]
    .sort((a, b) => a.id - b.id)
    .slice(0, quantity)
    .map((item) => item.id)
  error.value = ''
}

function applyOutboundSNSelection() {
  if (!parsedOutboundSNs.value.length) {
    error.value = '请粘贴或导入至少一个 SN'
    return
  }
  if (outboundSNMissing.value.length) {
    error.value =
      '有 ' +
      outboundSNMissing.value.length +
      ' 个 SN 不在当前设备/仓库的可出库库存中，请核对后再出库'
    return
  }

  selectedOutboundDeviceIDs.value = outboundSNMatchedDevices.value.map(
    (item) => item.id,
  )
  error.value = ''
}

async function importOutboundSNFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  const content = await file.text()
  outboundSNText.value = outboundSNText.value
    ? outboundSNText.value + '\n' + content
    : content
  input.value = ''
}

function onOutboundAgentChange() {
  const agent = agents.value.find(
    (item) => item.organization_id === Number(outboundForm.recipient_org_id),
  )
  if (!agent) return
  outboundForm.recipient_name = agent.name || agent.display_name || agent.username
  outboundForm.recipient_phone = agent.phone || ''
}

async function importSNFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const content = await file.text()
  batchInboundForm.sns_text = batchInboundForm.sns_text
    ? batchInboundForm.sns_text + '\n' + content
    : content
  input.value = ''
}

function openTransition(item: InventoryDevice) {
  selectedDevice.value = item
  transitionForm.to_status = ''
  transitionForm.to_warehouse_id = item.custody_warehouse_id ? String(item.custody_warehouse_id) : ''
  transitionForm.to_owner_org_id = item.owner_org_id ? String(item.owner_org_id) : ''
  transitionForm.to_customer_id = item.current_customer_id ? String(item.current_customer_id) : ''
  transitionForm.reason = ''
  transitionForm.reference_no = ''
  error.value = ''
  modal.value = 'transition'
}

async function openLedger(item: InventoryDevice) {
  selectedDevice.value = item
  error.value = ''
  try {
    const data = await getInventoryDeviceLedger(item.id)
    selectedDeviceLedger.value = data.items
    modal.value = 'ledger'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取设备流水失败'
  }
}

function openCreateRMA(item?: InventoryDevice) {
  rmaForm.device_id = item ? String(item.id) : ''
  rmaForm.service_type = 'repair'
  rmaForm.customer_name = ''
  rmaForm.issue = ''
  error.value = ''
  modal.value = 'create-rma'
}

function replacementDevice(item: InventoryRMA) {
  if (!item.replacement_device_id) return null
  return devices.value.find((row) => row.id === item.replacement_device_id) || null
}

function rmaDevice(item: InventoryRMA) {
  return devices.value.find((row) => row.id === item.device_id) || null
}

function warehouseByCode(code: string) {
  return warehouses.value.find((item) => item.code === code) || null
}

function openRMALogistics(
  item: InventoryRMA,
  shipmentType: 'rma_return' | 'repair_outbound' | 'repair_return' | 'exchange' | 'resend',
  deviceID?: number,
) {
  router.push({
    path: '/resources/logistics',
    query: {
      shipment_type: shipmentType,
      business_type: 'rma',
      business_id: String(item.id),
      business_no: item.rma_no,
      device_id: String(deviceID || item.device_id),
      recipient_name: item.customer_name || '',
    },
  })
}

async function moveRMAToRepair(item: InventoryRMA) {
  saving.value = true
  error.value = ''
  try {
    await startInventoryRMARepair(item.id)
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '转入内部维修失败'
  } finally {
    saving.value = false
  }
}

async function acceptRMA(item: InventoryRMA) {
  saving.value = true
  error.value = ''
  try {
    await acceptInventoryRMA(item.id)
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '受理维修单失败'
  } finally {
    saving.value = false
  }
}

function openScrapRMA(item: InventoryRMA) {
  selectedRMA.value = item
  const scrapWarehouse = warehouseByCode('SCRAP_HOLD')
  completeRMAForm.resolution = ''
  completeRMAForm.to_status = 'SCRAP_PENDING'
  completeRMAForm.to_warehouse_id = scrapWarehouse?.id
    ? String(scrapWarehouse.id)
    : ''
  completeRMAForm.replacement_device_id = ''
  error.value = ''
  modal.value = 'complete-rma'
}

function openScrapDisposal(item: InventoryRMA) {
  selectedRMA.value = item
  scrapDisposalForm.amount_yuan = ''
  scrapDisposalForm.buyer_name = ''
  scrapDisposalForm.payment_method = 'bank_transfer'
  scrapDisposalForm.note = ''
  error.value = ''
  modal.value = 'scrap-dispose'
}

async function openRMADetail(item: InventoryRMA) {
  selectedRMA.value = item
  selectedRMAEvents.value = []
  selectedRMACosts.value = []
  rmaCostForm.cost_type = 'parts'
  rmaCostForm.amount_yuan = ''
  rmaCostForm.counterparty_name = ''
  rmaCostForm.payment_method = 'bank_transfer'
  rmaCostForm.note = ''
  error.value = ''
  modal.value = 'rma-detail'
  rmaDetailLoading.value = true
  try {
    const [eventData, costData] = await Promise.all([
      getInventoryRMAEvents(item.id),
      getInventoryRMACosts(item.id),
    ])
    selectedRMAEvents.value = eventData.items
    selectedRMACosts.value = costData.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取维修单详情失败'
  } finally {
    rmaDetailLoading.value = false
  }
}

async function submitRMACost() {
  if (!selectedRMA.value) return
  const amountYuan = Number(rmaCostForm.amount_yuan)
  if (!Number.isFinite(amountYuan) || amountYuan <= 0) {
    error.value = '维修费用金额必须大于 0'
    return
  }
  saving.value = true
  error.value = ''
  try {
    await createInventoryRMACost(selectedRMA.value.id, {
      cost_type: rmaCostForm.cost_type,
      amount_cents: Math.round(amountYuan * 100),
      counterparty_name: rmaCostForm.counterparty_name.trim(),
      payment_method: rmaCostForm.payment_method,
      note: rmaCostForm.note.trim(),
    })
    const costData = await getInventoryRMACosts(selectedRMA.value.id)
    selectedRMACosts.value = costData.items
    rmaCostForm.amount_yuan = ''
    rmaCostForm.counterparty_name = ''
    rmaCostForm.note = ''
  } catch (value) {
    error.value = value instanceof Error ? value.message : '登记维修费用失败'
  } finally {
    saving.value = false
  }
}

async function openRMADeviceLedger(deviceID?: number) {
  if (!deviceID) return
  const item = devices.value.find((row) => row.id === deviceID)
  if (item) await openLedger(item)
}

function openCompleteRMA(item: InventoryRMA) {
  selectedRMA.value = item
  completeRMAForm.resolution = ''
  completeRMAForm.to_status =
    item.service_type === 'scrap'
      ? 'SCRAP_PENDING'
      : item.service_type === 'return'
        ? 'IN_STOCK'
        : item.service_type === 'exchange'
          ? 'REPLACED'
          : 'ACTIVE'
  completeRMAForm.to_warehouse_id = ''
  completeRMAForm.replacement_device_id = ''
  error.value = ''
  modal.value = 'complete-rma'
}

function optionalNumber(value: string) {
  const n = Number(value)
  return value.trim() && Number.isFinite(n) && n > 0 ? n : undefined
}

async function submitBatchInbound() {
  if (!batchInboundForm.product_id || !batchInboundForm.warehouse_id) {
    error.value = '请选择设备名称和入库仓库'
    return
  }
  if (!parsedBatchSNs.value.length) {
    error.value = '请录入至少一个 SN'
    return
  }
  if (!batchInboundForm.supplier_name.trim()) {
    error.value = '请填写设备采购供应商'
    return
  }
  const purchaseAmountYuan = Number(batchInboundForm.purchase_amount_yuan)
  if (!Number.isFinite(purchaseAmountYuan) || purchaseAmountYuan <= 0) {
    error.value = '请填写大于 0 的采购总金额'
    return
  }

  saving.value = true
  error.value = ''
  try {
    const result = await createInventoryBatchInbound({
      product_id: Number(batchInboundForm.product_id),
      batch_no: batchInboundForm.batch_no.trim(),
      purchase_no: batchInboundForm.purchase_no.trim(),
      warehouse_id: Number(batchInboundForm.warehouse_id),
      expected_quantity:
        Number(batchInboundForm.expected_quantity) || parsedBatchSNs.value.length,
      purchase_amount_cents: Math.round(Number(batchInboundForm.purchase_amount_yuan) * 100),
      supplier_name: batchInboundForm.supplier_name.trim(),
      payment_method: batchInboundForm.payment_method,
      sns: parsedBatchSNs.value,
      quality_status: batchInboundForm.quality_status,
      reason: batchInboundForm.reason.trim() || '采购入库',
    })
    selectedInventoryProductID.value = result.product_id
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '批量采购入库失败'
  } finally {
    saving.value = false
  }
}

async function submitOutbound() {
  if (!outboundForm.product_id || !outboundForm.from_warehouse_id) {
    error.value = '请选择设备名称和出库仓库'
    return
  }
  if (!selectedOutboundDeviceIDs.value.length) {
    error.value = '请至少选择一台设备出库'
    return
  }
  if (outboundForm.recipient_type === 'agent' && !outboundForm.recipient_org_id) {
    error.value = '请选择出库代理'
    return
  }
  if (
    outboundForm.delivery_method === 'courier' &&
    String(outboundForm.logistics_fee_yuan).trim() === ''
  ) {
    error.value = '请填写物流费用，无费用请填 0'
    return
  }
  if (
    outboundForm.delivery_method === 'courier' &&
    !outboundForm.carrier_code
  ) {
    error.value = '请选择快递公司'
    return
  }

  const carrier = logisticsProviders.value.find(
    (item) => item.code === outboundForm.carrier_code,
  )

  const payload: CreateLogisticsShipmentInput = {
    shipment_type: 'outbound',
    business_type: 'manual',
    business_no: '',
    from_warehouse_id: Number(outboundForm.from_warehouse_id),
    recipient_org_id:
      outboundForm.recipient_type === 'agent'
        ? Number(outboundForm.recipient_org_id)
        : undefined,
    recipient_type: outboundForm.recipient_type,
    delivery_method: outboundForm.delivery_method,
    logistics_fee_cents:
      outboundForm.delivery_method === 'pickup'
        ? 0
        : Math.round(Number(outboundForm.logistics_fee_yuan || 0) * 100),
    recipient_name: outboundForm.recipient_name.trim(),
    recipient_phone: outboundForm.recipient_phone.trim(),
    recipient_address: outboundForm.recipient_address.trim(),
    carrier_code: carrier?.code || outboundForm.carrier_code,
    carrier_name: carrier?.label || outboundForm.carrier_code,
    tracking_no: outboundForm.tracking_no.trim(),
    device_ids: [...selectedOutboundDeviceIDs.value],
    note: outboundForm.note.trim(),
  }

  saving.value = true
  error.value = ''
  try {
    const shipment = await createLogisticsShipment(payload)
    await updateLogisticsShipmentStatus(shipment.id, {
      status: 'ready_to_ship',
      location: warehouseName(Number(outboundForm.from_warehouse_id)),
      description: '设备已完成出库核对',
    })
    await updateLogisticsShipmentStatus(shipment.id, {
      status: outboundForm.delivery_method === 'pickup' ? 'delivered' : 'shipped',
      location:
        outboundForm.delivery_method === 'pickup'
          ? '仓库现场'
          : warehouseName(Number(outboundForm.from_warehouse_id)),
      description:
        outboundForm.delivery_method === 'pickup'
          ? '设备已由领取人现场领取'
          : '设备已出库并交付快递',
    })
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '设备出库失败'
  } finally {
    saving.value = false
  }
}

async function submitScrapDisposal() {
  if (!selectedRMA.value) return
  const device = rmaDevice(selectedRMA.value)
  if (!device || device.lifecycle_status !== 'SCRAP_PENDING') {
    error.value = '当前设备不在报废待处置状态'
    return
  }
  const amountYuan = Number(scrapDisposalForm.amount_yuan || 0)
  if (!Number.isFinite(amountYuan) || amountYuan < 0) {
    error.value = '处置金额不能小于 0'
    return
  }
  if (amountYuan > 0 && !scrapDisposalForm.buyer_name.trim()) {
    error.value = '有回收收入时必须填写回收方/购买方'
    return
  }

  saving.value = true
  error.value = ''
  try {
    await disposeInventoryScrapDevice(device.id, {
      amount_cents: Math.round(amountYuan * 100),
      buyer_name: scrapDisposalForm.buyer_name.trim(),
      payment_method:
        amountYuan > 0 ? scrapDisposalForm.payment_method : 'none',
      note: scrapDisposalForm.note.trim(),
    })
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '报废处置失败'
  } finally {
    saving.value = false
  }
}

async function submitCreateDevice() {
  const payload: InventoryCreateDeviceInput = {
    sn: createForm.sn.trim(),
    sku_code: createForm.sku_code.trim(),
    batch_no: createForm.batch_no.trim(),
    owner_org_id: optionalNumber(createForm.owner_org_id),
    warehouse_id: Number(createForm.warehouse_id),
    quality_status: createForm.quality_status,
    reason: createForm.reason.trim(),
  }
  saving.value = true
  error.value = ''
  try {
    await createInventoryDevice(payload)
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '设备入库失败'
  } finally {
    saving.value = false
  }
}

async function submitTransition() {
  if (!selectedDevice.value) return
  const payload: InventoryDeviceTransitionInput = {
    to_status: transitionForm.to_status,
    to_warehouse_id: optionalNumber(transitionForm.to_warehouse_id),
    to_owner_org_id: optionalNumber(transitionForm.to_owner_org_id),
    to_customer_id: optionalNumber(transitionForm.to_customer_id),
    reason: transitionForm.reason.trim(),
    reference_no: transitionForm.reference_no.trim(),
  }
  saving.value = true
  error.value = ''
  try {
    await transitionInventoryDevice(selectedDevice.value.id, payload)
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '状态流转失败'
  } finally {
    saving.value = false
  }
}

async function submitCreateRMA() {
  const payload: InventoryCreateRMAInput = {
    device_id: Number(rmaForm.device_id),
    service_type: rmaForm.service_type,
    customer_name: rmaForm.customer_name.trim(),
    contact_phone: rmaForm.contact_phone.trim(),
    issue: rmaForm.issue.trim(),
  }
  saving.value = true
  error.value = ''
  try {
    await createInventoryRMA(payload)
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '创建售后单失败'
  } finally {
    saving.value = false
  }
}

async function submitCompleteRMA() {
  if (!selectedRMA.value) return
  const payload: InventoryCompleteRMAInput = {
    resolution: completeRMAForm.resolution.trim(),
    to_status: completeRMAForm.to_status,
    to_warehouse_id: optionalNumber(completeRMAForm.to_warehouse_id),
    replacement_device_id: optionalNumber(completeRMAForm.replacement_device_id),
  }
  saving.value = true
  error.value = ''
  try {
    await completeInventoryRMA(selectedRMA.value.id, payload)
    modal.value = ''
    await loadAll()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '完成售后失败'
  } finally {
    saving.value = false
  }
}

async function loadRMAData() {
  rmaLoading.value = true
  error.value = ''
  try {
    const data = await getInventoryRMAs(rmaPage.value, rmaPageSize.value)
    rmas.value = data.items
    rmaTotal.value = data.total
    rmaOpenTotal.value = data.open_total
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取售后单失败'
  } finally {
    rmaLoading.value = false
  }
}

async function goRMAPage(value: number) {
  const next = Math.min(Math.max(value, 1), rmaTotalPages.value)
  if (next === rmaPage.value) return
  rmaPage.value = next
  await loadRMAData()
}

async function changeRMAPageSize() {
  rmaPage.value = 1
  await loadRMAData()
}
async function loadAll() {
  loading.value = true
  error.value = ''
  try {
    const [
      warehouseData,
      productData,
      agentData,
      deviceData,
      documentData,
      ledgerData,
      rmaData,
      logisticsData,
    ] = await Promise.all([
      getInventoryWarehouses(),
      getInventoryDeviceProducts(),
      getInventoryAgents(),
      getInventoryDevices(),
      getInventoryDocuments(),
      getInventoryLedger(),
      getInventoryRMAs(rmaPage.value, rmaPageSize.value),
      getSystemDictionaryItems('logistics_provider'),
    ])
    warehouses.value = warehouseData.items
    products.value = productData.items
    agents.value = agentData.items
    devices.value = deviceData.items
    documents.value = documentData.items
    ledger.value = ledgerData.items
    rmas.value = rmaData.items
    logisticsProviders.value = logisticsData.items
    rmaTotal.value = rmaData.total
    rmaOpenTotal.value = rmaData.open_total
    if (
      !selectedInventoryProductID.value ||
      !products.value.some((item) => item.id === selectedInventoryProductID.value)
    ) {
      selectedInventoryProductID.value = products.value[0]?.id || null
    }
    if (
      !inventorySelectedDeviceID.value ||
      !devices.value.some((item) => item.id === inventorySelectedDeviceID.value)
    ) {
      inventorySelectedDeviceID.value = devices.value[0]?.id || null
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取库存数据失败'
  } finally {
    loading.value = false
  }
}

onMounted(loadAll)
</script>

<template>
  <div class="management-page inventory-lifecycle-page">
    <ModulePageNav :context="focus === 'after-sales' ? 'staff-after-sales' : 'resources'" :active-title="focus === 'after-sales' ? '售后维修' : pageTitle" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">
          {{ focus === 'inventory' ? 'WAREHOUSE & STOCK' : focus === 'after-sales' ? 'AFTER-SALES SERVICE' : 'DEVICE REGISTRY' }}
        </p>
        <h2>{{ pageTitle }}</h2>
      </div>

      <div class="inventory-hero-actions">
        <button class="ghost-button" type="button" :disabled="loading" @click="loadAll">
          {{ loading ? '刷新中...' : '刷新数据' }}
        </button>
        <button
          v-if="focus === 'devices' && canManageInventory"
          class="primary-button"
          type="button"
          @click="openCreateDevice"
        >
          ＋ 设备入库
        </button>
        <button
          v-if="focus === 'inventory' && canManageInventory"
          class="primary-button"
          type="button"
          @click="openBatchInbound(selectedInventoryProduct?.product_id)"
        >
          ＋ 采购入库
        </button>
        <button
          v-if="focus === 'inventory' && canManageInventory && canManageLogistics"
          class="ghost-button"
          type="button"
          @click="openOutbound(selectedInventoryProduct?.product_id)"
        >
          设备出库
        </button>
        <button
          v-if="focus === 'after-sales' && canManageAfterSales"
          class="primary-button"
          type="button"
          @click="openCreateRMA()"
        >
          ＋ 手工录入维修单
        </button>
      </div>
    </section>

    <section v-if="focus !== 'after-sales'" class="module-hub-metrics-v2">
      <article class="module-hub-metric-v2 tone-primary">
        <span>设备总数</span><strong>{{ devices.length }}</strong><small>所有 SN 档案</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>在库可用</span><strong>{{ availableCount }}</strong><small>IN_STOCK</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>锁定 / 在途</span><strong>{{ reservedCount + transitCount }}</strong><small>{{ reservedCount }} 锁定 · {{ transitCount }} 在途</small>
      </article>
      <article class="module-hub-metric-v2 tone-warning">
        <span>售后 / 报废</span><strong>{{ afterSalesCount + scrappedCount }}</strong><small>{{ afterSalesCount }} 售后 · {{ scrappedCount }} 报废</small>
      </article>
    </section>

    <section v-else class="module-hub-metrics-v2 after-sales-project-metrics">
      <article class="module-hub-metric-v2 tone-warning">
        <span>处理中维修单</span><strong>{{ openRMA }}</strong><small>待受理、物流、检测和维修中的工单</small>
      </article>
      <article class="module-hub-metric-v2 tone-primary">
        <span>累计维修单</span><strong>{{ rmaTotal }}</strong><small>全部售后维修单</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>售后 / 维修设备</span><strong>{{ afterSalesCount }}</strong><small>当前在售后、维修和往返链路中的设备</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>已结束</span><strong>{{ Math.max(rmaTotal - openRMA, 0) }}</strong><small>已完成或已取消的维修单</small>
      </article>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>

    <section v-if="focus === 'devices'" class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="SN / SKU / 批次 / 仓库 / 组织 / 终端"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="loading" class="panel-loading">正在读取设备档案...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>设备 SN</th>
              <th>SKU / 批次</th>
              <th>当前仓库</th>
              <th>所有权组织</th>
              <th>当前终端</th>
              <th>生命周期</th>
              <th>质检</th>
              <th>更新时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedDevices" :key="item.id">
              <td><strong>{{ item.sn }}</strong><small>设备 #{{ item.id }}</small></td>
              <td><span>{{ item.sku_code }}</span><small>{{ item.batch_no || '无批次' }}</small></td>
              <td>{{ item.custody_warehouse || '—' }}</td>
              <td>{{ item.owner_org_id ? '#' + item.owner_org_id : '—' }}</td>
              <td>{{ item.current_customer_id ? '#' + item.current_customer_id : '—' }}</td>
              <td><span class="status-pill">{{ deviceStatusLabel(item.lifecycle_status) }}</span></td>
              <td>{{ item.quality_status === 'qualified' ? '合格' : '不良' }}</td>
              <td>{{ new Date(item.updated_at).toLocaleString('zh-CN') }}</td>
              <td>
                <div class="table-actions">
                  <button class="text-action" type="button" @click="openLedger(item)">全链</button>
                  <button
                    v-if="canManageInventory && item.lifecycle_status !== 'SCRAPPED'"
                    class="text-action"
                    type="button"
                    @click="openTransition(item)"
                  >
                    流转
                  </button>
                  <button
                    v-if="canManageAfterSales && ['ACTIVE','CUSTOMER_BOUND','SOLD','AGENT_STOCK','IN_STOCK'].includes(item.lifecycle_status)"
                    class="text-action"
                    type="button"
                    @click="openCreateRMA(item)"
                  >
                    售后
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredDevices.length === 0" class="empty-state">暂无符合条件的设备。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in pagedDevices" :key="item.id" class="feature-record-card">
          <header>
            <div><span>{{ item.sku_code }}</span><h3>{{ item.sn }}</h3></div>
            <span class="status-pill">{{ deviceStatusLabel(item.lifecycle_status) }}</span>
          </header>
          <dl>
            <div><dt>批次</dt><dd>{{ item.batch_no || '—' }}</dd></div>
            <div><dt>仓库</dt><dd>{{ item.custody_warehouse || '—' }}</dd></div>
            <div><dt>所有权</dt><dd>{{ item.owner_org_id ? '#' + item.owner_org_id : '—' }}</dd></div>
            <div><dt>当前终端</dt><dd>{{ item.current_customer_id ? '#' + item.current_customer_id : '—' }}</dd></div>
          </dl>
          <footer>
            <button class="text-action" type="button" @click="openLedger(item)">全链</button>
            <button v-if="canManageInventory && item.lifecycle_status !== 'SCRAPPED'" class="text-action" type="button" @click="openTransition(item)">流转</button>
          </footer>
        </article>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredDevices.length"
        :page-size="pageSize"
      />
    </section>

    <template v-else-if="focus === 'inventory'">
      <section class="settings-card inventory-v2-product-panel">
        <header class="inventory-section-head">
          <div>
            <strong>设备库存</strong>
            <span>先按设备名称汇总，再下钻到每一台 SN 实物。</span>
          </div>
          <input
            v-model="search"
            class="text-input inventory-v2-search"
            type="search"
            placeholder="搜索设备名称 / SKU / SN"
          />
        </header>

        <div class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>设备名称</th>
                <th>SKU</th>
                <th>总台数</th>
                <th>在库可用</th>
                <th>在途</th>
                <th>售后 / 维修</th>
                <th>报废</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in inventoryProductGroups"
                :key="item.sku_code"
                class="clickable-table-row"
                :class="{ active: item.product_id === selectedInventoryProductID }"
                @click="selectInventoryProduct(item.product_id)"
              >
                <td><strong>{{ item.name }}</strong></td>
                <td>{{ item.sku_code }}</td>
                <td><strong>{{ item.total }}</strong></td>
                <td>{{ item.available }}</td>
                <td>{{ item.transit }}</td>
                <td>{{ item.repair }}</td>
                <td>{{ item.scrap }}</td>
                <td>
                  <div class="table-actions">
                    <button
                      v-if="canManageInventory && item.product_id"
                      class="text-action"
                      type="button"
                      @click.stop="openBatchInbound(item.product_id)"
                    >
                      采购入库
                    </button>
                    <button
                      v-if="canManageInventory && canManageLogistics && item.product_id"
                      class="text-action"
                      type="button"
                      @click.stop="openOutbound(item.product_id)"
                    >
                      设备出库
                    </button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-if="inventoryProductGroups.length === 0" class="empty-state">
            暂无设备商品或库存记录。
          </div>
        </div>
      </section>

      <section class="settings-card inventory-v2-sn-panel">
        <header class="inventory-section-head">
          <div v-if="selectedInventoryProduct">
            <strong>{{ selectedInventoryProduct.name }}</strong>
            <span>
              {{ selectedInventoryProduct.sku_code }} ·
              共 {{ selectedInventoryProductDevices.length }} 台实物设备
            </span>
          </div>
          <div v-else>
            <strong>SN 实物设备</strong>
            <span>从上方选择一个设备名称查看具体设备。</span>
          </div>
          <select v-model.number="pageSize" class="text-input inventory-v2-page-size">
            <option :value="20">每页 20 台</option>
            <option :value="50">每页 50 台</option>
            <option :value="100">每页 100 台</option>
          </select>
        </header>

        <div v-if="selectedInventoryProduct" class="data-table-wrap">
          <table class="data-table">
            <thead>
              <tr>
                <th>内部 ID</th>
                <th>SN</th>
                <th>批次</th>
                <th>当前仓库</th>
                <th>当前状态</th>
                <th>品质</th>
                <th>当前归属</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in pagedInventoryProductDevices" :key="item.id">
                <td>#{{ item.id }}</td>
                <td><strong>{{ item.sn }}</strong></td>
                <td>{{ item.batch_no || '—' }}</td>
                <td>{{ item.custody_warehouse || '—' }}</td>
                <td><span class="status-pill">{{ deviceStatusLabel(item.lifecycle_status) }}</span></td>
                <td>{{ item.quality_status === 'qualified' ? '合格' : '不良' }}</td>
                <td>
                  {{
                    item.current_customer_id
                      ? '终端 #' + item.current_customer_id
                      : item.owner_org_id
                        ? '组织 #' + item.owner_org_id
                        : '公司'
                  }}
                </td>
                <td>
                  <button class="text-action" type="button" @click="openLedger(item)">
                    查看全链
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
          <div v-if="selectedInventoryProductFilteredDevices.length === 0" class="empty-state">
            该设备名称下暂无符合条件的 SN。
          </div>
        </div>
        <div v-else class="empty-state">请选择一个设备名称。</div>

        <PaginationBar
          v-if="selectedInventoryProduct"
          v-model:page="page"
          :total-pages="selectedInventoryProductTotalPages"
          :total="selectedInventoryProductFilteredDevices.length"
          :page-size="pageSize"
        />
      </section>
    </template>

    <section v-else class="settings-card inventory-summary-panel">
      <header class="inventory-section-head">
        <div>
          <strong>售后单</strong>
          <span>{{ openRMA }} 个处理中 · {{ rmaTotal }} 个累计</span>
        </div>
        <label class="rma-page-size-control">
          <span>每页</span>
          <select v-model.number="rmaPageSize" @change="changeRMAPageSize">
            <option :value="20">20 条</option>
            <option :value="50">50 条</option>
            <option :value="100">100 条</option>
          </select>
        </label>

      </header>

      <div v-if="loading || rmaLoading" class="panel-loading">正在读取售后数据...</div>
      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr><th>售后单</th><th>设备 SN</th><th>类型</th><th>终端/代理</th><th>状态</th><th>原订单 / 物流链</th><th>问题</th><th>处理结果</th><th>时间</th><th>操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="item in rmas" :key="item.id" class="clickable-table-row" tabindex="0" @click="openRMADetail(item)" @keydown.enter="openRMADetail(item)">
              <td><button class="table-link-button" type="button" @click.stop="openRMADetail(item)">{{ item.rma_no }}</button></td>
              <td>{{ item.device_sn }}</td>
              <td>{{ serviceTypeLabel(item.service_type) }}</td>
              <td>
                <div class="rma-party-cell">
                  <strong>{{ item.customer_name || '—' }}</strong>
                  <small>{{ item.source_type === 'customer' ? '终端发起' : item.source_type === 'agent' ? '代理发起' : '内部录入' }}{{ item.contact_phone ? ' · ' + item.contact_phone : '' }}</small>
                </div>
              </td>
              <td>
                <span class="status-pill" :class="rmaStatusTone(item.status)">
                  {{ rmaStatusLabel(item.status) }}
                </span>
              </td>
              <td class="table-drilldown-cell" title="点击查看完整物流链">
                <div class="rma-trace-cell table-two-line-stack">
                  <span>订单：{{ item.source_order_no || '—' }}</span>
                  <span>原运单：{{ item.source_shipment_no || '—' }}</span>
                  <span>退回：{{ item.return_shipment_no || '—' }}</span>
                  <span>外送维修：{{ item.repair_outbound_shipment_no || '—' }}</span>
                  <span>维修返回：{{ item.repair_return_shipment_no || '—' }}</span>
                  <span>寄出：{{ item.outbound_shipment_no || '—' }}</span>
                </div>
                <span class="table-more-hint">点击查看完整链路</span>
              </td>
              <td class="table-drilldown-cell">
                <small class="table-two-line-clamp">{{ item.issue || '—' }}</small>
              </td>
              <td class="table-drilldown-cell">
                <small class="table-two-line-clamp">{{ item.resolution || '—' }}</small>
              </td>
              <td>{{ new Date(item.completed_at || item.created_at).toLocaleString('zh-CN') }}</td>
              <td>
                <div class="table-actions">
                  <button
                    v-if="canManageAfterSales && item.status === 'SUBMITTED'"
                    class="text-action"
                    type="button"
                    @click.stop="acceptRMA(item)"
                  >
                    受理
                  </button>
                  <button
                    v-if="canManageLogistics && item.status === 'RETURN_PENDING'"
                    class="text-action"
                    type="button"
                    @click.stop="openRMALogistics(item, 'rma_return')"
                  >
                    登记退回
                  </button>
                  <button
                    v-if="canManageAfterSales && item.status === 'PROCESSING' && rmaDevice(item)?.lifecycle_status === 'AFTER_SALES' && ['repair','refurbish'].includes(item.service_type)"
                    class="text-action"
                    type="button"
                    @click.stop="moveRMAToRepair(item)"
                  >
                    转维修库
                  </button>
                  <button
                    v-if="canManageLogistics && item.status === 'REPAIRING' && rmaDevice(item)?.lifecycle_status === 'REPAIRING'"
                    class="text-action"
                    type="button"
                    @click.stop="openRMALogistics(item, 'repair_outbound')"
                  >
                    外送维修
                  </button>
                  <button
                    v-if="canManageLogistics && item.status === 'EXTERNAL_REPAIR' && rmaDevice(item)?.lifecycle_status === 'EXTERNAL_REPAIR'"
                    class="text-action"
                    type="button"
                    @click.stop="openRMALogistics(item, 'repair_return')"
                  >
                    维修返回
                  </button>
                  <button
                    v-if="canManageLogistics && item.status === 'REPLACEMENT_PENDING' && item.replacement_device_id"
                    class="text-action"
                    type="button"
                    @click.stop="openRMALogistics(item, 'exchange', item.replacement_device_id)"
                  >
                    发出换机设备
                  </button>
                  <button
                    v-if="canManageLogistics && item.status === 'RETURN_TO_CUSTOMER_PENDING'"
                    class="text-action"
                    type="button"
                    @click.stop="openRMALogistics(item, 'resend')"
                  >
                    返还终端
                  </button>
                  <button
                    v-if="canManageAfterSales && canCompleteRMA(item)"
                    class="text-action"
                    type="button"
                    @click.stop="openCompleteRMA(item)"
                  >
                    处理结果
                  </button>
                  <button
                    v-if="canManageAfterSales && rmaDevice(item)?.lifecycle_status === 'SCRAP_PENDING'"
                    class="text-action"
                    type="button"
                    @click.stop="openScrapDisposal(item)"
                  >
                    处置报废
                  </button>
                  <button
                    v-if="canManageAfterSales && canCompleteRMA(item) && rmaDevice(item)?.lifecycle_status !== 'SCRAPPED'"
                    class="text-action"
                    type="button"
                    @click.stop="openScrapRMA(item)"
                  >
                    报废待处置
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="rmas.length === 0" class="empty-state">暂无售后单。</div>
      </div>

      <PaginationBar
        :page="rmaPage"
        :total-pages="rmaTotalPages"
        :total="rmaTotal"
        :page-size="rmaPageSize"
        @update:page="goRMAPage"
      />
    </section>

    <div v-if="modal" class="feature-editor-backdrop" @click.self="modal = ''">
      <section
        class="feature-editor-panel inventory-editor-panel"
        :class="{
          'inventory-rma-detail-modal': modal === 'rma-detail',
          'inventory-compact-operation-modal': modal === 'batch-inbound' || modal === 'outbound',
          'inventory-inbound-operation-modal': modal === 'batch-inbound',
          'inventory-outbound-operation-modal': modal === 'outbound',
        }"
      >
        <header>
          <div>
            <span class="section-kicker">INVENTORY OPERATION</span>
            <h3>
              {{
                modal === 'batch-inbound' ? '采购入库' :
                modal === 'outbound' ? '设备出库' :
                modal === 'create-device' ? '单台设备入库' :
                modal === 'transition' ? '设备状态流转' :
                modal === 'ledger' ? 'SN 全链路' :
                modal === 'create-rma' ? '发起售后' :
                modal === 'rma-detail' ? '售后详情' :
                modal === 'scrap-dispose' ? '报废处置' : '完成售后'
              }}
            </h3>
          </div>
          <button class="icon-button" type="button" @click="modal = ''">×</button>
        </header>

        <div v-if="modal === 'batch-inbound'" class="feature-editor-grid inventory-operation-form inventory-inbound-form">
          <label>
            <span>设备名称 *</span>
            <select v-model="batchInboundForm.product_id">
              <option value="">请选择设备</option>
              <option
                v-for="product in products"
                :key="product.id"
                :value="String(product.id)"
              >
                {{ product.name }} · {{ product.sku_code }}
              </option>
            </select>
          </label>
          <label>
            <span>采购单号</span>
            <input v-model="batchInboundForm.purchase_no" type="text" placeholder="采购单 / 到货单号" />
          </label>
          <label>
            <span>供应商 *</span>
            <input v-model="batchInboundForm.supplier_name" type="text" placeholder="设备供应商 / 厂家" />
          </label>
          <label>
            <span>采购总金额（元） *</span>
            <input v-model="batchInboundForm.purchase_amount_yuan" type="number" min="0.01" step="0.01" placeholder="例如 8000.00" />
          </label>
          <label>
            <span>支付方式 *</span>
            <select v-model="batchInboundForm.payment_method">
              <option value="bank_transfer">银行转账</option>
              <option value="wechat">微信</option>
              <option value="alipay">支付宝</option>
              <option value="cash">现金</option>
              <option value="other">其他</option>
            </select>
          </label>
          <label>
            <span>计划数量 *</span>
            <input v-model.number="batchInboundForm.expected_quantity" type="number" min="1" placeholder="例如 1000" />
          </label>
          <label>
            <span>采购批次</span>
            <input v-model="batchInboundForm.batch_no" type="text" placeholder="例如 20260922-A" />
          </label>
          <label>
            <span>入库仓库 *</span>
            <select v-model="batchInboundForm.warehouse_id">
              <option value="">请选择仓库</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">
                {{ warehouse.name }}
              </option>
            </select>
          </label>
          <label>
            <span>质检结果</span>
            <select v-model="batchInboundForm.quality_status">
              <option value="qualified">合格</option>
              <option value="defective">不良</option>
            </select>
          </label>

          <div class="feature-editor-wide inventory-v2-sn-input">
            <div class="inventory-v2-sn-input-head">
              <div>
                <strong>设备 SN *</strong>
                <span>支持扫码枪连续录入、粘贴一列 SN，或导入 TXT / CSV。</span>
              </div>
              <label class="ghost-button inventory-v2-file-button">
                导入文件
                <input type="file" accept=".txt,.csv,text/plain,text/csv" @change="importSNFile" />
              </label>
            </div>
            <textarea
              v-model="batchInboundForm.sns_text"
              rows="7"
              placeholder="每行一个 SN，也支持用逗号、空格分隔"
            />
            <div class="inventory-v2-sn-stats">
              <span>计划 {{ batchInboundForm.expected_quantity || 0 }} 台</span>
              <span>已录 {{ parsedBatchSNs.length }} 台</span>
              <span v-if="batchDuplicateCount">重复 {{ batchDuplicateCount }} 条已自动去重</span>
              <span
                v-if="batchInboundForm.expected_quantity > 0 && parsedBatchSNs.length < batchInboundForm.expected_quantity"
              >
                还差 {{ batchInboundForm.expected_quantity - parsedBatchSNs.length }} 台
              </span>
            </div>
          </div>
          <label class="feature-editor-wide">
            <span>入库备注</span>
            <textarea v-model="batchInboundForm.reason" rows="2" />
          </label>
        </div>

        <div v-else-if="modal === 'outbound'" class="feature-editor-grid inventory-operation-form inventory-outbound-form">
          <label>
            <span>设备名称 *</span>
            <select v-model="outboundForm.product_id" @change="resetOutboundSelection">
              <option value="">请选择设备</option>
              <option v-for="product in products" :key="product.id" :value="String(product.id)">
                {{ product.name }} · {{ product.sku_code }}
              </option>
            </select>
          </label>
          <label>
            <span>出库仓库 *</span>
            <select v-model="outboundForm.from_warehouse_id" @change="resetOutboundSelection">
              <option value="">请选择仓库</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">
                {{ warehouse.name }}
              </option>
            </select>
          </label>

          <div class="feature-editor-wide inventory-outbound-selector">
            <div class="inventory-outbound-selector-head">
              <div>
                <strong>选择出库设备 *</strong>
                <span>
                  {{ selectedOutboundDeviceIDs.length }} 台已选 ·
                  {{ outboundEligibleDevices.length }} 台在库可出
                </span>
              </div>
              <button
                v-if="selectedOutboundDeviceIDs.length"
                class="text-action"
                type="button"
                @click="selectedOutboundDeviceIDs = []"
              >
                清空已选
              </button>
            </div>

            <div class="inventory-outbound-mode-tabs">
              <button
                type="button"
                :class="{ active: outboundSelectionMode === 'manual' }"
                @click="setOutboundSelectionMode('manual')"
              >
                少量勾选
                <small>1～20 台</small>
              </button>
              <button
                type="button"
                :class="{ active: outboundSelectionMode === 'quantity' }"
                @click="setOutboundSelectionMode('quantity')"
              >
                按数量批量
                <small>几十 / 几百台</small>
              </button>
              <button
                type="button"
                :class="{ active: outboundSelectionMode === 'sn' }"
                @click="setOutboundSelectionMode('sn')"
              >
                SN 清单
                <small>精确指定设备</small>
              </button>
            </div>

            <div
              v-if="outboundSelectionMode === 'manual'"
              class="inventory-outbound-mode-panel"
            >
              <div class="inventory-outbound-filter-row">
                <input
                  v-model="outboundManualSearch"
                  type="search"
                  placeholder="搜索 SN / SKU / 批次"
                />
                <select
                  v-model="outboundBatchFilter"
                  @change="selectedOutboundDeviceIDs = []"
                >
                  <option value="">全部批次</option>
                  <option
                    v-for="batch in outboundBatchOptions"
                    :key="batch"
                    :value="batch"
                  >
                    {{ batch }}
                  </option>
                </select>
                <button
                  class="ghost-button"
                  type="button"
                  :disabled="!outboundManualMatchingDevices.length"
                  @click="toggleAllOutboundDevices"
                >
                  全选匹配
                </button>
              </div>

              <div class="logistics-device-list inventory-v2-outbound-list">
                <button
                  v-for="device in outboundManualVisibleDevices"
                  :key="device.id"
                  type="button"
                  class="logistics-device-option"
                  :class="{ selected: selectedOutboundDeviceIDs.includes(device.id) }"
                  @click="toggleOutboundDevice(device.id)"
                >
                  <span class="logistics-check">
                    {{ selectedOutboundDeviceIDs.includes(device.id) ? '✓' : '' }}
                  </span>
                  <div>
                    <strong>#{{ device.id }} · {{ device.sn }}</strong>
                    <small>
                      {{ device.sku_code }} ·
                      {{ device.batch_no || '无批次' }} ·
                      {{ device.custody_warehouse || '无仓库' }}
                    </small>
                  </div>
                </button>

                <div
                  v-if="outboundManualMatchingDevices.length === 0"
                  class="empty-state"
                >
                  当前条件没有可出库设备。
                </div>
              </div>

              <p
                v-if="outboundManualMatchingDevices.length > outboundManualVisibleDevices.length"
                class="inventory-outbound-list-note"
              >
                匹配 {{ outboundManualMatchingDevices.length }} 台，当前仅展示前
                {{ outboundManualVisibleDevices.length }} 台；可继续输入 SN 缩小范围，
                或切换“按数量批量”。
              </p>
            </div>

            <div
              v-else-if="outboundSelectionMode === 'quantity'"
              class="inventory-outbound-mode-panel"
            >
              <div class="inventory-outbound-batch-controls">
                <label>
                  <span>采购 / 入库批次</span>
                  <select
                    v-model="outboundBatchFilter"
                    @change="selectedOutboundDeviceIDs = []"
                  >
                    <option value="">全部批次混合出库</option>
                    <option
                      v-for="batch in outboundBatchOptions"
                      :key="batch"
                      :value="batch"
                    >
                      {{ batch }}
                    </option>
                  </select>
                </label>
                <label>
                  <span>出库数量 *</span>
                  <input
                    v-model="outboundQuantity"
                    type="number"
                    min="1"
                    :max="outboundBatchDevices.length"
                    placeholder="例如 100"
                  />
                </label>
                <button
                  class="primary-button"
                  type="button"
                  @click="applyOutboundQuantitySelection"
                >
                  自动选择设备
                </button>
              </div>

              <div class="inventory-outbound-batch-summary">
                <div>
                  <span>当前条件可出</span>
                  <strong>{{ outboundBatchDevices.length }} 台</strong>
                </div>
                <p>
                  系统按内部设备 ID 从小到大自动选择实际设备。提交后仍会逐台记录
                  SN、出库流水和物流去向，不会只记一个数量。
                </p>
              </div>
            </div>

            <div v-else class="inventory-outbound-mode-panel">
              <div class="inventory-outbound-sn-head">
                <div>
                  <strong>粘贴 / 导入 SN 清单</strong>
                  <span>适合订单系统或仓库扫描结果已经明确指定具体设备的情况。</span>
                </div>
                <label class="ghost-button inventory-v2-file-button">
                  导入 TXT / CSV
                  <input
                    type="file"
                    accept=".txt,.csv,text/plain,text/csv"
                    @change="importOutboundSNFile"
                  />
                </label>
              </div>

              <textarea
                v-model="outboundSNText"
                rows="6"
                placeholder="每行一个 SN，也支持逗号、空格分隔"
              />

              <div class="inventory-outbound-sn-stats">
                <span>录入 {{ parsedOutboundSNs.length }} 个 SN</span>
                <span>可匹配 {{ outboundSNMatchedDevices.length }} 台</span>
                <span v-if="outboundSNMissing.length" class="danger">
                  异常 {{ outboundSNMissing.length }} 个
                </span>
              </div>

              <div
                v-if="outboundSNMissing.length"
                class="inventory-outbound-sn-missing"
              >
                <strong>以下 SN 当前不可出库：</strong>
                <span>
                  {{ outboundSNMissing.slice(0, 12).join('、') }}
                  {{ outboundSNMissing.length > 12 ? ' 等' : '' }}
                </span>
              </div>

              <button
                class="primary-button inventory-outbound-sn-apply"
                type="button"
                @click="applyOutboundSNSelection"
              >
                校验并选择这些设备
              </button>
            </div>

            <div
              v-if="selectedOutboundDeviceIDs.length"
              class="inventory-outbound-selected-summary"
            >
              <div>
                <span>本次准备出库</span>
                <strong>{{ selectedOutboundDeviceIDs.length }} 台</strong>
              </div>
              <p>已锁定到具体设备 ID / SN，下面继续填写收货对象和物流信息。</p>
            </div>
          </div>


          <label>
            <span>出库给谁 *</span>
            <select
              v-model="outboundForm.recipient_type"
              @change="outboundForm.recipient_org_id = ''; outboundForm.recipient_name = ''; outboundForm.recipient_phone = ''; outboundForm.recipient_address = ''"
            >
              <option value="agent">代理</option>
              <option value="individual">个人</option>
            </select>
          </label>
          <label v-if="outboundForm.recipient_type === 'agent'">
            <span>选择代理 *</span>
            <select v-model="outboundForm.recipient_org_id" @change="onOutboundAgentChange">
              <option value="">请选择代理</option>
              <option v-for="agent in agents" :key="agent.organization_id" :value="String(agent.organization_id)">
                {{ agent.name }} · {{ agent.username }}
              </option>
            </select>
          </label>
          <label>
            <span>{{ outboundForm.delivery_method === 'pickup' ? '领取人' : '收件人' }} *</span>
            <input v-model="outboundForm.recipient_name" type="text" />
          </label>
          <label><span>联系电话 *</span><input v-model="outboundForm.recipient_phone" type="text" /></label>
          <label>
            <span>交付方式 *</span>
            <select v-model="outboundForm.delivery_method">
              <option value="courier">快递</option>
              <option value="pickup">直接领取 / 自提</option>
            </select>
          </label>
          <label v-if="outboundForm.delivery_method === 'courier'" class="feature-editor-wide">
            <span>收货地址 *</span>
            <input v-model="outboundForm.recipient_address" type="text" placeholder="省 / 市 / 区县 / 详细地址" />
          </label>
          <label v-if="outboundForm.delivery_method === 'courier'">
            <span>快递公司 *</span>
            <select v-model="outboundForm.carrier_code">
              <option value="">请选择物流公司</option>
              <option v-for="provider in logisticsProviders" :key="provider.id" :value="provider.code">
                {{ provider.label }}
              </option>
            </select>
          </label>
          <label v-if="outboundForm.delivery_method === 'courier'">
            <span>快递单号 *</span>
            <input v-model="outboundForm.tracking_no" type="text" placeholder="真实运单号" />
          </label>
          <label v-if="outboundForm.delivery_method === 'courier'">
            <span>物流费用（元） *</span>
            <input v-model="outboundForm.logistics_fee_yuan" type="number" min="0" step="0.01" placeholder="无费用填 0" />
          </label>
          <div v-else class="feature-editor-wide inventory-v2-pickup-tip">
            直接领取不需要快递单号。确认提交后，设备会直接记录为已交付给领取人。
          </div>
          <label class="feature-editor-wide"><span>出库备注</span><textarea v-model="outboundForm.note" rows="3" /></label>
        </div>

        <div v-if="modal === 'create-device'" class="feature-editor-grid">
          <label><span>设备 SN *</span><input v-model="createForm.sn" type="text" /></label>
          <label><span>SKU *</span><input v-model="createForm.sku_code" type="text" /></label>
          <label><span>批次号</span><input v-model="createForm.batch_no" type="text" /></label>
          <label><span>所有权组织 ID</span><input v-model="createForm.owner_org_id" type="number" /></label>
          <label>
            <span>入库仓库 *</span>
            <select v-model="createForm.warehouse_id">
              <option value="">请选择</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">{{ warehouse.name }}</option>
            </select>
          </label>
          <label>
            <span>质检结果</span>
            <select v-model="createForm.quality_status">
              <option value="qualified">合格</option>
              <option value="defective">不良</option>
            </select>
          </label>
          <label class="feature-editor-wide"><span>入库原因</span><textarea v-model="createForm.reason" rows="3" /></label>
        </div>

        <div v-else-if="modal === 'transition' && selectedDevice" class="feature-editor-grid">
          <div class="inventory-current-state feature-editor-wide">
            <strong>{{ selectedDevice.sn }}</strong>
            <span>{{ deviceStatusLabel(selectedDevice.lifecycle_status) }} · {{ selectedDevice.custody_warehouse || '无仓库' }}</span>
          </div>
          <label>
            <span>目标状态 *</span>
            <select v-model="transitionForm.to_status">
              <option value="">请选择</option>
              <option v-for="status in allowedTransitions" :key="status" :value="status">{{ deviceStatusLabel(status) }}</option>
            </select>
          </label>
          <label>
            <span>目标仓库</span>
            <select v-model="transitionForm.to_warehouse_id">
              <option value="">保持/无仓库</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">{{ warehouse.name }}</option>
            </select>
          </label>
          <label><span>目标所有权组织 ID</span><input v-model="transitionForm.to_owner_org_id" type="number" /></label>
          <label><span>目标终端 ID</span><input v-model="transitionForm.to_customer_id" type="number" /></label>
          <label><span>关联业务单号</span><input v-model="transitionForm.reference_no" type="text" /></label>
          <label class="feature-editor-wide"><span>变更原因</span><textarea v-model="transitionForm.reason" rows="3" /></label>
        </div>

        <div v-else-if="modal === 'ledger' && selectedDevice" class="inventory-ledger-modal">
          <div class="inventory-current-state">
            <strong>{{ selectedDevice.sn }}</strong>
            <span>{{ selectedDevice.sku_code }} · 当前 {{ deviceStatusLabel(selectedDevice.lifecycle_status) }}</span>
          </div>
          <div class="data-table-wrap">
            <table class="data-table">
              <thead><tr><th>时间</th><th>单号</th><th>动作</th><th>状态变化</th><th>仓库变化</th><th>经办人</th><th>原因</th></tr></thead>
              <tbody>
                <tr v-for="item in selectedDeviceLedger" :key="item.id">
                  <td>{{ new Date(item.created_at).toLocaleString('zh-CN') }}</td>
                  <td>{{ item.document_no }}</td>
                  <td>{{ actionLabel(item.action) }}</td>
                  <td>{{ item.from_status ? deviceStatusLabel(item.from_status) : '—' }} → {{ deviceStatusLabel(item.to_status) }}</td>
                  <td>{{ warehouseName(item.from_warehouse_id) }} → {{ warehouseName(item.to_warehouse_id) }}</td>
                  <td>{{ item.operator_user_id ? '#' + item.operator_user_id : '系统' }}</td>
                  <td>{{ item.reason || '—' }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-else-if="modal === 'create-rma'" class="feature-editor-grid">
          <label>
            <span>设备 *</span>
            <select v-model="rmaForm.device_id">
              <option value="">请选择</option>
              <option
                v-for="item in devices.filter((device) => ['ACTIVE','CUSTOMER_BOUND','SOLD','AGENT_STOCK','IN_STOCK'].includes(device.lifecycle_status))"
                :key="item.id"
                :value="String(item.id)"
              >
                {{ item.sn }} · {{ deviceStatusLabel(item.lifecycle_status) }}
              </option>
            </select>
          </label>
          <label>
            <span>售后类型 *</span>
            <select v-model="rmaForm.service_type">
              <option value="return">退货</option>
              <option value="exchange">换货</option>
              <option value="repair">维修</option>
              <option value="refurbish">翻新</option>
              <option value="scrap">报废</option>
            </select>
          </label>
          <label><span>终端 / 代理</span><input v-model="rmaForm.customer_name" type="text" placeholder="联系人或单位名称" /></label>
          <label><span>联系电话</span><input v-model="rmaForm.contact_phone" type="text" placeholder="便于售后联系" /></label>
          <label class="feature-editor-wide"><span>问题说明 *</span><textarea v-model="rmaForm.issue" rows="4" /></label>
        </div>

        <div v-else-if="modal === 'rma-detail' && selectedRMA" class="rma-detail-panel">
          <div class="rma-detail-hero">
            <div class="rma-detail-hero-main">
              <span class="rma-detail-hero-icon">售</span>
              <div>
                <span>AFTER-SALES ORDER</span>
                <strong>{{ selectedRMA.rma_no }}</strong>
                <small>{{ serviceTypeLabel(selectedRMA.service_type) }} · {{ selectedRMA.customer_name || '内部售后' }}</small>
              </div>
            </div>
            <span class="status-pill" :class="rmaStatusTone(selectedRMA.status)">{{ rmaStatusLabel(selectedRMA.status) }}</span>
          </div>

          <section class="rma-info-section">
            <div class="rma-info-section-head">
              <span>OVERVIEW</span>
              <strong>基础信息</strong>
            </div>
            <div class="rma-overview-grid">
              <div><span>售后类型</span><strong>{{ serviceTypeLabel(selectedRMA.service_type) }}</strong></div>
              <div><span>发起来源</span><strong>{{ selectedRMA.source_type === 'customer' ? '终端发起' : selectedRMA.source_type === 'agent' ? '代理发起' : '内部录入' }}</strong></div>
              <div><span>终端 / 代理</span><strong>{{ selectedRMA.customer_name || '—' }}</strong></div>
              <div><span>联系电话</span><strong>{{ selectedRMA.contact_phone || '—' }}</strong></div>
            </div>
          </section>

          <section class="rma-info-section">
            <div class="rma-info-section-head">
              <span>DEVICE RELATION</span>
              <strong>设备与订单关联</strong>
            </div>
            <div class="rma-link-grid">
              <div>
                <span>原设备 SN</span>
                <button class="rma-sn-link" type="button" @click="openRMADeviceLedger(selectedRMA.device_id)">
                  {{ selectedRMA.device_sn }}
                </button>
              </div>
              <div>
                <span>换机新设备</span>
                <button
                  v-if="replacementDevice(selectedRMA)"
                  class="rma-sn-link"
                  type="button"
                  @click="openRMADeviceLedger(selectedRMA.replacement_device_id)"
                >
                  {{ replacementDevice(selectedRMA)?.sn }}
                </button>
                <strong v-else>—</strong>
              </div>
              <div><span>原订单</span><strong>{{ selectedRMA.source_order_no || '—' }}</strong></div>
            </div>
          </section>

          <section class="rma-info-section">
            <div class="rma-info-section-head">
              <span>LOGISTICS FLOW</span>
              <strong>物流链路</strong>
            </div>
            <div class="rma-logistics-grid">
              <div><span>原物流</span><strong>{{ selectedRMA.source_shipment_no || '—' }}</strong></div>
              <div><span>退回物流</span><strong>{{ selectedRMA.return_shipment_no || '—' }}</strong></div>
              <div><span>外送维修</span><strong>{{ selectedRMA.repair_outbound_shipment_no || '—' }}</strong></div>
              <div><span>维修返回</span><strong>{{ selectedRMA.repair_return_shipment_no || '—' }}</strong></div>
              <div><span>寄出物流</span><strong>{{ selectedRMA.outbound_shipment_no || '—' }}</strong></div>
            </div>
          </section>

          <section class="rma-info-section">
            <div class="rma-info-section-head">
              <span>TIME LINE</span>
              <strong>时间节点</strong>
            </div>
            <div class="rma-time-grid">
              <div><span>创建时间</span><strong>{{ new Date(selectedRMA.created_at).toLocaleString('zh-CN') }}</strong></div>
              <div><span>受理时间</span><strong>{{ selectedRMA.accepted_at ? new Date(selectedRMA.accepted_at).toLocaleString('zh-CN') : '待受理' }}</strong></div>
              <div><span>完成时间</span><strong>{{ selectedRMA.completed_at ? new Date(selectedRMA.completed_at).toLocaleString('zh-CN') : '—' }}</strong></div>
            </div>
          </section>

          <div class="rma-detail-copy-grid">
            <div class="rma-detail-copy">
              <span>问题说明</span>
              <p>{{ selectedRMA.issue || '—' }}</p>
            </div>
            <div class="rma-detail-copy">
              <span>处理结果</span>
              <p>{{ selectedRMA.resolution || '尚未完成处理' }}</p>
            </div>
          </div>

          <section class="rma-detail-section">
            <div class="rma-detail-section-head">
              <div><strong>维修进度</strong><span>终端可见的状态时间线</span></div>
            </div>
            <div v-if="rmaDetailLoading" class="panel-loading">正在读取维修进度...</div>
            <div v-else-if="selectedRMAEvents.length" class="rma-timeline">
              <article v-for="event in selectedRMAEvents" :key="event.id" class="rma-timeline-item">
                <span class="rma-timeline-dot"></span>
                <div>
                  <strong>{{ event.title }}</strong>
                  <p>{{ event.description || rmaStatusLabel(event.status) }}</p>
                  <small>{{ new Date(event.occurred_at).toLocaleString('zh-CN') }}{{ event.customer_visible ? ' · 终端可见' : ' · 内部记录' }}</small>
                </div>
              </article>
            </div>
            <div v-else class="empty-state">暂无维修进度记录。</div>
          </section>

          <section class="rma-detail-section">
            <div class="rma-detail-section-head">
              <div><strong>维修费用</strong><span>配件、人工、检测、上游维修等；物流费在物流单中单独记账</span></div>
            </div>
            <div v-if="selectedRMACosts.length" class="rma-cost-list">
              <div v-for="cost in selectedRMACosts" :key="cost.id" class="rma-cost-row">
                <span>{{ cost.cost_type === 'parts' ? '配件' : cost.cost_type === 'labor' ? '人工' : cost.cost_type === 'external_repair' ? '上游维修' : cost.cost_type === 'inspection' ? '检测' : '其他' }}</span>
                <strong>¥{{ (cost.amount_cents / 100).toFixed(2) }}</strong>
                <small>{{ cost.counterparty_name || '—' }} · {{ cost.note || cost.cost_no }}</small>
              </div>
            </div>
            <div v-else class="empty-state">暂无维修费用。</div>

            <div v-if="canManageAfterSales && !['COMPLETED','CANCELLED'].includes(selectedRMA.status)" class="rma-cost-form">
              <select v-model="rmaCostForm.cost_type">
                <option value="parts">配件费用</option>
                <option value="labor">人工费用</option>
                <option value="inspection">检测费用</option>
                <option value="external_repair">上游维修费用</option>
                <option value="other">其他费用</option>
              </select>
              <input v-model="rmaCostForm.amount_yuan" type="number" min="0.01" step="0.01" placeholder="金额（元）" />
              <input v-model="rmaCostForm.counterparty_name" type="text" placeholder="收款方 / 供应方" />
              <select v-model="rmaCostForm.payment_method">
                <option value="bank_transfer">银行转账</option>
                <option value="wechat">微信</option>
                <option value="alipay">支付宝</option>
                <option value="cash">现金</option>
                <option value="other">其他</option>
              </select>
              <input v-model="rmaCostForm.note" type="text" placeholder="费用说明" />
              <button class="primary-button" type="button" :disabled="saving" @click="submitRMACost">
                登记费用
              </button>
            </div>
          </section>

          <div v-if="selectedRMA.service_type === 'exchange' && replacementDevice(selectedRMA)" class="rma-exchange-link">
            <strong>换机设备关联</strong>
            <span>{{ selectedRMA.device_sn }} → {{ replacementDevice(selectedRMA)?.sn }}</span>
            <small>原机与新机均可点击进入 SN 全链路。</small>
          </div>
        </div>

        <div v-else-if="modal === 'scrap-dispose' && selectedRMA" class="feature-editor-grid">
          <div class="inventory-current-state feature-editor-wide">
            <strong>{{ selectedRMA.rma_no }} · {{ selectedRMA.device_sn }}</strong>
            <span>设备已进入报废待处置库。出售回收品填写实际收入；纯销毁金额填 0。</span>
          </div>
          <label>
            <span>处置收入（元） *</span>
            <input v-model="scrapDisposalForm.amount_yuan" type="number" min="0" step="0.01" placeholder="纯销毁填 0" />
          </label>
          <label>
            <span>回收方 / 购买方</span>
            <input v-model="scrapDisposalForm.buyer_name" type="text" placeholder="有收入时必填" />
          </label>
          <label v-if="Number(scrapDisposalForm.amount_yuan || 0) > 0">
            <span>收款方式</span>
            <select v-model="scrapDisposalForm.payment_method">
              <option value="bank_transfer">银行转账</option>
              <option value="wechat">微信</option>
              <option value="alipay">支付宝</option>
              <option value="cash">现金</option>
              <option value="other">其他</option>
            </select>
          </label>
          <label class="feature-editor-wide">
            <span>处置说明</span>
            <textarea v-model="scrapDisposalForm.note" rows="4" placeholder="回收出售、实物销毁方式、交接说明等" />
          </label>
        </div>

        <div v-else-if="modal === 'complete-rma' && selectedRMA" class="feature-editor-grid">
          <div class="inventory-current-state feature-editor-wide">
            <strong>{{ selectedRMA.rma_no }} · {{ selectedRMA.device_sn }}</strong>
            <span>{{ serviceTypeLabel(selectedRMA.service_type) }} · {{ selectedRMA.issue }}</span>
          </div>
          <label>
            <span>处理后设备状态 *</span>
            <select v-model="completeRMAForm.to_status">
              <option value="ACTIVE">恢复正常使用</option>
              <option value="IN_STOCK">返仓可用</option>
              <option value="AFTER_SALES">继续售后处理</option>
              <option value="SCRAP_PENDING">报废待处置</option>
              <option value="REPLACED">已换机</option>
            </select>
          </label>
          <label>
            <span>处理后仓库</span>
            <select v-model="completeRMAForm.to_warehouse_id">
              <option value="">保持当前仓库</option>
              <option v-for="warehouse in warehouses" :key="warehouse.id" :value="String(warehouse.id)">{{ warehouse.name }}</option>
            </select>
          </label>
          <label v-if="selectedRMA.service_type === 'exchange'">
            <span>换机新设备 *</span>
            <select v-model="completeRMAForm.replacement_device_id">
              <option value="">请选择在库设备</option>
              <option
                v-for="device in devices.filter((row) => row.lifecycle_status === 'IN_STOCK' && row.id !== selectedRMA?.device_id)"
                :key="device.id"
                :value="String(device.id)"
              >
                {{ device.sn }} · {{ device.sku_code }} · {{ device.custody_warehouse || '无仓库' }}
              </option>
            </select>
          </label>
          <label class="feature-editor-wide"><span>处理结果 *</span><textarea v-model="completeRMAForm.resolution" rows="4" /></label>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>

        <footer v-if="!['ledger', 'rma-detail'].includes(modal)">
          <button class="ghost-button" type="button" @click="modal = ''">取消</button>
          <button
            class="primary-button"
            type="button"
            :disabled="saving"
            @click="
              modal === 'batch-inbound' ? submitBatchInbound() :
              modal === 'outbound' ? submitOutbound() :
              modal === 'create-device' ? submitCreateDevice() :
              modal === 'transition' ? submitTransition() :
              modal === 'create-rma' ? submitCreateRMA() :
              modal === 'scrap-dispose' ? submitScrapDisposal() :
              submitCompleteRMA()
            "
          >
            {{ saving ? '处理中...' : '确认提交' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>
