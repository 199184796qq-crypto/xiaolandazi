<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { confirmAction } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  createIncentiveProgram,
  getIncentivePrograms,
  publishIncentiveProgram,
  saveIncentiveProgramDraft,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  IncentiveProgram,
  IncentiveProgramInput,
  IncentiveRule,
  IncentiveRuleInput,
} from '../types'

const props = withDefaults(
  defineProps<{ mode?: 'referral' | 'settlement' }>(),
  { mode: 'referral' },
)

interface EditorRule {
  event_type: string
  value: number
  minimum_yuan: number
  refund_reversal: boolean
  enabled: boolean
}

const loading = ref(false)
const saving = ref(false)
const publishingId = ref<number | null>(null)
const error = useFeedbackErrorRef()
const items = ref<IncentiveProgram[]>([])
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('name-asc')
const page = ref(1)
const pageSize = ref(12)
const viewMode = ref<'card' | 'table'>('table')
const editorOpen = ref(false)
const editing = ref<IncentiveProgram | null>(null)
const editorRules = ref<EditorRule[]>([])

const form = reactive({
  code: '',
  name: '',
  program_type: 'referral',
  description: '',
  pending_days: 0,
})

const pageTitle = computed(() =>
  props.mode === 'referral' ? '奖励管理' : '结算规则',
)
const kicker = computed(() =>
  props.mode === 'referral' ? 'MARKETING REWARDS' : 'SETTLEMENT RULES',
)
const pageDescription = computed(() =>
  props.mode === 'referral'
    ? '配置老带新等推荐奖励的触发事件、固定金额、冻结期和退款冲回规则。规则发布后生成不可变版本；AI 时长奖励继续走“AI 时长 → 财务审批 → 时长流水”的真实入账链路。'
    : '分别配置销售提成和代理返佣比例、最低订单金额与冻结期。历史订单继续引用下单时规则版本。',
)
const navContext = computed(() =>
  props.mode === 'referral' ? 'activityMarketing' as const : 'finance' as const,
)

const canManage = computed(() => {
  const bootstrap = session.bootstrap
  if (!bootstrap) return false
  if (bootstrap.actor.role === 'platform_admin') return true
  const access = bootstrap.staff_access
  const permission =
    props.mode === 'referral'
      ? 'commercial.referral.manage'
      : 'finance.settlement_rules.manage'
  return Boolean(
    access &&
      (access.is_super_admin ||
        access.permissions.includes(permission)),
  )
})

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '已发布', value: 'published' },
  { label: '有草稿', value: 'draft' },
  { label: '未发布', value: 'unpublished' },
]

const sortOptions = [
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '名称 Z-A', value: 'name-desc' },
  { label: '最近更新', value: 'updated-desc' },
]

const eventOptions = computed(() => {
  if (props.mode === 'referral') {
    return [
      { label: '首次充值完成', value: 'first_recharge' },
      { label: '会员购买完成', value: 'membership_paid' },
      { label: '时长卡购买完成', value: 'time_card_paid' },
    ]
  }
  return [
    { label: '订单支付完成', value: 'order_paid' },
    { label: '会员购买完成', value: 'membership_paid' },
    { label: '时长卡购买完成', value: 'time_card_paid' },
  ]
})

const filteredItems = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const result = items.value.filter((item) => {
    const matchesKeyword =
      !keyword ||
      [item.name, item.code, item.description, programTypeLabel(item.program_type)]
        .some((value) => String(value || '').toLowerCase().includes(keyword))
    const matchesStatus =
      statusFilter.value === 'all' ||
      (statusFilter.value === 'published' && Boolean(item.active_version)) ||
      (statusFilter.value === 'draft' && Boolean(item.draft_version)) ||
      (statusFilter.value === 'unpublished' && !item.active_version)
    return matchesKeyword && matchesStatus
  })

  return [...result].sort((a, b) => {
    if (sortMode.value === 'name-desc') return b.name.localeCompare(a.name, 'zh-CN')
    if (sortMode.value === 'updated-desc') {
      return new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime()
    }
    return a.name.localeCompare(b.name, 'zh-CN')
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
watch(() => props.mode, () => {
  page.value = 1
  load()
})

function displayVersion(item: IncentiveProgram) {
  return item.draft_version || item.active_version || item.latest_version
}

function programTypeLabel(value: string) {
  const map: Record<string, string> = {
    referral: '推荐奖励',
    sales_commission: '销售提成',
    agent_settlement: '代理返佣',
  }
  return map[value] || value
}

function eventLabel(value: string) {
  return eventOptions.value.find((item) => item.value === value)?.label || value
}

function versionLabel(item: IncentiveProgram) {
  if (item.active_version && item.draft_version) {
    return '已发布 V' + item.active_version.version_no + ' · 草稿 V' + item.draft_version.version_no
  }
  if (item.draft_version) return '草稿 V' + item.draft_version.version_no
  if (item.active_version) return '已发布 V' + item.active_version.version_no
  return '未发布'
}

function parseJSON(value: string) {
  try {
    return JSON.parse(value || '{}') as Record<string, unknown>
  } catch {
    return {}
  }
}

function ruleValue(rule: IncentiveRule) {
  const action = parseJSON(rule.action_config_json)
  if (props.mode === 'referral') {
    return '奖励 ¥' + ((Number(action.amount_cents || 0)) / 100).toFixed(2)
  }
  return '比例 ' + (Number(action.rate_bps || 0) / 100).toFixed(2).replace(/\.00$/, '') + '%'
}

function ruleMinimum(rule: IncentiveRule) {
  const conditions = parseJSON(rule.conditions_json)
  const cents = Number(conditions.minimum_paid_cents || 0)
  return cents > 0 ? '最低 ¥' + (cents / 100).toFixed(2) : '无金额门槛'
}

function resetForm() {
  form.code = ''
  form.name = ''
  form.program_type = props.mode === 'referral' ? 'referral' : 'sales_commission'
  form.description = ''
  form.pending_days = 0
  editorRules.value = [
    {
      event_type: eventOptions.value[0]?.value || 'order_paid',
      value: props.mode === 'referral' ? 10 : 10,
      minimum_yuan: 0,
      refund_reversal: true,
      enabled: true,
    },
  ]
}

function openCreate() {
  editing.value = null
  resetForm()
  editorOpen.value = true
  error.value = ''
}

function openEdit(item: IncentiveProgram) {
  editing.value = item
  const version = displayVersion(item)
  form.code = item.code
  form.name = item.name
  form.program_type = item.program_type
  form.description = item.description
  form.pending_days = version?.pending_days || 0
  editorRules.value = (version?.rules || []).map((rule) => {
    const conditions = parseJSON(rule.conditions_json)
    const action = parseJSON(rule.action_config_json)
    return {
      event_type: rule.event_type,
      value:
        props.mode === 'referral'
          ? Number(action.amount_cents || 0) / 100
          : Number(action.rate_bps || 0) / 100,
      minimum_yuan: Number(conditions.minimum_paid_cents || 0) / 100,
      refund_reversal: Boolean(conditions.refund_reversal ?? true),
      enabled: rule.enabled,
    }
  })
  if (!editorRules.value.length) {
    addRule()
  }
  editorOpen.value = true
  error.value = ''
}

function addRule() {
  editorRules.value.push({
    event_type: eventOptions.value[0]?.value || 'order_paid',
    value: props.mode === 'referral' ? 10 : 10,
    minimum_yuan: 0,
    refund_reversal: true,
    enabled: true,
  })
}

function removeRule(index: number) {
  if (editorRules.value.length <= 1) return
  editorRules.value.splice(index, 1)
}

function buildInput(): IncentiveProgramInput {
  const rules: IncentiveRuleInput[] = editorRules.value.map((rule, index) => ({
    priority: (index + 1) * 10,
    event_type: rule.event_type,
    conditions_json: JSON.stringify({
      minimum_paid_cents: Math.max(0, Math.round(Number(rule.minimum_yuan || 0) * 100)),
      refund_reversal: rule.refund_reversal,
      referral_level: props.mode === 'referral' ? 1 : undefined,
    }),
    action_type: props.mode === 'referral' ? 'fixed_amount' : 'percent_paid_amount',
    action_config_json: JSON.stringify(
      props.mode === 'referral'
        ? { amount_cents: Math.max(0, Math.round(Number(rule.value || 0) * 100)) }
        : { rate_bps: Math.max(0, Math.round(Number(rule.value || 0) * 100)) },
    ),
    enabled: rule.enabled,
  }))

  return {
    code: form.code.trim(),
    name: form.name.trim(),
    program_type: form.program_type,
    description: form.description.trim(),
    pending_days: Math.max(0, Math.round(Number(form.pending_days || 0))),
    rules,
  }
}

async function save() {
  if (!form.code.trim() || !form.name.trim()) {
    error.value = '内部编码和规则名称不能为空'
    return
  }
  if (!editorRules.value.length) {
    error.value = '至少配置一条规则'
    return
  }

  saving.value = true
  error.value = ''
  try {
    const input = buildInput()
    if (editing.value) {
      await saveIncentiveProgramDraft(editing.value.id, input)
    } else {
      await createIncentiveProgram(input)
    }
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存规则失败'
  } finally {
    saving.value = false
  }
}

async function publish(item: IncentiveProgram) {
  if (!item.draft_version) return
  if (!(await confirmAction({ title: '发布奖励规则', message: '确认发布“' + item.name + '”当前草稿？新业务将引用新版本，历史业务继续保留原版本。', confirmText: '确认发布' }))) return

  publishingId.value = item.id
  error.value = ''
  try {
    await publishIncentiveProgram(item.id)
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
    if (props.mode === 'referral') {
      const data = await getIncentivePrograms('referral')
      items.value = data.items
    } else {
      const [sales, agents] = await Promise.all([
        getIncentivePrograms('sales_commission'),
        getIncentivePrograms('agent_settlement'),
      ])
      items.value = [...sales.items, ...agents.items]
    }
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取规则失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page incentive-program-page">
    <ModulePageNav :context="navContext" :active-title="pageTitle" :active-nav-title="pageTitle" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">{{ kicker }}</p>
        <h2>{{ pageTitle }}</h2>
        <p>{{ pageDescription }}</p>
      </div>
      <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
        ＋ 新建{{ props.mode === 'referral' ? '营销奖励' : '结算' }}规则
      </button>
    </section>

    <section class="settings-card feature-workspace-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        :search-placeholder="'规则名称 / 编码 / ' + (props.mode === 'referral' ? '触发事件' : '结算类型')"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <p v-if="error" class="inline-error">{{ error }}</p>
      <div v-if="loading" class="panel-loading">正在读取规则...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table">
          <thead>
            <tr>
              <th>规则</th>
              <th>类型</th>
              <th>冻结期</th>
              <th>规则明细</th>
              <th>版本状态</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedItems" :key="item.id">
              <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
              <td>{{ programTypeLabel(item.program_type) }}</td>
              <td>{{ displayVersion(item)?.pending_days || 0 }} 天</td>
              <td>
                <div class="incentive-rule-summary">
                  <span
                    v-for="rule in (displayVersion(item)?.rules || []).slice(0, 3)"
                    :key="rule.id"
                  >
                    {{ eventLabel(rule.event_type) }} · {{ ruleValue(rule) }} · {{ ruleMinimum(rule) }}
                  </span>
                </div>
              </td>
              <td><span class="status-pill">{{ versionLabel(item) }}</span></td>
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
        <div v-if="filteredItems.length === 0" class="empty-state">暂无规则。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in pagedItems" :key="item.id" class="feature-record-card">
          <header>
            <div><span>{{ item.code }}</span><h3>{{ item.name }}</h3></div>
            <span class="status-pill">{{ versionLabel(item) }}</span>
          </header>
          <dl>
            <div><dt>类型</dt><dd>{{ programTypeLabel(item.program_type) }}</dd></div>
            <div><dt>冻结期</dt><dd>{{ displayVersion(item)?.pending_days || 0 }} 天</dd></div>
            <div><dt>规则数</dt><dd>{{ displayVersion(item)?.rules.length || 0 }}</dd></div>
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
      <section class="feature-editor-panel incentive-editor-panel">
        <header>
          <div>
            <span class="section-kicker">{{ editing ? 'EDIT DRAFT' : 'CREATE' }}</span>
            <h3>{{ editing ? '编辑规则草稿' : '新建规则' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label><span>内部编码 *</span><input v-model="form.code" type="text" /></label>
          <label><span>规则名称 *</span><input v-model="form.name" type="text" /></label>

          <label v-if="props.mode === 'settlement'">
            <span>结算类型 *</span>
            <select v-model="form.program_type">
              <option value="sales_commission">销售提成</option>
              <option value="agent_settlement">代理返佣</option>
            </select>
          </label>

          <label>
            <span>冻结期</span>
            <div class="feature-input-unit">
              <input v-model.number="form.pending_days" type="number" min="0" />
              <span>天</span>
            </div>
          </label>

          <label class="feature-editor-wide">
            <span>说明</span>
            <textarea v-model="form.description" rows="3" />
          </label>
        </div>

        <section class="incentive-rule-editor">
          <header>
            <div>
              <strong>规则明细</strong>
              <span>按优先级从上到下执行</span>
            </div>
            <button class="ghost-button" type="button" @click="addRule">＋ 添加一条</button>
          </header>

          <article
            v-for="(rule, index) in editorRules"
            :key="index"
            class="incentive-rule-row"
          >
            <span class="incentive-rule-order">{{ index + 1 }}</span>
            <label>
              <span>触发事件</span>
              <select v-model="rule.event_type">
                <option v-for="option in eventOptions" :key="option.value" :value="option.value">
                  {{ option.label }}
                </option>
              </select>
            </label>
            <label>
              <span>{{ props.mode === 'referral' ? '奖励金额' : '结算比例' }}</span>
              <div class="feature-input-unit">
                <input v-model.number="rule.value" type="number" min="0" step="0.01" />
                <span>{{ props.mode === 'referral' ? '元' : '%' }}</span>
              </div>
            </label>
            <label>
              <span>最低支付金额</span>
              <div class="feature-input-unit">
                <input v-model.number="rule.minimum_yuan" type="number" min="0" step="0.01" />
                <span>元</span>
              </div>
            </label>
            <label v-if="props.mode === 'referral'" class="feature-checkbox incentive-rule-check">
              <input v-model="rule.refund_reversal" type="checkbox" />
              <span>退款冲回</span>
            </label>
            <label class="feature-checkbox incentive-rule-check">
              <input v-model="rule.enabled" type="checkbox" />
              <span>启用</span>
            </label>
            <button
              class="icon-button danger"
              type="button"
              :disabled="editorRules.length <= 1"
              @click="removeRule(index)"
            >
              ×
            </button>
          </article>
        </section>

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
