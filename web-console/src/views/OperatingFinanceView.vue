<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, reactive, ref } from 'vue'
import { createTokenPurchase, getOperatingFinance } from '../api'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type { OperatingFinanceOverview, TokenPurchaseInput } from '../types'


const loading = ref(false)
const saving = ref(false)
const error = useFeedbackErrorRef()
const data = ref<OperatingFinanceOverview | null>(null)
const search = ref('')
const category = ref('all')
const direction = ref('all')
const showTokenPurchase = ref(false)
const entryPage = ref(1)
const entryPageSize = 20
const tokenSearch = ref('')
const tokenPage = ref(1)
const tokenPageSize = 20

const tokenForm = reactive({
  provider_name: '',
  model_scope: '',
  token_quantity: '',
  amount_yuan: '',
  payment_method: 'bank_transfer',
  invoice_no: '',
  purchased_at: '',
  note: '',
})

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes('finance.operating.manage')),
  )
})

const filteredEntries = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return (data.value?.entries || []).filter((item) => {
    const categoryMatch = category.value === 'all' || item.category === category.value
    const directionMatch =
      direction.value === 'all' || item.direction === direction.value
    const keywordMatch =
      !keyword ||
      [
        item.entry_no,
        item.business_no,
        item.counterparty_name,
        item.description,
        item.operator_name,
      ].some((value) => String(value || '').toLowerCase().includes(keyword))
    return categoryMatch && directionMatch && keywordMatch
  })
})
const entryPageCount = computed(() => Math.max(1, Math.ceil(filteredEntries.value.length / entryPageSize)))
const pagedEntries = computed(() => {
  const page = Math.min(entryPage.value, entryPageCount.value)
  const start = (page - 1) * entryPageSize
  return filteredEntries.value.slice(start, start + entryPageSize)
})
const filteredTokenPurchases = computed(() => {
  const keyword = tokenSearch.value.trim().toLowerCase()
  const rows = data.value?.token_purchases || []
  if (!keyword) return rows
  return rows.filter((item) =>
    [item.purchase_no, item.provider_name, item.model_scope, item.invoice_no, item.operator_name, item.note]
      .some((value) => String(value || '').toLowerCase().includes(keyword)),
  )
})
const tokenPageCount = computed(() => Math.max(1, Math.ceil(filteredTokenPurchases.value.length / tokenPageSize)))
const pagedTokenPurchases = computed(() => {
  const page = Math.min(tokenPage.value, tokenPageCount.value)
  const start = (page - 1) * tokenPageSize
  return filteredTokenPurchases.value.slice(start, start + tokenPageSize)
})

function money(cents: number) {
  return '¥' + (Number(cents || 0) / 100).toFixed(2)
}

function categoryLabel(value: string) {
  const map: Record<string, string> = {
    device_purchase: '设备采购',
    logistics: '物流费用',
    scrap_disposal: '报废处置',
    token_purchase: 'Token 采购',
    after_sales_parts: '售后配件',
    after_sales_labor: '售后人工',
    after_sales_external_repair: '上游维修',
    after_sales_inspection: '售后检测',
    after_sales_other: '售后其他',
  }
  return map[value] || value
}

function directionLabel(value: string) {
  return value === 'income' ? '收入' : '支出'
}

function resetTokenForm() {
  tokenForm.provider_name = ''
  tokenForm.model_scope = ''
  tokenForm.token_quantity = ''
  tokenForm.amount_yuan = ''
  tokenForm.payment_method = 'bank_transfer'
  tokenForm.invoice_no = ''
  tokenForm.purchased_at = ''
  tokenForm.note = ''
}

function openTokenPurchase() {
  resetTokenForm()
  error.value = ''
  showTokenPurchase.value = true
}

async function submitTokenPurchase() {
  const quantity = Number(tokenForm.token_quantity)
  const amountYuan = Number(tokenForm.amount_yuan)
  if (!tokenForm.provider_name.trim()) {
    error.value = '请填写 Token 供应商'
    return
  }
  if (!Number.isFinite(quantity) || quantity <= 0) {
    error.value = 'Token 数量必须大于 0'
    return
  }
  if (!Number.isFinite(amountYuan) || amountYuan <= 0) {
    error.value = '采购金额必须大于 0'
    return
  }

  const payload: TokenPurchaseInput = {
    provider_name: tokenForm.provider_name.trim(),
    model_scope: tokenForm.model_scope.trim(),
    token_quantity: Math.round(quantity),
    amount_cents: Math.round(amountYuan * 100),
    payment_method: tokenForm.payment_method,
    invoice_no: tokenForm.invoice_no.trim(),
    purchased_at: tokenForm.purchased_at
      ? new Date(tokenForm.purchased_at).toISOString()
      : undefined,
    note: tokenForm.note.trim(),
  }

  saving.value = true
  error.value = ''
  try {
    await createTokenPurchase(payload)
    showTokenPurchase.value = false
    await loadData()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '登记 Token 采购失败'
  } finally {
    saving.value = false
  }
}

async function loadData() {
  loading.value = true
  error.value = ''
  try {
    data.value = await getOperatingFinance()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取经营收支失败'
  } finally {
    loading.value = false
  }
}

onMounted(loadData)
</script>

<template>
  <div class="management-page operating-finance-page">
    <ModulePageNav context="finance" active-title="经营收支" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">OPERATING FINANCE</p>
        <h2>经营收支</h2>
        <p>
          设备采购、物流费用、售后维修费用、报废处置收入和 Token 采购成本统一进入公司经营台账，并可反查原业务单。
        </p>
      </div>
      <button
        v-if="canManage"
        class="primary-button"
        type="button"
        @click="openTokenPurchase"
      >
        ＋ 登记 Token 采购
      </button>
    </section>

    <section class="settings-card operating-ledger-panel">
      <header class="inventory-section-head">
        <div>
          <strong>经营收支流水</strong>
          <span>{{ filteredEntries.length }} 条 · 每笔均保留业务单号和经办人</span>
        </div>
        <button class="ghost-button" type="button" @click="loadData">刷新</button>
      </header>

      <div class="operating-filter-row">
        <input
          v-model="search"
          class="text-input"
          type="search"
          placeholder="搜索流水号 / 业务单号 / 对方 / 经办人"
          @input="entryPage = 1"
        />
        <select v-model="category" class="text-input" @change="entryPage = 1">
          <option value="all">全部业务</option>
          <option value="device_purchase">设备采购</option>
          <option value="logistics">物流费用</option>
          <option value="scrap_disposal">报废处置</option>
          <option value="token_purchase">Token 采购</option>
          <option value="after_sales_parts">售后配件</option>
          <option value="after_sales_labor">售后人工</option>
          <option value="after_sales_external_repair">上游维修</option>
          <option value="after_sales_inspection">售后检测</option>
          <option value="after_sales_other">售后其他</option>
        </select>
        <select v-model="direction" class="text-input" @change="entryPage = 1">
          <option value="all">全部收支</option>
          <option value="income">收入</option>
          <option value="expense">支出</option>
        </select>
      </div>

      <div v-if="loading" class="panel-loading">正在读取经营收支...</div>
      <div v-else class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>时间</th>
              <th>业务类型</th>
              <th>收支</th>
              <th>金额</th>
              <th>原业务单</th>
              <th>对方</th>
              <th>支付/收款</th>
              <th>经办人</th>
              <th>说明</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedEntries" :key="item.id">
              <td>{{ new Date(item.occurred_at).toLocaleString('zh-CN') }}</td>
              <td><span class="status-pill">{{ categoryLabel(item.category) }}</span></td>
              <td>
                <span
                  class="operating-direction"
                  :class="item.direction === 'income' ? 'income' : 'expense'"
                >
                  {{ directionLabel(item.direction) }}
                </span>
              </td>
              <td>
                <strong>{{ item.direction === 'income' ? '+' : '-' }}{{ money(item.amount_cents) }}</strong>
              </td>
              <td>
                <strong>{{ item.business_no || '—' }}</strong>
                <small>{{ item.business_type }}</small>
              </td>
              <td>{{ item.counterparty_name || '—' }}</td>
              <td>{{ item.payment_method || '—' }}</td>
              <td>{{ item.operator_name || (item.operator_user_id ? '#' + item.operator_user_id : '系统') }}</td>
              <td><small>{{ item.description || '—' }}</small></td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredEntries.length === 0" class="empty-state">暂无符合条件的经营流水。</div>
      </div>
      <PaginationBar
        :page="Math.min(entryPage, entryPageCount)"
        :total-pages="entryPageCount"
        :total="filteredEntries.length"
        :page-size="entryPageSize"
        @update:page="entryPage = $event"
      />
    </section>

    <section class="settings-card operating-token-panel">
      <header class="inventory-section-head">
        <div>
          <strong>Token 采购记录</strong>
          <span>{{ filteredTokenPurchases.length }} 条 · 记录供应商、模型范围、Token 数量、采购成本和发票信息。</span>
        </div>
      </header>
      <div class="operating-token-toolbar">
        <input
          v-model="tokenSearch"
          class="text-input"
          type="search"
          placeholder="搜索采购单 / 供应商 / 模型 / 发票 / 经办人"
          @input="tokenPage = 1"
        />
      </div>
      <div class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>采购单</th>
              <th>供应商</th>
              <th>模型 / 用途</th>
              <th>Token 数量</th>
              <th>采购金额</th>
              <th>采购时间</th>
              <th>经办人</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedTokenPurchases" :key="item.id">
              <td><strong>{{ item.purchase_no }}</strong></td>
              <td>{{ item.provider_name }}</td>
              <td>{{ item.model_scope || '通用' }}</td>
              <td>{{ item.token_quantity.toLocaleString('zh-CN') }}</td>
              <td><strong>{{ money(item.amount_cents) }}</strong></td>
              <td>{{ new Date(item.purchased_at).toLocaleString('zh-CN') }}</td>
              <td>{{ item.operator_name || (item.operator_user_id ? '#' + item.operator_user_id : '—') }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="!filteredTokenPurchases.length" class="empty-state">
          暂无 Token 采购记录。
        </div>
      </div>
      <PaginationBar
        :page="Math.min(tokenPage, tokenPageCount)"
        :total-pages="tokenPageCount"
        :total="filteredTokenPurchases.length"
        :page-size="tokenPageSize"
        @update:page="tokenPage = $event"
      />
    </section>

    <p v-if="error && !showTokenPurchase" class="inline-error">{{ error }}</p>

    <div
      v-if="showTokenPurchase"
      class="feature-editor-backdrop"
      @click.self="showTokenPurchase = false"
    >
      <section class="feature-editor-panel operating-token-editor">
        <header>
          <div>
            <span class="section-kicker">TOKEN PURCHASE</span>
            <h3>登记 Token 采购</h3>
          </div>
          <button class="icon-button" type="button" @click="showTokenPurchase = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label>
            <span>供应商 *</span>
            <input v-model="tokenForm.provider_name" type="text" placeholder="例如 阿里云 / 火山引擎" />
          </label>
          <label>
            <span>模型 / 用途</span>
            <input v-model="tokenForm.model_scope" type="text" placeholder="例如 Qwen / 声音 / LLM" />
          </label>
          <label>
            <span>Token 数量 *</span>
            <input v-model="tokenForm.token_quantity" type="number" min="1" step="1" />
          </label>
          <label>
            <span>采购总金额（元） *</span>
            <input v-model="tokenForm.amount_yuan" type="number" min="0.01" step="0.01" />
          </label>
          <label>
            <span>支付方式</span>
            <select v-model="tokenForm.payment_method">
              <option value="bank_transfer">银行转账</option>
              <option value="wechat">微信</option>
              <option value="alipay">支付宝</option>
              <option value="corporate_card">企业卡</option>
              <option value="other">其他</option>
            </select>
          </label>
          <label>
            <span>发票号</span>
            <input v-model="tokenForm.invoice_no" type="text" />
          </label>
          <label>
            <span>采购时间</span>
            <input v-model="tokenForm.purchased_at" type="datetime-local" />
          </label>
          <label class="feature-editor-wide">
            <span>备注</span>
            <textarea v-model="tokenForm.note" rows="4" />
          </label>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>
        <footer>
          <button class="ghost-button" type="button" @click="showTokenPurchase = false">取消</button>
          <button class="primary-button" type="button" :disabled="saving" @click="submitTokenPurchase">
            {{ saving ? '处理中...' : '确认登记' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>

<style scoped>
.operating-summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 18px;
}

.operating-summary-card {
  display: grid;
  gap: 8px;
  padding: 20px;
}

.operating-summary-card > span,
.operating-summary-card small {
  color: #737a91;
}

.operating-summary-card strong {
  font-size: 26px;
}

.operating-summary-card strong.negative {
  color: #c94f62;
}

.operating-ledger-panel,
.operating-token-panel {
  margin-bottom: 18px;
}

.operating-filter-row {
  display: grid;
  grid-template-columns: minmax(260px, 1fr) 180px 160px;
  gap: 12px;
  padding: 0 20px 18px;
}

.operating-direction {
  font-weight: 700;
}

.operating-direction.income {
  color: #1c8a62;
}

.operating-direction.expense {
  color: #c85c68;
}

.data-table td small {
  display: block;
  margin-top: 4px;
  color: #8a8fa0;
}

.operating-token-editor {
  width: min(760px, calc(100vw - 32px));
}

@media (max-width: 980px) {
  .operating-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .operating-filter-row {
    grid-template-columns: 1fr;
  }
}
</style>
