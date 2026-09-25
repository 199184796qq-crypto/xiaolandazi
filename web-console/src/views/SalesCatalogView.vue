<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { getSalesCatalog } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import type {
  SalesCatalog,
  SalesCatalogDevice,
  SalesCatalogMembership,
  SalesCatalogTimeCard,
} from '../types'
import { useFeedbackErrorRef } from '../uiFeedback'

const loading = ref(false)
const error = useFeedbackErrorRef()
const notice = ref('')
const activeTab = ref<'membership' | 'time_card' | 'device'>('membership')
const catalog = ref<SalesCatalog>({
  memberships: [],
  time_cards: [],
  devices: [],
})

const currentCount = computed(() => {
  if (activeTab.value === 'membership') return catalog.value.memberships.length
  if (activeTab.value === 'time_card') return catalog.value.time_cards.length
  return catalog.value.devices.length
})

function money(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function hours(seconds: number) {
  const value = seconds / 3600
  return Number.isInteger(value) ? String(value) : value.toFixed(1)
}

function periodLabel(item: SalesCatalogMembership) {
  const unitLabels: Record<string, string> = {
    day: '天',
    month: '个月',
    year: '年',
  }
  return String(item.billing_period_count || 1) + (unitLabels[item.billing_period_unit] || item.billing_period_unit || '个月')
}

function timeCardActivationLabel(item: SalesCatalogTimeCard) {
  if (item.activation_mode === 'first_use') {
    return item.activation_deadline_days > 0
      ? '首次使用激活，购买后 ' + item.activation_deadline_days + ' 天内需激活'
      : '首次使用激活'
  }
  return '购买后按规则生效'
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    notice.value = '报价文案已复制，可以直接发给客户。'
    window.setTimeout(() => {
      if (notice.value.startsWith('报价文案已复制')) notice.value = ''
    }, 2200)
  } catch {
    error.value = '复制失败，请手动选择报价内容'
  }
}

function copyMembership(item: SalesCatalogMembership) {
  const lines = [
    '【会员方案】' + item.name,
    '销售价格：' + money(item.price_cents),
    '周期：' + periodLabel(item),
    item.included_seconds > 0 ? '包含 AI 时长：' + hours(item.included_seconds) + ' 小时' : '',
    item.description,
  ].filter(Boolean)
  return copyText(lines.join('\n'))
}

function copyTimeCard(item: SalesCatalogTimeCard) {
  const lines = [
    '【时长卡】' + item.name,
    '销售价格：' + money(item.price_cents),
    'AI 时长：' + hours(item.duration_seconds) + ' 小时',
    item.validity_days > 0 ? '激活后有效期：' + item.validity_days + ' 天' : '',
    timeCardActivationLabel(item),
    item.description,
  ].filter(Boolean)
  return copyText(lines.join('\n'))
}

function copyDevice(item: SalesCatalogDevice) {
  const lines = [
    '【设备】' + item.name,
    '销售价格：' + money(item.sale_price_cents),
    item.list_price_cents > item.sale_price_cents ? '标价：' + money(item.list_price_cents) : '',
    '当前销售库存：' + item.available_stock + (item.unit_label || '台'),
    item.description,
  ].filter(Boolean)
  return copyText(lines.join('\n'))
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    catalog.value = await getSalesCatalog()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取产品与报价失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page sales-catalog-page">
    <ModulePageNav context="workspace-sales" active-title="产品与报价" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">PRODUCTS & QUOTATION</p>
        <h2>产品与报价</h2>
        <p>这里读取后台已经正式发布的会员、时长卡和设备方案。销售可以复制报价，但不能在此修改价格、折扣、库存或佣金规则。</p>
      </div>
      <button class="ghost-button" type="button" :disabled="loading" @click="load">
        {{ loading ? '刷新中...' : '刷新方案' }}
      </button>
    </section>

    <p v-if="error" class="auth-error">{{ error }}</p>
    <p v-if="notice" class="inline-success">{{ notice }}</p>

    <section class="sales-catalog-summary">
      <button
        type="button"
        :class="{ active: activeTab === 'membership' }"
        @click="activeTab = 'membership'"
      >
        <span>会员方案</span>
      </button>
      <button
        type="button"
        :class="{ active: activeTab === 'time_card' }"
        @click="activeTab = 'time_card'"
      >
        <span>时长卡</span>
      </button>
      <button
        type="button"
        :class="{ active: activeTab === 'device' }"
        @click="activeTab = 'device'"
      >
        <span>设备</span>
      </button>
    </section>

    <section class="settings-card">
      <header class="settings-card-header">
        <div>
          <span class="section-kicker">PUBLISHED ONLY</span>
          <h3>
            {{ activeTab === 'membership' ? '会员方案' : activeTab === 'time_card' ? '时长卡方案' : '设备方案' }}
          </h3>
        </div>
        <span class="sales-catalog-count">{{ currentCount }} 个已发布方案</span>
      </header>

      <div v-if="loading" class="panel-loading">正在读取销售方案...</div>
      <div v-else-if="!currentCount" class="empty-state">当前没有已发布的该类销售方案。</div>

      <div v-else-if="activeTab === 'membership'" class="sales-catalog-grid">
        <article v-for="item in catalog.memberships" :key="item.id" class="sales-catalog-card">
          <div class="sales-catalog-card-head">
            <div>
              <span>会员</span>
              <h3>{{ item.name }}</h3>
            </div>
            <strong>{{ money(item.price_cents) }}</strong>
          </div>
          <p>{{ item.description || '暂无方案说明' }}</p>
          <dl>
            <div><dt>计费周期</dt><dd>{{ periodLabel(item) }}</dd></div>
            <div><dt>包含时长</dt><dd>{{ hours(item.included_seconds) }} 小时</dd></div>
            <div><dt>自动续费</dt><dd>{{ item.allow_auto_renew ? '支持' : '不支持' }}</dd></div>
          </dl>
          <button class="secondary-button" type="button" @click="copyMembership(item)">复制报价</button>
        </article>
      </div>

      <div v-else-if="activeTab === 'time_card'" class="sales-catalog-grid">
        <article v-for="item in catalog.time_cards" :key="item.id" class="sales-catalog-card">
          <div class="sales-catalog-card-head">
            <div>
              <span>时长卡</span>
              <h3>{{ item.name }}</h3>
            </div>
            <strong>{{ money(item.price_cents) }}</strong>
          </div>
          <p>{{ item.description || '暂无方案说明' }}</p>
          <dl>
            <div><dt>AI 时长</dt><dd>{{ hours(item.duration_seconds) }} 小时</dd></div>
            <div><dt>使用有效期</dt><dd>{{ item.validity_days > 0 ? item.validity_days + ' 天' : '按系统规则' }}</dd></div>
            <div><dt>激活方式</dt><dd>{{ timeCardActivationLabel(item) }}</dd></div>
          </dl>
          <button class="secondary-button" type="button" @click="copyTimeCard(item)">复制报价</button>
        </article>
      </div>

      <div v-else class="sales-catalog-grid">
        <article v-for="item in catalog.devices" :key="item.id" class="sales-catalog-card">
          <div class="sales-catalog-card-head">
            <div>
              <span>设备</span>
              <h3>{{ item.name }}</h3>
            </div>
            <strong>{{ money(item.sale_price_cents) }}</strong>
          </div>
          <p>{{ item.description || '暂无设备说明' }}</p>
          <dl>
            <div><dt>SKU</dt><dd>{{ item.sku_code }}</dd></div>
            <div><dt>当前库存</dt><dd>{{ item.available_stock }} {{ item.unit_label || '台' }}</dd></div>
            <div>
              <dt>参考标价</dt>
              <dd>{{ money(item.list_price_cents) }}</dd>
            </div>
          </dl>
          <button class="secondary-button" type="button" @click="copyDevice(item)">复制报价</button>
        </article>
      </div>
    </section>

    <div class="notice-card sales-catalog-boundary">
      <strong>销售权限边界</strong>
      <p>报价内容只来自已经发布的正式方案。销售端不提供改价、创建商品、改库存、改会员规则、改提成或审批功能。</p>
    </div>
  </div>
</template>

<style scoped>
.sales-catalog-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  margin: 18px 0;
}

.sales-catalog-summary button {
  display: flex;
  align-items: center;
  justify-content: space-between;
  min-height: 42px;
  font-size: 18px;
  border: 1px solid #e1e6f0;
  border-radius: 16px;
  padding: 0 18px;
  background: #fff;
  color: #677187;
  text-align: left;
}

.sales-catalog-summary button.active {
  border-color: #8192e9;
  background: linear-gradient(145deg, #f1f4ff, #fff);
  box-shadow: 0 10px 26px rgba(74, 91, 177, 0.1);
  color: #394baf;
}


.sales-catalog-count {
  color: #8e98ab;
  font-size: 13px;
}

.sales-catalog-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
}

.sales-catalog-card {
  display: flex;
  flex-direction: column;
  min-height: 290px;
  padding: 18px;
  border-radius: 17px;
  border: 1px solid #e7ebf3;
  background: #fbfcff;
}

.sales-catalog-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 15px;
}

.sales-catalog-card-head span {
  color: #7380c6;
  font-size: 11px;
  font-weight: 800;
  letter-spacing: 0.08em;
}

.sales-catalog-card-head h3 {
  margin: 5px 0 0;
}

.sales-catalog-card-head > strong {
  color: #3d50bb;
  font-size: 20px;
  white-space: nowrap;
}

.sales-catalog-card > p {
  min-height: 44px;
  margin: 14px 0;
  color: #7c879b;
  line-height: 1.55;
}

.sales-catalog-card dl {
  display: grid;
  gap: 8px;
  margin: 0 0 18px;
}

.sales-catalog-card dl > div {
  display: flex;
  justify-content: space-between;
  gap: 12px;
}

.sales-catalog-card dt {
  color: #939bad;
}

.sales-catalog-card dd {
  margin: 0;
  text-align: right;
  color: #475165;
  font-weight: 700;
}

.sales-catalog-card button {
  margin-top: auto;
}

.sales-catalog-boundary {
  margin-top: 16px;
}

.sales-catalog-boundary p {
  margin-bottom: 0;
}

@media (max-width: 1120px) {
  .sales-catalog-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .sales-catalog-summary,
  .sales-catalog-grid {
    grid-template-columns: 1fr;
  }
}
</style>
