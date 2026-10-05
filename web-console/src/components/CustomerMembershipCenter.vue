<script setup lang="ts">
import { claimFreeMarketingOrder } from '../commerce'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import {
  floorToWholeYuanCents,
  marketingPayableCents,
  wholeYuanPerHour,
} from '../pricingRules'
import {
  createCustomerShopOrder,
  getAccountDashboard,
  getCustomerMembershipOffers,
  sandboxPayCustomerShopOrder,
} from '../api'
import type {
  AccountDashboard,
  CustomerMembershipOffer,
  MarketingCampaign,
  MarketingCampaignItem,
} from '../types'

interface CycleTab {
  key: string
  label: string
  months: number
}

interface OfferPricing {
  months: number
  discountBps: number
  listCents: number
  payableCents: number
  savingCents: number
}

const dashboard = ref<AccountDashboard | null>(null)
const offers = ref<CustomerMembershipOffer[]>([])
const open = ref(false)
const loading = ref(false)
const purchasingPlanId = ref<number | null>(null)
const error = ref('')
const success = ref('')
const selectedCycle = ref('base')
let statusRefreshTimer: number | undefined

const cycleTabs = computed<CycleTab[]>(() => {
  const months = new Set<number>()
  for (const offer of offers.value) {
    for (const campaign of offer.marketing_campaigns ?? []) {
      const item = membershipCampaignItem(campaign, offer.id)
      if (campaign.status === 'active' && item?.pricing_mode === 'package') {
        months.add(Math.max(1, item.package_months || 1))
      }
    }
  }
  const tabs: CycleTab[] = [{ key: 'base', label: '单月', months: 1 }]
  for (const value of [...months].sort((a, b) => a - b)) {
    const label =
      value === 1 ? '包月' :
      value === 3 ? '包季' :
      value === 6 ? '包半年' :
      value === 12 ? '包年' : value + '个月'
    tabs.push({ key: 'package:' + value, label, months: value })
  }
  return tabs
})

const aiHours = computed(() => (dashboard.value?.quota.total_seconds ?? 0) / 3600)
const aiHoursText = computed(() => {
  const value = aiHours.value
  if (value >= 1000) return Math.floor(value).toLocaleString('zh-CN')
  if (value >= 100) return value.toFixed(0)
  return value.toFixed(1)
})

const currentMembershipName = computed(
  () => dashboard.value?.membership?.plan_name || '普通用户',
)

const currentMembershipTone = computed(() => {
  const planId = dashboard.value?.membership?.plan_id
  const index = offers.value.findIndex((item) => item.id === planId)
  if (index < 0) return 'level-basic'
  return 'level-' + Math.min(index + 1, 4)
})

function normalizeDiscountBps(value: number) {
  if (value >= 100 && value < 1000) return value * 10
  if (value > 10000) return 10000
  return Math.max(0, value || 0)
}

function membershipCampaignItem(
  campaign: MarketingCampaign,
  offerId: number,
): MarketingCampaignItem | undefined {
  const item = (campaign.items ?? []).find(
    (entry) => entry.target_type === 'membership' && entry.target_id === offerId,
  )
  if (item) return item
  if (campaign.target_type === 'membership' && campaign.target_id === offerId) {
    return {
      target_type: 'membership',
      target_id: offerId,
      pricing_mode: campaign.pricing_mode || 'discount',
      package_months: campaign.package_months || 1,
      discount_bps: campaign.discount_bps ?? 10000,
      quantity: 1,
    }
  }
  return undefined
}

function campaignFor(offer: CustomerMembershipOffer, cycle = selectedCycle.value) {
  const campaigns = (offer.marketing_campaigns ?? []).filter(
    (campaign) => campaign.status === 'active' && campaign.eligible !== false && Boolean(membershipCampaignItem(campaign, offer.id)),
  )
  if (cycle === 'base') {
    return campaigns
      .filter((campaign) => membershipCampaignItem(campaign, offer.id)?.pricing_mode !== 'package')
      .sort((a, b) => {
        const aDiscount = membershipCampaignItem(a, offer.id)?.discount_bps ?? 10000
        const bDiscount = membershipCampaignItem(b, offer.id)?.discount_bps ?? 10000
        return a.sort_order - b.sort_order || aDiscount - bDiscount
      })[0]
  }
  const months = Number(cycle.split(':')[1] || 0)
  return campaigns
    .filter(
      (campaign) =>
        membershipCampaignItem(campaign, offer.id)?.pricing_mode === 'package' &&
        membershipCampaignItem(campaign, offer.id)?.package_months === months,
    )
    .sort((a, b) => {
      const aDiscount = membershipCampaignItem(a, offer.id)?.discount_bps ?? 10000
      const bDiscount = membershipCampaignItem(b, offer.id)?.discount_bps ?? 10000
      return a.sort_order - b.sort_order || aDiscount - bDiscount
    })[0]
}

function pricingFor(offer: CustomerMembershipOffer, cycle = selectedCycle.value): OfferPricing {
  const campaign = campaignFor(offer, cycle)
  const campaignItem = campaign ? membershipCampaignItem(campaign, offer.id) : undefined
  const months =
    cycle === 'base'
      ? 1
      : Math.max(1, Number(cycle.split(':')[1] || campaignItem?.package_months || 1))
  const discountBps = Math.min(
    Math.max(normalizeDiscountBps(campaignItem?.discount_bps ?? 10000), 0),
    10000,
  )
  const rawListCents = offer.monthly_price_cents * months * Math.max(1,campaignItem?.quantity || 1)
  const listCents = floorToWholeYuanCents(rawListCents)
  const payableCents = campaignItem?.pricing_mode==='fixed' ? campaignItem.fixed_price_cents??0 : campaignItem?.pricing_mode==='free' ? 0 : marketingPayableCents(listCents, discountBps)
  return {
    months,
    discountBps,
    listCents,
    payableCents,
    savingCents: Math.max(0, listCents - payableCents),
  }
}

function bestDiscountForCycle(cycle: string) {
  if (!offers.value.length) return 10000
  return offers.value.reduce((best, offer) => {
    const campaign = campaignFor(offer, cycle)
    if (cycle !== 'base' && !campaign) return best
    const item = campaign ? membershipCampaignItem(campaign, offer.id) : undefined
    const current = Math.min(
      Math.max(normalizeDiscountBps(item?.discount_bps ?? 10000), 0),
      10000,
    )
    return Math.min(best, current)
  }, 10000)
}

function formatMoney(cents: number) {
  const yuan = cents / 100
  return '¥' + yuan.toLocaleString('zh-CN', {
    maximumFractionDigits: 2,
  })
}

function formatHours(seconds: number) {
  const hours = seconds / 3600
  return Number.isInteger(hours)
    ? hours.toLocaleString('zh-CN') + ' 小时'
    : hours.toFixed(1) + ' 小时'
}

function formatHourlyPrice(offer: CustomerMembershipOffer) {
  const pricing = pricingFor(offer)
  const monthlyHours = offer.included_seconds / 3600
  const totalHours = monthlyHours * pricing.months
  if (totalHours <= 0) return '¥0/时'
  return `¥${wholeYuanPerHour(pricing.payableCents, totalHours)}/时`
}

function formatDiscount(bps: number) {
  const safeBps = normalizeDiscountBps(bps)
  if (safeBps <= 0) return '赠送'
  if (safeBps >= 10000) return '原价'
  const zhe = safeBps / 1000
  return zhe.toFixed(zhe % 1 === 0 ? 0 : 1) + '折'
}

function cycleLabel(cycle = selectedCycle.value) {
  return cycleTabs.value.find((item) => item.key === cycle)?.label || '单月'
}

function selectedCampaignName(offer: CustomerMembershipOffer) {
  return campaignFor(offer)?.name || (selectedCycle.value === 'base' ? '单月基础价' : '暂无对应活动')
}

function offerSupportsSelectedCycle(offer: CustomerMembershipOffer) {
  return selectedCycle.value === 'base' || Boolean(campaignFor(offer))
}

function newKey(prefix: string) {
  const id =
    typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
      ? crypto.randomUUID()
      : Date.now().toString(36) + '-' + Math.random().toString(36).slice(2)
  return prefix + '-' + id
}

async function loadStatus() {
  try {
    dashboard.value = await getAccountDashboard()
  } catch {
    // Header keeps the last known value when a background refresh fails.
  }
}

async function loadHeader() {
  await loadStatus()
  try {
    const membershipData = await getCustomerMembershipOffers()
    offers.value = membershipData.items
  } catch {
    // Membership badge keeps its neutral appearance until offers are available.
  }
}

async function loadCenter() {
  loading.value = true
  error.value = ''
  try {
    const [account, membershipData] = await Promise.all([
      getAccountDashboard(),
      getCustomerMembershipOffers(),
    ])
    dashboard.value = account
    offers.value = membershipData.items
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取会员方案失败'
  } finally {
    loading.value = false
  }
}

async function openCenter() {
  open.value = true
  success.value = ''
  error.value = ''
  selectedCycle.value = 'base'
  await loadCenter()
}

function closeCenter() {
  if (purchasingPlanId.value !== null) return
  open.value = false
}

async function purchaseMembership(offer: CustomerMembershipOffer) {
  if (purchasingPlanId.value !== null) return
  if (!offerSupportsSelectedCycle(offer)) {
    error.value = '当前会员档位没有挂链这个周期的营销活动。'
    return
  }
  const campaign = campaignFor(offer)

  purchasingPlanId.value = offer.id
  error.value = ''
  success.value = ''

  try {
    const order = await createCustomerShopOrder({
      product_type: 'membership',
      product_id: offer.id,
      quantity: 1,
      membership_cycle: campaign ? 'campaign:' + campaign.id : 'single_month',
      marketing_campaign_id: campaign?.id,
      marketing_placement: 'membership',
      idempotency_key: newKey('membership-order'),
    })

    const result = order.payable_amount_cents===0 ? await claimFreeMarketingOrder(order.id) : await sandboxPayCustomerShopOrder(order.id, {
      amount_cents: order.payable_amount_cents,
      simulate_result: 'success',
      idempotency_key: newKey('membership-pay'),
    })

    dashboard.value = await getAccountDashboard()
    const refreshed = await getCustomerMembershipOffers()
    offers.value = refreshed.items
    window.dispatchEvent(new CustomEvent('membership-updated'))
    success.value =
      '购买成功：' +
      offer.name +
      ' · ' +
      cycleLabel() +
      '，本次支付 ' +
      formatMoney(result.order.paid_amount_cents) +
      '。'
  } catch (value) {
    error.value = value instanceof Error ? value.message : '会员购买失败'
  } finally {
    purchasingPlanId.value = null
  }
}

onMounted(() => {
  void loadHeader()
  statusRefreshTimer = window.setInterval(loadStatus, 5000)
})

onBeforeUnmount(() => {
  if (statusRefreshTimer !== undefined) window.clearInterval(statusRefreshTimer)
})
</script>

<template>
  <button
    class="customer-membership-trigger"
    type="button"
    aria-label="查看 AI 剩余时长和会员中心"
    @click="openCenter"
  >
    <span class="membership-trigger-time">
      <i>⚡</i>
      <span>
        <small>AI 剩余</small>
        <strong>{{ aiHoursText }}</strong>
      </span>
      <em>小时</em>
    </span>
    <span class="membership-trigger-divider"></span>
    <span class="membership-trigger-level" :class="currentMembershipTone">
      <i>◆</i>
      <span>
        <small>当前会员</small>
        <strong>{{ currentMembershipName }}</strong>
      </span>
      <b>›</b>
    </span>
  </button>

  <Teleport to="body">
    <div
      v-if="open"
      class="membership-center-backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="会员购买"
      @click.self="closeCenter"
    >
      <section class="membership-center-modal">
        <header class="membership-center-banner">
          <div class="membership-banner-copy">
            <span>MEMBERSHIP CENTER</span>
            <h2>升级会员，享受更多直播权益</h2>
            <p>购买周期和折扣来自后台“营销设计”，会员产品本身只保存基础价格与权益。</p>
          </div>
          <div class="membership-banner-visual" aria-hidden="true">
            <span class="visual-orbit orbit-one"></span>
            <span class="visual-orbit orbit-two"></span>
            <strong>AI</strong>
            <small>LIVE</small>
          </div>
          <button class="membership-center-close" type="button" @click="closeCenter">×</button>
        </header>

        <div class="membership-center-body">
          <div v-if="loading" class="membership-center-loading">正在读取会员方案...</div>
          <div v-else-if="!offers.length" class="membership-center-empty">
            当前没有已上架会员方案。
          </div>

          <template v-else>
            <div class="membership-period-tabs" aria-label="会员购买周期">
              <button
                v-for="tab in cycleTabs"
                :key="tab.key"
                type="button"
                :class="{ selected: selectedCycle === tab.key }"
                @click="selectedCycle = tab.key"
              >
                <span>{{ tab.label }}</span>
                <strong>
                  {{ bestDiscountForCycle(tab.key) < 10000 ? '低至 ' : '' }}{{ formatDiscount(bestDiscountForCycle(tab.key)) }}
                </strong>
              </button>
            </div>

            <div v-if="error" class="membership-center-message error">{{ error }}</div>
            <div v-if="success" class="membership-center-message success">{{ success }}</div>

            <div class="membership-offer-grid">
              <article
                v-for="(offer, index) in offers"
                :key="offer.id"
                class="membership-offer-card"
                :class="[
                  'offer-level-' + Math.min(index + 1, 4),
                  { current: dashboard?.membership?.plan_id === offer.id },
                ]"
              >
                <div class="membership-offer-head">
                  <span class="membership-level-mark">M{{ index + 1 }}</span>
                  <span
                    v-if="dashboard?.membership?.plan_id === offer.id"
                    class="membership-current-mark"
                  >
                    当前会员
                  </span>
                </div>

                <div class="membership-offer-title">
                  <div>
                    <h3>{{ offer.name }}</h3>
                    <p>{{ offer.description || 'AI 直播会员权益' }}</p>
                  </div>
                  <span class="membership-card-discount">
                    {{ offerSupportsSelectedCycle(offer) ? formatDiscount(pricingFor(offer).discountBps) : '暂无活动' }}
                  </span>
                </div>

                <div class="membership-campaign-name">
                  {{ selectedCampaignName(offer) }}
                </div>

                <div class="membership-offer-price">
                  <small>{{ cycleLabel() }}到手价</small>
                  <div>
                    <strong>{{ formatMoney(pricingFor(offer).payableCents) }}</strong>
                    <span>/ {{ pricingFor(offer).months }}个月</span>
                  </div>
                </div>

                <div class="membership-price-saving">
                  <del v-if="pricingFor(offer).savingCents > 0">
                    原价 {{ formatMoney(pricingFor(offer).listCents) }}
                  </del>
                  <span v-else>原价 {{ formatMoney(pricingFor(offer).listCents) }}</span>
                  <em v-if="pricingFor(offer).savingCents > 0">
                    立省 {{ formatMoney(pricingFor(offer).savingCents) }}
                  </em>
                </div>

                <div class="membership-offer-benefits">
                  <span>
                    每月基础 AI 时长
                    <strong>
                      {{ formatHours(offer.included_seconds) }}
                      <em class="membership-hour-price">（{{ formatHourlyPrice(offer) }}）</em>
                    </strong>
                  </span>
                  <span>超额时长卡 <strong>{{ formatDiscount(offer.time_card_discount_bps) }}</strong></span>
                  <span>设备购买 <strong>{{ formatDiscount(offer.device_discount_bps) }}</strong></span>
                </div>

                <button
                  type="button"
                  class="membership-buy-button"
                  :disabled="purchasingPlanId !== null || !offerSupportsSelectedCycle(offer)"
                  @click="purchaseMembership(offer)"
                >
                  {{
                    purchasingPlanId === offer.id
                      ? '正在支付...'
                      : !offerSupportsSelectedCycle(offer)
                        ? '本档暂无' + cycleLabel() + '活动'
                        : '立即购买 ' + cycleLabel() + ' · ' + formatMoney(pricingFor(offer).payableCents)
                  }}
                </button>
              </article>
            </div>

            <p class="membership-center-footnote">
              会员单月基础价由会员方案维护；促销折扣、包月、包季、包半年和包年统一来自“营销设计”中当前生效的活动。
            </p>
          </template>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.customer-membership-trigger {
  display: flex;
  min-height: 46px;
  align-items: stretch;
  padding: 0;
  overflow: hidden;
  border: 1px solid #e5e9f2;
  border-radius: 14px;
  color: #2b3241;
  background: linear-gradient(180deg, #ffffff 0%, #f8faff 100%);
  box-shadow: 0 6px 20px rgba(51, 69, 112, 0.06);
  transition: 0.2s ease;
}

.customer-membership-trigger:hover {
  border-color: #cfd9f3;
  box-shadow: 0 8px 24px rgba(58, 83, 157, 0.12), 0 0 18px rgba(83, 110, 214, 0.08);
  transform: translateY(-1px);
}

.membership-trigger-time,
.membership-trigger-level {
  display: flex;
  align-items: center;
}

.membership-trigger-time {
  gap: 7px;
  padding: 6px 11px 6px 10px;
}

.membership-trigger-time > i {
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: 8px;
  color: #2f6de8;
  font-size: 12px;
  font-style: normal;
  background: #eef4ff;
}

.membership-trigger-time > span,
.membership-trigger-level > span {
  display: flex;
  min-width: 0;
  flex-direction: column;
  align-items: flex-start;
}

.membership-trigger-time small,
.membership-trigger-level small {
  color: #9aa3b4;
  font-size: 9px;
  line-height: 1.1;
  white-space: nowrap;
}

.membership-trigger-time strong {
  margin-top: 2px;
  color: #2869df;
  font-size: 15px;
  line-height: 1;
}

.membership-trigger-time em {
  align-self: flex-end;
  margin-bottom: 2px;
  color: #9aa3b4;
  font-size: 9px;
  font-style: normal;
}

.membership-trigger-divider {
  width: 1px;
  margin: 8px 0;
  background: #e7ebf3;
}

.membership-trigger-level {
  min-width: 126px;
  gap: 8px;
  padding: 6px 9px 6px 11px;
}

.membership-trigger-level > i {
  display: grid;
  width: 25px;
  height: 25px;
  place-items: center;
  border-radius: 9px;
  font-size: 10px;
  font-style: normal;
}

.membership-trigger-level strong {
  max-width: 82px;
  margin-top: 2px;
  overflow: hidden;
  color: #454f64;
  font-size: 11px;
  line-height: 1.1;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.membership-trigger-level > b {
  margin-left: auto;
  color: #abb3c2;
  font-size: 15px;
  font-weight: 500;
}

.membership-trigger-level.level-basic > i {
  color: #7e8799;
  background: #f0f2f5;
}

.membership-trigger-level.level-1 > i {
  color: #6c58b7;
  background: #f0edff;
}

.membership-trigger-level.level-2 > i {
  color: #276cd8;
  background: #eaf3ff;
}

.membership-trigger-level.level-3 > i {
  color: #a97518;
  background: #fff5dc;
}

.membership-trigger-level.level-4 > i {
  color: #7955b8;
  background: linear-gradient(135deg, #efe8ff, #e7f2ff);
}

.membership-center-backdrop {
  position: fixed;
  z-index: 3000;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(18, 25, 42, 0.38);
  backdrop-filter: blur(8px);
}

.membership-center-modal {
  width: min(1160px, 96vw);
  max-height: 91vh;
  overflow: hidden;
  border: 1px solid rgba(202, 213, 237, 0.9);
  border-radius: 24px;
  background: #f7f9fd;
  box-shadow: 0 30px 80px rgba(32, 48, 82, 0.24), 0 0 30px rgba(75, 102, 201, 0.12);
}

.membership-center-banner {
  position: relative;
  display: flex;
  min-height: 148px;
  align-items: center;
  justify-content: space-between;
  padding: 28px 42px;
  overflow: hidden;
  color: #fff;
  background:
    radial-gradient(circle at 76% 20%, rgba(174, 219, 255, 0.42), transparent 26%),
    linear-gradient(120deg, #526cbe 0%, #6679c9 48%, #7891dd 100%);
}

.membership-center-banner::after {
  position: absolute;
  right: -80px;
  bottom: -100px;
  width: 340px;
  height: 240px;
  border: 1px solid rgba(255,255,255,.14);
  border-radius: 50%;
  content: "";
  transform: rotate(-18deg);
}

.membership-banner-copy {
  position: relative;
  z-index: 2;
}

.membership-banner-copy > span {
  color: rgba(255,255,255,.72);
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .18em;
}

.membership-banner-copy h2 {
  margin: 8px 0 8px;
  font-size: 27px;
  letter-spacing: -.03em;
}

.membership-banner-copy p {
  max-width: 620px;
  margin: 0;
  color: rgba(255,255,255,.84);
  font-size: 12px;
}

.membership-banner-visual {
  position: relative;
  z-index: 1;
  display: grid;
  width: 150px;
  height: 96px;
  place-items: center;
  margin-right: 48px;
  border: 1px solid rgba(255,255,255,.18);
  border-radius: 26px;
  background: rgba(255,255,255,.08);
  box-shadow: inset 0 0 30px rgba(255,255,255,.08);
  transform: rotate(-4deg);
}

.membership-banner-visual strong {
  font-size: 37px;
  letter-spacing: -.08em;
}

.membership-banner-visual small {
  margin-top: -30px;
  color: rgba(255,255,255,.62);
  font-size: 9px;
  letter-spacing: .32em;
}

.visual-orbit {
  position: absolute;
  border: 1px solid rgba(255,255,255,.2);
  border-radius: 50%;
}

.orbit-one {
  width: 118px;
  height: 46px;
  transform: rotate(24deg);
}

.orbit-two {
  width: 90px;
  height: 90px;
  transform: rotate(-20deg);
}

.membership-center-close {
  position: absolute;
  z-index: 3;
  top: 16px;
  right: 18px;
  display: grid;
  width: 34px;
  height: 34px;
  place-items: center;
  border-radius: 11px;
  color: #fff;
  font-size: 22px;
  background: rgba(255,255,255,.12);
}

.membership-center-close:hover {
  background: rgba(255,255,255,.2);
}

.membership-center-body {
  max-height: calc(91vh - 148px);
  padding: 22px 28px 26px;
  overflow-y: auto;
}

.membership-center-loading,
.membership-center-empty {
  padding: 64px 20px;
  color: #8790a2;
  text-align: center;
}

.membership-period-tabs {
  display: grid;
  width: min(680px, 100%);
  grid-template-columns: repeat(3, 1fr);
  gap: 4px;
  margin: 0 auto 20px;
  padding: 4px;
  border: 1px solid #e1e6f0;
  border-radius: 16px;
  background: #edf1f7;
}

.membership-period-tabs button {
  display: flex;
  min-height: 52px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  padding: 0 14px;
  border: 1px solid transparent;
  border-radius: 12px;
  color: #687285;
  background: transparent;
  transition: .18s ease;
}

.membership-period-tabs button > span {
  font-size: 15px;
  font-weight: 800;
}

.membership-period-tabs button > strong {
  padding: 5px 9px;
  border-radius: 999px;
  color: #8d6117;
  font-size: 12px;
  background: #fff0c3;
}

.membership-period-tabs button:hover {
  color: #2a2418;
  background: rgba(255, 251, 239, .9);
}

.membership-period-tabs button.selected {
  border-color: #c79a42;
  color: #f6e7bd;
  background: linear-gradient(180deg, #25231f 0%, #151411 100%);
  box-shadow:
    0 8px 20px rgba(92, 66, 17, .18),
    0 0 18px rgba(218, 171, 74, .14);
}

.membership-period-tabs button.selected > strong {
  color: #1b160d;
  background: linear-gradient(135deg, #f4dd9a 0%, #c89432 100%);
}

.membership-center-message {
  width: min(760px, 100%);
  margin: 0 auto 14px;
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 11px;
}

.membership-center-message.error {
  color: #b34853;
  background: #fff0f2;
}

.membership-center-message.success {
  color: #26704d;
  background: #ecf8f1;
}

.membership-offer-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
  gap: 18px;
}

.membership-offer-card {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 440px;
  flex-direction: column;
  padding: 26px 26px 24px;
  overflow: hidden;
  border: 1px solid rgba(205, 157, 58, .46);
  border-radius: 22px;
  color: #fff9ea;
  background:
    radial-gradient(circle at 84% 3%, rgba(216, 168, 67, .14), transparent 31%),
    linear-gradient(145deg, #1f1e1a 0%, #12110f 56%, #090908 100%);
  box-shadow:
    0 18px 40px rgba(28, 22, 11, .18),
    inset 0 1px 0 rgba(255, 233, 176, .05);
  transition:
    transform .24s ease,
    border-color .24s ease,
    box-shadow .24s ease;
}

.membership-offer-card::before {
  position: absolute;
  inset: 0;
  pointer-events: none;
  border-radius: inherit;
  background: linear-gradient(135deg, rgba(255, 224, 148, .07), transparent 28%, transparent 72%, rgba(188, 129, 34, .05));
  content: "";
}

.membership-offer-card:hover {
  border-color: rgba(243, 202, 108, .96);
  transform: translateY(-5px);
  box-shadow:
    0 26px 54px rgba(42, 31, 10, .28),
    0 0 22px rgba(231, 181, 73, .34),
    0 0 46px rgba(214, 160, 48, .16),
    inset 0 1px 0 rgba(255, 238, 190, .09);
}

.membership-offer-card.current {
  border-color: #ddb45a;
  box-shadow:
    0 22px 46px rgba(48, 35, 9, .22),
    0 0 24px rgba(221, 174, 67, .18);
}

.membership-offer-card:hover .membership-level-mark,
.membership-offer-card:hover .membership-card-discount {
  filter: brightness(1.08);
  box-shadow: 0 0 18px rgba(231, 183, 82, .28);
}

.membership-offer-card:hover .membership-buy-button {
  box-shadow:
    0 12px 28px rgba(188, 132, 32, .34),
    0 0 18px rgba(242, 200, 103, .30);
}

.membership-offer-head {
  display: flex;
  min-height: 30px;
  align-items: center;
  justify-content: flex-start;
}

.membership-level-mark,
.membership-current-mark {
  display: inline-flex;
  align-items: center;
  font-weight: 800;
}

.membership-level-mark {
  position: absolute;
  z-index: 2;
  top: 0;
  right: 0;
  min-width: 78px;
  min-height: 44px;
  justify-content: center;
  padding: 8px 14px 8px 18px;
  border-radius: 0 21px 0 18px;
  color: #1c160c;
  font-size: 14px;
  letter-spacing: .08em;
  background: linear-gradient(135deg, #f6dfa0 0%, #d6a849 48%, #b97f24 100%);
  box-shadow: 0 8px 18px rgba(106, 72, 15, .22);
}

.membership-current-mark {
  padding: 6px 10px;
  border: 1px solid rgba(239, 199, 107, .42);
  border-radius: 999px;
  color: #f1d38b;
  font-size: 11px;
  background: rgba(214, 166, 65, .10);
}

.membership-offer-title {
  display: flex;
  min-height: 78px;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-top: 14px;
}

.membership-offer-title h3 {
  margin: 0 0 7px;
  color: #fffaf0;
  font-size: 27px;
  font-weight: 850;
  letter-spacing: -.025em;
}

.membership-offer-title p {
  margin: 0;
  color: #e0cfa3;
  font-size: 14px;
  line-height: 1.55;
}

.membership-card-discount {
  flex: 0 0 auto;
  margin-top: 2px;
  padding: 8px 11px;
  border: 1px solid rgba(238, 198, 104, .38);
  border-radius: 10px;
  color: #f3d789;
  font-size: 16px;
  font-weight: 900;
  background: rgba(211, 162, 58, .11);
}

.membership-campaign-name {
  display: inline-flex;
  width: fit-content;
  max-width: 100%;
  margin-top: 4px;
  padding: 5px 9px;
  overflow: hidden;
  border: 1px solid rgba(238, 198, 104, .2);
  border-radius: 999px;
  color: #efd99c;
  font-size: 11px;
  font-weight: 750;
  background: rgba(255,255,255,.055);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.membership-offer-price {
  margin-top: 22px;
}

.membership-offer-price > small {
  color: #efcf7f;
  font-size: 14px;
  font-weight: 750;
}

.membership-offer-price > div {
  display: flex;
  align-items: baseline;
  gap: 8px;
  margin-top: 8px;
}

.membership-offer-price strong {
  color: #fff3cf;
  font-size: 48px;
  font-weight: 850;
  line-height: .98;
  letter-spacing: -.04em;
  text-shadow: 0 0 22px rgba(225, 179, 75, .12);
}

.membership-offer-price span {
  color: #d9c597;
  font-size: 14px;
  font-weight: 650;
}

.membership-price-saving {
  display: flex;
  min-height: 38px;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 12px;
}

.membership-price-saving del,
.membership-price-saving > span {
  color: #e4d4aa;
  font-size: 18px;
  font-weight: 800;
  line-height: 1.2;
}

.membership-price-saving em {
  padding: 7px 12px;
  border: 1px solid rgba(236, 193, 93, .38);
  border-radius: 9px;
  color: #f6d886;
  font-size: 18px;
  font-style: normal;
  font-weight: 900;
  line-height: 1.1;
  background: rgba(222, 172, 58, .13);
  box-shadow: inset 0 0 14px rgba(240, 197, 93, .05);
}

.membership-offer-benefits {
  display: grid;
  gap: 12px;
  margin-top: 18px;
  padding: 18px 0;
  border-top: 1px solid rgba(230, 190, 96, .16);
  border-bottom: 1px solid rgba(230, 190, 96, .16);
}

.membership-offer-benefits > span {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  color: #f3ead4;
  font-size: 14px;
  font-weight: 650;
}

.membership-offer-benefits strong {
  color: #f4d88f;
  font-size: 15px;
  font-weight: 850;
}

.membership-hour-price {
  margin-left: 4px;
  color: #fff0bf;
  font-size: 13px;
  font-style: normal;
  font-weight: 750;
  white-space: nowrap;
}

.offer-level-2 .membership-level-mark {
  background: linear-gradient(135deg, #f8e8b2 0%, #d9b15d 48%, #bb8430 100%);
}

.offer-level-3 .membership-level-mark {
  background: linear-gradient(135deg, #ffe9a8 0%, #e4b647 46%, #c78922 100%);
}

.offer-level-4 .membership-level-mark {
  background: linear-gradient(135deg, #fff0b5 0%, #e8bd58 45%, #bb7e1d 100%);
}

.membership-buy-button {
  width: 100%;
  min-height: 52px;
  margin-top: auto;
  padding: 0 18px;
  border: 1px solid rgba(255, 230, 166, .35);
  border-radius: 13px;
  color: #1b160d;
  font-size: 15px;
  font-weight: 900;
  background: linear-gradient(135deg, #f6dfa0 0%, #d6a747 48%, #b97f25 100%);
  box-shadow: 0 10px 24px rgba(153, 105, 26, .24);
  transition:
    transform .18s ease,
    filter .18s ease,
    box-shadow .18s ease;
}

.membership-buy-button:hover:not(:disabled) {
  filter: brightness(1.08);
  transform: translateY(-2px);
  box-shadow:
    0 14px 30px rgba(176, 119, 25, .34),
    0 0 20px rgba(242, 200, 103, .34);
}

.membership-buy-button:disabled {
  opacity: .6;
}

.membership-center-footnote {
  margin: 18px 0 0;
  color: #7b8494;
  font-size: 12px;
  text-align: center;
}

@media (max-width: 900px) {
  .membership-center-banner {
    padding: 24px;
  }

  .membership-banner-visual {
    display: none;
  }

  .membership-offer-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 620px) {
  .membership-center-backdrop {
    padding: 10px;
  }

  .membership-center-body {
    padding: 16px;
  }

  .membership-period-tabs {
    grid-template-columns: 1fr;
  }

  .membership-period-tabs button {
    justify-content: space-between;
  }

  .membership-offer-grid {
    grid-template-columns: 1fr;
  }

  .membership-offer-card {
    min-height: 360px;
  }
}
</style>
