<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import {
  cancelCustomerShopOrder,
  createCustomerShopOrder,
  getAccountDashboard,
  getCustomerDeviceOffers,
  getCustomerMarketingCampaigns,
  getCustomerMembershipOffers,
  getCustomerShopOrders,
  getCustomerTimeCardOffers,
  sandboxPayCustomerShopOrder,
  sandboxRefundCustomerShopOrder,
} from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import RegionSelect from '../components/RegionSelect.vue'
import {
  floorToWholeYuanCents,
  formatWholeYuanMoney,
  marketingPayableCents,
} from '../pricingRules'
import type {
  AccountProfile,
  CustomerDeviceOffer,
  CustomerMembershipOffer,
  CustomerShopOrder,
  CustomerTimeCardOffer,
  MarketingCampaign,
  MarketingCampaignItem,
  SandboxPaymentRecord,
} from '../types'

const loading = ref(false)
const ordersLoading = ref(false)
const creatingOrder = ref<number | null>(null)
const paying = ref(false)
const refunding = ref(false)
const error = useFeedbackErrorRef()
const paymentError = ref('')
const refundError = ref('')
const successMessage = ref('')
const products = ref<CustomerTimeCardOffer[]>([])
const deviceProducts = ref<CustomerDeviceOffer[]>([])
const membershipOffers = ref<CustomerMembershipOffer[]>([])
const marketingPlans = ref<MarketingCampaign[]>([])
const accountProfile = ref<AccountProfile | null>(null)
const orders = ref<CustomerShopOrder[]>([])
const selectedDeviceProduct = ref<CustomerDeviceOffer | null>(null)
const selectedDeviceMarketing = ref<{
  campaign: MarketingCampaign
  item: MarketingCampaignItem
} | null>(null)
const deviceQuantity = ref(1)
const timePage = ref(1)
const timePageSize = 8
const devicePage = ref(1)
const devicePageSize = 8
const orderPage = ref(1)
const orderPageSize = 12

const shippingForm = reactive({
  recipient_name: '',
  recipient_phone: '',
  province: '',
  city: '',
  district: '',
  address: '',
})

const modal = ref<'shipping' | 'payment' | 'refund' | 'order' | ''>('')
const selectedOrder = ref<CustomerShopOrder | null>(null)
const paymentAmount = ref('')
const refundAmount = ref('')
const refundReason = ref('')
const simulateResult = ref<'success' | 'failure'>('success')

const timePageCount = computed(() => Math.max(1, Math.ceil(products.value.length / timePageSize)))
const devicePageCount = computed(() => Math.max(1, Math.ceil(deviceProducts.value.length / devicePageSize)))
const orderPageCount = computed(() => Math.max(1, Math.ceil(orders.value.length / orderPageSize)))
const pagedProducts = computed(() => {
  const current = Math.min(timePage.value, timePageCount.value)
  return products.value.slice((current - 1) * timePageSize, current * timePageSize)
})
const pagedDeviceProducts = computed(() => {
  const current = Math.min(devicePage.value, devicePageCount.value)
  return deviceProducts.value.slice((current - 1) * devicePageSize, current * devicePageSize)
})
const pagedOrders = computed(() => {
  const current = Math.min(orderPage.value, orderPageCount.value)
  return orders.value.slice((current - 1) * orderPageSize, current * orderPageSize)
})

function formatMoney(cents: number) {
  return '¥' + (cents / 100).toFixed(2)
}

function normalizeMarketingDiscountBps(value: number) {
  if (value >= 100 && value < 1000) return value * 10
  if (value > 10000) return 10000
  return Math.max(0, Number(value || 0))
}

function marketingDiscountLabel(value: number) {
  const bps = normalizeMarketingDiscountBps(value)
  if (bps <= 0) return '赠送'
  if (bps >= 10000) return '原价'
  const zhe = bps / 1000
  return zhe.toFixed(zhe % 1 === 0 ? 0 : 1) + ' 折'
}

function marketingTargetName(item: MarketingCampaignItem) {
  if (item.target_type === 'membership') {
    return membershipOffers.value.find((offer) => offer.id === item.target_id)?.name || '会员方案 #' + item.target_id
  }
  if (item.target_type === 'time_card') {
    return products.value.find((offer) => offer.id === item.target_id)?.name || '时长卡 #' + item.target_id
  }
  return deviceProducts.value.find((offer) => offer.id === item.target_id)?.name || '设备商品 #' + item.target_id
}

function marketingTargetTypeLabel(item: MarketingCampaignItem) {
  if (item.target_type === 'membership') return '会员'
  if (item.target_type === 'time_card') return '时长卡'
  return '设备'
}

function marketingPackageLabel(item: MarketingCampaignItem) {
  const quantity = Math.max(1, item.quantity || 1)
  if (item.target_type === 'membership' && item.pricing_mode === 'package') {
    const months = Math.max(1, item.package_months || 1)
    const label =
      months === 1 ? '包月' :
      months === 3 ? '包季' :
      months === 6 ? '包半年' :
      months === 12 ? '包年' : months + '个月'
    return quantity > 1 ? label + ' × ' + quantity : label
  }
  return quantity > 1 ? '数量 × ' + quantity : '1 份'
}

function marketingItemBaseCents(item: MarketingCampaignItem) {
  const quantity = Math.max(1, item.quantity || 1)
  if (item.target_type === 'membership') {
    const offer = membershipOffers.value.find((row) => row.id === item.target_id)
    return floorToWholeYuanCents(
      (offer?.monthly_price_cents || 0) * Math.max(1, item.package_months || 1) * quantity,
    )
  }
  if (item.target_type === 'time_card') {
    const offer = products.value.find((row) => row.id === item.target_id)
    return floorToWholeYuanCents((offer?.original_price_cents || 0) * quantity)
  }
  const offer = deviceProducts.value.find((row) => row.id === item.target_id)
  return floorToWholeYuanCents(
    (offer?.base_sale_price_cents || offer?.original_price_cents || 0) * quantity,
  )
}

function marketingItemPayableCents(item: MarketingCampaignItem) {
  return marketingPayableCents(
    marketingItemBaseCents(item),
    normalizeMarketingDiscountBps(item.discount_bps),
  )
}

function formatMarketingMoney(cents: number) {
  return formatWholeYuanMoney(cents)
}

function marketingPlanBaseCents(plan: MarketingCampaign) {
  return (plan.items ?? []).reduce((sum, item) => sum + marketingItemBaseCents(item), 0)
}

function marketingPlanPayableCents(plan: MarketingCampaign) {
  return (plan.items ?? []).reduce((sum, item) => sum + marketingItemPayableCents(item), 0)
}

function marketingPlanSavingCents(plan: MarketingCampaign) {
  return Math.max(0, marketingPlanBaseCents(plan) - marketingPlanPayableCents(plan))
}

function marketingItemDetail(item: MarketingCampaignItem) {
  const quantity = Math.max(1, item.quantity || 1)
  if (item.target_type === 'membership') {
    const offer = membershipOffers.value.find((row) => row.id === item.target_id)
    const hours = (offer?.included_seconds || 0) / 3600 *
      Math.max(1, item.package_months || 1) * quantity
    return hours > 0 ? hours.toLocaleString('zh-CN') + ' 小时权益' : '会员权益'
  }
  if (item.target_type === 'time_card') {
    const offer = products.value.find((row) => row.id === item.target_id)
    const totalHours = (offer?.duration_seconds || 0) / 3600 * quantity
    return totalHours.toLocaleString('zh-CN') + ' 小时'
  }
  const offer = deviceProducts.value.find((row) => row.id === item.target_id)
  return quantity + ' ' + (offer?.unit_label || '台')
}

const visibleMarketingPlans = computed(() =>
  marketingPlans.value.filter(
    (plan) => plan.status === 'active' && (plan.items?.length || 0) > 0,
  ),
)

function marketingPlansForTarget(type: MarketingCampaignItem['target_type'], id: number) {
  return visibleMarketingPlans.value
    .map((plan) => ({
      plan,
      item: (plan.items ?? []).find(
        (entry) => entry.target_type === type && entry.target_id === id,
      ),
    }))
    .filter(
      (entry): entry is { plan: MarketingCampaign; item: MarketingCampaignItem } =>
        Boolean(entry.item),
    )
}

function hours(product: CustomerTimeCardOffer) {
  return product.duration_seconds / 3600
}

function orderHours(order: CustomerShopOrder) {
  return order.items.reduce(
    (sum, item) => sum + (item.duration_seconds * item.quantity) / 3600,
    0,
  )
}

function savings(product: CustomerTimeCardOffer) {
  return Math.max(product.original_price_cents - product.sale_price_cents, 0)
}

function discountLabel(product: CustomerTimeCardOffer) {
  if (product.discount_bps >= 10000) return '原价'
  return (product.discount_bps / 1000).toFixed(1).replace(/\.0$/, '') + ' 折'
}

function badge(product: CustomerTimeCardOffer) {
  const h = hours(product)
  if (h <= 10) return '体验'
  if (h <= 50) return '灵活'
  if (h <= 100) return '常用'
  return '大额'
}

function deviceSavings(product: CustomerDeviceOffer) {
  return Math.max(product.base_sale_price_cents - product.sale_price_cents, 0)
}

function deviceDiscountLabel(product: CustomerDeviceOffer) {
  if (product.membership_discount_bps >= 10000) return '原价'
  return (product.membership_discount_bps / 1000).toFixed(1).replace(/.0$/, '') + ' 折'
}

function orderSpec(order: CustomerShopOrder) {
  if (order.order_type === 'device') {
    const count = order.items.reduce((sum, item) => sum + item.quantity, 0)
    return count.toLocaleString('zh-CN') + ' 台'
  }
  if (order.order_type === 'membership') {
    return order.items[0]?.product_name || '会员权益'
  }
  return orderHours(order).toLocaleString('zh-CN') + ' 小时'
}

function fulfillmentLabel(order: CustomerShopOrder) {
  if (order.status === 'cancelled') return '已取消'
  if (order.order_type === 'time_card') {
    return order.fulfillment_status === 'fulfilled' ? '时长已入账' : '等待时长入账'
  }
  const map: Record<string, string> = {
    pending_fulfillment: '等待分配库存',
    reserved: '设备已锁库',
    pending_shipment: '待仓库出库',
    shipping: '配送中',
    fulfilled: '已签收并绑定终端',
    logistics_exception: '物流异常',
    returned: '已退回',
    shipment_cancelled: '物流已取消',
  }
  return map[order.fulfillment_status] || '等待处理'
}

function shipmentStatusLabel(value: string) {
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

function shipmentStatusClass(value: string) {
  if (value === 'delivered') return 'status-success'
  if (value === 'exception') return 'status-error'
  if (value === 'returned' || value === 'cancelled') return 'status-muted'
  if (value === 'shipped' || value === 'in_transit') return 'status-info'
  return 'status-warning'
}

function orderStatusLabel(order: CustomerShopOrder) {
  if (order.status === 'cancelled') return '已取消'
  if (order.status === 'refunded') return '已全额退款'
  if (order.status === 'partially_refunded') return '已部分退款'
  if (order.payment_status === 'paid' && order.fulfillment_status === 'fulfilled') {
    return order.order_type === 'device' ? '已支付 · 已签收' : '已支付 · 已入账'
  }
  if (order.payment_status === 'paid') {
    if (order.order_type === 'device') return '已支付 · ' + fulfillmentLabel(order)
    return '已支付 · 待入账'
  }
  return '待支付'
}

function orderStatusClass(order: CustomerShopOrder) {
  if (order.status === 'cancelled' || order.status === 'refunded') return 'status-muted'
  if (order.status === 'partially_refunded') return 'status-info'
  if (order.payment_status === 'paid' && order.fulfillment_status === 'fulfilled') {
    return 'status-success'
  }
  if (order.payment_status === 'paid') return 'status-info'
  return 'status-warning'
}

function paymentStatusLabel(payment: SandboxPaymentRecord) {
  if (payment.status === 'paid') return '模拟支付成功'
  if (payment.failure_reason === 'amount_mismatch') return '金额不一致'
  if (payment.status === 'failed') return '模拟支付失败'
  return payment.status
}

function paymentStatusClass(payment: SandboxPaymentRecord) {
  if (payment.status === 'paid') return 'status-success'
  if (payment.status === 'failed') return 'status-error'
  return 'status-warning'
}

function newIdempotencyKey(prefix: string) {
  const id =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : Date.now().toString(36) + '-' + Math.random().toString(36).slice(2)
  return prefix + '-' + id
}

function centsToInput(cents: number) {
  return (cents / 100).toFixed(2)
}

function inputToCents(value: string) {
  const normalized = value.trim()
  if (!/^\d+(\.\d{1,2})?$/.test(normalized)) return null
  const [yuan, decimal = ''] = normalized.split('.')
  const cents = Number(yuan) * 100 + Number((decimal + '00').slice(0, 2))
  return Number.isSafeInteger(cents) ? cents : null
}

async function loadProducts() {
  const [timeCardData, deviceData, membershipData, marketingData, accountData] = await Promise.all([
    getCustomerTimeCardOffers(),
    getCustomerDeviceOffers(),
    getCustomerMembershipOffers(),
    getCustomerMarketingCampaigns(),
    getAccountDashboard(),
  ])
  products.value = timeCardData.items
  deviceProducts.value = deviceData.items
  membershipOffers.value = membershipData.items
  marketingPlans.value = marketingData.items
  accountProfile.value = accountData.profile
}

async function loadOrders() {
  ordersLoading.value = true
  try {
    const data = await getCustomerShopOrders()
    orders.value = data.items
  } finally {
    ordersLoading.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    await Promise.all([loadProducts(), loadOrders()])
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取终端商城数据失败'
  } finally {
    loading.value = false
  }
}

function openDevicePurchase(
  product: CustomerDeviceOffer,
  campaign?: MarketingCampaign,
  campaignItem?: MarketingCampaignItem,
) {
  selectedDeviceProduct.value = product
  selectedDeviceMarketing.value = campaign && campaignItem
    ? { campaign, item: campaignItem }
    : null
  deviceQuantity.value = selectedDeviceMarketing.value
    ? Math.max(1, selectedDeviceMarketing.value.item.quantity || 1)
    : 1
  const profile = accountProfile.value
  shippingForm.recipient_name = profile?.display_name || ''
  shippingForm.recipient_phone = profile?.phone || ''
  shippingForm.province = profile?.province || ''
  shippingForm.city = profile?.city || ''
  shippingForm.district = profile?.district || ''
  shippingForm.address = profile?.address || ''
  error.value = ''
  successMessage.value = ''
  modal.value = 'shipping'
}

async function submitDeviceOrder() {
  const product = selectedDeviceProduct.value
  if (!product) return
  const quantity = Math.max(1, Math.floor(Number(deviceQuantity.value || 1)))
  if (quantity > product.available_stock) {
    error.value = '购买数量超过当前可售库存。'
    return
  }
  if (
    !shippingForm.recipient_name.trim() ||
    !shippingForm.recipient_phone.trim() ||
    !shippingForm.province.trim() ||
    !shippingForm.city.trim() ||
    !shippingForm.district.trim() ||
    !shippingForm.address.trim()
  ) {
    error.value = '请填写完整的收货人、联系电话、省、市、区/县和详细地址。'
    return
  }

  creatingOrder.value = product.id
  error.value = ''
  try {
    const order = await createCustomerShopOrder({
      product_type: 'device',
      product_id: product.id,
      quantity,
      marketing_campaign_id: selectedDeviceMarketing.value?.campaign.id || undefined,
      marketing_placement: 'shop',
      idempotency_key: newIdempotencyKey('device-order'),
      recipient_name: shippingForm.recipient_name.trim(),
      recipient_phone: shippingForm.recipient_phone.trim(),
      province: shippingForm.province.trim(),
      city: shippingForm.city.trim(),
      district: shippingForm.district.trim(),
      address: shippingForm.address.trim(),
    })
    selectedOrder.value = order
    paymentAmount.value = centsToInput(order.payable_amount_cents)
    simulateResult.value = 'success'
    paymentError.value = ''
    modal.value = 'payment'
    await loadOrders()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '创建设备订单失败'
  } finally {
    creatingOrder.value = null
  }
}

async function buy(
  product: CustomerTimeCardOffer,
  marketingCampaignId = 0,
  marketingQuantity = 1,
) {
  creatingOrder.value = product.id
  error.value = ''
  successMessage.value = ''
  try {
    const order = await createCustomerShopOrder({
      product_type: 'time_card',
      product_id: product.id,
      quantity: Math.max(1, marketingQuantity),
      marketing_campaign_id: marketingCampaignId || undefined,
      marketing_placement: 'shop',
      idempotency_key: newIdempotencyKey(marketingCampaignId ? 'marketing-time-card' : 'shop-order'),
    })
    selectedOrder.value = order
    paymentAmount.value = centsToInput(order.payable_amount_cents)
    simulateResult.value = 'success'
    paymentError.value = ''
    modal.value = 'payment'
    await loadOrders()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '创建订单失败'
  } finally {
    creatingOrder.value = null
  }
}

function openPayment(order: CustomerShopOrder) {
  selectedOrder.value = order
  paymentAmount.value = centsToInput(order.payable_amount_cents)
  simulateResult.value = 'success'
  paymentError.value = ''
  successMessage.value = ''
  modal.value = 'payment'
}

function openOrder(order: CustomerShopOrder) {
  selectedOrder.value = order
  paymentError.value = ''
  modal.value = 'order'
}

function openRefund(order: CustomerShopOrder) {
  if (order.refundable_amount_cents <= 0) return
  selectedOrder.value = order
  refundAmount.value = centsToInput(order.refundable_amount_cents)
  refundReason.value = 'Sandbox 测试退款'
  refundError.value = ''
  successMessage.value = ''
  modal.value = 'refund'
}

async function submitPayment(result: 'success' | 'failure' = simulateResult.value) {
  if (!selectedOrder.value) return
  const cents = inputToCents(paymentAmount.value)
  if (cents === null) {
    paymentError.value = '请输入正确的支付金额，最多保留两位小数。'
    return
  }

  paying.value = true
  paymentError.value = ''
  successMessage.value = ''
  try {
    const response = await sandboxPayCustomerShopOrder(selectedOrder.value.id, {
      amount_cents: cents,
      simulate_result: result,
      idempotency_key: newIdempotencyKey('sandbox-pay'),
    })
    selectedOrder.value = response.order
    await loadOrders()

    if (result === 'success') {
      if (response.order.order_type === 'device') {
        successMessage.value =
          '模拟支付成功，系统已自动锁定设备 SN，并生成待出库物流单。'
      } else {
        const creditedHours = orderHours(response.order)
        successMessage.value =
          '模拟支付成功，' +
          creditedHours.toLocaleString('zh-CN') +
          ' 小时时长已入账。'
      }
      modal.value = 'order'
    }
  } catch (value) {
    paymentError.value =
      value instanceof Error ? value.message : '模拟支付失败'
    await loadOrders()
    const refreshed = orders.value.find(
      (item) => item.id === selectedOrder.value?.id,
    )
    if (refreshed) selectedOrder.value = refreshed
  } finally {
    paying.value = false
  }
}

async function submitRefund() {
  if (!selectedOrder.value) return
  const cents = inputToCents(refundAmount.value)
  if (cents === null || cents <= 0) {
    refundError.value = '请输入正确的退款金额，最多保留两位小数。'
    return
  }
  if (cents > selectedOrder.value.refundable_amount_cents) {
    refundError.value =
      '当前最多可退 ' + formatMoney(selectedOrder.value.refundable_amount_cents)
    return
  }

  refunding.value = true
  refundError.value = ''
  successMessage.value = ''
  try {
    const response = await sandboxRefundCustomerShopOrder(
      selectedOrder.value.id,
      {
        amount_cents: cents,
        reason: refundReason.value.trim() || 'Sandbox 测试退款',
        idempotency_key: newIdempotencyKey('sandbox-refund'),
      },
    )
    selectedOrder.value = response.order
    successMessage.value =
      '模拟退款成功：' +
      formatMoney(response.refund.refund_amount_cents) +
      '，对应未使用时长已同步冲减。'
    modal.value = 'order'
    await loadOrders()
  } catch (value) {
    refundError.value =
      value instanceof Error ? value.message : '模拟退款失败'
    await loadOrders()
    const refreshed = orders.value.find(
      (item) => item.id === selectedOrder.value?.id,
    )
    if (refreshed) selectedOrder.value = refreshed
  } finally {
    refunding.value = false
  }
}

async function cancelOrder(order: CustomerShopOrder) {
  if (order.payment_status === 'paid' || order.status === 'cancelled') return
  error.value = ''
  try {
    const updated = await cancelCustomerShopOrder(order.id)
    selectedOrder.value =
      selectedOrder.value?.id === updated.id ? updated : selectedOrder.value
    await loadOrders()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '取消订单失败'
  }
}

function handleMembershipUpdated() {
  void loadProducts()
}

onMounted(() => {
  void load()
  window.addEventListener('membership-updated', handleMembershipUpdated)
})

onBeforeUnmount(() => {
  window.removeEventListener('membership-updated', handleMembershipUpdated)
})
</script>

<template>
  <div class="management-page customer-shop-page">
    <ModulePageNav context="workspace-customer" active-title="终端商城" />

    <section class="customer-shop-hero customer-shop-hero-compact">
      <div class="customer-shop-hero-copy">
        <p class="section-kicker">CUSTOMER STORE</p>
        <h2>终端商城</h2>
      </div>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>
    <p v-if="successMessage" class="inline-success shop-success-message">
      {{ successMessage }}
    </p>
    <div v-if="loading" class="panel-loading">正在读取终端商城商品...</div>

    <section v-if="!loading && visibleMarketingPlans.length" class="shop-marketing-section">
      <header class="shop-category-head">
        <div>
          <span>MARKETING PLANS</span>
          <strong>当前营销活动</strong>

        </div>
      </header>

      <div class="shop-marketing-grid">
        <article v-for="plan in visibleMarketingPlans" :key="'marketing-' + plan.id" class="shop-marketing-card">
          <header>
            <div>
              <span>{{ plan.items.length }} 项营销内容</span>
              <h3>{{ plan.name }}</h3>
              <p v-if="plan.description">{{ plan.description }}</p>
            </div>
            <strong class="shop-marketing-total">{{ formatMarketingMoney(marketingPlanPayableCents(plan)) }}</strong>
          </header>

          <div class="shop-marketing-value-row">
            <div>
              <span>商品总价值</span>
              <strong>{{ formatMarketingMoney(marketingPlanBaseCents(plan)) }}</strong>
            </div>
            <div>
              <span>活动实际价值</span>
              <strong>{{ formatMarketingMoney(marketingPlanPayableCents(plan)) }}</strong>
            </div>
            <div>
              <span>优惠 / 赠送价值</span>
              <strong>{{ formatMarketingMoney(marketingPlanSavingCents(plan)) }}</strong>
            </div>
          </div>

          <div class="shop-marketing-items">
            <div v-for="(item, index) in plan.items" :key="plan.id + '-' + index" class="shop-marketing-item">
              <div class="shop-marketing-item-copy">
                <span>{{ marketingTargetTypeLabel(item) }} · {{ marketingPackageLabel(item) }}</span>
                <strong>{{ marketingTargetName(item) }}</strong>
                <small>{{ marketingItemDetail(item) }}</small>
              </div>
              <div class="shop-marketing-item-discount" :class="{ gift: item.discount_bps === 0 }">
                {{ marketingDiscountLabel(item.discount_bps) }}
              </div>
              <div class="shop-marketing-item-price">
                <del>{{ formatMarketingMoney(marketingItemBaseCents(item)) }}</del>
                <strong>{{ item.discount_bps === 0 ? '赠送' : formatMarketingMoney(marketingItemPayableCents(item)) }}</strong>
              </div>
            </div>
          </div>
        </article>
      </div>
    </section>

    <section v-if="!loading && products.length" class="customer-shop-grid">
      <article
        v-for="product in pagedProducts"
        :key="product.id"
        class="time-card-product"
      >
        <header class="time-card-product-head">
          <div>
            <span class="time-card-product-badge">{{ badge(product) }}</span>
            <h3>{{ product.name }}</h3>
          </div>
          <div class="time-card-hours">
            <strong>{{ hours(product).toLocaleString('zh-CN') }}</strong>
            <span>小时</span>
          </div>
        </header>

        <p class="time-card-description">
          {{ product.description || '时长卡商品' }}
        </p>

        <div class="time-card-price-block">
          <div class="time-card-sale-price">
            <span>当前价</span>
            <strong>{{ formatMoney(product.sale_price_cents) }}</strong>
          </div>

          <div class="time-card-original-price">
            <span>原价</span>
            <del v-if="product.sale_price_cents < product.original_price_cents">
              {{ formatMoney(product.original_price_cents) }}
            </del>
            <strong v-else>{{ formatMoney(product.original_price_cents) }}</strong>
          </div>
        </div>

        <div class="time-card-meta">
          <div>
            <span>当前折扣</span>
            <strong>{{ discountLabel(product) }}</strong>
          </div>
          <div>
            <span>立省</span>
            <strong>{{ formatMoney(savings(product)) }}</strong>
          </div>
          <div>
            <span>首次使用后有效</span>
            <strong>{{ product.validity_days }} 天</strong>
          </div>
        </div>

        <small class="time-card-activation-note">
          购买后不会立即开始倒计时；首次实际使用时自动激活。
          <template v-if="product.activation_deadline_days > 0">
            需在购买后 {{ product.activation_deadline_days }} 天内激活。
          </template>
          <template v-else>未激活前可长期储备。</template>
        </small>

        <div
          v-if="marketingPlansForTarget('time_card', product.id).length"
          class="shop-product-marketing-links"
        >
          <span>正在参与</span>
          <div>
            <div
              v-for="entry in marketingPlansForTarget('time_card', product.id)"
              :key="entry.plan.id"
              class="shop-product-marketing-option"
            >
              <small>
                {{ entry.plan.name }} · {{ marketingDiscountLabel(entry.item.discount_bps) }}
                · {{ entry.item.quantity || 1 }}份
              </small>
              <button
                type="button"
                class="shop-marketing-buy-link"
                :disabled="creatingOrder === product.id"
                @click="buy(product, entry.plan.id, entry.item.quantity || 1)"
              >
                活动价 {{ formatMarketingMoney(marketingItemPayableCents(entry.item)) }} · 购买
              </button>
            </div>
          </div>
        </div>

        <small class="time-card-version">商品版本 V{{ product.version_no }}</small>

        <button
          class="time-card-buy-button enabled"
          type="button"
          :disabled="creatingOrder === product.id"
          @click="buy(product)"
        >
          {{ creatingOrder === product.id ? '正在创建订单...' : '立即购买' }}
        </button>
      </article>
    </section>
    <PaginationBar
      v-if="!loading && products.length"
      :page="Math.min(timePage, timePageCount)"
      :total-pages="timePageCount"
      :total="products.length"
      :page-size="timePageSize"
      @update:page="timePage = $event"
    />

    <section v-if="!loading && deviceProducts.length" class="shop-device-section">
      <header class="shop-category-head">
        <div>
          <span>PHYSICAL GOODS</span>
          <strong>设备商品</strong>
          <p>实体设备按库存 SN 履约，支付成功后自动锁库并创建模拟物流单。</p>
        </div>
      </header>

      <div class="customer-shop-grid device-shop-grid">
        <article
          v-for="product in pagedDeviceProducts"
          :key="'device-' + product.id"
          class="time-card-product device-product-card"
        >
          <header class="time-card-product-head">
            <div>
              <span class="time-card-product-badge">实体设备</span>
              <h3>{{ product.name }}</h3>
              <small class="device-sku">SKU · {{ product.sku_code }}</small>
            </div>
            <div class="device-stock-badge" :class="{ empty: product.available_stock <= 0 }">
              <strong>{{ product.available_stock }}</strong>
              <span>可售{{ product.unit_label || '台' }}</span>
            </div>
          </header>

          <div class="shop-device-product-image">
            <img v-if="product.image_url" :src="product.image_url" :alt="product.name" />
            <div v-else>
              <span>LIVE DEVICE</span>
              <strong>▣</strong>
              <small>商品图片待配置</small>
            </div>
          </div>

          <p class="time-card-description">
            {{ product.description || '直播伴播实体设备' }}
          </p>

          <div class="time-card-price-block">
            <div class="time-card-sale-price">
              <span>当前会员价</span>
              <strong>{{ formatMoney(product.sale_price_cents) }}</strong>
            </div>
            <div class="time-card-original-price">
              <span>设备正常售价</span>
              <del v-if="product.sale_price_cents < product.base_sale_price_cents">
                {{ formatMoney(product.base_sale_price_cents) }}
              </del>
              <strong v-else>{{ formatMoney(product.base_sale_price_cents) }}</strong>
            </div>
          </div>

          <div class="time-card-meta">
            <div>
              <span>会员设备折扣</span>
              <strong>{{ deviceDiscountLabel(product) }}</strong>
            </div>
            <div>
              <span>会员单台优惠</span>
              <strong>{{ formatMoney(deviceSavings(product)) }}</strong>
            </div>
            <div>
              <span>商品原价</span>
              <strong>{{ formatMoney(product.original_price_cents) }}</strong>
            </div>
          </div>

          <div
            v-if="marketingPlansForTarget('device_product', product.id).length"
            class="shop-product-marketing-links"
          >
            <span>正在参与</span>
            <div>
              <div
                v-for="entry in marketingPlansForTarget('device_product', product.id)"
                :key="entry.plan.id"
                class="shop-product-marketing-option"
              >
                <small>
                  {{ entry.plan.name }} · {{ marketingDiscountLabel(entry.item.discount_bps) }}
                  · {{ entry.item.quantity || 1 }}{{ product.unit_label || '台' }}
                </small>
                <button
                  type="button"
                  class="shop-marketing-buy-link"
                  :disabled="(entry.item.quantity || 1) > product.available_stock"
                  @click="openDevicePurchase(product, entry.plan, entry.item)"
                >
                  活动价 {{ formatMarketingMoney(marketingItemPayableCents(entry.item)) }} · 购买
                </button>
              </div>
            </div>
          </div>

          <small class="time-card-version">商品版本 V{{ product.version_no }}</small>

          <button
            class="time-card-buy-button enabled"
            type="button"
            :disabled="product.available_stock <= 0 || creatingOrder === product.id"
            @click="openDevicePurchase(product)"
          >
            {{
              product.available_stock <= 0
                ? '库存不足'
                : creatingOrder === product.id
                  ? '正在创建订单...'
                  : '立即购买设备'
            }}
          </button>
        </article>
      </div>
      <PaginationBar
        :page="Math.min(devicePage, devicePageCount)"
        :total-pages="devicePageCount"
        :total="deviceProducts.length"
        :page-size="devicePageSize"
        @update:page="devicePage = $event"
      />
    </section>

    <div v-if="!loading && !products.length && !deviceProducts.length" class="empty-state">
      当前还没有已发布商品，请联系管理员在“商品与会员”中发布时长卡或设备商品。
    </div>

    <section class="settings-card shop-orders-panel">
      <header class="inventory-section-head">
        <div>
          <strong>我的终端商城订单</strong>
          <span>订单金额为下单时快照，后台后续改价不会影响已经生成的订单。</span>
        </div>
        <button class="ghost-button" type="button" :disabled="ordersLoading" @click="loadOrders">
          {{ ordersLoading ? '刷新中...' : '刷新订单' }}
        </button>
      </header>

      <div v-if="ordersLoading && !orders.length" class="panel-loading">正在读取订单...</div>
      <div v-else-if="pagedOrders.length" class="data-table-wrap">
        <table class="data-table shop-orders-table">
          <thead>
            <tr>
              <th>订单号</th>
              <th>商品</th>
              <th>规格</th>
              <th>原价</th>
              <th>优惠</th>
              <th>应付</th>
              <th>状态</th>
              <th>下单时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="order in pagedOrders" :key="order.id">
              <td><strong>{{ order.order_no }}</strong></td>
              <td>{{ order.items[0]?.product_name || '终端商城商品' }}</td>
              <td>{{ orderSpec(order) }}</td>
              <td>{{ formatMoney(order.list_amount_cents) }}</td>
              <td>{{ formatMoney(order.discount_amount_cents) }}</td>
              <td><strong>{{ formatMoney(order.payable_amount_cents) }}</strong></td>
              <td>
                <span class="status-pill" :class="orderStatusClass(order)">
                  {{ orderStatusLabel(order) }}
                </span>
              </td>
              <td>{{ new Date(order.created_at).toLocaleString('zh-CN') }}</td>
              <td>
                <div class="table-actions">
                  <button class="text-action" type="button" @click="openOrder(order)">
                    详情
                  </button>
                  <button
                    v-if="order.payment_status !== 'paid' && order.status !== 'cancelled'"
                    class="text-action"
                    type="button"
                    @click="openPayment(order)"
                  >
                    支付
                  </button>
                  <button
                    v-if="order.refundable_amount_cents > 0"
                    class="text-action"
                    type="button"
                    @click="openRefund(order)"
                  >
                    退款
                  </button>
                  <button
                    v-if="order.payment_status !== 'paid' && order.status !== 'cancelled'"
                    class="text-action danger"
                    type="button"
                    @click="cancelOrder(order)"
                  >
                    取消
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="empty-state">还没有终端商城订单。</div>
      <PaginationBar
        :page="Math.min(orderPage, orderPageCount)"
        :total-pages="orderPageCount"
        :total="orders.length"
        :page-size="orderPageSize"
        @update:page="orderPage = $event"
      />
    </section>

    <div
      v-if="modal === 'shipping' && selectedDeviceProduct"
      class="feature-editor-backdrop"
      @click.self="modal = ''"
    >
      <section class="feature-editor-panel shop-shipping-panel">
        <header>
          <div>
            <span class="section-kicker">SHIPPING SNAPSHOT</span>
            <h3>确认设备订单与收货信息</h3>
          </div>
          <button class="icon-button" type="button" @click="modal = ''">×</button>
        </header>

        <section class="sandbox-order-summary device-order-preview">
          <div>
            <span>商品</span>
            <strong>{{ selectedDeviceProduct.name }}</strong>
          </div>
          <div>
            <span>SKU</span>
            <strong>{{ selectedDeviceProduct.sku_code }}</strong>
          </div>
          <div>
            <span>{{ selectedDeviceMarketing ? '活动总价' : '单价' }}</span>
            <strong>
              {{
                selectedDeviceMarketing
                  ? formatMarketingMoney(marketingItemPayableCents(selectedDeviceMarketing.item))
                  : formatMoney(selectedDeviceProduct.sale_price_cents)
              }}
            </strong>
          </div>
          <div v-if="selectedDeviceMarketing">
            <span>营销活动</span>
            <strong>{{ selectedDeviceMarketing.campaign.name }}</strong>
          </div>
          <div>
            <span>可售库存</span>
            <strong>{{ selectedDeviceProduct.available_stock }} 台</strong>
          </div>
        </section>

        <div class="shop-shipping-form">
          <label>
            <span>购买数量 *</span>
            <input
              v-model.number="deviceQuantity"
              type="number"
              min="1"
              :max="selectedDeviceProduct.available_stock"
              :disabled="Boolean(selectedDeviceMarketing)"
            />
          </label>
          <label>
            <span>收货人 *</span>
          <small v-if="selectedDeviceMarketing" class="shop-campaign-fixed-quantity">
            活动数量由营销计划固定为 {{ selectedDeviceMarketing.item.quantity || 1 }} {{ selectedDeviceProduct.unit_label || '台' }}。
          </small>
            <input v-model="shippingForm.recipient_name" type="text" />
          </label>
          <label>
            <span>联系电话 *</span>
            <input v-model="shippingForm.recipient_phone" type="text" />
          </label>
          <div class="shop-shipping-region">
            <span>省 / 市 / 区县 *</span>
            <RegionSelect
              v-model:province="shippingForm.province"
              v-model:city="shippingForm.city"
              v-model:district="shippingForm.district"
            />
          </div>
          <label class="shop-shipping-address">
            <span>详细地址 *</span>
            <textarea
              v-model="shippingForm.address"
              rows="2"
              placeholder="街道、门牌号、楼栋等"
            />
          </label>
        </div>

        <div class="sandbox-payment-warning">
          <strong>地址会成为订单快照</strong>
          <p>支付成功后系统会按当前订单快照锁定设备 SN 并生成物流单，之后修改个人资料不会改变这张订单的收货地址。</p>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>
        <footer class="sandbox-payment-actions">
          <button class="ghost-button" type="button" @click="modal = ''">取消</button>
          <button
            class="primary-button"
            type="button"
            :disabled="creatingOrder === selectedDeviceProduct.id"
            @click="submitDeviceOrder"
          >
            {{ creatingOrder === selectedDeviceProduct.id ? '正在创建订单...' : '确认订单并进入支付' }}
          </button>
        </footer>
      </section>
    </div>

    <div v-if="modal && modal !== 'shipping' && selectedOrder" class="feature-editor-backdrop" @click.self="modal = ''">
      <section class="feature-editor-panel shop-payment-panel">
        <header>
          <div>
            <span class="section-kicker">
              {{
                modal === 'payment'
                  ? 'SANDBOX PAYMENT'
                  : modal === 'refund'
                    ? 'SANDBOX REFUND'
                    : 'ORDER DETAIL'
              }}
            </span>
            <h3>
              {{ modal === 'payment' ? '模拟支付' : modal === 'refund' ? '模拟退款' : '订单详情' }}
            </h3>
          </div>
          <button class="icon-button" type="button" @click="modal = ''">×</button>
        </header>

        <section class="sandbox-order-summary">
          <div>
            <span>订单号</span>
            <strong>{{ selectedOrder.order_no }}</strong>
          </div>
          <div>
            <span>商品</span>
            <strong>{{ selectedOrder.items[0]?.product_name || '终端商城商品' }}</strong>
          </div>
          <div>
            <span>{{ selectedOrder.order_type === 'device' ? '购买数量' : '购买时长' }}</span>
            <strong>{{ orderSpec(selectedOrder) }}</strong>
          </div>
          <div>
            <span>应付金额</span>
            <strong class="money">{{ formatMoney(selectedOrder.payable_amount_cents) }}</strong>
          </div>
        </section>

        <template v-if="modal === 'payment'">
          <div class="sandbox-payment-warning">
            <strong>这是模拟支付页面</strong>
            <p>输入的是测试金额，不会发起任何真实扣款。金额必须与应付金额完全一致才能模拟支付成功。</p>
          </div>

          <div class="sandbox-payment-form">
            <label>
              <span>模拟支付金额（元）</span>
              <div class="sandbox-money-input">
                <b>¥</b>
                <input
                  v-model="paymentAmount"
                  type="text"
                  inputmode="decimal"
                  autocomplete="off"
                  placeholder="0.00"
                />
              </div>
            </label>

            <div class="sandbox-quick-amount">
              <button
                type="button"
                @click="paymentAmount = centsToInput(selectedOrder.payable_amount_cents)"
              >
                填入应付金额
              </button>
              <span>应付 {{ formatMoney(selectedOrder.payable_amount_cents) }}</span>
            </div>
          </div>

          <p v-if="paymentError" class="inline-error">{{ paymentError }}</p>

          <footer class="sandbox-payment-actions">
            <button class="ghost-button" type="button" :disabled="paying" @click="submitPayment('failure')">
              模拟失败
            </button>
            <button class="primary-button" type="button" :disabled="paying" @click="submitPayment('success')">
              {{ paying ? '正在模拟支付...' : '确认模拟支付' }}
            </button>
          </footer>
        </template>

        <template v-else-if="modal === 'refund'">
          <div class="sandbox-payment-warning refund-warning">
            <strong>这是模拟退款</strong>
            <p>
              只会退还当前未使用时长对应的可退金额。成功后会同时生成退款单、冲减时长资产和资源流水，不会发生真实渠道退款。
            </p>
          </div>

          <section class="sandbox-refund-capacity">
            <div>
              <span>已支付</span>
              <strong>{{ formatMoney(selectedOrder.paid_amount_cents) }}</strong>
            </div>
            <div>
              <span>已退款</span>
              <strong>{{ formatMoney(selectedOrder.refunded_amount_cents) }}</strong>
            </div>
            <div>
              <span>当前最多可退</span>
              <strong class="money">{{ formatMoney(selectedOrder.refundable_amount_cents) }}</strong>
            </div>
            <div>
              <span>当前可退时长</span>
              <strong>{{ (selectedOrder.refundable_seconds / 3600).toLocaleString('zh-CN') }} 小时</strong>
            </div>
          </section>

          <div class="sandbox-payment-form">
            <label>
              <span>模拟退款金额（元）</span>
              <div class="sandbox-money-input">
                <b>¥</b>
                <input
                  v-model="refundAmount"
                  type="text"
                  inputmode="decimal"
                  autocomplete="off"
                  placeholder="0.00"
                />
              </div>
            </label>

            <div class="sandbox-quick-amount">
              <button
                type="button"
                @click="refundAmount = centsToInput(selectedOrder.refundable_amount_cents)"
              >
                退当前最大金额
              </button>
              <span>上限 {{ formatMoney(selectedOrder.refundable_amount_cents) }}</span>
            </div>

            <label>
              <span>退款原因</span>
              <textarea v-model="refundReason" rows="3" placeholder="请输入退款原因" />
            </label>
          </div>

          <p v-if="refundError" class="inline-error">{{ refundError }}</p>

          <footer class="sandbox-payment-actions">
            <button class="ghost-button" type="button" :disabled="refunding" @click="modal = 'order'">
              返回订单
            </button>
            <button class="primary-button" type="button" :disabled="refunding" @click="submitRefund">
              {{ refunding ? '正在模拟退款...' : '确认模拟退款' }}
            </button>
          </footer>
        </template>

        <template v-else>
          <p v-if="successMessage" class="inline-success">{{ successMessage }}</p>

          <section class="sandbox-order-state">
            <div>
              <span>支付状态</span>
              <strong>{{ orderStatusLabel(selectedOrder) }}</strong>
            </div>
            <div>
              <span>实际支付</span>
              <strong>{{ formatMoney(selectedOrder.paid_amount_cents) }}</strong>
            </div>
            <div>
              <span>已退款</span>
              <strong>{{ formatMoney(selectedOrder.refunded_amount_cents) }}</strong>
            </div>
            <div>
              <span>履约状态</span>
              <strong>{{ fulfillmentLabel(selectedOrder) }}</strong>
            </div>
          </section>

          <section v-if="selectedOrder.order_type === 'device'" class="device-order-detail">
            <header>
              <strong>实体履约</strong>
              <span>{{ selectedOrder.devices.length }} 台设备 · {{ selectedOrder.shipments.length }} 张物流单</span>
            </header>

            <div v-if="selectedOrder.shipping" class="device-order-shipping">
              <div>
                <span>收货人</span>
                <strong>{{ selectedOrder.shipping.recipient_name }} · {{ selectedOrder.shipping.recipient_phone }}</strong>
              </div>
              <div>
                <span>收货地址</span>
                <strong>{{ selectedOrder.shipping.full_address }}</strong>
              </div>
            </div>

            <div v-if="selectedOrder.devices.length" class="device-order-sn-list">
              <span v-for="device in selectedOrder.devices" :key="device.id">
                {{ device.sn }} · {{ device.status }}
              </span>
            </div>

            <div v-if="selectedOrder.shipments.length" class="device-order-shipment-list">
              <article
                v-for="shipment in selectedOrder.shipments"
                :key="shipment.id"
                class="customer-shipment-card"
              >
                <header>
                  <div>
                    <strong>{{ shipment.shipment_no }}</strong>
                    <span>{{ shipment.carrier_name }} · {{ shipment.tracking_no }}</span>
                  </div>
                  <span class="status-pill" :class="shipmentStatusClass(shipment.status)">
                    {{ shipmentStatusLabel(shipment.status) }}
                  </span>
                </header>

                <div class="customer-shipment-meta">
                  <span v-if="shipment.shipped_at">
                    发货：{{ new Date(shipment.shipped_at).toLocaleString('zh-CN') }}
                  </span>
                  <span v-if="shipment.delivered_at">
                    签收：{{ new Date(shipment.delivered_at).toLocaleString('zh-CN') }}
                  </span>
                </div>

                <div v-if="shipment.events.length" class="customer-shipment-timeline">
                  <article
                    v-for="event in shipment.events"
                    :key="event.id"
                  >
                    <i></i>
                    <div>
                      <header>
                        <strong>{{ shipmentStatusLabel(event.status) }}</strong>
                        <time>{{ new Date(event.occurred_at).toLocaleString('zh-CN') }}</time>
                      </header>
                      <p>{{ event.description || '物流状态更新' }}</p>
                      <span v-if="event.location">{{ event.location }}</span>
                    </div>
                  </article>
                </div>
              </article>
            </div>
          </section>

          <section class="sandbox-payment-history">
            <header>
              <strong>支付记录</strong>
              <span>{{ selectedOrder.payments.length }} 次尝试</span>
            </header>

            <div v-if="selectedOrder.payments.length" class="sandbox-payment-history-list">
              <article v-for="payment in selectedOrder.payments" :key="payment.id">
                <div>
                  <strong>{{ payment.payment_no }}</strong>
                  <span>{{ new Date(payment.created_at).toLocaleString('zh-CN') }}</span>
                </div>
                <div>
                  <span>输入 {{ formatMoney(payment.input_amount_cents) }}</span>
                  <span class="status-pill" :class="paymentStatusClass(payment)">
                    {{ paymentStatusLabel(payment) }}
                  </span>
                </div>
              </article>
            </div>
            <div v-else class="empty-state compact">暂无支付记录。</div>
          </section>

          <footer>
            <button
              v-if="selectedOrder.payment_status !== 'paid' && selectedOrder.status !== 'cancelled'"
              class="primary-button"
              type="button"
              @click="openPayment(selectedOrder)"
            >
              继续支付
            </button>
            <button
              v-if="selectedOrder.refundable_amount_cents > 0"
              class="ghost-button"
              type="button"
              @click="openRefund(selectedOrder)"
            >
              模拟退款
            </button>
            <button class="ghost-button" type="button" @click="modal = ''">关闭</button>
          </footer>
        </template>
      </section>
    </div>
  </div>
</template>
