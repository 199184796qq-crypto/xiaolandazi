<script setup lang="ts">
import { useFeedbackErrorRef } from '../uiFeedback'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  assignAdminAgentLevel,
  createAdminAgentLevel,
  getAdminAgentLevels,
  getAdminAgents,
  updateAdminAgentLevel,
} from '../api'
import DataListControls from '../components/DataListControls.vue'
import ModulePageNav from '../components/ModulePageNav.vue'
import PaginationBar from '../components/PaginationBar.vue'
import { session } from '../session'
import type {
  AgentLevel,
  AgentLevelHistory,
  AgentSummary,
} from '../types'

const loading = ref(false)
const saving = ref(false)
const assigning = ref(false)
const error = useFeedbackErrorRef()
const levels = ref<AgentLevel[]>([])
const history = ref<AgentLevelHistory[]>([])
const agents = ref<AgentSummary[]>([])

const viewMode = ref<'card' | 'table'>('table')
const search = ref('')
const statusFilter = ref('all')
const sortMode = ref('sort-asc')
const page = ref(1)
const pageSize = ref(12)

const editorOpen = ref(false)
const editing = ref<AgentLevel | null>(null)
const form = reactive({
  code: '',
  name: '',
  status: 'active',
  entry_fee_yuan: '',
  included_devices: '0',
  device_discount_percent: '100',
  consumer_share_percent: '0',
  reserve_percent: '0',
  settlement_cycle: 'monthly',
  hold_days: '0',
  oem_enabled: false,
  note: '',
})

const assignment = reactive({
  organization_id: 0,
  level_id: 0,
  effective_at: '',
  reason: '',
})

const canManage = computed(() => {
  const access = session.bootstrap?.staff_access
  return Boolean(
    session.bootstrap?.actor.role === 'platform_admin' ||
      access?.is_super_admin ||
      access?.permissions.includes('agent.level.manage'),
  )
})

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '启用', value: 'active' },
  { label: '停用', value: 'inactive' },
]
const sortOptions = [
  { label: '默认排序', value: 'sort-asc' },
  { label: '名称 A-Z', value: 'name-asc' },
  { label: '门槛从高到低', value: 'fee-desc' },
  { label: '返还比例从高到低', value: 'share-desc' },
]

const filteredLevels = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  const items = levels.value.filter((item) => {
    const matchSearch =
      !keyword ||
      [item.name, item.code, item.note]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    const matchStatus =
      statusFilter.value === 'all' || item.status === statusFilter.value
    return matchSearch && matchStatus
  })
  return [...items].sort((a, b) => {
    if (sortMode.value === 'name-asc') {
      return a.name.localeCompare(b.name, 'zh-CN')
    }
    if (sortMode.value === 'fee-desc') {
      return b.entry_fee_cents - a.entry_fee_cents
    }
    if (sortMode.value === 'share-desc') {
      return b.consumer_share_bps - a.consumer_share_bps
    }
    return a.id - b.id
  })
})

const totalPages = computed(() =>
  Math.max(1, Math.ceil(filteredLevels.value.length / pageSize.value)),
)
const pagedLevels = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredLevels.value.slice(start, start + pageSize.value)
})

const activeAssignments = computed(() =>
  history.value.filter((item) => item.status === 'active'),
)
const scheduledAssignments = computed(() =>
  history.value.filter((item) => item.status === 'scheduled'),
)
const currentLevelByAgent = computed(() => {
  const result = new Map<number, AgentLevelHistory>()
  for (const item of activeAssignments.value) {
    if (!result.has(item.agent_tenant_id)) {
      result.set(item.agent_tenant_id, item)
    }
  }
  return result
})

watch([search, statusFilter, sortMode, pageSize], () => {
  page.value = 1
})
watch(totalPages, (value) => {
  if (page.value > value) page.value = value
})

function money(cents: number) {
  return '¥' + (cents / 100).toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

function percent(bps: number) {
  return (bps / 100).toFixed(2).replace(/\.00$/, '') + '%'
}

function settlementLabel(value: string) {
  return (
    {
      weekly: '每周',
      monthly: '每月',
      quarterly: '每季度',
      manual: '人工结算',
    }[value] || value
  )
}

function statusLabel(value: string) {
  return value === 'active' ? '启用' : value === 'inactive' ? '停用' : value
}

function historyStatusLabel(value: string) {
  return (
    {
      active: '当前生效',
      scheduled: '待生效',
      ended: '历史',
      cancelled: '已取消',
    }[value] || value
  )
}

function resetForm() {
  editing.value = null
  Object.assign(form, {
    code: '',
    name: '',
    status: 'active',
    entry_fee_yuan: '',
    included_devices: '0',
    device_discount_percent: '100',
    consumer_share_percent: '0',
    reserve_percent: '0',
    settlement_cycle: 'monthly',
    hold_days: '0',
    oem_enabled: false,
    note: '',
  })
}

function openCreate() {
  resetForm()
  editorOpen.value = true
}

function openEdit(item: AgentLevel) {
  editing.value = item
  Object.assign(form, {
    code: item.code,
    name: item.name,
    status: item.status,
    entry_fee_yuan: String(item.entry_fee_cents / 100),
    included_devices: String(item.included_devices),
    device_discount_percent: String(item.device_discount_bps / 100),
    consumer_share_percent: String(item.consumer_share_bps / 100),
    reserve_percent: String(item.reserve_bps / 100),
    settlement_cycle: item.settlement_cycle,
    hold_days: String(item.hold_days),
    oem_enabled: item.oem_enabled,
    note: item.note,
  })
  editorOpen.value = true
}

function numberValue(value: string, fallback = 0) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function levelPayload() {
  return {
    code: form.code.trim(),
    name: form.name.trim(),
    status: form.status,
    entry_fee_cents: Math.max(0, Math.round(numberValue(form.entry_fee_yuan) * 100)),
    included_devices: Math.max(0, Math.round(numberValue(form.included_devices))),
    device_discount_bps: Math.max(
      0,
      Math.round(numberValue(form.device_discount_percent, 100) * 100),
    ),
    consumer_share_bps: Math.max(
      0,
      Math.round(numberValue(form.consumer_share_percent) * 100),
    ),
    reserve_bps: Math.max(
      0,
      Math.round(numberValue(form.reserve_percent) * 100),
    ),
    settlement_cycle: form.settlement_cycle,
    hold_days: Math.max(0, Math.round(numberValue(form.hold_days))),
    oem_enabled: form.oem_enabled,
    note: form.note.trim(),
  }
}

async function saveLevel() {
  saving.value = true
  error.value = ''
  try {
    const payload = levelPayload()
    if (editing.value) {
      await updateAdminAgentLevel(editing.value.id, payload)
    } else {
      await createAdminAgentLevel(payload)
    }
    editorOpen.value = false
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '保存代理等级失败'
  } finally {
    saving.value = false
  }
}

function nowLocalInput() {
  const now = new Date()
  const local = new Date(now.getTime() - now.getTimezoneOffset() * 60000)
  return local.toISOString().slice(0, 16)
}

function prepareAssignment(agent?: AgentSummary) {
  if (agent) assignment.organization_id = agent.organization_id
  if (!assignment.effective_at) assignment.effective_at = nowLocalInput()
  const current = currentLevelByAgent.value.get(assignment.organization_id)
  if (current) assignment.level_id = current.level_id
}

async function submitAssignment() {
  if (!assignment.organization_id || !assignment.level_id) {
    error.value = '请选择代理和目标等级'
    return
  }
  assigning.value = true
  error.value = ''
  try {
    const effective = assignment.effective_at
      ? new Date(assignment.effective_at).toISOString()
      : new Date().toISOString()
    await assignAdminAgentLevel(assignment.organization_id, {
      level_id: assignment.level_id,
      effective_at: effective,
      reason: assignment.reason.trim(),
    })
    assignment.reason = ''
    await load()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '调整代理等级失败'
  } finally {
    assigning.value = false
  }
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [levelData, agentData] = await Promise.all([
      getAdminAgentLevels(),
      getAdminAgents(),
    ])
    levels.value = levelData.levels
    history.value = levelData.history
    agents.value = agentData.items
    if (!assignment.organization_id && agents.value.length) {
      assignment.organization_id = agents.value[0].organization_id
    }
    if (!assignment.level_id && levels.value.length) {
      const current = currentLevelByAgent.value.get(assignment.organization_id)
      assignment.level_id = current?.level_id || levels.value.find((item) => item.status === 'active')?.id || 0
    }
    if (!assignment.effective_at) assignment.effective_at = nowLocalInput()
  } catch (value) {
    error.value = value instanceof Error ? value.message : '读取代理等级失败'
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="management-page agent-levels-page">
    <ModulePageNav context="agents" active-title="代理等级" />

    <section class="feature-workspace-hero">
      <div>
        <p class="section-kicker">AGENT LEVEL POLICY</p>
        <h2>代理等级</h2>
        <p>商业等级与代理上下级完全分离。等级政策可调整，但代理每一次升级/降级都会保留独立生效历史。</p>
      </div>
      <button v-if="canManage" class="primary-button" type="button" @click="openCreate">
        ＋ 新建等级
      </button>
    </section>

    <p v-if="error" class="inline-error">{{ error }}</p>

    <section class="module-hub-metrics-v2">
      <article class="module-hub-metric-v2 tone-primary">
        <span>等级政策</span>
        <strong>{{ levels.length }}</strong>
        <small>独立商业等级</small>
      </article>
      <article class="module-hub-metric-v2 tone-success">
        <span>启用等级</span>
        <strong>{{ levels.filter((item) => item.status === 'active').length }}</strong>
        <small>可分配给代理</small>
      </article>
      <article class="module-hub-metric-v2 tone-neutral">
        <span>当前等级关系</span>
        <strong>{{ activeAssignments.length }}</strong>
        <small>当前生效记录</small>
      </article>
      <article class="module-hub-metric-v2 tone-warning">
        <span>待生效调整</span>
        <strong>{{ scheduledAssignments.length }}</strong>
        <small>未来生效记录</small>
      </article>
    </section>

    <section class="settings-card feature-record-panel">
      <DataListControls
        v-model:view-mode="viewMode"
        v-model:search="search"
        v-model:status="statusFilter"
        v-model:sort="sortMode"
        v-model:page-size="pageSize"
        search-placeholder="等级名称 / 编码 / 说明"
        :status-options="statusOptions"
        :sort-options="sortOptions"
      />

      <div v-if="loading" class="panel-loading">正在读取代理等级...</div>

      <div v-else-if="viewMode === 'table'" class="data-table-wrap">
        <table class="data-table agent-level-table">
          <thead>
            <tr>
              <th>等级</th>
              <th>准入门槛</th>
              <th>首批设备</th>
              <th>设备折扣</th>
              <th>消费返还</th>
              <th>准备金</th>
              <th>结算</th>
              <th>状态</th>
              <th v-if="canManage">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in pagedLevels" :key="item.id">
              <td><strong>{{ item.name }}</strong><small>{{ item.code }}</small></td>
              <td>{{ money(item.entry_fee_cents) }}</td>
              <td>{{ item.included_devices }} 台</td>
              <td>{{ percent(item.device_discount_bps) }}</td>
              <td>{{ percent(item.consumer_share_bps) }}</td>
              <td>{{ percent(item.reserve_bps) }}</td>
              <td><span>{{ settlementLabel(item.settlement_cycle) }}</span><small>留存 {{ item.hold_days }} 天</small></td>
              <td><span class="status-pill">{{ statusLabel(item.status) }}</span></td>
              <td v-if="canManage"><button class="text-action" type="button" @click="openEdit(item)">编辑政策</button></td>
            </tr>
          </tbody>
        </table>
        <div v-if="filteredLevels.length === 0" class="empty-state">暂无符合条件的代理等级。</div>
      </div>

      <div v-else class="feature-record-grid">
        <article v-for="item in pagedLevels" :key="item.id" class="feature-record-card">
          <header>
            <div><span>{{ item.code }}</span><h3>{{ item.name }}</h3></div>
            <span class="status-pill">{{ statusLabel(item.status) }}</span>
          </header>
          <dl>
            <div><dt>准入门槛</dt><dd>{{ money(item.entry_fee_cents) }}</dd></div>
            <div><dt>首批设备</dt><dd>{{ item.included_devices }} 台</dd></div>
            <div><dt>设备折扣</dt><dd>{{ percent(item.device_discount_bps) }}</dd></div>
            <div><dt>消费返还</dt><dd>{{ percent(item.consumer_share_bps) }}</dd></div>
            <div><dt>准备金</dt><dd>{{ percent(item.reserve_bps) }}</dd></div>
            <div><dt>OEM</dt><dd>{{ item.oem_enabled ? '允许' : '不允许' }}</dd></div>
          </dl>
          <footer v-if="canManage">
            <button class="text-action" type="button" @click="openEdit(item)">编辑政策</button>
          </footer>
        </article>
      </div>

      <PaginationBar
        v-model:page="page"
        :total-pages="totalPages"
        :total="filteredLevels.length"
        :page-size="pageSize"
      />
    </section>

    <section class="settings-card agent-level-assignment-panel">
      <header class="commercial-section-head">
        <div>
          <span class="section-kicker">LEVEL ASSIGNMENT</span>
          <h3>代理等级调整</h3>
          <p>调整时只新增一条历史，不覆盖旧等级。支持立即生效或预约未来时间生效。</p>
        </div>
      </header>

      <div class="agent-level-assignment-grid">
        <label>
          <span>代理</span>
          <select v-model.number="assignment.organization_id" @change="prepareAssignment()">
            <option v-for="agent in agents" :key="agent.organization_id" :value="agent.organization_id">
              {{ agent.name }}（{{ agent.code }}）
            </option>
          </select>
        </label>
        <label>
          <span>目标等级</span>
          <select v-model.number="assignment.level_id">
            <option
              v-for="item in levels.filter((level) => level.status === 'active')"
              :key="item.id"
              :value="item.id"
            >
              {{ item.name }}
            </option>
          </select>
        </label>
        <label>
          <span>生效时间</span>
          <input v-model="assignment.effective_at" type="datetime-local" />
        </label>
        <label class="assignment-reason">
          <span>调整原因</span>
          <input v-model="assignment.reason" type="text" maxlength="512" placeholder="升级、降级、续约调整等" />
        </label>
        <button
          v-if="canManage"
          class="primary-button"
          type="button"
          :disabled="assigning"
          @click="submitAssignment"
        >
          {{ assigning ? '提交中...' : '提交等级调整' }}
        </button>
      </div>

      <div class="data-table-wrap agent-level-history-table">
        <table class="data-table">
          <thead>
            <tr><th>代理</th><th>原等级</th><th>新等级</th><th>状态</th><th>生效时间</th><th>原因</th></tr>
          </thead>
          <tbody>
            <tr v-for="item in history.slice(0, 100)" :key="item.id">
              <td>{{ item.agent_name }}</td>
              <td>{{ item.previous_level_name || '首次定级' }}</td>
              <td><strong>{{ item.level_name }}</strong><small>{{ item.level_code }}</small></td>
              <td><span class="status-pill">{{ historyStatusLabel(item.status) }}</span></td>
              <td>{{ new Date(item.effective_at).toLocaleString('zh-CN') }}</td>
              <td>{{ item.reason || '—' }}</td>
            </tr>
          </tbody>
        </table>
        <div v-if="history.length === 0" class="empty-state">暂无等级变更历史。</div>
      </div>
    </section>

    <div v-if="editorOpen" class="feature-editor-backdrop" @click.self="editorOpen = false">
      <section class="feature-editor-panel agent-level-editor">
        <header>
          <div>
            <span class="section-kicker">{{ editing ? 'EDIT POLICY' : 'CREATE POLICY' }}</span>
            <h3>{{ editing ? '编辑代理等级' : '新建代理等级' }}</h3>
          </div>
          <button class="icon-button" type="button" @click="editorOpen = false">×</button>
        </header>

        <div class="feature-editor-grid">
          <label><span>等级编码</span><input v-model="form.code" type="text" /></label>
          <label><span>等级名称</span><input v-model="form.name" type="text" /></label>
          <label>
            <span>状态</span>
            <select v-model="form.status"><option value="active">启用</option><option value="inactive">停用</option></select>
          </label>
          <label><span>准入门槛（元）</span><input v-model="form.entry_fee_yuan" type="number" min="0" step="0.01" /></label>
          <label><span>首批设备（台）</span><input v-model="form.included_devices" type="number" min="0" /></label>
          <label><span>设备拿货折扣（%）</span><input v-model="form.device_discount_percent" type="number" min="0" max="100" step="0.01" /></label>
          <label><span>消费资金返还（%）</span><input v-model="form.consumer_share_percent" type="number" min="0" max="100" step="0.01" /></label>
          <label><span>准备金比例（%）</span><input v-model="form.reserve_percent" type="number" min="0" max="100" step="0.01" /></label>
          <label>
            <span>结算周期</span>
            <select v-model="form.settlement_cycle">
              <option value="weekly">每周</option>
              <option value="monthly">每月</option>
              <option value="quarterly">每季度</option>
              <option value="manual">人工结算</option>
            </select>
          </label>
          <label><span>留存天数</span><input v-model="form.hold_days" type="number" min="0" max="3650" /></label>
          <label class="feature-editor-wide feature-checkbox-row">
            <span>OEM 权限</span>
            <label class="feature-checkbox"><input v-model="form.oem_enabled" type="checkbox" /><span>允许该等级使用 OEM 能力</span></label>
          </label>
          <label class="feature-editor-wide"><span>等级说明</span><textarea v-model="form.note" rows="3"></textarea></label>
        </div>

        <p v-if="error" class="inline-error">{{ error }}</p>
        <footer>
          <button class="ghost-button" type="button" @click="editorOpen = false">取消</button>
          <button class="primary-button" type="button" :disabled="saving" @click="saveLevel">
            {{ saving ? '保存中...' : '保存等级政策' }}
          </button>
        </footer>
      </section>
    </div>
  </div>
</template>
